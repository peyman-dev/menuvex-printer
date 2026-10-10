# پرامپت کامل — ایجنتِ فرانت: انتخابِ منبعِ داده بر اساس `menova-print-method` + قراردادِ داده‌های Agent + حالتِ Agentِ خاموش

> این پرامپت را به ایجنتِ فرانت (ایجنتِ پروژهٔ سایت/اپ سفارش‌گیری منویکس — همان «فرانت») بده.
> ایجنت به کل پروژه دسترسی دارد و می‌داند چه چیز به چه چیز وصل است.

> **این پرامپت در ادامهٔ `docs/frontend-agent-prompt.md` است — همان پرامپتِ اولیهٔ اینتگراسیونِ Agent در فرانت. این یکی، «منبعِ داده» و «قراردادِ داده» را مشخص می‌کند.**

---

## ۱. موضوع — «چه منبعِ داده‌ای؟»

یک تنظیمِ انتخابِ روشِ چاپ وجود دارد: **`menova-print-method`** (همان تنظیمِ روشِ چاپِ کاربر/مجموعه).

| مقدارِ `menova-print-method` | معنی | منبعِ داده |
|---|---|---|
| `agent` | چاپ از طریقِ **دستیارِ MenuVex** (Agentِ دسکتاپ — «MenuVex Printer Agent») | **داده‌های Agent** (بخش ۲) — **نه بک‌اندِ پروژه** |
| QZ Tray | چاپ از طریقِ QZ Tray (مرورگر → اپِ QZ Tray) | **بک‌اندِ پروژه** (داده‌های خودِ سایت — مثلاً WooCommerce) + مسیرِ QZ Tray |

**قانونِ اصلی:**

- تا وقتی `menova-print-method == "agent"` است، فرانت باید از داده‌هایی که **Agent می‌دهد** استفاده کند — فهرستِ پرینترها، ایستگاه‌ها، و خودِ چاپ — **نه بک‌اندِ پروژه**.
- وقتی کاربر از **QZ Tray** استفاده می‌کند، از **بک‌اندِ پروژه** استفاده کن (داده‌های خودِ سایت) — و مسیرِ QZ Tray دقیقاً مثلِ قبل کار می‌کند.
- این انتخاب باید **زنده** باشد: اگر مقدارِ تنظیم عوض شد، منبعِ داده هم عوض می‌شود (خودکار، یا با رفرش).

---

## ۲. قراردادِ داده — Agent دقیقاً چه می‌دهد؟

Agent (دستیارِ دسکتاپ) از طریق **SDK** (`sdk/` در ریپوی `menuvex-printer` — کلاسِ `PrinterAgentClient`) با WebSocket به `127.0.0.1` وصل می‌شود (HMAC + secret).

### ۲.۱. فهرستِ پرینترها — `client.getPrinters()` → `printers.list`

یک **JSON Array** — یک آبجکت برای هر پرینترِ پیکربندی‌شده:

```json
[
  {
    "id": "printer:7be80e8c-3d6a-42cf-973f-750ab6d3dd45",
    "name": "صندوق فروش",
    "connection": { "type": "network", "host": "192.168.1.50", "port": 9100 },
    "paperMm": 80,
    "widthDots": 576,
    "copies": 1,
    "cut": true,
    "fontFamily": "Noto Sans Arabic",
    "fontSize": 24,
    "rawPassthrough": true,
    "status": "online"
  }
]
```

| فیلد | توضیح |
|---|---|
| `id` | `printer:<uuid>` — **IDِ خام** — برای چاپ دقیقاً همین را بفرست |
| `name` | نامِ انتخاب‌شده — **دقیقاً همان‌طور که هست** (نام = شناسهٔ پرینتر؛ هرگز تغییر نده) |
| `connection` | شکلِ compat — پایین‌تر |
| `paperMm` / `widthDots` / `copies` / `cut` / `fontFamily` / `fontSize` | تنظیماتِ چاپ |
| `rawPassthrough` | bool — همیشه هست |
| `forceRaw` | bool — فقط وقتی `true` باشد می‌آید |
| `usbFallbackTarget` | فقط برای پرینترِ USB با مسیر جایگزین — وگرنه اصلاً نیست |
| `status` | `online` \| `offline` \| `unknown` \| `busy` \| `error` — زنده (`busy` یعنی همین الان یک job روی آن در حال چاپ است) |

