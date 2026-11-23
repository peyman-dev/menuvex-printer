# پرامپت یکپارچه‌سازی سیستم پرینتر منووکس در اپلیکیشن آفلاین

> **این پرامپت را به ایجنت (Agent) توسعه‌دهنده اپلیکیشن آفلاین منووکس بدهید.**

---

## هدف

سیستم پرینتر منووکس (MenuVex Printer Agent) باید به‌صورت کامل و embed‌شده در اپلیکیشن آفلاین منووکس یکپارچه شود. **دیگر نسخه آنلاین، PAIR کردن با مرورگر کروم، WebSocket loopback به مرورگر و HMAC challenge-response با PWA وجود ندارد.** تمام عملیات چاپ مستقیماً داخل خود اپلیکیشن آفلاین انجام می‌شود.

سورس کد پرینتر از ریپازیتوری `menuvex-printer` خوانده می‌شود و شما باید آن را به‌عنوان یک ماژول داخلی اپلیکیشن آفلاین بیاورید.

---

## ساختار سورس کد پرینتر

سورس کد در ریپازیتوری `menuvex-printer` قرار دارد. ساختار اصلی:

```
menuvex-printer/
├── src-tauri/                    # بک‌اند Rust (هسته اصلی پرینت)
│   ├── Cargo.toml                # وابستگی‌ها: tokio, rusqlite, cosmic-text, rusb, keyring, ...
│   └── src/
│       ├── lib.rs                # ماژول‌های اصلی
│       ├── main.rs               # نقطه شروع اپ Tauri (شامل tray icon, IPC commands)
│       ├── config/
│       │   ├── mod.rs            # ساختار Config (port, printers, routes, raw_passthrough)
│       │   └── storage.rs        # SQLite storage (WAL, FULL sync, schema version 1)
│       ├── error.rs              # AgentError با code, message, retryable, uncertain, printer, actionRequired
│       ├── print/
│       │   ├── mod.rs            # Renderer اصلی (cosmic-text + Swash rasterization → ESC/POS raster)
│       │   ├── escpos.rs         # ESC/POS encoder (initialize, bold, align, raster, QR, barcode, cut, ...)
│       │   ├── layout.rs         # طراحی رسید: فاکتور و رسید متنی (RTL فارسی, اعداد فارسی, money grouping)
│       │   └── queue.rs          # صف چاپ SQLite (enqueue, claim, dedup, backoff, retry)
│       ├── printers/
│       │   ├── mod.rs            # Printer profile, Connection enum (Network/Spooler/Usb), Transport trait, HardwareTransport
│       │   ├── network.rs        # چاپ TCP روی پورت 9100 (private IPv4 فقط)
│       │   ├── spooler.rs        # چاپ از طریق صف OS (Windows winspool RAW / CUPS lp -o raw)
│       │   ├── usb.rs            # چاپ مستقیم USB با libusb (feature-gated)
│       │   ├── raw_printer.rs    # Win32 RAW spooler path + Generic/Text Only queue creation
│       │   ├── discovery.rs      # کشف خودکار پرینتر شبکه (LAN scan port 9100) و USB
│       │   └── usb_policy.rs     # سیاست failover برای USB
│       ├── protocol.rs           # پروتکل: Document (Invoice/Receipt/Escpos), Command, validation
│       ├── security.rs           # HMAC-SHA256، origin validation، keyring secret management
│       ├── server.rs             # WebSocket server روی loopback (برای ارتباط با PWA — در نسخه آفلاین حذف شود)
│       └── state.rs              # State management, dispatch, worker, monitor
├── sdk/                          # TypeScript SDK (برای PWA — در نسخه آفلاین مستقیماً استفاده نمی‌شود)
│   └── src/
│       ├── types.ts              # Zod schemas: document, connection, printer, job, error
│       ├── protocol.ts           # Wire protocol version 1
│       ├── client.ts             # PrinterAgentClient (WebSocket client — حذف در آفلاین)
│       ├── credentials.ts        # WebCrypto credential store (حذف در آفلاین)
│       ├── providers.ts          # MenuVexAgentPrinter, MigratingPrinterProvider
│       ├── compat.ts             # spoolerQueueName helper
│       ├── routing.ts            # Browser routing store (حذف در آفلاین)
│       └── agent.ts              # Singleton client
├── src/                          # فرانت‌اند React (UI اپ Tauri)
│   ├── App.tsx                   # UI اصلی: لیست پرینترها، صف چاپ، تنظیمات
│   ├── lib/agent.ts              # Tauri IPC API bindings
│   └── components/               # PrinterCard, PrinterList, Settings, ConnectionStatus
└── docs/                         # مستندات کامل
    ├── architecture.md           # معماری و flow
    ├── protocol.md               # پروتکل v1 (کامل)
    ├── escpos.md                 # ESC/POS و رندر فارسی
    ├── integration.md            # راهنمای یکپارچه‌سازی
    └── frontend-agent-prompt.md  # پرامپت فرانت‌اند
```

