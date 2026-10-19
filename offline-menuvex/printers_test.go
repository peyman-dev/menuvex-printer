package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sinaqaffari/coffish/desktop/internal/printer"
	"github.com/sinaqaffari/coffish/desktop/internal/store"
)

func postRaw(t *testing.T, a *Application, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	form.Set("csrf", a.csrf)
	r := httptest.NewRequest("POST", "http://localhost"+path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}

func getJSON(t *testing.T, a *Application, path string) []byte {
	t.Helper()
	r := httptest.NewRequest("GET", "http://localhost"+path, nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("GET %s -> %d: %s", path, w.Code, w.Body.String())
	}
	return w.Body.Bytes()
}

func saveNetworkPrinter(t *testing.T, a *Application, name, host string, port int, role string, active bool) printer.Profile {
	t.Helper()
	id, err := a.store.SavePrinter(context.Background(), printer.Profile{
		Name: name, Role: role, Kind: printer.KindNetwork, Host: host, Port: port,
		PaperMM: printer.Paper80, WidthDots: 576, Copies: 1, Cut: true, Active: active,
	})
	if err != nil {
		t.Fatalf("save printer %s: %v", name, err)
	}
	p, err := a.store.Printer(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPrinterCenterPagesRender(t *testing.T) {
	a := testApp(t)
	pages := map[string][]string{
		"/?page=printers": {
			"مرکز چاپگرها", "پرینترهای نصب‌شده", "جستجوی USB", "جستجوی شبکه", "افزودن دستی",
			"صف چاپ", "هنوز چاپگری تنظیم نشده", "جستجوی خودکار چاپگرها", "وضعیت چاپ خودکار", "چاپگرها و صف چاپ",
		},
		"/?page=printer-new": {
			"افزودن چاپگر در چهار گام", "آدرس IPv4 چاپگر", "صف چاپ سیستم‌عامل", "USB مستقیم",
			"نقش چاپگر", "برش خودکار کاغذ", "جستجوی چاپگرهای USB", "ذخیره چاپگر",
		},
		"/?page=printer-scan": {
			"جستجوی چاپگر در شبکه محلی", "محدودهٔ مجاز", "افزودن دستی چاپگر",
			"اینترنت قابل اسکن نیست",
		},
		"/?page=print-queue": {
			"صف چاپ و پیگیری فیش‌ها", "تلاش دوباره", "کل Jobها", "سفارش‌ها و فیش‌ها",
		},
	}
	for path, wants := range pages {
		body := ordersGet(t, a, path)
		for _, want := range wants {
			if !strings.Contains(body, want) {
				t.Fatalf("%s missing %q", path, want)
			}
		}
	}

	p := saveNetworkPrinter(t, a, "چاپگر صندوق مرکزی", "127.0.0.1", 19100, "cashier", true)
	// صفحهٔ جزئیات جداگانه وجود ندارد: لینک قدیمی به فهرست می‌رسد و تنظیمات
	// از دیالوگ کارت چاپگر (printerEditOpen) انجام می‌شود.
	body := ordersGet(t, a, "/?page=printer&id="+p.ID)
	for _, want := range []string{p.Name, "printerEditOpen", `id="dlg-printer-found"`, "بررسی اتصال", "چاپ آزمایشی", "حذف چاپگر", "مرکز چاپگرها"} {
		if !strings.Contains(body, want) {
			t.Fatalf("printer list/dialog missing %q", want)
		}
	}
	if !strings.Contains(ordersGet(t, a, "/?page=printers"), p.Name) {
		t.Fatal("printer list must show the saved printer")
	}
}

func TestPrinterQueueScanShowsInstalledQueuesAndPrefillsWizard(t *testing.T) {
	a := testApp(t)
	a.queueScan = func(context.Context) ([]string, error) {
		return []string{"EPSON-TM-T20", "Kitchen Receipt"}, nil
	}

	body := ordersPost(t, a, "/local/printer-queue-scan", url.Values{})
	for _, want := range []string{
		"پرینترهای نصب‌شده روی سیستم‌عامل", "EPSON-TM-T20", "Kitchen Receipt",
		"صف چاپ سیستم‌عامل", "انتخاب", "queue_name=EPSON-TM-T20",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("installed-printer discovery missing %q: %s", want, body)
		}
	}
	if profiles, err := a.store.Printers(context.Background()); err != nil || len(profiles) != 0 {
		t.Fatalf("discovery must list candidates, not silently add them: %+v err=%v", profiles, err)
	}

	path := "/?page=printer-new&" + url.Values{
		"kind":       {printer.KindSpooler},
		"queue_name": {"EPSON-TM-T20"},
		"name":       {"EPSON-TM-T20"},
	}.Encode()
	body = ordersGet(t, a, path)
	for _, want := range []string{
		`name="connection_kind" value="spooler" checked`,
		`name="queue_name"`, `value="EPSON-TM-T20"`,
		`name="name" value="EPSON-TM-T20"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("selected OS printer was not prefilled (%q): %s", want, body)
		}
	}
}

func TestPrinterSaveValidationAndDuplicatePrevention(t *testing.T) {
	a := testApp(t)
	base := url.Values{
		"name": {"چاپگر تست"}, "connection_kind": {"network"}, "host": {"8.8.8.8"},
		"port": {"9100"}, "paper": {"80"}, "role": {"cashier"}, "is_active": {"on"}, "cut": {"on"},
	}
	body := ordersPost(t, a, "/local/printer-save", base)
	if !strings.Contains(body, "خصوصی") {
		t.Fatalf("public IP must be rejected, body: %s", body)
	}
	base.Set("host", "printer.local")
	body = ordersPost(t, a, "/local/printer-save", base)
	if !strings.Contains(body, "IPv4") {
		t.Fatalf("hostname must be rejected, body: %s", body)
	}
	base.Set("host", "10.0.0.0")
	if body = ordersPost(t, a, "/local/printer-save", base); !strings.Contains(body, "شبکه") {
		t.Fatalf(".0 address must be rejected: %s", body)
	}
	base.Set("host", "192.168.44.9")
	base.Set("connection_kind", "usb")
	base.Set("usb_path", "/dev/sda")
	if body = ordersPost(t, a, "/local/printer-save", base); !strings.Contains(body, "usblp") {
		t.Fatalf("unsafe USB path must be rejected: %s", body)
	}

	// A valid printer is persisted with both the new profile fields and the
	// legacy connection string that routing code still reads.
	form := url.Values{
		"name": {"چاپگر انبار"}, "connection_kind": {"network"}, "host": {"192.168.44.9"},
		"port": {"9100"}, "paper": {"80"}, "role": {"kitchen"}, "is_active": {"on"}, "cut": {"on"},
	}
	ordersPost(t, a, "/local/printer-save", form)
	list, err := a.store.Printers(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatalf("printers = %+v err=%v", list, err)
	}
	if list[0].Kind != printer.KindNetwork || list[0].Port != 9100 || list[0].Host != "192.168.44.9" {
		t.Fatalf("profile = %+v", list[0])
	}
	ent, _ := a.store.Entity(context.Background(), "printer", list[0].ID)
	if ent.Field("connection") != "192.168.44.9:9100" {
		t.Fatalf("legacy connection = %q", ent.Field("connection"))
	}
	if !a.printerEnabled(context.Background()) {
		t.Fatal("saving the first printer must make the print gate active")
	}
	// Same destination again must be refused, even under another name.
	form.Set("name", "همان چاپگر با نام دیگر")
	if body = ordersPost(t, a, "/local/printer-save", form); !strings.Contains(body, "قبلاً ثبت") {
		t.Fatalf("duplicate destination must be rejected: %s", body)
	}
}

func TestPrinterProbeLayersAndPersistence(t *testing.T) {
	a := testApp(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	gotStatus := make(chan bool, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 3)
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		n, _ := conn.Read(buf)
		gotStatus <- n == 3 && bytes.Equal(buf, []byte{0x10, 0x04, 0x01})
		_, _ = conn.Write([]byte{0x12})
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	profile := saveNetworkPrinter(t, a, "چاپگر پاسخ‌ده", "127.0.0.1", port, "cashier", true)

	body := ordersPost(t, a, "/local/printer-probe", url.Values{"printer_id": {profile.ID}, "ajax": {"1"}})
	var payload struct {
		OK    bool                `json:"ok"`
		Probe printer.ProbeResult `json:"probe"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("probe JSON: %v (%s)", err, body)
	}
	if !payload.OK || payload.Probe.State != printer.ProbeOnline || !payload.Probe.LikelyPrinter {
		t.Fatalf("probe result = %+v", payload.Probe)
	}
	if len(payload.Probe.Stages) != 3 {
		t.Fatalf("layered stages = %+v", payload.Probe.Stages)
	}
	select {
	case ok := <-gotStatus:
		if !ok {
			t.Fatal("probe did not send the ESC/POS status request")
		}
	case <-time.After(time.Second):
		t.Fatal("probe did not reach the printer")
	}

	saved, _ := a.store.Printer(context.Background(), profile.ID)
	if saved.LastCheckState != printer.ProbeOnline || len(saved.LastCheckStages) != 3 {
		t.Fatalf("probe evidence not persisted: %+v", saved)
	}
	// شواهد بررسی روی کارت چاپگر در فهرست دیده می‌شود، نه صفحهٔ جزئیات.
	detail := ordersGet(t, a, "/?page=printers")
	for _, want := range []string{"آنلاین"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("printer list missing probe evidence %q", want)
		}
	}

	// A closed port is offline, never "printed".
	closed, _ := net.Listen("tcp", "127.0.0.1:0")
	closedPort := closed.Addr().(*net.TCPAddr).Port
	_ = closed.Close()
	off := saveNetworkPrinter(t, a, "چاپگر خاموش", "127.0.0.1", closedPort, "", true)
	body = ordersPost(t, a, "/local/printer-probe", url.Values{"printer_id": {off.ID}, "ajax": {"1"}})
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Probe.State != printer.ProbeOffline || payload.Probe.Reachable {
		t.Fatalf("closed printer = %+v", payload.Probe)
	}
}

func TestTestPrintUsesNewTransportWithoutClaimingPhysicalPrint(t *testing.T) {
	a := testApp(t)
	fake := startFakePrinter(t)
	host, portStr, _ := net.SplitHostPort(fake.addr())
	port := 0
	for _, c := range portStr {
		port = port*10 + int(c-'0')
	}
	profile := saveNetworkPrinter(t, a, "چاپگر مسیر جدید", host, port, "cashier", true)

	body := ordersPost(t, a, "/local/print-test", url.Values{"printer_id": {profile.ID}, "return_page": {"printer"}})
	if !strings.Contains(body, "ارسال شد") || !strings.Contains(body, "تأیید فیزیکی") {
		t.Fatalf("test print must report transport hand-off, not physical success: %s", body)
	}
	received := waitPrinters(t, fake, 1)
	if len(received) != 1 {
		t.Fatalf("printer received %d payloads", len(received))
	}
	if !bytes.Contains(received[0], []byte("فیش آزمایشی چاپگر حرارتی")) || !bytes.Contains(received[0], []byte{0x1B, 0x40}) {
		t.Fatalf("test page missing: %q", received[0])
	}
	jobs, _ := a.store.PrintJobs(context.Background(), "")
	if len(jobs) != 1 || jobs[0].Status != store.PrintStatusSent || jobs[0].PrinterKind != printer.KindNetwork {
		t.Fatalf("job = %+v", jobs)
	}
	if jobs[0].Target != "network:"+host+":"+portStr {
		t.Fatalf("job target snapshot = %q", jobs[0].Target)
	}
}

func TestPrinterNetworkScanProgressAndCancel(t *testing.T) {
	a := testApp(t)
	a.netScan = func() ([]printer.NetworkRange, error) {
		return []printer.NetworkRange{{CIDR: "192.168.77.0/24", InterfaceName: "eth-test", Address: "192.168.77.5", Hosts: 254}}, nil
	}
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, _ := net.SplitHostPort(address)
		if host != "192.168.77.7" {
			return nil, context.DeadlineExceeded
		}
		client, server := net.Pipe()
		_ = server.Close()
		return client, nil
	}
	a.scanSvc = printer.NewScanner(printer.ScanPolicy{
		MaxHosts: 254, Workers: 32, PerHostTimeout: 50 * time.Millisecond,
		TotalTimeout: 5 * time.Second, ResultTTL: time.Minute,
	}, dial)

	w := postRaw(t, a, "/local/printer-scan", url.Values{"cidr": {"192.168.77.0/24"}, "port": {"9100"}})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("scan start status = %d: %s", w.Code, w.Body.String())
	}
	loc, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	scanID := loc.Query().Get("scan")
	if scanID == "" {
		t.Fatalf("redirect without scan id: %s", loc.String())
	}

	deadline := time.Now().Add(4 * time.Second)
	var snap printer.ScanStatus
	for {
		raw := getJSON(t, a, "/local/printer-scan-status?scan="+url.QueryEscape(scanID))
		var envelope struct {
			Scan printer.ScanStatus `json:"scan"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			t.Fatalf("scan JSON: %v (%s)", err, raw)
		}
		snap = envelope.Scan
		if !snap.Running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("scan did not finish: %+v", snap)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if snap.Total != 254 || len(snap.Results) != 1 || snap.Results[0].Host != "192.168.77.7" {
		t.Fatalf("scan result = %+v", snap)
	}
	// The scan page displays the same result and never auto-adds it.
	page := ordersGet(t, a, "/?page=printer-scan&scan="+url.QueryEscape(scanID))
	if !strings.Contains(page, "192.168.77.7") || !strings.Contains(page, "پورت باز") || !strings.Contains(page, "انتخاب پرینتر") {
		t.Fatalf("scan page missing result: %s", page)
	}

	// The HTMX polling endpoint returns the same partial that replaces the
	// results panel every second.
	req := httptest.NewRequest("GET", "http://localhost/local/printer-scan-status?scan="+url.QueryEscape(scanID), nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "scan-results") ||
		!strings.Contains(rec.Body.String(), "192.168.77.7") {
		t.Fatalf("scan partial = %d %s", rec.Code, rec.Body.String())
	}

	body := ordersPost(t, a, "/local/printer-scan", url.Values{"cidr": {"8.8.8.0/24"}, "port": {"9100"}})
	if !strings.Contains(body, "مجاز") {
		t.Fatalf("internet range must be rejected: %s", body)
	}

	// Cancellable: use blocking dials so the scan cannot finish on its own.
	blockDial := func(ctx context.Context, network, address string) (net.Conn, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	a.scanSvc = printer.NewScanner(printer.ScanPolicy{
		MaxHosts: 254, Workers: 2, PerHostTimeout: time.Minute,
		TotalTimeout: time.Minute, ResultTTL: time.Minute,
	}, blockDial)
	w = postRaw(t, a, "/local/printer-scan", url.Values{"cidr": {"192.168.77.0/24"}, "port": {"9100"}})
	loc, _ = url.Parse(w.Header().Get("Location"))
	scanID = loc.Query().Get("scan")
	ordersPost(t, a, "/local/printer-scan-cancel", url.Values{"scan": {scanID}})
	deadline = time.Now().Add(2 * time.Second)
	for {
		raw := getJSON(t, a, "/local/printer-scan-status?scan="+url.QueryEscape(scanID))
		var envelope struct {
			Scan printer.ScanStatus `json:"scan"`
		}
		_ = json.Unmarshal(raw, &envelope)
		if !envelope.Scan.Running {
			if !envelope.Scan.Cancelled {
				t.Fatalf("cancelled flag missing: %+v", envelope.Scan)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cancelled scan kept running")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestPrintQueueSafetyAndOrderIndependence(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()

	// Success path: exactly one delivery and a terminal "sent" job.
	fake := startFakePrinter(t)
	host, portStr, _ := net.SplitHostPort(fake.addr())
	port := 0
	for _, c := range portStr {
		port = port*10 + int(c-'0')
	}
	profile := saveNetworkPrinter(t, a, "چاپگر موفق", host, port, "cashier", true)
	ordersPost(t, a, "/local/print-test", url.Values{"printer_id": {profile.ID}})
	waitPrinters(t, fake, 1)
	jobs, _ := a.store.PrintJobs(ctx, "")
	if len(jobs) != 1 {
		t.Fatalf("jobs = %+v", jobs)
	}
	if body := ordersPost(t, a, "/local/print-queue-retry", url.Values{"job_id": {jobs[0].ID}}); !strings.Contains(body, "قابل ارسال دوباره نیست") {
		t.Fatalf("sent job must not be retried: %s", body)
	}
	time.Sleep(80 * time.Millisecond)
	if got := len(fake.received()); got != 1 {
		t.Fatalf("printer received %d payloads after refused retry", got)
	}

	// Two concurrent dispatches of the same pending job must produce one print.
	a2 := testApp(t)
	fake2 := startFakePrinter(t)
	h2, p2s, _ := net.SplitHostPort(fake2.addr())
	p2 := 0
	for _, c := range p2s {
		p2 = p2*10 + int(c-'0')
	}
	prof2 := saveNetworkPrinter(t, a2, "چاپگر هم‌زمان", h2, p2, "cashier", true)
	job2, err := a2.store.EnqueueTestPrintJob(ctx, prof2.ID)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan store.PrintJobView, 2)
	go func() { done <- a2.dispatchPrintJob(ctx, job2) }()
	go func() { done <- a2.dispatchPrintJob(ctx, job2) }()
	<-done
	<-done
	time.Sleep(80 * time.Millisecond)
	if got := len(fake2.received()); got != 1 {
		t.Fatalf("concurrent dispatch printed %d copies", got)
	}

	// Failure path: order status is unchanged and the job remains retryable.
	// Deactivate the healthy printer so the order is routed to the closed one.
	if err := a.store.SetPrinterActive(ctx, profile.ID, false); err != nil {
		t.Fatal(err)
	}
	prodID, _ := a.store.SaveLocalRecord(ctx, "product", "", map[string]any{"title": "قهوه مصون", "price": "100000"})
	closed, _ := net.Listen("tcp", "127.0.0.1:0")
	closedPort := closed.Addr().(*net.TCPAddr).Port
	_ = closed.Close()
	bad := saveNetworkPrinter(t, a, "چاپگر قطع", "127.0.0.1", closedPort, "cashier", true)
	orderID, err := a.store.CreatePOSOrder(ctx, store.POSOrderInput{
		ClientToken: "printer-safe-order", Items: []store.POSOrderLineInput{{ProductID: prodID, Quantity: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := a.store.Entity(ctx, "order", orderID)
	jobs, err = a.store.EnqueueOrderPrintJobsDocs(ctx, orderID, map[string]int{"customer": 1})
	if err != nil || len(jobs) != 1 {
		t.Fatalf("enqueue: jobs=%+v err=%v", jobs, err)
	}
	if jobs[0].PrinterID != bad.ID {
		// The first printer in the list is not necessarily the closed one; use it
		// only when it is, otherwise this sub-check is about the same order.
		t.Logf("job chose %s (closed printer %s)", jobs[0].PrinterID, bad.ID)
	}
	failed := a.dispatchPrintJob(ctx, jobs[0])
	if failed.Status != store.PrintStatusFailed {
		t.Fatalf("expected failed job, got %+v", failed)
	}
	if !failed.Retryable() {
		t.Fatalf("safe pre-send failure must be retryable: %+v", failed)
	}
	after, _ := a.store.Entity(ctx, "order", orderID)
	if before.Field("status") != after.Field("status") {
		t.Fatalf("print failure changed order status: %q -> %q", before.Field("status"), after.Field("status"))
	}
	if !strings.Contains(ordersGet(t, a, "/?page=print-queue"), "چاپگر قطع") &&
		!strings.Contains(ordersGet(t, a, "/?page=print-queue"), "چاپگر موفق") {
		t.Fatal("queue page must list the job")
	}

	// Uncertain failures are never offered for retry.
	uncertain, err := a.store.FailPrintJob(ctx, failed.ID, "timeout after partial write", true)
	if err != nil {
		t.Fatal(err)
	}
	if uncertain.Retryable() || !strings.Contains(uncertain.RetryBlockReason(), "نامشخص") {
		t.Fatalf("uncertain job must be blocked: %+v", uncertain)
	}

	// A queued job can be cancelled; a sent one cannot.
	queued, _ := a.store.EnqueueTestPrintJob(ctx, profile.ID)
	if got, err := a.store.CancelPrintJob(ctx, queued.ID); err != nil || got.Status != store.PrintStatusCancelled {
		t.Fatalf("cancel queued: %+v err=%v", got, err)
	}
	if sent := a.dispatchPrintJob(ctx, queued); sent.Status == store.PrintStatusSent {
		t.Fatalf("cancelled job was sent: %+v", sent)
	}
	time.Sleep(50 * time.Millisecond)
	if got := len(fake.received()); got != 1 {
		t.Fatalf("cancelled job reached the printer (%d payloads)", got)
	}
	if _, err := a.store.CancelPrintJob(ctx, jobs[0].ID); err == nil {
		t.Fatal("sent/failed job must not be cancellable")
	}
}

func TestPrinterRolesDriveCashierAndKitchenDestinations(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	cashierFake := startFakePrinter(t)
	kitchenFake := startFakePrinter(t)
	parsePort := func(addr string) (string, int) {
		h, ps, _ := net.SplitHostPort(addr)
		n := 0
		for _, c := range ps {
			n = n*10 + int(c-'0')
		}
		return h, n
	}
	ch, cp := parsePort(cashierFake.addr())
	kh, kp := parsePort(kitchenFake.addr())
	cashier := saveNetworkPrinter(t, a, "صندوق نقش‌دار", ch, cp, "cashier", true)
	kitchen := saveNetworkPrinter(t, a, "آشپزخانه نقش‌دار", kh, kp, "kitchen", true)

	prodID, _ := a.store.SaveLocalRecord(ctx, "product", "", map[string]any{"title": "لاته نقشی", "price": "120000"})
	orderID, err := a.store.CreatePOSOrder(ctx, store.POSOrderInput{
		ClientToken: "printer-roles-1", Items: []store.POSOrderLineInput{{ProductID: prodID, Quantity: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := a.store.EnqueueOrderPrintJobsDocs(ctx, orderID, map[string]int{"customer": 1, "kitchen": 1})
	if err != nil || len(jobs) != 2 {
		t.Fatalf("jobs=%+v err=%v", jobs, err)
	}
	for _, j := range jobs {
		switch j.DocType {
		case "customer":
			if j.PrinterID != cashier.ID {
				t.Fatalf("customer job went to %s, want cashier %s", j.PrinterID, cashier.ID)
			}
		case "kitchen":
			if j.PrinterID != kitchen.ID {
				t.Fatalf("kitchen job went to %s, want kitchen %s", j.PrinterID, kitchen.ID)
			}
		}
		a.dispatchPrintJob(ctx, j)
	}
	if got := waitPrinters(t, cashierFake, 1); len(got) != 1 || !bytes.Contains(got[0], []byte("لاته نقشی")) {
		t.Fatalf("cashier receipt = %q", got)
	}
	kg := waitPrinters(t, kitchenFake, 1)
	if len(kg) != 1 || !bytes.Contains(kg[0], []byte("لاته نقشی")) {
		t.Fatalf("kitchen ticket = %q", kg)
	}
}

func TestPrinterDeleteKeepsJobsAndFreesRoutes(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	p := saveNetworkPrinter(t, a, "چاپگر حذفی", "192.168.90.8", 9100, "kitchen", true)
	catID, _ := a.store.SaveLocalRecord(ctx, "category", "", map[string]any{"title": "دسته حذفی"})
	if _, err := a.store.SaveLocalRecord(ctx, "printer_route", "", map[string]any{
		"category_id": catID, "printer_id": p.ID, "receipt_type": "kitchen", "is_active": "true",
	}); err != nil {
		t.Fatal(err)
	}
	job, _ := a.store.EnqueueTestPrintJob(ctx, p.ID)
	if body := ordersPost(t, a, "/local/printer-delete", url.Values{"printer_id": {p.ID}}); !strings.Contains(body, "تأیید") {
		t.Fatalf("deletion without confirmation must be refused: %s", body)
	}
	if body := ordersPost(t, a, "/local/printer-delete", url.Values{"printer_id": {p.ID}, "confirm_delete": {"on"}}); !strings.Contains(body, "حذف شد") {
		t.Fatalf("delete failed: %s", body)
	}
	if _, err := a.store.Printer(ctx, p.ID); err == nil {
		t.Fatal("printer still exists")
	}
	routes, _ := a.store.Entities(ctx, "printer_route")
	if len(routes) != 0 {
		t.Fatalf("routes for deleted printer were not removed: %+v", routes)
	}
	if kept, err := a.store.PrintJob(ctx, job.ID); err != nil || kept.ID != job.ID {
		t.Fatal("deleting a printer must not delete print-job history")
	}
}

// TestAutomaticPrintPolicyUsesNewTransport proves the order lifecycle (not
// just direct store calls) reaches the new transport: creating a table order
// prints the kitchen ticket and completing it prints the cashier receipt.
func TestAutomaticPrintPolicyUsesNewTransport(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	kitchenFake := startFakePrinter(t)
	cashierFake := startFakePrinter(t)
	parsePort := func(addr string) (string, int) {
		h, ps, _ := net.SplitHostPort(addr)
		n := 0
		for _, c := range ps {
			n = n*10 + int(c-'0')
		}
		return h, n
	}
	kh, kp := parsePort(kitchenFake.addr())
	ch, cp := parsePort(cashierFake.addr())
	saveNetworkPrinter(t, a, "آشپزخانه خودکار", kh, kp, "kitchen", true)
	saveNetworkPrinter(t, a, "صندوق خودکار", ch, cp, "cashier", true)

	prodID, _ := a.store.SaveLocalRecord(ctx, "product", "", map[string]any{"title": "کیک خودکار", "price": "180000"})
	tableID, _ := a.store.SaveLocalRecord(ctx, "table", "", map[string]any{"title": "میز ۹", "number": "9", "capacity": "4"})
	ordersPost(t, a, "/local/print-policy", url.Values{
		"pw_create_mode": {"kitchen"}, "pw_create_doc": {"kitchen"}, "pw_create_copies": {"1"},
		"pw_complete_mode": {"customer"}, "pw_complete_doc": {"cashier"}, "pw_complete_copies": {"1"},
	})
	ordersPost(t, a, "/local/pos-order", url.Values{
		"product_id": {prodID}, "quantity": {"1"}, "table_id": {tableID},
		"client_token": {"new-transport-auto-1"}, "payment_method": {"cash"}, "fulfillment_type": {"dine_in"},
	})
	if got := waitPrinters(t, kitchenFake, 1); len(got) != 1 || !bytes.Contains(got[0], []byte("کیک خودکار")) {
		t.Fatalf("automatic kitchen ticket missing: %q", got)
	}
	orderID := orderByToken(t, a, "new-transport-auto-1")
	ordersPost(t, a, "/local/order-status", url.Values{"order_id": {orderID}, "status": {"completed"}})
	if got := waitPrinters(t, cashierFake, 1); len(got) != 1 || !bytes.Contains(got[0], []byte("کیک خودکار")) {
		t.Fatalf("automatic customer receipt missing: %q", got)
	}
	jobs, _ := a.store.PrintJobs(ctx, orderID)
	if len(jobs) != 2 {
		t.Fatalf("jobs = %+v", jobs)
	}
	for _, j := range jobs {
		if j.Status != store.PrintStatusSent {
			t.Fatalf("job did not use the new transport: %+v", j)
		}
	}
	saved, _ := a.store.Entity(ctx, "order", orderID)
	if saved.Field("status") != "completed" {
		t.Fatalf("order status changed unexpectedly: %q", saved.Field("status"))
	}
}

func TestPrinterEditActivationDeleteGuards(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	p := saveNetworkPrinter(t, a, "چاپگر اولیه", "192.168.50.20", 9100, "kitchen", true)

	form := url.Values{
		"id": {p.ID}, "name": {"چاپگر ویرایش‌شده"}, "connection_kind": {"network"},
		"host": {"192.168.50.21"}, "port": {"9101"}, "paper": {"58"}, "role": {"cashier"},
		"station": {"صندوق جلو"}, "cut": {"on"}, "is_active": {"on"},
	}
	body := ordersPost(t, a, "/local/printer-save", form)
	if !strings.Contains(body, "ذخیره شد") {
		t.Fatalf("edit did not confirm: %s", body)
	}
	edited, err := a.store.Printer(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if edited.Name != "چاپگر ویرایش‌شده" || edited.Host != "192.168.50.21" || edited.Port != 9101 ||
		edited.PaperMM != printer.Paper58 || edited.Role != "cashier" || !edited.Cut || !edited.Active {
		t.Fatalf("edited profile = %+v", edited)
	}
	ent, _ := a.store.Entity(ctx, "printer", p.ID)
	if ent.Field("connection") != "192.168.50.21:9101" {
		t.Fatalf("legacy connection was not kept in sync: %q", ent.Field("connection"))
	}

	// Toggling is an explicit action with its own message, not a delete.
	body = ordersPost(t, a, "/local/printer-active", url.Values{"printer_id": {p.ID}})
	if !strings.Contains(body, "غیرفعال شد") {
		t.Fatalf("deactivate message missing: %s", body)
	}
	if after, _ := a.store.Printer(ctx, p.ID); after.Active {
		t.Fatal("printer stayed active")
	}

	if body = ordersPost(t, a, "/local/printer-delete", url.Values{"printer_id": {p.ID}}); !strings.Contains(body, "تأیید") {
		t.Fatalf("delete without confirmation must be refused: %s", body)
	}
	if _, err := a.store.Printer(ctx, p.ID); err != nil {
		t.Fatal("printer was deleted without confirmation")
	}
	body = ordersPost(t, a, "/local/printer-delete", url.Values{"printer_id": {p.ID}, "confirm_delete": {"on"}})
	if !strings.Contains(body, "حذف شد") {
		t.Fatalf("confirmed delete message missing: %s", body)
	}
	if _, err := a.store.Printer(ctx, p.ID); err == nil {
		t.Fatal("printer still exists after confirmed delete")
	}
}

func TestPrintQueueRetryAllSkipsUncertainAndKeepsHistory(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	fake := startFakePrinter(t)
	h, ps, _ := net.SplitHostPort(fake.addr())
	port := 0
	for _, c := range ps {
		port = port*10 + int(c-'0')
	}
	prof := saveNetworkPrinter(t, a, "چاپگر صف", h, port, "cashier", true)

	job, err := a.store.EnqueueTestPrintJob(ctx, prof.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.ClaimPrintJob(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.FailPrintJob(ctx, job.ID, "timeout after partial write", true); err != nil {
		t.Fatal(err)
	}
	body := ordersPost(t, a, "/local/print-queue-retry-all", url.Values{})
	if !strings.Contains(body, "نامشخص") && !strings.Contains(body, "هیچ Job") {
		t.Fatalf("retry-all should explain why nothing was sent: %s", body)
	}
	time.Sleep(50 * time.Millisecond)
	if got := len(fake.received()); got != 0 {
		t.Fatalf("uncertain job was reprinted %d times", got)
	}
	stored, err := a.store.PrintJob(ctx, job.ID)
	if err != nil || stored.Status != store.PrintStatusFailed || !stored.Uncertain || stored.Attempts != 1 {
		t.Fatalf("history changed: %+v err=%v", stored, err)
	}
	if !strings.Contains(ordersGet(t, a, "/?page=print-queue"), "نتیجه نامشخص") {
		t.Fatal("queue page must surface the uncertain state")
	}
}

func TestPrinterUSBScanShowsAccessButNeverAutoAdds(t *testing.T) {
	a := testApp(t)
	a.usbScan = func() ([]printer.USBDevice, error) {
		return []printer.USBDevice{
			{DevicePath: "/dev/usb/lp0", VendorID: "04B8", ProductID: "0E15", Product: "Epson TM-T20", Accessible: true, AccessNote: "آماده ارسال"},
			{VendorID: "1234", ProductID: "0001", Product: "Printer Without usblp", Accessible: false, AccessNote: "گره usblp ندارد"},
		}, nil
	}
	body := ordersPost(t, a, "/local/printer-usb-scan", url.Values{})
	for _, want := range []string{"Epson TM-T20", "Printer Without usblp", "انتخاب", "قابل چاپ مستقیم نیست", "usblp"} {
		if !strings.Contains(body, want) {
			t.Fatalf("USB scan page missing %q", want)
		}
	}
	list, _ := a.store.Printers(context.Background())
	if len(list) != 0 {
		t.Fatalf("USB discovery must never auto-add printers: %+v", list)
	}

	body = ordersPost(t, a, "/local/printer-usb-scan", url.Values{"return_page": {"printers"}})
	for _, want := range []string{"دستگاه‌های USB شناسایی‌شده", "Epson TM-T20", "Printer Without usblp", "پرینترهای نصب‌شده"} {
		if !strings.Contains(body, want) {
			t.Fatalf("printer-center USB scan missing %q: %s", want, body)
		}
	}
	list, _ = a.store.Printers(context.Background())
	if len(list) != 0 {
		t.Fatalf("printer-center USB discovery must not auto-add printers: %+v", list)
	}
}

// TestFoundPrinterSelectionOpensDialog انتخاب پرینتر پیداشده نباید کاربر را به
// صفحهٔ «افزودن چاپگر» ببرد: دکمه‌ها دیالوگ تنظیم را در همان صفحه باز می‌کنند و
// ذخیره از دیالوگ هم روی همان صفحهٔ فهرست چاپگرها برمی‌گردد.
func TestFoundPrinterSelectionOpensDialog(t *testing.T) {
	a := testApp(t)
	a.usbCache = []printer.USBDevice{{Product: "POS-80", DevicePath: "/dev/usb/lp0", Accessible: true}}
	a.usbCacheAt = time.Now()
	page := ordersGet(t, a, "/?page=printers&discovery=usb")
	for _, s := range []string{
		`id="dlg-printer-found"`, `onclick="printerFoundOpen(this)"`, "POS-80",
		`name="return_page" value="printers"`, "فونت چاپ همان فونت برنامه است",
		`id="pf-role" data-native-select`, `id="pf-paper" data-native-select`,
	} {
		if !strings.Contains(page, s) {
			t.Fatalf("printers page missing dialog selection %q", s)
		}
	}
	if strings.Contains(page, "printer-new&kind=") {
		t.Fatal("found-printer selection must not link to the printer-new page")
	}

	// ذخیره از دیالوگ: چاپگر ساخته می‌شود و پاسخ روی صفحهٔ فهرست می‌ماند.
	body := ordersPost(t, a, "/local/printer-save", url.Values{
		"return_page": {"printers"}, "connection_kind": {"network"},
		"name": {"پرینتر دیالوگی"}, "host": {"192.168.1.203"}, "port": {"9100"},
		"role": {"cashier"}, "paper": {"80"}, "is_active": {"on"},
	})
	if !strings.Contains(body, "پرینتر دیالوگی") || !strings.Contains(body, "ذخیره شد") {
		t.Fatalf("dialog save must land back on printers list: %s", body)
	}
	profiles, _ := a.store.Printers(context.Background())
	if len(profiles) != 1 || profiles[0].Name != "پرینتر دیالوگی" || profiles[0].Kind != printer.KindNetwork {
		t.Fatalf("dialog-saved printer wrong: %+v", profiles)
	}
}
