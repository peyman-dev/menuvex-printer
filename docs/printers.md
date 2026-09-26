# Printer setup and compatibility

## Profiles

Each configured printer has a stable internal ID, display name, tagged `connection`, 58/80 paper mm, actual raster width (128–832, divisible by 8), copies (1–3), cutter enable, local font family and size (12–48 px). Default UI uses 80mm/576 dots; changing paper size does **not** silently overwrite actual dot width. Choose 384 for common 58mm mechanisms only if the manual agrees. Saved job profiles are immutable snapshots.

USB descriptor: VID/PID, optional serial, bus and port topology, interface, alternate setting, bulk OUT endpoint. Manufacturer/product are discovery metadata. With serial: match VID/PID/serial; multiple matches rejected. Without serial: physical topology is used, never bus address (which changes on reconnect). Changing hub/port or bus topology can require re-selection; identical serial-less printers cannot be magically distinguished. Access-denied discovery can list descriptors but not serial/product. Re-select after permission setup if needed.

Only standard class 0x07 printer interfaces are selectable through discovery. Never treat HID/storage/vendor interfaces as generic printers. All saved endpoint data is revalidated against live descriptors before sending. USB permission errors are actionable, not retried forever. Vendor-specific support requires a device contract and hardware tests.

LAN: exact RFC1918 IPv4, default 9100, adjustable port through local UI, no DNS/public IP/loopback/link-local. Use DHCP reservation/static address to avoid silent address changes. Probe makes a connection only; it does not send dummy ESC/POS. Wrong service at that address may accept connections, so operator must confirm with actual print. Configure invoice, kitchen, bar routes in Settings; PWA reads routes in `agent.status`. The Windows 7 Legacy agent offers the same direct LAN (TCP/9100, private IPv4) per printer, alongside installed Windows RAW spooler queues.

## Status meaning

`online`: USB device identity present / TCP connection succeeds. Not paper/cutter/cover/temperature confirmation. `offline`: absent/unreachable. `unknown`: not probed or enumeration failed. `busy`: current hardware transfer, visible in printer list; job `printing` events also indicate activity. `error`: representable in protocol; actual job has structured errors. USB changes are checked at most once every ten seconds, not OS hotplug notification; no claim of instantaneous unplug detection. Probing skips the active printer when observed, but is advisory and cannot reserve hardware.

`completed` test print means bytes sent. **Inspect the paper** to validate encoding, alignment, clipping and cutter. A failed/incomplete physical print despite successful writes needs model-specific status commands and a human decision; do not auto-replay.

## ESC/POS variants

GS v 0 raster support is assumed; some printers need a different image command, pacing or driver. Printer-specific code pages are not used for Persian. Rasterization increases byte volume and print time. QR/barcode/cash drawer commands differ by model/firmware and are not exposed to the remote v1 document schema. Never enable a cash drawer command without matching voltage/pin/timing documentation.

No real model is certified by this change. Track manufacturer, model, firmware, USB driver/interface, dot width, raster/cut behavior and OS/browser in the hardware acceptance sheet.
