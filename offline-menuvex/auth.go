package desktop

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/sinaqaffari/coffish/desktop/internal/store"
)

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
	// Phone شمارهٔ ثبت‌شدهٔ مالک برای پیش‌پرکردن فرم ورود.
	Phone string
	// Registered یعنی حساب مالک قبلاً ثبت شده است (ثبت‌نام دوباره ممکن نیست).
	Registered bool
}

func isValidPhone(phone string) bool {
	if len(phone) != 11 || !strings.HasPrefix(phone, "09") {
		return false
	}
	for _, ch := range phone {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
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
	case path == "/license", path == "/license/status", path == "/license/refresh", path == "/login", path == "/logout", path == "/license/activate", path == "/register":
		return true
	case path == "/api/v1/license":
		return true
	case strings.HasPrefix(path, "/static/"):
		return true
	}
	return false
}

func licenseRecoveryPath(path, method string) bool {
	if method == http.MethodGet {
		switch path {
		case "/backup.json", "/local/products-export", "/local/inventory-export", "/local/debtors-export":
			return true
		}
	}
	return method == http.MethodPost && path == "/local/export-backup"
}

// enforceAuth ترتیب گیت: ابتدا وضعیت verified لایسنس و سپس ورود کاربر.
// صفحهٔ وضعیت و خروجی/پشتیبان‌گیری در حالت lock باقی می‌مانند؛ هیچ داده‌ای
// برای expired/revoked پاک نمی‌شود.
func (a *Application) enforceAuth(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	lic := a.LicenseState()
	if path == "/license/status" {
		a.renderAuth(w, "license-status", authPageData{License: lic, Notice: r.URL.Query().Get("refreshed")})
		return true
	}
	if path == "/license" {
		if lic.CanMutate() {
			if _, ok := a.sessionOf(r); ok {
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return true
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return true
		}
		a.renderAuth(w, "license", authPageData{License: lic, Expired: lic.Status == "expired" || lic.Status == "revoked"})
		return true
	}
	if path == "/license/refresh" {
		return false
	}
	// Authentication and one-time registration remain reachable while the
	// license is locked so an operator can still access backup/export recovery.
	if !lic.CanMutate() && path != "/login" && path != "/register" {
		if licenseRecoveryPath(path, r.Method) {
			if _, ok := a.sessionOf(r); ok {
				return false
			}
		}
		http.Redirect(w, r, "/license", http.StatusSeeOther)
		return true
	}
	if path == "/register" {
		if a.ownerRegistered(r.Context()) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return true
		}
		if r.Method == http.MethodGet {
			a.renderAuth(w, "register", authPageData{License: lic})
			return true
		}
		return false
	}
	if path == "/login" {
		if _, ok := a.sessionOf(r); ok {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return true
		}
		owner, registered := a.ownerAccount(r.Context())
		if !registered {
			http.Redirect(w, r, "/register", http.StatusSeeOther)
			return true
		}
		a.renderAuth(w, "login", authPageData{License: lic, Phone: owner.Phone, Registered: true})
		return true
	}
	if _, ok := a.sessionOf(r); !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return true
	}
	return false
}

