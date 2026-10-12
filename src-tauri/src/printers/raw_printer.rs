//! Local RAW-spooler diagnostics and transport.
//!
//! The Windows send path is deliberately small and auditable: OpenPrinterW, StartDocPrinterW
//! (`DOC_INFO_1W.pDatatype = "RAW"`), StartPagePrinter, one WritePrinter with the exact byte
//! slice, EndPagePrinter, EndDocPrinter, and ClosePrinter. RAW bypasses GDI rendering; it does not
//! promise that every vendor driver or spooler extension will preserve bytes, so the UI warns and
//! offers a separate Generic / Text Only queue on the existing USB port.
use crate::error::{AgentError, Result};
use serde::Serialize;
use std::time::Instant;

const RAW_DATATYPE: &str = "RAW";
const GENERIC_TEXT_ONLY_DRIVER: &str = "Generic / Text Only";

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct RawPrinterInfo {
    pub queue_name: String,
    pub platform: String,
    pub driver_name: Option<String>,
    pub port_name: Option<String>,
    pub is_generic_text_only: bool,
    /// Enumerated local Windows USB ports (for example USB001 and USB002). Empty off Windows.
    pub available_usb_ports: Vec<String>,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct RawTargetCreated {
    pub queue_name: String,
    pub driver_name: String,
    pub port_name: String,
    pub created: bool,
}

pub fn platform_name() -> &'static str {
    if cfg!(windows) {
        "windows"
    } else if cfg!(target_os = "macos") {
        "macos"
    } else if cfg!(target_os = "linux") {
        "linux"
    } else {
        "other"
    }
}

/// Read the selected queue's installed driver and port. This is local-only; it does not modify
/// that queue or its driver.
pub fn inspect_printer(queue_name: &str) -> Result<RawPrinterInfo> {
    if queue_name.trim().is_empty() || queue_name.chars().any(char::is_control) {
        return Err(AgentError::new("INVALID_PAYLOAD", "A valid local print queue is required"));
    }
    #[cfg(windows)]
    {
        return windows_impl::inspect_printer(queue_name);
    }
    #[cfg(not(windows))]
    {
        let queues = crate::printers::spooler::list()?;
        if !queues.iter().any(|queue| queue.queue_name == queue_name) {
            let mut error = AgentError::new("SPOOLER_QUEUE_NOT_FOUND", "Print queue is not installed");
            error.printer = Some(queue_name.to_owned());
            return Err(error);
        }
        Ok(RawPrinterInfo {
            queue_name: queue_name.to_owned(),
            platform: platform_name().to_owned(),
            driver_name: Some("CUPS RAW queue".into()),
            port_name: None,
            is_generic_text_only: false,
            available_usb_ports: vec![],
        })
    }
}

/// Create a second, explicitly named Generic / Text Only queue on the selected source queue's
/// existing USB port. The source queue and its vendor driver are never changed. AddPrinterW only
/// references the installed Generic driver; it does not install or replace a driver.
pub fn create_generic_raw_target(source_queue: &str, vendor_name: &str) -> Result<RawTargetCreated> {
    #[cfg(windows)]
    {
        return windows_impl::create_generic_raw_target(source_queue, vendor_name);
    }
    #[cfg(not(windows))]
    {
        let _ = (source_queue, vendor_name);
        Err(AgentError::new(
            "UNSUPPORTED_PLATFORM",
            "Generic / Text Only raw target creation is available only on Windows.",
        ))
    }
}

