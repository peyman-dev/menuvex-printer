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
fn default_raw_passthrough_max_bytes() -> usize {
    crate::protocol::MAX_ESCPOS
}

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
#[serde(rename_all = "snake_case", deny_unknown_fields)]
pub struct RawPrinterSettings {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub raw_target: Option<String>,
    #[serde(default)]
    pub force_raw: bool,
    #[serde(default)]
    pub created_generic: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "snake_case", deny_unknown_fields)]
pub struct RawPassthroughConfig {
    #[serde(default)]
    pub enabled: bool,
    #[serde(default = "default_raw_passthrough_max_bytes")]
    pub max_bytes: usize,
    #[serde(default)]
    pub printers: std::collections::HashMap<String, RawPrinterSettings>,
}
impl Default for RawPassthroughConfig {
    fn default() -> Self {
        Self {
            enabled: false,
            max_bytes: default_raw_passthrough_max_bytes(),
            printers: std::collections::HashMap::new(),
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Config {
    pub port: u16,
    pub max_attempts: u32,
    pub autostart: bool,
    /// Local-only raw printing configuration. The explicit snake_case name matches the
    /// persisted schema and is never exposed through a website command.
    #[serde(default, rename = "raw_passthrough")]
    pub raw_passthrough: RawPassthroughConfig,
    pub printers: Vec<Printer>,
    pub routes: Vec<Route>,
}
impl Default for Config {
    fn default() -> Self {
        Self {
            port: 8765,
            max_attempts: 3,
            autostart: true,
            raw_passthrough: RawPassthroughConfig::default(),
            printers: vec![],
            routes: vec![],
        }
    }
}
impl Config {
    pub fn validate(&self) -> Result<()> {
        if
            self.port < 1024 ||
            !(1..=5).contains(&self.max_attempts) ||
            self.printers.len() > 16 ||
            self.routes.len() > 16 ||
            self.raw_passthrough.printers.len() > 16 ||
            !(1024..=crate::protocol::MAX_ESCPOS).contains(&self.raw_passthrough.max_bytes)
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
        for (printer_id, settings) in &self.raw_passthrough.printers {
            if !valid_id(printer_id) || !self.printers.iter().any(|p| p.id == *printer_id) {
                return Err(AgentError::new("INVALID_CONFIG", "Invalid raw printer settings key"));
            }
            if settings.raw_target.as_ref().is_some_and(|queue| {
                queue.trim().is_empty() || queue.chars().any(char::is_control) || queue.len() > 256
            }) {
                return Err(AgentError::new("INVALID_CONFIG", "Invalid raw print queue name"));
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
        c.port = 8765;
        c.raw_passthrough.max_bytes = 1023;
        assert!(c.validate().is_err());
        c.raw_passthrough.max_bytes = crate::protocol::MAX_ESCPOS + 1;
        assert!(c.validate().is_err());
    }

    #[test]
    fn older_config_deserializes_with_safe_raw_defaults() {
        let old = serde_json::json!({
            "port": 8765,
            "maxAttempts": 3,
            "autostart": true,
            "printers": [],
            "routes": []
        });
        let config: Config = serde_json::from_value(old).unwrap();
        assert!(!config.raw_passthrough.enabled);
        assert_eq!(config.raw_passthrough.max_bytes, crate::protocol::MAX_ESCPOS);
        let encoded = serde_json::to_value(&config).unwrap();
        assert_eq!(encoded["raw_passthrough"]["enabled"], false);
        assert!(encoded.get("rawPassthroughEnabled").is_none());
        config.validate().unwrap();
    }
}
