# 发布 Windows 安装包（GitHub Release）

给别人用的产物是 **NSIS 安装包**，不是单个 `YZJBridge.exe`。

用户拿到 `YZJBridge-<version>-Windows-x64-setup.exe` 后：双击安装 → 开始菜单出现「YZJ Bridge」→ 也可在完成页勾选桌面快捷方式。安装器会带上桥进程、`WebView2Loader.dll`，并在缺少 WebView2 时静默补装。

当前仓库版本号写在 [`gui/src-tauri/tauri.conf.json`](../gui/src-tauri/tauri.conf.json) 的 `version`（例如 `0.2.1`）。Git tag 必须是 `v` + 同一版本号，例如 `v0.2.1`。

> ⚠️ **发版铁律：先改版本号，再打 tag。** 安装包文件名（`YZJBridge-<version>-Windows-x64-setup.exe`）与打包产物名称都取自 `tauri.conf.json` 的 `version`，不是 git tag。若只打 tag 不更新 `tauri.conf.json`，打包出来的仍是旧版本号（例如打了 `v0.3.2` 却打包成 `YZJBridge-0.3.1-...`）。因此每次发版必须：① 改 `tauri.conf.json` 的 `version` 并提交 → ② 再打同号 tag。二者不一致视为发布事故。

---

## 客户端自动更新（轻量 GitHub Releases）

正式安装包启动后会请求 `https://api.github.com/repos/JinRyu-online/yzj_bot_bridge/releases/latest`：

1. 若远端 tag 大于本机 `tauri.conf.json` 版本，且资源里有  
   `YZJBridge-<version>-Windows-x64-setup.exe`，则弹窗展示 Release 正文（更新日志）。
2. 用户可 **稍后提醒**（仅关弹窗）、**跳过此版本**（写入 `%USERPROFILE%\.yzj-bridge\update-prefs.json`）、或 **立即更新**（下载安装包到临时目录、启动安装向导并退出本应用）。
3. 系统设置页「检查更新」可手动再查；手动检查会忽略「跳过」，仍能打开弹窗。
4. **`tauri dev` / Vite 开发模式不会自动检测**，避免干扰调试；设置页手动检查仍可用。
5. 启动时网络失败静默忽略；手动检查失败会 toast 提示。若有新版本但缺少上述安装包文件名，会提示缺少资源而不是「已是最新」。

发版时请保证：

- 安装包文件名与 tag 版本一致（含版本号那段）。
- **Release notes 写清用户可见变更（会直接出现在弹窗里），且必须是完整 what's change 发布日志**——按「新功能 / 优化 / 修复 / 其他」分组，写用户视角的变更说明，见下方「推荐流程」1.5 节。

---

## 推荐流程（Actions 自动打包）

本机 Cursor 往往推不上 GitHub，请在 **系统终端 / Git Bash** 里操作（已配置 SSH 或已 `gh auth login`）。

### 1. 改版本并提交

把 `gui/src-tauri/tauri.conf.json` 的 `version` 改成新号，例如 `0.2.2`。不要复用已经发布过的 tag。

> 改版本前先确认 `tauri.conf.json` 的 `version` 与本次 tag 完全一致（如 `0.2.2` ↔ `v0.2.2`），否则打包产物与安装包文件名会停留在旧版本号。

```powershell
git add gui/src-tauri/tauri.conf.json
# 连同本次要发的功能一起提交
git commit -m "chore(release): 0.2.2"
git push origin main
```

### 1.5 编写完整 what's change 发布日志（每次发版必须）

**每次发版都必须在 Release notes 里编写完整的 what's change 发布日志**（会直接显示在客户端「检查更新」弹窗里），不允许只写一行版本号或留空。工作流用 `generate_release_notes: true` 自动生成的是 GitHub 提交列表，**不能替代人工编写的用户视角发布日志**——发布前需人工补全。

发布日志编写要求：

1. **用户视角、按价值分组**，不要罗列 commit 标题。推荐分组：
   - **新功能**（用户能用到的增量能力）
   - **优化/体验**（交互改进、性能、稳定性）
   - **修复**（用户可感知的 bug 修复）
   - **其他**（依赖升级、文档、内部重构等一句话带过）
