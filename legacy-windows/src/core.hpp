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
/// Progress of the isolated spooler child, as reported in its `payload-*.status` file.
struct SpoolReport {
    int stage = 0;
    int win32 = 0;
};
SpoolReport spool_report_parse(const std::string &text);
/// Classify a spooler child result. `stage` is the last progress marker the child wrote, so a
/// failure before the first WritePrinter is retryable and never claims an unknown outcome.
Error spool_failure(int exit_code, const SpoolReport &report, bool timed_out = false);
bool valid_id(const std::string &s);
void fields(const Json &j, std::initializer_list<const char *> required,
            std::initializer_list<const char *> optional = {});
void document(const Json &j);
Json parse_request(const std::string &text);
void validate_config(const Json &config);
Json upsert_printer(const Json &config, const Json &printer);
Json default_config();
std::vector<std::string> lines(const Json &doc);
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
