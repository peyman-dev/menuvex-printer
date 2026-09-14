# راهنمای نصب صفر تا صد — Novex Printer Agent

> این راهنما فرض می‌کند از صفر شروع می‌کنی: بدون Go، بدون باینری آماده.
> پایان راهنما: ایجنت روی کامپیوتر کنار پرینتر اجرا می‌شود، پرینتر تست‌پرینت
> می‌گیرد، و اپ Next.js تو به آن وصل می‌شود.

## قدم صفر: این برنامه دقیقاً کجا نصب می‌شود؟

این مهم‌ترین نکته است:

```
┌─────────────────────────────┐      اینترنت      ┌──────────────┐
│ کامپیوتر صندوق / رستوران     │ ◄──────────────► │ سرور سایت    │
│  - مرورگر (سایت منووکس)      │                    │ (Next.js)    │
│  - Novex Printer Agent  ◄── نصب می‌شود          └──────────────┘
│  - پرینتر (شبکه یا USB)      │
└─────────────────────────────┘
```

- ایجنت روی **همان کامپیوتری** نصب می‌شود که به پرینتر دسترسی دارد
  (کامپیوتر صندوق، کیوسک، دفتر).
- ایجنت روی **سرور** نصب نمی‌شود.
- مرورگرِ همان کامپیوتر با ایجنت حرف می‌زند (`http://127.0.0.1:8765`).
- پس اگر ۵ شعبه داری، ایجنت روی ۵ کامپیوتر نصب می‌شود (هر کدام توکن خودش).

## قدم ۱: پیش‌نیازها

فقط دو چیز لازم است (فقط برای «ساخت» برنامه؛ خود برنامه هیچ پیش‌نیازی ندارد):

| ابزار | نصب |
|---|---|
| **Git** | ویندوز: `winget install Git.Git` · مک: `xcode-select --install` · لینوکس: `sudo apt install git` |
| **Go 1.21+** | از https://go.dev/dl دانلود و نصب کن (Next → Next)، بعد ترمینال را ببند و باز کن |

تست نصب:

```bash
git --version
go version     # باید go1.21 یا بالاتر نشان بدهد
```

## قدم ۲: گرفتن کد

> ⚠️ فعلاً کد روی برنچ `arena/01a0a04a-menuvex-printer` است (PR شماره ۱).
> راحت‌ترین کار: اول در گیت‌هاب PR را Merge کن، بعد دستور زیر.

```bash
git clone https://github.com/peyman-dev/menuvex-printer.git
cd menuvex-printer
```

اگر PR را Merge نکرده‌ای، بعد از clone این را هم بزن:

```bash
git checkout arena/01a0a04a-menuvex-printer
```

## قدم ۳: ساخت فایل اجرایی

داخل پوشه پروژه:

```bash
# ویندوز (PowerShell یا CMD):
go build -o NovexPrinterAgent.exe ./cmd/agent

# مک / لینوکس:
go build -o novex-printer-agent ./cmd/agent
```

اگر خطایی نداد، فایل اجرایی ساخته شد. ✅ (اولین بار کمی طول می‌کشد.)

## قدم ۴: نصب (هر سیستم‌عامل)

### ویندوز

```powershell
cd menuvex-printer
powershell -ExecutionPolicy Bypass -File scripts\install-windows.ps1
```

چه اتفاقی می‌افتد:
- فایل به `%LOCALAPPDATA%\NovexPrinterAgent\NovexPrinterAgent.exe` کپی می‌شود.
- با هر بار روشن شدن ویندوز، خودکار اجرا می‌شود (بدون پنجره سیاه).
- توکن را چاپ می‌کند — **آن را نگه دار.**

> اگر ویندوز درباره ناشناس بودن برنامه هشدار داد (SmartScreen)، روی
> More info → Run anyway بزن (چون فعلاً امضای دیجیتال ندارد).

### مک

```bash
cd menuvex-printer
./scripts/install-macos.sh
```

- فایل به `~/bin/novex-printer-agent` کپی می‌شود.
- با هر بار لاگین، خودکار اجرا می‌شود.
- توکن را چاپ می‌کند — **آن را نگه دار.**

> اگر مک گفت «cannot verify developer»: روی فایل راست‌کلیک → Open → Open.
> یا یک‌بار: `xattr -d com.apple.quarantine ~/bin/novex-printer-agent`

### لینوکس

```bash
cd menuvex-printer
./scripts/install-linux.sh
```

