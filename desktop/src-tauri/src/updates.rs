use semver::Version;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::fs::{self, OpenOptions};
use std::io::{self, Read, Write};
#[cfg(any(windows, test))]
use std::path::Path;
use std::path::PathBuf;
use std::time::Duration;
use tauri::AppHandle;

const REPOSITORY: &str = "2722039978-source/Noosphere";
const MAX_PACKAGE_BYTES: u64 = 1024 * 1024 * 1024;
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
    package_type: Option<&'static str>,
}

#[derive(Serialize)]
pub(crate) struct DownloadedUpdate {
    version: String,
    installer: String,
    chat_backup: String,
    package_type: &'static str,
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

#[cfg(any(windows, test))]
fn windows_package_type(executable: &Path) -> Option<&'static str> {
    let filename = executable.file_name()?.to_str()?;
    if filename.eq_ignore_ascii_case("Noosphere.exe") {
        return Some("zip");
    }
    let directory = executable.parent()?;
    (filename.eq_ignore_ascii_case("noosphere-desktop.exe") && directory.join("uninstall.exe").is_file())
        .then_some("nsis")
}

fn package_type() -> Option<&'static str> {
    #[cfg(windows)]
    {
        windows_package_type(&std::env::current_exe().ok()?)
    }
    #[cfg(target_os = "macos")]
    {
        Some("dmg")
    }
    #[cfg(target_os = "linux")]
    {
        let executable = std::env::current_exe().ok()?;
        if std::env::var_os("APPIMAGE").map(PathBuf::from).is_some_and(|path| path.is_absolute() && path.is_file())
            && std::env::var_os("APPDIR").map(PathBuf::from)
                .is_some_and(|root| root.is_absolute() && executable.starts_with(root))
        {
            return Some("appimage");
        }
        executable.starts_with("/usr/bin").then_some("deb")
    }
}

fn package_name(version: &Version, kind: &str) -> Result<String, String> {
    crate::platform::target()?.package_name(version, kind)
        .ok_or_else(|| "本目标没有对应的更新包".into())
}

fn download_url(tag: &str, name: &str) -> Result<String, String> {
    let version = release_version(tag).ok_or("发布版本格式无效")?;
    if name != "SHA256SUMS.txt" && !crate::platform::target()?.packages.values()
        .any(|pattern| pattern.replace("{version}", &version.to_string()) == name) {
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

// Honor explicit user proxy settings on remote package requests only. Engine probes
// continue using their dedicated loopback client.
fn bypass_proxy(host: &str, patterns: &str) -> bool {
    patterns.split([',', ';']).any(|item| {
        let item = item.trim().to_ascii_lowercase();
        let pattern = item.strip_suffix(":443").unwrap_or(&item);
        if pattern == "*" || pattern == host {
            return true;
        }
        if let Some(suffix) = pattern.strip_prefix('*') {
            return !suffix.is_empty() && host.ends_with(suffix);
        }
        if let Some(domain) = pattern.strip_prefix('.') {
            return host == domain || host.ends_with(pattern);
        }
        false
    })
}

#[cfg(windows)]
fn windows_proxy(host: &str) -> Option<String> {
    use windows_sys::Win32::Foundation::GlobalFree;
    use windows_sys::Win32::Networking::WinHttp::{
        WinHttpGetIEProxyConfigForCurrentUser, WINHTTP_CURRENT_USER_IE_PROXY_CONFIG,
    };
    let mut config = WINHTTP_CURRENT_USER_IE_PROXY_CONFIG::default();
    if unsafe { WinHttpGetIEProxyConfigForCurrentUser(&mut config) } == 0 {
        return None;
    }
    // All three optional strings are allocated by WinHTTP and owned by the caller.
    fn take(pointer: *mut u16) -> String {
        if pointer.is_null() {
            return String::new();
        }
        unsafe {
            let mut len = 0;
            while *pointer.add(len) != 0 {
                len += 1;
            }
            let value = String::from_utf16_lossy(std::slice::from_raw_parts(pointer, len));
            GlobalFree(pointer.cast());
            value
        }
    }
    let _pac = take(config.lpszAutoConfigUrl);
    let proxy = take(config.lpszProxy);
    let bypass = take(config.lpszProxyBypass);
    if bypass_proxy(host, &bypass) {
        return None;
    }
    let selected = proxy
        .split([';', ' '])
        .filter_map(|v| v.trim().strip_prefix("https="))
        .next()
        .or_else(|| {
            proxy
                .split([';', ' '])
                .map(str::trim)
                .find(|v| !v.is_empty() && !v.contains('='))
        })?;
    Some(if selected.contains("://") {
        selected.into()
    } else {
        format!("http://{selected}")
    })
}

fn download_proxy(host: &str) -> Result<Option<ureq::Proxy>, String> {
    let bypass = std::env::var("no_proxy")
        .or_else(|_| std::env::var("NO_PROXY"))
        .unwrap_or_default();
    if bypass_proxy(host, &bypass) {
        return Ok(None);
    }
    let proxy = ["https_proxy", "HTTPS_PROXY", "all_proxy", "ALL_PROXY"]
        .iter()
        .find_map(|key| std::env::var(key).ok().filter(|v| !v.is_empty()));
    #[cfg(windows)]
    let proxy = proxy.or_else(|| windows_proxy(host));
    proxy
        .map(ureq::Proxy::new)
        .transpose()
        .map_err(|_| "系统代理配置无效，请检查 HTTPS 代理".into())
}

pub(crate) fn official_get(
    url: &str,
    hosts: &[&str],
    timeout: Duration,
) -> Result<ureq::Response, String> {
    let mut target = url.to_owned();
    for _ in 0..4 {
        if !trusted_https(&target, hosts) {
            return Err("更新地址超出允许的 GitHub 域".into());
        }
        let parsed = reqwest::Url::parse(&target).map_err(|_| "下载地址无效")?;
        let mut builder = ureq::AgentBuilder::new()
            .redirects(0)
            .try_proxy_from_env(false)
            .timeout_connect(Duration::from_secs(15))
            .timeout(timeout);
        if let Some(proxy) = download_proxy(parsed.host_str().ok_or("下载地址缺少主机名")?)?
        {
            builder = builder.proxy(proxy);
        }
        let response = builder
            .build()
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
    let executable = std::env::current_exe().map_err(io_error)?;
    let directory = executable.parent().ok_or("应用程序路径没有父目录")?;
    let default = crate::runtime::default_data_dir(app)?;
    let override_dir = std::env::var_os("NOOSPHERE_DATA_DIR").map(PathBuf::from);
    Ok(crate::runtime::choose_data_dir(directory, &default, override_dir.as_deref())?.join("updates"))
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
        let package_type = package_type();
        let name = package_type.map(|kind| package_name(&version, kind)).transpose()?;
        let installer_available = version > current
            && name.as_ref().is_some_and(|name| release_asset(&release, name).is_some())
            && release_asset(&release, "SHA256SUMS.txt").is_some();
        Ok(UpdateInfo {
            current_version: current.to_string(),
            available_version: (version > current).then(|| version.to_string()),
            installer_available,
            downloaded_version,
            package_type,
        })
    }).await.map_err(|err| err.to_string())?
}

fn release_asset<'a>(release: &'a Release, name: &str) -> Option<&'a Asset> {
    let mut matches = release.assets.iter().filter(|entry| entry.name == name);
    let selected = matches.next()?;
    (matches.next().is_none() && (1..=MAX_PACKAGE_BYTES).contains(&selected.size)).then_some(selected)
}