/// Send one RAW document through an installed OS queue. Non-Windows retains the existing CUPS
/// route; only the Windows implementation uses the new Win32 API pipeline.
pub fn print_raw(queue_name: &str, bytes: &[u8], force_raw: bool) -> Result<usize> {
    let start = Instant::now();
    let preview = hex_preview(bytes);
    #[cfg(windows)]
    let mut result = windows_impl::print_raw(queue_name, bytes, force_raw);
    #[cfg(not(windows))]
    let mut result = crate::printers::spooler::send_with_options(queue_name, bytes, force_raw);

    if let Err(error) = &mut result {
        error.printer = Some(queue_name.to_owned());
        if error.action_required.is_none() {
            error.action_required = Some(if error.uncertain {
                "Check the Windows print queue and physical output before retrying; the job outcome may be uncertain.".into()
            } else {
                "Check that the selected print queue is installed and available.".into()
            });
        }
    }
    match &result {
        Ok(written) => tracing::info!(
            target: "raw_print",
            event = "RAW_SPOOLER_PRINT_COMPLETED",
            timestamp_unix_ms = unix_time_ms(),
            printer = queue_name,
            printer_target = queue_name,
            payload_bytes = bytes.len(),
            bytes_written = written,
            first_32_bytes_hex = %preview,
            force_raw,
            elapsed_ms = start.elapsed().as_millis(),
            "RAW spooler document accepted"
        ),
        Err(error) => tracing::error!(
            target: "raw_print",
            event = "RAW_SPOOLER_PRINT_FAILED",
            timestamp_unix_ms = unix_time_ms(),
            printer = queue_name,
            printer_target = queue_name,
            payload_bytes = bytes.len(),
            first_32_bytes_hex = %preview,
            force_raw,
            error_code = %error.code,
            error_message = %error.message,
            retryable = error.retryable,
            uncertain = error.uncertain,
            elapsed_ms = start.elapsed().as_millis(),
            "RAW spooler document failed"
        ),
    }
    result
}

fn unix_time_ms() -> u128 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis()
}

fn hex_preview(bytes: &[u8]) -> String {
    bytes.iter().take(32).map(|byte| format!("{byte:02X}")).collect::<Vec<_>>().join(" ")
}

fn bool_code(value: bool) -> u32 {
    if value { 1 } else { 0 }
}

/// API seam for a deterministic byte/sequence test without a physical printer.
trait RawSpoolerApi {
    type Handle;

    fn open_printer(&mut self, queue_name: &str, force_raw: bool) -> Result<Self::Handle>;
    fn start_doc(&mut self, handle: &Self::Handle, datatype: &str) -> Result<u32>;
    fn start_page(&mut self, handle: &Self::Handle) -> Result<()>;
    fn write(&mut self, handle: &Self::Handle, bytes: &[u8]) -> Result<usize>;
    fn end_page(&mut self, handle: &Self::Handle) -> Result<()>;
    fn end_doc(&mut self, handle: &Self::Handle) -> Result<()>;
    fn close_printer(&mut self, handle: &Self::Handle) -> Result<()>;
}

/// The order is intentionally identical to the documented Win32 RAW spooler pipeline. On every
/// failure after StartDoc, cleanup is attempted but the original (potentially uncertain) error is
/// preserved; a ClosePrinter failure after EndDoc is itself marked uncertain to avoid duplicate
/// automatic printing.
fn print_raw_with_api<A: RawSpoolerApi>(
    api: &mut A,
    queue_name: &str,
    bytes: &[u8],
    force_raw: bool,
) -> Result<usize> {
    if bytes.is_empty() {
        return Err(AgentError::new("RAW_ESC_POS_INVALID", "RAW spooler document is empty"));
    }
    let byte_count = u32::try_from(bytes.len()).map_err(|_| {
        AgentError::new("RAW_PAYLOAD_TOO_LARGE", "RAW spooler document exceeds the Win32 byte-count limit")
    })?;

    let handle = api.open_printer(queue_name, force_raw)?;
    let mut document_started = false;
    let mut page_started = false;
    let result = (|| {
        let job_id = api.start_doc(&handle, RAW_DATATYPE)?;
        if job_id == 0 {
            return Err(AgentError::retry("RAW_SPOOLER_STARTDOC_FAILED"));
        }
        document_started = true;
        api.start_page(&handle)?;
        page_started = true;
        let written = api.write(&handle, bytes)?;
        if written != byte_count as usize {
            let mut error = AgentError::new(
                "RAW_SPOOLER_PARTIAL_WRITE",
                &format!("WritePrinter accepted {written} of {byte_count} bytes"),
            );
            error.uncertain = true;
            return Err(error);
        }
        let end_page = api.end_page(&handle);
        page_started = false;
        end_page?;
        let end_doc = api.end_doc(&handle);
        document_started = false;
        end_doc?;
        Ok(written)
    })();

    if page_started {
        if let Err(error) = api.end_page(&handle) {
            tracing::warn!(
                target: "raw_print",
                event = "RAW_SPOOLER_CLEANUP_FAILED",
                api = "EndPagePrinter",
                error_code = %error.code,
                error_message = %error.message,
                "cleanup after RAW print failure"
            );
        }
    }
    if document_started {
        if let Err(error) = api.end_doc(&handle) {
            tracing::warn!(
                target: "raw_print",
                event = "RAW_SPOOLER_CLEANUP_FAILED",
                api = "EndDocPrinter",
                error_code = %error.code,
                error_message = %error.message,
                "cleanup after RAW print failure"
            );
        }
    }
    let close_result = api.close_printer(&handle);
    match (result, close_result) {
        (Err(error), _) => Err(error),
        (Ok(written), Ok(())) => Ok(written),
        (Ok(_), Err(error)) => {
            let mut uncertain = error;
            uncertain.uncertain = true;
            uncertain.retryable = false;
            if uncertain.action_required.is_none() {
                uncertain.action_required = Some(
                    "EndDocPrinter succeeded but ClosePrinter failed; check the queue before retrying.".into(),
                );
            }
            Err(uncertain)
        }
    }
}

