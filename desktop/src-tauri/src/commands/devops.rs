use crate::commands::services::{api_get, api_post};
use crate::commands::types::{ApiError, SystemMetrics};
use serde_json::{json, Value};

#[tauri::command]
pub fn get_status() -> Result<Value, ApiError> {
    api_get("/api/v1/devops/status")
}

#[tauri::command]
pub fn get_metrics() -> Result<SystemMetrics, ApiError> {
    api_get("/api/v1/devops/metrics")
}

#[tauri::command]
pub fn diagnose(issue: String) -> Result<Value, ApiError> {
    api_post("/api/v1/devops/diagnose", &json!({ "issue": issue }).to_string())
}

#[tauri::command]
pub fn execute_tool(
    name: String,
    args: Option<Value>,
) -> Result<Value, ApiError> {
    api_post(
        "/api/v1/devops/tools/execute",
        &json!({ "name": name, "args": args.unwrap_or(json!({})) }).to_string(),
    )
}

#[tauri::command]
pub fn list_tools() -> Result<Value, ApiError> {
    api_get("/api/v1/devops/tools")
}

#[tauri::command]
pub async fn devops_loop(request_id: String, issue: String, streams: tauri::State<'_, crate::commands::stream::StreamTasks>) -> Result<Value, ApiError> {
    crate::runtime::ensure_backend_ready()?;
    let registration = streams.register(request_id)?;
    let token = crate::local_auth::token().map_err(ApiError::new)?;
    let client = reqwest::Client::builder().no_proxy().redirect(reqwest::redirect::Policy::none())
        .timeout(std::time::Duration::from_secs(130)).build().map_err(|e| ApiError::new(e.to_string()))?;
    tokio::select! {
        biased;
        _ = registration.cancel.cancelled() => Err(ApiError::new("诊断已取消")),
        result = async {
            let response = client.post("http://127.0.0.1:8080/api/v1/devops/loop").bearer_auth(token)
                .json(&json!({"issue":issue,"max_steps":5,"auto_approve":false})).send().await
                .map_err(|e| ApiError::new(e.to_string()))?;
            let status=response.status();
            let body=response.text().await.map_err(|e| ApiError::new(e.to_string()))?;
            if !status.is_success() { return Err(ApiError::new(format!("诊断请求失败: {status}"))); }
            crate::commands::types::parse_body::<Value>(&body)
        } => result,
    }
}
