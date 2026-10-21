package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

// سفارش صندوق وقتی دستگاه به سرور متصل است باید عملیات سینک را در همان
// تراکنش صف کند؛ ری‌استارت برنامه هم نباید سفارش یا عملیاتش را از دست بدهد.
func TestPOSOrderQueuesSyncOpAndSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "offline.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Bind(ctx, "https://server|device"); err != nil {
		t.Fatal(err)
	}
	if err := s.SeedIranianCafeData(ctx); err != nil {
		t.Fatal(err)
	}
	orderID, err := s.CreatePOSOrder(ctx, POSOrderInput{
		Items: []POSOrderLineInput{{ProductID: "22222222-2222-4222-8222-000000000002", Quantity: 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ops, _ := s.Outbox(ctx, true)
	found := false
	for _, op := range ops {
		if op.EntityType == "order" && op.EntityID == orderID && op.Action == "create" {
			found = true
			var p map[string]any
			if json.Unmarshal(op.Payload, &p) != nil || p["id"] != orderID {
				t.Fatalf("order op payload mismatch: %s", op.Payload)
			}
		}
	}
	if !found {
		t.Fatalf("order create was not queued for sync: %+v", ops)
	}
	// گذار وضعیت هم باید صف شود.
	if err := s.UpdateLocalOrderStatus(ctx, orderID, "accepted"); err != nil {
		t.Fatal(err)
	}
	ops, _ = s.Outbox(ctx, true)
	foundTransition := false
	for _, op := range ops {
		if op.EntityType == "order" && op.Action == "transition" && op.EntityID == orderID {
			foundTransition = true
		}
	}
	if !foundTransition {
		t.Fatal("order transition was not queued for sync")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Entity(ctx, "order", orderID); err != nil {
		t.Fatalf("order lost after restart: %v", err)
	}
	ops, _ = s.Outbox(ctx, true)
	if len(ops) < 2 {
		t.Fatalf("queued sync operations lost after restart: %+v", ops)
	}
}

// در حالت کاملاً مستقل (بدون سرور پیکربندی‌شده) نباید صفی ساخته شود.
func TestStandaloneOrderDoesNotQueueSyncOps(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if err := s.SeedIranianCafeData(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePOSOrder(ctx, POSOrderInput{
		Items: []POSOrderLineInput{{ProductID: "22222222-2222-4222-8222-000000000002", Quantity: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	ops, _ := s.Outbox(ctx, true)
	if len(ops) != 0 {
		t.Fatalf("standalone mode must not queue sync operations: %+v", ops)
	}
}

// Bootstrap هرگز نباید سفارش محلی همگام‌نشده را پاک کند؛ فقط دادهٔ سروری
// با اسنپ‌شات تازه جایگزین می‌شود.
func TestBootstrapPreservesLocalUnsyncedOrder(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "offline.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Bind(ctx, "https://server|device"); err != nil {
		t.Fatal(err)
	}
	if err := s.SeedIranianCafeData(ctx); err != nil {
		t.Fatal(err)
	}
	orderID, err := s.CreatePOSOrder(ctx, POSOrderInput{
		Items: []POSOrderLineInput{{ProductID: "22222222-2222-4222-8222-000000000002", Quantity: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	sections := map[string][]json.RawMessage{
		"categories": {json.RawMessage(`{"id":"` + entityA + `","title":"از سرور","version":3}`)},
	}
	if err := s.Bootstrap(ctx, sections, "42"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Entity(ctx, "order", orderID); err != nil {
		t.Fatalf("local unsynced order was destroyed by bootstrap: %v", err)
	}
	if _, err := s.Entity(ctx, "category", entityA); err != nil {
		t.Fatalf("server snapshot category missing: %v", err)
	}
	ops, _ := s.Outbox(ctx, true)
	if len(ops) == 0 {
		t.Fatal("outbox lost its queued order during bootstrap")
	}
}

// شکست کل درخواست push عملیات را حذف نمی‌کند؛ فقط به پنجرهٔ تلاش مجدد می‌روند.
func TestPushFailureBacksOffWithoutDroppingOps(t *testing.T) {
	s := scoped(t)
	ctx := context.Background()
	if err := s.SeedIranianCafeData(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePOSOrder(ctx, POSOrderInput{
		Items: []POSOrderLineInput{{ProductID: "22222222-2222-4222-8222-000000000002", Quantity: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	before, _ := s.Outbox(ctx, true)
	if len(before) == 0 {
		t.Fatal("expected queued ops before push failure")
	}
	if err := s.NotePushFailure(ctx, "push request failed"); err != nil {
		t.Fatal(err)
	}
	// در پنجرهٔ بک‌آف فعلاً واجد ارسال نیستند ولی از دست نرفته‌اند.
	eligible, _ := s.Outbox(ctx, true)
	all, _ := s.Outbox(ctx, false)
	if len(eligible) != 0 {
		t.Fatalf("ops must wait out their backoff window: %+v", eligible)
	}
	if len(all) != len(before) {
		t.Fatalf("ops lost after push failure: %d -> %d", len(before), len(all))
	}
	for _, op := range all {
		if op.State == "queued" && op.Attempts < 1 {
			t.Fatalf("attempt not recorded: %+v", op)
		}
	}
}
