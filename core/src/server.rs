//! The NDJSON service: one request per line on stdin, one response per
//! line on stdout, `id` echoed back. Diagnostics go to stderr — stdout is
//! protocol only, so a thread panic, a parse error, or a logged aside can
//! never corrupt it with something that isn't `{"id":...}` JSON.
//!
//! Requests are handled on their own thread so a slow discovery/download
//! never blocks an unrelated snapshot or local mutation; mutating methods
//! additionally take `write_lock` for their whole handler so writes to the
//! library and to agent files are fully serialized against each other.

use std::io::{BufRead, Write};
use std::panic::AssertUnwindSafe;
use std::sync::{Arc, Mutex};

use serde_json::{Value, json};

use crate::handlers;
use crate::scan::ScanManager;

pub struct App {
    pub write_lock: Mutex<()>,
    pub scans: ScanManager,
    pub version: String,
    /// Market server descriptors handed out by `discover`, kept in memory
    /// so `install_server` can trust metadata it actually offered rather
    /// than whatever a caller claims — bounded so a long session can't
    /// grow this without limit.
    pub market_cache: Mutex<Vec<(String, crate::market::MarketServer)>>,
}

impl App {
    pub fn new() -> Self {
        App {
            write_lock: Mutex::new(()),
            scans: ScanManager::new(),
            version: env!("CARGO_PKG_VERSION").to_string(),
            market_cache: Mutex::new(Vec::new()),
        }
    }
}

struct Request {
    id: Value,
    method: String,
    params: Value,
}

fn parse_request(line: &str) -> Result<Request, String> {
    let v: Value = serde_json::from_str(line).map_err(|e| format!("invalid JSON: {e}"))?;
    let id = v.get("id").cloned().unwrap_or(Value::Null);
    let method = v
        .get("method")
        .and_then(Value::as_str)
        .ok_or("method is required")?
        .to_string();
    let params = v
        .get("params")
        .cloned()
        .unwrap_or(Value::Object(Default::default()));
    Ok(Request { id, method, params })
}

fn handle(app: &App, line: &str) -> Value {
    let req = match parse_request(line) {
        Ok(r) => r,
        Err(e) => return json!({"id": Value::Null, "ok": false, "error": e}),
    };
    let id = req.id.clone();
    let outcome = std::panic::catch_unwind(AssertUnwindSafe(|| {
        if handlers::mutates(&req.method) {
            let _guard = app.write_lock.lock().unwrap_or_else(|p| p.into_inner());
            handlers::dispatch(app, &req.method, &req.params)
        } else {
            handlers::dispatch(app, &req.method, &req.params)
        }
    }));
    match outcome {
        Ok(Ok(data)) => json!({"id": id, "ok": true, "data": data}),
        Ok(Err(error)) => json!({"id": id, "ok": false, "error": error}),
        Err(_) => json!({"id": id, "ok": false, "error": "internal error handling the request"}),
    }
}

pub fn run() {
    let app = Arc::new(App::new());
    let stdout = Arc::new(Mutex::new(std::io::stdout()));
    let stdin = std::io::stdin();
    let mut reader = stdin.lock();
    let mut handles = Vec::new();
    loop {
        let mut line = String::new();
        match reader.read_line(&mut line) {
            Ok(0) => break,
            Ok(_) => {}
            Err(_) => break,
        }
        let line = line.trim_end().to_string();
        if line.is_empty() {
            continue;
        }
        let app = app.clone();
        let stdout = stdout.clone();
        handles.push(std::thread::spawn(move || {
            let response = handle(&app, &line);
            let text = serde_json::to_string(&response).unwrap_or_else(|_| {
                r#"{"id":null,"ok":false,"error":"internal encoding error"}"#.to_string()
            });
            if let Ok(mut out) = stdout.lock() {
                let _ = writeln!(out, "{text}");
                let _ = out.flush();
            }
        }));
        // keep the handle list from growing without bound across a very
        // long-lived session: drop handles for threads that already
        // finished instead of joining (responses are independent of
        // thread lifetime, this is only cleanup)
        handles.retain(|h| !h.is_finished());
    }
    for h in handles {
        let _ = h.join();
    }
}
