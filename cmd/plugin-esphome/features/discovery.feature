Feature: ESPHome Device Discovery via mDNS
  # ESPHome devices advertise themselves via mDNS using the service type _esphomelib._tcp
  # This feature tests the discovery of local ESPHome devices on the network

  Background:
    Given the mDNS discovery service is configured

  Scenario: Discover ESPHome devices on the network
    When I start mDNS discovery
    And I wait for discovery to complete
    Then I should receive a list of discovered devices

  Scenario: ESPHome device has required fields
    Given an ESPHome device is advertising on the network with name "test-device"
    And the device has address "192.168.1.100"
    And the device has port 6053
    When I discover devices
    Then the discovered device "test-device" should have:
      | field     | value       |
      | name      | test-device |
      | address   | 192.168.1.100 |
      | port      | 6053        |

  Scenario: ESPHome device provides TXT records
    Given an ESPHome device is advertising with:
      | txt_key         | txt_value       |
      | version         | 2023.12.0       |
      | mac             | A1B2C3D4E5F6    |
      | board           | esp32dev        |
      | api_encryption  | 0               |
    When I discover devices
    Then the discovered device should have TXT records:
      | version         | 2023.12.0       |
      | mac             | A1B2C3D4E5F6    |
      | board           | esp32dev        |
      | api_encryption  | 0               |

  Scenario: Parse device metadata from TXT records
    Given an ESPHome device is advertising with:
      | txt_key | txt_value    |
      | version | 2023.12.0    |
      | mac     | A1B2C3D4E5F6 |
      | board   | esp32dev     |
    When I discover devices
    Then the device version should be "2023.12.0"
    And the device MAC should be "A1B2C3D4E5F6"
    And the device board should be "esp32dev"

  Scenario: Detect API encryption requirement
    Given an ESPHome device is advertising with:
      | txt_key        | txt_value |
      | api_encryption | 1         |
    When I discover devices
    Then the device should require API key

  Scenario: Device without API encryption
    Given an ESPHome device is advertising with:
      | txt_key        | txt_value |
      | api_encryption | 0         |
    When I discover devices
    Then the device should not require API key

  Scenario: Multiple ESPHome devices discovered
    Given an ESPHome device is advertising on the network with name "living-room-sensor"
    And an ESPHome device is advertising on the network with name "kitchen-light"
    And an ESPHome device is advertising on the network with name "bedroom-climate"
    When I discover devices
    Then I should have 3 discovered devices
    And the discovered devices should include:
      | living-room-sensor |
      | kitchen-light      |
      | bedroom-climate    |

  Scenario: Device address resolution
    Given an ESPHome device is advertising with:
      | field     | value           |
      | hostname  | esphome-test.local |
      | addresses | 192.168.1.50,192.168.1.51 |
    When I discover devices
    Then the device should have 2 addresses
    And the first address should be "192.168.1.50"

  Scenario: Continuous discovery updates device list
    Given an ESPHome device is advertising on the network with name "device-1"
    When I start continuous discovery
    And I wait for 2 seconds
    Then the device "device-1" should be in the cache
    When a new ESPHome device "device-2" appears on the network
    And I wait for 2 seconds
    Then the device "device-2" should be in the cache
    And I should have 2 devices in the cache

  Scenario: Stale device detection
    Given an ESPHome device was discovered 10 minutes ago
    When I check for stale devices
    Then the device should be marked as stale

  Scenario: Stop discovery service
    Given the mDNS discovery service is running
    When I stop the discovery service
    Then the discovery service should be stopped
    And no new devices should be discovered
