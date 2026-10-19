package desktop

import (
	"context"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestStaffPagesMatchWeb صفحه کارکنان وب (کارت‌ها، آمار، پروفایل با دسترسی‌ها) را قفل می‌کند.
func TestStaffPagesMatchWeb(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	id, err := a.store.SaveLocalRecord(ctx, "staff", "", map[string]any{
		"full_name": "نگار تست", "phone": "09911234567", "role_key": "manager",
		"salary_irr": "900000000", "salary_period": "monthly", "permissions": "pos.view,orders.view",
	})
	if err != nil {
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
	list := get("/?page=staff")
	for _, want := range []string{"مدیریت کارکنان", "کل کارکنان", "شیفت‌های هفتگی", "پروفایل پرسنل", "نوع همکاری", "حقوق پرسنل", "نگار تست", "افزودن کارمند", "/?page=staff-checklist"} {
		if !strings.Contains(list, want) {
			t.Fatalf("staff list missing %q", want)
		}
	}
	for _, banned := range []string{`id="dlg-checklist-new"`, `<select name="priority"`, `id="staff-tasks"`} {
		if strings.Contains(list, banned) {
			t.Fatalf("staff list must not embed the checklist creator %q", banned)
		}
	}
	form := get("/?page=staff-form&id=" + id)
	for _, want := range []string{"اطلاعات ورود و دسترسی", "دسترسی‌های این کارمند", "حقوق و بیمه", "شیفت‌های کاری", "مرخصی و تعطیلی", "ذخیره تغییرات"} {
		if !strings.Contains(form, want) {
			t.Fatalf("staff form missing %q", want)
		}
	}
	if !strings.Contains(form, `value="pos.view" checked`) {
		t.Fatal("saved permissions must render as checked in the profile form")
	}
	// کلیدهای دسترسی باید عین CAFE_PERMISSION_GROUPS وب باشند، نه کلیدهای قدیمی آفلاین.
	for _, want := range []string{`value="inventory.recipes.view"`, `value="inventory.waste.view"`, `value="staff.view"`, `value="customers.retention.view"`, `value="customers.online.view"`, `value="menu.qr.view"`, `value="jobs.manage"`, `value="settings.subscription.view"`, `value="updates.view"`, "بیمه فعال است", `id="perm-manager-note"`} {
		if !strings.Contains(form, want) {
			t.Fatalf("staff form missing web permission piece %q", want)
		}
	}
	for _, banned := range []string{`value="recipes.view"`, `value="waste.view"`, `value="staff.manage"`, `value="settings.manage"`} {
		if strings.Contains(form, banned) {
			t.Fatalf("staff form must not use legacy permission key %q", banned)
		}
	}
	// کلیدهای قدیمی ذخیره‌شده باید روی کلیدهای وب نمایش داده شوند (بدون از دست رفتن داده).
	legacyID, err := a.store.SaveLocalRecord(ctx, "staff", "", map[string]any{
		"full_name": "قدیمی تست", "phone": "09911234568", "role_key": "staff",
		"permissions": "recipes.view,staff.manage",
	})
	if err != nil {
		t.Fatal(err)
	}
	legacyForm := get("/?page=staff-form&id=" + legacyID)
	if !strings.Contains(legacyForm, `value="inventory.recipes.view" checked`) || !strings.Contains(legacyForm, `value="staff.view" checked`) {
		t.Fatal("legacy permission keys must map onto web keys in the form")
	}
	// فرم ساخت: پیش‌فرض‌های نقش کارمند وب (بدون داشبورد/مالی) باید از پیش انتخاب شده باشند.
	createForm := get("/?page=staff-form")
	if !strings.Contains(createForm, `value="pos.view" checked`) || !strings.Contains(createForm, `value="printers.manage" checked`) {
		t.Fatal("create form must preselect web staff-role default permissions")
	}
	if strings.Contains(createForm, `value="dashboard.view" checked`) {
		t.Fatal("staff role defaults must not include dashboard access")
	}
	// باگ سراسری چک‌باکس: استایل اینپوت متنی نباید روی چک‌باکس اعمال شود.
	css := get("/static/app.css")
	for _, want := range []string{`input:not([type="checkbox"])`, `input[type="checkbox"]`, "width: 16px; height: 16px"} {
		if !strings.Contains(css, want) {
			t.Fatalf("app.css missing checkbox rule %q", want)
		}
	}
	// رمز عبور نباید به‌صورت متن ساده ذخیره شود.
	formVals := url.Values{
		"csrf": {a.csrf}, "kind": {"staff"}, "id": {id}, "return_page": {"staff"},
		"full_name": {"نگار تست"}, "phone": {"09911234567"}, "password": {"secret-pass-123"},
		"perm": {"pos.view", "inventory.view"},
	}
	r := httptest.NewRequest("POST", "http://localhost/local/save", strings.NewReader(formVals.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("staff save -> %d", w.Code)
	}
	saved, err := a.store.Entity(ctx, "staff", id)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved.Payload), "secret-pass-123") {
		t.Fatal("plaintext password must never be stored")
	}
	if saved.Field("password_hash") == "" {
		t.Fatal("password must be stored as hash")
	}
	if saved.Field("permissions") != "pos.view,inventory.view" {
		t.Fatalf("permissions = %q", saved.Field("permissions"))
	}
}

// TestStaffChecklistIsPersonnelSubpageWithDialog چک‌لیست را به‌عنوان زیرصفحه
// مجموعه پرسنل قفل می‌کند: ساخت وظیفه فقط در دیالوگ است و شیفت‌ها از پروفایل خوانده می‌شوند.
func TestStaffChecklistIsPersonnelSubpageWithDialog(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	if _, err := a.store.SaveLocalRecord(ctx, "staff", "", map[string]any{
		"full_name": "شیفت تست", "phone": "09911234569", "role_key": "staff", "shift": "شیفت صبح تست",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.SaveLocalRecord(ctx, "task", "", map[string]any{
		"title": "وظیفه تستی چک‌لیست", "assignee": "شیفت تست", "priority": "بالا", "is_done": "false", "status": "pending",
	}); err != nil {
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
	page := get("/?page=staff-checklist")
	for _, want := range []string{
		"چک‌لیست اجرایی روزانه و شیفت‌ها", "وظیفه‌های امروز", "وظیفه تستی چک‌لیست", "شیفت صبح تست",
		`id="dlg-checklist-new"`, `id="checklist-priority"`, `id="checklist-assignee"`,
		`name="return_page" value="staff-checklist"`, "وظیفه تازه",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("staff checklist missing %q", want)
		}
	}
	css := get("/static/app.css")
	for _, want := range []string{".ui-select-trigger", ".ui-select-menu", ".ui-select-option.is-selected", ".check-layout"} {
		if !strings.Contains(css, want) {
			t.Fatalf("app.css missing product select/checklist rule %q", want)
		}
	}

	form := url.Values{
		"csrf": {a.csrf}, "kind": {"task"}, "return_page": {"staff-checklist"},
		"title": {"وظیفه ساخته‌شده از دیالوگ"}, "assignee": {"شیفت تست"}, "priority": {"متوسط"},
		"is_done": {"false"}, "status": {"pending"},
	}
	r := httptest.NewRequest("POST", "http://localhost/local/save", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "وظیفه ساخته‌شده از دیالوگ") {
		t.Fatalf("checklist create -> %d %s", w.Code, w.Body.String())
	}
	tasks, err := a.store.Entities(ctx, "task")
	if err != nil {
		t.Fatal(err)
	}
	var createdID string
	for _, task := range tasks {
		if task.Label() == "وظیفه ساخته‌شده از دیالوگ" {
			createdID = task.ID
		}
	}
	if createdID == "" {
		t.Fatal("created checklist task not stored")
	}
	toggle := url.Values{"csrf": {a.csrf}, "task_id": {createdID}, "return_page": {"staff-checklist"}}
	r = httptest.NewRequest("POST", "http://localhost/local/task-toggle", strings.NewReader(toggle.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("HX-Request", "true")
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "انجام شد") {
		t.Fatalf("checklist toggle -> %d %s", w.Code, w.Body.String())
	}
}

// TestInventoryCategoriesCrud ساخت/حذف دسته‌بندی مواد اولیه و اتصال آن به فرم قلم را قفل می‌کند.
func TestInventoryCategoriesCrud(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	catID, err := a.store.SaveLocalRecord(ctx, "ingredient_category", "", map[string]any{"title": "دسته‌تست‌انبار", "description": "دانه‌های قهوه"})
	if err != nil {
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
	page := get("/?page=inv-categories")
	if !strings.Contains(page, "دسته‌بندی مواد اولیه") || !strings.Contains(page, "دسته‌تست‌انبار") {
		t.Fatal("inventory categories page must list created categories")
	}
	form := get("/?page=ingredient-form")
	if !strings.Contains(form, "دسته‌تست‌انبار") {
		t.Fatal("ingredient form must offer inventory categories in its select")
	}
	purchases := get("/?page=inv-purchases")
	if !strings.Contains(purchases, "فاکتورهای خرید انبار") {
		t.Fatal("inventory purchases page missing")
	}
	if err := a.store.DeleteLocalRecord(ctx, "ingredient_category", catID); err != nil {
		t.Fatal(err)
	}
	if page := get("/?page=inv-categories"); strings.Contains(page, "دسته‌تست‌انبار") {
		t.Fatal("deleted category must disappear")
	}
	// داشبورد: چارت باید تول‌تیپ تیره وب (ChartTooltipCard) داشته باشد:
	// زون هاور هر نقطه با داده درآمد/هزینه + کرسر و نقطه فعال.
	if dash := get("/"); !strings.Contains(dash, "chart-hover-zone") || !strings.Contains(dash, "data-income=") || !strings.Contains(dash, "data-expense=") || !strings.Contains(dash, "chart-cursor") {
		t.Fatal("dashboard chart must carry web tooltip hover zones, cursor and active dots")
	}
}
