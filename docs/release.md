# 发布 Windows 安装包（GitHub Release）

给别人用的产物是 **NSIS 安装包**，不是单个 `YZJBridge.exe`。

用户拿到 `YZJBridge-<version>-Windows-x64-setup.exe` 后：双击安装 → 开始菜单出现「YZJ Bridge」→ 也可在完成页勾选桌面快捷方式。安装器会带上桥进程、`WebView2Loader.dll`，并在缺少 WebView2 时静默补装。

当前仓库版本号写在 [`gui/src-tauri/tauri.conf.json`](../gui/src-tauri/tauri.conf.json) 的 `version`（例如 `0.2.1`）。Git tag 必须是 `v` + 同一版本号，例如 `v0.2.1`。

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
- Release notes 写清用户可见变更（会直接出现在弹窗里）。

---

## 推荐流程（Actions 自动打包）

本机 Cursor 往往推不上 GitHub，请在 **系统终端 / Git Bash** 里操作（已配置 SSH 或已 `gh auth login`）。

### 1. 改版本并提交

把 `gui/src-tauri/tauri.conf.json` 的 `version` 改成新号，例如 `0.2.2`。不要复用已经发布过的 tag。

```powershell
git add gui/src-tauri/tauri.conf.json
# 连同本次要发的功能一起提交
git commit -m "chore(release): 0.2.2"
git push origin main
```

### 2. 打 tag 并推送（这一步才会发 Release）

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
