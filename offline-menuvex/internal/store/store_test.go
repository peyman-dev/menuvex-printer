package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, e := Open(filepath.Join(t.TempDir(), "local.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func TestLegacyStockCountPersistsWithExactUnit(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "local.sqlite")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.AddLegacyStockCount(ctx, LegacyStockCount{Name: "Tea", Quantity: "100.0000", Unit: "g", Note: "not 100 kg"}); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	d, e := s.LegacyStockCounts(ctx)
	if e != nil || len(d) != 1 || d[0].Quantity != "100.0000" || d[0].Unit != "g" {
		t.Fatalf("%+v %v", d, e)
	}
}
func TestBootstrapPreservesLegacyStockCountAndReplacesSnapshot(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	s.AddLegacyStockCount(ctx, LegacyStockCount{Name: "legacy count", Quantity: "1", Unit: "g"})
	s.ApplyPage(ctx, []Change{{EntityType: "product", EntityID: "old", Version: 1}}, "1.1")
	if e := s.Bootstrap(ctx, map[string][]json.RawMessage{"products": {json.RawMessage(`{"id":"new","version":2,"price":"123.456"}`)}}, "7.0"); e != nil {
		t.Fatal(e)
	}
	es, _ := s.Entities(ctx, "product")
	ds, _ := s.LegacyStockCounts(ctx)
	cur, _ := s.Cursor(ctx)
	if len(es) != 1 || es[0].ID != "new" || len(ds) != 1 || cur != "7.0" {
		t.Fatalf("%+v %+v %s", es, ds, cur)
	}
}
func TestPageRollbackIncludesCursor(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	s.ApplyPage(ctx, nil, "1.0")
	e := s.ApplyPage(ctx, []Change{{EntityType: "product", EntityID: "valid", Version: 1}, {EntityType: "product", EntityID: ""}}, "2.0")
	cur, _ := s.Cursor(ctx)
	es, _ := s.Entities(ctx, "product")
	if e == nil || cur != "1.0" || len(es) != 0 {
		t.Fatalf("%v %s %+v", e, cur, es)
	}
}
func TestTombstoneCannotBeResurrectedByStaleVersion(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	for i, c := range []Change{{EntityType: "product", EntityID: "p", Version: 2}, {EntityType: "product", EntityID: "p", Version: 3, Tombstone: true}, {EntityType: "product", EntityID: "p", Version: 2}} {
		if e := s.ApplyPage(ctx, []Change{c}, string(rune('a'+i))); e != nil {
			t.Fatal(e)
		}
	}
	es, _ := s.Entities(ctx, "product")
	if len(es) != 0 {
		t.Fatal("stale resurrection", es)
	}
}
func TestScopeCannotChange(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if e := s.Bind(ctx, "https://a|device-a"); e != nil {
		t.Fatal(e)
	}
	if e := s.Bind(ctx, "https://a|device-a"); e != nil {
		t.Fatal(e)
	}
	if e := s.Bind(ctx, "https://b|device-b"); e == nil {
		t.Fatal("cross-device database reuse allowed")
	}
}
func TestBadBootstrapDoesNotEraseSnapshot(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	s.ApplyPage(ctx, []Change{{EntityType: "product", EntityID: "p", Version: 1}}, "1.0")
	e := s.Bootstrap(ctx, map[string][]json.RawMessage{"products": {json.RawMessage(`{"version":2}`)}}, "2.0")
	es, _ := s.Entities(ctx, "product")
	cur, _ := s.Cursor(ctx)
	if e == nil || len(es) != 1 || cur != "1.0" {
		t.Fatalf("%v %+v %s", e, es, cur)
	}
}

func TestOrderStatusEventsAreVisibleAlongsideOrders(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	err := s.ApplyPage(ctx, []Change{{EntityType: "order", EntityID: "o", Payload: json.RawMessage(`{"status":"pending"}`)}, {EntityType: "order_status", EntityID: "o", Payload: json.RawMessage(`{"status":"completed"}`)}}, "2.0")
	if err != nil {
		t.Fatal(err)
	}
	es, err := s.Entities(ctx, "order")
	if err != nil || len(es) != 2 {
		t.Fatalf("status hidden: %+v %v", es, err)
	}
}
