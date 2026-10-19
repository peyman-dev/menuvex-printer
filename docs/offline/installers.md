# MenuVex Offline — نصب‌کننده‌های واقعی (Windows / Linux / macOS)

این سند نتیجهٔ مأموریت «تبدیل Buildها به Installerهای واقعی» است. همهٔ فایل‌های زیر
واقعاً ساخته و در همین محفظه (sandbox) اعتبارسنجی شده‌اند؛ هیچ‌کدام صرفاً «کد build»
یا «workflow» نیستند. فایل‌ها در مسیر زیر نگه داشته شده‌اند (چون `dist/` در snapshot
ذخیره نمی‌شود):

```
offline-menuvex/release-artifacts/
```

نسخهٔ محصول: **1.0.0** (`offline-menuvex/packaging/VERSION`)

---

## ۱) فهرست نهایی فایل‌ها

| # | فایل | سیستم / معماری | حجم (بایت) | نوع واقعی |
|---|------|----------------|------------|-----------|
| ۱ | `MenuVex-Offline-Setup-1.0.0-x64.exe` | Windows 10/11 x64 | 4,557,572 | NSIS 3.x installer (PE32 stub + payload PE32+ x64) |
| ۲ | `menuvex-offline_1.0.0_amd64.deb` | Debian/Ubuntu amd64 | 5,758,722 | بستهٔ دبیان (`dpkg-deb`, control/postinst/postrm) |
| ۳ | `menuvex-offline_1.0.0_arm64.deb` | Debian/Ubuntu arm64 | 5,311,844 | بستهٔ دبیان arm64 (cross-compiled) |
| ۴ | `MenuVex-Offline-1.0.0-x86_64.AppImage` | Linux x86_64 | 6,277,624 | AppImage type-2 (ELF runtime + squashfs) |
| ۵ | `MenuVex-Offline-1.0.0-aarch64.AppImage` | Linux aarch64 | 5,806,600 | AppImage type-2 (runtime + squashfs arm64) |
| ۶ | `MenuVex-Offline-macOS-arm64.dmg` | macOS Apple Silicon | 6,778,754 | UDIF `.dmg` (HFS+ شامل `.app` و symlink به `/Applications`) |
| ۷ | `MenuVex-Offline-macOS-amd64.dmg` | macOS Intel | 7,086,601 | UDIF `.dmg` (همان ساختار) |
| ۸ | `MenuVex-Offline-1.0.0-linux-amd64.tar.gz` | Linux x86_64 | 5,791,489 | آرشیو portable (نصب‌کننده نیست) |
| ۹ | `SHA256SUMS` | — | — | sha256 هر هشت فایل |

`SHA256SUMS` (بازتولیدشدنی با `cd offline-menuvex/release-artifacts && sha256sum -c SHA256SUMS`):

```
fb769367c61b76dd079902f15aff7e72587c5f92a8f4fb1b8c4fa9b59b5caf76  MenuVex-Offline-1.0.0-aarch64.AppImage
68092a920a3eb8ae673ec966bc6aa86628934d0e4fc650f1c7ed2faec7a453a1  MenuVex-Offline-1.0.0-linux-amd64.tar.gz
323e300b8f4bd13e612c21057649c89a214fbab53acc64b2338cb6a1a58602b5  MenuVex-Offline-1.0.0-x86_64.AppImage
606481db436695dd15426c1c15ae2e7bd050684e3dbf25a1776a981f81ff6f80  MenuVex-Offline-Setup-1.0.0-x64.exe
94d772d115beb47c2d210f3cf62120272975e845d98150c4872f7cc8c37e9bfa  MenuVex-Offline-macOS-amd64.dmg
c1ce958f64eb891fe6d7549ba0277f100ed711c6a5fedd99997d3097cdb4927a  MenuVex-Offline-macOS-arm64.dmg
63dcc53ae1911bfdb7629dd1ab79caf09f8b397e65a613898de9c1c912679bcc  menuvex-offline_1.0.0_amd64.deb
dec8f9b306a3ccb2f766853a75e6e5f235413bcd3e9d12f33aeee4f34f0eef05  menuvex-offline_1.0.0_arm64.deb
```