---

## معماری هدف (اپلیکیشن آفلاین)

```
┌────────────────────────────────────────────────────────────┐
│                 اپلیکیشن آفلاین منووکس                      │
│                                                            │
│  ┌─────────────────────┐    ┌─────────────────────────┐   │
│  │    رابط کاربری       │    │     هسته بیزینس         │   │
│  │  (مدیریت پرینتر،     │    │  (سفارشات، صندوق،       │   │
│  │   صف چاپ، تنظیمات)   │    │   گزارشات، ...)         │   │
│  └────────┬────────────┘    └────────┬────────────────┘   │
│           │                           │                    │
│           │     فراخوانی مستقیم       │                    │
│           │     (بدون WebSocket)      │                    │
│           ▼                           ▼                    │
│  ┌────────────────────────────────────────────────────┐   │
│  │              ماژول پرینتر (embed شده)               │   │
│  │                                                     │   │
│  │  ┌──────────┐ ┌──────────┐ ┌───────────┐           │   │
│  │  │ protocol │ │  print   │ │  printers  │           │   │
│  │  │ (validate│ │ (render  │ │ (transport │           │   │
│  │  │  document│ │  layout  │ │  network   │           │   │
│  │  │  & queue)│ │  escpos) │ │  spooler   │           │   │
│  │  └──────────┘ └──────────┘ │  usb)      │           │   │
│  │                             └───────────┘           │   │
│  │  ┌──────────────────────────────────────────────┐   │   │
│  │  │     config + storage (SQLite)                 │   │   │
│  │  │     پرینترها، صف، routes، raw_passthrough     │   │   │
│  │  └──────────────────────────────────────────────┘   │   │
│  └────────────────────────────────────────────────────┘   │
│                                                            │
│  ┌────────────────────────────────────────────────────┐   │
│  │              سخت‌افزار پرینتر                        │   │
│  │  USB Receipt Printer │ LAN Printer (TCP:9100)       │   │
│  │  Windows/CUPS Spooler                               │   │
│  └────────────────────────────────────────────────────┘   │
└────────────────────────────────────────────────────────────┘
```

---

## تغییرات اصلی نسبت به نسخه فعلی

### ۱. حذف کامل لایه WebSocket و PWA Pairing

**چه چیزی حذف شود:**
- `src-tauri/src/server.rs` — سرور WebSocket loopback (دیگر مرورگری برای اتصال وجود ندارد)
- `sdk/src/client.ts` — کلاینت WebSocket (PrinterAgentClient)
- `sdk/src/credentials.ts` — WebCrypto credential store و IndexedDB pairing
- `sdk/src/routing.ts` — Browser routing store
- `sdk/src/providers.ts` — MigratingPrinterProvider و BrowserWebUSBPrinter
- سیستم `secret` / `pairing` / `nonce` / `serverProof` / HMAC challenge-response
- `origin_allowed()` و بررسی Origin/Host
- سیستم `epoch` و `session` management

**چرا:** در نسخه آفلاین، اپلیکیشن مستقیماً به ماژول پرینتر دسترسی دارد. نیازی به احراز هویت مرورگر، WebSocket، و pairing secret نیست.

**جایگزین:** فراخوانی مستقیم توابع Rust از طریق Tauri IPC (invoke) یا اگر اپ آفلاین هم Tauri است، مستقیماً از `State::dispatch()` استفاده کنید.

### ۲. تغییر سیستم Security

**فعلی:** کلید HMAC-SHA256 در OS keyring ذخیره می‌شود و برای اثبات هویت مرورگر استفاده می‌شود.

