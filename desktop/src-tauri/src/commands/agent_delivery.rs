use super::projects::select_folder;
use super::types::ApiError;
use super::workspaces::{request_with_timeout, validate_id};
use serde_json::{json, Value};
use sha2::{Digest, Sha256};
use std::io::{self, Read, Write};
use std::path::Path;
use std::time::Duration;

const PREVIEW_LIMIT: u64 = 8 * 1024 * 1024;

fn run_path(workspace: &str, run: &str) -> Result<String, ApiError> {
    validate_id(workspace)?;
    validate_id(run)?;
    Ok(format!("/api/workspaces/{workspace}/runs/{run}"))
}

#[tauri::command]
pub async fn agent_render_packs(kind: Option<String>) -> Result<Value, ApiError> {
    if kind
        .as_deref()
        .is_some_and(|v| !matches!(v, "html" | "office"))
    {
        return Err(ApiError::new("无效排版器"));
    }
    request_with_timeout(
        "/api/agent/render-pack".into(),
        kind.map(|kind| json!({"kind":kind})),
        Duration::from_secs(16 * 60),
    )
    .await
}

#[tauri::command]
pub async fn agent_toolpack(install: bool) -> Result<Value, ApiError> {
    request_with_timeout(
        "/api/agent/toolpack".into(),
        install.then(|| json!({})),
        Duration::from_secs(300),
    )
    .await
}

#[tauri::command]
pub async fn agent_grant(
    workspace_id: String,
    binding_id: Option<String>,
) -> Result<Value, ApiError> {
    validate_id(&workspace_id)?;
    if let Some(id) = &binding_id {
        validate_id(id)?;
    }
    request_with_timeout(
        format!("/api/workspaces/{workspace_id}/agent-grant"),
        binding_id.map(|id| json!({"binding_id":id})),
        Duration::from_secs(15),
    )
    .await
}

#[tauri::command]
pub async fn agent_control(
    workspace_id: String,
    run_id: String,
    control: Value,
) -> Result<Value, ApiError> {
    let path = run_path(&workspace_id, &run_id)?;
    request_with_timeout(
        format!("{path}/control"),
        Some(control),
        Duration::from_secs(120),
    )
    .await
}

#[tauri::command]
pub async fn agent_delivery(
    workspace_id: String,
    run_id: String,
    change_offset: Option<usize>,
) -> Result<Value, ApiError> {
    let path = run_path(&workspace_id, &run_id)?;
    request_with_timeout(
        format!(
            "{path}/delivery?change_offset={}",
            change_offset.unwrap_or(0)
        ),
        None,
        Duration::from_secs(30),
    )
    .await
}

fn read_artifact(path: &str) -> Result<ureq::Response, ApiError> {
    let connection = crate::runtime::backend_connection()?;
    crate::runtime::local_agent()
        .get(&format!("{}{path}", connection.base))
        .set("Authorization", &connection.authorization())
        .timeout(Duration::from_secs(120))
        .call()
        .map_err(|err| ApiError::new(format!("无法读取产物：{err}")))
}

#[tauri::command]
pub async fn agent_preview(
    workspace_id: String,
    run_id: String,
    artifact_id: String,
) -> Result<tauri::ipc::Response, ApiError> {
    let path = run_path(&workspace_id, &run_id)?;
    validate_id(&artifact_id)?;
    tauri::async_runtime::spawn_blocking(move || {
        let mut bytes = Vec::new();
        let response = read_artifact(&format!("{path}/artifacts/{artifact_id}?preview=1"))?;
        let mime = response
            .header("Content-Type")
            .unwrap_or("")
            .split(';')
            .next()
            .unwrap_or("")
            .trim();
        if !matches!(mime, "image/png" | "image/jpeg" | "text/plain") {
            return Err(ApiError::new("此格式请导出后查看"));
        }
        response
            .into_reader()
            .take(PREVIEW_LIMIT + 1)
            .read_to_end(&mut bytes)
            .map_err(|e| ApiError::new(e.to_string()))?;
        if bytes.len() as u64 > PREVIEW_LIMIT {
            return Err(ApiError::new("预览超过 8 MiB，可导出原件"));
        }
        Ok(tauri::ipc::Response::new(bytes))
    })
    .await
    .map_err(|e| ApiError::new(e.to_string()))?
}

