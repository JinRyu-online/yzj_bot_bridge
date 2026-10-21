# 计划：入站改为 yzj-cli 群聊轮询 + 图片接收

> 分支：`feat/yzj-cli-im-source`（已创建）
> 状态：**待确认**（Phase 0 需要一个登录动作；方案末尾列出需要你提供的配置）
> 调研输入：现有 Go 桥代码（file:line 见正文）、`@yunzhijia/cli@0.1.8` 实测与包内文档、本机运行态证据（`~/.yzj-bridge` 日志/会话、控制 API）。

---

## 0. 目标与已确认决策

### 0.1 目标

1. 群聊**入站**不再依赖云之家机器人 WSS：改为用 `yzj-cli` 轮询你本人可见的群消息（可看到机器人看不到的普通消息），并支持图片。
2. **出站**保持现状：仍用机器人 `send_msg_url`（`yzjout.SendText`）以机器人身份回发。
3. 只处理「@ 机器人 / 回复机器人」的消息（默认），避免把群聊全部内容塞给模型。
4. 图片：下载到 bot workspace，prompt 注入本地路径；后端支持视觉/读图时尽量真看图，不支持时退化为路径 + 文字说明。

### 0.2 调研结论（决定方案形状）

| # | 结论 | 证据 |
|---|---|---|
| 1 | 现有实现**完全不读本地云之家 app 数据**（Go/GUI/历史 pyc/git 全量均无引用） | 全仓 grep + `%APPDATA%\云之家` 无引用；`workspace/incoming_media`、`workspace/inbox` 仅 `.gitignore` 占位，从未实现 |
| 2 | 当前图片消息**必丢**：`Normalize` 要求 `content` 非空，纯图片消息 `ok=false`，随后被 `ClassifyWS` 归为 control 静默丢弃 | `bridge/internal/inbound/inbound.go:82-111`、`:119-144`；运行态日志 0 条图片痕迹 |
| 3 | 机器人 WSS/Webhook 只能收到 `@机器人` 的文本；群内普通消息/图片拿不到 | `bridge/internal/ws/client.go:174-181`、`bridge/internal/webhook/server.go:98-112` |
| 4 | `yzj-cli` **没有实时/监听能力**，只能轮询；`im message list` 无 since/时间过滤，`--limit` 范围 1–20 | 实测命令树 + 二进制端点/字符串 |
| 5 | `yzj-cli` 输出对 Go 友好：成功 stdout 单行 JSON `{success,identity,data}`；失败 stderr 单行 JSON；exit `0/1/2/3/4/5/10` | 实测；`exit 3`=`credentials_missing` |
| 6 | 登录：浏览器 `auth login` 或 `--device` 设备码；个人授权即可，不需要管理员；配置 `~/.yzj-cli/config.json`，`YZJ_CLI_CONFIG_DIR` 可覆盖；Windows 凭据实测在 `HKCU\Software\YzjCli\keychain`（疑似 base64，非加密） | 实测 + 二进制字符串 |
| 7 | 图片/文件经 `fileId` → `file download --id <FILE_ID> --output <PATH>` 获取；默认不覆盖（自动改名），必须显式 `--output` | 包内 reference + 二进制字符串 |
| 8 | `im message list` 的**消息字段上游透传、CLI 未文档化**；图片消息的 `msgType`/`fileId` 形态必须登录后实测 | `internal` 无消息结构体；reference 只写「msgId、内容、发送人」 |
| 9 | 入站链路唯一入口是 `Normalized` + `Dispatcher.Handle`，新增轮询源 = 新增 producer，orchestrator/backends 基本不动 | `bridge/internal/inbound/inbound.go:167-228` |
| 10 | 三处云之家硬约束必须一起改：`send_msg_url` 必填、`inbound_mode` 白名单、出站固定 `msgtype:2` | `bridge/internal/config/config.go:202-206,242-246`；`yzjout.go:69-89` |

### 0.3 关键未知（Phase 0 登录后 30 分钟内可定）

