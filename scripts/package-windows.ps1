# Collect NSIS installer and portable folder into dist/.
$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
$TauriConfPath = Join-Path $Root "gui\src-tauri\tauri.conf.json"
$TauriConf = Get-Content $TauriConfPath -Raw -Encoding UTF8 | ConvertFrom-Json
$Ver = [string]$TauriConf.version
if (-not $Ver) { throw "tauri.conf.json missing version" }

$TargetRoot = Join-Path $Root "gui\src-tauri\target"
$ReleaseDirs = @(
    (Join-Path $TargetRoot "release"),
    (Join-Path $TargetRoot "x86_64-pc-windows-msvc\release"),
    (Join-Path $TargetRoot "x86_64-pc-windows-gnu\release")
) | Where-Object { Test-Path $_ }

$Setup = $null
foreach ($rel in $ReleaseDirs) {
    $nsis = Join-Path $rel "bundle\nsis"
    $found = Get-ChildItem $nsis -Filter "*-setup.exe" -ErrorAction SilentlyContinue |
        Sort-Object LastWriteTime -Descending |
        Select-Object -First 1
    if ($found) {
        $Setup = $found
        break
    }
}
if (-not $Setup) {
    throw "NSIS installer not found under $TargetRoot"
}

$Dist = Join-Path $Root "dist"
New-Item -ItemType Directory -Force -Path $Dist | Out-Null
$Versioned = Join-Path $Dist "YZJBridge-$Ver-Windows-x64-setup.exe"
$Latest = Join-Path $Dist "YZJBridge-Windows-x64-setup.exe"
Copy-Item -Force $Setup.FullName $Versioned
Copy-Item -Force $Setup.FullName $Latest

$Portable = Join-Path $Dist "YZJBridge"
New-Item -ItemType Directory -Force -Path $Portable | Out-Null
$GuiExe = $null
$Dll = $null
foreach ($rel in $ReleaseDirs) {
    foreach ($name in @("YZJBridge.exe", "gui.exe")) {
        $p = Join-Path $rel $name
        if ((-not $GuiExe) -and (Test-Path $p)) { $GuiExe = $p }
    }
    $dllPath = Join-Path $rel "WebView2Loader.dll"
    if ((-not $Dll) -and (Test-Path $dllPath)) { $Dll = $dllPath }
}
if (-not $GuiExe) {
    throw "missing GUI exe under $TargetRoot"
}
Copy-Item -Force $GuiExe (Join-Path $Portable "YZJBridge.exe")
if ($Dll) {
    Copy-Item -Force $Dll (Join-Path $Portable "WebView2Loader.dll")
}
Copy-Item -Force (Join-Path $Root "bridge\bin\yzj-bridge.exe") (Join-Path $Portable "yzj-bridge.exe")
Copy-Item -Force (Join-Path $Root "config.default.yaml") (Join-Path $Portable "config.default.yaml")

$bundleWebView2Loader = ($TauriConf.bundle.resources | Where-Object { $_ -eq "WebView2Loader.dll" }).Count -gt 0

# GNU builds need WebView2Loader.dll beside YZJBridge.exe; MSVC statically links it.
if ($bundleWebView2Loader -and -not $Dll) {
    throw "missing WebView2Loader.dll under $TargetRoot - run scripts/prepare-bundle.ps1 before tauri build"
}

$NsisScript = $null
foreach ($rel in $ReleaseDirs) {
    $candidate = Join-Path $rel "nsis\x64\installer.nsi"
    if (Test-Path $candidate) { $NsisScript = $candidate; break }
}
if ($NsisScript -and $bundleWebView2Loader) {
    $nsisText = Get-Content $NsisScript -Raw -Encoding UTF8
    if ($nsisText -notmatch "WebView2Loader\.dll") {
        throw "NSIS script does not bundle WebView2Loader.dll: $NsisScript"
    }
}

Write-Host "installer : $Versioned"
Write-Host "installer : $Latest"
Write-Host "portable  : $Portable\YZJBridge.exe"
