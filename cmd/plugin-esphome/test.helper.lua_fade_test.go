//go:build bdd || local || integration

package main

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/slidebolt/plugin-esphome/app"
	domain "github.com/slidebolt/sb-domain"
	managersdk "github.com/slidebolt/sb-manager-sdk"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	scriptserver "github.com/slidebolt/sb-script/server"
	storage "github.com/slidebolt/sb-storage-sdk"
)

// startScript starts the sb-script engine against the given TestEnv's
// messenger and storage. Registers cleanup via t.Cleanup.
func startScript(t *testing.T, env *managersdk.TestEnv) {
	t.Helper()
	scriptMsg, err := messenger.Connect(map[string]json.RawMessage{
		"messenger": env.MessengerPayload(),
	})
	if err != nil {
		t.Fatalf("sb-script messenger: %v", err)
	}
	svc, err := scriptserver.New(scriptMsg, env.Storage())
	if err != nil {
		t.Fatalf("sb-script start: %v", err)
	}
	// Flush ensures NATS has registered subscriptions before first request.
	if err := scriptMsg.Flush(); err != nil {
		t.Fatalf("sb-script flush: %v", err)
	}
	t.Cleanup(func() { svc.Shutdown(); scriptMsg.Close() })
}

// scriptResp is the standard envelope returned by the script.* NATS API.
type scriptResp struct {
	OK    bool   `json:"ok"`
	Hash  string `json:"hash,omitempty"`
	Error string `json:"error,omitempty"`
}

func scriptAPI(t *testing.T, msg messenger.Messenger, subject string, body any) scriptResp {
	t.Helper()
	data, _ := json.Marshal(body)
	resp, err := msg.Request(subject, data, 5*time.Second)
	if err != nil {
		t.Fatalf("script API %s: %v", subject, err)
	}
	var r scriptResp
	if err := json.Unmarshal(resp.Data, &r); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	return r
}

func loadLua(t *testing.T, name string) string {
	t.Helper()
	src, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("os.ReadFile %s: %v", name, err)
	}
	return string(src)
}

func seedLight(t *testing.T, store storage.Storage, deviceID, entityID, name string) {
	t.Helper()
	e := domain.Entity{
		ID: entityID, Plugin: app.PluginID, DeviceID: deviceID,
		Type:     "light",
		Name:     name,
		Commands: []string{"light_turn_on", "light_turn_off", "light_set_brightness"},
		State:    domain.Light{Power: false},
	}
	if err := store.Save(e); err != nil {
		t.Fatalf("seed light %s: %v", entityID, err)
	}
}

// TestLuaFade_ESPHomeLights proves that a Lua fade script targets ESPHome
// light entities discovered by the plugin, ramps brightness correctly over
// NATS, and that commands arrive on the plugin's own command subjects.
//
// No real hardware required — entities are seeded directly into storage,
// exactly as connectAndRegister would populate them after mDNS discovery.
func TestLuaFade_ESPHomeLights(t *testing.T) {
	env := managersdk.NewTestEnv(t)
	env.Start("messenger")
	env.Start("storage")
	startScript(t, env)

	store := env.Storage()
	msg := env.Messenger()

	// Two Edison lights — same device, different entity IDs.
	const deviceID = "edison-bedroom"
	lights := []struct{ id, name string }{
		{"edison-bedroom_1001", "Edison A"},
		{"edison-bedroom_1002", "Edison B"},
	}
	for _, l := range lights {
		seedLight(t, store, deviceID, l.id, l.name)
	}

	// Save the fade script definition via NATS API.
	// The script ramps brightness from 0 to 254 in steps of 50 every 50ms.
	saveResp := scriptAPI(t, msg, "script.save_definition", map[string]string{
		"name":   "esphome_fade",
		"source": loadLua(t, "lua_fade_test.lua"),
	})
	if !saveResp.OK {
		t.Fatalf("save_definition: %s", saveResp.Error)
	}

	// Subscribe to light_set_brightness on both entities before starting.
	type entry struct {
		id         string
		brightness int
	}
	received := make(chan entry, 200)

	for _, l := range lights {
		l := l
		subject := app.PluginID + "." + deviceID + "." + l.id + ".command.light_set_brightness"
		sub, err := msg.Subscribe(subject, func(m *messenger.Message) {
			var cmd struct {
				Brightness int `json:"brightness"`
			}
			if err := json.Unmarshal(m.Data, &cmd); err != nil {
				return
			}
			received <- entry{l.id, cmd.Brightness}
		})
		if err != nil {
			t.Fatalf("subscribe %s: %v", subject, err)
		}
		defer sub.Unsubscribe()
	}

	// Start the script targeting all esphome lights.
	startResp := scriptAPI(t, msg, "script.start", map[string]string{
		"name":  "esphome_fade",
		"query": "?type=light",
	})
	if !startResp.OK {
		t.Fatalf("script.start: %s", startResp.Error)
	}

	// Collect brightness values per entity.
	// Expect the ramp: 0, 50, 100, 150 — at least 4 ticks within 1s.
	const wantTicks = 4
	const wantEntities = 2
	type entityReadings struct {
		values []int
	}
	readings := make(map[string]*entityReadings)
	for _, l := range lights {
		readings[l.id] = &entityReadings{}
	}

	deadline := time.After(time.Second)
	total := 0
	for total < wantTicks*wantEntities {
		select {
		case e := <-received:
			r := readings[e.id]
			r.values = append(r.values, e.brightness)
			total++
		case <-deadline:
			t.Fatalf("timeout: collected %d/%d readings", total, wantTicks*wantEntities)
		}
	}

	// Each entity must have received brightness values in ascending order.
	wantSequence := []int{0, 50, 100, 150}
	for _, l := range lights {
		got := readings[l.id].values
		if len(got) < wantTicks {
			t.Errorf("entity %s: got %d readings, want >=%d", l.id, len(got), wantTicks)
			continue
		}
		for i, want := range wantSequence {
			if got[i] != want {
				t.Errorf("entity %s tick %d: brightness=%d, want %d", l.id, i, got[i], want)
			}
		}
	}
}

