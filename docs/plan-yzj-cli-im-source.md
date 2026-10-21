# 计划：入站改为 yzj-cli 群聊轮询 + 图片接收（v2，含 Phase 0 实测）

> 分支：`feat/yzj-cli-im-source`
> 状态：**方案已按你的 9 条反馈定稿，进入实现前最后确认**
> Phase 0 已完成：登录验证、消息 JSON 字段实测、图片下载验证（样例与结论见 §0.2 / 附录 A）

---

## 0. 定稿决策与实测结论

### 0.1 已确认决策（来自你的反馈）

| # | 决策 | 说明 |
|---|---|---|
| 1 | **只轮询配置了机器人的群** | 不会后台扫全部会话；`im group recent` 只在 GUI「选择群」/配置解析时用 |
| 2 | 群支持**按群名配置** | GUI 按群名搜索/选择，保存时解析为 `groupId` 并同时存群名用于展示；运行期只认真实 `groupId` |
| 3 | yzj-cli **运行时检测 + 自动安装** | 不把二进制打进安装包；缺失时按固定版本下载官方 `.pak` 并校验 SHA256 |
| 4 | 迁移顺序：**先 fairy** | fairy 切 `yzj_cli` 观察后再迁 `bsame_city_log` / `border_bot` |
| 5 | 纯图片消息**可以触发** | 默认按「引用机器人消息发图」触发（Q6）；另提供 `image_trigger: any` 开关 |
| 6 | 入站只处理 @机器人 / 引用机器人；出站仍用机器人 webhook | 与现网回复身份一致 |
| 7 | 轮询间隔 **5s**（±20% 抖动） | 仅对已配置群、串行调用；带频控退避 |
| 8 | 图片：单张 ≤20MB、保留 7 天、总量 500MB 上限清理 | 下载到 bot workspace 的 `incoming_media/` |
| 9 | 迁移期保留 `websocket` 回退，不做删代码式重构 | 每 bot 改配置即可切回 |

### 0.2 Phase 0 实测结论（本机已登录，只读调用）

**登录**：`yzj-cli whoami` 成功，身份 `user`，`tokenStatus=valid`，`expiresAt=2026-10-21T15:43:01+08:00`。

**`im message list` 真实 schema**（4 个群共 160 条样本，已脱敏存档到 `.research/probe/` 作为解析 fixture）：

```jsonc
// 外层：{"success":true,"identity":"user","data":{count,groupId,list,more}}
{
  "content": "[图片]",              // 显示文本；图片消息固定占位；@ 文本含 "@名字"
  "extFields": null,                // 可选：quickExpr（快捷回复）、replyCount
  "fromOpenId": "BOT-6a953c…",      // 用户 openId 或 BOT-<id>（机器人）
  "msgId": "6ad8389183…" ,          // 24hex 或 BOT-…
  "msgType": "text|richText|file|other",
  "sendTime": "2026-10-21 11:59:13.446",
  "param": {
    "desc": [                       // 富文本分段：@ 与内嵌图片
      {"type":"at","data":"<openId>|all","start":0,"length":4},
      {"type":"image","data":"<fileId>","w":720,"h":1005}
    ],
    "replyMsgId":"BOT-…",           // 引用消息（有引用时出现）
    "replyOpenId":"BOT-…",          // 被引用消息发送者（机器人）
    "replyPersonName":"机器人名",     // 被引用消息发送者姓名
    "replyRootMsgId":"…","replySummary":"…","isReference":true,
    // msgType=file 时：
    "file_id":"6ad8389183…","ext":"jpg","name":"图片.jpg","size":"108992",
    "ftype":1,"picWidth":720,"picHeight":1005,"isEncrypted":false,
    // 机器人消息：{"formatType":"markdown"}；撤回通知：{"sysType":"withdrawMsg"}
  }
}
```

实测要点：