**جدید:**
- `src-tauri/src/security.rs` ساده‌سازی شود — فقط در صورت نیاز به رمزنگاری داخلی باقی بماند.
- کلید pairing حذف شود (دیگر مرورگری pair نمی‌شود).
- اگر اپ آفلاین خودش Tauri است، از Tauri IPC security model استفاده کنید (capability-based).
- داده‌های حساس (مثل اطلاعات پرینتر) همچنان در SQLite با دسترسی محدود ذخیره شوند.

### ۳. تغییر نقطه ورود (Entry Point)

**فعلی (`main.rs`):** اپ Tauri مستقل با tray icon، WebSocket server، و single-instance plugin.

**جدید:**
- ماژول پرینتر به‌صورت کتابخانه Rust (`menuvex_agent`) در اپ آفلاین embed شود.
- `main.rs` فعلی به‌عنوان مرجع استفاده شود ولی مستقیماً import نشود.
- Tauri commands (`local_command`, `get_config`, `save_config`, `discover_usb`, ...) باید در اپ آفلاین ثبت شوند.
- سیستم tray icon و single-instance می‌تواند در اپ آفلاین ادغام شود.

### ۴. رابط مستقیم با ماژول پرینتر

به جای اینکه PWA از طریق WebSocket به Agent وصل شود، اپ آفلاین مستقیماً فراخوانی می‌کند:

```rust
// نمونه فراخوانی مستقیم از State::dispatch
use menuvex_agent::{
    state::State,
    protocol::{Command, Document},
    printers::Printer,
};

// چاپ فاکتور
let result = state.dispatch(Command::Print {
    printer_id: "invoice-printer".into(),
    job_id: format!("order:{}:invoice", order_id),
    document: Document::Invoice {
        data: InvoiceData {
            store_name: "کافه منووکس".into(),
            order_number: "1842".into(),
            items: vec![InvoiceItem {
                name: "اسپرسو".into(),
                quantity: 2,
                unit_price: 120_000,
            }],
            total: 240_000,
            currency: "تومان".into(),
            // فیلدهای اختیاری...
            ..Default::default()
        },
    },
});
```

---

## ماژول‌هایی که باید بدون تغییر منتقل شوند

### هسته پرینت (بدون تغییر)
- **`src-tauri/src/print/mod.rs`** — Renderer: cosmic-text + rustybuzz + Swash → monochrome raster → ESC/POS
- **`src-tauri/src/print/escpos.rs`** — ESC/POS encoder (initialize, bold, align, size, text, raster, QR, barcode, feed, cut, drawer)
- **`src-tauri/src/print/layout.rs`** — طراحی رسید فارسی (invoice/receipt layout, RTL, money grouping, Persian digits)
- **`src-tauri/src/print/queue.rs`** — صف چاپ SQLite (enqueue, claim, dedup, backoff, cancel, clear)

### پروفایل پرینتر و Transport (بدون تغییر)
- **`src-tauri/src/printers/mod.rs`** — Printer profile, Connection enum, Transport trait, HardwareTransport
- **`src-tauri/src/printers/network.rs`** — TCP/9100 (private IPv4 LAN فقط)
- **`src-tauri/src/printers/spooler.rs`** — OS spooler (Windows winspool RAW / CUPS `lp -o raw`)
- **`src-tauri/src/printers/usb.rs`** — libusb direct USB (feature-gated `libusb`)
- **`src-tauri/src/printers/raw_printer.rs`** — Win32 RAW spooler + Generic/Text Only creation
- **`src-tauri/src/printers/discovery.rs`** — LAN scan + USB discovery
- **`src-tauri/src/printers/usb_policy.rs`** — USB failover policy

### پیکربندی و ذخیره‌سازی (بدون تغییر)
- **`src-tauri/src/config/mod.rs`** — Config struct (port, printers, routes, raw_passthrough)
- **`src-tauri/src/config/storage.rs`** — SQLite (WAL, FULL sync, schema v1, crash recovery)

### پروتکل و Validation (بدون تغییر)
- **`src-tauri/src/protocol.rs`** — Document types (Invoice/Receipt/Escpos), Command parsing, validation, ESC/POS signatures

### خطاها (بدون تغییر)
- **`src-tauri/src/error.rs`** — AgentError با retryable/uncertain/printer/actionRequired

### فونت (بدون تغییر)
- **`src-tauri/assets/NotoSansArabic-Regular.ttf`** — فونت فارسی embedded

