#include "server.hpp"
#include <atomic>
#include <cstdlib>
#include <iostream>
using namespace mv;
int main() {
    try {
        wchar_t temp[MAX_PATH], file[MAX_PATH];
        if (!GetTempPathW(MAX_PATH, temp) || !GetTempFileNameW(temp, L"mvx", 0, file))
            return 1;
        struct Cleanup {
            std::wstring file;
            ~Cleanup() {
                DeleteFileW(file.c_str());
                DeleteFileW((file + L"-wal").c_str());
                DeleteFileW((file + L"-shm").c_str());
            }
        } cleanup{file};
        Store store(utf8(file));
        Secret secret(std::vector<unsigned char>(32, 7));
        load_font();
        Json config = default_config();
        config["printers"].push_back(
            {{"id", "integration-printer"},
             {"name", "Test-only transport"},
             {"connection", {{"type", "spooler"}, {"queueName", "Test-only queue"}}},
             {"paperMm", 80},
             {"widthDots", 576},
             {"copies", 1},
             {"cut", true},
             {"fontFamily", "Noto Sans Arabic"},
             {"fontSize", 24}});
        store.save(config);
        if (!secret.verify("nonce", "https://menuvex.ir",
                           secret.proof("nonce", "https://menuvex.ir", false)))
            return 2;
        if (secret.verify("different", "https://menuvex.ir",
                          secret.proof("nonce", "https://menuvex.ir", false)))
            return 3;
        Server server(store, secret, [] {});
        server.start(0);
        std::atomic<bool> stop{false};
        std::atomic<int> count{0}, failure{0};
        std::thread worker([&] {
            try {
                while (!stop) {
                    auto work = store.claim(now());
                    if (!work.is_null()) {
                        auto bytes = render(work["profile"], work["document"]);
                        if (bytes.size() < 100 || bytes[0] != 27 || bytes[1] != 64)
                            ++failure;
                        ++count;
                        auto job = store.finish(work["job"]["jobId"], nullptr, 3, now());
                        server.event({{"type", "print.completed"}, {"version", 1}, {"job", job}});
                    }
                    Sleep(20);
                }
            } catch (...) {
                ++failure;
            }
        });
        _putenv_s("MENUVEX_TEST_PORT", std::to_string(server.port()).c_str());
        _putenv_s("MENUVEX_TEST_SECRET", secret.reveal().c_str());
        int result = std::system("npm exec -- vitest run sdk/tests/live.test.ts");
        stop = true;
        worker.join();
        server.stop();
        _putenv_s("MENUVEX_TEST_PORT", "");
        _putenv_s("MENUVEX_TEST_SECRET", "");
        if (result != 0 || count != 1 || failure != 0) {
            std::cerr << "Legacy SDK integration failed\n";
            return 4;
        }
        std::cout << "Real SDK/WS/HMAC/SQLite/Uniscribe with test-only handoff passed\n";
        return 0;
    } catch (const std::exception &e) {
        std::cerr << e.what() << "\n";
        return 1;
    }
}
