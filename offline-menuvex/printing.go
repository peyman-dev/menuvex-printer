package desktop

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/sinaqaffari/coffish/desktop/internal/printer"
	"github.com/sinaqaffari/coffish/desktop/internal/store"
)

// ─── چاپ مستقیم حرارتی بدون پنجره Print مرورگر ─────────────────────────
// قرارداد محصول در نسخه وب (core/lib/pos-printer/print-method.ts):
// «هیچ‌کدام از مسیرهای چاپ پنجره پیش‌فرض چاپ مرورگر را باز نمی‌کنند».
// معادل بومی آفلاین همین‌جاست: Job چاپ در دیتابیس محلی ساخته می‌شود،
// سند ESC/POS در Go تولید و مستقیم به چاپگر می‌رسد:
//   ۱) چاپگر شبکه: اتصال TCP خام به پورت 9100 (استاندارد فیش‌پرینترها)
//   ۲) صف چاپ سیستم‌عامل: lp/CUPS در لینوکس و macOS (در صورت وجود صف)
//   ۳) پوشه Spool (COFFISH_PRINT_SPOOL_DIR) برای یکپارچه‌سازی فایلی و تست
// اگر هیچ مسیری در دسترس نباشد Job با خطای روشن «failed» می‌ماند،
// قابل Retry است و سفارش ثبت‌شده دست‌نخورده باقی می‌ماند.

type receiptLine struct {
	Text   string
	Bold   bool
	Center bool
	Big    bool // اندازه دوبرابر (GS !) برای تأکیدهای فیش وب
	Feed   bool // خط جداکننده نقطه‌چین (dashed وب)
	Solid  bool // خط جداکننده توپر (solid وب)
}

var networkPrinterPattern = regexp.MustCompile(`(\d{1,3}(?:\.\d{1,3}){3}):(\d{2,5})`)

// parseNetworkTarget آدرس host:port را از فیلد آزاد «اتصال» چاپگر بیرون می‌کشد
// (مثل «LAN 192.168.1.201:9100» یا «192.168.1.203:9100»).
func parseNetworkTarget(connection string) string {
	m := networkPrinterPattern.FindString(connection)
	return m
}

// ─── نگاشت‌های فیش، هم‌معنای FactorPrint / KitchenTicket وب ──────────────

// digitsFa فقط رقم‌های یک متن را به‌صورت ارقام فارسی برمی‌گرداند
// (شماره میز روی فیش وب فقط رقم است: «میز ۳»).
func digitsFa(v string) string {
	var b strings.Builder
	for _, r := range v {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= '۰' && r <= '۹':
			b.WriteRune('0' + (r - '۰'))
		case r >= '٠' && r <= '٩':
			b.WriteRune('0' + (r - '٠'))
		}
	}
	return store.ToPersianDigits(b.String())
}

// jalaliDateTimeFa تاریخ فیش وب: «۱۴۰۵/۰۷/۲۶  ۲۱:۱۴» (Jalali + ساعت تهران).
func jalaliDateTimeFa(iso string) string {
	iso = strings.TrimSpace(iso)
	if iso == "" {
		return ""
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, iso); err == nil {
			loc := t.In(tehranZone())
			jy, jm, jd := GregorianToJalali(loc.Year(), int(loc.Month()), loc.Day())
			return store.ToPersianDigits(fmt.Sprintf("%04d/%02d/%02d  %02d:%02d", jy, jm, jd, loc.Hour(), loc.Minute()))
		}
	}
	return store.ToPersianDigits(iso)
}

// jalaliTimeFa فقط ساعت «۲۱:۱۴» برای بلیت آشپزخانه وب.
func jalaliTimeFa(iso string) string {
	full := jalaliDateTimeFa(iso)
	if idx := strings.LastIndex(full, "  "); idx >= 0 {
		return full[idx+2:]
	}
	return full
}

// orderTypeLabelFa نوع سفارش مانند getOrderTypeLabel وب.
func orderTypeLabelFa(order store.Entity) string {
	switch strings.ToLower(strings.TrimSpace(order.Field("fulfillment_type"))) {
	case "outside", "takeaway":
		return "بیرون‌بر"
	case "delivery":
		return "ارسال"
	default:
		return "حضوری"
	}
}

