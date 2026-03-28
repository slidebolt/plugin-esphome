// plugin-esphome connects to ESPHome devices via mDNS discovery and the
// ESPHome native API. It discovers devices on the LAN, lists their entities,
// stores them in SlideBolt storage, and subscribes to state updates.
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mycontroller-org/esphome_api/pkg/api"
	espclient "github.com/mycontroller-org/esphome_api/pkg/client"
	espmodel "github.com/mycontroller-org/esphome_api/pkg/types"
	internalmdns "github.com/slidebolt/plugin-esphome/internal/mdns"
	contract "github.com/slidebolt/sb-contract"
	domain "github.com/slidebolt/sb-domain"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	storage "github.com/slidebolt/sb-storage-sdk"
	"google.golang.org/protobuf/proto"
)

const PluginID = "plugin-esphome"

type entityConn struct {
	send   func(proto.Message) error
	espKey uint32
}

type entityRef struct {
	id string
}

type deviceConn struct {
	send             func(proto.Message) error
	close            func() error
	markDisconnected func(error)
	address          string // TCP address (host:port) for trace logging
}

type DeviceConfig struct {
	APIKey           string `json:"apiKey,omitempty"`
	LastKnownAddress string `json:"lastKnownAddress,omitempty"`
	LastKnownPort    int    `json:"lastKnownPort,omitempty"`
	LastSeenAt       string `json:"lastSeenAt,omitempty"`
	MAC              string `json:"mac,omitempty"`
}

type esphomeClient interface {
	Send(proto.Message) error
	Ping() error
	DeviceInfo() (*espmodel.DeviceInfo, error)
	Close() error
}

type managedDevice struct {
	mu        sync.RWMutex
	dev       *internalmdns.Device
	reconnect chan struct{}
}

func newManagedDevice(dev *internalmdns.Device) *managedDevice {
	return &managedDevice{
		dev:       cloneDevice(dev),
		reconnect: make(chan struct{}, 1),
	}
}

func (d *managedDevice) update(dev *internalmdns.Device) {
	d.mu.Lock()
	d.dev = cloneDevice(dev)
	d.mu.Unlock()
	d.requestReconnect()
}

func (d *managedDevice) snapshot() *internalmdns.Device {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return cloneDevice(d.dev)
}

func (d *managedDevice) requestReconnect() {
	select {
	case d.reconnect <- struct{}{}:
	default:
	}
}

func cloneDevice(dev *internalmdns.Device) *internalmdns.Device {
	if dev == nil {
		return nil
	}
	clone := *dev
	if len(dev.Addresses) > 0 {
		clone.Addresses = append([]string(nil), dev.Addresses...)
	}
	if len(dev.TXTRecords) > 0 {
		clone.TXTRecords = make(map[string]string, len(dev.TXTRecords))
		for k, v := range dev.TXTRecords {
			clone.TXTRecords[k] = v
		}
	}
	return &clone
}

type App struct {
	msg   messenger.Messenger
	store storage.Storage
	cmds  *messenger.Commands
	subs  []messenger.Subscription

	discovery       *internalmdns.Discovery
	ctx             context.Context
	cancel          context.CancelFunc
	deviceManagers  sync.Map
	devices         sync.Map
	trace           bool
	clientFactory   func(address, encKey string, handler func(proto.Message)) (esphomeClient, error)
	sleep           func(time.Duration)
	startDeviceLoop func(*managedDevice)
}

// tracef logs a message when trace mode is enabled (ESPHOME_TRACE=1).
func (a *App) tracef(format string, args ...any) {
	if a.trace {
		log.Printf("plugin-esphome: [trace] "+format, args...)
	}
}

func New() *App {
	app := &App{}
	app.clientFactory = func(address, encKey string, handler func(proto.Message)) (esphomeClient, error) {
		return espclient.GetClient(PluginID, address, encKey, 10*time.Second, handler)
	}
	app.sleep = time.Sleep
	return app
}

func (a *App) Hello() contract.HelloResponse {
	return contract.HelloResponse{
		ID:              PluginID,
		Kind:            contract.KindPlugin,
		ContractVersion: contract.ContractVersion,
		DependsOn:       []string{"messenger", "storage"},
	}
}

func (a *App) OnStart(deps map[string]json.RawMessage) (json.RawMessage, error) {
	a.trace = os.Getenv("ESPHOME_TRACE") == "1"
	if a.trace {
		log.Printf("plugin-esphome: trace logging enabled")
	}

	msg, err := messenger.Connect(deps)
	if err != nil {
		return nil, fmt.Errorf("connect messenger: %w", err)
	}
	a.msg = msg

	storeClient, err := storage.Connect(deps)
	if err != nil {
		return nil, fmt.Errorf("connect storage: %w", err)
	}
	a.store = storeClient

	a.cmds = messenger.NewCommands(msg, domain.LookupCommand)
	sub, err := a.cmds.Receive(PluginID+".>", a.handleCommand)
	if err != nil {
		return nil, fmt.Errorf("subscribe commands: %w", err)
	}
	a.subs = append(a.subs, sub)

	disc, err := internalmdns.NewDiscovery(internalmdns.WithTimeout(5 * time.Second))
	if err != nil {
		return nil, fmt.Errorf("create mDNS discovery: %w", err)
	}
	a.discovery = disc

	ctx, cancel := context.WithCancel(context.Background())
	a.ctx = ctx
	a.cancel = cancel

	go func() {
		a.bootstrapCachedDevices()
		devices, err := disc.Discover(ctx)
		if err != nil {
			log.Printf("plugin-esphome: initial probe error: %v", err)
		} else {
			log.Printf("plugin-esphome: initial probe found %d device(s)", len(devices))
			for _, dev := range devices {
				a.onDeviceFound(dev)
			}
		}
		disc.Listen(ctx, a.onDeviceFound)
	}()

	log.Println("plugin-esphome: started, listening for devices via mDNS")
	return nil, nil
}

