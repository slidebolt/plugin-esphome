package app

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mycontroller-org/esphome_api/pkg/api"
	espmodel "github.com/mycontroller-org/esphome_api/pkg/types"
	internalmdns "github.com/slidebolt/plugin-esphome/internal/mdns"
	domain "github.com/slidebolt/sb-domain"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	storage "github.com/slidebolt/sb-storage-sdk"
	storageserver "github.com/slidebolt/sb-storage-server"
	"google.golang.org/protobuf/proto"
)

type fakeStore struct {
	mu       sync.RWMutex
	entries  []storage.Entry
	data     map[string]json.RawMessage
	private  map[string]json.RawMessage
	internal map[string]json.RawMessage
}

func (f *fakeStore) Save(v storage.Keyed) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.data == nil {
		f.data = make(map[string]json.RawMessage)
	}
	f.data[v.Key()] = data
	return nil
}
func (f *fakeStore) Get(key storage.Keyed) (json.RawMessage, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.data == nil {
		return nil, nil
	}
	return f.data[key.Key()], nil
}
func (f *fakeStore) Delete(key storage.Keyed) error                 { return nil }
func (f *fakeStore) Query(q storage.Query) ([]storage.Entry, error) { return nil, nil }
func (f *fakeStore) WriteFile(target storage.StorageTarget, key storage.Keyed, data json.RawMessage) error {
	return nil
}
func (f *fakeStore) ReadFile(target storage.StorageTarget, key storage.Keyed) (json.RawMessage, error) {
	return nil, nil
}
func (f *fakeStore) DeleteFile(target storage.StorageTarget, key storage.Keyed) error { return nil }
func (f *fakeStore) SetPrivate(key storage.Keyed, data json.RawMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.private == nil {
		f.private = make(map[string]json.RawMessage)
	}
	f.private[key.Key()] = data
	return nil
}
func (f *fakeStore) GetPrivate(key storage.Keyed) (json.RawMessage, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.private == nil {
		return nil, nil
	}
	return f.private[key.Key()], nil
}
func (f *fakeStore) DeletePrivate(key storage.Keyed) error { return nil }
func (f *fakeStore) SetInternal(key storage.Keyed, data json.RawMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.internal == nil {
		f.internal = make(map[string]json.RawMessage)
	}
	f.internal[key.Key()] = data
	return nil
}
func (f *fakeStore) GetInternal(key storage.Keyed) (json.RawMessage, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.internal == nil {
		return nil, nil
	}
	return f.internal[key.Key()], nil
}
func (f *fakeStore) DeleteInternal(key storage.Keyed) error                   { return nil }
func (f *fakeStore) SetProfile(key storage.Keyed, data json.RawMessage) error { return nil }
func (f *fakeStore) Close()                                                   {}
func (f *fakeStore) Search(pattern string) ([]storage.Entry, error)           { return f.entries, nil }
func (f *fakeStore) SearchFiles(target storage.StorageTarget, pattern string) ([]storage.Entry, error) {
	return f.entries, nil
}

func TestLookupESPKey(t *testing.T) {
	got, ok := lookupESPKey("basement-05-tv-right-corner_270689882")
	if !ok {
		t.Fatal("lookupESPKey should parse ESPHome suffix")
	}
	if got != 270689882 {
		t.Fatalf("lookupESPKey = %d, want %d", got, uint32(270689882))
	}
}

