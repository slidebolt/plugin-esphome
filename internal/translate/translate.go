package translate

import (
	"encoding/json"
	"fmt"

	domain "github.com/slidebolt/sb-domain"
)

func Decode(entityType string, raw json.RawMessage) (any, bool) {
	switch entityType {
	case "light":
		return decodeLight(raw)
	case "switch":
		return decodeSwitch(raw)
	case "cover":
		return decodeCover(raw)
	case "lock":
		return decodeLock(raw)
	case "fan":
		return decodeFan(raw)
	case "sensor":
		return decodeSensor(raw)
	case "binary_sensor":
		return decodeBinarySensor(raw)
	case "climate":
		return decodeClimate(raw)
	case "button":
		return decodeButton(raw)
	case "number":
		return decodeNumber(raw)
	case "select":
		return decodeSelect(raw)
	case "text":
		return decodeText(raw)
	default:
		return nil, false
	}
}

func Encode(cmd any, internal json.RawMessage) (json.RawMessage, error) {
	switch c := cmd.(type) {
	case domain.LightTurnOn:
		return encodeLightTurnOn(c, internal)
	case domain.LightTurnOff:
		return encodeLightTurnOff(c, internal)
	case domain.LightSetBrightness:
		return encodeLightSetBrightness(c, internal)
	case domain.LightSetColorTemp:
		return encodeLightSetColorTemp(c, internal)
	case domain.LightSetRGB:
		return encodeLightSetRGB(c, internal)
	case domain.LightSetRGBW:
		return encodeLightSetRGBW(c, internal)
	case domain.LightSetRGBWW:
		return encodeLightSetRGBWW(c, internal)
	case domain.LightSetHS:
		return encodeLightSetHS(c, internal)
	case domain.LightSetXY:
		return encodeLightSetXY(c, internal)
	case domain.LightSetWhite:
		return encodeLightSetWhite(c, internal)
	case domain.LightSetEffect:
		return encodeLightSetEffect(c, internal)
	case domain.SwitchTurnOn:
		return encodeSwitchTurnOn(c, internal)
	case domain.SwitchTurnOff:
		return encodeSwitchTurnOff(c, internal)
	case domain.SwitchToggle:
		return encodeSwitchToggle(c, internal)
	case domain.FanTurnOn:
		return encodeFanTurnOn(c, internal)
	case domain.FanTurnOff:
		return encodeFanTurnOff(c, internal)
	case domain.FanSetSpeed:
		return encodeFanSetSpeed(c, internal)
	case domain.CoverOpen:
		return encodeCoverOpen(c, internal)
	case domain.CoverClose:
		return encodeCoverClose(c, internal)
	case domain.CoverSetPosition:
		return encodeCoverSetPosition(c, internal)
	case domain.LockLock:
		return encodeLockLock(c, internal)
	case domain.LockUnlock:
		return encodeLockUnlock(c, internal)
	case domain.ButtonPress:
		return encodeButtonPress(c, internal)
	case domain.NumberSetValue:
		return encodeNumberSetValue(c, internal)
	case domain.SelectOption:
		return encodeSelectOption(c, internal)
	case domain.TextSetValue:
		return encodeTextSetValue(c, internal)
	case domain.ClimateSetMode:
		return encodeClimateSetMode(c, internal)
	case domain.ClimateSetTemperature:
		return encodeClimateSetTemperature(c, internal)
	default:
		return nil, fmt.Errorf("translate: unsupported command type %T", cmd)
	}
}

func decodeLight(raw json.RawMessage) (any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var s domain.Light
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, false
	}
	if s.Brightness < 0 {
		s.Brightness = 0
	}
	if s.Brightness > 254 {
		s.Brightness = 254
	}
	return s, true
}

func decodeSwitch(raw json.RawMessage) (any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var s domain.Switch
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, false
	}
	return s, true
}

func decodeCover(raw json.RawMessage) (any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var s domain.Cover
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, false
	}
	if s.Position < 0 {
		s.Position = 0
	}
	if s.Position > 100 {
		s.Position = 100
	}
	return s, true
}

func decodeLock(raw json.RawMessage) (any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var s domain.Lock
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, false
	}
	return s, true
}

