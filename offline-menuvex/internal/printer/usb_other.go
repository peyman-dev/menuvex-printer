//go:build !linux

package printer

import (
	"errors"
	"io"
)

// ErrUSBUnsupported is returned on systems without direct USB access.
var ErrUSBUnsupported = errors.New("شناسایی و چاپ مستقیم USB در این سیستم‌عامل پشتیبانی نمی‌شود؛ چاپگر را به‌صورت صف چاپ سیستم‌عامل اضافه کنید")

// ErrUSBMissing indicates that no usblp device node can receive bytes.
var ErrUSBMissing = errors.New("گره دستگاه usblp در دسترس نیست")

// DiscoverUSB reports that direct USB discovery is not implemented here. The
// audited Agent uses libusb directly; the Go rewrite avoids a C dependency and
// therefore relies on the operating system's print queue instead.
func DiscoverUSB() ([]USBDevice, error) {
	return nil, ErrUSBUnsupported
}

func usbSupport() error { return ErrUSBUnsupported }

func openUSBDevice(path string) (io.WriteCloser, error) {
	return nil, ErrUSBUnsupported
}
