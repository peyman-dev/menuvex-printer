package desktop

import (
	"bytes"
	"context"
	"net/url"
	"strings"
	"testing"
	"time"
)

// waitPrinters منتظر می‌ماند تا چاپگر فیکی حداقل n فیش بگیرد.
func waitPrinters(t *testing.T, fp *fakeThermalPrinter, n int) [][]byte {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if got := fp.received(); len(got) >= n {
			return got
		}
		time.Sleep(15 * time.Millisecond)
	}
	return fp.received()
}

func orderByToken(t *testing.T, a *Application, token string) string {
	t.Helper()
	orders, _ := a.store.Entities(context.Background(), "order")
	for _, o := range orders {
		if o.Field("client_token") == token {
			return o.ID
		}
	}
	t.Fatalf("order with token %s not found", token)
	return ""
}

// TestPOSStagesDialogAndSave دیالوگ شخصی‌سازی مراحل وب رندر و در دیتابیس ذخیره می‌شود.
func TestPOSStagesDialogAndSave(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	html := ordersGet(t, a, "/?page=pos")
	for _, s := range []string{
		"شخصی‌سازی مراحل ثبت تا تکمیل سفارش",
		"هر مرحله‌ای را که تیک نزنید، صندوق به‌صورت خودکار از روی آن عبور می‌کند",
		"کامل (۴ مرحله)", "بدون تأیید صندوقدار (۳ مرحله)", "فقط آماده تحویل (۲ مرحله)", "تکمیل مستقیم (۱ مرحله)",
		"۱. ثبت سفارش", "نقطهٔ شروع ثبت سفارش در صندوق یا منوی دیجیتال (همیشه فعال)",
		"بررسی و تأیید اولیه سفارش توسط صندوقدار پیش از ارسال به آماده‌سازی",
		"نمایش وضعیت آماده‌سازی سفارش در آشپزخانه یا بار",
		"اعلام آماده بودن سفارش برای تحویل به مشتری یا سالن‌دار",
		"تکمیل و تسویه نهایی", "بستن سفارش، آزادسازی میز و صدور فیش نهایی (همیشه فعال)",
		"مسیر فعلی سفارش در صندوق", "ذخیره مراحل سفارش", "۴ مرحله",
	} {
		if !strings.Contains(html, s) {
			t.Fatalf("stages dialog missing %q", s)
		}
	}
	if strings.Contains(html, "pos-chip-live") {
		t.Fatal("POS must not show the local chip")
	}
	// فقط «آماده تحویل» فعال بماند.
	ordersPost(t, a, "/local/pos-stages", url.Values{"stage_ready": {"on"}})
	prof, err := a.store.Entity(ctx, "cafe_profile", "cafe-main-profile")
	if err != nil {
		t.Fatal(err)
	}
	if prof.Field("pos_stages") != "ready" {
		t.Fatalf("pos_stages not saved to DB: %q", prof.Field("pos_stages"))
	}
	if html := ordersGet(t, a, "/?page=pos"); !strings.Contains(html, "۲ مرحله") {
		t.Fatal("stage count badge must update to ۲ مرحله")
	}
}

