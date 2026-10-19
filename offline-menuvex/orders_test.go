package desktop

import (
	"context"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sinaqaffari/coffish/desktop/internal/store"
)

// seedOrder یک سفارش واقعی از مسیر صندوق می‌سازد (بدون داده ساختگی نمایشی).
func seedOrder(t *testing.T, a *Application, productID string, qty int) string {
	t.Helper()
	id, err := a.store.CreatePOSOrder(context.Background(), store.POSOrderInput{
		ClientToken: "test-" + time.Now().Format("150405.000000000"),
		Items:       []store.POSOrderLineInput{{ProductID: productID, Quantity: qty}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func ordersGet(t *testing.T, a *Application, path string) string {
	t.Helper()
	r := httptest.NewRequest("GET", "http://localhost"+path, nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("GET %s -> %d", path, w.Code)
	}
	return w.Body.String()
}

func ordersPost(t *testing.T, a *Application, path string, form url.Values) string {
	t.Helper()
	form.Set("csrf", a.csrf)
	r := httptest.NewRequest("POST", "http://localhost"+path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("POST %s -> %d", path, w.Code)
	}
	return w.Body.String()
}

// TestOrdersPageMatchesWeb ساختار صفحه سفارش‌های وب (هدر، آمار، جستجو،
// چیپ‌های وضعیت با شمارنده، جدول ردیفی، بج‌ها و اقدام اصلی) را قفل می‌کند.
func TestOrdersPageMatchesWeb(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	prodID, err := a.store.SaveLocalRecord(ctx, "product", "", map[string]any{"title": "لاته سفارش", "price": "1500000"})
	if err != nil {
		t.Fatal(err)
	}
	// کافه چاپگر دارد تا اقدام‌های چاپ منوی ردیف (فیش مشتری/بلیت) نمایش داده شوند.
	if _, err := a.store.SaveLocalRecord(ctx, "printer", "", map[string]any{"title": "چاپگر تست", "connection": "192.0.2.30:9100", "paper_width": "80mm"}); err != nil {
		t.Fatal(err)
	}
	pendingID := seedOrder(t, a, prodID, 2)
	doneID := seedOrder(t, a, prodID, 1)
	if err := a.store.UpdateLocalOrderStatus(ctx, doneID, "completed"); err != nil {
		t.Fatal(err)
	}
	_ = pendingID

	html := ordersGet(t, a, "/?page=orders")
	for _, want := range []string{
		`class="ord-wrap"`, "<h1>سفارش‌ها</h1>", "ord-live", "محلی",
		"صف زنده صندوق و سفارش آنلاین", "فیش‌ها",
		"درآمد تکمیل‌شده", "سفارش فعال",
		"شماره سفارش، آیتم یا نام مشتری", "جستجو",
		"در انتظار تأیید", "تأیید صندوقدار", "در حال آماده‌سازی", "آماده تحویل", "تکمیل‌شده", "لغوشده",
		`class="ord-table-head"`, "شماره", "میز", "مبلغ", "زمان", "وضعیت", "اقدام",
		"در انتظار تأیید صندوقدار", "تکمیل شده",
		"لاته سفارش ×۲", "تومان", "فیش مشتری", "بلیت آشپزخانه",
		"ord-badge-PENDING", "ord-badge-COMPLETED", "ord-next-btn",
		// منوی ردیف و دیالوگ‌های وب
		`class="dd-menu"`, "لغو سفارش", "ویرایش سفارش", "بعداً پرداخت می‌کنند (بدهکاران)", "حذف سفارش",
		`id="dlg-cancel"`, `id="dlg-delete"`, `id="dlg-edit"`, `id="dlg-defer"`,
		"مشتری منصرف شد", "سفارش اشتباه ثبت شد", "آشپزخانه نتوانست تهیه کند", "سفارش تکراری بود",
		"دلیل لغو:", "مثلاً: مشتری ۲۰ دقیقه منتظر ماند و رفت", "بله، حذف شود",
		"دلیل حذف (اجباری)", "ثبت در لیست بدهکاران (بعداً پرداخت)", "ثبت در بدهکاران",
		"کی پرداخت می‌کند؟ (اختیاری)", "بدون سررسید", "افزودن محصول",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("orders page missing web piece %q", want)
		}
	}
	for _, banned := range []string{"مانیتور بار", "دنگ", "ارسال به بار", "order-card", "تکمیل و کسر از انبار", "هیچ سفارشی ثبت نشده است"} {
		if strings.Contains(html, banned) {
			t.Fatalf("orders page must not contain legacy/invented piece %q", banned)
		}
	}
	// وضعیت خالی وب: متن مخصوص فیلتر بدون نتیجه.
	empty := ordersGet(t, a, "/?page=orders&status=CANCELLED")
	if !strings.Contains(empty, "سفارشی در این فیلتر نیست") || !strings.Contains(empty, "بروزرسانی") {
		t.Fatal("orders empty state must match web")
	}
	// جستجو روی نام آیتم و فیلتر وضعیت باید سمت سرور اعمال شود.
	found := ordersGet(t, a, "/?page=orders&q="+url.QueryEscape("لاته"))
	if !strings.Contains(found, "لاته سفارش") {
		t.Fatal("orders search by item name failed")
	}
	onlyDone := ordersGet(t, a, "/?page=orders&status=COMPLETED")
	if strings.Contains(onlyDone, "ord-badge-PENDING") {
		t.Fatal("status filter COMPLETED must hide pending rows")
	}
}

// TestOrderStatusFlowMatchesWeb چرخه وضعیت وب (تأیید ← آماده‌سازی ← آماده
// تحویل ← تکمیل)، لغو با دلیل اجباری و رد گذار نامعتبر را قفل می‌کند.
func TestOrderStatusFlowMatchesWeb(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	prodID, err := a.store.SaveLocalRecord(ctx, "product", "", map[string]any{"title": "اسپرسو چرخه", "price": "900000"})
	if err != nil {
		t.Fatal(err)
	}
	id := seedOrder(t, a, prodID, 1)

	step := func(status, reason string) {
		ordersPost(t, a, "/local/order-status", url.Values{
			"order_id": {id}, "status": {status}, "reason": {reason},
		})
	}
	statusOf := func() string {
		e, eErr := a.store.Entity(ctx, "order", id)
		if eErr != nil {
			t.Fatal(eErr)
		}
		return e.Field("status")
	}

	step("accepted", "")
	if statusOf() != "accepted" {
		t.Fatalf("pending->accepted failed, got %q", statusOf())
	}
	step("preparing", "")
	if statusOf() != "preparing" {
		t.Fatalf("accepted->preparing failed, got %q", statusOf())
	}
	// لغو بدون دلیل باید رد شود (قاعده وب).
	step("cancelled", "")
	if statusOf() != "preparing" {
		t.Fatalf("cancel without reason must be rejected, got %q", statusOf())
	}
	step("cancelled", "اشتباه ثبت شد")
	if statusOf() != "cancelled" {
		t.Fatalf("cancel with reason failed, got %q", statusOf())
	}
	e, _ := a.store.Entity(ctx, "order", id)
	if e.Field("status_reason") != "اشتباه ثبت شد" {
		t.Fatal("cancel reason must be stored on the order")
	}
	// از وضعیت نهایی گذار دیگری ممکن نیست.
	if err := a.store.UpdateLocalOrderStatus(ctx, id, "preparing"); err == nil {
		t.Fatal("cancelled->preparing must be rejected by the state machine")
	}

	// تکمیل‌شده هم به عقب برنمی‌گردد.
	id2 := seedOrder(t, a, prodID, 1)
	if err := a.store.UpdateLocalOrderStatus(ctx, id2, "completed"); err != nil {
		t.Fatal(err)
	}
	if err := a.store.UpdateLocalOrderStatus(ctx, id2, "accepted"); err == nil {
		t.Fatal("completed->accepted must be rejected")
	}
}

// TestOrderDeleteRequiresTypedNumber حذف سفارش فقط با تایپ شماره سفارش و
// دلیل (مانند دیالوگ حذف وب) ممکن است.
func TestOrderDeleteRequiresTypedNumber(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	prodID, err := a.store.SaveLocalRecord(ctx, "product", "", map[string]any{"title": "کاپوچینو حذف", "price": "800000"})
	if err != nil {
		t.Fatal(err)
	}
	id := seedOrder(t, a, prodID, 1)
	ent, _ := a.store.Entity(ctx, "order", id)
	number := ent.Field("order_number")
	if number == "" {
		t.Fatal("seeded order must have a number")
	}
	del := func(confirm, reason string) {
		ordersPost(t, a, "/local/delete", url.Values{
			"kind": {"order"}, "id": {id}, "return_page": {"orders"},
			"confirm_number": {confirm}, "reason": {reason},
		})
	}
	exists := func() bool {
		_, eErr := a.store.Entity(ctx, "order", id)
		return eErr == nil
	}
	del("999999", "دلیل کافی برای حذف")
	if !exists() {
		t.Fatal("delete with wrong typed number must be rejected")
	}
	del(number, "")
	if !exists() {
		t.Fatal("delete without reason must be rejected")
	}
	// شماره با ارقام فارسی هم پذیرفته می‌شود (ورودی فارسی/انگلیسی وب).
	del(store.ToPersianDigits(number), "ثبت اشتباه توسط صندوق")
	if exists() {
		t.Fatal("delete with correct typed number (fa digits) + reason must succeed")
	}
}
