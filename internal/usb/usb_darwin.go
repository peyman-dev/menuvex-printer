//go:build darwin

package usb

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// macOS backend: CUPS IPP to a Raw USB queue.
//
// How it works: macOS does not offer a stable user-space USB bulk API
// without IOKit/CGO, so the agent speaks IPP (Internet Printing Protocol,
// pure HTTP + binary encoding, stdlib only) to the local CUPS daemon at
// http://127.0.0.1:631 and submits the payload as application/octet-stream.
//
// Honest requirements (see docs/USB.md):
//
//   - The USB printer must be added to CUPS as a *Raw* queue
//     (lpadmin -p NAME -E -v <usb-uri> -m raw). Only then are bytes passed
//     through unmodified.
//   - Only CUPS queues whose device-uri starts with "usb:" are listed.
//     Vendor/product IDs are not exposed by CUPS, so they are reported as
//     unknown (0). Printers are identified by queue name:
//     id = "usb:cups:<Queue Name>".
//   - CUPS must be running and reachable on 127.0.0.1:631.

const (
	darwinDetail  = "macOS CUPS IPP backend (Raw USB queues on localhost:631)"
	darwinIDPref  = "usb:cups:"
	cupsBase      = "http://127.0.0.1:631"
	cupsDiscovery = 10 * time.Second
	cupsPrint     = 30 * time.Second
)

const (
	ippOpPrintJob   = 0x0002
	ippOpGetPrinter = 0x4002

	ippTagOpAttrs      = 0x01
	ippTagPrinterAttrs = 0x04
	ippTagEnd          = 0x03

	ippTagCharset = 0x47
	ippTagLang    = 0x48
	ippTagURI     = 0x45
	ippTagName    = 0x42
	ippTagKeyword = 0x44
	ippTagMime    = 0x49
)

func newPlatformBackend() (USBPrinter, bool, string) {
	return &darwinBackend{open: make(map[string]bool)}, true, darwinDetail
}

type darwinBackend struct {
	mu    sync.Mutex
	open  map[string]bool
	reqID int32
}

func (b *darwinBackend) nextReqID() int32 {
	return atomic.AddInt32(&b.reqID, 1)
}

