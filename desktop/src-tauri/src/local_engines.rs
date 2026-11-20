//! Discovery and ownership for external local model runtimes.
mod portable;
mod discovery;
#[cfg(windows)]
mod windows;

use crate::commands::types::ApiError;
use crate::runtime::{local_agent, BackendService, RuntimePaths};
use portable::{canonical_local, PortableInstall, PortableProfile, BONSAI, DEEPSEEK};
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use std::collections::{HashMap, HashSet};
use std::fs::{self, File};
use std::io::{Read, Seek, SeekFrom};
use std::net::{Ipv4Addr, SocketAddr, TcpListener, TcpStream};
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::sync::{Arc, Mutex, MutexGuard};
use std::time::{Duration, Instant};
use tauri::Manager;

const POLL: Duration = Duration::from_millis(400);
const MAX_MODELS: usize = 256;

#[derive(Clone, Copy, Debug, Deserialize, Serialize, Hash, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum EngineId {
    Ollama,
    LmStudio,
    Bonsai,
    DeepseekLocal,
}

impl EngineId {
    const ALL: [Self; 4] = [
        Self::Ollama,
        Self::LmStudio,
        Self::Bonsai,
        Self::DeepseekLocal,
    ];

    fn id(self) -> &'static str {
        match self {
            Self::Ollama => "ollama",
            Self::LmStudio => "lm_studio",
            Self::Bonsai => "bonsai",
            Self::DeepseekLocal => "deepseek_local",
        }
    }

    fn name(self) -> &'static str {
        match self {
            Self::Ollama => "Ollama",
            Self::LmStudio => "LM Studio",
            Self::Bonsai => BONSAI.name,
            Self::DeepseekLocal => DEEPSEEK.name,
        }
    }

    fn portable(self) -> Option<PortableProfile> {
        if !cfg!(all(windows, target_arch = "x86_64")) {
            return None;
        }
        match self {
            Self::Bonsai => Some(BONSAI),
            Self::DeepseekLocal => Some(DEEPSEEK),
            _ => None,
        }
    }

    fn supported(self) -> bool {
        matches!(self, Self::Ollama | Self::LmStudio) || self.portable().is_some()
    }

    fn other_portable(self) -> Option<Self> {
        match self {
            Self::Bonsai => Some(Self::DeepseekLocal),
            Self::DeepseekLocal => Some(Self::Bonsai),
            _ => None,
        }
    }

    fn port(self) -> u16 {
        match self {
            Self::Ollama => 11434,
            Self::LmStudio => 1234,
            Self::Bonsai => 18787,
            Self::DeepseekLocal => 18788,
        }
    }

    fn origin(self) -> String {
        format!("http://127.0.0.1:{}", self.port())
    }

    fn address(self) -> SocketAddr {
        SocketAddr::from((Ipv4Addr::LOCALHOST, self.port()))
    }

    fn log(self, data: &Path) -> PathBuf {
        data.join("local-engines")
            .join(format!("{}.log", self.id()))
    }
}

#[derive(Clone, Copy, Debug, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum EngineAction {
    Start,
    Stop,
    Restart,
    Switch,
}

#[derive(Clone, Serialize)]
pub struct LocalModel {
    id: String,
    name: String,
    downloaded: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    loaded: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    vision: Option<bool>,
}

impl LocalModel {
    fn new(id: &str, name: &str, loaded: Option<bool>, vision: Option<bool>) -> Option<Self> {
        if id.is_empty() || id.len() > 512 || id.chars().any(char::is_control) {
            return None;
        }
        let name = if name.is_empty() || name.len() > 512 || name.chars().any(char::is_control) {
            id
        } else {
            name
        };
        Some(Self {
            id: id.into(),
            name: name.into(),
            downloaded: true,
            loaded,
            vision,
        })
    }
}

#[derive(Serialize)]
pub struct LocalEngine {
    id: EngineId,
    name: &'static str,
    provider: &'static str,
    base_url: String,
    installed: bool,
    can_start: bool,
    managed: bool,
    state: &'static str,
    supported: bool,
    available: bool,
    verification: &'static str,
    models: Vec<LocalModel>,
    #[serde(skip_serializing_if = "Option::is_none")]
    version: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    code: Option<&'static str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    error: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    log_path: Option<PathBuf>,
    #[serde(skip_serializing_if = "Option::is_none")]
    directory: Option<PathBuf>,
}

#[derive(Default)]
struct Installation {
    launcher: Option<PathBuf>,
    portable: Option<PortableInstall>,
    detected: bool,
}

fn file_at(path: PathBuf) -> Option<PathBuf> {
    canonical_local(&path).filter(|path| path.is_file())
}

