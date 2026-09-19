pub mod discovery;
pub mod network;
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
pub trait Transport: Send + Sync {
    fn send(&self, printer: &Printer, bytes: &[u8]) -> Result<()>;
    fn status(&self, printer: &Printer) -> String;
}
pub struct HardwareTransport;
impl Transport for HardwareTransport {
    fn send(&self, p: &Printer, b: &[u8]) -> Result<()> {
        match &p.connection {
            Connection::Network { host, port } => network::send(host, *port, b),
            Connection::Usb { .. } => usb::send(&p.connection, b),
        }
    }
    fn status(&self, p: &Printer) -> String {
        match &p.connection {
            Connection::Network { host, port } =>
                (if network::probe(host, *port).is_ok() { "online" } else { "offline" }).into(),
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
