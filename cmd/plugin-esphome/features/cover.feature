Feature: Cover Entity
  # Source ref: contracts/cover.md

  Scenario: Create with default state
    Given a cover entity "test.dev1.cover001" named "Blinds" with position 0
    When I retrieve "test.dev1.cover001"
    Then the entity type is "cover"
    And the cover position is 0

  Scenario: State fields hydrate correctly
    Given a cover entity "test.dev1.cover002" named "Roller Shade" with position 75
    When I retrieve "test.dev1.cover002"
    Then the cover position is 75

  Scenario: Query by type
    Given a cover entity "test.dev1.cover003" named "Curtain" with position 0
    And a switch entity "test.dev1.sw001" named "Switch" with power off
    When I query where "type" equals "cover"
    Then the results include "test.dev1.cover003"
    And the results do not include "test.dev1.sw001"

  Scenario: Query by position
    Given a cover entity "test.dev1.coverOpen" named "Open" with position 80
    And a cover entity "test.dev1.coverClosed" named "Closed" with position 10
    When I query where "type" equals "cover" and "state.position" greater than 50
    Then I get 1 result

  Scenario: Update is reflected on retrieval
    Given a cover entity "test.dev1.coverUpd" named "Cover" with position 0
    And I update cover "test.dev1.coverUpd" to position 50
    When I retrieve "test.dev1.coverUpd"
    Then the cover position is 50

  Scenario: Delete removes entity
    Given a cover entity "test.dev1.coverDel" named "Cover" with position 0
    When I delete "test.dev1.coverDel"
    Then retrieving "test.dev1.coverDel" should fail

  Scenario: cover_open command is dispatched
    Given a command listener on "test.>"
    When I send "cover_open" to "test.dev1.cover001"
    Then the received command action is "cover_open"

  Scenario: cover_close command is dispatched
    Given a command listener on "test.>"
    When I send "cover_close" to "test.dev1.cover001"
    Then the received command action is "cover_close"

  Scenario: cover_set_position command is dispatched
    Given a command listener on "test.>"
    When I send "cover_set_position" with position 45 to "test.dev1.cover001"
    Then the received command action is "cover_set_position"

  Scenario: Raw payload decodes to canonical state
    When I decode a "cover" payload '{"position":75}'
    Then the cover position is 75

  Scenario: cover_set_position encodes to wire format
    When I encode "cover_set_position" command with '{"position":75}'
    Then the wire payload field "position" equals 75

  Scenario: Raw discovery data is stored internally and hidden from queries
    Given a cover entity "test.dev1.cover001" named "Blinds" with position 0
    And I write internal data for "test.dev1.cover001" with payload '{"commandTopic":"zigbee2mqtt/blind/set","positionOpen":100,"positionClosed":0}'
    When I read internal data for "test.dev1.cover001"
    Then the internal data matches '{"commandTopic":"zigbee2mqtt/blind/set","positionOpen":100,"positionClosed":0}'
    And querying type "cover" returns only state entities