#[cfg(windows)]
mod windows_impl {
    use super::*;
    use std::{ffi::c_void, ptr, slice};
    use windows::{
        core::{PCWSTR, PWSTR},
        Win32::{
            Foundation::{GetLastError, HANDLE},
            Graphics::{
                Gdi::DEVMODEW,
                Printing::{
                    AddPrinterW, ClosePrinter, EndDocPrinter, EndPagePrinter, EnumPortsW,
                    GetPrinterW, OpenPrinterW, StartDocPrinterW, StartPagePrinter, WritePrinter,
                    DOC_INFO_1W, PORT_INFO_1W, PRINTER_ACCESS_USE, PRINTER_DEFAULTSW,
                    PRINTER_HANDLE, PRINTER_INFO_2W, PRINTER_ATTRIBUTE_LOCAL,
                },
            },
            Security::PSECURITY_DESCRIPTOR,
        },
    };

    fn wide(value: &str) -> Vec<u16> {
        value.encode_utf16().chain(std::iter::once(0)).collect()
    }

    fn last_error_code() -> u32 {
        unsafe { GetLastError().0 }
    }

    fn api_error(
        api: &'static str,
        code: &'static str,
        win32_error_code: u32,
        retryable: bool,
        uncertain: bool,
    ) -> AgentError {
        let mut error = AgentError::new(
            code,
            &format!("{api} failed with Win32 error {win32_error_code}"),
        );
        error.retryable = retryable;
        error.uncertain = uncertain;
        error
    }

    fn log_api(api: &'static str, return_code: u32, win32_error_code: u32, outcome: &'static str) {
        if return_code == 0 {
            tracing::error!(
                target: "raw_print",
                event = "WIN32_SPOOLER_API",
                api,
                return_code,
                win32_error_code,
                outcome,
                "Win32 RAW spooler API call"
            );
        } else {
            tracing::info!(
                target: "raw_print",
                event = "WIN32_SPOOLER_API",
                api,
                return_code,
                win32_error_code,
                outcome,
                "Win32 RAW spooler API call"
            );
        }
    }

    fn pwstr_to_string(value: PWSTR) -> Option<String> {
        if value.0.is_null() {
            return None;
        }
        unsafe {
            let mut len = 0usize;
            while *value.0.add(len) != 0 {
                len += 1;
            }
            Some(String::from_utf16_lossy(slice::from_raw_parts(value.0, len)))
        }
    }

    fn open(queue_name: &str, force_raw: bool) -> Result<PRINTER_HANDLE> {
        let name = wide(queue_name);
        let datatype = wide(RAW_DATATYPE);
        let default_datatype = if force_raw {
            PWSTR(datatype.as_ptr() as *mut u16)
        } else {
            PWSTR::null()
        };
        let defaults = PRINTER_DEFAULTSW {
            pDatatype: default_datatype,
            pDevMode: ptr::null_mut::<DEVMODEW>(),
            DesiredAccess: PRINTER_ACCESS_USE,
        };
        let mut handle = PRINTER_HANDLE { Value: ptr::null_mut() };
        let result = unsafe {
            OpenPrinterW(PCWSTR(name.as_ptr()), &mut handle, Some(&defaults as *const _))
        };
        let error_code = if result.is_err() { last_error_code() } else { 0 };
        log_api("OpenPrinterW", bool_code(result.is_ok()), error_code, "open");
        result.map_err(|_| api_error(
            "OpenPrinterW",
            "RAW_SPOOLER_OPEN_FAILED",
            error_code,
            true,
            false,
        ))?;
        if handle.Value.is_null() {
            return Err(api_error(
                "OpenPrinterW",
                "RAW_SPOOLER_OPEN_FAILED",
                error_code,
                true,
                false,
            ));
        }
        Ok(handle)
    }

