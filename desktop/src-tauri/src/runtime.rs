//! One owner for backend paths, process lifetime, readiness, and retries.
use crate::commands::types::{ApiError, BackendStatus, HealthResponse};
use std::fs::{self, OpenOptions};
use std::io::{BufRead, BufReader, Write};
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::sync::atomic::{AtomicU16, AtomicU64, AtomicU8, Ordering};
use std::sync::{Arc, Condvar, Mutex};
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};
use tauri::Manager;

static CONNECTION: Mutex<Option<LocalConnection>> = Mutex::new(None);
static INSTANCE_SEQUENCE: AtomicU64 = AtomicU64::new(0);

#[derive(Clone)]
pub(crate) struct LocalConnection {
    pub(crate) base: String,
    pub(crate) token: String,
}

impl LocalConnection {
    pub(crate) fn authorization(&self) -> String {
        format!("Bearer {}", self.token)
    }
}

pub(crate) fn backend_connection() -> Result<LocalConnection, ApiError> {
    CONNECTION.lock().unwrap_or_else(|e| e.into_inner()).clone()
        .ok_or_else(|| ApiError::new("本应用的后端尚未就绪，请在系统状态中查看启动错误"))
}

pub(crate) fn local_agent() -> ureq::Agent {
    ureq::AgentBuilder::new().redirects(0).try_proxy_from_env(false).build()
}

fn set_connection(connection: Option<LocalConnection>) {
    if let Ok(mut current) = CONNECTION.lock() {
        *current = connection;
    }
}

#[derive(Clone, Debug)]
pub(crate) struct RuntimePaths {
    pub(crate) backend: PathBuf,
    codelens: Option<PathBuf>,
    python: Option<PathBuf>,
    rea: Option<PathBuf>,
    pub(crate) data_dir: PathBuf,
    pub(crate) config: PathBuf,
}

impl RuntimePaths {
    pub(crate) fn from_app(app: &tauri::AppHandle) -> Result<Self, String> {
        let exe = std::env::current_exe().map_err(|e| format!("定位应用程序失败: {e}"))?;
        let exe_dir = exe.parent().ok_or("应用程序路径没有父目录")?;
        let mut app_data = app.path().app_local_data_dir().map_err(|e| e.to_string())?;
        if cfg!(debug_assertions) {
            app_data = app_data.join("dev");
        }
        let override_dir = std::env::var_os("NOOSPHERE_DATA_DIR").map(PathBuf::from);
        let dev_dir = cfg!(debug_assertions).then(|| PathBuf::from(env!("CARGO_MANIFEST_DIR")));
        Self::resolve(
            exe_dir,
            app.path().resource_dir().ok().as_deref(),
            &app_data,
            override_dir.as_deref(),
            dev_dir.as_deref(),
        )
    }

    fn resolve(
        exe_dir: &Path,
        resource_dir: Option<&Path>,
        app_data: &Path,
        override_dir: Option<&Path>,
        dev_dir: Option<&Path>,
    ) -> Result<Self, String> {
        let mut roots = vec![exe_dir.to_owned()];
        for root in [resource_dir, dev_dir].into_iter().flatten() {
            if !roots.iter().any(|existing| existing == root) {
                roots.push(root.to_owned());
            }
        }
        let backend = find_resource(
            &roots,
            &["bin/noosphere-backend.exe", "noosphere-backend.exe"],
        )
        .ok_or("未找到 bin/noosphere-backend.exe，请使用完整安装包或便携包")?;
        let codelens = find_resource(&roots, &["codelens-python/main.py"]);
        let python = find_resource(&roots, &["codelens-runtime/python.exe"]);
        let rea = find_resource(&roots, &["rea-runtime/node.exe"])
            .and_then(|node| node.parent().map(Path::to_path_buf));
        let data_dir = choose_data_dir(exe_dir, app_data, override_dir)?;
        fs::create_dir_all(&data_dir)
            .map_err(|e| format!("无法创建数据目录 {}: {e}", data_dir.display()))?;
        let config = data_dir.join("config.yaml");
        if !config.is_file() {
            let defaults = find_resource(&roots, &["config.defaults.yaml"])
                .ok_or("首次启动需要 config.defaults.yaml；没有修改已有配置或模型数据")?;
            copy_initial_config(&defaults, &config)?;
        }
        Ok(Self {
            backend,
            codelens,
            python,
            rea,
            data_dir,
            config,
        })
    }
}

