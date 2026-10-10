use std::{
    sync::{ Arc, Mutex, MutexGuard, atomic::{ AtomicBool, AtomicU64, Ordering } },
    collections::HashMap,
    time::Duration,
};
use serde_json::{ json, Value };
use tokio::sync::{ broadcast, Notify };
use crate::{
    config::{ Config, storage::Storage },
    error::{ AgentError, Result },
    print::{ Renderer, queue::{ now, deliver, Job } },
    printers::{ Printer, Transport },
    protocol::{ Command, Document },
    security::Secret,
};
pub struct State {
    pub store: Mutex<Storage>,
    pub secret: Mutex<Secret>,
    pub epoch: AtomicU64,
    pub events: broadcast::Sender<Value>,
    pub wake: Notify,
    pub stopping: AtomicBool,
    pub worker_done: AtomicBool,
    pub ready: AtomicBool,
    pub server_error: Mutex<Option<String>>,
    pub statuses: Mutex<HashMap<String, String>>,
    pub active: Mutex<Option<String>>,
    pub transport: Arc<dyn Transport>,
}

/// Lock a mutex and recover from poisoning instead of panicking.
///
/// Every value behind these mutexes is safe to reuse after unwinding: the SQLite transaction
/// objects are locals that roll back during the unwind, `Renderer` only owns a font database and
/// a glyph cache (the mutable `Paper` is a local), and the rest are plain data. The alternative
/// — `lock().unwrap()` — turned a single panic inside one printer's job into a permanently
/// poisoned mutex, after which `agent.status`, `printers.list` and `queue.list` all started
/// failing and the website looked like it had lost the agent entirely.
pub fn lock<'a, T>(mutex: &'a Mutex<T>, what: &'static str) -> MutexGuard<'a, T> {
    mutex
        .lock()
        .unwrap_or_else(|poisoned| {
            tracing::error!(
                target: "lock",
                event = "MUTEX_POISON_RECOVERED",
                mutex = what,
                backtrace = %std::backtrace::Backtrace::capture(),
                "recovering a poisoned mutex instead of failing the agent"
            );
            poisoned.into_inner()
        })
}

/// Render a panic payload for the log. `Box<dyn Any>` has no `Debug`, so pull the string out of
/// the two shapes `panic!` actually produces.
fn panic_detail(payload: &(dyn std::any::Any + Send)) -> String {
    payload
        .downcast_ref::<String>()
        .cloned()
        .or_else(|| payload.downcast_ref::<&'static str>().map(|s| (*s).to_owned()))
        .unwrap_or_else(|| "non-string panic payload".to_owned())
}

/// A repeatable width-check ticket. The profile dimensions are printed as a label and are also
/// used by `Renderer::encode` for wrapping and raster width; the agent cannot query physical paper
/// width generically through ESC/POS or an installed spooler driver.
fn test_receipt_lines(paper_mm: u16, width_dots: u16) -> Vec<String> {
    vec![
        "[center] MenuVex printer test".into(),
        format!("Paper profile | {paper_mm} mm | {width_dots} dots"),
        "Item | Qty | Amount".into(),
        "Espresso | 2 | 240,000".into(),
        "----------------------------------------".into(),
        "آزمون چاپ فارسی — سلام دنیا".into(),
        "۰۱۲۳۴۵۶۷۸۹ / 0123456789".into(),
    ]
}

fn raw_hex_preview(bytes: &[u8]) -> String {
    bytes.iter().take(32).map(|byte| format!("{byte:02X}")).collect::<Vec<_>>().join(" ")
}

fn unix_time_ms() -> u128 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis()
}

/// Structured job lifecycle log. Job IDs and printer IDs are diagnostics the operator needs;
/// document contents and receipt data are deliberately never logged.
fn log_job(event: &'static str, job: &Job, printer: &Printer) {
    let (vendor_id, product_id) = match &printer.connection {
        crate::printers::Connection::Usb { vendor_id, product_id, .. } =>
            (Some(*vendor_id), Some(*product_id)),
        _ => (None, None),
    };
    tracing::info!(
        target: "print",
        event,
        job_id = %job.job_id,
        printer_id = %printer.id,
        connection = printer.connection.kind(),
        vendor_id = vendor_id.unwrap_or(0),
        product_id = product_id.unwrap_or(0),
        attempts = job.attempts,
        status = %job.status,
        error_code = job.error.as_ref().map(|e| e.code.as_str()).unwrap_or(""),
        uncertain = job.error.as_ref().is_some_and(|e| e.uncertain),
        "print job lifecycle"
    );
}

