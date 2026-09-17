use serde::{ Deserialize, Serialize };
use crate::error::{ AgentError, Result };
pub const VERSION: u8 = 1;
pub const MAX_MESSAGE: usize = 128 * 1024;
pub fn valid_id(s: &str) -> bool {
    !s.is_empty() &&
        s.len() <= 128 &&
        s.bytes().all(|c| (c.is_ascii_alphanumeric() || b"-_: .".contains(&c))) &&
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
    fn rejects_commands_and_control_characters() {
        assert!(parse(r#"{"version":1,"requestId":"x","type":"exec"}"#).is_err());
        assert!(!text_ok("\u{1b}@", 10));
        assert!(!valid_id("../../file"));
    }
}
