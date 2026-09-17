#pragma once
#ifndef _WIN32_WINNT
#define _WIN32_WINNT 0x0601
#endif
#ifndef NOMINMAX
#define NOMINMAX
#endif
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
// Winsock 2 must precede windows.h (which otherwise pulls in incompatible Winsock 1).
#include <winsock2.h>
#include <windows.h>
#include "core.hpp"
namespace mv {
std::wstring wide(const std::string &s);
std::string utf8(const std::wstring &s);
std::wstring data_directory();
std::wstring executable();
std::string read_file(const std::wstring &path, std::size_t max = 2 * 1024 * 1024);
void write_file(const std::wstring &path, const std::string &data);
std::string random_base64();
std::string base64(const std::vector<unsigned char> &data);
std::vector<unsigned char> unbase64(const std::string &data);
class Secret {
    std::mutex mutex_;
    std::vector<unsigned char> key_;

  public:
    Secret();
    ~Secret();
#ifdef MENUVEX_TESTING
    explicit Secret(std::vector<unsigned char> key) : key_(std::move(key)) {}
#endif
    std::string reveal();
    void rotate();
    std::string proof(const std::string &nonce, const std::string &origin, bool server);
    bool verify(const std::string &nonce, const std::string &origin, const std::string &proof);
};
void autostart(bool enabled);
std::vector<std::string> installed_printers();
void load_font();
std::vector<unsigned char> render(const Json &profile, const Json &doc);
void send_spooler(const std::wstring &directory, const Json &profile,
                  const std::vector<unsigned char> &bytes);
int spool_child(const std::wstring &file);
void log_code(const std::wstring &directory, const std::string &code);
} // namespace mv