func decodeFan(raw json.RawMessage) (any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var s domain.Fan
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, false
	}
	if s.Percentage < 0 {
		s.Percentage = 0
	}
	if s.Percentage > 100 {
		s.Percentage = 100
	}
	return s, true
}

func decodeSensor(raw json.RawMessage) (any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var s domain.Sensor
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, false
	}
	return s, true
}

func decodeBinarySensor(raw json.RawMessage) (any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var s domain.BinarySensor
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, false
	}
	return s, true
}

func decodeClimate(raw json.RawMessage) (any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var s domain.Climate
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, false
	}
	return s, true
}

func decodeButton(raw json.RawMessage) (any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var s domain.Button
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, false
	}
	return s, true
}

func decodeNumber(raw json.RawMessage) (any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var s domain.Number
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, false
	}
	return s, true
}

func decodeSelect(raw json.RawMessage) (any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var s domain.Select
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, false
	}
	return s, true
}

func decodeText(raw json.RawMessage) (any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var s domain.Text
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, false
	}
	return s, true
}

func encodeLightTurnOn(_ domain.LightTurnOn, _ json.RawMessage) (json.RawMessage, error) {
	return json.Marshal(map[string]any{"state": "ON"})
}

func encodeLightTurnOff(c domain.LightTurnOff, _ json.RawMessage) (json.RawMessage, error) {
	return json.Marshal(c)
}

func encodeLightSetBrightness(c domain.LightSetBrightness, _ json.RawMessage) (json.RawMessage, error) {
	if c.Brightness < 0 || c.Brightness > 254 {
		return nil, fmt.Errorf("translate: brightness %d out of range [0,254]", c.Brightness)
	}
	return json.Marshal(c)
}

func encodeLightSetColorTemp(c domain.LightSetColorTemp, _ json.RawMessage) (json.RawMessage, error) {
	if c.Mireds < 153 || c.Mireds > 500 {
		return nil, fmt.Errorf("translate: mireds %d out of range [153,500]", c.Mireds)
	}
	return json.Marshal(c)
}

func encodeLightSetRGB(c domain.LightSetRGB, _ json.RawMessage) (json.RawMessage, error) {
	for name, v := range map[string]int{"r": c.R, "g": c.G, "b": c.B} {
		if v < 0 || v > 255 {
			return nil, fmt.Errorf("translate: %s value %d out of range [0,255]", name, v)
		}
	}
	return json.Marshal(c)
}

func encodeLightSetRGBW(c domain.LightSetRGBW, _ json.RawMessage) (json.RawMessage, error) {
	for name, v := range map[string]int{"r": c.R, "g": c.G, "b": c.B, "w": c.W} {
		if v < 0 || v > 255 {
			return nil, fmt.Errorf("translate: %s value %d out of range [0,255]", name, v)
		}
	}
	return json.Marshal(c)
}

func encodeLightSetRGBWW(c domain.LightSetRGBWW, _ json.RawMessage) (json.RawMessage, error) {
	for name, v := range map[string]int{"r": c.R, "g": c.G, "b": c.B, "cw": c.CW, "ww": c.WW} {
		if v < 0 || v > 255 {
			return nil, fmt.Errorf("translate: %s value %d out of range [0,255]", name, v)
		}
	}
	return json.Marshal(c)
}

func encodeLightSetHS(c domain.LightSetHS, _ json.RawMessage) (json.RawMessage, error) {
	if c.Hue < 0 || c.Hue > 360 {
		return nil, fmt.Errorf("translate: hue %.2f out of range [0,360]", c.Hue)
	}
	if c.Saturation < 0 || c.Saturation > 100 {
		return nil, fmt.Errorf("translate: saturation %.2f out of range [0,100]", c.Saturation)
	}
	return json.Marshal(c)
}

func encodeLightSetXY(c domain.LightSetXY, _ json.RawMessage) (json.RawMessage, error) {
	if c.X < 0 || c.X > 1 {
		return nil, fmt.Errorf("translate: x %.4f out of range [0,1]", c.X)
	}
	if c.Y < 0 || c.Y > 1 {
		return nil, fmt.Errorf("translate: y %.4f out of range [0,1]", c.Y)
	}
	return json.Marshal(c)
}

