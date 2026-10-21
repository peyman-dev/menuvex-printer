package desktop

// License support for the standalone Go desktop application.
//
// The server contract is deliberately kept at this boundary: activation and
// refresh speak the documented /api/v1/offline-licenses endpoints, while the
// rest of the application only sees a verified LicenseState. No local caller
// is allowed to consume response JSON or decrypted claims directly.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"filippo.io/age"
	"github.com/zalando/go-keyring"

	"github.com/sinaqaffari/coffish/desktop/internal/store"
)

const (
	licenseDefaultAPIBase = "https://menuvex.ir/api/v1"
	licenseIssuer         = "https://menuvex.ir"
	licenseAudience       = "menuvex-offline-desktop"
	licenseFileFormat     = "age+compact-jws-ed25519-v1"
	licenseSchemaVersion  = 1
	licenseServiceName    = "com.menuvex.desktop.offline-license.v1"
	licenseMaxFileBytes   = 1 << 20
	licenseMaxCodeBytes    = 512
	licenseClockTolerance  = 5 * time.Minute
	licenseRefreshInterval = 10 * time.Minute
)

var errLicenseNotFound = keyring.ErrNotFound

// SecureStringStore is the small OS credential-store surface used by the
// license manager. The production implementation is go-keyring; tests inject
// an in-memory implementation. It is intentionally not a file-backed fallback.
type SecureStringStore interface {
	Get(service, user string) (string, error)
	Set(service, user, value string) error
	Delete(service, user string) error
}

type osSecureStringStore struct{}

func (osSecureStringStore) Get(service, user string) (string, error) {
	return keyring.Get(service, user)
}

func (osSecureStringStore) Set(service, user, value string) error {
	return keyring.Set(service, user, value)
}

func (osSecureStringStore) Delete(service, user string) error {
	return keyring.Delete(service, user)
}

// LicenseConfig contains runtime configuration only. In particular, public
// verification keys are supplied by deployment configuration and never guessed
// or replaced with a built-in test key.
type LicenseConfig struct {
	Enabled                bool
	APIBaseURL             string
	Issuer                 string
	Audience               string
	PublicKeys             map[string]ed25519.PublicKey
	DeviceLabel            string
	AppVersion             string
	ClockRollbackTolerance time.Duration
	RefreshInterval        time.Duration
	HTTPClient             *http.Client
	Now                    func() time.Time
	ConfigError            string
}

// LicenseState is the only license value exposed to the UI and local API.
// Valid/Expired/Reason are presentation gates and are intentionally omitted
// from the JSON summary where appropriate.
type LicenseState struct {
	Status                 string
	Valid                  bool   `json:"-"`
	Expired                bool   `json:"-"`
	Reason                 string `json:"-"`
	Holder                 string `json:"-"`
	Plan                   string `json:"-"`
	LicenseID              string `json:"license_id,omitempty"`
	CafeName               string `json:"cafe_name,omitempty"`
	PlanTitle              string `json:"plan_title,omitempty"`
	ValidFrom              string `json:"valid_from,omitempty"`
	ExpiresAt              string `json:"expires_at,omitempty"`
	DaysLeft               int    `json:"remaining_days"`
	LastValidatedAt        string `json:"last_validated_at,omitempty"`
	OnlineRefreshAvailable bool   `json:"online_refresh_available"`
	ActivationAvailable    bool   `json:"-"`
	DeviceID               string `json:"-"`
}

func (s LicenseState) CanMutate() bool {
	return s.Valid && s.Status == "active" && !s.Expired
}

func (s LicenseState) DisplayStatus() string {
	switch s.Status {
	case "active":
		return "فعال"
	case "expired":
		return "منقضی‌شده"
	case "revoked":
		return "لغوشده"
	case "suspended":
		return "معلق"
	case "clock_rollback":
		return "اختلال در ساعت دستگاه"
	case "not_yet_valid":
		return "هنوز معتبر نشده"
	case "secure_store_unavailable":
		return "فضای امن دستگاه در دسترس نیست"
	case "invalid":
		return "نامعتبر"
	default:
		return "فعال‌نشده"
	}
}

// licenseClaims is intentionally private. It is created only after the JWS
// signature has been verified and is never serialized into the local API.
type licenseClaims struct {
	SchemaVersion              int      `json:"schema_version"`
	Issuer                     string   `json:"iss"`
	Audience                  string   `json:"aud"`
	JTI                        string   `json:"jti"`
	LicenseID                 string   `json:"license_id"`
	CafeID                    string   `json:"cafe_id"`
	CafeName                  string   `json:"cafe_name"`
	CustomerPhone             string   `json:"customer_phone"`
	PlanCode                  string   `json:"plan_code"`
	PlanTitle                 string   `json:"plan_title"`
	ProductType               string   `json:"product_type"`
	Entitlements              []string `json:"entitlements"`
	DeviceID                  string   `json:"device_id"`
	DevicePublicKeyFingerprint string  `json:"device_public_key_fingerprint"`
	IssuedAt                  string   `json:"issued_at"`
	NotBefore                 string   `json:"not_before"`
	ExpiresAt                 string   `json:"expires_at"`
	LicenseVersion            int64    `json:"license_version"`
	Status                    string   `json:"status"`
}

type compactJWSHeader struct {
	Algorithm string `json:"alg"`
	KeyID     string `json:"kid"`
	Type      string `json:"typ,omitempty"`
}

type licenseAPIError struct {
	Code       string
	HTTPStatus int
	Retryable  bool
	Message    string
}

func (e *licenseAPIError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

type licenseActivationResponse struct {
	LicenseID       string `json:"license_id"`
	DeviceID        string `json:"device_id"`
	Status          string `json:"status"`
	ServerTime      string `json:"server_time"`
	LicenseVersion  int64  `json:"license_version"`
	FileFormat      string `json:"license_file_format"`
	FileName        string `json:"license_file_name"`
	FileSHA256      string `json:"license_file_sha256"`
	FileBase64      string `json:"license_file_base64"`
	RefreshToken    string `json:"refresh_token"`
	ExpiresAt       string `json:"expires_at"`
}

type licenseCurrentResponse struct {
	Status         string `json:"status"`
	ServerTime     string `json:"server_time"`
	LicenseVersion int64  `json:"license_version"`
	ExpiresAt      string `json:"expires_at"`
	FileFormat     string `json:"license_file_format"`
	FileSHA256     string `json:"license_file_sha256"`
	FileBase64     string `json:"license_file_base64"`
	RefreshToken   string `json:"refresh_token"`
}

type licenseEnvelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   json.RawMessage `json:"error"`
}