pub(crate) fn verify_and_copy(
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
        let package_type = package_type().ok_or("无法识别当前安装方式，请手动下载与系统和架构匹配的包")?;
        let name = package_name(&version, package_type)?;
        let installer = release_asset(&release, &name)
            .ok_or("官方版本尚未提供与当前系统、架构及安装方式匹配的更新包")?;
        if release_asset(&release, "SHA256SUMS.txt").is_none() {
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
        #[cfg(unix)]
        {
            use std::os::unix::fs::DirBuilderExt;
            fs::DirBuilder::new().mode(0o700).create(&unique_path).map_err(io_error)?;
        }
        #[cfg(windows)]
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
                package_type,
            })
        })();
        if result.is_err() {
            let _ = fs::remove_dir_all(unique_path);
        }
        result
    }).await.map_err(|err| err.to_string())?
}

#[tauri::command]
pub(crate) fn update_show_folder(app: AppHandle) -> Result<(), String> {
    crate::platform::open_directory(&app_update_dir(&app)?)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn windows_updates_keep_the_installation_format() {
        let root = std::env::temp_dir().join(format!("noosphere-update-{}", std::process::id()));
        fs::create_dir(&root).unwrap();
        let installed = root.join("noosphere-desktop.exe");
        let portable = root.join("Noosphere.exe");
        assert_eq!(windows_package_type(&installed), None);
        fs::write(root.join("uninstall.exe"), b"").unwrap();
        assert_eq!(windows_package_type(&installed), Some("nsis"));
        assert_eq!(windows_package_type(&portable), Some("zip"));
        fs::remove_dir_all(root).unwrap();
    }

    #[test]
    fn accepts_formal_tags_and_legacy_date_tags() {
        let version = "1.0.0";
        assert_eq!(release_version(&format!("v{version}")).unwrap(), Version::parse(version).unwrap());
        assert_eq!(release_version("v0.6.0-20261114").unwrap(), Version::new(0, 6, 0));
        assert!(release_version(&format!("v{version}-beta.1")).is_none());
        assert!(release_version("v1.0.0-rc.1").is_none());
        assert!(release_version(version).is_none());
    }

    #[test]
    fn requires_exact_unique_installer_checksum() {
        let installer = concat!("Noosphere-", env!("CARGO_PKG_VERSION"), "-windows-x64-setup.exe");
        let hash = "a".repeat(64);
        let line = format!("{hash}  {installer}");
        assert_eq!(checksum(&line, installer).unwrap(), hash);
        assert!(checksum(&format!("{line}\n{line}"), installer).is_err());
        assert!(checksum(&format!("{hash}  ../{installer}"), installer).is_err());
        assert!(checksum(&format!("short  {installer}"), installer).is_err());
    }

    #[test]
    fn rejects_official_path_lookalikes_and_non_https_redirects() {
        assert!(download_url(concat!("v", env!("CARGO_PKG_VERSION")), "../Noosphere.exe").is_err());
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