func (a *App) OnShutdown() error {
	if a.cancel != nil {
		a.cancel()
	}
	if a.discovery != nil {
		a.discovery.Stop()
	}
	for _, sub := range a.subs {
		sub.Unsubscribe()
	}
	if a.store != nil {
		a.store.Close()
	}
	if a.msg != nil {
		a.msg.Close()
	}
	return nil
}

func (a *App) OnDeviceFound(dev *internalmdns.Device) {
	a.onDeviceFound(dev)
}

func (a *App) onDeviceFound(dev *internalmdns.Device) {
	a.rememberDevice(dev)
	if existing, ok := a.deviceManagers.Load(dev.Name); ok {
		existing.(*managedDevice).update(dev)
		a.tracef("refreshed discovery for %s addr=%s port=%d", dev.Name, dev.GetAddress(), dev.GetAPIPort())
		return
	}

	md := newManagedDevice(dev)
	actual, loaded := a.deviceManagers.LoadOrStore(dev.Name, md)
	if loaded {
		actual.(*managedDevice).update(dev)
		return
	}

	log.Printf("plugin-esphome: discovered %s - connecting", dev.Name)
	if a.startDeviceLoop != nil {
		a.startDeviceLoop(md)
		return
	}
	go a.runDeviceLoop(a.ctx, md)
}

func (a *App) resolveAPIKey(dev *internalmdns.Device) string {
	devKey := domain.DeviceKey{Plugin: PluginID, ID: dev.Name}
	if raw, err := a.store.GetPrivate(devKey); err == nil && len(raw) > 0 {
		var cfg DeviceConfig
		if json.Unmarshal(raw, &cfg) == nil && strings.TrimSpace(cfg.APIKey) != "" {
			return strings.TrimSpace(cfg.APIKey)
		}
	}
	if !dev.HasAPIKey() {
		return ""
	}
	if key := os.Getenv("ESPHOME_API_KEY"); key != "" {
		a.updateDeviceConfig(dev.Name, func(cfg *DeviceConfig) {
			cfg.APIKey = key
		})
		return key
	}
	return ""
}

func (a *App) bootstrapCachedDevices() {
	entries, err := a.store.Search(PluginID + ".*")
	if err != nil {
		log.Printf("plugin-esphome: bootstrap cached devices search: %v", err)
		return
	}

	seen := make(map[string]struct{})
	for _, entry := range entries {
		var entity domain.Entity
		if err := json.Unmarshal(entry.Data, &entity); err != nil {
			continue
		}
		if entity.DeviceID == "" {
			continue
		}
		if _, ok := seen[entity.DeviceID]; ok {
			continue
		}
		seen[entity.DeviceID] = struct{}{}

		cfg, ok := a.loadDeviceConfig(entity.DeviceID)
		if !ok || strings.TrimSpace(cfg.LastKnownAddress) == "" {
			continue
		}
		port := cfg.LastKnownPort
		if port == 0 {
			port = 6053
		}
		dev := &internalmdns.Device{
			Name:      entity.DeviceID,
			Addresses: []string{strings.TrimSpace(cfg.LastKnownAddress)},
			Port:      port,
		}
		if strings.TrimSpace(cfg.MAC) != "" {
			dev.TXTRecords = map[string]string{"mac": cfg.MAC}
		}
		a.tracef("bootstrap cached device %s addr=%s port=%d", dev.Name, dev.GetAddress(), dev.GetAPIPort())
		a.onDeviceFound(dev)
	}
}

func (a *App) loadDeviceConfig(deviceID string) (DeviceConfig, bool) {
	if a.store == nil {
		return DeviceConfig{}, false
	}
	raw, err := a.store.GetPrivate(domain.DeviceKey{Plugin: PluginID, ID: deviceID})
	if err != nil || len(raw) == 0 {
		return DeviceConfig{}, false
	}
	var cfg DeviceConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return DeviceConfig{}, false
	}
	return cfg, true
}