1. **图片两种形态**：`msgType=file + param.ftype=1`（单图，`param.file_id`）；`msgType=richText + param.desc[type=image].data`（可多图）。`ext` 为 png/jpg 等，`size` 可能是字符串。
2. **@机器人可精确识别**：`param.desc` 中 `type=at` 且 `data` 以 `BOT-` 开头的段（实测样本 `@游泳管家` 即 `data=BOT-6ad82e8d…`）；`@all` 为 `data="all"`。
3. **引用机器人可精确识别**：`param.replyOpenId` 以 `BOT-` 开头、`param.replyPersonName` 为机器人名（实测回复告警机器人样本）。
4. **机器人消息**：`fromOpenId` 与 `msgId` 均为 `BOT-` 前缀，`param.formatType=markdown`；不同机器人 `BOT-<id>` 不同。
5. **图片下载实测通过**：`yzj-cli file download --id <file_id> --output <path>` 成功（23051 bytes，PNG magic `89504e47…` 校验通过）。
6. **消息里没有发送人姓名**（只有 openId）；姓名需 `contact user get --open-id` 解析并缓存（非必需，白名单/@ 回复都用 openId）。
7. **同群多机器人**：`BOT-` 前缀只能说明“是某个机器人”，要区分是不是“我们这只”，用 `replyPersonName` 匹配机器人名/别名，或配置/学习 `yzj_bot_open_id`。

**群名映射（实测 `im group search`）**：

| 配置里的 group 标签 | 真实群名 | groupId |
|---|---|---|
| `workAssistant`（fairy） | **Fairy（个人机器人会话，groupType=3）** | `BOT-67bfd544e4b0cc79e38a4271-BOT-6a7294c1e4b0a214555b2935` |
| `同城线上问题排查群` | 同城线上问题排查群 | `6a2b63f5e4b0718dc04be1b8` |
| `寄件助手群` | 寄件助手 | `6a850d9ae4b0e54b216106a7` |
| `同城告警群` | 同城告警群 | `6a9535cce4b0a737044473d7` |

> fairy 是通过 `https://yunzhijia.com/im/personalRobotCreate` 创建的个人机器人，IM 里表现为 groupType=3 的助手会话；消息直接从用户发给机器人，无需 @，因此用 `yzj_trigger: direct`（全部用户消息触发，机器人自身 `BOT-` 消息自动跳过）。

### 0.3 登录生命周期（回答你的问题）

- **电脑重启不需要重新登录**：凭据持久化在用户目录/注册表（本机实测 `HKCU\Software\YzjCli\keychain\yzj-cli`，`config.json` 只有 app 配置），重启后 `whoami` 直接可用。
- **access token 会过期但会自动刷新**：`whoami` 显示当前 access token 到期时间（今天 15:43），CLI 用 refresh token 自动续期，桥不接触 token。
- **什么时候需要重新登录**：refresh token 过期/被吊销、`yzj-cli auth logout`、改密码/企业策略变更时，CLI 返回 `exit 3 + credentials_missing/session_expired`。桥会把通道标记为「需重新登录」并停止轮询，不自动反复登录（登录需要你本人浏览器授权）。
- 我们会在 GUI/状态接口显示 `whoami` 身份与「需重新登录」提示；是否要显示 access token 到期时间可加（不显示 token 本身）。

---

## 1. 目标架构

```text
云之家（你本人账号可见的、已配置的群）
        │  yzj-cli im message list（5s 轮询，只读，仅配置群）
        ▼
bridge/internal/yzjcli
  GroupWatcher（profile+groupId，仅配置群）
   · newest 20 + 向前补齐 / 持久游标 / 频控退避 / 启停
   · 按 bot 匹配 @ / 引用 / 白名单
        │  Normalized{Source:yzj_cli, GroupID, MsgID, OpenID, Content, Attachments}
        ▼
inbound.Dispatcher（现有：去重 / 白名单 / 命令 / 排队）
        ▼
runJob：图片下载到 workspace → RunOpts.Attachments
        ▼
Orchestrator → Backend（cursor_cli / claude_code / openai / dsh）
        │  prompt 追加「图片本地路径 + 读图指令」
        ▼
yzjout.SendText（机器人 webhook，不变）
```

旧路径 `websocket|webhook|both` 原样保留；`inbound_mode: yzj_cli` 按通道独立启用，可逐 bot 迁移/回退。

---

## 2. 详细设计

### 2.1 watcher 范围（回答「只轮询配置群」）

