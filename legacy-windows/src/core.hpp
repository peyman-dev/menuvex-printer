#pragma once
#include <cstdint>
#include <functional>
#include <mutex>
#include <nlohmann/json.hpp>
#include <sqlite3.h>
#include <stdexcept>
#include <string>
#include <vector>
namespace mv {
using Json = nlohmann::json;
struct Error : std::runtime_error {
    std::string code;
    bool retryable, uncertain;
    Error(std::string c, std::string m, bool retry = false, bool unknown = false)
        : std::runtime_error(std::move(m)), code(std::move(c)), retryable(retry),
          uncertain(unknown) {}
    Json json() const;
};
Error unknown();
bool valid_id(const std::string &s);
// Page origins allowed to open the loopback WebSocket. The MenuVex dashboard that shows the Pair
// button runs on a subdomain (`https://app.menuvex.ir`); refusing it made pairing fail with the
// site's generic "cannot reach the local agent" while the listener was healthy. The apex
// (`https://menuvex.ir`) and any HTTPS subdomain of it are accepted; everything else — other
// schemes, ports, paths, userinfo and look-alike hosts — is refused.
bool origin_allowed(const std::string &origin);
// ---- Automatic printer discovery (portable policy; the Windows calls live in platform.cpp) ----
// Identity of a USB device parsed from its Windows device instance ID, e.g.
// `USB\VID_0416&PID_5011\5&2F1A8C3C&0&1`. `serial` is set only when the last segment is a
// device-reported serial number; Windows uses an `&`-separated location ID otherwise.
struct UsbIdentity {
    bool ok = false;
    int vendor = 0, product = 0;
    std::string serial;
};
UsbIdentity parse_usb_instance(const std::string &instance);
// USB port chain from a Windows location path such as
// `PCIROOT(0)#PCI(1D00)#USBROOT(0)#USB(1)#USB(4)` -> {1, 4}. Empty when unknown.
std::vector<int> usb_port_chain(const std::string &location_path);
// A connected USB printer as seen by discovery.
struct UsbCandidate {
    UsbIdentity id;
    std::vector<int> ports;
};
// Ready-to-save direct USB connection in the wire shape shared with the modern Agent (Legacy
// prints through the in-box Windows "USB Printing Support" driver, so bus/endpoint stay 0).
Json usb_connection(int vendor, int product, const std::string &serial,
                    const std::vector<int> &ports);
// Index of the connected device a stored USB connection refers to. Throws PRINTER_OFFLINE
// (retryable, nothing was sent) when it is not connected, and USB_AMBIGUOUS when identical
// printers without serial numbers cannot be told apart by their USB port.
std::size_t select_usb_device(const Json &connection, const std::vector<UsbCandidate> &present);
// Classify an installed Windows queue by its port/driver: "usb", "network", "wsd", "lpt",
// "com", "shared", "virtual" (XPS/PDF/Fax/OneNote/file) or "other".
std::string queue_kind(const std::string &name, const std::string &port, const std::string &driver);
// Literal private IPv4 of a Standard TCP/IP port name (`IP_192.168.1.50`, `192.168.1.50_1`).
std::string port_ipv4(const std::string &port);
// 58 for names that announce a 58 mm model (`POS-58`, `XP-58IIH`, `POS-5890`), otherwise 80.
int guess_paper_mm(const std::string &name);
// IPv4 helpers (host byte order). parse_ipv4 accepts only a strict dotted quad.
bool parse_ipv4(const std::string &text, std::uint32_t &address);
std::string format_ipv4(std::uint32_t address);
// RFC1918 unicast host (not .0/.255): the only addresses the LAN transport will ever contact.
bool private_lan_host(std::uint32_t address);
struct Ipv4Interface {
    std::uint32_t address, mask;
};
// Addresses to probe for RAW/9100 printers: `extra` first (IPs already known from Windows TCP/IP
// queues and saved printers), then every host of each private local subnet, bounded to the /24
// around the PC when the subnet is larger. Never the PC itself, never a public address.
std::vector<std::uint32_t> scan_targets(const std::vector<Ipv4Interface> &interfaces,
                                        const std::vector<std::uint32_t> &extra,
                                        std::size_t limit = 1024);
// Name of the saved printer that already uses `connection`, or "".
std::string configured_name(const Json &printers, const Json &connection);
// Solar Hijri (Jalali) date of a Gregorian date, for the Persian queue view (arithmetic 33-year
// cycle; exact for the contemporary era).
void jalali(int gy, int gm, int gd, int &jy, int &jm, int &jd);
void fields(const Json &j, std::initializer_list<const char *> required,
            std::initializer_list<const char *> optional = {});
void document(const Json &j);
Json parse_request(const std::string &text);
void validate_config(const Json &config);
Json upsert_printer(const Json &config, const Json &printer);
Json compatible_printers(Json printers);
Json default_config();
std::vector<std::string> lines(const Json &doc);
std::vector<std::string> printable_lines(const Json &doc, unsigned width_dots);
Json printer_test_document(const Json &profile);
std::vector<unsigned char> raster(unsigned width, unsigned height,
                                  const std::vector<unsigned char> &bits, bool cut);
std::int64_t now();
int backoff(int attempt);
class Store {
    sqlite3 *db_ = nullptr;
    std::mutex mutex_;
    Json job_unlocked(const std::string &id);

  public:
    explicit Store(const std::string &file);
    ~Store();
    Store(const Store &) = delete;
    Store &operator=(const Store &) = delete;
    Json config();
    void save(const Json &c);
    Json enqueue(const std::string &id, const std::string &printer, const Json &doc);
    Json get(const std::string &id);
    Json queue();
    Json claim(std::int64_t timestamp);
    Json finish(const std::string &id, const Json &error, int max_attempts, std::int64_t timestamp);
    Json cancel(const std::string &id);
    Json clear();
};
} // namespace mv
