package app

import (
	"context"
	"encoding/json"
	"sort"
	"testing"
	"time"

	domain "github.com/slidebolt/sb-domain"
	logcfg "github.com/slidebolt/sb-logging"
	logging "github.com/slidebolt/sb-logging-sdk"
	logserver "github.com/slidebolt/sb-logging/server"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	storage "github.com/slidebolt/sb-storage-sdk"
	storageserver "github.com/slidebolt/sb-storage-server"
	virtual "github.com/slidebolt/sb-virtual/virtual"
)

type traceBlob struct {
	key  string
	data json.RawMessage
}

func (b traceBlob) Key() string                  { return b.key }
func (b traceBlob) MarshalJSON() ([]byte, error) { return b.data, nil }

func saveTraceEntity(t *testing.T, store storage.Storage, e domain.Entity) {
	t.Helper()
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(traceBlob{key: e.Key(), data: data}); err != nil {
		t.Fatal(err)
	}
	if len(e.Labels) == 0 && len(e.Meta) == 0 {
		return
	}
	profile := map[string]any{}
	if len(e.Labels) > 0 {
		profile["labels"] = e.Labels
	}
	if len(e.Meta) > 0 {
		profile["meta"] = e.Meta
	}
	pd, _ := json.Marshal(profile)
	if err := store.SetProfile(traceBlob{key: e.Key()}, json.RawMessage(pd)); err != nil {
		t.Fatalf("setprofile %s: %v", e.Key(), err)
	}
}

func TestBasementTraceFlowsAcrossVirtualAndESPHome(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatal(err)
	}
	defer msg.Close()

	store, err := storageserver.Mock(msg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	logSvc, err := logserver.New(logcfg.Config{Target: "memory"})
	if err != nil {
		t.Fatalf("log server: %v", err)
	}
	logger := logSvc.Store()

	virtualHandler := virtual.NewHandlerWithLogger(msg, store, logger)
	virtualSub, err := virtualHandler.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer virtualSub.Unsubscribe()

	esphomeApp := NewWithLogger(logger)
	esphomeApp.msg = msg
	esphomeApp.store = store
	esphomeApp.cmds = messenger.NewCommands(msg, domain.LookupCommand)
	esphomeSub, err := esphomeApp.cmds.ReceiveMessage(PluginID+".>", esphomeApp.handleCommandMessage)
	if err != nil {
		t.Fatal(err)
	}
	defer esphomeSub.Unsubscribe()

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

	headers := messenger.Headers{
		messenger.HeaderTraceID:      "trace-basement-e2e-1",
		messenger.HeaderOriginEntity: "plugin-esphome.switch_main_basement.switch_main_basement_3558733165",
		messenger.HeaderOriginAction: "light_turn_off",
	}
	if err := msg.PublishWithHeaders("plugin-automation.group.basement.command.light_turn_off", []byte(`{}`), headers); err != nil {
		t.Fatalf("publish: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	var events []logging.Event
	for time.Now().Before(deadline) {
		events, err = logger.List(context.Background(), logging.ListRequest{TraceID: "trace-basement-e2e-1"})
		if err != nil {
			t.Fatalf("list logs: %v", err)
		}
		if len(events) >= 4 {
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

	for _, event := range events {
		data, _ := json.Marshal(event)
		t.Logf("event: %s", data)
	}

	var sawVirtualCommand bool
	var sawFanoutLight bool
	var sawFanoutControl bool
	var sawESPHomeLight bool
	for _, event := range events {
		switch {
		case event.Source == "sb-virtual" && event.Kind == "command.received" && event.Entity == "plugin-automation.group.basement":
			sawVirtualCommand = true
		case event.Source == "sb-virtual" && event.Kind == "fanout.published" && event.Data["recipient"] == "plugin-esphome.basement-light-1.basement-light-1":
			sawFanoutLight = true
		case event.Source == "sb-virtual" && event.Kind == "fanout.published" && event.Data["recipient"] == "plugin-esphome.switch_main_basement.switch_main_basement_3558733165":
			sawFanoutControl = true
		case event.Source == PluginID && event.Kind == "command.received" && event.Entity == "plugin-esphome.basement-light-1.basement-light-1":
			sawESPHomeLight = true
		}
	}

	if !sawVirtualCommand || !sawFanoutLight || !sawFanoutControl || !sawESPHomeLight {
		t.Fatalf("missing expected trace events: virtual=%v fanoutLight=%v fanoutControl=%v esphomeLight=%v", sawVirtualCommand, sawFanoutLight, sawFanoutControl, sawESPHomeLight)
	}
}
