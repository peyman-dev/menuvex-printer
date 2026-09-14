package printer

import (
	"bytes"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/menuvex/novex-printer-agent/internal/config"
)

// mockPrinter is a fake TCP thermal printer: it accepts connections and
// records the exact bytes received per connection.
type mockPrinter struct {
	t     *testing.T
	ln    net.Listener
	mu    sync.Mutex
	conns map[net.Conn]bool
	got   [][]byte
	wg    sync.WaitGroup
}

func startMockPrinter(t *testing.T) *mockPrinter {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	m := &mockPrinter{t: t, ln: ln, conns: make(map[net.Conn]bool)}
	m.wg.Add(1)
	go m.acceptLoop()
	t.Cleanup(m.close)
	return m
}

func (m *mockPrinter) acceptLoop() {
	defer m.wg.Done()
	for {
		conn, err := m.ln.Accept()
		if err != nil {
			return
		}
		m.mu.Lock()
		m.conns[conn] = true
		m.mu.Unlock()
		m.wg.Add(1)
		go func(c net.Conn) {
			defer m.wg.Done()
			defer func() {
				m.mu.Lock()
				delete(m.conns, c)
				m.mu.Unlock()
				_ = c.Close()
			}()
			data, err := io.ReadAll(c)
			if err != nil {
				return
			}
			m.mu.Lock()
			m.got = append(m.got, data)
			m.mu.Unlock()
		}(conn)
	}
}

func (m *mockPrinter) addr() (string, int) {
	a := m.ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", a.Port
}

func (m *mockPrinter) id() string {
	host, port := m.addr()
	return TCPPrinterID(host, port)
}

// allBytes returns received payloads once count connections arrived
// (waits up to timeout).
func (m *mockPrinter) waitFor(count int, timeout time.Duration) [][]byte {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		n := len(m.got)
		m.mu.Unlock()
		if n >= count {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([][]byte, len(m.got))
	copy(out, m.got)
	return out
}

func (m *mockPrinter) close() {
	_ = m.ln.Close()
	m.mu.Lock()
	for c := range m.conns {
		_ = c.Close()
	}
	m.mu.Unlock()
	m.wg.Wait()
}

func testConfig() (*config.Config, string) {
	cfg := config.DefaultConfig()
	cfg.Token = "test-token"
	cfg.ConnectTimeoutMs = 2000
	cfg.WriteTimeoutMs = 2000
	return cfg, ""
}

func TestParseTCPPrinterID(t *testing.T) {
	host, port, err := ParseTCPPrinterID("tcp:192.168.1.50:9100")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if host != "192.168.1.50" || port != 9100 {
		t.Fatalf("got %s:%d", host, port)
	}
	if _, _, err := ParseTCPPrinterID("tcp:printer.lan:9100"); err != nil {
		t.Fatalf("hostname should be allowed: %v", err)
	}
	for _, bad := range []string{
		"", "usb:123", "tcp:", "tcp:192.168.1.1", "tcp:192.168.1.1:",
		"tcp:192.168.1.1:0", "tcp:192.168.1.1:99999", "tcp:192.168.1.1:abc",
		"TCP:192.168.1.1:9100",
	} {
		if _, _, err := ParseTCPPrinterID(bad); err == nil {
			t.Fatalf("expected error for %q", bad)
		} else if perr, ok := AsError(err); !ok || perr.Code != CodeInvalidPrinterID {
			t.Fatalf("expected INVALID_PRINTER_ID for %q, got %v", bad, err)
		}
	}
}

func TestTCPPrintAdhocExactBytes(t *testing.T) {
	mock := startMockPrinter(t)
	cfg, _ := testConfig()
	reg := NewRegistry(cfg, "", nil)

	payload := []byte{0x1b, 0x40, 'H', 'i', 0x00, 0xff, 0x1d, 0x56, 0x00}
	if err := reg.Print(mock.id(), payload); err != nil {
		t.Fatalf("print failed: %v", err)
	}
	got := mock.waitFor(1, 3*time.Second)
	if len(got) != 1 {
		t.Fatalf("expected 1 connection, got %d", len(got))
	}
	if !bytes.Equal(got[0], payload) {
		t.Fatalf("bytes modified in transit: got %x want %x", got[0], payload)
	}
}

func TestTCPConnectStatusDisconnect(t *testing.T) {
	mock := startMockPrinter(t)
	cfg, _ := testConfig()
	reg := NewRegistry(cfg, "", nil)
	id := mock.id()

	st, err := reg.GetStatus(id)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Connected {
		t.Fatalf("should start disconnected")
	}
	if err := reg.Connect(id); err != nil {
		t.Fatalf("connect: %v", err)
	}
	st, _ = reg.GetStatus(id)
	if !st.Connected || st.Status != StatusConnected {
		t.Fatalf("expected connected, got %+v", st)
	}
	// Double connect is fine.
	if err := reg.Connect(id); err != nil {
		t.Fatalf("second connect: %v", err)
	}
	if err := reg.Disconnect(id); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	st, _ = reg.GetStatus(id)
	if st.Connected {
		t.Fatalf("expected disconnected, got %+v", st)
	}
	// Double disconnect is a no-op success.
	if err := reg.Disconnect(id); err != nil {
		t.Fatalf("second disconnect: %v", err)
	}
}

func TestTCPPrintConcurrentSamePrinter(t *testing.T) {
	mock := startMockPrinter(t)
	cfg, _ := testConfig()
	reg := NewRegistry(cfg, "", nil)
	id := mock.id()

	// Explicit connect so all prints share one connection: without the
	// per-printer mutex the streams would interleave.
	if err := reg.Connect(id); err != nil {
		t.Fatalf("connect: %v", err)
	}

	const writers = 8
	const chunk = 4096
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			payload := bytes.Repeat([]byte{byte(n + 1)}, chunk)
			errs <- reg.Print(id, payload)
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("print error: %v", err)
		}
	}
	if err := reg.Disconnect(id); err != nil {
		t.Fatalf("disconnect: %v", err)
	}

	got := mock.waitFor(1, 3*time.Second)
	if len(got) != 1 {
		t.Fatalf("expected 1 shared connection, got %d", len(got))
	}
	stream := got[0]
	if len(stream) != writers*chunk {
		t.Fatalf("expected %d bytes, got %d", writers*chunk, len(stream))
	}
	// Every chunk must be uniform: any interleave breaks uniformity.
	for c := 0; c < writers; c++ {
		part := stream[c*chunk : (c+1)*chunk]
		for _, b := range part {
			if b != part[0] {
				t.Fatalf("interleaved stream in chunk %d", c)
			}
		}
	}
}