fn choose_data_dir(
    exe_dir: &Path,
    app_data: &Path,
    override_dir: Option<&Path>,
) -> Result<PathBuf, String> {
    let chosen = if let Some(path) = override_dir {
        if !path.is_absolute() {
            return Err("NOOSPHERE_DATA_DIR 必须是绝对路径".into());
        }
        path
    } else if exe_dir.join("config.yaml").is_file() {
        exe_dir
    } else {
        app_data
    };
    if !chosen.is_absolute() {
        return Err("数据目录必须是绝对路径".into());
    }
    Ok(chosen.to_owned())
}

fn find_resource(roots: &[PathBuf], candidates: &[&str]) -> Option<PathBuf> {
    roots
        .iter()
        .flat_map(|root| candidates.iter().map(move |name| root.join(name)))
        .find(|path| path.is_absolute() && path.is_file())
}

fn copy_initial_config(source: &Path, destination: &Path) -> Result<(), String> {
    // create_new makes first-run initialization safe against concurrent starts;
    // existing user config, models, data and logs are never read or replaced.
    let mut output = match OpenOptions::new()
        .write(true)
        .create_new(true)
        .open(destination)
    {
        Ok(file) => file,
        Err(error) if error.kind() == std::io::ErrorKind::AlreadyExists => return Ok(()),
        Err(error) => return Err(format!("无法初始化配置 {}: {error}", destination.display())),
    };
    let result = fs::File::open(source)
        .and_then(|mut input| std::io::copy(&mut input, &mut output))
        .and_then(|_| output.sync_all());
    if let Err(error) = result {
        drop(output);
        let _ = fs::remove_file(destination); // Only the new file created above.
        return Err(format!("初始化默认配置失败: {error}"));
    }
    Ok(())
}

fn instance_id() -> String {
    let nanos = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_nanos();
    let sequence = INSTANCE_SEQUENCE.fetch_add(1, Ordering::Relaxed);
    format!("{}-{nanos:x}-{sequence:x}", std::process::id())
}

trait ManagedProcess: Send {
    fn exit_status(&mut self) -> Result<Option<String>, String>;
    fn connection(&self) -> Option<LocalConnection>;
    fn graceful_stop(&mut self) -> bool;
    fn upgrade_phase(&self) -> u8 {
        0
    }
}

trait Host {
    type Process: ManagedProcess;
    fn spawn(&mut self, paths: &RuntimePaths, instance: &str) -> Result<Self::Process, String>;
    fn health(&mut self, connection: &LocalConnection, expected_instance: &str) -> Health;
}

enum Health {
    Ready,
    Foreign,
    Unavailable(String),
}

struct Lifecycle<P: ManagedProcess> {
    paths: Option<RuntimePaths>,
    process: Option<P>,
    ready: bool,
    error: Option<String>,
    stopping: bool,
    worker_started: bool,
    attempts: u32,
    next_check: Instant,
    started_at: Instant,
    instance: String,
    was_ready: bool,
    failed_checks: u32,
    upgrade_finished_at: Option<Instant>,
}

impl<P: ManagedProcess> Default for Lifecycle<P> {
    fn default() -> Self {
        let now = Instant::now();
        Self {
            paths: None,
            process: None,
            ready: false,
            error: None,
            stopping: false,
            worker_started: false,
            attempts: 0,
            next_check: now,
            started_at: now,
            instance: String::new(),
            was_ready: false,
            failed_checks: 0,
            upgrade_finished_at: None,
        }
    }
}

impl<P: ManagedProcess> Lifecycle<P> {
    fn status(&self) -> BackendStatus {
        BackendStatus {
            running: self.ready,
            restarts: self.attempts.saturating_sub(1),
            error: self.error.clone(),
            data_dir: self
                .paths
                .as_ref()
                .map(|p| p.data_dir.display().to_string()),
        }
    }

    fn request_start(&mut self) -> Result<String, String> {
        if self.stopping {
            return Err("应用正在关闭，不会重新启动后端".into());
        }
        if self.paths.is_none() {
            return Err(self.error.clone().unwrap_or("后端资源尚未初始化".into()));
        }
        if self.ready {
            return Ok("already_running".into());
        }
        self.next_check = Instant::now();
        Ok("starting".into())
    }

    fn shutdown(&mut self) -> Option<P> {
        self.stopping = true;
        self.ready = false;
        self.process.take()
    }

