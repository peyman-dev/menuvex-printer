package desktop

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sinaqaffari/coffish/desktop/internal/store"
)

// TestDebtorKindFromPOS فلو «بعداً پرداخت می‌کند» را قفل می‌کند: سفارش
// credit_debtor با نوع حساب شرکتی/گروه باید پرونده بدهکار با account_type
// درست، شناسه گروه و سررسید بسازد؛ بدون نوع، پیش‌فرض «شخصی» است.
func TestDebtorKindFromPOS(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	p1 := posProduct(t, a, "لاته", "120000")

	id, err := a.store.CreatePOSOrder(ctx, store.POSOrderInput{
		ClientToken:   "defer-co-1",
		PaymentMethod: "credit_debtor",
		CustomerName:  "شرکت نمونه",
		DebtorKind:    "company",
		DebtorGroupID: "grp-42",
		DebtorDueAt:   "2026-11-01",
		DebtorNote:    "تسویه ماهانه",
		Items:         []store.POSOrderLineInput{{ProductID: p1, Quantity: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("empty order id")
	}
	debtors, _ := a.store.Entities(ctx, "debtor")
	if len(debtors) != 1 {
		t.Fatalf("expected 1 debtor, got %d", len(debtors))
	}
	d := debtors[0]
	if d.Field("account_type") != "company" || d.Field("group_id") != "grp-42" {
		t.Fatalf("company debtor wrong: kind=%q group=%q", d.Field("account_type"), d.Field("group_id"))
	}
	if d.Field("due_at") != "2026-11-01" || d.Field("full_name") != "شرکت نمونه" || d.Field("remaining") != "120000" {
		t.Fatalf("debtor fields wrong: due=%q name=%q remaining=%q", d.Field("due_at"), d.Field("full_name"), d.Field("remaining"))
	}
	if d.Field("order_id") == "" {
		t.Fatal("debtor must link to the order")
	}

	// حالت شخصی: بدون debtor_kind صریح
	if _, err := a.store.CreatePOSOrder(ctx, store.POSOrderInput{
		ClientToken:   "defer-personal-1",
		PaymentMethod: "credit_debtor",
		CustomerName:  "علی رضایی",
		CustomerPhone: "09123456789",
		Items:         []store.POSOrderLineInput{{ProductID: p1, Quantity: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	debtors, _ = a.store.Entities(ctx, "debtor")
	found := false
	for _, d := range debtors {
		if d.Field("full_name") == "علی رضایی" {
			found = true
			if d.Field("account_type") != "personal" {
				t.Fatalf("personal debtor kind wrong: %q", d.Field("account_type"))
			}
			if d.Field("phone") != "09123456789" {
				t.Fatalf("personal debtor phone wrong: %q", d.Field("phone"))
			}
		}
	}
	if !found {
		t.Fatal("personal debtor not created")
	}
}

// TestDialogOpenClassAndResponsiveFixes رگرسیون باگ «دیالوگ‌ها باز
// نمی‌شوند» (helperها فقط hidden را عوض می‌کردند و کلاس .open لازم بود)،
// هندلر عمومی دراپ‌داون «پرینترها»، دیالوگ بدهکاران مثل وب، اسکرول
// .main-view و تصویر مربع/گرید ریسپانسیو کارت محصول را قفل می‌کند.
func TestDialogOpenClassAndResponsiveFixes(t *testing.T) {
	a := testApp(t)
	get := func(path string) string {
		r := httptest.NewRequest(http.MethodGet, "http://localhost"+path, nil)
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s -> %d", path, w.Code)
		}
		return w.Body.String()
	}
	body := get("/?page=pos")
	for _, want := range []string{
		"ثبت در لیست بدهکاران (بعداً پرداخت)",        // عنوان دیالوگ عین وب
		"نام و شماره موبایل مشتری را وارد کنید",       // زیرعنوان وب
		"مشتری شخصی", "حساب شرکتی / گروه",             // تب‌های نوع حساب
		"انتخاب تاریخ…",                               // چیپ سررسید سفارشی
		`id="pos-defer-submit"`,                       // دکمه ثبت با مبلغ داینامیک
		`id="pos-defer-kind"`, `id="pos-defer-group-id"`,
		"classList.add('open')",                       // فیکس بازشدن دیالوگ‌ها
		"posCloseDlg(this.id)",                        // بستن با کلیک روی پس‌زمینه
		"__ddToggleInstalled",                         // هندلر دراپ‌داون پرینترها
		"posOpenDlg('dlg-stages')",                    // دکمه مراحل سفارش
		`data-days="custom"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("pos page missing %q", want)
		}
	}

	// CSS: اسکرول نما، تصویر مربع تمام‌عرض و breakpointهای گرید محصول
	css := get("/static/app.css")
	for _, want := range []string{
		"overflow-y: auto",       // داشبورد/پرسنل اسکرول می‌شوند
		"aspect-ratio: 1 / 1",    // تصویر کارت محصول مربع تمام‌عرض (وب)
		"minmax(min(100%, 152px), 1fr)", // ≥۱۴۴۰px: ۴ تا ۵ ستون در مانیتور کاربر
		"minmax(min(100%, 128px), 1fr)", // موبایل کوچک
		".pos-defer-kind",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("app.css missing %q", want)
		}
	}
	if strings.Contains(css, "max-height: 144px") {
		t.Fatal("product photo must not be capped at 144px anymore")
	}
}