fn home_path(relative: &str) -> Option<PathBuf> {
    std::env::var_os(if cfg!(windows) { "USERPROFILE" } else { "HOME" })
        .map(PathBuf::from)
        .filter(|home| home.is_absolute())
        .map(|home| home.join(relative))
}

fn installation(
    id: EngineId,
    data: &Path,
    directory: Option<&Path>,
    workspace: Option<&Path>,
) -> Installation {
    if let Some(profile) = id.portable() {
        let portable = portable::discover(profile, data, id.id(), directory, workspace);
        return Installation {
            detected: portable.is_some(),
            portable,
            launcher: None,
        };
    }
    let (launcher, detected) = discovery::installed(id);
    Installation {
        detected,
        launcher,
        portable: None,
    }
}

fn model_list(value: &Value, key: &str) -> Option<Vec<LocalModel>> {
    let entries = value.get(key)?.as_array()?;
    let mut models = Vec::new();
    let mut seen = HashSet::new();
    for entry in entries.iter().take(MAX_MODELS) {
        let id = match key {
            "models" => {
                // Hosted Ollama models do not count as downloaded local weights.
                if ["remote_host", "remote_model"]
                    .into_iter()
                    .any(|field| entry[field].as_str().is_some_and(|s| !s.is_empty()))
                {
                    continue;
                }
                entry["name"].as_str().or_else(|| entry["model"].as_str())
            }
            _ => entry["id"].as_str(),
        };
        let Some(id) = id else {
            continue;
        };
        let kind = entry["type"].as_str();
        if matches!(kind, Some("embeddings" | "embedding" | "decision"))
            || id.ends_with(":cloud")
            || id.ends_with("-cloud")
            || !seen.insert(id)
        {
            continue;
        }
        let name = entry["display_name"].as_str().unwrap_or(id);
        let loaded = match entry["state"].as_str() {
            Some("loaded") => Some(true),
            Some("not-loaded") => Some(false),
            _ => None,
        };
        models.extend(LocalModel::new(
            id,
            name,
            loaded,
            (kind == Some("vlm")).then_some(true),
        ));
    }
    Some(models)
}

fn lm_v1_list(value: &Value) -> Option<Vec<LocalModel>> {
    let entries = value.get("models")?.as_array()?;
    let mut models = Vec::new();
    for entry in entries.iter().take(MAX_MODELS) {
        let kind = entry["type"].as_str()?;
        let key = entry["key"].as_str()?;
        let instances = entry["loaded_instances"].as_array()?;
        if kind != "llm" {
            continue;
        }
        let name = entry["display_name"].as_str().unwrap_or(key);
        let vision = entry["capabilities"]["vision"].as_bool();
        if instances.is_empty() {
            models.extend(LocalModel::new(key, name, Some(false), vision));
        }
        for instance in instances.iter().take(16) {
            if let Some(id) = instance["id"].as_str() {
                models.extend(LocalModel::new(id, name, Some(true), vision));
            }
        }
    }
    models.truncate(MAX_MODELS);
    Some(models)
}

#[derive(Default)]
struct Observation {
    state: &'static str,
    models: Vec<LocalModel>,
    version: Option<String>,
    code: Option<&'static str>,
    error: Option<String>,
}

impl Observation {
    fn online(models: Vec<LocalModel>) -> Self {
        Self {
            state: "online",
            models,
            ..Self::default()
        }
    }
    fn stopped() -> Self {
        Self {
            state: "stopped",
            ..Self::default()
        }
    }
    fn issue(state: &'static str, code: &'static str, error: impl Into<String>) -> Self {
        Self {
            state,
            code: Some(code),
            error: Some(error.into()),
            ..Self::default()
        }
    }
}

enum RequestFailure {
    Offline,
    Unauthorized,
    Loading,
    Protocol,
}

fn read_json(mut reader: impl Read) -> Option<Value> {
    const LIMIT: u64 = 1024 * 1024;
    let mut body = Vec::new();
    (&mut reader).take(LIMIT + 1).read_to_end(&mut body).ok()?;
    if body.len() > LIMIT as usize {
        return None;
    }
    serde_json::from_slice(&body).ok()
}

fn loopback_json(id: EngineId, path: &str) -> Result<Value, RequestFailure> {
    match local_agent()
        .get(&format!("{}{path}", id.origin()))
        .timeout(Duration::from_millis(1200))
        .call()
    {
        Ok(response) if (200..300).contains(&response.status()) => {
            read_json(response.into_reader()).ok_or(RequestFailure::Protocol)
        }
        Err(ureq::Error::Status(401 | 403, _)) => Err(RequestFailure::Unauthorized),
        Err(ureq::Error::Status(503, response)) => {
            let body = read_json(response.into_reader()).unwrap_or_default();
            let loading = body["status"]
                .as_str()
                .or_else(|| body["error"]["message"].as_str())
                .is_some_and(|status| status.to_ascii_lowercase().contains("loading model"));
            Err(if loading {
                RequestFailure::Loading
            } else {
                RequestFailure::Protocol
            })
        }
        Err(ureq::Error::Transport(_)) => Err(RequestFailure::Offline),
        _ => Err(RequestFailure::Protocol),
    }
}

