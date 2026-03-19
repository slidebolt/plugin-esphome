package main

// translate_test.go — table-driven tests for the Decode/Encode translation layer.
//
// Test layer philosophy for translation:
//   Each decode/encode function gets a table of cases covering:
//   - Happy path (valid canonical input)
//   - Empty / nil payload (silent skip for Decode, pass-through for Encode)
//   - Garbage input (silent skip for Decode, error for Encode where applicable)
//   - Out-of-range values (clamped by Decode, rejected by Encode)
//
// When copying this plugin, replace the test cases with your protocol's
// actual wire format — the table structure stays the same.

import (
	"encoding/json"
	"testing"

	domain "github.com/slidebolt/sb-domain"
	translate "github.com/slidebolt/plugin-esphome/internal/translate"
)

// ---------------------------------------------------------------------------
// Decode tests
// ---------------------------------------------------------------------------

func TestDecode_Light(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantOK    bool
		wantPower bool
		wantBr    int
	}{
		{"valid on with brightness", `{"power":true,"brightness":200}`, true, true, 200},
		{"valid off", `{"power":false,"brightness":0}`, true, false, 0},
		{"brightness clamped at max", `{"power":true,"brightness":300}`, true, true, 254},
		{"brightness clamped at min", `{"power":true,"brightness":-10}`, true, true, 0},
		{"empty payload", ``, false, false, 0},
		{"garbage payload", `not json`, false, false, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := translate.Decode("light", json.RawMessage(tc.raw))
			if ok != tc.wantOK {
				t.Fatalf("ok: got %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			s, ok2 := got.(domain.Light)
			if !ok2 {
				t.Fatalf("type: got %T, want domain.Light", got)
			}
			if s.Power != tc.wantPower {
				t.Errorf("Power: got %v, want %v", s.Power, tc.wantPower)
			}
			if s.Brightness != tc.wantBr {
				t.Errorf("Brightness: got %d, want %d", s.Brightness, tc.wantBr)
			}
		})
	}
}

func TestDecode_Switch(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantOK    bool
		wantPower bool
	}{
		{"on", `{"power":true}`, true, true},
		{"off", `{"power":false}`, true, false},
		{"empty", ``, false, false},
		{"garbage", `!!!`, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := translate.Decode("switch", json.RawMessage(tc.raw))
			if ok != tc.wantOK {
				t.Fatalf("ok: got %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			s := got.(domain.Switch)
			if s.Power != tc.wantPower {
				t.Errorf("Power: got %v, want %v", s.Power, tc.wantPower)
			}
		})
	}
}

func TestDecode_Cover(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		wantOK   bool
		wantPos  int
	}{
		{"mid", `{"position":50}`, true, 50},
		{"open", `{"position":100}`, true, 100},
		{"closed", `{"position":0}`, true, 0},
		{"over max clamped", `{"position":150}`, true, 100},
		{"under min clamped", `{"position":-5}`, true, 0},
		{"empty", ``, false, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := translate.Decode("cover", json.RawMessage(tc.raw))
			if ok != tc.wantOK {
				t.Fatalf("ok: got %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			s := got.(domain.Cover)
			if s.Position != tc.wantPos {
				t.Errorf("Position: got %d, want %d", s.Position, tc.wantPos)
			}
		})
	}
}

func TestDecode_Fan(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantOK     bool
		wantPct    int
	}{
		{"on full", `{"power":true,"percentage":100}`, true, 100},
		{"on half", `{"power":true,"percentage":50}`, true, 50},
		{"off", `{"power":false,"percentage":0}`, true, 0},
		{"over max clamped", `{"power":true,"percentage":120}`, true, 100},
		{"under min clamped", `{"power":true,"percentage":-5}`, true, 0},
		{"empty", ``, false, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := translate.Decode("fan", json.RawMessage(tc.raw))
			if ok != tc.wantOK {
				t.Fatalf("ok: got %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			s := got.(domain.Fan)
			if s.Percentage != tc.wantPct {
				t.Errorf("Percentage: got %d, want %d", s.Percentage, tc.wantPct)
			}
		})
	}
}

