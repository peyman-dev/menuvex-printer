package api

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/menuvex/novex-printer-agent/internal/config"
	"github.com/menuvex/novex-printer-agent/internal/printer"
	"github.com/menuvex/novex-printer-agent/internal/usb"
)

// ---------- test harness ----------

// capturePrinter records exact bytes received per TCP connection.
type capturePrinter struct {
	ln    net.Listener
	mu    sync.Mutex
	got   [][]byte
	conns map[net.Conn]bool
	wg    sync.WaitGroup
}

func startCapture(t *testing.T) *capturePrinter {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	c := &capturePrinter{ln: ln, conns: make(map[net.Conn]bool)}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			c.mu.Lock()
			c.conns[conn] = true
			c.mu.Unlock()
			c.wg.Add(1)
			go func(cc net.Conn) {
				defer c.wg.Done()
				defer func() {
					c.mu.Lock()
					delete(c.conns, cc)
					c.mu.Unlock()
					_ = cc.Close()
				}()
				data, _ := io.ReadAll(cc)
				c.mu.Lock()
				c.got = append(c.got, data)
				c.mu.Unlock()
			}(conn)
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		c.mu.Lock()
		for cc := range c.conns {
			_ = cc.Close()
		}
		c.mu.Unlock()
		c.wg.Wait()
	})
	return c
}

func (c *capturePrinter) port() int {
	return c.ln.Addr().(*net.TCPAddr).Port
}

func (c *capturePrinter) id() string {
	return printer.TCPPrinterID("127.0.0.1", c.port())
}

