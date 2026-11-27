use super::*;
use crate::model_library::{models, modify_for_test};
use std::cell::Cell;

thread_local! { pub(super) static PORTS: Cell<Option<[u16; 5]>> = const {Cell::new(None)}; }

struct TempDir(PathBuf);
impl Drop for TempDir {
    fn drop(&mut self) {
        let _ = fs::remove_dir_all(&self.0);
    }
}

#[test]
fn target_support_tracks_real_launch_implementations() {
    assert_eq!(EngineId::CustomLlama.port(), 18789);
    assert!(EngineId::CustomLlama.supported());
    for id in [EngineId::Bonsai, EngineId::DeepseekLocal] {
        assert_eq!(id.supported(), cfg!(all(windows, target_arch = "x86_64")));
    }
}

#[test]
fn local_gguf_import_launch_switch_recover_cancel_and_exit() {
    // Exercise production command/lifecycle logic on isolated loopback ports.
    // The fixture implements HTTP and the actual Unix supervisor. It requires
    // only the same Go toolchain used to build the desktop sidecar.
    let sockets = (0..5)
        .map(|_| TcpListener::bind((Ipv4Addr::LOCALHOST, 0)).unwrap())
        .collect::<Vec<_>>();
    let ports: [u16; 5] = sockets
        .iter()
        .map(|s| s.local_addr().unwrap().port())
        .collect::<Vec<_>>()
        .try_into()
        .unwrap();
    PORTS.set(Some(ports));
    drop(sockets);
    let engines = LocalEngines::default();
    let dir = TempDir(std::env::temp_dir().join(crate::local_auth::new_token().unwrap()));
    fs::create_dir_all(&dir.0).unwrap();
    let data = dir.0.join("state");
    let exe = dir
        .0
        .join(format!("llama-server{}", std::env::consts::EXE_SUFFIX));
    let backend = Path::new(env!("CARGO_MANIFEST_DIR")).join("../../backend");
    let go = std::env::var_os("NOOSPHERE_TEST_GO").unwrap_or_else(|| "go".into());
    let build = Command::new(go)
        .current_dir(backend)
        .args(["build", "-mod=readonly", "-o"])
        .arg(&exe)
        .arg("./internal/ownedprocess/_testsupport/llama-server")
        .output()
        .unwrap();
    assert!(
        build.status.success(),
        "{}",
        String::from_utf8_lossy(&build.stderr)
    );
    let mut header = b"GGUF".to_vec();
    header.extend(3u32.to_le_bytes());
    header.extend(1u64.to_le_bytes());
    header.extend(0u64.to_le_bytes());
    header.extend([0; 64]);
    let paths =
        ["first.gguf", "second.gguf", "broken.gguf", "slow.gguf"].map(|name| dir.0.join(name));
    for path in &paths {
        fs::write(path, &header).unwrap();
    }
    modify_for_test(&data, json!({"action":"import","paths":paths})).unwrap();
    let id = EngineId::CustomLlama;
    let call = |action, alias| engines.handle(id, action, &data, None, Some(&exe), alias, None);
    let records = models(&data).unwrap();
    let aliases = paths.map(|path| {
        records
            .iter()
            .find(|m| m.path == canonical_local(&path).unwrap())
            .unwrap()
            .id
            .clone()
    });
    let [a, b, broken, slow] = aliases.each_ref().map(String::as_str);
    assert!(call(EngineAction::Start, Some(a)).is_err());
    assert!(!socket_online(id.address()));
    modify_for_test(&data, json!({"action":"authorize_runtime","path":exe})).unwrap();
    assert!(engines
        .handle(
            id,
            EngineAction::Start,
            &data,
            None,
            Some(&exe),
            Some(a),
            Some("outdated")
        )
        .is_err());
    assert!(!socket_online(id.address()));
    let snapshot = call(EngineAction::Start, Some(a)).unwrap();
    assert!(snapshot.managed && snapshot.available);
    let lease_path = data.join("local-engines/custom-listener.json");
    let lease: Value = serde_json::from_reader(File::open(&lease_path).unwrap()).unwrap();
    assert_eq!(lease["model"], a);
    assert_eq!(lease["vision"], false);
    assert_eq!(lease["desktop_pid"], std::process::id());
    assert!(discovery::listener_matches(
        id.port(),
        lease["pid"].as_u64().unwrap() as u32
    ));
    assert_eq!(
        snapshot
            .models
            .iter()
            .filter(|m| m.loaded == Some(true))
            .count(),
        1
    );
    let stream = local_agent()
        .post(&format!("{}/v1/chat/completions", id.origin()))
        .send_json(json!({"model":a,"stream":true,"messages":[{"role":"user","content":"hello"}]}))
        .unwrap()
        .into_string()
        .unwrap();
    assert!(stream.contains("fixture") && stream.ends_with("data: [DONE]\n\n"));
    assert!(call(EngineAction::Start, Some(b)).is_err());
    let busy = request_lock::reader(&data);
    assert!(call(EngineAction::Switch, Some(b)).is_err());
    assert!(call(EngineAction::Stop, None).is_err());
    assert!(engines.observe(id).models.iter().any(|m| m.id == a));
    drop(busy);
    let switched = call(EngineAction::Switch, Some(b)).unwrap();
    assert!(switched
        .models
        .iter()
        .any(|m| m.id == b && m.loaded == Some(true)));
    modify_for_test(
        &data,
        json!({"action":"configure", "id": b, "name":"Updated profile", "context":1024}),
    )
    .unwrap();
    let changed = engines.snapshot(id, &data, None, None);
    assert_eq!(
        changed
            .models
            .iter()
            .find(|m| m.id == b)
            .unwrap()
            .needs_restart,
        Some(true)
    );
    assert!(call(EngineAction::Start, Some(b)).is_err());
    let applied = call(EngineAction::Switch, Some(b)).unwrap();
    assert_eq!(
        applied
            .models
            .iter()
            .find(|m| m.id == b)
            .unwrap()
            .needs_restart,
        Some(false)
    );
    assert!(call(EngineAction::Switch, Some(broken)).is_err());
    assert!(engines.observe(id).models.iter().any(|m| m.id == b));
    assert!(
        String::from_utf8(log_tail(&id.log(&data).with_extension("failed.log")).unwrap())
            .unwrap()
            .contains("model load failed")
    );
    let _ = local_agent()
        .post(&format!("{}/fixture-exit", id.origin()))
        .call();
    let timeout = Instant::now() + Duration::from_secs(5);
    while engines
        .state()
        .slots
        .get(&id)
        .is_some_and(|slot| slot.process.is_some())
        && Instant::now() < timeout
    {
        engines.recent_exit(id);
        std::thread::sleep(POLL);
    }
    assert_eq!(
        engines.snapshot(id, &data, None, None).code,
        Some("PROCESS_EXITED")
    );
    std::thread::scope(|scope| {
        let startup = scope.spawn(|| {
            PORTS.set(Some(ports));
            call(EngineAction::Start, Some(slow))
        });
        let deadline = Instant::now() + Duration::from_secs(10);
        while !engines
            .state()
            .slots
            .get(&id)
            .is_some_and(|s| s.process.is_some())
        {
            assert!(Instant::now() < deadline, "slow fixture did not start");
            std::thread::sleep(Duration::from_millis(50));
        }
        call(EngineAction::Stop, None).unwrap();
        assert!(startup.join().unwrap().is_err());
    });
    assert!(!socket_online(id.address()));
    let outsider = TcpListener::bind(id.address()).unwrap();
    assert!(call(EngineAction::Start, Some(a)).is_err());
    assert!(call(EngineAction::Stop, None).is_err());
    assert!(socket_online(id.address()));
    drop(outsider);
    call(EngineAction::Start, Some(a)).unwrap();
    engines.shutdown();
    assert!(!lease_path.exists());
    assert!(!socket_online(id.address()));
}
