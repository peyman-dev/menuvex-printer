package printer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ScanPolicy contains the safety bounds of a network scan. The numbers are
// deliberately conservative: one local /24, a short per-host timeout and a
// small worker pool, so a scan can never turn into an internet port sweep.
type ScanPolicy struct {
	MaxHosts       int
	Workers        int
	PerHostTimeout time.Duration
	TotalTimeout   time.Duration
	ResultTTL      time.Duration
}

// DefaultScanPolicy matches the audited Agent discovery budget (400 ms per
// host, 64 workers on a /24) with a lower worker count for the embedded app.
func DefaultScanPolicy() ScanPolicy {
	return ScanPolicy{
		MaxHosts:       254,
		Workers:        32,
		PerHostTimeout: 400 * time.Millisecond,
		TotalTimeout:   20 * time.Second,
		ResultTTL:      10 * time.Minute,
	}
}

// NetworkRange is one local /24 the operator is allowed to scan.
type NetworkRange struct {
	CIDR          string `json:"cidr"`
	InterfaceName string `json:"interfaceName"`
	Address       string `json:"address"`
	Hosts         int    `json:"hosts"`
}

// LocalNetworks lists the private IPv4 /24 networks of this machine. It never
// invents ranges and never returns loopback, link-local or public networks.
func LocalNetworks() ([]NetworkRange, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []NetworkRange
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, aErr := iface.Addrs()
		if aErr != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipNet.IP.To4()
			if ip4 == nil || !ip4.IsPrivate() || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() {
				continue
			}
			ones, bits := ipNet.Mask.Size()
			if bits != 32 || ones < 8 || ones >= 32 {
				continue
			}
			// The Agent scans exactly one /24. When this interface is smaller
			// than a /24 (e.g. /16), stay inside it by using the /24 fragment
			// that contains this address.
			base := net.IPv4(ip4[0], ip4[1], ip4[2], 0).To4()
			cidr := base.String() + "/24"
			if seen[cidr] {
				continue
			}
			seen[cidr] = true
			out = append(out, NetworkRange{
				CIDR:          cidr,
				InterfaceName: iface.Name,
				Address:       ip4.String(),
				Hosts:         254,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		pi, pj := networkPriority(out[i]), networkPriority(out[j])
		if pi != pj {
			return pi < pj
		}
		return out[i].CIDR < out[j].CIDR
	})
	if len(out) == 0 {
		return nil, errors.New("هیچ شبکه محلی خصوصی فعالی پیدا نشد؛ برای اسکن، ابتدا دستگاه را به شبکه کافه وصل کنید یا چاپگر را دستی اضافه کنید")
	}
	return out, nil
}

func networkPriority(r NetworkRange) int {
	name := strings.ToLower(r.InterfaceName)
	for _, virt := range []string{"tun", "tap", "utun", "wg", "docker", "veth", "br-", "virbr", " tailscale"} {
		if strings.HasPrefix(name, strings.TrimSpace(virt)) {
			return 3
		}
	}
	if strings.HasPrefix(r.CIDR, "192.168.") {
		return 0
	}
	if strings.HasPrefix(r.CIDR, "10.") || strings.HasPrefix(r.CIDR, "172.") {
		return 1
	}
	return 2
}

// ScanResult is one open host. Only open RAW ports are returned; no device is
// ever added to the printer list by the scan itself.
type ScanResult struct {
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Open      bool   `json:"open"`
	CheckedAt string `json:"checkedAt"`
}

// ScanStatus is an immutable snapshot for the UI and the JSON endpoint.
type ScanStatus struct {
	ID         string       `json:"id"`
	CIDR       string       `json:"cidr"`
	Port       int          `json:"port"`
	Total      int          `json:"total"`
	Done       int          `json:"done"`
	Open       int          `json:"open"`
	Running    bool         `json:"running"`
	Cancelled  bool         `json:"cancelled"`
	Error      string       `json:"error,omitempty"`
	StartedAt  string       `json:"startedAt"`
	FinishedAt string       `json:"finishedAt,omitempty"`
	Results    []ScanResult `json:"results"`
}

// Percent is safe for progress bars (0–100).
func (s ScanStatus) Percent() int {
	if s.Total <= 0 {
		return 0
	}
	p := s.Done * 100 / s.Total
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}

var (
	ErrScanBusy   = errors.New("یک جستجوی شبکه دیگر در حال اجراست؛ ابتدا آن را متوقف کنید")
	ErrScanRange  = errors.New("این محدوده در شبکه‌های محلی مجاز این دستگاه نیست")
	ErrScanLimit  = errors.New("تعداد میزبان‌های محدوده بیش از حد مجاز است")
	errScanClosed = errors.New("جستجو پیدا نشد")
)

