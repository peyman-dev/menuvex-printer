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
