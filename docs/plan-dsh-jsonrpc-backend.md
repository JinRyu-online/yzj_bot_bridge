# 计划：YZJ Bridge 接入 DSH 后端（JSON-RPC 进程池 + resume）

- 分支：`feat/dsh-cli-backend`（Step 1 commit `5c6b41f` 已完成）
- 依据：`docs/dsh-cli-backend-feasibility.md` 第 11-14 节（官方 JSON-RPC 通道实测、resume 插件验证、池模式评估）
- 用户决策：**池模式单点可接受**；要求 resume（跨进程会话恢复）+ 省内存（无常驻每 bot 进程）+ 暂活 5 分钟回收
- 审批状态：Step 4 subagent **有条件通过**（4 项必须修改，已全部纳入本修订版；关键 cwd 方案经源码实证确认可行）
- 实施状态：**全部完成**（A/B/C/D/E 含真实冒烟与审计修复均已落地，验证方式见文末「实施状态」节；验收标准 1/2/3 全部满足）

## 目标

在 YZJ Bridge 新增 `dsh` 后端：以**每模型一个共享 DSH 进程池**运行官方 `@deepseek-ai/dsh-sdk-jsonrpc-server`（rc.8）+ 桥自持 ~120 行 `session/resume`+`cwd` 插件，实现：

1. **多 bot 并发共享实例**：全部 bot 的会话在同一池进程内（每会话独立、事件按 sessionId 分流），内存 ≈ 模型数 × ~160MB，与 bot 总数无关。
2. **真 resume**：`sessionId = bot 稳定 id`（复用桥 store 会话机制）；进程重启/崩溃后 `session/resume` 冷载，跨进程记忆保持。
3. **冷热双路径**：会话在池中 → `session/prompt`（热，~1s）；不在 → resume 载入（冷，~3s）。
4. **TTL 暂活回收**：池进程 idle 超 `dsh_ttl_seconds`（默认 300s）→ shutdown 归还内存；`dsh_max_warm` 硬上限。
5. **上下文自动压缩**：官方 compaction-basic（dsh-base 默认 `auto:true`），桥零配置。
6. **per-session 工作目录**：每个 bot 会话的 `header.cwd = bot.workspace`（DSH 工具链以 session.header.cwd 为权威，源码实证：`dsh-sandbox-policy/lib/index.js:142` `resolveWorkspaceRoot(session?.header.cwd ?? this.workspaceRoot)`、`dsh-tool-bash/lib/index.js:178` `exec.agent?.session.header.cwd`）；池进程 cwd 为中性目录。

## 关键文件与改动

### A. DSH 侧部署物（新增，非业务代码）