func TestEntityFromMsgLightIncludesPersistentColorCapabilities(t *testing.T) {
	entity, _, ok := EntityFromMsg("tv-light", &api.ListEntitiesLightResponse{
		Key:                 42,
		Name:                "TV Light",
		SupportedColorModes: []api.ColorMode{api.ColorMode_COLOR_MODE_RGB, api.ColorMode_COLOR_MODE_COLOR_TEMPERATURE},
	})
	if !ok {
		t.Fatal("EntityFromMsg should accept ListEntitiesLightResponse")
	}
	wantCommands := []string{"light_turn_on", "light_turn_off", "light_set_brightness", "light_set_color_temp", "light_set_rgb"}
	if got := entity.Commands; len(got) != len(wantCommands) {
		t.Fatalf("commands len = %d, want %d (%v)", len(got), len(wantCommands), got)
	}
	light, ok := entity.State.(domain.Light)
	if !ok {
		t.Fatalf("state type = %T, want domain.Light", entity.State)
	}
	if light.ColorMode != "rgb" {
		t.Fatalf("state.colorMode = %q, want rgb", light.ColorMode)
	}
}

func TestEntityFromMsgLightColorTempOnlyOmitsRGBCommand(t *testing.T) {
	entity, _, ok := EntityFromMsg("edison-light", &api.ListEntitiesLightResponse{
		Key:                 7,
		Name:                "Edison Light",
		SupportedColorModes: []api.ColorMode{api.ColorMode_COLOR_MODE_COLOR_TEMPERATURE},
	})
	if !ok {
		t.Fatal("EntityFromMsg should accept ListEntitiesLightResponse")
	}
	wantCommands := []string{"light_turn_on", "light_turn_off", "light_set_brightness", "light_set_color_temp"}
	if !reflect.DeepEqual(entity.Commands, wantCommands) {
		t.Fatalf("commands = %v, want %v", entity.Commands, wantCommands)
	}
	light, ok := entity.State.(domain.Light)
	if !ok {
		t.Fatalf("state type = %T, want domain.Light", entity.State)
	}
	if light.ColorMode != "color_temp" {
		t.Fatalf("state.colorMode = %q, want color_temp", light.ColorMode)
	}
}

func TestEntityFromMsgCoverPublishesDomainCapabilities(t *testing.T) {
	entity, _, ok := EntityFromMsg("garage", &api.ListEntitiesCoverResponse{
		Key:               99,
		ObjectId:          "garage_door",
		Name:              "Garage Door",
		UniqueId:          "garage-door-unique",
		AssumedState:      true,
		SupportsPosition:  true,
		SupportsTilt:      false,
		DeviceClass:       "garage",
		DisabledByDefault: false,
		Icon:              "mdi:garage",
	})
	if !ok {
		t.Fatal("EntityFromMsg should accept ListEntitiesCoverResponse")
	}
	if !reflect.DeepEqual(entity.Commands, []string{"cover_open", "cover_close", "cover_set_position"}) {
		t.Fatalf("commands = %v", entity.Commands)
	}
	cover, ok := entity.State.(domain.Cover)
	if !ok {
		t.Fatalf("state type = %T, want domain.Cover", entity.State)
	}
	if cover.DeviceClass != "garage" {
		t.Fatalf("cover.DeviceClass = %q, want garage", cover.DeviceClass)
	}
	if len(entity.Meta) != 0 {
		t.Fatalf("public entity meta = %+v, want empty; ESPHome provider data belongs in internal storage", entity.Meta)
	}
}

func TestEntityFromMsgCoverWithoutPositionOmitsSetPosition(t *testing.T) {
	entity, _, ok := EntityFromMsg("awning", &api.ListEntitiesCoverResponse{
		Key:              100,
		Name:             "Awning",
		SupportsPosition: false,
	})
	if !ok {
		t.Fatal("EntityFromMsg should accept ListEntitiesCoverResponse")
	}
	if !reflect.DeepEqual(entity.Commands, []string{"cover_open", "cover_close"}) {
		t.Fatalf("commands = %v, want open/close only", entity.Commands)
	}
}

