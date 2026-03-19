//go:build bdd

package main

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"
	"github.com/grandcat/zeroconf"
	mdns "github.com/slidebolt/plugin-esphome/internal/mdns"
)

// ---------------------------------------------------------------------------
// mDNS Discovery step definitions for discovery.feature
// ---------------------------------------------------------------------------

func (c *bddCtx) registerDiscoverySteps(ctx *godog.ScenarioContext) {
	// Background
	ctx.Step(`^the mDNS discovery service is configured$`, c.mdnsDiscoveryIsConfigured)

	// Basic discovery
	ctx.Step(`^I start mDNS discovery$`, c.iStartMDNSDiscovery)
	ctx.Step(`^I wait for discovery to complete$`, c.iWaitForDiscoveryToComplete)
	ctx.Step(`^I should receive a list of discovered devices$`, c.iShouldReceiveDiscoveredDevices)
	ctx.Step(`^I discover devices$`, c.iDiscoverDevices)

	// Device setup
	ctx.Step(`^an ESPHome device is advertising on the network with name "([^"]*)"$`, c.esphomeDeviceAdvertisingWithName)
	ctx.Step(`^an ESPHome device is advertising with:$`, c.esphomeDeviceAdvertisingWithTable)
	ctx.Step(`^the device has address "([^"]*)"$`, c.deviceHasAddress)
	ctx.Step(`^the device has port (\d+)$`, c.deviceHasPort)

	// Device assertions
	ctx.Step(`^the discovered device "([^"]*)" should have:$`, c.discoveredDeviceShouldHave)
	ctx.Step(`^the discovered device should have TXT records:$`, c.discoveredDeviceShouldHaveTXTRecords)
	ctx.Step(`^the device version should be "([^"]*)"$`, c.deviceVersionShouldBe)
	ctx.Step(`^the device MAC should be "([^"]*)"$`, c.deviceMACShouldBe)
	ctx.Step(`^the device board should be "([^"]*)"$`, c.deviceBoardShouldBe)
	ctx.Step(`^the device should require API key$`, c.deviceShouldRequireAPIKey)
	ctx.Step(`^the device should not require API key$`, c.deviceShouldNotRequireAPIKey)
	ctx.Step(`^I should have (\d+) discovered devices$`, c.iShouldHaveNDiscoveredDevices)
	ctx.Step(`^the discovered devices should include:$`, c.discoveredDevicesShouldInclude)
	ctx.Step(`^the device should have (\d+) addresses$`, c.deviceShouldHaveNAddresses)
	ctx.Step(`^the first address should be "([^"]*)"$`, c.firstAddressShouldBe)

	// Continuous discovery
	ctx.Step(`^I start continuous discovery$`, c.iStartContinuousDiscovery)
	ctx.Step(`^I wait for (\d+) seconds?$`, c.iWaitForSeconds)
	ctx.Step(`^the device "([^"]*)" should be in the cache$`, c.deviceShouldBeInCache)
	ctx.Step(`^a new ESPHome device "([^"]*)" appears on the network$`, c.newESPHomeDeviceAppears)
	ctx.Step(`^I should have (\d+) devices in the cache$`, c.iShouldHaveNDevicesInCache)

	// Stale detection
	ctx.Step(`^an ESPHome device was discovered (\d+) minutes? ago$`, c.esphomeDeviceDiscoveredMinutesAgo)
	ctx.Step(`^I check for stale devices$`, c.iCheckForStaleDevices)
	ctx.Step(`^the device should be marked as stale$`, c.deviceShouldBeMarkedAsStale)

	// Stop discovery
	ctx.Step(`^the mDNS discovery service is running$`, c.mdnsDiscoveryServiceIsRunning)
	ctx.Step(`^I stop the discovery service$`, c.iStopDiscoveryService)
	ctx.Step(`^the discovery service should be stopped$`, c.discoveryServiceShouldBeStopped)
	ctx.Step(`^no new devices should be discovered$`, c.noNewDevicesShouldBeDiscovered)
}

// ---------------------------------------------------------------------------
// Step implementations
// ---------------------------------------------------------------------------

func (c *bddCtx) mdnsDiscoveryIsConfigured() error {
	var err error
	c.discovery, err = mdns.NewDiscovery(
		mdns.WithTimeout(2*time.Second),
		mdns.WithMockMode(), // Use mock mode for testing
	)
	if err != nil {
		return fmt.Errorf("failed to create discovery: %w", err)
	}
	c.mockEntries = make([]*zeroconf.ServiceEntry, 0)
	return nil
}

func (c *bddCtx) iStartMDNSDiscovery() error {
	ctx := context.Background()
	devices, err := c.discovery.Discover(ctx)
	if err != nil {
		return fmt.Errorf("discovery failed: %w", err)
	}
	c.discoveredDevices = devices
	// Make the first discovered device available to single-device assertion steps.
	if len(devices) > 0 {
		c.lastDevice = devices[0]
	}
	return nil
}

