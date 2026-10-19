package desktop

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sinaqaffari/coffish/desktop/internal/store"
)

// ── لایسنس آفلاین ─────────────────────────────────────────────────────────────
// فایل license.enc کنار پایگاه داده ساخته می‌شود: payload خوانا + امضای HMAC
// با کلید محرمانه دستگاه (.license-secret). دستکاری فایل یا گذشتن تاریخ انقضا
// برنامه را قفل می‌کند.

type licensePayload struct {
	KeyHash   string `json:"key_hash"`
	Holder    string `json:"holder"`
	Plan      string `json:"plan"`
	IssuedAt  string `json:"issued_at"`
	ExpiresAt string `json:"expires_at"`
}

type licenseFile struct {
	Payload licensePayload `json:"payload"`
	Sig     string         `json:"sig"`
}

type LicenseState struct {
	Valid     bool
	Expired   bool
	Holder    string
	Plan      string
	DaysLeft  int
	ExpiresAt string
}

func (a *Application) licensePath() string {
	return filepath.Join(filepath.Dir(a.uploadDir), "license.enc")
}

func (a *Application) licenseSecret() ([]byte, error) {
	secretPath := filepath.Join(filepath.Dir(a.uploadDir), ".license-secret")
	if raw, err := os.ReadFile(secretPath); err == nil && len(raw) >= 32 {
		return raw, nil
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(secretPath), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(secretPath, secret, 0o600); err != nil {
		return nil, err
	}
	return secret, nil
}

func (a *Application) signLicense(payload licensePayload) (string, error) {
	secret, err := a.licenseSecret()
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(raw)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// LicenseState وضعیت فعلی لایسنس را از فایل امضاشده می‌خواند و اعتبارسنجی می‌کند.
func (a *Application) LicenseState() LicenseState {
	raw, err := os.ReadFile(a.licensePath())
	if err != nil {
		return LicenseState{}
	}
	var lf licenseFile
	if json.Unmarshal(raw, &lf) != nil {
		return LicenseState{}
	}
	expect, err := a.signLicense(lf.Payload)
	if err != nil || subtle.ConstantTimeCompare([]byte(expect), []byte(lf.Sig)) != 1 {
		return LicenseState{} // فایل دستکاری شده = بدون لایسنس
	}
	exp, err := time.Parse(time.RFC3339, lf.Payload.ExpiresAt)
	if err != nil {
		return LicenseState{}
	}
	st := LicenseState{Valid: true, Holder: lf.Payload.Holder, Plan: lf.Payload.Plan, ExpiresAt: lf.Payload.ExpiresAt}
	days := int(time.Until(exp).Hours() / 24)
	if days < 0 {
		days = 0
	}
	st.DaysLeft = days
	if time.Now().After(exp) {
		st.Expired = true
	}
	return st
}

// ActivateLicense لایسنس را فعال و فایل امضاشده license.enc را ایجاد می‌کند.
// حالت فعلی (Dummy): هر کلید با حداقل ۴ نویسه پذیرفته می‌شود؛ پسوند ‎-N‎ یعنی اشتراک N روزه.
func (a *Application) ActivateLicense(key, holder string) (LicenseState, error) {
	key = strings.TrimSpace(key)
	holder = strings.TrimSpace(holder)
	if len([]rune(key)) < 4 {
		return LicenseState{}, errors.New("کد لایسنس معتبر نیست؛ کد خریداری‌شده را کامل وارد کنید")
	}
	if holder == "" {
		holder = "کافه من"
	}
	days := 365
	if idx := strings.LastIndex(key, "-"); idx > 0 {
		if n, err := strconv.Atoi(strings.TrimSpace(key[idx+1:])); err == nil && n >= 7 && n <= 3650 {
			days = n
		}
	}
	now := time.Now()
	keyHash := sha256.Sum256([]byte(key))
	payload := licensePayload{
		KeyHash:   hex.EncodeToString(keyHash[:]),
		Holder:    holder,
		Plan:      "اشتراک آفلاین منووکس",
		IssuedAt:  now.Format(time.RFC3339),
		ExpiresAt: now.AddDate(0, 0, days).Format(time.RFC3339),
	}
	sig, err := a.signLicense(payload)
	if err != nil {
		return LicenseState{}, err
	}
	raw, err := json.MarshalIndent(licenseFile{Payload: payload, Sig: sig}, "", "  ")
	if err != nil {
		return LicenseState{}, err
	}
	if err := os.MkdirAll(filepath.Dir(a.licensePath()), 0o755); err != nil {
		return LicenseState{}, err
	}
	tmp := a.licensePath() + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return LicenseState{}, err
	}
	if err := os.Rename(tmp, a.licensePath()); err != nil {
		return LicenseState{}, err
	}
	return a.LicenseState(), nil
}

// ── نشست ورود (مدیر / پرسنل) ─────────────────────────────────────────────────

type sessionInfo struct {
	Role string // manager | staff
	Name string
}

// نشست‌ها روی دیسک می‌مانند تا با هر بار اجرای برنامه، کاربر مجبور به
// ورود دوباره نباشد؛ خروج صریح (logout) نشست را از دیسک هم پاک می‌کند.
func loadSessionsFile(path string) map[string]sessionInfo {
	out := map[string]sessionInfo{}
	raw, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	return out
}

func (a *Application) persistSessionsLocked() {
	if a.sessionsPath == "" {
		return
	}
	raw, err := json.Marshal(a.sessions)
	if err != nil {
		return
	}
	tmp := a.sessionsPath + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, a.sessionsPath)
	}
}

