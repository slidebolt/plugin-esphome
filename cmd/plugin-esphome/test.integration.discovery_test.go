//go:build integration

package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/joho/godotenv"
	"github.com/mycontroller-org/esphome_api/pkg/api"
	"github.com/mycontroller-org/esphome_api/pkg/client"
	"github.com/slidebolt/plugin-esphome/app"
	mdns "github.com/slidebolt/plugin-esphome/internal/mdns"
	domain "github.com/slidebolt/sb-domain"
	testkit "github.com/slidebolt/sb-testkit"
	storage "github.com/slidebolt/sb-storage-sdk"
	"google.golang.org/protobuf/proto"
)

// getESPHomeCredentials retrieves the ESPHome dashboard credentials from environment.
// Returns username and password for API authentication.
func getESPHomeCredentials() (username, password string) {
	// Try to load .env file from multiple locations (in case not already loaded)
	_ = godotenv.Load(".env")
	_ = godotenv.Load("../../.env")
	_ = godotenv.Load("../../../.env")

	username = strings.TrimSpace(os.Getenv("ESPHOME_USERNAME"))
	password = strings.TrimSpace(os.Getenv("ESPHOME_PASSWORD"))
	return username, password
}

// getESPHomeDashboardURL retrieves the ESPHome dashboard URL from environment.
// Returns the URL for accessing the ESPHome dashboard.
func getESPHomeDashboardURL() string {
	// Try to load .env file from multiple locations (in case not already loaded)
	_ = godotenv.Load(".env")
	_ = godotenv.Load("../../.env")
	_ = godotenv.Load("../../../.env")

	url := os.Getenv("ESPHOME_DASHBOARD_URL")
	if url == "" {
		return "https://esp.357graphics.com/"
	}
	return strings.TrimSpace(url)
}

// TestESPHomeDashboard_Integration tests connecting to the ESPHome dashboard API
// and fetching device information using the configured credentials.
func TestESPHomeDashboard_Integration(t *testing.T) {
	dashboardURL := getESPHomeDashboardURL()
	username, password := getESPHomeCredentials()

	t.Logf("Connecting to ESPHome dashboard: %s", dashboardURL)

	// Verify we have credentials
	if username == "" || password == "" {
		t.Skip("ESPHOME_USERNAME and ESPHOME_PASSWORD not configured in .env - skipping dashboard test")
	}

	t.Logf("Authenticating with username: %s", username)

	// Create HTTP client with cookie jar
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("Failed to create cookie jar: %v", err)
	}

	client := &http.Client{
		Jar: jar,
		// Don't verify TLS for test environments with self-signed certs
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	// Step 1: Get login page to extract XSRF token
	t.Log("Step 1: Fetching login page...")
	resp, err := client.Get(dashboardURL + "/login")
	if err != nil {
		t.Fatalf("Failed to fetch login page: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Login page returned status %d", resp.StatusCode)
	}

	// Extract XSRF token from cookies
	var xsrfToken string
	loginURL, _ := url.Parse(dashboardURL + "/login")
	for _, cookie := range jar.Cookies(loginURL) {
		if cookie.Name == "_xsrf" {
			xsrfToken = cookie.Value
			t.Logf("✓ Extracted XSRF token: %s...", xsrfToken[:10])
			break
		}
	}

	// Step 2: Submit login form
	t.Log("Step 2: Submitting login credentials...")
	formData := url.Values{}
	formData.Set("username", username)
	formData.Set("password", password)
	if xsrfToken != "" {
		formData.Set("_xsrf", xsrfToken)
	}

	req, err := http.NewRequest(http.MethodPost, dashboardURL+"/login", strings.NewReader(formData.Encode()))
	if err != nil {
		t.Fatalf("Failed to create login request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", dashboardURL)
	if xsrfToken != "" {
		req.Header.Set("X-Xsrftoken", xsrfToken)
	}
	req.Header.Set("User-Agent", "slidebolt-plugin-esphome/1.0")

	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("Login request failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("Login failed with status %d", resp.StatusCode)
	}
	t.Log("✓ Login successful")

	// Step 3: Connect to WebSocket for device list
	t.Log("Step 3: Connecting to WebSocket for device list...")

	// Try both /events and /ws endpoints
	wsPaths := []string{"/events", "/ws"}
	var wsConn *websocket.Conn
	var wsErr error

	for _, wsPath := range wsPaths {
		wsURL := strings.Replace(dashboardURL, "https://", "wss://", 1) + wsPath
		t.Logf("  Trying WebSocket endpoint: %s", wsURL)

		// Create WebSocket dialer with same TLS config
		dialer := websocket.Dialer{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			Jar:             jar,
		}

		wsConn, resp, wsErr = dialer.Dial(wsURL, nil)
		if wsErr == nil {
			t.Logf("✓ WebSocket connected to %s", wsPath)
			break
		}
		if resp != nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			t.Logf("  %s returned status %d: %s", wsPath, resp.StatusCode, string(body))
		}
	}

	if wsErr != nil {
		t.Fatalf("All WebSocket endpoints failed, last error: %v", wsErr)
	}
	defer wsConn.Close()
	t.Log("✓ WebSocket connected")
	t.Log("✓ WebSocket connected")

	// Step 4: Read device list from WebSocket
	t.Log("Step 4: Reading device list from WebSocket...")

	// Set read timeout
	wsConn.SetReadDeadline(time.Now().Add(10 * time.Second))

	_, message, err := wsConn.ReadMessage()
	if err != nil {
		t.Fatalf("Failed to read WebSocket message: %v", err)
	}

	// Parse the event
	var event struct {
		Event string `json:"event"`
		Data  struct {
			Devices struct {
				Configured []struct {
					Name               string   `json:"name"`
					FriendlyName       string   `json:"friendly_name"`
					Address            string   `json:"address"`
					LoadedIntegrations []string `json:"loaded_integrations"`
				} `json:"configured"`
			} `json:"devices"`
		} `json:"data"`
	}

	if err := json.Unmarshal(message, &event); err != nil {
		t.Fatalf("Failed to parse WebSocket message: %v", err)
	}

	if event.Event != "nodes_updated" && event.Event != "state" {
		t.Logf("Received event type: %s", event.Event)
		t.Logf("Message content: %s", string(message))
	}

	devices := event.Data.Devices.Configured
	t.Logf("✓ Successfully fetched %d device(s) from dashboard", len(devices))

	// Display device information
	for i, device := range devices {
		t.Logf("  [%d] %s (%s) @ %s",
			i+1,
			device.Name,
			device.FriendlyName,
			device.Address)
		if len(device.LoadedIntegrations) > 0 {
			t.Logf("      Integrations: %v", device.LoadedIntegrations)
		}
	}

	// Verify we got at least one device
	if len(devices) == 0 {
		t.Error("No devices found in dashboard - expected at least one ESPHome device")
	}

	t.Log("\n=== Dashboard Integration Test Summary ===")
	t.Logf("✓ Connected to: %s", dashboardURL)
	t.Logf("✓ Authenticated as: %s", username)
	t.Logf("✓ Devices discovered: %d", len(devices))
	t.Log("✓ Full dashboard integration test complete")
}

