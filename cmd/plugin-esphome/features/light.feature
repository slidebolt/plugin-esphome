Feature: Light Entity
  # Source ref: contracts/light.md

  Scenario: Create with default state
    Given a light entity "test.dev1.light001" named "Ceiling Light" with power off
    When I retrieve "test.dev1.light001"
    Then the entity type is "light"
    And the light power is off

  Scenario: State fields hydrate correctly
    Given a light entity "test.dev1.light002" named "Desk Lamp" with power on brightness 200 temperature 4000
    When I retrieve "test.dev1.light002"
    Then the light power is on
    And the light brightness is 200
    And the light temperature is 4000

  Scenario: Query by type
    Given a light entity "test.dev1.light003" named "Wall Light" with power off
    And a switch entity "test.dev1.sw001" named "Switch" with power off
    When I query where "type" equals "light"
    Then the results include "test.dev1.light003"
    And the results do not include "test.dev1.sw001"

  Scenario: Query by brightness
    Given a light entity "test.dev1.bright" named "Bright" with power on brightness 200 temperature 0
    And a light entity "test.dev1.dim" named "Dim" with power on brightness 50 temperature 0
    When I query where "type" equals "light" and "state.brightness" greater than 100
    Then I get 1 result

  Scenario: Update is reflected on retrieval
    Given a light entity "test.dev1.upd" named "Light" with power off
    And I update "test.dev1.upd" to power on brightness 254
    When I retrieve "test.dev1.upd"
    Then the light power is on
    And the light brightness is 254

  Scenario: Delete removes entity
    Given a light entity "test.dev1.del" named "Light" with power off
    When I delete "test.dev1.del"
    Then retrieving "test.dev1.del" should fail

  Scenario: light_turn_on command is dispatched
    Given a command listener on "test.>"
    When I send "light_turn_on" to "test.dev1.light001"
    Then the received command action is "light_turn_on"

  Scenario: light_turn_off command is dispatched
    Given a command listener on "test.>"
    When I send "light_turn_off" to "test.dev1.light001"
    Then the received command action is "light_turn_off"

  Scenario: light_set_brightness command is dispatched
    Given a command listener on "test.>"
    When I send "light_set_brightness" with brightness 200 to "test.dev1.light001"
    Then the received command action is "light_set_brightness"

  Scenario: light_set_color_temp command is dispatched
    Given a command listener on "test.>"
    When I send "light_set_color_temp" with mireds 370 to "test.dev1.light001"
    Then the received command action is "light_set_color_temp"

  Scenario: light_set_rgb command is dispatched
    Given a command listener on "test.>"
    When I send "light_set_rgb" with r 255 g 128 b 0 to "test.dev1.light001"
    Then the received command action is "light_set_rgb"

  Scenario: Raw payload decodes to canonical state
    When I decode a "light" payload '{"power":true,"brightness":200}'
    Then the light power is on
    And the light brightness is 200

  Scenario: light_set_brightness encodes to wire format
    When I encode "light_set_brightness" command with '{"brightness":200}'
    Then the wire payload field "brightness" equals 200

  Scenario: Raw discovery data is stored internally and hidden from queries
    Given a light entity "test.dev1.light001" named "Ceiling Light" with power off
    And I write internal data for "test.dev1.light001" with payload '{"commandTopic":"zigbee2mqtt/ceiling/set","brightnessScale":254}'
    When I read internal data for "test.dev1.light001"
    Then the internal data matches '{"commandTopic":"zigbee2mqtt/ceiling/set","brightnessScale":254}'
    And querying type "light" returns only state entities
