Feature: Sensor Entity
  # Source ref: contracts/sensor.md
  # Sensors are read-only — no commands.

  Scenario: Create with default state
    Given a sensor entity "test.dev1.temp001" named "Temperature" with value "22.5" and unit "°C"
    When I retrieve "test.dev1.temp001"
    Then the entity type is "sensor"
    And the sensor value is "22.5"
    And the sensor unit is "°C"

  Scenario: State fields hydrate correctly including device class
    Given a sensor entity "test.dev1.hum001" named "Humidity" with value "65" unit "%" and device_class "humidity"
    When I retrieve "test.dev1.hum001"
    Then the sensor value is "65"
    And the sensor unit is "%"
    And the sensor device_class is "humidity"

  Scenario: Query by type
    Given a sensor entity "test.dev1.temp002" named "Outdoor Temp" with value "18" and unit "°C"
    And a light entity "test.dev1.light001" named "Light" with power off
    When I query where "type" equals "sensor"
    Then the results include "test.dev1.temp002"
    And the results do not include "test.dev1.light001"

  Scenario: Update is reflected on retrieval
    Given a sensor entity "test.dev1.tempUpd" named "Sensor" with value "20" and unit "°C"
    And I update sensor "test.dev1.tempUpd" to value "25" and unit "°C"
    When I retrieve "test.dev1.tempUpd"
    Then the sensor value is "25"

  Scenario: Delete removes entity
    Given a sensor entity "test.dev1.tempDel" named "Sensor" with value "0" and unit ""
    When I delete "test.dev1.tempDel"
    Then retrieving "test.dev1.tempDel" should fail

  Scenario: Raw payload decodes to canonical state
    When I decode a "sensor" payload '{"value":22.5,"unit":"°C"}'
    Then the sensor value is "22.5"
    And the sensor unit is "°C"

  Scenario: Raw discovery data is stored internally and hidden from queries
    Given a sensor entity "test.dev1.temp001" named "Temperature" with value "22.5" and unit "°C"
    And I write internal data for "test.dev1.temp001" with payload '{"stateTopic":"zigbee2mqtt/temp/state","deviceClass":"temperature"}'
    When I read internal data for "test.dev1.temp001"
    Then the internal data matches '{"stateTopic":"zigbee2mqtt/temp/state","deviceClass":"temperature"}'
    And querying type "sensor" returns only state entities