func (a *App) updateDeviceConfig(deviceID string, mutate func(*DeviceConfig)) {
	if a.store == nil {
		return
	}
	cfg, _ := a.loadDeviceConfig(deviceID)
	mutate(&cfg)
	data, err := json.Marshal(cfg)
	if err != nil {
		log.Printf("plugin-esphome: marshal private config for %s: %v", deviceID, err)
		return
	}
	if err := a.store.SetPrivate(domain.DeviceKey{Plugin: PluginID, ID: deviceID}, data); err != nil {
		log.Printf("plugin-esphome: persist private config for %s: %v", deviceID, err)
	}
}

func (a *App) rememberDevice(dev *internalmdns.Device) {
	if dev == nil || strings.TrimSpace(dev.Name) == "" {
		return
	}
	address := strings.TrimSpace(dev.GetAddress())
	port := dev.GetAPIPort()
	mac := normalizeMAC(dev.ParseMAC())
	a.updateDeviceConfig(dev.Name, func(cfg *DeviceConfig) {
		if address != "" {
			cfg.LastKnownAddress = address
			cfg.LastKnownPort = port
			cfg.LastSeenAt = time.Now().UTC().Format(time.RFC3339Nano)
		}
		if mac != "" {
			cfg.MAC = mac
		}
	})
}

func (a *App) runDeviceLoop(ctx context.Context, md *managedDevice) {
	backoff := time.Second
	for {
		dev := md.snapshot()
		if dev == nil {
			if !a.waitForReconnect(ctx, md, backoff) {
				return
			}
			continue
		}

		if err := a.connectAndRegister(ctx, md, dev); err != nil && ctx.Err() == nil {
			log.Printf("plugin-esphome: device %s disconnected: %v", dev.Name, err)
		}
		if ctx.Err() != nil {
			return
		}
		if !a.waitForReconnect(ctx, md, backoff) {
			return
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (a *App) waitForReconnect(ctx context.Context, md *managedDevice, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-md.reconnect:
		return true
	case <-timer.C:
		return true
	}
}

func (a *App) connectAndRegister(ctx context.Context, md *managedDevice, dev *internalmdns.Device) error {
	address := fmt.Sprintf("%s:%d", dev.GetAddress(), dev.GetAPIPort())
	encKey := a.resolveAPIKey(dev)

	listingDone := make(chan struct{}, 1)
	listingPhase := true
	created := 0

	handler := func(msg proto.Message) {
		if listingPhase {
			switch msg.(type) {
			case *api.ListEntitiesDoneResponse:
				listingPhase = false
				select {
				case listingDone <- struct{}{}:
				default:
				}
			default:
				entity, espKey, ok := EntityFromMsg(dev.Name, msg)
				if ok {
					if err := a.store.Save(entity); err != nil {
						log.Printf("plugin-esphome: save entity from %s: %v", dev.Name, err)
					} else {
						created++
					}
					_ = espKey
				}
			}
		} else {
			a.applyStateUpdate(dev.Name, msg)
		}
	}

	espClient, err := a.clientFactory(address, encKey, handler)
	if err != nil {
		return fmt.Errorf("connect %s (%s): %w", dev.Name, address, err)
	}
	defer espClient.Close()

	if err := espClient.Send(&api.HelloRequest{
		ClientInfo:      "slidebolt-esphome-plugin",
		ApiVersionMajor: 1,
		ApiVersionMinor: 9,
	}); err != nil {
		return fmt.Errorf("hello %s: %w", dev.Name, err)
	}
	a.sleep(200 * time.Millisecond)

	if err := espClient.Send(&api.ConnectRequest{}); err != nil {
		return fmt.Errorf("connect-req %s: %w", dev.Name, err)
	}
	a.sleep(200 * time.Millisecond)

	info, err := espClient.DeviceInfo()
	if err != nil {
		return fmt.Errorf("device-info %s: %w", dev.Name, err)
	}
	if err := a.verifyConnectedDevice(dev, info); err != nil {
		a.forgetDeviceAddress(dev.Name)
		return err
	}
	a.rememberVerifiedDevice(dev, info)

	if err := espClient.Send(&api.ListEntitiesRequest{}); err != nil {
		return fmt.Errorf("list-entities %s: %w", dev.Name, err)
	}

	select {
	case <-listingDone:
	case <-time.After(5 * time.Second):
		return fmt.Errorf("timeout waiting for entity list from %s", dev.Name)
	case <-ctx.Done():
		return nil
	}

	log.Printf("plugin-esphome: %s - registered %d entities", dev.Name, created)

	a.devices.Store(dev.Name, &deviceConn{
		send:    espClient.Send,
		close:   espClient.Close,
		address: address,
		markDisconnected: func(err error) {
			a.markDeviceDisconnected(dev.Name, err)
			md.requestReconnect()
		},
	})
	defer a.devices.Delete(dev.Name)

	if err := espClient.Send(&api.SubscribeStatesRequest{}); err != nil {
		return fmt.Errorf("subscribe-states %s: %w", dev.Name, err)
	}

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ping.C:
			if err := espClient.Ping(); err != nil {
				return fmt.Errorf("ping %s failed: %w", dev.Name, err)
			}
		}
	}
}

func normalizeMAC(mac string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(mac), "-", ":"))
}

