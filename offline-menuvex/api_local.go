package desktop

import (
	"encoding/json"
	"math/big"
	"net/http"
	"strconv"
	"strings"

	"github.com/sinaqaffari/coffish/desktop/internal/store"
)

func writeJSONEnvelope(w http.ResponseWriter, status int, success bool, message string, data any, errObj any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": success,
		"message": message,
		"data":    data,
		"error":   errObj,
	})
}

func (a *Application) registerLocalAPIRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/license", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(a.LicenseSummary(r.Context()))
	})
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSONEnvelope(w, http.StatusOK, true, "سرویس دسکتاپ منووکس فعال و آماده است", map[string]any{
			"name":    "MenuVex Desktop Local Engine",
			"version": "1.0.0-ir",
			"mode":    "standalone_offline_desktop",
			"status":  "ok",
		}, nil)
	})
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		st := a.store.Stats(r.Context())
		writeJSONEnvelope(w, http.StatusOK, true, "پایگاه داده محلی دسکتاپ آماده بهره‌برداری است", map[string]any{
			"status":   "ready",
			"products": st.ProductsCount,
			"orders":   st.OrdersCount,
		}, nil)
	})

	domainMap := map[string]string{
		"/api/v1/categories":                      "category",
		"/api/v1/products":                        "product",
		"/api/v1/modifier-groups":                 "modifier_group",
		"/api/v1/inventory/items":                 "ingredient",
		"/api/v1/inventory/movements":             "inventory_movement",
		"/api/v1/inventory/recipes":               "recipe",
		"/api/v1/inventory/waste":                 "waste_event",
		"/api/v1/inventory/suppliers":             "supplier",
		"/api/v1/inventory/purchases":             "purchase",
		"/api/v1/orders":                          "order",
		"/api/v1/tables":                          "table",
		"/api/v1/tables/reservations":             "reservation",
		"/api/v1/customers":                       "customer",
		"/api/v1/retention/campaigns":             "retention_campaign",
		"/api/online/cafe-plans":                  "cafe_plan",
		"/api/v1/debtors":                         "debtor",
		"/api/v1/expenses":                        "expense",
		"/api/v1/accounting/entries":              "accounting_entry",
		"/api/v1/staff":                           "staff",
		"/api/v1/tasks":                           "task",
		"/api/v1/jobs":                            "job_posting",
		"/api/v1/banners":                         "banner",
		"/api/v1/printers":                        "printer",
		"/api/v1/print-jobs":                      "print_job",
		"/api/printer-configs":                    "printer",
		"/api/printer-configs/category-routes":    "printer_route",
		"/api/v1/printer-configs/category-routes": "printer_route",
		"/api/v1/cafe/profile":                    "cafe_profile",
		"/api/cafe/profile":                       "cafe_profile",
	}

	for pathPrefix, kind := range domainMap {
		p := pathPrefix
		k := kind
		mux.HandleFunc("GET "+p, func(w http.ResponseWriter, r *http.Request) {
			items, err := a.store.Entities(r.Context(), k)
			if err != nil {
				writeJSONEnvelope(w, http.StatusInternalServerError, false, err.Error(), nil, map[string]string{"code": "STORE_ERROR"})
				return
			}
			decoded := make([]json.RawMessage, 0, len(items))
			for _, it := range items {
				decoded = append(decoded, it.Payload)
			}
			writeJSONEnvelope(w, http.StatusOK, true, "اطلاعات با موفقیت از پایگاه داده محلی دریافت شد", decoded, nil)
		})
	}

	mux.HandleFunc("GET /api/v1/reports/summary", func(w http.ResponseWriter, r *http.Request) {
		fa := a.store.Analytics(r.Context())
		st := a.store.Stats(r.Context())
		writeJSONEnvelope(w, http.StatusOK, true, "گزارش جامع مالی و عملیاتی کافه", map[string]any{
			"analytics": fa,
			"stats":     st,
		}, nil)
	})

	mux.HandleFunc("GET /api/v1/deep-analytics/overview", func(w http.ResponseWriter, r *http.Request) {
		fa := a.store.Analytics(r.Context())
		st := a.store.Stats(r.Context())
		writeJSONEnvelope(w, http.StatusOK, true, "تحلیل عمیق سودآوری و مهندسی منو (Deep Analytics)", map[string]any{
			"analytics":  fa,
			"stats":      st,
			"peak_hours": []string{"۰۹:۰۰ تا ۱۱:۳۰", "۱۸:۰۰ تا ۲۱:۳۰"},
		}, nil)
	})

	mux.HandleFunc("GET /api/v1/pos", func(w http.ResponseWriter, r *http.Request) {
		products, _ := a.store.Entities(r.Context(), "product")
		categories, _ := a.store.Entities(r.Context(), "category")
		tables, _ := a.store.Entities(r.Context(), "table")
		orders, _ := a.store.Entities(r.Context(), "order")
		writeJSONEnvelope(w, http.StatusOK, true, "اطلاعات صندوق فروش آفلاین", map[string]any{
			"categories": categories,
			"products":   products,
			"tables":     tables,
			"orders":     orders,
		}, nil)
	})

	mux.HandleFunc("POST /api/v1/pos/checkout", func(w http.ResponseWriter, r *http.Request) {
		var req store.POSOrderInput
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONEnvelope(w, http.StatusBadRequest, false, "فرمت درخواست نامعتبر است", nil, nil)
			return
		}
		id, err := a.store.CreatePOSOrder(r.Context(), req)
		if err != nil {
			writeJSONEnvelope(w, http.StatusBadRequest, false, err.Error(), nil, nil)
			return
		}
		ent, _ := a.store.Entity(r.Context(), "order", id)
		writeJSONEnvelope(w, http.StatusCreated, true, "سفارش با موفقیت در صندوق ثبت شد", ent.Payload, nil)
	})

	mux.HandleFunc("POST /api/v1/split-bill/calculate", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			TotalIRR string `json:"total_irr"`
			People   int    `json:"people"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.People <= 0 {
			req.People = 2
		}
		total := new(big.Int)
		total.SetString(strings.TrimSpace(req.TotalIRR), 10)
		share := new(big.Int).Div(total, big.NewInt(int64(req.People)))
		writeJSONEnvelope(w, http.StatusOK, true, "محاسبه دنگ هر نفر انجام شد", map[string]any{
			"people":          req.People,
			"share_irr":       share.String(),
			"share_formatted": store.FormatIRRToToman(share.String()),
		}, nil)
	})

	mux.HandleFunc("POST /api/v1/orders/day-close", func(w http.ResponseWriter, r *http.Request) {
		fa := a.store.Analytics(r.Context())
		id, _ := a.store.SaveLocalRecord(r.Context(), "shift_close", "", map[string]any{
			"title":          "گزارش بستن صندوق پایان شب",
			"total_sales":    fa.TotalSalesIRR,
			"cash_sales":     fa.CashSalesIRR,
			"card_sales":     fa.CardSalesIRR,
			"credit_sales":   fa.CreditSalesIRR,
			"total_expenses": fa.TotalExpensesIRR,
			"net_operating":  fa.NetOperatingIRR,
			"orders_count":   strconv.Itoa(fa.CompletedOrders),
		})
		writeJSONEnvelope(w, http.StatusOK, true, "صندوق روزانه با موفقیت بسته شد", map[string]any{
			"id":        id,
			"analytics": fa,
		}, nil)
	})

	// Generic local JSON create/update for standalone API callers
	mux.HandleFunc("POST /api/v1/local/", func(w http.ResponseWriter, r *http.Request) {
		kind := strings.TrimPrefix(r.URL.Path, "/api/v1/local/")
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONEnvelope(w, http.StatusBadRequest, false, "ورودی JSON نامعتبر است", nil, nil)
			return
		}
		id, _ := body["id"].(string)
		savedID, err := a.store.SaveLocalRecord(r.Context(), kind, id, body)
		if err != nil {
			writeJSONEnvelope(w, http.StatusBadRequest, false, err.Error(), nil, nil)
			return
		}
		ent, _ := a.store.Entity(r.Context(), kind, savedID)
		writeJSONEnvelope(w, http.StatusOK, true, "رکورد با موفقیت در پایگاه داده محلی ذخیره شد", ent.Payload, nil)
	})
}
