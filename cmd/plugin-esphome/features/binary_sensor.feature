Feature: Binary Sensor Entity
  # Source ref: contracts/binary_sensor.md
  # Binary sensors are read-only — no commands.

  Scenario: Create with default state
    Given a binary_sensor entity "test.dev1.motion001" named "Motion Sensor" with on false
    When I retrieve "test.dev1.motion001"
    Then the entity type is "binary_sensor"
    And the binary sensor is off

  Scenario: State fields hydrate correctly including device class
    Given a binary_sensor entity "test.dev1.door001" named "Door Sensor" with on true and device_class "door"
    When I retrieve "test.dev1.door001"
    Then the binary sensor is on
    And the binary sensor device_class is "door"

  Scenario: Query by type
    Given a binary_sensor entity "test.dev1.motion002" named "Hall Motion" with on false
    And a sensor entity "test.dev1.temp001" named "Temp" with value "20" and unit "°C"
    When I query where "type" equals "binary_sensor"
    Then the results include "test.dev1.motion002"
    And the results do not include "test.dev1.temp001"

  Scenario: Query by on state
    Given a binary_sensor entity "test.dev1.bsOn" named "Active" with on true
    And a binary_sensor entity "test.dev1.bsOff" named "Inactive" with on false
    When I query where "type" equals "binary_sensor" and "state.on" equals "true"
    Then I get 1 result

  Scenario: Delete removes entity
    Given a binary_sensor entity "test.dev1.bsDel" named "Sensor" with on false
    When I delete "test.dev1.bsDel"
    Then retrieving "test.dev1.bsDel" should fail

  Scenario: Raw payload decodes to canonical state
    When I decode a "binary_sensor" payload '{"on":true}'
    Then the binary sensor is on

  Scenario: Raw discovery data is stored internally and hidden from queries
    Given a binary_sensor entity "test.dev1.motion001" named "Motion Sensor" with on false
    And I write internal data for "test.dev1.motion001" with payload '{"stateTopic":"zigbee2mqtt/motion/state","deviceClass":"motion"}'
    When I read internal data for "test.dev1.motion001"
    Then the internal data matches '{"stateTopic":"zigbee2mqtt/motion/state","deviceClass":"motion"}'
    And querying type "binary_sensor" returns only state entities
