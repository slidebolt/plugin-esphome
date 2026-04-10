//go:build bdd

package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cucumber/godog"
	"github.com/grandcat/zeroconf"
	mdns "github.com/slidebolt/plugin-esphome/internal/mdns"
	translate "github.com/slidebolt/plugin-esphome/internal/translate"
	domain "github.com/slidebolt/sb-domain"
	testkit "github.com/slidebolt/sb-testkit"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	storage "github.com/slidebolt/sb-storage-sdk"
)

// ---------------------------------------------------------------------------
// Scenario context — one per scenario, reset in BeforeScenario
// ---------------------------------------------------------------------------

type bddCtx struct {
	t     *testing.T
	env   *testkit.TestEnv
	store storage.Storage
	cmds  *messenger.Commands

	lastEntity       domain.Entity
	lastGetErr       error
	lastEntries      []storage.Entry
	lastInternalData json.RawMessage
	lastWirePayload  json.RawMessage
	cmdReceived      chan string // receives ActionName of next command
	cmdSub           messenger.Subscription

	// mDNS Discovery fields
	discovery         *mdns.Discovery
	discoveredDevices []*mdns.Device
	lastDevice        *mdns.Device
	mockEntries       []*zeroconf.ServiceEntry
	discoveryRunning  bool
}

func newBDDCtx(t *testing.T) *bddCtx {
	t.Helper()
	env := testkit.NewTestEnv(t)
	env.Start("messenger")
	env.Start("storage")
	c := &bddCtx{
		t:           t,
		env:         env,
		store:       env.Storage(),
		cmds:        messenger.NewCommands(env.Messenger(), domain.LookupCommand),
		cmdReceived: make(chan string, 1),
	}
	return c
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// parseKey splits "plugin.device.entity" into its three parts.
func parseKey(key string) (plugin, device, id string, err error) {
	parts := strings.SplitN(key, ".", 3)
	if len(parts) != 3 {
		return "", "", "", fmt.Errorf("key %q must have 3 dot-separated segments", key)
	}
	return parts[0], parts[1], parts[2], nil
}

func (c *bddCtx) saveEntity(e domain.Entity) error {
	return c.store.Save(e)
}

func (c *bddCtx) getEntity(key string) (domain.Entity, error) {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return domain.Entity{}, err
	}
	raw, err := c.store.Get(domain.EntityKey{Plugin: plug, DeviceID: dev, ID: id})
	if err != nil {
		return domain.Entity{}, err
	}
	var e domain.Entity
	return e, json.Unmarshal(raw, &e)
}

// ---------------------------------------------------------------------------
// Entity creation steps
// ---------------------------------------------------------------------------

// Light

func (c *bddCtx) aLightEntity(key, name, powerStr string) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	return c.saveEntity(domain.Entity{
		ID: id, Plugin: plug, DeviceID: dev,
		Type: "light", Name: name,
		State: domain.Light{Power: powerStr == "on"},
	})
}

func (c *bddCtx) aLightEntityFull(key, name, powerStr string, brightness, temperature int) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	return c.saveEntity(domain.Entity{
		ID: id, Plugin: plug, DeviceID: dev,
		Type: "light", Name: name,
		State: domain.Light{Power: powerStr == "on", Brightness: brightness, Temperature: temperature},
	})
}

// Switch

func (c *bddCtx) aSwitchEntity(key, name, powerStr string) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	return c.saveEntity(domain.Entity{
		ID: id, Plugin: plug, DeviceID: dev,
		Type: "switch", Name: name,
		State: domain.Switch{Power: powerStr == "on"},
	})
}

// Cover

func (c *bddCtx) aCoverEntity(key, name string, position int) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	return c.saveEntity(domain.Entity{
		ID: id, Plugin: plug, DeviceID: dev,
		Type: "cover", Name: name,
		State: domain.Cover{Position: position},
	})
}

// Lock

func (c *bddCtx) aLockEntity(key, name string, lockedStr string) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	return c.saveEntity(domain.Entity{
		ID: id, Plugin: plug, DeviceID: dev,
		Type: "lock", Name: name,
		State: domain.Lock{Locked: lockedStr == "true"},
	})
}

// Fan

func (c *bddCtx) aFanEntity(key, name, powerStr string, percentage int) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	return c.saveEntity(domain.Entity{
		ID: id, Plugin: plug, DeviceID: dev,
		Type: "fan", Name: name,
		State: domain.Fan{Power: powerStr == "on", Percentage: percentage},
	})
}

// Sensor

func (c *bddCtx) aSensorEntity(key, name, valueStr, unit string) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	val, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		return fmt.Errorf("sensor value %q: %w", valueStr, err)
	}
	return c.saveEntity(domain.Entity{
		ID: id, Plugin: plug, DeviceID: dev,
		Type: "sensor", Name: name,
		State: domain.Sensor{Value: val, Unit: unit},
	})
}

func (c *bddCtx) aSensorEntityWithDeviceClass(key, name, valueStr, unit, deviceClass string) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	val, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		return fmt.Errorf("sensor value %q: %w", valueStr, err)
	}
	return c.saveEntity(domain.Entity{
		ID: id, Plugin: plug, DeviceID: dev,
		Type: "sensor", Name: name,
		State: domain.Sensor{Value: val, Unit: unit, DeviceClass: deviceClass},
	})
}

// BinarySensor

func (c *bddCtx) aBinarySensorEntity(key, name, onStr string) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	return c.saveEntity(domain.Entity{
		ID: id, Plugin: plug, DeviceID: dev,
		Type: "binary_sensor", Name: name,
		State: domain.BinarySensor{On: onStr == "true"},
	})
}

func (c *bddCtx) aBinarySensorEntityWithDeviceClass(key, name, onStr, deviceClass string) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	return c.saveEntity(domain.Entity{
		ID: id, Plugin: plug, DeviceID: dev,
		Type: "binary_sensor", Name: name,
		State: domain.BinarySensor{On: onStr == "true", DeviceClass: deviceClass},
	})
}

// Climate