type LicenseManager struct {
	mu       sync.RWMutex
	refreshMu sync.Mutex
	store    *store.Store
	dataDir  string
	config   LicenseConfig
	keyring  SecureStringStore
	service  string
	user     string
	http     *http.Client
	now      func() time.Time
	state    LicenseState
	ready    bool
	pending  map[string]string
}

func defaultLicenseConfig() LicenseConfig {
	enabled := false
	switch strings.ToLower(strings.TrimSpace(os.Getenv("COFFISH_OFFLINE_LICENSES_ENABLED"))) {
	case "1", "true", "yes", "on":
		enabled = true
	}
	base := strings.TrimSpace(os.Getenv("COFFISH_LICENSE_API_BASE_URL"))
	if base == "" {
		base = strings.TrimSpace(os.Getenv("API_BASE_URL"))
	}
	if base == "" {
		base = licenseDefaultAPIBase
	}
	issuer := strings.TrimSpace(os.Getenv("COFFISH_LICENSE_ISSUER"))
	if issuer == "" {
		issuer = licenseIssuer
	}
	audience := strings.TrimSpace(os.Getenv("COFFISH_LICENSE_AUDIENCE"))
	if audience == "" {
		audience = licenseAudience
	}
	keys, keyErr := parsePublicKeys(os.Getenv("COFFISH_LICENSE_PUBLIC_KEYS"))
	refreshInterval := licenseRefreshInterval
	if rawInterval := strings.TrimSpace(os.Getenv("COFFISH_LICENSE_REFRESH_INTERVAL")); rawInterval != "" {
		if parsed, err := time.ParseDuration(rawInterval); err == nil && parsed > 0 && parsed <= 24*time.Hour {
			refreshInterval = parsed
		}
	}
	cfg := LicenseConfig{
		Enabled:                enabled,
		APIBaseURL:             base,
		Issuer:                 issuer,
		Audience:               audience,
		PublicKeys:             keys,
		DeviceLabel:            strings.TrimSpace(os.Getenv("COFFISH_DEVICE_LABEL")),
		AppVersion:             strings.TrimSpace(os.Getenv("COFFISH_APP_VERSION")),
		ClockRollbackTolerance: licenseClockTolerance,
		RefreshInterval:        refreshInterval,
		HTTPClient:             &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }},
		Now:                    time.Now,
	}
	if cfg.DeviceLabel == "" {
		cfg.DeviceLabel = "صندوق اصلی"
	}
	if cfg.AppVersion == "" {
		cfg.AppVersion = "unknown"
	}
	if keyErr != nil {
		cfg.ConfigError = keyErr.Error()
	}
	if _, err := normalizeLicenseBaseURL(cfg.APIBaseURL); err != nil && cfg.ConfigError == "" {
		cfg.ConfigError = err.Error()
	}
	return cfg
}

func newLicenseManager(dataDir string, st *store.Store, cfg LicenseConfig) *LicenseManager {
	if cfg.Issuer == "" {
		cfg.Issuer = licenseIssuer
	}
	if cfg.Audience == "" {
		cfg.Audience = licenseAudience
	}
	if cfg.ClockRollbackTolerance <= 0 {
		cfg.ClockRollbackTolerance = licenseClockTolerance
	}
	if cfg.RefreshInterval <= 0 {
		cfg.RefreshInterval = licenseRefreshInterval
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	absDir, err := filepath.Abs(dataDir)
	if err != nil {
		absDir = dataDir
	}
	hash := sha256.Sum256([]byte(absDir))
	return &LicenseManager{
		store:   st,
		dataDir: dataDir,
		config:  cfg,
		keyring: osSecureStringStore{},
		service: licenseServiceName,
		user:    "data-" + hex.EncodeToString(hash[:8]),
		http:    cfg.HTTPClient,
		now:     cfg.Now,
		pending: map[string]string{},
	}
}

// setSecureStore exists for same-package integration tests. Production always
// uses the OS credential store; there is no file fallback.
func (m *LicenseManager) setSecureStore(s SecureStringStore) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.keyring = s
}

func (m *LicenseManager) startupValidate(ctx context.Context) LicenseState {
	state := m.validateStored(ctx, true)
	m.mu.Lock()
	m.state = state
	m.ready = true
	m.mu.Unlock()
	return state
}

func (a *Application) LicenseState() LicenseState {
	if a == nil || a.license == nil {
		return LicenseState{Status: "not_activated"}
	}
	return a.license.Current()
}

func (a *Application) LicenseSummary(ctx context.Context) map[string]any {
	if a == nil || a.license == nil {
		return map[string]any{"status": "not_activated", "remaining_days": 0, "online_refresh_available": false}
	}
	return a.license.summary(ctx)
}

func (a *Application) licenseRefreshLoop(ctx context.Context, manager *LicenseManager) {
	initial, cancel := context.WithTimeout(ctx, 15*time.Second)
	_ = manager.Refresh(initial)
	cancel()
	ticker := time.NewTicker(manager.config.RefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			state := manager.Current()
			// Do not automatically revive an expired/revoked installation. A
			// user-initiated refresh is required after renewal/recovery.
			if state.Status != "active" || !state.OnlineRefreshAvailable {
				continue
			}
			refreshCtx, stop := context.WithTimeout(ctx, 15*time.Second)
			_ = manager.Refresh(refreshCtx)
			stop()
		}
	}
}

func (m *LicenseManager) Current() LicenseState {
	m.mu.RLock()
	state := m.state
	ready := m.ready
	m.mu.RUnlock()
	if !ready {
		return m.withAvailability(LicenseState{Status: "not_activated"})
	}
	if state.Status == "active" && state.Valid && m.store != nil {
		if metadata := m.store.LoadLicenseMetadata(context.Background()); metadata != nil && m.clockRollback(metadata) {
			state.Valid = false
			state.Expired = false
			state.Status = "clock_rollback"
			state.Reason = "ساعت دستگاه به‌طور غیرمنتظره به عقب برگشته است"
		}
	}
	if state.Status == "active" && state.ExpiresAt != "" {
		if expiry, err := parseUTC(state.ExpiresAt); err == nil && !m.now().Before(expiry) {
			state.Valid = false
			state.Expired = true
			state.Status = "expired"
			state.DaysLeft = 0
		}
	}
	return m.withAvailability(state)
}