fn socket_online(address: SocketAddr) -> bool {
    TcpStream::connect_timeout(&address, Duration::from_millis(180)).is_ok()
}

fn failed_observation(id: EngineId, problem: RequestFailure) -> Observation {
    match problem {
        RequestFailure::Unauthorized => Observation::issue(
            "unavailable",
            "AUTH_REQUIRED",
            "本机服务要求 API 令牌。请在模型连接中填写令牌。",
        ),
        RequestFailure::Loading => {
            Observation::issue("starting", "MODEL_LOADING", "本机引擎正在加载模型。")
        }
        _ if !socket_online(id.address()) => Observation::stopped(),
        _ => Observation::issue(
            "unavailable",
            "PORT_IN_USE",
            format!(
                "端口 {} 正被占用，响应与 {} 的接口不符。",
                id.port(),
                id.name()
            ),
        ),
    }
}

fn inspect(id: EngineId) -> Observation {
    if let Some(profile) = id.portable() {
        let health = match loopback_json(id, "/health") {
            Ok(value) if value["status"].as_str() == Some("ok") => true,
            Ok(_) => false,
            Err(problem) => return failed_observation(id, problem),
        };
        if health {
            if let Ok(value) = loopback_json(id, "/v1/models") {
                if let Some(mut model) = model_list(&value, "data")
                    .and_then(|models| models.into_iter().find(|model| model.id == profile.alias))
                {
                    model.name = profile.name.into();
                    model.loaded = Some(true);
                    model.vision = Some(false);
                    let mut result = Observation::online(vec![model]);
                    result.version = loopback_json(id, "/props").ok().and_then(|props| {
                        props["build_info"]
                            .as_str()
                            .filter(|version| version.len() <= 256)
                            .map(str::to_owned)
                    });
                    return result;
                }
            }
        }
        return failed_observation(id, RequestFailure::Protocol);
    }
    match id {
        EngineId::Ollama => {
            let version = match loopback_json(id, "/api/version") {
                Ok(value) => value["version"]
                    .as_str()
                    .filter(|version| !version.is_empty() && version.len() <= 128)
                    .map(str::to_owned),
                Err(problem) => return failed_observation(id, problem),
            };
            if version.is_none() {
                return failed_observation(id, RequestFailure::Protocol);
            }
            let mut observation = match loopback_json(id, "/api/tags") {
                Ok(value) => model_list(&value, "models")
                    .map(Observation::online)
                    .unwrap_or_else(|| failed_observation(id, RequestFailure::Protocol)),
                Err(problem) => failed_observation(id, problem),
            };
            if observation.state == "online" {
                if let Some(rows) = loopback_json(id, "/api/ps")
                    .ok()
                    .and_then(|value| model_list(&value, "models"))
                {
                    let loaded: HashSet<_> = rows.iter().map(|model| model.id.as_str()).collect();
                    for model in &mut observation.models {
                        model.loaded = Some(loaded.contains(model.id.as_str()));
                    }
                }
            }
            observation.version = version;
            observation
        }
        EngineId::LmStudio => {
            match loopback_json(id, "/api/v1/models") {
                Ok(value) => {
                    if let Some(models) = lm_v1_list(&value) {
                        return Observation::online(models);
                    }
                }
                Err(RequestFailure::Unauthorized) => {
                    return failed_observation(id, RequestFailure::Unauthorized)
                }
                Err(RequestFailure::Offline) => {
                    return failed_observation(id, RequestFailure::Offline)
                }
                Err(_) => {}
            }
            match loopback_json(id, "/api/v0/models") {
                Ok(value) => model_list(&value, "data")
                    .map(Observation::online)
                    .unwrap_or_else(|| failed_observation(id, RequestFailure::Protocol)),
                Err(problem) => failed_observation(id, problem),
            }
        }
        _ => Observation::stopped(),
    }
}

struct OwnedEngineProcess {
    child: Child,
    #[cfg(windows)]
    job: Option<crate::runtime::ProcessJob>,
}

impl Drop for OwnedEngineProcess {
    fn drop(&mut self) {
        #[cfg(windows)]
        self.job.take();
        #[cfg(unix)]
        crate::platform::terminate_process_group(&self.child);
        let _ = self.child.kill();
        let _ = self.child.wait();
    }
}

