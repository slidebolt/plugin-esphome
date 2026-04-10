//go:build bdd || local || integration

package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/slidebolt/plugin-esphome/app"
	domain "github.com/slidebolt/sb-domain"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	scriptserver "github.com/slidebolt/sb-script/server"
	storage "github.com/slidebolt/sb-storage-sdk"
	testkit "github.com/slidebolt/sb-testkit"
)

// startScript starts the sb-script engine against the given TestEnv's
// messenger and storage. Registers cleanup via t.Cleanup.
func startScript(t *testing.T, env *testkit.TestEnv) {
	t.Helper()
	startScriptWithDeps(t, env.MessengerPayload(), env.Storage())
}

func startScriptWithDeps(t *testing.T, payload json.RawMessage, store storage.Storage) {
	t.Helper()
	scriptMsg, err := messenger.Connect(map[string]json.RawMessage{
		"messenger": payload,
	})
	if err != nil {
		t.Fatalf("sb-script messenger: %v", err)
	}
	svc, err := scriptserver.New(scriptMsg, store)
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

func saveScriptDefinition(t *testing.T, store storage.Storage, name, source string) {
	t.Helper()
	data, err := json.Marshal(map[string]string{
		"type":     "script",
		"language": "lua",
		"name":     name,
		"source":   source,
	})
	if err != nil {
		t.Fatalf("marshal script %s: %v", name, err)
	}
	if err := store.Save(scriptDefBlob{key: "sb-script.scripts." + name, data: data}); err != nil {
		t.Fatalf("save script %s: %v", name, err)
	}
}

type scriptDefBlob struct {
	key  string
	data json.RawMessage
}

func (b scriptDefBlob) Key() string                  { return b.key }
func (b scriptDefBlob) MarshalJSON() ([]byte, error) { return b.data, nil }

func loadLua(t *testing.T, name string) string {
	t.Helper()
	src, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("os.ReadFile %s: %v", name, err)
	}
	return string(src)
}

type scriptInstanceMeta struct {
	Hash            string    `json:"hash"`
	QueryRef        string    `json:"queryRef,omitempty"`
	ResolvedTargets []string  `json:"resolvedTargets,omitempty"`
	LastError       string    `json:"lastError,omitempty"`
	FireCount       int       `json:"fireCount,omitempty"`
	LastFiredAt     time.Time `json:"lastFiredAt,omitempty"`
}