impl State {
    pub fn new(store: Storage, secret: Secret, transport: Arc<dyn Transport>) -> Arc<Self> {
        let (events, _) = broadcast::channel(256);
        Arc::new(Self {
            store: Mutex::new(store),
            secret: Mutex::new(secret),
            epoch: AtomicU64::new(0),
            events,
            wake: Notify::new(),
            stopping: AtomicBool::new(false),
            worker_done: AtomicBool::new(false),
            ready: AtomicBool::new(false),
            server_error: Mutex::new(None),
            statuses: Mutex::new(HashMap::new()),
            active: Mutex::new(None),
            transport,
        })
    }
    pub fn config(&self) -> Result<Config> {
        lock(&self.store, "store").config()
    }
    pub fn printer(&self, id: &str) -> Result<Printer> {
        self.config()?
            .printers.into_iter()
            .find(|p| p.id == id)
            .ok_or_else(|| AgentError::new("PRINTER_NOT_FOUND", "Printer is not configured"))
    }
    pub fn event(&self, value: Value) {
        let _ = self.events.send(value);
    }
    pub fn job_event(&self, j: &Job) {
        self.event(json!({"type":format!("print.{}",j.status),"version":1,"job":j}));
    }
    pub fn printer_views(&self) -> Result<Value> {
        let statuses = lock(&self.statuses, "statuses");
        let active = lock(&self.active, "active");
        Ok(
            json!(
                self
                    .config()?
                    .printers.iter()
                    .map(|p| {
                        let mut v = serde_json::to_value(p).unwrap_or(Value::Null);
                        // Keep the local config and print queue truthful (`type: spooler`), but
                        // publish the reserved USB-shaped descriptor expected by older clients.
                        // The website-facing wire therefore only ever carries `network` or `usb`,
                        // which is what keeps a strict discriminated union on the frontend valid.
                        v["connection"] = p.connection.api_value();
                        v["status"] = json!(
                            if active.as_deref() == Some(&p.id) {
                                "busy"
                            } else {
                                statuses.get(&p.id).map(String::as_str).unwrap_or("unknown")
                            }
                        );
                        v
                    })
                    .collect::<Vec<_>>()
            )
        )
    }
    pub fn dispatch(&self, cmd: Command) -> Result<Value> {
        if self.stopping.load(Ordering::SeqCst) {
            return Err(AgentError::new("AGENT_NOT_READY", "Agent is stopping"));
        }
        match cmd {
            Command::Hello | Command::Ping =>
                Ok(json!({"agentVersion":env!("CARGO_PKG_VERSION"),"version":1})),
            Command::AgentStatus => {
                let c = self.config()?;
                Ok(
                    json!({"ready":self.ready.load(Ordering::SeqCst),"agentVersion":env!("CARGO_PKG_VERSION"),"port":c.port,"routes":c.routes,"serverError":*lock(&self.server_error,"server_error")})
                )
            }
            Command::PrintersList => self.printer_views(),
            Command::PrinterGet { printer_id } =>
                self
                    .printer_views()?
                    .as_array()
                    .and_then(|a|
                        a
                            .iter()
                            .find(|p| p["id"] == printer_id)
                            .cloned()
                    )
                    .ok_or_else(|| AgentError::new("PRINTER_NOT_FOUND", "Printer not configured")),
            Command::Print { printer_id, job_id, document } =>
                self.enqueue(&job_id, &printer_id, &document),
            Command::PrinterTest { printer_id, job_id } => {
                let printer = self.printer(&printer_id)?;
                self.enqueue(
                    &job_id,
                    &printer_id,
                    &Document::Receipt {
                        lines: test_receipt_lines(printer.paper_mm, printer.width_dots),
                    },
                )
            }
            Command::PrintStatus { job_id } =>
                Ok(json!(lock(&self.store, "store").job(&job_id)?)),
            Command::QueueList => Ok(json!(lock(&self.store, "store").queue()?)),
            Command::QueueCancel { job_id } => {
                let j = lock(&self.store, "store").cancel(&job_id)?;
                self.job_event(&j);
                Ok(json!(j))
            }
            Command::QueueClear => {
                let (cancelled, removed) = lock(&self.store, "store").clear()?;
                for job in &cancelled {
                    self.job_event(job);
                }
                self.event(json!({"type":"resync","version":1}));
                Ok(json!({"cancelled":cancelled.len(),"removed":removed}))
            }
            Command::DiscoverNetwork =>
                Ok(json!(crate::printers::network::discover()?)),
            Command::PrintersInstalled =>
                Ok(json!(crate::printers::spooler::list()?)),
            Command::PrinterSave { printer } => self.save_printer(printer),
            Command::Shutdown =>
                Err(
                    AgentError::new(
                        "LOCAL_CONFIRMATION_REQUIRED",
                        "Quit is available only from the local system tray"
                    )
                ),
            Command::Authenticate { .. } =>
                Err(
                    AgentError::new(
                        "ALREADY_AUTHENTICATED",
                        "Authentication is handled by the connection handshake"
                    )
                ),
        }
    }
    fn save_printer(&self, mut printer: Printer) -> Result<Value> {
        // Older frontends can only send USB descriptors. Accept our reserved `queue:` form and
        // store a real spooler profile so transport and status handling stay explicit internally.
        printer.connection.normalize_api_compat();
        // Raw passthrough, target selection, force mode, and USB fallback are local operator
        // settings. A remote printer.save cannot enable or redirect any of them.
        let mut config = self.config()?;
        if let Some(existing) = config.printers.iter().find(|p| p.id == printer.id) {
            printer.raw_passthrough = existing.raw_passthrough;
            printer.force_raw = false;
            printer.usb_fallback_target = if matches!(&printer.connection, crate::printers::Connection::Usb { .. }) {
                existing.usb_fallback_target.clone()
            } else {
                None
            };
        } else {
            printer.raw_passthrough = false;
            printer.force_raw = false;
            printer.usb_fallback_target = None;
        }
        printer.validate()?;
        let id = printer.id.clone();
        if let Some(slot) = config.printers.iter_mut().find(|p| p.id == id) {
            *slot = printer;
        } else {
            if config.printers.len() >= 16 {
                return Err(AgentError::new("INVALID_CONFIG", "At most 16 printer profiles"));
            }
            config.printers.push(printer);
        }
        config.validate()?;
        lock(&self.store, "store").save_config(&config)?;
        self.event(json!({"type":"resync","version":1}));
        let saved = self
            .printer_views()?
            .as_array()
            .and_then(|a| a.iter().find(|p| p["id"] == id).cloned())
            .ok_or_else(|| AgentError::new("PRINTER_NOT_FOUND", "Saved printer not found"))?;
        let registered = config.printers.iter().find(|p| p.id == id);
        if let Some(p) = registered {
            tracing::info!(
                target: "printer",
                event = "PRINTER_REGISTERED",
                printer_id = %p.id,
                connection = p.connection.kind(),
                paper_mm = p.paper_mm,
                width_dots = p.width_dots,
                "printer profile saved"
            );
        }
        Ok(saved)
    }
    fn enqueue(&self, id: &str, printer_id: &str, document: &Document) -> Result<Value> {
        if self.worker_done.load(Ordering::SeqCst) {
            return Err(
                AgentError::new(
                    "AGENT_NOT_READY",
                    "Queue worker is stopped; restart and reconcile pending jobs"
                )
            );
        }
        let config = self.config()?;
        let mut p = config
            .printers.iter()
            .find(|p| p.id == printer_id)
            .cloned()
            .ok_or_else(|| AgentError::new("PRINTER_NOT_FOUND", "Printer is not configured"))?;
        document.validate().map_err(|mut error| {
            error.printer = Some(p.name.clone());
            error
        })?;
        if matches!(document, Document::Escpos { .. }) {
            if !config.raw_passthrough.enabled || !p.raw_passthrough {
                let mut error = AgentError::new(
                    "RAW_PASSTHROUGH_DISABLED",
                    "Raw ESC/POS documents are disabled by the local operator.",
                );
                error.printer = Some(p.name.clone());
                error.action_required = Some(if !config.raw_passthrough.enabled {
                    "Enable the global Raw ESC/POS switch and this printer's local switch in Settings. A website cannot enable them.".into()
                } else {
                    "Enable Raw ESC/POS for this printer in the local Settings window. A website cannot enable it.".into()
                });
                return Err(error);
            }
            // The configured raw target is local-only. It changes only this raw job's route,
            // never rendered invoices or later copies, and is not an automatic failure fallback.
            let raw_settings = config.raw_passthrough.printers.get(&p.id);
            // Zero-touch default: a printer with no saved raw entry prints raw ESC/POS with
            // the RAW spooler datatype so vendor drivers cannot reinterpret the receipt.
            // Rendered documents are unaffected (force_raw stays false for them below).
            p.force_raw = raw_settings.map(|settings| settings.force_raw).unwrap_or(true);
            if let Some(queue_name) = raw_settings.and_then(|settings| settings.raw_target.clone()) {
                p.connection = crate::printers::Connection::Spooler { queue_name };
                p.usb_fallback_target = None;
            }
            let bytes = document.escpos_bytes()?;
            if bytes.len() > config.raw_passthrough.max_bytes {
                let mut error = AgentError::new(
                    "RAW_PAYLOAD_TOO_LARGE",
                    &format!(
                        "Raw ESC/POS document exceeds the configured {}-byte limit",
                        config.raw_passthrough.max_bytes
                    ),
                );
                error.printer = Some(p.name.clone());
                error.action_required = Some(format!(
                    "Reduce this ESC/POS document to at most {} bytes in Settings.",
                    config.raw_passthrough.max_bytes
                ));
                return Err(error);
            }
            let target = match &p.connection {
                crate::printers::Connection::Spooler { queue_name } => queue_name.clone(),
                crate::printers::Connection::Network { host, port } => format!("{host}:{port}"),
                crate::printers::Connection::Usb { vendor_id, product_id, .. } => {
                    format!("direct-usb:{vendor_id:04X}:{product_id:04X}")
                }
            };
            let preview = raw_hex_preview(&bytes);
            tracing::info!(
                target: "raw_print",
                event = "RAW_ESC_POS_ACCEPTED",
                timestamp_unix_ms = unix_time_ms(),
                printer = %p.name,
                printer_id = %p.id,
                printer_target = %target,
                payload_bytes = bytes.len(),
                first_32_bytes_hex = %preview,
                force_raw = p.force_raw,
                "accepted local-operator-enabled raw ESC/POS document"
            );
        } else {
            // Force RAW is intentionally scoped to ESC/POS; ordinary rendered print routes and
            // their existing spooler defaults are not modified.
            p.force_raw = false;
        }
        let job = lock(&self.store, "store").enqueue(id, &p, document)?;
        log_job("PRINT_JOB_RECEIVED", &job, &p);
        self.job_event(&job);
        self.wake.notify_one();
        Ok(json!(job))
    }
}
/// The queue worker. It is deliberately **restart-proof**: no failure inside a job — a poisoned
/// lock, a SQLite hiccup, a panicking renderer or a USB device that wedges — is allowed to end
/// the loop. It logs, backs off and keeps going, because a permanently stopped worker is exactly
/// the "agent became unresponsive after adding a USB printer" outage this replaces.
pub async fn worker(s: Arc<State>) {
    use futures_util::FutureExt;
    if let Err(panic) = std::panic::AssertUnwindSafe(worker_loop(s.clone())).catch_unwind().await {
        tracing::error!(
            target: "queue",
            event = "QUEUE_WORKER_PANIC",
            panic = %panic_detail(&panic),
            backtrace = %std::backtrace::Backtrace::capture(),
            "queue worker stopped unexpectedly"
        );
    }
    s.worker_done.store(true, Ordering::SeqCst);
}
/// Idle poll interval when the queue is empty.
const IDLE_POLL: Duration = Duration::from_secs(1);
/// Consecutive internal failures tolerated before the worker reports a degraded state.
const DEGRADED_AFTER: u32 = 3;
/// Longest pause between attempts while the worker is in a failure backoff.
const MAX_FAILURE_PAUSE: Duration = Duration::from_secs(15);
/// Prefix of the only `server_error` message the queue worker is allowed to write or clear.
const QUEUE_DEGRADED_PREFIX: &str = "QUEUE_DEGRADED: ";

