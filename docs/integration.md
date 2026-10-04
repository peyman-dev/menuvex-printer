# Integrating the actual MenuVex PWA (separate repository)

## Required next inspection

Before editing MenuVex: find existing printer abstraction, WebUSB implementation, invoice data mapping, order creation/order WebSocket subscriber, persistent settings, auth/CSP configuration, routes, download page and UI conventions. Nothing in this dedicated Agent repo establishes those contracts. Keep existing UI/order WebSocket code and dependencies unless a minimal adapter needs them.

Place/re-export `sdk/src/*` under the established PWA service location, e.g. `src/lib/printer/`. Add the SDK's `zod` dependency using that repo's package manager, not automatically converting it to npm. Use one `printerAgent` singleton or one custom client at composition root for a nondefault port. Optional `sdk/react/PrinterAgentProvider.tsx` provides status and client; component code should not instantiate WebSockets.

## Pairing and refresh

A real pairing page calls `await client.pair(secret)` after the operator reveals the key in the Agent. Clear the input immediately, do not send it to analytics/server logs. PWA `connect()` after refresh loads its non-extractable IndexedDB CryptoKey. If unavailable: show “Agent unreachable — may not be installed, running, permitted or on this port”, retry/setup/download actions, not a false definitive uninstall diagnosis. If `unauthorized`: show pairing/revoke instructions, **no legacy fallback**.

Agent contains printer profiles and routes. PWA reads `getStatus().routes` after connect and on configuration resync when relevant; SDK refreshes printer/queue snapshots automatically. Use stable order-derived IDs including role/profile purpose, e.g. `order:<backend-ID>:invoice`. Never a timestamp/random ID for retries. Deliberate reprints use a separately audited suffix only after inspecting output.

Do not claim “printed” on `print()` resolve. The result is durably queued/existing. Display `printing`, `completed` (sent), or failed/unknown via events/query. If response is lost, check `getJob(id)` and resubmit only the identical request under that same ID. Dedup is in Agent SQLite, not a browser Set.

## Legacy migration

`BrowserWebUSBPrinter` is an adapter accepting the real existing print callback; it is not a second WebUSB implementation and never calls `navigator.usb.requestDevice()`. `MenuVexAgentPrinter` delegates to the SDK. `MigratingPrinterProvider` chooses Agent first, with legacy fallback only for initial `AGENT_UNAVAILABLE`, explicitly enabled by application policy, **before** any submission.

Routing is durably assigned per job in browser IndexedDB before invoking either transport. A previously Agent-owned ID never moves to legacy during an outage. Legacy IDs are not automatically replayed because its delivery outcome/dedup semantics are unknown. Atomic IndexedDB claims prevent two same-profile tabs both starting legacy for an ID. Uncertain Agent timeouts/auth failures never fall back.

This ledger is scoped to one browser origin/profile. Cross-browser or multi-cashier automatic printing requires the real backend to designate one print owner and share stable routing decisions; local Agent dedup cannot coordinate independent Agents or a WebUSB transport in another profile. Keep automatic legacy fallback disabled until that ownership policy is established. Clearing site data removes the browser routing ledger; existing physical outcomes remain unknown.

## Download route

Implement `/download/print-agent` in MenuVex's existing router only after real signed assets exist. Platform buttons should reference actual released, checksummed artifacts, version/architecture, and signing status. Do not generate fabricated GitHub release URLs. Simple Persian instructions: نصب کنید → Agent را باز کنید → پرینتر را یک‌بار انتخاب و آزمایش کنید → مرورگر را جفت کنید. Explain Windows driver setup and Linux administrator-assisted USB permissions; do not promise every model is zero-setup.

The standalone UI preview is not this download page and cannot inspect the cashier's local hardware through the hosted sandbox.
