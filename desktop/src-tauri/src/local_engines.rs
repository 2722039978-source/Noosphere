//! Discovery and ownership for external local model runtimes.
mod discovery;
mod portable;
mod request_lock;
#[cfg(windows)]
mod windows;

use crate::commands::types::ApiError;
use crate::runtime::{local_agent, BackendService, RuntimePaths};
pub(crate) use portable::canonical_local;
use portable::{PortableInstall, PortableProfile, BONSAI, DEEPSEEK};
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use std::collections::{HashMap, HashSet};
use std::fs::{self, File};
use std::io::{Read, Seek, SeekFrom};
use std::net::{Ipv4Addr, SocketAddr, TcpListener, TcpStream};
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::sync::{Arc, Condvar, Mutex, MutexGuard};
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
    CustomLlama,
}

impl EngineId {
    const ALL: [Self; 5] = [
        Self::Ollama,
        Self::LmStudio,
        Self::Bonsai,
        Self::DeepseekLocal,
        Self::CustomLlama,
    ];

    fn id(self) -> &'static str {
        match self {
            Self::Ollama => "ollama",
            Self::LmStudio => "lm_studio",
            Self::Bonsai => "bonsai",
            Self::DeepseekLocal => "deepseek_local",
            Self::CustomLlama => "custom_llama",
        }
    }

    fn name(self) -> &'static str {
        match self {
            Self::Ollama => "Ollama",
            Self::LmStudio => "LM Studio",
            Self::Bonsai => BONSAI.name,
            Self::DeepseekLocal => DEEPSEEK.name,
            Self::CustomLlama => "llama.cpp",
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
        if self == Self::CustomLlama {
            return crate::platform::target().is_ok();
        }
        matches!(self, Self::Ollama | Self::LmStudio) || self.portable().is_some()
    }

    fn loads_weights(self) -> bool {
        self == Self::CustomLlama || self.portable().is_some()
    }

    fn port(self) -> u16 {
        #[cfg(test)]
        if let Some(ports) = lifecycle_tests::PORTS.get() {
            return ports[self as usize];
        }
        match self {
            Self::Ollama => 11434,
            Self::LmStudio => 1234,
            Self::Bonsai => 18787,
            Self::DeepseekLocal => 18788,
            Self::CustomLlama => 18789,
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
    launch_identity: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    needs_restart: Option<bool>,
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
            launch_identity: None,
            needs_restart: None,
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

fn inspect(id: EngineId, alias: Option<&str>) -> Observation {
    if !id.supported() || !socket_online(id.address()) {
        return Observation::stopped();
    }
    if id.loads_weights() {
        let profile = id.portable();
        let expected = profile.map(|profile| profile.alias).or(alias);
        if expected.is_none() {
            return failed_observation(id, RequestFailure::Protocol);
        }
        let health = match loopback_json(id, "/health") {
            Ok(value) if value["status"].as_str() == Some("ok") => true,
            Ok(_) => false,
            Err(problem) => return failed_observation(id, problem),
        };
        if health {
            if let Ok(value) = loopback_json(id, "/v1/models") {
                if let Some(mut model) = model_list(&value, "data").and_then(|models| {
                    models
                        .into_iter()
                        .find(|model| Some(model.id.as_str()) == expected)
                }) {
                    if let Some(profile) = profile {
                        model.name = profile.name.into();
                    }
                    model.loaded = Some(true);
                    let props = loopback_json(id, "/props").ok();
                    model.vision = if profile.is_some() {
                        Some(false)
                    } else {
                        props
                            .as_ref()
                            .and_then(|v| v["modalities"]["vision"].as_bool())
                    };
                    let mut result = Observation::online(vec![model]);
                    result.version = props.and_then(|props| {
                        props["build_info"]
                            .as_str()
                            .filter(|version| version.len() <= 256)
                            .map(str::to_owned)
                    });
                    return result;
                }
                return Observation::issue(
                    "unavailable",
                    "MODEL_ID_MISMATCH",
                    "运行时未报告所选模型 ID。请确认运行时支持 --alias，然后重新启动。",
                );
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
    listener_lease: Option<PathBuf>,
    #[cfg(unix)]
    engine_pid: u32,
    #[cfg(windows)]
    job: Option<crate::runtime::ProcessJob>,
}

impl OwnedEngineProcess {
    fn pid(&self) -> u32 {
        #[cfg(windows)]
        {
            self.child.id()
        }
        #[cfg(unix)]
        {
            self.engine_pid
        }
    }

    fn publish_lease(&mut self, directory: &Path, alias: &str, vision: bool) -> Result<(), String> {
        let path = directory.join("custom-listener.json");
        let tmp = directory.join(format!(".listener-{}.tmp", crate::local_auth::new_token()?));
        let record = json!({"desktop_pid": std::process::id(), "pid": self.pid(), "group_pid": self.child.id(), "model": alias, "vision": vision});
        fs::write(&tmp, record.to_string()).map_err(|e| format!("无法保存监听进程身份：{e}"))?;
        if let Err(e) = fs::rename(&tmp, &path) {
            let _ = fs::remove_file(tmp);
            return Err(format!("无法发布监听进程身份：{e}"));
        }
        self.listener_lease = Some(path);
        Ok(())
    }
}

impl Drop for OwnedEngineProcess {
    fn drop(&mut self) {
        if let Some(path) = self.listener_lease.take() {
            let _ = fs::remove_file(path);
        }
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
    active_model: Option<String>,
    active_identity: Option<String>,
    requested_model: Option<String>,
    cancelled: bool,
}

#[derive(Default)]
struct EngineState {
    slots: HashMap<EngineId, Slot>,
    shutting_down: bool,
    ollama_cache: Option<(Instant, Vec<LocalModel>)>,
}

#[derive(Clone, Default)]
pub struct LocalEngines(Arc<(Mutex<EngineState>, Condvar)>);

struct Operation {
    manager: LocalEngines,
    id: EngineId,
}

impl Drop for Operation {
    fn drop(&mut self) {
        if let Some(slot) = self.manager.state().slots.get_mut(&self.id) {
            slot.operation = None;
            slot.requested_model = None;
            self.manager.0 .1.notify_all();
        }
    }
}

impl LocalEngines {
    fn state(&self) -> MutexGuard<'_, EngineState> {
        self.0 .0.lock().unwrap_or_else(|error| error.into_inner())
    }

    #[cfg(test)]
    fn begin(&self, id: EngineId, action: EngineAction) -> Result<Option<Operation>, ApiError> {
        self.begin_model(id, action, None)
    }

    fn begin_model(
        &self,
        id: EngineId,
        action: EngineAction,
        model: Option<&str>,
    ) -> Result<Option<Operation>, ApiError> {
        let mut state = self.state();
        if action == EngineAction::Stop {
            while let Some(slot) = state.slots.get_mut(&id).filter(|s| s.operation.is_some()) {
                slot.cancelled = true;
                state = self.0 .1.wait(state).unwrap_or_else(|err| err.into_inner());
            }
        }
        if state.shutting_down {
            return Err(ApiError::new("Noosphere 正在退出。"));
        }
        if id.loads_weights()
            && state.slots.iter().any(|(other, slot)| {
                other != &id && other.loads_weights() && slot.operation.is_some()
            })
        {
            return Err(ApiError::new("另一项模型加载或切换正在进行，请稍后重试。"));
        }
        let slot = state.slots.entry(id).or_default();
        if let Some(pending) = slot.operation {
            if action == EngineAction::Start
                && slot.requested_model.as_deref() == model
                && matches!(
                    pending,
                    EngineAction::Start | EngineAction::Restart | EngineAction::Switch
                )
            {
                return Ok(None);
            }
            return Err(ApiError::new("本地引擎正在处理其他操作，完成后可重试。"));
        }
        slot.operation = Some(action);
        slot.cancelled = false;
        slot.requested_model = model.map(str::to_owned);
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
                    slot.active_model = None;
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

    fn tracked_listener_matches(&self, id: EngineId) -> Option<bool> {
        let state = self.state();
        let process = state.slots.get(&id)?.process.as_ref()?;
        let pid = process.pid();
        #[cfg(unix)]
        {
            // The supervisor reports the engine PID. Require it to still
            // belong to the retained process group before trusting the port.
            let group = unsafe { libc::getpgid(pid as libc::pid_t) };
            if pid == 0 || group <= 0 || group as u32 != process.child.id() {
                return Some(false);
            }
        }
        drop(state);
        Some(discovery::listener_matches(id.port(), pid))
    }

    fn owns_listener(&self, id: EngineId) -> bool {
        self.tracked_listener_matches(id) == Some(true)
    }

    fn ollama_inventory(&self) -> Vec<LocalModel> {
        if let Some((_, rows)) = self
            .state()
            .ollama_cache
            .as_ref()
            .filter(|(at, _)| at.elapsed() < Duration::from_secs(30))
        {
            return rows.clone();
        }
        let rows = ollama_disk_models();
        self.state().ollama_cache = Some((Instant::now(), rows.clone()));
        rows
    }

    fn observe(&self, id: EngineId) -> Observation {
        if id == EngineId::CustomLlama && !socket_online(id.address()) {
            return Observation::stopped();
        }
        let owned = id == EngineId::CustomLlama && self.owns_listener(id);
        self.observe_listener(id, owned)
    }

    fn observe_listener(&self, id: EngineId, owned: bool) -> Observation {
        let alias = if id == EngineId::CustomLlama && owned {
            self.state()
                .slots
                .get(&id)
                .and_then(|s| s.active_model.clone())
        } else {
            None
        };
        inspect(id, alias.as_deref())
    }

    fn interrupted(&self, id: EngineId) -> bool {
        let state = self.state();
        state.shutting_down || state.slots.get(&id).is_some_and(|s| s.cancelled)
    }

    fn snapshot(
        &self,
        id: EngineId,
        data: &Path,
        workspace: Option<&Path>,
        selected_directory: Option<&Path>,
    ) -> LocalEngine {
        let install = installation(id, data, selected_directory, workspace);
        let prior_error = self.recent_exit(id);
        let managed = self.owns_listener(id);
        let observed = self.observe_listener(id, managed);
        let records = if id == EngineId::CustomLlama {
            crate::model_library::models(data)
        } else {
            Ok(Vec::new())
        };
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
            can_start: (id == EngineId::Ollama && install.launcher.is_some())
                || install.portable.is_some(),
            managed,
            state: observed.state,
            supported: id.supported(),
            available: observed.state == "online",
            verification: if observed.state == "online" {
                "service_identity"
            } else {
                "not_checked"
            },
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
        if id == EngineId::CustomLlama {
            match records {
                Ok(records) => {
                    engine.installed |= !records.is_empty();
                    engine.can_start = records.iter().any(|record| record.status == "ready");
                    let active_identity = self
                        .state()
                        .slots
                        .get(&id)
                        .and_then(|s| s.active_identity.clone());
                    for record in records {
                        if let Some(row) = engine.models.iter_mut().find(|row| row.id == record.id)
                        {
                            row.name = record.name;
                            row.needs_restart = Some(
                                record.status != "ready"
                                    || active_identity.as_deref()
                                        != Some(record.launch_identity.as_str()),
                            );
                            row.launch_identity = Some(record.launch_identity);
                        } else if let Some(mut row) =
                            LocalModel::new(&record.id, &record.name, Some(false), None)
                        {
                            row.launch_identity = Some(record.launch_identity);
                            engine.models.push(row);
                        }
                    }
                }
                Err(message) => {
                    engine.state = "unavailable";
                    engine.code = Some("LIBRARY_UNAVAILABLE");
                    engine.error = Some(message);
                }
            }
        }
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
            if id == EngineId::Ollama
                && install.detected
                && matches!(engine.state, "stopped" | "starting")
            {
                engine.models = self.ollama_inventory();
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
                engine.error = Some("请在 LM Studio 中管理服务，再回到 Noosphere 连接。".into());
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
        supervisor: Option<&Path>,
        model: Option<&str>,
        expected_identity: Option<&str>,
    ) -> Result<LocalEngine, ApiError> {
        if !id.supported() {
            return Err(ApiError::new("本平台不支持此运行配置。"));
        }
        if id == EngineId::LmStudio {
            return Err(ApiError::new("请在 LM Studio 中管理它的服务。"));
        }
        if id.portable().is_none() && directory.is_some() {
            return Err(ApiError::new("请在模型库中选择独立的运行时。"));
        }
        if id == EngineId::CustomLlama && action != EngineAction::Stop && model.is_none() {
            return Err(ApiError::new("请选择要启动的 GGUF 模型。"));
        }
        let Some(operation) = self.begin_model(id, action, model)? else {
            return Ok(self.snapshot(id, data, None, directory));
        };
        let result = (|| {
            #[cfg(windows)]
            let _portable_lock = id
                .loads_weights()
                .then(windows::PortableLock::acquire)
                .transpose()?;
            if action == EngineAction::Stop {
                let _requests = if id == EngineId::CustomLlama {
                    Some(request_lock::exclusive(data)?)
                } else {
                    None
                };
                return self.stop(id, data);
            }
            self.load(
                id,
                action,
                data,
                directory,
                supervisor,
                model,
                expected_identity,
            )
        })();
        if let Err(error) = &result {
            self.state().slots.entry(id).or_default().error = Some(error.clone());
        }
        drop(operation);
        result.map_err(ApiError::new)?;
        Ok(self.snapshot(id, data, None, directory))
    }

    fn load(
        &self,
        id: EngineId,
        action: EngineAction,
        data: &Path,
        directory: Option<&Path>,
        supervisor: Option<&Path>,
        model: Option<&str>,
        expected_identity: Option<&str>,
    ) -> Result<(), String> {
        self.recent_exit(id);
        let observed = self.observe(id);
        let same_model = id != EngineId::CustomLlama
            || observed.models.iter().any(|m| Some(m.id.as_str()) == model);
        if action != EngineAction::Restart && same_model && observed.state == "online" {
            if id != EngineId::CustomLlama {
                return Ok(());
            }
            let running = self
                .state()
                .slots
                .get(&id)
                .and_then(|s| s.active_identity.clone());
            let unchanged = crate::model_library::models(data)?.iter().any(|record| {
                Some(record.id.as_str()) == model
                    && record.status == "ready"
                    && running.as_deref() == Some(record.launch_identity.as_str())
            });
            if unchanged {
                check_expected_identity(expected_identity, running.as_deref())?;
                return Ok(());
            }
            if action == EngineAction::Start {
                return Err("运行配置已更改。请切换或重新启动以应用设置。".into());
            }
        }
        if observed.state == "unavailable" && !self.owns_listener(id) {
            return Err(observed
                .error
                .unwrap_or_else(|| "本机服务当前不可用。".into()));
        }
        // Preflight the target before releasing an existing model.
        let command = Self::prepare(id, data, directory, supervisor, model)?;
        check_expected_identity(expected_identity, command.1.as_deref())?;
        let mut previous = None;
        for candidate in EngineId::ALL {
            if candidate != id && !(id.loads_weights() && candidate.loads_weights()) {
                continue;
            }
            self.recent_exit(candidate);
            let tracked = self
                .state()
                .slots
                .get(&candidate)
                .is_some_and(|s| s.process.is_some());
            if !tracked && !socket_online(candidate.address()) {
                continue;
            }
            if !tracked || !self.owns_listener(candidate) {
                return Err(format!(
                    "端口 {} 的服务未由 Noosphere 管理。请从原应用停止服务。",
                    candidate.port()
                ));
            }
            if action != EngineAction::Switch
                && !(action == EngineAction::Restart && id == candidate)
            {
                return Err(format!("{} 正在运行，请使用切换模型。", candidate.name()));
            }
            if previous.is_some() {
                return Err("检测到多个本地引擎正在运行，请先手动停止。".into());
            }
            let state = self.state();
            let slot = state
                .slots
                .get(&candidate)
                .ok_or("原模型进程已退出，请刷新后重试。")?;
            previous = Some((
                candidate,
                slot.active_model.clone(),
                slot.active_identity.clone(),
            ));
        }
        if self.interrupted(id) {
            return Err("模型启动已取消。".into());
        }
        // This same OS lock is held shared by each live gateway connection.
        // Retain it through rollback to prevent routing to a half-switched model.
        let _requests = if id == EngineId::CustomLlama
            || previous
                .as_ref()
                .is_some_and(|(old, _, _)| *old == EngineId::CustomLlama)
        {
            Some(request_lock::exclusive(data)?)
        } else {
            None
        };
        if let Some((old, _, _)) = &previous {
            self.stop(*old, data)?;
        }
        if let Err(error) = self.launch(id, data, command, model, id) {
            if let Some((old, alias, identity)) = previous.filter(|_| !self.interrupted(id)) {
                // Reloading the prior alias on the same engine replaces the main log.
                // Preserve the failing target's last 16 KiB before rollback.
                if old == id {
                    if let Ok(tail) = log_tail(&id.log(data)) {
                        let _ = fs::write(id.log(data).with_extension("failed.log"), tail);
                    }
                }
                let restored = Self::prepare(old, data, None, supervisor, alias.as_deref())
                    .and_then(|cmd| {
                        check_expected_identity(identity.as_deref(), cmd.1.as_deref())?;
                        self.launch(old, data, cmd, alias.as_deref(), id)
                    });
                return match restored {
                    Ok(()) => Err(format!("{error} 已恢复原模型服务。")),
                    Err(restore_error) => Err(format!("{error} 恢复原模型失败：{restore_error}")),
                };
            }
            return Err(error);
        }
        Ok(())
    }

    fn prepare(
        id: EngineId,
        data: &Path,
        directory: Option<&Path>,
        supervisor: Option<&Path>,
        model: Option<&str>,
    ) -> Result<(Command, Option<String>), String> {
        #[cfg(windows)]
        let _ = supervisor;
        fs::create_dir_all(data.join("local-engines")).map_err(|_| "无法创建本地引擎日志目录。")?;
        #[cfg(windows)]
        if id.loads_weights() && windows::low_available_memory() {
            return Err("可用内存低于 512 MiB，请先释放内存再启动模型。".into());
        }
        if id == EngineId::CustomLlama {
            let alias = model.ok_or("请选择要启动的模型。")?;
            let spec = crate::model_library::resolve(data, alias)?;
            let identity = spec.launch_identity.clone();
            return Ok((
                custom_command(spec, alias, id.port(), supervisor)?,
                Some(identity),
            ));
        }
        let install = installation(id, data, directory, None);
        if let Some(profile) = id.portable() {
            let portable = install
                .portable
                .ok_or("未发现已校验的本地权重和 SYCL 引擎，请选择完整的便携模型目录。")?;
            #[cfg(windows)]
            portable.authorize(data, id.id())?;
            let command = portable.command(profile, id.port(), data)?;
            portable.remember(data, id.id())?;
            return Ok((command, None));
        }
        let exe = install.launcher.ok_or("未发现 Ollama，请安装后刷新。")?;
        #[cfg(windows)]
        let mut command = Command::new(exe);
        #[cfg(windows)]
        command.arg("serve");
        #[cfg(unix)]
        let mut command = {
            let backend = supervisor.ok_or("应用内的本地引擎进程管理组件不可用，请检查安装包。")?;
            let mut command = Command::new(backend);
            command.arg("--noosphere-owned-ollama").arg(exe);
            command
        };
        command
            .current_dir(data)
            .env("OLLAMA_HOST", "127.0.0.1:11434")
            .env("OLLAMA_NO_CLOUD", "1")
            .env_remove("OLLAMA_DEBUG")
            .env_remove("OLLAMA_DEBUG_LOG_REQUESTS");
        Ok((command, None))
    }

    fn launch(
        &self,
        id: EngineId,
        data: &Path,
        prepared: (Command, Option<String>),
        model: Option<&str>,
        operation_id: EngineId,
    ) -> Result<(), String> {
        if self.interrupted(operation_id) {
            return Err("模型启动已取消。".into());
        }
        if TcpListener::bind(id.address()).is_err() {
            return Err(format!(
                "本机端口 {} 已被占用，请检查正在使用它的服务。",
                id.port()
            ));
        }
        if id == EngineId::CustomLlama {
            if let Err(err) = fs::remove_file(data.join("local-engines/custom-listener.json")) {
                if err.kind() != std::io::ErrorKind::NotFound {
                    return Err(format!("无法清理旧的模型进程身份：{err}"));
                }
            }
        }
        let (command, identity) = prepared;
        let process = spawn_owned(command, &id.log(data))?;
        {
            let mut state = self.state();
            if state.shutting_down {
                return Err("Noosphere 正在退出。".into());
            }
            let slot = state.slots.entry(id).or_default();
            slot.active_model = model.map(str::to_owned);
            slot.active_identity = identity;
            slot.error = None;
            slot.process = Some(process);
        }
        let ready = self.wait_ready(id, operation_id).and_then(|observed| {
            if id != EngineId::CustomLlama {
                return Ok(());
            }
            let mut state = self.state();
            let process = state
                .slots
                .get_mut(&id)
                .and_then(|slot| slot.process.as_mut())
                .ok_or("模型进程已结束。")?;
            let alias = model.ok_or("缺少模型 ID。")?;
            let vision = observed
                .models
                .iter()
                .any(|m| m.id == alias && m.loaded == Some(true) && m.vision == Some(true));
            process.publish_lease(&data.join("local-engines"), alias, vision)
        });
        if let Err(error) = ready {
            let process = self.state().slots.entry(id).or_default().process.take();
            drop(process);
            return Err(explain_start_error(&id.log(data), error));
        }
        Ok(())
    }

    fn wait_ready(&self, id: EngineId, operation_id: EngineId) -> Result<Observation, String> {
        let timeout = if id.loads_weights() { 300 } else { 45 };
        let deadline = Instant::now() + Duration::from_secs(timeout);
        #[cfg(windows)]
        let mut low_ram_samples = 0;
        while Instant::now() < deadline {
            if self.interrupted(operation_id) {
                return Err("模型启动已取消。".into());
            }
            if let Some(error) = self.recent_exit(id) {
                return Err(error);
            }
            #[cfg(windows)]
            if id.loads_weights()
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
            let observed = self.observe(id);
            if observed.state == "online" {
                if self.tracked_listener_matches(id) != Some(true) {
                    return Err("此端口的就绪响应来自其他进程，启动已中止。".into());
                }
                return Ok(observed);
            }
            if matches!(observed.code, Some("AUTH_REQUIRED" | "MODEL_ID_MISMATCH")) {
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
        let mut state = self.state();
        let slot = state.slots.entry(id).or_default();
        slot.error = None;
        slot.active_model = None;
        slot.active_identity = None;
        Ok(())
    }

    pub(crate) fn shutdown(&self) {
        let slots = {
            let mut state = self.state();
            state.shutting_down = true;
            std::mem::take(&mut state.slots)
        };
        self.0 .1.notify_all();
        drop(slots); // Each retained job reclaims the process started by this app.
    }
}

fn log_tail(path: &Path) -> std::io::Result<Vec<u8>> {
    let mut file = File::open(path)?;
    const LIMIT: u64 = 16 * 1024;
    file.seek(SeekFrom::Start(
        file.metadata()?.len().saturating_sub(LIMIT),
    ))?;
    let mut bytes = Vec::new();
    file.take(LIMIT).read_to_end(&mut bytes)?;
    Ok(bytes)
}

fn explain_start_error(path: &Path, error: String) -> String {
    let Ok(bytes) = log_tail(path) else {
        return error;
    };
    let log = String::from_utf8_lossy(&bytes).to_ascii_lowercase();
    let hint = if ["out of memory", "failed to allocate", "not enough memory"]
        .iter()
        .any(|s| log.contains(s))
    {
        "内存或显存不足。请降低上下文长度或 GPU 卸载层数再试。"
    } else if log.contains("unknown argument") || log.contains("invalid argument:") {
        "当前运行时不支持启动参数。请检查高级设置或更换兼容版本。"
    } else if log.contains("failed to load")
        && (log.contains("mmproj") || log.contains("projector"))
    {
        "视觉投影器加载失败。请检查主模型与投影器是否配套。"
    } else if log.contains("unknown model architecture")
        || log.contains("unsupported model architecture")
    {
        "当前运行时不支持此模型架构。请更新运行时或选择其他模型。"
    } else {
        return error;
    };
    format!("{hint} {error}")
}

fn check_expected_identity(expected: Option<&str>, actual: Option<&str>) -> Result<(), String> {
    if expected.is_some() && expected != actual {
        return Err("模型或运行配置已更改，请刷新模型库并重新确认启动。".into());
    }
    Ok(())
}

fn custom_command(
    spec: crate::model_library::LaunchSpec,
    alias: &str,
    port: u16,
    supervisor: Option<&Path>,
) -> Result<Command, String> {
    #[cfg(windows)]
    let mut command = {
        let _ = supervisor;
        Command::new(&spec.executable)
    };
    #[cfg(unix)]
    let mut command = {
        let backend = supervisor.ok_or("应用内的本地引擎进程管理组件不可用，请检查安装包。")?;
        let mut command = Command::new(backend);
        command.arg("--noosphere-owned-llama").arg(&spec.executable);
        command
    };
    crate::model_library::prepare_runtime_environment(&mut command, &spec.executable)?;
    command
        .arg("-m")
        .arg(&spec.model)
        .arg("--alias")
        .arg(alias)
        .args([
            "--host",
            "127.0.0.1",
            "--port",
            &port.to_string(),
            "-np",
            "1",
            "-c",
            &spec.context.to_string(),
        ]);
    if let Some(layers) = spec.gpu_layers {
        command.arg("-ngl").arg(layers.to_string());
    }
    if let Some(device) = spec.device {
        command.arg("--device").arg(device);
    }
    if let Some(projector) = spec.projector {
        command.arg("--mmproj").arg(projector);
    }
    Ok(command)
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
    {
        crate::platform::own_process_group(&mut command);
        command.stdin(Stdio::piped()).stdout(Stdio::piped());
    }
    let child = command
        .spawn()
        .map_err(|error| format!("启动本地引擎失败：{error}"))?;
    let process = OwnedEngineProcess {
        child,
        listener_lease: None,
        #[cfg(unix)]
        engine_pid: 0,
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
    {
        let mut process = process;
        process.engine_pid = start_unix_engine(&mut process.child)?;
        Ok(process)
    }
}

#[cfg(unix)]
fn start_unix_engine(child: &mut Child) -> Result<u32, String> {
    use std::io::Write;
    use std::os::fd::AsRawFd;

    let control = child.stdin.as_mut().ok_or("缺少本地引擎退出控制管道。")?;
    // Keep the write end in Child. The OS closes it even after a hard exit.
    control
        .write_all(&[1])
        .map_err(|error| format!("无法启动本地引擎管理进程：{error}"))?;
    let mut status = child.stdout.take().ok_or("缺少本地引擎状态管道。")?;
    let mut pipe = libc::pollfd {
        fd: status.as_raw_fd(),
        events: libc::POLLIN,
        revents: 0,
    };
    // The private supervisor acknowledges the PID in one short pipe write.
    if unsafe { libc::poll(&mut pipe, 1, 5000) } <= 0 {
        return Err("本地引擎管理进程未报告状态，请查看日志。".into());
    }
    let mut reply = [0; 32];
    let count = status
        .read(&mut reply)
        .map_err(|error| format!("本地引擎启动状态读取失败：{error}"))?;
    let pid = std::str::from_utf8(&reply[..count])
        .ok()
        .and_then(|reply| reply.strip_suffix('\n'))
        .and_then(|reply| reply.parse::<libc::pid_t>().ok())
        .filter(|pid| *pid > 0)
        .ok_or("本地引擎启动失败，请查看日志。")?;
    Ok(pid as u32)
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

pub(crate) fn data_directory(app: &tauri::AppHandle) -> Result<PathBuf, ApiError> {
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
    model: Option<String>,
    expected_identity: Option<String>,
    state: tauri::State<'_, LocalEngines>,
) -> Result<LocalEngine, ApiError> {
    let state = state.inner().clone();
    let data = data_directory(&app)?;
    #[cfg(unix)]
    let supervisor = RuntimePaths::from_app(&app).ok().map(|paths| paths.backend);
    #[cfg(windows)]
    let supervisor: Option<PathBuf> = None;
    tauri::async_runtime::spawn_blocking(move || {
        state.handle(
            engine_id,
            action,
            &data,
            directory.as_deref().map(Path::new),
            supervisor.as_deref(),
            model.as_deref(),
            expected_identity.as_deref(),
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
        let tail =
            log_tail(&path).map_err(|_| ApiError::new("本地引擎暂时没有可读的启动日志。"))?;
        let mut content = String::from_utf8_lossy(&tail).into_owned();
        if let Ok(failure) = log_tail(&path.with_extension("failed.log")) {
            content.push_str("\n\n上一次切换失败的日志\n");
            content.push_str(&String::from_utf8_lossy(&failure));
        }
        Ok(json!({"path": path, "content": content}))
    })
    .await
    .map_err(|_| ApiError::new("读取引擎日志失败。"))?
}

#[cfg(test)]
mod lifecycle_tests;

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