// paymentLabelFa روش پرداخت مانند برچسب‌های صندوق وب.
func paymentLabelFa(order store.Entity) string {
	switch strings.ToLower(strings.TrimSpace(order.Field("payment_method"))) {
	case "cash", "card_pos":
		return "نقدی / کارتخوان"
	case "credit_debtor", "credit":
		return "خرید اعتباری (نسیه)"
	case "mixed":
		return "ترکیبی"
	}
	return ""
}

// cafeReceiptTexts متن سربرگ/پابرگ فیش صندوق از تنظیمات چاپگرها (وب).
// receipt_note قدیمی به‌عنوان پابرگ خوانده می‌شود تا داده قبلی از دست نرود.
func cafeReceiptTexts(cafe store.Entity) (header, footer string) {
	if cafe.ID == "" {
		return "", ""
	}
	header = strings.TrimSpace(cafe.Field("receipt_header_text"))
	footer = strings.TrimSpace(cafe.Field("receipt_footer_text"))
	if footer == "" {
		footer = strings.TrimSpace(cafe.Field("receipt_note"))
	}
	return header, footer
}

// printerEnabled مشخص می‌کند کاربر استفاده از چاپگر حرارتی را فعال کرده یا نه.
// انتخاب صریح کاربر در پروفایل کافه ذخیره می‌شود (printer_enabled)؛ تا وقتی
// انتخابی ثبت نشده، داشتنِ حداقل یک چاپگر تعریف‌شده به‌معنای فعال است تا
// نصب‌های فعلی بدون تغییر رفتار ادامه دهند و صندوقِ بدون چاپگر دکمه چاپ نبیند.
// printSender is the injectable transport used by the new printer subsystem.
// Tests replace Application.sender with a fake; production falls back to the
// audited network/spooler/USB transport.
func (a *Application) printSender() *printer.Sender {
	if a.sender != nil {
		return a.sender
	}
	return printer.NewSender()
}

func (a *Application) printerEnabled(ctx context.Context) bool {
	if prof, err := a.store.Entity(ctx, "cafe_profile", "cafe-main-profile"); err == nil {
		switch strings.TrimSpace(prof.Field("printer_enabled")) {
		case "true":
			return true
		case "false":
			return false
		}
	}
	printers, _ := a.store.Entities(ctx, "printer")
	for _, p := range printers {
		if strings.ToLower(strings.TrimSpace(p.Field("is_active"))) == "false" ||
			strings.TrimSpace(p.Field("is_active")) == "0" ||
			strings.Contains(p.Field("status"), "غیرفعال") {
			continue
		}
		return true
	}
	return false
}

// printerDisabledMsg پیام یکدست رد چاپ وقتی کاربر چاپگر را غیرفعال کرده است.
const printerDisabledMsg = "چاپگر حرارتی غیرفعال است؛ اگر فیش‌پرینتر دارید از «پرینترها و تنظیمات» آن را فعال کنید."

// PrintScenario یک سناریوی چاپ خودکار وب برای یک لحظه از چرخه سفارش است:
// کاربر با جمله ساده انتخاب می‌کند («فقط برای آشپزخانه پرینت بگیره» و…).
type PrintScenario struct {
	Mode     string // none | kitchen | customer | both | custom
	Doc      string // kitchen | cashier | all (فقط چیدمان دلخواه)
	Copies   int    // ۱ تا ۵
	Merchant bool   // فقط تسویه: نسخه کافه هم چاپ شود (چیدمان دلخواه)
	Checkout bool   // این سناریو مربوط به «موقع تسویه صندوق» است
}

// PrintPolicy سیاست چاپ خودکار چهار لحظه سفارش (PrintWorkflowPolicy وب).
type PrintPolicy struct {
	Create, NoTable, Checkout, Complete PrintScenario
}