func (m *LicenseManager) withAvailability(state LicenseState) LicenseState {
	state.OnlineRefreshAvailable = m.config.Enabled && m.config.APIBaseURL != "" && m.hasRefreshToken()
	state.ActivationAvailable = m.config.Enabled && m.config.ConfigError == "" && len(m.config.PublicKeys) > 0
	return state
}

func (m *LicenseManager) hasRefreshToken() bool {
	value, err := m.keyring.Get(m.service, m.userKey("refresh-token"))
	return err == nil && strings.TrimSpace(value) != ""
}

func (m *LicenseManager) userKey(kind string) string {
	return m.user + ":" + kind
}

func (m *LicenseManager) licensePath(fileName string) (string, error) {
	name := filepath.Base(strings.TrimSpace(fileName))
	if name == "." || name == "" || name != strings.TrimSpace(fileName) || strings.ContainsAny(name, `/\\`) || strings.HasPrefix(name, ".") {
		return "", errors.New("invalid local license file name")
	}
	path := filepath.Join(m.dataDir, name)
	return path, nil
}

func (m *LicenseManager) validateStored(ctx context.Context, persist bool) LicenseState {
	if !m.config.Enabled {
		return m.withAvailability(LicenseState{Status: "not_activated", Reason: "سرویس لایسنس هنوز فعال نشده است"})
	}
	if m.config.ConfigError != "" {
		return m.withAvailability(LicenseState{Status: "invalid", Reason: "پیکربندی اعتبارسنجی لایسنس کامل نیست"})
	}
	metadata := m.store.LoadLicenseMetadata(ctx)
	if metadata == nil {
		return m.withAvailability(LicenseState{Status: "not_activated", Reason: "هنوز لایسنسی روی این دستگاه فعال نشده است"})
	}
	identity, deviceID, fingerprint, err := m.credentialsForValidation(metadata)
	if err != nil {
		return m.withAvailability(LicenseState{Status: "secure_store_unavailable", Reason: "فضای امن دستگاه در دسترس نیست"})
	}
	path, err := m.licensePath(metadata.CiphertextFile)
	if err != nil {
		return m.withAvailability(LicenseState{Status: "invalid", Reason: "فایل لایسنس محلی نامعتبر است"})
	}
	ciphertext, err := os.ReadFile(path)
	if err != nil {
		return m.withAvailability(LicenseState{Status: "invalid", Reason: "فایل لایسنس محلی پیدا نشد"})
	}
	if !sameSHA256(ciphertext, metadata.SHA256) {
		return m.withAvailability(LicenseState{Status: "invalid", Reason: "صحت فایل لایسنس تأیید نشد"})
	}
	claims, kid, err := m.decryptAndVerify(ciphertext, identity, deviceID, fingerprint)
	if err != nil {
		return m.withAvailability(LicenseState{Status: "invalid", Reason: "امضای لایسنس معتبر نیست"})
	}
	if metadata.LicenseID != claims.LicenseID || metadata.DeviceID != claims.DeviceID || metadata.LicenseVersion != claims.LicenseVersion {
		return m.withAvailability(LicenseState{Status: "invalid", Reason: "شناسهٔ دستگاه و لایسنس همخوان نیست"})
	}
	if metadata.KeyID != "" && metadata.KeyID != kid {
		return m.withAvailability(LicenseState{Status: "invalid", Reason: "کلید امضای لایسنس معتبر نیست"})
	}
	if rollback := m.clockRollback(metadata); rollback {
		return m.withAvailability(m.stateFromClaims(claims, "clock_rollback", "ساعت دستگاه به‌طور غیرمنتظره به عقب برگشته است"))
	}
	state := m.stateFromClaims(claims, claims.Status, "")
	state.LastValidatedAt = metadata.LastSuccessfulValidation
	if metadata.Status == "revoked" || metadata.Status == "suspended" || metadata.Status == "expired" {
		state.Status = metadata.Status
		state.Valid = false
		state.Expired = metadata.Status == "expired"
	}
	if persist {
		updated := *metadata
		updated.LastSuccessfulValidation = m.now().UTC().Format(time.RFC3339)
		updated.LastSeenLocalUTC = updated.LastSuccessfulValidation
		if state.Status == "active" && state.Valid {
			updated.KeyID = kid
			updated.Status = claims.Status
			updated.CafeName = claims.CafeName
			updated.PlanTitle = claims.PlanTitle
			if updated.PlanTitle == "" {
				updated.PlanTitle = claims.PlanCode
			}
			updated.ValidFrom = claims.NotBefore
			updated.ExpiresAt = claims.ExpiresAt
		}
		if saveErr := m.store.SaveLicenseMetadata(ctx, updated); saveErr != nil {
			return m.withAvailability(LicenseState{Status: "invalid", Reason: "ذخیرهٔ امن وضعیت لایسنس انجام نشد"})
		}
		if markerErr := m.saveClockMarker(updated.LastSeenLocalUTC); markerErr != nil {
			return m.withAvailability(LicenseState{Status: "secure_store_unavailable", Reason: "ذخیرهٔ نشانگر امن زمان دستگاه ممکن نیست"})
		}
		state.LastValidatedAt = updated.LastSuccessfulValidation
	}
	return m.withAvailability(state)
}

func (m *LicenseManager) credentialsForValidation(metadata *store.LicenseMetadata) (age.Identity, string, string, error) {
	identityRaw, err := m.keyring.Get(m.service, m.userKey("age-identity"))
	if err != nil || strings.TrimSpace(identityRaw) == "" {
		return nil, "", "", err
	}
	identity, err := age.ParseX25519Identity(strings.TrimSpace(identityRaw))
	if err != nil {
		return nil, "", "", err
	}
	deviceID, err := m.keyring.Get(m.service, m.userKey("device-id"))
	if err != nil || strings.TrimSpace(deviceID) == "" {
		deviceID = metadata.DeviceID
	}
	if strings.TrimSpace(deviceID) == "" {
		return nil, "", "", errors.New("missing device id")
	}
	fingerprint := deviceFingerprint(identity.Recipient().String())
	return identity, deviceID, fingerprint, nil
}

func (m *LicenseManager) clockRollback(metadata *store.LicenseMetadata) bool {
	metadataSeen, err := parseUTC(metadata.LastSeenLocalUTC)
	if err != nil {
		return true
	}
	markerRaw, err := m.keyring.Get(m.service, m.userKey("clock-last-seen"))
	if err != nil || strings.TrimSpace(markerRaw) == "" {
		return true
	}
	marker, err := parseUTC(markerRaw)
	if err != nil {
		return true
	}
	threshold := marker.Add(-m.config.ClockRollbackTolerance)
	// The keyring marker survives a data-directory rollback. The metadata
	// comparison catches a restored database even when the process clock has
	// not yet moved backwards again.
	return m.now().Before(threshold) || metadataSeen.Before(threshold)
}

