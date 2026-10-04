//! OS print-queue (spooler) transport.
//!
//! Prints RAW ESC/POS bytes through the print queues that are already installed on the
//! operating system — the vendor driver the customer installed stays in place. This is the
//! recommended way to use USB receipt printers: no libusb/libusbK driver replacement is ever
//! required (replacing the driver breaks other POS software on the same machine).
//!
//! * **Windows** — winspool (`OpenPrinterW`/`WritePrinter` with the `RAW` datatype), exactly
//!   like the Legacy Windows agent.
//! * **Linux/macOS** — CUPS via `lp -d <queue> -o raw`.
//!
//! "Completed" means the document was accepted by the OS spooler, not that paper came out;
//! the spooler may still hold the job if the device is off (see `docs/protocol.md`).
use serde::Serialize;
use crate::error::{ AgentError, Result };

/// One installed OS print queue, as returned by `printers.installed`.
#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct InstalledPrinter {
    pub queue_name: String,
}

/// Installed OS print queues, for the setup UI and the `printers.installed` command.
pub fn list() -> Result<Vec<InstalledPrinter>> {
    Ok(
        platform
            ::queues()?
            .into_iter()
            .map(|queue_name| InstalledPrinter { queue_name })
            .collect()
    )
}

/// Hand `bytes` to the OS queue `queue`. Errors before the document is submitted are
/// retryable; errors after submission are uncertain because the spooler may still print it.
pub fn send(queue: &str, bytes: &[u8]) -> Result<()> {
    platform::send(queue, bytes)
}

/// `online` when the queue is installed and the spooler answers, otherwise `offline` /
/// `unknown`. Queue presence says nothing about paper or the device power state.
pub fn status(queue: &str) -> String {
    match platform::queues() {
        Ok(queues) =>
            (if queues.iter().any(|q| q == queue) { "online" } else { "offline" }).into(),
        Err(_) => "unknown".into(),
    }
}

#[cfg(windows)]
mod platform {
    use super::*;
    use std::ffi::c_void;

    type Handle = *mut c_void;
    #[repr(C)]
    struct PrinterDefaultsW {
        datatype: *mut u16,
        dev_mode: *mut c_void,
        desired_access: u32,
    }
    #[repr(C)]
    struct DocInfo1W {
        doc_name: *mut u16,
        output_file: *mut u16,
        datatype: *mut u16,
    }
    #[repr(C)]
    struct PrinterInfo4W {
        printer_name: *mut u16,
        server_name: *mut u16,
        attributes: u32,
    }
    #[link(name = "winspool")]
    extern "system" {
        fn OpenPrinterW(name: *const u16, printer: *mut Handle, default: *mut PrinterDefaultsW) -> i32;
        fn ClosePrinter(printer: Handle) -> i32;
        fn StartDocPrinterW(printer: Handle, level: u32, info: *mut DocInfo1W) -> u32;
        fn EndDocPrinter(printer: Handle) -> i32;
        fn StartPagePrinter(printer: Handle) -> i32;
        fn EndPagePrinter(printer: Handle) -> i32;
        fn WritePrinter(printer: Handle, buf: *const c_void, len: u32, written: *mut u32) -> i32;
        fn EnumPrintersW(
            flags: u32,
            name: *const u16,
            level: u32,
            buffer: *mut u8,
            size: u32,
            needed: *mut u32,
            returned: *mut u32
        ) -> i32;
    }
    const PRINTER_ENUM_LOCAL: u32 = 2;
    const PRINTER_ENUM_CONNECTIONS: u32 = 4;
    const PRINTER_ACCESS_USE: u32 = 8;

    fn wide(s: &str) -> Vec<u16> {
        s.encode_utf16().chain(std::iter::once(0)).collect()
    }

