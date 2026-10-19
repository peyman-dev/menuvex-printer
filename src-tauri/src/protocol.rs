use serde::{ Deserialize, Serialize };
use crate::error::{ AgentError, Result };
pub const VERSION: u8 = 1;
/// The wire envelope must hold a base64-encoded raw ESC/POS document of up to 1 MiB.
pub const MAX_MESSAGE: usize = 2 * 1024 * 1024;
pub fn valid_id(s: &str) -> bool {
    !s.is_empty() &&
        s.len() <= 128 &&
        s.bytes().all(|c| c.is_ascii_alphanumeric() || b"-_: .".contains(&c)) &&
        !s.contains(' ')
}
pub fn text_ok(s: &str, limit: usize) -> bool {
    s.len() <= limit && !s.chars().any(|c| c.is_control() && c != '\n')
}
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct InvoiceItem {
    pub name: String,
    pub quantity: u32,
    pub unit_price: u64,
}
/// Invoice payload. Only `storeName`, `orderNumber`, `items` and `total` are required; every
/// other field is optional and is printed only when the adapter supplies it. The agent never
/// invents business data: no currency word, subtotal or date is computed locally.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct InvoiceData {
    pub store_name: String,
    pub order_number: String,
    pub items: Vec<InvoiceItem>,
    pub total: u64,
    #[serde(default)] pub footer: String,
    /// Document title shown next to the slip number, e.g. "فاکتور فروش" or "بلیط آشپزخانه".
    #[serde(default)] pub title: String,
    #[serde(default)] pub address: String,
    #[serde(default)] pub phone: String,
    #[serde(default)] pub date: String,
    #[serde(default)] pub status: String,
    #[serde(default)] pub order_type: String,
    #[serde(default)] pub table: String,
    #[serde(default)] pub note: String,
    /// Currency word appended to amounts, e.g. "تومان". Amounts print bare when empty.
    #[serde(default)] pub currency: String,
    /// Items subtotal as sent by the adapter; the agent never derives it from the items.
    #[serde(default)] pub subtotal: Option<u64>,
}
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
#[serde(tag = "type", rename_all = "camelCase", deny_unknown_fields)]
pub enum Document {
    Invoice {
        data: InvoiceData,
    },
    Receipt {
        lines: Vec<String>,
    },
    /// Frontend-authored raw ESC/POS. Once the local operator enables both gates, the agent
    /// transports these bytes without rendering, shaping, font substitution, or added commands.
    /// The Windows RAW spooler queue and driver are still part of the OS path; Generic / Text Only
    /// is recommended when the vendor driver may reinterpret printer data. A website cannot change
    /// the operator-owned global or per-printer switches (see `State::enqueue`).
    Escpos {
        /// Byte values, e.g. `[27, 64]`. Convenient for short command sequences.
        #[serde(default)] commands: Vec<u8>,
        /// Base64 of the same bytes. Use this for raster receipts; a JSON number array is much
        /// larger than the binary payload.
        #[serde(default)] data: String,
    },
}
/// Upper bound for the decoded raw ESC/POS document (the base64 wire value inflates by 4/3).
pub const MAX_ESCPOS: usize = 1024 * 1024;

/// Known, common ESC/POS command headers. Requiring a recognized command prevents arbitrary
/// binary documents from being silently treated as printer programs.
const ESCPOS_SIGNATURES: &[&[u8]] = &[
    &[0x1b, 0x40],       // ESC @ (initialize)
    &[0x1b, 0x21],       // ESC ! (print mode)
    &[0x1b, 0x61],       // ESC a (alignment)
    &[0x1b, 0x45],       // ESC E (emphasis)
    &[0x1b, 0x4d],       // ESC M (font)
    &[0x1b, 0x2d],       // ESC - (underline)
    &[0x1b, 0x64],       // ESC d (feed)
    &[0x1b, 0x74],       // ESC t (code page)
    &[0x1d, 0x21],       // GS ! (character size)
    &[0x1d, 0x28],       // GS ( (extended command)
    &[0x1d, 0x48],       // GS H (HRI position)
    &[0x1d, 0x56],       // GS V (cut)
    &[0x1d, 0x68],       // GS h (barcode height)
    &[0x1d, 0x77],       // GS w (barcode width)
    &[0x1d, 0x76, 0x30], // GS v 0 (raster image)
    &[0x1c, 0x26],       // FS & (kanji mode)
    &[0x1c, 0x43],       // FS C (kanji code)
    &[0x10, 0x04],       // DLE EOT (status query)
];

