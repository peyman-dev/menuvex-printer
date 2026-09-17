use rusb::{ UsbContext, Device, Context, Direction, TransferType };
use std::time::{ Duration, Instant };
use crate::{ error::{ AgentError, Result }, printers::Connection };
use serde::Serialize;
#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct DiscoveredUsb {
    pub connection: Connection,
    pub manufacturer: Option<String>,
    pub product: Option<String>,
    pub accessible: bool,
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
        _ =>
            AgentError::new(
                "USB_DEVICE_ERROR",
                "USB operation failed; inspect driver and interface"
            ),
    }
}
pub fn discover() -> Result<Vec<DiscoveredUsb>> {
    let ctx = Context::new().map_err(map)?;
    let devices = ctx.devices().map_err(map)?;
    let mut out = Vec::new();
    for dev in devices.iter() {
        let Ok(desc) = dev.device_descriptor() else {
            continue;
        };
        let handle = dev.open().ok();
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
                        out.push(DiscoveredUsb {
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
                        });
                    }
                }
            }
        }
    }
    Ok(out)
}
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
    if let Some(s) = serial {
        dev
            .open()
            .ok()
            .and_then(|h| h.read_serial_number_string_ascii(&d).ok())
            .as_ref() == Some(s)
    } else {
        dev.bus_number() == *bus && dev.port_numbers().ok().as_ref() == Some(ports)
    }
}
pub fn present(c: &Connection) -> Result<bool> {
    let ctx = Context::new().map_err(map)?;
    Ok(
        ctx
            .devices()
            .map_err(map)?
            .iter()
            .any(|d| matches(&d, c))
    )
}
pub fn send(c: &Connection, bytes: &[u8]) -> Result<()> {
    let Connection::Usb { interface, endpoint, alternate, .. } = c else {
        return Err(AgentError::new("INVALID_CONFIG", "Not USB"));
    };
    let ctx = Context::new().map_err(map)?;
    let devices = ctx.devices().map_err(map)?;
    let candidates: Vec<_> = devices
        .iter()
        .filter(|d| matches(d, c))
        .collect();
    if candidates.len() != 1 {
        return Err(
            if candidates.is_empty() {
                AgentError::retry("PRINTER_OFFLINE")
            } else {
                AgentError::new("USB_AMBIGUOUS", "Multiple devices share this identity")
            }
        );
    }
    let dev = &candidates[0];
    let config = dev.active_config_descriptor().map_err(map)?;
    let valid = config
        .interfaces()
        .flat_map(|i| i.descriptors())
        .any(
            |a|
                a.class_code() == 7 &&
                a.interface_number() == *interface &&
                a.setting_number() == *alternate &&
                a
                    .endpoint_descriptors()
                    .any(
                        |e|
                            e.address() == *endpoint &&
                            e.direction() == Direction::Out &&
                            e.transfer_type() == TransferType::Bulk
                    )
        );
    if !valid {
        return Err(
            AgentError::new("USB_DEVICE_ERROR", "Saved printer interface no longer matches")
        );
    }
    let handle = dev.open().map_err(map)?;
    #[cfg(target_os = "linux")]
    handle.set_auto_detach_kernel_driver(true).map_err(map)?;
    handle.claim_interface(*interface).map_err(map)?;
    handle.set_alternate_setting(*interface, *alternate).map_err(map)?;
    let deadline = Instant::now() + Duration::from_secs(30);
    for chunk in bytes.chunks(16 * 1024) {
        if Instant::now() > deadline {
            return Err(AgentError::uncertain());
        }
        // Any bulk error can hide a partial transfer: never automatically retry it.
        let n = handle
            .write_bulk(*endpoint, chunk, Duration::from_secs(3))
            .map_err(|_| AgentError::uncertain())?;
        if n != chunk.len() {
            return Err(AgentError::uncertain());
        }
    }
    Ok(())
}
