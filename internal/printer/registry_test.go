package printer

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/menuvex/novex-printer-agent/internal/config"
	"github.com/menuvex/novex-printer-agent/internal/usb"
)

func testRegistry(t *testing.T) (*Registry, *usb.MockUSB, *config.Config) {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Token = "test-token"
	cfg.ConnectTimeoutMs = 2000
	cfg.WriteTimeoutMs = 2000
	mock := usb.NewMock()
	reg := NewRegistry(cfg, filepath.Join(t.TempDir(), "config.json"), mock)
	return reg, mock, cfg
}

func TestListPrintersIncludesUSB(t *testing.T) {
	reg, mock, _ := testRegistry(t)
	printers, err := reg.ListPrinters()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(printers) != len(mock.Devices) {
		t.Fatalf("expected %d printers, got %d", len(mock.Devices), len(printers))
	}
	for _, p := range printers {
		if p.Type != TypeUSB || p.Status != StatusAvailable {
			t.Fatalf("unexpected printer %+v", p)
		}
	}
}

func TestAddRemoveNetworkPrinter(t *testing.T) {
	reg, _, cfg := testRegistry(t)
	id, err := reg.AddNetworkPrinter("OCOM", "192.168.1.50", 9100)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if id != "tcp:192.168.1.50:9100" {
		t.Fatalf("unexpected id %s", id)
	}
	printers, _ := reg.ListPrinters()
	found := false
	for _, p := range printers {
		if p.ID == id && p.Name == "OCOM" && p.Type == TypeNetwork {
			found = true
		}
	}
	if !found {
		t.Fatalf("registered printer missing from list: %+v", printers)
	}
	// Persisted to disk?
	data, err := os.ReadFile(reg.cfgPath)
	if err != nil {
		t.Fatalf("config not persisted: %v", err)
	}
	if !bytes.Contains(data, []byte("192.168.1.50")) {
		t.Fatalf("config missing printer: %s", data)
	}
	_ = cfg

	st, err := reg.GetStatus(id)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Status != StatusRegistered {
		t.Fatalf("expected registered status, got %+v", st)
	}

	if err := reg.RemoveNetworkPrinter(id); err != nil {
		t.Fatalf("remove: %v", err)
	}
	printers, _ = reg.ListPrinters()
	for _, p := range printers {
		if p.ID == id {
			t.Fatalf("printer still listed after removal")
		}
	}
	if err := reg.RemoveNetworkPrinter(id); err == nil {
		t.Fatal("expected not-found on second removal")
	}
}

func TestGetPrinterAdhoc(t *testing.T) {
	reg, _, _ := testRegistry(t)
	p, err := reg.GetPrinter("tcp:10.0.0.5:9100")
	if err != nil {
		t.Fatalf("adhoc resolve: %v", err)
	}
	if p.Type != TypeNetwork || p.Address != "10.0.0.5" || p.Port != 9100 {
		t.Fatalf("unexpected printer %+v", p)
	}
}

func TestUSBPrintViaMock(t *testing.T) {
	reg, mock, _ := testRegistry(t)
	id := mock.Devices[0].ID
	payload := []byte{0x1b, 0x40, 'U', 'S', 'B'}
	if err := reg.Connect(id); err != nil {
		t.Fatalf("connect: %v", err)
	}
	st, _ := reg.GetStatus(id)
	if !st.Connected {
		t.Fatalf("expected connected: %+v", st)
	}
	if err := reg.Print(id, payload); err != nil {
		t.Fatalf("print: %v", err)
	}
	writes := mock.Written(id)
	if len(writes) != 1 || !bytes.Equal(writes[0], payload) {
		t.Fatalf("mock got %x", writes)
	}
	st, _ = reg.GetStatus(id)
	if st.LastPrintAt == nil {
		t.Fatalf("expected LastPrintAt set: %+v", st)
	}
	if err := reg.Disconnect(id); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	st, _ = reg.GetStatus(id)
	if st.Connected {
		t.Fatalf("expected disconnected: %+v", st)
	}
}

func TestUSBUnknownDevice(t *testing.T) {
	reg, _, _ := testRegistry(t)
	if _, err := reg.GetPrinter("usb:9999:9999:nope"); err == nil {
		t.Fatal("expected not-found")
	}
	if err := reg.Print("usb:9999:9999:nope", []byte("x")); err == nil {
		t.Fatal("expected device-not-found")
	} else if perr, _ := AsError(err); perr.Code != CodeUSBDeviceNotFound {
		t.Fatalf("unexpected code %s", perr.Code)
	}
}

func TestUSBErrorMapping(t *testing.T) {
	reg, mock, _ := testRegistry(t)
	id := mock.Devices[0].ID
	mock.WriteErr = bytes.ErrTooLarge
	if err := reg.Print(id, []byte("x")); err == nil {
		t.Fatal("expected error")
	} else if perr, _ := AsError(err); perr.Code != CodeUSBError {
		t.Fatalf("unexpected code %s", perr.Code)
	}
	st, _ := reg.GetStatus(id)
	if st.Status != StatusError {
		t.Fatalf("expected error status: %+v", st)
	}
}

func TestUSBUnsupportedWhenBackendNil(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Token = "x"
	reg := NewRegistry(cfg, filepath.Join(t.TempDir(), "config.json"), nil)

	printers, err := reg.ListPrinters()
	if err != nil {
		t.Fatalf("list must work without USB: %v", err)
	}
	if len(printers) != 0 {
		t.Fatalf("expected no printers, got %+v", printers)
	}
	if err := reg.Print("usb:1:2:3", []byte("x")); err == nil {
		t.Fatal("expected unsupported error")
	} else if perr, _ := AsError(err); perr.Code != CodeUSBUnsupported {
		t.Fatalf("unexpected code %s", perr.Code)
	}
	if err := reg.Connect("usb:1:2:3"); err == nil {
		t.Fatal("expected unsupported error")
	}
	if _, err := reg.GetPrinter("usb:1:2:3"); err == nil {
		t.Fatal("expected unsupported error")
	}
}

func TestEventsEmitted(t *testing.T) {
	reg, _, _ := testRegistry(t)
	var events []Event
	reg.SetEventHandler(func(ev Event) { events = append(events, ev) })

	mock := startMockPrinter(t)
	id := mock.id()
	if err := reg.Connect(id); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := reg.Disconnect(id); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	// Failed print to a dead printer emits printer.error.
	dead := TCPPrinterID("127.0.0.1", 1)
	_ = reg.Print(dead, []byte("x"))

	types := map[string]bool{}
	for _, ev := range events {
		types[ev.Type] = true
	}
	for _, want := range []string{EventConnected, EventDisconnected, EventError} {
		if !types[want] {
			t.Fatalf("missing event %s (got %+v)", want, events)
		}
	}
}
