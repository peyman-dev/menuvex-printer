// Package store owns durable, device-scoped offline state in pure Go (zero CGO).
// Every write executes inside an ACID mutex-guarded transaction with atomic
// write-ahead temp-file fsync + rename so crashes or validation errors never
// leave partial state on disk. Works identically across Windows (.exe), macOS,
// and Linux without requiring a C compiler.
package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	_ "time/tzdata" // منطقه زمانی تهران داخل باینری باشد (بدون tzdata سیستم)
)

type Store struct {
	mu     sync.Mutex
	path   string
	closed bool
	state  persistedDB
	// notify بعد از هر تراکنشی که عملیات سینک تازه‌ای صف کرده صدا زده
	// می‌شود تا موتور همگام‌سازی بدون بلاک‌کردن درخواست HTMX بیدار شود.
	notify func()
}

type persistedDB struct {
	Meta     map[string]string       `json:"meta"`
	Entities map[string]storedEntity `json:"entities"`
	Outbox   []storedOperation       `json:"outbox"`
	// License stores only verified, non-secret metadata. The encrypted age
	// ciphertext lives in a separate 0600 file and device credentials stay in
	// the OS keyring; neither a refresh token nor an age identity is persisted
	// in this JSON database.
	License *LicenseMetadata `json:"license,omitempty"`
	// LegacyStockCounts فقط برای سازگاری با فایل‌های محلی قدیمی نگه داشته می‌شود؛
	// صفحه و مسیر «برگه شمارش» حذف شده و این داده دیگر از هیچ رابطی خوانده یا نوشته نمی‌شود.
	LegacyStockCounts      []LegacyStockCount `json:"drafts"`
	NextOutboxSeq          int64              `json:"next_outbox_seq"`
	NextLegacyStockCountID int64              `json:"next_draft_id"`
	Connection             *ConnectionConfig  `json:"connection,omitempty"`
}

type storedEntity struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Version int64           `json:"version"`
	Deleted bool            `json:"deleted"`
	Payload json.RawMessage `json:"payload"`
	// Origin مشخص می‌کند رکورد از کجا آمده: "local" یعنی روی همین دستگاه
	// ساخته/ویرایش شده و ممکن است هنوز به سرور نرسیده باشد؛ "server" یعنی
	// از اسنپ‌شات یا pull سرور آمده. فایل‌های قدیمی مقدار "" دارند که برای
	// امنیت داده «محلی» فرض می‌شود و هرگز در Bootstrap حذف نمی‌شود.
	Origin string `json:"origin,omitempty"`
}

type storedOperation struct {
	ID          string          `json:"operation_id"`
	Sequence    int64           `json:"client_sequence"`
	EntityType  string          `json:"entity_type"`
	EntityID    string          `json:"entity_id"`
	Action      string          `json:"operation"`
	BaseVersion *int64          `json:"base_version,omitempty"`
	DependsOn   []string        `json:"depends_on,omitempty"`
	Payload     json.RawMessage `json:"payload"`
	CreatedAt   string          `json:"client_created_at"`
	State       string          `json:"state"`
	Result      json.RawMessage `json:"result"`
	// متادیتای تلاش مجدد: عملیات ناموفق هرگز دور ریخته نمی‌شود؛ با بک‌آف
	// پلکانی دوباره فرستاده می‌شود و دلیل آخرین خطا قابل مشاهده است.
	Attempts    int    `json:"attempts,omitempty"`
	NextRetryAt string `json:"next_retry_at,omitempty"`
	LastError   string `json:"last_error,omitempty"`
}

type ConnectionConfig struct {
	APIURL       string `json:"api_url"`
	DeviceID     string `json:"device_id"`
	DeviceToken  string `json:"device_token"`
	DeviceName   string `json:"device_name,omitempty"`
	CafeName     string `json:"cafe_name,omitempty"`
	AccountPhone string `json:"account_phone,omitempty"`
	UpdatedAt    string `json:"updated_at,omitempty"`
}

