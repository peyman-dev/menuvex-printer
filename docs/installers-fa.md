# فایل نصبی برای کافه، نه سورس پروژه

مشتری نباید Node.js، Rust، npm یا ابزار build نصب کند. آن‌ها فقط روی سیستم توسعه یا runnerهای GitHub نصب می‌شوند. مشتری فایل اجرایی ساخته‌شده را نصب می‌کند؛ آماده‌سازی driver/permission پرینتر یک موضوع جداست.

## خروجی‌ها

پس از موفقیت workflow، چهار دانلود زیر در بخش Artifacts آن اجرا ظاهر می‌شوند:

| دانلود                                  | فایل‌های داخل آن                                   |
| --------------------------------------- | -------------------------------------------------- |
| `MenuVex-Installer-Windows-x64`         | نصب‌کنندهٔ NSIS با پسوند `-setup.exe`              |
| `MenuVex-Installer-Linux-x64`           | `.deb` و `.AppImage`                               |
| `MenuVex-Installer-macOS-Apple-Silicon` | `.dmg` برای M1/M2/M3/M4 و سایر مک‌های ARM64 سازگار |
| `MenuVex-Installer-macOS-Intel`         | `.dmg` برای مک Intel                               |

GitHub این خروجی‌ها را در یک ZIP حمل می‌کند؛ **ZIP سورس نیست**. آن را خودتان استخراج کنید و فایل `.exe` / `.dmg` / `.deb` را برای تست بدهید یا روی سایت بگذارید. checksum و manifest هم همراه‌اند. فایل‌های `Build-Lock-*` برای تیم فنی هستند، نه مشتری.

## یک‌بار فعال‌سازی توسط مالک مخزن

اتصال GitHub این محیط اجازهٔ ساخت فایل workflow ندارد. نمونهٔ آماده در `docs/ci/checks.yml` است، اما تا وقتی زیر `.github/workflows/` قرار نگیرد اجرا نمی‌شود.

راه مرورگری، بدون نصب ابزار جدید:

1. در GitHub مخزن `peyman-dev/menuvex-printer` را باز کنید.
2. branch را روی `arena/01a0affb-menuvex-printer` بگذارید؛ نه `main`.
3. فایل `docs/ci/checks.yml` را باز کنید و محتوای کاملش را کپی کنید.
4. در صفحهٔ اصلی همان branch، `Add file → Create new file` را بزنید.
5. نام فایل را `.github/workflows/installers.yml` بگذارید و محتوای کپی‌شده را وارد کنید.
6. `Commit changes` را روی همین branch انجام دهید.
7. وارد تب `Actions` شوید. اجرای `Build MenuVex Installers` با همین push شروع می‌شود. اگر Actions در مخزن غیرفعال است، مالک باید آن را در تنظیمات مجاز کند.
8. پس از موفقیت buildها، در پایین صفحهٔ همان اجرا، بخش `Artifacts` را باز کنید.

یا با حساب GitHub دارای مجوز push و workflow، در checkout محلی همین branch:

```sh
git pull --ff-only origin arena/01a0affb-menuvex-printer
mkdir -p .github/workflows
cp docs/ci/checks.yml .github/workflows/installers.yml
git add .github/workflows/installers.yml
git commit -m "Enable native installer builds"
git push origin arena/01a0affb-menuvex-printer
```

در صورت خطای مجوز، workflow را با حساب مالک/دارای مجوز در وب GitHub ایجاد کنید یا اتصال GitHub را با مجوز مناسب دوباره تنظیم کنید. هیچ رمز یا token را در چت ارسال نکنید.

## روند ساخت

GitHub روی runnerهای واقعی Windows، Linux و دو معماری macOS، ابزارهای build را نصب می‌کند، تست‌ها را اجرا می‌کند و Tauri installer می‌سازد. سیستم لینوکس توسعه‌دهنده لازم نیست خودش تبدیل به محیط build مک و ویندوز شود.

