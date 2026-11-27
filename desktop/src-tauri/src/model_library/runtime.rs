use super::{canonical_local, unique_id, Stamp};
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::fs::{self, File, OpenOptions};
use std::io::{Read, Seek, SeekFrom};
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};
use std::time::{Duration, Instant};

pub(super) const SERVER: &str = if cfg!(windows) {
    "llama-server.exe"
} else {
    "llama-server"
};

#[derive(Clone, Deserialize, Serialize)]
pub(super) struct Runtime {
    pub id: String,
    pub path: PathBuf,
    pub version: String,
    pub devices: Vec<String>,
    pub digest: String,
    pub stamp: Stamp,
    #[serde(default)]
    package_stamps: Vec<(PathBuf, Stamp)>,
    #[serde(skip)]
    observed: std::sync::OnceLock<&'static str>,
    #[serde(default)]
    pub release_asset: Option<String>,
    #[serde(default)]
    pub device_details: Vec<Device>,
}

impl Runtime {
    pub fn status(&self) -> &'static str {
        self.observed.get_or_init(|| match Stamp::read(&self.path) {
            Ok(stamp)
                if stamp == self.stamp
                    && (self.package_stamps.is_empty()
                        || package_stamps(&self.path).ok().as_ref()
                            == Some(&self.package_stamps)) =>
            {
                "ready"
            }
            Ok(_) => "changed",
            Err(_) => "missing",
        })
    }
}

pub(super) fn candidates() -> Vec<PathBuf> {
    let mut paths = vec![];
    if let Some(explicit) = std::env::var_os("NOOSPHERE_LLAMA_SERVER") {
        paths.push(PathBuf::from(explicit));
    }
    if let Some(path) = std::env::var_os("PATH") {
        paths.extend(
            std::env::split_paths(&path)
                .filter(|p| p.is_absolute())
                .map(|p| p.join(SERVER)),
        );
    }
    paths.extend(
        ["/opt/homebrew/bin", "/usr/local/bin", "/usr/bin"]
            .iter()
            .map(|p| Path::new(p).join(SERVER)),
    );
    let mut found = paths
        .iter()
        .filter_map(|path| canonical_local(path))
        .filter(|p| p.is_file())
        .collect::<Vec<_>>();
    found.sort();
    found.dedup();
    found
}

fn package_files(path: &Path) -> Result<Vec<PathBuf>, String> {
    let dir = path.parent().ok_or("运行时目录无效")?;
    let mut files = vec![path.to_owned()];
    for entry in fs::read_dir(dir).map_err(|e| e.to_string())? {
        let entry = entry.map_err(|e| e.to_string())?;
        let name = entry.file_name().to_string_lossy().to_ascii_lowercase();
        if name.ends_with(".dll")
            || name.ends_with(".dylib")
            || name.ends_with(".so")
            || name.contains(".so.")
        {
            files.push(entry.path());
        }
    }
    files.sort();
    Ok(files)
}
fn package_stamps(path: &Path) -> Result<Vec<(PathBuf, Stamp)>, String> {
    package_files(path)?
        .into_iter()
        .map(|p| {
            let stamp = Stamp::read(&p)?;
            Ok((p, stamp))
        })
        .collect()
}
pub(crate) fn execution_digest(path: &Path) -> Result<String, String> {
    let files = package_files(path)?;
    let mut digest = Sha256::new();
    let mut blobs = std::collections::BTreeMap::new();
    for path in files {
        let target = canonical_local(&path)
            .filter(|p| p.is_file())
            .ok_or("运行时动态库路径无效")?;
        let blob = if let Some(cached) = blobs.get(&target) {
            *cached
        } else {
            let mut file = File::open(&target).map_err(|e| e.to_string())?;
            let mut hash = Sha256::new();
            std::io::copy(&mut file, &mut hash).map_err(|e| e.to_string())?;
            let result: [u8; 32] = hash.finalize().into();
            blobs.insert(target, result);
            result
        };
        digest.update(path.file_name().unwrap_or_default().as_encoded_bytes());
        digest.update([0]);
        digest.update(blob);
    }
    Ok(format!("{:x}", digest.finalize()))
}