// LicenseMetadata is the durable, non-secret projection of the last license
// file that passed age decryption and JWS verification. It deliberately does
// not contain claims, customer_phone, activation codes, refresh tokens, or the
// age private identity. CiphertextFile is a file name relative to the data
// directory, never an arbitrary path supplied by a server.
type LicenseMetadata struct {
	CiphertextFile           string `json:"ciphertext_file"`
	FileFormat               string `json:"file_format"`
	SHA256                   string `json:"sha256"`
	LicenseID                string `json:"license_id"`
	DeviceID                 string `json:"device_id"`
	DeviceFingerprint        string `json:"device_public_key_fingerprint"`
	LicenseVersion           int64  `json:"license_version"`
	KeyID                    string `json:"kid"`
	Issuer                   string `json:"issuer"`
	Audience                 string `json:"audience"`
	ProductType              string `json:"product_type"`
	CafeName                string `json:"cafe_name,omitempty"`
	PlanTitle               string `json:"plan_title,omitempty"`
	PlanCode                string `json:"plan_code,omitempty"`
	ValidFrom               string `json:"valid_from"`
	ExpiresAt               string `json:"expires_at"`
	Status                  string `json:"status"`
	ServerTime              string `json:"server_time,omitempty"`
	LastSuccessfulValidation string `json:"last_successful_validation,omitempty"`
	LastSeenLocalUTC        string `json:"last_seen_local_utc,omitempty"`
}

type Entity struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Version int64           `json:"version"`
	Payload json.RawMessage `json:"payload"`
}

type Change struct {
	EntityType string          `json:"entity_type"`
	EntityID   string          `json:"entity_id"`
	Version    int64           `json:"entity_version"`
	Tombstone  bool            `json:"tombstone"`
	Operation  string          `json:"operation"`
	Payload    json.RawMessage `json:"payload"`
}

// LegacyStockCount یادداشت شمارش قدیمیِ جدا از دفتر موجودی است. نوع آن فقط
// برای خواندن و پشتیبان‌گیری از داده‌های قبلی دستگاه باقی مانده است.
type LegacyStockCount struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Quantity  string `json:"quantity"`
	Unit      string `json:"unit"`
	Note      string `json:"note"`
	CreatedAt string `json:"created_at"`
}

type OrderLineView struct {
	ID          string `json:"id,omitempty"`
	ProductID   string `json:"product_id"`
	ProductName string `json:"product_name"`
	Quantity    int    `json:"quantity"`
	UnitPrice   string `json:"unit_price"`
	LineTotal   string `json:"line_total"`
	Note        string `json:"note,omitempty"`
}

type SummaryStats struct {
	CategoriesCount   int
	ProductsCount     int
	IngredientsCount  int
	LowStockCount     int
	OrdersCount       int
	OpenOrdersCount   int
	RecipesCount      int
	BannersCount      int
	TablesCount       int
	CustomersCount    int
	DebtorsCount      int
	LegacyStockCount  int
	OutboxQueuedCount int
	OutboxBlocked     int
	OutboxApplied     int
}

func newEmptyDB() persistedDB {
	return persistedDB{
		Meta:              map[string]string{},
		Entities:          map[string]storedEntity{},
		Outbox:            []storedOperation{},
		LegacyStockCounts: []LegacyStockCount{},
	}
}

func cloneDB(src persistedDB) persistedDB {
	dst := persistedDB{
		Meta:                   make(map[string]string, len(src.Meta)),
		Entities:               make(map[string]storedEntity, len(src.Entities)),
		Outbox:                 make([]storedOperation, len(src.Outbox)),
		LegacyStockCounts:      make([]LegacyStockCount, len(src.LegacyStockCounts)),
		NextOutboxSeq:          src.NextOutboxSeq,
		NextLegacyStockCountID: src.NextLegacyStockCountID,
	}
	if src.License != nil {
		license := *src.License
		dst.License = &license
	}
	for k, v := range src.Meta {
		dst.Meta[k] = v
	}
	for k, v := range src.Entities {
		raw := make(json.RawMessage, len(v.Payload))
		copy(raw, v.Payload)
		v.Payload = raw
		dst.Entities[k] = v
	}
	for i, op := range src.Outbox {
		raw := make(json.RawMessage, len(op.Payload))
		copy(raw, op.Payload)
		op.Payload = raw
		res := make(json.RawMessage, len(op.Result))
		copy(res, op.Result)
		op.Result = res
		if op.BaseVersion != nil {
			bv := *op.BaseVersion
			op.BaseVersion = &bv
		}
		if len(op.DependsOn) > 0 {
			deps := make([]string, len(op.DependsOn))
			copy(deps, op.DependsOn)
			op.DependsOn = deps
		}
		dst.Outbox[i] = op
	}
	copy(dst.LegacyStockCounts, src.LegacyStockCounts)
	if src.Connection != nil {
		c := *src.Connection
		dst.Connection = &c
	}
	return dst
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	f.Close()

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// بازیابی از قطع برق وسط جایگزینی فایل: اگر فایل اصلی خالی یا خراب
	// است ولی نسخهٔ پشتیبان آخرین تراکنش سالم مانده، همان بارگذاری می‌شود
	// تا هیچ سفارش یا عملیات صف‌شده‌ای به‌خاطر کرش از دست نرود.
	if trimmed := bytes.TrimSpace(raw); len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(trimmed) {
		if bak, bErr := os.ReadFile(path + ".bak"); bErr == nil {
			bt := bytes.TrimSpace(bak)
			if len(bt) > 0 && bt[0] == '{' && json.Valid(bt) {
				raw = bak
			}
		}
	}
	st := newEmptyDB()
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		if err := json.Unmarshal(trimmed, &st); err != nil {
			return nil, fmt.Errorf("corrupt local store: %w", err)
		}
		if st.Meta == nil {
			st.Meta = map[string]string{}
		}
		if st.Entities == nil {
			st.Entities = map[string]storedEntity{}
		}
		if st.Outbox == nil {
			st.Outbox = []storedOperation{}
		}
		if st.LegacyStockCounts == nil {
			st.LegacyStockCounts = []LegacyStockCount{}
		}
	}
	if st.Meta == nil {
		st.Meta = map[string]string{}
	}
	if st.Meta["schema_version"] == "" {
		// نسخهٔ ۱: فرمت فعلی فایل. مهاجرت‌های آینده باید افزایشی باشند و
		// هرگز سفارش‌ها یا عملیات صف‌شده را پاک نکنند.
		st.Meta["schema_version"] = "1"
	}
	return &Store{path: path, state: st}, nil
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *Store) withTx(ctx context.Context, fn func(st *persistedDB) error) error {
	return s.withTxMode(ctx, true, fn)
}