    fn fail(&mut self, error: String, now: Instant) {
        self.ready = false;
        self.error = Some(error);
        self.process.take();
        self.next_check = now + Duration::from_secs(3);
    }

    fn tick<H: Host<Process = P>>(&mut self, host: &mut H, now: Instant) {
        if self.stopping || now < self.next_check {
            return;
        }
        let Some(paths) = self.paths.as_ref() else {
            return;
        };
        if self.process.is_none() {
            self.instance = instance_id();
            self.attempts += 1;
            match host.spawn(paths, &self.instance) {
                Ok(process) => self.process = Some(process),
                Err(error) => {
                    self.fail(format!("启动后端失败: {error}"), now);
                    return;
                }
            }
            self.started_at = now;
            self.was_ready = false;
            self.failed_checks = 0;
            self.upgrade_finished_at = None;
            self.error = None;
        }
        match self.process.as_mut().unwrap().exit_status() {
            Ok(Some(status)) => {
                self.fail(format!("后端已退出 ({status})，正在重试"), now);
                return;
            }
            Err(error) => {
                self.fail(format!("无法检查后端进程: {error}"), now);
                return;
            }
            Ok(None) => {}
        }
        let process = self.process.as_ref().unwrap();
        let upgrade_phase = process.upgrade_phase();
        if upgrade_phase == 2 && self.upgrade_finished_at.is_none() {
            self.upgrade_finished_at = Some(now);
        }
        let health = match process.connection() {
            Some(connection) => host.health(&connection, &self.instance),
            None => Health::Unavailable("后端尚未报告监听地址".into()),
        };
        match health {
            Health::Ready => {
                self.ready = true;
                self.was_ready = true;
                self.failed_checks = 0;
                self.error = None;
                self.next_check = now + Duration::from_secs(5);
            }
            Health::Foreign => self.fail(
                "本地服务身份不匹配，正在重新启动本应用的后端".into(),
                now,
            ),
            Health::Unavailable(error) => {
                self.ready = false;
                self.failed_checks += 1;
                let startup_timeout = if upgrade_phase == 1 { 900 } else { 90 };
                if (!self.was_ready
                    && now.duration_since(self.upgrade_finished_at.unwrap_or(self.started_at))
                        >= Duration::from_secs(startup_timeout))
                    || (self.was_ready && self.failed_checks >= 3)
                {
                    self.fail(format!("后端未能就绪，正在重试: {error}"), now);
                } else {
                    self.error = Some(if upgrade_phase == 1 {
                        "正在备份现有数据，完成后将启动本地服务".into()
                    } else {
                        format!("正在等待后端启动: {error}")
                    });
                    self.next_check = now + Duration::from_secs(1);
                }
            }
        }
    }
}

#[derive(Clone, Default)]
pub struct BackendService {
    state: Arc<(Mutex<Lifecycle<OwnedProcess>>, Condvar)>,
}

impl BackendService {
    pub(crate) fn initialize(&self, paths: Result<RuntimePaths, String>) {
        let (mutex, _) = &*self.state;
        let mut state = mutex.lock().unwrap_or_else(|e| e.into_inner());
        if state.worker_started || state.stopping {
            return;
        }
        match paths {
            Ok(paths) => state.paths = Some(paths),
            Err(error) => {
                state.error = Some(error);
                return;
            }
        }
        state.worker_started = true;
        let shared = self.state.clone();
        std::thread::spawn(move || {
            let mut host = RealHost;
            let (mutex, wake) = &*shared;
            let mut state = mutex.lock().unwrap_or_else(|e| e.into_inner());
            loop {
                if state.stopping {
                    break;
                }
                state.tick(&mut host, Instant::now());
                set_connection(if state.ready {
                    state.process.as_ref().and_then(ManagedProcess::connection)
                } else {
                    None
                });
                let delay = state
                    .next_check
                    .saturating_duration_since(Instant::now())
                    .max(Duration::from_millis(50));
                state = wake
                    .wait_timeout(state, delay)
                    .unwrap_or_else(|e| e.into_inner())
                    .0;
            }
        });
    }

    pub(crate) fn request_start(&self) -> Result<String, String> {
        let (mutex, wake) = &*self.state;
        let result = mutex
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .request_start();
        wake.notify_one();
        result
    }

    pub(crate) fn status(&self) -> BackendStatus {
        self.state
            .0
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .status()
    }