#[derive(Default)]
struct Slot {
    process: Option<OwnedEngineProcess>,
    operation: Option<EngineAction>,
    error: Option<String>,
}

#[derive(Default)]
struct EngineState {
    slots: HashMap<EngineId, Slot>,
    shutting_down: bool,
}

#[derive(Clone, Default)]
pub struct LocalEngines(Arc<Mutex<EngineState>>);

struct Operation {
    manager: LocalEngines,
    id: EngineId,
}

impl Drop for Operation {
    fn drop(&mut self) {
        if let Some(slot) = self.manager.state().slots.get_mut(&self.id) {
            slot.operation = None;
        }
    }
}

impl LocalEngines {
    fn state(&self) -> MutexGuard<'_, EngineState> {
        self.0.lock().unwrap_or_else(|error| error.into_inner())
    }

    fn begin(&self, id: EngineId, action: EngineAction) -> Result<Option<Operation>, ApiError> {
        let mut state = self.state();
        if state.shutting_down {
            return Err(ApiError::new("Noosphere 正在退出。"));
        }
        let slot = state.slots.entry(id).or_default();
        if let Some(pending) = slot.operation {
            if action == EngineAction::Start
                && matches!(pending, EngineAction::Start | EngineAction::Restart | EngineAction::Switch)
            {
                return Ok(None);
            }
            return Err(ApiError::new("本地引擎正在处理其他操作，完成后可重试。"));
        }
        slot.operation = Some(action);
        Ok(Some(Operation {
            manager: self.clone(),
            id,
        }))
    }

    fn recent_exit(&self, id: EngineId) -> Option<String> {
        let mut state = self.state();
        let slot = state.slots.entry(id).or_default();
        if let Some(process) = &mut slot.process {
            match crate::platform::poll_owned_child(&mut process.child) {
                Ok(None) => {}
                result => {
                    slot.process.take();
                    slot.error = Some(match result {
                        Ok(Some(status)) => format!(
                            "{} 进程已退出（{}），可查看日志或重新启动。",
                            id.name(),
                            status
                        ),
                        _ => format!("无法读取 {} 进程状态，请查看日志。", id.name()),
                    });
                }
            }
        }
        slot.error.clone()
    }

    fn process_id(&self, id: EngineId) -> Option<u32> {
        self.state()
            .slots
            .get(&id)
            .and_then(|slot| slot.process.as_ref())
            .map(|process| process.child.id())
    }

    fn owns_listener(&self, id: EngineId) -> bool {
        self.process_id(id).is_some_and(|pid| discovery::listener_matches(id.port(), pid))
    }

    fn snapshot(
        &self,
        id: EngineId,
        data: &Path,
        workspace: Option<&Path>,
        selected_directory: Option<&Path>,
    ) -> LocalEngine {
        let install = installation(id, data, selected_directory, workspace);
        let observed = inspect(id);
        let prior_error = self.recent_exit(id);
        let mut engine = LocalEngine {
            id,
            name: id.name(),
            provider: if id == EngineId::Ollama {
                "ollama"
            } else {
                "openai_compatible"
            },
            base_url: if id == EngineId::Ollama {
                id.origin()
            } else {
                format!("{}/v1", id.origin())
            },
            installed: installed_or_online(&install, &observed),
            can_start: (id == EngineId::Ollama && install.launcher.is_some()) || install.portable.is_some(),
            managed: self.owns_listener(id),
            state: observed.state,
            supported: id.supported(),
            available: observed.state == "online",
            verification: if observed.state == "online" { "service_identity" } else { "not_checked" },
            models: observed.models,
            version: observed.version,
            code: observed.code,
            error: observed.error,
            log_path: id.log(data).is_file().then(|| id.log(data)),
            directory: install
                .portable
                .as_ref()
                .map(|portable| portable.directory.clone()),
        };
        if !engine.supported {
            engine.state = "unavailable";
            engine.code = Some("PLATFORM_UNSUPPORTED");
            engine.error = Some("此便携模型使用 Windows x64 SYCL 配置。".into());
            return engine;
        }
        if engine.state != "online" {
            if let Some(profile) = id.portable().filter(|_| install.portable.is_some()) {
                engine.models.extend(LocalModel::new(
                    profile.alias,
                    profile.name,
                    Some(false),
                    Some(false),
                ));
            }
            if id == EngineId::Ollama && install.detected && matches!(engine.state, "stopped" | "starting") {
                engine.models = ollama_disk_models();
            }
        }
        if engine.state == "stopped" {
            if self
                .state()
                .slots
                .get(&id)
                .and_then(|slot| slot.operation)
                .is_some_and(|action| action != EngineAction::Stop)
            {
                engine.state = "starting";
            } else if let Some(error) = prior_error {
                engine.state = "unavailable";
                engine.code = Some("PROCESS_EXITED");
                engine.error = Some(error);
            } else if !engine.installed {
                engine.state = "missing";
            } else if id == EngineId::LmStudio {
                engine.code = Some("EXTERNAL_SERVICE");
                engine.error =
                    Some("请在 LM Studio 中管理服务，再回到 Noosphere 连接。".into());
            }
        }
        engine
    }

    fn handle(
        &self,
        id: EngineId,
        action: EngineAction,
        data: &Path,
        directory: Option<&Path>,
    ) -> Result<LocalEngine, ApiError> {
        if !id.supported() {
            return Err(ApiError::new("此便携模型配置仅适用于 Windows x64。"));
        }
        if id == EngineId::LmStudio {
            return Err(ApiError::new("请在 LM Studio 中管理它的服务。"));
        }
        let Some(operation) = self.begin(id, action)? else {
            return Ok(self.snapshot(id, data, None, directory));
        };
        let result = (|| {
            if id.portable().is_none() && directory.is_some() {
                return Err("此引擎使用已安装的运行时。".into());
            }
            #[cfg(windows)]
            let _portable_lock = id
                .portable()
                .map(|_| windows::PortableLock::acquire())
                .transpose()?;
            if action == EngineAction::Switch {
                return self.switch_portable(id, data, directory);
            }
            if matches!(action, EngineAction::Stop | EngineAction::Restart) {
                self.stop(id, data)?;
            }
            if matches!(action, EngineAction::Start | EngineAction::Restart) {
                self.start(id, data, directory)?;
            }
            Ok::<(), String>(())
        })();
        drop(operation);
        result.map_err(ApiError::new)?;
        Ok(self.snapshot(id, data, None, directory))
    }

    fn switch_portable(
        &self,
        id: EngineId,
        data: &Path,
        directory: Option<&Path>,
    ) -> Result<(), String> {
        let other = id.other_portable().ok_or("此运行环境不支持便携模型切换。")?;
        let target = inspect(id);
        if target.state != "stopped" || !socket_online(other.address()) {
            return self.start(id, data, directory);
        }
        self.recent_exit(other);
        if !self.owns_listener(other) {
            return Err(format!("请先在原应用中停止 {}，再切换模型。", other.name()));
        }
        if installation(id, data, directory, None).portable.is_none() {
            return Err("未找到所选模型的完整目录，请重新扫描。".into());
        }
        // The caller holds the portable runtime lock across both operations.
        let _stopping = self.begin(other, EngineAction::Stop)
            .map_err(|error| error.message)?
            .ok_or("另一个模型正在切换，请稍后重试。")?;
        self.stop(other, data)?;
        if let Err(error) = self.start(id, data, directory) {
            return match self.start(other, data, None) {
                Ok(()) => Err(format!("{error} 已恢复原模型服务。")),
                Err(_) => Err(format!("{error} 原模型服务也未能恢复，可重新启动。")),
            };
        }
        Ok(())
    }

    fn start(&self, id: EngineId, data: &Path, directory: Option<&Path>) -> Result<(), String> {
        let status = inspect(id);
        match status.state {
            "online" => return Ok(()),
            "starting" => return self.wait_ready(id),
            "unavailable" => {
                return Err(status
                    .error
                    .unwrap_or_else(|| "本机服务当前不可用。".into()))
            }
            _ => {}
        }
        if TcpListener::bind(id.address()).is_err() {
            return Err(format!(
                "本机端口 {} 已被占用，请先检查占用它的服务。",
                id.port()
            ));
        }
        let mut install = installation(id, data, directory, None);
        fs::create_dir_all(data.join("local-engines")).map_err(|_| "无法创建本地引擎日志目录。")?;
        if let Some(slot) = self.state().slots.get_mut(&id) {
            slot.error = None;
        }
        let command = if let Some(profile) = id.portable() {
            if let Some(other) = id.other_portable().filter(|other| socket_online(other.address())) {
                return Err(format!(
                    "另一套便携模型已占用端口 {}。请先在该模型原有入口停止服务，释放显存后再试。",
                    other.port()
                ));
            }
            #[cfg(windows)]
            if windows::low_available_memory() {
                return Err("可用内存低于 512 MiB，请先释放内存再启动模型。".into());
            }
            let portable = install
                .portable
                .take()
                .ok_or("未发现完成校验的本地权重和 SYCL 引擎，请选择完整的便携模型目录。")?;
            let command = portable.command(profile, id.port(), data)?;
            portable.remember(data, id.id())?;
            command
        } else {
            let exe = install.launcher.ok_or("未发现 Ollama，请安装后刷新。")?;
            let mut command = Command::new(exe);
            command
                .arg("serve")
                .current_dir(data)
                .env("OLLAMA_HOST", "127.0.0.1:11434")
                .env("OLLAMA_NO_CLOUD", "1")
                .env_remove("OLLAMA_DEBUG")
                .env_remove("OLLAMA_DEBUG_LOG_REQUESTS");
            command
        };
        let process = spawn_owned(command, &id.log(data))?;
        {
            let mut state = self.state();
            if state.shutting_down {
                return Err("Noosphere 正在退出。".into());
            }
            state.slots.entry(id).or_default().process = Some(process);
        }
        if let Err(error) = self.wait_ready(id) {
            let process = {
                let mut state = self.state();
                let slot = state.slots.entry(id).or_default();
                slot.error = Some(error.clone());
                slot.process.take()
            };
            drop(process);
            return Err(error);
        }
        Ok(())
    }

    fn wait_ready(&self, id: EngineId) -> Result<(), String> {
        let timeout = if id.portable().is_some() { 300 } else { 45 };
        let deadline = Instant::now() + Duration::from_secs(timeout);
        #[cfg(windows)]
        let mut low_ram_samples = 0;
        while Instant::now() < deadline {
            if self.state().shutting_down {
                return Err("Noosphere 正在退出。".into());
            }
            if let Some(error) = self.recent_exit(id) {
                return Err(error);
            }
            #[cfg(windows)]
            if id.portable().is_some()
                && self
                    .state()
                    .slots
                    .get(&id)
                    .is_some_and(|slot| slot.process.is_some())
            {
                low_ram_samples = if windows::low_available_memory() {
                    low_ram_samples + 1
                } else {
                    0
                };
                if low_ram_samples >= 10 {
                    return Err("可用内存持续偏低，已停止本次模型加载。".into());
                }
            }
            let observed = inspect(id);
            if observed.state == "online" {
                if let Some(pid) = self.process_id(id) {
                    if !discovery::listener_matches(id.port(), pid) {
                        return Err("此端口的就绪响应来自其他进程，启动已中止。".into());
                    }
                }
                return Ok(());
            }
            if observed.code == Some("AUTH_REQUIRED") {
                return Err(observed.error.unwrap_or_default());
            }
            std::thread::sleep(POLL);
        }
        Err(format!(
            "{} 未在 {timeout} 秒内报告就绪，请查看启动日志。",
            id.name()
        ))
    }

    fn stop(&self, id: EngineId, _data: &Path) -> Result<(), String> {
        let process = self.state().slots.entry(id).or_default().process.take();
        if process.is_none() && socket_online(id.address()) {
            return Err("此服务由其他应用启动，请从原入口停止它。".into());
        }
        drop(process);
        let deadline = Instant::now() + Duration::from_secs(5);
        while socket_online(id.address()) {
            if Instant::now() >= deadline {
                return Err("服务端口仍在监听，请查看引擎状态。".into());
            }
            std::thread::sleep(POLL);
        }
        self.state().slots.entry(id).or_default().error = None;
        Ok(())
    }

    pub(crate) fn shutdown(&self) {
        let slots = {
            let mut state = self.state();
            state.shutting_down = true;
            std::mem::take(&mut state.slots)
        };
        drop(slots); // Each retained job reclaims the process started by this app.
    }
}

