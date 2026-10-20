package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"
)

type POSOrderLineInput struct {
	ProductID string `json:"product_id"`
	Quantity  int    `json:"quantity"`
	Note      string `json:"note,omitempty"`
}

type POSOrderInput struct {
	// ClientToken توکن یکتای فرم صندوق است؛ ارسال دوباره همان فرم
	// (دابل‌کلیک/Retry شبکه محلی) سفارش دوم نمی‌سازد.
	ClientToken     string `json:"client_token,omitempty"`
	FulfillmentType string `json:"fulfillment_type"` // dine_in | outside | delivery
	TableID         string `json:"table_id,omitempty"`
	CustomerID      string `json:"customer_id,omitempty"`
	CustomerName    string `json:"customer_name,omitempty"`
	CustomerPhone   string `json:"customer_phone,omitempty"`
	// CashierName نام ثبت‌کننده سفارش است (در لیست سفارش‌های وب به‌صورت
	// «ثبت: {cashier_name}» نمایش داده می‌شود).
	CashierName   string `json:"cashier_name,omitempty"`
	PaymentMethod string `json:"payment_method"` // cash | card_pos | credit_debtor | mixed
	DiscountIRR   string `json:"discount_irr,omitempty"`
	// ExtraIRR مبلغ اضافی صندوق (مثلاً سیروپ اضافه) به ریال؛ ExtraNote شرح آن.
	ExtraIRR  string `json:"extra_irr,omitempty"`
	ExtraNote string `json:"extra_note,omitempty"`
	// DebtorDueAt/DebtorNote سررسید و یادداشت پرونده بدهی (وقتی پرداخت اعتباری است).
	DebtorDueAt string `json:"debtor_due_at,omitempty"`
	DebtorNote  string `json:"debtor_note,omitempty"`
	// DebtorKind نوع حساب بدهکار است: personal | company (گروه).
	// DebtorGroupID شناسه گروه/شرکت باشگاه وقتی نوع company است.
	DebtorKind    string              `json:"debtor_kind,omitempty"`
	DebtorGroupID string              `json:"debtor_group_id,omitempty"`
	Note          string              `json:"note,omitempty"`
	Items         []POSOrderLineInput `json:"items"`
}

type RecipeLineView struct {
	IngredientID   string `json:"ingredient_id"`
	IngredientName string `json:"ingredient_name"`
	Quantity       string `json:"quantity"`
	Unit           string `json:"unit"`
}

// ProductPortion می‌گوید با موجودی فعلی انبار چند واحد از یک محصول قابل تهیه
// است (بر پایهٔ دستور تهیه/رسپی). Portions = -1 یعنی محصول رسپی ندارد و
// محدودیت مواد اولیه ندارد؛ Portions = 0 یعنی همین حالا «ناموجود» است.
type ProductPortion struct {
	Portions int    `json:"portions"`
	Missing  string `json:"missing,omitempty"`
	// Shortages برای دیالوگ «چه چیزی کم است؟» است: هر ماده اولیه‌ای که
	// موجودی‌اش از نیاز یک واحد کمتر است، با مقدار نیاز/موجودی/کمبود.
	Shortages []PortionShortage `json:"shortages,omitempty"`
}

// PortionShortage یک ماده اولیهٔ کم‌آورده در رسپی یک محصول است.
type PortionShortage struct {
	IngredientID string `json:"id"`
	Name         string `json:"name"`
	Unit         string `json:"unit"`
	PerPortion   string `json:"per_portion"`
	Stock        string `json:"stock"`
	Short        string `json:"short"`
	Blocking     bool   `json:"blocking"`
}

// Out یعنی با موجودی فعلی حتی یک واحد از محصول قابل تهیه نیست.
func (p ProductPortion) Out() bool { return p.Portions == 0 }

// stockSnapshot عکس لحظه‌ای از موجودی مواد اولیه و رسپی محصولات است تا
// بتوان بدون قفل گرفتن مکرر (و در همان تراکنش ثبت سفارش) موجودی قابل تهیه
// هر محصول را حساب کرد.
type stockSnapshot struct {
	stock   map[string]float64
	labels  map[string]string
	units   map[string]string
	recipes map[string][]RecipeLineView
}

func stockSnapshotFromEntities(ingredients, recipes []Entity) stockSnapshot {
	snap := stockSnapshot{
		stock:   map[string]float64{},
		labels:  map[string]string{},
		units:   map[string]string{},
		recipes: map[string][]RecipeLineView{},
	}
	for _, ing := range ingredients {
		stock, err := strconv.ParseFloat(strings.TrimSpace(ing.Field("current_stock")), 64)
		if err != nil {
			stock = 0
		}
		snap.stock[ing.ID] = stock
		snap.labels[ing.ID] = ing.Label()
		snap.units[ing.ID] = ing.Field("base_unit")
	}
	for _, rec := range recipes {
		pid := strings.TrimSpace(rec.Field("product_id"))
		if pid == "" {
			continue
		}
		lines := rec.RecipeLines()
		if len(lines) == 0 {
			continue
		}
		snap.recipes[pid] = append(snap.recipes[pid], lines...)
	}
	return snap
}

func stockSnapshotFromState(st map[string]storedEntity) stockSnapshot {
	ingredients := make([]Entity, 0, 16)
	recipes := make([]Entity, 0, 16)
	for _, e := range st {
		if e.Deleted || (e.Type != "ingredient" && e.Type != "recipe") {
			continue
		}
		raw := make(json.RawMessage, len(e.Payload))
		copy(raw, e.Payload)
		ent := Entity{ID: e.ID, Type: e.Type, Version: e.Version, Payload: raw}
		if ent.Type == "ingredient" {
			ingredients = append(ingredients, ent)
		} else {
			recipes = append(recipes, ent)
		}
	}
	return stockSnapshotFromEntities(ingredients, recipes)
}

// portionsFor تعداد واحدهای قابل تهیه از یک محصول را برمی‌گرداند؛ گلوگاه
// (کم‌ترین) ماده اولیه تعیین‌کننده است و نامش برای پیام «کمبود» نگه داشته می‌شود.
func (snap stockSnapshot) portionsFor(productID string) ProductPortion {
	lines := snap.recipes[strings.TrimSpace(productID)]
	if len(lines) == 0 {
		return ProductPortion{Portions: -1}
	}
	best := -1
	missing := ""
	shortages := make([]PortionShortage, 0, len(lines))
	for _, ln := range lines {
		per, err := strconv.ParseFloat(strings.TrimSpace(ln.Quantity), 64)
		if err != nil || per <= 0 {
			continue
		}
		id := strings.TrimSpace(ln.IngredientID)
		stock := snap.stock[id]
		units := int(math.Floor(stock/per + 1e-9))
		if units < 0 {
			units = 0
		}
		name := strings.TrimSpace(ln.IngredientName)
		if name == "" {
			name = snap.labels[id]
		}
		if best < 0 || units < best {
			best = units
			if units <= 0 {
				missing = name
			}
		}
		// کمبود یعنی حتی برای «یک واحد» از این محصول هم ماده اولیه کافی نیست.
		if short := per - stock; short > 1e-9 && len(shortages) < 8 {
			unit := strings.TrimSpace(ln.Unit)
			if unit == "" {
				unit = snap.units[id]
			}
			shortages = append(shortages, PortionShortage{
				IngredientID: id,
				Name:         name,
				Unit:         ShortUnitName(unit),
				PerPortion:   faQtyNumber(per),
				Stock:        faQtyNumber(stock),
				Short:        faQtyNumber(short),
				Blocking:     units <= 0,
			})
		}
	}
	if best < 0 {
		return ProductPortion{Portions: -1}
	}
	return ProductPortion{Portions: best, Missing: missing, Shortages: shortages}
}

// ShortUnitName برچسب کوتاه واحد را برمی‌گرداند («گرم (g)» ← «گرم»).
func ShortUnitName(unit string) string {
	full := PersianUnitName(unit)
	if i := strings.Index(full, " ("); i > 0 {
		return full[:i]
	}
	return full
}

// shortfall اگر موجودی مواد اولیه برای اقلام سفارش کافی نباشد، پیام فارسی
// کمبود را برمی‌گرداند؛ در غیر این صورت رشتهٔ خالی.
func (snap stockSnapshot) shortfall(items []POSOrderLineInput) string {
	type need struct {
		qty   float64
		label string
	}
	needed := map[string]*need{}
	order := make([]string, 0, 8)
	for _, it := range items {
		if it.Quantity <= 0 {
			continue
		}
		for _, ln := range snap.recipes[strings.TrimSpace(it.ProductID)] {
			per, err := strconv.ParseFloat(strings.TrimSpace(ln.Quantity), 64)
			if err != nil || per <= 0 {
				continue
			}
			id := strings.TrimSpace(ln.IngredientID)
			cur, ok := needed[id]
			if !ok {
				label := strings.TrimSpace(ln.IngredientName)
				if label == "" {
					label = snap.labels[id]
				}
				cur = &need{label: label}
				needed[id] = cur
				order = append(order, id)
			}
			cur.qty += per * float64(it.Quantity)
		}
	}
	for _, id := range order {
		n := needed[id]
		if n.qty > snap.stock[id]+1e-9 {
			return fmt.Sprintf("موجودی «%s» برای ثبت این سفارش کافی نیست؛ نیاز: %s، موجود: %s",
				n.label, faQtyNumber(n.qty), faQtyNumber(snap.stock[id]))
		}
	}
	return ""
}

func faQtyNumber(v float64) string {
	return FormatPersianNumber(strconv.FormatFloat(v, 'f', -1, 64))
}

// ProductPortions موجودی قابل تهیهٔ هر محصول (بر پایهٔ رسپی) را برای نمایش
// «ناموجود» در صندوق برمی‌گرداند.
func (s *Store) ProductPortions(ctx context.Context) map[string]ProductPortion {
	out := map[string]ProductPortion{}
	if err := ctx.Err(); err != nil {
		return out
	}
	s.mu.Lock()
	snap := stockSnapshotFromState(s.state.Entities)
	s.mu.Unlock()
	for pid := range snap.recipes {
		out[pid] = snap.portionsFor(pid)
	}
	return out
}

type FinancialAnalytics struct {
	TotalSalesIRR       string
	CashSalesIRR        string
	CardSalesIRR        string
	CreditSalesIRR      string
	TotalExpensesIRR    string
	TotalReceivablesIRR string
	NetOperatingIRR     string
	CompletedOrders     int
	OpenOrders          int
	CancelledOrders     int
	OccupiedTables      int
	TotalTables         int
	PendingTasks        int
	CompletedTasks      int
	ActiveStaff         int
}

// validateLocalFields اعتبارسنجی حداقلیِ سمت سرور برای فیلدهای مالی/عددی؛
// UI به‌تنهایی قابل اعتماد نیست و مقادیر نامعتبر نباید وارد دفتر شوند.
func validateLocalFields(kind string, fields map[string]any) error {
	parseNonNegative := func(key string) error {
		raw, ok := fields[key]
		if !ok || raw == nil {
			return nil
		}
		str := strings.TrimSpace(fmt.Sprint(raw))
		if str == "" {
			return nil
		}
		bi := new(big.Int)
		if _, ok := bi.SetString(strings.SplitN(str, ".", 2)[0], 10); !ok || bi.Sign() < 0 {
			return errors.New("مقدار فیلد «" + key + "» باید عددی و نامنفی باشد")
		}
		return nil
	}
	switch kind {
	case "product":
		for _, k := range []string{"price", "cost"} {
			if err := parseNonNegative(k); err != nil {
				return err
			}
		}
	case "ingredient":
		for _, k := range []string{"current_stock", "minimum_stock", "reorder_point", "average_cost"} {
			if err := parseNonNegative(k); err != nil {
				return err
			}
		}
	case "debtor":
		for _, k := range []string{"total", "paid", "remaining"} {
			if err := parseNonNegative(k); err != nil {
				return err
			}
		}
	case "expense":
		if err := parseNonNegative("price"); err != nil {
			return err
		}
	}
	return nil
}

