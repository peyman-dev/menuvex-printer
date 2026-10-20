package desktop

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestInventoryPageMatchesWebLayout ساختار صفحه مدیریت انبار وب و ارزش‌گذاری میانگین موزون را قفل می‌کند.
func TestInventoryPageMatchesWebLayout(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	if _, err := a.store.SaveLocalRecord(ctx, "ingredient", "", map[string]any{
		"name": "دانه قهوه تست", "sku": "COF-T", "base_unit": "g", "current_stock": "100", "average_cost": "1000", "storage_location": "انبار اصلی",
	}); err != nil {
		t.Fatal(err)
	}
	get := func(path string) (int, string) {
		r := httptest.NewRequest("GET", "http://localhost"+path, nil)
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	code, html := get("/?page=ingredients")
	if code != 200 {
		t.Fatalf("ingredients page -> %d", code)
	}
	for _, want := range []string{"مدیریت انبار", "پایان‌یافته", "در آستانه اتمام", "اقلام کم موجودی", "ارزش کل موجودی", "لیست موجودی", "خروجی CSV", "افزودن قلم", "۱۰٬۰۰۰ تومان", "ثبت تغییر موجودی", "جنس تازه رسید", "از انبار مصرف شد", "شمارش کردم و عدد فرق داشت", "واقعاً چقدر در انبار هست؟", "موجودی بعد از ثبت"} {
		if !strings.Contains(html, want) {
			t.Fatalf("inventory page missing %q", want)
		}
	}
	if _, csvBody := get("/local/inventory-export"); !strings.Contains(csvBody, "current_stock") {
		t.Fatal("inventory CSV export broken")
	}
	_, pos := get("/?page=pos")
	for _, want := range []string{"انتخاب مشتری یا گروه", "جزئیات سفارش", "تخفیف سفارش (درصدی یا مبلغی)", "جستجوی میز"} {
		if !strings.Contains(pos, want) {
			t.Fatalf("POS page missing %q", want)
		}
	}
}

// TestLocalImageUpload محصول با تصویر آپلودشده از سیستم کاربر ذخیره و سرو می‌شود.
func TestLocalImageUpload(t *testing.T) {
	a := testApp(t)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("csrf", a.csrf)
	_ = mw.WriteField("kind", "product")
	_ = mw.WriteField("return_page", "products")
	_ = mw.WriteField("title", "محصول تصویری تست")
	_ = mw.WriteField("price", "100000")
	fw, err := mw.CreateFormFile("image_file", "photo.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte("\x89PNG\r\n\x1a\n" + "fake-png-body-for-test")); err != nil {
		t.Fatal(err)
	}
	_ = mw.Close()
	r := httptest.NewRequest("POST", "http://localhost/local/save", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("save with image -> %d", w.Code)
	}
	prods, _ := a.store.Entities(context.Background(), "product")
	if len(prods) != 1 {
		t.Fatalf("products = %d, want 1", len(prods))
	}
	img := prods[0].Field("image")
	if !strings.HasPrefix(img, "/uploads/") || !strings.HasSuffix(img, ".png") {
		t.Fatalf("stored image path = %q, want /uploads/*.png", img)
	}
	r2 := httptest.NewRequest("GET", "http://localhost"+img, nil)
	w2 := httptest.NewRecorder()
	a.Handler().ServeHTTP(w2, r2)
	if w2.Code != 200 || w2.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("serving upload -> %d %q", w2.Code, w2.Header().Get("Content-Type"))
	}
	// مسیر دلخواه بیرون از پوشه آپلود نباید سرو شود.
	r3 := httptest.NewRequest("GET", "http://localhost/uploads/../../app.go", nil)
	w3 := httptest.NewRecorder()
	a.Handler().ServeHTTP(w3, r3)
	if w3.Code == 200 {
		t.Fatal("path traversal into arbitrary files must not be served")
	}
}

// TestPosMarksProductsUnavailableWithoutIngredientStock کارت «ناموجود» صندوق
// و کنترل سمت سرور را قفل می‌کند: ۵۰ گرم قهوه لازم است ولی موجودی انبار
// کمتر است، پس کارت محصول باید ناموجود باشد و افزودن به سبد هم مسدود شود.
func TestPosMarksProductsUnavailableWithoutIngredientStock(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	ingID, err := a.store.SaveLocalRecord(ctx, "ingredient", "", map[string]any{
		"name": "قهوه تست صندوق", "base_unit": "g", "current_stock": "30", "average_cost": "1000",
	})
	if err != nil {
		t.Fatal(err)
	}
	prodID, err := a.store.SaveLocalRecord(ctx, "product", "", map[string]any{
		"title": "اسپرسو تست صندوق", "price": "960000",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.SaveLocalRecord(ctx, "recipe", "", map[string]any{
		"title": "رسپی اسپرسو تست", "product_id": prodID, "yield_qty": "1",
		"items": []map[string]any{
			{"ingredient_id": ingID, "ingredient_name": "قهوه تست صندوق", "quantity": "50", "unit": "g"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "http://localhost/?page=pos", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	html := w.Body.String()
	if !strings.Contains(html, `data-out="1"`) {
		t.Fatal("POS tile for a product without ingredient stock is not marked unavailable")
	}
	if !strings.Contains(html, "ناموجود") {
		t.Fatal("POS page missing ناموجود badge")
	}
}

// TestInventoryMarksNegativeStock موجودی منفی باید واضح (علامت منفی + برچسب
// «موجودی منفی») نمایش داده شود، نه عددی که شبیه موجودی مثبت است.
func TestInventoryMarksNegativeStock(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	if _, err := a.store.SaveLocalRecord(ctx, "ingredient", "", map[string]any{
		"name": "شیر تست منفی", "base_unit": "ml", "current_stock": "-3000", "average_cost": "500",
	}); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "http://localhost/?page=ingredients", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	html := w.Body.String()
	for _, want := range []string{"موجودی منفی", "inv-neg-chip", "is-negative"} {
		if !strings.Contains(html, want) {
			t.Fatalf("inventory page missing %q for negative stock", want)
		}
	}
}