fn installed_or_online(install: &Installation, observed: &Observation) -> bool {
    install.detected || observed.state == "online" || observed.state == "starting"
}

fn configure_process(command: &mut Command, file: File) -> std::io::Result<()> {
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        command.creation_flags(0x08000000 | 0x00004000); // CREATE_NO_WINDOW | BELOW_NORMAL_PRIORITY_CLASS
    }
    command
        .env_remove("NOOSPHERE_BACKEND_TOKEN")
        .stdin(Stdio::null())
        .stderr(file.try_clone()?)
        .stdout(file);
    Ok(())
}

fn spawn_owned(mut command: Command, path: &Path) -> Result<OwnedEngineProcess, String> {
    let file = File::create(path).map_err(|_| "无法打开本地引擎启动日志。")?;
    configure_process(&mut command, file).map_err(|_| "无法配置本地引擎日志。")?;
    #[cfg(unix)]
    crate::platform::own_process_group(&mut command);
    let child = command
        .spawn()
        .map_err(|error| format!("启动本地引擎失败：{error}"))?;
    let process = OwnedEngineProcess {
        child,
        #[cfg(windows)]
        job: None,
    };
    #[cfg(windows)]
    {
        let mut process = process;
        process.job = Some(
            crate::runtime::ProcessJob::attach(&process.child)
                .map_err(|error| format!("无法保护本地引擎进程：{error}"))?,
        );
        return Ok(process);
    }
    #[cfg(unix)]
    Ok(process)
}