// SaveLocalRecord creates or updates any entity directly in the standalone local database.
func (s *Store) SaveLocalRecord(ctx context.Context, kind, id string, fields map[string]any) (string, error) {
	if strings.TrimSpace(kind) == "" {
		return "", errors.New("نوع موجودیت مشخص نشده است")
	}
	if err := validateLocalFields(kind, fields); err != nil {
		return "", err
	}
	// اگر قیمت محصول دستی عوض شود، تخفیف دسته‌بندی باطل می‌شود؛ وگرنه قیمت
	// پایهٔ قبلی می‌ماند و جدول محصولات قیمت قدیمی را خط‌خورده نشان می‌دهد.
	// وقتی قیمت تغییر نکرده (مثلاً فقط توضیحات ویرایش شده) تخفیف حفظ می‌شود.
	// اعمال تخفیف دسته خودش base_price را همراه price می‌فرستد و از این قاعده
	// مستثناست.
	if kind == "product" && strings.TrimSpace(id) != "" {
		if rawPrice, hasPrice := fields["price"]; hasPrice {
			if _, hasBase := fields["base_price"]; !hasBase {
				newPrice := strings.TrimSpace(fmt.Sprint(rawPrice))
				if current, curErr := s.Entity(ctx, "product", id); curErr == nil {
					if currentPrice := strings.TrimSpace(current.Field("price")); currentPrice != newPrice {
						fields["base_price"] = ""
						fields["discount_percent"] = ""
					}
				}
			}
		}
	}
	var err error
	isNew := false
	if strings.TrimSpace(id) == "" {
		id, err = UUID()
		if err != nil {
			return "", err
		}
		isNew = true
	}
	now := time.Now().UTC().Format(time.RFC3339)

	err = s.withTx(ctx, func(st *persistedDB) error {
		key := entityKey(kind, id)
		existing, exists := st.Entities[key]
		merged := map[string]any{}
		if exists && !existing.Deleted {
			_ = json.Unmarshal(existing.Payload, &merged)
		} else {
			isNew = true
		}
		for k, v := range fields {
			merged[k] = v
		}
		merged["id"] = id
		if isNew || merged["created_at"] == nil {
			merged["created_at"] = now
		}
		merged["updated_at"] = now

		nextVer := int64(1)
		if exists && existing.Version >= 1 {
			nextVer = existing.Version + 1
		}
		merged["version"] = nextVer

		raw, mErr := json.Marshal(merged)
		if mErr != nil {
			return mErr
		}
		st.Entities[key] = storedEntity{
			ID:      id,
			Type:    kind,
			Version: nextVer,
			Deleted: false,
			Payload: raw,
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

// DeleteLocalRecord marks an entity as deleted in the standalone local database.
func (s *Store) DeleteLocalRecord(ctx context.Context, kind, id string) error {
	if strings.TrimSpace(kind) == "" || strings.TrimSpace(id) == "" {
		return errors.New("شناسه رکورد نامعتبر است")
	}
	return s.withTx(ctx, func(st *persistedDB) error {
		key := entityKey(kind, id)
		existing, ok := st.Entities[key]
		if !ok || existing.Deleted {
			return errors.New("رکورد مورد نظر یافت نشد")
		}
		existing.Version++
		existing.Deleted = true
		st.Entities[key] = existing
		return nil
	})
}

// CreatePOSOrder creates a complete café order locally, updates table occupancy, customer loyalty, and debtor ledger if credit.
// debtorKind نوع حساب بدهکار را نرمال می‌کند: فقط «company» معتبر است و
// بقیه مقادیر (از جمله خالی) «personal» محسوب می‌شوند.
func debtorKind(kind string) string {
	if strings.TrimSpace(kind) == "company" {
		return "company"
	}
	return "personal"
}

// tehranLoc منطقه زمانی تهران برای مرز روز سفارش‌ها؛ بدون دیتابیس tz
// سیستم هم کار می‌کند (import خالی time/tzdata در همین پکیج).
func tehranLoc() *time.Location {
	if loc, err := time.LoadLocation("Asia/Tehran"); err == nil {
		return loc
	}
	return time.FixedZone("IRST", 3*3600+30*60)
}

// countOrdersOnTehranDay تعداد سفارش‌های ساخته‌شده در همان روز تهران را
// می‌شمارد تا شماره روزانه از ۱۰۰ شروع شود.
func countOrdersOnTehranDay(st *persistedDB, now time.Time) int {
	loc := tehranLoc()
	ty, tm, td := now.In(loc).Date()
	count := 0
	for _, e := range st.Entities {
		if e.Type != "order" || e.Deleted {
			continue
		}
		ent := Entity{ID: e.ID, Type: e.Type, Payload: e.Payload}
		ct, err := time.Parse(time.RFC3339, ent.Field("created_at"))
		if err != nil {
			continue
		}
		if y, m, d := ct.In(loc).Date(); y == ty && m == tm && d == td {
			count++
		}
	}
	return count
}

func (s *Store) CreatePOSOrder(ctx context.Context, req POSOrderInput) (string, error) {
	if len(req.Items) == 0 {
		return "", errors.New("لطفاً حداقل یک محصول برای ثبت سفارش انتخاب کنید")
	}
	orderID, err := UUID()
	if err != nil {
		return "", err
	}
	existingOrderID := ""
	now := time.Now().UTC().Format(time.RFC3339)

	err = s.withTx(ctx, func(st *persistedDB) error {
		if token := strings.TrimSpace(req.ClientToken); token != "" {
			for _, e := range st.Entities {
				if e.Deleted || e.Type != "order" {
					continue
				}
				ent := Entity{ID: e.ID, Type: e.Type, Version: e.Version, Payload: e.Payload}
				if ent.Field("client_token") == token {
					existingOrderID = e.ID
					return nil
				}
			}
		}
		subtotal := big.NewInt(0)
		lines := make([]map[string]any, 0, len(req.Items))

		for _, it := range req.Items {
			if strings.TrimSpace(it.ProductID) == "" || it.Quantity <= 0 {
				continue
			}
			prodEnt, ok := st.Entities[entityKey("product", it.ProductID)]
			if !ok || prodEnt.Deleted {
				return errors.New("یکی از محصولات انتخاب‌شده در منو یافت نشد")
			}
			ent := Entity{ID: prodEnt.ID, Type: prodEnt.Type, Version: prodEnt.Version, Payload: prodEnt.Payload}
			unitPrice := parseBigIntSafe(ent.PriceIRR())
			lineTotal := new(big.Int).Mul(unitPrice, big.NewInt(int64(it.Quantity)))
			subtotal.Add(subtotal, lineTotal)
			lineID, _ := UUID()
			lines = append(lines, map[string]any{
				"id":                    lineID,
				"product_id":            it.ProductID,
				"product_name_snapshot": ent.Label(),
				"quantity":              it.Quantity,
				"unit_price_irr":        unitPrice.String(),
				"line_total_irr":        lineTotal.String(),
				"note":                  strings.TrimSpace(it.Note),
			})
		}
		if len(lines) == 0 {
			return errors.New("هیچ ردیف معتبری در سفارش وجود ندارد")
		}
		// کنترل «ناموجودی»: اگر موجودی مواد اولیهٔ رسپی برای این اقلام کافی
		// نباشد، سفارش ثبت نمی‌شود (همان قاعده‌ای که صندوق هم نشان می‌دهد).
		if msg := stockSnapshotFromState(st.Entities).shortfall(req.Items); msg != "" {
			return errors.New(msg)
		}

		discount := parseBigIntSafe(req.DiscountIRR)
		if discount.Sign() < 0 {
			discount.SetInt64(0)
		}
		if discount.Cmp(subtotal) > 0 {
			discount.Set(subtotal)
		}
		extra := parseBigIntSafe(req.ExtraIRR)
		if extra.Sign() < 0 {
			extra.SetInt64(0)
		}
		total := new(big.Int).Sub(subtotal, discount)
		total.Add(total, extra)
		if total.Sign() < 0 {
			total.SetInt64(0)
		}

		// شماره سفارش روزانه از ۱۰۰ شروع می‌شود و نیمه‌شب به وقت تهران
		// از نو می‌شمارد؛ پیشوند لاتین ندارد (فقط عدد).
		orderNumber := strconv.Itoa(100 + countOrdersOnTehranDay(st, time.Now()))

		ft := strings.TrimSpace(req.FulfillmentType)
		if ft == "" {
			ft = "dine_in"
		}
		pm := strings.TrimSpace(req.PaymentMethod)
		if pm == "" {
			pm = "card_pos"
		}

		tableLabel := ""
		if req.TableID != "" {
			if tEnt, ok := st.Entities[entityKey("table", req.TableID)]; ok && !tEnt.Deleted {
				ent := Entity{ID: tEnt.ID, Type: tEnt.Type, Version: tEnt.Version, Payload: tEnt.Payload}
				tableLabel = ent.Label()
				// Mark table occupied
				var tm map[string]any
				if json.Unmarshal(tEnt.Payload, &tm) == nil {
					tm["status"] = "occupied"
					tm["current_order_id"] = orderID
					if b, mErr := json.Marshal(tm); mErr == nil {
						tEnt.Version++
						tEnt.Payload = b
						st.Entities[entityKey("table", req.TableID)] = tEnt
					}
				}
			}
		}

		custName := strings.TrimSpace(req.CustomerName)
		custPhone := strings.TrimSpace(req.CustomerPhone)
		if req.CustomerID != "" {
			if cEnt, ok := st.Entities[entityKey("customer", req.CustomerID)]; ok && !cEnt.Deleted {
				ent := Entity{ID: cEnt.ID, Type: cEnt.Type, Version: cEnt.Version, Payload: cEnt.Payload}
				if custName == "" {
					custName = ent.Label()
				}
				if custPhone == "" {
					custPhone = ent.Field("phone")
				}
				// Add loyalty points and update total spent
				var cm map[string]any
				if json.Unmarshal(cEnt.Payload, &cm) == nil {
					pts, _ := strconv.Atoi(ent.Field("loyalty_points"))
					earned := int(new(big.Int).Div(total, big.NewInt(100000)).Int64())
					if earned < 1 {
						earned = 1
					}
					cm["loyalty_points"] = pts + earned
					prevOrders, _ := strconv.Atoi(ent.Field("orders_count"))
					cm["orders_count"] = prevOrders + 1
					if b, mErr := json.Marshal(cm); mErr == nil {
						cEnt.Version++
						cEnt.Payload = b
						st.Entities[entityKey("customer", req.CustomerID)] = cEnt
					}
				}
			}
		}
		if custName == "" {
			custName = "مهمان حضوری کافه"
		}

		// If credit/debtor payment method, create a debtor record automatically
		if pm == "credit_debtor" {
			debtID, _ := UUID()
			debtPayload, _ := json.Marshal(map[string]any{
				"id":           debtID,
				"full_name":    custName,
				"phone":        custPhone,
				"total":        total.String(),
				"paid":         "0",
				"remaining":    total.String(),
				"currency":     "IRR",
				"status":       "open",
				"description":  "سفارش نسیه شماره " + orderNumber,
				"note":         strings.TrimSpace(req.DebtorNote),
				"due_at":       strings.TrimSpace(req.DebtorDueAt),
				"account_type": debtorKind(req.DebtorKind),
				"group_id":     strings.TrimSpace(req.DebtorGroupID),
				"order_id":     orderID,
				"created_at":   now,
				"version":      1,
			})
			st.Entities[entityKey("debtor", debtID)] = storedEntity{
				ID:      debtID,
				Type:    "debtor",
				Version: 1,
				Payload: debtPayload,
			}
		}

		orderPayload, err := json.Marshal(map[string]any{
			"id":               orderID,
			"client_token":     strings.TrimSpace(req.ClientToken),
			"order_number":     orderNumber,
			"fulfillment_type": ft,
			"table_id":         req.TableID,
			"table_label":      tableLabel,
			"customer_id":      req.CustomerID,
			"customer_name":    custName,
			"customer_phone":   custPhone,
			"cashier_name":     strings.TrimSpace(req.CashierName),
			"payment_method":   pm,
			"subtotal_irr":     subtotal.String(),
			"discount_irr":     discount.String(),
			"extra_irr":        extra.String(),
			"extra_note":       strings.TrimSpace(req.ExtraNote),
			"total_irr":        total.String(),
			"price":            total.String(),
			"status":           "pending",
			"note":             strings.TrimSpace(req.Note),
			"items":            lines,
			"created_at":       now,
			"version":          1,
		})
		if err != nil {
			return err
		}
		st.Entities[entityKey("order", orderID)] = storedEntity{
			ID:      orderID,
			Type:    "order",
			Version: 1,
			Payload: orderPayload,
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if existingOrderID != "" {
		return existingOrderID, nil
	}
	return orderID, nil
}

// OrderItemsUpdate ورودی ویرایش کامل سفارش (صندوق/دیالوگ ویرایش وب).
// اشاره‌گرهای nil یعنی «مقدار فعلی سفارش حفظ شود».
type OrderItemsUpdate struct {
	Lines       []POSOrderLineInput
	DiscountIRR *string
	ExtraIRR    *string
	ExtraNote   *string
	Note        *string
}

// UpdateOrderItems اقلام سفارش را با قیمت‌های فعلی منو (سمت سرور) جایگزین
// و جمع‌ها را دوباره محاسبه می‌کند؛ هم‌معنای updateOrderItems وب.
// سفارش تکمیل‌شده (کسر BOM انجام شده) یا لغو/برگشتی قابل ویرایش نیست.
func (s *Store) UpdateOrderItems(ctx context.Context, orderID string, lines []POSOrderLineInput) error {
	return s.UpdateOrderItemsFull(ctx, orderID, OrderItemsUpdate{Lines: lines})
}

// UpdateOrderItemsFull مانند UpdateOrderItems است و تخفیف/مبلغ اضافی/یادداشت
// را هم (در صورت ارسال) به‌روز می‌کند؛ جمع نهایی سمت سرور محاسبه می‌شود:
// total = subtotal − discount + extra (کف صفر).
func (s *Store) UpdateOrderItemsFull(ctx context.Context, orderID string, upd OrderItemsUpdate) error {
	lines := upd.Lines
	now := time.Now().UTC().Format(time.RFC3339)
	return s.withTx(ctx, func(st *persistedDB) error {
		key := entityKey("order", orderID)
		oEnt, ok := st.Entities[key]
		if !ok || oEnt.Deleted {
			return errors.New("سفارش مورد نظر یافت نشد")
		}
		var om map[string]any
		if err := json.Unmarshal(oEnt.Payload, &om); err != nil {
			return err
		}
		switch strings.ToLower(strings.TrimSpace(fmt.Sprint(om["status"]))) {
		case "completed":
			return errors.New("سفارش تکمیل‌شده قابل ویرایش نیست؛ کسر انبار (BOM) آن انجام شده است")
		case "cancelled", "canceled", "refunded":
			return errors.New("سفارش لغو یا برگشت‌داده‌شده قابل ویرایش نیست.")
		}
		subtotal := big.NewInt(0)
		newLines := make([]map[string]any, 0, len(lines))
		for _, it := range lines {
			if strings.TrimSpace(it.ProductID) == "" || it.Quantity <= 0 {
				continue
			}
			prodEnt, pOk := st.Entities[entityKey("product", it.ProductID)]
			if !pOk || prodEnt.Deleted {
				return errors.New("یکی از محصولات انتخاب‌شده در منو یافت نشد")
			}
			ent := Entity{ID: prodEnt.ID, Type: prodEnt.Type, Version: prodEnt.Version, Payload: prodEnt.Payload}
			unitPrice := parseBigIntSafe(ent.PriceIRR())
			lineTotal := new(big.Int).Mul(unitPrice, big.NewInt(int64(it.Quantity)))
			subtotal.Add(subtotal, lineTotal)
			lineID, _ := UUID()
			newLines = append(newLines, map[string]any{
				"id":                    lineID,
				"product_id":            it.ProductID,
				"product_name_snapshot": ent.Label(),
				"quantity":              it.Quantity,
				"unit_price_irr":        unitPrice.String(),
				"line_total_irr":        lineTotal.String(),
				"note":                  strings.TrimSpace(it.Note),
			})
		}
		if len(newLines) == 0 {
			return errors.New("حداقل یک محصول در سفارش لازم است.")
		}
		// ویرایش سفارش هم مثل ثبت اولیه، نباید موجودی منفی بسازد.
		if msg := stockSnapshotFromState(st.Entities).shortfall(lines); msg != "" {
			return errors.New(msg)
		}
		ent := Entity{ID: oEnt.ID, Type: oEnt.Type, Version: oEnt.Version, Payload: oEnt.Payload}
		discount := parseBigIntSafe(ent.Field("discount_irr"))
		if upd.DiscountIRR != nil {
			discount = parseBigIntSafe(*upd.DiscountIRR)
		}
		if discount.Sign() < 0 {
			discount.SetInt64(0)
		}
		if discount.Cmp(subtotal) > 0 {
			discount.Set(subtotal)
		}
		extra := parseBigIntSafe(ent.Field("extra_irr"))
		if upd.ExtraIRR != nil {
			extra = parseBigIntSafe(*upd.ExtraIRR)
		}
		if extra.Sign() < 0 {
			extra.SetInt64(0)
		}
		total := new(big.Int).Sub(subtotal, discount)
		total.Add(total, extra)
		if total.Sign() < 0 {
			total.SetInt64(0)
		}
		om["items"] = newLines
		om["subtotal_irr"] = subtotal.String()
		om["discount_irr"] = discount.String()
		om["extra_irr"] = extra.String()
		om["total_irr"] = total.String()
		om["price"] = total.String()
		if upd.ExtraNote != nil {
			om["extra_note"] = strings.TrimSpace(*upd.ExtraNote)
		}
		if upd.Note != nil {
			om["note"] = strings.TrimSpace(*upd.Note)
		}
		om["updated_at"] = now
		b, err := json.Marshal(om)
		if err != nil {
			return err
		}
		oEnt.Version++
		oEnt.Payload = b
		st.Entities[key] = oEnt
		return nil
	})
}

// ─── حساب دنگی (Split Bill) — هم‌معنای utils/split-bill.ts وب ───────────────
// هر نفر فقط سهم خودش را می‌پردازد؛ مبلغ قابل‌پرداخت کلِ سفارش (با تخفیف و
// مبلغ اضافی) به‌تناسب مبلغ هر قلم بین اقلام پخش می‌شود و مانده‌ی هر قلم از
// روی پرداخت‌های ثبت‌شده کم می‌شود. تا مانده صفر نشود، تسویه کامل نیست.

type SplitBillLine struct {
	Key            string // شناسه خط سفارش (id)
	Title          string
	Quantity       int
	PaidQuantity   int
	UnitPayableIRR int64 // قیمت قابل‌پرداخت یک عدد (سهمی از مبلغ نهایی)
	LinePayableIRR int64 // سهم این قلم از مبلغ نهایی سفارش
	PaidIRR        int64
	RemainingIRR   int64
}

type SplitPaymentLine struct {
	LineID    string
	Quantity  int
	AmountIRR int64
}

type SplitPayment struct {
	ID          string
	At          string
	Method      string // cash | card | transfer
	AmountIRR   int64
	ClientToken string
	Lines       []SplitPaymentLine
}

func parseSplitPayments(e Entity) []SplitPayment {
	raw := strings.TrimSpace(e.Field("split_payments"))
	if raw == "" {
		return nil
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		return nil
	}
	out := make([]SplitPayment, 0, len(rows))
	for _, r := range rows {
		p := SplitPayment{
			ID:          toStr(r["id"]),
			At:          toStr(r["at"]),
			Method:      toStr(r["method"]),
			AmountIRR:   parseBigIntSafe(toStr(r["amount_irr"])).Int64(),
			ClientToken: toStr(r["client_token"]),
		}
		if ll, ok := r["lines"].([]any); ok {
			for _, li := range ll {
				if lm, ok := li.(map[string]any); ok {
					q, _ := strconv.Atoi(toStr(lm["quantity"]))
					p.Lines = append(p.Lines, SplitPaymentLine{
						LineID:    toStr(lm["line_id"]),
						Quantity:  q,
						AmountIRR: parseBigIntSafe(toStr(lm["amount_irr"])).Int64(),
					})
				}
			}
		}
		out = append(out, p)
	}
	return out
}

func toStr(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatInt(int64(t), 10)
	case nil:
		return ""
	default:
		return fmt.Sprint(t)
	}
}

// SplitBillOf صورت‌حساب دنگی سفارش را از روی اقلام و پرداخت‌های ثبت‌شده می‌سازد.
func SplitBillOf(e Entity) (lines []SplitBillLine, total, paid, remaining int64) {
	total = parseBigIntSafe(e.PriceIRR()).Int64()
	items := e.OrderItems()
	subtotal := int64(0)
	for _, it := range items {
		subtotal += parseBigIntSafe(it.LineTotal).Int64()
	}
	payments := parseSplitPayments(e)
	paidQty := map[string]int{}
	paidAmt := map[string]int64{}
	for _, p := range payments {
		paid += p.AmountIRR
		for _, l := range p.Lines {
			paidQty[l.LineID] += l.Quantity
			paidAmt[l.LineID] += l.AmountIRR
		}
	}
	lines = make([]SplitBillLine, 0, len(items))
	allocated := int64(0)
	for i, it := range items {
		lineTotal := parseBigIntSafe(it.LineTotal).Int64()
		var payable int64
		if subtotal > 0 && total > 0 {
			if i == len(items)-1 {
				payable = total - allocated
			} else {
				payable = lineTotal * total / subtotal
			}
		}
		if payable < 0 {
			payable = 0
		}
		allocated += payable
		qty := it.Quantity
		var unit int64
		if qty > 0 {
			unit = payable / int64(qty)
		}
		rem := payable - paidAmt[it.ID]
		if rem < 0 {
			rem = 0
		}
		lines = append(lines, SplitBillLine{
			Key: it.ID, Title: it.ProductName, Quantity: qty,
			PaidQuantity: paidQty[it.ID], UnitPayableIRR: unit,
			LinePayableIRR: payable, PaidIRR: paidAmt[it.ID], RemainingIRR: rem,
		})
	}
	remaining = total - paid
	if remaining < 0 {
		remaining = 0
	}
	return lines, total, paid, remaining
}

// splitQuote مبلغ انتخاب فعلی یک قلم را حساب می‌کند (quotedLineAmount وب):
// انتخاب کل مانده‌ی قلم = مانده واقعی همان قلم (بدون خطای گرد کردن).
func splitQuote(l SplitBillLine, qty int) int64 {
	unpaid := l.Quantity - l.PaidQuantity
	if qty <= 0 || unpaid <= 0 {
		return 0
	}
	if qty >= unpaid {
		return l.RemainingIRR
	}
	amt := int64(qty) * l.UnitPayableIRR
	if amt <= 0 {
		amt = l.RemainingIRR * int64(qty) / int64(unpaid)
	}
	if amt > l.RemainingIRR {
		amt = l.RemainingIRR
	}
	return amt
}

// RecordSplitPayment پرداخت یک نفر را در حساب دنگی ثبت می‌کند.
// picks: نگاشت شناسه خط ← تعداد انتخابی این نفر. clientToken برای
// جلوگیری از ثبت دوباره‌ی همان پرداخت (دابل‌کلیک/Retry) استفاده می‌شود.
func (s *Store) RecordSplitPayment(ctx context.Context, orderID string, picks map[string]int, method, clientToken string) (paid, remaining int64, err error) {
	method = strings.TrimSpace(strings.ToLower(method))
	if method != "cash" && method != "card" && method != "transfer" {
		return 0, 0, errors.New("روش پرداخت دنگی معتبر نیست")
	}
	if len(picks) == 0 {
		return 0, 0, errors.New("اول آیتمی را که این نفر می‌پردازد انتخاب کنید.")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	err = s.withTx(ctx, func(st *persistedDB) error {
		key := entityKey("order", orderID)
		oEnt, ok := st.Entities[key]
		if !ok || oEnt.Deleted {
			return errors.New("سفارش مورد نظر یافت نشد")
		}
		ent := Entity{ID: oEnt.ID, Type: oEnt.Type, Version: oEnt.Version, Payload: oEnt.Payload}
		switch strings.ToLower(ent.Field("status")) {
		case "completed":
			return errors.New("این سفارش تکمیل و تسویه شده است")
		case "cancelled", "canceled", "refunded":
			return errors.New("سفارش لغو یا برگشت‌داده‌شده قابل پرداخت دنگی نیست.")
		}
		payments := parseSplitPayments(ent)
		if tok := strings.TrimSpace(clientToken); tok != "" {
			for _, p := range payments {
				if p.ClientToken == tok {
					_, _, paid, remaining = SplitBillOf(ent)
					return nil // همان پرداخت قبلاً ثبت شده؛ دوباره ثبت نمی‌شود
				}
			}
		}
		lines, total, _, _ := SplitBillOf(ent)
		byKey := map[string]SplitBillLine{}
		for _, l := range lines {
			byKey[l.Key] = l
		}
		var payLines []SplitPaymentLine
		var amount int64
		for lineID, qty := range picks {
			l, ok := byKey[lineID]
			if !ok {
				return errors.New("یکی از اقلام انتخاب‌شده در سفارش نیست")
			}
			unpaid := l.Quantity - l.PaidQuantity
			if qty <= 0 || qty > unpaid {
				return errors.New("تعداد انتخابی برای «" + l.Title + "» بیش از مانده‌ی آن است")
			}
			amt := splitQuote(l, qty)
			payLines = append(payLines, SplitPaymentLine{LineID: lineID, Quantity: qty, AmountIRR: amt})
			amount += amt
		}
		if amount <= 0 {
			return errors.New("مبلغ پرداخت صفر است؛ آیتمی انتخاب کنید.")
		}
		pid, uErr := UUID()
		if uErr != nil {
			return uErr
		}
		payments = append(payments, SplitPayment{ID: pid, At: now, Method: method, AmountIRR: amount, ClientToken: strings.TrimSpace(clientToken), Lines: payLines})
		var om map[string]any
		if uErr := json.Unmarshal(oEnt.Payload, &om); uErr != nil {
			return uErr
		}
		rows := make([]map[string]any, 0, len(payments))
		var paidSum int64
		for _, p := range payments {
			paidSum += p.AmountIRR
			lr := make([]map[string]any, 0, len(p.Lines))
			for _, l := range p.Lines {
				lr = append(lr, map[string]any{"line_id": l.LineID, "quantity": l.Quantity, "amount_irr": strconv.FormatInt(l.AmountIRR, 10)})
			}
			rows = append(rows, map[string]any{"id": p.ID, "at": p.At, "method": p.Method, "amount_irr": strconv.FormatInt(p.AmountIRR, 10), "client_token": p.ClientToken, "lines": lr})
		}
		blob, mErr := json.Marshal(rows)
		if mErr != nil {
			return mErr
		}
		rem := total - paidSum
		if rem < 0 {
			rem = 0
		}
		status := "UNPAID"
		if rem == 0 && total > 0 {
			status = "PAID"
		} else if paidSum > 0 {
			status = "PARTIAL"
		}
		om["split_payments"] = string(blob)
		om["split_paid_irr"] = strconv.FormatInt(paidSum, 10)
		om["split_remaining_irr"] = strconv.FormatInt(rem, 10)
		om["split_status"] = status
		om["updated_at"] = now
		b, mErr := json.Marshal(om)
		if mErr != nil {
			return mErr
		}
		oEnt.Version++
		oEnt.Payload = b
		st.Entities[key] = oEnt
		paid, remaining = paidSum, rem
		return nil
	})
	return paid, remaining, err
}

// UndoLastSplitPayment آخرین پرداخت دنگی را برمی‌گرداند (مانند وب).
func (s *Store) UndoLastSplitPayment(ctx context.Context, orderID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	return s.withTx(ctx, func(st *persistedDB) error {
		key := entityKey("order", orderID)
		oEnt, ok := st.Entities[key]
		if !ok || oEnt.Deleted {
			return errors.New("سفارش مورد نظر یافت نشد")
		}
		ent := Entity{ID: oEnt.ID, Type: oEnt.Type, Version: oEnt.Version, Payload: oEnt.Payload}
		if strings.EqualFold(ent.Field("status"), "completed") {
			return errors.New("سفارش تکمیل‌شده قابل برگرداندن پرداخت نیست")
		}
		payments := parseSplitPayments(ent)
		if len(payments) == 0 {
			return errors.New("پرداخت دنگی برای برگرداندن وجود ندارد")
		}
		payments = payments[:len(payments)-1]
		var om map[string]any
		if err := json.Unmarshal(oEnt.Payload, &om); err != nil {
			return err
		}
		rows := make([]map[string]any, 0, len(payments))
		var paidSum int64
		for _, p := range payments {
			paidSum += p.AmountIRR
			lr := make([]map[string]any, 0, len(p.Lines))
			for _, l := range p.Lines {
				lr = append(lr, map[string]any{"line_id": l.LineID, "quantity": l.Quantity, "amount_irr": strconv.FormatInt(l.AmountIRR, 10)})
			}
			rows = append(rows, map[string]any{"id": p.ID, "at": p.At, "method": p.Method, "amount_irr": strconv.FormatInt(p.AmountIRR, 10), "client_token": p.ClientToken, "lines": lr})
		}
		blob, err := json.Marshal(rows)
		if err != nil {
			return err
		}
		total := parseBigIntSafe(ent.PriceIRR()).Int64()
		rem := total - paidSum
		if rem < 0 {
			rem = 0
		}
		status := "UNPAID"
		if rem == 0 && total > 0 {
			status = "PAID"
		} else if paidSum > 0 {
			status = "PARTIAL"
		}
		om["split_payments"] = string(blob)
		om["split_paid_irr"] = strconv.FormatInt(paidSum, 10)
		om["split_remaining_irr"] = strconv.FormatInt(rem, 10)
		om["split_status"] = status
		om["updated_at"] = now
		b, err := json.Marshal(om)
		if err != nil {
			return err
		}
		oEnt.Version++
		oEnt.Payload = b
		st.Entities[key] = oEnt
		return nil
	})
}

// TransferOrder سفارش باز را به میز دیگری منتقل می‌کند (انتقال میز وب):
// میز مبدأ آزاد و میز مقصد درگیر همین سفارش می‌شود. مقصد نباید سفارش باز داشته باشد.
func (s *Store) TransferOrder(ctx context.Context, orderID, targetTableID string) error {
	targetTableID = strings.TrimSpace(targetTableID)
	if targetTableID == "" {
		return errors.New("میز مقصد را انتخاب کنید")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	return s.withTx(ctx, func(st *persistedDB) error {
		key := entityKey("order", orderID)
		oEnt, ok := st.Entities[key]
		if !ok || oEnt.Deleted {
			return errors.New("سفارش مورد نظر یافت نشد")
		}
		ent := Entity{ID: oEnt.ID, Type: oEnt.Type, Version: oEnt.Version, Payload: oEnt.Payload}
		switch strings.ToLower(ent.Field("status")) {
		case "completed", "cancelled", "canceled", "refunded":
			return errors.New("فقط سفارش باز قابل انتقال است")
		}
		sourceTableID := ent.Field("table_id")
		if sourceTableID == targetTableID {
			return errors.New("میز مقصد همان میز فعلی است")
		}
		tKey := entityKey("table", targetTableID)
		tEnt, ok := st.Entities[tKey]
		if !ok || tEnt.Deleted {
			return errors.New("میز مقصد یافت نشد")
		}
		tEntView := Entity{ID: tEnt.ID, Type: tEnt.Type, Version: tEnt.Version, Payload: tEnt.Payload}
		if strings.TrimSpace(tEntView.Field("current_order_id")) != "" {
			return errors.New("میز مقصد سفارش باز دارد؛ اول آن را تسویه یا منتقل کنید")
		}
		for _, e := range st.Entities {
			if e.Deleted || e.Type != "order" || e.ID == orderID {
				continue
			}
			oe := Entity{ID: e.ID, Type: e.Type, Version: e.Version, Payload: e.Payload}
			if oe.Field("table_id") == targetTableID {
				switch strings.ToLower(oe.Field("status")) {
				case "pending", "accepted", "preparing", "ready":
					return errors.New("میز مقصد سفارش باز دارد؛ اول آن را تسویه یا منتقل کنید")
				}
			}
		}
		var om map[string]any
		if err := json.Unmarshal(oEnt.Payload, &om); err != nil {
			return err
		}
		om["table_id"] = targetTableID
		om["table_label"] = tEntView.Label()
		om["updated_at"] = now
		b, err := json.Marshal(om)
		if err != nil {
			return err
		}
		oEnt.Version++
		oEnt.Payload = b
		st.Entities[key] = oEnt
		// میز مبدأ آزاد شود
		if sourceTableID != "" {
			if sEnt, sOk := st.Entities[entityKey("table", sourceTableID)]; sOk && !sEnt.Deleted {
				var sm map[string]any
				if json.Unmarshal(sEnt.Payload, &sm) == nil {
					if toStr(sm["current_order_id"]) == orderID || strings.TrimSpace(toStr(sm["current_order_id"])) == "" {
						sm["current_order_id"] = ""
						sm["status"] = "available"
						if sb, mErr := json.Marshal(sm); mErr == nil {
							sEnt.Version++
							sEnt.Payload = sb
							st.Entities[entityKey("table", sourceTableID)] = sEnt
						}
					}
				}
			}
		}
		var tm map[string]any
		if json.Unmarshal(tEnt.Payload, &tm) == nil {
			tm["status"] = "occupied"
			tm["current_order_id"] = orderID
			if tb, mErr := json.Marshal(tm); mErr == nil {
				tEnt.Version++
				tEnt.Payload = tb
				st.Entities[tKey] = tEnt
			}
		}
		return nil
	})
}

// DeferOrderToDebtor سفارش را به فهرست بدهکاران منتقل می‌کند؛ هم‌معنای
// «بعداً پرداخت می‌کنند (بدهکاران)» وب: پرونده بدهی با مانده کامل ساخته و
// روش پرداخت سفارش اعتباری می‌شود. ثبت دوباره همان سفارش خطاست.
func (s *Store) DeferOrderToDebtor(ctx context.Context, orderID, name, phone, dueAt, note string) (string, error) {
	name = strings.TrimSpace(name)
	if len([]rune(name)) < 2 {
		return "", errors.New("نام مشتری را وارد کنید.")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	debtID := ""
	err := s.withTx(ctx, func(st *persistedDB) error {
		key := entityKey("order", orderID)
		oEnt, ok := st.Entities[key]
		if !ok || oEnt.Deleted {
			return errors.New("سفارش مورد نظر یافت نشد")
		}
		var om map[string]any
		if err := json.Unmarshal(oEnt.Payload, &om); err != nil {
			return err
		}
		ent := Entity{ID: oEnt.ID, Type: oEnt.Type, Version: oEnt.Version, Payload: oEnt.Payload}
		switch strings.ToLower(ent.Field("status")) {
		case "cancelled", "canceled":
			return errors.New("سفارش لغوشده را نمی‌توان به بدهکاران منتقل کرد")
		}
		if strings.EqualFold(ent.Field("payment_method"), "credit_debtor") {
			return errors.New("این سفارش قبلاً در بدهکاران ثبت شده است")
		}
		total := parseBigIntSafe(ent.PriceIRR())
		if total.Sign() <= 0 {
			return errors.New("مبلغ سفارش برای ثبت بدهی معتبر نیست")
		}
		id, uErr := UUID()
		if uErr != nil {
			return uErr
		}
		debtID = id
		debtPayload, mErr := json.Marshal(map[string]any{
			"id":          debtID,
			"full_name":   name,
			"phone":       strings.TrimSpace(phone),
			"total":       total.String(),
			"paid":        "0",
			"remaining":   total.String(),
			"currency":    "IRR",
			"status":      "open",
			"description": "سفارش نسیه شماره " + ent.Field("order_number"),
			"note":        strings.TrimSpace(note),
			"due_at":      strings.TrimSpace(dueAt),
			"order_id":    orderID,
			"created_at":  now,
			"version":     1,
		})
		if mErr != nil {
			return mErr
		}
		st.Entities[entityKey("debtor", debtID)] = storedEntity{ID: debtID, Type: "debtor", Version: 1, Payload: debtPayload}
		om["payment_method"] = "credit_debtor"
		om["customer_name"] = name
		if strings.TrimSpace(phone) != "" {
			om["customer_phone"] = strings.TrimSpace(phone)
		}
		om["updated_at"] = now
		b, mErr := json.Marshal(om)
		if mErr != nil {
			return mErr
		}
		oEnt.Version++
		oEnt.Payload = b
		st.Entities[key] = oEnt
		return nil
	})
	return debtID, err
}

// allowedOrderTransitions ماشین وضعیت سفارش (هم‌معنای چرخه وب):
// فقط گذارهای معتبر پذیرفته می‌شوند تا مثلاً سفارش تکمیل‌شده دوباره
// به بار برنگردد یا کسر انبار دوبار رخ ندهد.
// چرخه وب: PENDING → ACCEPTED → PREPARING → READY → COMPLETED و لغو از هر
// وضعیت باز. گذارهای میان‌بر قبلی (مثلاً pending→completed) برای سازگاری با
// داده/جریان‌های قدیمPREPARING → READY → COMPLETED و لغو از هر
// وضعیت باز. گذارهای میان‌بر قبلی (مثلاً pending→completed) برای سازگاری با
// داده/جریان‌های قدیمی آفلاین نگه داشته شده‌اند؛ رابط کاربری فقط چرخه وب را
// به‌صورت گام‌به‌گام عرضه می‌کند.
var allowedOrderTransitions = map[string][]string{
	"":          {"pending", "accepted", "preparing", "ready", "completed", "cancelled"},
	"pending":   {"accepted", "preparing", "ready", "completed", "cancelled"},
	"accepted":  {"preparing", "ready", "completed", "cancelled"},
	"preparing": {"ready", "completed", "cancelled"},
	"ready":     {"completed", "cancelled"},
	"completed": {},
	"cancelled": {},
	"canceled":  {},
}

func validOrderTransition(from, to string) bool {
	from = strings.ToLower(strings.TrimSpace(from))
	to = strings.ToLower(strings.TrimSpace(to))
	if from == to {
		return true // تکرار همان وضعیت، عملیات بی‌اثر و امن است
	}
	for _, allowed := range allowedOrderTransitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// UpdateLocalOrderStatus transitions an order's state and deducts recipe BOM inventory on completion.
func (s *Store) UpdateLocalOrderStatus(ctx context.Context, orderID, newStatus string) error {
	return s.UpdateLocalOrderStatusReason(ctx, orderID, newStatus, "")
}

// UpdateLocalOrderStatusReason همان گذار وضعیت است با ثبت دلیل (در وب، لغو
// سفارش بدون دلیل ممکن نیست؛ دلیل روی خود سفارش ذخیره می‌شود).
func (s *Store) UpdateLocalOrderStatusReason(ctx context.Context, orderID, newStatus, reason string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	return s.withTx(ctx, func(st *persistedDB) error {
		key := entityKey("order", orderID)
		oEnt, ok := st.Entities[key]
		if !ok || oEnt.Deleted {
			return errors.New("سفارش مورد نظر یافت نشد")
		}
		var om map[string]any
		if err := json.Unmarshal(oEnt.Payload, &om); err != nil {
			return err
		}
		prevStatus, _ := om["status"].(string)
		if !validOrderTransition(prevStatus, newStatus) {
			fa := func(s string) string {
				switch strings.ToLower(strings.TrimSpace(s)) {
				case "pending":
					return "در انتظار تأیید"
				case "accepted":
					return "تأیید صندوقدار"
				case "preparing":
					return "در حال آماده‌سازی"
				case "ready":
					return "آماده تحویل"
				case "completed":
					return "تکمیل‌شده"
				case "cancelled", "canceled":
					return "لغوشده"
				}
				return s
			}
			if ls := strings.ToLower(strings.TrimSpace(prevStatus)); ls == "completed" || ls == "cancelled" || ls == "canceled" {
				return errors.New("این سفارش «" + fa(prevStatus) + "» است و دیگر قابل تغییر نیست")
			}
			return errors.New("این تغییر وضعیت ممکن نیست: سفارش «" + fa(prevStatus) + "» را نمی‌توان به «" + fa(newStatus) + "» تغییر داد")
		}
		if strings.EqualFold(prevStatus, newStatus) {
			return nil
		}
		om["status"] = newStatus
		om["updated_at"] = now
		if strings.TrimSpace(reason) != "" {
			om["status_reason"] = strings.TrimSpace(reason)
		}
		if newStatus == "completed" {
			om["completed_at"] = now
		}

		// When completing an order for the first time, deduct recipe BOM ingredients & free table
		if newStatus == "completed" && prevStatus != "completed" {
			ent := Entity{ID: oEnt.ID, Type: oEnt.Type, Version: oEnt.Version, Payload: oEnt.Payload}
			if tableID := ent.Field("table_id"); tableID != "" {
				if tEnt, exists := st.Entities[entityKey("table", tableID)]; exists && !tEnt.Deleted {
					var tm map[string]any
					if json.Unmarshal(tEnt.Payload, &tm) == nil {
						tm["status"] = "available"
						delete(tm, "current_order_id")
						if b, mErr := json.Marshal(tm); mErr == nil {
							tEnt.Version++
							tEnt.Payload = b
							st.Entities[entityKey("table", tableID)] = tEnt
						}
					}
				}
			}
			// Deduct recipe BOM
			orderItems := ent.OrderItems()
			for _, line := range orderItems {
				for _, rEnt := range st.Entities {
					if rEnt.Deleted || rEnt.Type != "recipe" {
						continue
					}
					rec := Entity{ID: rEnt.ID, Type: rEnt.Type, Version: rEnt.Version, Payload: rEnt.Payload}
					if rec.Field("product_id") != line.ProductID {
						continue
					}
					for _, rl := range rec.RecipeLines() {
						ingKey := entityKey("ingredient", rl.IngredientID)
						if ingEnt, iOk := st.Entities[ingKey]; iOk && !ingEnt.Deleted {
							var im map[string]any
							if json.Unmarshal(ingEnt.Payload, &im) == nil {
								curStock, _ := strconv.ParseFloat(fmt.Sprint(im["current_stock"]), 64)
								perUnit, _ := strconv.ParseFloat(rl.Quantity, 64)
								deduct := perUnit * float64(line.Quantity)
								newStock := curStock - deduct
								im["current_stock"] = strconv.FormatFloat(newStock, 'f', 2, 64)
								if b, mErr := json.Marshal(im); mErr == nil {
									ingEnt.Version++
									ingEnt.Payload = b
									st.Entities[ingKey] = ingEnt
									// سند گردش انبار برای ردیابی کسر رسپی (در همان تراکنش اتمیک)
									if movID, uErr := UUID(); uErr == nil && deduct != 0 {
										ingView := Entity{ID: ingEnt.ID, Type: ingEnt.Type, Version: ingEnt.Version, Payload: ingEnt.Payload}
										movPayload, _ := json.Marshal(map[string]any{
											"id":               movID,
											"title":            ingView.Label(),
											"ingredient_id":    rl.IngredientID,
											"ingredient_name":  ingView.Label(),
											"unit":             ingView.Field("base_unit"),
											"transaction_type": "consumption",
											"quantity":         strconv.FormatFloat(-deduct, 'f', 2, 64),
											"stock_after":      strconv.FormatFloat(newStock, 'f', 2, 64),
											"description":      "کسر خودکار رسپی سفارش " + ent.Field("order_number"),
											"order_id":         orderID,
											"created_at":       now,
											"version":          1,
										})
										st.Entities[entityKey("inventory_movement", movID)] = storedEntity{ID: movID, Type: "inventory_movement", Version: 1, Payload: movPayload}
									}
								}
							}
						}
					}
				}
			}
		}

		if newStatus == "cancelled" {
			ent := Entity{ID: oEnt.ID, Type: oEnt.Type, Version: oEnt.Version, Payload: oEnt.Payload}
			if tableID := ent.Field("table_id"); tableID != "" {
				if tEnt, exists := st.Entities[entityKey("table", tableID)]; exists && !tEnt.Deleted {
					var tm map[string]any
					if json.Unmarshal(tEnt.Payload, &tm) == nil {
						tm["status"] = "available"
						delete(tm, "current_order_id")
						if b, mErr := json.Marshal(tm); mErr == nil {
							tEnt.Version++
							tEnt.Payload = b
							st.Entities[entityKey("table", tableID)] = tEnt
						}
					}
				}
			}
		}

		b, err := json.Marshal(om)
		if err != nil {
			return err
		}
		oEnt.Version++
		oEnt.Payload = b
		st.Entities[key] = oEnt
		return nil
	})
}

// RecordStockMovement records a local inventory movement and updates ingredient current_stock immediately.
func (s *Store) RecordStockMovement(ctx context.Context, ingredientID, txType, qtyStr, unitCostStr, note string) (string, error) {
	qtyVal, err := strconv.ParseFloat(strings.TrimSpace(qtyStr), 64)
	if err != nil || qtyVal == 0 {
		return "", errors.New("مقدار وارد شده برای گردش انبار معتبر نیست")
	}
	movID, err := UUID()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC().Format(time.RFC3339)

	err = s.withTx(ctx, func(st *persistedDB) error {
		ingKey := entityKey("ingredient", ingredientID)
		ingEnt, ok := st.Entities[ingKey]
		if !ok || ingEnt.Deleted {
			return errors.New("کالای انبار انتخاب شده یافت نشد")
		}
		var im map[string]any
		if err := json.Unmarshal(ingEnt.Payload, &im); err != nil {
			return err
		}
		ingView := Entity{ID: ingEnt.ID, Type: ingEnt.Type, Version: ingEnt.Version, Payload: ingEnt.Payload}
		curStock, _ := strconv.ParseFloat(ingView.Field("current_stock"), 64)

		absQty := qtyVal
		if absQty < 0 {
			absQty = -absQty
		}
		signedDelta := absQty
		switch txType {
		case "consumption", "waste", "return_to_supplier", "transfer_out", "outgoing":
			signedDelta = -absQty
			if txType == "outgoing" {
				// دیالوگ وب برای مصرفِ بیشتر از موجودی، موجودی را تا صفر پایین می‌آورد.
				if curStock <= 0 {
					signedDelta = 0
				} else if absQty > curStock {
					signedDelta = -curStock
				}
				txType = "consumption"
			}
		case "purchase", "opening_balance", "transfer_in", "incoming":
			signedDelta = absQty
			if txType == "incoming" {
				txType = "purchase"
			}
		case "adjustment":
			signedDelta = qtyVal
		case "adjustment_target":
			// در سناریوی «شمارش کردم و عدد فرق داشت» مقدار ورودی موجودی نهایی
			// است، نه دلتا؛ اختلاف با موجودی فعلی به‌عنوان گردش اصلاحی ثبت می‌شود.
			signedDelta = qtyVal - curStock
			txType = "adjustment"
		}
		newStock := curStock + signedDelta
		im["current_stock"] = strconv.FormatFloat(newStock, 'f', 2, 64)
		if strings.TrimSpace(unitCostStr) != "" {
			// بهای تمام‌شده انبار «میانگین موزون متحرک» است (مانند وب):
			// در ورود کالا میانگین با موجودی قبلی ترکیب می‌شود، نه اینکه فی آخرین خرید جایگزین آن شود.
			if unitCost, cErr := strconv.ParseFloat(strings.TrimSpace(unitCostStr), 64); cErr == nil && unitCost >= 0 {
				if signedDelta > 0 {
					oldAvg, _ := strconv.ParseFloat(ingView.Field("average_cost"), 64)
					switch {
					case curStock > 0 && newStock > 0:
						im["average_cost"] = strconv.FormatFloat((curStock*oldAvg+signedDelta*unitCost)/newStock, 'f', 2, 64)
					case newStock > 0:
						im["average_cost"] = strconv.FormatFloat(unitCost, 'f', 2, 64)
					}
					if txType == "purchase" {
						im["last_purchase_cost"] = strconv.FormatFloat(unitCost, 'f', 2, 64)
					}
				}
			}
		}
		im["updated_at"] = now
		updatedIng, err := json.Marshal(im)
		if err != nil {
			return err
		}
		ingEnt.Version++
		ingEnt.Payload = updatedIng
		st.Entities[ingKey] = ingEnt

		movPayload, _ := json.Marshal(map[string]any{
			"id":               movID,
			"title":            ingView.Label(),
			"ingredient_id":    ingredientID,
			"ingredient_name":  ingView.Label(),
			"unit":             ingView.Field("base_unit"),
			"transaction_type": txType,
			"quantity":         strconv.FormatFloat(signedDelta, 'f', 2, 64),
			"stock_after":      strconv.FormatFloat(newStock, 'f', 2, 64),
			"unit_cost":        strings.TrimSpace(unitCostStr),
			"description":      strings.TrimSpace(note),
			"created_at":       now,
			"version":          1,
		})
		st.Entities[entityKey("inventory_movement", movID)] = storedEntity{
			ID:      movID,
			Type:    "inventory_movement",
			Version: 1,
			Payload: movPayload,
		}

		if txType == "waste" {
			// note برای ضایعات می‌تواند «reason::description» باشد: دلیل با کد
			// استاندارد وب جدا از یادداشت آزاد ذخیره می‌شود.
			reason := strings.TrimSpace(note)
			desc := ""
			if parts := strings.SplitN(note, "::", 2); len(parts) == 2 {
				reason = strings.TrimSpace(parts[0])
				desc = strings.TrimSpace(parts[1])
			}
			wasteID, _ := UUID()
			wastePayload, _ := json.Marshal(map[string]any{
				"id":              wasteID,
				"title":           "ضایعات " + ingView.Label(),
				"ingredient_id":   ingredientID,
				"ingredient_name": ingView.Label(),
				"quantity":        strconv.FormatFloat(absQty, 'f', 2, 64),
				"unit":            ingView.Field("base_unit"),
				"reason":          reason,
				"description":     desc,
				"created_at":      now,
				"version":         1,
			})
			st.Entities[entityKey("waste_event", wasteID)] = storedEntity{
				ID:      wasteID,
				Type:    "waste_event",
				Version: 1,
				Payload: wastePayload,
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return movID, nil
}

// VoidWasteEvent رویداد ضایعات را ابطال می‌کند: وضعیت رویداد «ابطال‌شده»
// می‌شود و مقدار ضایع‌شده با یک سند اصلاحی به موجودی برمی‌گردد (وب:
// voidWasteEvent). ابطال دوباره خطا می‌دهد تا موجودی دوبار برنگردد.
func (s *Store) VoidWasteEvent(ctx context.Context, wasteID string) error {
	var ingredientID, qty string
	err := s.withTx(ctx, func(st *persistedDB) error {
		key := entityKey("waste_event", wasteID)
		ent, ok := st.Entities[key]
		if !ok || ent.Deleted {
			return errors.New("رویداد ضایعات یافت نشد")
		}
		var m map[string]any
		if err := json.Unmarshal(ent.Payload, &m); err != nil {
			return err
		}
		if m["status"] == "voided" {
			return errors.New("این رویداد ضایعات قبلاً ابطال شده است")
		}
		m["status"] = "voided"
		m["voided_at"] = time.Now().UTC().Format(time.RFC3339)
		payload, mErr := json.Marshal(m)
		if mErr != nil {
			return mErr
		}
		ent.Payload = payload
		ent.Version++
		st.Entities[key] = ent
		ingredientID, _ = m["ingredient_id"].(string)
		qty, _ = m["quantity"].(string)
		return nil
	})
	if err != nil {
		return err
	}
	if ingredientID != "" && qty != "" && qty != "0" {
		if _, rErr := s.RecordStockMovement(ctx, ingredientID, "adjustment", qty, "", "ابطال ضایعات"); rErr != nil {
			return rErr
		}
	}
	return nil
}

// SettleDebtorPayment records a partial or full payment on a debtor account.
func (s *Store) SettleDebtorPayment(ctx context.Context, debtorID, amountIRR string) error {
	return s.SettleDebtorPaymentWithNote(ctx, debtorID, amountIRR, "")
}

// SettleDebtorPaymentWithNote applies a payment and writes an immutable
// debtor_payment ledger row so the "وصول این ماه" figure on the debtors page
// comes from real receipts instead of guessing from the updated_at timestamp.
func (s *Store) SettleDebtorPaymentWithNote(ctx context.Context, debtorID, amountIRR, note string) error {
	payBig := parseBigIntSafe(amountIRR)
	if payBig.Cmp(big.NewInt(0)) <= 0 {
		return errors.New("مبلغ تسویه باید بزرگ‌تر از صفر باشد")
	}
	return s.withTx(ctx, func(st *persistedDB) error {
		key := entityKey("debtor", debtorID)
		dEnt, ok := st.Entities[key]
		if !ok || dEnt.Deleted {
			return errors.New("حساب دفتری مورد نظر یافت نشد")
		}
		var dm map[string]any
		if err := json.Unmarshal(dEnt.Payload, &dm); err != nil {
			return err
		}
		ent := Entity{ID: dEnt.ID, Type: dEnt.Type, Version: dEnt.Version, Payload: dEnt.Payload}
		remBig := parseBigIntSafe(ent.Field("remaining"))
		paidBig := parseBigIntSafe(ent.Field("paid"))
		if remBig.Sign() <= 0 {
			return errors.New("این حساب قبلاً تسویه شده است")
		}
		applied := new(big.Int).Set(payBig)
		if payBig.Cmp(remBig) >= 0 {
			applied = new(big.Int).Set(remBig)
			paidBig.Add(paidBig, remBig)
			remBig = big.NewInt(0)
			dm["status"] = "paid"
		} else {
			paidBig.Add(paidBig, payBig)
			remBig.Sub(remBig, payBig)
			dm["status"] = "partial"
		}
		now := time.Now().UTC().Format(time.RFC3339)
		dm["paid"] = paidBig.String()
		dm["remaining"] = remBig.String()
		dm["updated_at"] = now
		b, err := json.Marshal(dm)
		if err != nil {
			return err
		}
		dEnt.Version++
		dEnt.Payload = b
		st.Entities[key] = dEnt

		payID, uErr := UUID()
		if uErr != nil {
			return uErr
		}
		payPayload, mErr := json.Marshal(map[string]any{
			"id":         payID,
			"debtor_id":  debtorID,
			"amount_irr": applied.String(),
			"note":       strings.TrimSpace(note),
			"created_at": now,
			"version":    1,
		})
		if mErr != nil {
			return mErr
		}
		st.Entities[entityKey("debtor_payment", payID)] = storedEntity{ID: payID, Type: "debtor_payment", Version: 1, Payload: payPayload}
		return nil
	})
}

func (e Entity) RecipeLines() []RecipeLineView {
	var m struct {
		Items []RecipeLineView `json:"items"`
	}
	if json.Unmarshal(e.Payload, &m) != nil {
		return nil
	}
	return m.Items
}

func (s *Store) Analytics(ctx context.Context) FinancialAnalytics {
	return s.analyticsRange(time.Time{})
}

// AnalyticsSince همان محاسبه Analytics است ولی فروش و هزینه‌ها فقط از
// زمان داده‌شده به بعد شمرده می‌شوند (گزارش دوره‌ای روزانه/هفتگی/ماهانه).
// مطالبات، میزها و وظایف همیشه وضعیت فعلی‌اند.
func (s *Store) AnalyticsSince(ctx context.Context, from time.Time) FinancialAnalytics {
	return s.analyticsRange(from)
}

func (s *Store) analyticsRange(from time.Time) FinancialAnalytics {
	s.mu.Lock()
	defer s.mu.Unlock()

	totalSales := big.NewInt(0)
	cashSales := big.NewInt(0)
	cardSales := big.NewInt(0)
	creditSales := big.NewInt(0)
	totalExpenses := big.NewInt(0)
	totalReceivables := big.NewInt(0)

	var fa FinancialAnalytics
	for _, e := range s.state.Entities {
		if e.Deleted {
			continue
		}
		ent := Entity{ID: e.ID, Type: e.Type, Version: e.Version, Payload: e.Payload}
		inRange := true
		if !from.IsZero() && (e.Type == "order" || e.Type == "expense") {
			ct, pErr := time.Parse(time.RFC3339, ent.Field("created_at"))
			inRange = pErr == nil && !ct.Before(from)
		}
		if !inRange {
			continue
		}
		switch e.Type {
		case "order":
			status := strings.ToLower(ent.Field("status"))
			if status == "cancelled" || status == "canceled" {
				fa.CancelledOrders++
				continue
			}
			if status == "completed" {
				fa.CompletedOrders++
			} else {
				fa.OpenOrders++
			}
			// فقط فاکتور تکمیل‌شده در فروش و سود روز شمرده می‌شود؛
			// سفارش باز/در حال آماده‌سازی هنوز تسویه نشده است.
			if status == "completed" {
				amt := parseBigIntSafe(ent.PriceIRR())
				totalSales.Add(totalSales, amt)
				switch ent.Field("payment_method") {
				case "cash":
					cashSales.Add(cashSales, amt)
				case "credit_debtor":
					creditSales.Add(creditSales, amt)
				default:
					cardSales.Add(cardSales, amt)
				}
			}
		case "expense":
			totalExpenses.Add(totalExpenses, parseBigIntSafe(ent.PriceIRR()))
		case "debtor":
			totalReceivables.Add(totalReceivables, parseBigIntSafe(ent.Field("remaining")))
		case "table":
			fa.TotalTables++
			if ent.Field("status") == "occupied" || ent.Field("status") == "reserved" {
				fa.OccupiedTables++
			}
		case "task":
			if ent.Field("is_done") == "true" || ent.Field("status") == "done" {
				fa.CompletedTasks++
			} else {
				fa.PendingTasks++
			}
		case "staff":
			fa.ActiveStaff++
		}
	}
	netOp := new(big.Int).Sub(totalSales, totalExpenses)
	fa.TotalSalesIRR = totalSales.String()
	fa.CashSalesIRR = cashSales.String()
	fa.CardSalesIRR = cardSales.String()
	fa.CreditSalesIRR = creditSales.String()
	fa.TotalExpensesIRR = totalExpenses.String()
	fa.TotalReceivablesIRR = totalReceivables.String()
	fa.NetOperatingIRR = netOp.String()
	return fa
}

func parseBigIntSafe(raw string) *big.Int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return big.NewInt(0)
	}
	parts := strings.SplitN(raw, ".", 2)
	bi := new(big.Int)
	if _, ok := bi.SetString(parts[0], 10); !ok {
		return big.NewInt(0)
	}
	return bi
}

// SeedIranianCafeData populates a rich, authentic Iranian café dataset into the local store.
func (s *Store) SeedIranianCafeData(ctx context.Context) error {
	return s.withTx(ctx, func(st *persistedDB) error {
		now := time.Now().UTC().Format(time.RFC3339)
		put := func(kind, id string, m map[string]any) {
			m["id"] = id
			m["version"] = 1
			if m["created_at"] == nil {
				m["created_at"] = now
			}
			raw, _ := json.Marshal(m)
			st.Entities[entityKey(kind, id)] = storedEntity{
				ID:      id,
				Type:    kind,
				Version: 1,
				Payload: raw,
			}
		}

		// 1. Cafe Profile
		put("cafe_profile", "cafe-main-profile", map[string]any{
			"title":         "کافه نمونه — شعبه مرکزی",
			"name":          "کافه نمونه",
			"phone":         "۰۲۱-۸۸۹۹۴۴۱۱",
			"address":       "تهران، خیابان ولیعصر، بالاتر از پارک ساعی، نبش کوچه دهم، پلاک ۱۴۲",
			"tax_percent":   "9",
			"currency":      "IRR",
			"receipt_note":  "از حضور گرم شما سپاسگزاریم",
			"working_hours": "هر روز از ساعت ۰۷:۳۰ صبح تا ۲۳:۳۰ شب",
		})

		// 2. Categories
		catEspresso := "11111111-1111-4111-8111-000000000001"
		catCold := "11111111-1111-4111-8111-000000000002"
		catBrew := "11111111-1111-4111-8111-000000000003"
		catCake := "11111111-1111-4111-8111-000000000004"
		catTea := "11111111-1111-4111-8111-000000000005"

		put("category", catEspresso, map[string]any{"title": "بار گرم و اسپرسو تخصصی", "slug": "espresso-bar", "description": "انواع اسپرسو، لاته، کاپوچینو و نوشیدنی‌های پایه اسپرسو با دانه ۱۰۰٪ عربیکا"})
		put("category", catCold, map[string]any{"title": "بار سرد، آیس و ماچا", "slug": "cold-bar", "description": "نوشیدنی‌های خنک، آیس لاته، کلدبرو و ماچا لاته"})
		put("category", catBrew, map[string]any{"title": "قهوه دمی و موج سوم", "slug": "brew-bar", "description": "وی۶۰ (V60)، کمکس، ایروپرس و قهوه ترک تخصصی"})
		put("category", catCake, map[string]any{"title": "کیک روز، کروسان و دسر", "slug": "bakery-dessert", "description": "پخت روزانه کیک زعفرانی، چیزکیک سن‌سباستین و کروسان کره‌ای"})
		put("category", catTea, map[string]any{"title": "دمنوش‌های ایرانی و چای", "slug": "herbal-tea", "description": "دمنوش‌های طبیعی بهارنارنج، زعفران، گل‌گاوزبان و چای لاهیجان"})

		// 3. Products (Prices in IRR: 1 Toman = 10 IRR)
		p1 := "22222222-2222-4222-8222-000000000001"
		p2 := "22222222-2222-4222-8222-000000000002"
		p3 := "22222222-2222-4222-8222-000000000003"
		p4 := "22222222-2222-4222-8222-000000000004"
		p5 := "22222222-2222-4222-8222-000000000005"
		p6 := "22222222-2222-4222-8222-000000000006"
		p7 := "22222222-2222-4222-8222-000000000007"
		p8 := "22222222-2222-4222-8222-000000000008"

		put("product", p1, map[string]any{"title": "اسپرسو دبل تخصصی (اتیوپی)", "category_id": catEspresso, "price": "950000", "cost": "280000", "is_available": true, "description": "عصاره‌گیری دبل شات از دانه عربیکا اتیوپی یرگاشف"})
		put("product", p2, map[string]any{"title": "کافه لاته فندق", "category_id": catEspresso, "price": "1450000", "cost": "450000", "is_available": true, "description": "دبل اسپرسو، شیر بخاردهی شده ابریشمی و سیروپ فندق اورجینال"})
		put("product", p3, map[string]any{"title": "کاپوچینو کلاسیک ایتالیایی", "category_id": catEspresso, "price": "1300000", "cost": "390000", "is_available": true, "description": "فوم شیر غلیظ و اسپرسو تخصصی"})
		put("product", p4, map[string]any{"title": "آیس کارامل ماکیاتو", "category_id": catCold, "price": "1550000", "cost": "480000", "is_available": true, "description": "سرو سرد با یخ بلوری، شیر تازه، کارامل دست‌ساز و اسپرسو"})
		put("product", p5, map[string]any{"title": "ماچا لاته تشریفاتی (امپریال)", "category_id": catCold, "price": "1750000", "cost": "620000", "is_available": true, "description": "پودر ماچای درجه یک ژاپنی با شیر بادام یا شیر تازه"})
		put("product", p6, map[string]any{"title": "قهوه دمی V60 (کنیا AA)", "category_id": catBrew, "price": "1650000", "cost": "520000", "is_available": true, "description": "دم‌آوری دستی با فیلتر کاغذی، طعم‌یاد توت سیاه و مرکبات"})
		put("product", p7, map[string]any{"title": "چیزکیک سن‌سباستین با سس شکلات بلژیکی", "category_id": catCake, "price": "1850000", "cost": "680000", "is_available": true, "description": "پخت روز کافه با بافت کرمی و سس شکلات داغ"})
		put("product", p8, map[string]any{"title": "کیک زعفرانی و پسته ایرانی", "category_id": catCake, "price": "1400000", "cost": "420000", "is_available": true, "description": "کیک خانگی با زعفران قائنات، گلاب کاشان و مغز پسته کرمان"})

		// 4. Ingredients
		i1 := "33333333-3333-4333-8333-000000000001"
		i2 := "33333333-3333-4333-8333-000000000002"
		i3 := "33333333-3333-4333-8333-000000000003"
		i4 := "33333333-3333-4333-8333-000000000004"
		i5 := "33333333-3333-4333-8333-000000000005"
		i6 := "33333333-3333-4333-8333-000000000006"

		put("ingredient", i1, map[string]any{"name": "دانه قهوه عربیکا اتیوپی", "sku": "COF-ETH-01", "base_unit": "g", "current_stock": "4250", "minimum_stock": "1500", "reorder_point": "2000", "average_cost": "14500"})
		put("ingredient", i2, map[string]any{"name": "شیر تازه پرچرب باریستا", "sku": "MLK-BAR-01", "base_unit": "ml", "current_stock": "12500", "minimum_stock": "5000", "reorder_point": "8000", "average_cost": "480"})
		put("ingredient", i3, map[string]any{"name": "سیروپ فندق مونین", "sku": "SYR-HAZ-01", "base_unit": "ml", "current_stock": "650", "minimum_stock": "700", "reorder_point": "1000", "average_cost": "8500"})
		put("ingredient", i4, map[string]any{"name": "پودر ماچا تشریفاتی ژاپن", "sku": "MTC-JPN-01", "base_unit": "g", "current_stock": "180", "minimum_stock": "200", "reorder_point": "300", "average_cost": "95000"})
		put("ingredient", i5, map[string]any{"name": "زعفران سرگل قائنات", "sku": "SAF-QAN-01", "base_unit": "g", "current_stock": "45", "minimum_stock": "15", "reorder_point": "25", "average_cost": "1150000"})
		put("ingredient", i6, map[string]any{"name": "لیوان بیرون‌بر دوجداره کرافت", "sku": "CUP-8OZ-01", "base_unit": "pcs", "current_stock": "340", "minimum_stock": "100", "reorder_point": "150", "average_cost": "45000"})

		// 5. Recipes (BOM)
		put("recipe", "44444444-4444-4444-8444-000000000001", map[string]any{
			"title":        "رسپی اسپرسو دبل تخصصی",
			"product_id":   p1,
			"product_name": "اسپرسو دبل تخصصی (اتیوپی)",
			"yield_qty":    "1",
			"note":         "عصاره‌گیری ۱۸ گرم سبد دبل در ۲۸ ثانیه با دمای ۹۳ درجه",
			"items": []map[string]any{
				{"ingredient_id": i1, "ingredient_name": "دانه قهوه عربیکا اتیوپی", "quantity": "18.5", "unit": "g"},
			},
		})
		put("recipe", "44444444-4444-4444-8444-000000000002", map[string]any{
			"title":        "رسپی کافه لاته فندق",
			"product_id":   p2,
			"product_name": "کافه لاته فندق",
			"yield_qty":    "1",
			"note":         "دبل شات اسپرسو + ۲۲۰ میلی‌لیتر شیر فوم‌گیری‌شده + ۲۰ میلی‌لیتر سیروپ فندق",
			"items": []map[string]any{
				{"ingredient_id": i1, "ingredient_name": "دانه قهوه عربیکا اتیوپی", "quantity": "18.5", "unit": "g"},
				{"ingredient_id": i2, "ingredient_name": "شیر تازه پرچرب باریستا", "quantity": "220", "unit": "ml"},
				{"ingredient_id": i3, "ingredient_name": "سیروپ فندق مونین", "quantity": "20", "unit": "ml"},
			},
		})

		// 6. Tables & Reservations
		t1 := "55555555-5555-4555-8555-000000000001"
		t2 := "55555555-5555-4555-8555-000000000002"
		t3 := "55555555-5555-4555-8555-000000000003"
		t4 := "55555555-5555-4555-8555-000000000004"
		t5 := "55555555-5555-4555-8555-000000000005"
		t6 := "55555555-5555-4555-8555-000000000006"

		put("table", t1, map[string]any{"number": "۱", "title": "میز ۱ — کنار پنجره ولیعصر", "zone": "سالن اصلی", "capacity": "2", "status": "occupied"})
		put("table", t2, map[string]any{"number": "۲", "title": "میز ۲ — سالن اصلی", "zone": "سالن اصلی", "capacity": "4", "status": "available"})
		put("table", t3, map[string]any{"number": "۳", "title": "میز ۳ — سالن اصلی", "zone": "سالن اصلی", "capacity": "4", "status": "available"})
		put("table", t4, map[string]any{"number": "۴", "title": "میز ۴ — تراس سبز", "zone": "تراس", "capacity": "2", "status": "reserved"})
		put("table", t5, map[string]any{"number": "۵", "title": "میز ۵ — تراس سبز", "zone": "تراس", "capacity": "4", "status": "available"})
		put("table", t6, map[string]any{"number": "۶", "title": "میز ۶ — میز بزرگ گروهی (VIP)", "zone": "بخش VIP", "capacity": "8", "status": "available"})

		put("reservation", "55555555-5555-4555-8555-000000000099", map[string]any{
			"title":        "رزرو میز ۴ تراس — سرکار خانم تابش",
			"guest_name":   "سارا تابش",
			"guest_phone":  "09123456789",
			"table_label":  "میز ۴ — تراس سبز",
			"guests_count": "2",
			"reserve_time": "ساعت ۱۸:۳۰ امروز",
			"note":         "جشن سالگرد ازدواج — گل روی میز",
			"status":       "confirmed",
		})

		// 7. Customers & Debtors
		c1 := "66666666-6666-4666-8666-000000000001"
		c2 := "66666666-6666-4666-8666-000000000002"
		c3 := "66666666-6666-4666-8666-000000000003"
		put("customer", c1, map[string]any{"full_name": "امیرحسین رضایی", "phone": "09121112233", "tier": "طلایی (VIP)", "loyalty_points": "48", "orders_count": "24", "note": "اسپرسو بدون شکر، شیر کم‌چرب"})
		put("customer", c2, map[string]any{"full_name": "مهسا کریمی", "phone": "09124445566", "tier": "نقره‌ای", "loyalty_points": "19", "orders_count": "11", "note": "علاقه‌مند به قهوه دمی V60"})
		put("customer", c3, map[string]any{"full_name": "دکتر فرهاد نادری (دفتر معماری مجاور)", "phone": "09127778899", "tier": "طلایی (حساب شرکتی)", "loyalty_points": "85", "orders_count": "42", "note": "سفارش‌های جلسات دفتر معماری"})

		put("debtor", "77777777-7777-4777-8777-000000000001", map[string]any{
			"full_name":   "دکتر فرهاد نادری (دفتر معماری مجاور)",
			"phone":       "09127778899",
			"total":       "14500000",
			"paid":        "5000000",
			"remaining":   "9500000",
			"currency":    "IRR",
			"status":      "partial",
			"description": "پذیرایی جلسات هفتگی دفتر معماری (تسویه پایان ماه)",
		})

		// 8. Staff & Daily Checklist Tasks & Expenses
		put("staff", "88888888-8888-4888-8888-000000000001", map[string]any{"full_name": "آرش کاظمی", "role": "سرپرست بار و باریستا ارشد", "phone": "09120001122", "shift": "شیفت صبح (۰۷:۳۰ تا ۱۵:۳۰)", "salary_irr": "220000000", "status": "حاضر در شیفت"})
		put("staff", "88888888-8888-4888-8888-000000000002", map[string]any{"full_name": "نگار محمدی", "role": "صندوقدار و مسئول پذیرش", "phone": "09120003344", "shift": "شیفت صبح (۰۸:۰۰ تا ۱۶:۰۰)", "salary_irr": "180000000", "status": "حاضر در شیفت"})
		put("staff", "88888888-8888-4888-8888-000000000003", map[string]any{"full_name": "کیوان راد", "role": "باریستا بار گرم و دمی", "phone": "09120005566", "shift": "شیفت عصر (۱۵:۳۰ تا ۲۳:۳۰)", "salary_irr": "195000000", "status": "شیفت عصر"})

		put("task", "88888888-8888-4888-8888-000000000011", map[string]any{"title": "کالیبراسیون آسیاب اسپرسو و تست عصاره‌گیری اول صبح", "assignee": "آرش کاظمی", "priority": "بالا", "status": "done", "is_done": "true"})
		put("task", "88888888-8888-4888-8888-000000000012", map[string]any{"title": "کنترل موجودی شیر تازه و ثبت سفارش سیروپ فندق", "assignee": "نگار محمدی", "priority": "متوسط", "status": "pending", "is_done": "false"})
		put("task", "88888888-8888-4888-8888-000000000013", map[string]any{"title": "بک‌واش (Backflush) هدگروپ‌های اسپرسوماشین با پودر کافیزا", "assignee": "کیوان راد", "priority": "بالا", "status": "pending", "is_done": "false"})

		put("expense", "88888888-8888-4888-8888-000000000021", map[string]any{"title": "خرید شیر تازه روزانه و خامه قنادی (۲۰ بطری)", "category": "خرید مواد اولیه روزانه", "price": "6800000", "payment_method": "کارت شرکتی", "note": "فاکتور لبنیات صباح"})
		put("expense", "88888888-8888-4888-8888-000000000022", map[string]any{"title": "سرویس دوره‌ای تصفیه آب اسپرسوماشین", "category": "تعمیر و نگهداری تجهیزات", "price": "4500000", "payment_method": "کارت به کارت", "note": "تعویض فیلتر رزینی"})

		// 9. Sample Orders
		o1 := "99999999-9999-4999-8999-000000000001"
		o2 := "99999999-9999-4999-8999-000000000002"
		put("order", o1, map[string]any{
			"order_number":     "101",
			"fulfillment_type": "dine_in",
			"table_id":         t1,
			"table_label":      "میز ۱ — کنار پنجره ولیعصر",
			"customer_id":      c1,
			"customer_name":    "امیرحسین رضایی",
			"customer_phone":   "09121112233",
			"payment_method":   "card_pos",
			"subtotal_irr":     "3300000",
			"discount_irr":     "0",
			"total_irr":        "3300000",
			"price":            "3300000",
			"status":           "preparing",
			"note":             "لاته کم‌شیرین سرو شود",
			"items": []map[string]any{
				{"id": "l-1", "product_id": p2, "product_name_snapshot": "کافه لاته فندق", "quantity": 1, "unit_price_irr": "1450000", "line_total_irr": "1450000"},
				{"id": "l-2", "product_id": p7, "product_name_snapshot": "چیزکیک سن‌سباستین با سس شکلات بلژیکی", "quantity": 1, "unit_price_irr": "1850000", "line_total_irr": "1850000"},
			},
		})
		put("order", o2, map[string]any{
			"order_number":     "102",
			"fulfillment_type": "outside",
			"table_label":      "بیرون‌بر (Takeaway)",
			"customer_id":      c2,
			"customer_name":    "مهسا کریمی",
			"customer_phone":   "09124445566",
			"payment_method":   "cash",
			"subtotal_irr":     "3050000",
			"discount_irr":     "150000",
			"total_irr":        "2900000",
			"price":            "2900000",
			"status":           "completed",
			"note":             "بسته‌بندی بیرون‌بر دوجداره",
			"items": []map[string]any{
				{"id": "l-3", "product_id": p6, "product_name_snapshot": "قهوه دمی V60 (کنیا AA)", "quantity": 1, "unit_price_irr": "1650000", "line_total_irr": "1650000"},
				{"id": "l-4", "product_id": p8, "product_name_snapshot": "کیک زعفرانی و پسته ایرانی", "quantity": 1, "unit_price_irr": "1400000", "line_total_irr": "1400000"},
			},
		})

		// 10. Thermal Printers & Category Print Routes
		put("printer", "99999999-9999-4999-8999-000000000091", map[string]any{"title": "چاپگر حرارتی صندوق (فیش مشتری)", "model": "Epson TM-T20III (80mm)", "connection": "USB / LAN 192.168.1.201:9100", "paper_width": "80mm", "station": "صندوق فروش", "is_active": "true"})
		put("printer", "99999999-9999-4999-8999-000000000092", map[string]any{"title": "چاپگر حرارتی بار گرم و سرد", "model": "Bixolon SRP-330II (80mm)", "connection": "LAN 192.168.1.202:9100", "paper_width": "80mm", "station": "بار گرم و سرد", "is_active": "true"})

		put("printer_route", "99999999-9999-4999-8999-000000000095", map[string]any{"title": "بار گرم و اسپرسو ← چاپگر حرارتی بار گرم و سرد", "category_name": "بار گرم و اسپرسو تخصصی", "printer_name": "چاپگر حرارتی بار گرم و سرد (Bixolon)", "copies": "1", "receipt_type": "حواله آماده‌سازی باریستا"})
		put("printer_route", "99999999-9999-4999-8999-000000000096", map[string]any{"title": "کلیه فاکتورهای تسویه ← چاپگر حرارتی صندوق", "category_name": "همه دسته‌بندی‌ها", "printer_name": "چاپگر حرارتی صندوق (Epson TM-T20III)", "copies": "1", "receipt_type": "فیش تسویه مشتری + باشگاه مشتریان"})

		// 11. Modifier Groups (افزودنی‌ها و سفارشی‌سازی محصول)
		put("modifier_group", "aaaaaaaa-1111-4111-8111-000000000001", map[string]any{"title": "انتخاب نوع شیر", "options": "شیر تازه پرچرب (پیش‌فرض) • شیر کم‌چرب • شیر بادام (+۲۵,۰۰۰ تومان) • شیر جو دوسر (+۳۰,۰۰۰ تومان)", "price": "250000", "is_required": "اختیاری"})
		put("modifier_group", "aaaaaaaa-1111-4111-8111-000000000002", map[string]any{"title": "شات و سیروپ اضافه", "options": "شات اضافه عربیکا (+۳۵,۰۰۰ تومان) • سیروپ فندق/کارامل (+۲۰,۰۰۰ تومان) • دی‌کف (بدون کافئین)", "price": "350000", "is_required": "اختیاری"})

		// 12. Suppliers & Purchase Invoices (تامین‌کنندگان و فاکتورهای خرید انبار)
		put("supplier", "bbbbbbbb-1111-4111-8111-000000000001", map[string]any{"title": "برشته‌کاری قهوه تخصصی دارک‌رست تهران", "contact_name": "مهندس سهرابی", "phone": "02188776655", "items_supplied": "دانه قهوه عربیکا اتیوپی، کنیا و کلمبیا", "balance_irr": "0"})
		put("supplier", "bbbbbbbb-1111-4111-8111-000000000002", map[string]any{"title": "پخش لبنیات و خامه باریستا صباح", "contact_name": "آقای حیدری", "phone": "02144556677", "items_supplied": "شیر پرچرب باریستا، خامه قنادی و کره", "balance_irr": "6800000"})

		put("purchase", "bbbbbbbb-1111-4111-8111-000000000011", map[string]any{"title": "فاکتور خرید #P-401 — دانه قهوه عربیکا (۵ کیلوگرم)", "supplier_name": "برشته‌کاری قهوه تخصصی دارک‌رست تهران", "price": "72500000", "status": "تسویه شده", "note": "تحویل انبار مرکزی کافه"})

		// 13. Customer Retention Campaigns & Cafe Membership Plans
		put("retention_campaign", "cccccccc-1111-4111-8111-000000000001", map[string]any{"title": "هدیه سالروز تولد اعضای باشگاه مشتریان (۲۰٪ تخفیف + کیک روز)", "segment": "اعضای وفادار (طلایی و نقره‌ای)", "channel": "پیامک خودکار + کیف پول", "discount_percent": "20", "status": "فعال"})
		put("cafe_plan", "cccccccc-1111-4111-8111-000000000011", map[string]any{"title": "اشتراک ماهانه قهوه صبحگاهی (۲۰ فنجان اسپرسو یا آمریکانو)", "price": "15500000", "duration_days": "30", "subscribers_count": "18", "description": "مخصوص کارمندان و دفاتر اطراف خیابان ولیعصر با ۲۰٪ صرفه‌جویی"})

		// 14. Accounting Ledger Entries & Job Postings
		put("accounting_entry", "dddddddd-1111-4111-8111-000000000001", map[string]any{"title": "سند روزنامه #۱۰۱ — ثبت فروش روزانه صندوق و واریز کارتخوان", "debit_account": "بانک ملت - پایانه فروشگاهی (۱۱۰۲)", "credit_account": "درآمد فروش نوشیدنی و کیک (۴۱۰۱)", "price": "6200000", "note": "تراز خودکار پایان شیفت"})
		put("job_posting", "eeeeeeee-1111-4111-8111-000000000001", map[string]any{"title": "استخدام باریستا مسلط به بار گرم و لاته آرت (شیفت عصر)", "shift": "۱۵:۳۰ الی ۲۳:۳۰", "salary_range": "۲۰ تا ۲۵ میلیون تومان + بیمه و پاداش", "applicants_count": "6", "status": "در حال دریافت رزومه"})

		return nil
	})
}

// ProductRecipeCostIRR بهای تمام‌شده یک واحد محصول را از روی رسپی (BOM)
// و average_cost مواد اولیه حساب می‌کند: Σ(مقدار مصرف × بهای واحد ماده).
// مقدار دوم false یعنی محصول رسپی ندارد (بهای دستی محصول ملاک است).
func (s *Store) ProductRecipeCostIRR(ctx context.Context, productID string) (string, bool, error) {
	recipes, err := s.Entities(ctx, "recipe")
	if err != nil {
		return "", false, err
	}
	for _, r := range recipes {
		if r.Field("product_id") != productID {
			continue
		}
		total := new(big.Rat)
		foundLine := false
		for _, line := range r.RecipeLines() {
			ing, iErr := s.Entity(ctx, "ingredient", line.IngredientID)
			if iErr != nil {
				continue
			}
			qty, ok := new(big.Rat).SetString(strings.TrimSpace(line.Quantity))
			if !ok {
				continue
			}
			cost, ok := new(big.Rat).SetString(strings.TrimSpace(ing.Field("average_cost")))
			if !ok {
				continue
			}
			total.Add(total, new(big.Rat).Mul(qty, cost))
			foundLine = true
		}
		if !foundLine {
			return "", false, nil
		}
		// گرد به نزدیک‌ترین ریال (IRR واحد پایه دفتر است)
		rounded := new(big.Float).SetRat(total)
		rounded.Add(rounded, big.NewFloat(0.5))
		asInt, _ := rounded.Int(nil)
		return asInt.String(), true, nil
	}
	return "", false, nil
}
