package printer

import (
	"strings"
	"testing"
)

func TestValidateNetworkTargetPolicy(t *testing.T) {
	valid := []struct {
		host string
		port int
	}{
		{"192.168.1.50", 9100},
		{"10.0.0.9", 9100},
		{"172.16.5.4", 9100},
		{"127.0.0.1", 9100}, // documented offline-only extension for local bridges/tests
	}
	for _, tc := range valid {
		if err := ValidateNetworkTarget(tc.host, tc.port); err != nil {
			t.Fatalf("valid target %s:%d rejected: %v", tc.host, tc.port, err)
		}
	}
	invalid := []struct {
		host string
		port int
		want string
	}{
		{"8.8.8.8", 9100, "خصوصی"},
		{"172.32.0.1", 9100, "خصوصی"},
		{"192.168.1.0", 9100, "شبکه"},
		{"192.168.1.255", 9100, "broadcast"},
		{"0.0.0.0", 9100, "multicast"},
		{"224.0.0.2", 9100, "multicast"},
		{"169.254.10.10", 9100, "link-local"},
		{"localhost", 9100, "IPv4"},
		{"printer.local", 9100, "IPv4"},
		{"::1", 9100, "IPv4"},
		{"", 9100, "الزامی"},
		{"192.168.1.9", 0, "پورت"},
		{"192.168.1.9", 70000, "پورت"},
	}
	for _, tc := range invalid {
		err := ValidateNetworkTarget(tc.host, tc.port)
		if err == nil {
			t.Fatalf("target %q:%d must be rejected", tc.host, tc.port)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("error for %q:%d = %q, want substring %q", tc.host, tc.port, err, tc.want)
		}
	}
}

func TestProfileValidation(t *testing.T) {
	base := Profile{
		Name: "چاپگر صندوق", Kind: KindNetwork, Host: "192.168.1.20", Port: 9100,
		PaperMM: Paper80, WidthDots: 576, Copies: 1, Active: true,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid profile rejected: %v", err)
	}
	cases := []struct {
		name string
		edit func(*Profile)
		want string
	}{
		{"empty name", func(p *Profile) { p.Name = " " }, "نام چاپگر"},
		{"bad paper", func(p *Profile) { p.PaperMM = 76 }, "۵۸"},
		{"width not aligned", func(p *Profile) { p.WidthDots = 577 }, "مضرب"},
		{"width too small", func(p *Profile) { p.WidthDots = 64 }, "عرض"},
		{"too many copies", func(p *Profile) { p.Copies = 4 }, "نسخه"},
		{"unknown kind", func(p *Profile) { p.Kind = "bluetooth" }, "نوع اتصال"},
		{"network needs host", func(p *Profile) { p.Host = "" }, "IP"},
		{"spooler needs queue", func(p *Profile) { p.Kind = KindSpooler; p.QueueName = "" }, "صف"},
		{"usb disabled path", func(p *Profile) { p.Kind = KindUSB; p.USB.DevicePath = "" }, "USB"},
		{"usb wrong path", func(p *Profile) { p.Kind = KindUSB; p.USB.DevicePath = "/dev/sda" }, "usblp"},
		{"usb traversal", func(p *Profile) { p.Kind = KindUSB; p.USB.DevicePath = "/dev/usb/../sda" }, "نامعتبر"},
	}
	for _, tc := range cases {
		p := base
		tc.edit(&p)
		err := p.Validate()
		if err == nil {
			t.Fatalf("%s: expected rejection", tc.name)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: error %q, want %q", tc.name, err, tc.want)
		}
	}
	if err := ValidateQueueName("POS-80\n; rm -rf /"); err == nil {
		t.Fatal("control characters in a queue name must be rejected")
	}
}

func TestProfileUtility(t *testing.T) {
	p := Profile{PaperMM: Paper58, WidthDots: 384, Kind: KindNetwork, Host: "192.168.1.2", Port: 9100}
	if got := p.PaperColumns(); got != 32 {
		t.Fatalf("58mm columns = %d, want 32", got)
	}
	p.PaperMM, p.WidthDots = Paper80, 576
	if got := p.PaperColumns(); got != 48 {
		t.Fatalf("80mm columns = %d, want 48", got)
	}
	if p.ConnectionKey() != "network|192.168.1.2|9100" {
		t.Fatalf("connection key = %q", p.ConnectionKey())
	}
	if p.TargetLabel() != "network:192.168.1.2:9100" {
		t.Fatalf("target label = %q", p.TargetLabel())
	}
	usb := USBDevice{VendorID: "04b8", ProductID: "0e15", Serial: "ABC", DevicePath: "/dev/usb/lp0"}
	if usb.Identity() != "usb|path|/dev/usb/lp0" {
		t.Fatalf("usb identity = %q", usb.Identity())
	}
	usb.DevicePath = ""
	if usb.Identity() != "usb|04B8:0E15|ABC" {
		t.Fatalf("serial identity = %q", usb.Identity())
	}
}

func TestConnectionKeyPreventsDuplicateDestinations(t *testing.T) {
	a := Profile{Kind: KindNetwork, Host: "192.168.1.5", Port: 9100}
	b := Profile{Kind: KindNetwork, Host: "192.168.1.5", Port: 9100, Name: "another name"}
	if a.ConnectionKey() != b.ConnectionKey() {
		t.Fatal("same destination must produce the same identity")
	}
	c := Profile{Kind: KindSpooler, QueueName: "POS-80"}
	d := Profile{Kind: KindSpooler, QueueName: "pos-80"}
	if c.ConnectionKey() != d.ConnectionKey() {
		t.Fatal("queue names must compare case-insensitively")
	}
}
