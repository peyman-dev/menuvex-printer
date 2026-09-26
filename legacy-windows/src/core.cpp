#include "core.hpp"
#include <algorithm>
#include <chrono>
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
// Literal RFC1918 unicast IPv4 only (no DNS, loopback, link-local, multicast or public), matching
// the modern Agent's network rules. Rejects leading zeros so the value round-trips through
// InetPtonA on the transport side.
static bool private_ipv4(const std::string &s) {
    int octets[4];
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
        }
    }
    if (count != 4)
        return false;
    bool private_range = octets[0] == 10 ||
                         (octets[0] == 172 && octets[1] >= 16 && octets[1] <= 31) ||
                         (octets[0] == 192 && octets[1] == 168);
    return private_range && octets[3] != 0 && octets[3] != 255;
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
        fields(d, {"storeName", "orderNumber", "items", "total"}, {"footer"});
        require(text(d["storeName"], 300) && text(d["orderNumber"], 128) &&
                    (!d.contains("footer") || text(d["footer"], 1000)),
                "Invalid invoice text");
        require(number(d["total"], 0, 9000000000000LL), "Invalid total");
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
            type == "printers.list" || type == "queue.list" || type == "agent.shutdown")
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
        // Legacy supports installed Windows spooler queues and direct LAN TCP/9100, but not
        // invented USB VID/PID records.
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
        } else {
            throw Error("INVALID_CONFIG", "Connection type must be spooler or network");
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
std::vector<std::string> lines(const Json &doc) {
    document(doc);
    if (doc["type"] == "receipt")
        return doc["lines"].get<std::vector<std::string>>();
    const auto &d = doc["data"];
    std::vector<std::string> out = {d["storeName"],
                                    std::string(u8"سفارش: ") + d["orderNumber"].get<std::string>(),
                                    "----------------"};
    for (const auto &i : d["items"]) {
        out.push_back(i["name"]);
        out.push_back(
            std::to_string(i["quantity"].get<int>()) + " x " +
            std::to_string(i["unitPrice"].get<std::int64_t>()) + " = " +
            std::to_string(i["quantity"].get<std::int64_t>() * i["unitPrice"].get<std::int64_t>()));
    }
    out.push_back("----------------");
    out.push_back(std::string(u8"جمع: ") + std::to_string(d["total"].get<std::int64_t>()));
    out.push_back(d.value("footer", std::string{}));
    return out;
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
} // namespace mv