    pub(crate) fn shutdown(&self) -> bool {
        let (mutex, wake) = &*self.state;
        let mut process = mutex.lock().unwrap_or_else(|e| e.into_inner()).shutdown();
        set_connection(None);
        wake.notify_all();
        process.as_mut().map_or(true, ManagedProcess::graceful_stop)
    }
}

struct RealHost;

impl Host for RealHost {
    type Process = OwnedProcess;
    fn spawn(&mut self, paths: &RuntimePaths, instance: &str) -> Result<OwnedProcess, String> {
        let token = crate::local_auth::new_token()?;
        let log_path = paths.data_dir.join("desktop-services.log");
        let log = OpenOptions::new().create(true).append(true).open(&log_path)
            .map_err(|e| format!("无法打开服务日志 {}: {e}", log_path.display()))?;
        let stderr = log.try_clone().map_err(|e| e.to_string())?;
        let mut command = Command::new(&paths.backend);
        command
            .arg("-config")
            .arg(&paths.config)
            .current_dir(&paths.data_dir)
            .env("NOOSPHERE_DESKTOP_MANAGED", "1")
            .env("NOOSPHERE_DESKTOP_DEVELOPMENT", if cfg!(debug_assertions) { "1" } else { "0" })
            .env_remove("NOOSPHERE_CODELENS_SCRIPT")
            .env_remove("NOOSPHERE_CODELENS_PYTHON")
            .env_remove("NOOSPHERE_REA_RUNTIME")
            .env("NOOSPHERE_BACKEND_INSTANCE", instance)
            .env("NOOSPHERE_BACKEND_TOKEN", &token)
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::from(stderr));
        if let Some(script) = &paths.codelens {
            command.env("NOOSPHERE_CODELENS_SCRIPT", script);
        }
        if let Some(python) = &paths.python {
            command.env("NOOSPHERE_CODELENS_PYTHON", python);
        }
        if let Some(rea) = &paths.rea {
            command.env("NOOSPHERE_REA_RUNTIME", rea);
        }
        #[cfg(windows)]
        {
            use std::os::windows::process::CommandExt;
            command.creation_flags(0x08000000); // CREATE_NO_WINDOW
        }
        let mut child = command
            .spawn()
            .map_err(|e| format!("{}: {e}", paths.backend.display()))?;
        #[cfg(windows)]
        let job = match ProcessJob::attach(&child) {
            Ok(job) => job,
            Err(error) => {
                let _ = child.kill();
                let _ = child.wait();
                return Err(format!("创建后端进程保护失败: {error}"));
            }
        };
        let port = Arc::new(AtomicU16::new(0));
        let upgrade = Arc::new(AtomicU8::new(0));
        let stdout = child.stdout.take().expect("stdout was piped");
        let reader_port = port.clone();
        let reader_upgrade = upgrade.clone();
        std::thread::spawn(move || read_backend_output(stdout, log, reader_port, reader_upgrade));
        let mut process = OwnedProcess {
            child,
            port,
            upgrade,
            token,
            #[cfg(windows)]
            job: Some(job),
        };
        process.child.stdin.as_mut().expect("stdin was piped").write_all(b"start\n")
            .map_err(|e| format!("向后端发送启动指令失败: {e}"))?;
        Ok(process)
    }

    fn health(&mut self, connection: &LocalConnection, expected_instance: &str) -> Health {
        let response = local_agent().get(&format!("{}/health", connection.base))
            .set("Authorization", &connection.authorization())
            .timeout(Duration::from_secs(2))
            .call();
        let result = response.map_err(|e| e.to_string()).and_then(|response| {
            response
                .into_json::<HealthResponse>()
                .map_err(|e| e.to_string())
        });
        match result {
            Ok(health) if health.instance_id.as_deref() == Some(expected_instance) => Health::Ready,
            Ok(_) => Health::Foreign,
            Err(error) => Health::Unavailable(error),
        }
    }
}