func (c *bddCtx) aClimateEntity(key, name, hvacMode string, temperature int) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	return c.saveEntity(domain.Entity{
		ID: id, Plugin: plug, DeviceID: dev,
		Type: "climate", Name: name,
		State: domain.Climate{HVACMode: hvacMode, Temperature: float64(temperature)},
	})
}

func (c *bddCtx) aClimateEntityFull(key, name, hvacMode string, temperature int, unit string) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	return c.saveEntity(domain.Entity{
		ID: id, Plugin: plug, DeviceID: dev,
		Type: "climate", Name: name,
		State: domain.Climate{HVACMode: hvacMode, Temperature: float64(temperature), TemperatureUnit: unit},
	})
}

// Button

func (c *bddCtx) aButtonEntity(key, name string, presses int) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	return c.saveEntity(domain.Entity{
		ID: id, Plugin: plug, DeviceID: dev,
		Type: "button", Name: name,
		State: domain.Button{Presses: presses},
	})
}

// Number

func (c *bddCtx) aNumberEntity(key, name string, value, min, max, step float64, unit string) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	return c.saveEntity(domain.Entity{
		ID: id, Plugin: plug, DeviceID: dev,
		Type: "number", Name: name,
		State: domain.Number{Value: value, Min: min, Max: max, Step: step, Unit: unit},
	})
}

// Select

func (c *bddCtx) aSelectEntity(key, name, option, optionsStr string) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	options := strings.Split(optionsStr, ",")
	return c.saveEntity(domain.Entity{
		ID: id, Plugin: plug, DeviceID: dev,
		Type: "select", Name: name,
		State: domain.Select{Option: option, Options: options},
	})
}

// Text

func (c *bddCtx) aTextEntity(key, name, value string) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	return c.saveEntity(domain.Entity{
		ID: id, Plugin: plug, DeviceID: dev,
		Type: "text", Name: name,
		State: domain.Text{Value: value},
	})
}

func (c *bddCtx) aTextEntityFull(key, name, value string, min, max int, pattern, mode string) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	return c.saveEntity(domain.Entity{
		ID: id, Plugin: plug, DeviceID: dev,
		Type: "text", Name: name,
		State: domain.Text{Value: value, Min: min, Max: max, Pattern: pattern, Mode: mode},
	})
}

// ---------------------------------------------------------------------------
// Update steps
// ---------------------------------------------------------------------------

func (c *bddCtx) updateLightToPowerOnBrightness(key string, brightness int) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	return c.saveEntity(domain.Entity{
		ID: id, Plugin: plug, DeviceID: dev,
		Type:  "light",
		State: domain.Light{Power: true, Brightness: brightness},
	})
}

func (c *bddCtx) updateToPowerOn(key string) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	return c.saveEntity(domain.Entity{
		ID: id, Plugin: plug, DeviceID: dev,
		Type:  "light",
		State: domain.Light{Power: true},
	})
}

func (c *bddCtx) updateSwitchToPowerOn(key string) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	return c.saveEntity(domain.Entity{
		ID: id, Plugin: plug, DeviceID: dev,
		Type:  "switch",
		State: domain.Switch{Power: true},
	})
}

func (c *bddCtx) updateCoverToPosition(key string, position int) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	return c.saveEntity(domain.Entity{
		ID: id, Plugin: plug, DeviceID: dev,
		Type:  "cover",
		State: domain.Cover{Position: position},
	})
}

func (c *bddCtx) updateLockToLocked(key string, lockedStr string) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	return c.saveEntity(domain.Entity{
		ID: id, Plugin: plug, DeviceID: dev,
		Type:  "lock",
		State: domain.Lock{Locked: lockedStr == "true"},
	})
}

func (c *bddCtx) updateFanToPowerAndPercentage(key, powerStr string, percentage int) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	return c.saveEntity(domain.Entity{
		ID: id, Plugin: plug, DeviceID: dev,
		Type:  "fan",
		State: domain.Fan{Power: powerStr == "on", Percentage: percentage},
	})
}

func (c *bddCtx) updateSensorValueAndUnit(key, valueStr, unit string) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	val, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		return fmt.Errorf("sensor value %q: %w", valueStr, err)
	}
	return c.saveEntity(domain.Entity{
		ID: id, Plugin: plug, DeviceID: dev,
		Type:  "sensor",
		State: domain.Sensor{Value: val, Unit: unit},
	})
}

func (c *bddCtx) updateClimate(key, hvacMode string, temperature int) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	return c.saveEntity(domain.Entity{
		ID: id, Plugin: plug, DeviceID: dev,
		Type:  "climate",
		State: domain.Climate{HVACMode: hvacMode, Temperature: float64(temperature)},
	})
}

func (c *bddCtx) updateButtonPresses(key string, presses int) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	return c.saveEntity(domain.Entity{
		ID: id, Plugin: plug, DeviceID: dev,
		Type:  "button",
		State: domain.Button{Presses: presses},
	})
}

func (c *bddCtx) updateNumberValue(key string, value float64) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	return c.saveEntity(domain.Entity{
		ID: id, Plugin: plug, DeviceID: dev,
		Type:  "number",
		State: domain.Number{Value: value},
	})
}

func (c *bddCtx) updateSelectOption(key, option string) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	return c.saveEntity(domain.Entity{
		ID: id, Plugin: plug, DeviceID: dev,
		Type:  "select",
		State: domain.Select{Option: option},
	})
}

func (c *bddCtx) updateTextValue(key, value string) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	return c.saveEntity(domain.Entity{
		ID: id, Plugin: plug, DeviceID: dev,
		Type:  "text",
		State: domain.Text{Value: value},
	})
}

// ---------------------------------------------------------------------------
// Entity Lifecycle steps
// ---------------------------------------------------------------------------

func (c *bddCtx) iRetrieve(key string) error {
	e, err := c.getEntity(key)
	c.lastEntity = e
	c.lastGetErr = err
	return nil // don't fail yet — assertion steps check the error
}

func (c *bddCtx) iDelete(key string) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	return c.store.Delete(domain.EntityKey{Plugin: plug, DeviceID: dev, ID: id})
}

