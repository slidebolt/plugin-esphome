Feature: Text Entity
  # Source ref: contracts/text.md

  Scenario: Create with default state
    Given a text entity "test.dev1.txt001" named "Label" with value "hello"
    When I retrieve "test.dev1.txt001"
    Then the entity type is "text"
    And the text value is "hello"

  Scenario: State fields hydrate correctly
    Given a text entity "test.dev1.txt002" named "Pattern Input" with value "abc123" min 3 max 10 pattern "[a-z0-9]+" mode "text"
    When I retrieve "test.dev1.txt002"
    Then the text value is "abc123"

  Scenario: Query by type
    Given a text entity "test.dev1.txt003" named "Note" with value "test"
    And a switch entity "test.dev1.sw001" named "Switch" with power off
    When I query where "type" equals "text"
    Then the results include "test.dev1.txt003"
    And the results do not include "test.dev1.sw001"

  Scenario: Query by value
    Given a text entity "test.dev1.txtA" named "Text A" with value "active"
    And a text entity "test.dev1.txtB" named "Text B" with value "inactive"
    When I query where "type" equals "text" and "state.value" equals "active"
    Then I get 1 result

  Scenario: Update is reflected on retrieval
    Given a text entity "test.dev1.txtUpd" named "Text" with value "old"
    And I update text "test.dev1.txtUpd" to value "new"
    When I retrieve "test.dev1.txtUpd"
    Then the text value is "new"

  Scenario: Delete removes entity
    Given a text entity "test.dev1.txtDel" named "Text" with value "bye"
    When I delete "test.dev1.txtDel"
    Then retrieving "test.dev1.txtDel" should fail

  Scenario: text_set_value command is dispatched
    Given a command listener on "test.>"
    When I send "text_set_value" with value "world" to "test.dev1.txt001"
    Then the received command action is "text_set_value"

  Scenario: Raw payload decodes to canonical state
    When I decode a "text" payload '{"value":"hello"}'
    Then the text value is "hello"

  Scenario: text_set_value encodes to wire format
    When I encode "text_set_value" command with '{"value":"hello"}'
    Then the wire payload field "value" equals "hello"

  Scenario: Raw discovery data is stored internally and hidden from queries
    Given a text entity "test.dev1.txt001" named "Label" with value "hello"
    And I write internal data for "test.dev1.txt001" with payload '{"commandTopic":"zigbee2mqtt/text/set","maxLength":255}'
    When I read internal data for "test.dev1.txt001"
    Then the internal data matches '{"commandTopic":"zigbee2mqtt/text/set","maxLength":255}'
    And querying type "text" returns only state entities
