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
#include <atomic>
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
void send_network(const Json &profile, const std::vector<unsigned char> &bytes);
void probe_network(const Json &profile);
Json discover_network();
int spool_child(const std::wstring &file);
// ---- Automatic printer discovery (no PowerShell/CMD, no driver installation) ----
struct QueueInfo {
    std::string name, port, driver, kind, ipv4;
    bool offline = false;
};
// Installed Windows queues with port/driver classification (see queue_kind). Local queues come
// from EnumPrinters level 2; shared \\server\printer connections are listed by name only so the
// remote server is never contacted.
std::vector<QueueInfo> installed_queues();
struct UsbPrinter {
    std::wstring path;    // device interface path, opened only when printing/probing
    std::string instance; // PnP device instance ID
    std::string name;     // product string reported by the device (may be empty)
    std::string port;     // Windows USB monitor port such as USB001 (may be empty)
    UsbCandidate device;
};
// USB printers bound to the in-box Windows "USB Printing Support" driver (device interface
// GUID_DEVINTERFACE_USBPRINT). Reads Plug and Play properties only; never opens a device.
std::vector<UsbPrinter> usb_printers();
// Interface class for WM_DEVICECHANGE notifications when a USB printer is (un)plugged.
const GUID &usb_printer_interface();
// Private IPv4 interfaces of this PC on every adapter (no Internet/default route needed).
std::vector<Ipv4Interface> local_interfaces();
// Connect-only TCP probe of each target (passive: nothing is ever sent). Returns the addresses
// that accepted a connection, sorted. `cancel` stops the scan early.
std::vector<std::uint32_t> probe_raw_printers(const std::vector<std::uint32_t> &targets,
                                              std::uint16_t port,
                                              const std::atomic<bool> *cancel = nullptr);
// Direct USB printing through the in-box driver, isolated in a child process like the spooler.
void send_usb(const std::wstring &directory, const Json &profile,
              const std::vector<unsigned char> &bytes);
// Locate and open (then close) the USB printer without sending any data.
void probe_usb(const Json &profile);
int usb_child(const std::wstring &file);
void log_code(const std::wstring &directory, const std::string &code);
} // namespace mv