1. `im message list` 单条消息真实字段：发送人 openId/姓名、`msgType` 数值、`content` 结构、`sendTime`、`fileId` 位置、多图形态、`replyMsgId`。
2. `msgType` 数值 ↔ 发送侧字符串（text/file/richText）映射；图片是 `file` 消息还是 `richText` 内嵌。
3. 机器人 webhook 回发响应里是否带**平台 msgId**（带则可做「回复机器人」触发与引用回复校验）。
4. DSH `contentBlocks` 是否支持 image block（决定 DSH 是否走原生视觉还是 `read_image` 工具路径）。
5. OpenAI 兼容网关（`aiapi.kuaidi100.com`）是否支持 `image_url` 多模态（决定 Phase 3 是否值得做原生 vision）。

---

## 1. 目标架构

```text
云之家群聊（你本人账号可见）
        │  yzj-cli im message list（轮询，Read-only）
        ▼
bridge/internal/yzjcli  ──  GroupWatcher（每 profile+groupID 一个）
   · 拉消息 / 解析 / 游标持久化 / 限流退避
   · 每群只拉一次，按 bot 匹配 @ 与白名单
        │  Normalized{Source: yzj_cli, GroupID, MsgID, OpenID, Content, Attachments}
        ▼
inbound.Dispatcher（现有：去重 / 白名单 / 命令 / 排队）
        ▼
runJob：图片附件下载到 workspace → RunOpts.Attachments
        ▼
Orchestrator → Backend（cursor_cli / claude_code / openai / dsh）
        │  prompt 末尾追加「图片本地路径 + 读图指令」
        ▼
yzjout.SendText（机器人 webhook，不变）
```

保留的旧路径：`inbound_mode: websocket | webhook | both` 原样可用；新路径是 `inbound_mode: yzj_cli`，按通道（bot 或 channel）独立开关，便于逐个迁移。

---

## 2. 代码设计

### 2.1 新包 `bridge/internal/yzjcli`

| 文件 | 职责 |
|---|---|
| `client.go` | 定位/校验 yzj-cli 二进制；`exec.CommandContext` 直传 argv（不经 shell）；解析 stdout 信封 / stderr JSON / exit code；`Runner` 接口便于单测注入 |
| `commands.go` | `WhoAmI`、`GroupRecent(limit,page)`、`MessageList(groupID,type,msgID,limit)`、`FileDownload(fileID,outPath)` |
| `message.go` | 宽松消息解析：从 `map[string]any` 取 `msgID/发送人/文本/msgType/sendTime/replyMsgID`，递归收集 `fileId`（兼容 `content` 是对象、JSON 字符串、URL 形态）；`Attachment{Kind, FileID, Name, Ext}` |
| `state.go` | 游标持久化 `~/.yzj-bridge/yzj-poll-state.json`：`{profile|groupID → {lastMsgID,lastSendTime, recentMsgIDs[ring 200], updatedAt}}`；tmp+rename 原子写 |
| `watcher.go` | 轮询循环：抖动间隔、`group recent` 预热、`newest`+`old` 补齐、按游标产新消息、per-profile 串行、exit 4 指数退避、exit 3 连续 N 次自动停用并回调（对齐 WSS 的 `AuthFailLimit` 语义）、`SetEnabled/Dispose` 生命周期对齐 `ws.Client` |
| `testdata/*.json` | Phase 0 抓取的真实消息样例（脱敏），作为解析回归 fixture |

错误模型：

```go
type CLIError struct {
    ExitCode int
    Type, Subtype, Message, Hint string // stderr JSON 解析
}
func IsAuthError(err) bool   // exit 3 / credentials_missing / session_expired / refresh_failed
func IsRateLimited(err) bool // exit 4
```

### 2.2 `Normalized` / Dispatcher 扩展

```go
// bridge/internal/inbound/inbound.go
type Attachment struct {
    Kind   string // image | file
    FileID string
    Name   string
    Ext    string
    Path   string // 下载后填充
}
type Normalized struct {
    // ...现有字段...
    GroupID     string
    MsgType     string
    Source      string // websocket|webhook|yzj_cli
    Attachments []Attachment
    ReplyToMsg  string
}
```

