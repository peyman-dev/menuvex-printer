// Package desktop implements the standalone offline-first Iranian Café & Restaurant
// Management Desktop Application (MenuVex / منووکس).
package desktop

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"math"
	"math/big"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sinaqaffari/coffish/desktop/internal/printer"
	"github.com/sinaqaffari/coffish/desktop/internal/store"
	"github.com/sinaqaffari/coffish/desktop/internal/syncclient"
)

//go:embed ui/* ui/static/* contracts/*.json
var assets embed.FS

type Endpoint struct {
	Method      string          `json:"method"`
	Path        string          `json:"path"`
	Summary     string          `json:"summary"`
	Tags        []string        `json:"tags"`
	OperationID string          `json:"operation_id"`
	Request     json.RawMessage `json:"request_body"`
	Responses   json.RawMessage `json:"responses"`
	Parameters  json.RawMessage `json:"parameters"`
	Security    json.RawMessage `json:"security"`
}

type Application struct {
	store        *store.Store
	uploadDir    string
	sessions     map[string]sessionInfo
	sessionsPath string
	sessionsMu   sync.Mutex
	authDisabled bool // فقط برای تست‌های خودکار
	client       *syncclient.Client
	csrf         string
	templates    *template.Template
	endpoints    []Endpoint
	allowed      map[string]bool

	// Printer subsystem (all fields are safe zero values; New initializes them
	// and tests may replace them with fakes).
	sender    *printer.Sender
	scanSvc   *printer.Scanner
	probeSvc  *printer.Prober
	usbScan   func() ([]printer.USBDevice, error)
	queueScan func(context.Context) ([]string, error)
	netScan   func() ([]printer.NetworkRange, error)

	usbCacheMu  sync.Mutex
	usbCache    []printer.USBDevice
	usbCacheErr string
	usbCacheAt  time.Time
}

type View struct {
	CSRF, Page, Message, Cursor string
	JalaliNow                   string
	Connected                   bool
	PrinterEnabled              bool
	Stats                       store.SummaryStats
	Analytics                   store.FinancialAnalytics
	CafeProfile                 store.Entity
	Entities                    []store.Entity
	Products                    []store.Entity
	Categories                  []store.Entity
	ModifierGroups              []store.Entity
	Ingredients                 []store.Entity
	Recipes                     []store.Entity
	Suppliers                   []store.Entity
	Purchases                   []store.Entity
	Tables                      []store.Entity
	Reservations                []store.Entity
	Customers                   []store.Entity
	RetentionCampaigns          []store.Entity
	CafePlans                   []store.Entity
	Debtors                     []store.Entity
	Expenses                    []store.Entity
	AccountingEntries           []store.Entity
	ShiftCloses                 []store.Entity
	StaffList                   []store.Entity
	Tasks                       []store.Entity
	JobPostings                 []store.Entity
	Movements                   []store.Entity
	WasteEvents                 []store.Entity
	Printers                    []store.Entity
	PrinterRoutes               []store.Entity
	PrintJobs                   []store.PrintJobView
	// مرکز چاپگرها: پروفایل‌های جدید، وضعیت بررسی، اسکن و صف چاپ
	PrinterProfiles     []printer.Profile
	PrinterProfile      printer.Profile
	PrinterDraft        printer.Profile
	PrinterStats        store.PrinterStats
	PrinterUSBDevices   []printer.USBDevice
	PrinterUSBError     string
	PrinterQueues       []string
	PrinterQueueError   string
	PrinterNetworks     []printer.NetworkRange
	PrinterNetworkError string
	PrinterCheck        *printer.ProbeResult
	PrinterScan         *printer.ScanStatus
	PrintJobCounts      store.PrintJobCounts
	PrintJobFilter      string
	PrintQueuePrinter   string
	PrinterRecentJobs   []store.PrintJobView

	PrinterDiscoveryMode string

	POSFormToken        string
	POSGroups           []POSGroup
	// POSStock موجودی قابل تهیهٔ هر محصول (از رسپی) است؛ کارت‌های «ناموجود»
	// صندوق از همین نقشه ساخته می‌شوند و ثبت سفارش هم سمت سرور کنترل می‌شود.
	POSStock map[string]store.ProductPortion
	// POSStockJSON همان نقشه به شکل JSON برای دیالوگ «چه چیزی کم است؟»؛
	// فقط محصول‌هایی که کمبود دارند داخلش می‌آیند.
	POSStockJSON string
	POSTables           []POSTableRow
	POSRecent           []POSRecentRow
	POSLastOrderID      string
	POSLastOrderTotalFa string
	POSEdit             *POSEditState
	POSStages           []POSStageOpt
	POSStageCount       int
	PrintPolicy         PrintPolicy
	CategoryRoutes      []CategoryRouteRow
	POSOpenSplit        string
	POSSplitJSON        template.JS
	ClubTab             string
	ClubSubTab          string
	ClubRows            []ClubCustomerRow
	ClubPayments        []ClubPaymentRow
	ClubStatMembers     int
	ClubStatPending     int
	ClubStatPaid        int
	ClubSearch          string
	ClubGroups          []ClubGroupRow
	ClubGroupOpts       []store.Entity
	ClubOnlyValid       bool
	ClubGroupDetail     *ClubGroupDetail
	ClubGroupsTotal     int
	ClubGroupsValid     int
	ClubGroupsMembers   int
	// دیالوگ «گروه جدید / ویرایش گروه» روی همان صفحهٔ باشگاه مشتریان باز می‌شود.
	ClubGroupsJSON  template.JS
	ClubGroupDialog string // "" | new | edit
	ClubGroupEditID string
	Pricing             []PricingRow
	SmartQuotes         []store.Entity
	// حساب و گزارش (accounting وب): تب فعال و ارقام نمای کلی/سود و زیان
	FinTab           string
	FinCashFa        string
	FinBankFa        string
	FinInvValueFa    string
	FinReceivablesFa string
	FinMonthRevFa    string
	FinMonthCOGSFa   string
	FinMonthExpFa    string
	FinMonthProfitFa string
	DebtorRows       []DebtorRow
	// صفحه «بدهکاران و تسویه» هم‌طراز وب: تب فعال، کارت‌های خلاصه، فیلترها،
	// گزارش گروه‌ها و کاتالوگ محصولات برای انتخاب آیتم‌های بدهی دستی.
	DebtorTab             string
	DebtorCount           int
	DebtorKPIRemainingFa  string
	DebtorKPIOpenFa       string
	DebtorKPIOverdueFa    string
	DebtorKPICollectedFa  string
	DebtorKPIOverdueSumFa string
	DebtorQuery           string
	DebtorStatusFilter    string
	DebtorSourceFilter    string
	DebtorAccountFilter   string
	DebtorSortFilter      string
	DebtorGroupFilter     string
	DebtorFromShamsi      string
	DebtorToShamsi        string
	DebtorFromISO         string
	DebtorToISO           string
	DebtorGroups          []store.Entity
	DebtorGroupRows       []DebtorGroupRow
	DebtorProductsJSON    template.JS
	DebtorRowsJSON        template.JS
	DebtorTodayShamsi     string
	DebtorFiltered        int
	DebtorFilterQuery     string
	TopProducts      []TopProduct
	ProductRows      []ProductRow
	// ضایعات (waste-page وب): ردیف‌های نمایشی + تجمیع دلیل/اقلام
	WasteRows        []WasteRow
	WasteByReason    []WasteAgg
	WasteTopItems    []WasteAgg
	WasteTotalCostFa string
	// دسته‌بندی‌ها (categories وب): ردیف با شمارش محصولات
	CatRows []CategoryRow
	// گزارش‌ها (reports وب): دوره و پرفروش‌ترین‌ها
	ReportsPeriod                       string
	BestSellers                         []BestSellerRow
	ReportProducts                      []ReportProductRow
	ReportOrders                        []ReportOrderRow
	ReportStatusSlices                  []ReportStatusSlice
	ReportStatusTotal                   int
	ReportIncomeChart                   template.HTML
	ReportPeriodLabel                   string
	ReportOrdersCount                   int
	ReportCompletedCount                int
	ReportAverageFa                     string
	ReportPrevIncomeFa                  string
	ReportIncomeGrowthLabel             string
	ReportIncomeGrowthClass             string
	ReportOrdersGrowthLabel             string
	ReportOrdersGrowthClass             string
	ReportAverageGrowthLabel            string
	ReportAverageGrowthClass            string
	ReportYesterdayIncomeFa             string
	ReportAllIncomeIRR                  string
	ReportAllOrdersCount                int
	ReportSoldProductsCount             int
	ProductsTotal                       int
	ProductsInStock                     int
	ProductsLowStock                    int
	ProductsOutOfStock                  int
	ProductsCategory                    string
	ProductsStatus                      string
	InvRows                             []InvRow
	InvTotal                            int
	InvOut                              int
	InvLow                              int
	InvNegative                         int
	InvEndingSoon                       int
	InvValueToman                       string
	InvCategories                       []string
	InvLocations                        []string
	InvCategory                         string
	InvLocation                         string
	InvStatus                           string
	InvSort                             string
	InvAlerts                           []InvRow
	InvMovements                        []store.Entity
	DashTodayLabel                      string
	DashRevTodayFa                      string
	DashAvgTodayFa                      string
	DashOrdersToday                     int
	DashRevGrowthLabel                  string
	DashRevGrowthClass                  string
	DashOrdersGrowthLabel               string
	DashOrdersGrowthClass               string
	DashAvgGrowthLabel                  string
	DashAvgGrowthClass                  string
	DashLowStock                        int
	DashWaiting                         int
	DashCompletedToday                  int
	DashChartRange                      string
	DashChart                           template.HTML
	DashRecent                          []DashRecentRow
	DashRecentTotal                     string
	DashPopular                         []DashPopularRow
	DashLowItems                        []InvRow
	StaffRows                           []StaffRow
	OrderRows                           []OrderRow
	OrdCntAll                           int
	OrdCntPending                       int
	OrdCntAccepted                      int
	OrdCntPreparing                     int
	OrdCntReady                         int
	OrdCntCompleted                     int
	OrdCntCancelled                     int
	OrdersOpenCount                     int
	OrdersCompletedRevenueFa            string
	OrderStatusFilter                   string
	OrderScopeAll                       bool
	OrderPageNum                        int
	OrderTotalPages                     int
	OrderFilteredTotal                  int
	OrderFrom                           int
	OrderTo                             int
	OrderPrevPage                       int
	OrderNextPage                       int
	StaffTotal                          int
	StaffActive                         int
	StaffInactive                       int
	StaffShifts                         int
	StaffShiftRows                      []StaffRow
	StaffChecklistPercent               int
	StaffPerms                          map[string]bool
	SessionName                         string
	SessionRole                         string
	LicenseDaysLeft                     int
	LicenseHolder                       string
	InvCategoryEntities                 []store.Entity
	InvCategoryRows                     []InvCategoryRow
	ProductVariants                     []ProductVariantRow
	ProductRecipeLines                  []ProductRecipeLineRow
	Endpoints                           []Endpoint
	Query                               string
	Total                               int
	Outbox                              []store.Operation
	Kind, EntityID, Action, EditPayload string
	Version                             int64
	Detail                              *Endpoint
	OrderDetail                         *store.Entity
	ReceiptCafeName                     string
	ReceiptHeaderText                   string
	ReceiptFooterText                   string
	ReceiptItemCount                    int
	ReceiptTableNumber                  string
	ReceiptDateTime                     string
	ReceiptDateOnly                     string
	ReceiptTimeOnly                     string
	ReceiptOrderType                    string
	ReceiptPayment                      string
	Fields                              map[string]string
	Units                               []string
}

func entityTime(e store.Entity) (time.Time, bool) {
	raw := strings.TrimSpace(e.Field("created_at"))
	if raw == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func growthBadge(today, yesterday float64) (string, string) {
	if yesterday <= 0 {
		return "", ""
	}
	pct := int(((today-yesterday)/yesterday)*100 + 0.5)
	if today < yesterday {
		pct = int(((yesterday-today)/yesterday)*100 + 0.5)
		if pct == 0 {
			return "بدون تغییر", "neutral"
		}
		return store.FormatPersianNumber(strconv.Itoa(pct)) + "٪ کاهش", "neg"
	}
	if pct == 0 {
		return "بدون تغییر", "neutral"
	}
	return store.FormatPersianNumber(strconv.Itoa(pct)) + "٪ افزایش", "pos"
}

func tomanNumber(irr string) string {
	bi := parseIRRBig(irr)
	toman := new(big.Int).Quo(bi, big.NewInt(10))
	return store.FormatPersianNumber(toman.String())
}

var staffRoleLabels = map[string]string{
	"owner": "مالک", "manager": "مدیر", "staff": "کارمند",
	"cashier": "صندوقدار", "waiter": "گارسون", "inventory_manager": "مسئول انبار",
}

func staffRowOf(e store.Entity) StaffRow {
	row := StaffRow{Entity: e}
	row.Active = e.Field("is_active") != "false" && !strings.Contains(e.Field("status"), "غیرفعال")
	row.RoleLabel = staffRoleLabels[e.Field("role_key")]
	if row.RoleLabel == "" {
		row.RoleLabel = e.Field("role")
	}
	if row.RoleLabel == "" {
		row.RoleLabel = "کارمند"
	}
	switch e.Field("salary_period") {
	case "daily":
		row.Cooperation = "روزمزد"
	case "hourly":
		row.Cooperation = "ساعتی"
	default:
		row.Cooperation = "ماهانه"
	}
	if e.Field("insurance_enabled") == "true" {
		row.Insurance = e.Field("insurance_provider")
		if row.Insurance == "" {
			row.Insurance = "دارد"
		}
	} else {
		row.Insurance = "ندارد"
	}
	if e.Field("salary_irr") != "" && e.Field("salary_irr") != "0" {
		row.SalaryText = e.PriceToman()
	} else {
		row.SalaryText = "-"
	}
	return row
}

// fillDashboard شاخص‌های «امروز» داشبورد را دقیقاً مانند وب از داده‌های محلی محاسبه می‌کند.
func (a *Application) fillDashboard(v *View, r *http.Request) {
	loc := tehranZone()
	now := time.Now().In(loc)
	v.DashTodayLabel = "امروز، " + strings.Split(FormatJalaliFull(now), " — ")[0]
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	yStart := dayStart.AddDate(0, 0, -1)

	var revToday, revY, avgTodaySum float64
	var ordersToday, ordersY int
	type recentItem struct {
		e store.Entity
		t time.Time
	}
	var recents []recentItem
	popular := map[string]int{}
	popularOrder := []string{}
	var completedToday int
	for _, o := range v.Entities {
		status := strings.ToLower(o.Field("status"))
		t, ok := entityTime(o)
		if !ok {
			continue
		}
		lt := t.In(loc)
		recents = append(recents, recentItem{o, lt})
		if status == "preparing" || status == "accepted" || status == "pending" || status == "ready" {
			v.DashWaiting++
		}
		if status != "completed" {
			continue
		}
		total, _ := strconv.ParseFloat(o.PriceIRR(), 64)
		switch {
		case !lt.Before(dayStart):
			revToday += total
			ordersToday++
			completedToday++
			for _, line := range o.OrderItems() {
				if _, seen := popular[line.ProductName]; !seen {
					popularOrder = append(popularOrder, line.ProductName)
				}
				popular[line.ProductName] += line.Quantity
			}
		case !lt.Before(yStart):
			revY += total
			ordersY++
		}
	}
	v.DashCompletedToday = completedToday
	v.DashOrdersToday = ordersToday
	v.DashRevTodayFa = tomanNumber(strconv.FormatFloat(revToday, 'f', 0, 64))
	avgTodaySum = 0
	if ordersToday > 0 {
		avgTodaySum = revToday / float64(ordersToday)
	}
	v.DashAvgTodayFa = tomanNumber(strconv.FormatFloat(avgTodaySum, 'f', 0, 64))
	avgY := 0.0
	if ordersY > 0 {
		avgY = revY / float64(ordersY)
	}
	v.DashRevGrowthLabel, v.DashRevGrowthClass = growthBadge(revToday, revY)
	v.DashOrdersGrowthLabel, v.DashOrdersGrowthClass = growthBadge(float64(ordersToday), float64(ordersY))
	v.DashAvgGrowthLabel, v.DashAvgGrowthClass = growthBadge(avgTodaySum, avgY)

	for _, ing := range v.Ingredients {
		if ing.NeedsReorder() {
			v.DashLowStock++
			if len(v.DashLowItems) < 4 {
				stock, _ := strconv.ParseFloat(ing.Field("current_stock"), 64)
				key := "low"
				if stock < 0 {
					key = "negative"
				} else if stock == 0 {
					key = "out"
				}
				stockText := store.FormatPersianNumber(strconv.FormatFloat(stock, 'f', -1, 64))
				if stock < 0 {
					stockText = "−" + strings.TrimPrefix(stockText, "-")
				}
				v.DashLowItems = append(v.DashLowItems, InvRow{Entity: ing, Negative: stock < 0, StatusKey: key, StockDisplay: stockText + " " + ing.UnitLabel()})
			}
		}
	}

	sort.Slice(recents, func(i, j int) bool { return recents[i].t.After(recents[j].t) })
	recentTotal := big.NewInt(0)
	for i, rc := range recents {
		if i >= 6 {
			break
		}
		var names []string
		var badges []string
		qty := 0
		for _, line := range rc.e.OrderItems() {
			names = append(names, line.ProductName)
			badge := line.ProductName
			if line.Quantity > 1 {
				badge += " ×" + store.FormatPersianNumber(strconv.Itoa(line.Quantity))
			}
			badges = append(badges, badge)
			qty += line.Quantity
		}
		recentTotal.Add(recentTotal, parseIRRBig(rc.e.PriceIRR()))
		v.DashRecent = append(v.DashRecent, DashRecentRow{
			Entity: rc.e, ItemsLabel: strings.Join(names, "، "), Items: badges, Quantity: qty,
			TimeLabel: store.ToPersianDigits(rc.t.Format("15:04")),
		})
	}
	v.DashRecentTotal = store.FormatIRRToToman(recentTotal.String())

	maxQty := 0
	for _, name := range popularOrder {
		if popular[name] > maxQty {
			maxQty = popular[name]
		}
	}
	sort.Slice(popularOrder, func(i, j int) bool { return popular[popularOrder[i]] > popular[popularOrder[j]] })
	for i, name := range popularOrder {
		if i >= 5 {
			break
		}
		pct := 0
		if maxQty > 0 {
			pct = popular[name] * 100 / maxQty
		}
		v.DashPopular = append(v.DashPopular, DashPopularRow{Rank: i + 1, Name: name, Qty: popular[name], Percent: pct})
	}

	v.DashChartRange = strings.TrimSpace(r.URL.Query().Get("range"))
	if v.DashChartRange != "7d" && v.DashChartRange != "30d" {
		v.DashChartRange = "today"
	}
	v.DashChart = buildRevenueChart(v.DashChartRange, v.Entities, v.Expenses, now)
}

// buildRevenueChart نمودار درآمد/هزینه وب را با SVG واقعی از سفارش‌ها و هزینه‌های محلی می‌سازد.
// تول‌تیپ تیره (ChartTooltipCard وب)، کرسر عمودی و نقطه فعال روی هر دو سری دارد.
func buildRevenueChart(rangeKey string, orders, expenses []store.Entity, now time.Time) template.HTML {
	loc := tehranZone()
	now = now.In(loc)
	type bucket struct {
		label   string
		title   string
		income  float64
		expense float64
	}
	fullDate := func(d time.Time) string {
		jy, jm, jd := GregorianToJalali(d.Year(), int(d.Month()), d.Day())
		monthName := ""
		if jm >= 1 && jm <= 12 {
			monthName = jalaliMonthNames[jm]
		}
		return store.ToPersianDigits(fmt.Sprintf("%s %d %s %d", jalaliWeekdayNames[int(d.Weekday())%7], jd, monthName, jy))
	}
	var buckets []bucket
	if rangeKey == "today" {
		for h := 0; h < 24; h++ {
			label := fmt.Sprintf("%02d:00", h)
			buckets = append(buckets, bucket{
				label: label,
				title: fullDate(now) + " - ساعت " + store.ToPersianDigits(label),
			})
		}
		for _, o := range orders {
			if strings.ToLower(o.Field("status")) != "completed" {
				continue
			}
			t, ok := entityTime(o)
			if !ok {
				continue
			}
			lt := t.In(loc)
			if lt.Year() == now.Year() && lt.YearDay() == now.YearDay() {
				val, _ := strconv.ParseFloat(o.PriceIRR(), 64)
				buckets[lt.Hour()].income += val
			}
		}
		for _, e := range expenses {
			t, ok := entityTime(e)
			if !ok {
				continue
			}
			lt := t.In(loc)
			if lt.Year() == now.Year() && lt.YearDay() == now.YearDay() {
				val, _ := strconv.ParseFloat(e.PriceIRR(), 64)
				buckets[lt.Hour()].expense += val
			}
		}
	} else {
		days := 7
		if rangeKey == "30d" {
			days = 30
		}
		dayIndex := map[string]int{}
		for i := days - 1; i >= 0; i-- {
			d := now.AddDate(0, 0, -i)
			key := d.Format("2006-01-02")
			dayIndex[key] = len(buckets)
			_, jm, jd := GregorianToJalali(d.Year(), int(d.Month()), d.Day())
			monthName := ""
			if jm >= 1 && jm <= 12 {
				monthName = jalaliMonthNames[jm]
			}
			buckets = append(buckets, bucket{
				label: store.ToPersianDigits(strconv.Itoa(jd)) + " " + monthName,
				title: fullDate(d),
			})
		}
		for _, o := range orders {
			if strings.ToLower(o.Field("status")) != "completed" {
				continue
			}
			t, ok := entityTime(o)
			if !ok {
				continue
			}
			if idx, seen := dayIndex[t.In(loc).Format("2006-01-02")]; seen {
				val, _ := strconv.ParseFloat(o.PriceIRR(), 64)
				buckets[idx].income += val
			}
		}
		for _, e := range expenses {
			t, ok := entityTime(e)
			if !ok {
				continue
			}
			if idx, seen := dayIndex[t.In(loc).Format("2006-01-02")]; seen {
				val, _ := strconv.ParseFloat(e.PriceIRR(), 64)
				buckets[idx].expense += val
			}
		}
	}
	maxVal := 0.0
	for _, b := range buckets {
		if b.income > maxVal {
			maxVal = b.income
		}
		if b.expense > maxVal {
			maxVal = b.expense
		}
	}
	// محور مبلغ مانند وب: تیک‌های گرد (۱/۲/۲٫۵/۵×۱۰ⁿ) روی مقیاس تومان و واحد خودکار.
	maxToman := maxVal / 10
	divisor, suffix := 1.0, "ت"
	if maxToman >= 1_000_000 {
		divisor, suffix = 1_000_000, "م.ت"
	} else if maxToman >= 1_000 {
		divisor, suffix = 1_000, "ه.ت"
	}
	var ticks []float64
	if maxToman <= 4 {
		ticks = []float64{0, 1, 2, 3, 4}
	} else {
		rawStep := maxToman / 4
		magnitude := math.Pow(10, math.Floor(math.Log10(rawStep)))
		normalized := rawStep / magnitude
		nice := 10.0
		switch {
		case normalized <= 1:
			nice = 1
		case normalized <= 2:
			nice = 2
		case normalized <= 2.5:
			nice = 2.5
		case normalized <= 5:
			nice = 5
		}
		step := math.Max(1, nice*magnitude)
		top := math.Ceil(maxToman/step) * step
		for v := 0.0; v <= top+step/2; v += step {
			ticks = append(ticks, v)
		}
	}
	topToman := ticks[len(ticks)-1]
	if topToman <= 0 {
		topToman = 1
	}
	axisLabel := func(v float64) string {
		if v == 0 {
			return "۰"
		}
		num := strconv.FormatFloat(v/divisor, 'f', 1, 64)
		num = strings.TrimSuffix(num, ".0")
		return store.ToPersianDigits(strings.ReplaceAll(num, ".", "٫")) + " " + suffix
	}
	const W, H, padL, padB, padT = 720.0, 250.0, 46.0, 26.0, 12.0
	plotW, plotH := W-padL-8, H-padB-padT
	x := func(i int) float64 {
		if len(buckets) <= 1 {
			return padL
		}
		return padL + plotW*float64(i)/float64(len(buckets)-1)
	}
	y := func(valIRR float64) float64 { return padT + plotH - ((valIRR/10)/topToman)*plotH }
	var sb strings.Builder
	sb.WriteString(`<svg viewBox="0 0 720 250" class="dash-chart" role="img" aria-label="نمودار درآمد و هزینه">`)
	for _, tick := range ticks {
		gy := y(tick * 10)
		fmt.Fprintf(&sb, `<line x1="%.0f" y1="%.1f" x2="%.0f" y2="%.1f" class="chart-grid"/><text x="%.0f" y="%.1f" class="chart-axis" text-anchor="end">%s</text>`, padL, gy, W-8, gy, padL-6, gy+3, axisLabel(tick))
	}
	buildPoints := func(key string) string {
		var pts []string
		for i, b := range buckets {
			v := b.income
			if key == "expense" {
				v = b.expense
			}
			pts = append(pts, fmt.Sprintf("%.1f,%.1f", x(i), y(v)))
		}
		return strings.Join(pts, " ")
	}
	incPts := buildPoints("income")
	expPts := buildPoints("expense")
	area := fmt.Sprintf("%.0f,%.1f ", padL, y(0)) + incPts + fmt.Sprintf(" %.1f,%.1f", x(len(buckets)-1), y(0))
	fmt.Fprintf(&sb, `<polygon points="%s" class="chart-area"/>`, area)
	fmt.Fprintf(&sb, `<polyline points="%s" class="chart-line-expense"/>`, expPts)
	fmt.Fprintf(&sb, `<polyline points="%s" class="chart-line-income"/>`, incPts)
	// کرسر و نقاط فعال (با هاور روی زون هر نقطه جابه‌جا می‌شوند)
	fmt.Fprintf(&sb, `<line class="chart-cursor" x1="0" y1="%.0f" x2="0" y2="%.0f" hidden/>`, padT, padT+plotH)
	sb.WriteString(`<circle class="chart-adot expense" r="5" hidden/><circle class="chart-adot income" r="5" hidden/>`)
	// زون‌های هاور نامرئی: نزدیک‌ترین نقطه فعال می‌شود (مانند Tooltip رچارتس وب)
	for i, b := range buckets {
		zx1 := padL
		if i > 0 {
			zx1 = (x(i-1) + x(i)) / 2
		}
		zx2 := W - 8
		if i < len(buckets)-1 {
			zx2 = (x(i) + x(i+1)) / 2
		}
		fmt.Fprintf(&sb, `<rect class="chart-hover-zone" x="%.1f" y="%.0f" width="%.1f" height="%.0f" data-title="%s" data-income="%s" data-expense="%s" data-cx="%.1f" data-iy="%.1f" data-ey="%.1f"/>`,
			zx1, padT, zx2-zx1, plotH,
			b.title,
			store.FormatIRRToToman(strconv.FormatFloat(b.income, 'f', 0, 64)),
			store.FormatIRRToToman(strconv.FormatFloat(b.expense, 'f', 0, 64)),
			x(i), y(b.income), y(b.expense))
	}
	step := 1
	if rangeKey == "today" {
		step = 2
	} else if rangeKey == "30d" {
		step = 5
	}
	for i, b := range buckets {
		if i%step != 0 && i != len(buckets)-1 {
			continue
		}
		label := b.label
		if rangeKey == "today" {
			label = store.ToPersianDigits(label)
		}
		fmt.Fprintf(&sb, `<text x="%.1f" y="%.0f" class="chart-axis" text-anchor="middle">%s</text>`, x(i), H-6, label)
	}
	sb.WriteString(`</svg>`)
	return template.HTML(sb.String())
}

// buildReportIncomeChart نمودار فروش گزارشات روزانه وب (income-chart) را با
// SVG واقعی از سفارش‌های محلی می‌سازد: امروز ساعتی، هفته/ماه روزانه و همهٔ
// دوره‌ها روزانه یا ماهانه بر اساس طول بازه.
func buildReportIncomeChart(period string, orders []store.Entity, now time.Time) template.HTML {
	loc := tehranZone()
	now = now.In(loc)
	type point struct {
		label  string
		title  string
		income float64
		orders int
	}
	var points []point
	dayLabel := func(d time.Time) string {
		_, jm, jd := GregorianToJalali(d.Year(), int(d.Month()), d.Day())
		monthName := ""
		if jm >= 1 && jm <= 12 {
			monthName = jalaliMonthNames[jm]
		}
		return store.ToPersianDigits(strconv.Itoa(jd)) + " " + monthName
	}
	fullDay := func(d time.Time) string {
		return FormatJalaliFull(d)
	}
	addOrder := func(idx int, o store.Entity) {
		if idx < 0 || idx >= len(points) {
			return
		}
		status := strings.ToLower(strings.TrimSpace(o.Field("status")))
		if status != "cancelled" && status != "canceled" {
			points[idx].orders++
		}
		if status == "completed" {
			val, _ := strconv.ParseFloat(o.PriceIRR(), 64)
			points[idx].income += val
		}
	}

	switch period {
	case "today":
		for h := 0; h < 24; h++ {
			label := store.ToPersianDigits(fmt.Sprintf("%02d:00", h))
			points = append(points, point{label: label, title: fullDay(now) + " — ساعت " + label})
		}
		for _, o := range orders {
			t, ok := entityTime(o)
			if !ok {
				continue
			}
			lt := t.In(loc)
			if lt.Year() == now.Year() && lt.YearDay() == now.YearDay() {
				addOrder(lt.Hour(), o)
			}
		}
	case "month":
		index := map[string]int{}
		for i := 29; i >= 0; i-- {
			d := now.AddDate(0, 0, -i)
			index[d.Format("2006-01-02")] = len(points)
			points = append(points, point{label: dayLabel(d), title: fullDay(d)})
		}
		for _, o := range orders {
			t, ok := entityTime(o)
			if !ok {
				continue
			}
			if idx, seen := index[t.In(loc).Format("2006-01-02")]; seen {
				addOrder(idx, o)
			}
		}
	case "all":
		var dated []time.Time
		for _, o := range orders {
			if t, ok := entityTime(o); ok {
				dated = append(dated, t.In(loc))
			}
		}
		if len(dated) == 0 {
			points = append(points, point{label: dayLabel(now), title: fullDay(now)})
			break
		}
		sort.Slice(dated, func(i, j int) bool { return dated[i].Before(dated[j]) })
		minD := time.Date(dated[0].Year(), dated[0].Month(), dated[0].Day(), 0, 0, 0, 0, loc)
		maxD := time.Date(dated[len(dated)-1].Year(), dated[len(dated)-1].Month(), dated[len(dated)-1].Day(), 0, 0, 0, 0, loc)
		spanDays := int(maxD.Sub(minD).Hours()/24) + 1
		if spanDays <= 62 {
			index := map[string]int{}
			for d := minD; !d.After(maxD); d = d.AddDate(0, 0, 1) {
				index[d.Format("2006-01-02")] = len(points)
				points = append(points, point{label: dayLabel(d), title: fullDay(d)})
			}
			for _, o := range orders {
				t, ok := entityTime(o)
				if !ok {
					continue
				}
				if idx, seen := index[t.In(loc).Format("2006-01-02")]; seen {
					addOrder(idx, o)
				}
			}
		} else {
			index := map[string]int{}
			for d := time.Date(minD.Year(), minD.Month(), 1, 0, 0, 0, 0, loc); !d.After(maxD); d = d.AddDate(0, 1, 0) {
				index[d.Format("2006-01")] = len(points)
				_, jm, _ := GregorianToJalali(d.Year(), int(d.Month()), d.Day())
				monthName := ""
				if jm >= 1 && jm <= 12 {
					monthName = jalaliMonthNames[jm]
				}
				jy, _, _ := GregorianToJalali(d.Year(), int(d.Month()), d.Day())
				points = append(points, point{label: monthName + " " + store.ToPersianDigits(strconv.Itoa(jy)), title: monthName + " " + store.ToPersianDigits(strconv.Itoa(jy))})
			}
			for _, o := range orders {
				t, ok := entityTime(o)
				if !ok {
					continue
				}
				if idx, seen := index[t.In(loc).Format("2006-01")]; seen {
					addOrder(idx, o)
				}
			}
		}
	default: // week
		index := map[string]int{}
		for i := 6; i >= 0; i-- {
			d := now.AddDate(0, 0, -i)
			index[d.Format("2006-01-02")] = len(points)
			points = append(points, point{label: dayLabel(d), title: fullDay(d)})
		}
		for _, o := range orders {
			t, ok := entityTime(o)
			if !ok {
				continue
			}
			if idx, seen := index[t.In(loc).Format("2006-01-02")]; seen {
				addOrder(idx, o)
			}
		}
	}
	if len(points) == 0 {
		points = append(points, point{label: dayLabel(now), title: fullDay(now)})
	}

	maxVal := 0.0
	for _, p := range points {
		if p.income > maxVal {
			maxVal = p.income
		}
	}
	maxToman := maxVal / 10
	divisor, suffix := 1.0, "ت"
	if maxToman >= 1_000_000 {
		divisor, suffix = 1_000_000, "م.ت"
	} else if maxToman >= 1_000 {
		divisor, suffix = 1_000, "ه.ت"
	}
	var ticks []float64
	if maxToman <= 4 {
		ticks = []float64{0, 1, 2, 3, 4}
	} else {
		rawStep := maxToman / 4
		magnitude := math.Pow(10, math.Floor(math.Log10(rawStep)))
		normalized := rawStep / magnitude
		nice := 10.0
		switch {
		case normalized <= 1:
			nice = 1
		case normalized <= 2:
			nice = 2
		case normalized <= 2.5:
			nice = 2.5
		case normalized <= 5:
			nice = 5
		}
		step := math.Max(1, nice*magnitude)
		top := math.Ceil(maxToman/step) * step
		for v := 0.0; v <= top+step/2; v += step {
			ticks = append(ticks, v)
		}
	}
	topToman := ticks[len(ticks)-1]
	if topToman <= 0 {
		topToman = 1
	}
	axisLabel := func(v float64) string {
		if v == 0 {
			return "۰"
		}
		num := strconv.FormatFloat(v/divisor, 'f', 1, 64)
		num = strings.TrimSuffix(num, ".0")
		return store.ToPersianDigits(strings.ReplaceAll(num, ".", "٫")) + " " + suffix
	}

	const W, H = 720.0, 270.0
	const padL, padR, padT, padB = 14.0, 76.0, 14.0, 30.0
	plotW, plotH := W-padL-padR, H-padT-padB
	x := func(i int) float64 {
		if len(points) <= 1 {
			return W - padR
		}
		return padL + plotW*float64(i)/float64(len(points)-1)
	}
	y := func(valIRR float64) float64 { return padT + plotH - ((valIRR/10)/topToman)*plotH }

	var sb strings.Builder
	sb.WriteString(`<svg viewBox="0 0 720 270" class="report-chart" role="img" aria-label="نمودار کامل فروش"><defs><linearGradient id="reports-sales-fill" x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stop-color="#14B8A6" stop-opacity="0.28"/><stop offset="90%" stop-color="#14B8A6" stop-opacity="0.02"/></linearGradient></defs>`)
	for _, tick := range ticks {
		gy := y(tick * 10)
		fmt.Fprintf(&sb, `<line x1="%.0f" y1="%.1f" x2="%.0f" y2="%.1f" class="rpt-chart-grid"/><text x="%.0f" y="%.1f" class="rpt-chart-axis" text-anchor="end">%s</text>`, padL, gy, W-padR, gy, W-8, gy+3, axisLabel(tick))
	}
	var pts []string
	for i, p := range points {
		pts = append(pts, fmt.Sprintf("%.1f,%.1f", x(i), y(p.income)))
	}
	area := fmt.Sprintf("%.1f,%.1f ", x(0), y(0)) + strings.Join(pts, " ") + fmt.Sprintf(" %.1f,%.1f", x(len(points)-1), y(0))
	fmt.Fprintf(&sb, `<polygon points="%s" class="rpt-chart-area"/>`, area)
	fmt.Fprintf(&sb, `<polyline points="%s" class="rpt-chart-line"/>`, strings.Join(pts, " "))
	for i, p := range points {
		fmt.Fprintf(&sb, `<circle cx="%.1f" cy="%.1f" r="3.5" class="rpt-chart-dot"><title>%s — فروش %s — %s سفارش</title></circle>`, x(i), y(p.income), p.title, store.FormatIRRToToman(strconv.FormatFloat(p.income, 'f', 0, 64)), store.FormatPersianNumber(strconv.Itoa(p.orders)))
	}
	step := 1
	switch {
	case period == "today":
		step = 4
	case period == "month":
		step = 5
	case len(points) > 14:
		step = 5
	}
	for i, p := range points {
		if i%step != 0 && i != len(points)-1 {
			continue
		}
		fmt.Fprintf(&sb, `<text x="%.1f" y="%.0f" class="rpt-chart-axis" text-anchor="middle">%s</text>`, x(i), H-7, p.label)
	}
	sb.WriteString(`</svg>`)
	return template.HTML(sb.String())
}

// parseIRRBig پارس امن مبلغ IRR (بخش صحیح) برای محاسبات نمایشی.
func parseIRRBig(raw string) *big.Int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return big.NewInt(0)
	}
	bi := new(big.Int)
	if _, ok := bi.SetString(strings.SplitN(raw, ".", 2)[0], 10); !ok {
		return big.NewInt(0)
	}
	return bi
}

