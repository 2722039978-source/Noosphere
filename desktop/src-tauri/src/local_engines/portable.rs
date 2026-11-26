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
    /// When true the directory holds user-supplied weights and any `*.gguf`
    /// under `models/` is accepted, so the pinned file name, size and SHA-256
    /// are not enforced.
    pub custom: bool,
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
    custom: false,
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
    custom: false,
};

/// A user-supplied llama.cpp directory: any `*.gguf` under `models/` is
/// offered, and the runtime is taken from the same directory.
pub(super) const CUSTOM: PortableProfile = PortableProfile {
    name: "自定义 llama.cpp 模型",
    alias: "custom-local",
    file: "",
    size: 0,
    digest: "",
    folder: "",
    reasoning_budget: "512",
    temperature: ".8",
    top_k: "40",
    min_p: ".05",
    custom: true,
};

#[derive(Clone)]
pub(super) struct PortableModel {
    pub alias: String,
    pub name: String,
    pub file: PathBuf,
}

#[derive(Clone)]
pub(super) struct PortableInstall {
    pub directory: PathBuf,
    executable: PathBuf,
    model: PathBuf,
    /// Every weight file this directory can serve. The pinned deployments hold
    /// exactly one; a custom directory holds as many as the user dropped in.
    pub models: Vec<PortableModel>,
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
    if profile.custom {
        return custom_at(directory);
    }
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
        models: Vec::new(),
    })
}

/// Accept a user-supplied llama.cpp layout. Two shapes work:
/// - `<dir>/models/*.gguf`, runtime under `<dir>/runtime/` or `<dir>`
/// - `<dir>/*.gguf`, runtime next to the weights
/// The runtime is also probed in up to three ancestor directories and through
/// `NOOSPHERE_LLAMA_SERVER`, so pointing at a bare weights folder is enough.
/// No integrity sidecar is demanded here — the user vouches for their weights.
fn custom_at(directory: &Path) -> Option<PortableInstall> {
    let directory = canonical_local(directory)?;
    let executable = find_runtime(&directory)?;
    let weights = weight_dir(&directory)?;
    let mut models = Vec::new();
    let mut entries = fs::read_dir(&weights).ok()?.flatten().take(1024);
    while let Some(entry) = entries.next() {
        let path = entry.path();
        if !path.is_file() {
            continue;
        }
        let name = entry.file_name().to_string_lossy().to_string();
        let lower = name.to_ascii_lowercase();
        if !lower.ends_with(".gguf") || lower.contains("mmproj") {
            continue;
        }
        let file = match canonical_local(&path) {
            Some(file) if file.starts_with(&weights) => file,
            _ => continue,
        };
        let stem = name.trim_end_matches(".gguf").trim_end_matches(".GGUF");
        let alias = portable_alias(stem);
        if alias.is_empty() {
            continue;
        }
        models.push(PortableModel {
            alias,
            name: stem.to_string(),
            file,
        });
    }
    models.sort_by(|a, b| a.name.to_ascii_lowercase().cmp(&b.name.to_ascii_lowercase()));
    let model = models.first()?.file.clone();
    Some(PortableInstall {
        directory,
        executable,
        model,
        models,
    })
}

fn runtime_in(root: &Path) -> Option<PathBuf> {
    [
        "runtime/llama-server.exe",
        "runtime/bin/llama-server.exe",
        "runtime/sycl/llama-server.exe",
        "llama-server.exe",
        "bin/llama-server.exe",
    ]
    .into_iter()
    .find_map(|relative| contained_file(root, relative))
}

fn find_runtime(directory: &Path) -> Option<PathBuf> {
    let mut scope = Some(directory);
    for _ in 0..4 {
        let Some(root) = scope else { break };
        if let Some(found) = runtime_in(root) {
            return Some(found);
        }
        scope = root.parent();
    }
    std::env::var_os("NOOSPHERE_LLAMA_SERVER")
        .map(PathBuf::from)
        .and_then(|path| canonical_local(&path))
}

fn has_weights(directory: &Path) -> bool {
    fs::read_dir(directory)
        .map(|entries| {
            entries.flatten().any(|entry| {
                let name = entry.file_name().to_string_lossy().to_ascii_lowercase();
                entry.path().is_file() && name.ends_with(".gguf") && !name.contains("mmproj")
            })
        })
        .unwrap_or(false)
}

fn weight_dir(directory: &Path) -> Option<PathBuf> {
    if let Some(nested) = canonical_local(&directory.join("models")) {
        if nested.is_dir() && has_weights(&nested) {
            return Some(nested);
        }
    }
    has_weights(directory).then(|| directory.to_owned())
}

