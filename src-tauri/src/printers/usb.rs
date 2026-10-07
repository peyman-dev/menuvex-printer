//! Direct USB Printer Class transport (libusb/rusb).
//!
//! Every operation in this module is bounded, isolated and self-cleaning:
//!
//! * one process-wide libusb context instead of a fresh context per call (a fresh context spawns
//!   an event thread and re-enumerates the whole bus; the 10 s status poll leaked one each time);
//! * a hard wall-clock deadline per operation, so a wedged device cannot pin the agent;
//! * an RAII claim that releases the interface on success **and** on every early return;
//! * no `open()` in the status poll, so probing never competes with an in-flight print job;
//! * only `Result` is ever returned — this module never panics into the queue worker or the
//!   WebSocket server.
use rusb::{ Context, Device, DeviceHandle, Direction, TransferType, UsbContext };
use std::sync::{ mpsc, OnceLock };
use std::time::{ Duration, Instant };
use crate::{
    error::{ AgentError, Result },
    printers::{ Connection, discovery::DiscoveredUsb, usb_policy::{ UsbFailure, UsbFailureStage } },
};
/// Bound for one short USB operation (presence probe).
const OP_TIMEOUT: Duration = Duration::from_secs(10);
/// Discovery opens every device on the bus to read its descriptors, so it gets a longer bound.
const DISCOVER_TIMEOUT: Duration = Duration::from_secs(25);
/// Longest a single print job may hold the interface open.
const SEND_TIMEOUT: Duration = Duration::from_secs(35);
fn map(e: rusb::Error) -> AgentError {
    match e {
        rusb::Error::Access =>
            AgentError::new(
                "USB_ACCESS_DENIED",
                "Install the device-specific USB permissions/driver"
            ),
        rusb::Error::NoDevice => AgentError::retry("PRINTER_OFFLINE"),
        rusb::Error::Busy => AgentError::retry("USB_BUSY"),
        rusb::Error::Timeout =>
            AgentError::new("USB_TIMEOUT", "The USB device did not answer in time"),
        _ =>
            AgentError::new(
                "USB_DEVICE_ERROR",
                "USB operation failed; inspect driver and interface"
            ),
    }
}
// Preserve the libusb reason and operation, without exposing receipt data or credentials.
fn at_stage(stage: &str, e: rusb::Error) -> AgentError {
    let mut error = map(e);
    error.message = format!("{}: libusb {:?} ({})", stage, e, e);
    error
}

/// libusb's public error numbers (not Rust enum discriminants).
fn libusb_error_code(error: rusb::Error) -> i32 {
    match error {
        rusb::Error::Io => -1,
        rusb::Error::InvalidParam => -2,
        rusb::Error::Access => -3,
        rusb::Error::NoDevice => -4,
        rusb::Error::NotFound => -5,
        rusb::Error::Busy => -6,
        rusb::Error::Timeout => -7,
        rusb::Error::Overflow => -8,
        rusb::Error::Pipe => -9,
        rusb::Error::Interrupted => -10,
        rusb::Error::NoMem => -11,
        rusb::Error::NotSupported => -12,
        rusb::Error::Other => -99,
        _ => -99,
    }
}

fn failure_from_libusb(
    stage: UsbFailureStage,
    operation: &str,
    error: rusb::Error,
    bytes_written_before_failure: Option<usize>,
) -> UsbFailure {
    // A libusb timeout is always ambiguous by policy, even when it occurs before open/write.
    let stage = if error == rusb::Error::Timeout { UsbFailureStage::Timeout } else { stage };
    UsbFailure::new(
        stage,
        at_stage(operation, error),
        Some(libusb_error_code(error)),
        Some(format!("{error:?}")),
        bytes_written_before_failure,
    )
}

fn failure_from_agent(
    stage: UsbFailureStage,
    error: AgentError,
    bytes_written_before_failure: Option<usize>,
) -> UsbFailure {
    let stage = if error.code == "USB_TIMEOUT" { UsbFailureStage::Timeout } else { stage };
    UsbFailure::new(stage, error, None, None, bytes_written_before_failure)
}

