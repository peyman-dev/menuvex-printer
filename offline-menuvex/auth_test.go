package desktop

import (
	"context"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLicenseAndLoginGate چرخه کامل: بدون لایسنس قفل، فعال‌سازی Dummy، ورود مدیر، انقضا = قفل با شماره پشتیبانی.
func TestLicenseAndLoginGate(t *testing.T) {
	dir := t.TempDir()
	a, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	handler := a.Handler()
	do := func(method, path string, form url.Values, cookie string) (int, string, string) {
		var body *strings.Reader
		if form != nil {
			body = strings.NewReader(form.Encode())
		} else {
			body = strings.NewReader("")
		}
		r := httptest.NewRequest(method, "http://localhost"+path, body)
		if form != nil {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		if cookie != "" {
			r.Header.Set("Cookie", cookie)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code, w.Body.String(), w.Header().Get("Set-Cookie")
	}
	// ۱) بدون لایسنس: صفحه اصلی به فعال‌سازی می‌رود.
	if code, _, _ := do("GET", "/", nil, ""); code != 200 && code != 303 && code != 302 {
		t.Fatalf("no-license home -> %d", code)
	}
	if code, body, _ := do("GET", "/license", nil, ""); code != 200 || !strings.Contains(body, "لایسنس خریداری‌شده را وارد کنید") {
		t.Fatalf("license page -> %d", code)
	}
	if _, body, _ := do("GET", "/license", nil, ""); !strings.Contains(body, "0910") || !strings.Contains(body, "غفاری") {
		t.Fatal("license page must show support contact (09101486607 آقای غفاری)")
	}
	// ۲) فعال‌سازی با کلید Dummy: فایل رمزنگاری‌شده/امضاشده ساخته می‌شود.
	if _, err := a.ActivateLicense("DUMMY", "کافه تست"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "license.enc"))
	if err != nil {
		t.Fatalf("license.enc not created: %v", err)
	}
	if strings.Contains(string(raw), "\"DUMMY\"") || !strings.Contains(string(raw), "expires_at") {
		t.Fatal("license.enc must contain signed payload with expiry, not the raw key")
	}
	if st := a.LicenseState(); !st.Valid || st.Expired || st.DaysLeft < 300 {
		t.Fatalf("license state after dummy activation: %+v", st)
	}
	// ۳) بدون ورود: صفحه اصلی به ورود می‌رود؛ ورود مدیر Dummy نشست می‌سازد.
	if code, _, _ := do("GET", "/", nil, ""); code == 200 {
		// رندر مستقیم صفحه ورود هم قابل قبول است اگر ۲۰۰ باشد؛ وجود فرم را بررسی کن
		if _, body, _ := do("GET", "/login", nil, ""); !strings.Contains(body, "ورود به حساب کاربری") {
			t.Fatal("expected login page")
		}
	}
	code, _, setCookie := do("POST", "/login", url.Values{
		"csrf": {a.csrf}, "mode": {"manager"}, "phone": {"09120001122"}, "password": {"dummy-pass"},
	}, "")
	if code != 303 && code != 302 && code != 200 {
		t.Fatalf("manager login -> %d", code)
	}
	if !strings.Contains(setCookie, "coffish_session") {
		// ممکن است ریدایرکت بدون کوکی برگردد اگر فرم رد شده؛ متن را بررسی کن
		t.Fatalf("login must set session cookie, got %q", setCookie)
	}
	cookie := strings.Split(setCookie, ";")[0]
	if code, body, _ := do("GET", "/", nil, cookie); code != 200 || !strings.Contains(body, "خلاصه وضعیت کافه") {
		t.Fatalf("authed home -> %d", code)
	}
	// ۴) انقضای لایسنس: بازنویسی فایل با انقضای گذشته و امضای معتبر → قفل با پیام تمدید.
	past := time.Now().AddDate(0, 0, -2).Format(time.RFC3339)
	payload := licensePayload{KeyHash: "x", Holder: "کافه تست", Plan: "اشتراک آفلاین منووکس", IssuedAt: time.Now().AddDate(-1, 0, 0).Format(time.RFC3339), ExpiresAt: past}
	sig, err := a.signLicense(payload)
	if err != nil {
		t.Fatal(err)
	}
	content := `{"payload":{"key_hash":"x","holder":"کافه تست","plan":"اشتراک آفلاین منووکس","issued_at":"` + payload.IssuedAt + `","expires_at":"` + past + `"},"sig":"` + sig + `"}`
	if err := os.WriteFile(filepath.Join(dir, "license.enc"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if st := a.LicenseState(); !st.Expired {
		t.Fatal("expired license must be detected")
	}
	if _, body, _ := do("GET", "/license?expired=1", nil, cookie); !strings.Contains(body, "اشتراک شما به پایان رسیده است") {
		t.Fatal("expired lock screen must ask for renewal")
	}
	// ۵) دستکاری فایل لایسنس = نامعتبر
	if err := os.WriteFile(filepath.Join(dir, "license.enc"), []byte(`{"payload":{"key_hash":"y","holder":"x","plan":"p","issued_at":"x","expires_at":"2999-01-01T00:00:00Z"},"sig":"deadbeef"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if st := a.LicenseState(); st.Valid {
		t.Fatal("tampered license file must be rejected")
	}
}

// TestPOSHasNoRecentInvoices سفارش‌های اخیر نباید داخل صفحه صندوق نمایش داده شوند.
func TestPOSHasNoRecentInvoices(t *testing.T) {
	a := testApp(t)
	if _, err := a.store.SaveLocalRecord(context.Background(), "product", "", map[string]any{"title": "لاته تست صندوق", "price": "250000"}); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "http://localhost/?page=pos", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("pos -> %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "آخرین فاکتورهای صادرشده") {
		t.Fatal("recent invoices table must not be rendered inside POS")
	}
	body := w.Body.String()
	for _, want := range []string{"pos-layout", "انتخاب مشتری یا گروه", "جزئیات سفارش", "sidebar-toggle"} {
		if want == "sidebar-toggle" {
			continue // تولگل در شل اصلی است، نه محتوای POS
		}
		if !strings.Contains(body, want) {
			t.Fatalf("POS missing %q", want)
		}
	}
	// صندوق وب: کلیک روی کل کارت محصول باید همان افزودن به سبد باشد، نه فقط دکمه +/−
	for _, want := range []string{"pos-tile-card", "posTileAdd(this)", "posTileKey", "pos-cart-empty", "pos-submit", "مبلغ نهایی", "جمع سبد"} {
		if !strings.Contains(body, want) {
			t.Fatalf("POS missing web-parity piece %q", want)
		}
	}
	// هیچ بلوک داشبورد/فاکتور نباید داخل صندوق باشد
	for _, banned := range []string{"فروش کل ثبت‌شده در صندوق", "kpi-grid", "dash-kpis", "آخرین سفارش‌ها"} {
		if strings.Contains(body, banned) {
			t.Fatalf("POS must not contain dashboard block %q", banned)
		}
	}
	home := httptest.NewRequest("GET", "http://localhost/", nil)
	hw := httptest.NewRecorder()
	a.Handler().ServeHTTP(hw, home)
	if !strings.Contains(hw.Body.String(), "sidebar-toggle") || !strings.Contains(hw.Body.String(), "nav-tooltip") {
		t.Fatal("shell must include the sidebar toggle and tooltip behavior")
	}
	// داشبورد فقط بلوک جدید وب را دارد؛ بلوک قدیمی KPI و جدول «آخرین فاکتورها» حذف شده است
	homeBody := hw.Body.String()
	if !strings.Contains(homeBody, "خلاصه وضعیت کافه") {
		t.Fatal("home must render the new web dashboard")
	}
	for _, banned := range []string{"فروش کل ثبت‌شده در صندوق", "آخرین فاکتورهای صادرشده در صندوق", "kpi-grid"} {
		if strings.Contains(homeBody, banned) {
			t.Fatalf("home must not render legacy block %q", banned)
		}
	}
	// صفحه مدیریت انبار و دیالوگ ثبت تغییر موجودی باید رندر شوند
	for page, want := range map[string]string{
		"ingredients": "ثبت تغییر موجودی",
	} {
		req := httptest.NewRequest("GET", "http://localhost/?page="+page, nil)
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, req)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("page %s must render %q (code %d)", page, want, rec.Code)
		}
	}
	// سایدبار باید به بالا و راست بچسبد: ریل sticky با قفل ارتفاع نما در CSS ایستا
	cssReq := httptest.NewRequest("GET", "http://localhost/static/app.css", nil)
	cssRec := httptest.NewRecorder()
	a.Handler().ServeHTTP(cssRec, cssReq)
	css := cssRec.Body.String()
	for _, want := range []string{".sidebar-rail", "position: sticky", "height: 100dvh", "top: 0"} {
		if !strings.Contains(css, want) {
			t.Fatalf("app.css missing sticky-sidebar rule %q", want)
		}
	}
}
