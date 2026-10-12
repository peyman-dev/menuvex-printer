pub mod discovery;
pub mod network;
pub mod raw_printer;
pub mod spooler;
#[cfg(feature = "libusb")]
pub mod usb;
pub mod usb_policy;
use crate::{
    error::{ AgentError, Result },
    protocol::{ valid_id, text_ok },
    printers::usb_policy::{ apply_fallback_with_decision, fallback_decision, FallbackDecision, UsbFailure },
};
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

/// Connection types this agent understands. Anything else is rejected by name, so an operator
/// sees the offending value instead of a generic parser error.
pub const CONNECTION_TYPES: [&str; 3] = ["network", "usb", "spooler"];

/// Reject an unknown `connection.type` **before** serde turns it into the anonymous
/// `INVALID_PAYLOAD: Invalid or unsupported payload` message. Called on every path that can
/// receive a printer profile from outside the process (WebSocket `printer.save`, the local
/// `save_config` command and the persisted config row).
pub fn check_connection_tag(connection: &serde_json::Value) -> Result<()> {
    let received = connection
        .get("type")
        .and_then(|v| v.as_str())
        .unwrap_or("");
    if CONNECTION_TYPES.contains(&received) {
        Ok(())
    } else {
        Err(AgentError::unsupported_connection_type(received))
    }
}

impl Connection {
    /// Stable name of the transport, for structured logs that must never include receipt data.
    pub fn kind(&self) -> &'static str {
        match self {
            Self::Network { .. } => "network",
            Self::Spooler { .. } => "spooler",
            Self::Usb { .. } => "usb",
        }
    }

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