fn read_backend_output(stdout: impl std::io::Read, mut log: fs::File, port: Arc<AtomicU16>, upgrade: Arc<AtomicU8>) {
    for line in BufReader::new(stdout).lines().map_while(Result::ok) {
        match line.as_str() {
            "NOOSPHERE_UPGRADE_START" => {
                let _ = upgrade.compare_exchange(0, 1, Ordering::Release, Ordering::Relaxed);
                continue;
            }
            "NOOSPHERE_UPGRADE_DONE" => {
                let _ = upgrade.compare_exchange(1, 2, Ordering::Release, Ordering::Relaxed);
                continue;
            }
            _ => {}
        }
        if let Some(value) = line.strip_prefix("NOOSPHERE_READY ") {
            if let Ok(value) = value.parse::<u16>() {
                if value != 0 && port.compare_exchange(0, value, Ordering::Release, Ordering::Relaxed).is_ok() {
                    continue;
                }
            }
        }
        let _ = writeln!(log, "{line}");
    }
}

struct OwnedProcess {
    child: Child,
    port: Arc<AtomicU16>,
    upgrade: Arc<AtomicU8>,
    token: String,
    #[cfg(windows)]
    job: Option<ProcessJob>,
}

impl ManagedProcess for OwnedProcess {
    fn exit_status(&mut self) -> Result<Option<String>, String> {
        self.child
            .try_wait()
            .map(|status| status.map(|status| status.to_string()))
            .map_err(|e| e.to_string())
    }

    fn connection(&self) -> Option<LocalConnection> {
        let port = self.port.load(Ordering::Acquire);
        (port != 0).then(|| LocalConnection {
            base: format!("http://127.0.0.1:{port}"),
            token: self.token.clone(),
        })
    }

    fn graceful_stop(&mut self) -> bool {
        self.child.stdin.take();
        let deadline = Instant::now() + Duration::from_secs(12);
        while Instant::now() < deadline {
            match self.child.try_wait() {
                Ok(Some(status)) => return status.success(),
                Ok(None) => std::thread::sleep(Duration::from_millis(50)),
                Err(_) => return false,
            }
        }
        false // Drop still reclaims the entire process tree.
    }

    fn upgrade_phase(&self) -> u8 {
        self.upgrade.load(Ordering::Acquire)
    }
}

