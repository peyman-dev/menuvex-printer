package desktop

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sinaqaffari/coffish/desktop/internal/store"
)

// TestDashboardMatchesWebHome داشبورد وب (KPI امروز، چارت درآمد/هزینه، نبض عملیات، پرفروش‌ها) را قفل می‌کند.
func TestDashboardMatchesWebHome(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	if _, err := a.store.SaveLocalRecord(ctx, "product", "", map[string]any{"title": "لاته داشبورد", "price": "1000000"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.SaveLocalRecord(ctx, "ingredient", "", map[string]any{"name": "شیر تست", "base_unit": "ml", "current_stock": "10", "minimum_stock": "500"}); err != nil {
		t.Fatal(err)
	}
	prods, _ := a.store.Entities(ctx, "product")
	orderID, err := a.store.CreatePOSOrder(ctx, store.POSOrderInput{Items: []store.POSOrderLineInput{{ProductID: prods[0].ID, Quantity: 2}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.UpdateLocalOrderStatus(ctx, orderID, "completed"); err != nil {
		t.Fatal(err)
	}
	get := func(path string) string {
		r := httptest.NewRequest("GET", "http://localhost"+path, nil)
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("GET %s -> %d", path, w.Code)
		}
		return w.Body.String()
	}
	html := get("/")
	for _, want := range []string{
		"خلاصه وضعیت کافه", "نمای عملیاتی امروز", "درآمد امروز", "سفارش‌های امروز", "میانگین فاکتور",
		"موجودی کم", "سفارش‌های در انتظار", "گزارش درآمد و هزینه", "نبض عملیات امروز",
		"آخرین سفارش‌ها", "محصولات پرفروش امروز", "لاته داشبورد", "chart-line-income", "<svg",
		"۲۰۰٬۰۰۰", // ۲ × ۱٬۰۰۰٬۰۰۰ ریال = ۲۰۰٬۰۰۰ تومان، سه‌رقم جداشده
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("dashboard missing %q", want)
		}
	}
	for _, rng := range []string{"today", "7d", "30d"} {
		page := get("/?page=home&range=" + rng)
		if !strings.Contains(page, "chart-line-income") {
			t.Fatalf("chart range %s has no income line", rng)
		}
	}
}