fn is_false(value: &bool) -> bool {
    !*value
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
    /// Operator-owned switch for frontend-authored raw ESC/POS (`document.type == "escpos"`).
    /// Defaults to `false` for every existing profile, is only writable from the local desktop
    /// window (`save_config`), and is forced back to its stored value on a remote `printer.save`.
    #[serde(default)] pub raw_passthrough: bool,
    /// Job-snapshot-only RAW override. The persisted source of truth is the local
    /// `Config.raw_passthrough.printers` map; normal profile serialization omits `false`.
    #[serde(default, skip_serializing_if = "is_false")]
    pub force_raw: bool,
    /// Optional, explicitly selected failover for a direct USB profile. It is local-only and
    /// may only be a spooler queue or private-LAN network connection. A remote printer.save
    /// cannot change it. Existing profiles deserialize with no fallback.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub usb_fallback_target: Option<Connection>,
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
        match (&self.connection, &self.usb_fallback_target) {
            (_, None) => (),
            (Connection::Usb { .. }, Some(Connection::Network { host, port })) => {
                network::address(host, *port)?;
            }
            (Connection::Usb { .. }, Some(Connection::Spooler { queue_name })) => {
                if queue_name.trim().is_empty() || !text_ok(queue_name, 256) {
                    return Err(AgentError::new("INVALID_CONFIG", "Invalid USB fallback print queue name"));
                }
            }
            _ => {
                return Err(AgentError::new(
                    "INVALID_CONFIG",
                    "USB fallback is only allowed for USB profiles and must target a print queue or network printer",
                ));
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
            raw_passthrough: false,
            force_raw: false,
            usb_fallback_target: None,
        }
    }

    #[test]
    fn old_printer_profiles_deserialize_without_a_usb_fallback_target() {
        let mut value = serde_json::to_value(profile(Connection::Network {
            host: "192.168.1.50".into(),
            port: 9100,
        })).unwrap();
        let object = value.as_object_mut().unwrap();
        object.remove("usbFallbackTarget");
        object.remove("forceRaw");
        let restored: Printer = serde_json::from_value(value).unwrap();
        assert_eq!(restored.usb_fallback_target, None);
        assert!(!restored.force_raw);
        restored.validate().unwrap();
    }

    #[test]
    fn usb_fallback_is_optional_and_only_allows_spooler_or_network_targets() {
        let usb = Connection::Usb {
            vendor_id: 1,
            product_id: 2,
            serial: None,
            bus: 1,
            ports: vec![1],
            interface: 0,
            endpoint: 1,
            alternate: 0,
        };
        let mut printer = profile(usb.clone());
        printer.validate().expect("no fallback remains the default");

        printer.usb_fallback_target = Some(Connection::Spooler { queue_name: "POS-80".into() });
        printer.validate().expect("an explicit spooler fallback is valid");
        printer.usb_fallback_target = Some(Connection::Network {
            host: "192.168.1.50".into(),
            port: 9100,
        });
        printer.validate().expect("an explicit private-network fallback is valid");

        printer.usb_fallback_target = Some(usb);
        assert!(printer.validate().is_err(), "USB cannot recursively fall back to USB");
        printer.connection = Connection::Spooler { queue_name: "POS-80".into() };
        printer.usb_fallback_target = Some(Connection::Network {
            host: "192.168.1.50".into(),
            port: 9100,
        });
        assert!(printer.validate().is_err(), "non-USB profiles cannot carry USB fallbacks");
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

    /// The website only understands `network` and `usb` discriminators, so every connection
    /// published to it must serialize to one of those two. A `spooler` value on that wire is
    /// what makes a strict `z.discriminatedUnion('type', [network, usb])` reject the whole
    /// `printers.list` payload with `invalid_union`.
    #[test]
    fn wire_descriptor_never_exposes_a_third_connection_type() {
        let spooler = Connection::Spooler { queue_name: "POS-80".into() };
        let direct_usb = Connection::Usb {
            vendor_id: 1,
            product_id: 2,
            serial: None,
            bus: 1,
            ports: vec![1],
            interface: 0,
            endpoint: 1,
            alternate: 0,
        };
        let lan = Connection::Network { host: "192.168.1.50".into(), port: 9100 };
        for (connection, expected) in
            [(&spooler, "usb"), (&direct_usb, "usb"), (&lan, "network")]
        {
            let value = connection.api_value();
            assert_eq!(value["type"], expected, "unexpected wire type for {connection:?}");
            assert!(["network", "usb"].contains(&value["type"].as_str().unwrap()));
        }
        assert_eq!(spooler.kind(), "spooler");
        assert_eq!(direct_usb.kind(), "usb");
        assert_eq!(lan.kind(), "network");
    }

    #[test]
    fn unknown_connection_types_are_rejected_by_name() {
        for (received, expected) in [
            (serde_json::json!({"type":"lan","host":"192.168.1.50","port":9100}), "\"lan\""),
            (serde_json::json!({"type":"bluetooth"}), "\"bluetooth\""),
            (serde_json::json!({"host":"192.168.1.50"}), "<missing>"),
            (serde_json::json!({"type":null}), "<missing>"),
        ] {
            let error = check_connection_tag(&received).expect_err("must be rejected");
            assert_eq!(error.code, "UNSUPPORTED_CONNECTION_TYPE");
            assert!(
                error.message.contains(expected),
                "message {:?} must name {expected}",
                error.message
            );
            assert!(error.message.starts_with("Unsupported printer connection type:"));
        }
        for ok in ["network", "usb", "spooler"] {
            check_connection_tag(&serde_json::json!({ "type": ok })).unwrap();
        }
    }

    #[test]
    fn echoed_connection_type_is_truncated_and_sanitized() {
        let hostile = format!("{}\u{1b}[31m", "x".repeat(4096));
        let error = AgentError::unsupported_connection_type(&hostile);
        assert!(error.message.len() < 200, "message must stay short: {}", error.message.len());
        assert!(!error.message.contains('\u{1b}'));
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
    /// Default copy delivery preserves the historical per-copy transport behavior. Hardware USB
    /// overrides this to pin all copies to the route selected before the first byte is sent.
    fn send_copies(&self, printer: &Printer, bytes: &[u8], copies: u8) -> Result<()> {
        for copy in 0..copies {
            if let Err(error) = self.send(printer, bytes) {
                return Err(if copy > 0 { AgentError::uncertain() } else { error });
            }
        }
        Ok(())
    }
}
pub struct HardwareTransport;
impl HardwareTransport {
    fn send_connection(connection: &Connection, bytes: &[u8], force_raw: bool) -> Result<()> {
        match connection {
            Connection::Network { host, port } => network::send(host, *port, bytes),
            Connection::Spooler { queue_name } => {
                spooler::send_with_options(queue_name, bytes, force_raw).map(|_| ())
            },
            Connection::Usb { .. } => Err(AgentError::new(
                "INVALID_CONFIG",
                "A USB connection cannot be used as a USB fallback target",
            )),
        }
    }

    fn send_usb_failure(
        printer: &Printer,
        bytes: &[u8],
        failure: UsbFailure,
        forced_decision: Option<FallbackDecision>,
        prior_copies_sent: u8,
    ) -> Result<Connection> {
        Self::send_usb_failure_with(
            printer,
            failure,
            forced_decision,
            prior_copies_sent,
            |target| Self::send_connection(target, bytes, printer.force_raw),
        )
    }

    fn send_usb_failure_with(
        printer: &Printer,
        failure: UsbFailure,
        forced_decision: Option<FallbackDecision>,
        prior_copies_sent: u8,
        send_fallback: impl FnOnce(&Connection) -> Result<()>,
    ) -> Result<Connection> {
        let fallback = printer.usb_fallback_target.as_ref();
        let decision = if prior_copies_sent > 0 {
            FallbackDecision::Forbid
        } else {
            forced_decision.unwrap_or_else(|| {
                fallback_decision(
                    failure.stage,
                    fallback.is_some(),
                    failure.bytes_written_before_failure,
                )
            })
        };
        let configured_target_label = fallback.map(connection_log_label).unwrap_or_else(|| "<none>".into());
        let target_label = if decision == FallbackDecision::Auto {
            configured_target_label.clone()
        } else {
            "<not-used>".into()
        };
        let bytes_written = failure.bytes_written_before_failure.map(|n| n as i64).unwrap_or(-1);
        let bytes_written_known = failure.bytes_written_before_failure.is_some();
        tracing::warn!(
            target: "usb",
            event = "USB_TRANSPORT_FAILURE",
            printer_id = %printer.id,
            connection = "usb",
            stage = %failure.stage,
            libusb_error_code = ?failure.libusb_error_code,
            libusb_error_name = ?failure.libusb_error_name,
            fallback_decision = decision.as_str(),
            fallback_target = %target_label,
            configured_fallback_target = %configured_target_label,
            bytes_written_before_failure = bytes_written,
            bytes_written_known,
            completed_copies_before_failure = prior_copies_sent,
            error_code = %failure.cause.code,
            error_message = %failure.cause.message,
            "direct USB transport failed"
        );

        if decision == FallbackDecision::Auto {
            tracing::warn!(
                target: "usb",
                event = "USB_FALLBACK_STARTED",
                printer_id = %printer.id,
                stage = %failure.stage,
                fallback_target = %target_label,
                bytes_written_before_failure = bytes_written,
                completed_copies_before_failure = prior_copies_sent,
                "using the explicitly configured fallback before any job bytes were sent"
            );
        }
        let selected_fallback = if decision == FallbackDecision::Auto {
            fallback.cloned()
        } else {
            None
        };
        let result = apply_fallback_with_decision(failure, fallback, decision, send_fallback);
        match (&result, decision) {
            (Ok(()), FallbackDecision::Auto) => tracing::info!(
                target: "usb",
                event = "USB_FALLBACK_COMPLETED",
                printer_id = %printer.id,
                fallback_target = %target_label,
                completed_copies_before_failure = prior_copies_sent,
                "configured fallback accepted the print job"
            ),
            (Err(error), FallbackDecision::Auto) => tracing::error!(
                target: "usb",
                event = "USB_FALLBACK_FAILED",
                printer_id = %printer.id,
                fallback_target = %target_label,
                error_code = %error.code,
                error_message = %error.message,
                uncertain = error.uncertain,
                "configured USB fallback failed"
            ),
            _ => (),
        }
        match result {
            Ok(()) => selected_fallback.ok_or_else(||
                AgentError::new("INVALID_CONFIG", "USB fallback decision had no configured target")
            ),
            Err(error) => Err(error),
        }
    }

    #[cfg(feature = "libusb")]
    fn send_usb(printer: &Printer, bytes: &[u8]) -> Result<Connection> {
        match usb::LibusbTransport.send(&printer.connection, bytes) {
            Ok(()) => Ok(printer.connection.clone()),
            Err(failure) => Self::send_usb_failure(printer, bytes, failure, None, 0),
        }
    }

    #[cfg(not(feature = "libusb"))]
    fn send_usb(printer: &Printer, bytes: &[u8]) -> Result<Connection> {
        Self::send_usb_failure(printer, bytes, UsbFailure::feature_disabled(), None, 0)
    }

    fn send_once(printer: &Printer, bytes: &[u8]) -> Result<Connection> {
        match &printer.connection {
            Connection::Usb { .. } => Self::send_usb(printer, bytes),
            connection => {
                Self::send_connection(connection, bytes, printer.force_raw)?;
                Ok(connection.clone())
            }
        }
    }

    fn send_selected_route(
        printer: &Printer,
        route: &Connection,
        bytes: &[u8],
        prior_copies_sent: u8,
    ) -> Result<()> {
        match route {
            Connection::Network { host, port } => network::send(host, *port, bytes),
            Connection::Spooler { queue_name } => {
                spooler::send_with_options(queue_name, bytes, printer.force_raw).map(|_| ())
            },
            Connection::Usb { .. } => {
                #[cfg(feature = "libusb")]
                {
                    match usb::LibusbTransport.send(route, bytes) {
                        Ok(()) => Ok(()),
                        Err(failure) =>
                            Self::send_usb_failure(
                                printer,
                                bytes,
                                failure,
                                Some(FallbackDecision::Forbid),
                                prior_copies_sent,
                            ).map(|_| ()),
                    }
                }
                #[cfg(not(feature = "libusb"))]
                {
                    Self::send_usb_failure(
                        printer,
                        bytes,
                        UsbFailure::feature_disabled(),
                        Some(FallbackDecision::Forbid),
                        prior_copies_sent,
                    ).map(|_| ())
                }
            }
        }
    }

    fn send_usb_copies_with(
        printer: &Printer,
        bytes: &[u8],
        copies: u8,
        mut send_first: impl FnMut(&Printer, &[u8]) -> Result<Connection>,
        mut send_selected: impl FnMut(&Printer, &Connection, &[u8], u8) -> Result<()>,
    ) -> Result<()> {
        let mut selected_route: Option<Connection> = None;
        let mut result = Ok(());
        for copy in 0..copies {
            let attempt = if copy == 0 {
                send_first(printer, bytes).map(|route| selected_route = Some(route))
            } else {
                let route = selected_route.as_ref().expect("first copy selects a transport");
                send_selected(printer, route, bytes, copy)
            };
            if let Err(mut error) = attempt {
                if copy > 0 {
                    // Earlier copies have already left this process. Even an open failure on a
                    // later copy cannot move the remainder to another printer or be retried.
                    error.retryable = false;
                    error.uncertain = true;
                }
                result = Err(error);
                break;
            }
        }
        result
    }
}

fn connection_log_label(connection: &Connection) -> String {
    match connection {
        Connection::Spooler { queue_name } => format!("spooler:{queue_name}"),
        Connection::Network { host, port } => format!("network:{host}:{port}"),
        Connection::Usb { .. } => "usb:<invalid-fallback>".into(),
    }
}

fn log_transport_failure(printer: &Printer, outcome: &Result<()>) {
    if let Err(error) = outcome {
        tracing::warn!(
            target: "print",
            event = "TRANSPORT_SEND_FAILED",
            printer_id = %printer.id,
            connection = printer.connection.kind(),
            error_code = %error.code,
            error_message = %error.message,
            uncertain = error.uncertain,
            "transport rejected the job"
        );
    }
}

impl Transport for HardwareTransport {
    fn send(&self, p: &Printer, b: &[u8]) -> Result<()> {
        let outcome = Self::send_once(p, b).map(|_| ());
        log_transport_failure(p, &outcome);
        outcome
    }

    fn send_copies(&self, p: &Printer, b: &[u8], copies: u8) -> Result<()> {
        if matches!(&p.connection, Connection::Usb { .. }) {
            let result = Self::send_usb_copies_with(
                p,
                b,
                copies,
                |printer, bytes| Self::send_once(printer, bytes),
                |printer, route, bytes, prior_copies| {
                    Self::send_selected_route(printer, route, bytes, prior_copies)
                },
            );
            log_transport_failure(p, &result);
            result
        } else {
            // Leave Windows queue/network copy behavior unchanged for non-USB profiles.
            let mut result = Ok(());
            for copy in 0..copies {
                if let Err(error) = self.send(p, b) {
                    result = Err(if copy > 0 { AgentError::uncertain() } else { error });
                    break;
                }
            }
            result
        }
    }

    fn status(&self, p: &Printer) -> String {
        // Never propagate a probe failure as an error: an unreachable or disabled transport
        // reports a status, it does not break the monitor loop serving other printers.
        match &p.connection {
            Connection::Network { host, port } =>
                (if network::probe(host, *port).is_ok() { "online" } else { "offline" }).into(),
            Connection::Spooler { queue_name } => spooler::status(queue_name),
            Connection::Usb { .. } => {
                #[cfg(feature = "libusb")]
                {
                    match usb::present(&p.connection) {
                        Ok(true) => "online".into(),
                        Ok(false) => "offline".into(),
                        Err(e) => {
                            tracing::warn!(
                                target: "usb",
                                event = "USB_STATUS_UNKNOWN",
                                printer_id = %p.id,
                                connection = "usb",
                                error_code = %e.code,
                                error_message = %e.message,
                                "USB presence probe failed; reporting unknown"
                            );
                            "unknown".into()
                        }
                    }
                }
                #[cfg(not(feature = "libusb"))]
                {
                    tracing::warn!(
                        target: "usb",
                        event = "LIBUSB_FEATURE_DISABLED",
                        printer_id = %p.id,
                        fallback_configured = p.usb_fallback_target.is_some(),
                        "USB profile is saved but this build has no libusb transport"
                    );
                    "unknown".into()
                }
            }
        }
    }
}

#[cfg(test)]
mod usb_transport_tests {
    use super::*;
    use crate::printers::usb_policy::UsbFailureStage;

    fn usb_connection() -> Connection {
        Connection::Usb {
            vendor_id: 1,
            product_id: 2,
            serial: None,
            bus: 1,
            ports: vec![1],
            interface: 0,
            endpoint: 1,
            alternate: 0,
        }
    }

    fn printer(usb_fallback_target: Option<Connection>) -> Printer {
        Printer {
            id: "printer:usb-transport-test".into(),
            name: "USB test printer".into(),
            connection: usb_connection(),
            paper_mm: 80,
            width_dots: 576,
            copies: 3,
            cut: true,
            font_family: "Noto Sans Arabic".into(),
            font_size: 24,
            raw_passthrough: false,
            force_raw: false,
            usb_fallback_target,
        }
    }

    fn failure(stage: UsbFailureStage) -> UsbFailure {
        UsbFailure::new(
            stage,
            AgentError::new("USB_DEVICE_ERROR", "injected USB failure"),
            Some(-12),
            Some("NotSupported".into()),
            Some(0),
        )
    }

    #[test]
    fn hardware_transport_executes_only_the_configured_prewrite_fallback() {
        let target = Connection::Network { host: "192.168.1.50".into(), port: 9100 };
        let profile = printer(Some(target.clone()));
        let mut calls = 0;
        let selected = HardwareTransport::send_usb_failure_with(
            &profile,
            failure(UsbFailureStage::PreOpen),
            None,
            0,
            |actual| {
                calls += 1;
                assert_eq!(actual, &target);
                Ok(())
            },
        ).expect("the explicitly selected network target should be used");
        assert_eq!(selected, target);
        assert_eq!(calls, 1);
    }

    #[test]
    fn hardware_transport_never_executes_fallback_for_partial_or_ambiguous_failures() {
        let target = Connection::Spooler { queue_name: "POS-80".into() };
        let profile = printer(Some(target));
        for stage in [UsbFailureStage::MidWrite, UsbFailureStage::Timeout, UsbFailureStage::Unknown] {
            let error = HardwareTransport::send_usb_failure_with(
                &profile,
                failure(stage),
                None,
                0,
                |_| panic!("forbidden stage {stage} must not invoke the fallback sender"),
            ).expect_err("partial or ambiguous USB outcomes must fail");
            assert!(error.uncertain, "uncertainty lost for {stage}");
        }
    }

    #[test]
    fn all_copies_remain_on_the_route_selected_for_the_first_copy() {
        let profile = printer(None);
        let fallback = Connection::Network { host: "192.168.1.50".into(), port: 9100 };
        let mut first_copy_calls = 0;
        let mut later_copy_routes = Vec::new();
        let result = HardwareTransport::send_usb_copies_with(
            &profile,
            b"receipt",
            3,
            |_, _| {
                first_copy_calls += 1;
                Ok(fallback.clone())
            },
            |_, route, _, prior_copies| {
                assert_eq!(route, &fallback);
                assert_eq!(prior_copies, later_copy_routes.len() as u8 + 1);
                later_copy_routes.push(route.clone());
                Ok(())
            },
        );
        assert!(result.is_ok());
        assert_eq!(first_copy_calls, 1);
        assert_eq!(later_copy_routes, vec![fallback.clone(), fallback]);
    }

    #[test]
    fn later_preopen_usb_failure_cannot_switch_a_multi_copy_job_to_fallback() {
        let target = Connection::Spooler { queue_name: "POS-80".into() };
        let profile = printer(Some(target));
        let usb = usb_connection();
        let mut fallback_calls = 0;
        let mut later_attempts = 0;
        let error = HardwareTransport::send_usb_copies_with(
            &profile,
            b"receipt",
            3,
            |_, _| Ok(usb.clone()),
            |profile, route, bytes, prior_copies| {
                assert_eq!(route, &usb);
                later_attempts += 1;
                HardwareTransport::send_usb_failure_with(
                    profile,
                    failure(UsbFailureStage::PreOpen),
                    None,
                    prior_copies,
                    |_| {
                        fallback_calls += 1;
                        Ok(())
                    },
                ).map(|_| ())
            },
        ).expect_err("later-copy USB failure must not change routes");
        assert_eq!(later_attempts, 1, "stop the job rather than split its remaining copies");
        assert_eq!(fallback_calls, 0, "prior completed copies forbid fallback");
        assert!(error.uncertain);
        assert!(!error.retryable);
    }
}