// printPolicy سیاست ذخیره‌شده در دیتابیس محلی را با پیش‌فرض‌های وب برمی‌گرداند
// (DEFAULT_PRINT_WORKFLOW_POLICY: ثبت=آشپزخانه، بدون‌میز=آشپزخانه،
// تسویه=مشتری+کافه، تکمیل=مشتری).
func (a *Application) printPolicy(ctx context.Context) PrintPolicy {
	p := PrintPolicy{
		Create:   PrintScenario{Mode: "kitchen", Doc: "kitchen", Copies: 1},
		NoTable:  PrintScenario{Mode: "kitchen", Doc: "kitchen", Copies: 1},
		Checkout: PrintScenario{Mode: "both", Doc: "cashier", Copies: 2, Merchant: true, Checkout: true},
		Complete: PrintScenario{Mode: "customer", Doc: "cashier", Copies: 1},
	}
	prof, err := a.store.Entity(ctx, "cafe_profile", "cafe-main-profile")
	if err != nil {
		return p
	}
	apply := func(sc *PrintScenario, prefix string) {
		if v := strings.TrimSpace(prof.Field(prefix + "_mode")); v != "" {
			sc.Mode = v
		}
		if v := strings.TrimSpace(prof.Field(prefix + "_doc")); v != "" {
			sc.Doc = v
		}
		if v := strings.TrimSpace(prof.Field(prefix + "_copies")); v != "" {
			if n, aErr := strconv.Atoi(v); aErr == nil && n >= 1 && n <= 5 {
				sc.Copies = n
			}
		}
	}
	apply(&p.Create, "pw_create")
	apply(&p.NoTable, "pw_notable")
	apply(&p.Checkout, "pw_checkout")
	apply(&p.Complete, "pw_complete")
	if v := strings.TrimSpace(prof.Field("pw_checkout_merchant")); v != "" {
		p.Checkout.Merchant = v == "true"
	}
	return p
}

// docs سندها و تعداد نسخه‌ای را که این سناریو باید چاپ کند برمی‌گرداند.
func (sc PrintScenario) docs() map[string]int {
	c := sc.Copies
	if c < 1 {
		c = 1
	}
	if c > 5 {
		c = 5
	}
	if sc.Checkout {
		switch sc.Mode {
		case "customer":
			return map[string]int{"customer": c}
		case "both":
			// فیش مشتری + فیش کافه (بایگانی صندوق) = دو نسخه از فیش مشتری
			return map[string]int{"customer": 2}
		case "custom":
			n := c
			if sc.Merchant {
				n += c
			}
			return map[string]int{"customer": n}
		}
		return map[string]int{}
	}
	switch sc.Mode {
	case "kitchen":
		return map[string]int{"kitchen": c}
	case "customer":
		return map[string]int{"customer": c}
	case "both":
		return map[string]int{"customer": c, "kitchen": c}
	case "custom":
		switch sc.Doc {
		case "kitchen":
			return map[string]int{"kitchen": c}
		case "cashier":
			return map[string]int{"customer": c}
		case "all":
			return map[string]int{"customer": c, "kitchen": c}
		}
	}
	return map[string]int{}
}

// autoPrint چاپ خودکار یک لحظه از چرخه سفارش را طبق سیاست کاربر انجام می‌دهد.
// event: create (ثبت روی میز) | notable (ثبت بدون میز) | checkout (تسویه) | complete (تکمیل)
func (a *Application) autoPrint(ctx context.Context, orderID, event string) string {
	if !a.printerEnabled(ctx) {
		return ""
	}
	pol := a.printPolicy(ctx)
	var sc PrintScenario
	switch event {
	case "create":
		sc = pol.Create
	case "notable":
		sc = pol.NoTable
	case "checkout":
		sc = pol.Checkout
	case "complete":
		sc = pol.Complete
	default:
		return ""
	}
	docs := sc.docs()
	if len(docs) == 0 {
		return ""
	}
	_, summary := a.enqueueAndDispatchPrintJobsDocs(ctx, orderID, docs)
	return summary
}

// autoPrintAsync Jobهای چاپ را همان لحظه (سریع، روی دیسک محلی) می‌سازد
// ولی ارسال به چاپگر در پس‌زمینه انجام می‌شود: صندوقدار نه منتظر چاپگر
// می‌ماند نه پیام خطای چاپ می‌بیند (کاربران بدون چاپگر اصلاً چیزی درباره
// چاپ نمی‌بینند). Jobهای ناموفق در صفحه فیش سفارش برای تلاش مجدد می‌مانند.
func (a *Application) autoPrintAsync(orderID, event string) {
	ctx := context.Background()
	if !a.printerEnabled(ctx) {
		return
	}
	pol := a.printPolicy(ctx)
	var sc PrintScenario
	switch event {
	case "create":
		sc = pol.Create
	case "notable":
		sc = pol.NoTable
	case "checkout":
		sc = pol.Checkout
	case "complete":
		sc = pol.Complete
	default:
		return
	}
	docs := sc.docs()
	if len(docs) == 0 {
		return
	}
	jobs, err := a.store.EnqueueOrderPrintJobsDocs(ctx, orderID, docs)
	if err != nil {
		return
	}
	go func() {
		for _, j := range jobs {
			if j.Status == store.PrintStatusPrinted {
				continue
			}
			a.dispatchPrintJob(ctx, j)
		}
	}()
}