// withTxServer برای اعمال دادهٔ آمده از سرور (Bootstrap/Pull) است؛ رکوردهای
// این مسیر منشأ "server" می‌گیرند و در Bootstrap بعدی قابل جایگزینی‌اند.
func (s *Store) withTxServer(ctx context.Context, fn func(st *persistedDB) error) error {
	return s.withTxMode(ctx, false, fn)
}

func (s *Store) withTxMode(ctx context.Context, localWrite bool, fn func(st *persistedDB) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("store is closed")
	}
	tx := cloneDB(s.state)
	prevOutbox := len(s.state.Outbox)
	if err := fn(&tx); err != nil {
		return err
	}
	if localWrite {
		// هر رکوردی که در این تراکنش محلی ساخته یا تغییر کرده «محلی» است:
		// Bootstrap و Pull سرور هرگز نباید آن را بی‌صدا جایگزین کند.
		for key, e := range tx.Entities {
			old, ok := s.state.Entities[key]
			if !ok || old.Version != e.Version || old.Deleted != e.Deleted || !bytes.Equal(old.Payload, e.Payload) {
				e.Origin = "local"
				tx.Entities[key] = e
			}
		}
	}
	encoded, err := json.Marshal(tx)
	if err != nil {
		return err
	}
	tmpPath := s.path + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(encoded); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return err
	}
	if err = f.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err = os.Rename(tmpPath, s.path); err != nil {
		// ویندوز گاهی rename روی فایل موجود را رد می‌کند (آنتی‌ویروس/هندل
		// باز). به‌جای بازنویسی غیراتمیک با WriteFile — که قطع برق وسط آن
		// کل دیتابیس را خراب می‌کرد — اول فایل سالم فعلی کنار گذاشته
		// می‌شود (.bak)، بعد فایل جدید جایش می‌نشیند؛ اگر قدم دوم شکست
		// خورد بک‌آپ برگردانده می‌شود و Open هم از .bak بازیابی می‌کند.
		bakPath := s.path + ".bak"
		_ = os.Remove(bakPath)
		if rErr := os.Rename(s.path, bakPath); rErr != nil {
			os.Remove(tmpPath)
			return err
		}
		if rErr := os.Rename(tmpPath, s.path); rErr != nil {
			_ = os.Rename(bakPath, s.path)
			os.Remove(tmpPath)
			return err
		}
	}
	// fsync پوشهٔ والد تا خودِ rename هم در برابر قطع برق بادوام باشد
	// (روی ویندوز ممکن است پشتیبانی نشود؛ آنجا خطا نادیده گرفته می‌شود).
	if dir, dErr := os.Open(filepath.Dir(s.path)); dErr == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	s.state = tx
	if s.notify != nil && len(s.state.Outbox) > prevOutbox {
		// کپی از هوک زیر قفل گرفته می‌شود ولی خود صدا زدن بعد از آزاد شدن
		// قفل انجام می‌شود تا موتور سینک هرگز مسیر نوشتن را بلاک نکند.
		notify := s.notify
		defer func() { go notify() }()
	}
	return nil
}

// SetSyncNotify هوک بیدارباش موتور همگام‌سازی را ثبت می‌کند؛ nil یعنی خاموش.
func (s *Store) SetSyncNotify(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notify = fn
}

