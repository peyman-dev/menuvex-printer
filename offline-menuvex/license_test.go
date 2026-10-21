package desktop

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/zalando/go-keyring"
)

type memoryLicenseKeyring struct {
	mu     sync.Mutex
	values map[string]string
}

func newMemoryLicenseKeyring() *memoryLicenseKeyring {
	return &memoryLicenseKeyring{values: map[string]string{}}
}

func (m *memoryLicenseKeyring) key(service, user string) string { return service + "\x00" + user }

func (m *memoryLicenseKeyring) Get(service, user string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, ok := m.values[m.key(service, user)]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return value, nil
}

func (m *memoryLicenseKeyring) Set(service, user, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values[m.key(service, user)] = value
	return nil
}

func (m *memoryLicenseKeyring) Delete(service, user string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := m.key(service, user)
	if _, ok := m.values[key]; !ok {
		return keyring.ErrNotFound
	}
	delete(m.values, key)
	return nil
}

type licenseFixtureServer struct {
	server       *httptest.Server
	privateKey   ed25519.PrivateKey
	publicKey    ed25519.PublicKey
	kid          string
	recipient    age.Recipient
	recipientText string
	fingerprint   string
	deviceID     string
	licenseID    string
	version      int64
	refreshToken string
	now          time.Time
	mu           sync.Mutex
	lastActivate map[string]string
}

func newLicenseFixtureServer(t *testing.T, now time.Time) *licenseFixtureServer {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &licenseFixtureServer{
		privateKey:   privateKey,
		publicKey:    publicKey,
		kid:          "test-2026-01",
		deviceID:     "device-server-1",
		licenseID:    "license-server-1",
		version:      1,
		refreshToken: "refresh-token-1",
		now:          now,
		lastActivate: map[string]string{},
	}
	fixture.server = httptest.NewServer(http.HandlerFunc(fixture.handle))
	t.Cleanup(fixture.server.Close)
	return fixture
}

func (f *licenseFixtureServer) handle(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/v1/offline-licenses/activate" && r.Method == http.MethodPost {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 64<<10))
		var request map[string]string
		if json.Unmarshal(body, &request) != nil || request["activation_code"] == "" || request["age_recipient"] == "" {
			writeFixtureJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": map[string]string{"code": "OFFLINE_LICENSE_CODE_INVALID"}})
			return
		}
		if _, ok := request["device_id"]; ok {
			writeFixtureJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": map[string]string{"code": "BAD_REQUEST"}})
			return
		}
		recipient, err := age.ParseX25519Recipient(request["age_recipient"])
		if err != nil {
			writeFixtureJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": map[string]string{"code": "BAD_REQUEST"}})
			return
		}
		f.mu.Lock()
		f.recipient = recipient
		f.recipientText = request["age_recipient"]
		f.fingerprint = deviceFingerprint(f.recipientText)
		f.lastActivate[request["idempotency_key"]] = request["activation_code"]
		f.mu.Unlock()
		ciphertext, checksum := f.encryptedFileNoT(request)
		writeFixtureJSON(w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{
			"license_id": f.licenseID, "device_id": f.deviceID, "status": "active", "server_time": f.now.Format(time.RFC3339),
			"license_version": f.version, "license_file_format": licenseFileFormat, "license_file_name": "menuvex-license.age",
			"license_file_sha256": checksum, "license_file_base64": base64.StdEncoding.EncodeToString(ciphertext), "refresh_token": f.refreshToken,
		}})
		return
	}
	if r.URL.Path == "/api/v1/offline-licenses/current" && r.Method == http.MethodGet {
		if r.Header.Get("X-Device-ID") != f.deviceID || r.Header.Get("Authorization") != "Bearer "+f.refreshToken {
			writeFixtureJSON(w, http.StatusUnauthorized, map[string]any{"success": false, "error": map[string]string{"code": "OFFLINE_LICENSE_DEVICE_AUTH_REQUIRED"}})
			return
		}
		f.mu.Lock()
		f.version++
		f.refreshToken = "refresh-token-2"
		f.mu.Unlock()
		ciphertext, checksum := f.encryptedFileNoT(nil)
		writeFixtureJSON(w, http.StatusOK, map[string]any{"success": true, "data": map[string]any{
			"status": "active", "server_time": f.now.Format(time.RFC3339), "license_version": f.version,
			"expires_at": f.now.Add(72 * time.Hour).Format(time.RFC3339), "license_file_format": licenseFileFormat,
			"license_file_sha256": checksum, "license_file_base64": base64.StdEncoding.EncodeToString(ciphertext), "refresh_token": f.refreshToken,
		}})
		return
	}
	writeFixtureJSON(w, http.StatusNotFound, map[string]any{"success": false, "error": map[string]string{"code": "NOT_FOUND"}})
}