- **只有配置里出现的群才创建 watcher**：配置来源 = `bots[].yzj_group_id`（单通道）或 `channels[].yzj_group_id`（多通道）。未配置群零调用、零日志。
- 群名只是配置/展示辅助：
  - GUI「选择群」调用 `im group recent --limit 20` 或 `im group search --keyword <群名>`，选中后保存 `{yzj_group_id, yzj_group_name}`；
  - 也允许手工填群名 → 保存时解析为唯一 groupId（0 个或多个匹配则拒绝并提示）；
  - 运行期只用 groupId，群名仅用于 UI 展示/日志。
- 同一 groupId 可被多个 bot 配置（例如 `同城告警群` 可同时挂 `bsame_city_log` 和其它 bot），每个 bot 按自己的 @/引用/白名单过滤；**每个群只发一次 list 调用**，结果分发给该群所有 bot。

### 2.2 新包 `bridge/internal/yzjcli`

| 文件 | 职责 |
|---|---|
| `client.go` | 定位/校验二进制（含自动安装入口）；`exec.CommandContext` 直传 argv；stdout 信封 / stderr JSON / exit code 解析；`Runner` 接口供单测 |
| `commands.go` | `WhoAmI`、`GroupRecent`、`GroupSearch`、`MessageList`、`FileDownload` |
| `message.go` | 宽松解析：`msgId/fromOpenId/msgType/content/sendTime/param`；引用字段；`@` 段；抽 `fileId`（file/ftype=1、richText image desc、以及 content 中的 URL 形态兜底）；`Attachment{Kind,FileID,Name,Ext,Mime}` |
| `state.go` | 游标 `~/.yzj-bridge/yzj-poll-state.json`：`profile|groupId → {lastMsgId,lastSendTime,recentMsgIds[ring],botOpenIds,contactNames}`，tmp+rename 原子写 |
| `watcher.go` | 5s 轮询：首启 baseline、newest+old 补齐、按 msgId 去重、按 sendTime 升序 dispatch、exit4 退避、exit3 连续 N 次停用并回调、SetEnabled/Dispose 对齐 `ws.Client` |
| `install.go` | 检测顺序 + 自动下载 `.pak`（见 §2.9） |

### 2.3 触发规则（定稿）

| 消息 | 触发条件 |
|---|---|
| 机器人自己发的消息 | 永不触发（`fromOpenId` 以 `BOT-` 开头），防自问自答 |
| 文本 | 群聊：`param.desc` 有 `type=at & data=BOT-*`（精确），或退回 `content` 含 `@机器人名/别名`；`yzj_trigger: direct`（个人机器人/私聊）：全部用户消息 |
| 图片（`msgType=file, ftype=1`） | 群聊默认：引用机器人（`replyOpenId` 以 `BOT-` 开头且 `replyPersonName` ∈ 机器人名/别名）；`direct`：直接触发；`image_trigger: any` 本群图片都触发；`off` 关闭 |
| 富文本（含图 + 文字） | 文本中的 @ 命中即触发；未 @ 但引用机器人也触发；`direct`/`any` 直接触发 |
| 文件（非图片） | Phase 2 起：引用机器人时接收并按普通附件处理；默认不触发 |
| `other`（撤回/系统） | 忽略 |

机器人身份解析：
- 配置 `yzj_bot_names`（展示名/别名列表）/ 可选 `yzj_bot_open_id`；
- 自动学习：用户引用机器人时 `replyOpenId` 即机器人 openId；或发出的回复在轮询里以 `BOT-` 消息出现（内容匹配）→ 记录 `botOpenId`；
- 同群多机器人按 `replyPersonName`/`botOpenId` 区分，避免抢答。

### 2.4 轮询算法（5s）

```text
每 profile 一个串行 worker（防 token 刷新竞态）；每群独立 next-tick（±20% 抖动）
tick(group):
  1) 游标不存在 → baseline：newest 20 只记 lastMsgId（不回放）
  2) newest 20 → 从新到旧收集到 lastMsgId（通常 1 页）
  3) 未命中 → type=old + 最旧 msgId 继续，最多 3 页
  4) 新消息按 sendTime 升序 → 逐条匹配 bot 的触发规则 → dispatch
  5) 先持久化游标再 dispatch（崩溃最多丢一轮，不重复刷屏）
  6) 失败：exit 4 → 指数退避(5s→10s→20s，上限 60s)；exit 3 → 通道「需重新登录」并暂停
```

