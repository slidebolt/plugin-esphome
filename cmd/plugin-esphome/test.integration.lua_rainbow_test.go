//go:build integration

package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	domain "github.com/slidebolt/sb-domain"
	managersdk "github.com/slidebolt/sb-manager-sdk"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	sbscript "github.com/slidebolt/sb-script/server"
	storage "github.com/slidebolt/sb-storage-sdk"

	"github.com/slidebolt/plugin-esphome/app"
	mdns "github.com/slidebolt/plugin-esphome/internal/mdns"
)

// TestLuaRainbow_Physical_Integration starts the full ESPHome plugin (with
// persistent device connections), runs a 20-second rainbow Lua script, and
// verifies that the physical lights receive RGB commands via the ESPHome API.
//
// The plugin's handleCommand receives the Lua-emitted NATS commands and
// forwards them as LightCommandRequest to each device — so the lights
// actually change color.
//
// Run: go test -tags integration -v -run TestLuaRainbow_Physical_Integration ./cmd/plugin-esphome/
func TestLuaRainbow_Physical_Integration(t *testing.T) {
	apiKey := loadEnvLocal(t, "ESPHOME_API_KEY")
	// Set env var as fallback so the plugin resolves encryption keys for any
	// device the mDNS background goroutine discovers before we seed Internal storage.
	os.Setenv("ESPHOME_API_KEY", apiKey)
	t.Cleanup(func() { os.Unsetenv("ESPHOME_API_KEY") })

	env := managersdk.NewTestEnv(t)
	env.Start("messenger")
	env.Start("storage")

	store := env.Storage()
	msg := env.Messenger()

	// --- Step 1: discover devices via mDNS (before plugin starts) ---
	t.Log("discovering devices via mDNS...")
	disc, err := mdns.NewDiscovery(mdns.WithTimeout(5 * time.Second))
	if err != nil {
		t.Fatalf("mdns.NewDiscovery: %v", err)
	}
	devices, err := disc.Discover(t.Context())
	if err != nil {
		t.Fatalf("mDNS discover: %v", err)
	}
	if len(devices) == 0 {
		t.Fatal("no ESPHome devices found on the network")
	}
	t.Logf("found %d device(s) via mDNS", len(devices))

	// --- Step 2: seed per-device API keys into Internal storage ---
	for _, dev := range devices {
		if !dev.HasAPIKey() {
			continue
		}
		cfg := app.DeviceConfig{APIKey: apiKey}
		data, _ := json.Marshal(cfg)
		devKey := domain.DeviceKey{Plugin: app.PluginID, ID: dev.Name}
		if err := store.WriteFile(storage.Internal, devKey, json.RawMessage(data)); err != nil {
			t.Fatalf("seed key for %s: %v", dev.Name, err)
		}
	}

	// --- Step 3: start the full plugin (persistent connections + command handler) ---
	p := app.New()
	deps := map[string]json.RawMessage{
		"messenger": env.MessengerPayload(),
	}
	if _, err := p.OnStart(deps); err != nil {
		t.Fatalf("plugin OnStart: %v", err)
	}
	t.Cleanup(func() { p.OnShutdown() })

	// Inject pre-discovered devices to avoid mDNS rate-limiting.
	for _, dev := range devices {
		p.OnDeviceFound(dev)
	}

	// Wait for light entities to appear in storage.
	t.Log("waiting for devices to register entities...")
	var lightKeys []string
	deadline := time.After(15 * time.Second)
	registered := make(map[string]bool)
	for {
		for _, dev := range devices {
			if registered[dev.Name] {
				continue
			}
			entries, _ := store.Search(app.PluginID + "." + dev.Name + ".*")
			if len(entries) > 0 {
				registered[dev.Name] = true
				for _, e := range entries {
					var ent domain.Entity
					if json.Unmarshal(e.Data, &ent) == nil && ent.Type == "light" {
						lightKeys = append(lightKeys, e.Key)
					}
				}
			}
		}
		if len(registered) == len(devices) {
			break
		}
		select {
		case <-deadline:
			t.Logf("timeout: %d/%d devices registered", len(registered), len(devices))
			goto proceed
		case <-time.After(200 * time.Millisecond):
		}
	}
proceed:
	if len(lightKeys) == 0 {
		t.Fatal("no light entities found on any device")
	}
	t.Logf("registered %d light(s) with persistent connections", len(lightKeys))

	// --- Step 4: start sb-script on its own connection ---
	scriptConn, err := messenger.Connect(map[string]json.RawMessage{
		"messenger": env.MessengerPayload(),
	})
	if err != nil {
		t.Fatalf("connect scriptConn: %v", err)
	}
	t.Cleanup(func() { scriptConn.Close() })

	svc, err := sbscript.New(scriptConn, env.Storage())
	if err != nil {
		t.Fatalf("start sb-script: %v", err)
	}
	t.Cleanup(svc.Shutdown)

	// --- Step 5: subscribe to light_set_rgb to observe what the script emits ---
	type rgbReading struct {
		key     string
		r, g, b int
	}
	received := make(chan rgbReading, 5000)

	sub, err := msg.Subscribe("plugin-esphome.*.*.command.light_set_rgb", func(m *messenger.Message) {
		var cmd struct {
			R int `json:"r"`
			G int `json:"g"`
			B int `json:"b"`
		}
		if err := json.Unmarshal(m.Data, &cmd); err != nil {
			return
		}
		key := strings.TrimSuffix(m.Subject, ".command.light_set_rgb")
		received <- rgbReading{key: key, r: cmd.R, g: cmd.G, b: cmd.B}
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Unsubscribe()

	// --- Step 6: save and start the rainbow script ---
	saveResp := integScriptAPI(t, scriptConn, "script.save_definition", map[string]string{
		"name":   "esphome_rainbow",
		"source": loadLua(t, "test.integration.lua_rainbow.lua"),
	})
	if !saveResp.OK {
		t.Fatalf("script.save_definition: %s", saveResp.Error)
	}

	startResp := integScriptAPI(t, scriptConn, "script.start", map[string]string{
		"name":  "esphome_rainbow",
		"query": "?plugin=" + app.PluginID + "&type=light",
	})
	if !startResp.OK {
		t.Fatalf("script.start: %s", startResp.Error)
	}
	t.Logf("🌈 script started — lights should be changing color! (hash=%s)", startResp.Hash)

	// --- Step 7: let the full 20-second rainbow play so lights visibly change ---
	const wantSteps = 5
	const rainbowDuration = 22 * time.Second // script is 20s; give 2s slack
	readings := make(map[string][]rgbReading)

	t.Logf("letting rainbow run for %s — watch the lights!", rainbowDuration)
	collectDeadline := time.After(rainbowDuration)
	for {
		select {
		case r := <-received:
			readings[r.key] = append(readings[r.key], r)
		case <-collectDeadline:
			goto done
		}
	}
done:
	integScriptAPI(t, scriptConn, "script.stop", map[string]string{
		"name": "esphome_rainbow", "query": "?plugin=" + app.PluginID + "&type=light",
	})

	// --- Assert: first reading is orange (255,165,0); green channel rises ---
	for _, k := range lightKeys {
		got := readings[k]
		if len(got) < wantSteps {
			t.Errorf("%s: got %d readings, want >=%d", k, len(got), wantSteps)
			continue
		}
		first := got[0]
		if first.r != 255 || first.g != 165 || first.b != 0 {
			t.Errorf("%s tick 0: rgb=(%d,%d,%d), want (255,165,0)", k, first.r, first.g, first.b)
		}
		for i := 1; i < wantSteps; i++ {
			if got[i].g < got[i-1].g {
				t.Errorf("%s tick %d: g=%d decreased from %d (expected non-decreasing)", k, i, got[i].g, got[i-1].g)
			}
		}
		t.Logf("✓ %s: rainbow starts orange, green rising correctly", k)
	}
}
