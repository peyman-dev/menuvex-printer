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
template <class F> void rejects(F f, const char *code = nullptr) {
    try {
        f();
    } catch (const Error &e) {
        check(!code || e.code == code, "Unexpected error code");
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
        nc["type"] = "usb";
        rejects([&] { validate_config(net); }, "INVALID_CONFIG");
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
        rejects([&] { upsert_printer(default_config(), usb); }, "INVALID_CONFIG");
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
