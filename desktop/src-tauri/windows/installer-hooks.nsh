; The application's normal exit drains writes. Refuse running processes before
; Tauri's default NSIS process check offers to terminate the application.
!macro NoosphereRequireStopped
  nsis_tauri_utils::FindProcessCurrentUser "noosphere-desktop.exe"
  Pop $R8
  nsis_tauri_utils::FindProcessCurrentUser "Noosphere.exe"
  Pop $R9
  nsis_tauri_utils::FindProcessCurrentUser "noosphere-backend.exe"
  Pop $R7
  ${If} $R8 = 0
  ${OrIf} $R9 = 0
  ${OrIf} $R7 = 0
    SetErrorLevel 2
    Abort "Close Noosphere normally before installing or uninstalling."
  ${EndIf}
!macroend

!macro NSIS_HOOK_PREINSTALL
  !insertmacro NoosphereRequireStopped
!macroend

!macro NSIS_HOOK_PREUNINSTALL
  !insertmacro NoosphereRequireStopped
!macroend