func cafeNameFa(cafe store.Entity) string {
	if cafe.ID != "" && cafe.Label() != "" && cafe.Label() != cafe.ID {
		return cafe.Label()
	}
	return "فاکتور فروش"
}

// tableNumberFa شماره میز به‌صورت رقم فارسی، بدون پیشوند «میز».
func tableNumberFa(order store.Entity) string {
	return digitsFa(order.Field("table_label"))
}

// buildReceiptLines متن فیش را از داده واقعی سفارش/کافه می‌سازد؛ ساختار و
// متن‌ها خط‌به‌خط از قالب واحد وب گرفته شده‌اند:
//   - docType=customer → FactorPrint (فاکتور فروش ۸۰mm)
//   - docType=kitchen → KitchenTicket (بلیت آشپزخانه، بدون مبلغ)
//   - docType=test → فیش آزمایشی چاپگر
func buildReceiptLines(order store.Entity, cafe store.Entity, docType string) []receiptLine {
	return buildReceiptLinesFiltered(order, cafe, docType, nil)
}

// buildReceiptLinesFiltered مانند buildReceiptLines است؛ وقتی keepItem داده
// شود فقط اقلامی می‌مانند که محصولشان پذیرفته شود (بلیت دسته‌ای چاپگر).
func buildReceiptLinesFiltered(order store.Entity, cafe store.Entity, docType string, keepItem func(productID string) bool) []receiptLine {
	cafeName := cafeNameFa(cafe)
	if docType == "test" {
		return []receiptLine{
			{Text: cafeName, Bold: true, Center: true},
			{Text: "SALES RECEIPT", Center: true},
			{Solid: true},
			{Text: "فیش آزمایشی چاپگر حرارتی", Center: true},
			{Text: "اتصال چاپگر برقرار است.", Center: true},
			{Text: FormatJalaliFull(time.Now()), Center: true},
			{Solid: true},
			{Text: "از خرید شما سپاسگزاریم", Bold: true, Center: true},
			{Text: "POWERED BY MENUVEX.IR", Center: true},
		}
	}
	items := order.OrderItems()
	if keepItem != nil {
		kept := make([]store.OrderLineView, 0, len(items))
		for _, it := range items {
			if keepItem(it.ProductID) {
				kept = append(kept, it)
			}
		}
		items = kept
	}
	headerText, footerText := cafeReceiptTexts(cafe)

	if docType == "kitchen" {
		// بلیت آشپزخانه وب: عنوان، کافه، باکس میز، شماره و ساعت، آیتم‌ها
		// با تعداد (بدون مبلغ)، یادداشت باکسی و «— پایان سفارش —».
		lines := []receiptLine{{Text: "سفارش آشپزخانه", Bold: true, Center: true}}
		if cafe.ID != "" {
			lines = append(lines, receiptLine{Text: cafeName, Center: true})
		}
		lines = append(lines, receiptLine{Solid: true})
		if tn := tableNumberFa(order); tn != "" {
			lines = append(lines, receiptLine{Text: "میز " + tn, Bold: true, Big: true, Center: true})
		}
		lines = append(lines,
			receiptLine{Text: "شماره " + store.ToPersianDigits(order.Field("order_number")), Bold: true},
			receiptLine{Text: "ساعت: " + jalaliTimeFa(order.Field("created_at"))},
			receiptLine{Text: "تاریخ: " + jalaliDateTimeFa(order.Field("created_at"))},
			receiptLine{Text: "نوع: " + orderTypeLabelFa(order)},
			receiptLine{Feed: true},
		)
		if len(items) == 0 {
			lines = append(lines, receiptLine{Text: "آیتمی ثبت نشده است", Center: true})
		}
		for _, it := range items {
			lines = append(lines, receiptLine{Text: fmt.Sprintf("×%s %s", store.FormatPersianNumber(strconv.Itoa(it.Quantity)), it.ProductName), Bold: true})
			if it.Note != "" {
				lines = append(lines, receiptLine{Text: "+ " + it.Note})
			}
		}
		if n := order.Field("note"); n != "" {
			lines = append(lines, receiptLine{Feed: true}, receiptLine{Text: "یادداشت: " + n, Bold: true})
		}
		lines = append(lines,
			receiptLine{Solid: true},
			receiptLine{Text: "— پایان سفارش —", Center: true},
		)
		return lines
	}

	// فاکتور فروش (FactorPrint): سربرگ، متا، جدول اقلام، جمع‌ها، پابرگ.
	lines := []receiptLine{
		{Text: "SALES RECEIPT", Center: true},
		{Text: cafeName, Bold: true, Center: true},
	}
	if cafe.ID != "" {
		if addr := cafe.Field("address"); addr != "" {
			lines = append(lines, receiptLine{Text: addr, Center: true})
		}
	}
	if headerText != "" {
		for _, part := range strings.Split(headerText, "\n") {
			if strings.TrimSpace(part) != "" {
				lines = append(lines, receiptLine{Text: part, Bold: true, Center: true})
			}
		}
	}
	lines = append(lines, receiptLine{Solid: true})
	lines = append(lines,
		receiptLine{Text: "شماره فیش: " + store.ToPersianDigits(order.Field("order_number"))},
		receiptLine{Text: "تاریخ: " + jalaliDateTimeFa(order.Field("created_at"))},
		receiptLine{Text: "نوع سفارش: " + orderTypeLabelFa(order)},
	)
	if pl := paymentLabelFa(order); pl != "" {
		lines = append(lines, receiptLine{Text: "پرداخت: " + pl})
	}
	if cn := order.Field("customer_name"); cn != "" {
		lines = append(lines, receiptLine{Text: "مشتری: " + cn})
	}
	if cp := order.Field("customer_phone"); cp != "" {
		lines = append(lines, receiptLine{Text: "تلفن مشتری: " + cp})
	}
	if tn := tableNumberFa(order); tn != "" {
		lines = append(lines, receiptLine{Text: "میز: " + tn})
	}
	lines = append(lines, receiptLine{Feed: true})
	if len(items) == 0 {
		lines = append(lines, receiptLine{Text: "آیتمی ثبت نشده است", Center: true})
	}
	totalQty := 0
	for _, it := range items {
		totalQty += it.Quantity
		lines = append(lines, receiptLine{Text: it.ProductName, Bold: true})
		if it.Note != "" {
			lines = append(lines, receiptLine{Text: "+ " + it.Note})
		}
		lines = append(lines, receiptLine{Text: fmt.Sprintf("%s × %s = %s",
			store.FormatPersianNumber(strconv.Itoa(it.Quantity)),
			store.FormatIRRToToman(it.UnitPrice),
			store.FormatIRRToToman(it.LineTotal))})
	}
	lines = append(lines, receiptLine{Feed: true})
	lines = append(lines, receiptLine{Text: "تعداد اقلام: " + store.FormatPersianNumber(strconv.Itoa(totalQty))})
	if sub := order.Field("subtotal_irr"); sub != "" && sub != "0" {
		lines = append(lines, receiptLine{Text: "جمع اقلام: " + store.FormatIRRToToman(sub)})
	}
	if disc := order.Field("discount_irr"); disc != "" && disc != "0" {
		lines = append(lines, receiptLine{Text: "تخفیف: - " + store.FormatIRRToToman(disc)})
	}
	lines = append(lines, receiptLine{Text: "مبلغ قابل پرداخت: " + order.PriceToman(), Bold: true, Big: true})
	if n := order.Field("note"); n != "" {
		lines = append(lines, receiptLine{Feed: true}, receiptLine{Text: "یادداشت: " + n})
	}
	lines = append(lines,
		receiptLine{Solid: true},
		receiptLine{Text: "از خرید شما سپاسگزاریم", Bold: true, Center: true},
	)
	if footerText != "" {
		for _, part := range strings.Split(footerText, "\n") {
			if strings.TrimSpace(part) != "" {
				lines = append(lines, receiptLine{Text: part, Bold: true, Center: true})
			}
		}
	}
	lines = append(lines, receiptLine{Text: "POWERED BY MENUVEX.IR", Center: true})
	return lines
}