- 5s × N 群 ≈ N/5 次/秒调用；先只开 fairy 1 个群，观察 API 频控后按需调大间隔。
- `catchup_pages`、`poll_interval_sec`、`image_trigger` 均可按 bot 覆盖。

### 2.5 图片接收与模型注入

| 步骤 | 设计 |
|---|---|
| 判定 | `msgType=file & (ftype==1 或 ext ∈ image types)`；`richText` 的 `desc[type=image]` 可多图；`isEncrypted` 未知时仍尝试下载，失败降级 |
| 下载 | `yzj-cli file download --id <file_id> --output <abs path>`；路径 `{workspace}/incoming_media/{YYYYMMDD}/{safeMsgId}-{idx}.{ext}`；不用默认命名（避免自动改名） |
| 校验 | 显式 `--output`；大小 ≤20MB；magic bytes（PNG/JPEG/GIF/WebP/BMP）与 ext 校验；文件名/ID 只保留 `[A-Za-z0-9._-]`；路径必须落在 workspace 内 |
| 注入 | `bot.RunOpts.Attachments []Attachment`；统一生成 prompt 片段（见下），各后端在 system/末尾注入 |
| 清理 | `keep_days=7`、总量 ≤500MB；启动 + 每日一次清理；失败日志不含图片内容 |
| 降级 | 下载失败 → 正文追加「图片下载失败：<原因>」继续处理；超大/非图片 → 忽略附件并提示 |

附件 prompt 片段：

```text
用户随消息发送了 1 张图片（本地路径，可直接读取）：
- 图片 1: <workspace>\incoming_media\20261021\msgBOT-6ad-1.jpg (image/jpeg, 320KB)
如需理解图片内容，请使用你的读图/读文件能力打开；若当前模型无法读图，请明确说明并用文字追问。
```

后端矩阵：cursor_cli / claude_code / dsh 先走路径注入（Claude Code Read、DSH `read_image` 支持读图）；OpenAI 原生 `image_url` 多模态放 Phase 3（需网关支持 + 兼容压缩逻辑）。

### 2.6 配置模型

```yaml
defaults:
  yzj_cli:
    bin: ""                     # 空=自动探测
    version: "0.1.8"            # 自动安装固定版本
    profile: "default"
    config_dir: ""              # 空=~/.yzj-cli
    auto_install: true          # 缺失时自动下载安装
    timeout_sec: 20
    poll_interval_sec: 5        # ±20% 抖动
    catchup_pages: 3
    startup_mode: baseline      # baseline | replay
    trigger: mention            # mention | keyword | all
    trigger_keywords: []
    image_trigger: quote_bot    # quote_bot | any | off
    image:
      enabled: true
      max_mb: 20
      types: [png, jpg, jpeg, gif, webp, bmp]
      keep_days: 7
      total_mb: 500

bots:
  - id: fairy
    inbound_mode: yzj_cli
    yzj_group_id: "G-xxx"        # 运行期唯一真源；单通道
    yzj_group_name: "群名"        # 仅展示/配置辅助
    yzj_bot_names: ["Fairy"]    # 识别 @/引用 的机器人名/别名
    # yzj_bot_open_id: "BOT-xxx"  # 可选：精确识别，留空自动学习
    send_msg_url: "https://...yzjtoken=..."   # 出站仍必填
    channels:
      - group: workAssistant
        send_msg_url: "..."
        yzj_group_id: "G-aaa"
        yzj_group_name: "群名"
```

配置校验：`inbound_mode` 白名单加 `yzj_cli`；`yzj_group_id` 在该模式必填（channel 优先，bot 兜底）；`send_msg_url` 仍必填（出站）；`yzj_group_id` 不做全局唯一（同群多 bot 合法）。

### 2.7 Runtime / 控制 API / GUI