func (c *capturePrinter) waitFor(count int, timeout time.Duration) [][]byte {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		n := len(c.got)
		c.mu.Unlock()
		if n >= count {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([][]byte, len(c.got))
	copy(out, c.got)
	return out
}

type testClient struct {
	t     *testing.T
	base  string
	token string
	http  *http.Client
}

func newTestEnv(t *testing.T) (*testClient, *usb.MockUSB, *printer.Registry) {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Token = "test-token-123"
	cfg.TrustedOrigins = []string{"http://localhost:3000"}
	cfg.ConnectTimeoutMs = 2000
	cfg.WriteTimeoutMs = 2000
	cfgPath := t.TempDir() + "/config.json"
	mock := usb.NewMock()
	reg := printer.NewRegistry(cfg, cfgPath, mock)
	srv := NewServer(reg, cfg, "test-1.0.0")
	httpSrv := httptest.NewServer(srv.routes())
	t.Cleanup(httpSrv.Close)
	return &testClient{t: t, base: httpSrv.URL, token: cfg.Token, http: httpSrv.Client()}, mock, reg
}

func (c *testClient) do(method, path string, body []byte, contentType string, headers map[string]string) (int, []byte) {
	c.t.Helper()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, c.base+path, rdr)
	if err != nil {
		c.t.Fatalf("new request: %v", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("do %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}

func (c *testClient) doRaw(method, path string, body []byte, contentType string, headers map[string]string) (int, http.Header, []byte) {
	c.t.Helper()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, c.base+path, rdr)
	if err != nil {
		c.t.Fatalf("new request: %v", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("do %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, data
}

func (c *testClient) doJSON(method, path string, v interface{}) (int, map[string]interface{}) {
	c.t.Helper()
	var body []byte
	if v != nil {
		var err error
		body, err = json.Marshal(v)
		if err != nil {
			c.t.Fatalf("marshal: %v", err)
		}
	}
	st, data := c.do(method, path, body, "application/json", nil)
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		c.t.Fatalf("invalid JSON response for %s %s (%d): %q", method, path, st, data)
	}
	return st, out
}

func errCode(t *testing.T, out map[string]interface{}) string {
	t.Helper()
	e, ok := out["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing error object in %v", out)
	}
	code, _ := e["code"].(string)
	return code
}

func printerPath(id string) string {
	return "/api/v1/printers/" + url.PathEscape(id)
}

// ---------- health / info / auth / CORS ----------

func TestHealthOpen(t *testing.T) {
	c, _, _ := newTestEnv(t)
	c.token = "" // no auth
	st, out := c.doJSON("GET", "/health", nil)
	if st != 200 {
		t.Fatalf("health status %d", st)
	}
	if out["status"] != "ok" || out["version"] != "test-1.0.0" {
		t.Fatalf("unexpected health %+v", out)
	}
	if out["platform"] == "" || out["arch"] == "" {
		t.Fatalf("missing platform/arch %+v", out)
	}
}

func TestInfoRequiresAuth(t *testing.T) {
	c, _, _ := newTestEnv(t)
	c.token = ""
	st, out := c.doJSON("GET", "/api/v1/info", nil)
	if st != 401 || errCode(t, out) != printer.CodeUnauthorized {
		t.Fatalf("got %d %+v", st, out)
	}
	c.token = "wrong"
	st, _ = c.doJSON("GET", "/api/v1/info", nil)
	if st != 401 {
		t.Fatalf("wrong token accepted: %d", st)
	}
	c.token = "test-token-123"
	st, out = c.doJSON("GET", "/api/v1/info", nil)
	if st != 200 {
		t.Fatalf("info status %d", st)
	}
	if _, ok := out["usbSupported"]; !ok {
		t.Fatalf("missing usbSupported in %+v", out)
	}
}

func TestCORSEnforced(t *testing.T) {
	c, _, _ := newTestEnv(t)
	// Trusted origin passes with ACAO echo.
	st, hdr, _ := c.doRaw("GET", "/health", nil, "", map[string]string{"Origin": "http://localhost:3000"})
	if st != 200 || hdr.Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Fatalf("trusted origin failed: %d %v", st, hdr)
	}
	// Untrusted origin rejected even with valid token.
	st, _, body := c.doRaw("GET", "/health", nil, "", map[string]string{"Origin": "https://evil.com"})
	if st != 403 {
		t.Fatalf("expected 403, got %d %s", st, body)
	}
	var out map[string]interface{}
	_ = json.Unmarshal(body, &out)
	if errCode(t, out) != printer.CodeForbiddenOrigin {
		t.Fatalf("unexpected code %+v", out)
	}
	// Preflight.
	st, hdr, _ = c.doRaw("OPTIONS", "/api/v1/printers", nil, "", map[string]string{
		"Origin": "http://localhost:3000", "Access-Control-Request-Method": "POST",
	})
	if st != 204 || hdr.Get("Access-Control-Allow-Headers") == "" {
		t.Fatalf("preflight failed: %d %v", st, hdr)
	}
}

func TestNotFoundAndMethodNotAllowed(t *testing.T) {
	c, _, _ := newTestEnv(t)
	st, out := c.doJSON("GET", "/api/v1/nope", nil)
	if st != 404 || errCode(t, out) != printer.CodeNotFound {
		t.Fatalf("got %d %+v", st, out)
	}
	st, _, _ = c.doRaw("GET", "/api/v1/printers/tcp:1.2.3.4:9100/print", nil, "", nil)
	if st != 405 {
		t.Fatalf("expected 405, got %d", st)
	}
}

// ---------- printers ----------

func TestListPrintersShowsUSB(t *testing.T) {
	c, mock, _ := newTestEnv(t)
	st, out := c.doJSON("GET", "/api/v1/printers", nil)
	if st != 200 {
		t.Fatalf("list status %d", st)
	}
	list, _ := out["printers"].([]interface{})
	if len(list) != len(mock.Devices) {
		t.Fatalf("expected %d printers, got %+v", len(mock.Devices), out)
	}
	first := list[0].(map[string]interface{})
	if first["type"] != "usb" || first["id"] == "" || first["vendorId"] == nil {
		t.Fatalf("unexpected printer shape %+v", first)
	}
}

func TestRegisterAndRemovePrinter(t *testing.T) {
	c, _, _ := newTestEnv(t)
	st, out := c.doJSON("POST", "/api/v1/printers", map[string]interface{}{
		"name": "OCOM", "address": "192.168.1.50", "port": 9100,
	})
	if st != 201 {
		t.Fatalf("register status %d %+v", st, out)
	}
	if out["printerId"] != "tcp:192.168.1.50:9100" {
		t.Fatalf("unexpected id %+v", out)
	}
	st, out = c.doJSON("GET", "/api/v1/printers", nil)
	list, _ := out["printers"].([]interface{})
	if len(list) != 3 { // 2 mock USB + 1 registered
		t.Fatalf("expected 3 printers, got %+v", out)
	}
	// Invalid registration.
	st, out = c.doJSON("POST", "/api/v1/printers", map[string]interface{}{
		"name": "bad", "address": "", "port": 9100,
	})
	if st != 400 {
		t.Fatalf("expected 400, got %d %+v", st, out)
	}
	// Remove.
	st, _ = c.doJSON("DELETE", printerPath("tcp:192.168.1.50:9100"), nil)
	if st != 200 {
		t.Fatalf("delete status %d", st)
	}
	st, out = c.doJSON("GET", "/api/v1/printers", nil)
	list, _ = out["printers"].([]interface{})
	if len(list) != 2 {
		t.Fatalf("expected 2 printers after removal, got %+v", out)
	}
}

func TestGetPrinterDetail(t *testing.T) {
	c, mock, _ := newTestEnv(t)
	st, out := c.doJSON("GET", printerPath("tcp:10.1.2.3:9100"), nil)
	if st != 200 {
		t.Fatalf("adhoc detail %d %+v", st, out)
	}
	p := out["printer"].(map[string]interface{})
	if p["address"] != "10.1.2.3" || p["port"] != float64(9100) {
		t.Fatalf("unexpected detail %+v", p)
	}
	st, _ = c.doJSON("GET", printerPath(mock.Devices[0].ID), nil)
	if st != 200 {
		t.Fatalf("usb detail %d", st)
	}
	st, out = c.doJSON("GET", printerPath("usb:0:0:missing"), nil)
	if st != 404 {
		t.Fatalf("expected 404, got %d %+v", st, out)
	}
	st, out = c.doJSON("GET", printerPath("bogus"), nil)
	if st != 400 || errCode(t, out) != printer.CodeInvalidPrinterID {
		t.Fatalf("got %d %+v", st, out)
	}
}

func TestScanFindsMockPrinter(t *testing.T) {
	c, _, _ := newTestEnv(t)
	cap := startCapture(t)
	st, out := c.doJSON("GET", fmt.Sprintf("/api/v1/printers?scan=true&ports=%d&scanTimeoutMs=12000", cap.port()), nil)
	if st != 200 {
		t.Fatalf("scan status %d %+v", st, out)
	}
	if out["scanned"] != true {
		t.Fatalf("expected scanned=true %+v", out)
	}
	found := false
	for _, item := range out["printers"].([]interface{}) {
		p := item.(map[string]interface{})
		if p["id"] == cap.id() {
			found = true
		}
	}
	if !found {
		t.Fatalf("scan missed mock printer %+v", out)
	}
}

// ---------- connect / print / status ----------

func TestConnectPrintStatusDisconnect(t *testing.T) {
	c, _, _ := newTestEnv(t)
	cap := startCapture(t)
	id := cap.id()

	st, _ := c.doJSON("POST", printerPath(id)+"/connect", nil)
	if st != 200 {
		t.Fatalf("connect %d", st)
	}
	st, out := c.doJSON("GET", printerPath(id)+"/status", nil)
	if st != 200 || out["connected"] != true || out["status"] != printer.StatusConnected {
		t.Fatalf("status %+v", out)
	}
	payload := []byte{0x1b, 0x40, 'H', 'e', 'l', 'l', 'o', 0x00, 0xff, 0x1d, 0x56, 0x00}
	st, out = c.doJSON("POST", printerPath(id)+"/print", map[string]interface{}{
		"data": base64.StdEncoding.EncodeToString(payload),
	})
	if st != 200 || out["success"] != true {
		t.Fatalf("print %d %+v", st, out)
	}
	st, _ = c.doJSON("POST", printerPath(id)+"/disconnect", nil)
	if st != 200 {
		t.Fatalf("disconnect %d", st)
	}
	got := cap.waitFor(1, 3*time.Second)
	if len(got) != 1 || !bytes.Equal(got[0], payload) {
		t.Fatalf("printer got %x", got)
	}
	st, out = c.doJSON("GET", printerPath(id)+"/status", nil)
	if out["connected"] != false || out["lastPrintAt"] == nil {
		t.Fatalf("expected disconnected with lastPrintAt: %+v", out)
	}
}

func TestPrintOctetStream(t *testing.T) {
	c, _, _ := newTestEnv(t)
	cap := startCapture(t)
	payload := []byte{0x1b, 0x40, 'r', 'a', 'w'}
	st, body := c.do("POST", printerPath(cap.id())+"/print", payload, "application/octet-stream", nil)
	if st != 200 {
		t.Fatalf("print %d %s", st, body)
	}
	got := cap.waitFor(1, 3*time.Second)
	if len(got) != 1 || !bytes.Equal(got[0], payload) {
		t.Fatalf("printer got %x", got)
	}
}

func TestPrintValidationErrors(t *testing.T) {
	c, _, _ := newTestEnv(t)
	cap := startCapture(t)
	path := printerPath(cap.id()) + "/print"

	st, out := c.doJSON("POST", path, map[string]interface{}{"data": "!!!not-base64!!!"})
	if st != 400 || errCode(t, out) != printer.CodeInvalidRequest {
		t.Fatalf("got %d %+v", st, out)
	}
	st, _ = c.doJSON("POST", path, map[string]interface{}{})
	if st != 400 {
		t.Fatalf("expected 400 for missing data, got %d", st)
	}
	st, body := c.do("POST", path, []byte("{oops"), "application/json", nil)
	if st != 400 {
		t.Fatalf("expected 400 for bad JSON, got %d %s", st, body)
	}
}

func TestPrintPayloadTooLarge(t *testing.T) {
	c, _, _ := newTestEnv(t)
	cap := startCapture(t)
	big := make([]byte, MaxPrintBytes+1)
	st, out := c.doJSON("POST", printerPath(cap.id())+"/print", map[string]interface{}{
		"data": base64.StdEncoding.EncodeToString(big),
	})
	_ = out
	if st != 400 {
		t.Fatalf("expected 400 for oversized payload, got %d", st)
	}
}

func TestPrintConnectionFailure(t *testing.T) {
	c, _, _ := newTestEnv(t)
	// Reserve then free a port so nothing listens.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	id := printer.TCPPrinterID("127.0.0.1", port)

	st, out := c.doJSON("POST", printerPath(id)+"/print", map[string]interface{}{
		"data": base64.StdEncoding.EncodeToString([]byte("x")),
	})
	if st != 502 || errCode(t, out) != printer.CodePrinterConnFailed {
		t.Fatalf("got %d %+v", st, out)
	}
	st, out = c.doJSON("GET", printerPath(id)+"/status", nil)
	if st != 200 || out["status"] != printer.StatusError || out["lastError"] == nil {
		t.Fatalf("expected error status: %+v", out)
	}
}

func TestUSBPrintViaAPI(t *testing.T) {
	c, mock, _ := newTestEnv(t)
	id := mock.Devices[1].ID
	payload := []byte{0x1b, 0x40, 'u', 's', 'b'}
	st, out := c.doJSON("POST", printerPath(id)+"/print", map[string]interface{}{
		"data": base64.StdEncoding.EncodeToString(payload),
	})
	if st != 200 || out["success"] != true {
		t.Fatalf("print %d %+v", st, out)
	}
	writes := mock.Written(id)
	if len(writes) != 1 || !bytes.Equal(writes[0], payload) {
		t.Fatalf("mock got %x", writes)
	}
	st, _ = c.doJSON("POST", printerPath("usb:0:0:missing")+"/print", map[string]interface{}{
		"data": base64.StdEncoding.EncodeToString([]byte("x")),
	})
	if st != 404 {
		t.Fatalf("expected 404 for missing USB device, got %d", st)
	}
}

func TestConcurrentPrintsSamePrinter(t *testing.T) {
	c, _, _ := newTestEnv(t)
	cap := startCapture(t)
	id := cap.id()
	if st, _ := c.doJSON("POST", printerPath(id)+"/connect", nil); st != 200 {
		t.Fatalf("connect %d", st)
	}
	const writers = 8
	const chunk = 2048
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			payload := bytes.Repeat([]byte{byte(n + 1)}, chunk)
			st, out := c.doJSON("POST", printerPath(id)+"/print", map[string]interface{}{
				"data": base64.StdEncoding.EncodeToString(payload),
			})
			if st != 200 {
				t.Errorf("print %d: %d %+v", n, st, out)
			}
		}(i)
	}
	wg.Wait()
	if st, _ := c.doJSON("POST", printerPath(id)+"/disconnect", nil); st != 200 {
		t.Fatalf("disconnect %d", st)
	}
	got := cap.waitFor(1, 5*time.Second)
	if len(got) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(got))
	}
	if len(got[0]) != writers*chunk {
		t.Fatalf("expected %d bytes, got %d", writers*chunk, len(got[0]))
	}
	for n := 0; n < writers; n++ {
		part := got[0][n*chunk : (n+1)*chunk]
		for _, b := range part {
			if b != part[0] {
				t.Fatalf("interleaved stream in chunk %d", n)
			}
		}
	}
}