// buildESCPos خطوط فیش را به بایت‌های ESC/POS تبدیل می‌کند.
// متن فارسی به‌صورت UTF-8 (ترتیب منطقی) ارسال می‌شود؛ چاپگرهای حرارتی
// رایج بازار ایران با فرم‌ویر عربی/فارسی همین را چاپ می‌کنند. عرض کاغذ
// روی تراز و برش اثر می‌گذارد: پایان هر فیش Feed + برش جزئی (GS V)
// فقط همین‌جا و فقط برای Job واقعی فرستاده می‌شود.
func buildESCPos(lines []receiptLine, paperWidth string) []byte {
	return buildESCPosCut(lines, paperWidth, true)
}

// buildESCPosCut is the same renderer with an explicit cutter setting. Turning
// the cutter off is a real per-printer option, so the GS V command is omitted
// entirely instead of being disabled by device configuration.
func buildESCPosCut(lines []receiptLine, paperWidth string, cut bool) []byte {
	var b strings.Builder
	buf := make([]byte, 0, 4096)
	buf = append(buf, 0x1B, 0x40) // ESC @ — مقداردهی اولیه
	for _, ln := range lines {
		if ln.Feed || ln.Solid {
			sep := "-"
			if ln.Solid {
				sep = "="
			}
			buf = append(buf, []byte(strings.Repeat(sep, paperChars(paperWidth)))...)
			buf = append(buf, '\n')
			continue
		}
		buf = append(buf, 0x1B, 0x61, boolByte(ln.Center)) // ESC a — تراز
		if ln.Bold {
			buf = append(buf, 0x1B, 0x45, 0x01) // ESC E 1 — پررنگ
		}
		if ln.Big {
			buf = append(buf, 0x1D, 0x21, 0x11) // GS ! — اندازه دوبرابر
		}
		buf = append(buf, []byte(ln.Text)...)
		if ln.Big {
			buf = append(buf, 0x1D, 0x21, 0x00)
		}
		if ln.Bold {
			buf = append(buf, 0x1B, 0x45, 0x00)
		}
		buf = append(buf, 0x1B, 0x61, 0x00, '\n')
	}
	buf = append(buf, 0x1B, 0x64, 0x03)       // ESC d 3 — جلو بردن کاغذ
	buf = append(buf, 0x1D, 0x56, 0x42, 0x00) // GS V 66 0 — برش جزئی
	_ = b
	return buf
}

