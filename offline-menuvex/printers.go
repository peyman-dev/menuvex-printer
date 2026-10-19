package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sinaqaffari/coffish/desktop/internal/printer"
	"github.com/sinaqaffari/coffish/desktop/internal/store"
)

// ─── مرکز چاپگرها (پرترهٔ وب از Printer Agent) ───────────────────────────
// این فایل فقط لایهٔ HTTP است: اعتبارسنجی، کشف دستگاه، اسکن و ارسال در
// بستهٔ internal/printer انجام می‌شود تا قوانین ایمنی یک‌جا و تست‌پذیر بمانند.
// هیچ‌یک از این مسیرها از shell با ورودی کاربر استفاده نمی‌کند و همهٔ
// عملیات شبکه/USB زمان‌بندی‌شده‌اند.

const (
	printerScanDefaultPort = printer.DefaultNetworkPort
	printerScanMaxResults  = 254
)

func (a *Application) prober() *printer.Prober {
	if a.probeSvc == nil {
		return printer.NewProber(nil)
	}
	return a.probeSvc
}

func (a *Application) scanner() *printer.Scanner {
	if a.scanSvc == nil {
		a.scanSvc = printer.NewScanner(printer.DefaultScanPolicy(), nil)
	}
	return a.scanSvc
}

func (a *Application) discoverUSB() ([]printer.USBDevice, error) {
	if a.usbScan != nil {
		return a.usbScan()
	}
	return printer.DiscoverUSB()
}

func (a *Application) listQueues(ctx context.Context) ([]string, error) {
	if a.queueScan != nil {
		return a.queueScan(ctx)
	}
	return printer.ListQueues(ctx)
}

func (a *Application) listNetworks() ([]printer.NetworkRange, error) {
	if a.netScan != nil {
		return a.netScan()
	}
	return printer.LocalNetworks()
}

// navigateQuery renders a page with extra query parameters, preserving the
// HTMX contract used everywhere else (HX-Push-Url + #main-view replacement).
func (a *Application) navigateQuery(w http.ResponseWriter, r *http.Request, page string, extra map[string]string, message string) {
	if r.Header.Get("HX-Request") != "true" {
		q := "?page=" + page
		for k, v := range extra {
			if v != "" {
				q += "&" + k + "=" + urlQueryEscape(v)
			}
		}
		http.Redirect(w, r, "/"+q, http.StatusSeeOther)
		return
	}
	rq := r.URL.Query()
	rq.Set("page", page)
	for k, v := range extra {
		if v == "" {
			rq.Del(k)
			continue
		}
		rq.Set(k, v)
	}
	r.URL.RawQuery = rq.Encode()
	w.Header().Set("HX-Push-Url", "/?"+r.URL.RawQuery)
	a.render(w, r, message)
}

func urlQueryEscape(v string) string { return url.QueryEscape(v) }

func (a *Application) navigatePrinter(w http.ResponseWriter, r *http.Request, id, message string) {
	if strings.TrimSpace(r.FormValue("return_page")) == "printers" {
		a.navigateQuery(w, r, "printers", nil, message)
		return
	}
	a.navigateQuery(w, r, "printer", map[string]string{"id": id}, message)
}

