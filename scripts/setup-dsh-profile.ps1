# setup-dsh-profile.ps1 — 幂等部署 YZJ Bridge 的 DSH JSON-RPC profile（含 resume/cwd 插件）。
#
# 创建/修复 ~/.dsh/profiles/jsonrpc（package.json + cordis.patch.yml + pnpm-workspace.yaml），
# 并把 plugin/dsh-jsonrpc-resume 复制到 flat fallback node_modules/@bridge/。
# 已存在时跳过或修复缺失/不一致文件；可重复执行。
#
# 用法：
#   powershell -ExecutionPolicy Bypass -File scripts/setup-dsh-profile.ps1
#   # 自定义 DSH 家目录：
#   powershell -ExecutionPolicy Bypass -File scripts/setup-dsh-profile.ps1 -DSHHome "D:\dsh-home"
#
# 前提：DSH rc.8 的 @deepseek-ai/dsh-base / dsh-sdk-jsonrpc-server / dsh-sdk-protocol 等包
# 已装入 <DSHHome>/profiles/node_modules（flat fallback 或 pnpm 链接），本脚本不负责装包。

param(
  [string]$DSHHome = "",
  [string]$PluginSource = ""
)

$ErrorActionPreference = "Stop"

if ($DSHHome -eq "") {
  $DSHHome = Join-Path $HOME ".dsh"
}
$profileDir = Join-Path $DSHHome "profiles\jsonrpc"
$bridgeDir  = Join-Path $DSHHome "profiles\node_modules\@bridge"
if ($PluginSource -eq "") {
  $PluginSource = Join-Path $PSScriptRoot "..\plugin\dsh-jsonrpc-resume"
}

Write-Host "[dsh] DSH_HOME = $DSHHome"

function Ensure-File([string]$Path, [string]$Content) {
  $dir = Split-Path $Path -Parent
  if (Test-Path $Path) {
    $existing = (Get-Content $Path -Raw).Trim()
    if ($existing -ne $Content.Trim()) {
      Write-Host "[fix]  $Path （内容与期望不一致，覆盖）"
      Set-Content -Path $Path -Value $Content -Encoding UTF8 -NoNewline
    } else {
      Write-Host "[ok]  $Path 已存在且一致"
    }
  } else {
    New-Item -ItemType Directory -Force -Path $dir | Out-Null
    Set-Content -Path $Path -Value $Content -Encoding UTF8 -NoNewline
    Write-Host "[new] $Path"
  }
}

# --- profile 目录与三个配置文件 ------------------------------------------------
New-Item -ItemType Directory -Force -Path $profileDir | Out-Null

$packageJson = @'
{
  "name": "dsh-profile-jsonrpc",
  "private": true,
  "dependencies": {},
  "dsh": {
    "profile": {
      "bundles": [
        "@deepseek-ai/dsh-base"
      ]
    }
  }
}
'@
Ensure-File (Join-Path $profileDir "package.json") $packageJson

# persona 注入 {{cwd}}/{{model}}，stdout 只承载 JSON-RPC 帧（不挂 stdout logger），
# 关闭 hmr，并把 sdk-jsonrpc-server 行指向桥自持插件（官方支持 resume 后改回官方包名）。
$cordisPatch = @'
# jsonrpc profile: headless stdio JSON-RPC server over dsh-base.
# stdout is reserved for JSON-RPC frames (no stdout logger here).
- id: system-prompt
  config:
    persona: >-
      You are a coding agent powered by the {{model}} model. Your working
      directory is {{cwd}}. Answer concisely and factually.
- id: hmr
  disabled: true
- insert:
    - id: sdk-jsonrpc-server
      name: '@bridge/dsh-jsonrpc-resume'
'@
Ensure-File (Join-Path $profileDir "cordis.patch.yml") $cordisPatch

$workspaceYaml = @'
packages:
  - .

nodeLinker: hoisted
autoInstallPeers: false
'@
Ensure-File (Join-Path $profileDir "pnpm-workspace.yaml") $workspaceYaml

# --- 插件包落位 flat fallback ---------------------------------------------------
$pluginPkg = Join-Path $PluginSource "package.json"
if (-not (Test-Path $pluginPkg)) {
  Write-Host "[err] 插件源不存在: $PluginSource （期望含 package.json + lib/index.js）" -ForegroundColor Red
  exit 1
}
$dest = Join-Path $bridgeDir "dsh-jsonrpc-resume"
Copy-Item -Path $PluginSource -Destination $dest -Recurse -Force
Write-Host "[ok]  插件已复制: $PluginSource -> $dest"

# --- 依赖自检（仅提示，不装包） --------------------------------------------------
$baseCheck = Join-Path $DSHHome "profiles\node_modules\@deepseek-ai\dsh-base"
if (-not (Test-Path $baseCheck)) {
  Write-Host "[warn] 未找到 $baseCheck — 请先安装 DSH rc.8 的 dsh-base 包（见 plugin/dsh-jsonrpc-resume/README.md）" -ForegroundColor Yellow
}

Write-Host "[done] profile 就绪: $profileDir"
