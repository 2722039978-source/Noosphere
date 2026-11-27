//! User-managed weights and separately authorized llama.cpp runtimes. This registry is
//! independent of gateway credentials, workspace records, and official bundle manifests.
mod gguf;
mod picker;
mod runtime;
#[cfg(test)]
mod tests;
#[tauri::command]
pub async fn pick_model_library_files(
    window: tauri::WebviewWindow,
    kind: picker::Kind,
) -> Result<Vec<String>, ApiError> {
    picker::pick(window, kind).await
}
use crate::commands::types::ApiError;
use crate::local_engines::{canonical_local, data_directory};
use runtime::Runtime;
pub(crate) use runtime::{execution_digest, prepare_runtime_environment};
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use sha2::{Digest, Sha256};
use std::fs::{self, File, OpenOptions};
use std::io::{Read, Write};
use std::path::{Path, PathBuf};
use std::sync::Mutex;
use std::time::{Duration, Instant, UNIX_EPOCH};

struct ModelCache {
    data: PathBuf,
    index: Option<Stamp>,
    observed: Instant,
    models: Vec<ModelRecord>,
}
static MODEL_CACHE: Mutex<Option<ModelCache>> = Mutex::new(None);
static WRITE_LOCK: Mutex<()> = Mutex::new(());
static INSTALL_LOCK: Mutex<()> = Mutex::new(());
const REGISTRY: &str = "model-library.v1.json";
const MAX_RECORDS: usize = 4096;
const MAX_INDEX_BYTES: u64 = 16 * 1024 * 1024;

fn unique_id(prefix: &str) -> Result<String, String> {
    let mut random = [0; 16];
    getrandom::fill(&mut random).map_err(|e| e.to_string())?;
    Ok(format!("{prefix}-{:032x}", u128::from_le_bytes(random)))
}

#[derive(Clone, Copy, Serialize, Deserialize, PartialEq, Eq)]
struct Stamp {
    size: u64,
    modified_ns: u64,
}
impl Stamp {
    fn read(path: &Path) -> Result<Self, String> {
        let m = path
            .metadata()
            .map_err(|e| format!("无法读取 {}：{e}", path.display()))?;
        if !m.is_file() {
            return Err("需要常规文件".into());
        }
        let modified_ns = m
            .modified()
            .ok()
            .and_then(|v| v.duration_since(UNIX_EPOCH).ok())
            .and_then(|v| u64::try_from(v.as_nanos()).ok())
            .ok_or("文件时间不可读取")?;
        Ok(Self {
            size: m.len(),
            modified_ns,
        })
    }
}

#[derive(Clone, Serialize, Deserialize)]
pub(crate) struct ModelRecord {
    pub id: String,
    pub name: String,
    pub path: PathBuf,
    pub projector: Option<PathBuf>,
    projector_stamp: Option<Stamp>,
    pub runtime_id: Option<String>,
    pub context: Option<u32>,
    pub gpu_layers: Option<u32>,
    pub device: Option<String>,
    pub architecture: Option<String>,
    pub trained_context: Option<u32>,
    embedding: Option<u32>,
    #[serde(default)]
    shards: std::collections::BTreeMap<String, Stamp>,
    stamp: Stamp,
    #[serde(skip)]
    pub status: String,
    #[serde(skip)]
    pub launch_identity: String,
}

