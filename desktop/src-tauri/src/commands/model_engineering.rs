//! Fixed, authenticated desktop transport for model engineering. The webview
//! supplies a tagged operation, never a URL, method, credential, or host path.
use crate::commands::types::ApiError;
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use std::{io::Read, time::Duration};

#[derive(Debug, Clone, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct RequestedOptions {
    temperature: f64,
    seed: i64,
    num_ctx: i64,
    max_output: i64,
    #[serde(skip_serializing_if = "Option::is_none")]
    top_p: Option<f64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    top_k: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    stop: Option<Vec<String>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    think: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    keep_alive: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    parallelism: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    deadline_ns: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    retry_limit: Option<i64>,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct DatasetDocument {
    schema_version: u32,
    benchmark_id: String,
    version: String,
    title: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    description: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    source: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    license: Option<String>,
    scoring_rule: String,
    cases: Vec<DatasetCase>,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct DatasetCase {
    id: String,
    category: String,
    task: String,
    function: String,
    input_type: String,
    output_type: String,
    faulty_source: String,
    assertions: Vec<DatasetAssertion>,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct DatasetAssertion { input: Value, expected: Value }

#[derive(Debug, Clone, Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct LiveCanaryPolicy {
    window_seconds: u32, minimum_samples_per_arm: u32, minimum_feedback_per_arm: u32,
    maximum_error_rate: f64, maximum_bad_feedback_rate: f64, maximum_latency_ratio: f64,
    auto_pause_consecutive_errors: u32,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(tag = "operation", rename_all = "snake_case", deny_unknown_fields)]
pub enum ModelEngineeringRequest {
    ListLiveCanaries { workspace_id: String, cursor: Option<String>, limit: Option<u32> },
    CreateLiveCanary { workspace_id: String, title: String, stable_revision_id: String, candidate_revision_id: String, profile_id: String, target_bps: u32, policy: LiveCanaryPolicy, idempotency_key: String },
    GetLiveCanary { workspace_id: String, id: String },
    LiveCanaryCommand { workspace_id: String, id: String, command: String, expected_revision: i64, idempotency_key: String },
    LiveCanaryRequest { workspace_id: String, id: String, prompt: String, idempotency_key: String },
    ListLiveCanaryRequests { workspace_id: String, id: String, cursor: Option<String>, limit: Option<u32> },
    GetLiveCanaryRequest { workspace_id: String, id: String, request_id: String },
    LiveCanaryFeedback { workspace_id: String, id: String, request_id: String, rating: String, note: String, idempotency_key: String },
    LiveCanaryEvents { workspace_id: String, id: String, after: Option<u64>, limit: Option<u32> },
    Discover { gateway_model_id: String },
    RegisterModel { gateway_model_id: String, discovered_identity_hash: String },
    ListModels { cursor: Option<String>, limit: Option<u32> },
    ListDatasets { workspace_id: Option<String>, include_archived: Option<bool> },
    CreateDataset { workspace_id: String, document: Option<DatasetDocument>, document_json: Option<String> },
    GetDataset { workspace_id: String, id: String },
    ArchiveDataset { workspace_id: String, id: String, archived: bool },
    CreateProfile { requested: RequestedOptions },
    ListProfiles { cursor: Option<String>, limit: Option<u32> },
    ListPolicies,
    Environment,
    PrepareEvaluation { workspace_id: String, stable_revision_id: String, candidate_revision_id: String, profile_id: String, dataset_id: String, workflow_id: String, evaluation_mode: Option<String> },
    CreateEvaluation { workspace_id: String, stable_revision_id: String, candidate_revision_id: String, profile_id: String, dataset_id: Option<String>, workflow_id: Option<String>, evaluation_mode: Option<String>, idempotency_key: String },
    ListEvaluations { workspace_id: String, cursor: Option<String>, limit: Option<u32> },
    GetEvaluation { workspace_id: String, id: String, cursor: Option<String>, limit: Option<u32> },
    EvaluationCommand { workspace_id: String, id: String, command: String, expected_revision: i64, case_id: Option<String>, arm: Option<String>, idempotency_key: String },
    GetReport { workspace_id: String, id: String },
    GetCase { workspace_id: String, evaluation_id: String, case_id: String, cursor: Option<String>, limit: Option<u32>, report_id: Option<String> },
    CreateRelease { workspace_id: String, mode: String, workflow_id: String, report_id: String, policy_id: Option<String>, task_type_filter: Option<String>, idempotency_key: String },
    ListReleases { workspace_id: String, cursor: Option<String>, limit: Option<u32> },
    GetRelease { workspace_id: String, id: String },
    PreviewRelease { workspace_id: String, id: String, command: String },
    ReleaseCommand { workspace_id: String, id: String, command: String, expected_revision: i64, idempotency_key: String },
    SimulateRelease { workspace_id: String, id: String, scenario: String, expected_revision: i64, seed: Option<i64>, virtual_seconds: Option<i64>, idempotency_key: String },
    ReleaseEvents { workspace_id: String, id: String, after: Option<u64>, limit: Option<u32> },
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum Method { Get, Post }

#[derive(Debug)]
struct RequestSpec { method: Method, path: String, body: Option<Value>, key: Option<String> }

fn valid_id(value: &str) -> Result<(), ApiError> {
    let bytes = value.as_bytes();
    if bytes.is_empty() || bytes.len() > 128 || !bytes[0].is_ascii_alphanumeric()
        || !bytes.iter().skip(1).all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'_' | b'.' | b':' | b'-')) {
        return Err(ApiError::new("invalid model engineering identifier"));
    }
    Ok(())
}

