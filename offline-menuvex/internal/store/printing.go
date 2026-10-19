package store

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ─── چاپ مستقیم حرارتی (معادل آفلاین Printer Agent نسخه وب) ─────────────
// در وب، چاپ از مسیر Agent (ESC/POS) انجام می‌شود و پنجره Print مرورگر
// هرگز باز نمی‌شود. اینجا همان قرارداد به‌صورت بومی پیاده شده است:
// هر چاپ یک «Job» پایدار در دیتابیس محلی است با وضعیت قابل پیگیری
// (pending → printing → printed | failed)، مسیر (Routing) دسته‌بندی
// منو به چاپگر از تنظیمات خوانده می‌شود و برای هر ترکیب
// (سفارش × نوع فیش × چاپگر) حداکثر یک Job موفق ساخته می‌شود تا
// چاپ ناخواستهٔ دوباره رخ ندهد. ثبت فروش و چاپ دو مرحلهٔ جدا هستند:
// خطای چاپ هرگز سفارش ثبت‌شده را حذف یا دوباره ثبت نمی‌کند.

const (
	PrintStatusPending   = "pending"
	PrintStatusPrinting  = "printing"
	PrintStatusPrinted   = "printed"
	PrintStatusSent      = "sent"
	PrintStatusFailed    = "failed"
	PrintStatusCancelled = "cancelled"
)

// MaxPrintAttempts bounds automatic/manual retries for one job. Once reached,
// the operator must create a new job deliberately instead of hammering the
// device (the Agent caps active jobs; this caps retries of a single job).
const MaxPrintAttempts = 5

// PrintJobView نمای خواندنی یک Job چاپ برای قالب‌ها و API محلی است.
type PrintJobView struct {
	ID           string
	OrderID      string
	OrderNumber  string
	DocType      string // customer | kitchen | test
	PrinterID    string
	PrinterTitle string
	Connection   string
	Queue        string
	PaperWidth   string
	Status       string
	LastError    string
	Attempts     int
	Copies       int      // تعداد نسخه چاپ همین Job (پیش‌فرض ۱)
	Categories   []string // فقط بلیت آشپزخانه: شناسه دسته‌هایی که این بلیت پوشش می‌دهد (خالی = همه)
	// PrinterKind/Target snapshot the destination so a queued job remains
	// diagnosable after the printer is edited or deleted. Empty Kind means the
	// job was created by the pre-printer-centre code path (legacy transport).
	PrinterKind string
	Target      string
	Cut         bool
	Uncertain   bool
	CreatedAt   string
	UpdatedAt   string
}

// StatusLabel برچسب فارسی وضعیت Job برای نمایش در UI.
func (j PrintJobView) StatusLabel() string {
	switch j.Status {
	case PrintStatusPrinted:
		return "چاپ شد"
	case PrintStatusPrinting:
		return "در حال چاپ"
	case PrintStatusSent:
		return "ارسال شد (بدون تأیید چاپ)"
	case PrintStatusCancelled:
		return "لغو شد"
	case PrintStatusFailed:
		return "خطای چاپ"
	default:
		return "در صف چاپ"
	}
}

// StatusClass کلاس نشان وضعیت هم‌زبان با Badgeهای رابط.
func (j PrintJobView) StatusClass() string {
	switch j.Status {
	case PrintStatusPrinted:
		return "status-ok"
	case PrintStatusPrinting:
		return "status-info"
	case PrintStatusSent:
		return "status-info"
	case PrintStatusCancelled:
		return "status-muted"
	case PrintStatusFailed:
		return "status-danger"
	default:
		return "status-warn"
	}
}

// Retryable reports whether the operator can safely resend this job. A printed
// job is terminal. A failed job is retryable only when the transport confirmed
// that zero bytes were handed over; an uncertain failure may already be on
// paper.
func (j PrintJobView) Retryable() bool {
	return j.Status == PrintStatusFailed && !j.Uncertain && j.Attempts < MaxPrintAttempts
}

