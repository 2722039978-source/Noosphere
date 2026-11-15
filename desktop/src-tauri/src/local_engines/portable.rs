use serde::Deserialize;
use std::fs;
use std::io::{Read, Write};
use std::path::{Path, PathBuf};
use std::process::Command;

#[derive(Clone, Copy)]
pub(super) struct PortableProfile {
    pub name: &'static str,
    pub alias: &'static str,
    pub file: &'static str,
    pub size: u64,
    digest: &'static str,
    folder: &'static str,
    reasoning_budget: &'static str,
    temperature: &'static str,
    top_k: &'static str,
    min_p: &'static str,
}

pub(super) const BONSAI: PortableProfile = PortableProfile {
    name: "Bonsai 2 27B（llama.cpp）",
    alias: "bonsai-local-27b",
    file: "Ternary-Bonsai-2-27B-PTQ1_0.gguf",
    size: 5_946_648_928,
    digest: "53107f530aa52eb00912263ab1ee29bd199261c87cd7b4ad4ca1318c1fe33ee3",
    folder: "Qwen",
    reasoning_budget: "512",
    temperature: "1.0",
    top_k: "20",
    min_p: ".05",
};

pub(super) const DEEPSEEK: PortableProfile = PortableProfile {
    name: "DeepSeek R1 8B（llama.cpp）",
    alias: "deepseek-r1-0528-qwen3-8b",
    file: "DeepSeek-R1-0528-Qwen3-8B-Q4_K_M.gguf",
    size: 5_027_785_216,
    digest: "a86349a4180c4e6bb43f874c29c404fa2be3f90b15509bd6d86f697dba724ec1",
    folder: "DeepSeek",
    reasoning_budget: "128",
    temperature: ".6",
    top_k: "40",
    min_p: "0",
};

#[derive(Clone)]
pub(super) struct PortableInstall {
    pub directory: PathBuf,
    executable: PathBuf,
    model: PathBuf,
}

#[derive(Deserialize)]
struct IntegrityRecord {
    file: PathBuf,
    verified: bool,
    sha256: String,
    size: Option<u64>,
    mtime_ns: Option<u64>,
}