func (a *Application) newSession(w http.ResponseWriter, role, name string) {
	tokenBytes := make([]byte, 24)
	if _, err := rand.Read(tokenBytes); err != nil {
		return
	}
	token := hex.EncodeToString(tokenBytes)
	a.sessionsMu.Lock()
	if a.sessions == nil {
		a.sessions = map[string]sessionInfo{}
	}
	a.sessions[token] = sessionInfo{Role: role, Name: name}
	a.persistSessionsLocked()
	a.sessionsMu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name: "coffish_session", Value: token, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 30 * 24 * 3600,
	})
}

func (a *Application) sessionOf(r *http.Request) (sessionInfo, bool) {
	c, err := r.Cookie("coffish_session")
	if err != nil || c.Value == "" {
		return sessionInfo{}, false
	}
	a.sessionsMu.Lock()
	defer a.sessionsMu.Unlock()
	s, ok := a.sessions[c.Value]
	return s, ok
}

func (a *Application) dropSession(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("coffish_session"); err == nil {
		a.sessionsMu.Lock()
		delete(a.sessions, c.Value)
		a.persistSessionsLocked()
		a.sessionsMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: "coffish_session", Value: "", Path: "/", HttpOnly: true, MaxAge: -1})
}

// ── صفحات و گیت دسترسی ───────────────────────────────────────────────────────

type authPageData struct {
	CSRF    string
	Error   string
	Notice  string
	Expired bool
	License LicenseState
}

func (a *Application) renderAuth(w http.ResponseWriter, name string, data authPageData) {
	data.CSRF = a.csrf
	var buf bytes.Buffer
	if err := a.templates.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, "خطا در نمایش صفحه ورود", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

func authExemptPath(path string) bool {
	switch {
	case path == "/license", path == "/login", path == "/logout", path == "/license/activate":
		return true
	case strings.HasPrefix(path, "/static/"):
		return true
	}
	return false
}

// enforceAuth ترتیب گیت: اول لایسنس معتبر، بعد ورود کاربر. true یعنی پاسخ کامل شد.
func (a *Application) enforceAuth(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	lic := a.LicenseState()
	if path == "/license" {
		if lic.Valid && !lic.Expired {
			if _, ok := a.sessionOf(r); ok {
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return true
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return true
		}
		data := authPageData{License: lic, Expired: lic.Expired}
		if r.URL.Query().Get("expired") == "1" {
			data.Expired = true
		}
		a.renderAuth(w, "license", data)
		return true
	}
	if !lic.Valid {
		http.Redirect(w, r, "/license", http.StatusSeeOther)
		return true
	}
	if lic.Expired {
		http.Redirect(w, r, "/license?expired=1", http.StatusSeeOther)
		return true
	}
	if path == "/login" {
		if _, ok := a.sessionOf(r); ok {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return true
		}
		a.renderAuth(w, "login", authPageData{License: lic})
		return true
	}
	if _, ok := a.sessionOf(r); !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return true
	}
	return false
}

func (a *Application) handleLicenseActivate(w http.ResponseWriter, r *http.Request) {
	st, err := a.ActivateLicense(r.Form.Get("license_key"), r.Form.Get("holder"))
	if err != nil {
		a.renderAuth(w, "license", authPageData{Error: err.Error(), License: a.LicenseState()})
		return
	}
	_ = st
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (a *Application) handleLogin(w http.ResponseWriter, r *http.Request) {
	mode := strings.TrimSpace(r.Form.Get("mode"))
	phone := strings.TrimSpace(r.Form.Get("phone"))
	password := r.Form.Get("password")
	fail := func(msg string) {
		a.renderAuth(w, "login", authPageData{Error: msg, License: a.LicenseState()})
	}
	if len([]rune(phone)) < 10 || !strings.HasPrefix(phone, "09") {
		fail("شماره موبایل معتبر وارد کنید (با ۰۹ شروع می‌شود).")
		return
	}
	if len([]rune(password)) < 4 {
		fail("گذرواژه را وارد کنید.")
		return
	}
	ctx := r.Context()
	if mode == "staff" {
		staff, err := a.store.Entities(ctx, "staff")
		if err != nil {
			fail("خطا در خواندن فهرست پرسنل.")
			return
		}
		for _, e := range staff {
			if strings.TrimSpace(e.Field("phone")) != phone {
				continue
			}
			if e.Field("is_active") == "false" || strings.Contains(e.Field("status"), "غیرفعال") {
				fail("حساب این پرسنل غیرفعال است؛ با مدیر کافه تماس بگیرید.")
				return
			}
			if hash := e.Field("password_hash"); hash != "" {
				sum := sha256.Sum256([]byte(password))
				if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(hash)) != 1 {
					fail("گذرواژه نادرست است.")
					return
				}
			}
			// حالت Dummy: اگر برای پرسنل گذرواژه‌ای ثبت نشده، هر گذرواژه ۴+ نویسه‌ای پذیرفته می‌شود.
			a.newSession(w, "staff", e.Label())
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		fail("پرسنلی با این شماره موبایل یافت نشد.")
		return
	}
	// ورود مدیر — فعلاً Dummy: هر موبایل معتبر با گذرواژه ۴+ نویسه‌ای.
	a.newSession(w, "manager", "مدیر کافه")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *Application) handleLogout(w http.ResponseWriter, r *http.Request) {
	a.dropSession(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

var _ = fmt.Sprintf
var _ = store.FormatPersianNumber
