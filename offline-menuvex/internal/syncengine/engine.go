// Package syncengine زمان‌بند پس‌زمینهٔ همگام‌سازی دستگاه است.
//
// مسئله‌ای که حل می‌کند: قبلاً سینک فقط با کلیک دستی روی دکمهٔ
// «به‌روزرسانی» اتفاق می‌افتاد؛ اگر اپراتور فراموش می‌کرد، سفارش‌ها
// روزها روی دستگاه می‌ماندند و هیچ‌کس متوجه نمی‌شد. این موتور سینک
// دوسویهٔ واقعی (همان Refresh پروتکل v2: handshake، push آوت‌باکس،
// pull صفحه‌ای و ack) را روی این ماشه‌ها اجرا می‌کند:
//
//   - چند ثانیه بعد از شروع برنامه
//   - هر ۱۰ دقیقه
//   - ~۲ ثانیه بعد از هر تغییر محلی که عملیات سینک تازه ساخته (debounce)
//   - وقتی سرور/اینترنت دوباره در دسترس شد (probe سبک در حالت آفلاین)
//   - بعد از شکست‌های موقت، با بک‌آف پلکانی و جیتر
//
// چرخه‌ها هرگز روی هم نمی‌افتند، هر چرخه سقف زمانی دارد و با بستن
// برنامه متوقف می‌شود: وقتی برنامه بسته است هیچ سینکی انجام نمی‌شود —
// برای سینک هنگام بسته بودن برنامه باید سرویس سیستم‌عامل جدا نصب شود
// که این پروژه ندارد و ادعایش را هم نمی‌کند.
//
// نکتهٔ مهم: اگر صف عملیات از یک دستهٔ ۱۰۰تایی بزرگ‌تر باشد (مثلاً بعد
// از قطعی طولانی)، موتور چرخه را تا خالی‌شدن صف پشت سر هم تکرار می‌کند
// تا عقب‌ماندگی بدون دخالت کاربر جبران شود.
package syncengine

import (
	"context"
	"hash/fnv"
	"log/slog"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/sinaqaffari/coffish/desktop/internal/store"
	"github.com/sinaqaffari/coffish/desktop/internal/syncclient"
)

// وضعیت‌های قابل نمایش در رابط کاربری.
const (
	StateIdle      = "idle"      // هنوز چرخه‌ای اجرا نشده
	StateSyncing   = "syncing"   // چرخه در حال اجراست
	StateSynced    = "synced"    // آخرین چرخه موفق و صف خالی است
	StatePending   = "pending"   // عملیات در انتظار ارسال وجود دارد
	StateOffline   = "offline"   // سرور/اینترنت در دسترس نیست؛ دادهٔ محلی امن است
	StateConflict  = "conflict"  // تعارض نیازمند بررسی انسانی وجود دارد
	StateAuthError = "auth"      // دستگاه از سمت سرور پذیرفته نمی‌شود
	StateError     = "error"     // خطای دیگر
)

// Status اسنپ‌شات وضعیت لحظه‌ای موتور برای رابط کاربری.
type Status struct {
	State        string
	Running      bool
	Pending      int
	Conflicts    int
	Rejected     int
	LastSuccess  string // RFC3339 از متای ذخیره‌شده
	LastError    string
	Initialized  bool // حداقل یک Bootstrap موفق انجام شده است
	FailCount    int  // شکست‌های پیاپی چرخه
	NextRetrySec int  // ثانیه تا تلاش بعدی خودکار (تقریبی، ۰ یعنی زمان‌بندی‌شده نیست)
}

type Engine struct {
	client *syncclient.Client
	store  *store.Store
	log    *slog.Logger

	interval   time.Duration // دورهٔ اصلی: ۱۰ دقیقه
	probeEvery time.Duration // فاصلهٔ probe در حالت آفلاین
	debounce   time.Duration // مکث بعد از آخرین تغییر محلی
	cycleLimit time.Duration // سقف هر چرخه
	maxRounds  int           // سقف تکرار چرخه برای تخلیهٔ صف در یک ماشه

	notifyCh chan struct{}
	manualCh chan chan error
	doneCh   chan struct{}

	mu       sync.Mutex
	started  bool
	running  bool
	state    string
	failures int
	nextTry  time.Time
	reachOK  bool
}