// A GGUF runtime receives only operating-system directories, locale and device
// selectors. Desktop/provider credentials and loader/plugin overrides do not
// cross into either the executable probe or the model server.
pub(crate) fn prepare_runtime_environment(
    command: &mut Command,
    executable: &Path,
) -> Result<(), String> {
    let directory = executable
        .parent()
        .filter(|p| p.is_absolute())
        .ok_or("运行时需要绝对路径")?;
    command.env_clear().current_dir(directory);
    for key in [
        "HOME",
        "USERPROFILE",
        "LOCALAPPDATA",
        "APPDATA",
        "TMP",
        "TEMP",
        "TMPDIR",
        "XDG_CACHE_HOME",
        "XDG_RUNTIME_DIR",
    ] {
        if let Some(value) = std::env::var_os(key).filter(|v| Path::new(v).is_absolute()) {
            command.env(key, value);
        }
    }
    for key in [
        "LANG",
        "LC_ALL",
        "LC_CTYPE",
        "CUDA_VISIBLE_DEVICES",
        "HIP_VISIBLE_DEVICES",
        "ROCR_VISIBLE_DEVICES",
        "ONEAPI_DEVICE_SELECTOR",
        "SYCL_DEVICE_FILTER",
        "ZE_AFFINITY_MASK",
        "GPU_DEVICE_ORDINAL",
    ] {
        if let Some(value) = std::env::var_os(key) {
            command.env(key, value);
        }
    }
    #[cfg(unix)]
    {
        command.env("PATH", "/usr/bin:/bin");
        // Load checked companion libraries into the runtime only. The bundled
        // Go supervisor receives no caller-controlled dynamic loader path.
        if Path::new(command.get_program()) == executable
            && !directory
                .as_os_str()
                .as_encoded_bytes()
                .iter()
                .any(|b| matches!(b, b':' | b';'))
        {
            #[cfg(target_os = "linux")]
            command.env("LD_LIBRARY_PATH", directory);
            #[cfg(target_os = "macos")]
            command.env("DYLD_LIBRARY_PATH", directory);
        }
    }
    #[cfg(windows)]
    {
        let system = system_directory()?;
        let windows = system.parent().ok_or("Windows 系统目录无效")?;
        command.env("SystemRoot", windows).env("WINDIR", windows);
        let mut paths = vec![directory.to_owned(), system.clone()];
        // Installed CUDA / oneAPI distributions may need their SDK DLLs.
        // Resolve Program Files through Windows, never through the inherited PATH.
        let roots = installed_vendor_roots();
        if let Some(existing) = std::env::var_os("PATH") {
            paths.extend(
                std::env::split_paths(&existing)
                    .filter_map(|p| canonical_local(&p))
                    .filter(|p| p.is_dir() && roots.iter().any(|root| p.starts_with(root))),
            );
        }
        command.env(
            "PATH",
            std::env::join_paths(paths).map_err(|e| e.to_string())?,
        );
    }
    Ok(())
}

#[cfg(windows)]
fn installed_vendor_roots() -> Vec<PathBuf> {
    use windows::Win32::System::Com::CoTaskMemFree;
    use windows::Win32::UI::Shell::{
        FOLDERID_ProgramFiles, FOLDERID_ProgramFilesX86, SHGetKnownFolderPath, KF_FLAG_DEFAULT,
    };
    let mut roots = Vec::new();
    for folder in [FOLDERID_ProgramFiles, FOLDERID_ProgramFilesX86] {
        let Ok(value) = (unsafe { SHGetKnownFolderPath(&folder, KF_FLAG_DEFAULT, None) }) else {
            continue;
        };
        let path = unsafe { value.to_string() };
        unsafe {
            CoTaskMemFree(Some(value.0.cast()));
        }
        if let Ok(path) = path {
            let root = Path::new(&path);
            for vendor in [
                "Intel",
                "IntelSWTools",
                "NVIDIA Corporation",
                "NVIDIA GPU Computing Toolkit",
                "AMD",
            ] {
                if let Some(path) =
                    canonical_local(&root.join(vendor)).filter(|p| p.starts_with(root))
                {
                    roots.push(path);
                }
            }
        }
    }
    roots
}