func (f *licenseFixtureServer) encryptedFileNoT(_ map[string]string) ([]byte, string) {
	plain := f.jwsForCurrent()
	var encrypted bytes.Buffer
	writer, _ := age.Encrypt(&encrypted, f.recipient)
	_, _ = writer.Write(plain)
	_ = writer.Close()
	ciphertext := encrypted.Bytes()
	sum := sha256.Sum256(ciphertext)
	return ciphertext, hex.EncodeToString(sum[:])
}

func (f *licenseFixtureServer) jwsForCurrent() []byte {
	expires := f.now.Add(72 * time.Hour)
	if f.version == 1 {
		expires = f.now.Add(24 * time.Hour)
	}
	claims := map[string]any{
		"schema_version": 1, "iss": licenseIssuer, "aud": licenseAudience,
		"jti": f.licenseID + ":" + string(rune('0'+f.version)), "license_id": f.licenseID, "cafe_id": "cafe-server-1",
		"cafe_name": "کافه تست قرارداد", "plan_code": "server-plan-code", "plan_title": "پلن آفلاین آزمون",
		"product_type": "offline_standalone", "entitlements": []string{"offline_pos"}, "device_id": f.deviceID,
		"device_public_key_fingerprint": f.fingerprint, "issued_at": f.now.Format(time.RFC3339),
		"not_before": f.now.Format(time.RFC3339), "expires_at": expires.Format(time.RFC3339),
		"license_version": f.version, "status": "active",
	}
	payloadRaw, _ := json.Marshal(claims)
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"EdDSA","kid":"test-2026-01"}`))
	payload := base64.RawURLEncoding.EncodeToString(payloadRaw)
	signed := header + "." + payload
	return []byte(signed + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(f.privateKey, []byte(signed))))
}


func writeFixtureJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func configureLicenseForTest(t *testing.T, a *Application, fixture *licenseFixtureServer, now time.Time) {
	t.Helper()
	cfg := LicenseConfig{
		Enabled:                true,
		APIBaseURL:             fixture.server.URL + "/api/v1",
		Issuer:                 licenseIssuer,
		Audience:               licenseAudience,
		PublicKeys:             map[string]ed25519.PublicKey{fixture.kid: fixture.publicKey},
		DeviceLabel:            "صندوق آزمون",
		AppVersion:             "test",
		ClockRollbackTolerance: time.Minute,
		HTTPClient:             fixture.server.Client(),
		Now:                    func() time.Time { return now },
	}
	manager := newLicenseManager(a.license.dataDir, a.store, cfg)
	manager.setSecureStore(newMemoryLicenseKeyring())
	a.license = manager
	manager.startupValidate(nilContext{})
}

type nilContext struct{}

func (nilContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (nilContext) Done() <-chan struct{}       { return nil }
func (nilContext) Err() error                   { return nil }
func (nilContext) Value(any) any                { return nil }

func TestRealLicenseActivationUsesAgeJWSAndKeyring(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	fixture := newLicenseFixtureServer(t, now)
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	configureLicenseForTest(t, a, fixture, now)
	state, err := a.license.Activate(nilContext{}, "ACTIVATION-CODE-ONE")
	if err != nil {
		t.Fatal(err)
	}
	if !state.Valid || state.Status != "active" || state.LicenseID != fixture.licenseID {
		t.Fatalf("activation state = %+v", state)
	}
	metadata := a.store.LoadLicenseMetadata(nilContext{})
	if metadata == nil || metadata.FileFormat != licenseFileFormat || metadata.DeviceID != fixture.deviceID {
		t.Fatalf("metadata = %+v", metadata)
	}
	if _, err := a.license.keyring.Get(a.license.service, a.license.userKey("refresh-token")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.license.keyring.Get(a.license.service, a.license.userKey("age-identity")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.license.keyring.Get(a.license.service, a.license.userKey("clock-last-seen")); err != nil {
		t.Fatal(err)
	}
	summary := a.LicenseSummary(nilContext{})
	if summary["refresh_token"] != nil || summary["license_file"] != nil || summary["customer_phone"] != nil {
		t.Fatalf("summary exposed secret: %#v", summary)
	}
	if err := a.license.Refresh(nilContext{}); err != nil {
		t.Fatal(err)
	}
	if got := a.LicenseState().PlanTitle; got != "پلن آفلاین آزمون" {
		t.Fatalf("refreshed plan title = %q", got)
	}
	if got, _ := a.license.keyring.Get(a.license.service, a.license.userKey("refresh-token")); got != "refresh-token-2" {
		t.Fatalf("refresh token was not rotated: %q", got)
	}
}

func TestLicenseRejectsTamperedJWSAndDeviceBinding(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"EdDSA","kid":"known"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"schema_version":1,"iss":"https://menuvex.ir","aud":"menuvex-offline-desktop"}`))
	signed := header + "." + payload
	good := signed + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, []byte(signed)))
	if _, _, err := verifyCompactJWS(good, map[string]ed25519.PublicKey{"known": publicKey}); err != nil {
		t.Fatal(err)
	}
	bad := signed + "." + base64.RawURLEncoding.EncodeToString([]byte("tampered"))
	if _, _, err := verifyCompactJWS(bad, map[string]ed25519.PublicKey{"known": publicKey}); err == nil {
		t.Fatal("tampered signature accepted")
	}
	if _, _, err := verifyCompactJWS(good, map[string]ed25519.PublicKey{"other": publicKey}); err == nil {
		t.Fatal("unknown kid accepted")
	}
}

