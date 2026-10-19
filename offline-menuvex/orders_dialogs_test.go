package desktop

import (
	"context"
	"net/url"
	"testing"

	"github.com/sinaqaffari/coffish/desktop/internal/store"
)

// TestOrderDialogActions وبِ منوی ردیف را قفل می‌کند: ویرایش اقلام با قیمت
// سروری، انتقال به بدهکاران (یک‌بار)، و منع حذف سفارش تکمیل‌شده.
func TestOrderDialogActions(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	p1, err := a.store.SaveLocalRecord(ctx, "product", "", map[string]any{"title": "آیتم یک", "price": "1000000"})
	if err != nil {
		t.Fatal(err)
	}
	p2, err := a.store.SaveLocalRecord(ctx, "product", "", map[string]any{"title": "آیتم دو", "price": "500000"})
	if err != nil {
		t.Fatal(err)
	}
	id := seedOrder(t, a, p1, 1)

	// ویرایش: ۲× آیتم یک + ۳× آیتم دو = ۳۵۰۰۰۰۰ ریال ⇒ ۳۵۰٬۰۰۰ تومان
	ordersPost(t, a, "/local/order-edit", url.Values{
		"order_id":   {id},
		"product_id": {p1, p2},
		"quantity":   {"2", "3"},
		"line_note":  {"", "کم‌یخ"},
	})
	ent, _ := a.store.Entity(ctx, "order", id)
	if ent.Field("total_irr") != "3500000" || ent.Field("subtotal_irr") != "3500000" {
		t.Fatalf("edit totals wrong: total=%s subtotal=%s", ent.Field("total_irr"), ent.Field("subtotal_irr"))
	}
	if len(ent.OrderItems()) != 2 {
		t.Fatalf("edit items wrong: %d", len(ent.OrderItems()))
	}

	// بدهکاران: پرونده بدهی ساخته و پرداخت اعتباری می‌شود؛ بار دوم خطاست.
	ordersPost(t, a, "/local/order-defer", url.Values{
		"order_id": {id}, "customer_name": {"علی تست"}, "customer_phone": {"09123456789"}, "due_days": {"7"},
	})
	ent, _ = a.store.Entity(ctx, "order", id)
	if ent.Field("payment_method") != "credit_debtor" || ent.Field("customer_name") != "علی تست" {
		t.Fatalf("defer did not update order: %+v", ent)
	}
	debtors, _ := a.store.Entities(ctx, "debtor")
	found := false
	for _, d := range debtors {
		if d.Field("order_id") == id && d.Field("remaining") == "3500000" && d.Field("due_at") != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("debtor record for deferred order not found")
	}
	// ثبت دوباره باید رد شود (نه پرونده دوم).
	before := len(debtors)
	ordersPost(t, a, "/local/order-defer", url.Values{
		"order_id": {id}, "customer_name": {"علی تست"},
	})
	debtors2, _ := a.store.Entities(ctx, "debtor")
	if len(debtors2) != before {
		t.Fatal("double defer must not create a second debtor")
	}

	// سفارش تکمیل‌شده: ویرایش و حذف هر دو ممنوع‌اند (یکپارچگی مالی/BOM).
	doneID := seedOrder(t, a, p1, 1)
	if err := a.store.UpdateLocalOrderStatus(ctx, doneID, "completed"); err != nil {
		t.Fatal(err)
	}
	if err := a.store.UpdateOrderItems(ctx, doneID, []store.POSOrderLineInput{{ProductID: p1, Quantity: 1}}); err == nil {
		t.Fatal("completed order edit must be rejected")
	}
	doneEnt, _ := a.store.Entity(ctx, "order", doneID)
	ordersPost(t, a, "/local/delete", url.Values{
		"kind": {"order"}, "id": {doneID}, "return_page": {"orders"},
		"confirm_number": {doneEnt.Field("order_number")}, "reason": {"دلیل کافی برای تست"},
	})
	if _, eErr := a.store.Entity(ctx, "order", doneID); eErr != nil {
		t.Fatal("completed order must not be deletable")
	}
}
