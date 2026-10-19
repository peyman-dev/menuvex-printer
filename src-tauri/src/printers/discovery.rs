//! USB printer discovery entry point.
//!
//! The result shape is stable in builds with and without direct USB support so Tauri commands
//! can report a clear feature-disabled error without compiling the libusb transport.
use serde::Serialize;
use crate::{ error::{ AgentError, Result }, printers::Connection };

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct DiscoveredUsb {
    pub connection: Connection,
    pub manufacturer: Option<String>,
    pub product: Option<String>,
    pub accessible: bool,
    pub access_error: Option<AgentError>,
}

#[cfg(feature = "libusb")]
pub use super::usb::discover;

#[cfg(not(feature = "libusb"))]
pub fn discover() -> Result<Vec<DiscoveredUsb>> {
    tracing::info!(
        target: "usb",
        event = "USB_DISCOVERY_SKIPPED",
        feature = "libusb",
        "direct USB discovery is not included in this build"
    );
    Err(AgentError::new(
        "LIBUSB_FEATURE_DISABLED",
        "Direct USB support is disabled in this build. Use an installed print queue or network printer, or rebuild with --features libusb.",
    ))
}

#[cfg(all(test, not(feature = "libusb")))]
mod tests {
    use super::*;

    #[test]
    fn feature_disabled_discovery_returns_an_actionable_error() {
        let error = match discover() {
            Ok(_) => panic!("feature-disabled discovery must not enumerate USB"),
            Err(error) => error,
        };
        assert_eq!(error.code, "LIBUSB_FEATURE_DISABLED");
        assert!(error.message.contains("--features libusb"));
    }
}
