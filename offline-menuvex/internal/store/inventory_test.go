package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestInventoryMovingAverageCost منطق وب: ارزش‌گذاری با میانگین موزون متحرک.
func TestInventoryMovingAverageCost(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	id, err := s.SaveLocalRecord(ctx, "ingredient", "", map[string]any{
		"name": "دانه قهوه تست", "base_unit": "g", "current_stock": "100", "average_cost": "1000",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordStockMovement(ctx, id, "purchase", "100", "2000", "خرید تست"); err != nil {
		t.Fatal(err)
	}
	ing, err := s.Entity(ctx, "ingredient", id)
	if err != nil {
		t.Fatal(err)
	}
	if ing.Field("current_stock") != "200.00" && ing.Field("current_stock") != "200" {
		t.Fatalf("stock after purchase = %s, want 200", ing.Field("current_stock"))
	}
	if got := ing.Field("average_cost"); got != "1500.00" && got != "1500" {
		t.Fatalf("moving average = %s, want 1500 (not last price 2000)", got)
	}
	if got := ing.Field("last_purchase_cost"); got != "2000.00" && got != "2000" {
		t.Fatalf("last purchase cost = %s, want 2000", got)
	}
	if _, err := s.RecordStockMovement(ctx, id, "consumption", "50", "", "مصرف بار"); err != nil {
		t.Fatal(err)
	}
	ing, _ = s.Entity(ctx, "ingredient", id)
	if got := ing.Field("average_cost"); got != "1500.00" && got != "1500" {
		t.Fatalf("average after consumption = %s, want unchanged 1500", got)
	}
	movs, _ := s.Entities(ctx, "inventory_movement")
	if len(movs) != 2 {
		t.Fatalf("movements = %d, want 2 ledger rows", len(movs))
	}
}

// TestRecordStockMovementWebDirections سناریوهای دیالوگ «ثبت تغییر موجودی» وب
// را قفل می‌کند: ورود افزایشی، مصرفِ سقف‌دار تا صفر و شمارش با موجودی نهایی.
func TestRecordStockMovementWebDirections(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	id, err := s.SaveLocalRecord(ctx, "ingredient", "", map[string]any{
		"name": "شیر تست", "base_unit": "ml", "current_stock": "100",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordStockMovement(ctx, id, "incoming", "25", "", "رسید خرید"); err != nil {
		t.Fatal(err)
	}
	ing, _ := s.Entity(ctx, "ingredient", id)
	if got := ing.Field("current_stock"); got != "125.00" {
		t.Fatalf("stock after incoming = %s, want 125.00", got)
	}
	if _, err := s.RecordStockMovement(ctx, id, "outgoing", "200", "", "مصرف روزانه"); err != nil {
		t.Fatal(err)
	}
	ing, _ = s.Entity(ctx, "ingredient", id)
	if got := ing.Field("current_stock"); got != "0.00" {
		t.Fatalf("stock after over-consumption = %s, want clamped 0.00", got)
	}
	if _, err := s.RecordStockMovement(ctx, id, "adjustment_target", "12.34", "", "اصلاح شمارش"); err != nil {
		t.Fatal(err)
	}
	ing, _ = s.Entity(ctx, "ingredient", id)
	if got := ing.Field("current_stock"); got != "12.34" {
		t.Fatalf("stock after count adjustment = %s, want target 12.34", got)
	}
	movs, _ := s.Entities(ctx, "inventory_movement")
	if len(movs) != 3 {
		t.Fatalf("movements = %d, want 3", len(movs))
	}
	byType := map[string]Entity{}
	for _, mov := range movs {
		byType[mov.Field("transaction_type")] = mov
	}
	if got := byType["purchase"].Field("quantity"); got != "25.00" {
		t.Fatalf("incoming movement quantity = %s", got)
	}
	if got := byType["consumption"].Field("quantity"); got != "-125.00" {
		t.Fatalf("outgoing movement quantity = %s, want effective -125.00", got)
	}
	adj := byType["adjustment"]
	if adj.Field("quantity") != "12.34" || adj.Field("stock_after") != "12.34" || adj.Field("description") != "اصلاح شمارش" {
		t.Fatalf("adjustment movement = %+v", adj)
	}
}

// TestPOSBlocksProductsWithoutIngredientStock قاعدهٔ «ناموجودی» صندوق را قفل
// می‌کند: اگر مواد اولیهٔ رسپی برای یک واحد کافی نباشد، محصول ناموجود است و
// ثبت سفارش همان محصول با خطای موجودی رد می‌شود (همان چیزی که کارت صندوق
// نشان می‌دهد).
func TestPOSBlocksProductsWithoutIngredientStock(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	ingID, err := s.SaveLocalRecord(ctx, "ingredient", "", map[string]any{
		"name": "دانه قهوه عربیکا", "base_unit": "g", "current_stock": "30", "average_cost": "1000",
	})
	if err != nil {
		t.Fatal(err)
	}
	prodID, err := s.SaveLocalRecord(ctx, "product", "", map[string]any{
		"title": "اسپرسو دوبل", "price": "960000",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveLocalRecord(ctx, "recipe", "", map[string]any{
		"title": "رسپی اسپرسو دوبل", "product_id": prodID, "yield_qty": "1",
		"items": []map[string]any{
			{"ingredient_id": ingID, "ingredient_name": "دانه قهوه عربیکا", "quantity": "50", "unit": "g"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	portions := s.ProductPortions(ctx)
	got, ok := portions[prodID]
	if !ok {
		t.Fatalf("product %s missing from portions map", prodID)
	}
	if got.Portions != 0 || !got.Out() {
		t.Fatalf("portions with 30g stock and 50g per unit = %+v, want 0 (ناموجود)", got)
	}
	if got.Missing != "دانه قهوه عربیکا" {
		t.Fatalf("missing ingredient = %q, want دانه قهوه عربیکا", got.Missing)
	}

	_, err = s.CreatePOSOrder(ctx, POSOrderInput{Items: []POSOrderLineInput{{ProductID: prodID, Quantity: 1}}})
	if err == nil {
		t.Fatal("POS order with insufficient ingredient stock was accepted")
	}
	if !strings.Contains(err.Error(), "دانه قهوه عربیکا") {
		t.Fatalf("stock error should name the ingredient, got %q", err.Error())
	}

	// با شارژ انبار (۱۲۰ گرم) دو واحد قابل تهیه است و ثبت سفارش مجاز می‌شود.
	if _, err := s.RecordStockMovement(ctx, ingID, "purchase", "90", "1000", "خرید تست"); err != nil {
		t.Fatal(err)
	}
	portions = s.ProductPortions(ctx)
	if got = portions[prodID]; got.Portions != 2 || got.Out() {
		t.Fatalf("portions after restock = %+v, want 2", got)
	}
	if _, err := s.CreatePOSOrder(ctx, POSOrderInput{Items: []POSOrderLineInput{{ProductID: prodID, Quantity: 2}}}); err != nil {
		t.Fatalf("POS order within stock rejected: %v", err)
	}
	// بیش از موجودی هم رد می‌شود.
	if _, err := s.CreatePOSOrder(ctx, POSOrderInput{Items: []POSOrderLineInput{{ProductID: prodID, Quantity: 3}}}); err == nil {
		t.Fatal("POS order above available portions was accepted")
	}
}
