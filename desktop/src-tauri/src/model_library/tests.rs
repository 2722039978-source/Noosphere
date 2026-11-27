use super::*;

struct Directory(PathBuf);
impl Directory {
    fn new() -> Self {
        let path = std::env::temp_dir().join(unique_id("noosphere-model-test").unwrap());
        fs::create_dir(&path).unwrap();
        // macOS can expose TMPDIR through /var -> /private/var. Use the same
        // canonical local directory contract as imported and relocated files.
        Self(canonical_local(&path).unwrap())
    }
}
impl Drop for Directory {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}
fn string(data: &mut Vec<u8>, v: &str) {
    data.extend((v.len() as u64).to_le_bytes());
    data.extend(v.as_bytes());
}
fn fixture(dir: &Path, name: &str, arch: &str, embedding: u32) -> PathBuf {
    let path = dir.join(name);
    let mut data = b"GGUF".to_vec();
    data.extend(3u32.to_le_bytes());
    data.extend(1u64.to_le_bytes());
    data.extend(3u64.to_le_bytes());
    string(&mut data, "general.architecture");
    data.extend(8u32.to_le_bytes());
    string(&mut data, arch);
    string(&mut data, &format!("{arch}.context_length"));
    data.extend(4u32.to_le_bytes());
    data.extend(8192u32.to_le_bytes());
    let key = if arch == "clip" {
        "clip.vision.projection_dim".into()
    } else {
        format!("{arch}.embedding_length")
    };
    string(&mut data, &key);
    data.extend(4u32.to_le_bytes());
    data.extend(embedding.to_le_bytes());
    data.extend([0; 32]);
    fs::write(&path, data).unwrap();
    path
}

#[test]
fn batch_import_is_idempotent_preserves_aliases_and_never_removes_weights() {
    let dir = Directory::new();
    let root = &dir.0;
    let file = fixture(root, "中文权重.gguf", "llama", 32);
    let other = root.join("sub");
    fs::create_dir(&other).unwrap();
    fixture(&other, "中文权重.gguf", "llama", 64);
    fs::write(root.join("bad.gguf"), b"invalid").unwrap();
    fixture(root, "vision.gguf", "clip", 32);
    // Importing weights must never discover and authorize an adjacent program.
    fs::write(root.join(runtime::SERVER), b"never execute me").unwrap();
    let snapshot = modify_for_test(root, json!({"action":"scan","path":root})).unwrap();
    let entries = snapshot["models"].as_array().unwrap();
    assert_eq!(entries.len(), 2);
    assert_ne!(entries[0]["id"], entries[1]["id"]);
    assert_eq!(snapshot["warnings"].as_array().unwrap().len(), 2);
    assert!(snapshot["runtimes"].as_array().unwrap().is_empty());
    let snap = modify_for_test(root, json!({"action":"import","paths":[file,file]})).unwrap();
    assert_eq!(snap["models"], snapshot["models"]);
    let row = entries
        .iter()
        .find(|m| m["path"].as_str() == file.to_str())
        .unwrap();
    let id = &row["id"];
    assert_eq!(row["trained_context"], 8192);
    assert_eq!(row["status"], "runtime_missing");
    assert!(resolve(root, id.as_str().unwrap()).is_err());
    let dest = root.join("moved.gguf");
    fs::rename(&file, &dest).unwrap();
    let lib = Library::open(root).unwrap();
    assert_eq!(
        lib.status(
            lib.models
                .iter()
                .find(|m| m.id == id.as_str().unwrap())
                .unwrap()
        ),
        "file_missing"
    );
    let snap = modify_for_test(root, json!({"action":"relocate","id":id,"path":dest})).unwrap();
    assert!(snap["models"]
        .as_array()
        .unwrap()
        .iter()
        .any(|m| m["id"] == *id && m["path"].as_str() == dest.to_str()));
    modify_for_test(root, json!({"action":"forget","id":id})).unwrap();
    assert!(dest.is_file());
}

