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
    void start(unsigned short port);
    void stop();
    void event(Json value);
    void revoke();
    bool ready() const;
    unsigned short port() const;
    void worker_failed();
};
} // namespace mv
