use semver::Version;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::fs::{self, OpenOptions};
use std::io::{self, Read, Write};
use std::path::PathBuf;
use std::time::Duration;
use tauri::{AppHandle, Manager};

const REPOSITORY: &str = "2722039978-source/Noosphere";
const MAX_EXE_BYTES: u64 = 1024 * 1024 * 1024;
const MAX_CHAT_BYTES: usize = 128 * 1024 * 1024;
const ALLOWED_DOWNLOAD_HOSTS: &[&str] = &[
    "github.com",
    "release-assets.githubusercontent.com",
    "objects.githubusercontent.com",
];

#[derive(Deserialize)]
struct Release {
    tag_name: String,
    draft: bool,
    prerelease: bool,
    assets: Vec<Asset>,
}

#[derive(Deserialize)]
struct Asset {
    name: String,
    size: u64,
}

#[derive(Serialize)]
pub(crate) struct UpdateInfo {
    current_version: String,
    available_version: Option<String>,
    installer_available: bool,
    downloaded_version: Option<String>,
}

#[derive(Serialize)]
pub(crate) struct DownloadedUpdate {
    version: String,
    installer: String,
    chat_backup: String,
}

fn release_version(tag: &str) -> Option<Version> {
    let text = tag.strip_prefix('v')?;
    let version = Version::parse(text).ok()?;
    if !version.build.is_empty() {
        return None;
    }
    if version.pre.is_empty() {
        return Some(version);
    }
    let suffix = version.pre.as_str();
    if suffix.len() == 8 && suffix.bytes().all(|byte| byte.is_ascii_digit()) {
        return Some(Version::new(version.major, version.minor, version.patch));
    }
    None
}

fn installer_name(version: &Version) -> String {
    format!("Noosphere-{version}-windows-x64-setup.exe")
}

fn download_url(tag: &str, name: &str) -> Result<String, String> {
    let version = release_version(tag).ok_or("发布版本格式无效")?;
    if name != "SHA256SUMS.txt" && name != installer_name(&version) {
        return Err("更新文件名无效".into());
    }
    Ok(format!("https://github.com/{REPOSITORY}/releases/download/{tag}/{name}"))
}

fn trusted_https(url: &str, allowed_hosts: &[&str]) -> bool {
    let Ok(parsed) = reqwest::Url::parse(url) else {
        return false;
    };
    parsed.scheme() == "https"
        && parsed.port_or_known_default() == Some(443)
        && parsed.username().is_empty()
        && parsed.password().is_none()
        && parsed.host_str().is_some_and(|host| allowed_hosts.contains(&host))
}

fn official_get(url: &str, hosts: &[&str], timeout: Duration) -> Result<ureq::Response, String> {
    let agent = ureq::AgentBuilder::new().redirects(0).timeout(timeout).build();
    let mut target = url.to_owned();
    for _ in 0..4 {
        if !trusted_https(&target, hosts) {
            return Err("更新地址超出允许的 GitHub 域".into());
        }
        let response = agent
            .get(&target)
            .set("User-Agent", "Noosphere Desktop")
            .set("Accept-Encoding", "identity")
            .call()
            .map_err(|error| format!("无法连接官方发布服务: {error}"))?;
        match response.status() {
            200 => return Ok(response),
            301 | 302 | 303 | 307 | 308 => {
                let location = response.header("Location").ok_or("更新重定向缺少地址")?;
                target = reqwest::Url::parse(&target)
                    .and_then(|url| url.join(location))
                    .map_err(|_| "更新重定向地址无效")?
                    .to_string();
            }
            status => return Err(format!("官方发布服务返回 HTTP {status}")),
        }
    }
    Err("官方发布服务重定向过多".into())
}

fn read_text(response: ureq::Response, limit: u64) -> Result<String, String> {
    let mut body = String::new();
    response.into_reader().take(limit + 1).read_to_string(&mut body).map_err(io_error)?;
    if body.len() as u64 > limit {
        return Err("更新元数据超出大小限制".into());
    }
    Ok(body)
}

