# Release gates and update readiness

Version metadata is 1.0.0 (semantic versioning), **not evidence of a completed production release**. This checkout supplies source and native candidate build configuration. No installer binary, code-signing result, notarization result, native test result or hardware certification was generated here.

## Blocking gates before restaurant deployment

1. Run native CI on all four targets; fix every compiler/test/bundler failure. Review resolved dependency versions/licenses/advisories, commit real Cargo.lock, pin release dependency/action revisions, rerun with locked dependencies. Rust was unavailable in this session; parser formatting is not compilation.
2. Run the real SDK→Rust WebSocket integration harness and review packet/schema/timeout behavior. Add full negative/slow-client socket and fault-injection coverage before security sign-off.
3. Execute every relevant hardware/browser checklist, including mid-write failures, physical Persian output and clean login/uninstall. No USB model is currently certified.
4. Integrate the actual PWA (not present here): existing order events, typed invoice adapter, one print-owner policy, pairing UX, status/error UI, legacy migration callback and real download route. Verify no unintended changes to order WebSocket or WebUSB legacy path.
5. Finalize installer UX: guided per-device Linux permissions, approved Windows USB drivers (or a separately implemented RAW spooler backend), startup failure dialog/recovery, administrator decommissioning/uninstall cleanup. Current technician-assisted steps are documented, not a fully automatic cashier installation experience.
6. Sign Windows application/installer. Sign and notarize macOS with Developer ID; verify Gatekeeper on clean machines. Publish checksummed artifacts and accurate platform/architecture links. Linux repo/package trust policy also needs an owner.
7. Security review: HTTPS→WS browser policy, OS keychain session availability, PWA XSS/CSP, same-user threat model, merchant/device scope, log/data retention and response/resource limits.
8. Establish operational reconciliation for ambiguous jobs and database restore. Exactly-once physical printing is impossible on generic RAW ESC/POS without hardware acknowledgements/transactions.

`docs/ci/checks.yml` is an inactive workflow template that builds **candidates only**, uploads limited-retention workflow artifacts, and does not create GitHub releases or need signing secrets. The current GitHub connection cannot create workflows. To activate CI, an authorized maintainer must place the template at `.github/workflows/checks.yml`. Workflow jobs have read-only repository permissions. Inspect every artifact; unsigned success is not production approval. It has not been triggered by this session.

## Updates

No pretend updater endpoint/public key is configured. Add Tauri's signed updater plugin when infrastructure exists. Use an HTTPS manifest and embedded public verification key; keep private signing keys only in CI secret storage. Drain current job, stop new submissions, back up SQLite, apply explicit schema migrations, restart, and retain all dedup tombstones. Queued jobs must survive an update. A rollback must not downgrade an unknown schema or erase completed IDs. Upgrade data and protocol versions independently of application semver.

Use 1.0.1 for compatible fixes, 1.1.0 for additive compatible features, 2.0.0 for breaking behavior; explicit wire version required on protocol changes. Prefer model-specific transport/profile support over widening unrestricted hardware access.