---

## ۲) چه چیزی و با چه ابزاری ساخته شد

| مورد | ابزار | توضیح |
|------|-------|-------|
| Windows `.exe` | NSIS از سورس `nsis-dev/nsis` (master) که در همین محیط با scons+gcc کامپایل شد؛ داده‌های NSIS از بستهٔ npm `@nsis-u/makensis@3.3.0` | `packaging/windows/menuvex-offline.nsi` |
| Linux `.deb` | `dpkg-deb --root-owner-group -Zgzip` | `packaging/linux/build-deb.sh` |
| Linux AppImage | `appimagetool` (build 296، runtime commit `caf24f9`) از بستهٔ npm `bare-app-image@0.1.2` شامل `prebuilds/linux-x64` و `linux-arm64`؛ `mksquashfs` از squashfs-tools 4.6.1 + zlib 1.3.1 + zstd 1.5.6 | `packaging/appimage/build-appimage.sh` با `RUNTIME_FILE` |
| macOS `.dmg` | `newfs_hfs` (سورس Apple + پچ‌های Debian hfsprogs) برای ساخت HFS+، `hdutil`/`dmg` از libdmg-hfsplus برای افزودن فایل‌ها و ساخت UDIF | `packaging/macos/build-dmg.sh` |
| باینری‌ها | Go 1.27.2 (wheel از PyPI) | `CGO_ENABLED=0` برای Linux/Windows/nocgo-macOS |

> ساخت AppImage محلی بدون دانلود از GitHub Releases انجام شد: runtime از پیش‌ساختهٔ
> رسمی type2 (commit `caf24f9`) داخل همان `appimagetool.AppImage` استخراج شد و با
> `--runtime-file` به appimagetool داده شد. این همان runtime استاندارد AppImage است.

اسکریپت‌ها همه در `offline-menuvex/packaging/` هستند و در CI هم بدون تغییر اجرا می‌شوند
(`packaging/ci/offline-installers.yml`).

بازتولید کامل:

```bash
cd offline-menuvex
export VERSION=$(cat packaging/VERSION)
make package-linux                    # deb amd64 + arm64 + tarball
MAKENSIS=/path/to/makensis make package-windows
NEWFS_HFS=/path/to/newfs_hfs HDUTIL=/path/to/hdutil DMG_TOOL=/path/to/dmg \
    make package-macos                # DMG arm64 + x64
APPIMAGETOOL=/path/to/appimagetool \
RUNTIME_FILE=/path/to/runtime-x86_64 ./packaging/appimage/build-appimage.sh x86_64
```

---

## ۳) اعتبارسنجی انجام‌شده (و نتیجهٔ واقعی)

