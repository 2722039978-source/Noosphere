use crate::commands::types::ApiError;
use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::time::Duration;

#[derive(Deserialize, Serialize)]
pub struct ModelSelection {
    scope: String,
    workspace_id: String,
    session_id: String,
    model_id: String,
}

async fn selection_response(response: reqwest::Response) -> Result<Value, ApiError> {
    let status = response.status();
    let body: Value = response.json().await.map_err(|_| ApiError::new("模型绑定响应无效，请重试"))?;
    if !status.is_success() {
        return Err(ApiError::new(body.get("error").and_then(Value::as_str)
            .unwrap_or("模型绑定请求失败，请重试")));
    }
    Ok(body)
}

fn client() -> Result<reqwest::Client, ApiError> {
    crate::runtime::ensure_backend_ready()?;
    reqwest::Client::builder().timeout(Duration::from_secs(25)).build()
        .map_err(|_| ApiError::new("无法初始化模型绑定连接"))
}

#[tauri::command]
pub async fn select_model(selection: ModelSelection) -> Result<Value, ApiError> {
    let response = client()?.post("http://127.0.0.1:8080/api/gateway/selection")
        .bearer_auth(crate::local_auth::token().map_err(ApiError::new)?)
        .json(&selection).send().await
        .map_err(|_| ApiError::new("模型切换请求未确认，请重新读取会话模型后重试"))?;
    selection_response(response).await
}

#[tauri::command]
pub async fn get_model_selection(workspace_id: String, session_id: String) -> Result<Value, ApiError> {
    let mut url = reqwest::Url::parse("http://127.0.0.1:8080/api/gateway/selection")
        .map_err(|_| ApiError::new("无效的模型绑定地址"))?;
    url.query_pairs_mut().append_pair("workspace_id", &workspace_id).append_pair("session_id", &session_id);
    let response = client()?.get(url)
        .bearer_auth(crate::local_auth::token().map_err(ApiError::new)?)
        .send().await.map_err(|_| ApiError::new("无法读取会话模型，请检查后端连接"))?;
    selection_response(response).await
}
