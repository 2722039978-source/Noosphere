use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};
use std::sync::OnceLock;

#[derive(Deserialize)]
pub(crate) struct Target {
    pub(crate) os: String,
    pub(crate) arch: String,
    pub(crate) minimum_os: String,
    pub(crate) abi: String,
    pub(crate) components: BTreeMap<String, Component>,
    pub(crate) packages: BTreeMap<String, String>,
}

#[derive(Deserialize)]
pub(crate) struct Component {
    pub(crate) entry: String,
}

#[derive(Deserialize)]
struct TargetManifest {
    schema: u32,
    targets: BTreeMap<String, Target>,
}

static MANIFEST: OnceLock<Result<TargetManifest, String>> = OnceLock::new();

fn manifest() -> Result<&'static TargetManifest, String> {
    MANIFEST
        .get_or_init(|| {
            let manifest: TargetManifest =
                serde_json::from_str(include_str!("../../../scripts/platform-targets.json"))
                    .map_err(|error| error.to_string())?;
            if manifest.schema != 1 {
                return Err("目标清单版本不受支持".into());
            }
            Ok(manifest)
        })
        .as_ref()
        .map_err(Clone::clone)
}

pub(crate) fn target() -> Result<&'static Target, String> {
    let arch = match std::env::consts::ARCH {
        "x86_64" => "x64",
        "aarch64" => "arm64",
        arch => arch,
    };
    manifest()?
        .targets
        .get(&format!("{}-{arch}", std::env::consts::OS))
        .filter(|target| target.os == std::env::consts::OS && target.arch == arch)
        .ok_or_else(|| "此系统或架构不在 Noosphere 桌面目标清单中".into())
}

impl Target {
    pub(crate) fn entry(&self, name: &str) -> Result<&str, String> {
        self.components
            .get(name)
            .map(|component| component.entry.as_str())
            .ok_or_else(|| format!("目标清单缺少 {name} 运行入口"))
    }

    pub(crate) fn package_name(&self, version: &semver::Version, kind: &str) -> Option<String> {
        self.packages
            .get(kind)
            .map(|pattern| pattern.replace("{version}", &version.to_string()))
    }
}

#[derive(Serialize)]
pub(crate) struct DesktopPlatform {
    os: &'static str,
    arch: String,
    native_titlebar: bool,
    supported: bool,
    available: bool,
    verification: &'static str,
    minimum_os: Option<String>,
    abi: Option<String>,
    reason: Option<String>,
}

#[tauri::command]
pub(crate) fn desktop_platform() -> DesktopPlatform {
    let host = target();
    DesktopPlatform {
        os: std::env::consts::OS,
        arch: host
            .as_ref()
            .map(|host| host.arch.clone())
            .unwrap_or_else(|_| std::env::consts::ARCH.into()),
        native_titlebar: cfg!(target_os = "macos"),
        supported: host.is_ok(),
        available: host.is_ok(),
        verification: "target_manifest",
        minimum_os: host.as_ref().ok().map(|host| host.minimum_os.clone()),
        abi: host.as_ref().ok().map(|host| host.abi.clone()),
        reason: host.err(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::path::Component as PathComponent;

    #[test]
    fn target_entries_are_relative_and_assets_are_architecture_specific() {
        let targets = &manifest().unwrap().targets;
        let requested = [
            (
                "windows-x64",
                "nsis",
                "Noosphere-0.9.0-windows-x64-setup.exe",
            ),
            ("linux-x64", "deb", "Noosphere-0.9.0-linux-x64.deb"),
            (
                "linux-x64",
                "appimage",
                "Noosphere-0.9.0-linux-x64.AppImage",
            ),
            ("macos-arm64", "dmg", "Noosphere-0.9.0-macos-arm64.dmg"),
            ("macos-x64", "dmg", "Noosphere-0.9.0-macos-x64.dmg"),
        ];
        for (id, kind, expected) in requested {
            let target = &targets[id];
            assert_eq!(id, format!("{}-{}", target.os, target.arch));
            assert_eq!(
                target
                    .package_name(&semver::Version::new(0, 9, 0), kind)
                    .as_deref(),
                Some(expected)
            );
            for component in ["go", "python", "node", "codelens", "rea"] {
                let entry = target.entry(component).unwrap();
                assert!(!entry.is_empty());
                assert!(Path::new(entry)
                    .components()
                    .all(|component| matches!(component, PathComponent::Normal(_))));
            }
        }
    }
}

pub(crate) fn open_directory(path: &Path) -> Result<(), String> {
    if !path.is_absolute() || !path.is_dir() {
        return Err("目录不存在或路径不是绝对路径".into());
    }
    #[cfg(windows)]
    let opener = std::env::var_os("SystemRoot")
        .map(PathBuf::from)
        .filter(|root| root.is_absolute())
        .ok_or("无法定位 Windows 资源管理器")?
        .join("explorer.exe");
    #[cfg(target_os = "macos")]
    let opener = PathBuf::from("/usr/bin/open");
    #[cfg(target_os = "linux")]
    let opener = PathBuf::from("/usr/bin/xdg-open");
    let mut command = Command::new(opener);
    #[cfg(target_os = "linux")]
    command.env("PATH", "/usr/local/bin:/usr/bin:/bin");
    let mut child = command
        .arg(path)
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .map_err(|error| format!("无法打开目录：{error}"))?;
    std::thread::spawn(move || {
        let _ = child.wait();
    });
    Ok(())
}

#[cfg(unix)]
pub(crate) fn own_process_group(command: &mut Command) {
    use std::os::unix::process::CommandExt;
    command.process_group(0);
}

#[cfg(unix)]
fn child_exited_without_reaping(child: &std::process::Child) -> std::io::Result<bool> {
    // SAFETY: waitid writes to a valid siginfo_t. WNOWAIT keeps the PID owned
    // until we have signalled the group and reaped the leader ourselves.
    let mut status: libc::siginfo_t = unsafe { std::mem::zeroed() };
    if unsafe {
        libc::waitid(
            libc::P_PID,
            child.id() as libc::id_t,
            &mut status,
            libc::WEXITED | libc::WNOHANG | libc::WNOWAIT,
        )
    } != 0
    {
        return Err(std::io::Error::last_os_error());
    }
    Ok(unsafe { status.si_pid() } != 0)
}

#[cfg(unix)]
pub(crate) fn terminate_process_group(child: &std::process::Child) {
    if child_exited_without_reaping(child).is_ok() {
        // SAFETY: kill accepts a negative process-group ID and no pointers.
        unsafe {
            libc::kill(-(child.id() as libc::pid_t), libc::SIGKILL);
        }
    }
}

pub(crate) fn poll_owned_child(
    child: &mut std::process::Child,
) -> std::io::Result<Option<std::process::ExitStatus>> {
    #[cfg(unix)]
    match child_exited_without_reaping(child) {
        Ok(false) => return Ok(None),
        Ok(true) => {
            terminate_process_group(child);
            return child.wait().map(Some);
        }
        Err(error) if error.raw_os_error() != Some(libc::ECHILD) => return Err(error),
        Err(_) => {} // The ExitStatus is already cached by Child.
    }
    child.try_wait()
}