func TestCoverCloseUsesPositionWhenEntitySupportsPosition(t *testing.T) {
	entity, _, _ := EntityFromMsg("garage", &api.ListEntitiesCoverResponse{
		Key:              99,
		Name:             "Garage Door",
		SupportsPosition: true,
	})
	raw, _ := json.Marshal(entity)
	store := &fakeStore{data: map[string]json.RawMessage{entity.Key(): raw}}
	app := &App{store: store}
	var sent proto.Message
	app.devices.Store("garage", &deviceConn{
		send: func(msg proto.Message) error {
			sent = msg
			return nil
		},
		address: "192.0.2.20:6053",
	})

	app.handleCommandWithTrace(messenger.Address{Plugin: PluginID, DeviceID: "garage", EntityID: entity.ID}, domain.CoverClose{}, "")

	req, ok := sent.(*api.CoverCommandRequest)
	if !ok {
		t.Fatalf("sent = %T, want *api.CoverCommandRequest", sent)
	}
	if !req.HasPosition || req.Position != 0 {
		t.Fatalf("request = %+v, want position close", req)
	}
	if req.HasLegacyCommand {
		t.Fatalf("request unexpectedly used legacy command: %+v", req)
	}
}

func TestCoverCloseFallsBackToLegacyWithoutPositionCommand(t *testing.T) {
	entity, _, _ := EntityFromMsg("awning", &api.ListEntitiesCoverResponse{
		Key:              100,
		Name:             "Awning",
		SupportsPosition: false,
	})
	raw, _ := json.Marshal(entity)
	store := &fakeStore{data: map[string]json.RawMessage{entity.Key(): raw}}
	app := &App{store: store}
	var sent proto.Message
	app.devices.Store("awning", &deviceConn{
		send: func(msg proto.Message) error {
			sent = msg
			return nil
		},
		address: "192.0.2.21:6053",
	})

	app.handleCommandWithTrace(messenger.Address{Plugin: PluginID, DeviceID: "awning", EntityID: entity.ID}, domain.CoverClose{}, "")

	req, ok := sent.(*api.CoverCommandRequest)
	if !ok {
		t.Fatalf("sent = %T, want *api.CoverCommandRequest", sent)
	}
	if !req.HasLegacyCommand || req.LegacyCommand != api.LegacyCoverCommand_LEGACY_COVER_COMMAND_CLOSE {
		t.Fatalf("request = %+v, want legacy close", req)
	}
	if req.HasPosition {
		t.Fatalf("request unexpectedly used position: %+v", req)
	}
}

func TestSaveESPHomeEntityMetadataStoresCoverDetailsInternally(t *testing.T) {
	msg := &api.ListEntitiesCoverResponse{
		Key:               99,
		ObjectId:          "garage_door",
		Name:              "Garage Door",
		UniqueId:          "garage-door-unique",
		AssumedState:      true,
		SupportsPosition:  true,
		SupportsTilt:      false,
		DeviceClass:       "garage",
		DisabledByDefault: true,
		Icon:              "mdi:garage",
	}
	entity, espKey, ok := EntityFromMsg("garage", msg)
	if !ok {
		t.Fatal("EntityFromMsg should accept ListEntitiesCoverResponse")
	}
	store := &fakeStore{}
	app := &App{store: store}

	app.saveESPHomeEntityMetadata(entity, msg, espKey)

	raw, err := store.GetInternal(entity)
	if err != nil {
		t.Fatalf("GetInternal: %v", err)
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatalf("unmarshal internal metadata: %v", err)
	}
	if meta["object_id"] != "garage_door" || meta["unique_id"] != "garage-door-unique" {
		t.Fatalf("unexpected identity metadata: %+v", meta)
	}
	if meta["supports_position"] != true || meta["supports_tilt"] != false {
		t.Fatalf("unexpected capability metadata: %+v", meta)
	}
	if meta["device_class"] != "garage" || meta["disabled_by_default"] != true || meta["icon"] != "mdi:garage" {
		t.Fatalf("unexpected provider metadata: %+v", meta)
	}
	if meta["esp_key"] != float64(99) {
		t.Fatalf("esp_key = %v, want 99", meta["esp_key"])
	}
}