fn ollama_disk_models() -> Vec<LocalModel> {
    let root = std::env::var_os("OLLAMA_MODELS")
        .map(PathBuf::from)
        .or_else(|| home_path(".ollama/models"));
    let Some(root) = root.and_then(|path| canonical_local(&path)) else {
        return Vec::new();
    };
    let registry = root.join("manifests/registry.ollama.ai");
    let Some(registry) = canonical_local(&registry).filter(|path| path.starts_with(&root)) else {
        return Vec::new();
    };
    let mut models = Vec::new();
    let mut inspected = 0;
    let Ok(namespaces) = fs::read_dir(&registry) else {
        return models;
    };
    for namespace in namespaces.take(32).flatten() {
        let Some(namespace_dir) =
            canonical_local(&namespace.path()).filter(|path| path.starts_with(&registry))
        else {
            continue;
        };
        let Ok(names) = fs::read_dir(namespace_dir) else {
            continue;
        };
        for name in names.take(64).flatten() {
            let Some(model_dir) =
                canonical_local(&name.path()).filter(|path| path.starts_with(&registry))
            else {
                continue;
            };
            let Ok(tags) = fs::read_dir(model_dir) else {
                continue;
            };
            for tag in tags.take(32).flatten() {
                inspected += 1;
                if inspected > 1024 || models.len() >= MAX_MODELS {
                    return models;
                }
                let ns = namespace.file_name().to_string_lossy().into_owned();
                let model = name.file_name().to_string_lossy().into_owned();
                let tag_name = tag.file_name().to_string_lossy().into_owned();
                if tag_name.ends_with("-cloud") || tag_name == "cloud" {
                    continue;
                }
                let Some(path) =
                    canonical_local(&tag.path()).filter(|path| path.starts_with(&registry))
                else {
                    continue;
                };
                let Some(manifest) = File::open(path).ok().and_then(read_json) else {
                    continue;
                };
                if !valid_ollama_manifest(&manifest, &root) {
                    continue;
                }
                let id = if ns == "library" {
                    format!("{model}:{tag_name}")
                } else {
                    format!("{ns}/{model}:{tag_name}")
                };
                models.extend(LocalModel::new(&id, &id, Some(false), None));
            }
        }
    }
    models
}

