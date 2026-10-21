package desktop

import (
	"context"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestJalaliShamsiConversions تقویم شمسی را قفل می‌کند: تبدیل دوطرفه میلادی و
// شمسی و خواندن تاریخ انتخاب‌شده در DatePicker (۱۴۰۵/۰۷/۲۶).
func TestJalaliShamsiConversions(t *testing.T) {
	jy, jm, jd := GregorianToJalali(2026, 10, 20)
	if jy != 1405 || jm != 7 || jd != 28 {
		t.Fatalf("2026-10-20 must be 1405/07/28, got %d/%d/%d", jy, jm, jd)
	}
	gy, gm, gd := JalaliToGregorian(1405, 7, 26)
	if gy != 2026 || gm != 10 || gd != 18 {
		t.Fatalf("1405/07/26 must be 2026-10-18, got %d-%d-%d", gy, gm, gd)
	}
	// رفت و برگشت روی سال‌های مختلف نباید یک روز جابه‌جا شود.
	for _, date := range [][3]int{{2024, 3, 20}, {2025, 1, 1}, {2026, 6, 15}, {2027, 12, 31}, {2030, 2, 28}} {
		jy, jm, jd := GregorianToJalali(date[0], date[1], date[2])
		gy, gm, gd := JalaliToGregorian(jy, jm, jd)
		if gy != date[0] || gm != date[1] || gd != date[2] {
			t.Fatalf("round trip failed for %v: got %d-%d-%d via %d/%d/%d", date, gy, gm, gd, jy, jm, jd)
		}
	}
	tm, ok := ParseJalali("۱۴۰۵/۰۷/۲۶")
	if !ok || tm.Format("2006-01-02") != "2026-10-18" {
		t.Fatalf("ParseJalali(۱۴۰۵/۰۷/۲۶) = %v/%v", tm, ok)
	}
	if _, ok := ParseJalali("۱۴۰۴/۱۲/۳۱"); ok {
		t.Fatal("۱۴۰۴/۱۲/۳۱ is not a real day and must be rejected")
	}
	if got := JalaliISOFromShamsi("۱۴۰۵/۰۷/۲۶"); got != "2026-10-18" {
		t.Fatalf("JalaliISOFromShamsi = %q", got)
	}
	if got := ShamsiFromISO("2026-10-18"); got != "۱۴۰۵/۰۷/۲۶" {
		t.Fatalf("ShamsiFromISO = %q", got)
	}
}

// TestDebtorsPageMatchesWebDesign ساختار صفحه وب بدهکاران را قفل می‌کند:
// سربرگ + تب‌ها + کارت‌های خلاصه + نوار فیلتر با تقویم شمسی + ستون مستندات
// سفارش + دیالوگ ثبت بدهی با جستجوی محصول.
func TestDebtorsPageMatchesWebDesign(t *testing.T) {
	a := testApp(t)
	body := ordersGet(t, a, "/?page=debtors")
	for _, want := range []string{
		"لیست بدهکاران، مستندات سفارش‌ها و خرید اعتباری",
		"ثبت بدهی دستی",
		"فهرست بدهکاران و مستندات سفارش‌ها",
		"گزارش بدهی گروه‌ها (سازمانی)",
		"مانده بدهی",
		"بدهکاران باز",
		"سررسید گذشته",
		"وصول این ماه",
		"همه وضعیت‌ها",
		"همه منابع ثبت",
		"همه حساب‌ها",
		"همه گروه‌ها",
		"مستندات سفارش (سفارش چی بود؟)",
		"تاریخ و ساعت ثبت",
		"بدهکاری مطابق این فیلتر پیدا نشد.",
		"خروجی CSV",
		"ثبت بدهکار جدید",
		"ثبت با نام محصولات سفارش",
		"فقط مبلغ کل بدهی",
		"جمع کل:",
		"سررسید پرداخت (اختیاری)",
		"data-jdp",
		"dbk-products",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("debtors page missing %q", want)
		}
	}
	// فرم قدیمی و بدون مستندات سفارش نباید برگردد.
	for _, gone := range []string{"ثبت حساب دفتری / نسیه جدید", "مبلغ بدهی (ریال)"} {
		if strings.Contains(body, gone) {
			t.Fatalf("debtors page still renders the old form: %q", gone)
		}
	}
	// صفحه تیتر عمومی (topbar) در این صفحه نمایش داده نمی‌شود؛ عنوان در سربرگ وب است.
	if strings.Contains(body, "دفتر حساب‌های دفتری و نسیه") {
		t.Fatal("debtors page must not render the generic topbar heading")
	}
}