func TestCoverDiscoveryStorageContractKeepsProviderMetadataInternal(t *testing.T) {
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

	resp := &api.ListEntitiesCoverResponse{
		Key:              99,
		ObjectId:         "garage_door",
		Name:             "Garage Door",
		UniqueId:         "garage-door-unique",
		SupportsPosition: true,
		DeviceClass:      "garage",
	}
	entity, espKey, ok := EntityFromMsg("garage", resp)
	if !ok {
		t.Fatal("EntityFromMsg should accept ListEntitiesCoverResponse")
	}
	if err := store.Save(entity); err != nil {
		t.Fatalf("Save: %v", err)
	}
	app := &App{store: store}
	app.saveESPHomeEntityMetadata(entity, resp, espKey)

	raw, err := store.Get(entity)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	var public domain.Entity
	if err := json.Unmarshal(raw, &public); err != nil {
		t.Fatalf("unmarshal public entity: %v", err)
	}
	if len(public.Meta) != 0 {
		t.Fatalf("public Meta = %+v, want empty", public.Meta)
	}
	if !reflect.DeepEqual(public.Commands, []string{"cover_open", "cover_close", "cover_set_position"}) {
		t.Fatalf("commands = %v", public.Commands)
	}
	cover, ok := public.State.(domain.Cover)
	if !ok {
		t.Fatalf("state type = %T, want domain.Cover", public.State)
	}
	if cover.DeviceClass != "garage" {
		t.Fatalf("cover.DeviceClass = %q, want garage", cover.DeviceClass)
	}

	internalRaw, err := store.GetInternal(entity)
	if err != nil {
		t.Fatalf("GetInternal: %v", err)
	}
	var internal map[string]any
	if err := json.Unmarshal(internalRaw, &internal); err != nil {
		t.Fatalf("unmarshal internal metadata: %v", err)
	}
	if internal["supports_position"] != true || internal["object_id"] != "garage_door" {
		t.Fatalf("internal metadata = %+v", internal)
	}
}

func TestLightStateFromResponseColorTempDoesNotBackfillRGB(t *testing.T) {
	light := lightStateFromResponse(&api.LightStateResponse{
		State:            true,
		Brightness:       0.5,
		ColorMode:        api.ColorMode_COLOR_MODE_COLOR_TEMPERATURE,
		Red:              1,
		Green:            0.75,
		Blue:             0.5,
		ColorTemperature: 370,
	})
	if light.ColorMode != "color_temp" {
		t.Fatalf("colorMode = %q, want color_temp", light.ColorMode)
	}
	if len(light.RGB) != 0 || len(light.RGBW) != 0 || len(light.RGBWW) != 0 {
		t.Fatalf("unexpected color payload in %+v", light)
	}
	if light.Temperature != 370 {
		t.Fatalf("temperature = %d, want 370", light.Temperature)
	}
}

func TestApplyStateUpdatePersistsFullLightState(t *testing.T) {
	entity := domain.Entity{
		ID:       "tv-light_42",
		Plugin:   PluginID,
		DeviceID: "tv-light",
		Type:     "light",
		Name:     "TV Light",
		Commands: []string{"light_turn_on", "light_turn_off", "light_set_brightness", "light_set_color_temp", "light_set_rgb"},
		State:    domain.Light{Power: false, ColorMode: "rgb"},
	}
	raw, _ := json.Marshal(entity)
	store := &fakeStore{
		entries: []storage.Entry{{Key: entity.Key(), Data: raw}},
		data:    map[string]json.RawMessage{entity.Key(): raw},
	}
	app := &App{store: store}

	app.applyStateUpdate("tv-light", &api.LightStateResponse{
		Key:              42,
		State:            true,
		Brightness:       0.5,
		ColorMode:        api.ColorMode_COLOR_MODE_RGB,
		Red:              1,
		Green:            0,
		Blue:             0,
		Effect:           "rainbow",
		ColorTemperature: 275,
	})

	gotRaw, _ := store.Get(domain.EntityKey{Plugin: PluginID, DeviceID: "tv-light", ID: "tv-light_42"})
	var got domain.Entity
	if err := json.Unmarshal(gotRaw, &got); err != nil {
		t.Fatalf("unmarshal updated entity: %v", err)
	}
	if len(got.Commands) == 0 {
		t.Fatal("commands were dropped during state update")
	}
	light, ok := got.State.(domain.Light)
	if !ok {
		t.Fatalf("state type = %T, want domain.Light", got.State)
	}
	if !light.Power || light.Brightness != 127 || light.ColorMode != "rgb" {
		t.Fatalf("light state = %+v", light)
	}
	if !reflect.DeepEqual(light.RGB, []int{255, 0, 0}) {
		t.Fatalf("light rgb = %v, want [255 0 0]", light.RGB)
	}
	if light.Temperature != 275 || light.Effect != "rainbow" {
		t.Fatalf("light extras = %+v", light)
	}
}

