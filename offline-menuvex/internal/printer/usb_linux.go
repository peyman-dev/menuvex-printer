//go:build linux

package printer

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// ErrUSBUnsupported is returned on systems without direct USB access.
var ErrUSBUnsupported = errors.New("دسترسی مستقیم USB در این سیستم‌عامل پشتیبانی نمی‌شود؛ از یک صف چاپ سیستم‌عامل استفاده کنید")

// ErrUSBMissing indicates that no usblp device node can receive bytes.
var ErrUSBMissing = errors.New("گره دستگاه usblp در دسترس نیست")

// DiscoverUSB lists USB devices that expose a Printer Class interface (0x07),
// read from sysfs. It never lists keyboards, storage or hubs: only interfaces
// whose descriptor class is the printer class become candidates.
//
// Direct printing additionally requires the Linux usblp driver to have created
// /dev/usb/lpN. Discovery succeeds without it, but Accessible stays false and
// AccessNote explains that a system print queue must be used instead.
func DiscoverUSB() ([]USBDevice, error) {
	devices, err := discoverUSBAt("/sys", "/dev")
	if err != nil {
		if os.IsNotExist(err) {
			return []USBDevice{}, nil
		}
		return nil, err
	}
	return devices, nil
}

func discoverUSBAt(sysRoot, devRoot string) ([]USBDevice, error) {
	bus := filepath.Join(sysRoot, "bus", "usb", "devices")
	entries, err := os.ReadDir(bus)
	if err != nil {
		return nil, err
	}
	usblp, _ := usblpMap(sysRoot, devRoot)
	seen := map[string]bool{}
	var out []USBDevice
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || strings.Contains(name, ":") {
			continue
		}
		deviceDir := filepath.Join(bus, name)
		vendor := readSys(filepath.Join(deviceDir, "idVendor"))
		product := readSys(filepath.Join(deviceDir, "idProduct"))
		if vendor == "" || product == "" {
			continue
		}
		ifaceDirs, err := os.ReadDir(deviceDir)
		if err != nil {
			continue
		}
		printerInterface := ""
		for _, iface := range ifaceDirs {
			if !iface.IsDir() || !strings.HasPrefix(iface.Name(), name+":") {
				continue
			}
			if strings.TrimSpace(readSys(filepath.Join(deviceDir, iface.Name(), "bInterfaceClass"))) == "07" {
				printerInterface = iface.Name()
				break
			}
		}
		if printerInterface == "" {
			continue
		}
		devPath := usblp[printerInterface]
		dev := USBDevice{
			DevicePath:   devPath,
			VendorID:     strings.ToUpper(vendor),
			ProductID:    strings.ToUpper(product),
			Serial:       readSys(filepath.Join(deviceDir, "serial")),
			Manufacturer: readSys(filepath.Join(deviceDir, "manufacturer")),
			Product:      readSys(filepath.Join(deviceDir, "product")),
			Bus:          readSys(filepath.Join(deviceDir, "busnum")),
			PortPath:     name,
			System:       "linux",
		}
		if devPath != "" {
			dev.Accessible = syscall.Access(devPath, 2) == nil
			if dev.Accessible {
				dev.AccessNote = "آماده ارسال مستقیم از مسیر " + devPath
			} else {
				dev.AccessNote = "گره دستگاه موجود است اما دسترسی نوشتن ندارد؛ قواعد udev یا عضویت در گروه lp را بررسی کنید"
			}
		} else {
			dev.AccessNote = "دستگاه در رده Printer Class شناسایی شد اما گره usblp ندارد؛ برای چاپ از صف چاپ سیستم‌عامل استفاده کنید یا ماژول usblp را فعال کنید"
		}
		key := dev.Identity()
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, dev)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Product == out[j].Product {
			return out[i].PortPath < out[j].PortPath
		}
		return out[i].Product < out[j].Product
	})
	return out, nil
}

// usblpMap maps interface names (e.g. "1-1:1.0") to /dev/usb/lpN nodes via
// /sys/class/usbmisc. Only existing nodes are returned.
func usblpMap(sysRoot, devRoot string) (map[string]string, error) {
	dir := filepath.Join(sysRoot, "class", "usbmisc")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return map[string]string{}, err
	}
	out := map[string]string{}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "lp") {
			continue
		}
		devPath := filepath.Join(devRoot, "usb", name)
		if _, statErr := os.Stat(devPath); statErr != nil {
			continue
		}
		link := filepath.Join(dir, name, "device")
		target, readErr := os.Readlink(link)
		if readErr != nil {
			continue
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(link), target)
		}
		resolved, evalErr := filepath.EvalSymlinks(target)
		if evalErr != nil {
			resolved = filepath.Clean(target)
		}
		out[filepath.Base(resolved)] = devPath
	}
	return out, nil
}

func readSys(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func usbSupport() error { return nil }

func openUSBDevice(path string) (io.WriteCloser, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, err
	}
	return f, nil
}