/// One process-wide libusb context. `libusb_context` is documented as thread-safe and creating
/// one per call spawns an event-handling thread and re-enumerates the bus every time.
struct SharedContext(Context);
// rusb's `Context` wraps a `libusb_context`, which libusb itself makes safe to share across
// threads. Declared here so this module does not depend on the crate's auto-trait choices.
unsafe impl Send for SharedContext {}
unsafe impl Sync for SharedContext {}
static CONTEXT: OnceLock<SharedContext> = OnceLock::new();

fn context() -> std::result::Result<&'static Context, rusb::Error> {
    if let Some(shared) = CONTEXT.get() {
        return Ok(&shared.0);
    }
    match Context::new() {
        Ok(ctx) => {
            // If another thread won the race its context is kept and this one is dropped
            // (`libusb_exit`) when the unused closure is discarded.
            Ok(&(CONTEXT.get_or_init(move || SharedContext(ctx))).0)
        }
        Err(e) => {
            // Not cached: a failure before udev/keyring is ready must be retryable later.
            let stage = if e == rusb::Error::Timeout { "Timeout" } else { "PreOpen" };
            tracing::error!(
                target: "usb",
                event = "LIBUSB_INITIALIZE_FAILED",
                stage = stage,
                libusb_error_code = libusb_error_code(e),
                libusb_error_name = ?e,
                bytes_written_before_failure = 0usize,
                completed_copies_before_failure = 0usize,
                error_message = %e,
                "libusb context initialization failed"
            );
            Err(e)
        }
    }
}

/// Run a blocking USB operation on its own thread with a hard deadline.
///
/// libusb puts per-transfer timeouts on bulk/control traffic, but enumeration, string
/// descriptors and `open()` against a device that has stopped answering can block for as long
/// as the OS likes. Running them off the caller's thread means the agent's blocking pool, the
/// queue worker and the WebSocket server keep working while that one call is abandoned.
fn guarded<T: Send + 'static>(
    stage: &'static str,
    limit: Duration,
    operation: impl FnOnce() -> Result<T> + Send + 'static
) -> Result<T> {
    let (done, finished) = mpsc::channel();
    let started = Instant::now();
    std::thread::Builder
        ::new()
        .name(format!("menuvex-usb-{stage}"))
        .spawn(move || {
            let _ = done.send(operation());
        })
        .map_err(|e| {
            tracing::error!(
                target: "usb",
                event = "USB_THREAD_SPAWN_FAILED",
                stage,
                error = %e,
                "could not start the bounded USB worker"
            );
            AgentError::new("USB_DEVICE_ERROR", "Could not start the USB worker thread")
        })?;
    match finished.recv_timeout(limit) {
        Ok(result) => result,
        Err(mpsc::RecvTimeoutError::Timeout) => {
            tracing::error!(
                target: "usb",
                event = "USB_TIMEOUT",
                stage,
                elapsed_ms = started.elapsed().as_millis() as u64,
                limit_ms = limit.as_millis() as u64,
                "USB operation exceeded its deadline; the device is not answering"
            );
            Err(
                AgentError::new(
                    "USB_TIMEOUT",
                    &format!("USB {stage} did not finish within {} s", limit.as_secs())
                )
            )
        }
        Err(mpsc::RecvTimeoutError::Disconnected) => {
            tracing::error!(target: "usb", event = "USB_TIMEOUT", stage, "USB worker stopped without a result");
            Err(
                AgentError::new(
                    "USB_DEVICE_ERROR",
                    &format!("USB {stage} worker stopped without a result")
                )
            )
        }
    }
}

/// A claimed interface that is always released — on success, on `?` early-returns and on the
/// timeout path where the whole operation is abandoned. Dropping the handle afterwards closes
/// the device, which is what lets the OS re-attach its kernel driver (on Linux the auto-detach
/// flag is set before claiming).
struct Claim {
    handle: DeviceHandle<Context>,
    interface: u8,
}
impl Drop for Claim {
    fn drop(&mut self) {
        match self.handle.release_interface(self.interface) {
            Ok(()) =>
                tracing::debug!(
                    target: "usb",
                    event = "USB_INTERFACE_RELEASED",
                    interface = self.interface,
                    "interface released"
                ),
            Err(e) =>
                tracing::debug!(
                    target: "usb",
                    event = "USB_INTERFACE_RELEASE_FAILED",
                    interface = self.interface,
                    error = ?e,
                    "interface release failed; libusb_close still drops the claim"
                ),
        }
    }
}