fn probe(path: &Path, flag: &str, data: &Path) -> Result<String, String> {
    let temporary = data.join(format!(".{}.log", unique_id("runtime-probe")?));
    let result = (|| {
        let mut out = OpenOptions::new()
            .read(true)
            .write(true)
            .create_new(true)
            .open(&temporary)
            .map_err(|e| e.to_string())?;
        let mut cmd = Command::new(path);
        prepare_runtime_environment(&mut cmd, path)?;
        #[cfg(windows)]
        {
            use std::os::windows::process::CommandExt;
            cmd.creation_flags(0x08000000);
        }
        let stdio = || out.try_clone().map(Stdio::from).map_err(|e| e.to_string());
        let mut child = cmd
            .arg(flag)
            .stdin(Stdio::null())
            .stdout(stdio()?)
            .stderr(stdio()?)
            .spawn()
            .map_err(|e| format!("运行时无法启动，请检查系统架构、驱动及动态库：{e}"))?;
        let start = Instant::now();
        loop {
            match child.try_wait() {
                Ok(Some(status)) if status.success() => break,
                Ok(Some(status)) => {
                    out.seek(SeekFrom::Start(0)).map_err(|e| e.to_string())?;
                    let mut details = Vec::new();
                    out.take(2048)
                        .read_to_end(&mut details)
                        .map_err(|e| e.to_string())?;
                    return Err(format!(
                        "运行时 {flag} 失败（{status}），请检查系统架构、驱动及动态库。{}",
                        String::from_utf8_lossy(&details).trim()
                    ));
                }
                Ok(None) if start.elapsed() < Duration::from_secs(12) => {
                    std::thread::sleep(Duration::from_millis(50))
                }
                _ => {
                    let _ = child.kill();
                    let _ = child.wait();
                    return Err("运行时诊断超时，请检查驱动及运行时版本".into());
                }
            }
        }
        out.seek(SeekFrom::Start(0)).map_err(|e| e.to_string())?;
        let mut bytes = Vec::new();
        out.take(64 * 1024)
            .read_to_end(&mut bytes)
            .map_err(|e| e.to_string())?;
        Ok(String::from_utf8_lossy(&bytes).into_owned())
    })();
    let _ = fs::remove_file(&temporary);
    result
}

#[derive(Clone, Deserialize, Serialize)]
pub(super) struct Device {
    pub id: String,
    pub description: String,
    pub total_mib: Option<u64>,
    pub free_mib: Option<u64>,
}
fn devices(output: &str) -> Vec<Device> {
    output
        .lines()
        .skip_while(|l| !l.trim().starts_with("Available devices:"))
        .skip(1)
        .filter_map(|line| {
            let (id, description) = line.trim().split_once(':')?;
            if id.is_empty()
                || id.len() > 64
                || !id
                    .bytes()
                    .all(|b| b.is_ascii_alphanumeric() || matches!(b, b'_' | b'-' | b'.'))
            {
                return None;
            }
            let memory = description
                .rsplit_once('(')
                .and_then(|(_, v)| v.strip_suffix(')'));
            let parse = |text: &str| text.trim().strip_suffix(" MiB")?.parse::<u64>().ok();
            let (total_mib, free_mib) = memory
                .and_then(|v| v.split_once(','))
                .map(|(a, b)| (parse(a), b.trim().strip_suffix(" free").and_then(parse)))
                .unwrap_or_default();
            Some(Device {
                id: id.into(),
                description: description.trim().chars().take(256).collect(),
                total_mib,
                free_mib,
            })
        })
        .take(32)
        .collect()
}

