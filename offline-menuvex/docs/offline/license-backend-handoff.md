# قرارداد و handoff بک‌اند لایسنس نسخهٔ دسکتاپ آفلاین منووکس

- **وضعیت:** قرارداد هماهنگی کلاینت Go با بک‌اند؛ وجود این فایل به‌معنی deploy یا فعال‌بودن endpointها نیست.
- **تاریخ قرارداد:** ۳۰ مهر ۱۴۰۵ (`2026-10-22`).
- **مصرف‌کننده:** اپ دسکتاپ Go + HTMX در `offline-menuvex/`.
- **مرجع تمدید:** `https://menuvex.ir/pricing#plans` — آقای غفاری، `09101486607`، `tel:+989101486607`.

## مرز مسئولیت

بک‌اند منبع حقیقت پرداخت، پلن، انقضا، revoke، شمار دستگاه، کد یک‌بارمصرف، rate limit و audit است. وب checkout و renewal را انجام می‌دهد؛ اپ Go داخل خودش قیمت، مدت، plan code، expiry یا دستگاه قبلی را تعیین/revoke نمی‌کند. اپ فقط کد را برای اتصال اولیه می‌گیرد، فایل دستگاه را refresh می‌کند و تا `expires_at` معتبر محلی کار می‌کند.

تا وقتی این endpoint‌ها در OpenAPI بک‌اند ثبت و feature flag سمت سرور فعال نشده‌اند، کلاینت باید fail closed بماند. `COFFISH_OFFLINE_LICENSES_ENABLED` در اپ نیز پیش‌فرض خاموش است.

## رمزنگاری و ترتیب اعتبارسنجی

ترتیب قراردادی:

```text
claims JSON -> compact JWS (alg=EdDSA, Ed25519, kid) -> age encrypt for this device's X25519 recipient
```

کلاینت برای هر نصب یک age X25519 identity تازه می‌سازد. private identity، refresh token و نشانگر آخرین زمان مشاهده‌شده فقط در OS credential store می‌مانند؛ ciphertext با permission محدود در data directory قرار می‌گیرد. private signing key یا symmetric decrypt key هیچ‌گاه در کلاینت نیست.

ترتیب اجباری کلاینت:

1. age ciphertext را با identity همان دستگاه decrypt کند.
2. compact JWS را parse کند.
3. `kid` را در مجموعهٔ public keyهای pinned/runtime پیدا کند و امضای Ed25519 را verify کند.
4. فقط بعد از verify، claims را decode و validate کند.

فایل format canonical برابر `age+compact-jws-ed25519-v1` است. fingerprint دستگاه در این نسخه `sha256:` به‌همراه SHA-256 رشتهٔ canonical `age_recipient` است. اگر backend الگوریتم fingerprint یا format دیگری لازم دارد، این فایل و fixture مشترک باید با approval هر دو تیم تغییر کند؛ کلاینت fallback رمزنگاری ندارد.

## claims امضاشده

فایل JWS پس از verify باید حداقل این schema را داشته باشد:

```json
{
  "schema_version": 1,
  "iss": "https://menuvex.ir",
  "aud": "menuvex-offline-desktop",
  "jti": "license-uuid:version",
  "license_id": "uuid",
  "cafe_id": "uuid",
  "cafe_name": "نام کافه",
  "customer_phone": "+989...",
  "plan_code": "کد واقعی از کاتالوگ",
  "product_type": "offline_standalone",
  "entitlements": ["offline_pos", "local_inventory", "printing"],
  "device_id": "uuid",
  "device_public_key_fingerprint": "sha256:...",
  "issued_at": "RFC3339 UTC",
  "not_before": "RFC3339 UTC",
  "expires_at": "RFC3339 UTC",
  "license_version": 4,
  "status": "active",
  "support": {
    "website_url": "https://menuvex.ir/pricing#plans",
    "phone_display": "09101486607",
    "phone_tel": "+989101486607",
    "contact_name": "آقای غفاری"
  }
}
```

کلاینت `iss`, `aud`, `schema_version`, `product_type`, device id/fingerprint، status و تمام تاریخ‌ها را بررسی می‌کند. تاریخ باید UTC با offset صریح (در implementation فعلی `Z` یا `+00:00`) باشد و مقایسه‌ها با UTC انجام می‌شود. plan code فقط از claims verifyشده می‌آید؛ کد یا مبلغی در Go hardcode نشده است.