func (m *LicenseManager) saveClockMarker(value string) error {
	if _, err := parseUTC(value); err != nil {
		return err
	}
	return m.keyring.Set(m.service, m.userKey("clock-last-seen"), value)
}

func (m *LicenseManager) stateFromClaims(claims licenseClaims, status, reason string) LicenseState {
	now := m.now()
	state := LicenseState{
		Status:        status,
		Valid:         false,
		Reason:        reason,
		Holder:        claims.CafeName,
		Plan:          claims.PlanTitle,
		LicenseID:     claims.LicenseID,
		CafeName:      claims.CafeName,
		PlanTitle:     claims.PlanTitle,
		ValidFrom:     claims.NotBefore,
		ExpiresAt:     claims.ExpiresAt,
		DeviceID:      claims.DeviceID,
	}
	if state.PlanTitle == "" {
		state.PlanTitle = claims.PlanCode
		state.Plan = state.PlanTitle
	}
	expires, expErr := parseUTC(claims.ExpiresAt)
	notBefore, nbfErr := parseUTC(claims.NotBefore)
	if expErr != nil || nbfErr != nil {
		state.Status = "invalid"
		state.Reason = "تاریخ اعتبار لایسنس معتبر نیست"
		return state
	}
	if status == "clock_rollback" || status == "revoked" || status == "suspended" {
		state.Status = status
		state.DaysLeft = daysRemaining(now, expires)
		return state
	}
	if !now.Before(expires) {
		state.Status = "expired"
		state.Expired = true
		state.DaysLeft = 0
		return state
	}
	if now.Before(notBefore) {
		state.Status = "not_yet_valid"
		state.DaysLeft = daysRemaining(now, expires)
		return state
	}
	if status != "active" {
		state.Status = status
		state.DaysLeft = daysRemaining(now, expires)
		return state
	}
	state.Status = "active"
	state.Valid = true
	state.DaysLeft = daysRemaining(now, expires)
	return state
}

func daysRemaining(now, expires time.Time) int {
	if !now.Before(expires) {
		return 0
	}
	return int(expires.Sub(now) / (24 * time.Hour))
}

func parseUTC(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" || !(strings.HasSuffix(value, "Z") || strings.HasSuffix(value, "+00:00")) {
		return time.Time{}, errors.New("license dates must be UTC")
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, errors.New("invalid UTC license date")
	}
	_, offset := parsed.Zone()
	if offset != 0 {
		return time.Time{}, errors.New("invalid UTC license date")
	}
	return parsed.UTC(), nil
}

func deviceFingerprint(recipient string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(recipient)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func sameSHA256(data []byte, expected string) bool {
	sum := sha256.Sum256(data)
	left := strings.ToLower(strings.TrimSpace(expected))
	right := hex.EncodeToString(sum[:])
	return left != "" && subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func decodeBase64(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if decoded, err := encoding.DecodeString(value); err == nil {
			return decoded, nil
		}
	}
	return nil, errors.New("invalid base64")
}

func parsePublicKeys(raw string) (map[string]ed25519.PublicKey, error) {
	keys := map[string]ed25519.PublicKey{}
	if strings.TrimSpace(raw) == "" {
		return keys, nil
	}
	var encoded map[string]string
	if err := json.Unmarshal([]byte(raw), &encoded); err != nil {
		return keys, errors.New("COFFISH_LICENSE_PUBLIC_KEYS must be a JSON object")
	}
	for kid, value := range encoded {
		kid = strings.TrimSpace(kid)
		if kid == "" {
			return keys, errors.New("license signing key id is empty")
		}
		value = strings.TrimSpace(value)
		var decoded []byte
		var err error
		if len(value) == ed25519.PublicKeySize*2 {
			decoded, err = hex.DecodeString(value)
		}
		if len(decoded) != ed25519.PublicKeySize {
			decoded, err = decodeBase64(value)
		}
		if err != nil || len(decoded) != ed25519.PublicKeySize {
			return keys, errors.New("license signing public key is not base64 or hex")
		}
		keys[kid] = append(ed25519.PublicKey(nil), decoded...)
	}
	return keys, nil
}

func verifyCompactJWS(compact string, publicKeys map[string]ed25519.PublicKey) (licenseClaims, string, error) {
	parts := strings.Split(strings.TrimSpace(compact), ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return licenseClaims{}, "", errors.New("license is not compact JWS")
	}
	headerRaw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return licenseClaims{}, "", err
	}
	var header compactJWSHeader
	if err := json.Unmarshal(headerRaw, &header); err != nil || header.Algorithm != "EdDSA" || header.KeyID == "" {
		return licenseClaims{}, "", errors.New("untrusted JWS header")
	}
	publicKey, ok := publicKeys[header.KeyID]
	if !ok || len(publicKey) != ed25519.PublicKeySize {
		return licenseClaims{}, "", errors.New("unknown license signing key")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(signature) != ed25519.SignatureSize {
		return licenseClaims{}, "", errors.New("invalid JWS signature encoding")
	}
	if !ed25519.Verify(publicKey, []byte(parts[0]+"."+parts[1]), signature) {
		return licenseClaims{}, "", errors.New("invalid license signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return licenseClaims{}, "", err
	}
	var claims licenseClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return licenseClaims{}, "", errors.New("invalid verified license claims")
	}
	return claims, header.KeyID, nil
}

func (m *LicenseManager) decryptAndVerify(ciphertext []byte, identity age.Identity, deviceID, fingerprint string) (licenseClaims, string, error) {
	reader, err := age.Decrypt(bytes.NewReader(ciphertext), identity)
	if err != nil {
		return licenseClaims{}, "", err
	}
	jws, err := io.ReadAll(io.LimitReader(reader, licenseMaxFileBytes))
	if err != nil || len(jws) == 0 {
		return licenseClaims{}, "", errors.New("license payload could not be read")
	}
	claims, kid, err := verifyCompactJWS(string(jws), m.config.PublicKeys)
	if err != nil {
		return licenseClaims{}, "", err
	}
	if err := m.validateClaims(claims, deviceID, fingerprint); err != nil {
		return licenseClaims{}, "", err
	}
	return claims, kid, nil
}

func (m *LicenseManager) validateClaims(claims licenseClaims, deviceID, fingerprint string) error {
	if claims.SchemaVersion != licenseSchemaVersion || claims.Issuer != m.config.Issuer || claims.Audience != m.config.Audience {
		return errors.New("license issuer, audience, or schema is not trusted")
	}
	for _, value := range []string{claims.JTI, claims.LicenseID, claims.CafeID, claims.CafeName, claims.PlanCode, claims.ProductType, claims.DeviceID, claims.DevicePublicKeyFingerprint, claims.IssuedAt, claims.NotBefore, claims.ExpiresAt} {
		if strings.TrimSpace(value) == "" {
			return errors.New("license claims are incomplete")
		}
	}
	if claims.ProductType != "offline_standalone" {
		return errors.New("license product type is not eligible")
	}
	if claims.LicenseVersion <= 0 {
		return errors.New("license version is invalid")
	}
	if claims.Status != "active" && claims.Status != "expired" && claims.Status != "suspended" && claims.Status != "revoked" {
		return errors.New("license status is invalid")
	}
	if subtle.ConstantTimeCompare([]byte(claims.DeviceID), []byte(deviceID)) != 1 || subtle.ConstantTimeCompare([]byte(claims.DevicePublicKeyFingerprint), []byte(fingerprint)) != 1 {
		return errors.New("license is bound to another device")
	}
	issued, err := parseUTC(claims.IssuedAt)
	if err != nil {
		return err
	}
	notBefore, err := parseUTC(claims.NotBefore)
	if err != nil {
		return err
	}
	expires, err := parseUTC(claims.ExpiresAt)
	if err != nil {
		return err
	}
	if issued.After(expires) || notBefore.After(expires) {
		return errors.New("license date range is invalid")
	}
	return nil
}

func normalizeLicenseBaseURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("license API base URL is empty")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return nil, errors.New("license API base URL is invalid")
	}
	if parsed.Scheme != "https" {
		host := parsed.Hostname()
		if parsed.Scheme != "http" || !(host == "localhost" || host == "127.0.0.1" || host == "::1") {
			return nil, errors.New("license API must use HTTPS outside loopback development")
		}
	}
	return parsed, nil
}