---

## ماژول‌هایی که نیاز به تغییر دارند

### `src-tauri/src/state.rs` — تغییرات جزئی
- `State::new()` همچنان `Storage`, `Secret`(ساده‌شده), و `Transport` می‌گیرد.
- متد `dispatch()` بدون تغییر باقی می‌ماند.
- `worker()` (صف چاپ background) و `monitor()` (printer status probing) بدون تغییر باقی می‌مانند.
- سیستم `events` (broadcast channel) حفظ شود — UI اپ آفلاین از این events برای به‌روزرسانی وضعیت استفاده می‌کند.

### `src-tauri/src/security.rs` — ساده‌سازی
- کلید pairing حذف شود.
- `origin_allowed()` حذف شود.
- اگر نیاز به رمزنگاری داخلی هست، فقط توابع utility باقی بمانند.
- `Secret::load()` می‌تواند ساده‌تر شود یا حذف شود.

### `src-tauri/src/main.rs` — ادغام در اپ آفلاین
- Tauri commands (`local_command`, `get_config`, `save_config`, `discover_usb`, ...) در اپ آفلاین ثبت شوند.
- Tray icon و single-instance plugin می‌توانند ادغام شوند.
- Background tasks (`worker`, `monitor`) در setup اپ آفلاین spawn شوند.

---

## پروتکل اسناد (Document Protocol) — بدون تغییر

### فاکتور (Invoice)
```json
{
  "type": "invoice",
  "data": {
    "storeName": "نام فروشگاه",
    "orderNumber": "1842",
    "items": [
      { "name": "نام کالا", "quantity": 1, "unitPrice": 240000 }
    ],
    "total": 240000,
    "footer": "با سپاس",
    "title": "فاکتور فروش",
    "address": "آدرس فروشگاه",
    "phone": "02112345678",
    "date": "۱۴۰۵/۰۹/۰۲ ۱۴:۳۰",
    "status": "تکمیل‌شده",
    "orderType": "حضوری",
    "table": "۳",
    "note": "بدون شکر",
    "currency": "تومان",
    "subtotal": 240000
  }
}
```

**فیلدهای اجباری:** `storeName`, `orderNumber`, `items` (حداقل ۱ آیتم), `total`
**فیلدهای اختیاری:** `footer`, `title`, `address`, `phone`, `date`, `status`, `orderType`, `table`, `note`, `currency`, `subtotal`

**محدودیت‌ها:**
- `storeName`: حداکثر ۳۰۰ کاراکتر
- `orderNumber`: حداکثر ۱۲۸ کاراکتر
- `items`: ۱ تا ۱۰۰ آیتم
- هر item: `name` (۱-۳۰۰ کاراکتر), `quantity` (۱-۹۹۹۹), `unitPrice` (۰-۹۰۰,۰۰۰,۰۰۰)
- `total`: ۰ تا ۹,۰۰۰,۰۰۰,۰۰۰,۰۰۰
- کاراکترهای کنترلی (غیر از `\n`) ممنوع

### رسید متنی (Receipt)
```json
{
  "type": "receipt",
  "lines": [
    "[center] کافه منووکس",
    "----------------",
    "سفارش: ۱۸۴۲",
    "اسپرسو | ۲ | ۲۴۰,۰۰۰",
    "",
    "جمع: ۲۴۰,۰۰۰ تومان"
  ]
}
```

**قوانین layout:**
- حداکثر ۱۰۰ خط، هر خط حداکثر ۵۰۰ کاراکتر
- `[center] text` — خط وسط‌چین
- `---` (حداقل ۳ کاراکتر خط) — جداکننده پیکسلی
- `cell1 | cell2 | cell3` — ستون‌ها (راست به چپ، ۲ تا ۴ ستون)
- خط خالی — فاصله عمودی

### ESC/POS خام (Raw)
```json
{
  "type": "escpos",
  "commands": [27, 64],
  "data": "<base64 ESC/POS bytes>"
}
```

**قوانین:**
- باید با یک ESC/POS command شناخته‌شده شروع شود (ESC @, GS v 0, ...)
- حداکثر ۱ مگابایت decoded
- نیاز به فعال‌سازی `rawPassthrough` توسط اپراتور در تنظیمات محلی
- Agent هیچ رندر، فونت، یا دستوری اضافه نمی‌کند — بایت‌ها verbatim ارسال می‌شوند

