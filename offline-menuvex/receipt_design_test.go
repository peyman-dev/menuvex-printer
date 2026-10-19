package desktop

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sinaqaffari/coffish/desktop/internal/store"
)

func linesText(lines []receiptLine) string {
	var parts []string
	for _, ln := range lines {
		if ln.Feed {
			parts = append(parts, "---")
		} else if ln.Solid {
			parts = append(parts, "===")
		} else {
			parts = append(parts, ln.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// TestReceiptDesignMatchesWeb قالب فیش مشتری و بلیت آشپزخانه را با
// FactorPrint و KitchenTicket وب قفل می‌کند (متن، ترتیب و نبود مبلغ در بلیت).
func TestReceiptDesignMatchesWeb(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	prodID, err := a.store.SaveLocalRecord(ctx, "product", "", map[string]any{"title": "لاته فیش", "price": "1500000"})
	if err != nil {
		t.Fatal(err)
	}
	orderID, err := a.store.CreatePOSOrder(ctx, store.POSOrderInput{
		ClientToken:   "receipt-design-1",
		PaymentMethod: "card_pos",
		CustomerName:  "مشتری فیش",
		Items:         []store.POSOrderLineInput{{ProductID: prodID, Quantity: 2, Note: "بدون شکر"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	order, err := a.store.Entity(ctx, "order", orderID)
	if err != nil {
		t.Fatal(err)
	}
	cafe, _ := a.store.Entity(ctx, "cafe_profile", "cafe-main-profile")

	cashier := linesText(buildReceiptLines(order, cafe, "customer"))
	for _, want := range []string{
		"SALES RECEIPT", "فاکتور فروش", "شماره فیش:", "تاریخ:",
		"نوع سفارش: حضوری", "پرداخت: نقدی / کارتخوان",
		"مشتری: مشتری فیش", "لاته فیش", "+ بدون شکر", "تعداد اقلام: ۲",
		"جمع اقلام:", "مبلغ قابل پرداخت:", "از خرید شما سپاسگزاریم", "POWERED BY MENUVEX.IR",
	} {
		if !strings.Contains(cashier, want) {
			t.Fatalf("cashier receipt missing web piece %q\n%s", want, cashier)
		}
	}
	for _, banned := range []string{"معادل", "دنگ", "حواله", "از حضور", "وضعیت:"} {
		if strings.Contains(cashier, banned) {
			t.Fatalf("cashier receipt must not contain invented piece %q", banned)
		}
	}

	kitchen := linesText(buildReceiptLines(order, cafe, "kitchen"))
	for _, want := range []string{"سفارش آشپزخانه", "شماره ", "نوع: حضوری", "×۲ لاته فیش", "+ بدون شکر", "— پایان سفارش —"} {
		if !strings.Contains(kitchen, want) {
			t.Fatalf("kitchen ticket missing web piece %q\n%s", want, kitchen)
		}
	}
	// بلیت آشپزخانه وب هیچ مبلغی ندارد.
	for _, banned := range []string{"تومان", "مبلغ قابل پرداخت", "جمع اقلام", "تخفیف"} {
		if strings.Contains(kitchen, banned) {
			t.Fatalf("kitchen ticket must not contain money piece %q", banned)
		}
	}

	// متن سربرگ/پابرگ تنظیمات چاپگر باید در فیش واقعی بیاید.
	if _, err := a.store.SaveLocalRecord(ctx, "cafe_profile", "cafe-main-profile", map[string]any{
		"title": "کافه تست فیش", "address": "نشانی تست",
		"receipt_header_text": "متن سربرگ تست", "receipt_footer_text": "متن پابرگ تست",
	}); err != nil {
		t.Fatal(err)
	}
	cafe2, _ := a.store.Entity(ctx, "cafe_profile", "cafe-main-profile")
	custom := linesText(buildReceiptLines(order, cafe2, "customer"))
	for _, want := range []string{"کافه تست فیش", "نشانی تست", "متن سربرگ تست", "متن پابرگ تست"} {
		if !strings.Contains(custom, want) {
			t.Fatalf("customized receipt missing %q", want)
		}
	}

	// بایت‌های ESC/POS: مقداردهی اولیه، اندازه دوبرابر و برش پایانی.
	esc := buildESCPos(buildReceiptLines(order, cafe2, "customer"), "80mm")
	if len(esc) < 4 || esc[0] != 0x1B || esc[1] != 0x40 {
		t.Fatal("ESC/POS payload must start with ESC @")
	}
	if !strings.Contains(string(esc), "POWERED BY MENUVEX.IR") {
		t.Fatal("ESC/POS payload missing footer")
	}
	foundCut, foundBig := false, false
	for i := 0; i+2 < len(esc); i++ {
		if esc[i] == 0x1D && esc[i+1] == 0x56 {
			foundCut = true
		}
		if esc[i] == 0x1D && esc[i+1] == 0x21 && esc[i+2] == 0x11 {
			foundBig = true
		}
	}
	if !foundCut || !foundBig {
		t.Fatalf("ESC/POS missing cut=%v big=%v", foundCut, foundBig)
	}
}

// TestReceiptPagePreviewMatchesWeb پیش‌نمایش صفحه فیش را با FactorPrint وب قفل می‌کند.
func TestReceiptPagePreviewMatchesWeb(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	prodID, err := a.store.SaveLocalRecord(ctx, "product", "", map[string]any{"title": "موکا پیش‌نمایش", "price": "1200000"})
	if err != nil {
		t.Fatal(err)
	}
	orderID, err := a.store.CreatePOSOrder(ctx, store.POSOrderInput{
		ClientToken: "receipt-preview-1",
		Items:       []store.POSOrderLineInput{{ProductID: prodID, Quantity: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "http://localhost/?page=receipt&id="+orderID, nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("receipt page -> %d", w.Code)
	}
	html := w.Body.String()
	for _, want := range []string{
		`class="factor-print"`, "SALES RECEIPT", "فاکتور فروش",
		"شماره فیش", "نوع سفارش", "شرح", "تعداد", "قیمت واحد", "جمع",
		"موکا پیش‌نمایش", "تعداد اقلام", "مبلغ قابل پرداخت",
		"از خرید شما سپاسگزاریم", "POWERED BY MENUVEX.IR",
		"سفارش آشپزخانه", "— پایان سفارش —",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("receipt preview missing web piece %q", want)
		}
	}
	for _, banned := range []string{"دنگ", "معادل ریالی", "حواله آماده‌سازی", "شرح کالا", "کافه تخصصی کافیش", "thermal-receipt"} {
		if strings.Contains(html, banned) {
			t.Fatalf("receipt preview must not contain legacy/invented piece %q", banned)
		}
	}

	// تنظیمات: کارت متن فیش وب و حذف مقادیر جعلی پیش‌فرض.
	// (کارت متن فیش فقط وقتی نمایش داده می‌شود که کاربر چاپگر را فعال کرده باشد.)
	if _, err := a.store.SaveLocalRecord(context.Background(), "printer", "", map[string]any{"title": "چاپگر تست", "connection": "192.0.2.20:9100", "paper_width": "80mm"}); err != nil {
		t.Fatal(err)
	}
	r2 := httptest.NewRequest("GET", "http://localhost/?page=print-policy", nil)
	w2 := httptest.NewRecorder()
	a.Handler().ServeHTTP(w2, r2)
	settings := w2.Body.String()
	for _, want := range []string{"سربرگ و پابرگ فیش صندوق", "متن سربرگ فیش", "متن پابرگ فیش", "ذخیرهٔ متن فیش", "فیش صندوق و پیش‌نمایش از همین متن استفاده می‌کنند"} {
		if !strings.Contains(settings, want) {
			t.Fatalf("settings missing receipt-text piece %q", want)
		}
	}
	for _, banned := range []string{"کافه تخصصی کافیش — شعبه ولیعصر تهران", "۰۲۱-۸۸۹۹۴۴۱۱", "پلاک ۱۴۲"} {
		if strings.Contains(settings, banned) {
			t.Fatalf("settings must not contain fake default %q", banned)
		}
	}
}