func (s *Store) Cursor(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.Meta["cursor"], nil
}

// SyncMeta آخرین وضعیت شناخته‌شدهٔ همگام‌سازی است که در همان فایل داده
// نگه داشته می‌شود تا بعد از ری‌استارت هم «آخرین سینک موفق» گم نشود.
type SyncMeta struct {
	LastSuccessAt string
	LastError     string
	LastErrorAt   string
	Initialized   bool // حداقل یک Bootstrap موفق انجام شده است
}

// NoteSyncSuccess پایان موفق یک چرخهٔ کامل (push+pull) را ثبت می‌کند.
func (s *Store) NoteSyncSuccess(ctx context.Context) error {
	return s.withTxServer(ctx, func(st *persistedDB) error {
		st.Meta["last_sync_success_at"] = time.Now().UTC().Format(time.RFC3339)
		st.Meta["last_sync_error"] = ""
		st.Meta["sync_initialized"] = "1"
		return nil
	})
}

// NoteSyncFailure خطای چرخه را بدون دست‌کاری هیچ دادهٔ کسب‌وکاری ثبت می‌کند.
func (s *Store) NoteSyncFailure(ctx context.Context, errText string) error {
	return s.withTxServer(ctx, func(st *persistedDB) error {
		st.Meta["last_sync_error"] = errText
		st.Meta["last_sync_error_at"] = time.Now().UTC().Format(time.RFC3339)
		return nil
	})
}

