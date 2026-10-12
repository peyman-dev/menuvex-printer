#include "core.hpp"
#include <algorithm>
#include <cctype>
#include <chrono>
#include <cstring>
#include <set>
namespace mv {
Json Error::json() const {
    return {
        {"code", code}, {"message", what()}, {"retryable", retryable}, {"uncertain", uncertain}};
}
Error unknown() {
    return {"PRINT_OUTCOME_UNKNOWN",
            "Data may have reached the printer. Inspect the paper (and the Windows print queue for "
            "spooler printers) before reprinting.",
            false, true};
}
static void require(bool ok, const char *message) {
    if (!ok)
        throw Error("INVALID_PAYLOAD", message);
}
bool valid_id(const std::string &s) {
    return !s.empty() && s.size() <= 128 && std::all_of(s.begin(), s.end(), [](unsigned char c) {
        return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
               c == '-' || c == '_' || c == ':' || c == '.';
    });
}
// Kept in the portable core (not in server.cpp) so the policy is covered by tests that run on
// every build machine, not only where WebSocket++/Windows is available.
bool origin_allowed(const std::string &origin) {
    static const std::string scheme = "https://";
    static const std::string domain = "menuvex.ir";
    if (origin.rfind(scheme, 0) != 0)
        return false;
    std::string rest = origin.substr(scheme.size());
    // A browser serializes an origin as exactly `scheme://host[:port]`: reject a path, query,
    // fragment, userinfo or whitespace before looking at the host.
    if (rest.empty() || !std::all_of(rest.begin(), rest.end(), [](unsigned char c) {
            return std::isalnum(c) || c == '.' || c == '-' || c == ':';
        }))
        return false;
    auto colon = rest.rfind(':');
    if (colon != std::string::npos) {
        if (rest.substr(colon + 1) != "443")
            return false;
        rest.resize(colon);
    }
    std::transform(rest.begin(), rest.end(), rest.begin(),
                   [](unsigned char c) { return static_cast<char>(std::tolower(c)); });
    if (rest == domain)
        return true;
    // Only a real label before an exact `.menuvex.ir` suffix counts, so `notmenuvex.ir` and
    // `menuvex.ir.evil.test` can never be mistaken for the product domain.
    return rest.size() > domain.size() + 1 &&
           rest.compare(rest.size() - domain.size(), domain.size(), domain) == 0 &&
           rest[rest.size() - domain.size() - 1] == '.';
}
// Strict dotted quad: exactly four decimal octets, no leading zeros, so a value round-trips
// through InetPtonA on the transport side.
static bool dotted_quad(const std::string &s, int (&octets)[4]) {
    int count = 0;
    std::size_t i = 0;
    while (i < s.size()) {
        std::size_t start = i;
        int value = 0;
        while (i < s.size() && s[i] >= '0' && s[i] <= '9') {
            value = value * 10 + (s[i] - '0');
            if (value > 255)
                return false;
            ++i;
        }
        std::size_t len = i - start;
        if (len == 0 || len > 3 || (len > 1 && s[start] == '0'))
            return false;
        if (count >= 4)
            return false;
        octets[count++] = value;
        if (i < s.size()) {
            if (s[i] != '.')
                return false;
            ++i;
            if (i == s.size())
                return false;
        }
    }
    return count == 4;
}
static bool private_octets(const int (&o)[4]) {
    bool private_range =
        o[0] == 10 || (o[0] == 172 && o[1] >= 16 && o[1] <= 31) || (o[0] == 192 && o[1] == 168);
    return private_range && o[3] != 0 && o[3] != 255;
}
// Literal RFC1918 unicast IPv4 only (no DNS, loopback, link-local, multicast or public), matching
// the modern Agent's network rules.
static bool private_ipv4(const std::string &s) {
    int octets[4];
    return dotted_quad(s, octets) && private_octets(octets);
}
bool parse_ipv4(const std::string &text, std::uint32_t &address) {
    int o[4];
    if (!dotted_quad(text, o))
        return false;
    address = (static_cast<std::uint32_t>(o[0]) << 24) | (static_cast<std::uint32_t>(o[1]) << 16) |
              (static_cast<std::uint32_t>(o[2]) << 8) | static_cast<std::uint32_t>(o[3]);
    return true;
}
std::string format_ipv4(std::uint32_t a) {
    return std::to_string(a >> 24) + "." + std::to_string((a >> 16) & 255) + "." +
           std::to_string((a >> 8) & 255) + "." + std::to_string(a & 255);
}
bool private_lan_host(std::uint32_t a) {
    const int o[4] = {static_cast<int>(a >> 24), static_cast<int>((a >> 16) & 255),
                      static_cast<int>((a >> 8) & 255), static_cast<int>(a & 255)};
    return private_octets(o);
}
// ---- Automatic printer discovery helpers (pure functions; covered by core_tests) ----
static std::string upper(std::string s) {
    std::transform(s.begin(), s.end(), s.begin(),
                   [](unsigned char c) { return static_cast<char>(std::toupper(c)); });
    return s;
}
static bool iequals(const std::string &a, const std::string &b) {
    return a.size() == b.size() &&
           std::equal(a.begin(), a.end(), b.begin(), [](unsigned char x, unsigned char y) {
               return std::toupper(x) == std::toupper(y);
           });
}
static std::string trim(const std::string &s) {
    auto first = s.find_first_not_of(" \t");
    if (first == std::string::npos)
        return {};
    return s.substr(first, s.find_last_not_of(" \t") - first + 1);
}
static bool hex4(const std::string &s, std::size_t at, int &value) {
    if (at + 4 > s.size())
        return false;
    value = 0;
    for (std::size_t i = at; i < at + 4; ++i) {
        const char c = s[i];
        int digit;
        if (c >= '0' && c <= '9')
            digit = c - '0';
        else if (c >= 'A' && c <= 'F')
            digit = c - 'A' + 10;
        else
            return false;
        value = value * 16 + digit;
    }
    return true;
}
UsbIdentity parse_usb_instance(const std::string &instance) {
    UsbIdentity out;
    const std::string id = upper(instance);
    // `USB\VID_xxxx&PID_yyyy[&MI_zz]\<instance>`
    int vendor = 0, product = 0;
    if (id.rfind("USB\\VID_", 0) != 0 || !hex4(id, 8, vendor) || id.compare(12, 5, "&PID_") != 0 ||
        !hex4(id, 17, product))
        return out;
    const auto slash = id.find('\\', 21);
    if (slash == std::string::npos || (slash != 21 && id.compare(21, 4, "&MI_") != 0))
        return out;
    const std::string tail = instance.substr(slash + 1);
    if (tail.empty() || tail.size() > 200 || tail.find('\\') != std::string::npos)
        return out;
    out.ok = true;
    out.vendor = vendor;
    out.product = product;
    // Windows keeps a device-reported serial number as the instance segment; a generated
    // location ID always contains '&'. An interface node (`&MI_xx`) never carries the serial.
    if (slash == 21 && tail.find('&') == std::string::npos && tail.size() <= 128)
        out.serial = tail;
    return out;
}
std::vector<int> usb_port_chain(const std::string &location_path) {
    const std::string path = upper(location_path);
    const auto root = path.find("USBROOT(");
    if (root == std::string::npos)
        return {};
    std::vector<int> ports;
    auto at = path.find(')', root);
    while (at != std::string::npos && at + 1 < path.size() && path[at + 1] == '#' &&
           path.compare(at + 2, 4, "USB(") == 0) {
        const auto open = at + 6;
        const auto close = path.find(')', open);
        if (close == std::string::npos || close == open || close - open > 3 || ports.size() >= 7)
            return {};
        int value = 0;
        for (auto i = open; i < close; ++i) {
            if (path[i] < '0' || path[i] > '9')
                return {};
            value = value * 10 + (path[i] - '0');
        }
        if (value < 1 || value > 255)
            return {};
        ports.push_back(value);
        at = close;
    }
    return ports;
}
Json usb_connection(int vendor, int product, const std::string &serial,
                    const std::vector<int> &ports) {
    Json chain = Json::array();
    for (int port : ports)
        chain.push_back(port);
    return {{"type", "usb"},
            {"vendorId", vendor},
            {"productId", product},
            {"serial", serial.empty() ? Json(nullptr) : Json(serial)},
            {"bus", 0},
            {"ports", chain},
            {"interface", 0},
            {"endpoint", 0},
            {"alternate", 0}};
}
static std::string usb_serial(const Json &connection) {
    if (!connection.contains("serial") || !connection["serial"].is_string())
        return {};
    return connection["serial"].get<std::string>();
}
static std::vector<int> usb_ports(const Json &connection) {
    std::vector<int> out;
    if (connection.contains("ports") && connection["ports"].is_array())
        for (const auto &port : connection["ports"])
            if (port.is_number_integer())
                out.push_back(port.get<int>());
    return out;
}
std::size_t select_usb_device(const Json &connection, const std::vector<UsbCandidate> &present) {
    const int vendor = connection.at("vendorId").get<int>();
    const int product = connection.at("productId").get<int>();
    const std::string serial = usb_serial(connection);
    std::vector<std::size_t> same;
    for (std::size_t i = 0; i < present.size(); ++i) {
        const auto &id = present[i].id;
        if (id.ok && id.vendor == vendor && id.product == product &&
            (serial.empty() || iequals(id.serial, serial)))
            same.push_back(i);
    }
    if (same.empty())
        throw Error("PRINTER_OFFLINE",
                    "The USB printer is not connected. Check its cable and power, then retry.",
                    true);
    if (same.size() == 1)
        return same.front();
    // Identical printers without a usable serial number: only the USB port tells them apart.
    const auto ports = usb_ports(connection);
    std::vector<std::size_t> located;
    if (!ports.empty())
        for (auto i : same)
            if (present[i].ports == ports)
                located.push_back(i);
    if (located.size() != 1)
        throw Error("USB_AMBIGUOUS", "Several identical USB printers are connected. Select this "
                                     "printer again in the Agent window.");
    return located.front();
}
std::string port_ipv4(const std::string &port) {
    std::string p = trim(port.substr(0, port.find(',')));
    if (upper(p).rfind("IP_", 0) == 0)
        p = p.substr(3);
    // LPR ports look like `host:queue`; Windows appends `_N` to duplicate TCP/IP port names.
    p = p.substr(0, p.find(':'));
    const auto underscore = p.find('_');
    if (underscore != std::string::npos) {
        const std::string suffix = p.substr(underscore + 1);
        if (suffix.empty() || suffix.size() > 3 ||
            suffix.find_first_not_of("0123456789") != std::string::npos)
            return {};
        p.resize(underscore);
    }
    std::uint32_t address = 0;
    return parse_ipv4(p, address) && private_lan_host(address) ? p : std::string();
}
std::string queue_kind(const std::string &name, const std::string &port,
                       const std::string &driver) {
    if (name.rfind("\\\\", 0) == 0)
        return "shared";
    const std::string first = upper(trim(port.substr(0, port.find(','))));
    const std::string d = upper(driver);
    for (const char *v : {"FILE:", "XPSPORT:", "PORTPROMPT:", "NUL:", "NUL",
                          "SHRFAX:", "MICROSOFT DOCUMENT IMAGING WRITER PORT:"})
        if (first == v)
            return "virtual";
    for (const char *v : {"XPS DOCUMENT WRITER", "FAX", "ONENOTE", "PDF", "DOCUMENT IMAGE WRITER"})
        if (d.find(v) != std::string::npos)
            return "virtual";
    auto numbered = [&](const char *prefix) {
        const std::size_t n = std::strlen(prefix);
        if (first.rfind(prefix, 0) != 0 || first.size() == n)
            return false;
        std::string rest = first.substr(n);
        if (!rest.empty() && rest.back() == ':')
            rest.pop_back();
        return !rest.empty() && rest.size() <= 3 &&
               rest.find_first_not_of("0123456789") == std::string::npos;
    };
    if (numbered("USB"))
        return "usb";
    if (numbered("LPT"))
        return "lpt";
    if (numbered("COM"))
        return "com";
    if (first.rfind("WSD-", 0) == 0)
        return "wsd";
    if (!port_ipv4(port).empty() || first.rfind("IP_", 0) == 0)
        return "network";
    return "other";
}
int guess_paper_mm(const std::string &name) {
    for (std::size_t i = 0; i + 1 < name.size(); ++i)
        if (name[i] == '5' && name[i + 1] == '8' &&
            (i == 0 || !std::isdigit(static_cast<unsigned char>(name[i - 1]))))
            return 58;
    return 80;
}
static int mask_prefix(std::uint32_t mask) {
    int prefix = 0;
    while (prefix < 32 && (mask & (0x80000000u >> prefix)))
        ++prefix;
    const std::uint32_t expected = prefix == 0 ? 0u : (0xFFFFFFFFu << (32 - prefix));
    return mask == expected ? prefix : -1;
}
std::vector<std::uint32_t> scan_targets(const std::vector<Ipv4Interface> &interfaces,
                                        const std::vector<std::uint32_t> &extra,
                                        std::size_t limit) {
    std::vector<std::uint32_t> out;
    std::set<std::uint32_t> seen, own;
    for (const auto &i : interfaces)
        own.insert(i.address);
    auto add = [&](std::uint32_t a) {
        if (out.size() < limit && private_lan_host(a) && !own.count(a) && seen.insert(a).second)
            out.push_back(a);
    };
    for (auto a : extra)
        add(a);
    for (const auto &i : interfaces) {
        if (!private_lan_host(i.address))
            continue;
        const int prefix = mask_prefix(i.mask);
        if (prefix > 30)
            continue; // a point-to-point/VPN host route is not a printer LAN
        std::uint32_t network = i.address & 0xFFFFFF00u, broadcast = network | 0xFFu;
        if (prefix >= 24) {
            network = i.address & i.mask;
            broadcast = network | ~i.mask;
        }
        for (std::uint32_t a = network + 1; a < broadcast; ++a)
            add(a);
    }
    return out;
}
std::string configured_name(const Json &printers, const Json &connection) {
    if (!printers.is_array() || !connection.is_object())
        return {};
    const std::string type = connection.value("type", std::string());
    for (const auto &p : printers) {
        if (!p.is_object() || !p.contains("connection") || !p["connection"].is_object())
            continue;
        const auto &c = p["connection"];
        if (c.value("type", std::string()) != type)
            continue;
        bool same = false;
        if (type == "network")
            same = c.value("host", std::string()) == connection.value("host", std::string()) &&
                   c.value("port", 0) == connection.value("port", 0);
        else if (type == "spooler")
            same = iequals(c.value("queueName", std::string()),
                           connection.value("queueName", std::string()));
        else if (type == "usb") {
            same = c.value("vendorId", -1) == connection.value("vendorId", -2) &&
                   c.value("productId", -1) == connection.value("productId", -2);
            const auto a = usb_serial(c), b = usb_serial(connection);
            const auto pa = usb_ports(c), pb = usb_ports(connection);
            if (same && (!a.empty() || !b.empty()))
                same = iequals(a, b);
            else if (same && !pa.empty() && !pb.empty())
                same = pa == pb;
        }
        if (same)
            return p.value("name", std::string());
    }
    return {};
}
void jalali(int gy, int gm, int gd, int &jy, int &jm, int &jd) {
    static const int before_month[] = {0, 31, 59, 90, 120, 151, 181, 212, 243, 273, 304, 334};
    const long gy2 = gm > 2 ? gy + 1 : gy;
    long days = 355666 + 365L * gy + (gy2 + 3) / 4 - (gy2 + 99) / 100 + (gy2 + 399) / 400 + gd +
                before_month[std::min(std::max(gm, 1), 12) - 1];
    long year = -1595 + 33 * (days / 12053);
    days %= 12053;
    year += 4 * (days / 1461);
    days %= 1461;
    if (days > 365) {
        year += (days - 1) / 365;
        days = (days - 1) % 365;
    }
    jy = static_cast<int>(year);
    if (days < 186) {
        jm = static_cast<int>(1 + days / 31);
        jd = static_cast<int>(1 + days % 31);
    } else {
        jm = static_cast<int>(7 + (days - 186) / 30);
        jd = static_cast<int>(1 + (days - 186) % 30);
    }
}
void fields(const Json &j, std::initializer_list<const char *> required,
            std::initializer_list<const char *> optional) {
    require(j.is_object(), "Expected an object");
    std::set<std::string> allowed;
    for (auto k : required) {
        require(j.contains(k), "Missing required field");
        allowed.insert(k);
    }
    for (auto k : optional)
        allowed.insert(k);
    for (auto it = j.begin(); it != j.end(); ++it)
        require(allowed.count(it.key()) != 0, "Unknown field");
}
static bool text(const Json &j, std::size_t max) {
    if (!j.is_string())
        return false;
    const auto &s = j.get_ref<const std::string &>();
    if (s.size() > max)
        return false;
    for (std::size_t i = 0; i < s.size(); ++i) {
        auto c = static_cast<unsigned char>(s[i]);
        if ((c < 32 && c != 10) || c == 127)
            return false;
        if (c == 0xc2 && i + 1 < s.size()) {
            auto n = static_cast<unsigned char>(s[i + 1]);
            if (n >= 0x80 && n <= 0x9f)
                return false;
        }
    }
    return true;
}
static bool number(const Json &j, std::int64_t min, std::int64_t max) {
    if (j.is_number_unsigned())
        return j.get<std::uint64_t>() <= static_cast<std::uint64_t>(max) &&
               j.get<std::uint64_t>() >= static_cast<std::uint64_t>(min);
    return j.is_number_integer() && j.get<std::int64_t>() >= min && j.get<std::int64_t>() <= max;
}
void document(const Json &j) {
    require(j.is_object() && j.contains("type") && j["type"].is_string(), "Invalid document");
    if (j["type"] == "receipt") {
        fields(j, {"type", "lines"});
        require(j["lines"].is_array() && !j["lines"].empty() && j["lines"].size() <= 100,
                "Invalid receipt lines");
        for (const auto &l : j["lines"])
            require(text(l, 500), "Invalid receipt text");
    } else if (j["type"] == "invoice") {
        fields(j, {"type", "data"});
        const auto &d = j["data"];
        fields(d, {"storeName", "orderNumber", "items", "total"},
               {"footer", "title", "address", "phone", "date", "status", "orderType", "table",
                "note", "currency", "subtotal"});
        require(text(d["storeName"], 300) && text(d["orderNumber"], 128) &&
                    (!d.contains("footer") || text(d["footer"], 1000)),
                "Invalid invoice text");
        // Optional app-template fields; printed only when supplied, never invented locally.
        const std::pair<const char *, std::size_t> optional_text[] = {
            {"title", 128},     {"address", 500}, {"phone", 64}, {"date", 64},     {"status", 128},
            {"orderType", 128}, {"table", 64},    {"note", 500}, {"currency", 32},
        };
        for (const auto &[key, limit] : optional_text)
            require(!d.contains(key) || text(d[key], limit), "Invalid invoice text");
        require(number(d["total"], 0, 9000000000000LL), "Invalid total");
        require(!d.contains("subtotal") || number(d["subtotal"], 0, 9000000000000LL),
                "Invalid subtotal");
        require(d["items"].is_array() && !d["items"].empty() && d["items"].size() <= 100,
                "Invalid invoice items");
        for (const auto &i : d["items"]) {
            fields(i, {"name", "quantity", "unitPrice"});
            require(text(i["name"], 300) && !i["name"].get_ref<const std::string &>().empty() &&
                        number(i["quantity"], 1, 9999) && number(i["unitPrice"], 0, 900000000),
                    "Invalid item");
        }
    } else
        throw Error("INVALID_JOB", "Unsupported document type");
}
Json parse_request(const std::string &raw) {
    require(raw.size() <= 128 * 1024, "Message too large");
    try {
        // Refuse duplicate keys as well as unknown keys, rather than accepting last-wins JSON.
        std::vector<std::set<std::string>> keys;
        Json j = Json::parse(raw, [&](int depth, Json::parse_event_t event, Json &value) {
            require(depth <= 32, "JSON nesting too deep");
            if (event == Json::parse_event_t::object_start)
                keys.emplace_back();
            if (event == Json::parse_event_t::key)
                require(keys.back().insert(value.get<std::string>()).second, "Duplicate field");
            if (event == Json::parse_event_t::object_end)
                keys.pop_back();
            return true;
        });
        require(j.is_object(), "Expected request object");
        if (!j.contains("version") || !number(j["version"], 1, 1))
            throw Error("PROTOCOL_VERSION", "Only version 1 is supported");
        require(j.contains("requestId") && j["requestId"].is_string() && valid_id(j["requestId"]),
                "Invalid requestId");
        require(j.contains("type") && j["type"].is_string(), "Missing command");
        auto type = j["type"].get<std::string>();
        if (type == "hello" || type == "ping" || type == "agent.status" ||
            type == "printers.list" || type == "queue.list" || type == "queue.clear" ||
            type == "agent.shutdown" || type == "discover.network" || type == "printers.installed")
            fields(j, {"version", "requestId", "type"});
        else if (type == "authenticate") {
            fields(j, {"version", "requestId", "type", "proof"});
            require(text(j["proof"], 128), "Invalid proof");
        } else if (type == "printer.get")
            fields(j, {"version", "requestId", "type", "printerId"});
        else if (type == "printer.test")
            fields(j, {"version", "requestId", "type", "printerId", "jobId"});
        else if (type == "print") {
            fields(j, {"version", "requestId", "type", "printerId", "jobId", "document"});
            document(j["document"]);
        } else if (type == "print.status" || type == "queue.cancel")
            fields(j, {"version", "requestId", "type", "jobId"});
        else if (type == "printer.save")
            fields(j, {"version", "requestId", "type", "printer"});
        else
            throw Error("INVALID_PAYLOAD", "Unknown command");
        for (auto k : {"printerId", "jobId"})
            if (j.contains(k))
                require(j[k].is_string() && valid_id(j[k]), "Invalid identifier");
        return j;
    } catch (const Json::exception &) {
        throw Error("INVALID_PAYLOAD", "Invalid JSON payload");
    }
}
Json default_config() {
    return {{"port", 8765},
            {"maxAttempts", 3},
            {"autostart", true},
            {"printers", Json::array()},
            {"routes", Json::array()}};
}
void validate_config(const Json &c) {
    fields(c, {"port", "maxAttempts", "autostart", "printers", "routes"});
    require(number(c["port"], 1024, 65535) && number(c["maxAttempts"], 1, 5) &&
                c["autostart"].is_boolean(),
            "Invalid settings");
    require(c["printers"].is_array() && c["printers"].size() <= 16 && c["routes"].is_array() &&
                c["routes"].size() <= 16,
            "Too many printers/routes");
    std::set<std::string> ids, roles;
    for (const auto &p : c["printers"]) {
        fields(p, {"id", "name", "connection", "paperMm", "widthDots", "copies", "cut",
                   "fontFamily", "fontSize"});
        require(p["id"].is_string() && valid_id(p["id"]) && ids.insert(p["id"]).second,
                "Invalid/duplicate printer ID");
        require(text(p["name"], 128) && !p["name"].get_ref<const std::string &>().empty(),
                "Invalid name");
        // Legacy supports installed Windows spooler queues, direct LAN TCP/9100 and direct USB
        // through the in-box Windows "USB Printing Support" driver.
        const auto &conn = p["connection"];
        require(conn.is_object() && conn.contains("type") && conn["type"].is_string(),
                "Invalid connection");
        auto ctype = conn["type"].get<std::string>();
        if (ctype == "spooler") {
            fields(conn, {"type", "queueName"});
            require(text(conn["queueName"], 512) &&
                        !conn["queueName"].get_ref<const std::string &>().empty(),
                    "Invalid spooler queue");
        } else if (ctype == "network") {
            fields(conn, {"type", "host", "port"});
            require(conn["host"].is_string() && private_ipv4(conn["host"].get<std::string>()),
                    "Use a literal private IPv4 address (e.g. 192.168.1.50)");
            require(number(conn["port"], 1, 65535), "Invalid LAN port (1-65535)");
        } else if (ctype == "usb") {
            // Same wire shape as the modern Agent. Legacy locates the device by VID/PID, serial
            // number and USB port chain; bus/interface/endpoint are kept as received. VID 0 is
            // reserved for the spooler compatibility descriptor and never a real device.
            fields(conn,
                   {"type", "vendorId", "productId", "bus", "ports", "interface", "endpoint",
                    "alternate"},
                   {"serial"});
            require(number(conn["vendorId"], 1, 65535) && number(conn["productId"], 0, 65535),
                    "Invalid USB vendor/product ID");
            require(!conn.contains("serial") || conn["serial"].is_null() ||
                        (text(conn["serial"], 128) &&
                         !conn["serial"].get_ref<const std::string &>().empty()),
                    "Invalid USB serial number");
            require(number(conn["bus"], 0, 255) && number(conn["interface"], 0, 255) &&
                        number(conn["endpoint"], 0, 255) && number(conn["alternate"], 0, 255) &&
                        conn["ports"].is_array() && conn["ports"].size() <= 7,
                    "Invalid USB device location");
            for (const auto &port : conn["ports"])
                require(number(port, 0, 255), "Invalid USB device location");
        } else {
            // Name the rejected value: an operator adding a printer must see which type their
            // client sent, not a generic message (same rule as the modern agent).
            throw Error("UNSUPPORTED_CONNECTION_TYPE",
                        "Unsupported printer connection type: \"" +
                            (ctype.size() > 64 ? ctype.substr(0, 64) : ctype) +
                            "\". The Legacy agent supports \"spooler\", \"network\" and \"usb\".");
        }
        require(number(p["paperMm"], 58, 80) && (p["paperMm"] == 58 || p["paperMm"] == 80) &&
                    number(p["widthDots"], 128, 832) && p["widthDots"].get<int>() % 8 == 0,
                "Invalid paper profile");
        require(number(p["copies"], 1, 3) && p["cut"].is_boolean() &&
                    number(p["fontSize"], 12, 48) && p["fontFamily"] == "Noto Sans Arabic",
                "Invalid print profile");
    }
    for (const auto &r : c["routes"]) {
        fields(r, {"role", "printerId", "autoPrint"});
        require(r["role"].is_string() && valid_id(r["role"]) && roles.insert(r["role"]).second &&
                    r["printerId"].is_string() && ids.count(r["printerId"]) &&
                    r["autoPrint"].is_boolean(),
                "Invalid route");
    }
}
Json compatible_printers(Json printers) {
    require(printers.is_array(), "Printer list must be an array");
    for (auto &printer : printers) {
        if (!printer.is_object() || !printer.contains("connection"))
            continue;
        auto &connection = printer["connection"];
        if (connection.is_object() && connection.value("type", std::string()) == "spooler") {
            const auto queue = connection.at("queueName").get<std::string>();
            connection = {{"type", "usb"},
                          {"vendorId", 0},
                          {"productId", 0},
                          {"serial", "queue:" + queue},
                          {"bus", 0},
                          {"ports", Json::array()},
                          {"interface", 0},
                          {"endpoint", 0},
                          {"alternate", 0}};
        } else if (connection.is_object() && connection.contains("type") &&
                   connection["type"] == "usb" && !connection.contains("serial")) {
            // The shared SDK schema requires the key; a device without a serial reports null.
            connection["serial"] = nullptr;
        }
    }
    return printers;
}

static Json normalize_printer_wire(const Json &printer) {
    Json normalized = printer;
    auto &connection = normalized.at("connection");
    const bool queue_compat = connection.is_object() && connection.contains("type") &&
                              connection["type"].is_string() && connection["type"] == "usb" &&
                              connection.contains("vendorId") &&
                              number(connection["vendorId"], 0, 0) &&
                              connection.contains("productId") &&
                              number(connection["productId"], 0, 65535) &&
                              connection.contains("serial") && connection["serial"].is_string() &&
                              connection["serial"].get<std::string>().rfind("queue:", 0) == 0;
    if (queue_compat) {
        fields(connection, {"type", "vendorId", "productId", "serial", "bus", "ports",
                            "interface", "endpoint", "alternate"});
        require(number(connection["bus"], 0, 0) && connection["ports"].is_array() &&
                    connection["ports"].empty() && number(connection["interface"], 0, 0) &&
                    number(connection["endpoint"], 0, 0) && number(connection["alternate"], 0, 0),
                "Invalid spooler compatibility descriptor");
        const auto serial = connection["serial"].get<std::string>();
        const std::string queue = serial.substr(6);
        require(!queue.empty() && text(queue, 512), "Invalid spooler queue");
        normalized["connection"] = {{"type", "spooler"}, {"queueName", queue}};
    }
    auto &stored = normalized.at("connection");
    if (stored.is_object() && stored.contains("type") && stored["type"] == "usb" &&
        !stored.contains("serial"))
        stored["serial"] = nullptr;
    return normalized;
}

Json upsert_printer(const Json &config, const Json &printer) {
    const Json normalized = normalize_printer_wire(printer);
    // Validate the single profile through the shared config validator first.
    Json probe = config;
    probe["printers"] = Json::array({normalized});
    probe["routes"] = Json::array();
    validate_config(probe);
    Json next = config;
    if (!next.contains("printers") || !next["printers"].is_array())
        next["printers"] = Json::array();
    const std::string id = normalized.at("id").get<std::string>();
    bool replaced = false;
    for (auto &p : next["printers"]) {
        if (p.is_object() && p.value("id", std::string()) == id) {
            p = normalized;
            replaced = true;
            break;
        }
    }
    if (!replaced) {
        if (next["printers"].size() >= 16)
            throw Error("INVALID_CONFIG", "Too many printers", false);
        next["printers"].push_back(normalized);
    }
    validate_config(next);
    return next;
}
// Thousands separators for readability only; the value itself is never converted.
static std::string group(std::int64_t value) {
    std::string digits = std::to_string(value);
    bool negative = !digits.empty() && digits.front() == '-';
    if (negative)
        digits.erase(digits.begin());
    std::string out;
    for (std::size_t i = 0; i < digits.size(); ++i) {
        if (i > 0 && (digits.size() - i) % 3 == 0)
            out.push_back(',');
        out.push_back(digits[i]);
    }
    return negative ? "-" + out : out;
}
std::vector<std::string> lines(const Json &doc) {
    document(doc);
    if (doc["type"] == "receipt")
        return doc["lines"].get<std::vector<std::string>>();
    const auto &d = doc["data"];
    // App-template projection (docs/escpos.md): optional fields print only when supplied and
    // the agent never invents business data (no local currency word or subtotal). Persian
    // lines hang on the right margin, number-only lines on the left margin; a run of dashes
    // becomes a solid separator rule in the renderer.
    const std::string currency = d.value("currency", std::string{});
    auto amount = [&](std::int64_t value) {
        return currency.empty() ? group(value) : group(value) + " " + currency;
    };
    std::vector<std::string> out = {d["storeName"]};
    if (d.contains("address") && !d["address"].get<std::string>().empty())
        out.push_back(d["address"]);
    if (d.contains("phone") && !d["phone"].get<std::string>().empty())
        out.push_back(std::string(u8"تلفن: ") + d["phone"].get<std::string>());
    out.push_back(std::string(16, '-'));
    std::string title = d.value("title", std::string{});
    if (title.empty())
        title = u8"فاکتور فروش";
    out.push_back(title + u8" | فیش " + d["orderNumber"].get<std::string>());
    out.push_back(std::string(16, '-'));
    const std::pair<const char *, const char *> details[] = {{"date", u8"تاریخ: "},
                                                             {"status", u8"وضعیت: "},
                                                             {"orderType", u8"نوع سفارش: "},
                                                             {"table", u8"میز: "}};
    for (const auto &[key, label] : details)
        if (d.contains(key) && !d[key].get<std::string>().empty())
            out.push_back(label + d[key].get<std::string>());
    out.push_back(std::string(16, '-'));
    for (const auto &i : d["items"]) {
        out.push_back(i["name"]);
        out.push_back(
            group(i["quantity"].get<std::int64_t>()) + u8" × " +
            group(i["unitPrice"].get<std::int64_t>()) + u8" = " +
            group(i["quantity"].get<std::int64_t>() * i["unitPrice"].get<std::int64_t>()));
    }
    out.push_back(std::string(16, '-'));
    out.push_back(std::string(u8"تعداد اقلام: ") + std::to_string(d["items"].size()));
    if (d.contains("subtotal"))
        out.push_back(std::string(u8"جمع اقلام: ") + amount(d["subtotal"].get<std::int64_t>()));
    out.push_back(std::string(u8"مبلغ قابل پرداخت: ") + amount(d["total"].get<std::int64_t>()));
    if (d.contains("note") && !d["note"].get<std::string>().empty())
        out.push_back(std::string(u8"یادداشت: ") + d["note"].get<std::string>());
    out.push_back(d.value("footer", std::string{}));
    out.push_back("POWERED BY MENUVEX.IR");
    return out;
}
static std::string fa_digits(const std::string &text) {
    static const char *digits[] = {u8"۰", u8"۱", u8"۲", u8"۳", u8"۴",
                                   u8"۵", u8"۶", u8"۷", u8"۸", u8"۹"};
    std::string out;
    for (char c : text)
        if (c >= '0' && c <= '9')
            out += digits[c - '0'];
        else
            out.push_back(c);
    return out;
}
std::vector<std::string> printable_lines(const Json &doc, unsigned width_dots) {
    document(doc);
    if (doc["type"] == "receipt")
        return doc["lines"].get<std::vector<std::string>>();

    const auto &d = doc["data"];
    const auto currency = d.value("currency", std::string{});
    auto amount = [&](std::int64_t value) {
        auto grouped = fa_digits(group(value));
        return currency.empty() ? grouped : grouped + " " + currency;
    };
    std::vector<std::string> out;
    auto append_centered = [&](const std::string &value) {
        if (!value.empty())
            out.push_back("[center] " + value);
    };
    append_centered(d["storeName"].get<std::string>());
    if (d.contains("address"))
        append_centered(d["address"].get<std::string>());
    if (d.contains("phone") && !d["phone"].get<std::string>().empty())
        append_centered(std::string(u8"تلفن: ") + fa_digits(d["phone"].get<std::string>()));

    const std::string rule(24, '-');
    const std::string title = d.value("title", std::string{}).empty()
                                  ? std::string(u8"فاکتور فروش")
                                  : d.value("title", std::string{});
    out.push_back(rule);
    out.push_back(title + " | " + std::string(u8"فیش ") + fa_digits(d["orderNumber"]));
    out.push_back(rule);
    const std::pair<const char *, const char *> details[] = {{"date", u8"تاریخ"},
                                                             {"status", u8"وضعیت"},
                                                             {"orderType", u8"نوع سفارش"},
                                                             {"table", u8"میز"}};
    for (const auto &[key, label] : details)
        if (d.contains(key) && !d[key].get<std::string>().empty())
            out.push_back(std::string(label) + " | " + fa_digits(d[key].get<std::string>()));

    if (width_dots >= 464) {
        out.push_back(rule);
        out.push_back(std::string(u8"شرح کالا | تعداد | قیمت واحد | جمع"));
        out.push_back(rule);
        for (const auto &item : d["items"]) {
            const auto quantity = item["quantity"].get<std::int64_t>();
            const auto unit_price = item["unitPrice"].get<std::int64_t>();
            const auto line_total = quantity * unit_price;
            out.push_back(item["name"].get<std::string>() + " | " +
                          fa_digits(std::to_string(quantity)) + " | " +
                          fa_digits(group(unit_price)) + " | " + fa_digits(group(line_total)));
        }
    } else {
        out.push_back(rule);
        out.push_back(std::string(u8"شرح کالا | جمع"));
        out.push_back(rule);
        for (const auto &item : d["items"]) {
            const auto quantity = item["quantity"].get<std::int64_t>();
            const auto unit_price = item["unitPrice"].get<std::int64_t>();
            out.push_back(item["name"].get<std::string>() + " | " +
                          fa_digits(group(quantity * unit_price)));
            if (quantity > 1)
                out.push_back(fa_digits(std::to_string(quantity)) + " × " +
                              fa_digits(group(unit_price)));
        }
    }
    out.push_back(rule);
    out.push_back(std::string(u8"تعداد اقلام") + " | " +
                  fa_digits(std::to_string(d["items"].size())));
    if (d.contains("subtotal"))
        out.push_back(std::string(u8"جمع اقلام") + " | " + amount(d["subtotal"]));
    out.push_back(rule);
    out.push_back(std::string(u8"مبلغ قابل پرداخت") + " | " + amount(d["total"]));
    if (d.contains("note") && !d["note"].get<std::string>().empty()) {
        out.push_back("[dashed]");
        append_centered(std::string(u8"یادداشت: ") + d["note"].get<std::string>());
        out.push_back("[dashed]");
    }
    append_centered(d.value("footer", std::string{}));
    append_centered("POWERED BY MENUVEX.IR");
    return out;
}
Json printer_test_document(const Json &profile) {
    const int paper_mm = profile.at("paperMm").get<int>();
    const int width_dots = profile.at("widthDots").get<int>();
    return {{"type", "receipt"},
            {"lines", {"[center] MenuVex printer test",
                       "Paper profile | " + std::to_string(paper_mm) + " mm | " +
                           std::to_string(width_dots) + " dots",
                       "Item | Qty | Amount",
                       "Espresso | 2 | 240,000",
                       std::string(40, '-'),
                       u8"آزمون چاپ فارسی — سلام دنیا",
                       u8"۰۱۲۳۴۵۶۷۸۹ / 0123456789"}}};
}
std::vector<unsigned char> raster(unsigned width, unsigned height,
                                  const std::vector<unsigned char> &bits, bool cut) {
    require(width >= 128 && width <= 832 && width % 8 == 0 && height > 0 && height <= 4096 &&
                bits.size() == (width / 8) * height,
            "Invalid raster");
    std::vector<unsigned char> out = {27, 64};
    unsigned stride = width / 8;
    for (unsigned row = 0; row < height; row += 128) {
        unsigned h = std::min(128u, height - row);
        out.insert(out.end(), {29, 118, 48, 0, static_cast<unsigned char>(stride), 0,
                               static_cast<unsigned char>(h), 0});
        out.insert(out.end(), bits.begin() + row * stride, bits.begin() + (row + h) * stride);
    }
    out.insert(out.end(), {27, 100, 3});
    if (cut)
        out.insert(out.end(), {29, 86, 0});
    return out;
}
std::int64_t now() {
    return std::chrono::duration_cast<std::chrono::seconds>(
               std::chrono::system_clock::now().time_since_epoch())
        .count();
}
int backoff(int attempt) { return std::min(60, 1 << std::min(std::max(0, attempt), 6)); }
struct Statement {
    sqlite3_stmt *s = nullptr;
    Statement(sqlite3 *db, const char *sql) {
        if (sqlite3_prepare_v2(db, sql, -1, &s, nullptr) != SQLITE_OK)
            throw Error("QUEUE_ERROR", "Unable to prepare database operation");
    }
    ~Statement() { sqlite3_finalize(s); }
    void bind(int i, const std::string &v) {
        if (sqlite3_bind_text(s, i, v.c_str(), static_cast<int>(v.size()), SQLITE_TRANSIENT) !=
            SQLITE_OK)
            throw Error("QUEUE_ERROR", "Unable to bind value");
    }
    void bind(int i, std::int64_t v) {
        if (sqlite3_bind_int64(s, i, v) != SQLITE_OK)
            throw Error("QUEUE_ERROR", "Unable to bind integer");
    }
    bool row() {
        int r = sqlite3_step(s);
        if (r == SQLITE_ROW)
            return true;
        if (r == SQLITE_DONE)
            return false;
        throw Error("QUEUE_ERROR", "Database operation failed");
    }
    std::string str(int i) {
        auto p = sqlite3_column_text(s, i);
        return p ? reinterpret_cast<const char *>(p) : "";
    }
};
static void exec(sqlite3 *db, const char *sql) {
    if (sqlite3_exec(db, sql, nullptr, nullptr, nullptr) != SQLITE_OK)
        throw Error("QUEUE_ERROR", "Database transaction failed");
}
struct Transaction {
    sqlite3 *db;
    bool committed = false;
    explicit Transaction(sqlite3 *d) : db(d) { exec(db, "BEGIN IMMEDIATE"); }
    ~Transaction() {
        if (!committed)
            sqlite3_exec(db, "ROLLBACK", nullptr, nullptr, nullptr);
    }
    void commit() {
        exec(db, "COMMIT");
        committed = true;
    }
};
Store::Store(const std::string &file) {
    if (sqlite3_open_v2(file.c_str(), &db_,
                        SQLITE_OPEN_READWRITE | SQLITE_OPEN_CREATE | SQLITE_OPEN_FULLMUTEX,
                        nullptr) != SQLITE_OK) {
        if (db_)
            sqlite3_close(db_);
        db_ = nullptr;
        throw Error("QUEUE_ERROR", "Cannot open data store");
    }
    try {
        sqlite3_busy_timeout(db_, 5000);
        {
            Statement v(db_, "PRAGMA user_version");
            v.row();
            if (sqlite3_column_int(v.s, 0) > 1)
                throw Error("SCHEMA_VERSION_UNSUPPORTED", "Refusing database downgrade");
        }
        exec(db_, "PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; CREATE TABLE IF NOT EXISTS "
                  "config(id INTEGER PRIMARY KEY CHECK(id=1),json TEXT NOT NULL); CREATE TABLE IF "
                  "NOT EXISTS jobs(id TEXT PRIMARY KEY,printer TEXT NOT NULL,document TEXT NOT "
                  "NULL,profile TEXT NOT NULL,status TEXT NOT NULL,attempts INTEGER NOT "
                  "NULL,created INTEGER NOT NULL,next INTEGER NOT NULL,error TEXT); CREATE INDEX "
                  "IF NOT EXISTS due ON jobs(status,next); PRAGMA user_version=1;");
        Statement recover(db_, "UPDATE jobs SET status='failed',error=? WHERE status='printing'");
        recover.bind(1, unknown().json().dump());
        recover.row();
    } catch (...) {
        sqlite3_close(db_);
        db_ = nullptr;
        throw;
    }
}
Store::~Store() { sqlite3_close(db_); }
Json Store::config() {
    std::lock_guard<std::mutex> lock(mutex_);
    Statement q(db_, "SELECT json FROM config WHERE id=1");
    auto c = q.row() ? Json::parse(q.str(0)) : default_config();
    validate_config(c);
    return c;
}
void Store::save(const Json &c) {
    validate_config(c);
    std::lock_guard<std::mutex> lock(mutex_);
    Statement q(db_,
                "INSERT INTO config VALUES(1,?) ON CONFLICT(id) DO UPDATE SET json=excluded.json");
    q.bind(1, c.dump());
    q.row();
}
Json Store::job_unlocked(const std::string &id) {
    Statement q(db_, "SELECT id,printer,status,attempts,created,next,error FROM jobs WHERE id=?");
    q.bind(1, id);
    if (!q.row())
        throw Error("JOB_NOT_FOUND", "Job ID not found");
    Json error = nullptr;
    if (sqlite3_column_type(q.s, 6) != SQLITE_NULL)
        error = Json::parse(q.str(6));
    return {{"jobId", q.str(0)},
            {"printerId", q.str(1)},
            {"status", q.str(2)},
            {"attempts", sqlite3_column_int(q.s, 3)},
            {"createdAt", sqlite3_column_int64(q.s, 4)},
            {"nextAt", sqlite3_column_int64(q.s, 5)},
            {"error", error}};
}
Json Store::get(const std::string &id) {
    std::lock_guard<std::mutex> lock(mutex_);
    return job_unlocked(id);
}
Json Store::enqueue(const std::string &id, const std::string &printer, const Json &input) {
    require(valid_id(id) && valid_id(printer), "Invalid ID");
    document(input);
    Json doc = input;
    if (doc["type"] == "invoice" && !doc["data"].contains("footer"))
        doc["data"]["footer"] = "";
    std::lock_guard<std::mutex> lock(mutex_);
    Transaction tx(db_);
    Statement old(db_, "SELECT printer,document FROM jobs WHERE id=?");
    old.bind(1, id);
    if (old.row()) {
        if (old.str(0) != printer || old.str(1) != doc.dump())
            throw Error("JOB_ID_CONFLICT", "This ID belongs to another document/printer");
        tx.commit();
        return job_unlocked(id);
    }
    Statement cfg(db_, "SELECT json FROM config WHERE id=1");
    if (!cfg.row())
        throw Error("PRINTER_NOT_FOUND", "Configure a printer locally first");
    Json c = Json::parse(cfg.str(0)), p;
    for (const auto &candidate : c["printers"])
        if (candidate["id"] == printer)
            p = candidate;
    if (p.is_null())
        throw Error("PRINTER_NOT_FOUND", "Printer not configured");
    Statement count(db_, "SELECT COUNT(*) FROM jobs WHERE status IN ('queued','printing')");
    count.row();
    if (sqlite3_column_int(count.s, 0) >= 256)
        throw Error("QUEUE_FULL", "Resolve pending jobs first");
    Statement add(db_, "INSERT INTO jobs VALUES(?,?,?,?,'queued',0,?,?,NULL)");
    add.bind(1, id);
    add.bind(2, printer);
    add.bind(3, doc.dump());
    add.bind(4, p.dump());
    add.bind(5, now());
    add.bind(6, now());
    add.row();
    tx.commit();
    return job_unlocked(id);
}
Json Store::queue() {
    std::lock_guard<std::mutex> lock(mutex_);
    Statement q(db_, "SELECT id FROM jobs ORDER BY CASE WHEN status IN ('queued','printing') THEN "
                     "0 ELSE 1 END,created DESC,rowid DESC LIMIT 500");
    Json out = Json::array();
    while (q.row())
        out.push_back(job_unlocked(q.str(0)));
    return out;
}
Json Store::claim(std::int64_t time) {
    std::lock_guard<std::mutex> lock(mutex_);
    Transaction tx(db_);
    Statement q(db_,
                "SELECT id,profile,document FROM jobs WHERE status='queued' AND next<=? AND NOT "
                "EXISTS(SELECT 1 FROM jobs prior WHERE prior.printer=jobs.printer AND prior.status "
                "IN ('queued','printing') AND prior.rowid<jobs.rowid) ORDER BY rowid LIMIT 1");
    q.bind(1, time);
    if (!q.row()) {
        tx.commit();
        return nullptr;
    }
    auto id = q.str(0);
    Json work = {{"profile", Json::parse(q.str(1))}, {"document", Json::parse(q.str(2))}};
    Statement u(db_, "UPDATE jobs SET status='printing',attempts=attempts+1,error=NULL WHERE id=?");
    u.bind(1, id);
    u.row();
    tx.commit();
    work["job"] = job_unlocked(id);
    return work;
}
Json Store::finish(const std::string &id, const Json &error, int max, std::int64_t time) {
    std::lock_guard<std::mutex> lock(mutex_);
    auto job = job_unlocked(id);
    if (job["status"] != "printing")
        throw Error("QUEUE_ERROR", "Job is not claimed");
    bool retry = !error.is_null() && error.value("retryable", false) &&
                 !error.value("uncertain", false) && job["attempts"].get<int>() < max;
    Statement u(db_, "UPDATE jobs SET status=?,next=?,error=? WHERE id=?");
    u.bind(1, error.is_null() ? "completed" : retry ? "queued" : "failed");
    u.bind(2, time + (retry ? backoff(job["attempts"]) : 0));
    if (error.is_null())
        sqlite3_bind_null(u.s, 3);
    else
        u.bind(3, error.dump());
    u.bind(4, id);
    u.row();
    return job_unlocked(id);
}
Json Store::cancel(const std::string &id) {
    std::lock_guard<std::mutex> lock(mutex_);
    auto job = job_unlocked(id);
    if (job["status"] == "cancelled")
        return job;
    if (job["status"] != "queued")
        throw Error(
            "JOB_NOT_CANCELLABLE",
            "Only queued jobs can be cancelled; Windows spooler handoffs cannot be recalled here");
    Statement u(db_, "UPDATE jobs SET status='cancelled' WHERE id=?");
    u.bind(1, id);
    u.row();
    return job_unlocked(id);
}
// Empty the queue: cancel every queued job and delete finished history. A job in 'printing'
// is never touched — the spooler may already own its bytes. Deleting history removes the
// duplicate-submission protection of those job IDs (docs/protocol.md).
Json Store::clear() {
    std::lock_guard<std::mutex> lock(mutex_);
    Transaction tx(db_);
    std::vector<std::string> queued;
    {
        Statement q(db_, "SELECT id FROM jobs WHERE status='queued' ORDER BY rowid");
        while (q.row())
            queued.push_back(q.str(0));
    }
    {
        Statement u(db_, "UPDATE jobs SET status='cancelled' WHERE status='queued'");
        u.row();
    }
    Json cancelled = Json::array();
    for (const auto &id : queued)
        cancelled.push_back(job_unlocked(id));
    {
        Statement d(db_, "DELETE FROM jobs WHERE status != 'printing'");
        d.row();
    }
    int removed = sqlite3_changes(db_);
    tx.commit();
    // `cancelledJobs` feeds the event broadcast; clients receive {cancelled, removed} counts.
    return {{"cancelledJobs", cancelled}, {"removed", removed}};
}
} // namespace mv
