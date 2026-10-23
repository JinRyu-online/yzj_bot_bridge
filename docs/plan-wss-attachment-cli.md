# 计划：WSS 入站图片/文件改由 yzj-cli 补拉（feat/wss-upgrade）

> 状态：设计稿（未动代码）　分支：`feat_wss_upgrade`　基线：`main@02e9c6f (v0.4.2)`
>
> 目标：WSS（以及复用同一解析的 Webhook）收到带图片/文件的消息时，**只把 WSS 当"触发信号 + 文本"**，附件统一走 `yzj-cli file download` 拉取到机器人工作目录，并把本地路径写进提示词。解决"WSS 拿不到图片/文件"的问题。

---

## 1. 问题定义

### 1.1 现象

| 入站方式 | 文本 | 图片 | 文件 | 说明 |
|---|---|---|---|---|
| `yzj_cli` 轮询 | ✅ | ✅（已实现） | ❌（`Attachments()` 只返回图片） | `im message list` 拿到 `msgType/param` 后 `file download` |
| `websocket`（WSS） | ✅ | ❌ | ❌ | 解析层根本没读 `msgType/param` |
| `webhook` | ✅ | ❌ | ❌ | 与 WSS 共用 `inbound.Normalize` |

WSS 侧的根因不是"平台不推二进制"，而是**桥的解析层丢弃了结构化字段**，且没有把 `file_id` 交给下载器。

### 1.2 代码定位（当前 main）