func TestConcurrentPrintsDifferentPrinters(t *testing.T) {
	c, _, _ := newTestEnv(t)
	c1 := startCapture(t)
	c2 := startCapture(t)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		st, _ := c.doJSON("POST", printerPath(c1.id())+"/print", map[string]interface{}{
			"data": base64.StdEncoding.EncodeToString([]byte("one")),
		})
		if st != 200 {
			t.Errorf("printer one: %d", st)
		}
	}()
	go func() {
		defer wg.Done()
		st, _ := c.doJSON("POST", printerPath(c2.id())+"/print", map[string]interface{}{
			"data": base64.StdEncoding.EncodeToString([]byte("two")),
		})
		if st != 200 {
			t.Errorf("printer two: %d", st)
		}
	}()
	wg.Wait()
	g1 := c1.waitFor(1, 3*time.Second)
	g2 := c2.waitFor(1, 3*time.Second)
	if len(g1) != 1 || string(g1[0]) != "one" {
		t.Fatalf("printer one got %q", g1)
	}
	if len(g2) != 1 || string(g2[0]) != "two" {
		t.Fatalf("printer two got %q", g2)
	}
}

// ---------- websocket ----------

func wsHandshake(t *testing.T, addr, token, origin string, extra map[string]string) (net.Conn, *bufio.Reader, *http.Response) {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		t.Fatalf("rand: %v", err)
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)
	var sb strings.Builder
	fmt.Fprintf(&sb, "GET /ws?token=%s HTTP/1.1\r\n", url.QueryEscape(token))
	fmt.Fprintf(&sb, "Host: %s\r\n", addr)
	sb.WriteString("Upgrade: websocket\r\nConnection: Upgrade\r\n")
	fmt.Fprintf(&sb, "Sec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n", key)
	if origin != "" {
		fmt.Fprintf(&sb, "Origin: %s\r\n", origin)
	}
	for k, v := range extra {
		fmt.Fprintf(&sb, "%s: %s\r\n", k, v)
	}
	sb.WriteString("\r\n")
	if _, err := conn.Write([]byte(sb.String())); err != nil {
		_ = conn.Close()
		t.Fatalf("write handshake: %v", err)
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: "GET"})
	if err != nil {
		_ = conn.Close()
		t.Fatalf("read handshake response: %v", err)
	}
	// Stash expected accept on the response for the caller to verify.
	sum := sha1.Sum([]byte(key + wsGUID))
	resp.Header.Set("X-Expect-Accept", base64.StdEncoding.EncodeToString(sum[:]))
	return conn, br, resp
}

