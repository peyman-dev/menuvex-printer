// Package usb abstracts USB printer access per operating system.
//
// There is no single cross-platform "WebUSB" API at OS level, so each
// platform uses its native mechanism (stdlib only, no libusb/CGO):
//
//	Linux:   direct writes to usblp device nodes (/dev/usb/lp*),
//	        discovery via sysfs (/sys/class/usblp).
//	Windows: Winspool RAW passthrough (OpenPrinter/WritePrinter) via
//	        stdlib syscall. Requires the printer to be installed in
//	        Windows; bytes are sent unmodified with the RAW datatype.
//	macOS:   CUPS IPP (localhost:631) to a Raw USB queue. Requires the
//	        printer to be added as a Raw queue; bytes are sent as
//	        application/octet-stream.
//
// See docs/USB.md for setup steps and honest platform limitations.
package usb

import "errors"

// ErrUnsupported is returned when USB printing is unavailable on this
// platform or build.
var ErrUnsupported = errors.New("USB printing is not supported on this platform or build")

// ErrDeviceNotFound is returned when the addressed USB device is gone.
var ErrDeviceNotFound = errors.New("USB device not found")

// UnsupportedMessage explains the lack of USB support to API consumers.
func UnsupportedMessage() string {
	_, detail := SupportInfo()
	if detail == "" {
		return ErrUnsupported.Error()
	}
	return detail
}

// USBDevice describes one discovered USB printer.
type USBDevice struct {
	ID           string
	Name         string
	VendorID     int
	ProductID    int
	Serial       string
	Manufacturer string
	Product      string
	Detail       string // backend-specific locator (device node, port, queue URI...)
}

// USBPrinter is the abstraction every platform backend implements.
type USBPrinter interface {
	ListDevices() ([]USBDevice, error)
	Open(deviceID string) error
	Close(deviceID string) error
	Write(deviceID string, data []byte) error
}

// New returns the platform USB backend. When USB is unsupported it returns
// (nil, false, detail) — callers must handle a nil backend gracefully and
// must never report USB printers as available in that case.
func New() (USBPrinter, bool, string) {
	return newPlatformBackend()
}

// SupportInfo reports whether USB printing is implemented on this platform
// and a human-readable detail string.
func SupportInfo() (supported bool, detail string) {
	_, supported, detail = newPlatformBackend()
	return supported, detail
}