| 环节 | 位置 | 现状 |
|---|---|---|
| WSS 帧分发 | [`ws.Client.loop`](../bridge/internal/ws/client.go#L140-L183) | `inbound.ClassifyWS(raw)` → `Dispatcher.Handle` |
| 帧归一化 | [`inbound.Normalize`](../bridge/internal/inbound/inbound.go#L112-L141) | 只取 openId/msgId/content/robotName；**完全不读 `msgType`、`param`**；`Attachments` 永远为空 |
| 纯图片消息 | 同上 `L133-L135` | `content == ""` 时直接返回 `ok=false`，**整条消息被丢弃**（平台文本一般是 `[图片]`，是否会命中取决于群聊文案，不可依赖） |
| 触发判定 | [`inbound.Handle`](../bridge/internal/inbound/inbound.go#L198-L217) + [`commands.MessageTargetsBot`](../bridge/internal/commands/) | 只按文本 @；引用机器人发的图、`param.desc` 里的 at 段都不参与匹配 |
| 附件下载 | [`Dispatcher.prepareAttachments`](../bridge/internal/inbound/inbound.go#L285-L376) | 已实现"下载+校验+注入 prompt"，但**只处理 `Kind=="image"`** |
| 下载器注入 | [`setupPollers`](../bridge/internal/runtime/yzjcli.go#L142-L150) | `r.Disp.DownloadAttachment` **只在存在轮询 watcher 时才赋值**；纯 WSS 部署下为 nil |
| 可复用的解析/下载 | [`yzjcli.Message`](../bridge/internal/yzjcli/message.go#L94-L180)、[`MessageParam`](../bridge/internal/yzjcli/message.go#L71-L92)、[`ImageAttachments`](../bridge/internal/yzjcli/message.go#L149-L174)、[`FileDownload`](../bridge/internal/yzjcli/client.go#L354-L380) | 轮询模式已验证可用（`file download --id --output --overwrite`） |
| 配置 | [`config.default.yaml`](../config.default.yaml#L65-L79) | 只有 `yzj_cli.image`（轮询用），没有 WSS 附件相关配置 |

### 1.3 现状结论

1. WSS 消息**永远不会**带 `Attachments`；
2. 即使补上 `Attachments`，纯 WSS 部署下 `DownloadAttachment == nil`，下载也不会发生；
3. 纯图片/文件消息在 WSS 下大概率连 `Handle` 都进不去（内容为空或被 `MessageTargetsBot` 拒绝）。

---

## 2. 目标与验收标准

### 2.1 功能目标（v1）

- G1：WSS 收到图片（单图 `msgType=file,ftype=1`；富文本多图 `msgType=richText,param.desc[type=image]`）时，下载到 `workspace/incoming_media/YYYYMMDD/`，并把本地路径注入提示词。
- G2：WSS 收到文件（`msgType=file`，非图片）时同样下载并注入路径（v1 支持任意类型，但带扩展名黑名单与大小上限）。
- G3：纯图片/文件消息在"引用机器人消息"或"@机器人"语义下能正确触发（与 `yzj_cli.image_trigger: quote_bot | any | off` 对齐）。
- G4：下载失败、超限、CLI 未安装/未登录时**不阻塞主流程**，在提示词里追加一句说明，正常回复。
- G5：`both` 模式（WSS + 轮询/Webhook 同时开）不重复下载、不重复回复。

### 2.2 非功能目标

- 单条消息下载耗时上限 `attachment.timeout_sec`（默认 180s），不拖垮 job 队列；
- 文件名与元数据全部转义，落在 workspace 内，无路径穿越；
- 下载行为可观测（日志 + 状态字段）；
- 默认配置下**不影响现有纯文本链路**（零回归）。

### 2.3 验收用例（手动 + 单测）

| # | 场景 | 期望 |
|---|---|---|
| A1 | 群里 @机器人 发一张 jpg | 触发；prompt 含本地路径；模型能读到图 |
| A2 | 引用机器人消息再发一张图（`image_trigger=quote_bot`） | 触发；下载成功 |
| A3 | 富文本一条消息带 3 张图 | 3 个附件都下载，prompt 列出 3 条 |
| A4 | 发一个 2MB pdf | 触发（若配置允许）；prompt 含路径；不按图片校验 |
| A5 | 发 60MB 文件 | 不下载；prompt 注明"超过上限"；正常回复 |
| A6 | 发 `.exe` | 被黑名单拒绝；prompt 注明 |
| A7 | 未安装/未登录 yzj-cli | 不报错；prompt 注明"无法获取附件：yzj-cli 未就绪" |
| A8 | 图片伪装成 jpg（魔数不是图片） | 删除文件；prompt 注明"不是可识别的图片" |
| A9 | 纯文本 @机器人 | 行为与改造前完全一致 |
| A10 | `both` 模式同一消息双入口 | 只下载一次、只回复一次 |
| A11 | WSS 帧里没有 `file_id` | 走 CLI 反查兜底；仍拿不到则跳过并记日志（Phase 0 实测帧里有 `file_id`，本条为可选兜底） |
| A12 | 机器人自己发的图片 | 不触发（防循环） |

### 2.4 设计约束：事件驱动，**不引入轮询**

这是本方案的红线，必须写进实现约定：

- **WSS 仍然是被动事件源**：只在收到 `directpush/msgchg` 帧时处理，不新增任何 `for/select/goroutine` 轮询循环，不加定时器；
- **CLI 只在"识别到附件"时调用一次** `file download`：一条消息有 N 个附件就调 N 次（N ≤ `max_files`），处理完即返回，不常驻、不订阅、不 watch；
- **不调用 `im message list` 做常规取数**：唯一例外是"WSS 帧里没有 `file_id`"时的**单次反查**（见 L5），且必须满足四个条件：帧内带 `groupId`、`cli_fallback != off`、命中 5 分钟 LRU 缓存未命中、最多 2 页（40 条）；拿不到就放弃并降级为纯文本，**不得退化成轮询**；
- 与现有 `yzj_cli` 轮询入站**互不影响**：本方案不修改 `Watcher`、不改变 `poll_interval_sec`、不共享任何游标状态。

> 一句话：**WSS 负责"有没有附件"，CLI 只负责"把这一次的附件拿下来"。**

---

## 3. 关键假设与 Phase 0 门禁（必须先做）

**核心未知：WSS `directpush/msgchg` 帧里到底有没有 `msgType` / `param` / `file_id` / 群 ID。** 现有仓库没有任何 WSS 帧 fixture，代码里也没有读取过这些字段，因此**不能假设**。

Phase 0（0.5 人日，硬门禁）：

1. 在 [`ws.Client.loop`](../bridge/internal/ws/client.go#L140-L183) 临时加一行 debug 日志（仅 `log_level: debug`，截断 4KB），打印原始帧；
2. 用一个测试机器人分别发送：纯文本 @、单图、富文本多图（含 @）、pdf、引用机器人后发图；
3. 记录每种帧的字段路径，写入 `docs/wss-frame-samples.md` 并作为单测 fixture；
4. 若帧内**没有** `file_id`：确认是否有可用的群 ID + `msgId`（用于 `im message list --group-id --type newest --limit 20` 反查），否则该场景降级为"仅文本 + 提示用户附件无法获取"。

> 门禁结论决定 Phase 1 的解析字段和 Phase 4 是否必须做反查兜底。**Phase 0 未完成前不动生产代码。**

### 3.1 Phase 0 结论（2026-10-23，已完成，门禁通过）

**帧里直接带 `file_id`，不需要反查。**

- 图片：`msg.msgParam`（转义后的 **JSON 字符串**）→ `desc[].data`（`type=image`）；
- 文件：`msg.msgParam.file_id`（`ftype=0`、`name`、`ext`、`size` 齐全）；
- `msgType` 在 WSS 里是**数字**：`2=text`、`8=file`、`23=富文本/图片`；
- `content` 占位符非空（`[图片]…` / `[文件]:name`），不会命中 `content==""` 的丢弃分支；
- 控制帧（`auth`/`addressbook`/`pong`）无 `content`，归为 `control`。

字段表与原始帧见 [`wss-frame-samples.md`](wss-frame-samples.md)，fixture 在 `bridge/internal/inbound/testdata/`。

因此 **Phase 4（CLI 反查兜底）降级为可选**：只在 `msgParam` 缺失或解析失败时尝试，默认关闭。

> 本次抓的是机器人单聊（`groupId=BOT-…`）。**群聊 @ 帧仍需补一次样本**，用于确认 `desc` 里的 at 段与引用字段（Phase 1 触发层依赖）。

---

## 4. 总体方案

### 4.1 数据流

```
WSS 帧 ──► ClassifyWS ──► NormalizeWS（增强）
                              │
                              ├─ 文本/发送人/msgId/robotName（现有逻辑不动）
                              ├─ GroupID（新增，供反查）
                              └─ Attachments[]（新增：image | file）
                              │
                    触发判定（文本 @ || param.desc at 段 || 引用机器人）
                              │
                        Handle（去重、白名单、命令解析，现有）
                              │
                          runJob
                              │
                 prepareAttachments（扩展：image + file）
                              │
                    MediaDownloader.Download(fileID, out)
                              │  yzj-cli file download --id --output --overwrite
                              ▼
             workspace/incoming_media/YYYYMMDD/<msgid>-<idx>.<ext>
                              │
                     prompt："用户随消息发送了 N 个附件（本地路径…）"
                              │
                          后端执行
```

### 4.2 分层设计

**L1 解析层（`inbound`）**

- 新增 `NormalizeWS(raw) (Normalized, bool)`，内部：
  - 复用现有 `unwrap()` 与字段别名取文本/openId/msgId；
  - 新增 `GroupID`：别名 `groupId/group_id/groupOpenId/chatId/conversationId`；
  - 新增附件解析：把 `unwrap` 后的 map 用 `json.Marshal` → `json.Unmarshal` 进 [`yzjcli.Message`](../bridge/internal/yzjcli/message.go#L94-L103)（复用其 json tag，避免结构体重复），再调用 `Message.Attachments()`；
  - 若 `content` 为空但有附件 → 用平台占位符构造内容，**不再丢弃**。
- `Normalized` 增加 `GroupID string`、`HasMedia bool`；
- `Attachment.Kind` 由 `image` 扩展出 `file`；`yzjcli.Message.Attachments()` 同步扩展（`msgType=file` 且 `ftype!=1` 时返回 `Kind:"file"`）。

> 依赖方向：`inbound → yzjcli`（yzjcli 目前只依赖标准库，无环）。

**L2 触发层（`inbound` + `commands`）**

- 新增 `inbound.TargetsBot(b *bot.Bot, n Normalized, msg yzjcli.Message) bool`：
  1. `ForceTarget` 直接通过（轮询已匹配）；
  2. 文本 @ 规则（现有 `commands.MessageTargetsBot`）；
  3. **新增**：`msg.MentionsBots(...)`（`param.desc` 内 `BOT-` at 段）；
  4. **新增**：`msg.QuotesBot(...)` 且 `image_trigger != off`（与轮询一致）；
  5. 机器人自身 `msgId` 前缀 `BOT-` → 永不触发。
- `Handle` 中把 `!n.ForceTarget && !commands.MessageTargetsBot(...)` 替换为 `!TargetsBot(...)`；纯文本路径保持行为等价（用回归测试锁死）。

**L3 下载层（`inbound` + `runtime`）**

- 定义接口（`inbound/media.go`）：
  ```go
  type MediaDownloader interface {
      Download(ctx context.Context, fileID, outPath string) error
      Ready() bool
  }
  ```
- `runtime` 侧新增 `ensureMediaDownloader(profile) (MediaDownloader, error)`：
  - 用 [`yzjcli.ResolveBinary`](../bridge/internal/yzjcli/install.go#L72-L102) 定位已安装二进制（**WSS 路径不触发自动安装**，避免阻塞消息处理）；
  - 用 [`yzjcli.NewClient`](../bridge/internal/yzjcli/client.go#L148-L157) + `FileDownload`；
  - 与轮询共用同一 `profile/config_dir` 登录态；
  - 失败只 log 一次（去重告警），返回 `Ready()==false` 的空实现。
- `setupPollers` 之外的装配点：`Runtime.Load` 末尾统一设置 `r.Disp.DownloadAttachment`（改造点，注意锁内不能下载）。
- 下载前校验顺序（与 `prepareAttachments` 现有顺序一致并扩展）：
  1. `Kind` → 图片走图片白名单 + 魔数校验；文件走黑名单 + 大小上限；
  2. `max_files`（默认 5）截断；
  3. `timeout_sec`（默认 180s）；
  4. 落盘后用 `os.Stat` 复核大小；图片再 `sniffImageMime`；
  5. 失败清理半成品文件。

**L4 提示词层（`inbound.prepareAttachments`）**

- 支持 `Kind=="file"`：输出
  ```
  用户随消息发送了 2 个附件（本地路径，可直接读取）：
  - 图片 1: <path> (image/png, 21KB)
  - 文件 2: <path> (pdf, 1.2MB，原文件名: 对账单.pdf)
  ```
- 失败项统一汇总到末尾；不改变现有图片文案语义（仅追加文件块）。

**L5 兜底解析（Phase 4，条件触发）**

若 Phase 0 证明 WSS 帧缺 `file_id`：

- 用 `GroupID + MsgID` 反查：
  ```go
  page, err := client.MessageList(ctx, groupID, "newest", "", 20)
  // 按 msgId 匹配；最多 2 页（40 条），超出放弃并记日志
  ```
- 反查带 5 分钟 LRU 缓存（`msgId → param`），避免重复拉取；
- **反查是一次性查询**：不循环、不等待、不重试超过 1 次；`attachment.cli_fallback: off` 时完全不查；
- 仍失败 → 降级为"仅文本 + 提示附件无法获取"。

**L6 配置层**

`config.default.yaml`（`yzj_cli` 下新增，轮询与 WSS 共用）：

```yaml
yzj_cli:
  attachment:
    enabled: true            # WSS/Webhook 附件总开关
    images: true
    files: true
    max_files: 5             # 单条消息最多下载数
    image_max_mb: 20
    file_max_mb: 50
    image_types: [png, jpg, jpeg, gif, webp, bmp]
    file_block_ext: [exe, dll, bat, cmd, ps1, vbs, js, msi, scr, com, jar]
    timeout_sec: 180
    keep_days: 7             # 复用现有 cleanupIncomingMedia
    cli_fallback: auto       # auto|on|off：WSS 帧缺 file_id 时是否用 CLI 反查
```

兼容性：保留原 `yzj_cli.image`（轮询），v1 不做合并；Phase 5 可让 `image` 读 `attachment` 的默认值（带弃用注释）。

**L7 去重与并发**

- 去重键不变（`botID + ":" + msgID`，[`Handle` L209-L214](../bridge/internal/inbound/inbound.go#L198-L217)）；
- `both` 模式下 WSS 先到则下载一次；Webhook/轮询后到会被去重；
- 下载在 job 内串行（现有 `jobs` 队列），不新增并发池。

---

## 5. 任务拆分（建议按序提交）

| Phase | 任务 | 主要文件 | 产出/测试 | 预估 |
|---|---|---|---|---|
| 0 | WSS 帧抓样与字段落档 | `ws/client.go`（临时 debug）、`docs/wss-frame-samples.md` | 5 类帧样本 + fixture | 0.5d |
| 1 | 解析层：`NormalizeWS` + `GroupID` + `Attachments` + `msgType=file` | `inbound.go`、`yzjcli/message.go`、`message_test.go` | 单测：单图/多图/文件/空文本 | 1d |
| 2 | 下载器装配 + 配置 + 文件类型支持 | `runtime/*`、`inbound/media.go`、`config.default.yaml`、`config` 测试 | 单测：fake downloader/超限/黑名单/未就绪 | 1d |
| 3 | 触发规则（at 段/引用/图片） | `inbound.go`、`commands/*` | 回归：纯文本不变；新增 A1/A2 用例 | 0.7d |
| 4 | 兜底反查（若 Phase 0 需要）+ 重试/日志指标 | `inbound.go`、`yzjcli/client.go` | 集成测试 A11；日志字段 | 1d |
| 5 | 文档/变更记录/GUI 文案 + 全量验收 | `docs/bridge-runtime.md`、`changelog.md`、GUI 配置项（如有） | A1–A12 全过 | 0.5d |

合计约 4.7 人日（不含联调等待）。每 Phase 独立可编译、可测、可回滚。

---

## 6. 关键实现草案（Go 伪代码）

```go
// inbound.go
func NormalizeWS(raw map[string]any) (Normalized, bool) {
    n, ok := Normalize(raw)          // 现有文本字段逻辑
    if !ok {
        // 空文本但有附件：仍要放行
        if media, mok := wsMedia(raw); mok && len(media) > 0 {
            n = Normalized{MsgID: firstString(unwrap(raw), "msgId", "id"), Content: "[附件]"}
        } else {
            return n, false
        }
    }
    n.Source = "websocket"
    n.GroupID = firstString(unwrap(raw), "groupId", "group_id", "groupOpenId", "chatId", "conversationId")
    n.Attachments = wsMedia(raw)
    return n, true
}

func wsMedia(raw map[string]any) []Attachment {
    inner := unwrap(raw)
    b, _ := json.Marshal(inner)
    var m yzjcli.Message
    if json.Unmarshal(b, &m) != nil {
        return nil
    }
    out := m.Attachments() // 已扩展 image + file
    return convert(out)    // yzjcli.Attachment → inbound.Attachment
}
```

```go
// runtime: 纯 WSS 部署也要装配下载器
func (r *Runtime) ensureMediaDownloaderLocked() {
    cfg := r.yzjCLIConfigLocked()
    bin, err := yzjcli.ResolveBinary(cfg)   // 不自动安装
    if err != nil {
        log.Printf("yzjcli media: disabled: %v（安装/登录 yzj-cli 后可恢复附件）", err)
        return
    }
    client := yzjcli.NewClient(bin, cfg.Profile, cfg.ConfigDir, cfg.Timeout)
    r.Disp.DownloadAttachment = client.FileDownload
}
```

---

## 7. 错误处理与降级矩阵

| 情况 | 行为 | 用户可见 |
|---|---|---|
| yzj-cli 未安装 / 未登录 | 关闭附件下载，主流程继续 | 提示词注明"无法获取附件：yzj-cli 未就绪" |
| `file download` 非 0 退出 | 记录 exit code + 分类（transport/api/auth） | 提示词注明失败原因（截断） |
| 下载超时 | 按 `timeout_sec` 取消，清理半成品 | 注明"下载超时" |
| 超过大小/数量上限 | 不下载 | 注明"超过上限" |
| 扩展名黑名单 | 不下载 | 注明"类型不允许" |
| 图片魔数校验失败 | 删除文件 | 注明"不是可识别的图片" |
| WSS 帧缺 file_id 且反查失败 | 仅文本 | 注明"未找到附件信息" |
| `both` 双入口 | 去重命中后直接返回 | 只回一次 |

---

## 8. 安全与合规

1. **路径**：`safeFileToken(msgID)` + 索引 + 白名单扩展名；目录固定 `workspace/incoming_media/YYYYMMDD/`；拒绝 `..`、绝对路径、UNC。
2. **类型**：图片必须魔数匹配；文件按黑名单拒绝可执行/脚本类（可配置）；下载后复核大小。
3. **内容隔离**：只注入"本地路径"，不把文件内容直接塞进 prompt；模型按需读取。
4. **清理**：复用 [`cleanupIncomingMedia`](../bridge/internal/runtime/yzjcli.go#L353-L377)，按 `keep_days` 清理。
5. **凭据**：桥不接触云之家 token，全部由 yzj-cli 管理（维持现状）。
6. **限流**：附件只在被触发消息上下载，量小；`file download` 是独立接口，不占用 `msgList` 频控预算。
7. **日志**：不打印 file_id 全量（保留后 6 位）、不打印文件内容。

---

## 9. 测试计划

**单元测试**
- `inbound_test.go`：`NormalizeWS` 对 5 类 fixture 的字段解析；空文本+附件放行；`TargetsBot` 各分支；`prepareAttachments` 的 file 分支与失败分支。
- `yzjcli/message_test.go`：`Attachments()` 对 `msgType=file/ftype!=1` 返回 `Kind:"file"`。
- `runtime`：下载器装配失败/成功；不自动安装。

**集成测试**
- `fakeRunner`（已有 [`client_test.go`](../bridge/internal/yzjcli/client_test.go)）模拟 `file download` 成功/失败/超时；
- 端到端：构造 WSS 帧 → `Handle` → 断言 prompt 内容与落盘文件。

**实机验收（灰度）**
- 用测试群 + 1 个 WSS bot 跑 A1–A12；确认 `incoming_media` 落盘、`keep_days` 清理、日志无泄露。

**回归**
- 纯文本 @、`/stop`、`/jobs`、白名单拦截、`both` 去重、轮询图片（不能退化）。

---

## 10. 发布与回滚

- **开关**：`yzj_cli.attachment.enabled`（默认 true，但仅在配置了 WSS/Webhook 的 bot 上生效；想完全关闭可置 false）。
- **灰度**：先在 1 个测试 bot 开；观察 24h 日志（下载成功率、耗时、失败分类）；再全量。
- **回滚**：配置关开关即可（无需回滚二进制）；或 `git revert` 分支提交（Phase 独立）。
- **兼容**：不改 `yzj_cli` 轮询主流程；不改发送侧；不改数据库/状态文件格式（`Normalized` 只加字段）。

---

## 11. 风险与未决问题

| 风险 | 影响 | 缓解 |
|---|---|---|
| WSS 帧不含 `file_id` | 需反查，增加一次 `msgList` 调用 | Phase 0 门禁；反查限 2 页 + LRU 缓存 |
| 个人机器人（direct）会话无群 ID | 反查不可用 | Phase 0 确认；退化为仅文本 |
| CLI 未登录/未装（WSS 用户常见） | 附件不可用 | 明确提示 + GUI 通道卡片状态 |
| 大文件阻塞 job 队列 | 回复变慢 | 大小上限 + 超时 + `max_files` |
| 富文本多图顺序/重复 | 附件错位 | 按 `desc` 顺序 + `fileID` 去重 |
| 平台改字段 | 解析失效 | 别名容错 + 解析失败降级为纯文本 |
| 与轮询功能重复 | 维护成本 | v1 复用同一 `yzjcli.Message` 与 `FileDownload`，不另写一套 |

未决（Phase 0 结论后回填）：
- WSS 帧字段路径与样例；
- 是否需要 `cli_fallback`；
- 文件类型是否需要默认更严格（仅日志/图片/压缩包）。

---

## 12. 参考

- [`docs/plan-yzj-cli-im-source.md`](plan-yzj-cli-im-source.md)：轮询/图片接收的设计与实测
- [`docs/bridge-runtime.md`](bridge-runtime.md)：运行时索引
- [`.research/yzj-cli.md`](../.research/yzj-cli.md)：CLI 命令树、退出码、实测样本
- 代码：`inbound`、`yzjcli`、`runtime`、`ws`、`webhook`