impl ModelRecord {
    fn size(&self) -> u64 {
        self.shards
            .values()
            .fold(self.stamp.size, |a, s| a.saturating_add(s.size))
    }
    fn shard_status(&self) -> Option<&'static str> {
        for (name, stamp) in &self.shards {
            let path = self.path.with_file_name(name);
            match Stamp::read(&path) {
                Err(_) => return Some("file_missing"),
                Ok(s) if &s != stamp => return Some("file_changed"),
                _ => {}
            }
        }
        None
    }
}
#[derive(Serialize, Deserialize)]
struct Library {
    version: u32,
    models: Vec<ModelRecord>,
    runtimes: Vec<Runtime>,
}
impl Library {
    fn open(data: &Path) -> Result<Self, String> {
        let file = match File::open(data.join(REGISTRY)) {
            Ok(f) => f,
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => {
                return Ok(Self {
                    version: 1,
                    models: vec![],
                    runtimes: vec![],
                })
            }
            Err(e) => return Err(format!("无法读取模型库：{e}")),
        };
        if file.metadata().map_err(|e| e.to_string())?.len() > MAX_INDEX_BYTES {
            return Err("模型注册表超出大小限制".into());
        }
        let lib: Self = serde_json::from_reader(file.take(MAX_INDEX_BYTES))
            .map_err(|e| format!("模型注册表损坏，请从备份恢复：{e}"))?;
        if lib.version != 1 {
            return Err(format!(
                "模型注册表版本 {} 不受支持，请升级 Noosphere",
                lib.version
            ));
        }
        Ok(lib)
    }
    fn save(&self, data: &Path) -> Result<(), String> {
        let temporary = data.join(unique_id(".library")?);
        let bytes = serde_json::to_vec_pretty(self).map_err(|e| e.to_string())?;
        if bytes.len() as u64 > MAX_INDEX_BYTES {
            return Err("模型注册表已满".into());
        }
        let result = (|| {
            let mut writer = OpenOptions::new()
                .create_new(true)
                .write(true)
                .open(&temporary)?;
            writer.write_all(&bytes)?;
            writer.sync_all()?;
            drop(writer);
            fs::rename(&temporary, data.join(REGISTRY))
        })();
        if result.is_err() {
            let _ = fs::remove_file(&temporary);
        }
        result.map_err(|e| format!("模型库保存失败：{e}"))?;
        if let Ok(mut cache) = MODEL_CACHE.lock() {
            *cache = None;
        }
        Ok(())
    }
    fn automatic_runtime(&self) -> Option<&Runtime> {
        self.runtimes
            .iter()
            .rev()
            .filter(|r| r.status() == "ready")
            .max_by_key(|r| {
                let memory = r
                    .device_details
                    .iter()
                    .filter_map(|d| d.free_mib)
                    .max()
                    .unwrap_or(0);
                (!r.devices.is_empty(), memory)
            })
    }
    fn runtime(&self, m: &ModelRecord) -> Option<&Runtime> {
        match &m.runtime_id {
            Some(id) => self.runtimes.iter().find(|r| r.id == *id),
            None => self.automatic_runtime(),
        }
    }
    fn status(&self, m: &ModelRecord) -> &'static str {
        match Stamp::read(&m.path) {
            Err(_) => return "file_missing",
            Ok(s) if s != m.stamp => return "file_changed",
            _ => {}
        }
        if let Some(status) = m.shard_status() {
            return status;
        }
        if let Some(path) = &m.projector {
            match Stamp::read(path) {
                Err(_) => return "projector_missing",
                Ok(s) if Some(s) != m.projector_stamp => return "file_changed",
                _ => {}
            }
        }
        match self.runtime(m).map(Runtime::status) {
            Some("ready") => "ready",
            Some("changed") => "runtime_changed",
            _ => "runtime_missing",
        }
    }
    fn identity(&self, m: &ModelRecord) -> String {
        let runtime = self.runtime(m).map(|r| (&r.id, &r.path, &r.digest));
        let identity = json!([
            m.id,
            m.path,
            m.stamp,
            m.shards,
            m.projector,
            m.projector_stamp,
            runtime,
            m.context,
            m.device,
            m.gpu_layers
        ]);
        format!("{:x}", Sha256::digest(identity.to_string().as_bytes()))
    }
    fn snapshot(&self, warnings: Vec<String>) -> Value {
        let ram = available_ram();
        let models=self.models.iter().map(|m|{
            let runtime=self.runtime(m);
            json!({
                "id": m.id, "name": m.name, "path": m.path, "size": m.size(),
                "architecture": m.architecture, "trained_context": m.trained_context,
                "projector": m.projector, "runtime_id": m.runtime_id,
                "effective_runtime_id": runtime.map(|r| &r.id),
                "context": m.context, "gpu_layers": m.gpu_layers, "device": m.device,
                "launch_identity": self.identity(m), "status": self.status(m),
                "suggested_context": suggested_context(m, ram, runtime), "fit": fit(m, ram, runtime)
            })
        }).collect::<Vec<_>>();
        let runtimes = self
            .runtimes
            .iter()
            .map(|r| {
                json!({
                    "id": r.id, "path": r.path, "version": r.version, "devices": r.devices,
                    "device_details": r.device_details, "status": r.status()
                })
            })
            .collect::<Vec<_>>();
        json!({
            "version": self.version, "models": models, "runtimes": runtimes,
            "candidates": runtime::candidates(), "warnings": warnings,
            "available_ram_bytes": ram, "install_options": runtime::packages()
        })
    }
    fn import(&mut self, path: &Path) -> Result<(), String> {
        let p = gguf_path(path)?;
        let stamp = Stamp::read(&p)?;
        let existing = self.models.iter().position(|m| m.path == p);
        if existing.is_some_and(|i| {
            self.models[i].stamp == stamp && self.models[i].shard_status().is_none()
        }) {
            return Ok(());
        }
        let model = read_model(&p)?;
        if let Some(i) = existing {
            let old = &mut self.models[i];
            if old.architecture != model.architecture
                || old.trained_context != model.trained_context
                || old.embedding != model.embedding
            {
                return Err(
                    "此位置的模型元数据已变化，旧配置已保留。请先移除旧记录，再导入替换模型".into(),
                );
            }
            old.stamp = model.stamp;
            old.shards = model.shards;
        } else {
            if self.models.len() >= MAX_RECORDS {
                return Err("模型记录已满".into());
            }
            self.models.push(model);
        }
        Ok(())
    }
    fn register_runtime(&mut self, mut item: Runtime) -> Result<(), String> {
        if let Some(existing) = self.runtimes.iter_mut().find(|r| r.path == item.path) {
            item.id = existing.id.clone();
            *existing = item;
        } else {
            if self.runtimes.len() >= 128 {
                return Err("运行时记录已满".into());
            }
            self.runtimes.push(item);
        }
        Ok(())
    }
    fn change(&mut self, change: Change) -> Result<Vec<String>, String> {
        let mut warnings = vec![];
        match change {
            Change::Scan { path } => {
                let paths = scan(&path, &mut warnings)?;
                for path in paths {
                    if let Err(e) = self.import(&path) {
                        warnings.push(format!("{}：{e}", path.display()));
                    }
                }
            }
            Change::Import { paths } => {
                if paths.len() > MAX_RECORDS {
                    return Err("一次最多导入 4096 个文件".into());
                }
                for path in paths {
                    if let Err(e) = self.import(&path) {
                        warnings.push(format!("{}：{e}", path.display()));
                    }
                }
            }
            Change::Forget { id } => self.models.retain(|m| m.id != id),
            Change::ForgetRuntime { id } => self.runtimes.retain(|r| r.id != id),
            Change::Relocate { id, path } => {
                let new = read_model(&gguf_path(&path)?)?;
                if self.models.iter().any(|m| m.id != id && m.path == new.path) {
                    return Err("另一个模型记录已使用此文件".into());
                }
                let m = self
                    .models
                    .iter_mut()
                    .find(|m| m.id == id)
                    .ok_or("模型记录不存在")?;
                if m.architecture != new.architecture
                    || m.size() != new.size()
                    || m.shards.len() != new.shards.len()
                    || m.embedding != new.embedding
                    || m.trained_context != new.trained_context
                {
                    return Err("重定位的文件与原记录不一致，请将该文件单独导入".into());
                }
                m.path = new.path;
                m.stamp = new.stamp;
                m.shards = new.shards;
            }
            Change::Configure {
                id,
                name,
                projector,
                runtime_id,
                context,
                gpu_layers,
                device,
            } => {
                if name.trim().is_empty() || name.len() > 512 {
                    return Err("显示名称不能为空或过长".into());
                }
                let idx = self
                    .models
                    .iter()
                    .position(|m| m.id == id)
                    .ok_or("模型记录不存在")?;
                let m = &self.models[idx];
                if context.is_some_and(|v| {
                    !(64..=2_097_152).contains(&v)
                        || m.trained_context.is_some_and(|limit| v > limit)
                }) {
                    return Err("上下文长度超出支持范围".into());
                }
                if gpu_layers.is_some_and(|v| v > 999) {
                    return Err("GPU 卸载层数应在 0 到 999 之间".into());
                }
                let selected = match &runtime_id {
                    Some(id) => Some(
                        self.runtimes
                            .iter()
                            .find(|r| r.id == *id)
                            .ok_or("所选运行时未授权")?,
                    ),
                    None => self.automatic_runtime(),
                };
                if device
                    .as_ref()
                    .is_some_and(|d| !selected.is_some_and(|r| r.devices.contains(d)))
                {
                    return Err("运行时未报告此设备".into());
                }
                let pair = projector
                    .map(|p| -> Result<_, String> {
                        let p = gguf_path(&p)?;
                        if p == m.path {
                            return Err("主模型与投影器不能使用同一文件".into());
                        }
                        let metadata = gguf::read(&p)?;
                        if !metadata.projector || metadata.has_vision == Some(false) {
                            return Err("GGUF 缺少视觉投影器标记".into());
                        }
                        if metadata
                            .projection
                            .zip(m.embedding)
                            .is_some_and(|(a, b)| a != b)
                        {
                            return Err("投影器输出维度与主模型嵌入维度不匹配".into());
                        }
                        Ok((p.clone(), Stamp::read(&p)?))
                    })
                    .transpose()?;
                let m = &mut self.models[idx];
                m.name = name.trim().into();
                m.context = context;
                m.runtime_id = runtime_id;
                m.gpu_layers = gpu_layers;
                m.device = device;
                (m.projector, m.projector_stamp) = pair.unzip();
            }
            Change::AuthorizeRuntime { .. }
            | Change::DiagnoseRuntime { .. }
            | Change::InstallRuntime { .. } => return Err("运行时尚未授权".into()),
        }
        Ok(warnings)
    }
}