2. **每条讲"能做什么/解决了什么"**，不写技术实现细节。例如「修复：切页后成功提示不再重复弹出」而不是「fix(gui): FadingHint resetKey 记忆统一」。
3. **涉及配置/行为变化的必须写明**：新增配置字段、默认值变化、需要用户手动迁移的操作（如「Claude Code 不再需要填 API Key，使用本机登录凭据」）。
4. **破坏性变更（breaking changes）单独列出**并给迁移指引。
5. 版本号放在标题：`v0.2.2`，下方正文用上面的分组结构。

发布日志落盘到 [`docs/changelog.md`](../docs/changelog.md)，按版本倒序追加（最新在上）。Release notes 内容与之一致。

### 2. 打 tag 并推送（这一步才会发 Release）

> 打 tag 前确认 **1.5 的发布日志已写好**（`docs/changelog.md` 已更新、Release notes 正文已准备），否则 Actions 打包完成后你还得回头补日志。

```powershell
git tag v0.2.2
git push origin v0.2.2
```

只推 `main` **不会**发版。工作流文件：[`.github/workflows/release-windows.yml`](../.github/workflows/release-windows.yml)，触发条件是 tag `v*`。

### 3. 等 Actions，再把安装包链给人

1. 打开 [Actions → Release Windows](https://github.com/JinRyu-online/yzj_bot_bridge/actions/workflows/release-windows.yml)
2. 对应 `v0.2.2` 的 run 变绿（约 8–15 分钟）
3. 打开 [Releases](https://github.com/JinRyu-online/yzj_bot_bridge/releases)，下载  
   `YZJBridge-0.2.2-Windows-x64-setup.exe`

用 GitHub CLI 看状态：

```powershell
gh run list --workflow "Release Windows" --limit 5
gh run watch
gh release view v0.2.2
```

---

## 本机先打包装再手动发（备用）

CI 挂了、或想自己验安装包时用。

```powershell
.\build_all.ps1
```

产物：

| 路径 | 用途 |
|------|------|
| `dist/YZJBridge-<version>-Windows-x64-setup.exe` | 发给用户 / 挂到 Release |
| `dist/YZJBridge-Windows-x64-setup.exe` | 同上，不带版本号的副本 |
| `dist/YZJBridge/` | 便携目录，必须**整夹**拷贝（含 dll 和 `yzj-bridge.exe`） |

手动创建 Release（tag 要先存在，或 `gh release create` 会建 tag）：

```powershell
gh release create v0.2.2 dist/YZJBridge-0.2.2-Windows-x64-setup.exe --title "YZJ Bridge v0.2.2" --generate-notes
```

若该 tag 的 Release 已由 Actions 建好、只差补文件：

```powershell
gh release upload v0.2.2 dist/YZJBridge-0.2.2-Windows-x64-setup.exe --clobber
```

---

## 推送与登录（本机）

优先 SSH。`~/.ssh/config` 里 `Host github.com` 应指向本机私钥（例如 `~/.ssh/id_ed25519_jinryu`）。对应公钥要加到 GitHub：[SSH keys](https://github.com/settings/keys)。

```powershell
ssh -T git@github.com
# 成功会看到：Hi JinRyu-online! ...
git remote -v
# 若仍是 https 且 push 要密码，可改成 SSH：
git remote set-url origin git@github.com:JinRyu-online/yzj_bot_bridge.git
```

也可用 GitHub CLI（不要把 PAT 发到聊天里）：

```powershell
gh auth login
# 选 GitHub.com → HTTPS 或 SSH → 浏览器登录
```

`workflow_dispatch` 只打安装包工件，**不会**创建 GitHub Release；正式发版请推 `v*` tag。

---

## 注意

- 已经发布成功的 tag **不要** `git tag -f` / `git push --force` 改指向；下个版本用新号。
- 安装包文件名来自 `tauri.conf.json` 的 `version`，和 tag 对不上会让人搞混（也会导致客户端匹配不到更新包）。
- 不要只发 `YZJBridge.exe`：缺 `WebView2Loader.dll` 和 `yzj-bridge.exe` 会打不开或起不了桥。
- 用户配置在 `%USERPROFILE%\.yzj-bridge\`，安装包不会覆盖对方已有的 `config.yaml`。
  「跳过此版本」偏好保存在同目录下的 `update-prefs.json`。
- 客户端更新行为详见上文「客户端自动更新」。