pub(super) fn canonical_local(path: &Path) -> Option<PathBuf> {
    if !path.is_absolute() {
        return None;
    }
    #[cfg(windows)]
    if !matches!(path.components().next(), Some(std::path::Component::Prefix(kind)) if matches!(kind.kind(), std::path::Prefix::Disk(_) | std::path::Prefix::VerbatimDisk(_)))
    {
        return None;
    }
    let path = path.canonicalize().ok()?;
    #[cfg(windows)]
    {
        use std::path::{Component, Prefix};
        // Local drive paths only; no UNC, device paths or named pipes. Strip
        // the canonicalization prefix for runtimes with narrow path support.
        match path.components().next()? {
            Component::Prefix(kind) => match kind.kind() {
                Prefix::Disk(_) => Some(path),
                Prefix::VerbatimDisk(_) => {
                    Some(PathBuf::from(path.to_str()?.strip_prefix(r"\\?\")?))
                }
                _ => None,
            },
            _ => None,
        }
    }
    #[cfg(not(windows))]
    Some(path)
}

fn contained_file(base: &Path, relative: impl AsRef<Path>) -> Option<PathBuf> {
    let file = canonical_local(&base.join(relative))?;
    (file.starts_with(base) && file.is_file()).then_some(file)
}

fn install_at(profile: PortableProfile, directory: &Path) -> Option<PortableInstall> {
    let directory = canonical_local(directory)?;
    let executable = contained_file(&directory, "runtime/sycl/llama-server.exe")?;
    let model = contained_file(&directory, Path::new("models").join(profile.file))?;
    let metadata = model.metadata().ok()?;
    if metadata.len() != profile.size {
        return None;
    }
    let record_path = contained_file(&directory, "evidence/verified-model.json")
        .or_else(|| contained_file(&directory, "evidence/model-integrity.json"))?;
    let mut data = Vec::new();
    fs::File::open(record_path)
        .ok()?
        .take(8193)
        .read_to_end(&mut data)
        .ok()?;
    if data.len() > 8192 {
        return None;
    }
    let record: IntegrityRecord = serde_json::from_slice(&data).ok()?;
    if !record.verified
        || record.file.file_name()?.to_str()? != profile.file
        || !record.sha256.eq_ignore_ascii_case(profile.digest)
        || record.size.is_some_and(|size| size != metadata.len())
        || record.mtime_ns.is_some_and(|stamp| {
            metadata
                .modified()
                .ok()
                .and_then(|time| time.duration_since(std::time::UNIX_EPOCH).ok())
                .map(|time| time.as_nanos())
                != Some(stamp.into())
        })
    {
        return None;
    }
    Some(PortableInstall {
        directory,
        executable,
        model,
    })
}

pub(super) fn discover(
    profile: PortableProfile,
    data_dir: &Path,
    id: &str,
    directory: Option<&Path>,
    workspace: Option<&Path>,
) -> Option<PortableInstall> {
    if let Some(directory) = directory {
        return install_at(profile, directory);
    }
    let saved = fs::read_to_string(profile_path(data_dir, id))
        .ok()
        .filter(|path| path.len() <= 4096)
        .map(PathBuf::from);
    let cwd = std::env::current_dir().ok();
    let exe_dir = std::env::current_exe()
        .ok()
        .and_then(|path| path.parent().map(Path::to_owned));
    let override_dir = std::env::var_os("NOOSPHERE_MODEL_ROOT").map(PathBuf::from);
    for scope in saved
        .as_deref()
        .into_iter()
        .chain(override_dir.as_deref())
        .chain(workspace)
        .chain(cwd.as_deref())
        .chain(exe_dir.as_deref())
    {
        let Some(scope) = canonical_local(scope) else {
            continue;
        };
        for ancestor in scope
            .ancestors()
            .take(6)
            .filter(|path| path.parent().is_some())
        {
            for candidate in [
                ancestor.to_owned(),
                ancestor.join(profile.folder),
                ancestor.join("model").join(profile.folder),
            ] {
                if let Some(install) = install_at(profile, &candidate) {
                    return Some(install);
                }
            }
            if profile.alias == BONSAI.alias {
                for candidate in [
                    ancestor.join("bonsai-local"),
                    ancestor.join("modle/bonsai-local"),
                ] {
                    if let Some(install) = install_at(profile, &candidate) {
                        return Some(install);
                    }
                }
            }
        }
    }
    None
}

fn profile_path(data_dir: &Path, id: &str) -> PathBuf {
    data_dir
        .join("local-engines")
        .join(format!("{id}-directory.txt"))
}

impl PortableInstall {
    pub(super) fn remember(&self, data_dir: &Path, id: &str) -> Result<(), String> {
        let path = profile_path(data_dir, id);
        let mut file =
            fs::File::create(path).map_err(|_| "无法保存已选的本地模型目录。".to_string())?;
        file.write_all(self.directory.to_string_lossy().as_bytes())
            .map_err(|_| "无法保存已选的本地模型目录。".to_string())
    }

    pub(super) fn command(
        &self,
        profile: PortableProfile,
        port: u16,
        data_dir: &Path,
    ) -> Result<Command, String> {
        let mut command = Command::new(&self.executable);
        command.current_dir(self.executable.parent().ok_or("本地运行时没有目录。")?);
        // Never interpret paths or portable profile files as shell code.
        // Use the validated, single-slot SYCL profile shipped by this deployment.
        command.arg("-m").arg(&self.model).args([
            "--alias",
            profile.alias,
            "--host",
            "127.0.0.1",
            "--port",
            &port.to_string(),
            "-c",
            "2048",
            "-np",
            "1",
            "-t",
            "4",
            "-tb",
            "4",
            "--threads-http",
            "2",
            "--poll",
            "0",
            "--poll-batch",
            "0",
            "--prio",
            "-1",
            "-b",
            "128",
            "-ub",
            "32",
            "-ngl",
            "99",
            "--fit",
            "off",
            "-fa",
            "on",
            "--cache-ram",
            "0",
            "--load-mode",
            "none",
            "--warmup",
            "--jinja",
            "--reasoning-format",
            "deepseek",
            "--reasoning",
            "on",
            "--reasoning-effort",
            "medium",
            "--reasoning-budget",
            profile.reasoning_budget,
            "--temp",
            profile.temperature,
            "--top-p",
            ".95",
            "--top-k",
            profile.top_k,
            "--min-p",
            profile.min_p,
            "--presence-penalty",
            "0",
            "--repeat-penalty",
            "1",
            "-lv",
            "3",
        ]);
        for (key, _) in std::env::vars_os() {
            let name = key.to_string_lossy();
            if ["GGML_", "LLAMA_ARG_", "LLAMA_FUSED_", "LLAMA_GDN_"]
                .iter()
                .any(|prefix| name.starts_with(prefix))
                || [
                    "ONEAPI_DEVICE_SELECTOR",
                    "SYCL_DEVICE_FILTER",
                    "SYCL_UR_USE_LEVEL_ZERO_V2",
                    "ZE_AFFINITY_MASK",
                ]
                .contains(&name.as_ref())
            {
                command.env_remove(key);
            }
        }
        for (name, folder) in [
            ("TEMP", "tmp"),
            ("TMP", "tmp"),
            ("LLAMA_CACHE", "cache"),
            ("HF_HOME", "cache/hf"),
            ("XDG_CACHE_HOME", "cache"),
            ("LOCALAPPDATA", "cache/localappdata"),
            ("APPDATA", "cache/appdata"),
        ] {
            let path = data_dir
                .join("local-engines")
                .join(profile.alias)
                .join(folder);
            fs::create_dir_all(&path).map_err(|_| "无法创建本地模型的缓存目录。".to_string())?;
            command.env(name, path);
        }
        Ok(command)
    }
}