type scanState struct {
	status  ScanStatus
	cancel  context.CancelFunc
	seen    map[string]bool
	results []ScanResult
}

// Scanner runs at most one bounded local-network scan at a time. All state is
// in memory; a restart simply forgets old scans.
type Scanner struct {
	mu     sync.Mutex
	policy ScanPolicy
	dial   DialFunc
	scans  map[string]*scanState
	order  []string
	nextID int
}

// NewScanner creates a scanner with the given policy and dialer.
func NewScanner(policy ScanPolicy, dial DialFunc) *Scanner {
	if policy.Workers <= 0 {
		policy.Workers = 32
	}
	if policy.MaxHosts <= 0 {
		policy.MaxHosts = 254
	}
	if policy.PerHostTimeout <= 0 {
		policy.PerHostTimeout = 400 * time.Millisecond
	}
	if policy.TotalTimeout <= 0 {
		policy.TotalTimeout = 20 * time.Second
	}
	if policy.ResultTTL <= 0 {
		policy.ResultTTL = 10 * time.Minute
	}
	if dial == nil {
		dial = DefaultDial
	}
	return &Scanner{policy: policy, dial: dial, scans: map[string]*scanState{}}
}

// Start validates a scan against the supplied local ranges, then launches it
// asynchronously. The returned snapshot is safe to hand to a template.
func (s *Scanner) Start(cidr string, port int, allowed []NetworkRange) (ScanStatus, error) {
	cidr = strings.TrimSpace(cidr)
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil || ipNet.IP.To4() == nil {
		return ScanStatus{}, errors.New("محدوده اسکن باید یک CIDR معتبر IPv4 باشد")
	}
	if ones, bits := ipNet.Mask.Size(); bits != 32 || ones != 24 {
		return ScanStatus{}, errors.New("برای محدود ماندن اسکن، فقط محدوده /24 مجاز است")
	}
	cidr = ipNet.String()
	allowedOK := false
	allowed = append([]NetworkRange(nil), allowed...)
	for _, r := range allowed {
		if strings.TrimSpace(r.CIDR) == cidr {
			allowedOK = true
			break
		}
	}
	if !allowedOK {
		return ScanStatus{}, ErrScanRange
	}
	ip4 := ipNet.IP.To4()
	if ip4 == nil || !ip4.IsPrivate() || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() {
		return ScanStatus{}, ErrScanRange
	}
	if port < 1 || port > 65535 {
		return ScanStatus{}, errors.New("پورت اسکن باید بین ۱ و ۶۵۵۳۵ باشد")
	}
	hosts, err := hostIPs(ipNet)
	if err != nil {
		return ScanStatus{}, err
	}
	if len(hosts) > s.policy.MaxHosts {
		return ScanStatus{}, ErrScanLimit
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.policy.TotalTimeout)
	id := ""
	s.mu.Lock()
	for _, existing := range s.scans {
		if existing.status.Running {
			s.mu.Unlock()
			cancel()
			return ScanStatus{}, ErrScanBusy
		}
	}
	s.nextID++
	id = fmt.Sprintf("scan-%d-%d", time.Now().UnixNano(), s.nextID)
	st := &scanState{
		status: ScanStatus{
			ID:        id,
			CIDR:      cidr,
			Port:      port,
			Total:     len(hosts),
			Running:   true,
			StartedAt: time.Now().UTC().Format(time.RFC3339),
			Results:   []ScanResult{},
		},
		cancel: cancel,
		seen:   map[string]bool{},
	}
	s.scans[id] = st
	s.order = append(s.order, id)
	s.pruneLocked()
	snapshot := cloneScanStatus(st.status)
	s.mu.Unlock()

	go s.run(ctx, st, hosts, port)
	return snapshot, nil
}

func (s *Scanner) run(ctx context.Context, st *scanState, hosts []net.IP, port int) {
	type job struct{ ip string }
	jobs := make(chan job)
	var wg sync.WaitGroup
	for i := 0; i < s.policy.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				if ctx.Err() != nil {
					return
				}
				hostCtx, cancel := context.WithTimeout(ctx, s.policy.PerHostTimeout)
				conn, err := s.dial(hostCtx, "tcp", net.JoinHostPort(j.ip, strconv.Itoa(port)))
				cancel()
				if err == nil && conn != nil {
					_ = conn.Close()
					s.recordOpen(st, j.ip)
				}
				s.recordDone(st)
			}
		}()
	}
