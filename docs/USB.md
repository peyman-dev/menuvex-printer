# USB printer support — per-OS reality

There is no single cross-platform "WebUSB" API at OS level, so the agent
uses each platform's **native** mechanism. All three backends are
implemented with the Go standard library only (no libusb, no CGO, no
drivers to install for the agent itself).

Check what your agent supports at runtime:

```bash
curl -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8765/api/v1/info
# → {"usbSupported":true,"usbDetail":"..."}
```

The agent **never pretends**: printers it cannot reach are not listed, and
USB calls on unsupported platforms fail with `USB_UNSUPPORTED`.

## Linux — direct usblp writes (best support)

- **Mechanism:** raw bytes are written to the kernel `usblp` device node
  (`/dev/usb/lp0`, …). Discovery reads VID/PID/serial/manufacturer from
  sysfs (`/sys/class/usblp`).
- **Printer ID:** `usb:<VID>:<PID>:<serial>`, e.g. `usb:04B8:0202:ABC123`
  (VID/PID uppercase hex). Devices without a serial number get a stable
  `busNNNdevMMM` suffix instead. IDs survive replugs (resolved by
  re-scanning, not by cached `/dev` paths).
- **Requirements:**
  1. The `usblp` kernel module must be loaded and bound to the printer
     (true by default for most USB thermal printers; check with
     `ls /dev/usb/`).
  2. The agent user needs write access: add it to the `lp` group
     (`sudo usermod -aG lp $USER`, then log out/in) and/or install the
     shipped udev rule `scripts/99-novex-printer-agent.rules`.
- **Limitations:**
  - Printers **not** claimed by `usblp` (vendor-specific USB classes, or
    printers grabbed by another driver) are invisible to the agent. They
    will not appear in listings — this is by design, not a bug.
  - No USB permission prompts exist on Linux; a permission error means the
    group/rule setup above is missing (the error message says so).

## Windows — Winspool RAW passthrough

- **Mechanism:** the agent opens the printer with Win32 `OpenPrinter` and
  sends each print as one spooler job with the `RAW` datatype via
  `WritePrinter` — bytes go to the device unmodified. Same technique QZ
  Tray and POS software use. No WinUSB/Zadig driver replacement needed.
- **Printer ID:** `usb:win:<Printer Name>`, e.g. `usb:win:OCOM Printer`
  (URL-escape it in API paths). Only printers installed in Windows on a
  `USB001`-style port are listed.
- **Requirements:** install the printer once in
  *Settings → Bluetooth & devices → Printers* (any driver works for RAW
  passthrough; the vendor driver is fine).
- **Limitations (honest):**
  - A USB printer with **no Windows printer object** is NOT visible.
  - The spooler API does not expose VID/PID, so `vendorId`/`productId`
    are reported as unknown (`0`). Identification is by spooler name.
  - Renaming the printer in Windows changes its printer ID.

## macOS — CUPS IPP to a Raw queue

- **Mechanism:** the agent speaks IPP (pure HTTP + binary encoding) to the
  local CUPS daemon (`http://127.0.0.1:631`) and submits the payload as
  `application/octet-stream`. macOS offers no stable user-space USB bulk
  API without IOKit/CGO, so CUPS is the native path.
- **Printer ID:** `usb:cups:<Queue Name>`, e.g. `usb:cups:OCOM_USB`. Only
  CUPS queues whose device-uri starts with `usb:` are listed.
- **Requirements:**
  1. CUPS must be running (`cupsctl` / Printer Sharing is *not* required;
     local `localhost:631` is enough).
  2. The printer **must be added as a Raw queue**, otherwise CUPS filters
     will mangle the bytes:
     ```bash
     lpstat -v                                   # find the usb:// URI
     lpadmin -p OCOM_USB -E -v "usb://OCOM/Thermal?serial=ABC123" -m raw
     ```
     Verify with `lpstat -v` that the queue's device-uri starts with `usb:`.
- **Limitations (honest):**
  - Non-Raw queues are not safe for raw ESC/POS and must not be used; the
    agent cannot verify a queue is Raw, so a misconfigured queue fails at
    print time with a CUPS status error (the message points back here).
  - CUPS does not expose VID/PID, so they are reported as unknown (`0`).
    Make/model/serial are parsed best-effort from the `usb://` device-uri.
  - If CUPS is stopped or unreachable, USB calls fail with
    `PRINTER_CONNECTION_FAILED` (not silently).

## Other platforms

`usb_unsupported.go` returns `USB_UNSUPPORTED` for any OS without a
backend. v1 supports Windows, macOS and Linux only.

## Troubleshooting checklist

1. `GET /api/v1/info` → is `usbSupported` true?
2. `GET /api/v1/printers` → is the printer listed? If not, the OS-level
   requirement above is not met (permissions / spooler install / Raw queue).
3. `POST .../print` with a tiny payload (e.g. `1B 40` + text + `1D 56 00`).
4. Still failing? `GET .../status` shows `lastError`; the agent log shows
   the same error with the printer ID.