// PricingRow یک ردیف قیمت‌گذاری: قیمت فروش، بهای رسپی (BOM) و حاشیه.
// POSGroup یک دسته از محصولات صندوق با سرتیتر دسته (مانند وب).
type POSGroup struct {
	ID       string
	Label    string
	Products []store.Entity
}

// POSStep یک گام نوار پیشرفت وضعیت سفارش روی کارت میز صندوق.
type POSStep struct {
	Label   string
	Done    bool
	Current bool
}

// POSTableRow کارت میز صندوق: خود میز + سفارش باز روی آن (اگر باشد).
type POSTableRow struct {
	Table            store.Entity
	HasOrder         bool
	OrderID          string
	Number           string
	TotalFa          string
	ItemsCount       int
	TimeLabel        string
	StateKey         string // free | pending | accepted | preparing | ready | reserved | disabled
	StateLabel       string
	NextTarget       string // وضعیت هدف دکمه اصلی (مرحله بعد یا completed)
	NextLabel        string
	ShowComplete     bool // نمایش «تکمیل و تسویه میز» (وقتی مرحله‌ای نمانده)
	SplitRemainingFa string
	SplitSettled     bool
	Steps            []POSStep
}

// POSRecentRow یک ردیف دیالوگ «سفارش‌های صندوق» (سفارش‌های اخیر).
type POSRecentRow struct {
	ID          string
	Number      string
	TotalFa     string
	StatusLabel string
	StatusKey   string
	TimeLabel   string
	TableLabel  string
	// فیلدهای کارت وب (pos-recent-orders-dialog): خلاصه اقلام، مبدأ،
	// پیشرفت مراحل و اقدام اصلی بعدی.
	ItemsLabel   string
	OriginLabel  string
	CustomerName string
	NextTarget   string
	NextLabel    string
	ProgressPct  int
	IsOpen       bool
	IsCompleted  bool
	IsCancelled  bool
	HasTable     bool
}

// DebtorRow ردیف بدهکار با جزئیات کامل سفارش پیوندخورده برای دیالوگ جزئیات:
// چه چیزی سفارش داده، چقدر بدهی دارد، توضیحات و تاریخ/ساعت دقیق سفارش.
// فیلدهای قالبی صفحه «بدهکاران و تسویه» عین ستون‌های نسخه وب هستند.
type DebtorRow struct {
	Entity      store.Entity
	ItemsText   string // اقلام سفارش پیوندخورده (نام × تعداد)
	OrderNumber string
	OrderWhen   string // تاریخ و ساعت دقیق سفارش (جلالی کامل)
	DueLabel    string
	NoteText    string
	OtherDebts  string // خلاصه سایر بدهی‌های همان شخص
	OtherCount  int

	// ستون‌ها و جزئیات صفحهٔ بدهکاران (هم‌طراز لیست وب)
	Phone        string
	GroupLabel   string
	AccountKey   string // personal | company
	AccountLabel string
	SourceKey    string // pos | manual
	SourceLabel  string
	TotalFa      string
	PaidFa       string
	RemainingFa  string
	RemainingIRR string
	DateFa       string
	TimeFa       string
	StatusKey    string // open | partial | paid | overdue
	StatusLabel  string
	StatusClass  string
	Overdue      bool
	IsNew        bool
	ItemCount    int
	HasDocument  bool
	PaymentFa    string // جمع پرداخت‌های ثبت‌شده برای این حساب
	PaymentCount int
	Payments     []DebtorPaymentRow
	CreatedISO   string
	DueISO       string
}

// DebtorPaymentRow یک سطر پرداخت ثبت‌شده روی حساب دفتری (دفتر وصولی).
type DebtorPaymentRow struct {
	AmountFa string
	DateFa   string
	Note     string
}

// DebtorGroupRow گزارش بدهی گروه‌های سازمانی: تعداد حساب‌ها و مانده بدهی.
type DebtorGroupRow struct {
	Entity       store.Entity
	Name         string
	Accounts     int
	Members      int
	TotalFa      string
	RemainingFa  string
	OverdueCount int
	StatusKey    string
	StatusLabel  string
}

// webDigitsFa ارقام لاتین را فارسی و جداکننده هزارگان را کاما می‌گذارد؛
// دقیقاً همان شکل عددی نسخه وب (۲۴,۱۹۵,۰۰۰).
func webDigitsFa(digits string) string {
	digits = strings.TrimSpace(digits)
	if digits == "" {
		return "۰"
	}
	neg := strings.HasPrefix(digits, "-")
	if neg {
		digits = strings.TrimPrefix(digits, "-")
	}
	var b strings.Builder
	for i, r := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	out := store.ToPersianDigits(b.String())
	if neg {
		return "-" + out
	}
	return out
}

// tomanFaWeb مبلغ ریالی را به «۲۴,۱۹۵,۰۰۰ تومان» تبدیل می‌کند.
func tomanFaWeb(irr string) string {
	bi := parseIRRBig(irr)
	bi.Quo(bi, big.NewInt(10))
	return webDigitsFa(bi.String()) + " تومان"
}

// tomanFaWebInt همان tomanFaWeb برای مبالغ ریالی big.Int.
func tomanFaWebInt(irr *big.Int) string {
	if irr == nil {
		return webDigitsFa("0") + " تومان"
	}
	return tomanFaWeb(irr.String())
}

// debtorStatusOf وضعیت نمایشی حساب را یک‌جا تعیین می‌کند تا لیست، فیلتر و
// شمارنده‌های بالای صفحه همیشه یک پاسخ بدهند.
func debtorStatusOf(remaining *big.Int, rawStatus string, overdue bool) (string, string, string) {
	status := strings.ToLower(strings.TrimSpace(rawStatus))
	if remaining.Sign() <= 0 || status == "paid" {
		return "paid", "تسویه شده", "status-ok"
	}
	if overdue {
		return "overdue", "سررسید گذشته", "status-danger"
	}
	if status == "partial" {
		return "partial", "پرداخت بخشی از بدهی", "status-info"
	}
	return "open", "بدهکار (باز)", "status-warn"
}

// debtorDueInfo تاریخ سررسید ذخیره‌شده (ISO یا شمسی یا متن آزاد) را برای
// نمایش و مقایسه برمی‌گرداند. نتیجه: ISO، نمایش شمسی، سررسید گذشته.
func debtorDueInfo(rawDue string, remaining *big.Int, now time.Time) (iso, label string, overdue bool) {
	rawDue = strings.TrimSpace(rawDue)
	if rawDue == "" {
		return "", "", false
	}
	if t, ok := jalaliOrISODate(rawDue); ok {
		iso = t.Format("2006-01-02")
		label = ShamsiFromISO(iso)
		y, m, d := now.In(tehranZone()).Date()
		todayISO := time.Date(y, m, d, 0, 0, 0, 0, tehranZone()).Format("2006-01-02")
		overdue = remaining.Sign() > 0 && iso < todayISO
		return iso, label, overdue
	}
	return "", rawDue, false
}

// jalaliOrISODate ورودی تاریخ را از قالب ISO یا ۱۴۰۵/۰۷/۲۶ می‌خواند.
func jalaliOrISODate(raw string) (time.Time, bool) {
	if t, ok := JalaliTimeOf(raw); ok {
		return t, true
	}
	return ParseJalali(raw)
}

// isoDateOrEmpty ورودی تاریخ (ISO یا شمسی) را فقط در صورت معتبر بودن به
// 2006-01-02 تبدیل می‌کند تا مقایسه بازه تاریخ همیشه درست باشد.
func isoDateOrEmpty(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if t, ok := jalaliOrISODate(raw); ok {
		return t.Format("2006-01-02")
	}
	return ""
}

// debtorItemsFromPayload اقلام ثبت‌شده روی خود حساب (ثبت دستی بدهی) را
// می‌خواند تا ستون «مستندات سفارش (سفارش چی بود؟)» خالی نماند.
func debtorItemsFromPayload(e store.Entity) [][3]string {
	var payload struct {
		Items []struct {
			Title        string          `json:"title"`
			Qty          json.RawMessage `json:"qty"`
			UnitPriceIRR json.RawMessage `json:"unit_price_irr"`
		} `json:"items"`
	}
	if json.Unmarshal(e.Payload, &payload) != nil {
		return nil
	}
	out := make([][3]string, 0, len(payload.Items))
	for _, it := range payload.Items {
		title := strings.TrimSpace(it.Title)
		if title == "" {
			continue
		}
		qty := strings.Trim(strings.TrimSpace(string(it.Qty)), `"`)
		price := strings.Trim(strings.TrimSpace(string(it.UnitPriceIRR)), `"`)
		out = append(out, [3]string{title, qty, price})
	}
	return out
}


// WasteRow یک ردیف نمایشی ضایعات: دلیل با برچسب وب و هزینه تقریبی
// (مقدار × میانگین بهای ماده اولیه).
type WasteRow struct {
	Entity      store.Entity
	ReasonLabel string
	CostFa      string
	Voided      bool
}

// WasteAgg ردیف تجمیعی ضایعات بر اساس دلیل یا قلم.
type WasteAgg struct {
	Label  string
	Events int
	CostFa string
	QtyFa  string
}

// CategoryRow ردیف دسته‌بندی منو با تعداد محصولات متصل.
type CategoryRow struct {
	Entity   store.Entity
	Products int
	// AvailableProducts تعداد محصول‌های قابل فروش دسته است تا دکمهٔ
	// «ناموجود کردن» یا «موجود کردن» دسته درست انتخاب شود.
	AvailableProducts int
	DiscountPercent   int
	AllUnavailable    bool
}

// HasDiscount یعنی تخفیف دسته‌ای فعال است و قیمت خط‌خورده باید نمایش داده شود.
func (r CategoryRow) HasDiscount() bool { return r.DiscountPercent > 0 }

// BestSellerRow ردیف پرفروش‌ترین محصولات در گزارش دوره.
type BestSellerRow struct {
	Title     string
	QtyFa     string
	RevenueFa string
}

// ReportProductRow ردیف عملکرد محصول در گزارشات روزانه (products-performance وب).
type ReportProductRow struct {
	Title      string
	Quantity   int
	QuantityFa string
	RevenueIRR int64
	RevenueFa  string
	Share      float64
	ShareFa    string
	BarWidth   int
}

// ReportOrderRow ردیف فشرده تراکنش/سفارش اخیر در گزارشات روزانه.
type ReportOrderRow struct {
	Entity      store.Entity
	Number      string
	ItemsCount  int
	ItemsFa     string
	TotalFa     string
	StatusKey   string
	StatusLabel string
	StatusClass string
	DateFa      string
}

// ReportStatusSlice یک برش نمودار دونات وضعیت سفارش‌ها.
type ReportStatusSlice struct {
	Key     string
	Label   string
	Color   string
	Count   int
	Percent int
	Dash    float64
	Offset  float64
}

// ProductVariantRow تنوع محصول در فرم ساخت/ویرایش (variants وب):
// هر تنوع یک عنوان، مبلغ (تومان) و وضعیت فعال دارد تا برای حالت‌هایی مثل
// «اسپرسو ۵۰/۵۰» و «اسپرسو ۲۰/۸۰» محصول جداگانه ساخته نشود.
type ProductVariantRow struct {
	Title  string
	Value  string
	Active bool
}

// ProductRecipeLineRow یک خط دستور تهیه در فرم ساخت محصول (recepie وب).
type ProductRecipeLineRow struct {
	IngredientID string
	Quantity     string
	Unit         string
}

// POSEditState وضعیت «ویرایش سفارش» صندوق: سفارش باز در سبد بارگذاری شده است.
type POSEditState struct {
	OrderID       string
	Number        string
	TableLabel    string
	CustomerName  string
	CustomerPhone string
	Note          string
	ExtraNote     string
	DiscountIRR   int64
	ExtraIRR      int64
	ItemsJSON     template.JS
	ItemsCount    int
}

// POSStageOpt یک مرحله قابل‌پیکربندی چرخه سفارش (PosOrderWorkflowControl وب).
type POSStageOpt struct {
	Key     string
	Label   string
	Desc    string
	Enabled bool
}

// CategoryRouteRow یک ردیف «مسیر چاپ دسته‌بندی» در تنظیمات چاپگرها.
type CategoryRouteRow struct {
	Category    store.Entity
	Products    int
	HasRoute    bool
	Active      bool
	PrinterID   string
	PrinterName string
	Copies      int
}

// ClubCustomerRow ردیف مشتری باشگاه با خلاصه خرید واقعی از سفارش‌های محلی.
type ClubCustomerRow struct {
	Entity            store.Entity
	Name              string
	Mobile            string
	IsMember          bool
	TierKey           string
	TierLabel         string
	PointsFa          string
	MemberCode        string
	CreditEnabled     bool
	GroupName         string
	GroupID           string
	LastVisitFa       string
	LastOrderNo       string
	LastOrderMeta     string
	LastOrderAmountFa string
	TotalSpentFa      string
	TotalOrders       int
	DebtorRemainFa    string
	Initials          string
	OnlineOrders      int
	LastOrderDateFa   string
}

// ClubPaymentRow ردیف پرداخت اشتراک آنلاین (club-membership-panel وب).
type ClubPaymentRow struct {
	Entity        store.Entity
	CustomerName  string
	CustomerPhone string
	PlanTitle     string
	AmountFa      string
	MethodLabel   string
	StatusKey     string
	StatusLabel   string
	Pending       bool
	Reference     string
	DateFa        string
}

// ClubGroupRow ردیف گروه مشتریان (club-groups-page وب).
type ClubGroupRow struct {
	Entity       store.Entity
	MembersCount int
	DiscountText string
	CapText      string
	ValidityText string
	StatusKey    string // valid | out | inactive
	StatusLabel  string
}

// ClubGroupOrder سفارش اخیر یک عضو گروه در صفحه جزئیات گروه.
type ClubGroupOrder struct {
	Number       string
	CustomerName string
	TotalFa      string
	StatusLabel  string
	DateFa       string
}

// ClubGroupDetail صفحه جزئیات گروه: اطلاعات قرارداد، اعضا و سفارش‌هایشان.
type ClubGroupDetail struct {
	Group         store.Entity
	Row           ClubGroupRow
	Members       []ClubCustomerRow
	RecentOrders  []ClubGroupOrder
	TotalSpentFa  string
	CreditSpentFa string
}

// defaultCafePermissionsForRole آیینه‌ی DEFAULT_CAFE_ROLE_PERMISSIONS وب
// (core/authentication/cafe-permissions.ts)؛ مدیر دسترسی کامل ضمنی دارد.
func defaultCafePermissionsForRole(role string) []string {
	common := []string{"printers.manage", "support.tickets.view", "help.view", "updates.view"}
	withCommon := func(extra ...string) []string {
		return append(append([]string{}, common...), extra...)
	}
	switch strings.TrimSpace(strings.ToLower(role)) {
	case "cashier":
		return withCommon("pos.view", "orders.view", "orders.update", "receipts.view", "tables.view", "tables.transfer", "menu.qr.view")
	case "waiter":
		return withCommon("pos.view", "orders.view", "tables.view", "menu.qr.view")
	case "inventory_manager":
		return withCommon("pos.view", "orders.view", "receipts.view", "tables.view", "inventory.view", "inventory.purchases.view", "inventory.recipes.view", "inventory.waste.view", "menu.qr.view")
	case "staff":
		return withCommon("pos.view", "orders.view", "tables.view")
	default:
		return nil
	}
}

type PricingRow struct {
	Product       store.Entity
	RecipeCostIRR string
	HasRecipe     bool
	MarginIRR     string
}

// CostToman بهای تمام‌شده رسپی به تومان برای نمایش.
func (r PricingRow) CostToman() string {
	if !r.HasRecipe {
		return "—"
	}
	return store.FormatIRRToToman(r.RecipeCostIRR)
}

// MarginToman حاشیه ناخالص هر واحد (قیمت منهای بهای رسپی) به تومان.
func (r PricingRow) MarginToman() string {
	if !r.HasRecipe {
		return "—"
	}
	return store.FormatIRRToToman(r.MarginIRR)
}

// TopProduct محصول پرفروش بر پایه اقلام سفارش‌های تکمیل‌شده.
type TopProduct struct {
	Name       string
	Quantity   int
	RevenueIRR string
}

// RevenueToman درآمد محصول به تومان برای نمایش.
func (t TopProduct) RevenueToman() string { return store.FormatIRRToToman(t.RevenueIRR) }

// StaffRow ردیف کارت/لیست کارکنان هم‌تراز با صفحه پرسنل وب.
type StaffRow struct {
	Entity      store.Entity
	Active      bool
	RoleLabel   string
	Cooperation string
	Insurance   string
	SalaryText  string
}

// DashRecentRow ردیف آخرین سفارش‌های داشبورد.
type DashRecentRow struct {
	Entity     store.Entity
	ItemsLabel string
	Items      []string // نام هر قلم (+ تعداد) برای نمایش به‌صورت بج
	Quantity   int
	TimeLabel  string
}

// OrderRow ردیف لیست سفارش‌ها هم‌تراز با جدول سفارش‌های داشبورد وب
// (orders-list): شماره، میز، خلاصه آیتم‌ها، مبلغ، زمان نسبی، بج وضعیت و
// دکمه اقدام اصلی بعدی (PRIMARY_NEXT_STATUS وب).
type OrderRow struct {
	Entity       store.Entity
	Number       string // شماره خوانا یا «#shortId»
	ShortID      string // شناسه کوتاه زیر شماره (وقتی شماره خوانا هست)
	TableNumber  string // فقط عدد میز؛ خالی یعنی «—»
	ItemsPreview string // حداکثر دو قلم اول مانند وب
	ItemsHidden  int    // تعداد اقلام پنهان (+N مورد)
	ItemsFull    string // متن کامل آیتم‌ها برای جستجو/عنوان
	TotalFa      string // مبلغ گروه‌بندی‌شده (تومان، بدون پسوند)
	TimeLabel    string // زمان نسبی فارسی مانند وب (fromNowPersian)
	StatusKey    string // PENDING|ACCEPTED|PREPARING|READY|COMPLETED|CANCELLED|REFUNDED
	StatusLabel  string
	CustomerName string
	CashierName  string
	OriginKind   string // pos | table | takeaway
	OriginLabel  string
	NextStatus   string // وضعیت بعدی چرخه وب (lowercase)؛ خالی یعنی اقدام اصلی ندارد
	NextLabel    string
	Cancellable  bool
	CanDelete    bool
	// داده‌های دیالوگ‌های ردیف (لغو/حذف/ویرایش/بدهکاران) مانند منوی وب
	NumberRaw     string
	CustomerPhone string
	ItemsJSON     string
	Editable      bool
}

// orderStatusKey وضعیت ذخیره‌شده را به کلیدهای وب نرمال می‌کند
// (PENDING شامل OPEN و خالی هم می‌شود؛ مانند فیلتر وب).
// displayOrderNumber شماره سفارش را برای نمایش نرمال می‌کند: داده‌های
// قدیمی با پیشوند «F-» ذخیره شده‌اند؛ نمایش فقط عدد است.
func displayOrderNumber(raw string) string {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "F-") {
		s = strings.TrimSpace(strings.TrimPrefix(s, "F-"))
	}
	return s
}

// isTehranToday آیا زمان ساخته‌شدن رکورد (RFC3339) در روز جاری تهران است؟
func isTehranToday(createdAt string) bool {
	ct, err := time.Parse(time.RFC3339, strings.TrimSpace(createdAt))
	if err != nil {
		return false
	}
	loc, lErr := time.LoadLocation("Asia/Tehran")
	if lErr != nil {
		loc = time.FixedZone("IRST", 3*3600+30*60)
	}
	y1, m1, d1 := ct.In(loc).Date()
	y2, m2, d2 := time.Now().In(loc).Date()
	return y1 == y2 && m1 == m2 && d1 == d2
}

func orderStatusKey(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "accepted":
		return "ACCEPTED"
	case "preparing":
		return "PREPARING"
	case "ready":
		return "READY"
	case "completed":
		return "COMPLETED"
	case "cancelled", "canceled":
		return "CANCELLED"
	case "refunded":
		return "REFUNDED"
	default:
		return "PENDING"
	}
}

// orderNextStep اقدام اصلی بعدی هر وضعیت در چرخه وب
// (PRIMARY_NEXT_STATUS + ACTION_META در orders-list وب).
func orderNextStep(key string) (status, label string) {
	switch key {
	case "PENDING":
		return "accepted", "تأیید صندوقدار"
	case "ACCEPTED":
		return "preparing", "در حال آماده‌سازی"
	case "PREPARING":
		return "ready", "آماده تحویل"
	case "READY":
		return "completed", "تکمیل سفارش"
	}
	return "", ""
}

// posStageDefs مراحل قابل‌پیکربندی چرخه سفارش صندوق (POS_CONFIGURABLE_STAGE_OPTIONS وب).
var posStageDefs = []struct {
	Key         string
	Label       string
	ActionLabel string
	Desc        string
}{
	{"accepted", "تأیید صندوقدار", "تأیید سفارش", "بررسی و تأیید اولیه سفارش توسط صندوقدار پیش از ارسال به آماده‌سازی"},
	{"preparing", "در حال آماده‌سازی", "شروع آماده‌سازی", "نمایش وضعیت آماده‌سازی سفارش در آشپزخانه یا بار"},
	{"ready", "آماده تحویل", "آماده تحویل", "اعلام آماده بودن سفارش برای تحویل به مشتری یا سالن‌دار"},
}

// posEnabledStages مراحل فعال چرخه را از پروفایل کافه می‌خواند (پیش‌فرض وب: هر سه فعال).
func (a *Application) posEnabledStages(ctx context.Context) []string {
	stages := []string{"accepted", "preparing", "ready"}
	if prof, err := a.store.Entity(ctx, "cafe_profile", "cafe-main-profile"); err == nil {
		if raw := strings.TrimSpace(prof.Field("pos_stages")); raw != "" {
			stages = nil
			for _, part := range strings.Split(raw, ",") {
				p := strings.TrimSpace(strings.ToLower(part))
				for _, d := range posStageDefs {
					if p == d.Key {
						stages = append(stages, p)
					}
				}
			}
		}
	}
	return stages
}

// clubTierLabels سطوح باشگاه وب (TIER_LABELS).
var clubTierLabels = map[string]string{
	"bronze": "برنزی", "silver": "نقره‌ای", "gold": "طلایی", "platinum": "پلاتینیوم",
}