impl Drop for OwnedProcess {
    fn drop(&mut self) {
        #[cfg(windows)]
        self.job.take(); // Closing the job also terminates owned descendants.
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

#[cfg(windows)]
struct ProcessJob {
    _handle: std::os::windows::io::OwnedHandle,
}

#[cfg(windows)]
impl ProcessJob {
    fn attach(child: &Child) -> std::io::Result<Self> {
        use std::os::windows::io::{AsRawHandle, FromRawHandle, OwnedHandle};
        use windows_sys::Win32::System::JobObjects::{
            AssignProcessToJobObject, CreateJobObjectW, JobObjectExtendedLimitInformation,
            SetInformationJobObject, JOBOBJECT_EXTENDED_LIMIT_INFORMATION,
            JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
        };
        // The handle is owned only by the desktop process and is not inherited.
        // The OS closes it even on desktop crashes, terminating the child job.
        unsafe {
            let raw = CreateJobObjectW(std::ptr::null(), std::ptr::null());
            if raw.is_null() {
                return Err(std::io::Error::last_os_error());
            }
            let handle = OwnedHandle::from_raw_handle(raw);
            let mut info: JOBOBJECT_EXTENDED_LIMIT_INFORMATION = std::mem::zeroed();
            info.BasicLimitInformation.LimitFlags = JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE;
            if SetInformationJobObject(
                handle.as_raw_handle(),
                JobObjectExtendedLimitInformation,
                &info as *const _ as *const _,
                std::mem::size_of_val(&info) as u32,
            ) == 0
            {
                return Err(std::io::Error::last_os_error());
            }
            if AssignProcessToJobObject(handle.as_raw_handle(), child.as_raw_handle()) == 0 {
                return Err(std::io::Error::last_os_error());
            }
            Ok(Self { _handle: handle })
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::{AtomicBool, AtomicUsize};

    struct Fixture(PathBuf);
    impl Fixture {
        fn new() -> Self {
            let path = std::env::temp_dir().join(format!("noosphere-path-test-{}", instance_id()));
            fs::create_dir_all(&path).unwrap();
            Self(path)
        }
        fn file(&self, name: &str, text: &str) {
            let path = self.0.join(name);
            fs::create_dir_all(path.parent().unwrap()).unwrap();
            fs::write(path, text).unwrap();
        }
        fn resources(&self) {
            self.file("app/bin/noosphere-backend.exe", "fixture");
            self.file("app/codelens-python/main.py", "fixture");
            self.file("app/config.defaults.yaml", "safe-default");
        }
        fn resolve(&self, override_dir: Option<&Path>) -> Result<RuntimePaths, String> {
            RuntimePaths::resolve(
                &self.0.join("app"),
                None,
                &self.0.join("user"),
                override_dir,
                None,
            )
        }
    }
    impl Drop for Fixture {
        fn drop(&mut self) {
            assert!(self.0.starts_with(std::env::temp_dir()));
            assert!(self
                .0
                .file_name()
                .unwrap()
                .to_string_lossy()
                .starts_with("noosphere-path-test-"));
            let _ = fs::remove_dir_all(&self.0);
        }
    }

    #[test]
    fn portable_keeps_existing_configuration_and_data() {
        let f = Fixture::new();
        f.resources();
        f.file("app/config.yaml", "user-config");
        f.file("app/models.yaml", "fixture-models");
        f.file("app/data/session", "fixture-session");
        let paths = f.resolve(None).unwrap();
        assert_eq!(paths.data_dir, f.0.join("app"));
        assert_eq!(fs::read_to_string(paths.config).unwrap(), "user-config");
        assert_eq!(
            fs::read_to_string(f.0.join("app/models.yaml")).unwrap(),
            "fixture-models"
        );
        assert_eq!(
            fs::read_to_string(f.0.join("app/data/session")).unwrap(),
            "fixture-session"
        );
    }

    #[test]
    fn installed_layout_initializes_writable_data_once() {
        let f = Fixture::new();
        f.resources();
        let paths = f.resolve(None).unwrap();
        assert_eq!(paths.data_dir, f.0.join("user"));
        assert_eq!(fs::read_to_string(&paths.config).unwrap(), "safe-default");
        fs::write(&paths.config, "edited-config").unwrap();
        f.resolve(None).unwrap();
        assert_eq!(fs::read_to_string(&paths.config).unwrap(), "edited-config");
        assert!(!f.0.join("app/config.yaml").exists());
    }

    #[test]
    fn explicit_data_directory_is_absolute_and_isolated() {
        let f = Fixture::new();
        f.resources();
        f.file("app/config.yaml", "portable-config");
        let paths = f.resolve(Some(&f.0.join("isolated data"))).unwrap();
        assert_eq!(paths.data_dir, f.0.join("isolated data"));
        assert_eq!(fs::read_to_string(paths.config).unwrap(), "safe-default");
        assert!(f
            .resolve(Some(Path::new("relative")))
            .unwrap_err()
            .contains("绝对路径"));
    }

    #[test]
    fn missing_resources_are_reported_and_can_be_restored() {
        let f = Fixture::new();
        assert!(f
            .resolve(None)
            .unwrap_err()
            .contains("noosphere-backend.exe"));
        f.resources();
        assert!(f.resolve(None).is_ok());
    }

    #[test]
    fn resource_and_development_roots_work_independently_of_executable_directory() {
        let f = Fixture::new();
        f.resources();
        for (resource, development) in
            [(Some(f.0.join("app")), None), (None, Some(f.0.join("app")))]
        {
            let paths = RuntimePaths::resolve(
                &f.0.join("other/target/debug"),
                resource.as_deref(),
                &f.0.join("user"),
                None,
                development.as_deref(),
            )
            .unwrap();
            assert_eq!(paths.backend, f.0.join("app/bin/noosphere-backend.exe"));
        }
    }

    struct FakeProcess {
        dropped: Arc<AtomicUsize>,
        exited: Arc<AtomicBool>,
        listening: Arc<AtomicBool>,
    }
    impl ManagedProcess for FakeProcess {
        fn exit_status(&mut self) -> Result<Option<String>, String> {
            Ok(self
                .exited
                .load(Ordering::Relaxed)
                .then(|| "test exit".into()))
        }
        fn connection(&self) -> Option<LocalConnection> {
            self.listening.load(Ordering::Relaxed).then(|| LocalConnection {
                base: "http://127.0.0.1:12345".into(),
                token: "fixture".into(),
            })
        }
        fn graceful_stop(&mut self) -> bool {
            true
        }
    }
    impl Drop for FakeProcess {
        fn drop(&mut self) {
            self.dropped.fetch_add(1, Ordering::Relaxed);
        }
    }
    struct FakeHost {
        listening: Arc<AtomicBool>,
        spawns: usize,
        foreign: bool,
        unavailable: bool,
        dropped: Arc<AtomicUsize>,
        exited: Arc<AtomicBool>,
    }
    impl Default for FakeHost {
        fn default() -> Self {
            Self {
                listening: Arc::new(AtomicBool::new(true)),
                spawns: 0,
                foreign: false,
                unavailable: false,
                dropped: Arc::new(AtomicUsize::new(0)),
                exited: Arc::new(AtomicBool::new(false)),
            }
        }
    }
    impl Host for FakeHost {
        type Process = FakeProcess;
        fn spawn(&mut self, _: &RuntimePaths, _: &str) -> Result<FakeProcess, String> {
            self.spawns += 1;
            self.exited.store(false, Ordering::Relaxed);
            Ok(FakeProcess {
                dropped: self.dropped.clone(),
                exited: self.exited.clone(),
                listening: self.listening.clone(),
            })
        }
        fn health(&mut self, _: &LocalConnection, _: &str) -> Health {
            if self.foreign {
                Health::Foreign
            } else if self.unavailable {
                Health::Unavailable("fixture timeout".into())
            } else {
                Health::Ready
            }
        }
    }
    fn lifecycle() -> Lifecycle<FakeProcess> {
        Lifecycle {
            paths: Some(RuntimePaths {
                backend: PathBuf::new(),
                codelens: None,
                python: None,
                rea: None,
                data_dir: PathBuf::new(),
                config: PathBuf::new(),
            }),
            ..Default::default()
        }
    }

    #[test]
    fn repeated_starts_do_not_duplicate_processes_and_shutdown_is_final() {
        let mut state = lifecycle();
        let mut host = FakeHost::default();
        state.tick(&mut host, Instant::now());
        for _ in 0..10 {
            assert_eq!(state.request_start().unwrap(), "already_running");
        }
        state.tick(&mut host, Instant::now() + Duration::from_secs(20));
        assert_eq!(host.spawns, 1);
        state.shutdown();
        state.shutdown();
        assert_eq!(host.dropped.load(Ordering::Relaxed), 1);
        assert!(state.request_start().is_err());
        state.tick(&mut host, Instant::now() + Duration::from_secs(40));
        assert_eq!(host.spawns, 1);
    }

    #[test]
    fn handshake_is_required_before_readiness_and_does_not_duplicate_spawn() {
        let mut state = lifecycle();
        let mut host = FakeHost {
            listening: Arc::new(AtomicBool::new(false)),
            ..Default::default()
        };
        let now = Instant::now();
        state.tick(&mut host, now);
        assert!(!state.ready);
        assert_eq!(host.spawns, 1);
        assert!(state.error.as_ref().unwrap().contains("监听地址"));
        host.listening.store(true, Ordering::Relaxed);
        state.tick(&mut host, now + Duration::from_secs(4));
        assert!(state.ready);
        assert_eq!(host.spawns, 1);
    }

    #[test]
    fn foreign_health_identity_drops_only_owned_process() {
        let mut state = lifecycle();
        let mut host = FakeHost {
            foreign: true,
            ..Default::default()
        };
        state.tick(&mut host, Instant::now());
        assert!(!state.ready);
        assert!(state.error.as_ref().unwrap().contains("身份不匹配"));
        assert_eq!(host.dropped.load(Ordering::Relaxed), 1);
    }

    #[test]
    fn crashed_child_is_reaped_and_restarted_with_new_identity() {
        let mut state = lifecycle();
        let mut host = FakeHost::default();
        let now = Instant::now();
        state.tick(&mut host, now);
        let identity = state.instance.clone();
        host.exited.store(true, Ordering::Relaxed);
        state.tick(&mut host, now + Duration::from_secs(6));
        assert!(!state.ready);
        assert_eq!(host.dropped.load(Ordering::Relaxed), 1);
        state.tick(&mut host, now + Duration::from_secs(10));
        assert!(state.ready);
        assert_eq!(host.spawns, 2);
        assert_ne!(state.instance, identity);
        assert_eq!(state.status().restarts, 1);
    }

    #[test]
    fn unhealthy_start_has_bounded_wait_and_releases_child() {
        let mut state = lifecycle();
        let mut host = FakeHost {
            unavailable: true,
            ..Default::default()
        };
        let now = Instant::now();
        state.tick(&mut host, now);
        state.tick(&mut host, now + Duration::from_secs(91));
        assert!(!state.ready);
        assert!(state.process.is_none());
        assert_eq!(host.dropped.load(Ordering::Relaxed), 1);
    }
}