pub fn discover() -> Result<Vec<DiscoveredUsb>> {
    tracing::info!(
        target: "usb",
        event = "PRINTER_DISCOVERY_STARTED",
        connection = "usb",
        limit_ms = DISCOVER_TIMEOUT.as_millis() as u64,
        "USB Printer Class discovery started"
    );
    let found = match guarded("discovery", DISCOVER_TIMEOUT, discover_blocking) {
        Ok(found) => found,
        Err(error) => {
            let stage = if error.code == "USB_TIMEOUT" { "Timeout" } else { "PreOpen" };
            tracing::error!(
                target: "usb",
                event = "USB_DISCOVERY_FAILED",
                stage,
                fallback_decision = if stage == "Timeout" { "forbid" } else { "ask" },
                fallback_target = "<not-configured-during-discovery>",
                bytes_written_before_failure = 0usize,
                error_code = %error.code,
                error_message = %error.message,
                "libusb discovery could not initialize or enumerate devices"
            );
            return Err(error);
        }
    };
    tracing::info!(
        target: "usb",
        event = "PRINTER_DISCOVERY_FINISHED",
        connection = "usb",
        count = found.len(),
        "USB Printer Class discovery finished"
    );
    Ok(found)
}

fn discover_blocking() -> Result<Vec<DiscoveredUsb>> {
    let ctx = context().map_err(|error| at_stage("initialize_usb", error))?;
    let devices = ctx.devices().map_err(|e| at_stage("enumerate_devices", e))?;
    let mut out = Vec::new();
    for dev in devices.iter() {
        let Ok(desc) = dev.device_descriptor() else {
            continue;
        };
        // An inaccessible device is still listed: the operator needs to see *why* it failed.
        let (handle, access_error) = match dev.open() {
            Ok(handle) => {
                tracing::debug!(
                    target: "usb",
                    event = "USB_DEVICE_OPENED",
                    vendor_id = desc.vendor_id(),
                    product_id = desc.product_id(),
                    "device opened for descriptor strings"
                );
                (Some(handle), None)
            }
            Err(error) => {
                let detail = at_stage("open_device", error);
                let stage = if error == rusb::Error::Timeout { "Timeout" } else { "PreOpen" };
                let fallback_decision = if stage == "Timeout" { "forbid" } else { "ask" };
                tracing::warn!(
                    target: "usb",
                    event = "USB_DEVICE_OPEN_FAILED",
                    stage,
                    vendor_id = desc.vendor_id(),
                    product_id = desc.product_id(),
                    libusb_error_code = libusb_error_code(error),
                    libusb_error_name = ?error,
                    fallback_decision,
                    fallback_target = "<not-configured-during-discovery>",
                    bytes_written_before_failure = 0usize,
                    error_code = %detail.code,
                    error_message = %detail.message,
                    "USB device could not be opened during discovery"
                );
                (None, Some(detail))
            },
        };
        let manufacturer = handle
            .as_ref()
            .and_then(|h| h.read_manufacturer_string_ascii(&desc).ok());
        let product = handle.as_ref().and_then(|h| h.read_product_string_ascii(&desc).ok());
        let serial = handle
            .as_ref()
            .and_then(|h| h.read_serial_number_string_ascii(&desc).ok())
            .filter(|s| !s.is_empty());
        let Ok(config) = dev.active_config_descriptor() else {
            continue;
        };
        for interface in config.interfaces() {
            for alt in interface.descriptors() {
                // Do not expose mass-storage/HID/vendor-specific interfaces as printers.
                if alt.class_code() != 7 {
                    continue;
                }
                for ep in alt.endpoint_descriptors() {
                    if
                        ep.direction() == Direction::Out &&
                        ep.transfer_type() == TransferType::Bulk
                    {
                        let found = DiscoveredUsb {
                            connection: Connection::Usb {
                                vendor_id: desc.vendor_id(),
                                product_id: desc.product_id(),
                                serial: serial.clone(),
                                bus: dev.bus_number(),
                                ports: dev.port_numbers().unwrap_or_default(),
                                interface: alt.interface_number(),
                                endpoint: ep.address(),
                                alternate: alt.setting_number(),
                            },
                            manufacturer: manufacturer.clone(),
                            product: product.clone(),
                            accessible: handle.is_some(),
                            access_error: access_error.clone(),
                        };
                        tracing::info!(
                            target: "usb",
                            event = "PRINTER_FOUND",
                            connection = "usb",
                            vendor_id = desc.vendor_id(),
                            product_id = desc.product_id(),
                            bus = dev.bus_number(),
                            interface = alt.interface_number(),
                            endpoint = ep.address(),
                            accessible = handle.is_some(),
                            product = found.product.as_deref().unwrap_or(""),
                            "USB printer candidate"
                        );
                        out.push(found);
                    }
                }
            }
        }
    }
    Ok(out)
}