fn checked_key(key: String) -> Result<String, ApiError> { valid_id(&key)?; Ok(key) }

fn page(mut path: String, cursor: Option<String>, limit: Option<u32>) -> Result<String, ApiError> {
    let mut query = Vec::new();
    if let Some(cursor) = cursor { valid_id(&cursor)?; query.push(format!("cursor={cursor}")); }
    if let Some(limit) = limit {
        if !(1..=100).contains(&limit) { return Err(ApiError::new("invalid page limit")); }
        query.push(format!("limit={limit}"));
    }
    if !query.is_empty() { path.push('?'); path.push_str(&query.join("&")); }
    Ok(path)
}

fn pick(value: &str, allowed: &[&str]) -> Result<(), ApiError> {
    if allowed.contains(&value) { Ok(()) } else { Err(ApiError::new("unsupported model engineering operation value")) }
}

fn get(path: String) -> RequestSpec { RequestSpec { method: Method::Get, path, body: None, key: None } }
fn post(path: String, body: Value, key: Option<String>) -> RequestSpec { RequestSpec { method: Method::Post, path, body: Some(body), key } }

fn build_request(request: ModelEngineeringRequest) -> Result<RequestSpec, ApiError> {
    use ModelEngineeringRequest::*;
    let spec = match request {
        ListLiveCanaries {workspace_id,cursor,limit} => { valid_id(&workspace_id)?;get(page(format!("/api/workspaces/{workspace_id}/live-canaries"),cursor,limit)?) },
        CreateLiveCanary {workspace_id,title,stable_revision_id,candidate_revision_id,profile_id,target_bps,policy,idempotency_key} => {
            for id in [&workspace_id,&stable_revision_id,&candidate_revision_id,&profile_id]{valid_id(id)?;}
            if title.trim().is_empty()||title.len()>200||stable_revision_id==candidate_revision_id||![500,2500,5000].contains(&target_bps)
                ||!(60..=86400).contains(&policy.window_seconds)||!(2..=500).contains(&policy.minimum_samples_per_arm)
                ||policy.minimum_feedback_per_arm<1||policy.minimum_feedback_per_arm>policy.minimum_samples_per_arm
                ||!(0.0..=0.5).contains(&policy.maximum_error_rate)||!(0.0..=0.5).contains(&policy.maximum_bad_feedback_rate)
                ||!(1.0..=10.0).contains(&policy.maximum_latency_ratio)||!(1..=10).contains(&policy.auto_pause_consecutive_errors){return Err(ApiError::new("invalid live canary policy or identities"));}
            post(format!("/api/workspaces/{workspace_id}/live-canaries"),json!({"title":title,"stable_revision_id":stable_revision_id,"candidate_revision_id":candidate_revision_id,"profile_id":profile_id,"target_bps":target_bps,"policy":policy}),Some(checked_key(idempotency_key)?))
        },
        GetLiveCanary {workspace_id,id} => {valid_id(&workspace_id)?;valid_id(&id)?;get(format!("/api/workspaces/{workspace_id}/live-canaries/{id}"))},
        LiveCanaryCommand {workspace_id,id,command,expected_revision,idempotency_key} => {
            valid_id(&workspace_id)?;valid_id(&id)?;pick(&command,&["start","pause","resume","advance","rollback"])?;if expected_revision<1{return Err(ApiError::new("invalid live revision"));}
            post(format!("/api/workspaces/{workspace_id}/live-canaries/{id}/commands"),json!({"command":command,"expected_revision":expected_revision}),Some(checked_key(idempotency_key)?))
        },
        LiveCanaryRequest {workspace_id,id,prompt,idempotency_key} => {
            valid_id(&workspace_id)?;valid_id(&id)?;if prompt.trim().is_empty()||prompt.len()>16384{return Err(ApiError::new("live prompt must be 1–16384 UTF-8 bytes"));}
            post(format!("/api/workspaces/{workspace_id}/live-canaries/{id}/requests"),json!({"prompt":prompt}),Some(checked_key(idempotency_key)?))
        },
        ListLiveCanaryRequests {workspace_id,id,cursor,limit} => {valid_id(&workspace_id)?;valid_id(&id)?;if limit.is_some_and(|v|v>20){return Err(ApiError::new("live evidence page limit is 20"));}get(page(format!("/api/workspaces/{workspace_id}/live-canaries/{id}/requests"),cursor,limit)?)},
        GetLiveCanaryRequest {workspace_id,id,request_id} => {for value in [&workspace_id,&id,&request_id]{valid_id(value)?;}get(format!("/api/workspaces/{workspace_id}/live-canaries/{id}/requests/{request_id}"))},
        LiveCanaryFeedback {workspace_id,id,request_id,rating,note,idempotency_key} => {
            for value in [&workspace_id,&id,&request_id]{valid_id(value)?;}pick(&rating,&["good","bad"])?;if note.len()>2000{return Err(ApiError::new("feedback note exceeds 2000 bytes"));}
            post(format!("/api/workspaces/{workspace_id}/live-canaries/{id}/feedback"),json!({"request_id":request_id,"rating":rating,"note":note}),Some(checked_key(idempotency_key)?))
        },
        LiveCanaryEvents {workspace_id,id,after,limit} => {
            valid_id(&workspace_id)?;valid_id(&id)?;if limit.is_some_and(|v|!(1..=100).contains(&v)){return Err(ApiError::new("invalid live events page limit"));}
            get(format!("/api/workspaces/{workspace_id}/live-canaries/{id}/events?after={}&limit={}",after.unwrap_or(0),limit.unwrap_or(20)))
        },
        PrepareEvaluation {workspace_id,stable_revision_id,candidate_revision_id,profile_id,dataset_id,workflow_id,evaluation_mode} => {
            for id in [&workspace_id,&stable_revision_id,&candidate_revision_id,&profile_id,&dataset_id,&workflow_id] {valid_id(id)?;}
            if let Some(ref mode)=evaluation_mode {pick(mode,&["baseline","compare"])?;}
            post(format!("/api/workspaces/{workspace_id}/evaluations/preparation"),json!({"stable_revision_id":stable_revision_id,"candidate_revision_id":candidate_revision_id,"profile_id":profile_id,"dataset_id":dataset_id,"workflow_id":workflow_id,"evaluation_mode":evaluation_mode.unwrap_or_default()}),None)
        },
        Discover { gateway_model_id } => {valid_id(&gateway_model_id)?;post("/api/model-lab/discovery".into(), json!({"gateway_model_id":gateway_model_id}), None)},
        RegisterModel { gateway_model_id, discovered_identity_hash } => {
            valid_id(&gateway_model_id)?;
            if discovered_identity_hash.len()!=64 || !discovered_identity_hash.bytes().all(|b|b.is_ascii_hexdigit()){return Err(ApiError::new("invalid discovered identity hash"));}
            post("/api/model-lab/models".into(), json!({"gateway_model_id":gateway_model_id,"discovered_identity_hash":discovered_identity_hash}), None)
        }
        ListModels { cursor, limit } => get(page("/api/model-lab/models".into(), cursor, limit)?),
        ListDatasets { workspace_id, include_archived } => {
            match workspace_id {
                Some(ws) => { valid_id(&ws)?; get(format!("/api/workspaces/{ws}/datasets{}", if include_archived == Some(true) {"?include_archived=true"} else {""})) },
                None if include_archived.unwrap_or(false) => return Err(ApiError::new("workspace required for archived datasets")),
                None => get("/api/model-lab/datasets".into()),
            }
        }
        CreateDataset { workspace_id, document, document_json } => {
            valid_id(&workspace_id)?;
            let doc = match (document, document_json) {
                (Some(doc), None) => doc,
                (None, Some(raw)) => {
                    if raw.len() > 64*1024 { return Err(ApiError::new("dataset document exceeds 64 KiB")); }
                    serde_json::from_str::<DatasetDocument>(&raw).map_err(|error| ApiError::new(format!("invalid dataset document: {error}")))?
                },
                _ => return Err(ApiError::new("supply exactly one dataset document representation")),
            };
            if doc.schema_version != 1 || doc.scoring_rule != "exact" || !(1..=50).contains(&doc.cases.len()) || doc.cases.iter().any(|case| !(1..=100).contains(&case.assertions.len())) {
                return Err(ApiError::new("unsupported dataset schema, scoring rule, or case/assertion count"));
            }
            post(format!("/api/workspaces/{workspace_id}/datasets"), json!(doc), None)
        }
        GetDataset { workspace_id, id } => {
            valid_id(&workspace_id)?; valid_id(&id)?;
            get(format!("/api/workspaces/{workspace_id}/datasets/{id}"))
        }
        ArchiveDataset { workspace_id, id, archived } => {
            valid_id(&workspace_id)?; valid_id(&id)?;
            post(format!("/api/workspaces/{workspace_id}/datasets/{id}/archive"), json!({"archived":archived}), None)
        }
        CreateProfile { requested } => post("/api/model-lab/profiles".into(), json!({"requested":requested}), None),
        ListProfiles { cursor, limit } => get(page("/api/model-lab/profiles".into(), cursor, limit)?),
        ListPolicies => get("/api/model-ops/policies?mode=simulation".into()),
        Environment => get("/api/model-lab/environment".into()),
        CreateEvaluation { workspace_id, stable_revision_id, candidate_revision_id, profile_id, dataset_id, workflow_id, evaluation_mode, idempotency_key } => {
            for id in [&workspace_id,&stable_revision_id,&candidate_revision_id,&profile_id] { valid_id(id)?; }
            if let Some(id)=&dataset_id {valid_id(id)?;} if let Some(id)=&workflow_id {valid_id(id)?;}
            let mut body=json!({"stable_revision_id":stable_revision_id,"candidate_revision_id":candidate_revision_id,"profile_id":profile_id,"dataset_id":dataset_id,"workflow_id":workflow_id});
            if let Some(mode)=evaluation_mode { pick(&mode,&["baseline","compare"])?; body["evaluation_mode"]=json!(mode); }
            post(format!("/api/workspaces/{workspace_id}/evaluations"),body,Some(checked_key(idempotency_key)?))
        }
        ListEvaluations { workspace_id, cursor, limit } => {valid_id(&workspace_id)?;get(page(format!("/api/workspaces/{workspace_id}/evaluations"),cursor,limit)?)}
        GetEvaluation { workspace_id, id, cursor, limit } => {valid_id(&workspace_id)?;valid_id(&id)?;get(page(format!("/api/workspaces/{workspace_id}/evaluations/{id}"),cursor,limit)?)}
        EvaluationCommand { workspace_id, id, command, expected_revision, case_id, arm, idempotency_key } => {
            valid_id(&workspace_id)?;valid_id(&id)?;pick(&command,&["cancel","resume","retry_case"])?;
            if expected_revision < 1 {return Err(ApiError::new("invalid expected revision"));}
            if command == "retry_case" {let case=case_id.as_ref().ok_or_else(||ApiError::new("case_id required"))?;valid_id(case)?;pick(arm.as_deref().unwrap_or(""), &["stable","candidate"])?;}
            else if case_id.is_some() || arm.is_some() {return Err(ApiError::new("case_id and arm only apply to retry_case"));}
            post(format!("/api/workspaces/{workspace_id}/evaluations/{id}/commands"),json!({"command":command,"expected_revision":expected_revision,"case_id":case_id,"arm":arm}),Some(checked_key(idempotency_key)?))
        }
        GetReport { workspace_id, id } => {valid_id(&workspace_id)?;valid_id(&id)?;get(format!("/api/workspaces/{workspace_id}/reports/{id}"))}
        GetCase { workspace_id, evaluation_id, case_id, cursor, limit, report_id } => {
            for id in [&workspace_id,&evaluation_id,&case_id] {valid_id(id)?;}
            if report_id.is_some() && cursor.is_some() {return Err(ApiError::new("frozen report selection is not paginated"));}
            let mut path=page(format!("/api/workspaces/{workspace_id}/evaluations/{evaluation_id}/cases/{case_id}"),cursor,limit)?;
            if let Some(id)=report_id {valid_id(&id)?;path.push_str(if path.contains('?') {"&report_id="} else {"?report_id="});path.push_str(&id);}
            get(path)
        }
        CreateRelease { workspace_id, mode, workflow_id, report_id, policy_id, task_type_filter, idempotency_key } => {
            for id in [&workspace_id,&workflow_id,&report_id] {valid_id(id)?;}
            pick(&mode,&["simulation"])?;
            if let Some(policy)=&policy_id {pick(policy,&["simulation-demo-v1"])?;}
            if let Some(task)=&task_type_filter {pick(task,&["codetask_go"])?;}
            post(format!("/api/workspaces/{workspace_id}/releases"),json!({"mode":mode,"workflow_id":workflow_id,"report_id":report_id,"policy_id":policy_id,"task_type_filter":task_type_filter}),Some(checked_key(idempotency_key)?))
        }
        ListReleases { workspace_id, cursor, limit } => {valid_id(&workspace_id)?;get(page(format!("/api/workspaces/{workspace_id}/releases"),cursor,limit)?)}
        GetRelease { workspace_id, id } => {valid_id(&workspace_id)?;valid_id(&id)?;get(format!("/api/workspaces/{workspace_id}/releases/{id}"))}
        PreviewRelease { workspace_id, id, command } => {valid_id(&workspace_id)?;valid_id(&id)?;pick(&command,&["start_offline","advance","pause","resume","reject","rollback"])?;post(format!("/api/workspaces/{workspace_id}/releases/{id}/preview"),json!({"command":command}),None)}
        ReleaseCommand { workspace_id, id, command, expected_revision, idempotency_key } => {valid_id(&workspace_id)?;valid_id(&id)?;pick(&command,&["start_offline","advance","pause","resume","reject","rollback"])?;if expected_revision<1{return Err(ApiError::new("invalid expected revision"));}post(format!("/api/workspaces/{workspace_id}/releases/{id}/commands"),json!({"command":command,"expected_revision":expected_revision}),Some(checked_key(idempotency_key)?))}
        SimulateRelease { workspace_id, id, scenario, expected_revision, seed, virtual_seconds, idempotency_key } => {
            valid_id(&workspace_id)?;valid_id(&id)?;pick(&scenario,&["replay_stage","inject_oom","inject_failure"])?;
            if expected_revision<1 || virtual_seconds.is_some_and(|value|value<0){return Err(ApiError::new("invalid simulation revision or duration"));}
            let mut body=json!({"scenario":scenario,"expected_revision":expected_revision});
            if let Some(seed)=seed {body["seed"]=json!(seed);} if let Some(seconds)=virtual_seconds {body["virtual_seconds"]=json!(seconds);}
            post(format!("/api/workspaces/{workspace_id}/releases/{id}/simulation"),body,Some(checked_key(idempotency_key)?))
        }
        ReleaseEvents { workspace_id, id, after, limit } => {
            valid_id(&workspace_id)?;valid_id(&id)?;
            if limit.is_some_and(|value|!(1..=100).contains(&value)){return Err(ApiError::new("invalid page limit"));}
            let mut path=format!("/api/workspaces/{workspace_id}/releases/{id}/events");
            let mut query=Vec::new();if let Some(after)=after{query.push(format!("after={after}"));}if let Some(limit)=limit{query.push(format!("limit={limit}"));}
            if !query.is_empty(){path.push('?');path.push_str(&query.join("&"));}get(path)
        }
    };
    if let Some(body)=&spec.body {
        if body.to_string().len()>64*1024{return Err(ApiError::new("model engineering request exceeds 64 KiB"));}
    }
    Ok(spec)
}