fn raw_document_error(code: &str, message: &str, action: &str) -> AgentError {
    let mut error = AgentError::new(code, message);
    error.action_required = Some(action.to_owned());
    error
}

impl Document {
    pub fn validate(&self) -> Result<()> {
        if let Self::Escpos { .. } = self {
            self.escpos_bytes()?;
        }
        let valid = match self {
            Self::Invoice { data } =>
                text_ok(&data.store_name, 300) &&
                    text_ok(&data.order_number, 128) &&
                    text_ok(&data.footer, 1000) &&
                    text_ok(&data.title, 128) &&
                    text_ok(&data.address, 500) &&
                    text_ok(&data.phone, 64) &&
                    text_ok(&data.date, 64) &&
                    text_ok(&data.status, 128) &&
                    text_ok(&data.order_type, 128) &&
                    text_ok(&data.table, 64) &&
                    text_ok(&data.note, 500) &&
                    text_ok(&data.currency, 32) &&
                    data.subtotal.map_or(true, |v| v <= 9_000_000_000_000) &&
                    !data.items.is_empty() &&
                    data.items.len() <= 100 &&
                    data.total <= 9_000_000_000_000 &&
                    data.items
                        .iter()
                        .all(
                            |i|
                                text_ok(&i.name, 300) &&
                                !i.name.is_empty() &&
                                i.quantity > 0 &&
                                i.quantity <= 9999 &&
                                i.unit_price <= 900_000_000
                        ),
            Self::Receipt { lines } =>
                !lines.is_empty() && lines.len() <= 100 && lines.iter().all(|s| text_ok(s, 500)),
            // Bounds and base64 were already checked by `escpos_bytes` above.
            Self::Escpos { .. } => true,
        };
        if valid {
            Ok(())
        } else {
            Err(AgentError::new("INVALID_JOB", "Document limits or text validation failed"))
        }
    }
    /// The exact bytes of a raw ESC/POS document. `commands` come first, then `data`; nothing is
    /// added, reordered or removed. Any other document type is a programming error.
    pub fn escpos_bytes(&self) -> Result<Vec<u8>> {
        let Self::Escpos { commands, data } = self else {
            return Err(AgentError::new("INVALID_JOB", "Not a raw ESC/POS document"));
        };
        let encoded = data.trim();
        let decoded = if encoded.is_empty() {
            Vec::new()
        } else {
            use base64::{ engine::general_purpose::STANDARD, Engine };
            STANDARD.decode(encoded).map_err(|_| raw_document_error(
                "RAW_ESC_POS_INVALID",
                "Raw ESC/POS payload is not valid base64",
                "Send the receipt as valid base64-encoded ESC/POS bytes.",
            ))?
        };
        let mut bytes = commands.clone();
        bytes.extend(decoded);
        if bytes.is_empty() {
            return Err(raw_document_error(
                "RAW_ESC_POS_INVALID",
                "Raw ESC/POS document carries no bytes",
                "Send at least one valid ESC/POS command.",
            ));
        }
        if bytes.len() > MAX_ESCPOS {
            return Err(raw_document_error(
                "RAW_PAYLOAD_TOO_LARGE",
                &format!("Raw ESC/POS document exceeds the {MAX_ESCPOS}-byte limit"),
                &format!("Reduce the ESC/POS payload to at most {MAX_ESCPOS} bytes."),
            ));
        }
        if !ESCPOS_SIGNATURES.iter().any(|signature| bytes.starts_with(signature)) {
            return Err(raw_document_error(
                "RAW_ESC_POS_INVALID",
                "Raw ESC/POS document does not start with a recognized ESC/POS command",
                "Start the payload with a valid ESC/POS command such as ESC @ or GS v 0.",
            ));
        }
        Ok(bytes)
    }
    /// Plain-text projection of the document for diagnostics and tests. This is **not** the
    /// printed design: paper output is produced by [`crate::print::layout`], which draws its own
    /// separators as pixels and keeps Persian on the right and amounts on the left margin.
    pub fn lines(&self) -> Vec<String> {
        match self {
            Self::Receipt { lines } => lines.clone(),
            // Diagnostics only: the raw bytes are the design, they are never projected to text.
            Self::Escpos { commands, data } =>
                vec![
                    format!(
                        "<raw ESC/POS passthrough: {} inline byte(s), {} base64 char(s)>",
                        commands.len(),
                        data.trim().len()
                    ),
                ],
            Self::Invoice { data: d } => {
                let mut lines = vec![
                    d.store_name.clone(),
                    format!("سفارش: {}", d.order_number),
                    "-".repeat(16)
                ];
                for (label, value) in [
                    ("تاریخ", &d.date),
                    ("وضعیت", &d.status),
                    ("نوع سفارش", &d.order_type),
                    ("میز", &d.table),
                ] {
                    if !value.trim().is_empty() {
                        lines.push(format!("{label}: {value}"));
                    }
                }
                for i in &d.items {
                    lines.push(i.name.clone());
                    lines.push(
                        format!(
                            "{} × {} = {}",
                            i.quantity,
                            i.unit_price,
                            (i.quantity as u64) * i.unit_price
                        )
                    );
                }
                lines.push("-".repeat(16));
                lines.push(format!("جمع: {}", d.total));
                lines.push(d.footer.clone());
                lines
            }
        }
    }
}
#[derive(Debug)]
pub struct Request {
    pub version: u8,
    pub request_id: String,
    pub command: Command,
}
#[derive(Debug, Deserialize)]
#[serde(tag = "type", deny_unknown_fields)]
pub enum Command {
    #[serde(rename = "hello")] Hello,
    #[serde(rename = "authenticate")] Authenticate {
        proof: String,
    },
    #[serde(rename = "ping")] Ping,
    #[serde(rename = "agent.status")] AgentStatus,
    #[serde(rename = "printers.list")] PrintersList,
    #[serde(rename = "printer.get")] PrinterGet {
        #[serde(rename = "printerId")] printer_id: String,
    },
    #[serde(rename = "printer.test")] PrinterTest {
        #[serde(rename = "printerId")] printer_id: String,
        #[serde(rename = "jobId")] job_id: String,
    },
    #[serde(rename = "print")] Print {
        #[serde(rename = "printerId")] printer_id: String,
        #[serde(rename = "jobId")] job_id: String,
        document: Document,
    },
    #[serde(rename = "print.status")] PrintStatus {
        #[serde(rename = "jobId")] job_id: String,
    },
    #[serde(rename = "queue.list")] QueueList,
    #[serde(rename = "queue.cancel")] QueueCancel {
        #[serde(rename = "jobId")] job_id: String,
    },
    #[serde(rename = "queue.clear")] QueueClear,
    #[serde(rename = "discover.network")] DiscoverNetwork,
    #[serde(rename = "printers.installed")] PrintersInstalled,
    #[serde(rename = "printer.save")] PrinterSave {
        printer: crate::printers::Printer,
    },
    #[serde(rename = "agent.shutdown")] Shutdown,
}
pub fn parse(text: &str) -> Result<Request> {
    if text.len() > MAX_MESSAGE {
        return Err(AgentError::new("INVALID_PAYLOAD", "Message too large"));
    }
    // Flatten and deny_unknown_fields are not composable in serde. Validate envelope keys separately.
    let value: serde_json::Value = serde_json::from_str(text)?;
    let obj = value
        .as_object()
        .ok_or_else(|| AgentError::new("INVALID_PAYLOAD", "Expected object"))?;
    let version = obj.get("version").and_then(|v| v.as_u64());
    if version != Some(VERSION as u64) {
        return Err(AgentError::new("PROTOCOL_VERSION", "Only version 1 is supported"));
    }
    let id = obj
        .get("requestId")
        .and_then(|v| v.as_str())
        .filter(|s| valid_id(s))
        .ok_or_else(|| AgentError::new("INVALID_PAYLOAD", "Invalid requestId"))?;
    let mut body = obj.clone();
    body.remove("version");
    body.remove("requestId");
    // Reject an unknown `connection.type` *before* serde collapses it into the anonymous
    // `INVALID_PAYLOAD: Invalid or unsupported payload`. The operator has to see which value
    // their client sent, otherwise "add a USB printer" fails with no actionable message.
    if obj.get("type").and_then(|v| v.as_str()) == Some("printer.save") {
        if let Some(connection) = body.get("printer").and_then(|p| p.get("connection")) {
            crate::printers::check_connection_tag(connection)?;
        }
    }
    let command: Command = serde_json::from_value(body.into())?;
    // Serde's internally tagged unit variants can ignore extra fields even with
    // deny_unknown_fields. Enforce the complete envelope allowlist explicitly
    // for every command, including zero-argument commands such as ping.
    let command_fields: &[&str] = match &command {
        Command::Hello | Command::Ping | Command::AgentStatus |
        Command::PrintersList | Command::QueueList | Command::QueueClear |
        Command::Shutdown | Command::DiscoverNetwork | Command::PrintersInstalled => &[],
        Command::Authenticate { .. } => &["proof"],
        Command::PrinterGet { .. } => &["printerId"],
        Command::PrinterSave { .. } => &["printer"],
        Command::PrinterTest { .. } => &["printerId", "jobId"],
        Command::Print { .. } => &["printerId", "jobId", "document"],
        Command::PrintStatus { .. } | Command::QueueCancel { .. } => &["jobId"],
    };
    if obj.keys().any(|key| {
        !["version", "requestId", "type"].contains(&key.as_str()) &&
            !command_fields.contains(&key.as_str())
    }) {
        return Err(AgentError::new("INVALID_PAYLOAD", "Unknown request field"));
    }
    match &command {
        Command::Print { job_id, printer_id, document } => {
            ids(&[job_id, printer_id])?;
            document.validate().map_err(|mut error| {
                error.printer = Some(printer_id.clone());
                error
            })?;
        }
        Command::PrinterTest { job_id, printer_id } => ids(&[job_id, printer_id])?,
        Command::PrinterGet { printer_id } => ids(&[printer_id])?,
        Command::PrintStatus { job_id } | Command::QueueCancel { job_id } => ids(&[job_id])?,
        Command::Authenticate { proof } if proof.len() > 128 => {
            return Err(AgentError::new("AUTH_FAILED", "Invalid proof"));
        }
        _ => (),
    }
    Ok(Request { version: VERSION, request_id: id.to_owned(), command })
}
fn ids(ids: &[&String]) -> Result<()> {
    if ids.iter().all(|s| valid_id(s)) {
        Ok(())
    } else {
        Err(AgentError::new("INVALID_JOB", "Invalid identifier"))
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn rejects_versions_and_unknown_fields() {
        assert!(parse(r#"{"version":2,"requestId":"x","type":"ping"}"#).is_err());
        assert!(parse(r#"{"version":1,"requestId":"x","type":"ping","host":"1.2.3.4"}"#).is_err());
        assert!(parse(r#"{"version":1,"requestId":"x","type":"ping"}"#).is_ok());
    }
    #[test]
    fn all_commands_reject_unexpected_envelope_fields() {
        use serde_json::json;
        let commands = [
            json!({"type":"hello"}),
            json!({"type":"authenticate","proof":"signature"}),
            json!({"type":"ping"}),
            json!({"type":"agent.status"}),
            json!({"type":"printers.list"}),
            json!({"type":"printer.get","printerId":"p1"}),
            json!({"type":"printer.test","printerId":"p1","jobId":"test:1"}),
            json!({"type":"print","printerId":"p1","jobId":"order:1",
                "document":{"type":"receipt","lines":["test"]}}),
            json!({"type":"print.status","jobId":"order:1"}),
            json!({"type":"queue.list"}),
            json!({"type":"queue.cancel","jobId":"order:1"}),
            json!({"type":"queue.clear"}),
            json!({"type":"discover.network"}),
            json!({"type":"printers.installed"}),
            json!({"type":"agent.shutdown"}),
        ];
        for mut command in commands {
            command["version"] = json!(1);
            command["requestId"] = json!("r1");
            assert!(parse(&command.to_string()).is_ok(), "Valid command: {command}");
            for (field, value) in [
                ("host", json!("1.2.3.4")),
                ("unexpected", json!(null)),
                ("payload", json!({"raw": [27, 64]})),
            ] {
                let mut invalid = command.clone();
                invalid[field] = value;
                let error = parse(&invalid.to_string()).expect_err("Unknown field accepted");
                assert_eq!(error.code, "INVALID_PAYLOAD", "Command: {invalid}");
            }
        }
    }

    #[test]
    fn rejects_unknown_fields_inside_documents() {
        use serde_json::json;
        let mut request = json!({"version":1,"requestId":"r1","type":"print",
            "printerId":"p1","jobId":"order:1",
            "document":{"type":"receipt","lines":["test"],"raw":[27,64]}});
        assert!(parse(&request.to_string()).is_err());
        request["document"] = json!({"type":"invoice","data":{
            "storeName":"Store","orderNumber":"1","items":[
                {"name":"Item","quantity":1,"unitPrice":100,"unknown":true}
            ],"total":100
        }});
        assert!(parse(&request.to_string()).is_err());
    }

    #[test]
    fn invoice_optional_fields_are_accepted_and_validated() {
        use serde_json::json;
        let request = |data: serde_json::Value| {
            json!({"version":1,"requestId":"r1","type":"print","printerId":"p1",
                "jobId":"order:1","document":{"type":"invoice","data":data}}).to_string()
        };
        // Minimal invoice stays valid (backwards compatible).
        let minimal = json!({"storeName":"کافه","orderNumber":"1","items":[
            {"name":"چای","quantity":1,"unitPrice":100}],"total":100});
        assert!(parse(&request(minimal.clone())).is_ok());
        // Full invoice with the app template fields.
        let mut full = minimal.clone();
        for (key, value) in [
            ("title", json!("فاکتور فروش")),
            ("address", json!("زنجان، میدان کوه نورد")),
            ("phone", json!("09120000000")),
            ("date", json!("۱۴۰۵/۰۷/۱۲ ۲۱:۴۷")),
            ("status", json!("تکمیل‌شده")),
            ("orderType", json!("حضوری")),
            ("table", json!("۳")),
            ("note", json!("بدون شکر")),
            ("currency", json!("تومان")),
            ("subtotal", json!(100)),
        ] {
            full[key] = value;
        }
        assert!(parse(&request(full)).is_ok());
        // Limits are enforced on the optional fields too.
        let mut long = minimal.clone();
        long["currency"] = json!("x".repeat(33));
        assert!(parse(&request(long)).is_err());
        let mut subtotal = minimal;
        subtotal["subtotal"] = json!(9_000_000_000_001u64);
        assert!(parse(&request(subtotal)).is_err());
    }

    #[test]
    fn required_command_fields_are_still_enforced() {
        for command in ["authenticate", "printer.get", "printer.test", "print",
            "print.status", "queue.cancel"] {
            let request = serde_json::json!({"version":1,"requestId":"r1","type":command});
            assert!(parse(&request.to_string()).is_err(), "Command: {command}");
        }
    }

    #[test]
    fn rejects_commands_and_control_characters() {
        assert!(parse(r#"{"version":1,"requestId":"x","type":"exec"}"#).is_err());
        assert!(!text_ok("\u{1b}@", 10));
        assert!(!valid_id("../../file"));
    }

    /// The frontend owns the design when it sends raw ESC/POS: the bytes must survive parsing
    /// and projection unchanged, with no command inserted by the agent.
    #[test]
    fn raw_escpos_document_is_preserved_verbatim() {
        let request = parse(
            r#"{"version":1,"requestId":"r1","type":"print","printerId":"p1","jobId":"order:1",
                "document":{"type":"escpos","commands":[27,64,29,86,0]}}"#
        ).unwrap();
        let Command::Print { document, .. } = request.command else {
            panic!("expected a print command")
        };
        assert_eq!(document.escpos_bytes().unwrap(), vec![27, 64, 29, 86, 0]);
        // `commands` first, then the base64 `data`; nothing appended or reordered.
        let both = parse(
            r#"{"version":1,"requestId":"r1","type":"print","printerId":"p1","jobId":"order:1",
                "document":{"type":"escpos","commands":[27,64],"data":"G0A="}}"#
        ).unwrap();
        let Command::Print { document, .. } = both.command else {
            panic!("expected a print command")
        };
        assert_eq!(document.escpos_bytes().unwrap(), vec![27, 64, 27, 64]);
        assert!(document.validate().is_ok());
    }

    #[test]
    fn raw_escpos_document_rejects_empty_bad_and_oversized_payloads() {
        let parse_document = |document: serde_json::Value| {
            parse(
                &serde_json::json!({
                    "version":1,"requestId":"r1","type":"print","printerId":"p1",
                    "jobId":"order:1","document":document
                }).to_string()
            )
        };
        let empty = parse_document(serde_json::json!({"type":"escpos"}));
        assert!(empty.is_err(), "an empty raw document must be rejected");
        let bad_base64 =
            parse_document(serde_json::json!({"type":"escpos","data":"not base64 at all!!"}));
        assert!(bad_base64.is_err());
        // Unknown fields inside a raw document are still refused.
        assert!(
            parse_document(serde_json::json!({"type":"escpos","commands":[27],"drawer":true}))
                .is_err()
        );
        // An unknown document type is refused too — the agent never guesses a rendering.
        assert!(parse_document(serde_json::json!({"type":"html","body":"<b>x</b>"})).is_err());
    }

    /// The decoded-size cap is asserted on the full 1 MiB raw document. The wire envelope has
    /// headroom for base64's 4/3 expansion plus the JSON request fields.
    #[test]
    fn raw_escpos_size_cap_applies_to_the_decoded_bytes() {
        let mut over_bytes = vec![0u8; MAX_ESCPOS + 1];
        over_bytes[..2].copy_from_slice(&[0x1b, 0x40]);
        let over = Document::Escpos { commands: over_bytes, data: String::new() };
        assert_eq!(over.escpos_bytes().unwrap_err().code, "RAW_PAYLOAD_TOO_LARGE");
        assert!(over.validate().is_err());
        let mut at_limit_bytes = vec![0u8; MAX_ESCPOS];
        at_limit_bytes[..2].copy_from_slice(&[0x1b, 0x40]);
        let at_limit = Document::Escpos { commands: at_limit_bytes, data: String::new() };
        assert_eq!(at_limit.escpos_bytes().unwrap().len(), MAX_ESCPOS);
        assert!(at_limit.validate().is_ok());
        // A base64 blob alone is enough; `commands` may stay empty.
        let encoded_only = Document::Escpos { commands: vec![], data: base64_of(&[27, 64]) };
        assert_eq!(encoded_only.escpos_bytes().unwrap(), vec![27, 64]);
    }

    #[test]
    fn raw_escpos_rejects_arbitrary_binary_and_accepts_common_command_signatures() {
        let invalid = Document::Escpos { commands: vec![0x00, 0x01, 0x02], data: String::new() };
        let error = invalid.escpos_bytes().unwrap_err();
        assert_eq!(error.code, "RAW_ESC_POS_INVALID");
        assert!(error.action_required.is_some());
        for signature in [
            vec![0x1b, 0x40],
            vec![0x1b, 0x21, 0],
            vec![0x1d, 0x76, 0x30, 0],
            vec![0x1d, 0x56, 0],
        ] {
            assert!(Document::Escpos { commands: signature, data: String::new() }.validate().is_ok());
        }
    }

    fn base64_of(bytes: &[u8]) -> String {
        use base64::{ engine::general_purpose::STANDARD, Engine };
        STANDARD.encode(bytes)
    }

    /// A connection type this agent does not implement must be rejected **by name**, so the
    /// operator sees `lan` (or whatever the client sent) instead of a generic payload error.
    #[test]
    fn printer_save_names_the_unsupported_connection_type() {
        let save = |connection: serde_json::Value| {
            parse(
                &serde_json::json!({
                    "version":1,"requestId":"r1","type":"printer.save",
                    "printer":{
                        "id":"printer:1","name":"POS","connection":connection,
                        "paperMm":80,"widthDots":576,"copies":1,"cut":true,
                        "fontFamily":"Noto Sans Arabic","fontSize":24
                    }
                }).to_string()
            )
        };
        for (connection, expected) in [
            (serde_json::json!({"type":"lan","host":"192.168.1.50","port":9100}), "lan"),
            (serde_json::json!({"type":"bluetooth","address":"00:11"}), "bluetooth"),
            (serde_json::json!({"type":"USB"}), "USB"),
        ] {
            let error = save(connection).expect_err("must be rejected");
            assert_eq!(error.code, "UNSUPPORTED_CONNECTION_TYPE");
            assert!(
                error.message.contains(&format!("\"{expected}\"")),
                "message {:?} must name {expected}",
                error.message
            );
        }
        for connection in [
            serde_json::json!({"type":"network","host":"192.168.1.50","port":9100}),
            serde_json::json!({"type":"spooler","queueName":"POS-80"}),
            serde_json::json!({"type":"usb","vendorId":1,"productId":2,"serial":null,
                "bus":1,"ports":[1],"interface":0,"endpoint":1,"alternate":0}),
        ] {
            save(connection).expect("supported connection type rejected");
        }
    }
}
