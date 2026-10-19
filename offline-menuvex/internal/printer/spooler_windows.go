//go:build windows

package printer

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// ErrSpoolerMissing means no usable Windows printing subsystem was found.
var ErrSpoolerMissing = errors.New("صف چاپ ویندوز پیدا نشد؛ نام صف را در «چاپگرها و دستگاه‌ها» بررسی کنید")

// ErrSpoolerUnsupported means raw spooler printing is not available on this OS.
var ErrSpoolerUnsupported = errors.New("ارسال خام به صف چاپ در این سیستم‌عامل پشتیبانی نمی‌شود")

// ListQueues uses the same local/connection enumeration as MenuVex Printer Agent
// (EnumPrintersW level 4), so installed USB and shared queues are found without
// requiring PowerShell, WMIC or any user-entered printer details.
func ListQueues(ctx context.Context) ([]string, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	const flags = printerEnumLocal | printerEnumConnections
	var needed, count uint32
	procEnumPrintersW.Call(
		uintptr(flags), 0, 4, 0, 0,
		uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&count)),
	)
	if needed == 0 {
		return []string{}, nil
	}
	if needed > maxPrinterEnumBytes {
		return nil, fmt.Errorf("فهرست صف‌های چاپ ویندوز بیش از حد مجاز بزرگ است (%d بایت)", needed)
	}

	buffer := make([]byte, int(needed))
	ok, _, callErr := procEnumPrintersW.Call(
		uintptr(flags), 0, 4, uintptr(unsafe.Pointer(&buffer[0])), uintptr(needed),
		uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&count)),
	)
	runtime.KeepAlive(buffer)
	if ok == 0 {
		if callErr == syscall.Errno(0) {
			return nil, errors.New("خواندن صف‌های چاپ ویندوز ناموفق بود")
		}
		return nil, fmt.Errorf("خواندن صف‌های چاپ ویندوز ناموفق بود: %w", callErr)
	}

	infoSize := unsafe.Sizeof(printerInfo4W{})
	if infoSize == 0 || uint64(count)*uint64(infoSize) > uint64(len(buffer)) {
		return nil, errors.New("پاسخ صف چاپ ویندوز ساختار نامعتبر دارد")
	}
	infos := unsafe.Slice((*printerInfo4W)(unsafe.Pointer(&buffer[0])), int(count))
	base := uintptr(unsafe.Pointer(&buffer[0]))
	end := base + uintptr(len(buffer))
	var names []string
	for _, info := range infos {
		if info.printerName == nil {
			continue
		}
		namePtr := uintptr(unsafe.Pointer(info.printerName))
		if namePtr < base || namePtr >= end {
			continue
		}
		maxUnits := (end - namePtr) / unsafe.Sizeof(uint16(0))
		var units []uint16
		for i := uintptr(0); i < maxUnits; i++ {
			unit := *(*uint16)(unsafe.Add(unsafe.Pointer(info.printerName), i*unsafe.Sizeof(uint16(0))))
			if unit == 0 {
				break
			}
			units = append(units, unit)
		}
		if len(units) != 0 {
			names = append(names, syscall.UTF16ToString(units))
		}
	}
	return ParseQueueList(strings.Join(names, "\n")), nil
}

var (
	winspool             = syscall.NewLazyDLL("winspool.drv")
	procEnumPrintersW    = winspool.NewProc("EnumPrintersW")
	procOpenPrinterW     = winspool.NewProc("OpenPrinterW")
	procClosePrinter     = winspool.NewProc("ClosePrinter")
	procStartDocPrinterW = winspool.NewProc("StartDocPrinterW")
	procStartPagePrinter = winspool.NewProc("StartPagePrinter")
	procWritePrinter     = winspool.NewProc("WritePrinter")
	procEndPagePrinter   = winspool.NewProc("EndPagePrinter")
	procEndDocPrinter    = winspool.NewProc("EndDocPrinter")
)

const (
	printerEnumLocal       = 0x00000002
	printerEnumConnections = 0x00000004
	maxPrinterEnumBytes    = 16 << 20
)

type printerInfo4W struct {
	printerName *uint16
	serverName  *uint16
	attributes  uint32
}

type printerInfo2W struct {
	serverName         *uint16
	printerName        *uint16
	shareName          *uint16
	portName           *uint16
	driverName         *uint16
	comment            *uint16
	location           *uint16
	devMode            uintptr
	sepFile            *uint16
	printProcessor     *uint16
	datatype           *uint16
	parameters         *uint16
	securityDescriptor uintptr
	attributes         uint32
	priority           uint32
	defaultPriority    uint32
	startTime          uint32
	untilTime          uint32
	status             uint32
	cJobs              uint32
	averagePPM         uint32
}

func readUTF16Bounded(ptr *uint16, base, end uintptr) string {
	if ptr == nil {
		return ""
	}
	addr := uintptr(unsafe.Pointer(ptr))
	if addr < base || addr >= end {
		return ""
	}
	maxUnits := (end - addr) / unsafe.Sizeof(uint16(0))
	var units []uint16
	for i := uintptr(0); i < maxUnits; i++ {
		unit := *(*uint16)(unsafe.Add(unsafe.Pointer(ptr), i*unsafe.Sizeof(uint16(0))))
		if unit == 0 {
			break
		}
		units = append(units, unit)
	}
	if len(units) == 0 {
		return ""
	}
	return syscall.UTF16ToString(units)
}

