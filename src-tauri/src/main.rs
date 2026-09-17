#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]
use std::sync::{ Arc, atomic::Ordering };
use tauri::{ Manager, Emitter, menu::{ Menu, MenuItem }, tray::TrayIconBuilder };
use tauri_plugin_autostart::ManagerExt;
use menuvex_agent::{
    state::{ self, State },
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
    tauri::async_runtime
        ::spawn_blocking(move || s.dispatch(req.command)).await
        .map_err(|_| AgentError::new("AGENT_NOT_READY", "Task failed"))?
}
#[tauri::command]
async fn discover_usb() -> Result<Vec<printers::discovery::DiscoveredUsb>> {
    tauri::async_runtime
        ::spawn_blocking(printers::discovery::discover).await
        .map_err(|_| AgentError::new("USB_DEVICE_ERROR", "Discovery failed"))?
}
#[tauri::command]
fn get_config(state: tauri::State<'_, Arc<State>>) -> Result<Config> {
    state.config()
}
#[tauri::command]
fn save_config(
    app: tauri::AppHandle,
    state: tauri::State<'_, Arc<State>>,
    config: Config
) -> Result<()> {
    config.validate()?;
    (if config.autostart { app.autolaunch().enable() } else { app.autolaunch().disable() }).map_err(
        |_| AgentError::new("AUTOSTART_ERROR", "OS login startup could not be changed")
    )?;
    state.store.lock().unwrap().save_config(&config)?;
    state.event(serde_json::json!({"type":"resync","version":1}));
    Ok(())
}
#[tauri::command]
fn pairing_secret(state: tauri::State<'_, Arc<State>>) -> String {
    state.secret.lock().unwrap().reveal()
}
#[tauri::command]
fn rotate_secret(state: tauri::State<'_, Arc<State>>) -> Result<()> {
    state.secret.lock().unwrap().rotate()?;
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
                    &MenuItem::with_id(app, "open", "Open", true, None::<&str>)?,
                    &MenuItem::with_id(app, "printers", "Printers", true, None::<&str>)?,
                    &MenuItem::with_id(app, "queue", "Print Queue", true, None::<&str>)?,
                    &MenuItem::with_id(app, "settings", "Settings", true, None::<&str>)?,
                    &MenuItem::with_id(app, "restart", "Restart Agent", true, None::<&str>)?,
                    &MenuItem::with_id(app, "quit", "Quit", true, None::<&str>)?,
                ]
            )?;
            let mut rgba = vec![0u8;32*32*4];
            for y in 0..32 {
                for x in 0..32 {
                    let i = (y * 32 + x) * 4;
                    let on = (5..27).contains(&x) && (7..26).contains(&y);
                    rgba[i..i + 4].copy_from_slice(
                        if on {
                            &[30, 190, 145, 255]
                        } else {
                            &[0, 0, 0, 0]
                        }
                    );
                }
            }
            TrayIconBuilder::new()
                .icon(tauri::image::Image::new_owned(rgba, 32, 32))
                .tooltip("MenuVex Printer Agent")
                .menu(&menu)
                .on_menu_event(|app, event| {
                    match event.id.as_ref() {
                        "quit" => stop(app.clone(), false),
                        "restart" => stop(app.clone(), true),
                        tab => show(app, tab),
                    }
                })
                .build(app)?;
            tauri::async_runtime::spawn(state::worker(state.clone()));
            tauri::async_runtime::spawn(state::monitor(state.clone()));
            tauri::async_runtime::spawn(menuvex_agent::server::run(state.clone()));
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