fn safe_filename(name: &str) -> bool {
    let stem = name
        .split('.')
        .next()
        .unwrap_or("")
        .trim_end()
        .to_ascii_uppercase();
    let mut chars = stem.chars();
    let last = chars.next_back();
    let device = matches!(
        stem.as_str(),
        "CON" | "PRN" | "AUX" | "NUL" | "CONIN$" | "CONOUT$"
    ) || matches!(chars.as_str(), "COM" | "LPT")
        && last.is_some_and(|c| c.is_ascii_digit() || "¹²³".contains(c));
    !name.is_empty()
        && name.len() <= 255
        && !device
        && !name.ends_with(['.', ' '])
        && !name
            .chars()
            .any(|c| c.is_control() || "<>:\"/\\|?*".contains(c))
}

fn copy_verified(
    mut source: impl Read,
    mut dest: impl Write,
    size: u64,
    expected: &str,
) -> io::Result<()> {
    let mut hash = Sha256::new();
    let mut written = 0u64;
    let mut buffer = [0u8; 64 * 1024];
    loop {
        let n = source.read(&mut buffer)?;
        if n == 0 {
            break;
        }
        written += n as u64;
        if written > size {
            return Err(io::Error::other("产物大小已变化"));
        }
        hash.update(&buffer[..n]);
        dest.write_all(&buffer[..n])?;
    }
    if written != size || format!("{:x}", hash.finalize()) != expected {
        return Err(io::Error::other("产物校验失败"));
    }
    dest.flush()
}

#[tauri::command]
pub async fn export_agent_artifact(
    window: tauri::WebviewWindow,
    workspace_id: String,
    run_id: String,
    artifact_id: String,
    name: String,
    size: u64,
    sha256: String,
) -> Result<bool, ApiError> {
    let path = run_path(&workspace_id, &run_id)?;
    validate_id(&artifact_id)?;
    if !safe_filename(&name) || sha256.len() != 64 || !sha256.bytes().all(|b| b.is_ascii_hexdigit())
    {
        return Err(ApiError::new("产物文件名或哈希无效"));
    }
    let Some(dir) = select_folder(window, "选择产物导出文件夹").await? else {
        return Ok(false);
    };
    tauri::async_runtime::spawn_blocking(move || {
        let source = read_artifact(&format!("{path}/artifacts/{artifact_id}"))?.into_reader();
        let destination = Path::new(&dir).join(&name);
        let mut file =
            tempfile::NamedTempFile::new_in(&dir).map_err(|e| ApiError::new(e.to_string()))?;
        copy_verified(source, file.as_file_mut(), size, &sha256)
            .and_then(|_| file.as_file().sync_all())
            .map_err(|e| ApiError::new(e.to_string()))?;
        file.persist_noclobber(&destination)
            .map_err(|e| ApiError::new(format!("无法创建 {name}：{}", e.error)))?;
        Ok(true)
    })
    .await
    .map_err(|e| ApiError::new(e.to_string()))?
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn export_checks_content_and_filename() {
        for name in [
            "../report",
            "a:b",
            "NUL.pdf",
            "com1.pdf",
            "a\\b",
            "NUL .pdf",
        ] {
            assert!(!safe_filename(name));
        }
        assert!(safe_filename("季度报告 1.pdf"));
        assert!(safe_filename(".gitignore"));
        let data = b"report";
        let expected = format!("{:x}", Sha256::digest(data));
        assert!(copy_verified(&data[..], Vec::new(), data.len() as u64, &expected).is_ok());
        assert!(copy_verified(&data[..], Vec::new(), 2, &expected).is_err());
        assert!(copy_verified(&data[..], Vec::new(), data.len() as u64, &"0".repeat(64)).is_err());
    }
}
