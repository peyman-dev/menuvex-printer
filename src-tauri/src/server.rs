use std::{ sync::{ Arc, atomic::Ordering }, time::Duration };
use futures_util::{ SinkExt, StreamExt };
use tokio::{ net::TcpListener, sync::Semaphore, time::timeout };
use tokio_tungstenite::{
    accept_hdr_async_with_config,
    tungstenite::{
        Message,
        protocol::WebSocketConfig,
        handshake::server::{ Request as Upgrade, Response, ErrorResponse },
        http::StatusCode,
    },
};
use serde_json::json;
use crate::{
    error::{ AgentError, Result },
    state::{ lock, State },
    protocol::{ self, Command },
    security::{ origin_allowed, random_secret },
};
/// The hardware bridge MUST stay loopback-only; unrelated Vite preview binding is not this listener.
pub async fn run(state: Arc<State>) {
    let port = match state.config() {
        Ok(c) => c.port,
        Err(e) => {
            tracing::error!(
                target: "server",
                event = "SERVER_CONFIG_ERROR",
                error_code = %e.code,
                error_message = %e.message,
                "loopback listener not started; the agent window still works"
            );
            *lock(&state.server_error, "server_error") =
                Some(format!("CONFIG_ERROR: {}", e.message));
            return;
        }
    };
    let listener = match TcpListener::bind((std::net::Ipv4Addr::LOCALHOST, port)).await {
        Ok(l) => l,
        Err(e) => {
            tracing::error!(
                target: "server",
                event = "SERVER_BIND_FAILED",
                port,
                error = %e,
                "the loopback port is unavailable"
            );
            *lock(&state.server_error, "server_error") = Some(
                format!(
                    "PORT_IN_USE_OR_UNAVAILABLE: 127.0.0.1:{port}; choose another port and restart"
                )
            );
            state.ready.store(false, Ordering::SeqCst);
            return;
        }
    };
    state.ready.store(true, Ordering::SeqCst);
    tracing::info!(port, "loopback WebSocket ready");
    let limit = Arc::new(Semaphore::new(8));
    loop {
        if state.stopping.load(Ordering::SeqCst) {
            break;
        }
        tokio::select! {
   accepted=listener.accept()=>{let Ok((stream,peer))=accepted else{continue};if !peer.ip().is_loopback(){continue;}let Ok(permit)=limit.clone().try_acquire_owned() else{continue};let s=state.clone();tokio::spawn(async move{let _permit=permit;if let Err(e)=session(stream,s,port).await{tracing::debug!(target:"server",event="SESSION_CLOSED",error=%e,"WebSocket session ended");}});},
   _=tokio::time::sleep(Duration::from_secs(1))=>()
  }
    }
    state.ready.store(false, Ordering::SeqCst);
}
async fn session(
    stream: tokio::net::TcpStream,
    state: Arc<State>,
    port: u16
) -> std::result::Result<(), Box<dyn std::error::Error + Send + Sync>> {
    let mut origin = String::new();
    let mut config = WebSocketConfig::default();
    config.max_message_size = Some(protocol::MAX_MESSAGE);
    config.max_frame_size = Some(protocol::MAX_MESSAGE);
    config.max_write_buffer_size = 512 * 1024;
    let mut ws = timeout(
        Duration::from_secs(5),
        accept_hdr_async_with_config(
            stream,
            |req: &Upgrade, response: Response| {
                let o = req
                    .headers()
                    .get("origin")
                    .and_then(|v| v.to_str().ok())
                    .unwrap_or("");
                let host = req
                    .headers()
                    .get("host")
                    .and_then(|v| v.to_str().ok())
                    .unwrap_or("");
                if
                    !origin_allowed(o) ||
                    host != format!("127.0.0.1:{port}") ||
                    req.uri().path() != "/" ||
                    req.uri().query().is_some()
                {
                    let mut denied = ErrorResponse::new(Some("Forbidden".into()));
                    *denied.status_mut() = StatusCode::FORBIDDEN;
                    return Err(denied);
                }
                origin = o.to_owned();
                Ok(response)
            },
            Some(config)
        )
    ).await??;
    let nonce = random_secret();
    let server_proof = lock(&state.secret, "secret").server_proof(&nonce, &origin);
    let epoch = state.epoch.load(Ordering::SeqCst);
    timeout(
        Duration::from_secs(5),
        ws.send(
            Message::Text(
                json!({"type":"hello","version":1,"agentVersion":env!("CARGO_PKG_VERSION"),"nonce":nonce,"authentication":"hmac-sha256","serverProof":server_proof})
                    .to_string()
                    .into()
            )
        )
    ).await??;
    let Some(Ok(Message::Text(text))) = timeout(Duration::from_secs(10), ws.next()).await? else {
        return Ok(());
    };
    let request = protocol::parse(&text)?;
    let valid = match request.command {
        Command::Authenticate { proof } =>
            lock(&state.secret, "secret").verify(&nonce, &origin, &proof),
        _ => false,
    };
    if !valid {
        timeout(
            Duration::from_secs(3),
            ws.send(
                Message::Text(
                    json!({"type":"error","version":1,"requestId":request.request_id,"error":{"code":"AUTH_FAILED","message":"Pair this browser using the agent window","retryable":false,"uncertain":false}})
                        .to_string()
                        .into()
                )
            )
        ).await??;
        ws.close(None).await?;
        return Ok(());
    }
    let mut events = state.events.subscribe();
    timeout(
        Duration::from_secs(5),
        ws.send(
            Message::Text(
                json!({"type":"authenticated","version":1,"requestId":request.request_id,"agentVersion":env!("CARGO_PKG_VERSION")})
                    .to_string()
                    .into()
            )
        )
    ).await??;
    let mut window = tokio::time::Instant::now();
    let mut commands = 0u32;
    let mut last = tokio::time::Instant::now();
    let mut heartbeat = tokio::time::interval(Duration::from_secs(15));
    loop {
        if state.stopping.load(Ordering::SeqCst) || state.epoch.load(Ordering::SeqCst) != epoch {
            break;
        }
        let response =
            tokio::select! {
   frame=ws.next()=>{
    let Some(frame)=frame else{break};last=tokio::time::Instant::now();
    match frame?{
     Message::Text(text)=>{
      if window.elapsed()>Duration::from_secs(1){window=tokio::time::Instant::now();commands=0;}commands+=1;if commands>30{break;}
      let req=match protocol::parse(&text){Ok(r)=>r,Err(e)=>{timeout(Duration::from_secs(5),ws.send(Message::Text(json!({"type":"error","version":1,"error":e}).to_string().into()))).await??;break;}};
      let s=state.clone();let result=dispatch_isolated(s,req.command).await;
      Some(match result{Ok(data)=>json!({"type":"response","version":1,"requestId":req.request_id,"data":data}),Err(e)=>json!({"type":"error","version":1,"requestId":req.request_id,"error":e})})
     },
     Message::Close(_)=>break,
     Message::Ping(data)=>{timeout(Duration::from_secs(5),ws.send(Message::Pong(data))).await??;None},
     Message::Pong(_)=>None,
     _=>break,
    }
   },
   event=events.recv()=>match event{Ok(v)=>Some(v),Err(tokio::sync::broadcast::error::RecvError::Lagged(_))=>Some(json!({"type":"resync","version":1})),Err(_)=>break},
   _=heartbeat.tick()=>{if last.elapsed()>Duration::from_secs(60){break;}timeout(Duration::from_secs(5),ws.send(Message::Ping(Vec::new().into()))).await??;None}
  };
        if let Some(value) = response {
            timeout(
                Duration::from_secs(5),
                ws.send(Message::Text(value.to_string().into()))
            ).await??;
        }
    }
    let _ = timeout(Duration::from_secs(2), ws.close(None)).await;
    Ok(())
}
/// Run one command with the printer/queue failure domain fully isolated from this socket.
///
/// Both failure modes used to end the session: a `?` on the `JoinError` closed the website's
/// WebSocket when a handler panicked, and the panic itself was invisible because nothing
/// installed a hook. Now a panicking or failing command answers with an `error` frame and the
/// connection — and every other printer — keeps working.
async fn dispatch_isolated(state: Arc<State>, command: Command) -> Result<serde_json::Value> {
    let outcome = tokio::task
        ::spawn_blocking(move || {
            match
                std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| state.dispatch(command)))
            {
                Ok(result) => result,
                Err(_) => {
                    tracing::error!(
                        target: "server",
                        event = "COMMAND_PANIC",
                        backtrace = %std::backtrace::Backtrace::capture(),
                        "a command handler panicked; only this request failed"
                    );
                    Err(AgentError::internal("command handler panicked"))
                }
            }
        })
        .await;
    outcome.unwrap_or_else(|join| {
        tracing::error!(
            target: "server",
            event = "COMMAND_TASK_FAILED",
            task = %join,
            "the blocking command task did not return a result"
        );
        Err(AgentError::internal("command task did not complete"))
    })
}

