pub mod storage;
use crate::{ error::{ AgentError, Result }, printers::Printer, protocol::valid_id };
use serde::{ Serialize, Deserialize };
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Route {
    pub role: String,
    pub printer_id: String,
    pub auto_print: bool,
}
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Config {
    pub port: u16,
    pub max_attempts: u32,
    pub autostart: bool,
    pub printers: Vec<Printer>,
    pub routes: Vec<Route>,
}
impl Default for Config {
    fn default() -> Self {
        Self { port: 8765, max_attempts: 3, autostart: true, printers: vec![], routes: vec![] }
    }
}
impl Config {
    pub fn validate(&self) -> Result<()> {
        if
            self.port < 1024 ||
            !(1..=5).contains(&self.max_attempts) ||
            self.printers.len() > 16 ||
            self.routes.len() > 16
        {
            return Err(
                AgentError::new("INVALID_CONFIG", "Port, attempts or printer count out of range")
            );
        }
        let mut ids = std::collections::HashSet::new();
        for p in &self.printers {
            p.validate()?;
            if !ids.insert(&p.id) {
                return Err(AgentError::new("INVALID_CONFIG", "Duplicate printer ID"));
            }
        }
        let mut roles = std::collections::HashSet::new();
        for r in &self.routes {
            if !valid_id(&r.role) || !roles.insert(&r.role) || !ids.contains(&r.printer_id) {
                return Err(AgentError::new("INVALID_CONFIG", "Invalid printer route"));
            }
        }
        Ok(())
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn limits() {
        let mut c = Config::default();
        assert!(c.validate().is_ok());
        c.port = 80;
        assert!(c.validate().is_err());
    }
}
