pub mod discovery;
pub mod network;
pub mod spooler;
pub mod usb;
use crate::{ error::{ AgentError, Result }, protocol::{ valid_id, text_ok } };
use serde::{ Serialize, Deserialize };
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
#[serde(tag = "type", rename_all = "lowercase", deny_unknown_fields)]
pub enum Connection {
    Network {
        host: String,
        port: u16,
    },
    /// An installed OS print queue (Windows spooler or CUPS). The recommended connection for
    /// USB printers: the vendor driver stays installed, no libusb driver replacement.
    Spooler {
        #[serde(rename = "queueName")] queue_name: String,
    },
    Usb {
        #[serde(rename = "vendorId")] vendor_id: u16,
        #[serde(rename = "productId")] product_id: u16,
        serial: Option<String>,
        bus: u8,
        ports: Vec<u8>,
        interface: u8,
        endpoint: u8,
        alternate: u8,
    },
}

impl Connection {
    /// Return the backwards-compatible wire descriptor used by `printers.list`/`printer.get`.
    /// Older MenuVex frontends only understand USB connections; reserve VID 0 and a `queue:`
    /// serial for OS spooler queues while keeping the stored transport explicitly tagged.
    pub fn api_value(&self) -> serde_json::Value {
        match self {
            Self::Spooler { queue_name } => serde_json::json!({
                "type": "usb",
                "vendorId": 0,
                "productId": 0,
                "serial": format!("queue:{queue_name}"),
                "bus": 0,
                "ports": [],
                "interface": 0,
                "endpoint": 0,
                "alternate": 0
            }),
            _ => serde_json::to_value(self).expect("connection serialization is infallible"),
        }
    }

    /// Normalize the reserved USB-shaped descriptor back into its real spooler transport before
    /// validating or persisting a `printer.save` request. Vendor ID zero is not a valid USB VID.
    pub fn normalize_api_compat(&mut self) {
        let queue_name = match self {
            Self::Usb {
                vendor_id: 0,
                serial: Some(serial),
                ..
            } => serial.strip_prefix("queue:").map(str::to_owned),
            _ => None,
        };
        if let Some(queue_name) = queue_name {
            *self = Self::Spooler { queue_name };
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Printer {
    pub id: String,
    pub name: String,
    pub connection: Connection,
    pub paper_mm: u16,
    pub width_dots: u16,
    pub copies: u8,
    pub cut: bool,
    pub font_family: String,
    pub font_size: u16,
}
impl Printer {
    pub fn validate(&self) -> Result<()> {
        if
            !valid_id(&self.id) ||
            self.name.is_empty() ||
            !text_ok(&self.name, 128) ||
            ![58, 80].contains(&self.paper_mm) ||
            !(128..=832).contains(&self.width_dots) ||
            self.width_dots % 8 != 0 ||
            !(1..=3).contains(&self.copies) ||
            !(12..=48).contains(&self.font_size) ||
            self.font_family.is_empty() ||
            !text_ok(&self.font_family, 128)
        {
            return Err(AgentError::new("INVALID_CONFIG", "Invalid printer profile"));
        }
        match &self.connection {
            Connection::Network { host, port } => {
                network::address(host, *port)?;
            }
            Connection::Spooler { queue_name } => {
                if queue_name.trim().is_empty() || !text_ok(queue_name, 256) {
                    return Err(AgentError::new("INVALID_CONFIG", "Invalid print queue name"));
                }
            }
            Connection::Usb { vendor_id, product_id, serial, ports, endpoint, .. } => {
                if
                    *vendor_id == 0 ||
                    *product_id == 0 ||
                    (endpoint & 0x80) != 0 ||
                    *endpoint == 0 ||
                    ports.is_empty() ||
                    ports.len() > 8 ||
                    serial.as_ref().is_some_and(|s| !text_ok(s, 256))
                {
                    return Err(AgentError::new("INVALID_CONFIG", "Invalid USB profile"));
                }
            }
        }
        Ok(())
    }
}

#[cfg(test)]
mod compatibility_tests {
    use super::*;

    fn profile(connection: Connection) -> Printer {
        Printer {
            id: "printer:compat".into(),
            name: "Receipt printer".into(),
            connection,
            paper_mm: 80,
            width_dots: 576,
            copies: 1,
            cut: true,
            font_family: "Noto Sans Arabic".into(),
            font_size: 24,
        }
    }

    #[test]
    fn spooler_wire_descriptor_round_trips_without_persisting_as_usb() {
        let spooler = Connection::Spooler { queue_name: "POS-80".into() };
        let value = spooler.api_value();
        assert_eq!(value["type"], "usb");
        assert_eq!(value["vendorId"], 0);
        assert_eq!(value["productId"], 0);
        assert_eq!(value["serial"], "queue:POS-80");

        let mut printer = profile(serde_json::from_value(value).unwrap());
        printer.connection.normalize_api_compat();
        assert_eq!(printer.connection, spooler);
        printer.validate().unwrap();
    }

    #[test]
    fn only_the_reserved_zero_vid_queue_serial_is_normalized() {
        let mut compatible_with_unused_product_id = Connection::Usb {
            vendor_id: 0,
            product_id: 73,
            serial: Some("queue:POS-80".into()),
            bus: 0,
            ports: vec![],
            interface: 0,
            endpoint: 0,
            alternate: 0,
        };
        compatible_with_unused_product_id.normalize_api_compat();
        assert!(matches!(compatible_with_unused_product_id, Connection::Spooler { .. }));

        let mut direct_usb = Connection::Usb {
            vendor_id: 1,
            product_id: 2,
            serial: Some("queue:POS-80".into()),
            bus: 1,
            ports: vec![1],
            interface: 0,
            endpoint: 1,
            alternate: 0,
        };
        direct_usb.normalize_api_compat();
        assert!(matches!(direct_usb, Connection::Usb { vendor_id: 1, .. }));

        let mut not_reserved = Connection::Usb {
            vendor_id: 0,
            product_id: 0,
            serial: Some("ordinary-serial".into()),
            bus: 0,
            ports: vec![1],
            interface: 0,
            endpoint: 1,
            alternate: 0,
        };
        not_reserved.normalize_api_compat();
        assert!(matches!(&not_reserved, Connection::Usb { .. }));
        assert!(profile(not_reserved).validate().is_err());
    }
}

pub trait Transport: Send + Sync {
    fn send(&self, printer: &Printer, bytes: &[u8]) -> Result<()>;
    fn status(&self, printer: &Printer) -> String;
}
pub struct HardwareTransport;
impl Transport for HardwareTransport {
    fn send(&self, p: &Printer, b: &[u8]) -> Result<()> {
        match &p.connection {
            Connection::Network { host, port } => network::send(host, *port, b),
            Connection::Spooler { queue_name } => spooler::send(queue_name, b),
            Connection::Usb { .. } => usb::send(&p.connection, b),
        }
    }
    fn status(&self, p: &Printer) -> String {
        match &p.connection {
            Connection::Network { host, port } =>
                (if network::probe(host, *port).is_ok() { "online" } else { "offline" }).into(),
            Connection::Spooler { queue_name } => spooler::status(queue_name),
            Connection::Usb { .. } =>
                (
                    match usb::present(&p.connection) {
                        Ok(true) => "online",
                        Ok(false) => "offline",
                        Err(_) => "unknown",
                    }
                ).into(),
        }
    }
}
