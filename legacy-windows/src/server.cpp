#include "server.hpp"
#include <map>
#include <websocketpp/config/asio_no_tls.hpp>
#include <websocketpp/server.hpp>
namespace mv {
using Ws = websocketpp::server<websocketpp::config::asio>;
using Hdl = websocketpp::connection_hdl;
struct Server::Impl {
    Store &store;
    Secret &secret;
    std::function<void()> changed;
    Ws ws;
    std::thread thread;
    std::atomic<bool> ready{false}, worker_ok{true};
    bool stopping = false;
    struct Session {
        std::string origin, nonce;
        bool auth = false;
        std::int64_t opened = now(), last = now(), window = now();
        unsigned commands = 0;
    };
    std::map<Hdl, Session, std::owner_less<Hdl>> clients;
    Impl(Store &s, Secret &k, std::function<void()> f)
        : store(s), secret(k), changed(std::move(f)) {
        ws.clear_access_channels(websocketpp::log::alevel::all);
        ws.clear_error_channels(websocketpp::log::elevel::all);
        ws.init_asio();
        ws.set_max_message_size(128 * 1024);
        ws.set_open_handshake_timeout(5000);
        ws.set_close_handshake_timeout(2000);
        ws.set_pong_timeout(5000);
        ws.set_validate_handler([this](Hdl h) {
            auto c = ws.get_con_from_hdl(h);
            auto origin = c->get_request_header("Origin");
            bool allowed = origin == "https://menuvex.ir" || origin == "https://www.menuvex.ir";
#ifdef _DEBUG
            allowed =
                allowed || origin == "http://localhost:5173" || origin == "http://127.0.0.1:5173";
#endif
            auto host = c->get_request_header("Host");
            websocketpp::lib::error_code ec;
            auto port = ws.get_local_endpoint(ec).port();
            if (stopping || !allowed || c->get_resource() != "/" ||
                host != "127.0.0.1:" + std::to_string(port) || clients.size() >= 8) {
                c->set_status(websocketpp::http::status_code::forbidden);
                return false;
            }
            clients.emplace(h, Session{origin, random_base64()});
            return true;
        });
        ws.set_open_handler([this](Hdl h) {
            try {
                auto &s = clients.at(h);
                send(h, {{"type", "hello"},
                         {"version", 1},
                         {"agentVersion", "1.0.0"},
                         {"authentication", "hmac-sha256"},
                         {"nonce", s.nonce},
                         {"serverProof", secret.proof(s.nonce, s.origin, true)}});
            } catch (...) {
                close(h);
            }
        });
        ws.set_close_handler([this](Hdl h) { clients.erase(h); });
        ws.set_fail_handler([this](Hdl h) { clients.erase(h); });
        ws.set_pong_handler([this](Hdl h, const std::string &) {
            auto it = clients.find(h);
            if (it != clients.end())
                it->second.last = now();
        });
        ws.set_message_handler([this](Hdl h, Ws::message_ptr m) { message(h, m); });
    }
    void close(Hdl h) {
        websocketpp::lib::error_code ec;
        ws.close(h, websocketpp::close::status::policy_violation, "Connection closed", ec);
    }
    void send(Hdl h, const Json &value) {
        websocketpp::lib::error_code ec;
        auto c = ws.get_con_from_hdl(h, ec);
        if (ec)
            return;
        if (c->get_buffered_amount() > 512 * 1024) {
            close(h);
            return;
        }
        ws.send(h, value.dump(), websocketpp::frame::opcode::text, ec);
    }
    Json printers() {
        Json result = store.config()["printers"];
        for (auto &p : result)
            p["status"] = "unknown";
        auto jobs = store.queue();
        for (const auto &j : jobs)
            if (j["status"] == "printing")
                for (auto &p : result)
                    if (j["printerId"] == p["id"])
                        p["status"] = "busy";
        return result;
    }
    Json dispatch(const Json &r) {
        auto type = r["type"].get<std::string>();
        if (type == "ping" || type == "hello")
            return {{"version", 1}, {"agentVersion", "1.0.0"}};
        if (type == "agent.status") {
            auto c = store.config();
            return {{"ready", ready.load() && worker_ok.load()},
                    {"agentVersion", "1.0.0"},
                    {"port", c["port"]},
                    {"routes", c["routes"]},
                    {"serverError", worker_ok.load()
                                        ? Json(nullptr)
                                        : Json("QUEUE_ERROR: restart and inspect pending jobs")}};
        }
        if (type == "printers.list")
            return printers();
        if (type == "printer.get") {
            for (auto &p : printers())
                if (p["id"] == r["printerId"])
                    return p;
            throw Error("PRINTER_NOT_FOUND", "Printer not configured");
        }
        if (type == "queue.list")
            return store.queue();
        if (type == "print.status")
            return store.get(r["jobId"]);
        if (type == "queue.cancel") {
            auto job = store.cancel(r["jobId"]);
            broadcast_job(job);
            return job;
        }
        if (type == "print" || type == "printer.test") {
            if (!worker_ok)
                throw Error("AGENT_NOT_READY", "Print worker stopped; reconcile and restart");
            Json doc =
                type == "print"
                    ? r["document"]
                    : Json({{"type", "receipt"},
                            {"lines",
                             {u8"آزمون چاپ فارسی — سلام دنیا", "MenuVex Legacy / Windows RAW",
                              u8"0123456789 / ۱۲۳۴۵۶۷۸۹۰"}}});
            auto job = store.enqueue(r["jobId"], r["printerId"], doc);
            broadcast_job(job);
            return job;
        }
        if (type == "agent.shutdown")
            throw Error("LOCAL_CONFIRMATION_REQUIRED", "Use the local system tray to quit");
        throw Error("ALREADY_AUTHENTICATED", "Handshake already completed");
    }
    void broadcast(const Json &e) {
        for (auto &entry : clients)
            if (entry.second.auth)
                send(entry.first, e);
        changed();
    }
    void broadcast_job(const Json &j) {
        broadcast(
            {{"type", "print." + j["status"].get<std::string>()}, {"version", 1}, {"job", j}});
    }
    void message(Hdl h, Ws::message_ptr m) {
        Json request;
        auto it = clients.find(h);
        if (it == clients.end()) {
            close(h);
            return;
        }
        auto &s = it->second;
        try {
            if (m->get_opcode() != websocketpp::frame::opcode::text)
                throw Error("INVALID_PAYLOAD", "Text frames only");
            if (now() != s.window) {
                s.window = now();
                s.commands = 0;
            }
            if (++s.commands > 30) {
                close(h);
                return;
            }
            s.last = now();
            request = parse_request(m->get_payload());
            if (!s.auth) {
                if (now() - s.opened > 10 || request["type"] != "authenticate" ||
                    !secret.verify(s.nonce, s.origin, request["proof"]))
                    throw Error("AUTH_FAILED", "Pair this browser using the local Agent settings");
                s.auth = true;
                s.nonce.clear();
                send(h, {{"type", "authenticated"},
                         {"version", 1},
                         {"requestId", request["requestId"]},
                         {"agentVersion", "1.0.0"}});
                return;
            }
            auto value = dispatch(request);
            send(h, {{"type", "response"},
                     {"version", 1},
                     {"requestId", request["requestId"]},
                     {"data", value}});
        } catch (const Error &error) {
            Json value = {{"type", "error"}, {"version", 1}, {"error", error.json()}};
            if (request.contains("requestId"))
                value["requestId"] = request["requestId"];
            send(h, value);
            if (!s.auth || error.code == "INVALID_PAYLOAD")
                close(h);
        } catch (...) {
            send(h, {{"type", "error"},
                     {"version", 1},
                     {"error", Error("AGENT_NOT_READY", "Internal request failure").json()}});
            close(h);
        }
    }
    void tick() {
        if (stopping)
            return;
        ws.set_timer(1000, [this](websocketpp::lib::error_code ec) {
            if (ec || stopping)
                return;
            for (auto &entry : clients) {
                auto &s = entry.second;
                if ((!s.auth && now() - s.opened > 10) || now() - s.last > 60)
                    close(entry.first);
                else if (s.auth && now() % 15 == 0) {
                    websocketpp::lib::error_code ignored;
                    ws.ping(entry.first, "", ignored);
                }
            }
            tick();
        });
    }
};
Server::Server(Store &store, Secret &secret, std::function<void()> changed)
    : impl_(new Impl(store, secret, std::move(changed))) {}
Server::~Server() { stop(); }
void Server::start(unsigned short port) {
    auto &i = *impl_;
    websocketpp::lib::error_code ec;
    i.ws.listen(asio::ip::tcp::endpoint(asio::ip::address_v4::loopback(), port), ec);
    if (ec)
        throw Error(
            "PORT_IN_USE_OR_UNAVAILABLE",
            "Local port unavailable. Quit the modern Agent or choose another port in settings.");
    i.ws.start_accept();
    i.ready = true;
    i.tick();
    i.thread = std::thread([this] {
        try {
            impl_->ws.run();
        } catch (...) {
            impl_->ready = false;
        }
        impl_->ready = false;
    });
}
void Server::stop() {
    if (!impl_ || !impl_->thread.joinable())
        return;
    auto *i = impl_.get();
    i->ws.get_io_service().post([i] {
        i->stopping = true;
        i->ready = false;
        websocketpp::lib::error_code ec;
        i->ws.stop_listening(ec);
        for (auto &entry : i->clients)
            i->close(entry.first);
        i->ws.set_timer(2200, [i](websocketpp::lib::error_code) { i->ws.stop(); });
    });
    i->thread.join();
}
void Server::event(Json e) {
    auto *i = impl_.get();
    i->ws.get_io_service().post([i, e] { i->broadcast(e); });
}
void Server::revoke() {
    auto *i = impl_.get();
    i->ws.get_io_service().post([i] {
        for (auto &entry : i->clients) {
            entry.second.auth = false;
            i->close(entry.first);
        }
    });
}
unsigned short Server::port() const {
    websocketpp::lib::error_code ec;
    return impl_->ws.get_local_endpoint(ec).port();
}
bool Server::ready() const { return impl_->ready; }
void Server::worker_failed() {
    impl_->worker_ok = false;
    event({{"type", "resync"}, {"version", 1}});
}
} // namespace mv
