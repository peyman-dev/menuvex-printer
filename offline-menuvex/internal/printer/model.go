// Package printer implements the printer subsystem that MenuVex Offline uses to
// discover, validate, probe and talk to receipt printers without the Tauri
// Printer Agent.
//
// The package is deliberately free of HTTP, templates and storage concerns so
// the same validation and transport rules are used by the UI, the queue worker
// and the tests. It mirrors the behaviour that was audited in the Rust agent
// (private-LAN RAW 9100, installed OS print queues, USB printer-class devices)
// and adds an explicit, layered probe so “TCP is open” is never reported as
// “a receipt was printed”.
package printer

import (
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strings"
)

// Connection kinds understood by the Offline printer subsystem. They match the
// three transports of the Printer Agent: network, spooler and direct USB.
const (
	KindNetwork = "network"
	KindSpooler = "spooler"
	KindUSB     = "usb"

	// ProtocolESCPOS is the only document protocol the Offline app generates.
	ProtocolESCPOS = "escpos"

	// DefaultNetworkPort is the standard RAW/JetDirect port used by the Agent.
	DefaultNetworkPort = 9100

	// MaxPrinters mirrors the Agent profile limit. It keeps the printer centre
	// and every print job snapshot bounded.
	MaxPrinters = 16

	// Paper widths supported by the Agent (58 mm and 80 mm mechanisms).
	Paper58 = 58
	Paper80 = 80

	// Width dots are the raster widths validated by the Agent. The Offline app
	// derives character columns from them for text-mode ESC/POS.
	MinWidthDots = 128
	MaxWidthDots = 832
)

// USBDevice is one candidate printer-class USB device. DevicePath is only set
// when this operating system exposes a path that can actually receive bytes
// (on Linux, the usblp device node). A discovered device without a path is
// still shown to the operator, but it cannot be added for direct printing.
type USBDevice struct {
	DevicePath   string `json:"devicePath,omitempty"`
	VendorID     string `json:"vendorId,omitempty"`
	ProductID    string `json:"productId,omitempty"`
	Serial       string `json:"serial,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty"`
	Product      string `json:"product,omitempty"`
	Bus          string `json:"bus,omitempty"`
	PortPath     string `json:"portPath,omitempty"`
	Accessible   bool   `json:"accessible"`
	AccessNote   string `json:"accessNote,omitempty"`
	System       string `json:"system,omitempty"`
}

// Identity is the stable deduplication key of a USB device. Serial is preferred
// when available (the Agent matches VID/PID/serial); physical topology is used
// for serial-less devices, exactly like the Agent.
func (d USBDevice) Identity() string {
	if d.DevicePath != "" {
		return "usb|path|" + d.DevicePath
	}
	if d.VendorID != "" && d.ProductID != "" && d.Serial != "" {
		return fmt.Sprintf("usb|%s:%s|%s", strings.ToUpper(d.VendorID), strings.ToUpper(d.ProductID), d.Serial)
	}
	return fmt.Sprintf("usb|%s:%s|bus%s|%s", strings.ToUpper(d.VendorID), strings.ToUpper(d.ProductID), d.Bus, d.PortPath)
}

// DisplayName returns a human-readable label without inventing a model name.
func (d USBDevice) DisplayName() string {
	for _, candidate := range []string{d.Product, d.Manufacturer} {
		if strings.TrimSpace(candidate) != "" {
			return strings.TrimSpace(candidate)
		}
	}
	if d.VendorID != "" {
		return fmt.Sprintf("USB Printer %s:%s", d.VendorID, d.ProductID)
	}
	return "USB Printer"
}

// Profile is one saved printer. Field names intentionally describe the
// operator-visible values; the storage layer maps them to entity fields.
type Profile struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Model      string `json:"model,omitempty"`
	Station    string `json:"station,omitempty"`
	Role       string `json:"role,omitempty"` // cashier | kitchen | all
	Kind       string `json:"kind"`
	Host       string `json:"host,omitempty"`
	Port       int    `json:"port,omitempty"`
	QueueName  string `json:"queueName,omitempty"`
	USB        USBDevice
	PaperMM    int    `json:"paperMm"`
	WidthDots  int    `json:"widthDots"`
	Copies     int    `json:"copies"`
	Cut        bool   `json:"cut"`
	Active     bool   `json:"active"`
	CreatedAt  string `json:"createdAt,omitempty"`
	UpdatedAt  string `json:"updatedAt,omitempty"`
	ConfigNote string `json:"configNote,omitempty"` // validation hint for legacy records

	// LastCheck* is the persisted evidence of the most recent verification.
	LastCheckAt     string       `json:"lastCheckAt,omitempty"`
	LastCheckState  string       `json:"lastCheckState,omitempty"`
	LastCheckDetail string       `json:"lastCheckDetail,omitempty"`
	LastCheckStages []ProbeStage `json:"lastCheckStages,omitempty"`
}

