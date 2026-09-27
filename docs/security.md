# Security model

## Trust boundary

The local logged-in OS user and the bundled Agent webview are trusted. The PWA is privileged to print **only after explicit pairing**. Internet pages, cross-origin pages, missing-Origin clients and unauthenticated localhost clients are untrusted. Local malware running as the same user can often read OS credentials or inject into processes; this design is not a defense against a compromised OS account. Host/Origin checks alone are not authentication: native clients can spoof both.

Production allows only the two exact HTTPS MenuVex origins. Debug-only localhost PWA origins are hardcoded narrowly. Agent does not bind IPv6 or LAN interfaces. No `0.0.0.0` listener, HTTP file server, raw TCP server, device path, host override or arbitrary DNS resolution is exposed to the PWA. LAN IP/port selection is a deliberate local operator action; only RFC1918 unicast literal IPv4 is accepted (also excludes .0/.255 conservatively). That does not prove a service is a printer; the operator must select a real RAW printer.

## Pairing

OS secret: 32 bytes from `OsRng`, persisted through keyring 3.x Windows Credential Manager / macOS Keychain / Linux Secret Service. Fail closed if storage is locked or unavailable, including corruption; no plaintext file fallback. Secret shown only on an explicit native UI action, cleared from UI after 60 seconds or tab unmount. Rotation requires explicit confirmation, persists first, then revokes old sessions. Secret is never logged, placed in a URL, or included in ordinary protocol responses.

PWA: copy the secret **only into an official pairing form**, import into WebCrypto as non-extractable HMAC/SHA-256 key, structured-clone the key into origin-scoped IndexedDB, clear the form immediately. Subsequent refreshes use that key and a fresh nonce. Different browser profiles, cleared site data or storage eviction need pairing again. `www` and apex are separate storage origins. No localStorage token. Incognito is not suitable for persistent POS setup.

A non-extractable key prevents ordinary `exportKey` retrieval; it is not equivalent to a hardware-backed browser keychain. Same-origin XSS can invoke signing or print through the SDK. Harden the real PWA CSP, third-party scripts, dependency integrity, admin permissions and session locking. The master key currently grants all configured printers, with global rather than per-browser revocation. Multi-tenant/per-device grants need a protocol/storage extension before using shared OS accounts across unrelated merchants.

The server proves possession in its hello using a separate HMAC domain; the PWA verifies before authenticating. This reduces rogue-listener impersonation. A compromised same-user local process capable of relaying or reading credentials remains outside the trust boundary.

## Browser HTTPS → loopback

Browser policy varies. Even where loopback is treated as trustworthy, WebSocket mixed-content rules, local-network access permission, enterprise settings and PWA `connect-src` can block it. The SDK reports **unavailable**, not definitively “not installed.” The real PWA must explicitly allow the chosen loopback WebSocket endpoint in CSP and test supported Chrome/Edge/Firefox/Safari versions on each OS. A browser permission for local-network access may still occur once; it is not a USB device chooser. Never disable browser security flags or import a broadly trusted self-signed root as a cashier workaround.

For environments that prohibit HTTPS→WS loopback, plan a properly managed loopback TLS deployment (hostname, certificate issuance/renewal and threat review) or an authenticated outbound relay transport with explicit offline limitations. Neither is falsely implemented here. Do not claim universal Safari support until proven by the hardware/browser acceptance matrix.

## Resource and access limits

8 simultaneous sessions, 5s upgrade, 10s authentication, 30 commands/sec per session, 128 KiB frames/messages, 5s response writes, 60s idle disconnect. A malicious local process can still consume these slots: loopback service denial is not fully preventable without OS isolation. 16 printers, 256 queued/printing jobs, bounded text and raster dimensions. SQLite and font work run outside the async IO thread. No document/job/token logging.

User-private application data directory (0700 on Unix) contains DB and WAL. Receipt content is not encrypted at rest; use OS full-disk encryption and restricted desktop accounts. Logs are daily, at most seven files; currently only coarse lifecycle/worker codes are logged. Protect diagnostic bundles and never attach the keychain, pairing secret or raw order DB to public issues.