async fn worker_loop(s: Arc<State>) {
    let renderer = Arc::new(Mutex::new(Renderer::default()));
    let mut failures = 0u32;
    while !s.stopping.load(Ordering::SeqCst) {
        let state = s.clone();
        let renderer = renderer.clone();
        let result = tokio::task::spawn_blocking(move || -> Result<bool> {
            let (work, max) = {
                let mut store = lock(&state.store, "store");
                let max = store.config()?.max_attempts;
                (store.claim(now())?, max)
            };
            let Some(w) = work else {
                return Ok(false);
            };
            *lock(&state.active, "active") = Some(w.printer.id.clone());
            state.job_event(&w.job);
            log_job("PRINT_JOB_STARTED", &w.job, &w.printer);
            // Recover a poisoned renderer rather than losing every later job: `Renderer` owns a
            // font database and a glyph cache, and the mutable `Paper` is a local that unwinds.
            let mut painter = lock(&renderer, "renderer");
            let result = painter
                .encode(&w.printer, &w.document)
                .and_then(|bytes: Vec<u8>| deliver(state.transport.as_ref(), &w.printer, &bytes));
            drop(painter);
            *lock(&state.active, "active") = None;
            let job =
                lock(&state.store, "store").finish(&w.job.job_id, result, max, now())?;
            state.job_event(&job);
            log_job(
                match job.status.as_str() {
                    "completed" => "PRINT_JOB_COMPLETED",
                    "failed" => "PRINT_JOB_FAILED",
                    // Retryable transport failure: the job is back in the queue, not lost.
                    _ => "PRINT_JOB_RETRY_SCHEDULED",
                },
                &job,
                &w.printer
            );
            Ok(true)
        }).await;
        match result {
            Ok(Ok(true)) => {
                failures = 0;
                continue;
            }
            Ok(Ok(false)) => {
                failures = 0;
            }
            Ok(Err(e)) => {
                failures += 1;
                tracing::error!(
                    target: "queue",
                    event = "QUEUE_WORKER_ERROR",
                    consecutive_failures = failures,
                    error_code = %e.code,
                    error_message = %e.message,
                    "queue worker iteration failed; the worker stays alive and keeps the queue"
                );
            }
            Err(join) => {
                failures += 1;
                tracing::error!(
                    target: "queue",
                    event = "QUEUE_WORKER_PANIC",
                    consecutive_failures = failures,
                    panic = %join,
                    backtrace = %std::backtrace::Backtrace::capture(),
                    "a print job panicked; only that job is lost, the queue and the WebSocket survive"
                );
            }
        }
        // Only ever touch our own marker: `server_error` also carries the listener's port failure,
        // and the queue worker must not clear a message it did not write.
        {
            let mut guard = lock(&s.server_error, "server_error");
            let ours = guard.as_deref().is_some_and(|m| m.starts_with(QUEUE_DEGRADED_PREFIX));
            if failures >= DEGRADED_AFTER {
                *guard = Some(
                    format!(
                        "{QUEUE_DEGRADED_PREFIX}{failures} consecutive internal failures; jobs are still accepted and retried"
                    )
                );
            } else if ours {
                *guard = None;
            }
        }
        let pause =
            if failures == 0 {
                IDLE_POLL
            } else {
                MAX_FAILURE_PAUSE.min(Duration::from_secs(1u64 << failures.min(4)))
            };
        tokio::select! { _=s.wake.notified()=>(),_=tokio::time::sleep(pause)=>() }
    }
}

