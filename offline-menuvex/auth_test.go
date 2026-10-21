package desktop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLicenseAndLoginGate verifies that the old local HMAC/Dummy activation path
// is gone and that a fresh install fails closed until a real backend response is
// received. The test intentionally does not contact production.
func TestLicenseAndLoginGate(t *testing.T) {
	for _, key := range []string{"COFFISH_OFFLINE_LICENSES_ENABLED", "COFFISH_LICENSE_PUBLIC_KEYS", "COFFISH_LICENSE_API_BASE_URL", "API_BASE_URL"} {
		t.Setenv(key, "")
	}
	dir := t.TempDir()
	a, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err := os.Stat(filepath.Join(dir, ".license-secret")); !os.IsNotExist(err) {
		t.Fatal("legacy HMAC license secret must not be created")
	}
	handler := a.Handler()
	do := func(method, path string, form url.Values) (int, string) {
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
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	if code, body := do("GET", "/license", nil); code != 200 || !strings.Contains(body, "کد فعال‌سازی") {
		t.Fatalf("license page -> %d", code)
	}
	if _, body := do("GET", "/license", nil); !strings.Contains(body, "https://menuvex.ir/pricing#plans") || !strings.Contains(body, "tel:+989101486607") {
		t.Fatal("license page must show approved renewal/support links")
	}
	if code, body := do("POST", "/license/activate", url.Values{"csrf": {a.csrf}, "activation_code": {"DUMMY"}}); code != 200 || !strings.Contains(body, "سرویس فعال‌سازی") {
		t.Fatalf("disabled activation must fail closed: %d %s", code, body)
	}
	var summary map[string]any
	r := httptest.NewRequest("GET", "http://localhost/api/v1/license", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK || json.NewDecoder(w.Body).Decode(&summary) != nil {
		t.Fatalf("local license summary -> %d %s", w.Code, w.Body.String())
	}
	if summary["status"] != "not_activated" || summary["refresh_token"] != nil || summary["license_file"] != nil {
		t.Fatalf("unsafe or unexpected license summary: %#v", summary)
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
