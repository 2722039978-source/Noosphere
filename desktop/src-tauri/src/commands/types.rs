use serde::{Deserialize, Serialize};
use std::collections::HashMap;

/// 统一 IPC 错误: HTTP 失败 / Go `{"error":...}` / 反序列化失败 全部归一
#[derive(Debug, Clone, Serialize)]
pub struct ApiError {
    pub message: String,
}

impl ApiError {
    pub fn new(msg: impl Into<String>) -> Self {
        Self { message: msg.into() }
    }
}

impl std::fmt::Display for ApiError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}", self.message)
    }
}

/// 原始响应体 → T: 先探测 Go 错误载荷, 再反序列化目标类型
pub(crate) fn parse_body<T: serde::de::DeserializeOwned>(body: &str) -> Result<T, ApiError> {
    if let Ok(v) = serde_json::from_str::<serde_json::Value>(body) {
        if let Some(err) = v.get("error").and_then(|e| e.as_str()) {
            return Err(ApiError::new(err));
        }
    }
    serde_json::from_str(body).map_err(|e| ApiError::new(format!("parse response: {e}")))
}

// ============ services ============

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub struct ModuleState {
    pub name: String,
    pub state: Option<String>,
    pub error: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct HealthResponse {
    pub status: String,
    pub version: String,
    pub modules: HashMap<String, ModuleState>,
    pub time: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub instance_id: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct BackendStatus {
    pub running: bool,
    pub restarts: u32,
    pub error: Option<String>,
    pub data_dir: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StartBackendResponse {
    pub status: String,
}

// ============ gateway ============

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ModelConfig {
    pub id: String,
    pub name: String,
    pub provider: String,
    pub base_url: String,
    pub model_name: String,
    pub enabled: Option<bool>,
    pub is_default: Option<bool>,
    #[serde(default)]
    pub api_key: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ModelsResponse {
    #[serde(default)]
    pub models: Vec<ModelConfig>,
    pub count: Option<u64>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ChatMessageParam {
    pub role: String,
    pub content: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ChatResponse {
    #[serde(default)]
    pub id: Option<String>,
    #[serde(default)]
    pub model: Option<String>,
    #[serde(default)]
    pub provider: Option<String>,
    #[serde(default)]
    pub content: String,
    #[serde(default)]
    pub tokens_used: Option<u64>,
    #[serde(default)]
    pub latency_ms: Option<f64>,
    #[serde(default)]
    pub timestamp: Option<String>,
}

/// 流式 Channel 消息 (替代旧的 JSON 字符串透传)
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StreamChunk {
    pub request_id: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub token: Option<String>,
    /// Final authoritative text; replace accumulated text rather than append.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub content: Option<String>,
    /// 累计 token 数 (进度指示)
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub count: Option<u64>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
    /// 流结束标记
    #[serde(default, skip_serializing_if = "std::ops::Not::not")]
    pub done: bool,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub status: Option<StreamStatus>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum StreamStatus {
    Completed,
    Cancelled,
    Failed,
}

// ============ nebula ============

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SessionInfo {
    pub id: String,
    pub name: String,
    pub created_at: String,
    pub memory_count: Option<u64>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SessionsResponse {
    #[serde(default)]
    pub sessions: Vec<SessionInfo>,
    pub count: Option<u64>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CreateSessionResponse {
    pub session_id: String,
    #[serde(default)]
    pub name: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MemoryRecord {
    pub id: String,
    pub session_id: String,
    pub content: String,
    #[serde(default, rename = "type")]
    pub memory_type: Option<String>,
    #[serde(default)]
    pub tags: Vec<String>,
    #[serde(default)]
    pub metadata: HashMap<String, String>,
    #[serde(default)]
    pub importance: Option<f64>,
    #[serde(default)]
    pub created_at: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SearchHit {
    pub key: String,
    pub score: f64,
    #[serde(default)]
    pub record: Option<MemoryRecord>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SearchResponse {
    #[serde(default)]
    pub results: Vec<SearchHit>,
    #[serde(default)]
    pub count: Option<u64>,
    #[serde(default)]
    pub query: Option<String>,
}

// ============ devops ============

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CpuMetrics {
    pub usage_percent: f64,
    pub cores: u32,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MemoryMetrics {
    pub total_gb: f64,
    pub used_gb: f64,
    pub usage_percent: f64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DiskMetrics {
    pub total_gb: f64,
    pub free_gb: f64,
    pub usage_percent: f64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SystemMetrics {
    pub cpu: CpuMetrics,
    pub memory: MemoryMetrics,
    pub disk: DiskMetrics,
    pub process_count: u32,
    #[serde(default)]
    pub goroutines: Option<u32>,
    pub timestamp: String,
}