func boolByte(v bool) byte {
	if v {
		return 0x01
	}
	return 0x00
}

func paperChars(paperWidth string) int {
	if paperWidth == "58mm" {
		return 32
	}
	return 48
}

// sendToPrinter بایت‌های آماده را به مقصد واقعی می‌رساند.
// ترتیب تلاش: پوشه Spool (اگر تنظیم شده) ← شبکه TCP ← صف سیستم‌عامل.
func sendToPrinter(job store.PrintJobView, payload []byte) error {
	if dir := strings.TrimSpace(os.Getenv("COFFISH_PRINT_SPOOL_DIR")); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("ساخت پوشه اسپول چاپ ناموفق بود: %w", err)
		}
		name := fmt.Sprintf("print-%s-%s.bin", job.ID, job.DocType)
		if err := os.WriteFile(filepath.Join(dir, name), payload, 0o600); err != nil {
			return fmt.Errorf("نوشتن فایل اسپول چاپ ناموفق بود: %w", err)
		}
		return nil
	}
	if target := parseNetworkTarget(job.Connection); target != "" {
		conn, err := net.DialTimeout("tcp", target, 2*time.Second)
		if err != nil {
			return fmt.Errorf("اتصال شبکه به چاپگر (%s) برقرار نشد: %w", target, err)
		}
		defer conn.Close()
		_ = conn.SetWriteDeadline(time.Now().Add(6 * time.Second))
		if _, err := conn.Write(payload); err != nil {
			return fmt.Errorf("ارسال داده به چاپگر شبکه ناموفق بود: %w", err)
		}
		return nil
	}
	// صف چاپ سیستم‌عامل (CUPS): فقط وقتی صف مشخص است یا lp در دسترس است.
	if runtime.GOOS != "windows" {
		if lpPath, err := exec.LookPath("lp"); err == nil {
			args := []string{"-o", "raw"}
			if q := strings.TrimSpace(job.Queue); q != "" && q != "-" {
				args = append(args, "-d", q)
			}
			cmd := exec.Command(lpPath, args...)
			cmd.Stdin = strings.NewReader(string(payload))
			if out, runErr := cmd.CombinedOutput(); runErr != nil {
				return fmt.Errorf("صف چاپ سیستم‌عامل خطا داد: %s", strings.TrimSpace(string(out)))
			}
			return nil
		}
	}
	if runtime.GOOS == "windows" {
		return fmt.Errorf("برای این چاپگر آدرس شبکه (IP:9100) ثبت نشده و ارسال خام به صف ویندوز در این نسخه پشتیبانی نمی‌شود؛ آدرس شبکه چاپگر را در تنظیمات وارد کنید")
	}
	return fmt.Errorf("چاپگر مقصد مشخص نیست: آدرس شبکه (IP:9100) ثبت نشده و صف چاپ سیستم‌عامل (CUPS) هم در دسترس نیست")
}