// printerProfileFromForm builds a profile from the dialog/wizard/detail form.
// It is shared by create and edit so both apply the exact same validation rules.
func printerProfileFromForm(r *http.Request) (printer.Profile, error) {
	get := r.FormValue
	name := strings.TrimSpace(get("name"))
	station := strings.TrimSpace(get("station"))
	if name == "" && station != "" {
		name = station
	}
	if station == "" && name != "" {
		station = name
	}
	p := printer.Profile{
		ID:         strings.TrimSpace(get("id")),
		Name:       name,
		Model:      strings.TrimSpace(get("model")),
		Station:    station,
		Role:       strings.ToLower(strings.TrimSpace(get("role"))),
		Kind:       strings.ToLower(strings.TrimSpace(get("connection_kind"))),
		FontFamily: strings.TrimSpace(get("font_family")),
	}
	if p.Kind == "" {
		p.Kind = strings.ToLower(strings.TrimSpace(get("kind")))
	}
	switch p.Role {
	case "", "cashier", "kitchen", "all":
	default:
		return p, errors.New("نقش چاپگر باید صندوق، آشپزخانه یا هر دو باشد")
	}
	paperVal := strings.TrimSpace(get("paper"))
	if paperVal == "" {
		paperVal = strings.TrimSpace(get("paper_mm"))
	}
	paperVal = strings.TrimSuffix(strings.ToLower(paperVal), "mm")
	paperVal = strings.TrimSpace(normalizeDigits(paperVal))
	switch paperVal {
	case "58":
		p.PaperMM = printer.Paper58
	case "80", "":
		p.PaperMM = printer.Paper80
	default:
		return p, errors.New("عرض کاغذ فقط ۵۸ یا ۸۰ میلی‌متر است")
	}
	if wd, err := strconv.Atoi(normalizeDigits(strings.TrimSpace(get("width_dots")))); err == nil && wd > 0 {
		p.WidthDots = wd
	} else {
		p.WidthDots = printer.DefaultWidthDots(p.PaperMM)
	}
	if cp, err := strconv.Atoi(normalizeDigits(strings.TrimSpace(get("copies")))); err == nil && cp >= 1 && cp <= 3 {
		p.Copies = cp
	} else {
		p.Copies = 1
	}
	if fs, err := strconv.Atoi(normalizeDigits(strings.TrimSpace(get("font_size")))); err == nil && fs >= 12 && fs <= 48 {
		p.FontSize = fs
	} else {
		p.FontSize = 24
	}
	if p.FontFamily == "" {
		p.FontFamily = "Noto Sans Arabic"
	}
	p.Cut = get("cut") == "on" || get("cut") == "true" || get("cut") == "1"
	p.Active = get("is_active") == "on" || get("is_active") == "true" || get("is_active") == "1"
	switch p.Kind {
	case printer.KindNetwork:
		host := strings.TrimSpace(get("host"))
		port := 0
		if h, ps, ok := strings.Cut(host, ":"); ok {
			host = strings.TrimSpace(h)
			port, _ = strconv.Atoi(normalizeDigits(strings.TrimSpace(ps)))
		}
		if port == 0 {
			port, _ = strconv.Atoi(normalizeDigits(strings.TrimSpace(get("port"))))
		}
		if port == 0 {
			port = printer.DefaultNetworkPort
		}
		p.Host, p.Port = host, port
	case printer.KindSpooler:
		p.QueueName = strings.TrimSpace(get("queue_name"))
	case printer.KindUSB:
		p.USB = printer.USBDevice{
			DevicePath:   strings.TrimSpace(get("usb_path")),
			VendorID:     strings.TrimSpace(get("usb_vid")),
			ProductID:    strings.TrimSpace(get("usb_pid")),
			Serial:       strings.TrimSpace(get("usb_serial")),
			Manufacturer: strings.TrimSpace(get("usb_manufacturer")),
			Product:      strings.TrimSpace(get("usb_product")),
			Bus:          strings.TrimSpace(get("usb_bus")),
			PortPath:     strings.TrimSpace(get("usb_port")),
		}
	case "":
		return p, errors.New("نوع اتصال چاپگر را انتخاب کنید")
	default:
		return p, errors.New("نوع اتصال چاپگر نامعتبر است")
	}
	return p, nil
}

// ─── Handlers ───────────────────────────────────────────────────────────

func (a *Application) handlePrinterSave(w http.ResponseWriter, r *http.Request) {
	errPage := "printer-new"
	if strings.TrimSpace(r.FormValue("return_page")) == "printers" {
		errPage = "printers"
	}
	profile, err := printerProfileFromForm(r)
	if err != nil {
		a.navigateQuery(w, r, errPage, map[string]string{"kind": profile.Kind}, "خطا در ثبت چاپگر: "+err.Error())
		return
	}
	id, err := a.store.SavePrinter(r.Context(), profile)
	if err != nil {
		a.navigateQuery(w, r, errPage, map[string]string{"kind": profile.Kind}, "خطا در ثبت چاپگر: "+err.Error())
		return
	}
	a.navigatePrinter(w, r, id, "چاپگر «"+profile.Name+"» ذخیره شد. برای اطمینان از مسیر، «بررسی اتصال» و سپس «چاپ آزمایشی» را اجرا کنید.")
}

