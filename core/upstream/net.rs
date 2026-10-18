//! HTTP downloads with live proxy routing and a complete-fetch deadline.
//!
//! Vendored from upstream siu (see UPSTREAM.md). A separate deadline bounds
//! the entire request, including response-body reads, even if the HTTP
//! transport stops honoring its own timeout.

use std::io::Read;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::{Mutex, OnceLock, mpsc};
use std::time::Duration;

use anyhow::{Context, Result, bail};

// skills.sh answers a client it doesn't know with a page of its own
const UA: &str = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0 Safari/537.36 siu";

const REQUEST_TIMEOUT: Duration = Duration::from_secs(60);
const MAX_HTTP_WORKERS: usize = 8;
static HTTP_WORKERS: AtomicUsize = AtomicUsize::new(0);

struct WorkerPermit(&'static AtomicUsize);

impl WorkerPermit {
    fn acquire(workers: &'static AtomicUsize) -> Result<Self> {
        workers
            .try_update(Ordering::Relaxed, Ordering::Relaxed, |active| {
                (active < MAX_HTTP_WORKERS).then_some(active + 1)
            })
            .map_err(|_| anyhow::anyhow!(
                "HTTP requests are still stalled; check your connection or proxy before retrying"
            ))?;
        Ok(Self(workers))
    }
}

impl Drop for WorkerPermit {
    fn drop(&mut self) {
        self.0.fetch_sub(1, Ordering::Relaxed);
    }
}

fn agent(proxy: Option<ureq::Proxy>) -> ureq::Agent {
    // A proxy switch must never reuse a direct connection (or another
    // proxy's connection). Keep a small pool per resolved route instead.
    type CachedAgents = Vec<(Option<ureq::Proxy>, ureq::Agent)>;
    static AGENTS: OnceLock<Mutex<CachedAgents>> = OnceLock::new();
    let mut agents = AGENTS
        .get_or_init(|| Mutex::new(Vec::new()))
        .lock()
        .unwrap_or_else(|p| p.into_inner());
    if let Some((_, agent)) = agents.iter().find(|(key, _)| key == &proxy) {
        return agent.clone();
    }
    let agent: ureq::Agent = ureq::Agent::config_builder()
        .proxy(proxy.clone()) // None explicitly means direct, never auto-env.
        .timeout_global(Some(REQUEST_TIMEOUT))
        .max_redirects(0) // Re-resolve system/bypass rules on each redirect.
        .user_agent(UA)
        .build()
        .into();
    if agents.len() == 8 {
        agents.remove(0);
    }
    agents.push((proxy, agent.clone()));
    agent
}

pub fn get(url: &str, accept: &str, limit: u64) -> Result<Vec<u8>> {
    get_with_timeout(None, url, accept, limit, REQUEST_TIMEOUT)
}

fn get_with_timeout(
    agent: Option<ureq::Agent>,
    url: &str,
    accept: &str,
    limit: u64,
    timeout: Duration,
) -> Result<Vec<u8>> {
    // Bound even requests whose underlying transport never returns. The
    // permit stays with the worker until its socket I/O actually finishes.
    let permit = WorkerPermit::acquire(&HTTP_WORKERS)?;
    let request_url = url.to_string();
    let accept = accept.to_string();
    let (tx, rx) = mpsc::sync_channel(1);
    // Only network I/O runs here. A late response is discarded and can
    // never update the library after the caller has reported a timeout.
    std::thread::Builder::new()
        .name("siu-http".into())
        .spawn(move || {
            let _permit = permit;
            let _ = tx.send(get_blocking(agent.as_ref(), &request_url, &accept, limit));
        })
        .context("couldn't start HTTP request")?;
    match rx.recv_timeout(timeout) {
        Ok(result) => result,
        Err(mpsc::RecvTimeoutError::Timeout) => {
            bail!(
                "GET {} timed out after {timeout:?}; check your connection or proxy and retry",
                host(url)
            )
        }
        Err(mpsc::RecvTimeoutError::Disconnected) => {
            bail!("GET {} ended without a response", host(url))
        }
    }
}

fn get_blocking(
    agent: Option<&ureq::Agent>,
    url: &str,
    accept: &str,
    limit: u64,
) -> Result<Vec<u8>> {
    let settings = if agent.is_none() {
        Some(crate::proxy::current()?)
    } else {
        None
    };
    get_configured(agent, settings.as_ref(), url, accept, limit)
}

fn get_configured(
    override_agent: Option<&ureq::Agent>,
    settings: Option<&crate::proxy::Settings>,
    url: &str,
    accept: &str,
    limit: u64,
) -> Result<Vec<u8>> {
    let mut current = proxy_watch::Url::parse(url).context("invalid download URL")?;
    for _ in 0..=10 {
        let agents = match override_agent {
            Some(agent) => vec![agent.clone()],
            None => {
                crate::proxy::routes(settings.context("proxy settings are missing")?, &current)?
                    .into_iter()
                    .map(agent)
                    .collect()
            }
        };
        let mut response = None;
        let mut error = None;
        for agent in agents {
            match agent.get(current.as_str()).header("Accept", accept).call() {
                Ok(value) => {
                    response = Some(value);
                    break;
                }
                Err(e) => error = Some(e),
            }
        }
        let Some(resp) = response else {
            return Err(error
                .map(anyhow::Error::from)
                .unwrap_or_else(|| anyhow::anyhow!("no proxy route available")))
            .with_context(|| format!("GET {}", host(current.as_str())));
        };
        if matches!(resp.status().as_u16(), 301 | 302 | 303 | 307 | 308)
            && let Some(location) = resp.headers().get("Location")
        {
            let next = current.join(location.to_str().context("invalid download redirect")?)?;
            if !matches!(next.scheme(), "http" | "https") {
                bail!("unsupported download redirect");
            }
            current = next;
            continue;
        }
        let mut out = vec![];
        resp.into_body()
            .into_with_config()
            .limit(limit + 1)
            .reader()
            .read_to_end(&mut out)
            .with_context(|| format!("reading {}", host(current.as_str())))?;
        if out.len() as u64 > limit {
            bail!(
                "{} sent more than {} MB",
                host(current.as_str()),
                limit >> 20
            );
        }
        return Ok(out);
    }
    bail!("too many download redirects")
}

pub fn get_json<T: serde::de::DeserializeOwned>(url: &str) -> Result<T> {
    let b = get(url, "application/json", 16 << 20)?;
    serde_json::from_slice(&b)
        .with_context(|| format!("{} answered something unexpected", host(url)))
}

fn host(url: &str) -> &str {
    url.split("://")
        .nth(1)
        .and_then(|r| r.split('/').next())
        .unwrap_or(url)
}

/// Percent-encodes a query value.
pub fn encode(s: &str) -> String {
    let mut out = String::new();
    for b in s.bytes() {
        match b {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => {
                out.push(b as char)
            }
            _ => out.push_str(&format!("%{b:02X}")),
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::{Read, Write};
    use std::net::{TcpListener, TcpStream};
    use std::time::Instant;

    fn test_agent() -> ureq::Agent {
        // No transport timeout: exercise the independent deadline, with
        // no dependency on the machine's proxy settings or the internet.
        ureq::Agent::config_builder().proxy(None).build().into()
    }

    #[test]
    fn stalled_workers_are_bounded_and_completed_workers_free_capacity() {
        static WORKERS: AtomicUsize = AtomicUsize::new(0);
        let mut permits: Vec<_> = (0..MAX_HTTP_WORKERS)
            .map(|_| WorkerPermit::acquire(&WORKERS).unwrap())
            .collect();
        assert!(WorkerPermit::acquire(&WORKERS).is_err());
        permits.pop();
        permits.push(WorkerPermit::acquire(&WORKERS).unwrap());
        drop(permits);
        assert_eq!(WORKERS.load(Ordering::Relaxed), 0);
    }

    fn assert_stalled_request_times_out(send_headers: bool) {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let url = format!("http://{}/skill.tar.gz", listener.local_addr().unwrap());
        let (release, wait) = mpsc::channel();
        let server = std::thread::spawn(move || {
            let (mut stream, _) = listener.accept().unwrap();
            stream
                .set_read_timeout(Some(Duration::from_secs(3)))
                .unwrap();
            let mut buf = [0; 4096];
            assert!(stream.read(&mut buf).unwrap() > 0);
            if send_headers {
                stream
                    .write_all(
                        b"HTTP/1.1 200 OK\r\nContent-Length: 4\r\nConnection: close\r\n\r\no",
                    )
                    .unwrap();
            }
            let _ = wait.recv_timeout(Duration::from_secs(3));
        });
        let started = Instant::now();
        let result = get_with_timeout(
            Some(test_agent()),
            &url,
            "application/gzip",
            1024,
            Duration::from_millis(150),
        );
        let elapsed = started.elapsed();
        let _ = release.send(());
        server.join().unwrap();
        let error = format!("{:#}", result.unwrap_err());
        assert!(error.contains("timed out"), "{error}");
        assert!(error.contains("127.0.0.1"), "{error}");
        assert!(elapsed < Duration::from_secs(2), "waited {elapsed:?}");
    }

    #[test]
    fn stalled_response_headers_have_a_deadline() {
        assert_stalled_request_times_out(false);
    }

    #[test]
    fn stalled_response_body_has_a_deadline() {
        assert_stalled_request_times_out(true);
    }

    #[test]
    fn complete_responses_and_size_limits_are_preserved() {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let url = format!("http://{}/skill.tar.gz", listener.local_addr().unwrap());
        let server = std::thread::spawn(move || {
            for _ in 0..2 {
                let (mut stream, _) = listener.accept().unwrap();
                stream
                    .set_read_timeout(Some(Duration::from_secs(3)))
                    .unwrap();
                let mut buf = [0; 4096];
                assert!(stream.read(&mut buf).unwrap() > 0);
                stream
                    .write_all(
                        b"HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok",
                    )
                    .unwrap();
            }
        });
        let agent = test_agent();
        let body = get_with_timeout(
            Some(agent.clone()),
            &url,
            "application/gzip",
            2,
            Duration::from_secs(2),
        )
        .unwrap();
        assert_eq!(body, b"ok");
        let error = get_with_timeout(
            Some(agent),
            &url,
            "application/gzip",
            1,
            Duration::from_secs(2),
        )
        .unwrap_err();
        assert!(
            format!("{error:#}").contains("larger than request limit"),
            "{error:#}"
        );
        server.join().unwrap();
    }

    fn read_headers(stream: &mut TcpStream) -> String {
        stream
            .set_read_timeout(Some(Duration::from_secs(3)))
            .unwrap();
        let mut bytes = Vec::new();
        while !bytes.ends_with(b"\r\n\r\n") {
            let mut b = [0];
            stream.read_exact(&mut b).unwrap();
            bytes.push(b[0]);
            assert!(bytes.len() < 16384);
        }
        String::from_utf8(bytes).unwrap()
    }

    fn http_proxy(reply: &'static [u8]) -> (String, std::thread::JoinHandle<String>) {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let url = format!("http://{}", listener.local_addr().unwrap());
        let server = std::thread::spawn(move || {
            let (mut stream, _) = listener.accept().unwrap();
            let connect = read_headers(&mut stream);
            assert!(
                connect.starts_with("CONNECT downloads.invalid:80 HTTP/1.1"),
                "{connect}"
            );
            stream
                .write_all(b"HTTP/1.1 200 Connection established\r\n\r\n")
                .unwrap();
            let request = read_headers(&mut stream);
            assert!(request.starts_with("GET /x HTTP/1.1"), "{request}");
            assert!(!request.to_ascii_lowercase().contains("proxy-authorization"));
            stream.write_all(reply).unwrap();
            connect
        });
        (url, server)
    }

    #[test]
    fn http_proxy_authentication_forwards_a_complete_download() {
        let (proxy, server) =
            http_proxy(b"HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok");
        let settings = crate::proxy::Settings {
            mode: crate::proxy::Mode::Custom,
            url: proxy.replace("http://", "http://Alice:Pa%40sS@"),
            no_proxy: String::new(),
        };
        assert_eq!(
            get_configured(
                None,
                Some(&settings),
                "http://downloads.invalid/x",
                "application/gzip",
                1024
            )
            .unwrap(),
            b"ok"
        );
        let connect = server.join().unwrap();
        assert!(
            connect.contains("Proxy-Authorization: Basic QWxpY2U6UGFAc1M="),
            "{connect}"
        );
    }

    #[test]
    fn saved_proxy_changes_and_redirect_bypass_take_effect_without_restart() {
        let home = tempfile::tempdir().unwrap();
        let (first, first_server) =
            http_proxy(b"HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok");
        let settings = crate::proxy::Settings {
            mode: crate::proxy::Mode::Custom,
            url: first,
            no_proxy: "127.0.0.1".into(),
        };
        crate::proxy::save(home.path(), settings).unwrap();
        let fetch = |url| {
            get_configured(
                None,
                Some(&crate::proxy::load(home.path()).unwrap()),
                url,
                "application/gzip",
                1024,
            )
            .unwrap()
        };
        assert_eq!(fetch("http://downloads.invalid/x"), b"ok");
        first_server.join().unwrap();

        let target = TcpListener::bind("127.0.0.1:0").unwrap();
        let target_url = format!("http://{}/redirected", target.local_addr().unwrap());
        let target_server = std::thread::spawn(move || {
            for _ in 0..2 {
                let (mut stream, _) = target.accept().unwrap();
                assert!(read_headers(&mut stream).starts_with("GET /redirected HTTP/1.1"));
                stream
                    .write_all(
                        b"HTTP/1.1 200 OK\r\nContent-Length: 6\r\nConnection: close\r\n\r\ndirect",
                    )
                    .unwrap();
            }
        });
        let second = TcpListener::bind("127.0.0.1:0").unwrap();
        let second_url = format!("http://{}", second.local_addr().unwrap());
        let redirect_url = target_url.clone();
        let second_server = std::thread::spawn(move || {
            let (mut stream, _) = second.accept().unwrap();
            assert!(read_headers(&mut stream).starts_with("CONNECT downloads.invalid:80 HTTP/1.1"));
            stream
                .write_all(b"HTTP/1.1 200 Connection established\r\n\r\n")
                .unwrap();
            assert!(read_headers(&mut stream).starts_with("GET /x HTTP/1.1"));
            write!(stream, "HTTP/1.1 302 Found\r\nLocation: {redirect_url}\r\nContent-Length: 0\r\nConnection: close\r\n\r\n").unwrap();
        });
        crate::proxy::save(
            home.path(),
            crate::proxy::Settings {
                mode: crate::proxy::Mode::Custom,
                url: second_url,
                no_proxy: "127.0.0.1".into(),
            },
        )
        .unwrap();
        assert_eq!(fetch("http://downloads.invalid/x"), b"direct");
        second_server.join().unwrap();
        crate::proxy::save(
            home.path(),
            crate::proxy::Settings {
                mode: crate::proxy::Mode::Direct,
                ..Default::default()
            },
        )
        .unwrap();
        assert_eq!(fetch(&target_url), b"direct");
        target_server.join().unwrap();
    }

    /// A SOCKS5 proxy uses remote DNS and the shared download path.
    #[test]
    fn requests_go_through_a_socks5_proxy() {
        let l = TcpListener::bind("127.0.0.1:0").unwrap();
        let port = l.local_addr().unwrap().port();
        let server = std::thread::spawn(move || {
            let (mut s, _) = l.accept().unwrap();
            let mut b = [0u8; 512];
            // greeting: no authentication
            let n = s.read(&mut b).unwrap();
            assert_eq!(b[0], 5, "{:?}", &b[..n]);
            s.write_all(&[5, 0]).unwrap();
            // CONNECT, then answer the request ourselves
            let n = s.read(&mut b).unwrap();
            assert_eq!(&b[..2], &[5, 1], "{:?}", &b[..n]);
            s.write_all(&[5, 0, 0, 1, 0, 0, 0, 0, 0, 0]).unwrap();
            let n = s.read(&mut b).unwrap();
            assert!(b[..n].starts_with(b"GET /x "));
            s.write_all(b"HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
                .unwrap();
        });
        let settings = crate::proxy::Settings {
            mode: crate::proxy::Mode::Custom,
            url: format!("socks5h://127.0.0.1:{port}"),
            no_proxy: String::new(),
        };
        let body = get_configured(
            None,
            Some(&settings),
            "http://downloads.invalid/x",
            "application/gzip",
            1024,
        )
        .unwrap();
        assert_eq!(body, b"ok");
        server.join().unwrap();
    }
}
