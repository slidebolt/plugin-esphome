Feature: Climate Entity
  # Source ref: contracts/climate.md

  Scenario: Create with default state
    Given a climate entity "test.dev1.hvac001" named "Living Room AC" with hvac_mode "off" temperature 22
    When I retrieve "test.dev1.hvac001"
    Then the entity type is "climate"
    And the climate hvac_mode is "off"
    And the climate temperature is 22

  Scenario: State fields hydrate correctly
    Given a climate entity "test.dev1.hvac002" named "Bedroom AC" with hvac_mode "cool" temperature 20 unit "C"
    When I retrieve "test.dev1.hvac002"
    Then the climate hvac_mode is "cool"
    And the climate temperature is 20

  Scenario: Query by type
    Given a climate entity "test.dev1.hvac003" named "Office AC" with hvac_mode "off" temperature 22
    And a sensor entity "test.dev1.temp001" named "Temp" with value "22" and unit "°C"
    When I query where "type" equals "climate"
    Then the results include "test.dev1.hvac003"
    And the results do not include "test.dev1.temp001"

  Scenario: Query by hvac mode
    Given a climate entity "test.dev1.hvacCool" named "Cooling" with hvac_mode "cool" temperature 20
    And a climate entity "test.dev1.hvacOff" named "Off Unit" with hvac_mode "off" temperature 22
    When I query where "type" equals "climate" and "state.hvacMode" equals "cool"
    Then I get 1 result

  Scenario: Update is reflected on retrieval
    Given a climate entity "test.dev1.hvacUpd" named "Climate" with hvac_mode "off" temperature 22
    And I update climate "test.dev1.hvacUpd" to hvac_mode "heat" temperature 24
    When I retrieve "test.dev1.hvacUpd"
    Then the climate hvac_mode is "heat"
    And the climate temperature is 24

  Scenario: Delete removes entity
    Given a climate entity "test.dev1.hvacDel" named "Climate" with hvac_mode "off" temperature 22
    When I delete "test.dev1.hvacDel"
    Then retrieving "test.dev1.hvacDel" should fail

  Scenario: climate_set_mode command is dispatched
    Given a command listener on "test.>"
    When I send "climate_set_mode" to "test.dev1.hvac001"
    Then the received command action is "climate_set_mode"

  Scenario: climate_set_temperature command is dispatched
    Given a command listener on "test.>"
    When I send "climate_set_temperature" to "test.dev1.hvac001"
    Then the received command action is "climate_set_temperature"

  Scenario: Raw payload decodes to canonical state
    When I decode a "climate" payload '{"hvacMode":"cool","temperature":20}'
    Then the climate hvac_mode is "cool"
    And the climate temperature is 20

  Scenario: climate_set_mode encodes to wire format
    When I encode "climate_set_mode" command with '{"hvacMode":"cool"}'
    Then the wire payload field "hvacMode" equals "cool"

  Scenario: Raw discovery data is stored internally and hidden from queries
    Given a climate entity "test.dev1.hvac001" named "Living Room AC" with hvac_mode "off" temperature 22
    And I write internal data for "test.dev1.hvac001" with payload '{"modeCommandTopic":"zigbee2mqtt/ac/mode/set","temperatureCommandTopic":"zigbee2mqtt/ac/temp/set"}'
    When I read internal data for "test.dev1.hvac001"
    Then the internal data matches '{"modeCommandTopic":"zigbee2mqtt/ac/mode/set","temperatureCommandTopic":"zigbee2mqtt/ac/temp/set"}'
    And querying type "climate" returns only state entities