func TestDecode_Sensor(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantOK  bool
		wantUnit string
	}{
		{"temp with unit", `{"value":22.5,"unit":"°C"}`, true, "°C"},
		{"no unit", `{"value":100}`, true, ""},
		{"empty", ``, false, ""},
		{"garbage", `xyz`, false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := translate.Decode("sensor", json.RawMessage(tc.raw))
			if ok != tc.wantOK {
				t.Fatalf("ok: got %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			s := got.(domain.Sensor)
			if s.Unit != tc.wantUnit {
				t.Errorf("Unit: got %q, want %q", s.Unit, tc.wantUnit)
			}
		})
	}
}

func TestDecode_Climate(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		wantOK   bool
		wantMode string
		wantTemp int
	}{
		{"cool mode", `{"hvacMode":"cool","temperature":21}`, true, "cool", 21},
		{"off", `{"hvacMode":"off","temperature":0}`, true, "off", 0},
		{"empty", ``, false, "", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := translate.Decode("climate", json.RawMessage(tc.raw))
			if ok != tc.wantOK {
				t.Fatalf("ok: got %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			s := got.(domain.Climate)
			if s.HVACMode != tc.wantMode {
				t.Errorf("HVACMode: got %q, want %q", s.HVACMode, tc.wantMode)
			}
			if s.Temperature != tc.wantTemp {
				t.Errorf("Temperature: got %d, want %d", s.Temperature, tc.wantTemp)
			}
		})
	}
}

func TestDecode_UnknownType(t *testing.T) {
	_, ok := translate.Decode("thermostat_v2", json.RawMessage(`{"foo":"bar"}`))
	if ok {
		t.Fatal("expected unknown entity type to return ok=false")
	}
}

// ---------------------------------------------------------------------------
// Encode tests
// ---------------------------------------------------------------------------

