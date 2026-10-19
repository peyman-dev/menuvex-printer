package desktop

import (
	"context"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func renderPage(t *testing.T, a *Application, path string) string {
	t.Helper()
	r := httptest.NewRequest("GET", "http://localhost"+path, nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("GET %s -> %d", path, w.Code)
	}
	return w.Body.String()
}

// TestPrinterDialogUsesCustomSelects دیالوگ پرینتر نباید سلکت بومی داشته باشد.
func TestPrinterDialogUsesCustomSelects(t *testing.T) {
	a := testApp(t)
	body := renderPage(t, a, "/?page=printers")
	if strings.Contains(body, "data-native-select") {
		t.Fatal("printer dialog must not opt out of the custom ui-select")
	}
	if !strings.Contains(body, `id="pf-role"`) || !strings.Contains(body, "syncUiSelects(document);") {
		t.Fatal("printer dialog selects must be enhanced and synced after pfFill")
	}
}

// TestPOSHasNoPaymentToggle صندوق انتخاب روش پرداخت ندارد؛ پیش‌فرض کارتخوان است
// و نسیه فقط از «بعداً پرداخت می‌کند» (با مشخصات مشتری) ممکن است.
func TestPOSHasNoPaymentToggle(t *testing.T) {
	a := testApp(t)
	body := renderPage(t, a, "/?page=pos")
	for _, banned := range []string{"pos-payseg", "pos-pay-cash", "pos-pay-credit", "اعتباری (نسیه)</button>"} {
		if strings.Contains(body, banned) {
			t.Fatalf("POS must not render payment toggle piece %q", banned)
		}
	}
	if !strings.Contains(body, `id="pos-payment-method"`) || !strings.Contains(body, `<option value="card_pos" selected>`) {
		t.Fatal("POS payment must default to card_pos")
	}
	if !strings.Contains(body, "بعداً پرداخت می‌کنند (ثبت در بدهکاران)") {
		t.Fatal("deferred (debtor) path with customer details must stay")
	}
	// کارت پرینتر در منوی صندوق: نقش و وضعیت اتصال دیده می‌شود.
	if _, err := a.store.SaveLocalRecord(context.Background(), "printer", "", map[string]any{
		"name": "پرینتر صندوق تست", "role": "cashier", "station": "صندوق اصلی",
		"paper_width": "80mm", "last_check_state": "ok", "is_active": true,
	}); err != nil {
		t.Fatal(err)
	}
	body = renderPage(t, a, "/?page=pos")
	for _, want := range []string{"pos-printer-card", "pos-printer-role", "متصل", "صندوق", "مرکز چاپگرها و تنظیمات"} {
		if !strings.Contains(body, want) {
			t.Fatalf("POS printer dropdown missing %q", want)
		}
	}
}

// TestPrintQueueDeleteAndClear صف چاپ باید حذف تکی و پاک‌کردن سوابق تمام‌شده را داشته باشد.
func TestPrintQueueDeleteAndClear(t *testing.T) {
	a := testApp(t)
	body := renderPage(t, a, "/?page=print-queue")
	for _, want := range []string{"/local/print-queue-clear", "پاک‌کردن سوابق تمام‌شده"} {
		if !strings.Contains(body, want) {
			t.Fatalf("print queue page missing %q", want)
		}
	}
	ctx := context.Background()
	if _, err := a.store.SaveLocalRecord(ctx, "print_job", "job-done-1", map[string]any{"status": "printed", "doc_type": "customer"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.SaveLocalRecord(ctx, "print_job", "job-pending-1", map[string]any{"status": "pending", "doc_type": "customer"}); err != nil {
		t.Fatal(err)
	}
	body = renderPage(t, a, "/?page=print-queue")
	if !strings.Contains(body, "/local/print-queue-delete") {
		t.Fatal("queued jobs must have a delete action")
	}
	// حذف تکی Job تمام‌شده
	w := postRaw(t, a, "/local/print-queue-delete", url.Values{"job_id": {"job-done-1"}})
	if w.Code != 303 && w.Code != 302 && w.Code != 200 {
		t.Fatalf("delete job -> %d", w.Code)
	}
	if _, err := a.store.Entity(ctx, "print_job", "job-done-1"); err == nil {
		t.Fatal("deleted job must be gone")
	}
	// پاک‌کردن گروهی نباید Job در صف را حذف کند.
	if _, err := a.store.SaveLocalRecord(ctx, "print_job", "job-failed-1", map[string]any{"status": "failed", "doc_type": "kitchen"}); err != nil {
		t.Fatal(err)
	}
	w = postRaw(t, a, "/local/print-queue-clear", url.Values{})
	if w.Code != 303 && w.Code != 302 && w.Code != 200 {
		t.Fatalf("clear queue -> %d", w.Code)
	}
	if _, err := a.store.Entity(ctx, "print_job", "job-failed-1"); err == nil {
		t.Fatal("terminal job must be cleared")
	}
	if _, err := a.store.Entity(ctx, "print_job", "job-pending-1"); err != nil {
		t.Fatal("pending job must survive clear")
	}
}

// TestRegisterTemplateExists قالب ثبت‌نام مطابق اسکیمای وب رندر می‌شود.
func TestRegisterTemplateExists(t *testing.T) {
	a := testApp(t)
	if tmpl := a.templates.Lookup("register"); tmpl == nil {
		t.Fatal("register template missing")
	}
	var buf strings.Builder
	if err := a.templates.Lookup("register").Execute(&buf, authPageData{CSRF: "x"}); err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	for _, want := range []string{"ایجاد حساب کاربری", `name="first_name"`, `name="last_name"`, `name="phone"`, `name="cafe_name"`, `name="password"`, `name="password_confirm"`, "۱۵ روز اشتراک کامل هدیه", "مدیریت حرفه‌ای", "btn btn-primary btn-lg btn-block"} {
		if !strings.Contains(body, want) {
			t.Fatalf("register page missing %q", want)
		}
	}
}