func (c *bddCtx) retrievingKeyFails(key string) error {
	_, err := c.getEntity(key)
	if err == nil {
		return fmt.Errorf("expected retrieval of %q to fail, but it succeeded", key)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Assertion steps
// ---------------------------------------------------------------------------

func (c *bddCtx) entityTypeIs(expected string) error {
	if c.lastGetErr != nil {
		return fmt.Errorf("retrieve failed: %w", c.lastGetErr)
	}
	if c.lastEntity.Type != expected {
		return fmt.Errorf("entity type: got %q, want %q", c.lastEntity.Type, expected)
	}
	return nil
}

func (c *bddCtx) entityNameIs(expected string) error {
	if c.lastGetErr != nil {
		return fmt.Errorf("retrieve failed: %w", c.lastGetErr)
	}
	if c.lastEntity.Name != expected {
		return fmt.Errorf("entity name: got %q, want %q", c.lastEntity.Name, expected)
	}
	return nil
}

// Light assertions

func (c *bddCtx) lightPowerIs(expected string) error {
	if c.lastGetErr != nil {
		return fmt.Errorf("retrieve failed: %w", c.lastGetErr)
	}
	st, ok := c.lastEntity.State.(domain.Light)
	if !ok {
		return fmt.Errorf("state type: got %T, want domain.Light", c.lastEntity.State)
	}
	want := expected == "on"
	if st.Power != want {
		return fmt.Errorf("light.Power: got %v, want %v", st.Power, want)
	}
	return nil
}

func (c *bddCtx) lightBrightnessIs(expected int) error {
	if c.lastGetErr != nil {
		return fmt.Errorf("retrieve failed: %w", c.lastGetErr)
	}
	st, ok := c.lastEntity.State.(domain.Light)
	if !ok {
		return fmt.Errorf("state type: got %T, want domain.Light", c.lastEntity.State)
	}
	if st.Brightness != expected {
		return fmt.Errorf("light.Brightness: got %d, want %d", st.Brightness, expected)
	}
	return nil
}

func (c *bddCtx) lightTemperatureIs(expected int) error {
	if c.lastGetErr != nil {
		return fmt.Errorf("retrieve failed: %w", c.lastGetErr)
	}
	st, ok := c.lastEntity.State.(domain.Light)
	if !ok {
		return fmt.Errorf("state type: got %T, want domain.Light", c.lastEntity.State)
	}
	if st.Temperature != expected {
		return fmt.Errorf("light.Temperature: got %v, want %v", st.Temperature, expected)
	}
	return nil
}

// Switch assertions

func (c *bddCtx) switchPowerIs(expected string) error {
	if c.lastGetErr != nil {
		return fmt.Errorf("retrieve failed: %w", c.lastGetErr)
	}
	st, ok := c.lastEntity.State.(domain.Switch)
	if !ok {
		return fmt.Errorf("state type: got %T, want domain.Switch", c.lastEntity.State)
	}
	want := expected == "on"
	if st.Power != want {
		return fmt.Errorf("switch.Power: got %v, want %v", st.Power, want)
	}
	return nil
}

// Cover assertions

func (c *bddCtx) coverPositionIs(expected int) error {
	if c.lastGetErr != nil {
		return fmt.Errorf("retrieve failed: %w", c.lastGetErr)
	}
	st, ok := c.lastEntity.State.(domain.Cover)
	if !ok {
		return fmt.Errorf("state type: got %T, want domain.Cover", c.lastEntity.State)
	}
	if st.Position != expected {
		return fmt.Errorf("cover.Position: got %d, want %d", st.Position, expected)
	}
	return nil
}

// Lock assertions

func (c *bddCtx) lockIs(expected string) error {
	if c.lastGetErr != nil {
		return fmt.Errorf("retrieve failed: %w", c.lastGetErr)
	}
	st, ok := c.lastEntity.State.(domain.Lock)
	if !ok {
		return fmt.Errorf("state type: got %T, want domain.Lock", c.lastEntity.State)
	}
	want := expected == "locked"
	if st.Locked != want {
		return fmt.Errorf("lock.Locked: got %v, want %v", st.Locked, want)
	}
	return nil
}

// Fan assertions

func (c *bddCtx) fanPowerIs(expected string) error {
	if c.lastGetErr != nil {
		return fmt.Errorf("retrieve failed: %w", c.lastGetErr)
	}
	st, ok := c.lastEntity.State.(domain.Fan)
	if !ok {
		return fmt.Errorf("state type: got %T, want domain.Fan", c.lastEntity.State)
	}
	want := expected == "on"
	if st.Power != want {
		return fmt.Errorf("fan.Power: got %v, want %v", st.Power, want)
	}
	return nil
}

func (c *bddCtx) fanPercentageIs(expected int) error {
	if c.lastGetErr != nil {
		return fmt.Errorf("retrieve failed: %w", c.lastGetErr)
	}
	st, ok := c.lastEntity.State.(domain.Fan)
	if !ok {
		return fmt.Errorf("state type: got %T, want domain.Fan", c.lastEntity.State)
	}
	if st.Percentage != expected {
		return fmt.Errorf("fan.Percentage: got %d, want %d", st.Percentage, expected)
	}
	return nil
}

// Sensor assertions

func (c *bddCtx) sensorValueIs(expected string) error {
	if c.lastGetErr != nil {
		return fmt.Errorf("retrieve failed: %w", c.lastGetErr)
	}
	st, ok := c.lastEntity.State.(domain.Sensor)
	if !ok {
		return fmt.Errorf("state type: got %T, want domain.Sensor", c.lastEntity.State)
	}
	got := fmt.Sprintf("%v", st.Value)
	if got != expected {
		return fmt.Errorf("sensor.Value: got %q, want %q", got, expected)
	}
	return nil
}

func (c *bddCtx) sensorUnitIs(expected string) error {
	if c.lastGetErr != nil {
		return fmt.Errorf("retrieve failed: %w", c.lastGetErr)
	}
	st, ok := c.lastEntity.State.(domain.Sensor)
	if !ok {
		return fmt.Errorf("state type: got %T, want domain.Sensor", c.lastEntity.State)
	}
	if st.Unit != expected {
		return fmt.Errorf("sensor.Unit: got %q, want %q", st.Unit, expected)
	}
	return nil
}

func (c *bddCtx) sensorDeviceClassIs(expected string) error {
	if c.lastGetErr != nil {
		return fmt.Errorf("retrieve failed: %w", c.lastGetErr)
	}
	st, ok := c.lastEntity.State.(domain.Sensor)
	if !ok {
		return fmt.Errorf("state type: got %T, want domain.Sensor", c.lastEntity.State)
	}
	if st.DeviceClass != expected {
		return fmt.Errorf("sensor.DeviceClass: got %q, want %q", st.DeviceClass, expected)
	}
	return nil
}

// BinarySensor assertions

func (c *bddCtx) binarySensorIs(expected string) error {
	if c.lastGetErr != nil {
		return fmt.Errorf("retrieve failed: %w", c.lastGetErr)
	}
	st, ok := c.lastEntity.State.(domain.BinarySensor)
	if !ok {
		return fmt.Errorf("state type: got %T, want domain.BinarySensor", c.lastEntity.State)
	}
	want := expected == "on"
	if st.On != want {
		return fmt.Errorf("binary_sensor.On: got %v, want %v", st.On, want)
	}
	return nil
}

func (c *bddCtx) binarySensorDeviceClassIs(expected string) error {
	if c.lastGetErr != nil {
		return fmt.Errorf("retrieve failed: %w", c.lastGetErr)
	}
	st, ok := c.lastEntity.State.(domain.BinarySensor)
	if !ok {
		return fmt.Errorf("state type: got %T, want domain.BinarySensor", c.lastEntity.State)
	}
	if st.DeviceClass != expected {
		return fmt.Errorf("binary_sensor.DeviceClass: got %q, want %q", st.DeviceClass, expected)
	}
	return nil
}

// Climate assertions

func (c *bddCtx) climateHVACModeIs(expected string) error {
	if c.lastGetErr != nil {
		return fmt.Errorf("retrieve failed: %w", c.lastGetErr)
	}
	st, ok := c.lastEntity.State.(domain.Climate)
	if !ok {
		return fmt.Errorf("state type: got %T, want domain.Climate", c.lastEntity.State)
	}
	if st.HVACMode != expected {
		return fmt.Errorf("climate.HVACMode: got %q, want %q", st.HVACMode, expected)
	}
	return nil
}

func (c *bddCtx) climateTemperatureIs(expected int) error {
	if c.lastGetErr != nil {
		return fmt.Errorf("retrieve failed: %w", c.lastGetErr)
	}
	st, ok := c.lastEntity.State.(domain.Climate)
	if !ok {
		return fmt.Errorf("state type: got %T, want domain.Climate", c.lastEntity.State)
	}
	if st.Temperature != float64(expected) {
		return fmt.Errorf("climate.Temperature: got %v, want %v", st.Temperature, expected)
	}
	return nil
}

// Button assertions

func (c *bddCtx) buttonPressesIs(expected int) error {
	if c.lastGetErr != nil {
		return fmt.Errorf("retrieve failed: %w", c.lastGetErr)
	}
	st, ok := c.lastEntity.State.(domain.Button)
	if !ok {
		return fmt.Errorf("state type: got %T, want domain.Button", c.lastEntity.State)
	}
	if st.Presses != expected {
		return fmt.Errorf("button.Presses: got %d, want %d", st.Presses, expected)
	}
	return nil
}

// Number assertions

func (c *bddCtx) numberValueIs(expected float64) error {
	if c.lastGetErr != nil {
		return fmt.Errorf("retrieve failed: %w", c.lastGetErr)
	}
	st, ok := c.lastEntity.State.(domain.Number)
	if !ok {
		return fmt.Errorf("state type: got %T, want domain.Number", c.lastEntity.State)
	}
	if st.Value != expected {
		return fmt.Errorf("number.Value: got %v, want %v", st.Value, expected)
	}
	return nil
}

// Select assertions

func (c *bddCtx) selectOptionIs(expected string) error {
	if c.lastGetErr != nil {
		return fmt.Errorf("retrieve failed: %w", c.lastGetErr)
	}
	st, ok := c.lastEntity.State.(domain.Select)
	if !ok {
		return fmt.Errorf("state type: got %T, want domain.Select", c.lastEntity.State)
	}
	if st.Option != expected {
		return fmt.Errorf("select.Option: got %q, want %q", st.Option, expected)
	}
	return nil
}

// Text assertions

func (c *bddCtx) textValueIs(expected string) error {
	if c.lastGetErr != nil {
		return fmt.Errorf("retrieve failed: %w", c.lastGetErr)
	}
	st, ok := c.lastEntity.State.(domain.Text)
	if !ok {
		return fmt.Errorf("state type: got %T, want domain.Text", c.lastEntity.State)
	}
	if st.Value != expected {
		return fmt.Errorf("text.Value: got %q, want %q", st.Value, expected)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Command Dispatch steps
// ---------------------------------------------------------------------------

func (c *bddCtx) aCommandListenerOn(pattern string) error {
	if c.cmdSub != nil {
		c.cmdSub.Unsubscribe()
	}
	c.cmdReceived = make(chan string, 1)
	sub, err := c.cmds.Receive(pattern, func(_ messenger.Address, cmd any) {
		select {
		case c.cmdReceived <- actionNameOf(cmd):
		default:
		}
	})
	if err != nil {
		return err
	}
	c.cmdSub = sub
	return nil
}

func (c *bddCtx) iSendCommandTo(action, key string) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	target := domain.EntityKey{Plugin: plug, DeviceID: dev, ID: id}
	cmd, err := makeCommand(action)
	if err != nil {
		return err
	}
	return c.cmds.Send(target, cmd)
}

func (c *bddCtx) iSendCoverSetPositionTo(position int, key string) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	target := domain.EntityKey{Plugin: plug, DeviceID: dev, ID: id}
	return c.cmds.Send(target, domain.CoverSetPosition{Position: position})
}

func (c *bddCtx) iSendLightSetBrightnessTo(brightness int, key string) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	target := domain.EntityKey{Plugin: plug, DeviceID: dev, ID: id}
	return c.cmds.Send(target, domain.LightSetBrightness{Brightness: brightness})
}

func (c *bddCtx) iSendLightSetColorTempTo(mireds int, key string) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	target := domain.EntityKey{Plugin: plug, DeviceID: dev, ID: id}
	return c.cmds.Send(target, domain.LightSetColorTemp{Mireds: mireds})
}