func joinLicenseURL(base, suffix string) (string, error) {
	parsed, err := normalizeLicenseBaseURL(base)
	if err != nil {
		return "", err
	}
	left := strings.TrimRight(parsed.Path, "/")
	right := "/" + strings.TrimLeft(suffix, "/")
	parsed.Path = left + right
	parsed.RawPath = ""
	return parsed.String(), nil
}

func newUUID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s", hex.EncodeToString(raw[0:4]), hex.EncodeToString(raw[4:6]), hex.EncodeToString(raw[6:8]), hex.EncodeToString(raw[8:10]), hex.EncodeToString(raw[10:16])), nil
}

func validateActivationCode(code string) error {
	code = strings.TrimSpace(code)
	if code == "" || len([]byte(code)) > licenseMaxCodeBytes {
		return errors.New("کد فعال‌سازی را کامل وارد کنید")
	}
	for _, r := range code {
		if r < 0x20 || r == 0x7f {
			return errors.New("کد فعال‌سازی نامعتبر است")
		}
	}
	return nil
}

func (m *LicenseManager) ensureCredentials() (age.Identity, string, string, error) {
	identityRaw, err := m.keyring.Get(m.service, m.userKey("age-identity"))
	if err != nil && !errors.Is(err, errLicenseNotFound) {
		return nil, "", "", errors.New("فضای امن دستگاه در دسترس نیست")
	}
	var identity *age.X25519Identity
	if strings.TrimSpace(identityRaw) == "" {
		identity, err = age.GenerateX25519Identity()
		if err != nil {
			return nil, "", "", err
		}
		identityRaw = identity.String()
		if err := m.keyring.Set(m.service, m.userKey("age-identity"), identityRaw); err != nil {
			return nil, "", "", errors.New("ذخیرهٔ کلید امن دستگاه ممکن نیست")
		}
	} else {
		identity, err = age.ParseX25519Identity(strings.TrimSpace(identityRaw))
		if err != nil {
			return nil, "", "", errors.New("کلید امن دستگاه معتبر نیست")
		}
	}
	deviceID, err := m.keyring.Get(m.service, m.userKey("device-id"))
	if err != nil && !errors.Is(err, errLicenseNotFound) {
		return nil, "", "", errors.New("شناسهٔ امن دستگاه در دسترس نیست")
	}
	// The backend assigns device_id during activation; it is intentionally not
	// sent in the activation request. Once the response is verified, commitVerified
	// stores that server-issued id in the keyring.
	deviceID = strings.TrimSpace(deviceID)
	clockMarker, err := m.keyring.Get(m.service, m.userKey("clock-last-seen"))
	if err != nil && !errors.Is(err, errLicenseNotFound) {
		return nil, "", "", errors.New("نشانگر امن زمان دستگاه در دسترس نیست")
	}
	if strings.TrimSpace(clockMarker) == "" {
		clockMarker = m.now().UTC().Format(time.RFC3339)
		if err := m.saveClockMarker(clockMarker); err != nil {
			return nil, "", "", errors.New("ذخیرهٔ نشانگر امن زمان دستگاه ممکن نیست")
		}
	}
	return identity, deviceID, deviceFingerprint(identity.Recipient().String()), nil
}

func (m *LicenseManager) idempotencyKey(code string) (string, error) {
	sum := sha256.Sum256([]byte(code))
	hash := hex.EncodeToString(sum[:])
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing := m.pending[hash]; existing != "" {
		return existing, nil
	}
	id, err := newUUID()
	if err != nil {
		return "", err
	}
	m.pending[hash] = id
	return id, nil
}

func (m *LicenseManager) finishIdempotency(code string, keep bool) {
	if keep {
		return
	}
	sum := sha256.Sum256([]byte(code))
	m.mu.Lock()
	delete(m.pending, hex.EncodeToString(sum[:]))
	m.mu.Unlock()
}