**`connection` — سه شکل (compat):**

```json
// network:
{ "type": "network", "host": "192.168.1.50", "port": 9100 }

// spooler — به شکلِ reserved USB (برای سازگاری با فرانت‌های قدیمی):
{ "type": "usb", "vendorId": 0, "productId": 0, "serial": "queue:POS-80C",
  "bus": 0, "ports": [], "interface": 0, "endpoint": 0, "alternate": 0 }
// یعنی: نامِ صف داخلِ serial بعد از "queue:" است و vendorId=0 یعنی «USBِ فیزیکی نیست».
// با spoolerQueueName() (از sdk/src/compat.ts) نامِ واقعیِ صف را از هر دو شکل دربیار.

// usb:
{ "type": "usb", "vendorId": 1155, "productId": 22339, "serial": "ABC123",
  "bus": 1, "ports": [1], "interface": 0, "endpoint": 1, "alternate": 0 }
```

**نکته:** آرایه **همهٔ پرینترهای پیکربندی‌شده** را دارد — نه فقط متصل‌ها. هر کدام `status` زندهٔ خودش را دارد. فرانت باید **همه را نشان بدهد** و «متصل» را با `status` مشخص کند.

### ۲.۲. به‌روزرسانیِ زنده — PUSH (نیازی به poll نیست)

- Agent هر **۱۰ ثانیه** وضعیتِ همهٔ پرینترها را probe می‌کند؛ وقتی وضعیت عوض شود، event می‌فرستد: `{"type":"printer.status","version":1,"printerId":"...","status":"online"}` — سرورِ WS آن را به **همهٔ کلاینت‌های متصل** می‌فرستد.
- SDK: `client.onPrinterStatus(cb)` → `{ printerId, status }` — **subscribe کن، poll نکن.**
- Eventهای job هم زنده می‌آیند: `print.queued` / `print.printing` / `print.completed` / `print.failed` / `print.cancelled` + خودِ job → `client.onPrintJob(cb)`.
- `{"type":"resync","version":1}` → SDK خودش `getPrinters()` + `getQueue()` را refresh می‌کند.
- وضعیتِ اتصال: `client.onAgentStatus(cb)` → `'connected' | 'disconnected' | 'connecting' | 'unauthorized' | 'error'` — و `client.getConnectionState()` / `client.isConnected()`.

### ۲.۳. سایر داده‌ها

| متد | داده |
|---|---|
| `client.getStatus()` → `agent.status` | `{ ready, agentVersion, port, routes: [{ role, printerId, autoPrint }], serverError }` — **routes برای چاپِ خودکار**; `role` نامِ ایستگاه است — دقیقاً همان‌طور که هست |
| `client.getQueue()` → `queue.list` | فهرستِ jobها (`PrintJob`) — وضعیت‌ها: `queued` / `printing` / `completed` / `failed` / `cancelled` |
| `client.getJob(jobId)` → `print.status` | یک job |
| `client.print({ printerId, jobId, document })` → `print` | **چاپ** — `printerId` همان IDِ خام است. `document` می‌تواند `invoice` / `receipt` / `html` یا **`escpos`** (سندِ خام — با `escposDocument(bytes)`) باشد — فاکتورها با دیزاینِ خودشان می‌آیند؛ آن‌ها را با `escposDocument` بفرست و **هرگز** client-side render یا تغییرشان نده |
| `client.getInstalledPrinters()` → `printers.installed` | صف‌هایِ سیستم‌عامل (`{ queueName }`) — برای انتخابِ spooler |
| `client.discoverNetwork()` → `discover.network` | پرینترهایِ شبکهٔ LAN (`{ host, port }`) |
| `client.savePrinter(printer)` → `printer.save` | ذخیره/ویرایشِ پروفایلِ پرینتر — **نکته**: تنظیماتِ local-only (raw passthrough، USB fallback) را Agent نادیده می‌گیرد و مقدارِ ذخیره‌شده را نگه می‌دارد — وب‌سایت نمی‌تواند آن‌ها را تغییر دهد |
| `client.getPrinter(printerId)` → `printer.get` | یک پرینتر |
| `client.clearQueue()` → `queue.clear` | `{ cancelled, removed }` |