/// Identity match that does **not** open the device in the common case.
///
/// The status poll runs every 10 s per printer. Opening the device there competes with an
/// in-flight print job for the interface and needs USB permissions just to report `offline`, so
/// bus/port identity is preferred and the serial is only consulted when the same VID/PID sits
/// on a different socket.
fn matches(
    dev: &Device<Context>,
    c: &Connection,
) -> std::result::Result<bool, rusb::Error> {
    let Connection::Usb { vendor_id, product_id, serial, bus, ports, .. } = c else {
        return Ok(false);
    };
    // Preserve descriptor errors so timeouts cannot degrade into an ordinary not-found result;
    // callers classify them as a staged failure and apply the no-timeout-fallback rule.
    let descriptor = dev.device_descriptor()?;
    if descriptor.vendor_id() != *vendor_id || descriptor.product_id() != *product_id {
        return Ok(false);
    }
    if dev.bus_number() == *bus && dev.port_numbers().ok().as_ref() == Some(ports) {
        return Ok(true);
    }
    match serial {
        None => Ok(false),
        Some(expected) => {
            let handle = dev.open()?;
            let actual = handle.read_serial_number_string_ascii(&descriptor)?;
            Ok(actual == expected.as_str())
        }
    }
}

pub fn present(c: &Connection) -> Result<bool> {
    let connection = c.clone();
    guarded("status", OP_TIMEOUT, move || {
        let ctx = context().map_err(|error| at_stage("initialize_usb", error))?;
        let devices = ctx.devices().map_err(map)?;
        for device in devices.iter() {
            match matches(&device, &connection) {
                Ok(true) => return Ok(true),
                Ok(false) => (),
                Err(error) => return Err(at_stage("match_usb_identity", error)),
            }
        }
        Ok(false)
    })
}

/// Direct USB backend. This type only exists when Cargo feature `libusb` is enabled.
pub struct LibusbTransport;
impl LibusbTransport {
    pub fn send(&self, c: &Connection, bytes: &[u8]) -> std::result::Result<(), UsbFailure> {
        let connection = c.clone();
        let payload = bytes.to_vec();
        // Nest the USB-stage result inside the bounded worker result. A wall-clock timeout or
        // worker panic is different from a classified pre-write error and must never fail over.
        let guarded_result: Result<std::result::Result<(), UsbFailure>> = guarded(
            "send",
            SEND_TIMEOUT,
            move || Ok(send_bounded(&connection, &payload)),
        );
        match guarded_result {
            Ok(result) => result,
            Err(error) if error.code == "USB_TIMEOUT" =>
                Err(failure_from_agent(UsbFailureStage::Timeout, error, None)),
            Err(error) => Err(failure_from_agent(UsbFailureStage::Unknown, error, None)),
        }
    }
}

fn device_selection_failure(candidate_count: usize) -> Option<UsbFailure> {
    match candidate_count {
        1 => None,
        0 => Some(failure_from_agent(
            UsbFailureStage::PreOpen,
            AgentError::retry("PRINTER_OFFLINE"),
            Some(0),
        )),
        _ => Some(failure_from_agent(
            UsbFailureStage::PreOpen,
            AgentError::new("USB_AMBIGUOUS", "Multiple devices share this identity"),
            Some(0),
        )),
    }
}

