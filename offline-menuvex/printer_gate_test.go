package desktop

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sinaqaffari/coffish/desktop/internal/store"
)

// TestPrinterOptInGate کاربر خودش تعیین می‌کند از چاپگر استفاده کند یا نه:
// بدون چاپگر/با سوییچ خاموش، هیچ دکمه و Job چاپی نیست؛ با روشن‌کردن،
// پیکربندی چاپگر و چاپ مستقیم فعال می‌شود. فرم «مشخصات کافه» هم دیگر
// در نسخه آفلاین ورودی ندارد.
func TestPrinterOptInGate(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	jobCount := func() int {
		jobs, err := a.store.PrintJobs(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		return len(jobs)
	}

	// حالت پیش‌فرض بدون هیچ چاپگری: چاپ غیرفعال است.
	settings := ordersGet(t, a, "/?page=settings")
	for _, s := range []string{"استفاده از چاپگر حرارتی"} {
		if !strings.Contains(settings, s) {
			t.Fatalf("settings missing %q", s)
		}
	}
	for _, s := range []string{"افزودن فیش‌پرینتر جدید", "ذخیره مشخصات کافه", "name=\"working_hours\"", "name=\"address\""} {
		if strings.Contains(settings, s) {
			t.Fatalf("settings must not contain %q while printer disabled / cafe form removed", s)
		}
	}
	pos := ordersGet(t, a, "/?page=pos")
	if strings.Contains(pos, "pos-print-row") || strings.Contains(pos, "pos-printers-menu") {
		t.Fatal("POS must hide print actions when no printer is used")
	}
	for _, s := range []string{"posApplyCatalog", "posNormText", "pos-no-results", "محصولی پیدا نشد"} {
		if !strings.Contains(pos, s) {
			t.Fatalf("POS search fix missing %q", s)
		}
	}
	// چاپ آزمایشی در حالت غیرفعال Job نمی‌سازد.
	ordersPost(t, a, "/local/print-test", url.Values{"printer_id": {"any"}})
	if jobCount() != 0 {
		t.Fatal("disabled printer must not enqueue test jobs")
	}

	// با تعریف اولین چاپگر (بدون انتخاب صریح) چاپ فعال می‌شود.
	printer := startFakePrinter(t)
	printerID, err := a.store.SaveLocalRecord(ctx, "printer", "", map[string]any{
		"title": "چاپگر صندوق تست", "connection": printer.addr(), "station": "صندوق فروش", "paper_width": "80mm", "is_active": "true",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.SaveLocalRecord(ctx, "product", "", map[string]any{"title": "اسپرسو تست", "price": "950000"}); err != nil {
		t.Fatal(err)
	}
	pos = ordersGet(t, a, "/?page=pos")
	if !strings.Contains(pos, "pos-print-row") || !strings.Contains(pos, "pos-printers-menu") {
		t.Fatal("POS must show print actions once a printer exists")
	}
	settings = ordersGet(t, a, "/?page=settings")
	if !strings.Contains(settings, "مرکز چاپگرها") || strings.Contains(settings, "افزودن فیش‌پرینتر جدید") {
		t.Fatal("settings must link to the printer center, not host a manual printer form")
	}

	// خاموش‌کردن صریح: دکمه‌ها پنهان، چاپ دستی و چاپ خودکار هر دو متوقف.
	ordersPost(t, a, "/local/printer-toggle", url.Values{})
	if pos = ordersGet(t, a, "/?page=pos"); strings.Contains(pos, "pos-print-row") {
		t.Fatal("POS must hide print actions after printer toggle off")
	}
	prods, _ := a.store.Entities(ctx, "product")
	orderID, err := a.store.CreatePOSOrder(ctx, store.POSOrderInput{
		ClientToken: "gate-off-1",
		Items:       []store.POSOrderLineInput{{ProductID: prods[0].ID, Quantity: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	before := jobCount()
	body := ordersPost(t, a, "/local/print-order", url.Values{"order_id": {orderID}})
	if !strings.Contains(body, "چاپگر حرارتی غیرفعال است") {
		t.Fatal("print-order must be refused while printer disabled")
	}
	ordersPost(t, a, "/local/order-status", url.Values{"order_id": {orderID}, "status": {"completed"}})
	ordersPost(t, a, "/local/print-test", url.Values{"printer_id": {printerID}})
	if jobCount() != before {
		t.Fatalf("no print jobs may be created while disabled: before=%d after=%d", before, jobCount())
	}
	if len(printer.received()) != 0 {
		t.Fatal("physical printer must receive nothing while disabled")
	}

	// روشن‌کردن دوباره: چاپ آزمایشی واقعاً به چاپگر می‌رسد.
	ordersPost(t, a, "/local/printer-toggle", url.Values{"enabled": {"on"}})
	ordersPost(t, a, "/local/print-test", url.Values{"printer_id": {printerID}})
	deadline := time.Now().Add(3 * time.Second)
	for len(printer.received()) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if len(printer.received()) == 0 {
		t.Fatal("test print must reach printer after enabling")
	}
}
