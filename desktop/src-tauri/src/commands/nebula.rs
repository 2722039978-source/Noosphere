use crate::commands::services::{api_get, api_post};
use crate::commands::types::{
    ApiError, CreateSessionResponse, SearchResponse, SessionsResponse,
};
use serde_json::{json, Value};
use std::collections::HashMap;

#[tauri::command]
pub fn list_sessions() -> Result<SessionsResponse, ApiError> {
    api_get("/api/v1/sessions")
}

#[tauri::command]
pub fn create_session(name: String) -> Result<CreateSessionResponse, ApiError> {
    let body = json!({ "name": name });
    api_post("/api/v1/sessions", &body.to_string())
}

#[tauri::command]
pub fn store_memory(
    session_id: String,
    content: String,
    memory_type: Option<String>,
    tags: Option<Vec<String>>,
    metadata: Option<HashMap<String, String>>,
) -> Result<Value, ApiError> {
    let body = json!({
        "session_id": session_id,
        "content": content,
        "type": memory_type.unwrap_or_else(|| "episodic".into()),
        "tags": tags.unwrap_or_default(),
        "metadata": metadata.unwrap_or_default(),
    });
    api_post(
        &format!("/api/v1/sessions/{session_id}/memories"),
        &body.to_string(),
    )
}

#[tauri::command]
pub fn search_memories(
    session_id: String,
    query: String,
    top_k: Option<u32>,
) -> Result<SearchResponse, ApiError> {
    let body = json!({ "query": query, "top_k": top_k.unwrap_or(10) });
    api_post(
        &format!("/api/v1/sessions/{session_id}/search"),
        &body.to_string(),
    )
}

#[tauri::command]
pub fn nebula_stats() -> Result<Value, ApiError> {
    api_get("/api/v1/stats")
}