func (a *App) verifyConnectedDevice(dev *internalmdns.Device, info *espmodel.DeviceInfo) error {
	expectedName := strings.TrimSpace(dev.Name)
	actualName := strings.TrimSpace(info.Name)
	expectedMAC := normalizeMAC(dev.ParseMAC())
	if cfg, ok := a.loadDeviceConfig(dev.Name); ok && strings.TrimSpace(cfg.MAC) != "" {
		expectedMAC = normalizeMAC(cfg.MAC)
	}
	actualMAC := normalizeMAC(info.MacAddress)

	if expectedMAC != "" && actualMAC != "" && expectedMAC != actualMAC {
		return fmt.Errorf("identity mismatch for %s: expected mac %s, got %s", dev.Name, expectedMAC, actualMAC)
	}
	if expectedName != "" && actualName != "" && expectedName != actualName {
		return fmt.Errorf("identity mismatch for %s: expected name %s, got %s", dev.Name, expectedName, actualName)
	}
	return nil
}

func (a *App) rememberVerifiedDevice(dev *internalmdns.Device, info *espmodel.DeviceInfo) {
	address := strings.TrimSpace(dev.GetAddress())
	port := dev.GetAPIPort()
	actualMAC := normalizeMAC(info.MacAddress)
	a.updateDeviceConfig(dev.Name, func(cfg *DeviceConfig) {
		if address != "" {
			cfg.LastKnownAddress = address
			cfg.LastKnownPort = port
			cfg.LastSeenAt = time.Now().UTC().Format(time.RFC3339Nano)
		}
		if actualMAC != "" {
			cfg.MAC = actualMAC
		}
	})
}

func (a *App) forgetDeviceAddress(deviceID string) {
	a.updateDeviceConfig(deviceID, func(cfg *DeviceConfig) {
		cfg.LastKnownAddress = ""
		cfg.LastKnownPort = 0
		cfg.LastSeenAt = ""
	})
}

func (a *App) markDeviceDisconnected(deviceID string, err error) {
	if v, ok := a.devices.LoadAndDelete(deviceID); ok {
		devConn := v.(*deviceConn)
		a.tracef("mark disconnected device=%s addr=%s err=%v", deviceID, devConn.address, err)
		if devConn.close != nil {
			_ = devConn.close()
		}
	}
	if v, ok := a.deviceManagers.Load(deviceID); ok {
		v.(*managedDevice).requestReconnect()
	}
}

func EntityFromMsg(devName string, msg proto.Message) (domain.Entity, uint32, bool) {
	switch resp := msg.(type) {
	case *api.ListEntitiesBinarySensorResponse:
		return domain.Entity{
			ID: fmt.Sprintf("%s_%d", devName, resp.Key), Plugin: PluginID, DeviceID: devName,
			Type: "binary_sensor", Name: resp.Name,
			State: domain.BinarySensor{On: false, DeviceClass: resp.DeviceClass},
		}, resp.Key, true
	case *api.ListEntitiesSensorResponse:
		return domain.Entity{
			ID: fmt.Sprintf("%s_%d", devName, resp.Key), Plugin: PluginID, DeviceID: devName,
			Type: "sensor", Name: resp.Name,
			State: domain.Sensor{Unit: resp.UnitOfMeasurement, DeviceClass: resp.DeviceClass},
		}, resp.Key, true
	case *api.ListEntitiesSwitchResponse:
		return domain.Entity{
			ID: fmt.Sprintf("%s_%d", devName, resp.Key), Plugin: PluginID, DeviceID: devName,
			Type: "switch", Name: resp.Name,
			Commands: []string{"switch_turn_on", "switch_turn_off", "switch_toggle"},
			State:    domain.Switch{Power: false},
		}, resp.Key, true
	case *api.ListEntitiesLightResponse:
		cmds := []string{"light_turn_on", "light_turn_off", "light_set_brightness"}
		if supportsColorTemperature(resp.SupportedColorModes) {
			cmds = append(cmds, "light_set_color_temp")
		}
		if supportsRGB(resp.SupportedColorModes) {
			cmds = append(cmds, "light_set_rgb")
		}
		state := domain.Light{Power: false}
		for _, mode := range resp.SupportedColorModes {
			if normalized := normalizeColorMode(mode); normalized != "" {
				state.ColorMode = normalized
				break
			}
		}
		return domain.Entity{
			ID: fmt.Sprintf("%s_%d", devName, resp.Key), Plugin: PluginID, DeviceID: devName,
			Type: "light", Name: resp.Name,
			Commands: cmds,
			State:    state,
		}, resp.Key, true
	case *api.ListEntitiesFanResponse:
		return domain.Entity{
			ID: fmt.Sprintf("%s_%d", devName, resp.Key), Plugin: PluginID, DeviceID: devName,
			Type: "fan", Name: resp.Name,
			Commands: []string{"fan_turn_on", "fan_turn_off", "fan_set_speed"},
			State:    domain.Fan{Power: false},
		}, resp.Key, true
	case *api.ListEntitiesClimateResponse:
		return domain.Entity{
			ID: fmt.Sprintf("%s_%d", devName, resp.Key), Plugin: PluginID, DeviceID: devName,
			Type: "climate", Name: resp.Name,
			Commands: []string{"climate_set_mode", "climate_set_temperature"},
			State:    domain.Climate{HVACMode: "off"},
		}, resp.Key, true
	case *api.ListEntitiesCoverResponse:
		return domain.Entity{
			ID: fmt.Sprintf("%s_%d", devName, resp.Key), Plugin: PluginID, DeviceID: devName,
			Type: "cover", Name: resp.Name,
			Commands: []string{"cover_open", "cover_close", "cover_set_position"},
			State:    domain.Cover{Position: 0},
		}, resp.Key, true
	case *api.ListEntitiesLockResponse:
		return domain.Entity{
			ID: fmt.Sprintf("%s_%d", devName, resp.Key), Plugin: PluginID, DeviceID: devName,
			Type: "lock", Name: resp.Name,
			Commands: []string{"lock_lock", "lock_unlock"},
			State:    domain.Lock{Locked: false},
		}, resp.Key, true
	case *api.ListEntitiesButtonResponse:
		return domain.Entity{
			ID: fmt.Sprintf("%s_%d", devName, resp.Key), Plugin: PluginID, DeviceID: devName,
			Type: "button", Name: resp.Name,
			Commands: []string{"button_press"},
			State:    domain.Button{Presses: 0},
		}, resp.Key, true
	case *api.ListEntitiesNumberResponse:
		return domain.Entity{
			ID: fmt.Sprintf("%s_%d", devName, resp.Key), Plugin: PluginID, DeviceID: devName,
			Type: "number", Name: resp.Name,
			Commands: []string{"number_set_value"},
			State:    domain.Number{Value: float64(resp.MinValue), Min: float64(resp.MinValue), Max: float64(resp.MaxValue)},
		}, resp.Key, true
	case *api.ListEntitiesSelectResponse:
		return domain.Entity{
			ID: fmt.Sprintf("%s_%d", devName, resp.Key), Plugin: PluginID, DeviceID: devName,
			Type: "select", Name: resp.Name,
			Commands: []string{"select_option"},
			State:    domain.Select{Options: resp.Options},
		}, resp.Key, true
	case *api.ListEntitiesTextSensorResponse:
		return domain.Entity{
			ID: fmt.Sprintf("%s_%d", devName, resp.Key), Plugin: PluginID, DeviceID: devName,
			Type: "sensor", Name: resp.Name,
			State: domain.Sensor{},
		}, resp.Key, true
	}
	return domain.Entity{}, 0, false
}