    struct OwnedPrinterInfo {
        queue_name: Option<String>,
        driver_name: Option<String>,
        port_name: Option<String>,
    }

    fn read_printer_info(handle: PRINTER_HANDLE) -> Result<OwnedPrinterInfo> {
        let mut needed = 0u32;
        let probe = unsafe { GetPrinterW(handle, 2, None, &mut needed) };
        let probe_error = if probe.is_err() { last_error_code() } else { 0 };
        // ERROR_INSUFFICIENT_BUFFER (122) is the expected two-call sizing probe.
        tracing::info!(
            target: "raw_print",
            event = "WIN32_SPOOLER_API",
            api = "GetPrinterW(size-probe)",
            return_code = bool_code(probe.is_ok()),
            win32_error_code = probe_error,
            required_bytes = needed,
            "Win32 queue-info buffer sizing"
        );
        if needed == 0 {
            return Err(api_error(
                "GetPrinterW",
                "RAW_SPOOLER_INFO_FAILED",
                probe_error,
                false,
                false,
            ));
        }
        // u64 backing guarantees alignment for PRINTER_INFO_2W and the pointers it contains.
        let mut words = vec![0u64; (needed as usize + 7) / 8];
        let buffer = unsafe {
            slice::from_raw_parts_mut(words.as_mut_ptr().cast::<u8>(), words.len() * 8)
        };
        let mut actual = needed;
        let result = unsafe { GetPrinterW(handle, 2, Some(buffer), &mut actual) };
        let error_code = if result.is_err() { last_error_code() } else { 0 };
        log_api("GetPrinterW", bool_code(result.is_ok()), error_code, "inspect queue");
        result.map_err(|_| api_error(
            "GetPrinterW",
            "RAW_SPOOLER_INFO_FAILED",
            error_code,
            false,
            false,
        ))?;
        let info = unsafe { &*words.as_ptr().cast::<PRINTER_INFO_2W>() };
        // Copy strings while the backing buffer is alive; PRINTER_INFO_2W's pointers must never
        // escape the allocation returned by GetPrinterW.
        Ok(OwnedPrinterInfo {
            queue_name: pwstr_to_string(info.pPrinterName),
            driver_name: pwstr_to_string(info.pDriverName),
            port_name: pwstr_to_string(info.pPortName),
        })
    }

    fn enum_usb_ports() -> Result<Vec<String>> {
        let mut needed = 0u32;
        let mut count = 0u32;
        let probe = unsafe {
            EnumPortsW(PCWSTR::null(), 1, None, &mut needed, &mut count)
        };
        let probe_error = if probe.0 == 0 { last_error_code() } else { 0 };
        tracing::info!(
            target: "raw_print",
            event = "WIN32_SPOOLER_API",
            api = "EnumPortsW(size-probe)",
            return_code = bool_code(probe.0 != 0),
            win32_error_code = probe_error,
            required_bytes = needed,
            returned_ports = count,
            "Win32 USB port enumeration sizing"
        );
        if needed == 0 {
            return Ok(Vec::new());
        }
        let mut words = vec![0u64; (needed as usize + 7) / 8];
        let buffer = unsafe {
            slice::from_raw_parts_mut(words.as_mut_ptr().cast::<u8>(), words.len() * 8)
        };
        let mut actual = needed;
        let mut returned = 0u32;
        let result = unsafe {
            EnumPortsW(PCWSTR::null(), 1, Some(buffer), &mut actual, &mut returned)
        };
        let error_code = if result.0 == 0 { last_error_code() } else { 0 };
        log_api("EnumPortsW", bool_code(result.0 != 0), error_code, "enumerate ports");
        if result.0 == 0 {
            return Err(api_error(
                "EnumPortsW",
                "RAW_USB_PORT_ENUMERATION_FAILED",
                error_code,
                false,
                false,
            ));
        }
        let infos = words.as_ptr().cast::<PORT_INFO_1W>();
        let mut ports = Vec::new();
        for index in 0..returned as usize {
            if let Some(name) = pwstr_to_string(unsafe { (*infos.add(index)).pName }) {
                if name.to_ascii_uppercase().starts_with("USB") {
                    ports.push(name);
                }
            }
        }
        ports.sort_by_key(|port| port.to_ascii_uppercase());
        ports.dedup_by(|a, b| a.eq_ignore_ascii_case(b));
        Ok(ports)
    }

