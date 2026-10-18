//! Cancellable HTTP/SSE transport, independent of Tauri IPC for integration tests.
use super::types::{ApiError, StreamChunk, StreamStatus};
use serde_json::Value;
use std::collections::HashMap;
use std::sync::{Arc, Mutex};
use std::time::Duration;
use tokio_util::sync::CancellationToken;

type Registry = Arc<Mutex<HashMap<String, CancellationToken>>>;

#[derive(Clone, Default)]
pub(crate) struct StreamTasks {
    active: Registry,
}

impl StreamTasks {
    pub(crate) fn register(&self, request_id: String) -> Result<Registration, ApiError> {
        if request_id.trim().is_empty() || request_id.len() > 256 {
            return Err(ApiError::new("requestId must contain 1 to 256 bytes"));
        }
        let mut active = self.active.lock().unwrap_or_else(|e| e.into_inner());
        if active.contains_key(&request_id) {
            return Err(ApiError::new("requestId already has an active stream"));
        }
        let cancel = CancellationToken::new();
        active.insert(request_id.clone(), cancel.clone());
        Ok(Registration {
            request_id,
            cancel,
            active: self.active.clone(),
        })
    }

    pub(crate) fn cancel(&self, request_id: &str) {
        let active = self.active.lock().unwrap_or_else(|e| e.into_inner());
        if let Some(cancel) = active.get(request_id) {
            cancel.cancel();
        }
    }

    pub(crate) fn cancel_all(&self) {
        let active = self.active.lock().unwrap_or_else(|e| e.into_inner());
        for cancel in active.values() {
            cancel.cancel();
        }
    }
}

/// Kept until the HTTP future is dropped. Cancelling must not remove the entry:
/// otherwise a reused ID could register before the previous task finishes.
pub(crate) struct Registration {
    request_id: String,
    pub(crate) cancel: CancellationToken,
    active: Registry,
}

impl Drop for Registration {
    fn drop(&mut self) {
        self.cancel.cancel();
        self.active
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .remove(&self.request_id);
    }
}

#[derive(Debug)]
enum Outcome {
    Completed(Option<String>),
    Cancelled,
    Failed(String),
    ReceiverClosed,
}

pub(crate) async fn run_stream<F>(registration: Registration, url: String, body: Value, token: Option<String>, mut send: F)
where
    F: FnMut(StreamChunk) -> bool + Send + 'static,
{
    let request_id = registration.request_id.clone();
    let mut count = 0;
    // Selecting over the entire future cancels connect, waiting for headers,
    // and an idle body read immediately, without waiting for another token.
    let outcome = tokio::select! {
        biased;
        _ = registration.cancel.cancelled() => Outcome::Cancelled,
        result = consume(&url, body, token, &request_id, &mut count, &mut send) => result,
    };
    // All network futures have been dropped before the registry and terminal
    // event are released; a new request cannot inherit a live old connection.
    drop(registration);
    let (status, content, error) = match outcome {
        Outcome::Completed(content) => (StreamStatus::Completed, content, None),
        Outcome::Cancelled => (StreamStatus::Cancelled, None, None),
        Outcome::Failed(error) => (StreamStatus::Failed, None, Some(error)),
        Outcome::ReceiverClosed => return,
    };
    let _ = send(StreamChunk {
        request_id,
        token: None,
        content,
        count: Some(count),
        error,
        done: true,
        status: Some(status),
    });
}

