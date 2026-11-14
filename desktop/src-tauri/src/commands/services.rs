use crate::commands::types::{
    parse_body, ApiError, BackendStatus, HealthResponse, StartBackendResponse,
};
use serde::de::DeserializeOwned;
use std::time::Duration;
use tauri::State;

pub use crate::runtime::BackendService as ServiceState;

pub(crate) fn api_get<T: DeserializeOwned>(path: &str) -> Result<T, ApiError> {
    let connection = crate::runtime::backend_connection()?;
    let resp = crate::runtime::local_agent().get(&format!("{}{path}", connection.base))
        .set("Authorization", &connection.authorization())
        .timeout(Duration::from_secs(5))
        .call()
        .map_err(|e| ApiError::new(format!("request failed: {e}")))?;
    let body = resp
        .into_string()
        .map_err(|e| ApiError::new(format!("read body: {e}")))?;
    parse_body(&body)
}

pub(crate) fn api_post<T: DeserializeOwned>(path: &str, body: &str) -> Result<T, ApiError> {
    let connection = crate::runtime::backend_connection()?;
    let resp = crate::runtime::local_agent().post(&format!("{}{path}", connection.base))
        .set("Authorization", &connection.authorization())
        .set("Content-Type", "application/json")
        .timeout(Duration::from_secs(180))
        .send_string(body)
        .map_err(|e| ApiError::new(format!("request failed: {e}")))?;
    let body = resp
        .into_string()
        .map_err(|e| ApiError::new(format!("read body: {e}")))?;
    parse_body(&body)
}

pub(crate) fn api_delete<T: DeserializeOwned>(path: &str) -> Result<T, ApiError> {
    let connection = crate::runtime::backend_connection()?;
    let resp = crate::runtime::local_agent().delete(&format!("{}{path}", connection.base))
        .set("Authorization", &connection.authorization())
        .timeout(Duration::from_secs(5))
        .call()
        .map_err(|e| ApiError::new(format!("request failed: {e}")))?;
    let body = resp
        .into_string()
        .map_err(|e| ApiError::new(format!("read body: {e}")))?;
    parse_body(&body)
}

fn encode<T: serde::Serialize>(body: &T) -> Result<String, ApiError> {
    serde_json::to_string(body).map_err(|e| ApiError::new(format!("encode request: {e}")))
}

#[tauri::command]
pub fn start_backend(
    app: tauri::AppHandle,
    state: State<ServiceState>,
) -> Result<StartBackendResponse, ApiError> {
    // A missing resource may have been restored since the initial launch.
    state.initialize(crate::runtime::RuntimePaths::from_app(&app));
    let status = state.request_start().map_err(ApiError::new)?;
    Ok(StartBackendResponse { status })
}

#[tauri::command]
pub fn check_health() -> Result<HealthResponse, ApiError> {
    api_get("/health")
}

#[tauri::command]
pub fn get_backend_status(state: State<ServiceState>) -> Result<BackendStatus, ApiError> {
    Ok(state.status())
}

pub(crate) fn req_body<T: serde::Serialize>(body: &T) -> String {
    encode(body).unwrap_or_default()
}
