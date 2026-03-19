//go:build integration

package main

// harness_isolation_test.go — reproducible minimal tests for two known issues:
//
//  1. Bad device hangs registerLightsFromDevice forever
//     A device whose TCP port is open but whose encrypted handshake never
//     completes (e.g. a device with a wrong key, or a non-ESPHome TCP server)
//     causes registerLightsFromDevice to block past its listing deadline
//     because GetClient's timeout only covers the TCP dial, not the handshake.
//
//  2. Concurrent store.Save() bursts kill the shared NATS connection
//     NewTestEnv puts the storage server AND the caller on the same messenger
//     connection. N concurrent saves → N simultaneous state.changed.* publishes
//     → connection overload → subsequent msg.Request calls fail.
//
// Run:
//   go test -tags integration -v -run TestIsolation ./cmd/plugin-esphome/ -timeout 30s

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	domain "github.com/slidebolt/sb-domain"
	managersdk "github.com/slidebolt/sb-manager-sdk"
	messenger "github.com/slidebolt/sb-messenger-sdk"

	"github.com/slidebolt/plugin-esphome/app"
	mdns "github.com/slidebolt/plugin-esphome/internal/mdns"
)

// TestIsolation_BadDeviceHangsRegistration proves that a device whose TCP
// port is reachable but never completes the ESPHome handshake will block
// registerLightsFromDevice indefinitely (no per-device context deadline on
// the handshake phase).
func TestIsolation_BadDeviceHangsRegistration(t *testing.T) {
	// Start a TCP listener that accepts connections but never sends anything
	// (simulates a device that is reachable but unresponsive after TCP connect).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	// Accept connections silently — never respond.
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Hold the connection open, send nothing.
			t.Logf("bad device: connection accepted from %s, holding open silently", conn.RemoteAddr())
		}
	}()

	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	badDev := &mdns.Device{
		Name:      "bad-device",
		Addresses: []string{host},
		Port:      port,
		// No TXTRecords → HasAPIKey() == false → open API (no handshake key needed)
	}

	env := managersdk.NewTestEnv(t)
	env.Start("messenger")
	env.Start("storage")
	store := env.Storage()

	start := time.Now()
	done := make(chan struct{})
	go func() {
		registerLightsFromDevice(t, badDev, store)
		close(done)
	}()

	select {
	case <-done:
		t.Logf("registerLightsFromDevice returned in %s (good — has deadline)", time.Since(start))
	case <-time.After(12 * time.Second):
		t.Error("FAIL: registerLightsFromDevice blocked >12s on unresponsive device — missing handshake deadline")
	}
}

// TestIsolation_ConcurrentSavesBurstNATS proves that N concurrent store.Save()
// calls on a NewTestEnv burst the shared messenger connection, causing a
// subsequent msg.Request to fail.
func TestIsolation_ConcurrentSavesBurstNATS(t *testing.T) {
	const numSaves = 35

	env := managersdk.NewTestEnv(t)
	env.Start("messenger")
	env.Start("storage")
	store := env.Storage()
	msg := env.Messenger()

	var wg sync.WaitGroup
	for i := range numSaves {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ent := domain.Entity{
				ID:       fmt.Sprintf("test-entity-%d", i),
				Plugin:   app.PluginID,
				DeviceID: "test-device",
				Type:     "light",
				Name:     fmt.Sprintf("Test Light %d", i),
			}
			if err := store.Save(ent); err != nil {
				t.Logf("save %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	t.Logf("%d concurrent saves completed", numSaves)

	// Now try a simple NATS request — if the connection was burst-closed this fails.
	data, _ := json.Marshal(map[string]string{"key": "test"})
	_, err := msg.Request("storage.get", data, 3*time.Second)
	if err != nil {
		t.Errorf("FAIL: msg.Request after concurrent saves: %v — shared connection was broken by save burst", err)
	} else {
		t.Log("msg.Request succeeded after concurrent saves (connection held)")
	}
}

// (end of file)

// TestIsolation_SubscribeThenSave verifies that subscribing to a multi-token
// wildcard subject doesn't kill the NATS connection. This caught a bug where
// "plugin-esphome.>.command.light_set_rgb" used `>` mid-subject (invalid NATS
// syntax — `>` must be the last token). The server silently closed the connection.
// The fix is to use `*` per-token: "plugin-esphome.*.*.command.light_set_rgb".
func TestIsolation_SubscribeThenSave(t *testing.T) {
	env := managersdk.NewTestEnv(t)
	env.Start("messenger")
	env.Start("storage")
	store := env.Storage()
	msg := env.Messenger()

	sub, err := msg.Subscribe("plugin-esphome.*.*.command.light_set_rgb", func(m *messenger.Message) {})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Unsubscribe()

	probe := domain.Entity{ID: "probe", Plugin: app.PluginID, DeviceID: "probe", Type: "probe", Name: "probe"}
	if err := store.Save(probe); err != nil {
		t.Fatalf("save after subscribe: %v — connection was closed by invalid subscription subject", err)
	}
	t.Log("subscribe + save OK")
}
