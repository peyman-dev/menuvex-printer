package printer

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// Stage states. "pass" means the stage produced positive evidence; "warn"
// means it could not be confirmed; "fail" means it explicitly failed; "skip"
// means it was not attempted.
const (
	StagePass = "pass"
	StageWarn = "warn"
	StageFail = "fail"
	StageSkip = "skip"
)

// ProbeStage is one independently reported verification step. The whole point
// of the layered probe is that a reachable TCP port is never presented as a
// working printer.
type ProbeStage struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	State  string `json:"state"`
	Detail string `json:"detail"`
}

// ProbeResult is the complete, operator-visible result of a probe.
type ProbeResult struct {
	Host          string       `json:"host"`
	Port          int          `json:"port"`
	State         string       `json:"state"` // online | unconfirmed | offline | invalid
	Reachable     bool         `json:"reachable"`
	Responded     bool         `json:"responded"`
	LikelyPrinter bool         `json:"likelyPrinter"`
	Summary       string       `json:"summary"`
	Stages        []ProbeStage `json:"stages"`
	CheckedAt     string       `json:"checkedAt"`
}

// State labels used by UI badges.
const (
	ProbeOnline      = "online"
	ProbeUnconfirmed = "unconfirmed"
	ProbeOffline     = "offline"
	ProbeInvalid     = "invalid"
)

// DialFunc is the injectable TCP dialer used by probes, scanners and network
// tests. Production uses net.Dialer.
type DialFunc func(ctx context.Context, network, address string) (net.Conn, error)

// DefaultDial is the production dialer.
func DefaultDial(ctx context.Context, network, address string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, network, address)
}

// Prober performs the layered network verification.
type Prober struct {
	Dial           DialFunc
	ConnectTimeout time.Duration
	StatusTimeout  time.Duration
}

// NewProber returns a prober with the audited Agent timeouts: a 3 s connect
// probe and a short read window for the printer status byte.
func NewProber(dial DialFunc) *Prober {
	if dial == nil {
		dial = DefaultDial
	}
	return &Prober{Dial: dial, ConnectTimeout: 3 * time.Second, StatusTimeout: 1500 * time.Millisecond}
}

