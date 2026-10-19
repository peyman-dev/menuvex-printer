//go:build linux

package printer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverUSBReadsOnlyPrinterClassDevices(t *testing.T) {
	root := t.TempDir()
	sys, dev := filepath.Join(root, "sys"), filepath.Join(root, "dev")
	bus := filepath.Join(sys, "bus", "usb", "devices")

	// 1-1: a printer-class device with a usblp node.
	printerDir := filepath.Join(bus, "1-1")
	writeFile(t, filepath.Join(printerDir, "idVendor"), "04b8")
	writeFile(t, filepath.Join(printerDir, "idProduct"), "0e15")
	writeFile(t, filepath.Join(printerDir, "serial"), "EPSON123")
	writeFile(t, filepath.Join(printerDir, "product"), "EPSON TM-T20III")
	writeFile(t, filepath.Join(printerDir, "manufacturer"), "Seiko Epson")
	writeFile(t, filepath.Join(printerDir, "busnum"), "1")
	iface := filepath.Join(printerDir, "1-1:1.0")
	writeFile(t, filepath.Join(iface, "bInterfaceClass"), "07")
	writeFile(t, filepath.Join(dev, "usb", "lp0"), "device-node-placeholder")
	linkDir := filepath.Join(sys, "class", "usbmisc", "lp0")
	if err := os.MkdirAll(linkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(iface, filepath.Join(linkDir, "device")); err != nil {
		t.Fatal(err)
	}

	// 1-2: HID keyboard — must never be listed.
	hid := filepath.Join(bus, "1-2")
	writeFile(t, filepath.Join(hid, "idVendor"), "046d")
	writeFile(t, filepath.Join(hid, "idProduct"), "c31c")
	writeFile(t, filepath.Join(hid, "1-2:1.0", "bInterfaceClass"), "03")

	// 1-3: printer class without a usblp node — listed but not directly usable.
	noDriver := filepath.Join(bus, "1-3")
	writeFile(t, filepath.Join(noDriver, "idVendor"), "1234")
	writeFile(t, filepath.Join(noDriver, "idProduct"), "5678")
	writeFile(t, filepath.Join(noDriver, "1-3:1.0", "bInterfaceClass"), "07")

	got, err := discoverUSBAt(sys, dev)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("devices = %+v, want 2 printer-class devices", got)
	}
	byProduct := map[string]USBDevice{}
	for _, d := range got {
		byProduct[d.Product] = d
		if strings.EqualFold(d.VendorID, "046D") {
			t.Fatalf("HID device leaked into printer discovery: %+v", d)
		}
	}
	epson, ok := byProduct["EPSON TM-T20III"]
	if !ok {
		t.Fatalf("EPSON missing: %+v", got)
	}
	if epson.DevicePath != filepath.Join(dev, "usb", "lp0") {
		t.Fatalf("usblp path = %q", epson.DevicePath)
	}
	if !epson.Accessible || !strings.Contains(epson.AccessNote, "آماده") {
		t.Fatalf("accessible device note = %+v", epson)
	}
	if epson.Serial != "EPSON123" || epson.PortPath != "1-1" || epson.Bus != "1" {
		t.Fatalf("identity fields lost: %+v", epson)
	}

	var noNode *USBDevice
	for i := range got {
		if got[i].VendorID == "1234" {
			noNode = &got[i]
		}
	}
	if noNode == nil {
		t.Fatal("printer-class device without usblp was dropped; it must be shown with an explanation")
	}
	if noNode.Accessible || noNode.DevicePath != "" {
		t.Fatalf("device without usblp must not claim direct access: %+v", noNode)
	}
	if !strings.Contains(noNode.AccessNote, "usblp") {
		t.Fatalf("missing usblp guidance: %q", noNode.AccessNote)
	}
}

func TestDiscoverUSBHandlesMissingSysfs(t *testing.T) {
	root := t.TempDir()
	got, err := discoverUSBAt(filepath.Join(root, "sys"), filepath.Join(root, "dev"))
	if err == nil {
		t.Fatal("missing sysfs must return an error that DiscoverUSB converts to an empty list")
	}
	if got != nil {
		t.Fatalf("got %+v", got)
	}
}

func TestUSBIdentityDeduplicatesByPathAndSerial(t *testing.T) {
	a := USBDevice{DevicePath: "/dev/usb/lp0", VendorID: "04b8", ProductID: "0e15"}
	b := USBDevice{DevicePath: "/dev/usb/lp0", VendorID: "04b8", ProductID: "0e15"}
	if a.Identity() != b.Identity() {
		t.Fatal("same path must dedupe")
	}
	withSerial := USBDevice{VendorID: "04b8", ProductID: "0e15", Serial: "X"}
	withSerial2 := USBDevice{VendorID: "04B8", ProductID: "0E15", Serial: "X"}
	if withSerial.Identity() != withSerial2.Identity() {
		t.Fatal("VID/PID/serial identity must be case-insensitive")
	}
}