// TestDebtorCreateWithProductItems: ثبت بدهی دستی با آیتم‌های انتخاب‌شده از
// فهرست محصولات، جمع کل در ریال و مستندات سفارش در جدول.
func TestDebtorCreateWithProductItems(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	latte := posProduct(t, a, "لاته", "300000")    // ۳۰,۰۰۰ تومان
	cake := posProduct(t, a, "کیک شکلاتی", "500000") // ۵۰,۰۰۰ تومان

	body := ordersPost(t, a, "/local/debtor-create", url.Values{
		"account_kind":     {"personal"},
		"full_name":        {"سارا محمدی"},
		"phone":            {"۰۹۱۲۳۴۵۶۷۸۹"},
		"mode":             {"items"},
		"item_title":       {"لاته", "کیک شکلاتی"},
		"item_product_id":  {latte, cake},
		"item_qty":         {"2", "1"},
		"item_price":       {"30000", "50000"},
		"due_at":           {"2026-10-25"},
		"note":             {"نسیه میز ۸"},
	})
	debtors, _ := a.store.Entities(ctx, "debtor")
	if len(debtors) != 1 {
		t.Fatalf("expected 1 debtor, got %d", len(debtors))
	}
	d := debtors[0]
	if d.Field("total") != "1100000" || d.Field("remaining") != "1100000" {
		t.Fatalf("total/remaining must be 2×30000 + 1×50000 Toman in IRR, got %q / %q", d.Field("total"), d.Field("remaining"))
	}
	if d.Field("paid") != "0" || d.Field("status") != "open" || d.Field("source") != "manual" {
		t.Fatalf("new debtor fields wrong: paid=%q status=%q source=%q", d.Field("paid"), d.Field("status"), d.Field("source"))
	}
	if d.Field("phone") != "09123456789" {
		t.Fatalf("phone must be normalized to Latin digits, got %q", d.Field("phone"))
	}
	if !strings.Contains(d.Field("items"), "لاته") || !strings.Contains(d.Field("items"), cake) {
		t.Fatalf("items were not stored on the debtor: %q", d.Field("items"))
	}
	if d.Field("due_at") != "2026-10-25" {
		t.Fatalf("due_at must keep the Shamsi picker ISO value, got %q", d.Field("due_at"))
	}
	if !strings.Contains(body, "لاته ×۲") || !strings.Contains(body, "کیک شکلاتی ×۱") {
		t.Fatalf("order documents column must list the items: %s", firstMatch(body, "dbk-doc"))
	}
	if !strings.Contains(body, "سارا محمدی") {
		t.Fatal("new debtor is not listed on the page")
	}
	if !strings.Contains(body, "۱۱۰,۰۰۰ تومان") {
		t.Fatalf("the total must be shown in Toman with web grouping: %s", firstMatch(body, "dbk-remain"))
	}
}

// TestDebtorCreateAmountModeOnly: حالت «فقط مبلغ کل بدهی» بدون هیچ آیتمی.
func TestDebtorCreateAmountModeOnly(t *testing.T) {
	a := testApp(t)
	ordersPost(t, a, "/local/debtor-create", url.Values{
		"account_kind": {"personal"},
		"full_name":    {"پیمان احمدی"},
		"phone":        {"09121112222"},
		"mode":         {"amount"},
		"total_toman":  {"۲۵۰,۰۰۰"},
	})
	debtors, _ := a.store.Entities(context.Background(), "debtor")
	if len(debtors) != 1 {
		t.Fatalf("expected 1 debtor, got %d", len(debtors))
	}
	if debtors[0].Field("total") != "2500000" {
		t.Fatalf("amount mode total = %q, want 2500000 IRR", debtors[0].Field("total"))
	}
	if debtors[0].Field("entry_mode") != "amount" {
		t.Fatalf("entry_mode = %q", debtors[0].Field("entry_mode"))
	}
}

