# Build Go bridge + Tauri GUI Windows installer (NSIS) and portable folder.
param(
    [switch]$SkipPause
)
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

    Write-Host "==> Prepare Tauri sidecar"
    & (Join-Path $Root "scripts\prepare-bundle.ps1")
    Assert-LastExit "prepare-bundle"

    Write-Host "==> Build Tauri GUI (NSIS installer)"
    # Prefer GNU target if MSVC link.exe is shadowed (e.g. coreutils)
    rustup default stable-x86_64-pc-windows-gnu 2>$null | Out-Null
    $gnuGcc = Join-Path $env:USERPROFILE ".rustup\toolchains\stable-x86_64-pc-windows-gnu\lib\rustlib\x86_64-pc-windows-gnu\bin\self-contained\x86_64-w64-mingw32-gcc.exe"
    if (Test-Path $gnuGcc) {
        $env:CARGO_TARGET_X86_64_PC_WINDOWS_GNU_LINKER = $gnuGcc
    }
    Push-Location gui
    try {
        if (-not (Test-Path node_modules)) { npm install; Assert-LastExit "npm install" }
        npm run tauri build
        Assert-LastExit "tauri build"
    } finally {
        Pop-Location
    }

    Write-Host "==> Collect dist artifacts"
    & (Join-Path $Root "scripts\package-windows.ps1")
    Assert-LastExit "package-windows"

    Write-Host "Done." -ForegroundColor Green
} catch {
    $exitCode = 1
    Write-Host ""
    Write-Host "FAILED: $_" -ForegroundColor Red
} finally {
    Write-Host ""
    $shouldPause = -not $SkipPause -and -not $env:GITHUB_ACTIONS -and [Environment]::UserInteractive
    if ($shouldPause) {
        Read-Host "按 Enter 键关闭窗口"
    }
    exit $exitCode
}
