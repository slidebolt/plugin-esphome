Feature: Lock Entity
  # Source ref: contracts/lock.md

  Scenario: Create with default state
    Given a lock entity "test.dev1.lock001" named "Front Door" with locked false
    When I retrieve "test.dev1.lock001"
    Then the entity type is "lock"
    And the lock is unlocked

  Scenario: State fields hydrate correctly - locked
    Given a lock entity "test.dev1.lock002" named "Back Door" with locked true
    When I retrieve "test.dev1.lock002"
    Then the lock is locked

  Scenario: Query by type
    Given a lock entity "test.dev1.lock003" named "Garage Lock" with locked false
    And a switch entity "test.dev1.sw001" named "Switch" with power off
    When I query where "type" equals "lock"
    Then the results include "test.dev1.lock003"
    And the results do not include "test.dev1.sw001"

  Scenario: Query locked entities
    Given a lock entity "test.dev1.lockA" named "Lock A" with locked true
    And a lock entity "test.dev1.lockB" named "Lock B" with locked false
    When I query where "type" equals "lock" and "state.locked" equals "true"
    Then I get 1 result

  Scenario: Query unlocked entities
    Given a lock entity "test.dev1.lockC" named "Lock C" with locked true
    And a lock entity "test.dev1.lockD" named "Lock D" with locked false
    When I query where "type" equals "lock" and "state.locked" equals "false"
    Then I get 1 result

  Scenario: Update is reflected on retrieval
    Given a lock entity "test.dev1.lockUpd" named "Lock" with locked false
    And I update lock "test.dev1.lockUpd" to locked true
    When I retrieve "test.dev1.lockUpd"
    Then the lock is locked

  Scenario: Delete removes entity
    Given a lock entity "test.dev1.lockDel" named "Lock" with locked false
    When I delete "test.dev1.lockDel"
    Then retrieving "test.dev1.lockDel" should fail

  Scenario: lock_lock command is dispatched
    Given a command listener on "test.>"
    When I send "lock_lock" to "test.dev1.lock001"
    Then the received command action is "lock_lock"

  Scenario: lock_unlock command is dispatched
    Given a command listener on "test.>"
    When I send "lock_unlock" to "test.dev1.lock001"
    Then the received command action is "lock_unlock"

  Scenario: Raw payload decodes to canonical state
    When I decode a "lock" payload '{"locked":true}'
    Then the lock is locked

  Scenario: lock_lock encodes to wire format
    When I encode "lock_lock" command with '{}'
    Then the wire payload field "state" equals "LOCK"

  Scenario: Raw discovery data is stored internally and hidden from queries
    Given a lock entity "test.dev1.lock001" named "Front Door" with locked false
    And I write internal data for "test.dev1.lock001" with payload '{"commandTopic":"zigbee2mqtt/lock/set","lockPayload":"LOCK","unlockPayload":"UNLOCK"}'
    When I read internal data for "test.dev1.lock001"
    Then the internal data matches '{"commandTopic":"zigbee2mqtt/lock/set","lockPayload":"LOCK","unlockPayload":"UNLOCK"}'
    And querying type "lock" returns only state entities
