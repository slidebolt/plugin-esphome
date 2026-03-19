Feature: Switch Entity
  # Source ref: contracts/switch.md

  Scenario: Create with default state
    Given a switch entity "test.dev1.sw001" named "Outlet" with power off
    When I retrieve "test.dev1.sw001"
    Then the entity type is "switch"
    And the switch power is off

  Scenario: State fields hydrate correctly
    Given a switch entity "test.dev1.sw002" named "Wall Switch" with power on
    When I retrieve "test.dev1.sw002"
    Then the switch power is on

  Scenario: Query by type
    Given a switch entity "test.dev1.sw003" named "Garage" with power off
    And a light entity "test.dev1.light001" named "Light" with power off
    When I query where "type" equals "switch"
    Then the results include "test.dev1.sw003"
    And the results do not include "test.dev1.light001"

  Scenario: Query by power state
    Given a switch entity "test.dev1.swOn" named "On Switch" with power on
    And a switch entity "test.dev1.swOff" named "Off Switch" with power off
    When I query where "type" equals "switch" and "state.power" equals "true"
    Then I get 1 result

  Scenario: Update is reflected on retrieval
    Given a switch entity "test.dev1.swUpd" named "Switch" with power off
    And I update switch "test.dev1.swUpd" to power on
    When I retrieve "test.dev1.swUpd"
    Then the switch power is on

  Scenario: Delete removes entity
    Given a switch entity "test.dev1.swDel" named "Switch" with power off
    When I delete "test.dev1.swDel"
    Then retrieving "test.dev1.swDel" should fail

  Scenario: switch_turn_on command is dispatched
    Given a command listener on "test.>"
    When I send "switch_turn_on" to "test.dev1.sw001"
    Then the received command action is "switch_turn_on"

  Scenario: switch_turn_off command is dispatched
    Given a command listener on "test.>"
    When I send "switch_turn_off" to "test.dev1.sw001"
    Then the received command action is "switch_turn_off"

  Scenario: switch_toggle command is dispatched
    Given a command listener on "test.>"
    When I send "switch_toggle" to "test.dev1.sw001"
    Then the received command action is "switch_toggle"

  Scenario: Raw payload decodes to canonical state
    When I decode a "switch" payload '{"power":true}'
    Then the switch power is on

  Scenario: switch_turn_on encodes to wire format
    When I encode "switch_turn_on" command with '{}'
    Then the wire payload field "state" equals "ON"

  Scenario: Raw discovery data is stored internally and hidden from queries
    Given a switch entity "test.dev1.sw001" named "Outlet" with power off
    And I write internal data for "test.dev1.sw001" with payload '{"commandTopic":"zigbee2mqtt/outlet/set"}'
    When I read internal data for "test.dev1.sw001"
    Then the internal data matches '{"commandTopic":"zigbee2mqtt/outlet/set"}'
    And querying type "switch" returns only state entities
