# Build Go bridge + Tauri GUI (Windows)
$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $Root

function Assert-LastExit([string]$Step) {
    if ($null -ne $LASTEXITCODE -and $LASTEXITCODE -ne 0) {
        throw "$Step failed (exit $LASTEXITCODE)"
    }
}

$exitCode = 0
try {
    Write-Host "==> Build Go bridge"
    Push-Location bridge
    try {
        $env:GOTOOLCHAIN = "local"
        go test ./...
        Assert-LastExit "go test"
        New-Item -ItemType Directory -Force -Path bin | Out-Null
        go build -o bin/yzj-bridge.exe ./cmd/yzj-bridge
        Assert-LastExit "go build"
    } finally {
        Pop-Location
    }

    Write-Host "==> Build Tauri GUI"
    # Prefer GNU target if MSVC link.exe is shadowed (e.g. coreutils)
    rustup default stable-x86_64-pc-windows-gnu 2>$null | Out-Null
    Push-Location gui
    try {
        if (-not (Test-Path node_modules)) { npm install; Assert-LastExit "npm install" }
        New-Item -ItemType Directory -Force -Path "src-tauri/binaries" | Out-Null
        Copy-Item -Force "..\bridge\bin\yzj-bridge.exe" "src-tauri\binaries\yzj-bridge.exe"
        npm run tauri build
        Assert-LastExit "tauri build"
    } finally {
        Pop-Location
    }

    Write-Host "==> Sync portable dist/YZJBridge"
    $DistDir = Join-Path $Root "dist\YZJBridge"
    New-Item -ItemType Directory -Force -Path $DistDir | Out-Null
    $GuiExe = Join-Path $Root "gui\src-tauri\target\release\gui.exe"
    if (-not (Test-Path $GuiExe)) {
        throw "missing GUI exe: $GuiExe"
    }
    Copy-Item -Force $GuiExe (Join-Path $DistDir "YZJBridge.exe")
    Copy-Item -Force (Join-Path $Root "bridge\bin\yzj-bridge.exe") (Join-Path $DistDir "yzj-bridge.exe")
    $DefaultCfg = Join-Path $Root "config.default.yaml"
    if (Test-Path $DefaultCfg) {
        Copy-Item -Force $DefaultCfg (Join-Path $DistDir "config.default.yaml")
    }
    $Setup = Get-ChildItem (Join-Path $Root "gui\src-tauri\target\release\bundle\nsis") -Filter "*-setup.exe" -ErrorAction SilentlyContinue |
        Sort-Object LastWriteTime -Descending |
        Select-Object -First 1
    if ($Setup) {
        Copy-Item -Force $Setup.FullName (Join-Path $Root "dist\$($Setup.Name)")
    }

    Write-Host "Done." -ForegroundColor Green
    Write-Host "  portable : $DistDir\YZJBridge.exe (+ yzj-bridge.exe)"
    Write-Host "  release  : gui\src-tauri\target\release\gui.exe"
    if ($Setup) {
        Write-Host "  installer: dist\$($Setup.Name)"
    }
} catch {
    $exitCode = 1
    Write-Host ""
    Write-Host "FAILED: $_" -ForegroundColor Red
} finally {
    Write-Host ""
    Read-Host "按 Enter 键关闭窗口"
    exit $exitCode
}