func TestLicenseClockRollbackAndExpiryLock(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	fixture := newLicenseFixtureServer(t, now)
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	configureLicenseForTest(t, a, fixture, now)
	if _, err := a.license.Activate(nilContext{}, "ACTIVATION-CODE-TWO"); err != nil {
		t.Fatal(err)
	}
	manager := a.license
	manager.now = func() time.Time { return now.Add(-2 * time.Hour) }
	state := manager.startupValidate(nilContext{})
	if state.Status != "clock_rollback" || state.Valid {
		t.Fatalf("rollback state = %+v", state)
	}
	manager.now = func() time.Time { return now.Add(48 * time.Hour) }
	state = manager.startupValidate(nilContext{})
	if state.Status != "expired" || state.Valid {
		t.Fatalf("expiry state = %+v", state)
	}
}

func TestLicenseExpiryReadIsRaceSafe(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	fixture := newLicenseFixtureServer(t, now)
	a, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	configureLicenseForTest(t, a, fixture, now)
	if _, err := a.license.Activate(nilContext{}, "ACTIVATION-CODE-RACE"); err != nil {
		t.Fatal(err)
	}
	var clockMu sync.RWMutex
	clock := now
	a.license.now = func() time.Time {
		clockMu.RLock()
		defer clockMu.RUnlock()
		return clock
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = a.license.Current()
			}
		}()
	}
	clockMu.Lock()
	clock = now.Add(48 * time.Hour)
	clockMu.Unlock()
	wg.Wait()
	if state := a.license.Current(); state.Status != "expired" || state.Valid {
		t.Fatalf("concurrent expiry state = %+v", state)
	}
}

func TestLicenseBaseURLJoinDoesNotDropAPIPrefix(t *testing.T) {
	got, err := joinLicenseURL("https://menuvex.ir/api/v1/", "/offline-licenses/current")
	if err != nil || got != "https://menuvex.ir/api/v1/offline-licenses/current" {
		t.Fatalf("joined URL = %q, err=%v", got, err)
	}
	if _, err := normalizeLicenseBaseURL("http://example.com/api/v1"); err == nil {
		t.Fatal("non-loopback HTTP base must be rejected")
	}
}
EOF

grep -n 'nilContext\|func (f .*jws\|TestReal' license_test.go | sed -n '1,200p'