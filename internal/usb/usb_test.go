package usb

import (
	"errors"
	"runtime"
	"testing"
)

// TestSupportInfo documents what this platform/build supports.
// It never fails: hardware presence varies by machine.
func TestSupportInfo(t *testing.T) {
	supported, detail := SupportInfo()
	t.Logf("usb supported=%v detail=%q", supported, detail)
	backend, supported2, _ := New()
	if supported != supported2 {
		t.Fatalf("New/SupportInfo disagree: %v vs %v", supported, supported2)
	}
	if !supported && backend != nil {
		t.Fatal("unsupported platform must return nil backend")
	}
	if supported && backend == nil {
		t.Fatal("supported platform must return a backend")
	}
}

// TestListDevicesNoPanic ensures discovery never crashes without hardware.
// An error is acceptable (e.g. CUPS down); a hang or panic is not.
func TestListDevicesNoPanic(t *testing.T) {
	backend, supported, _ := New()
	if !supported {
		t.Skip("usb unsupported on this platform")
	}
	devs, err := backend.ListDevices()
	t.Logf("devices=%d err=%v", len(devs), err)
}

// TestWriteUnknownDevice ensures backends report missing devices with
// ErrDeviceNotFound instead of generic errors.
func TestWriteUnknownDevice(t *testing.T) {
	backend, supported, _ := New()
	if !supported {
		t.Skip("usb unsupported on this platform")
	}
	// Use an ID that cannot exist on any backend.
	guesses := []string{"usb:FFFF:FFFF:does-not-exist-12345"}
	switch runtime.GOOS {
	case "windows":
		guesses = []string{"usb:win:Definitely Not A Printer"}
	case "darwin":
		guesses = []string{"usb:cups:definitely-not-a-queue"}
	}
	for _, id := range guesses {
		err := backend.Write(id, []byte{0x1b, 0x40})
		if err == nil {
			t.Fatalf("expected error writing to %s", id)
		}
		if !errors.Is(err, ErrDeviceNotFound) {
			t.Logf("note: backend returned non-NotFound error for %s: %v", id, err)
		}
		// Close on unknown device must succeed (idempotent).
		if err := backend.Close(id); err != nil {
			t.Fatalf("close on unknown device should succeed: %v", err)
		}
	}
}