---

## انواع اتصال پرینتر (Connection Types)

### ۱. Network (TCP/9100)
```json
{ "type": "network", "host": "192.168.1.50", "port": 9100 }
```
- فقط private IPv4 (RFC 1918)
- بدون DNS — فقط IP address مستقیم
- timeout اتصال: ۳ ثانیه، timeout نوشتن: ۳۰ ثانیه

### ۲. Spooler (OS Print Queue)
```json
{ "type": "spooler", "queueName": "POS-80" }
```
- Windows: `OpenPrinterW` / `WritePrinter` با `pDatatype = "RAW"`
- Linux/macOS: `lp -d <queue> -o raw`
- **توصیه‌شده برای پرینترهای USB** — درایور فروشنده حفظ می‌شود
- لیست صف‌ها: `printers.installed` → `[{queueName}]`

### ۳. USB مستقیم (libusb)
```json
{
  "type": "usb",
  "vendorId": 1234,
  "productId": 5678,
  "serial": null,
  "bus": 1,
  "ports": [1],
  "interface": 0,
  "endpoint": 1,
  "alternate": 0
}
```
- Feature-gated (`libusb` feature)
- کشف خودکار: `discover_usb` Tauri command
- USB failover policy: فقط `PreOpen` / `PostOpenPreWrite` به fallback می‌رود

### سازگاری Spooler Descriptor
در `printers.list` و `printer.get`، spooler به‌صورت USB descriptor سازگار منتشر می‌شود:
```json
{
  "type": "usb",
  "vendorId": 0,
  "productId": 0,
  "serial": "queue:POS-80",
  "bus": 0,
  "ports": [],
  "interface": 0,
  "endpoint": 0,
  "alternate": 0
}
```
**همیشه قبل از بررسی `connection.type`، از `spoolerQueueName(connection)` استفاده کنید.**

---

## سیستم صف چاپ (Print Queue)

### ویژگی‌ها
- **SQLite** با WAL mode و FULL synchronous
- **Deduplication**: همان `jobId` + همان `printerId` + همان `document` → نتیجه قبلی برمی‌گردد
- **تضاد**: همان `jobId` با `printerId` یا `document` متفاوت → خطای `JOB_ID_CONFLICT`
- **Snapshot**: پروفایل پرینتر در job ذخیره می‌شود — تغییرات بعدی config روی jobهای قبلی اثر نمی‌گذارد
- **Crash recovery**: jobهایی که هنگام crash در وضعیت `printing` بودند، `failed` با `uncertain: true` می‌شوند
- **Backoff**: ۱ ثانیه → حداکثر ۶۰ ثانیه
- **حداکثر**: ۲۵۶ job فعال، ۵۰۰ job در لیست
- **Worker**: یک worker سریالی — همه چاپ‌ها ترتیبی

### Job ID Convention
```
order:<backend-ID>:invoice      # فاکتور اصلی
order:<backend-ID>:kitchen      # بلیط آشپزخانه
order:<backend-ID>:bar          # بلیط بار
test:<uuid>                     # تست پرینت
raw-test:<timestamp>            # تست RAW
```

**هرگز از timestamp یا random ID برای retry استفاده نکنید.** فقط از همان `jobId` اصلی استفاده کنید.

### وضعیت‌ها
- `queued` → `printing` → `completed` / `failed`
- `cancelled` — فقط jobهای `queued` قابل لغو هستند
- `completed` = تحویل به transport (نه تأیید چاپ فیزیکی!)

---

## رندر فارسی و ESC/POS

### Pipeline
```
UTF-8 → cosmic-text → rustybuzz (Arabic shaping + BiDi) → Swash rasterization
→ alpha threshold (100/255) → 1-bit monochrome → GS v 0 raster stripes (≤128 rows each)
→ feed + optional cut
```

