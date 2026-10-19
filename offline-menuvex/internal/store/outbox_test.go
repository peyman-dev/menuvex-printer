package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

const entityA = "11111111-1111-4111-8111-111111111111"

func scoped(t *testing.T) *Store {
	s := openTest(t)
	if e := s.Bind(context.Background(), "https://server|device"); e != nil {
		t.Fatal(e)
	}
	return s
}
func TestQueueSurvivesRestartWithStableIdentity(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "offline.sqlite")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	s.Bind(ctx, "server|device")
	entity, e := s.QueueCatalog(ctx, "category", "", "create", json.RawMessage(`{"title":"Tea"}`), 0)
	if e != nil {
		t.Fatal(e)
	}
	before, _ := s.Outbox(ctx, true)
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	after, e := s.Outbox(ctx, true)
	if e != nil || len(after) != 1 || after[0].ID != before[0].ID || after[0].Sequence != before[0].Sequence || after[0].EntityID != entity {
		t.Fatalf("lost identity: %+v %v", after, e)
	}
	if !uuidPattern.MatchString(entity) || !uuidPattern.MatchString(after[0].ID) {
		t.Fatal("not UUIDs")
	}
}
func TestOutboxRejectsUnsupportedOrDangerousPayloads(t *testing.T) {
	s := scoped(t)
	ctx := context.Background()
	for _, tc := range []struct{ kind, raw string }{{"payment", `{"amount":100}`}, {"category", `null`}, {"category", `[]`}, {"category", `{"title":"","tenant_id":"x"}`}, {"product", `{"title":"Tea","category_id":"bad","price":"1"}`}, {"product", `{"title":"Tea","category_id":"` + entityA + `","price":1e20}`}, {"ingredient", `{"name":"Tea","base_unit":"g","current_stock":100}`}, {"category", `{"title":null}`}, {"banner", `{"title":"A","is_active":null}`}} {
		if _, e := s.QueueCatalog(ctx, tc.kind, "", "create", json.RawMessage(tc.raw), 0); e == nil {
			t.Fatal("accepted", tc)
		}
	}
	ops, _ := s.Outbox(ctx, true)
	if len(ops) != 0 {
		t.Fatal(ops)
	}
}
func TestQueueRequiresConfiguredScope(t *testing.T) {
	s := openTest(t)
	if _, e := s.QueueCatalog(context.Background(), "category", "", "create", json.RawMessage(`{"title":"Tea"}`), 0); e == nil {
		t.Fatal("unscoped intent accepted")
	}
}
func TestQueueUsesObservedVersionAndOneIntentPerEntity(t *testing.T) {
	s := scoped(t)
	ctx := context.Background()
	s.ApplyPage(ctx, []Change{{EntityType: "category", EntityID: entityA, Version: 2}}, "1.0")
	if _, e := s.QueueCatalog(ctx, "category", entityA, "update", json.RawMessage(`{"title":"Tea"}`), 1); e == nil {
		t.Fatal("stale editor accepted")
	}
	if _, e := s.QueueCatalog(ctx, "category", entityA, "update", json.RawMessage(`{"title":"Tea"}`), 2); e != nil {
		t.Fatal(e)
	}
	if _, e := s.QueueCatalog(ctx, "category", entityA, "delete", json.RawMessage(`{}`), 2); e == nil {
		t.Fatal("parallel edits accepted")
	}
	ops, _ := s.Outbox(ctx, true)
	if len(ops) != 1 || ops[0].BaseVersion == nil || *ops[0].BaseVersion != 2 {
		t.Fatal(ops)
	}
}
func TestResultValidationIsAtomic(t *testing.T) {
	s := scoped(t)
	ctx := context.Background()
	for _, title := range []string{"A", "B"} {
		s.QueueCatalog(ctx, "category", "", "create", json.RawMessage(`{"title":"`+title+`"}`), 0)
	}
	ops, _ := s.Outbox(ctx, true)
	bad := [][]Result{{{ID: ops[0].ID, Status: "applied"}}, {{ID: ops[0].ID, Status: "applied"}, {ID: ops[0].ID, Status: "applied"}}, {{ID: ops[0].ID, Status: "applied"}, {ID: ops[1].ID, Status: "unknown"}}}
	for _, results := range bad {
		if e := s.SaveResults(ctx, ops, results); e == nil {
			t.Fatal("invalid result accepted")
		}
		current, _ := s.Outbox(ctx, true)
		if len(current) != 2 || current[0].State != "queued" {
			t.Fatal(current)
		}
	}
	if e := s.SaveResults(ctx, ops, []Result{{ID: ops[1].ID, Status: "rejected", Error: json.RawMessage(`{"code":"LIMIT"}`)}, {ID: ops[0].ID, Status: "applied"}}); e != nil {
		t.Fatal(e)
	}
	pending, _ := s.Outbox(ctx, true)
	if len(pending) != 0 {
		t.Fatal(pending)
	}
}
func TestUncertainIntentCannotBeDiscardedAndBlockedCan(t *testing.T) {
	s := scoped(t)
	ctx := context.Background()
	s.QueueCatalog(ctx, "category", "", "create", json.RawMessage(`{"title":"A"}`), 0)
	ops, _ := s.Outbox(ctx, true)
	if e := s.Discard(ctx, ops[0].ID); e == nil {
		t.Fatal("uncertain queued operation discarded")
	}
	s.SaveResults(ctx, ops, []Result{{ID: ops[0].ID, Status: "retry"}})
	if e := s.Discard(ctx, ops[0].ID); e == nil {
		t.Fatal("uncertain retry discarded")
	}
	ops, _ = s.Outbox(ctx, true)
	s.SaveResults(ctx, ops, []Result{{ID: ops[0].ID, Status: "conflict"}})
	if e := s.Discard(ctx, ops[0].ID); e != nil {
		t.Fatal(e)
	}
	all, _ := s.Outbox(ctx, false)
	if all[0].State != "discarded" {
		t.Fatal(all)
	}
}
func TestBootstrapKeepsOutbox(t *testing.T) {
	s := scoped(t)
	ctx := context.Background()
	s.QueueCatalog(ctx, "category", "", "create", json.RawMessage(`{"title":"A"}`), 0)
	before, _ := s.Outbox(ctx, true)
	if e := s.Bootstrap(ctx, map[string][]json.RawMessage{}, "7.0"); e != nil {
		t.Fatal(e)
	}
	after, _ := s.Outbox(ctx, true)
	if len(after) != 1 || after[0].ID != before[0].ID {
		t.Fatal("bootstrap dropped intent")
	}
}
func TestIngredientUnitChangesAreForbidden(t *testing.T) {
	s := scoped(t)
	ctx := context.Background()
	s.ApplyPage(ctx, []Change{{EntityType: "ingredient", EntityID: entityA, Version: 1}}, "1.0")
	if _, e := s.QueueCatalog(ctx, "ingredient", entityA, "update", json.RawMessage(`{"base_unit":"kg"}`), 1); e == nil {
		t.Fatal("offline rescale accepted")
	}
}