func (c *bddCtx) iSendLightSetRGBTo(r, g, b int, key string) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	target := domain.EntityKey{Plugin: plug, DeviceID: dev, ID: id}
	return c.cmds.Send(target, domain.LightSetRGB{R: r, G: g, B: b})
}

func (c *bddCtx) iSendFanSetSpeedTo(percentage int, key string) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	target := domain.EntityKey{Plugin: plug, DeviceID: dev, ID: id}
	return c.cmds.Send(target, domain.FanSetSpeed{Percentage: percentage})
}

func (c *bddCtx) iSendNumberSetValueTo(value float64, key string) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	target := domain.EntityKey{Plugin: plug, DeviceID: dev, ID: id}
	return c.cmds.Send(target, domain.NumberSetValue{Value: value})
}

func (c *bddCtx) iSendSelectOptionTo(option, key string) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	target := domain.EntityKey{Plugin: plug, DeviceID: dev, ID: id}
	return c.cmds.Send(target, domain.SelectOption{Option: option})
}

func (c *bddCtx) iSendTextSetValueTo(value, key string) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	target := domain.EntityKey{Plugin: plug, DeviceID: dev, ID: id}
	return c.cmds.Send(target, domain.TextSetValue{Value: value})
}

func (c *bddCtx) receivedCommandActionIs(expected string) error {
	select {
	case got := <-c.cmdReceived:
		if got != expected {
			return fmt.Errorf("command action: got %q, want %q", got, expected)
		}
		return nil
	case <-time.After(2 * time.Second):
		return fmt.Errorf("timed out waiting for command %q", expected)
	}
}