### طراحی فاکتور
- **هدر وسط‌چین**: نام فروشگاه ۳۲% بزرگ‌تر و bold
- **آدرس و تلفن**: ۸۴% اندازه پایه
- **ردیف فیش**: `title` (پیش‌فرض `فاکتور فروش`) سمت راست، `فیش <orderNumber>` سمت چپ
- **جزئیات**: `تاریخ`, `وضعیت`, `نوع سفارش`, `میز` — برچسب راست، مقدار چپ
- **جدول اقلام** (۸۰ میلی‌متر ≥ ۴۶۴ dots): ۴ ستون — `شرح کالا | تعداد | قیمت واحد | جمع`
- **شمارنده‌ها**: `تعداد اقلام` و `جمع اقلام` (فقط اگر `subtotal` ارسال شده)
- **مبلغ قابل پرداخت**: ۲۰% بزرگ‌تر و bold
- **یادداشت**: وسط‌چین بین دو خط‌چین پیکسلی
- **فوتر**: وسط‌چین و bold
- **برند**: `POWERED BY MENUVEX.IR` در انتها با ۶۶% اندازه

### مقادیر عددی
- اعداد فارسی: `240000` → `۲۴۰,۰۰۰`
- گروه‌بندی هزارگان با `,`
- **ارزش هرگز تبدیل، گرد یا محاسبه نمی‌شود** — فقط نمایش
- `currency` (مثلاً `تومان`) فقط اگر adapter ارسال کرده باشد

### جداکننده‌ها
- **پیکسل** (نه glyph فونت) — U+2500 در Noto Sans Arabic وجود ندارد!
- ضخامت: `fontSize / 12` (حداقل ۱)
- Dashed: ۸ پیکسل روشن، ۵ پیکسل خاموش

### محدودیت‌ها
- حداکثر ۴۰۹۶ سطر پیکسلی
- عرض کاغذ: `widthDots` (128-832 dots، مضرب ۸)
- حاشیه: ۲% عرض (۲-۲۴ dots)
- فونت: `Noto Sans Arabic Regular` embedded (`src-tauri/assets/`)

---

## Raw Passthrough (ESC/POS خام)

### گیت‌ها
1. **Global gate**: `config.raw_passthrough.enabled` (پیش‌فرض: `true`)
2. **Per-printer gate**: `printer.raw_passthrough` (پیش‌فرض: `false`)
3. هر دو باید فعال باشند وگرنه `RAW_PASSTHROUGH_DISABLED` برگردانده می‌شود

### رفتار
- Agent **هیچ** رندر، shaping، فونت، `ESC @`، feed یا cut اضافه نمی‌کند
- بایت‌ها verbatim به پرینتر ارسال می‌شوند
- Windows: `pDatatype = "RAW"` از spooler
- `force_raw`: override برای Win32 spooler datatype

### خطاها
- `RAW_PASSTHROUGH_DISABLED` — اپراتور باید در تنظیمات فعال کند
- `RAW_ESC_POS_INVALID` — base64 نامعتبر، بایت خالی، یا signature ناشناخته
- `RAW_PAYLOAD_TOO_LARGE` — بیش از limit (1 KiB تا 1 MiB)

---

## Printer Routes (مسیریابی چاپ)

Routes نقش‌های ایستگاه چاپ را به پرینترها متصل می‌کنند:
```json
{
  "role": "صندوق",
  "printerId": "printer:1",
  "autoPrint": true
}
```

- `role`: لیبل آزاد (فارسی یا انگلیسی) — یکتا
- `autoPrint`: چاپ خودکار هنگام دریافت سفارش
- **محدودیت تعداد ندارد** — هر تعداد station که نیاز باشد

---

## Tauri IPC Commands (رابط فرانت‌اند)

این commands باید در اپ آفلاین ثبت شوند:

| Command | ورودی | خروجی | توضیح |
|---------|-------|-------|-------|
| `local_command` | `{request: string}` | `Result<Value>` | اجرای دستورات پروتکل |
| `get_config` | — | `Config` | خواندن پیکربندی |
| `save_config` | `{config: Config}` | `void` | ذخیره پیکربندی |
| `discover_usb` | — | `UsbDevice[]` | کشف پرینتر USB |
| `test_connection` | `{printerId}` | `string` (status) | بررسی اتصال |
| `pairing_secret` | — | `string` | **(حذف در آفلاین)** |
| `rotate_secret` | — | `void` | **(حذف در آفلاین)** |
| `raw_printer_platform` | — | `string` | نام پلتفرم RAW |
| `raw_printer_info` | `{queueName}` | `RawPrinterInfo` | اطلاعات صف RAW |
| `create_generic_raw_target` | `{printerId, sourceQueue}` | `RawTargetCreated` | ساخت صف Generic |
| `test_raw_print` | `{printerId}` | `Value` (job) | تست چاپ RAW |

