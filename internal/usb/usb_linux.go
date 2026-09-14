//go:build linux

package usb

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Linux backend: direct writes to usblp device nodes (/dev/usb/lp*).
//
// This is real native USB printing with zero dependencies: most USB thermal
// printers are claimed by the kernel usblp driver and accept raw bytes.
// Requirements (see docs/USB.md):
//
//   - the usblp kernel module must be loaded and bound to the printer
//   - the agent user needs write access (lp group or a udev rule)
//
// Printers NOT claimed by usblp (e.g. vendor-specific class devices) are not
// visible to this backend. That is reported honestly: they simply do not
// appear in ListDevices.

const linuxDetail = "Linux usblp backend (/dev/usb/lp* + sysfs discovery)"

func newPlatformBackend() (USBPrinter, bool, string) {
	return &linuxBackend{open: make(map[string]*os.File)}, true, linuxDetail
}

type linuxBackend struct {
	mu   sync.Mutex
	open map[string]*os.File // deviceID -> handle
}

func (b *linuxBackend) ListDevices() ([]USBDevice, error) {
	nodes, err := filepath.Glob("/dev/usb/lp*")
	if err != nil {
		return nil, err
	}
	var out []USBDevice
	for _, node := range nodes {
		dev, ok := probeUSBLP(node, "/sys/class/usblp")
		if !ok {
			continue
		}
		out = append(out, dev)
	}
	return out, nil
}

// probeUSBLP maps /dev/usb/lpN to its sysfs USB device attributes.
// classDir is "/sys/class/usblp" in production and a fake tree in tests.
func probeUSBLP(node, classDir string) (USBDevice, bool) {
	base := filepath.Base(node)
	link := filepath.Join(classDir, base, "device")
	target, err := os.Readlink(link)
	if err != nil {
		return USBDevice{}, false
	}
	// "device" points at the USB *interface* (…/1-2/1-2:1.0);
	// the parent directory is the USB *device* (…/1-2).
	iface := target
	if !filepath.IsAbs(iface) {
		iface = filepath.Join(filepath.Dir(link), target)
	}
	devDir := filepath.Dir(iface)

	vidHex := readSysfs(devDir, "idVendor")
	pidHex := readSysfs(devDir, "idProduct")
	if vidHex == "" || pidHex == "" {
		return USBDevice{}, false
	}
	vid64, err := strconv.ParseUint(vidHex, 16, 32)
	if err != nil {
		return USBDevice{}, false
	}
	pid64, err := strconv.ParseUint(pidHex, 16, 32)
	if err != nil {
		return USBDevice{}, false
	}
	serial := sanitizeIDPart(readSysfs(devDir, "serial"))
	if serial == "" {
		// Stable fallback when the device exposes no serial number.
		bus := sanitizeIDPart(readSysfs(devDir, "busnum"))
		dev := sanitizeIDPart(readSysfs(devDir, "devnum"))
		serial = "bus" + bus + "dev" + dev
	}
	manufacturer := readSysfs(devDir, "manufacturer")
	product := readSysfs(devDir, "product")
	name := strings.TrimSpace(manufacturer + " " + product)
	if name == "" {
		name = fmt.Sprintf("USB printer %04X:%04X", vid64, pid64)
	}
	return USBDevice{
		ID:           fmt.Sprintf("usb:%04X:%04X:%s", vid64, pid64, serial),
		Name:         name,
		VendorID:     int(vid64),
		ProductID:    int(pid64),
		Serial:       serial,
		Manufacturer: manufacturer,
		Product:      product,
		Detail:       node,
	}, true
}

func readSysfs(dir, attr string) string {
	data, err := os.ReadFile(filepath.Join(dir, attr))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// sanitizeIDPart strips characters that would break printer IDs.
func sanitizeIDPart(s string) string {
	s = strings.TrimSpace(s)
	r := strings.NewReplacer(":", "_", "/", "_", "\\", "_", " ", "_")
	return r.Replace(s)
}

// resolve maps a device ID back to its current device node by re-scanning.
// Re-scanning (instead of caching) keeps working across replugs where the
// kernel may hand out a different /dev/usb/lpN node.
func (b *linuxBackend) resolve(deviceID string) (string, error) {
	devs, err := b.ListDevices()
	if err != nil {
		return "", err
	}
	for _, d := range devs {
		if d.ID == deviceID {
			return d.Detail, nil
		}
	}
	return "", fmt.Errorf("%w: %s", ErrDeviceNotFound, deviceID)
}

func (b *linuxBackend) Open(deviceID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.open[deviceID]; ok {
		return nil
	}
	node, err := b.resolve(deviceID)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(node, os.O_WRONLY, 0)
	if err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("permission denied opening %s: add your user to the 'lp' group or install the udev rule from docs/USB.md: %w", node, err)
		}
		return fmt.Errorf("cannot open %s: %w", node, err)
	}
	b.open[deviceID] = f
	return nil
}

func (b *linuxBackend) Close(deviceID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	f, ok := b.open[deviceID]
	if !ok {
		return nil
	}
	delete(b.open, deviceID)
	return f.Close()
}

func (b *linuxBackend) Write(deviceID string, data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("empty payload")
	}
	b.mu.Lock()
	f, held := b.open[deviceID]
	b.mu.Unlock()

	transient := false
	if !held {
		node, err := b.resolve(deviceID)
		if err != nil {
			return err
		}
		f, err = os.OpenFile(node, os.O_WRONLY, 0)
		if err != nil {
			if os.IsPermission(err) {
				return fmt.Errorf("permission denied opening %s: add your user to the 'lp' group or install the udev rule from docs/USB.md: %w", node, err)
			}
			return fmt.Errorf("cannot open %s: %w", node, err)
		}
		transient = true
	}
	if transient {
		defer f.Close()
	}
	for len(data) > 0 {
		n, err := f.Write(data)
		if err != nil {
			return fmt.Errorf("USB write failed: %w", err)
		}
		if n == 0 {
			return fmt.Errorf("USB write failed: wrote 0 bytes")
		}
		data = data[n:]
	}
	return nil
}
