pub mod storage;
use crate::{
    error::{ AgentError, Result },
    printers::Printer,
    protocol::{ text_ok, valid_id },
};
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
            !(1024..=crate::protocol::MAX_ESCPOS).contains(&self.raw_passthrough.max_bytes)
        {
            return Err(
                AgentError::new("INVALID_CONFIG", "Port, attempts or raw byte limit out of range")
            );
        }
        // No cap on the number of printers, print routes (stations) or per-printer raw
        // settings: a cafe defines as many printers and stations as it needs, each with its
        // own operator-chosen name.
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
            // A route role is the operator-facing station label (e.g. "صندوق", "آشپزخانه"),
            // so it accepts free text like a printer name — unique and non-empty only.
            if
                r.role.trim().is_empty() ||
                !text_ok(&r.role, 128) ||
                !roles.insert(&r.role) ||
                !ids.contains(&r.printer_id)
            {
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

    fn network_printer(id: &str, name: &str) -> Printer {
        Printer {
            id: id.into(),
            name: name.into(),
            connection: crate::printers::Connection::Network {
                host: "192.168.1.50".into(),
                port: 9100,
            },
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

    /// A cafe defines as many printers and stations as it needs; there is no count cap, and
    /// station labels are free text (Persian names such as "صندوق" included).
    #[test]
    fn unlimited_printers_and_custom_station_routes_validate() {
        let mut c = Config::default();
        for index in 0..64 {
            c.printers.push(network_printer(
                &format!("printer:{index}"),
                &format!("پرینتر {index}"),
            ));
        }
        for (index, station) in
            ["صندوق", "آشپزخانه", "بار", "takeaway", "kiosk", "VIP"].iter().enumerate()
        {
            c.routes.push(Route {
                role: station.to_string(),
                printer_id: c.printers[index].id.clone(),
                auto_print: true,
            });
        }
        for printer in &c.printers {
            c.raw_passthrough.printers.insert(printer.id.clone(), RawPrinterSettings::default());
        }
        assert!(c.validate().is_ok(), "no printer/route/raw-settings count cap");

        // Duplicate station labels stay ambiguous and are rejected.
        c.routes.push(Route {
            role: "صندوق".into(),
            printer_id: c.printers[63].id.clone(),
            auto_print: false,
        });
        assert!(c.validate().is_err(), "duplicate station labels must be rejected");
        c.routes.pop();

        // A station pointing at an unknown printer is still rejected.
        c.routes.push(Route {
            role: "انبار".into(),
            printer_id: "printer:missing".into(),
            auto_print: false,
        });
        assert!(c.validate().is_err(), "routes must reference a configured printer");
        c.routes.pop();

        // Blank station labels are still rejected.
        c.routes.push(Route {
            role: "   ".into(),
            printer_id: c.printers[0].id.clone(),
            auto_print: false,
        });
        assert!(c.validate().is_err(), "blank station labels must be rejected");
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