fn valid_ollama_manifest(manifest: &Value, root: &Path) -> bool {
    let Some(layers) = manifest["layers"].as_array() else {
        return false;
    };
    let blob_exists = |entry: &Value| {
        let Some(digest) = entry["digest"]
            .as_str()
            .and_then(|digest| digest.strip_prefix("sha256:"))
        else {
            return false;
        };
        if digest.len() != 64 || !digest.bytes().all(|byte| byte.is_ascii_hexdigit()) {
            return false;
        }
        let Some(path) = canonical_local(&root.join("blobs").join(format!("sha256-{digest}")))
            .filter(|path| path.starts_with(root))
        else {
            return false;
        };
        entry["size"].as_u64().is_some_and(|size| {
            path.metadata()
                .is_ok_and(|info| info.is_file() && info.len() == size)
        })
    };
    layers.len() <= 32
        && layers
            .iter()
            .any(|layer| layer["mediaType"].as_str() == Some("application/vnd.ollama.image.model"))
        && blob_exists(&manifest["config"])
        && layers.iter().all(blob_exists)
}

fn data_directory(app: &tauri::AppHandle) -> Result<PathBuf, ApiError> {
    if let Some(data) = app.state::<BackendService>().status().data_dir {
        return Ok(data.into());
    }
    RuntimePaths::from_app(app)
        .map(|paths| paths.data_dir)
        .map_err(ApiError::new)
}

#[tauri::command]
pub async fn list_local_engines(
    app: tauri::AppHandle,
    workspace_path: Option<String>,
    state: tauri::State<'_, LocalEngines>,
) -> Result<Value, ApiError> {
    let state = state.inner().clone();
    let data = data_directory(&app)?;
    tauri::async_runtime::spawn_blocking(move || {
        let workspace = workspace_path.as_deref().map(Path::new);
        let engines = std::thread::scope(|scope| {
            let state = &state;
            let data = &data;
            let workers = EngineId::ALL
                .into_iter()
                .map(|id| scope.spawn(move || state.snapshot(id, data, workspace, None)))
                .collect::<Vec<_>>();
            workers
                .into_iter()
                .map(|worker| {
                    worker
                        .join()
                        .map_err(|_| ApiError::new("本地引擎扫描失败。"))
                })
                .collect::<Result<Vec<_>, _>>()
        })?;
        Ok(json!({"engines": engines}))
    })
    .await
    .map_err(|_| ApiError::new("无法扫描本地模型，请重试。"))?
}

