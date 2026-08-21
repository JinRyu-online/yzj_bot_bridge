# @bridge/dsh-jsonrpc-resume

YZJ Bridge 自持的 **DSH SDK JSON-RPC server 薄扩展**（~150 行，其余全部复用官方实现）。
在官方协议 `initialize / session/prompt / shutdown` 之外增加两个能力：

1. **`session/resume { sessionId, contentBlocks? } -> { messageId? }`** —— 跨进程会话恢复
   （官方协议不暴露 `ctx.agents.resume()`，进程生命周期 == 会话生命周期；本插件子类化官方
   server 补上该方法，恢复的会话会注册进 server 的 session map，后续 `session/prompt` 命中同一活 agent）。
2. **`session/prompt` 可选 `cwd` 参数** —— per-session 工作目录
   （override `createSession(sessionId, cwd)`：`meta.cwd = cwd ?? this.cwd`；DSH 工具链以
   `session.header.cwd` 为权威工作目录，桥的共享进程池靠它让每个 bot 会话在自己 workspace 里干活；
   `meta.cwd` 随会话 header 持久化，跨进程 `session/resume` 自动从磁盘恢复，无需额外参数）。

## 依赖与版本要求

- **DSH rc.8 线**：peer 依赖 `@deepseek-ai/dsh-sdk-jsonrpc-server` / `dsh-sdk-protocol` /
  `dsh-llm` / `dsh-session` 均须 `^0.1.0-rc.8`（旧版 `0.0.1-rc.5` server 的 peer 与 rc.8 主干不匹配，
  不可用）；`schemastery` 走稳定线 `^3.18.1`（与 rc.8 主干兼容，声明见 package.json）。
- 行插件运行在 `dsh-base`（rc.8）之上；`dsh` 启动器 0.1.0-rc.7 的 closure 内即为 rc.8 包，混装可用。
- 运行环境：Node ≥ 20（实测 v24）。

## 安装步骤

推荐直接跑仓库脚本（幂等，可重复执行）：

```powershell
powershell -ExecutionPolicy Bypass -File scripts/setup-dsh-profile.ps1
```

脚本会：

1. 创建/修复 `~/.dsh/profiles/jsonrpc/`：
   - `package.json`：`dsh.profile.bundles: ["@deepseek-ai/dsh-base"]`
   - `cordis.patch.yml`：persona（含 `{{model}}`/`{{cwd}}`）+ `hmr: disabled` + insert 行
     `- id: sdk-jsonrpc-server / name: '@bridge/dsh-jsonrpc-resume'`
   - `pnpm-workspace.yaml`：`packages: [.]`、`nodeLinker: hoisted`、`autoInstallPeers: false`
2. 把本插件复制到 flat fallback：`~/.dsh/profiles/node_modules/@bridge/dsh-jsonrpc-resume/`。

若 DSH 家目录非默认，用 `-DSHHome <path>` 覆盖。脚本**不装包**——rc.8 的
`dsh-base` / `dsh-sdk-jsonrpc-server` / `dsh-sdk-protocol` / `dsh-llm` / `dsh-session` /
`schemastery` 等需先装入 `~/.dsh/profiles/node_modules/`（flat fallback 实测可行；也可用
`dsh plugin --profile jsonrpc add ...` 官方路径，未实测）。

### 手动安装（等价）

```powershell
$dst = "$env:USERPROFILE\.dsh\profiles\node_modules\@bridge\dsh-jsonrpc-resume"
Copy-Item plugin\dsh-jsonrpc-resume $dst -Recurse -Force
```

启动验证（stdout 只承载 JSON-RPC 帧，诊断走 stderr）：

```powershell
node <dsh-package>/lib/bin.js --profile jsonrpc
# 输入 {"jsonrpc":"2.0","id":1,"method":"initialize","params":{"cwd":"<dir>","provider":"kuaidi100","model":"deepseek-v4-flash"}}
```

## cwd 扩展的依赖说明（重要）

- `session/prompt` 的 `cwd` 仅在**会话创建时**生效（`meta.cwd` 烘焙进 header）。
- 会话已存在（热路径再次 prompt）时 `cwd` 被忽略——工作目录保持首次创建值。
- 官方协议若一直没有 `cwd` 参数：**本插件退役后，新会话的工作目录回退为进程级 cwd**
  （即池进程的启动目录）。届时桥应改用「每 bot 进程 cwd=workspace」或等待官方支持。
- 跨进程 `session/resume` **不需要** `cwd`：header 从磁盘恢复。

## 官方协议演进后的退役切换

当官方 `@deepseek-ai/dsh-sdk-jsonrpc-server` 原生支持 resume（与 cwd）后，零代码切换：

1. 编辑 `~/.dsh/profiles/jsonrpc/cordis.patch.yml`，把 insert 行的 name 改回官方包：

   ```yaml
   - insert:
       - id: sdk-jsonrpc-server
         name: '@deepseek-ai/dsh-sdk-jsonrpc-server'
   ```

2. 桥侧无需改动（协议方法名 `session/resume` 与 `session/prompt` 的 `cwd` 参数保持兼容即可；
   若不兼容，按当时官方协议微调 `bridge/internal/backends/dsh.go`）。
3. 删除本插件目录 `~/.dsh/profiles/node_modules/@bridge/dsh-jsonrpc-resume/`。

## 卸载

```powershell
# 1) 移除插件包
Remove-Item "$env:USERPROFILE\.dsh\profiles\node_modules\@bridge\dsh-jsonrpc-resume" -Recurse -Force
# 2) 从 cordis.patch.yml 删除 insert 块（或整行指向官方包）
# 3) 若不再需要整个 profile：
Remove-Item "$env:USERPROFILE\.dsh\profiles\jsonrpc" -Recurse -Force
```

## 协议速览（本插件视图）

| 方法 | 参数 | 返回 |
|---|---|---|
| `initialize` | `{ cwd, provider, model, maxTokens? }` | `{ serverInfo }`（进程级路由） |
| `session/prompt` | `{ sessionId, cwd?, contentBlocks }` | `{ messageId }`（未知 sessionId 惰性创建） |
| `session/resume` | `{ sessionId, contentBlocks? }` | `{ messageId? }`（跨进程恢复） |
| `shutdown` | `{}` | `{}`（flush 后 dispose 根运行时并 exit 0） |

通知：`session.event { sessionId, event }` / `session.status` / `subagent.started` / `subagent.finished`。
