Feature: Select Entity
  # Source ref: contracts/select.md

  Scenario: Create with default state
    Given a select entity "test.dev1.sel001" named "Color Mode" with option "white" and options "white,color,effect"
    When I retrieve "test.dev1.sel001"
    Then the entity type is "select"
    And the select option is "white"

  Scenario: State fields hydrate correctly
    Given a select entity "test.dev1.sel002" named "Fan Speed" with option "medium" and options "low,medium,high"
    When I retrieve "test.dev1.sel002"
    Then the select option is "medium"

  Scenario: Query by type
    Given a select entity "test.dev1.sel003" named "Mode" with option "auto" and options "auto,manual"
    And a switch entity "test.dev1.sw001" named "Switch" with power off
    When I query where "type" equals "select"
    Then the results include "test.dev1.sel003"
    And the results do not include "test.dev1.sw001"

  Scenario: Query by current option
    Given a select entity "test.dev1.selA" named "Select A" with option "cool" and options "cool,heat,off"
    And a select entity "test.dev1.selB" named "Select B" with option "heat" and options "cool,heat,off"
    When I query where "type" equals "select" and "state.option" equals "cool"
    Then I get 1 result

  Scenario: Update is reflected on retrieval
    Given a select entity "test.dev1.selUpd" named "Select" with option "off" and options "off,on"
    And I update select "test.dev1.selUpd" to option "on"
    When I retrieve "test.dev1.selUpd"
    Then the select option is "on"

  Scenario: Delete removes entity
    Given a select entity "test.dev1.selDel" named "Select" with option "a" and options "a,b"
    When I delete "test.dev1.selDel"
    Then retrieving "test.dev1.selDel" should fail

  Scenario: select_option command is dispatched
    Given a command listener on "test.>"
    When I send "select_option" with option "color" to "test.dev1.sel001"
    Then the received command action is "select_option"

  Scenario: Raw payload decodes to canonical state
    When I decode a "select" payload '{"option":"cool","options":["cool","heat"]}'
    Then the select option is "cool"

  Scenario: select_option encodes to wire format
    When I encode "select_option" command with '{"option":"cool"}'
    Then the wire payload field "option" equals "cool"

  Scenario: Raw discovery data is stored internally and hidden from queries
    Given a select entity "test.dev1.sel001" named "Color Mode" with option "white" and options "white,color,effect"
    And I write internal data for "test.dev1.sel001" with payload '{"commandTopic":"zigbee2mqtt/select/set","options":["option1","option2","option3"]}'
    When I read internal data for "test.dev1.sel001"
    Then the internal data matches '{"commandTopic":"zigbee2mqtt/select/set","options":["option1","option2","option3"]}'
    And querying type "select" returns only state entities
