#include "core.hpp"
#include <atomic>
#include <cstdio>
#include <cstdlib>
#include <iostream>
#include <thread>
using namespace mv;
static int checks = 0;
static void check(bool value, const char *reason) {
    ++checks;
    if (!value)
        throw std::runtime_error(reason);
}
template <class F> void rejects(F f, const char *code = nullptr, const char *message = nullptr) {
    try {
        f();
    } catch (const Error &e) {
        check(!code || e.code == code, "Unexpected error code");
        check(!message || std::string(e.what()).find(message) != std::string::npos,
              "Error message does not name the rejected value");
        return;
    }
    throw std::runtime_error("Invalid input was accepted");
}
Json profile() {
    return {{"id", "p1"},
            {"name", "Test"},
            {"connection", {{"type", "spooler"}, {"queueName", "Test queue"}}},
            {"paperMm", 80},
            {"widthDots", 576},
            {"copies", 1},
            {"cut", true},
            {"fontFamily", "Noto Sans Arabic"},
            {"fontSize", 24}};
}
Json network_profile() {
    return {{"id", "n1"},
            {"name", "LAN"},
            {"connection", {{"type", "network"}, {"host", "192.168.1.50"}, {"port", 9100}}},
            {"paperMm", 58},
            {"widthDots", 384},
            {"copies", 1},
            {"cut", true},
            {"fontFamily", "Noto Sans Arabic"},
            {"fontSize", 24}};
}
Json receipt() { return {{"type", "receipt"}, {"lines", {u8"سلام دنیا", "Test"}}}; }
int main() {
    try {
        for (const char *type :
             {"hello", "ping", "agent.status", "printers.list", "queue.list", "agent.shutdown"}) {
            Json r = {{"version", 1}, {"requestId", "r1"}, {"type", type}};
            check(parse_request(r.dump())["type"] == type, "valid no-argument command");
            r["host"] = "127.0.0.1";
            rejects([&] { parse_request(r.dump()); }, "INVALID_PAYLOAD");
        }
        rejects([] {
            parse_request(R"({"version":1,"requestId":"r1","type":"ping","type":"hello"})");
        });
        rejects([] { parse_request(R"({"version":2,"requestId":"r1","type":"ping"})"); },
                "PROTOCOL_VERSION");
        rejects([] { parse_request(R"({"version":1,"requestId":"r1","type":"print"})"); });
        rejects([] { document({{"type", "receipt"}, {"lines", {"\x1b@"}}}); });
        auto bad = receipt();
        bad["host"] = "x";
        rejects([&] { document(bad); });
        Json invoice = {
            {"type", "invoice"},
            {"data",
             {{"storeName", u8"کافه"},
              {"orderNumber", "1"},
              {"items", Json::array({{{"name", u8"چای"}, {"quantity", 2}, {"unitPrice", 100}}})},
              {"total", 200}}}};
        check(lines(invoice).size() >= 6, "semantic invoice rendering");
        invoice["data"]["items"][0]["quantity"] = 1.5;
        rejects([&] { document(invoice); });
        // The dashboard that shows the Pair button is served from https://app.menuvex.ir; the
        // agent must accept the product domain and its HTTPS subdomains and nothing else.
        for (const char *good :
             {"https://menuvex.ir", "https://www.menuvex.ir", "https://app.menuvex.ir",
              "https://APP.Menuvex.IR", "https://app.menuvex.ir:443"})
            check(origin_allowed(good), "allowed product origin");
        for (const char *bad :
             {"http://app.menuvex.ir", "https://app.menuvex.ir:8443",
              "https://menuvex.ir.evil.test", "https://notmenuvex.ir", "https://.menuvex.ir",
              "https://evil.test/app.menuvex.ir", "https://user@menuvex.ir", "https://menuvex.ir/",
              "null", "", "https://menuvex.ir#"})
            check(!origin_allowed(bad), "foreign origin must be refused");
        auto c = default_config();
        c["printers"].push_back(profile());
        c["routes"].push_back({{"role", "invoice"}, {"printerId", "p1"}, {"autoPrint", true}});
        validate_config(c);
        auto invalid = c;
        invalid["printers"][0]["widthDots"] = 385;
        rejects([&] { validate_config(invalid); });
        auto net = default_config();
        net["printers"].push_back(network_profile());
        net["routes"].push_back({{"role", "invoice"}, {"printerId", "n1"}, {"autoPrint", true}});
        validate_config(net);
        auto &nc = net["printers"][0]["connection"];
        for (auto bad : {std::make_pair(std::string("127.0.0.1"), 9100),
                         {"169.254.169.254", 9100},
                         {"8.8.8.8", 9100},
                         {"224.0.0.1", 9100},
                         {"192.168.1.0", 9100},
                         {"192.168.1.255", 9100},
                         {"172.15.0.1", 9100},
                         {"172.32.0.1", 9100},
                         {"192.168.001.050", 9100},
                         {"192.168.1", 9100},
                         {"localhost", 9100},
                         {"10.0.0.1", 0},
                         {"10.0.0.1", 70000}}) {
            nc["host"] = bad.first;
            nc["port"] = bad.second;
            rejects([&] { validate_config(net); });
        }
        for (const char *good : {"10.0.0.5", "172.16.3.4", "172.31.255.254", "192.168.0.1"}) {
            nc["host"] = good;
            nc["port"] = 9100;
            validate_config(net);
        }
        // Direct USB (in-box "USB Printing Support" driver) uses the modern Agent's wire shape.
        auto usb_config = default_config();
        usb_config["printers"].push_back(profile());
        usb_config["printers"][0]["connection"] = usb_connection(0x0416, 0x5011, "", {1, 4});
        validate_config(usb_config);
        auto &uc = usb_config["printers"][0]["connection"];
        check(uc["serial"].is_null() && uc["bus"] == 0 && uc["ports"] == Json({1, 4}) &&
                  uc["interface"] == 0 && uc["endpoint"] == 0 && uc["alternate"] == 0,
              "USB connection uses the shared wire shape");
        uc["serial"] = "J5UF012345";
        validate_config(usb_config);
        uc.erase("serial");
        validate_config(usb_config);
        for (auto bad : {std::make_pair(std::string("vendorId"), Json(0)),
                         {"vendorId", Json(70000)},
                         {"productId", Json(-1)},
                         {"serial", Json("")},
                         {"serial", Json(7)},
                         {"bus", Json(256)},
                         {"ports", Json({1, 2, 3, 4, 5, 6, 7, 8})},
                         {"ports", Json({"1"})},
                         {"endpoint", Json(-1)}}) {
            auto broken = usb_config;
            broken["printers"][0]["connection"][bad.first] = bad.second;
            rejects([&] { validate_config(broken); }, "INVALID_PAYLOAD");
        }
        auto missing = usb_config;
        missing["printers"][0]["connection"].erase("ports");
        rejects([&] { validate_config(missing); }, "INVALID_PAYLOAD");
        auto unknown_field = usb_config;
        unknown_field["printers"][0]["connection"]["host"] = "192.168.1.50";
        rejects([&] { validate_config(unknown_field); }, "INVALID_PAYLOAD");
        nc["type"] = "lan";
        rejects([&] { validate_config(net); }, "UNSUPPORTED_CONNECTION_TYPE", "\"lan\"");
        nc["type"] = "network";
        nc["extra"] = 1;
        rejects([&] { validate_config(net); });
        nc.erase("extra");
        // printer.save upsert semantics.
        auto one = upsert_printer(default_config(), profile());
        check(one["printers"].size() == 1 && one["printers"][0]["id"] == "p1",
              "upsert adds a printer");
        auto two = upsert_printer(one, network_profile());
        check(two["printers"].size() == 2, "upsert preserves existing printers");
        auto renamed = profile();
        renamed["name"] = "Renamed";
        auto same = upsert_printer(two, renamed);
        check(same["printers"].size() == 2 && same["printers"][0]["name"] == "Renamed",
              "upsert replaces by id without duplicating");
        Json spooler = profile();
        spooler["connection"] = {{"type", "spooler"}, {"queueName", "Kitchen POS"}};
        auto wire_printers = compatible_printers(Json::array({spooler}));
        check(wire_printers[0]["connection"]["type"] == "usb" &&
                  wire_printers[0]["connection"]["vendorId"] == 0 &&
                  wire_printers[0]["connection"]["serial"] == "queue:Kitchen POS",
              "spooler is exposed using the reserved USB-compatible queue descriptor");
        Json client_wire = wire_printers[0];
        client_wire["connection"]["productId"] = 73;
        auto restored = upsert_printer(default_config(), client_wire);
        check(restored["printers"][0]["connection"]["type"] == "spooler" &&
                  restored["printers"][0]["connection"]["queueName"] == "Kitchen POS",
              "USB-compatible printer.save input is normalized to the spooler transport");
        Json test_profile = spooler;
        test_profile["paperMm"] = 58;
        test_profile["widthDots"] = 384;
        auto test_ticket = printer_test_document(test_profile);
        check(test_ticket["lines"][1] == "Paper profile | 58 mm | 384 dots",
              "test ticket reports the configured profile width");
        invoice["data"]["items"][0]["quantity"] = 2;
        auto invoice_lines = printable_lines(invoice, 384);
        check(invoice_lines.front() == std::string("[center] ") + u8"کافه" &&
                  std::find(invoice_lines.begin(), invoice_lines.end(),
                            std::string(u8"شرح کالا | جمع")) != invoice_lines.end(),
              "legacy invoice lines use shared center/column markers and narrow-width layout");
        Json usb = profile();
        usb["connection"] = {{"type", "usb"}, {"vendorId", 1}};
        rejects([&] { upsert_printer(default_config(), usb); }, "INVALID_PAYLOAD");
        usb["connection"] = usb_connection(0x04b8, 0x0202, "J5UF012345", {});
        usb["connection"].erase("serial");
        auto saved_usb = upsert_printer(default_config(), usb);
        check(saved_usb["printers"][0]["connection"]["serial"].is_null(),
              "printer.save stores a USB profile without serial as serial:null");
        check(compatible_printers(Json::array({usb}))[0]["connection"]["serial"].is_null(),
              "printers.list always carries the serial key for USB profiles");
        // Unsupported types are still named, so an operator sees what the client sent.
        Json lan = profile();
        lan["connection"] = {{"type", "lan"}};
        rejects([&] { upsert_printer(default_config(), lan); }, "UNSUPPORTED_CONNECTION_TYPE",
                "\"usb\"");
        // ---- Automatic discovery policy ----
        auto id = parse_usb_instance("USB\\VID_0416&PID_5011\\5&2F1A8C3C&0&1");
        check(id.ok && id.vendor == 0x0416 && id.product == 0x5011 && id.serial.empty(),
              "location-based instance ID has no serial");
        id = parse_usb_instance("usb\\vid_04b8&pid_0202\\J5UF012345");
        check(id.ok && id.vendor == 0x04b8 && id.product == 0x0202 && id.serial == "J5UF012345",
              "device serial is read from the instance ID");
        id = parse_usb_instance("USB\\VID_0FE6&PID_811E&MI_00\\7&1D2C3B4A&0&0000");
        check(id.ok && id.vendor == 0x0fe6 && id.product == 0x811e && id.serial.empty(),
              "composite interface node");
        for (const char *bad : {"", "USB\\VID_0416&PID_5011", "USB\\VID_04G6&PID_5011\\X",
                                "HID\\VID_0416&PID_5011\\X", "USB\\VID_0416&PID_5011&REV_0100\\X",
                                "USB\\VID_0416&PID_5011\\", "USB\\VID_0416&PID_5011\\A\\B"})
            check(!parse_usb_instance(bad).ok, "malformed instance ID rejected");
        check(usb_port_chain("PCIROOT(0)#PCI(1D00)#USBROOT(0)#USB(1)#USB(4)") ==
                  std::vector<int>({1, 4}),
              "USB port chain");
        check(usb_port_chain("PCIROOT(0)#PCI(1400)#USBROOT(0)#USB(2)#USBMI(0)") ==
                  std::vector<int>({2}),
              "port chain stops at the interface");
        check(usb_port_chain("ACPI(_SB_)#ACPI(PCI0)#USB(4)").empty() &&
                  usb_port_chain("PCIROOT(0)#USBROOT(0)#USB(0)").empty() &&
                  usb_port_chain("PCIROOT(0)#USBROOT(0)#USB(x)").empty(),
              "unknown or malformed location path");
        auto device = [](int vid, int pid, std::string serial, std::vector<int> ports) {
            UsbCandidate c;
            c.id.ok = true;
            c.id.vendor = vid;
            c.id.product = pid;
            c.id.serial = std::move(serial);
            c.ports = std::move(ports);
            return c;
        };
        std::vector<UsbCandidate> present = {device(0x0416, 0x5011, "", {1, 4}),
                                             device(0x0416, 0x5011, "", {2}),
                                             device(0x04b8, 0x0202, "J5UF012345", {3})};
        check(select_usb_device(usb_connection(0x0416, 0x5011, "", {2}), present) == 1,
              "identical serial-less printers are told apart by USB port");
        check(select_usb_device(usb_connection(0x04b8, 0x0202, "j5uf012345", {9}), present) == 2,
              "serial wins over a changed USB port");
        rejects([&] { select_usb_device(usb_connection(0x0416, 0x5011, "", {}), present); },
                "USB_AMBIGUOUS");
        rejects([&] { select_usb_device(usb_connection(0x0416, 0x5011, "", {5}), present); },
                "USB_AMBIGUOUS");
        rejects([&] { select_usb_device(usb_connection(0x1234, 0x0001, "", {}), present); },
                "PRINTER_OFFLINE");
        try {
            select_usb_device(usb_connection(0x04b8, 0x0202, "OTHER", {}), present);
            check(false, "a different serial number is a different printer");
        } catch (const Error &e) {
            check(e.code == "PRINTER_OFFLINE" && e.retryable && !e.uncertain,
                  "missing USB printer is retryable and certain (nothing sent)");
        }
        present.resize(1);
        check(select_usb_device(usb_connection(0x0416, 0x5011, "", {7}), present) == 0,
              "a single matching printer follows a cable moved to another port");
        check(queue_kind("POS-80", "USB001", "POS-80C") == "usb" &&
                  queue_kind("Kitchen", "IP_192.168.1.50", "Generic / Text Only") == "network" &&
                  queue_kind("Bar", "192.168.1.51_1", "Generic / Text Only") == "network" &&
                  queue_kind("Epson", "WSD-0b1c2d3e-0001", "EPSON TM-T20") == "wsd" &&
                  queue_kind("Old", "LPT1:", "Generic / Text Only") == "lpt" &&
                  queue_kind("Serial", "COM3:", "Generic / Text Only") == "com" &&
                  queue_kind("\\\\server\\POS", "", "") == "shared" &&
                  queue_kind("Microsoft XPS Document Writer",
                             "XPSPort:", "Microsoft XPS Document Writer") == "virtual" &&
                  queue_kind("Fax", "SHRFAX:", "Microsoft Shared Fax Driver") == "virtual" &&
                  queue_kind("PDF", "PORTPROMPT:", "Microsoft Print To PDF") == "virtual" &&
                  queue_kind("Vendor", "TMUSB001", "EPSON TM-T88V") == "other",
              "Windows queue classification");
        check(port_ipv4("IP_192.168.1.50") == "192.168.1.50" &&
                  port_ipv4("192.168.1.51_2") == "192.168.1.51" &&
                  port_ipv4("10.0.0.7:lpt1") == "10.0.0.7" && port_ipv4("IP_8.8.8.8").empty() &&
                  port_ipv4("USB001").empty() && port_ipv4("192.168.1.50_x").empty(),
              "Standard TCP/IP port address");
        check(guess_paper_mm("POS-58") == 58 && guess_paper_mm("XP-58IIH") == 58 &&
                  guess_paper_mm("POS-5890K") == 58 && guess_paper_mm("Printer POS-80") == 80 &&
                  guess_paper_mm("TM-T88V") == 80 && guess_paper_mm("XP-1580") == 80,
              "paper width guess");
        std::uint32_t ip = 0, mask = 0;
        check(parse_ipv4("192.168.1.20", ip) && format_ipv4(ip) == "192.168.1.20" &&
                  parse_ipv4("255.255.255.0", mask) && !parse_ipv4("192.168.1.20.", ip) &&
                  !parse_ipv4("192.168.01.20", ip) && !parse_ipv4("1.2.3", ip),
              "strict IPv4 parsing");
        std::uint32_t known = 0;
        parse_ipv4("192.168.50.9", known);
        auto targets = scan_targets({{ip, mask}}, {known});
        check(targets.size() == 254 && targets.front() == known &&
                  std::find(targets.begin(), targets.end(), ip) == targets.end(),
              "a /24 is scanned after known printer IPs, never the PC itself");
        std::uint32_t wide_mask = 0, office = 0;
        parse_ipv4("255.255.0.0", wide_mask);
        parse_ipv4("10.20.30.40", office);
        targets = scan_targets({{office, wide_mask}}, {});
        check(targets.size() == 253 && format_ipv4(targets.front()) == "10.20.30.1" &&
                  format_ipv4(targets.back()) == "10.20.30.254",
              "a large subnet is bounded to the PC's /24");
        std::uint32_t small_mask = 0, public_ip = 0, apipa = 0;
        parse_ipv4("255.255.255.192", small_mask);
        parse_ipv4("8.8.8.8", public_ip);
        parse_ipv4("169.254.10.10", apipa);
        targets = scan_targets({{ip, small_mask}, {public_ip, mask}, {apipa, mask}}, {public_ip});
        check(targets.size() == 61 && format_ipv4(targets.front()) == "192.168.1.1" &&
                  format_ipv4(targets.back()) == "192.168.1.62",
              "only private hosts of the PC's own subnet");
        check(scan_targets({{ip, mask}}, {}, 10).size() == 10, "scan size is bounded");
        Json saved = Json::array({profile(), network_profile()});
        saved[0]["connection"] = usb_connection(0x0416, 0x5011, "", {1, 4});
        check(configured_name(saved, usb_connection(0x0416, 0x5011, "", {1, 4})) == "Test" &&
                  configured_name(saved, usb_connection(0x0416, 0x5011, "", {2})).empty() &&
                  configured_name(
                      saved, {{"type", "network"}, {"host", "192.168.1.50"}, {"port", 9100}}) ==
                      "LAN" &&
                  configured_name(saved, {{"type", "spooler"}, {"queueName", "x"}}).empty(),
              "discovered printers that are already saved are recognised");
        auto jalali_is = [](int gy, int gm, int gd, int y, int m, int d) {
            int jy = 0, jm = 0, jd = 0;
            jalali(gy, gm, gd, jy, jm, jd);
            return jy == y && jm == m && jd == d;
        };
        check(jalali_is(2026, 10, 13, 1405, 7, 21) && jalali_is(2024, 3, 20, 1403, 1, 1) &&
                  jalali_is(2025, 3, 21, 1404, 1, 1) && jalali_is(2026, 3, 20, 1404, 12, 29) &&
                  jalali_is(2000, 1, 1, 1378, 10, 11),
              "Jalali dates for the Persian queue view");
        auto full = default_config();
        for (int i = 0; i < 16; ++i) {
            Json p = profile();
            p["id"] = "p" + std::to_string(i);
            full["printers"].push_back(p);
        }
        Json extra = profile();
        extra["id"] = "p99";
        rejects([&] { upsert_printer(full, extra); }, "INVALID_CONFIG");
        std::vector<unsigned char> pixels(48 * 2, 0x80);
        auto bytes = raster(384, 2, pixels, true);
        check(bytes[0] == 27 && bytes[1] == 64 && bytes[2] == 29 && bytes.back() == 0,
              "raster header and cut");
        rejects([] { raster(384, 2, {}, true); });
        check(backoff(1) == 2 && backoff(100) == 60, "backoff bounds");
        const std::string file = "legacy-core-test.sqlite3";
        std::remove(file.c_str());
        {
            Store s(file);
            s.save(c);
            s.enqueue("order:1", "p1", receipt());
            s.enqueue("order:1", "p1", receipt());
            check(s.queue().size() == 1, "persistent dedup");
            auto changed = receipt();
            changed["lines"][0] = "different";
            rejects([&] { s.enqueue("order:1", "p1", changed); }, "JOB_ID_CONFLICT");
            auto w = s.claim(now());
            check(w["job"]["attempts"] == 1, "atomic claim");
            s.finish("order:1", Error("PRINTER_OFFLINE", "Offline", true).json(), 3, now());
            check(s.claim(now()).is_null(), "backoff respected");
            check(!s.claim(now() + 10).is_null(), "retry becomes due");
        }
        {
            Store s(file);
            auto j = s.get("order:1");
            check(j["status"] == "failed" && j["error"]["uncertain"] == true,
                  "crash printing becomes unknown");
            check(s.enqueue("order:1", "p1", receipt())["status"] == "failed",
                  "failed ID not silently retried");
            s.enqueue("order:2", "p1", receipt());
            s.claim(now());
            s.finish("order:2", nullptr, 3, now());
            check(s.enqueue("order:2", "p1", receipt())["status"] == "completed",
                  "completed dedup");
            s.enqueue("order:3", "p1", receipt());
            check(s.cancel("order:3")["status"] == "cancelled", "cancel queued");
            rejects([&] { s.cancel("order:2"); }, "JOB_NOT_CANCELLABLE");
            s.enqueue("order:4", "p1", receipt());
            s.claim(now());
            s.finish("order:4", unknown().json(), 5, now());
            check(s.get("order:4")["status"] == "failed", "ambiguous errors never retry");
            std::atomic<int> failures{0};
            std::vector<std::thread> threads;
            for (int i = 0; i < 8; ++i)
                threads.emplace_back([&] {
                    try {
                        s.enqueue("concurrent:1", "p1", receipt());
                    } catch (...) {
                        ++failures;
                    }
                });
            for (auto &t : threads)
                t.join();
            check(failures == 0, "concurrent enqueue");
            check(s.queue().size() == 5, "one concurrent row");
        }
        std::remove(file.c_str());
        std::cout << checks << " checks passed\n";
        return 0;
    } catch (const std::exception &e) {
        std::cerr << "FAILED: " << e.what() << "\n";
        return 1;
    }
}
