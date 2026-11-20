use super::{file_at, home_path, EngineId};
#[cfg(target_os = "macos")]
use std::path::Path;
use std::path::PathBuf;

pub(super) fn installed(id: EngineId) -> (Option<PathBuf>, bool) {
    let launcher = match id {
        EngineId::Ollama => ollama(),
        EngineId::LmStudio => lm_studio(),
        _ => None,
    };
    let detected = launcher.is_some() || id == EngineId::LmStudio && lm_studio_app();
    (launcher, detected)
}

#[cfg(unix)]
fn first_file(entries: &[&str]) -> Option<PathBuf> {
    entries
        .iter()
        .find_map(|entry| file_at(PathBuf::from(entry)))
}

#[cfg(windows)]
fn local_app(relative: &str) -> Option<PathBuf> {
    std::env::var_os("LOCALAPPDATA")
        .map(PathBuf::from)
        .and_then(|home| file_at(home.join(relative)))
}

#[cfg(windows)]
fn windows_path(executable: &str) -> Option<PathBuf> {
    let path = std::env::var_os("PATH")?;
    std::env::split_paths(&path)
        .take(256)
        .filter(|directory| directory.is_absolute())
        .find_map(|directory| file_at(directory.join(executable)))
}

#[cfg(windows)]
fn ollama() -> Option<PathBuf> {
    local_app("Programs/Ollama/ollama.exe").or_else(|| windows_path("ollama.exe"))
}

#[cfg(target_os = "macos")]
fn ollama() -> Option<PathBuf> {
    first_file(&[
        "/Applications/Ollama.app/Contents/Resources/ollama",
        "/opt/homebrew/bin/ollama",
        "/usr/local/bin/ollama",
    ])
    .or_else(|| home_path("Applications/Ollama.app/Contents/Resources/ollama").and_then(file_at))
}

#[cfg(target_os = "linux")]
fn ollama() -> Option<PathBuf> {
    first_file(&["/usr/local/bin/ollama", "/usr/bin/ollama"])
        .or_else(|| home_path(".local/bin/ollama").and_then(file_at))
}

#[cfg(windows)]
fn lm_studio() -> Option<PathBuf> {
    home_path(".lmstudio/bin/lms.exe")
        .and_then(file_at)
        .or_else(|| windows_path("lms.exe"))
}

#[cfg(unix)]
fn lm_studio() -> Option<PathBuf> {
    home_path(".lmstudio/bin/lms").and_then(file_at)
}

#[cfg(windows)]
fn lm_studio_app() -> bool {
    [
        "Programs/LM Studio/LM Studio.exe",
        "LM Studio/LM Studio.exe",
    ]
    .into_iter()
    .any(|entry| local_app(entry).is_some())
}

#[cfg(target_os = "macos")]
fn lm_studio_app() -> bool {
    Path::new("/Applications/LM Studio.app").is_dir()
        || home_path("Applications/LM Studio.app").is_some_and(|path| path.is_dir())
}

#[cfg(target_os = "linux")]
fn lm_studio_app() -> bool {
    // An AppImage may be stored anywhere; its running HTTP service is discovered separately.
    first_file(&["/usr/local/bin/lm-studio", "/opt/LM Studio/lm-studio"]).is_some()
}

pub(super) fn listener_matches(port: u16, pid: u32) -> bool {
    #[cfg(windows)]
    return super::windows::ListenerOwner::observe(port).is_some_and(|owner| owner.pid() == pid);
    #[cfg(target_os = "macos")]
    return mac_listener_matches(port, pid);
    #[cfg(target_os = "linux")]
    return linux_listener_matches(port, pid);
}

#[cfg(target_os = "macos")]
fn mac_listener_matches(port: u16, pid: u32) -> bool {
    std::process::Command::new("/usr/sbin/lsof")
        .args([
            "-nP",
            "-a",
            "-p",
            &pid.to_string(),
            &format!("-i4TCP@127.0.0.1:{port}"),
            "-sTCP:LISTEN",
            "-Fp",
        ])
        .output()
        .is_ok_and(|result| {
            result.status.success()
                && String::from_utf8_lossy(&result.stdout)
                    .lines()
                    .any(|line| line == format!("p{pid}"))
        })
}

#[cfg(target_os = "linux")]
fn linux_listener_matches(port: u16, pid: u32) -> bool {
    use std::fs;
    let Ok(table) = fs::read_to_string("/proc/net/tcp") else {
        return false;
    };
    let address = format!("0100007F:{port:04X}");
    let mut sockets = table.lines().filter_map(|line| {
        let columns: Vec<_> = line.split_ascii_whitespace().take(10).collect();
        (columns.len() == 10 && columns[1] == address && columns[3] == "0A").then(|| columns[9])
    });
    let Some(inode) = sockets.next().filter(|_| sockets.next().is_none()) else {
        return false;
    };
    let expected = format!("socket:[{inode}]");
    fs::read_dir(format!("/proc/{pid}/fd")).is_ok_and(|entries| {
        entries
            .flatten()
            .filter_map(|entry| fs::read_link(entry.path()).ok())
            .any(|link| link.as_os_str() == expected.as_str())
    })
}