// clubBuildRows ردیف‌های باشگاه را از مشتری‌ها + سفارش‌ها + بدهکاران واقعی می‌سازد.
func clubBuildRows(customers, orders, debtors, groups []store.Entity) []ClubCustomerRow {
	groupName := map[string]string{}
	for _, g := range groups {
		groupName[g.ID] = g.Label()
	}
	rows := make([]ClubCustomerRow, 0, len(customers))
	for _, c := range customers {
		mobile := strings.TrimSpace(c.Field("phone"))
		var totalSpent int64
		totalOrders := 0
		var lastOrder *store.Entity
		for i := range orders {
			o := orders[i]
			match := strings.TrimSpace(o.Field("customer_id")) == c.ID ||
				(mobile != "" && strings.TrimSpace(o.Field("customer_phone")) == mobile)
			if !match || strings.EqualFold(o.Field("status"), "cancelled") || strings.EqualFold(o.Field("status"), "canceled") {
				continue
			}
			totalOrders++
			amt, _ := strconv.ParseInt(strings.TrimSpace(o.PriceIRR()), 10, 64)
			totalSpent += amt
			if lastOrder == nil || o.Field("created_at") > lastOrder.Field("created_at") {
				oc := o
				lastOrder = &oc
			}
		}
		var debtorRemain int64
		for _, d := range debtors {
			if mobile != "" && strings.TrimSpace(d.Field("phone")) == mobile {
				rem, _ := strconv.ParseInt(strings.TrimSpace(d.Field("remaining")), 10, 64)
				debtorRemain += rem
			}
		}
		tierKey, tierLabel := "none", "بدون سطح"
		if raw := strings.TrimSpace(c.Field("tier")); raw != "" {
			if l, ok := clubTierLabels[strings.ToLower(raw)]; ok {
				tierKey, tierLabel = strings.ToLower(raw), l
			} else {
				tierLabel = raw
			}
		}
		name := strings.TrimSpace(c.Label())
		row := ClubCustomerRow{
			Entity:        c,
			Name:          name,
			Mobile:        mobile,
			IsMember:      c.Field("club_member") == "true" || strings.TrimSpace(c.Field("tier")) != "" || strings.TrimSpace(c.Field("loyalty_points")) != "",
			TierKey:       tierKey,
			TierLabel:     tierLabel,
			PointsFa:      store.FormatPersianNumber(strings.TrimSpace(c.Field("loyalty_points"))),
			MemberCode:    strings.TrimSpace(c.Field("subscription_code")),
			CreditEnabled: c.Field("credit_enabled") == "true",
			GroupID:       strings.TrimSpace(c.Field("group_id")),
			TotalOrders:   totalOrders,
			OnlineOrders:  totalOrders,
		}
		if row.PointsFa == "" {
			row.PointsFa = "۰"
		}
		row.GroupName = groupName[row.GroupID]
		row.TotalSpentFa = store.FormatPersianNumber(strconv.FormatInt(totalSpent/10, 10)) + " تومان"
		if totalOrders == 0 {
			row.TotalSpentFa = "—"
		}
		if debtorRemain > 0 {
			row.DebtorRemainFa = store.FormatPersianNumber(strconv.FormatInt(debtorRemain/10, 10)) + " تومان"
		}
		if lastOrder != nil {
			row.LastOrderNo = strings.TrimSpace(lastOrder.Field("order_number"))
			row.LastOrderAmountFa = store.FormatPersianNumber(new(big.Int).Div(parseIRRBig(lastOrder.PriceIRR()), big.NewInt(10)).String()) + " تومان"
			row.LastOrderMeta = FormatJalaliShort(lastOrder.Field("created_at")) + " · " + lastOrder.StatusLabel()
			row.LastVisitFa = FormatJalaliShort(lastOrder.Field("created_at"))
			row.LastOrderDateFa = FormatJalaliDate(lastOrder.Field("created_at"))
		}
		if name != "" {
			runes := []rune(name)
			row.Initials = string(runes[0])
			if len(runes) > 1 {
				parts := strings.Fields(name)
				if len(parts) > 1 {
					row.Initials = string([]rune(parts[0])[0]) + string([]rune(parts[1])[0])
				}
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// clubGroupRowOf ردیف نمایشی یک گروه (تخفیف/سقف/اعتبار/وضعیت مانند club-utils وب).
func clubGroupRowOf(g store.Entity, membersCount int, now time.Time) ClubGroupRow {
	row := ClubGroupRow{Entity: g, MembersCount: membersCount}
	discVal, _ := strconv.ParseInt(normalizeDigits(strings.TrimSpace(g.Field("discount_value"))), 10, 64)
	switch {
	case discVal <= 0:
		if g.Field("credit_enabled") == "true" {
			row.DiscountText = "بدون تخفیف (فقط نسیه)"
		} else {
			row.DiscountText = "بدون تخفیف (۰٪)"
		}
	case g.Field("discount_type") == "fixed":
		row.DiscountText = store.FormatPersianNumber(strconv.FormatInt(discVal, 10)) + " تومان"
	default:
		row.DiscountText = store.FormatPersianNumber(strconv.FormatInt(discVal, 10)) + "٪"
	}
	if g.Field("discount_type") == "percentage" || g.Field("discount_type") == "" {
		cap, _ := strconv.ParseInt(normalizeDigits(strings.TrimSpace(g.Field("max_discount_amount"))), 10, 64)
		if cap > 0 {
			row.CapText = "سقف " + store.FormatPersianNumber(strconv.FormatInt(cap, 10)) + " تومان"
		} else {
			row.CapText = "بدون سقف"
		}
	} else {
		row.CapText = "—"
	}
	active := g.Field("is_active") != "false"
	// تاریخ‌ها یا ISO ذخیره شده‌اند (تقویم جدید) یا شمسی (فرم قدیمی)؛ برای
	// مقایسه همیشه به ISO تبدیل می‌شوند.
	validFrom := strings.TrimSpace(g.Field("valid_from"))
	validUntil := strings.TrimSpace(g.Field("valid_until"))
	if iso := isoDateOrEmpty(validFrom); iso != "" {
		validFrom = iso
	}
	if iso := isoDateOrEmpty(validUntil); iso != "" {
		validUntil = iso
	}
	today := now.Format("2006-01-02")
	currentlyValid := active && (validFrom == "" || validFrom <= today) && (validUntil == "" || validUntil >= today)
	switch {
	case !active:
		row.StatusKey, row.StatusLabel = "inactive", "غیرفعال"
	case currentlyValid:
		row.StatusKey, row.StatusLabel = "valid", "معتبر"
	default:
		row.StatusKey, row.StatusLabel = "out", "خارج از دوره"
	}
	switch {
	case row.StatusKey == "out":
		row.ValidityText = "خارج از اعتبار"
	case validUntil != "":
		row.ValidityText = "تا " + FormatJalaliDate(validUntil)
	case validFrom != "":
		row.ValidityText = "از " + FormatJalaliDate(validFrom)
	default:
		row.ValidityText = "بدون پایان"
	}
	return row
}

// ClubGroupDialogJSON شکل دادهٔ دیالوگ «گروه جدید / ویرایش گروه» است. همهٔ
// گروه‌ها یک‌جا به صفحه می‌روند تا بازکردن دیالوگ بدون رفت‌وبرگشت سرور و
// مستقل از فیلتر فعلی جدول انجام شود.
type ClubGroupDialogJSON struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Description       string `json:"description"`
	ContactName       string `json:"contact_name"`
	ContactPhone      string `json:"contact_phone"`
	ContractNumber    string `json:"contract_number"`
	ContractType      string `json:"contract_type"`
	Headcount         string `json:"headcount"`
	MealsPerDay       string `json:"meals_per_day"`
	BillingCycle      string `json:"billing_cycle"`
	BillingDay        string `json:"billing_day"`
	ServiceAddress    string `json:"service_address"`
	DiscountType      string `json:"discount_type"`
	DiscountValue     string `json:"discount_value"`
	MaxDiscountAmount string `json:"max_discount_amount"`
	ValidFrom         string `json:"valid_from"`
	ValidUntil        string `json:"valid_until"`
	Notes             string `json:"notes"`
	CreditEnabled     bool   `json:"credit_enabled"`
	Active            bool   `json:"active"`
	DiscountText      string `json:"discount_text"`
	ValidityText      string `json:"validity_text"`
	StatusLabel       string `json:"status_label"`
}

// clubGroupsJSON همهٔ گروه‌های مشتریان را برای دیالوگ صفحه باشگاه آماده می‌کند.
func (a *Application) clubGroupsJSON(ctx context.Context) template.JS {
	groups, err := a.store.Entities(ctx, "customer_group")
	if err != nil {
		return template.JS("[]")
	}
	out := make([]ClubGroupDialogJSON, 0, len(groups))
	for _, g := range groups {
		row := clubGroupRowOf(g, 0, time.Now())
		num := func(key string) string { return strings.TrimSpace(normalizeDigits(g.Field(key))) }
		contractType := strings.TrimSpace(g.Field("contract_type"))
		if contractType == "" {
			contractType = "catering"
		}
		discountType := strings.TrimSpace(g.Field("discount_type"))
		if discountType != "fixed" {
			discountType = "percentage"
		}
		billingCycle := strings.TrimSpace(g.Field("billing_cycle"))
		if billingCycle == "" {
			billingCycle = "monthly"
		}
		out = append(out, ClubGroupDialogJSON{
			ID:                g.ID,
			Name:              g.Label(),
			Description:       strings.TrimSpace(g.Field("description")),
			ContactName:       strings.TrimSpace(g.Field("contact_name")),
			ContactPhone:      strings.TrimSpace(normalizeDigits(g.Field("contact_phone"))),
			ContractNumber:    strings.TrimSpace(g.Field("contract_number")),
			ContractType:      contractType,
			Headcount:         num("headcount"),
			MealsPerDay:       num("meals_per_day"),
			BillingCycle:      billingCycle,
			BillingDay:        num("billing_day"),
			ServiceAddress:    strings.TrimSpace(g.Field("service_address")),
			DiscountType:      discountType,
			DiscountValue:     num("discount_value"),
			MaxDiscountAmount: num("max_discount_amount"),
			ValidFrom:         isoDateOrEmpty(g.Field("valid_from")),
			ValidUntil:        isoDateOrEmpty(g.Field("valid_until")),
			Notes:             strings.TrimSpace(g.Field("notes")),
			CreditEnabled:     g.Field("credit_enabled") == "true",
			Active:            g.Field("is_active") != "false",
			DiscountText:      row.DiscountText,
			ValidityText:      row.ValidityText,
			StatusLabel:       row.StatusLabel,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	raw, mErr := json.Marshal(out)
	if mErr != nil {
		return template.JS("[]")
	}
	return template.JS(raw)
}

// clubGroupFormError خطای فرم گروه را بدون بستن دیالوگ گزارش می‌کند: با
// HX-Request پاسخ swap نمی‌شود و فقط پیام برای همان دیالوگ فرستاده می‌شود تا
// چیزی که کاربر تایپ کرده از دست نرود.
func (a *Application) clubGroupFormError(w http.ResponseWriter, r *http.Request, msg string) {
	if r.Header.Get("HX-Request") == "true" {
		payload, _ := json.Marshal(map[string]string{"message": msg})
		w.Header().Set("HX-Reswap", "none")
		w.Header().Set("HX-Trigger", `{"club-group-error":`+string(payload)+`}`)
		w.WriteHeader(http.StatusOK)
		return
	}
	a.navigatePage(w, r, "club-groups", msg)
}

// orderAdvanceTarget مرحله بعدی فعال از وضعیت فعلی؛ اگر مرحله‌ای نمانده باشد
// رشته خالی برمی‌گردد (یعنی فقط «تکمیل» ممکن است). مراحل غیرفعال رد می‌شوند.
func orderAdvanceTarget(statusKey string, stages []string) (target, actionLabel string) {
	chain := []string{"accepted", "preparing", "ready"}
	idx := -1
	for i, k := range chain {
		if k == statusKey {
			idx = i
		}
	}
	// وضعیت‌های نهایی (تکمیل‌شده/لغوشده) و نامشخص هیچ «مرحله بعدی» ندارند؛
	// فقط pending بیرون از زنجیره است و به اولین مرحله فعال می‌رود.
	if idx < 0 && statusKey != "pending" {
		return "", ""
	}
	labels := map[string]string{"accepted": "تأیید سفارش", "preparing": "شروع آماده‌سازی", "ready": "آماده تحویل"}
	for i := idx + 1; i < len(chain); i++ {
		for _, s := range stages {
			if s == chain[i] {
				return chain[i], labels[chain[i]]
			}
		}
	}
	return "", ""
}

// orderPrimaryAction اقدام اصلی ردیف/کارت سفارش با احترام به مراحل فعال:
// اول مرحله بعدی، وگرنه تکمیل سفارش.
func orderPrimaryAction(statusKey string, stages []string) (target, label string) {
	if t, l := orderAdvanceTarget(statusKey, stages); t != "" {
		return t, l
	}
	switch statusKey {
	case "pending", "accepted", "preparing", "ready":
		return "completed", "تکمیل و تسویه"
	}
	return "", ""
}

// normalizeDigits ارقام فارسی/عربی را به انگلیسی برمی‌گرداند (ورودی‌های کاربر).
func normalizeDigits(s string) string {
	replacer := strings.NewReplacer(
		"۰", "0", "۱", "1", "۲", "2", "۳", "3", "۴", "4", "۵", "5", "۶", "6", "۷", "7", "۸", "8", "۹", "9",
		"٠", "0", "١", "1", "٢", "2", "٣", "3", "٤", "4", "٥", "5", "٦", "6", "٧", "7", "٨", "8", "٩", "9",
		"٬", "", ",", "", " ", "",
	)
	return replacer.Replace(s)
}

// tomanFormToIRR مقدار تومانی فرم را به رشته ریال تبدیل می‌کند.
func tomanFormToIRR(raw string) (string, bool) {
	digits := normalizeDigits(strings.TrimSpace(raw))
	if digits == "" {
		return "", false
	}
	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || n < 0 {
		return "", false
	}
	return strconv.FormatInt(n*10, 10), true
}

// orderRelTimeFa زمان نسبی فارسی مانند fromNowPersian وب.
func orderRelTimeFa(raw string) string {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "چند لحظه پیش"
	case d < time.Hour:
		return store.FormatPersianNumber(strconv.Itoa(int(d.Minutes()))) + " دقیقه پیش"
	case d < 24*time.Hour:
		return store.FormatPersianNumber(strconv.Itoa(int(d.Hours()))) + " ساعت پیش"
	default:
		return store.FormatPersianNumber(strconv.Itoa(int(d.Hours()/24))) + " روز پیش"
	}
}

// normalizeNumInput ارقام فارسی/عربی و فاصله را برای مقایسه شماره سفارش
// (تأیید تایپی حذف در وب) به ارقام لاتین تبدیل می‌کند.
func normalizeNumInput(v string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(v) {
		switch {
		case r >= '۰' && r <= '۹':
			b.WriteRune('0' + (r - '۰'))
		case r >= '٠' && r <= '٩':
			b.WriteRune('0' + (r - '٠'))
		case r == '٫':
			// ممیز فارسی (U+066B) — برای ParseFloat به نقطه تبدیل می‌شود.
			b.WriteRune('.')
		case r == ' ' || r == '‌':
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// DashPopularRow ردیف محصولات پرفروش امروز داشبورد.
type DashPopularRow struct {
	Rank    int
	Name    string
	Qty     int
	Percent int
}

// ProductRow ردیف لیست محصولات هم‌تراز با جدول محصولات داشبورد وب.
type ProductRow struct {
	Rank            int
	Entity          store.Entity
	CategoryLabel   string
	OrdersCount     int
	StatusKey       string // in | low | out
	StockDisplay    string
	ImageURL        string
	DiscountPercent int    // تخفیف فعال دسته‌بندی روی این محصول
	BasePriceToman  string // قیمت پیش از تخفیف (تومان)
	Featured        bool   // «پیشنهاد روز»
	Shortage        bool   // کمبود مواد اولیه دارد و علتش قابل دیدن است
}

// HasDiscount یعنی قیمت این محصول با تخفیف دسته‌بندی کم شده و باید قیمت
// قبلی هم کنارش دیده شود.
func (r ProductRow) HasDiscount() bool { return r.DiscountPercent > 0 && r.BasePriceToman != "" }

// InvRow ردیف لیست موجودی هم‌تراز با جدول انبار داشبورد وب.
type InvRow struct {
	Entity       store.Entity
	StatusKey    string // in | low | out | negative
	Negative     bool   // موجودی زیر صفر است (کمبود واقعی انبار)
	StockDisplay string
	ValueToman   string
	Location     string
	Category     string
	// مثل نسخهٔ وب، عدد موجودی و واحد جدا نمایش داده می‌شوند تا عدد بزرگ
	// و واحد کنارش بنشیند؛ حد سفارش هم زیر عدد می‌آید.
	QtyDisplay string
	UnitShort  string
	MinOrderFa string
	SKU        string
	UpdatedFa  string
	UpdatedRel string
	// کمبود نسبت به حد سفارش: برای دکمهٔ «چه چیزی کم است؟» در جدول انبار.
	HasShortfall         bool
	ShortfallFa          string
	ShortfallQty         string
	ShortfallValueToman  string
}

// InvCategoryRow ردیف جدول «دسته‌بندی مواد اولیه» (categories-page وب):
// دستهٔ انبار با تعداد اقلام منتسب، ترتیب نمایش و وضعیت.
type InvCategoryRow struct {
	Entity    store.Entity
	Items     int
	ItemsFa   string
	SortOrder int
	Active    bool
}

// StatusText برچسب وضعیت موجودی قلم انبار مانند نسخه وب.
func (r InvRow) StatusText() string {
	switch r.StatusKey {
	case "negative":
		return "موجودی منفی"
	case "low":
		return "کم‌موجودی"
	case "out":
		return "پایان‌یافته"
	default:
		return "موجود"
	}
}

// shortUnitLabel واحد را کوتاه می‌کند («گرم (g)» ← «گرم») تا کنار عدد
// موجودی مثل نسخهٔ وب بنشیند.
func shortUnitLabel(label string) string {
	if i := strings.Index(label, " ("); i > 0 {
		return label[:i]
	}
	return label
}

// jalaliDayMonth تاریخ ISO را به شکل «۲۴ مهر ۱۴۰۵ — ۱۳:۳۹» برمی‌گرداند؛
// ستون «به‌روزرسانی» جدول انبار از همین استفاده می‌کند.
func jalaliDayMonth(iso string) string {
	t, ok := JalaliTimeOf(iso)
	if !ok {
		return "—"
	}
	loc := t.In(tehranZone())
	jy, jm, jd := GregorianToJalali(loc.Year(), int(loc.Month()), loc.Day())
	name := ""
	if jm >= 1 && jm <= 12 {
		name = jalaliMonthNames[jm]
	}
	return store.ToPersianDigits(fmt.Sprintf("%d %s %d — %02d:%02d", jd, name, jy, loc.Hour(), loc.Minute()))
}

// relativeFa فاصلهٔ زمانی تا حالا را به فارسی می‌گوید («۳ روز پیش»).
func relativeFa(iso string) string {
	t, ok := JalaliTimeOf(iso)
	if !ok {
		return "—"
	}
	diff := time.Since(t)
	if diff < 0 {
		diff = 0
	}
	switch {
	case diff < time.Minute:
		return "همین حالا"
	case diff < time.Hour:
		return store.ToPersianDigits(fmt.Sprintf("%d دقیقه پیش", int(diff.Minutes())))
	case diff < 24*time.Hour:
		return store.ToPersianDigits(fmt.Sprintf("%d ساعت پیش", int(diff.Hours())))
	default:
		return store.ToPersianDigits(fmt.Sprintf("%d روز پیش", int(diff.Hours()/24)))
	}
}

// StatusText برچسب وضعیت موجودی محصول مانند نسخه وب.
func (r ProductRow) StatusText() string {
	switch r.StatusKey {
	case "low":
		return "کم‌موجودی"
	case "out":
		return "ناموجود"
	default:
		return "موجود"
	}
}

func New(dataDir string) (*Application, error) {
	isDefaultUserDir := false
	if dataDir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return nil, err
		}
		dataDir = filepath.Join(base, "CoffishDesktop")
		isDefaultUserDir = true
	}
	dbName := "offline.sqlite"
	if isDefaultUserDir {
		dbName = "coffish-desktop.db"
	}
	s, err := store.Open(filepath.Join(dataDir, dbName))
	if err != nil {
		return nil, err
	}
	if err = s.MigratePrinterSchema(context.Background()); err != nil {
		s.Close()
		return nil, err
	}
	if _, err = s.RecoverInterruptedPrintJobs(context.Background()); err != nil {
		s.Close()
		return nil, err
	}
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		s.Close()
		return nil, err
	}
	funcMap := template.FuncMap{
		"pretty": func(raw json.RawMessage) string {
			var dst any
			d := json.NewDecoder(strings.NewReader(string(raw)))
			d.UseNumber()
			if d.Decode(&dst) != nil {
				return string(raw)
			}
			out, _ := json.MarshalIndent(dst, "", "  ")
			return string(out)
		},
		"faDigits": store.ToPersianDigits,
		// شماره سفارش نمایشی: پیشوند قدیمی F- حذف می‌شود، فقط عدد (فارسی).
		"ordno": func(s string) string {
			return store.ToPersianDigits(displayOrderNumber(s))
		},
		"faNum":     store.FormatPersianNumber,
		"toman":     store.FormatIRRToToman,
		"jalaliISO": FormatJalaliShort,
		"unitLabel": store.PersianUnitName,
		"faInt": func(n int) string {
			return store.FormatPersianNumber(strconv.Itoa(n))
		},
		"faInt64": func(n int64) string {
			return store.FormatPersianNumber(strconv.FormatInt(n, 10))
		},
	}
	t, err := template.New("app").Funcs(funcMap).ParseFS(assets, "ui/*.html")
	if err != nil {
		s.Close()
		return nil, err
	}
	a := &Application{
		store:        s,
		uploadDir:    filepath.Join(dataDir, "uploads"),
		sessionsPath: filepath.Join(dataDir, "sessions.json"),
		sessions:     loadSessionsFile(filepath.Join(dataDir, "sessions.json")),
		csrf:         hex.EncodeToString(b),
		templates:    t,
		sender:       printer.NewSender(),
		scanSvc:      printer.NewScanner(printer.DefaultScanPolicy(), nil),
		probeSvc:     printer.NewProber(nil),
		allowed: map[string]bool{
			"localhost":       true,
			"127.0.0.1":       true,
			"::1":             true,
			"wails.localhost": true,
			"wails":           true,
		},
	}
	raw, _ := assets.ReadFile("contracts/catalog.json")
	if err = json.Unmarshal(raw, &a.endpoints); err != nil {
		s.Close()
		return nil, err
	}
	for _, host := range strings.Split(os.Getenv("COFFISH_ALLOWED_HOSTS"), ",") {
		if host = strings.TrimSpace(host); host != "" {
			a.allowed[host] = true
		}
	}
	base, id, token := os.Getenv("COFFISH_API_URL"), os.Getenv("COFFISH_DEVICE_ID"), os.Getenv("COFFISH_DEVICE_TOKEN")
	if base != "" || id != "" || token != "" {
		a.client, err = syncclient.New(base, id, token)
		if err != nil {
			s.Close()
			return nil, err
		}
		if err = s.Bind(context.Background(), a.client.BaseURL+"|"+id); err != nil {
			s.Close()
			return nil, err
		}
	} else if isDefaultUserDir {
		_ = a.SeedIfEmpty(context.Background())
	}
	return a, nil
}

func (a *Application) Close() error { return a.store.Close() }

// SeedIfEmpty populates authentic Iranian café initial data when launching the standalone desktop executable.
func (a *Application) SeedIfEmpty(ctx context.Context) error {
	st := a.store.Stats(ctx)
	if st.CategoriesCount == 0 && st.ProductsCount == 0 && st.OrdersCount == 0 {
		return a.store.SeedIranianCafeData(ctx)
	}
	return nil
}

func (a *Application) Handler() http.Handler {
	mux := http.NewServeMux()
	sub, _ := fs.Sub(assets, "ui/static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(sub))))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		a.render(w, r, "")
	})
	mux.HandleFunc("GET /backup.json", a.downloadBackup)
	mux.HandleFunc("POST /sync", a.refresh)
	mux.HandleFunc("POST /catalog", a.queueCatalog)

	// Full standalone desktop actions (no external server required)
	mux.HandleFunc("POST /local/seed-demo", a.handleSeedDemo)
	mux.HandleFunc("POST /local/save", a.handleLocalSave)
	mux.HandleFunc("POST /local/tables-bulk", a.handleTablesBulk)
	mux.HandleFunc("POST /local/delete", a.handleLocalDelete)
	mux.HandleFunc("POST /local/pos-order", a.handleCreatePOSOrder)
	mux.HandleFunc("POST /local/order-status", a.handleOrderStatus)
	mux.HandleFunc("POST /local/order-edit", a.handleOrderEdit)
	mux.HandleFunc("POST /local/order-defer", a.handleOrderDefer)
	mux.HandleFunc("POST /local/order-transfer", a.handleOrderTransfer)
	mux.HandleFunc("POST /local/order-advance", a.handleOrderAdvance)
	mux.HandleFunc("POST /local/split-pay", a.handleSplitPay)
	mux.HandleFunc("POST /local/split-undo", a.handleSplitUndo)
	mux.HandleFunc("POST /local/pos-stages", a.handlePOSStages)
	mux.HandleFunc("POST /local/club-customer", a.handleClubCustomer)
	mux.HandleFunc("POST /local/club-credit-toggle", a.handleClubCreditToggle)
	mux.HandleFunc("POST /local/club-payment", a.handleClubPayment)
	mux.HandleFunc("POST /local/club-payment-confirm", a.handleClubPaymentConfirm)
	mux.HandleFunc("POST /local/club-group", a.handleClubGroup)
	mux.HandleFunc("POST /local/club-group-toggle", a.handleClubGroupToggle)
	mux.HandleFunc("POST /local/club-group-credit", a.handleClubGroupCredit)
	mux.HandleFunc("POST /local/club-group-delete", a.handleClubGroupDelete)
	mux.HandleFunc("POST /local/club-group-member", a.handleClubGroupMember)
	mux.HandleFunc("POST /local/club-group-member-remove", a.handleClubGroupMemberRemove)
	mux.HandleFunc("POST /local/movement", a.handleStockMovement)
	mux.HandleFunc("POST /local/waste-void", a.handleWasteVoid)
	mux.HandleFunc("POST /local/recipe", a.handleSaveRecipe)
	mux.HandleFunc("POST /local/debtor-pay", a.handleDebtorPay)
	mux.HandleFunc("POST /local/debtor-create", a.handleDebtorCreate)
	mux.HandleFunc("GET /local/debtors-export", a.handleDebtorsExport)
	mux.HandleFunc("POST /local/task-toggle", a.handleTaskToggle)
	mux.HandleFunc("POST /local/day-close", a.handleDayClose)
	mux.HandleFunc("POST /local/printer-toggle", a.handlePrinterToggle)
	mux.HandleFunc("POST /local/print-policy", a.handlePrintPolicy)
	mux.HandleFunc("POST /local/printer-route", a.handlePrinterRouteSave)
	mux.HandleFunc("POST /local/printer-route-toggle", a.handlePrinterRouteToggle)
	mux.HandleFunc("POST /local/print-order", a.handlePrintOrder)
	mux.HandleFunc("POST /local/print-retry", a.handlePrintRetry)
	mux.HandleFunc("POST /local/print-test", a.handlePrintTest)
	mux.HandleFunc("POST /local/printer-save", a.handlePrinterSave)
	mux.HandleFunc("POST /local/printer-active", a.handlePrinterActive)
	mux.HandleFunc("POST /local/printer-probe", a.handlePrinterProbe)
	mux.HandleFunc("POST /local/printer-delete", a.handlePrinterDelete)
	mux.HandleFunc("POST /local/printer-usb-scan", a.handlePrinterUSBScan)
	mux.HandleFunc("POST /local/printer-queue-scan", a.handlePrinterQueueScan)
	mux.HandleFunc("POST /local/printer-scan", a.handlePrinterScanStart)
	mux.HandleFunc("GET /local/printer-scan-status", a.handlePrinterScanStatus)
	mux.HandleFunc("POST /local/printer-scan-cancel", a.handlePrinterScanCancel)
	mux.HandleFunc("POST /local/print-queue-retry", a.handlePrintQueueRetry)
	mux.HandleFunc("POST /local/print-queue-retry-all", a.handlePrintQueueRetryAll)
	mux.HandleFunc("POST /local/print-queue-cancel", a.handlePrintQueueCancel)
	mux.HandleFunc("POST /local/print-queue-delete", a.handlePrintQueueDelete)
	mux.HandleFunc("POST /local/print-queue-clear", a.handlePrintQueueClear)
	mux.HandleFunc("POST /local/export-backup", a.handleExportBackup)
	mux.HandleFunc("GET /local/products-export", a.handleProductsExport)
	mux.HandleFunc("GET /local/inventory-export", a.handleInventoryExport)
	mux.HandleFunc("POST /license/activate", a.handleLicenseActivate)
	mux.HandleFunc("POST /login", a.handleLogin)
	mux.HandleFunc("POST /register", a.handleRegister)
	mux.HandleFunc("POST /logout", a.handleLogout)
	mux.HandleFunc("GET /logout", a.handleLogout)
	mux.HandleFunc("GET /uploads/", a.handleUploadServe)
	mux.HandleFunc("POST /local/products-import", a.handleProductsImport)
	mux.HandleFunc("POST /local/products-bulk", a.handleProductsBulk)
	mux.HandleFunc("POST /local/category-availability", a.handleCategoryAvailability)
	mux.HandleFunc("POST /local/category-discount", a.handleCategoryDiscount)

	a.registerLocalAPIRoutes(mux)

	contractsFS, _ := fs.Sub(assets, "contracts")
	mux.Handle("GET /reference/", http.StripPrefix("/reference/", http.FileServer(http.FS(contractsFS))))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = strings.Trim(r.Host, "[]")
		}
		host = strings.ToLower(host)
		if !a.allowed[host] && !strings.HasSuffix(host, ".e2b.app") {
			http.Error(w, "host not allowed", http.StatusForbidden)
			return
		}
		// Marker header used by the desktop launcher to recognise its own
		// already-running backend instead of starting a second database writer.
		w.Header().Set("X-MenuVex-App", "offline")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; font-src 'self' data:; img-src 'self' data:; connect-src 'self'; frame-ancestors 'self' *; base-uri 'self'; form-action 'self'")
		if r.Method != "GET" && r.Method != "HEAD" && !strings.HasPrefix(r.URL.Path, "/api/v1/") {
			bodyLimit := int64(1 << 20)
			if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
				bodyLimit = 9 << 20 // آپلود تصویر محلی تا ۸ مگابایت + فرم
			}
			r.Body = http.MaxBytesReader(w, r.Body, bodyLimit)
			var parseErr error
			if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
				parseErr = r.ParseMultipartForm(9 << 20)
			} else {
				parseErr = r.ParseForm()
			}
			if parseErr != nil {
				http.Error(w, "invalid form", 400)
				return
			}
			if subtle.ConstantTimeCompare([]byte(r.Form.Get("csrf")), []byte(a.csrf)) != 1 {
				http.Error(w, "invalid CSRF token", 403)
				return
			}
		}
		if !a.authDisabled {
			if (r.URL.Path == "/license" || r.URL.Path == "/login" || r.URL.Path == "/register") && r.Method == "GET" {
				if a.enforceAuth(w, r) {
					return
				}
			} else if !authExemptPath(r.URL.Path) {
				if a.enforceAuth(w, r) {
					return
				}
			} else if r.URL.Path == "/logout" && r.Method == "GET" {
				a.handleLogout(w, r)
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}

func (a *Application) render(w http.ResponseWriter, r *http.Request, message string) {
	page := r.URL.Query().Get("page")
	if page == "" {
		page = "home"
	}
	ctx := r.Context()
	licState := a.LicenseState()
	v := View{
		CSRF:            a.csrf,
		LicenseDaysLeft: licState.DaysLeft,
		LicenseHolder:   licState.Holder,
		Page:            page,
		Message:         message,
		JalaliNow:       FormatJalaliFull(time.Now()),
		Connected:       a.client != nil,
		Stats:           a.store.Stats(ctx),
		Analytics:       a.store.Analytics(ctx),
		Query:           r.URL.Query().Get("q"),
		Total:           len(a.endpoints),
		Units:           []string{"g", "kg", "ml", "l", "pcs", "pack", "portion", "bottle", "box", "shot", "sachet"},
	}
	if sess, ok := a.sessionOf(r); ok {
		v.SessionName = sess.Name
		v.SessionRole = sess.Role
	}
	if prof, pErr := a.store.Entity(ctx, "cafe_profile", "cafe-main-profile"); pErr == nil {
		v.CafeProfile = prof
	}
	v.PrinterEnabled = a.printerEnabled(ctx)
	var err error
	v.Cursor, err = a.store.Cursor(ctx)
	if err != nil {
		http.Error(w, "cannot read local cursor", 500)
		return
	}

	// ساخت/ویرایش گروه باشگاه دیگر صفحهٔ جداگانه ندارد: همان فهرست گروه‌ها
	// رندر می‌شود و دیالوگ گروه روی آن باز می‌شود. لینک‌های قدیمی
	// (?page=club-group-form) هم به همین مسیر می‌رسند.
	if page == "club-group-form" {
		page = "club-groups"
		v.Page = page
		v.ClubGroupDialog = "new"
		if gid := strings.TrimSpace(r.URL.Query().Get("id")); gid != "" {
			v.ClubGroupDialog = "edit"
			v.ClubGroupEditID = gid
		}
	}
	// دیالوگ گروه روی هر سه صفحهٔ باشگاه در دسترس است (فهرست گروه‌ها، جزئیات
	// گروه و مشتریان) تا ویرایش همیشه در همان صفحه و بدون ترک کردن باز شود.
	if page == "club-groups" || page == "club-group" || page == "customers" {
		switch strings.TrimSpace(r.URL.Query().Get("dlg")) {
		case "new":
			v.ClubGroupDialog = "new"
		case "edit":
			v.ClubGroupDialog = "edit"
			if gid := strings.TrimSpace(r.URL.Query().Get("id")); gid != "" {
				v.ClubGroupEditID = gid
			}
		}
	}

	switch page {
	case "home":
		v.Entities, _ = a.store.Entities(ctx, "order")
		v.Tables, _ = a.store.Entities(ctx, "table")
		v.Ingredients, _ = a.store.Entities(ctx, "ingredient")
		v.Tasks, _ = a.store.Entities(ctx, "task")
		v.Expenses, _ = a.store.Entities(ctx, "expense")
		a.fillDashboard(&v, r)
	case "pos":
		tok := make([]byte, 16)
		if _, rErr := rand.Read(tok); rErr == nil {
			v.POSFormToken = hex.EncodeToString(tok)
		}
		v.Categories, _ = a.store.Entities(ctx, "category")
		v.Products, _ = a.store.Entities(ctx, "product")
		// «ناموجودی» صندوق: محصولی که مواد اولیهٔ رسپی‌اش در انبار کافی نیست
		// (یا مستقیم غیرفعال/بدون موجودی است) نباید قابل افزودن به سبد باشد.
		v.POSStock = a.store.ProductPortions(ctx)
		for _, pr := range v.Products {
			// پیش‌فرض: بدون رسپی = بدون محدودیت مواد اولیه.
			if _, seen := v.POSStock[pr.ID]; !seen {
				v.POSStock[pr.ID] = store.ProductPortion{Portions: -1}
			}
			if pr.Field("is_available") == "false" || strings.EqualFold(pr.Field("status"), "inactive") {
				v.POSStock[pr.ID] = store.ProductPortion{Portions: 0, Missing: "غیرفعال در منو"}
				continue
			}
			if raw := strings.TrimSpace(pr.Field("stock")); raw != "" {
				if st, stErr := strconv.ParseFloat(raw, 64); stErr == nil && st <= 0 {
					v.POSStock[pr.ID] = store.ProductPortion{Portions: 0, Missing: "موجودی محصول"}
				}
			}
		}
		v.POSStockJSON = posStockJSON(v.POSStock)
		v.Tables, _ = a.store.Entities(ctx, "table")
		v.Customers, _ = a.store.Entities(ctx, "customer")
		// گروه‌های باشگاه برای تب «حساب شرکتی / گروه» دیالوگ بدهکاران صندوق
		v.ClubGroupOpts, _ = a.store.Entities(ctx, "customer_group")
		v.Entities, _ = a.store.Entities(ctx, "order")
		// گروه‌بندی محصولات صندوق بر اساس دسته (مانند وب: سرتیتر هر دسته بالای کارت‌هایش)
		{
			byCat := map[string][]store.Entity{}
			for _, p := range v.Products {
				cid := p.Field("category_id")
				byCat[cid] = append(byCat[cid], p)
			}
			known := map[string]bool{}
			for _, c := range v.Categories {
				known[c.ID] = true
				if items := byCat[c.ID]; len(items) > 0 {
					v.POSGroups = append(v.POSGroups, POSGroup{ID: c.ID, Label: c.Label(), Products: items})
				}
			}
			var rest []store.Entity
			for _, p := range v.Products {
				if cid := p.Field("category_id"); !known[cid] {
					rest = append(rest, p)
				}
			}
			if len(rest) > 0 {
				v.POSGroups = append(v.POSGroups, POSGroup{ID: "__other", Label: "سایر محصولات", Products: rest})
			}
		}
		v.Printers, _ = a.store.Entities(ctx, "printer")
		// کارت‌های میز صندوق با سفارش باز و مراحل وضعیت (مانند pos-table-card وب)
		{
			stages := a.posEnabledStages(ctx)
			for _, d := range posStageDefs {
				en := false
				for _, s := range stages {
					if s == d.Key {
						en = true
					}
				}
				v.POSStages = append(v.POSStages, POSStageOpt{Key: d.Key, Label: d.Label, Desc: d.Desc, Enabled: en})
			}
			v.POSStageCount = len(stages) + 1
			openByTable := map[string]store.Entity{}
			var allOrders []store.Entity
			if orders, oErr := a.store.Entities(ctx, "order"); oErr == nil {
				allOrders = orders
				for _, o := range orders {
					switch strings.ToLower(strings.TrimSpace(o.Field("status"))) {
					case "pending", "accepted", "preparing", "ready":
						if tid := strings.TrimSpace(o.Field("table_id")); tid != "" {
							if cur, dup := openByTable[tid]; !dup || o.Field("created_at") > cur.Field("created_at") {
								openByTable[tid] = o
							}
						}
					}
				}
			}
			stepDefs := []struct {
				Key   string
				Label string
			}{{"pending", "ثبت"}, {"accepted", "تأیید"}, {"preparing", "آماده‌سازی"}, {"ready", "آماده"}, {"completed", "تکمیل"}}
			for _, t := range v.Tables {
				row := POSTableRow{Table: t}
				tStatus := strings.ToLower(strings.TrimSpace(t.Field("status")))
				if o, ok := openByTable[t.ID]; ok {
					row.HasOrder = true
					row.OrderID = o.ID
					row.Number = displayOrderNumber(o.Field("order_number"))
					row.TotalFa = store.FormatPersianNumber(new(big.Int).Div(parseIRRBig(o.PriceIRR()), big.NewInt(10)).String())
					items := 0
					for _, li := range o.OrderItems() {
						items += li.Quantity
					}
					row.ItemsCount = items
					if ct, pErr := time.Parse(time.RFC3339, o.Field("created_at")); pErr == nil {
						row.TimeLabel = FormatJalaliClock(ct)
					}
					key := strings.ToLower(strings.TrimSpace(o.Field("status")))
					row.StateKey = key
					row.StateLabel = o.StatusLabel()
					nextTarget, nextLabel := orderPrimaryAction(key, stages)
					row.NextTarget = nextTarget
					row.NextLabel = nextLabel
					row.ShowComplete = nextTarget == "completed"
					flowIdx := map[string]int{"pending": 0, "accepted": 1, "preparing": 2, "ready": 3}
					cur := flowIdx[key]
					for i, sd := range stepDefs {
						if sd.Key != "pending" && sd.Key != "completed" {
							enabled := false
							for _, s := range stages {
								if s == sd.Key {
									enabled = true
								}
							}
							if !enabled {
								continue
							}
						}
						row.Steps = append(row.Steps, POSStep{Label: sd.Label, Done: i < cur, Current: i == cur})
					}
					_, _, splitPaid, splitRem := store.SplitBillOf(o)
					_ = splitPaid
					row.SplitRemainingFa = store.FormatPersianNumber(strconv.FormatInt(splitRem/10, 10))
					row.SplitSettled = splitRem == 0 && o.Field("split_status") == "PAID"
				} else if tStatus == "reserved" {
					row.StateKey = "reserved"
					row.StateLabel = "رزرو"
				} else if tStatus == "disabled" || tStatus == "cleaning" {
					row.StateKey = tStatus
					row.StateLabel = t.StatusLabel()
				} else {
					row.StateKey = "free"
					row.StateLabel = "آزاد"
				}
				v.POSTables = append(v.POSTables, row)
			}
			// سفارش‌های اخیر صندوق (جدیدترین اول) + آخرین سفارش برای چاپ سریع
			sorted := append([]store.Entity(nil), allOrders...)
			sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Field("created_at") > sorted[j].Field("created_at") })
			for i, o := range sorted {
				if i == 0 {
					v.POSLastOrderID = o.ID
					v.POSLastOrderTotalFa = store.FormatPersianNumber(new(big.Int).Div(parseIRRBig(o.PriceIRR()), big.NewInt(10)).String())
				}
				if i >= 12 {
					break
				}
				rr := POSRecentRow{
					ID: o.ID, Number: displayOrderNumber(o.Field("order_number")),
					TotalFa:     store.FormatPersianNumber(new(big.Int).Div(parseIRRBig(o.PriceIRR()), big.NewInt(10)).String()),
					StatusLabel: o.StatusLabel(), StatusKey: strings.ToUpper(strings.TrimSpace(o.Field("status"))),
					TableLabel:   strings.TrimSpace(o.Field("table_label")),
					CustomerName: strings.TrimSpace(o.Field("customer_name")),
				}
				lowerStatus := strings.ToLower(strings.TrimSpace(o.Field("status")))
				rr.IsCompleted = lowerStatus == "completed"
				rr.IsCancelled = lowerStatus == "cancelled" || lowerStatus == "canceled"
				rr.IsOpen = !rr.IsCompleted && !rr.IsCancelled
				rr.HasTable = rr.TableLabel != ""
				// مبدأ سفارش مانند وب: میز، یا «حضوری • بدون میز»/بیرون‌بر/پیک
				switch {
				case rr.HasTable:
					rr.OriginLabel = rr.TableLabel
				case strings.TrimSpace(o.Field("fulfillment_type")) == "outside":
					rr.OriginLabel = "بیرون‌بر • بدون میز"
				case strings.TrimSpace(o.Field("fulfillment_type")) == "delivery":
					rr.OriginLabel = "پیک • بدون میز"
				default:
					rr.OriginLabel = "حضوری • بدون میز"
				}
				// خلاصه اقلام: «۳ قلم: لاته، …»
				if lines, _, _, _ := store.SplitBillOf(o); len(lines) > 0 {
					qtySum := 0
					for _, ln := range lines {
						qtySum += ln.Quantity
					}
					rr.ItemsLabel = store.FormatPersianNumber(strconv.Itoa(qtySum)) + " قلم: " + lines[0].Title
				}
				// پیشرفت مراحل فعال + اقدام اصلی (مرحله بعد یا تکمیل)
				enabledStages := make([]string, 0, len(v.POSStages))
				for _, stg := range v.POSStages {
					if stg.Enabled {
						enabledStages = append(enabledStages, stg.Key)
					}
				}
				steps := append([]string{"pending"}, enabledStages...)
				steps = append(steps, "completed")
				cur := 0
				for i, sk := range steps {
					if sk == lowerStatus {
						cur = i
					}
				}
				if rr.IsCompleted {
					cur = len(steps) - 1
				}
				if len(steps) > 1 {
					rr.ProgressPct = cur * 100 / (len(steps) - 1)
				}
				if rr.IsOpen {
					rr.NextTarget, rr.NextLabel = orderPrimaryAction(lowerStatus, enabledStages)
					if rr.NextTarget == "completed" {
						rr.NextLabel = "تکمیل و صدور فاکتور"
					}
				}
				if ct, pErr := time.Parse(time.RFC3339, o.Field("created_at")); pErr == nil {
					rr.TimeLabel = FormatJalaliClock(ct)
				}
				// نمای روزانه: سفارش‌های تمام‌شده روزهای قبل در دیالوگ نمی‌آیند
				// (نیمه‌شب تهران تازه می‌شود)؛ بازهای قدیمی برای تسویه می‌مانند.
				if !isTehranToday(o.Field("created_at")) && (rr.IsCompleted || rr.IsCancelled) {
					continue
				}
				v.POSRecent = append(v.POSRecent, rr)
			}
			// در دیالوگ سفارش‌های صندوق هم تکمیل‌شده‌ها (بعد لغوشده‌ها) پایین می‌روند.
			posRecentRank := func(rr POSRecentRow) int {
				if rr.IsCancelled {
					return 2
				}
				if rr.IsCompleted {
					return 1
				}
				return 0
			}
			sort.SliceStable(v.POSRecent, func(i, j int) bool { return posRecentRank(v.POSRecent[i]) < posRecentRank(v.POSRecent[j]) })
			// ردیف‌های باشگاه برای دیالوگ «انتخاب مشتری برای سفارش» (pos-club-picker وب):
			// همان سازنده فهرست باشگاه مشتریان + ردیف‌های گروه/شرکت.
			{
				debtors, _ := a.store.Entities(ctx, "debtor")
				v.ClubRows = clubBuildRows(v.Customers, allOrders, debtors, v.ClubGroupOpts)
				memberCounts := map[string]int{}
				for _, c := range v.Customers {
					if gid := strings.TrimSpace(c.Field("group_id")); gid != "" {
						memberCounts[gid]++
					}
				}
				v.ClubGroups = nil
				for _, g := range v.ClubGroupOpts {
					v.ClubGroups = append(v.ClubGroups, clubGroupRowOf(g, memberCounts[g.ID], time.Now()))
				}
			}
			// داده JSON حساب دنگی برای همه سفارش‌های باز (دیالوگ سمت کاربر از همین داده پر می‌شود)
			type splitLineJSON struct {
				Key       string `json:"key"`
				Title     string `json:"title"`
				Qty       int    `json:"qty"`
				PaidQty   int    `json:"paidQty"`
				RemainIRR int64  `json:"remainIrr"`
				UnitIRR   int64  `json:"unitIrr"`
			}
			type splitBillJSON struct {
				OrderID   string          `json:"orderId"`
				Number    string          `json:"number"`
				Table     string          `json:"table"`
				TotalIRR  int64           `json:"totalIrr"`
				PaidIRR   int64           `json:"paidIrr"`
				RemainIRR int64           `json:"remainIrr"`
				Lines     []splitLineJSON `json:"lines"`
				Payments  int             `json:"payments"`
			}
			bills := map[string]splitBillJSON{}
			for _, o := range allOrders {
				switch strings.ToLower(strings.TrimSpace(o.Field("status"))) {
				case "pending", "accepted", "preparing", "ready":
				default:
					continue
				}
				lines, total, paidAmt, rem := store.SplitBillOf(o)
				b := splitBillJSON{OrderID: o.ID, Number: displayOrderNumber(o.Field("order_number")), Table: strings.TrimSpace(o.Field("table_label")), TotalIRR: total, PaidIRR: paidAmt, RemainIRR: rem, Payments: len(strings.Split(strings.TrimSpace(o.Field("split_payments")), "\"id\"")) - 1}
				if strings.TrimSpace(o.Field("split_payments")) == "" {
					b.Payments = 0
				}
				for _, l := range lines {
					b.Lines = append(b.Lines, splitLineJSON{Key: l.Key, Title: l.Title, Qty: l.Quantity, PaidQty: l.PaidQuantity, RemainIRR: l.RemainingIRR, UnitIRR: l.UnitPayableIRR})
				}
				bills[o.ID] = b
			}
			if blob, mErr := json.Marshal(bills); mErr == nil {
				v.POSSplitJSON = template.JS(blob)
			}
			v.POSOpenSplit = strings.TrimSpace(r.URL.Query().Get("split"))
			// حالت ویرایش سفارش: اقلام سفارش باز در سبد صندوق بارگذاری می‌شود
			if editID := strings.TrimSpace(r.URL.Query().Get("edit")); editID != "" {
				if eo, eErr := a.store.Entity(ctx, "order", editID); eErr == nil {
					switch strings.ToLower(strings.TrimSpace(eo.Field("status"))) {
					case "pending", "accepted", "preparing", "ready":
						type editLine struct {
							PID   string `json:"pid"`
							Title string `json:"title"`
							Price int64  `json:"price"`
							Qty   int    `json:"qty"`
							Note  string `json:"note"`
						}
						var els []editLine
						cnt := 0
						for _, li := range eo.OrderItems() {
							price, _ := strconv.ParseInt(strings.TrimSpace(li.UnitPrice), 10, 64)
							els = append(els, editLine{PID: li.ProductID, Title: li.ProductName, Price: price, Qty: li.Quantity, Note: li.Note})
							cnt += li.Quantity
						}
						blob, _ := json.Marshal(els)
						disc, _ := strconv.ParseInt(strings.TrimSpace(eo.Field("discount_irr")), 10, 64)
						extra, _ := strconv.ParseInt(strings.TrimSpace(eo.Field("extra_irr")), 10, 64)
						v.POSEdit = &POSEditState{
							OrderID: eo.ID, Number: displayOrderNumber(eo.Field("order_number")),
							TableLabel:   strings.TrimSpace(eo.Field("table_label")),
							CustomerName: strings.TrimSpace(eo.Field("customer_name")), CustomerPhone: strings.TrimSpace(eo.Field("customer_phone")),
							Note: strings.TrimSpace(eo.Field("note")), ExtraNote: strings.TrimSpace(eo.Field("extra_note")),
							DiscountIRR: disc, ExtraIRR: extra, ItemsJSON: template.JS(blob), ItemsCount: cnt,
						}
					}
				}
			}
		}
	case "products":
		products, pErr := a.store.Entities(ctx, "product")
		v.Categories, _ = a.store.Entities(ctx, "category")
		v.ModifierGroups, _ = a.store.Entities(ctx, "modifier_group")
		// کمبود مواد اولیهٔ هر محصول برای دکمهٔ «چه چیزی کم است؟» در جدول.
		v.POSStock = a.store.ProductPortions(ctx)
		if pErr == nil {
			v.Entities = products
			catLabel := map[string]string{}
			for _, c := range v.Categories {
				catLabel[c.ID] = c.Label()
			}
			ordersCount := map[string]int{}
			if orders, oErr := a.store.Entities(ctx, "order"); oErr == nil {
				for _, o := range orders {
					if strings.EqualFold(o.Field("status"), "cancelled") || strings.EqualFold(o.Field("status"), "canceled") {
						continue
					}
					for _, line := range o.OrderItems() {
						ordersCount[line.ProductID] += line.Quantity
					}
				}
			}
			v.ProductsCategory = strings.TrimSpace(r.URL.Query().Get("category"))
			v.ProductsStatus = strings.TrimSpace(r.URL.Query().Get("status"))
			query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
			for _, prod := range products {
				row := ProductRow{Entity: prod, OrdersCount: ordersCount[prod.ID], ImageURL: prod.Field("image")}
				// تخفیف دسته‌بندی: قیمت فعلی تخفیف‌خورده است و قیمت پایه در
				// base_price مانده؛ هر دو در جدول نشان داده می‌شوند.
				if pct, pctErr := strconv.Atoi(strings.TrimSpace(prod.Field("discount_percent"))); pctErr == nil && pct > 0 {
					if baseRaw := strings.TrimSpace(prod.Field("base_price")); baseRaw != "" {
						row.DiscountPercent = pct
						row.BasePriceToman = store.FormatIRRToToman(baseRaw)
					}
				}
				row.Featured = prod.Field("is_featured") == "true"
				row.Shortage = len(v.POSStock[prod.ID].Shortages) > 0
				row.CategoryLabel = catLabel[prod.Field("category_id")]
				if row.CategoryLabel == "" {
					row.CategoryLabel = "—"
				}
				available := prod.Field("is_available") != "false"
				stockRaw := strings.TrimSpace(prod.Field("stock"))
				stock, stockErr := strconv.ParseFloat(stockRaw, 64)
				hasStock := stockErr == nil && stockRaw != ""
				switch {
				case !available || (hasStock && stock <= 0):
					row.StatusKey = "out"
				case hasStock && stock < 10:
					row.StatusKey = "low"
				default:
					row.StatusKey = "in"
				}
				if hasStock {
					row.StockDisplay = store.FormatPersianNumber(strconv.FormatFloat(stock, 'f', -1, 64))
				} else {
					row.StockDisplay = "—"
				}
				switch row.StatusKey {
				case "out":
					v.ProductsOutOfStock++
				case "low":
					v.ProductsLowStock++
				default:
					v.ProductsInStock++
				}
				v.ProductsTotal++
				if query != "" && !strings.Contains(strings.ToLower(prod.Label()), query) && !strings.Contains(strings.ToLower(prod.Field("description")), query) {
					continue
				}
				if v.ProductsCategory != "" && prod.Field("category_id") != v.ProductsCategory {
					continue
				}
				if v.ProductsStatus != "" && row.StatusKey != v.ProductsStatus {
					continue
				}
				row.Rank = len(v.ProductRows) + 1
				v.ProductRows = append(v.ProductRows, row)
			}
		}
		v.POSStockJSON = posStockJSON(v.POSStock)
	case "product-form":
		v.Kind = "product"
		v.EntityID = strings.TrimSpace(r.URL.Query().Get("id"))
		v.Action = "create"
		v.Categories, _ = a.store.Entities(ctx, "category")
		v.ModifierGroups, _ = a.store.Entities(ctx, "modifier_group")
		v.Ingredients, _ = a.store.Entities(ctx, "ingredient")
		v.Fields = map[string]string{}
		if v.EntityID != "" {
			if e, eErr := a.store.Entity(ctx, "product", v.EntityID); eErr == nil {
				v.Action = "update"
				var values map[string]json.RawMessage
				_ = json.Unmarshal(e.Payload, &values)
				for key, raw := range values {
					var value string
					if json.Unmarshal(raw, &value) != nil {
						value = string(raw)
					}
					v.Fields[key] = value
				}
				// تنوع‌های ذخیره‌شدهٔ محصول برای ویرایش در همان فرم (variants وب).
				var payload struct {
					Variants []struct {
						Title    string          `json:"title"`
						Value    json.RawMessage `json:"value"`
						IsActive *bool           `json:"is_active"`
					} `json:"variants"`
				}
				if json.Unmarshal(e.Payload, &payload) == nil {
					for _, variant := range payload.Variants {
						active := true
						if variant.IsActive != nil {
							active = *variant.IsActive
						}
						v.ProductVariants = append(v.ProductVariants, ProductVariantRow{
							Title:  variant.Title,
							Value:  strings.Trim(string(variant.Value), `"`),
							Active: active,
						})
					}
				}
				// دستور تهیهٔ فعلی محصول برای نمایش در فرم ویرایش.
				if recipes, rErr := a.store.Entities(ctx, "recipe"); rErr == nil {
					for _, rec := range recipes {
						if rec.Field("product_id") != v.EntityID {
							continue
						}
						for _, line := range rec.RecipeLines() {
							v.ProductRecipeLines = append(v.ProductRecipeLines, ProductRecipeLineRow{
								IngredientID: line.IngredientID,
								Quantity:     line.Quantity,
								Unit:         line.Unit,
							})
						}
						break
					}
				}
			}
		}
	case "categories":
		v.Entities, err = a.store.Entities(ctx, "category")
		{
			products, _ := a.store.Entities(ctx, "product")
			counts := map[string]int{}
			for _, p := range products {
				counts[p.Field("category_id")]++
			}
			for _, c := range v.Entities {
				available, _ := store.ProductsAvailableCount(products, c.ID)
				row := CategoryRow{
					Entity:            c,
					Products:          counts[c.ID],
					AvailableProducts: available,
					DiscountPercent:   store.CategoryDiscountPercent(products, c.ID),
				}
				row.AllUnavailable = row.Products > 0 && available == 0
				v.CatRows = append(v.CatRows, row)
			}
		}
	case "ingredients":
		ingredients, iErr := a.store.Entities(ctx, "ingredient")
		v.Movements, _ = a.store.Entities(ctx, "inventory_movement")
		v.WasteEvents, _ = a.store.Entities(ctx, "waste_event")
		v.Suppliers, _ = a.store.Entities(ctx, "supplier")
		v.Purchases, _ = a.store.Entities(ctx, "purchase")
		if iErr == nil {
			v.Entities = ingredients
			v.InvCategory = strings.TrimSpace(r.URL.Query().Get("icategory"))
			v.InvLocation = strings.TrimSpace(r.URL.Query().Get("ilocation"))
			v.InvStatus = strings.TrimSpace(r.URL.Query().Get("istatus"))
			v.InvSort = strings.TrimSpace(r.URL.Query().Get("isort"))
			// مصرف ۳۰ روز گذشته برای پیش‌بینی «در آستانه اتمام» مانند وب
			consumption := map[string]float64{}
			cutoff := time.Now().AddDate(0, 0, -30)
			for _, mv := range v.Movements {
				qty, qErr := strconv.ParseFloat(mv.Field("quantity"), 64)
				if qErr != nil || qty >= 0 {
					continue
				}
				if ts, tErr := time.Parse(time.RFC3339, mv.Field("created_at")); tErr == nil && ts.Before(cutoff) {
					continue
				}
				consumption[mv.Field("ingredient_id")] += -qty
			}
			catSet, locSet := map[string]bool{}, map[string]bool{}
			totalValue := big.NewInt(0)
			for _, ing := range ingredients {
				row := InvRow{Entity: ing, Location: ing.Field("storage_location"), Category: ing.Field("category")}
				if row.Location == "" {
					row.Location = "انبار اصلی"
				}
				if row.Category == "" {
					row.Category = "بدون دسته‌بندی"
				}
				catSet[row.Category] = true
				locSet[row.Location] = true
				stock, _ := strconv.ParseFloat(ing.Field("current_stock"), 64)
				row.Negative = stock < 0
				stockText := store.FormatPersianNumber(strconv.FormatFloat(stock, 'f', -1, 64))
				// موجودی منفی با علامت واضح و جدا از «پایان‌یافته» نشان داده
				// می‌شود تا کمبود واقعی انبار (مثلاً ۳ کیلو زیر صفر) گم نشود.
				if row.Negative {
					stockText = "−" + strings.TrimPrefix(stockText, "-")
				}
				row.StockDisplay = stockText + " " + ing.UnitLabel()
				row.QtyDisplay = stockText
				row.UnitShort = shortUnitLabel(ing.UnitLabel())
				row.SKU = ing.Field("sku")
				minOrder := strings.TrimSpace(ing.Field("reorder_point"))
				if minOrder == "" {
					minOrder = strings.TrimSpace(ing.Field("minimum_stock"))
				}
				if minOrder != "" {
					row.MinOrderFa = store.FormatPersianNumber(minOrder)
				}
				// کمبود نسبت به حد سفارش (و اگر حدی ثبت نشده، هر موجودی منفی):
				// دکمهٔ «چه چیزی کم است؟» و پیش‌پر کردن مقدار خرید در دیالوگ
				// ثبت ورود کالا.
				minOrderRaw := strings.NewReplacer(",", "", "٬", "").Replace(normalizeNumInput(minOrder))
				minOrderVal, minErr := strconv.ParseFloat(minOrderRaw, 64)
				if minErr != nil {
					minOrderVal = 0
				}
				if minOrderVal > stock {
					row.HasShortfall = true
					short := minOrderVal - stock
					row.ShortfallFa = store.FormatPersianNumber(strconv.FormatFloat(short, 'f', -1, 64))
					row.ShortfallQty = strconv.FormatFloat(short, 'f', -1, 64)
					if avg, aErr := strconv.ParseFloat(strings.TrimSpace(ing.Field("average_cost")), 64); aErr == nil && avg > 0 {
						value := new(big.Float).Mul(big.NewFloat(short), big.NewFloat(avg))
						if valueInt, _ := value.Int(nil); valueInt != nil {
							row.ShortfallValueToman = store.FormatIRRToToman(valueInt.String())
						}
					}
				}
				updatedISO := ing.Field("updated_at")
				if strings.TrimSpace(updatedISO) == "" {
					updatedISO = ing.Field("created_at")
				}
				row.UpdatedFa = jalaliDayMonth(updatedISO)
				row.UpdatedRel = relativeFa(updatedISO)
				switch {
				case stock < 0:
					row.StatusKey = "negative"
				case stock == 0:
					row.StatusKey = "out"
				case ing.NeedsReorder():
					row.StatusKey = "low"
				default:
					row.StatusKey = "in"
				}
				avgCost := parseIRRBig(ing.Field("average_cost"))
				val := new(big.Float).Mul(big.NewFloat(stock), new(big.Float).SetInt(avgCost))
				valInt, _ := val.Int(nil)
				totalValue.Add(totalValue, valInt)
				row.ValueToman = store.FormatIRRToToman(valInt.String())
				switch row.StatusKey {
				case "negative":
					v.InvOut++
					v.InvNegative++
				case "out":
					v.InvOut++
				case "low":
					v.InvLow++
				}
				if stock > 0 {
					if daily := consumption[ing.ID] / 30; daily > 0 && stock/daily <= 30 {
						v.InvEndingSoon++
					}
				}
				v.InvTotal++
				if row.StatusKey != "in" && len(v.InvAlerts) < 8 {
					v.InvAlerts = append(v.InvAlerts, row)
				}
				q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
				if q != "" && !strings.Contains(strings.ToLower(ing.Label()), q) && !strings.Contains(strings.ToLower(ing.Field("sku")), q) && !strings.Contains(strings.ToLower(row.Location), q) && !strings.Contains(strings.ToLower(ing.Field("supplier")), q) {
					continue
				}
				if v.InvCategory != "" && row.Category != v.InvCategory {
					continue
				}
				if v.InvLocation != "" && row.Location != v.InvLocation {
					continue
				}
				if v.InvStatus != "" && row.StatusKey != v.InvStatus {
					// فیلتر «پایان‌یافته» اقلام منفی را هم شامل می‌شود (موجودی ≤ ۰).
					if !(v.InvStatus == "out" && row.StatusKey == "negative") {
						continue
					}
				}
				v.InvRows = append(v.InvRows, row)
			}
			v.InvValueToman = store.FormatIRRToToman(totalValue.String())
			for c := range catSet {
				v.InvCategories = append(v.InvCategories, c)
			}
			for l := range locSet {
				v.InvLocations = append(v.InvLocations, l)
			}
			if catEnts, cErr := a.store.Entities(ctx, "ingredient_category"); cErr == nil {
				seen := map[string]bool{}
				merged := v.InvCategories[:0]
				for _, c := range v.InvCategories {
					seen[c] = true
					merged = append(merged, c)
				}
				for _, ce := range catEnts {
					if !seen[ce.Label()] {
						merged = append(merged, ce.Label())
						seen[ce.Label()] = true
					}
				}
				v.InvCategories = merged
				v.InvCategoryEntities = catEnts
			}
			sort.Strings(v.InvCategories)
			sort.Strings(v.InvLocations)
			switch v.InvSort {
			case "stock-asc":
				sort.SliceStable(v.InvRows, func(i, j int) bool {
					a, _ := strconv.ParseFloat(v.InvRows[i].Entity.Field("current_stock"), 64)
					b, _ := strconv.ParseFloat(v.InvRows[j].Entity.Field("current_stock"), 64)
					return a < b
				})
			case "stock-desc":
				sort.SliceStable(v.InvRows, func(i, j int) bool {
					a, _ := strconv.ParseFloat(v.InvRows[i].Entity.Field("current_stock"), 64)
					b, _ := strconv.ParseFloat(v.InvRows[j].Entity.Field("current_stock"), 64)
					return a > b
				})
			case "name":
				sort.SliceStable(v.InvRows, func(i, j int) bool { return v.InvRows[i].Entity.Label() < v.InvRows[j].Entity.Label() })
			}
			// در کارت «هشدارهای فوری» اول اقلام منفی (کمبود واقعی) می‌آیند.
			alertRank := func(k string) int {
				switch k {
				case "negative":
					return 0
				case "out":
					return 1
				case "low":
					return 2
				}
				return 3
			}
			sort.SliceStable(v.InvAlerts, func(i, j int) bool {
				return alertRank(v.InvAlerts[i].StatusKey) < alertRank(v.InvAlerts[j].StatusKey)
			})
			if len(v.Movements) > 8 {
				v.InvMovements = v.Movements[:8]
			} else {
				v.InvMovements = v.Movements
			}
		}
	case "ingredient-form":
		v.InvCategoryEntities, _ = a.store.Entities(ctx, "ingredient_category")
		v.Kind = "ingredient"
		v.EntityID = strings.TrimSpace(r.URL.Query().Get("id"))
		v.Action = "create"
		v.Fields = map[string]string{}
		if v.EntityID != "" {
			if e, eErr := a.store.Entity(ctx, "ingredient", v.EntityID); eErr == nil {
				v.Action = "update"
				var values map[string]json.RawMessage
				_ = json.Unmarshal(e.Payload, &values)
				for key, raw := range values {
					var value string
					if json.Unmarshal(raw, &value) != nil {
						value = string(raw)
					}
					v.Fields[key] = value
				}
			}
		}
	case "orders":
		// فهرست سفارش‌ها هم‌تراز با orders-list وب: فیلتر وضعیت و جستجو از
		// URL، شمارنده‌ها از کل سفارش‌ها (overview وب)، صفحه‌بندی ۲۰تایی.
		v.Products, _ = a.store.Entities(ctx, "product")
		v.Tables, _ = a.store.Entities(ctx, "table")
		allOrders, oErr := a.store.Entities(ctx, "order")
		if oErr != nil {
			err = oErr
			break
		}
		v.Entities = allOrders
		sort.SliceStable(allOrders, func(i, j int) bool {
			ci, cj := allOrders[i].Field("created_at"), allOrders[j].Field("created_at")
			if ci != cj {
				return ci > cj
			}
			return allOrders[i].Field("order_number") > allOrders[j].Field("order_number")
		})
		tableByID := map[string]store.Entity{}
		for _, t := range v.Tables {
			tableByID[t.ID] = t
		}
		digitsOf := func(v string) string {
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
			return b.String()
		}
		sess, hasSess := a.sessionOf(r)
		canDelete := a.authDisabled || (hasSess && sess.Role == "manager")
		query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
		v.Query = strings.TrimSpace(r.URL.Query().Get("q"))
		statusFilter := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status")))
		switch statusFilter {
		case "PENDING", "ACCEPTED", "PREPARING", "READY", "COMPLETED", "CANCELLED":
		default:
			statusFilter = "ALL"
		}
		v.OrderStatusFilter = statusFilter
		v.OrderScopeAll = strings.TrimSpace(r.URL.Query().Get("scope")) == "all"
		revenueIRR := big.NewInt(0)
		var filtered []OrderRow
		for _, o := range allOrders {
			key := orderStatusKey(o.Field("status"))
			switch key {
			case "PENDING":
				v.OrdCntPending++
			case "ACCEPTED":
				v.OrdCntAccepted++
			case "PREPARING":
				v.OrdCntPreparing++
			case "READY":
				v.OrdCntReady++
			case "COMPLETED":
				v.OrdCntCompleted++
				revenueIRR.Add(revenueIRR, parseIRRBig(o.PriceIRR()))
			case "CANCELLED":
				v.OrdCntCancelled++
			}
			if key != "COMPLETED" && key != "CANCELLED" && key != "REFUNDED" {
				v.OrdersOpenCount++
			}
			// شماره میز: اول از برچسب ذخیره‌شده، بعد از خود میز.
			tableNum := digitsOf(o.Field("table_label"))
			if tableNum == "" {
				if t, ok := tableByID[o.Field("table_id")]; ok {
					tableNum = digitsOf(t.Field("number"))
					if tableNum == "" {
						tableNum = digitsOf(t.Label())
					}
				}
			}
			// خلاصه آیتم‌ها مانند summarizeItems وب: حداکثر دو قلم + «+N مورد».
			items := o.OrderItems()
			parts := make([]string, 0, len(items))
			for _, it := range items {
				parts = append(parts, it.ProductName+" ×"+store.FormatPersianNumber(strconv.Itoa(it.Quantity)))
			}
			previewParts := parts
			hidden := 0
			if len(parts) > 2 {
				previewParts = parts[:2]
				hidden = len(parts) - 2
			}
			number := displayOrderNumber(o.Field("order_number"))
			shortID := ""
			if len(o.ID) >= 8 {
				shortID = "#" + o.ID[:8]
			}
			displayNumber := ""
			if number != "" && number != "0" {
				displayNumber = store.FormatPersianNumber(number)
			} else {
				displayNumber = shortID
				shortID = ""
			}
			type itemJSON struct {
				PID   string `json:"pid"`
				Title string `json:"title"`
				Qty   int    `json:"qty"`
				Unit  string `json:"unit"`
				Note  string `json:"note"`
			}
			ij := make([]itemJSON, 0, len(items))
			for _, it := range items {
				ij = append(ij, itemJSON{PID: it.ProductID, Title: it.ProductName, Qty: it.Quantity, Unit: it.UnitPrice, Note: it.Note})
			}
			itemsJSONBytes, _ := json.Marshal(ij)
			originKind, originLabel := "pos", "صندوق"
			switch strings.ToLower(strings.TrimSpace(o.Field("fulfillment_type"))) {
			case "outside", "delivery", "takeaway":
				originKind, originLabel = "takeaway", "بیرون‌بر"
			}
			nextStatus, nextLabel := orderPrimaryAction(strings.ToLower(key), a.posEnabledStages(ctx))
			row := OrderRow{
				Entity:        o,
				Number:        displayNumber,
				ShortID:       shortID,
				ItemsPreview:  strings.Join(previewParts, "، "),
				ItemsHidden:   hidden,
				ItemsFull:     strings.Join(parts, "، "),
				TotalFa:       store.FormatPersianNumber(new(big.Int).Div(parseIRRBig(o.PriceIRR()), big.NewInt(10)).String()),
				TimeLabel:     orderRelTimeFa(o.Field("created_at")),
				StatusKey:     key,
				StatusLabel:   o.StatusLabel(),
				CustomerName:  strings.TrimSpace(o.Field("customer_name")),
				CashierName:   strings.TrimSpace(o.Field("cashier_name")),
				OriginKind:    originKind,
				OriginLabel:   originLabel,
				NextStatus:    nextStatus,
				NextLabel:     nextLabel,
				CanDelete:     canDelete,
				NumberRaw:     number,
				CustomerPhone: strings.TrimSpace(o.Field("customer_phone")),
				ItemsJSON:     string(itemsJSONBytes),
			}
			if tableNum != "" {
				row.TableNumber = store.FormatPersianNumber(tableNum)
			}
			switch key {
			case "PENDING", "ACCEPTED", "PREPARING", "READY":
				row.Cancellable = true
				row.Editable = true
			}
			// دامنه روزانه: پیش‌فرض فقط سفارش‌های امروز (تهران) دیده می‌شوند
			// تا هر روز فهرست تازه باشد؛ با scope=all همه روزها می‌آیند.
			if !v.OrderScopeAll && !isTehranToday(o.Field("created_at")) {
				continue
			}
			if statusFilter != "ALL" && key != statusFilter {
				continue
			}
			if query != "" {
				haystack := strings.ToLower(number + " " + row.CustomerName + " " + row.ItemsFull + " " + tableNum + " " + row.TableNumber)
				if !strings.Contains(haystack, query) {
					continue
				}
			}
			filtered = append(filtered, row)
		}
		v.OrdCntAll = len(allOrders)
		v.OrdersCompletedRevenueFa = store.FormatPersianNumber(new(big.Int).Div(revenueIRR, big.NewInt(10)).String())
		// سفارش‌های نهایی (تکمیل‌شده، بعد لغوشده) همیشه پایین فهرست می‌روند تا
		// سفارش‌های فعال جلوی چشم صندوقدار بمانند؛ ترتیب زمانی داخل هر گروه
		// دست‌نخورده می‌ماند (مرتب‌سازی پایدار).
		ordRank := func(row OrderRow) int {
			switch row.StatusKey {
			case "COMPLETED":
				return 1
			case "CANCELLED":
				return 2
			}
			return 0
		}
		sort.SliceStable(filtered, func(i, j int) bool { return ordRank(filtered[i]) < ordRank(filtered[j]) })
		// صفحه‌بندی سمت سرور، ۲۰ ردیف در صفحه مانند DEFAULT_PAGE_SIZE وب.
		const orderPageSize = 20
		v.OrderFilteredTotal = len(filtered)
		v.OrderTotalPages = (len(filtered) + orderPageSize - 1) / orderPageSize
		if v.OrderTotalPages < 1 {
			v.OrderTotalPages = 1
		}
		pageNum := 1
		if raw := strings.TrimSpace(r.URL.Query().Get("p")); raw != "" {
			if n, cErr := strconv.Atoi(raw); cErr == nil && n > 0 {
				pageNum = n
			}
		}
		if pageNum > v.OrderTotalPages {
			pageNum = v.OrderTotalPages
		}
		v.OrderPageNum = pageNum
		v.OrderPrevPage = pageNum - 1
		v.OrderNextPage = pageNum + 1
		start := (pageNum - 1) * orderPageSize
		end := start + orderPageSize
		if start > len(filtered) {
			start = len(filtered)
		}
		if end > len(filtered) {
			end = len(filtered)
		}
		v.OrderRows = filtered[start:end]
		if len(filtered) == 0 {
			v.OrderFrom, v.OrderTo = 0, 0
		} else {
			v.OrderFrom, v.OrderTo = start+1, end
		}
	case "receipts":
		v.Entities, _ = a.store.Entities(ctx, "order")
		v.PrintJobs, _ = a.store.PrintJobs(ctx, "")
		// «کل سفارشات»: جستجو در همه روزها (شماره، مشتری، موبایل، میز، اقلام)
		// تا فاکتور روزهای قبل هم سریع پیدا شود.
		if q := strings.ToLower(strings.TrimSpace(v.Query)); q != "" {
			var filtered []store.Entity
			for _, o := range v.Entities {
				var itemNames []string
				for _, it := range o.OrderItems() {
					itemNames = append(itemNames, it.ProductName)
				}
				hay := strings.ToLower(displayOrderNumber(o.Field("order_number")) + " " + o.Field("customer_name") + " " + o.Field("customer_phone") + " " + o.Field("table_label") + " " + strings.Join(itemNames, " "))
				if strings.Contains(hay, q) {
					filtered = append(filtered, o)
				}
			}
			v.Entities = filtered
		}
	case "pricing":
		v.Products, _ = a.store.Entities(ctx, "product")
		v.Ingredients, _ = a.store.Entities(ctx, "ingredient")
		v.SmartQuotes, _ = a.store.Entities(ctx, "smart_quote")
		for _, prod := range v.Products {
			row := PricingRow{Product: prod, MarginIRR: "0"}
			if costIRR, hasRecipe, cErr := a.store.ProductRecipeCostIRR(ctx, prod.ID); cErr == nil && hasRecipe {
				row.RecipeCostIRR = costIRR
				row.HasRecipe = true
				price := parseIRRBig(prod.PriceIRR())
				cost := parseIRRBig(costIRR)
				row.MarginIRR = price.Sub(price, cost).String()
			}
			v.Pricing = append(v.Pricing, row)
		}
	case "analytics":
		v.Entities, _ = a.store.Entities(ctx, "order")
		v.Expenses, _ = a.store.Entities(ctx, "expense")
		agg := map[string]*TopProduct{}
		for _, o := range v.Entities {
			if o.Field("status") != "completed" {
				continue
			}
			for _, line := range o.OrderItems() {
				tp, ok := agg[line.ProductID]
				if !ok {
					tp = &TopProduct{Name: line.ProductName, RevenueIRR: "0"}
					agg[line.ProductID] = tp
				}
				tp.Quantity += line.Quantity
				rev := parseIRRBig(tp.RevenueIRR)
				rev.Add(rev, parseIRRBig(line.LineTotal))
				tp.RevenueIRR = rev.String()
			}
		}
		for _, tp := range agg {
			v.TopProducts = append(v.TopProducts, *tp)
		}
		sort.Slice(v.TopProducts, func(i, j int) bool { return v.TopProducts[i].Quantity > v.TopProducts[j].Quantity })
		if len(v.TopProducts) > 10 {
			v.TopProducts = v.TopProducts[:10]
		}
	case "waste":
		v.Entities, err = a.store.Entities(ctx, "waste_event")
		v.Ingredients, _ = a.store.Entities(ctx, "ingredient")
		v.Movements, _ = a.store.Entities(ctx, "inventory_movement")
		{
			// تجمیع‌های صفحه ضایعات وب: هزینه بر اساس دلیل و بیشترین اقلام ضایع‌شده.
			reasonLabels := map[string]string{
				"expired": "منقضی شده", "spoilage": "فاسد شده", "preparation": "آماده‌سازی",
				"overproduction": "تولید اضافه", "quality_reject": "رد کیفیت",
				"breakage": "شکستگی", "staff_error": "خطای پرسنل", "other": "سایر",
			}
			avgCost := map[string]float64{}
			for _, ing := range v.Ingredients {
				f, _ := strconv.ParseFloat(strings.TrimSpace(ing.Field("average_cost")), 64)
				avgCost[ing.ID] = f
			}
			costFa := func(costIRR float64) string {
				if costIRR <= 0 {
					return "—"
				}
				return store.FormatPersianNumber(strconv.FormatInt(int64(costIRR/10), 10)) + " تومان"
			}
			reasonAgg := map[string]*WasteAgg{}
			reasonCost := map[string]float64{}
			itemAgg := map[string]*WasteAgg{}
			itemCost := map[string]float64{}
			itemQty := map[string]float64{}
			var totalCost float64
			for _, e := range v.Entities {
				qty, _ := strconv.ParseFloat(strings.TrimSpace(e.Field("quantity")), 64)
				cost := qty * avgCost[e.Field("ingredient_id")]
				lbl := reasonLabels[strings.TrimSpace(e.Field("reason"))]
				if lbl == "" {
					lbl = strings.TrimSpace(e.Field("reason"))
				}
				if lbl == "" {
					lbl = "سایر"
				}
				voided := e.Field("status") == "voided"
				v.WasteRows = append(v.WasteRows, WasteRow{Entity: e, ReasonLabel: lbl, CostFa: costFa(cost), Voided: voided})
				if voided {
					continue
				}
				totalCost += cost
				ra := reasonAgg[lbl]
				if ra == nil {
					ra = &WasteAgg{Label: lbl}
					reasonAgg[lbl] = ra
				}
				ra.Events++
				reasonCost[lbl] += cost
				name := e.Field("ingredient_name")
				ia := itemAgg[name]
				if ia == nil {
					ia = &WasteAgg{Label: name}
					itemAgg[name] = ia
				}
				ia.Events++
				itemCost[name] += cost
				itemQty[name] += qty
			}
			for lbl, ra := range reasonAgg {
				ra.CostFa = costFa(reasonCost[lbl])
				v.WasteByReason = append(v.WasteByReason, *ra)
			}
			itemUnit := map[string]string{}
			for _, e := range v.Entities {
				if e.Field("status") == "voided" {
					continue
				}
				if itemUnit[e.Field("ingredient_name")] == "" {
					itemUnit[e.Field("ingredient_name")] = e.Field("unit")
				}
			}
			for name, ia := range itemAgg {
				ia.CostFa = costFa(itemCost[name])
				ia.QtyFa = store.FormatPersianNumber(strconv.FormatFloat(itemQty[name], 'f', -1, 64))
				if u := strings.TrimSpace(itemUnit[name]); u != "" {
					ia.QtyFa += " " + u
				}
				v.WasteTopItems = append(v.WasteTopItems, *ia)
			}
			sort.SliceStable(v.WasteByReason, func(i, j int) bool { return v.WasteByReason[i].Events > v.WasteByReason[j].Events })
			sort.SliceStable(v.WasteTopItems, func(i, j int) bool { return v.WasteTopItems[i].Events > v.WasteTopItems[j].Events })
			if len(v.WasteTopItems) > 5 {
				v.WasteTopItems = v.WasteTopItems[:5]
			}
			v.WasteTotalCostFa = costFa(totalCost)
		}
	case "receipt":
		orderID := r.URL.Query().Get("id")
		if order, oErr := a.store.Entity(ctx, "order", orderID); oErr == nil {
			v.OrderDetail = &order
			v.PrintJobs, _ = a.store.PrintJobs(ctx, order.ID)
		} else {
			orders, _ := a.store.Entities(ctx, "order")
			if len(orders) > 0 {
				v.OrderDetail = &orders[0]
			}
		}
		if v.OrderDetail != nil {
			// داده‌های قالب فیش دقیقاً مانند FactorPrint وب محاسبه می‌شوند
			// تا پیش‌نمایش صفحه و خروجی ESC/POS چاپگر یکی باشند.
			od := *v.OrderDetail
			v.ReceiptCafeName = cafeNameFa(v.CafeProfile)
			v.ReceiptHeaderText, v.ReceiptFooterText = cafeReceiptTexts(v.CafeProfile)
			for _, it := range od.OrderItems() {
				v.ReceiptItemCount += it.Quantity
			}
			v.ReceiptTableNumber = tableNumberFa(od)
			v.ReceiptDateTime = jalaliDateTimeFa(od.Field("created_at"))
			if parts := strings.SplitN(v.ReceiptDateTime, "  ", 2); len(parts) == 2 {
				v.ReceiptDateOnly, v.ReceiptTimeOnly = parts[0], parts[1]
			} else {
				v.ReceiptDateOnly = v.ReceiptDateTime
			}
			v.ReceiptOrderType = orderTypeLabelFa(od)
			v.ReceiptPayment = paymentLabelFa(od)
		}
	case "recipes":
		v.Entities, err = a.store.Entities(ctx, "recipe")
		v.Products, _ = a.store.Entities(ctx, "product")
		v.Ingredients, _ = a.store.Entities(ctx, "ingredient")
	case "tables":
		v.Tables, err = a.store.Entities(ctx, "table")
		v.Reservations, _ = a.store.Entities(ctx, "reservation")
		v.Entities = v.Tables
	case "customers":
		v.Customers, err = a.store.Entities(ctx, "customer")
		v.Debtors, _ = a.store.Entities(ctx, "debtor")
		v.ClubGroupOpts, _ = a.store.Entities(ctx, "customer_group")
		v.ClubGroupsJSON = a.clubGroupsJSON(ctx)
		clubOrders, _ := a.store.Entities(ctx, "order")
		v.ClubTab = strings.TrimSpace(r.URL.Query().Get("tab"))
		if v.ClubTab != "membership" {
			v.ClubTab = "customers"
		}
		v.ClubSubTab = strings.TrimSpace(r.URL.Query().Get("sub"))
		if v.ClubSubTab != "payments" {
			v.ClubSubTab = "members"
		}
		v.ClubSearch = strings.TrimSpace(r.URL.Query().Get("cq"))
		allRows := clubBuildRows(v.Customers, clubOrders, v.Debtors, v.ClubGroupOpts)
		matches := func(s string) bool {
			return v.ClubSearch == "" || strings.Contains(strings.ToLower(s), strings.ToLower(v.ClubSearch))
		}
		for _, row := range allRows {
			if v.ClubTab == "customers" {
				if matches(row.Name + " " + row.Mobile + " " + row.MemberCode) {
					v.ClubRows = append(v.ClubRows, row)
				}
			} else {
				if row.IsMember {
					v.ClubStatMembers++
				}
				if matches(row.Name + " " + row.Mobile) {
					v.ClubRows = append(v.ClubRows, row)
				}
			}
		}
		payments, _ := a.store.Entities(ctx, "membership_payment")
		custByID := map[string]store.Entity{}
		for _, c := range v.Customers {
			custByID[c.ID] = c
		}
		for _, p := range payments {
			status := strings.ToLower(strings.TrimSpace(p.Field("status")))
			pending := status == "pending"
			if pending {
				v.ClubStatPending++
			} else if status == "paid" || status == "confirmed" || status == "completed" || status == "success" {
				v.ClubStatPaid++
			}
			pr := ClubPaymentRow{Entity: p, Pending: pending, StatusKey: status}
			pr.CustomerName = strings.TrimSpace(p.Field("consumer_name"))
			pr.CustomerPhone = strings.TrimSpace(p.Field("consumer_phone"))
			if cid := strings.TrimSpace(p.Field("customer_id")); cid != "" {
				if c, ok := custByID[cid]; ok {
					if pr.CustomerName == "" {
						pr.CustomerName = c.Label()
					}
					if pr.CustomerPhone == "" {
						pr.CustomerPhone = strings.TrimSpace(c.Field("phone"))
					}
				}
			}
			if pr.CustomerName == "" {
				pr.CustomerName = "مشتری"
			}
			pr.PlanTitle = strings.TrimSpace(p.Field("plan_title"))
			if pr.PlanTitle == "" {
				pr.PlanTitle = "—"
			}
			amt, _ := strconv.ParseInt(strings.TrimSpace(p.Field("amount_irr")), 10, 64)
			pr.AmountFa = store.FormatPersianNumber(strconv.FormatInt(amt/10, 10)) + " تومان"
			switch strings.ToLower(strings.TrimSpace(p.Field("payment_method"))) {
			case "online":
				pr.MethodLabel = "آنلاین"
			case "cash":
				pr.MethodLabel = "نقدی"
			case "card":
				pr.MethodLabel = "کارت"
			case "transfer":
				pr.MethodLabel = "کارت به کارت"
			default:
				pr.MethodLabel = "—"
			}
			switch {
			case pending:
				pr.StatusLabel = "در انتظار تایید"
			case status == "paid" || status == "confirmed" || status == "completed" || status == "success":
				pr.StatusLabel = "تایید شده"
			case status == "failed":
				pr.StatusLabel = "ناموفق"
			case status == "cancelled" || status == "canceled":
				pr.StatusLabel = "لغو شده"
			default:
				pr.StatusLabel = "نامشخص"
			}
			pr.Reference = strings.TrimSpace(p.Field("reference_number"))
			when := strings.TrimSpace(p.Field("paid_at"))
			if when == "" {
				when = strings.TrimSpace(p.Field("created_at"))
			}
			pr.DateFa = FormatJalaliDate(when)
			if v.ClubSearch == "" || strings.Contains(strings.ToLower(pr.CustomerName+" "+pr.CustomerPhone+" "+pr.PlanTitle+" "+pr.Reference), strings.ToLower(v.ClubSearch)) {
				v.ClubPayments = append(v.ClubPayments, pr)
			}
		}
		v.Entities = v.Customers
	case "club-groups":
		groups, _ := a.store.Entities(ctx, "customer_group")
		customers, _ := a.store.Entities(ctx, "customer")
		memberCount := map[string]int{}
		for _, c := range customers {
			if gid := strings.TrimSpace(c.Field("group_id")); gid != "" {
				memberCount[gid]++
			}
		}
		v.ClubOnlyValid = r.URL.Query().Get("valid") == "1"
		v.ClubSearch = strings.TrimSpace(r.URL.Query().Get("cq"))
		v.ClubGroupsTotal = len(groups)
		for _, g := range groups {
			row := clubGroupRowOf(g, memberCount[g.ID], time.Now())
			if row.StatusKey == "valid" {
				v.ClubGroupsValid++
			}
			v.ClubGroupsMembers += memberCount[g.ID]
			if v.ClubOnlyValid && row.StatusKey != "valid" {
				continue
			}
			if v.ClubSearch != "" && !strings.Contains(strings.ToLower(g.Label()+" "+g.Field("contact_name")+" "+g.Field("contact_phone")), strings.ToLower(v.ClubSearch)) {
				continue
			}
			v.ClubGroups = append(v.ClubGroups, row)
		}
		v.Entities = groups
		v.ClubGroupOpts = groups
		v.ClubGroupsJSON = a.clubGroupsJSON(ctx)
	case "club-group":
		v.ClubGroupsJSON = a.clubGroupsJSON(ctx)
		gid := strings.TrimSpace(r.URL.Query().Get("id"))
		if g, gErr := a.store.Entity(ctx, "customer_group", gid); gErr == nil {
			customers, _ := a.store.Entities(ctx, "customer")
			orders, _ := a.store.Entities(ctx, "order")
			debtors, _ := a.store.Entities(ctx, "debtor")
			allGroups, _ := a.store.Entities(ctx, "customer_group")
			allRows := clubBuildRows(customers, orders, debtors, allGroups)
			det := &ClubGroupDetail{Group: g, Row: clubGroupRowOf(g, 0, time.Now())}
			memberIDs := map[string]bool{}
			var totalSpent, creditSpent int64
			for _, row := range allRows {
				if row.GroupID == gid {
					det.Members = append(det.Members, row)
					memberIDs[row.Entity.ID] = true
				}
			}
			det.Row.MembersCount = len(det.Members)
			memberPhones := map[string]bool{}
			for _, m := range det.Members {
				if m.Mobile != "" {
					memberPhones[m.Mobile] = true
				}
			}
			var memberOrders []store.Entity
			for _, o := range orders {
				if strings.EqualFold(o.Field("status"), "cancelled") || strings.EqualFold(o.Field("status"), "canceled") {
					continue
				}
				if memberIDs[strings.TrimSpace(o.Field("customer_id"))] || memberPhones[strings.TrimSpace(o.Field("customer_phone"))] {
					memberOrders = append(memberOrders, o)
					amt, _ := strconv.ParseInt(strings.TrimSpace(o.PriceIRR()), 10, 64)
					totalSpent += amt
					if strings.EqualFold(o.Field("payment_method"), "credit_debtor") {
						creditSpent += amt
					}
				}
			}
			sort.SliceStable(memberOrders, func(i, j int) bool { return memberOrders[i].Field("created_at") > memberOrders[j].Field("created_at") })
			det.TotalSpentFa = store.FormatPersianNumber(strconv.FormatInt(totalSpent/10, 10)) + " تومان"
			det.CreditSpentFa = store.FormatPersianNumber(strconv.FormatInt(creditSpent/10, 10)) + " تومان"
			for i, o := range memberOrders {
				if i >= 10 {
					break
				}
				det.RecentOrders = append(det.RecentOrders, ClubGroupOrder{
					Number:       displayOrderNumber(o.Field("order_number")),
					CustomerName: strings.TrimSpace(o.Field("customer_name")),
					TotalFa:      store.FormatPersianNumber(new(big.Int).Div(parseIRRBig(o.PriceIRR()), big.NewInt(10)).String()) + " تومان",
					StatusLabel:  o.StatusLabel(),
					DateFa:       FormatJalaliDate(o.Field("created_at")),
				})
			}
			v.ClubGroupDetail = det
		}
	case "debtors":
		v.Debtors, err = a.store.Entities(ctx, "debtor")
		v.Customers, _ = a.store.Entities(ctx, "customer")
		v.DebtorGroups, _ = a.store.Entities(ctx, "customer_group")
		v.Entities = v.Debtors
		{
			// صفحه «لیست بدهکاران، مستندات سفارش‌ها و خرید اعتباری» عین وب:
			// کارت‌های خلاصه، فیلترهای همان جدول، ستون «مستندات سفارش
			// (سفارش چی بود؟)»، تسویه و ثبت بدهی دستی با آیتم‌های محصول و
			// تاریخ شمسی. همه ارقام از همین داده محلی محاسبه می‌شوند.
			now := time.Now()
			loc := now.In(tehranZone())
			v.DebtorTab = strings.TrimSpace(r.URL.Query().Get("tab"))
			if v.DebtorTab != "groups" {
				v.DebtorTab = "list"
			}
			tjy, tjm, tjd := GregorianToJalali(loc.Year(), int(loc.Month()), loc.Day())
			v.DebtorTodayShamsi = store.ToPersianDigits(fmt.Sprintf("%04d/%02d/%02d", tjy, tjm, tjd))

			groupLabels := map[string]string{}
			for _, g := range v.DebtorGroups {
				groupLabels[g.ID] = g.Label()
			}
			memberCounts := map[string]int{}
			for _, c := range v.Customers {
				if gid := strings.TrimSpace(c.Field("group_id")); gid != "" {
					memberCounts[gid]++
				}
			}

			// جزئیات هر بدهی از سفارش پیوندخورده (یا آیتم‌های ثبت دستی) خوانده
			// می‌شود تا معلوم باشد مشتری چه سفارش داده، چقدر بدهکار است، چه
			// توضیحی ثبت شده و سفارش دقیقاً چه تاریخ و ساعتی بوده است.
			ordersByID := map[string]store.Entity{}
			if allOrders, oErr := a.store.Entities(ctx, "order"); oErr == nil {
				for _, o := range allOrders {
					ordersByID[o.ID] = o
				}
			}
			paymentsByDebtor := map[string][]store.Entity{}
			allPayments, _ := a.store.Entities(ctx, "debtor_payment")
			for _, p := range allPayments {
				key := strings.TrimSpace(p.Field("debtor_id"))
				if key == "" {
					continue
				}
				paymentsByDebtor[key] = append(paymentsByDebtor[key], p)
			}
			byPerson := map[string][]store.Entity{}
			for _, d := range v.Debtors {
				key := strings.TrimSpace(d.Field("phone"))
				if key == "" {
					key = strings.TrimSpace(d.Label())
				}
				byPerson[key] = append(byPerson[key], d)
			}

			rows := make([]DebtorRow, 0, len(v.Debtors))
			for _, d := range v.Debtors {
				row := DebtorRow{Entity: d, NoteText: strings.TrimSpace(d.Field("note"))}
				if row.NoteText == "" {
					row.NoteText = strings.TrimSpace(d.Field("description"))
				}
				row.Phone = store.ToPersianDigits(strings.TrimSpace(d.Field("phone")))
				row.AccountKey = "personal"
				row.AccountLabel = "مشتری شخصی"
				if strings.EqualFold(strings.TrimSpace(d.Field("account_type")), "company") {
					row.AccountKey = "company"
					row.AccountLabel = "حساب شرکتی / گروه"
				}
				if gid := strings.TrimSpace(d.Field("group_id")); gid != "" {
					row.GroupLabel = groupLabels[gid]
				}
				row.SourceKey = strings.TrimSpace(d.Field("source"))
				if row.SourceKey == "" {
					if strings.TrimSpace(d.Field("order_id")) != "" {
						row.SourceKey = "pos"
					} else {
						row.SourceKey = "manual"
					}
				}
				if row.SourceKey == "pos" {
					row.SourceLabel = "سفارش صندوق"
				} else {
					row.SourceLabel = "ثبت دستی"
				}

				totalIRR := parseIRRBig(d.Field("total"))
				paidIRR := parseIRRBig(d.Field("paid"))
				remainingIRR := parseIRRBig(d.Field("remaining"))
				row.RemainingIRR = remainingIRR.String()
				row.TotalFa = tomanFaWebInt(totalIRR)
				row.PaidFa = tomanFaWebInt(paidIRR)
				row.RemainingFa = tomanFaWebInt(remainingIRR)

				dueISO, dueLabel, overdue := debtorDueInfo(d.Field("due_at"), remainingIRR, now)
				row.DueISO = dueISO
				row.DueLabel = dueLabel
				row.Overdue = overdue
				row.StatusKey, row.StatusLabel, row.StatusClass = debtorStatusOf(remainingIRR, d.Field("status"), overdue)

				row.CreatedISO = strings.TrimSpace(d.Field("created_at"))
				if ct, ok := JalaliTimeOf(row.CreatedISO); ok {
					row.DateFa = ShamsiFromISO(row.CreatedISO)
					row.TimeFa = store.ToPersianDigits(ct.Format("15:04"))
					row.IsNew = now.Sub(ct) < 72*time.Hour
				}

				if oid := strings.TrimSpace(d.Field("order_id")); oid != "" {
					if o, ok := ordersByID[oid]; ok {
						row.OrderNumber = displayOrderNumber(o.Field("order_number"))
						var parts []string
						for _, it := range o.OrderItems() {
							parts = append(parts, it.ProductName+" ×"+store.FormatPersianNumber(strconv.Itoa(it.Quantity)))
						}
						row.ItemsText = strings.Join(parts, "، ")
						row.ItemCount = len(o.OrderItems())
						if ct, pErr := time.Parse(time.RFC3339, o.Field("created_at")); pErr == nil {
							row.OrderWhen = FormatJalaliFull(ct)
						}
					}
				}
				if row.ItemsText == "" {
					if items := debtorItemsFromPayload(d); len(items) > 0 {
						var parts []string
						for _, it := range items {
							qty := store.FormatPersianNumber(it[1])
							if strings.TrimSpace(it[1]) == "" {
								qty = store.FormatPersianNumber("1")
							}
							parts = append(parts, it[0]+" ×"+qty)
						}
						row.ItemsText = strings.Join(parts, "، ")
						row.ItemCount = len(items)
					}
				}
				row.HasDocument = row.ItemsText != "" || row.OrderNumber != ""
				if row.OrderWhen == "" {
					if ct, pErr := time.Parse(time.RFC3339, d.Field("created_at")); pErr == nil {
						row.OrderWhen = FormatJalaliFull(ct)
					}
				}

				// پرداخت‌های ثبت‌شده این حساب (دفتر وصولی) برای دیالوگ جزئیات.
				paySum := big.NewInt(0)
				pays := paymentsByDebtor[d.ID]
				sort.SliceStable(pays, func(i, j int) bool { return pays[i].Field("created_at") > pays[j].Field("created_at") })
				for _, p := range pays {
					amt := parseIRRBig(p.Field("amount_irr"))
					paySum.Add(paySum, amt)
					row.Payments = append(row.Payments, DebtorPaymentRow{
						AmountFa: tomanFaWebInt(amt),
						DateFa:   ShamsiFromISO(p.Field("created_at")),
						Note:     strings.TrimSpace(p.Field("note")),
					})
				}
				if len(row.Payments) > 0 {
					row.PaymentFa = tomanFaWebInt(paySum)
					row.PaymentCount = len(row.Payments)
				}

				key := strings.TrimSpace(d.Field("phone"))
				if key == "" {
					key = strings.TrimSpace(d.Label())
				}
				others := 0
				otherRemain := big.NewInt(0)
				for _, od := range byPerson[key] {
					if od.ID == d.ID {
						continue
					}
					others++
					otherRemain.Add(otherRemain, parseIRRBig(od.Field("remaining")))
				}
				row.OtherCount = others
				if others > 0 {
					row.OtherDebts = store.FormatPersianNumber(strconv.Itoa(others)) + " بدهی دیگر به مبلغ " + store.FormatIRRToToman(otherRemain.String())
				}
				rows = append(rows, row)
			}

			// کارت‌های خلاصه بالای صفحه: همه بدهی‌ها، حساب‌های باز، سررسید
			// گذشته و وصول همین ماه شمسی (از دفتر پرداخت‌های ثبت‌شده).
			remainingSum := big.NewInt(0)
			overdueSum := big.NewInt(0)
			openCount, overdueCount := 0, 0
			for _, row := range rows {
				rem := parseIRRBig(row.Entity.Field("remaining"))
				remainingSum.Add(remainingSum, rem)
				if row.StatusKey == "paid" {
					continue
				}
				openCount++
				if row.Overdue {
					overdueCount++
					overdueSum.Add(overdueSum, rem)
				}
			}
			collected := big.NewInt(0)
			for _, p := range allPayments {
				pt, ok := JalaliTimeOf(p.Field("created_at"))
				if !ok {
					continue
				}
				py, pm, _ := GregorianToJalali(pt.Year(), int(pt.Month()), pt.Day())
				if py == tjy && pm == tjm {
					collected.Add(collected, parseIRRBig(p.Field("amount_irr")))
				}
			}
			v.DebtorCount = len(rows)
			v.DebtorKPIRemainingFa = tomanFaWebInt(remainingSum)
			v.DebtorKPIOpenFa = store.FormatPersianNumber(strconv.Itoa(openCount))
			v.DebtorKPIOverdueFa = store.FormatPersianNumber(strconv.Itoa(overdueCount))
			v.DebtorKPIOverdueSumFa = tomanFaWebInt(overdueSum)
			v.DebtorKPICollectedFa = tomanFaWebInt(collected)

			// گزارش بدهی گروه‌ها (سازمانی) — تب دوم.
			for _, g := range v.DebtorGroups {
				grow := DebtorGroupRow{Entity: g, Name: g.Label(), Members: memberCounts[g.ID]}
				total := big.NewInt(0)
				remain := big.NewInt(0)
				for _, row := range rows {
					if strings.TrimSpace(row.Entity.Field("group_id")) != g.ID {
						continue
					}
					grow.Accounts++
					total.Add(total, parseIRRBig(row.Entity.Field("total")))
					remain.Add(remain, parseIRRBig(row.Entity.Field("remaining")))
					if row.Overdue {
						grow.OverdueCount++
					}
				}
				grow.TotalFa = tomanFaWebInt(total)
				grow.RemainingFa = tomanFaWebInt(remain)
				switch {
				case grow.OverdueCount > 0:
					grow.StatusKey, grow.StatusLabel = "overdue", "سررسید گذشته"
				case remain.Sign() > 0:
					grow.StatusKey, grow.StatusLabel = "open", "بدهی باز"
				default:
					grow.StatusKey, grow.StatusLabel = "paid", "تسویه شده"
				}
				v.DebtorGroupRows = append(v.DebtorGroupRows, grow)
			}
			sort.SliceStable(v.DebtorGroupRows, func(i, j int) bool {
				return v.DebtorGroupRows[i].Name < v.DebtorGroupRows[j].Name
			})

			// فیلترهای همان نوار وب (وضعیت، منبع ثبت، حساب، گروه، بازه تاریخ
			// شمسی و مرتب‌سازی).
			q := strings.TrimSpace(r.URL.Query().Get("q"))
			v.DebtorQuery = q
			v.DebtorStatusFilter = strings.TrimSpace(r.URL.Query().Get("status"))
			switch v.DebtorStatusFilter {
			case "open", "partial", "paid", "overdue":
			default:
				v.DebtorStatusFilter = "all"
			}
			v.DebtorSourceFilter = strings.TrimSpace(r.URL.Query().Get("source"))
			if v.DebtorSourceFilter != "pos" && v.DebtorSourceFilter != "manual" {
				v.DebtorSourceFilter = "all"
			}
			v.DebtorAccountFilter = strings.TrimSpace(r.URL.Query().Get("account"))
			if v.DebtorAccountFilter != "personal" && v.DebtorAccountFilter != "company" {
				v.DebtorAccountFilter = "all"
			}
			v.DebtorGroupFilter = strings.TrimSpace(r.URL.Query().Get("group"))
			if v.DebtorGroupFilter == "all" {
				v.DebtorGroupFilter = ""
			}
			v.DebtorSortFilter = strings.TrimSpace(r.URL.Query().Get("sort"))
			switch v.DebtorSortFilter {
			case "created", "remaining", "due", "name":
			default:
				v.DebtorSortFilter = "updated"
			}
			v.DebtorFromISO = isoDateOrEmpty(r.URL.Query().Get("from"))
			v.DebtorToISO = isoDateOrEmpty(r.URL.Query().Get("to"))
			v.DebtorFromShamsi = ShamsiFromISO(v.DebtorFromISO)
			v.DebtorToShamsi = ShamsiFromISO(v.DebtorToISO)

			needle := strings.ToLower(normalizeDigits(q))
			for _, row := range rows {
				if needle != "" {
					haystack := strings.ToLower(normalizeDigits(row.Entity.Label() + " " + row.Entity.Field("phone") + " " +
						row.GroupLabel + " " + row.ItemsText + " " + row.Entity.Field("description") + " " + row.OrderNumber))
					if !strings.Contains(haystack, needle) {
						continue
					}
				}
				if v.DebtorStatusFilter != "all" && row.StatusKey != v.DebtorStatusFilter {
					continue
				}
				if v.DebtorSourceFilter != "all" && row.SourceKey != v.DebtorSourceFilter {
					continue
				}
				if v.DebtorAccountFilter != "all" && row.AccountKey != v.DebtorAccountFilter {
					continue
				}
				if v.DebtorGroupFilter != "" && strings.TrimSpace(row.Entity.Field("group_id")) != v.DebtorGroupFilter {
					continue
				}
				createdDay := ""
				if ct, ok := JalaliTimeOf(row.CreatedISO); ok {
					createdDay = ct.Format("2006-01-02")
				}
				if v.DebtorFromISO != "" && (createdDay == "" || createdDay < v.DebtorFromISO) {
					continue
				}
				if v.DebtorToISO != "" && (createdDay == "" || createdDay > v.DebtorToISO) {
					continue
				}
				v.DebtorRows = append(v.DebtorRows, row)
			}
			switch v.DebtorSortFilter {
			case "remaining":
				sort.SliceStable(v.DebtorRows, func(i, j int) bool {
					return parseIRRBig(v.DebtorRows[i].Entity.Field("remaining")).Cmp(parseIRRBig(v.DebtorRows[j].Entity.Field("remaining"))) > 0
				})
			case "due":
				sort.SliceStable(v.DebtorRows, func(i, j int) bool {
					di, dj := v.DebtorRows[i].DueISO, v.DebtorRows[j].DueISO
					if di == "" || dj == "" {
						return dj != "" && di == ""
					}
					return di < dj
				})
			case "name":
				sort.SliceStable(v.DebtorRows, func(i, j int) bool {
					return v.DebtorRows[i].Entity.Label() < v.DebtorRows[j].Entity.Label()
				})
			case "created":
				sort.SliceStable(v.DebtorRows, func(i, j int) bool {
					return v.DebtorRows[i].CreatedISO > v.DebtorRows[j].CreatedISO
				})
			default:
				sort.SliceStable(v.DebtorRows, func(i, j int) bool {
					ui := v.DebtorRows[i].Entity.Field("updated_at")
					if ui == "" {
						ui = v.DebtorRows[i].CreatedISO
					}
					uj := v.DebtorRows[j].Entity.Field("updated_at")
					if uj == "" {
						uj = v.DebtorRows[j].CreatedISO
					}
					if ui == uj {
						return v.DebtorRows[i].CreatedISO > v.DebtorRows[j].CreatedISO
					}
					return ui > uj
				})
			}
			v.DebtorFiltered = len(v.DebtorRows)

			// فیلترهای فعال به‌صورت query نگه داشته می‌شوند تا بعد از ثبت/تسویه
			// کاربر دوباره روی همان نمای فیلترشده برگردد (رفتار وب).
			filterQ := url.Values{}
			if v.DebtorTab != "list" {
				filterQ.Set("tab", v.DebtorTab)
			}
			if q != "" {
				filterQ.Set("q", q)
			}
			if v.DebtorStatusFilter != "all" {
				filterQ.Set("status", v.DebtorStatusFilter)
			}
			if v.DebtorSourceFilter != "all" {
				filterQ.Set("source", v.DebtorSourceFilter)
			}
			if v.DebtorAccountFilter != "all" {
				filterQ.Set("account", v.DebtorAccountFilter)
			}
			if v.DebtorGroupFilter != "" {
				filterQ.Set("group", v.DebtorGroupFilter)
			}
			if v.DebtorSortFilter != "updated" {
				filterQ.Set("sort", v.DebtorSortFilter)
			}
			if v.DebtorFromISO != "" {
				filterQ.Set("from", v.DebtorFromISO)
			}
			if v.DebtorToISO != "" {
				filterQ.Set("to", v.DebtorToISO)
			}
			v.DebtorFilterQuery = filterQ.Encode()

			// کاتالوگ محصولات برای «ثبت با نام محصولات سفارش»: کاربر آیتم را از
			// فهرست واقعی محصولات جستجو و اضافه می‌کند، نه تایپ آزاد.
			type debtorProductJSON struct {
				ID       string `json:"id"`
				Title    string `json:"title"`
				Price    int64  `json:"price"`
				Category string `json:"category"`
			}
			catLabel := map[string]string{}
			if cats, cErr := a.store.Entities(ctx, "category"); cErr == nil {
				for _, c := range cats {
					catLabel[c.ID] = c.Label()
				}
			}
			options := []debtorProductJSON{}
			if products, pErr := a.store.Entities(ctx, "product"); pErr == nil {
				for _, p := range products {
					priceToman := parseIRRBig(p.PriceIRR())
					priceToman.Quo(priceToman, big.NewInt(10))
					price := priceToman.Int64()
					options = append(options, debtorProductJSON{
						ID:       p.ID,
						Title:    p.Label(),
						Price:    price,
						Category: catLabel[p.Field("category_id")],
					})
				}
			}
			sort.SliceStable(options, func(i, j int) bool { return options[i].Title < options[j].Title })
			if raw, mErr := json.Marshal(options); mErr == nil {
				v.DebtorProductsJSON = template.JS(raw)
			} else {
				v.DebtorProductsJSON = template.JS("[]")
			}

			// داده دیالوگ‌های «جزئیات حساب» و «ثبت پرداخت»: کل ردیف‌های فیلترشده
			// با همه پرداخت‌ها، تا صفحه بدون درخواست اضافه و بدون data-attribute
			// شکننده کار کند.
			type debtorPayJSON struct {
				Amount string `json:"amount"`
				Date   string `json:"date"`
				Note   string `json:"note"`
			}
			type debtorRowJSON struct {
				ID           string          `json:"id"`
				Name         string          `json:"name"`
				Phone        string          `json:"phone"`
				Account      string          `json:"account"`
				Group        string          `json:"group"`
				Items        string          `json:"items"`
				OrderNumber  string          `json:"order_number"`
				When         string          `json:"when"`
				Total        string          `json:"total"`
				Paid         string          `json:"paid"`
				Remaining    string          `json:"remaining"`
				RemainingIRR string          `json:"remaining_irr"`
				Due          string          `json:"due"`
				Note         string          `json:"note"`
				Status       string          `json:"status"`
				StatusKey    string          `json:"status_key"`
				Source       string          `json:"source"`
				Other        string          `json:"other"`
				Payments     []debtorPayJSON `json:"payments"`
			}
			rowJSON := make([]debtorRowJSON, 0, len(v.DebtorRows))
			for _, row := range v.DebtorRows {
				item := debtorRowJSON{
					ID:           row.Entity.ID,
					Name:         row.Entity.Label(),
					Phone:        row.Phone,
					Account:      row.AccountLabel,
					Group:        row.GroupLabel,
					Items:        row.ItemsText,
					OrderNumber:  row.OrderNumber,
					When:         row.OrderWhen,
					Total:        row.TotalFa,
					Paid:         row.PaidFa,
					Remaining:    row.RemainingFa,
					RemainingIRR: row.RemainingIRR,
					Due:          row.DueLabel,
					Note:         row.NoteText,
					Status:       row.StatusLabel,
					StatusKey:    row.StatusKey,
					Source:       row.SourceLabel,
					Other:        row.OtherDebts,
					Payments:     []debtorPayJSON{},
				}
				for _, p := range row.Payments {
					item.Payments = append(item.Payments, debtorPayJSON{Amount: p.AmountFa, Date: p.DateFa, Note: p.Note})
				}
				rowJSON = append(rowJSON, item)
			}
			if raw, mErr := json.Marshal(rowJSON); mErr == nil {
				v.DebtorRowsJSON = template.JS(raw)
			} else {
				v.DebtorRowsJSON = template.JS("[]")
			}
		}
	case "finance":
		v.Expenses, err = a.store.Entities(ctx, "expense")
		v.AccountingEntries, _ = a.store.Entities(ctx, "accounting_entry")
		v.ShiftCloses, _ = a.store.Entities(ctx, "shift_close")
		v.Debtors, _ = a.store.Entities(ctx, "debtor")
		v.Entities = v.Expenses
		{
			// «حساب و گزارش» عین accounting وب: تب‌ها از ?tab= خوانده می‌شوند و
			// ارقام نمای کلی از داده واقعی محلی (فروش نقد/کارت، ارزش انبار با
			// بهای میانگین، مطالبات) و عملکرد ماه جاری با COGS رسپی ساخته می‌شود.
			v.FinTab = strings.TrimSpace(r.URL.Query().Get("tab"))
			switch v.FinTab {
			case "expenses", "journal", "accounts", "pnl":
			default:
				v.FinTab = "overview"
			}
			now := time.Now()
			monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
			month := a.store.AnalyticsSince(ctx, monthStart)
			toman := func(irr string) string { return store.FormatIRRToToman(irr) }
			// موجودی صندوق = فروش نقدی کل؛ موجودی بانک = فروش کارتخوان کل.
			v.FinCashFa = toman(v.Analytics.CashSalesIRR)
			v.FinBankFa = toman(v.Analytics.CardSalesIRR)
			v.FinReceivablesFa = toman(v.Analytics.TotalReceivablesIRR)
			invValue := big.NewInt(0)
			if ings, iErr := a.store.Entities(ctx, "ingredient"); iErr == nil {
				for _, ing := range ings {
					stock, _ := strconv.ParseFloat(strings.TrimSpace(ing.Field("current_stock")), 64)
					avg := parseIRRBig(ing.Field("average_cost"))
					val := new(big.Float).Mul(big.NewFloat(stock), new(big.Float).SetInt(avg))
					vi, _ := val.Int(nil)
					invValue.Add(invValue, vi)
				}
			}
			v.FinInvValueFa = toman(invValue.String())
			// COGS ماه: مجموع بهای رسپی اقلام سفارش‌های تکمیل‌شده ماه جاری.
			cogs := big.NewInt(0)
			costCache := map[string]*big.Int{}
			if orders, oErr := a.store.Entities(ctx, "order"); oErr == nil {
				for _, o := range orders {
					if strings.ToLower(strings.TrimSpace(o.Field("status"))) != "completed" {
						continue
					}
					if ct, pErr := time.Parse(time.RFC3339, o.Field("created_at")); pErr != nil || ct.Before(monthStart) {
						continue
					}
					for _, it := range o.OrderItems() {
						unit, ok := costCache[it.ProductID]
						if !ok {
							unit = big.NewInt(0)
							if c, has, cErr := a.store.ProductRecipeCostIRR(ctx, it.ProductID); cErr == nil && has {
								unit = parseIRRBig(c)
							}
							costCache[it.ProductID] = unit
						}
						cogs.Add(cogs, new(big.Int).Mul(unit, big.NewInt(int64(it.Quantity))))
					}
				}
			}
			rev := parseIRRBig(month.TotalSalesIRR)
			exp := parseIRRBig(month.TotalExpensesIRR)
			profit := new(big.Int).Sub(new(big.Int).Sub(rev, cogs), exp)
			v.FinMonthRevFa = toman(rev.String())
			v.FinMonthCOGSFa = toman(cogs.String())
			v.FinMonthExpFa = toman(exp.String())
			v.FinMonthProfitFa = toman(profit.String())
		}
	case "reports":
		v.Entities, _ = a.store.Entities(ctx, "order")
		v.Expenses, _ = a.store.Entities(ctx, "expense")
		v.Ingredients, _ = a.store.Entities(ctx, "ingredient")
		v.Debtors, _ = a.store.Entities(ctx, "debtor")
		v.AccountingEntries, _ = a.store.Entities(ctx, "accounting_entry")
		{
			// فیلتر دوره گزارش (reports-filters وب): امروز/هفته/ماه/همه.
			// KPIها و پرفروش‌ترین‌ها بر اساس همین بازه محاسبه می‌شوند.
			period := strings.TrimSpace(r.URL.Query().Get("period"))
			now := time.Now()
			var from time.Time
			switch period {
			case "today":
				y, m, d := now.Date()
				from = time.Date(y, m, d, 0, 0, 0, 0, now.Location())
			case "week":
				from = now.AddDate(0, 0, -7)
			case "month":
				from = now.AddDate(0, 0, -30)
			default:
				period = "all"
			}
			v.ReportsPeriod = period
			allAnalytics := v.Analytics
			v.ReportAllIncomeIRR = allAnalytics.TotalSalesIRR
			v.ReportAllOrdersCount = allAnalytics.CompletedOrders + allAnalytics.OpenOrders + allAnalytics.CancelledOrders
			switch period {
			case "today":
				v.ReportPeriodLabel = "روزانه — امروز"
			case "week":
				v.ReportPeriodLabel = "هفتگی — ۷ روز اخیر"
			case "month":
				v.ReportPeriodLabel = "ماهانه — ۳۰ روز اخیر"
			default:
				v.ReportPeriodLabel = "همه دوره‌ها"
			}
			var prevFrom, prevTo time.Time
			hasPrev := false
			switch period {
			case "today":
				prevFrom, prevTo, hasPrev = from.AddDate(0, 0, -1), from, true
			case "week":
				prevFrom, prevTo, hasPrev = from.AddDate(0, 0, -7), from, true
			case "month":
				prevFrom, prevTo, hasPrev = from.AddDate(0, 0, -30), from, true
			}
			if period != "all" {
				v.Analytics = a.store.AnalyticsSince(ctx, from)
			}
			a.fillDashboard(&v, r)
			v.ReportIncomeChart = buildReportIncomeChart(period, v.Entities, now)

			normalizeStatus := func(raw string) string {
				switch strings.ToLower(strings.TrimSpace(raw)) {
				case "canceled":
					return "cancelled"
				case "", "open":
					return "pending"
				default:
					return strings.ToLower(strings.TrimSpace(raw))
				}
			}
			type timedOrder struct {
				e  store.Entity
				t  time.Time
				ok bool
			}
			filtered := make([]timedOrder, 0, len(v.Entities))
			for _, o := range v.Entities {
				t, ok := entityTime(o)
				if period != "all" && (!ok || t.Before(from)) {
					continue
				}
				filtered = append(filtered, timedOrder{e: o, t: t, ok: ok})
			}
			sort.SliceStable(filtered, func(i, j int) bool {
				if filtered[i].ok && filtered[j].ok {
					return filtered[i].t.After(filtered[j].t)
				}
				return filtered[i].ok && !filtered[j].ok
			})
			v.ReportStatusTotal = len(filtered)
			statusCounts := map[string]int{}
			for _, item := range filtered {
				key := normalizeStatus(item.e.Field("status"))
				statusCounts[key]++
				if key != "cancelled" {
					v.ReportOrdersCount++
				}
				if key == "completed" {
					v.ReportCompletedCount++
				}
			}
			// ردیف‌های فشرده ستون کناری: تراکنش‌های اخیر و زیر آن سفارش‌های اخیر.
			for _, item := range filtered {
				if len(v.ReportOrders) >= 6 {
					break
				}
				key := normalizeStatus(item.e.Field("status"))
				itemsCount := 0
				for _, line := range item.e.OrderItems() {
					itemsCount += line.Quantity
				}
				label := item.e.StatusLabel()
				switch key {
				case "pending":
					label = "در انتظار تأیید"
				case "accepted":
					label = "تأیید صندوقدار"
				case "preparing":
					label = "در حال آماده‌سازی"
				case "ready":
					label = "آماده تحویل"
				case "completed":
					label = "تکمیل‌شده"
				case "cancelled":
					label = "لغوشده"
				case "refunded":
					label = "بازگشت وجه"
				}
				v.ReportOrders = append(v.ReportOrders, ReportOrderRow{
					Entity:      item.e,
					Number:      displayOrderNumber(item.e.Field("order_number")),
					ItemsCount:  itemsCount,
					ItemsFa:     store.FormatPersianNumber(strconv.Itoa(itemsCount)) + " قلم",
					TotalFa:     item.e.PriceToman(),
					StatusKey:   key,
					StatusLabel: label,
					StatusClass: item.e.StatusClass(),
					DateFa:      FormatJalaliShort(item.e.Field("created_at")),
				})
			}
			// دونات وضعیت سفارش‌ها با ترتیب و رنگ‌های وب.
			statusDefs := []struct{ key, label, color string }{
				{"pending", "در انتظار", "#F59E0B"},
				{"accepted", "تأیید صندوقدار", "#0D9488"},
				{"preparing", "در حال آماده‌سازی", "#3B82F6"},
				{"ready", "آماده تحویل", "#8B5CF6"},
				{"completed", "تکمیل‌شده", "#10B981"},
				{"cancelled", "لغوشده", "#F43F5E"},
				{"refunded", "بازگشت وجه", "#64748B"},
			}
			activeSlices := 0
			for _, def := range statusDefs {
				if statusCounts[def.key] > 0 {
					activeSlices++
				}
			}
			const donutCircumference = 339.2920066
			donutStart := 0.0
			for _, def := range statusDefs {
				count := statusCounts[def.key]
				if count == 0 || v.ReportStatusTotal == 0 {
					continue
				}
				segment := float64(count) / float64(v.ReportStatusTotal) * donutCircumference
				visible := segment
				if activeSlices > 1 {
					gap := 3.0
					if segment*0.25 < gap {
						gap = segment * 0.25
					}
					visible -= gap
				}
				if visible < 0 {
					visible = 0
				}
				v.ReportStatusSlices = append(v.ReportStatusSlices, ReportStatusSlice{
					Key: def.key, Label: def.label, Color: def.color, Count: count,
					Percent: int(float64(count)/float64(v.ReportStatusTotal)*100 + 0.5),
					Dash:    visible, Offset: -donutStart,
				})
				donutStart += segment
			}

			type agg struct {
				qty int
				rev int64
			}
			byTitle := map[string]*agg{}
			for _, item := range filtered {
				if normalizeStatus(item.e.Field("status")) != "completed" {
					continue
				}
				lines, _, _, _ := store.SplitBillOf(item.e)
				for _, ln := range lines {
					a2 := byTitle[ln.Title]
					if a2 == nil {
						a2 = &agg{}
						byTitle[ln.Title] = a2
					}
					a2.qty += ln.Quantity
					a2.rev += ln.LinePayableIRR
				}
			}
			var totalProductRevenue int64
			maxProductRevenue := int64(0)
			for _, a2 := range byTitle {
				totalProductRevenue += a2.rev
				if a2.rev > maxProductRevenue {
					maxProductRevenue = a2.rev
				}
			}
			for title, a2 := range byTitle {
				share := 0.0
				if totalProductRevenue > 0 {
					share = float64(a2.rev) / float64(totalProductRevenue) * 100
				}
				barWidth := 0
				if maxProductRevenue > 0 && a2.rev > 0 {
					barWidth = int(float64(a2.rev) / float64(maxProductRevenue) * 100)
					if barWidth < 2 {
						barWidth = 2
					}
				}
				v.ReportProducts = append(v.ReportProducts, ReportProductRow{
					Title:      title,
					Quantity:   a2.qty,
					QuantityFa: store.FormatPersianNumber(strconv.Itoa(a2.qty)),
					RevenueIRR: a2.rev,
					RevenueFa:  store.FormatPersianNumber(strconv.FormatInt(a2.rev/10, 10)) + " تومان",
					Share:      share,
					ShareFa:    store.FormatPersianNumber(strconv.Itoa(int(share+0.5))) + "٪",
					BarWidth:   barWidth,
				})
			}
			sort.SliceStable(v.ReportProducts, func(i, j int) bool {
				if v.ReportProducts[i].RevenueIRR != v.ReportProducts[j].RevenueIRR {
					return v.ReportProducts[i].RevenueIRR > v.ReportProducts[j].RevenueIRR
				}
				return v.ReportProducts[i].Quantity > v.ReportProducts[j].Quantity
			})
			v.ReportSoldProductsCount = len(v.ReportProducts)
			bestByQty := append([]ReportProductRow(nil), v.ReportProducts...)
			sort.SliceStable(bestByQty, func(i, j int) bool {
				if bestByQty[i].Quantity != bestByQty[j].Quantity {
					return bestByQty[i].Quantity > bestByQty[j].Quantity
				}
				return bestByQty[i].RevenueIRR > bestByQty[j].RevenueIRR
			})
			for i, row := range bestByQty {
				if i >= 8 {
					break
				}
				v.BestSellers = append(v.BestSellers, BestSellerRow{Title: row.Title, QtyFa: row.QuantityFa, RevenueFa: row.RevenueFa})
			}

			aggregateRange := func(start, end time.Time) (*big.Int, int, int) {
				income := big.NewInt(0)
				ordersCount, completed := 0, 0
				for _, o := range v.Entities {
					t, ok := entityTime(o)
					if !ok || t.Before(start) || !t.Before(end) {
						continue
					}
					key := normalizeStatus(o.Field("status"))
					if key != "cancelled" {
						ordersCount++
					}
					if key == "completed" {
						completed++
						income.Add(income, parseIRRBig(o.PriceIRR()))
					}
				}
				return income, ordersCount, completed
			}
			currentIncome := parseIRRBig(v.Analytics.TotalSalesIRR)
			if v.ReportCompletedCount > 0 {
				avg := new(big.Int).Quo(currentIncome, big.NewInt(int64(v.ReportCompletedCount)))
				v.ReportAverageFa = store.FormatIRRToToman(avg.String())
			} else {
				v.ReportAverageFa = "۰ تومان"
			}
			yesterdayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -1)
			yesterdayEnd := yesterdayStart.AddDate(0, 0, 1)
			yesterdayIncome, _, _ := aggregateRange(yesterdayStart, yesterdayEnd)
			v.ReportYesterdayIncomeFa = store.FormatIRRToToman(yesterdayIncome.String())
			if hasPrev {
				prevIncome, prevOrders, prevCompleted := aggregateRange(prevFrom, prevTo)
				v.ReportPrevIncomeFa = store.FormatIRRToToman(prevIncome.String())
				curFloat, _ := new(big.Float).SetInt(currentIncome).Float64()
				prevFloat, _ := new(big.Float).SetInt(prevIncome).Float64()
				v.ReportIncomeGrowthLabel, v.ReportIncomeGrowthClass = growthBadge(curFloat, prevFloat)
				v.ReportOrdersGrowthLabel, v.ReportOrdersGrowthClass = growthBadge(float64(v.ReportOrdersCount), float64(prevOrders))
				curAvg, prevAvg := 0.0, 0.0
				if v.ReportCompletedCount > 0 {
					curAvg = curFloat / float64(v.ReportCompletedCount)
				}
				if prevCompleted > 0 {
					prevAvg = prevFloat / float64(prevCompleted)
				}
				v.ReportAverageGrowthLabel, v.ReportAverageGrowthClass = growthBadge(curAvg, prevAvg)
			}
		}
	case "staff":
		v.JobPostings, _ = a.store.Entities(ctx, "job_posting")
		staff, sErr := a.store.Entities(ctx, "staff")
		if sErr == nil {
			v.StaffList = staff
			q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
			statusF := strings.TrimSpace(r.URL.Query().Get("sstatus"))
			roleF := strings.TrimSpace(r.URL.Query().Get("srole"))
			for _, e := range staff {
				row := staffRowOf(e)
				v.StaffTotal++
				if row.Active {
					v.StaffActive++
				} else {
					v.StaffInactive++
				}
				if strings.TrimSpace(e.Field("shift")) != "" || strings.TrimSpace(e.Field("shifts")) != "" {
					v.StaffShifts++
				}
				if q != "" && !strings.Contains(strings.ToLower(e.Label()), q) && !strings.Contains(e.Field("phone"), q) && !strings.Contains(strings.ToLower(row.RoleLabel), q) {
					continue
				}
				if statusF == "active" && !row.Active {
					continue
				}
				if statusF == "inactive" && row.Active {
					continue
				}
				if roleF != "" && e.Field("role_key") != roleF {
					continue
				}
				v.StaffRows = append(v.StaffRows, row)
			}
		}
	case "staff-checklist":
		v.Tasks, _ = a.store.Entities(ctx, "task")
		doneTasks := 0
		for _, task := range v.Tasks {
			if task.Field("is_done") == "true" || task.Field("status") == "done" {
				doneTasks++
			}
		}
		if len(v.Tasks) > 0 {
			v.StaffChecklistPercent = doneTasks * 100 / len(v.Tasks)
		}
		staff, sErr := a.store.Entities(ctx, "staff")
		if sErr == nil {
			v.StaffList = staff
			for _, e := range staff {
				row := staffRowOf(e)
				v.StaffTotal++
				if row.Active {
					v.StaffActive++
				} else {
					v.StaffInactive++
				}
				if strings.TrimSpace(e.Field("shift")) != "" || strings.TrimSpace(e.Field("shifts")) != "" {
					v.StaffShifts++
					v.StaffShiftRows = append(v.StaffShiftRows, row)
				}
			}
		}
	case "staff-form":
		v.Kind = "staff"
		v.EntityID = strings.TrimSpace(r.URL.Query().Get("id"))
		v.Action = "create"
		v.Fields = map[string]string{}
		v.StaffPerms = map[string]bool{}
		if v.EntityID != "" {
			if e, eErr := a.store.Entity(ctx, "staff", v.EntityID); eErr == nil {
				v.Action = "update"
				var values map[string]json.RawMessage
				_ = json.Unmarshal(e.Payload, &values)
				for key, raw := range values {
					var value string
					if json.Unmarshal(raw, &value) != nil {
						value = string(raw)
					}
					v.Fields[key] = value
				}
				for _, key := range strings.Split(v.Fields["permissions"], ",") {
					if key = strings.TrimSpace(key); key != "" {
						v.StaffPerms[key] = true
					}
				}
				// کلیدهای قدیمی آفلاین به کلیدهای واقعی وب نگاشت می‌شوند تا داده قبلی گم نشود.
				for legacy, current := range map[string]string{
					"recipes.view": "inventory.recipes.view",
					"waste.view":   "inventory.waste.view",
					"staff.manage": "staff.view",
				} {
					if v.StaffPerms[legacy] {
						v.StaffPerms[current] = true
					}
				}
			}
		}
		// فقط وقتی کلید permissions اصلاً ذخیره نشده، پیش‌فرض نقش وب اعمال می‌شود
		// (DEFAULT_CAFE_ROLE_PERMISSIONS)؛ فهرست خالیِ ذخیره‌شده عین وب محترم است.
		if _, hasStoredPerms := v.Fields["permissions"]; !hasStoredPerms {
			role := v.Fields["role_key"]
			if role == "" {
				role = "staff"
			}
			for _, key := range defaultCafePermissionsForRole(role) {
				v.StaffPerms[key] = true
			}
		}
	case "inv-categories":
		cats, cErr := a.store.Entities(ctx, "ingredient_category")
		if cErr == nil {
			v.InvCategoryEntities = cats
		}
		v.Ingredients, _ = a.store.Entities(ctx, "ingredient")
		{
			// ردیف‌های جدول دسته‌بندی مواد اولیهٔ وب: اقلام منتسب بر اساس
			// فیلد category قلم‌ها شمرده می‌شود و مرتب‌سازی با ترتیب نمایش است.
			counts := map[string]int{}
			for _, ing := range v.Ingredients {
				if c := strings.TrimSpace(ing.Field("category")); c != "" {
					counts[c]++
				}
			}
			for _, c := range v.InvCategoryEntities {
				sortOrder, _ := strconv.Atoi(strings.TrimSpace(c.Field("sort_order")))
				v.InvCategoryRows = append(v.InvCategoryRows, InvCategoryRow{
					Entity:    c,
					Items:     counts[c.Label()],
					ItemsFa:   store.FormatPersianNumber(strconv.Itoa(counts[c.Label()])),
					SortOrder: sortOrder,
					Active:    c.Field("is_active") != "false",
				})
			}
			sort.SliceStable(v.InvCategoryRows, func(i, j int) bool {
				if v.InvCategoryRows[i].SortOrder != v.InvCategoryRows[j].SortOrder {
					return v.InvCategoryRows[i].SortOrder < v.InvCategoryRows[j].SortOrder
				}
				return v.InvCategoryRows[i].Entity.Label() < v.InvCategoryRows[j].Entity.Label()
			})
		}
		v.EntityID = strings.TrimSpace(r.URL.Query().Get("edit"))
		v.Fields = map[string]string{}
		if v.EntityID != "" {
			if e, eErr := a.store.Entity(ctx, "ingredient_category", v.EntityID); eErr == nil {
				var values map[string]json.RawMessage
				_ = json.Unmarshal(e.Payload, &values)
				for key, raw := range values {
					var value string
					if json.Unmarshal(raw, &value) != nil {
						value = string(raw)
					}
					v.Fields[key] = value
				}
			}
		}
	case "inv-purchases":
		v.Purchases, _ = a.store.Entities(ctx, "purchase")
		v.Suppliers, _ = a.store.Entities(ctx, "supplier")
		v.Ingredients, _ = a.store.Entities(ctx, "ingredient")
	case "suppliers":
		v.Suppliers, _ = a.store.Entities(ctx, "supplier")
		v.Purchases, _ = a.store.Entities(ctx, "purchase")
	case "settings", "print-policy", "print-routes":
		v.Printers, _ = a.store.Entities(ctx, "printer")
		v.PrinterRoutes, _ = a.store.Entities(ctx, "printer_route")
		v.PrintJobs, _ = a.store.PrintJobs(ctx, "")
		v.PrintPolicy = a.printPolicy(ctx)
		{
			cats, _ := a.store.Entities(ctx, "category")
			prods, _ := a.store.Entities(ctx, "product")
			counts := map[string]int{}
			for _, p := range prods {
				counts[p.Field("category_id")]++
			}
			routeByCat := map[string]store.Entity{}
			for _, rt := range v.PrinterRoutes {
				if cid := strings.TrimSpace(rt.Field("category_id")); cid != "" {
					routeByCat[cid] = rt
				}
			}
			printerNameByID := map[string]string{}
			for _, p := range v.Printers {
				printerNameByID[p.ID] = p.Label()
			}
			for _, c := range cats {
				row := CategoryRouteRow{Category: c, Products: counts[c.ID], Active: true, Copies: 1}
				if rt, ok := routeByCat[c.ID]; ok {
					row.HasRoute = true
					row.PrinterID = strings.TrimSpace(rt.Field("printer_id"))
					row.PrinterName = printerNameByID[row.PrinterID]
					if row.PrinterName == "" {
						row.PrinterName = strings.TrimSpace(rt.Field("printer_name"))
					}
					row.Active = rt.Field("is_active") != "false" && rt.Field("is_active") != "0"
					if n, aErr := strconv.Atoi(strings.TrimSpace(rt.Field("copies"))); aErr == nil && n >= 1 && n <= 5 {
						row.Copies = n
					}
				}
				v.CategoryRoutes = append(v.CategoryRoutes, row)
			}
		}
	case "printers":
		v.PrinterProfiles, _ = a.store.Printers(ctx)
		v.PrinterStats = a.store.PrinterStats(ctx)
		v.PrintJobCounts = a.store.PrintJobCounts(ctx)
		v.PrinterRecentJobs, _ = a.store.PrintJobsFilter(ctx, "", "", 8)
		v.Printers, _ = a.store.Entities(ctx, "printer")
		switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("discovery"))) {
		case "queues":
			v.PrinterDiscoveryMode = "queues"
			queues, queueErr := a.listQueues(ctx)
			v.PrinterQueues = queues
			if queueErr != nil {
				v.PrinterQueueError = queueErr.Error()
			}
		case "usb":
			v.PrinterDiscoveryMode = "usb"
			a.usbCacheMu.Lock()
			if time.Since(a.usbCacheAt) < 2*time.Minute {
				v.PrinterUSBDevices = append([]printer.USBDevice(nil), a.usbCache...)
				v.PrinterUSBError = a.usbCacheErr
			}
			a.usbCacheMu.Unlock()
		}
	case "printer":
		// صفحهٔ جزئیات چاپگر حذف شده است؛ لینک‌های قدیمی به فهرست چاپگرها می‌رسند
		// و تنظیمات از دیالوگ کارت چاپگر انجام می‌شود.
		v.Page = "printers"
		v.PrinterProfiles, _ = a.store.Printers(ctx)
		v.PrinterStats = a.store.PrinterStats(ctx)
		v.PrintJobCounts = a.store.PrintJobCounts(ctx)
		v.PrinterRecentJobs, _ = a.store.PrintJobsFilter(ctx, "", "", 8)
		v.Printers, _ = a.store.Entities(ctx, "printer")
	case "printer-new":
		draft := printer.Profile{PaperMM: printer.Paper80, WidthDots: 576, Copies: 1, Cut: true, Active: true}
		q := r.URL.Query()
		if kind := strings.ToLower(strings.TrimSpace(q.Get("kind"))); kind == printer.KindNetwork || kind == printer.KindSpooler || kind == printer.KindUSB {
			draft.Kind = kind
		}
		draft.Name = strings.TrimSpace(q.Get("name"))
		draft.Model = strings.TrimSpace(q.Get("model"))
		draft.Host = strings.TrimSpace(q.Get("host"))
		draft.Port, _ = strconv.Atoi(strings.TrimSpace(q.Get("port")))
		draft.QueueName = strings.TrimSpace(q.Get("queue_name"))
		draft.USB = printer.USBDevice{
			DevicePath:   strings.TrimSpace(q.Get("usb_path")),
			VendorID:     strings.TrimSpace(q.Get("usb_vid")),
			ProductID:    strings.TrimSpace(q.Get("usb_pid")),
			Serial:       strings.TrimSpace(q.Get("usb_serial")),
			Product:      strings.TrimSpace(q.Get("usb_product")),
			Manufacturer: strings.TrimSpace(q.Get("usb_manufacturer")),
			Bus:          strings.TrimSpace(q.Get("usb_bus")),
			PortPath:     strings.TrimSpace(q.Get("usb_port")),
		}
		v.PrinterDraft = draft
		queues, qErr := a.listQueues(ctx)
		v.PrinterQueues = queues
		if qErr != nil {
			v.PrinterQueueError = qErr.Error()
		}
		a.usbCacheMu.Lock()
		if time.Since(a.usbCacheAt) < 2*time.Minute {
			v.PrinterUSBDevices = append([]printer.USBDevice(nil), a.usbCache...)
			v.PrinterUSBError = a.usbCacheErr
		}
		a.usbCacheMu.Unlock()
		v.PrinterProfiles, _ = a.store.Printers(ctx)
	case "printer-scan":
		networks, nErr := a.listNetworks()
		v.PrinterNetworks = networks
		if nErr != nil {
			v.PrinterNetworkError = nErr.Error()
		}
		if id := strings.TrimSpace(r.URL.Query().Get("scan")); id != "" {
			if snap, ok := a.scanner().Status(id); ok {
				v.PrinterScan = &snap
			}
		}
		v.PrinterProfiles, _ = a.store.Printers(ctx)
	case "print-queue":
		v.PrintJobFilter = strings.TrimSpace(r.URL.Query().Get("status"))
		switch v.PrintJobFilter {
		case store.PrintStatusPending, store.PrintStatusPrinting, store.PrintStatusPrinted,
			store.PrintStatusSent, store.PrintStatusFailed, store.PrintStatusCancelled:
		default:
			v.PrintJobFilter = ""
		}
		v.PrintQueuePrinter = strings.TrimSpace(r.URL.Query().Get("printer"))
		v.PrintJobCounts = a.store.PrintJobCounts(ctx)
		v.PrintJobs, _ = a.store.PrintJobsFilter(ctx, v.PrintJobFilter, v.PrintQueuePrinter, 100)
		v.PrinterProfiles, _ = a.store.Printers(ctx)
	case "editor":
		v.Kind = r.URL.Query().Get("kind")
		v.EntityID = r.URL.Query().Get("id")
		v.Action = "create"
		defaults := map[string]string{
			"category":   `{"title":""}`,
			"product":    `{"title":"","category_id":"","price":"0"}`,
			"ingredient": `{"name":"","base_unit":"g"}`,
			"banner":     `{"title":""}`,
		}
		var ok bool
		v.EditPayload, ok = defaults[v.Kind]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if v.EntityID != "" {
			var e store.Entity
			e, err = a.store.Entity(ctx, v.Kind, v.EntityID)
			if err == nil {
				v.Version = e.Version
				v.Action = "update"
				v.EditPayload = string(store.EditablePayload(v.Kind, e.Payload))
			}
		}
		if r.Method == "POST" {
			v.EditPayload = r.Form.Get("payload")
			v.Action = r.Form.Get("action")
			v.Version, _ = strconv.ParseInt(r.Form.Get("version"), 10, 64)
		}
		var values map[string]json.RawMessage
		_ = json.Unmarshal([]byte(v.EditPayload), &values)
		v.Fields = map[string]string{}
		for key, raw := range values {
			var value string
			if json.Unmarshal(raw, &value) != nil {
				value = string(raw)
			}
			v.Fields[key] = value
		}
		if v.Kind == "product" {
			v.Categories, err = a.store.Entities(ctx, "category")
		}
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "cannot read local data", 500)
		return
	}
	name := "layout"
	if r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-History-Restore-Request") != "true" {
		name = "content"
	}
	var buf bytes.Buffer
	if err = a.templates.ExecuteTemplate(&buf, name, v); err != nil {
		fmt.Fprintln(os.Stderr, "template render failed:", err)
		http.Error(w, "template render failed: "+err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
}

func (a *Application) refresh(w http.ResponseWriter, r *http.Request) {
	if a.client == nil {
		a.render(w, r, "این برنامه در حالت دسکتاپ مستقل (Standalone Offline) فعال است و تمام داده‌ها روی دیسک محلی ذخیره می‌شوند.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	message := "Sync cycle finished. همگام‌سازی به پایان رسید."
	if err := a.client.Refresh(ctx, a.store); err != nil {
		message = "Sync did not finish: " + err.Error() + ". داده‌های محلی شما کاملاً امن و در دسترس هستند."
	}
	a.render(w, r, message)
}

func (a *Application) queueCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Form.Get("mode") == "form" {
		payload := map[string]any{}
		fields := map[string][]string{
			"category":   {"title", "slug", "description"},
			"product":    {"title", "slug", "description", "category_id", "price", "cost"},
			"ingredient": {"name", "sku", "barcode"},
			"banner":     {"title", "image", "link"},
		}
		for _, key := range fields[r.Form.Get("kind")] {
			value := strings.TrimSpace(r.Form.Get(key))
			if value != "" || key == "description" {
				payload[key] = value
			}
		}
		if r.Form.Get("kind") == "ingredient" && r.Form.Get("action") == "create" {
			payload["base_unit"] = r.Form.Get("base_unit")
		}
		for _, key := range []string{"is_available", "is_active"} {
			if value := r.Form.Get(key); value != "" {
				if value != "true" && value != "false" {
					a.render(w, r, "Invalid boolean field.")
					return
				}
				payload[key] = value == "true"
			}
		}
		raw, _ := json.Marshal(payload)
		r.Form.Set("payload", string(raw))
	}

	// If running in pure standalone desktop mode (no sync client configured), save directly to local database!
	if a.client == nil {
		var payloadMap map[string]any
		if err := json.Unmarshal([]byte(r.Form.Get("payload")), &payloadMap); err != nil {
			a.render(w, r, "فرمت اطلاعات نامعتبر است.")
			return
		}
		kind := r.Form.Get("kind")
		id := r.Form.Get("id")
		action := r.Form.Get("action")
		if action == "delete" {
			if err := a.store.DeleteLocalRecord(r.Context(), kind, id); err != nil {
				a.render(w, r, err.Error())
				return
			}
			a.navigatePage(w, r, kindToPage(kind), "رکورد با موفقیت از پایگاه داده محلی حذف شد.")
			return
		}
		if _, err := a.store.SaveLocalRecord(r.Context(), kind, id, payloadMap); err != nil {
			a.render(w, r, err.Error())
			return
		}
		a.navigatePage(w, r, kindToPage(kind), "تغییرات با موفقیت در پایگاه داده محلی دسکتاپ ذخیره شد (queued / local saved).")
		return
	}

	version, err := strconv.ParseInt(r.Form.Get("version"), 10, 64)
	if err != nil {
		a.render(w, r, "Invalid record version.")
		return
	}
	_, err = a.store.QueueCatalog(r.Context(), r.Form.Get("kind"), r.Form.Get("id"), r.Form.Get("action"), json.RawMessage(r.Form.Get("payload")), version)
	if err != nil {
		a.render(w, r, "Edit not queued: "+err.Error())
		return
	}
	a.showOutbox(w, r, "Saved durably on this device. The edit is queued, not yet accepted by the server.")
}

func (a *Application) showOutbox(w http.ResponseWriter, r *http.Request, message string) {
	// صفحه «تاریخچه تراکنش‌ها» از محصول حذف شده است؛ پس از ثبت آفلاین،
	// کاربر در همان ویرایشگر می‌ماند و پیام صف را به‌صورت Toast می‌بیند.
	a.navigatePage(w, r, "editor", message)
}

func (a *Application) navigatePage(w http.ResponseWriter, r *http.Request, page, message string) {
	if r.Header.Get("HX-Request") != "true" {
		http.Redirect(w, r, "/?page="+page, http.StatusSeeOther)
		return
	}
	q := r.URL.Query()
	q.Set("page", page)
	r.URL.RawQuery = q.Encode()
	w.Header().Set("HX-Push-Url", "/?page="+page)
	a.render(w, r, message)
}

func kindToPage(kind string) string {
	switch kind {
	case "category":
		return "categories"
	case "product", "modifier_group":
		return "products"
	case "ingredient_category":
		return "inv-categories"
	case "ingredient":
		return "ingredients"
	case "supplier":
		return "suppliers"
	case "purchase":
		return "inv-purchases"
	case "table", "reservation":
		return "tables"
	case "customer", "retention_campaign", "cafe_plan":
		return "customers"
	case "debtor":
		return "debtors"
	case "expense", "accounting_entry", "shift_close":
		return "finance"
	case "staff", "job_posting":
		return "staff"
	case "task":
		return "staff-checklist"
	case "printer", "printer_route", "cafe_profile":
		return "settings"
	default:
		return "home"
	}
}

func (a *Application) handleSeedDemo(w http.ResponseWriter, r *http.Request) {
	if err := a.store.SeedIranianCafeData(r.Context()); err != nil {
		a.navigatePage(w, r, "home", "خطا در بارگذاری داده‌های نمونه: "+err.Error())
		return
	}
	a.navigatePage(w, r, "home", "داده‌های نمونه منوی کافه (منو، انبار، رسپی، میزها، مشتریان، پرسنل و سفارش‌ها) با موفقیت بارگذاری شد.")
}

// handleTablesBulk: ساخت گروهی میزها — همان ورودی‌های bulk-create-dialog وب
// (پیشوند، تعداد، شروع شماره، بخش و ظرفیت) ولی روی پایگاه محلی.
func (a *Application) handleTablesBulk(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.render(w, r, "فرم نامعتبر است.")
		return
	}
	count, _ := strconv.Atoi(strings.TrimSpace(r.Form.Get("count")))
	start, _ := strconv.Atoi(strings.TrimSpace(r.Form.Get("start")))
	if count < 1 || count > 100 {
		a.render(w, r, "تعداد میزها باید بین ۱ تا ۱۰۰ باشد.")
		return
	}
	if start < 1 {
		start = 1
	}
	prefix := strings.TrimSpace(r.Form.Get("prefix"))
	if prefix == "" {
		prefix = "میز"
	}
	ctx := r.Context()
	zone := strings.TrimSpace(r.Form.Get("zone"))
	capacity := strings.TrimSpace(r.Form.Get("capacity"))
	for i := 0; i < count; i++ {
		num := start + i
		payload := map[string]any{
			"title":    fmt.Sprintf("%s %d", prefix, num),
			"number":   strconv.Itoa(num),
			"zone":     zone,
			"capacity": capacity,
			"status":   "available",
		}
		if _, err := a.store.SaveLocalRecord(ctx, "table", "", payload); err != nil {
			a.render(w, r, "ساخت گروهی میزها ناموفق بود: "+err.Error())
			return
		}
	}
	a.navigatePage(w, r, "tables", "میزها با موفقیت به‌صورت گروهی ساخته شدند.")
}

func (a *Application) handleLocalSave(w http.ResponseWriter, r *http.Request) {
	kind := strings.TrimSpace(r.Form.Get("kind"))
	id := strings.TrimSpace(r.Form.Get("id"))
	targetPage := strings.TrimSpace(r.Form.Get("return_page"))
	if targetPage == "" {
		targetPage = kindToPage(kind)
	}
	fields := map[string]any{}
	for k, vals := range r.Form {
		if k == "csrf" || k == "kind" || k == "id" || k == "return_page" {
			continue
		}
		// ردیف‌های پویای تنوع/دستور تهیهٔ فرم محصول جداگانه پردازش می‌شوند.
		if k == "variant_title" || k == "variant_value" || k == "variant_active" || k == "recipe_ingredient_id" || k == "recipe_quantity" || k == "variants_present" || k == "recipe_present" {
			continue
		}
		if len(vals) > 0 {
			val := strings.TrimSpace(vals[0])
			if (k == "is_available" || k == "is_active") && (val == "true" || val == "false") {
				fields[k] = val == "true"
			} else if val != "" {
				fields[k] = val
			}
		}
	}
	// آپلود تصویر محلی روی سیستم کاربر (محصول/قلم انبار): فایل در پوشه داده برنامه ذخیره می‌شود.
	if (kind == "product" || kind == "ingredient") && r.MultipartForm != nil {
		if _, fh, fErr := r.FormFile("image_file"); fErr == nil && fh != nil && fh.Size > 0 {
			if imgPath, uErr := a.saveUploadedImage(fh); uErr == nil {
				fields["image"] = imgPath
			} else {
				a.navigatePage(w, r, targetPage, "خطا در ذخیره تصویر: "+uErr.Error())
				return
			}
		}
	}
	if kind == "staff" {
		if pw := strings.TrimSpace(r.Form.Get("password")); pw != "" {
			sum := sha256.Sum256([]byte(pw))
			fields["password_hash"] = hex.EncodeToString(sum[:])
		}
		delete(fields, "password")
		fields["permissions"] = strings.Join(r.Form["perm"], ",")
	}
	if kind == "debtor" && fields["remaining"] == nil && fields["total"] != nil {
		fields["remaining"] = fields["total"]
		fields["paid"] = "0"
		fields["status"] = "open"
		fields["currency"] = "IRR"
	}
	// تنوع‌ها و دستور تهیهٔ فرم ساخت محصول (create وب): تنوع‌ها روی خود محصول
	// ذخیره می‌شوند و دستور تهیه به رکورد رسپی همان محصول وصل می‌شود؛ محصولی
	// که دستور تهیه دارد موجودی مستقیم نمی‌گیرد.
	var productRecipeItems []map[string]any
	if kind == "product" && r.Form.Get("variants_present") == "1" {
		activeSet := map[string]bool{}
		for _, av := range r.Form["variant_active"] {
			activeSet[strings.TrimSpace(av)] = true
		}
		variants := []map[string]any{}
		titles := r.Form["variant_title"]
		values := r.Form["variant_value"]
		for i, rawTitle := range titles {
			title := strings.TrimSpace(rawTitle)
			valStr := ""
			if i < len(values) {
				valStr = strings.ReplaceAll(strings.ReplaceAll(normalizeNumInput(values[i]), ",", ""), "٬", "")
			}
			if title == "" && valStr == "" {
				continue
			}
			if title == "" || valStr == "" {
				a.navigatePage(w, r, targetPage, "برای هر تنوع، هم عنوان و هم مبلغ را وارد کنید.")
				return
			}
			amount, pErr := strconv.ParseFloat(valStr, 64)
			if pErr != nil || amount <= 0 {
				a.navigatePage(w, r, targetPage, "مبلغ هر تنوع باید یک عدد معتبر بزرگ‌تر از صفر باشد.")
				return
			}
			variants = append(variants, map[string]any{
				"title":     title,
				"value":     amount,
				"is_active": activeSet[strconv.Itoa(i)],
			})
		}
		fields["variants"] = variants
	}
	if kind == "product" && r.Form.Get("recipe_present") == "1" {
		ingIDs := r.Form["recipe_ingredient_id"]
		qtys := r.Form["recipe_quantity"]
		for i := range ingIDs {
			iid := strings.TrimSpace(ingIDs[i])
			if iid == "" || i >= len(qtys) {
				continue
			}
			qtyStr := strings.NewReplacer(",", "", "٬", "", " ", "").Replace(normalizeNumInput(qtys[i]))
			qty, qErr := strconv.ParseFloat(qtyStr, 64)
			if qErr != nil || qty <= 0 {
				continue
			}
			// مقدار مصرف همیشه به شکل عددی تمیز ذخیره می‌شود (نه متن ورودی
			// با جداکنندهٔ هزارگان/ارقام فارسی) تا کسر خودکار انبار درست کار کند.
			qtyStored := strconv.FormatFloat(qty, 'f', -1, 64)
			ingEnt, iErr := a.store.Entity(r.Context(), "ingredient", iid)
			if iErr != nil {
				continue
			}
			productRecipeItems = append(productRecipeItems, map[string]any{
				"ingredient_id":   iid,
				"ingredient_name": ingEnt.Label(),
				"quantity":        qtyStored,
				"unit":            ingEnt.Field("base_unit"),
			})
		}
		if len(productRecipeItems) > 0 {
			fields["stock"] = ""
		}
	}
	savedID, saveErr := a.store.SaveLocalRecord(r.Context(), kind, id, fields)
	if saveErr != nil {
		a.navigatePage(w, r, targetPage, "خطا در ذخیره‌سازی: "+saveErr.Error())
		return
	}
	if kind == "product" && len(productRecipeItems) > 0 {
		prodEnt, pErr := a.store.Entity(r.Context(), "product", savedID)
		if pErr == nil {
			recipeID := ""
			recipeNote := ""
			if recipes, rErr := a.store.Entities(r.Context(), "recipe"); rErr == nil {
				for _, rec := range recipes {
					if rec.Field("product_id") == savedID {
						recipeID = rec.ID
						recipeNote = rec.Field("note")
						break
					}
				}
			}
			_, _ = a.store.SaveLocalRecord(r.Context(), "recipe", recipeID, map[string]any{
				"title":        "رسپی " + prodEnt.Label(),
				"product_id":   savedID,
				"product_name": prodEnt.Label(),
				"yield_qty":    "1",
				"note":         recipeNote,
				"items":        productRecipeItems,
			})
		}
	}
	a.navigatePage(w, r, targetPage, "اطلاعات با موفقیت در پایگاه داده محلی دسکتاپ ثبت شد.")
}

func (a *Application) handleLocalDelete(w http.ResponseWriter, r *http.Request) {
	kind := strings.TrimSpace(r.Form.Get("kind"))
	id := strings.TrimSpace(r.Form.Get("id"))
	targetPage := strings.TrimSpace(r.Form.Get("return_page"))
	if targetPage == "" {
		targetPage = kindToPage(kind)
	}
	// حذف سفارش در وب نیازمند تایپ دقیق شماره سفارش و ثبت دلیل است؛
	// همان کنترل در سرور محلی هم اعمال می‌شود تا حذف اشتباهی رخ ندهد.
	if kind == "order" {
		ent, eErr := a.store.Entity(r.Context(), "order", id)
		if eErr != nil {
			a.navigatePage(w, r, targetPage, "سفارش مورد نظر یافت نشد.")
			return
		}
		expected := normalizeNumInput(ent.Field("order_number"))
		got := normalizeNumInput(r.Form.Get("confirm_number"))
		reason := strings.TrimSpace(r.Form.Get("reason"))
		if expected == "" || got != expected {
			a.navigatePage(w, r, targetPage, "برای حذف سفارش باید شماره سفارش را دقیق وارد کنید.")
			return
		}
		if n := len([]rune(reason)); n < 2 || n > 500 {
			a.navigatePage(w, r, targetPage, "برای حذف سفارش، ثبت دلیل (۲ تا ۵۰۰ نویسه) الزامی است.")
			return
		}
		if strings.EqualFold(ent.Field("status"), "completed") {
			a.navigatePage(w, r, targetPage, "سفارش تکمیل‌شده (تسویه‌شده) قابل حذف نیست تا مغایرت مالی ایجاد نشود.")
			return
		}
	}
	// دسته‌بندی انبارِ دارای قلم حذف نمی‌شود (categories-page وب): اول باید
	// اقلام آن دسته از فرم ویرایش قلم به دستهٔ دیگری منتقل شوند.
	if kind == "ingredient_category" {
		cat, cErr := a.store.Entity(r.Context(), "ingredient_category", id)
		if cErr != nil {
			a.navigatePage(w, r, targetPage, "دسته‌بندی مورد نظر یافت نشد.")
			return
		}
		if ings, iErr := a.store.Entities(r.Context(), "ingredient"); iErr == nil {
			used := 0
			for _, ing := range ings {
				if strings.TrimSpace(ing.Field("category")) == cat.Label() {
					used++
				}
			}
			if used > 0 {
				a.navigatePage(w, r, targetPage, "این دسته‌بندی هنوز قلم دارد؛ ابتدا اقلام آن را به دستهٔ دیگری منتقل کنید.")
				return
			}
		}
	}
	if err := a.store.DeleteLocalRecord(r.Context(), kind, id); err != nil {
		a.navigatePage(w, r, targetPage, err.Error())
		return
	}
	a.navigatePage(w, r, targetPage, "رکورد مورد نظر از پایگاه داده محلی حذف شد.")
}

func (a *Application) handleCreatePOSOrder(w http.ResponseWriter, r *http.Request) {
	var items []store.POSOrderLineInput
	prodIDs := r.Form["product_id"]
	qtys := r.Form["quantity"]
	notes := r.Form["line_note"]
	for i := range prodIDs {
		pid := strings.TrimSpace(prodIDs[i])
		if pid == "" {
			continue
		}
		q := 1
		if i < len(qtys) {
			if parsed, err := strconv.Atoi(strings.TrimSpace(qtys[i])); err == nil && parsed > 0 {
				q = parsed
			} else {
				continue
			}
		}
		ln := ""
		if i < len(notes) {
			ln = strings.TrimSpace(notes[i])
		}
		items = append(items, store.POSOrderLineInput{
			ProductID: pid,
			Quantity:  q,
			Note:      ln,
		})
	}
	cashierName := ""
	if sess, ok := a.sessionOf(r); ok {
		cashierName = sess.Name
	}
	// بیرون‌بر/پیک در وب بدون نام و موبایل معتبر مشتری ثبت نمی‌شود.
	if ft := strings.TrimSpace(r.Form.Get("fulfillment_type")); ft == "outside" || ft == "delivery" {
		name := strings.TrimSpace(r.Form.Get("customer_name"))
		phone := normalizeDigits(strings.TrimSpace(r.Form.Get("customer_phone")))
		if name == "" || phone == "" {
			a.navigatePage(w, r, "pos", "نام و شماره موبایل مشتری برای سفارش بیرون‌بر الزامی است.")
			return
		}
		if len(phone) != 11 || !strings.HasPrefix(phone, "09") {
			a.navigatePage(w, r, "pos", "برای سفارش بیرون‌بر، شماره موبایل باید ۱۱ رقم و با ۰۹ شروع شود.")
			return
		}
	}
	orderID, err := a.store.CreatePOSOrder(r.Context(), store.POSOrderInput{
		ClientToken:     r.Form.Get("client_token"),
		CashierName:     cashierName,
		FulfillmentType: r.Form.Get("fulfillment_type"),
		TableID:         r.Form.Get("table_id"),
		CustomerID:      r.Form.Get("customer_id"),
		CustomerName:    r.Form.Get("customer_name"),
		CustomerPhone:   r.Form.Get("customer_phone"),
		PaymentMethod:   r.Form.Get("payment_method"),
		DiscountIRR:     r.Form.Get("discount_irr"),
		ExtraIRR:        r.Form.Get("extra_irr"),
		ExtraNote:       r.Form.Get("extra_note"),
		DebtorDueAt:     deferDueAt(r),
		DebtorNote:      r.Form.Get("defer_note"),
		DebtorKind:      r.Form.Get("debtor_kind"),
		DebtorGroupID:   r.Form.Get("debtor_group_id"),
		Note:            r.Form.Get("note"),
		Items:           items,
	})
	if err != nil {
		a.navigatePage(w, r, "pos", "خطا در ثبت سفارش صندوق: "+err.Error())
		return
	}
	// چاپ خودکار طبق سیاست کاربر در پس‌زمینه انجام می‌شود: صندوقدار منتظر
	// چاپگر نمی‌ماند و اگر چاپگری نباشد یا چاپ خطا بدهد، هیچ پیام خطایی
	// روی صندوق نمی‌بیند؛ Jobها در صفحه فیش سفارش برای تلاش مجدد می‌مانند.
	if strings.TrimSpace(r.Form.Get("table_id")) != "" {
		a.autoPrintAsync(orderID, "create")
	} else {
		a.autoPrintAsync(orderID, "notable")
		a.autoPrintAsync(orderID, "checkout")
	}
	// صندوقدار در صندوق می‌ماند: سبد خالی می‌شود و فقط Toast موفقیت می‌بیند.
	msg := "سفارش با موفقیت ثبت شد."
	if strings.TrimSpace(r.Form.Get("payment_method")) == "credit_debtor" {
		msg = "سفارش ثبت و در لیست بدهکاران ثبت شد."
	}
	a.navigatePage(w, r, "pos", msg)
}

func (a *Application) handleOrderStatus(w http.ResponseWriter, r *http.Request) {
	orderID := strings.TrimSpace(r.Form.Get("order_id"))
	newStatus := strings.TrimSpace(r.Form.Get("status"))
	reason := strings.TrimSpace(r.Form.Get("reason"))
	// در وب لغو سفارش بدون دلیل ممکن نیست (۲ تا ۵۰۰ نویسه)؛ همان قاعده اینجا.
	if strings.EqualFold(newStatus, "cancelled") {
		if n := len([]rune(reason)); n < 2 || n > 500 {
			a.navigatePage(w, r, "orders", "برای لغو سفارش، ثبت دلیل (۲ تا ۵۰۰ نویسه) الزامی است.")
			return
		}
	}
	retPage := strings.TrimSpace(r.Form.Get("return_page"))
	if retPage == "" {
		retPage = "orders"
	}
	if err := a.store.UpdateLocalOrderStatusReason(r.Context(), orderID, newStatus, reason); err != nil {
		a.navigatePage(w, r, retPage, err.Error())
		return
	}
	msg := "وضعیت سفارش به‌روزرسانی شد."
	if newStatus == "completed" {
		msg = "سفارش تکمیل و تسویه شد."
		a.autoPrintAsync(orderID, "complete")
	} else if newStatus == "cancelled" {
		msg = "سفارش لغو شد و میز مربوطه آزاد گردید."
	}
	a.navigatePage(w, r, retPage, msg)
}

// handleOrderEdit ویرایش اقلام سفارش از دیالوگ وب؛ قیمت‌ها سمت سرور از منو خوانده می‌شوند.
func (a *Application) handleOrderEdit(w http.ResponseWriter, r *http.Request) {
	orderID := strings.TrimSpace(r.Form.Get("order_id"))
	prodIDs := r.Form["product_id"]
	qtys := r.Form["quantity"]
	notes := r.Form["line_note"]
	var lines []store.POSOrderLineInput
	for i := range prodIDs {
		pid := strings.TrimSpace(prodIDs[i])
		if pid == "" {
			continue
		}
		q := 1
		if i < len(qtys) {
			if parsed, perr := strconv.Atoi(strings.TrimSpace(qtys[i])); perr == nil && parsed > 0 {
				q = parsed
			} else {
				continue
			}
		}
		note := ""
		if i < len(notes) {
			note = strings.TrimSpace(notes[i])
		}
		lines = append(lines, store.POSOrderLineInput{ProductID: pid, Quantity: q, Note: note})
	}
	upd := store.OrderItemsUpdate{Lines: lines}
	if irr, ok := tomanFormToIRR(r.Form.Get("discount_toman")); ok {
		upd.DiscountIRR = &irr
	} else if v := strings.TrimSpace(r.Form.Get("discount_irr")); v != "" {
		upd.DiscountIRR = &v
	}
	if irr, ok := tomanFormToIRR(r.Form.Get("extra_toman")); ok {
		upd.ExtraIRR = &irr
	} else if v := strings.TrimSpace(r.Form.Get("extra_irr")); v != "" {
		upd.ExtraIRR = &v
	}
	if _, present := r.Form["extra_note"]; present {
		v := r.Form.Get("extra_note")
		upd.ExtraNote = &v
	}
	if _, present := r.Form["note"]; present {
		v := r.Form.Get("note")
		upd.Note = &v
	}
	page := strings.TrimSpace(r.Form.Get("return_page"))
	if page == "" {
		page = "orders"
	}
	if err := a.store.UpdateOrderItemsFull(r.Context(), orderID, upd); err != nil {
		a.navigatePage(w, r, page, err.Error())
		return
	}
	a.navigatePage(w, r, page, "اقلام سفارش به‌روز شد.")
}

// navigatePOS بازگشت به صفحه صندوق؛ در صورت نیاز دیالوگ دنگی همان سفارش دوباره باز می‌شود.
func (a *Application) navigatePOS(w http.ResponseWriter, r *http.Request, message, splitOrderID string) {
	if r.Header.Get("HX-Request") == "true" {
		if splitOrderID != "" {
			q := r.URL.Query()
			q.Set("split", splitOrderID)
			r.URL.RawQuery = q.Encode()
		}
		a.navigatePage(w, r, "pos", message)
		return
	}
	target := "/?page=pos"
	if splitOrderID != "" {
		target += "&split=" + splitOrderID
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// deferDueAt سررسید بدهی را از فیلدهای فرم (تاریخ مستقیم یا چند روز دیگر) می‌سازد.
func deferDueAt(r *http.Request) string {
	if d := strings.TrimSpace(r.Form.Get("defer_due_date")); d != "" {
		return d
	}
	if days, err := strconv.Atoi(strings.TrimSpace(r.Form.Get("defer_due_days"))); err == nil && days > 0 {
		return time.Now().AddDate(0, 0, days).Format("2006-01-02")
	}
	return ""
}

// handleOrderTransfer انتقال سفارش باز به میز دیگر (انتقال میز صندوق وب).
func (a *Application) handleOrderTransfer(w http.ResponseWriter, r *http.Request) {
	orderID := strings.TrimSpace(r.Form.Get("order_id"))
	targetID := strings.TrimSpace(r.Form.Get("target_table_id"))
	if err := a.store.TransferOrder(r.Context(), orderID, targetID); err != nil {
		a.navigatePOS(w, r, err.Error(), "")
		return
	}
	label := ""
	if t, tErr := a.store.Entity(r.Context(), "table", targetID); tErr == nil {
		label = t.Label()
	}
	a.navigatePOS(w, r, "سفارش به «"+label+"» منتقل شد؛ میز قبلی آزاد شد.", "")
}

// handleOrderAdvance بردن سفارش به مرحله بعدی فعال چرخه (کلید A صندوق وب).
func (a *Application) handleOrderAdvance(w http.ResponseWriter, r *http.Request) {
	orderID := strings.TrimSpace(r.Form.Get("order_id"))
	page := strings.TrimSpace(r.Form.Get("return_page"))
	if page == "" {
		page = "pos"
	}
	o, oErr := a.store.Entity(r.Context(), "order", orderID)
	if oErr != nil {
		a.navigatePage(w, r, page, "سفارش مورد نظر یافت نشد")
		return
	}
	key := strings.ToLower(strings.TrimSpace(o.Field("status")))
	target, label := orderAdvanceTarget(key, a.posEnabledStages(r.Context()))
	if target == "" {
		a.navigatePage(w, r, page, "مرحلهٔ بعدی فعالی برای این سفارش نیست؛ سفارش را تکمیل کنید.")
		return
	}
	if err := a.store.UpdateLocalOrderStatusReason(r.Context(), orderID, target, ""); err != nil {
		a.navigatePage(w, r, page, err.Error())
		return
	}
	a.navigatePage(w, r, page, "سفارش به مرحلهٔ «"+label+"» رفت.")
}

// handleSplitPay ثبت پرداخت یک نفر در حساب دنگی میز.
func (a *Application) handleSplitPay(w http.ResponseWriter, r *http.Request) {
	orderID := strings.TrimSpace(r.Form.Get("order_id"))
	picks := map[string]int{}
	for k, vals := range r.Form {
		if !strings.HasPrefix(k, "pick_") || len(vals) == 0 {
			continue
		}
		if q, qErr := strconv.Atoi(normalizeDigits(strings.TrimSpace(vals[0]))); qErr == nil && q > 0 {
			picks[strings.TrimPrefix(k, "pick_")] = q
		}
	}
	_, remaining, err := a.store.RecordSplitPayment(r.Context(), orderID, picks, r.Form.Get("method"), r.Form.Get("client_token"))
	if err != nil {
		a.navigatePOS(w, r, err.Error(), orderID)
		return
	}
	msg := "پرداخت دنگی ثبت شد؛ مانده: " + store.FormatPersianNumber(strconv.FormatInt(remaining/10, 10)) + " تومان"
	if remaining == 0 {
		msg = "حساب این میز کامل تسویه شد؛ از کارت میز «تکمیل و تسویه» را بزنید تا میز آزاد شود."
	}
	a.navigatePOS(w, r, msg, orderID)
}

// handleSplitUndo برگرداندن آخرین پرداخت دنگی.
func (a *Application) handleSplitUndo(w http.ResponseWriter, r *http.Request) {
	orderID := strings.TrimSpace(r.Form.Get("order_id"))
	if err := a.store.UndoLastSplitPayment(r.Context(), orderID); err != nil {
		a.navigatePOS(w, r, err.Error(), orderID)
		return
	}
	a.navigatePOS(w, r, "آخرین پرداخت دنگی برگردانده شد.", orderID)
}

// handleClubCustomer ثبت/ویرایش مشتری باشگاه (CustomerFormDialog وب):
// نام حداقل ۲ نویسه، موبایل ۱۱ رقم با ۰۹ (در ویرایش ثابت)، موبایل تکراری ممنوع.
func (a *Application) handleClubCustomer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := strings.TrimSpace(r.Form.Get("customer_id"))
	name := strings.TrimSpace(r.Form.Get("full_name"))
	if len([]rune(name)) < 2 {
		a.navigatePage(w, r, "customers", "نام باید حداقل ۲ نویسه باشد.")
		return
	}
	fields := map[string]any{
		"full_name":         name,
		"subscription_code": strings.TrimSpace(r.Form.Get("subscription_code")),
		"group_id":          strings.TrimSpace(r.Form.Get("group_id")),
		"credit_enabled":    boolStr(r.Form.Get("credit_enabled") == "on"),
	}
	if id == "" {
		mobile := normalizeDigits(strings.TrimSpace(r.Form.Get("phone")))
		if len(mobile) != 11 || !strings.HasPrefix(mobile, "09") {
			a.navigatePage(w, r, "customers", "شماره موبایل باید ۱۱ رقم و با ۰۹ شروع شود.")
			return
		}
		if customers, cErr := a.store.Entities(ctx, "customer"); cErr == nil {
			for _, c := range customers {
				if strings.TrimSpace(c.Field("phone")) == mobile {
					a.navigatePage(w, r, "customers", "این شماره موبایل قبلاً برای مشتری دیگری ثبت شده است.")
					return
				}
			}
		}
		fields["phone"] = mobile
		fields["club_member"] = boolStr(r.Form.Get("enable_club") == "on")
		if pts := strings.TrimSpace(r.Form.Get("initial_points")); pts != "" && r.Form.Get("enable_club") == "on" {
			fields["loyalty_points"] = normalizeDigits(pts)
		}
	} else if _, eErr := a.store.Entity(ctx, "customer", id); eErr != nil {
		a.navigatePage(w, r, "customers", "مشتری مورد نظر یافت نشد.")
		return
	}
	if _, err := a.store.SaveLocalRecord(ctx, "customer", id, fields); err != nil {
		a.navigatePage(w, r, "customers", err.Error())
		return
	}
	msg := "مشتری با موفقیت ثبت شد."
	if id != "" {
		msg = "مشتری ویرایش شد."
	}
	a.navigatePage(w, r, "customers", msg)
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// handleClubCreditToggle روشن/خاموش کردن مجوز خرید اعتباری مشتری (سوییچ جدول وب).
func (a *Application) handleClubCreditToggle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := strings.TrimSpace(r.Form.Get("customer_id"))
	c, err := a.store.Entity(ctx, "customer", id)
	if err != nil {
		a.navigatePage(w, r, "customers", "مشتری مورد نظر یافت نشد.")
		return
	}
	next := "true"
	if c.Field("credit_enabled") == "true" {
		next = "false"
	}
	if _, err := a.store.SaveLocalRecord(ctx, "customer", id, map[string]any{"credit_enabled": next}); err != nil {
		a.navigatePage(w, r, "customers", err.Error())
		return
	}
	msg := "خرید اعتباری (نسیه) برای «" + c.Label() + "» غیرفعال شد."
	if next == "true" {
		msg = "خرید اعتباری (نسیه) برای «" + c.Label() + "» فعال شد."
	}
	a.navigatePage(w, r, "customers", msg)
}

// handleClubPayment ثبت دستی پرداخت اشتراک (معادل آفلاین خرید آنلاین اشتراک):
// پرداخت در انتظار تایید ساخته می‌شود تا با کد پیگیری تایید شود.
func (a *Application) handleClubPayment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	customerID := strings.TrimSpace(r.Form.Get("customer_id"))
	c, err := a.store.Entity(ctx, "customer", customerID)
	if err != nil {
		a.navigatePage(w, r, "customers", "مشتری اشتراک را انتخاب کنید.")
		return
	}
	irr, ok := tomanFormToIRR(r.Form.Get("amount_toman"))
	if !ok {
		a.navigatePage(w, r, "customers", "مبلغ اشتراک معتبر نیست.")
		return
	}
	plan := strings.TrimSpace(r.Form.Get("plan_title"))
	if plan == "" {
		plan = "پلن عضویت"
	}
	method := strings.TrimSpace(r.Form.Get("payment_method"))
	if method == "" {
		method = "card"
	}
	_, err = a.store.SaveLocalRecord(ctx, "membership_payment", "", map[string]any{
		"customer_id":    customerID,
		"consumer_name":  c.Label(),
		"consumer_phone": strings.TrimSpace(c.Field("phone")),
		"plan_title":     plan,
		"amount_irr":     irr,
		"payment_method": method,
		"status":         "pending",
	})
	if err != nil {
		a.navigatePage(w, r, "customers", err.Error())
		return
	}
	q := r.URL.Query()
	q.Set("tab", "membership")
	q.Set("sub", "payments")
	r.URL.RawQuery = q.Encode()
	a.navigatePage(w, r, "customers", "پرداخت اشتراک در انتظار تایید ثبت شد.")
}

// handleClubPaymentConfirm تایید پرداخت اشتراک با کد پیگیری (دیالوگ وب):
// اشتراک مشتری فعال و عضویت باشگاه او روشن می‌شود.
func (a *Application) handleClubPaymentConfirm(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	paymentID := strings.TrimSpace(r.Form.Get("payment_id"))
	reference := strings.TrimSpace(r.Form.Get("reference_number"))
	if reference == "" {
		a.navigatePage(w, r, "customers", "کد پیگیری پرداخت را وارد کنید.")
		return
	}
	p, err := a.store.Entity(ctx, "membership_payment", paymentID)
	if err != nil {
		a.navigatePage(w, r, "customers", "پرداخت مورد نظر یافت نشد.")
		return
	}
	if !strings.EqualFold(p.Field("status"), "pending") {
		a.navigatePage(w, r, "customers", "این پرداخت قبلاً تعیین تکلیف شده است.")
		return
	}
	if _, err := a.store.SaveLocalRecord(ctx, "membership_payment", paymentID, map[string]any{
		"status": "paid", "reference_number": reference,
		"paid_at": time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		a.navigatePage(w, r, "customers", err.Error())
		return
	}
	if cid := strings.TrimSpace(p.Field("customer_id")); cid != "" {
		_, _ = a.store.SaveLocalRecord(ctx, "customer", cid, map[string]any{"club_member": "true"})
	}
	q := r.URL.Query()
	q.Set("tab", "membership")
	q.Set("sub", "payments")
	r.URL.RawQuery = q.Encode()
	a.navigatePage(w, r, "customers", "پرداخت تایید و پلن مشتری فعال شد.")
}

// handleClubGroup ثبت/ویرایش گروه مشتریان (group-form وب).
func (a *Application) handleClubGroup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := strings.TrimSpace(r.Form.Get("group_id"))
	name := strings.TrimSpace(r.Form.Get("name"))
	if len([]rune(name)) < 2 {
		a.clubGroupFormError(w, r, "نام گروه باید حداقل ۲ نویسه باشد.")
		return
	}
	if day := strings.TrimSpace(r.Form.Get("billing_day")); day != "" {
		if n, nErr := strconv.Atoi(normalizeDigits(day)); nErr != nil || n < 1 || n > 31 {
			a.clubGroupFormError(w, r, "روز صورتحساب باید بین ۱ تا ۳۱ باشد.")
			return
		}
	}
	fields := map[string]any{}
	for _, k := range []string{"name", "description", "contact_name", "contact_phone", "contract_number", "contract_type", "headcount", "meals_per_day", "billing_cycle", "billing_day", "service_address", "discount_type", "discount_value", "max_discount_amount", "valid_from", "valid_until", "notes"} {
		fields[k] = strings.TrimSpace(r.Form.Get(k))
	}
	fields["credit_enabled"] = boolStr(r.Form.Get("credit_enabled") == "on")
	if id == "" {
		fields["is_active"] = "true"
	}
	savedID, err := a.store.SaveLocalRecord(ctx, "customer_group", id, fields)
	if err != nil {
		a.clubGroupFormError(w, r, err.Error())
		return
	}
	msg := "گروه مشتریان ساخته شد."
	if id != "" {
		msg = "گروه مشتریان ویرایش شد."
	}
	// بعد از ذخیره در همان صفحه‌ای می‌مانیم که دیالوگ از آن باز شده است
	// (فهرست گروه‌ها یا جزئیات گروه یا فهرست مشتریان)؛ دیگر پرش اجباری به
	// صفحهٔ جزئیات نداریم.
	target := "club-groups"
	switch strings.TrimSpace(r.Form.Get("return_page")) {
	case "customers":
		target = "customers"
	case "club-group":
		target = "club-group"
	}
	query := url.Values{}
	if target == "club-group" {
		query.Set("id", savedID)
	}
	if extra, pErr := url.ParseQuery(strings.TrimSpace(r.Form.Get("return_query"))); pErr == nil {
		for k, vals := range extra {
			if k == "page" || k == "id" || k == "dlg" || len(vals) == 0 {
				continue
			}
			query.Set(k, vals[0])
		}
	}
	if r.Header.Get("HX-Request") == "true" {
		// navigatePage خودش page را ست می‌کند؛ فقط فیلترها/شناسه را نگه می‌داریم.
		r.URL.RawQuery = query.Encode()
		a.navigatePage(w, r, target, msg)
		return
	}
	dest := "/?page=" + target
	if encoded := query.Encode(); encoded != "" {
		dest += "&" + encoded
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

// handleClubGroupToggle فعال/توقف قرارداد گروه.
func (a *Application) handleClubGroupToggle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := strings.TrimSpace(r.Form.Get("group_id"))
	g, err := a.store.Entity(ctx, "customer_group", id)
	if err != nil {
		a.navigatePage(w, r, "club-groups", "گروه مورد نظر یافت نشد.")
		return
	}
	next := "false"
	if g.Field("is_active") == "false" {
		next = "true"
	}
	if _, err := a.store.SaveLocalRecord(ctx, "customer_group", id, map[string]any{"is_active": next}); err != nil {
		a.navigatePage(w, r, "club-groups", err.Error())
		return
	}
	msg := "قرارداد غیرفعال شد."
	if next == "true" {
		msg = "قرارداد فعال شد."
	}
	a.navigatePage(w, r, "club-groups", msg)
}

// handleClubGroupCredit فعال/لغو خرید اعتباری گروه.
func (a *Application) handleClubGroupCredit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := strings.TrimSpace(r.Form.Get("group_id"))
	g, err := a.store.Entity(ctx, "customer_group", id)
	if err != nil {
		a.navigatePage(w, r, "club-groups", "گروه مورد نظر یافت نشد.")
		return
	}
	next := "true"
	if g.Field("credit_enabled") == "true" {
		next = "false"
	}
	if _, err := a.store.SaveLocalRecord(ctx, "customer_group", id, map[string]any{"credit_enabled": next}); err != nil {
		a.navigatePage(w, r, "club-groups", err.Error())
		return
	}
	msg := "خرید اعتباری (نسیه) برای گروه «" + g.Label() + "» غیرفعال شد."
	if next == "true" {
		msg = "خرید اعتباری (نسیه) برای گروه «" + g.Label() + "» فعال شد."
	}
	a.navigatePage(w, r, "club-groups", msg)
}

// handleClubGroupDelete حذف گروه؛ اعضا از گروه جدا می‌شوند (مانند وب).
func (a *Application) handleClubGroupDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := strings.TrimSpace(r.Form.Get("group_id"))
	customers, _ := a.store.Entities(ctx, "customer")
	for _, c := range customers {
		if strings.TrimSpace(c.Field("group_id")) == id {
			_, _ = a.store.SaveLocalRecord(ctx, "customer", c.ID, map[string]any{"group_id": ""})
		}
	}
	if err := a.store.DeleteLocalRecord(ctx, "customer_group", id); err != nil {
		a.navigatePage(w, r, "club-groups", err.Error())
		return
	}
	a.navigatePage(w, r, "club-groups", "گروه مشتریان حذف شد.")
}