pub(super) struct Consent {
    pub path: PathBuf,
    pub digest: String,
}
impl Consent {
    pub fn inspect(path: &Path) -> Result<Self, String> {
        let path = canonical_local(path)
            .filter(|p| {
                p.is_file()
                    && p.file_name()
                        .is_some_and(|n| n.eq_ignore_ascii_case(SERVER))
            })
            .ok_or("请选择本机 llama-server 可执行文件")?;
        let digest = execution_digest(&path)?;
        Ok(Self { path, digest })
    }
}
pub(super) fn authorize(
    path: &Path,
    data: &Path,
    consent: Option<&Consent>,
) -> Result<Runtime, String> {
    let path = canonical_local(path)
        .filter(|p| p.is_file())
        .ok_or("运行时文件不存在或路径无效")?;
    if !path
        .file_name()
        .is_some_and(|n| n.eq_ignore_ascii_case(SERVER))
    {
        return Err(format!("请选择 {SERVER} 可执行文件"));
    }
    let stamp = Stamp::read(&path)?;
    let package_stamps = package_stamps(&path)?;
    let digest = execution_digest(&path)?;
    if consent.is_some_and(|c| c.path != path || c.digest != digest) {
        return Err("运行时在确认后发生变化，请重新选择并授权".into());
    }
    let output = probe(&path, "--version", data)?;
    let version = output
        .lines()
        .find(|l| l.trim().starts_with("version:"))
        .or_else(|| output.lines().find(|l| !l.trim().is_empty()))
        .ok_or("运行时未报告版本")?
        .trim()
        .chars()
        .take(180)
        .collect();
    // Older CPU-only builds may lack --list-devices; keep their device list unknown.
    let output = probe(&path, "--list-devices", data).unwrap_or_default();
    let device_details = devices(&output);
    let devices = device_details.iter().map(|d| d.id.clone()).collect();
    if Stamp::read(&path)? != stamp || execution_digest(&path)? != digest {
        return Err("诊断期间运行时已更改，请重新授权".into());
    }
    Ok(Runtime {
        id: unique_id("rt")?,
        path,
        version,
        devices,
        digest,
        stamp,
        release_asset: None,
        package_stamps,
        observed: std::sync::OnceLock::new(),
        device_details,
    })
}