sendLoop:
	for _, ip := range hosts {
		select {
		case <-ctx.Done():
			break sendLoop
		case jobs <- job{ip: ip.String()}:
		}
	}
	close(jobs)
	wg.Wait()
	s.finish(st, ctx.Err())
}

func (s *Scanner) recordOpen(st *scanState, host string) {
	now := time.Now().UTC().Format(time.RFC3339)
	s.mu.Lock()
	defer s.mu.Unlock()
	if st.seen[host] {
		return
	}
	st.seen[host] = true
	st.results = append(st.results, ScanResult{Host: host, Port: st.status.Port, Open: true, CheckedAt: now})
	sort.Slice(st.results, func(i, j int) bool {
		return ipLess(st.results[i].Host, st.results[j].Host)
	})
	st.status.Results = append([]ScanResult(nil), st.results...)
	st.status.Open = len(st.results)
}

func (s *Scanner) recordDone(st *scanState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st.status.Done < st.status.Total {
		st.status.Done++
	}
}

func (s *Scanner) finish(st *scanState, ctxErr error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !st.status.Running {
		return
	}
	st.status.Running = false
	st.status.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	if ctxErr != nil {
		if errors.Is(ctxErr, context.Canceled) {
			st.status.Cancelled = true
		} else if errors.Is(ctxErr, context.DeadlineExceeded) {
			st.status.Error = "زمان مجاز اسکن تمام شد؛ نتایج تا همین مرحله معتبر است"
		}
	}
	st.status.Results = append([]ScanResult(nil), st.results...)
	st.status.Open = len(st.results)
}

// Status returns a copy of one scan.
func (s *Scanner) Status(id string) (ScanStatus, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.scans[id]
	if !ok {
		return ScanStatus{}, false
	}
	return cloneScanStatus(st.status), true
}

// LatestStatus returns the most recent scan snapshot (preferring a running scan).
func (s *Scanner) LatestStatus() (ScanStatus, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.order) - 1; i >= 0; i-- {
		if st, ok := s.scans[s.order[i]]; ok && st.status.Running {
			return cloneScanStatus(st.status), true
		}
	}
	if len(s.order) > 0 {
		if st, ok := s.scans[s.order[len(s.order)-1]]; ok {
			return cloneScanStatus(st.status), true
		}
	}
	return ScanStatus{}, false
}

// Cancel stops a running scan. Finished scans are unaffected.
func (s *Scanner) Cancel(id string) bool {
	s.mu.Lock()
	st, ok := s.scans[id]
	if !ok || !st.status.Running || st.cancel == nil {
		s.mu.Unlock()
		return false
	}
	st.cancel()
	s.mu.Unlock()
	return true
}

// CancelAll is used on shutdown/tests.
func (s *Scanner) CancelAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, st := range s.scans {
		if st.cancel != nil && st.status.Running {
			st.cancel()
		}
	}
}

func (s *Scanner) pruneLocked() {
	cutoff := time.Now().Add(-s.policy.ResultTTL)
	// Keep the newest 3, and at most 10, cancelable/finished snapshots. A
	// running scan is never pruned.
	for len(s.order) > 10 {
		removed := false
		for i, id := range s.order {
			st, ok := s.scans[id]
			if !ok {
				s.order = append(s.order[:i], s.order[i+1:]...)
				removed = true
				break
			}
			if st.status.Running {
				continue
			}
			if i >= len(s.order)-3 {
				break
			}
			if st.status.FinishedAt != "" {
				if t, err := time.Parse(time.RFC3339, st.status.FinishedAt); err == nil && t.Before(cutoff) {
					delete(s.scans, id)
					s.order = append(s.order[:i], s.order[i+1:]...)
					removed = true
					break
				}
			}
		}
		if !removed {
			break
		}
	}
}

func cloneScanStatus(in ScanStatus) ScanStatus {
	out := in
	out.Results = append([]ScanResult(nil), in.Results...)
	return out
}

func hostIPs(network *net.IPNet) ([]net.IP, error) {
	base := network.IP.To4()
	if base == nil {
		return nil, errors.New("محدوده IPv4 نامعتبر است")
	}
	var out []net.IP
	for i := 1; i <= 254; i++ {
		out = append(out, net.IPv4(base[0], base[1], base[2], byte(i)).To4())
	}
	return out, nil
}

func ipLess(a, b string) bool {
	ia, ib := net.ParseIP(a).To4(), net.ParseIP(b).To4()
	if ia == nil || ib == nil {
		return a < b
	}
	for i := 0; i < 4; i++ {
		if ia[i] != ib[i] {
			return ia[i] < ib[i]
		}
	}
	return false
}