// handleClubGroupMember افزودن عضو به گروه با شماره موبایل (مشتری جدید هم ساخته می‌شود).
func (a *Application) handleClubGroupMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	groupID := strings.TrimSpace(r.Form.Get("group_id"))
	mobile := normalizeDigits(strings.TrimSpace(r.Form.Get("mobile")))
	back := func(msg string) {
		if r.Header.Get("HX-Request") == "true" {
			q := r.URL.Query()
			q.Set("id", groupID)
			r.URL.RawQuery = q.Encode()
			a.navigatePage(w, r, "club-group", msg)
			return
		}
		http.Redirect(w, r, "/?page=club-group&id="+groupID, http.StatusSeeOther)
	}
	if len(mobile) != 11 || !strings.HasPrefix(mobile, "09") {
		back("شماره موبایل باید ۱۱ رقم و با ۰۹ شروع شود.")
		return
	}
	customers, _ := a.store.Entities(ctx, "customer")
	for _, c := range customers {
		if strings.TrimSpace(c.Field("phone")) == mobile {
			if _, err := a.store.SaveLocalRecord(ctx, "customer", c.ID, map[string]any{"group_id": groupID}); err != nil {
				back(err.Error())
				return
			}
			back("عضو به گروه اضافه شد.")
			return
		}
	}
	name := strings.TrimSpace(r.Form.Get("full_name"))
	if _, err := a.store.SaveLocalRecord(ctx, "customer", "", map[string]any{
		"full_name": name, "phone": mobile, "group_id": groupID, "club_member": "true",
	}); err != nil {
		back(err.Error())
		return
	}
	back("مشتری جدید ساخته و به گروه اضافه شد.")
}