// Digests and sizes from github.com/ggml-org/llama.cpp/releases/tag/b12815.
#[derive(Clone, Copy, Serialize)]
pub(super) struct Package {
    pub backend: &'static str,
    pub size: u64,
    #[serde(skip)]
    pub asset: &'static str,
    #[serde(skip)]
    digest: &'static str,
}
const fn pkg(
    backend: &'static str,
    asset: &'static str,
    size: u64,
    digest: &'static str,
) -> Package {
    Package {
        backend,
        asset,
        size,
        digest,
    }
}
const WINDOWS_X86_64: &[Package] = &[
    pkg(
        "cpu",
        "win-cpu-x64.zip",
        25351642,
        "8167d30d05420295ade9b9db3e73d0325aca75b72ced25c030d5dd3d7b0fde5a",
    ),
    pkg(
        "vulkan",
        "win-vulkan-x64.zip",
        39329185,
        "0086709a0ff5334d3a73cccd7f80afef9ec6f7b429ef985b25b38215b12973d0",
    ),
    pkg(
        "sycl",
        "win-sycl-x64.zip",
        174236362,
        "78dbb471456bfbe80a4e7c5ef96b0a30c76585a3734a1b8cebed2ab299102a06",
    ),
    pkg(
        "cuda",
        "win-cuda-12.4-x64.zip",
        370231708,
        "f3bcfedcdff52d409a4e5963c1057f4c39e930219009d1c931659bf4d8cead24",
    ),
];
const LINUX_X86_64: &[Package] = &[
    pkg(
        "cpu",
        "ubuntu-x64.tar.gz",
        23491803,
        "026ecc42e2b8ba93c5d0508a9715f0921aea89a88df6d75eb17831af0ef250e1",
    ),
    pkg(
        "vulkan",
        "ubuntu-vulkan-x64.tar.gz",
        37296919,
        "77ba16a04a2c17f532affcbb80fbf94082da6183f8ba78de3e9c193db25fd266",
    ),
    pkg(
        "sycl",
        "ubuntu-sycl-fp16-x64.tar.gz",
        70458126,
        "c665a4d06265f269ef084947bec53769cae0afd1a322c5df7fd0dbe51f1e0544",
    ),
    pkg(
        "cuda",
        "ubuntu-cuda-12.8-x64.tar.gz",
        227322383,
        "b50e663e11b5f91f16c6ef476c5b380ec91c013f2f56174ea9c3726ea6dfa89e",
    ),
];
const MACOS_AARCH64: &[Package] = &[pkg(
    "metal",
    "macos-arm64.tar.gz",
    16922733,
    "423fb47bdb2146469bf1bdaa7d9e8341777f06f57615bd050d280454a86c1bfa",
)];
const MACOS_X86_64: &[Package] = &[pkg(
    "cpu",
    "macos-x64.tar.gz",
    12652069,
    "f3dc9cc4a4f4d679026c57ed0e6937cbac851d037ab63c8da89c0d016eacde6a",
)];
pub(super) fn packages() -> &'static [Package] {
    match (std::env::consts::OS, std::env::consts::ARCH) {
        ("windows", "x86_64") => WINDOWS_X86_64,
        ("linux", "x86_64") => LINUX_X86_64,
        ("macos", "aarch64") => MACOS_AARCH64,
        ("macos", "x86_64") => MACOS_X86_64,
        _ => &[],
    }
}