func readServerFrame(t *testing.T, br *bufio.Reader) (byte, []byte) {
	t.Helper()
	b1, err := br.ReadByte()
	if err != nil {
		t.Fatalf("read frame header: %v", err)
	}
	b2, err := br.ReadByte()
	if err != nil {
		t.Fatalf("read frame len: %v", err)
	}
	if b2&0x80 != 0 {
		t.Fatalf("server frame must not be masked")
	}
	length := int64(b2 & 0x7F)
	switch length {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(br, ext[:]); err != nil {
			t.Fatalf("ext len: %v", err)
		}
		length = int64(ext[0])<<8 | int64(ext[1])
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(br, ext[:]); err != nil {
			t.Fatalf("ext len: %v", err)
		}
		length = int64(ext[6])<<8 | int64(ext[7])
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(br, payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	return b1 & 0x0F, payload
}

func sendClientText(t *testing.T, conn net.Conn, payload []byte) {
	t.Helper()
	mask := []byte{0x1, 0x2, 0x3, 0x4}
	frame := []byte{0x81, 0x80 | byte(len(payload))}
	frame = append(frame, mask...)
	masked := make([]byte, len(payload))
	for i := range payload {
		masked[i] = payload[i] ^ mask[i%4]
	}
	frame = append(frame, masked...)
	if _, err := conn.Write(frame); err != nil {
		t.Fatalf("write frame: %v", err)
	}
}

func TestWebSocketEvents(t *testing.T) {
	c, _, _ := newTestEnv(t)
	addr := strings.TrimPrefix(c.base, "http://")
	conn, br, resp := wsHandshake(t, addr, "test-token-123", "http://localhost:3000", nil)
	defer conn.Close()
	if resp.StatusCode != 101 {
		t.Fatalf("expected 101, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Sec-WebSocket-Accept") != resp.Header.Get("X-Expect-Accept") {
		t.Fatalf("bad accept key: %v", resp.Header)
	}

	// Hello first.
	op, payload := readServerFrame(t, br)
	if op != 0x1 {
		t.Fatalf("expected text frame, got op %x", op)
	}
	var hello map[string]interface{}
	if err := json.Unmarshal(payload, &hello); err != nil || hello["event"] != "agent.hello" {
		t.Fatalf("bad hello: %s", payload)
	}

	// Trigger connect/disconnect/error via HTTP and observe frames.
	cap := startCapture(t)
	id := cap.id()
	if st, _ := c.doJSON("POST", printerPath(id)+"/connect", nil); st != 200 {
		t.Fatalf("connect failed")
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	op, payload = readServerFrame(t, br)
	var ev map[string]interface{}
	_ = json.Unmarshal(payload, &ev)
	if op != 0x1 || ev["event"] != printer.EventConnected || ev["printerId"] != id {
		t.Fatalf("bad connected event: %s", payload)
	}

	if st, _ := c.doJSON("POST", printerPath(id)+"/disconnect", nil); st != 200 {
		t.Fatalf("disconnect failed")
	}
	_, payload = readServerFrame(t, br)
	_ = json.Unmarshal(payload, &ev)
	if ev["event"] != printer.EventDisconnected {
		t.Fatalf("bad disconnected event: %s", payload)
	}

	dead := printer.TCPPrinterID("127.0.0.1", 1)
	_, _ = c.doJSON("POST", printerPath(dead)+"/print", map[string]interface{}{
		"data": base64.StdEncoding.EncodeToString([]byte("x")),
	})
	_, payload = readServerFrame(t, br)
	_ = json.Unmarshal(payload, &ev)
	if ev["event"] != printer.EventError || ev["printerId"] != dead {
		t.Fatalf("bad error event: %s", payload)
	}

	// Client ping -> server pong.
	ping := []byte{0x89, 0x80 | 0x04, 0x1, 0x2, 0x3, 0x4, 'p' ^ 0x1, 'i' ^ 0x2, 'n' ^ 0x3, 'g' ^ 0x4}
	if _, err := conn.Write(ping); err != nil {
		t.Fatalf("write ping: %v", err)
	}
	op, payload = readServerFrame(t, br)
	if op != 0xA || string(payload) != "ping" {
		t.Fatalf("expected pong, got op %x %q", op, payload)
	}

	// Client text is ignored (no crash); then clean close.
	sendClientText(t, conn, []byte("hello server"))
	closeFrame := []byte{0x88, 0x80, 0x1, 0x2, 0x3, 0x4}
	if _, err := conn.Write(closeFrame); err != nil {
		t.Fatalf("write close: %v", err)
	}
	op, _ = readServerFrame(t, br)
	if op != 0x8 {
		t.Fatalf("expected close echo, got op %x", op)
	}
}

func TestWebSocketAuthAndOrigin(t *testing.T) {
	c, _, _ := newTestEnv(t)
	addr := strings.TrimPrefix(c.base, "http://")

	// Missing token -> 401.
	conn, _, resp := wsHandshake(t, addr, "", "http://localhost:3000", nil)
	_ = conn.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
	// Wrong token -> 401.
	conn, _, resp = wsHandshake(t, addr, "nope", "http://localhost:3000", nil)
	_ = conn.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
	// Untrusted origin -> 403 (rejected by CORS layer).
	conn, _, resp = wsHandshake(t, addr, "test-token-123", "https://evil.com", nil)
	_ = conn.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
	// No upgrade headers -> 400.
	raw, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	fmt.Fprintf(raw, "GET /ws?token=test-token-123 HTTP/1.1\r\nHost: %s\r\nOrigin: http://localhost:3000\r\n\r\n", addr)
	br := bufio.NewReader(raw)
	resp, err = http.ReadResponse(br, &http.Request{Method: "GET"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}