fn is_gguf(path: &Path) -> bool {
    path.extension()
        .is_some_and(|ext| ext.eq_ignore_ascii_case("gguf"))
}
fn gguf_path(path: &Path) -> Result<PathBuf, String> {
    canonical_local(path)
        .filter(|p| p.is_file() && is_gguf(p))
        .ok_or_else(|| "请选择本机现有的 .gguf 文件".into())
}
fn read_model(p: &Path) -> Result<ModelRecord, String> {
    let meta = gguf::read(p)?;
    if meta.projector {
        return Err("此文件为投影器，请在主模型的高级设置中关联".into());
    }
    let mut shards = std::collections::BTreeMap::new();
    if meta.shard_count > 1 {
        let count = meta.shard_count;
        if count > 4096 {
            return Err("GGUF 分片数超出上限".into());
        }
        let suffix = format!("-00001-of-{count:05}");
        let base = p
            .file_stem()
            .and_then(|v| v.to_str())
            .and_then(|v| v.strip_suffix(&suffix))
            .ok_or("请选择带有标准分片编号的首个 GGUF 文件")?;
        let ext = p
            .extension()
            .and_then(|v| v.to_str())
            .ok_or("GGUF 扩展名无效")?;
        for n in 2..=count {
            let name = format!("{base}-{n:05}-of-{count:05}.{ext}");
            let part = p.with_file_name(&name);
            let stamp = Stamp::read(&part)?;
            shards.insert(name, stamp);
        }
    }
    let name = meta
        .name
        .filter(|n| !n.trim().is_empty())
        .unwrap_or_else(|| {
            p.file_stem()
                .unwrap_or_default()
                .to_string_lossy()
                .into_owned()
        });
    Ok(ModelRecord {
        id: unique_id("gguf")?,
        name,
        path: p.into(),
        projector: None,
        projector_stamp: None,
        runtime_id: None,
        context: None,
        gpu_layers: None,
        device: None,
        architecture: meta.architecture,
        trained_context: meta.context,
        embedding: meta.embedding,
        stamp: Stamp::read(p)?,
        shards,
        status: String::new(),
        launch_identity: String::new(),
    })
}