/// Printer status monitor. Runs after every other task has had its say: a printer that cannot be
/// probed reports `unknown`, it never stops the monitor for the printers behind it, and it can
/// never take the process down.
pub async fn monitor(s: Arc<State>) {
    use futures_util::FutureExt;
    if let Err(panic) = std::panic::AssertUnwindSafe(monitor_loop(s)).catch_unwind().await {
        tracing::error!(
            target: "printer",
            event = "PRINTER_MONITOR_PANIC",
            panic = %panic_detail(&panic),
            backtrace = %std::backtrace::Backtrace::capture(),
            "printer status monitor stopped unexpectedly"
        );
    }
}

async fn monitor_loop(s: Arc<State>) {
    while !s.stopping.load(Ordering::SeqCst) {
        let state = s.clone();
        let _ = tokio::task::spawn_blocking(move || {
            let Ok(c) = state.config() else {
                return;
            };
            for p in c.printers {
                if state.stopping.load(Ordering::SeqCst) {
                    break;
                }
                if lock(&state.active, "active").as_deref() == Some(&p.id) {
                    continue;
                }
                // `Transport::status` never propagates an error: an unreachable or wedged printer
                // yields a status string and the loop moves on to the next one.
                let status = state.transport.status(&p);
                let old = lock(&state.statuses, "statuses").insert(p.id.clone(), status.clone());
                if old.as_deref() != Some(&status) {
                    tracing::info!(
                        target: "printer",
                        event = "PRINTER_STATUS_CHANGED",
                        printer_id = %p.id,
                        connection = p.connection.kind(),
                        status = %status,
                        previous = old.as_deref().unwrap_or(""),
                        "printer status changed"
                    );
                    state.event(
                        json!({"type":"printer.status","version":1,"printerId":p.id,"status":status})
                    );
                }
            }
        }).await;
        tokio::time::sleep(Duration::from_secs(10)).await;
    }
}