func (a *Application) handlePrinterActive(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.Form.Get("printer_id"))
	p, err := a.store.Printer(r.Context(), id)
	if err != nil {
		a.navigateQuery(w, r, "printers", nil, err.Error())
		return
	}
	next := !p.Active
	if err := a.store.SetPrinterActive(r.Context(), id, next); err != nil {
		a.navigatePrinter(w, r, id, "خطا در تغییر وضعیت چاپگر: "+err.Error())
		return
	}
	state := "غیرفعال"
	if next {
		state = "فعال"
	}
	a.navigatePrinter(w, r, id, "چاپگر «"+p.Name+"» "+state+" شد؛ سفارش‌های جدید از این تنظیم استفاده می‌کنند.")
}

func (a *Application) handlePrinterDelete(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.Form.Get("printer_id"))
	p, err := a.store.Printer(r.Context(), id)
	if err != nil {
		a.navigateQuery(w, r, "printers", nil, err.Error())
		return
	}
	if r.Form.Get("confirm_delete") != "on" && r.Form.Get("confirm_delete") != "true" {
		a.navigatePrinter(w, r, id, "برای حذف چاپگر، کادر تأیید را علامت بزنید.")
		return
	}
	if err := a.store.DeletePrinter(r.Context(), id); err != nil {
		a.navigatePrinter(w, r, id, "حذف چاپگر انجام نشد: "+err.Error())
		return
	}
	a.navigateQuery(w, r, "printers", nil, "چاپگر «"+p.Name+"» حذف شد و مسیرهای دسته‌بندی آن آزاد شدند. سفارش‌ها و Jobهای قبلی دست‌نخورده مانده‌اند.")
}

// probeProfile runs the layered verification appropriate to the transport and
// returns the result without ever claiming that a physical print happened.
func (a *Application) probeProfile(ctx context.Context, p printer.Profile) printer.ProbeResult {
	if p.Kind == printer.KindNetwork {
		return a.prober().Probe(ctx, p.Host, p.Port)
	}
	res := printer.ProbeResult{
		Host: p.Host, Port: p.Port,
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
		State:     printer.ProbeUnconfirmed,
	}
	add := func(key, label, state, detail string) {
		res.Stages = append(res.Stages, printer.ProbeStage{Key: key, Label: label, State: state, Detail: detail})
	}
	switch p.Kind {
	case printer.KindSpooler:
		queues, err := a.listQueues(ctx)
		if err != nil {
			add("spooler", "صف چاپ سیستم‌عامل", printer.StageFail, err.Error())
			res.State = printer.ProbeOffline
			res.Summary = "خواندن صف‌های چاپ ممکن نشد: " + err.Error()
			return res
		}
		found := false
		for _, q := range queues {
			if q == p.QueueName {
				found = true
				break
			}
		}
		if !found {
			add("spooler", "صف چاپ سیستم‌عامل", printer.StageFail, "صف «"+p.QueueName+"» در سیستم نصب نیست")
			res.State = printer.ProbeOffline
			res.Summary = "صف چاپ «" + p.QueueName + "» پیدا نشد. نام را اصلاح کنید یا صف را در سیستم‌عامل نصب کنید."
			return res
		}
		add("spooler", "صف چاپ سیستم‌عامل", printer.StagePass, "صف نصب است")
		add("physical", "تأیید چاپ فیزیکی", printer.StageSkip, "صف سیستم‌عامل تأیید فیزیکی برنمی‌گرداند")
		res.Summary = "صف چاپ نصب است؛ برای دیدن کاغذ واقعی، «چاپ آزمایشی» را اجرا کنید."
		res.State = printer.ProbeUnconfirmed
		return res
	case printer.KindUSB:
		if err := printer.ValidateUSBPath(p.USB.DevicePath); err != nil {
			add("usb", "مسیر دستگاه USB", printer.StageFail, err.Error())
			res.State = printer.ProbeOffline
			res.Summary = err.Error()
			return res
		}
		info, err := os.Stat(p.USB.DevicePath)
		if err != nil {
			add("usb", "مسیر دستگاه USB", printer.StageFail, "دستگاه در دسترس نیست: "+err.Error())
			res.State = printer.ProbeOffline
			res.Summary = "دستگاه USB در دسترس نیست؛ کابل و اتصال را بررسی کنید."
			return res
		}
		if info.Mode()&os.ModeCharDevice == 0 {
			add("usb", "مسیر دستگاه USB", printer.StageFail, "این مسیر دستگاه کاراکتری نیست")
			res.State = printer.ProbeOffline
			res.Summary = "مسیر ثبت‌شده دستگاه چاپگر نیست."
			return res
		}
		add("usb", "مسیر دستگاه USB", printer.StagePass, "گره دستگاه موجود است")
		add("physical", "تأیید چاپ فیزیکی", printer.StageSkip, "چاپ USB تأیید فیزیکی برنمی‌گرداند")
		res.Summary = "دستگاه USB موجود است؛ برای تأیید کاغذ، «چاپ آزمایشی» را اجرا کنید."
		return res
	default:
		add("legacy", "چاپگر قدیمی", printer.StageWarn, "این رکورد قبل از مرکز چاپگر ساخته شده و نوع اتصالش مشخص نیست؛ آن را ویرایش و دوباره ذخیره کنید")
		res.Summary = "نوع اتصال این رکورد قدیمی مشخص نیست؛ با ویرایش چاپگر، نوع اتصال را انتخاب کنید."
		return res
	}
}

