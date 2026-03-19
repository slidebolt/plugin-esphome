# Climate — Protocol Contract

> **Plugin authors:** Replace the "External Protocol" section below with the
> raw protocol binding for your plugin. The feature file at
> `features/climate.feature` tests the SlideBolt side; this document
> explains the external side.

## External Protocol

```yaml
# Example: Zigbee2MQTT climate discovery payload (homeassistant/climate/<id>/config)
mqtt:
  - climate:
      name: Study
      modes:
        - "off"
        - "cool"
        - "fan_only"
      fan_modes:
        - "high"
        - "medium"
        - "low"
      preset_modes:
        - "eco"
        - "sleep"
        - "activity"
      mode_command_topic: "study/ac/mode/set"
      temperature_command_topic: "study/ac/temperature/set"
      fan_mode_command_topic: "study/ac/fan/set"
      precision: 1.0
```

## SlideBolt Domain Mapping

| External field              | SlideBolt field           |
|-----------------------------|---------------------------|
| modes (current)             | Climate.HVACMode          |
| temperature (current)       | Climate.Temperature       |
| temperature_unit            | Climate.TemperatureUnit   |
| mode_command_topic payload  | ClimateSetMode.HVACMode   |
| temperature_command_topic   | ClimateSetTemperature.Temperature |

## Supported Commands

| Command                    | SlideBolt action           |
|---------------------------|---------------------------|
| Set HVAC mode              | climate_set_mode           |
| Set target temperature     | climate_set_temperature    |

## Notes

_(Add any integration notes, caveats, or quirks specific to this entity type in your plugin.)_
