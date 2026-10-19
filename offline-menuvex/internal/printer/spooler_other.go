//go:build !linux && !darwin && !windows

package printer

import (
	"context"
	"errors"
)

// ErrSpoolerMissing means no usable printing subsystem was found.
var ErrSpoolerMissing = errors.New("صف چاپ سیستم‌عامل در این سیستم پشتیبانی نمی‌شود")

// ErrSpoolerUnsupported means raw spooler printing is not available.
var ErrSpoolerUnsupported = errors.New("ارسال خام به صف چاپ در این سیستم‌عامل پشتیبانی نمی‌شود")

// ListQueues is not implemented on this platform.
func ListQueues(ctx context.Context) ([]string, error) {
	return nil, ErrSpoolerUnsupported
}

func sendSpoolerRaw(ctx context.Context, queue string, payload []byte) error {
	return NewOpError("SPOOLER_UNAVAILABLE", ErrSpoolerUnsupported.Error())
}