#[test]
fn projector_dimensions_and_stamp_are_checked_before_launch() {
    let dir = Directory::new();
    let root = &dir.0;
    let main = fixture(root, "主.gguf", "llama", 64);
    let wrong = fixture(root, "bad-proj.gguf", "clip", 32);
    let right = fixture(root, "good-proj.gguf", "clip", 64);
    let id = modify_for_test(root, json!({"action":"import","paths":[main]})).unwrap()["models"][0]
        ["id"]
        .clone();
    let cfg = |projector: &Path, ctx: u32| json!({"action":"configure","id":id,"name":"Vision","projector":projector,"context":ctx});
    assert!(modify_for_test(root, cfg(&wrong, 4096)).is_err());
    assert!(modify_for_test(root, cfg(&right, 9000)).is_err());
    let snap = modify_for_test(root, cfg(&right, 4096)).unwrap();
    assert_eq!(snap["models"][0]["name"], "Vision");
    assert_eq!(snap["models"][0]["context"], 4096);
    OpenOptions::new()
        .append(true)
        .open(&right)
        .unwrap()
        .write_all(b"x")
        .unwrap();
    let lib = Library::open(root).unwrap();
    assert_eq!(lib.status(&lib.models[0]), "file_changed");
    fs::remove_file(&right).unwrap();
    assert_eq!(lib.status(&lib.models[0]), "projector_missing");
}

#[test]
fn runtime_library_changes_and_new_siblings_invalidate_execution_consent() {
    let dir = Directory::new();
    let root = &dir.0;
    let file = root.join(runtime::SERVER);
    fs::write(&file, b"test image").unwrap();
    let digest = execution_digest(&file).unwrap();
    fs::write(root.join("docs.txt"), "irrelevant").unwrap();
    assert_eq!(digest, execution_digest(&file).unwrap());
    let library = root.join("ggml.dll");
    fs::write(&library, b"v1").unwrap();
    let next = execution_digest(&file).unwrap();
    assert_ne!(next, digest);
    fs::write(&library, b"v2").unwrap();
    assert_ne!(next, execution_digest(&file).unwrap());
}

#[test]
fn concurrent_imports_are_atomic_and_legacy_records_survive() {
    let dir = Directory::new();
    let root = &dir.0;
    let previous = root.join("models.yaml");
    fs::write(&previous, "legacy settings").unwrap();
    std::thread::scope(|scope| {
        let mut workers = vec![];
        for i in 0..8 {
            let file = fixture(root, &format!("{i}.gguf"), "llama", 32);
            workers.push(
                scope.spawn(move || {
                    modify_for_test(root, json!({"action":"import","paths":[file]}))
                }),
            );
        }
        for worker in workers {
            worker.join().unwrap().unwrap();
        }
    });
    assert_eq!(Library::open(root).unwrap().models.len(), 8);
    assert_eq!(fs::read_to_string(&previous).unwrap(), "legacy settings");
    fs::write(
        root.join(REGISTRY),
        br#"{"version":2,"models":[],"runtimes":[]}"#,
    )
    .unwrap();
    let before = fs::read(root.join(REGISTRY)).unwrap();
    assert!(modify_for_test(root, json!({"action":"forget","id":"absent"})).is_err());
    assert_eq!(fs::read(root.join(REGISTRY)).unwrap(), before);
}