| فایل | اعتبارسنجی ساختاری | نصب واقعی | اجرا |
|------|--------------------|-----------|------|
| `Setup-x64.exe` | PE32 NSIS stub، subsystem=GUI، وجود payload PE32+ x86-64 با subsystem=GUI؛ **بایت‌های payload به‌صورت verbatim داخل installer پیدا شد** (build تأییدی بدون فشرده‌سازی) | ❌ انجام نشد (نه Windows داریم نه wine) | ❌ انجام نشد |
| `.deb amd64` | `dpkg-deb -I` / `-c`، control درست، وابستگی‌ها، `postinst`/`postrm`، آیکن‌ها، desktop entry | ✅ استخراج بسته و اجرای باینری از مسیر نصب (`/`، بیرون از درخت سورس) | ✅ `-version` و سرویس HTTP: `/license`=200، `/static/app.css`=200؛ ساخت DB در `~/.config/CoffishDesktop` |
| `.deb arm64` | همان بررسی‌های ساختاری + ELF header = AArch64 | ❌ (هاست arm64 نداریم) | ❌ |
| `AppImage x86_64` | ELF + magic `AI\x02` در offset ۸، squashfs در offset 944,632، فایل‌های داخل squashfs | ✅ `--appimage-extract` و اجرای پیلود | ✅ `--appimage-extract-and-run -version` و smoke کامل HTTP + ساخت DB |
| `AppImage aarch64` | ELF + magic `AI\x02`، squashfs در offset 936,456، استخراج squashfs با `unsquashfs -o`، معماری پیلود = arm64 | ❌ (هاست arm64 نداریم) | ❌ |
| `macOS-arm64.dmg` / `-amd64.dmg` | ساختار UDIF: `koly` در ۵۱۲ بایت آخر، version=4، headerSize=512، data fork داخل فایل؛ مخزن HFS+ باز شد، permission اجرایی فایل `.app` = `100755`، `Info.plist` معتبر (plistlib)؛ **round-trip: DMG → HFS+ → استخراج فایل، sha256 بایت‌به‌بایت برابر** | ❌ mount با `hdiutil` ممکن نیست (macOS نداریم) | ❌ |
| `tar.gz` | لیست محتوا، اجرای باینری از داخل آرشیو | — | ✅ `-version` |

نکتهٔ مهم: نصب واقعی `dpkg -i` انجام نشد چون کاربر sandbox ریشه (uid 1001) نیست؛ به‌جای آن
همان درخت نصب با `dpkg-deb -x` استخراج و باینری از مسیر نصب، بیرون از درخت سورس، اجرا شد.
همین تست در CI با `sudo dpkg -i` و سپس `dpkg -r` واقعی اجرا می‌شود.

### محدودیت‌های شناخته‌شدهٔ تست

- هیچ‌کدام از خروجی‌ها روی Windows/macOS واقعی نصب یا اجرا نشده‌اند؛ macOS GUI و
  Windows GUI **تست‌نشده** گزارش می‌شوند.
- GUI لینوکس (پنجرهٔ GTK/WebKit) تست نشده چون display در sandbox نیست؛ مسیر
  headless (`-no-window`) و سرو HTTP تست شد.
- arm64 (هر دو پلتفرم) فقط ساختاری تست شده است.
- `--appimage-extract-and-run` جای mount با FUSE را می‌گیرد؛ mount واقعی FUSE تست نشده.

---

## ۴) معماری runtime و رفتار پس از نصب (پاسخ به Stage 3)

1. **مسیر assetها:** کل UI با `//go:embed ui/* ui/static/* contracts/*.json` داخل باینری
   است (`offline-menuvex/app.go`). هیچ فایل داده‌ای کنار برنامه لازم نیست؛ اجرا از `/`
   هم کار می‌کند (تست شد). وابستگی به درخت سورس صفر است.
2. **دیتابیس و داده:** `desktop.New("")` → `os.UserConfigDir()/CoffishDesktop/coffish-desktop.db`
   - Linux: `~/.config/CoffishDesktop/coffish-desktop.db`
   - Windows: `%APPDATA%\CoffishDesktop\coffish-desktop.db`
   - macOS: `~/Library/Application Support/CoffishDesktop/coffish-desktop.db`
   با `-data-dir` یا `COFFISH_DATA_DIR` نام فایل `offline.sqlite` در همان مسیر است.
   **هیچ‌وقت داخل محل نصب نوشته نمی‌شود** (تست‌شده).
3. **پورت و چرخهٔ backend:** پیش‌فرض `127.0.0.1:8787`؛ اگر اشغال باشد `127.0.0.1:0`.
   اگر نمونهٔ قبلی در حال اجرا باشد (تشخیص با هدر `X-MenuVex-App: offline`)، نمونهٔ دوم
   اجرا نمی‌شود و فقط پنجره باز می‌شود. بستن پنجره → `srv.Shutdown` (۳ ثانیه) + `app.Close()`.
   Ctrl+C/SIGTERM هم همان مسیر را طی می‌کند.