func (s *Store) SyncMeta(ctx context.Context) SyncMeta {
	if ctx.Err() != nil {
		return SyncMeta{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return SyncMeta{
		LastSuccessAt: s.state.Meta["last_sync_success_at"],
		LastError:     s.state.Meta["last_sync_error"],
		LastErrorAt:   s.state.Meta["last_sync_error_at"],
		Initialized:   s.state.Meta["sync_initialized"] == "1" || s.state.Meta["cursor"] != "",
	}
}

// OutboxCounts شمارنده‌های صف برای نمایش وضعیت سینک در رابط کاربری است.
func (s *Store) OutboxCounts(ctx context.Context) (pending, conflicts, rejected int) {
	if ctx.Err() != nil {
		return 0, 0, 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, op := range s.state.Outbox {
		switch op.State {
		case "queued", "retry", "not_processed":
			pending++
		case "conflict":
			conflicts++
		case "rejected", "skipped":
			rejected++
		}
	}
	return pending, conflicts, rejected
}

func (s *Store) Bind(ctx context.Context, scope string) error {
	return s.withTx(ctx, func(st *persistedDB) error {
		old, exists := st.Meta["scope"]
		if exists && old != "" && old != scope {
			return errors.New("local database belongs to a different server/device; use a separate data directory")
		}
		st.Meta["scope"] = scope
		return nil
	})
}

func (s *Store) SaveConnection(ctx context.Context, cfg ConnectionConfig) error {
	scope := cfg.APIURL + "|" + cfg.DeviceID
	return s.withTx(ctx, func(st *persistedDB) error {
		old, exists := st.Meta["scope"]
		if exists && old != "" && old != scope {
			return errors.New("این پایگاه داده محلی به سرور یا دستگاه دیگری متصل بوده است؛ برای تغییر حساب ابتدا بازنشانی محلی انجام دهید")
		}
		st.Meta["scope"] = scope
		cfg.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		st.Connection = &cfg
		return nil
	})
}

func (s *Store) LoadConnection(ctx context.Context) *ConnectionConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Connection == nil {
		return nil
	}
	c := *s.state.Connection
	return &c
}

// LoadLicenseMetadata returns a copy so callers cannot mutate the in-memory
// transaction state without going through the atomic store writer.
func (s *Store) LoadLicenseMetadata(ctx context.Context) *LicenseMetadata {
	if err := ctx.Err(); err != nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.License == nil {
		return nil
	}
	copy := *s.state.License
	return &copy
}

// SaveLicenseMetadata is an additive migration-safe write. Existing stores
// without a license field unmarshal as nil and continue to work unchanged.
func (s *Store) SaveLicenseMetadata(ctx context.Context, metadata LicenseMetadata) error {
	if strings.TrimSpace(metadata.CiphertextFile) == "" || strings.TrimSpace(metadata.LicenseID) == "" || strings.TrimSpace(metadata.DeviceID) == "" {
		return errors.New("license metadata is incomplete")
	}
	return s.withTx(ctx, func(st *persistedDB) error {
		copy := metadata
		st.License = &copy
		return nil
	})
}

func (s *Store) ClearLicenseMetadata(ctx context.Context) error {
	return s.withTx(ctx, func(st *persistedDB) error {
		st.License = nil
		return nil
	})
}

func (s *Store) ResetAll(ctx context.Context) error {
	return s.withTx(ctx, func(st *persistedDB) error {
		*st = newEmptyDB()
		return nil
	})
}

func entityKey(kind, id string) string {
	return kind + "\x00" + id
}

func (s *Store) Entities(ctx context.Context, kind string) ([]Entity, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Entity{}
	for _, e := range s.state.Entities {
		if e.Deleted {
			continue
		}
		if e.Type == kind || (kind == "order" && e.Type == "order_status") {
			raw := make(json.RawMessage, len(e.Payload))
			copy(raw, e.Payload)
			out = append(out, Entity{
				ID:      e.ID,
				Type:    e.Type,
				Version: e.Version,
				Payload: raw,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		return out[i].Type < out[j].Type
	})
	if len(out) > 500 {
		out = out[:500]
	}
	return out, nil
}

func (s *Store) Entity(ctx context.Context, kind, id string) (Entity, error) {
	if err := ctx.Err(); err != nil {
		return Entity{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.state.Entities[entityKey(kind, id)]
	if !ok || e.Deleted {
		return Entity{}, errors.New("record not found")
	}
	raw := make(json.RawMessage, len(e.Payload))
	copy(raw, e.Payload)
	return Entity{
		ID:      e.ID,
		Type:    e.Type,
		Version: e.Version,
		Payload: raw,
	}, nil
}

func applyChange(st *persistedDB, c Change) error {
	if c.EntityID == "" || c.EntityType == "" {
		return errors.New("change missing entity identity")
	}
	if len(c.Payload) == 0 || string(c.Payload) == "null" {
		c.Payload = json.RawMessage(`{}`)
	}
	if !json.Valid(c.Payload) {
		return errors.New("invalid change JSON")
	}
	deleted := c.Tombstone || c.Operation == "delete"
	key := entityKey(c.EntityType, c.EntityID)
	existing, exists := st.Entities[key]
	// Keep tombstone versions: deleting the row entirely permits stale resurrection.
	if !exists || c.Version > existing.Version || (c.Version == 0 && existing.Version == 0) {
		raw := make(json.RawMessage, len(c.Payload))
		copy(raw, c.Payload)
		st.Entities[key] = storedEntity{
			ID:      c.EntityID,
			Type:    c.EntityType,
			Version: c.Version,
			Deleted: deleted,
			Payload: raw,
			Origin:  "server",
		}
	}
	return nil
}

func (s *Store) ApplyPage(ctx context.Context, changes []Change, cursor string) error {
	if cursor == "" {
		return errors.New("server returned empty cursor")
	}
	return s.withTxServer(ctx, func(st *persistedDB) error {
		for _, c := range changes {
			if err := applyChange(st, c); err != nil {
				return err
			}
		}
		st.Meta["cursor"] = cursor
		return nil
	})
}

var bootstrapSectionTypes = map[string]string{
	"products":          "product",
	"categories":        "category",
	"ingredients":       "ingredient",
	"banners":           "banner",
	"recipes":           "recipe",
	"orders":            "order",
	"inventory_summary": "inventory_summary",
	"waste_events":      "waste_event",
	"waste_items":       "waste_item",
	"tables":            "table",
	"customers":         "customer",
	"debtors":           "debtor",
	"printers":          "printer",
	"cafe_profile":      "cafe_profile",
}

// Bootstrap replaces server-owned entities only. Legacy local stock-count notes and outbox always survive.
func (s *Store) Bootstrap(ctx context.Context, sections map[string][]json.RawMessage, cursor string) error {
	if cursor == "" {
		return errors.New("bootstrap missing snapshot cursor")
	}
	return s.withTxServer(ctx, func(st *persistedDB) error {
		// فقط رکوردهای با منشأ سرور با اسنپ‌شات جایگزین می‌شوند. رکوردهای
		// محلی (سفارش‌ها، پرداخت‌ها و ویرایش‌های همگام‌نشدهٔ همین دستگاه)
		// دست‌نخورده می‌مانند؛ پاک‌کردن آن‌ها در Bootstrap یعنی از دست
		// رفتن بی‌صدای کسب‌وکار هنگام انقضای کرسر یا اتصال مجدد.
		kept := make(map[string]storedEntity, len(st.Entities))
		for key, e := range st.Entities {
			if e.Origin != "server" {
				kept[key] = e
			}
		}
		st.Entities = kept
		for section, rows := range sections {
			kind, ok := bootstrapSectionTypes[section]
			if !ok {
				continue
			}
			for _, raw := range rows {
				var v struct {
					ID           string `json:"id"`
					IngredientID string `json:"ingredient_id"`
					Version      int64  `json:"version"`
				}
				if err := json.Unmarshal(raw, &v); err != nil {
					return err
				}
				id := v.ID
				if id == "" && kind == "inventory_summary" {
					id = v.IngredientID
				}
				if err := applyChange(st, Change{EntityType: kind, EntityID: id, Version: v.Version, Payload: raw}); err != nil {
					return fmt.Errorf("%s: %w", section, err)
				}
			}
		}
		st.Meta["cursor"] = cursor
		return nil
	})
}

// AddLegacyStockCount فقط در تست‌های سازگاری داده استفاده می‌شود تا ثابت شود
// یادداشت‌های شمارش قدیمی هنگام Bootstrap یا بازکردن فایل محلی از بین نمی‌روند.
// هیچ مسیر اجرایی برنامه دیگر این تابع را صدا نمی‌زند.
func (s *Store) AddLegacyStockCount(ctx context.Context, d LegacyStockCount) error {
	return s.withTx(ctx, func(st *persistedDB) error {
		st.NextLegacyStockCountID++
		d.ID = st.NextLegacyStockCountID
		d.CreatedAt = time.Now().UTC().Format(time.RFC3339)
		st.LegacyStockCounts = append(st.LegacyStockCounts, d)
		return nil
	})
}

// LegacyStockCounts داده‌های قدیمی را بدون تغییر برمی‌گرداند تا در خروجی
// پشتیبان باقی بمانند؛ موجودی واقعی فقط از دفتر گردش انبار خوانده می‌شود.
func (s *Store) LegacyStockCounts(ctx context.Context) ([]LegacyStockCount, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]LegacyStockCount, len(s.state.LegacyStockCounts))
	copy(out, s.state.LegacyStockCounts)
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID > out[j].ID
	})
	if len(out) > 500 {
		out = out[:500]
	}
	return out, nil
}

func (s *Store) Stats(ctx context.Context) SummaryStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	var st SummaryStats
	for _, e := range s.state.Entities {
		if e.Deleted {
			continue
		}
		switch e.Type {
		case "category":
			st.CategoriesCount++
		case "product":
			st.ProductsCount++
		case "ingredient":
			st.IngredientsCount++
			ent := Entity{ID: e.ID, Type: e.Type, Version: e.Version, Payload: e.Payload}
			if ent.NeedsReorder() {
				st.LowStockCount++
			}
		case "order":
			st.OrdersCount++
			ent := Entity{ID: e.ID, Type: e.Type, Version: e.Version, Payload: e.Payload}
			status := strings.ToLower(ent.Field("status"))
			if status == "" || status == "pending" || status == "accepted" || status == "preparing" || status == "ready" {
				st.OpenOrdersCount++
			}
		case "recipe":
			st.RecipesCount++
		case "banner":
			st.BannersCount++
		case "table":
			st.TablesCount++
		case "customer":
			st.CustomersCount++
		case "debtor":
			st.DebtorsCount++
		}
	}
	st.LegacyStockCount = len(s.state.LegacyStockCounts)
	for _, op := range s.state.Outbox {
		switch op.State {
		case "queued", "retry", "not_processed":
			st.OutboxQueuedCount++
		case "conflict", "rejected", "skipped":
			st.OutboxBlocked++
		case "applied":
			st.OutboxApplied++
		}
	}
	return st
}

func (s *Store) ExportBackup(ctx context.Context) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	export := cloneDB(s.state)
	// Never export secret device tokens in backup files.
	if export.Connection != nil {
		export.Connection.DeviceToken = ""
	}
	return json.MarshalIndent(export, "", "  ")
}

