package app

import (
	"context"
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/mycontroller-org/esphome_api/pkg/api"
	domain "github.com/slidebolt/sb-domain"
	logcfg "github.com/slidebolt/sb-logging"
	logging "github.com/slidebolt/sb-logging-sdk"
	logserver "github.com/slidebolt/sb-logging/server"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	scriptserver "github.com/slidebolt/sb-script/server"
	storage "github.com/slidebolt/sb-storage-sdk"
	storageserver "github.com/slidebolt/sb-storage-server"
	virtual "github.com/slidebolt/sb-virtual/virtual"
)

type basementFixture struct {
	t          *testing.T
	msg        messenger.Messenger
	store      storage.Storage
	logger     logging.Store
	scriptSvc  *scriptserver.Service
	virtualSub messenger.Subscription
	esphomeApp *App
	esphomeSub messenger.Subscription
}

func saveScriptDefinition(t *testing.T, store storage.Storage, name, source, defType string) {
	t.Helper()
	body, err := json.Marshal(map[string]string{
		"type":   defType,
		"name":   name,
		"source": source,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(traceBlob{key: "sb-script.scripts." + name, data: body}); err != nil {
		t.Fatal(err)
	}
}

func newBasementFixture(t *testing.T) *basementFixture {
	t.Helper()

	msg, err := messenger.Mock()
	if err != nil {
		t.Fatal(err)
	}

	store, err := storageserver.Mock(msg)
	if err != nil {
		t.Fatal(err)
	}

	logSvc, err := logserver.New(logcfg.Config{Target: "memory"})
	if err != nil {
		t.Fatalf("log server: %v", err)
	}
	logger := logSvc.Store()

	saveTraceEntity(t, store, domain.Entity{
		ID:       "switch_main_basement_3558733165",
		Plugin:   PluginID,
		DeviceID: "switch_main_basement",
		Type:     "binary_sensor",
		Name:     "Main Switch SingleClickActivated",
		Labels: map[string][]string{
			"PluginAutomation": {"Basement"},
		},
		State: domain.BinarySensor{On: false},
	})
	saveTraceEntity(t, store, domain.Entity{
		ID:       "basement-light-1",
		Plugin:   PluginID,
		DeviceID: "basement-light-1",
		Type:     "light",
		Name:     "Basement Light 1",
		Labels: map[string][]string{
			"PluginAutomation": {"Basement"},
		},
		State: domain.Light{Power: false},
	})
	saveTraceEntity(t, store, domain.Entity{
		ID:       "basement",
		Plugin:   "plugin-automation",
		DeviceID: "group",
		Type:     "light",
		Name:     "Basement",
		Target:   json.RawMessage(`{"pattern":"","where":[{"field":"labels.PluginAutomation","op":"eq","value":"Basement"}]}`),
		State:    domain.Light{Power: false},
	})
	if err := storage.SaveQueryDefinition(store, "basement_lights", storage.Query{
		Pattern: "",
		Where: []storage.Filter{
			{Field: "labels.PluginAutomation", Op: storage.Eq, Value: "Basement"},
			{Field: "type", Op: storage.Eq, Value: "light"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	saveScriptDefinition(t, store, "MainBasementSwitch", `Automation("MainBasementSwitch", {
  trigger = Entity("plugin-esphome.switch_main_basement.switch_main_basement_3558733165"),
  targets = Entity("plugin-automation.group.basement")
}, function(ctx)
  local trigger_on = ctx.trigger and ctx.trigger.entity and ctx.trigger.entity.state and ctx.trigger.entity.state.on
  if not trigger_on then
    ctx.decision("ignored_falling_edge", {trigger_on = false})
    return
  end

  local any_on = false
  ctx.targets:each(function(e)
    if e.state and e.state.power then
      any_on = true
    end
  end)

  if any_on then
    ctx.decision("branch_turn_off", {any_on = true})
    ctx.targets:each(function(e)
      ctx.send(e, "light_turn_off", {})
    end)
  else
    ctx.decision("branch_turn_on", {any_on = false})
    ctx.targets:each(function(e)
      ctx.send(e, "light_turn_on", {})
      ctx.send(e, "light_set_brightness", {brightness = 102})
      ctx.send(e, "light_set_color_temp", {mireds = 370})
    end)
  end

  ctx.scripts:stopAll("basement_lights")
end)`, "automation")

	scriptSvc, err := scriptserver.NewWithLogger(msg, store, logger)
	if err != nil {
		t.Fatalf("script server: %v", err)
	}

	virtualHandler := virtual.NewHandlerWithLogger(msg, store, logger)
	virtualSub, err := virtualHandler.Subscribe()
	if err != nil {
		t.Fatal(err)
	}

	esphomeApp := NewWithLogger(logger)
	esphomeApp.msg = msg
	esphomeApp.store = store
	esphomeApp.cmds = messenger.NewCommands(msg, domain.LookupCommand)
	esphomeSub, err := esphomeApp.cmds.ReceiveMessage(PluginID+".>", esphomeApp.handleCommandMessage)
	if err != nil {
		t.Fatal(err)
	}

	f := &basementFixture{
		t:          t,
		msg:        msg,
		store:      store,
		logger:     logger,
		scriptSvc:  scriptSvc,
		virtualSub: virtualSub,
		esphomeApp: esphomeApp,
		esphomeSub: esphomeSub,
	}
	t.Cleanup(func() {
		if f.esphomeSub != nil {
			f.esphomeSub.Unsubscribe()
		}
		if f.virtualSub != nil {
			f.virtualSub.Unsubscribe()
		}
		if f.scriptSvc != nil {
			f.scriptSvc.Shutdown()
		}
		if f.store != nil {
			f.store.Close()
		}
		if f.msg != nil {
			f.msg.Close()
		}
	})
	return f
}

func (f *basementFixture) pressMainSwitch(state bool) {
	f.t.Helper()
	f.esphomeApp.applyStateUpdate("switch_main_basement", &api.BinarySensorStateResponse{
		Key:   3558733165,
		State: state,
	})
}

func (f *basementFixture) waitForEvent(traceID, source, kind string) logging.Event {
	f.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		events, err := f.logger.List(context.Background(), logging.ListRequest{TraceID: traceID, Source: source, Kind: kind})
		if err != nil {
			f.t.Fatal(err)
		}
		if len(events) > 0 {
			return events[len(events)-1]
		}
		time.Sleep(10 * time.Millisecond)
	}
	f.t.Fatalf("expected %s/%s event on trace %s", source, kind, traceID)
	return logging.Event{}
}

func (f *basementFixture) waitForTrace() string {
	f.t.Helper()
	return f.waitForTraceCount(1)
}

func (f *basementFixture) waitForTraceCount(count int) string {
	f.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		events, err := f.logger.List(context.Background(), logging.ListRequest{
			Source: "plugin-esphome",
			Kind:   "state.updated",
			Entity: "plugin-esphome.switch_main_basement.switch_main_basement_3558733165",
		})
		if err != nil {
			f.t.Fatal(err)
		}
		if len(events) >= count {
			return events[count-1].TraceID
		}
		time.Sleep(10 * time.Millisecond)
	}
	f.t.Fatalf("expected %d root state.updated traces", count)
	return ""
}

func (f *basementFixture) waitForTraceEvents(traceID string, minCount int) []logging.Event {
	f.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var events []logging.Event
	var err error
	for time.Now().Before(deadline) {
		events, err = f.logger.List(context.Background(), logging.ListRequest{TraceID: traceID})
		if err != nil {
			f.t.Fatal(err)
		}
		if len(events) >= minCount {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].TS.Equal(events[j].TS) {
			return events[i].ID < events[j].ID
		}
		return events[i].TS.Before(events[j].TS)
	})
	return events
}

func TestBasementAutomationTraceFlowsAcrossPlugins(t *testing.T) {
	f := newBasementFixture(t)

	f.pressMainSwitch(true)
	traceID := f.waitForTrace()
	events := f.waitForTraceEvents(traceID, 8)
	for _, event := range events {
		data, _ := json.Marshal(event)
		t.Logf("event: %s", data)
	}

	var sawStateUpdated bool
	var sawScriptTrigger bool
	var sawScriptTargets bool
	var sawScriptCommand bool
	var sawStopAll bool
	var sawVirtualFanout bool
	var sawESPHomeCommand bool
	for _, event := range events {
		switch {
		case event.Source == PluginID && event.Kind == "state.updated":
			sawStateUpdated = true
		case event.Source == "sb-script" && event.Kind == "automation.triggered":
			sawScriptTrigger = true
		case event.Source == "sb-script" && event.Kind == "automation.targets.resolved":
			sawScriptTargets = true
		case event.Source == "sb-script" && event.Kind == "automation.command.published":
			sawScriptCommand = true
		case event.Source == "sb-script" && event.Kind == "script.control.stop_all":
			sawStopAll = true
		case event.Source == "sb-virtual" && event.Kind == "fanout.published":
			sawVirtualFanout = true
		case event.Source == PluginID && event.Kind == "command.received":
			sawESPHomeCommand = true
		}
	}
	if !sawStateUpdated || !sawScriptTrigger || !sawScriptTargets || !sawScriptCommand || !sawStopAll || !sawVirtualFanout || !sawESPHomeCommand {
		t.Fatalf("missing expected events trace=%s state=%v trigger=%v targets=%v cmd=%v stopAll=%v fanout=%v esphome=%v", traceID, sawStateUpdated, sawScriptTrigger, sawScriptTargets, sawScriptCommand, sawStopAll, sawVirtualFanout, sawESPHomeCommand)
	}
}

func TestBasementAutomationTraceContract(t *testing.T) {
	f := newBasementFixture(t)

	f.pressMainSwitch(true)
	traceID := f.waitForTrace()
	events := f.waitForTraceEvents(traceID, 8)

	required := map[string]bool{
		"plugin-esphome/state.updated":           false,
		"sb-script/automation.triggered":         false,
		"sb-script/automation.targets.resolved":  false,
		"sb-script/automation.command.published": false,
		"sb-script/script.control.stop_all":      false,
		"sb-virtual/fanout.published":            false,
		"plugin-esphome/command.received":        false,
	}
	for _, event := range events {
		key := event.Source + "/" + event.Kind
		if _, ok := required[key]; ok {
			required[key] = true
		}
		if event.TraceID != traceID {
			t.Fatalf("event %s/%s escaped trace: got %q want %q", event.Source, event.Kind, event.TraceID, traceID)
		}
	}
	for key, ok := range required {
		if !ok {
			t.Fatalf("missing required trace stage %s on trace %s", key, traceID)
		}
	}
}

func TestBasementAutomationMomentaryPulseContract(t *testing.T) {
	f := newBasementFixture(t)

	f.pressMainSwitch(true)
	pressTraceID := f.waitForTrace()
	f.waitForEvent(pressTraceID, "sb-script", "automation.command.published")

	f.pressMainSwitch(false)
	releaseTraceID := f.waitForTraceCount(2)

	pressEvents := f.waitForTraceEvents(pressTraceID, 10)
	releaseEvents := f.waitForTraceEvents(releaseTraceID, 4)

	var branchTurnOn bool
	commandPublishes := 0
	for _, event := range pressEvents {
		if event.Source == "sb-script" && event.Kind == "automation.command.published" {
			commandPublishes++
		}
		if event.Source == "sb-script" && event.Kind == "automation.decision" {
			if event.Data["label"] == "branch_turn_on" {
				branchTurnOn = true
			}
		}
	}

	if !branchTurnOn {
		t.Fatalf("missing branch_turn_on decision on trace %s", pressTraceID)
	}
	if commandPublishes != 3 {
		t.Fatalf("unexpected command publish count on trace %s: got %d want 3", pressTraceID, commandPublishes)
	}

	var ignoredFallingEdge bool
	releaseCommands := 0
	for _, event := range releaseEvents {
		if event.Source == "sb-script" && event.Kind == "automation.command.published" {
			releaseCommands++
		}
		if event.Source == "sb-script" && event.Kind == "automation.decision" && event.Data["label"] == "ignored_falling_edge" {
			ignoredFallingEdge = true
		}
	}

	if !ignoredFallingEdge {
		t.Fatalf("missing ignored_falling_edge decision on trace %s", releaseTraceID)
	}
	if releaseCommands != 0 {
		t.Fatalf("unexpected command publishes on release trace %s: got %d want 0", releaseTraceID, releaseCommands)
	}
}