## endpointهای دستگاه

Base URL از فایل `.env` وب خوانده نمی‌شود. اپ تنظیم runtime می‌خواند:

- `COFFISH_LICENSE_API_BASE_URL`، یا `API_BASE_URL` در محیط اجرا؛
- مقدار production هماهنگ: `https://menuvex.ir/api/v1`؛
- development فقط روی loopback با HTTP مجاز است، مثل `http://localhost:8088/api/v1`.

در join کردن URL، suffix دوباره `/api/v1` اضافه نمی‌شود.

### فعال‌سازی اولیه

```http
POST /api/v1/offline-licenses/activate
Content-Type: application/json
Cache-Control: no-store
```

```json
{
  "activation_code": "کد یک‌بارمصرف",
  "age_recipient": "age1...",
  "device_label": "صندوق اصلی",
  "app_version": "1.0.0",
  "idempotency_key": "UUID"
}
```

`device_id` را backend بعد از bind دستگاه اختصاص می‌دهد و در response برمی‌گرداند؛ کلاینت آن را قبل از درخواست حدس نمی‌زند. retry نامطمئن همان payload و همان `idempotency_key` را در عمر همان تلاش حفظ می‌کند.

پاسخ موفق envelope استاندارد `{success,data,error}` است:

```json
{
  "success": true,
  "data": {
    "license_id": "uuid",
    "device_id": "uuid",
    "status": "active",
    "server_time": "2026-10-22T12:00:00Z",
    "license_version": 4,
    "license_file_format": "age+compact-jws-ed25519-v1",
    "license_file_name": "menuvex-license.age",
    "license_file_sha256": "hex sha256",
    "license_file_base64": "...",
    "refresh_token": "توکن همین دستگاه"
  }
}
```

کد در query string، URL، log، analytics یا فایل محلی ذخیره نمی‌شود. پس از hash و verification موفق، file/token/metadata به‌شکل failure-safe جایگزین می‌شوند. response خام مستقیماً به UI داده نمی‌شود.

### refresh و renewal

```http
GET /api/v1/offline-licenses/current
Authorization: Bearer <device refresh token>
X-Device-ID: <uuid>
Cache-Control: no-store
```

`data` شامل `status`, `server_time`, `license_version`, `expires_at`, `license_file_format`, `license_file_sha256`, `license_file_base64` و refresh token چرخش‌یافتهٔ همان دستگاه است. کلاینت فایل تازه را decrypt+verify می‌کند و فقط پس از موفقیت file و token را atomically جایگزین می‌کند. پاسخ قدیمی/نامعتبر state قبلی معتبر را خراب نمی‌کند.

دستگاه فعال برای تمدید کد تازه نمی‌خواهد؛ پس از پرداخت قطعی، همین GET فایل جدید را می‌گیرد. کد جدید فقط برای اتصال دستگاه تازه یا recovery هویت امن ازدست‌رفته است.

## lifecycle وب/ادمین

اپ Go ایجاد، فروش، قیمت‌گذاری، حذف مدیریتی یا renewal order نمی‌سازد. قرارداد مدیریتی بک‌اند:

```text
POST   /api/v1/admin/cafes/{cafe_id}/offline-licenses       {paidOrderId}
GET    /api/v1/cafe/offline-licenses/{license_id}
GET    /api/v1/admin/cafes/{cafe_id}/offline-licenses/{license_id}
DELETE /api/v1/admin/cafes/{cafe_id}/offline-licenses/{license_id} {reason}
POST   /api/v1/cafe/offline-licenses/{license_id}/renewals {planCode,gateway}
POST   /api/v1/admin/cafes/{cafe_id}/offline-licenses/{license_id}/renewals {paidOrderId}
POST   /api/v1/cafe/offline-licenses/{license_id}/activation-codes
```

ایجاد و renewal فقط paid order واقعی و `product_type=offline_standalone` را می‌پذیرند و idempotent هستند. DELETE حذف منطقی/audit و `status=revoked` است؛ invoice، order، device history و دادهٔ محلی کافه حذف نمی‌شوند. پاسخ‌های customer/admin نباید code، token، ciphertext، private key یا شمارهٔ کامل موبایل داشته باشند.

## خطاهای پایدار

