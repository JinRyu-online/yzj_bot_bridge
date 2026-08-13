; Kill running app/bridge so upgrades can overwrite files.
!macro NSIS_HOOK_PREINSTALL
  nsExec::ExecToLog 'taskkill /F /IM YZJBridge.exe /T'
  nsExec::ExecToLog 'taskkill /F /IM yzj-bridge.exe /T'
  Sleep 400
!macroend

!macro NSIS_HOOK_PREUNINSTALL
  nsExec::ExecToLog 'taskkill /F /IM YZJBridge.exe /T'
  nsExec::ExecToLog 'taskkill /F /IM yzj-bridge.exe /T'
  Sleep 400
!macroend
