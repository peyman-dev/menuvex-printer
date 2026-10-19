package printer

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func fakeOpenDial(openHost string) DialFunc {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		if host != openHost {
			return nil, fmt.Errorf("connection refused")
		}
		client, server := net.Pipe()
		_ = server.Close()
		return client, nil
	}
}

func waitScan(t *testing.T, s *Scanner, id string, timeout time.Duration) ScanStatus {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		snap, ok := s.Status(id)
		if !ok {
			t.Fatal("scan disappeared")
		}
		if !snap.Running {
			return snap
		}
		if time.Now().After(deadline) {
			t.Fatalf("scan did not finish: %+v", snap)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func scanPolicy() ScanPolicy {
	return ScanPolicy{
		MaxHosts:       254,
		Workers:        8,
		PerHostTimeout: 50 * time.Millisecond,
		TotalTimeout:   5 * time.Second,
		ResultTTL:      time.Minute,
	}
}

func TestScannerFindsOnlyOpenHostsInsideAllowedRange(t *testing.T) {
	allowed := []NetworkRange{{CIDR: "192.168.50.0/24", InterfaceName: "eth-test", Address: "192.168.50.10", Hosts: 254}}
	s := NewScanner(scanPolicy(), fakeOpenDial("192.168.50.7"))
	snap, err := s.Start("192.168.50.0/24", 9100, allowed)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Total != 254 || !snap.Running {
		t.Fatalf("initial snapshot = %+v", snap)
	}
	done := waitScan(t, s, snap.ID, 5*time.Second)
	if done.Done != 254 {
		t.Fatalf("done = %d, want 254", done.Done)
	}
	if len(done.Results) != 1 || done.Results[0].Host != "192.168.50.7" || done.Results[0].Port != 9100 {
		t.Fatalf("results = %+v, want exactly .7", done.Results)
	}
	if done.Percent() != 100 {
		t.Fatalf("percent = %d", done.Percent())
	}
}

func TestScannerRejectsRangesOutsideLocalList(t *testing.T) {
	s := NewScanner(scanPolicy(), fakeOpenDial(""))
	allowed := []NetworkRange{{CIDR: "192.168.50.0/24"}}
	cases := []struct {
		cidr string
		port int
		want string
	}{
		{"8.8.8.0/24", 9100, "مجاز"},
		{"192.168.51.0/24", 9100, "مجاز"},
		{"192.168.50.0/16", 9100, "/24"},
		{"192.168.50.0/25", 9100, "/24"},
		{"127.0.0.0/24", 9100, "مجاز"},
		{"192.168.50.0/24", 0, "پورت"},
		{"192.168.50.0/24", 90000, "پورت"},
		{"not-a-cidr", 9100, "CIDR"},
	}
	for _, tc := range cases {
		_, err := s.Start(tc.cidr, tc.port, allowed)
		if err == nil {
			t.Fatalf("%s:%d must be rejected", tc.cidr, tc.port)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s error = %q, want %q", tc.cidr, err, tc.want)
		}
	}
}

func TestScannerHostLimitIsEnforced(t *testing.T) {
	policy := scanPolicy()
	policy.MaxHosts = 10
	s := NewScanner(policy, fakeOpenDial(""))
	_, err := s.Start("192.168.50.0/24", 9100, []NetworkRange{{CIDR: "192.168.50.0/24"}})
	if err == nil || !strings.Contains(err.Error(), "بیش از حد") {
		t.Fatalf("expected host limit error, got %v", err)
	}
}

func TestScannerIsSingleFlightAndCancellable(t *testing.T) {
	blocking := func(ctx context.Context, network, address string) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	policy := scanPolicy()
	policy.Workers = 2
	policy.PerHostTimeout = time.Minute
	policy.TotalTimeout = time.Minute
	policy.MaxHosts = 254
	s := NewScanner(policy, blocking)
	allowed := []NetworkRange{{CIDR: "192.168.50.0/24"}}
	first, err := s.Start("192.168.50.0/24", 9100, allowed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start("192.168.50.0/24", 9100, allowed); err == nil || !strings.Contains(err.Error(), "در حال اجرا") {
		t.Fatalf("second concurrent scan must be refused, got %v", err)
	}
	if !s.Cancel(first.ID) {
		t.Fatal("the running scan must be cancellable")
	}
	done := waitScan(t, s, first.ID, 3*time.Second)
	if !done.Cancelled {
		t.Fatalf("expected cancelled snapshot, got %+v", done)
	}
}

func TestScannerCancelStopsProgress(t *testing.T) {
	blocking := func(ctx context.Context, network, address string) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	policy := scanPolicy()
	policy.Workers = 2
	policy.PerHostTimeout = time.Minute
	policy.TotalTimeout = time.Minute
	s := NewScanner(policy, blocking)
	snap, err := s.Start("192.168.50.0/24", 9100, []NetworkRange{{CIDR: "192.168.50.0/24"}})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if !s.Cancel(snap.ID) {
		t.Fatal("cancel must report success for a running scan")
	}
	done := waitScan(t, s, snap.ID, 3*time.Second)
	if !done.Cancelled {
		t.Fatalf("cancelled flag missing: %+v", done)
	}
	// Cancellation must be idempotent-safe: a finished scan cannot be cancelled.
	if s.Cancel(snap.ID) {
		t.Fatal("finished scan must not be cancellable")
	}
	if done.Done >= done.Total {
		t.Fatalf("cancel should stop before all hosts are checked: %+v", done)
	}
}

func TestLocalNetworksNeverReturnsUnsafeRanges(t *testing.T) {
	ranges, err := LocalNetworks()
	if err != nil {
		if strings.TrimSpace(err.Error()) == "" {
			t.Fatal("empty error")
		}
		return
	}
	for _, r := range ranges {
		if !strings.HasSuffix(r.CIDR, "/24") {
			t.Fatalf("range %q is not /24", r.CIDR)
		}
		ip := net.ParseIP(strings.TrimSuffix(r.CIDR, "/24"))
		if ip == nil || ip.To4() == nil {
			t.Fatalf("invalid range %q", r.CIDR)
		}
		if ip.IsLoopback() || !ip.IsPrivate() {
			t.Fatalf("unsafe range %q returned", r.CIDR)
		}
	}
}