func TestApplyCoverStateUpdatePreservesDeviceClass(t *testing.T) {
	entity := domain.Entity{
		ID:       "garage_99",
		Plugin:   PluginID,
		DeviceID: "garage",
		Type:     "cover",
		Name:     "Garage Door",
		Commands: []string{"cover_open", "cover_close", "cover_set_position"},
		State:    domain.Cover{Position: 0, DeviceClass: "garage"},
	}
	raw, _ := json.Marshal(entity)
	store := &fakeStore{
		entries: []storage.Entry{{Key: entity.Key(), Data: raw}},
		data:    map[string]json.RawMessage{entity.Key(): raw},
	}
	app := &App{store: store}

	app.applyStateUpdate("garage", &api.CoverStateResponse{
		Key:      99,
		Position: 0.42,
	})

	gotRaw, _ := store.Get(domain.EntityKey{Plugin: PluginID, DeviceID: "garage", ID: "garage_99"})
	var got domain.Entity
	if err := json.Unmarshal(gotRaw, &got); err != nil {
		t.Fatalf("unmarshal updated entity: %v", err)
	}
	cover, ok := got.State.(domain.Cover)
	if !ok {
		t.Fatalf("state type = %T, want domain.Cover", got.State)
	}
	if cover.Position != 42 || cover.DeviceClass != "garage" {
		t.Fatalf("cover state = %+v, want position 42 and deviceClass garage", cover)
	}
}

func TestLookupEntityByESPKeyFromStorage(t *testing.T) {
	e1, _ := json.Marshal(domain.Entity{
		ID:       "basement-05-tv-right-corner_270689882",
		Plugin:   PluginID,
		DeviceID: "basement-05-tv-right-corner",
		Type:     "light",
		Name:     "TV Right Corner",
		State:    domain.Light{Power: true, Brightness: 81},
	})
	e2, _ := json.Marshal(domain.Entity{
		ID:       "basement-05-tv-right-corner_123",
		Plugin:   PluginID,
		DeviceID: "basement-05-tv-right-corner",
		Type:     "sensor",
		Name:     "Other",
		State:    domain.Sensor{Value: 1},
	})
	app := &App{
		store: &fakeStore{
			entries: []storage.Entry{
				{Key: PluginID + ".basement-05-tv-right-corner.basement-05-tv-right-corner_123", Data: e2},
				{Key: PluginID + ".basement-05-tv-right-corner.basement-05-tv-right-corner_270689882", Data: e1},
			},
		},
	}

	info, ok := app.lookupEntityByESPKey("basement-05-tv-right-corner", 270689882)
	if !ok {
		t.Fatal("lookupEntityByESPKey should find persisted entity")
	}
	if info.id != "basement-05-tv-right-corner_270689882" {
		t.Fatalf("lookupEntityByESPKey id = %q", info.id)
	}
}

