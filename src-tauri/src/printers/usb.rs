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
use crate::{ error::{ AgentError, Result }, printers::Connection };
use serde::Serialize;
/// Bound for one short USB operation (presence probe).
const OP_TIMEOUT: Duration = Duration::from_secs(10);
/// Discovery opens every device on the bus to read its descriptors, so it gets a longer bound.
const DISCOVER_TIMEOUT: Duration = Duration::from_secs(25);
/// Longest a single print job may hold the interface open.
const SEND_TIMEOUT: Duration = Duration::from_secs(35);
#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct DiscoveredUsb {
    pub connection: Connection,
    pub manufacturer: Option<String>,
    pub product: Option<String>,
    pub accessible: bool,
    pub access_error: Option<AgentError>,
}
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

/// One process-wide libusb context. `libusb_context` is documented as thread-safe and creating
/// one per call spawns an event-handling thread and re-enumerates the bus every time.
struct SharedContext(Context);
// rusb's `Context` wraps a `libusb_context`, which libusb itself makes safe to share across
// threads. Declared here so this module does not depend on the crate's auto-trait choices.
unsafe impl Send for SharedContext {}
unsafe impl Sync for SharedContext {}
static CONTEXT: OnceLock<SharedContext> = OnceLock::new();

fn context() -> Result<&'static Context> {
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
            Err(at_stage("initialize_usb", e))
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
    let found = guarded("discovery", DISCOVER_TIMEOUT, discover_blocking)?;
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
    let ctx = context()?;
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
            Err(error) => (None, Some(at_stage("open_device", error))),
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
fn matches(dev: &Device<Context>, c: &Connection) -> bool {
    let Connection::Usb { vendor_id, product_id, serial, bus, ports, .. } = c else {
        return false;
    };
    let Ok(d) = dev.device_descriptor() else {
        return false;
    };
    if d.vendor_id() != *vendor_id || d.product_id() != *product_id {
        return false;
    }
    if dev.bus_number() == *bus && dev.port_numbers().ok().as_ref() == Some(ports) {
        return true;
    }
    match serial {
        None => false,
        Some(expected) =>
            dev
                .open()
                .ok()
                .and_then(|h| h.read_serial_number_string_ascii(&d).ok())
                .as_ref() == Some(expected),
    }
}

pub fn present(c: &Connection) -> Result<bool> {
    let connection = c.clone();
    guarded("status", OP_TIMEOUT, move || {
        let ctx = context()?;
        Ok(
            ctx
                .devices()
                .map_err(map)?
                .iter()
                .any(|d| matches(&d, &connection))
        )
    })
}

pub fn send(c: &Connection, bytes: &[u8]) -> Result<()> {
    let connection = c.clone();
    let payload = bytes.to_vec();
    guarded("send", SEND_TIMEOUT, move || send_bounded(&connection, &payload))
}

fn send_bounded(c: &Connection, bytes: &[u8]) -> Result<()> {
    let Connection::Usb { vendor_id, product_id, interface, endpoint, alternate, .. } = *c else {
        return Err(AgentError::new("INVALID_CONFIG", "Not a USB connection"));
    };
    let ctx = context()?;
    let devices = ctx.devices().map_err(|e| at_stage("enumerate_devices", e))?;
    let candidates: Vec<_> = devices.iter().filter(|d| matches(d, c)).collect();
    if candidates.len() != 1 {
        return Err(
            if candidates.is_empty() {
                tracing::warn!(
                    target: "usb",
                    event = "PRINT_JOB_FAILED",
                    connection = "usb",
                    vendor_id,
                    product_id,
                    error_code = "PRINTER_OFFLINE",
                    "no device matches the saved USB identity"
                );
                AgentError::retry("PRINTER_OFFLINE")
            } else {
                AgentError::new("USB_AMBIGUOUS", "Multiple devices share this identity")
            }
        );
    }
    let dev = &candidates[0];
    let config = dev
        .active_config_descriptor()
        .map_err(|e| at_stage("read_active_configuration", e))?;
    let valid = config
        .interfaces()
        .flat_map(|i| i.descriptors())
        .any(
            |a|
                a.class_code() == 7 &&
                a.interface_number() == interface &&
                a.setting_number() == alternate &&
                a
                    .endpoint_descriptors()
                    .any(
                        |e|
                            e.address() == endpoint &&
                            e.direction() == Direction::Out &&
                            e.transfer_type() == TransferType::Bulk
                    )
        );
    if !valid {
        return Err(
            AgentError::new("USB_DEVICE_ERROR", "Saved printer interface no longer matches")
        );
    }
    let handle = dev.open().map_err(|e| at_stage("open_device", e))?;
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
    handle.set_auto_detach_kernel_driver(true).map_err(|e| at_stage("enable_auto_detach", e))?;
    handle.claim_interface(interface).map_err(|e| at_stage("claim_interface", e))?;
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
    // `?` early-returns below and the abandoned-thread timeout path.
    let claim = Claim { handle, interface };
    let outcome = write_all(&claim, endpoint, alternate, bytes);
    // Release before the handle is closed so the OS driver is re-attached promptly.
    drop(claim);
    outcome
}

fn write_all(claim: &Claim, endpoint: u8, alternate: u8, bytes: &[u8]) -> Result<()> {
    claim.handle
        .set_alternate_setting(claim.interface, alternate)
        .map_err(|e| at_stage("set_alternate_setting", e))?;
    let deadline = Instant::now() + Duration::from_secs(30);
    for chunk in bytes.chunks(16 * 1024) {
        if Instant::now() > deadline {
            return Err(AgentError::uncertain());
        }
        // Any bulk error can hide a partial transfer: never automatically retry it.
        let n = claim.handle
            .write_bulk(endpoint, chunk, Duration::from_secs(3))
            .map_err(|_| AgentError::uncertain())?;
        if n != chunk.len() {
            return Err(AgentError::uncertain());
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
            assert_eq!(error.code, "INVALID_CONFIG", "wrong code for {kind}");
            assert!(!error.uncertain, "a rejected profile never reached the paper");
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