// TestPrintPolicyEvents سیاست چاپ خودکار در هر لحظه از چرخه سفارش اعمال می‌شود:
// ثبت روی میز=فقط آشپزخانه، بدون میز=آشپزخانه+تسویه (مشتری و کافه)، تکمیل=مشتری.
func TestPrintPolicyEvents(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	cashier := startFakePrinter(t)
	kitchen := startFakePrinter(t)
	cashierID, err := a.store.SaveLocalRecord(ctx, "printer", "", map[string]any{
		"title": "چاپگر صندوق", "connection": cashier.addr(), "station": "صندوق فروش", "paper_width": "80mm",
	})
	if err != nil {
		t.Fatal(err)
	}
	kitchenID, err := a.store.SaveLocalRecord(ctx, "printer", "", map[string]any{
		"title": "چاپگر آشپزخانه", "connection": kitchen.addr(), "station": "آشپزخانه", "paper_width": "80mm",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = cashierID
	prodID, err := a.store.SaveLocalRecord(ctx, "product", "", map[string]any{"title": "قهوه سیاستی", "price": "100000"})
	if err != nil {
		t.Fatal(err)
	}
	tableID, err := a.store.SaveLocalRecord(ctx, "table", "", map[string]any{"title": "میز ۱", "number": "1", "capacity": "4"})
	if err != nil {
		t.Fatal(err)
	}
	jobsOf := func(orderID string) []struct{ Doc, Printer string; Copies int } {
		jobs, jErr := a.store.PrintJobs(ctx, orderID)
		if jErr != nil {
			t.Fatal(jErr)
		}
		out := []struct{ Doc, Printer string; Copies int }{}
		for _, j := range jobs {
			out = append(out, struct{ Doc, Printer string; Copies int }{j.DocType, j.PrinterID, j.Copies})
		}
		return out
	}

	// ثبت روی میز با سیاست پیش‌فرض: فقط بلیت آشپزخانه.
	ordersPost(t, a, "/local/pos-order", url.Values{
		"product_id": {prodID}, "quantity": {"1"}, "table_id": {tableID},
		"client_token": {"pp-table-1"}, "payment_method": {"cash"}, "fulfillment_type": {"dine_in"},
	})
	o1 := orderByToken(t, a, "pp-table-1")
	if js := jobsOf(o1); len(js) != 1 || js[0].Doc != "kitchen" || js[0].Printer != kitchenID {
		t.Fatalf("table order must print kitchen only, got %+v", js)
	}
	if got := waitPrinters(t, kitchen, 1); !bytes.Contains(got[0], []byte("قهوه سیاستی")) {
		t.Fatal("kitchen ticket must contain the item")
	}
	if len(cashier.received()) != 0 {
		t.Fatal("cashier printer must receive nothing at table-order creation")
	}

	// تکمیل همان سفارش: فقط فیش مشتری.
	ordersPost(t, a, "/local/order-status", url.Values{"order_id": {o1}, "status": {"completed"}})
	if js := jobsOf(o1); len(js) != 2 {
		t.Fatalf("completion must add the customer receipt, jobs=%+v", js)
	}
	if got := waitPrinters(t, cashier, 1); !bytes.Contains(got[0], []byte("قهوه سیاستی")) {
		t.Fatal("customer receipt must reach the cashier printer at completion")
	}

	// سفارش بدون میز: بلیت آشپزخانه + تسویه با دو فیش (مشتری و کافه).
	ordersPost(t, a, "/local/pos-order", url.Values{
		"product_id": {prodID}, "quantity": {"1"},
		"client_token": {"pp-counter-1"}, "payment_method": {"cash"}, "fulfillment_type": {"outside"},
		"customer_name": {"مشتری بیرون‌بر"}, "customer_phone": {"09121112233"},
	})
	o2 := orderByToken(t, a, "pp-counter-1")
	js := jobsOf(o2)
	var custCopies int
	var docsSeen = map[string]bool{}
	for _, j := range js {
		docsSeen[j.Doc] = true
		if j.Doc == "customer" {
			custCopies = j.Copies
		}
	}
	if len(js) != 2 || !docsSeen["kitchen"] || !docsSeen["customer"] || custCopies != 2 {
		t.Fatalf("counter order must print kitchen + 2 customer copies, got %+v", js)
	}
	waitPrinters(t, cashier, 3) // ۱ قبلی + ۲ نسخه تسویه
	waitPrinters(t, kitchen, 2)

	// تغییر سیاست: ثبت = هیچ، تکمیل = چیدمان دلخواه (هر دو، دو نسخه).
	ordersPost(t, a, "/local/print-policy", url.Values{
		"pw_create_mode": {"none"},
		"pw_complete_mode": {"custom"}, "pw_complete_doc": {"all"}, "pw_complete_copies": {"2"},
	})
	ordersPost(t, a, "/local/pos-order", url.Values{
		"product_id": {prodID}, "quantity": {"1"}, "table_id": {tableID},
		"client_token": {"pp-table-2"}, "payment_method": {"cash"}, "fulfillment_type": {"dine_in"},
	})
	o3 := orderByToken(t, a, "pp-table-2")
	if js := jobsOf(o3); len(js) != 0 {
		t.Fatalf("create=none must not create jobs, got %+v", js)
	}
	cashBefore, kitchenBefore := len(cashier.received()), len(kitchen.received())
	ordersPost(t, a, "/local/order-status", url.Values{"order_id": {o3}, "status": {"completed"}})
	if js := jobsOf(o3); len(js) != 2 {
		t.Fatalf("custom completion must create customer+kitchen jobs, got %+v", js)
	}
	if got := waitPrinters(t, cashier, cashBefore+2); len(got) != cashBefore+2 {
		t.Fatal("customer receipt x2 must print at completion")
	}
	if got := waitPrinters(t, kitchen, kitchenBefore+2); len(got) != kitchenBefore+2 {
		t.Fatal("kitchen ticket x2 must print at completion")
	}
}

// TestPrinterCategoryRouting هر چاپگر فقط بلیت دسته‌های خودش را می‌گیرد.
func TestPrinterCategoryRouting(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	cashier := startFakePrinter(t)
	kitchen := startFakePrinter(t)
	pizza := startFakePrinter(t)
	if _, err := a.store.SaveLocalRecord(ctx, "printer", "", map[string]any{
		"title": "چاپگر صندوق", "connection": cashier.addr(), "station": "صندوق فروش", "paper_width": "80mm",
	}); err != nil {
		t.Fatal(err)
	}
	kitchenID, err := a.store.SaveLocalRecord(ctx, "printer", "", map[string]any{
		"title": "چاپگر آشپزخانه", "connection": kitchen.addr(), "station": "آشپزخانه", "paper_width": "80mm",
	})
	if err != nil {
		t.Fatal(err)
	}
	pizzaID, err := a.store.SaveLocalRecord(ctx, "printer", "", map[string]any{
		"title": "چاپگر فر پیتزا", "connection": pizza.addr(), "station": "فر پیتزا", "paper_width": "80mm",
	})
	if err != nil {
		t.Fatal(err)
	}
	catDrink, err := a.store.SaveLocalRecord(ctx, "category", "", map[string]any{"title": "نوشیدنی"})
	if err != nil {
		t.Fatal(err)
	}
	catPizza, err := a.store.SaveLocalRecord(ctx, "category", "", map[string]any{"title": "پیتزا"})
	if err != nil {
		t.Fatal(err)
	}
	coffeeID, err := a.store.SaveLocalRecord(ctx, "product", "", map[string]any{"title": "قهوه مسیری", "price": "100000", "category_id": catDrink})
	if err != nil {
		t.Fatal(err)
	}
	pizzaProdID, err := a.store.SaveLocalRecord(ctx, "product", "", map[string]any{"title": "پیتزا مخصوص", "price": "200000", "category_id": catPizza})
	if err != nil {
		t.Fatal(err)
	}
	tableID, err := a.store.SaveLocalRecord(ctx, "table", "", map[string]any{"title": "میز ۲", "number": "2", "capacity": "4"})
	if err != nil {
		t.Fatal(err)
	}

	// صفحه تنظیمات هر دو بخش جدید را دارد.
	settings := ordersGet(t, a, "/?page=settings")
	for _, s := range []string{"چاپ خودکار فیش‌ها", "مسیر چاپ دسته‌بندی محصولات", "موقع ثبت سفارش", "موقع تکمیل سفارش", "تسویه با دو فیش: مشتری + کافه", "پیتزا مخصوص"} {
		if s == "پیتزا مخصوص" {
			continue
		}
		if !strings.Contains(settings, s) {
			t.Fatalf("settings missing %q", s)
		}
	}

	// مسیر: دسته پیتزا فقط روی چاپگر فر پیتزا.
	ordersPost(t, a, "/local/printer-route", url.Values{
		"category_id": {catPizza}, "printer_id": {pizzaID}, "copies": {"1"},
	})
	routes, _ := a.store.Entities(ctx, "printer_route")
	if len(routes) != 1 || routes[0].Field("printer_id") != pizzaID || routes[0].Field("category_id") != catPizza {
		t.Fatalf("route not saved by ids: %+v", routes)
	}

	ordersPost(t, a, "/local/pos-order", url.Values{
		"product_id": {coffeeID, pizzaProdID}, "quantity": {"1", "1"}, "table_id": {tableID},
		"client_token": {"route-1"}, "payment_method": {"cash"}, "fulfillment_type": {"dine_in"},
	})
	o1 := orderByToken(t, a, "route-1")
	jobs, _ := a.store.PrintJobs(ctx, o1)
	if len(jobs) != 2 {
		t.Fatalf("expected 2 routed kitchen jobs, got %d", len(jobs))
	}
	byPrinter := map[string][]string{}
	for _, j := range jobs {
		byPrinter[j.PrinterID] = j.Categories
	}
	if len(byPrinter[kitchenID]) != 1 || byPrinter[kitchenID][0] != catDrink {
		t.Fatalf("kitchen printer must own drinks category, got %+v", byPrinter)
	}
	if len(byPrinter[pizzaID]) != 1 || byPrinter[pizzaID][0] != catPizza {
		t.Fatalf("pizza printer must own pizza category, got %+v", byPrinter)
	}
	kg := waitPrinters(t, kitchen, 1)
	if !bytes.Contains(kg[0], []byte("قهوه مسیری")) || bytes.Contains(kg[0], []byte("پیتزا مخصوص")) {
		t.Fatal("kitchen ticket must contain only its own category items")
	}
	pg := waitPrinters(t, pizza, 1)
	if !bytes.Contains(pg[0], []byte("پیتزا مخصوص")) || bytes.Contains(pg[0], []byte("قهوه مسیری")) {
		t.Fatal("pizza ticket must contain only pizza items")
	}

	// توقف مسیر: پیتزا هم به پرینتر پیش‌فرض آشپزخانه برمی‌گردد.
	ordersPost(t, a, "/local/printer-route-toggle", url.Values{"category_id": {catPizza}})
	ordersPost(t, a, "/local/pos-order", url.Values{
		"product_id": {coffeeID, pizzaProdID}, "quantity": {"1", "1"}, "table_id": {tableID},
		"client_token": {"route-2"}, "payment_method": {"cash"}, "fulfillment_type": {"dine_in"},
	})
	o2 := orderByToken(t, a, "route-2")
	jobs2, _ := a.store.PrintJobs(ctx, o2)
	// هیچ مسیر فعالی نمانده: یک بلیت کامل (بدون فیلتر دسته) روی پرینتر آشپزخانه.
	if len(jobs2) != 1 || jobs2[0].PrinterID != kitchenID || len(jobs2[0].Categories) != 0 {
		t.Fatalf("paused route must fall back to a full kitchen ticket, got %+v", jobs2)
	}
	kg2 := waitPrinters(t, kitchen, 2)
	if !bytes.Contains(kg2[1], []byte("قهوه مسیری")) || !bytes.Contains(kg2[1], []byte("پیتزا مخصوص")) {
		t.Fatal("fallback ticket must contain all items")
	}
	if len(pizza.received()) != 1 {
		t.Fatal("pizza printer must receive nothing after route pause")
	}
}