- فایل به `~/.local/bin/novex-printer-agent` کپی می‌شود.
- با هر بار لاگین، خودکار اجرا می‌شود.
- توکن را چاپ می‌کند — **آن را نگه دار.**

> اگر گفـت `command not found`: یک‌بار `export PATH="$HOME/.local/bin:$PATH"`
> را به `~/.bashrc` اضافه کن و ترمینال را دوباره باز کن.

## قدم ۵: اولین اجرا و گرفتن توکن

```bash
# ویندوز:
%LOCALAPPDATA%\NovexPrinterAgent\NovexPrinterAgent.exe

# مک / لینوکس:
~/bin/novex-printer-agent        # یا novex-printer-agent اگر در PATH است
```

خروجی شبیه این است:

```
novex-agent: Novex Printer Agent v1.0.0 starting on http://127.0.0.1:8765
novex-agent: API token: 714331ba12d2... (۶۴ کاراکتر)
```

**توکن = رمز اتصال سایت به ایجنت.** هر کامپیوتر توکن خودش را دارد.
اگر گمش کردی:

```bash
novex-printer-agent --print-token
```

تست سلامت (در مرورگر یا ترمینال):

```bash
curl http://127.0.0.1:8765/health
# → {"status":"ok","version":"1.0.0",...}
```

## قدم ۶: وصل کردن پرینتر شبکه‌ای (LAN)

۱. **آی‌پی پرینتر را پیدا کن.** معمولاً: دکمه Feed را نگه دار و پرینتر را
   روشن کن تا صفحه تنظیمات (با IP) چاپ شود. مثلاً `192.168.1.50`.
   (پورت اکثر فیش‌پرینترها `9100` است ولی حتماً از دفترچه پرینتر مطمئن شو.)
۲. مطمئن شو کامپیوتر و پرینتر به **یک شبکه** وصل‌اند:
   ```bash
   ping 192.168.1.50
   ```
۳. کنسول داخلی را باز کن: **http://127.0.0.1:8765/**
۴. توکن را بچسبان → Connect → بخش «Register a LAN printer»:
   Name = مثلاً `OCOM`، Address = `192.168.1.50`، Port = `9100` → Register.
   (یا تیک LAN scan را بزن و Refresh کن تا خودش پیدا کند.)
۵. دکمه **Test Print** کنار پرینتر → باید فیش تست چاپ شود. 🎉

> برای پرینتر شبکه‌ای هیچ درایوری لازم نیست. آی‌پی را هم بهتر است در
> مودم **ثابت (Static/Reserved)** کنی تا با هر بار خاموش/روشن عوض نشود.

## قدم ۷: وصل کردن پرینتر USB

اول شرط هر سیستم‌عامل را انجام بده (فقط یک‌بار)، بعد در کنسول Refresh بزن:

| سیستم | کار لازم (یک‌بار) |
|---|---|
| **لینوکس** | `sudo usermod -aG lp $USER` بعد لاگ‌اوت/لاگین. (پرینتر باید در `/dev/usb/lp*` دیده شود: `ls /dev/usb/`) |
| **ویندوز** | پرینتر را یک‌بار در Settings → Printers نصب کن (هر درایوری؛ ایجنت RAW می‌فرستد). |
| **مک** | صف Raw بساز (جزئیات در `docs/USB.md`): `lpadmin -p OCOM_USB -E -v "usb://..." -m raw` |

اگر پرینتر در لیست آمد (با آی‌دی `usb:...`) → Test Print بزن.

اگر نیامد، یعنی شرط بالا برقرار نیست — جزئیات کامل و صادقانه محدودیت‌ها:
[`docs/USB.md`](USB.md).

## قدم ۸: اتصال سایت Next.js

### ۸-۱: نصب SDK

> ⚠️ پکیج هنوز روی npm منتشر نشده؛ فعلاً از مسیر لوکال نصب کن:

```bash
# یک‌بار: بیلد SDK
cd menuvex-printer/sdk/typescript
npm install
npm run build

# در پروژه Next.js:
npm install file:../menuvex-printer/sdk/typescript
```

### ۸-۲: معرفی آدرس سایت به ایجنت (خیلی مهم)

مرورگر فقط از آدرس‌هایی که در ایجنت مجاز شده‌اند می‌تواند وصل شود.
فایل کانفیگ را باز کن:

| سیستم | مسیر فایل کانفیگ |
|---|---|
| ویندوز | `%APPDATA%\NovexPrinterAgent\config.json` |
| مک | `~/Library/Application Support/NovexPrinterAgent/config.json` |
| لینوکس | `~/.config/novex-printer-agent/config.json` |

