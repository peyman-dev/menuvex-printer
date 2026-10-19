package store

import (
	"context"
	"strings"
	"testing"
)

func TestPrintEnqueueWithoutPrinterCreatesExplicitFailedJob(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if _, err := s.SaveLocalRecord(ctx, "product", "", map[string]any{"title": "تست", "price": "10000"}); err != nil {
		t.Fatal(err)
	}
	prods, _ := s.Entities(ctx, "product")
	orderID, err := s.CreatePOSOrder(ctx, POSOrderInput{Items: []POSOrderLineInput{{ProductID: prods[0].ID, Quantity: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := s.EnqueueOrderPrintJobs(ctx, orderID)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].Status != PrintStatusFailed || !strings.Contains(jobs[0].LastError, "چاپگر") {
		t.Fatalf("expected one explicit failed job, got %+v", jobs)
	}
	// ساخت دوباره نباید Job تکراری بسازد
	again, err := s.EnqueueOrderPrintJobs(ctx, orderID)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 1 {
		t.Fatalf("dedupe failed: %+v", again)
	}
	all, _ := s.PrintJobs(ctx, "")
	if len(all) != 1 {
		t.Fatalf("expected 1 persisted job, got %d", len(all))
	}
	// سفارش با وجود خطای چاپ سالم می‌ماند
	if _, err := s.Entity(ctx, "order", orderID); err != nil {
		t.Fatal("order must survive print failure:", err)
	}
}

func TestPOSOrderClientTokenIsIdempotent(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if _, err := s.SaveLocalRecord(ctx, "product", "", map[string]any{"title": "لاته", "price": "100000"}); err != nil {
		t.Fatal(err)
	}
	prods, _ := s.Entities(ctx, "product")
	input := POSOrderInput{ClientToken: "tok-1", Items: []POSOrderLineInput{{ProductID: prods[0].ID, Quantity: 2}}}
	first, err := s.CreatePOSOrder(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreatePOSOrder(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("same token created two orders: %s vs %s", first, second)
	}
	orders, _ := s.Entities(ctx, "order")
	if len(orders) != 1 {
		t.Fatalf("expected 1 order, got %d", len(orders))
	}
}

func TestOrderStatusMachineAndSingleBOMDeduction(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if err := s.SeedIranianCafeData(ctx); err != nil {
		t.Fatal(err)
	}
	latteID := "22222222-2222-4222-8222-000000000002" // کافه لاته فندق (دارای رسپی در Seed)
	before, _ := s.Entity(ctx, "ingredient", "33333333-3333-4333-8333-000000000001")
	orderID, err := s.CreatePOSOrder(ctx, POSOrderInput{Items: []POSOrderLineInput{{ProductID: latteID, Quantity: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateLocalOrderStatus(ctx, orderID, "completed"); err != nil {
		t.Fatal(err)
	}
	afterOnce, _ := s.Entity(ctx, "ingredient", "33333333-3333-4333-8333-000000000001")
	if before.Field("current_stock") == afterOnce.Field("current_stock") {
		t.Fatal("BOM deduction did not happen")
	}
	// تکرار همان وضعیت: بی‌اثر و بدون کسر دوباره
	if err := s.UpdateLocalOrderStatus(ctx, orderID, "completed"); err != nil {
		t.Fatal(err)
	}
	afterTwice, _ := s.Entity(ctx, "ingredient", "33333333-3333-4333-8333-000000000001")
	if afterOnce.Field("current_stock") != afterTwice.Field("current_stock") {
		t.Fatal("BOM deducted twice")
	}
	// گذار نامعتبر از وضعیت نهایی
	if err := s.UpdateLocalOrderStatus(ctx, orderID, "preparing"); err == nil {
		t.Fatal("completed -> preparing must be rejected")
	}
	// برای کسر رسپی باید سند گردش انبار هم ثبت شده باشد
	movements, _ := s.Entities(ctx, "inventory_movement")
	found := false
	for _, m := range movements {
		if m.Field("order_id") == orderID && m.Field("transaction_type") == "consumption" {
			found = true
		}
	}
	if !found {
		t.Fatal("BOM deduction has no inventory movement ledger entry")
	}
}

func TestRecipeCostFromBOM(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if err := s.SeedIranianCafeData(ctx); err != nil {
		t.Fatal(err)
	}
	// رسپی لاته: ۱۸.۵g قهوه (۱۴۵۰۰) + ۲۲۰ml شیر (۴۸۰) + ۲۰ml سیروپ (۸۵۰۰)
	cost, has, err := s.ProductRecipeCostIRR(ctx, "22222222-2222-4222-8222-000000000002")
	if err != nil || !has {
		t.Fatalf("cost lookup failed: %s %v %v", cost, has, err)
	}
	if cost != "543850" {
		t.Fatalf("expected 543850 IRR, got %s", cost)
	}
	if _, has, _ := s.ProductRecipeCostIRR(ctx, "no-such-product"); has {
		t.Fatal("unknown product must not have recipe cost")
	}
}

func TestSaveLocalRecordRejectsNegativePrice(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if _, err := s.SaveLocalRecord(ctx, "product", "", map[string]any{"title": "بد", "price": "-5"}); err == nil {
		t.Fatal("negative price must be rejected")
	}
	if _, err := s.SaveLocalRecord(ctx, "product", "", map[string]any{"title": "خوب", "price": "12000"}); err != nil {
		t.Fatal(err)
	}
}