اسکریپت `scripts/collect-installers.mjs` فقط فایل‌های خروجی واقعی build را جمع می‌کند. اگر فایل نصب وجود نداشته باشد، خالی باشد یا چند خروجی مبهم وجود داشته باشد، مرحله شکست می‌خورد. سورس پروژه را به‌جای installer منتشر نمی‌کند. SHA-256 از خود فایل محاسبه می‌شود. این بررسی ساختار فایل/هش است، نه تأیید امنیت یا اجراشدن installer.

خروجی‌ها فعلاً `test-candidate` هستند. انتشار خودکار عمومی نداریم؛ ابتدا نصب روی دستگاه تست و چاپ واقعی باید تأیید شود. ساخت‌ها تا قبل از اجرای موفق در GitHub، فایل آماده محسوب نمی‌شوند. فایل‌های artifact چهارده روز نگه داشته می‌شوند؛ برای لینک دائمی سایت، پس از تأیید فایل‌ها را روی فضای ذخیره‌سازی خودتان یا GitHub Release قرار دهید.

## محدودیت‌های مهم برای انتشار کشوری

- Windows: نصب‌کنندهٔ برنامه با driver پرینتر یک چیز نیست. بعضی پرینترهای USB با درایور کارخانه به libusb دسترسی نمی‌دهند؛ انتخاب مدل پشتیبانی‌شده/driver تأییدشده یا transport اسپولر جدا لازم است. نصب WebView2 ممکن است توسط installer و با دسترسی اینترنت انجام شود. Node/Rust لازم نیست.
- macOS: build بدون Developer ID و notarization ممکن است توسط Gatekeeper مسدود شود. به مشتری نگویید محافظت سیستم را غیرفعال کند؛ امضا و notarization باید پیش از انتشار عمومی انجام شود.
- Linux: بستهٔ deb وابستگی‌های زمان اجرا را از مدیر بسته می‌گیرد؛ USB ممکن است به تنظیم اولیهٔ udev توسط مسئول نصب نیاز داشته باشد.
- فایل نصبی به‌تنهایی integration سایت را ایجاد نمی‌کند. چاپ آزمایشی داخل Agent مستقل است؛ چاپ خودکار سفارش MenuVex نیاز به integration SDK و pairing سایت دارد.
- نسخهٔ عمومی نیازمند امضای Windows، notarization مک، آزمون نصب/حذف/آپگرید و ماتریس واقعی مدل‌های پرینتر است. موفقیت build یا ۱۸ تست هسته به معنی تأیید همهٔ این موارد نیست.

## نسخهٔ Legacy برای Windows 7 SP1

کد Legacy جدا از Tauri در `legacy-windows/` اضافه شده است. مرحلهٔ بسته‌بندی ویندوز در Actions موجود، ساخت و تست x86/x64 را هم اجرا می‌کند. پس از موفقیت، داخل دانلود `MenuVex-Installer-Windows-x64` این دو پوشه وجود خواهند داشت:

- `Legacy-Windows7/x86` برای Windows 7 SP1 نسخهٔ ۳۲بیتی
- `Legacy-Windows7/x64` برای Windows 7 SP1 نسخهٔ ۶۴بیتی

فایل setup ریشهٔ ZIP همچنان نسخهٔ جدید است و نباید روی ویندوز ۷ نصب شود. فایل Legacy را از پوشهٔ درست استخراج کنید. این فایل‌ها تا زمان build موفق وجود ندارند و تا تست روی Windows 7 واقعی تأییدشده نیستند.

Legacy از صف پرینترهای نصب‌شدهٔ ویندوز استفاده می‌کند و نیاز به WebView2 ندارد. SDK سایت باید نوع `connection.type = spooler` را پشتیبانی کند؛ SDK همین مخزن به‌روزرسانی شده ولی کپی آن در پروژهٔ Next.js باید جداگانه به‌روزرسانی شود. راهنمای کامل نصب، محدودیت‌ها و تست‌ها در `legacy-windows/README.md` است.
