//go:build integration

package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	mdns "github.com/slidebolt/plugin-esphome/internal/mdns"
	testkit "github.com/slidebolt/sb-testkit"
	messenger "github.com/slidebolt/sb-messenger-sdk"
)

// TestLuaFade_RealESPHomeLights_Integration discovers real ESPHome devices via
// mDNS, registers their light entities in storage, then runs the Lua fade script
// via the NATS API and verifies that brightness commands arrive on the correct
// NATS subjects in ascending order.
//
// Run: go test -tags integration -v -run TestLuaFade_RealESPHomeLights_Integration ./cmd/plugin-esphome/
func TestLuaFade_RealESPHomeLights_Integration(t *testing.T) {
	env := testkit.NewTestEnv(t)
	env.Start("messenger")
	env.Start("storage")
	env.Start("sb-script")

	store := env.Storage()
	msg := env.Messenger()

	// Discover real devices.
	disc, err := mdns.NewDiscovery(mdns.WithTimeout(10 * time.Second))
	if err != nil {
		t.Fatalf("create discovery: %v", err)
	}

	ctx := context.Background()
	t.Log("discovering ESPHome devices via mDNS...")
	devices, err := disc.Discover(ctx)
	if err != nil {
		t.Fatalf("discovery: %v", err)
	}
	if len(devices) == 0 {
		t.Skip("no ESPHome devices found on network")
	}
	t.Logf("found %d device(s)", len(devices))

	// Register light entities from all discovered devices.
	var allLights []discoveredLight
	for _, dev := range devices {
		if dev.HasAPIKey() && getAPIEncryptionKey() == "" {
			t.Logf("skipping %s (encrypted, no key)", dev.Name)
			continue
		}
		t.Logf("connecting to %s at %s...", dev.Name, dev.GetAddress())
		lights := registerLightsFromDevice(t, dev, store)
		allLights = append(allLights, lights...)
	}

	if len(allLights) == 0 {
		t.Skip("no light entities found on any discovered device")
	}
	t.Logf("registered %d light(s) total", len(allLights))
	for _, l := range allLights {
		t.Logf("  %s (%s)", l.name, l.entityKey)
	}

	// Subscribe to light_set_brightness on all discovered lights before starting.
	type brightCmd struct {
		entityKey  string
		brightness int
	}
	received := make(chan brightCmd, 500)

	for _, l := range allLights {
		l := l
		subject := l.entityKey + ".command.light_set_brightness"
		sub, err := msg.Subscribe(subject, func(m *messenger.Message) {
			var cmd struct {
				Brightness int `json:"brightness"`
			}
			if err := json.Unmarshal(m.Data, &cmd); err != nil {
				return
			}
			received <- brightCmd{l.entityKey, cmd.Brightness}
		})
		if err != nil {
			t.Fatalf("subscribe %s: %v", subject, err)
		}
		defer sub.Unsubscribe()
	}

	saveScriptDefinition(t, store, "esphome_fade", loadLua(t, "test.integration.lua_fade.lua"))

	// Start the script targeting all plugin-esphome lights.
	startResp := integScriptAPI(t, msg, "script.start", map[string]string{
		"name":  "esphome_fade",
		"query": "?type=light",
	})
	if !startResp.OK {
		t.Fatalf("script.start: %s", startResp.Error)
	}
	t.Logf("script started (hash=%s)", startResp.Hash)

	// Collect brightness readings per entity.
	const wantSteps = 4
	readings := make(map[string][]int)
	for _, l := range allLights {
		readings[l.entityKey] = nil
	}

	deadline := time.After(5 * time.Second)
	done := false
	for !done {
		select {
		case cmd := <-received:
			readings[cmd.entityKey] = append(readings[cmd.entityKey], cmd.brightness)
		case <-deadline:
			done = true
		}
		if !done {
			allDone := true
			for _, v := range readings {
				if len(v) < wantSteps {
					allDone = false
					break
				}
			}
			done = allDone
		}
	}

	// Stop the script.
	integScriptAPI(t, msg, "script.stop", map[string]string{
		"name": "esphome_fade", "query": "?type=light",
	})

	// Assert: each light must have received at least wantSteps brightness values
	// in non-decreasing order starting at 0.
	wantSequence := []int{0, 50, 100, 150}
	allPassed := true
	for _, l := range allLights {
		got := readings[l.entityKey]
		if len(got) < wantSteps {
			t.Errorf("light %s (%s): got %d readings, want >=%d", l.name, l.entityKey, len(got), wantSteps)
			allPassed = false
			continue
		}
		for i, want := range wantSequence {
			if got[i] != want {
				t.Errorf("light %s tick %d: brightness=%d, want %d", l.name, i, got[i], want)
				allPassed = false
			}
		}
		if allPassed {
			t.Logf("✓ %s: brightness ramp %v... correct", l.name, got[:wantSteps])
		}
	}
}