// actionNameOf extracts the action name from a concrete command value.
func actionNameOf(cmd any) string {
	type namer interface{ ActionName() string }
	if n, ok := cmd.(namer); ok {
		return n.ActionName()
	}
	return fmt.Sprintf("unknown(%T)", cmd)
}

// makeCommand creates a zero-value command for the given action name.
func makeCommand(action string) (messenger.Action, error) {
	switch action {
	case "light_turn_on":
		return domain.LightTurnOn{}, nil
	case "light_turn_off":
		return domain.LightTurnOff{}, nil
	case "light_set_brightness":
		return domain.LightSetBrightness{Brightness: 128}, nil
	case "light_set_color_temp":
		return domain.LightSetColorTemp{Mireds: 370}, nil
	case "light_set_rgb":
		return domain.LightSetRGB{R: 255, G: 128, B: 0}, nil
	case "light_set_rgbw":
		return domain.LightSetRGBW{R: 255, G: 128, B: 0, W: 0}, nil
	case "light_set_rgbww":
		return domain.LightSetRGBWW{R: 255, G: 128, B: 0, CW: 0, WW: 0}, nil
	case "light_set_hs":
		return domain.LightSetHS{Hue: 180, Saturation: 50}, nil
	case "light_set_xy":
		return domain.LightSetXY{X: 0.3, Y: 0.3}, nil
	case "light_set_white":
		return domain.LightSetWhite{White: 128}, nil
	case "light_set_effect":
		return domain.LightSetEffect{Effect: "rainbow"}, nil
	case "switch_turn_on":
		return domain.SwitchTurnOn{}, nil
	case "switch_turn_off":
		return domain.SwitchTurnOff{}, nil
	case "switch_toggle":
		return domain.SwitchToggle{}, nil
	case "fan_turn_on":
		return domain.FanTurnOn{}, nil
	case "fan_turn_off":
		return domain.FanTurnOff{}, nil
	case "fan_set_speed":
		return domain.FanSetSpeed{Percentage: 50}, nil
	case "cover_open":
		return domain.CoverOpen{}, nil
	case "cover_close":
		return domain.CoverClose{}, nil
	case "cover_set_position":
		return domain.CoverSetPosition{Position: 50}, nil
	case "lock_lock":
		return domain.LockLock{}, nil
	case "lock_unlock":
		return domain.LockUnlock{}, nil
	case "button_press":
		return domain.ButtonPress{}, nil
	case "number_set_value":
		return domain.NumberSetValue{Value: 0}, nil
	case "select_option":
		return domain.SelectOption{Option: "option1"}, nil
	case "text_set_value":
		return domain.TextSetValue{Value: "hello"}, nil
	case "climate_set_mode":
		return domain.ClimateSetMode{HVACMode: "cool"}, nil
	case "climate_set_temperature":
		return domain.ClimateSetTemperature{Temperature: 20}, nil
	default:
		return nil, fmt.Errorf("unknown action %q", action)
	}
}

// ---------------------------------------------------------------------------
// Query DSL steps
// ---------------------------------------------------------------------------

func (c *bddCtx) theFollowingEntitiesExist(table *godog.Table) error {
	for i, row := range table.Rows {
		if i == 0 {
			continue // header
		}
		if len(row.Cells) < 2 {
			return fmt.Errorf("row %d: need key and type columns", i)
		}
		key := row.Cells[0].Value
		typ := row.Cells[1].Value

		plug, dev, id, err := parseKey(key)
		if err != nil {
			return err
		}
		e := domain.Entity{
			ID: id, Plugin: plug, DeviceID: dev,
			Type: typ, Name: typ + "-" + id,
		}
		switch typ {
		case "light":
			e.State = domain.Light{}
		case "switch":
			e.State = domain.Switch{}
		case "sensor":
			e.State = domain.Sensor{Value: 0.0}
		case "binary_sensor":
			e.State = domain.BinarySensor{}
		case "lock":
			e.State = domain.Lock{}
		case "fan":
			e.State = domain.Fan{}
		case "cover":
			e.State = domain.Cover{}
		case "button":
			e.State = domain.Button{}
		case "number":
			e.State = domain.Number{}
		case "select":
			e.State = domain.Select{}
		case "text":
			e.State = domain.Text{}
		case "climate":
			e.State = domain.Climate{}
		}
		if err := c.store.Save(e); err != nil {
			return fmt.Errorf("save %s: %w", key, err)
		}
	}
	return nil
}