| 层 | 改动 |
|---|---|
| `runtime.go` | `Watchers map[string]*yzjcli.Watcher`（key=`profile|groupId`）；按 `ModeUsesPoll` 装配；启停复用现有 `enabled`（`wss_enabled.json` key=bot runtime id），一个群有任一 bot 启用即轮询，dispatch 时跳过停用 bot；`SnapshotStatus` 增加 `poll_enabled/last_poll_at/last_poll_error/last_msg_at` |
| `bot.go` | `Config` 加 `YzjGroupID/GroupName/YzjBotNames/YzjBotOpenID`；`RuntimeStatus` 加 poll 字段；新增 `ModeUsesPoll` |
| `controlapi` | `GET /v1/yzj/cli/status`（bin/version/profile/whoami/到期时间）、`GET /v1/yzj/cli/groups?keyword=`（选择群）、`POST /v1/yzj/cli/install`（手动触发安装，可选）；现有 `/v1/wss/*` 兼容 |
| `App.tsx` | AI 设置「yzj-cli」卡片（路径/版本/登录状态/检测安装）；机器人表单入站模式加「yzj-cli 轮询」→ 群选择器（搜索/最近）+ 机器人名/别名；状态徽标：轮询中 / 已停用 / 需重新登录 / 未安装 |
| `main.go` | `--no-start-wss` 语义扩展为「不自动启动任何入站源」，flag 保留 |

### 2.8 yzj-cli 检测与自动安装

1. 检测顺序：`yzj_cli.bin` 配置 → `PATH` → `%LOCALAPPDATA%\yzj-bridge\bin\yzj-cli-<ver>.exe` → npm 全局（`%APPDATA%\npm\yzj-cli.exe`）。
2. 校验：执行 `--version`，版本必须匹配 `yzj_cli.version`；不匹配则提示（不静默升级）。
3. 自动安装（`auto_install: true`）：`GET https://www.yunzhijia.com/mixed/yzj-cli/windows-x64/release/yzj-cli-<ver>.pak`（仅 HTTPS、官方域名白名单）→ 解压出 `update.json` + exe → 校验 `update.json.checksum` 与内置 SHA256（0.1.8 = `c6db6cc9…e098a`）→ 原子 rename 到受控目录 → `--version` 自检；失败重试 3 次（2s/4s），失败后 UI 提示手动安装命令。
4. 禁止：运行 `yzj-cli update`、把二进制打包进安装包（README 版权声明与 package.json 许可证不一致，再分发有合规风险）、写 PATH。
5. Linux/macOS 先检测，不自动安装（当前产品主战场 Windows；后续补对应 `.pak`）。

---

## 3. 实施阶段

### Phase 0（已完成）
- 登录验证、消息 schema 实测、图片下载验证、群名映射（见 §0.2）。

### Phase 0b（剩余探针，需要你配合一次）
- 你在测试群给机器人发一条「**引用机器人消息 + 图片**」，我确认图片消息是否携带 `replyOpenId/replyPersonName`（决定 `image_trigger: quote_bot` 是否 100% 可靠；若不带，则退回“本群图片触发”或“最近机器人对话窗口”兜底）。
- 确认 fairy 的群（候选见 §0.2 表格）。

### Phase 1：轮询入站骨架（不含图片）—— **后端已完成（2026-10-21）**，GUI/真实迁移待办
- 已完成：`bridge/internal/yzjcli`（client/message/state/watcher/install）、config 校验、runtime 装配与启停、控制 API（status/groups）、单测；`go vet`/`go test ./...` 全绿。
- 待办：GUI 表单与群选择器；fairy 配置切换与实测。
- 验收：@ 机器人正常回复；重启不重放；停用通道即停轮询；`go vet/test` 全绿。

### Phase 2：图片接收（后端已完成，GUI/视觉原生块待办）
- 已完成：runJob 前用 `yzj-cli file download` 下载到 `workspace/incoming_media/YYYYMMDD/`；校验大小/类型/PNG 魔数；本地路径写入 prompt（所有后端通用）；失败/超限在 prompt 说明；按 `keep_days` 启动清理；单测覆盖。
- 待办：GUI 图片开关；OpenAI 原生 vision content block、DSH image block（当前先走“路径 + 模型自行读图”）。
- 验收：@ + 图片、引用机器人 + 图片、个人机器人直接发图 均能被模型读取；失败有降级提示。

