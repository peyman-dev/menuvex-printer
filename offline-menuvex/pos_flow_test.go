package desktop

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/sinaqaffari/coffish/desktop/internal/store"
)

func posProduct(t *testing.T, a *Application, title, price string) string {
	t.Helper()
	id, err := a.store.SaveLocalRecord(context.Background(), "product", "", map[string]any{"title": title, "price": price})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func posTable(t *testing.T, a *Application, title, number string) string {
	t.Helper()
	id, err := a.store.SaveLocalRecord(context.Background(), "table", "", map[string]any{"title": title, "number": number, "status": "available", "capacity": "4"})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// TestPOSExtraAndEditFullTotals مبلغ اضافی صندوق و ویرایش کامل (تخفیف+اضافه)
// را قفل می‌کند: total = subtotal − discount + extra، همیشه سمت سرور.
func TestPOSExtraAndEditFullTotals(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	p1 := posProduct(t, a, "قهوه", "100000")
	id, err := a.store.CreatePOSOrder(ctx, store.POSOrderInput{
		ClientToken: "extra-1", DiscountIRR: "10000", ExtraIRR: "5000", ExtraNote: "سیروپ اضافه",
		Items: []store.POSOrderLineInput{{ProductID: p1, Quantity: 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ent, _ := a.store.Entity(ctx, "order", id)
	if ent.Field("total_irr") != "195000" || ent.Field("extra_irr") != "5000" || ent.Field("extra_note") != "سیروپ اضافه" {
		t.Fatalf("POS extra totals wrong: total=%s extra=%s", ent.Field("total_irr"), ent.Field("extra_irr"))
	}
	disc, extra := "20000", "0"
	err = a.store.UpdateOrderItemsFull(ctx, id, store.OrderItemsUpdate{
		Lines:       []store.POSOrderLineInput{{ProductID: p1, Quantity: 2}},
		DiscountIRR: &disc, ExtraIRR: &extra,
	})
	if err != nil {
		t.Fatal(err)
	}
	ent, _ = a.store.Entity(ctx, "order", id)
	if ent.Field("total_irr") != "180000" {
		t.Fatalf("edit-full total wrong: %s", ent.Field("total_irr"))
	}
}

// TestSplitBillFlow حساب دنگی: پرداخت سهمی هر نفر، عدم ثبت دوباره با همان
// توکن، رد انتخاب بیش از مانده، برگرداندن پرداخت و تسویه کامل.
func TestSplitBillFlow(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	p1 := posProduct(t, a, "آیتم یک", "100000")
	p2 := posProduct(t, a, "آیتم دو", "50000")
	id, err := a.store.CreatePOSOrder(ctx, store.POSOrderInput{
		ClientToken: "split-1",
		Items: []store.POSOrderLineInput{
			{ProductID: p1, Quantity: 2},
			{ProductID: p2, Quantity: 3},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ent, _ := a.store.Entity(ctx, "order", id)
	items := ent.OrderItems()
	if len(items) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(items))
	}
	line1, line2 := items[0].ID, items[1].ID

	paid, remaining, err := a.store.RecordSplitPayment(ctx, id, map[string]int{line1: 2}, "cash", "tok-1")
	if err != nil {
		t.Fatal(err)
	}
	if paid != 200000 || remaining != 150000 {
		t.Fatalf("first split payment wrong: paid=%d remaining=%d", paid, remaining)
	}
	// همان توکن دوباره: پرداخت دوم ساخته نمی‌شود.
	paid, remaining, err = a.store.RecordSplitPayment(ctx, id, map[string]int{line1: 2}, "cash", "tok-1")
	if err != nil {
		t.Fatal(err)
	}
	if paid != 200000 || remaining != 150000 {
		t.Fatalf("idempotent replay changed totals: paid=%d remaining=%d", paid, remaining)
	}
	// انتخاب بیش از مانده رد می‌شود.
	if _, _, err := a.store.RecordSplitPayment(ctx, id, map[string]int{line2: 4}, "card", "tok-2"); err == nil {
		t.Fatal("over-pick must be rejected")
	}
	paid, remaining, err = a.store.RecordSplitPayment(ctx, id, map[string]int{line2: 1}, "card", "tok-3")
	if err != nil {
		t.Fatal(err)
	}
	if paid != 250000 || remaining != 100000 {
		t.Fatalf("second split payment wrong: paid=%d remaining=%d", paid, remaining)
	}
	if err := a.store.UndoLastSplitPayment(ctx, id); err != nil {
		t.Fatal(err)
	}
	ent, _ = a.store.Entity(ctx, "order", id)
	if ent.Field("split_remaining_irr") != "150000" || ent.Field("split_status") != "PARTIAL" {
		t.Fatalf("undo wrong: remaining=%s status=%s", ent.Field("split_remaining_irr"), ent.Field("split_status"))
	}
	_, remaining, err = a.store.RecordSplitPayment(ctx, id, map[string]int{line2: 3}, "cash", "tok-4")
	if err != nil {
		t.Fatal(err)
	}
	ent, _ = a.store.Entity(ctx, "order", id)
	if remaining != 0 || ent.Field("split_status") != "PAID" {
		t.Fatalf("settle wrong: remaining=%d status=%s", remaining, ent.Field("split_status"))
	}
}

// TestTransferOrderFlow انتقال میز: مبدأ آزاد، مقصد درگیر، مقصدِ درگیر رد می‌شود.
func TestTransferOrderFlow(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	p1 := posProduct(t, a, "قهوه", "100000")
	tA := posTable(t, a, "میز الف", "1")
	tB := posTable(t, a, "میز ب", "2")
	id, err := a.store.CreatePOSOrder(ctx, store.POSOrderInput{
		ClientToken: "transfer-1", TableID: tA,
		Items: []store.POSOrderLineInput{{ProductID: p1, Quantity: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.TransferOrder(ctx, id, tB); err != nil {
		t.Fatal(err)
	}
	ent, _ := a.store.Entity(ctx, "order", id)
	if ent.Field("table_id") != tB {
		t.Fatalf("order not transferred: table_id=%s", ent.Field("table_id"))
	}
	ta, _ := a.store.Entity(ctx, "table", tA)
	tb, _ := a.store.Entity(ctx, "table", tB)
	if ta.Field("status") != "available" || ta.Field("current_order_id") != "" {
		t.Fatalf("source table not freed: status=%s current=%s", ta.Field("status"), ta.Field("current_order_id"))
	}
	if tb.Field("status") != "occupied" || tb.Field("current_order_id") != id {
		t.Fatalf("target table not occupied: status=%s current=%s", tb.Field("status"), tb.Field("current_order_id"))
	}
	// سفارش دوم روی مقصدِ درگیر رد می‌شود.
	id2, err := a.store.CreatePOSOrder(ctx, store.POSOrderInput{
		ClientToken: "transfer-2",
		Items:       []store.POSOrderLineInput{{ProductID: p1, Quantity: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.TransferOrder(ctx, id2, tB); err == nil {
		t.Fatal("transfer to occupied table must be rejected")
	}
}

// TestPOSPageParityBits اجزای کلیدی صفحه صندوق وب باید رندر شوند.
// کاربر چاپگر دارد (تعریف شده) تا منوی پرینترها و دکمه‌های چاپ دیده شوند.
func TestPOSPageParityBits(t *testing.T) {
	a := testApp(t)
	if _, err := a.store.SaveLocalRecord(context.Background(), "printer", "", map[string]any{"title": "چاپگر صندوق", "model": "Epson TM-T20 (80mm)", "connection": "192.0.2.10:9100", "station": "صندوق فروش", "paper_width": "80mm"}); err != nil {
		t.Fatal(err)
	}
	html := ordersGet(t, a, "/?page=pos")
	mustContain := []string{
		"جزئیات سفارش", "مبلغ اضافی", "مثلاً سیروپ اضافه", "تخفیف سفارش (درصدی یا مبلغی)",
		"مدیریت سفارشات و صدور فاکتور", "سفارش‌های صندوق", "حساب دنگی", "مراحل سفارش",
		"پرینترها", "posOpenSplit", "posOpenTransfer", "بعداً پرداخت می‌کنند (ثبت در بدهکاران)",
		"انتخاب همه‌ی مانده", "یک آیتم را لمس کنید", "تأیید پرداخت",
	}
	for _, s := range mustContain {
		if !strings.Contains(html, s) {
			t.Fatalf("POS page missing %q", s)
		}
	}
	if strings.Contains(html, "آخرین فاکتورهای صادرشده") {
		t.Fatal("POS must not show recent-invoices panel")
	}
}

// TestPOSAdvanceSkipsDisabledStages مرحله بعد باید مراحل غیرفعال را رد کند.
func TestPOSAdvanceSkipsDisabledStages(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	p1 := posProduct(t, a, "قهوه", "100000")
	ordersPost(t, a, "/local/pos-stages", url.Values{"stage_ready": {"on"}})
	id := seedOrder(t, a, p1, 1)
	ordersPost(t, a, "/local/order-advance", url.Values{"order_id": {id}})
	ent, _ := a.store.Entity(ctx, "order", id)
	if ent.Field("status") != "ready" {
		t.Fatalf("advance must skip disabled stages to ready, got %s", ent.Field("status"))
	}
}

// TestPOSSplitAndTransferEndpoints پرداخت دنگی و انتقال از مسیر HTTP.
func TestPOSSplitAndTransferEndpoints(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	p1 := posProduct(t, a, "قهوه", "100000")
	tA := posTable(t, a, "میز الف", "1")
	tB := posTable(t, a, "میز ب", "2")
	id, err := a.store.CreatePOSOrder(ctx, store.POSOrderInput{
		ClientToken: "split-ep-1", TableID: tA,
		Items: []store.POSOrderLineInput{{ProductID: p1, Quantity: 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ent, _ := a.store.Entity(ctx, "order", id)
	lineID := ent.OrderItems()[0].ID
	ordersPost(t, a, "/local/split-pay", url.Values{
		"order_id": {id}, "method": {"cash"}, "client_token": {"ep-tok-1"},
		"pick_" + lineID: {"1"},
	})
	ent, _ = a.store.Entity(ctx, "order", id)
	if ent.Field("split_status") != "PARTIAL" || ent.Field("split_remaining_irr") != "100000" {
		t.Fatalf("split-pay endpoint wrong: status=%s remaining=%s", ent.Field("split_status"), ent.Field("split_remaining_irr"))
	}
	ordersPost(t, a, "/local/order-transfer", url.Values{"order_id": {id}, "target_table_id": {tB}})
	ent, _ = a.store.Entity(ctx, "order", id)
	if ent.Field("table_id") != tB {
		t.Fatalf("transfer endpoint wrong: table_id=%s", ent.Field("table_id"))
	}
}

// TestPOSEditModePage حالت ویرایش سفارش صندوق با بنر و دکمه ذخیره وب.
func TestPOSEditModePage(t *testing.T) {
	a := testApp(t)
	p1 := posProduct(t, a, "قهوه", "100000")
	id := seedOrder(t, a, p1, 2)
	html := ordersGet(t, a, "/?page=pos&edit="+id)
	for _, s := range []string{"ذخیره تغییرات سفارش", "انصراف از ویرایش", "اقلام این سفارش در سبد است", "ویرایش سفارش"} {
		if !strings.Contains(html, s) {
			t.Fatalf("POS edit mode missing %q", s)
		}
	}
}
