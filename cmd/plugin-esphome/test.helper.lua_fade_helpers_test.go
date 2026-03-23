//go:build local || integration

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/mycontroller-org/esphome_api/pkg/api"
	espclient "github.com/mycontroller-org/esphome_api/pkg/client"
	"github.com/slidebolt/plugin-esphome/app"
	mdns "github.com/slidebolt/plugin-esphome/internal/mdns"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	storage "github.com/slidebolt/sb-storage-sdk"
	"google.golang.org/protobuf/proto"
)

// getAPIEncryptionKey retrieves the ESPHome API encryption key from environment.
// It first tries to load from .env file, then falls back to environment variable.
// Returns empty string if no key is configured.
func getAPIEncryptionKey() string {
	_ = godotenv.Load(".env")          // cmd/plugin-esphome/.env
	_ = godotenv.Load("../../.env")    // project root/.env
	_ = godotenv.Load("../../../.env") // one level up from project

	key := os.Getenv("ESPHOME_API_KEY")
	return strings.TrimSpace(key)
}

// integScriptResp is the response envelope for script.* NATS API calls.
type integScriptResp struct {
	OK    bool   `json:"ok"`
	Hash  string `json:"hash,omitempty"`
	Error string `json:"error,omitempty"`
}

func integScriptAPI(t *testing.T, msg messenger.Messenger, subject string, body any) integScriptResp {
	t.Helper()
	data, _ := json.Marshal(body)
	resp, err := msg.Request(subject, data, 5*time.Second)
	if err != nil {
		t.Fatalf("script API %s: %v", subject, err)
	}
	var r integScriptResp
	if err := json.Unmarshal(resp.Data, &r); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	return r
}

// discoveredLight holds the storage entity key for a registered light.
type discoveredLight struct {
	entityKey string // e.g. "plugin-esphome.edison-bedroom.edison-bedroom_12345"
	name      string
}

// registerLightsFromDevice connects to one ESPHome device, lists its entities,
// saves lights to storage, and returns their entity keys.
// Mirrors the listing phase of connectAndRegister in main.go.
func registerLightsFromDevice(t *testing.T, dev *mdns.Device, store storage.Storage) []discoveredLight {
	t.Helper()

	address := fmt.Sprintf("%s:%d", dev.GetAddress(), dev.GetAPIPort())
	encKey := getAPIEncryptionKey()
	if !dev.HasAPIKey() {
		encKey = ""
	}

	var mu sync.Mutex
	var lights []discoveredLight
	listingDone := make(chan struct{}, 1)
	listingPhase := true

	handler := func(msg proto.Message) {
		if !listingPhase {
			return
		}
		switch m := msg.(type) {
		case *api.ListEntitiesDoneResponse:
			listingPhase = false
			select {
			case listingDone <- struct{}{}:
			default:
			}
		case *api.ListEntitiesLightResponse:
			ent, espKey, ok := app.EntityFromMsg(dev.Name, msg)
			if !ok {
				return
			}
			if err := store.Save(ent); err != nil {
				t.Logf("save light %s: %v", ent.ID, err)
				return
			}
			_ = espKey
			mu.Lock()
			lights = append(lights, discoveredLight{
				entityKey: app.PluginID + "." + dev.Name + "." + ent.ID,
				name:      m.Name,
			})
			mu.Unlock()
			t.Logf("  registered light: %s (%s)", m.Name, ent.ID)
		}
	}

	// GetClient can block indefinitely on the Noise handshake when a server
	// accepts the TCP connection but never responds. Run it in a goroutine and
	// abandon it after a hard deadline so a bad device never stalls the test.
	type connectResult struct {
		cl  *espclient.Client
		err error
	}
	connectCh := make(chan connectResult, 1)
	go func() {
		cl, err := espclient.GetClient(app.PluginID+"-fade-test", address, encKey, 10*time.Second, handler)
		connectCh <- connectResult{cl, err}
	}()

	var cl *espclient.Client
	select {
	case r := <-connectCh:
		if r.err != nil {
			t.Logf("connect %s: %v — skipping", dev.Name, r.err)
			return nil
		}
		cl = r.cl
	case <-time.After(15 * time.Second):
		t.Logf("connect %s: timed out — skipping", dev.Name)
		return nil
	}
	// Also bound cl.Close() — it waits for a DisconnectResponse that may never arrive.
	defer func() {
		done := make(chan struct{}, 1)
		go func() { cl.Close(); close(done) }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
		}
	}()

	_ = cl.Send(&api.HelloRequest{ClientInfo: "slidebolt-fade-test", ApiVersionMajor: 1, ApiVersionMinor: 9})
	time.Sleep(200 * time.Millisecond)
	_ = cl.Send(&api.ConnectRequest{})
	time.Sleep(200 * time.Millisecond)
	_ = cl.Send(&api.ListEntitiesRequest{})

	select {
	case <-listingDone:
	case <-time.After(8 * time.Second):
		t.Logf("timeout listing entities from %s", dev.Name)
	}

	return lights
}
