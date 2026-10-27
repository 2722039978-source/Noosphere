mod commands;
mod runtime;
mod local_auth;
use commands::services::ServiceState;
use tauri::Manager;

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let mut builder = tauri::Builder::default();
    #[cfg(desktop)]
    {
        builder = builder.plugin(tauri_plugin_single_instance::init(|app, _args, _cwd| {
            if let Some(window) = app.get_webview_window("main") {
                let _ = window.unminimize();
                let _ = window.show();
                let _ = window.set_focus();
            }
        }));
    }
    let app = builder
        .manage(ServiceState::default())
        .manage(commands::stream::StreamTasks::default())
        .invoke_handler(tauri::generate_handler![
            commands::services::start_backend,
            commands::services::check_health,
            commands::services::get_backend_status,
            commands::gateway::get_models,
            commands::gateway::add_model,
            commands::gateway::delete_model,
            commands::gateway::test_model,
            commands::model_selection::select_model,
            commands::model_selection::get_model_selection,
            commands::gateway::gateway_chat,
            commands::gateway::gateway_chat_stream,
            commands::gateway::gateway_chat_cancel,
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
            commands::workspaces::list_workspaces,
            commands::workspaces::list_knowledge,
            commands::workspaces::save_knowledge,
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
        ])
        .setup(|app| {
            let paths = runtime::RuntimePaths::from_app(app.handle());
            app.state::<ServiceState>().initialize(paths);
            Ok(())
        })
        .on_window_event(|window, event| {
            if let tauri::WindowEvent::Destroyed = event {
                if let Some(streams) = window
                    .app_handle()
                    .try_state::<commands::stream::StreamTasks>()
                {
                    streams.cancel_all();
                }
                if let Some(state) = window.app_handle().try_state::<ServiceState>() {
                    state.shutdown();
                }
            }
        })
        .build(tauri::generate_context!())
        .expect("error while building Noosphere Desktop");

    app.run(|app, event| {
        if let tauri::RunEvent::Exit = event {
            app.state::<commands::stream::StreamTasks>().cancel_all();
            app.state::<ServiceState>().shutdown();
        }
    });
}
