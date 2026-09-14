//go:build linux

package usb

import (
	"os"
	"path/filepath"
	"testing"
)

// TestProbeUSBLPFakeSysfs builds a fake /sys/class/usblp tree and verifies
// VID/PID/serial parsing plus the printer ID format.
func TestProbeUSBLPFakeSysfs(t *testing.T) {
	root := t.TempDir()
	// Fake USB device 1-2 with interface 1-2:1.0.
	devDir := filepath.Join(root, "devices", "1-2")
	ifaceDir := filepath.Join(devDir, "1-2:1.0")
	if err := os.MkdirAll(ifaceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(dir, name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(devDir, "idVendor", "04b8")
	write(devDir, "idProduct", "0202")
	write(devDir, "serial", "ABC123")
	write(devDir, "manufacturer", "OCOM")
	write(devDir, "product", "Thermal Printer")
	write(devDir, "busnum", "001")
	write(devDir, "devnum", "004")

	// Fake class dir: lp0/device -> iface dir (use absolute symlink target
	// to also cover absolute-link handling).
	classDir := filepath.Join(root, "class", "usblp", "lp0")
	if err := os.MkdirAll(classDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(ifaceDir, filepath.Join(classDir, "device")); err != nil {
		t.Fatal(err)
	}

	dev, ok := probeUSBLP("/dev/usb/lp0", filepath.Join(root, "class", "usblp"))
	if !ok {
		t.Fatal("probe failed on fake sysfs")
	}
	if dev.ID != "usb:04B8:0202:ABC123" {
		t.Fatalf("unexpected id %q", dev.ID)
	}
	if dev.VendorID != 0x04B8 || dev.ProductID != 0x0202 {
		t.Fatalf("unexpected vid/pid %d/%d", dev.VendorID, dev.ProductID)
	}
	if dev.Manufacturer != "OCOM" || dev.Product != "Thermal Printer" {
		t.Fatalf("unexpected names %+v", dev)
	}
	if dev.Detail != "/dev/usb/lp0" {
		t.Fatalf("unexpected detail %q", dev.Detail)
	}
}

// TestProbeUSBLPNoSerial verifies the bus/dev fallback ID when the device
// exposes no serial number.
func TestProbeUSBLPNoSerial(t *testing.T) {
	root := t.TempDir()
	devDir := filepath.Join(root, "2-1")
	ifaceDir := filepath.Join(devDir, "2-1:1.0")
	if err := os.MkdirAll(ifaceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(devDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("idVendor", "0416")
	mustWrite("idProduct", "5011")
	mustWrite("busnum", "002")
	mustWrite("devnum", "007")

	classDir := filepath.Join(root, "usblp", "lp3")
	if err := os.MkdirAll(classDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Relative symlink (as on real sysfs).
	rel, err := filepath.Rel(classDir, ifaceDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(rel, filepath.Join(classDir, "device")); err != nil {
		t.Fatal(err)
	}

	dev, ok := probeUSBLP("/dev/usb/lp3", filepath.Join(root, "usblp"))
	if !ok {
		t.Fatal("probe failed")
	}
	if dev.ID != "usb:0416:5011:bus002dev007" {
		t.Fatalf("unexpected fallback id %q", dev.ID)
	}
}

func TestProbeUSBLPMissing(t *testing.T) {
	if _, ok := probeUSBLP("/dev/usb/lp99", t.TempDir()); ok {
		t.Fatal("expected probe to fail for missing device")
	}
}

func TestSanitizeIDPart(t *testing.T) {
	if got := sanitizeIDPart("AB:12/34 56"); got != "AB_12_34_56" {
		t.Fatalf("unexpected %q", got)
	}
}