func (a *Application) saveProbeResult(ctx context.Context, p printer.Profile, res printer.ProbeResult) {
	stages, _ := json.Marshal(res.Stages)
	_ = a.store.SavePrinterCheck(ctx, p.ID, res.State, res.Summary, string(stages), res.CheckedAt)
}

func (a *Application) handlePrinterProbe(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.Form.Get("printer_id"))
	p, err := a.store.Printer(r.Context(), id)
	if err != nil {
		a.navigateQuery(w, r, "printers", nil, err.Error())
		return
	}
	res := a.probeProfile(r.Context(), p)
	a.saveProbeResult(r.Context(), p, res)
	if r.Form.Get("ajax") == "1" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": res.State == printer.ProbeOnline, "probe": res})
		return
	}
	a.navigatePrinter(w, r, id, "بررسی اتصال: "+res.Summary)
}

func (a *Application) cacheUSBScan() ([]printer.USBDevice, error) {
	devices, err := a.discoverUSB()
	a.usbCacheMu.Lock()
	a.usbCache = append([]printer.USBDevice(nil), devices...)
	a.usbCacheAt = time.Now()
	a.usbCacheErr = ""
	if err != nil {
		a.usbCacheErr = err.Error()
	}
	a.usbCacheMu.Unlock()
	return devices, err
}

// handlePrinterDiscoverAll runs unified discovery across USB, OS queues, and
// LAN network on all operating systems without requiring manual IP/port entry.
func (a *Application) handlePrinterDiscoverAll(w http.ResponseWriter, r *http.Request) {
	_, _ = a.cacheUSBScan()
	extra := map[string]string{"discovery": "all"}
	if ranges, err := a.listNetworks(); err == nil && len(ranges) > 0 {
		if snap, sErr := a.scanner().Start(ranges[0].CIDR, printerScanDefaultPort, ranges); sErr == nil {
			extra["scan"] = snap.ID
		} else if latest, ok := a.scanner().LatestStatus(); ok {
			extra["scan"] = latest.ID
		}
	}
	a.navigateQuery(w, r, "printers", extra,
		"جستجوی خودکار پرینترهای USB، سیستم‌عامل و شبکه (LAN) انجام شد؛ پرینتر مورد نظر را از کارت‌های پیدا‌شده انتخاب کنید.")
}

func (a *Application) handlePrinterQueueScan(w http.ResponseWriter, r *http.Request) {
	_, _ = a.cacheUSBScan()
	a.navigateQuery(w, r, "printers", map[string]string{"discovery": "queues"},
		"فهرست چاپگرهای نصب‌شده روی سیستم بررسی شد؛ نتیجه همین‌جا نمایش داده می‌شود.")
}