| 文件 | 内容 |
|---|---|
| `plugin/dsh-jsonrpc-resume/package.json` | `@bridge/dsh-jsonrpc-resume` 包（从 `%TEMP%\dsh-jsonrpc-resume-plugin\` 落位并扩展） |
| `plugin/dsh-jsonrpc-resume/lib/index.js` | 子类化官方 server：`session/resume {sessionId, contentBlocks?}`（已实测）+ **`session/prompt` 扩展可选 `cwd` 参数**（override `createSession(sessionId, cwd)`：`meta.cwd = cwd ?? this.cwd`；官方协议无 cwd 时回退进程级）；apply 忠实复制官方实现 |
| `plugin/dsh-jsonrpc-resume/README.md` | 安装：建 `~/.dsh/profiles/jsonrpc`（package.json bundles=[dsh-base] + cordis.patch.yml 行 `@bridge/dsh-jsonrpc-resume`）、包落位 `~/.dsh/profiles/node_modules/@bridge/`、DSH rc.8 要求、**cwd 扩展的依赖说明**（官方协议若无 cwd 参数，退役后新会话回退进程级 cwd）、官方协议加 resume 后的退役切换 |
| `scripts/setup-dsh-profile.ps1`（新增） | 一键：建 profile 目录、写 package.json/cordis.patch.yml/pnpm-workspace.yaml、复制插件包到 flat fallback；幂等 |

### B. 桥 Go 侧（核心）

**`bridge/internal/backends/dshpool.go`（新）** — 共享进程池：
- `dshPool` 包级单例：`map[poolKey]*dshProc`（mutex 保护）。
- **poolKey = `provider|model|profile|nodeBin|dshEntry(绝对路径)`**——stub 冒烟与 live 冒烟因入口不同天然隔离；提供 `resetPoolForTest()` 钩子。
- `dshProc`：
  - `cmd *exec.Cmd`：spawn `exec.Command(nodeBin, dshEntry, "--profile", profile)`（node 直调 bin.js，绕过 .cmd）+ `processutil.HideWindow` + `DSH_PERMISSION_MODE=danger-full-access`；**`cmd.Dir = 中性目录`**（`<dataDir>/dsh-pool`，桥启动时 MkdirAll；非任何 bot workspace）。
  - spawn 后握手 `initialize{cwd: 中性目录, provider, model}`（读回 serverInfo；超时 30s 失败视为 spawn 失败）。
  - **stdin 写锁**（JSON-RPC 帧不可交错）+ **id→pending 响应 map**（initialize/shutdown 等 request/response 匹配，带超时清理）。
  - **常驻 stdout reader goroutine**：NDJSON 逐行解析（Scanner buffer 8MB，参照 claude.go）；`session.event` 按 sessionId 分发；**无订阅者的事件直接丢弃（不阻塞）**；stdin EOF/进程 exit → 标记 dead、清 knownSessions、通知全部等待者。
  - **per-session in-flight 守卫**：reader 收到 `turn/start` 置 inflight、`turn/end` 清除；`knownSessions[sid]` + `inflight` 联合决定冷热/排队（见 dsh.go）。
  - `lastActive` 更新点：写出任何请求或收到任何该进程事件时。
- 回收 goroutine：周期扫描；**跳过 inflight 会话的进程**；idle 超 `ttl` → 发 `shutdown`（等待 2s）→ 兜底 kill：Windows `taskkill /PID <pid> /T /F`（进程树），其他平台 `cmd.Process.Kill()`；`maxWarm` 超出按 LRU（同样跳过 inflight）提前回收。

**`bridge/internal/backends/dsh.go`（新）** — `type DSH struct{ cfg bot.Config; store *sessions.Store }` 实现 `bot.Backend`：
- `CreateSession()`：UUID（复用 Claude 生成方式）。
- `ClearSession(sessionID)`：新 UUID + **从池中该进程的 knownSessions/订阅 evict 旧 sessionId**（服务端无删除方法，旧记忆靠 TTL 兜底）。
- `Run(prompt, opts)`（同步阻塞）：
  1. sessionID 解析：`sessions.ResolveSessionKey(cfg, opts.OperatorOpenID)` + store 取/存（复用 Claude L63-77 模式；`opts.SessionID` 为空时新建）。
  2. 模型/provider：`opts.Model` → `cfg.Model` → `dsh_model` → 默认 `deepseek-v4-flash`；provider 默认 `kuaidi100`（settings.yaml 已有凭据；可配 `dsh_provider`）。
  3. workspace：`os.MkdirAll(opts.Workspace)`；system prompt / skills / memory 拼接进 prompt（同 claude.go L80-92）。
  4. 池取/建进程（poolKey）→ 冷热分流：
     - `knownSessions[sid]` 且非 inflight → `session/prompt{sessionId, cwd: workspace, contentBlocks}`（热）；
     - `knownSessions[sid]` 但 **inflight（上次 Run 超时弃置的残留 turn）** → 先等残留 turn/end（丢弃事件，`dsh_timeout` 内）再发；
     - 未知 sid → `session/resume{sessionId, contentBlocks}`，收到 `session "X" not found` → 回退 `session/prompt{sessionId, cwd, contentBlocks}` 创建。
  5. 订阅该 sessionId 事件流：收集 `assistant/message`（`event.data.message.content` 的 `type:text` 块）直至 `turn/end`（reason completed/error/max-tokens）或超时（`dsh_timeout` 默认 600s，`opts.Context` 优先，`resultFromCtx` 语义）；**Run 结束必须 unsubscribe**（reader 无订阅者即丢弃）。
  6. store 持久化 sessionID/AgentCWD（复用 Claude L216-223 模式）→ `RunResult{Reply, Status, SessionID}`。
- 崩溃语义：进程 dead 时 Run 返回错误（下次消息自动 spawn + resume）；turn 超时只弃置该 Run（服务端无 cancel 协议，残留 turn 继续跑完并留在会话上下文——**文档写明"中断≠服务端取消"**），进程保留。

**`bridge/internal/backends/factory.go`**：`SupportedBackends()` 加 `"dsh"`；`Create` switch 加 `case "dsh", "dsh_jsonrpc": return NewDSH(cfg, store)`；`canonicalBackend` 加 `"dsh_jsonrpc"→"dsh"`。

**`bridge/internal/bot/bot.go`**：`Config` 加字段：
`NodeBin string (json:"node_bin"，node 可执行路径，默认 "node")`、`DSHEntry string (json:"dsh_entry"，DSH CLI 入口，可注入；默认解析：DSHHome 定位 dsh 包 bin.js，找不到用 PATH 的 dsh)`、`DSHProfile string (默认 "jsonrpc")`、`DSHProvider string (默认 "kuaidi100")`、`DSHModel string (默认 "deepseek-v4-flash"，可留空复用 Model)`、`DSHTimeout int (默认 600)`、`DSHTTLSeconds int (默认 300)`、`DSHMaxWarm int (默认 3)`、`DSHHome string (可选 DSH_HOME 覆盖，默认 ~/.dsh)`。

**`bridge/internal/config/config.go`**：
- `defaultMap()` 加上述默认值（同 cursor_timeout 风格）。
- **`mapToBotConfig` 补全部 `dsh_*`/`node_bin` 字段读取**；model switch 加 `case "dsh", "dsh_jsonrpc": model = firstNonEmpty(model, asString(m["dsh_model"]))`（当前 dsh 会落入 default 分支读 cursor_model——必须修）。

**`bridge/internal/backends/binresolve.go`（或 dsh.go 内）**：node 路径解析（复用 `resolveWindowsBin` 模式；裸名走 `exec.LookPath`）。

### C. 测试

- `bridge/internal/backends/testdata/dsh_stub.mjs`（新）：**离线 stub JSON-RPC server**——`initialize` 回 serverInfo；`session/prompt`（**断言 cwd 参数传入并回显**）/`session/resume`（已知 sid 恢复、未知 sid 回 `session "X" not found` 错误）/`session.event`（turn/start、assistant/message "pong"、turn/end completed）/`shutdown` 退出。不碰真实 DSH/LLM（与 smokestub 假 CLI 同模式）。
- `backend_smoke_test.go`：`smokeDSH(t, stub, ws)` 加入 `TestBackendSmokeAllEngines`（stub 门控，poolKey 含入口路径 → 与 live 隔离；`resetPoolForTest` 保证用例间干净）；用例覆盖：**首条创建（cwd 断言）→ 热路径 → 双会话并发交错（A/B 回复不串扰）→ resume 已知/未知两分支 → ClearSession evict**；`TestLiveBackendSmoke` 加 dsh 分支（`YZJ_SMOKE=1`，skip 条件：无 node/无 dsh/无 profile；clamp `dsh_timeout`）。
- `bridge/internal/backends/dsh_test.go`（新）：帧解析、assistant 文本提取、事件分发、冷/热/inflight 判定、poolKey 构成单测。

### D. GUI

- `gui/src/App.tsx`：
  - `BACKENDS`（L180）加 `"dsh"`；
  - AI 设置页（L2261+）加 DSH 配置区（node_bin / dsh_entry / dsh_profile / dsh_provider / dsh_model / dsh_timeout / dsh_ttl_seconds / dsh_max_warm / dsh_home），defaults 保存/回读（同 cliForm 模式）；
  - **`resolveDisplayedModel`（L269-284）加 dsh 分支**（当前 dsh 会显示成 cursor_model）。
- 模型列表：本次不做动态拉取（文本框输入），后续可加。**对照 `docs/gui-design.md`**（审批已核对 2/3/4/6 节无违规）。

### E. 文档

- `plugin/dsh-jsonrpc-resume/README.md`（部署/退役/cwd 依赖说明）；`docs/dsh-cli-backend-feasibility.md` 已含方案。

## 实现顺序

1. A：插件扩展（cwd 参数）+ 落位 + `scripts/setup-dsh-profile.ps1`（先改插件并验证 cwd 生效：双 workspace 双会话）
2. B：`dshpool.go` → `dsh.go` → factory/config（含 mapToBotConfig）
3. C：stub + 单测 + 冒烟接入
4. D：GUI（含 resolveDisplayedModel）
5. E：文档；本机真实冒烟（kuaidi100 key 可用）：首条（cwd 生效）→ 热路径 → 杀进程 resume → 双 bot 并发 → TTL 回收

## 验收标准

1. `go build ./...`、`go vet ./...`、`go test ./internal/backends/ -count=1`（含 stub 冒烟：冷/热/并发/resume 两分支/cwd 断言）全绿。
2. 真实环境（`YZJ_SMOKE=1`）：
   - 首条消息 → `status=ok`、回复含目标词；**工作目录 = bot workspace**（写文件验证落点）；
   - 同会话第二条（热路径）正常；
   - 杀掉池进程后下一条消息 resume 成功（记忆保持，如暗号召回）；
   - 两个不同 workspace 的 bot 并发提问 → 单进程双会话均完成、回复不串扰、**各自文件落在各自 workspace**；
   - idle 超 TTL 后进程退出、内存归还。
3. GUI 可配置 dsh 后端并保存、重启后回读；摘要模型显示正确。

## 风险与假设

- **池单点**（用户已接受）：进程 crash 影响池内所有 bot → 桥检测 exit 后下次消息自动 resume 恢复；可后续加 `isolation:true` 每 bot 独立进程（本次不做）。
- **DSH rc 演进**：钉 rc.8；官方协议若加 resume/cwd，仅需把 profile 行名改回官方包（README 记录切换步骤）。
- **真实 LLM 冒烟成本**：默认 stub 门控；live 仅 `YZJ_SMOKE=1`。
- **Windows 进程树清理**：`taskkill /T /F` 兜底（processutil 仅 HideWindow，本次在 dshpool 内实现，不动 processutil 公共 API）。
- **node/dsh 依赖**：默认 PATH 查找 + `DSHHome` 解析，可配；环境缺失时 Run 返回 `agent_missing`（同 claude 语义）。
- **每进程单模型**：模型变更 = 新池键 = 新进程（旧进程 TTL 回收）。
- **超时语义**：turn 超时弃置 ≠ 服务端取消；残留回复留在上下文（文档明示）。
- 不做：PTY/屏抓、官方协议自研替代、模型列表动态拉取、per-bot 隔离进程、DiscoverCLI 扩展（dsh 入口由 profile 固定、node 走 PATH，非可发现 CLI——审批确认）。

## 实施状态（追加记录；上方正文为权威设计，未改动）

| 步骤 | 状态 | 验证方式 / 结果 |
|---|---|---|
| A 插件+脚本 | ✅ 完成 | `plugin/dsh-jsonrpc-resume/`（package.json + lib/index.js 153 行 + README）与 `scripts/setup-dsh-profile.ps1`（幂等）落位；README 与脚本字段/默认值已逐项核对一致（schemastery 版本描述已对齐 package.json） |
| B 桥 Go 侧 | ✅ 完成 | `dshpool.go`/`dsh.go` 新增；`factory.go`（SupportedBackends/Create 加 dsh 分支）、`bot.go` Config 9 字段、`config.go` defaultMap/mapToBotConfig（model switch 加 dsh 分支读 `dsh_model`） |
| C 测试 | ✅ 完成 | `testdata/dsh_stub.mjs` 离线 stub；`dsh_test.go` 单测 + `backend_smoke_test.go` 接入（stub 门控、`resetPoolForTest` 隔离）；`go build`/`go vet`/`go test ./internal/backends/ -count=1` 全绿 |
| D GUI | ✅ 完成 | `gui/src/App.tsx`：BACKENDS 加 `"dsh"`；AI 设置页 DSH 配置区 9 字段（node_bin/dsh_entry/dsh_profile/dsh_provider/dsh_model/dsh_timeout/dsh_ttl_seconds/dsh_max_warm/dsh_home），保存（cliForm→defaults→saveConfig）与回读（refreshConfig→cliForm）两条路径完整；`resolveDisplayedModel` 加 dsh/dsh_jsonrpc 分支且 defaults 参数类型补 `dsh_model`；`npm run build`（tsc && vite build）通过 |
| E 文档 | ✅ 完成 | plugin README 部署/退役/cwd 依赖说明齐备；验收标准第 3 条（GUI 可配置 dsh 并保存回读、摘要模型显示正确）已满足，验收 1 满足，验收 2（真实 LLM：cwd 落点/热路径/resume/并发/TTL）待 live 冒烟全绿后确认 |
| E 真实冒烟 | ✅ 全绿（2026-08-21） | `bridge/internal/backends/dsh_live_test.go`（`YZJ_SMOKE=1` 门控）五场景实测 PASS（34s，真实 LLM）：a) 首条创建 cwd 生效（probe.txt 落 workspace）→ b) 同会话热路径 → c) 杀池进程 resume 暗号跨进程召回 → d) 双 bot 不同 workspace 并发回复不串扰、文件各落各的 → e) idle 超 TTL `dshReap()` 强制回收进程退出。期间修复：仅测试侧 TTL 计时竞态（真实 DSH 异步生成 session/title 会刷新 lastActive，改为轮询等连续 idle≥TTL 再 reap；非产品 bug） |
| 审计修复 | ✅ 已并入 | 对照审计（只读）发现并修复：P1① JSON-RPC 错误帧被 `call` 吞掉→改为 `callCtx` 统一把 `resp.Error` 转 Go error（消除 600s 挂起）+ resume 仅 "not found" 回退；P1② `cfg.SystemPrompt` 未注入→Run 前置拼接；P2③ workspace 归一绝对路径（官方 header 校验）；P2④ readLoop 超长帧（>8MB）楔死进程→`sc.Err()` 非 EOF 时先 kill；P2⑥ stub 冒烟无 node 时 skip。修复后 `go build`/`go vet`/`go test ./internal/backends/ -count=1` 与 live 冒烟回归全绿 |

遗留事项：GUI e2e（`gui/e2e/ui.spec.ts`）已补一条「AI 设置：DSH 配置区默认值与保存回读」用例并通过（mock defaults 已加 `dsh_*` 键）；「设置页分组、密钥小眼睛、OpenAI 模型」用例存在既有 flaky（`cursor-bin` 断言 "agent" 但打开设置页时 discover 自动填充绝对路径，与 DSH 无关）。