// handleClubGroupMemberRemove جدا کردن عضو از گروه.
func (a *Application) handleClubGroupMemberRemove(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	groupID := strings.TrimSpace(r.Form.Get("group_id"))
	customerID := strings.TrimSpace(r.Form.Get("customer_id"))
	if _, err := a.store.SaveLocalRecord(ctx, "customer", customerID, map[string]any{"group_id": ""}); err != nil {
		a.navigatePage(w, r, "club-group", err.Error())
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		q := r.URL.Query()
		q.Set("id", groupID)
		r.URL.RawQuery = q.Encode()
	}
	a.navigatePage(w, r, "club-group", "عضو از گروه جدا شد.")
}

// handlePOSStages ذخیره مراحل فعال چرخه سفارش (PosOrderWorkflowControl وب).
func (a *Application) handlePOSStages(w http.ResponseWriter, r *http.Request) {
	var enabled []string
	for _, d := range posStageDefs {
		if r.Form.Get("stage_"+d.Key) == "on" {
			enabled = append(enabled, d.Key)
		}
	}
	stagesVal := strings.Join(enabled, ",")
	if stagesVal == "" {
		stagesVal = "none" // همه مراحل غیرفعال (SaveLocalRecord مقدار خالی را رد می‌کند)
	}
	if _, err := a.store.SaveLocalRecord(r.Context(), "cafe_profile", "cafe-main-profile", map[string]any{"pos_stages": stagesVal}); err != nil {
		a.navigatePage(w, r, "pos", err.Error())
		return
	}
	a.navigatePage(w, r, "pos", "مراحل سفارش ذخیره شد.")
}

