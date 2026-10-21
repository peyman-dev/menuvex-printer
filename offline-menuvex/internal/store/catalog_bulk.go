package store

import (
	"context"
	"errors"
	"math/big"
	"strconv"
	"strings"
)

// این فایل عملیات گروهی کاتالوگ را در خود دارد: ویرایش گروهی محصولات،
// ناموجود/موجود کردن همهٔ محصولات یک دسته و اعمال تخفیف درصدی روی یک دسته.
// همهٔ تغییرها از مسیر SaveLocalRecord رد می‌شوند تا نسخهٔ هر رکورد و
// زمان به‌روزرسانی درست ثبت شود و صف همگام‌سازی هم دست‌نخورده بماند.

// ProductBulkPatch تغییرهای یک محصول در ویرایش گروهی است؛ هر فیلد nil یعنی
// «این مقدار را تغییر نده» تا فقط فیلدهای واقعاً ویرایش‌شده ذخیره شوند.
type ProductBulkPatch struct {
	ID        string
	Price     *string
	Stock     *string
	Available *bool
	Featured  *bool
}

func normalizeBulkNumber(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", false
	}
	replacer := strings.NewReplacer(
		"۰", "0", "۱", "1", "۲", "2", "۳", "3", "۴", "4",
		"۵", "5", "۶", "6", "۷", "7", "۸", "8", "۹", "9",
		"٠", "0", "١", "1", "٢", "2", "٣", "3", "٤", "4",
		"٥", "5", "٦", "6", "٧", "7", "٨", "8", "٩", "9",
		"٫", ".",
		",", "", "٬", "", " ", "", "\u200c", "", "\u200f", "", "\u200e", "",
	)
	s = replacer.Replace(s)
	if strings.HasPrefix(s, "+") {
		s = s[1:]
	}
	return s, true
}

func parseBulkInt(raw string) (*big.Int, bool) {
	normalized, ok := normalizeBulkNumber(raw)
	if !ok {
		return nil, false
	}
	intPart := normalized
	if dot := strings.Index(intPart, "."); dot >= 0 {
		intPart = intPart[:dot]
	}
	if intPart == "" || intPart == "-" {
		return nil, false
	}
	bi, ok := new(big.Int).SetString(intPart, 10)
	if !ok {
		return nil, false
	}
	return bi, true
}

