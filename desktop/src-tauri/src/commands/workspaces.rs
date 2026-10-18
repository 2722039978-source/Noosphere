use crate::commands::types::ApiError;
use serde_json::Value;
use std::io::Read;
use std::time::Duration;

pub(crate) fn validate_id(id: &str) -> Result<(), ApiError> {
    if id.is_empty() || id.len()>128 || !id.bytes().all(|c|c.is_ascii_alphanumeric() || c==b'_' || c==b'-') {
        return Err(ApiError::new("无效的工作区或任务标识"));
    }
    Ok(())
}

fn decode_response(status: u16, body: &str) -> Result<Value, ApiError> {
    if !(200..300).contains(&status) {
        let parsed=serde_json::from_str::<Value>(body).ok();
        let message=parsed.as_ref().and_then(|v|v.get("error")).and_then(|v|v.as_str().or_else(||v.get("message").and_then(Value::as_str))).unwrap_or(body);
        return Err(ApiError::new(message.chars().take(4096).collect::<String>()));
    }
    serde_json::from_str(body).map_err(|error| ApiError::new(error.to_string()))
}

async fn request(path:String, body:Option<Value>)->Result<Value,ApiError>{
    tauri::async_runtime::spawn_blocking(move||{
        crate::runtime::ensure_backend_ready()?;
        let url=format!("http://127.0.0.1:8080{path}");
        let auth=crate::local_auth::authorization().map_err(ApiError::new)?;
        let response=match body {
            Some(value)=>ureq::post(&url).set("Authorization",&auth).set("Content-Type","application/json").timeout(Duration::from_secs(15)).send_string(&value.to_string()),
            None=>ureq::get(&url).set("Authorization",&auth).timeout(Duration::from_secs(15)).call(),
        };
        let response=match response {Ok(response)=>response,Err(ureq::Error::Status(_,response))=>response,Err(error)=>return Err(ApiError::new(format!("工作区服务连接失败：{error}")))};
        let status=response.status();let mut text=String::new();
        response.into_reader().take(20*1024*1024+1).read_to_string(&mut text).map_err(|error|ApiError::new(error.to_string()))?;
        if text.len()>20*1024*1024 {return Err(ApiError::new("工作区响应超出读取上限"));}
        decode_response(status,&text)
    }).await.map_err(|error|ApiError::new(error.to_string()))?
}

#[tauri::command]
pub async fn list_workspaces()->Result<Value,ApiError>{request("/api/workspaces".into(),None).await}
#[tauri::command]
pub async fn list_agent_actions(workspace_id:String,run_id:String)->Result<Value,ApiError>{validate_id(&workspace_id)?;validate_id(&run_id)?;request(format!("/api/workspaces/{workspace_id}/runs/{run_id}/actions"),None).await}
#[tauri::command]
pub async fn decide_agent_action(workspace_id:String,id:String,decision:String)->Result<Value,ApiError>{validate_id(&workspace_id)?;validate_id(&id)?;if !["approve","reject","cancel","rollback"].contains(&decision.as_str()){return Err(ApiError::new("无效的操作决定"));}request(format!("/api/workspaces/{workspace_id}/actions/{id}"),Some(serde_json::json!({"decision":decision}))).await}
#[tauri::command]
pub async fn list_knowledge(workspace_id:String)->Result<Value,ApiError>{validate_id(&workspace_id)?;request(format!("/api/workspaces/{workspace_id}/knowledge"),None).await}
#[tauri::command]
pub async fn save_knowledge(workspace_id:String,memory:Value)->Result<Value,ApiError>{validate_id(&workspace_id)?;request(format!("/api/workspaces/{workspace_id}/knowledge"),Some(memory)).await}
#[tauri::command]
pub async fn prepare_context(workspace_id:String,query:String)->Result<Value,ApiError>{validate_id(&workspace_id)?;request(format!("/api/workspaces/{workspace_id}/context"),Some(serde_json::json!({"query":query,"top_k":8}))).await}
#[tauri::command]
pub async fn save_workspace(workspace:Value)->Result<Value,ApiError>{
    if let Some(id)=workspace.get("id").and_then(Value::as_str) {if !id.is_empty(){validate_id(id)?;}}
    request("/api/workspaces".into(),Some(workspace)).await
}
#[tauri::command]
pub async fn list_workspace_runs(workspace_id:String)->Result<Value,ApiError>{validate_id(&workspace_id)?;request(format!("/api/workspaces/{workspace_id}/runs"),None).await}
#[tauri::command]
pub async fn submit_workspace_run(workspace_id:String,kind:String,title:String,input:Value)->Result<Value,ApiError>{
    validate_id(&workspace_id)?;request(format!("/api/workspaces/{workspace_id}/runs"),Some(serde_json::json!({"kind":kind,"title":title,"input":input}))).await
}
#[tauri::command]
pub async fn get_workspace_run(id:String)->Result<Value,ApiError>{validate_id(&id)?;request(format!("/api/runs/{id}"),None).await}
#[tauri::command]
pub async fn get_run_events(id:String,after:Option<u64>)->Result<Value,ApiError>{validate_id(&id)?;request(format!("/api/runs/{id}/events?after={}",after.unwrap_or(0)),None).await}
#[tauri::command]
pub async fn cancel_workspace_run(id:String)->Result<Value,ApiError>{validate_id(&id)?;request(format!("/api/runs/{id}/cancel"),Some(serde_json::json!({}))).await}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn opaque_ids_cannot_modify_the_request_path_or_query() {
        for id in ["", "..", "a/b", "a\\b", "a?x=1", "a#x", "a%2fb", " a", "a\n"] {
            assert!(validate_id(id).is_err(), "accepted {id:?}");
        }
        for id in ["local", "ws_abc-123", "550e8400-e29b-41d4-a716-446655440000"] {
            assert!(validate_id(id).is_ok());
        }
    }

    #[test]
    fn http_error_preserves_backend_message() {
        assert_eq!(decode_response(409, r#"{"error":"project root changed"}"#).unwrap_err().message, "project root changed");
        assert_eq!(decode_response(503, "backend busy").unwrap_err().message, "backend busy");
    }

    #[test]
    fn successful_run_error_is_data_and_malformed_json_is_not_success() {
        assert_eq!(decode_response(200, r#"{"id":"run-a","status":"failed","error":"worker failed"}"#).unwrap()["error"], "worker failed");
        assert_eq!(decode_response(200, r#"{"id":"run-a","error":""}"#).unwrap()["error"], "");
        assert!(decode_response(200, "not json").is_err());
    }
}
