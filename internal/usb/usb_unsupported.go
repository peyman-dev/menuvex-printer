//go:build !linux && !windows && !darwin

package usb

// Fallback for platforms without a USB backend. USB operations fail
// loudly with USB_UNSUPPORTED instead of pretending to work.

func newPlatformBackend() (USBPrinter, bool, string) {
	return nil, false, "USB printing is not implemented for this platform (only Windows, macOS and Linux are supported)"
}