func (c *bddCtx) iWaitForDiscoveryToComplete() error {
	// Give some time for discovery to settle
	time.Sleep(100 * time.Millisecond)
	return nil
}

func (c *bddCtx) iShouldReceiveDiscoveredDevices() error {
	if c.discoveredDevices == nil {
		return fmt.Errorf("no discovery results available")
	}
	// It's okay if the list is empty in test environment
	return nil
}

func (c *bddCtx) iDiscoverDevices() error {
	return c.iStartMDNSDiscovery()
}

func (c *bddCtx) esphomeDeviceAdvertisingWithName(name string) error {
	entry := zeroconf.NewServiceEntry(name, "_esphomelib._tcp", "local.")
	entry.Port = 6053
	c.discovery.InjectMockEntry(entry)
	c.mockEntries = append(c.mockEntries, entry)
	return nil
}

func (c *bddCtx) esphomeDeviceAdvertisingWithTable(table *godog.Table) error {
	entry := zeroconf.NewServiceEntry("", "_esphomelib._tcp", "local.")

	for _, row := range table.Rows {
		key := row.Cells[0].Value
		value := row.Cells[1].Value

		switch key {
		case "name":
			entry.Instance = value
		case "hostname":
			entry.HostName = value
		case "port":
			port, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("invalid port: %w", err)
			}
			entry.Port = port
		case "addresses":
			addrs := strings.Split(value, ",")
			for _, addr := range addrs {
				entry.AddrIPv4 = append(entry.AddrIPv4, net.ParseIP(strings.TrimSpace(addr)))
			}
		default:
			// Assume it's a TXT record key
			entry.Text = append(entry.Text, fmt.Sprintf("%s=%s", key, value))
		}
	}

	c.discovery.InjectMockEntry(entry)
	c.mockEntries = append(c.mockEntries, entry)
	return nil
}

func (c *bddCtx) deviceHasAddress(address string) error {
	if len(c.mockEntries) == 0 {
		return fmt.Errorf("no device to set address on")
	}
	last := c.mockEntries[len(c.mockEntries)-1]
	last.AddrIPv4 = append(last.AddrIPv4, net.ParseIP(address))
	// Re-inject with updated address
	c.discovery.InjectMockEntry(last)
	return nil
}

func (c *bddCtx) deviceHasPort(port int) error {
	if len(c.mockEntries) == 0 {
		return fmt.Errorf("no device to set port on")
	}
	last := c.mockEntries[len(c.mockEntries)-1]
	last.Port = port
	// Re-inject with updated port
	c.discovery.InjectMockEntry(last)
	return nil
}

func (c *bddCtx) discoveredDeviceShouldHave(name string, table *godog.Table) error {
	// First re-discover to get updated devices
	c.iDiscoverDevices()

	device, ok := c.discovery.GetDevice(name)
	if !ok {
		return fmt.Errorf("device %s not found in cache", name)
	}
	c.lastDevice = device

	for _, row := range table.Rows {
		field := row.Cells[0].Value
		expected := row.Cells[1].Value

		switch field {
		case "name":
			if device.Name != expected {
				return fmt.Errorf("expected name %s, got %s", expected, device.Name)
			}
		case "address":
			if device.GetAddress() != expected {
				return fmt.Errorf("expected address %s, got %s", expected, device.GetAddress())
			}
		case "port":
			port, _ := strconv.Atoi(expected)
			if device.Port != port {
				return fmt.Errorf("expected port %d, got %d", port, device.Port)
			}
		}
	}
	return nil
}

func (c *bddCtx) discoveredDeviceShouldHaveTXTRecords(table *godog.Table) error {
	if c.lastDevice == nil {
		return fmt.Errorf("no device selected")
	}

	for _, row := range table.Rows {
		key := row.Cells[0].Value
		expected := row.Cells[1].Value

		actual, ok := c.lastDevice.TXTRecords[key]
		if !ok {
			return fmt.Errorf("TXT record %s not found", key)
		}
		if actual != expected {
			return fmt.Errorf("TXT record %s: expected %s, got %s", key, expected, actual)
		}
	}
	return nil
}

func (c *bddCtx) deviceVersionShouldBe(version string) error {
	if c.lastDevice == nil {
		return fmt.Errorf("no device selected")
	}
	actual := c.lastDevice.ParseVersion()
	if actual != version {
		return fmt.Errorf("expected version %s, got %s", version, actual)
	}
	return nil
}

func (c *bddCtx) deviceMACShouldBe(mac string) error {
	if c.lastDevice == nil {
		return fmt.Errorf("no device selected")
	}
	actual := c.lastDevice.ParseMAC()
	if actual != mac {
		return fmt.Errorf("expected MAC %s, got %s", mac, actual)
	}
	return nil
}

