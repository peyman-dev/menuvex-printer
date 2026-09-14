//go:build windows

package usb

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

// Windows backend: Winspool RAW passthrough.
//
// How it works: the printer must be installed in Windows (Settings >
// Printers). The agent opens it with OpenPrinter and sends the payload with
// WritePrinter using the RAW datatype, which passes bytes to the device
// unmodified — the same mechanism QZ Tray and most POS software use.
//
// Honest limitations (see docs/USB.md):
//
//   - Only printers installed in Windows on a USB port (USB001, …) are
//     listed. A USB printer with no Windows printer object is NOT visible.
//   - Vendor/product IDs are not exposed by the spooler API, so they are
//     reported as unknown (0). Printers are identified by spooler name:
//     id = "usb:win:<Printer Name>".
//   - No WinUSB/Zadig driver replacement is required.

const (
	windowsDetail    = "Windows Winspool RAW backend (printers installed on USB ports)"
	windowsIDPref    = "usb:win:"
	printerEnumLocal = 0x00000002
	printerEnumConns = 0x00000004
)

func newPlatformBackend() (USBPrinter, bool, string) {
	return &windowsBackend{open: make(map[string]syscall.Handle)}, true, windowsDetail
}

var (
	modWinspool      = syscall.NewLazyDLL("winspool.drv")
	procEnumPrinters = modWinspool.NewProc("EnumPrintersW")
	procOpenPrinter  = modWinspool.NewProc("OpenPrinterW")
	procClosePrinter = modWinspool.NewProc("ClosePrinter")
	procStartDoc     = modWinspool.NewProc("StartDocPrinterW")
	procEndDoc       = modWinspool.NewProc("EndDocPrinterW")
	procWritePrinter = modWinspool.NewProc("WritePrinter")
)

// printerInfo2 mirrors Win32 PRINTER_INFO_2W. Field order and sizes must
// match the C struct exactly.
type printerInfo2 struct {
	ServerName         *uint16
	PrinterName        *uint16
	ShareName          *uint16
	PortName           *uint16
	DriverName         *uint16
	Comment            *uint16
	Location           *uint16
	DevMode            *byte
	SepFile            *uint16
	PrintProcessor     *uint16
	Datatype           *uint16
	Parameters         *uint16
	SecurityDescriptor *byte
	Attributes         uint32
	Priority           uint32
	DefaultPriority    uint32
	StartTime          uint32
	UntilTime          uint32
	Status             uint32
	Jobs               uint32
	AveragePPM         uint32
}

// docInfo1 mirrors Win32 DOC_INFO_1W.
type docInfo1 struct {
	DocName    *uint16
	OutputFile *uint16
	Datatype   *uint16
}

type windowsBackend struct {
	mu   sync.Mutex
	open map[string]syscall.Handle // deviceID -> printer handle
}

func (b *windowsBackend) ListDevices() ([]USBDevice, error) {
	infos, err := enumLocalPrinters()
	if err != nil {
		return nil, err
	}
	var out []USBDevice
	for _, info := range infos {
		port := utf16ToString(info.PortName)
		if !strings.HasPrefix(strings.ToUpper(port), "USB") {
			continue
		}
		name := utf16ToString(info.PrinterName)
		if name == "" {
			continue
		}
		driver := utf16ToString(info.DriverName)
		out = append(out, USBDevice{
			ID:     windowsIDPref + name,
			Name:   name,
			Detail: fmt.Sprintf("port %s, driver %s", port, driver),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func enumLocalPrinters() ([]printerInfo2, error) {
	flags := uintptr(printerEnumLocal | printerEnumConns)
	var needed, returned uint32
	// First call: query required buffer size (expected to fail).
	_, _, _ = procEnumPrinters.Call(flags, 0, 2, 0, 0,
		uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&returned)))
	if needed == 0 {
		return nil, nil // no printers installed
	}
	buf := make([]byte, needed)
	r1, _, errno := procEnumPrinters.Call(flags, 0, 2,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(needed),
		uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&returned)))
	if r1 == 0 {
		return nil, spoolErr("EnumPrinters", errno)
	}
	if returned == 0 {
		return nil, nil
	}
	out := make([]printerInfo2, 0, returned)
	elem := unsafe.Sizeof(printerInfo2{})
	base := uintptr(unsafe.Pointer(&buf[0]))
	for i := uint32(0); i < returned; i++ {
		info := (*printerInfo2)(unsafe.Pointer(base + uintptr(i)*elem))
		out = append(out, *info)
	}
	return out, nil
}

