package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/mycontroller-org/esphome_api/pkg/api"
	domain "github.com/slidebolt/sb-domain"
	logcfg "github.com/slidebolt/sb-logging"
	logging "github.com/slidebolt/sb-logging-sdk"
	logserver "github.com/slidebolt/sb-logging/server"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	storage "github.com/slidebolt/sb-storage-sdk"
	storageserver "github.com/slidebolt/sb-storage-server"
	"google.golang.org/protobuf/proto"
)

func TestHandleCommandWithTraceAppendsCommandLog(t *testing.T) {
	svc, err := logserver.New(logcfg.Config{Target: "memory"})
	if err != nil {
		t.Fatalf("server.New(memory): %v", err)
	}
	logger := svc.Store()
	app := NewWithLogger(logger)
	addr := messenger.Address{
		Plugin:   PluginID,
		DeviceID: "switch_main_basement",
		EntityID: "switch_main_basement_3558733165",
	}

	app.handleCommandWithTrace(addr, domain.LightTurnOn{}, "trace-basement-1")

	events, err := logger.List(context.Background(), logging.ListRequest{
		Kind:    "command.received",
		Entity:  addr.Key(),
		TraceID: "trace-basement-1",
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("List len: got %d want 1", len(events))
	}
	if events[0].Action != "light_turn_on" {
		t.Fatalf("Action: got %q want %q", events[0].Action, "light_turn_on")
	}
	if events[0].TraceID != "trace-basement-1" {
		t.Fatalf("TraceID: got %q want %q", events[0].TraceID, "trace-basement-1")
	}
}

func TestApplyStateUpdatePreservesCommandTrace(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatalf("messenger.Mock: %v", err)
	}
	defer msg.Close()

	store, err := storageserver.Mock(msg)
	if err != nil {
		t.Fatalf("storageserver.Mock: %v", err)
	}
	defer store.Close()

	svc, err := logserver.New(logcfg.Config{Target: "memory"})
	if err != nil {
		t.Fatalf("server.New(memory): %v", err)
	}
	logger := svc.Store()

	saveTraceEntity(t, store, domain.Entity{
		ID:       "basement-light-1_123",
		Plugin:   PluginID,
		DeviceID: "basement-light-1",
		Type:     "light",
		Name:     "Basement Light 1",
		State:    domain.Light{Power: false},
	})

	app := NewWithLogger(logger)
	app.msg = msg
	app.store = store
	app.devices.Store("basement-light-1", &deviceConn{
		send:    func(_ proto.Message) error { return nil },
		address: "test",
	})

	addr := messenger.Address{
		Plugin:   PluginID,
		DeviceID: "basement-light-1",
		EntityID: "basement-light-1_123",
	}
	app.handleCommandWithTrace(addr, domain.LightTurnOn{}, "trace-root-1")
	app.applyStateUpdate("basement-light-1", &api.LightStateResponse{
		Key:        123,
		State:      true,
		Brightness: 1,
	})

	events, err := logger.List(context.Background(), logging.ListRequest{
		Source: PluginID,
		Kind:   "state.updated",
		Entity: "plugin-esphome.basement-light-1.basement-light-1_123",
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("List len: got %d want 1", len(events))
	}
	if events[0].TraceID != "trace-root-1" {
		t.Fatalf("TraceID: got %q want %q", events[0].TraceID, "trace-root-1")
	}
}

func TestApplyBinarySensorStateUpdateLogsStateOn(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatalf("messenger.Mock: %v", err)
	}
	defer msg.Close()

	store, err := storageserver.Mock(msg)
	if err != nil {
		t.Fatalf("storageserver.Mock: %v", err)
	}
	defer store.Close()

	svc, err := logserver.New(logcfg.Config{Target: "memory"})
	if err != nil {
		t.Fatalf("server.New(memory): %v", err)
	}
	logger := svc.Store()

	saveTraceEntity(t, store, domain.Entity{
		ID:       "switch_main_basement_3558733165",
		Plugin:   PluginID,
		DeviceID: "switch_main_basement",
		Type:     "binary_sensor",
		Name:     "Main Switch SingleClickActivated",
		State:    domain.BinarySensor{On: false},
	})

	app := NewWithLogger(logger)
	app.msg = msg
	app.store = store

	app.applyStateUpdate("switch_main_basement", &api.BinarySensorStateResponse{
		Key:   3558733165,
		State: true,
	})

	events, err := logger.List(context.Background(), logging.ListRequest{
		Source: PluginID,
		Kind:   "state.updated",
		Entity: "plugin-esphome.switch_main_basement.switch_main_basement_3558733165",
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("List len: got %d want 1", len(events))
	}
	if got := events[0].Data["state_on"]; got != true {
		t.Fatalf("state_on: got %v want true", got)
	}
}

func TestHandleCommandNoConnectionLogsDroppedCommandAndStatus(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatalf("messenger.Mock: %v", err)
	}
	defer msg.Close()

	store, err := storageserver.Mock(msg)
	if err != nil {
		t.Fatalf("storageserver.Mock: %v", err)
	}
	defer store.Close()

	svc, err := logserver.New(logcfg.Config{Target: "memory"})
	if err != nil {
		t.Fatalf("server.New(memory): %v", err)
	}
	logger := svc.Store()

	app := NewWithLogger(logger)
	app.store = store
	addr := messenger.Address{Plugin: PluginID, DeviceID: "bar-light-1", EntityID: "bar-light-1_123"}

	app.handleCommandWithTrace(addr, domain.LightTurnOn{}, "trace-no-conn")

	events, err := logger.List(context.Background(), logging.ListRequest{
		Source:  PluginID,
		Kind:    "device.command.dropped",
		Entity:  addr.Key(),
		TraceID: "trace-no-conn",
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("dropped events len: got %d want 1", len(events))
	}
	if got := events[0].Data["reason"]; got != "no active ESPHome connection" {
		t.Fatalf("reason: got %v", got)
	}

	status := getDeviceStatusEntity(t, store, "bar-light-1")
	if got := status["commandsReceived"]; got != float64(1) {
		t.Fatalf("commandsReceived: got %v want 1", got)
	}
	if got := status["commandsDropped"]; got != float64(1) {
		t.Fatalf("commandsDropped: got %v want 1", got)
	}
}

func TestHandleCommandLogsSentOrFailedDeviceDelivery(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatalf("messenger.Mock: %v", err)
	}
	defer msg.Close()

	store, err := storageserver.Mock(msg)
	if err != nil {
		t.Fatalf("storageserver.Mock: %v", err)
	}
	defer store.Close()

	svc, err := logserver.New(logcfg.Config{Target: "memory"})
	if err != nil {
		t.Fatalf("server.New(memory): %v", err)
	}
	logger := svc.Store()

	app := NewWithLogger(logger)
	app.store = store
	sends := 0
	app.devices.Store("bar-light-1", &deviceConn{
		send: func(_ proto.Message) error {
			sends++
			return nil
		},
		address: "10.0.0.20:6053",
	})
	addr := messenger.Address{Plugin: PluginID, DeviceID: "bar-light-1", EntityID: "bar-light-1_123"}

	app.handleCommandWithTrace(addr, domain.LightSetBrightness{Brightness: 64}, "trace-send-ok")

	if sends != 1 {
		t.Fatalf("sends = %d, want 1", sends)
	}
	events, err := logger.List(context.Background(), logging.ListRequest{
		Source:  PluginID,
		Kind:    "device.command.sent",
		Entity:  addr.Key(),
		TraceID: "trace-send-ok",
	})
	if err != nil {
		t.Fatalf("List sent: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("sent events len: got %d want 1", len(events))
	}
	status := getDeviceStatusEntity(t, store, "bar-light-1")
	if got := status["commandsSent"]; got != float64(1) {
		t.Fatalf("commandsSent: got %v want 1", got)
	}

	app.devices.Store("bar-light-1", &deviceConn{
		send: func(_ proto.Message) error {
			return errors.New("broken pipe")
		},
		close:   func() error { return nil },
		address: "10.0.0.20:6053",
		markDisconnected: func(err error) {
			app.markDeviceDisconnected("bar-light-1", err)
		},
	})
	app.handleCommandWithTrace(addr, domain.LightSetBrightness{Brightness: 65}, "trace-send-fail")

	failed, err := logger.List(context.Background(), logging.ListRequest{
		Source:  PluginID,
		Kind:    "device.command.failed",
		Entity:  addr.Key(),
		TraceID: "trace-send-fail",
	})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(failed) != 1 {
		t.Fatalf("failed events len: got %d want 1", len(failed))
	}
	closed, err := logger.List(context.Background(), logging.ListRequest{
		Source: PluginID,
		Kind:   "device.connection.closed",
		Device: "bar-light-1",
	})
	if err != nil {
		t.Fatalf("List closed: %v", err)
	}
	if len(closed) != 1 {
		t.Fatalf("closed events len: got %d want 1", len(closed))
	}
	status = getDeviceStatusEntity(t, store, "bar-light-1")
	if got := status["commandsFailed"]; got != float64(1) {
		t.Fatalf("commandsFailed: got %v want 1", got)
	}
	if got := status["connected"]; got != false {
		t.Fatalf("connected: got %v want false", got)
	}
}

func TestDeviceConnectionLifecycleLogsAndStatus(t *testing.T) {
	msg, err := messenger.Mock()
	if err != nil {
		t.Fatalf("messenger.Mock: %v", err)
	}
	defer msg.Close()

	store, err := storageserver.Mock(msg)
	if err != nil {
		t.Fatalf("storageserver.Mock: %v", err)
	}
	defer store.Close()

	svc, err := logserver.New(logcfg.Config{Target: "memory"})
	if err != nil {
		t.Fatalf("server.New(memory): %v", err)
	}
	logger := svc.Store()

	app := NewWithLogger(logger)
	app.store = store
	app.recordDeviceConnectionOpened("bar-light-1", "10.0.0.20:6053", 4)
	app.recordDeviceConnectionClosed("bar-light-1", "10.0.0.20:6053", errors.New("ping failed"))

	opened, err := logger.List(context.Background(), logging.ListRequest{
		Source: PluginID,
		Kind:   "device.connection.opened",
		Device: "bar-light-1",
	})
	if err != nil {
		t.Fatalf("List opened: %v", err)
	}
	if len(opened) != 1 {
		t.Fatalf("opened events len: got %d want 1", len(opened))
	}
	closed, err := logger.List(context.Background(), logging.ListRequest{
		Source: PluginID,
		Kind:   "device.connection.closed",
		Device: "bar-light-1",
	})
	if err != nil {
		t.Fatalf("List closed: %v", err)
	}
	if len(closed) != 1 {
		t.Fatalf("closed events len: got %d want 1", len(closed))
	}

	status := getDeviceStatusEntity(t, store, "bar-light-1")
	if got := status["connected"]; got != false {
		t.Fatalf("connected: got %v want false", got)
	}
	if got := status["lastError"]; got != "ping failed" {
		t.Fatalf("lastError: got %v want ping failed", got)
	}
}

func getDeviceStatusEntity(t *testing.T, store storage.Storage, deviceID string) map[string]any {
	t.Helper()
	raw, err := store.Get(domain.EntityKey{Plugin: PluginID, DeviceID: deviceID, ID: "connection"})
	if err != nil {
		t.Fatalf("get status entity: %v", err)
	}
	var entity domain.Entity
	if err := json.Unmarshal(raw, &entity); err != nil {
		t.Fatalf("unmarshal status entity: %v", err)
	}
	state, ok := entity.State.(map[string]any)
	if !ok {
		t.Fatalf("status state type = %T, want map[string]any", entity.State)
	}
	return state
}