    pub fn inspect_printer(queue_name: &str) -> Result<RawPrinterInfo> {
        let handle = open(queue_name, false).map_err(|mut error| {
            error.printer = Some(queue_name.to_owned());
            error
        })?;
        let result = read_printer_info(handle);
        let close = unsafe { ClosePrinter(handle) };
        let close_error = if close.is_err() { last_error_code() } else { 0 };
        log_api("ClosePrinter", bool_code(close.is_ok()), close_error, "close after inspect");
        let info = result?;
        let usb_ports = enum_usb_ports()?;
        let driver_name = info.driver_name;
        Ok(RawPrinterInfo {
            queue_name: info.queue_name.unwrap_or_else(|| queue_name.to_owned()),
            platform: "windows".into(),
            driver_name: driver_name.clone(),
            port_name: info.port_name,
            is_generic_text_only: driver_name.as_deref().is_some_and(|name|
                name.eq_ignore_ascii_case(GENERIC_TEXT_ONLY_DRIVER)
            ),
            available_usb_ports: usb_ports,
        })
    }

    fn select_usb_port(info: &RawPrinterInfo) -> Result<String> {
        let ports = info.port_name.as_deref().unwrap_or("");
        let matches: Vec<String> = ports
            .split(',')
            .map(str::trim)
            .filter(|candidate| candidate.to_ascii_uppercase().starts_with("USB"))
            .filter_map(|candidate| {
                info.available_usb_ports
                    .iter()
                    .find(|available| available.eq_ignore_ascii_case(candidate))
                    .cloned()
            })
            .collect();
        match matches.as_slice() {
            [port] => Ok(port.clone()),
            [] => {
                let mut error = AgentError::new(
                    "RAW_USB_PORT_NOT_FOUND",
                    &format!("Queue {:?} is not attached to an enumerated USB printer port", info.queue_name),
                );
                error.printer = Some(info.queue_name.clone());
                error.action_required = Some(format!(
                    "Choose a queue whose port is one of: {}.",
                    if info.available_usb_ports.is_empty() {
                        "no USB printer ports were found".into()
                    } else {
                        info.available_usb_ports.join(", ")
                    }
                ));
                Err(error)
            }
            _ => {
                let mut error = AgentError::new(
                    "RAW_USB_PORT_AMBIGUOUS",
                    "The selected queue is associated with more than one USB port.",
                );
                error.printer = Some(info.queue_name.clone());
                error.action_required = Some("Select a queue with one unambiguous USB port; no port was guessed.".into());
                Err(error)
            }
        }
    }

