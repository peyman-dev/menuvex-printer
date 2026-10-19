package printer

import "fmt"

// OpError is a structured printer operation error. The queue worker persists
// its code and message; the UI uses Retryable and Uncertain to decide whether a
// retry button is safe. The semantics mirror the Agent's AgentError.
type OpError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
	Uncertain bool   `json:"uncertain"`
}

func (e *OpError) Error() string {
	if e == nil {
		return ""
	}
	return e.Code + ": " + e.Message
}

// NewOpError creates a non-retryable, certain error.
func NewOpError(code, message string) *OpError {
	return &OpError{Code: code, Message: message}
}

// RetryableError marks a failure that happened before any job byte was handed
// to a device or queue.
func RetryableError(code, message string) *OpError {
	return &OpError{Code: code, Message: message, Retryable: true}
}

// UncertainError marks a failure after at least one byte may have reached the
// printer. It must never be retried automatically or offered as a safe retry.
func UncertainError(message string) *OpError {
	return &OpError{
		Code:      "PRINT_OUTCOME_UNKNOWN",
		Message:   message,
		Uncertain: true,
	}
}

// AsOpError converts any transport error to a structured error.
func AsOpError(err error) *OpError {
	if err == nil {
		return nil
	}
	if op, ok := err.(*OpError); ok {
		return op
	}
	return NewOpError("PRINTER_ERROR", err.Error())
}

// IsUncertain reports whether an error means the physical outcome is unknown.
func IsUncertain(err error) bool {
	op, ok := err.(*OpError)
	return ok && op.Uncertain
}

// IsRetryable reports whether an error is safe to retry before any bytes were
// handed off.
func IsRetryable(err error) bool {
	op, ok := err.(*OpError)
	return ok && op.Retryable && !op.Uncertain
}

func describe(code, message string) string {
	return fmt.Sprintf("%s: %s", code, message)
}
