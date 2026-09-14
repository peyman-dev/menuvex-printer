// Package printer is the printing engine: printer registry, TCP transport
// orchestration and USB backend plumbing.
//
// Design rules for v1:
//
//   - RAW bytes in, RAW bytes out. The engine never modifies payloads.
//   - No persistent queue, no database. One in-memory mutex per printer
//     serializes concurrent print operations so raw streams can't interleave.
//   - Network printers can be used ad-hoc via "tcp:<host>:<port>" IDs or
//     registered by name in the config file.
package printer

import (
	"errors"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/menuvex/novex-printer-agent/internal/config"
	"github.com/menuvex/novex-printer-agent/internal/usb"
)

// Printer types.
const (
	TypeNetwork = "network"
	TypeUSB     = "usb"
)

// Printer statuses.
const (
	StatusAvailable  = "available"
	StatusConnected  = "connected"
	StatusError      = "error"
	StatusRegistered = "registered"
)

// Printer is the public description of one printer.
type Printer struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	Status       string `json:"status"`
	Address      string `json:"address,omitempty"`
	Port         int    `json:"port,omitempty"`
	VendorID     int    `json:"vendorId,omitempty"`
	ProductID    int    `json:"productId,omitempty"`
	Serial       string `json:"serial,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty"`
	Product      string `json:"product,omitempty"`
	Detail       string `json:"detail,omitempty"`
}

// Status is a point-in-time connection report for one printer.
type Status struct {
	PrinterID   string     `json:"printerId"`
	Connected   bool       `json:"connected"`
	Status      string     `json:"status"`
	LastError   string     `json:"lastError,omitempty"`
	LastPrintAt *time.Time `json:"lastPrintAt,omitempty"`
}

// Event is emitted on connect / disconnect / error for WebSocket fan-out.
type Event struct {
	Type      string `json:"event"` // printer.connected | printer.disconnected | printer.error
	PrinterID string `json:"printerId"`
	Message   string `json:"message,omitempty"`
}

// Event types.
const (
	EventConnected    = "printer.connected"
	EventDisconnected = "printer.disconnected"
	EventError        = "printer.error"
)

// Registry tracks known printers and live connections.
type Registry struct {
	cfg     *config.Config
	cfgPath string
	usbDev  usb.USBPrinter

	mu         sync.Mutex
	tcpConns   map[string]net.Conn
	usbOpen    map[string]bool
	lastErr    map[string]string
	lastPrint  map[string]time.Time
	registered map[string]config.NetworkPrinterConfig // by printer ID

	opMu    sync.Mutex
	opLocks map[string]*sync.Mutex

	onEvent func(Event)
}

// NewRegistry creates a Registry. usbDev may be nil when USB is unsupported;
// USB operations then fail with CodeUSBUnsupported instead of panicking.
func NewRegistry(cfg *config.Config, cfgPath string, usbDev usb.USBPrinter) *Registry {
	r := &Registry{
		cfg:        cfg,
		cfgPath:    cfgPath,
		usbDev:     usbDev,
		tcpConns:   make(map[string]net.Conn),
		usbOpen:    make(map[string]bool),
		lastErr:    make(map[string]string),
		lastPrint:  make(map[string]time.Time),
		registered: make(map[string]config.NetworkPrinterConfig),
		opLocks:    make(map[string]*sync.Mutex),
		onEvent:    func(Event) {},
	}
	for _, p := range cfg.Printers {
		r.registered[TCPPrinterID(p.Address, p.Port)] = p
	}
	return r
}

// SetEventHandler installs the callback invoked for printer events.
func (r *Registry) SetEventHandler(fn func(Event)) {
	if fn == nil {
		fn = func(Event) {}
	}
	r.mu.Lock()
	r.onEvent = fn
	r.mu.Unlock()
}

func (r *Registry) emit(ev Event) {
	r.mu.Lock()
	fn := r.onEvent
	r.mu.Unlock()
	fn(ev)
}

// opLock returns the per-printer mutex, creating it on first use.
func (r *Registry) opLock(id string) *sync.Mutex {
	r.opMu.Lock()
	defer r.opMu.Unlock()
	m, ok := r.opLocks[id]
	if !ok {
		m = &sync.Mutex{}
		r.opLocks[id] = m
	}
	return m
}

func (r *Registry) connectTimeout() time.Duration {
	return time.Duration(r.cfg.ConnectTimeoutMs) * time.Millisecond
}

func (r *Registry) writeTimeout() time.Duration {
	return time.Duration(r.cfg.WriteTimeoutMs) * time.Millisecond
}