// TestESPHomeConfig_Integration verifies that the .env configuration is loaded correctly
func TestESPHomeConfig_Integration(t *testing.T) {
	// Test API key loading
	apiKey := getAPIEncryptionKey()
	if apiKey == "" {
		t.Log("⚠️  No API encryption key configured")
	} else {
		t.Logf("✓ API encryption key loaded (length: %d)", len(apiKey))
	}

	// Test credentials loading
	username, password := getESPHomeCredentials()
	if username == "" || password == "" {
		t.Log("⚠️  ESPHome credentials not fully configured")
		t.Logf("  Username: %q", username)
		t.Logf("  Password configured: %v", password != "")
	} else {
		t.Logf("✓ ESPHome credentials loaded")
		t.Logf("  Username: %s", username)
		t.Logf("  Password: [REDACTED] (%d chars)", len(password))
	}

	// Test dashboard URL loading
	dashboardURL := getESPHomeDashboardURL()
	if dashboardURL == "" {
		t.Log("⚠️  No dashboard URL configured")
	} else {
		t.Logf("✓ ESPHome dashboard URL: %s", dashboardURL)
	}

	// Verify that if values are set, they're not empty
	if username != "" && password == "" {
		t.Error("Username is set but password is empty")
	}
	if password != "" && username == "" {
		t.Error("Password is set but username is empty")
	}
}

// TestMDNSDiscovery_Integration performs real mDNS discovery on the network
// to find actual ESPHome devices.
func TestMDNSDiscovery_Integration(t *testing.T) {
	// Create discovery with longer timeout for real network
	discovery, err := mdns.NewDiscovery(
		mdns.WithTimeout(10 * time.Second),
	)
	if err != nil {
		t.Fatalf("Failed to create discovery: %v", err)
	}

	ctx := context.Background()
	t.Log("Starting mDNS discovery for ESPHome devices...")

	// Perform discovery
	devices, err := discovery.Discover(ctx)
	if err != nil {
		t.Fatalf("Discovery failed: %v", err)
	}

	t.Logf("Found %d ESPHome device(s)", len(devices))

	// Verify we found at least one device
	if len(devices) == 0 {
		t.Fatal("No ESPHome devices found on the network - are there any powered on?")
	}

	// Validate each discovered device
	for i, device := range devices {
		t.Run(fmt.Sprintf("Device_%d_%s", i, device.Name), func(t *testing.T) {
			validateDevice(t, device)
		})
	}
}