### دستورات پروتکل (از طریق `local_command`)

| Type | فیلدها | خروجی |
|------|--------|-------|
| `agent.status` | — | `{ready, agentVersion, port, routes, serverError}` |
| `printers.list` | — | `Printer[]` با status |
| `discover.network` | — | `[{host, port}]` |
| `printers.installed` | — | `[{queueName}]` |
| `printer.get` | `printerId` | `Printer` |
| `printer.save` | `printer` | `Printer` ذخیره‌شده |
| `printer.test` | `printerId, jobId` | `Job` |
| `print` | `printerId, jobId, document` | `Job` |
| `print.status` | `jobId` | `Job` |
| `queue.list` | — | `Job[]` (حداکثر ۵۰۰) |
| `queue.cancel` | `jobId` | `Job` |
| `queue.clear` | — | `{cancelled, removed}` |

---

## Worker و Monitor (Background Tasks)

### Queue Worker
- هر ثانیه صف را بررسی می‌کند
- Jobهای `queued` با `nextAt <= now` را claim می‌کند
- Render → Transport send → outcome
- Failure: backoff (۱s → ۶۰s)، حداکثر `maxAttempts` تلاش
- **هرگز terminate نمی‌شود** — فقط log و backoff
- `printing` → crash recovery → `failed` with `uncertain: true`

### Printer Monitor
- هر ۱۰ ثانیه status پرینترهای تنظیم‌شده را بررسی می‌کند
- Network: TCP probe
- Spooler: queue existence
- USB: libusb present check
- Events broadcast: `printer.status`

---

## Failure Isolation (جداسازی خطا)

- **Panic handler**: یک handler پانیک یک درخواست را fail می‌کند — WebSocket/Agent پایین نمی‌آید
- **Mutex poison recovery**: `lock()` به جای `unwrap()` — mutex مسموم بازیابی می‌شود
- **USB deadlines**: probe ۱۰s، discovery ۲۵s، print ۳۵s — هر عملیات USB thread جداگانه
- **Interface release**: USB interface در هر path آزاد می‌شود
- **Structured logging**: روزانه rolling، `RUST_BACKTRACE=1`

### خطاهای ساختاریافته
```json
{
  "code": "PRINTER_OFFLINE",
  "message": "Printer is not reachable",
  "retryable": true,
  "uncertain": false,
  "printer": "POS-80",
  "actionRequired": "Check power and cable"
}
```

### کدهای خطای مهم
| Code | توضیح |
|------|-------|
| `PRINTER_NOT_FOUND` | پرینتر تنظیم نشده |
| `PRINTER_OFFLINE` | پرینتر در دسترس نیست |
| `JOB_ID_CONFLICT` | همان ID با data متفاوت |
| `QUEUE_FULL` | ۲۵۶ job فعال |
| `INVALID_JOB` | validation ناموفق |
| `INVALID_CONFIG` | پیکربندی نامعتبر |
| `RAW_PASSTHROUGH_DISABLED` | raw printing فعال نیست |
| `RAW_ESC_POS_INVALID` | ESC/POS نامعتبر |
| `RAW_PAYLOAD_TOO_LARGE` | بیش از limit |
| `FONT_UNAVAILABLE` | فونت نصب نیست |
| `FONT_GLYPH_MISSING` | کاراکتر بدون glyph |
| `NETWORK_TIMEOUT` | timeout شبکه |
| `USB_DEVICE_ERROR` | خطای USB |
| `PRINT_OUTCOME_UNKNOWN` | نتیجه نامشخص (uncertain) |
| `UNSUPPORTED_CONNECTION_TYPE` | نوع اتصال پشتیبانی‌نشد |

---

## وابستگی‌های Rust (Cargo.toml)