// ListPrinters returns configured network printers plus live USB devices.
func (r *Registry) ListPrinters() ([]Printer, error) {
	r.mu.Lock()
	registered := make([]config.NetworkPrinterConfig, 0, len(r.registered))
	for _, p := range r.registered {
		registered = append(registered, p)
	}
	r.mu.Unlock()

	out := make([]Printer, 0, len(registered)+4)
	for _, p := range registered {
		id := TCPPrinterID(p.Address, p.Port)
		name := p.Name
		if name == "" {
			name = p.Address
		}
		out = append(out, Printer{
			ID:      id,
			Name:    name,
			Type:    TypeNetwork,
			Status:  r.cachedStatus(id),
			Address: p.Address,
			Port:    p.Port,
		})
	}

	if r.usbDev != nil {
		devs, err := r.usbDev.ListDevices()
		if err != nil {
			return nil, NewError(CodeUSBError, "", "USB discovery failed: %s", shortErr(err))
		}
		for _, d := range devs {
			out = append(out, Printer{
				ID:           d.ID,
				Name:         d.Name,
				Type:         TypeUSB,
				Status:       r.cachedStatus(d.ID),
				VendorID:     d.VendorID,
				ProductID:    d.ProductID,
				Serial:       d.Serial,
				Manufacturer: d.Manufacturer,
				Product:      d.Product,
				Detail:       d.Detail,
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// cachedStatus derives a display status from live connection state.
func (r *Registry) cachedStatus(id string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tcpConns[id]; ok {
		return StatusConnected
	}
	if r.usbOpen[id] {
		return StatusConnected
	}
	if errMsg, ok := r.lastErr[id]; ok && errMsg != "" {
		return StatusError
	}
	if strings.HasPrefix(id, SchemeTCP) {
		if _, ok := r.registered[id]; ok {
			return StatusRegistered
		}
		return StatusAvailable
	}
	return StatusAvailable
}

// GetPrinter resolves any printer ID (registered, ad-hoc tcp:, or live usb:).
func (r *Registry) GetPrinter(id string) (Printer, error) {
	switch {
	case strings.HasPrefix(id, SchemeTCP):
		host, port, err := ParseTCPPrinterID(id)
		if err != nil {
			return Printer{}, err
		}
		r.mu.Lock()
		reg, ok := r.registered[id]
		r.mu.Unlock()
		name := host
		if ok && reg.Name != "" {
			name = reg.Name
		}
		return Printer{
			ID:      id,
			Name:    name,
			Type:    TypeNetwork,
			Status:  r.cachedStatus(id),
			Address: host,
			Port:    port,
		}, nil
	case strings.HasPrefix(id, SchemeUSB):
		if r.usbDev == nil {
			return Printer{}, NewError(CodeUSBUnsupported, id, "%s", usb.UnsupportedMessage())
		}
		devs, err := r.usbDev.ListDevices()
		if err != nil {
			return Printer{}, NewError(CodeUSBError, id, "USB discovery failed: %s", shortErr(err))
		}
		for _, d := range devs {
			if d.ID == id {
				return Printer{
					ID:           d.ID,
					Name:         d.Name,
					Type:         TypeUSB,
					Status:       r.cachedStatus(d.ID),
					VendorID:     d.VendorID,
					ProductID:    d.ProductID,
					Serial:       d.Serial,
					Manufacturer: d.Manufacturer,
					Product:      d.Product,
					Detail:       d.Detail,
				}, nil
			}
		}
		return Printer{}, NewError(CodePrinterNotFound, id, "USB device %q not found (it may have been unplugged)", id)
	default:
		return Printer{}, NewError(CodeInvalidPrinterID, id, "unknown printer id scheme (want tcp:... or usb:...)")
	}
}

// Connect opens (and caches) a connection to the printer.
func (r *Registry) Connect(id string) error {
	lock := r.opLock(id)
	lock.Lock()
	defer lock.Unlock()

	switch {
	case strings.HasPrefix(id, SchemeTCP):
		host, port, err := ParseTCPPrinterID(id)
		if err != nil {
			return err
		}
		r.mu.Lock()
		_, already := r.tcpConns[id]
		r.mu.Unlock()
		if already {
			return nil
		}
		conn, err := dialTCP(host, port, r.connectTimeout())
		if err != nil {
			r.setLastErr(id, err.Error())
			r.emit(Event{Type: EventError, PrinterID: id, Message: err.Error()})
			return err
		}
		r.mu.Lock()
		r.tcpConns[id] = conn
		delete(r.lastErr, id)
		r.mu.Unlock()
		r.emit(Event{Type: EventConnected, PrinterID: id})
		return nil
	case strings.HasPrefix(id, SchemeUSB):
		if r.usbDev == nil {
			return NewError(CodeUSBUnsupported, id, "%s", usb.UnsupportedMessage())
		}
		if err := r.usbDev.Open(id); err != nil {
			mapped := mapUSBError(id, err)
			r.setLastErr(id, mapped.Error())
			r.emit(Event{Type: EventError, PrinterID: id, Message: mapped.Error()})
			return mapped
		}
		r.mu.Lock()
		r.usbOpen[id] = true
		delete(r.lastErr, id)
		r.mu.Unlock()
		r.emit(Event{Type: EventConnected, PrinterID: id})
		return nil
	default:
		return NewError(CodeInvalidPrinterID, id, "unknown printer id scheme (want tcp:... or usb:...)")
	}
}

// Disconnect closes a cached connection. Disconnecting a printer that is
// not connected is a no-op success.
func (r *Registry) Disconnect(id string) error {
	lock := r.opLock(id)
	lock.Lock()
	defer lock.Unlock()

	switch {
	case strings.HasPrefix(id, SchemeTCP):
		r.mu.Lock()
		conn, ok := r.tcpConns[id]
		if ok {
			delete(r.tcpConns, id)
		}
		r.mu.Unlock()
		if !ok {
			return nil
		}
		_ = conn.Close()
		r.emit(Event{Type: EventDisconnected, PrinterID: id})
		return nil
	case strings.HasPrefix(id, SchemeUSB):
		if r.usbDev == nil {
			return nil
		}
		r.mu.Lock()
		wasOpen := r.usbOpen[id]
		delete(r.usbOpen, id)
		r.mu.Unlock()
		if err := r.usbDev.Close(id); err != nil && wasOpen {
			return mapUSBError(id, err)
		}
		if wasOpen {
			r.emit(Event{Type: EventDisconnected, PrinterID: id})
		}
		return nil
	default:
		return NewError(CodeInvalidPrinterID, id, "unknown printer id scheme (want tcp:... or usb:...)")
	}
}

// Print sends raw bytes to the printer. The payload is never modified.
// Print works with or without a prior Connect: when no cached connection
// exists, Print dials, writes and closes within the single operation.
// Concurrent prints to the same printer are serialized by its mutex.
func (r *Registry) Print(id string, data []byte) error {
	if len(data) == 0 {
		return NewError(CodeInvalidRequest, id, "print payload must not be empty")
	}
	lock := r.opLock(id)
	lock.Lock()
	defer lock.Unlock()

	switch {
	case strings.HasPrefix(id, SchemeTCP):
		return r.printTCP(id, data)
	case strings.HasPrefix(id, SchemeUSB):
		return r.printUSB(id, data)
	default:
		return NewError(CodeInvalidPrinterID, id, "unknown printer id scheme (want tcp:... or usb:...)")
	}
}

func (r *Registry) printTCP(id string, data []byte) error {
	host, port, err := ParseTCPPrinterID(id)
	if err != nil {
		return err
	}
	// Reuse the cached connection when present, else dial ad-hoc.
	r.mu.Lock()
	conn, cached := r.tcpConns[id]
	r.mu.Unlock()

	owned := false
	if !cached {
		conn, err = dialTCP(host, port, r.connectTimeout())
		if err != nil {
			r.setLastErr(id, err.Error())
			r.emit(Event{Type: EventError, PrinterID: id, Message: err.Error()})
			return err
		}
		owned = true
	}
	if err := writeAll(conn, id, data, r.writeTimeout()); err != nil {
		// The connection is suspect after a failed write: drop it.
		r.mu.Lock()
		if c, ok := r.tcpConns[id]; ok && c == conn {
			delete(r.tcpConns, id)
		}
		r.mu.Unlock()
		_ = conn.Close()
		r.setLastErr(id, err.Error())
		r.emit(Event{Type: EventError, PrinterID: id, Message: err.Error()})
		return err
	}
	if owned {
		_ = conn.Close()
	}
	r.mu.Lock()
	now := time.Now()
	r.lastPrint[id] = now
	delete(r.lastErr, id)
	r.mu.Unlock()
	return nil
}

func (r *Registry) printUSB(id string, data []byte) error {
	if r.usbDev == nil {
		return NewError(CodeUSBUnsupported, id, "%s", usb.UnsupportedMessage())
	}
	if err := r.usbDev.Write(id, data); err != nil {
		mapped := mapUSBError(id, err)
		r.setLastErr(id, mapped.Error())
		r.emit(Event{Type: EventError, PrinterID: id, Message: mapped.Error()})
		return mapped
	}
	r.mu.Lock()
	now := time.Now()
	r.lastPrint[id] = now
	delete(r.lastErr, id)
	r.mu.Unlock()
	return nil
}

// GetStatus reports the current connection state of a printer.
func (r *Registry) GetStatus(id string) (Status, error) {
	if _, err := r.GetPrinter(id); err != nil {
		return Status{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, tcpOn := r.tcpConns[id]
	connected := tcpOn || r.usbOpen[id]
	st := Status{
		PrinterID: id,
		Connected: connected,
		Status:    StatusAvailable,
	}
	switch {
	case connected:
		st.Status = StatusConnected
	case r.lastErr[id] != "":
		st.Status = StatusError
		st.LastError = r.lastErr[id]
	default:
		if _, ok := r.registered[id]; ok {
			st.Status = StatusRegistered
		}
	}
	if t, ok := r.lastPrint[id]; ok {
		ts := t
		st.LastPrintAt = &ts
	}
	return st, nil
}

// AddNetworkPrinter registers a network printer by friendly name and
// persists it to the config file. It returns the canonical printer ID.
func (r *Registry) AddNetworkPrinter(name, address string, port int) (string, error) {
	name = strings.TrimSpace(name)
	address = strings.TrimSpace(address)
	if address == "" {
		return "", NewError(CodeInvalidRequest, "", "address must not be empty")
	}
	if port < 1 || port > 65535 {
		return "", NewError(CodeInvalidRequest, "", "port %d out of range (1-65535)", port)
	}
	if name == "" {
		name = address
	}
	entry := config.NetworkPrinterConfig{Name: name, Address: address, Port: port}
	id := TCPPrinterID(address, port)

	r.mu.Lock()
	r.registered[id] = entry
	r.cfg.Printers = rebuildConfigPrinters(r.registered)
	cfgCopy := *r.cfg
	r.mu.Unlock()

	if err := cfgCopy.Save(r.cfgPath); err != nil {
		return "", NewError(CodeInternalError, id, "cannot persist printer: %s", shortErr(err))
	}
	return id, nil
}

// RemoveNetworkPrinter unregisters a previously added network printer.
func (r *Registry) RemoveNetworkPrinter(id string) error {
	if !strings.HasPrefix(id, SchemeTCP) {
		return NewError(CodeInvalidRequest, id, "only registered tcp: printers can be removed")
	}
	if _, _, err := ParseTCPPrinterID(id); err != nil {
		return err
	}
	_ = r.Disconnect(id)

	r.mu.Lock()
	if _, ok := r.registered[id]; !ok {
		r.mu.Unlock()
		return NewError(CodePrinterNotFound, id, "no registered printer %q", id)
	}
	delete(r.registered, id)
	delete(r.lastErr, id)
	delete(r.lastPrint, id)
	r.cfg.Printers = rebuildConfigPrinters(r.registered)
	cfgCopy := *r.cfg
	r.mu.Unlock()

	if err := cfgCopy.Save(r.cfgPath); err != nil {
		return NewError(CodeInternalError, id, "cannot persist printer removal: %s", shortErr(err))
	}
	return nil
}

func rebuildConfigPrinters(m map[string]config.NetworkPrinterConfig) []config.NetworkPrinterConfig {
	out := make([]config.NetworkPrinterConfig, 0, len(m))
	for _, p := range m {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Address != out[j].Address {
			return out[i].Address < out[j].Address
		}
		return out[i].Port < out[j].Port
	})
	return out
}

// mapUSBError translates backend failures into stable API error codes.
func mapUSBError(id string, err error) *Error {
	if err == nil {
		return nil
	}
	if e, ok := AsError(err); ok {
		return e
	}
	if errors.Is(err, usb.ErrDeviceNotFound) {
		return NewError(CodeUSBDeviceNotFound, id, "%s", shortErr(err))
	}
	if errors.Is(err, usb.ErrUnsupported) {
		return NewError(CodeUSBUnsupported, id, "%s", shortErr(err))
	}
	return NewError(CodeUSBError, id, "%s", shortErr(err))
}

func (r *Registry) setLastErr(id, msg string) {
	r.mu.Lock()
	r.lastErr[id] = msg
	r.mu.Unlock()
}

// USBBackend exposes the underlying USB backend (may be nil).
func (r *Registry) USBBackend() usb.USBPrinter {
	return r.usbDev
}

// CloseAll drops every cached connection. Used at shutdown.
func (r *Registry) CloseAll() {
	r.mu.Lock()
	conns := make([]net.Conn, 0, len(r.tcpConns))
	for _, c := range r.tcpConns {
		conns = append(conns, c)
	}
	r.tcpConns = make(map[string]net.Conn)
	r.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
	if r.usbDev != nil {
		r.mu.Lock()
		ids := make([]string, 0, len(r.usbOpen))
		for id := range r.usbOpen {
			ids = append(ids, id)
		}
		r.usbOpen = make(map[string]bool)
		r.mu.Unlock()
		for _, id := range ids {
			_ = r.usbDev.Close(id)
		}
	}
}