func (a *App) applyStateUpdate(devName string, msg proto.Message) {
	var espKey uint32
	var newState any

	switch resp := msg.(type) {
	case *api.BinarySensorStateResponse:
		espKey = resp.Key
		newState = domain.BinarySensor{On: resp.State}
	case *api.SwitchStateResponse:
		espKey = resp.Key
		newState = domain.Switch{Power: resp.State}
	case *api.LightStateResponse:
		espKey = resp.Key
		newState = lightStateFromResponse(resp)
	case *api.FanStateResponse:
		espKey = resp.Key
		newState = domain.Fan{Power: resp.State}
	case *api.CoverStateResponse:
		espKey = resp.Key
		newState = domain.Cover{Position: int(resp.Position * 100)}
	case *api.LockStateResponse:
		espKey = resp.Key
		newState = domain.Lock{Locked: resp.State == api.LockState_LOCK_STATE_LOCKED}
	case *api.SensorStateResponse:
		espKey = resp.Key
		info, ok := a.lookupEntityByESPKey(devName, espKey)
		if !ok {
			return
		}
		a.updateFromStorage(devName, info, func(e *domain.Entity) {
			if s, ok := e.State.(map[string]interface{}); ok {
				unit, _ := s["unit"].(string)
				dc, _ := s["deviceClass"].(string)
				e.State = domain.Sensor{Value: float64(resp.State), Unit: unit, DeviceClass: dc}
			} else {
				e.State = domain.Sensor{Value: float64(resp.State)}
			}
		})
		return
	case *api.NumberStateResponse:
		espKey = resp.Key
		info, ok := a.lookupEntityByESPKey(devName, espKey)
		if !ok {
			return
		}
		a.updateFromStorage(devName, info, func(e *domain.Entity) {
			if s, ok := e.State.(map[string]interface{}); ok {
				min, _ := s["min"].(float64)
				max, _ := s["max"].(float64)
				e.State = domain.Number{Value: float64(resp.State), Min: min, Max: max}
			} else {
				e.State = domain.Number{Value: float64(resp.State)}
			}
		})
		return
	case *api.SelectStateResponse:
		espKey = resp.Key
		info, ok := a.lookupEntityByESPKey(devName, espKey)
		if !ok {
			return
		}
		a.updateFromStorage(devName, info, func(e *domain.Entity) {
			if s, ok := e.State.(map[string]interface{}); ok {
				opts, _ := s["options"].([]interface{})
				strs := make([]string, len(opts))
				for i, o := range opts {
					strs[i], _ = o.(string)
				}
				e.State = domain.Select{Option: resp.State, Options: strs}
			} else {
				e.State = domain.Select{Option: resp.State}
			}
		})
		return
	case *api.TextSensorStateResponse:
		espKey = resp.Key
		newState = domain.Text{Value: resp.State}
	case *api.ClimateStateResponse:
		espKey = resp.Key
		mode := strings.ToLower(strings.TrimPrefix(resp.Mode.String(), "CLIMATE_MODE_"))
		newState = domain.Climate{
			HVACMode:    mode,
			Temperature: float64(resp.TargetTemperature),
		}
	default:
		return
	}

	info, ok := a.lookupEntityByESPKey(devName, espKey)
	if !ok {
		return
	}

	eKey := domain.EntityKey{Plugin: PluginID, DeviceID: devName, ID: info.id}
	raw, err := a.store.Get(eKey)
	if err != nil {
		return
	}
	var entity domain.Entity
	if err := json.Unmarshal(raw, &entity); err != nil {
		return
	}
	entity.State = newState
	a.tracef("state update dev=%s entity=%s msg=%T state=%+v", devName, info.id, msg, newState)
	if err := a.store.Save(entity); err != nil {
		log.Printf("plugin-esphome: update state %s: %v", info.id, err)
	}
}