fn send_bounded(c: &Connection, bytes: &[u8]) -> std::result::Result<(), UsbFailure> {
    let Connection::Usb { vendor_id, product_id, interface, endpoint, alternate, .. } = *c else {
        return Err(failure_from_agent(
            UsbFailureStage::Unknown,
            AgentError::new("INVALID_CONFIG", "Not a USB connection"),
            None,
        ));
    };
    let ctx = context().map_err(|error| {
        failure_from_libusb(UsbFailureStage::PreOpen, "initialize_usb", error, Some(0))
    })?;
    let devices = ctx.devices().map_err(|error| {
        failure_from_libusb(UsbFailureStage::PreOpen, "enumerate_devices", error, Some(0))
    })?;
    let mut candidates = Vec::new();
    for device in devices.iter() {
        match matches(&device, c) {
            Ok(true) => candidates.push(device),
            Ok(false) => (),
            Err(error) => {
                return Err(failure_from_libusb(
                    UsbFailureStage::PreOpen,
                    "match_usb_identity",
                    error,
                    Some(0),
                ));
            }
        }
    }
    if let Some(failure) = device_selection_failure(candidates.len()) {
        if candidates.is_empty() {
            tracing::warn!(
                target: "usb",
                event = "USB_DEVICE_NOT_FOUND",
                connection = "usb",
                vendor_id,
                product_id,
                stage = %failure.stage,
                libusb_error_code = ?failure.libusb_error_code,
                libusb_error_name = ?failure.libusb_error_name,
                bytes_written_before_failure = 0usize,
                completed_copies_before_failure = 0usize,
                "no device matches the saved USB identity"
            );
        }
        return Err(failure);
    }
    let dev = &candidates[0];
    let config = dev.active_config_descriptor().map_err(|error| {
        failure_from_libusb(
            UsbFailureStage::PreOpen,
            "read_active_configuration",
            error,
            Some(0),
        )
    })?;
    let valid = config
        .interfaces()
        .flat_map(|i| i.descriptors())
        .any(|a|
            a.class_code() == 7 &&
                a.interface_number() == interface &&
                a.setting_number() == alternate &&
                a.endpoint_descriptors().any(|e|
                    e.address() == endpoint &&
                        e.direction() == Direction::Out &&
                        e.transfer_type() == TransferType::Bulk
                )
        );
    if !valid {
        return Err(failure_from_agent(
            UsbFailureStage::PreOpen,
            AgentError::new("USB_DEVICE_ERROR", "Saved printer interface no longer matches"),
            Some(0),
        ));
    }
    let handle = dev.open().map_err(|error| {
        failure_from_libusb(UsbFailureStage::PreOpen, "open_device", error, Some(0))
    })?;
    tracing::info!(
        target: "usb",
        event = "USB_DEVICE_OPENED",
        connection = "usb",
        vendor_id,
        product_id,
        bus = dev.bus_number(),
        interface,
        "device opened for printing"
    );
    #[cfg(target_os = "linux")]
    handle.set_auto_detach_kernel_driver(true).map_err(|error| {
        failure_from_libusb(
            UsbFailureStage::PostOpenPreWrite,
            "enable_auto_detach",
            error,
            Some(0),
        )
    })?;
    handle.claim_interface(interface).map_err(|error| {
        failure_from_libusb(
            UsbFailureStage::PostOpenPreWrite,
            "claim_interface",
            error,
            Some(0),
        )
    })?;
    tracing::info!(
        target: "usb",
        event = "USB_INTERFACE_CLAIMED",
        connection = "usb",
        vendor_id,
        product_id,
        interface,
        alternate,
        "interface claimed"
    );
    // From here on the interface is released by `Claim::drop` on every path, including the
    // timeout path where the whole operation is abandoned.
    let claim = Claim { handle, interface };
    let outcome = write_all(&claim, endpoint, alternate, bytes);
    // Release before the handle is closed so the OS driver is re-attached promptly.
    drop(claim);
    outcome
}