// Normalize applies safe defaults without hiding invalid values. It is used
// before validation and before writing a profile.
func (p Profile) Normalize() Profile {
	p.ID = strings.TrimSpace(p.ID)
	p.Name = strings.TrimSpace(p.Name)
	p.Model = strings.TrimSpace(p.Model)
	p.Station = strings.TrimSpace(p.Station)
	p.Role = strings.ToLower(strings.TrimSpace(p.Role))
	p.Kind = strings.ToLower(strings.TrimSpace(p.Kind))
	p.Host = strings.TrimSpace(p.Host)
	p.QueueName = strings.TrimSpace(p.QueueName)
	p.USB.DevicePath = strings.TrimSpace(p.USB.DevicePath)
	p.USB.VendorID = strings.TrimSpace(p.USB.VendorID)
	p.USB.ProductID = strings.TrimSpace(p.USB.ProductID)
	p.USB.Serial = strings.TrimSpace(p.USB.Serial)
	p.USB.Bus = strings.TrimSpace(p.USB.Bus)
	p.USB.PortPath = strings.TrimSpace(p.USB.PortPath)
	p.USB.Manufacturer = strings.TrimSpace(p.USB.Manufacturer)
	p.USB.Product = strings.TrimSpace(p.USB.Product)
	if p.PaperMM == 0 {
		p.PaperMM = Paper80
	}
	if p.WidthDots == 0 {
		p.WidthDots = DefaultWidthDots(p.PaperMM)
	}
	if p.Copies == 0 {
		p.Copies = 1
	}
	if p.Kind == KindNetwork && p.Port == 0 {
		p.Port = DefaultNetworkPort
	}
	return p
}

// DefaultWidthDots is the raster width the Agent uses for each paper size.
func DefaultWidthDots(paperMM int) int {
	if paperMM == Paper58 {
		return 384
	}
	return 576
}

// PaperColumns is the text-mode column count used by the offline ESC/POS
// builder. 12 dots per character is the standard Font A cell, which is why the
// Agent's default widths (576/384) produce 48/32 columns.
func (p Profile) PaperColumns() int {
	width := p.WidthDots
	if width < MinWidthDots || width > MaxWidthDots {
		width = DefaultWidthDots(p.PaperMM)
	}
	cols := width / 12
	if cols < 24 {
		cols = 24
	}
	if cols > 64 {
		cols = 64
	}
	return cols
}

// EffectiveWidthDots is the validated/default raster width.
func (p Profile) EffectiveWidthDots() int {
	if p.WidthDots < MinWidthDots || p.WidthDots > MaxWidthDots || p.WidthDots%8 != 0 {
		return DefaultWidthDots(p.PaperMM)
	}
	return p.WidthDots
}

// Validate enforces the audited Agent rules. It returns operator-readable
// Persian errors because the same text is shown by the printer centre.
func (p Profile) Validate() error {
	p = p.Normalize()
	if p.Name == "" {
		return errors.New("نام چاپگر الزامی است")
	}
	if len([]rune(p.Name)) > 128 || hasControl(p.Name) {
		return errors.New("نام چاپگر باید حداکثر ۱۲۸ نویسه و بدون نویسه کنترلی باشد")
	}
	if p.Model != "" && (len([]rune(p.Model)) > 128 || hasControl(p.Model)) {
		return errors.New("مدل چاپگر نامعتبر است")
	}
	if p.Station != "" && (len([]rune(p.Station)) > 64 || hasControl(p.Station)) {
		return errors.New("ایستگاه چاپگر نامعتبر است")
	}
	if p.PaperMM != Paper58 && p.PaperMM != Paper80 {
		return errors.New("عرض کاغذ فقط ۵۸ یا ۸۰ میلی‌متر است")
	}
	width := p.WidthDots
	if width < MinWidthDots || width > MaxWidthDots || width%8 != 0 {
		return fmt.Errorf("عرض نقطه‌ای باید بین %d و %d و مضرب ۸ باشد", MinWidthDots, MaxWidthDots)
	}
	if p.Copies < 1 || p.Copies > 3 {
		return errors.New("تعداد نسخه چاپگر باید بین ۱ و ۳ باشد")
	}
	switch p.Kind {
	case KindNetwork:
		return ValidateNetworkTarget(p.Host, p.Port)
	case KindSpooler:
		if err := ValidateQueueName(p.QueueName); err != nil {
			return err
		}
	case KindUSB:
		if err := ValidateUSBPath(p.USB.DevicePath); err != nil {
			return err
		}
	default:
		return errors.New("نوع اتصال چاپگر باید network، spooler یا usb باشد")
	}
	return nil
}