func (c *bddCtx) iQueryWhereEquals(field, value string) error {
	entries, err := c.store.Query(storage.Query{
		Where: []storage.Filter{{Field: field, Op: storage.Eq, Value: value}},
	})
	if err != nil {
		return err
	}
	c.lastEntries = entries
	return nil
}

func (c *bddCtx) iQueryWhereTwoFilters(field1, value1, field2, value2 string) error {
	filter2Value := parseBoolOrString(value2)
	entries, err := c.store.Query(storage.Query{
		Where: []storage.Filter{
			{Field: field1, Op: storage.Eq, Value: value1},
			{Field: field2, Op: storage.Eq, Value: filter2Value},
		},
	})
	if err != nil {
		return err
	}
	c.lastEntries = entries
	return nil
}

func (c *bddCtx) iQueryWhereGreaterThan(field1, value1, field2 string, threshold float64) error {
	entries, err := c.store.Query(storage.Query{
		Where: []storage.Filter{
			{Field: field1, Op: storage.Eq, Value: value1},
			{Field: field2, Op: storage.Gt, Value: threshold},
		},
	})
	if err != nil {
		return err
	}
	c.lastEntries = entries
	return nil
}

// parseBoolOrString returns a bool if value is "true"/"false", else the string.
func parseBoolOrString(v string) any {
	switch v {
	case "true":
		return true
	case "false":
		return false
	default:
		return v
	}
}

func (c *bddCtx) iSearchWithPattern(pattern string) error {
	entries, err := c.store.Search(pattern)
	if err != nil {
		return err
	}
	c.lastEntries = entries
	return nil
}

func (c *bddCtx) iGetNResults(expected int) error {
	if len(c.lastEntries) != expected {
		return fmt.Errorf("result count: got %d, want %d", len(c.lastEntries), expected)
	}
	return nil
}

func (c *bddCtx) iGet1Result() error {
	return c.iGetNResults(1)
}

func (c *bddCtx) resultsInclude(key string) error {
	for _, e := range c.lastEntries {
		var entity domain.Entity
		if err := json.Unmarshal(e.Data, &entity); err != nil {
			continue
		}
		if entity.Key() == key {
			return nil
		}
	}
	return fmt.Errorf("results do not include %q (got %d results)", key, len(c.lastEntries))
}