func platformQueueDetails(ctx context.Context) map[string]QueueDetail {
	details := map[string]QueueDetail{}
	select {
	case <-ctx.Done():
		return details
	default:
	}
	const flags = printerEnumLocal | printerEnumConnections
	var needed, count uint32
	procEnumPrintersW.Call(
		uintptr(flags), 0, 2, 0, 0,
		uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&count)),
	)
	if needed == 0 || needed > maxPrinterEnumBytes {
		return details
	}
	buffer := make([]byte, int(needed))
	ok, _, _ := procEnumPrintersW.Call(
		uintptr(flags), 0, 2, uintptr(unsafe.Pointer(&buffer[0])), uintptr(needed),
		uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&count)),
	)
	runtime.KeepAlive(buffer)
	if ok == 0 {
		return details
	}
	infoSize := unsafe.Sizeof(printerInfo2W{})
	if infoSize == 0 || uint64(count)*uint64(infoSize) > uint64(len(buffer)) {
		return details
	}
	infos := unsafe.Slice((*printerInfo2W)(unsafe.Pointer(&buffer[0])), int(count))
	base := uintptr(unsafe.Pointer(&buffer[0]))
	end := base + uintptr(len(buffer))
	for _, info := range infos {
		name := strings.TrimSpace(readUTF16Bounded(info.printerName, base, end))
		if name == "" || ValidateQueueName(name) != nil {
			continue
		}
		port := strings.TrimSpace(readUTF16Bounded(info.portName, base, end))
		driver := strings.TrimSpace(readUTF16Bounded(info.driverName, base, end))
		mfg, model, cat, portInfo, paper := InferPrinterSpecs(name, "", driver, port)
		details[name] = QueueDetail{
			QueueName:    name,
			DriverModel:  model,
			Manufacturer: mfg,
			PortName:     portInfo,
			Category:     cat,
			PaperMM:      paper,
		}
	}
	return details
}

type docInfo1 struct {
	pDocName    *uint16
	pOutputFile *uint16
	pDatatype   *uint16
}

// sendSpoolerRaw starts a RAW spool job through winspool.drv, matching the
// agent's winspool path (no GDI rendering, no shell).
func sendSpoolerRaw(ctx context.Context, queue string, payload []byte) error {
	if err := ValidateQueueName(queue); err != nil {
		return NewOpError("INVALID_CONFIG", err.Error())
	}
	if len(payload) == 0 {
		return NewOpError("EMPTY_PAYLOAD", "داده‌ای برای چاپ وجود ندارد")
	}
	type result struct{ err error }
	done := make(chan result, 1)
	go func() { done <- result{err: winspoolRaw(queue, payload)} }()
	select {
	case <-ctx.Done():
		return UncertainError("ارسال به صف ویندوز به پایان نرسید؛ ممکن است Job در صف مانده باشد")
	case <-time.After(25 * time.Second):
		return UncertainError("صف چاپ ویندوز در زمان مجاز پاسخ نداد؛ ممکن است Job در صف مانده باشد")
	case res := <-done:
		return res.err
	}
}

func winspoolRaw(queue string, payload []byte) error {
	printerName, err := syscall.UTF16PtrFromString(queue)
	if err != nil {
		return NewOpError("INVALID_CONFIG", "نام صف چاپ نامعتبر است")
	}
	var handle syscall.Handle
	ok, _, callErr := procOpenPrinterW.Call(
		uintptr(unsafe.Pointer(printerName)),
		uintptr(unsafe.Pointer(&handle)),
		0,
	)
	if ok == 0 {
		return NewOpError("SPOOLER_UNAVAILABLE", "باز کردن صف چاپ ویندوز ناموفق بود: "+callErr.Error())
	}
	defer procClosePrinter.Call(uintptr(handle))

	docName, _ := syscall.UTF16PtrFromString("MenuVex Offline Receipt")
	dataType, _ := syscall.UTF16PtrFromString("RAW")
	info := docInfo1{pDocName: docName, pOutputFile: nil, pDatatype: dataType}
	jobID, _, startErr := procStartDocPrinterW.Call(
		uintptr(handle), 1, uintptr(unsafe.Pointer(&info)),
	)
	if jobID == 0 {
		return NewOpError("SPOOLER_REJECTED", "صف چاپ Job را نپذیرفت: "+startErr.Error())
	}
	if ok, _, pageErr := procStartPagePrinter.Call(uintptr(handle)); ok == 0 {
		return UncertainError("شروع صفحه در صف چاپ ناموفق بود: " + pageErr.Error())
	}
	var written uint32
	ok, _, writeErr := procWritePrinter.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&payload[0])),
		uintptr(len(payload)),
		uintptr(unsafe.Pointer(&written)),
	)
	procEndPagePrinter.Call(uintptr(handle))
	procEndDocPrinter.Call(uintptr(handle))
	if ok == 0 {
		return UncertainError("نوشتن در صف چاپ ویندوز ناموفق بود: " + writeErr.Error())
	}
	if int(written) != len(payload) {
		return UncertainError("فقط بخشی از فیش در صف چاپ ویندوز نوشته شد")
	}
	return nil
}
