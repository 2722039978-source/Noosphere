mod commands;
mod runtime;
mod local_auth;
mod local_engines;
mod updates;
mod platform;
use commands::services::ServiceState;
use tauri::Manager;

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let mut builder = tauri::Builder::default();
    #[cfg(desktop)]
    {
        builder = builder.plugin(tauri_plugin_single_instance::init(|app, _args, _cwd| {
            show_main_window(app);
        }));
    }
    #[cfg(unix)]
    {
        builder = builder.plugin(tauri_plugin_dialog::init());
    }
    let app = builder
        .manage(ServiceState::default())
        .manage(local_engines::LocalEngines::default())
        .manage(commands::stream::StreamTasks::default())
        .invoke_handler(tauri::generate_handler![
            commands::services::start_backend,
            commands::services::check_health,
            commands::services::get_backend_status,
            commands::gateway::get_models,
            commands::gateway::add_model,
            commands::gateway::delete_model,
            commands::gateway::test_model,
            commands::gateway::probe_model,
            local_engines::list_local_engines,
            local_engines::control_local_engine,
            local_engines::local_engine_log,
            commands::model_selection::select_model,
            commands::model_selection::get_model_selection,
            commands::gateway::gateway_chat,
            commands::gateway::gateway_chat_stream,
            commands::gateway::gateway_chat_cancel,
            commands::attachments::stage_chat_attachment,
            commands::attachments::discard_chat_attachment,
            commands::attachments::prepare_chat_attachments,
            commands::attachments::delete_session_attachments,
            commands::gateway::gateway_stats,
            commands::nebula::list_sessions,
            commands::nebula::create_session,
            commands::nebula::store_memory,
            commands::nebula::search_memories,
            commands::nebula::nebula_stats,
            commands::devops::get_status,
            commands::devops::get_metrics,
            commands::devops::diagnose,
            commands::devops::execute_tool,
            commands::devops::list_tools,
            commands::devops::devops_loop,
            commands::codelens::codelens_index,
            commands::codelens::codelens_search,
            commands::codelens::codelens_analyze,
            commands::codelens::codelens_health,
            commands::codelens::codelens_trace,
            commands::codelens::codelens_project_search,
            commands::codelens::codelens_source,
            commands::codelens::codelens_inspect_artifact,
            commands::codelens::codelens_get_artifact,
            commands::workspaces::list_workspaces,
            commands::projects::project_launch_candidate,
            commands::projects::pick_project_folder,
            commands::projects::open_project_workspace,
            commands::projects::reveal_project_folder,
            platform::desktop_platform,
            commands::workspaces::list_knowledge,
            commands::workspaces::save_knowledge,
            commands::workspaces::suggest_knowledge_revision,
            commands::workspaces::delete_knowledge,
            commands::workspaces::prepare_context,
            commands::workspaces::save_workspace,
            commands::workspaces::list_workspace_runs,
            commands::workspaces::submit_workspace_run,
            commands::workspaces::list_agent_actions,
            commands::workspaces::decide_agent_action,
            commands::workspaces::get_workspace_run,
            commands::workspaces::get_run_events,
            commands::workspaces::cancel_workspace_run,
            commands::model_engineering::model_engineering,
            updates::update_check,
            updates::update_download,
            updates::update_show_folder,
        ])
        .setup(|app| {
            let paths = runtime::RuntimePaths::from_app(app.handle());
            app.state::<ServiceState>().initialize(paths);
            Ok(())
        })
        .on_window_event(|window, event| {
            #[cfg(target_os = "macos")]
            if window.label() == "main" {
                if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                    api.prevent_close();
                    let _ = window.hide();
                }
            }
            #[cfg(not(target_os = "macos"))]
            let _ = (window, event);
        })
        .build(tauri::generate_context!())
        .expect("error while building Noosphere Desktop");

    app.run(|app, event| match event {
        #[cfg(target_os = "macos")]
        tauri::RunEvent::Reopen { .. } => show_main_window(app),
        tauri::RunEvent::Exit => {
            app.state::<commands::stream::StreamTasks>().cancel_all();
            app.state::<ServiceState>().shutdown();
            app.state::<local_engines::LocalEngines>().shutdown();
        }
        _ => {}
    });
}

#[cfg(desktop)]
fn show_main_window(app: &tauri::AppHandle) {
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.unminimize();
        let _ = window.show();
        let _ = window.set_focus();
    }
}