// ValidateNetworkTarget mirrors printers::network::address in the Agent:
// literal IPv4 only, no DNS, private LAN unicast, no network/broadcast address.
// Loopback is accepted as a documented Offline-only extension because the
// offline test suite and local bridge queues use 127.0.0.1; subnet scanning
// never includes loopback.
func ValidateNetworkTarget(host string, port int) error {
	host = strings.TrimSpace(host)
	if host == "" {
		return errors.New("آدرس IP چاپگر شبکه الزامی است")
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() == nil {
		return errors.New("آدرس IP باید یک IPv4 عددی باشد؛ نام دامنه (DNS) پشتیبانی نمی‌شود")
	}
	ip4 := ip.To4()
	if ip4.IsUnspecified() || ip4.IsMulticast() || ip4.IsLinkLocalUnicast() || ip4.IsLinkLocalMulticast() {
		return errors.New("آدرس IP باید یک نشانی یکتای شبکه محلی باشد (نه multicast/link-local/0.0.0.0)")
	}
	if !ip4.IsPrivate() && !ip4.IsLoopback() {
		return errors.New("فقط آدرس IPv4 خصوصی شبکه محلی (یا 127.0.0.1 برای آزمون محلی) مجاز است")
	}
	if ip4[3] == 0 || ip4[3] == 255 {
		return errors.New("آدرس شبکه یا broadcast قابل استفاده برای چاپگر نیست")
	}
	if port < 1 || port > 65535 {
		return errors.New("پورت چاپگر باید بین ۱ و ۶۵۵۳۵ باشد")
	}
	return nil
}

// ValidateQueueName checks an installed OS print queue name. It is passed as a
// single argv element to lp, never through a shell.
func ValidateQueueName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("نام صف چاپ سیستم‌عامل الزامی است")
	}
	if len([]rune(name)) > 256 || hasControl(name) || strings.ContainsAny(name, "\x00") {
		return errors.New("نام صف چاپ نامعتبر است")
	}
	return nil
}

// ValidateUSBPath protects against path traversal and accidental file writes.
func ValidateUSBPath(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("مسیر دستگاه USB برای چاپ مستقیم ثبت نشده است؛ از یک صف چاپ سیستم‌عامل استفاده کنید")
	}
	if hasControl(path) || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("مسیر دستگاه USB نامعتبر است")
	}
	if !strings.HasPrefix(path, "/dev/usb/lp") {
		return errors.New("چاپ مستقیم USB فقط از مسیر دستگاه usblp (‎/dev/usb/lp*) پشتیبانی می‌شود")
	}
	return nil
}

// ConnectionKey is the identity used to prevent duplicate printer records.
func (p Profile) ConnectionKey() string {
	p = p.Normalize()
	switch p.Kind {
	case KindNetwork:
		return fmt.Sprintf("network|%s|%d", strings.ToLower(p.Host), p.Port)
	case KindSpooler:
		return "spooler|" + strings.ToLower(p.QueueName)
	case KindUSB:
		return p.USB.Identity()
	}
	return ""
}

// TargetLabel is a stable diagnostic string (no receipt data).
func (p Profile) TargetLabel() string {
	p = p.Normalize()
	switch p.Kind {
	case KindNetwork:
		return fmt.Sprintf("network:%s:%d", p.Host, p.Port)
	case KindSpooler:
		return "queue:" + p.QueueName
	case KindUSB:
		return "usb:" + p.USB.DevicePath
	}
	return ""
}

// KindLabel is the Persian label used by the UI.
func (p Profile) KindLabel() string {
	switch p.Normalize().Kind {
	case KindNetwork:
		return "چاپگر شبکه"
	case KindSpooler:
		return "صف چاپ سیستم‌عامل"
	case KindUSB:
		return "USB مستقیم"
	}
	if p.ID != "" {
		return "چاپگر قدیمی (بدون نوع)"
	}
	return "نامشخص"
}

// ConnectionDetail is shown next to a printer in the list.
func (p Profile) ConnectionDetail() string {
	p = p.Normalize()
	switch p.Kind {
	case KindNetwork:
		return fmt.Sprintf("%s:%d (RAW)", p.Host, p.Port)
	case KindSpooler:
		return p.QueueName
	case KindUSB:
		detail := p.USB.DevicePath
		if p.USB.VendorID != "" {
			detail += fmt.Sprintf(" — %s:%s", p.USB.VendorID, p.USB.ProductID)
		}
		return detail
	}
	return ""
}

// StationLabel maps the free-text station to the destination roles the order
// engine understands.
func StationLabel(station string) string {
	switch strings.ToLower(strings.TrimSpace(station)) {
	case "cashier", "صندوق", "صندوق فروش":
		return "صندوق"
	case "kitchen", "آشپزخانه", "بار", "بار گرم", "آشپزخانه و بار":
		return "آشپزخانه و بار"
	}
	if strings.TrimSpace(station) == "" {
		return "بدون ایستگاه"
	}
	return station
}

func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}