// dispatchPrintJob یک Job را واقعاً چاپ می‌کند و وضعیتش را پایدار می‌نویسد.
// خطا برمی‌گرداند تا لایه UI پیام دقیق نشان دهد؛ سفارش هرگز دست نمی‌خورد.
func (a *Application) dispatchPrintJob(ctx context.Context, job store.PrintJobView) store.PrintJobView {
	job, err := a.store.ClaimPrintJob(ctx, job.ID)
	if err != nil {
		job.LastError = err.Error()
		return job
	}
	var order store.Entity
	if job.OrderID != "" {
		order, _ = a.store.Entity(ctx, "order", job.OrderID)
	}
	cafe, _ := a.store.Entity(ctx, "cafe_profile", "cafe-main-profile")
	lines := buildReceiptLines(order, cafe, job.DocType)
	if job.DocType == "kitchen" && len(job.Categories) > 0 && order.ID != "" {
		// بلیت این چاپگر فقط اقلام دسته‌هایی را دارد که به خودش مسیر داده شده‌اند.
		catSet := map[string]bool{}
		for _, c := range job.Categories {
			catSet[c] = true
		}
		prodCat := map[string]string{}
		if prods, pErr := a.store.Entities(ctx, "product"); pErr == nil {
			for _, p := range prods {
				prodCat[p.ID] = p.Field("category_id")
			}
		}
		lines = buildReceiptLinesFiltered(order, cafe, job.DocType, func(productID string) bool {
			return catSet[prodCat[productID]]
		})
	}
	payload := buildESCPosCut(lines, job.PaperWidth, job.Cut)
	copies := job.Copies
	if copies < 1 {
		copies = 1
	}
	if copies > 5 {
		copies = 5
	}
	// New-style printers use the audited transport layer; jobs created before
	// the printer centre (and all rows written by legacy code paths) keep the
	// original sender so upgrading never changes their behaviour.
	if job.PrinterKind != "" {
		profile := a.printerProfileForJob(ctx, job)
		sender := a.printSender()
		for i := 0; i < copies; i++ {
			sendErr := sender.Send(ctx, profile, payload)
			if sendErr != nil {
				uncertain := printer.IsUncertain(sendErr) || i > 0
				updated, _ := a.store.FailPrintJob(ctx, job.ID, sendErr.Error(), uncertain)
				return updated
			}
		}
		updated, _ := a.store.UpdatePrintJobStatus(ctx, job.ID, store.PrintStatusSent, "")
		return updated
	}
	for i := 0; i < copies; i++ {
		if sendErr := sendToPrinter(job, payload); sendErr != nil {
			updated, _ := a.store.UpdatePrintJobStatus(ctx, job.ID, store.PrintStatusFailed, sendErr.Error())
			return updated
		}
	}
	updated, _ := a.store.UpdatePrintJobStatus(ctx, job.ID, store.PrintStatusPrinted, "")
	return updated
}