```toml
[dependencies]
tauri = { version = "2", features = ["tray-icon"] }
serde = { version = "1", features = ["derive"] }
serde_json = "1"
tokio = { version = "1", features = ["rt-multi-thread", "net", "sync", "time", "macros"] }
rusqlite = { version = "0.32", features = ["bundled"] }
rusb = { version = "0.9.4", features = ["vendored"], optional = true }
keyring = { version = "3.6", features = ["apple-native", "windows-native", "sync-secret-secret"] }
cosmic-text = "0.14.2"
rand = "0.8"
base64 = "0.22"
hmac = "0.12"
sha2 = "0.10"
tracing = "0.1"
tracing-subscriber = { version = "0.3", features = ["env-filter"] }
tracing-appender = "0.2"
zeroize = "1"

# Windows فقط
[target.'cfg(windows)'.dependencies]
windows = { version = "0.62.2", features = [
    "Win32_Foundation", "Win32_Graphics_Gdi",
    "Win32_Graphics_Printing", "Win32_Security"
] }
```

**نکته:** `tokio-tungstenite` و `futures-util` (وابستگی‌های WebSocket) در نسخه آفلاین لازم نیستند.

---

## چک‌لیست پیاده‌سازی

### فاز ۱: انتقال هسته
- [ ] کپی `src-tauri/src/` (بدون `server.rs`) در اپ آفلاین
- [ ] کپی `src-tauri/assets/NotoSansArabic-Regular.ttf`
- [ ] تنظیم `Cargo.toml` با وابستگی‌های لازم
- [ ] ساده‌سازی `security.rs` (حذف pairing/HMAC)
- [ ] ثبت Tauri commands در `main.rs` اپ آفلاین
- [ ] Spawn کردن `worker()` و `monitor()` در setup

### فاز ۲: رابط کاربری
- [ ] UI مدیریت پرینترها (افزودن، ویرایش، حذف)
- [ ] UI صف چاپ (لیست jobها، لغو، پاک‌سازی)
- [ ] UI تنظیمات (port, autostart, raw passthrough)
- [ ] نمایش وضعیت پرینترها (online/offline/busy/unknown)
- [ ] دکمه تست چاپ
- [ ] کشف خودکار پرینتر (USB و Network)

### فاز ۳: یکپارچه‌سازی بیزینس
- [ ] اتصال سفارشات به سیستم چاپ
- [ ] استفاده از `routes` برای مسیریابی خودکار
- [ ] Stable job IDs از backend order IDs
- [ ] مدیریت خطاها و نمایش پیام‌های کاربرپسند
- [ ] Reconciliation بعد از reconnection/بازگشت از crash

### فاز ۴: تست
- [ ] تست چاپ فاکتور فارسی روی پرینتر واقعی
- [ ] تست چاپ رسید متنی
- [ ] تست ESC/POS خام
- [ ] تست USB failover
- [ ] تست crash recovery
- [ ] تست dedup
- [ ] تست queue overflow

---

## نکات مهم و هشدارها

1. **`completed` ≠ چاپ فیزیکی**: `completed` فقط یعنی بایت‌ها به transport تحویل داده شدند. paper out ممکن است هنوز اتفاق نیفتاده باشد.

2. **`uncertain` را جدی بگیرید**: اگر `error.uncertain == true`، یعنی ممکن است داده به پرینتر رسیده باشد. **هرگز بدون بررسی فیزیکی چاپ مجدد نکنید.**

3. **Job ID ثابت**: هرگز job ID جدید برای retry نسازید. همان ID قبلی را دوباره submit کنید — dedup خودکار نتیجه قبلی را برمی‌گرداند.

4. **عرض کاغذ**: `paperMm` فقط metadata است. `widthDots` کنترل خروجی واقعی را دارد. عرض فیزیکی رول کاغذ قابل تشخیص از طریق ESC/POS نیست.

5. **فونت**: `Noto Sans Arabic Regular` embedded است. فونت‌های سیستمی با نام profile انتخاب می‌شوند، نه مسیر فایل.

6. **جداسازی خطا**: یک پرینتر خراب نباید بقیه پرینترها یا UI را تحت تأثیر قرار دهد.

7. **USB**: در لینوکس ممکن است نیاز به `udev rules` باشد (اسکریپت `scripts/linux-usb-permissions.sh` موجود است).

8. **Windows RAW**: ترجیحاً از Generic / Text Only driver استفاده شود. Vendor driver ممکن است بایت‌ها را reinterpret کند.

9. **Spooler توصیه‌شده**: برای پرینترهای USB، اتصال `spooler` بهترین گزینه است — نیازی به تعویض درایور نیست.

10. **مستندات**: تمام `docs/` را بخوانید — به‌ویژه `protocol.md`, `escpos.md`, `architecture.md`.
