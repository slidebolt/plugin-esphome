package app_test

import (
	"testing"

	"github.com/slidebolt/plugin-esphome/app"
)

func TestHelloManifest(t *testing.T) {
	got := app.New().Hello()
	if got.ID != app.PluginID {
		t.Fatalf("Hello().ID = %q, want %q", got.ID, app.PluginID)
	}
	if len(got.DependsOn) != 2 {
		t.Fatalf("Hello().DependsOn = %v, want messenger+storage", got.DependsOn)
	}
}