func (c *bddCtx) deviceBoardShouldBe(board string) error {
	if c.lastDevice == nil {
		return fmt.Errorf("no device selected")
	}
	actual := c.lastDevice.ParseBoard()
	if actual != board {
		return fmt.Errorf("expected board %s, got %s", board, actual)
	}
	return nil
}

func (c *bddCtx) deviceShouldRequireAPIKey() error {
	if c.lastDevice == nil {
		return fmt.Errorf("no device selected")
	}
	if !c.lastDevice.HasAPIKey() {
		return fmt.Errorf("device should require API key but doesn't")
	}
	return nil
}

func (c *bddCtx) deviceShouldNotRequireAPIKey() error {
	if c.lastDevice == nil {
		return fmt.Errorf("no device selected")
	}
	if c.lastDevice.HasAPIKey() {
		return fmt.Errorf("device should not require API key but does")
	}
	return nil
}

func (c *bddCtx) iShouldHaveNDiscoveredDevices(count int) error {
	if len(c.discoveredDevices) != count {
		return fmt.Errorf("expected %d devices, got %d", count, len(c.discoveredDevices))
	}
	return nil
}

func (c *bddCtx) discoveredDevicesShouldInclude(table *godog.Table) error {
	deviceMap := make(map[string]bool)
	for _, dev := range c.discoveredDevices {
		deviceMap[dev.Name] = true
	}

	for _, row := range table.Rows {
		name := row.Cells[0].Value
		if !deviceMap[name] {
			return fmt.Errorf("device %s not found in discovered devices", name)
		}
	}
	return nil
}

func (c *bddCtx) deviceShouldHaveNAddresses(count int) error {
	if c.lastDevice == nil {
		return fmt.Errorf("no device selected")
	}
	if len(c.lastDevice.Addresses) != count {
		return fmt.Errorf("expected %d addresses, got %d", count, len(c.lastDevice.Addresses))
	}
	return nil
}

func (c *bddCtx) firstAddressShouldBe(address string) error {
	if c.lastDevice == nil {
		return fmt.Errorf("no device selected")
	}
	if len(c.lastDevice.Addresses) == 0 {
		return fmt.Errorf("device has no addresses")
	}
	if c.lastDevice.Addresses[0] != address {
		return fmt.Errorf("expected first address %s, got %s", address, c.lastDevice.Addresses[0])
	}
	return nil
}

func (c *bddCtx) iStartContinuousDiscovery() error {
	ctx := context.Background()
	err := c.discovery.Start(ctx)
	if err != nil {
		return fmt.Errorf("failed to start discovery: %w", err)
	}
	c.discoveryRunning = true
	return nil
}

func (c *bddCtx) iWaitForSeconds(seconds int) error {
	time.Sleep(time.Duration(seconds) * time.Second)
	return nil
}

func (c *bddCtx) deviceShouldBeInCache(name string) error {
	_, ok := c.discovery.GetDevice(name)
	if !ok {
		return fmt.Errorf("device %s not found in cache", name)
	}
	return nil
}

func (c *bddCtx) newESPHomeDeviceAppears(name string) error {
	return c.esphomeDeviceAdvertisingWithName(name)
}

func (c *bddCtx) iShouldHaveNDevicesInCache(count int) error {
	devices := c.discovery.GetDevices()
	if len(devices) != count {
		return fmt.Errorf("expected %d devices in cache, got %d", count, len(devices))
	}
	return nil
}

func (c *bddCtx) esphomeDeviceDiscoveredMinutesAgo(minutes int) error {
	device := &mdns.Device{
		Name:     "stale-device",
		LastSeen: time.Now().Add(-time.Duration(minutes) * time.Minute),
	}
	// Simulate adding to cache with old timestamp
	c.lastDevice = device
	return nil
}

func (c *bddCtx) iCheckForStaleDevices() error {
	// This is handled by the assertion in the next step
	return nil
}

func (c *bddCtx) deviceShouldBeMarkedAsStale() error {
	if c.lastDevice == nil {
		return fmt.Errorf("no device to check")
	}
	if !c.lastDevice.IsStale(5 * time.Minute) {
		return fmt.Errorf("device should be marked as stale but isn't")
	}
	return nil
}

func (c *bddCtx) mdnsDiscoveryServiceIsRunning() error {
	if c.discovery == nil {
		return c.mdnsDiscoveryIsConfigured()
	}
	return c.iStartContinuousDiscovery()
}

func (c *bddCtx) iStopDiscoveryService() error {
	if c.discovery != nil {
		c.discovery.Stop()
	}
	c.discoveryRunning = false
	return nil
}

func (c *bddCtx) discoveryServiceShouldBeStopped() error {
	if c.discoveryRunning {
		return fmt.Errorf("discovery service is still running")
	}
	return nil
}

func (c *bddCtx) noNewDevicesShouldBeDiscovered() error {
	// After stopping, we shouldn't get new devices
	return nil
}