func waitForScriptInstance(t *testing.T, store storage.Storage, hash string, pred func(scriptInstanceMeta) bool) scriptInstanceMeta {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		entries, err := store.Search("sb-script.instances.*")
		if err == nil {
			for _, entry := range entries {
				var inst scriptInstanceMeta
				if err := json.Unmarshal(entry.Data, &inst); err == nil && inst.Hash == hash && pred(inst) {
					return inst
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for script instance %s", hash)
	return scriptInstanceMeta{}
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

func seedFadeLights(t *testing.T, store storage.Storage) []string {
	t.Helper()
	const deviceID = "edison-bedroom"
	lights := []struct{ id, name string }{
		{"edison-bedroom_1001", "Edison A"},
		{"edison-bedroom_1002", "Edison B"},
	}
	keys := make([]string, 0, len(lights))
	for _, l := range lights {
		seedLight(t, store, deviceID, l.id, l.name)
		keys = append(keys, app.PluginID+"."+deviceID+"."+l.id)
	}
	return keys
}

func saveESPHomeLightQuery(t *testing.T, store storage.Storage, name string) {
	t.Helper()
	if err := storage.EnsureQueryLayout(store); err != nil {
		t.Fatalf("EnsureQueryLayout: %v", err)
	}
	if err := storage.SaveQueryDefinition(store, name, storage.Query{
		Pattern: app.PluginID + ".>",
		Where: []storage.Filter{
			{Field: "plugin", Op: storage.Eq, Value: app.PluginID},
			{Field: "type", Op: storage.Eq, Value: "light"},
		},
	}); err != nil {
		t.Fatalf("SaveQueryDefinition: %v", err)
	}
}

func saveESPHomeLightGroupQuery(t *testing.T, store storage.Storage, name, group string) {
	t.Helper()
	if err := storage.EnsureQueryLayout(store); err != nil {
		t.Fatalf("EnsureQueryLayout: %v", err)
	}
	if err := storage.SaveQueryDefinition(store, name, storage.Query{
		Pattern: app.PluginID + ".>",
		Where: []storage.Filter{
			{Field: "plugin", Op: storage.Eq, Value: app.PluginID},
			{Field: "type", Op: storage.Eq, Value: "light"},
			{Field: "labels.group", Op: storage.Eq, Value: group},
		},
	}); err != nil {
		t.Fatalf("SaveQueryDefinition: %v", err)
	}
}

func waitForCommand(t *testing.T, ch <-chan string, want string) {
	t.Helper()
	deadline := time.After(500 * time.Millisecond)
	for {
		select {
		case got := <-ch:
			if want == "" || strings.HasSuffix(got, want) {
				return
			}
		case <-deadline:
			if want == "" {
				t.Fatal("timed out waiting for any command")
			}
			t.Fatalf("timed out waiting for command %s", want)
		}
	}
}

func TestLuaFade_QueryFindsSeededESPHomeLights(t *testing.T) {
	env := testkit.NewTestEnv(t)
	env.Start("messenger")
	env.Start("storage")

	store := env.Storage()
	keys := seedFadeLights(t, store)

	entries, err := store.Query(storage.Query{
		Pattern: app.PluginID + ".>",
		Where: []storage.Filter{
			{Field: "plugin", Op: storage.Eq, Value: app.PluginID},
			{Field: "type", Op: storage.Eq, Value: "light"},
		},
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(entries) != len(keys) {
		t.Fatalf("query returned %d entries, want %d", len(entries), len(keys))
	}
}

func TestLuaFade_StartWithQueryFieldLeavesResolvedTargetsEmpty(t *testing.T) {
	env := testkit.NewTestEnv(t)
	env.Start("messenger")
	env.Start("storage")
	startScript(t, env)

	store := env.Storage()
	msg := env.Messenger()

	seedFadeLights(t, store)
	saveScriptDefinition(t, store, "esphome_fade", loadLua(t, "lua_fade_test.lua"))

	seen := make(chan string, 10)
	sub, err := msg.Subscribe(app.PluginID+".>", func(m *messenger.Message) {
		if strings.Contains(m.Subject, ".command.") {
			seen <- m.Subject
		}
	})
	if err != nil {
		t.Fatalf("subscribe wildcard: %v", err)
	}
	defer sub.Unsubscribe()
	if err := msg.Flush(); err != nil {
		t.Fatalf("flush wildcard subscription: %v", err)
	}

	startResp := scriptAPI(t, msg, "script.start", map[string]string{
		"name":  "esphome_fade",
		"query": "?type=light",
	})
	if !startResp.OK {
		t.Fatalf("script.start: %s", startResp.Error)
	}

	inst := waitForScriptInstance(t, store, startResp.Hash, func(inst scriptInstanceMeta) bool {
		return inst.FireCount > 0
	})
	if inst.QueryRef != "" {
		t.Fatalf("queryRef = %q, want empty because script.start ignores unknown field \"query\"", inst.QueryRef)
	}
	if len(inst.ResolvedTargets) != 0 {
		t.Fatalf("resolvedTargets = %v, want none when only \"query\" is provided", inst.ResolvedTargets)
	}
	select {
	case got := <-seen:
		t.Fatalf("unexpected command published with ignored query field: %s", got)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestLuaFade_StartWithQueryRefPublishesCommands(t *testing.T) {
	env := testkit.NewTestEnv(t)
	env.Start("messenger")
	env.Start("storage")
	startScript(t, env)

	store := env.Storage()
	msg := env.Messenger()

	keys := seedFadeLights(t, store)
	saveESPHomeLightQuery(t, store, "esphome_lights")
	saveScriptDefinition(t, store, "esphome_fade", loadLua(t, "lua_fade_test.lua"))

	seen := make(chan string, 20)
	sub, err := msg.Subscribe(app.PluginID+".>", func(m *messenger.Message) {
		if strings.Contains(m.Subject, ".command.light_set_brightness") {
			seen <- m.Subject
		}
	})
	if err != nil {
		t.Fatalf("subscribe wildcard: %v", err)
	}
	defer sub.Unsubscribe()
	if err := msg.Flush(); err != nil {
		t.Fatalf("flush wildcard subscription: %v", err)
	}

	startResp := scriptAPI(t, msg, "script.start", map[string]string{
		"name":     "esphome_fade",
		"queryRef": "esphome_lights",
	})
	if !startResp.OK {
		t.Fatalf("script.start: %s", startResp.Error)
	}

	inst := waitForScriptInstance(t, store, startResp.Hash, func(inst scriptInstanceMeta) bool {
		return inst.FireCount > 0 && len(inst.ResolvedTargets) == len(keys)
	})
	if inst.QueryRef != "esphome_lights" {
		t.Fatalf("queryRef = %q, want esphome_lights", inst.QueryRef)
	}
	for _, key := range keys {
		found := false
		for _, got := range inst.ResolvedTargets {
			if got == key {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("resolvedTargets missing %s in %v", key, inst.ResolvedTargets)
		}
	}
	waitForCommand(t, seen, ".command.light_set_brightness")
}

// TestLuaFade_ESPHomeLights proves that a Lua fade script targets ESPHome
// light entities discovered by the plugin, ramps brightness correctly over
// NATS, and that commands arrive on the plugin's own command subjects.
//
// No real hardware required — entities are seeded directly into storage,
// exactly as connectAndRegister would populate them after mDNS discovery.
func TestLuaFade_ESPHomeLights(t *testing.T) {
	env := testkit.NewTestEnv(t)
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
	saveESPHomeLightQuery(t, store, "esphome_lights")

	// Save the fade script definition via NATS API.
	// The script ramps brightness from 0 to 254 in steps of 50 every 50ms.
	saveScriptDefinition(t, store, "esphome_fade", loadLua(t, "lua_fade_test.lua"))

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
	if err := msg.Flush(); err != nil {
		t.Fatalf("flush brightness subscriptions: %v", err)
	}

	// Start the script targeting all esphome lights.
	startResp := scriptAPI(t, msg, "script.start", map[string]string{
		"name":     "esphome_fade",
		"queryRef": "esphome_lights",
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
	env := testkit.NewTestEnv(t)
	env.Start("messenger")
	env.Start("storage")
	startScript(t, env)

	store := env.Storage()
	msg := env.Messenger()

	seedLight(t, store, "edison-bedroom", "edison-bedroom_1001", "Edison A")
	saveESPHomeLightQuery(t, store, "esphome_lights")

	saveScriptDefinition(t, store, "esphome_fade", loadLua(t, "lua_fade_test.lua"))

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
	if err := msg.Flush(); err != nil {
		t.Fatalf("flush stop subscription: %v", err)
	}

	scriptAPI(t, msg, "script.start", map[string]string{
		"name": "esphome_fade", "queryRef": "esphome_lights",
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
		"name": "esphome_fade", "queryRef": "esphome_lights",
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
	env := testkit.NewTestEnv(t)
	env.Start("messenger")
	env.Start("storage")
	startScript(t, env)

	store := env.Storage()
	msg := env.Messenger()

	// Start with one light.
	seedLight(t, store, "edison-bedroom", "edison-bedroom_1001", "Edison A")
	saveESPHomeLightQuery(t, store, "esphome_lights")

	saveScriptDefinition(t, store, "esphome_fade", loadLua(t, "lua_fade_test.lua"))

	scriptAPI(t, msg, "script.start", map[string]string{
		"name": "esphome_fade", "queryRef": "esphome_lights",
	})

	// Confirm first light is receiving commands.
	firstArrived := make(chan struct{}, 10)
	sub1, _ := msg.Subscribe(
		app.PluginID+".edison-bedroom.edison-bedroom_1001.command.light_set_brightness",
		func(m *messenger.Message) { firstArrived <- struct{}{} },
	)
	defer sub1.Unsubscribe()
	if err := msg.Flush(); err != nil {
		t.Fatalf("flush first-light subscription: %v", err)
	}

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
	if err := msg.Flush(); err != nil {
		t.Fatalf("flush new-light subscription: %v", err)
	}

	// The new device should appear on the next QueryService.Find tick.
	select {
	case <-newArrived:
		// new device picked up
	case <-time.After(500 * time.Millisecond):
		t.Fatal("newly added ESPHome light was not picked up by the running script")
	}
}