#[tauri::command]
pub async fn control_local_engine(
    app: tauri::AppHandle,
    engine_id: EngineId,
    action: EngineAction,
    directory: Option<String>,
    state: tauri::State<'_, LocalEngines>,
) -> Result<LocalEngine, ApiError> {
    let state = state.inner().clone();
    let data = data_directory(&app)?;
    tauri::async_runtime::spawn_blocking(move || {
        state.handle(
            engine_id,
            action,
            &data,
            directory.as_deref().map(Path::new),
        )
    })
    .await
    .map_err(|_| ApiError::new("本地引擎操作失败，请重试。"))?
}

#[tauri::command]
pub async fn local_engine_log(
    app: tauri::AppHandle,
    engine_id: EngineId,
) -> Result<Value, ApiError> {
    let path = engine_id.log(&data_directory(&app)?);
    tauri::async_runtime::spawn_blocking(move || {
        let mut file = File::open(&path)
            .map_err(|_| ApiError::new("本地引擎暂时没有 Noosphere 启动日志。"))?;
        let length = file
            .metadata()
            .map_err(|_| ApiError::new("无法读取日志大小。"))?
            .len();
        let tail = 16 * 1024;
        file.seek(SeekFrom::Start(length.saturating_sub(tail)))
            .map_err(|_| ApiError::new("无法定位日志结尾。"))?;
        let mut bytes = Vec::new();
        file.take(tail)
            .read_to_end(&mut bytes)
            .map_err(|_| ApiError::new("无法读取引擎日志。"))?;
        Ok(json!({"path": path, "content": String::from_utf8_lossy(&bytes)}))
    })
    .await
    .map_err(|_| ApiError::new("读取引擎日志失败。"))?
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn lm_inventory_retains_exact_loaded_ids_and_distinguishes_disk_models() {
        let rows = lm_v1_list(&json!({"models": [
            {"key":"model-a","type":"llm","display_name":"Model A","loaded_instances":[],"capabilities":{"vision":false}},
            {"key":"model-b","type":"llm","display_name":"Model B","loaded_instances":[{"id":"loaded-b"}],"capabilities":{"vision":true}},
            {"key":"embed","type":"embedding","loaded_instances":[]}
        ]})).unwrap();
        assert_eq!(rows.len(), 2);
        assert_eq!(
            (rows[0].id.as_str(), rows[0].downloaded, rows[0].loaded),
            ("model-a", true, Some(false))
        );
        assert_eq!(
            (rows[1].id.as_str(), rows[1].loaded, rows[1].vision),
            ("loaded-b", Some(true), Some(true))
        );
        assert!(lm_v1_list(&json!({"models":[{"name":"unrelated"}]})).is_none());
    }

    #[test]
    fn ollama_cloud_models_never_appear_as_downloaded_weights() {
        let rows = model_list(
            &json!({"models":[
                {"name":"local-model:latest"},
                {"name":"cloud-model:latest","remote_host":"https://ollama.com"},
                {"name":"explicit-model:4b-cloud"}
            ]}),
            "models",
        )
        .unwrap();
        assert_eq!(rows.len(), 1);
        assert_eq!(rows[0].id, "local-model:latest");
    }

    #[test]
    fn repeated_start_shares_one_operation_and_shutdown_blocks_new_work() {
        let engines = LocalEngines::default();
        let start = engines
            .begin(EngineId::Ollama, EngineAction::Start)
            .unwrap();
        assert!(start.is_some());
        assert!(engines
            .begin(EngineId::Ollama, EngineAction::Start)
            .unwrap()
            .is_none());
        assert!(engines
            .begin(EngineId::Ollama, EngineAction::Restart)
            .is_err());
        drop(start);
        assert!(engines
            .begin(EngineId::Ollama, EngineAction::Restart)
            .unwrap()
            .is_some());
        engines.shutdown();
        assert!(engines
            .begin(EngineId::Ollama, EngineAction::Start)
            .is_err());
    }

    #[cfg(windows)]
    #[test]
    fn listener_identity_uses_the_live_process_and_does_not_survive_close() {
        let socket = TcpListener::bind((Ipv4Addr::LOCALHOST, 0)).unwrap();
        let port = socket.local_addr().unwrap().port();
        assert_eq!(
            windows::ListenerOwner::observe(port).unwrap().pid(),
            std::process::id()
        );
        drop(socket);
        assert!(windows::ListenerOwner::observe(port).is_none());
    }
}