// handleOrderDefer انتقال سفارش به بدهکاران از دیالوگ «بعداً پرداخت می‌کنند» وب.
func (a *Application) handleOrderDefer(w http.ResponseWriter, r *http.Request) {
	orderID := strings.TrimSpace(r.Form.Get("order_id"))
	dueAt := strings.TrimSpace(r.Form.Get("due_date"))
	if dueAt == "" {
		if days, derr := strconv.Atoi(strings.TrimSpace(r.Form.Get("due_days"))); derr == nil && days > 0 {
			dueAt = time.Now().AddDate(0, 0, days).Format("2006-01-02")
		}
	}
	page := strings.TrimSpace(r.Form.Get("return_page"))
	if page != "pos" {
		page = "orders"
	}
	if _, err := a.store.DeferOrderToDebtor(r.Context(), orderID,
		r.Form.Get("customer_name"), r.Form.Get("customer_phone"), dueAt, r.Form.Get("note")); err != nil {
		a.navigatePage(w, r, page, err.Error())
		return
	}
	a.navigatePage(w, r, page, "سفارش در لیست بدهکاران ثبت شد؛ از بخش بدهکاران قابل تسویه است.")
}

// handleWasteVoid ابطال رویداد ضایعات (برگرداندن مقدار به موجودی).
func (a *Application) handleWasteVoid(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.Form.Get("id"))
	if id == "" {
		a.navigatePage(w, r, "waste", "شناسه رویداد ضایعات ارسال نشده است.")
		return
	}
	if err := a.store.VoidWasteEvent(r.Context(), id); err != nil {
		a.navigatePage(w, r, "waste", err.Error())
		return
	}
	a.navigatePage(w, r, "waste", "رویداد ضایعات ابطال شد و مقدار آن به موجودی برگشت.")
}

var stockChangeQuantityPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]{1,2})?$`)

func (a *Application) handleStockMovement(w http.ResponseWriter, r *http.Request) {
	target := "ingredients"
	if rp := strings.TrimSpace(r.Form.Get("return_page")); rp == "waste" || rp == "ingredients" {
		target = rp
	}

	// مسیر هم‌تراز با InventoryAdjustDialog وب: کاربر سناریو را انتخاب می‌کند،
	// نه نوع فنی سند. در «adjustment» مقدار، موجودی نهاییِ شمارش‌شده است.
	if direction := strings.TrimSpace(r.Form.Get("direction")); direction != "" {
		reasons := map[string][]string{
			"incoming":   {"رسید خرید", "برگشت از ضایعات", "اصلاح شمارش"},
			"outgoing":   {"مصرف روزانه", "تسویه ضایعات", "انتقال بین انبارها"},
			"adjustment": {"اصلاح شمارش", "خطای ثبت قبلی", "برگشت از ضایعات"},
		}
		allowedReasons, ok := reasons[direction]
		if !ok {
			a.navigatePage(w, r, target, "سناریوی تغییر موجودی معتبر نیست.")
			return
		}
		quantity := strings.TrimSpace(r.Form.Get("quantity"))
		qty, qErr := strconv.ParseFloat(quantity, 64)
		if !stockChangeQuantityPattern.MatchString(quantity) || qErr != nil || qty <= 0 {
			a.navigatePage(w, r, target, "مقدار تغییر موجودی باید عددی بزرگ‌تر از صفر و حداکثر با دو رقم اعشار باشد.")
			return
		}
		reason := strings.TrimSpace(r.Form.Get("reason"))
		if reason == "" {
			reason = allowedReasons[0]
		}
		reasonOK := false
		for _, allowed := range allowedReasons {
			if reason == allowed {
				reasonOK = true
				break
			}
		}
		if !reasonOK {
			a.navigatePage(w, r, target, "دلیل انتخاب‌شده برای این سناریو معتبر نیست.")
			return
		}
		txType := map[string]string{
			"incoming":   "incoming",
			"outgoing":   "outgoing",
			"adjustment": "adjustment_target",
		}[direction]
		if _, err := a.store.RecordStockMovement(r.Context(), r.Form.Get("ingredient_id"), txType, quantity, "", reason); err != nil {
			a.navigatePage(w, r, target, "خطا در ثبت تغییر موجودی: "+err.Error())
			return
		}
		messages := map[string]string{
			"incoming":   "موجودی قلم افزایش یافت.",
			"outgoing":   "مصرف کالا ثبت شد.",
			"adjustment": "موجودی اصلاح شد.",
		}
		a.navigatePage(w, r, target, messages[direction])
		return
	}

	note := r.Form.Get("description")
	// دلیل ضایعات مانند وب کد استاندارد دارد (expired/spoilage/…) و جدا از
	// یادداشت آزاد ذخیره می‌شود تا گزارش «هزینه بر اساس دلیل» درست گروه‌بندی شود.
	if r.Form.Get("transaction_type") == "waste" {
		if reason := strings.TrimSpace(r.Form.Get("reason")); reason != "" {
			note = reason
			if desc := strings.TrimSpace(r.Form.Get("description")); desc != "" {
				note = reason + "::" + desc
			}
		}
	}
	_, err := a.store.RecordStockMovement(
		r.Context(),
		r.Form.Get("ingredient_id"),
		r.Form.Get("transaction_type"),
		r.Form.Get("quantity"),
		r.Form.Get("unit_cost"),
		note,
	)
	if err != nil {
		a.navigatePage(w, r, target, "خطا در ثبت سند انبار: "+err.Error())
		return
	}
	a.navigatePage(w, r, target, "سند گردش انبار ثبت شد و موجودی فعلی کالا بلافاصله به‌روزرسانی گردید.")
}

func (a *Application) handleSaveRecipe(w http.ResponseWriter, r *http.Request) {
	prodID := strings.TrimSpace(r.Form.Get("product_id"))
	if prodID == "" {
		a.navigatePage(w, r, "recipes", "لطفاً محصول مربوط به رسپی را انتخاب کنید.")
		return
	}
	prodEnt, err := a.store.Entity(r.Context(), "product", prodID)
	if err != nil {
		a.navigatePage(w, r, "recipes", "محصول انتخاب شده یافت نشد.")
		return
	}
	ingIDs := r.Form["ingredient_id"]
	qtys := r.Form["quantity"]
	var items []map[string]any
	for i := range ingIDs {
		iid := strings.TrimSpace(ingIDs[i])
		if iid == "" || i >= len(qtys) || strings.TrimSpace(qtys[i]) == "" {
			continue
		}
		ingEnt, iErr := a.store.Entity(r.Context(), "ingredient", iid)
		if iErr != nil {
			continue
		}
		// مقدار مصرف همیشه عددی تمیز ذخیره می‌شود تا کسر خودکار انبار
		// (که parseFloat روی همین رشته انجام می‌شود) درست کار کند.
		qtyStr := strings.NewReplacer(",", "", "٬", "", " ", "").Replace(normalizeNumInput(qtys[i]))
		qty, qErr := strconv.ParseFloat(qtyStr, 64)
		if qErr != nil || qty <= 0 {
			continue
		}
		items = append(items, map[string]any{
			"ingredient_id":   iid,
			"ingredient_name": ingEnt.Label(),
			"quantity":        strconv.FormatFloat(qty, 'f', -1, 64),
			"unit":            ingEnt.Field("base_unit"),
		})
	}
	if len(items) == 0 {
		a.navigatePage(w, r, "recipes", "لطفاً حداقل یک ماده اولیه و مقدار مصرفی آن را وارد کنید.")
		return
	}
	_, err = a.store.SaveLocalRecord(r.Context(), "recipe", strings.TrimSpace(r.Form.Get("recipe_id")), map[string]any{
		"title":        "رسپی " + prodEnt.Label(),
		"product_id":   prodID,
		"product_name": prodEnt.Label(),
		"yield_qty":    "1",
		"note":         strings.TrimSpace(r.Form.Get("note")),
		"items":        items,
	})
	if err != nil {
		a.navigatePage(w, r, "recipes", err.Error())
		return
	}
	a.navigatePage(w, r, "recipes", "فرمول ساخت (BOM) محصول ذخیره شد. در زمان تکمیل سفارش، این مقادیر به‌صورت خودکار از انبار کسر می‌شوند.")
}

// handlePrinterToggle انتخاب صریح کاربر برای استفاده از چاپگر حرارتی؛
// کافه‌ای که فیش‌پرینتر ندارد با خاموش‌کردن این گزینه هیچ دکمه و چاپی نمی‌بیند.
func (a *Application) handlePrinterToggle(w http.ResponseWriter, r *http.Request) {
	enabled := r.Form.Get("enabled") == "on" || r.Form.Get("enabled") == "true"
	val := "false"
	if enabled {
		val = "true"
	}
	if _, err := a.store.SaveLocalRecord(r.Context(), "cafe_profile", "cafe-main-profile", map[string]any{"printer_enabled": val}); err != nil {
		a.navigatePage(w, r, "settings", "خطا در ذخیره تنظیم چاپگر: "+err.Error())
		return
	}
	retPage := strings.TrimSpace(r.Form.Get("return_page"))
	if retPage == "" {
		retPage = "settings"
	}
	if enabled {
		a.navigatePage(w, r, retPage, "چاپگر حرارتی فعال شد؛ حالا چاپگرها را تعریف کنید تا فیش‌ها مستقیم چاپ شوند.")
		return
	}
	a.navigatePage(w, r, retPage, "چاپگر حرارتی غیرفعال شد؛ دکمه‌های چاپ پنهان می‌شوند و هیچ فیشی ارسال نمی‌شود.")
}

// handlePrintPolicy ذخیره سیاست چاپ خودکار (PrintWorkflowPolicy وب) در
// دیتابیس محلی؛ هر گروهی که در فرم آمده باشد به‌روز می‌شود تا دکمه‌های
// سریع (مثل «تسویه با دو فیش») فقط همان لحظه را تغییر دهند.
func (a *Application) handlePrintPolicy(w http.ResponseWriter, r *http.Request) {
	modes := map[string]bool{"none": true, "kitchen": true, "customer": true, "both": true, "custom": true}
	docs := map[string]bool{"kitchen": true, "cashier": true, "all": true}
	fields := map[string]any{}
	for _, g := range []string{"create", "notable", "checkout", "complete"} {
		mode := strings.TrimSpace(r.Form.Get("pw_" + g + "_mode"))
		if mode == "" {
			continue
		}
		if !modes[mode] {
			a.navigatePage(w, r, "print-policy", "سناریوی چاپ نامعتبر است.")
			return
		}
		fields["pw_"+g+"_mode"] = mode
		doc := strings.TrimSpace(r.Form.Get("pw_" + g + "_doc"))
		if !docs[doc] {
			doc = "kitchen"
			if g == "checkout" || g == "complete" {
				doc = "cashier"
			}
		}
		fields["pw_"+g+"_doc"] = doc
		copies := 1
		if n, aErr := strconv.Atoi(normalizeDigits(strings.TrimSpace(r.Form.Get("pw_" + g + "_copies")))); aErr == nil && n >= 1 && n <= 5 {
			copies = n
		}
		fields["pw_"+g+"_copies"] = strconv.Itoa(copies)
		if g == "checkout" {
			fields["pw_checkout_merchant"] = boolStr(r.Form.Get("pw_checkout_merchant") == "on")
		}
	}
	if len(fields) == 0 {
		a.navigatePage(w, r, "print-policy", "تغییری برای ذخیره ارسال نشد.")
		return
	}
	if _, err := a.store.SaveLocalRecord(r.Context(), "cafe_profile", "cafe-main-profile", fields); err != nil {
		a.navigatePage(w, r, "print-policy", "خطا در ذخیره تنظیمات چاپ: "+err.Error())
		return
	}
	a.navigatePage(w, r, "print-policy", "تنظیمات چاپ خودکار ذخیره شد.")
}

// handlePrinterRouteSave تعیین مقصد چاپ دسته‌بندی‌های منو (کدام چاپگر بلیت
// کدام دسته را بگیرد). چند مقدار category_id در یک درخواست پذیرفته می‌شود تا
// کاربر بتواند مثلاً ده دسته را یک‌جا به یک چاپگر بدهد؛ printer_id خالی یعنی
// حذف مسیر اختصاصی دسته‌های انتخاب‌شده. یک category_id هم مثل قبل کار می‌کند.
func (a *Application) handlePrinterRouteSave(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	_ = r.ParseForm()
	seen := map[string]bool{}
	var catIDs []string
	for _, raw := range r.Form["category_id"] {
		if id := strings.TrimSpace(raw); id != "" && !seen[id] {
			seen[id] = true
			catIDs = append(catIDs, id)
		}
	}
	if len(catIDs) == 0 {
		a.navigatePage(w, r, "print-routes", "هیچ دسته‌بندی‌ای انتخاب نشده است؛ اول دسته‌ها را تیک بزنید.")
		return
	}
	printerID := strings.TrimSpace(r.Form.Get("printer_id"))
	var printer store.Entity
	if printerID != "" {
		p, pErr := a.store.Entity(ctx, "printer", printerID)
		if pErr != nil {
			a.navigatePage(w, r, "print-routes", "چاپگر مقصد یافت نشد.")
			return
		}
		printer = p
	}
	copies := 1
	if n, aErr := strconv.Atoi(normalizeDigits(strings.TrimSpace(r.Form.Get("copies")))); aErr == nil && n >= 1 && n <= 5 {
		copies = n
	}
	routes, _ := a.store.Entities(ctx, "printer_route")
	existingByCat := map[string]store.Entity{}
	for _, rt := range routes {
		if cid := strings.TrimSpace(rt.Field("category_id")); cid != "" {
			existingByCat[cid] = rt
		}
	}
	saved, removed, missing := 0, 0, 0
	for _, catID := range catIDs {
		cat, err := a.store.Entity(ctx, "category", catID)
		if err != nil {
			missing++
			continue
		}
		existingID, existingActive := "", "true"
		if rt, ok := existingByCat[catID]; ok {
			existingID = rt.ID
			if v := strings.TrimSpace(rt.Field("is_active")); v != "" {
				existingActive = v
			}
		}
		if printerID == "" {
			if existingID != "" {
				if err := a.store.DeleteLocalRecord(ctx, "printer_route", existingID); err != nil {
					a.navigatePage(w, r, "print-routes", "خطا در حذف مسیر چاپ: "+err.Error())
					return
				}
				removed++
			}
			continue
		}
		if _, err := a.store.SaveLocalRecord(ctx, "printer_route", existingID, map[string]any{
			"title":         cat.Label(),
			"category_id":   catID,
			"category_name": cat.Label(),
			"printer_id":    printerID,
			"printer_name":  printer.Label(),
			"receipt_type":  "kitchen",
			"copies":        strconv.Itoa(copies),
			"is_active":     existingActive,
		}); err != nil {
			a.navigatePage(w, r, "print-routes", "خطا در ذخیره مسیر چاپ: "+err.Error())
			return
		}
		saved++
	}
	switch {
	case missing > 0 && saved == 0 && removed == 0:
		a.navigatePage(w, r, "print-routes", "دسته‌بندی‌های انتخاب‌شده یافت نشدند.")
	case printerID == "":
		a.navigatePage(w, r, "print-routes", "مسیر چاپ "+strconv.Itoa(removed)+" دسته حذف شد؛ بلیت آن‌ها به پرینتر پیش‌فرض آشپزخانه می‌رود.")
	case len(catIDs) == 1 && saved == 1:
		a.navigatePage(w, r, "print-routes", "مسیر چاپ دسته انتخاب‌شده به چاپگر «"+printer.Label()+"» ذخیره شد.")
	default:
		msg := "مسیر چاپ " + strconv.Itoa(saved) + " دسته به چاپگر «" + printer.Label() + "» ذخیره شد."
		if missing > 0 {
			msg += " (" + strconv.Itoa(missing) + " دسته یافت نشد.)"
		}
		a.navigatePage(w, r, "print-routes", msg)
	}
}

// handlePrinterRouteToggle توقف/فعال‌سازی موقت مسیر چاپ یک دسته.
func (a *Application) handlePrinterRouteToggle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	catID := strings.TrimSpace(r.Form.Get("category_id"))
	routes, _ := a.store.Entities(ctx, "printer_route")
	for _, rt := range routes {
		if strings.TrimSpace(rt.Field("category_id")) == catID {
			next := "false"
			state := "متوقف"
			if rt.Field("is_active") == "false" || rt.Field("is_active") == "0" {
				next = "true"
				state = "فعال"
			}
			if _, err := a.store.SaveLocalRecord(ctx, "printer_route", rt.ID, map[string]any{"is_active": next}); err != nil {
				a.navigatePage(w, r, "print-routes", err.Error())
				return
			}
			a.navigatePage(w, r, "print-routes", "مسیر چاپ این دسته "+state+" شد.")
			return
		}
	}
	a.navigatePage(w, r, "print-routes", "برای این دسته هنوز مقصد چاپی انتخاب نشده است.")
}

// handlePrintOrder چاپ مستقیم همه فیش‌های یک سفارش (مشتری + حواله بار)
// بدون باز شدن پنجره Print مرورگر؛ نتیجه واقعی Jobها نمایش داده می‌شود.
func (a *Application) handlePrintOrder(w http.ResponseWriter, r *http.Request) {
	if !a.printerEnabled(r.Context()) {
		if r.Form.Get("ajax") == "1" {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "message": printerDisabledMsg})
			return
		}
		a.navigatePage(w, r, "orders", printerDisabledMsg)
		return
	}
	orderID := strings.TrimSpace(r.Form.Get("order_id"))
	if orderID == "" {
		a.navigatePage(w, r, "receipts", "شناسه سفارش برای چاپ مشخص نیست.")
		return
	}
	// چاپ یک سند مشخص از منوی ردیف (فیش مشتری یا بلیت آشپزخانه) مانند وب:
	// فقط Job همان سند ارسال می‌شود و چاپ دوبارهٔ دستی هم ممکن است.
	if doc := strings.TrimSpace(r.Form.Get("doc")); doc == "customer" || doc == "kitchen" {
		jobs, jErr := a.store.EnqueueOrderPrintJobs(r.Context(), orderID)
		if jErr != nil {
			a.navigatePage(w, r, "orders", jErr.Error())
			return
		}
		label := "فیش مشتری"
		if doc == "kitchen" {
			label = "بلیت آشپزخانه"
		}
		printed, sent, failed, found := 0, 0, 0, false
		for _, j := range jobs {
			if j.DocType != doc {
				continue
			}
			found = true
			if j.Uncertain {
				failed++
				continue
			}
			if j.Status == store.PrintStatusPrinted || j.Status == store.PrintStatusSent {
				_, _ = a.store.UpdatePrintJobStatus(r.Context(), j.ID, store.PrintStatusPending, "")
				j.Status = store.PrintStatusPending
				j.Uncertain = false
			}
			if j.Status == store.PrintStatusFailed && j.PrinterID == "" {
				failed++
				continue
			}
			done := a.dispatchPrintJob(r.Context(), j)
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
		msg := label + " چاپ شد."
		if sent > 0 && printed == 0 {
			msg = label + " به چاپگر ارسال شد؛ چاپگرهای RAW تأیید فیزیکی برنمی‌گردانند."
		}
		switch {
		case !found:
			msg = "برای «" + label + "» چاپگر مقصدی تعریف نشده است؛ از تنظیمات چاپگرها بررسی کنید."
		case delivered == 0 && failed > 0:
			msg = "چاپ " + label + " ناموفق بود؛ از «فیش‌ها» تلاش مجدد بزنید."
		case failed > 0:
			msg = label + " ارسال شد و یک مسیر چاپ خطا دارد."
		}
		if r.Form.Get("ajax") == "1" {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			ok := found && (printed+sent) > 0
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": ok, "message": msg})
			return
		}
		a.navigatePage(w, r, "orders", msg)
		return
	}
	_, summary := a.enqueueAndDispatchPrintJobs(r.Context(), orderID)
	q := r.URL.Query()
	q.Set("page", "receipt")
	q.Set("id", orderID)
	r.URL.RawQuery = q.Encode()
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Push-Url", "/?page=receipt&id="+orderID)
	}
	a.render(w, r, summary)
}

// handlePrintRetry تلاش دوباره یک Job ناموفق؛ Job تحویل‌شده دوباره چاپ نمی‌شود.
// نتیجهٔ نامشخص (مثلاً timeout وسط نوشتن) عمداً قابل تلاش دوباره نیست تا فیش
// دوبار چاپ نشود؛ کاربر باید ابتدا کاغذ را بررسی کند.
func (a *Application) handlePrintRetry(w http.ResponseWriter, r *http.Request) {
	if !a.printerEnabled(r.Context()) {
		a.navigatePage(w, r, "receipts", printerDisabledMsg)
		return
	}
	jobID := strings.TrimSpace(r.Form.Get("job_id"))
	job, err := a.store.PrintJob(r.Context(), jobID)
	if err != nil {
		a.navigatePage(w, r, "receipts", err.Error())
		return
	}
	if !job.Retryable() {
		a.navigatePage(w, r, "receipts", "این Job قابل ارسال دوباره نیست: "+job.RetryBlockReason())
		return
	}
	done := a.dispatchPrintJob(r.Context(), job)
	if done.Status == store.PrintStatusPrinted || done.Status == store.PrintStatusSent {
		a.navigatePage(w, r, "receipts", printOutcomeMessage(done))
		return
	}
	a.navigatePage(w, r, "receipts", "چاپ دوباره ناموفق بود: "+done.LastError)
}

// handlePrintTest چاپ آزمایشی یک چاپگر از تنظیمات (بدون ساخت سفارش صوری).
func (a *Application) handlePrintTest(w http.ResponseWriter, r *http.Request) {
	if !a.printerEnabled(r.Context()) {
		ret := strings.TrimSpace(r.Form.Get("return_page"))
		if ret == "" {
			ret = "printers"
		}
		a.navigatePage(w, r, ret, printerDisabledMsg)
		return
	}
	retPage := strings.TrimSpace(r.Form.Get("return_page"))
	if retPage == "" {
		retPage = "printers"
	}
	printerID := strings.TrimSpace(r.Form.Get("printer_id"))
	job, err := a.store.EnqueueTestPrintJob(r.Context(), printerID)
	if err != nil {
		a.navigatePage(w, r, retPage, err.Error())
		return
	}
	done := a.dispatchPrintJob(r.Context(), job)
	message := printOutcomeMessage(done)
	if retPage == "printer" && printerID != "" {
		a.navigatePrinter(w, r, printerID, message)
		return
	}
	a.navigatePage(w, r, retPage, message)
}

// handleDebtorPay ثبت پرداخت (کامل یا اقساطی) روی حساب دفتری؛ همان دکمه‌های
// «تسویه کامل» و «ثبت پرداخت» نسخه وب. مبلغ تومانی فرم به ریال تبدیل می‌شود و
// پرداخت در دفتر وصولی ثبت می‌گردد.
func (a *Application) handleDebtorPay(w http.ResponseWriter, r *http.Request) {
	debtorID := strings.TrimSpace(r.Form.Get("debtor_id"))
	amount := strings.TrimSpace(r.Form.Get("amount_irr"))
	full := r.Form.Get("pay_mode") == "full" || r.Form.Get("full") == "1"
	if full && debtorID != "" {
		if ent, err := a.store.Entity(r.Context(), "debtor", debtorID); err == nil {
			amount = strings.TrimSpace(ent.Field("remaining"))
		}
	}
	if amount == "" {
		if irr, ok := tomanFormToIRR(r.Form.Get("amount_toman")); ok {
			amount = irr
		}
	}
	if amount == "" || parseIRRBig(amount).Sign() <= 0 {
		a.navigatePage(w, r, "debtors", "مبلغ پرداخت باید بزرگ‌تر از صفر باشد.")
		return
	}
	if err := a.store.SettleDebtorPaymentWithNote(r.Context(), debtorID, amount, r.Form.Get("note")); err != nil {
		a.navigatePage(w, r, "debtors", err.Error())
		return
	}
	a.navigatePage(w, r, "debtors", "مبلغ پرداختی با موفقیت از مانده بدهی کسر گردید.")
}

// handleDebtorCreate ثبت حساب دفتری جدید (دکمه «ثبت بدهی دستی (جدید)»):
// یا با اقلام محصول انتخاب‌شده از فهرست محصولات، یا با مبلغ کل بدهی.
func (a *Application) handleDebtorCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	accountKind := strings.TrimSpace(r.Form.Get("account_kind"))
	if accountKind != "company" {
		accountKind = "personal"
	}
	name := strings.TrimSpace(r.Form.Get("full_name"))
	groupID := strings.TrimSpace(r.Form.Get("group_id"))
	if accountKind == "company" {
		if groupID == "" {
			a.navigatePage(w, r, "debtors", "برای حساب شرکتی، گروه یا شرکت را انتخاب کنید.")
			return
		}
		g, gErr := a.store.Entity(ctx, "customer_group", groupID)
		if gErr != nil {
			a.navigatePage(w, r, "debtors", "گروه انتخاب‌شده یافت نشد.")
			return
		}
		if name == "" {
			name = g.Label()
		}
	} else {
		groupID = ""
	}
	if len([]rune(name)) < 2 {
		a.navigatePage(w, r, "debtors", "نام مشتری باید حداقل ۲ نویسه باشد.")
		return
	}
	phone := normalizeDigits(strings.TrimSpace(r.Form.Get("phone")))
	if phone != "" && (len(phone) < 10 || len(phone) > 15) {
		a.navigatePage(w, r, "debtors", "شماره موبایل معتبر نیست.")
		return
	}
	if accountKind == "personal" && phone == "" {
		a.navigatePage(w, r, "debtors", "شماره موبایل مشتری الزامی است.")
		return
	}

	mode := strings.TrimSpace(r.Form.Get("mode"))
	if mode != "amount" {
		mode = "items"
	}
	total := big.NewInt(0)
	items := []map[string]any{}
	if mode == "amount" {
		irr, ok := tomanFormToIRR(r.Form.Get("total_toman"))
		if !ok {
			a.navigatePage(w, r, "debtors", "مبلغ کل بدهی را به تومان وارد کنید.")
			return
		}
		if _, ok := total.SetString(irr, 10); !ok {
			a.navigatePage(w, r, "debtors", "مبلغ کل بدهی معتبر نیست.")
			return
		}
	} else {
		titles := r.Form["item_title"]
		qtys := r.Form["item_qty"]
		prices := r.Form["item_price"]
		ids := r.Form["item_product_id"]
		for i, rawTitle := range titles {
			title := strings.TrimSpace(rawTitle)
			if title == "" {
				continue
			}
			qty := 1
			if i < len(qtys) {
				if n, qErr := strconv.Atoi(normalizeDigits(strings.TrimSpace(qtys[i]))); qErr == nil && n > 0 && n <= 9999 {
					qty = n
				}
			}
			unit := big.NewInt(0)
			if i < len(prices) {
				if irr, ok := tomanFormToIRR(prices[i]); ok {
					if _, ok := unit.SetString(irr, 10); !ok {
						unit = big.NewInt(0)
					}
				}
			}
			if unit.Sign() <= 0 {
				a.navigatePage(w, r, "debtors", "برای هر آیتم، قیمت واحد را وارد کنید.")
				return
			}
			lineTotal := new(big.Int).Mul(unit, big.NewInt(int64(qty)))
			productID := ""
			if i < len(ids) {
				productID = strings.TrimSpace(ids[i])
			}
			items = append(items, map[string]any{
				"product_id":     productID,
				"title":          title,
				"qty":            qty,
				"unit_price_irr": unit.String(),
				"line_total_irr": lineTotal.String(),
			})
			total.Add(total, lineTotal)
		}
		if len(items) == 0 {
			a.navigatePage(w, r, "debtors", "حداقل یک آیتم از فهرست محصولات اضافه کنید یا حالت «فقط مبلغ کل بدهی» را انتخاب کنید.")
			return
		}
	}
	if total.Sign() <= 0 {
		a.navigatePage(w, r, "debtors", "مبلغ بدهی باید بزرگ‌تر از صفر باشد.")
		return
	}

	dueISO := isoDateOrEmpty(r.Form.Get("due_at"))
	note := strings.TrimSpace(r.Form.Get("note"))
	fields := map[string]any{
		"full_name":      name,
		"phone":          phone,
		"account_type":   accountKind,
		"total":          total.String(),
		"paid":           "0",
		"remaining":      total.String(),
		"currency":       "IRR",
		"status":         "open",
		"source":         "manual",
		"entry_mode":     mode,
		"description":    note,
		"note":           note,
		"order_id":       "",
		"group_id":       groupID,
		"due_at":         dueISO,
		"items_count":    len(items),
	}
	if mode == "items" {
		fields["items"] = items
	}
	if _, sErr := a.store.SaveLocalRecord(ctx, "debtor", "", fields); sErr != nil {
		a.navigatePage(w, r, "debtors", "ثبت بدهی انجام نشد: "+sErr.Error())
		return
	}
	a.navigatePage(w, r, "debtors", "حساب بدهی «"+name+"» با مبلغ "+tomanFaWeb(total.String())+" ثبت شد.")
}
// handleDebtorsExport خروجی CSV فهرست بدهکاران (دکمه «خروجی CSV» نسخه وب).
// مبالغ به تومان و بدون جداکننده نوشته می‌شوند تا در اکسل عدد بمانند.
func (a *Application) handleDebtorsExport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	debtors, err := a.store.Entities(ctx, "debtor")
	if err != nil {
		http.Error(w, "cannot read debtors", http.StatusInternalServerError)
		return
	}
	groupLabels := map[string]string{}
	if groups, gErr := a.store.Entities(ctx, "customer_group"); gErr == nil {
		for _, g := range groups {
			groupLabels[g.ID] = g.Label()
		}
	}
	now := time.Now()
	sort.SliceStable(debtors, func(i, j int) bool { return debtors[i].Label() < debtors[j].Label() })
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=menuvex-debtors.csv")
	w.Header().Set("Cache-Control", "no-store")
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"نام طرف حساب", "موبایل", "گروه", "نوع حساب", "منبع ثبت", "مبلغ کل (تومان)", "پرداخت‌شده (تومان)", "مانده بدهی (تومان)", "سررسید", "وضعیت", "توضیحات"})
	for _, d := range debtors {
		remaining := parseIRRBig(d.Field("remaining"))
		_, dueLabel, overdue := debtorDueInfo(d.Field("due_at"), remaining, now)
		_, statusLabel, _ := debtorStatusOf(remaining, d.Field("status"), overdue)
		account := "مشتری شخصی"
		if strings.EqualFold(strings.TrimSpace(d.Field("account_type")), "company") {
			account = "حساب شرکتی / گروه"
		}
		source := "ثبت دستی"
		if strings.TrimSpace(d.Field("order_id")) != "" {
			source = "سفارش صندوق"
		}
		note := strings.TrimSpace(d.Field("note"))
		if note == "" {
			note = strings.TrimSpace(d.Field("description"))
		}
		totalToman := parseIRRBig(d.Field("total"))
		totalToman.Quo(totalToman, big.NewInt(10))
		paidToman := parseIRRBig(d.Field("paid"))
		paidToman.Quo(paidToman, big.NewInt(10))
		remToman := new(big.Int).Set(remaining)
		remToman.Quo(remToman, big.NewInt(10))
		_ = cw.Write([]string{
			d.Label(),
			store.ToPersianDigits(strings.TrimSpace(d.Field("phone"))),
			groupLabels[strings.TrimSpace(d.Field("group_id"))],
			account,
			source,
			totalToman.String(),
			paidToman.String(),
			remToman.String(),
			dueLabel,
			statusLabel,
			note,
		})
	}
	cw.Flush()
}

func (a *Application) handleTaskToggle(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimSpace(r.Form.Get("task_id"))
	ent, err := a.store.Entity(r.Context(), "task", taskID)
	if err != nil {
		a.navigatePage(w, r, "staff-checklist", err.Error())
		return
	}
	newDone := "true"
	newStatus := "done"
	if ent.Field("is_done") == "true" || ent.Field("status") == "done" {
		newDone = "false"
		newStatus = "pending"
	}
	_, _ = a.store.SaveLocalRecord(r.Context(), "task", taskID, map[string]any{
		"is_done": newDone,
		"status":  newStatus,
	})
	returnPage := strings.TrimSpace(r.Form.Get("return_page"))
	if returnPage == "" {
		returnPage = "staff-checklist"
	}
	a.navigatePage(w, r, returnPage, "وضعیت چک‌لیست روزانه به‌روزرسانی شد.")
}

func (a *Application) handleDayClose(w http.ResponseWriter, r *http.Request) {
	fa := a.store.Analytics(r.Context())
	_, err := a.store.SaveLocalRecord(r.Context(), "shift_close", "", map[string]any{
		"title":          "گزارش بستن صندوق روزانه",
		"total_sales":    fa.TotalSalesIRR,
		"cash_sales":     fa.CashSalesIRR,
		"card_sales":     fa.CardSalesIRR,
		"credit_sales":   fa.CreditSalesIRR,
		"total_expenses": fa.TotalExpensesIRR,
		"net_operating":  fa.NetOperatingIRR,
		"orders_count":   strconv.Itoa(fa.CompletedOrders),
		"cashier_note":   strings.TrimSpace(r.Form.Get("note")),
	})
	if err != nil {
		a.navigatePage(w, r, "finance", err.Error())
		return
	}
	a.navigatePage(w, r, "finance", "صندوق روزانه با موفقیت بسته شد.")
}

// handleProductsExport دانلود فهرست محصولات به‌صورت CSV (معادل دکمه «دانلود محصولات» وب).
func (a *Application) handleProductsExport(w http.ResponseWriter, r *http.Request) {
	products, err := a.store.Entities(r.Context(), "product")
	if err != nil {
		http.Error(w, "خطا در خواندن محصولات", http.StatusInternalServerError)
		return
	}
	cats, _ := a.store.Entities(r.Context(), "category")
	catLabel := map[string]string{}
	for _, c := range cats {
		catLabel[c.ID] = c.Label()
	}
	var buf bytes.Buffer
	buf.WriteString("\ufeff")
	cw := csv.NewWriter(&buf)
	_ = cw.Write([]string{"title", "category", "price_irr", "cost_irr", "stock", "is_available", "description"})
	for _, prod := range products {
		_ = cw.Write([]string{prod.Label(), catLabel[prod.Field("category_id")], prod.PriceIRR(), prod.Field("cost"), prod.Field("stock"), prod.Field("is_available"), prod.Field("description")})
	}
	cw.Flush()
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="products.csv"`)
	_, _ = w.Write(buf.Bytes())
}