func (a *Application) handlePrinterUSBScan(w http.ResponseWriter, r *http.Request) {
	devices, err := a.cacheUSBScan()
	msg := fmt.Sprintf("%d دستگاه چاپگر USB پیدا شد؛ فقط دستگاه‌هایی که گره usblp دارند قابل افزودن مستقیم هستند.", len(devices))
	if err != nil {
		msg = err.Error()
	}
	page := "printer-new"
	extra := map[string]string{"kind": "usb"}
	if strings.TrimSpace(r.FormValue("return_page")) == "printers" {
		page = "printers"
		extra = map[string]string{"discovery": "usb"}
	}
	a.navigateQuery(w, r, page, extra, msg)
}

func (a *Application) handlePrinterScanStart(w http.ResponseWriter, r *http.Request) {
	targetPage := "printer-scan"
	if strings.TrimSpace(r.FormValue("return_page")) == "printers" {
		targetPage = "printers"
	}
	ranges, err := a.listNetworks()
	if err != nil {
		extra := map[string]string(nil)
		if targetPage == "printers" {
			extra = map[string]string{"discovery": "lan"}
		}
		a.navigateQuery(w, r, targetPage, extra, err.Error())
		return
	}
	cidr := strings.TrimSpace(r.Form.Get("cidr"))
	if cidr == "" && len(ranges) > 0 {
		cidr = ranges[0].CIDR
	}
	port, _ := strconv.Atoi(normalizeDigits(strings.TrimSpace(r.Form.Get("port"))))
	if port == 0 {
		port = printerScanDefaultPort
	}
	snap, err := a.scanner().Start(cidr, port, ranges)
	if err != nil {
		if errors.Is(err, printer.ErrScanBusy) {
			if latest, ok := a.scanner().LatestStatus(); ok {
				extra := map[string]string{"scan": latest.ID}
				if targetPage == "printers" {
					extra["discovery"] = "lan"
				}
				a.navigateQuery(w, r, targetPage, extra, "جستجوی شبکه در حال اجراست.")
				return
			}
		}
		extra := map[string]string(nil)
		if targetPage == "printers" {
			extra = map[string]string{"discovery": "lan"}
		}
		a.navigateQuery(w, r, targetPage, extra, "شروع جستجو انجام نشد: "+err.Error())
		return
	}
	extra := map[string]string{"scan": snap.ID}
	if targetPage == "printers" {
		extra["discovery"] = "lan"
	}
	a.navigateQuery(w, r, targetPage, extra,
		fmt.Sprintf("جستجوی شبکه در %s آغاز شد؛ نتایج به‌صورت زنده به‌روزرسانی می‌شوند.", snap.CIDR))
}

func (a *Application) handlePrinterScanStatus(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("scan"))
	if id == "" {
		id = strings.TrimSpace(r.Form.Get("id"))
	}
	snap, ok := a.scanner().Status(id)
	if !ok {
		http.Error(w, "scan not found", http.StatusNotFound)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		profiles, _ := a.store.Printers(r.Context())
		discovered := printer.BuildDiscoveredPrinters(nil, nil, nil, &snap, profiles)
		view := View{
			CSRF:               a.csrf,
			Page:               "printer-scan",
			PrinterScan:        &snap,
			PrinterProfiles:    profiles,
			DiscoveredPrinters: discovered,
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := a.templates.ExecuteTemplate(w, "scanResults", view); err != nil {
			http.Error(w, "template render failed", http.StatusInternalServerError)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "scan": snap})
}

func (a *Application) handlePrinterScanCancel(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.Form.Get("scan"))
	if id == "" {
		id = strings.TrimSpace(r.Form.Get("id"))
	}
	msg := "جستجوی شبکه متوقف شد؛ نتایج تا این لحظه معتبر است."
	if !a.scanner().Cancel(id) {
		msg = "این جستجو در حال اجرا نیست یا قبلاً تمام شده است."
	}
	targetPage := "printer-scan"
	extra := map[string]string{"scan": id}
	if strings.TrimSpace(r.FormValue("return_page")) == "printers" {
		targetPage = "printers"
		extra["discovery"] = "lan"
	}
	a.navigateQuery(w, r, targetPage, extra, msg)
}

// ─── Print queue ────────────────────────────────────────────────────────