کلاینت این کدها را به پیام فارسی جداگانه نگاشت می‌کند و متن خام سرور را نمایش نمی‌دهد:

```text
OFFLINE_LICENSE_CODE_INVALID
OFFLINE_LICENSE_CODE_EXPIRED
OFFLINE_LICENSE_DEVICE_LIMIT_REACHED
OFFLINE_LICENSE_RATE_LIMITED
OFFLINE_LICENSE_DEVICE_AUTH_REQUIRED
OFFLINE_LICENSE_CAFE_SCOPE_MISMATCH
OFFLINE_LICENSE_EXPIRED
OFFLINE_LICENSE_REVOKED
OFFLINE_LICENSES_NOT_ENABLED
```

network timeout/5xx/rate limit در دورهٔ معتبر، دسترسی محلی را قطع نمی‌کند. بعد از `expires_at` هیچ grace یا تمدید فرضی وجود ندارد. revoke آنلاین بعد از refresh قابل مشاهده است؛ دستگاه کاملاً آفلاین تا sync نمی‌تواند revoke تازه را بداند اما expiry و signature را رعایت می‌کند.

## سرویس محلی و قفل

اپ فقط خلاصهٔ verified را روی loopback می‌دهد:

```http
GET http://127.0.0.1:<port>/api/v1/license
```

خروجی شامل `status`, `license_id`, `cafe_name`, `plan_title`, `valid_from`, `expires_at`, `remaining_days`, `last_validated_at` و `online_refresh_available` است. `refresh_token`, activation code، ciphertext، private identity، claims خام و PII غیرلازم هرگز برنمی‌گردد. host allowlist و CSRF موجود برنامه حفظ می‌شود.

stateهای `expired`, `revoked`, `suspended`, `invalid`, `clock_rollback`, `not_yet_valid` و `not_activated` عملیات فروش و تغییر‌دهنده را fail closed می‌کنند. صفحهٔ `/license/status`، export و backup برای جلوگیری از گروگان‌گرفتن اطلاعات در دسترس می‌مانند. هیچ data wipe یا destructive migration انجام نمی‌شود.

clock rollback با مقایسهٔ `last_seen_local_utc` و نشانگر آخرین زمان مشاهده‌شده در OS keyring تشخیص داده می‌شود؛ این نشانگر برای تشخیص rollback فایل داده است، نه امضای لایسنس. این tamper detection تضمین مطلق در برابر مالک رایانه، patch باینری، rollback کامل keyring یا تغییر ساعت نیست و نباید چنین ادعایی در UI یا مستندات شود.

## کلیدهای runtime و rollout

- `COFFISH_OFFLINE_LICENSES_ENABLED`: پیش‌فرض `false` تا backend/OpenAPI و fixture staging آماده باشند.
- `COFFISH_LICENSE_PUBLIC_KEYS`: JSON map از `kid` به public Ed25519 key (base64 یا hex). حداقل کلید فعلی و کلیدهای قبلی تا پایان اعتبار فایل‌هایشان نگه داشته شوند.
- `COFFISH_LICENSE_API_BASE_URL` یا `API_BASE_URL`.
- `COFFISH_LICENSE_ISSUER`, `COFFISH_LICENSE_AUDIENCE` برای محیط‌های هماهنگ؛ پیش‌فرض همان مقادیر قرارداد.
- `COFFISH_DEVICE_LABEL`, `COFFISH_APP_VERSION`.
- `COFFISH_LICENSE_REFRESH_INTERVAL` (اختیاری؛ cadence سمت کلاینت، پیش‌فرض ۱۰ دقیقه و نه policy اعتبار لایسنس).

نبودن key یا keyring باعث پذیرش fallback یا fixture در production نمی‌شود؛ activation fail closed و با پیام قابل‌فهم رد می‌شود. هیچ secret/private key در git یا binary قرار ندارد.

## قراردادهای باز برای تأیید backend

این handoff در خود backend/OpenAPI این repository ثبت نشده است. قبل از فعال‌سازی production باید موارد زیر با fixture مشترک تأیید شوند: `kid` و public keyهای staging/production، الگوریتم دقیق fingerprint age recipient، status code و envelope نهایی، policy/tolerance clock rollback، چرخش token و پاسخ revoke/expired. تا آن زمان این implementation فقط adapter واقعی feature-flag شده و تست cross-language آن در staging اجرا نشده است.
