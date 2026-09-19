#include "platform.hpp"
#include <algorithm>
#include <bcrypt.h>
#include <cstring>
#include <fstream>
#include <sddl.h>
#include <shlobj.h>
#include <usp10.h>
#include <wincred.h>
#include <wincrypt.h>
#include <winspool.h>
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
    std::vector<std::wstring> wrapped;
    for (const auto &logical : lines(doc)) {
        auto text = wide(logical);
        std::size_t start = 0;
        do {
            auto end = text.find(L'\n', start);
            if (end == std::wstring::npos)
                end = text.size();
            auto part = text.substr(start, end - start);
            if (part.empty())
                wrapped.push_back(L"");
            while (!part.empty()) {
                Analysis a;
                int pixels = measure(canvas.dc, part, a);
                if (pixels <= width) {
                    wrapped.push_back(part);
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
                    int size = measure(canvas.dc, part.substr(0, mid), test);
                    if (size <= width) {
                        best = mid;
                        low = mid + 1;
                        if (low < part.size() && part[low] >= 0xdc00 && part[low] <= 0xdfff)
                            ++low;
                    } else
                        high = mid - 1;
                }
                if (!best)
                    throw Error("ESC_POS_ERROR", "A glyph exceeds printer width");
                auto space = part.rfind(L' ', best - 1);
                if (space != std::wstring::npos && space > 0)
                    best = space;
                wrapped.push_back(part.substr(0, best));
                part.erase(0, best);
                while (!part.empty() && part.front() == L' ')
                    part.erase(part.begin());
                if (wrapped.size() * static_cast<std::size_t>(step) > 4096)
                    throw Error("INVALID_JOB", "Receipt too tall; split into stable part IDs");
            }
            if (end == text.size())
                break;
            start = end + 1;
        } while (start <= text.size());
    }
    int height = std::max(step, static_cast<int>(wrapped.size()) * step);
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
    for (const auto &text : wrapped) {
        if (!text.empty()) {
            Analysis a;
            int pixels = measure(canvas.dc, text, a);
            if (FAILED(ScriptStringOut(a.a, rtl(text) ? std::max(0, width - pixels) : 0, y, 0,
                                       nullptr, 0, 0, FALSE)))
                throw Error("ESC_POS_ERROR", "Cannot draw shaped text");
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
