# Shared workspace runtime contract

Backend package `internal/workspaces` owns `Manager` and storage. Open(databasePath string) (*Manager,error); Close() error. SQLite location is app.data_dir/workspace.sqlite, falling back to the parent of the configured Nebula data directory if app.data_dir is empty. Default workspace ID `local`, display name 本地工作区, empty project path.

JSON Workspace: id,name,project_path,project_id,default_model_id,created_at,updated_at (all strings). `project_id` is backend-managed and set by successful index; updating project_path invalidates it. IDs are opaque safe strings. POST workspace with omitted id creates; known id updates. Unique canonical nonempty project path. Reject nonexistent/non-directory roots.

JSON Run: id,workspace_id,session_id,kind,title,status,phase,error,model_id,created_at,updated_at,result (JSON). Status queued/running/completed/failed/cancelled/interrupted. Kind chat/index/diagnosis. Event: seq (monotonic int),run_id,phase,message,data (JSON),created_at.

HTTP/IPC:
- GET /api/workspaces -> {workspaces:Workspace[]}; command list_workspaces
- POST /api/workspaces body Workspace -> Workspace; command save_workspace(workspace)
- GET /api/workspaces/{id}/runs -> {runs:Run[]} latest100 metadata only (result=null); command list_workspace_runs(workspaceId). Full results use GET /api/runs/{id}.
- POST /api/workspaces/{id}/runs body {kind,title,input} -> Run (202); command submit_workspace_run(workspaceId,kind,title,input)
- GET /api/runs/{id} -> Run; command get_workspace_run(id)
- GET /api/runs/{id}/events?after=N -> {events:Event[]}; command get_run_events(id,after)
- POST /api/runs/{id}/cancel -> Run; command cancel_workspace_run(id)

Go contract:
type Handler func(ctx context.Context, ws Workspace, input json.RawMessage, emit func(phase,message string,data any)) (any,error)
Manager.Register(kind,lane string, handler Handler); lanes interactive (2 concurrent) and background (1 concurrent), bounded queue. Submit(workspaceID,kind,title string,input json.RawMessage) (Run,error).
Each queue holds at most 32 pending jobs. External chat has 2 reserved slots independent of diagnostic workers. Deadline expiration is failed; explicit/context cancellation is cancelled. No automatic replay after process restart.
Manager.Workspace(id string) (Workspace,error); BindProject(workspaceID,expectedPath,projectID string) error (CAS root path, reject changed root).
Manager.RegisterRoutes(mux *http.ServeMux).
Manager.Start(ctx context.Context,spec Run) (context.Context,*Execution,error) for existing synchronous chat stream, preserving spec.ID if supplied; blank workspace ID maps local. Duplicate run IDs rejected. Execution.Emit(phase,message string,data any); Execution.Finish(result any,err error) error; Finish uses cancellation context when set; terminal state monotonic. Manager.Cancel(id) and Runs/workspace/event helpers as needed.
Run result is durable JSON bounded to 2MiB; external chat checkpoints may use Emit data with preview only, final full result stored within bound. Never put credentials in run/event metadata.

Registered workers (root implements): index input {exclude?:string[]}, uses ws.project_path and returns existing codelens index summary with project_id/retrieval_ready; diagnosis input {issue:string}, returns existing {answer,trace,...}. Chat streaming stays current gateway_chat_stream command with added optional workspaceId/sessionId and request_id sent upstream. Root backend uses Manager.Start to track it.

Frontend api additions (IPC agent implements): Workspace,WorkspaceRun,RunEvent types; api.listWorkspaces(),saveWorkspace(workspace),listWorkspaceRuns(workspaceId),submitWorkspaceRun(workspaceId,kind,title,input),getWorkspaceRun(id),getRunEvents(id,after=0),cancelWorkspaceRun(id).

UI agent implements workspace-store.ts exports useWorkspaceStore: {activeId:'local',workspaces:Workspace[],refresh():Promise<void>,select(id):void,save(input):Promise<Workspace>}; selection synchronizes useUiStore projectPath/projectId. Default workspace is valid before hydration. WorkspaceHub component manages selector/editor/task list and details, poll only while visible/active tasks.

Chat/IPC agent scopes ChatSession.workspaceId (optional legacy -> local); root chat UI filtering belongs to that agent. New sessions default to selected workspace; imported legacy sessions remain local. Send freezes workspaceId/sessionId/projectId/model choice. Metadata args optional for backwards compatibility.

Ownership: core agent owns internal/workspaces and go.mod/go.sum only. UI agent owns workspace-store,WorkspaceHub,layout/HomePage integration, useBackend indexProject and DiagnosticAgentPanel plus task-client helper. IPC/chat agent owns lib/api.ts, Rust commands/workspaces.rs+gateway.rs+mod.rs+lib.rs, chat store/runtime/sidebar/page scoping. Root owns bootstrap/server binding, gateway Go stream tracking, worker adapter integration and integration tests.
