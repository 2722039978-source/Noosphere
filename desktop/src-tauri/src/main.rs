// Prevents additional console window on Windows in release, DO NOT REMOVE!!
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

fn main() {
  #[cfg(target_os = "linux")]
  {
    use std::env::{set_var, var_os};

    // Tauri's AppImage hook selects X11 before this executable starts.
    // In a Wayland-only session, select its available backend before GTK starts.
    if var_os("APPIMAGE").is_some()
      && var_os("GDK_BACKEND").is_some_and(|backend| backend == "x11")
      && var_os("DISPLAY").is_none_or(|display| display.is_empty())
      && (var_os("WAYLAND_DISPLAY").is_some_and(|display| !display.is_empty())
        || var_os("WAYLAND_SOCKET").is_some_and(|socket| !socket.is_empty()))
    {
      set_var("GDK_BACKEND", "wayland");
    }
  }
  app_lib::run();
}
