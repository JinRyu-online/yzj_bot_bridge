# YZJ Bridge GUI（Tauri + React）

控制面板：机器人/通道、AI 设置、运行日志、系统托盘。

## 开发

```powershell
# 需先有 bridge/bin/yzj-bridge.exe（可用仓库根目录 build_all.ps1 或单独 go build）
npm install
npm run tauri dev
```

## 测试

```powershell
npm run test:e2e
```

## IDE

建议安装 [Tauri](https://marketplace.visualstudio.com/items?itemName=tauri-apps.tauri-vscode) 与 [rust-analyzer](https://marketplace.visualstudio.com/items?itemName=rust-analyzer.rust-analyzer)。