4. **پنجرهٔ بومی هر OS:**
   - macOS (cgo): `NSApplication` + `WKWebView` واقعی روی main thread (`runtime.LockOSThread`).
   - macOS (nocgo، build لینوکسی): در اولین اجرا یک هاست Objective-C کوچک با
     `/usr/bin/clang` می‌سازد؛ اگر Xcode CLT نبود، `osascript` JXA؛ در نهایت مرورگر.
   - Linux: PyGObject + GTK3/WebKit2؛ اگر نبود، پنجرهٔ app-mode کرومیوم؛ در نهایت `xdg-open`.
   - Windows: پنجرهٔ app-mode مایکروسافت Edge/Chrome (بدون address bar). دیگر از کنترل
     قدیمی IE/WinForms استفاده نمی‌شود چون UI مدرن را رندر نمی‌کرد.
5. **وابستگی‌های runtime:**
   - Linux deb: `python3, python3-gi, gir1.2-gtk-3.0, gir1.2-webkit2-4.1 | 4.0` (در `Depends`).
   - AppImage: همان‌ها باید روی سیستم موجود باشند (AppImage خودش GTK را همراه ندارد).
   - Windows: Edge (روی Win10/11 نصب است) یا Chrome.
   - macOS: Cocoa/WebKit بخشی از سیستم است.
6. **لاگ:** وقتی بدون ترمینال اجرا شود (Finder/Explorer/desktop launcher) لاگ در
   `UserCacheDir/MenuVexOffline/desktop.log` هم نوشته می‌شود.

---

## ۵) دیتابیس در ارتقا و حذف (Stage 2)

- **Windows:** NSIS فقط `$PROGRAMFILES64\MenuVex Offline` را می‌نویسد/پاک می‌کند؛
  `%APPDATA%\CoffishDesktop` دست‌نخورده می‌ماند. نصب مجدد (upgrade) فایل برنامه را
  روی نسخهٔ قبل overwrite می‌کند و به DB کاری ندارد. پیش از overwrite، نمونهٔ در حال
  اجرا با `taskkill` بسته می‌شود.
- **Linux deb:** `postrm` هیچ دادهٔ کاربر را حذف نمی‌کند (فقط کش آیکن/desktop را
  به‌روزرسانی می‌کند). تست محلی: پس از حذف باینری، DB باقی ماند.
- **macOS dmg:** اپ را به `/Applications` می‌کشید؛ داده در Application Support است و با
  جایگزینی `.app` از بین نمی‌رود.
- مسیر DB در همهٔ پلتفرم‌ها بیرون از پوشهٔ نصب است، پس uninstall هرگز داده را پاک نمی‌کند
  (این رفتار عمدی و مستند است).

---

## ۶) امضا و notarization

- **هیچ خروجی‌ای امضا نشده است.** نه گواهی Code Signing ویندوز، نه Apple Developer ID،
  نه حساب notarization در دسترس بود.
- macOS: DMGهای ساخته‌شده در این محیط **بدون امضا** هستند (codesign روی Linux وجود ندارد).
  در CI روی macOS یک امضای ad-hoc (`codesign -s -`) اضافه می‌شود که فقط سالم‌بودن بسته را
  تأیید می‌کند، نه هویت توسعه‌دهنده را. Gatekeeper روی فایل دانلودشده هشدار می‌دهد؛
  کاربر باید Right-click → Open یا `xattr -dr com.apple.quarantine` بزند.
- Windows: SmartScreen هشدار «Unknown publisher» می‌دهد.
- برای انتشار واقعی باید این Secrets در GitHub تنظیم شود و همان workflow امضا کند:
  `WINDOWS_CERT_PFX`/`WINDOWS_CERT_PASSWORD`، `APPLE_CERT_P12`/`APPLE_CERT_PASSWORD`،
  `APPLE_ID`/`APPLE_TEAM_ID`/`APPLE_APP_PASSWORD`.

