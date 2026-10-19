//go:build linux || darwin

package printer

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

// ErrSpoolerMissing means this OS has CUPS tooling but it is not installed.
var ErrSpoolerMissing = errors.New("ابزار صف چاپ (CUPS/lpstat) روی این دستگاه پیدا نشد؛ چاپگر را با آدرس شبکه اضافه کنید یا CUPS را نصب کنید")

// ErrSpoolerUnsupported means raw spooler printing is not available on this OS.
var ErrSpoolerUnsupported = errors.New("ارسال خام به صف چاپ در این سیستم‌عامل پشتیبانی نمی‌شود")

// ListQueues returns installed CUPS queue names. lpstat is invoked with a
// fixed argument list, never through a shell.
func ListQueues(ctx context.Context) ([]string, error) {
	lpstat, err := exec.LookPath("lpstat")
	if err != nil {
		return nil, ErrSpoolerMissing
	}
	cctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, lpstat, "-e").Output()
	if err != nil {
		if cctx.Err() != nil {
			return nil, errors.New("خواندن صف‌های چاپ سیستم‌عامل زمان‌بندی شد و تمام شد")
		}
		return nil, errors.New("خواندن صف‌های چاپ سیستم‌عامل ناموفق بود: " + err.Error())
	}
	return ParseQueueList(string(out)), nil
}

// sendSpoolerRaw submits bytes to one CUPS queue as a RAW job. Payload is
// passed on stdin so no receipt text ever appears in the process list.
func sendSpoolerRaw(ctx context.Context, queue string, payload []byte) error {
	if err := ValidateQueueName(queue); err != nil {
		return NewOpError("INVALID_CONFIG", err.Error())
	}
	lp, err := exec.LookPath("lp")
	if err != nil {
		return NewOpError("SPOOLER_UNAVAILABLE", ErrSpoolerMissing.Error())
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, lp, "-d", queue, "-o", "raw")
	cmd.Stdin = bytes.NewReader(payload)
	out, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(out))
		if message == "" {
			message = err.Error()
		}
		if cctx.Err() != nil {
			return UncertainError("صف چاپ در زمان مجاز پاسخ نداد؛ ممکن است Job در صف مانده باشد: " + message)
		}
		return UncertainError("صف چاپ خطا داد؛ وضعیت ارسال نامشخص است: " + message)
	}
	return nil
}