func (m *LicenseManager) activateEndpoint(ctx context.Context, body []byte) (licenseActivationResponse, *licenseAPIError) {
	urlValue, err := joinLicenseURL(m.config.APIBaseURL, "/offline-licenses/activate")
	if err != nil {
		return licenseActivationResponse{}, &licenseAPIError{Message: "نشانی سرویس فعال‌سازی معتبر نیست"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlValue, bytes.NewReader(body))
	if err != nil {
		return licenseActivationResponse{}, &licenseAPIError{Message: "ساخت درخواست فعال‌سازی ممکن نیست"}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cache-Control", "no-store")
	return doLicenseJSON(m, req, func(raw json.RawMessage) (licenseActivationResponse, error) {
		var data licenseActivationResponse
		if err := json.Unmarshal(raw, &data); err != nil {
			return data, errors.New("پاسخ فعال‌سازی ساختار معتبر ندارد")
		}
		return data, nil
	})
}

func (m *LicenseManager) currentEndpoint(ctx context.Context, deviceID, token string) (licenseCurrentResponse, *licenseAPIError) {
	urlValue, err := joinLicenseURL(m.config.APIBaseURL, "/offline-licenses/current")
	if err != nil {
		return licenseCurrentResponse{}, &licenseAPIError{Message: "نشانی سرویس تمدید معتبر نیست"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlValue, nil)
	if err != nil {
		return licenseCurrentResponse{}, &licenseAPIError{Message: "ساخت درخواست تمدید ممکن نیست"}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cache-Control", "no-store")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Device-ID", deviceID)
	return doLicenseJSON(m, req, func(raw json.RawMessage) (licenseCurrentResponse, error) {
		var data licenseCurrentResponse
		if err := json.Unmarshal(raw, &data); err != nil {
			return data, errors.New("پاسخ تمدید ساختار معتبر ندارد")
		}
		return data, nil
	})
}

func doLicenseJSON[T any](m *LicenseManager, req *http.Request, decode func(json.RawMessage) (T, error)) (T, *licenseAPIError) {
	var zero T
	resp, err := m.http.Do(req)
	if err != nil {
		return zero, &licenseAPIError{Retryable: true, Message: "اتصال به سرویس لایسنس برقرار نشد؛ دوباره تلاش کنید"}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return zero, &licenseAPIError{Retryable: true, Message: "پاسخ سرویس لایسنس دریافت نشد"}
	}
	var envelope licenseEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
			return zero, &licenseAPIError{HTTPStatus: resp.StatusCode, Retryable: true, Message: "سرویس لایسنس موقتاً در دسترس نیست"}
		}
		return zero, &licenseAPIError{HTTPStatus: resp.StatusCode, Message: "پاسخ سرویس لایسنس معتبر نیست"}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !envelope.Success {
		return zero, mapLicenseAPIError(resp.StatusCode, envelope.Error)
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return zero, &licenseAPIError{HTTPStatus: resp.StatusCode, Message: "پاسخ سرویس لایسنس ناقص است"}
	}
	decoded, err := decode(envelope.Data)
	if err != nil {
		return zero, &licenseAPIError{HTTPStatus: resp.StatusCode, Message: err.Error()}
	}
	return decoded, nil
}

func mapLicenseAPIError(status int, raw json.RawMessage) *licenseAPIError {
	code := ""
	var object struct {
		Code string `json:"code"`
	}
	if json.Unmarshal(raw, &object) == nil {
		code = strings.TrimSpace(object.Code)
	}
	messages := map[string]string{
		"OFFLINE_LICENSE_CODE_INVALID":       "کد فعال‌سازی نادرست است.",
		"OFFLINE_LICENSE_CODE_EXPIRED":       "کد فعال‌سازی منقضی شده است؛ از وب کد تازه بگیرید.",
		"OFFLINE_LICENSE_DEVICE_LIMIT_REACHED": "ظرفیت دستگاه‌های این اشتراک تکمیل است؛ از پنل وب یا پشتیبانی کمک بگیرید.",
		"OFFLINE_LICENSE_RATE_LIMITED":       "تعداد تلاش‌ها زیاد است؛ کمی بعد دوباره تلاش کنید.",
		"OFFLINE_LICENSE_DEVICE_AUTH_REQUIRED": "دستگاه نیاز به فعال‌سازی دوباره دارد.",
		"OFFLINE_LICENSE_CAFE_SCOPE_MISMATCH": "این کد برای این دستگاه یا کافه قابل استفاده نیست.",
		"OFFLINE_LICENSE_EXPIRED":            "اشتراک نسخهٔ آفلاین منووکس به پایان رسیده است.",
		"OFFLINE_LICENSE_REVOKED":            "این اشتراک لغو شده است؛ با پشتیبانی تماس بگیرید.",
		"OFFLINE_LICENSES_NOT_ENABLED":       "سرویس لایسنس هنوز فعال نشده است؛ با پشتیبانی تماس بگیرید.",
	}
	message := messages[code]
	if message == "" {
		if status >= 500 || status == http.StatusTooManyRequests {
			message = "سرویس لایسنس موقتاً در دسترس نیست؛ دوباره تلاش کنید."
		} else {
			message = "درخواست لایسنس انجام نشد؛ اطلاعات واردشده را بررسی کنید."
		}
	}
	retryable := status >= 500 || status == http.StatusTooManyRequests || code == "OFFLINE_LICENSE_RATE_LIMITED"
	return &licenseAPIError{Code: code, HTTPStatus: status, Retryable: retryable, Message: message}
}

func (m *LicenseManager) Activate(ctx context.Context, code string) (LicenseState, error) {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()
	if !m.config.Enabled {
		return m.Current(), errors.New("سرویس فعال‌سازی لایسنس هنوز فعال نشده است")
	}
	if m.config.ConfigError != "" || len(m.config.PublicKeys) == 0 {
		return m.Current(), errors.New("کلید عمومی اعتبارسنجی لایسنس در تنظیمات برنامه ثبت نشده است")
	}
	code = strings.TrimSpace(code)
	if err := validateActivationCode(code); err != nil {
		return m.Current(), err
	}
	identity, _, fingerprint, err := m.ensureCredentials()
	if err != nil {
		return m.Current(), err
	}
	idempotency, err := m.idempotencyKey(code)
	if err != nil {
		return m.Current(), errors.New("شناسهٔ امن درخواست فعال‌سازی ساخته نشد")
	}
	body, err := json.Marshal(map[string]string{
		"activation_code": code,
		"age_recipient":   identity.Recipient().String(),
		"device_label":    m.config.DeviceLabel,
		"app_version":     m.config.AppVersion,
		"idempotency_key": idempotency,
	})
	if err != nil {
		return m.Current(), errors.New("درخواست فعال‌سازی ساخته نشد")
	}
	response, apiErr := m.activateEndpoint(ctx, body)
	if apiErr != nil {
		m.finishIdempotency(code, apiErr.Retryable)
		return m.Current(), apiErr
	}
	if response.Status != "active" || response.LicenseID == "" || response.DeviceID == "" || response.LicenseVersion <= 0 || response.FileFormat != licenseFileFormat || strings.TrimSpace(response.RefreshToken) == "" || strings.TrimSpace(response.ServerTime) == "" {
		return m.Current(), errors.New("پاسخ فعال‌سازی کامل یا معتبر نیست")
	}
	ciphertext, err := decodeBase64(response.FileBase64)
	if err != nil || len(ciphertext) == 0 || len(ciphertext) > licenseMaxFileBytes {
		return m.Current(), errors.New("فایل رمز‌شدهٔ لایسنس معتبر نیست")
	}
	if !sameSHA256(ciphertext, response.FileSHA256) {
		return m.Current(), errors.New("صحت فایل لایسنس تأیید نشد")
	}
	claims, kid, err := m.decryptAndVerify(ciphertext, identity, response.DeviceID, fingerprint)
	if err != nil || claims.Status != "active" || claims.LicenseID != response.LicenseID || claims.DeviceID != response.DeviceID || claims.LicenseVersion != response.LicenseVersion {
		return m.Current(), errors.New("فایل لایسنس برای این دستگاه معتبر نیست")
	}
	if _, err := parseUTC(response.ServerTime); err != nil {
		return m.Current(), errors.New("زمان سرویس لایسنس معتبر نیست")
	}
	if err := m.commitVerified(ctx, ciphertext, response.FileSHA256, response.FileFormat, response.LicenseID, response.DeviceID, claims, kid, response.ServerTime, response.RefreshToken); err != nil {
		return m.Current(), err
	}
	m.finishIdempotency(code, false)
	return m.Current(), nil
}

func (m *LicenseManager) Refresh(ctx context.Context) error {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()
	if !m.config.Enabled {
		return errors.New("سرویس تمدید لایسنس هنوز فعال نشده است")
	}
	if m.config.ConfigError != "" || len(m.config.PublicKeys) == 0 {
		return errors.New("کلید عمومی اعتبارسنجی لایسنس در تنظیمات برنامه ثبت نشده است")
	}
	metadata := m.store.LoadLicenseMetadata(ctx)
	if metadata == nil {
		return errors.New("هنوز لایسنسی روی این دستگاه فعال نشده است")
	}
	if m.clockRollback(metadata) {
		return errors.New("ساعت دستگاه به‌طور غیرمنتظره به عقب برگشته است")
	}
	identity, deviceID, fingerprint, err := m.credentialsForValidation(metadata)
	if err != nil {
		return errors.New("فضای امن دستگاه در دسترس نیست")
	}
	token, err := m.keyring.Get(m.service, m.userKey("refresh-token"))
	if err != nil || strings.TrimSpace(token) == "" {
		return errors.New("توکن امن دستگاه پیدا نشد؛ دستگاه را با کد فعال‌سازی متصل کنید")
	}
	response, apiErr := m.currentEndpoint(ctx, deviceID, token)
	if apiErr != nil {
		if apiErr.Code == "OFFLINE_LICENSE_REVOKED" {
			m.setServerLock(ctx, metadata, "revoked")
		} else if apiErr.Code == "OFFLINE_LICENSE_EXPIRED" {
			m.setServerLock(ctx, metadata, "expired")
		}
		return apiErr
	}
	if response.Status != "active" || response.LicenseVersion <= 0 || response.FileFormat != licenseFileFormat || strings.TrimSpace(response.RefreshToken) == "" || strings.TrimSpace(response.ServerTime) == "" {
		return errors.New("پاسخ تمدید لایسنس کامل یا معتبر نیست")
	}
	ciphertext, err := decodeBase64(response.FileBase64)
	if err != nil || len(ciphertext) == 0 || len(ciphertext) > licenseMaxFileBytes || !sameSHA256(ciphertext, response.FileSHA256) {
		return errors.New("فایل تازهٔ لایسنس معتبر نیست")
	}
	claims, kid, err := m.decryptAndVerify(ciphertext, identity, deviceID, fingerprint)
	if err != nil || claims.Status != "active" || claims.LicenseID != metadata.LicenseID || claims.LicenseVersion != response.LicenseVersion {
		return errors.New("فایل تازهٔ لایسنس برای این دستگاه معتبر نیست")
	}
	if _, err := parseUTC(response.ServerTime); err != nil {
		return errors.New("زمان سرویس لایسنس معتبر نیست")
	}
	_, err = m.commitVerified(ctx, ciphertext, response.FileSHA256, response.FileFormat, metadata.LicenseID, deviceID, claims, kid, response.ServerTime, response.RefreshToken)
	return err
}

func (m *LicenseManager) setServerLock(ctx context.Context, old *store.LicenseMetadata, status string) {
	copy := *old
	copy.Status = status
	_ = m.store.SaveLicenseMetadata(ctx, copy)
	state := m.Current()
	state.Status = status
	state.Valid = false
	state.Expired = status == "expired"
	m.mu.Lock()
	m.state = state
	m.mu.Unlock()
}

func writePrivateAtomic(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp := path + ".tmp-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(tmp)
		}
	}()
	if _, err := file.Write(content); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	ok = true
	return nil
}

