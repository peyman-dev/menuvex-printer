# MenuVex Offline — production installers

این سند مسیر رسمی ساخت installer برای اپ دسکتاپ Go/HTMX در
`offline-menuvex/` است؛ نه ایجنت پرینتر Tauri.

## قانون Production: بدون Dummy data

- هر پنج اسکریپت بسته‌بندی با `-tags production` بیلد می‌شوند.
- دیتاست بزرگ کافهٔ ایرانی فقط در
  `offline-menuvex/internal/store/seed_fixture.go` با build constraint
  `//go:build !production` برای تست و توسعهٔ محلی وجود دارد.
- در بیلد Production این فایل اصلاً کامپایل نمی‌شود.
- startup هیچ تابع seed را صدا نمی‌زند.
- route `POST /local/seed-demo` و دکمهٔ «بارگذاری منوی نمونه» در اپ Production
  وجود ندارند.
- دیتابیس نصب‌شده خالی است و فقط با اطلاعات واقعی کافه، ورود/ثبت‌نام، یا
  hydration از سرور sync پر می‌شود.

پس از نصب، مسیر داده‌ها بیرون از محل نصب است:

| سیستم‌عامل | مسیر پیش‌فرض |
|---|---|
| Linux | `~/.config/CoffishDesktop/coffish-desktop.db` |
| Windows | `%APPDATA%\\CoffishDesktop\\coffish-desktop.db` |
| macOS | `~/Library/Application Support/CoffishDesktop/coffish-desktop.db` |

حذف یا ارتقا نباید این مسیر را پاک کند. اگر `COFFISH_DATA_DIR` یا `-data-dir`
صریحاً داده شود، برنامه در آن پوشه از فایل `offline.sqlite` استفاده می‌کند.

## خروجی‌ها

| سیستم | خروجی واقعی | معماری‌های آماده |
|---|---|---|
| Linux | `.deb` + `.AppImage` | amd64/x86_64 و arm64/aarch64 |
| Windows | NSIS `Setup.exe` | x64؛ ARM64 باینری جداگانه در Makefile موجود است |
| macOS | `.dmg` | Apple Silicon و Intel |

اسکریپت‌ها:

- `offline-menuvex/packaging/linux/build-deb.sh [amd64|arm64]`
- `offline-menuvex/packaging/linux/build-tarball.sh [amd64|arm64]`
- `offline-menuvex/packaging/appimage/build-appimage.sh [x86_64|aarch64]`
- `offline-menuvex/packaging/windows/build-nsis.sh`
- `offline-menuvex/packaging/macos/build-dmg.sh [arm64|amd64]`

نسخه در `offline-menuvex/packaging/VERSION` است و با `-X main.version` داخل
باینری ثبت می‌شود.

## ساخت محلی

ساخت Production روی Linux برای `.deb` و باینری‌ها ممکن است؛ AppImage، NSIS و
DMG به ابزارهای مربوط خودشان نیاز دارند. برای DMG واقعی و GUI، native macOS
runner توصیه می‌شود.

```bash
cd offline-menuvex
VERSION="$(cat packaging/VERSION)" make package-linux
VERSION="$(cat packaging/VERSION)" MAKENSIS=/path/to/makensis make package-windows
VERSION="$(cat packaging/VERSION)" make package-macos
```

ساخت مستقیم با هر سیستم‌عامل و خروجی قابل حمل:

```bash
cd offline-menuvex
./packaging/linux/build-deb.sh amd64
./packaging/linux/build-deb.sh arm64
./packaging/linux/build-tarball.sh amd64
APPIMAGETOOL=/path/to/appimagetool ./packaging/appimage/build-appimage.sh x86_64
MAKENSIS=/path/to/makensis ./packaging/windows/build-nsis.sh
./packaging/macos/build-dmg.sh arm64
./packaging/macos/build-dmg.sh amd64
```

برای ساخت AppImage در محیط کاملاً آفلاین، `APPIMAGETOOL` و در صورت نیاز
`RUNTIME_FILE` را از قبل آماده کنید. اسکریپت‌ها خودشان از اینترنت چیزی را به
دیتابیس برنامه اضافه نمی‌کنند.

## CI و Release

فایل workflow آماده در این مسیر است:

```text
offline-menuvex/packaging/ci/offline-installers.yml
```

این فایل باید توسط مالک مخزن به `.github/workflows/offline-installers.yml`
منتقل شود؛ دسترسی `workflows:write` لازم است. Workflow روی همین branch اجرا
می‌شود:

```text
arena/d792fd1d-menuvex-printer
```

بعد از انتقال، CI این کارها را انجام می‌دهد:

1. `go vet`، `gofmt` و `go test ./...` با fixtureهای تستی عادی؛
2. ساخت packageها با `-tags production` و در نتیجه بدون دیتای نمونه؛
3. اعتبارسنجی ساختاری `.deb`، `.AppImage`، `.exe` و `.dmg`؛
4. تست نصب/ارتقا/حذف و حفظ دیتابیس کاربر در runnerهای مربوط؛
5. آپلود artifact؛
6. روی tagهایی مثل `offline-v1.0.0`، آماده‌سازی Release.

## امضا

نسخهٔ فعلی unsigned است:

- Windows ممکن است SmartScreen با عنوان Unknown publisher نشان دهد.
- macOS ممکن است Gatekeeper هشدار بدهد و notarization ندارد.
- برای انتشار عمومی باید بعداً Windows code-signing و Apple Developer ID +
  notarization به Secrets و workflow اضافه شود.

## وضعیت راستی‌آزمایی این checkout

در sandbox فعلی Go toolchain، `makensis`، `appimagetool` و ابزارهای native macOS
در دسترس نیستند؛ بنابراین اجرای کامل installerها در این محیط ادعا نمی‌شود.
قبل از انتشار، این دستورات باید روی Linux توسعه‌دهنده و سپس native GitHub runners
اجرا شوند:

```bash
cd offline-menuvex
go test ./...
go build -tags production ./cmd/coffish-desktop
./packaging/linux/build-deb.sh amd64
```

موفقیت `go test ./...` به‌تنهایی اثبات نمی‌کند که نصب و اجرای GUI ویندوز یا مک
تست شده است؛ نتیجهٔ native runner باید جداگانه ثبت شود.
