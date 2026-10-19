package printer

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"
)

// startTCP starts a loopback listener and runs handle for every connection.
func startTCP(t *testing.T, handle func(net.Conn)) (host string, port int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) { handle(c) }(conn)
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", addr.Port
}

func TestProbeReportsProtocolResponseAsOnline(t *testing.T) {
	host, port := startTCP(t, func(c net.Conn) {
		defer c.Close()
		buf := make([]byte, 3)
		_ = c.SetReadDeadline(time.Now().Add(time.Second))
		if _, err := c.Read(buf); err == nil && string(buf) == "\x10\x04\x01" {
			_, _ = c.Write([]byte{0x12})
		}
	})
	p := NewProber(nil)
	p.ConnectTimeout = time.Second
	p.StatusTimeout = 500 * time.Millisecond
	res := p.Probe(context.Background(), host, port)
	if res.State != ProbeOnline {
		t.Fatalf("state = %s, want online (%+v)", res.State, res)
	}
	if !res.Reachable || !res.Responded || !res.LikelyPrinter {
		t.Fatalf("expected reachable+responded+likely: %+v", res)
	}
	if len(res.Stages) != 3 {
		t.Fatalf("expected address/reachable/status stages, got %+v", res.Stages)
	}
	if res.Stages[2].State != StagePass {
		t.Fatalf("status stage = %+v, want pass", res.Stages[2])
	}
}

func TestProbeNeverCallsOpenPortAWorkingPrinter(t *testing.T) {
	host, port := startTCP(t, func(c net.Conn) {
		defer c.Close()
		time.Sleep(2 * time.Second) // accept but never answer
	})
	p := NewProber(nil)
	p.ConnectTimeout = time.Second
	p.StatusTimeout = 150 * time.Millisecond
	start := time.Now()
	res := p.Probe(context.Background(), host, port)
	if time.Since(start) > time.Second {
		t.Fatal("probe must be time bounded")
	}
	if res.State != ProbeUnconfirmed {
		t.Fatalf("silent open port state = %s, want unconfirmed", res.State)
	}
	if !res.Reachable || res.Responded || res.LikelyPrinter {
		t.Fatalf("silent port must not be marked as printer: %+v", res)
	}
	if res.Stages[1].State != StagePass || res.Stages[2].State != StageWarn {
		t.Fatalf("unexpected stages: %+v", res.Stages)
	}
}

func TestProbeClosedPortIsOffline(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	p := NewProber(nil)
	p.ConnectTimeout = 300 * time.Millisecond
	res := p.Probe(context.Background(), "127.0.0.1", port)
	if res.State != ProbeOffline || res.Reachable {
		t.Fatalf("closed port must be offline: %+v", res)
	}
	if len(res.Stages) != 3 || res.Stages[1].State != StageFail || res.Stages[2].State != StageSkip {
		t.Fatalf("unexpected stages: %+v", res.Stages)
	}
}

func TestProbeRejectsPublicAndHostnameWithoutDialing(t *testing.T) {
	dialed := false
	p := NewProber(func(ctx context.Context, network, address string) (net.Conn, error) {
		dialed = true
		return nil, fmt.Errorf("should not dial")
	})
	for _, host := range []string{"8.8.8.8", "example.com", "10.0.0.0"} {
		res := p.Probe(context.Background(), host, 9100)
		if res.State != ProbeInvalid {
			t.Fatalf("%s: state = %s, want invalid", host, res.State)
		}
	}
	if dialed {
		t.Fatal("invalid targets must never be dialed")
	}
}

func TestPlausibleStatus(t *testing.T) {
	for _, b := range []byte{0x00, 0x12, 0x16, 0x32} {
		if !plausibleStatus(b) {
			t.Fatalf("0x%02X must be accepted", b)
		}
	}
	for _, b := range []byte{0x01, 0x48, 0xFF} {
		if plausibleStatus(b) {
			t.Fatalf("0x%02X must be rejected", b)
		}
	}
}
