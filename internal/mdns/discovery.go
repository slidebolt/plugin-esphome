package mdns

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/grandcat/zeroconf"
)

type Discovery struct {
	serviceType string
	domain      string
	timeout     time.Duration
	ifaces      []net.Interface

	mu       sync.RWMutex
	devices  map[string]*Device
	stopChan chan struct{}
	resolver *zeroconf.Resolver

	mockMode bool
	mockChan chan *zeroconf.ServiceEntry
}

type Device struct {
	Name       string
	Host       string
	Addresses  []string
	Port       int
	TXTRecords map[string]string
	LastSeen   time.Time
}

func NewDiscovery(opts ...DiscoveryOption) (*Discovery, error) {
	d := &Discovery{
		serviceType: "_esphomelib._tcp",
		domain:      "local",
		timeout:     5 * time.Second,
		devices:     make(map[string]*Device),
		stopChan:    make(chan struct{}),
	}
	for _, opt := range opts {
		opt(d)
	}

	if len(d.ifaces) == 0 {
		d.ifaces = defaultDiscoveryInterfaces()
	}

	if !d.mockMode {
		resolver, err := zeroconf.NewResolver(zeroconf.SelectIfaces(d.ifaces))
		if err != nil {
			return nil, fmt.Errorf("failed to create mDNS resolver: %w", err)
		}
		d.resolver = resolver
	}

	return d, nil
}

type DiscoveryOption func(*Discovery)

func WithTimeout(timeout time.Duration) DiscoveryOption {
	return func(d *Discovery) {
		d.timeout = timeout
	}
}

func WithServiceType(serviceType string) DiscoveryOption {
	return func(d *Discovery) {
		d.serviceType = serviceType
	}
}

func WithInterfaces(ifaces []net.Interface) DiscoveryOption {
	return func(d *Discovery) {
		d.ifaces = append([]net.Interface(nil), ifaces...)
	}
}

func WithMockMode() DiscoveryOption {
	return func(d *Discovery) {
		d.mockMode = true
		d.mockChan = make(chan *zeroconf.ServiceEntry, 100)
	}
}

func (d *Discovery) InjectMockEntry(entry *zeroconf.ServiceEntry) {
	d.mu.Lock()
	defer d.mu.Unlock()

	device := d.entryToDevice(entry)
	d.devices[device.Name] = device

	if d.mockChan != nil {
		select {
		case d.mockChan <- entry:
		default:
		}
	}
}

func (d *Discovery) Start(ctx context.Context) error {
	if d.mockMode {
		go func() { <-d.stopChan }()
		return nil
	}
	go d.listenLoop(ctx, nil)
	return nil
}

func (d *Discovery) Listen(ctx context.Context, onDevice func(*Device)) {
	if d.mockMode {
		go func() { <-d.stopChan }()
		return
	}
	go d.listenLoop(ctx, onDevice)
}

func (d *Discovery) Stop() {
	close(d.stopChan)
}

func (d *Discovery) listenLoop(ctx context.Context, onDevice func(*Device)) {
	entriesChan := make(chan *zeroconf.ServiceEntry)

	go func() {
		if err := d.resolver.Browse(ctx, d.serviceType, d.domain, entriesChan); err != nil {
			fmt.Printf("mDNS browse error: %v\n", err)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stopChan:
			return
		case entry, ok := <-entriesChan:
			if !ok {
				return
			}
			device := d.entryToDevice(entry)
			d.mu.Lock()
			d.devices[device.Name] = device
			d.mu.Unlock()
			if onDevice != nil {
				go onDevice(device)
			}
		}
	}
}