// RetryBlockReason explains a disabled retry button.
func (j PrintJobView) RetryBlockReason() string {
	switch {
	case j.Status == PrintStatusPrinted || j.Status == PrintStatusSent:
		return "این Job قبلاً به چاپگر تحویل داده شده است"
	case j.Status == PrintStatusCancelled:
		return "این Job لغو شده است"
	case j.Status == PrintStatusPrinting:
		return "این Job همین حالا در حال چاپ است"
	case j.Uncertain:
		return "نتیجهٔ ارسال نامشخص است؛ پیش از تلاش دوباره، کاغذ چاپگر را بررسی کنید"
	case j.Attempts >= MaxPrintAttempts:
		return "حداکثر تلاش مجاز برای این Job تمام شده است"
	case j.Status != PrintStatusFailed:
		return "این Job در صف است و نیازی به تلاش دوباره ندارد"
	}
	return ""
}

// DocTypeLabel برچسب فارسی نوع فیش.
func (j PrintJobView) DocTypeLabel() string {
	switch j.DocType {
	case "kitchen":
		return "بلیت آشپزخانه (بار)"
	case "test":
		return "فیش آزمایشی چاپگر"
	default:
		return "فیش مشتری (صندوق)"
	}
}

type printJobPayload struct {
	ID           string   `json:"id"`
	OrderID      string   `json:"order_id"`
	OrderNumber  string   `json:"order_number"`
	DocType      string   `json:"doc_type"`
	PrinterID    string   `json:"printer_id"`
	PrinterTitle string   `json:"printer_title"`
	Connection   string   `json:"connection"`
	Queue        string   `json:"queue"`
	PaperWidth   string   `json:"paper_width"`
	Status       string   `json:"status"`
	LastError    string   `json:"last_error"`
	Attempts     int      `json:"attempts"`
	Copies       int      `json:"copies,omitempty"`
	Categories   []string `json:"categories,omitempty"`
	PrinterKind  string   `json:"printer_kind,omitempty"`
	Target       string   `json:"target,omitempty"`
	Cut          *bool    `json:"cut,omitempty"`
	Uncertain    bool     `json:"uncertain,omitempty"`
	DedupeKey    string   `json:"dedupe_key"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
	Version      int64    `json:"version"`
}

func printJobView(p printJobPayload) PrintJobView {
	copies := p.Copies
	if copies < 1 {
		copies = 1
	}
	cut := true
	if p.Cut != nil {
		cut = *p.Cut
	}
	return PrintJobView{
		ID: p.ID, OrderID: p.OrderID, OrderNumber: p.OrderNumber, DocType: p.DocType,
		PrinterID: p.PrinterID, PrinterTitle: p.PrinterTitle, Connection: p.Connection,
		Queue: p.Queue, PaperWidth: p.PaperWidth, Status: p.Status, LastError: p.LastError,
		Attempts: p.Attempts, Copies: copies, Categories: p.Categories,
		PrinterKind: p.PrinterKind, Target: p.Target, Cut: cut, Uncertain: p.Uncertain,
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
}

func decodePrintJob(e storedEntity) (printJobPayload, bool) {
	var p printJobPayload
	if json.Unmarshal(e.Payload, &p) != nil || p.ID == "" {
		return p, false
	}
	return p, true
}

func isActivePrinter(e Entity) bool {
	v := strings.ToLower(strings.TrimSpace(e.Field("is_active")))
	return v != "false" && v != "0" && v != "غیرفعال"
}

// EnqueueOrderPrintJobs چاپ کامل پیش‌فرض (فیش مشتری + بلیت آشپزخانه) —
// برای چاپ دستی از صفحه فیش و منوی سفارش‌ها.
func (s *Store) EnqueueOrderPrintJobs(ctx context.Context, orderID string) ([]PrintJobView, error) {
	return s.EnqueueOrderPrintJobsDocs(ctx, orderID, map[string]int{"customer": 1, "kitchen": 1})
}

// EnqueueOrderPrintJobsDocs بر اساس چاپگرها و قوانین مسیردهی ذخیره‌شده،
// Jobهای چاپ یک سفارش را فقط برای سندهای خواسته‌شده می‌سازد
// (docs: نوع سند ← تعداد نسخه). فراخوانی دوباره برای همان ترکیب
// (سفارش+سند+چاپگر+نسخه+دسته‌ها) Job موفق/درصف تکراری نمی‌سازد.
// مسیریابی آشپزخانه مانند وب است: دسته دارای مسیر فعال فقط روی چاپگر
// خودش و فقط با اقلام همان دسته چاپ می‌شود؛ دسته‌های بدون مسیر به
// پرینتر پیش‌فرض آشپزخانه می‌روند و تا وقتی هیچ مسیری فعال نباشد،
// رفتار قبلی (یک بلیت کامل برای ایستگاه بار/آشپزخانه) ادامه دارد.
func (s *Store) EnqueueOrderPrintJobsDocs(ctx context.Context, orderID string, docs map[string]int) ([]PrintJobView, error) {
	order, err := s.Entity(ctx, "order", orderID)
	if err != nil {
		return nil, errors.New("سفارش مورد نظر برای چاپ یافت نشد")
	}
	wantCustomer, wantKitchen := docs["customer"], docs["kitchen"]
	if wantCustomer <= 0 && wantKitchen <= 0 {
		return nil, nil
	}
	printers, _ := s.Entities(ctx, "printer")
	routes, _ := s.Entities(ctx, "printer_route")
	products, _ := s.Entities(ctx, "product")
	categories, _ := s.Entities(ctx, "category")

	active := make([]Entity, 0, len(printers))
	for _, p := range printers {
		if isActivePrinter(p) {
			active = append(active, p)
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	orderNumber := order.Field("order_number")

	findPrinter := func(match func(Entity) bool) *Entity {
		for i := range active {
			if match(active[i]) {
				return &active[i]
			}
		}
		return nil
	}
	byID := func(id string) *Entity {
		if id == "" {
			return nil
		}
		for i := range active {
			if active[i].ID == id {
				return &active[i]
			}
		}
		return nil
	}
	hasRole := func(printer Entity, role string) bool {
		r := strings.ToLower(strings.TrimSpace(printer.Field("role")))
		if r == role || r == "all" || r == "both" {
			return true
		}
		text := printer.Field("station") + " " + printer.Label()
		switch role {
		case "cashier":
			return strings.Contains(text, "صندوق") || strings.Contains(strings.ToLower(text), "cashier")
		case "kitchen":
			return strings.Contains(text, "بار") || strings.Contains(text, "آشپزخانه") || strings.Contains(strings.ToLower(text), "kitchen")
		}
		return false
	}
	nameMatches := func(printer Entity, name string) bool {
		name = strings.TrimSpace(name)
		if name == "" {
			return false
		}
		label := printer.Label()
		return label == name || strings.Contains(label, name) || strings.Contains(name, label)
	}

	if len(active) == 0 {
		// Job ناموفقِ صریح: کاربر باید ببیند چرا فیش چاپ نشده است.
		doc, copies := "customer", wantCustomer
		if copies <= 0 {
			doc, copies = "kitchen", wantKitchen
		}
		jobs, jErr := s.createPrintJobs(ctx, orderID, orderNumber, []printJobPayload{{
			OrderID: orderID, OrderNumber: orderNumber, DocType: doc, Copies: copies,
			DedupeKey:    orderID + "|" + doc + "||" + strconv.Itoa(copies) + "|all",
			PrinterTitle: "چاپگر تعریف‌نشده", PaperWidth: "80mm",
			Status: PrintStatusFailed, CreatedAt: now, UpdatedAt: now,
			LastError: "هیچ چاپگر فعالی تعریف نشده است؛ از «پرینترها و تنظیمات» یک چاپگر حرارتی اضافه کنید.",
		}})
		return jobs, jErr
	}

	// فیش مشتری ← چاپگر صندوق (ایستگاه صندوق/کشیر) یا اولین چاپگر فعال
	cashier := findPrinter(func(p Entity) bool { return hasRole(p, "cashier") })
	if cashier == nil {
		cashier = &active[0]
	}
	kitchenStation := func() *Entity {
		return findPrinter(func(p Entity) bool { return hasRole(p, "kitchen") })
	}

	type wanted struct {
		DocType string
		Printer *Entity
		Copies  int
		Cats    []string // nil = همه دسته‌ها
	}
	var wants []wanted
	if wantCustomer > 0 {
		wants = append(wants, wanted{DocType: "customer", Printer: cashier, Copies: wantCustomer})
	}

	if wantKitchen > 0 {
		catTitleByID := map[string]string{}
		for _, c := range categories {
			catTitleByID[c.ID] = c.Label()
		}
		productCat := map[string]string{}
		for _, p := range products {
			productCat[p.ID] = p.Field("category_id")
		}
		orderCats := map[string]bool{}
		for _, line := range order.OrderItems() {
			if c := productCat[line.ProductID]; c != "" {
				orderCats[c] = true
			}
		}
		// قوانین مسیردهی فعال: دسته ← چاپگر و تعداد نسخه مسیر
		type routeInfo struct {
			printer *Entity
			copies  int
		}
		routeByCat := map[string]routeInfo{}
		anyRoute := false
		for _, r := range routes {
			if !isActivePrinter(r) {
				continue
			}
			catID := strings.TrimSpace(r.Field("category_id"))
			if catID == "" {
				for id, ttl := range catTitleByID {
					if ttl != "" && ttl == strings.TrimSpace(r.Field("category_name")) {
						catID = id
					}
				}
			}
			pr := byID(strings.TrimSpace(r.Field("printer_id")))
			if pr == nil {
				pr = findPrinter(func(x Entity) bool { return nameMatches(x, r.Field("printer_name")) })
			}
			if catID == "" || pr == nil {
				continue
			}
			anyRoute = true
			rc, aErr := strconv.Atoi(strings.TrimSpace(r.Field("copies")))
			if aErr != nil || rc < 1 {
				rc = 1
			}
			if rc > 5 {
				rc = 5
			}
			routeByCat[catID] = routeInfo{printer: pr, copies: rc}
		}
		// بلیت‌های آشپزخانه بر اساس چاپگر ادغام می‌شوند تا برای یک چاپگر
		// چند Job تکه‌تکه ساخته نشود.
		kitchenByPrinter := map[string]*wanted{}
		var kitchenWants []*wanted
		addKitchen := func(p *Entity, cats []string, copies int) {
			if p == nil {
				return
			}
			if copies < 1 {
				copies = 1
			}
			if copies > 5 {
				copies = 5
			}
			if w, ok := kitchenByPrinter[p.ID]; ok {
				if cats == nil {
					w.Cats = nil
				} else if w.Cats != nil {
					seen := map[string]bool{}
					for _, c := range w.Cats {
						seen[c] = true
					}
					for _, c := range cats {
						if !seen[c] {
							w.Cats = append(w.Cats, c)
						}
					}
				}
				if copies > w.Copies {
					w.Copies = copies
				}
				return
			}
			nw := &wanted{DocType: "kitchen", Printer: p, Copies: copies, Cats: cats}
			kitchenByPrinter[p.ID] = nw
			kitchenWants = append(kitchenWants, nw)
		}
		var leftover []string
		for catID := range orderCats {
			if ri, ok := routeByCat[catID]; ok {
				addKitchen(ri.printer, []string{catID}, wantKitchen*ri.copies)
			} else {
				leftover = append(leftover, catID)
			}
		}
		if !anyRoute {
			// رفتار قبلی: یک بلیت کامل برای چاپگر ایستگاه بار/آشپزخانه (اگر جدا از صندوق است)
			if kp := kitchenStation(); kp != nil && kp.ID != cashier.ID {
				addKitchen(kp, nil, wantKitchen)
			}
		} else if len(leftover) > 0 {
			// دسته‌های بدون مسیر ← پرینتر پیش‌فرض آشپزخانه (قاعده وب)
			if kp := kitchenStation(); kp != nil {
				addKitchen(kp, leftover, wantKitchen)
			}
		}
		for _, kw := range kitchenWants {
			wants = append(wants, *kw)
		}
	}

	catSig := func(cats []string) string {
		if cats == nil {
			return "all"
		}
		s := append([]string(nil), cats...)
		sort.Strings(s)
		return strings.Join(s, ",")
	}
	payloads := make([]printJobPayload, 0, len(wants))
	for _, wn := range wants {
		paper := wn.Printer.Field("paper_width")
		if paper != "58mm" && paper != "80mm" {
			paper = "80mm"
		}
		cats := wn.Cats
		if cats != nil {
			sorted := append([]string(nil), cats...)
			sort.Strings(sorted)
			cats = sorted
		}
		profile := ProfileFromEntity(*wn.Printer)
		kind := strings.ToLower(strings.TrimSpace(wn.Printer.Field("kind")))
		target := ""
		if kind != "" {
			target = profile.TargetLabel()
		}
		cut := profile.Cut
		payloads = append(payloads, printJobPayload{
			OrderID: orderID, OrderNumber: orderNumber, DocType: wn.DocType,
			PrinterID: wn.Printer.ID, PrinterTitle: wn.Printer.Label(),
			Connection: wn.Printer.Field("connection"), Queue: wn.Printer.Field("queue"),
			PaperWidth: paper, Status: PrintStatusPending, CreatedAt: now, UpdatedAt: now,
			Copies: wn.Copies, Categories: cats,
			PrinterKind: kind, Target: target, Cut: &cut,
			DedupeKey: orderID + "|" + wn.DocType + "|" + wn.Printer.ID + "|" + strconv.Itoa(wn.Copies) + "|" + catSig(cats),
		})
	}
	return s.createPrintJobs(ctx, orderID, orderNumber, payloads)
}

// EnqueueTestPrintJob برای دکمه «چاپ آزمایشی» تنظیمات چاپگر.
func (s *Store) EnqueueTestPrintJob(ctx context.Context, printerID string) (PrintJobView, error) {
	printer, err := s.Entity(ctx, "printer", printerID)
	if err != nil {
		return PrintJobView{}, errors.New("چاپگر مورد نظر یافت نشد")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	paper := printer.Field("paper_width")
	if paper != "58mm" && paper != "80mm" {
		paper = "80mm"
	}
	id, err := UUID()
	if err != nil {
		return PrintJobView{}, err
	}
	profile := ProfileFromEntity(printer)
	kind := strings.ToLower(strings.TrimSpace(printer.Field("kind")))
	target := ""
	if kind != "" {
		target = profile.TargetLabel()
	}
	cut := profile.Cut
	p := printJobPayload{
		ID: id, DocType: "test", PrinterID: printer.ID, PrinterTitle: printer.Label(),
		Connection: printer.Field("connection"), Queue: printer.Field("queue"),
		PaperWidth: paper, Status: PrintStatusPending, CreatedAt: now, UpdatedAt: now,
		PrinterKind: kind, Target: target, Cut: &cut,
		DedupeKey: "test|" + id,
	}
	if err := s.savePrintJob(ctx, p); err != nil {
		return PrintJobView{}, err
	}
	return printJobView(p), nil
}

func (s *Store) createPrintJobs(ctx context.Context, orderID, orderNumber string, payloads []printJobPayload) ([]PrintJobView, error) {
	existing, _ := s.PrintJobs(ctx, orderID)
	byDedupe := map[string]PrintJobView{}
	for _, j := range existing {
		// کلید dedupe از روی ترکیب فیلدها بازسازی می‌شود (در View نگه داشته نمی‌شود)
		sig := "all"
		if j.Categories != nil {
			s := append([]string(nil), j.Categories...)
			sort.Strings(s)
			sig = strings.Join(s, ",")
		}
		byDedupe[j.OrderID+"|"+j.DocType+"|"+j.PrinterID+"|"+strconv.Itoa(j.Copies)+"|"+sig] = j
	}
	out := make([]PrintJobView, 0, len(payloads))
	for _, p := range payloads {
		if p.DedupeKey != "" {
			if old, ok := byDedupe[p.DedupeKey]; ok &&
				(old.Status == PrintStatusPending || old.Status == PrintStatusPrinting ||
					old.Status == PrintStatusPrinted || old.Status == PrintStatusSent) {
				out = append(out, old)
				continue
			}
			if old, ok := byDedupe[p.DedupeKey]; ok && old.Status == PrintStatusFailed {
				// Job ناموفق قبلی همان مقصد: همان رکورد برای Retry نگه داشته می‌شود.
				out = append(out, old)
				continue
			}
		}
		id, err := UUID()
		if err != nil {
			return out, err
		}
		p.ID = id
		if err := s.savePrintJob(ctx, p); err != nil {
			return out, err
		}
		out = append(out, printJobView(p))
	}
	return out, nil
}

func (s *Store) savePrintJob(ctx context.Context, p printJobPayload) error {
	p.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return s.withTx(ctx, func(st *persistedDB) error {
		key := entityKey("print_job", p.ID)
		nextVer := int64(1)
		if old, ok := st.Entities[key]; ok {
			nextVer = old.Version + 1
		}
		p.Version = nextVer
		raw, err := json.Marshal(p)
		if err != nil {
			return err
		}
		st.Entities[key] = storedEntity{ID: p.ID, Type: "print_job", Version: nextVer, Payload: raw}
		return nil
	})
}

// PrintJobs فهرست Jobها (برای یک سفارش یا همه) از تازه به قدیمی.
func (s *Store) PrintJobs(ctx context.Context, orderID string) ([]PrintJobView, error) {
	ents, err := s.Entities(ctx, "print_job")
	if err != nil {
		return nil, err
	}
	out := make([]PrintJobView, 0, len(ents))
	for _, e := range ents {
		p, ok := decodePrintJob(storedEntity{ID: e.ID, Type: e.Type, Version: e.Version, Payload: e.Payload})
		if !ok {
			continue
		}
		if orderID != "" && p.OrderID != orderID {
			continue
		}
		out = append(out, printJobView(p))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	if len(out) > 100 {
		out = out[:100]
	}
	return out, nil
}

// PrintJob یک Job را برمی‌گرداند.
func (s *Store) PrintJob(ctx context.Context, jobID string) (PrintJobView, error) {
	e, err := s.Entity(ctx, "print_job", jobID)
	if err != nil {
		return PrintJobView{}, errors.New("Job چاپ یافت نشد")
	}
	p, ok := decodePrintJob(storedEntity{ID: e.ID, Type: e.Type, Version: e.Version, Payload: e.Payload})
	if !ok {
		return PrintJobView{}, errors.New("Job چاپ نامعتبر است")
	}
	return printJobView(p), nil
}

// UpdatePrintJobStatus وضعیت Job را در همان تراکنش دیتابیس به‌روز می‌کند.
// شروع هر تلاش (pending→printing) شمارنده تلاش را یک واحد بالا می‌برد.
func (s *Store) UpdatePrintJobStatus(ctx context.Context, jobID, status, lastError string) (PrintJobView, error) {
	var result PrintJobView
	err := s.withTx(ctx, func(st *persistedDB) error {
		key := entityKey("print_job", jobID)
		e, ok := st.Entities[key]
		if !ok || e.Deleted {
			return errors.New("Job چاپ یافت نشد")
		}
		p, decOK := decodePrintJob(e)
		if !decOK {
			return errors.New("Job چاپ نامعتبر است")
		}
		if status == PrintStatusPrinting {
			p.Attempts++
		}
		p.Status = status
		p.LastError = lastError
		p.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		p.Version++
		raw, mErr := json.Marshal(p)
		if mErr != nil {
			return mErr
		}
		e.Version = p.Version
		e.Payload = raw
		st.Entities[key] = e
		result = printJobView(p)
		return nil
	})
	return result, err
}

// ClaimPrintJob atomically moves a job to "printing" before any byte is sent.
// This is the concurrency guard that makes repeated HTTP retries safe: the
// second request sees "printing"/"sent"/"printed" and is refused instead of
// printing the same receipt again.
func (s *Store) ClaimPrintJob(ctx context.Context, jobID string) (PrintJobView, error) {
	var result PrintJobView
	err := s.withTx(ctx, func(st *persistedDB) error {
		key := entityKey("print_job", jobID)
		e, ok := st.Entities[key]
		if !ok || e.Deleted {
			return errors.New("Job چاپ یافت نشد")
		}
		p, decOK := decodePrintJob(e)
		if !decOK {
			return errors.New("Job چاپ نامعتبر است")
		}
		switch p.Status {
		case PrintStatusPrinting:
			return errors.New("این Job همین حالا در حال چاپ است؛ درخواست تکراری ارسال نشد")
		case PrintStatusPrinted, PrintStatusSent:
			return errors.New("این Job قبلاً به چاپگر تحویل داده شده است و دوباره ارسال نمی‌شود")
		case PrintStatusCancelled:
			return errors.New("این Job لغو شده است و دوباره ارسال نمی‌شود")
		}
		if p.Uncertain {
			return errors.New("نتیجهٔ ارسال قبلی نامشخص است؛ برای جلوگیری از چاپ تکراری، ابتدا کاغذ چاپگر را بررسی کنید")
		}
		if p.Attempts >= MaxPrintAttempts {
			return errors.New("حداکثر تلاش مجاز برای این Job تمام شده است")
		}
		p.Status = PrintStatusPrinting
		p.Attempts++
		p.LastError = ""
		p.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		p.Version++
		raw, mErr := json.Marshal(p)
		if mErr != nil {
			return mErr
		}
		e.Version = p.Version
		e.Payload = raw
		st.Entities[key] = e
		result = printJobView(p)
		return nil
	})
	return result, err
}

// FailPrintJob records a failed delivery. uncertain means bytes may already
// have reached the device, so the UI must not offer a one-click retry.
func (s *Store) FailPrintJob(ctx context.Context, jobID, message string, uncertain bool) (PrintJobView, error) {
	var result PrintJobView
	err := s.withTx(ctx, func(st *persistedDB) error {
		key := entityKey("print_job", jobID)
		e, ok := st.Entities[key]
		if !ok || e.Deleted {
			return errors.New("Job چاپ یافت نشد")
		}
		p, decOK := decodePrintJob(e)
		if !decOK {
			return errors.New("Job چاپ نامعتبر است")
		}
		p.Status = PrintStatusFailed
		p.LastError = message
		p.Uncertain = uncertain
		p.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		p.Version++
		raw, mErr := json.Marshal(p)
		if mErr != nil {
			return mErr
		}
		e.Version = p.Version
		e.Payload = raw
		st.Entities[key] = e
		result = printJobView(p)
		return nil
	})
	return result, err
}

// PrintJobsFilter lists jobs by optional status and printer, newest first.
func (s *Store) PrintJobsFilter(ctx context.Context, status, printerID string, limit int) ([]PrintJobView, error) {
	all, err := s.printJobsAll(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]PrintJobView, 0, len(all))
	for _, j := range all {
		if status != "" && j.Status != status {
			continue
		}
		if printerID != "" && j.PrinterID != printerID {
			continue
		}
		out = append(out, j)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// PrintJobCounts summarizes the queue for the management dashboard.
type PrintJobCounts struct {
	Total, Pending, Printing, Printed, Sent, Failed, Cancelled, Retryable int
}

// PrintJobCounts computes counters from every stored job (not only the latest
// 100 returned by PrintJobs).
func (s *Store) PrintJobCounts(ctx context.Context) PrintJobCounts {
	all, _ := s.printJobsAll(ctx)
	var c PrintJobCounts
	for _, j := range all {
		c.Total++
		switch j.Status {
		case PrintStatusPending:
			c.Pending++
		case PrintStatusPrinting:
			c.Printing++
		case PrintStatusPrinted:
			c.Printed++
		case PrintStatusSent:
			c.Sent++
		case PrintStatusFailed:
			c.Failed++
		case PrintStatusCancelled:
			c.Cancelled++
		}
		if j.Retryable() {
			c.Retryable++
		}
	}
	return c
}

func (s *Store) printJobsAll(ctx context.Context) ([]PrintJobView, error) {
	ents, err := s.Entities(ctx, "print_job")
	if err != nil {
		return nil, err
	}
	out := make([]PrintJobView, 0, len(ents))
	for _, e := range ents {
		p, ok := decodePrintJob(storedEntity{ID: e.ID, Type: e.Type, Version: e.Version, Payload: e.Payload})
		if !ok {
			continue
		}
		out = append(out, printJobView(p))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt == out[j].CreatedAt {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt > out[j].CreatedAt
	})
	return out, nil
}

// CancelPrintJob cancels a job as long as it has not already been delivered to
// the printer (pending, failed, or stuck printing). Jobs that are already sent
// or printed cannot be cancelled.
func (s *Store) CancelPrintJob(ctx context.Context, jobID string) (PrintJobView, error) {
	var result PrintJobView
	err := s.withTx(ctx, func(st *persistedDB) error {
		key := entityKey("print_job", jobID)
		e, ok := st.Entities[key]
		if !ok || e.Deleted {
			return errors.New("Job چاپ یافت نشد")
		}
		p, decOK := decodePrintJob(e)
		if !decOK {
			return errors.New("Job چاپ نامعتبر است")
		}
		if p.Status == PrintStatusPrinted || p.Status == PrintStatusSent {
			return errors.New("فقط Job در صف یا خطادار را می‌توان لغو کرد؛ این Job قبلاً ارسال شده است")
		}
		if p.Status == PrintStatusCancelled {
			result = printJobView(p)
			return nil
		}
		p.Status = PrintStatusCancelled
		p.Uncertain = false
		p.LastError = "توسط کاربر لغو شد"
		p.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		p.Version++
		raw, mErr := json.Marshal(p)
		if mErr != nil {
			return mErr
		}
		e.Version = p.Version
		e.Payload = raw
		st.Entities[key] = e
		result = printJobView(p)
		return nil
	})
	return result, err
}

// CancelAllPrintJobs cancels all pending, printing, and failed jobs in the
// queue so the operator can stop/clear active and errored queue items at once.
func (s *Store) CancelAllPrintJobs(ctx context.Context) (int, error) {
	cancelled := 0
	err := s.withTx(ctx, func(st *persistedDB) error {
		now := time.Now().UTC().Format(time.RFC3339)
		for key, e := range st.Entities {
			if e.Type != "print_job" || e.Deleted {
				continue
			}
			p, ok := decodePrintJob(e)
			if !ok {
				continue
			}
			if p.Status != PrintStatusPending && p.Status != PrintStatusFailed && p.Status != PrintStatusPrinting {
				continue
			}
			p.Status = PrintStatusCancelled
			p.Uncertain = false
			p.LastError = "توسط کاربر لغو شد"
			p.UpdatedAt = now
			p.Version++
			raw, mErr := json.Marshal(p)
			if mErr != nil {
				return mErr
			}
			e.Version = p.Version
			e.Payload = raw
			st.Entities[key] = e
			cancelled++
		}
		return nil
	})
	return cancelled, err
}

// ClearPrintQueue deletes all print jobs from the queue (except any in-flight
// job currently mid-write, which is marked cancelled) so the operator can start
// with a clean queue.
func (s *Store) ClearPrintQueue(ctx context.Context) (int, error) {
	cleared := 0
	err := s.withTx(ctx, func(st *persistedDB) error {
		for key, e := range st.Entities {
			if e.Type != "print_job" || e.Deleted {
				continue
			}
			e.Deleted = true
			e.Version++
			st.Entities[key] = e
			cleared++
		}
		return nil
	})
	return cleared, err
}

// RecoverInterruptedPrintJobs closes jobs that were mid-send when the process
// stopped. Bytes may or may not have reached the printer, so the recovered job
// is marked uncertain and is never offered for automatic/one-click retry.
func (s *Store) RecoverInterruptedPrintJobs(ctx context.Context) (int, error) {
	recovered := 0
	err := s.withTx(ctx, func(st *persistedDB) error {
		now := time.Now().UTC().Format(time.RFC3339)
		for key, e := range st.Entities {
			if e.Type != "print_job" || e.Deleted {
				continue
			}
			p, ok := decodePrintJob(e)
			if !ok || p.Status != PrintStatusPrinting {
				continue
			}
			p.Status = PrintStatusFailed
			p.Uncertain = true
			p.LastError = "برنامه پیش از پایان ارسال بسته شد؛ ممکن است بخشی از فیش چاپ شده باشد. پیش از هر اقدامی کاغذ چاپگر را بررسی کنید."
			p.UpdatedAt = now
			p.Version++
			raw, err := json.Marshal(p)
			if err != nil {
				return err
			}
			e.Payload = raw
			e.Version = p.Version
			st.Entities[key] = e
			recovered++
		}
		return nil
	})
	return recovered, err
}
