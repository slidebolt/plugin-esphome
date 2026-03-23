// Standalone Edison bulb test — connects directly to 3 ESPHome devices,
// sends one command each, and logs everything.
//
// Usage: go run ./cmd/edison-test/
package main

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/mycontroller-org/esphome_api/pkg/api"
	client "github.com/mycontroller-org/esphome_api/pkg/client"
	"google.golang.org/protobuf/proto"
)

type bulb struct {
	name    string
	addr    string
	espKey  uint32 // discovered from entity listing
	client  *client.Client
	gotList chan struct{}
}

func main() {
	apiKey := os.Getenv("ESPHOME_API_KEY")
	if apiKey == "" {
		log.Fatal("set ESPHOME_API_KEY")
	}

	bulbs := []*bulb{
		{name: "basement-09-edison-01", addr: "192.168.88.61:6053"},
		{name: "basement-12-edison-02", addr: "192.168.88.69:6053"},
		{name: "basement-13-edison-03", addr: "192.168.88.56:6053"},
	}

	// Connect to each device sequentially.
	for _, b := range bulbs {
		b.gotList = make(chan struct{}, 1)
		connectBulb(b, apiKey)
	}

	// Verify all connected.
	for _, b := range bulbs {
		if b.client == nil {
			log.Fatalf("FAILED to connect to %s (%s)", b.name, b.addr)
		}
	}
	log.Println("all 3 connected, waiting 1s for state to settle...")
	time.Sleep(1 * time.Second)

	// Step 1: turn all off.
	log.Println("=== STEP 1: turning all off ===")
	for _, b := range bulbs {
		sendCmd(b, &api.LightCommandRequest{
			Key: b.espKey, HasState: true, State: false,
		}, "light_turn_off")
		time.Sleep(200 * time.Millisecond)
	}
	log.Println("waiting 3s...")
	time.Sleep(3 * time.Second)

	// Step 2: set color temp + brightness in one command.
	log.Println("=== STEP 2: light_set_color_temp mireds=370 brightness=50% ===")
	for _, b := range bulbs {
		sendCmd(b, &api.LightCommandRequest{
			Key: b.espKey, HasState: true, State: true,
			HasBrightness: true, Brightness: 0.5,
			HasColorTemperature: true, ColorTemperature: 370,
		}, "light_set_color_temp")
		time.Sleep(200 * time.Millisecond)
	}

	log.Println("waiting 5s for state responses...")
	// Collect state responses for 5 seconds.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(5 * time.Second)
	}()
	wg.Wait()

	// Step 3: turn all off again.
	log.Println("=== STEP 3: turning all off (cleanup) ===")
	for _, b := range bulbs {
		sendCmd(b, &api.LightCommandRequest{
			Key: b.espKey, HasState: true, State: false,
		}, "light_turn_off")
		time.Sleep(200 * time.Millisecond)
	}
	time.Sleep(2 * time.Second)

	// Close.
	for _, b := range bulbs {
		b.client.Close()
	}
	log.Println("done")
}

func connectBulb(b *bulb, apiKey string) {
	log.Printf("[%s] connecting to %s ...", b.name, b.addr)

	listDone := false
	handler := func(msg proto.Message) {
		switch m := msg.(type) {
		case *api.ListEntitiesDoneResponse:
			if !listDone {
				listDone = true
				select {
				case b.gotList <- struct{}{}:
				default:
				}
			}
		case *api.LightStateResponse:
			pct := int(m.Brightness * 100)
			log.Printf("[%s] STATE power=%v brightness=%d%% color_temp=%.0f color_mode=%s",
				b.name, m.State, pct, m.ColorTemperature, m.ColorMode)
		case *api.ListEntitiesLightResponse:
			log.Printf("[%s] ENTITY key=%d name=%q effects=%v", b.name, m.Key, m.Name, m.Effects)
			if b.espKey == 0 {
				b.espKey = m.Key
			}
		default:
			log.Printf("[%s] MSG %T", b.name, msg)
		}
	}

	c, err := client.GetClient("edison-test", b.addr, apiKey, 10*time.Second, handler)
	if err != nil {
		log.Printf("[%s] CONNECT FAILED: %v", b.name, err)
		return
	}

	if err := c.Send(&api.HelloRequest{
		ClientInfo: "edison-standalone-test", ApiVersionMajor: 1, ApiVersionMinor: 9,
	}); err != nil {
		log.Printf("[%s] HELLO FAILED: %v", b.name, err)
		c.Close()
		return
	}
	time.Sleep(200 * time.Millisecond)

	if err := c.Send(&api.ConnectRequest{}); err != nil {
		log.Printf("[%s] CONNECT_REQ FAILED: %v", b.name, err)
		c.Close()
		return
	}
	time.Sleep(200 * time.Millisecond)

	if err := c.Send(&api.ListEntitiesRequest{}); err != nil {
		log.Printf("[%s] LIST_ENTITIES FAILED: %v", b.name, err)
		c.Close()
		return
	}

	select {
	case <-b.gotList:
		log.Printf("[%s] entity listing complete", b.name)
	case <-time.After(5 * time.Second):
		log.Printf("[%s] TIMEOUT waiting for entity list", b.name)
		c.Close()
		return
	}

	if err := c.Send(&api.SubscribeStatesRequest{}); err != nil {
		log.Printf("[%s] SUBSCRIBE_STATES FAILED: %v", b.name, err)
		c.Close()
		return
	}

	log.Printf("[%s] connected and subscribed", b.name)
	b.client = c
}

func sendCmd(b *bulb, req *api.LightCommandRequest, label string) {
	log.Printf("[%s] SENDING %s req=%s", b.name, label, fmt.Sprintf("%+v", req))
	if err := b.client.Send(req); err != nil {
		log.Printf("[%s] SEND FAILED: %v", b.name, err)
	} else {
		log.Printf("[%s] SEND OK", b.name)
	}
}