改动点：

1. `Normalize` 不再要求文本非空：`content=="" && len(attachments)>0` 时用占位 `[图片]`/`[文件]` 进入链路；仍无文本无附件才丢弃（并补一条 `drop` 日志，不再静默）。
2. 去重键从 `botID+msgiD` 改为 `source|botID|msgID`，避免 WSS 与轮询同时开时互相吞消息。
3. `Dispatcher` 增加 `prepareAttachments(b, n)`：在 `runJob` 调 `Orch.DispatchWithContext` 前下载图片（放在 job 线程内，不阻塞轮询）；`sessions.ResolveAgentWorkspace(cfg, openID, store)` 定位 workspace，落 `workspace/incoming_media/{YYYYMMDD}/{msgID}-{n}.{ext}`；大小/类型/数量校验；失败时在正文追加「图片下载失败：…」，不阻断回答。
4. `ReplyMeta` 增加 `GroupID/ReplyToMsgID`（出站仍是 webhook，暂不使用，但为 Phase 3 的「回复机器人」检测留字段）。

### 2.3 轮询算法（关键）

```text
每 profile 一个串行 worker（防止 yzj-cli token 刷新竞态）
每 tick：
  1) 对每个 watcher（profile+groupID）：
     a. 读游标；无游标 → baseline：拉 newest 20 条，只记 lastMsgID（不回放）
     b. 拉 newest 20 条；按 msgID 从新到旧收集，直到命中 lastMsgID
     c. 未命中且还有更早 → 用 type=old + 最旧 msgID 继续翻，最多 catchup_pages 页
     d. 新消息按 sendTime 升序；过滤异常/过旧（默认只处理 10 分钟内）
     e. 先推进并持久化游标，再逐条 dispatch（崩溃最多丢一轮，不重复刷屏）
  2) 抖动休眠 poll_interval_sec × (1 ± 0.2)
```

触发过滤（默认 `trigger: mention`）：沿用 `commands.MessageTargetsBot`（含 `@名字`/`@机器人名` 正则）+ 白名单；`all`/`keyword` 为可选项。**注意**：纯图片消息无法带 @，默认不触发（见 §6 开放问题与缓解方案）。

隐私：未触发消息只用于推进游标，不写正文日志、不进 memory。

### 2.4 图片接收与模型侧注入

| 后端 | Phase 1（路径注入） | Phase 2+（原生视觉） |
|---|---|---|
| cursor_cli | prompt 末尾追加附件块，要求用自身读文件能力查看 | 无 `--image` 参数，维持路径方案 |
| claude_code | 同上；Claude Code Read 工具支持读图 | 同上 |
| dsh | 同上；DSH 有 `read_image` 工具（需 vision 模型） | 验证 `contentBlocks` image block 后原生传图 |
| openai | 同上（agent 工具只能读文本，视觉暂不可用） | `oaMessage.Content` 改多模态 `content:[{type:"image_url",...}]`，需网关支持 + 兼容压缩/历史 |

附件块示例（统一 helper，避免四处复制）：

```text
用户随消息发送了 1 张图片（本地路径，可直接读取）：
- 图片 1: <workspace>\incoming_media\20261021\msg-abc-1.jpg (image/jpeg, 320KB)
如需理解图片内容，请使用你的读图/读文件能力打开该路径；若当前模型无法读图，请明确说明并以文字追问。
```

下载策略：仅对已触发的消息下载；单张 ≤ `image.max_mb`（默认 20）；扩展名与 magic bytes 双校验；跳过非图片文件（Phase 2 再支持文档）；`keep_days` 清理（启动 + 每日一次）。

### 2.5 配置模型