// This test exercises the actual upstream release over HTTPS on the native CI host.
#[cfg(feature = "live-runtime-install")]
#[test]
fn verify_pinned_install_live() {
    let dir = Directory::new();
    // Exercise the installer's native Unicode working directory on every platform.
    let root = &dir.0.join("模型 验收 🪐");
    fs::create_dir(root).unwrap();
    let backend = std::env::var("NOOSPHERE_TEST_RUNTIME_BACKEND").ok();
    let install = json!({"action":"install_runtime","backend":backend});
    let snapshot = modify_for_test(root, install.clone()).unwrap();
    let second = modify_for_test(root, install).unwrap();
    assert_eq!(snapshot["runtimes"], second["runtimes"]);
    let runtime = &snapshot["runtimes"][0];
    println!(
        "pinned runtime version {} devices {} status {}",
        runtime["version"], runtime["device_details"], runtime["status"]
    );
    assert_eq!(runtime["status"], "ready");
    let file = fixture(root, "isolated-fixture.gguf", "llama", 128);
    let model = modify_for_test(root, json!({"action":"import","paths":[file]})).unwrap();
    assert!(resolve(root, model["models"][0]["id"].as_str().unwrap()).is_ok());
    match std::env::var_os("NOOSPHERE_TEST_GGUF") {
        Some(path) => stream_real_model(root, &PathBuf::from(path)),
        None => assert_ne!(
            std::env::var("NOOSPHERE_TEST_RUNTIME_INSTALL").as_deref(),
            Ok("1"),
            "native delivery requires NOOSPHERE_TEST_GGUF"
        ),
    }
}
#[cfg(feature = "live-runtime-install")]
fn stream_real_model(data: &Path, path: &Path) {
    use std::io::{BufRead, BufReader};
    use std::process::{Child, Command, Stdio};
    struct Server(Child);
    impl Drop for Server {
        fn drop(&mut self) {
            let _ = self.0.kill();
            let _ = self.0.wait();
        }
    }
    let snapshot = modify_for_test(data, json!({"action":"import","paths":[path]})).unwrap();
    assert!(
        snapshot["warnings"].as_array().unwrap().is_empty(),
        "{snapshot}"
    );
    let canonical = canonical_local(path).unwrap();
    let model = snapshot["models"]
        .as_array()
        .unwrap()
        .iter()
        .find(|m| m["path"].as_str() == canonical.to_str())
        .unwrap();
    let id = model["id"].as_str().unwrap();
    let spec = resolve(data, id).unwrap();
    let listener = std::net::TcpListener::bind(("127.0.0.1", 0)).unwrap();
    let address = listener.local_addr().unwrap();
    let base = format!("http://{address}/v1");
    let log = data.join("inference.log");
    let writer = File::create(&log).unwrap();
    let mut cmd = Command::new(&spec.executable);
    prepare_runtime_environment(&mut cmd, &spec.executable).unwrap();
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        cmd.creation_flags(0x08000000);
    }
    cmd.current_dir(spec.executable.parent().unwrap())
        .arg("-m")
        .arg(spec.model)
        .arg("--alias")
        .arg(id)
        .args([
            "--host",
            "127.0.0.1",
            "--port",
            &address.port().to_string(),
            "-np",
            "1",
            "-c",
            "512",
            "-t",
            "2",
        ])
        .stdin(Stdio::null())
        .stdout(writer.try_clone().unwrap())
        .stderr(writer);
    let accelerator = std::env::var("NOOSPHERE_TEST_RUNTIME_BACKEND")
        .ok()
        .filter(|s| s != "cpu");
    if accelerator.is_some() {
        let device = snapshot["runtimes"][0]["device_details"][0]["id"]
            .as_str()
            .expect("requested accelerator device");
        cmd.args(["--device", device, "-ngl", "99", "--verbosity", "5"]);
    }
    drop(listener);
    let mut server = Server(cmd.spawn().unwrap());
    let agent = crate::runtime::local_agent();
    let start = Instant::now();
    loop {
        if let Ok(response) = agent
            .get(&format!("{base}/models"))
            .timeout(Duration::from_secs(2))
            .call()
        {
            let inventory: Value = response.into_json().unwrap();
            if inventory["data"]
                .as_array()
                .is_some_and(|v| v.iter().any(|entry| entry["id"] == id))
            {
                break;
            }
        }
        assert!(
            server.0.try_wait().unwrap().is_none() && start.elapsed() < Duration::from_secs(120),
            "expected model alias was not served: {}",
            fs::read_to_string(&log).unwrap_or_default()
        );
        std::thread::sleep(Duration::from_millis(250));
    }
    let response = agent
        .post(&format!("{base}/chat/completions"))
        .timeout(Duration::from_secs(90))
        .send_json(json!({
            "model":id,"messages":[{"role":"user","content":"Say hello in English."}],
            "stream":true,"max_tokens":24
        }))
        .unwrap();
    assert!(response
        .header("Content-Type")
        .unwrap_or("")
        .contains("text/event-stream"));
    let mut content = String::new();
    let mut terminal = false;
    for line in BufReader::new(response.into_reader().take(1024 * 1024)).lines() {
        let line = line.unwrap();
        let Some(payload) = line.strip_prefix("data:") else {
            continue;
        };
        if payload.trim() == "[DONE]" {
            terminal = true;
            break;
        }
        let event: Value = serde_json::from_str(payload.trim()).unwrap();
        assert_eq!(event["model"], id);
        if let Some(token) = event["choices"][0]["delta"]["content"].as_str() {
            content.push_str(token);
        }
    }
    assert!(
        terminal && !content.trim().is_empty(),
        "incomplete generation: {content:?}"
    );
    println!(
        "Real GGUF returned {} output bytes and [DONE].",
        content.len()
    );
    if accelerator.is_some() {
        // Native backend logs may mix Windows code-page paths with ASCII metrics.
        let bytes = fs::read(&log).unwrap();
        let output = String::from_utf8_lossy(&bytes);
        let line = output
            .lines()
            .find(|l| {
                l.split_once("offloaded ")
                    .and_then(|(_, suffix)| suffix.split('/').next()?.parse::<u32>().ok())
                    .is_some_and(|n| n > 0)
            })
            .unwrap_or_else(|| panic!("expected positive accelerator layer offload: {output}"));
        println!("{line}");
    }
    drop(server);
    assert!(std::net::TcpStream::connect_timeout(&address, Duration::from_millis(300)).is_err());
    #[cfg(all(target_os = "linux", target_arch = "x86_64"))]
    crate::local_engines::live_gateway_tests::stream_managed_model(data, id);
    #[cfg(windows)]
    if std::env::var("NOOSPHERE_TEST_MANAGED_GATEWAY").as_deref() == Ok("1") {
        crate::local_engines::live_gateway_tests::stream_managed_model(data, id);
    }
}

