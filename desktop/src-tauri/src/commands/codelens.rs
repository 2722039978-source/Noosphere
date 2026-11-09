use crate::commands::services::api_post;
use crate::commands::types::ApiError;
use crate::commands::workspaces::{request_with_timeout, validate_id};
use serde_json::{json, Value};
use std::time::Duration;

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

#[tauri::command]
pub async fn codelens_trace(workspace_id: String, query: String) -> Result<Value, ApiError> {
    validate_id(&workspace_id)?;
    let body = json!({ "workspace_id": workspace_id, "query": query });
    request_with_timeout("/api/v1/codelens/trace".into(), Some(body), Duration::from_secs(15)).await
}

#[tauri::command]
pub async fn codelens_project_search(workspace_id: String, query: String) -> Result<Value, ApiError> {
    validate_id(&workspace_id)?;
    let body = json!({ "workspace_id": workspace_id, "query": query });
    request_with_timeout("/api/v1/codelens/project-search".into(), Some(body), Duration::from_secs(15)).await
}

#[tauri::command]
pub async fn codelens_source(
    workspace_id: String,
    file: String,
    start_line: usize,
    end_line: usize,
    source_hash: Option<String>,
) -> Result<Value, ApiError> {
    validate_id(&workspace_id)?;
    let body = json!({
        "workspace_id": workspace_id, "file": file, "start_line": start_line,
        "end_line": end_line, "source_hash": source_hash,
    });
    request_with_timeout("/api/v1/codelens/source".into(), Some(body), Duration::from_secs(15)).await
}

#[tauri::command]
pub async fn codelens_inspect_artifact(
    workspace_id: String,
    path: String,
    tool: String,
) -> Result<Value, ApiError> {
    validate_id(&workspace_id)?;
    let body = json!({ "workspace_id": workspace_id, "path": path, "tool": tool });
    request_with_timeout("/api/v1/codelens/artifacts".into(), Some(body), Duration::from_secs(55)).await
}

#[tauri::command]
pub async fn codelens_get_artifact(workspace_id: String, id: String) -> Result<Value, ApiError> {
    validate_id(&workspace_id)?;
    validate_id(&id)?;
    let path = format!("/api/v1/codelens/artifacts/{id}?workspace_id={workspace_id}");
    request_with_timeout(path, None, Duration::from_secs(15)).await
}