### Phase 3：体验与边界
- 机器人身份自动学习与多机器人区分；OpenAI 原生 vision / DSH image block 评估；
- 自动安装 GUI 引导、登录过期提示、安装包不捆绑的文档；
- README / config.default.yaml / changelog / bridge-runtime / chat-test 同步。

---

## 4. 测试计划

- 单元：CLI 信封/错误码/超时解析；消息解析 fixture（文本/@/图片/多图/引用/撤回/未知 msgType）；游标与 baseline；5s 调度抖动；触发规则；图片校验与清理；配置校验。
- 集成：假 CLI（Go test helper）跑 Runtime，验证「只轮询配置群」「重启不重放」「同群多 bot 不抢答」。
- 手工 E2E：@ 文本；@ + 图片；引用机器人 + 图片；引用机器人 + 纯图片；非 @ 闲聊不回复；`auth logout` 后状态正确且不刷屏；重启后不重复；停用/启用。
- 回归：`gofmt/vet/test`、`npm run build`、现有 41 条 GUI e2e、WSS/webhook 模式冒烟。

---

## 5. 风险与对策

| 风险 | 对策 |
|---|---|
| 5s 轮询触发频控 | 只轮询配置群；per-profile 串行；exit4 指数退避；先 fidelity 单群观察，再按需调大 |
| 图片消息引用字段未实测（Phase 0b） | 若图片引用不携带 reply 字段：用「本群最近一次机器人对话后 N 秒内的图片」兜底，或显式 `image_trigger: any` |
| 同群多机器人抢答/误触发 | `replyPersonName` + `botOpenId` 双重判定；同一消息只交给最先匹配的 bot |
| 自动安装供应链 | 固定版本 + 官方 HTTPS CDN + SHA256 双校验 + 原子落盘；失败回退手动安装 |
| token 刷新并发 | 单 profile 单 worker；CLI 调用串行 |
| 凭据安全（注册表存储） | 桥不读取/不记录凭据；日志不采集 `--debug`；状态接口只显示 identity/到期时间 |
| 纯图片无 @ 的触发可靠性 | 以引用机器人为默认触发；`any` 作为显式开关 |
| 消息字段漂移 | fixture 回归 + 宽松解析 + 未知字段忽略 |

---

## 6. 剩余待确认（不阻塞 Phase 1 开工）

1. ~~fairy 的群~~ **已确认**：个人机器人会话（groupId 见 §0.2），使用 `yzj_trigger: direct`。
2. **图片引用探针**：fairy 是个人机器人会话，`direct` 模式下图片无需引用即可触发；群聊场景的引用图片字段仍建议补一次实测。
3. 机器人展示名：`yzj_bot_names` 里填的别名是否就是群里 @ 时显示的名字（如 `Fairy`、`同城日志查询`）。

---

## 7. 需要你提供/操作

| 项 | 谁做 | 说明 |
|---|---|---|
| 确认 fairy 群 | 你 | 从 §0.2 候选或告诉我群名 |
| 一条「引用机器人 + 图片」测试 | 你 | 测试群即可；我据此定 `quote_bot` 判定 |
| 机器人 @ 显示名 | 你 | 用于 `yzj_bot_names`；也可先按机器人配置名 |
| 其余（间隔/图片上限/迁移顺序/回退） | 已确认 | 见 §0.1 |

---

## 8. 工作量

| 阶段 | 规模 |
|---|---|
| Phase 0b 探针 | 0.5 天（依赖你测试） |
| Phase 1 骨架 | 2–3 天 |
| Phase 2 图片 | 2 天 |
| Phase 3 体验/文档 | 1–2 天 |
| 合计 | 约 6–8 个工作日 |

---

## 9. 回滚

每个 bot 的 `inbound_mode` 是唯一开关：改回 `websocket` 即恢复现网行为；新增代码在新包/新字段，旧路径语义不变；游标/图片目录独立可删；无需发版回退。
