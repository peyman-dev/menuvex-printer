package desktop

import (
	"fmt"
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