func normalizeColorMode(mode api.ColorMode) string {
	s := strings.ToLower(strings.TrimPrefix(mode.String(), "COLOR_MODE_"))
	switch s {
	case "", "unknown":
		return ""
	case "color_temperature":
		return "color_temp"
	default:
		return s
	}
}

func supportsColorTemperature(modes []api.ColorMode) bool {
	for _, mode := range modes {
		switch normalizeColorMode(mode) {
		case "color_temp", "cold_warm_white":
			return true
		}
	}
	return false
}

func supportsRGB(modes []api.ColorMode) bool {
	for _, mode := range modes {
		if strings.HasPrefix(normalizeColorMode(mode), "rgb") {
			return true
		}
	}
	return false
}

func scaleTo255(v float32) int {
	n := int(v * 255)
	if n < 0 {
		return 0
	}
	if n > 255 {
		return 255
	}
	return n
}

func scaleTo254(v float32) int {
	n := int(v * 254)
	if n < 0 {
		return 0
	}
	if n > 254 {
		return 254
	}
	return n
}

func lightStateFromResponse(resp *api.LightStateResponse) domain.Light {
	state := domain.Light{
		Power:      resp.State,
		Brightness: scaleTo254(resp.Brightness),
		ColorMode:  normalizeColorMode(resp.ColorMode),
	}

	if mode := state.ColorMode; strings.HasPrefix(mode, "rgb") {
		rgb := []int{scaleTo255(resp.Red), scaleTo255(resp.Green), scaleTo255(resp.Blue)}
		switch mode {
		case "rgbw":
			state.RGBW = append(rgb, scaleTo255(resp.White))
		case "rgbww":
			state.RGBWW = append(rgb, scaleTo255(resp.ColdWhite), scaleTo255(resp.WarmWhite))
		default:
			state.RGB = rgb
		}
	}
	if resp.ColorTemperature > 0 {
		state.Temperature = int(resp.ColorTemperature)
	}
	if resp.White > 0 && len(state.RGBW) == 0 {
		state.White = scaleTo255(resp.White)
	}
	if resp.Effect != "" {
		state.Effect = resp.Effect
	}
	return state
}

func (a *App) updateFromStorage(devName string, info entityRef, mutate func(*domain.Entity)) {
	eKey := domain.EntityKey{Plugin: PluginID, DeviceID: devName, ID: info.id}
	raw, err := a.store.Get(eKey)
	if err != nil {
		return
	}
	var entity domain.Entity
	if err := json.Unmarshal(raw, &entity); err != nil {
		return
	}
	mutate(&entity)
	if err := a.store.Save(entity); err != nil {
		log.Printf("plugin-esphome: update state %s: %v", info.id, err)
	}
}

func lookupESPKey(entityID string) (uint32, bool) {
	i := strings.LastIndexByte(entityID, '_')
	if i <= 0 || i == len(entityID)-1 {
		return 0, false
	}
	n, err := strconv.ParseUint(entityID[i+1:], 10, 32)
	if err != nil {
		return 0, false
	}
	return uint32(n), true
}

func (a *App) lookupEntityByESPKey(devName string, espKey uint32) (entityRef, bool) {
	entries, err := a.store.Search(PluginID + "." + devName + ".*")
	if err != nil {
		return entityRef{}, false
	}
	for _, entry := range entries {
		var entity domain.Entity
		if err := json.Unmarshal(entry.Data, &entity); err != nil {
			continue
		}
		if key, ok := lookupESPKey(entity.ID); ok && key == espKey {
			return entityRef{id: entity.ID}, true
		}
	}
	return entityRef{}, false
}

