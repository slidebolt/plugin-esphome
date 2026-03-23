package app

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/mycontroller-org/esphome_api/pkg/api"
	internalmdns "github.com/slidebolt/plugin-esphome/internal/mdns"
	domain "github.com/slidebolt/sb-domain"
	storage "github.com/slidebolt/sb-storage-sdk"
)

type fakeStore struct {
	entries []storage.Entry
	data    map[string]json.RawMessage
	private map[string]json.RawMessage
}

func (f *fakeStore) Save(v storage.Keyed) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if f.data == nil {
		f.data = make(map[string]json.RawMessage)
	}
	f.data[v.Key()] = data
	return nil
}
func (f *fakeStore) Get(key storage.Keyed) (json.RawMessage, error) {
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
	if f.private == nil {
		f.private = make(map[string]json.RawMessage)
	}
	f.private[key.Key()] = data
	return nil
}
func (f *fakeStore) GetPrivate(key storage.Keyed) (json.RawMessage, error) {
	if f.private == nil {
		return nil, nil
	}
	return f.private[key.Key()], nil
}
func (f *fakeStore) DeletePrivate(key storage.Keyed) error                     { return nil }
func (f *fakeStore) SetInternal(key storage.Keyed, data json.RawMessage) error { return nil }
func (f *fakeStore) GetInternal(key storage.Keyed) (json.RawMessage, error)    { return nil, nil }
func (f *fakeStore) DeleteInternal(key storage.Keyed) error                    { return nil }
func (f *fakeStore) SetProfile(key storage.Keyed, data json.RawMessage) error  { return nil }
func (f *fakeStore) Close()                                                    {}
func (f *fakeStore) Search(pattern string) ([]storage.Entry, error)            { return f.entries, nil }
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
