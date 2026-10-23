# WSS 推送帧样本（Phase 0 抓帧结果）

- 抓帧时间：2026-10-23 11:59:49 – 12:02:19（Asia/Shanghai）
- 抓帧方式：`defaults.ws_debug_frames: true`（`ws.Client.DebugFrames`，截断 4KB）→ `~/.yzj-bridge/logs/runtime-2026-10-23.jsonl`
- 目标：`bot=fairy`，`inbound_mode: both`（WSS + webhook），通道是机器人单聊（`groupId` 形如 `BOT-…`）
- 原始帧 fixture：`bridge/internal/inbound/testdata/ws_{text,image,file}.json`
- 样本中的姓名已替换为「张三」，其余 id 保持原样（不透明 id，便于对照日志）

## 1. 帧清单

| # | 帧 | `len` | 说明 |
|---|---|---|---|
| 1 | `{"success":true,"cmd":"auth"}` | 29 | 鉴权成功 |
| 2 | `{"cmd":"addressbook","lastUpdateTime":"…"}` | 60 | 通讯录变更通知 |
| 3 | `{"cmd":"pong"}` | 14 | 心跳 |
| 4 | `directPush` + `robotMessage`，`msgType=2` | 750 | 纯文本 |
| 5 | `directPush` + `robotMessage`，`msgType=23` | 871 | 图片（富文本段） |
| 6 | `directPush` + `robotMessage`，`msgType=8` | 877 | 文件 |

## 2. 消息帧结构（外层）

```json
{
  "msg": { "...见下..." },
  "level": 0,
  "cmd": "directPush",
  "type": "robotMessage"
}
```

> `type` 是 `robotMessage`（驼峰），现有 `wrap`/`unwrap` 靠 `msg` 键命中，行为正确。

## 3. `msg` 关键字段

| 字段 | 类型 | 文本样本 | 图片样本 | 文件样本 |
|---|---|---|---|---|
| `msgType` | 数字 | `2` | `23` | `8` |
| `content` | string | `文件里是什么` | `[图片]这是什么` | `[文件]:示例文件.json` |
| `msgParam` | **JSON 字符串** | `{"desc":[],"notifyTo":[],"notifyType":1}` | `{"desc":[{"data":"6adadbdf9162a60001e6a2a5","h":91,"length":4,"start":0,"type":"image","w":92}],"notifyTo":[],"notifyType":1}` | `{"ext":"json","file_id":"6adadbf8ae10d100011722a8","ftype":0,"name":"示例文件.json","size":9187,"unreadMonitor":1}` |
| `groupId` | string | `BOT-67bfd…-BOT-6a72…` | 同左 | 同左 |
| `robotId` / `robotName` | string | `BOT-6a72…` / `Fairy` | 同左 | 同左 |
| `openId` | string | `67bfd…4272` | 同左 | 同左 |
| `msgId` | string | `6adadbf1…` | `6adadbe0…` | `6adadbf9…` |
| `operatorOpenid` | string | `67bfd…4272` | 同左 | 同左 |
| `operatorName` / `senderName` | string | 张三 | 张三 | 张三 |
| `time` | int64(ms) | `1792728049669` | `1792728032078` | `1792728057301` |
| `groupType` | 数字 | `3` | `3` | `3` |

## 4. 结论（门禁判定）

1. **帧里带 `file_id`，不需要 CLI 反查**：
   - 图片：`msgParam.desc[].data`（`type=image`）就是 `file download --id` 要的 id；
   - 文件：`msgParam.file_id`；
   - `msgParam` 是**转义后的 JSON 字符串**，需要二次 `json.Unmarshal`（轮询路径里 `param` 是对象，两者形状不同）。
2. **`msgType` 在 WSS 里是数字**：`2=text`、`8=file`、`23=富文本/图片`。映射后可直接复用 [`yzjcli.Message.Attachments()`](../bridge/internal/yzjcli/message.go) 的解析逻辑。
3. **`content` 不会是空串**：图片是 `[图片]…`、文件是 `[文件]:name`，所以不会命中 `Normalize` 里 `content == "" → ok=false` 的丢弃分支；纯图无文字时预计是 `[图片]`（待补样本）。
4. **`groupId` 存在**（虽本样本是单聊 `BOT-…`），仍按计划保留 `Normalized.GroupID` 与兜底开关，但不作为主路径。
5. **控制帧**（`auth`/`addressbook`/`pong`）没有 `content`，`Normalize` 返回 false，`ClassifyWS` 会归为 `control`，不会误触发。
6. **群聊 @ 帧尚未抓**：本次通道是机器人单聊（`groupId=BOT-…`、`groupType=3`）。群聊 `@机器人` 的 at 段、引用（reply）字段预计同样在 `msgParam.desc` / `msgParam.reply*` 里，但**需补一次群聊抓帧确认**（Phase 1 的 `TargetsBot` 依赖它）。

## 5. 对实现的影响

- `inbound.NormalizeWS`：在 `Normalize` 之上补 `GroupID` + 附件；`msgParam` 支持字符串/对象两种形态。
- `yzjcli.Message.Attachments()`：`msgType=file` 且 `ftype!=1` 时返回 `Kind:"file"`（图片仍走 `ImageAttachments`）。
- `Dispatcher.prepareAttachments`：图片与文件统一下载、分类校验、注入 prompt（文件超限/黑名单/失败均降级为文字说明）。
- `runtime.applyMediaSettingsLocked`：图片/附件下载设置与「是否有轮询群」解耦，纯 WSS 部署也会注入 `DownloadAttachment`（`yzj-cli file download`）。