func (a *App) handleCommand(addr messenger.Address, cmd any) {
	v, ok := a.devices.Load(addr.DeviceID)
	if !ok {
		log.Printf("plugin-esphome: no device connection for %s (device may be offline)", addr.Key())
		if v, ok := a.deviceManagers.Load(addr.DeviceID); ok {
			v.(*managedDevice).requestReconnect()
		}
		return
	}
	devConn := v.(*deviceConn)
	espKey, ok := lookupESPKey(addr.EntityID)
	if !ok {
		log.Printf("plugin-esphome: invalid entity key %s (missing ESPHome suffix)", addr.Key())
		return
	}
	ec := entityConn{send: devConn.send, espKey: espKey}
	a.tracef("command recv key=%s device=%s entity=%s addr=%s cmd=%T payload=%+v", addr.Key(), addr.DeviceID, addr.EntityID, devConn.address, cmd, cmd)

	var req proto.Message
	switch c := cmd.(type) {
	case domain.LightTurnOn:
		req = &api.LightCommandRequest{Key: ec.espKey, HasState: true, State: true}
	case domain.LightTurnOff:
		r := &api.LightCommandRequest{Key: ec.espKey, HasState: true, State: false}
		if c.Transition != nil {
			r.HasTransitionLength = true
			r.TransitionLength = uint32(*c.Transition)
		}
		req = r
	case domain.LightSetBrightness:
		r := &api.LightCommandRequest{
			Key: ec.espKey, HasState: true, State: true,
			HasBrightness: true, Brightness: float32(c.Brightness) / 254.0,
		}
		if c.Transition != nil {
			r.HasTransitionLength = true
			r.TransitionLength = uint32(*c.Transition)
		}
		req = r
	case domain.LightSetColorTemp:
		r := &api.LightCommandRequest{
			Key: ec.espKey, HasState: true, State: true,
			HasColorTemperature: true, ColorTemperature: float32(c.Mireds),
		}
		if c.Brightness > 0 {
			r.HasBrightness = true
			r.Brightness = float32(c.Brightness) / 254.0
		}
		if c.Transition != nil {
			r.HasTransitionLength = true
			r.TransitionLength = uint32(*c.Transition)
		}
		req = r
	case domain.LightSetRGB:
		r := &api.LightCommandRequest{
			Key: ec.espKey, HasState: true, State: true,
			HasRgb: true,
			Red:    float32(c.R) / 255.0,
			Green:  float32(c.G) / 255.0,
			Blue:   float32(c.B) / 255.0,
		}
		if c.Brightness > 0 {
			r.HasBrightness = true
			r.Brightness = float32(c.Brightness) / 254.0
		}
		if c.Transition != nil {
			r.HasTransitionLength = true
			r.TransitionLength = uint32(*c.Transition)
		}
		req = r
	case domain.LightSetRGBW:
		r := &api.LightCommandRequest{
			Key: ec.espKey, HasState: true, State: true,
			HasRgb:   true,
			Red:      float32(c.R) / 255.0,
			Green:    float32(c.G) / 255.0,
			Blue:     float32(c.B) / 255.0,
			HasWhite: true, White: float32(c.W) / 255.0,
		}
		if c.Brightness > 0 {
			r.HasBrightness = true
			r.Brightness = float32(c.Brightness) / 254.0
		}
		if c.Transition != nil {
			r.HasTransitionLength = true
			r.TransitionLength = uint32(*c.Transition)
		}
		req = r
	case domain.LightSetRGBWW:
		r := &api.LightCommandRequest{
			Key: ec.espKey, HasState: true, State: true,
			HasRgb:       true,
			Red:          float32(c.R) / 255.0,
			Green:        float32(c.G) / 255.0,
			Blue:         float32(c.B) / 255.0,
			HasColdWhite: true, ColdWhite: float32(c.CW) / 255.0,
			HasWarmWhite: true, WarmWhite: float32(c.WW) / 255.0,
		}
		if c.Brightness > 0 {
			r.HasBrightness = true
			r.Brightness = float32(c.Brightness) / 254.0
		}
		if c.Transition != nil {
			r.HasTransitionLength = true
			r.TransitionLength = uint32(*c.Transition)
		}
		req = r
	case domain.LightSetWhite:
		r := &api.LightCommandRequest{
			Key: ec.espKey, HasState: true, State: true,
			HasWhite: true, White: float32(c.White) / 254.0,
		}
		if c.Transition != nil {
			r.HasTransitionLength = true
			r.TransitionLength = uint32(*c.Transition)
		}
		req = r
	case domain.LightSetEffect:
		req = &api.LightCommandRequest{
			Key: ec.espKey, HasState: true, State: true,
			HasEffect: true, Effect: c.Effect,
		}
	case domain.LightSetHS:
		r, g, b := hsToRGB(c.Hue, c.Saturation/100.0)
		lr := &api.LightCommandRequest{
			Key: ec.espKey, HasState: true, State: true,
			HasRgb: true, Red: r, Green: g, Blue: b,
		}
		if c.Brightness > 0 {
			lr.HasBrightness = true
			lr.Brightness = float32(c.Brightness) / 254.0
		}
		if c.Transition != nil {
			lr.HasTransitionLength = true
			lr.TransitionLength = uint32(*c.Transition)
		}
		req = lr
	case domain.LightSetXY:
		log.Printf("plugin-esphome: light %s set_xy not supported by ESPHome native API", addr.Key())
		return
	case domain.SwitchTurnOn:
		req = &api.SwitchCommandRequest{Key: ec.espKey, State: true}
	case domain.SwitchTurnOff:
		req = &api.SwitchCommandRequest{Key: ec.espKey, State: false}
	case domain.SwitchToggle:
		log.Printf("plugin-esphome: switch %s toggle (not supported natively, use turn_on/turn_off)", addr.Key())
		return
	case domain.FanTurnOn:
		req = &api.FanCommandRequest{Key: ec.espKey, HasState: true, State: true}
	case domain.FanTurnOff:
		req = &api.FanCommandRequest{Key: ec.espKey, HasState: true, State: false}
	case domain.FanSetSpeed:
		req = &api.FanCommandRequest{
			Key: ec.espKey, HasState: true, State: true,
			HasSpeedLevel: true, SpeedLevel: int32(c.Percentage),
		}
	case domain.CoverOpen:
		// SHIM: TODO: This uses a legacy command format.
		// Investigate if there's a more modern way to send these commands to the ESPHome device.
		req = &api.CoverCommandRequest{
			Key:              ec.espKey,
			HasLegacyCommand: true, LegacyCommand: api.LegacyCoverCommand_LEGACY_COVER_COMMAND_OPEN,
		}
	case domain.CoverClose:
		// SHIM: TODO: This uses a legacy command format.
		req = &api.CoverCommandRequest{
			Key:              ec.espKey,
			HasLegacyCommand: true, LegacyCommand: api.LegacyCoverCommand_LEGACY_COVER_COMMAND_CLOSE,
		}
	case domain.CoverSetPosition:
		req = &api.CoverCommandRequest{
			Key:         ec.espKey,
			HasPosition: true, Position: float32(c.Position) / 100.0,
		}
	case domain.LockLock:
		req = &api.LockCommandRequest{Key: ec.espKey, Command: api.LockCommand_LOCK_LOCK}
	case domain.LockUnlock:
		req = &api.LockCommandRequest{Key: ec.espKey, Command: api.LockCommand_LOCK_UNLOCK}
	case domain.ButtonPress:
		req = &api.ButtonCommandRequest{Key: ec.espKey}
	case domain.NumberSetValue:
		req = &api.NumberCommandRequest{Key: ec.espKey, State: float32(c.Value)}
	case domain.SelectOption:
		req = &api.SelectCommandRequest{Key: ec.espKey, State: c.Option}
	case domain.TextSetValue:
		log.Printf("plugin-esphome: text %s set_value not supported in API v1.3", addr.Key())
		return
	case domain.ClimateSetMode:
		mode := domainModeToESPHome(c.HVACMode)
		req = &api.ClimateCommandRequest{Key: ec.espKey, HasMode: true, Mode: mode}
	case domain.ClimateSetTemperature:
		req = &api.ClimateCommandRequest{
			Key:                  ec.espKey,
			HasTargetTemperature: true, TargetTemperature: float32(c.Temperature),
		}
	default:
		log.Printf("plugin-esphome: unknown command %T for %s", cmd, addr.Key())
		return
	}
	a.tracef("command send key=%s addr=%s cmd=%T req=%+v", addr.Key(), devConn.address, cmd, req)

	if err := ec.send(req); err != nil {
		log.Printf("plugin-esphome: send command %T to %s (%s): %v", cmd, addr.Key(), devConn.address, err)
		if devConn.markDisconnected != nil {
			devConn.markDisconnected(err)
		}
	} else {
		a.tracef("command sent ok key=%s addr=%s cmd=%T", addr.Key(), devConn.address, cmd)
	}
}

