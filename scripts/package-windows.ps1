# Collect NSIS installer and portable folder into dist/.
$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
$TauriConfPath = Join-Path $Root "gui\src-tauri\tauri.conf.json"
$TauriConf = Get-Content $TauriConfPath -Raw | ConvertFrom-Json
$Ver = [string]$TauriConf.version
if (-not $Ver) { throw "tauri.conf.json missing version" }

$NsisDir = Join-Path $Root "gui\src-tauri\target\release\bundle\nsis"
$Setup = Get-ChildItem $NsisDir -Filter "*-setup.exe" -ErrorAction SilentlyContinue |
    Sort-Object LastWriteTime -Descending |
    Select-Object -First 1
if (-not $Setup) {
    throw "NSIS installer not found in $NsisDir"
}

$Dist = Join-Path $Root "dist"
New-Item -ItemType Directory -Force -Path $Dist | Out-Null
$Versioned = Join-Path $Dist "YZJBridge-$Ver-Windows-x64-setup.exe"
$Latest = Join-Path $Dist "YZJBridge-Windows-x64-setup.exe"
Copy-Item -Force $Setup.FullName $Versioned
Copy-Item -Force $Setup.FullName $Latest

$Portable = Join-Path $Dist "YZJBridge"
New-Item -ItemType Directory -Force -Path $Portable | Out-Null
$ReleaseDir = Join-Path $Root "gui\src-tauri\target\release"
$GuiExe = @(
    (Join-Path $ReleaseDir "YZJBridge.exe"),
    (Join-Path $ReleaseDir "gui.exe")
) | Where-Object { Test-Path $_ } | Select-Object -First 1
if (-not $GuiExe) {
    throw "missing GUI exe in $ReleaseDir"
}
Copy-Item -Force $GuiExe (Join-Path $Portable "YZJBridge.exe")
$Dll = Join-Path $ReleaseDir "WebView2Loader.dll"
if (Test-Path $Dll) {
    Copy-Item -Force $Dll (Join-Path $Portable "WebView2Loader.dll")
}
Copy-Item -Force (Join-Path $Root "bridge\bin\yzj-bridge.exe") (Join-Path $Portable "yzj-bridge.exe")
Copy-Item -Force (Join-Path $Root "config.default.yaml") (Join-Path $Portable "config.default.yaml")

Write-Host "installer : $Versioned"
Write-Host "installer : $Latest"
Write-Host "portable  : $Portable\YZJBridge.exe"