    pub fn create_generic_raw_target(source_queue: &str, vendor_name: &str) -> Result<RawTargetCreated> {
        if vendor_name.trim().is_empty() || vendor_name.chars().any(char::is_control) {
            return Err(AgentError::new("INVALID_PAYLOAD", "A valid printer name is required"));
        }
        let source = inspect_printer(source_queue)?;
        let port_name = select_usb_port(&source)?;
        let queue_name = format!("{} (Raw)", vendor_name.trim());
        if queue_name.encode_utf16().count() > 256 {
            return Err(AgentError::new("INVALID_PAYLOAD", "Raw target printer name is too long"));
        }

        if crate::printers::spooler::list()?.iter().any(|queue| queue.queue_name.eq_ignore_ascii_case(&queue_name)) {
            let existing = inspect_printer(&queue_name)?;
            if existing.is_generic_text_only && existing.port_name.as_deref().is_some_and(|p|
                p.split(',').any(|port| port.trim().eq_ignore_ascii_case(&port_name))
            ) {
                return Ok(RawTargetCreated {
                    queue_name,
                    driver_name: GENERIC_TEXT_ONLY_DRIVER.into(),
                    port_name,
                    created: false,
                });
            }
            let mut error = AgentError::new(
                "RAW_TARGET_EXISTS",
                "A queue with the proposed raw target name already exists with a different driver or port.",
            );
            error.printer = Some(queue_name);
            error.action_required = Some("Rename or remove the conflicting queue in Windows Settings, then retry.".into());
            return Err(error);
        }

        let mut server_name = Vec::<u16>::new();
        let mut target_wide = wide(&queue_name);
        let mut port_wide = wide(&port_name);
        let mut driver_wide = wide(GENERIC_TEXT_ONLY_DRIVER);
        let mut print_processor_wide = wide("WinPrint");
        let mut datatype_wide = wide(RAW_DATATYPE);
        let info = PRINTER_INFO_2W {
            pServerName: if server_name.is_empty() { PWSTR::null() } else { PWSTR(server_name.as_mut_ptr()) },
            pPrinterName: PWSTR(target_wide.as_mut_ptr()),
            pShareName: PWSTR::null(),
            pPortName: PWSTR(port_wide.as_mut_ptr()),
            pDriverName: PWSTR(driver_wide.as_mut_ptr()),
            pComment: PWSTR::null(),
            pLocation: PWSTR::null(),
            pDevMode: ptr::null_mut(),
            pSepFile: PWSTR::null(),
            pPrintProcessor: PWSTR(print_processor_wide.as_mut_ptr()),
            pDatatype: PWSTR(datatype_wide.as_mut_ptr()),
            pParameters: PWSTR::null(),
            pSecurityDescriptor: PSECURITY_DESCRIPTOR(ptr::null_mut()),
            Attributes: PRINTER_ATTRIBUTE_LOCAL,
            Priority: 1,
            DefaultPriority: 1,
            StartTime: 0,
            UntilTime: 0,
            Status: 0,
            cJobs: 0,
            AveragePPM: 0,
        };
        let add_result = unsafe {
            AddPrinterW(PCWSTR::null(), 2, (&info as *const PRINTER_INFO_2W).cast::<u8>())
        };
        let add_error = if add_result.is_err() { last_error_code() } else { 0 };
        log_api("AddPrinterW", bool_code(add_result.is_ok()), add_error, "create Generic / Text Only raw target");
        let handle: HANDLE = match add_result {
            Ok(handle) => handle,
            Err(_) => {
                let (code, message, action) = if add_error == 5 {
                    (
                        "RAW_ADMIN_REQUIRED",
                        "Windows denied creation of the Generic / Text Only target (administrator permission is required).",
                        "Approve administrator access for this queue-creation action only; ordinary printing does not require elevation.",
                    )
                } else if add_error == 1797 || add_error == 1798 {
                    (
                        "RAW_GENERIC_DRIVER_MISSING",
                        "The Generic / Text Only driver is not installed in Windows.",
                        "Install the built-in Generic / Text Only driver through Windows printer settings, then retry. MenuVex will not install or replace drivers.",
                    )
                } else {
                    (
                        "RAW_TARGET_CREATE_FAILED",
                        "Windows could not create the Generic / Text Only raw target.",
                        "Check Windows printer permissions and confirm the Generic / Text Only driver is installed.",
                    )
                };
                let mut error = api_error("AddPrinterW", code, add_error, false, false);
                error.message = format!("{message} Win32 error {add_error}.");
                error.printer = Some(queue_name.clone());
                error.action_required = Some(action.into());
                return Err(error);
            }
        };
        // AddPrinterW returns an open printer handle. Close it immediately; the new queue remains.
        let close_result = unsafe { ClosePrinter(PRINTER_HANDLE { Value: handle.0 }) };
        let close_error = if close_result.is_err() { last_error_code() } else { 0 };
        log_api("ClosePrinter", bool_code(close_result.is_ok()), close_error, "close created target");
        let verified = inspect_printer(&queue_name)?;
        Ok(RawTargetCreated {
            queue_name,
            driver_name: verified.driver_name.unwrap_or_else(|| GENERIC_TEXT_ONLY_DRIVER.into()),
            port_name,
            created: true,
        })
    }

    struct Win32Spooler;

    impl RawSpoolerApi for Win32Spooler {
        type Handle = PRINTER_HANDLE;

        fn open_printer(&mut self, queue_name: &str, force_raw: bool) -> Result<Self::Handle> {
            open(queue_name, force_raw)
        }

