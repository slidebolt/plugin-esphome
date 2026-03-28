package dashboard_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/slidebolt/plugin-esphome/internal/dashboard"
)

// fakeDashboard serves a minimal ESPHome dashboard login+devices flow.
func fakeDashboard(t *testing.T, devicesJSON string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			http.SetCookie(w, &http.Cookie{Name: "_xsrf", Value: "testtoken"})
			w.WriteHeader(http.StatusOK)
			return
		}
		// POST — validate credentials
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		if r.FormValue("username") != "admin" || r.FormValue("password") != "secret" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "authenticated", Value: "yes"})
		http.Redirect(w, r, "/", http.StatusFound)
	})

	mux.HandleFunc("/devices", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(devicesJSON))
	})

	return httptest.NewServer(mux)
}

func TestFetch_ReturnsDevicesWithIPs(t *testing.T) {
	srv := fakeDashboard(t, `{"configured":[
		{"name":"light-01","address":"192.168.1.10"},
		{"name":"light-02","address":"192.168.1.11"},
		{"name":"template","address":"template.local"}
	]}`)
	defer srv.Close()

	cfg := dashboard.Config{URL: srv.URL, Username: "admin", Password: "secret"}
	client := dashboard.NewClient(cfg, 0)

	devices, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(devices) != 2 {
		t.Fatalf("expected 2 devices (mDNS-only filtered), got %d", len(devices))
	}
	if devices[0].Name != "light-01" || devices[0].Address != "192.168.1.10" {
		t.Errorf("unexpected device[0]: %+v", devices[0])
	}
	if devices[1].Name != "light-02" || devices[1].Address != "192.168.1.11" {
		t.Errorf("unexpected device[1]: %+v", devices[1])
	}
	for _, d := range devices {
		if d.Port != 6053 {
			t.Errorf("expected port 6053 for %s, got %d", d.Name, d.Port)
		}
	}
}

func TestFetch_BadCredentials(t *testing.T) {
	srv := fakeDashboard(t, `{}`)
	defer srv.Close()

	cfg := dashboard.Config{URL: srv.URL, Username: "admin", Password: "wrong"}
	client := dashboard.NewClient(cfg, 0)

	_, err := client.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected error for bad credentials, got nil")
	}
}

func TestConfigValid(t *testing.T) {
	cases := []struct {
		cfg  dashboard.Config
		want bool
	}{
		{dashboard.Config{URL: "http://x", Username: "u", Password: "p"}, true},
		{dashboard.Config{URL: "", Username: "u", Password: "p"}, false},
		{dashboard.Config{URL: "http://x", Username: "", Password: "p"}, false},
		{dashboard.Config{URL: "http://x", Username: "u", Password: ""}, false},
	}
	for _, tc := range cases {
		if got := tc.cfg.Valid(); got != tc.want {
			t.Errorf("Config%+v.Valid() = %v, want %v", tc.cfg, got, tc.want)
		}
	}
}
