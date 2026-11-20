use crate::commands::types::ApiError;
use crate::commands::workspaces::request_with_timeout;
use serde_json::{json, Value};
use std::path::{Path, PathBuf};
use std::time::Duration;

fn project_at_launch(explicit: Option<PathBuf>, current: Option<PathBuf>) -> Option<PathBuf> {
    if let Some(path) = explicit {
        let path = if path.is_absolute() { path } else { current?.join(path) };
        return path.is_dir().then_some(path);
    }
    let path = current?;
    [".git", "go.mod", "Cargo.toml", "package.json", "pyproject.toml", ".noosphere"]
        .iter()
        .any(|marker| path.join(marker).exists())
        .then_some(path)
}

#[tauri::command]
pub fn project_launch_candidate() -> Option<String> {
    let mut args = std::env::args_os().skip(1);
    let explicit = args.find(|arg| arg == "--project").and_then(|_| args.next());
    project_at_launch(explicit.map(PathBuf::from), std::env::current_dir().ok())?
        .into_os_string().into_string().ok()
}

#[tauri::command]
pub async fn open_project_workspace(path: String) -> Result<Value, ApiError> {
    if !Path::new(&path).is_absolute() {
        return Err(ApiError::new("请选择本机项目文件夹"));
    }
    request_with_timeout("/api/workspaces/open".into(), Some(json!({ "path": path })), Duration::from_secs(15)).await
}

#[tauri::command]
pub async fn pick_project_folder(window: tauri::WebviewWindow) -> Result<Option<String>, ApiError> {
    #[cfg(windows)]
    {
        let parent = window.hwnd().map_err(|error| ApiError::new(error.to_string()))?.0 as usize;
        tauri::async_runtime::spawn_blocking(move || pick_folder(parent)).await
            .map_err(|error| ApiError::new(error.to_string()))?
            .map_err(|error| ApiError::new(format!("无法打开文件夹选择器：{error}")))
    }
    #[cfg(unix)]
    {
        use tauri_plugin_dialog::DialogExt;
        let (send, receive) = tokio::sync::oneshot::channel();
        window.dialog().file().set_parent(&window).set_title("选择项目文件夹")
            .pick_folder(move |folder| { let _ = send.send(folder); });
        match receive.await.map_err(|_| ApiError::new("文件夹选择器未返回结果"))? {
            Some(folder) => folder.into_path()
                .map_err(|error| ApiError::new(error.to_string()))?
                .into_os_string().into_string().map(Some)
                .map_err(|_| ApiError::new("项目路径不是有效的 UTF-8")),
            None => Ok(None),
        }
    }
}

#[tauri::command]
pub fn reveal_project_folder(path: String) -> Result<(), ApiError> {
    crate::platform::open_directory(Path::new(&path)).map_err(ApiError::new)
}

#[cfg(windows)]
fn pick_folder(parent: usize) -> windows::core::Result<Option<String>> {
    use windows::core::{w, HRESULT};
    use windows::Win32::Foundation::HWND;
    use windows::Win32::System::Com::{CoCreateInstance, CoInitializeEx, CoTaskMemFree, CoUninitialize, CLSCTX_INPROC_SERVER, COINIT_APARTMENTTHREADED};
    use windows::Win32::UI::Shell::{FileOpenDialog, IFileOpenDialog, FOS_FORCEFILESYSTEM, FOS_PATHMUSTEXIST, FOS_PICKFOLDERS, SIGDN_FILESYSPATH};

    struct ComApartment;
    impl Drop for ComApartment {
        fn drop(&mut self) { unsafe { CoUninitialize() } }
    }

    // COM objects stay on this blocking thread. The dialog is modal to the caller's window.
    unsafe {
        CoInitializeEx(None, COINIT_APARTMENTTHREADED).ok()?;
        let _apartment = ComApartment;
        let dialog: IFileOpenDialog = CoCreateInstance(&FileOpenDialog, None, CLSCTX_INPROC_SERVER)?;
        dialog.SetOptions(dialog.GetOptions()? | FOS_PICKFOLDERS | FOS_FORCEFILESYSTEM | FOS_PATHMUSTEXIST)?;
        dialog.SetTitle(w!("选择项目文件夹"))?;
        if let Err(error) = dialog.Show(Some(HWND(parent as _))) {
            // HRESULT_FROM_WIN32(ERROR_CANCELLED)
            return if error.code() == HRESULT(0x800704C7u32 as i32) { Ok(None) } else { Err(error) };
        }
        let path = dialog.GetResult()?.GetDisplayName(SIGDN_FILESYSPATH)?;
        let result = path.to_string();
        CoTaskMemFree(Some(path.0.cast()));
        result.map(Some).map_err(Into::into)
    }
}
