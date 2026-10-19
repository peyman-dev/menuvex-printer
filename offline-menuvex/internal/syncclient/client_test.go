package syncclient

import (
	"context"
	"encoding/json"
	"github.com/sinaqaffari/coffish/desktop/internal/store"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestOriginValidation(t *testing.T) {
	for _, base := range []string{"http://remote.test", "https://a/path", "https://u:p@a", "https://a?x=y", "file:///tmp/api"} {
		if _, e := New(base, "d", "secret"); e == nil {
			t.Fatal("accepted", base)
		}
	}
	for _, base := range []string{"https://a", "http://localhost:5000", "http://127.0.0.1:5000"} {
		if _, e := New(base, "d", "secret"); e != nil {
			t.Fatal(base, e)
		}
	}
}
func TestRefreshCommitsBeforeAck(t *testing.T) {
	ctx := context.Background()
	s, e := store.Open(filepath.Join(t.TempDir(), "local.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	calls := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		if r.Header.Get("Authorization") != "Device d:secret" {
			t.Error("wrong auth")
		}
		var data any
		switch r.URL.Path {
		case "/api/v1/sync/handshake":
			data = map[string]any{"supported_protocol_version": 2, "bootstrap_required": true}
		case "/api/v1/sync/bootstrap":
			data = map[string]any{"snapshot_cursor": "7.0", "data": map[string]any{"products": []any{map[string]any{"id": "p", "version": 1, "price": "987654321.1234"}}}}
		case "/api/v1/sync/pull":
			if r.URL.Query().Get("cursor") != "7.0" {
				t.Error("wrong cursor")
			}
			data = map[string]any{"next_cursor": "8.1", "changes": []any{map[string]any{"entity_type": "product", "entity_id": "p", "entity_version": 2, "payload": map[string]any{"id": "p", "price": "1.25"}}}, "has_more": false}
		case "/api/v1/sync/ack":
			cur, _ := s.Cursor(ctx)
			es, _ := s.Entities(ctx, "product")
			if cur != "8.1" || len(es) != 1 || es[0].Version != 2 {
				t.Errorf("acked before commit: %s %+v", cur, es)
			}
			data = map[string]any{}
		default:
			t.Error("unexpected endpoint", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data})
	}))
	defer srv.Close()
	c, _ := New(srv.URL, "d", "secret")
	if e = c.Refresh(ctx, s); e != nil {
		t.Fatal(e)
	}
	if len(calls) != 4 {
		t.Fatal(calls)
	}
}
func TestRedirectDoesNotLeakCredentials(t *testing.T) {
	called := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer source.Close()
	c, _ := New(source.URL, "d", "secret")
	if e := c.request(context.Background(), "GET", "/status", nil, nil); e == nil {
		t.Fatal("redirect accepted")
	}
	if called {
		t.Fatal("followed credential-bearing redirect")
	}
}
func TestFailedApplyNeverAcknowledges(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "local.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	s.ApplyPage(context.Background(), nil, "1.0")
	acked := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := `{"supported_protocol_version":2,"bootstrap_required":false}`
		if strings.HasSuffix(r.URL.Path, "/pull") {
			data = `{"next_cursor":"2.0","changes":[{"entity_type":"product"}]}`
		} else if strings.HasSuffix(r.URL.Path, "/ack") {
			acked = true
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true,"data":` + data + `}`))
	}))
	defer srv.Close()
	c, _ := New(srv.URL, "d", "secret")
	if e := c.Refresh(context.Background(), s); e == nil {
		t.Fatal("bad page accepted")
	}
	cur, _ := s.Cursor(context.Background())
	if acked || cur != "1.0" {
		t.Fatal("bad page committed or acked", acked, cur)
	}
}

func TestMultiplePagesAndAckFailureKeepDurableProgress(t *testing.T) {
	ctx := context.Background()
	s, e := store.Open(filepath.Join(t.TempDir(), "local.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	s.ApplyPage(ctx, nil, "1.0")
	pulls, acks := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data any
		switch {
		case strings.HasSuffix(r.URL.Path, "/handshake"):
			data = map[string]any{"supported_protocol_version": 2, "bootstrap_required": false}
		case strings.HasSuffix(r.URL.Path, "/pull"):
			pulls++
			cursor := "1.0"
			next := "2.0"
			if pulls == 2 {
				cursor = "2.0"
				next = "3.0"
			}
			if r.URL.Query().Get("cursor") != cursor {
				t.Error("wrong resume cursor")
			}
			data = map[string]any{"next_cursor": next, "has_more": pulls == 1, "changes": []any{map[string]any{"entity_type": "product", "entity_id": "p", "entity_version": pulls}}}
		case strings.HasSuffix(r.URL.Path, "/ack"):
			acks++
			if acks == 2 {
				w.WriteHeader(503)
				w.Write([]byte(`{"success":false,"error":{"code":"SYNC_UNAVAILABLE","message":"retry later","retry_class":"retry"}}`))
				return
			}
			data = map[string]any{}
		default:
			t.Error("unexpected", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data})
	}))
	defer srv.Close()
	c, _ := New(srv.URL, "d", "secret")
	if e = c.Refresh(ctx, s); e == nil {
		t.Fatal("ack failure hidden")
	}
	cur, _ := s.Cursor(ctx)
	es, _ := s.Entities(ctx, "product")
	if cur != "3.0" || pulls != 2 || acks != 2 || len(es) != 1 || es[0].Version != 2 {
		t.Fatalf("lost progress: %s %d %d %+v", cur, pulls, acks, es)
	}
}

func TestLostPushResponseReplaysTheSameOperation(t *testing.T) {
	ctx := context.Background()
	s, e := store.Open(filepath.Join(t.TempDir(), "local.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	pushes := 0
	seen := map[string]bool{}
	var original string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data any
		switch {
		case strings.HasSuffix(r.URL.Path, "/handshake"):
			data = map[string]any{"supported_protocol_version": 2, "bootstrap_required": false}
		case strings.HasSuffix(r.URL.Path, "/push"):
			pushes++
			var request struct {
				Operations []store.Operation `json:"operations"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			if len(request.Operations) != 1 {
				t.Fatal("wrong batch")
			}
			op := request.Operations[0]
			if original == "" {
				original = op.ID
			} else if original != op.ID {
				t.Error("retry generated new identity")
			}
			seen[op.ID] = true
			if pushes == 1 {
				w.WriteHeader(503)
				w.Write([]byte(`{"success":false,"error":{"code":"SYNC_UNAVAILABLE","retry_class":"later"}}`))
				return
			}
			data = map[string]any{"results": []any{map[string]any{"operation_id": op.ID, "status": "applied", "replayed": true}}}
		case strings.HasSuffix(r.URL.Path, "/pull"):
			data = map[string]any{"next_cursor": "2.0", "has_more": false, "changes": []any{}}
		case strings.HasSuffix(r.URL.Path, "/ack"):
			data = map[string]any{}
		default:
			t.Error("unexpected", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data})
	}))
	defer srv.Close()
	c, _ := New(srv.URL, "d", "secret")
	s.Bind(ctx, c.BaseURL+"|d")
	s.ApplyPage(ctx, nil, "1.0")
	if _, e = s.QueueCatalog(ctx, "category", "", "create", json.RawMessage(`{"title":"Tea"}`), 0); e != nil {
		t.Fatal(e)
	}
	if e = c.Refresh(ctx, s); e == nil {
		t.Fatal("lost response hidden")
	}
	pending, _ := s.Outbox(ctx, true)
	if len(pending) != 1 {
		t.Fatal("lost intent")
	}
	if e = c.Refresh(ctx, s); e != nil {
		t.Fatal(e)
	}
	pending, _ = s.Outbox(ctx, true)
	all, _ := s.Outbox(ctx, false)
	if len(pending) != 0 || len(seen) != 1 || pushes != 2 || all[0].State != "applied" {
		t.Fatal(pending, seen, pushes, all)
	}
}
func TestCursorAheadRecoversOnceWithoutLosingLegacyStockCounts(t *testing.T) {
	ctx := context.Background()
	s, e := store.Open(filepath.Join(t.TempDir(), "local.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	s.ApplyPage(ctx, nil, "999.0")
	s.AddLegacyStockCount(ctx, store.LegacyStockCount{Name: "keep", Quantity: "100", Unit: "g"})
	pulls, bootstraps := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data any
		switch {
		case strings.HasSuffix(r.URL.Path, "/handshake"):
			data = map[string]any{"supported_protocol_version": 2, "bootstrap_required": false}
		case strings.HasSuffix(r.URL.Path, "/bootstrap"):
			bootstraps++
			data = map[string]any{"snapshot_cursor": "1.0", "data": map[string]any{}}
		case strings.HasSuffix(r.URL.Path, "/pull"):
			pulls++
			if pulls == 1 {
				w.WriteHeader(409)
				w.Write([]byte(`{"success":false,"error":{"code":"SYNC_CURSOR_AHEAD","retry_class":"bootstrap"}}`))
				return
			}
			if r.URL.Query().Get("cursor") != "1.0" {
				t.Error("did not reset cursor")
			}
			data = map[string]any{"next_cursor": "2.0", "has_more": false}
		case strings.HasSuffix(r.URL.Path, "/ack"):
			data = map[string]any{}
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data})
	}))
	defer srv.Close()
	c, _ := New(srv.URL, "d", "secret")
	if e = c.Refresh(ctx, s); e != nil {
		t.Fatal(e)
	}
	legacyCounts, _ := s.LegacyStockCounts(ctx)
	if bootstraps != 1 || pulls != 2 || len(legacyCounts) != 1 {
		t.Fatal(bootstraps, pulls, legacyCounts)
	}
}
