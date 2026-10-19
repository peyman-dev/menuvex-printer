package desktop

import (
	"bytes"
	"context"
	"net"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/sinaqaffari/coffish/desktop/internal/store"
	"time"
)

// fakeThermalPrinter یک چاپگر شبکه واقعی (TCP) برای تست چاپ مستقیم است.
type fakeThermalPrinter struct {
	mu   sync.Mutex
	data [][]byte
	ln   net.Listener
}

func startFakePrinter(t *testing.T) *fakeThermalPrinter {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeThermalPrinter{ln: ln}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := new(bytes.Buffer)
				tmp := make([]byte, 4096)
				_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
				for {
					n, rErr := c.Read(tmp)
					if n > 0 {
						buf.Write(tmp[:n])
					}
					if rErr != nil {
						break
					}
				}
				f.mu.Lock()
				f.data = append(f.data, buf.Bytes())
				f.mu.Unlock()
			}(conn)
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return f
}

func (f *fakeThermalPrinter) addr() string { return f.ln.Addr().String() }

func (f *fakeThermalPrinter) received() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]byte, len(f.data))
	copy(out, f.data)
	return out
}

func TestDirectPrintToNetworkPrinterWithoutBrowserPrint(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	printer := startFakePrinter(t)

	if _, err := a.store.SaveLocalRecord(ctx, "printer", "", map[string]any{
		"title": "چاپگر صندوق تست", "connection": printer.addr(), "station": "صندوق فروش", "paper_width": "80mm", "is_active": "true",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.SaveLocalRecord(ctx, "product", "", map[string]any{"title": "اسپرسو تست", "price": "950000"}); err != nil {
		t.Fatal(err)
	}
	prods, _ := a.store.Entities(ctx, "product")
	orderID, err := a.store.CreatePOSOrder(ctx, store.POSOrderInput{Items: []store.POSOrderLineInput{{ProductID: prods[0].ID, Quantity: 1}}})
	if err != nil {
		t.Fatal(err)
	}

	form := url.Values{"csrf": {a.csrf}, "order_id": {orderID}}
	r := httptest.NewRequest("POST", "http://localhost/local/print-order", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("print-order status %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "چاپ شد") {
		t.Fatalf("expected printed status in UI, got: %.400s", w.Body.String())
	}

	deadline := time.Now().Add(3 * time.Second)
	for len(printer.received()) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	got := printer.received()
	if len(got) != 1 {
		t.Fatalf("printer must receive exactly 1 job, got %d", len(got))
	}
	payload := got[0]
	if payload[0] != 0x1B || payload[1] != 0x40 {
		t.Fatal("payload must start with ESC @ init")
	}
	if !bytes.Contains(payload, []byte{0x1D, 0x56, 0x42, 0x00}) {
		t.Fatal("payload must end with GS V partial cut")
	}
	// شماره سفارش روی فیش فقط عدد است (از ۱۰۰ به‌ترتیب روزانه، بدون پیشوند F-).
	ordersNow, _ := a.store.Entities(ctx, "order")
	orderNumber := ""
	for _, oe := range ordersNow {
		if oe.ID == orderID {
			orderNumber = oe.Field("order_number")
		}
	}
	if orderNumber == "" {
		t.Fatal("order number missing on created order")
	}
	if !bytes.Contains(payload, []byte(store.ToPersianDigits(orderNumber))) {
		t.Fatalf("payload must contain order number %q", orderNumber)
	}
	if bytes.Contains(payload, []byte("F-")) {
		t.Fatal("payload must not contain F- prefix")
	}

	jobs, _ := a.store.PrintJobs(ctx, orderID)
	if len(jobs) != 1 || jobs[0].Status != "printed" {
		t.Fatalf("expected one printed job, got %+v", jobs)
	}

	// چاپ دوباره همان سفارش: Job موفق ساخته/ارسال مجدد نمی‌شود
	form2 := url.Values{"csrf": {a.csrf}, "order_id": {orderID}}
	r2 := httptest.NewRequest("POST", "http://localhost/local/print-order", strings.NewReader(form2.Encode()))
	r2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r2.Header.Set("HX-Request", "true")
	w2 := httptest.NewRecorder()
	a.Handler().ServeHTTP(w2, r2)
	time.Sleep(150 * time.Millisecond)
	if len(printer.received()) != 1 {
		t.Fatalf("duplicate print detected: printer received %d jobs", len(printer.received()))
	}
	jobs, _ = a.store.PrintJobs(ctx, orderID)
	if len(jobs) != 1 {
		t.Fatalf("duplicate job persisted: %+v", jobs)
	}
}

func TestFailedPrintKeepsOrderAndAllowsRetryState(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	if _, err := a.store.SaveLocalRecord(ctx, "printer", "", map[string]any{
		"title": "چاپگر خاموش", "connection": "127.0.0.1:1", "station": "صندوق فروش", "paper_width": "58mm", "is_active": "true",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.SaveLocalRecord(ctx, "product", "", map[string]any{"title": "چای تست", "price": "500000"}); err != nil {
		t.Fatal(err)
	}
	prods, _ := a.store.Entities(ctx, "product")
	orderID, err := a.store.CreatePOSOrder(ctx, store.POSOrderInput{Items: []store.POSOrderLineInput{{ProductID: prods[0].ID, Quantity: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	jobs, _ := a.enqueueAndDispatchPrintJobs(ctx, orderID)
	if len(jobs) != 1 || jobs[0].Status != "failed" || jobs[0].LastError == "" {
		t.Fatalf("expected one failed job with error, got %+v", jobs)
	}
	if _, err := a.store.Entity(ctx, "order", orderID); err != nil {
		t.Fatal("failed print must not remove the order:", err)
	}
	// وضعیت Job برای Retry پایدار مانده است
	persisted, _ := a.store.PrintJobs(ctx, orderID)
	if len(persisted) != 1 || persisted[0].Attempts < 1 {
		t.Fatalf("failed job must persist with attempts, got %+v", persisted)
	}
}

func TestNewParityPagesAndFontsAreServed(t *testing.T) {
	a := testApp(t)
	for _, path := range []string{
		"/?page=receipts", "/?page=pricing", "/?page=analytics", "/?page=waste",
		"/static/fonts/Peyda-Regular.woff", "/static/fonts/YekanBakh-Bold.woff", "/static/logo.webp",
	} {
		r := httptest.NewRequest("GET", "http://localhost"+path, nil)
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("%s -> %d", path, w.Code)
		}
	}
	r := httptest.NewRequest("GET", "http://localhost/", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	body := w.Body.String()
	if !strings.Contains(body, "منووکس") || strings.Contains(body, "Vazirmatn") {
		t.Fatal("layout must use MenuVex identity and web fonts")
	}
	cssR := httptest.NewRequest("GET", "http://localhost/static/app.css", nil)
	cssW := httptest.NewRecorder()
	a.Handler().ServeHTTP(cssW, cssR)
	if !strings.Contains(cssW.Body.String(), "Peyda") || !strings.Contains(cssW.Body.String(), "#1D4F8C") {
		t.Fatal("app.css must carry web font + brand tokens")
	}
}