func (a *Application) handleLicenseActivate(w http.ResponseWriter, r *http.Request) {
	if a.license == nil {
		a.renderAuth(w, "license", authPageData{Error: "سرویس فعال‌سازی لایسنس در دسترس نیست", License: a.LicenseState()})
		return
	}
	code := strings.TrimSpace(r.Form.Get("activation_code"))
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	_, err := a.license.Activate(ctx, code)
	if err != nil {
		a.renderAuth(w, "license", authPageData{Error: refreshErrorText(err), License: a.LicenseState()})
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (a *Application) handleLicenseRefresh(w http.ResponseWriter, r *http.Request) {
	if a.license == nil {
		a.renderAuth(w, "license", authPageData{Error: "سرویس تمدید لایسنس در دسترس نیست", License: a.LicenseState()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := a.license.Refresh(ctx); err != nil {
		a.renderAuth(w, "license", authPageData{Error: refreshErrorText(err), License: a.LicenseState()})
		return
	}
	http.Redirect(w, r, "/license/status?refreshed=1", http.StatusSeeOther)
}

func (a *Application) handleLicenseStatus(w http.ResponseWriter, r *http.Request) {
	a.renderAuth(w, "license-status", authPageData{License: a.LicenseState(), Notice: r.URL.Query().Get("refreshed")})
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
			hash := strings.TrimSpace(e.Field("password_hash"))
			if hash == "" {
				fail("برای این پرسنل هنوز گذرواژه ثبت نشده است؛ با مدیر کافه تماس بگیرید.")
				return
			}
			sum := sha256.Sum256([]byte(password))
			if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(hash)) != 1 {
				fail("گذرواژه نادرست است.")
				return
			}
			a.newSession(w, "staff", e.Label())
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		fail("پرسنلی با این شماره موبایل یافت نشد.")
		return
	}
	// ورود مدیر: فقط حساب مالکی که با «ثبت‌نام» ساخته شده پذیرفته می‌شود.
	owner, ok := a.ownerAccount(ctx)
	if !ok {
		fail("هنوز حسابی ثبت نشده است؛ ابتدا از صفحهٔ ثبت‌نام حساب بسازید.")
		return
	}
	if phone != owner.Phone || subtle.ConstantTimeCompare([]byte(hashPassword(password)), []byte(owner.PasswordHash)) != 1 {
		fail("شماره موبایل یا گذرواژه نادرست است.")
		return
	}
	name := strings.TrimSpace(owner.FirstName + " " + owner.LastName)
	if name == "" {
		name = "مدیر کافه"
	}
	a.newSession(w, "manager", name)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// ownerAccount حساب مالک ثبت‌نامی روی پروفایل کافه (cafe-main-profile) ذخیره می‌شود (مطابق اسکیمای وب account).
type ownerAccount struct {
	FirstName    string
	LastName     string
	Phone        string
	CafeName     string
	PasswordHash string
}

func hashPassword(pw string) string {
	sum := sha256.Sum256([]byte(pw))
	return fmt.Sprintf("%x", sum)
}

func (a *Application) ownerAccount(ctx context.Context) (ownerAccount, bool) {
	prof, err := a.store.Entity(ctx, "cafe_profile", "cafe-main-profile")
	if err != nil {
		return ownerAccount{}, false
	}
	phone := strings.TrimSpace(prof.Field("owner_phone"))
	hash := strings.TrimSpace(prof.Field("owner_password_hash"))
	if phone == "" || hash == "" {
		return ownerAccount{}, false
	}
	return ownerAccount{
		FirstName:    strings.TrimSpace(prof.Field("owner_first_name")),
		LastName:     strings.TrimSpace(prof.Field("owner_last_name")),
		Phone:        phone,
		CafeName:     strings.TrimSpace(prof.Field("name")),
		PasswordHash: hash,
	}, true
}

// ownerRegistered آیا ثبت‌نام مالک قبلاً انجام شده است؟ (ثبت‌نام فقط یک بار انجام می‌شود.)
func (a *Application) ownerRegistered(ctx context.Context) bool {
	_, ok := a.ownerAccount(ctx)
	return ok
}

func (a *Application) renderRegister(w http.ResponseWriter, r *http.Request, msg string) {
	a.renderAuth(w, "register", authPageData{Error: msg, License: a.LicenseState()})
}

// handleRegister ثبت‌نام یک‌باره مالک (مطابق اسکیمای وب: account + tenant) و ورود خودکار پس از آن.
func (a *Application) handleRegister(w http.ResponseWriter, r *http.Request) {
	if a.ownerRegistered(r.Context()) {
		// ثبت‌نام دوم ممکن نیست.
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	first := strings.TrimSpace(r.Form.Get("first_name"))
	last := strings.TrimSpace(r.Form.Get("last_name"))
	phone := strings.TrimSpace(r.Form.Get("phone"))
	cafe := strings.TrimSpace(r.Form.Get("cafe_name"))
	address := strings.TrimSpace(r.Form.Get("address"))
	pw := r.Form.Get("password")
	confirm := r.Form.Get("password_confirm")
	fail := func(msg string) { a.renderRegister(w, r, msg) }
	switch {
	case first == "" || last == "":
		fail("نام و نام خانوادگی را وارد کنید.")
		return
	case !isValidPhone(phone):
		fail("شماره موبایل معتبر نیست. مثال: 09123456789")
		return
	case cafe == "":
		fail("نام کافه یا مجموعه را وارد کنید.")
		return
	case len(pw) < 6:
		fail("گذرواژه باید حداقل ۶ نویسه باشد.")
		return
	case pw != confirm:
		fail("تکرار گذرواژه با گذرواژه یکسان نیست.")
		return
	}
	now := time.Now()
	fields := map[string]any{
		"owner_first_name":    first,
		"owner_last_name":     last,
		"owner_phone":         phone,
		"owner_name":          strings.TrimSpace(first + " " + last),
		"owner_role":          "owner",
		"owner_password_hash": hashPassword(pw),
		"owner_registered_at": now.Format(time.RFC3339),
		"name":                cafe,
		"phone":               phone,
	}
	if address != "" {
		fields["address"] = address
	}
	if _, err := a.store.SaveLocalRecord(r.Context(), "cafe_profile", "cafe-main-profile", fields); err != nil {
		fail("خطا در ذخیره ثبت‌نام: " + err.Error())
		return
	}
	// ورود خودکار بلافاصله بعد از ثبت‌نام (مثل نسخه وب).
	a.newSession(w, "manager", strings.TrimSpace(first+" "+last))
	http.Redirect(w, r, "/?page=pos&msg="+url.QueryEscape("ثبت‌نام با موفقیت انجام شد؛ خوش آمدید."), http.StatusSeeOther)
}

func (a *Application) handleLogout(w http.ResponseWriter, r *http.Request) {
	a.dropSession(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

var _ = fmt.Sprintf
var _ = store.FormatPersianNumber