func (d *Discovery) Discover(ctx context.Context) ([]*Device, error) {
	if d.mockMode {
		return d.GetDevices(), nil
	}

	resolver, err := zeroconf.NewResolver(zeroconf.SelectIfaces(d.ifaces))
	if err != nil {
		return nil, fmt.Errorf("mDNS resolver: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	entriesChan := make(chan *zeroconf.ServiceEntry)
	var discovered []*Device
	var mu sync.Mutex

	go func() {
		if err := resolver.Browse(ctx, d.serviceType, d.domain, entriesChan); err != nil {
			fmt.Printf("mDNS browse error: %v\n", err)
		}
	}()

	seen := make(map[string]struct{})
	for {
		select {
		case entry, ok := <-entriesChan:
			if !ok {
				goto done
			}
			device := d.entryToDevice(entry)
			if _, ok := seen[device.Name]; ok {
				continue
			}
			seen[device.Name] = struct{}{}

			mu.Lock()
			discovered = append(discovered, device)
			mu.Unlock()

			d.mu.Lock()
			d.devices[device.Name] = device
			d.mu.Unlock()

		case <-ctx.Done():
			goto done
		}
	}
done:
	return discovered, nil
}

func defaultDiscoveryInterfaces() []net.Interface {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

	var preferred []net.Interface
	var fallback []net.Interface
	for _, ifi := range ifaces {
		if (ifi.Flags&net.FlagUp) == 0 || (ifi.Flags&net.FlagMulticast) == 0 || (ifi.Flags&net.FlagLoopback) != 0 {
			continue
		}
		fallback = append(fallback, ifi)
		if isDiscoveryInterface(ifi.Name) {
			preferred = append(preferred, ifi)
		}
	}
	if len(preferred) > 0 {
		return preferred
	}
	return fallback
}

func isDiscoveryInterface(name string) bool {
	for _, prefix := range []string{"docker", "br-", "veth", "virbr", "cni", "flannel", "tailscale", "zt"} {
		if strings.HasPrefix(name, prefix) {
			return false
		}
	}
	return true
}

func (d *Discovery) entryToDevice(entry *zeroconf.ServiceEntry) *Device {
	txtRecords := make(map[string]string)
	for _, txt := range entry.Text {
		parts := strings.SplitN(txt, "=", 2)
		if len(parts) == 2 {
			txtRecords[parts[0]] = parts[1]
		}
	}

	addresses := make([]string, 0, len(entry.AddrIPv4)+len(entry.AddrIPv6))
	for _, ip := range entry.AddrIPv4 {
		addresses = append(addresses, ip.String())
	}
	for _, ip := range entry.AddrIPv6 {
		addresses = append(addresses, ip.String())
	}

	return &Device{
		Name:       entry.Instance,
		Host:       entry.HostName,
		Addresses:  addresses,
		Port:       entry.Port,
		TXTRecords: txtRecords,
		LastSeen:   time.Now(),
	}
}

func (d *Discovery) GetDevices() []*Device {
	d.mu.RLock()
	defer d.mu.RUnlock()

	devices := make([]*Device, 0, len(d.devices))
	for _, device := range d.devices {
		devices = append(devices, device)
	}
	return devices
}

func (d *Discovery) GetDevice(name string) (*Device, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	device, ok := d.devices[name]
	return device, ok
}

func (dev *Device) ParseVersion() string {
	if version, ok := dev.TXTRecords["version"]; ok {
		return version
	}
	if version, ok := dev.TXTRecords["v"]; ok {
		return version
	}
	return "unknown"
}

func (dev *Device) ParseMAC() string {
	if mac, ok := dev.TXTRecords["mac"]; ok {
		return mac
	}
	if strings.HasPrefix(dev.Host, "esphome-") {
		parts := strings.Split(dev.Host, "-")
		if len(parts) > 1 {
			return parts[1]
		}
	}
	return ""
}

func (dev *Device) ParseBoard() string {
	if board, ok := dev.TXTRecords["board"]; ok {
		return board
	}
	return "unknown"
}

func (dev *Device) HasAPIKey() bool {
	if apiKey, ok := dev.TXTRecords["api_encryption"]; ok {
		if apiKey == "0" || strings.ToLower(apiKey) == "false" || apiKey == "" {
			return false
		}
		return true
	}
	return false
}

func (dev *Device) GetAPIPort() int {
	if dev.Port > 0 {
		return dev.Port
	}
	return 6053
}

func (dev *Device) GetAddress() string {
	if len(dev.Addresses) > 0 {
		return dev.Addresses[0]
	}
	if dev.Host != "" {
		addrs, err := net.LookupHost(dev.Host)
		if err == nil && len(addrs) > 0 {
			return addrs[0]
		}
		return dev.Host
	}
	return ""
}

func (dev *Device) IsStale(threshold time.Duration) bool {
	return time.Since(dev.LastSeen) > threshold
}
