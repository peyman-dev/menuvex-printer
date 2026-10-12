// MenuVex Printer Agent Legacy — native Win32 window for Windows 7 SP1 and later.
//
// The layout mirrors the modern Agent (src/App.tsx, src/styles/app.css) with plain GDI so it
// needs no WebView: a right-hand sidebar with pages, status pills for the local Agent and the
// MenuVex website, summary cards, printer cards, and an automatic "find printers" page that lists
// USB printers (in-box Windows driver), LAN printers (TCP/9100) and Windows queues for one-click
// setup — no driver installation, PowerShell or CMD.
#include "server.hpp"
#include <algorithm>
#include <atomic>
#include <cctype>
#include <commctrl.h>
#include <dbt.h>
#include <map>
#include <objbase.h>
#include <set>
#include <shellapi.h>
namespace {
using namespace mv;
constexpr UINT Changed = WM_APP + 1, Tray = WM_APP + 2, Discovered = WM_APP + 3,
               Rejected = WM_APP + 4, Probed = WM_APP + 5;
constexpr UINT_PTR SecretTimer = 1, TrayTimer = 2, UsbTimer = 3, PulseTimer = 4, ClipboardTimer = 5;
// Fixed client size that fits a 1024x768 cashier screen with the taskbar. The window is not
// DPI-aware, so Windows scales it as one unit and controls never overlap at 125%/150%.
constexpr int ClientW = 984, ClientH = 640, SidebarW = 200, HeaderH = 60;
constexpr int Left = 24, Right = ClientW - SidebarW - 24, SideLeft = ClientW - SidebarW;
// Form rows: labels sit right of the controls (right-to-left reading order).
constexpr int LabelRight = Right - 20, ControlRight = Right - 152;
// Palette shared with the modern Agent (src/styles/app.css).
namespace ink {
constexpr COLORREF page = RGB(245, 247, 251), card = RGB(255, 255, 255), line = RGB(226, 232, 244),
                   text = RGB(30, 41, 59), muted = RGB(120, 135, 160), faint = RGB(176, 186, 204),
                   blue = RGB(37, 99, 235), blue_dark = RGB(29, 78, 216),
                   blue_soft = RGB(232, 239, 255), blue_line = RGB(188, 210, 250),
                   green = RGB(22, 163, 74), green_soft = RGB(233, 247, 239),
                   red = RGB(209, 67, 67), red_soft = RGB(253, 241, 239), amber = RGB(150, 104, 18),
                   amber_soft = RGB(255, 245, 217), nav = RGB(107, 122, 148),
                   gray_soft = RGB(241, 244, 250), button_line = RGB(215, 224, 240),
                   violet = RGB(109, 40, 217), violet_soft = RGB(243, 237, 255),
                   disabled = RGB(160, 184, 236);
}
enum Page { Home, Find, Edit, Pair, Queue, Settings };
enum Tone { Info, Success, Warning, Failure };
enum class Kind { Nav, Primary, Secondary, Subtle, Danger, Field, List, Check };
enum : int {
    NavHome = 100,
    NavFind,
    NavPair,
    NavQueue,
    NavSettings,
    PrinterList = 200,
    AddPrinter,
    TestPrinter,
    ProbePrinter,
    EditPrinter,
    RemovePrinter,
    GoPair,
    FoundList = 300,
    Rescan,
    AddFound,
    AddManual,
    ConnType = 400,
    UsbPick,
    QueuePick,
    Host,
    NetPort,
    CheckEdit,
    RefreshPick,
    Name,
    Role,
    AutoPrint,
    Cutter,
    Paper80,
    Paper58,
    Copies,
    Dots,
    Font,
    SaveEdit,
    SaveTest,
    CancelEdit,
    DeleteEdit,
    Reveal = 500,
    CopyKey,
    SecretBox,
    Rotate,
    QueueList = 600,
    CancelJob,
    ClearHistory,
    Port = 700,
    Attempts,
    AutoStart,
    SaveSettings,
    OpenFolder,
    Quit,
    Open = 800,
    TrayAdd
};
struct Ctl {
    HWND hwnd;
    int page; // -1: every page (sidebar)
    Kind kind;
    COLORREF surface; // colour behind the control, for rounded corners and checkbox text
};
struct Message {
    Tone tone = Info;
    std::wstring text;
};
struct Badge {
    std::wstring text;
    COLORREF fg, bg;
};
// One automatically discovered printer, ready to be added with a single click.
struct Found {
    std::string kind, name, configured;
    std::wstring title, detail, note;
    Json connection;
    int paper = 80;
};
std::wstring w(const std::string &s) {
    try {
        return wide(s);
    } catch (...) {
        return L"?";
    }
}
// Persian digits for counts and dates; technical values (IP, port, IDs) stay Latin.
std::wstring fa(long long value) {
    std::wstring s = std::to_wstring(value);
    for (auto &c : s)
        if (c >= L'0' && c <= L'9')
            c = static_cast<wchar_t>(0x06F0 + (c - L'0'));
    return s;
}
std::wstring fa2(int value) { return value < 10 ? fa(0) + fa(value) : fa(value); }
bool same_text(const std::string &a, const std::string &b) {
    return a.size() == b.size() &&
           std::equal(a.begin(), a.end(), b.begin(), [](unsigned char x, unsigned char y) {
               return std::toupper(x) == std::toupper(y);
           });
}
// True when two connections point at the same printer (same rules as "already added").
bool same_connection(const Json &a, const Json &b) {
    Json one = Json::array();
    one.push_back({{"name", "x"}, {"connection", a}});
    return !configured_name(one, b).empty();
}
std::wstring hex4(int value) {
    const wchar_t *digits = L"0123456789ABCDEF";
    std::wstring s(4, L'0');
    for (int i = 3; i >= 0; --i, value >>= 4)
        s[static_cast<std::size_t>(i)] = digits[value & 15];
    return s;
}
std::wstring usb_id(const Json &c) {
    return hex4(c.value("vendorId", 0)) + L":" + hex4(c.value("productId", 0));
}
// Local wall-clock time (Solar Hijri date when it is not today).
std::wstring when(std::int64_t unix_seconds) {
    auto local = [](std::int64_t t, SYSTEMTIME &out) {
        ULARGE_INTEGER ticks;
        ticks.QuadPart = static_cast<unsigned long long>(t + 11644473600LL) * 10000000ULL;
        FILETIME utc{ticks.LowPart, ticks.HighPart}, here{};
        return FileTimeToLocalFileTime(&utc, &here) && FileTimeToSystemTime(&here, &out);
    };
    SYSTEMTIME at{}, today{};
    if (unix_seconds <= 0 || !local(unix_seconds, at) || !local(now(), today))
        return L"";
    std::wstring clock = fa2(at.wHour) + L":" + fa2(at.wMinute);
    if (at.wYear == today.wYear && at.wMonth == today.wMonth && at.wDay == today.wDay)
        return clock;
    int jy = 0, jm = 0, jd = 0;
    jalali(at.wYear, at.wMonth, at.wDay, jy, jm, jd);
    return fa(jy) + L"/" + fa2(jm) + L"/" + fa2(jd) + L" " + clock;
}
// Plain-Persian explanation of an agent error code, for the owner rather than a developer.
std::wstring explain(const std::string &code, const std::string &message = {}) {
    if (code == "PRINTER_OFFLINE")
        return L"پرینتر در دسترس نبود (خاموش، جدا شده یا مشغول). روشن بودن و کابل را بررسی کنید؛ "
               L"چاپ چند بار خودکار تکرار می‌شود.";
    if (code == "PRINT_OUTCOME_UNKNOWN")
        return L"نتیجهٔ چاپ نامشخص است و ممکن است چاپ شده باشد. پیش از چاپ دوباره، کاغذ را بررسی "
               L"کنید.";
    if (code == "NETWORK_TIMEOUT" || code == "NETWORK_CONNECTION_FAILED")
        return L"پرینتر شبکه پاسخ نداد. روشن بودن پرینتر، کابل شبکه و درست بودن IP را بررسی کنید.";
    if (code == "SPOOLER_UNAVAILABLE")
        return L"صف چاپ ویندوز پیدا نشد یا سرویس چاپ ویندوز (Print Spooler) در دسترس نیست.";
    if (code == "QUEUE_OFFLINE")
        return L"ویندوز این صف چاپ را «آفلاین» نشان می‌دهد؛ پرینتر را روشن کنید و در «دستگاه‌ها و "
               L"چاپگرها» حالت «استفاده آفلاین» را بردارید.";
    if (code == "USB_AMBIGUOUS")
        return L"دو پرینتر USB یکسان وصل است و از هم تشخیص داده نمی‌شوند. پرینتر را در «ویرایش» "
               L"دوباره انتخاب کنید.";
    if (code == "USB_BUSY")
        return L"پرینتر USB در اختیار برنامهٔ دیگری است. برنامهٔ چاپ دیگر را ببندید و دوباره "
               L"امتحان کنید.";
    if (code == "USB_DISCOVERY_FAILED" || code == "USB_DEVICE_ERROR")
        return L"ویندوز نتوانست پرینترهای USB را بخواند. کابل را جدا و دوباره وصل کنید.";
    if (code == "QUEUE_FULL")
        return L"صف چاپ پر است؛ ابتدا کارهای معلق را بررسی یا لغو کنید.";
    if (code == "AGENT_NOT_READY" || code == "QUEUE_ERROR")
        return L"پردازش چاپ متوقف شده است؛ برنامه را ببندید و دوباره اجرا کنید. چاپ دوباره خودکار "
               L"انجام نمی‌شود.";
    if (code == "PRINTER_NOT_FOUND")
        return L"این پرینتر دیگر در تنظیمات نیست.";
    if (code == "JOB_NOT_CANCELLABLE")
        return L"فقط کارهای «در صف» قابل لغو هستند؛ کاری که به پرینتر تحویل شده برنمی‌گردد.";
    if (code == "AUTOSTART_ERROR")
        return L"تنظیم اجرای خودکار ویندوز انجام نشد.";
    if (code == "PORT_IN_USE_OR_UNAVAILABLE")
        return L"پورت محلی در اختیار برنامهٔ دیگری است (معمولاً Agent جدید منووکس). آن را از کنار "
               L"ساعت ویندوز ببندید یا پورت را در «تنظیمات» عوض کنید.";
    if (code == "INVALID_CONFIG" || code == "INVALID_PAYLOAD") {
        static const std::pair<const char *, const wchar_t *> known[] = {
            {"Invalid paper profile",
             L"عرض کاغذ باید ۵۸ یا ۸۰ میلی‌متر و عرض چاپ مضربی از ۸ بین ۱۲۸ تا ۸۳۲ نقطه باشد."},
            {"Invalid print profile", L"تعداد نسخه باید ۱ تا ۳ و اندازهٔ قلم ۱۲ تا ۴۸ باشد."},
            {"Use a literal private IPv4",
             L"IP پرینتر باید یک IP شبکهٔ داخلی مثل 192.168.1.50 باشد."},
            {"Invalid LAN port", L"پورت پرینتر شبکه باید بین ۱ تا ۶۵۵۳۵ باشد (معمولاً 9100)."},
            {"Invalid name", L"نام پرینتر را وارد کنید (حداکثر ۱۲۸ نویسه)."},
            {"Too many printers", L"حداکثر ۱۶ پرینتر قابل تعریف است."},
            {"Invalid settings", L"پورت محلی باید بین ۱۰۲۴ تا ۶۵۵۳۵ و تعداد تلاش بین ۱ تا ۵ باشد."},
            {"valid integer", L"در این خانه فقط عدد وارد کنید."},
            {"Select a USB printer", L"یک پرینتر USB از فهرست انتخاب کنید."},
            {"Select a Windows queue", L"یک صف چاپ ویندوز از فهرست انتخاب کنید."},
            {"Enter the printer IP", L"IP پرینتر شبکه را وارد کنید."},
            {"Enter a printer name", L"یک نام برای پرینتر بنویسید؛ مثلاً «صندوق»."},
            {"Invalid route",
             L"هر کاربرد (فاکتور، آشپزخانه، بار) فقط به یک پرینتر وصل می‌شود."}};
        for (const auto &entry : known)
            if (message.find(entry.first) != std::string::npos)
                return entry.second;
        return L"مقدار واردشده درست نیست: " + w(message);
    }
    return w(code) + (message.empty() ? L"" : L" — " + w(message));
}
std::wstring status_label(const std::string &s) {
    if (s == "queued")
        return L"در صف";
    if (s == "printing")
        return L"در حال چاپ";
    if (s == "completed")
        return L"ارسال شد";
    if (s == "failed")
        return L"ناموفق";
    if (s == "cancelled")
        return L"لغو شد";
    return w(s);
}
std::wstring role_label(const std::string &role) {
    if (role == "invoice")
        return L"فاکتور مشتری";
    if (role == "kitchen")
        return L"آشپزخانه";
    if (role == "bar")
        return L"بار";
    return w(role);
}
// ---- GDI helpers ----
void fill(HDC dc, const RECT &r, COLORREF color) {
    SetDCBrushColor(dc, color);
    FillRect(dc, &r, reinterpret_cast<HBRUSH>(GetStockObject(DC_BRUSH)));
}
void box(HDC dc, const RECT &r, COLORREF background, COLORREF border, int radius = 12) {
    SelectObject(dc, GetStockObject(DC_BRUSH));
    SelectObject(dc, GetStockObject(DC_PEN));
    SetDCBrushColor(dc, background);
    SetDCPenColor(dc, border);
    RoundRect(dc, r.left, r.top, r.right, r.bottom, radius, radius);
}
void dot(HDC dc, int x, int y, int size, COLORREF color) {
    SelectObject(dc, GetStockObject(DC_BRUSH));
    SelectObject(dc, GetStockObject(DC_PEN));
    SetDCBrushColor(dc, color);
    SetDCPenColor(dc, color);
    Ellipse(dc, x, y, x + size, y + size);
}
void draw(HDC dc, const std::wstring &s, RECT r, HFONT font, COLORREF color, UINT flags) {
    SelectObject(dc, font);
    SetTextColor(dc, color);
    SetBkMode(dc, TRANSPARENT);
    DrawTextW(dc, s.c_str(), static_cast<int>(s.size()), &r, flags | DT_NOPREFIX);
}
// One right-aligned right-to-left line.
void rtl(HDC dc, const std::wstring &s, RECT r, HFONT font, COLORREF color) {
    draw(dc, s, r, font, color,
         DT_RIGHT | DT_RTLREADING | DT_SINGLELINE | DT_VCENTER | DT_END_ELLIPSIS);
}
// A wrapped right-to-left paragraph ('\n' starts a new line).
void para(HDC dc, const std::wstring &s, RECT r, HFONT font, COLORREF color) {
    draw(dc, s, r, font, color, DT_RIGHT | DT_RTLREADING | DT_WORDBREAK);
}
int width_of(HDC dc, const std::wstring &s, HFONT font) {
    SelectObject(dc, font);
    SIZE size{};
    GetTextExtentPoint32W(dc, s.c_str(), static_cast<int>(s.size()), &size);
    return size.cx;
}
// Rounded status pill with a dot. Anchored at its left edge, or at its right edge if `from_right`.
RECT pill(HDC dc, int x, int y, int h, const std::wstring &s, HFONT font, COLORREF fg, COLORREF bg,
          bool from_right = false, bool with_dot = true) {
    int width = width_of(dc, s, font) + (with_dot ? 34 : 22);
    RECT r{from_right ? x - width : x, y, from_right ? x : x + width, y + h};
    box(dc, r, bg, bg, h);
    if (with_dot)
        dot(dc, r.right - 16, y + h / 2 - 3, 7, fg);
    RECT t{r.left + 10, y, r.right - (with_dot ? 24 : 11), y + h};
    draw(dc, s, t, font, fg, DT_RIGHT | DT_RTLREADING | DT_SINGLELINE | DT_VCENTER);
    return r;
}
struct App {
    HWND window = nullptr;
    struct {
        HFONT body = nullptr, bold = nullptr, small = nullptr, tiny = nullptr, title = nullptr,
              heading = nullptr, big = nullptr, brand = nullptr, mono = nullptr;
    } font;
    HBRUSH page_brush = nullptr, card_brush = nullptr;
    HICON icon = nullptr, brand_icon = nullptr;
    std::wstring directory;
    std::unique_ptr<Store> store;
    std::unique_ptr<Secret> secret;
    std::unique_ptr<Server> server;
    std::thread worker, discovery, prober;
    std::atomic<bool> stopping{false}, worker_failed{false};
    std::mutex wake_mutex;
    std::condition_variable wake;
    Json config = default_config(), printers = Json::array(), jobs = Json::array();
    Page page = Home, edit_return = Home;
    std::map<int, Ctl> controls;
    std::map<Page, Message> messages;
    std::string selected, selected_job, draft_id, watch_test_job, watch_test_name;
    // Tray icon creation can fail or time out while Explorer is still starting (typical at login
    // on slow Windows 7 PCs). That must never stop the agent, so it is retried, not fatal.
    bool tray_ok = false;
    HDEVNOTIFY device_notify = nullptr;
    // Why the last browser connection was refused (network thread writes, UI thread reads).
    std::mutex reject_mutex;
    std::string reject_code, reject_detail;
    std::string reject_key;                                  // network thread only
    std::int64_t reject_logged_at = 0, reject_posted_at = 0; // network thread only
    std::wstring reject_text;                                // UI thread
    std::int64_t reject_seen_at = 0;                         // UI thread
    bool start_failed = false, stopped_reported = false;
    std::string start_code;
    int last_site_count = 0;
    // Automatic discovery: the worker thread fills `incoming`, the UI thread copies it.
    struct Scan {
        std::vector<QueueInfo> queues;
        std::vector<UsbPrinter> usb;
        std::vector<std::uint32_t> lan;
        std::wstring scope;
        std::string error;
        bool usb_ok = false;
    };
    std::mutex discovery_mutex;
    Scan incoming;
    std::vector<QueueInfo> queues;
    std::vector<UsbPrinter> usb;
    std::vector<std::uint32_t> lan;
    std::vector<Found> found;
    std::set<std::string> known_usb;
    std::wstring scan_scope;
    std::string scan_error;
    bool scanning = false, scanning_lan = false, usb_scanned = false, lan_scanned = false,
         rescan = false, rescan_lan = false;
    std::int64_t scanned_at = 0, lan_scanned_at = 0;
    int pulse = 0;
    // Printer editor.
    std::vector<Json> usb_options;
    std::vector<std::string> queue_options;
    // Connection check (runs off the UI thread).
    struct ProbeResult {
        bool ok = false;
        std::string type, code, message, label;
    };
    std::mutex probe_mutex;
    ProbeResult probe_result;
    bool probing = false;
    Page probe_page = Home;
    ~App() {
        stopping = true;
        wake.notify_all();
        if (worker.joinable())
            worker.join();
        if (discovery.joinable())
            discovery.join();
        if (prober.joinable())
            prober.join();
        if (server)
            server->stop();
        for (HFONT f : {font.body, font.bold, font.small, font.tiny, font.title, font.heading,
                        font.big, font.brand, font.mono})
            if (f)
                DeleteObject(f);
        if (page_brush)
            DeleteObject(page_brush);
        if (card_brush)
            DeleteObject(card_brush);
        if (brand_icon)
            DestroyIcon(brand_icon);
    }
    // ---- Small control helpers ----
    HWND ctl(int id) const {
        auto it = controls.find(id);
        return it == controls.end() ? nullptr : it->second.hwnd;
    }
    std::wstring text(int id) const {
        HWND h = ctl(id);
        int n = GetWindowTextLengthW(h);
        std::wstring s(static_cast<std::size_t>(n) + 1, L'\0');
        GetWindowTextW(h, &s[0], n + 1);
        s.resize(static_cast<std::size_t>(n));
        return s;
    }
    void set(int id, const std::wstring &s) { SetWindowTextW(ctl(id), s.c_str()); }
    void relabel(int id, const std::wstring &s) {
        if (text(id) != s)
            set(id, s);
    }
    bool checked(int id) const { return SendMessageW(ctl(id), BM_GETCHECK, 0, 0) == BST_CHECKED; }
    void check(int id, bool on) {
        SendMessageW(ctl(id), BM_SETCHECK, on ? BST_CHECKED : BST_UNCHECKED, 0);
    }
    int pick(int id) const { return static_cast<int>(SendMessageW(ctl(id), CB_GETCURSEL, 0, 0)); }
    void choose(int id, int index) { SendMessageW(ctl(id), CB_SETCURSEL, index, 0); }
    int cursel(int id) const { return static_cast<int>(SendMessageW(ctl(id), LB_GETCURSEL, 0, 0)); }
    int integer(int id) const {
        auto s = text(id);
        if (s.empty() || s.find_first_not_of(L"0123456789") != std::wstring::npos || s.size() > 5)
            throw Error("INVALID_CONFIG", "Enter a valid integer");
        return std::stoi(s);
    }
    void redraw() {
        if (window)
            InvalidateRect(window, nullptr, FALSE);
    }
    void say(Page p, Tone tone, const std::wstring &s) {
        messages[p] = {tone, s};
        redraw();
    }
    void changed() {
        PostMessageW(window, Changed, 0, 0);
        wake.notify_one();
    }
    void error(const std::exception &e) {
        auto agent = dynamic_cast<const Error *>(&e);
        say(page, Failure, agent ? explain(agent->code, e.what()) : w(e.what()));
    }
    int site_count() const { return server ? server->authenticated() : 0; }
    bool recent_reject() const {
        return reject_seen_at && now() - reject_seen_at < 600 && site_count() == 0 &&
               (!server || server->last_authenticated() < reject_seen_at);
    }
    int pending() const {
        int n = 0;
        for (const auto &j : jobs)
            if (j["status"] == "queued" || j["status"] == "printing")
                ++n;
        return n;
    }
    int port() const {
        return server && server->ready() ? server->port() : config["port"].get<int>();
    }
    const Json *printer(const std::string &id) const {
        for (const auto &p : printers)
            if (p["id"] == id)
                return &p;
        return nullptr;
    }
    void update_title() {
        std::wstring title = L"منووکس پرینتر — Legacy";
        if (server && server->ready())
            title += L" | 127.0.0.1:" + std::to_wstring(server->port()) + L" آماده";
        else
            title += L" | اتصال سایت غیرفعال";
        SetWindowTextW(window, title.c_str());
    }
    // ---- Website connection diagnostics ----
    // Runs on the network thread. Log a code only (never the key/proof) and wake the UI thread.
    void rejected(const std::string &code, const std::string &detail) {
        {
            std::lock_guard<std::mutex> lock(reject_mutex);
            reject_code = code;
            reject_detail = detail;
        }
        // A page retrying in a loop must not flood the log or the UI message queue.
        auto key = code + "|" + detail;
        auto t = now();
        bool fresh = key != reject_key;
        reject_key = key;
        if (fresh || t - reject_logged_at >= 60) {
            reject_logged_at = t;
            log_code(directory, code);
        }
        if (window && !stopping && (fresh || t != reject_posted_at)) {
            reject_posted_at = t;
            PostMessageW(window, Rejected, 0, 0);
        }
    }
    void show_rejected() {
        std::string code, detail;
        {
            std::lock_guard<std::mutex> lock(reject_mutex);
            code = reject_code;
            detail = reject_detail;
        }
        if (code.empty())
            return;
        auto d = w(detail);
        if (code == "ORIGIN_NOT_ALLOWED")
            reject_text =
                L"آدرس سایت «" + d +
                L"» مجاز نیست. فقط نشانی‌های رسمی منووکس روی HTTPS وصل می‌شوند: menuvex.ir و "
                L"زیردامنه‌هایش مثل https://app.menuvex.ir و https://www.menuvex.ir.";
        else if (code == "HOST_MISMATCH")
            reject_text = L"آدرس اتصال باید دقیقاً 127.0.0.1:" + std::to_wstring(port()) +
                          L" باشد، نه «" + d + L"».";
        else if (code == "PATH_NOT_ALLOWED")
            reject_text = L"مسیر «" + d + L"» مجاز نیست.";
        else if (code == "TOO_MANY_CONNECTIONS")
            reject_text =
                L"تعداد اتصال‌های باز زیاد است؛ تب‌های اضافی مرورگر را ببندید.";
        else
            reject_text = L"کد " + w(code) + L".";
        bool first = reject_seen_at == 0 || now() - reject_seen_at > 60;
        reject_seen_at = now();
        if (first)
            say(Home, Warning,
                L"یک مرورگر تلاش کرد به این Agent وصل شود اما رد شد. علت و راه‌حل در صفحهٔ «اتصال "
                L"به سایت» است.");
        redraw();
    }
    // ---- Tray ----
    NOTIFYICONDATAW tray_data() {
        NOTIFYICONDATAW data{};
        data.cbSize = sizeof(data);
        data.hWnd = window;
        data.uID = 1;
        data.uFlags = NIF_MESSAGE | NIF_ICON | NIF_TIP;
        data.uCallbackMessage = Tray;
        data.hIcon = icon;
        lstrcpynW(data.szTip, L"منووکس پرینتر — Legacy", 128);
        return data;
    }
    // NIM_ADD is refused when the icon already exists (e.g. a timed-out add that completed later),
    // so confirm with NIM_MODIFY before treating it as a failure.
    bool add_tray() {
        auto data = tray_data();
        tray_ok = Shell_NotifyIconW(NIM_ADD, &data) || Shell_NotifyIconW(NIM_MODIFY, &data);
        return tray_ok;
    }
    void balloon(const std::wstring &title, const std::wstring &body) {
        if (!tray_ok)
            return;
        auto data = tray_data();
        data.uFlags = NIF_INFO;
        lstrcpynW(data.szInfoTitle, title.c_str(), 64);
        lstrcpynW(data.szInfo, body.c_str(), 256);
        data.dwInfoFlags = NIIF_INFO;
        Shell_NotifyIconW(NIM_MODIFY, &data);
    }
    // ---- Local server and print worker ----
    void start_server() {
        if (server)
            server->stop();
        server.reset(new Server(*store, *secret, [this] { changed(); }));
        server->on_rejected(
            [this](const std::string &code, const std::string &detail) { rejected(code, detail); });
        start_failed = false;
        start_code.clear();
        int p = config["port"].get<int>();
        try {
            server->start(static_cast<unsigned short>(p));
        } catch (const Error &e) {
            start_failed = true;
            start_code = e.code;
            log_code(directory, e.code);
        }
        update_title();
    }
    void start_worker() {
        worker = std::thread([this] {
            while (!stopping) {
                try {
                    if (!server->ready()) {
                        std::unique_lock<std::mutex> lock(wake_mutex);
                        wake.wait_for(lock, std::chrono::seconds(1));
                        continue;
                    }
                    auto work = store->claim(now());
                    if (work.is_null()) {
                        std::unique_lock<std::mutex> lock(wake_mutex);
                        wake.wait_for(lock, std::chrono::seconds(1));
                        continue;
                    }
                    auto job = work["job"];
                    server->event({{"type", "print.printing"}, {"version", 1}, {"job", job}});
                    Json failure = nullptr;
                    unsigned sent = 0;
                    const std::string type = work["profile"]["connection"]["type"];
                    try {
                        auto bytes = render(work["profile"], work["document"]);
                        unsigned copies = work["profile"]["copies"];
                        for (; sent < copies; ++sent)
                            if (type == "network")
                                send_network(work["profile"], bytes);
                            else if (type == "usb")
                                send_usb(directory, work["profile"], bytes);
                            else
                                send_spooler(directory, work["profile"], bytes);
                    } catch (const Error &e) {
                        if (sent)
                            failure = Error("PRINT_OUTCOME_UNKNOWN",
                                            type == "network"
                                                ? "A later copy may have partially reached the LAN "
                                                  "printer. Inspect the paper before reprinting."
                                            : type == "usb"
                                                ? "A later copy may have partially reached the USB "
                                                  "printer. Inspect the paper before reprinting."
                                                : "Data may have reached the Windows spooler. "
                                                  "Inspect paper and the Windows print queue "
                                                  "before reprinting.",
                                            false, true)
                                          .json();
                        else
                            failure = e.json();
                        log_code(directory, failure["code"]);
                    } catch (...) {
                        failure = unknown().json();
                        log_code(directory, "PRINT_OUTCOME_UNKNOWN");
                    }
                    auto result =
                        store->finish(job["jobId"], failure, store->config()["maxAttempts"], now());
                    server->event({{"type", "print." + result["status"].get<std::string>()},
                                   {"version", 1},
                                   {"job", result}});
                } catch (...) {
                    worker_failed = true;
                    server->worker_failed();
                    log_code(directory, "QUEUE_ERROR");
                    changed();
                    break;
                }
            }
        });
    }
    // ---- Lists ----
    void fill_list(int id, std::size_t count, int selection) {
        HWND list = ctl(id);
        SendMessageW(list, WM_SETREDRAW, FALSE, 0);
        auto top = SendMessageW(list, LB_GETTOPINDEX, 0, 0);
        SendMessageW(list, LB_RESETCONTENT, 0, 0);
        for (std::size_t i = 0; i < count; ++i)
            SendMessageW(list, LB_ADDSTRING, 0, static_cast<LPARAM>(i));
        if (top > 0 && static_cast<std::size_t>(top) < count)
            SendMessageW(list, LB_SETTOPINDEX, static_cast<WPARAM>(top), 0);
        SendMessageW(list, LB_SETCURSEL, static_cast<WPARAM>(selection), 0);
        SendMessageW(list, WM_SETREDRAW, TRUE, 0);
        InvalidateRect(list, nullptr, TRUE);
    }
    void refresh() {
        config = store->config();
        printers = config["printers"];
        jobs = store->queue();
        int selection = -1;
        for (std::size_t i = 0; i < printers.size(); ++i)
            if (printers[i]["id"] == selected)
                selection = static_cast<int>(i);
        if (selection < 0)
            selected.clear();
        fill_list(PrinterList, printers.size(), selection);
        int job_selection = -1;
        for (std::size_t i = 0; i < jobs.size(); ++i)
            if (jobs[i]["jobId"] == selected_job)
                job_selection = static_cast<int>(i);
        if (job_selection < 0)
            selected_job.clear();
        fill_list(QueueList, jobs.size(), job_selection);
        rebuild_found();
        // Report the outcome of the test print the owner just started.
        if (!watch_test_job.empty())
            for (const auto &j : jobs)
                if (j["jobId"] == watch_test_job) {
                    const std::string status = j["status"];
                    if (status == "completed") {
                        say(Home, Success,
                            L"چاپ آزمایشی «" + w(watch_test_name) +
                                L"» به پرینتر ارسال شد. کاغذ را نگاه کنید: متن فارسی، اعداد و عرض "
                                L"کاغذ باید درست باشد.");
                        watch_test_job.clear();
                    } else if (status == "failed" || status == "cancelled") {
                        say(Home, Failure,
                            L"چاپ آزمایشی «" + w(watch_test_name) + L"» انجام نشد: " +
                                (j["error"].is_null()
                                     ? status_label(status)
                                     : explain(j["error"]["code"], j["error"]["message"])));
                        watch_test_job.clear();
                    } else if (status == "queued" && !j["error"].is_null()) {
                        say(Home, Warning,
                            L"تلاش دوباره برای چاپ آزمایشی: " +
                                explain(j["error"]["code"], j["error"]["message"]));
                    }
                }
        int sites = site_count();
        if (sites > last_site_count)
            say(Home, Success,
                L"سایت منووکس به این Agent وصل شد. سفارش‌ها از این پس روی پرینترهای همین کامپیوتر "
                L"چاپ می‌شوند.");
        last_site_count = sites;
        update_title();
        if (server && !start_failed && !server->ready() && !stopping && !stopped_reported) {
            stopped_reported = true;
            log_code(directory, "SERVER_STOPPED");
        }
        update_visibility();
    }
    // ---- Automatic discovery ----
    void scan(bool with_lan) {
        if (scanning) {
            rescan = true;
            rescan_lan = rescan_lan || with_lan;
            return;
        }
        scanning = true;
        scanning_lan = with_lan;
        Json saved = printers;
        SetTimer(window, PulseTimer, 400, nullptr);
        discovery = std::thread([this, with_lan, saved] {
            Scan result;
            try {
                result.queues = installed_queues();
            } catch (const std::exception &e) {
                result.error = e.what();
            }
            try {
                result.usb = usb_printers();
                result.usb_ok = true;
            } catch (const std::exception &e) {
                result.error = e.what();
            }
            std::vector<Ipv4Interface> interfaces;
            if (with_lan) {
                interfaces = local_interfaces();
                for (const auto &i : interfaces) {
                    auto subnet = format_ipv4(i.address);
                    subnet = subnet.substr(0, subnet.rfind('.')) + ".x";
                    if (!result.scope.empty())
                        result.scope += L"، ";
                    result.scope += w(subnet);
                }
            }
            {
                std::lock_guard<std::mutex> lock(discovery_mutex);
                incoming = result;
            }
            PostMessageW(window, Discovered, 1, with_lan ? 0 : 1);
            if (!with_lan || stopping)
                return;
            // Printers already known to Windows (TCP/IP queues) or saved here are probed first,
            // even when they live outside this PC's own subnet.
            std::vector<std::uint32_t> extra;
            std::uint32_t address = 0;
            for (const auto &q : result.queues)
                if (!q.ipv4.empty() && parse_ipv4(q.ipv4, address))
                    extra.push_back(address);
            for (const auto &p : saved)
                if (p["connection"]["type"] == "network" &&
                    parse_ipv4(p["connection"]["host"].get<std::string>(), address))
                    extra.push_back(address);
            std::vector<std::uint32_t> targets = scan_targets(interfaces, extra);
            if (interfaces.empty())
                for (std::uint32_t base : {0xC0A80100u, 0xC0A80000u})
                    for (std::uint32_t host = 1; host < 255; ++host)
                        targets.push_back(base | host);
            auto hits = probe_raw_printers(targets, 9100, &stopping);
            {
                std::lock_guard<std::mutex> lock(discovery_mutex);
                incoming.lan = std::move(hits);
            }
            PostMessageW(window, Discovered, 2, 1);
        });
        redraw();
    }
    void discovered(WPARAM phase, LPARAM final_phase) {
        bool usb_changed = false, first_usb = !usb_scanned;
        {
            std::lock_guard<std::mutex> lock(discovery_mutex);
            if (phase == 1) {
                queues = incoming.queues;
                if (incoming.usb_ok) {
                    usb = incoming.usb;
                    usb_scanned = true;
                    usb_changed = true;
                }
                scan_error = incoming.error;
                if (!incoming.scope.empty() || scanning_lan)
                    scan_scope = incoming.scope;
            } else {
                lan = incoming.lan;
                lan_scanned = true;
                lan_scanned_at = now();
            }
        }
        if (final_phase) {
            if (discovery.joinable())
                discovery.join();
            scanning = false;
            scanning_lan = false;
            scanned_at = now();
            KillTimer(window, PulseTimer);
        }
        if (usb_changed)
            announce_new_usb(first_usb);
        if (page == Edit && phase == 1)
            refill_editor_options();
        rebuild_found();
        InvalidateRect(ctl(PrinterList), nullptr, FALSE); // USB presence badges
        if (final_phase && rescan) {
            bool lan_too = rescan_lan;
            rescan = rescan_lan = false;
            scan(lan_too);
        }
        update_visibility();
    }
    // Tell the owner about a USB printer that was just plugged in and is not set up yet. The
    // first scan only learns what is already connected.
    void announce_new_usb(bool first_scan) {
        for (const auto &u : usb) {
            if (!known_usb.insert(u.instance).second || first_scan)
                continue;
            auto c = usb_connection(u.device.id.vendor, u.device.id.product, u.device.id.serial,
                                    u.device.ports);
            if (!configured_name(printers, c).empty())
                continue;
            std::wstring name = u.name.empty() ? L"پرینتر USB" : w(u.name);
            say(Home, Info,
                L"پرینتر USB جدید وصل شد: «" + name +
                    L"». برای راه‌اندازی، «افزودن پرینتر» را بزنید و آن را انتخاب کنید.");
            say(Find, Info, L"پرینتر USB جدید پیدا شد: «" + name + L"».");
            balloon(
                L"پرینتر USB جدید پیدا شد",
                name +
                    L" — برای راه‌اندازی، منووکس پرینتر را باز کنید.");
        }
    }
    static std::wstring queue_port_label(const QueueInfo &q) {
        if (q.kind == "usb")
            return L"پورت USB (" + w(q.port) + L")";
        if (q.kind == "network")
            return L"شبکه " + w(q.ipv4.empty() ? q.port : q.ipv4);
        if (q.kind == "wsd")
            return L"شبکه (WSD)";
        if (q.kind == "lpt")
            return L"پورت موازی " + w(q.port);
        if (q.kind == "com")
            return L"پورت سریال " + w(q.port);
        if (q.kind == "shared")
            return L"پرینتر اشتراکی کامپیوتر دیگر";
        return L"پورت " + w(q.port);
    }
    void rebuild_found() {
        Json previous;
        int selection = cursel(FoundList);
        if (selection >= 0 && static_cast<std::size_t>(selection) < found.size())
            previous = found[static_cast<std::size_t>(selection)].connection;
        std::vector<Found> next;
        for (const auto &u : usb) {
            Found f;
            f.kind = "usb";
            f.connection = usb_connection(u.device.id.vendor, u.device.id.product,
                                          u.device.id.serial, u.device.ports);
            f.name = u.name.empty() || same_text(u.name, "USB Printing Support") ? "" : u.name;
            f.title = f.name.empty() ? L"پرینتر USB" : w(f.name);
            if (f.name.empty())
                f.name = utf8(L"پرینتر USB");
            f.detail = L"USB مستقیم — بدون نیاز به درایور" +
                       (u.port.empty() ? std::wstring() : L" · " + w(u.port)) + L" · " +
                       usb_id(f.connection);
            for (const auto &q : queues)
                if (!u.port.empty() && q.kind == "usb" && same_text(q.port, u.port))
                    f.note = L"صف ویندوز «" + w(q.name) + L"» هم به همین پرینتر وصل است.";
            f.paper = guess_paper_mm(f.name);
            next.push_back(f);
        }
        for (auto address : lan) {
            Found f;
            f.kind = "network";
            const std::string ip = format_ipv4(address);
            f.connection = {{"type", "network"}, {"host", ip}, {"port", 9100}};
            f.name = utf8(L"پرینتر شبکه ") + ip;
            f.title = L"پرینتر شبکه " + w(ip);
            f.detail = L"شبکهٔ داخلی (LAN) · " + w(ip) + L":9100 · به درخواست اتصال پاسخ داد";
            for (const auto &q : queues)
                if (q.ipv4 == ip)
                    f.note = L"صف ویندوز «" + w(q.name) +
                             L"» هم به همین IP چاپ می‌کند.";
            next.push_back(f);
        }
        for (const auto &q : queues) {
            if (q.kind == "virtual")
                continue; // XPS, PDF, Fax, OneNote: never a receipt printer
            Found f;
            f.kind = "spooler";
            f.connection = {{"type", "spooler"}, {"queueName", q.name}};
            f.name = q.name;
            f.title = w(q.name);
            f.detail = L"صف چاپ ویندوز · " + queue_port_label(q) +
                       (q.driver.empty() ? std::wstring() : L" · درایور: " + w(q.driver));
            if (q.offline)
                f.note = L"ویندوز این صف را «آفلاین» نشان می‌دهد.";
            f.paper = guess_paper_mm(q.name + " " + q.driver);
            next.push_back(f);
        }
        for (auto &f : next)
            f.configured = configured_name(printers, f.connection);
        // USB first, then LAN, then Windows queues; ones that are already set up sink down.
        auto rank = [](const Found &f) {
            int group = f.kind == "usb" ? 0 : f.kind == "network" ? 1 : 2;
            return group * 2 + (f.configured.empty() ? 0 : 1);
        };
        std::stable_sort(next.begin(), next.end(),
                         [&](const Found &a, const Found &b) { return rank(a) < rank(b); });
        found = std::move(next);
        int keep = -1;
        for (std::size_t i = 0; i < found.size(); ++i)
            if (!previous.is_null() && found[i].connection == previous)
                keep = static_cast<int>(i);
        if (keep < 0 && !found.empty() && found.front().configured.empty())
            keep = 0;
        fill_list(FoundList, found.size(), keep);
    }
    bool usb_present(const Json &connection) const {
        std::vector<UsbCandidate> present;
        for (const auto &u : usb)
            present.push_back(u.device);
        try {
            select_usb_device(connection, present);
            return true;
        } catch (const Error &e) {
            return e.code != "PRINTER_OFFLINE";
        }
    }
    std::wstring usb_name(const Json &connection) const {
        std::vector<UsbCandidate> present;
        for (const auto &u : usb)
            present.push_back(u.device);
        try {
            const auto &u = usb[select_usb_device(connection, present)];
            if (!u.name.empty() && !same_text(u.name, "USB Printing Support"))
                return w(u.name);
        } catch (...) {
        }
        return L"";
    }
    std::wstring connection_text(const Json &c) const {
        const std::string type = c.value("type", std::string());
        if (type == "network")
            return L"شبکه · " + w(c["host"].get<std::string>()) + L":" +
                   std::to_wstring(c["port"].get<int>());
        if (type == "usb") {
            auto name = usb_name(c);
            return L"USB مستقیم · " + (name.empty() ? usb_id(c) : name + L" (" + usb_id(c) + L")");
        }
        return L"صف ویندوز · " + w(c.value("queueName", std::string()));
    }
    Badge printer_badge(const Json &p) const {
        const std::string id = p["id"];
        for (const auto &j : jobs)
            if (j["printerId"] == id && j["status"] == "printing")
                return {L"در حال چاپ", ink::blue, ink::blue_soft};
        if (p["connection"]["type"] == "usb" && usb_scanned && !usb_present(p["connection"]))
            return {L"USB وصل نیست", ink::amber, ink::amber_soft};
        for (const auto &j : jobs) {
            if (j["printerId"] != id || j["status"] == "cancelled")
                continue;
            if (j["status"] == "completed")
                return {L"آخرین چاپ ارسال شد", ink::green, ink::green_soft};
            if (j["status"] == "failed")
                return {L"آخرین چاپ ناموفق", ink::red, ink::red_soft};
            if (j["status"] == "queued")
                return {L"در صف چاپ", ink::blue, ink::blue_soft};
        }
        return {L"هنوز چاپی نداشته", ink::muted, ink::gray_soft};
    }
    // ---- Visibility / enabled state ----
    int draft_type() const { return std::max(0, pick(ConnType)); } // 0 usb, 1 network, 2 queue
    bool wanted(int id, const Ctl &c) const {
        if (c.page != -1 && c.page != page)
            return false;
        switch (id) {
        case PrinterList:
            return !printers.empty();
        case FoundList:
            return !found.empty();
        case QueueList:
            return !jobs.empty();
        case GoPair:
            return site_count() == 0;
        case UsbPick:
            return draft_type() == 0;
        case QueuePick:
            return draft_type() == 2;
        case RefreshPick:
            return draft_type() != 1;
        case Host:
        case NetPort:
        case CheckEdit:
            return draft_type() == 1;
        case DeleteEdit:
            return !draft_id.empty();
        default:
            return true;
        }
    }
    void update_visibility() {
        for (const auto &entry : controls) {
            bool show = wanted(entry.first, entry.second);
            bool shown = (GetWindowLongW(entry.second.hwnd, GWL_STYLE) & WS_VISIBLE) != 0;
            if (show != shown)
                ShowWindow(entry.second.hwnd, show ? SW_SHOWNA : SW_HIDE);
        }
        bool has_printer = !selected.empty() && printer(selected);
        EnableWindow(ctl(TestPrinter), has_printer && !worker_failed);
        EnableWindow(ctl(ProbePrinter), has_printer && !probing);
        EnableWindow(ctl(EditPrinter), has_printer);
        EnableWindow(ctl(RemovePrinter), has_printer);
        int f = cursel(FoundList);
        EnableWindow(ctl(AddFound), f >= 0 && static_cast<std::size_t>(f) < found.size());
        relabel(AddFound, f >= 0 && static_cast<std::size_t>(f) < found.size() &&
                                  !found[static_cast<std::size_t>(f)].configured.empty()
                              ? L"ویرایش پرینتر انتخاب‌شده"
                              : L"افزودن پرینتر انتخاب‌شده");
        EnableWindow(ctl(Rescan), !scanning);
        relabel(Rescan, scanning ? L"در حال جستجو…" : L"جستجوی دوباره");
        EnableWindow(ctl(RefreshPick), !scanning);
        relabel(RefreshPick,
                draft_type() == 0 ? L"جستجوی دوباره" : L"تازه‌سازی فهرست");
        EnableWindow(ctl(CheckEdit), !probing);
        bool cancellable = false;
        for (const auto &j : jobs)
            if (j["jobId"] == selected_job && j["status"] == "queued")
                cancellable = true;
        EnableWindow(ctl(CancelJob), cancellable);
        EnableWindow(ctl(ClearHistory), !jobs.empty());
        for (int id : {NavHome, NavFind, NavPair, NavQueue, NavSettings})
            InvalidateRect(ctl(id), nullptr, FALSE);
        redraw();
    }
    void show_page(Page p) {
        page = p;
        update_visibility();
        int focus = p == Home       ? NavHome
                    : p == Find     ? NavFind
                    : p == Pair     ? NavPair
                    : p == Queue    ? NavQueue
                    : p == Settings ? NavSettings
                                    : Name;
        SetFocus(ctl(focus));
    }
    void navigate(Page p) {
        messages.erase(p);
        show_page(p);
        // Opening the "add printer" page always shows a fresh automatic search.
        if (p == Find && (!lan_scanned_at || now() - lan_scanned_at > 30))
            scan(true);
    }
    // ---- Printer editor ----
    void fill_usb_options(const Json &current) {
        usb_options.clear();
        HWND combo = ctl(UsbPick);
        SendMessageW(combo, CB_RESETCONTENT, 0, 0);
        int selection = -1;
        for (const auto &u : usb) {
            auto c = usb_connection(u.device.id.vendor, u.device.id.product, u.device.id.serial,
                                    u.device.ports);
            std::wstring label =
                (u.name.empty() || same_text(u.name, "USB Printing Support") ? L"پرینتر USB"
                                                                             : w(u.name)) +
                L" — " + (u.port.empty() ? L"" : w(u.port) + L" · ") + usb_id(c);
            if (!current.is_null() && same_connection(current, c))
                selection = static_cast<int>(usb_options.size());
            usb_options.push_back(c);
            SendMessageW(combo, CB_ADDSTRING, 0, reinterpret_cast<LPARAM>(label.c_str()));
        }
        if (!current.is_null() && selection < 0) {
            std::wstring label = L"(الان وصل نیست) پرینتر USB — " + usb_id(current);
            usb_options.insert(usb_options.begin(), current);
            SendMessageW(combo, CB_INSERTSTRING, 0, reinterpret_cast<LPARAM>(label.c_str()));
            selection = 0;
        }
        choose(UsbPick, selection >= 0 ? selection : (usb_options.empty() ? -1 : 0));
    }
    void fill_queue_options(const std::string &current) {
        queue_options.clear();
        HWND combo = ctl(QueuePick);
        SendMessageW(combo, CB_RESETCONTENT, 0, 0);
        int selection = -1;
        for (int pass = 0; pass < 2; ++pass)
            for (const auto &q : queues) {
                if ((q.kind == "virtual") != (pass == 1))
                    continue; // receipt-capable queues first, XPS/PDF/Fax last
                if (same_text(q.name, current))
                    selection = static_cast<int>(queue_options.size());
                queue_options.push_back(q.name);
                std::wstring label = w(q.name) + L" — " + queue_port_label(q);
                SendMessageW(combo, CB_ADDSTRING, 0, reinterpret_cast<LPARAM>(label.c_str()));
            }
        if (!current.empty() && selection < 0) {
            queue_options.insert(queue_options.begin(), current);
            std::wstring label = w(current) + L" — (در فهرست ویندوز پیدا نشد)";
            SendMessageW(combo, CB_INSERTSTRING, 0, reinterpret_cast<LPARAM>(label.c_str()));
            selection = 0;
        }
        choose(QueuePick, selection >= 0 ? selection : (queue_options.empty() ? -1 : 0));
    }
    // After "search again" on the editor: new lists, same choice when it is still there.
    void refill_editor_options() {
        Json current_usb;
        std::string current_queue;
        int u = pick(UsbPick), q = pick(QueuePick);
        if (u >= 0 && static_cast<std::size_t>(u) < usb_options.size())
            current_usb = usb_options[static_cast<std::size_t>(u)];
        if (q >= 0 && static_cast<std::size_t>(q) < queue_options.size())
            current_queue = queue_options[static_cast<std::size_t>(q)];
        fill_usb_options(current_usb);
        fill_queue_options(current_queue);
        say(Edit, Info,
            L"فهرست به‌روز شد: " + fa(static_cast<long long>(usb.size())) +
                L" پرینتر USB و " + fa(static_cast<long long>(queues.size())) + L" صف ویندوز.");
    }
    void set_paper(int mm) {
        check(Paper80, mm != 58);
        check(Paper58, mm == 58);
    }
    // `profile` may be a complete saved printer or a partial suggestion from discovery.
    void open_editor(const Json &profile, bool is_new, Page back) {
        draft_id = is_new ? std::string() : profile.value("id", std::string());
        edit_return = back;
        const Json c = profile["connection"];
        const std::string type = c.value("type", std::string("network"));
        fill_usb_options(type == "usb" ? c : Json());
        fill_queue_options(type == "spooler" ? c.value("queueName", std::string()) : "");
        choose(ConnType, type == "usb" ? 0 : type == "network" ? 1 : 2);
        set(Host, type == "network" ? w(c.value("host", std::string())) : L"");
        set(NetPort, std::to_wstring(type == "network" ? c.value("port", 9100) : 9100));
        set(Name, w(profile.value("name", std::string())));
        int paper = profile.value("paperMm", 80);
        set_paper(paper);
        set(Dots, std::to_wstring(profile.value("widthDots", paper == 58 ? 384 : 576)));
        set(Font, std::to_wstring(profile.value("fontSize", 24)));
        choose(Copies, std::min(std::max(profile.value("copies", 1), 1), 3) - 1);
        check(Cutter, profile.value("cut", true));
        int role = 0;
        bool auto_print = false, invoice_taken = false;
        for (const auto &r : config["routes"]) {
            if (r["role"] == "invoice")
                invoice_taken = true;
            if (!draft_id.empty() && r["printerId"] == draft_id) {
                const std::string name = r["role"];
                role = name == "invoice" ? 1 : name == "kitchen" ? 2 : name == "bar" ? 3 : 0;
                auto_print = r["autoPrint"];
            }
        }
        // A brand-new first printer is most likely the cashier's invoice printer.
        if (is_new && !invoice_taken)
            role = 1;
        choose(Role, role);
        check(AutoPrint, auto_print);
        messages.erase(Edit);
        show_page(Edit);
        SendMessageW(ctl(Name), EM_SETSEL, 0, -1);
    }
    Json draft_connection() const {
        switch (draft_type()) {
        case 0: {
            int i = pick(UsbPick);
            if (i < 0 || static_cast<std::size_t>(i) >= usb_options.size())
                throw Error("INVALID_CONFIG", "Select a USB printer");
            return usb_options[static_cast<std::size_t>(i)];
        }
        case 1: {
            auto host = utf8(text(Host));
            host.erase(std::remove(host.begin(), host.end(), ' '), host.end());
            if (host.empty())
                throw Error("INVALID_CONFIG", "Enter the printer IP");
            return {{"type", "network"}, {"host", host}, {"port", integer(NetPort)}};
        }
        default: {
            int i = pick(QueuePick);
            if (i < 0 || static_cast<std::size_t>(i) >= queue_options.size())
                throw Error("INVALID_CONFIG", "Select a Windows queue");
            return {{"type", "spooler"}, {"queueName", queue_options[static_cast<std::size_t>(i)]}};
        }
        }
    }
    void save_editor(bool and_test) {
        auto c = store->config();
        Json connection = draft_connection();
        auto name = text(Name);
        while (!name.empty() && name.back() == L' ')
            name.pop_back();
        while (!name.empty() && name.front() == L' ')
            name.erase(name.begin());
        if (name.empty())
            throw Error("INVALID_CONFIG", "Enter a printer name");
        std::string id = draft_id;
        if (id.empty()) {
            auto random = random_base64();
            random.erase(
                std::remove_if(random.begin(), random.end(),
                               [](char ch) { return ch == '/' || ch == '+' || ch == '='; }),
                random.end());
            id = "printer:" + random;
        }
        Json p = {{"id", id},
                  {"name", utf8(name)},
                  {"connection", connection},
                  {"paperMm", checked(Paper58) ? 58 : 80},
                  {"widthDots", integer(Dots)},
                  {"copies", std::max(0, pick(Copies)) + 1},
                  {"cut", checked(Cutter)},
                  {"fontFamily", "Noto Sans Arabic"},
                  {"fontSize", integer(Font)}};
        Json list = Json::array();
        bool replaced = false;
        for (auto &old : c["printers"])
            if (old["id"] == id) {
                list.push_back(p);
                replaced = true;
            } else
                list.push_back(old);
        if (!replaced)
            list.push_back(p);
        c["printers"] = list;
        int role = pick(Role);
        const char *roles[] = {"", "invoice", "kitchen", "bar"};
        Json routes = Json::array();
        for (auto &r : c["routes"])
            if (r["printerId"] != id && (role <= 0 || r["role"] != roles[role]))
                routes.push_back(r);
        if (role > 0 && role < 4)
            routes.push_back(
                {{"role", roles[role]}, {"printerId", id}, {"autoPrint", checked(AutoPrint)}});
        c["routes"] = routes;
        validate_config(c);
        store->save(c);
        config = c;
        server->event({{"type", "resync"}, {"version", 1}});
        selected = id;
        draft_id.clear();
        refresh();
        show_page(Home);
        say(Home, Success,
            L"«" + name + L"» ذخیره شد. " +
                (and_test ? std::wstring(L"چاپ آزمایشی در صف است…")
                          : std::wstring(L"برای اطمینان «چاپ آزمایشی» را بزنید.")));
        if (and_test)
            test(id);
    }
    // ---- Printer actions ----
    void test(const std::string &id) {
        if (worker_failed)
            throw Error("AGENT_NOT_READY", "Worker stopped; restart first");
        const Json *profile = printer(id);
        if (!profile)
            throw Error("PRINTER_NOT_FOUND", "Selected printer profile no longer exists");
        auto job_id = "test:" + std::to_string(now()) + ":" + std::to_string(GetTickCount());
        auto j = store->enqueue(job_id, id, printer_test_document(*profile));
        server->event({{"type", "print.queued"}, {"version", 1}, {"job", j}});
        watch_test_job = job_id;
        watch_test_name = (*profile)["name"].get<std::string>();
        wake.notify_one();
        say(Home, Info, L"چاپ آزمایشی «" + w(watch_test_name) + L"» به صف رفت…");
        refresh();
    }
    void remove(const std::string &id) {
        const Json *profile = printer(id);
        if (!profile)
            return;
        std::wstring name = w((*profile)["name"]);
        std::wstring question =
            L"پرینتر «" + name +
            L"» حذف شود؟\nسفارش‌هایی که سایت برای آن می‌فرستد دیگر چاپ نمی‌شوند. کارهای معلق قبلی "
            L"را جداگانه بررسی کنید.";
        if (MessageBoxW(window, question.c_str(), L"حذف پرینتر",
                        MB_YESNO | MB_ICONWARNING | MB_RTLREADING | MB_RIGHT) != IDYES)
            return;
        auto c = store->config();
        Json list = Json::array(), routes = Json::array();
        for (auto &p : c["printers"])
            if (p["id"] != id)
                list.push_back(p);
        for (auto &r : c["routes"])
            if (r["printerId"] != id)
                routes.push_back(r);
        c["printers"] = list;
        c["routes"] = routes;
        store->save(c);
        server->event({{"type", "resync"}, {"version", 1}});
        selected.clear();
        draft_id.clear();
        refresh();
        show_page(Home);
        say(Home, Success, L"«" + name + L"» حذف شد.");
    }
    void probe(const Json &profile, Page where) {
        if (probing)
            return;
        if (prober.joinable())
            prober.join();
        probing = true;
        probe_page = where;
        say(where, Info, L"در حال بررسی اتصال…");
        update_visibility();
        prober = std::thread([this, profile] {
            ProbeResult result;
            result.type = profile["connection"].value("type", std::string());
            result.label = profile.value("name", std::string());
            try {
                if (result.type == "network")
                    probe_network(profile);
                else if (result.type == "usb")
                    probe_usb(profile);
                else {
                    const std::string queue = profile["connection"]["queueName"];
                    bool present = false;
                    for (const auto &q : installed_queues())
                        if (same_text(q.name, queue)) {
                            present = true;
                            if (q.offline)
                                result.code = "QUEUE_OFFLINE";
                        }
                    if (!present)
                        throw Error("SPOOLER_UNAVAILABLE", "Queue not found");
                }
                result.ok = true;
            } catch (const Error &e) {
                result.code = e.code;
                result.message = e.what();
            } catch (const std::exception &e) {
                result.code = "PROBE_FAILED";
                result.message = e.what();
            }
            {
                std::lock_guard<std::mutex> lock(probe_mutex);
                probe_result = result;
            }
            PostMessageW(window, Probed, 0, 0);
        });
    }
    void probed() {
        if (prober.joinable())
            prober.join();
        probing = false;
        ProbeResult r;
        {
            std::lock_guard<std::mutex> lock(probe_mutex);
            r = probe_result;
        }
        if (!r.ok)
            say(probe_page, Failure, explain(r.code, r.message));
        else if (r.type == "network")
            say(probe_page, Success,
                L"اتصال برقرار است: پرینتر شبکه به درخواست اتصال پاسخ داد. برای اطمینان از چاپ روی "
                L"کاغذ، یک چاپ آزمایشی بگیرید.");
        else if (r.type == "usb")
            say(probe_page, Success,
                L"پرینتر USB وصل است و آمادهٔ دریافت داده است. برای اطمینان یک چاپ آزمایشی بگیرید.");
        else if (r.code == "QUEUE_OFFLINE")
            say(probe_page, Warning, explain("QUEUE_OFFLINE"));
        else
            say(probe_page, Success,
                L"صف ویندوز پیدا شد. ویندوز وضعیت فیزیکی پرینتر را گزارش نمی‌دهد؛ یک چاپ آزمایشی "
                L"بگیرید.");
        update_visibility();
    }
    void cancel_job() {
        if (selected_job.empty())
            return;
        auto j = store->cancel(selected_job);
        server->event({{"type", "print.cancelled"}, {"version", 1}, {"job", j}});
        refresh();
        say(Queue, Success, L"کار انتخاب‌شده لغو شد و چاپ نمی‌شود.");
    }
    void clear_history() {
        if (MessageBoxW(
                window,
                L"سابقهٔ صف چاپ پاک شود؟\nکارهای «در صف» لغو می‌شوند و کارِ در حال چاپ دست "
                L"نمی‌خورد.",
                L"پاک کردن سابقه", MB_YESNO | MB_ICONWARNING | MB_RTLREADING | MB_RIGHT) != IDYES)
            return;
        auto result = store->clear();
        for (const auto &job : result["cancelledJobs"])
            server->event({{"type", "print.cancelled"}, {"version", 1}, {"job", job}});
        server->event({{"type", "resync"}, {"version", 1}});
        refresh();
        say(Queue, Success, L"سابقهٔ صف پاک شد.");
    }
    void save_settings() {
        const auto before = store->config();
        auto c = before;
        c["port"] = integer(Port);
        c["maxAttempts"] = integer(Attempts);
        c["autostart"] = checked(AutoStart);
        validate_config(c);
        autostart(c["autostart"]);
        store->save(c);
        server->event({{"type", "resync"}, {"version", 1}});
        refresh();
        bool port_changed = c["port"] != before["port"] ||
                            (server && server->ready() && c["port"] != Json(server->port()));
        say(Settings, Success,
            port_changed
                ? L"ذخیره شد. پورت جدید پس از خروج کامل و اجرای دوبارهٔ برنامه فعال می‌شود؛ در "
                  L"سایت هم همین پورت را تنظیم کنید."
                : L"تنظیمات ذخیره شد.");
    }
    // ---- Pairing ----
    void reveal_key() {
        set(SecretBox, w(secret->reveal()));
        SetTimer(window, SecretTimer, 60000, nullptr);
        say(Pair, Info, L"کلید تا ۶۰ ثانیه نمایش داده می‌شود.");
    }
    void copy_key() {
        std::wstring key = w(secret->reveal());
        if (!OpenClipboard(window))
            throw Error("CLIPBOARD_BUSY", "Clipboard is in use; try again");
        EmptyClipboard();
        HGLOBAL memory = GlobalAlloc(GMEM_MOVEABLE, (key.size() + 1) * sizeof(wchar_t));
        if (memory) {
            if (void *target = GlobalLock(memory)) {
                std::copy(key.c_str(), key.c_str() + key.size() + 1,
                          static_cast<wchar_t *>(target));
                GlobalUnlock(memory);
                if (!SetClipboardData(CF_UNICODETEXT, memory))
                    GlobalFree(memory);
            } else {
                GlobalFree(memory);
            }
        }
        // Keep the key out of cloud clipboard/history on newer Windows versions.
        if (HGLOBAL marker = GlobalAlloc(GMEM_MOVEABLE, sizeof(DWORD))) {
            UINT format = RegisterClipboardFormatW(L"ExcludeClipboardContentFromMonitorProcessing");
            if (!format || !SetClipboardData(format, marker))
                GlobalFree(marker);
        }
        CloseClipboard();
        SecureZeroMemory(&key[0], key.size() * sizeof(wchar_t));
        SetTimer(window, ClipboardTimer, 60000, nullptr);
        say(Pair, Success,
            L"کلید کپی شد. در پنجرهٔ Pair سایت با Ctrl+V بچسبانید. پس از ۶۰ ثانیه از حافظهٔ کپی "
            L"پاک می‌شود.");
    }
    void forget_clipboard_key() {
        if (!OpenClipboard(window))
            return;
        if (HANDLE data = GetClipboardData(CF_UNICODETEXT)) {
            if (auto *current = static_cast<const wchar_t *>(GlobalLock(data))) {
                std::wstring value = current;
                GlobalUnlock(data);
                if (value == w(secret->reveal()))
                    EmptyClipboard();
                SecureZeroMemory(&value[0], value.size() * sizeof(wchar_t));
            }
        }
        CloseClipboard();
    }
    void rotate_key() {
        if (MessageBoxW(
                window,
                L"اتصال همهٔ مرورگرها قطع و کلید جدید ساخته شود؟\nبرای چاپ خودکار باید دوباره از "
                L"سایت Pair کنید.",
                L"قطع اتصال مرورگرها",
                MB_YESNO | MB_ICONWARNING | MB_RTLREADING | MB_RIGHT) != IDYES)
            return;
        secret->rotate();
        server->revoke();
        set(SecretBox, L"");
        say(Pair, Success, L"اتصال همهٔ مرورگرها قطع شد و کلید جدید ساخته شد.");
    }
    void open_folder() {
        ShellExecuteW(window, L"open", directory.c_str(), nullptr, nullptr, SW_SHOWNORMAL);
    }
    void quit() {
        if (stopping.exchange(true))
            return;
        say(page, Info, L"پس از عملیات چاپ جاری خارج می‌شود…");
        UpdateWindow(window);
        wake.notify_all();
        if (worker.joinable())
            worker.join();
        if (discovery.joinable())
            discovery.join();
        if (prober.joinable())
            prober.join();
        server->stop();
        if (device_notify)
            UnregisterDeviceNotification(device_notify);
        device_notify = nullptr;
        NOTIFYICONDATAW data{};
        data.cbSize = sizeof(data);
        data.hWnd = window;
        data.uID = 1;
        Shell_NotifyIconW(NIM_DELETE, &data);
        DestroyWindow(window);
    }
    // ---- Painting ----
    std::wstring page_title() const {
        switch (page) {
        case Find:
            return L"افزودن پرینتر — جستجوی خودکار";
        case Edit:
            return draft_id.empty() ? L"تنظیم پرینتر جدید" : L"ویرایش پرینتر";
        case Pair:
            return L"اتصال به سایت منووکس";
        case Queue:
            return L"صف چاپ";
        case Settings:
            return L"تنظیمات";
        default:
            return L"پرینترها";
        }
    }
    void paint_message(HDC dc, Page p, RECT r) {
        auto it = messages.find(p);
        Message m;
        if (it != messages.end())
            m = it->second;
        if (p == Home) {
            // Problems that stop printing always win over routine feedback.
            if (worker_failed)
                m = {Failure, explain("QUEUE_ERROR")};
            else if (start_failed)
                m = {Failure, L"اتصال سایت غیرفعال است: پورت 127.0.0.1:" +
                                  std::to_wstring(config["port"].get<int>()) + L" در دسترس نیست. " +
                                  explain("PORT_IN_USE_OR_UNAVAILABLE")};
            else if (stopped_reported)
                m = {Failure,
                     L"سرویس اتصال سایت متوقف شد (SERVER_STOPPED). برنامه را ببندید و دوباره اجرا "
                     L"کنید."};
            else if (m.text.empty() && printers.empty())
                m = {
                    Info,
                    L"برای شروع، «افزودن پرینتر» را بزنید؛ پرینترهای USB، شبکه و صف‌های ویندوز "
                    L"خودکار پیدا می‌شوند."};
            else if (m.text.empty() && site_count() == 0)
                m = {
                    Info,
                    L"پرینترها آماده‌اند. برای چاپ خودکار سفارش‌ها، سایت را از صفحهٔ «اتصال به "
                    L"سایت» وصل کنید."};
            else if (m.text.empty())
                m = {
                    Success,
                    L"همه‌چیز آماده است: سایت وصل است و سفارش‌ها روی پرینترهای همین کامپیوتر چاپ "
                    L"می‌شوند."};
        }
        if (m.text.empty())
            return;
        COLORREF fg = m.tone == Success   ? ink::green
                      : m.tone == Warning ? ink::amber
                      : m.tone == Failure ? ink::red
                                          : ink::blue_dark;
        COLORREF bg = m.tone == Success   ? ink::green_soft
                      : m.tone == Warning ? ink::amber_soft
                      : m.tone == Failure ? ink::red_soft
                                          : ink::blue_soft;
        box(dc, r, bg, bg, 12);
        dot(dc, r.right - 22, r.top + (r.bottom - r.top) / 2 - 4, 8, fg);
        RECT t{r.left + 14, r.top + 6, r.right - 36, r.bottom - 4};
        RECT measure = t;
        SelectObject(dc, font.body);
        DrawTextW(dc, m.text.c_str(), static_cast<int>(m.text.size()), &measure,
                  DT_RIGHT | DT_RTLREADING | DT_WORDBREAK | DT_CALCRECT | DT_NOPREFIX);
        int height = measure.bottom - measure.top;
        if (height < t.bottom - t.top)
            t.top += (t.bottom - t.top - height) / 2;
        para(dc, m.text, t, font.body, fg);
    }
    void paint_sidebar(HDC dc) {
        RECT side{SideLeft, 0, ClientW, ClientH};
        fill(dc, side, ink::card);
        fill(dc, {SideLeft, 0, SideLeft + 1, ClientH}, ink::line);
        if (brand_icon)
            DrawIconEx(dc, ClientW - 22 - 40, 24, brand_icon, 40, 40, 0, nullptr, DI_NORMAL);
        draw(dc, L"MenuVex", {SideLeft + 10, 22, ClientW - 72, 48}, font.brand, ink::text,
             DT_RIGHT | DT_SINGLELINE | DT_VCENTER);
        SetTextCharacterExtra(dc, 2);
        draw(dc, L"PRINTER AGENT", {SideLeft + 10, 48, ClientW - 72, 64}, font.tiny, ink::faint,
             DT_RIGHT | DT_SINGLELINE | DT_VCENTER);
        SetTextCharacterExtra(dc, 0);
        pill(dc, ClientW - 22, 80, 22, L"نسخهٔ ویندوز ۷", font.tiny, ink::violet, ink::violet_soft,
             true, false);
        rtl(dc, L"فضای چاپ شما", {SideLeft + 14, 118, ClientW - 26, 138}, font.small, ink::faint);
        fill(dc, {SideLeft + 18, 470, ClientW - 18, 471}, ink::line);
        rtl(dc, L"محلی. امن. همیشه آماده.", {SideLeft + 14, 482, ClientW - 22, 502}, font.bold,
            RGB(81, 97, 138));
        para(
            dc,
            L"چاپ مستقیم سفارش‌های MenuVex روی پرینترهای همین کامپیوتر؛ بدون نصب درایور و دستور خط "
            L"فرمان.",
            {SideLeft + 14, 506, ClientW - 22, 570}, font.small, ink::muted);
        draw(dc, L"v1.0.0 · Legacy", {SideLeft + 14, 600, ClientW - 22, 620}, font.mono, ink::faint,
             DT_RIGHT | DT_SINGLELINE | DT_VCENTER);
    }
    void paint_header(HDC dc) {
        fill(dc, {0, 0, SideLeft, HeaderH}, ink::card);
        fill(dc, {0, HeaderH - 1, SideLeft, HeaderH}, ink::line);
        rtl(dc, L"MenuVex  /  مدیریت چاپ", {Left + 360, 8, Right, 26}, font.small, ink::muted);
        rtl(dc, page_title(), {Left + 360, 25, Right, 53}, font.title, ink::text);
        int x = Left;
        RECT agent;
        if (worker_failed)
            agent = pill(dc, x, 17, 26, L"چاپ متوقف شده", font.small, ink::red, ink::red_soft);
        else if (server && server->ready())
            agent = pill(dc, x, 17, 26,
                         L"Agent آماده است · 127.0.0.1:" + std::to_wstring(server->port()),
                         font.small, ink::green, ink::green_soft);
        else
            agent = pill(dc, x, 17, 26, L"اتصال محلی غیرفعال", font.small, ink::red, ink::red_soft);
        x = agent.right + 8;
        int sites = site_count();
        if (sites > 0)
            pill(dc, x, 17, 26, L"سایت منووکس وصل است", font.small, ink::green, ink::green_soft);
        else if (recent_reject())
            pill(dc, x, 17, 26, L"اتصال سایت رد شد", font.small, ink::red, ink::red_soft);
        else
            pill(dc, x, 17, 26, L"سایت هنوز وصل نشده", font.small, ink::muted, ink::gray_soft);
    }
    void paint_empty(HDC dc, RECT r, const std::wstring &title, const std::wstring &body) {
        fill(dc, r, ink::page);
        SelectObject(dc, GetStockObject(NULL_BRUSH));
        HPEN pen = CreatePen(PS_DOT, 1, RGB(205, 217, 236));
        HGDIOBJ old = SelectObject(dc, pen);
        SetBkMode(dc, TRANSPARENT);
        RoundRect(dc, r.left, r.top, r.right, r.bottom, 14, 14);
        SelectObject(dc, old);
        DeleteObject(pen);
        int mid = (r.top + r.bottom) / 2;
        draw(dc, title, {r.left + 20, mid - 40, r.right - 20, mid - 12}, font.heading,
             RGB(93, 113, 145), DT_CENTER | DT_RTLREADING | DT_SINGLELINE | DT_VCENTER);
        draw(dc, body, {r.left + 40, mid - 4, r.right - 40, r.bottom - 8}, font.body, ink::muted,
             DT_CENTER | DT_RTLREADING | DT_WORDBREAK);
    }
    void paint_home(HDC dc) {
        RECT summary{Left, 76, Right, 150};
        box(dc, summary, ink::card, ink::line);
        int c1 = Right - 200, c2 = Right - 400;
        fill(dc, {c1, 90, c1 + 1, 136}, ink::line);
        fill(dc, {c2, 90, c2 + 1, 136}, ink::line);
        rtl(dc, L"پرینترهای تنظیم‌شده", {c1 + 12, 86, Right - 18, 104},
            font.small, ink::muted);
        rtl(dc, fa(static_cast<long long>(printers.size())), {c1 + 12, 104, Right - 18, 142},
            font.big, ink::text);
        rtl(dc, L"در انتظار چاپ", {c2 + 12, 86, c1 - 18, 104}, font.small, ink::muted);
        rtl(dc, fa(pending()), {c2 + 12, 104, c1 - 18, 142}, font.big,
            pending() ? ink::blue : ink::text);
        rtl(dc, L"اتصال سایت منووکس", {Left + 12, 86, c2 - 18, 104}, font.small, ink::muted);
        int sites = site_count();
        if (sites > 0)
            rtl(dc, L"وصل است · " + fa(sites) + L" مرورگر", {Left + 12, 106, c2 - 18, 138},
                font.heading, ink::green);
        else
            rtl(dc, recent_reject() ? L"تلاش اتصال رد شد" : L"هنوز وصل نشده",
                {Left + 160, 106, c2 - 18, 138}, font.heading,
                recent_reject() ? ink::red : ink::muted);
        rtl(dc, L"پرینترهای شما", {Right - 260, 162, Right, 196}, font.heading, ink::text);
        int title_width = width_of(dc, L"پرینترهای شما", font.heading);
        RECT chip{Right - title_width - 44, 169, Right - title_width - 10, 191};
        box(dc, chip, ink::card, RGB(217, 226, 243), 10);
        draw(dc, fa(static_cast<long long>(printers.size())), chip, font.small, RGB(138, 160, 200),
             DT_CENTER | DT_SINGLELINE | DT_VCENTER);
        if (printers.empty())
            paint_empty(
                dc, {Left, 204, Right, 468}, L"هنوز پرینتری اضافه نشده است",
                L"روی «افزودن پرینتر» بزنید؛ پرینترهای USB، شبکه و صف‌های ویندوز خودکار پیدا "
                L"می‌شوند.\nنیازی به نصب درایور یا تایپ دستور در CMD و PowerShell نیست.");
        paint_message(dc, Home, {Left, 524, Right, 588});
        draw(
            dc,
            L"ESC/POS · USB · TCP/9100 — همهٔ ارتباط با پرینتر روی همین کامپیوتر انجام می‌شود.",
            {Left, 600, Right, 624}, font.small, ink::faint,
            DT_CENTER | DT_RTLREADING | DT_SINGLELINE | DT_VCENTER);
    }
    void paint_find(HDC dc) {
        para(
            dc,
            L"پرینترهای USB وصل به همین کامپیوتر، پرینترهای شبکهٔ داخلی (پورت 9100) و صف‌های چاپ "
            L"ویندوز خودکار جستجو می‌شوند. پرینتر را انتخاب کنید و «افزودن» را بزنید؛ نیازی به نصب "
            L"درایور یا دستور خط فرمان نیست.",
            {Left, 72, Right, 114}, font.body, ink::muted);
        RECT status{Left, 162, Right, 184};
        std::wstring s;
        COLORREF color = ink::muted;
        if (scanning) {
            color = (pulse % 2) ? ink::blue : ink::blue_line;
            s = scanning_lan
                    ? L"در حال جستجو… USB و صف‌های ویندوز" +
                          std::wstring(usb_scanned ? L" بررسی شد" : L"") + L"؛ شبکه: " +
                          (scan_scope.empty() ? std::wstring(L"در حال بررسی") : scan_scope)
                    : L"در حال جستجوی پرینترهای USB و صف‌های ویندوز…";
        } else if (scanned_at) {
            int n_usb = 0, n_lan = 0, n_queue = 0;
            for (const auto &f : found)
                (f.kind == "usb" ? n_usb : f.kind == "network" ? n_lan : n_queue)++;
            color = ink::green;
            s = L"جستجو کامل شد (" + when(scanned_at) + L"): " + fa(n_usb) + L" پرینتر USB · " +
                (lan_scanned ? fa(n_lan) + L" پرینتر شبکه" : std::wstring(L"شبکه جستجو نشد")) +
                L" · " + fa(n_queue) + L" صف ویندوز";
            if (!scan_error.empty())
                s += L" — بخشی از جستجو انجام نشد";
        }
        if (!s.empty()) {
            dot(dc, Right - 9, status.top + 7, 8, color);
            rtl(dc, s, {Left, status.top, Right - 18, status.bottom}, font.small,
                scanning ? ink::blue_dark : ink::muted);
        }
        if (found.empty())
            paint_empty(
                dc, {Left, 190, Right, 540},
                scanning ? L"در حال جستجوی پرینترها…" : L"پرینتری پیدا نشد",
                scanning
                    ? L"چند ثانیه صبر کنید؛ شبکه و پورت‌های USB بررسی می‌شوند."
                    : L"پرینتر روشن است؟ کابل USB یا شبکه وصل است؟ سپس «جستجوی دوباره» را "
                      L"بزنید.\nبرای پرینتر شبکه، IP آن را با «افزودن دستی با IP» هم می‌توانید "
                      L"وارد کنید.");
        auto it = messages.find(Find);
        if (it != messages.end() && !it->second.text.empty())
            paint_message(dc, Find, {Left, 548, Right, 626});
        else
            para(
                dc,
                L"• پرینتر USB: کافی است کابل وصل و پرینتر روشن باشد؛ ویندوز ۷ آن را با «USB "
                L"Printing Support» می‌شناسد و برنامه مستقیم به آن چاپ می‌کند. با وصل کردن پرینتر، "
                L"این فهرست خودکار به‌روز می‌شود.\n• پرینتر شبکه: پرینتر و این کامپیوتر باید به یک "
                L"مودم یا سوییچ وصل باشند. IP پرینتر را با نگه‌داشتن دکمهٔ FEED هنگام روشن کردن "
                L"آن، روی کاغذ می‌بینید.",
                {Left, 550, Right, 630}, font.small, ink::muted);
    }
    void paint_label(HDC dc, const std::wstring &s, int y, int right = LabelRight,
                     int width = 120) {
        rtl(dc, s, {right - width, y, right, y + 28}, font.body, ink::text);
    }
    void paint_edit(HDC dc) {
        box(dc, {Left, 74, Right, 212}, ink::card, ink::line);
        rtl(dc, L"۱. اتصال پرینتر", {Left + 20, 82, LabelRight, 104}, font.bold, ink::text);
        paint_label(dc, L"نوع اتصال", 112);
        int type = draft_type();
        if (type == 0) {
            paint_label(dc, L"پرینتر USB", 152);
            rtl(dc,
                usb_options.empty()
                    ? L"هیچ پرینتر USB پیدا نشد؛ کابل و روشن بودن پرینتر را بررسی و «جستجوی "
                      L"دوباره» را بزنید."
                    : L"چاپ مستقیم از پورت USB با درایور داخلی ویندوز؛ نصب درایور پرینتر لازم نیست.",
                {Left + 20, 186, LabelRight, 206}, font.small,
                usb_options.empty() ? ink::amber : ink::muted);
        } else if (type == 1) {
            paint_label(dc, L"IP پرینتر", 152);
            rtl(dc, L"پورت", {ControlRight - 170 - 12 - 50, 152, ControlRight - 170 - 12, 180},
                font.body, ink::text);
            rtl(dc,
                L"IP شبکهٔ داخلی مثل 192.168.1.50 و پورت 9100. IP را با «جستجوی خودکار» یا برگهٔ "
                L"تنظیمات چاپ‌شدهٔ خود پرینتر پیدا کنید.",
                {Left + 20, 186, LabelRight, 206}, font.small, ink::muted);
        } else {
            paint_label(dc, L"صف ویندوز", 152);
            rtl(dc,
                L"برای پرینتری که درایور آن در ویندوز نصب است. درایور باید داده را بدون تغییر "
                L"(RAW) بفرستد؛ درایور XPS مناسب نیست.",
                {Left + 20, 186, LabelRight, 206}, font.small, ink::muted);
        }
        box(dc, {Left, 224, Right, 448}, ink::card, ink::line);
        rtl(dc, L"۲. مشخصات چاپ", {Left + 20, 232, LabelRight, 254}, font.bold, ink::text);
        paint_label(dc, L"نام پرینتر", 262);
        rtl(dc, L"کاربرد", {ControlRight - 326, 262, ControlRight - 256, 290}, font.body,
            ink::text);
        paint_label(dc, L"عرض کاغذ", 340);
        rtl(dc, L"تعداد نسخه", {ControlRight - 346, 340, ControlRight - 266, 368}, font.body,
            ink::text);
        paint_label(dc, L"عرض چاپ (نقطه)", 380, LabelRight, 130);
        rtl(dc, L"اندازهٔ قلم", {ControlRight - 186, 380, ControlRight - 96, 408}, font.body,
            ink::text);
        rtl(dc,
            L"کاغذ ۸۰ میلی‌متر معمولاً ۵۷۶ نقطه و کاغذ ۵۸ میلی‌متر ۳۸۴ نقطه است (با انتخاب عرض "
            L"کاغذ خودکار تنظیم می‌شود).",
            {Left + 20, 416, LabelRight, 436}, font.small, ink::muted);
        paint_message(dc, Edit, {Left, 508, Right, 572});
    }
    void step(HDC dc, int number, int y, const std::wstring &s, int height) {
        RECT circle{Right - 20 - 28, y, Right - 20, y + 28};
        dot(dc, circle.left, circle.top, 28, ink::blue_soft);
        draw(dc, fa(number), circle, font.bold, ink::blue, DT_CENTER | DT_SINGLELINE | DT_VCENTER);
        para(dc, s, {Left + 20, y + 4, Right - 60, y + height}, font.body, ink::text);
    }
    void paint_pair(HDC dc) {
        box(dc, {Left, 74, Right, 162}, ink::card, ink::line);
        int sites = site_count();
        std::wstring title, detail;
        COLORREF color;
        if (start_failed || !(server && server->ready())) {
            color = ink::red;
            title = L"اتصال محلی غیرفعال است";
            detail = explain(start_code.empty() ? "PORT_IN_USE_OR_UNAVAILABLE" : start_code);
        } else if (sites > 0) {
            color = ink::green;
            title = L"سایت منووکس وصل است";
            detail =
                fa(sites) + L" مرورگر با کلید معتبر وصل است (آخرین اتصال " +
                when(server->last_authenticated()) +
                L"). سفارش‌ها روی پرینترهای همین کامپیوتر چاپ می‌شوند.";
        } else if (recent_reject()) {
            color = ink::red;
            title = L"اتصال مرورگر رد شد";
            detail = reject_text;
        } else {
            color = ink::muted;
            title = L"هنوز هیچ مرورگری وصل نشده است";
            detail =
                L"سه مرحلهٔ زیر را یک بار انجام دهید؛ اتصال ذخیره می‌شود و دفعه‌های بعد خودکار "
                L"برقرار است.";
        }
        dot(dc, Right - 30, 92, 10, color);
        rtl(dc, title, {Left + 20, 82, Right - 40, 110}, font.heading, color);
        para(dc, detail, {Left + 20, 114, Right - 40, 160}, font.body, ink::muted);
        box(dc, {Left, 174, Right, 452}, ink::card, ink::line);
        step(dc, 1, 188,
             L"در مرورگر همین کامپیوتر، داشبورد منووکس را باز کنید: https://app.menuvex.ir ← "
             L"تنظیمات ← پرینترها ← دکمهٔ Pair",
             46);
        step(dc, 2, 240, L"کلید اتصال را کپی (یا نمایش) کنید و در پنجرهٔ Pair سایت وارد کنید:", 26);
        step(
            dc, 3, 318,
            L"وقتی وصل شد، وضعیت بالای همین صفحه سبز می‌شود. سپس از داخل سایت یک چاپ آزمایشی "
            L"بگیرید.",
            44);
        para(
            dc,
            L"نشانی محلی این Agent: 127.0.0.1:" + std::to_wstring(port()) +
                L" — سایت باید به همین نشانی وصل شود. اگر در سایت عدد دیگری تنظیم شده، همان را در "
                L"«تنظیمات» وارد کنید.\nAgent جدید و این نسخه را هم‌زمان باز نکنید؛ هر دو از پورت "
                L"8765 استفاده می‌کنند.",
            {Left + 20, 370, Right - 20, 448}, font.small, ink::muted);
        box(dc, {Left, 464, Right, 588}, ink::card, ink::line);
        para(
            dc,
            L"کلید را فقط در سایت رسمی منووکس (menuvex.ir) وارد کنید و آن را در چت یا پیام "
            L"نفرستید. اگر فکر می‌کنید کلید لو رفته، اتصال همهٔ مرورگرها را قطع کنید و دوباره Pair "
            L"کنید.",
            {Left + 20, 476, Right - 20, 534}, font.body, ink::text);
        paint_message(dc, Pair, {Left, 598, Right, 632});
    }
    void paint_queue(HDC dc) {
        para(dc,
             L"«ارسال شد» یعنی داده به پرینتر یا صف ویندوز تحویل شد، نه اینکه کاغذ حتماً بیرون "
             L"آمده "
             L"باشد. اگر وضعیتی «نامشخص» بود، پیش از چاپ دوباره کاغذ را بررسی کنید.",
             {Left, 72, Right, 112}, font.body, ink::muted);
        if (jobs.empty())
            paint_empty(
                dc, {Left, 116, Right, 532}, L"صف چاپ خالی است",
                L"سفارش‌هایی که سایت برای چاپ می‌فرستد و چاپ‌های آزمایشی اینجا دیده می‌شوند.");
        paint_message(dc, Queue, {Left, 588, Right, 630});
    }
    void paint_settings(HDC dc) {
        box(dc, {Left, 74, Right, 262}, ink::card, ink::line);
        rtl(dc, L"اتصال و اجرا", {Left + 20, 84, LabelRight, 106}, font.bold, ink::text);
        paint_label(dc, L"پورت محلی", 116, LabelRight, 150);
        rtl(dc,
            L"پیش‌فرض 8765؛ باید با سایت یکی باشد و پس از اجرای دوبارهٔ برنامه اعمال می‌شود.",
            {Left + 20, 116, Right - 284, 144}, font.small, ink::muted);
        paint_label(dc, L"تعداد تلاش برای چاپ", 156, LabelRight, 150);
        rtl(dc,
            L"۱ تا ۵ — فقط وقتی چیزی به پرینتر نرسیده باشد دوباره تلاش می‌شود.",
            {Left + 20, 156, Right - 284, 184}, font.small, ink::muted);
        box(dc, {Left, 274, Right, 430}, ink::card, ink::line);
        rtl(dc, L"درباره و عیب‌یابی", {Left + 20, 284, LabelRight, 306}, font.bold,
            ink::text);
        rtl(dc, L"MenuVex Printer Agent — نسخهٔ Legacy 1.0.0 برای ویندوز ۷ SP1 و جدیدتر",
            {Left + 20, 312, LabelRight, 332}, font.body, ink::text);
        rtl(dc, L"پوشهٔ داده و گزارش:", {Right - 200, 336, LabelRight, 356}, font.body, ink::text);
        draw(dc, directory, {Left + 20, 336, Right - 206, 356}, font.mono, ink::muted,
             DT_LEFT | DT_SINGLELINE | DT_VCENTER | DT_PATH_ELLIPSIS);
        rtl(dc,
            L"گزارش فقط کد رویدادها را دارد؛ متن فاکتور و کلید اتصال در آن ذخیره نمی‌شود.",
            {Left + 20, 360, LabelRight, 380}, font.small, ink::muted);
        box(dc, {Left, 442, Right, 540}, ink::card, ink::line);
        rtl(dc, L"خروج از برنامه", {Left + 220, 452, LabelRight, 474}, font.bold, ink::text);
        para(
            dc,
            L"بستن پنجره، برنامه را کنار ساعت ویندوز نگه می‌دارد تا سفارش‌ها چاپ شوند. برای خروج "
            L"کامل این دکمه را بزنید.",
            {Left + 220, 478, LabelRight, 534}, font.body, ink::muted);
        paint_message(dc, Settings, {Left, 552, Right, 600});
    }
    void paint(HDC target) {
        HDC dc = CreateCompatibleDC(target);
        HBITMAP bitmap = CreateCompatibleBitmap(target, ClientW, ClientH);
        HGDIOBJ old_bitmap = SelectObject(dc, bitmap);
        HGDIOBJ old_font = SelectObject(dc, font.body);
        fill(dc, {0, 0, ClientW, ClientH}, ink::page);
        paint_sidebar(dc);
        paint_header(dc);
        switch (page) {
        case Home:
            paint_home(dc);
            break;
        case Find:
            paint_find(dc);
            break;
        case Edit:
            paint_edit(dc);
            break;
        case Pair:
            paint_pair(dc);
            break;
        case Queue:
            paint_queue(dc);
            break;
        case Settings:
            paint_settings(dc);
            break;
        }
        BitBlt(target, 0, 0, ClientW, ClientH, dc, 0, 0, SRCCOPY);
        SelectObject(dc, old_font);
        SelectObject(dc, old_bitmap);
        DeleteObject(bitmap);
        DeleteDC(dc);
    }
    // ---- Owner-drawn controls ----
    int nav_badge(int id) const { return id == NavQueue ? pending() : 0; }
    bool nav_active(int id) const {
        return (id == NavHome && (page == Home || (page == Edit && edit_return == Home))) ||
               (id == NavFind && (page == Find || (page == Edit && edit_return == Find))) ||
               (id == NavPair && page == Pair) || (id == NavQueue && page == Queue) ||
               (id == NavSettings && page == Settings);
    }
    void draw_button(const DRAWITEMSTRUCT *item) {
        auto it = controls.find(static_cast<int>(item->CtlID));
        if (it == controls.end())
            return;
        const Ctl &c = it->second;
        HDC dc = item->hDC;
        RECT r = item->rcItem;
        fill(dc, r, c.surface);
        wchar_t label[160] = {};
        GetWindowTextW(item->hwndItem, label, 160);
        bool disabled = (item->itemState & ODS_DISABLED) != 0;
        bool pressed = (item->itemState & ODS_SELECTED) != 0;
        bool focus = (item->itemState & ODS_FOCUS) != 0 && !(item->itemState & ODS_NOFOCUSRECT);
        if (c.kind == Kind::Nav) {
            bool active = nav_active(static_cast<int>(item->CtlID));
            if (active || pressed)
                box(dc, r, ink::blue_soft, ink::blue_soft, 12);
            RECT t{r.left + 40, r.top, r.right - 16, r.bottom};
            rtl(dc, label, t, active ? font.bold : font.body, active ? ink::blue : ink::nav);
            int badge = nav_badge(static_cast<int>(item->CtlID));
            if (badge > 0) {
                RECT b{r.left + 10, r.top + 10, r.left + 36, r.bottom - 10};
                box(dc, b, RGB(219, 231, 255), RGB(219, 231, 255), 8);
                draw(dc, fa(badge), b, font.small, ink::blue,
                     DT_CENTER | DT_SINGLELINE | DT_VCENTER);
            }
        } else {
            COLORREF bg = ink::card, fg = ink::blue, border = ink::button_line;
            HFONT f = font.bold;
            switch (c.kind) {
            case Kind::Primary:
                bg = disabled ? ink::disabled : pressed ? ink::blue_dark : ink::blue;
                fg = RGB(255, 255, 255);
                border = bg;
                break;
            case Kind::Secondary:
                bg = pressed ? ink::blue_soft : ink::card;
                fg = disabled ? ink::faint : ink::blue;
                break;
            case Kind::Danger:
                bg = pressed ? RGB(254, 226, 226) : ink::red_soft;
                fg = disabled ? ink::faint : RGB(220, 38, 38);
                border = bg;
                break;
            default: // Subtle
                bg = pressed ? ink::gray_soft : c.surface;
                fg = disabled ? ink::faint : RGB(90, 107, 132);
                border = bg;
                f = font.body;
                break;
            }
            box(dc, r, bg, border, 12);
            draw(dc, label, r, f, fg, DT_CENTER | DT_VCENTER | DT_SINGLELINE | DT_RTLREADING);
        }
        if (focus) {
            RECT f = r;
            InflateRect(&f, -4, -4);
            SetTextColor(dc, ink::text);
            DrawFocusRect(dc, &f);
        }
    }
    // Card frame shared by the owner-drawn list items; returns the inner card rectangle.
    RECT item_card(const DRAWITEMSTRUCT *item, bool selected) {
        HDC dc = item->hDC;
        RECT r = item->rcItem;
        fill(dc, r, ink::page);
        RECT card{r.left + 1, r.top + 4, r.right - 1, r.bottom - 4};
        box(dc, card, selected ? ink::blue_soft : ink::card, selected ? ink::blue_line : ink::line,
            14);
        return card;
    }
    void draw_printer(const DRAWITEMSTRUCT *item) {
        std::size_t i = static_cast<std::size_t>(item->itemData);
        if (i >= printers.size())
            return;
        const Json &p = printers[i];
        HDC dc = item->hDC;
        RECT card = item_card(item, (item->itemState & ODS_SELECTED) != 0);
        // A simple printer glyph drawn with GDI (no icon font needed on Windows 7).
        RECT glyph{card.right - 54, card.top + 12, card.right - 16, card.bottom - 12};
        box(dc, glyph, RGB(238, 243, 252), RGB(221, 231, 248), 10);
        fill(dc, {glyph.left + 10, glyph.top + 8, glyph.right - 10, glyph.top + 11},
             RGB(95, 134, 196));
        fill(dc, {glyph.left + 8, glyph.top + 13, glyph.right - 8, glyph.bottom - 12},
             RGB(95, 134, 196));
        fill(dc, {glyph.left + 12, glyph.bottom - 12, glyph.right - 12, glyph.bottom - 7},
             RGB(160, 186, 226));
        std::wstring name = w(p["name"].get<std::string>());
        int text_right = glyph.left - 12;
        rtl(dc, name, {card.left + 170, card.top + 8, text_right, card.top + 30}, font.heading,
            ink::text);
        const int room = text_right - static_cast<int>(card.left) - 300;
        int x = text_right - std::min(width_of(dc, name, font.heading), room) - 10;
        for (const auto &r : config["routes"])
            if (r["printerId"] == p["id"]) {
                std::wstring tag =
                    role_label(r["role"]) + (r["autoPrint"] == true ? L" · خودکار" : L"");
                RECT t = pill(dc, x, card.top + 10, 20, tag, font.tiny, ink::blue, ink::blue_soft,
                              true, false);
                x = t.left - 6;
            }
        std::wstring detail = connection_text(p["connection"]) + L" · " +
                              fa(p["paperMm"].get<int>()) + L" میلی‌متر · " +
                              fa(p["copies"].get<int>()) + L" نسخه";
        rtl(dc, detail, {card.left + 170, card.top + 32, text_right, card.bottom - 6}, font.small,
            ink::muted);
        Badge badge = printer_badge(p);
        pill(dc, card.left + 14, card.top + (card.bottom - card.top) / 2 - 12, 24, badge.text,
             font.small, badge.fg, badge.bg);
    }
    void draw_found(const DRAWITEMSTRUCT *item) {
        std::size_t i = static_cast<std::size_t>(item->itemData);
        if (i >= found.size())
            return;
        const Found &f = found[i];
        HDC dc = item->hDC;
        bool selected = (item->itemState & ODS_SELECTED) != 0;
        RECT card = item_card(item, selected);
        COLORREF fg = f.kind == "usb" ? ink::green : f.kind == "network" ? ink::blue : ink::violet;
        COLORREF bg = f.kind == "usb"       ? ink::green_soft
                      : f.kind == "network" ? ink::blue_soft
                                            : ink::violet_soft;
        RECT kind{card.right - 74, card.top + 14, card.right - 14, card.top + 38};
        box(dc, kind, bg, bg, 10);
        draw(dc,
             f.kind == "usb"       ? L"USB"
             : f.kind == "network" ? L"شبکه"
                                   : L"ویندوز",
             kind, font.bold, fg, DT_CENTER | DT_SINGLELINE | DT_VCENTER | DT_RTLREADING);
        int text_right = kind.left - 12;
        int text_left = card.left + 180;
        rtl(dc, f.title, {text_left, card.top + 6, text_right, card.top + 27}, font.heading,
            ink::text);
        rtl(dc, f.detail, {text_left, card.top + 28, text_right, card.top + 46}, font.small,
            ink::muted);
        if (!f.note.empty())
            rtl(dc, f.note, {text_left, card.top + 46, text_right, card.bottom - 4}, font.small,
                ink::amber);
        RECT side{card.left + 14, card.top + (card.bottom - card.top) / 2 - 12, 0, 0};
        if (!f.configured.empty())
            pill(dc, side.left, side.top, 24, L"اضافه شده: " + w(f.configured), font.small,
                 ink::green, ink::green_soft);
        else if (selected)
            pill(dc, side.left, side.top, 24, L"دوبار کلیک = افزودن", font.small, ink::blue,
                 ink::card, false, false);
    }
    void draw_job(const DRAWITEMSTRUCT *item) {
        std::size_t i = static_cast<std::size_t>(item->itemData);
        if (i >= jobs.size())
            return;
        const Json &j = jobs[i];
        HDC dc = item->hDC;
        RECT card = item_card(item, (item->itemState & ODS_SELECTED) != 0);
        const std::string status = j["status"];
        bool uncertain = !j["error"].is_null() && j["error"].value("uncertain", false);
        COLORREF fg = status == "completed"   ? ink::green
                      : status == "failed"    ? (uncertain ? ink::amber : ink::red)
                      : status == "cancelled" ? ink::muted
                                              : ink::blue;
        COLORREF bg = status == "completed"   ? ink::green_soft
                      : status == "failed"    ? (uncertain ? ink::amber_soft : ink::red_soft)
                      : status == "cancelled" ? ink::gray_soft
                                              : ink::blue_soft;
        std::wstring label = uncertain && status == "failed" ? L"نامشخص" : status_label(status);
        RECT tag = pill(dc, card.right - 14, card.top + 9, 22, label, font.small, fg, bg, true);
        const Json *p = printer(j["printerId"]);
        std::wstring who = p ? w((*p)["name"].get<std::string>()) : w(j["printerId"]);
        std::string id = j["jobId"];
        bool test = id.rfind("test:", 0) == 0;
        rtl(dc, (test ? L"چاپ آزمایشی · " : L"سفارش · ") + who + L" · " + when(j["createdAt"]),
            {card.left + 14, card.top + 6, tag.left - 10, card.top + 28}, font.bold, ink::text);
        std::wstring detail = L"شناسه: " + w(id) + L" · تلاش " + fa(j["attempts"].get<int>());
        if (!j["error"].is_null())
            detail = explain(j["error"]["code"], j["error"]["message"]) + L"  (" + detail + L")";
        rtl(dc, detail, {card.left + 14, card.top + 27, card.right - 14, card.bottom - 4},
            font.small, j["error"].is_null() ? ink::muted : fg);
    }
};
App *app = nullptr;
UINT taskbar_created = 0; // registered "TaskbarCreated" broadcast (Explorer restart/late start)
HWND make(HWND parent, int page, Kind kind, const wchar_t *cls, const wchar_t *label, int id, int x,
          int y, int width, int height, DWORD style, DWORD ex = 0, COLORREF surface = ink::page) {
    style |= WS_CHILD;
    if (page == -1 || page == app->page)
        style |= WS_VISIBLE;
    HWND hwnd = CreateWindowExW(ex, cls, label, style, x, y, width, height, parent,
                                reinterpret_cast<HMENU>(static_cast<INT_PTR>(id)),
                                GetModuleHandleW(nullptr), nullptr);
    SendMessageW(hwnd, WM_SETFONT,
                 reinterpret_cast<WPARAM>(id == SecretBox ? app->font.mono : app->font.body), TRUE);
    app->controls[id] = {hwnd, page, kind, surface};
    return hwnd;
}
void button(HWND w, int page, Kind kind, const wchar_t *label, int id, int x, int y, int width,
            int height = 34, COLORREF surface = ink::page) {
    make(w, page, kind, L"BUTTON", label, id, x, y, width, height, BS_OWNERDRAW | WS_TABSTOP, 0,
         surface);
}
void edit(HWND w, int page, int id, int x, int y, int width, bool persian = false,
          DWORD style = 0) {
    HWND h = make(w, page, Kind::Field, L"EDIT", L"", id, x, y, width, 28,
                  ES_AUTOHSCROLL | WS_TABSTOP | style | (persian ? ES_RIGHT : 0),
                  WS_EX_CLIENTEDGE | (persian ? WS_EX_RTLREADING : 0), ink::card);
    SendMessageW(h, EM_SETMARGINS, EC_LEFTMARGIN | EC_RIGHTMARGIN, MAKELONG(6, 6));
}
void combo(HWND w, int page, int id, int x, int y, int width,
           std::initializer_list<const wchar_t *> items = {}) {
    HWND h = make(w, page, Kind::Field, L"COMBOBOX", L"", id, x, y, width, 260,
                  CBS_DROPDOWNLIST | WS_VSCROLL | WS_TABSTOP, WS_EX_LAYOUTRTL, ink::card);
    for (auto item : items)
        SendMessageW(h, CB_ADDSTRING, 0, reinterpret_cast<LPARAM>(item));
    SendMessageW(h, CB_SETMINVISIBLE, 10, 0);
}
void checkbox(HWND w, int page, const wchar_t *label, int id, int x, int y, int width,
              DWORD style = BS_AUTOCHECKBOX) {
    make(w, page, Kind::Check, L"BUTTON", label, id, x, y, width, 26, style | WS_TABSTOP,
         WS_EX_LAYOUTRTL, ink::card);
}
void list(HWND w, int page, int id, int x, int y, int width, int height) {
    make(w, page, Kind::List, L"LISTBOX", L"", id, x, y, width, height,
         LBS_OWNERDRAWFIXED | LBS_NOTIFY | LBS_NOINTEGRALHEIGHT | WS_VSCROLL | WS_TABSTOP);
}
void create_ui(HWND w) {
    // Sidebar navigation (every page).
    const std::pair<int, const wchar_t *> nav[] = {{NavHome, L"پرینترها"},
                                                   {NavFind, L"افزودن پرینتر"},
                                                   {NavPair, L"اتصال به سایت"},
                                                   {NavQueue, L"صف چاپ"},
                                                   {NavSettings, L"تنظیمات"}};
    int y = 146;
    for (const auto &item : nav) {
        button(w, -1, Kind::Nav, item.second, item.first, SideLeft + 14, y, SidebarW - 28, 42,
               ink::card);
        y += 48;
    }
    // Home.
    button(w, Home, Kind::Secondary, L"راهنمای اتصال", GoPair, Left + 14, 105, 130, 30, ink::card);
    button(w, Home, Kind::Primary, L"+  افزودن پرینتر", AddPrinter, Left, 162, 170);
    list(w, Home, PrinterList, Left, 204, Right - Left, 264);
    button(w, Home, Kind::Primary, L"چاپ آزمایشی", TestPrinter, Right - 140, 478, 140);
    button(w, Home, Kind::Secondary, L"بررسی اتصال", ProbePrinter, Right - 280, 478, 130);
    button(w, Home, Kind::Secondary, L"ویرایش", EditPrinter, Right - 400, 478, 110);
    button(w, Home, Kind::Danger, L"حذف", RemovePrinter, Left, 478, 100);
    // Find (automatic discovery).
    button(w, Find, Kind::Primary, L"افزودن پرینتر انتخاب‌شده", AddFound,
           Right - 190, 120, 190);
    button(w, Find, Kind::Secondary, L"جستجوی دوباره", Rescan, Right - 340, 120, 140);
    button(w, Find, Kind::Secondary, L"افزودن دستی با IP", AddManual, Left, 120, 170);
    list(w, Find, FoundList, Left, 190, Right - Left, 350);
    // Edit.
    combo(w, Edit, ConnType, ControlRight - 280, 112, 280,
          {L"USB مستقیم (بدون نصب درایور)", L"شبکه — با IP پرینتر", L"صف چاپ ویندوز"});
    combo(w, Edit, UsbPick, ControlRight - 400, 152, 400);
    combo(w, Edit, QueuePick, ControlRight - 400, 152, 400);
    button(w, Edit, Kind::Secondary, L"جستجوی دوباره", RefreshPick, ControlRight - 540, 151, 130,
           30, ink::card);
    edit(w, Edit, Host, ControlRight - 170, 152, 170);
    edit(w, Edit, NetPort, ControlRight - 320, 152, 80, false, ES_NUMBER);
    button(w, Edit, Kind::Secondary, L"بررسی اتصال", CheckEdit, ControlRight - 462, 151, 130, 30,
           ink::card);
    edit(w, Edit, Name, ControlRight - 240, 262, 240, true);
    SendMessageW(GetDlgItem(w, Name), EM_LIMITTEXT, 128, 0);
    SendMessageW(GetDlgItem(w, Name), EM_SETCUEBANNER, TRUE,
                 reinterpret_cast<LPARAM>(L"مثلاً: صندوق، آشپزخانه یا بار"));
    SendMessageW(GetDlgItem(w, Host), EM_SETCUEBANNER, TRUE,
                 reinterpret_cast<LPARAM>(L"192.168.1.50"));
    combo(w, Edit, Role, ControlRight - 514, 262, 180,
          {L"بدون کاربرد", L"فاکتور مشتری", L"آشپزخانه", L"بار"});
    checkbox(w, Edit,
             L"چاپ خودکار سفارش‌های سایت برای این کاربرد",
             AutoPrint, LabelRight - 340, 302, 340);
    checkbox(w, Edit, L"برش خودکار کاغذ", Cutter, LabelRight - 560, 302, 200);
    checkbox(w, Edit, L"۸۰ میلی‌متر", Paper80, ControlRight - 120, 341, 120,
             BS_AUTORADIOBUTTON | WS_GROUP);
    checkbox(w, Edit, L"۵۸ میلی‌متر", Paper58, ControlRight - 250, 341, 120,
             BS_AUTORADIOBUTTON);
    combo(w, Edit, Copies, ControlRight - 424, 340, 70, {L"۱", L"۲", L"۳"});
    SetWindowLongW(GetDlgItem(w, Copies), GWL_STYLE,
                   GetWindowLongW(GetDlgItem(w, Copies), GWL_STYLE) | WS_GROUP);
    edit(w, Edit, Dots, ControlRight - 80, 380, 80, false, ES_NUMBER);
    edit(w, Edit, Font, ControlRight - 254, 380, 60, false, ES_NUMBER);
    button(w, Edit, Kind::Primary, L"ذخیره", SaveEdit, Right - 130, 462, 130);
    button(w, Edit, Kind::Secondary, L"ذخیره و چاپ آزمایشی", SaveTest, Right - 340, 462, 200);
    button(w, Edit, Kind::Subtle, L"انصراف", CancelEdit, Right - 450, 462, 100);
    button(w, Edit, Kind::Danger, L"حذف این پرینتر", DeleteEdit, Left, 462, 140);
    // Pair.
    button(w, Pair, Kind::Secondary, L"نمایش کلید (۶۰ ثانیه)", Reveal, Right - 60 - 160, 270, 160,
           32, ink::card);
    button(w, Pair, Kind::Primary, L"کپی کلید", CopyKey, Right - 60 - 160 - 10 - 120, 270, 120, 32,
           ink::card);
    make(w, Pair, Kind::Field, L"EDIT", L"", SecretBox, Left + 20, 271, (Right - 360) - (Left + 20),
         30, ES_READONLY | ES_AUTOHSCROLL, WS_EX_CLIENTEDGE, ink::card);
    button(w, Pair, Kind::Danger, L"قطع اتصال همهٔ مرورگرها و ساخت کلید جدید", Rotate,
           Right - 20 - 330, 542, 330, 34, ink::card);
    // Queue.
    list(w, Queue, QueueList, Left, 116, Right - Left, 416);
    button(w, Queue, Kind::Secondary, L"لغو کار انتخاب‌شده", CancelJob,
           Right - 180, 544, 180);
    button(w, Queue, Kind::Danger, L"پاک کردن سابقه", ClearHistory, Right - 350, 544, 160);
    // Settings.
    edit(w, Settings, Port, Right - 272, 116, 90, false, ES_NUMBER);
    edit(w, Settings, Attempts, Right - 272, 156, 60, false, ES_NUMBER);
    checkbox(w, Settings, L"اجرای خودکار هنگام ورود به ویندوز (پیشنهادی)", AutoStart,
             LabelRight - 380, 198, 380);
    button(w, Settings, Kind::Primary, L"ذخیرهٔ تنظیمات", SaveSettings, Left + 20, 214, 160, 34,
           ink::card);
    button(w, Settings, Kind::Secondary, L"باز کردن پوشهٔ گزارش", OpenFolder, Right - 20 - 190, 388,
           190, 32, ink::card);
    button(w, Settings, Kind::Danger, L"خروج کامل از برنامه", Quit, Left + 20, 486, 180, 34,
           ink::card);
}
Json selected_profile() {
    const Json *p = app->printer(app->selected);
    if (!p)
        throw Error("PRINTER_NOT_FOUND", "Select a printer first");
    return *p;
}
void command(HWND w, int id, int code) {
    App &a = *app;
    switch (id) {
    case NavHome:
        a.navigate(Home);
        break;
    case NavFind:
    case AddPrinter:
    case TrayAdd:
        if (id == TrayAdd) {
            ShowWindow(w, SW_SHOW);
            SetForegroundWindow(w);
        }
        a.navigate(Find);
        break;
    case NavPair:
    case GoPair:
        a.navigate(Pair);
        break;
    case NavQueue:
        a.navigate(Queue);
        break;
    case NavSettings:
        a.navigate(Settings);
        break;
    case PrinterList:
        if (code == LBN_SELCHANGE || code == LBN_DBLCLK) {
            int i = a.cursel(PrinterList);
            a.selected = i >= 0 && static_cast<std::size_t>(i) < a.printers.size()
                             ? a.printers[static_cast<std::size_t>(i)]["id"].get<std::string>()
                             : std::string();
            a.update_visibility();
            if (code == LBN_DBLCLK && !a.selected.empty())
                a.open_editor(selected_profile(), false, Home);
        }
        break;
    case TestPrinter:
        a.test(selected_profile()["id"]);
        break;
    case ProbePrinter:
        a.probe(selected_profile(), Home);
        break;
    case EditPrinter:
        a.open_editor(selected_profile(), false, Home);
        break;
    case RemovePrinter:
        a.remove(a.selected);
        break;
    case FoundList:
        if (code == LBN_SELCHANGE)
            a.update_visibility();
        if (code != LBN_DBLCLK)
            break;
        [[fallthrough]];
    case AddFound: {
        int i = a.cursel(FoundList);
        if (i < 0 || static_cast<std::size_t>(i) >= a.found.size())
            break;
        const Found &f = a.found[static_cast<std::size_t>(i)];
        if (!f.configured.empty()) {
            for (const auto &p : a.printers)
                if (same_connection(p["connection"], f.connection)) {
                    a.open_editor(p, false, Find);
                    break;
                }
            break;
        }
        a.open_editor({{"name", f.name},
                       {"connection", f.connection},
                       {"paperMm", f.paper},
                       {"widthDots", f.paper == 58 ? 384 : 576},
                       {"copies", 1},
                       {"cut", true},
                       {"fontSize", 24}},
                      true, Find);
        break;
    }
    case Rescan:
        a.messages.erase(Find);
        a.scan(true);
        break;
    case AddManual:
        a.open_editor(
            {{"name", ""}, {"connection", {{"type", "network"}, {"host", ""}, {"port", 9100}}}},
            true, Find);
        break;
    case ConnType:
        if (code == CBN_SELCHANGE) {
            a.messages.erase(Edit);
            a.update_visibility();
        }
        break;
    case RefreshPick:
        a.scan(false);
        a.say(Edit, Info, L"فهرست در حال به‌روزرسانی است…");
        break;
    case CheckEdit:
        a.probe({{"name", utf8(a.text(Name))}, {"connection", a.draft_connection()}}, Edit);
        break;
    case Paper80:
    case Paper58:
        if (code == BN_CLICKED) {
            int dots = 0;
            try {
                dots = a.integer(Dots);
            } catch (...) {
            }
            if (dots == 0 || dots == 384 || dots == 576)
                a.set(Dots, id == Paper58 ? L"384" : L"576");
        }
        break;
    case SaveEdit:
    case SaveTest:
        a.save_editor(id == SaveTest);
        break;
    case CancelEdit:
    case IDCANCEL:
        if (a.page == Edit) {
            a.draft_id.clear();
            a.show_page(a.edit_return);
        }
        break;
    case DeleteEdit:
        a.remove(a.draft_id);
        break;
    case Reveal:
        a.reveal_key();
        break;
    case CopyKey:
        a.copy_key();
        break;
    case Rotate:
        a.rotate_key();
        break;
    case QueueList:
        if (code == LBN_SELCHANGE) {
            int i = a.cursel(QueueList);
            a.selected_job = i >= 0 && static_cast<std::size_t>(i) < a.jobs.size()
                                 ? a.jobs[static_cast<std::size_t>(i)]["jobId"].get<std::string>()
                                 : std::string();
            a.update_visibility();
        }
        break;
    case CancelJob:
        a.cancel_job();
        break;
    case ClearHistory:
        a.clear_history();
        break;
    case SaveSettings:
        a.save_settings();
        break;
    case OpenFolder:
        a.open_folder();
        break;
    case Open:
        ShowWindow(w, SW_SHOW);
        SetForegroundWindow(w);
        break;
    case Quit:
        a.quit();
        break;
    }
}
LRESULT CALLBACK window_proc(HWND w, UINT message, WPARAM wp, LPARAM lp) {
    try {
        if (taskbar_created && message == taskbar_created) {
            // Explorer (re)created the taskbar: our icon is gone or was never added.
            if (app && !app->stopping && !app->add_tray())
                SetTimer(w, TrayTimer, 3000, nullptr);
            return 0;
        }
        switch (message) {
        case WM_ERASEBKGND:
            return 1; // WM_PAINT covers the whole client area (double-buffered)
        case WM_PAINT: {
            PAINTSTRUCT ps;
            HDC dc = BeginPaint(w, &ps);
            if (app)
                app->paint(dc);
            EndPaint(w, &ps);
            return 0;
        }
        case WM_CTLCOLORSTATIC: {
            // Checkboxes, radio buttons and the read-only key box sit on white cards.
            HDC dc = reinterpret_cast<HDC>(wp);
            SetTextColor(dc, ink::text);
            SetBkColor(dc, ink::card);
            return reinterpret_cast<LRESULT>(app && app->card_brush ? app->card_brush
                                                                    : GetStockObject(WHITE_BRUSH));
        }
        case WM_CTLCOLORLISTBOX: {
            // Card lists sit on the page background; combo box drop-downs stay white.
            HDC dc = reinterpret_cast<HDC>(wp);
            HWND list = reinterpret_cast<HWND>(lp);
            bool card_list = app && (list == app->ctl(PrinterList) || list == app->ctl(FoundList) ||
                                     list == app->ctl(QueueList));
            SetBkColor(dc, card_list ? ink::page : ink::card);
            if (!app)
                return reinterpret_cast<LRESULT>(GetStockObject(WHITE_BRUSH));
            return reinterpret_cast<LRESULT>(card_list ? app->page_brush : app->card_brush);
        }
        case WM_MEASUREITEM: {
            auto item = reinterpret_cast<MEASUREITEMSTRUCT *>(lp);
            if (item->CtlType == ODT_LISTBOX) {
                item->itemHeight = item->CtlID == PrinterList ? 66
                                   : item->CtlID == FoundList ? 70
                                                              : 52;
                return TRUE;
            }
            break;
        }
        case WM_DRAWITEM: {
            auto item = reinterpret_cast<DRAWITEMSTRUCT *>(lp);
            if (!app)
                break;
            if (item->CtlType == ODT_BUTTON) {
                app->draw_button(item);
                return TRUE;
            }
            if (item->CtlType == ODT_LISTBOX) {
                if (item->itemID == static_cast<UINT>(-1))
                    return TRUE;
                if (item->CtlID == PrinterList)
                    app->draw_printer(item);
                else if (item->CtlID == FoundList)
                    app->draw_found(item);
                else
                    app->draw_job(item);
                return TRUE;
            }
            break;
        }
        case WM_CREATE:
            app->window = w;
            create_ui(w);
            return 0;
        case WM_CLOSE:
            // Without a tray icon a hidden window could never be reopened: minimize instead.
            ShowWindow(w, app && !app->tray_ok ? SW_MINIMIZE : SW_HIDE);
            return 0;
        case WM_DESTROY:
            PostQuitMessage(0);
            return 0;
        case WM_DEVICECHANGE:
            // A USB printer was plugged in or removed: refresh the list once things settle.
            if (app && !app->stopping &&
                (wp == DBT_DEVICEARRIVAL || wp == DBT_DEVICEREMOVECOMPLETE))
                SetTimer(w, UsbTimer, 900, nullptr);
            return TRUE;
        case Changed:
            if (app && !app->stopping)
                app->refresh();
            return 0;
        case Discovered:
            if (app && !app->stopping)
                app->discovered(wp, lp);
            return 0;
        case Rejected:
            if (app && !app->stopping)
                app->show_rejected();
            return 0;
        case Probed:
            if (app && !app->stopping)
                app->probed();
            return 0;
        case WM_TIMER:
            if (!app)
                return 0;
            if (wp == SecretTimer) {
                app->set(SecretBox, L"");
                KillTimer(w, SecretTimer);
            } else if (wp == TrayTimer) {
                if (app->stopping || app->add_tray())
                    KillTimer(w, TrayTimer);
            } else if (wp == UsbTimer) {
                KillTimer(w, UsbTimer);
                if (!app->stopping)
                    app->scan(false);
            } else if (wp == PulseTimer) {
                ++app->pulse;
                if (app->page == Find) {
                    RECT status{Left, 160, Right, 186};
                    InvalidateRect(w, &status, FALSE);
                }
            } else if (wp == ClipboardTimer) {
                KillTimer(w, ClipboardTimer);
                app->forget_clipboard_key();
            }
            return 0;
        case Tray:
            if (lp == WM_LBUTTONDBLCLK) {
                ShowWindow(w, SW_SHOW);
                SetForegroundWindow(w);
            } else if (lp == WM_RBUTTONUP) {
                HMENU menu = CreatePopupMenu();
                AppendMenuW(menu, MF_STRING, Open, L"باز کردن منووکس پرینتر");
                AppendMenuW(menu, MF_STRING, TrayAdd, L"افزودن پرینتر");
                AppendMenuW(menu, MF_SEPARATOR, 0, nullptr);
                AppendMenuW(menu, MF_STRING, Quit, L"خروج از برنامه");
                POINT pt;
                GetCursorPos(&pt);
                SetForegroundWindow(w);
                TrackPopupMenu(menu, TPM_RIGHTBUTTON | TPM_LAYOUTRTL, pt.x, pt.y, 0, w, nullptr);
                DestroyMenu(menu);
            }
            return 0;
        case WM_COMMAND:
            if (app)
                command(w, LOWORD(wp), HIWORD(wp));
            return 0;
        }
    } catch (const std::exception &e) {
        if (app && app->window)
            app->error(e);
        else
            MessageBoxW(w, L"راه‌اندازی انجام نشد", L"MenuVex",
                        MB_OK | MB_ICONERROR);
    }
    return DefWindowProcW(w, message, wp, lp);
}
HFONT make_font(int height, int weight, const wchar_t *face) {
    return CreateFontW(height, 0, 0, 0, weight, FALSE, FALSE, FALSE, DEFAULT_CHARSET,
                       OUT_DEFAULT_PRECIS, CLIP_DEFAULT_PRECIS, CLEARTYPE_QUALITY,
                       DEFAULT_PITCH | FF_DONTCARE, face);
}
} // namespace
int WINAPI wWinMain(HINSTANCE instance, HINSTANCE, LPWSTR, int) {
    int argc = 0;
    LPWSTR *argv = CommandLineToArgvW(GetCommandLineW(), &argc);
    if (argc == 3 && std::wstring(argv[1]) == L"--spool-child") {
        auto file = std::wstring(argv[2]);
        LocalFree(argv);
        return mv::spool_child(file);
    }
    if (argc == 3 && std::wstring(argv[1]) == L"--usb-child") {
        auto file = std::wstring(argv[2]);
        LocalFree(argv);
        return mv::usb_child(file);
    }
    bool background = argc == 2 && std::wstring(argv[1]) == L"--background";
    LocalFree(argv);
    HANDLE mutex = CreateMutexW(nullptr, FALSE, L"Local\\MenuVexPrinterLegacy.SingleInstance");
    if (!mutex)
        return 1;
    if (GetLastError() == ERROR_ALREADY_EXISTS) {
        auto other = FindWindowW(L"MenuVexPrinterLegacy", nullptr);
        if (other) {
            ShowWindow(other, SW_SHOW);
            SetForegroundWindow(other);
        }
        CloseHandle(mutex);
        return 0;
    }
    int result = 1;
    try {
        CoInitializeEx(nullptr, COINIT_APARTMENTTHREADED);
        INITCOMMONCONTROLSEX controls{sizeof(controls), ICC_STANDARD_CLASSES | ICC_WIN95_CLASSES};
        InitCommonControlsEx(&controls);
        App state;
        app = &state;
        state.directory = mv::data_directory();
        state.store.reset(new mv::Store(mv::utf8(state.directory + L"\\agent.sqlite3")));
        state.secret.reset(new mv::Secret());
        mv::load_font();
        // Tahoma is the Persian UI font of Windows 7; Segoe UI/Consolas ship with Windows 7 too.
        state.font.body = make_font(-14, FW_NORMAL, L"Tahoma");
        state.font.bold = make_font(-14, FW_BOLD, L"Tahoma");
        state.font.small = make_font(-12, FW_NORMAL, L"Tahoma");
        state.font.tiny = make_font(-11, FW_NORMAL, L"Tahoma");
        state.font.heading = make_font(-16, FW_BOLD, L"Tahoma");
        state.font.title = make_font(-19, FW_BOLD, L"Tahoma");
        state.font.big = make_font(-26, FW_NORMAL, L"Tahoma");
        state.font.brand = make_font(-22, FW_BOLD, L"Segoe UI");
        state.font.mono = make_font(-13, FW_NORMAL, L"Consolas");
        state.page_brush = CreateSolidBrush(ink::page);
        state.card_brush = CreateSolidBrush(ink::card);
        WNDCLASSW wc{};
        wc.lpfnWndProc = window_proc;
        wc.hInstance = instance;
        wc.lpszClassName = L"MenuVexPrinterLegacy";
        wc.hCursor = LoadCursorW(nullptr, IDC_ARROW);
        wc.hIcon = LoadIconW(instance, MAKEINTRESOURCEW(101));
        wc.hbrBackground = nullptr;
        RegisterClassW(&wc);
        state.icon = wc.hIcon;
        state.brand_icon = static_cast<HICON>(
            LoadImageW(instance, MAKEINTRESOURCEW(101), IMAGE_ICON, 48, 48, LR_DEFAULTCOLOR));
        DWORD style = WS_OVERLAPPED | WS_CAPTION | WS_SYSMENU | WS_MINIMIZEBOX | WS_CLIPCHILDREN;
        RECT frame{0, 0, ClientW, ClientH};
        AdjustWindowRect(&frame, style, FALSE);
        state.config = state.store->config();
        state.printers = state.config["printers"];
        state.window =
            CreateWindowExW(0, wc.lpszClassName, L"منووکس پرینتر — Legacy", style, CW_USEDEFAULT,
                            CW_USEDEFAULT, frame.right - frame.left, frame.bottom - frame.top,
                            nullptr, nullptr, instance, nullptr);
        if (!state.window)
            throw mv::Error("STARTUP_ERROR", "Cannot create native window");
        state.set(Port, std::to_wstring(state.config["port"].get<int>()));
        state.set(Attempts, std::to_wstring(state.config["maxAttempts"].get<int>()));
        state.check(AutoStart, state.config["autostart"]);
        try {
            mv::autostart(state.config["autostart"]);
        } catch (const std::exception &e) {
            state.say(Settings, Warning, explain("AUTOSTART_ERROR", e.what()));
        }
        taskbar_created = RegisterWindowMessageW(L"TaskbarCreated");
        state.start_server();
        state.refresh();
        // The tray icon is a convenience, not a requirement: if Explorer is not ready yet (login on
        // a slow PC) keep serving the website and retry, instead of quitting with an error box.
        if (!state.add_tray()) {
            log_code(state.directory, "TRAY_RETRY");
            SetTimer(state.window, TrayTimer, 3000, nullptr);
        }
        state.start_worker();
        // Watch USB printers being plugged in/out, so new ones appear without any button.
        DEV_BROADCAST_DEVICEINTERFACE_W filter{};
        filter.dbcc_size = sizeof(filter);
        filter.dbcc_devicetype = DBT_DEVTYP_DEVICEINTERFACE;
        filter.dbcc_classguid = mv::usb_printer_interface();
        state.device_notify =
            RegisterDeviceNotificationW(state.window, &filter, DEVICE_NOTIFY_WINDOW_HANDLE);
        // Automatic discovery at start: USB presence and Windows queues always; with no printer
        // configured yet, also the LAN, and open the "add printer" page straight away.
        if (state.printers.empty())
            state.navigate(Find);
        else
            state.scan(false);
        // Never start hidden without a tray icon: the operator would have no way to open the app.
        ShowWindow(state.window, background && state.tray_ok ? SW_HIDE : SW_SHOW);
        MSG msg;
        while (GetMessageW(&msg, nullptr, 0, 0) > 0) {
            if (!IsDialogMessageW(state.window, &msg)) {
                TranslateMessage(&msg);
                DispatchMessageW(&msg);
            }
        }
        state.stopping = true;
        state.wake.notify_all();
        if (state.worker.joinable())
            state.worker.join();
        if (state.discovery.joinable())
            state.discovery.join();
        if (state.prober.joinable())
            state.prober.join();
        state.server->stop();
        if (state.device_notify)
            UnregisterDeviceNotification(state.device_notify);
        app = nullptr;
        CoUninitialize();
        result = 0;
    } catch (const std::exception &e) {
        MessageBoxW(nullptr, mv::wide(e.what()).c_str(),
                    L"منووکس Legacy — خطای راه‌اندازی",
                    MB_OK | MB_ICONERROR);
    }
    CloseHandle(mutex);
    return result;
}