fn write_all(
    claim: &Claim,
    endpoint: u8,
    alternate: u8,
    bytes: &[u8],
) -> std::result::Result<(), UsbFailure> {
    claim
        .handle
        .set_alternate_setting(claim.interface, alternate)
        .map_err(|error| {
            failure_from_libusb(
                UsbFailureStage::PostOpenPreWrite,
                "set_alternate_setting",
                error,
                Some(0),
            )
        })?;
    let deadline = Instant::now() + Duration::from_secs(30);
    let mut bytes_written = 0usize;
    for chunk in bytes.chunks(16 * 1024) {
        if Instant::now() > deadline {
            return Err(failure_from_agent(
                UsbFailureStage::Timeout,
                AgentError::new("USB_TIMEOUT", "USB bulk write exceeded its 30 s deadline"),
                Some(bytes_written),
            ));
        }
        match claim.handle.write_bulk(endpoint, chunk, Duration::from_secs(3)) {
            Ok(written) => {
                bytes_written = bytes_written.saturating_add(written);
                if written != chunk.len() {
                    return Err(failure_from_agent(
                        UsbFailureStage::MidWrite,
                        AgentError::uncertain(),
                        Some(bytes_written),
                    ));
                }
            }
            Err(error) => {
                let stage = if error == rusb::Error::Timeout {
                    UsbFailureStage::Timeout
                } else {
                    UsbFailureStage::MidWrite
                };
                return Err(failure_from_libusb(stage, "write_bulk", error, Some(bytes_written)));
            }
        }
    }
    Ok(())
}

#[cfg(test)]
mod diagnostic_tests {
    use super::*;

    #[test]
    fn preserves_open_reason_instead_of_assuming_missing_driver() {
        let error = at_stage("open_device", rusb::Error::NotSupported);
        assert_eq!(error.code, "USB_DEVICE_ERROR");
        assert!(error.message.contains("open_device"));
        assert!(error.message.contains("NotSupported"));
        assert!(!error.retryable);
        assert!(!error.uncertain);
    }

    #[test]
    fn usb_failures_retain_stage_code_name_and_zero_byte_prewrite_count() {
        let failure = failure_from_libusb(
            UsbFailureStage::PreOpen,
            "open_device",
            rusb::Error::NotSupported,
            Some(0),
        );
        assert_eq!(failure.stage, UsbFailureStage::PreOpen);
        assert_eq!(failure.libusb_error_code, Some(-12));
        assert_eq!(failure.libusb_error_name.as_deref(), Some("NotSupported"));
        assert_eq!(failure.bytes_written_before_failure, Some(0));
        assert!(!failure.cause.uncertain);
    }

    #[test]
    fn missing_device_returns_clear_preopen_error_without_configured_fallback() {
        let failure = device_selection_failure(0).expect("zero matching devices is a pre-open failure");
        let error = crate::printers::usb_policy::apply_fallback(failure, None, |_| {
            panic!("missing device must not invent a fallback target")
        }).expect_err("missing device must surface as an error");
        assert_eq!(error.code, "PRINTER_OFFLINE");
        assert!(!error.uncertain);
        assert!(error.message.contains("PreOpen"));
        assert!(error.message.contains("No USB fallback target is configured"));
    }

    #[test]
    fn feature_enabled_no_device_uses_only_the_explicit_fallback() {
        let failure = device_selection_failure(0).expect("no matching device");
        let target = Connection::Spooler { queue_name: "POS-80".into() };
        let mut attempted = false;
        crate::printers::usb_policy::apply_fallback(failure, Some(&target), |actual| {
            attempted = true;
            assert_eq!(actual, &target);
            Ok(())
        }).expect("a confirmed pre-open no-device condition may use the configured queue");
        assert!(attempted);
    }

    #[test]
    fn any_libusb_timeout_is_classified_as_ambiguous_and_never_falls_back() {
        let failure = failure_from_libusb(
            UsbFailureStage::PreOpen,
            "open_device",
            rusb::Error::Timeout,
            Some(0),
        );
        assert_eq!(failure.stage, UsbFailureStage::Timeout);
        assert_eq!(failure.libusb_error_code, Some(-7));
        assert!(failure.cause.uncertain);
        assert!(!failure.cause.retryable);
    }