func encodeLightSetWhite(c domain.LightSetWhite, _ json.RawMessage) (json.RawMessage, error) {
	if c.White < 0 || c.White > 254 {
		return nil, fmt.Errorf("translate: white %d out of range [0,254]", c.White)
	}
	return json.Marshal(c)
}

func encodeLightSetEffect(c domain.LightSetEffect, _ json.RawMessage) (json.RawMessage, error) {
	if c.Effect == "" {
		return nil, fmt.Errorf("translate: effect must not be empty")
	}
	return json.Marshal(c)
}

func encodeSwitchTurnOn(_ domain.SwitchTurnOn, _ json.RawMessage) (json.RawMessage, error) {
	return json.Marshal(map[string]any{"state": "ON"})
}

func encodeSwitchTurnOff(_ domain.SwitchTurnOff, _ json.RawMessage) (json.RawMessage, error) {
	return json.Marshal(map[string]any{"state": "OFF"})
}

func encodeSwitchToggle(_ domain.SwitchToggle, _ json.RawMessage) (json.RawMessage, error) {
	return json.Marshal(map[string]any{"state": "TOGGLE"})
}

func encodeFanTurnOn(_ domain.FanTurnOn, _ json.RawMessage) (json.RawMessage, error) {
	return json.Marshal(map[string]any{"state": "ON"})
}

func encodeFanTurnOff(_ domain.FanTurnOff, _ json.RawMessage) (json.RawMessage, error) {
	return json.Marshal(map[string]any{"state": "OFF"})
}

func encodeFanSetSpeed(c domain.FanSetSpeed, _ json.RawMessage) (json.RawMessage, error) {
	if c.Percentage < 0 || c.Percentage > 100 {
		return nil, fmt.Errorf("translate: fan percentage %d out of range 0-100", c.Percentage)
	}
	return json.Marshal(c)
}

func encodeCoverOpen(_ domain.CoverOpen, _ json.RawMessage) (json.RawMessage, error) {
	return json.Marshal(map[string]any{"state": "OPEN"})
}

func encodeCoverClose(_ domain.CoverClose, _ json.RawMessage) (json.RawMessage, error) {
	return json.Marshal(map[string]any{"state": "CLOSE"})
}

func encodeCoverSetPosition(c domain.CoverSetPosition, _ json.RawMessage) (json.RawMessage, error) {
	if c.Position < 0 || c.Position > 100 {
		return nil, fmt.Errorf("translate: cover position %d out of range 0-100", c.Position)
	}
	return json.Marshal(c)
}

func encodeLockLock(_ domain.LockLock, _ json.RawMessage) (json.RawMessage, error) {
	return json.Marshal(map[string]any{"state": "LOCK"})
}

func encodeLockUnlock(c domain.LockUnlock, _ json.RawMessage) (json.RawMessage, error) {
	return json.Marshal(c)
}

func encodeButtonPress(_ domain.ButtonPress, _ json.RawMessage) (json.RawMessage, error) {
	return json.Marshal(map[string]any{"action": "PRESS"})
}

func encodeNumberSetValue(c domain.NumberSetValue, _ json.RawMessage) (json.RawMessage, error) {
	return json.Marshal(c)
}

func encodeSelectOption(c domain.SelectOption, _ json.RawMessage) (json.RawMessage, error) {
	if c.Option == "" {
		return nil, fmt.Errorf("translate: select option must not be empty")
	}
	return json.Marshal(c)
}

func encodeTextSetValue(c domain.TextSetValue, _ json.RawMessage) (json.RawMessage, error) {
	return json.Marshal(c)
}

func encodeClimateSetMode(c domain.ClimateSetMode, _ json.RawMessage) (json.RawMessage, error) {
	if c.HVACMode == "" {
		return nil, fmt.Errorf("translate: climate hvac_mode must not be empty")
	}
	return json.Marshal(c)
}

func encodeClimateSetTemperature(c domain.ClimateSetTemperature, _ json.RawMessage) (json.RawMessage, error) {
	return json.Marshal(c)
}