fn scan(directory: &Path, warnings: &mut Vec<String>) -> Result<Vec<PathBuf>, String> {
    let root = canonical_local(directory)
        .filter(|p| p.is_dir())
        .ok_or("模型目录不存在")?;
    let mut dirs = vec![root];
    let mut paths = vec![];
    let mut examined = 0;
    while let Some(dir) = dirs.pop() {
        let entries = match fs::read_dir(&dir) {
            Ok(e) => e,
            Err(e) => {
                warnings.push(format!("{}：{e}", dir.display()));
                continue;
            }
        };
        for entry in entries {
            examined += 1;
            if examined > 50_000 || paths.len() >= MAX_RECORDS {
                warnings.push("达到扫描上限，请选择更具体的目录继续导入".into());
                return Ok(paths);
            }
            let entry = match entry {
                Ok(e) => e,
                Err(e) => {
                    warnings.push(e.to_string());
                    continue;
                }
            };
            let kind = entry.file_type().map_err(|e| e.to_string())?;
            if kind.is_dir() {
                #[cfg(windows)]
                {
                    use std::os::windows::fs::MetadataExt;
                    if entry
                        .metadata()
                        .map_err(|e| e.to_string())?
                        .file_attributes()
                        & 0x400
                        != 0
                    {
                        continue;
                    }
                }
                dirs.push(entry.path());
            } else if kind.is_file() && is_gguf(&entry.path()) {
                paths.push(entry.path());
            }
        }
    }
    paths.sort();
    Ok(paths)
}

