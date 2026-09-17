# Third-party software used by the Legacy agent

- nlohmann/json 3.11.3, MIT, https://github.com/nlohmann/json — immutable commit in CMakeLists.txt.
- SQLite 3.53.4, public domain, https://sqlite.org — amalgamation from pinned SQLiteCpp repository commit. The SQLiteCpp C++ wrapper is not linked.
- WebSocket++ 0.8.2, BSD-3-Clause, https://github.com/zaphoyd/websocketpp.
- Asio 1.30.2, Boost Software License 1.0, https://github.com/chriskohlhoff/asio.
- Noto Sans Arabic Regular, SIL Open Font License 1.1. Unmodified font embedded in the executable. Full license is included as OFL-NotoSansArabic.txt.

The build script collects full dependency licenses into THIRD-PARTY-LICENSES.txt in the installer output. Dependency selection is not a security certification; review current advisories before signing/public release. No vendor printer drivers are redistributed.