async fn consume<F>(
    url: &str,
    body: Value,
    token: Option<String>,
    request_id: &str,
    count: &mut u64,
    send: &mut F,
) -> Outcome
where
    F: FnMut(StreamChunk) -> bool,
{
    let client = match reqwest::Client::builder()
        .no_proxy()
        .redirect(reqwest::redirect::Policy::none())
        .connect_timeout(Duration::from_secs(10))
        .read_timeout(Duration::from_secs(120))
        .pool_max_idle_per_host(0)
        .build()
    {
        Ok(client) => client,
        Err(error) => return Outcome::Failed(format!("create stream client: {error}")),
    };
    let mut request = client.post(url).json(&body);
    if let Some(token) = token { request = request.bearer_auth(token); }
    let mut response = match request.send().await {
        Ok(response) => response,
        Err(error) => return Outcome::Failed(format!("connect stream: {error}")),
    };
    if !response.status().is_success() {
        let status = response.status();
        let text = response.text().await.unwrap_or_default();
        let detail = serde_json::from_str::<Value>(&text)
            .ok()
            .and_then(|value| value.get("error")?.as_str().map(str::to_owned));
        return Outcome::Failed(detail.unwrap_or_else(|| format!("stream HTTP {status}")));
    }

    let mut decoder = SseDecoder::default();
    loop {
        let bytes = match response.chunk().await {
            Ok(Some(bytes)) => bytes,
            Ok(None) => {
                // Accept legacy servers with no final blank line, but never
                // infer successful completion from the transport reaching EOF.
                let frames = match decoder.finish() {
                    Ok(frames) => frames,
                    Err(error) => return Outcome::Failed(error),
                };
                if let Some(outcome) = dispatch(frames, request_id, count, send) {
                    return outcome;
                }
                return Outcome::Failed("stream interrupted before completion".into());
            }
            Err(error) => return Outcome::Failed(format!("read stream: {error}")),
        };
        let frames = match decoder.push(&bytes) {
            Ok(frames) => frames,
            Err(error) => return Outcome::Failed(error),
        };
        if let Some(outcome) = dispatch(frames, request_id, count, send) {
            return outcome;
        }
    }
}

fn dispatch<F>(
    frames: Vec<String>,
    request_id: &str,
    count: &mut u64,
    send: &mut F,
) -> Option<Outcome>
where
    F: FnMut(StreamChunk) -> bool,
{
    for data in frames {
        let frame = match parse_frame(&data) {
            Ok(frame) => frame,
            Err(error) => return Some(Outcome::Failed(error)),
        };
        match frame {
            Frame::Token(token) => {
                *count += 1;
                if !send(StreamChunk {
                    request_id: request_id.to_owned(),
                    token: Some(token),
                    content: None,
                    count: Some(*count),
                    error: None,
                    done: false,
                    status: None,
                }) {
                    return Some(Outcome::ReceiverClosed);
                }
            }
            Frame::Terminal(outcome) => return Some(outcome),
            Frame::Ignore => {}
        }
    }
    None
}

#[derive(Debug)]
enum Frame {
    Token(String),
    Terminal(Outcome),
    Ignore,
}

fn parse_frame(data: &str) -> Result<Frame, String> {
    if data.trim() == "[DONE]" {
        return Ok(Frame::Terminal(Outcome::Completed(None)));
    }
    let value: Value =
        serde_json::from_str(data).map_err(|error| format!("invalid stream event: {error}"))?;
    if !value.is_object() {
        return Err("invalid stream event: expected an object".into());
    }
    let kind = value.get("type").and_then(Value::as_str);
    let status = value.get("status").and_then(Value::as_str);
    let error = value.get("error").and_then(Value::as_str);
    if status == Some("cancelled") || kind == Some("cancelled") {
        return Ok(Frame::Terminal(Outcome::Cancelled));
    }
    if error.is_some() || kind == Some("error") || status == Some("failed") {
        return Ok(Frame::Terminal(Outcome::Failed(
            error.unwrap_or("model stream failed").to_owned(),
        )));
    }
    if kind == Some("done")
        || value.get("done").and_then(Value::as_bool) == Some(true)
        || status == Some("completed")
    {
        return Ok(Frame::Terminal(Outcome::Completed(
            value
                .get("content")
                .and_then(Value::as_str)
                .map(str::to_owned),
        )));
    }
    if let Some(token) = value.get("token").and_then(Value::as_str) {
        return Ok(Frame::Token(token.to_owned()));
    }
    Ok(Frame::Ignore)
}

/// Decode UTF-8 only after a complete SSE event, including split multibyte
/// characters, CRLF, comments, and optional spaces following `data:`.
#[derive(Default)]
struct SseDecoder {
    line: Vec<u8>,
    data: Vec<u8>,
}

const MAX_EVENT_BYTES: usize = 4 * 1024 * 1024;

impl SseDecoder {
    fn push(&mut self, bytes: &[u8]) -> Result<Vec<String>, String> {
        let mut frames = Vec::new();
        for &byte in bytes {
            if byte == b'\n' {
                self.end_line(&mut frames)?;
            } else {
                self.line.push(byte);
                if self.line.len() + self.data.len() > MAX_EVENT_BYTES {
                    return Err("stream event exceeds 4 MiB".into());
                }
            }
        }
        Ok(frames)
    }

