//! Safe failover policy for direct USB printing.
//!
//! A fallback is safe only before the first USB write is attempted. Once a write starts, a
//! transfer error or timeout may have delivered some bytes, so sending the same job through a
//! second transport could duplicate or corrupt the receipt.
use crate::{ error::{ AgentError, Result }, printers::Connection };

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum UsbFailureStage {
    PreOpen,
    PostOpenPreWrite,
    MidWrite,
    Timeout,
    Unknown,
}
impl UsbFailureStage {
    pub const fn allows_fallback(self) -> bool {
        matches!(self, Self::PreOpen | Self::PostOpenPreWrite)
    }
}
impl std::fmt::Display for UsbFailureStage {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(match self {
            Self::PreOpen => "PreOpen",
            Self::PostOpenPreWrite => "PostOpenPreWrite",
            Self::MidWrite => "MidWrite",
            Self::Timeout => "Timeout",
            Self::Unknown => "Unknown",
        })
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum FallbackDecision {
    Auto,
    Ask,
    Forbid,
}
impl FallbackDecision {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Auto => "auto",
            Self::Ask => "ask",
            Self::Forbid => "forbid",
        }
    }
}

pub const fn fallback_decision(
    stage: UsbFailureStage,
    has_configured_target: bool,
    bytes_written_before_failure: Option<usize>,
) -> FallbackDecision {
    let zero_bytes_confirmed = match bytes_written_before_failure {
        Some(0) => true,
        _ => false,
    };
    if !stage.allows_fallback() || !zero_bytes_confirmed {
        FallbackDecision::Forbid
    } else if has_configured_target {
        FallbackDecision::Auto
    } else {
        FallbackDecision::Ask
    }
}

/// USB-specific failure details retained until the safe fallback decision has been made.
#[derive(Debug)]
pub struct UsbFailure {
    pub stage: UsbFailureStage,
    pub cause: AgentError,
    /// libusb's documented negative error number, when the failure came from libusb itself.
    pub libusb_error_code: Option<i32>,
    /// The rusb/libusb error variant name (for example `NotSupported`).
    pub libusb_error_name: Option<String>,
    /// Bytes from completed transfers before the failing operation. `None` means the count is
    /// unknown (notably when the outer wall-clock deadline expires).
    pub bytes_written_before_failure: Option<usize>,
}
impl UsbFailure {
    pub fn new(
        stage: UsbFailureStage,
        mut cause: AgentError,
        libusb_error_code: Option<i32>,
        libusb_error_name: Option<String>,
        bytes_written_before_failure: Option<usize>,
    ) -> Self {
        // A pre-write label alone is insufficient: only a confirmed zero-byte count is safe.
        let stage = if stage.allows_fallback() && bytes_written_before_failure != Some(0) {
            UsbFailureStage::Unknown
        } else {
            stage
        };
        if !stage.allows_fallback() {
            // A write may have reached the device, or the operation may have been abandoned while
            // its worker was still running. Never let the queue retry an ambiguous USB result.
            cause.retryable = false;
            cause.uncertain = true;
        }
        Self {
            stage,
            cause,
            libusb_error_code,
            libusb_error_name,
            bytes_written_before_failure,
        }
    }

    pub fn feature_disabled() -> Self {
        Self::new(
            UsbFailureStage::PreOpen,
            AgentError::new(
                "LIBUSB_FEATURE_DISABLED",
                "Direct USB support is not included in this build; use a configured fallback or rebuild with --features libusb",
            ),
            None,
            None,
            Some(0),
        )
    }

    pub fn into_agent_error(self, decision: FallbackDecision) -> AgentError {
        let mut error = self.cause;
        if decision == FallbackDecision::Forbid {
            error.retryable = false;
            error.uncertain = true;
        }
        let guidance = match decision {
            FallbackDecision::Ask =>
                " No USB fallback target is configured; choose an installed print queue or network target in local settings.",
            FallbackDecision::Forbid =>
                " Automatic fallback was forbidden to prevent a duplicate or corrupted print; inspect the paper before retrying.",
            FallbackDecision::Auto => "",
        };
        error.message = format!("USB {} failure: {}{}", self.stage, error.message, guidance);
        error
    }
}