func TestEncode_LightTurnOn(t *testing.T) {
	out, err := translate.Encode(domain.LightTurnOn{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result map[string]any
	json.Unmarshal(out, &result)
	if result["state"] != "ON" {
		t.Errorf("expected state=ON, got %v", result)
	}
}

func TestEncode_LightSetBrightness(t *testing.T) {
	tests := []struct {
		name    string
		cmd     domain.LightSetBrightness
		wantErr bool
	}{
		{"valid 200", domain.LightSetBrightness{Brightness: 200}, false},
		{"valid 0", domain.LightSetBrightness{Brightness: 0}, false},
		{"max 254", domain.LightSetBrightness{Brightness: 254}, false},
		{"255 rejected", domain.LightSetBrightness{Brightness: 255}, true},
		{"negative rejected", domain.LightSetBrightness{Brightness: -1}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := translate.Encode(tc.cmd, nil)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err: got %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && out == nil {
				t.Error("expected non-nil output")
			}
		})
	}
}

func TestEncode_LightSetColorTemp(t *testing.T) {
	tests := []struct {
		name    string
		cmd     domain.LightSetColorTemp
		wantErr bool
	}{
		{"valid 370", domain.LightSetColorTemp{Mireds: 370}, false},
		{"min 153", domain.LightSetColorTemp{Mireds: 153}, false},
		{"max 500", domain.LightSetColorTemp{Mireds: 500}, false},
		{"152 rejected", domain.LightSetColorTemp{Mireds: 152}, true},
		{"501 rejected", domain.LightSetColorTemp{Mireds: 501}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := translate.Encode(tc.cmd, nil)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err: got %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestEncode_LightSetRGB(t *testing.T) {
	tests := []struct {
		name    string
		cmd     domain.LightSetRGB
		wantErr bool
	}{
		{"valid", domain.LightSetRGB{R: 255, G: 128, B: 0}, false},
		{"all zero", domain.LightSetRGB{R: 0, G: 0, B: 0}, false},
		{"R=256 rejected", domain.LightSetRGB{R: 256, G: 0, B: 0}, true},
		{"B negative rejected", domain.LightSetRGB{R: 0, G: 0, B: -1}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := translate.Encode(tc.cmd, nil)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err: got %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestEncode_LightSetHS(t *testing.T) {
	tests := []struct {
		name    string
		cmd     domain.LightSetHS
		wantErr bool
	}{
		{"valid", domain.LightSetHS{Hue: 180, Saturation: 50}, false},
		{"hue 361 rejected", domain.LightSetHS{Hue: 361, Saturation: 50}, true},
		{"saturation 101 rejected", domain.LightSetHS{Hue: 90, Saturation: 101}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := translate.Encode(tc.cmd, nil)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err: got %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestEncode_LightSetEffect(t *testing.T) {
	if _, err := translate.Encode(domain.LightSetEffect{Effect: "rainbow"}, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := translate.Encode(domain.LightSetEffect{Effect: ""}, nil); err == nil {
		t.Error("expected error for empty effect")
	}
}

func TestEncode_FanSetSpeed(t *testing.T) {
	tests := []struct {
		name    string
		cmd     domain.FanSetSpeed
		wantErr bool
	}{
		{"valid 50%", domain.FanSetSpeed{Percentage: 50}, false},
		{"valid 0%", domain.FanSetSpeed{Percentage: 0}, false},
		{"valid 100%", domain.FanSetSpeed{Percentage: 100}, false},
		{"101% rejected", domain.FanSetSpeed{Percentage: 101}, true},
		{"negative rejected", domain.FanSetSpeed{Percentage: -1}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := translate.Encode(tc.cmd, nil)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err: got %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestEncode_CoverSetPosition(t *testing.T) {
	tests := []struct {
		name    string
		cmd     domain.CoverSetPosition
		wantErr bool
	}{
		{"valid 50", domain.CoverSetPosition{Position: 50}, false},
		{"valid 0", domain.CoverSetPosition{Position: 0}, false},
		{"valid 100", domain.CoverSetPosition{Position: 100}, false},
		{"101 rejected", domain.CoverSetPosition{Position: 101}, true},
		{"negative rejected", domain.CoverSetPosition{Position: -1}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := translate.Encode(tc.cmd, nil)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err: got %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestEncode_SelectOption(t *testing.T) {
	tests := []struct {
		name    string
		cmd     domain.SelectOption
		wantErr bool
	}{
		{"valid option", domain.SelectOption{Option: "eco"}, false},
		{"empty option rejected", domain.SelectOption{Option: ""}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := translate.Encode(tc.cmd, nil)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err: got %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestEncode_ClimateSetMode(t *testing.T) {
	tests := []struct {
		name    string
		cmd     domain.ClimateSetMode
		wantErr bool
	}{
		{"cool", domain.ClimateSetMode{HVACMode: "cool"}, false},
		{"off", domain.ClimateSetMode{HVACMode: "off"}, false},
		{"empty rejected", domain.ClimateSetMode{HVACMode: ""}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := translate.Encode(tc.cmd, nil)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err: got %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestEncode_SwitchCommands(t *testing.T) {
	for _, cmd := range []any{domain.SwitchTurnOn{}, domain.SwitchTurnOff{}, domain.SwitchToggle{}} {
		out, err := translate.Encode(cmd, nil)
		if err != nil {
			t.Errorf("%T: unexpected error: %v", cmd, err)
		}
		if len(out) == 0 {
			t.Errorf("%T: empty output", cmd)
		}
	}
}

func TestEncode_UnknownCommand(t *testing.T) {
	type unknownCmd struct{}
	_, err := translate.Encode(unknownCmd{}, nil)
	if err == nil {
		t.Fatal("expected error for unknown command type")
	}
}

// ---------------------------------------------------------------------------
// Round-trip: Decode then Encode produces consistent output
// ---------------------------------------------------------------------------

func TestRoundTrip_LightState(t *testing.T) {
	raw := json.RawMessage(`{"power":true,"brightness":150}`)
	state, ok := translate.Decode("light", raw)
	if !ok {
		t.Fatal("Decode failed")
	}
	light := state.(domain.Light)
	if !light.Power || light.Brightness != 150 {
		t.Errorf("unexpected state: %+v", light)
	}

	// Encode a set_brightness command using the decoded brightness
	out, err := translate.Encode(domain.LightSetBrightness{Brightness: light.Brightness}, nil)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var result map[string]any
	json.Unmarshal(out, &result)
	if result["brightness"] == nil {
		t.Error("encoded output missing brightness")
	}
}
