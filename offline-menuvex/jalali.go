package desktop

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sinaqaffari/coffish/desktop/internal/store"
)

var jalaliMonthNames = [...]string{
	"",
	"فروردین",
	"اردیبهشت",
	"خرداد",
	"تیر",
	"مرداد",
	"شهریور",
	"مهر",
	"آبان",
	"آذر",
	"دی",
	"بهمن",
	"اسفند",
}

var jalaliWeekdayNames = [...]string{
	"یکشنبه",
	"دوشنبه",
	"سه‌شنبه",
	"چهارشنبه",
	"پنج‌شنبه",
	"جمعه",
	"شنبه",
}

// GregorianToJalali converts a Gregorian date (year, month, day) to a Solar Hijri (Jalali) date.
func GregorianToJalali(gy, gm, gd int) (int, int, int) {
	if gm < 1 || gm > 12 {
		return gy, gm, gd
	}
	gDaysInMonth := [...]int{0, 31, 59, 90, 120, 151, 181, 212, 243, 273, 304, 334}
	gy2 := gy
	if gm > 2 {
		gy2 = gy + 1
	}
	days := 355666 + (365 * gy) + ((gy2 + 3) / 4) - ((gy2 + 99) / 100) + ((gy2 + 399) / 400) + gd + gDaysInMonth[gm-1]
	jy := -1595 + (33 * (days / 12053))
	days %= 12053
	jy += 4 * (days / 1461)
	days %= 1461
	if days > 365 {
		jy += (days - 1) / 365
		days = (days - 1) % 365
	}
	var jm, jd int
	if days < 186 {
		jm = 1 + (days / 31)
		jd = 1 + (days % 31)
	} else {
		jm = 7 + ((days - 186) / 30)
		jd = 1 + ((days - 186) % 30)
	}
	return jy, jm, jd
}

// JalaliToGregorian converts a Solar Hijri (Jalali) date to Gregorian, the exact
// inverse of GregorianToJalali. It is used by the Shamsi date picker: the browser
// sends a Jalali day and the server stores/compares the Gregorian equivalent.
func JalaliToGregorian(jy, jm, jd int) (int, int, int) {
	jy2 := jy + 1595
	days := -355668 + (365 * jy2) + ((jy2 / 33) * 8) + (((jy2 % 33) + 3) / 4) + jd
	if jm < 7 {
		days += (jm - 1) * 31
	} else {
		days += ((jm - 7) * 30) + 186
	}
	gy := 400 * (days / 146097)
	days %= 146097
	if days > 36524 {
		days--
		gy += 100 * (days / 36524)
		days %= 36524
		if days >= 365 {
			days++
		}
	}
	gy += 4 * (days / 1461)
	days %= 1461
	if days > 365 {
		gy += (days - 1) / 365
		days = (days - 1) % 365
	}
	gd := days + 1
	months := [...]int{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	if (gy%4 == 0 && gy%100 != 0) || gy%400 == 0 {
		months[1] = 29
	}
	gm := 0
	for gm < 12 && gd > months[gm] {
		gd -= months[gm]
		gm++
	}
	return gy, gm + 1, gd
}

// jalaliDigitValue maps Persian/Arabic-Indic digits to their Latin value.
func jalaliDigitValue(r rune) (rune, bool) {
	switch {
	case r >= '۰' && r <= '۹':
		return '0' + (r - '۰'), true
	case r >= '٠' && r <= '٩':
		return '0' + (r - '٠'), true
	case r >= '0' && r <= '9':
		return r, true
	}
	return 0, false
}

// ParseJalali parses a Shamsi date written as ۱۴۰۵/۰۷/۲۶ (Persian or Latin
// digits, "/" or "-") and returns the equivalent Gregorian date in Tehran.
func ParseJalali(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	numbers := []int{}
	current := ""
	flush := func() {
		if current == "" {
			return
		}
		if n, err := strconv.Atoi(current); err == nil {
			numbers = append(numbers, n)
		}
		current = ""
	}
	for _, r := range raw {
		if d, ok := jalaliDigitValue(r); ok {
			current += string(d)
			continue
		}
		flush()
	}
	flush()
	if len(numbers) != 3 {
		return time.Time{}, false
	}
	jy, jm, jd := numbers[0], numbers[1], numbers[2]
	if jy < 1000 || jy > 1600 || jm < 1 || jm > 12 || jd < 1 || jd > 31 {
		return time.Time{}, false
	}
	gy, gm, gd := JalaliToGregorian(jy, jm, jd)
	loc := tehranZone()
	t := time.Date(gy, time.Month(gm), gd, 0, 0, 0, 0, loc)
	// Reject impossible days such as ۱۴۰۵/۱۲/۳۱ in a non-leap year.
	if by, bm, bd := GregorianToJalali(t.Year(), int(t.Month()), t.Day()); by != jy || bm != jm || bd != jd {
		return time.Time{}, false
	}
	return t, true
}

// JalaliISOFromShamsi converts a Shamsi input to the ISO date the store keeps.
func JalaliISOFromShamsi(raw string) string {
	t, ok := ParseJalali(raw)
	if !ok {
		return ""
	}
	return t.Format("2006-01-02")
}

// ShamsiFromISO renders a stored ISO date/timestamp as ۱۴۰۵/۰۷/۲۶ for the UI.
func ShamsiFromISO(iso string) string {
	return store.ToPersianDigits(formatJalaliNumber(iso))
}

// formatJalaliNumber renders an ISO date/timestamp as Latin-digit 1405/07/26.
// The Shamsi date picker and its hidden ISO value share this single conversion.
func formatJalaliNumber(iso string) string {
	iso = strings.TrimSpace(iso)
	if iso == "" {
		return ""
	}
	if t, ok := JalaliTimeOf(iso); ok {
		jy, jm, jd := GregorianToJalali(t.Year(), int(t.Month()), t.Day())
		return fmt.Sprintf("%04d/%02d/%02d", jy, jm, jd)
	}
	return iso
}

// JalaliTimeOf returns the Tehran-local time for a stored ISO timestamp.
func JalaliTimeOf(iso string) (time.Time, bool) {
	iso = strings.TrimSpace(iso)
	if iso == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, iso); err == nil {
			return t.In(tehranZone()), true
		}
	}
	return time.Time{}, false
}