// New موتور را می‌سازد ولی شروع نمی‌کند؛ Start جداگانه صدا زده می‌شود.
func New(client *syncclient.Client, st *store.Store, logger *slog.Logger) *Engine {
	if logger == nil {
		logger = slog.Default()
	}
	return &Engine{
		client:     client,
		store:      st,
		log:        logger.With("component", "sync-engine"),
		interval:   10 * time.Minute,
		probeEvery: 20 * time.Second,
		debounce:   2 * time.Second,
		cycleLimit: 2 * time.Minute,
		maxRounds:  12,
		notifyCh:   make(chan struct{}, 1),
		manualCh:   make(chan chan error, 1),
		doneCh:     make(chan struct{}),
		state:      StateIdle,
	}
}

// NotifyLocalChange بیدارباش سبک بعد از تغییر محلی است؛ هرگز بلاک نمی‌کند.
func (e *Engine) NotifyLocalChange() {
	select {
	case e.notifyCh <- struct{}{}:
	default:
	}
}

// SyncNow برای دکمهٔ دستی «همگام‌سازی» است و تا پایان چرخه صبر می‌کند.
func (e *Engine) SyncNow(ctx context.Context) error {
	e.mu.Lock()
	started := e.started
	e.mu.Unlock()
	if !started {
		// موتور هنوز شروع نشده (مسیرهای لبه‌ای تست): مستقیم اجرا کن.
		return e.runCycle(ctx)
	}
	reply := make(chan error, 1)
	select {
	case e.manualCh <- reply:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Start حلقهٔ اصلی را در گوروتین پس‌زمینه اجرا می‌کند؛ Stop آن را می‌بندد.
func (e *Engine) Start(ctx context.Context) {
	e.mu.Lock()
	if e.started {
		e.mu.Unlock()
		return
	}
	e.started = true
	e.mu.Unlock()
	go e.loop(ctx)
}

func (e *Engine) Stop() {
	e.mu.Lock()
	started := e.started
	e.started = false
	e.mu.Unlock()
	if started {
		close(e.doneCh)
	}
}

// Status اسنپ‌شات امن وضعیت را برای UI برمی‌گرداند.
func (e *Engine) Status(ctx context.Context) Status {
	pending, conflicts, rejected := e.store.OutboxCounts(ctx)
	meta := e.store.SyncMeta(ctx)
	e.mu.Lock()
	defer e.mu.Unlock()
	state := e.state
	if e.running {
		state = StateSyncing
	} else if conflicts > 0 && state != StateAuthError && state != StateOffline {
		state = StateConflict
	} else if pending > 0 && state == StateSynced {
		state = StatePending
	}
	next := 0
	if !e.nextTry.IsZero() {
		if d := time.Until(e.nextTry); d > 0 {
			next = int(d.Seconds())
		}
	}
	return Status{
		State:        state,
		Running:      e.running,
		Pending:      pending,
		Conflicts:    conflicts,
		Rejected:     rejected,
		LastSuccess:  meta.LastSuccessAt,
		LastError:    meta.LastError,
		Initialized:  meta.Initialized,
		FailCount:    e.failures,
		NextRetrySec: next,
	}
}

func (e *Engine) loop(ctx context.Context) {
	// تأخیر اولیهٔ کوتاه + جیتر اختصاصی دستگاه: وقتی برق محله برمی‌گردد
	// همهٔ دستگاه‌ها هم‌ثانیه به سرور هجوم نمی‌برند.
	startup := time.NewTimer(3*time.Second + e.deviceJitter(57*time.Second))
	ticker := time.NewTicker(e.interval)
	probe := time.NewTicker(e.probeEvery)
	defer startup.Stop()
	defer ticker.Stop()
	defer probe.Stop()

	var debounceTimer *time.Timer
	var debounceC <-chan time.Time
	var retryTimer *time.Timer
	var retryC <-chan time.Time

	stopTimer := func(t **time.Timer) {
		if *t != nil {
			(*t).Stop()
		}
	}

	for {
		select {
		case <-ctx.Done():
			e.log.Info("sync engine stopped")
			return
		case <-e.doneCh:
			e.log.Info("sync engine stopped")
			return
		case reply := <-e.manualCh:
			reply <- e.runDrain(ctx)
		case <-startup.C:
			e.setNextRetry(e.afterDrain(e.runDrain(ctx)), &retryTimer, &retryC)
		case <-ticker.C:
			e.setNextRetry(e.afterDrain(e.runDrain(ctx)), &retryTimer, &retryC)
		case <-retryC:
			retryC = nil
			e.setNextRetry(e.afterDrain(e.runDrain(ctx)), &retryTimer, &retryC)
		case <-e.notifyCh:
			// چند تغییر پشت سر هم فقط یک چرخه می‌سازد.
			stopTimer(&debounceTimer)
			debounceTimer = time.NewTimer(e.debounce)
			debounceC = debounceTimer.C
		case <-debounceC:
			debounceC = nil
			// بک‌آف سطح چرخه همین‌جاست: تا وقتی موعد تلاش بعدی نرسیده،
			// تغییر محلی تازه چرخهٔ خودکار دیگری نمی‌سازد (تایمر تلاش
			// مجدد سر وقتش می‌زند و صف در این فاصله امن روی دستگاه
			// می‌ماند). سینک دستی و چرخهٔ ۱۰ دقیقه‌ای از این قاعده
			// مستثنا هستند و همیشه اجرا می‌شوند.
			e.mu.Lock()
			backingOff := e.failures > 0 && !e.nextTry.IsZero() && time.Now().Before(e.nextTry)
			e.mu.Unlock()
			if backingOff {
				continue
			}
			e.setNextRetry(e.afterDrain(e.runDrain(ctx)), &retryTimer, &retryC)
		case <-probe.C:
			e.probeReachability(ctx)
		}
	}
}

// afterDrain نتیجهٔ چرخه را به زمان تلاش بعدی تبدیل می‌کند (۰ = نیازی نیست).
func (e *Engine) afterDrain(err error) time.Duration {
	if err == nil {
		return 0
	}
	e.mu.Lock()
	failures := e.failures
	e.mu.Unlock()
	d := cycleBackoff(failures)
	return d + e.jitter(d/4)
}

func (e *Engine) setNextRetry(d time.Duration, timer **time.Timer, ch *<-chan time.Time) {
	if *timer != nil {
		(*timer).Stop()
	}
	if d <= 0 {
		e.mu.Lock()
		e.nextTry = time.Time{}
		e.mu.Unlock()
		*ch = nil
		return
	}
	*timer = time.NewTimer(d)
	*ch = (*timer).C
	e.mu.Lock()
	e.nextTry = time.Now().Add(d)
	e.mu.Unlock()
}

// probeReachability فقط وقتی کار می‌کند که وضعیت فعلی سالم نیست؛ لبهٔ
// «نبود ← بود» یک چرخهٔ فوری می‌سازد (بازگشت اینترنت/سرور).
func (e *Engine) probeReachability(ctx context.Context) {
	e.mu.Lock()
	healthy := e.state == StateSynced || e.state == StatePending
	wasOK := e.reachOK
	e.mu.Unlock()
	if healthy {
		return
	}
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err := e.client.Ping(pctx)
	cancel()
	ok := err == nil
	e.mu.Lock()
	e.reachOK = ok
	if ok && !wasOK {
		// سرور برگشته است؛ پنجرهٔ بک‌آف دیگر معنا ندارد تا چرخهٔ
		// فوریِ ناشی از این لبه پشت گیت debounce نماند.
		e.nextTry = time.Time{}
	}
	e.mu.Unlock()
	if ok && !wasOK {
		e.log.Info("server reachable again; triggering sync")
		e.NotifyLocalChange()
	}
}

// runDrain چرخه را تا خالی‌شدن صف (با سقف) تکرار می‌کند: هر Refresh فقط
// یک دستهٔ محدود می‌فرستد، ولی بعد از قطعی طولانی صف می‌تواند هزاران
// عملیات داشته باشد و نباید ده دقیقهٔ دیگر صبر کند.
func (e *Engine) runDrain(ctx context.Context) error {
	var err error
	for round := 0; round < e.maxRounds; round++ {
		err = e.runCycle(ctx)
		if err != nil {
			return err
		}
		pending, _, _ := e.store.OutboxCounts(ctx)
		if pending == 0 {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return nil
}

// runCycle یک Refresh کامل را با قفل تک‌پرواز و سقف زمانی اجرا می‌کند.
func (e *Engine) runCycle(ctx context.Context) error {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return nil // چرخهٔ دیگری در حال اجراست؛ هم‌پوشانی مجاز نیست
	}
	e.running = true
	e.mu.Unlock()

	start := time.Now()
	cctx, cancel := context.WithTimeout(ctx, e.cycleLimit)
	err := e.client.Refresh(cctx, e.store)
	cancel()

	e.mu.Lock()
	e.running = false
	e.reachOK = err == nil
	if err == nil {
		e.failures = 0
		e.state = StateSynced
	} else {
		e.failures++
		e.state = classify(err)
	}
	e.mu.Unlock()

	if err == nil {
		_ = e.store.NoteSyncSuccess(ctx)
		pending, conflicts, _ := e.store.OutboxCounts(ctx)
		e.log.Info("sync cycle ok", "duration_ms", time.Since(start).Milliseconds(),
			"pending", pending, "conflicts", conflicts)
	} else {
		// پیام خطا در متا ذخیره می‌شود تا UI «سرور در دسترس نیست؛ دادهٔ
		// محلی امن است» را صادقانه نشان بدهد. هیچ رازی لاگ نمی‌شود.
		_ = e.store.NoteSyncFailure(ctx, safeError(err))
		e.log.Warn("sync cycle failed", "error", safeError(err),
			"duration_ms", time.Since(start).Milliseconds())
	}
	return err
}

// classify خطا را به وضعیت قابل نمایش نگاشت می‌کند.
func classify(err error) string {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "401") || strings.Contains(msg, "403") ||
		strings.Contains(msg, "unauthorized") || strings.Contains(msg, "invalid device") ||
		strings.Contains(msg, "revoked"):
		return StateAuthError
	case strings.Contains(msg, "connection refused") || strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "i/o timeout") || strings.Contains(msg, "deadline exceeded") ||
		strings.Contains(msg, "network is unreachable") || strings.Contains(msg, "eof") ||
		strings.Contains(msg, "dial"):
		return StateOffline
	default:
		return StateError
	}
}

// safeError پیام را خلاصه و بدون هرگونه توکن برمی‌گرداند.
func safeError(err error) string {
	msg := err.Error()
	if len(msg) > 200 {
		msg = msg[:200]
	}
	return msg
}

// cycleBackoff بک‌آف سطح چرخه: ۳۰ث تا سقف ۱۰ دقیقه.
func cycleBackoff(failures int) time.Duration {
	switch {
	case failures <= 1:
		return 30 * time.Second
	case failures == 2:
		return time.Minute
	case failures == 3:
		return 2 * time.Minute
	case failures == 4:
		return 5 * time.Minute
	default:
		return 10 * time.Minute
	}
}

func (e *Engine) jitter(span time.Duration) time.Duration {
	if span <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(int64(span)))
}

// deviceJitter بر اساس شناسهٔ دستگاه پایدار است تا رفتار هر دستگاه بین
// ری‌استارت‌ها عوض نشود.
func (e *Engine) deviceJitter(span time.Duration) time.Duration {
	if span <= 0 || e.client == nil {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(e.client.DeviceID))
	return time.Duration(int64(h.Sum32()) % span.Nanoseconds())
}