fn latest_release() -> Result<(Version, Release), String> {
    let response = official_get(
        &format!("https://api.github.com/repos/{REPOSITORY}/releases/latest"),
        &["api.github.com"],
        Duration::from_secs(15),
    )?;
    let release: Release = serde_json::from_str(&read_text(response, 1024 * 1024)?)
        .map_err(|error| format!("发布元数据无效: {error}"))?;
    if release.draft || release.prerelease {
        return Err("官方发布服务返回非正式版本".into());
    }
    let version = release_version(&release.tag_name).ok_or("发布版本格式无效")?;
    Ok((version, release))
}

fn checksum(contents: &str, name: &str) -> Result<String, String> {
    let mut found = None;
    for row in contents.trim_start_matches('\u{feff}').lines() {
        let Some((hash, filename)) = row.split_once(char::is_whitespace) else { continue };
        let filename = filename.trim().strip_prefix('*').unwrap_or(filename.trim());
        if filename != name {
            continue;
        }
        if found.is_some() || hash.len() != 64 || !hash.bytes().all(|byte| byte.is_ascii_hexdigit()) {
            return Err("安装包 SHA-256 清单无效".into());
        }
        found = Some(hash.to_ascii_lowercase());
    }
    found.ok_or_else(|| "官方清单缺少此安装包的 SHA-256".into())
}

fn app_update_dir(app: &AppHandle) -> Result<PathBuf, String> {
    Ok(app.path().app_local_data_dir().map_err(|err| err.to_string())?.join("updates"))
}

fn io_error(error: io::Error) -> String {
    format!("更新文件操作失败: {error}")
}

#[tauri::command]
pub(crate) async fn update_check(app: AppHandle) -> Result<UpdateInfo, String> {
    let directory = app_update_dir(&app)?;
    let current = app.package_info().version.clone();
    tauri::async_runtime::spawn_blocking(move || {
        let downloaded_version = fs::read_to_string(directory.join("last-download.txt")).ok();
        let (version, release) = latest_release()?;
        let installer_available = version > current
            && release.assets.iter().any(|entry| entry.name == installer_name(&version))
            && release.assets.iter().any(|entry| entry.name == "SHA256SUMS.txt");
        Ok(UpdateInfo {
            current_version: current.to_string(),
            available_version: (version > current).then(|| version.to_string()),
            installer_available,
            downloaded_version,
        })
    }).await.map_err(|err| err.to_string())?
}

fn verify_and_copy(
    reader: impl Read,
    output: &mut impl Write,
    expected_size: u64,
    expected_hash: &str,
) -> Result<(), String> {
    let mut reader = reader.take(expected_size + 1);
    let mut bytes = [0; 64 * 1024];
    let mut hash = Sha256::new();
    let mut size = 0;
    loop {
        let read = reader.read(&mut bytes).map_err(io_error)?;
        if read == 0 {
            break;
        }
        size += read as u64;
        hash.update(&bytes[..read]);
        output.write_all(&bytes[..read]).map_err(io_error)?;
    }
    if size != expected_size || format!("{:x}", hash.finalize()) != expected_hash {
        return Err("安装包大小或 SHA-256 与官方发布记录不符".into());
    }
    Ok(())
}

