# کافیش دسکتاپ (Coffish Desktop — نسخه مستقل آفلاین برای ویندوز، مک و لینوکس)

`desktop/` یک برنامه دسکتاپ کامل، مستقل و آفلاین‌محور (**Standalone Production Offline Desktop App**) با معماری **Go + HTMX + قالب‌های بومی استاندارد ایرانی (RTL)** برای مدیریت صفر تا صد کافه و رستوران است.

## ویژگی‌های کلیدی نسخه دسکتاپ

1. **کاملاً مستقل و بدون نیاز به اینترنت یا سرور خارجی (Standalone Offline App)**:
   - موتور ذخیره‌سازی تراکنشی (ACID) در گولنگ خالص (`CGO_ENABLED=0`) بدون نیاز به کامپایلر C یا کتابخانه‌های خارجی.
   - ذخیره اتمیک تمامی اطلاعات روی دیسک محلی (`offline.sqlite` / JSON-ACID) با مجوز امنیتی `0600`.
2. **فایل اجرایی کراس‌پلتفرم برای تمامی سیستم‌عامل‌ها (`Windows .exe`، `macOS .app`، `Linux`)**:
   - کامپایل مستقیم به فایل `.exe` ویندوز (`amd64` و `arm64`)، بسته `.app` و باینری مک (`Apple Silicon` و `Intel`) و باینری لینوکس (`amd64` و `arm64`) از روی هر سیستم‌عاملی.
3. **پوشش کامل ماژول‌های عملیاتی کافه و رستوران (هم‌تراز با بک‌اند FastAPI)**:
   - **صندوق فروش و صدور فیش (POS — `/?page=pos`)**: ثبت سریع سفارش سالن، بیرون‌بر و پیک، محاسبه دقیق ریال و تومان، تخفیف، تسویه کارتخوان/نقدی/حساب دفتری و صدور فوری فیش.
   - **چاپ فیش حرارتی ۸۰ میلی‌متری استاندارد ایران (`/?page=receipt`)**: پیش‌نمایش و چاپ همزمان **فیش مشتری** و **حواله آماده‌سازی بار (فیش باریستا)** با تقویم هجری شمسی.
   - **مدیریت سفارش‌ها و مانیتور بار (`/?page=orders`)**: تغییر وضعیت سفارش و **کسر خودکار مواد اولیه از انبار بر اساس فرمول ساخت (BOM)** به محض تکمیل سفارش.
   - **منو، دسته‌بندی‌ها، محصولات و بنرها (`/?page=products`, `/?page=categories`, `/?page=banners`)**: ثبت، ویرایش و حذف مستقیم محلی.
   - **انبارداری، مواد اولیه، گردش کالا و ضایعات (`/?page=ingredients`, `/?page=recipes`)**: ثبت ورود، مصرف، ضایعات و اصلاح شمارش از «ثبت تغییر موجودی» روی قلم انبار و هشدار نقطه سفارش.
   - **سالن، میزها و رزرواسیون (`/?page=tables`)**: مدیریت وضعیت میزها (آزاد، اشغال، رزرو شده) و تقویم رزرو مهمانان.
   - **باشگاه مشتریان و دفتر نسیه/بدهکاران (`/?page=customers`, `/?page=debtors`)**: امتیاز وفاداری خودکار، صفحهٔ بدهکاران عین وب (کارت‌های خلاصه، فیلترهای وضعیت/منبع/حساب/گروه/بازهٔ تاریخ شمسی، ستون مستندات سفارش، گزارش بدهی گروه‌ها، خروجی CSV)، ثبت بدهی دستی با **جستجوی محصول از فهرست واقعی** و محاسبهٔ جمع کل، تقویم شمسی برای سررسید و بازهٔ فیلتر، و تسویه اقساطی یا کامل با دفتر پرداخت‌ها.
   - **امور مالی، هزینه‌ها، پرسنل و چک‌لیست روزانه (`/?page=finance`, `/?page=staff`, `/?page=staff-checklist`, `/?page=reports`)**: ثبت هزینه‌های جاری، شیفت پرسنل، چک‌لیست روزانه بار و «گزارشات روزانه» وب‌مانند با نمودار فروش و وضعیت سفارش‌ها.
   - **تنظیمات کافه، چاپگرهای حرارتی و پشتیبان‌گیری (`/?page=settings`, `/backup.json`)**.
   - **سرور محلی `/api/v1/*`**: ارائه مستقیم نقاط پایانی REST استاندارد روی سیستم محلی.

## ساخت فایل اجرایی (`.exe` و باینری تمام سیستم‌عامل‌ها)

```bash
cd desktop

# ساخت همزمان برای Windows (.exe)، macOS (.app + binary) و Linux:
make all
# یا:
./build-all.sh
```

در ویندوز (PowerShell):
```powershell
cd desktop
.\build-all.ps1
```

خروجی‌های تولیدشده در پوشه `desktop/dist/`:
- `coffish-desktop-windows-amd64.exe`
- `coffish-desktop-windows-arm64.exe`
- `coffish-desktop-darwin-arm64` و بسته `CoffishDesktop.app`
- `coffish-desktop-darwin-amd64`
- `coffish-desktop-linux-amd64`
- `coffish-desktop-linux-arm64`

## لایسنس آفلاین (قراردادی و feature-flag شده)

فعال‌سازی واقعی از مسیرهای پیشنهادی backend انجام می‌شود و تا ثبت endpointها، fixture مشترک و کلیدهای عمومی در OpenAPI، به‌صورت پیش‌فرض خاموش است:

```text
COFFISH_OFFLINE_LICENSES_ENABLED=true
COFFISH_LICENSE_API_BASE_URL=https://menuvex.ir/api/v1
COFFISH_LICENSE_PUBLIC_KEYS='{"kid":"base64-or-hex-ed25519-public-key"}'
```

`API_BASE_URL` نیز به‌عنوان override runtime پشتیبانی می‌شود؛ `COFFISH_LICENSE_REFRESH_INTERVAL` اختیاری است (پیش‌فرض ۱۰ دقیقه و فقط cadence تلاش کلاینت).

- کد یک‌بارمصرف فقط در body `POST /api/v1/offline-licenses/activate` می‌رود؛ در URL، log یا فایل ذخیره نمی‌شود.
- فایل `age+compact-jws-ed25519-v1` با age identity همان نصب decrypt و سپس با Ed25519، `kid` و کلید public verify می‌شود. response یا JSON decode نشده به‌تنهایی معتبر نیست.
- age identity، refresh token و نشانگر آخرین زمان مشاهده‌شده فقط در OS keyring هستند. ciphertext با permission `0600` و metadata غیرمحرمانه در data directory نگهداری می‌شود.
- تا `expires_at` قطعی شبکه دسترسی POS را قطع نمی‌کند. پس از expiry/revoke/signature-invalid/clock-rollback عملیات تغییر‌دهنده قفل می‌شود و backup/export و داده‌ها باقی می‌مانند.
- خلاصهٔ verified فقط از `GET /api/v1/license` روی loopback و صفحهٔ `/license/status` در دسترس است.

قرارداد کامل و مواردی که باید با Backend/OpenAPI تأیید شوند: [`docs/offline/license-backend-handoff.md`](docs/offline/license-backend-handoff.md).