```yaml
defaults:
  yzj_cli:
    bin: ""                      # 空=自动探测 PATH → npm 全局 → ~/.yzj-bridge/bin
    profile: "default"
    config_dir: ""               # 空=~/.yzj-cli；可用 YZJ_CLI_CONFIG_DIR 隔离
    timeout_sec: 20
    poll_interval_sec: 15        # ±20% 抖动
    catchup_pages: 3             # 单次最多向前翻 3 页（每页 ≤20）
    startup_mode: baseline       # baseline=只建游标不回放；replay=回放最近 20 条
    trigger: mention             # mention | all | keyword
    trigger_keywords: []
    unknown_msg_log: false       # 未知 msgType 是否落日志（排障用）
    image:
      enabled: true
      max_mb: 20
      types: [png, jpg, jpeg, gif, webp, bmp]
      keep_days: 7

bots:
  - id: fairy
    inbound_mode: yzj_cli        # 新取值；websocket|webhook|both 原样保留
    yzj_group_id: "G-xxxxxxxx"   # 单通道；多通道写 channels[].yzj_group_id
    # 出站不变：机器人 send_msg_url 仍必填（除非将来支持 outbound:none）
    send_msg_url: "https://www.yunzhijia.com/gateway/robot/webhook/send?...yzjtoken=..."
    channels:
      - group: workAssistant
        send_msg_url: "..."
        yzj_group_id: "G-aaaaaaaa"
```

校验调整（`config.go`）：
- `inbound_mode` 白名单增加 `yzj_cli`；
- `yzj_group_id` 在 `yzj_cli` 模式下必填（channel 级优先，bot 级兜底）；
- `send_msg_url` 在 `yzj_cli` 模式下仍必填（出站需要），错误提示改为「出站发送地址」；
- `checkWebhookUnique` 仅对 ws/webhook/both 生效；`yzj_group_id` 不做唯一（同群可挂多个 bot，按 @ 分发）。

### 2.6 Runtime / 控制 API / GUI

| 层 | 改动 |
|---|---|
| `runtime.go` | `Pollers map[string]*yzjcli.Watcher`（key=`profile|groupID`）；`Load` 时按 `ModeUsesPoll` 装配；`StartAllWSS/StopAllWSS/SetChannelEnabled` 同步 watcher 启停（复用 `wss_enabled.json` 的 id→bool，GUI 开关语义不变）；`SnapshotStatus` 增加 `poll_enabled/last_poll_at/last_poll_error/last_msg_at` |
| `bot.go` | `Config` 增加 `YzjGroupID/YzjProfile/YzjTrigger`；`RuntimeStatus` 增加 poll 字段；新增 `ModeUsesPoll` |
| `controlapi` | 新增 `GET /v1/yzj/cli/status`（bin/version/profile/whoami）、`GET /v1/yzj/cli/groups`（最近群组，供 GUI 选择 groupId）、`POST /v1/yzj/cli/relogin-hint`（可选）；现有 `/v1/wss/*` 保持兼容 |
| `App.tsx` | AI 设置页新增「yzj-cli」卡片（bin/profile/检测登录/轮询间隔/图片开关）；机器人表单入站模式加「yzj-cli 轮询」，显示群 ID 输入 + 「从最近群组选择」；通道弹窗同理；状态徽标区分「轮询中/已停用/需登录」 |
| `main.go` | `--no-start-wss` 语义扩展为「不自动启动任何入站源」，保留 flag 兼容 |

GUI 不包含登录动作（Phase 1）：只显示 `whoami` 检测结果与 `yzj-cli auth login` 引导；设备码登录内嵌 GUI 放 Phase 3。

---

## 3. 分阶段实施

### Phase 0：登录 + 字段探针（阻塞项，需要你操作）

1. 你执行 `yzj-cli auth login`（浏览器，或 `yzj-cli auth login --device` 设备码）。
2. 我在有文本/图片/文件的群里各取样后运行**只读**调用，把真实 JSON 存成 fixture：
   - `yzj-cli im group recent --limit 20`
   - `yzj-cli im message list --group-id <G> --type newest --limit 20`
   - `yzj-cli file download --id <F> --output <tmp>`
3. 产出：字段表 + `msgType` 映射 + 多图形态；更新本计划 §0.3 / §2.1 解析器；确认「机器人 webhook 回发是否返回 msgId」。
4. 验收：一条含图消息能被解析出 `msgID/发送人/文本/fileId`，并能成功下载到指定路径。

