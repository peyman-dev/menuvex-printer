//go:build !linux

package printer

import (
	"context"
	"errors"
	"io"
	"runtime"
	"sort"
	"time"
)

// ErrUSBUnsupported is returned on systems without direct usblp device nodes.
var ErrUSBUnsupported = errors.New("چاپ مستقیم از مسیر دستگاه لینوکس در این سیستم‌عامل استفاده نمی‌شود؛ چاپ از طریق صف سیستم‌عامل انجام می‌شود")

// ErrUSBMissing indicates that no usblp device node can receive bytes.
var ErrUSBMissing = errors.New("گره دستگاه usblp در دسترس نیست")

// DiscoverUSB discovers USB-attached printers on Windows and macOS via the
// platform print/USB subsystem so operators can see and select USB printers on
// every operating system.
func DiscoverUSB() ([]USBDevice, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	details := platformQueueDetails(ctx)
	var out []USBDevice
	for _, d := range details {
		if d.Category != "usb" {
			continue
		}
		out = append(out, USBDevice{
			Product:      d.DriverModel,
			Manufacturer: d.Manufacturer,
			PortPath:     d.PortName,
			Accessible:   false,
			AccessNote:   "آماده انتخاب از طریق صف چاپ سیستم‌عامل (" + d.QueueName + ")",
			System:       runtime.GOOS,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Product < out[j].Product
	})
	return out, nil
}

func usbSupport() error { return ErrUSBUnsupported }

func openUSBDevice(path string) (io.WriteCloser, error) {
	return nil, ErrUSBUnsupported
}
