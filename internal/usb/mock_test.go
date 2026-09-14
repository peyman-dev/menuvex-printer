package usb

import (
	"errors"
	"testing"
)

func TestMockListOpenWriteClose(t *testing.T) {
	m := NewMock()
	devs, err := m.ListDevices()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(devs) != 2 {
		t.Fatalf("expected 2 mock devices, got %d", len(devs))
	}
	id := devs[0].ID
	if err := m.Open(id); err != nil {
		t.Fatalf("open: %v", err)
	}
	if !m.IsOpen(id) {
		t.Fatalf("expected open")
	}
	payload := []byte{0x1b, 0x40, 1, 2, 3}
	if err := m.Write(id, payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Mutating the original must not affect the recording (copy semantics).
	payload[0] = 0x00
	writes := m.Written(id)
	if len(writes) != 1 || writes[0][0] != 0x1b {
		t.Fatalf("write not recorded correctly: %x", writes)
	}
	if err := m.Close(id); err != nil {
		t.Fatalf("close: %v", err)
	}
	if m.IsOpen(id) {
		t.Fatalf("expected closed")
	}
}

func TestMockUnknownDevice(t *testing.T) {
	m := NewMock()
	if err := m.Open("usb:0:0:nope"); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("expected ErrDeviceNotFound, got %v", err)
	}
	if err := m.Write("usb:0:0:nope", []byte("x")); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("expected ErrDeviceNotFound, got %v", err)
	}
	// Closing an unknown/never-opened device is a no-op success.
	if err := m.Close("usb:0:0:nope"); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestMockErrorInjection(t *testing.T) {
	m := NewMock()
	m.ListErr = errors.New("boom")
	if _, err := m.ListDevices(); err == nil {
		t.Fatal("expected injected list error")
	}
	m.Reset()
	if _, err := m.ListDevices(); err != nil {
		t.Fatalf("reset should clear errors: %v", err)
	}
}
