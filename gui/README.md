# YZJ Bridge GUI（Tauri + React）

控制面板：机器人/通道、AI 设置、运行日志、系统托盘。系统设置页展示应用版本（与 `tauri.conf.json` 一致），并支持检查更新。

## 开发

```powershell
# 需先有 bridge/bin/yzj-bridge.exe（可用仓库根目录 build_all.ps1 或单独 go build）
npm install
npm run tauri dev
```

本地 `tauri dev` **不会**在启动时自动检测更新（避免打扰调试）；系统设置 →「检查更新」仍可手动触发。正式安装包启动后会自动检测 GitHub Releases，详见 [docs/release.md](../docs/release.md)「客户端自动更新」。

## 测试

```powershell
npm run test:e2e
```

更新相关用例覆盖：启动自动弹窗、启动失败静默、稍后提醒、跳过此版本、下载中状态、缺少安装包提示。

## IDE

建议安装 [Tauri](https://marketplace.visualstudio.com/items?itemName=tauri-apps.tauri-vscode) 与 [rust-analyzer](https://marketplace.visualstudio.com/items?itemName=rust-analyzer.rust-analyzer)。