func hsToRGB(hue, sat float64) (r, g, b float32) {
	h := hue / 60.0
	i := int(h)
	f := h - float64(i)
	p := 1 - sat
	q := 1 - sat*f
	t := 1 - sat*(1-f)
	var rv, gv, bv float64
	switch i % 6 {
	case 0:
		rv, gv, bv = 1, t, p
	case 1:
		rv, gv, bv = q, 1, p
	case 2:
		rv, gv, bv = p, 1, t
	case 3:
		rv, gv, bv = p, q, 1
	case 4:
		rv, gv, bv = t, p, 1
	case 5:
		rv, gv, bv = 1, p, q
	}
	return float32(rv), float32(gv), float32(bv)
}

func domainModeToESPHome(mode string) api.ClimateMode {
	switch mode {
	case "heat":
		return api.ClimateMode_CLIMATE_MODE_HEAT
	case "cool":
		return api.ClimateMode_CLIMATE_MODE_COOL
	case "auto":
		return api.ClimateMode_CLIMATE_MODE_AUTO
	case "dry":
		return api.ClimateMode_CLIMATE_MODE_DRY
	case "fan_only":
		return api.ClimateMode_CLIMATE_MODE_FAN_ONLY
	case "heat_cool":
		return api.ClimateMode_CLIMATE_MODE_HEAT_COOL
	default:
		return api.ClimateMode_CLIMATE_MODE_OFF
	}
}
