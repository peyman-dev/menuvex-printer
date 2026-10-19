package desktop

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

// TestClubCustomerFlow ثبت/ویرایش مشتری باشگاه با اعتبارسنجی‌های وب
// (نام ≥۲، موبایل ۱۱ رقم ۰۹، موبایل یکتا، موبایلِ غیرقابل‌ویرایش) و سوییچ نسیه.
func TestClubCustomerFlow(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()

	ordersPost(t, a, "/local/club-customer", url.Values{
		"full_name": {"علی رضایی"}, "phone": {"09121234567"},
		"enable_club": {"on"}, "initial_points": {"100"}, "credit_enabled": {"on"},
		"subscription_code": {"SUB-1042"},
	})
	customers, _ := a.store.Entities(ctx, "customer")
	if len(customers) != 1 {
		t.Fatalf("customer not created: %d", len(customers))
	}
	c := customers[0]
	if c.Field("club_member") != "true" || c.Field("loyalty_points") != "100" || c.Field("credit_enabled") != "true" || c.Field("subscription_code") != "SUB-1042" {
		t.Fatalf("club fields wrong: member=%s points=%s credit=%s code=%s",
			c.Field("club_member"), c.Field("loyalty_points"), c.Field("credit_enabled"), c.Field("subscription_code"))
	}
	// موبایل نامعتبر رد می‌شود.
	ordersPost(t, a, "/local/club-customer", url.Values{"full_name": {"مشتری بد"}, "phone": {"123"}})
	if cs, _ := a.store.Entities(ctx, "customer"); len(cs) != 1 {
		t.Fatal("invalid mobile must be rejected")
	}
	// موبایل تکراری رد می‌شود.
	ordersPost(t, a, "/local/club-customer", url.Values{"full_name": {"مشتری دوم"}, "phone": {"09121234567"}})
	if cs, _ := a.store.Entities(ctx, "customer"); len(cs) != 1 {
		t.Fatal("duplicate mobile must be rejected")
	}
	// نام کوتاه رد می‌شود.
	ordersPost(t, a, "/local/club-customer", url.Values{"full_name": {"ع"}, "phone": {"09129999999"}})
	if cs, _ := a.store.Entities(ctx, "customer"); len(cs) != 1 {
		t.Fatal("short name must be rejected")
	}
	// ویرایش: نام و کد عوض می‌شود، موبایل دست نمی‌خورد (چک‌باکس نسیه روشن می‌ماند).
	ordersPost(t, a, "/local/club-customer", url.Values{
		"customer_id": {c.ID}, "full_name": {"علی رضایی‌فر"}, "subscription_code": {"SUB-2000"}, "credit_enabled": {"on"},
	})
	c, _ = a.store.Entity(ctx, "customer", c.ID)
	if c.Label() != "علی رضایی‌فر" || c.Field("subscription_code") != "SUB-2000" || c.Field("phone") != "09121234567" {
		t.Fatalf("edit wrong: name=%s code=%s phone=%s", c.Label(), c.Field("subscription_code"), c.Field("phone"))
	}
	// سوییچ اعتبار خرید برعکس می‌شود (الان فعال است → غیرفعال).
	ordersPost(t, a, "/local/club-credit-toggle", url.Values{"customer_id": {c.ID}})
	c, _ = a.store.Entity(ctx, "customer", c.ID)
	if c.Field("credit_enabled") != "false" {
		t.Fatal("credit toggle did not turn off")
	}
}

// TestClubPaymentFlow پرداخت اشتراک: ساخت در انتظار، تایید بدون کد پیگیری رد،
// تایید با کد پیگیری = پرداخت‌شده + فعال‌شدن عضویت مشتری.
func TestClubPaymentFlow(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	cid, err := a.store.SaveLocalRecord(ctx, "customer", "", map[string]any{"full_name": "سارا محمدی", "phone": "09123334455"})
	if err != nil {
		t.Fatal(err)
	}
	ordersPost(t, a, "/local/club-payment", url.Values{
		"customer_id": {cid}, "plan_title": {"اشتراک ماهانه قهوه"}, "amount_toman": {"500000"}, "payment_method": {"online"},
	})
	payments, _ := a.store.Entities(ctx, "membership_payment")
	if len(payments) != 1 || payments[0].Field("status") != "pending" || payments[0].Field("amount_irr") != "5000000" {
		t.Fatalf("payment not created pending: %+v", payments)
	}
	p := payments[0]
	ordersPost(t, a, "/local/club-payment-confirm", url.Values{"payment_id": {p.ID}})
	p, _ = a.store.Entity(ctx, "membership_payment", p.ID)
	if p.Field("status") != "pending" {
		t.Fatal("confirm without reference must be rejected")
	}
	ordersPost(t, a, "/local/club-payment-confirm", url.Values{"payment_id": {p.ID}, "reference_number": {"TRX-1042"}})
	p, _ = a.store.Entity(ctx, "membership_payment", p.ID)
	if p.Field("status") != "paid" || p.Field("reference_number") != "TRX-1042" {
		t.Fatalf("confirm failed: status=%s ref=%s", p.Field("status"), p.Field("reference_number"))
	}
	c, _ := a.store.Entity(ctx, "customer", cid)
	if c.Field("club_member") != "true" {
		t.Fatal("confirmed payment must activate club membership")
	}
}