#[cfg(test)]
mod test_print_tests {
    use super::*;
    use crate::{print::layout::{self, Align, Item}, printers::Connection};

    struct TestTransport;
    impl Transport for TestTransport {
        fn send(&self, _: &Printer, _: &[u8]) -> crate::error::Result<()> {
            Ok(())
        }
        fn status(&self, _: &Printer) -> String {
            "unknown".into()
        }
    }

    fn test_profile(id: &str) -> Printer {
        Printer {
            id: id.into(),
            name: "Test printer".into(),
            connection: Connection::Network { host: "192.168.1.50".into(), port: 9100 },
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

    #[test]
    fn test_ticket_identifies_configured_paper_and_exercises_receipt_layout() {
        let lines = test_receipt_lines(58, 384);
        assert!(lines.iter().any(|line| line == "Paper profile | 58 mm | 384 dots"));
        let items = layout::plan(&Document::Receipt { lines }, 24, 384);
        assert!(matches!(&items[0], Item::Text(line) if line.align == Align::Center));
        assert!(items.iter().any(|item| matches!(item, Item::Cells { .. })));
        assert!(items.iter().any(|item| matches!(item, Item::Rule)));
    }

    #[test]
    fn printer_save_normalizes_compat_descriptor_but_api_lists_spooler_shape() {
        let directory = tempfile::tempdir().unwrap();
        let store = Storage::open(&directory.path().join("agent.sqlite3")).unwrap();
        let state = State::new(store, crate::security::test_secret(), Arc::new(TestTransport));
        let printer = Printer {
            id: "printer:queue".into(),
            name: "Kitchen POS".into(),
            connection: Connection::Usb {
                vendor_id: 0,
                product_id: 0,
                serial: Some("queue:Kitchen POS".into()),
                bus: 0,
                ports: vec![],
                interface: 0,
                endpoint: 0,
                alternate: 0,
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
        };

        let saved = state.dispatch(Command::PrinterSave { printer }).unwrap();
        assert_eq!(saved["connection"]["serial"], "queue:Kitchen POS");
        assert_eq!(saved["connection"]["vendorId"], 0);
        let saved_config = state.config().unwrap();
        assert!(matches!(&saved_config.printers[0].connection,
            Connection::Spooler { queue_name } if queue_name == "Kitchen POS"));
        let listed = state.dispatch(Command::PrintersList).unwrap();
        assert_eq!(listed[0]["connection"]["type"], "usb");
    }

    #[test]
    fn remote_printer_save_cannot_change_local_usb_fallback_target() {
        let directory = tempfile::tempdir().unwrap();
        let store = Storage::open(&directory.path().join("agent.sqlite3")).unwrap();
        let state = State::new(store, crate::security::test_secret(), Arc::new(TestTransport));
        let configured_target = Connection::Spooler { queue_name: "Receipt-RAW".into() };
        let usb_connection = Connection::Usb {
            vendor_id: 1,
            product_id: 2,
            serial: None,
            bus: 1,
            ports: vec![1],
            interface: 0,
            endpoint: 1,
            alternate: 0,
        };
        let mut local = test_profile("printer:usb-fallback");
        local.connection = usb_connection.clone();
        local.usb_fallback_target = Some(configured_target.clone());
        let mut config = state.config().unwrap();
        config.printers.push(local);
        config.raw_passthrough.printers.insert("printer:usb-fallback".into(), crate::config::RawPrinterSettings {
            raw_target: Some("Local Receipt RAW".into()),
            force_raw: true,
            created_generic: true,
        });
        lock(&state.store, "store").save_config(&config).unwrap();

        let mut remote = test_profile("printer:usb-fallback");
        remote.connection = usb_connection;
        remote.raw_passthrough = true;
        remote.force_raw = true;
        remote.usb_fallback_target = Some(Connection::Network {
            host: "192.168.1.77".into(),
            port: 9100,
        });
        state.dispatch(Command::PrinterSave { printer: remote.clone() }).unwrap();
        let saved = state.config().unwrap();
        assert_eq!(saved.printers[0].usb_fallback_target, Some(configured_target));
        assert!(!saved.printers[0].raw_passthrough);
        assert!(!saved.printers[0].force_raw);
        let raw = saved.raw_passthrough.printers.get("printer:usb-fallback").unwrap();
        assert_eq!(raw.raw_target.as_deref(), Some("Local Receipt RAW"));
        assert!(raw.force_raw);
        assert!(raw.created_generic);

        remote.connection = Connection::Network { host: "192.168.1.50".into(), port: 9100 };
        remote.usb_fallback_target = Some(Connection::Spooler { queue_name: "UNTRUSTED".into() });
        state.dispatch(Command::PrinterSave { printer: remote }).unwrap();
        assert_eq!(state.config().unwrap().printers[0].usb_fallback_target, None);
    }

    /// A remote `printer.save` must never be able to switch on raw ESC/POS passthrough: the flag
    /// is operator-owned and lives in the local desktop window only.
    #[test]
    fn remote_printer_save_cannot_enable_raw_passthrough() {
        let directory = tempfile::tempdir().unwrap();
        let store = Storage::open(&directory.path().join("agent.sqlite3")).unwrap();
        let state = State::new(store, crate::security::test_secret(), Arc::new(TestTransport));
        let printer = Printer {
            id: "printer:lan".into(),
            name: "Bar".into(),
            connection: Connection::Network { host: "192.168.1.50".into(), port: 9100 },
            paper_mm: 80,
            width_dots: 576,
            copies: 1,
            cut: true,
            font_family: "Noto Sans Arabic".into(),
            font_size: 24,
            raw_passthrough: true,
            force_raw: false,
            usb_fallback_target: None,
        };
        state.dispatch(Command::PrinterSave { printer }).unwrap();
        assert!(!state.config().unwrap().printers[0].raw_passthrough);
        // Pin the global gate off so this scenario is independent of the zero-touch default.
        let mut config = state.config().unwrap();
        config.raw_passthrough.enabled = false;
        lock(&state.store, "store").save_config(&config).unwrap();
        let escpos = Document::Escpos { commands: vec![27, 64], data: String::new() };
        let error = state
            .dispatch(Command::Print {
                printer_id: "printer:lan".into(),
                job_id: "order:1:raw".into(),
                document: escpos,
            })
            .expect_err("raw passthrough must be refused");
        assert_eq!(error.code, "RAW_PASSTHROUGH_DISABLED");
        assert_eq!(error.printer.as_deref(), Some("Bar"));
        assert!(error.action_required.as_deref().is_some_and(|s| s.contains("global")));
    }

    /// The operator can enable it locally, and only then does the agent forward the bytes
    /// verbatim — with no rendering and no layout of its own.
    #[test]
    fn local_config_can_enable_raw_passthrough() {
        let directory = tempfile::tempdir().unwrap();
        let store = Storage::open(&directory.path().join("agent.sqlite3")).unwrap();
        let state = State::new(store, crate::security::test_secret(), Arc::new(TestTransport));
        let printer = Printer {
            id: "printer:lan".into(),
            name: "Bar".into(),
            connection: Connection::Network { host: "192.168.1.50".into(), port: 9100 },
            paper_mm: 80,
            width_dots: 576,
            copies: 1,
            cut: true,
            font_family: "Noto Sans Arabic".into(),
            font_size: 24,
            raw_passthrough: false,
            force_raw: false,
            usb_fallback_target: None,
        };
        state.dispatch(Command::PrinterSave { printer }).unwrap();
        let mut config = state.config().unwrap();
        config.raw_passthrough.enabled = true;
        config.printers[0].raw_passthrough = true;
        config.raw_passthrough.printers.insert("printer:lan".into(), crate::config::RawPrinterSettings {
            raw_target: Some("Test Receipt RAW".into()),
            force_raw: true,
            created_generic: true,
        });
        lock(&state.store, "store").save_config(&config).unwrap();

        state
            .dispatch(Command::Print {
                printer_id: "printer:lan".into(),
                job_id: "order:1:raw".into(),
                document: Document::Escpos { commands: vec![27, 64], data: String::new() },
            })
            .expect("enabled passthrough must be accepted");
        let job = lock(&state.store, "store").job("order:1:raw").unwrap();
        assert_eq!(job.status, "queued");
        let work = lock(&state.store, "store").claim(crate::print::queue::now() + 1).unwrap().unwrap();
        assert!(matches!(work.printer.connection,
            Connection::Spooler { ref queue_name } if queue_name == "Test Receipt RAW"));
        assert!(work.printer.force_raw);
        assert!(config.raw_passthrough.printers["printer:lan"].created_generic);
    }

    /// Zero-touch default: a raw ESC/POS document for a printer with no saved raw entry
    /// prints with the RAW spooler datatype (force_raw defaults to true), so vendor
    /// drivers cannot reinterpret the receipt. The fresh global gate is ON by default,
    /// and rendered documents are unaffected (force_raw stays false for them).
    #[test]
    fn raw_document_without_saved_entry_uses_force_raw_by_default() {
        let directory = tempfile::tempdir().unwrap();
        let store = Storage::open(&directory.path().join("agent.sqlite3")).unwrap();
        let state = State::new(store, crate::security::test_secret(), Arc::new(TestTransport));
        let printer = Printer {
            id: "printer:lan".into(),
            name: "POS".into(),
            connection: Connection::Network { host: "192.168.1.50".into(), port: 9100 },
            paper_mm: 80,
            width_dots: 576,
            copies: 1,
            cut: true,
            font_family: "Noto Sans Arabic".into(),
            font_size: 24,
            raw_passthrough: true,
            force_raw: false,
            usb_fallback_target: None,
        };
        let mut config = state.config().unwrap();
        config.printers.push(printer);
        lock(&state.store, "store").save_config(&config).unwrap();
        state
            .dispatch(Command::Print {
                printer_id: "printer:lan".into(),
                job_id: "order:1:raw".into(),
                document: Document::Escpos { commands: vec![27, 64], data: String::new() },
            })
            .expect("enabled passthrough must be accepted with the default gates");
        let work = lock(&state.store, "store").claim(crate::print::queue::now() + 1).unwrap().unwrap();
        assert!(work.printer.force_raw, "force_raw defaults to true without a saved entry");
    }

    #[test]
    fn locally_configured_raw_size_limit_returns_structured_error() {
        let directory = tempfile::tempdir().unwrap();
        let store = Storage::open(&directory.path().join("agent.sqlite3")).unwrap();
        let state = State::new(store, crate::security::test_secret(), Arc::new(TestTransport));
        let mut config = state.config().unwrap();
        let mut printer = test_profile("printer:limited-raw");
        printer.raw_passthrough = true;
        config.raw_passthrough.enabled = true;
        config.raw_passthrough.max_bytes = 1024;
        config.printers.push(printer);
        lock(&state.store, "store").save_config(&config).unwrap();
        let mut commands = vec![0u8; 1025];
        commands[..2].copy_from_slice(&[0x1b, 0x40]);
        let error = state.dispatch(Command::Print {
            printer_id: "printer:limited-raw".into(),
            job_id: "order:raw:oversized".into(),
            document: Document::Escpos { commands, data: String::new() },
        }).unwrap_err();
        assert_eq!(error.code, "RAW_PAYLOAD_TOO_LARGE");
        assert_eq!(error.printer.as_deref(), Some("Test printer"));
        assert!(error.action_required.is_some());
        assert!(lock(&state.store, "store").job("order:raw:oversized").is_err());
    }

    /// A poisoned mutex must be recovered, not turned into a panic that takes the agent down.
    #[test]
    fn poisoned_lock_is_recovered_instead_of_panicking() {
        let mutex = Mutex::new(7u32);
        let shared = &mutex;
        let panicked = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
            let _guard = shared.lock().unwrap();
            panic!("simulated failure inside a guarded section");
        }));
        assert!(panicked.is_err());
        assert!(mutex.is_poisoned(), "the fixture must actually be poisoned");
        assert_eq!(*lock(&mutex, "unit_test"), 7);
    }
}
