//go:build integration || local

package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	domain "github.com/slidebolt/sb-domain"
	managersdk "github.com/slidebolt/sb-manager-sdk"

	"github.com/slidebolt/plugin-esphome/app"
	mdns "github.com/slidebolt/plugin-esphome/internal/mdns"
)

// TestDeviceDiscovery_Physical_Integration is a baseline test that:
//  1. Discovers all ESPHome devices on the network via mDNS (before the plugin starts).
//  2. Seeds a per-device apiKey into private storage for every device that
//     advertises encryption — except the ratgdo (open API, no key needed).
//  3. Starts the plugin in-process; it discovers devices and reads their keys.
//  4. Asserts that every encrypted device ends up registered in storage as entities.
//
// The encryption key is stored only in .env.local (gitignored) and never in code.
// Run: go test -tags integration -v -run TestDeviceDiscovery_Physical_Integration ./cmd/plugin-esphome/
func TestDeviceDiscovery_Physical_Integration(t *testing.T) {
	apiKey := loadEnvLocal(t, "ESPHOME_API_KEY")

	env := managersdk.NewTestEnv(t)
	env.Start("messenger")
	env.Start("storage")

	store := env.Storage()

	// Step 1: discover devices via mDNS before the plugin starts.
	t.Log("discovering devices via mDNS...")
	disc, err := mdns.NewDiscovery(mdns.WithTimeout(5 * time.Second))
	if err != nil {
		t.Fatalf("mdns.NewDiscovery: %v", err)
	}
	devices, err := disc.Discover(t.Context())
	if err != nil {
		t.Fatalf("mDNS discover: %v", err)
	}
	if len(devices) == 0 {
		t.Fatal("no ESPHome devices found on the network")
	}
	t.Logf("found %d device(s) via mDNS:", len(devices))
	for _, dev := range devices {
		t.Logf("  %s  hasAPIKey=%v", dev.Name, dev.HasAPIKey())
	}

	// Step 2: seed per-device keys into private storage.
	// Devices that don't advertise encryption get no entry (plugin treats empty key as open API).
	seeded := 0
	for _, dev := range devices {
		if !dev.HasAPIKey() {
			t.Logf("  %s — open API, skipping key seed", dev.Name)
			continue
		}
		cfg := app.DeviceConfig{APIKey: apiKey}
		data, _ := json.Marshal(cfg)
		devKey := domain.DeviceKey{Plugin: app.PluginID, ID: dev.Name}
		if err := store.SetPrivate(devKey, json.RawMessage(data)); err != nil {
			t.Fatalf("SetPrivate for %s: %v", dev.Name, err)
		}
		t.Logf("  %s — key seeded", dev.Name)
		seeded++
	}
	t.Logf("seeded keys for %d/%d device(s)", seeded, len(devices))

	// Step 3: start the plugin, then inject the pre-discovered device list
	// directly to avoid a second mDNS probe that would hit RFC 6762 rate-limiting
	// (devices suppress re-answering the same PTR query within ~1s of the
	// test's own Discover call).
	p := app.New()
	deps := map[string]json.RawMessage{
		"messenger": env.MessengerPayload(),
	}
	if _, err := p.OnStart(deps); err != nil {
		t.Fatalf("plugin OnStart: %v", err)
	}
	t.Cleanup(func() { p.OnShutdown() })

	// Inject all pre-discovered devices. The plugin's own background probe
	// may find some of the same devices, but LoadOrStore prevents duplicates.
	for _, dev := range devices {
		p.OnDeviceFound(dev)
	}

	// Step 4: wait for entities to appear in storage for each device.
	// All 40 devices should connect within a few seconds on a healthy LAN.
	t.Log("waiting for entities to be registered...")
	deadline := time.After(15 * time.Second)
	registered := make(map[string]bool)
	for {
		for _, dev := range devices {
			if registered[dev.Name] {
				continue
			}
			entries, _ := store.Search(app.PluginID + "." + dev.Name + ".*")
			if len(entries) > 0 {
				registered[dev.Name] = true
				t.Logf("  ✓ %s — %d entit(ies)", dev.Name, len(entries))
			}
		}
		if len(registered) == len(devices) {
			break
		}
		select {
		case <-deadline:
			t.Logf("timeout: %d/%d devices registered", len(registered), len(devices))
			goto done
		case <-time.After(200 * time.Millisecond):
		}
	}
done:
	// Assert every device registered at least one entity.
	for _, dev := range devices {
		entries, err := store.Search(app.PluginID + "." + dev.Name + ".*")
		if err != nil || len(entries) == 0 {
			t.Errorf("✗ %s: no entities registered", dev.Name)
		}
	}
}

// loadEnvLocal reads a key from the module-root .env.local.
// Skips the test if the file is missing or the key is not set.
func loadEnvLocal(t *testing.T, key string) string {
	t.Helper()
	data, err := os.ReadFile("../../.env.local")
	if err != nil {
		t.Skipf(".env.local not found — set %s there to run this test", key)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, key+"=") {
			return strings.TrimSpace(strings.TrimPrefix(line, key+"="))
		}
	}
	t.Skipf("%s not set in .env.local", key)
	return ""
}