### Phase 1：轮询入站骨架（不含图片）

- 新增 `yzjcli` 包（client/commands/message/state/watcher），消息解析先按 Phase 0 字段；
- `Normalized` 加 `GroupID/MsgType/Source`；`Normalize` 允许附件占位；去重键加 source；
- `config` 支持 `inbound_mode: yzj_cli` + `yzj_group_id`；`runtime` 装配 watcher + 启停；
- `controlapi` 加 CLI 状态/群组接口；GUI 加最小字段（模式 + 群 ID + 状态）；
- 单测：信封/错误码解析、游标、baseline 不回放、重启不重复、@ 过滤、未知字段忽略。
- 验收：把一个 bot（建议 `fairy`）切到 `yzj_cli`，群里 @ 它 → 机器人正常回复；重启 bridge 不重复回复；停用通道后不再轮询；`go vet/test` 全绿。

### Phase 2：图片接收 + 附件注入

- `Dispatcher.prepareAttachments` + 下载/校验/清理；
- `RunOpts.Attachments` + 统一 prompt 片段（cursor/claude/dsh/openai 注入点）；
- GUI：图片开关/大小/保留天数；日志脱敏；
- 单测：纯图片消息不丢、下载失败降级、路径越界防护、清理策略；
- 验收：@ + 图片 → 模型能读图并回答（Claude Code / Cursor / DSH 至少两个后端实测）；图片损坏/超大/无权限时有明确降级回复。

### Phase 3：体验与边界

- 「回复机器人」触发：抓机器人 webhook 响应 msgId（若可用）+ 轮询自己发言记录 → 维护 bot msgId 集合；
- 图片消息无 @ 的跟进窗口（`image_followup_window_sec`，默认 120s，仅在刚发生机器人对话的群）；
- OpenAI 原生 vision（网关支持时）/ DSH image block（协议支持时）；
- GUI 设备码登录引导；`yzj-cli` 版本缺失/过期提示；
- 文档：`README.md` 架构/前置条件/配置要点、`config.default.yaml`、`docs/changelog.md`（发版必须写 what's change）、`docs/bridge-runtime.md`、`docs/chat-test.md`。

---

## 4. 测试计划

**单元/集成（go test，无需真实凭据）**
- `yzjcli`：假 `Runner` 返回 stdout/stderr/exit 组合（成功、exit 3、exit 4、非 JSON、超时、空 data）；
- 解析器：文本 / 图片 / 富文本多图 / 文件 / 未知 msgType / content 为对象或 JSON 字符串；
- watcher：baseline、增量、翻页补齐、游标损坏回退、重启不重放、@ 过滤、白名单、禁用通道；
- `inbound`：附件占位不丢、去重键、下载失败降级；
- config：`yzj_cli` 模式校验、缺群 ID 报错、多通道展开；
- backends：附件 prompt 注入（现有 fake backend 测试模式可直接复用）。

**手工 E2E（需要你的账号）**
1. 文本 @ → 回复；2. @ + 图片 → 模型读图；3. 图片单独发（无 @）→ 默认不回复（符合预期）；4. 非 @ 闲聊 → 不回复、不写日志；5. 重启 → 不重复；6. 停用/启用通道 → 轮询停止/恢复；7. token 失效（`auth logout`）→ 出现「需要重新登录」且不刷屏。

**回归**
- `cd bridge; gofmt -l .; go vet ./...; go test ./...`
- `cd gui; npm run build`；必要时 `npm run test:e2e`（现有 41 例）
- 现有 WSS/Webhook 模式冒烟（不改动路径必须仍绿）。

---

## 5. 风险与对策