        fn start_doc(&mut self, handle: &Self::Handle, datatype: &str) -> Result<u32> {
            let mut doc_name = wide("MenuVex ESC/POS");
            let mut datatype = wide(datatype);
            let info = DOC_INFO_1W {
                pDocName: PWSTR(doc_name.as_mut_ptr()),
                pOutputFile: PWSTR::null(),
                // Windows' datatype match is case-sensitive. Keep this exactly "RAW".
                pDatatype: PWSTR(datatype.as_mut_ptr()),
            };
            let job_id = unsafe { StartDocPrinterW(*handle, 1, &info) };
            let error_code = if job_id == 0 { last_error_code() } else { 0 };
            log_api("StartDocPrinterW", job_id, error_code, "start RAW document");
            if job_id == 0 {
                Err(api_error(
                    "StartDocPrinterW",
                    "RAW_SPOOLER_STARTDOC_FAILED",
                    error_code,
                    true,
                    false,
                ))
            } else {
                Ok(job_id)
            }
        }

        fn start_page(&mut self, handle: &Self::Handle) -> Result<()> {
            let result = unsafe { StartPagePrinter(*handle) };
            let error_code = if result.0 == 0 { last_error_code() } else { 0 };
            log_api("StartPagePrinter", bool_code(result.0 != 0), error_code, "start page");
            if result.0 == 0 {
                Err(api_error(
                    "StartPagePrinter",
                    "RAW_SPOOLER_STARTPAGE_FAILED",
                    error_code,
                    false,
                    true,
                ))
            } else {
                Ok(())
            }
        }

        fn write(&mut self, handle: &Self::Handle, bytes: &[u8]) -> Result<usize> {
            let count = u32::try_from(bytes.len()).map_err(|_|
                AgentError::new("RAW_PAYLOAD_TOO_LARGE", "RAW spooler document exceeds the Win32 byte-count limit")
            )?;
            let mut written = 0u32;
            let result = unsafe {
                WritePrinter(*handle, bytes.as_ptr().cast::<c_void>(), count, &mut written)
            };
            let error_code = if result.0 == 0 { last_error_code() } else { 0 };
            if result.0 == 0 {
                tracing::error!(
                    target: "raw_print",
                    event = "WIN32_SPOOLER_API",
                    api = "WritePrinter",
                    return_code = 0u32,
                    win32_error_code = error_code,
                    requested_bytes = count,
                    written_bytes = written,
                    "Win32 RAW spooler API call"
                );
                let mut error = api_error(
                    "WritePrinter",
                    "RAW_SPOOLER_WRITE_FAILED",
                    error_code,
                    false,
                    true,
                );
                error.message = format!("WritePrinter failed after reporting {written} of {count} bytes (Win32 error {error_code})");
                return Err(error);
            }
            tracing::info!(
                target: "raw_print",
                event = "WIN32_SPOOLER_API",
                api = "WritePrinter",
                return_code = 1u32,
                win32_error_code = 0u32,
                requested_bytes = count,
                written_bytes = written,
                "Win32 RAW spooler API call"
            );
            Ok(written as usize)
        }

        fn end_page(&mut self, handle: &Self::Handle) -> Result<()> {
            let result = unsafe { EndPagePrinter(*handle) };
            let error_code = if result.0 == 0 { last_error_code() } else { 0 };
            log_api("EndPagePrinter", bool_code(result.0 != 0), error_code, "end page");
            if result.0 == 0 {
                Err(api_error(
                    "EndPagePrinter",
                    "RAW_SPOOLER_ENDPAGE_FAILED",
                    error_code,
                    false,
                    true,
                ))
            } else {
                Ok(())
            }
        }

        fn end_doc(&mut self, handle: &Self::Handle) -> Result<()> {
            let result = unsafe { EndDocPrinter(*handle) };
            let error_code = if result.0 == 0 { last_error_code() } else { 0 };
            log_api("EndDocPrinter", bool_code(result.0 != 0), error_code, "end document");
            if result.0 == 0 {
                Err(api_error(
                    "EndDocPrinter",
                    "RAW_SPOOLER_ENDDOC_FAILED",
                    error_code,
                    false,
                    true,
                ))
            } else {
                Ok(())
            }
        }

        fn close_printer(&mut self, handle: &Self::Handle) -> Result<()> {
            let result = unsafe { ClosePrinter(*handle) };
            let error_code = if result.is_err() { last_error_code() } else { 0 };
            log_api("ClosePrinter", bool_code(result.is_ok()), error_code, "close printer");
            result.map_err(|_| api_error(
                "ClosePrinter",
                "RAW_SPOOLER_CLOSE_FAILED",
                error_code,
                false,
                true,
            ))
        }
    }