    fn end_line(&mut self, frames: &mut Vec<String>) -> Result<(), String> {
        if self.line.last() == Some(&b'\r') {
            self.line.pop();
        }
        let line = std::mem::take(&mut self.line);
        if line.is_empty() {
            self.emit(frames)?;
        } else if let Some(data) = line.strip_prefix(b"data:") {
            let data = data.strip_prefix(b" ").unwrap_or(data);
            if !self.data.is_empty() {
                self.data.push(b'\n');
            }
            self.data.extend_from_slice(data);
        }
        Ok(())
    }

    fn emit(&mut self, frames: &mut Vec<String>) -> Result<(), String> {
        if !self.data.is_empty() {
            frames.push(
                String::from_utf8(std::mem::take(&mut self.data))
                    .map_err(|error| format!("invalid stream UTF-8: {error}"))?,
            );
        }
        Ok(())
    }

    fn finish(&mut self) -> Result<Vec<String>, String> {
        let mut frames = Vec::new();
        if !self.line.is_empty() {
            self.end_line(&mut frames)?;
        }
        self.emit(&mut frames)?;
        Ok(frames)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    use tokio::io::{AsyncReadExt, AsyncWriteExt};
    use tokio::net::TcpListener;
    use tokio::sync::{mpsc, oneshot};
    use tokio::time::timeout;

    const DEADLINE: Duration = Duration::from_secs(3);

    fn active_count(tasks: &StreamTasks) -> usize {
        tasks.active.lock().unwrap().len()
    }

    // A real local HTTP peer lets cancellation tests assert that the transport
    // closes the socket, not merely that UI callbacks stop being delivered.
    async fn mock_server(
        response: Option<(&str, &str)>,
        stay_open: bool,
    ) -> (String, oneshot::Receiver<()>, oneshot::Receiver<()>) {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let url = format!("http://{}/stream", listener.local_addr().unwrap());
        let response = response.map(|(status, body)| (status.to_owned(), body.to_owned()));
        let (accepted_tx, accepted) = oneshot::channel();
        let (closed_tx, closed) = oneshot::channel();
        tokio::spawn(async move {
            let (mut socket, _) = listener.accept().await.unwrap();
            let mut request = Vec::new();
            let mut buf = [0u8; 4096];
            loop {
                let n = socket.read(&mut buf).await.unwrap();
                if n == 0 {
                    return;
                }
                request.extend_from_slice(&buf[..n]);
                if let Some(end) = request.windows(4).position(|v| v == b"\r\n\r\n") {
                    let headers = String::from_utf8_lossy(&request[..end]);
                    let length = headers
                        .lines()
                        .find_map(|line| {
                            let (key, value) = line.split_once(':')?;
                            key.eq_ignore_ascii_case("content-length")
                                .then(|| value.trim().parse::<usize>().unwrap())
                        })
                        .unwrap_or(0);
                    if request.len() >= end + 4 + length {
                        break;
                    }
                }
            }
            if let Some((status, body)) = response {
                let wire = if stay_open {
                    format!("HTTP/1.1 {status}\r\nContent-Type: text/event-stream\r\nTransfer-Encoding: chunked\r\n\r\n{:x}\r\n{body}\r\n", body.len())
                } else {
                    format!("HTTP/1.1 {status}\r\nContent-Type: text/event-stream\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}", body.len())
                };
                socket.write_all(wire.as_bytes()).await.unwrap();
            }
            let _ = accepted_tx.send(());
            if stay_open {
                loop {
                    match socket.read(&mut buf).await {
                        Ok(0) | Err(_) => break,
                        Ok(_) => {}
                    }
                }
            }
            let _ = closed_tx.send(());
        });
        (url, accepted, closed)
    }

    async fn collect(status: &str, body: &str) -> Vec<StreamChunk> {
        let tasks = StreamTasks::default();
        let (url, _, _) = mock_server(Some((status, body)), false).await;
        let registration = tasks.register("test-request".into()).unwrap();
        let events = Arc::new(Mutex::new(Vec::new()));
        let received = events.clone();
        timeout(
            DEADLINE,
            run_stream(registration, url, json!({}), None, move |chunk| {
                received.lock().unwrap().push(chunk);
                true
            }),
        )
        .await
        .unwrap();
        assert_eq!(active_count(&tasks), 0);
        let events = events.lock().unwrap().clone();
        assert!(events
            .iter()
            .all(|event| event.request_id == "test-request"));
        assert_eq!(events.iter().filter(|event| event.done).count(), 1);
        events
    }

    #[test]
    fn decoder_preserves_split_unicode_multiline_comments_and_crlf() {
        let wire =
            ": heartbeat\r\nevent: token\r\ndata:{\"token\":\r\ndata: \"莱茵生命🧪\"}\r\n\r\n";
        let mut decoder = SseDecoder::default();
        let mut frames = Vec::new();
        for byte in wire.as_bytes() {
            frames.extend(decoder.push(&[*byte]).unwrap());
        }
        assert_eq!(frames.len(), 1);
        assert!(
            matches!(parse_frame(&frames[0]).unwrap(), Frame::Token(token) if token == "莱茵生命🧪")
        );
    }

    #[test]
    fn decoder_rejects_invalid_utf8_and_oversized_events() {
        assert!(SseDecoder::default().push(b"data: \xff\n\n").is_err());
        assert!(SseDecoder::default()
            .push(&vec![b'x'; MAX_EVENT_BYTES + 1])
            .is_err());
    }

    #[test]
    fn registration_rejects_duplicates_and_cancel_is_idempotent() {
        let tasks = StreamTasks::default();
        assert!(tasks.register(" ".into()).is_err());
        let registration = tasks.register("one".into()).unwrap();
        assert!(tasks.register("one".into()).is_err());
        tasks.cancel("one");
        tasks.cancel("one");
        assert!(registration.cancel.is_cancelled());
        assert!(tasks.register("one".into()).is_err());
        drop(registration);
        tasks.cancel("one");
        assert_eq!(active_count(&tasks), 0);
        assert!(tasks.register("one".into()).is_ok());
    }

    #[tokio::test]
    async fn preserves_final_body_without_duplicating_token_text() {
        let events = collect("200 OK", "data: {\"token\":\"part\"}\n\ndata: {\"type\":\"done\",\"content\":\"complete body\",\"status\":\"completed\"}\n\n").await;
        assert_eq!(events.len(), 2);
        assert_eq!(events[0].token.as_deref(), Some("part"));
        assert_eq!(events[1].token, None);
        assert_eq!(events[1].content.as_deref(), Some("complete body"));
        assert_eq!(events[1].status, Some(StreamStatus::Completed));
        assert_eq!(events[1].count, Some(1));
    }

    #[tokio::test]
    async fn one_shot_fallback_body_is_delivered() {
        let events = collect(
            "200 OK",
            "data: {\"type\":\"done\",\"content\":\"Gemini answer\"}\n\n",
        )
        .await;
        assert_eq!(events.len(), 1);
        assert_eq!(events[0].content.as_deref(), Some("Gemini answer"));
        assert_eq!(events[0].status, Some(StreamStatus::Completed));
    }

    #[tokio::test]
    async fn eof_is_failed_while_preserving_received_tokens() {
        let events = collect("200 OK", "data: {\"token\":\"partial answer\"}\n\n").await;
        assert_eq!(events[0].token.as_deref(), Some("partial answer"));
        assert_eq!(events[1].status, Some(StreamStatus::Failed));
        assert!(events[1].error.as_deref().unwrap().contains("interrupted"));
    }

    #[tokio::test]
    async fn explicit_error_has_one_failed_terminal() {
        let events = collect("200 OK", "data: {\"type\":\"error\",\"error\":\"upstream disconnected\",\"status\":\"failed\"}\n\ndata: {\"type\":\"done\"}\n\n").await;
        assert_eq!(events.len(), 1);
        assert_eq!(events[0].status, Some(StreamStatus::Failed));
        assert_eq!(events[0].error.as_deref(), Some("upstream disconnected"));
    }

    #[tokio::test]
    async fn malformed_json_is_failed_and_http_error_is_preserved() {
        let events = collect("200 OK", "data: {broken}\n\n").await;
        assert_eq!(events[0].status, Some(StreamStatus::Failed));
        let events = collect(
            "503 Service Unavailable",
            "{\"error\":\"model unavailable\"}",
        )
        .await;
        assert_eq!(events[0].status, Some(StreamStatus::Failed));
        assert_eq!(events[0].error.as_deref(), Some("model unavailable"));
    }

    #[tokio::test]
    async fn legacy_done_formats_and_duplicate_terminal_are_supported() {
        for body in [
            "data: [DONE]\n\n",
            "data: {\"done\":true}",
            "data: {\"type\":\"done\"}\n\ndata: {\"type\":\"done\"}\n\n",
        ] {
            let events = collect("200 OK", body).await;
            assert_eq!(events.len(), 1);
            assert_eq!(events[0].status, Some(StreamStatus::Completed));
        }
    }

    async fn cancellation_case(body: Option<&str>) {
        let tasks = StreamTasks::default();
        let (url, accepted, closed) = mock_server(body.map(|body| ("200 OK", body)), true).await;
        let registration = tasks.register("cancel-test".into()).unwrap();
        let (tx, mut rx) = mpsc::unbounded_channel();
        let task = tokio::spawn(run_stream(registration, url, json!({}), None, move |chunk| {
            tx.send(chunk).is_ok()
        }));
        timeout(DEADLINE, accepted).await.unwrap().unwrap();
        if body.is_some() {
            let token = timeout(DEADLINE, rx.recv()).await.unwrap().unwrap();
            assert!(!token.done);
            assert_eq!(token.token.as_deref(), Some("hello"));
        }
        tasks.cancel("cancel-test");
        tasks.cancel("cancel-test");
        timeout(DEADLINE, task).await.unwrap().unwrap();
        timeout(DEADLINE, closed).await.unwrap().unwrap();
        let terminal = rx.recv().await.unwrap();
        assert!(terminal.done);
        assert_eq!(terminal.status, Some(StreamStatus::Cancelled));
        assert!(rx.recv().await.is_none());
        assert_eq!(active_count(&tasks), 0);
    }

    #[tokio::test]
    async fn cancels_while_waiting_for_first_response_headers() {
        cancellation_case(None).await;
    }

    #[tokio::test]
    async fn cancels_while_waiting_for_next_token_and_closes_socket() {
        cancellation_case(Some("data: {\"token\":\"hello\"}\n\n")).await;
    }

    #[tokio::test]
    async fn closed_ipc_receiver_drops_transport_and_registration() {
        let tasks = StreamTasks::default();
        let (url, _, closed) =
            mock_server(Some(("200 OK", "data: {\"token\":\"hello\"}\n\n")), true).await;
        let registration = tasks.register("closed-receiver".into()).unwrap();
        let attempts = Arc::new(Mutex::new(0));
        let count = attempts.clone();
        timeout(
            DEADLINE,
            run_stream(registration, url, json!({}), None, move |_| {
                *count.lock().unwrap() += 1;
                false
            }),
        )
        .await
        .unwrap();
        timeout(DEADLINE, closed).await.unwrap().unwrap();
        assert_eq!(*attempts.lock().unwrap(), 1);
        assert_eq!(active_count(&tasks), 0);
    }

    #[tokio::test]
    async fn window_shutdown_cancels_all_active_streams() {
        let tasks = StreamTasks::default();
        let mut workers = Vec::new();
        for index in 0..3 {
            let (url, accepted, closed) = mock_server(None, true).await;
            let registration = tasks.register(format!("window-{index}")).unwrap();
            let worker = tokio::spawn(run_stream(registration, url, json!({}), None, |event| {
                assert_eq!(event.status, Some(StreamStatus::Cancelled));
                true
            }));
            timeout(DEADLINE, accepted).await.unwrap().unwrap();
            workers.push((worker, closed));
        }
        assert_eq!(active_count(&tasks), 3);
        tasks.cancel_all();
        for (worker, closed) in workers {
            timeout(DEADLINE, worker).await.unwrap().unwrap();
            timeout(DEADLINE, closed).await.unwrap().unwrap();
        }
        assert_eq!(active_count(&tasks), 0);
    }

    #[tokio::test]
    async fn aborted_task_cleans_registration() {
        let tasks = StreamTasks::default();
        let (url, accepted, closed) = mock_server(None, true).await;
        let registration = tasks.register("abort-test".into()).unwrap();
        let worker = tokio::spawn(run_stream(registration, url, json!({}), None, |_| true));
        timeout(DEADLINE, accepted).await.unwrap().unwrap();
        worker.abort();
        assert!(worker.await.unwrap_err().is_cancelled());
        timeout(DEADLINE, closed).await.unwrap().unwrap();
        assert_eq!(active_count(&tasks), 0);
    }
}
