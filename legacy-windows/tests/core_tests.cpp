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