    pub fn print_raw(queue_name: &str, bytes: &[u8], force_raw: bool) -> Result<usize> {
        print_raw_with_api(&mut Win32Spooler, queue_name, bytes, force_raw)
    }
}

#[cfg(not(windows))]
mod windows_impl {
    // This module is never selected off Windows; keeping the signatures here would accidentally
    // create a second transport. `print_raw` falls through to the existing CUPS path above.
}

#[cfg(test)]
mod tests {
    use super::*;

    #[derive(Default)]
    struct MockSpooler {
        calls: Vec<String>,
        received: Vec<u8>,
        datatype: Option<String>,
        force_raw: Option<bool>,
        short_write: bool,
    }

    impl RawSpoolerApi for MockSpooler {
        type Handle = ();

        fn open_printer(&mut self, queue: &str, force_raw: bool) -> Result<Self::Handle> {
            self.calls.push(format!("OpenPrinterW:{queue}"));
            self.force_raw = Some(force_raw);
            Ok(())
        }

        fn start_doc(&mut self, _: &Self::Handle, datatype: &str) -> Result<u32> {
            self.calls.push("StartDocPrinterW".into());
            self.datatype = Some(datatype.to_owned());
            Ok(17)
        }

        fn start_page(&mut self, _: &Self::Handle) -> Result<()> {
            self.calls.push("StartPagePrinter".into());
            Ok(())
        }

        fn write(&mut self, _: &Self::Handle, bytes: &[u8]) -> Result<usize> {
            self.calls.push("WritePrinter".into());
            let count = if self.short_write { bytes.len().saturating_sub(1) } else { bytes.len() };
            self.received.extend_from_slice(&bytes[..count]);
            Ok(count)
        }

        fn end_page(&mut self, _: &Self::Handle) -> Result<()> {
            self.calls.push("EndPagePrinter".into());
            Ok(())
        }

        fn end_doc(&mut self, _: &Self::Handle) -> Result<()> {
            self.calls.push("EndDocPrinter".into());
            Ok(())
        }

        fn close_printer(&mut self, _: &Self::Handle) -> Result<()> {
            self.calls.push("ClosePrinter".into());
            Ok(())
        }
    }

    #[test]
    fn raw_pipeline_uses_case_sensitive_raw_and_forwards_exact_bytes_once() {
        let bytes = [0x1b, 0x40, 0x1d, 0x76, 0x30, 0x00, 0xff, 0x7f];
        let mut spooler = MockSpooler::default();
        let written = print_raw_with_api(&mut spooler, "POS-80C (copy 2)", &bytes, true).unwrap();
        assert_eq!(written, bytes.len());
        assert_eq!(spooler.received, bytes, "the RAW pipeline must not alter any byte");
        assert_eq!(spooler.datatype.as_deref(), Some("RAW"));
        assert_eq!(spooler.force_raw, Some(true));
        assert_eq!(
            spooler.calls,
            [
                "OpenPrinterW:POS-80C (copy 2)",
                "StartDocPrinterW",
                "StartPagePrinter",
                "WritePrinter",
                "EndPagePrinter",
                "EndDocPrinter",
                "ClosePrinter",
            ]
        );
    }

    #[test]
    fn incomplete_write_is_uncertain_and_never_reports_success() {
        let mut spooler = MockSpooler { short_write: true, ..MockSpooler::default() };
        let error = print_raw_with_api(&mut spooler, "POS-80", &[1, 2, 3], false).unwrap_err();
        assert_eq!(error.code, "RAW_SPOOLER_PARTIAL_WRITE");
        assert!(error.uncertain);
        assert!(!error.retryable);
        assert!(spooler.calls.ends_with(&[
            "EndPagePrinter".into(),
            "EndDocPrinter".into(),
            "ClosePrinter".into(),
        ]));
    }

    #[test]
    fn raw_log_preview_never_includes_more_than_32_bytes() {
        let bytes = (0..64).collect::<Vec<u8>>();
        let preview = hex_preview(&bytes);
        assert_eq!(preview.split_whitespace().count(), 32);
        assert!(preview.starts_with("00 01 02 03"));
        assert!(!preview.contains("20"), "the 33rd byte must not be logged");
    }
}