// TestClubGroupFlow چرخه گروه: ساخت، افزودن عضو با موبایل، نسیه، جزئیات، حذف.
func TestClubGroupFlow(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	ordersPost(t, a, "/local/club-group", url.Values{
		"name": {"شرکت آکمه"}, "discount_type": {"percentage"}, "discount_value": {"15"},
		"contract_type": {"corporate"}, "contact_name": {"مدیر اداری"},
	})
	groups, _ := a.store.Entities(ctx, "customer_group")
	if len(groups) != 1 {
		t.Fatalf("group not created: %d", len(groups))
	}
	g := groups[0]
	if g.Field("is_active") != "true" || g.Field("discount_value") != "15" {
		t.Fatalf("group fields wrong: active=%s discount=%s", g.Field("is_active"), g.Field("discount_value"))
	}
	// افزودن عضو با موبایل جدید → مشتری ساخته و عضو می‌شود.
	ordersPost(t, a, "/local/club-group-member", url.Values{"group_id": {g.ID}, "mobile": {"09125556677"}, "full_name": {"کارمند یک"}})
	customers, _ := a.store.Entities(ctx, "customer")
	if len(customers) != 1 || customers[0].Field("group_id") != g.ID {
		t.Fatal("member was not added to group")
	}
	// موبایل نامعتبر رد می‌شود.
	ordersPost(t, a, "/local/club-group-member", url.Values{"group_id": {g.ID}, "mobile": {"123"}})
	if cs, _ := a.store.Entities(ctx, "customer"); len(cs) != 1 {
		t.Fatal("invalid member mobile must be rejected")
	}
	// صفحه جزئیات گروه عضو را نشان می‌دهد.
	detail := ordersGet(t, a, "/?page=club-group&id="+g.ID)
	if !strings.Contains(detail, "کارمند یک") || !strings.Contains(detail, "اعضای گروه") {
		t.Fatal("group detail must list members")
	}
	// فعال‌سازی نسیه گروه و توقف قرارداد.
	ordersPost(t, a, "/local/club-group-credit", url.Values{"group_id": {g.ID}})
	g, _ = a.store.Entity(ctx, "customer_group", g.ID)
	if g.Field("credit_enabled") != "true" {
		t.Fatal("group credit toggle failed")
	}
	ordersPost(t, a, "/local/club-group-toggle", url.Values{"group_id": {g.ID}})
	g, _ = a.store.Entity(ctx, "customer_group", g.ID)
	if g.Field("is_active") != "false" {
		t.Fatal("group toggle failed")
	}
	// جدا کردن عضو و حذف گروه.
	ordersPost(t, a, "/local/club-group-member-remove", url.Values{"group_id": {g.ID}, "customer_id": {customers[0].ID}})
	cust, _ := a.store.Entity(ctx, "customer", customers[0].ID)
	if cust.Field("group_id") != "" {
		t.Fatal("member remove failed")
	}
	ordersPost(t, a, "/local/club-group-delete", url.Values{"group_id": {g.ID}})
	if gs, _ := a.store.Entities(ctx, "customer_group"); len(gs) != 0 {
		t.Fatal("group delete failed")
	}
}

// TestClubPagesRender ساختار وب باشگاه رندر می‌شود.
func TestClubPagesRender(t *testing.T) {
	a := testApp(t)
	ordersPost(t, a, "/local/club-customer", url.Values{
		"full_name": {"مشتری نمونه"}, "phone": {"09121112233"}, "enable_club": {"on"},
	})
	html := ordersGet(t, a, "/?page=customers")
	for _, s := range []string{"اشتراک آنلاین", "جستجوی نام، موبایل یا شماره اشتراک", "مشتری جدید", "اعتبار خرید", "آخرین حضور", "مجموع خرید", "مشتری جدید باشگاه", "مجوز خرید اعتباری (نسیه) در صندوق", "عضویت در باشگاه", "گروه‌های باشگاه مشتریان"} {
		if !strings.Contains(html, s) {
			t.Fatalf("club customers page missing %q", s)
		}
	}
	membership := ordersGet(t, a, "/?page=customers&tab=membership")
	for _, s := range []string{"اعضای اشتراک", "پرداخت در انتظار", "پرداخت تاییدشده", "خریداران اشتراک", "تایید پرداخت عضویت", "ثبت پرداخت اشتراک"} {
		if !strings.Contains(membership, s) {
			t.Fatalf("club membership page missing %q", s)
		}
	}
	groups := ordersGet(t, a, "/?page=club-groups")
	for _, s := range []string{"گروه‌های ثبت‌شده", "قراردادهای معتبر", "فقط قراردادهای معتبر", "جستجو نام گروه یا رابط", "هنوز گروهی ساخته نشده"} {
		if !strings.Contains(groups, s) {
			t.Fatalf("club groups page missing %q", s)
		}
	}
	form := ordersGet(t, a, "/?page=club-group-form")
	for _, s := range []string{"نام گروه", "کترینگ", "شرکتی", "سقف هر سفارش (اختیاری · تومان)", "روز صورتحساب در ماه", "خرید اعتباری (نسیه) گروه", "شروع اعتبار"} {
		if !strings.Contains(form, s) {
			t.Fatalf("club group form missing %q", s)
		}
	}
}

// TestSidebarActiveSync اسکریپت همگام‌سازی لینک فعال سایدبار با URL فعلی.
func TestSidebarActiveSync(t *testing.T) {
	a := testApp(t)
	html := ordersGet(t, a, "/?page=orders")
	for _, s := range []string{"syncNavActive", "htmx:pushedIntoHistory", "'receipt': 'receipts'", "'club-groups': 'customers'"} {
		if !strings.Contains(html, s) {
			t.Fatalf("sidebar active sync missing %q", s)
		}
	}
}
