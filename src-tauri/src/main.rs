#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]
use std::sync::{ Arc, atomic::Ordering };
use tauri::{ Manager, Emitter, menu::{ Menu, MenuItem }, tray::TrayIconBuilder };
use tauri_plugin_autostart::ManagerExt;
use menuvex_agent::{
    state::{ self, lock, State },
    config::{ Config, storage::Storage },
    security::Secret,
    printers::{ self, HardwareTransport },
    protocol,
    error::{ AgentError, Result },
};
#[tauri::command]
async fn local_command(
    state: tauri::State<'_, Arc<State>>,
    request: String
) -> Result<serde_json::Value> {
    let s = state.inner().clone();
    let req = protocol::parse(&request)?;
    // Same isolation as the WebSocket path: a panicking handler fails one request, it never
    // takes the desktop window or the loopback listener with it.
    tauri::async_runtime
        ::spawn_blocking(move || {
            match std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| s.dispatch(req.command)))
            {
                Ok(result) => result,
                Err(_) => {
                    tracing::error!(
                        target: "desktop",
                        event = "COMMAND_PANIC",
                        backtrace = %std::backtrace::Backtrace::capture(),
                        "a local command handler panicked; only this request failed"
                    );
                    Err(AgentError::internal("command handler panicked"))
                }
            }
        })
        .await
        .unwrap_or_else(|_| Err(AgentError::internal("command task did not complete")))
}
/// Desktop-side bound for USB discovery. `printers::discovery::discover` already bounds itself;
/// this is the outer guard so the window's “جستجوی USB” button can never spin forever.
const DISCOVER_UI_TIMEOUT: std::time::Duration = std::time::Duration::from_secs(40);
#[tauri::command]
async fn discover_usb() -> Result<Vec<printers::discovery::DiscoveredUsb>> {
    let found = tokio::time::timeout(
        DISCOVER_UI_TIMEOUT,
        tauri::async_runtime::spawn_blocking(printers::discovery::discover)
    ).await;
    match found {
        Ok(Ok(result)) => result,
        Ok(Err(_)) => {
            tracing::error!(target: "usb", event = "PRINTER_DISCOVERY_FAILED", "discovery task did not return a result");
            Err(AgentError::new("USB_DEVICE_ERROR", "USB discovery failed; unplug the printer and retry"))
        }
        Err(_) => {
            tracing::error!(
                target: "usb",
                event = "PRINTER_DISCOVERY_TIMEOUT",
                limit_ms = DISCOVER_UI_TIMEOUT.as_millis() as u64,
                "USB discovery exceeded its deadline"
            );
            Err(
                AgentError::new(
                    "USB_TIMEOUT",
                    "USB discovery timed out; a device is not answering. Unplug it and retry."
                )
            )
        }
    }
}
#[tauri::command]
fn get_config(state: tauri::State<'_, Arc<State>>) -> Result<Config> {
    state.config()
}
#[tauri::command]
fn save_config(
    app: tauri::AppHandle,
    state: tauri::State<'_, Arc<State>>,
    config: serde_json::Value
) -> Result<()> {
    // Name the offending `connection.type` instead of serde's anonymous payload error.
    if let Some(printers) = config.get("printers").and_then(|p| p.as_array()) {
        for printer in printers {
            if let Some(connection) = printer.get("connection") {
                printers::check_connection_tag(connection)?;
            }
        }
    }
    let config: Config = serde_json::from_value(config)?;
    config.validate()?;
    (if config.autostart { app.autolaunch().enable() } else { app.autolaunch().disable() }).map_err(
        |_| AgentError::new("AUTOSTART_ERROR", "OS login startup could not be changed")
    )?;
    lock(&state.store, "store").save_config(&config)?;
    state.event(serde_json::json!({"type":"resync","version":1}));
    tracing::info!(
        target: "desktop",
        event = "CONFIG_SAVED",
        printers = config.printers.len(),
        raw_passthrough_printers = config
            .printers
            .iter()
            .filter(|p| p.raw_passthrough)
            .count(),
        "local configuration saved"
    );
    Ok(())
}
#[tauri::command]
fn pairing_secret(state: tauri::State<'_, Arc<State>>) -> String {
    lock(&state.secret, "secret").reveal()
}
#[tauri::command]
fn rotate_secret(state: tauri::State<'_, Arc<State>>) -> Result<()> {
    lock(&state.secret, "secret").rotate()?;
    state.epoch.fetch_add(1, Ordering::SeqCst);
    Ok(())
}
#[tauri::command]
async fn test_connection(
    state: tauri::State<'_, Arc<State>>,
    printer_id: String
) -> Result<String> {
    let p = state.printer(&printer_id)?;
    let s = state.inner().clone();
    tauri::async_runtime
        ::spawn_blocking(move || s.transport.status(&p)).await
        .map_err(|_| AgentError::new("AGENT_NOT_READY", "Probe failed"))
}
/// Log when a long-lived background task stops. Stopping is normal on quit; anything else has to
/// leave a line in the daily log so the failure is diagnosable from the field.
async fn log_task_end(name: &'static str, task: impl std::future::Future<Output = ()>) {
    task.await;
    tracing::warn!(target: "lifecycle", event = "TASK_STOPPED", task = name, "background task stopped");
}
fn show(app: &tauri::AppHandle, tab: &str) {
    if let Some(w) = app.get_webview_window("main") {
        let _ = w.show();
        let _ = w.set_focus();
        let _ = w.emit("navigate", tab);
    }
}
fn stop(app: tauri::AppHandle, restart: bool) {
    let state = app.state::<Arc<State>>().inner().clone();
    if state.stopping.swap(true, Ordering::SeqCst) {
        return;
    }
    state.wake.notify_one();
    tauri::async_runtime::spawn(async move {
        while !state.worker_done.load(Ordering::SeqCst) {
            tokio::time::sleep(std::time::Duration::from_millis(100)).await;
        }
        if restart {
            app.restart();
        } else {
            app.exit(0);
        }
    });
}
fn main() {
    // Backtraces in the daily log are the only way to diagnose a field failure; opt in unless
    // the operator already set the variable.
    if std::env::var_os("RUST_BACKTRACE").is_none() {
        std::env::set_var("RUST_BACKTRACE", "1");
    }
    // Without a hook a panic in a spawned task is silent: the queue worker or the status monitor
    // could die and the operator would only see "the agent stopped responding".
    let previous_hook = std::panic::take_hook();
    std::panic::set_hook(
        Box::new(move |info| {
            tracing::error!(
                target: "panic",
                event = "PANIC",
                location = info.location().map(|l| l.to_string()).unwrap_or_default(),
                payload = info
                    .payload()
                    .downcast_ref::<String>()
                    .cloned()
                    .or_else(|| info.payload().downcast_ref::<&'static str>().map(|s| (*s).to_owned()))
                    .unwrap_or_else(|| "non-string panic payload".to_owned()),
                backtrace = %std::backtrace::Backtrace::capture(),
                "panic caught; the affected task is isolated, the agent keeps running"
            );
            previous_hook(info);
        })
    );
    let result = tauri::Builder
        ::default()
        .plugin(tauri_plugin_single_instance::init(|app, _, _| show(app, "printers")))
        .plugin(
            tauri_plugin_autostart::init(
                tauri_plugin_autostart::MacosLauncher::LaunchAgent,
                Some(vec!["--background"])
            )
        )
        .invoke_handler(
            tauri::generate_handler![
                local_command,
                discover_usb,
                get_config,
                save_config,
                pairing_secret,
                rotate_secret,
                test_connection
            ]
        )
        .setup(|app| {
            #[cfg(unix)]
            if (unsafe { libc::geteuid() }) == 0 {
                return Err(
                    "Refusing to run the printer agent as root; use a normal desktop user".into()
                );
            }
            let data = app.path().app_data_dir()?;
            std::fs::create_dir_all(&data)?;
            #[cfg(unix)]
            {
                use std::os::unix::fs::PermissionsExt;
                std::fs::set_permissions(&data, std::fs::Permissions::from_mode(0o700))?;
            }
            let logs = app.path().app_log_dir()?;
            std::fs::create_dir_all(&logs)?;
            let file = tracing_appender::rolling::Builder
                ::new()
                .rotation(tracing_appender::rolling::Rotation::DAILY)
                .filename_prefix("agent")
                .max_log_files(7)
                .build(logs)?;
            tracing_subscriber
                ::fmt()
                .with_ansi(false)
                .with_max_level(tracing::Level::INFO)
                .with_writer(file)
                .init();
            let store = Storage::open(&data.join("agent.sqlite3"))?;
            let config = store.config()?;
            if config.autostart {
                if app.autolaunch().enable().is_err() {
                    tracing::warn!("autostart registration failed; use Settings to retry");
                }
            }
            let state = State::new(store, Secret::load()?, Arc::new(HardwareTransport));
            app.manage(state.clone());
            let menu = Menu::with_items(
                app,
                &[
                    &MenuItem::with_id(app, "open", "باز کردن منووکس", true, None::<&str>)?,
                    &MenuItem::with_id(app, "printers", "پرینترها", true, None::<&str>)?,
                    &MenuItem::with_id(app, "queue", "صف چاپ", true, None::<&str>)?,
                    &MenuItem::with_id(app, "settings", "تنظیمات", true, None::<&str>)?,
                    &MenuItem::with_id(app, "restart", "راه‌اندازی مجدد", true, None::<&str>)?,
                    &MenuItem::with_id(app, "quit", "خروج از برنامه", true, None::<&str>)?,
                ]
            )?;
            // Tray icon: the MenuVex printer logo, pre-rendered as raw 32x32 RGBA
            // (`icons/tray-32.rgba`, regenerated alongside the other icon sizes).
            let rgba = include_bytes!("../icons/tray-32.rgba").to_vec();
            TrayIconBuilder::new()
                .icon(tauri::image::Image::new_owned(rgba, 32, 32))
                .tooltip("منووکس پرینتر")
                .menu(&menu)
                .on_menu_event(|app, event| {
                    match event.id.as_ref() {
                        "quit" => stop(app.clone(), false),
                        "restart" => stop(app.clone(), true),
                        tab => show(app, tab),
                    }
                })
                .build(app)?;
            // Each background task is logged if it ever returns: a silently dead worker or
            // listener is what made the agent look "unresponsive" with nothing in the log.
            tauri::async_runtime::spawn(log_task_end("queue-worker", state::worker(state.clone())));
            tauri::async_runtime::spawn(log_task_end("printer-monitor", state::monitor(state.clone())));
            tauri::async_runtime::spawn(
                log_task_end("websocket-server", menuvex_agent::server::run(state.clone()))
            );
            let handle = app.handle().clone();
            tauri::async_runtime::spawn(async move {
                let mut events = state.events.subscribe();
                loop {
                    match events.recv().await {
                        Ok(value) => {
                            let _ = handle.emit("agent-event", value);
                        }
                        Err(tokio::sync::broadcast::error::RecvError::Lagged(_)) => {
                            let _ = handle.emit(
                                "agent-event",
                                serde_json::json!({"type":"resync","version":1})
                            );
                        }
                        Err(_) => {
                            break;
                        }
                    }
                }
            });
            if std::env::args().any(|s| s == "--background") {
                if let Some(w) = app.get_webview_window("main") {
                    w.hide()?;
                }
            }
            Ok(())
        })
        .on_window_event(|window, event| {
            if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                api.prevent_close();
                let _ = window.hide();
            }
        })
        .run(tauri::generate_context!());
    if let Err(e) = result {
        eprintln!("MenuVex startup failed: {e}");
        std::process::exit(1);
    }
}
