//go:build !production

// This file is deliberately excluded from production/release builds. It exists
// only as a rich local fixture for desktop/store/sync tests.
package store

import (
	"context"
	"encoding/json"
	"time"
)

// SeedIranianCafeData populates a rich fixture dataset for tests and local development.
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