    pub fn queues() -> Result<Vec<String>> {
        let flags = PRINTER_ENUM_LOCAL | PRINTER_ENUM_CONNECTIONS;
        unsafe {
            let (mut needed, mut count) = (0u32, 0u32);
            EnumPrintersW(flags, std::ptr::null(), 4, std::ptr::null_mut(), 0, &mut needed, &mut count);
            if needed == 0 {
                return Ok(Vec::new());
            }
            let mut buffer = vec![0u8; needed as usize];
            if
                EnumPrintersW(
                    flags,
                    std::ptr::null(),
                    4,
                    buffer.as_mut_ptr(),
                    needed,
                    &mut needed,
                    &mut count
                ) == 0
            {
                return Err(
                    AgentError::new("SPOOLER_ERROR", "Windows print queues could not be listed")
                );
            }
            let infos = buffer.as_ptr() as *const PrinterInfo4W;
            let mut out = Vec::new();
            for i in 0..count as usize {
                let info = &*infos.add(i);
                if info.printer_name.is_null() {
                    continue;
                }
                let mut len = 0usize;
                while *info.printer_name.add(len) != 0 {
                    len += 1;
                }
                out.push(
                    String::from_utf16_lossy(std::slice::from_raw_parts(info.printer_name, len))
                );
            }
            Ok(out)
        }
    }

    pub fn send(queue: &str, bytes: &[u8]) -> Result<()> {
        let name = wide(queue);
        let mut datatype = wide("RAW");
        unsafe {
            let mut handle: Handle = std::ptr::null_mut();
            let mut defaults = PrinterDefaultsW {
                datatype: datatype.as_mut_ptr(),
                dev_mode: std::ptr::null_mut(),
                desired_access: PRINTER_ACCESS_USE,
            };
            if OpenPrinterW(name.as_ptr(), &mut handle, &mut defaults) == 0 || handle.is_null() {
                return Err(AgentError::retry("PRINTER_OFFLINE"));
            }
            let mut doc_name = wide("MenuVex Receipt");
            let mut info = DocInfo1W {
                doc_name: doc_name.as_mut_ptr(),
                output_file: std::ptr::null_mut(),
                datatype: datatype.as_mut_ptr(),
            };
            if StartDocPrinterW(handle, 1, &mut info) == 0 {
                ClosePrinter(handle);
                return Err(AgentError::retry("PRINTER_OFFLINE"));
            }
            // From here on the spooler may already own the document: failures are ambiguous
            // and must never trigger an automatic reprint.
            let mut ok = StartPagePrinter(handle) != 0;
            if ok {
                let mut written = 0u32;
                ok =
                    WritePrinter(
                        handle,
                        bytes.as_ptr() as *const c_void,
                        bytes.len() as u32,
                        &mut written
                    ) != 0 && (written as usize) == bytes.len();
                ok = (EndPagePrinter(handle) != 0) && ok;
            }
            ok = (EndDocPrinter(handle) != 0) && ok;
            ClosePrinter(handle);
            if ok {
                Ok(())
            } else {
                Err(AgentError::uncertain())
            }
        }
    }
}

#[cfg(not(windows))]
mod platform {
    use super::*;
    use std::io::Write;
    use std::process::{ Command, Stdio };

    pub fn queues() -> Result<Vec<String>> {
        // `lpstat -e` prints one destination per line; it fails when CUPS has none.
        let output = Command::new("lpstat")
            .arg("-e")
            .output()
            .map_err(|_| {
                AgentError::new("SPOOLER_ERROR", "CUPS tools (lpstat) are not installed")
            })?;
        if !output.status.success() {
            return Ok(Vec::new());
        }
        Ok(
            String::from_utf8_lossy(&output.stdout)
                .lines()
                .map(str::trim)
                .filter(|l| !l.is_empty())
                .map(str::to_owned)
                .collect()
        )
    }

    pub fn send(queue: &str, bytes: &[u8]) -> Result<()> {
        let mut child = Command::new("lp")
            .args(["-d", queue, "-o", "raw", "-s"])
            .stdin(Stdio::piped())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .spawn()
            .map_err(|_| AgentError::new("SPOOLER_ERROR", "CUPS tools (lp) are not installed"))?;
        let accepted = child.stdin
            .take()
            .ok_or_else(|| AgentError::retry("PRINTER_OFFLINE"))
            .and_then(|mut stdin| {
                stdin.write_all(bytes).map_err(|_| AgentError::retry("PRINTER_OFFLINE"))
            });
        match child.wait() {
            // `lp` exits right after the queue accepts the job; a non-zero exit means the
            // job was rejected before submission (unknown queue, CUPS down) and is retryable.
            Ok(status) if status.success() && accepted.is_ok() => Ok(()),
            Ok(_) => Err(accepted.err().unwrap_or_else(|| AgentError::retry("PRINTER_OFFLINE"))),
            Err(_) => Err(AgentError::uncertain()),
        }
    }
}