/// llama.cpp reports the model id from `--alias`, and the gateway stores it in
/// `model_name`, so keep it to characters that survive a JSON round-trip and
/// look sane in the model picker.
fn portable_alias(stem: &str) -> String {
    let mut alias = String::with_capacity(stem.len());
    for ch in stem.chars() {
        if ch.is_ascii_alphanumeric() || matches!(ch, '.' | '_' | '-' | '/') {
            alias.push(ch);
        } else if !ch.is_control() {
            alias.push('-');
        }
    }
    let alias = alias.trim_matches('-').to_string();
    if alias.len() > 200 {
        alias[..200].to_string()
    } else {
        alias
    }
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
    // A custom directory is only ever the one the user picked (or the one we
    // saved last time); guessing by walking parent directories would attach
    // unrelated llama.cpp folders that happen to sit nearby.
    if profile.custom {
        for candidate in [workspace, saved.as_deref()].into_iter().flatten() {
            if let Some(install) = install_at(profile, candidate) {
                return Some(install);
            }
        }
        return None;
    }
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
    /// Point this installation at one of the weights it advertises. Used when a
    /// custom directory holds several GGUF files and the user picks one.
    pub(super) fn with_model(&self, alias: &str) -> Option<PortableInstall> {
        if alias.is_empty() {
            return Some(self.clone());
        }
        let model = self
            .models
            .iter()
            .find(|model| model.alias == alias)
            .map(|model| model.file.clone())?;
        let mut install = self.clone();
        install.model = model;
        Some(install)
    }

    fn alias(&self, profile: PortableProfile) -> String {
        self.models
            .iter()
            .find(|model| model.file == self.model)
            .map(|model| model.alias.clone())
            .unwrap_or_else(|| profile.alias.to_string())
    }

    /// Multimodal projector that sits next to the weights, if the user supplied
    /// one. Looked up in `models/` first, then beside the weights themselves.
    fn mmproj(&self) -> Option<PathBuf> {
        for directory in [self.directory.join("models"), self.directory.clone()] {
            let Ok(entries) = fs::read_dir(directory) else {
                continue;
            };
            for entry in entries.flatten().take(1024) {
                let path = entry.path();
                let name = entry.file_name().to_string_lossy().to_ascii_lowercase();
                if !name.ends_with(".gguf") || !name.contains("mmproj") {
                    continue;
                }
                if let Some(file) = canonical_local(&path) {
                    if file.is_file() {
                        return Some(file);
                    }
                }
            }
        }
        None
    }

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
        let alias = self.alias(profile);
        command.arg("-m").arg(&self.model);
        if profile.custom {
            if let Some(mmproj) = self.mmproj() {
                command.arg("--mmproj").arg(mmproj);
            }
        }
        command.args([
            "--alias",
            alias.as_str(),
            "--host",
            "127.0.0.1",
            "--port",
            &port.to_string(),
        ]);
        if profile.custom {
            // User-supplied runtimes are usually CUDA or Vulkan builds that want
            // a normal context; the SYCL tuning below belongs to the pinned
            // deployments only. Both knobs stay overridable through the
            // environment so a heavier model can be dialled in without a rebuild.
            let context = std::env::var("NOOSPHERE_CUSTOM_CONTEXT")
                .ok()
                .and_then(|value| value.parse::<u32>().ok())
                .filter(|value| (512..=1_048_576).contains(value))
                .unwrap_or(8192);
            let gpu_layers = std::env::var("NOOSPHERE_CUSTOM_GPU_LAYERS")
                .ok()
                .and_then(|value| value.parse::<u32>().ok())
                .filter(|value| *value <= 1000)
                .unwrap_or(99);
            let context = context.to_string();
            let gpu_layers = gpu_layers.to_string();
            command.args([
                "-c",
                context.as_str(),
                "-ngl",
                gpu_layers.as_str(),
                "-fa",
                "on",
                "--jinja",
                "--temp",
                "0.7",
                "--top-p",
                ".95",
                "--top-k",
                "40",
                "--min-p",
                ".05",
                "--presence-penalty",
                "0",
                "--repeat-penalty",
                "1.05",
                "-lv",
                "3",
            ]);
        } else {
            command.args([
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
        }
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

#[cfg(test)]
mod tests {
    use super::*;

    fn scratch(name: &str) -> PathBuf {
        let dir = Path::new(env!("CARGO_MANIFEST_DIR"))
            .join("target")
            .join("tmp")
            .join(name);
        let _ = fs::remove_dir_all(&dir);
        fs::create_dir_all(dir.join("models")).unwrap();
        fs::create_dir_all(dir.join("runtime")).unwrap();
        fs::write(dir.join("runtime").join("llama-server.exe"), b"stub").unwrap();
        dir
    }

    #[test]
    fn custom_directory_offers_every_weight_and_ignores_projectors() {
        let dir = scratch("custom-any-gguf");
        fs::write(dir.join("models").join("alpha.Q6_K.gguf"), b"a").unwrap();
        fs::write(dir.join("models").join("beta.Q8_0.gguf"), b"b").unwrap();
        fs::write(dir.join("models").join("mmproj-vision.gguf"), b"p").unwrap();
        let install = custom_at(&dir).expect("custom install");
        assert_eq!(
            install
                .models
                .iter()
                .map(|model| model.alias.as_str())
                .collect::<Vec<_>>(),
            vec!["alpha.Q6_K", "beta.Q8_0"]
        );
        assert!(install.mmproj().is_some());
        assert!(install.model.ends_with("alpha.Q6_K.gguf"));
    }

    #[test]
    fn custom_directory_without_runtime_is_rejected() {
        let dir = scratch("custom-no-runtime");
        fs::remove_file(dir.join("runtime").join("llama-server.exe")).unwrap();
        fs::write(dir.join("models").join("alpha.gguf"), b"a").unwrap();
        assert!(custom_at(&dir).is_none());
    }

    #[test]
    fn custom_directory_needs_at_least_one_weight() {
        let dir = scratch("custom-empty");
        assert!(custom_at(&dir).is_none());
    }

    #[test]
    fn with_model_switches_the_selected_weight() {
        let dir = scratch("custom-switch");
        fs::write(dir.join("models").join("alpha.gguf"), b"a").unwrap();
        fs::write(dir.join("models").join("beta.gguf"), b"b").unwrap();
        let install = custom_at(&dir).expect("custom install");
        let switched = install.with_model("beta").expect("beta is listed");
        assert!(switched.model.ends_with("beta.gguf"));
        assert!(install.with_model("missing").is_none());
        assert!(install.with_model("").is_some());
    }

    #[test]
    fn aliases_stay_reportable_through_the_gateway() {
        assert_eq!(
            portable_alias("DeepSeek-V4-Pro_9B.Q6_K"),
            "DeepSeek-V4-Pro_9B.Q6_K"
        );
        assert_eq!(portable_alias("模型 A"), "A");
        assert_eq!(portable_alias("---"), "");
    }

    #[test]
    fn pinned_profile_still_requires_its_integrity_record() {
        let dir = scratch("pinned-missing-record");
        fs::create_dir_all(dir.join("runtime").join("sycl")).unwrap();
        fs::write(
            dir.join("runtime").join("sycl").join("llama-server.exe"),
            b"stub",
        )
        .unwrap();
        fs::write(dir.join("models").join(BONSAI.file), b"not the real weights").unwrap();
        assert!(install_at(BONSAI, &dir).is_none());
    }

    #[test]
    fn weights_beside_the_runtime_are_discovered() {
        let dir = scratch("custom-flat");
        fs::remove_dir_all(dir.join("models")).unwrap();
        fs::write(dir.join("alpha.gguf"), b"a").unwrap();
        let install = custom_at(&dir).expect("flat layout is accepted");
        assert_eq!(install.models.len(), 1);
        assert_eq!(install.models[0].alias, "alpha");
    }

    #[test]
    fn runtime_is_found_in_an_ancestor_directory() {
        let dir = scratch("custom-ancestor");
        let weights = dir.join("models").join("SomeModel");
        fs::create_dir_all(&weights).unwrap();
        fs::write(weights.join("alpha.gguf"), b"a").unwrap();
        let install = custom_at(&weights).expect("runtime resolved from an ancestor");
        assert_eq!(install.models.len(), 1);
        assert_eq!(install.models[0].alias, "alpha");
    }

    #[test]
    fn custom_start_command_points_at_the_selected_weight() {
        let dir = scratch("custom-command");
        fs::write(dir.join("models").join("alpha.gguf"), b"a").unwrap();
        fs::write(dir.join("models").join("beta.gguf"), b"b").unwrap();
        fs::write(dir.join("models").join("mmproj-alpha.gguf"), b"p").unwrap();
        let install = custom_at(&dir).unwrap().with_model("beta").unwrap();
        let data = dir.join("data");
        fs::create_dir_all(&data).unwrap();

        let command = install.command(CUSTOM, 18789, &data).unwrap();
        let args: Vec<String> = command
            .get_args()
            .map(|arg| arg.to_string_lossy().into_owned())
            .collect();

        let model = args.iter().position(|arg| arg == "-m").expect("-m is set");
        assert!(args[model + 1].ends_with("beta.gguf"));
        assert!(!args[model + 1].contains("mmproj"));

        let projector = args
            .iter()
            .position(|arg| arg == "--mmproj")
            .expect("projector is attached");
        assert!(args[projector + 1].ends_with("mmproj-alpha.gguf"));

        let alias = args.iter().position(|arg| arg == "--alias").unwrap();
        assert_eq!(args[alias + 1], "beta");
        assert!(args.iter().any(|arg| arg == "18789"));
        assert!(args.iter().any(|arg| arg == "--jinja"));

        // Custom directories get a plain, larger context and none of the SYCL
        // tuning that the pinned bundles rely on.
        let context = args.iter().position(|arg| arg == "-c").expect("-c is set");
        assert_eq!(args[context + 1], "8192");
        assert!(!args.iter().any(|arg| arg == "--cache-ram"));
        assert!(!args.iter().any(|arg| arg == "--load-mode"));
    }
}