func (m *LicenseManager) commitVerified(ctx context.Context, ciphertext []byte, checksum, format, licenseID, deviceID string, claims licenseClaims, kid, serverTime, refreshToken string) error {
	if strings.TrimSpace(refreshToken) == "" {
		return errors.New("توکن تازهٔ دستگاه دریافت نشد")
	}
	old := m.store.LoadLicenseMetadata(ctx)
	oldToken, oldTokenErr := m.keyring.Get(m.service, m.userKey("refresh-token"))
	oldDeviceID, oldDeviceIDErr := m.keyring.Get(m.service, m.userKey("device-id"))
	oldClockMarker, oldClockMarkerErr := m.keyring.Get(m.service, m.userKey("clock-last-seen"))
	if oldClockMarkerErr != nil && !errors.Is(oldClockMarkerErr, errLicenseNotFound) {
		return errors.New("نشانگر امن زمان دستگاه در دسترس نیست")
	}
	if err := m.keyring.Set(m.service, m.userKey("device-id"), deviceID); err != nil {
		return errors.New("ذخیرهٔ امن شناسهٔ دستگاه ممکن نیست")
	}
	if err := m.keyring.Set(m.service, m.userKey("refresh-token"), refreshToken); err != nil {
		m.restoreDeviceID(oldDeviceID, oldDeviceIDErr)
		return errors.New("ذخیرهٔ امن توکن دستگاه ممکن نیست")
	}
	fileName := "license-" + strings.ToLower(strings.TrimSpace(checksum)) + ".age"
	path, err := m.licensePath(fileName)
	if err != nil {
		m.restoreRefreshToken(oldToken, oldTokenErr)
		m.restoreDeviceID(oldDeviceID, oldDeviceIDErr)
		return err
	}
	existing, readErr := os.ReadFile(path)
	wroteFile := false
	if readErr != nil || !bytes.Equal(existing, ciphertext) {
		if err := writePrivateAtomic(path, ciphertext); err != nil {
			m.restoreRefreshToken(oldToken, oldTokenErr)
			m.restoreDeviceID(oldDeviceID, oldDeviceIDErr)
			return errors.New("ذخیرهٔ امن فایل لایسنس ممکن نیست")
		}
		wroteFile = true
	}
	metadata := store.LicenseMetadata{
		CiphertextFile:    fileName,
		FileFormat:        format,
		SHA256:            strings.ToLower(strings.TrimSpace(checksum)),
		LicenseID:         licenseID,
		DeviceID:          deviceID,
		DeviceFingerprint: claims.DevicePublicKeyFingerprint,
		LicenseVersion:    claims.LicenseVersion,
		KeyID:             kid,
		Issuer:            claims.Issuer,
		Audience:          claims.Audience,
		ProductType:       claims.ProductType,
		CafeName:          claims.CafeName,
		PlanTitle:         claims.PlanTitle,
		PlanCode:          claims.PlanCode,
		ValidFrom:         claims.NotBefore,
		ExpiresAt:         claims.ExpiresAt,
		Status:            claims.Status,
		ServerTime:        serverTime,
		LastSuccessfulValidation: m.now().UTC().Format(time.RFC3339),
		LastSeenLocalUTC:        m.now().UTC().Format(time.RFC3339),
	}
	if metadata.PlanTitle == "" {
		metadata.PlanTitle = claims.PlanCode
	}
	if err := m.saveClockMarker(metadata.LastSeenLocalUTC); err != nil {
		if wroteFile {
			_ = os.Remove(path)
		}
		m.restoreRefreshToken(oldToken, oldTokenErr)
		m.restoreDeviceID(oldDeviceID, oldDeviceIDErr)
		return errors.New("ذخیرهٔ نشانگر امن زمان دستگاه ممکن نیست")
	}
	if err := m.store.SaveLicenseMetadata(ctx, metadata); err != nil {
		if wroteFile {
			_ = os.Remove(path)
		}
		m.restoreClockMarker(oldClockMarker, oldClockMarkerErr)
		m.restoreRefreshToken(oldToken, oldTokenErr)
		m.restoreDeviceID(oldDeviceID, oldDeviceIDErr)
		return errors.New("ذخیرهٔ اتمیک وضعیت لایسنس انجام نشد")
	}
	if old != nil && old.CiphertextFile != fileName {
		if oldPath, oldErr := m.licensePath(old.CiphertextFile); oldErr == nil {
			_ = os.Remove(oldPath)
		}
	}
	state := m.stateFromClaims(claims, claims.Status, "")
	state.LastValidatedAt = metadata.LastSuccessfulValidation
	m.mu.Lock()
	m.state = state
	m.ready = true
	m.mu.Unlock()
	return nil
}