func (b *darwinBackend) ListDevices() ([]USBDevice, error) {
	printers, err := cupsGetPrinters()
	if err != nil {
		return nil, err
	}
	var out []USBDevice
	for _, p := range printers {
		name := p.attrs["printer-name"]
		uri := p.attrs["device-uri"]
		if name == "" || uri == "" {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(uri), "usb:") {
			continue
		}
		make_, model, serial := parseUSBURI(uri)
		display := name
		if model != "" {
			display = fmt.Sprintf("%s (%s)", name, model)
		}
		out = append(out, USBDevice{
			ID:           darwinIDPref + name,
			Name:         display,
			Serial:       serial,
			Manufacturer: make_,
			Product:      model,
			Detail:       uri,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// queueName validates the ID and returns the CUPS queue name.
func queueName(deviceID string) (string, error) {
	if !strings.HasPrefix(deviceID, darwinIDPref) {
		return "", fmt.Errorf("%w: %s", ErrDeviceNotFound, deviceID)
	}
	name := strings.TrimPrefix(deviceID, darwinIDPref)
	if name == "" {
		return "", fmt.Errorf("%w: %s", ErrDeviceNotFound, deviceID)
	}
	return name, nil
}

func printerURI(name string) string {
	return cupsBase + "/printers/" + url.PathEscape(name)
}

func (b *darwinBackend) Open(deviceID string) error {
	if _, err := queueName(deviceID); err != nil {
		return err
	}
	devs, err := b.ListDevices()
	if err != nil {
		return err
	}
	for _, d := range devs {
		if d.ID == deviceID {
			b.mu.Lock()
			b.open[deviceID] = true
			b.mu.Unlock()
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrDeviceNotFound, deviceID)
}

func (b *darwinBackend) Close(deviceID string) error {
	if _, err := queueName(deviceID); err != nil {
		return err
	}
	b.mu.Lock()
	delete(b.open, deviceID)
	b.mu.Unlock()
	return nil
}

func (b *darwinBackend) Write(deviceID string, data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("empty payload")
	}
	name, err := queueName(deviceID)
	if err != nil {
		return err
	}
	msg := encodeIPP(ippOpPrintJob, b.nextReqID(), opAttrs(printerURI(name), cupsUser(),
		ippAttr{tag: ippTagName, name: "job-name", value: "Novex Print Job"},
		ippAttr{tag: ippTagMime, name: "document-format", value: "application/octet-stream"},
	))
	body := append(msg, data...)
	resp, err := ippPost(printerURI(name), body, cupsPrint)
	if err != nil {
		return err
	}
	status, err := ippStatus(resp)
	if err != nil {
		return err
	}
	if status != 0x0000 {
		return fmt.Errorf("CUPS print failed with status 0x%04x (is %q a Raw queue? see docs/USB.md)", status, name)
	}
	return nil
}

func cupsGetPrinters() ([]ippPrinter, error) {
	msg := encodeIPP(ippOpGetPrinter, 1, opAttrs("", cupsUser(),
		ippAttr{tag: ippTagKeyword, name: "requested-attributes", value: "printer-name"},
		ippAttr{tag: ippTagKeyword, name: "requested-attributes", value: "device-uri"},
	))
	resp, err := ippPost(cupsBase+"/", msg, cupsDiscovery)
	if err != nil {
		return nil, err
	}
	status, err := ippStatus(resp)
	if err != nil {
		return nil, err
	}
	if status != 0x0000 {
		return nil, fmt.Errorf("CUPS Get-Printers failed with status 0x%04x", status)
	}
	return parsePrinterGroups(resp)
}

func cupsUser() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	if u := os.Getenv("USERNAME"); u != "" {
		return u
	}
	return "novex"
}

// ippAttr is one IPP operation attribute.
type ippAttr struct {
	tag   byte
	name  string
	value string
}

// opAttrs builds the standard operation-attributes group.
func opAttrs(printerURI, user string, extra ...ippAttr) []ippAttr {
	attrs := []ippAttr{
		{tag: ippTagCharset, name: "attributes-charset", value: "utf-8"},
		{tag: ippTagLang, name: "attributes-natural-language", value: "en"},
	}
	if printerURI != "" {
		attrs = append(attrs, ippAttr{tag: ippTagURI, name: "printer-uri", value: printerURI})
	}
	attrs = append(attrs, ippAttr{tag: ippTagName, name: "requesting-user-name", value: user})
	return append(attrs, extra...)
}

// encodeIPP builds a minimal IPP/1.1 request message.
func encodeIPP(op uint16, reqID int32, attrs []ippAttr) []byte {
	var b bytes.Buffer
	b.Write([]byte{0x01, 0x01}) // IPP version 1.1
	_ = binary.Write(&b, binary.BigEndian, op)
	_ = binary.Write(&b, binary.BigEndian, reqID)
	b.WriteByte(ippTagOpAttrs)
	for _, a := range attrs {
		b.WriteByte(a.tag)
		_ = binary.Write(&b, binary.BigEndian, uint16(len(a.name)))
		b.WriteString(a.name)
		_ = binary.Write(&b, binary.BigEndian, uint16(len(a.value)))
		b.WriteString(a.value)
	}
	b.WriteByte(ippTagEnd)
	return b.Bytes()
}

// ippPost sends an IPP request and returns the raw response.
func ippPost(uri string, body []byte, timeout time.Duration) ([]byte, error) {
	req, err := http.NewRequest("POST", uri, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/ipp")
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach CUPS at %s: %v (is CUPS running?)", cupsBase, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("CUPS HTTP error: %s", resp.Status)
	}
	return data, nil
}

// ippStatus extracts the 2-byte status-code from an IPP response.
func ippStatus(data []byte) (uint16, error) {
	if len(data) < 4 {
		return 0, fmt.Errorf("truncated IPP response (%d bytes)", len(data))
	}
	return binary.BigEndian.Uint16(data[2:4]), nil
}

// ippPrinter is one parsed printer-attributes group.
type ippPrinter struct {
	attrs map[string]string
}

// parsePrinterGroups walks an IPP response and returns each
// printer-attributes (0x04) group as a string map. Attributes outside
// printer groups (e.g. the operation group) are ignored.
func parsePrinterGroups(data []byte) ([]ippPrinter, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("truncated IPP response (%d bytes)", len(data))
	}
	var out []ippPrinter
	cur := make(map[string]string)
	have := false
	inPrinter := false
	pos := 4
	read := func(n int) ([]byte, bool) {
		if n < 0 || pos+n > len(data) {
			return nil, false
		}
		b := data[pos : pos+n]
		pos += n
		return b, true
	}
	flush := func() {
		if inPrinter && have {
			out = append(out, ippPrinter{attrs: cur})
		}
		cur = make(map[string]string)
		have = false
	}
	for {
		tb, ok := read(1)
		if !ok {
			return nil, fmt.Errorf("truncated IPP response in attribute tag")
		}
		tag := tb[0]
		if tag == ippTagEnd {
			break
		}
		if tag < 0x10 {
			// Group delimiter.
			if tag == ippTagPrinterAttrs {
				flush()
				inPrinter = true
			} else {
				flush()
				inPrinter = false
			}
			continue
		}
		lb, ok := read(2)
		if !ok {
			return nil, fmt.Errorf("truncated IPP response in name length")
		}
		nl := int(binary.BigEndian.Uint16(lb))
		nb, ok := read(nl)
		if !ok {
			return nil, fmt.Errorf("truncated IPP response in name")
		}
		vb, ok := read(2)
		if !ok {
			return nil, fmt.Errorf("truncated IPP response in value length")
		}
		vl := int(binary.BigEndian.Uint16(vb))
		vv, ok := read(vl)
		if !ok {
			return nil, fmt.Errorf("truncated IPP response in value")
		}
		name := string(nb)
		if inPrinter && name != "" {
			if _, exists := cur[name]; !exists {
				cur[name] = string(vv)
			}
			have = true
		}
	}
	flush()
	return out, nil
}

// parseUSBURI extracts make/model/serial from a CUPS usb:// device-uri,
// e.g. "usb://Vendor/Model?serial=ABC123".
func parseUSBURI(uri string) (make_, model, serial string) {
	rest := uri
	if i := strings.Index(rest, "://"); i >= 0 {
		rest = rest[i+3:]
	}
	path := rest
	query := ""
	if i := strings.IndexByte(rest, '?'); i >= 0 {
		path = rest[:i]
		query = rest[i+1:]
	}
	parts := strings.SplitN(path, "/", 2)
	if len(parts) > 0 {
		make_ = unescape(parts[0])
	}
	if len(parts) > 1 {
		model = unescape(parts[1])
	}
	for _, kv := range strings.Split(query, "&") {
		if len(kv) > len("serial=") && strings.EqualFold(kv[:len("serial=")], "serial=") {
			serial = unescape(kv[len("serial="):])
		}
	}
	return make_, model, serial
}

func unescape(s string) string {
	if u, err := url.QueryUnescape(s); err == nil {
		return u
	}
	return s
}