#[test]
fn split_model_sizes_and_all_shards_participate_in_readiness() {
    let temp = Directory::new();
    let root = &temp.0;
    let first = fixture(root, "weights-00001-of-00002.gguf", "llama", 16);
    let mut bytes = fs::read(&first).unwrap();
    bytes[16..24].copy_from_slice(&4u64.to_le_bytes());
    bytes.truncate(bytes.len() - 32);
    string(&mut bytes, "split.count");
    bytes.extend(2u32.to_le_bytes());
    bytes.extend(2u16.to_le_bytes());
    bytes.extend([0; 32]);
    fs::write(&first, bytes).unwrap();
    let second = fixture(root, "weights-00002-of-00002.gguf", "llama", 16);
    let snapshot = modify_for_test(root, json!({"action":"import","paths":[first]})).unwrap();
    let total = fs::metadata(&first).unwrap().len() + fs::metadata(&second).unwrap().len();
    assert_eq!(snapshot["models"][0]["size"], total);
    fs::remove_file(&second).unwrap();
    let lib = Library::open(root).unwrap();
    assert_eq!(lib.status(&lib.models[0]), "file_missing");
}
#[test]
fn manual_execution_requires_confirmation_bound_to_program_and_libraries() {
    let temp = Directory::new();
    let root = &temp.0;
    let file = root.join(runtime::SERVER);
    fs::write(&file, b"test-file-must-never-execute").unwrap();
    let change = || Change::AuthorizeRuntime { path: file.clone() };
    assert!(modify(root, change(), None).unwrap_err().contains("确认"));
    let pending = runtime::Consent::inspect(&file).unwrap();
    fs::write(root.join("ggml-vulkan.dll"), b"new code").unwrap();
    assert!(modify(root, change(), Some(pending))
        .unwrap_err()
        .contains("变化"));
    assert!(Library::open(root).unwrap().runtimes.is_empty());
}

#[test]
fn runtime_environment_drops_credentials_and_loader_overrides() {
    let executable = std::env::current_exe().unwrap();
    let mut cmd = std::process::Command::new(&executable);
    for key in [
        "UNLISTED_PROVIDER_CREDENTIAL",
        "OPENAI_API_KEY",
        "LD_PRELOAD",
        "DYLD_INSERT_LIBRARIES",
        "LD_LIBRARY_PATH",
        "DYLD_LIBRARY_PATH",
    ] {
        cmd.env(key, "fixture");
    }
    prepare_runtime_environment(&mut cmd, &executable).unwrap();
    assert_eq!(cmd.get_current_dir(), executable.parent());
    assert!(cmd
        .get_envs()
        .all(|(_, value)| value != Some(std::ffi::OsStr::new("fixture"))));
}
