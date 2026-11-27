use super::*;
use std::io::{BufRead, BufReader};

struct GatewayProcess(Child);
impl Drop for GatewayProcess {
    fn drop(&mut self) {
        let _ = self.0.kill();
        let _ = self.0.wait();
    }
}

// The Rust process is the desktop parent of both the production supervisor and
// the gateway. The gateway authenticates this parent, process group and the
// kernel owner of the specific connected socket before writing the HTTP request.
pub(crate) fn stream_managed_model(data: &Path, alias: &str) {
    let id = EngineId::CustomLlama;
    let sidecar = Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("bin")
        .join(format!("noosphere-backend{}", std::env::consts::EXE_SUFFIX));
    assert!(sidecar.is_file(), "stage the native desktop sidecar first");
    assert!(
        TcpListener::bind(id.address()).is_ok(),
        "managed model port already occupied"
    );
    let engines = LocalEngines::default();
    let control =
        |action, model| engines.handle(id, action, data, None, Some(&sidecar), model, None);
    let started = control(EngineAction::Start, Some(alias)).unwrap();
    assert!(started.available && started.managed);
    assert!(started
        .models
        .iter()
        .any(|m| m.id == alias && m.loaded == Some(true)));
    let lease_path = data.join("local-engines/custom-listener.json");
    let lease_bytes = fs::read(&lease_path).unwrap();
    let mut lease: Value = serde_json::from_slice(&lease_bytes).unwrap();
    assert_eq!(lease["desktop_pid"], std::process::id());
    assert_eq!(lease["model"], alias);
    let pid = u32::try_from(lease["pid"].as_u64().unwrap()).unwrap();
    assert!(discovery::listener_matches(id.port(), pid));

    let gateway_listener = TcpListener::bind((Ipv4Addr::LOCALHOST, 0)).unwrap();
    let address = gateway_listener.local_addr().unwrap();
    let models_path = data.join("models.yaml");
    let config_path = data.join("config.yaml");
    fs::write(
        &models_path,
        json!({
            "default_model":"managed-live",
            "providers":[{"id":"managed-live","name":"Managed GGUF","type":"openai_compatible",
                          "model":alias,"base_url":format!("{}/v1", id.origin()),"enabled":true}]
        })
        .to_string(),
    )
    .unwrap();
    fs::write(&config_path, json!({
        "app":{"data_dir":data.join("data")}, "server":{"host":"127.0.0.1","port":address.port()},
        "gateway":{"config_path":models_path}, "logging":{"dir":data.join("logs")},
        "modules":{"nebula":{"enabled":false},"devops":{"enabled":false},"codelens":{"enabled":false}}
    }).to_string()).unwrap();
    let token = crate::local_auth::new_token().unwrap();
    let bearer = format!("Bearer {token}");
    let log_path = data.join("live-gateway.log");
    let log = File::create(&log_path).unwrap();
    let mut command = Command::new(&sidecar);
    command
        .arg("-config")
        .arg(config_path)
        .current_dir(data)
        .env("NOOSPHERE_BACKEND_TOKEN", token)
        .env_remove("NOOSPHERE_DESKTOP_MANAGED")
        .stdin(Stdio::null())
        .stdout(log.try_clone().unwrap())
        .stderr(log);
    drop(gateway_listener);
    let mut gateway = GatewayProcess(command.spawn().unwrap());
    let agent = local_agent();
    let deadline = Instant::now() + Duration::from_secs(30);
    loop {
        let health = agent
            .get(&format!("http://{address}/health"))
            .set("Authorization", &bearer)
            .timeout(Duration::from_secs(1))
            .call()
            .ok()
            .and_then(|r| r.into_json::<Value>().ok());
        if health.is_some_and(|v| v["status"] == "ok") {
            break;
        }
        assert!(
            Instant::now() < deadline && gateway.0.try_wait().unwrap().is_none(),
            "gateway did not start: {}",
            fs::read_to_string(&log_path).unwrap_or_default()
        );
        std::thread::sleep(Duration::from_millis(100));
    }

    let chat = || {
        agent
            .post(&format!("http://{address}/api/gateway/chat/stream"))
            .set("Authorization", &bearer)
            .timeout(Duration::from_secs(90))
            .send_json(
                json!({"model_id":"managed-live","preview":true,"max_tokens":24,
                          "messages":[{"role":"user","content":"Say hello in English."}]}),
            )
    };

    // A live HTTP server with a stale process group must never receive this prompt.
    // Change only the lease in this test's isolated, disposable data directory.
    lease["group_pid"] = json!(u32::MAX);
    fs::write(&lease_path, lease.to_string()).unwrap();
    let rejected = chat();
    fs::write(&lease_path, lease_bytes).unwrap();
    match rejected {
        Err(ureq::Error::Status(code, _)) => assert!(code >= 400),
        Ok(response) => {
            let body = response.into_string().unwrap();
            let events = body
                .lines()
                .filter_map(|l| l.strip_prefix("data:"))
                .map(|s| serde_json::from_str::<Value>(s.trim()).unwrap())
                .collect::<Vec<_>>();
            assert!(
                events.iter().any(|e| e["type"] == "error")
                    && events
                        .iter()
                        .all(|e| e["type"] != "done" && e["type"] != "token"),
                "{body}"
            );
        }
        Err(error) => panic!("gateway transport failed during ownership check: {error}"),
    }

    let response = chat().unwrap_or_else(|err| match err {
        ureq::Error::Status(code, r) => panic!(
            "gateway HTTP {code}: {}",
            r.into_string().unwrap_or_default()
        ),
        other => panic!("gateway transport failed: {other}"),
    });
    assert!(response
        .header("Content-Type")
        .unwrap_or_default()
        .contains("text/event-stream"));
    let mut content = String::new();
    let mut done = None;
    for line in BufReader::new(response.into_reader().take(1024 * 1024)).lines() {
        let line = line.unwrap();
        let Some(payload) = line.strip_prefix("data:") else {
            continue;
        };
        let event: Value = serde_json::from_str(payload.trim()).unwrap();
        match event["type"].as_str().unwrap() {
            "token" => content.push_str(event["token"].as_str().unwrap()),
            "done" => {
                assert_eq!(event["status"], "completed");
                done = event["content"].as_str().map(str::to_owned);
                break;
            }
            _ => panic!("unexpected gateway SSE event: {event}"),
        }
    }
    assert!(!content.trim().is_empty());
    assert_eq!(done.as_deref(), Some(content.as_str()));
    let stopped = control(EngineAction::Stop, None).unwrap();
    assert!(!stopped.managed && !stopped.available);
    assert!(!lease_path.exists() && !socket_online(id.address()));
    drop(gateway);
    assert!(!socket_online(address));
    println!("Managed supervisor and gateway verified PID {pid}, rejected stale group, streamed {} bytes and released model and gateway ports.", content.len());
}
