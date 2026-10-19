package desktop

import (
	"context"
	"encoding/json"
	"github.com/sinaqaffari/coffish/desktop/internal/store"
	"github.com/sinaqaffari/coffish/desktop/internal/syncclient"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func testApp(t *testing.T) *Application {
	t.Helper()
	for _, k := range []string{"COFFISH_API_URL", "COFFISH_DEVICE_ID", "COFFISH_DEVICE_TOKEN", "COFFISH_ALLOWED_HOSTS"} {
		t.Setenv(k, "")
	}
	a, e := New(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	a.authDisabled = true // تست‌های خودکار از گیت لایسنس/ورود عبور می‌کنند
	t.Cleanup(func() { a.Close() })
	return a
}
func TestAllOfflineViewsAndAssets(t *testing.T) {
	a := testApp(t)
	for _, path := range []string{"/", "/?page=products", "/?page=ingredients", "/?page=orders", "/?page=staff", "/?page=staff-checklist", "/?page=finance", "/static/app.css", "/static/htmx.min.js"} {
		r := httptest.NewRequest("GET", "http://localhost"+path, nil)
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(path, w.Code, w.Body.String())
		}
	}
	// صفحات «مرجع سرویس‌های داخلی» و «تاریخچه تراکنش‌ها» طبق تصمیم محصول
	// کاملاً حذف شده‌اند و نباید دیگر نمایش داده شوند.
	for _, path := range []string{"/?page=endpoints", "/?page=endpoint", "/?page=outbox", "/?page=drafts"} {
		r := httptest.NewRequest("GET", "http://localhost"+path, nil)
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		if w.Code != 404 {
			t.Fatalf("removed page %s must be 404, got %d", path, w.Code)
		}
	}
}
func TestRejectsHostAndCSRF(t *testing.T) {
	a := testApp(t)
	for _, tc := range []struct{ host, method, path string }{{"evil.test", "GET", "/"}, {"localhost", "POST", "/local/movement"}} {
		r := httptest.NewRequest(tc.method, "http://"+tc.host+tc.path, strings.NewReader("name=Tea&quantity=100&unit=g"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal(tc, w.Code)
		}
	}
}
func TestRemovedDraftsPageAndEndpoints(t *testing.T) {
	a := testApp(t)
	for _, tc := range []struct{ method, path string }{
		{"GET", "/?page=drafts"},
		{"POST", "/drafts?page=drafts"},
		{"POST", "/drafts/delete"},
	} {
		r := httptest.NewRequest(tc.method, "http://localhost"+tc.path, strings.NewReader("csrf="+a.csrf))
		if tc.method == "POST" {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		if tc.method == "GET" && w.Code != 404 {
			t.Fatalf("removed drafts page %s -> %d, want 404", tc.path, w.Code)
		}
		if tc.method == "POST" && w.Code != 404 && w.Code != 405 {
			t.Fatalf("removed drafts endpoint %s -> %d, want 404/405", tc.path, w.Code)
		}
	}
	r := httptest.NewRequest("GET", "http://localhost/", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if strings.Contains(w.Body.String(), "page=drafts") || strings.Contains(w.Body.String(), "برگه شمارش انبارگردانی") {
		t.Fatal("sidebar must not link to the removed stock-count sheet")
	}
}

func TestStockChangeAdjustmentSetsCountedStock(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	id, err := a.store.SaveLocalRecord(ctx, "ingredient", "", map[string]any{
		"name": "دانه قهوه تست", "base_unit": "g", "current_stock": "100",
	})
	if err != nil {
		t.Fatal(err)
	}
	post := func(values url.Values) int {
		r := httptest.NewRequest("POST", "http://localhost/local/movement", strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		return w.Code
	}
	v := url.Values{"csrf": {a.csrf}, "ingredient_id": {id}, "direction": {"adjustment"}, "quantity": {"12.34"}, "reason": {"اصلاح شمارش"}, "return_page": {"ingredients"}}
	if code := post(v); code != 303 && code != 200 {
		t.Fatalf("stock-count adjustment -> %d", code)
	}
	ing, err := a.store.Entity(ctx, "ingredient", id)
	if err != nil {
		t.Fatal(err)
	}
	if got := ing.Field("current_stock"); got != "12.34" {
		t.Fatalf("counted stock = %q, want 12.34", got)
	}
	movs, _ := a.store.Entities(ctx, "inventory_movement")
	if len(movs) != 1 || movs[0].Field("transaction_type") != "adjustment" || movs[0].Field("quantity") != "-87.66" || movs[0].Field("stock_after") != "12.34" || movs[0].Field("description") != "اصلاح شمارش" {
		t.Fatalf("adjustment movement = %+v", movs)
	}
	v.Set("quantity", "1.234")
	_ = post(v)
	ing, _ = a.store.Entity(ctx, "ingredient", id)
	movs, _ = a.store.Entities(ctx, "inventory_movement")
	if ing.Field("current_stock") != "12.34" || len(movs) != 1 {
		t.Fatal("quantity with more than two decimals must not change stock")
	}
}

func TestNativeHostsAndNewOfflineScreens(t *testing.T) {
	a := testApp(t)
	for _, host := range []string{"wails", "wails.localhost", "[::1]:8787", "localhost:34115"} {
		// صفحه بنرها طبق تصمیم محصول حذف شده است؛ «میزها» و «قیمت‌گذاری» جایش را گرفته‌اند.
		for _, path := range []string{"/?page=categories", "/?page=tables", "/?page=pricing", "/?page=recipes", "/?page=finance", "/?page=editor&kind=category", "/?page=editor&kind=product", "/?page=editor&kind=ingredient", "/reference/openapi.json"} {
			r := httptest.NewRequest("GET", "http://"+host+path, nil)
			w := httptest.NewRecorder()
			a.Handler().ServeHTTP(w, r)
			if w.Code != 200 {
				t.Fatalf("%s %s: %d %s", host, path, w.Code, w.Body.String())
			}
		}
	}
}
func TestStructuredOfflineEditQueuesWithoutNetwork(t *testing.T) {
	a := testApp(t)
	c, e := syncclient.New("http://127.0.0.1:1", "device", "never-render-this-secret")
	if e != nil {
		t.Fatal(e)
	}
	a.client = c
	a.store.Bind(context.Background(), c.BaseURL+"|device")
	v := url.Values{"csrf": {a.csrf}, "kind": {"category"}, "action": {"create"}, "version": {"0"}, "mode": {"form"}, "title": {"Tea"}, "description": {"Saved while disconnected"}}
	r := httptest.NewRequest("POST", "http://localhost/catalog?page=editor&kind=category", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "queued") || strings.Contains(w.Body.String(), "never-render-this-secret") {
		t.Fatal(w.Code, w.Body.String())
	}
	pending, e := a.store.Outbox(context.Background(), true)
	if e != nil || len(pending) != 1 || !strings.Contains(string(pending[0].Payload), "Tea") {
		t.Fatal(pending, e)
	}
}
func TestEditorDoesNotSilentlyRebaseStaleForm(t *testing.T) {
	a := testApp(t)
	a.client, _ = syncclient.New("http://127.0.0.1:1", "device", "secret")
	a.store.Bind(context.Background(), a.client.BaseURL+"|device")
	id := "11111111-1111-4111-8111-111111111111"
	a.store.ApplyPage(context.Background(), []store.Change{{EntityType: "category", EntityID: id, Version: 2, Payload: json.RawMessage(`{"title":"Server edit"}`)}}, "1.0")
	v := url.Values{"csrf": {a.csrf}, "kind": {"category"}, "id": {id}, "action": {"update"}, "version": {"1"}, "payload": {`{"title":"Old form edit"}`}}
	r := httptest.NewRequest("POST", "http://localhost/catalog?page=editor&kind=category&id="+id, strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "record changed while editing") || !strings.Contains(w.Body.String(), `name="version" value="1"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	ops, _ := a.store.Outbox(context.Background(), true)
	if len(ops) != 0 {
		t.Fatal("stale form queued")
	}
}

func TestRemovedReferencePagesReturn404(t *testing.T) {
	// «مرجع سرویس‌های داخلی» و «تاریخچه تراکنش‌ها» طبق تصمیم محصول حذف شده‌اند؛
	// هیچ صفحه مرجعی نباید دیگر رندر شود.
	a := testApp(t)
	for _, path := range []string{"/?page=endpoints", "/?page=endpoint&method=GET&path=%2Fapi%2Fv1%2Fproducts", "/?page=outbox"} {
		r := httptest.NewRequest("GET", "http://localhost"+path, nil)
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		if w.Code != 404 {
			t.Fatalf("removed reference page %s must be 404, got %d", path, w.Code)
		}
		if strings.Contains(w.Body.String(), "مرجع کامل نقاط پایانی") || strings.Contains(w.Body.String(), "Declared security") {
			t.Fatalf("removed reference page %s still renders content", path)
		}
	}
}