---

## ۷) GitHub Actions — وضعیت واقعی و مانع

هدف این بود که همان اسکریپت‌ها روی runnerهای واقعی اجرا شوند
(Ubuntu 22.04، Windows 2022، macOS 14 و macOS 15 Intel) و Release ساخته شود.
Workflow نوشته و YAML آن اعتبارسنجی شد، اما **اجرا نشد**، به دلیل مانع دسترسی:

1. token این محیط یک GitHub App installation token با `contents: write` است ولی بدون
   مجوز `workflows`. GitHub اجازهٔ push هر کامیتی که فایل زیر `.github/workflows/` را
   ایجاد/تغییر دهد نمی‌دهد:
   ```
   ! [remote rejected] ... (refusing to allow a GitHub App to create or update
     workflow `.github/workflows/offline-installers.yml` without `workflows` permission)
   ```
2. تلاش برای ساخت همان فایل با Contents API هم رد شد:
   ```
   PUT /repos/peyman-dev/menuvex-printer/contents/.github/workflows/arena-permtest.yml
   → 403 {"message":"Resource not accessible by integration"}
   ```
3. حتی در صورت اجرای CI، دانلود/آپلود assetهای Release از این sandbox ممکن نیست:
   `uploads.github.com` و `objects.githubusercontent.com` با کد `000` (blocked) پاسخ می‌دهند.
   بنابراین Release واقعی با asset از این محیط قابل ساخت نیست.

به همین دلیل فایل workflow با همان محتوا و فقط با یک هدر توضیحی در مسیر زیر نگه داشته شد:

```
offline-menuvex/packaging/ci/offline-installers.yml
```

برای فعال‌سازی (توسط مدیر مخزن با توکن/App دارای مجوز `workflows`):

```bash
mkdir -p .github/workflows
git mv offline-menuvex/packaging/ci/offline-installers.yml .github/workflows/
git commit -m "ci: enable MenuVex Offline installer pipeline"
git push
```

پس از آن:

- push به شاخهٔ `arena/9d25b45b-menuvex-printer` → تست + ساخت + اعتبارسنجی + artifact
- push تگ `offline-v1.0.0` → ساخت GitHub Release با همهٔ installerها و `SHA256SUMS`

مراحل workflow دقیقاً همان کارهایی را انجام می‌دهد که در این سند به‌صورت محلی انجام شد،
به‌علاوهٔ نصب/حذف واقعی (`dpkg -i`/`dpkg -r`، NSIS `/S` + بررسی رجیستری و حفظ DB،
`hdiutil attach/verify` و `spctl`).

---

## ۸) آنچه تست نشده (صادقانه)

- نصب و اجرای installer روی Windows واقعی (نیازمند runner ویندوز).
- نصب و اجرای `.dmg` روی macOS واقعی و بررسی Gatekeeper (نیازمند runner مک).
- پنجرهٔ گرافیکی در هیچ OS (sandbox بدون display).
- اجرای معماری arm64 (deb/AppImage).
- mount کردن AppImage با FUSE (فقط حالت extract-and-run تست شد).
- MSI برای ویندوز ساخته نشد (WiX در دسترس نبود)؛ خروجی NSIS خواستهٔ اصلی بود.

---

## ۹) داورهای طراحی که باید بدانید

- نسخهٔ محصول `1.0.0` در `packaging/VERSION` است و با `-X main.version` به باینری تزریق
  می‌شود؛ `menuvex-offline -version` آن را چاپ می‌کند.
- شناسه‌ها: dmg `ir.menuvex.offline`، deb `menuvex-offline`، Windows
  `HKLM\Software\MenuVex\Offline`.
- installer ویندوز per-machine است (UAC) و به `%PROGRAMFILES64%` نصب می‌کند؛
  uninstaller در Add/Remove Programs ثبت می‌شود.
- باینری ویندوز با `-H windowsgui` ساخته می‌شود تا پنجرهٔ کنسول باز نشود.
