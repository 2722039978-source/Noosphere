use crate::commands::services::{api_delete, api_get, api_post, api_post_response};
use crate::commands::stream::{run_stream, StreamTasks};
use crate::commands::types::{
    ApiError, ChatMessageParam, ChatResponse, ModelConfig, ModelsResponse, StreamChunk,
};
use serde::{Deserialize, Serialize};
use serde_json::{json, Map, Value};
use tauri::ipc::Channel;

#[tauri::command]
pub fn get_models() -> Result<ModelsResponse, ApiError> {
    api_get("/api/gateway/models")
}

#[tauri::command]
pub fn add_model(model: ModelConfig) -> Result<Value, ApiError> {
    let mut body = serde_json::to_value(model).map_err(|_| ApiError::new("无法保存模型配置"))?;
    body["strict_credential_origin"] = Value::Bool(true);
    api_post("/api/gateway/models", &body.to_string())
}

#[tauri::command]
pub fn delete_model(id: String) -> Result<Value, ApiError> {
    api_delete(&format!("/api/gateway/models/{id}"))
}

#[tauri::command]
pub async fn test_model(id: String) -> Result<ModelDiagnostic, ApiError> {
    tauri::async_runtime::spawn_blocking(move || {
        if id.is_empty() || id.contains('/') || id.contains('?') || id.contains('#') {
            return Err(ApiError::new("无效的模型标识"));
        }
        model_diagnostic(&api_post_response(&format!("/api/gateway/models/{id}/test"), "{}")?)
    })
    .await
    .map_err(|error| ApiError::new(error.to_string()))?
}

#[tauri::command]
pub async fn probe_model(model: ModelConfig, infer: Option<bool>) -> Result<ModelDiagnostic, ApiError> {
    tauri::async_runtime::spawn_blocking(move || {
        let body = json!({"model": model, "infer": infer.unwrap_or(true)});
        model_diagnostic(&api_post_response("/api/gateway/models/probe", &body.to_string())?)
    })
    .await
    .map_err(|_| ApiError::new("无法执行模型连接验证，请重试"))?
}

#[derive(Debug, Deserialize, Serialize)]
pub struct ModelDiagnostic {
    status: String,
    provider: String,
    model: String,
    endpoint: String,
    stage: String,
    code: String,
    service: String,
    model_discovery: String,
    model_loaded: String,
    inference: String,
    #[serde(flatten)]
    details: Map<String, Value>,
}

// An HTTP 200 diagnostic can describe an upstream failure. The explanatory
// `error` is part of the report, alongside the code, stage and request address.
fn model_diagnostic(body: &str) -> Result<ModelDiagnostic, ApiError> {
    serde_json::from_str(body).map_err(|_| ApiError::new("模型诊断响应格式不完整，请重试"))
}

#[cfg(test)]
mod diagnostic_tests {
    use super::*;

    #[test]
    fn domain_error_in_successful_diagnostic_preserves_the_envelope() {
        let report = json!({
            "status": "error", "provider": "openai_compatible", "model": "fixture",
            "endpoint": "http://127.0.0.1:18788/health", "stage": "service", "code": "SERVICE_UNREACHABLE",
            "service": "offline", "model_discovery": "unknown", "model_loaded": "unknown", "inference": "skipped",
            "error": "服务不可达。请检查端口。", "checks": [{"stage": "service", "status": "error"}]
        });
        let diagnostic = model_diagnostic(&report.to_string()).unwrap();
        assert_eq!(serde_json::to_value(diagnostic).unwrap(), report);
        assert!(model_diagnostic(r#"{"error":"missing report"}"#).is_err());
    }
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
    attachment_turn: Option<Value>,
    preview: Option<bool>,
    on_event: Channel<StreamChunk>,
    streams: tauri::State<'_, StreamTasks>,
) -> Result<(), ApiError> {
    let connection = crate::runtime::backend_connection()?;
    let registration = streams.register(request_id.clone())?;
    let body = json!({
        "messages": messages,
        "model_id": model_id.unwrap_or_default(),
        "request_id":request_id,
        "workspace_id":workspace_id.unwrap_or_else(||"local".into()),
        "session_id":session_id.unwrap_or_default(),
        "attachment_turn":attachment_turn,
        "preview":preview.unwrap_or(false),
    });
    tauri::async_runtime::spawn(run_stream(
        registration,
        format!("{}/api/gateway/chat/stream", connection.base),
        body,
        Some(connection.token),
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