fn available_ram() -> Option<u64> {
    #[cfg(windows)]
    {
        use windows_sys::Win32::System::SystemInformation::{GlobalMemoryStatusEx, MEMORYSTATUSEX};
        let mut state = MEMORYSTATUSEX {
            dwLength: std::mem::size_of::<MEMORYSTATUSEX>() as u32,
            ..Default::default()
        };
        if unsafe { GlobalMemoryStatusEx(&mut state) } != 0 {
            return Some(state.ullAvailPhys);
        }
    }
    #[cfg(target_os = "linux")]
    {
        let mem = fs::read_to_string("/proc/meminfo").ok()?;
        return mem.lines().find_map(|line| {
            line.strip_prefix("MemAvailable:")?
                .split_whitespace()
                .next()?
                .parse::<u64>()
                .ok()?
                .checked_mul(1024)
        });
    }
    #[cfg(target_os = "macos")]
    {
        // Retain one process-wide Mach host send right, as the port can be queried repeatedly.
        static HOST: std::sync::OnceLock<libc::mach_port_t> = std::sync::OnceLock::new();
        let host = *HOST.get_or_init(|| unsafe { libc::mach_host_self() });
        let mut stats = std::mem::MaybeUninit::<libc::vm_statistics64>::uninit();
        let mut count = libc::HOST_VM_INFO64_COUNT;
        let status = unsafe {
            libc::host_statistics64(
                host,
                libc::HOST_VM_INFO64,
                stats.as_mut_ptr().cast(),
                &mut count,
            )
        };
        let page = unsafe { libc::sysconf(libc::_SC_PAGESIZE) };
        if status != 0 || count < libc::HOST_VM_INFO64_COUNT || page <= 0 {
            return None;
        }
        let stats = unsafe { stats.assume_init() };
        return Some((u64::from(stats.free_count) + u64::from(stats.inactive_count)) * page as u64);
    }
    #[cfg(not(any(target_os = "linux", target_os = "macos")))]
    None
}
fn available_memory(m: &ModelRecord, ram: Option<u64>, runtime: Option<&Runtime>) -> Option<u64> {
    // Use a single device; host and accelerator budgets overlap on unified-memory systems.
    let device = runtime
        .filter(|_| m.gpu_layers != Some(0))
        .and_then(|r| {
            r.device_details
                .iter()
                .filter(|d| m.device.as_ref().is_none_or(|id| id == &d.id))
                .filter_map(|d| d.free_mib)
                .max()
        })
        .and_then(|mib| mib.checked_mul(1024 * 1024));
    ram.max(device)
}
fn fit(m: &ModelRecord, ram: Option<u64>, runtime: Option<&Runtime>) -> &'static str {
    let bytes = m
        .size()
        .saturating_add(m.projector_stamp.map_or(0, |s| s.size));
    match available_memory(m, ram, runtime) {
        None => "unknown",
        Some(available)
            if available
                < bytes
                    .saturating_add(bytes / 4)
                    .saturating_add(512 * 1024 * 1024) =>
        {
            "tight"
        }
        _ => "likely",
    }
}
fn suggested_context(m: &ModelRecord, ram: Option<u64>, runtime: Option<&Runtime>) -> u32 {
    let headroom = available_memory(m, ram, runtime).map(|r| {
        r.saturating_sub(m.size())
            .saturating_sub(m.projector_stamp.map_or(0, |s| s.size))
    });
    let ctx = match headroom {
        Some(bytes) if bytes < 1024 * 1024 * 1024 => 512,
        Some(bytes) if bytes >= 8 * 1024 * 1024 * 1024 => 4096,
        _ => 2048,
    };
    m.trained_context.unwrap_or(ctx).min(ctx).max(1)
}