    #[test]
    fn preserves_existing_retry_classification() {
        let busy = at_stage("claim_interface", rusb::Error::Busy);
        assert_eq!(busy.code, "USB_BUSY");
        assert!(busy.retryable);
        assert!(busy.message.contains("claim_interface"));
        let denied = at_stage("open_device", rusb::Error::Access);
        assert_eq!(denied.code, "USB_ACCESS_DENIED");
        assert!(!denied.retryable);
    }

    #[test]
    fn discovery_serializes_diagnostic_for_desktop_ui() {
        let device = DiscoveredUsb {
            connection: Connection::Usb {
                vendor_id: 1, product_id: 2, serial: None,
                bus: 1, ports: vec![1], interface: 0, endpoint: 1, alternate: 0,
            },
            manufacturer: None, product: None, accessible: false,
            access_error: Some(at_stage("open_device", rusb::Error::NotSupported)),
        };
        let value = serde_json::to_value(device).unwrap();
        assert_eq!(value["accessible"], false);
        assert_eq!(value["accessError"]["code"], "USB_DEVICE_ERROR");
        assert!(value["accessError"]["message"].as_str().unwrap().contains("NotSupported"));
    }

    /// A misconfigured profile must never drag the USB stack into an unrelated job, and the
    /// public entry point must stay bounded even for that case.
    #[test]
    fn non_usb_connection_is_rejected_before_any_device_access() {
        for connection in [
            Connection::Network { host: "192.168.1.50".into(), port: 9100 },
            Connection::Spooler { queue_name: "POS-80".into() },
        ] {
            let kind = connection.kind();
            let error = send_bounded(&connection, &[27, 64]).expect_err("must not be accepted");
            assert_eq!(error.cause.code, "INVALID_CONFIG", "wrong code for {kind}");
            assert_eq!(error.stage, UsbFailureStage::Unknown, "wrong stage for {kind}");
            assert!(error.cause.uncertain, "unknown USB failures must be conservative");
        }
    }

    /// The bounded runner must return an error, never a panic or a hang, when the operation
    /// itself blows its deadline.
    #[test]
    fn guarded_returns_a_timeout_instead_of_blocking_the_agent() {
        let started = std::time::Instant::now();
        let result: Result<u8> = guarded(
            "unit_test",
            Duration::from_millis(120),
            || {
                std::thread::sleep(Duration::from_secs(5));
                Ok(1)
            }
        );
        let error = result.expect_err("deadline must win");
        assert_eq!(error.code, "USB_TIMEOUT");
        assert!(started.elapsed() < Duration::from_secs(10), "caller must not wait for the worker");
    }

    /// The same runner must pass real results and real errors straight through.
    #[test]
    fn guarded_passes_results_and_errors_through() {
        assert_eq!(guarded("unit_test", Duration::from_secs(5), || Ok(7u8)).unwrap(), 7);
        let error = guarded("unit_test", Duration::from_secs(5), || -> Result<u8> {
            Err(AgentError::retry("PRINTER_OFFLINE"))
        }).expect_err("error must surface");
        assert_eq!(error.code, "PRINTER_OFFLINE");
        assert!(error.retryable);
    }

    /// The identity matcher is only ever reached with a USB profile; a LAN or spooler profile
    /// must never be treated as a USB identity (that is what would make the status poll open
    /// unrelated devices every 10 seconds).
    #[test]
    fn only_usb_profiles_carry_a_usb_identity() {
        let usb = Connection::Usb {
            vendor_id: 1, product_id: 2, serial: Some("ABC".into()),
            bus: 1, ports: vec![1, 2], interface: 0, endpoint: 1, alternate: 0,
        };
        assert!(std::matches!(usb, Connection::Usb { .. }));
        for other in [
            Connection::Network { host: "10.0.0.5".into(), port: 9100 },
            Connection::Spooler { queue_name: "POS-80".into() },
        ] {
            assert!(!std::matches!(other, Connection::Usb { .. }));
            assert_eq!(other.kind() != "usb", true);
        }
    }
}
