use std::{
    sync::{ Arc, Mutex, atomic::{ AtomicBool, AtomicU64, Ordering } },
    collections::HashMap,
};
use serde_json::{ json, Value };
use tokio::sync::{ broadcast, Notify };
use crate::{
    config::{ Config, storage::Storage },
    error::{ AgentError, Result },
    print::{ Renderer, queue::{ now, deliver } },
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
        self.store
            .lock()
            .map_err(|_| lock_error())?
            .config()
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
    pub fn job_event(&self, j: &crate::print::queue::Job) {
        self.event(json!({"type":format!("print.{}",j.status),"version":1,"job":j}));
    }
    pub fn printer_views(&self) -> Result<Value> {
        let statuses = self.statuses.lock().map_err(|_| lock_error())?;
        let active = self.active.lock().map_err(|_| lock_error())?;
        Ok(
            json!(
                self
                    .config()?
                    .printers.iter()
                    .map(|p| {
                        let mut v = serde_json::to_value(p).unwrap_or(Value::Null);
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
                    json!({"ready":self.ready.load(Ordering::SeqCst),"agentVersion":env!("CARGO_PKG_VERSION"),"port":c.port,"routes":c.routes,"serverError":*self.server_error.lock().map_err(|_|lock_error())?})
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
            Command::PrinterTest { printer_id, job_id } =>
                self.enqueue(
                    &job_id,
                    &printer_id,
                    &(Document::Receipt {
                        lines: vec![
                            "MenuVex Printer Agent".into(),
                            "آزمون چاپ فارسی — سلام دنیا".into(),
                            "0123456789 / ۱۲۳۴۵۶۷۸۹۰".into(),
                            "Test print / USB · LAN / ESC-POS".into()
                        ],
                    })
                ),
            Command::PrintStatus { job_id } =>
                Ok(
                    json!(
                        self.store
                            .lock()
                            .map_err(|_| lock_error())?
                            .job(&job_id)?
                    )
                ),
            Command::QueueList =>
                Ok(
                    json!(
                        self.store
                            .lock()
                            .map_err(|_| lock_error())?
                            .queue()?
                    )
                ),
            Command::QueueCancel { job_id } => {
                let j = self.store
                    .lock()
                    .map_err(|_| lock_error())?
                    .cancel(&job_id)?;
                self.job_event(&j);
                Ok(json!(j))
            }
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
    fn enqueue(&self, id: &str, printer_id: &str, document: &Document) -> Result<Value> {
        if self.worker_done.load(Ordering::SeqCst) {
            return Err(
                AgentError::new(
                    "AGENT_NOT_READY",
                    "Queue worker is stopped; restart and reconcile pending jobs"
                )
            );
        }
        let p = self.printer(printer_id)?;
        let job = self.store
            .lock()
            .map_err(|_| lock_error())?
            .enqueue(id, &p, document)?;
        self.job_event(&job);
        self.wake.notify_one();
        Ok(json!(job))
    }
}
fn lock_error() -> AgentError {
    AgentError::new("AGENT_NOT_READY", "Internal state unavailable; restart agent")
}
pub async fn worker(s: Arc<State>) {
    let renderer = Arc::new(Mutex::new(Renderer::default()));
    while !s.stopping.load(Ordering::SeqCst) {
        let state = s.clone();
        let renderer = renderer.clone();
        let result = tokio::task::spawn_blocking(
            move || -> Result<bool> {
                let (work, max) = {
                    let mut store = state.store.lock().map_err(|_| lock_error())?;
                    let max = store.config()?.max_attempts;
                    (store.claim(now())?, max)
                };
                let Some(w) = work else {
                    return Ok(false);
                };
                *state.active.lock().map_err(|_| lock_error())? = Some(w.printer.id.clone());
                state.job_event(&w.job);
                let result = renderer
                    .lock()
                    .map_err(|_| lock_error())?
                    .encode(&w.printer, &w.document)
                    .and_then(|bytes| deliver(state.transport.as_ref(), &w.printer, &bytes));
                *state.active.lock().map_err(|_| lock_error())? = None;
                let job = state.store
                    .lock()
                    .map_err(|_| lock_error())?
                    .finish(&w.job.job_id, result, max, now())?;
                state.job_event(&job);
                Ok(true)
            }
        ).await;
        match result {
            Ok(Ok(true)) => {
                continue;
            }
            Ok(Ok(false)) => (),
            _ => {
                tracing::error!("queue worker stopped after internal failure");
                s.ready.store(false, Ordering::SeqCst);
                *s.server_error.lock().unwrap() = Some(
                    "QUEUE_ERROR: restart and inspect queue".into()
                );
                break;
            }
        }
        tokio::select! { _=s.wake.notified()=>(),_=tokio::time::sleep(std::time::Duration::from_secs(1))=>() }
    }
    s.worker_done.store(true, Ordering::SeqCst);
}
pub async fn monitor(s: Arc<State>) {
    while !s.stopping.load(Ordering::SeqCst) {
        let state = s.clone();
        let _ = tokio::task::spawn_blocking(move || {
            if let Ok(c) = state.config() {
                for p in c.printers {
                    if state.stopping.load(Ordering::SeqCst) {
                        break;
                    }
                    if state.active.lock().unwrap().as_deref() == Some(&p.id) {
                        continue;
                    }
                    let status = state.transport.status(&p);
                    let old = state.statuses.lock().unwrap().insert(p.id.clone(), status.clone());
                    if old.as_deref() != Some(&status) {
                        state.event(
                            json!({"type":"printer.status","version":1,"printerId":p.id,"status":status})
                        );
                    }
                }
            }
        }).await;
        tokio::time::sleep(std::time::Duration::from_secs(10)).await;
    }
}