// Label is presentation only: no numeric decoding or tenant-sensitive projection.
func (e Entity) Label() string {
	var m map[string]json.RawMessage
	if json.Unmarshal(e.Payload, &m) == nil {
		for _, key := range []string{"title", "name", "full_name", "order_number", "number"} {
			var value string
			if json.Unmarshal(m[key], &value) == nil && value != "" {
				if key == "number" && e.Type == "table" {
					return "میز شماره " + value
				}
				return value
			}
		}
	}
	return e.ID
}

// Field extracts a scalar field from Payload as a clean string for UI display.
func (e Entity) Field(key string) string {
	var m map[string]json.RawMessage
	if json.Unmarshal(e.Payload, &m) != nil {
		return ""
	}
	raw, ok := m[key]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return strings.Trim(string(raw), `"`)
}

// PriceIRR returns the primary monetary value in IRR (or IRT for debtors).
func (e Entity) PriceIRR() string {
	for _, k := range []string{"price", "total_irr", "total", "remaining", "average_cost"} {
		if v := e.Field(k); v != "" {
			return v
		}
	}
	return ""
}

// PriceToman formats an IRR decimal string as Iranian Toman (1 Toman = 10 IRR).
func (e Entity) PriceToman() string {
	raw := e.PriceIRR()
	if raw == "" {
		return ""
	}
	if e.Type == "debtor" && strings.EqualFold(e.Field("currency"), "IRT") {
		return FormatPersianNumber(raw) + " تومان"
	}
	return FormatIRRToToman(raw)
}