// BulkUpdateProducts فیلدهای تغییرکردهٔ چند محصول را یک‌جا ذخیره می‌کند.
// ابتدا همهٔ مقادیر اعتبارسنجی می‌شوند؛ اگر یکی نامعتبر باشد هیچ تغییری
// ثبت نمی‌شود (نه ذخیرهٔ نیمه‌کاره).
func (s *Store) BulkUpdateProducts(ctx context.Context, patches []ProductBulkPatch) (int, error) {
	if len(patches) == 0 {
		return 0, errors.New("هیچ محصولی برای ویرایش انتخاب نشده است")
	}
	if len(patches) > 300 {
		return 0, errors.New("در هر نوبت حداکثر ۳۰۰ محصول را می‌توان گروهی ویرایش کرد")
	}
	fieldsByID := make([]map[string]any, 0, len(patches))
	ids := make([]string, 0, len(patches))
	seen := map[string]bool{}
	for _, p := range patches {
		id := strings.TrimSpace(p.ID)
		if id == "" {
			return 0, errors.New("شناسهٔ محصول نامعتبر است")
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		if _, err := s.Entity(ctx, "product", id); err != nil {
			return 0, errors.New("یکی از محصول‌های انتخاب‌شده پیدا نشد؛ صفحه را بازخوانی کنید")
		}
		fields := map[string]any{}
		if p.Price != nil {
			price, ok := parseBulkInt(*p.Price)
			if !ok || price.Sign() < 0 {
				return 0, errors.New("قیمت باید یک عدد نامنفی باشد")
			}
			// ویرایش دستی قیمت، تخفیف دسته‌بندی را باطل می‌کند؛ وگرنه قیمت
			// پایهٔ قبلی در کارت محصول می‌ماند و عدد اشتباهی دیده می‌شود.
			fields["price"] = price.String()
			fields["base_price"] = ""
			fields["discount_percent"] = ""
		}
		if p.Stock != nil {
			raw, ok := normalizeBulkNumber(*p.Stock)
			if !ok {
				return 0, errors.New("موجودی باید یک عدد باشد")
			}
			qty, err := strconv.ParseFloat(raw, 64)
			if err != nil || qty < 0 {
				return 0, errors.New("موجودی باید یک عدد نامنفی باشد")
			}
			fields["stock"] = strconv.FormatFloat(qty, 'f', -1, 64)
		}
		if p.Available != nil {
			fields["is_available"] = *p.Available
		}
		if p.Featured != nil {
			fields["is_featured"] = *p.Featured
		}
		if len(fields) == 0 {
			continue
		}
		fieldsByID = append(fieldsByID, fields)
		ids = append(ids, id)
	}
	saved := 0
	for i, fields := range fieldsByID {
		if _, err := s.SaveLocalRecord(ctx, "product", ids[i], fields); err != nil {
			return saved, err
		}
		saved++
	}
	return saved, nil
}

// categoryProducts شناسه و رکورد محصول‌های یک دسته را برمی‌گرداند.
func (s *Store) categoryProducts(ctx context.Context, categoryID string) ([]Entity, error) {
	categoryID = strings.TrimSpace(categoryID)
	if categoryID == "" {
		return nil, errors.New("دسته‌بندی انتخاب نشده است")
	}
	if _, err := s.Entity(ctx, "category", categoryID); err != nil {
		return nil, errors.New("دسته‌بندی مورد نظر پیدا نشد")
	}
	products, err := s.Entities(ctx, "product")
	if err != nil {
		return nil, err
	}
	out := make([]Entity, 0, len(products))
	for _, p := range products {
		if p.Field("category_id") == categoryID {
			out = append(out, p)
		}
	}
	return out, nil
}

// SetCategoryAvailability همهٔ محصولات یک دسته را موجود یا ناموجود می‌کند و
// تعداد محصول‌های تغییرکرده را برمی‌گرداند.
func (s *Store) SetCategoryAvailability(ctx context.Context, categoryID string, available bool) (int, error) {
	products, err := s.categoryProducts(ctx, categoryID)
	if err != nil {
		return 0, err
	}
	changed := 0
	for _, p := range products {
		current := p.Field("is_available") != "false"
		if current == available {
			continue
		}
		if _, err := s.SaveLocalRecord(ctx, "product", p.ID, map[string]any{"is_available": available}); err != nil {
			return changed, err
		}
		changed++
	}
	return changed, nil
}

// ApplyCategoryDiscount درصد تخفیف را روی قیمت فروش همهٔ محصولات یک دسته
// اعمال می‌کند. قیمت پایهٔ هر محصول در base_price نگه داشته می‌شود تا تخفیف
// روی تخفیف قبلی سوار نشود و درصد صفر قیمت اصلی را برگرداند.
func (s *Store) ApplyCategoryDiscount(ctx context.Context, categoryID string, percent int) (int, error) {
	if percent < 0 || percent > 90 {
		return 0, errors.New("درصد تخفیف باید بین ۰ تا ۹۰ باشد")
	}
	products, err := s.categoryProducts(ctx, categoryID)
	if err != nil {
		return 0, err
	}
	changed := 0
	hundred := big.NewInt(100)
	for _, p := range products {
		baseRaw := p.Field("base_price")
		if strings.TrimSpace(baseRaw) == "" {
			baseRaw = p.Field("price")
		}
		base, ok := parseBulkInt(baseRaw)
		if !ok || base.Sign() <= 0 {
			continue
		}
		next := new(big.Int).Set(base)
		if percent > 0 {
			amount := new(big.Int).Mul(base, big.NewInt(int64(100-percent)))
			rem := new(big.Int)
			next.QuoRem(amount, hundred, rem)
			// گرد کردن به نزدیک‌ترین ریال (نیم به بالا).
			if rem.Cmp(big.NewInt(50)) >= 0 {
				next.Add(next, big.NewInt(1))
			}
		}
		fields := map[string]any{
			"price":      next.String(),
			"base_price": base.String(),
		}
		if percent == 0 {
			fields["discount_percent"] = ""
		} else {
			fields["discount_percent"] = strconv.Itoa(percent)
		}
		if _, err := s.SaveLocalRecord(ctx, "product", p.ID, fields); err != nil {
			return changed, err
		}
		changed++
	}
	return changed, nil
}

// CategoryDiscountPercent درصد تخفیف فعال یک دسته را برمی‌گرداند. فقط وقتی
// عدد برمی‌گردد که همهٔ محصول‌های قیمت‌دار دسته همین درصد را داشته باشند؛
// وگرنه صفر یعنی «تخفیف یکدست نیست».
func CategoryDiscountPercent(products []Entity, categoryID string) int {
	percent := 0
	eligible := 0
	for _, p := range products {
		if p.Field("category_id") != categoryID {
			continue
		}
		price, ok := parseBulkInt(p.Field("price"))
		if !ok || price.Sign() <= 0 {
			continue
		}
		raw := strings.TrimSpace(p.Field("discount_percent"))
		if raw == "" {
			return 0
		}
		value, err := strconv.Atoi(raw)
		if err != nil || value <= 0 {
			return 0
		}
		if eligible > 0 && value != percent {
			return 0
		}
		percent = value
		eligible++
	}
	if eligible == 0 {
		return 0
	}
	return percent
}

// ProductsAvailableCount تعداد محصول‌های موجود (قابل فروش) یک دسته را
// می‌شمارد تا دکمهٔ «موجود کردن» یا «ناموجود کردن» درست انتخاب شود.
func ProductsAvailableCount(products []Entity, categoryID string) (available, total int) {
	for _, p := range products {
		if p.Field("category_id") != categoryID {
			continue
		}
		total++
		if p.Field("is_available") != "false" {
			available++
		}
	}
	return available, total
}
