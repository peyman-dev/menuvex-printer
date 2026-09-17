use serde::Serialize;
#[derive(Debug, Clone, Serialize)]
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
