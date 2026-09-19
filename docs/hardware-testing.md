# Hardware/browser acceptance checklist — not yet executed

Record build commit/version, signature, OS version/architecture, browser version, firmware/model, driver, USB VID/PID/interface/serial/topology, IP/port and profile for every run. Use controlled test orders, never seeded production data. Observe actual paper, job DB state and event sequence.

| #   | Scenario                             | Expected result                                                                                                                                 |
| --- | ------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | USB connected at launch              | Enumerated Printer Class bulk OUT endpoint, correct identity; local test prints Persian.                                                        |
| 2   | USB disconnected before send         | Status offline within monitor interval; queued job retries before send only.                                                                    |
| 3   | USB reconnected                      | Same serial/topology returns online; eligible queued job resumes once. Move serial-less device to new port to confirm re-selection requirement. |
| 4   | LAN online                           | Connect probe then real print, correct port/profile, socket closed after handoff.                                                               |
| 5   | LAN offline                          | Bounded connection timeout, backoff, attempts capped; queue persists.                                                                           |
| 6   | LAN IP changed                       | No subnet scan. Offline reported; local config updated. Existing queued job keeps old snapshot: cancel/reconcile before explicit new job.       |
| 7   | Agent restart                        | Queued jobs survive, completed IDs do not replay, active `printing` becomes ambiguous if hard-killed. Graceful restart drains current transfer. |
| 8   | Computer restart                     | Optional login launch works; keychain unlocked; queued jobs restored. Hard shutdown never blindly replays `printing`.                           |
| 9   | PWA refresh                          | IndexedDB key loads, fresh challenge auth, list/queue refresh, same ID dedup; no WebUSB chooser.                                                |
| 10  | Internet disconnected                | Already-loaded offline-capable PWA and Agent still communicate locally. Backend order creation itself depends on the real PWA offline policy.   |
| 11  | Duplicate request                    | Concurrent tabs, reconnect resend and post-restart duplicate cause exactly one handoff for same Agent DB. Different payload same ID rejected.   |
| 12  | Multiple jobs/printers               | Per-printer order preserved, other printer can proceed during retry backoff; no interleaved bytes; queue cancel race is safe.                   |
| 13  | Persian invoice                      | Joined letters, RTL, mixed Latin/SKU/numbers, punctuation, currency, long names and wrapping checked on paper.                                  |
| 14  | 58mm                                 | Correct model's dots (often 384), no clipping, valid feed/cutter.                                                                               |
| 15  | 80mm                                 | Correct model's dots (often 576, not assumed), no clipping.                                                                                     |
| 16  | Pull USB/cable mid-transfer          | `PRINT_OUTCOME_UNKNOWN`; no automatic repeat. Count physical partial/full copies.                                                               |
| 17  | Disk full / DB write error           | No handoff before durable claim; stop worker if final state persistence fails; reconcile after restart.                                         |
| 18  | Rogue origin / Host / token / replay | Upgrade rejected or auth failed; no printer/queue/secret leakage.                                                                               |
| 19  | Slow/oversized/flooding WS           | Deadlines, limits, close; native UI and active queue remain usable.                                                                             |
| 20  | Rotate pairing secret                | Old key rejected, active sessions revoked within heartbeat bound, re-pair explicit.                                                             |
| 21  | Browser CSP/local-network policy     | Test real https://menuvex.ir across supported browsers; clear guidance, no insecure flags.                                                      |
| 22  | Closed window / tray / login         | Hide rather than exit; all tray actions work; autostart toggle persists.                                                                        |
| 23  | First install / upgrade / uninstall  | Nonadmin runtime, driver/udev setup, signing/quarantine, shortcuts and login cleanup verified.                                                  |
| 24  | Copies and failure on second copy    | Ambiguous failure does not duplicate earlier copies.                                                                                            |

Targets: Windows 10/11 x64, Ubuntu/Debian desktops, macOS Intel + Apple Silicon. A passing fake/test transport is not a substitute for these cases. Benchmark idle CPU/RSS/startup over at least 30 minutes; current code makes no measured low-memory/performance certification.