func TestTCPPrintConcurrentDifferentPrinters(t *testing.T) {
	m1 := startMockPrinter(t)
	m2 := startMockPrinter(t)
	cfg, _ := testConfig()
	reg := NewRegistry(cfg, "", nil)

	p1 := []byte("printer-one-payload\x1b\x40")
	p2 := []byte("printer-two-payload\x1dV\x00")
	var wg sync.WaitGroup
	wg.Add(2)
	var e1, e2 error
	go func() { defer wg.Done(); e1 = reg.Print(m1.id(), p1) }()
	go func() { defer wg.Done(); e2 = reg.Print(m2.id(), p2) }()
	wg.Wait()
	if e1 != nil || e2 != nil {
		t.Fatalf("errors: %v %v", e1, e2)
	}
	g1 := m1.waitFor(1, 3*time.Second)
	g2 := m2.waitFor(1, 3*time.Second)
	if len(g1) != 1 || !bytes.Equal(g1[0], p1) {
		t.Fatalf("printer 1 got %q", g1)
	}
	if len(g2) != 1 || !bytes.Equal(g2[0], p2) {
		t.Fatalf("printer 2 got %q", g2)
	}
}

func TestTCPConnectionRefused(t *testing.T) {
	// Grab a port then close it so nothing listens there.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	cfg, _ := testConfig()
	reg := NewRegistry(cfg, "", nil)
	id := TCPPrinterID("127.0.0.1", port)
	err = reg.Print(id, []byte("x"))
	if err == nil {
		t.Fatal("expected connection error")
	}
	perr, ok := AsError(err)
	if !ok {
		t.Fatalf("expected *Error, got %T", err)
	}
	if perr.Code != CodePrinterConnFailed && perr.Code != CodePrinterTimeout {
		t.Fatalf("unexpected code %s", perr.Code)
	}
	st, _ := reg.GetStatus(id)
	if st.Status != StatusError || st.LastError == "" {
		t.Fatalf("expected error status with message, got %+v", st)
	}
}

func TestTCPConnectTimeout(t *testing.T) {
	cfg, _ := testConfig()
	cfg.ConnectTimeoutMs = 300
	reg := NewRegistry(cfg, "", nil)
	// TEST-NET / documentation range: unroutable, dial must fail fast-ish.
	id := TCPPrinterID("10.255.255.1", 9100)
	start := time.Now()
	err := reg.Connect(id)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected timeout/connection error")
	}
	if elapsed > 15*time.Second {
		t.Fatalf("connect took too long: %s", elapsed)
	}
	t.Logf("connect failed as expected in %s: %v", elapsed, err)
}

func TestPrintValidation(t *testing.T) {
	cfg, _ := testConfig()
	reg := NewRegistry(cfg, "", nil)
	if err := reg.Print("bogus-id", []byte("x")); err == nil {
		t.Fatal("expected invalid ID error")
	} else if perr, _ := AsError(err); perr.Code != CodeInvalidPrinterID {
		t.Fatalf("unexpected code %s", perr.Code)
	}
	mock := startMockPrinter(t)
	if err := reg.Print(mock.id(), nil); err == nil {
		t.Fatal("expected empty payload error")
	}
}

func TestTCPPrinterIDRoundtrip(t *testing.T) {
	id := TCPPrinterID("192.168.1.50", 9100)
	if id != "tcp:192.168.1.50:9100" {
		t.Fatalf("unexpected id %s", id)
	}
	h, p, err := ParseTCPPrinterID(id)
	if err != nil || h != "192.168.1.50" || p != 9100 {
		t.Fatalf("roundtrip failed: %s:%d %v", h, p, err)
	}
}
