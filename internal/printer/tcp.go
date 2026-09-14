package printer

import (
	"net"
	"strconv"
	"strings"
	"time"
)

// Printer ID schemes:
//
//	tcp:<host>:<port>   LAN/TCP printer, e.g. tcp:192.168.1.50:9100
//	usb:<backend-id>    USB printer, backend-specific suffix
const (
	SchemeTCP = "tcp:"
	SchemeUSB = "usb:"
)

// ParseTCPPrinterID parses "tcp:<host>:<port>" into host and port.
// The port is never assumed: it is always part of the ID.
func ParseTCPPrinterID(id string) (host string, port int, err error) {
	if !strings.HasPrefix(id, SchemeTCP) {
		return "", 0, NewError(CodeInvalidPrinterID, id, "expected id starting with %q", SchemeTCP)
	}
	rest := strings.TrimPrefix(id, SchemeTCP)
	h, p, err := net.SplitHostPort(rest)
	if err != nil {
		return "", 0, NewError(CodeInvalidPrinterID, id, "expected format tcp:<host>:<port>")
	}
	if h == "" {
		return "", 0, NewError(CodeInvalidPrinterID, id, "host must not be empty")
	}
	portNum, err := strconv.Atoi(p)
	if err != nil || portNum < 1 || portNum > 65535 {
		return "", 0, NewError(CodeInvalidPrinterID, id, "port %q out of range (1-65535)", p)
	}
	return h, portNum, nil
}

// TCPPrinterID builds a canonical printer ID for a host/port pair.
func TCPPrinterID(host string, port int) string {
	return SchemeTCP + net.JoinHostPort(host, strconv.Itoa(port))
}

// dialTCP opens a TCP connection with a connection timeout.
func dialTCP(host string, port int, timeout time.Duration) (net.Conn, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		if isTimeoutErr(err) {
			return nil, NewError(CodePrinterTimeout, TCPPrinterID(host, port),
				"connection to %s timed out after %s", addr, timeout)
		}
		return nil, NewError(CodePrinterConnFailed, TCPPrinterID(host, port),
			"unable to connect to %s: %s", addr, shortErr(err))
	}
	return conn, nil
}

// writeAll sends the full payload with a write deadline.
// Bytes are passed through untouched (RAW printing).
func writeAll(conn net.Conn, id string, data []byte, timeout time.Duration) error {
	if len(data) == 0 {
		return NewError(CodeInvalidRequest, id, "print payload must not be empty")
	}
	if err := conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return NewError(CodeInternalError, id, "cannot set write deadline: %s", shortErr(err))
	}
	written := 0
	for written < len(data) {
		n, err := conn.Write(data[written:])
		written += n
		if err != nil {
			if isTimeoutErr(err) {
				return NewError(CodePrinterTimeout, id,
					"write to printer timed out after %s (%d/%d bytes sent)",
					timeout, written, len(data))
			}
			return NewError(CodePrinterWriteFailed, id,
				"write failed after %d/%d bytes: %s", written, len(data), shortErr(err))
		}
		if n == 0 {
			return NewError(CodePrinterWriteFailed, id, "connection closed by printer")
		}
	}
	return nil
}

// isTimeoutErr reports whether err is (or wraps) a timeout.
func isTimeoutErr(err error) bool {
	if err == nil {
		return false
	}
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return true
	}
	// Fallback for wrapped timeouts.
	return strings.Contains(strings.ToLower(err.Error()), "timed out") ||
		strings.Contains(strings.ToLower(err.Error()), "timeout")
}

// shortErr keeps error messages one line.
func shortErr(err error) string {
	msg := err.Error()
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	return strings.TrimSpace(msg)
}