fn decode_response(status: u16, body: &str) -> Result<Value, ApiError> {
    let parsed: Value=match serde_json::from_str(body) {
        Ok(value)=>value,
        Err(error)=>{
            let mut failure=ApiError::new(if (200..300).contains(&status) {format!("invalid backend response: {error}")} else {body.chars().take(4096).collect()});
            if !(200..300).contains(&status){failure.status=Some(status);}
            return Err(failure);
        }
    };
    if (200..300).contains(&status) {return Ok(parsed);}
    let envelope=parsed.get("error");
    let message=envelope.and_then(|error|error.get("message").and_then(Value::as_str).or_else(||error.as_str()))
        .or_else(||parsed.get("message").and_then(Value::as_str)).unwrap_or("model engineering request failed");
    let mut error=ApiError::new(message.chars().take(4096).collect::<String>());
    error.status=Some(status);
    error.code=envelope.and_then(|value|value.get("code")).and_then(Value::as_str).map(str::to_owned);
    error.details=envelope.and_then(|value|value.get("details")).cloned();
    Err(error)
}

fn send(base: &str, authorization: &str, spec: RequestSpec) -> Result<Value, ApiError> {
    let url=format!("{base}{}",spec.path);
    let timeout=if spec.path.contains("/live-canaries/") && spec.method==Method::Post {140} else if spec.path.ends_with("/evaluations/preparation") {50} else {20};
    let agent=ureq::AgentBuilder::new().timeout(Duration::from_secs(timeout)).redirects(0).try_proxy_from_env(false).build();
    let mut request=match spec.method {Method::Get=>agent.get(&url),Method::Post=>agent.post(&url)}
        .set("Authorization",authorization);
    if let Some(key)=&spec.key {request=request.set("Idempotency-Key",key);}
    let result=match spec.body {Some(body)=>request.set("Content-Type","application/json").send_string(&body.to_string()),None=>request.call()};
    let response=match result {Ok(response)=>response,Err(ureq::Error::Status(_,response))=>response,Err(error)=>return Err(ApiError::new(format!("model engineering transport: {error}")))};
    let status=response.status();let mut text=String::new();
    response.into_reader().take(8*1024*1024+1).read_to_string(&mut text).map_err(|error|ApiError::new(format!("read backend response: {error}")))?;
    if text.len()>8*1024*1024{return Err(ApiError::new("model engineering response exceeds 8 MiB"));}
    decode_response(status,&text)
}