func utf16ToString(p *uint16) string {
	if p == nil {
		return ""
	}
	const maxLen = 1 << 15
	raw := (*[maxLen]uint16)(unsafe.Pointer(p))
	n := 0
	for n < maxLen && raw[n] != 0 {
		n++
	}
	return syscall.UTF16ToString(raw[:n])
}

func spoolName(deviceID string) (string, error) {
	if !strings.HasPrefix(deviceID, windowsIDPref) {
		return "", fmt.Errorf("%w: %s", ErrDeviceNotFound, deviceID)
	}
	name := strings.TrimPrefix(deviceID, windowsIDPref)
	if name == "" {
		return "", fmt.Errorf("%w: %s", ErrDeviceNotFound, deviceID)
	}
	return name, nil
}

func openPrinterHandle(name string) (syscall.Handle, error) {
	namePtr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	var h syscall.Handle
	r1, _, errno := procOpenPrinter.Call(
		uintptr(unsafe.Pointer(namePtr)),
		uintptr(unsafe.Pointer(&h)),
		0, // pDefault: NULL
	)
	if r1 == 0 {
		if errno == syscall.Errno(1801) { // ERROR_INVALID_PRINTER_NAME
			return 0, fmt.Errorf("%w: %s", ErrDeviceNotFound, name)
		}
		return 0, spoolErr("OpenPrinter", errno)
	}
	return h, nil
}

func (b *windowsBackend) Open(deviceID string) error {
	name, err := spoolName(deviceID)
	if err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.open[deviceID]; ok {
		return nil
	}
	h, err := openPrinterHandle(name)
	if err != nil {
		return err
	}
	b.open[deviceID] = h
	return nil
}

func (b *windowsBackend) Close(deviceID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	h, ok := b.open[deviceID]
	if !ok {
		return nil
	}
	delete(b.open, deviceID)
	r1, _, errno := procClosePrinter.Call(uintptr(h))
	if r1 == 0 {
		return spoolErr("ClosePrinter", errno)
	}
	return nil
}

func (b *windowsBackend) Write(deviceID string, data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("empty payload")
	}
	name, err := spoolName(deviceID)
	if err != nil {
		return err
	}
	b.mu.Lock()
	h, held := b.open[deviceID]
	b.mu.Unlock()

	transient := false
	if !held {
		h, err = openPrinterHandle(name)
		if err != nil {
			return err
		}
		transient = true
	}
	if transient {
		defer procClosePrinter.Call(uintptr(h))
	}
	return writeRawJob(h, data)
}

// writeRawJob sends one RAW spooler job on an open printer handle.
func writeRawJob(h syscall.Handle, data []byte) error {
	docName, _ := syscall.UTF16PtrFromString("Novex Print Job")
	rawType, _ := syscall.UTF16PtrFromString("RAW")
	doc := docInfo1{DocName: docName, OutputFile: nil, Datatype: rawType}
	jobID, _, errno := procStartDoc.Call(uintptr(h), 1, uintptr(unsafe.Pointer(&doc)))
	if jobID == 0 {
		return spoolErr("StartDocPrinter", errno)
	}
	// Always end the job, even after a failed write.
	defer procEndDoc.Call(uintptr(h))

	sent := 0
	for sent < len(data) {
		chunk := data[sent:]
		var written uint32
		var p *byte
		if len(chunk) > 0 {
			p = &chunk[0]
		}
		r1, _, errno := procWritePrinter.Call(uintptr(h),
			uintptr(unsafe.Pointer(p)), uintptr(len(chunk)),
			uintptr(unsafe.Pointer(&written)))
		if r1 == 0 {
			return spoolErr("WritePrinter", errno)
		}
		if written == 0 {
			return fmt.Errorf("WritePrinter wrote 0 bytes")
		}
		sent += int(written)
	}
	return nil
}

func spoolErr(op string, err error) error {
	if err == nil {
		return fmt.Errorf("Winspool %s failed", op)
	}
	if errno, ok := err.(syscall.Errno); ok && errno == 0 {
		return fmt.Errorf("Winspool %s failed", op)
	}
	return fmt.Errorf("Winspool %s failed: %v", op, err)
}