---

## ۳. اگر Agent بسته بود / خاموش بود

وقتی `menova-print-method == "agent"` است ولی Agent **بسته / خاموش / وصل نیست**:

- **هیچ خطایِ فنیِ خام** (ID، UUID، خطای شبکه/WS) نشان نده.
- یک **empty stateِ دوستانه** نشان بده:
  - «**دستیارِ MenuVex (Printer Agent) خاموش است — لطفاً آن را باز کنید.**» — و فهرست: «**هیچی پیدا نشد**».
- وضعیت را با `client.getConnectionState()` / `client.onAgentStatus` تشخیص بده (`disconnected` / `error` → Agent خاموش است).
- **QZ Tray باید همیشه کار کند** — کاربر می‌تواند به QZ Tray سوییچ کند (و برعکس). دو مسیرِ کاملاً مستقل — هیچ‌کدام دیگری را خراب نکن.
- وقتی Agent وصل شد (`connected`) — داده‌ها خودکار sync شوند.

---

## ۴. قوانین

1. **منبعِ داده = `menova-print-method`**: `agent` → داده‌های Agent (نه بک‌اندِ پروژه)؛ QZ Tray → بک‌اندِ پروژه + مسیرِ QZ Tray.
2. **داده‌ها دقیقاً همان‌طور که هستند** — نام = شناسه؛ هیچ پیشوند، ترجمه، یا کوتاه‌سازی.
3. **IDِ خام** برای چاپ (`printer:<uuid>`) — هرگز IDِ display/پیشوندی.
4. **PUSH را ترجیح بده** — `onPrinterStatus` / `onPrintJob` / `onAgentStatus` + `resync` — poll نکن مگر لازم باشد.
5. **Agentِ خاموش** → پیامِ دوستانه («دستیار خاموش است — بازش کنید») + empty state («هیچی پیدا نشد») — بدونِ خطایِ فنی.
6. **QZ Tray دست‌نخورده** — همیشه کار می‌کند؛ با بک‌اندِ پروژه.
7. **سندهایِ خام (escpos) هرگز به رندرِ fallback نروند** — اگر گیتِ RAWِ کاربر خاموش است، خطای `RAW_PASSTHROUGH_DISABLED` را با پیامِ مناسب نشان بده.
8. **سازگاریِ قبلی** — routeهای legacy (`invoice`/`kitchen`/`bar`) با برچسب‌های فارسیِ قبلی (صندوق/آشپزخانه/بار) نمایش داده می‌شوند.

---

## ۵. تست

- `menova-print-method = agent` + Agent باز → فهرستِ پرینترها با داده‌های Agent می‌آید (همه، با statusِ زنده) — نه بک‌اندِ پروژه.
- `menova-print-method = qz` → داده‌های بک‌اندِ پروژه + QZ Tray کار می‌کند.
- تغییرِ مقدارِ تنظیم → منبعِ داده عوض می‌شود.
- Agent را ببند → پیامِ «دستیار خاموش است — بازش کنید» + «هیچی پیدا نشد» — بدونِ خطایِ فنی؛ QZ Tray همچنان کار می‌کند.
- Agent را باز کن → داده‌ها خودکار sync می‌شوند.
- فاکتور با دیزاینِ خودش (escpos) → با `escposDocument` چاپ می‌شود — RAW (با گیت‌های روشنِ کاربر).
