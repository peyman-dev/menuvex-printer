package desktop

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sinaqaffari/coffish/desktop/internal/store"
)

// TestWasteVoidRestoresStock چرخه ضایعات را قفل می‌کند: ثبت با دلیل
// استاندارد وب (کد + یادداشت جدا)، ابطال با برگشت موجودی و رد ابطال دوباره.
func TestWasteVoidRestoresStock(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	ingID, err := a.store.SaveLocalRecord(ctx, "ingredient", "", map[string]any{
		"title": "شیر", "base_unit": "ml", "current_stock": "1000", "average_cost": "200",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.RecordStockMovement(ctx, ingID, "waste", "250", "", "expired::شیر ترش شده بود"); err != nil {
		t.Fatal(err)
	}
	ing, _ := a.store.Entity(ctx, "ingredient", ingID)
	if ing.Field("current_stock") != "750.00" {
		t.Fatalf("stock after waste = %q, want 750.00", ing.Field("current_stock"))
	}
	events, _ := a.store.Entities(ctx, "waste_event")
	if len(events) != 1 {
		t.Fatalf("waste events = %d", len(events))
	}
	ev := events[0]
	if ev.Field("reason") != "expired" || ev.Field("description") != "شیر ترش شده بود" {
		t.Fatalf("reason/desc split wrong: %q / %q", ev.Field("reason"), ev.Field("description"))
	}
	if err := a.store.VoidWasteEvent(ctx, ev.ID); err != nil {
		t.Fatal(err)
	}
	ing, _ = a.store.Entity(ctx, "ingredient", ingID)
	if ing.Field("current_stock") != "1000.00" {
		t.Fatalf("stock after void = %q, want 1000.00", ing.Field("current_stock"))
	}
	ev2, _ := a.store.Entity(ctx, "waste_event", ev.ID)
	if ev2.Field("status") != "voided" {
		t.Fatalf("waste status = %q, want voided", ev2.Field("status"))
	}
	if err := a.store.VoidWasteEvent(ctx, ev.ID); err == nil {
		t.Fatal("double void must fail")
	}
}

// TestAnalyticsSincePeriod گزارش دوره‌ای: سفارش امروز در بازه فردا نیست.
func TestAnalyticsSincePeriod(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	p1 := posProduct(t, a, "اسپرسو", "100000")
	id, err := a.store.CreatePOSOrder(ctx, store.POSOrderInput{
		ClientToken:   "rpt-1",
		PaymentMethod: "cash",
		Items:         []store.POSOrderLineInput{{ProductID: p1, Quantity: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.UpdateLocalOrderStatus(ctx, id, "completed"); err != nil {
		t.Fatal(err)
	}
	all := a.store.Analytics(ctx)
	if all.TotalSalesIRR != "100000" {
		t.Fatalf("all-time sales = %q", all.TotalSalesIRR)
	}
	future := a.store.AnalyticsSince(ctx, time.Now().Add(24*time.Hour))
	if future.TotalSalesIRR != "0" || future.CompletedOrders != 0 {
		t.Fatalf("future range must be empty: sales=%q completed=%d", future.TotalSalesIRR, future.CompletedOrders)
	}
	today := a.store.AnalyticsSince(ctx, time.Now().Add(-time.Hour))
	if today.TotalSalesIRR != "100000" {
		t.Fatalf("today range sales = %q", today.TotalSalesIRR)
	}
}

// TestDialogsAndPagesParityRender رندر دیالوگ انتخاب مشتری/گروه و سفارش‌های
// صندوق، Deselect میز و صفحات ضایعات/رسپی/دسته‌بندی/گزارش را قفل می‌کند.
func TestDialogsAndPagesParityRender(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	if _, err := a.store.SaveLocalRecord(ctx, "customer", "", map[string]any{"full_name": "مشتری آزمایشی", "phone": "09120000001"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.SaveLocalRecord(ctx, "customer_group", "", map[string]any{"title": "شرکت نمونه", "credit_enabled": "true", "discount_value": "10", "discount_type": "percentage", "is_active": "true"}); err != nil {
		t.Fatal(err)
	}
	catID, err := a.store.SaveLocalRecord(ctx, "category", "", map[string]any{"title": "بار گرم", "slug": "hot-bar"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.SaveLocalRecord(ctx, "product", "", map[string]any{"title": "لاته", "price": "120000", "category_id": catID}); err != nil {
		t.Fatal(err)
	}
	get := func(path string) string {
		r := httptest.NewRequest(http.MethodGet, "http://localhost"+path, nil)
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s -> %d", path, w.Code)
		}
		return w.Body.String()
	}

	pos := get("/?page=pos")
	for _, want := range []string{
		"انتخاب مشتری برای سفارش", "مشتریان و اعضا", "گروه و شرکت",
		"posClubPickMember", "posClubPickGroup", "posClubPickGuest",
		"مشتری آزمایشی", "شرکت نمونه", "نسیه فعال",
		"posClearTable", // حذف انتخاب میز
		"در جریان", "بدون میز", "تکمیل‌شده", "جستجوی شماره سفارش، مشتری یا میز",
		"posRecentTab", "posRecentToggle",
	} {
		if !strings.Contains(pos, want) {
			t.Fatalf("pos page missing %q", want)
		}
	}

	waste := get("/?page=waste")
	for _, want := range []string{"مدیریت ضایعات", "dlg-waste-new", "ثبت ضایعات", "هزینه کل ضایعات", "wasteApplyFilter", "منقضی شده"} {
		if !strings.Contains(waste, want) {
			t.Fatalf("waste page missing %q", want)
		}
	}

	recipes := get("/?page=recipes")
	for _, want := range []string{"دستورهای تهیه", "dlg-recipe", "recipeAddRow", "recipeEdit", "دستور تهیه جدید"} {
		if !strings.Contains(recipes, want) {
			t.Fatalf("recipes page missing %q", want)
		}
	}

	cats := get("/?page=categories")
	for _, want := range []string{"دسته‌بندی‌های منو", "dlg-category", "catEdit", "تعداد محصولات", "ایجاد دسته‌بندی"} {
		if !strings.Contains(cats, want) {
			t.Fatalf("categories page missing %q", want)
		}
	}

	reports := get("/?page=reports&period=today")
	for _, want := range []string{"گزارشات روزانه", "وضعیت امروز", "rpt-sticky", "انتخاب دوره", "روزانه", "هفتگی", "ماهانه", "rpt-chip active", "خلاصه دوره", "عملکرد محصولات", "تراکنش‌های اخیر", "سفارش‌های اخیر", "نمودار کامل فروش", "report-chart", "وضعیت سفارشات", "report-orders", "report-transactions"} {
		if !strings.Contains(reports, want) {
			t.Fatalf("reports page missing %q", want)
		}
	}
	if strings.Contains(reports, "بدون فروش در این دوره") {
		t.Fatal("reports page must not render the unsold-products section")
	}
	reportsNav := strings.Index(reports, "گزارشات روزانه")
	inventoryNav := strings.Index(reports, "انبار و آماده‌سازی")
	if reportsNav < 0 || inventoryNav < 0 || reportsNav > inventoryNav {
		t.Fatal("reports navigation must appear before inventory")
	}
	transactionsPos := strings.Index(reports, `id="report-transactions"`)
	recentOrdersPos := strings.Index(reports, `id="report-orders"`)
	if transactionsPos < 0 || recentOrdersPos < 0 || transactionsPos > recentOrdersPos {
		t.Fatal("recent orders must be below recent transactions in the reports sidebar")
	}
}

// TestMenusPricingWasteAndProductFormParity برش «منو و قیمت‌گذاری جلو چشم،
// واحد مصرف داینامیک، دسته‌بندی انبار و فرم محصول وب» را قفل می‌کند.
func TestMenusPricingWasteAndProductFormParity(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	catID, err := a.store.SaveLocalRecord(ctx, "category", "", map[string]any{"title": "بار گرم"})
	if err != nil {
		t.Fatal(err)
	}
	ingID, err := a.store.SaveLocalRecord(ctx, "ingredient", "", map[string]any{
		"name": "شیر تست", "base_unit": "ml", "current_stock": "5000", "average_cost": "500", "category": "لبنیات",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.SaveLocalRecord(ctx, "ingredient_category", "", map[string]any{"title": "لبنیات", "sort_order": "1", "is_active": true}); err != nil {
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

	home := get("/?page=home")
	if strings.Contains(home, "ذخیره‌سازی محلی فعال") {
		t.Fatal("local-storage chip must not be shown")
	}
	if strings.Contains(home, "page=analytics") || strings.Contains(home, ">تحلیل فروش<") {
		t.Fatal("analytics must be hidden from the sidebar")
	}
	menuPos := strings.Index(home, "منو و قیمت‌گذاری")
	reportsPos := strings.Index(home, "گزارش و مالی")
	inventoryPos := strings.Index(home, "انبار و آماده‌سازی")
	if menuPos < 0 || reportsPos < 0 || inventoryPos < 0 || !(menuPos < reportsPos && reportsPos < inventoryPos) {
		t.Fatal("menu & pricing section must sit above reports and inventory in the sidebar")
	}
	if !strings.Contains(home, "page=inv-categories") || !strings.Contains(home, "دسته‌بندی مواد اولیه") {
		t.Fatal("sidebar must link inventory categories")
	}

	invCats := get("/?page=inv-categories")
	for _, want := range []string{"دسته‌بندی مواد اولیه", "inv-cat-table", "اقلام منتسب", "ترتیب نمایش", "dlg-inv-category", "دسته‌بندی جدید انبار", "لبنیات", "۱ قلم"} {
		if !strings.Contains(invCats, want) {
			t.Fatalf("inv-categories page missing %q", want)
		}
	}

	pricing := get("/?page=pricing")
	for _, want := range []string{"واحد مصرف", "sp-unit", "spIngChange", "spUnitChange", "data-unit=", "واحد مصرف هر ماده با انتخابش از انبار"} {
		if !strings.Contains(pricing, want) {
			t.Fatalf("pricing page missing %q", want)
		}
	}

	waste := get("/?page=waste")
	for _, want := range []string{"دلیل ضایعات", "وضعیت رویداد", "waste-status", "بازنشانی فیلترها", "waste-nomatch"} {
		if !strings.Contains(waste, want) {
			t.Fatalf("waste page missing %q", want)
		}
	}

	form := get("/?page=product-form")
	for _, want := range []string{"اطلاعات محصول", "قیمت‌گذاری محصول", "تنوع‌های محصول", "موجودی محصول", "دستور تهیه", "pf-variants", "pf-recipe-lines", "variants_present", "recipe_present", "اسپرسو ۵۰/۵۰"} {
		if !strings.Contains(form, want) {
			t.Fatalf("product form missing %q", want)
		}
	}

	// ساخت محصول با تنوع و دستور تهیه: تنوع روی خود محصول می‌نشیند، رسپی جدا
	// ذخیره می‌شود و موجودی مستقیم برای محصول دارای رسپی ثبت نمی‌شود.
	formBody := "csrf=" + a.csrf + "&kind=product&return_page=products" +
		"&title=اسپرسو تست&category_id=" + catID + "&price=2000000" +
		"&variants_present=1&variant_title=اسپرسو ۵۰/۵۰&variant_value=250000&variant_active=0" +
		"&recipe_present=1&recipe_ingredient_id=" + ingID + "&recipe_quantity=200&stock=50"
	req := httptest.NewRequest("POST", "http://localhost/local/save", strings.NewReader(formBody))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 200 && w.Code != 303 {
		t.Fatalf("POST /local/save -> %d", w.Code)
	}
	products, _ := a.store.Entities(ctx, "product")
	var prod *store.Entity
	for i := range products {
		if products[i].Label() == "اسپرسو تست" {
			prod = &products[i]
		}
	}
	if prod == nil {
		t.Fatal("product was not created")
	}
	if !strings.Contains(prod.Field("variants"), "اسپرسو ۵۰/۵۰") {
		t.Fatalf("variants not stored on product: %q", prod.Field("variants"))
	}
	if prod.Field("stock") != "" {
		t.Fatalf("recipe product must not keep direct stock, got %q", prod.Field("stock"))
	}
	recipes, _ := a.store.Entities(ctx, "recipe")
	found := false
	for _, rec := range recipes {
		if rec.Field("product_id") == prod.ID && strings.Contains(rec.Field("items"), ingID) {
			found = true
		}
	}
	if !found {
		t.Fatal("recipe was not stored for the created product")
	}
}