// TestDebtorCreateValidation: فرم بدون نام/آیتم نباید چیزی ثبت کند.
func TestDebtorCreateValidation(t *testing.T) {
	a := testApp(t)
	body := ordersPost(t, a, "/local/debtor-create", url.Values{
		"account_kind": {"personal"},
		"full_name":    {"ی"},
		"phone":        {"09120000000"},
		"mode":         {"items"},
	})
	debtors, _ := a.store.Entities(context.Background(), "debtor")
	if len(debtors) != 0 {
		t.Fatalf("invalid debtor must not be stored, got %d", len(debtors))
	}
	if !strings.Contains(body, "حداقل ۲ نویسه") {
		t.Fatal("validation message for the name is missing")
	}
}

// TestDebtorCompanyAccountUsesGroup: حساب شرکتی نامش را از گروه می‌گیرد.
func TestDebtorCompanyAccountUsesGroup(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	gid, err := a.store.SaveLocalRecord(ctx, "customer_group", "", map[string]any{"name": "شرکت منووکس", "is_active": "true"})
	if err != nil {
		t.Fatal(err)
	}
	ordersPost(t, a, "/local/debtor-create", url.Values{
		"account_kind": {"company"},
		"group_id":     {gid},
		"mode":         {"amount"},
		"total_toman":  {"120000"},
	})
	debtors, _ := a.store.Entities(ctx, "debtor")
	if len(debtors) != 1 {
		t.Fatalf("expected 1 debtor, got %d", len(debtors))
	}
	d := debtors[0]
	if d.Field("account_type") != "company" || d.Field("group_id") != gid {
		t.Fatalf("company debtor wrong: kind=%q group=%q", d.Field("account_type"), d.Field("group_id"))
	}
	if d.Label() != "شرکت منووکس" {
		t.Fatalf("company debtor must be named after its group, got %q", d.Label())
	}
	// تب گزارش گروه‌ها باید همین گروه را با مانده بدهی نشان دهد.
	groupsPage := ordersGet(t, a, "/?page=debtors&tab=groups")
	if !strings.Contains(groupsPage, "شرکت منووکس") || !strings.Contains(groupsPage, "۱۲۰,۰۰۰ تومان") {
		t.Fatalf("group report must list the company balance: %s", firstMatch(groupsPage, "dbk-card"))
	}
}

// TestDebtorPartialPaymentAndLedger: پرداخت اقساطی، مانده و دفتر وصولی.
func TestDebtorPartialPaymentAndLedger(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	ordersPost(t, a, "/local/debtor-create", url.Values{
		"account_kind": {"personal"},
		"full_name":    {"دکتر نادری"},
		"phone":        {"09127778899"},
		"mode":         {"amount"},
		"total_toman":  {"500000"},
	})
	debtors, _ := a.store.Entities(ctx, "debtor")
	if len(debtors) != 1 {
		t.Fatalf("expected 1 debtor, got %d", len(debtors))
	}
	id := debtors[0].ID

	ordersPost(t, a, "/local/debtor-pay", url.Values{
		"debtor_id":    {id},
		"amount_toman": {"125000"},
		"note":         {"کارت به کارت"},
	})
	d, _ := a.store.Entity(ctx, "debtor", id)
	if d.Field("remaining") != "3750000" || d.Field("paid") != "1250000" {
		t.Fatalf("partial payment wrong: remaining=%q paid=%q", d.Field("remaining"), d.Field("paid"))
	}
	if d.Field("status") != "partial" {
		t.Fatalf("status must be partial, got %q", d.Field("status"))
	}
	pays, _ := a.store.Entities(ctx, "debtor_payment")
	if len(pays) != 1 || pays[0].Field("amount_irr") != "1250000" {
		t.Fatalf("payment ledger entry missing: %+v", pays)
	}
	page := ordersGet(t, a, "/?page=debtors")
	if !strings.Contains(page, "پرداخت بخشی از بدهی") {
		t.Fatal("partial status label is missing from the list")
	}
	// کارت «وصول این ماه» باید همان پرداخت ثبت‌شده را نشان دهد.
	if !strings.Contains(page, "۱۲۵,۰۰۰ تومان") {
		t.Fatalf("collected-this-month KPI must include the payment: %s", firstMatch(page, "وصول این ماه"))
	}

	// تسویه کامل از مسیر قدیمی (مبلغ ریالی) هم باید کار کند.
	ordersPost(t, a, "/local/debtor-pay", url.Values{"debtor_id": {id}, "pay_mode": {"full"}})
	d, _ = a.store.Entity(ctx, "debtor", id)
	if d.Field("remaining") != "0" || d.Field("status") != "paid" {
		t.Fatalf("full settlement wrong: remaining=%q status=%q", d.Field("remaining"), d.Field("status"))
	}
}

