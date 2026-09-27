use serde::{Serialize, Deserialize};
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AgentError {
    pub code: String,
    pub message: String,
    pub retryable: bool,
    pub uncertain: bool,
}
pub type Result<T> = std::result::Result<T, AgentError>;
impl AgentError {
    pub fn new(code: &str, message: &str) -> Self {
        Self { code: code.into(), message: message.into(), retryable: false, uncertain: false }
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
