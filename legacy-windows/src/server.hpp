#pragma once
#include "platform.hpp"
#include <atomic>
#include <condition_variable>
#include <memory>
#include <thread>
namespace mv {
class Server {
    struct Impl;
    std::unique_ptr<Impl> impl_;

  public:
    Server(Store &store, Secret &secret, std::function<void()> changed);
    ~Server();
    // Called on the network thread whenever a browser connection is refused BEFORE the WebSocket
    // opens (foreign Origin, wrong Host, wrong path, too many clients). A browser reports every
    // such refusal as one generic connection error, so the native window is the only place the
    // operator can learn why. `code` is a stable ASCII code; `detail` is a sanitized, bounded
    // copy of the offending request value (never a key, proof or nonce). Set before start().
    void
    on_rejected(std::function<void(const std::string &code, const std::string &detail)> callback);
    void start(unsigned short port);
    void stop();
    void event(Json value);
    void revoke();
    bool ready() const;
    unsigned short port() const;
    void worker_failed();
};
} // namespace mv