func (e Entity) PriceRialFormatted() string {
	raw := e.PriceIRR()
	if raw == "" {
		return ""
	}
	if e.Type == "debtor" && strings.EqualFold(e.Field("currency"), "IRT") {
		return FormatPersianNumber(raw) + " تومان"
	}
	return FormatPersianNumber(raw) + " ریال"
}

// SplitShareToman calculates each person's equal share in Toman for Iranian Split-Bill (دنگ).
func (e Entity) SplitShareToman(people int) string {
	if people <= 1 {
		return e.PriceToman()
	}
	raw := strings.TrimSpace(e.PriceIRR())
	if raw == "" {
		return "۰ تومان"
	}
	parts := strings.SplitN(raw, ".", 2)
	bi := new(big.Int)
	if _, ok := bi.SetString(parts[0], 10); !ok {
		return "۰ تومان"
	}
	shareIRR := new(big.Int).Div(bi, big.NewInt(int64(people)))
	return FormatIRRToToman(shareIRR.String())
}

func (e Entity) StatusLabel() string {
	switch e.Type {
	case "product":
		if e.Field("is_available") == "false" || strings.EqualFold(e.Field("status"), "inactive") {
			return "ناموجود"
		}
		return "موجود در منو"
	case "banner":
		if e.Field("is_active") == "false" {
			return "غیرفعال"
		}
		return "فعال"
	case "order", "order_status":
		// برچسب‌ها دقیقاً از STATUS_CONFIG نسخه وب گرفته شده‌اند.
		switch strings.ToLower(e.Field("status")) {
		case "completed":
			return "تکمیل شده"
		case "cancelled", "canceled":
			return "لغو شده"
		case "refunded":
			return "بازگشت وجه"
		case "preparing":
			return "در حال آماده‌سازی"
		case "ready":
			return "آماده تحویل"
		case "accepted":
			return "تأیید شده (صندوقدار)"
		default:
			return "در انتظار تأیید صندوقدار"
		}
	case "table":
		switch strings.ToLower(e.Field("status")) {
		case "occupied":
			return "اشغال (دارای مهمان)"
		case "reserved":
			return "رزرو شده"
		case "cleaning":
			return "در حال نظافت"
		case "disabled":
			return "غیرفعال"
		default:
			return "آزاد و آماده پذیرش"
		}
	case "debtor":
		switch strings.ToLower(e.Field("status")) {
		case "paid":
			return "تسویه شده"
		case "partial":
			return "پرداخت بخشی از بدهی"
		case "overdue":
			return "سررسید گذشته"
		default:
			return "بدهکار (باز)"
		}
	}
	if e.NeedsReorder() {
		return "نیاز به شارژ انبار"
	}
	return "فعال"
}

func (e Entity) StatusClass() string {
	switch e.Type {
	case "product":
		if e.Field("is_available") == "false" {
			return "status-warn"
		}
		return "status-ok"
	case "order", "order_status":
		switch strings.ToLower(e.Field("status")) {
		case "completed":
			return "status-ok"
		case "cancelled", "canceled":
			return "status-danger"
		default:
			return "status-warn"
		}
	case "table":
		switch strings.ToLower(e.Field("status")) {
		case "occupied", "reserved":
			return "status-warn"
		case "disabled":
			return "status-danger"
		default:
			return "status-ok"
		}
	}
	if e.NeedsReorder() {
		return "status-danger"
	}
	return "status-ok"
}

func (e Entity) UnitLabel() string {
	u := e.Field("base_unit")
	if u == "" {
		u = e.Field("unit")
	}
	return PersianUnitName(u)
}

func (e Entity) NeedsReorder() bool {
	if e.Type != "ingredient" {
		return false
	}
	cur, err1 := strconv.ParseFloat(e.Field("current_stock"), 64)
	if err1 != nil {
		return false
	}
	if rp, err := strconv.ParseFloat(e.Field("reorder_point"), 64); err == nil && rp > 0 && cur <= rp {
		return true
	}
	if ms, err := strconv.ParseFloat(e.Field("minimum_stock"), 64); err == nil && ms > 0 && cur <= ms {
		return true
	}
	return cur < 0
}