func (m *LicenseManager) restoreRefreshToken(old string, oldErr error) {
	if oldErr == nil && old != "" {
		_ = m.keyring.Set(m.service, m.userKey("refresh-token"), old)
		return
	}
	_ = m.keyring.Delete(m.service, m.userKey("refresh-token"))
}

func (m *LicenseManager) restoreDeviceID(old string, oldErr error) {
	if oldErr == nil && old != "" {
		_ = m.keyring.Set(m.service, m.userKey("device-id"), old)
		return
	}
	_ = m.keyring.Delete(m.service, m.userKey("device-id"))
}

func (m *LicenseManager) restoreClockMarker(old string, oldErr error) {
	if oldErr == nil && old != "" {
		_ = m.keyring.Set(m.service, m.userKey("clock-last-seen"), old)
		return
	}
	_ = m.keyring.Delete(m.service, m.userKey("clock-last-seen"))
}

func (m *LicenseManager) forgetDevice(ctx context.Context) error {
	for _, kind := range []string{"age-identity", "device-id", "refresh-token", "clock-last-seen"} {
		if err := m.keyring.Delete(m.service, m.userKey(kind)); err != nil && !errors.Is(err, errLicenseNotFound) {
			return err
		}
	}
	// Sales/catalog entities remain untouched; only the license projection is
	// cleared. This method is intentionally not exposed as a public HTTP route.
	return m.store.ClearLicenseMetadata(ctx)
}

func (m *LicenseManager) summary(ctx context.Context) map[string]any {
	state := m.Current()
	return map[string]any{
		"status":                   state.Status,
		"license_id":               state.LicenseID,
		"cafe_name":                state.CafeName,
		"plan_title":               state.PlanTitle,
		"valid_from":               state.ValidFrom,
		"expires_at":               state.ExpiresAt,
		"remaining_days":           state.DaysLeft,
		"last_validated_at":        state.LastValidatedAt,
		"online_refresh_available": state.OnlineRefreshAvailable,
	}
}

// refreshErrorText is kept separate from server response bodies so secrets or
// PII can never be reflected into HTMX output.
func refreshErrorText(err error) string {
	if err == nil {
		return ""
	}
	var apiErr *licenseAPIError
	if errors.As(err, &apiErr) {
		return apiErr.Message
	}
	return err.Error()
}

func licenseMutationRequest(r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
		return false
	}
	switch r.URL.Path {
	case "/license/activate", "/license/refresh", "/login", "/register", "/logout", "/local/export-backup":
		return false
	default:
		return true
	}
}

func (a *Application) blockLockedMutation(w http.ResponseWriter, r *http.Request) bool {
	if a.authDisabled || a.license == nil || !licenseMutationRequest(r) {
		return false
	}
	state := a.license.Current()
	if state.CanMutate() {
		return false
	}
	message := "عملیات فروش و تغییر اطلاعات تا معتبرشدن لایسنس قفل است؛ اطلاعات محلی شما حفظ شده است."
	if strings.HasPrefix(r.URL.Path, "/api/v1/") {
		writeJSONEnvelope(w, http.StatusLocked, false, message, nil, map[string]string{"code": "OFFLINE_LICENSE_LOCKED", "status": state.Status})
	} else {
		http.Error(w, message, http.StatusLocked)
	}
	return true
}