func TestResolveAPIKeyPersistsFallbackToPrivate(t *testing.T) {
	t.Setenv("ESPHOME_API_KEY", "env-secret")

	store := &fakeStore{}
	app := &App{store: store}
	dev := &internalmdns.Device{
		Name:       "basement-05-tv-right-corner",
		TXTRecords: map[string]string{"api_encryption": "1"},
	}

	got := app.resolveAPIKey(dev)
	if got != "env-secret" {
		t.Fatalf("resolveAPIKey = %q, want env-secret", got)
	}

	raw, ok := store.private["plugin-esphome.basement-05-tv-right-corner"]
	if !ok {
		t.Fatal("expected device private data to be persisted")
	}
	var cfg DeviceConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal device config: %v", err)
	}
	if cfg.APIKey != "env-secret" {
		t.Fatalf("private api key = %q, want env-secret", cfg.APIKey)
	}
}

func TestResolveAPIKeyUsesStoredPrivateConfigWithoutMDNSEncryptionFlag(t *testing.T) {
	store := &fakeStore{
		private: map[string]json.RawMessage{},
	}
	raw, _ := json.Marshal(DeviceConfig{
		APIKey:           "stored-secret",
		LastKnownAddress: "10.0.0.20",
		LastKnownPort:    6053,
	})
	store.private["plugin-esphome.basement-edison"] = raw

	app := &App{store: store}
	got := app.resolveAPIKey(&internalmdns.Device{Name: "basement-edison"})
	if got != "stored-secret" {
		t.Fatalf("resolveAPIKey = %q, want stored-secret", got)
	}
}

func TestRememberDevicePersistsLastKnownEndpoint(t *testing.T) {
	store := &fakeStore{}
	app := &App{store: store}

	app.rememberDevice(&internalmdns.Device{
		Name:      "basement-edison",
		Addresses: []string{"10.0.0.30"},
		Port:      7000,
		TXTRecords: map[string]string{
			"mac": "AA:BB:CC:DD:EE:FF",
		},
	})

	cfg, ok := app.loadDeviceConfig("basement-edison")
	if !ok {
		t.Fatal("expected cached device config to be stored")
	}
	if cfg.LastKnownAddress != "10.0.0.30" {
		t.Fatalf("lastKnownAddress = %q, want 10.0.0.30", cfg.LastKnownAddress)
	}
	if cfg.LastKnownPort != 7000 {
		t.Fatalf("lastKnownPort = %d, want 7000", cfg.LastKnownPort)
	}
	if cfg.MAC != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("mac = %q, want aa:bb:cc:dd:ee:ff", cfg.MAC)
	}
	if cfg.LastSeenAt == "" {
		t.Fatal("expected lastSeenAt to be recorded")
	}
}

func TestVerifyConnectedDeviceRejectsMACMismatch(t *testing.T) {
	store := &fakeStore{}
	app := &App{store: store}
	app.updateDeviceConfig("basement-edison", func(cfg *DeviceConfig) {
		cfg.MAC = "aa:bb:cc:dd:ee:ff"
		cfg.LastKnownAddress = "10.0.0.40"
		cfg.LastKnownPort = 6053
	})

	err := app.verifyConnectedDevice(
		&internalmdns.Device{Name: "basement-edison"},
		&espmodel.DeviceInfo{Name: "basement-edison", MacAddress: "11:22:33:44:55:66"},
	)
	if err == nil {
		t.Fatal("expected identity mismatch error")
	}

	app.forgetDeviceAddress("basement-edison")
	cfg, ok := app.loadDeviceConfig("basement-edison")
	if !ok {
		t.Fatal("expected private config to still exist")
	}
	if cfg.LastKnownAddress != "" || cfg.LastKnownPort != 0 || cfg.LastSeenAt != "" {
		t.Fatalf("expected cached endpoint to be cleared, got %+v", cfg)
	}
	if cfg.MAC != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("expected mac to be retained, got %q", cfg.MAC)
	}
}

