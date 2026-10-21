package syncengine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sinaqaffari/coffish/desktop/internal/store"
	"github.com/sinaqaffari/coffish/desktop/internal/syncclient"
)

// fakeServer یک سرور حداقلی پروتکل v2 است که رفتارهای حیاتی را شبیه‌سازی
// می‌کند: handshake، push با نتیجهٔ applied و pull خالی. سرور واقعی FastAPI
// در offline-menuvex/server با تست‌های pytest خودش پوشش داده شده است.
func fakeServer(t *testing.T, onPush func(ops []map[string]any)) *httptest.Server {
	t.Helper()
	var seq int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data any
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
			return
		case "/api/v1/sync/handshake":
			data = map[string]any{"supported_protocol_version": 2, "bootstrap_required": false,
				"limits": map[string]any{"max_batch_size": 100}}
		case "/api/v1/sync/bootstrap":
			data = map[string]any{"snapshot_cursor": "1", "data": map[string]any{}, "has_more": false}
		case "/api/v1/sync/push":
			var body struct {
				Operations []map[string]any `json:"operations"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if onPush != nil {
				onPush(body.Operations)
			}
			results := make([]map[string]any, 0, len(body.Operations))
			for _, op := range body.Operations {
				results = append(results, map[string]any{"operation_id": op["operation_id"], "status": "applied"})
			}
			atomic.AddInt64(&seq, int64(len(body.Operations)))
			data = map[string]any{"results": results}
		case "/api/v1/sync/pull":
			cur := r.URL.Query().Get("cursor")
			if cur == "" {
				cur = "0"
			}
			data = map[string]any{"next_cursor": cur, "changes": []any{}, "has_more": false}
		case "/api/v1/sync/ack":
			data = map[string]any{}
		default:
			t.Error("unexpected path", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newEngineStore(t *testing.T, srvURL string) (*store.Store, *syncclient.Client) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "offline.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	if err := st.Bind(ctx, srvURL+"|dev-1"); err != nil {
		t.Fatal(err)
	}
	if err := st.SeedIranianCafeData(ctx); err != nil {
		t.Fatal(err)
	}
	c, err := syncclient.New(srvURL, "dev-1", "secret")
	if err != nil {
		t.Fatal(err)
	}
	return st, c
}

// سفارشی که هنگام قطعی ثبت شده، با اولین چرخه به سرور می‌رسد و وضعیت
// «همگام» گزارش می‌شود.
func TestEngineDrainsOfflineOrder(t *testing.T) {
	ctx := context.Background()
	var mu sync.Mutex
	var pushedOrderIDs []string
	srv := fakeServer(t, func(ops []map[string]any) {
		mu.Lock()
		defer mu.Unlock()
		for _, op := range ops {
			if op["entity_type"] == "order" {
				pushedOrderIDs = append(pushedOrderIDs, op["entity_id"].(string))
			}
		}
	})
	st, client := newEngineStore(t, srv.URL)
	orderID, err := st.CreatePOSOrder(ctx, store.POSOrderInput{
		Items: []store.POSOrderLineInput{{ProductID: "22222222-2222-4222-8222-000000000002", Quantity: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	e := New(client, st, nil)
	engCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	e.Start(engCtx)
	defer e.Stop()
	st.SetSyncNotify(e.NotifyLocalChange)
	if err := e.SyncNow(ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	found := false
	for _, id := range pushedOrderIDs {
		if id == orderID {
			found = true
		}
	}
	mu.Unlock()
	if !found {
		t.Fatal("offline order never reached the server")
	}
	status := e.Status(ctx)
	if status.State != StateSynced || status.Pending != 0 {
		t.Fatalf("unexpected status after drain: %+v", status)
	}
	if status.LastSuccess == "" {
		t.Fatal("last successful sync time not recorded")
	}
}

// دو درخواست هم‌زمان نباید دو چرخهٔ هم‌پوشان بسازند.
func TestNoOverlappingCycles(t *testing.T) {
	ctx := context.Background()
	var inFlight, maxInFlight int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := atomic.AddInt64(&inFlight, 1)
		for {
			m := atomic.LoadInt64(&maxInFlight)
			if cur <= m || atomic.CompareAndSwapInt64(&maxInFlight, m, cur) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		atomic.AddInt64(&inFlight, -1)
		var data any = map[string]any{}
		switch r.URL.Path {
		case "/api/v1/sync/handshake":
			data = map[string]any{"supported_protocol_version": 2, "bootstrap_required": false,
				"limits": map[string]any{"max_batch_size": 100}}
		case "/api/v1/sync/pull":
			data = map[string]any{"next_cursor": "1", "changes": []any{}, "has_more": false}
		case "/api/v1/sync/bootstrap":
			data = map[string]any{"snapshot_cursor": "1", "data": map[string]any{}, "has_more": false}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data})
	}))
	defer srv.Close()
	st, client := newEngineStore(t, srv.URL)
	e := New(client, st, nil)
	engCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	e.Start(engCtx)
	defer e.Stop()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = e.SyncNow(ctx)
		}()
	}
	wg.Wait()
	if maxInFlight > 2 {
		// هر Refresh ممکن است خودش چند درخواست ترتیبی بزند؛ هم‌پوشانی
		// چرخه‌ها خودش را با موج درخواست‌های هم‌زمان نشان می‌دهد.
		t.Fatalf("sync cycles overlapped: max concurrent requests = %d", maxInFlight)
	}
}