#[tauri::command]
pub(crate) async fn update_download(app: AppHandle, expected_version: String, chat_backup: String) -> Result<DownloadedUpdate, String> {
    if chat_backup.len() > MAX_CHAT_BYTES {
        return Err("会话备份过大，请先在会话备份面板导出".into());
    }
    let directory = app_update_dir(&app)?;
    let current = app.package_info().version.clone();
    tauri::async_runtime::spawn_blocking(move || {
        let (version, release) = latest_release()?;
        if version <= current || version.to_string() != expected_version {
            return Err("官方发布版本已变化，请重新检查更新".into());
        }
        let name = installer_name(&version);
        let installer = release.assets.iter().find(|entry| entry.name == name)
            .filter(|entry| (1..=MAX_EXE_BYTES).contains(&entry.size))
            .ok_or("官方版本未发布支持的 Windows x64 安装包")?;
        if !release.assets.iter().any(|entry| entry.name == "SHA256SUMS.txt") {
            return Err("官方版本未发布 SHA-256 清单".into());
        }
        let sums_url = download_url(&release.tag_name, "SHA256SUMS.txt")?;
        let expected_hash = checksum(
            &read_text(official_get(&sums_url, ALLOWED_DOWNLOAD_HOSTS, Duration::from_secs(15))?, 128 * 1024)?,
            &name,
        )?;
        fs::create_dir_all(&directory).map_err(io_error)?;
        let mut random = [0; 8];
        getrandom::fill(&mut random).map_err(|err| err.to_string())?;
        let unique_path = directory.join(format!("{version}-{:016x}", u64::from_le_bytes(random)));
        fs::create_dir(&unique_path).map_err(io_error)?;
        let stage = unique_path.join("installer.partial");
        let final_path = unique_path.join(&name);
        let backup_path = unique_path.join("conversations.json");
        let result = (|| {
            let mut output = OpenOptions::new().write(true).create_new(true).open(&stage).map_err(io_error)?;
            let url = download_url(&release.tag_name, &name)?;
            let response = official_get(&url, ALLOWED_DOWNLOAD_HOSTS, Duration::from_secs(15 * 60))?;
            verify_and_copy(response.into_reader(), &mut output, installer.size, &expected_hash)?;
            output.sync_all().map_err(io_error)?;
            drop(output);
            let mut backup = OpenOptions::new().write(true).create_new(true).open(&backup_path).map_err(io_error)?;
            backup.write_all(chat_backup.as_bytes()).map_err(io_error)?;
            backup.sync_all().map_err(io_error)?;
            fs::rename(&stage, &final_path).map_err(io_error)?;
            fs::write(directory.join("last-download.txt"), version.to_string()).map_err(io_error)?;
            Ok(DownloadedUpdate {
                version: version.to_string(),
                installer: final_path.display().to_string(),
                chat_backup: backup_path.display().to_string(),
            })
        })();
        if result.is_err() {
            let _ = fs::remove_file(stage);
        }
        result
    }).await.map_err(|err| err.to_string())?
}

#[tauri::command]
pub(crate) fn update_show_folder(app: AppHandle) -> Result<(), String> {
    let directory = app_update_dir(&app)?;
    #[cfg(windows)]
    {
        let explorer = std::env::var_os("SystemRoot").map(PathBuf::from)
            .ok_or("无法找到 Windows 资源管理器")?.join("explorer.exe");
        std::process::Command::new(explorer).arg(&directory).spawn().map_err(io_error)?;
        Ok(())
    }
    #[cfg(not(windows))]
    {
        let _ = directory;
        Err("此平台不支持 Windows 安装包".into())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn accepts_formal_tags_and_legacy_date_tags() {
        assert_eq!(release_version("v0.7.1").unwrap(), Version::new(0, 7, 1));
        assert_eq!(release_version("v0.6.0-20261114").unwrap(), Version::new(0, 6, 0));
        assert!(release_version("v0.7.1-beta.1").is_none());
        assert!(release_version("0.7.1").is_none());
    }

    #[test]
    fn requires_exact_unique_installer_checksum() {
        let installer = "Noosphere-0.7.0-windows-x64-setup.exe";
        let hash = "a".repeat(64);
        let line = format!("{hash}  {installer}");
        assert_eq!(checksum(&line, installer).unwrap(), hash);
        assert!(checksum(&format!("{line}\n{line}"), installer).is_err());
        assert!(checksum(&format!("{hash}  ../{installer}"), installer).is_err());
        assert!(checksum(&format!("short  {installer}"), installer).is_err());
    }

    #[test]
    fn rejects_official_path_lookalikes_and_non_https_redirects() {
        assert!(download_url("v0.7.0", "../Noosphere.exe").is_err());
        assert!(!trusted_https("http://github.com/path", &["github.com"]));
        assert!(!trusted_https("https://github.com.example.org/path", &["github.com"]));
        assert!(!trusted_https("https://github.com@other.example/path", &["github.com"]));
    }

    #[test]
    fn validates_installer_hash_and_length_before_publication() {
        let installer = b"test installer payload";
        let expected = format!("{:x}", Sha256::digest(installer));
        let mut output = Vec::new();
        let size = installer.len() as u64;
        verify_and_copy(&installer[..], &mut output, size, &expected).unwrap();
        assert_eq!(output, installer);
        assert!(verify_and_copy(&installer[..], &mut Vec::new(), size - 1, &expected).is_err());
        assert!(verify_and_copy(&installer[..], &mut Vec::new(), size + 1, &expected).is_err());
        assert!(verify_and_copy(&installer[..], &mut Vec::new(), size, &"0".repeat(64)).is_err());
    }
}