/// Decide and perform a fallback. The injected sender keeps the policy testable without USB
/// hardware and guarantees that a forbidden stage never invokes another transport.
pub fn apply_fallback(
    failure: UsbFailure,
    fallback_target: Option<&Connection>,
    send_fallback: impl FnOnce(&Connection) -> Result<()>,
) -> Result<()> {
    let decision = fallback_decision(
        failure.stage,
        fallback_target.is_some(),
        failure.bytes_written_before_failure,
    );
    apply_fallback_with_decision(failure, fallback_target, decision, send_fallback)
}

/// Apply an already-computed decision. `Forbid` is used after an earlier copy of the same job
/// has been delivered: a later pre-open error is technically safe for that transfer, but the
/// whole job has already emitted bytes and must remain on its original route.
pub fn apply_fallback_with_decision(
    failure: UsbFailure,
    fallback_target: Option<&Connection>,
    decision: FallbackDecision,
    send_fallback: impl FnOnce(&Connection) -> Result<()>,
) -> Result<()> {
    let decision = if !failure.stage.allows_fallback()
        || failure.bytes_written_before_failure != Some(0)
    {
        FallbackDecision::Forbid
    } else {
        decision
    };
    match (decision, fallback_target) {
        (FallbackDecision::Auto, Some(target)) => {
            if !matches!(target, Connection::Spooler { .. } | Connection::Network { .. }) {
                return Err(AgentError::new(
                    "INVALID_CONFIG",
                    "USB fallback must be an installed print queue or network connection",
                ));
            }
            match send_fallback(target) {
                Ok(()) => Ok(()),
                Err(mut fallback_error) => {
                    let usb_stage = failure.stage;
                    let usb_message = failure.cause.message;
                    fallback_error.message = format!(
                        "USB {usb_stage} failure before the first byte: {usb_message}. Configured fallback also failed: {}",
                        fallback_error.message,
                    );
                    Err(fallback_error)
                }
            }
        }
        (FallbackDecision::Auto, None) =>
            Err(failure.into_agent_error(FallbackDecision::Ask)),
        (decision, _) => Err(failure.into_agent_error(decision)),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::{ atomic::{ AtomicUsize, Ordering }, Arc };

    fn queue() -> Connection {
        Connection::Spooler { queue_name: "POS-80".into() }
    }

    fn failure(stage: UsbFailureStage) -> UsbFailure {
        UsbFailure::new(
            stage,
            AgentError::new("USB_DEVICE_ERROR", "test libusb failure"),
            Some(-12),
            Some("NotSupported".into()),
            if matches!(stage, UsbFailureStage::PreOpen | UsbFailureStage::PostOpenPreWrite) {
                Some(0)
            } else {
                None
            },
        )
    }

    #[test]
    fn pre_open_falls_back_only_to_an_explicit_target() {
        let calls = Arc::new(AtomicUsize::new(0));
        let observed = calls.clone();
        let result = apply_fallback(failure(UsbFailureStage::PreOpen), Some(&queue()), move |target| {
            assert!(matches!(target, Connection::Spooler { queue_name } if queue_name == "POS-80"));
            observed.fetch_add(1, Ordering::SeqCst);
            Ok(())
        });
        assert!(result.is_ok());
        assert_eq!(calls.load(Ordering::SeqCst), 1);
    }

    #[test]
    fn post_open_pre_write_falls_back_only_to_an_explicit_target() {
        let calls = Arc::new(AtomicUsize::new(0));
        let observed = calls.clone();
        let result = apply_fallback(
            failure(UsbFailureStage::PostOpenPreWrite),
            Some(&queue()),
            move |_| {
                observed.fetch_add(1, Ordering::SeqCst);
                Ok(())
            },
        );
        assert!(result.is_ok());
        assert_eq!(calls.load(Ordering::SeqCst), 1);
    }

    #[test]
    fn safe_prewrite_failure_without_target_returns_clear_error() {
        let calls = Arc::new(AtomicUsize::new(0));
        let observed = calls.clone();
        let error = apply_fallback(failure(UsbFailureStage::PreOpen), None, move |_| {
            observed.fetch_add(1, Ordering::SeqCst);
            Ok(())
        }).expect_err("missing fallback target must not route automatically");
        assert_eq!(calls.load(Ordering::SeqCst), 0);
        assert!(error.message.contains("No USB fallback target is configured"));
        assert!(!error.uncertain);
    }

    #[test]
    fn prewrite_stage_without_confirmed_zero_bytes_is_reclassified_as_unknown() {
        for written in [None, Some(1)] {
            let failure = UsbFailure::new(
                UsbFailureStage::PreOpen,
                AgentError::new("USB_DEVICE_ERROR", "ambiguous byte count"),
                None,
                None,
                written,
            );
            assert_eq!(failure.stage, UsbFailureStage::Unknown);
            assert_eq!(
                fallback_decision(failure.stage, true, failure.bytes_written_before_failure),
                FallbackDecision::Forbid
            );
        }
    }

    #[test]
    fn mid_write_timeout_and_unknown_never_fall_back() {
        for stage in [
            UsbFailureStage::MidWrite,
            UsbFailureStage::Timeout,
            UsbFailureStage::Unknown,
        ] {
            let calls = Arc::new(AtomicUsize::new(0));
            let observed = calls.clone();
            let error = apply_fallback(failure(stage), Some(&queue()), move |_| {
                observed.fetch_add(1, Ordering::SeqCst);
                Ok(())
            }).expect_err("ambiguous USB failure must not fall back");
            assert_eq!(calls.load(Ordering::SeqCst), 0, "fallback called for {stage}");
            assert!(error.uncertain, "uncertainty lost for {stage}");
            assert!(error.message.contains("Automatic fallback was forbidden"));
        }
    }

    #[test]
    fn build_without_libusb_uses_only_the_configured_fallback() {
        let target = queue();
        let mut attempted = false;
        apply_fallback(UsbFailure::feature_disabled(), Some(&target), |actual| {
            attempted = true;
            assert_eq!(actual, &target);
            Ok(())
        }).expect("feature-disabled is a safe pre-open condition when zero bytes are confirmed");
        assert!(attempted);
    }

    #[test]
    fn build_without_libusb_uses_the_same_safe_preopen_policy() {
        let error = apply_fallback(UsbFailure::feature_disabled(), None, |_| {
            panic!("must not choose an unconfigured target")
        }).expect_err("disabled feature must fail clearly when no fallback is configured");
        assert_eq!(error.code, "LIBUSB_FEATURE_DISABLED");
        assert!(error.message.contains("--features libusb"));
    }

    #[test]
    fn a_preopen_failure_after_a_prior_copy_is_forbidden_from_failing_over() {
        let calls = Arc::new(AtomicUsize::new(0));
        let observed = calls.clone();
        let error = apply_fallback_with_decision(
            failure(UsbFailureStage::PreOpen),
            Some(&queue()),
            FallbackDecision::Forbid,
            move |_| {
                observed.fetch_add(1, Ordering::SeqCst);
                Ok(())
            },
        ).expect_err("copies already delivered means the route must remain pinned");
        assert_eq!(calls.load(Ordering::SeqCst), 0);
        assert!(error.uncertain);
        assert!(error.message.contains("Automatic fallback was forbidden"));
    }

    #[test]
    fn usb_cannot_be_used_as_its_own_fallback() {
        let usb = Connection::Usb {
            vendor_id: 1,
            product_id: 2,
            serial: None,
            bus: 1,
            ports: vec![1],
            interface: 0,
            endpoint: 1,
            alternate: 0,
        };
        let error = apply_fallback(failure(UsbFailureStage::PreOpen), Some(&usb), |_| {
            panic!("invalid fallback must not be sent")
        }).expect_err("USB cannot be a USB fallback");
        assert_eq!(error.code, "INVALID_CONFIG");
    }
}