func tehranZone() *time.Location {
	return time.FixedZone("Asia/Tehran", 3*3600+30*60)
}

// FormatJalaliFull formats a time.Time in Asia/Tehran as "یکشنبه ۲۶ مهر ۱۴۰۵ — ساعت ۱۴:۳۰".
func FormatJalaliFull(t time.Time) string {
	loc := t.In(tehranZone())
	jy, jm, jd := GregorianToJalali(loc.Year(), int(loc.Month()), loc.Day())
	wd := jalaliWeekdayNames[int(loc.Weekday())%7]
	monthName := ""
	if jm >= 1 && jm <= 12 {
		monthName = jalaliMonthNames[jm]
	}
	raw := fmt.Sprintf("%s %d %s %d — ساعت %02d:%02d", wd, jd, monthName, jy, loc.Hour(), loc.Minute())
	return store.ToPersianDigits(raw)
}

// FormatJalaliClock ساعت روز را به وقت تهران با ارقام فارسی برمی‌گرداند («۱۴:۳۰»).
func FormatJalaliClock(t time.Time) string {
	loc := t.In(tehranZone())
	return store.ToPersianDigits(fmt.Sprintf("%02d:%02d", loc.Hour(), loc.Minute()))
}

// FormatJalaliDate تاریخ را به‌صورت «۱۴۰۵/۰۷/۲۶» برمی‌گرداند (YYYY/MM/DD جلالی).
func FormatJalaliDate(iso string) string {
	iso = strings.TrimSpace(iso)
	if iso == "" {
		return "—"
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, iso); err == nil {
			loc := t.In(tehranZone())
			jy, jm, jd := GregorianToJalali(loc.Year(), int(loc.Month()), loc.Day())
			return store.ToPersianDigits(fmt.Sprintf("%04d/%02d/%02d", jy, jm, jd))
		}
	}
	return store.ToPersianDigits(iso)
}

// FormatJalaliShort formats an ISO/RFC3339 timestamp as "۱۴۰۵/۰۷/۲۶ - ۱۴:۳۰".
func FormatJalaliShort(iso string) string {
	iso = strings.TrimSpace(iso)
	if iso == "" {
		return "—"
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, iso); err == nil {
			loc := t.In(tehranZone())
			jy, jm, jd := GregorianToJalali(loc.Year(), int(loc.Month()), loc.Day())
			return store.ToPersianDigits(fmt.Sprintf("%04d/%02d/%02d — %02d:%02d", jy, jm, jd, loc.Hour(), loc.Minute()))
		}
	}
	return store.ToPersianDigits(iso)
}
