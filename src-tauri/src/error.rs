use serde::{Serialize, Deserialize};
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AgentError {
    pub code: String,
    pub message: String,
    pub retryable: bool,
    pub uncertain: bool,
    /// Optional structured context for local actions that the web client can display or branch on.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub printer: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub action_required: Option<String>,
}
pub type Result<T> = std::result::Result<T, AgentError>;
impl AgentError {
    pub fn new(code: &str, message: &str) -> Self {
        Self {
            code: code.into(),
            message: message.into(),
            retryable: false,
            uncertain: false,
            printer: None,
            action_required: None,
        }
    }
    pub fn retry(code: &str) -> Self {
        Self {
            retryable: true,
            ..Self::new(code, "Printer unavailable before sending; retry scheduled within policy")
        }
    }
    pub fn uncertain() -> Self {
        Self {
            uncertain: true,
            ..Self::new(
                "PRINT_OUTCOME_UNKNOWN",
                "Data may have reached the printer. Check paper before explicitly reprinting with a new job ID."
            )
        }
    }
    /// Name the value that was rejected instead of hiding it behind a generic parser message.
    /// `received` comes from the network, so it is truncated and control characters are dropped
    /// before being echoed back into a message that an operator will read.
    pub fn unsupported_connection_type(received: &str) -> Self {
        let cleaned: String = received
            .chars()
            .filter(|c| !c.is_control())
            .take(64)
            .collect();
        let shown: &str = if cleaned.trim().is_empty() { "<missing>" } else { &cleaned };
        Self::new(
            "UNSUPPORTED_CONNECTION_TYPE",
            &format!(
                "Unsupported printer connection type: \"{shown}\". Supported: \"network\", \"usb\", \"spooler\"."
            ),
        )
    }
    /// An unexpected internal failure. The agent stays up; only the affected request fails.
    pub fn internal(detail: &str) -> Self {
        Self::new("AGENT_INTERNAL_ERROR", &format!("Agent stayed up; request failed: {detail}"))
    }
}
impl std::fmt::Display for AgentError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}: {}", self.code, self.message)
    }
}
impl std::error::Error for AgentError {}
impl From<rusqlite::Error> for AgentError {
    fn from(_: rusqlite::Error) -> Self {
        Self::new("QUEUE_ERROR", "Database operation failed")
    }
}
impl From<serde_json::Error> for AgentError {
    fn from(_: serde_json::Error) -> Self {
        Self::new("INVALID_PAYLOAD", "Invalid or unsupported payload")
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::print::queue::Job;

    #[test]
    fn persisted_errors_preserve_retry_and_uncertainty_flags() {
        for original in [
            AgentError::new("USB_ACCESS_DENIED", "Permission required"),
            AgentError::retry("PRINTER_OFFLINE"),
            AgentError::uncertain(),
        ] {
            let encoded = serde_json::to_string(&original).unwrap();
            let restored: AgentError = serde_json::from_str(&encoded).unwrap();
            assert_eq!(restored.code, original.code);
            assert_eq!(restored.message, original.message);
            assert_eq!(restored.retryable, original.retryable);
            assert_eq!(restored.uncertain, original.uncertain);
        }
    }

    #[test]
    fn structured_print_error_context_round_trips_and_legacy_errors_default() {
        let mut error = AgentError::new("RAW_PASSTHROUGH_DISABLED", "Local opt-in required");
        error.printer = Some("POS-80C copy 2".into());
        error.action_required = Some("Enable both local switches in Settings".into());
        let encoded = serde_json::to_value(&error).unwrap();
        assert_eq!(encoded["printer"], "POS-80C copy 2");
        assert_eq!(encoded["actionRequired"], "Enable both local switches in Settings");
        let restored: AgentError = serde_json::from_value(encoded).unwrap();
        assert_eq!(restored.printer, error.printer);
        assert_eq!(restored.action_required, error.action_required);

        let legacy: AgentError = serde_json::from_value(serde_json::json!({
            "code":"PRINTER_OFFLINE","message":"Offline","retryable":true,"uncertain":false
        })).unwrap();
        assert_eq!(legacy.printer, None);
        assert_eq!(legacy.action_required, None);
    }

    #[test]
    fn job_round_trip_preserves_ambiguous_print_error() {
        let job = Job {
            job_id: "order:42:invoice".into(),
            printer_id: "invoice-printer".into(),
            status: "failed".into(),
            attempts: 1,
            created_at: 1,
            next_at: 1,
            error: Some(AgentError::uncertain()),
        };
        let encoded = serde_json::to_string(&job).unwrap();
        let restored: Job = serde_json::from_str(&encoded).unwrap();
        assert_eq!(restored.job_id, job.job_id);
        assert_eq!(restored.status, "failed");
        let error = restored.error.expect("Stored print outcome must not disappear");
        assert_eq!(error.code, "PRINT_OUTCOME_UNKNOWN");
        assert!(error.uncertain);
        assert!(!error.retryable);
    }
}