// TestLuaFade_StopHaltsCommands proves StopScript ends the fade — no further
// brightness commands arrive after the stop call.
func TestLuaFade_StopHaltsCommands(t *testing.T) {
	env := managersdk.NewTestEnv(t)
	env.Start("messenger")
	env.Start("storage")
	startScript(t, env)

	store := env.Storage()
	msg := env.Messenger()

	seedLight(t, store, "edison-bedroom", "edison-bedroom_1001", "Edison A")

	scriptAPI(t, msg, "script.save_definition", map[string]string{
		"name":   "esphome_fade",
		"source": loadLua(t, "lua_fade_test.lua"),
	})

	arrived := make(chan int, 50)
	subject := app.PluginID + ".edison-bedroom.edison-bedroom_1001.command.light_set_brightness"
	sub, _ := msg.Subscribe(subject, func(m *messenger.Message) {
		var cmd struct {
			Brightness int `json:"brightness"`
		}
		json.Unmarshal(m.Data, &cmd)
		arrived <- cmd.Brightness
	})
	defer sub.Unsubscribe()

	scriptAPI(t, msg, "script.start", map[string]string{
		"name": "esphome_fade", "query": "?type=light",
	})

	// Wait for at least 3 commands.
	count := 0
	deadline := time.After(500 * time.Millisecond)
	for count < 3 {
		select {
		case <-arrived:
			count++
		case <-deadline:
			t.Fatalf("only %d commands arrived before stop", count)
		}
	}

	// Stop the script.
	stopResp := scriptAPI(t, msg, "script.stop", map[string]string{
		"name": "esphome_fade", "query": "?type=light",
	})
	if !stopResp.OK {
		t.Fatalf("script.stop: %s", stopResp.Error)
	}

	// Drain any in-flight commands, then assert silence.
	for len(arrived) > 0 {
		<-arrived
	}
	select {
	case v := <-arrived:
		t.Fatalf("received brightness=%d after StopScript", v)
	case <-time.After(300 * time.Millisecond):
		// silence confirmed
	}
}

// TestLuaFade_NewDevicePickedUp proves that adding a new ESPHome light after
// StartScript is picked up on the next timer tick via QueryService.Find.
func TestLuaFade_NewDevicePickedUp(t *testing.T) {
	env := managersdk.NewTestEnv(t)
	env.Start("messenger")
	env.Start("storage")
	startScript(t, env)

	store := env.Storage()
	msg := env.Messenger()

	// Start with one light.
	seedLight(t, store, "edison-bedroom", "edison-bedroom_1001", "Edison A")

	scriptAPI(t, msg, "script.save_definition", map[string]string{
		"name":   "esphome_fade",
		"source": loadLua(t, "lua_fade_test.lua"),
	})

	scriptAPI(t, msg, "script.start", map[string]string{
		"name": "esphome_fade", "query": "?type=light",
	})

	// Confirm first light is receiving commands.
	firstArrived := make(chan struct{}, 10)
	sub1, _ := msg.Subscribe(
		app.PluginID+".edison-bedroom.edison-bedroom_1001.command.light_set_brightness",
		func(m *messenger.Message) { firstArrived <- struct{}{} },
	)
	defer sub1.Unsubscribe()

	select {
	case <-firstArrived:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("first light never received a command")
	}

	// Add a second light mid-run (simulates a newly discovered ESPHome device).
	seedLight(t, store, "edison-bedroom", "edison-bedroom_1002", "Edison B")

	// Subscribe to the new light's commands.
	newArrived := make(chan struct{}, 10)
	sub2, _ := msg.Subscribe(
		app.PluginID+".edison-bedroom.edison-bedroom_1002.command.light_set_brightness",
		func(m *messenger.Message) { newArrived <- struct{}{} },
	)
	defer sub2.Unsubscribe()

	// The new device should appear on the next QueryService.Find tick.
	select {
	case <-newArrived:
		// new device picked up
	case <-time.After(500 * time.Millisecond):
		t.Fatal("newly added ESPHome light was not picked up by the running script")
	}
}