func (a *Application) handlePrintQueueRetry(w http.ResponseWriter, r *http.Request) {
	jobID := strings.TrimSpace(r.Form.Get("job_id"))
	job, err := a.store.PrintJob(r.Context(), jobID)
	if err != nil {
		a.navigateQuery(w, r, "print-queue", nil, err.Error())
		return
	}
	if !job.Retryable() {
		a.navigateQuery(w, r, "print-queue", nil, "این Job قابل ارسال دوباره نیست: "+job.RetryBlockReason())
		return
	}
	done := a.dispatchPrintJob(r.Context(), job)
	a.navigateQuery(w, r, "print-queue", map[string]string{"status": r.Form.Get("status_filter")}, printOutcomeMessage(done))
}

func (a *Application) handlePrintQueueRetryAll(w http.ResponseWriter, r *http.Request) {
	jobs, err := a.store.PrintJobsFilter(r.Context(), store.PrintStatusFailed, "", 50)
	if err != nil {
		a.navigateQuery(w, r, "print-queue", nil, err.Error())
		return
	}
	attempted, delivered := 0, 0
	for _, j := range jobs {
		if attempted >= 20 {
			break
		}
		if !j.Retryable() {
			continue
		}
		attempted++
		done := a.dispatchPrintJob(r.Context(), j)
		if done.Status == store.PrintStatusPrinted || done.Status == store.PrintStatusSent {
			delivered++
		}
	}
	msg := fmt.Sprintf("%d Job بررسی شد و %d مورد به چاپگر تحویل داده شد.", attempted, delivered)
	if attempted == 0 {
		msg = "هیچ Job خطادارِ قابل‌تلاش دوباره‌ای پیدا نشد؛ Jobهای با نتیجه نامشخص عمداً ارسال نمی‌شوند."
	}
	a.navigateQuery(w, r, "print-queue", nil, msg)
}

func (a *Application) handlePrintQueueCancel(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.Form.Get("job_id"))
	job, err := a.store.CancelPrintJob(r.Context(), id)
	if err != nil {
		a.navigateQuery(w, r, "print-queue", nil, "لغو Job انجام نشد: "+err.Error())
		return
	}
	a.navigateQuery(w, r, "print-queue", map[string]string{"status": r.Form.Get("status_filter")},
		"Job «"+job.DocTypeLabel()+"» لغو شد؛ سفارش و وضعیت آن تغییری نکرد.")
}

func (a *Application) handlePrintQueueCancelAll(w http.ResponseWriter, r *http.Request) {
	n, err := a.store.CancelAllPrintJobs(r.Context())
	if err != nil {
		a.navigateQuery(w, r, "print-queue", nil, "لغو صف چاپ انجام نشد: "+err.Error())
		return
	}
	msg := fmt.Sprintf("%d مورد در صف چاپ لغو شد؛ سفارش‌ها دست‌نخورده باقی ماندند.", n)
	if n == 0 {
		msg = "هیچ موردی در صف چاپ برای لغو وجود نداشت."
	}
	a.navigateQuery(w, r, "print-queue", nil, msg)
}

func (a *Application) handlePrintQueueClear(w http.ResponseWriter, r *http.Request) {
	n, err := a.store.ClearPrintQueue(r.Context())
	if err != nil {
		a.navigateQuery(w, r, "print-queue", nil, "پاک‌سازی صف چاپ انجام نشد: "+err.Error())
		return
	}
	msg := fmt.Sprintf("صف چاپ پاک‌سازی شد (%d مورد حذف شد).", n)
	if n == 0 {
		msg = "صف چاپ از قبل خالی بود."
	}
	a.navigateQuery(w, r, "print-queue", nil, msg)
}

// printOutcomeMessage separates "handed to the transport" from "physically
// printed". RAW printers cannot acknowledge paper, so the wording must not.
func printOutcomeMessage(job store.PrintJobView) string {
	switch job.Status {
	case store.PrintStatusSent:
		return "فیش «" + job.DocTypeLabel() + "» برای «" + job.PrinterTitle + "» به چاپگر ارسال شد؛ چاپگرهای RAW تأیید فیزیکی برنمی‌گردانند."
	case store.PrintStatusPrinted:
		return "فیش «" + job.DocTypeLabel() + "» برای «" + job.PrinterTitle + "» چاپ شد."
	default:
		return "ارسال فیش ناموفق بود: " + job.LastError
	}
}
