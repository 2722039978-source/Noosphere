use crate::commands::types::ApiError;
use serde::Deserialize;

#[derive(Clone, Copy, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Kind {
    Models,
    Model,
    Projector,
    Runtime,
    Directory,
}

impl Kind {
    fn title(self) -> &'static str {
        match self {
            Self::Models | Self::Model => "选择 GGUF 模型",
            Self::Projector => "选择视觉投影器",
            Self::Runtime => "选择 llama-server 运行时",
            Self::Directory => "选择要扫描的目录",
        }
    }
}

pub async fn pick(window: tauri::WebviewWindow, kind: Kind) -> Result<Vec<String>, ApiError> {
    #[cfg(windows)]
    {
        let parent = window.hwnd().map_err(|e| ApiError::new(e.to_string()))?.0 as usize;
        tauri::async_runtime::spawn_blocking(move || windows_pick(parent, kind))
            .await
            .map_err(|e| ApiError::new(e.to_string()))?
            .map_err(|e| ApiError::new(format!("无法选择文件：{e}")))
    }
    #[cfg(unix)]
    {
        use tauri_plugin_dialog::DialogExt;
        let builder = window
            .dialog()
            .file()
            .set_parent(&window)
            .set_title(kind.title());
        let builder = if matches!(kind, Kind::Models | Kind::Model | Kind::Projector) {
            builder.add_filter("GGUF", &["gguf", "GGUF"])
        } else {
            builder
        };
        let (tx, rx) = tokio::sync::oneshot::channel();
        match kind {
            Kind::Directory => builder.pick_folder(move |f| {
                let _ = tx.send(f.into_iter().collect::<Vec<_>>());
            }),
            Kind::Models => builder.pick_files(move |files| {
                let _ = tx.send(files.unwrap_or_default());
            }),
            _ => builder.pick_file(move |f| {
                let _ = tx.send(f.into_iter().collect::<Vec<_>>());
            }),
        }
        rx.await
            .map_err(|e| ApiError::new(e.to_string()))?
            .into_iter()
            .map(|p| {
                p.into_path()
                    .map_err(|e| ApiError::new(e.to_string()))?
                    .into_os_string()
                    .into_string()
                    .map_err(|_| ApiError::new("文件路径不是有效的 UTF-8"))
            })
            .collect()
    }
}

#[cfg(windows)]
fn windows_pick(parent: usize, kind: Kind) -> windows::core::Result<Vec<String>> {
    use windows::core::{w, HRESULT, HSTRING};
    use windows::Win32::Foundation::HWND;
    use windows::Win32::System::Com::{
        CoCreateInstance, CoInitializeEx, CoTaskMemFree, CoUninitialize, CLSCTX_INPROC_SERVER,
        COINIT_APARTMENTTHREADED,
    };
    use windows::Win32::UI::Shell::{
        Common::COMDLG_FILTERSPEC, FileOpenDialog, IFileOpenDialog, FOS_ALLOWMULTISELECT,
        FOS_FILEMUSTEXIST, FOS_FORCEFILESYSTEM, FOS_PATHMUSTEXIST, FOS_PICKFOLDERS,
        SIGDN_FILESYSPATH,
    };
    struct Apartment;
    impl Drop for Apartment {
        fn drop(&mut self) {
            unsafe { CoUninitialize() }
        }
    }
    unsafe {
        CoInitializeEx(None, COINIT_APARTMENTTHREADED).ok()?;
        let _apartment = Apartment;
        let dialog: IFileOpenDialog =
            CoCreateInstance(&FileOpenDialog, None, CLSCTX_INPROC_SERVER)?;
        let mut options = dialog.GetOptions()? | FOS_FORCEFILESYSTEM | FOS_PATHMUSTEXIST;
        if matches!(kind, Kind::Directory) {
            options |= FOS_PICKFOLDERS;
        } else {
            options |= FOS_FILEMUSTEXIST;
        }
        if matches!(kind, Kind::Models) {
            options |= FOS_ALLOWMULTISELECT;
        }
        dialog.SetOptions(options)?;
        dialog.SetTitle(&HSTRING::from(kind.title()))?;
        if !matches!(kind, Kind::Directory) {
            let filter = if matches!(kind, Kind::Runtime) {
                COMDLG_FILTERSPEC {
                    pszName: w!("llama-server"),
                    pszSpec: w!("llama-server.exe"),
                }
            } else {
                COMDLG_FILTERSPEC {
                    pszName: w!("GGUF"),
                    pszSpec: w!("*.gguf"),
                }
            };
            dialog.SetFileTypes(&[filter])?;
        }
        if let Err(e) = dialog.Show(Some(HWND(parent as _))) {
            return if e.code() == HRESULT(0x800704C7u32 as i32) {
                Ok(vec![])
            } else {
                Err(e)
            };
        }
        let items = dialog.GetResults()?;
        let mut paths = vec![];
        for i in 0..items.GetCount()? {
            let path = items.GetItemAt(i)?.GetDisplayName(SIGDN_FILESYSPATH)?;
            let value = path.to_string();
            CoTaskMemFree(Some(path.0.cast()));
            paths.push(value?);
        }
        Ok(paths)
    }
}
pub async fn confirm_execution(
    window: &tauri::WebviewWindow,
    path: &std::path::Path,
    digest: &str,
) -> Result<bool, ApiError> {
    let text=format!("允许 Noosphere 执行此运行时以检测版本、设备并启动模型？\n\n{}\n\n程序及同目录动态库 SHA-256\n{digest}",path.display());
    #[cfg(windows)]
    {
        use windows_sys::Win32::UI::WindowsAndMessaging::{
            MessageBoxW, IDYES, MB_DEFBUTTON2, MB_ICONWARNING, MB_YESNO,
        };
        let parent = window.hwnd().map_err(|e| ApiError::new(e.to_string()))?.0 as usize;
        let title: Vec<u16> = "Noosphere · 运行时执行授权"
            .encode_utf16()
            .chain([0])
            .collect();
        let text: Vec<u16> = text.encode_utf16().chain([0]).collect();
        tauri::async_runtime::spawn_blocking(move || unsafe {
            MessageBoxW(
                parent as _,
                text.as_ptr(),
                title.as_ptr(),
                MB_YESNO | MB_DEFBUTTON2 | MB_ICONWARNING,
            ) == IDYES
        })
        .await
        .map_err(|e| ApiError::new(e.to_string()))
    }
    #[cfg(unix)]
    {
        use tauri_plugin_dialog::{DialogExt, MessageDialogButtons, MessageDialogKind};
        let (send, recv) = tokio::sync::oneshot::channel();
        window
            .dialog()
            .message(text)
            .parent(window)
            .title("Noosphere · 运行时执行授权")
            .kind(MessageDialogKind::Warning)
            .buttons(MessageDialogButtons::YesNo)
            .show(move |allow| {
                let _ = send.send(allow);
            });
        recv.await.map_err(|e| ApiError::new(e.to_string()))
    }
}
