# Development and test evidence

## Environment

Node 22 / npm; Rust stable (minimum declared 1.85; resolved dependencies may require a newer stable); C toolchain for bundled libusb/SQLite; Linux pkg-config, DBus and GTK/WebKit desktop packages. `src-tauri` is a standalone Cargo package. No root Cargo workspace is needed. `desktop` is the default feature; disable it for core tests.

```sh
npm ci
npm test
npm run build
npm run test:rust
npm run tauri -- dev
```

Disable autostart in development Settings to avoid registering a transient `target/debug` executable. Debug WS origins are only localhost/127.0.0.1:5173. The Agent's own Vite UI uses 1420 and native IPC, not this PWA origin whitelist. Do not add the hosted preview domain to the hardware bridge's production allowlist.

## Tests

- `sdk/tests/client.test.ts`: isolated test-only socket, real WebCrypto and fake IndexedDB test backend. Auth, single persistent connection, reconnect refresh, duplicate coalescing, ID conflict, subscriptions, control-byte rejection, key persistence/non-extractability, origin binding, durable migration assignment and no fallback after uncertain sends.
- Rust inline unit tests: strict protocol, origins/HMAC replay, network target validation, ESC/POS bytes/raster dimensions, config bounds, queue persistence, dedup, conflict, retry/backoff/cancel, crash recovery, test-only transport pipeline.
- `server::integration::sdk_to_real_agent`: real loopback WebSocket, actual Rust auth, SQLite, actual Persian renderer and a `#[cfg(test)]` counting transport. It launches `sdk/tests/live.test.ts` through npm; the **real SDK** submits and repeats a job; the test asserts one transport handoff. No keychain/hardware is required for this test. `npm ci` must precede Cargo tests.
- `npm test` alone intentionally skips the live test unless the Rust harness supplies an ephemeral port/secret. Never interpret that skip as an integration pass. Test secrets are scoped to the test child environment; production does not support environment-based key overrides.

## Evidence from this session (2026-09-17)

| Check                                   | Result                                                                          |
| --------------------------------------- | ------------------------------------------------------------------------------- |
| Initial whole repository inspection     | Complete, only README present                                                   |
| `npm ci` / dependency install           | Successful                                                                      |
| `npm run build`                         | TypeScript + Vite production UI passed                                          |
| `npm test`                              | 17 passed; live Rust integration skipped                                        |
| `npm audit`                             | Zero reported vulnerabilities at check time                                     |
| `npm run tauri -- icon assets/icon.svg` | Desktop icons generated successfully                                            |
| Rust parse/format pass                  | Syntax parsed by prettier-plugin-rust; **not a type check**                     |
| `npm run tauri -- info`                 | Reports missing Rust/Cargo, WebKit/GTK prerequisites                            |
| Rust installation / apt dependencies    | Blocked by TLS/network failures to toolchain, crates index and Debian endpoints |
| Rust compilation/unit/integration tests | Not run                                                                         |
| Windows/Linux/macOS installers          | Not built locally; candidate CI provided, not executed                          |
| Real printer / installed PWA / signing  | Not tested; required release gates                                              |

The first successful Cargo resolution must produce a reviewed `src-tauri/Cargo.lock`. Commit that lock and use `--locked` for subsequent release builds. Do not fabricate one without resolving the real dependency graph. The inactive CI template at `docs/ci/checks.yml` includes native compilation/test steps rather than skipping them. An authorized maintainer must activate it under `.github/workflows/checks.yml`; the current GitHub connection lacks workflow-write permission.

## Formatting

`npx prettier --write src sdk docs README.md '*.json' vite.config.ts` for TypeScript/CSS/docs; `cargo fmt --manifest-path src-tauri/Cargo.toml` when Rust is available. Rust in this checkout was formatted with an external JS Rust parser because rustfmt was unavailable; normalize with rustfmt in the native verification pass.

## Subsequent operator-reported native verification

The owner provided terminal output showing 18/18 Rust tests passing on Linux x86_64, including `server::integration::sdk_to_real_agent`, after commit `fddc83f`. A desktop screenshot also shows the Agent ready. These are owner-reported results, not tests rerun in this sandbox. Windows/macOS builds, installed-package tests, signing and physical printer output remain unverified.
