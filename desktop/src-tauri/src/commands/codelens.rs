use crate::commands::services::api_post;
use crate::commands::types::ApiError;
use serde_json::{json, Value};

#[tauri::command]
pub fn codelens_index(project_path: String, exclude: Option<Vec<String>>) -> Result<Value, ApiError> {
    api_post(
        "/api/v1/codelens/index",
        &json!({
            "project_path": project_path,
            "exclude": exclude.unwrap_or_else(|| vec![".git".into(), "node_modules".into(), "target".into()]),
        })
        .to_string(),
    )
}

#[tauri::command]
pub fn codelens_search(project_path: String, query: String) -> Result<Value, ApiError> {
    api_post(
        "/api/v1/codelens/search",
        &json!({ "project_path": project_path, "query": query }).to_string(),
    )
}

#[tauri::command]
pub fn codelens_analyze(project_path: String, file: String) -> Result<Value, ApiError> {
    api_post(
        "/api/v1/codelens/analyze/file",
        &json!({ "project_path": project_path, "file": file }).to_string(),
    )
}

#[tauri::command]
pub fn codelens_health() -> Result<Value, ApiError> {
    crate::commands::services::api_get("/api/v1/codelens/health")
}
