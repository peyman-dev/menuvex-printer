package printer

import "fmt"

// Stable API error codes. These are part of the public API contract:
// web applications match on Code, never on the human Message.
const (
	CodeInvalidRequest     = "INVALID_REQUEST"
	CodeUnauthorized       = "UNAUTHORIZED"
	CodeForbiddenOrigin    = "FORBIDDEN_ORIGIN"
	CodePrinterNotFound    = "PRINTER_NOT_FOUND"
	CodeInvalidPrinterID   = "INVALID_PRINTER_ID"
	CodePrinterConnFailed  = "PRINTER_CONNECTION_FAILED"
	CodePrinterWriteFailed = "PRINTER_WRITE_FAILED"
	CodePrinterTimeout     = "PRINTER_TIMEOUT"
	CodePrinterBusy        = "PRINTER_BUSY"
	CodeUSBUnsupported     = "USB_UNSUPPORTED"
	CodeUSBDeviceNotFound  = "USB_DEVICE_NOT_FOUND"
	CodeUSBError           = "USB_ERROR"
	CodeScanFailed         = "SCAN_FAILED"
	CodePayloadTooLarge    = "PAYLOAD_TOO_LARGE"
	CodeInternalError      = "INTERNAL_ERROR"
	CodeMethodNotAllowed   = "METHOD_NOT_ALLOWED"
	CodeNotFound           = "NOT_FOUND"
)

// Error is a printer operation failure with a stable machine-readable code.
type Error struct {
	Code      string
	Message   string
	PrinterID string
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.PrinterID != "" {
		return fmt.Sprintf("%s: %s (printer %s)", e.Code, e.Message, e.PrinterID)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// NewError builds an *Error with a formatted message.
func NewError(code, printerID, format string, args ...interface{}) *Error {
	return &Error{Code: code, PrinterID: printerID, Message: fmt.Sprintf(format, args...)}
}

// AsError unwraps err to *Error if possible.
func AsError(err error) (*Error, bool) {
	if e, ok := err.(*Error); ok {
		return e, true
	}
	return nil, false
}
