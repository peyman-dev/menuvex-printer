#include "server.hpp"
#include <algorithm>
#include <atomic>
#include <commctrl.h>
#include <objbase.h>
#include <shellapi.h>
#include <sstream>
namespace {
using namespace mv;
constexpr UINT Changed = WM_APP + 1, Tray = WM_APP + 2, Discovered = WM_APP + 3;
enum {
    PrinterList = 100,
    QueueList,
    Installed,
    Name,
    Dots,
    Paper,
    Copies,
    Font,
    Port,
    Attempts,
    AutoStart,
    Cutter,
    Role,
    AutoPrint,
    Save,
    Add,
    Remove,
    Discover,
    Test,
    Cancel,
    Reveal,
    Rotate,
    Quit,
    Open,
    RestartServer,
    SecretBox,
    Status
};
struct App {
    HWND window = nullptr;
    HFONT ui_font = nullptr;
    std::wstring directory;
    std::unique_ptr<Store> store;
    std::unique_ptr<Secret> secret;
    std::unique_ptr<Server> server;
    std::thread worker, discovery;
    std::atomic<bool> stopping{false}, worker_failed{false};
    std::mutex wake_mutex;
    std::condition_variable wake;
    Json config, printers, jobs;
    std::vector<std::string> installed;
    std::string selected;
    std::mutex discovery_mutex;
    std::vector<std::string> discovery_result;
    std::string discovery_error;
    ~App() {
        stopping = true;
        wake.notify_all();
        if (worker.joinable())
            worker.join();
        if (discovery.joinable())
            discovery.join();
        if (server)
            server->stop();
        if (ui_font)
            DeleteObject(ui_font);
    }
    HWND control(int id) { return GetDlgItem(window, id); }
    std::wstring text(int id) {
        int n = GetWindowTextLengthW(control(id));
        std::vector<wchar_t> b(n + 1);
        GetWindowTextW(control(id), b.data(), n + 1);
        return b.data();
    }
    int integer(int id) {
        auto s = text(id);
        if (s.empty() || s.find_first_not_of(L"0123456789") != std::wstring::npos || s.size() > 5)
            throw Error("INVALID_CONFIG", "Enter a valid integer");
        return std::stoi(s);
    }
    void set(int id, const std::wstring &s) { SetWindowTextW(control(id), s.c_str()); }
    bool checked(int id) { return SendMessageW(control(id), BM_GETCHECK, 0, 0) == BST_CHECKED; }
    void check(int id, bool on) {
        SendMessageW(control(id), BM_SETCHECK, on ? BST_CHECKED : BST_UNCHECKED, 0);
    }
    void changed() {
        PostMessageW(window, Changed, 0, 0);
        wake.notify_one();
    }
    void error(const std::exception &e) {
        MessageBoxW(window, wide(e.what()).c_str(), L"منووکس — نیاز به بررسی",
                    MB_OK | MB_ICONWARNING);
    }
    void start_server() {
        if (server)
            server->stop();
        server.reset(new Server(*store, *secret, [this] { changed(); }));
        try {
            server->start(static_cast<unsigned short>(config["port"].get<int>()));
            set(Status, L"برنامه آماده است؛ پرینتر را تنظیم و سپس سایت را متصل کنید.");
        } catch (const Error &e) {
            set(Status, wide(e.code + ": " + e.what()));
            log_code(directory, e.code);
        }
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
                    try {
                        auto bytes = render(work["profile"], work["document"]);
                        unsigned copies = work["profile"]["copies"];
                        for (; sent < copies; ++sent)
                            send_spooler(directory, work["profile"], bytes);
                    } catch (const Error &e) {
                        failure = sent ? unknown().json() : e.json();
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
    void refresh() {
        config = store->config();
        printers = config["printers"];
        jobs = store->queue();
        auto selected_job = SendMessageW(control(QueueList), LB_GETCURSEL, 0, 0);
        std::string old_id;
        if (selected_job >= 0 && selected_job < static_cast<LRESULT>(last_jobs.size()))
            old_id = last_jobs[static_cast<std::size_t>(selected_job)]["jobId"];
        SendMessageW(control(PrinterList), LB_RESETCONTENT, 0, 0);
        for (std::size_t i = 0; i < printers.size(); ++i) {
            auto line = wide(printers[i]["name"].get<std::string>() + " — " +
                             printers[i]["connection"]["queueName"].get<std::string>());
            SendMessageW(control(PrinterList), LB_ADDSTRING, 0,
                         reinterpret_cast<LPARAM>(line.c_str()));
            if (printers[i]["id"] == selected)
                SendMessageW(control(PrinterList), LB_SETCURSEL, i, 0);
        }
        SendMessageW(control(QueueList), LB_RESETCONTENT, 0, 0);
        for (std::size_t i = 0; i < jobs.size(); ++i) {
            auto &j = jobs[i];
            std::string line = j["jobId"].get<std::string>() + " | " +
                               j["status"].get<std::string>() + " | attempts " +
                               std::to_string(j["attempts"].get<int>());
            if (!j["error"].is_null())
                line += " | " + j["error"]["code"].get<std::string>();
            auto w = wide(line);
            SendMessageW(control(QueueList), LB_ADDSTRING, 0, reinterpret_cast<LPARAM>(w.c_str()));
            if (j["jobId"] == old_id)
                SendMessageW(control(QueueList), LB_SETCURSEL, i, 0);
        }
        last_jobs = jobs;
        if (worker_failed)
            set(Status, L"QUEUE_ERROR: پردازش چاپ متوقف شده؛ صف را بررسی و برنامه را دوباره اجرا "
                        L"کنید. چاپ مجدد خودکار انجام نمی‌شود.");
    }
    Json last_jobs = Json::array();
    void clear() {
        selected.clear();
        set(Name, L"");
        set(Dots, L"576");
        set(Paper, L"80");
        set(Copies, L"1");
        set(Font, L"24");
        check(Cutter, true);
        SendMessageW(control(Role), CB_SETCURSEL, 0, 0);
        check(AutoPrint, false);
        SendMessageW(control(PrinterList), LB_SETCURSEL, -1, 0);
    }
    void select() {
        auto i = SendMessageW(control(PrinterList), LB_GETCURSEL, 0, 0);
        if (i < 0 || i >= static_cast<LRESULT>(printers.size()))
            return;
        auto p = printers[static_cast<std::size_t>(i)];
        selected = p["id"];
        set(Name, wide(p["name"]));
        for (auto pair : {std::make_pair(Dots, "widthDots"),
                          {Paper, "paperMm"},
                          {Copies, "copies"},
                          {Font, "fontSize"}})
            set(pair.first, std::to_wstring(p[pair.second].get<int>()));
        check(Cutter, p["cut"]);
        auto q = wide(p["connection"]["queueName"]);
        auto index = SendMessageW(control(Installed), CB_FINDSTRINGEXACT, -1,
                                  reinterpret_cast<LPARAM>(q.c_str()));
        if (index == CB_ERR) {
            index = SendMessageW(control(Installed), CB_ADDSTRING, 0,
                                 reinterpret_cast<LPARAM>(q.c_str()));
            installed.push_back(p["connection"]["queueName"]);
        }
        SendMessageW(control(Installed), CB_SETCURSEL, index, 0);
        SendMessageW(control(Role), CB_SETCURSEL, 0, 0);
        check(AutoPrint, false);
        for (auto &r : config["routes"])
            if (r["printerId"] == selected) {
                auto role = r["role"].get<std::string>();
                int index = role == "invoice" ? 1 : role == "kitchen" ? 2 : role == "bar" ? 3 : 0;
                SendMessageW(control(Role), CB_SETCURSEL, index, 0);
                check(AutoPrint, r["autoPrint"]);
            }
    }
    void discover() {
        if (discovery.joinable())
            return;
        EnableWindow(control(Discover), FALSE);
        set(Status,
            L"در حال دریافت پرینترهای نصب‌شدهٔ ویندوز…");
        discovery = std::thread([this] {
            try {
                auto list = installed_printers();
                std::lock_guard<std::mutex> lock(discovery_mutex);
                discovery_result = std::move(list);
                discovery_error.clear();
            } catch (const std::exception &e) {
                std::lock_guard<std::mutex> lock(discovery_mutex);
                discovery_error = e.what();
            }
            PostMessageW(window, Discovered, 0, 0);
        });
    }
    void discovered() {
        if (discovery.joinable())
            discovery.join();
        EnableWindow(control(Discover), TRUE);
        std::lock_guard<std::mutex> lock(discovery_mutex);
        if (!discovery_error.empty()) {
            set(Status, wide(discovery_error));
            return;
        }
        installed = discovery_result;
        SendMessageW(control(Installed), CB_RESETCONTENT, 0, 0);
        for (auto &name : installed) {
            auto w = wide(name);
            SendMessageW(control(Installed), CB_ADDSTRING, 0, reinterpret_cast<LPARAM>(w.c_str()));
        }
        if (!installed.empty())
            SendMessageW(control(Installed), CB_SETCURSEL, 0, 0);
        if (!selected.empty())
            select();
        set(Status, L"صف پرینتر ESC/POS را انتخاب کنید؛ وجود صف به معنی آماده بودن چاپگر نیست.");
    }
    void save() {
        auto c = store->config();
        c["port"] = integer(Port);
        c["maxAttempts"] = integer(Attempts);
        c["autostart"] = checked(AutoStart);
        int i = static_cast<int>(SendMessageW(control(Installed), CB_GETCURSEL, 0, 0));
        if (i >= 0 && i < static_cast<int>(installed.size()) && !text(Name).empty()) {
            if (selected.empty()) {
                auto random = random_base64();
                random.erase(
                    std::remove_if(random.begin(), random.end(),
                                   [](char ch) { return ch == '/' || ch == '+' || ch == '='; }),
                    random.end());
                selected = "printer:" + random;
            }
            Json p = {{"id", selected},
                      {"name", utf8(text(Name))},
                      {"connection", {{"type", "spooler"}, {"queueName", installed[i]}}},
                      {"paperMm", integer(Paper)},
                      {"widthDots", integer(Dots)},
                      {"copies", integer(Copies)},
                      {"cut", checked(Cutter)},
                      {"fontFamily", "Noto Sans Arabic"},
                      {"fontSize", integer(Font)}};
            Json ps = Json::array();
            for (auto &old : c["printers"])
                if (old["id"] != selected)
                    ps.push_back(old);
            ps.push_back(p);
            c["printers"] = ps;
            int role = static_cast<int>(SendMessageW(control(Role), CB_GETCURSEL, 0, 0));
            const char *roles[] = {"", "invoice", "kitchen", "bar"};
            Json routes = Json::array();
            for (auto &r : c["routes"])
                if (r["printerId"] != selected && (role <= 0 || r["role"] != roles[role]))
                    routes.push_back(r);
            if (role > 0 && role < 4)
                routes.push_back({{"role", roles[role]},
                                  {"printerId", selected},
                                  {"autoPrint", checked(AutoPrint)}});
            c["routes"] = routes;
        }
        validate_config(c);
        autostart(c["autostart"]);
        store->save(c);
        config = c;
        server->event({{"type", "resync"}, {"version", 1}});
        refresh();
        MessageBoxW(window,
                    L"ذخیره شد. تغییر پورت پس از خروج و اجرای دوباره اعمال "
                    L"می‌شود. "
                    L"پیش از فعال‌کردن چاپ خودکار، چاپ واقعی را بررسی "
                    L"کنید.",
                    L"MenuVex", MB_OK);
    }
    void remove() {
        if (selected.empty())
            return;
        if (MessageBoxW(window,
                        L"این پرینتر حذف شود؟ تنظیمات کارهای قبلی حفظ "
                        L"می‌شود؛ "
                        L"کارهای معلق را جداگانه بررسی کنید.",
                        L"تأیید", MB_YESNO | MB_ICONWARNING) != IDYES)
            return;
        auto c = store->config();
        Json ps = Json::array(), routes = Json::array();
        for (auto &p : c["printers"])
            if (p["id"] != selected)
                ps.push_back(p);
        for (auto &r : c["routes"])
            if (r["printerId"] != selected)
                routes.push_back(r);
        c["printers"] = ps;
        c["routes"] = routes;
        store->save(c);
        clear();
        server->event({{"type", "resync"}, {"version", 1}});
        refresh();
    }
    void test() {
        if (selected.empty())
            throw Error("PRINTER_NOT_FOUND", "Save and select a profile first");
        if (worker_failed)
            throw Error("AGENT_NOT_READY", "Worker stopped; restart first");
        auto id = "test:" + std::to_string(now()) + ":" + std::to_string(GetTickCount());
        auto j = store->enqueue(id, selected,
                                {{"type", "receipt"},
                                 {"lines",
                                  {u8"آزمون چاپ فارسی — سلام دنیا", "MenuVex Legacy / Windows RAW",
                                   u8"۱۲۳۴۵۶۷۸۹۰ / 0123456789"}}});
        server->event({{"type", "print.queued"}, {"version", 1}, {"job", j}});
        wake.notify_one();
        refresh();
    }
    void cancel() {
        auto i = SendMessageW(control(QueueList), LB_GETCURSEL, 0, 0);
        if (i < 0 || i >= static_cast<LRESULT>(last_jobs.size()))
            return;
        auto j = store->cancel(last_jobs[static_cast<std::size_t>(i)]["jobId"]);
        server->event({{"type", "print.cancelled"}, {"version", 1}, {"job", j}});
        refresh();
    }
    void quit() {
        if (stopping.exchange(true))
            return;
        set(Status, L"پس از عملیات چاپ جاری خارج می‌شود؛ سفارش‌های معلق را دوباره ارسال "
                    L"نکنید.");
        wake.notify_all();
        if (worker.joinable())
            worker.join();
        if (discovery.joinable())
            discovery.join();
        server->stop();
        NOTIFYICONDATAW icon{};
        icon.cbSize = sizeof(icon);
        icon.hWnd = window;
        icon.uID = 1;
        Shell_NotifyIconW(NIM_DELETE, &icon);
        DestroyWindow(window);
    }
};
App *app = nullptr;
void control(HWND parent, const wchar_t *cls, const wchar_t *label, int id, int x, int y, int w,
             int h, DWORD style = 0) {
    bool text_label = wcscmp(cls, L"STATIC") == 0;
    bool button = wcscmp(cls, L"BUTTON") == 0;
    DWORD ex = (wcscmp(cls, L"EDIT") == 0 || wcscmp(cls, L"LISTBOX") == 0) ? WS_EX_CLIENTEDGE : 0;
    // Keep technical fields (port, pairing key, queue IDs) LTR.
    if (text_label || button || id == Name)
        ex |= WS_EX_RTLREADING;
    if (text_label)
        style |= SS_RIGHT;
    if (id == Save || id == Test)
        style |= BS_OWNERDRAW;
    auto handle = CreateWindowExW(ex, cls, label, WS_CHILD | WS_VISIBLE | style, x, y, w, h, parent,
                                  reinterpret_cast<HMENU>(static_cast<INT_PTR>(id)),
                                  GetModuleHandleW(nullptr), nullptr);
    SendMessageW(handle, WM_SETFONT,
                 reinterpret_cast<WPARAM>(app && app->ui_font
                                              ? reinterpret_cast<HGDIOBJ>(app->ui_font)
                                              : GetStockObject(DEFAULT_GUI_FONT)),
                 TRUE);
}
void label(HWND w, const wchar_t *s, int x, int y, int width = 150) {
    control(w, L"STATIC", s, 0, x, y, width, 20);
}
void create_ui(HWND w) {
    control(w, L"STATIC", L"منووکس پرینتر — نسخهٔ Legacy | ویندوز ۷ SP1 و جدیدتر | آزمایشی", Status,
            20, 15, 910, 38);
    label(w, L"پرینترهای شما", 20, 58, 300);
    control(w, L"LISTBOX", L"", PrinterList, 20, 80, 900, 80, LBS_NOTIFY | WS_VSCROLL);
    label(w, L"پرینتر نصب‌شده در ویندوز", 20, 177, 190);
    control(w, L"COMBOBOX", L"", Installed, 210, 172, 510, 250,
            CBS_DROPDOWNLIST | WS_VSCROLL | WS_TABSTOP);
    control(w, L"BUTTON", L"جستجوی پرینترهای ویندوز", Discover, 735, 171, 185, 26, WS_TABSTOP);
    label(w, L"نام پرینتر", 20, 214);
    control(w, L"EDIT", L"", Name, 170, 209, 360, 26, ES_AUTOHSCROLL | WS_TABSTOP);
    control(w, L"BUTTON", L"پرینتر جدید", Add, 550, 209, 115, 26, WS_TABSTOP);
    control(w, L"BUTTON", L"حذف پرینتر", Remove, 680, 209, 130, 26, WS_TABSTOP);
    int x = 20;
    for (auto p : {std::make_pair(Paper, L"کاغذ: ۵۸ یا ۸۰ میلی‌متر"),
                   {Dots, L"عرض چاپ: ۳۸۴ یا ۵۷۶ نقطه"},
                   {Copies, L"تعداد نسخه: ۱ تا ۳"},
                   {Font, L"اندازهٔ قلم: ۱۲ تا ۴۸"}}) {
        label(w, p.second, x, 253, 210);
        control(w, L"EDIT", L"", p.first, x, 277, 190, 26, ES_NUMBER | WS_TABSTOP);
        x += 230;
    }
    label(w, L"نقش پرینتر", 20, 319);
    control(w, L"COMBOBOX", L"", Role, 170, 313, 200, 180, CBS_DROPDOWNLIST | WS_TABSTOP);
    for (auto r : {L"بدون نقش", L"فاکتور", L"آشپزخانه", L"بار"})
        SendMessageW(GetDlgItem(w, Role), CB_ADDSTRING, 0, reinterpret_cast<LPARAM>(r));
    control(w, L"BUTTON", L"چاپ خودکار (نیازمند اتصال سایت)", AutoPrint, 400, 313, 290, 26,
            BS_AUTOCHECKBOX | WS_TABSTOP);
    control(w, L"BUTTON", L"برش کاغذ", Cutter, 720, 313, 190, 26, BS_AUTOCHECKBOX | WS_TABSTOP);
    label(w, L"پورت محلی", 20, 356);
    control(w, L"EDIT", L"8765", Port, 110, 350, 90, 26, ES_NUMBER | WS_TABSTOP);
    label(w, L"تعداد تلاش", 220, 356);
    control(w, L"EDIT", L"3", Attempts, 320, 350, 70, 26, ES_NUMBER | WS_TABSTOP);
    control(w, L"BUTTON", L"اجرا هنگام ورود", AutoStart, 420, 350, 180, 26,
            BS_AUTOCHECKBOX | WS_TABSTOP);
    control(w, L"BUTTON", L"ذخیرهٔ تنظیمات", Save, 620, 349, 140, 28, WS_TABSTOP);
    control(w, L"BUTTON", L"چاپ آزمایشی", Test, 780, 349, 140, 28, WS_TABSTOP);
    label(
        w,
        L"اتصال سایت: کلید را فقط در سایت رسمی منووکس وارد کنید؛ آن را در چت یا گزارش خطا نفرستید.",
        20, 395, 900);
    control(w, L"BUTTON", L"نمایش کلید: ۶۰ ثانیه", Reveal, 20, 420, 190, 28, WS_TABSTOP);
    control(w, L"EDIT", L"", SecretBox, 220, 420, 450, 28, ES_READONLY | ES_AUTOHSCROLL);
    control(w, L"BUTTON", L"لغو اتصال مرورگرها", Rotate, 700, 420, 220, 28, WS_TABSTOP);
    label(w,
          L"صف چاپ — تکمیل یعنی تحویل به ویندوز، نه تأیید چاپ کاغذ. نتیجهٔ نامشخص را پیش از چاپ "
          L"مجدد بررسی کنید.",
          20, 470, 910);
    control(w, L"LISTBOX", L"", QueueList, 20, 495, 900, 155, LBS_NOTIFY | WS_VSCROLL | WS_HSCROLL);
    SendMessageW(GetDlgItem(w, QueueList), LB_SETHORIZONTALEXTENT, 1400, 0);
    control(w, L"BUTTON", L"لغو کار انتخاب‌شده در صف", Cancel, 20, 660, 230,
            28, WS_TABSTOP);
    control(w, L"BUTTON", L"خروج از برنامه", Quit, 780, 660, 140, 28, WS_TABSTOP);
}
LRESULT CALLBACK window_proc(HWND w, UINT message, WPARAM wp, LPARAM lp) {
    try {
        switch (message) {
        case WM_ERASEBKGND: {
            RECT rect;
            GetClientRect(w, &rect);
            HDC dc = reinterpret_cast<HDC>(wp);
            SetDCBrushColor(dc, RGB(245, 249, 247));
            FillRect(dc, &rect, reinterpret_cast<HBRUSH>(GetStockObject(DC_BRUSH)));
            return 1;
        }
        case WM_CTLCOLORSTATIC:
        case WM_CTLCOLORBTN: {
            HDC dc = reinterpret_cast<HDC>(wp);
            SetTextColor(dc, RGB(24, 69, 58));
            SetBkColor(dc, RGB(245, 249, 247));
            SetDCBrushColor(dc, RGB(245, 249, 247));
            return reinterpret_cast<LRESULT>(GetStockObject(DC_BRUSH));
        }
        case WM_DRAWITEM: {
            auto item = reinterpret_cast<DRAWITEMSTRUCT *>(lp);
            if (item->CtlID != Save && item->CtlID != Test)
                break;
            bool disabled = (item->itemState & ODS_DISABLED) != 0;
            SetDCBrushColor(item->hDC, disabled                           ? RGB(105, 122, 116)
                                       : (item->itemState & ODS_SELECTED) ? RGB(12, 92, 72)
                                                                          : RGB(15, 125, 98));
            FillRect(item->hDC, &item->rcItem, reinterpret_cast<HBRUSH>(GetStockObject(DC_BRUSH)));
            SetTextColor(item->hDC, RGB(255, 255, 255));
            SetBkMode(item->hDC, TRANSPARENT);
            auto old_font =
                SelectObject(item->hDC, app->ui_font ? reinterpret_cast<HGDIOBJ>(app->ui_font)
                                                     : GetStockObject(DEFAULT_GUI_FONT));
            wchar_t text[128]{};
            GetWindowTextW(item->hwndItem, text, 128);
            DrawTextW(item->hDC, text, -1, &item->rcItem,
                      DT_CENTER | DT_VCENTER | DT_SINGLELINE | DT_RTLREADING);
            SelectObject(item->hDC, old_font);
            if (item->itemState & ODS_FOCUS) {
                RECT focus = item->rcItem;
                InflateRect(&focus, -3, -3);
                DrawFocusRect(item->hDC, &focus);
            }
            return TRUE;
        }
        case WM_CREATE:
            create_ui(w);
            return 0;
        case WM_CLOSE:
            ShowWindow(w, SW_HIDE);
            return 0;
        case WM_DESTROY:
            PostQuitMessage(0);
            return 0;
        case Changed:
            if (app && !app->stopping)
                app->refresh();
            return 0;
        case Discovered:
            if (app && !app->stopping)
                app->discovered();
            return 0;
        case WM_TIMER:
            if (wp == 1) {
                app->set(SecretBox, L"");
                KillTimer(w, 1);
            }
            return 0;
        case Tray:
            if (lp == WM_LBUTTONDBLCLK) {
                ShowWindow(w, SW_SHOW);
                SetForegroundWindow(w);
            } else if (lp == WM_RBUTTONUP) {
                HMENU menu = CreatePopupMenu();
                AppendMenuW(menu, MF_STRING, Open, L"باز کردن منووکس");
                AppendMenuW(menu, MF_STRING, Quit, L"خروج از برنامه");
                POINT pt;
                GetCursorPos(&pt);
                SetForegroundWindow(w);
                TrackPopupMenu(menu, TPM_RIGHTBUTTON, pt.x, pt.y, 0, w, nullptr);
                DestroyMenu(menu);
            }
            return 0;
        case WM_COMMAND:
            if (!app)
                return 0;
            switch (LOWORD(wp)) {
            case PrinterList:
                if (HIWORD(wp) == LBN_SELCHANGE)
                    app->select();
                break;
            case Add:
                app->clear();
                break;
            case Discover:
                app->discover();
                break;
            case Save:
                app->save();
                break;
            case Remove:
                app->remove();
                break;
            case Test:
                app->test();
                break;
            case Cancel:
                app->cancel();
                break;
            case Reveal:
                app->set(SecretBox, wide(app->secret->reveal()));
                SetTimer(w, 1, 60000, nullptr);
                break;
            case Rotate:
                if (MessageBoxW(w, L"اتصال همهٔ مرورگرها لغو شود؟ اتصال دوباره نیازمند کلید است.",
                                L"تأیید", MB_YESNO | MB_ICONWARNING) == IDYES) {
                    app->secret->rotate();
                    app->server->revoke();
                    app->set(SecretBox, L"");
                }
                break;
            case Open:
                ShowWindow(w, SW_SHOW);
                SetForegroundWindow(w);
                break;
            case Quit:
                app->quit();
                break;
            }
            return 0;
        }
    } catch (const std::exception &e) {
        if (app)
            app->error(e);
        else
            MessageBoxW(w, L"راه‌اندازی انجام نشد", L"MenuVex",
                        MB_OK | MB_ICONERROR);
    }
    return DefWindowProcW(w, message, wp, lp);
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
        App state;
        app = &state;
        state.directory = mv::data_directory();
        state.store.reset(new mv::Store(mv::utf8(state.directory + L"\\agent.sqlite3")));
        state.secret.reset(new mv::Secret());
        mv::load_font();
        state.ui_font = CreateFontW(-15, 0, 0, 0, FW_NORMAL, FALSE, FALSE, FALSE, DEFAULT_CHARSET,
                                    OUT_DEFAULT_PRECIS, CLIP_DEFAULT_PRECIS, CLEARTYPE_QUALITY,
                                    DEFAULT_PITCH, L"Tahoma");
        WNDCLASSW wc{};
        wc.lpfnWndProc = window_proc;
        wc.hInstance = instance;
        wc.lpszClassName = L"MenuVexPrinterLegacy";
        wc.hCursor = LoadCursorW(nullptr, IDC_ARROW);
        wc.hIcon = LoadIconW(instance, MAKEINTRESOURCEW(101));
        wc.hbrBackground = reinterpret_cast<HBRUSH>(COLOR_WINDOW + 1);
        RegisterClassW(&wc);
        state.window =
            CreateWindowExW(0, wc.lpszClassName, L"منووکس پرینتر — Legacy",
                            WS_OVERLAPPED | WS_CAPTION | WS_SYSMENU | WS_MINIMIZEBOX, CW_USEDEFAULT,
                            CW_USEDEFAULT, 970, 750, nullptr, nullptr, instance, nullptr);
        if (!state.window)
            throw mv::Error("STARTUP_ERROR", "Cannot create native window");
        state.config = state.store->config();
        state.clear();
        state.set(Port, std::to_wstring(state.config["port"].get<int>()));
        state.set(Attempts, std::to_wstring(state.config["maxAttempts"].get<int>()));
        state.check(AutoStart, state.config["autostart"]);
        try {
            mv::autostart(state.config["autostart"]);
        } catch (const std::exception &e) {
            state.error(e);
        }
        state.start_server();
        state.refresh();
        NOTIFYICONDATAW icon{};
        icon.cbSize = sizeof(icon);
        icon.hWnd = state.window;
        icon.uID = 1;
        icon.uFlags = NIF_MESSAGE | NIF_ICON | NIF_TIP;
        icon.uCallbackMessage = Tray;
        icon.hIcon = wc.hIcon;
        lstrcpynW(icon.szTip, L"منووکس پرینتر — Legacy", 128);
        if (!Shell_NotifyIconW(NIM_ADD, &icon))
            throw mv::Error("TRAY_ERROR", "Cannot create system tray icon");
        state.start_worker();
        ShowWindow(state.window, background ? SW_HIDE : SW_SHOW);
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
        state.server->stop();
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