func (e Entity) OrderItems() []OrderLineView {
	var m struct {
		Items []struct {
			ID          string          `json:"id"`
			ProductID   string          `json:"product_id"`
			ProductName string          `json:"product_name_snapshot"`
			Title       string          `json:"title"`
			Quantity    int             `json:"quantity"`
			UnitPrice   json.RawMessage `json:"unit_price_irr"`
			Price       json.RawMessage `json:"unit_price"`
			LineTotal   json.RawMessage `json:"line_total_irr"`
			Total       json.RawMessage `json:"total_price"`
			Note        string          `json:"note"`
		} `json:"items"`
	}
	if json.Unmarshal(e.Payload, &m) != nil {
		return nil
	}
	out := make([]OrderLineView, 0, len(m.Items))
	for _, it := range m.Items {
		name := it.ProductName
		if name == "" {
			name = it.Title
		}
		if name == "" {
			name = it.ProductID
		}
		up := strings.Trim(string(it.UnitPrice), `"`)
		if up == "" || up == "null" {
			up = strings.Trim(string(it.Price), `"`)
		}
		lt := strings.Trim(string(it.LineTotal), `"`)
		if lt == "" || lt == "null" {
			lt = strings.Trim(string(it.Total), `"`)
		}
		out = append(out, OrderLineView{
			ID:          it.ID,
			ProductID:   it.ProductID,
			ProductName: name,
			Quantity:    it.Quantity,
			UnitPrice:   up,
			LineTotal:   lt,
			Note:        it.Note,
		})
	}
	return out
}

func PersianUnitName(u string) string {
	switch strings.ToLower(strings.TrimSpace(u)) {
	case "g":
		return "گرم (g)"
	case "kg":
		return "کیلوگرم (kg)"
	case "mg":
		return "میلی‌گرم (mg)"
	case "ml":
		return "میلی‌لیتر (ml)"
	case "cl":
		return "سانتی‌لیتر (cl)"
	case "l":
		return "لیتر (l)"
	case "pcs":
		return "عدد (pcs)"
	case "shot":
		return "شات اسپرسو (shot)"
	case "pack":
		return "بسته (pack)"
	case "portion":
		return "پرس / وعده (portion)"
	case "bottle":
		return "بطری (bottle)"
	case "box":
		return "جعبه / کارتن (box)"
	case "sachet":
		return "ساشه (sachet)"
	default:
		return u
	}
}

var persianDigitsReplacer = strings.NewReplacer(
	"0", "۰", "1", "۱", "2", "۲", "3", "۳", "4", "۴",
	"5", "۵", "6", "۶", "7", "۷", "8", "۸", "9", "۹",
)

func ToPersianDigits(s string) string {
	return persianDigitsReplacer.Replace(s)
}

func addThousandCommas(intPart string) string {
	neg := false
	if strings.HasPrefix(intPart, "-") {
		neg = true
		intPart = intPart[1:]
	}
	n := len(intPart)
	if n <= 3 {
		if neg {
			return "-" + intPart
		}
		return intPart
	}
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	rem := n % 3
	if rem > 0 {
		b.WriteString(intPart[:rem])
		if n > rem {
			b.WriteString("٬")
		}
	}
	for i := rem; i < n; i += 3 {
		b.WriteString(intPart[i : i+3])
		if i+3 < n {
			b.WriteString("٬")
		}
	}
	return b.String()
}

func FormatPersianNumber(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "۰"
	}
	parts := strings.SplitN(raw, ".", 2)
	intPart := addThousandCommas(parts[0])
	if len(parts) == 2 {
		frac := strings.TrimRight(parts[1], "0")
		if frac != "" {
			return ToPersianDigits(intPart + "/" + frac)
		}
	}
	return ToPersianDigits(intPart)
}

func FormatIRRToToman(rawIRR string) string {
	rawIRR = strings.TrimSpace(rawIRR)
	if rawIRR == "" {
		return "۰ تومان"
	}
	parts := strings.SplitN(rawIRR, ".", 2)
	bi := new(big.Int)
	if _, ok := bi.SetString(parts[0], 10); !ok {
		return ToPersianDigits(rawIRR) + " ریال"
	}
	ten := big.NewInt(10)
	toman, rem := new(big.Int).QuoRem(bi, ten, new(big.Int))
	formatted := FormatPersianNumber(toman.String())
	if rem.Cmp(big.NewInt(0)) != 0 {
		return formatted + " تومان (" + FormatPersianNumber(parts[0]) + " ریال)"
	}
	return formatted + " تومان"
}
