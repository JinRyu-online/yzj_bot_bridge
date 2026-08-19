# Copy sidecar + default config + (GNU only) WebView2Loader.dll into src-tauri for Tauri NSIS resources.
$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
$BridgeExe = Join-Path $Root "bridge\bin\yzj-bridge.exe"
if (-not (Test-Path $BridgeExe)) {
    throw "missing $BridgeExe - build the Go bridge first"
}

$TauriDir = Join-Path $Root "gui\src-tauri"
$BinDir = Join-Path $TauriDir "binaries"
$TauriConfPath = Join-Path $TauriDir "tauri.conf.json"
New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
Copy-Item -Force $BridgeExe (Join-Path $BinDir "yzj-bridge.exe")
Copy-Item -Force (Join-Path $Root "config.default.yaml") (Join-Path $BinDir "config.default.yaml")

function Find-WebView2Loader {
    param([string]$TargetRoot)
    $releaseDirs = @(
        (Join-Path $TargetRoot "release"),
        (Join-Path $TargetRoot "x86_64-pc-windows-gnu\release"),
        (Join-Path $TargetRoot "x86_64-pc-windows-msvc\release")
    )
    foreach ($rel in $releaseDirs) {
        $dll = Join-Path $rel "WebView2Loader.dll"
        if (Test-Path $dll) { return $dll }
    }
    return $null
}

function Set-TauriWebView2LoaderResource {
    param([bool]$Include)
    $text = Get-Content $TauriConfPath -Raw -Encoding UTF8
    if ($Include) {
        if ($text -notmatch '"WebView2Loader\.dll"') {
            $text = $text -replace '("resources":\s*\[\s*\r?\n)', "`$1      `"WebView2Loader.dll`",`r`n"
        }
    } else {
        $text = $text -replace '\s*"WebView2Loader\.dll",\s*\r?\n', ''
    }
    [System.IO.File]::WriteAllText($TauriConfPath, $text, [System.Text.UTF8Encoding]::new($false))
}

function Test-GnuToolchainActive {
    $toolchain = (rustup show active-toolchain 2>$null)
    return $toolchain -match 'gnu'
}

$TargetRoot = Join-Path $TauriDir "target"
$destRoot = Join-Path $TauriDir "WebView2Loader.dll"
$destPortable = Join-Path $BinDir "WebView2Loader.dll"
Remove-Item $destRoot, $destPortable -ErrorAction SilentlyContinue

# Strip DLL resource first so MSVC/CI cargo invocations never fail on a missing file.
Set-TauriWebView2LoaderResource -Include $false

$loader = Find-WebView2Loader $TargetRoot
if (-not $loader -and (Test-GnuToolchainActive) -and $env:GITHUB_ACTIONS -ne "true") {
    Write-Host "WebView2Loader.dll not found; running cargo build --release (GNU local)..."
    Push-Location $TauriDir
    try {
        $gnuGcc = Join-Path $env:USERPROFILE ".rustup\toolchains\stable-x86_64-pc-windows-gnu\lib\rustlib\x86_64-pc-windows-gnu\bin\self-contained\x86_64-w64-mingw32-gcc.exe"
        if (Test-Path $gnuGcc) {
            $env:CARGO_TARGET_X86_64_PC_WINDOWS_GNU_LINKER = $gnuGcc
        }
        cargo build --release
        if ($LASTEXITCODE -ne 0) { throw "cargo build --release failed (exit $LASTEXITCODE)" }
    } finally {
        Pop-Location
    }
    $loader = Find-WebView2Loader $TargetRoot
}

if ($loader) {
    Copy-Item -Force $loader $destRoot
    Copy-Item -Force $loader $destPortable
    Set-TauriWebView2LoaderResource -Include $true
    Write-Host "Prepared WebView2Loader.dll: $destRoot"
} else {
    Write-Host "WebView2Loader.dll not bundled (MSVC/static WebView2 build)"
}

Write-Host "Prepared bundle sidecar: $BinDir"
