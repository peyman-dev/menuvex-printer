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

/// Hand `bytes` to the OS queue `queue`. On Windows the implementation is the explicit
/// Win32 RAW StartDoc/Write/EndDoc pipeline in `raw_printer`; Linux/macOS keep using CUPS `lp -o raw`.
pub fn send(queue: &str, bytes: &[u8]) -> Result<()> {
    send_with_options(queue, bytes, false).map(|_| ())
}

/// RAW spooler path with the optional Windows `PRINTER_DEFAULTSW.pDatatype = RAW` override.
pub fn send_with_options(queue: &str, bytes: &[u8], force_raw: bool) -> Result<usize> {
    #[cfg(windows)]
    {
        crate::printers::raw_printer::print_raw(queue, bytes, force_raw)
    }
    #[cfg(not(windows))]
    {
        let _ = force_raw;
        platform::send(queue, bytes)?;
        Ok(bytes.len())
    }
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
    #[repr(C)]
    struct PrinterInfo4W {
        printer_name: *mut u16,
        server_name: *mut u16,
        attributes: u32,
    }
    #[link(name = "winspool")]
    extern "system" {
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
