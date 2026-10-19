//go:build windows

package printer

import (
	"context"
	"errors"
	"os/exec"
	"syscall"
	"time"
	"unsafe"
)

// ErrSpoolerMissing means no usable Windows printing subsystem was found.
var ErrSpoolerMissing = errors.New("صف چاپ ویندوز پیدا نشد؛ نام صف را در «چاپگرها و دستگاه‌ها» بررسی کنید")

// ErrSpoolerUnsupported means raw spooler printing is not available on this OS.
var ErrSpoolerUnsupported = errors.New("ارسال خام به صف چاپ در این سیستم‌عامل پشتیبانی نمی‌شود")

// ListQueues enumerates installed printers through the Windows printing
// subsystem. PowerShell is invoked with a fixed command, never user input.
func ListQueues(ctx context.Context) ([]string, error) {
	shell, err := exec.LookPath("powershell.exe")
	if err != nil {
		if wmic, wErr := exec.LookPath("wmic.exe"); wErr == nil {
			cctx, cancel := context.WithTimeout(ctx, 6*time.Second)
			defer cancel()
			out, runErr := exec.CommandContext(cctx, wmic, "printer", "get", "name").Output()
			if runErr != nil {
				return nil, ErrSpoolerMissing
			}
			return ParseQueueList(string(out)), nil
		}
		return nil, ErrSpoolerMissing
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, shell, "-NoProfile", "-NonInteractive", "-Command",
		"Get-Printer | Select-Object -ExpandProperty Name").Output()
	if err != nil {
		return nil, errors.New("خواندن صف‌های چاپ ویندوز ناموفق بود: " + err.Error())
	}
	return ParseQueueList(string(out)), nil
}

var (
	winspool             = syscall.NewLazyDLL("winspool.drv")
	procOpenPrinterW     = winspool.NewProc("OpenPrinterW")
	procClosePrinter     = winspool.NewProc("ClosePrinter")
	procStartDocPrinterW = winspool.NewProc("StartDocPrinterW")
	procStartPagePrinter = winspool.NewProc("StartPagePrinter")
	procWritePrinter     = winspool.NewProc("WritePrinter")
	procEndPagePrinter   = winspool.NewProc("EndPagePrinter")
	procEndDocPrinter    = winspool.NewProc("EndDocPrinter")
)

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
