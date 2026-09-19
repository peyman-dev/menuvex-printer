use serde::{ Deserialize, Serialize };
use crate::error::{ AgentError, Result };
pub const VERSION: u8 = 1;
pub const MAX_MESSAGE: usize = 128 * 1024;
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
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct InvoiceData {
    pub store_name: String,
    pub order_number: String,
    pub items: Vec<InvoiceItem>,
    pub total: u64,
    #[serde(default)] pub footer: String,
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
}
impl Document {
    pub fn validate(&self) -> Result<()> {
        let valid = match self {
            Self::Invoice { data } =>
                text_ok(&data.store_name, 300) &&
                    text_ok(&data.order_number, 128) &&
                    text_ok(&data.footer, 1000) &&
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
        };
        if valid {
            Ok(())
        } else {
            Err(AgentError::new("INVALID_JOB", "Document limits or text validation failed"))
        }
    }
    pub fn lines(&self) -> Vec<String> {
        match self {
            Self::Receipt { lines } => lines.clone(),
            Self::Invoice { data: d } => {
                let mut lines = vec![
                    d.store_name.clone(),
                    format!("سفارش: {}", d.order_number),
                    "────────────────".into()
                ];
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
                lines.push("────────────────".into());
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
    let command: Command = serde_json::from_value(body.into())?;
    // Serde's internally tagged unit variants can ignore extra fields even with
    // deny_unknown_fields. Enforce the complete envelope allowlist explicitly
    // for every command, including zero-argument commands such as ping.
    let command_fields: &[&str] = match &command {
        Command::Hello | Command::Ping | Command::AgentStatus |
        Command::PrintersList | Command::QueueList | Command::Shutdown => &[],
        Command::Authenticate { .. } => &["proof"],
        Command::PrinterGet { .. } => &["printerId"],
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
            document.validate()?;
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
}
