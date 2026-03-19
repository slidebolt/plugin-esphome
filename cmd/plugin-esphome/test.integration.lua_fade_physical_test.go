//go:build integration

package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/slidebolt/plugin-esphome/app"
	domain "github.com/slidebolt/sb-domain"
	managersdk "github.com/slidebolt/sb-manager-sdk"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	storage "github.com/slidebolt/sb-storage-sdk"
)

// TestLuaFade_Physical_Integration starts the full plugin in-process, waits
// for real ESPHome devices to be discovered and registered via mDNS, then
// runs a Lua fade script. Verifies that state.changed events come back with
// ascending brightness values — proving the commands reached the devices.
//
// Run: go test -tags integration -v -run TestLuaFade_Physical_Integration ./cmd/plugin-esphome/
func TestLuaFade_Physical_Integration(t *testing.T) {
	env := managersdk.NewTestEnv(t)
	env.Start("messenger")
	env.Start("storage")
	env.Start("sb-script")

	store := env.Storage()
	msg := env.Messenger()

	// Start the plugin in-process using the test env's messenger bus.
	p := app.New()
	deps := map[string]json.RawMessage{
		"messenger": env.MessengerPayload(),
	}
	if _, err := p.OnStart(deps); err != nil {
		t.Fatalf("plugin OnStart: %v", err)
	}
	t.Cleanup(func() { p.OnShutdown() })

	// Poll storage until the light count stabilizes (no new lights for 3s).
	// This ensures we don't start the script before most devices have connected.
	t.Log("waiting for plugin to discover and register lights...")
	var lights []storage.Entry
	queryLights := func() []storage.Entry {
		result, _ := store.Query(storage.Query{
			Where: []storage.Filter{
				{Field: "plugin", Op: storage.Eq, Value: app.PluginID},
				{Field: "type", Op: storage.Eq, Value: "light"},
			},
		})
		return result
	}
	deadline := time.After(30 * time.Second)
	stableDeadline := time.NewTimer(3 * time.Second)
	lastCount := -1
	for {
		lights = queryLights()
		if len(lights) != lastCount {
			lastCount = len(lights)
			stableDeadline.Reset(3 * time.Second)
		}
		select {
		case <-deadline:
			if len(lights) == 0 {
				t.Fatal("timed out waiting for plugin to register light entities")
			}
			goto stable
		case <-stableDeadline.C:
			if len(lights) > 0 {
				goto stable
			}
		case <-time.After(500 * time.Millisecond):
		}
	}
stable:
	t.Logf("plugin registered %d light(s)", len(lights))
	for _, e := range lights {
		t.Logf("  %s", e.Key)
	}

	// Subscribe to state.changed.plugin-esphome.> — these are published by
	// the storage server whenever the plugin writes updated brightness back.
	type stateChange struct {
		key        string
		brightness int
	}
	received := make(chan stateChange, 500)

	sub, err := msg.Subscribe("state.changed."+app.PluginID+".>", func(m *messenger.Message) {
		var ent domain.Entity
		if err := json.Unmarshal(m.Data, &ent); err != nil {
			return
		}
		if _, ok := ent.State.(domain.Light); !ok {
			return
		}
		light := ent.State.(domain.Light)
		received <- stateChange{key: m.Subject, brightness: light.Brightness}
	})
	if err != nil {
		t.Fatalf("subscribe state.changed: %v", err)
	}
	defer sub.Unsubscribe()

	// Turn all lights off so the test starts from a known state.
	for _, e := range lights {
		msg.Publish(e.Key+".command.light_turn_off", []byte(`{}`))
	}
	time.Sleep(500 * time.Millisecond)
	// Drain any state.changed events from the turn-off so the channel is clean.
	for {
		select {
		case <-received:
		default:
			goto drained
		}
	}
drained:

	// Save and start a Lua fade script targeting all plugin-esphome lights.
	// Steps: 0 → 50 → 100 → 150, every 200ms.
	saveResp := integScriptAPI(t, msg, "script.save_definition", map[string]string{
		"name":   "esphome_physical_fade",
		"source": loadLua(t, "test.integration.lua_fade_physical.lua"),
	})
	if !saveResp.OK {
		t.Fatalf("script.save_definition: %s", saveResp.Error)
	}

	startResp := integScriptAPI(t, msg, "script.start", map[string]string{
		"name":  "esphome_physical_fade",
		"query": "?plugin=" + app.PluginID + "&type=light",
	})
	if !startResp.OK {
		t.Fatalf("script.start: %s", startResp.Error)
	}
	t.Logf("script started (hash=%s)", startResp.Hash)

	// Collect state.changed readings per entity key.
	// Each light should report brightness 0 → 50 → 100 → 150 in order.
	const wantSteps = 4
	readings := make(map[string][]int)

	deadline2 := time.After(10 * time.Second)
	for {
		allDone := true
		for _, e := range lights {
			if len(readings[e.Key]) < wantSteps {
				allDone = false
				break
			}
		}
		if allDone {
			break
		}
		select {
		case sc := <-received:
			// Strip "state.changed." prefix to get the entity key.
			key := sc.key[len("state.changed."):]
			readings[key] = append(readings[key], sc.brightness)
			t.Logf("  %s brightness=%d", key, sc.brightness)
		case <-deadline2:
			goto done
		}
	}
done:
	integScriptAPI(t, msg, "script.stop", map[string]string{
		"name": "esphome_physical_fade", "query": "?plugin=" + app.PluginID + "&type=light",
	})

	// Assert each light received brightness values in ascending order.
	wantSequence := []int{0, 50, 100, 150}
	for _, e := range lights {
		got := readings[e.Key]
		if len(got) < wantSteps {
			t.Errorf("%s: got %d readings, want >=%d (got %v)", e.Key, len(got), wantSteps, got)
			continue
		}
		for i, want := range wantSequence {
			if got[i] != want {
				t.Errorf("%s tick %d: brightness=%d, want %d", e.Key, i, got[i], want)
			}
		}
		t.Logf("✓ %s: brightness ramp %v correct", e.Key, got[:wantSteps])
	}
}