// printerProfileForJob resolves the live printer configuration, falling back to
// the job's own destination snapshot when the printer was deleted or renamed.
func (a *Application) printerProfileForJob(ctx context.Context, job store.PrintJobView) printer.Profile {
	if job.PrinterID != "" {
		if p, err := a.store.Printer(ctx, job.PrinterID); err == nil {
			if p.Kind != "" {
				return p
			}
		}
	}
	profile := printer.Profile{
		ID: job.PrinterID, Name: job.PrinterTitle, Kind: job.PrinterKind, Cut: job.Cut,
	}
	if strings.HasSuffix(job.PaperWidth, "58mm") {
		profile.PaperMM = printer.Paper58
	} else {
		profile.PaperMM = printer.Paper80
	}
	target := job.Target
	switch job.PrinterKind {
	case printer.KindNetwork:
		raw := strings.TrimPrefix(target, "network:")
		if h, p, ok := strings.Cut(raw, ":"); ok {
			profile.Host = h
			if n, err := strconv.Atoi(p); err == nil {
				profile.Port = n
			}
		}
	case printer.KindSpooler:
		profile.QueueName = strings.TrimPrefix(target, "queue:")
	case printer.KindUSB:
		profile.USB.DevicePath = strings.TrimPrefix(target, "usb:")
	}
	return profile.Normalize()
}

// enqueueAndDispatchPrintJobs چاپ کامل پیش‌فرض (مشتری + آشپزخانه) برای
// چاپ دستی؛ چاپ خودکار از سیاست کاربر و enqueueAndDispatchPrintJobsDocs می‌آید.
func (a *Application) enqueueAndDispatchPrintJobs(ctx context.Context, orderID string) (jobs []store.PrintJobView, summary string) {
	return a.enqueueAndDispatchPrintJobsDocs(ctx, orderID, map[string]int{"customer": 1, "kitchen": 1})
}

// enqueueAndDispatchPrintJobsDocs Jobهای سندهای خواسته‌شده را می‌سازد
// (بدون تکرار) و همان لحظه چاپ می‌کند؛ خلاصه نتیجه برمی‌گردد.
func (a *Application) enqueueAndDispatchPrintJobsDocs(ctx context.Context, orderID string, docs map[string]int) (jobs []store.PrintJobView, summary string) {
	jobs, err := a.store.EnqueueOrderPrintJobsDocs(ctx, orderID, docs)
	if err != nil {
		return nil, "خطای ساخت Job چاپ: " + err.Error()
	}
	var printed, sent, failed int
	for _, j := range jobs {
		if j.Status == store.PrintStatusPrinted || j.Status == store.PrintStatusSent {
			if j.Status == store.PrintStatusSent {
				sent++
			} else {
				printed++
			}
			continue
		}
		if j.Status == store.PrintStatusFailed && j.Attempts > 0 && j.PrinterID == "" {
			failed++
			continue
		}
		done := a.dispatchPrintJob(ctx, j)
		for i := range jobs {
			if jobs[i].ID == done.ID {
				jobs[i] = done
			}
		}
		switch done.Status {
		case store.PrintStatusPrinted:
			printed++
		case store.PrintStatusSent:
			sent++
		default:
			failed++
		}
	}
	delivered := printed + sent
	switch {
	case failed == 0 && sent > 0 && printed == 0:
		summary = "فیش‌ها برای چاپ به چاپگر ارسال شدند (" + strconv.Itoa(sent) + " فیش)؛ دستگاه‌های RAW تأیید فیزیکی برنمی‌گردانند."
	case failed == 0 && delivered > 0:
		summary = "فیش‌ها به‌صورت خودکار چاپ شدند (" + strconv.Itoa(delivered) + " فیش)."
	case delivered > 0:
		summary = "بخشی از فیش‌ها ارسال شد و " + strconv.Itoa(failed) + " فیش خطا دارد؛ از «کل سفارشات» تلاش مجدد بزنید."
	case failed > 0:
		summary = "سفارش ثبت شد اما چاپ خودکار ناموفق بود (" + strconv.Itoa(failed) + " Job ناموفق)؛ سفارش سالم است و از «کل سفارشات» می‌توانید دوباره چاپ کنید."
	default:
		summary = "Job چاپی برای این سفارش ساخته نشد."
	}
	return jobs, summary
}
