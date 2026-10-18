use crate::commands::services::{api_delete, api_get, api_post, req_body};
use crate::commands::stream::{run_stream, StreamTasks};
use crate::commands::types::{
    ApiError, ChatMessageParam, ChatResponse, ModelConfig, ModelsResponse, StreamChunk,
};
use serde_json::{json, Value};
use tauri::ipc::Channel;

#[tauri::command]
pub fn get_models() -> Result<ModelsResponse, ApiError> {
    api_get("/api/gateway/models")
}

#[tauri::command]
pub fn add_model(model: ModelConfig) -> Result<Value, ApiError> {
    api_post("/api/gateway/models", &req_body(&model))
}

#[tauri::command]
pub fn delete_model(id: String) -> Result<Value, ApiError> {
    api_delete(&format!("/api/gateway/models/{id}"))
}

#[tauri::command]
pub async fn test_model(id: String) -> Result<Value, ApiError> {
    tauri::async_runtime::spawn_blocking(move || {
        if id.is_empty() || id.contains('/') || id.contains('?') || id.contains('#') { return Err(ApiError::new("无效的模型标识")); }
        api_post(&format!("/api/gateway/models/{id}/test"), "{}")
    }).await.map_err(|error| ApiError::new(error.to_string()))?
}

/// 非流式对话: 返回结构化 ChatResponse
#[tauri::command]
pub fn gateway_chat(
    messages: Vec<ChatMessageParam>,
    model_id: Option<String>,
    project: Option<String>,
    system_prompt: Option<String>,
) -> Result<ChatResponse, ApiError> {
    let body = json!({
        "messages": messages,
        "model_id": model_id.unwrap_or_default(),
        "project": project.unwrap_or_default(),
        "system_prompt": system_prompt.unwrap_or_default(),
    });
    api_post("/api/gateway/chat", &body.to_string())
}

#[tauri::command]
pub fn gateway_stats() -> Result<Value, ApiError> {
    api_get("/api/gateway/stats")
}

/// Resolve only once the request is registered, so a subsequent cancel cannot miss it.
#[tauri::command]
pub fn gateway_chat_stream(
    request_id: String,
    messages: Vec<ChatMessageParam>,
    model_id: Option<String>,
    workspace_id: Option<String>,
    session_id: Option<String>,
    on_event: Channel<StreamChunk>,
    streams: tauri::State<'_, StreamTasks>,
) -> Result<(), ApiError> {
    crate::runtime::ensure_backend_ready()?;
    let registration = streams.register(request_id.clone())?;
    let body = json!({
        "messages": messages,
        "model_id": model_id.unwrap_or_default(),
        "request_id":request_id,
        "workspace_id":workspace_id.unwrap_or_else(||"local".into()),
        "session_id":session_id.unwrap_or_default(),
    });
    tauri::async_runtime::spawn(run_stream(
        registration,
        "http://127.0.0.1:8080/api/gateway/chat/stream".to_owned(),
        body,
        Some(crate::local_auth::token().map_err(ApiError::new)?.to_owned()),
        move |chunk| on_event.send(chunk).is_ok(),
    ));
    Ok(())
}

/// Cancellation is idempotent, including requests which have already finished.
#[tauri::command]
pub fn gateway_chat_cancel(
    request_id: String,
    streams: tauri::State<'_, StreamTasks>,
) -> Result<(), ApiError> {
    streams.cancel(&request_id);
    Ok(())
}