func TestVerifyConnectedDeviceAcceptsEquivalentMACFormats(t *testing.T) {
	store := &fakeStore{}
	app := &App{store: store}
	app.updateDeviceConfig("basement-edison", func(cfg *DeviceConfig) {
		cfg.MAC = "7cf666735a3a"
	})

	err := app.verifyConnectedDevice(
		&internalmdns.Device{Name: "basement-edison"},
		&espmodel.DeviceInfo{Name: "basement-edison", MacAddress: "7c:f6:66:73:5a:3a"},
	)
	if err != nil {
		t.Fatalf("expected equivalent MAC formats to match, got %v", err)
	}
}

func TestOnDeviceFoundStartsOneManagedLoopPerDevice(t *testing.T) {
	app := New()

	started := 0
	var managed *managedDevice
	app.startDeviceLoop = func(md *managedDevice) {
		started++
		managed = md
	}

	app.onDeviceFound(&internalmdns.Device{
		Name:      "basement-edison",
		Addresses: []string{"10.0.0.10"},
		Port:      6053,
	})
	app.onDeviceFound(&internalmdns.Device{
		Name:      "basement-edison",
		Addresses: []string{"10.0.0.11"},
		Port:      7000,
	})

	if started != 1 {
		t.Fatalf("managed loop starts = %d, want 1", started)
	}
	if managed == nil {
		t.Fatal("expected managed device to be created")
	}
	got := managed.snapshot()
	if got == nil {
		t.Fatal("expected managed device snapshot")
	}
	if got.GetAddress() != "10.0.0.11" {
		t.Fatalf("managed device address = %q, want 10.0.0.11", got.GetAddress())
	}
	if got.GetAPIPort() != 7000 {
		t.Fatalf("managed device port = %d, want 7000", got.GetAPIPort())
	}
}

func TestHandleCommandSendFailureDropsActiveConnectionAndRequestsReconnect(t *testing.T) {
	app := New()
	md := newManagedDevice(&internalmdns.Device{Name: "basement-edison"})
	app.deviceManagers.Store("basement-edison", md)

	closeCalls := 0
	app.devices.Store("basement-edison", &deviceConn{
		send: func(proto.Message) error { return errors.New("broken pipe") },
		close: func() error {
			closeCalls++
			return nil
		},
		address: "10.0.0.10:6053",
		markDisconnected: func(err error) {
			app.markDeviceDisconnected("basement-edison", err)
		},
	})

	app.handleCommand(
		messenger.Address{Plugin: PluginID, DeviceID: "basement-edison", EntityID: "basement-edison_42"},
		domain.LightSetBrightness{Brightness: 128},
	)

	if _, ok := app.devices.Load("basement-edison"); ok {
		t.Fatal("expected active device connection to be removed after send failure")
	}
	if closeCalls != 1 {
		t.Fatalf("close calls = %d, want 1", closeCalls)
	}

	select {
	case <-md.reconnect:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected reconnect to be requested after send failure")
	}
}

func TestConnectAndRegisterTimesOutHungClientFactory(t *testing.T) {
	store := &fakeStore{}
	app := New()
	app.store = store
	app.connectTimeout = 50 * time.Millisecond

	release := make(chan struct{})
	app.clientFactory = func(address, encKey string, handler func(proto.Message)) (esphomeClient, error) {
		<-release
		return nil, errors.New("unblocked")
	}

	start := time.Now()
	err := app.connectAndRegister(
		context.Background(),
		newManagedDevice(&internalmdns.Device{Name: "switch-basement-track"}),
		&internalmdns.Device{
			Name:       "switch-basement-track",
			Addresses:  []string{"192.0.2.10"},
			Port:       6053,
			TXTRecords: map[string]string{"api_encryption": "1"},
		},
	)
	close(release)

	if err == nil {
		t.Fatal("expected connectAndRegister to time out")
	}
	if got := err.Error(); !strings.Contains(got, "timed out waiting for client handshake") {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("connectAndRegister took %v, want under 500ms", elapsed)
	}
}
