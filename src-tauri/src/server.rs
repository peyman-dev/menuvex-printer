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
    state::State,
    protocol::{ self, Command },
    security::{ origin_allowed, random_secret },
};
/// The hardware bridge MUST stay loopback-only; unrelated Vite preview binding is not this listener.
pub async fn run(state: Arc<State>) {
    let port = match state.config() {
        Ok(c) => c.port,
        Err(_) => {
            return;
        }
    };
    let listener = match TcpListener::bind((std::net::Ipv4Addr::LOCALHOST, port)).await {
        Ok(l) => l,
        Err(_) => {
            *state.server_error.lock().unwrap() = Some(
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
   accepted=listener.accept()=>{let Ok((stream,peer))=accepted else{continue};if !peer.ip().is_loopback(){continue;}let Ok(permit)=limit.clone().try_acquire_owned() else{continue};let s=state.clone();tokio::spawn(async move{let _permit=permit;let _=session(stream,s,port).await;});},
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
    let server_proof = state.secret.lock().unwrap().server_proof(&nonce, &origin);
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
            state.secret.lock().unwrap().verify(&nonce, &origin, &proof),
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
      let s=state.clone();let result=tokio::task::spawn_blocking(move||s.dispatch(req.command)).await?;
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