#[cfg(test)]
mod integration {
    use super::*;
    use crate::{
        config::{ Config, storage::Storage },
        error::Result,
        printers::{ Printer, Connection, Transport },
    };
    use std::sync::atomic::AtomicUsize;
    struct TestTransport(Arc<AtomicUsize>);
    impl Transport for TestTransport {
        fn send(&self, _: &Printer, bytes: &[u8]) -> Result<()> {
            assert!(bytes.len() > 10);
            assert_eq!(&bytes[..2], &[27, 64]);
            self.0.fetch_add(1, Ordering::SeqCst);
            Ok(())
        }
        fn status(&self, _: &Printer) -> String {
            "online".into()
        }
    }
    #[tokio::test(flavor = "multi_thread", worker_threads = 2)]
    async fn sdk_to_real_agent() {
        let port = std::net::TcpListener::bind("127.0.0.1:0").unwrap().local_addr().unwrap().port();
        let dir = tempfile::tempdir().unwrap();
        let store = Storage::open(&dir.path().join("test.sqlite3")).unwrap();
        let mut config = Config::default();
        config.port = port;
        config.printers.push(Printer {
            id: "integration-printer".into(),
            name: "Test transport".into(),
            connection: Connection::Network { host: "192.168.1.50".into(), port: 9100 },
            paper_mm: 80,
            width_dots: 576,
            copies: 1,
            cut: true,
            font_family: "Noto Sans Arabic".into(),
            font_size: 24,
            raw_passthrough: false,
        });
        store.save_config(&config).unwrap();
        let count = Arc::new(AtomicUsize::new(0));
        let secret = crate::security::test_secret();
        let encoded = secret.reveal();
        let state = State::new(store, secret, Arc::new(TestTransport(count.clone())));
        let server = tokio::spawn(run(state.clone()));
        let worker = tokio::spawn(crate::state::worker(state.clone()));
        for _ in 0..100 {
            if state.ready.load(Ordering::SeqCst) {
                break;
            }
            tokio::time::sleep(Duration::from_millis(20)).await;
        }
        assert!(state.ready.load(Ordering::SeqCst), "test listener failed");
        let result = tokio::task
            ::spawn_blocking(move || {
                let mut command = if cfg!(windows) {
                    let mut c = std::process::Command::new("cmd");
                    c.args(["/c", "npm", "exec", "--", "vitest", "run", "sdk/tests/live.test.ts"]);
                    c
                } else {
                    let mut c = std::process::Command::new("npm");
                    c.args(["exec", "--", "vitest", "run", "sdk/tests/live.test.ts"]);
                    c
                };
                command
                    .current_dir(std::path::Path::new(env!("CARGO_MANIFEST_DIR")).parent().unwrap())
                    .env("MENUVEX_TEST_PORT", port.to_string())
                    .env("MENUVEX_TEST_SECRET", encoded)
                    .output()
                    .expect("npm ci must run before Rust integration tests")
            }).await
            .unwrap();
        state.stopping.store(true, Ordering::SeqCst);
        state.wake.notify_one();
        worker.await.unwrap();
        server.await.unwrap();
        assert!(
            result.status.success(),
            "SDK integration failed: {} {}",
            String::from_utf8_lossy(&result.stdout),
            String::from_utf8_lossy(&result.stderr)
        );
        assert_eq!(count.load(Ordering::SeqCst), 1);
    }
}
