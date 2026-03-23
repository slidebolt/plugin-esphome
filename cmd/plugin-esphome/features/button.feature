Feature: Button Entity
  # Source ref: contracts/button.md
  # Button tracks press count as an informational counter.

  Scenario: Create with default state
    Given a button entity "test.dev1.btn001" named "Doorbell" with presses 0
    When I retrieve "test.dev1.btn001"
    Then the entity type is "button"
    And the button presses is 0

  Scenario: State fields hydrate correctly
    Given a button entity "test.dev1.btn002" named "Panic Button" with presses 5
    When I retrieve "test.dev1.btn002"
    Then the button presses is 5

  Scenario: Query by type
    Given a button entity "test.dev1.btn003" named "Reset Button" with presses 0
    And a switch entity "test.dev1.sw001" named "Switch" with power off
    When I query where "type" equals "button"
    Then the results include "test.dev1.btn003"
    And the results do not include "test.dev1.sw001"

  Scenario: Update is reflected on retrieval
    Given a button entity "test.dev1.btnUpd" named "Button" with presses 0
    And I update button "test.dev1.btnUpd" to presses 3
    When I retrieve "test.dev1.btnUpd"
    Then the button presses is 3

  Scenario: Delete removes entity
    Given a button entity "test.dev1.btnDel" named "Button" with presses 0
    When I delete "test.dev1.btnDel"
    Then retrieving "test.dev1.btnDel" should fail

  Scenario: button_press command is dispatched
    Given a command listener on "test.>"
    When I send "button_press" to "test.dev1.btn001"
    Then the received command action is "button_press"

  Scenario: Raw payload decodes to canonical state
    When I decode a "button" payload '{"presses":3}'
    Then the button presses is 3

  Scenario: Raw discovery data is stored internally and hidden from queries
    Given a button entity "test.dev1.btn001" named "Doorbell" with presses 0
    And I write internal data for "test.dev1.btn001" with payload '{"commandTopic":"zigbee2mqtt/btn/set","payload":"PRESS"}'
    When I read internal data for "test.dev1.btn001"
    Then the internal data matches '{"commandTopic":"zigbee2mqtt/btn/set","payload":"PRESS"}'
    And querying type "button" returns only state entities
