# Copy sidecar + default config into src-tauri/binaries for Tauri NSIS resources.
$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
$BridgeExe = Join-Path $Root "bridge\bin\yzj-bridge.exe"
if (-not (Test-Path $BridgeExe)) {
    throw "missing $BridgeExe — build the Go bridge first"
}
$BinDir = Join-Path $Root "gui\src-tauri\binaries"
New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
Copy-Item -Force $BridgeExe (Join-Path $BinDir "yzj-bridge.exe")
Copy-Item -Force (Join-Path $Root "config.default.yaml") (Join-Path $BinDir "config.default.yaml")
Write-Host "Prepared bundle sidecar: $BinDir"