pub(crate) fn models(data: &Path) -> Result<Vec<ModelRecord>, String> {
    let index = Stamp::read(&data.join(REGISTRY)).ok();
    if let Ok(guard) = MODEL_CACHE.lock() {
        if let Some(cached) = guard.as_ref().filter(|c| {
            c.data == data && c.index == index && c.observed.elapsed() < Duration::from_secs(5)
        }) {
            return Ok(cached.models.clone());
        }
    }
    let lib = Library::open(data)?;
    let rows: Vec<_> = lib
        .models
        .iter()
        .cloned()
        .map(|mut m| {
            m.status = lib.status(&m).into();
            m.launch_identity = lib.identity(&m);
            m
        })
        .collect();
    if let Ok(mut cache) = MODEL_CACHE.lock() {
        *cache = Some(ModelCache {
            data: data.into(),
            index,
            observed: Instant::now(),
            models: rows.clone(),
        });
    }
    Ok(rows)
}
pub(crate) struct LaunchSpec {
    pub launch_identity: String,
    pub executable: PathBuf,
    pub model: PathBuf,
    pub projector: Option<PathBuf>,
    pub context: u32,
    pub gpu_layers: Option<u32>,
    pub device: Option<String>,
}
pub(crate) fn resolve(data: &Path, id: &str) -> Result<LaunchSpec, String> {
    let lib = Library::open(data)?;
    let m = lib
        .models
        .iter()
        .find(|m| m.id == id)
        .ok_or("未找到导入的模型，请刷新模型库")?;
    match lib.status(m) {
        "ready" => {}
        "file_missing" => return Err("找不到模型文件，请重新定位".into()),
        "file_changed" => return Err("GGUF 文件已修改，请重新导入或重新选择投影器".into()),
        "projector_missing" => return Err("投影器文件缺失，请在高级设置中重新选择".into()),
        "runtime_changed" => return Err("运行时已修改，请重新授权".into()),
        _ => return Err("缺少可用运行时，请安装或授权 llama-server".into()),
    }
    let r = lib.runtime(m).ok_or("缺少运行时")?;
    if canonical_local(&r.path).as_ref() != Some(&r.path) || execution_digest(&r.path)? != r.digest
    {
        return Err("运行时文件已修改，请重新授权".into());
    }
    if m.device.as_ref().is_some_and(|d| !r.devices.contains(d)) {
        return Err("运行时设备已变更，请重新选择计算设备".into());
    }
    Ok(LaunchSpec {
        launch_identity: lib.identity(m),
        executable: r.path.clone(),
        model: m.path.clone(),
        projector: m.projector.clone(),
        context: m
            .context
            .unwrap_or_else(|| suggested_context(m, available_ram(), Some(r))),
        gpu_layers: m.gpu_layers,
        device: m.device.clone(),
    })
}

#[derive(Deserialize)]
#[serde(tag = "action", rename_all = "snake_case", deny_unknown_fields)]
pub enum Change {
    Import {
        paths: Vec<PathBuf>,
    },
    Scan {
        path: PathBuf,
    },
    Forget {
        id: String,
    },
    Relocate {
        id: String,
        path: PathBuf,
    },
    Configure {
        id: String,
        name: String,
        projector: Option<PathBuf>,
        runtime_id: Option<String>,
        context: Option<u32>,
        gpu_layers: Option<u32>,
        device: Option<String>,
    },
    AuthorizeRuntime {
        path: PathBuf,
    },
    DiagnoseRuntime {
        id: String,
    },
    ForgetRuntime {
        id: String,
    },
    InstallRuntime {
        backend: Option<String>,
    },
}

