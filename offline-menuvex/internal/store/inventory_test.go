package store

import (
	"context"
	"path/filepath"
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
