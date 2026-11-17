use crate::commands::types::{parse_body, ApiError};
use serde_json::{json, Value};
use std::time::Duration;
use tauri::ipc::{InvokeBody, Request};

const MAX_CHUNK_BYTES: usize = 8 << 20;
const META: &str = "x-noosphere-attachment";

fn attachment_post(path: &str, body: Vec<u8>, metadata: Option<String>) -> Result<Value, ApiError> {
    let connection = crate::runtime::backend_connection()?;
    let timeout = if matches!(path, "stage" | "prepare") { 100 } else { 24 };
    let request = crate::runtime::local_agent()
        .post(&format!("{}/api/chat-attachments/{path}", connection.base))
        .set("Authorization", &connection.authorization())
        .timeout(Duration::from_secs(timeout));
    let request = match metadata {
        Some(value) => request
            .set(META, &value)
            .set("Content-Type", "application/octet-stream"),
        None => request.set("Content-Type", "application/json"),
    };
    let response = match request.send_bytes(&body) {
        Ok(response) => response,
        Err(ureq::Error::Status(status, response)) => {
            let body = response.into_string().unwrap_or_default();
            let mut error = parse_body::<Value>(&body)
                .err()
                .unwrap_or_else(|| ApiError::new(format!("附件服务返回 HTTP {status}，请重试")));
            error.status = Some(status);
            return Err(error);
        }
        Err(_) => return Err(ApiError::new("无法连接本地附件服务或请求已超时，请重试")),
    };
    let body = response
        .into_string()
        .map_err(|_| ApiError::new("无法读取附件处理结果，请重试"))?;
    parse_body(&body)
}

async fn attachment_command(path: &'static str, body: Vec<u8>, metadata: Option<String>) -> Result<Value, ApiError> {
    tauri::async_runtime::spawn_blocking(move || attachment_post(path, body, metadata))
        .await
        .map_err(|_| ApiError::new("附件处理已中断，请重试"))?
}

#[tauri::command]
pub async fn stage_chat_attachment(request: Request<'_>) -> Result<Value, ApiError> {
    let invalid_size = || ApiError::new("附件分段为空或超过单次传输上限，请重新选择文件");
    let invalid_data = || ApiError::new("未收到附件数据，请重新选择文件");
    let bytes = match request.body() {
        InvokeBody::Raw(bytes) if !bytes.is_empty() && bytes.len() <= MAX_CHUNK_BYTES => bytes.clone(),
        InvokeBody::Raw(_) => return Err(invalid_size()),
        // Tauri's WebView postMessage fallback serializes ArrayBuffers as byte arrays.
        InvokeBody::Json(value) => {
            let data = value.as_array().ok_or_else(invalid_data)?;
            if data.is_empty() || data.len() > MAX_CHUNK_BYTES {
                return Err(invalid_size());
            }
            data.iter().map(|byte| byte.as_u64().and_then(|value| u8::try_from(value).ok()).ok_or_else(invalid_data))
                .collect::<Result<Vec<_>, _>>()?
        }
    };
    let metadata = request
        .headers()
        .get(META)
        .and_then(|value| value.to_str().ok())
        .filter(|value| value.len() <= 65536)
        .ok_or_else(|| ApiError::new("附件所属会话无效，请重新选择文件"))?;
    attachment_command("stage", bytes, Some(metadata.to_owned())).await
}

#[tauri::command]
pub async fn discard_chat_attachment(workspace_id: String, session_id: String, id: String) -> Result<Value, ApiError> {
    let body = json!({"workspace_id": workspace_id, "session_id": session_id, "id": id});
    attachment_command("discard", body.to_string().into_bytes(), None).await
}

#[tauri::command]
pub async fn prepare_chat_attachments(workspace_id: String, session_id: String, turn: Value) -> Result<Value, ApiError> {
    let mut body = turn.as_object().ok_or_else(|| ApiError::new("附件引用格式无效"))?.clone();
    body.insert("workspace_id".into(), json!(workspace_id));
    body.insert("session_id".into(), json!(session_id));
    attachment_command("prepare", Value::Object(body).to_string().into_bytes(), None).await
}

#[tauri::command]
pub async fn delete_session_attachments(workspace_id: String, session_id: String) -> Result<Value, ApiError> {
    let body = json!({"workspace_id": workspace_id, "session_id": session_id});
    attachment_command("delete-session", body.to_string().into_bytes(), None).await
}
