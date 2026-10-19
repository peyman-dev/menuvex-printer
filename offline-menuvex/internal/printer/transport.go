package printer

import (
	"context"
	"io"
	"net"
	"strconv"
	"time"
)

// PayloadLimit mirrors the Agent's 1 MiB ESC/POS hard limit. A receipt that
// large is certainly a bug and must never be streamed to hardware.
const PayloadLimit = 1 << 20

// Sender writes an already-rendered ESC/POS payload to exactly one configured
// transport. It never retries by itself: retry policy belongs to the queue
// worker so the same job cannot be printed twice by accident.
type Sender struct {
	Dial           DialFunc
	ConnectTimeout time.Duration
	WriteTimeout   time.Duration
	QueueSend      func(ctx context.Context, queue string, payload []byte) error
	USBOpen        func(path string) (io.WriteCloser, error)
	USBAvailable   func() error
}

// NewSender builds the production sender. OS-specific operations are provided
// by build-tagged files, so no shell or path logic leaks into shared code.
func NewSender() *Sender {
	return &Sender{
		Dial:           DefaultDial,
		ConnectTimeout: 3 * time.Second,
		WriteTimeout:   10 * time.Second,
		QueueSend:      sendSpoolerRaw,
		USBOpen:        openUSBDevice,
		USBAvailable:   usbSupport,
	}
}

// Send hands the payload to the configured destination.
//
// Contract:
//   - nil means the bytes were accepted by the transport (TCP write completed,
//     spooler accepted the job, or the device write finished). It does NOT mean
//     the printer physically printed anything; RAW printers have no ack.
//   - *OpError with Uncertain=true means bytes may have reached the device; the
//     caller must never retry automatically.
func (s *Sender) Send(ctx context.Context, p Profile, payload []byte) error {
	if len(payload) == 0 {
		return NewOpError("EMPTY_PAYLOAD", "داده‌ای برای چاپ وجود ندارد")
	}
	if len(payload) > PayloadLimit {
		return NewOpError("PAYLOAD_TOO_LARGE", "حجم فیش بیش از حد مجاز (۱ مگابایت) است")
	}
	p = p.Normalize()
	switch p.Kind {
	case KindNetwork:
		return s.sendNetwork(ctx, p, payload)
	case KindSpooler:
		return s.sendQueue(ctx, p, payload)
	case KindUSB:
		return s.sendUSB(ctx, p, payload)
	default:
		return NewOpError("INVALID_CONFIG", "نوع اتصال چاپگر پشتیبانی نمی‌شود")
	}
}

func (s *Sender) sendNetwork(ctx context.Context, p Profile, payload []byte) error {
	if err := ValidateNetworkTarget(p.Host, p.Port); err != nil {
		return NewOpError("INVALID_CONFIG", err.Error())
	}
	dial := s.Dial
	if dial == nil {
		dial = DefaultDial
	}
	timeout := s.ConnectTimeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	addr := net.JoinHostPort(p.Host, strconv.Itoa(p.Port))
	conn, err := dial(dialCtx, "tcp", addr)
	if err != nil {
		return RetryableError("PRINTER_OFFLINE", "اتصال به چاپگر شبکه ("+addr+") برقرار نشد: "+err.Error())
	}
	defer conn.Close()
	writeTimeout := s.WriteTimeout
	if writeTimeout <= 0 {
		writeTimeout = 10 * time.Second
	}
	_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	written, werr := conn.Write(payload)
	if werr != nil {
		if written > 0 {
			return UncertainError("ارسال فیش نیمه‌کاره ماند؛ ممکن است بخشی از آن چاپ شده باشد: " + werr.Error())
		}
		if isTimeout(werr) {
			return UncertainError("نوشتن روی چاپگر شبکه به پایان نرسید: " + werr.Error())
		}
		return RetryableError("PRINT_WRITE_FAILED", "ارسال داده به چاپگر شبکه ناموفق بود: "+werr.Error())
	}
	if written != len(payload) {
		return UncertainError("فقط بخشی از فیش به چاپگر شبکه ارسال شد")
	}
	return nil
}

func (s *Sender) sendQueue(ctx context.Context, p Profile, payload []byte) error {
	if err := ValidateQueueName(p.QueueName); err != nil {
		return NewOpError("INVALID_CONFIG", err.Error())
	}
	if s.QueueSend == nil {
		return NewOpError("SPOOLER_UNAVAILABLE", "ارسال خام به صف چاپ در این سیستم‌عامل پشتیبانی نمی‌شود")
	}
	if err := s.QueueSend(ctx, p.QueueName, payload); err != nil {
		if op, ok := err.(*OpError); ok {
			return op
		}
		return UncertainError("نتیجهٔ ارسال به صف چاپ نامشخص است: " + err.Error())
	}
	return nil
}

func (s *Sender) sendUSB(ctx context.Context, p Profile, payload []byte) error {
	if p.USB.DevicePath == "" {
		return NewOpError("USB_QUEUE_REQUIRED",
			"چاپ مستقیم USB برای این دستگاه ممکن نیست؛ چاپگر را به‌صورت «صف چاپ سیستم‌عامل» اضافه کنید")
	}
	if err := ValidateUSBPath(p.USB.DevicePath); err != nil {
		return NewOpError("INVALID_CONFIG", err.Error())
	}
	if s.USBAvailable != nil {
		if err := s.USBAvailable(); err != nil {
			return NewOpError("USB_UNAVAILABLE", err.Error())
		}
	}
	if s.USBOpen == nil {
		return NewOpError("USB_UNAVAILABLE", "دسترسی مستقیم به دستگاه USB در این ساختار فعال نیست")
	}
	conn, err := s.USBOpen(p.USB.DevicePath)
	if err != nil {
		return NewOpError("USB_OPEN_FAILED", "باز کردن دستگاه USB ناموفق بود: "+err.Error())
	}
	defer conn.Close()

	timeout := s.WriteTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	type result struct {
		written int
		err     error
	}
	done := make(chan result, 1)
	go func() {
		n, wErr := conn.Write(payload)
		done <- result{written: n, err: wErr}
	}()
	select {
	case <-ctx.Done():
		_ = conn.Close()
		return UncertainError("ارسال USB به پایان نرسید؛ ممکن است بخشی از فیش چاپ شده باشد")
	case <-time.After(timeout):
		_ = conn.Close()
		return UncertainError("دستگاه USB در زمان مجاز پاسخ نداد؛ ممکن است بخشی از فیش چاپ شده باشد")
	case res := <-done:
		if res.err != nil {
			if res.written > 0 {
				return UncertainError("ارسال USB نیمه‌کاره ماند؛ ممکن است بخشی از فیش چاپ شده باشد: " + res.err.Error())
			}
			return RetryableError("USB_WRITE_FAILED", "نوشتن روی دستگاه USB ناموفق بود: "+res.err.Error())
		}
		if res.written != len(payload) {
			return UncertainError("فقط بخشی از فیش به دستگاه USB نوشته شد")
		}
		return nil
	}
}

func isTimeout(err error) bool {
	type timeout interface{ Timeout() bool }
	if t, ok := err.(timeout); ok && t.Timeout() {
		return true
	}
	return false
}