بخش `trustedOrigins` را ویرایش کن (آدرس دقیق سایتی که در مرورگر باز می‌شود):

```json
{
  "trustedOrigins": [
    "https://menuvex.ir",
    "http://localhost:3000"
  ]
}
```

بعد ایجنت را ببند و دوباره اجرا کن. (اگر خطای `FORBIDDEN_ORIGIN` گرفتی،
یعنی همین قدم را جا انداخته‌ای.)

### ۸-۳: کد نمونه

```tsx
"use client";
import { NovexPrinterAgent } from "novex-printer-agent";

// توکنِ همین کامپیوتر (قدم ۵) — مثلاً از صفحه تنظیمات اپ بخوان:
const agent = new NovexPrinterAgent({
  host: "127.0.0.1",
  port: 8765,
  token: localStorage.getItem("novex_token")!,
});

await agent.connect();
const printers = await agent.getPrinters();
// printers[0].id مثل "tcp:192.168.1.50:9100" یا "usb:..."

// ساخت بایت‌های ESC/POS (init + متن + برش):
const enc = new TextEncoder();
const bytes = new Uint8Array([
  0x1b, 0x40,                       // init
  ...enc.encode("Hello MenuVex!\n"),
  0x0a, 0x0a, 0x1d, 0x56, 0x00,     // چند خط + برش
]);

await agent.print({ printerId: printers[0].id, data: bytes });
```

### ۸-۴: نکته مهم درباره توکن در سایت واقعی

توکنِ هر کامپیوتر فرق می‌کند و سایت تو در مرورگرِ همان کامپیوتر اجرا
می‌شود. پس توکن را **هاردکد نکن**. راه درست: یک صفحه «تنظیمات چاپگر» در
اپ بگذار که کاربر هر شعبه یک‌بار توکن کامپیوتر خودش را وارد کند و در
`localStorage` ذخیره شود. (مثل وارد کردن رمز وای‌فای — یک‌بار برای همیشه.)

## قدم ۹: استارت خودکار (شروع با ویندوز/مک/لینوکس)

اسکریپت نصب (قدم ۴) این را خودکار انجام داد. برای مدیریت دستی:

```bash
novex-printer-agent --install-autostart     # فعال
novex-printer-agent --uninstall-autostart   # غیرفعال
novex-printer-agent --autostart-status      # وضعیت
```

## قدم ۱۰: عیب‌یابی سریع

| علامت | علت و راه‌حل |
|---|---|
| مرورگر به `127.0.0.1:8765` وصل نمی‌شود | ایجنت اجرا نیست → اجرایش کن (قدم ۵). |
| `UNAUTHORIZED` / 401 | توکن اشتباه است → `--print-token` بگیر و درست بچسبان. |
| `FORBIDDEN_ORIGIN` / 403 | آدرس سایت در `trustedOrigins` نیست → قدم ۸-۲. |
| `PRINTER_CONNECTION_FAILED` | آی‌پی/پورت پرینتر اشتباه است، یا هم‌شبکه نیستند → `ping` بزن. |
| `PRINTER_TIMEOUT` | پرینتر خاموش است یا فایروال جلوی پورت 9100 را گرفته. |
| پرینتر USB در لیست نیست | شرط سیستم‌عامل (قدم ۷) انجام نشده. |
| چاپ شد ولی خرچنگ‌قورباغه (؟؟؟) | بایت‌های ESC/POS را اشتباه ساخته‌ای (مشکل از ایجنت نیست؛ ایجنت بایت را دست نمی‌زند). اول با Test Print کنسول امتحان کن. |
| پورت 8765 اشغال است | `--port 8766` بده (و در سایت هم همین پورت). |

لاگ ایجنت (همان ترمینالی که در آن اجراست) همیشه خطا را با جزئیات نشان می‌دهد.

## دستورات پرکاربرد (Cheat Sheet)

```bash
novex-printer-agent                         # اجرا
novex-printer-agent --print-token           # نمایش توکن
novex-printer-agent --port 8766             # پورت دیگر
novex-printer-agent --version               # نسخه
novex-printer-agent --install-autostart     # استارت خودکار
curl http://127.0.0.1:8765/health           # تست سلامت
# کنسول وب: http://127.0.0.1:8765/
```

قدم بعدی برای حرفه‌ای شدن: [`docs/API.md`](API.md) (همه endpointها)،
[`docs/USB.md`](USB.md) (جزئیات USB هر سیستم‌عامل)،
[`docs/INSTALL.md`](INSTALL.md) (ساخت نصاب/پکیج).