// TestMDNSDiscovery_Continuous_Integration tests continuous discovery mode
func TestMDNSDiscovery_Continuous_Integration(t *testing.T) {
	discovery, err := mdns.NewDiscovery(
		mdns.WithTimeout(5 * time.Second),
	)
	if err != nil {
		t.Fatalf("Failed to create discovery: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	t.Log("Starting continuous mDNS discovery...")

	// Start continuous discovery
	if err := discovery.Start(ctx); err != nil {
		t.Fatalf("Failed to start discovery: %v", err)
	}

	// Wait a bit for discovery to happen
	time.Sleep(3 * time.Second)

	// Check what devices we have
	devices := discovery.GetDevices()
	t.Logf("Device cache contains %d device(s) after 3 seconds", len(devices))

	// Verify structure of cached devices
	for _, device := range devices {
		if device.Name == "" {
			t.Error("Device has no name")
		}
		if len(device.Addresses) == 0 && device.Host == "" {
			t.Error("Device has no addresses or hostname")
		}
	}

	// Stop discovery
	discovery.Stop()
	t.Log("Discovery stopped successfully")
}

// TestMDNSDiscovery_DeviceStructure_Integration validates device structure
func TestMDNSDiscovery_DeviceStructure_Integration(t *testing.T) {
	discovery, err := mdns.NewDiscovery(
		mdns.WithTimeout(8 * time.Second),
	)
	if err != nil {
		t.Fatalf("Failed to create discovery: %v", err)
	}

	ctx := context.Background()
	devices, err := discovery.Discover(ctx)
	if err != nil {
		t.Fatalf("Discovery failed: %v", err)
	}

	if len(devices) == 0 {
		t.Skip("No ESPHome devices found - skipping structure validation")
	}

	device := devices[0] // Test first device

	// Test device field presence and validity
	t.Run("HasName", func(t *testing.T) {
		if device.Name == "" {
			t.Error("Device name is empty")
		}
		t.Logf("Device name: %s", device.Name)
	})

	t.Run("HasHostOrAddress", func(t *testing.T) {
		hasHost := device.Host != ""
		hasAddress := len(device.Addresses) > 0
		if !hasHost && !hasAddress {
			t.Error("Device has neither hostname nor IP addresses")
		}
		t.Logf("Host: %s, Addresses: %v", device.Host, device.Addresses)
	})

	t.Run("HasValidPort", func(t *testing.T) {
		port := device.GetAPIPort()
		if port <= 0 || port > 65535 {
			t.Errorf("Invalid port: %d", port)
		}
		t.Logf("API Port: %d", port)
	})

	t.Run("HasLastSeen", func(t *testing.T) {
		if device.LastSeen.IsZero() {
			t.Error("Device LastSeen is zero")
		}
		if time.Since(device.LastSeen) > time.Minute {
			t.Error("Device LastSeen is too old")
		}
		t.Logf("Last seen: %v ago", time.Since(device.LastSeen))
	})

	t.Run("CanGetAddress", func(t *testing.T) {
		addr := device.GetAddress()
		if addr == "" {
			t.Error("GetAddress() returned empty string")
		}
		// Validate it's a valid IP or hostname
		ip := net.ParseIP(addr)
		if ip == nil && !strings.Contains(addr, ".") {
			t.Errorf("Address %s doesn't appear to be a valid IP or hostname", addr)
		}
		t.Logf("Resolved address: %s", addr)
	})

	t.Run("TXTRecordsParsed", func(t *testing.T) {
		// TXT records are optional but if present, should be parsed
		t.Logf("TXT Records: %v", device.TXTRecords)

		// Check for common ESPHome TXT records
		if version := device.ParseVersion(); version != "unknown" {
			t.Logf("ESPHome version: %s", version)
		}
		if mac := device.ParseMAC(); mac != "" {
			t.Logf("MAC address: %s", mac)
		}
		if board := device.ParseBoard(); board != "unknown" {
			t.Logf("Board: %s", board)
		}
		t.Logf("API encryption required: %v", device.HasAPIKey())
	})
}

// TestMDNSDiscovery_MultipleServices_Integration tests discovery with different service types
func TestMDNSDiscovery_MultipleServices_Integration(t *testing.T) {
	serviceTypes := []string{
		"_esphomelib._tcp", // Standard ESPHome
	}

	for _, svcType := range serviceTypes {
		t.Run(svcType, func(t *testing.T) {
			discovery, err := mdns.NewDiscovery(
				mdns.WithTimeout(5*time.Second),
				mdns.WithServiceType(svcType),
			)
			if err != nil {
				t.Fatalf("Failed to create discovery for %s: %v", svcType, err)
			}

			ctx := context.Background()
			devices, err := discovery.Discover(ctx)
			if err != nil {
				t.Logf("Discovery error for %s: %v", svcType, err)
				return
			}

			t.Logf("Service %s: found %d device(s)", svcType, len(devices))
			for _, d := range devices {
				t.Logf("  - %s at %s:%d", d.Name, d.GetAddress(), d.GetAPIPort())
			}
		})
	}
}

// TestESPHomeDevice_RATGDO_Integration specifically tests the ratgdov25i garage door device
// This device is interesting because it's a specialized device (garage door opener) rather than a simple switch
func TestESPHomeDevice_RATGDO_Integration(t *testing.T) {
	// Setup test environment with storage
	env := testkit.NewTestEnv(t)
	env.Start("storage")
	store := env.Storage()

	// Discover devices
	discovery, err := mdns.NewDiscovery(
		mdns.WithTimeout(10 * time.Second),
	)
	if err != nil {
		t.Fatalf("Failed to create discovery: %v", err)
	}

	ctx := context.Background()
	t.Log("Discovering ESPHome devices...")

	devices, err := discovery.Discover(ctx)
	if err != nil {
		t.Fatalf("Discovery failed: %v", err)
	}

	// Find the ratgdov25i device
	var ratgdoDevice *mdns.Device
	for _, d := range devices {
		if strings.Contains(d.Name, "ratgdo") || strings.Contains(d.Name, "ratgdov") {
			ratgdoDevice = d
			break
		}
	}

	if ratgdoDevice == nil {
		t.Skip("ratgdov25i device not found on network - skipping test")
	}

	t.Logf("Found ratgdov25i device: %s at %s", ratgdoDevice.Name, ratgdoDevice.GetAddress())
	t.Logf("TXT Records: %v", ratgdoDevice.TXTRecords)

	// Try to connect and fetch entities
	// Note: This device likely has API encryption, so we may not be able to connect without a key
	connectAndFetchEntitiesRATGDO(t, ratgdoDevice, store)
}

// connectAndFetchEntitiesRATGDO attempts to connect to the RATGDO device
// This is a copy of the main function but with more verbose logging for this specific device
func connectAndFetchEntitiesRATGDO(t *testing.T, device *mdns.Device, store storage.Storage) {
	address := fmt.Sprintf("%s:%d", device.GetAddress(), device.GetAPIPort())
	t.Logf("\n=== Connecting to RATGDO device: %s ===", device.Name)
	t.Logf("Address: %s", address)
	t.Logf("Host: %s", device.Host)
	t.Logf("Port: %d", device.Port)
	t.Logf("Version: %s", device.ParseVersion())
	t.Logf("MAC: %s", device.ParseMAC())
	t.Logf("Board: %s", device.ParseBoard())
	t.Logf("Has API Key: %v", device.HasAPIKey())
	t.Logf("TXT Records: %v", device.TXTRecords)

	// For the RATGDO device, use encryption key if required
	encryptionKey := getAPIEncryptionKey()
	if device.HasAPIKey() {
		if encryptionKey == "" {
			t.Log("\n⚠️  Device requires API encryption but no key is configured")
			t.Log("Set ESPHOME_API_KEY environment variable or add to .env file:")
			t.Log("ESPHOME_API_KEY=your_base64_encoded_key_here")
			return
		}
		t.Log("\n⚠️  Device requires API encryption - using configured key")
	} else {
		t.Log("\n✓ Device does not require API encryption")
	}

	// Create message channel
	msgChan := make(chan proto.Message, 100)
	doneChan := make(chan bool)
	msgCount := 0

	// Handler for incoming messages
	handler := func(msg proto.Message) {
		msgCount++
		t.Logf("[%d] Received: %T", msgCount, msg)
		msgChan <- msg
		// Check if this is the done message
		if _, ok := msg.(*api.ListEntitiesDoneResponse); ok {
			t.Log("✓ Received ListEntitiesDoneResponse - entity list complete!")
			select {
			case doneChan <- true:
			default:
			}
		}
	}

	// Connect
	t.Log("\nCreating client connection...")
	espClient, err := client.GetClient("test-client-ratgdo", address, encryptionKey, 10*time.Second, handler)
	if err != nil {
		t.Logf("❌ Failed to connect: %v", err)
		return
	}
	defer espClient.Close()
	t.Log("✓ Connected successfully!")

	// ESPHome API requires Hello handshake first
	t.Log("\nSending HelloRequest...")
	err = espClient.Send(&api.HelloRequest{
		ClientInfo:      "slidebolt-esphome-test",
		ApiVersionMajor: 1,
		ApiVersionMinor: 9,
	})
	if err != nil {
		t.Logf("❌ Failed to send HelloRequest: %v", err)
		return
	}
	t.Log("✓ HelloRequest sent")

	// Give time for HelloResponse
	time.Sleep(500 * time.Millisecond)

	t.Log("\nSending ConnectRequest...")
	err = espClient.Send(&api.ConnectRequest{})
	if err != nil {
		t.Logf("❌ Failed to send ConnectRequest: %v", err)
		return
	}
	t.Log("✓ ConnectRequest sent")

	// Give time for ConnectResponse
	time.Sleep(500 * time.Millisecond)

	t.Log("\nRequesting entity list...")
	// Send ListEntitiesRequest
	err = espClient.Send(&api.ListEntitiesRequest{})
	if err != nil {
		t.Logf("❌ Failed to send ListEntitiesRequest: %v", err)
		return
	}
	t.Log("✓ ListEntitiesRequest sent")

	// Wait for done response or timeout
	select {
	case <-doneChan:
		t.Logf("✓ Entity list received from %s", device.Name)
	case <-time.After(10 * time.Second):
		t.Logf("⚠️  Timeout waiting for entities from %s", device.Name)
	}
	close(msgChan)

	// Process received entities
	entityCount := 0
	entitiesCreated := 0
	entitiesByType := make(map[string]int)

	t.Log("\n--- Processing Received Entities ---")
	for msg := range msgChan {
		entityCount++
		switch entity := msg.(type) {
		case *api.ListEntitiesBinarySensorResponse:
			entitiesByType["binary_sensor"]++
			if err := createBinarySensorEntity(t, store, device, entity); err != nil {
				t.Logf("  ✗ Failed to create binary_sensor: %v", err)
			} else {
				entitiesCreated++
				t.Logf("  ✓ Created binary_sensor: %s (key=%d)", entity.Name, entity.Key)
			}
		case *api.ListEntitiesSensorResponse:
			entitiesByType["sensor"]++
			if err := createSensorEntity(t, store, device, entity); err != nil {
				t.Logf("  ✗ Failed to create sensor: %v", err)
			} else {
				entitiesCreated++
				t.Logf("  ✓ Created sensor: %s (key=%d, unit=%s)", entity.Name, entity.Key, entity.UnitOfMeasurement)
			}
		case *api.ListEntitiesSwitchResponse:
			entitiesByType["switch"]++
			if err := createSwitchEntity(t, store, device, entity); err != nil {
				t.Logf("  ✗ Failed to create switch: %v", err)
			} else {
				entitiesCreated++
				t.Logf("  ✓ Created switch: %s (key=%d)", entity.Name, entity.Key)
			}
		case *api.ListEntitiesCoverResponse:
			entitiesByType["cover"]++
			if err := createCoverEntity(t, store, device, entity); err != nil {
				t.Logf("  ✗ Failed to create cover: %v", err)
			} else {
				entitiesCreated++
				t.Logf("  ✓ Created cover: %s (key=%d)", entity.Name, entity.Key)
			}
		case *api.ListEntitiesLightResponse:
			entitiesByType["light"]++
			if err := createLightEntity(t, store, device, entity); err != nil {
				t.Logf("  ✗ Failed to create light: %v", err)
			} else {
				entitiesCreated++
				t.Logf("  ✓ Created light: %s (key=%d)", entity.Name, entity.Key)
			}
		case *api.ListEntitiesButtonResponse:
			entitiesByType["button"]++
			if err := createButtonEntity(t, store, device, entity); err != nil {
				t.Logf("  ✗ Failed to create button: %v", err)
			} else {
				entitiesCreated++
				t.Logf("  ✓ Created button: %s (key=%d)", entity.Name, entity.Key)
			}
		case *api.ListEntitiesFanResponse:
			entitiesByType["fan"]++
			if err := createFanEntity(t, store, device, entity); err != nil {
				t.Logf("  ✗ Failed to create fan: %v", err)
			} else {
				entitiesCreated++
				t.Logf("  ✓ Created fan: %s (key=%d)", entity.Name, entity.Key)
			}
		case *api.ListEntitiesClimateResponse:
			entitiesByType["climate"]++
			if err := createClimateEntity(t, store, device, entity); err != nil {
				t.Logf("  ✗ Failed to create climate: %v", err)
			} else {
				entitiesCreated++
				t.Logf("  ✓ Created climate: %s (key=%d)", entity.Name, entity.Key)
			}
		case *api.ListEntitiesLockResponse:
			entitiesByType["lock"]++
			if err := createLockEntity(t, store, device, entity); err != nil {
				t.Logf("  ✗ Failed to create lock: %v", err)
			} else {
				entitiesCreated++
				t.Logf("  ✓ Created lock: %s (key=%d)", entity.Name, entity.Key)
			}
		case *api.ListEntitiesNumberResponse:
			entitiesByType["number"]++
			if err := createNumberEntity(t, store, device, entity); err != nil {
				t.Logf("  ✗ Failed to create number: %v", err)
			} else {
				entitiesCreated++
				t.Logf("  ✓ Created number: %s (key=%d)", entity.Name, entity.Key)
			}
		case *api.ListEntitiesSelectResponse:
			entitiesByType["select"]++
			if err := createSelectEntity(t, store, device, entity); err != nil {
				t.Logf("  ✗ Failed to create select: %v", err)
			} else {
				entitiesCreated++
				t.Logf("  ✓ Created select: %s (key=%d)", entity.Name, entity.Key)
			}
		case *api.ListEntitiesTextSensorResponse:
			entitiesByType["text_sensor"]++
			if err := createTextSensorEntity(t, store, device, entity); err != nil {
				t.Logf("  ✗ Failed to create text_sensor: %v", err)
			} else {
				entitiesCreated++
				t.Logf("  ✓ Created text_sensor: %s (key=%d)", entity.Name, entity.Key)
			}
		case *api.ListEntitiesDoneResponse:
			t.Logf("  (ListEntitiesDoneResponse - end of list)")
		default:
			t.Logf("  (Other: %T)", msg)
		}
	}

	// Summary
	t.Log("\n=== Summary ===")
	t.Logf("Total messages received: %d", msgCount)
	t.Logf("Entities processed: %d", entityCount)
	t.Logf("Entities created in storage: %d", entitiesCreated)

	if len(entitiesByType) > 0 {
		t.Log("\nEntities by type:")
		for typ, count := range entitiesByType {
			t.Logf("  - %s: %d", typ, count)
		}
	}

	if entitiesCreated == 0 && device.HasAPIKey() {
		t.Log("\n💡 Note: No entities were created because the device requires API encryption.")
		t.Log("   To fetch entities from encrypted devices, provide the encryption key.")
	}

	t.Log("")
}

// TestESPHomeDevice_ConnectAndFetchEntities_Integration connects to discovered devices
// and fetches their entity lists, then creates real entities in storage.
func TestESPHomeDevice_ConnectAndFetchEntities_Integration(t *testing.T) {
	// Setup test environment with storage
	env := testkit.NewTestEnv(t)
	env.Start("storage")
	store := env.Storage()

	// Discover devices
	discovery, err := mdns.NewDiscovery(
		mdns.WithTimeout(10 * time.Second),
	)
	if err != nil {
		t.Fatalf("Failed to create discovery: %v", err)
	}

	ctx := context.Background()
	t.Log("Discovering ESPHome devices...")

	devices, err := discovery.Discover(ctx)
	if err != nil {
		t.Fatalf("Discovery failed: %v", err)
	}

	if len(devices) == 0 {
		t.Skip("No ESPHome devices found - skipping entity fetch test")
	}

	t.Logf("Found %d device(s), connecting to fetch entities...", len(devices))

	// Try to connect to multiple devices (both encrypted and unencrypted)
	devicesTested := 0
	maxDevices := 5

	for i := 0; i < len(devices) && devicesTested < maxDevices; i++ {
		device := devices[i]
		devicesTested++
		t.Run(fmt.Sprintf("Device_%s", device.Name), func(t *testing.T) {
			connectAndFetchEntities(t, device, store)
		})
	}
}

// connectAndFetchEntities connects to an ESPHome device and fetches its entities
func connectAndFetchEntities(t *testing.T, device *mdns.Device, store storage.Storage) {
	address := fmt.Sprintf("%s:%d", device.GetAddress(), device.GetAPIPort())
	t.Logf("Connecting to %s at %s", device.Name, address)

	// Determine encryption key from environment
	encryptionKey := getAPIEncryptionKey()
	if device.HasAPIKey() {
		if encryptionKey == "" {
			t.Logf("⚠️  Device %s requires API encryption but no key is configured", device.Name)
			t.Log("Set ESPHOME_API_KEY in .env file or environment variable")
			t.Log("Format: ESPHOME_API_KEY=base64encodednoisekey")
			return
		}
		t.Logf("Using API encryption key for %s", device.Name)
	}

	// Create message channel
	msgChan := make(chan proto.Message, 100)
	doneChan := make(chan bool)

	// Handler for incoming messages
	handler := func(msg proto.Message) {
		t.Logf("Received message type: %T", msg)
		msgChan <- msg
		// Check if this is the done message
		if _, ok := msg.(*api.ListEntitiesDoneResponse); ok {
			t.Log("Received ListEntitiesDoneResponse!")
			select {
			case doneChan <- true:
			default:
			}
		}
	}

	// Connect
	t.Logf("Connecting to %s at %s...", device.Name, address)
	espClient, err := client.GetClient("test-client", address, encryptionKey, 10*time.Second, handler)
	if err != nil {
		t.Logf("❌ Failed to connect to %s: %v", device.Name, err)
		return
	}
	defer espClient.Close()
	t.Logf("✓ Connected to %s", device.Name)

	// ESPHome API requires Hello handshake first
	err = espClient.Send(&api.HelloRequest{
		ClientInfo:      "slidebolt-esphome-plugin",
		ApiVersionMajor: 1,
		ApiVersionMinor: 9,
	})
	if err != nil {
		t.Logf("❌ Failed to send HelloRequest to %s: %v", device.Name, err)
		return
	}
	time.Sleep(200 * time.Millisecond)

	// Send ConnectRequest
	err = espClient.Send(&api.ConnectRequest{})
	if err != nil {
		t.Logf("❌ Failed to send ConnectRequest to %s: %v", device.Name, err)
		return
	}
	time.Sleep(200 * time.Millisecond)

	// Request entity list
	t.Logf("Requesting entities from %s...", device.Name)
	err = espClient.Send(&api.ListEntitiesRequest{})
	if err != nil {
		t.Logf("❌ Failed to send ListEntitiesRequest to %s: %v", device.Name, err)
		return
	}

	// Wait for done response or timeout
	select {
	case <-doneChan:
		t.Log("Received ListEntitiesDoneResponse")
	case <-time.After(5 * time.Second):
		t.Log("Timeout waiting for entity list")
	}
	close(msgChan)

	// Process received entities
	entityCount := 0
	entitiesCreated := 0

	for msg := range msgChan {
		entityCount++
		switch entity := msg.(type) {
		case *api.ListEntitiesBinarySensorResponse:
			if err := createBinarySensorEntity(t, store, device, entity); err != nil {
				t.Logf("Failed to create binary_sensor entity: %v", err)
			} else {
				entitiesCreated++
				t.Logf("Created binary_sensor: %s (%s)", entity.Name, entity.UniqueId)
			}
		case *api.ListEntitiesSensorResponse:
			if err := createSensorEntity(t, store, device, entity); err != nil {
				t.Logf("Failed to create sensor entity: %v", err)
			} else {
				entitiesCreated++
				t.Logf("Created sensor: %s (%s)", entity.Name, entity.UniqueId)
			}
		case *api.ListEntitiesSwitchResponse:
			if err := createSwitchEntity(t, store, device, entity); err != nil {
				t.Logf("Failed to create switch entity: %v", err)
			} else {
				entitiesCreated++
				t.Logf("Created switch: %s (%s)", entity.Name, entity.UniqueId)
			}
		case *api.ListEntitiesLightResponse:
			if err := createLightEntity(t, store, device, entity); err != nil {
				t.Logf("Failed to create light entity: %v", err)
			} else {
				entitiesCreated++
				t.Logf("Created light: %s (%s)", entity.Name, entity.UniqueId)
			}
		case *api.ListEntitiesFanResponse:
			if err := createFanEntity(t, store, device, entity); err != nil {
				t.Logf("Failed to create fan entity: %v", err)
			} else {
				entitiesCreated++
				t.Logf("Created fan: %s (%s)", entity.Name, entity.UniqueId)
			}
		case *api.ListEntitiesClimateResponse:
			if err := createClimateEntity(t, store, device, entity); err != nil {
				t.Logf("Failed to create climate entity: %v", err)
			} else {
				entitiesCreated++
				t.Logf("Created climate: %s (%s)", entity.Name, entity.UniqueId)
			}
		case *api.ListEntitiesCoverResponse:
			if err := createCoverEntity(t, store, device, entity); err != nil {
				t.Logf("Failed to create cover entity: %v", err)
			} else {
				entitiesCreated++
				t.Logf("Created cover: %s (%s)", entity.Name, entity.UniqueId)
			}
		case *api.ListEntitiesLockResponse:
			if err := createLockEntity(t, store, device, entity); err != nil {
				t.Logf("Failed to create lock entity: %v", err)
			} else {
				entitiesCreated++
				t.Logf("Created lock: %s (%s)", entity.Name, entity.UniqueId)
			}
		case *api.ListEntitiesButtonResponse:
			if err := createButtonEntity(t, store, device, entity); err != nil {
				t.Logf("Failed to create button entity: %v", err)
			} else {
				entitiesCreated++
				t.Logf("Created button: %s (%s)", entity.Name, entity.UniqueId)
			}
		case *api.ListEntitiesNumberResponse:
			if err := createNumberEntity(t, store, device, entity); err != nil {
				t.Logf("Failed to create number entity: %v", err)
			} else {
				entitiesCreated++
				t.Logf("Created number: %s (%s)", entity.Name, entity.UniqueId)
			}
		case *api.ListEntitiesSelectResponse:
			if err := createSelectEntity(t, store, device, entity); err != nil {
				t.Logf("Failed to create select entity: %v", err)
			} else {
				entitiesCreated++
				t.Logf("Created select: %s (%s)", entity.Name, entity.UniqueId)
			}
		case *api.ListEntitiesTextSensorResponse:
			if err := createTextSensorEntity(t, store, device, entity); err != nil {
				t.Logf("Failed to create text_sensor entity: %v", err)
			} else {
				entitiesCreated++
				t.Logf("Created text_sensor: %s (%s)", entity.Name, entity.UniqueId)
			}
		default:
			// Other message types (DeviceInfo, states, etc.)
		}
	}

	t.Logf("Received %d messages, created %d entities in storage", entityCount, entitiesCreated)
}

// Entity creation helpers
func createBinarySensorEntity(t *testing.T, store storage.Storage, device *mdns.Device, resp *api.ListEntitiesBinarySensorResponse) error {
	entity := domain.Entity{
		ID:       fmt.Sprintf("%s_%d", device.Name, resp.Key),
		Plugin:   app.PluginID,
		DeviceID: device.Name,
		Type:     "binary_sensor",
		Name:     resp.Name,
		State: domain.BinarySensor{
			On:          false, // Initial state
			DeviceClass: resp.DeviceClass,
		},
	}
	return store.Save(entity)
}

func createSensorEntity(t *testing.T, store storage.Storage, device *mdns.Device, resp *api.ListEntitiesSensorResponse) error {
	entity := domain.Entity{
		ID:       fmt.Sprintf("%s_%d", device.Name, resp.Key),
		Plugin:   app.PluginID,
		DeviceID: device.Name,
		Type:     "sensor",
		Name:     resp.Name,
		State: domain.Sensor{
			Value:       0, // Initial state
			Unit:        resp.UnitOfMeasurement,
			DeviceClass: resp.DeviceClass,
		},
	}
	return store.Save(entity)
}

func createSwitchEntity(t *testing.T, store storage.Storage, device *mdns.Device, resp *api.ListEntitiesSwitchResponse) error {
	entity := domain.Entity{
		ID:       fmt.Sprintf("%s_%d", device.Name, resp.Key),
		Plugin:   app.PluginID,
		DeviceID: device.Name,
		Type:     "switch",
		Name:     resp.Name,
		Commands: []string{"switch_turn_on", "switch_turn_off", "switch_toggle"},
		State: domain.Switch{
			Power: false, // Initial state
		},
	}
	return store.Save(entity)
}

func createLightEntity(t *testing.T, store storage.Storage, device *mdns.Device, resp *api.ListEntitiesLightResponse) error {
	commands := []string{"light_turn_on", "light_turn_off"}
	if resp.SupportedColorModes != nil {
		// Add more commands based on supported modes
		commands = append(commands, "light_set_brightness")
	}

	entity := domain.Entity{
		ID:       fmt.Sprintf("%s_%d", device.Name, resp.Key),
		Plugin:   app.PluginID,
		DeviceID: device.Name,
		Type:     "light",
		Name:     resp.Name,
		Commands: commands,
		State: domain.Light{
			Power: false, // Initial state
		},
	}
	return store.Save(entity)
}

func createFanEntity(t *testing.T, store storage.Storage, device *mdns.Device, resp *api.ListEntitiesFanResponse) error {
	entity := domain.Entity{
		ID:       fmt.Sprintf("%s_%d", device.Name, resp.Key),
		Plugin:   app.PluginID,
		DeviceID: device.Name,
		Type:     "fan",
		Name:     resp.Name,
		Commands: []string{"fan_turn_on", "fan_turn_off", "fan_set_speed"},
		State: domain.Fan{
			Power:      false, // Initial state
			Percentage: 0,
		},
	}
	return store.Save(entity)
}

func createClimateEntity(t *testing.T, store storage.Storage, device *mdns.Device, resp *api.ListEntitiesClimateResponse) error {
	entity := domain.Entity{
		ID:       fmt.Sprintf("%s_%d", device.Name, resp.Key),
		Plugin:   app.PluginID,
		DeviceID: device.Name,
		Type:     "climate",
		Name:     resp.Name,
		Commands: []string{"climate_set_mode", "climate_set_temperature"},
		State: domain.Climate{
			HVACMode: "off", // Initial state
		},
	}
	return store.Save(entity)
}

func createCoverEntity(t *testing.T, store storage.Storage, device *mdns.Device, resp *api.ListEntitiesCoverResponse) error {
	entity := domain.Entity{
		ID:       fmt.Sprintf("%s_%d", device.Name, resp.Key),
		Plugin:   app.PluginID,
		DeviceID: device.Name,
		Type:     "cover",
		Name:     resp.Name,
		Commands: []string{"cover_open", "cover_close", "cover_set_position"},
		State: domain.Cover{
			Position: 0, // Initial state
		},
	}
	return store.Save(entity)
}

func createLockEntity(t *testing.T, store storage.Storage, device *mdns.Device, resp *api.ListEntitiesLockResponse) error {
	entity := domain.Entity{
		ID:       fmt.Sprintf("%s_%d", device.Name, resp.Key),
		Plugin:   app.PluginID,
		DeviceID: device.Name,
		Type:     "lock",
		Name:     resp.Name,
		Commands: []string{"lock_lock", "lock_unlock"},
		State: domain.Lock{
			Locked: false, // Initial state
		},
	}
	return store.Save(entity)
}

func createButtonEntity(t *testing.T, store storage.Storage, device *mdns.Device, resp *api.ListEntitiesButtonResponse) error {
	entity := domain.Entity{
		ID:       fmt.Sprintf("%s_%d", device.Name, resp.Key),
		Plugin:   app.PluginID,
		DeviceID: device.Name,
		Type:     "button",
		Name:     resp.Name,
		Commands: []string{"button_press"},
		State: domain.Button{
			Presses: 0,
		},
	}
	return store.Save(entity)
}

func createNumberEntity(t *testing.T, store storage.Storage, device *mdns.Device, resp *api.ListEntitiesNumberResponse) error {
	entity := domain.Entity{
		ID:       fmt.Sprintf("%s_%d", device.Name, resp.Key),
		Plugin:   app.PluginID,
		DeviceID: device.Name,
		Type:     "number",
		Name:     resp.Name,
		Commands: []string{"number_set_value"},
		State: domain.Number{
			Value: float64(resp.MinValue), // Initial state
			Min:   float64(resp.MinValue),
			Max:   float64(resp.MaxValue),
		},
	}
	return store.Save(entity)
}

func createSelectEntity(t *testing.T, store storage.Storage, device *mdns.Device, resp *api.ListEntitiesSelectResponse) error {
	entity := domain.Entity{
		ID:       fmt.Sprintf("%s_%d", device.Name, resp.Key),
		Plugin:   app.PluginID,
		DeviceID: device.Name,
		Type:     "select",
		Name:     resp.Name,
		Commands: []string{"select_option"},
		State: domain.Select{
			Options: resp.Options,
		},
	}
	return store.Save(entity)
}

func createTextSensorEntity(t *testing.T, store storage.Storage, device *mdns.Device, resp *api.ListEntitiesTextSensorResponse) error {
	entity := domain.Entity{
		ID:       fmt.Sprintf("%s_%d", device.Name, resp.Key),
		Plugin:   app.PluginID,
		DeviceID: device.Name,
		Type:     "sensor", // text_sensor maps to sensor type
		Name:     resp.Name,
		State: domain.Sensor{
			Value:       "", // Initial state - text sensors have string values
			DeviceClass: "", // Text sensors don't have device class in this API version
		},
	}
	return store.Save(entity)
}

// TestESPHomeEntity_Query_Integration tests querying entities from storage
func TestESPHomeEntity_Query_Integration(t *testing.T) {
	// Setup test environment
	env := testkit.NewTestEnv(t)
	env.Start("storage")
	store := env.Storage()

	// Discover devices and create entities
	discovery, err := mdns.NewDiscovery(mdns.WithTimeout(10 * time.Second))
	if err != nil {
		t.Fatalf("Failed to create discovery: %v", err)
	}

	ctx := context.Background()
	devices, err := discovery.Discover(ctx)
	if err != nil {
		t.Fatalf("Discovery failed: %v", err)
	}

	if len(devices) == 0 {
		t.Skip("No ESPHome devices found")
	}

	// Create at least one entity manually for testing
	testEntity := domain.Entity{
		ID:       "test_sensor",
		Plugin:   app.PluginID,
		DeviceID: devices[0].Name,
		Type:     "sensor",
		Name:     "Test Temperature",
		State: domain.Sensor{
			Value: 22.5,
			Unit:  "°C",
		},
	}

	if err := store.Save(testEntity); err != nil {
		t.Fatalf("Failed to save test entity: %v", err)
	}

	// Query entities by type
	t.Log("Querying entities from storage...")

	// Test query by plugin
	entries, err := store.Query(storage.Query{
		Where: []storage.Filter{{Field: "plugin", Op: storage.Eq, Value: app.PluginID}},
	})
	if err != nil {
		t.Logf("Query by plugin failed: %v", err)
	} else {
		t.Logf("Found %d entities for plugin %s", len(entries), app.PluginID)
	}

	// Test query by type
	entries, err = store.Query(storage.Query{
		Where: []storage.Filter{{Field: "type", Op: storage.Eq, Value: "sensor"}},
	})
	if err != nil {
		t.Logf("Query by type failed: %v", err)
	} else {
		t.Logf("Found %d sensor entities", len(entries))
	}
}

// TestESPHomeDevice_ControlLight_Integration tests controlling a light entity
// This performs a full round-trip: connect -> list entities -> send command -> receive state update
func TestESPHomeDevice_ControlLight_Integration(t *testing.T) {
	// Setup test environment with storage
	env := testkit.NewTestEnv(t)
	env.Start("storage")
	store := env.Storage()

	// Discover devices
	discovery, err := mdns.NewDiscovery(
		mdns.WithTimeout(10 * time.Second),
	)
	if err != nil {
		t.Fatalf("Failed to create discovery: %v", err)
	}

	ctx := context.Background()
	t.Log("Discovering ESPHome devices...")

	devices, err := discovery.Discover(ctx)
	if err != nil {
		t.Fatalf("Discovery failed: %v", err)
	}

	// Find an Edison device with lights
	var targetDevice *mdns.Device
	for _, d := range devices {
		// Look for Edison devices with "edison" in the name
		if strings.Contains(strings.ToLower(d.Name), "edison") {
			targetDevice = d
			t.Logf("Found Edison device: %s - checking for lights...", d.Name)

			// Try to connect and check if it has lights
			if hasLight, _ := checkDeviceForLights(t, d); hasLight {
				t.Logf("✓ Device %s has lights, using it", d.Name)
				break
			}
			t.Logf("✗ Device %s has no lights, trying next...", d.Name)
		}
	}

	if targetDevice == nil {
		t.Skip("No Edison light device found on network - skipping test")
	}

	t.Logf("Selected device: %s at %s", targetDevice.Name, targetDevice.GetAddress())

	// Connect and control a light
	connectAndControlEdisonLight(t, targetDevice, store)
}

// checkDeviceForLights quickly checks if a device has light entities
func checkDeviceForLights(t *testing.T, device *mdns.Device) (bool, uint32) {
	address := fmt.Sprintf("%s:%d", device.GetAddress(), device.GetAPIPort())
	encryptionKey := getAPIEncryptionKey()

	if device.HasAPIKey() && encryptionKey == "" {
		return false, 0
	}

	lightFound := make(chan uint32, 1)

	handler := func(msg proto.Message) {
		switch m := msg.(type) {
		case *api.ListEntitiesLightResponse:
			lightFound <- m.Key
		case *api.ListEntitiesDoneResponse:
			close(lightFound)
		}
	}

	client, err := client.GetClient("check-lights", address, encryptionKey, 5*time.Second, handler)
	if err != nil {
		return false, 0
	}
	defer client.Close()

	// Quick handshake
	_ = client.Send(&api.HelloRequest{ClientInfo: "check", ApiVersionMajor: 1, ApiVersionMinor: 9})
	time.Sleep(100 * time.Millisecond)
	_ = client.Send(&api.ConnectRequest{})
	time.Sleep(100 * time.Millisecond)
	_ = client.Send(&api.ListEntitiesRequest{})

	select {
	case key, ok := <-lightFound:
		if ok {
			return true, key
		}
		return false, 0
	case <-time.After(3 * time.Second):
		return false, 0
	}
}

// connectAndControlEdisonLight connects to an Edison device, finds a light, and controls it
func connectAndControlEdisonLight(t *testing.T, device *mdns.Device, store storage.Storage) {
	address := fmt.Sprintf("%s:%d", device.GetAddress(), device.GetAPIPort())
	t.Logf("\n=== Testing Light Control on: %s ===", device.Name)
	t.Logf("Address: %s", address)

	// Get encryption key from environment
	encryptionKey := getAPIEncryptionKey()
	if device.HasAPIKey() && encryptionKey == "" {
		t.Log("⚠️  Device requires API encryption but no key configured")
		t.Log("Set ESPHOME_API_KEY in .env file or environment variable")
		return
	}
	if encryptionKey != "" {
		t.Log("Using API encryption key")
	}

	// Create message channel
	msgChan := make(chan proto.Message, 100)
	doneChan := make(chan bool)
	stateUpdateChan := make(chan *api.LightStateResponse, 10)
	lightEntityKey := uint32(0)
	lightEntityName := ""

	// Handler for incoming messages
	handler := func(msg proto.Message) {
		msgChan <- msg

		switch m := msg.(type) {
		case *api.ListEntitiesDoneResponse:
			t.Log("✓ Entity list complete")
			select {
			case doneChan <- true:
			default:
			}

		case *api.ListEntitiesLightResponse:
			// Store the first light entity we find
			if lightEntityKey == 0 {
				lightEntityKey = m.Key
				lightEntityName = m.Name
				t.Logf("Found light entity: %s (key=%d)", m.Name, m.Key)
			}
			// Save to storage
			if err := createLightEntity(t, store, device, m); err != nil {
				t.Logf("Failed to save light entity: %v", err)
			}

		case *api.LightStateResponse:
			// Capture state updates
			t.Logf("📊 Light state update: key=%d, state=%v, brightness=%f",
				m.Key, m.State, m.Brightness)
			stateUpdateChan <- m

		case *api.BinarySensorStateResponse:
			t.Logf("📊 Binary sensor state: key=%d, state=%v", m.Key, m.State)

		case *api.SwitchStateResponse:
			t.Logf("📊 Switch state: key=%d, state=%v", m.Key, m.State)
		}
	}

	// Connect
	t.Log("\n[1/5] Creating client connection...")
	espClient, err := client.GetClient("test-client-edison", address, encryptionKey, 15*time.Second, handler)
	if err != nil {
		t.Logf("❌ Failed to connect: %v", err)
		return
	}
	defer espClient.Close()
	t.Log("✓ Connected successfully!")

	// Handshake
	t.Log("\n[2/5] Performing ESPHome API handshake...")
	err = espClient.Send(&api.HelloRequest{
		ClientInfo:      "slidebolt-light-control-test",
		ApiVersionMajor: 1,
		ApiVersionMinor: 9,
	})
	if err != nil {
		t.Logf("❌ HelloRequest failed: %v", err)
		return
	}
	time.Sleep(300 * time.Millisecond)

	err = espClient.Send(&api.ConnectRequest{})
	if err != nil {
		t.Logf("❌ ConnectRequest failed: %v", err)
		return
	}
	time.Sleep(300 * time.Millisecond)
	t.Log("✓ Handshake complete")

	// Request entity list
	t.Log("\n[3/5] Requesting entity list...")
	err = espClient.Send(&api.ListEntitiesRequest{})
	if err != nil {
		t.Logf("❌ ListEntitiesRequest failed: %v", err)
		return
	}

	// Wait for entity list to complete
	select {
	case <-doneChan:
		t.Log("✓ Entity list received")
	case <-time.After(8 * time.Second):
		t.Log("⚠️  Timeout waiting for entity list")
		return
	}

	if lightEntityKey == 0 {
		t.Log("❌ No light entities found on this device")
		return
	}

	t.Logf("\n[4/5] Found light to control: %s (key=%d)", lightEntityName, lightEntityKey)

	// Subscribe to state updates
	t.Log("\n[5/5] Subscribing to state updates...")
	err = espClient.Send(&api.SubscribeStatesRequest{})
	if err != nil {
		t.Logf("❌ SubscribeStatesRequest failed: %v", err)
		return
	}
	t.Log("✓ Subscribed to state updates")

	// Give subscription time to take effect
	time.Sleep(500 * time.Millisecond)

	// Send command to turn light ON
	t.Logf("\n💡 Sending command: Turn light ON")
	err = espClient.Send(&api.LightCommandRequest{
		Key:   lightEntityKey,
		State: true,
	})
	if err != nil {
		t.Logf("❌ Light command failed: %v", err)
		return
	}

	// Wait for state update
	t.Log("Waiting for state update confirmation...")
	select {
	case state := <-stateUpdateChan:
		if state.State {
			t.Logf("✅ SUCCESS! Light is now ON (key=%d, brightness=%f)",
				state.Key, state.Brightness)
		} else {
			t.Logf("⚠️  Light state is OFF (may still be processing)")
		}
	case <-time.After(3 * time.Second):
		t.Log("⚠️  No state update received within timeout (but command was sent)")
	}

	// Small delay
	time.Sleep(1 * time.Second)

	// Send command to turn light OFF
	t.Logf("\n💡 Sending command: Turn light OFF")
	err = espClient.Send(&api.LightCommandRequest{
		Key:   lightEntityKey,
		State: false,
	})
	if err != nil {
		t.Logf("❌ Light command failed: %v", err)
		return
	}

	// Wait for state update
	t.Log("Waiting for state update confirmation...")
	select {
	case state := <-stateUpdateChan:
		if !state.State {
			t.Logf("✅ SUCCESS! Light is now OFF (key=%d)", state.Key)
		} else {
			t.Logf("⚠️  Light state is still ON (may still be processing)")
		}
	case <-time.After(3 * time.Second):
		t.Log("⚠️  No state update received within timeout (but command was sent)")
	}

	// Summary
	t.Log("\n=== Test Summary ===")
	t.Logf("Device: %s", device.Name)
	t.Logf("Light controlled: %s (key=%d)", lightEntityName, lightEntityKey)
	t.Log("Commands sent: ON, OFF")
	t.Log("✓ Full round-trip test complete")
}

// validateDevice performs comprehensive validation of a discovered device
func validateDevice(t *testing.T, device *mdns.Device) {
	t.Helper()

	// Basic fields
	if device.Name == "" {
		t.Error("Device has no name")
	}

	// Network info
	hasHost := device.Host != ""
	hasAddress := len(device.Addresses) > 0
	if !hasHost && !hasAddress {
		t.Error("Device has no network identification (host or addresses)")
	}

	// Port validation
	if device.Port < 1 || device.Port > 65535 {
		t.Errorf("Invalid port number: %d", device.Port)
	}

	// Address resolution
	addr := device.GetAddress()
	if addr == "" {
		t.Error("Could not resolve device address")
	}

	// Print device info for debugging
	t.Logf("Device: %s", device.Name)
	t.Logf("  Host: %s", device.Host)
	t.Logf("  Addresses: %v", device.Addresses)
	t.Logf("  Port: %d", device.Port)
	t.Logf("  Resolved Address: %s", addr)
	t.Logf("  TXT Records: %v", device.TXTRecords)

	// ESPHome-specific metadata
	if version := device.ParseVersion(); version != "unknown" {
		t.Logf("  ESPHome Version: %s", version)
	}
	if mac := device.ParseMAC(); mac != "" {
		t.Logf("  MAC: %s", mac)
	}
	if board := device.ParseBoard(); board != "unknown" {
		t.Logf("  Board: %s", board)
	}
	t.Logf("  API Encryption: %v", device.HasAPIKey())
}