// TestDebtorFiltersAndSort: فیلتر وضعیت/منبع/حساب و مرتب‌سازی مانده.
func TestDebtorFiltersAndSort(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	ordersPost(t, a, "/local/debtor-create", url.Values{
		"account_kind": {"personal"}, "full_name": {"الف باز"}, "phone": {"09120000001"},
		"mode": {"amount"}, "total_toman": {"100000"},
	})
	ordersPost(t, a, "/local/debtor-create", url.Values{
		"account_kind": {"personal"}, "full_name": {"ب تسویه"}, "phone": {"09120000002"},
		"mode": {"amount"}, "total_toman": {"50000"},
	})
	debtors, _ := a.store.Entities(ctx, "debtor")
	var paidID string
	for _, d := range debtors {
		if d.Label() == "ب تسویه" {
			paidID = d.ID
		}
	}
	ordersPost(t, a, "/local/debtor-pay", url.Values{"debtor_id": {paidID}, "pay_mode": {"full"}})

	openPage := ordersGet(t, a, "/?page=debtors&status=open")
	if !strings.Contains(openPage, "الف باز") || strings.Contains(openPage, "ب تسویه") {
		t.Fatal("status=open filter must only list unpaid accounts")
	}
	paidPage := ordersGet(t, a, "/?page=debtors&status=paid")
	if strings.Contains(paidPage, "الف باز") || !strings.Contains(paidPage, "ب تسویه") {
		t.Fatal("status=paid filter must only list settled accounts")
	}
	sortPage := ordersGet(t, a, "/?page=debtors&sort=remaining")
	if strings.Index(sortPage, "الف باز") > strings.Index(sortPage, "ب تسویه") {
		t.Fatal("sort=remaining must put the biggest remaining balance first")
	}
	nonePage := ordersGet(t, a, "/?page=debtors&q=ناموجود")
	if !strings.Contains(nonePage, "بدهکاری مطابق این فیلتر پیدا نشد.") {
		t.Fatal("empty state for a filter with no match is missing")
	}
}

// TestDebtorsExportCSV: خروجی CSV با سرستون‌های وب و مبلغ تومانی.
func TestDebtorsExportCSV(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	if _, err := a.store.SaveLocalRecord(ctx, "debtor", "", map[string]any{
		"full_name": "مشتری خروجی", "phone": "09121234567", "total": "1450000",
		"paid": "450000", "remaining": "1000000", "status": "partial", "currency": "IRR",
	}); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "http://localhost/local/debtors-export", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("export -> %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/csv") {
		t.Fatalf("content type = %q", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, "نام طرف حساب") || !strings.Contains(body, "مانده بدهی (تومان)") {
		t.Fatalf("csv header missing: %s", body)
	}
	if !strings.Contains(body, "مشتری خروجی") || !strings.Contains(body, "100000") {
		t.Fatalf("csv row missing: %s", body)
	}
}

// firstMatch فقط برای پیام خطای کوتاه‌تر: بخشی از HTML اطراف یک نشانه را برمی‌گرداند.
func firstMatch(body, needle string) string {
	idx := strings.Index(body, needle)
	if idx < 0 {
		return "(not found)"
	}
	end := idx + 240
	if end > len(body) {
		end = len(body)
	}
	return body[idx:end]
}
