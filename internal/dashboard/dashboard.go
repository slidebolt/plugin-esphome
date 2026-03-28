// Package dashboard provides a client for the ESPHome dashboard HTTP API.
// It fetches the list of configured devices and their current IP addresses.
// The client is stateless between calls — each Fetch re-authenticates —
// so it is safe to call from a startup loader or a periodic polling loop.
package dashboard

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"time"
)

const defaultNativeAPIPort = 6053

// Config holds the ESPHome dashboard connection settings.
type Config struct {
	URL      string // base URL, e.g. https://esp.example.com
	Username string
	Password string
	Insecure bool // skip TLS certificate verification (useful for LAN IPs with hostname certs)
}

// ConfigFromEnv reads dashboard config from environment variables:
//
//	ESPHOME_DASHBOARD_URL
//	ESPHOME_DASHBOARD_USER
//	ESPHOME_DASHBOARD_PASSWORD
//	ESPHOME_DASHBOARD_INSECURE  (set to "1" to skip TLS certificate verification)
func ConfigFromEnv() Config {
	return Config{
		URL:      os.Getenv("ESPHOME_DASHBOARD_URL"),
		Username: os.Getenv("ESPHOME_DASHBOARD_USER"),
		Password: os.Getenv("ESPHOME_DASHBOARD_PASSWORD"),
		Insecure: os.Getenv("ESPHOME_DASHBOARD_INSECURE") == "1",
	}
}

// Valid returns true when all required fields are non-empty.
func (c Config) Valid() bool {
	return strings.TrimSpace(c.URL) != "" &&
		strings.TrimSpace(c.Username) != "" &&
		strings.TrimSpace(c.Password) != ""
}

// Device is a device entry returned by the ESPHome dashboard.
type Device struct {
	Name    string // matches the ESPHome device name / SlideBolt device ID
	Address string // current IP address
	Port    int    // native API port (always 6053 unless overridden)
}

// Client fetches device addresses from the ESPHome dashboard API.
// It is safe to call Fetch from multiple goroutines simultaneously.
type Client struct {
	cfg     Config
	timeout time.Duration
}

// NewClient creates a Client with the given config.
// Timeout defaults to 15 seconds if zero.
func NewClient(cfg Config, timeout time.Duration) *Client {
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	return &Client{cfg: cfg, timeout: timeout}
}

// Fetch fetches the current device list from the dashboard.
// It is stateless — each call performs a fresh login.
// Returns only devices that have a non-empty IP address.
func (c *Client) Fetch(ctx context.Context) ([]Device, error) {
	jar, _ := cookiejar.New(nil)
	transport := http.DefaultTransport
	if c.cfg.Insecure {
		transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // intentional for LAN use
		}
	}
	hc := &http.Client{
		Jar:       jar,
		Timeout:   c.timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // handle redirects manually
		},
	}
	base := strings.TrimRight(c.cfg.URL, "/")

	xsrf, err := c.getXSRF(ctx, hc, base)
	if err != nil {
		return nil, fmt.Errorf("get xsrf: %w", err)
	}

	if err := c.login(ctx, hc, base, xsrf); err != nil {
		return nil, fmt.Errorf("login: %w", err)
	}

	return c.fetchDevices(ctx, hc, base)
}

// getXSRF fetches the login page and extracts the _xsrf token from the cookie.
func (c *Client) getXSRF(ctx context.Context, hc *http.Client, base string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/login", nil)
	if err != nil {
		return "", err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()

	u, _ := url.Parse(base)
	for _, ck := range hc.Jar.Cookies(u) {
		if ck.Name == "_xsrf" {
			return ck.Value, nil
		}
	}
	return "", fmt.Errorf("_xsrf cookie not found in login response")
}

// login posts credentials and the xsrf token, leaving the authenticated cookie in the jar.
func (c *Client) login(ctx context.Context, hc *http.Client, base, xsrf string) error {
	form := url.Values{
		"username": {c.cfg.Username},
		"password": {c.cfg.Password},
		"_xsrf":    {xsrf},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/login", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()

	// Dashboard returns 302 on success, 403 on bad credentials.
	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected login response: %s", resp.Status)
	}
	return nil
}

// dashboardDevice mirrors the relevant fields of the ESPHome dashboard /devices JSON.
type dashboardDevice struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}

type dashboardResponse struct {
	Configured []dashboardDevice `json:"configured"`
}

func (c *Client) fetchDevices(ctx context.Context, hc *http.Client, base string) ([]Device, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/devices", nil)
	if err != nil {
		return nil, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("devices endpoint returned %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read devices body: %w", err)
	}

	var dr dashboardResponse
	if err := json.Unmarshal(body, &dr); err != nil {
		return nil, fmt.Errorf("parse devices JSON: %w", err)
	}

	out := make([]Device, 0, len(dr.Configured))
	for _, d := range dr.Configured {
		addr := strings.TrimSpace(d.Address)
		if addr == "" || strings.HasSuffix(addr, ".local") {
			// Skip devices with no IP or only a mDNS hostname — mDNS handles those.
			continue
		}
		out = append(out, Device{
			Name:    strings.TrimSpace(d.Name),
			Address: addr,
			Port:    defaultNativeAPIPort,
		})
	}
	return out, nil
}
