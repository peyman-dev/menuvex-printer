package network

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestLocalSubnets(t *testing.T) {
	nets, err := LocalSubnets()
	if err != nil {
		t.Fatalf("subnets: %v", err)
	}
	if len(nets) == 0 {
		t.Skip("no network interfaces (sandboxed CI?)")
	}
	for _, n := range nets {
		t.Logf("subnet: %s", n)
	}
}

func TestScanFindsLocalListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()

	_, loop24, err := net.ParseCIDR("127.0.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	opts := DefaultOptions()
	opts.Ports = []int{port}
	hits := Scan(ctx, []*net.IPNet{loop24}, opts)
	found := false
	for _, h := range hits {
		if h.Address == "127.0.0.1" && h.Port == port {
			found = true
		}
	}
	if !found {
		t.Fatalf("listener 127.0.0.1:%d not found (hits=%v)", port, hits)
	}
}

func TestScanCancelledContext(t *testing.T) {
	_, loop24, _ := net.ParseCIDR("127.0.0.0/24")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	hits := Scan(ctx, []*net.IPNet{loop24}, DefaultOptions())
	if time.Since(start) > 10*time.Second {
		t.Fatalf("cancelled scan took too long")
	}
	if len(hits) != 0 {
		t.Fatalf("expected no hits on cancelled scan, got %v", hits)
	}
}

func TestScanClosedPortNoHits(t *testing.T) {
	// Find a surely-closed port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	// Scan loopback /24; the closed port must not appear for 127.0.0.1.
	_, loop24, _ := net.ParseCIDR("127.0.0.0/24")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	opts := DefaultOptions()
	opts.Ports = []int{port}
	opts.DialTimeout = 200 * time.Millisecond
	hits := Scan(ctx, []*net.IPNet{loop24}, opts)
	for _, h := range hits {
		if h.Address == "127.0.0.1" && h.Port == port {
			t.Fatalf("closed port reported as hit: %v", h)
		}
	}
}