pub(super) fn install(data: &Path, backend: Option<&str>) -> Result<Runtime, String> {
    #[cfg(target_os = "macos")]
    {
        let mut value = [0u8; 128];
        let mut len = value.len();
        let ok = unsafe {
            libc::sysctlbyname(
                c"kern.osproductversion".as_ptr(),
                value.as_mut_ptr().cast(),
                &mut len,
                std::ptr::null_mut(),
                0,
            )
        };
        if ok == 0 {
            let parts = String::from_utf8_lossy(&value[..len.min(value.len())]);
            let mut components = parts.trim_end_matches('\0').split('.');
            let major = components
                .next()
                .and_then(|v| v.parse::<u32>().ok())
                .unwrap_or(0);
            let minor = components
                .next()
                .and_then(|v| v.parse::<u32>().ok())
                .unwrap_or(0);
            if (major, minor) < (13, 3) {
                return Err("官方运行时需要 macOS 13.3 或更高版本，请手动选择兼容构建".into());
            }
        }
    }
    #[cfg(all(target_os = "linux", target_env = "gnu"))]
    {
        let version =
            unsafe { std::ffi::CStr::from_ptr(libc::gnu_get_libc_version()) }.to_string_lossy();
        let mut parts = version.split('.');
        let major = parts
            .next()
            .and_then(|v| v.parse::<u32>().ok())
            .unwrap_or(0);
        let minor = parts
            .next()
            .and_then(|v| v.parse::<u32>().ok())
            .unwrap_or(0);
        if (major, minor) < (2, 34) {
            return Err("此官方包需要 glibc 2.34 和 GLIBCXX_3.4.30，请手动选择兼容构建".into());
        }
    }
    let pkg = packages()
        .iter()
        .find(|p| backend.is_none_or(|wanted| wanted == p.backend))
        .ok_or("当前平台无此运行时，请手动选择兼容的 llama-server")?;
    let size = pkg.size;
    let digest = pkg.digest;
    let name = format!("llama-b12815-bin-{}", pkg.asset);
    let root = data.join("inference-runtimes");
    fs::create_dir_all(&root).map_err(|e| e.to_string())?;
    let dest = root.join(unique_id("b12815")?);
    #[cfg(unix)]
    {
        use std::os::unix::fs::DirBuilderExt;
        fs::DirBuilder::new()
            .mode(0o700)
            .create(&dest)
            .map_err(|e| e.to_string())?;
    }
    #[cfg(windows)]
    fs::create_dir(&dest).map_err(|e| e.to_string())?;
    let result = (|| {
        let url = format!("https://github.com/ggml-org/llama.cpp/releases/download/b12815/{name}");
        let response = crate::updates::official_get(
            &url,
            &[
                "github.com",
                "release-assets.githubusercontent.com",
                "objects.githubusercontent.com",
            ],
            Duration::from_secs(300),
        )?;
        let archive = dest.join(name);
        let mut file = OpenOptions::new()
            .create_new(true)
            .write(true)
            .open(&archive)
            .map_err(|e| e.to_string())?;
        crate::updates::verify_and_copy(response.into_reader(), &mut file, size, digest)?;
        drop(file);
        #[cfg(windows)]
        let tar = system_directory()?.join("tar.exe");
        #[cfg(unix)]
        let tar = Path::new("/usr/bin/tar");
        let mut extract = Command::new(&tar);
        prepare_runtime_environment(&mut extract, tar.as_ref())?;
        #[cfg(unix)]
        extract.env("PATH", "/usr/bin:/bin").env_remove("GZIP");
        #[cfg(windows)]
        {
            use std::os::windows::process::CommandExt;
            extract.creation_flags(0x08000000);
        }
        let output = extract
            .arg("-xf")
            .arg(&archive)
            .arg("-C")
            .arg(&dest)
            .env_remove("TAR_OPTIONS")
            .output()
            .map_err(|e| format!("无法解压运行时，请使用手动选择：{e}"))?;
        if !output.status.success() {
            return Err(format!(
                "运行时解压失败：{}",
                String::from_utf8_lossy(&output.stderr)
            ));
        }
        fs::remove_file(&archive).map_err(|e| e.to_string())?;
        let dirs = std::iter::once(dest.clone()).chain(
            fs::read_dir(&dest)
                .map_err(|e| e.to_string())?
                .filter_map(Result::ok)
                .map(|e| e.path()),
        );
        dirs.map(|p| p.join(SERVER))
            .find(|p| p.is_file())
            .ok_or_else(|| "官方发行包缺少 llama-server".into())
    })();
    let result = result.and_then(|path| {
        let mut rt = authorize(&path, data, None)?;
        if matches!(backend, Some("cuda" | "sycl" | "vulkan")) && rt.devices.is_empty() {
            return Err(format!(
                "{} 运行时未报告可用的加速设备，请检查显卡、驱动和厂商运行库，或安装 CPU 版本",
                pkg.backend.to_ascii_uppercase()
            ));
        }
        rt.release_asset = Some(pkg.asset.to_string());
        Ok(rt)
    });
    if result.is_err() {
        let _ = fs::remove_dir_all(dest);
    }
    result
}

#[cfg(windows)]
fn system_directory() -> Result<PathBuf, String> {
    // Resolve from the OS system directory, never the project or user-controlled PATH.
    use windows_sys::Win32::System::SystemInformation::GetSystemDirectoryW;
    let mut dir = [0u16; 32768];
    let n = unsafe { GetSystemDirectoryW(dir.as_mut_ptr(), dir.len() as u32) } as usize;
    if n == 0 || n >= dir.len() {
        return Err("无法定位 Windows 系统目录".into());
    }
    use std::os::windows::ffi::OsStringExt;
    Ok(PathBuf::from(std::ffi::OsString::from_wide(&dir[..n])))
}
