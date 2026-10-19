package desktop

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestProductsPageMatchesWebLayout ساختار صفحه محصولات داشبورد وب را قفل می‌کند:
// نوار ابزار، کارت‌های آمار، جست‌وجو/فیلتر و جدول بدون فرم شلوغ ثبت محصول.
func TestProductsPageMatchesWebLayout(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	if _, err := a.store.SaveLocalRecord(ctx, "category", "", map[string]any{"title": "بار گرم"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.SaveLocalRecord(ctx, "product", "", map[string]any{"title": "لاته تست", "price": "1850000", "stock": "3"}); err != nil {
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
	html := get("/?page=products")
	for _, want := range []string{"prod-toolbar-card", "آنالیز قیمت", "دانلود محصولات", "افزودن گروهی", "افزودن محصول", "جستجوی محصول", "کل محصولات", "کم‌موجودی", "ناموجود", "رتبه", "موجودی"} {
		if !strings.Contains(html, want) {
			t.Fatalf("products page missing %q", want)
		}
	}
	if strings.Contains(html, "افزودن محصول جدید به منو") {
		t.Fatal("products list must not embed the create form (web keeps it on a separate page)")
	}
	if !strings.Contains(html, "page=product-form") {
		t.Fatal("products page must link to the dedicated product form page")
	}
	filtered := get("/?page=products&q=" + "لاته")
	if strings.Count(filtered, "prod-name") > strings.Count(html, "prod-name") {
		t.Fatal("search filter must narrow the product list")
	}
	form := get("/?page=product-form")
	for _, want := range []string{"اطلاعات محصول", "قیمت‌گذاری محصول", "تصویر محصول", "دسته‌بندی", "افزودن محصول"} {
		if !strings.Contains(form, want) {
			t.Fatalf("product form missing %q", want)
		}
	}
	csvBody := get("/local/products-export")
	if !strings.Contains(csvBody, "title,category,price_irr") {
		t.Fatal("products export must be a CSV with the import-compatible header")
	}
}
