# Generated endpoint catalog

Generated from `app.openapi()`. Authentication/permissions must also be checked against registered dependencies and domain rules.

| Method | Path | Tags | Summary |
|---|---|---|---|
| GET | `/` | System | وضعیت سرویس اصلی کافیش |
| POST | `/api/auth/register` | Registration | ثبت‌نام مستقیم و استاندارد فروشگاه و مالک (بدون نیاز به پیامک OTP) |
| GET | `/api/cafe/notifications` | Cafe Profile | اعلان‌های کافه |
| PATCH | `/api/cafe/printing` | Cafe Profile | روشن/خاموش کردن استفاده از چاپگر در کل کافه |
| GET | `/api/cafe/profile` | Cafe Profile | دریافت پروفایل کافه |
| PUT | `/api/cafe/profile` | Cafe Profile | ویرایش پروفایل کافه (نام، نام کاربری، توضیح، آدرس، ویژگی‌ها، ساعت کاری، شبکه‌های اجتماعی، تم منو) |
| POST | `/api/cafe/profile/logo` | Cafe Profile | آپلود تصویر پروفایل کافه |
| GET | `/api/cafe/profile/username` | Cafe Profile | بررسی دسترس‌پذیری نام کاربری کافه |
| PUT | `/api/cafe/subscription` | Cafe Profile | تغییر پلن اشتراک |
| DELETE | `/api/cafe/working-hours` | Cafe Profile | بازنشانی ساعات کاری به حالت همیشه باز |
| GET | `/api/cafe/working-hours` | Cafe Profile | دریافت ساعات کاری کافه |
| PATCH | `/api/cafe/working-hours` | Cafe Profile | ویرایش جزئی ساعات کاری کافه |
| POST | `/api/cafe/working-hours` | Cafe Profile | تنظیم ساعات کاری کافه |
| PUT | `/api/cafe/working-hours` | Cafe Profile | ویرایش کامل ساعات کاری کافه |
| GET | `/api/cafe/working-hours/status` | Cafe Profile | وضعیت زنده کاری کافه |
| GET | `/api/online/addresses` | Online ordering | List Addresses |
| POST | `/api/online/addresses` | Online ordering | Create Address |
| PUT | `/api/online/addresses/{address_id}` | Online ordering | تغییر آدرس روی نقشه (lat/lng) |
| GET | `/api/online/cafe-plans` | Online ordering | لیست پلن‌های عضویت کافه |
| POST | `/api/online/cafe-plans` | Online ordering | ایجاد پلن عضویت کافه |
| DELETE | `/api/online/cafe-plans/{plan_id}` | Online ordering | حذف پلن عضویت کافه |
| GET | `/api/online/cafe-plans/{plan_id}` | Online ordering | جزئیات پلن عضویت کافه |
| PUT | `/api/online/cafe-plans/{plan_id}` | Online ordering | ویرایش پلن عضویت کافه |
| POST | `/api/online/cafe-plans/{plan_id}/features` | Online ordering | افزودن ویژگی به پلن عضویت |
| DELETE | `/api/online/cafe-plans/{plan_id}/features/{feature_key}` | Online ordering | حذف ویژگی پلن عضویت |
| PUT | `/api/online/cafe-plans/{plan_id}/features/{feature_key}` | Online ordering | ویرایش ویژگی پلن عضویت |
| GET | `/api/online/cafes/{username}` | Online ordering | Cafe Info |
| GET | `/api/online/cafes/{username}/club` | Online ordering | دریافت وضعیت عضویت باشگاه مشتری |
| POST | `/api/online/cafes/{username}/club` | Online ordering | عضویت مشتری در باشگاه کافه |
| POST | `/api/online/cafes/{username}/orders` | Online ordering | ثبت سفارش آنلاین مشتری |
| GET | `/api/online/cafes/{username}/plans` | Online ordering | لیست پلن‌های فعال عضویت کافه |
| POST | `/api/online/cafes/{username}/plans/{plan_id}/checkout` | Online ordering | انتخاب پلن و ایجاد درخواست پرداخت عضویت |
| POST | `/api/online/cafes/{username}/subscribe` | Online ordering | Subscribe |
| GET | `/api/online/cards` | Online ordering | کارت‌های پرداخت (مشتری یا کافه) |
| POST | `/api/online/cards` | Online ordering | ایجاد کارت پرداخت مشتری یا کافه |
| DELETE | `/api/online/cards/{card_id}` | Online ordering | حذف کارت ثبت‌شده |
| PUT | `/api/online/cards/{card_id}` | Online ordering | ویرایش کارت پرداخت مشتری یا کافه |
| DELETE | `/api/online/cart` | Online ordering | Clear Cart |
| GET | `/api/online/cart` | Online ordering | Get Cart |
| PUT | `/api/online/cart` | Online ordering | Set Cart |
| GET | `/api/online/customers` | Online ordering | مشتریانی که سفارش آنلاین ثبت کرده‌اند |
| POST | `/api/online/customers` | Online ordering | ایجاد مشتری آنلاین و عضویت باشگاه (Gym/Club) |
| POST | `/api/online/customers/gym` | Online ordering | ایجاد مشتری آنلاین در باشگاه مشتریان (Gym/Club) |
| DELETE | `/api/online/customers/gym/{consumer_id}` | Online ordering | حذف مشتری آنلاین از باشگاه مشتریان (Gym/Club) |
| GET | `/api/online/customers/gym/{consumer_id}` | Online ordering | دریافت جزئیات مشتری آنلاین در باشگاه مشتریان (Gym/Club) |
| DELETE | `/api/online/customers/{consumer_id}` | Online ordering | حذف مشتری آنلاین و لغو عضویت باشگاه (Gym/Club) |
| GET | `/api/online/customers/{consumer_id}` | Online ordering | دریافت جزئیات مشتری آنلاین و عضویت باشگاه (Gym/Club) |
| PATCH | `/api/online/customers/{consumer_id}` | Online ordering | ویرایش جزئی مشخصات مشتری آنلاین و عضویت باشگاه (Gym/Club) |
| PUT | `/api/online/customers/{consumer_id}` | Online ordering | ویرایش مشخصات مشتری آنلاین و عضویت باشگاه (Gym/Club) |
| POST | `/api/online/login` | Online ordering | ورود مشتری با شماره و رمز عبور |
| POST | `/api/online/login-otp` | Online ordering | ورود مشتری با کد تأیید |
| GET | `/api/online/me` | Online ordering | پروفایل مشتری و آدرس‌ها |
| PUT | `/api/online/me` | Online ordering | ویرایش نام مشتری |
| GET | `/api/online/membership-payments` | Online ordering | پرداخت‌های عضویت مشتریان کافه |
| POST | `/api/online/membership-payments/{payment_id}/confirm` | Online ordering | تایید پرداخت عضویت و فعال‌سازی پلن مشتری |
| GET | `/api/online/orders` | Online ordering | فهرست سفارش‌های آنلاین مشتری |
| GET | `/api/online/orders/realtime/docs` | Online ordering | قرارداد ریل‌تایم سفارش‌های آنلاین |
| GET | `/api/online/orders/stream` | Online ordering | جریان ریل‌تایم سفارش‌های آنلاین مشتری (SSE / HTTP Streaming) |
| GET | `/api/online/orders/stream/` | Online ordering | جریان ریل‌تایم سفارش‌های آنلاین مشتری (SSE / HTTP Streaming) |
| GET | `/api/online/orders/{order_id}` | Online ordering | جزئیات سفارش آنلاین مشتری |
| POST | `/api/online/otp/request` | Online ordering | ارسال کد تأیید مشتری |
| POST | `/api/online/otp/verify` | Online ordering | تأیید کد مشتری |
| POST | `/api/online/refresh` | Online ordering | نوسازی نشست مشتری |
| POST | `/api/online/register` | Online ordering | ثبت‌نام مشتری با شماره و رمز عبور |
| POST | `/api/online/reset-password` | Online ordering | بازنشانی رمز عبور مشتری |
| POST | `/api/online/reset-password/request` | Online ordering | ارسال کد تأیید بازنشانی رمز عبور مشتری |
| POST | `/api/online/signup` | Online ordering | ثبت‌نام مشتری با شماره و رمز عبور |
| GET | `/api/online/stream` | Online ordering | جریان ریل‌تایم سفارش‌های آنلاین مشتری (SSE / HTTP Streaming) |
| GET | `/api/online/stream/` | Online ordering | جریان ریل‌تایم سفارش‌های آنلاین مشتری (SSE / HTTP Streaming) |
| GET | `/api/online/subscribers` | Online ordering | اعضای باشگاه کافه |
| GET | `/api/online/tickets` | Online ordering | List Customer Tickets |
| POST | `/api/online/tickets` | Online ordering | Create Customer Ticket |
| GET | `/api/online/tickets/{ticket_id}` | Online ordering | جزئیات تیکت مشتری |
| POST | `/api/online/tickets/{ticket_id}/messages` | Online ordering | Add Customer Ticket Message |
| GET | `/api/online/wishlist` | Online ordering | Wishlist |
| POST | `/api/online/wishlist` | Online ordering | Add Wishlist |
| DELETE | `/api/online/wishlist/{item_id}` | Online ordering | Delete Wishlist |
| GET | `/api/printer-configs` | Printer Configurations | فهرست پرینترهای ثبت‌شده کافه |
| POST | `/api/printer-configs` | Printer Configurations | تعریف و افزودن پرینتر جدید |
| GET | `/api/printer-configs/category-routes` | Printer Category Routes | فهرست قوانین دسته‌بندی ← پرینتر |
| POST | `/api/printer-configs/category-routes` | Printer Category Routes | ساخت قانون دسته‌بندی ← پرینتر |
| DELETE | `/api/printer-configs/category-routes/{route_id}` | Printer Category Routes | حذف قانون |
| GET | `/api/printer-configs/category-routes/{route_id}` | Printer Category Routes | دریافت یک قانون |
| PATCH | `/api/printer-configs/category-routes/{route_id}` | Printer Category Routes | تغییر جزئی قانون |
| PUT | `/api/printer-configs/category-routes/{route_id}` | Printer Category Routes | جایگزینی کامل قانون |
| GET | `/api/printer-configs/policy` | Printer Configurations | دریافت سیاست‌های چاپ تننت |
| PATCH | `/api/printer-configs/policy` | Printer Configurations | ویرایش جزیی سیاست‌های چاپ تننت |
| PUT | `/api/printer-configs/policy` | Printer Configurations | بروزرسانی سیاست‌های چاپ تننت |
| GET | `/api/printer-configs/receipts/orders/{order_id}` | Printer Configurations | دریافت پاکت کامل فیش سفارش |
| GET | `/api/printer-configs/receipts/{order_id}/club` | Printer Configurations | دریافت فاکتور اختصاصی باشگاه مشتریان برای چاپ |
| POST | `/api/printer-configs/receipts/{order_id}/club/print` | Printer Configurations | ثبت و ارسال چاپ فاکتور اختصاصی باشگاه مشتریان |
| GET | `/api/printer-configs/receipts/{order_id}/order-completion` | Printer Configurations | دریافت فیش تکمیل سفارش برای چاپ |
| POST | `/api/printer-configs/receipts/{order_id}/order-completion/print` | Printer Configurations | ثبت و ارسال چاپ فیش تکمیل سفارش |
| GET | `/api/printer-configs/receipts/{order_id}/settlement` | Printer Configurations | دریافت فاکتور تسویه برای چاپ |
| POST | `/api/printer-configs/receipts/{order_id}/settlement/print` | Printer Configurations | ثبت و ارسال چاپ فاکتور تسویه |
| GET | `/api/printer-configs/target/club` | Printer Configurations | پرینترهای هدف برای فاکتور باشگاه مشتریان |
| GET | `/api/printer-configs/target/order-completion` | Printer Configurations | پرینترهای هدف برای تکمیل سفارش |
| GET | `/api/printer-configs/target/settlement` | Printer Configurations | پرینترهای هدف برای فاکتور تسویه |
| DELETE | `/api/printer-configs/{printer_id}` | Printer Configurations | حذف پرینتر |
| GET | `/api/printer-configs/{printer_id}` | Printer Configurations | دریافت مشخصات یک پرینتر |
| PATCH | `/api/printer-configs/{printer_id}` | Printer Configurations | ویرایش جزیی تنظیمات پرینتر |
| PUT | `/api/printer-configs/{printer_id}` | Printer Configurations | ویرایش کامل مشخصات پرینتر |
| GET | `/api/printer-configs/{printer_id}/test` | Printer Configurations | فیش آزمایشی پرینتر |
| GET | `/api/printers` | Printers | List active printers |
| POST | `/api/printers` | Printers | Create or update printer settings |
| GET | `/api/printers/receipts/{order_id}/order-completion` | Printers | دریافت فیش تکمیل سفارش برای چاپ |
| POST | `/api/printers/receipts/{order_id}/order-completion/print` | Printers | ثبت و ارسال چاپ فیش تکمیل سفارش |
| GET | `/api/printers/receipts/{order_id}/settlement` | Printers | دریافت فاکتور تسویه برای چاپ |
| POST | `/api/printers/receipts/{order_id}/settlement/print` | Printers | ثبت و ارسال چاپ فاکتور تسویه |
| DELETE | `/api/printers/{id}` | Printers | Delete a printer |
| PATCH | `/api/printers/{id}` | Printers | Partially update a printer |
| GET | `/api/printers/{role}` | Printers | Get printer settings for a role |
| PUT | `/api/printers/{role}` | Printers | Replace printer settings for a role |
| POST | `/api/register/direct` | Registration | ثبت‌نام مستقیم فروشگاه و مالک (بدون نیاز به پیامک OTP) |
| GET | `/api/v1/accounting/accounts` | Accounting | فهرست حساب‌ها |
| POST | `/api/v1/accounting/accounts` | Accounting | ایجاد حساب جدید |
| PATCH | `/api/v1/accounting/accounts/{account_id}` | Accounting | ویرایش حساب |
| GET | `/api/v1/accounting/expenses` | Accounting | فهرست هزینه‌ها |
| POST | `/api/v1/accounting/expenses` | Accounting | ثبت هزینه |
| GET | `/api/v1/accounting/expenses/{expense_id}` | Accounting | جزئیات هزینه |
| POST | `/api/v1/accounting/expenses/{expense_id}/void` | Accounting | ابطال هزینه |
| GET | `/api/v1/accounting/inventory-valuation` | Accounting | ارزش موجودی انبار |
| GET | `/api/v1/accounting/journal` | Accounting | فهرست اسناد حسابداری |
| POST | `/api/v1/accounting/journal` | Accounting | ثبت سند حسابداری |
| POST | `/api/v1/accounting/journal/from-inventory/{transaction_id}` | Accounting | ثبت سند از گردش انبار |
| GET | `/api/v1/accounting/journal/{entry_id}` | Accounting | جزئیات سند حسابداری |
| POST | `/api/v1/accounting/journal/{entry_id}/void` | Accounting | ابطال سند حسابداری |
| GET | `/api/v1/accounting/overview` | Accounting | نمای کلی حسابداری |
| GET | `/api/v1/accounting/profit-loss` | Accounting | صورت سود و زیان |
| PUT | `/api/v1/accounts/{account_id}/role` | Authentication | تعیین نقش مدیر/مالک برای یک حساب |
| GET | `/api/v1/admin/alerts` | Admin — Alerts | لیست همه آلرت‌های داشبورد |
| POST | `/api/v1/admin/alerts` | Admin — Alerts | ایجاد آلرت داشبورد |
| DELETE | `/api/v1/admin/alerts/{alert_id}` | Admin — Alerts | حذف آلرت داشبورد |
| PATCH | `/api/v1/admin/alerts/{alert_id}` | Admin — Alerts | ویرایش آلرت داشبورد |
| GET | `/api/v1/admin/analytics/articles` | Admin — Finance | آمار مقاله‌ها |
| GET | `/api/v1/admin/analytics/content` | Admin — Finance | بازدید محتوا (روزانه) |
| GET | `/api/v1/admin/analytics/landing-views` | Admin — Finance | گزارش کامل آمار بازدید لندینگ پیج |
| GET | `/api/v1/admin/analytics/registrations` | Admin — Finance | ثبت‌نام کافه‌ها |
| GET | `/api/v1/admin/analytics/revenue-by-day` | Admin — Finance | درآمد روزانه |
| GET | `/api/v1/admin/analytics/revenue-by-month` | Admin — Finance | درآمد ماهانه |
| GET | `/api/v1/admin/article-categories` | Admin — Content | دسته‌بندی مقالات |
| POST | `/api/v1/admin/article-categories` | Admin — Content | ایجاد دسته مقاله |
| DELETE | `/api/v1/admin/article-categories/{category_id}` | Admin — Content | حذف دسته مقاله |
| PUT | `/api/v1/admin/article-categories/{category_id}` | Admin — Content | ویرایش دسته مقاله |
| GET | `/api/v1/admin/articles` | Admin — Content | لیست مقالات |
| POST | `/api/v1/admin/articles` | Admin — Content | ایجاد مقاله |
| DELETE | `/api/v1/admin/articles/{article_id}` | Admin — Content | حذف مقاله |
| GET | `/api/v1/admin/articles/{article_id}` | Admin — Content | جزئیات مقاله |
| PUT | `/api/v1/admin/articles/{article_id}` | Admin — Content | ویرایش مقاله |
| POST | `/api/v1/admin/articles/{article_id}/preview` | Admin — Content | لینک پیش‌نمایش مقاله (۲۴ ساعت) |
| POST | `/api/v1/admin/articles/{article_id}/publish` | Admin — Content | انتشار مقاله |
| GET | `/api/v1/admin/audit-logs` | Admin — Misc | لاگ حسابرسی |
| GET | `/api/v1/admin/authors` | Admin — Content | لیست نویسندگان |
| POST | `/api/v1/admin/authors` | Admin — Content | ایجاد نویسنده |
| DELETE | `/api/v1/admin/authors/{author_id}` | Admin — Content | حذف نویسنده |
| PUT | `/api/v1/admin/authors/{author_id}` | Admin — Content | ویرایش نویسنده |
| GET | `/api/v1/admin/cafes` | Admin — Cafes | لیست کافه‌ها |
| POST | `/api/v1/admin/cafes` | Admin — Cafes | ایجاد کافه + مالک |
| DELETE | `/api/v1/admin/cafes/{cafe_id}` | Admin — Cafes | حذف (نرم) کافه |
| GET | `/api/v1/admin/cafes/{cafe_id}` | Admin — Cafes | جزئیات کافه |
| PUT | `/api/v1/admin/cafes/{cafe_id}` | Admin — Cafes | ویرایش کافه |
| GET | `/api/v1/admin/cafes/{cafe_id}/permissions` | Admin — Cafes | دسترسی‌های ماژول کافه |
| PUT | `/api/v1/admin/cafes/{cafe_id}/permissions` | Admin — Cafes | ذخیره دسترسی‌های ماژول کافه |
| GET | `/api/v1/admin/cafes/{cafe_id}/revenue` | Admin — Cafes | درآمد کافه (فروش) |
| PATCH | `/api/v1/admin/cafes/{cafe_id}/status` | Admin — Cafes | تغییر وضعیت کافه |
| GET | `/api/v1/admin/cafes/{cafe_id}/subscription` | Admin — Subscriptions | اشتراک فعلی یک کافه + تاریخچه |
| POST | `/api/v1/admin/cafes/{cafe_id}/subscription/cancel` | Admin — Subscriptions | لغو اشتراک |
| POST | `/api/v1/admin/cafes/{cafe_id}/subscription/extend` | Admin — Subscriptions | تمدید دستی (روز) |
| POST | `/api/v1/admin/cafes/{cafe_id}/subscription/grant` | Admin — Subscriptions | فعال‌سازی دستی اشتراک |
| POST | `/api/v1/admin/cafes/{cafe_id}/subscription/trial/reset` | Admin — Subscriptions | دادن دوباره تست رایگان |
| GET | `/api/v1/admin/chats` | Admin — Misc | گفتگوهای پشتیبانی |
| GET | `/api/v1/admin/chats/{chat_id}/messages` | Admin — Misc | پیام‌های گفتگو |
| POST | `/api/v1/admin/chats/{chat_id}/messages` | Admin — Misc | ارسال پیام به کافه |
| GET | `/api/v1/admin/dashboard` | Admin — Dashboard | داشبورد پلتفرم |
| GET | `/api/v1/admin/jobs` | Admin — Jobs | All postings across businesses (moderation queue: status=pending_review) |
| GET | `/api/v1/admin/jobs/reports` | Admin — Jobs | Reader reports on postings (newest first) |
| POST | `/api/v1/admin/jobs/reports/{report_id}/resolve` | Admin — Jobs | Mark a report as handled (idempotent) |
| POST | `/api/v1/admin/jobs/{job_id}/moderate` | Admin — Jobs | approve / reject / suspend / unsuspend |
| GET | `/api/v1/admin/leads` | Admin — Leads | لیست لیدها |
| POST | `/api/v1/admin/leads` | Admin — Leads | ثبت لید (ادمین) |
| DELETE | `/api/v1/admin/leads/{lead_id}` | Admin — Leads | حذف لید |
| PATCH | `/api/v1/admin/leads/{lead_id}` | Admin — Leads | به‌روزرسانی لید |
| POST | `/api/v1/admin/login` | Admin — Auth | ورود ادمین پنل |
| GET | `/api/v1/admin/media` | Admin — Content | کتابخانه رسانه |
| POST | `/api/v1/admin/media` | Admin — Content | بارگذاری رسانه |
| DELETE | `/api/v1/admin/media/{media_id}` | Admin — Content | حذف رسانه |
| PATCH | `/api/v1/admin/media/{media_id}` | Admin — Content | ویرایش alt رسانه |
| GET | `/api/v1/admin/notifications` | Admin — Misc | اعلان‌های پنل |
| POST | `/api/v1/admin/notifications` | Admin — Misc | ایجاد اعلان |
| GET | `/api/v1/admin/permissions` | Admin — Access | کاتالوگ دسترسی‌ها |
| POST | `/api/v1/admin/push/broadcast` | Admin — Misc | ارسال پوش (OneSignal) |
| GET | `/api/v1/admin/push/stats` | Admin — Misc | آمار پوش |
| GET | `/api/v1/admin/revenue` | Admin — Finance | درآمد پلتفرم |
| POST | `/api/v1/admin/revenue` | Admin — Finance | ثبت دستی درآمد |
| GET | `/api/v1/admin/roles` | Admin — Access | نقش‌های پنل ادمین |
| POST | `/api/v1/admin/roles` | Admin — Access | ایجاد نقش |
| DELETE | `/api/v1/admin/roles/{role_id}` | Admin — Access | حذف نقش |
| PUT | `/api/v1/admin/roles/{role_id}` | Admin — Access | ویرایش نقش |
| GET | `/api/v1/admin/settings` | Admin — Misc | تنظیمات سراسری |
| PUT | `/api/v1/admin/settings/{key}` | Admin — Misc | ذخیره تنظیم (upsert) |
| GET | `/api/v1/admin/subscription-campaigns` | Admin — Subscription plans | لیست همه کمپین‌ها |
| GET | `/api/v1/admin/subscription-orders` | Admin — Subscription orders | لیست سفارش‌های اشتراک |
| GET | `/api/v1/admin/subscription-orders/{order_id}` | Admin — Subscription orders | جزئیات سفارش |
| POST | `/api/v1/admin/subscription-orders/{order_id}/mark-paid` | Admin — Subscription orders | تأیید دستی پرداخت (کارت‌به‌کارت) |
| POST | `/api/v1/admin/subscription-orders/{order_id}/refund` | Admin — Subscription orders | برگشت وجه |
| GET | `/api/v1/admin/subscription-plans` | Admin — Subscription plans | لیست همه پلن‌ها + کمپین + آمار |
| POST | `/api/v1/admin/subscription-plans` | Admin — Subscription plans | ایجاد پلن |
| PATCH | `/api/v1/admin/subscription-plans/reorder` | Admin — Subscription plans | ترتیب نمایش پلن‌ها |
| DELETE | `/api/v1/admin/subscription-plans/{plan_id}` | Admin — Subscription plans | حذف پلن (فقط بدون اشتراک فعال) |
| GET | `/api/v1/admin/subscription-plans/{plan_id}` | Admin — Subscription plans | جزئیات پلن |
| PATCH | `/api/v1/admin/subscription-plans/{plan_id}` | Admin — Subscription plans | ویرایش جزئی پلن |
| PUT | `/api/v1/admin/subscription-plans/{plan_id}` | Admin — Subscription plans | ویرایش کامل پلن |
| PATCH | `/api/v1/admin/subscription-plans/{plan_id}/activate` | Admin — Subscription plans | فعال‌سازی پلن |
| DELETE | `/api/v1/admin/subscription-plans/{plan_id}/campaign` | Admin — Subscription plans | حذف کمپین (قیمت به originalPrice برمی‌گردد) |
| GET | `/api/v1/admin/subscription-plans/{plan_id}/campaign` | Admin — Subscription plans | کمپین فعلی پلن |
| PUT | `/api/v1/admin/subscription-plans/{plan_id}/campaign` | Admin — Subscription plans | ایجاد/جایگزینی کمپین |
| POST | `/api/v1/admin/subscription-plans/{plan_id}/campaign/reset-usage` | Admin — Subscription plans | صفر کردن usedCount |
| PATCH | `/api/v1/admin/subscription-plans/{plan_id}/deactivate` | Admin — Subscription plans | غیرفعال‌سازی پلن |
| PATCH | `/api/v1/admin/subscription-plans/{plan_id}/recommend` | Admin — Subscription plans | این پلن پیشنهادی شود |
| GET | `/api/v1/admin/subscription-settings/trial` | Admin — Subscription plans | تنظیمات تست رایگان |
| PUT | `/api/v1/admin/subscription-settings/trial` | Admin — Subscription plans | ویرایش تنظیمات تست رایگان |
| GET | `/api/v1/admin/subscriptions` | Admin — Subscriptions | لیست اشتراک کافه‌ها |
| POST | `/api/v1/admin/subscriptions` | Admin — Subscriptions | ایجاد اشتراک |
| GET | `/api/v1/admin/subscriptions/catalog/features` | Admin — Subscriptions | لیست فیچرهای اشتراک |
| POST | `/api/v1/admin/subscriptions/catalog/features` | Admin — Subscriptions | ایجاد فیچر اشتراک |
| DELETE | `/api/v1/admin/subscriptions/catalog/features/{feature_id}` | Admin — Subscriptions | حذف فیچر اشتراک |
| PUT | `/api/v1/admin/subscriptions/catalog/features/{feature_id}` | Admin — Subscriptions | ویرایش فیچر اشتراک |
| GET | `/api/v1/admin/subscriptions/stats` | Admin — Subscriptions | KPI اشتراک‌ها |
| DELETE | `/api/v1/admin/subscriptions/{sub_id}` | Admin — Subscriptions | حذف اشتراک |
| PATCH | `/api/v1/admin/subscriptions/{sub_id}` | Admin — Subscriptions | ویرایش اشتراک |
| GET | `/api/v1/admin/subscriptions/{subscription_id}` | Admin — Subscriptions | جزئیات اشتراک + سفارش‌ها |
| GET | `/api/v1/admin/tags` | Admin — Content | برچسب‌های مقالات |
| POST | `/api/v1/admin/tags` | Admin — Content | ایجاد برچسب |
| DELETE | `/api/v1/admin/tags/{tag_id}` | Admin — Content | حذف برچسب |
| PUT | `/api/v1/admin/tags/{tag_id}` | Admin — Content | ویرایش برچسب |
| GET | `/api/v1/admin/tenants` | Superadmin | فهرست کافه‌ها و پلن اشتراک (سوپرادمین) |
| DELETE | `/api/v1/admin/tenants/{tenant_id}/subscription` | Superadmin | لغو اشتراک کافه (سوپرادمین) |
| GET | `/api/v1/admin/tenants/{tenant_id}/subscription` | Superadmin | دریافت اشتراک یک کافه (سوپرادمین) |
| POST | `/api/v1/admin/tenants/{tenant_id}/subscription` | Superadmin | ایجاد اشتراک برای کافه (سوپرادمین) |
| PUT | `/api/v1/admin/tenants/{tenant_id}/subscription` | Superadmin | تعیین پلن اشتراک کافه پس از خرید |
| GET | `/api/v1/admin/tenants/{tenant_id}/subscription/status` | Superadmin | دریافت وضعیت فعال‌سازی پلن کافه (سوپرادمین) |
| PATCH | `/api/v1/admin/tenants/{tenant_id}/subscription/status` | Superadmin | فعال یا غیرفعال‌سازی اشتراک کافه (سوپرادمین) |
| PUT | `/api/v1/admin/tenants/{tenant_id}/subscription/status` | Superadmin | جایگزینی وضعیت اشتراک کافه (سوپرادمین) |
| GET | `/api/v1/admin/themes` | Themes | فهرست کامل تم‌ها برای سوپرادمین |
| POST | `/api/v1/admin/themes` | Themes | ایجاد تم منو توسط سوپرادمین |
| PUT | `/api/v1/admin/themes/{theme_id}` | Themes | ویرایش تم منو توسط سوپرادمین |
| GET | `/api/v1/admin/tickets` | Tickets | همه تیکت‌ها (سوپرادمین) |
| GET | `/api/v1/admin/tickets/{ticket_id}` | Tickets | Admin Get Ticket |
| PUT | `/api/v1/admin/tickets/{ticket_id}` | Tickets | Admin Update Ticket |
| POST | `/api/v1/admin/tickets/{ticket_id}/messages` | Tickets | Admin Add Message |
| GET | `/api/v1/admin/updates` | Admin — Updates | لیست همه بروزرسانی‌ها (پیش‌نویس و منتشرشده) |
| POST | `/api/v1/admin/updates` | Admin — Updates | ایجاد بروزرسانی |
| DELETE | `/api/v1/admin/updates/{update_id}` | Admin — Updates | حذف فیزیکی بروزرسانی |
| GET | `/api/v1/admin/updates/{update_id}` | Admin — Updates | جزئیات یک بروزرسانی (حتی پیش‌نویس) |
| PATCH | `/api/v1/admin/updates/{update_id}` | Admin — Updates | به‌روزرسانی جزئی بروزرسانی |
| PUT | `/api/v1/admin/updates/{update_id}` | Admin — Updates | جایگزینی کامل بروزرسانی |
| GET | `/api/v1/admin/users` | Admin — Access | کاربران پنل ادمین |
| POST | `/api/v1/admin/users` | Admin — Access | ایجاد کاربر ادمین |
| DELETE | `/api/v1/admin/users/{user_id}` | Admin — Access | حذف کاربر ادمین |
| PATCH | `/api/v1/admin/users/{user_id}` | Admin — Access | ویرایش کاربر ادمین |
| GET | `/api/v1/alerts` | Alerts | آلرت‌های داشبورد کاربر |
| POST | `/api/v1/analytics/track` | Analytics | ثبت بازدید (عمومی) |
| POST | `/api/v1/auth/register` | Authentication | ثبت‌نام مستقیم و استاندارد فروشگاه و مالک (بدون نیاز به پیامک OTP) |
| GET | `/api/v1/banners` | Banners | دریافت فهرست بنرها |
| POST | `/api/v1/banners` | Banners | ایجاد بنر جدید |
| DELETE | `/api/v1/banners/{banner_id}` | Banners | حذف بنر |
| GET | `/api/v1/banners/{banner_id}` | Banners | دریافت جزئیات بنر |
| PUT | `/api/v1/banners/{banner_id}` | Banners | ویرایش بنر |
| GET | `/api/v1/business-types` | Business Types | فهرست انواع کسب‌وکار فعال |
| POST | `/api/v1/business-types` | Business Types | تعریف نوع کسب‌وکار جدید (فقط مدیر کل) |
| PATCH | `/api/v1/business-types/{code}` | Business Types | ویرایش نوع کسب‌وکار (فقط مدیر کل) |
| GET | `/api/v1/cafe/notifications` | Cafe Profile | اعلان‌های کافه |
| PATCH | `/api/v1/cafe/printing` | Cafe Profile | روشن/خاموش کردن استفاده از چاپگر در کل کافه |
| GET | `/api/v1/cafe/profile` | Cafe Profile | دریافت پروفایل کافه |
| PUT | `/api/v1/cafe/profile` | Cafe Profile | ویرایش پروفایل کافه (نام، نام کاربری، توضیح، آدرس، ویژگی‌ها، ساعت کاری، شبکه‌های اجتماعی، تم منو) |
| POST | `/api/v1/cafe/profile/logo` | Cafe Profile | آپلود تصویر پروفایل کافه |
| GET | `/api/v1/cafe/profile/username` | Cafe Profile | بررسی دسترس‌پذیری نام کاربری کافه |
| GET | `/api/v1/cafe/sidebar` | Cafe Profile | منوی داینامیک داشبورد کافه |
| GET | `/api/v1/cafe/subscription` | Subscription billing — cafe | وضعیت اشتراک کافه + کاتالوگ + تاریخچه |
| PUT | `/api/v1/cafe/subscription` | Cafe Profile | تغییر پلن اشتراک |
| GET | `/api/v1/cafe/subscription/invoices/{order_id}` | Subscription billing — cafe | فاکتور سفارش (JSON) |
| GET | `/api/v1/cafe/subscription/orders` | Subscription billing — cafe | تاریخچه سفارش‌های اشتراک کافه |
| POST | `/api/v1/cafe/subscription/orders` | Subscription billing — cafe | ایجاد سفارش خرید اشتراک |
| GET | `/api/v1/cafe/subscription/orders/{order_id}` | Subscription billing — cafe | وضعیت یک سفارش |
| POST | `/api/v1/cafe/subscription/orders/{order_id}/cancel` | Subscription billing — cafe | لغو سفارش در انتظار |
| POST | `/api/v1/cafe/subscription/orders/{order_id}/receipt` | Subscription billing — cafe | آپلود رسید کارت‌به‌کارت |
| POST | `/api/v1/cafe/subscription/trial` | Subscription billing — cafe | شروع تست رایگان |
| DELETE | `/api/v1/cafe/working-hours` | Cafe Profile | بازنشانی ساعات کاری به حالت همیشه باز |
| GET | `/api/v1/cafe/working-hours` | Cafe Profile | دریافت ساعات کاری کافه |
| PATCH | `/api/v1/cafe/working-hours` | Cafe Profile | ویرایش جزئی ساعات کاری کافه |
| POST | `/api/v1/cafe/working-hours` | Cafe Profile | تنظیم ساعات کاری کافه |
| PUT | `/api/v1/cafe/working-hours` | Cafe Profile | ویرایش کامل ساعات کاری کافه |
| GET | `/api/v1/cafe/working-hours/status` | Cafe Profile | وضعیت زنده کاری کافه |
| GET | `/api/v1/categories` | Categories | لیست دسته‌بندی‌ها |
| POST | `/api/v1/categories` | Categories | ایجاد دسته‌بندی |
| POST | `/api/v1/categories/bulk` | Categories | ایجاد گروهی دسته‌بندی‌ها |
| DELETE | `/api/v1/categories/{category_id}` | Categories | حذف دسته‌بندی |
| GET | `/api/v1/categories/{category_id}` | Categories | جزئیات دسته‌بندی |
| PUT | `/api/v1/categories/{category_id}` | Categories | ویرایش دسته‌بندی |
| PUT | `/api/v1/change-password` | Authentication | تغییر رمز عبور حساب جاری |
| GET | `/api/v1/companies/{slug}` | Jobs - public | صفحهٔ عمومی کسب‌وکار |
| GET | `/api/v1/customer-groups` | Customers & Customer Club | لیست گروه‌های مشتریان (قراردادهای سازمانی) و تخفیف هر گروه |
| POST | `/api/v1/customer-groups` | Customers & Customer Club | ایجاد گروه مشتریان با تخفیف قراردادی (درصدی یا مبلغ ثابت) |
| GET | `/api/v1/customer-groups/loyalty-settings` | Customers & Customer Club | تنظیم امتیاز باشگاه برای سفارش‌های گروهی (پیش‌فرض: بدون امتیاز) |
| PUT | `/api/v1/customer-groups/loyalty-settings` | Customers & Customer Club | روشن/خاموش کردن امتیاز سفارش گروهی و نرخ امتیاز به‌ازای هر ۱۰٬۰۰۰ تومان |
| DELETE | `/api/v1/customer-groups/{group_id}` | Customers & Customer Club | حذف گروه مشتریان (اعضا از گروه خارج می‌شوند؛ سوابق سفارش‌ها حفظ می‌شود) |
| GET | `/api/v1/customer-groups/{group_id}` | Customers & Customer Club | جزئیات گروه مشتریان |
| PATCH | `/api/v1/customer-groups/{group_id}` | Customers & Customer Club | ویرایش گروه مشتریان (نام، تخفیف، سقف، وضعیت، بازه قرارداد) |
| PUT | `/api/v1/customer-groups/{group_id}` | Customers & Customer Club | ویرایش گروه مشتریان (معادل PATCH) |
| GET | `/api/v1/customer-groups/{group_id}/activity` | Customers & Customer Club | فعالیت گروه به تفکیک کانال: حضوری / آنلاین / سفارش گروهی (بدون داده = null) |
| GET | `/api/v1/customer-groups/{group_id}/invoices` | Customers & Customer Club | فهرست فاکتورهای دوره‌ای گروه |
| POST | `/api/v1/customer-groups/{group_id}/invoices` | Customers & Customer Club | صدور فاکتور دوره (اعداد در همان لحظه قفل می‌شوند) |
| DELETE | `/api/v1/customer-groups/{group_id}/invoices/{invoice_id}` | Customers & Customer Club | حذف فاکتور پیش‌نویس یا لغوشده |
| GET | `/api/v1/customer-groups/{group_id}/invoices/{invoice_id}` | Customers & Customer Club | جزئیات فاکتور (همراه اقلام، اعضا و پرداخت‌ها) |
| PATCH | `/api/v1/customer-groups/{group_id}/invoices/{invoice_id}` | Customers & Customer Club | ویرایش جزئی فاکتور (معادل PUT) |
| PUT | `/api/v1/customer-groups/{group_id}/invoices/{invoice_id}` | Customers & Customer Club | ویرایش مهلت/یادداشت/وضعیت پیش‌نویس↔صادرشده (مبالغ قفل است) |
| POST | `/api/v1/customer-groups/{group_id}/invoices/{invoice_id}/cancel` | Customers & Customer Club | لغو فاکتور |
| POST | `/api/v1/customer-groups/{group_id}/invoices/{invoice_id}/payments` | Customers & Customer Club | ثبت پرداخت (جزئی یا کامل) روی فاکتور |
| GET | `/api/v1/customer-groups/{group_id}/members` | Customers & Customer Club | اعضای گروه مشتریان |
| POST | `/api/v1/customer-groups/{group_id}/members` | Customers & Customer Club | افزودن اعضا به گروه (با شناسه مشتری یا شماره تماس؛ شماره ناشناس مشتری جدید می‌سازد) |
| PUT | `/api/v1/customer-groups/{group_id}/members` | Customers & Customer Club | افزودن اعضا به گروه (معادل POST؛ idempotent) |
| DELETE | `/api/v1/customer-groups/{group_id}/members/{customer_id}` | Customers & Customer Club | حذف عضو از گروه |
| POST | `/api/v1/customer-groups/{group_id}/members/{customer_id}` | Customers & Customer Club | افزودن یک مشتری مشخص به گروه |
| PUT | `/api/v1/customer-groups/{group_id}/members/{customer_id}` | Customers & Customer Club | افزودن یک مشتری مشخص به گروه (معادل POST؛ idempotent) |
| GET | `/api/v1/customer-groups/{group_id}/orders` | Customers & Customer Club | سفارش‌های پرداخت‌شده یا نسیه‌شدهٔ گروه (فیلتر تاریخ و کانال) |
| GET | `/api/v1/customer-groups/{group_id}/statement` | Customers & Customer Club | صورتحساب دورهٔ قرارداد (جمع سفارش‌های اعضا) |
| GET | `/api/v1/customer-retention/automations` | Customer retention | فهرست اتوماسیون‌ها |
| POST | `/api/v1/customer-retention/automations` | Customer retention | ساخت اتوماسیون |
| DELETE | `/api/v1/customer-retention/automations/{automation_id}` | Customer retention | حذف اتوماسیون |
| GET | `/api/v1/customer-retention/automations/{automation_id}` | Customer retention | جزئیات اتوماسیون |
| PATCH | `/api/v1/customer-retention/automations/{automation_id}` | Customer retention | ویرایش اتوماسیون |
| GET | `/api/v1/customer-retention/campaigns` | Customer retention | فهرست کمپین‌ها |
| POST | `/api/v1/customer-retention/campaigns` | Customer retention | ساخت کمپین پیش‌نویس |
| POST | `/api/v1/customer-retention/campaigns/audience-preview` | Customer retention | محاسبهٔ مخاطب پیش از ارسال |
| DELETE | `/api/v1/customer-retention/campaigns/{campaign_id}` | Customer retention | حذف کمپین پیش‌نویس |
| GET | `/api/v1/customer-retention/campaigns/{campaign_id}` | Customer retention | جزئیات و آمار کمپین |
| PATCH | `/api/v1/customer-retention/campaigns/{campaign_id}` | Customer retention | ویرایش کمپین پیش‌نویس |
| POST | `/api/v1/customer-retention/campaigns/{campaign_id}/launch` | Customer retention | ارسال فوری کمپین (نیازمند Idempotency-Key و snapshot_token) |
| GET | `/api/v1/customer-retention/customers` | Customer retention | فهرست مشتریان CRM |
| GET | `/api/v1/customer-retention/customers/{customer_id}` | Customer retention | پروندهٔ مشتری |
| GET | `/api/v1/customer-retention/customers/{customer_id}/timeline` | Customer retention | تاریخچهٔ تعامل مشتری |
| GET | `/api/v1/customer-retention/dashboard` | Customer retention | KPI و روند بازگردانی مشتری |
| GET | `/api/v1/customer-retention/recovery` | Customer retention | مرکز بازیابی |
| GET | `/api/v1/customer-retention/templates` | Customer retention | فهرست قالب‌ها |
| POST | `/api/v1/customer-retention/templates` | Customer retention | ساخت قالب |
| DELETE | `/api/v1/customer-retention/templates/{template_id}` | Customer retention | حذف قالب استفاده‌نشده |
| GET | `/api/v1/customer-retention/templates/{template_id}` | Customer retention | جزئیات قالب |
| PATCH | `/api/v1/customer-retention/templates/{template_id}` | Customer retention | ویرایش یا آرشیو قالب |
| GET | `/api/v1/customers` | Customers & Customer Club | لیست مشتریان کافه و اعضای باشگاه (Gym/Club) |
| POST | `/api/v1/customers` | Customers & Customer Club | ایجاد مشتری جدید همراه با عضویت باشگاه مشتریان (Gym/Club) |
| GET | `/api/v1/customers/gym` | Customers & Customer Club | لیست اعضای باشگاه مشتریان (Gym/Club) |
| POST | `/api/v1/customers/gym` | Customers & Customer Club | ایجاد مشتری در باشگاه مشتریان (Gym/Club) |
| DELETE | `/api/v1/customers/{customer_id}` | Customers & Customer Club | حذف مشتری و لغو عضویت باشگاه (Gym/Club) |
| GET | `/api/v1/customers/{customer_id}` | Customers & Customer Club | دریافت جزئیات مشتری و اطلاعات باشگاه (Gym/Club) |
| PATCH | `/api/v1/customers/{customer_id}` | Customers & Customer Club | ویرایش جزئی مشخصات مشتری و عضویت باشگاه (Gym/Club) |
| PUT | `/api/v1/customers/{customer_id}` | Customers & Customer Club | ویرایش مشخصات مشتری و عضویت باشگاه (Gym/Club) |
| GET | `/api/v1/dashboard` | Dashboard | Get Dashboard |
| GET | `/api/v1/dashboard/sidebar` | Dashboard | منوی کناری داشبورد بر اساس اشتراک کافه |
| GET | `/api/v1/debtors` | Debtors | فهرست بدهکاران و نسیه‌ها |
| POST | `/api/v1/debtors` | Debtors | ثبت بدهکار / نسیه جدید |
| GET | `/api/v1/debtors/group-report` | Debtors | گزارش سفارش‌های اعضای یک گروه/قرارداد با وضعیت پرداخت |
| GET | `/api/v1/debtors/overview` | Debtors | خلاصه آمار بدهکاران |
| DELETE | `/api/v1/debtors/{debtor_id}` | Debtors | حذف بدهکار |
| GET | `/api/v1/debtors/{debtor_id}` | Debtors | جزئیات بدهکار |
| PATCH | `/api/v1/debtors/{debtor_id}` | Debtors | ویرایش بدهکار |
| POST | `/api/v1/debtors/{debtor_id}/orders` | Debtors | افزودن سفارش نسیه به پروندهٔ بدهکار |
| GET | `/api/v1/debtors/{debtor_id}/payments` | Debtors | فهرست پرداخت‌های بدهکار |
| POST | `/api/v1/debtors/{debtor_id}/payments` | Debtors | ثبت پرداخت بدهکار |
| POST | `/api/v1/debtors/{debtor_id}/settle` | Debtors | تسویه کامل بدهکار |
| GET | `/api/v1/deep-analytics/basket/affinity` | Deep analytics | Products bought together (support / confidence / lift) |
| GET | `/api/v1/deep-analytics/controls` | Deep analytics | Per-cashier void / delete / discount control flags |
| GET | `/api/v1/deep-analytics/costs` | Deep analytics | Food cost %, waste %, purchases |
| GET | `/api/v1/deep-analytics/customers/cohorts` | Deep analytics | Monthly acquisition cohorts and retention |
| GET | `/api/v1/deep-analytics/customers/rfm` | Deep analytics | RFM segments with a heuristic 12-month value |
| GET | `/api/v1/deep-analytics/data-quality` | Deep analytics | Cost coverage and customer capture; fix these to trust the numbers |
| GET | `/api/v1/deep-analytics/digest` | Deep analytics | Last full week vs the week before (includes a ready Persian message) |
| GET | `/api/v1/deep-analytics/export/{dataset}.csv` | Deep analytics | CSV export of a rollup dataset (UTF-8 with BOM, Excel-friendly) |
| GET | `/api/v1/deep-analytics/forecast/products` | Deep analytics | Expected units per product and suggested prep quantity |
| GET | `/api/v1/deep-analytics/forecast/sales` | Deep analytics | Daily sales forecast (weekly seasonality, 80% interval) |
| GET | `/api/v1/deep-analytics/insights` | Deep analytics | Automatic alerts and findings |
| POST | `/api/v1/deep-analytics/insights/read-all` | Deep analytics | Mark every unread insight as read |
| PATCH | `/api/v1/deep-analytics/insights/{insight_id}` | Deep analytics | Mark an insight read / dismissed |
| GET | `/api/v1/deep-analytics/inventory/forecast` | Deep analytics | Days of stock, stock-out date and reorder suggestions per ingredient |
| GET | `/api/v1/deep-analytics/menu-engineering` | Deep analytics | Stars / plowhorses / puzzles / dogs |
| GET | `/api/v1/deep-analytics/overview` | Deep analytics | KPIs for a period, optionally compared with the previous period |
| GET | `/api/v1/deep-analytics/price-impact` | Deep analytics | Volume before/after detected price changes |
| GET | `/api/v1/deep-analytics/products` | Deep analytics | Product performance (quantity, revenue, margin) |
| POST | `/api/v1/deep-analytics/refresh` | Deep analytics | Rebuild the recent rollups and re-evaluate insights now |
| GET | `/api/v1/deep-analytics/sales/heatmap` | Deep analytics | Weekday x hour heatmap |
| GET | `/api/v1/deep-analytics/sales/trend` | Deep analytics | Sales time series (day / week / month) |
| GET | `/api/v1/employer/applications/{application_id}` | Jobs - employer | Get Employer Application |
| POST | `/api/v1/employer/applications/{application_id}/notes` | Jobs - employer | Add Application Note |
| GET | `/api/v1/employer/applications/{application_id}/resume` | Jobs - employer | دانلود رزومهٔ متقاضی (فقط کارفرمای همان آگهی) |
| POST | `/api/v1/employer/applications/{application_id}/resume-link` | Jobs - employer | Signed 5-minute download URL for the applicant's resume |
| POST | `/api/v1/employer/applications/{application_id}/status` | Jobs - employer | Change Application Status |
| GET | `/api/v1/employer/business` | Jobs - employer | Get Employer Business |
| PUT | `/api/v1/employer/business` | Jobs - employer | Put Employer Business |
| GET | `/api/v1/employer/jobs` | Jobs - employer | List Employer Jobs |
| POST | `/api/v1/employer/jobs` | Jobs - employer | Create Employer Job |
| GET | `/api/v1/employer/jobs/{job_id}` | Jobs - employer | Get Employer Job |
| PUT | `/api/v1/employer/jobs/{job_id}` | Jobs - employer | Update Employer Job |
| GET | `/api/v1/employer/jobs/{job_id}/applications` | Jobs - employer | List Job Applications |
| POST | `/api/v1/employer/jobs/{job_id}/status` | Jobs - employer | Change Job Status |
| GET | `/api/v1/employer/overview` | Jobs - employer | Employer Overview |
| GET | `/api/v1/employer/resume-files/{token}` | Jobs - employer | Download via signed link (no login; token is the credential) |
| POST | `/api/v1/guest-session` | Authentication | ایجاد نشست مهمان |
| GET | `/api/v1/inventory/categories` | Inventory | دسته‌بندی‌های انبار |
| POST | `/api/v1/inventory/categories` | Inventory | افزودن دسته‌بندی انبار |
| DELETE | `/api/v1/inventory/categories/{code}` | Inventory | حذف دسته‌بندی انبار |
| GET | `/api/v1/inventory/categories/{code}` | Inventory | جزئیات دسته‌بندی انبار |
| PATCH | `/api/v1/inventory/categories/{code}` | Inventory | ویرایش دسته‌بندی انبار |
| GET | `/api/v1/inventory/items` | Inventory | لیست قلم‌های انبار |
| POST | `/api/v1/inventory/items` | Inventory | افزودن قلم انبار |
| DELETE | `/api/v1/inventory/items/{ingredient_id}` | Inventory | حذف قلم انبار |
| GET | `/api/v1/inventory/items/{ingredient_id}` | Inventory | جزئیات قلم انبار |
| PUT | `/api/v1/inventory/items/{ingredient_id}` | Inventory | ویرایش قلم انبار |
| POST | `/api/v1/inventory/items/{ingredient_id}/archive` | Inventory | بایگانی قلم انبار |
| POST | `/api/v1/inventory/items/{ingredient_id}/restore` | Inventory | بازیابی قلم انبار از بایگانی |
| GET | `/api/v1/inventory/movements` | Inventory | فهرست کامل گردش‌های انبار با فیلتر |
| POST | `/api/v1/inventory/movements` | Inventory | ثبت ورود یا خروج |
| POST | `/api/v1/inventory/movements/bulk` | Inventory | ثبت گروهی گردش انبار (اتمیک) |
| GET | `/api/v1/inventory/movements/recent` | Inventory | آخرین گردش‌های انبار |
| POST | `/api/v1/inventory/movements/{transaction_id}/reverse` | Inventory | برگشت گردش انبار (معکوس تغییرناپذیر) |
| GET | `/api/v1/inventory/overview` | Inventory | نمای کلی انبار |
| GET | `/api/v1/inventory/purchases` | Inventory Purchases | فهرست فاکتورهای خرید |
| POST | `/api/v1/inventory/purchases` | Inventory Purchases | ثبت فاکتور خرید همراه با فی |
| GET | `/api/v1/inventory/purchases/{purchase_id}` | Inventory Purchases | جزئیات فاکتور خرید |
| GET | `/api/v1/inventory/reasons` | Inventory | کدهای دلایل گردش انبار |
| GET | `/api/v1/inventory/recipes` | Inventory | فهرست دستورهای تهیه |
| POST | `/api/v1/inventory/recipes` | Inventory | ایجاد دستور تهیه |
| DELETE | `/api/v1/inventory/recipes/{product_id}` | Inventory | غیرفعال‌سازی دستور تهیه |
| GET | `/api/v1/inventory/recipes/{product_id}` | Inventory | دستور تهیه محصول |
| PUT | `/api/v1/inventory/recipes/{product_id}` | Inventory | ویرایش دستور تهیه |
| POST | `/api/v1/inventory/recipes/{product_id}/cost-preview` | Inventory | پیش‌نمایش هزینه دستور تهیه |
| GET | `/api/v1/inventory/summary` | Inventory | خلاصه آماری انبار (تعداد، کمبود، ارزش) |
| GET | `/api/v1/inventory/suppliers` | Inventory Suppliers | فهرست طرف حساب‌ها / تامین‌کنندگان |
| POST | `/api/v1/inventory/suppliers` | Inventory Suppliers | تعریف تامین‌کننده / طرف حساب جدید |
| GET | `/api/v1/inventory/suppliers/{supplier_id}` | Inventory Suppliers | دریافت جزئیات طرف حساب |
| PUT | `/api/v1/inventory/suppliers/{supplier_id}` | Inventory Suppliers | ویرایش طرف حساب |
| GET | `/api/v1/inventory/units` | Inventory | واحدهای اندازه‌گیری استاندارد |
| GET | `/api/v1/inventory/warehouses` | Inventory | انبارهای کافه |
| GET | `/api/v1/inventory/waste` | Inventory | فهرست ضایعات |
| POST | `/api/v1/inventory/waste` | Inventory | ثبت ضایعات |
| GET | `/api/v1/inventory/waste/summary` | Inventory | خلاصه ضایعات |
| GET | `/api/v1/inventory/waste/{waste_id}` | Inventory | جزئیات ضایعات |
| POST | `/api/v1/inventory/waste/{waste_id}/void` | Inventory | ابطال ضایعات |
| GET | `/api/v1/jobs` | Jobs - public | جستجوی آگهی‌های کاریابی |
| GET | `/api/v1/jobs/{slug}` | Jobs - public | جزئیات آگهی |
| POST | `/api/v1/jobs/{slug}/applications` | Jobs - public | ارسال درخواست همکاری |
| POST | `/api/v1/jobs/{slug}/reports` | Jobs - public | گزارش آگهی |
| GET | `/api/v1/jobs/{slug}/similar` | Jobs - public | آگهی‌های مشابه |
| GET | `/api/v1/landing/admin/article-categories` | Landing service | List landing article categories |
| POST | `/api/v1/landing/admin/article-categories` | Landing service | Create landing article category |
| DELETE | `/api/v1/landing/admin/article-categories/{id}` | Landing service | Delete landing article category |
| GET | `/api/v1/landing/admin/article-categories/{id}` | Landing service | Get landing article category |
| PUT | `/api/v1/landing/admin/article-categories/{id}` | Landing service | Update landing article category |
| GET | `/api/v1/landing/admin/articles` | Landing service | List landing articles |
| POST | `/api/v1/landing/admin/articles` | Landing service | Create landing article |
| DELETE | `/api/v1/landing/admin/articles/{id}` | Landing service | Delete landing article |
| GET | `/api/v1/landing/admin/articles/{id}` | Landing service | Get landing article |
| PUT | `/api/v1/landing/admin/articles/{id}` | Landing service | Update landing article |
| POST | `/api/v1/landing/admin/articles/{id}/publish` | Landing service | Publish landing article |
| POST | `/api/v1/landing/admin/articles/{id}/unpublish` | Landing service | Unpublish landing article |
| GET | `/api/v1/landing/admin/categories` | Landing service | List landing article categories (alias) |
| POST | `/api/v1/landing/admin/categories` | Landing service | Create landing article category (alias) |
| DELETE | `/api/v1/landing/admin/categories/{id}` | Landing service | Delete landing article category (alias) |
| GET | `/api/v1/landing/admin/categories/{id}` | Landing service | Get landing article category (alias) |
| PUT | `/api/v1/landing/admin/categories/{id}` | Landing service | Update landing article category (alias) |
| GET | `/api/v1/landing/admin/leads` | Landing service | List landing leads |
| DELETE | `/api/v1/landing/admin/leads/{id}` | Landing service | Delete landing lead |
| GET | `/api/v1/landing/admin/leads/{id}` | Landing service | Get landing lead |
| PUT | `/api/v1/landing/admin/leads/{id}` | Landing service | Update landing lead |
| GET | `/api/v1/landing/admin/me` | Landing service | Get landing admin profile |
| GET | `/api/v1/landing/admin/pages` | Landing service | List landing pages |
| POST | `/api/v1/landing/admin/pages` | Landing service | Create landing page |
| GET | `/api/v1/landing/admin/pages/by-path` | Landing service | Get landing admin page by path |
| GET | `/api/v1/landing/admin/pages/by-slug/{slug}` | Landing service | Get landing admin page by slug |
| DELETE | `/api/v1/landing/admin/pages/{id}` | Landing service | Delete landing page |
| GET | `/api/v1/landing/admin/pages/{id}` | Landing service | Get landing page |
| PUT | `/api/v1/landing/admin/pages/{id}` | Landing service | Update landing page |
| POST | `/api/v1/landing/admin/pages/{id}/publish` | Landing service | Publish landing page |
| PUT | `/api/v1/landing/admin/pages/{id}/sections/reorder` | Landing service | Reorder landing page sections |
| POST | `/api/v1/landing/admin/pages/{id}/unpublish` | Landing service | Unpublish landing page |
| GET | `/api/v1/landing/admin/site` | Landing service | Get landing site settings |
| PUT | `/api/v1/landing/admin/site` | Landing service | Update landing site settings |
| GET | `/api/v1/landing/bundle` | Landing service | Get public marketing bundle (alias) |
| GET | `/api/v1/landing/pages` | Landing service | List published marketing pages (alias) |
| GET | `/api/v1/landing/pages/by-path` | Landing service | Get public page by path (alias) |
| GET | `/api/v1/landing/pages/{slug}` | Landing service | Get public page by slug (alias) |
| GET | `/api/v1/landing/site` | Landing service | Get public marketing site (alias) |
| POST | `/api/v1/leads` | Landing service | Create public lead (alias) |
| GET | `/api/v1/legacy/admin/subscription-plans` | Subscription plans | لیست کامل پلن‌ها (سوپرادمین) |
| POST | `/api/v1/legacy/admin/subscription-plans` | Subscription plans | ایجاد پلن اشتراک (سوپرادمین) |
| DELETE | `/api/v1/legacy/admin/subscription-plans/{plan_id}` | Subscription plans | حذف پلن اشتراک (سوپرادمین) |
| GET | `/api/v1/legacy/admin/subscription-plans/{plan_id}` | Subscription plans | جزئیات پلن (سوپرادمین) |
| PUT | `/api/v1/legacy/admin/subscription-plans/{plan_id}` | Subscription plans | ویرایش پلن اشتراک (سوپرادمین) |
| GET | `/api/v1/legacy/subscription-plans` | Subscription plans | لیست پلن‌های اشتراک |
| POST | `/api/v1/legacy/subscription-plans` | Subscription plans | ایجاد پلن اشتراک |
| DELETE | `/api/v1/legacy/subscription-plans/{plan_id}` | Subscription plans | حذف پلن اشتراک |
| GET | `/api/v1/legacy/subscription-plans/{plan_id}` | Subscription plans | جزئیات پلن اشتراک |
| PUT | `/api/v1/legacy/subscription-plans/{plan_id}` | Subscription plans | ویرایش پلن اشتراک |
| POST | `/api/v1/login` | Authentication | ورود با شماره موبایل و رمز عبور |
| GET | `/api/v1/me` | Authentication | دریافت اطلاعات حساب کاربری فعلی |
| GET | `/api/v1/me/applications` | Jobs - candidate | List My Applications |
| GET | `/api/v1/me/applications/{application_id}` | Jobs - candidate | Get My Application |
| POST | `/api/v1/me/applications/{application_id}/withdraw` | Jobs - candidate | Withdraw Application |
| GET | `/api/v1/me/candidate-profile` | Jobs - candidate | Get Candidate Profile |
| PUT | `/api/v1/me/candidate-profile` | Jobs - candidate | Put Candidate Profile |
| DELETE | `/api/v1/me/job-data` | Jobs - candidate | Erase my jobs data (profile, resumes + files, saved jobs, notifications) |
| GET | `/api/v1/me/job-notifications` | Jobs - candidate | List Job Notifications |
| POST | `/api/v1/me/job-notifications/read` | Jobs - candidate | Mark notifications read (body {ids?: []}; omitted = all) |
| GET | `/api/v1/me/resumes` | Jobs - candidate | List Resumes |
| POST | `/api/v1/me/resumes/uploads` | Jobs - candidate | Request Resume Upload |
| PUT | `/api/v1/me/resumes/uploads/{upload_id}/content` | Jobs - candidate | بارگذاری بایت‌های رزومه (مرحلهٔ بین درخواست و نهایی‌سازی) |
| POST | `/api/v1/me/resumes/uploads/{upload_id}/finalize` | Jobs - candidate | Finalize Resume Upload |
| DELETE | `/api/v1/me/resumes/{resume_id}` | Jobs - candidate | Delete Resume |
| PUT | `/api/v1/me/resumes/{resume_id}/default` | Jobs - candidate | Set Default Resume |
| GET | `/api/v1/me/saved-jobs` | Jobs - candidate | List Saved Jobs |
| DELETE | `/api/v1/me/saved-jobs/{slug}` | Jobs - candidate | Unsave Job |
| PUT | `/api/v1/me/saved-jobs/{slug}` | Jobs - candidate | Save Job |
| GET | `/api/v1/menu/{username}` | Public Menu | دریافت منوی عمومی کافه |
| GET | `/api/v1/menu/{username}/products/{product_id}` | Public Menu | دریافت جزئیات محصول منوی عمومی |
| GET | `/api/v1/modifier-groups` | Modifier Groups | فهرست گروه‌های افزونه قابل استفاده مجدد |
| GET | `/api/v1/online/addresses` | Online ordering | List Addresses |
| POST | `/api/v1/online/addresses` | Online ordering | Create Address |
| PUT | `/api/v1/online/addresses/{address_id}` | Online ordering | تغییر آدرس روی نقشه (lat/lng) |
| GET | `/api/v1/online/cafe-plans` | Online ordering | لیست پلن‌های عضویت کافه |
| POST | `/api/v1/online/cafe-plans` | Online ordering | ایجاد پلن عضویت کافه |
| DELETE | `/api/v1/online/cafe-plans/{plan_id}` | Online ordering | حذف پلن عضویت کافه |
| GET | `/api/v1/online/cafe-plans/{plan_id}` | Online ordering | جزئیات پلن عضویت کافه |
| PUT | `/api/v1/online/cafe-plans/{plan_id}` | Online ordering | ویرایش پلن عضویت کافه |
| POST | `/api/v1/online/cafe-plans/{plan_id}/features` | Online ordering | افزودن ویژگی به پلن عضویت |
| DELETE | `/api/v1/online/cafe-plans/{plan_id}/features/{feature_key}` | Online ordering | حذف ویژگی پلن عضویت |
| PUT | `/api/v1/online/cafe-plans/{plan_id}/features/{feature_key}` | Online ordering | ویرایش ویژگی پلن عضویت |
| GET | `/api/v1/online/cafe/tickets` | Online customer tickets | تیکت‌های مشتریان آنلاین کافه |
| GET | `/api/v1/online/cafe/tickets/{ticket_id}` | Online customer tickets | جزئیات تیکت مشتری آنلاین |
| PUT | `/api/v1/online/cafe/tickets/{ticket_id}` | Online customer tickets | بستن یا بازکردن تیکت مشتری آنلاین |
| POST | `/api/v1/online/cafe/tickets/{ticket_id}/messages` | Online customer tickets | پاسخ کافه به مشتری آنلاین |
| GET | `/api/v1/online/cafes/{username}` | Online ordering | Cafe Info |
| GET | `/api/v1/online/cafes/{username}/club` | Online ordering | دریافت وضعیت عضویت باشگاه مشتری |
| POST | `/api/v1/online/cafes/{username}/club` | Online ordering | عضویت مشتری در باشگاه کافه |
| POST | `/api/v1/online/cafes/{username}/orders` | Online ordering | ثبت سفارش آنلاین مشتری |
| GET | `/api/v1/online/cafes/{username}/plans` | Online ordering | لیست پلن‌های فعال عضویت کافه |
| POST | `/api/v1/online/cafes/{username}/plans/{plan_id}/checkout` | Online ordering | انتخاب پلن و ایجاد درخواست پرداخت عضویت |
| POST | `/api/v1/online/cafes/{username}/subscribe` | Online ordering | Subscribe |
| GET | `/api/v1/online/cards` | Online ordering | کارت‌های پرداخت (مشتری یا کافه) |
| POST | `/api/v1/online/cards` | Online ordering | ایجاد کارت پرداخت مشتری یا کافه |
| DELETE | `/api/v1/online/cards/{card_id}` | Online ordering | حذف کارت ثبت‌شده |
| PUT | `/api/v1/online/cards/{card_id}` | Online ordering | ویرایش کارت پرداخت مشتری یا کافه |
| DELETE | `/api/v1/online/cart` | Online ordering | Clear Cart |
| GET | `/api/v1/online/cart` | Online ordering | Get Cart |
| PUT | `/api/v1/online/cart` | Online ordering | Set Cart |
| GET | `/api/v1/online/customers` | Online ordering | مشتریانی که سفارش آنلاین ثبت کرده‌اند |
| POST | `/api/v1/online/customers` | Online ordering | ایجاد مشتری آنلاین و عضویت باشگاه (Gym/Club) |
| POST | `/api/v1/online/customers/gym` | Online ordering | ایجاد مشتری آنلاین در باشگاه مشتریان (Gym/Club) |
| DELETE | `/api/v1/online/customers/gym/{consumer_id}` | Online ordering | حذف مشتری آنلاین از باشگاه مشتریان (Gym/Club) |
| GET | `/api/v1/online/customers/gym/{consumer_id}` | Online ordering | دریافت جزئیات مشتری آنلاین در باشگاه مشتریان (Gym/Club) |
| DELETE | `/api/v1/online/customers/{consumer_id}` | Online ordering | حذف مشتری آنلاین و لغو عضویت باشگاه (Gym/Club) |
| GET | `/api/v1/online/customers/{consumer_id}` | Online ordering | دریافت جزئیات مشتری آنلاین و عضویت باشگاه (Gym/Club) |
| PATCH | `/api/v1/online/customers/{consumer_id}` | Online ordering | ویرایش جزئی مشخصات مشتری آنلاین و عضویت باشگاه (Gym/Club) |
| PUT | `/api/v1/online/customers/{consumer_id}` | Online ordering | ویرایش مشخصات مشتری آنلاین و عضویت باشگاه (Gym/Club) |
| POST | `/api/v1/online/login` | Online ordering | ورود مشتری با شماره و رمز عبور |
| POST | `/api/v1/online/login-otp` | Online ordering | ورود مشتری با کد تأیید |
| GET | `/api/v1/online/me` | Online ordering | پروفایل مشتری و آدرس‌ها |
| PUT | `/api/v1/online/me` | Online ordering | ویرایش نام مشتری |
| GET | `/api/v1/online/membership-payments` | Online ordering | پرداخت‌های عضویت مشتریان کافه |
| POST | `/api/v1/online/membership-payments/{payment_id}/confirm` | Online ordering | تایید پرداخت عضویت و فعال‌سازی پلن مشتری |
| GET | `/api/v1/online/orders` | Online ordering | فهرست سفارش‌های آنلاین مشتری |
| GET | `/api/v1/online/orders/realtime/docs` | Online ordering | قرارداد ریل‌تایم سفارش‌های آنلاین |
| GET | `/api/v1/online/orders/stream` | Online ordering | جریان ریل‌تایم سفارش‌های آنلاین مشتری (SSE / HTTP Streaming) |
| GET | `/api/v1/online/orders/stream/` | Online ordering | جریان ریل‌تایم سفارش‌های آنلاین مشتری (SSE / HTTP Streaming) |
| GET | `/api/v1/online/orders/{order_id}` | Online ordering | جزئیات سفارش آنلاین مشتری |
| POST | `/api/v1/online/otp/request` | Online ordering | ارسال کد تأیید مشتری |
| POST | `/api/v1/online/otp/verify` | Online ordering | تأیید کد مشتری |
| POST | `/api/v1/online/refresh` | Online ordering | نوسازی نشست مشتری |
| POST | `/api/v1/online/register` | Online ordering | ثبت‌نام مشتری با شماره و رمز عبور |
| POST | `/api/v1/online/reset-password` | Online ordering | بازنشانی رمز عبور مشتری |
| POST | `/api/v1/online/reset-password/request` | Online ordering | ارسال کد تأیید بازنشانی رمز عبور مشتری |
| POST | `/api/v1/online/signup` | Online ordering | ثبت‌نام مشتری با شماره و رمز عبور |
| GET | `/api/v1/online/stream` | Online ordering | جریان ریل‌تایم سفارش‌های آنلاین مشتری (SSE / HTTP Streaming) |
| GET | `/api/v1/online/stream/` | Online ordering | جریان ریل‌تایم سفارش‌های آنلاین مشتری (SSE / HTTP Streaming) |
| GET | `/api/v1/online/subscribers` | Online ordering | اعضای باشگاه کافه |
| GET | `/api/v1/online/tickets` | Online ordering | List Customer Tickets |
| POST | `/api/v1/online/tickets` | Online ordering | Create Customer Ticket |
| GET | `/api/v1/online/tickets/{ticket_id}` | Online ordering | جزئیات تیکت مشتری |
| POST | `/api/v1/online/tickets/{ticket_id}/messages` | Online ordering | Add Customer Ticket Message |
| GET | `/api/v1/online/wishlist` | Online ordering | Wishlist |
| POST | `/api/v1/online/wishlist` | Online ordering | Add Wishlist |
| DELETE | `/api/v1/online/wishlist/{item_id}` | Online ordering | Delete Wishlist |
| GET | `/api/v1/orders` | Orders | لیست سفارش‌ها |
| POST | `/api/v1/orders` | Orders | ثبت سفارش جدید |
| POST | `/api/v1/orders/close-day` | Orders | بستن روز کاری و صفر کردن سفارش‌های باز |
| GET | `/api/v1/orders/history` | Orders | تاریخچه سفارش‌های روزهای قبل |
| GET | `/api/v1/orders/online/stream` | Orders Streaming | جریان ریل‌تایم سفارش‌های آنلاین (SSE / HTTP Streaming) |
| GET | `/api/v1/orders/online/stream/` | Orders Streaming | جریان ریل‌تایم سفارش‌های آنلاین (SSE / HTTP Streaming) |
| GET | `/api/v1/orders/products` | Orders | محصولات سفارش سریع |
| POST | `/api/v1/orders/start-day` | Orders | شروع روز کاری جدید |
| GET | `/api/v1/orders/stream` | Orders Streaming | جریان ریل‌تایم سفارش‌های آنلاین (SSE / HTTP Streaming) |
| GET | `/api/v1/orders/stream/` | Orders Streaming | جریان ریل‌تایم سفارش‌های آنلاین (SSE / HTTP Streaming) |
| DELETE | `/api/v1/orders/{order_id}` | Orders | حذف سفارش (نرم، با دلیل و audit) |
| GET | `/api/v1/orders/{order_id}` | Orders | جزئیات سفارش |
| PUT | `/api/v1/orders/{order_id}/accept` | Orders | پذیرش سفارش آنلاین توسط کافه |
| PUT | `/api/v1/orders/{order_id}/cancel` | Orders | لغو سفارش |
| POST | `/api/v1/orders/{order_id}/complete` | Orders | تکمیل سفارش (POST) |
| PUT | `/api/v1/orders/{order_id}/complete` | Orders | تکمیل سفارش |
| PUT | `/api/v1/orders/{order_id}/items` | Orders | ویرایش اقلام سفارش |
| PUT | `/api/v1/orders/{order_id}/prepare` | Orders | شروع آماده‌سازی سفارش |
| PUT | `/api/v1/orders/{order_id}/ready` | Orders | آماده شدن سفارش |
| POST | `/api/v1/orders/{order_id}/receipt` | Orders | آپلود عکس رسید کارت‌به‌کارت سفارش آنلاین |
| POST | `/api/v1/orders/{order_id}/refund` | Orders | مرجوع کردن سفارش تکمیل‌شده (فقط مالک/مدیر) |
| GET | `/api/v1/orders/{order_id}/split-bill` | Split bill | حساب دنگی یک سفارش باز |
| POST | `/api/v1/orders/{order_id}/split-payments` | Split bill | ثبت پرداخت یک نفر از حساب دنگی |
| DELETE | `/api/v1/orders/{order_id}/split-payments/{payment_id}` | Split bill | برگرداندن یک پرداخت دنگی |
| POST | `/api/v1/otp-login/request` | Authentication | ارسال کد ورود |
| POST | `/api/v1/otp-login/verify` | Authentication | ورود با کد تأیید |
| POST | `/api/v1/otp/request` | Authentication | ارسال کد تأیید |
| POST | `/api/v1/otp/verify` | Authentication | تأیید کد ثبت‌نام |
| GET | `/api/v1/payments/callback/{gateway}` | Subscription billing — payments | کال‌بک درگاه پرداخت (بدون احراز هویت) |
| GET | `/api/v1/pos` | POS | دریافت اطلاعات صندوق فروش |
| POST | `/api/v1/pos/checkout` | POS | ثبت و تسویه سفارش صندوق فروش |
| POST | `/api/v1/pos/close-day` | POS | بستن روز کاری در صندوق فروش |
| GET | `/api/v1/pos/orders` | POS | دریافت صف سفارش‌های صندوق شامل سفارش آنلاین |
| POST | `/api/v1/pos/orders/{order_id}/close` | POS | بستن و تسویه سفارش میز در صندوق فروش |
| GET | `/api/v1/pos/print-jobs` | POS Print Jobs | سابقه چاپ (قابل فیلتر با order_id) |
| POST | `/api/v1/pos/print-jobs` | POS Print Jobs | ثبت چاپ (ضد تکرار) |
| POST | `/api/v1/pos/quote` | POS | محاسبه سبد صندوق فروش |
| POST | `/api/v1/pos/start-day` | POS | شروع روز کاری جدید در صندوق فروش |
| GET | `/api/v1/printer-configs` | Printer Configurations | فهرست پرینترهای ثبت‌شده کافه |
| POST | `/api/v1/printer-configs` | Printer Configurations | تعریف و افزودن پرینتر جدید |
| GET | `/api/v1/printer-configs/category-routes` | Printer Category Routes | فهرست قوانین دسته‌بندی ← پرینتر |
| POST | `/api/v1/printer-configs/category-routes` | Printer Category Routes | ساخت قانون دسته‌بندی ← پرینتر |
| DELETE | `/api/v1/printer-configs/category-routes/{route_id}` | Printer Category Routes | حذف قانون |
| GET | `/api/v1/printer-configs/category-routes/{route_id}` | Printer Category Routes | دریافت یک قانون |
| PATCH | `/api/v1/printer-configs/category-routes/{route_id}` | Printer Category Routes | تغییر جزئی قانون |
| PUT | `/api/v1/printer-configs/category-routes/{route_id}` | Printer Category Routes | جایگزینی کامل قانون |
| GET | `/api/v1/printer-configs/policy` | Printer Configurations | دریافت سیاست‌های چاپ تننت |
| PATCH | `/api/v1/printer-configs/policy` | Printer Configurations | ویرایش جزیی سیاست‌های چاپ تننت |
| PUT | `/api/v1/printer-configs/policy` | Printer Configurations | بروزرسانی سیاست‌های چاپ تننت |
| GET | `/api/v1/printer-configs/receipts/orders/{order_id}` | Printer Configurations | دریافت پاکت کامل فیش سفارش |
| GET | `/api/v1/printer-configs/receipts/{order_id}/club` | Printer Configurations | دریافت فاکتور اختصاصی باشگاه مشتریان برای چاپ |
| POST | `/api/v1/printer-configs/receipts/{order_id}/club/print` | Printer Configurations | ثبت و ارسال چاپ فاکتور اختصاصی باشگاه مشتریان |
| GET | `/api/v1/printer-configs/receipts/{order_id}/order-completion` | Printer Configurations | دریافت فیش تکمیل سفارش برای چاپ |
| POST | `/api/v1/printer-configs/receipts/{order_id}/order-completion/print` | Printer Configurations | ثبت و ارسال چاپ فیش تکمیل سفارش |
| GET | `/api/v1/printer-configs/receipts/{order_id}/settlement` | Printer Configurations | دریافت فاکتور تسویه برای چاپ |
| POST | `/api/v1/printer-configs/receipts/{order_id}/settlement/print` | Printer Configurations | ثبت و ارسال چاپ فاکتور تسویه |
| GET | `/api/v1/printer-configs/target/club` | Printer Configurations | پرینترهای هدف برای فاکتور باشگاه مشتریان |
| GET | `/api/v1/printer-configs/target/order-completion` | Printer Configurations | پرینترهای هدف برای تکمیل سفارش |
| GET | `/api/v1/printer-configs/target/settlement` | Printer Configurations | پرینترهای هدف برای فاکتور تسویه |
| DELETE | `/api/v1/printer-configs/{printer_id}` | Printer Configurations | حذف پرینتر |
| GET | `/api/v1/printer-configs/{printer_id}` | Printer Configurations | دریافت مشخصات یک پرینتر |
| PATCH | `/api/v1/printer-configs/{printer_id}` | Printer Configurations | ویرایش جزیی تنظیمات پرینتر |
| PUT | `/api/v1/printer-configs/{printer_id}` | Printer Configurations | ویرایش کامل مشخصات پرینتر |
| GET | `/api/v1/printer-configs/{printer_id}/test` | Printer Configurations | فیش آزمایشی پرینتر |
| GET | `/api/v1/printers` | Printers | List active printers |
| POST | `/api/v1/printers` | Printers | Create or update printer settings |
| GET | `/api/v1/printers/receipts/{order_id}/order-completion` | Printers | دریافت فیش تکمیل سفارش برای چاپ |
| POST | `/api/v1/printers/receipts/{order_id}/order-completion/print` | Printers | ثبت و ارسال چاپ فیش تکمیل سفارش |
| GET | `/api/v1/printers/receipts/{order_id}/settlement` | Printers | دریافت فاکتور تسویه برای چاپ |
| POST | `/api/v1/printers/receipts/{order_id}/settlement/print` | Printers | ثبت و ارسال چاپ فاکتور تسویه |
| DELETE | `/api/v1/printers/{id}` | Printers | Delete a printer |
| PATCH | `/api/v1/printers/{id}` | Printers | Partially update a printer |
| GET | `/api/v1/printers/{role}` | Printers | Get printer settings for a role |
| PUT | `/api/v1/printers/{role}` | Printers | Replace printer settings for a role |
| GET | `/api/v1/products` | Products | لیست محصولات منو |
| POST | `/api/v1/products` | Products | ایجاد محصول جدید همراه با تنوع‌ها و افزونه‌ها |
| POST | `/api/v1/products/bulk` | Products | ایجاد گروهی محصولات |
| PUT | `/api/v1/products/bulk` | Products | ویرایش گروهی محصولات |
| DELETE | `/api/v1/products/{product_id}` | Products | حذف محصول |
| GET | `/api/v1/products/{product_id}` | Products | جزئیات محصول (عمومی) |
| PATCH | `/api/v1/products/{product_id}` | Products | ویرایش جزئی محصول |
| PUT | `/api/v1/products/{product_id}` | Products | ویرایش محصول |
| POST | `/api/v1/products/{product_id}/modifier-groups` | Products | افزودن گروه افزونه به محصول |
| POST | `/api/v1/products/{product_id}/variants` | Products | افزودن تنوع به محصول |
| DELETE | `/api/v1/products/{product_id}/variants/{variant_id}` | Products | حذف تنوع محصول |
| PUT | `/api/v1/products/{product_id}/variants/{variant_id}` | Products | ویرایش تنوع محصول |
| POST | `/api/v1/products/{product_id}/variants/{variant_id}/modifier-groups/{group_id}` | Products | اتصال گروه افزونه موجود به تنوع محصول |
| GET | `/api/v1/public/article-categories` | Landing service | List article categories |
| GET | `/api/v1/public/articles` | Landing service | List published articles |
| GET | `/api/v1/public/articles/{slug}` | Landing service | Get published article by slug |
| POST | `/api/v1/public/leads` | Landing service | Create public lead |
| GET | `/api/v1/public/marketing/bundle` | Landing service | Get public marketing bundle |
| GET | `/api/v1/public/marketing/pages` | Landing service | List published marketing pages |
| GET | `/api/v1/public/marketing/pages/by-path` | Landing service | Get public page by path |
| GET | `/api/v1/public/marketing/pages/{slug}` | Landing service | Get public page by slug |
| GET | `/api/v1/public/marketing/site` | Landing service | Get public marketing site |
| POST | `/api/v1/quick-start/import` | Quick Start | شروع ورود بسته (job غیرهمزمان)؛ ساخت منو در worker انجام می‌شود |
| GET | `/api/v1/quick-start/import/status/{job_id}` | Quick Start | وضعیت job ورود (polling هر ۴۰۰ms تا completed/failed) |
| GET | `/api/v1/quick-start/packages` | Quick Start | فهرست بسته‌های فعال شروع سریع (کارت انتخاب صنعت) |
| GET | `/api/v1/quick-start/packages/{package_id}` | Quick Start | پیش‌نمایش بسته: دسته‌ها، شش محصول نمونه و نام مواد |
| POST | `/api/v1/refresh` | Authentication | نوسازی توکن |
| POST | `/api/v1/register` | Authentication | ثبت‌نام فروشگاه |
| POST | `/api/v1/register/direct` | Authentication | ثبت‌نام مستقیم فروشگاه و مالک (بدون نیاز به پیامک OTP) |
| POST | `/api/v1/reports/cost-control-calculator` | Accounting & Reports | ماشین‌حساب استاندارد cost control بر اساس مواد اولیه و درصد سود هدف |
| GET | `/api/v1/reports/income` | Accounting & Reports | گزارش جامع درآمد، سفارش‌ها و عملکرد محصولات |
| GET | `/api/v1/reports/inventory-history` | Accounting & Reports | تاریخچه تغییرات انبار |
| GET | `/api/v1/reports/online-users` | Accounting & Reports | گزارش مشتریان سفارش آنلاین (مرتب‌شده بر اساس تعداد سفارش) |
| GET | `/api/v1/reports/profit` | Accounting & Reports | محاسبه سود و کنترل بهای تمام‌شده محصولات بر اساس قیمت منو، دستور تهیه و بهای مواد اولیه |
| GET | `/api/v1/reports/profit/history` | Accounting & Reports | تاریخچه محاسبات سود محصولات |
| GET | `/api/v1/reports/profit/history/{batch_id}` | Accounting & Reports | جزئیات یک محاسبه سود و cost control (با جست‌وجو، فیلتر و مرتب‌سازی) |
| POST | `/api/v1/reset-password` | Authentication | بازنشانی رمز عبور با کد یکبار مصرف (OTP) |
| POST | `/api/v1/reset-password/confirm` | Authentication | تغییر رمز عبور با استفاده از توکن بازنشانی |
| POST | `/api/v1/reset-password/request` | Authentication | درخواست کد تایید برای بازنشانی رمز عبور |
| POST | `/api/v1/reset-password/verify` | Authentication | تایید کد و دریافت توکن بازنشانی رمز عبور |
| GET | `/api/v1/settings/features` | Settings | دریافت وضعیت ماژول‌های اختیاری |
| PUT | `/api/v1/settings/features` | Settings | فعال یا غیرفعال‌سازی ماژول‌ها (انبار، حسابداری، سفارش آنلاین، مساعده) |
| GET | `/api/v1/staff` | Staff | لیست کارکنان کافه |
| POST | `/api/v1/staff` | Staff | ایجاد حساب کارمند جدید |
| GET | `/api/v1/staff/dashboard` | Staff | داشبورد عملیاتی کارکنان (صف سفارش‌ها) |
| POST | `/api/v1/staff/login` | Staff | ورود کارکنان |
| GET | `/api/v1/staff/me` | Staff | هویت و مجوزهای مؤثر کارمند جاری |
| DELETE | `/api/v1/staff/{account_id}` | Staff | غیرفعال‌سازی حساب کارمند |
| GET | `/api/v1/staff/{account_id}` | Staff | مشاهده حساب کارمند |
| PUT | `/api/v1/staff/{account_id}` | Staff | ویرایش حساب کارمند |
| GET | `/api/v1/staff/{account_id}/advances` | Staff | لیست مساعده‌های کارمند |
| POST | `/api/v1/staff/{account_id}/advances` | Staff | ثبت مساعده / پیش‌پرداخت کارمند |
| PUT | `/api/v1/staff/{account_id}/advances/{advance_id}` | Staff | تسویه یا ویرایش مساعده |
| PUT | `/api/v1/staff/{account_id}/insurance` | Staff | ثبت یا ویرایش بیمه کارمند |
| PUT | `/api/v1/staff/{account_id}/salary` | Staff | ثبت یا ویرایش حقوق کارمند |
| PUT | `/api/v1/staff/{account_id}/shifts` | Staff | ثبت یا ویرایش شیفت‌های کاری |
| PUT | `/api/v1/staff/{account_id}/time-off` | Staff | ثبت یا ویرایش مرخصی و تعطیلی |
| GET | `/api/v1/subscription-plans` | Subscription billing — public | کاتالوگ عمومی پلن‌های اشتراک |
| GET | `/api/v1/subscription-plans/offer` | Subscription billing — public | پیشنهاد ویژه (نوار بالای سایت) |
| GET | `/api/v1/subscription-plans/{code}` | Subscription billing — public | یک پلن اشتراک |
| GET | `/api/v1/subscription-settings/trial` | Subscription billing — public | تنظیمات تست رایگان |
| GET | `/api/v1/tables` | Tables | لیست میزهای کافه |
| POST | `/api/v1/tables` | Tables | افزودن میز جدید |
| POST | `/api/v1/tables/bulk` | Tables | افزودن چند میز و QR متناظر |
| POST | `/api/v1/tables/{source_table_id}/transfer` | Tables | انتقال اتمیک سفارش‌ها و مشتریان فعال یک میز به میز دیگر |
| DELETE | `/api/v1/tables/{table_id}` | Tables | حذف میز |
| GET | `/api/v1/tables/{table_id}` | Tables | جزئیات میز |
| PATCH | `/api/v1/tables/{table_id}` | Tables | ویرایش میز |
| PUT | `/api/v1/tables/{table_id}` | Tables | ویرایش میز |
| GET | `/api/v1/tables/{table_id}/qr` | Tables | دریافت QR متناظر با شماره میز |
| POST | `/api/v1/tables/{table_id}/release` | Tables | خالی شدن میز: ارسال پیامک به رزروکننده و آزاد کردن میز |
| DELETE | `/api/v1/tables/{table_id}/reservation` | Tables | لغو رزرو میز (بدون پیامک) |
| POST | `/api/v1/tables/{table_id}/reserve` | Tables | رزرو میز برای مشتری (نام و شماره موبایل الزامی) |
| GET | `/api/v1/themes` | Themes | فهرست تم‌های منو برای داشبورد مدیر |
| GET | `/api/v1/tickets` | Tickets | تیکت‌های کافه |
| POST | `/api/v1/tickets` | Tickets | ثبت تیکت پشتیبانی |
| GET | `/api/v1/tickets/presence` | Tickets | آیا پشتیبانی آنلاین است |
| POST | `/api/v1/tickets/telegram/webhook` | Tickets | وب‌هوک ربات تلگرام پشتیبانی |
| GET | `/api/v1/tickets/{ticket_id}` | Tickets | جزئیات تیکت |
| PUT | `/api/v1/tickets/{ticket_id}` | Tickets | بستن یا حل‌شده کردن تیکت |
| POST | `/api/v1/tickets/{ticket_id}/messages` | Tickets | پیام کافه روی تیکت |
| GET | `/api/v1/updates` | Updates | لیست بروزرسانی‌های منتشرشده |
| GET | `/api/v1/updates/{update_id}` | Updates | جزئیات یک بروزرسانی منتشرشده |
| GET | `/health` | System | بررسی سلامت فرآیند سرویس (Liveness) |
| GET | `/ready` | System | بررسی آمادگی سرویس و پایگاه داده (Readiness) |
