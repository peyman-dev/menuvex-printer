#include "platform.hpp"
#include <algorithm>
#include <atomic>
#include <mutex>
#include <thread>
#include <utility>
#include <vector>
#include <bcrypt.h>
#include <cstring>
#include <fstream>
#include <sddl.h>
#include <shlobj.h>
#include <usp10.h>
#include <wincred.h>
#include <wincrypt.h>
#include <winspool.h>
#include <ws2tcpip.h>
// After winspool/ws2tcpip: SetupAPI (USB printer discovery) and IP Helper (adapter list).
#include <devpropdef.h>
#include <iphlpapi.h>
#include <setupapi.h>
namespace mv {
struct Handle {
    HANDLE h = nullptr;
    ~Handle() {
        if (h && h != INVALID_HANDLE_VALUE)
            CloseHandle(h);
    }
    operator HANDLE() const { return h; }
};
std::wstring wide(const std::string &s) {
    if (s.empty())
        return {};
    int n = MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, s.data(), static_cast<int>(s.size()),
                                nullptr, 0);
    if (!n)
        throw Error("INVALID_PAYLOAD", "Invalid UTF-8");
    std::wstring w(n, 0);
    MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, s.data(), static_cast<int>(s.size()), &w[0],
                        n);
    return w;
}
std::string utf8(const std::wstring &s) {
    if (s.empty())
        return {};
    int n = WideCharToMultiByte(CP_UTF8, WC_ERR_INVALID_CHARS, s.data(), static_cast<int>(s.size()),
                                nullptr, 0, nullptr, nullptr);
    if (!n)
        throw Error("INVALID_PAYLOAD", "Invalid UTF-16");
    std::string out(n, 0);
    WideCharToMultiByte(CP_UTF8, WC_ERR_INVALID_CHARS, s.data(), static_cast<int>(s.size()),
                        &out[0], n, nullptr, nullptr);
    return out;
}
std::wstring executable() {
    std::vector<wchar_t> b(32768);
    DWORD n = GetModuleFileNameW(nullptr, b.data(), static_cast<DWORD>(b.size()));
    if (!n || n == b.size())
        throw Error("STARTUP_ERROR", "Cannot locate executable");
    return {b.data(), n};
}
std::wstring data_directory() {
    PWSTR root = nullptr;
    if (FAILED(SHGetKnownFolderPath(FOLDERID_LocalAppData, 0, nullptr, &root)))
        throw Error("STORAGE_ERROR", "LocalAppData unavailable");
    std::wstring dir = std::wstring(root) + L"\\MenuVexPrinterLegacy";
    CoTaskMemFree(root);
    Handle token;
    if (!OpenProcessToken(GetCurrentProcess(), TOKEN_QUERY, &token.h))
        throw Error("STORAGE_ERROR", "Cannot inspect user identity");
    DWORD bytes = 0;
    GetTokenInformation(token, TokenUser, nullptr, 0, &bytes);
    std::vector<unsigned char> info(bytes);
    if (!GetTokenInformation(token, TokenUser, info.data(), bytes, &bytes))
        throw Error("STORAGE_ERROR", "Cannot inspect user identity");
    LPWSTR sid = nullptr;
    if (!ConvertSidToStringSidW(reinterpret_cast<TOKEN_USER *>(info.data())->User.Sid, &sid))
        throw Error("STORAGE_ERROR", "Cannot encode identity");
    std::wstring sddl = L"D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;" + std::wstring(sid) + L")";
    LocalFree(sid);
    PSECURITY_DESCRIPTOR sd = nullptr;
    if (!ConvertStringSecurityDescriptorToSecurityDescriptorW(sddl.c_str(), SDDL_REVISION_1, &sd,
                                                              nullptr))
        throw Error("STORAGE_ERROR", "Cannot protect storage");
    SECURITY_ATTRIBUTES sa = {sizeof(sa), sd, FALSE};
    BOOL made = CreateDirectoryW(dir.c_str(), &sa);
    DWORD error = GetLastError();
    if (!made && error != ERROR_ALREADY_EXISTS) {
        LocalFree(sd);
        throw Error("STORAGE_ERROR", "Cannot create storage directory");
    }
    BOOL secured = SetFileSecurityW(
        dir.c_str(), DACL_SECURITY_INFORMATION | PROTECTED_DACL_SECURITY_INFORMATION, sd);
    LocalFree(sd);
    if (!secured)
        throw Error("STORAGE_ERROR", "Cannot restrict storage permissions");
    return dir;
}
std::string read_file(const std::wstring &path, std::size_t max) {
    Handle h;
    h.h = CreateFileW(path.c_str(), GENERIC_READ, FILE_SHARE_READ, nullptr, OPEN_EXISTING,
                      FILE_ATTRIBUTE_NORMAL, nullptr);
    LARGE_INTEGER size{};
    if (h.h == INVALID_HANDLE_VALUE || !GetFileSizeEx(h, &size) || size.QuadPart < 0 ||
        static_cast<unsigned long long>(size.QuadPart) > max)
        throw Error("STORAGE_ERROR", "Cannot read bounded data file");
    std::string out(static_cast<std::size_t>(size.QuadPart), '\0');
    DWORD read = 0;
    if (!out.empty() && (!ReadFile(h, &out[0], static_cast<DWORD>(out.size()), &read, nullptr) ||
                         read != out.size()))
        throw Error("STORAGE_ERROR", "Incomplete data read");
    return out;
}
void write_file(const std::wstring &path, const std::string &data) {
    Handle h;
    h.h = CreateFileW(path.c_str(), GENERIC_WRITE, 0, nullptr, CREATE_NEW, FILE_ATTRIBUTE_NORMAL,
                      nullptr);
    if (h.h == INVALID_HANDLE_VALUE)
        throw Error("STORAGE_ERROR", "Cannot create temporary file");
    DWORD written = 0;
    if (!WriteFile(h, data.data(), static_cast<DWORD>(data.size()), &written, nullptr) ||
        written != data.size() || !FlushFileBuffers(h))
        throw Error("STORAGE_ERROR", "Cannot persist temporary data");
}
std::string base64(const std::vector<unsigned char> &data) {
    DWORD n = 0;
    DWORD flags = CRYPT_STRING_BASE64 | CRYPT_STRING_NOCRLF;
    if (!CryptBinaryToStringA(data.data(), static_cast<DWORD>(data.size()), flags, nullptr, &n))
        throw Error("AUTH_FAILED", "Base64 unavailable");
    std::string out(n, 0);
    if (!CryptBinaryToStringA(data.data(), static_cast<DWORD>(data.size()), flags, &out[0], &n))
        throw Error("AUTH_FAILED", "Base64 unavailable");
    while (!out.empty() && out.back() == 0)
        out.pop_back();
    return out;
}
std::vector<unsigned char> unbase64(const std::string &s) {
    DWORD n = 0;
    if (s.size() > 2 * 1024 * 1024 ||
        !CryptStringToBinaryA(s.data(), static_cast<DWORD>(s.size()),
                              CRYPT_STRING_BASE64 | CRYPT_STRING_STRICT, nullptr, &n, nullptr,
                              nullptr))
        throw Error("AUTH_FAILED", "Invalid base64");
    std::vector<unsigned char> out(n);
    if (!CryptStringToBinaryA(s.data(), static_cast<DWORD>(s.size()),
                              CRYPT_STRING_BASE64 | CRYPT_STRING_STRICT, out.data(), &n, nullptr,
                              nullptr))
        throw Error("AUTH_FAILED", "Invalid base64");
    return out;
}
static std::vector<unsigned char> random_bytes() {
    std::vector<unsigned char> b(32);
    if (BCryptGenRandom(nullptr, b.data(), static_cast<ULONG>(b.size()),
                        BCRYPT_USE_SYSTEM_PREFERRED_RNG) < 0)
        throw Error("SECURE_STORAGE_UNAVAILABLE", "Secure random unavailable");
    return b;
}
std::string random_base64() {
    auto b = random_bytes();
    auto s = base64(b);
    SecureZeroMemory(b.data(), b.size());
    return s;
}
static const wchar_t *credential = L"MenuVex.Printer.Legacy.Pairing.v1";
static void store_key(const std::vector<unsigned char> &b) {
    CREDENTIALW c{};
    c.Type = CRED_TYPE_GENERIC;
    c.TargetName = const_cast<LPWSTR>(credential);
    c.CredentialBlobSize = static_cast<DWORD>(b.size());
    c.CredentialBlob = const_cast<LPBYTE>(b.data());
    c.Persist = CRED_PERSIST_LOCAL_MACHINE;
    if (!CredWriteW(&c, 0))
        throw Error("SECURE_STORAGE_UNAVAILABLE", "Windows Credential Manager unavailable");
}
Secret::Secret() {
    PCREDENTIALW c = nullptr;
    if (CredReadW(credential, CRED_TYPE_GENERIC, 0, &c)) {
        if (c->CredentialBlobSize != 32) {
            CredFree(c);
            throw Error("SECURE_STORAGE_UNAVAILABLE", "Invalid saved pairing credential");
        }
        key_.assign(c->CredentialBlob, c->CredentialBlob + 32);
        SecureZeroMemory(c->CredentialBlob, c->CredentialBlobSize);
        CredFree(c);
    } else if (GetLastError() == ERROR_NOT_FOUND) {
        key_ = random_bytes();
        store_key(key_);
    } else
        throw Error("SECURE_STORAGE_UNAVAILABLE", "Unlock Windows Credential Manager");
}
Secret::~Secret() { SecureZeroMemory(key_.data(), key_.size()); }
std::string Secret::reveal() {
    std::lock_guard<std::mutex> l(mutex_);
    return base64(key_);
}
void Secret::rotate() {
    std::lock_guard<std::mutex> l(mutex_);
    auto b = random_bytes();
    store_key(b);
    SecureZeroMemory(key_.data(), key_.size());
    key_.swap(b);
    SecureZeroMemory(b.data(), b.size());
}
std::string Secret::proof(const std::string &nonce, const std::string &origin, bool server) {
    std::lock_guard<std::mutex> l(mutex_);
    BCRYPT_ALG_HANDLE algorithm = nullptr;
    BCRYPT_HASH_HANDLE hash = nullptr;
    std::vector<unsigned char> digest(32);
    auto message =
        std::string(server ? "menuvex-print-agent:server:v1\n" : "menuvex-print-agent:v1\n") +
        nonce + "\n" + origin;
    auto status = BCryptOpenAlgorithmProvider(&algorithm, BCRYPT_SHA256_ALGORITHM, nullptr,
                                              BCRYPT_ALG_HANDLE_HMAC_FLAG);
    if (status >= 0)
        status = BCryptCreateHash(algorithm, &hash, nullptr, 0, key_.data(),
                                  static_cast<ULONG>(key_.size()), 0);
    if (status >= 0)
        status = BCryptHashData(hash, reinterpret_cast<PUCHAR>(&message[0]),
                                static_cast<ULONG>(message.size()), 0);
    if (status >= 0)
        status = BCryptFinishHash(hash, digest.data(), 32, 0);
    if (hash)
        BCryptDestroyHash(hash);
    if (algorithm)
        BCryptCloseAlgorithmProvider(algorithm, 0);
    if (status < 0)
        throw Error("AUTH_FAILED", "HMAC unavailable");
    return base64(digest);
}
bool Secret::verify(const std::string &nonce, const std::string &origin,
                    const std::string &supplied) {
    try {
        auto expected = unbase64(proof(nonce, origin, false));
        auto actual = unbase64(supplied);
        if (actual.size() != expected.size())
            return false;
        unsigned char diff = 0;
        for (std::size_t i = 0; i < actual.size(); ++i)
            diff |= actual[i] ^ expected[i];
        return diff == 0;
    } catch (...) {
        return false;
    }
}
void autostart(bool enabled) {
    HKEY key = nullptr;
    if (RegCreateKeyExW(HKEY_CURRENT_USER, L"Software\\Microsoft\\Windows\\CurrentVersion\\Run", 0,
                        nullptr, 0, KEY_SET_VALUE, nullptr, &key, nullptr) != ERROR_SUCCESS)
        throw Error("AUTOSTART_ERROR", "Cannot access login startup settings");
    LSTATUS result;
    if (enabled) {
        auto command = L"\"" + executable() + L"\" --background";
        result = RegSetValueExW(key, L"MenuVexPrinterLegacy", 0, REG_SZ,
                                reinterpret_cast<const BYTE *>(command.c_str()),
                                static_cast<DWORD>((command.size() + 1) * sizeof(wchar_t)));
    } else {
        result = RegDeleteValueW(key, L"MenuVexPrinterLegacy");
        if (result == ERROR_FILE_NOT_FOUND)
            result = ERROR_SUCCESS;
    }
    RegCloseKey(key);
    if (result != ERROR_SUCCESS)
        throw Error("AUTOSTART_ERROR", "Cannot update login startup settings");
}
std::vector<std::string> installed_printers() {
    DWORD needed = 0, count = 0;
    DWORD flags = PRINTER_ENUM_LOCAL | PRINTER_ENUM_CONNECTIONS;
    EnumPrintersW(flags, nullptr, 4, nullptr, 0, &needed, &count);
    if (needed == 0) {
        if (GetLastError() != ERROR_SUCCESS && GetLastError() != ERROR_INSUFFICIENT_BUFFER)
            throw Error("SPOOLER_UNAVAILABLE", "Windows printer enumeration failed");
        return {};
    }
    if (needed > 4 * 1024 * 1024)
        throw Error("SPOOLER_UNAVAILABLE", "Printer list is too large");
    std::vector<BYTE> b(needed);
    if (!EnumPrintersW(flags, nullptr, 4, b.data(), needed, &needed, &count))
        throw Error("SPOOLER_UNAVAILABLE", "Cannot list Windows printers");
    std::vector<std::string> out;
    auto p = reinterpret_cast<PRINTER_INFO_4W *>(b.data());
    for (DWORD i = 0; i < count; ++i)
        if (p[i].pPrinterName)
            out.push_back(utf8(p[i].pPrinterName));
    return out;
}
void load_font() {
    HRSRC r = FindResourceW(nullptr, MAKEINTRESOURCEW(102), RT_RCDATA);
    HGLOBAL h = r ? LoadResource(nullptr, r) : nullptr;
    void *data = h ? LockResource(h) : nullptr;
    DWORD n = 0;
    if (!data || !AddFontMemResourceEx(data, SizeofResource(nullptr, r), nullptr, &n) || n == 0)
        throw Error("FONT_UNAVAILABLE", "Bundled Arabic font could not load");
}
struct Analysis {
    SCRIPT_STRING_ANALYSIS a = nullptr;
    ~Analysis() {
        if (a)
            ScriptStringFree(&a);
    }
};
static bool rtl(const std::wstring &text) {
    for (wchar_t c : text) {
        if ((c >= 0x0600 && c <= 0x08ff) || (c >= 0xfb50 && c <= 0xfdff))
            return true;
        if ((c >= L'A' && c <= L'Z') || (c >= L'a' && c <= L'z'))
            return false;
    }
    return false;
}
// A line made only of separator marks is a rule drawn as pixels, never as a font glyph.
static bool separator(const std::wstring &text) {
    if (text.size() < 3)
        return false;
    for (wchar_t c : text)
        if (c != L'-' && c != L'_' && c != L'=' && c != L'*' && c != 0x2010 &&
            c != 0x2011 && c != 0x2012 && c != 0x2013 && c != 0x2014 && c != 0x2015 &&
            c != 0xfe58 && c != 0xfe63 && c != 0xff0d && c != 0x2500 && c != 0x2501 &&
            c != 0x2550)
            return false;
    return true;
}
static int measure(HDC dc, const std::wstring &text, Analysis &a) {
    if (text.empty())
        return 0;
    DWORD flags = SSA_GLYPHS | SSA_FALLBACK | SSA_LINK | (rtl(text) ? SSA_RTL : 0);
    HRESULT hr = ScriptStringAnalyse(dc, text.data(), static_cast<int>(text.size()),
                                     static_cast<int>(text.size() * 2 + 16), -1, flags, 0, nullptr,
                                     nullptr, nullptr, nullptr, nullptr, &a.a);
    if (FAILED(hr))
        throw Error("ESC_POS_ERROR", "Arabic shaping failed");
    auto size = ScriptString_pSize(a.a);
    if (!size)
        throw Error("ESC_POS_ERROR", "Cannot measure shaped text");
    return size->cx;
}
std::vector<unsigned char> render(const Json &p, const Json &doc) {
    document(doc);
    int width = p["widthDots"], font_size = p["fontSize"], step = font_size * 2;
    const int margin = std::max(2, std::min(24, static_cast<int>(width * 0.02)));
    const int limit = width - margin * 2;
    struct Canvas {
        HDC dc = nullptr;
        HFONT font = nullptr;
        HBITMAP bitmap = nullptr;
        HGDIOBJ old_font = nullptr, old_bitmap = nullptr;
        ~Canvas() {
            if (dc) {
                if (old_font)
                    SelectObject(dc, old_font);
                if (old_bitmap)
                    SelectObject(dc, old_bitmap);
            }
            if (font)
                DeleteObject(font);
            if (bitmap)
                DeleteObject(bitmap);
            if (dc)
                DeleteDC(dc);
        }
    } canvas;
    canvas.dc = CreateCompatibleDC(nullptr);
    canvas.font = CreateFontW(-font_size, 0, 0, 0, FW_NORMAL, FALSE, FALSE, FALSE, DEFAULT_CHARSET,
                              OUT_DEFAULT_PRECIS, CLIP_DEFAULT_PRECIS, ANTIALIASED_QUALITY,
                              DEFAULT_PITCH, L"Noto Sans Arabic");
    if (!canvas.dc || !canvas.font)
        throw Error("ESC_POS_ERROR", "Cannot create text renderer");
    canvas.old_font = SelectObject(canvas.dc, canvas.font);
    enum class Align { left, center, right };
    struct DrawCell {
        std::wstring text;
        Align align;
        int left, right;
    };
    struct DrawRow {
        bool rule = false;
        bool dashed = false;
        std::vector<DrawCell> cells;
    };
    std::vector<DrawRow> rows;
    auto trim = [](std::wstring value) {
        auto whitespace = [](wchar_t c) { return c == L' ' || c == L'\t' || c == L'\r'; };
        while (!value.empty() && whitespace(value.front()))
            value.erase(value.begin());
        while (!value.empty() && whitespace(value.back()))
            value.pop_back();
        return value;
    };
    auto wrap = [&](std::wstring part, int max_width) {
        std::vector<std::wstring> out;
        max_width = std::max(1, max_width);
        if (part.empty()) {
            out.push_back(L"");
            return out;
        }
        while (!part.empty()) {
            Analysis a;
            if (measure(canvas.dc, part, a) <= max_width) {
                out.push_back(part);
                break;
            }
            std::size_t low = 1, high = part.size(), best = 0;
            while (low <= high) {
                auto mid = (low + high) / 2;
                if (mid < part.size() && part[mid] >= 0xdc00 && part[mid] <= 0xdfff)
                    --mid;
                if (mid == 0) {
                    low = 2;
                    continue;
                }
                Analysis test;
                if (measure(canvas.dc, part.substr(0, mid), test) <= max_width) {
                    best = mid;
                    low = mid + 1;
                    if (low < part.size() && part[low] >= 0xdc00 && part[low] <= 0xdfff)
                        ++low;
                } else {
                    high = mid - 1;
                }
            }
            if (!best)
                throw Error("ESC_POS_ERROR", "A glyph exceeds printer width");
            auto space = part.rfind(L' ', best - 1);
            if (space != std::wstring::npos && space > 0)
                best = space;
            out.push_back(part.substr(0, best));
            part.erase(0, best);
            while (!part.empty() && part.front() == L' ')
                part.erase(part.begin());
        }
        return out;
    };
    auto append_segment = [&](std::wstring text, bool centered) {
        text = trim(std::move(text));
        if (text.empty()) {
            rows.push_back({});
            return;
        }
        if (text == L"[dashed]") {
            rows.push_back({true, true, {}});
            return;
        }
        if (separator(text)) {
            rows.push_back({true, false, {}});
            return;
        }
        if (!centered) {
            std::vector<std::wstring> cells;
            std::size_t start = 0;
            for (;;) {
                auto delimiter = text.find(L'|', start);
                cells.push_back(trim(text.substr(start, delimiter == std::wstring::npos
                                                           ? std::wstring::npos
                                                           : delimiter - start)));
                if (delimiter == std::wstring::npos)
                    break;
                start = delimiter + 1;
            }
            if (cells.size() >= 2 && cells.size() <= 4) {
                static const int weights[][4] = {{3, 2, 0, 0}, {45, 15, 40, 0},
                                                  {34, 12, 27, 27}};
                const int *row_weights = weights[cells.size() - 2];
                const int gutter = std::max(4, font_size / 3);
                const int usable = std::max(static_cast<int>(cells.size()), limit -
                    gutter * static_cast<int>(cells.size() - 1));
                int right = width - margin;
                std::vector<std::vector<std::wstring>> wrapped_cells;
                std::vector<std::pair<int, int>> bounds;
                std::size_t row_count = 1;
                const int sum_weights = cells.size() == 2 ? 5 : 100;
                for (std::size_t i = 0; i < cells.size(); ++i) {
                    const int cell_width = std::max(1, usable * row_weights[i] / sum_weights);
                    const int left = right - cell_width;
                    auto parts = wrap(cells[i], cell_width);
                    row_count = std::max(row_count, parts.size());
                    wrapped_cells.push_back(std::move(parts));
                    bounds.emplace_back(left, right);
                    right = left - gutter;
                }
                for (std::size_t row_index = 0; row_index < row_count; ++row_index) {
                    DrawRow row;
                    for (std::size_t i = 0; i < cells.size(); ++i) {
                        const auto &parts = wrapped_cells[i];
                        row.cells.push_back({row_index < parts.size() ? parts[row_index] : L"",
                                             i == 0 ? Align::right :
                                                 i + 1 == cells.size() ? Align::left : Align::center,
                                             bounds[i].first, bounds[i].second});
                    }
                    rows.push_back(std::move(row));
                }
                return;
            }
        }
        const auto align = centered ? Align::center : (rtl(text) ? Align::right : Align::left);
        for (const auto &part : wrap(text, limit))
            rows.push_back({false, false, {{part, align, margin, width - margin}}});
    };
    for (const auto &logical : printable_lines(doc, static_cast<unsigned>(width))) {
        auto text = wide(logical);
        bool centered = false;
        if (text.rfind(L"[center]", 0) == 0) {
            centered = true;
            text = trim(text.substr(8));
            if (!text.empty() && text.front() == L':')
                text = trim(text.substr(1));
        }
        std::size_t start = 0;
        do {
            const auto end = text.find(L'\n', start);
            append_segment(text.substr(start, end == std::wstring::npos ? std::wstring::npos
                                                                          : end - start),
                           centered);
            if (end == std::wstring::npos)
                break;
            start = end + 1;
        } while (start <= text.size());
        if (rows.size() * static_cast<std::size_t>(step) > 4096)
            throw Error("INVALID_JOB", "Receipt too tall; split into stable part IDs");
    }
    int height = std::max(step, static_cast<int>(rows.size()) * step);
    if (height > 4096)
        throw Error("INVALID_JOB", "Receipt too tall");
    BITMAPINFO info{};
    info.bmiHeader.biSize = sizeof(BITMAPINFOHEADER);
    info.bmiHeader.biWidth = width;
    info.bmiHeader.biHeight = -height;
    info.bmiHeader.biPlanes = 1;
    info.bmiHeader.biBitCount = 32;
    info.bmiHeader.biCompression = BI_RGB;
    void *memory = nullptr;
    canvas.bitmap = CreateDIBSection(canvas.dc, &info, DIB_RGB_COLORS, &memory, nullptr, 0);
    if (!canvas.bitmap || !memory)
        throw Error("ESC_POS_ERROR", "Cannot create raster bitmap");
    canvas.old_bitmap = SelectObject(canvas.dc, canvas.bitmap);
    std::memset(memory, 255, static_cast<std::size_t>(width) * height * 4);
    SetBkMode(canvas.dc, TRANSPARENT);
    SetTextColor(canvas.dc, RGB(0, 0, 0));
    int y = 0;
    for (const auto &row : rows) {
        if (row.rule) {
            auto brush = static_cast<HBRUSH>(GetStockObject(BLACK_BRUSH));
            const int top = y + step / 2;
            if (row.dashed) {
                for (int x = margin; x < width - margin; x += 13) {
                    RECT dash{x, top, std::min(x + 8, width - margin), top + 2};
                    FillRect(canvas.dc, &dash, brush);
                }
            } else {
                RECT rule{margin, top, width - margin, top + 2};
                FillRect(canvas.dc, &rule, brush);
            }
        } else {
            for (const auto &cell : row.cells) {
                if (cell.text.empty())
                    continue;
                Analysis a;
                int pixels = measure(canvas.dc, cell.text, a);
                int x = cell.left;
                if (cell.align == Align::right)
                    x = std::max(cell.left, cell.right - pixels);
                else if (cell.align == Align::center)
                    x = cell.left + std::max(0, (cell.right - cell.left - pixels) / 2);
                if (FAILED(ScriptStringOut(a.a, x, y, 0, nullptr, 0, 0, FALSE)))
                    throw Error("ESC_POS_ERROR", "Cannot draw shaped text");
            }
        }
        y += step;
    }
    GdiFlush();
    std::vector<unsigned char> bits(static_cast<std::size_t>(width / 8) * height);
    auto pixels = static_cast<unsigned char *>(memory);
    for (int yy = 0; yy < height; ++yy)
        for (int x = 0; x < width; ++x) {
            auto i = (static_cast<std::size_t>(yy) * width + x) * 4;
            if ((pixels[i] + pixels[i + 1] + pixels[i + 2]) / 3 < 160)
                bits[static_cast<std::size_t>(yy) * (width / 8) + x / 8] |=
                    static_cast<unsigned char>(0x80 >> (x % 8));
        }
    return raster(width, height, bits, p["cut"]);
}
// Direct LAN TCP/9100 transport for RFC1918 unicast IPv4, mirroring the modern Agent's network
// rules. Only ws2_32 APIs are used, so no newer OS import is introduced.
static void network_endpoint(const Json &profile, IN_ADDR &addr, u_short &port) {
    const auto &c = profile.at("connection");
    auto host = c.at("host").get<std::string>();
    port = static_cast<u_short>(c.at("port").get<int>());
    IN_ADDR parsed{};
    if (InetPtonA(AF_INET, host.c_str(), &parsed) != 1)
        throw Error("INVALID_CONFIG", "Use a literal private IPv4 address", false);
    auto *o = reinterpret_cast<unsigned char *>(&parsed);
    bool private_range = o[0] == 10 || (o[0] == 172 && o[1] >= 16 && o[1] <= 31) ||
                         (o[0] == 192 && o[1] == 168);
    if (!private_range || port == 0 || o[3] == 0 || o[3] == 255)
        throw Error("INVALID_CONFIG", "Only private LAN unicast IPv4 addresses are supported",
                    false);
    addr = parsed;
}
static void ensure_winsock() {
    static std::once_flag once;
    std::call_once(once, [] {
        WSADATA data;
        WSAStartup(MAKEWORD(2, 2), &data);
    });
}
static SOCKET lan_connect(const IN_ADDR &addr, u_short port) {
    SOCKET s = socket(AF_INET, SOCK_STREAM, IPPROTO_TCP);
    if (s == INVALID_SOCKET)
        throw Error("NETWORK_CONNECTION_FAILED", "Cannot create LAN socket", true);
    sockaddr_in target{};
    target.sin_family = AF_INET;
    target.sin_port = htons(port);
    target.sin_addr = addr;
    u_long nonblocking = 1;
    ioctlsocket(s, FIONBIO, &nonblocking);
    int result = connect(s, reinterpret_cast<sockaddr *>(&target), sizeof(target));
    if (result == SOCKET_ERROR) {
        int err = WSAGetLastError();
        if (err != WSAEWOULDBLOCK && err != WSAEINPROGRESS) {
            closesocket(s);
            throw Error("NETWORK_CONNECTION_FAILED", "Cannot reach the LAN printer", true);
        }
        fd_set writable;
        FD_ZERO(&writable);
        FD_SET(s, &writable);
        timeval timeout{3, 0};
        int ready = select(0, nullptr, &writable, nullptr, &timeout);
        if (ready <= 0) {
            closesocket(s);
            throw Error(ready == 0 ? "NETWORK_TIMEOUT" : "NETWORK_CONNECTION_FAILED",
                        ready == 0 ? "LAN printer did not accept the connection in time"
                                   : "Cannot reach the LAN printer",
                        true);
        }
        int so_error = 0;
        int len = sizeof(so_error);
        getsockopt(s, SOL_SOCKET, SO_ERROR, reinterpret_cast<char *>(&so_error), &len);
        if (so_error != 0) {
            closesocket(s);
            throw Error("NETWORK_CONNECTION_FAILED", "Cannot reach the LAN printer", true);
        }
    }
    u_long blocking = 0;
    ioctlsocket(s, FIONBIO, &blocking);
    return s;
}
void send_network(const Json &profile, const std::vector<unsigned char> &bytes) {
    ensure_winsock();
    IN_ADDR addr{};
    u_short port = 0;
    network_endpoint(profile, addr, port);
    SOCKET s = lan_connect(addr, port);
    DWORD send_timeout = 10000;
    setsockopt(s, SOL_SOCKET, SO_SNDTIMEO, reinterpret_cast<const char *>(&send_timeout),
               sizeof(send_timeout));
    std::size_t offset = 0;
    while (offset < bytes.size()) {
        int chunk = static_cast<int>(std::min<std::size_t>(16384, bytes.size() - offset));
        int written = send(s, reinterpret_cast<const char *>(bytes.data()) + offset, chunk, 0);
        if (written == SOCKET_ERROR)
            throw Error("PRINT_OUTCOME_UNKNOWN",
                        "Some bytes may have reached the LAN printer. Inspect the paper before "
                        "reprinting.",
                        false, true);
        offset += static_cast<std::size_t>(written);
    }
    shutdown(s, SD_SEND);
    closesocket(s);
}
void probe_network(const Json &profile) {
    ensure_winsock();
    IN_ADDR addr{};
    u_short port = 0;
    network_endpoint(profile, addr, port);
    SOCKET s = lan_connect(addr, port);
    closesocket(s);
}
// Passive LAN scan for RAW printers, mirroring the modern Agent's discover.network. Only a TCP
// connect is attempted (nothing is sent) and a hit is a candidate until a test print confirms it.
static bool probe_host(const IN_ADDR &addr, u_short port, long timeout_ms) {
    SOCKET s = socket(AF_INET, SOCK_STREAM, IPPROTO_TCP);
    if (s == INVALID_SOCKET)
        return false;
    sockaddr_in target{};
    target.sin_family = AF_INET;
    target.sin_port = htons(port);
    target.sin_addr = addr;
    u_long nonblocking = 1;
    ioctlsocket(s, FIONBIO, &nonblocking);
    bool ok = false;
    if (connect(s, reinterpret_cast<sockaddr *>(&target), sizeof(target)) == 0) {
        ok = true;
    } else {
        int err = WSAGetLastError();
        if (err == WSAEWOULDBLOCK || err == WSAEINPROGRESS) {
            // Windows reports a refused/failed non-blocking connect in the except set; watching
            // it lets a closed port answer immediately instead of waiting for the timeout.
            fd_set writable, failed;
            FD_ZERO(&writable);
            FD_ZERO(&failed);
            FD_SET(s, &writable);
            FD_SET(s, &failed);
            timeval timeout;
            timeout.tv_sec = timeout_ms / 1000;
            timeout.tv_usec = (timeout_ms % 1000) * 1000;
            if (select(0, nullptr, &writable, &failed, &timeout) > 0 && FD_ISSET(s, &writable)) {
                int so_error = 0;
                int len = sizeof(so_error);
                getsockopt(s, SOL_SOCKET, SO_ERROR, reinterpret_cast<char *>(&so_error), &len);
                ok = (so_error == 0);
            }
        }
    }
    closesocket(s);
    return ok;
}
std::vector<Ipv4Interface> local_interfaces() {
    // GetAdaptersInfo (Windows 2000+) lists every adapter, so an offline cafe LAN without a
    // default gateway or Internet access is still found (the old UDP-to-8.8.8.8 trick was not).
    ULONG size = 0;
    if (GetAdaptersInfo(nullptr, &size) != ERROR_BUFFER_OVERFLOW || size == 0 || size > (1u << 20))
        return {};
    std::vector<unsigned char> buffer(size);
    auto *adapters = reinterpret_cast<IP_ADAPTER_INFO *>(buffer.data());
    if (GetAdaptersInfo(adapters, &size) != NO_ERROR)
        return {};
    std::vector<Ipv4Interface> out;
    for (auto *adapter = adapters; adapter && out.size() < 16; adapter = adapter->Next)
        for (auto *ip = &adapter->IpAddressList; ip && out.size() < 16; ip = ip->Next) {
            std::uint32_t address = 0, mask = 0;
            if (parse_ipv4(ip->IpAddress.String, address) && parse_ipv4(ip->IpMask.String, mask) &&
                private_lan_host(address))
                out.push_back({address, mask});
        }
    return out;
}
std::vector<std::uint32_t> probe_raw_printers(const std::vector<std::uint32_t> &targets,
                                              std::uint16_t port, const std::atomic<bool> *cancel) {
    ensure_winsock();
    std::atomic<std::size_t> next(0);
    std::mutex guard;
    std::vector<std::uint32_t> found;
    auto worker = [&]() {
        for (;;) {
            if (cancel && cancel->load())
                break;
            const std::size_t index = next.fetch_add(1);
            if (index >= targets.size())
                break;
            // Defence in depth: never contact anything the print transport would refuse.
            if (!private_lan_host(targets[index]))
                continue;
            IN_ADDR hit{};
            hit.s_addr = htonl(targets[index]);
            if (probe_host(hit, port, 450)) {
                std::lock_guard<std::mutex> lock(guard);
                found.push_back(targets[index]);
            }
        }
    };
    const std::size_t workers = std::min<std::size_t>(48, targets.size());
    std::vector<std::thread> pool;
    for (std::size_t i = 0; i < workers; ++i) {
        try {
            pool.emplace_back(worker);
        } catch (...) {
            break; // fewer threads only make the scan slower
        }
    }
    if (pool.empty())
        worker();
    for (auto &t : pool)
        t.join();
    std::sort(found.begin(), found.end());
    return found;
}
Json discover_network() {
    auto interfaces = local_interfaces();
    std::vector<std::uint32_t> targets;
    if (!interfaces.empty()) {
        targets = scan_targets(interfaces, {});
    } else {
        // No private adapter found: fall back to the most common home/cafe router subnets.
        for (std::uint32_t base : {0xC0A80100u, 0xC0A80000u})
            for (std::uint32_t host = 1; host < 255; ++host)
                targets.push_back(base | host);
    }
    Json found = Json::array();
    for (auto address : probe_raw_printers(targets, 9100))
        found.push_back({{"host", format_ipv4(address)}, {"port", 9100}});
    return found;
}
// ---- Installed Windows queues and USB printers ----
std::vector<QueueInfo> installed_queues() {
    std::vector<QueueInfo> out;
    DWORD needed = 0, count = 0;
    EnumPrintersW(PRINTER_ENUM_LOCAL, nullptr, 2, nullptr, 0, &needed, &count);
    if (needed == 0) {
        if (GetLastError() != ERROR_SUCCESS && GetLastError() != ERROR_INSUFFICIENT_BUFFER)
            throw Error("SPOOLER_UNAVAILABLE", "Windows printer enumeration failed", true);
    } else {
        if (needed > 8 * 1024 * 1024)
            throw Error("SPOOLER_UNAVAILABLE", "Printer list is too large");
        std::vector<BYTE> b(needed);
        if (!EnumPrintersW(PRINTER_ENUM_LOCAL, nullptr, 2, b.data(), needed, &needed, &count))
            throw Error("SPOOLER_UNAVAILABLE", "Cannot list Windows printers", true);
        auto *p = reinterpret_cast<PRINTER_INFO_2W *>(b.data());
        for (DWORD i = 0; i < count && out.size() < 128; ++i) {
            if (!p[i].pPrinterName)
                continue;
            QueueInfo q;
            q.name = utf8(p[i].pPrinterName);
            q.port = p[i].pPortName ? utf8(p[i].pPortName) : std::string();
            q.driver = p[i].pDriverName ? utf8(p[i].pDriverName) : std::string();
            q.offline = (p[i].Attributes & PRINTER_ATTRIBUTE_WORK_OFFLINE) != 0 ||
                        (p[i].Status & PRINTER_STATUS_OFFLINE) != 0;
            q.kind = queue_kind(q.name, q.port, q.driver);
            q.ipv4 = port_ipv4(q.port);
            out.push_back(q);
        }
    }
    // Connections to shared printers: level 4 reads the local registry only.
    needed = count = 0;
    EnumPrintersW(PRINTER_ENUM_CONNECTIONS, nullptr, 4, nullptr, 0, &needed, &count);
    if (needed > 0 && needed <= 4 * 1024 * 1024) {
        std::vector<BYTE> b(needed);
        if (EnumPrintersW(PRINTER_ENUM_CONNECTIONS, nullptr, 4, b.data(), needed, &needed,
                          &count)) {
            auto *p = reinterpret_cast<PRINTER_INFO_4W *>(b.data());
            for (DWORD i = 0; i < count && out.size() < 128; ++i)
                if (p[i].pPrinterName) {
                    QueueInfo q;
                    q.name = utf8(p[i].pPrinterName);
                    q.kind = "shared";
                    out.push_back(q);
                }
        }
    }
    return out;
}
// GUID_DEVINTERFACE_USBPRINT (usbprint.h, WDK): registered by the in-box usbprint.sys for every
// USB Printer Class (0x07) device — no vendor driver or Windows print queue is required.
static const GUID usbprint_interface = {
    0x28d78fad, 0x5a12, 0x11d1, {0xae, 0x5b, 0x00, 0x00, 0xf8, 0x03, 0xa8, 0xc2}};