// handleProductsImport افزودن گروهی محصولات از فایل CSV (ستون‌های خروجی دانلود محصولات).
func (a *Application) handleProductsImport(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		a.navigatePage(w, r, "products", "خطا در خواندن فایل گروهی: "+err.Error())
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		a.navigatePage(w, r, "products", "فایل CSV محصولات انتخاب نشده است.")
		return
	}
	defer file.Close()
	cats, _ := a.store.Entities(r.Context(), "category")
	catByLabel := map[string]string{}
	for _, c := range cats {
		catByLabel[c.Label()] = c.ID
		catByLabel[c.ID] = c.ID
	}
	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		a.navigatePage(w, r, "products", "فایل CSV معتبر نیست: "+err.Error())
		return
	}
	created, failed := 0, 0
	for i, rec := range records {
		if len(rec) == 0 {
			continue
		}
		title := strings.TrimSpace(strings.TrimPrefix(rec[0], "\ufeff"))
		if i == 0 && (strings.EqualFold(title, "title") || title == "نام محصول") {
			continue
		}
		if title == "" {
			failed++
			continue
		}
		get := func(idx int) string {
			if idx < len(rec) {
				return strings.TrimSpace(rec[idx])
			}
			return ""
		}
		fields := map[string]any{"title": title}
		if catID := catByLabel[get(1)]; catID != "" {
			fields["category_id"] = catID
		}
		if v := get(2); v != "" {
			fields["price"] = v
		}
		if v := get(3); v != "" {
			fields["cost"] = v
		}
		if v := get(4); v != "" {
			fields["stock"] = v
		}
		if v := get(5); v != "" {
			fields["is_available"] = v
		}
		if v := get(6); v != "" {
			fields["description"] = v
		}
		if _, sErr := a.store.SaveLocalRecord(r.Context(), "product", "", fields); sErr != nil {
			failed++
			continue
		}
		created++
	}
	a.navigatePage(w, r, "products", "افزودن گروهی انجام شد: "+store.FormatPersianNumber(strconv.Itoa(created))+" محصول ثبت و "+store.FormatPersianNumber(strconv.Itoa(failed))+" ردیف رد شد.")
}

// posStockJSON نقشهٔ موجودی قابل تهیه را برای دیالوگ «چه چیزی کم است؟» به
// JSON تبدیل می‌کند؛ فقط محصول‌هایی که کمبود یا دلیل ناموجودی دارند.
func posStockJSON(stock map[string]store.ProductPortion) string {
	out := map[string]store.ProductPortion{}
	for id, p := range stock {
		if len(p.Shortages) == 0 && strings.TrimSpace(p.Missing) == "" {
			continue
		}
		out[id] = p
	}
	if len(out) == 0 {
		return "{}"
	}
	blob, mErr := json.Marshal(out)
	if mErr != nil {
		return "{}"
	}
	return string(blob)
}

// handleProductsBulk ویرایش گروهی محصولات را از JSON ارسالی صفحهٔ محصولات
// اعمال می‌کند. فقط فیلدهایی که کاربر واقعاً تغییر داده در payload می‌آید،
// بنابراین محصولی که دست نخورده باشد بازنویسی نمی‌شود.
func (a *Application) handleProductsBulk(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.navigatePage(w, r, "products", "فرم نامعتبر است.")
		return
	}
	raw := strings.TrimSpace(r.Form.Get("payload"))
	if raw == "" {
		a.navigatePage(w, r, "products", "هیچ تغییری برای ذخیره انتخاب نشده است.")
		return
	}
	var items []struct {
		ID        string  `json:"id"`
		Price     *string `json:"price"`
		Stock     *string `json:"stock"`
		Available *bool   `json:"available"`
		Featured  *bool   `json:"featured"`
	}
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		a.navigatePage(w, r, "products", "دادهٔ ویرایش گروهی نامعتبر است.")
		return
	}
	patches := make([]store.ProductBulkPatch, 0, len(items))
	for _, it := range items {
		patch := store.ProductBulkPatch{ID: it.ID, Available: it.Available, Featured: it.Featured}
		if it.Price != nil {
			value := strings.TrimSpace(*it.Price)
			patch.Price = &value
		}
		if it.Stock != nil {
			value := strings.TrimSpace(*it.Stock)
			patch.Stock = &value
		}
		patches = append(patches, patch)
	}
	saved, err := a.store.BulkUpdateProducts(r.Context(), patches)
	if err != nil {
		a.navigatePage(w, r, "products", "ویرایش گروهی ناموفق بود: "+err.Error())
		return
	}
	msg := "ویرایش گروهی انجام شد: " + store.FormatPersianNumber(strconv.Itoa(saved)) + " محصول به‌روزرسانی شد."
	if saved == 0 {
		msg = "تغییری برای ذخیره پیدا نشد؛ حداقل یک فیلد را عوض کنید."
	}
	a.navigatePage(w, r, "products", msg)
}

// handleCategoryAvailability همهٔ محصولات یک دسته را یک‌جا موجود یا ناموجود
// می‌کند (درخواست کاربر: «یک دسته‌بندی و آیتم‌هایش را ناموجود کن»).
func (a *Application) handleCategoryAvailability(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.navigatePage(w, r, "categories", "فرم نامعتبر است.")
		return
	}
	categoryID := strings.TrimSpace(r.Form.Get("category_id"))
	available := strings.TrimSpace(r.Form.Get("available")) != "false"
	label := "این دسته"
	if cat, catErr := a.store.Entity(r.Context(), "category", categoryID); catErr == nil {
		label = cat.Label()
	}
	count, err := a.store.SetCategoryAvailability(r.Context(), categoryID, available)
	if err != nil {
		a.navigatePage(w, r, "categories", "تغییر وضعیت دسته ناموفق بود: "+err.Error())
		return
	}
	countFa := store.FormatPersianNumber(strconv.Itoa(count))
	var msg string
	switch {
	case !available && count > 0:
		msg = "دستهٔ «" + label + "» ناموجود شد؛ " + countFa + " محصول از منوی صندوق خارج شد."
	case !available:
		msg = "محصول‌های دستهٔ «" + label + "» از قبل ناموجود بودند."
	case count > 0:
		msg = "دستهٔ «" + label + "» موجود شد؛ " + countFa + " محصول دوباره قابل فروش است."
	default:
		msg = "محصول‌های دستهٔ «" + label + "» از قبل موجود بودند."
	}
	a.navigatePage(w, r, "categories", msg)
}

// handleCategoryDiscount تخفیف درصدی را روی همهٔ محصولات یک دسته اعمال
// می‌کند؛ درصد صفر تخفیف را برمی‌دارد و قیمت اصلی را برمی‌گرداند.
func (a *Application) handleCategoryDiscount(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.navigatePage(w, r, "categories", "فرم نامعتبر است.")
		return
	}
	categoryID := strings.TrimSpace(r.Form.Get("category_id"))
	percent := 0
	rawPercent := normalizeNumInput(r.Form.Get("percent"))
	rawPercent = strings.NewReplacer(",", "", "٬", "", " ", "").Replace(rawPercent)
	if strings.TrimSpace(rawPercent) != "" {
		value, convErr := strconv.Atoi(rawPercent)
		if convErr != nil || value < 0 || value > 90 {
			a.navigatePage(w, r, "categories", "درصد تخفیف باید عددی بین ۰ تا ۹۰ باشد.")
			return
		}
		percent = value
	}
	label := "این دسته"
	if cat, catErr := a.store.Entity(r.Context(), "category", categoryID); catErr == nil {
		label = cat.Label()
	}
	count, err := a.store.ApplyCategoryDiscount(r.Context(), categoryID, percent)
	if err != nil {
		a.navigatePage(w, r, "categories", "اعمال تخفیف ناموفق بود: "+err.Error())
		return
	}
	countFa := store.FormatPersianNumber(strconv.Itoa(count))
	var msg string
	switch {
	case count == 0:
		msg = "هیچ محصول قیمت‌داری در دستهٔ «" + label + "» پیدا نشد."
	case percent == 0:
		msg = "تخفیف دستهٔ «" + label + "» برداشته شد؛ قیمت " + countFa + " محصول به حالت اصلی برگشت."
	default:
		msg = "تخفیف " + store.FormatPersianNumber(strconv.Itoa(percent)) + "٪ روی " + countFa + " محصول دستهٔ «" + label + "» اعمال شد."
	}
	a.navigatePage(w, r, "categories", msg)
}

// saveUploadedImage تصویر آپلودشده کاربر را با نام تصادفی امن در پوشه داده محلی ذخیره می‌کند.
func (a *Application) saveUploadedImage(fh *multipart.FileHeader) (string, error) {
	if fh.Size > 8<<20 {
		return "", errors.New("حجم تصویر نباید بیشتر از ۸ مگابایت باشد")
	}
	src, err := fh.Open()
	if err != nil {
		return "", err
	}
	defer src.Close()
	head := make([]byte, 512)
	n, _ := src.Read(head)
	kind := http.DetectContentType(head[:n])
	ext := ""
	switch kind {
	case "image/png":
		ext = ".png"
	case "image/jpeg":
		ext = ".jpg"
	case "image/webp":
		ext = ".webp"
	case "image/gif":
		ext = ".gif"
	default:
		return "", errors.New("فقط تصویر PNG ،JPG ،WEBP یا GIF مجاز است")
	}
	if _, err := src.Seek(0, 0); err != nil {
		return "", err
	}
	if err := os.MkdirAll(a.uploadDir, 0o755); err != nil {
		return "", err
	}
	rb := make([]byte, 16)
	if _, err := rand.Read(rb); err != nil {
		return "", err
	}
	name := hex.EncodeToString(rb) + ext
	dst, err := os.OpenFile(filepath.Join(a.uploadDir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	defer dst.Close()
	if _, err := io.Copy(dst, src); err != nil {
		return "", err
	}
	return "/uploads/" + name, nil
}

// handleUploadServe فقط فایل‌های داخل پوشه آپلود محلی را سرو می‌کند (بدون دسترسی به مسیر دلخواه).
func (a *Application) handleUploadServe(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(strings.TrimPrefix(r.URL.Path, "/uploads/"))
	if name == "." || name == "/" || !regexp.MustCompile(`^[a-f0-9]{32}\.(png|jpg|webp|gif)$`).MatchString(name) {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(filepath.Join(a.uploadDir, name))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeContent(w, r, name, st.ModTime(), f)
}

// handleInventoryExport دانلود لیست موجودی انبار به‌صورت CSV (معادل خروجی CSV وب).
func (a *Application) handleInventoryExport(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.Entities(r.Context(), "ingredient")
	if err != nil {
		http.Error(w, "خطا در خواندن انبار", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	buf.WriteString("\ufeff")
	cw := csv.NewWriter(&buf)
	_ = cw.Write([]string{"name", "sku", "category", "location", "unit", "current_stock", "reorder_point", "average_cost_irr", "last_purchase_cost_irr"})
	for _, ing := range items {
		_ = cw.Write([]string{ing.Label(), ing.Field("sku"), ing.Field("category"), ing.Field("storage_location"), ing.Field("base_unit"), ing.Field("current_stock"), ing.Field("reorder_point"), ing.Field("average_cost"), ing.Field("last_purchase_cost")})
	}
	cw.Flush()
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="inventory.csv"`)
	_, _ = w.Write(buf.Bytes())
}

func (a *Application) downloadBackup(w http.ResponseWriter, r *http.Request) {
	raw, err := a.store.ExportBackup(r.Context())
	if err != nil {
		http.Error(w, "cannot export backup", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="coffish-desktop-backup.json"`)
	w.Write(raw)
}

func (a *Application) handleExportBackup(w http.ResponseWriter, r *http.Request) {
	raw, err := a.store.ExportBackup(r.Context())
	if err != nil {
		a.navigatePage(w, r, "home", "خطا در تولید فایل پشتیبان: "+err.Error())
		return
	}
	targetDir := "."
	if homeDir, hErr := os.UserHomeDir(); hErr == nil && homeDir != "" {
		dl := filepath.Join(homeDir, "Downloads")
		if info, sErr := os.Stat(dl); sErr == nil && info.IsDir() {
			targetDir = dl
		} else {
			targetDir = homeDir
		}
	}
	fileName := "coffish-backup-" + time.Now().Format("20060102-150405") + ".json"
	fullPath := filepath.Join(targetDir, fileName)
	if err := os.WriteFile(fullPath, raw, 0o600); err != nil {
		a.navigatePage(w, r, "home", "خطا در ذخیره فایل پشتیبان: "+err.Error())
		return
	}
	a.navigatePage(w, r, "home", "فایل پشتیبان با موفقیت در مسیر "+fullPath+" ذخیره شد.")
}
