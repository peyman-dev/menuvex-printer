# Direct USB feature and safe fallback

## Build feature

Direct USB through `rusb`/libusb is optional at compile time. It is **not** in Cargo's default feature set:

```sh
# Default transports only: desktop, OS print queues and network
cargo build --manifest-path src-tauri/Cargo.toml

# Include direct USB support
cargo build --manifest-path src-tauri/Cargo.toml --features libusb
```

The dependency remains `vendored`, so an installed libusb shared library is not required. A compatible OS/device driver and the appropriate per-platform permissions are still required. On Windows, this Agent never installs or replaces USB drivers; direct access may require a WinUSB-compatible driver or UsbDk. Use the installed Windows print queue when preserving the vendor driver is important.

A default build can still read an existing USB profile and report `LIBUSB_FEATURE_DISABLED`; network and spooler profiles continue to work. If that USB profile has an explicit local fallback target, it may use that target under the same safe pre-write rule below. The direct discovery button reports that the feature is unavailable rather than crashing or silently changing a driver.

## Operator-selected fallback

Each USB profile can optionally store one local `usbFallbackTarget`:

- an installed OS spooler queue (Windows RAW / CUPS RAW); or
- a private IPv4 network printer and port.

The default is empty. The target is selected in the local Agent settings, validated as non-USB, and is not remotely writable by the website's `printer.save` command. The Agent never guesses a queue from VID/PID, chooses a route automatically, or changes driver bindings.

The profile UI describes the target as: **“Used only when USB fails before any data is sent.”**

## Before-first-byte policy

A fallback is attempted only when all of the following are true:

1. The selected USB operation is in `PreOpen` or `PostOpenPreWrite`.
2. The byte count is known to be exactly zero (`Some(0)`), not missing or inferred from the stage alone.
3. No earlier copy of this job has been handed to any transport.
4. A fallback target was explicitly configured by the operator.

| Stage | Examples | Decision |
| --- | --- | --- |
| `PreOpen` | non-timeout initialization/enumeration error, device absent, non-timeout `open_device` failure | Use configured fallback only with confirmed zero bytes; otherwise return an actionable error |
| `PostOpenPreWrite` | non-timeout interface claim or alternate-setting failure before the first bulk write | Use configured fallback only with confirmed zero bytes; otherwise return an actionable error |
| `MidWrite` | bulk write failure, including a possible partial transfer | **Forbid** fallback; mark outcome uncertain |
| `Timeout` | libusb transfer timeout or outer USB operation deadline | **Forbid** fallback; mark outcome uncertain |
| `Unknown` | worker panic/disconnect or any unclassified state | **Forbid** fallback; mark outcome uncertain |

Once a job has emitted a copy, all remaining copies stay on that selected route. A later USB open failure cannot move the remaining copies to another printer. This avoids splitting a multi-copy job across devices and prevents duplicate output.

The same policy applies when the binary was built without `libusb`: that is a `PreOpen` condition. It may use only an explicitly configured fallback. With no configured target, the job fails clearly with `LIBUSB_FEATURE_DISABLED`.

## Diagnostics

Structured USB failure events contain the failure stage, libusb numeric error (when supplied by libusb), error variant name, fallback decision (`auto`, `ask`, or `forbid`), configured target, known bytes completed before the failed operation, and completed copies before failure. `bytes_written_before_failure = -1` together with `bytes_written_known = false` means the count is indeterminate; pre-open/pre-write failures record zero. Logs never contain receipt contents.

For USB bulk writes, the count is the number of bytes confirmed by completed transfers before the failing transfer. A failed/timeout transfer itself may have delivered an unknown partial amount, which is why `MidWrite` and `Timeout` always forbid fallback.

## Tests

- `npm run test:rust`: compile/test with no optional libusb feature; existing spooler/network code remains available.
- `npm run test:rust:libusb`: compile/test with direct USB enabled.
- `npm test`: SDK schema and frontend tests.
- `npm run build`: TypeScript and UI build.

Real-device acceptance is still required for each model/driver. The failover unit tests inject classified failures; they do not claim that every USB driver, spooler or printer model is certified.