func (c *bddCtx) resultsDoNotInclude(key string) error {
	for _, e := range c.lastEntries {
		var entity domain.Entity
		if err := json.Unmarshal(e.Data, &entity); err != nil {
			continue
		}
		if entity.Key() == key {
			return fmt.Errorf("results unexpectedly include %q", key)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Internal storage steps
// ---------------------------------------------------------------------------

func (c *bddCtx) iWriteInternalData(key, payload string) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	k := domain.EntityKey{Plugin: plug, DeviceID: dev, ID: id}
	return c.store.WriteFile(storage.Internal, k, json.RawMessage(payload))
}

func (c *bddCtx) iReadInternalData(key string) error {
	plug, dev, id, err := parseKey(key)
	if err != nil {
		return err
	}
	k := domain.EntityKey{Plugin: plug, DeviceID: dev, ID: id}
	data, err := c.store.ReadFile(storage.Internal, k)
	if err != nil {
		return fmt.Errorf("ReadFile internal %s: %w", key, err)
	}
	c.lastInternalData = data
	return nil
}

func (c *bddCtx) internalDataMatches(expected string) error {
	if string(c.lastInternalData) != expected {
		return fmt.Errorf("internal data: got %s, want %s", c.lastInternalData, expected)
	}
	return nil
}

func (c *bddCtx) queryingTypeReturnsOnlyStateEntities(typ string) error {
	entries, err := c.store.Query(storage.Query{
		Where: []storage.Filter{{Field: "type", Op: storage.Eq, Value: typ}},
	})
	if err != nil {
		return err
	}
	// We just saved 1 entity of this type — internal data must not inflate the count
	if len(entries) != 1 {
		return fmt.Errorf("query type=%s: got %d results, want 1 (internal data must not appear)", typ, len(entries))
	}
	return nil
}

// ---------------------------------------------------------------------------
// Translate: Decode and Encode steps
// ---------------------------------------------------------------------------

// iDecodePayloadAs calls translate.Decode(typeName, raw) and stores the result in
// lastEntity.State so all existing state assertion steps work unchanged.
func (c *bddCtx) iDecodePayloadAs(typeName, rawJSON string) error {
	state, ok := translate.Decode(typeName, json.RawMessage(rawJSON))
	if !ok {
		return fmt.Errorf("translate.Decode(%q, %s) returned false", typeName, rawJSON)
	}
	c.lastEntity.State = state
	c.lastGetErr = nil
	return nil
}

// iEncodeCommandWithJSON looks up the action type, unmarshals jsonPayload into it,
// calls Encode, and stores the result in lastWirePayload.
func (c *bddCtx) iEncodeCommandWithJSON(action, jsonPayload string) error {
	typ, ok := domain.LookupCommand(action)
	if !ok {
		return fmt.Errorf("unknown action %q", action)
	}
	v := reflect.New(typ).Interface()
	if err := json.Unmarshal([]byte(jsonPayload), v); err != nil {
		return fmt.Errorf("unmarshal command %q: %w", action, err)
	}
	cmd := reflect.ValueOf(v).Elem().Interface()
	out, err := translate.Encode(cmd, nil)
	if err != nil {
		return fmt.Errorf("translate.Encode(%q): %w", action, err)
	}
	c.lastWirePayload = out
	return nil
}

// wirePayloadFieldEqualsNum asserts that lastWirePayload[field] == expected number.
func (c *bddCtx) wirePayloadFieldEqualsNum(field string, expected float64) error {
	var m map[string]any
	if err := json.Unmarshal(c.lastWirePayload, &m); err != nil {
		return fmt.Errorf("wire payload is not JSON: %w", err)
	}
	v, ok := m[field]
	if !ok {
		return fmt.Errorf("wire payload missing field %q; got %s", field, c.lastWirePayload)
	}
	got, ok := v.(float64)
	if !ok {
		return fmt.Errorf("wire payload field %q: got %T (%v), want float64", field, v, v)
	}
	if got != expected {
		return fmt.Errorf("wire payload field %q: got %v, want %v", field, got, expected)
	}
	return nil
}

// wirePayloadFieldEqualsString asserts that lastWirePayload[field] == expected string.
func (c *bddCtx) wirePayloadFieldEqualsString(field, expected string) error {
	var m map[string]any
	if err := json.Unmarshal(c.lastWirePayload, &m); err != nil {
		return fmt.Errorf("wire payload is not JSON: %w", err)
	}
	v, ok := m[field]
	if !ok {
		return fmt.Errorf("wire payload missing field %q; got %s", field, c.lastWirePayload)
	}
	got := fmt.Sprintf("%v", v)
	if got != expected {
		return fmt.Errorf("wire payload field %q: got %q, want %q", field, got, expected)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Step registration
// ---------------------------------------------------------------------------

func (c *bddCtx) RegisterSteps(ctx *godog.ScenarioContext) {
	// --- Entity creation ---

	// Light
	ctx.Step(`^a light entity "([^"]*)" named "([^"]*)" with power (on|off)$`, c.aLightEntity)
	ctx.Step(`^a light entity "([^"]*)" named "([^"]*)" with power (on|off) brightness (\d+) temperature (\d+)$`, c.aLightEntityFull)

	// Switch
	ctx.Step(`^a switch entity "([^"]*)" named "([^"]*)" with power (on|off)$`, c.aSwitchEntity)

	// Cover
	ctx.Step(`^a cover entity "([^"]*)" named "([^"]*)" with position (\d+)$`, c.aCoverEntity)

	// Lock
	ctx.Step(`^a lock entity "([^"]*)" named "([^"]*)" with locked (true|false)$`, c.aLockEntity)

	// Fan
	ctx.Step(`^a fan entity "([^"]*)" named "([^"]*)" with power (on|off) and percentage (\d+)$`, c.aFanEntity)

	// Sensor
	ctx.Step(`^a sensor entity "([^"]*)" named "([^"]*)" with value "([^"]*)" and unit "([^"]*)"$`, c.aSensorEntity)
	ctx.Step(`^a sensor entity "([^"]*)" named "([^"]*)" with value "([^"]*)" unit "([^"]*)" and device_class "([^"]*)"$`, c.aSensorEntityWithDeviceClass)

	// BinarySensor
	ctx.Step(`^a binary_sensor entity "([^"]*)" named "([^"]*)" with on (true|false)$`, c.aBinarySensorEntity)
	ctx.Step(`^a binary_sensor entity "([^"]*)" named "([^"]*)" with on (true|false) and device_class "([^"]*)"$`, c.aBinarySensorEntityWithDeviceClass)

	// Climate
	ctx.Step(`^a climate entity "([^"]*)" named "([^"]*)" with hvac_mode "([^"]*)" temperature (\d+)$`, c.aClimateEntity)
	ctx.Step(`^a climate entity "([^"]*)" named "([^"]*)" with hvac_mode "([^"]*)" temperature (\d+) unit "([^"]*)"$`, c.aClimateEntityFull)

	// Button
	ctx.Step(`^a button entity "([^"]*)" named "([^"]*)" with presses (\d+)$`, c.aButtonEntity)

	// Number
	ctx.Step(`^a number entity "([^"]*)" named "([^"]*)" with value ([\d.]+) min ([\d.]+) max ([\d.]+) step ([\d.]+) unit "([^"]*)"$`, c.aNumberEntity)

	// Select
	ctx.Step(`^a select entity "([^"]*)" named "([^"]*)" with option "([^"]*)" and options "([^"]*)"$`, c.aSelectEntity)

	// Text
	ctx.Step(`^a text entity "([^"]*)" named "([^"]*)" with value "([^"]*)"$`, c.aTextEntity)
	ctx.Step(`^a text entity "([^"]*)" named "([^"]*)" with value "([^"]*)" min (\d+) max (\d+) pattern "([^"]*)" mode "([^"]*)"$`, c.aTextEntityFull)

	// --- Update steps ---
	ctx.Step(`^I update "([^"]*)" to power on$`, c.updateToPowerOn)
	ctx.Step(`^I update "([^"]*)" to power on brightness (\d+)$`, c.updateLightToPowerOnBrightness)
	ctx.Step(`^I update switch "([^"]*)" to power on$`, c.updateSwitchToPowerOn)
	ctx.Step(`^I update cover "([^"]*)" to position (\d+)$`, c.updateCoverToPosition)
	ctx.Step(`^I update lock "([^"]*)" to locked (true|false)$`, c.updateLockToLocked)
	ctx.Step(`^I update fan "([^"]*)" to power (on|off) and percentage (\d+)$`, c.updateFanToPowerAndPercentage)
	ctx.Step(`^I update sensor "([^"]*)" to value "([^"]*)" and unit "([^"]*)"$`, c.updateSensorValueAndUnit)
	ctx.Step(`^I update climate "([^"]*)" to hvac_mode "([^"]*)" temperature (\d+)$`, c.updateClimate)
	ctx.Step(`^I update button "([^"]*)" to presses (\d+)$`, c.updateButtonPresses)
	ctx.Step(`^I update number "([^"]*)" to value ([\d.]+)$`, c.updateNumberValue)
	ctx.Step(`^I update select "([^"]*)" to option "([^"]*)"$`, c.updateSelectOption)
	ctx.Step(`^I update text "([^"]*)" to value "([^"]*)"$`, c.updateTextValue)

	// --- Lifecycle ---
	ctx.Step(`^I retrieve "([^"]*)"$`, c.iRetrieve)
	ctx.Step(`^I delete "([^"]*)"$`, c.iDelete)
	ctx.Step(`^retrieving "([^"]*)" should fail$`, c.retrievingKeyFails)

	// --- Assertions: generic ---
	ctx.Step(`^the entity type is "([^"]*)"$`, c.entityTypeIs)
	ctx.Step(`^the entity name is "([^"]*)"$`, c.entityNameIs)

	// --- Assertions: per-entity ---
	ctx.Step(`^the light power is (on|off)$`, c.lightPowerIs)
	ctx.Step(`^the light brightness is (\d+)$`, c.lightBrightnessIs)
	ctx.Step(`^the light temperature is (\d+)$`, c.lightTemperatureIs)
	ctx.Step(`^the switch power is (on|off)$`, c.switchPowerIs)
	ctx.Step(`^the cover position is (\d+)$`, c.coverPositionIs)
	ctx.Step(`^the lock is (locked|unlocked)$`, c.lockIs)
	ctx.Step(`^the fan power is (on|off)$`, c.fanPowerIs)
	ctx.Step(`^the fan percentage is (\d+)$`, c.fanPercentageIs)
	ctx.Step(`^the sensor value is "([^"]*)"$`, c.sensorValueIs)
	ctx.Step(`^the sensor unit is "([^"]*)"$`, c.sensorUnitIs)
	ctx.Step(`^the sensor device_class is "([^"]*)"$`, c.sensorDeviceClassIs)
	ctx.Step(`^the binary sensor is (on|off)$`, c.binarySensorIs)
	ctx.Step(`^the binary sensor device_class is "([^"]*)"$`, c.binarySensorDeviceClassIs)
	ctx.Step(`^the climate hvac_mode is "([^"]*)"$`, c.climateHVACModeIs)
	ctx.Step(`^the climate temperature is (\d+)$`, c.climateTemperatureIs)
	ctx.Step(`^the button presses is (\d+)$`, c.buttonPressesIs)
	ctx.Step(`^the number value is ([\d.]+)$`, c.numberValueIs)
	ctx.Step(`^the select option is "([^"]*)"$`, c.selectOptionIs)
	ctx.Step(`^the text value is "([^"]*)"$`, c.textValueIs)

	// --- Command dispatch ---
	ctx.Step(`^a command listener on "([^"]*)"$`, c.aCommandListenerOn)
	ctx.Step(`^I send "([^"]*)" to "([^"]*)"$`, c.iSendCommandTo)
	ctx.Step(`^I send "light_set_brightness" with brightness (\d+) to "([^"]*)"$`, c.iSendLightSetBrightnessTo)
	ctx.Step(`^I send "light_set_color_temp" with mireds (\d+) to "([^"]*)"$`, c.iSendLightSetColorTempTo)
	ctx.Step(`^I send "light_set_rgb" with r (\d+) g (\d+) b (\d+) to "([^"]*)"$`, c.iSendLightSetRGBTo)
	ctx.Step(`^I send "cover_set_position" with position (\d+) to "([^"]*)"$`, c.iSendCoverSetPositionTo)
	ctx.Step(`^I send "fan_set_speed" with percentage (\d+) to "([^"]*)"$`, c.iSendFanSetSpeedTo)
	ctx.Step(`^I send "number_set_value" with value ([\d.]+) to "([^"]*)"$`, c.iSendNumberSetValueTo)
	ctx.Step(`^I send "select_option" with option "([^"]*)" to "([^"]*)"$`, c.iSendSelectOptionTo)
	ctx.Step(`^I send "text_set_value" with value "([^"]*)" to "([^"]*)"$`, c.iSendTextSetValueTo)
	ctx.Step(`^the received command action is "([^"]*)"$`, c.receivedCommandActionIs)

	// --- Query DSL ---
	ctx.Step(`^the following entities exist:$`, c.theFollowingEntitiesExist)
	ctx.Step(`^I query where "([^"]*)" equals "([^"]*)"$`, c.iQueryWhereEquals)
	ctx.Step(`^I query where "([^"]*)" equals "([^"]*)" and "([^"]*)" equals "([^"]*)"$`, c.iQueryWhereTwoFilters)
	ctx.Step(`^I query where "([^"]*)" equals "([^"]*)" and "([^"]*)" greater than ([\d.]+)$`, c.iQueryWhereGreaterThan)
	ctx.Step(`^I search with pattern "([^"]*)"$`, c.iSearchWithPattern)
	ctx.Step(`^I get (\d+) results$`, c.iGetNResults)
	ctx.Step(`^I get 1 result$`, c.iGet1Result)
	ctx.Step(`^the results include "([^"]*)"$`, c.resultsInclude)
	ctx.Step(`^the results do not include "([^"]*)"$`, c.resultsDoNotInclude)

	// --- Internal storage ---
	ctx.Step(`^I write internal data for "([^"]*)" with payload '([^']*)'$`, c.iWriteInternalData)
	ctx.Step(`^I read internal data for "([^"]*)"$`, c.iReadInternalData)
	ctx.Step(`^the internal data matches '([^']*)'$`, c.internalDataMatches)
	ctx.Step(`^querying type "([^"]*)" returns only state entities$`, c.queryingTypeReturnsOnlyStateEntities)

	// --- Translate: Decode / Encode ---
	ctx.Step(`^I decode a "([^"]*)" payload '([^']*)'$`, c.iDecodePayloadAs)
	ctx.Step(`^I encode "([^"]*)" command with '([^']*)'$`, c.iEncodeCommandWithJSON)
	ctx.Step(`^the wire payload field "([^"]*)" equals (\d+(?:\.\d+)?)$`, c.wirePayloadFieldEqualsNum)
	ctx.Step(`^the wire payload field "([^"]*)" equals "([^"]*)"$`, c.wirePayloadFieldEqualsString)

	// --- mDNS Discovery ---
	c.registerDiscoverySteps(ctx)
}