// Probe performs the following bounded checks:
//
//  1. address policy (literal private IPv4 + valid port),
//  2. TCP connect within ConnectTimeout,
//  3. ESC/POS real-time status request (DLE EOT 1) with a short read window.
//
// A silent-but-open port is reported as "unconfirmed", never as a printer.
// Nothing is printed by Probe.
func (p *Prober) Probe(ctx context.Context, host string, port int) ProbeResult {
	host = strings.TrimSpace(host)
	res := ProbeResult{Host: host, Port: port, CheckedAt: time.Now().UTC().Format(time.RFC3339), State: ProbeInvalid}
	if err := ValidateNetworkTarget(host, port); err != nil {
		res.Stages = append(res.Stages, ProbeStage{
			Key: "address", Label: "اعتبارسنجی نشانی", State: StageFail, Detail: err.Error(),
		})
		res.Summary = "نشانی معتبر نیست: " + err.Error()
		return res
	}
	res.Stages = append(res.Stages, ProbeStage{
		Key: "address", Label: "اعتبارسنجی نشانی", State: StagePass,
		Detail: "IPv4 خصوصی و پورت معتبر است",
	})

	addr := net.JoinHostPort(host, strconv.Itoa(port))
	dialCtx, cancel := context.WithTimeout(ctx, p.ConnectTimeout)
	defer cancel()
	start := time.Now()
	conn, err := p.Dial(dialCtx, "tcp", addr)
	elapsed := time.Since(start).Milliseconds()
	if err != nil {
		res.Stages = append(res.Stages,
			ProbeStage{Key: "reachable", Label: "دسترسی TCP", State: StageFail, Detail: err.Error()},
			ProbeStage{Key: "status", Label: "پاسخ پروتکل چاپگر", State: StageSkip, Detail: "چون اتصال برقرار نشد بررسی نشد"},
		)
		res.Summary = fmt.Sprintf("در %d میلی‌ثانیه هیچ اتصالی به %s برقرار نشد؛ چاپگر خاموش، جدا از شبکه یا نشانی اشتباه است.", elapsed, addr)
		res.State = ProbeOffline
		return res
	}
	defer conn.Close()
	res.Reachable = true
	res.Stages = append(res.Stages, ProbeStage{
		Key: "reachable", Label: "دسترسی TCP", State: StagePass,
		Detail: fmt.Sprintf("پورت %d در %d میلی‌ثانیه باز شد", port, elapsed),
	})

	// A TCP handshake only proves a listener exists. Ask the printer class for
	// its real-time status; no response is common among network bridges and is
	// therefore inconclusive, not a failure.
	_ = conn.SetWriteDeadline(time.Now().Add(p.StatusTimeout))
	if _, werr := conn.Write([]byte{0x10, 0x04, 0x01}); werr != nil {
		res.Stages = append(res.Stages, ProbeStage{
			Key: "status", Label: "پاسخ پروتکل چاپگر", State: StageWarn,
			Detail: "اتصال باز است اما ارسال درخواست وضعیت ممکن نشد: " + werr.Error(),
		})
		res.Summary = "پورت TCP باز است اما وضعیت چاپگر تأیید نشد؛ این هنوز به معنای چاپ موفق نیست."
		res.State = ProbeUnconfirmed
		return res
	}
	_ = conn.SetReadDeadline(time.Now().Add(p.StatusTimeout))
	status := make([]byte, 8)
	n, rerr := conn.Read(status)
	if n > 0 {
		res.Responded = true
		res.LikelyPrinter = plausibleStatus(status[0])
		if res.LikelyPrinter {
			res.Stages = append(res.Stages, ProbeStage{
				Key: "status", Label: "پاسخ پروتکل چاپگر", State: StagePass,
				Detail: fmt.Sprintf("پاسخ ESC/POS دریافت شد (0x%02X)؛ دستگاه الگوی وضعیت چاپگر حرارتی را دارد", status[0]),
			})
			res.Summary = "چاپگر به درخواست وضعیت ESC/POS پاسخ داد. برای اطمینان از چاپ فیزیکی، «چاپ آزمایشی» را بزنید."
			res.State = ProbeOnline
			return res
		}
		res.Stages = append(res.Stages, ProbeStage{
			Key: "status", Label: "پاسخ پروتکل چاپگر", State: StageWarn,
			Detail: fmt.Sprintf("دستگاه پاسخ داد (0x%02X) اما الگوی وضعیت ESC/POS استاندارد نیست", status[0]),
		})
		res.Summary = "پورت باز است و دستگاهی پاسخ داد، اما نوع آن چاپگر حرارتی تأیید نشد."
		res.State = ProbeUnconfirmed
		return res
	}
	detail := "چاپگرهای RAW اغلب به درخواست وضعیت پاسخ نمی‌دهند"
	if rerr != nil {
		detail += ": " + rerr.Error()
	}
	res.Stages = append(res.Stages, ProbeStage{Key: "status", Label: "پاسخ پروتکل چاپگر", State: StageWarn, Detail: detail})
	res.Summary = "پورت TCP باز است اما پاسخی از پروتکل چاپگر نرسید؛ «باز بودن پورت» به معنای چاپ موفق نیست."
	res.State = ProbeUnconfirmed
	return res
}

// plausibleStatus rejects clearly non-ESC/POS bytes while accepting the
// documented status formats (bit 0 fixed at 0 for DLE EOT responses).
func plausibleStatus(b byte) bool {
	if b == 0x00 {
		return true
	}
	// Real-time status bytes are 0bxxxxxxx0 (bit 0 clear) with at least one
	// meaningful state bit achievable. Reject printable ASCII such as 'H'
	// (0x48) unless it matches a known status bit pattern; in practice status
	// bytes are <= 0x3F.
	return b&0x01 == 0 && b <= 0x3F
}

// ProbeJSON is a convenience wrapper for handlers.
func (r ProbeResult) StateLabel() string {
	switch r.State {
	case ProbeOnline:
		return "آنلاین (پاسخ پروتکل)"
	case ProbeUnconfirmed:
		return "پورت باز، تأییدنشده"
	case ProbeOffline:
		return "در دسترس نیست"
	default:
		return "نشانی نامعتبر"
	}
}

// StateClass maps the probe state to the existing badge classes.
func (r ProbeResult) StateClass() string {
	switch r.State {
	case ProbeOnline:
		return "status-ok"
	case ProbeUnconfirmed:
		return "status-warn"
	case ProbeOffline:
		return "status-danger"
	default:
		return "status-muted"
	}
}

// LayerLabel renders a stage for compact UI lists.
func (s ProbeStage) LayerLabel() string {
	switch s.State {
	case StagePass:
		return "تأیید شد"
	case StageWarn:
		return "تأیید نشد"
	case StageFail:
		return "ناموفق"
	default:
		return "انجام نشد"
	}
}

// LayerClass maps a stage state to a badge class.
func (s ProbeStage) LayerClass() string {
	switch s.State {
	case StagePass:
		return "status-ok"
	case StageWarn:
		return "status-warn"
	case StageFail:
		return "status-danger"
	default:
		return "status-muted"
	}
}