#[tauri::command]
pub async fn model_library_snapshot(app: tauri::AppHandle) -> Result<Value, ApiError> {
    let data = data_directory(&app)?;
    tauri::async_runtime::spawn_blocking(move || {
        Library::open(&data).map(|lib| lib.snapshot(vec![]))
    })
    .await
    .map_err(|e| ApiError::new(e.to_string()))?
    .map_err(ApiError::new)
}
#[tauri::command]
pub async fn modify_model_library(
    app: tauri::AppHandle,
    window: tauri::WebviewWindow,
    change: Change,
) -> Result<Value, ApiError> {
    let data = data_directory(&app)?;
    let consent = if let Change::AuthorizeRuntime { path } = &change {
        let path = path.clone();
        let pending =
            tauri::async_runtime::spawn_blocking(move || runtime::Consent::inspect(&path))
                .await
                .map_err(|e| ApiError::new(e.to_string()))?
                .map_err(ApiError::new)?;
        if !picker::confirm_execution(&window, &pending.path, &pending.digest).await? {
            return Err(ApiError::new("已取消运行时授权"));
        }
        Some(pending)
    } else {
        None
    };
    tauri::async_runtime::spawn_blocking(move || modify(&data, change, consent))
        .await
        .map_err(|e| ApiError::new(e.to_string()))?
        .map_err(ApiError::new)
}

#[cfg(test)]
pub(crate) fn modify_for_test(data: &Path, change: Value) -> Result<Value, String> {
    let change = serde_json::from_value(change).map_err(|e| e.to_string())?;
    let consent = if let Change::AuthorizeRuntime { path } = &change {
        Some(runtime::Consent::inspect(path)?)
    } else {
        None
    };
    modify(data, change, consent)
}
fn modify(data: &Path, change: Change, consent: Option<runtime::Consent>) -> Result<Value, String> {
    fs::create_dir_all(data).map_err(|e| e.to_string())?;
    let runtime = match &change {
        Change::AuthorizeRuntime { path } => {
            let consent = consent.as_ref().ok_or("需要在本机确认运行时授权")?;
            Some(runtime::authorize(path, data, Some(consent))?)
        }
        Change::DiagnoseRuntime { id } => {
            let mut current = Library::open(data)?
                .runtimes
                .into_iter()
                .find(|r| r.id == *id)
                .ok_or("运行时授权已撤销")?;
            let consent = runtime::Consent {
                path: current.path.clone(),
                digest: current.digest.clone(),
            };
            let mut refreshed = runtime::authorize(&current.path, data, Some(&consent))?;
            refreshed.release_asset = current.release_asset.take();
            let _write = WRITE_LOCK.lock().map_err(|_| "模型库忙，请重试")?;
            let mut lib = Library::open(data)?;
            if !lib
                .runtimes
                .iter()
                .any(|r| r.id == *id && r.path == consent.path && r.digest == consent.digest)
            {
                return Err("运行时授权已变更，请刷新模型库".into());
            }
            lib.register_runtime(refreshed)?;
            lib.save(data)?;
            return Ok(lib.snapshot(vec![]));
        }
        Change::InstallRuntime { backend } => {
            let _guard = INSTALL_LOCK.lock().map_err(|_| "运行时安装繁忙")?;
            let lib = Library::open(data)?;
            let pkg = runtime::packages()
                .iter()
                .find(|p| backend.as_ref().is_none_or(|key| key == p.backend))
                .ok_or("此平台不支持所选运行时")?;
            if lib.runtimes.iter().any(|r| {
                r.release_asset.as_deref() == Some(pkg.asset)
                    && r.status() == "ready"
                    && execution_digest(&r.path).ok().as_ref() == Some(&r.digest)
            }) {
                return Ok(lib.snapshot(vec![]));
            }
            let new = runtime::install(data, backend.as_deref())?;
            let _write = WRITE_LOCK.lock().map_err(|_| "模型库忙，请重试")?;
            let mut lib = Library::open(data)?;
            lib.register_runtime(new)?;
            lib.save(data)?;
            return Ok(lib.snapshot(vec![]));
        }
        _ => None,
    };
    let _guard = WRITE_LOCK.lock().map_err(|_| "模型库忙，请重试")?;
    let mut lib = Library::open(&data)?;
    let warnings = if let Some(r) = runtime {
        lib.register_runtime(r)?;
        vec![]
    } else {
        lib.change(change)?
    };
    lib.save(&data)?;
    Ok(lib.snapshot(warnings))
}