const GUID &usb_printer_interface() { return usbprint_interface; }
#ifndef SPDRP_LOCATION_PATHS
#define SPDRP_LOCATION_PATHS 0x00000023
#endif
static std::string clean_text(const wchar_t *text) {
    std::string out;
    try {
        out = utf8(text);
    } catch (...) {
        return {};
    }
    std::string printable;
    for (unsigned char c : out)
        if (c >= 0x20 && c != 0x7f)
            printable.push_back(static_cast<char>(c));
    while (!printable.empty() && printable.back() == ' ')
        printable.pop_back();
    return printable.size() > 120 ? printable.substr(0, 120) : printable;
}
static std::string device_name(HDEVINFO info, SP_DEVINFO_DATA &device) {
    wchar_t buffer[256] = {};
    // DEVPKEY_Device_BusReportedDeviceDesc (Windows 7+): the product string the printer itself
    // reports, e.g. "Printer POS-80"; the class description is only "USB Printing Support".
    static const DEVPROPKEY bus_reported = {
        {0x540b947e, 0x8b40, 0x45bc, {0xa8, 0xa2, 0x6a, 0x0b, 0x89, 0x4c, 0xbd, 0xa2}}, 4};
    DEVPROPTYPE type = 0;
    if (SetupDiGetDevicePropertyW(info, &device, &bus_reported, &type,
                                  reinterpret_cast<PBYTE>(buffer),
                                  sizeof(buffer) - sizeof(wchar_t), nullptr, 0) &&
        type == DEVPROP_TYPE_STRING && buffer[0]) {
        auto name = clean_text(buffer);
        if (!name.empty())
            return name;
    }
    DWORD reg_type = 0;
    std::fill(std::begin(buffer), std::end(buffer), L'\0');
    if (SetupDiGetDeviceRegistryPropertyW(info, &device, SPDRP_FRIENDLYNAME, &reg_type,
                                          reinterpret_cast<PBYTE>(buffer),
                                          sizeof(buffer) - sizeof(wchar_t), nullptr) &&
        reg_type == REG_SZ && buffer[0])
        return clean_text(buffer);
    return {};
}
static std::string location_path(HDEVINFO info, SP_DEVINFO_DATA &device) {
    wchar_t buffer[512] = {};
    DWORD type = 0;
    if (SetupDiGetDeviceRegistryPropertyW(info, &device, SPDRP_LOCATION_PATHS, &type,
                                          reinterpret_cast<PBYTE>(buffer),
                                          sizeof(buffer) - 2 * sizeof(wchar_t), nullptr) &&
        type == REG_MULTI_SZ)
        return clean_text(buffer); // the first (PCIROOT-based) path
    return {};
}
static std::string monitor_port(HDEVINFO info, SP_DEVICE_INTERFACE_DATA &iface) {
    // usbprint.sys stores the Windows USB monitor port number (USB001, ...) under the interface.
    HKEY key = SetupDiOpenDeviceInterfaceRegKey(info, &iface, 0, KEY_READ);
    if (key == INVALID_HANDLE_VALUE)
        return {};
    std::string out;
    DWORD number = 0, size = sizeof(number), type = 0;
    if (RegQueryValueExW(key, L"Port Number", nullptr, &type, reinterpret_cast<LPBYTE>(&number),
                         &size) == ERROR_SUCCESS &&
        type == REG_DWORD && number > 0 && number < 1000) {
        wchar_t base[32] = {};
        DWORD base_size = sizeof(base) - sizeof(wchar_t), base_type = 0;
        std::wstring name = L"USB";
        if (RegQueryValueExW(key, L"Base Name", nullptr, &base_type, reinterpret_cast<LPBYTE>(base),
                             &base_size) == ERROR_SUCCESS &&
            base_type == REG_SZ && base[0])
            name = base;
        std::wstring digits = std::to_wstring(number);
        while (digits.size() < 3)
            digits = L"0" + digits;
        out = clean_text((name + digits).c_str());
    }
    RegCloseKey(key);
    return out;
}
std::vector<UsbPrinter> usb_printers() {
    HDEVINFO info = SetupDiGetClassDevsW(&usbprint_interface, nullptr, nullptr,
                                         DIGCF_PRESENT | DIGCF_DEVICEINTERFACE);
    if (info == INVALID_HANDLE_VALUE)
        throw Error("USB_DISCOVERY_FAILED", "Windows could not list USB printers", true);
    struct Guard {
        HDEVINFO info;
        ~Guard() { SetupDiDestroyDeviceInfoList(info); }
    } guard{info};
    std::vector<UsbPrinter> out;
    for (DWORD index = 0; index < 64; ++index) {
        SP_DEVICE_INTERFACE_DATA iface{};
        iface.cbSize = sizeof(iface);
        if (!SetupDiEnumDeviceInterfaces(info, nullptr, &usbprint_interface, index, &iface))
            break; // ERROR_NO_MORE_ITEMS
        DWORD required = 0;
        SetupDiGetDeviceInterfaceDetailW(info, &iface, nullptr, 0, &required, nullptr);
        if (required < sizeof(SP_DEVICE_INTERFACE_DETAIL_DATA_W) || required > 64 * 1024)
            continue;
        std::vector<unsigned char> buffer(required + sizeof(wchar_t));
        auto *detail = reinterpret_cast<SP_DEVICE_INTERFACE_DETAIL_DATA_W *>(buffer.data());
        detail->cbSize = sizeof(SP_DEVICE_INTERFACE_DETAIL_DATA_W);
        SP_DEVINFO_DATA device{};
        device.cbSize = sizeof(device);
        if (!SetupDiGetDeviceInterfaceDetailW(info, &iface, detail, required, nullptr, &device))
            continue;
        UsbPrinter printer;
        printer.path = detail->DevicePath;
        wchar_t instance[512] = {};
        if (SetupDiGetDeviceInstanceIdW(info, &device, instance, 511, nullptr))
            printer.instance = clean_text(instance);
        printer.device.id = parse_usb_instance(printer.instance);
        if (!printer.device.id.ok)
            continue;
        printer.device.ports = usb_port_chain(location_path(info, device));
        printer.name = device_name(info, device);
        printer.port = monitor_port(info, iface);
        out.push_back(std::move(printer));
    }
    return out;
}
static const UsbPrinter &locate_usb(const std::vector<UsbPrinter> &printers, const Json &profile) {
    std::vector<UsbCandidate> present;
    for (const auto &printer : printers)
        present.push_back(printer.device);
    return printers[select_usb_device(profile.at("connection"), present)];
}
void probe_usb(const Json &profile) {
    const auto printers = usb_printers();
    const auto &printer = locate_usb(printers, profile);
    Handle device;
    device.h = CreateFileW(printer.path.c_str(), GENERIC_WRITE, FILE_SHARE_READ | FILE_SHARE_WRITE,
                           nullptr, OPEN_EXISTING, FILE_ATTRIBUTE_NORMAL, nullptr);
    if (device.h == INVALID_HANDLE_VALUE) {
        const DWORD error = GetLastError();
        if (error == ERROR_ACCESS_DENIED || error == ERROR_SHARING_VIOLATION || error == ERROR_BUSY)
            throw Error("USB_BUSY",
                        "The USB printer is in use by another program or Windows denied access",
                        true);
        throw Error("PRINTER_OFFLINE", "The USB printer is connected but not ready", true);
    }
}
// Blocking driver calls are isolated in a disposable process. Killing it never permits automatic
// replay.
int spool_child(const std::wstring &file) {
    HANDLE printer = nullptr;
    bool started = false;
    try {
        auto j = Json::parse(read_file(file));
        auto name = wide(j.at("queueName"));
        auto bytes = unbase64(j.at("data"));
        PRINTER_DEFAULTSW defaults{};
        defaults.DesiredAccess = PRINTER_ACCESS_USE;
        if (!OpenPrinterW(&name[0], &printer, &defaults))
            return 10;
        DOC_INFO_1W doc{};
        doc.pDocName = const_cast<LPWSTR>(L"MenuVex receipt");
        doc.pDatatype = const_cast<LPWSTR>(L"RAW");
        if (!StartDocPrinterW(printer, 1, reinterpret_cast<LPBYTE>(&doc))) {
            ClosePrinter(printer);
            return 20;
        }
        started = true;
        if (!StartPagePrinter(printer))
            throw unknown();
        for (std::size_t offset = 0; offset < bytes.size();) {
            DWORD n = static_cast<DWORD>(std::min<std::size_t>(16384, bytes.size() - offset)),
                  written = 0;
            if (!WritePrinter(printer, bytes.data() + offset, n, &written) || written != n)
                throw unknown();
            offset += written;
        }
        if (!EndPagePrinter(printer) || !EndDocPrinter(printer))
            throw unknown();
        ClosePrinter(printer);
        return 0;
    } catch (...) {
        if (printer) {
            if (started)
                AbortPrinter(printer);
            ClosePrinter(printer);
        }
        return 20;
    }
}
void send_spooler(const std::wstring &dir, const Json &p, const std::vector<unsigned char> &bytes) {
    auto random = random_base64();
    random.erase(std::remove_if(random.begin(), random.end(),
                                [](char c) { return c == '/' || c == '+' || c == '='; }),
                 random.end());
    auto file = dir + L"\\payload-" + wide(random) + L".json";
    struct Cleanup {
        std::wstring path;
        ~Cleanup() { DeleteFileW(path.c_str()); }
    } cleanup{file};
    write_file(file,
               Json({{"queueName", p["connection"]["queueName"]}, {"data", base64(bytes)}}).dump());
    auto command = L"\"" + executable() + L"\" --spool-child \"" + file + L"\"";
    std::vector<wchar_t> mutable_command(command.begin(), command.end());
    mutable_command.push_back(0);
    STARTUPINFOW startup{};
    startup.cb = sizeof(startup);
    PROCESS_INFORMATION pi{};
    if (!CreateProcessW(executable().c_str(), mutable_command.data(), nullptr, nullptr, FALSE,
                        CREATE_NO_WINDOW, nullptr, dir.c_str(), &startup, &pi))
        throw Error("SPOOLER_UNAVAILABLE", "Cannot start isolated print worker", true);
    Handle process{pi.hProcess}, thread{pi.hThread};
    DWORD waited = WaitForSingleObject(process, 30000);
    if (waited != WAIT_OBJECT_0) {
        TerminateProcess(process, 20);
        WaitForSingleObject(process, 5000);
        throw unknown();
    }
    DWORD code = 20;
    if (!GetExitCodeProcess(process, &code))
        throw unknown();
    if (code == 10)
        throw Error("PRINTER_OFFLINE", "Cannot open Windows printer before submission", true);
    if (code != 0)
        throw unknown();
}
// Direct USB: the parent resolves the device (VID/PID, serial, port chain) and a disposable child
// opens it and writes, so a printer that stops accepting data (paper out, cover open) can never
// block the queue worker. Same outcome rules as the spooler child: failure to open is certain
// (nothing sent, retryable); a timeout, partial write or crash after opening is uncertain.
int usb_child(const std::wstring &file) {
    std::wstring path;
    std::vector<unsigned char> bytes;
    try {
        auto j = Json::parse(read_file(file));
        path = wide(j.at("devicePath").get<std::string>());
        bytes = unbase64(j.at("data"));
    } catch (...) {
        return 10;
    }
    // Only a USB device interface may ever be opened here, never an arbitrary file.
    if (path.size() < 8 || _wcsnicmp(path.c_str(), L"\\\\?\\usb#", 8) != 0 || bytes.empty())
        return 10;
    Handle device;
    device.h = CreateFileW(path.c_str(), GENERIC_WRITE, FILE_SHARE_READ | FILE_SHARE_WRITE, nullptr,
                           OPEN_EXISTING, FILE_ATTRIBUTE_NORMAL, nullptr);
    if (device.h == INVALID_HANDLE_VALUE)
        return 10;
    for (std::size_t offset = 0; offset < bytes.size();) {
        DWORD n = static_cast<DWORD>(std::min<std::size_t>(16384, bytes.size() - offset)),
              written = 0;
        if (!WriteFile(device, bytes.data() + offset, n, &written, nullptr) || written == 0)
            return 20;
        offset += written;
    }
    return 0;
}
void send_usb(const std::wstring &dir, const Json &p, const std::vector<unsigned char> &bytes) {
    const auto printers = usb_printers();
    const auto &printer = locate_usb(printers, p);
    auto random = random_base64();
    random.erase(std::remove_if(random.begin(), random.end(),
                                [](char c) { return c == '/' || c == '+' || c == '='; }),
                 random.end());
    auto file = dir + L"\\usb-payload-" + wide(random) + L".json";
    struct Cleanup {
        std::wstring path;
        ~Cleanup() { DeleteFileW(path.c_str()); }
    } cleanup{file};
    write_file(file, Json({{"devicePath", utf8(printer.path)}, {"data", base64(bytes)}}).dump());
    auto command = L"\"" + executable() + L"\" --usb-child \"" + file + L"\"";
    std::vector<wchar_t> mutable_command(command.begin(), command.end());
    mutable_command.push_back(0);
    STARTUPINFOW startup{};
    startup.cb = sizeof(startup);
    PROCESS_INFORMATION pi{};
    if (!CreateProcessW(executable().c_str(), mutable_command.data(), nullptr, nullptr, FALSE,
                        CREATE_NO_WINDOW, nullptr, dir.c_str(), &startup, &pi))
        throw Error("USB_DEVICE_ERROR", "Cannot start isolated USB print worker", true);
    Handle process{pi.hProcess}, thread{pi.hThread};
    DWORD waited = WaitForSingleObject(process, 30000);
    if (waited != WAIT_OBJECT_0) {
        TerminateProcess(process, 20);
        WaitForSingleObject(process, 5000);
        throw unknown();
    }
    DWORD code = 20;
    if (!GetExitCodeProcess(process, &code))
        throw unknown();
    if (code == 10)
        throw Error("PRINTER_OFFLINE",
                    "Cannot open the USB printer before submission (unplugged, off or in use)",
                    true);
    if (code != 0)
        throw unknown();
}
void log_code(const std::wstring &dir, const std::string &code) {
    static std::mutex mutex;
    std::lock_guard<std::mutex> lock(mutex);
    auto file = dir + L"\\agent.log";
    WIN32_FILE_ATTRIBUTE_DATA info{};
    if (GetFileAttributesExW(file.c_str(), GetFileExInfoStandard, &info) &&
        (info.nFileSizeHigh || info.nFileSizeLow > 1024 * 1024)) {
        DeleteFileW((dir + L"\\agent.previous.log").c_str());
        MoveFileW(file.c_str(), (dir + L"\\agent.previous.log").c_str());
    }
    Handle h;
    h.h = CreateFileW(file.c_str(), FILE_APPEND_DATA, FILE_SHARE_READ, nullptr, OPEN_ALWAYS,
                      FILE_ATTRIBUTE_NORMAL, nullptr);
    if (h.h != INVALID_HANDLE_VALUE) {
        auto line = std::to_string(now()) + " " + code + "\r\n";
        DWORD n;
        WriteFile(h, line.data(), static_cast<DWORD>(line.size()), &n, nullptr);
    }
}
} // namespace mv