| 风险 | 等级 | 对策 |
|---|---|---|
| 消息字段未文档化、上游漂移 | 高 | Phase 0 固定 fixture；解析器宽松 + 未知字段忽略；`unknown_msg_log` 开关 |
| 轮询延迟/漏消息 | 中 | 15s 抖动轮询 + newest/old 补齐；游标持久化；重启不回放 |
| API 频控未知 | 中 | 同 profile 串行；每群每 tick 最多 1 次 list；`exit 4` 指数退避；多 bot 共享 group watcher |
| 游标/去重导致重复回复 | 高 | 持久化游标 + 内存 dedupe + 启动 baseline；测试覆盖重启场景 |
| 凭据安全（注册表疑似 base64） | 中 | 桥不读/不写凭据，只调 CLI；日志不采集 `config.json`/注册表/`--debug` 输出 |
| yzj-cli 分发与许可证（package.json MIT vs README © Kingdee） | 中 | **不把二进制打进安装包**；运行时探测 + 引导 `npm i -g @yunzhijia/cli`；锁定 0.1.8 + SHA256；禁用 `update` |
| 纯图片无法 @ → 不触发 | 中 | Phase 3 跟进窗口 + 引用机器人消息触发；文档明确「图片请配文字 @」 |
| 引用回复 msgId 与机器人接口不兼容 | 中 | 先验证；不兼容则回发不带 `replyMsgId`，只保留文本回复 |
| GUI 改动面大（App.tsx 4290 行） | 中 | 先最小字段（模式/群 ID/状态），其余放 Phase 3 |
| 机器人 webhook 仍需 `send_msg_url`（出站） | 低 | 文档写清「入站用 CLI、出站用机器人」；机器人需留在群内 |

---

## 6. 开放问题（需你决策，不阻塞 Phase 0）

1. **纯图片消息无 @ 是否要回复？** 建议 Phase 3 加「紧跟机器人对话 120s 内的图片视为追问」+「引用机器人消息发图」两种触发；否则默认不处理。
2. **轮询间隔**：默认 15s（每群每 tick 1 次调用）。若群多/接口频控严，可调 30s。
3. **图片上限/保留**：默认单张 20MB、保留 7 天、总量 500MB 上限清理。是否需要更久（例如排障留证）？
4. **分发策略**：安装包不捆绑 yzj-cli（许可证与版本锁定），改为「检测不到时引导安装」。是否接受？
5. **迁移顺序**：建议先迁 `fairy`（workAssistant），观察 1 天再迁 `bsame_city_log`/`border_bot`；是否同意？
6. **保留 WSS 回退**：迁移期每个 bot 保留 `websocket` 模式可随时切回，不做删代码式重构。

---

## 7. 需要你提供/操作

| 项 | 谁做 | 说明 |
|---|---|---|
| `yzj-cli auth login` | **你** | 浏览器登录（或设备码）；完成后告诉我一声，我跑 Phase 0 探针 |
| 目标群 + bot 映射 | 你确认 | 登录后我用 `im group recent` 列出群名/ID，你确认哪个群给哪个 bot（不要手抄 ID） |
| 轮询间隔 / 图片上限 / 保留天数 | 你确认或接受默认 | 默认见 §2.5 |
| 是否接受「安装包不捆绑 yzj-cli」 | 你确认 | 涉及分发与许可证 |
| 迁移顺序 | 你确认 | 默认先 `fairy` |

---

## 8. 预计工作量

| 阶段 | 内容 | 规模 |
|---|---|---|
| Phase 0 | 登录探针 + fixture + 字段结论 | 0.5 天（依赖你登录） |
| Phase 1 | yzjcli 包 + 配置/runtime/控制 API + GUI 最小字段 + 单测 | 2–3 天 |
| Phase 2 | 图片下载/注入/清理 + GUI 图片项 + 单测 + 后端实测 | 2 天 |
| Phase 3 | 触发增强 + 原生视觉评估 + 登录引导 + 文档 | 1–2 天 |
| 合计 | | 约 6–8 个工作日；约 15–20 个提交 |

---

## 9. 回滚

- 每个 bot 的 `inbound_mode` 是唯一开关：改回 `websocket` 即回到现网行为；
- 新增代码全部在新包/新字段，旧路径不改语义；`send_msg_url`/WSS/webhook 不动；
- 游标/图片目录独立，删除不影响 `sessions.json`/`chat_sessions.json`；
- 若 yzj-cli 侧字段/接口不可用，回滚仅为配置切换，无需发版回退。