#[tauri::command]
pub async fn model_engineering(request: ModelEngineeringRequest) -> Result<Value, ApiError> {
    let spec=build_request(request)?;
    tauri::async_runtime::spawn_blocking(move || {
        let connection=crate::runtime::backend_connection()?;
        send(&connection.base,&connection.authorization(),spec)
    }).await.map_err(|error|ApiError::new(format!("model engineering task: {error}")))?
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn live_canary_transport_has_fixed_scoped_routes_and_keeps_intent() {
        let command=json!({"operation":"live_canary_command","workspace_id":"local","id":"live-a","command":"rollback","expected_revision":4,"idempotency_key":"same-key"});
        let spec=build_request(serde_json::from_value(command).unwrap()).unwrap();
        assert_eq!(spec.path,"/api/workspaces/local/live-canaries/live-a/commands");
        assert_eq!(spec.key.as_deref(),Some("same-key"));
        assert_eq!(spec.body.unwrap(),json!({"command":"rollback","expected_revision":4}));
        let request=json!({"operation":"live_canary_request","workspace_id":"local","id":"live-a","prompt":"实际用户输入","idempotency_key":"request-key"});
        let spec=build_request(serde_json::from_value(request).unwrap()).unwrap();
        assert_eq!(spec.path,"/api/workspaces/local/live-canaries/live-a/requests");
        assert_eq!(spec.body.unwrap(),json!({"prompt":"实际用户输入"}));
        let feedback=json!({"operation":"live_canary_feedback","workspace_id":"local","id":"live-a","request_id":"request-1","rating":"good","note":"人工确认","idempotency_key":"feedback-key"});
        let spec=build_request(serde_json::from_value(feedback).unwrap()).unwrap();
        assert_eq!(spec.path,"/api/workspaces/local/live-canaries/live-a/feedback");
        assert_eq!(spec.body.unwrap()["request_id"],"request-1");
        for (operation,path) in [("get_live_canary","/api/workspaces/local/live-canaries/live-a"),("list_live_canary_requests","/api/workspaces/local/live-canaries/live-a/requests"),("live_canary_events","/api/workspaces/local/live-canaries/live-a/events?after=0&limit=20")] {
            let spec=build_request(serde_json::from_value(json!({"operation":operation,"workspace_id":"local","id":"live-a"})).unwrap()).unwrap();
            assert_eq!(spec.method,Method::Get);assert_eq!(spec.path,path);assert!(spec.key.is_none());
        }
    }

    #[test]
    fn live_canary_transport_rejects_unsafe_overrides_bounds_and_virtual_time() {
        let request=json!({"operation":"live_canary_request","workspace_id":"local","id":"live-a","prompt":"真实请求","idempotency_key":"request-key"});
        for (key,value) in [("workspace_id",json!("a/b")),("id",json!("live?x=1")),("model_id",json!("override")),("url",json!("http://evil")),("prompt",json!("中".repeat(5462))),("prompt",json!(" ")),("idempotency_key",json!(""))] {
            let mut invalid=request.clone();invalid[key]=value;
            assert!(serde_json::from_value::<ModelEngineeringRequest>(invalid).map_err(|e|ApiError::new(e.to_string())).and_then(build_request).is_err());
        }
        for invalid in [json!({"operation":"live_canary_command","workspace_id":"local","id":"live-a","command":"advance","expected_revision":1,"idempotency_key":"key","virtual_seconds":86400}),json!({"operation":"list_live_canary_requests","workspace_id":"local","id":"live-a","limit":21}),json!({"operation":"live_canary_feedback","workspace_id":"local","id":"live-a","request_id":"request-1","rating":"5","note":"","idempotency_key":"key"})] {
            assert!(serde_json::from_value::<ModelEngineeringRequest>(invalid).map_err(|e|ApiError::new(e.to_string())).and_then(build_request).is_err());
        }
        let create=json!({"operation":"create_live_canary","workspace_id":"local","title":"真实灰度","stable_revision_id":"model-a","candidate_revision_id":"model-b","profile_id":"profile-a","target_bps":500,"policy":{"window_seconds":60,"minimum_samples_per_arm":2,"minimum_feedback_per_arm":1,"maximum_error_rate":0.1,"maximum_bad_feedback_rate":0.2,"maximum_latency_ratio":1.5,"auto_pause_consecutive_errors":2},"idempotency_key":"create-key"});
        assert!(build_request(serde_json::from_value(create.clone()).unwrap()).is_ok());
        for (key,value) in [("target_bps",json!(10000)),("candidate_revision_id",json!("model-a"))] {
            let mut invalid=create.clone();invalid[key]=value;assert!(build_request(serde_json::from_value(invalid).unwrap()).is_err());
        }
        let mut short_window=create;short_window["policy"]["window_seconds"]=json!(0);
        assert!(build_request(serde_json::from_value(short_window).unwrap()).is_err());
    }

    #[test]
    fn case_report_selection_is_fixed_scoped_and_rejects_injected_queries() {
        let spec=build_request(serde_json::from_value(json!({"operation":"get_case","workspace_id":"local","evaluation_id":"eval","case_id":"case-one","report_id":"report-old","limit":100})).unwrap()).unwrap();
        assert_eq!(spec.path,"/api/workspaces/local/evaluations/eval/cases/case-one?limit=100&report_id=report-old");
        for extra in [json!({"report_id":"../bad"}),json!({"report_id":"report&limit=1"}),json!({"report_id":"report-old","cursor":"older"})] {
            let mut request=json!({"operation":"get_case","workspace_id":"local","evaluation_id":"eval","case_id":"case-one"});
            for (key,value) in extra.as_object().unwrap() { request[key]=value.clone(); }
            assert!(serde_json::from_value(request).map_err(|e|ApiError::new(e.to_string())).and_then(build_request).is_err());
        }
    }

    #[test]
    fn workspace_datasets_keep_routes_scope_and_lossless_int64_documents() {
        let document=r#"{"schema_version":1,"benchmark_id":"my-go","version":"1.0.0","title":"My tasks","scoring_rule":"exact","cases":[{"id":"integer-edge","category":"logic","task":"Preserve input","function":"Solve","input_type":"int64","output_type":"int64","faulty_source":"package main\nfunc Solve(input int64) int64 { return 0 }","assertions":[{"input":9223372036854775807,"expected":-9223372036854775808}]}]}"#;
        let request:ModelEngineeringRequest=serde_json::from_value(json!({"operation":"create_dataset","workspace_id":"local","document_json":document})).unwrap();
        let spec=build_request(request).unwrap();
        assert_eq!(spec.method,Method::Post);
        assert_eq!(spec.path,"/api/workspaces/local/datasets");
        let body=spec.body.unwrap();
        assert_eq!(body["cases"][0]["assertions"][0]["input"].as_i64(),Some(i64::MAX));
        assert_eq!(body["cases"][0]["assertions"][0]["expected"].as_i64(),Some(i64::MIN));
        for (operation,path) in [("get_dataset","/api/workspaces/local/datasets/custom.abc.def"),("archive_dataset","/api/workspaces/local/datasets/custom.abc.def/archive")] {
            let mut value=json!({"operation":operation,"workspace_id":"local","id":"custom.abc.def"});
            if operation=="archive_dataset" {value["archived"]=json!(true);}
            let spec=build_request(serde_json::from_value(value).unwrap()).unwrap();
            assert_eq!(spec.path,path);
        }
        let spec=build_request(serde_json::from_value(json!({"operation":"list_datasets","workspace_id":"local","include_archived":true})).unwrap()).unwrap();
        assert_eq!(spec.path,"/api/workspaces/local/datasets?include_archived=true");
        let legacy=build_request(serde_json::from_value(json!({"operation":"list_datasets"})).unwrap()).unwrap();
        assert_eq!(legacy.path,"/api/model-lab/datasets");
    }

    #[test]
    fn dataset_transport_rejects_paths_scripts_ambiguous_and_oversized_documents() {
        for value in [
            json!({"operation":"get_dataset","workspace_id":"a/b","id":"custom.x.y"}),
            json!({"operation":"archive_dataset","workspace_id":"local","id":"../other","archived":true}),
            json!({"operation":"create_dataset","workspace_id":"local"}),
            json!({"operation":"create_dataset","workspace_id":"local","document":{},"document_json":"{}"}),
            json!({"operation":"create_dataset","workspace_id":"local","document_json":"{\"script\":\"powershell\"}"}),
            json!({"operation":"create_dataset","workspace_id":"local","document_json":" ".repeat(64*1024+1)}),
        ] {
            let result=serde_json::from_value::<ModelEngineeringRequest>(value).map_err(|e|ApiError::new(e.to_string())).and_then(build_request);
            assert!(result.is_err());
        }
    }

    #[test]
    fn preparation_is_read_only_scoped_and_rejects_injected_paths() {
        let request=json!({"operation":"prepare_evaluation","workspace_id":"local","stable_revision_id":"model-a","candidate_revision_id":"model-a","profile_id":"profile-a","dataset_id":"dataset-a","workflow_id":"codelens.codetask.go.v1","evaluation_mode":"baseline"});
        let spec=build_request(serde_json::from_value(request.clone()).unwrap()).unwrap();
        assert_eq!(spec.method,Method::Post);
        assert_eq!(spec.path,"/api/workspaces/local/evaluations/preparation");
        assert!(spec.key.is_none());
        assert_eq!(spec.body.unwrap()["evaluation_mode"],"baseline");
        for (key,value) in [("workspace_id","../other"),("dataset_id","d?x=1"),("evaluation_mode","pretend"),("url","http://evil"),("idempotency_key","write-key")] {
            let mut invalid=request.clone(); invalid[key]=json!(value);
            assert!(serde_json::from_value::<ModelEngineeringRequest>(invalid).map_err(|e|ApiError::new(e.to_string())).and_then(build_request).is_err());
        }
    }

    #[test]
    fn evaluation_mode_is_explicit_validated_and_forwarded() {
        for mode in ["baseline", "compare"] {
            let body=json!({"operation":"create_evaluation","workspace_id":"local","stable_revision_id":"model-a","candidate_revision_id":"model-a","profile_id":"profile-a","evaluation_mode":mode,"idempotency_key":"first-run"});
            let request:ModelEngineeringRequest=serde_json::from_value(body).unwrap();
            let spec=build_request(request).unwrap();
            assert_eq!(spec.path,"/api/workspaces/local/evaluations");
            assert_eq!(spec.key.as_deref(),Some("first-run"));
            assert_eq!(spec.body.unwrap()["evaluation_mode"],mode);
        }
        let invalid=json!({"operation":"create_evaluation","workspace_id":"local","stable_revision_id":"model-a","candidate_revision_id":"model-a","profile_id":"profile-a","evaluation_mode":"pretend","idempotency_key":"first-run"});
        let rejected=serde_json::from_value::<ModelEngineeringRequest>(invalid)
            .map_err(|e|ApiError::new(e.to_string())).and_then(build_request);
        assert!(rejected.is_err());
    }

    #[test]
    fn fixed_operations_reject_path_injection_and_unknown_fields() {
        let invalid = [
            r#"{"operation":"get_release","workspace_id":"ws/a","id":"release-a"}"#,
            r#"{"operation":"get_release","workspace_id":"ws","id":"a?x=1"}"#,
            r#"{"operation":"get_release","workspace_id":"ws","id":"a","url":"http://evil"}"#,
            r#"{"operation":"create_release","workspace_id":"ws","mode":"live","workflow_id":"wf","report_id":"r","idempotency_key":"key"}"#,
            r#"{"operation":"simulate_release","workspace_id":"ws","id":"rel","scenario":"replay_stage","expected_revision":0,"idempotency_key":"key"}"#,
        ];
        for value in invalid {
            let result = serde_json::from_str::<ModelEngineeringRequest>(value)
                .map_err(|error| ApiError::new(error.to_string()))
                .and_then(|request| build_request(request));
            assert!(result.is_err(), "accepted {value}");
        }
    }

    #[test]
    fn simulation_request_keeps_cas_and_idempotency_key() {
        let request: ModelEngineeringRequest = serde_json::from_str(r#"{"operation":"simulate_release","workspace_id":"ws","id":"rel","scenario":"replay_stage","expected_revision":4,"seed":0,"idempotency_key":"same-key"}"#).unwrap();
        let spec = build_request(request).unwrap();
        assert_eq!(spec.path, "/api/workspaces/ws/releases/rel/simulation");
        assert_eq!(spec.method, Method::Post);
        assert_eq!(spec.key.as_deref(), Some("same-key"));
        assert_eq!(spec.body.unwrap(), serde_json::json!({"scenario":"replay_stage","expected_revision":4,"seed":0}));
    }

    #[test]
    fn backend_gate_error_keeps_status_code_and_details() {
        let error = decode_response(409, r#"{"error":{"code":"GATE_DENIED","message":"evidence missing","details":{"current_revision":7,"gates":[{"status":"insufficient"}]}}}"#).unwrap_err();
        let value = serde_json::to_value(error).unwrap();
        assert_eq!(value["message"], "evidence missing");
        assert_eq!(value["status"], 409);
        assert_eq!(value["code"], "GATE_DENIED");
        assert_eq!(value["details"]["gates"][0]["status"], "insufficient");
    }

    #[test]
    fn non_json_backend_failure_still_keeps_http_status() {
        let error=decode_response(503,"upstream unavailable").unwrap_err();
        assert_eq!(error.status,Some(503));
        assert!(error.message.contains("upstream unavailable"));
    }

    #[test]
    fn old_message_only_error_remains_unchanged() {
        assert_eq!(serde_json::to_value(ApiError::new("old")).unwrap(), serde_json::json!({"message":"old"}));
    }

    /// Opt-in only: the Go harness starts real authenticated handlers and an
    /// ephemeral store. This invokes the same builder and sender as the IPC.
    #[test]
    #[ignore]
    fn real_authenticated_go_simulation_round_trip() {
        let ready_path=std::env::var("NOOSPHERE_MODEL_ENGINEERING_READY_FILE").expect("Go harness ready file path");
        struct Stop(String);
        impl Drop for Stop {fn drop(&mut self){let _=std::fs::write(&self.0,b"stop");}}
        let _stop=Stop(format!("{ready_path}.stop"));
        let ready:Value=serde_json::from_slice(&std::fs::read(&ready_path).expect("Go harness ready file")).unwrap();
        let base=format!("http://127.0.0.1:{}",ready["port"].as_u64().unwrap());
        let auth=format!("Bearer {}",ready["token"].as_str().unwrap());
        let workspace=ready["workspace_id"].as_str().unwrap().to_owned();
        let release=ready["release_id"].as_str().unwrap().to_owned();
        let report=ready["report_id"].as_str().unwrap().to_owned();
        let revision=ready["revision"].as_i64().unwrap();
        let report_value=send(&base,&auth,build_request(ModelEngineeringRequest::GetReport {workspace_id:workspace.clone(),id:report.clone()}).unwrap()).unwrap();
        assert_eq!(report_value["report"]["id"],report);
        assert!(!report_value["report_hash"].as_str().unwrap().is_empty());
        let request=ModelEngineeringRequest::SimulateRelease {workspace_id:workspace.clone(),id:release.clone(),scenario:"replay_stage".into(),expected_revision:revision,seed:Some(7),virtual_seconds:Some(60),idempotency_key:"desktop-retry-same-key".into()};
        let first=send(&base,&auth,build_request(request.clone()).unwrap()).unwrap();
        assert_eq!(first["release_id"],release);
        assert!(!first["run_id"].as_str().unwrap_or("").is_empty());
        let retry=send(&base,&auth,build_request(request).unwrap()).unwrap();
        assert_eq!(retry["run_id"],first["run_id"]);
        let deadline=std::time::Instant::now()+Duration::from_secs(20);
        loop {
            let current=send(&base,&auth,build_request(ModelEngineeringRequest::GetRelease {workspace_id:workspace.clone(),id:release.clone()}).unwrap()).unwrap();
            if current["simulation"]["task"]["status"]=="completed" {
                assert!(current["revision"].as_i64().unwrap()>revision);
                assert_eq!(current["simulation"]["source"],"simulation_replay");
                assert_eq!(current["simulation"]["offline_source"],"offline_fixture");
                let stages=current["simulation"]["stages"].as_array().unwrap();
                assert!(!stages.is_empty());
                assert_eq!(stages.last().unwrap()["report_id"],report);
                break;
            }
            assert!(std::time::Instant::now()<deadline,"durable simulation did not complete: {current}");
            std::thread::sleep(Duration::from_millis(100));
        }
    }
}
