# YZJ Bridge v0.4.1

## 新功能

- **DSH 思考强度配置**：新增 `defaults.dsh_reasoning_effort`，并在「AI 设置 → DSH」增加下拉框，可选 `默认 / 低 low / 中 medium / 高 high`。
- 自定义 OpenAI 兼容 provider（如 kuaidi100 的 `deepseek-flash`）会自动声明 `reasoningEfforts` 并关闭 DSH 的 developer role；否则 DSH 会报 `UNSUPPORTED_REASONING_EFFORT` 或网关 422。
- `high` 思考更充分但更慢、更耗 token；不设置时保持模型/provider 默认行为，不影响现有配置。

## 说明

- 思考强度只对支持 reasoning 的 DSH 模型生效；不同 provider 支持的档位可能不同，DSH 返回不支持时会直接报错。
- 配置同时作用于 headless 与 sdk 模式生成的 DSH profile；旧 `cli` 入口保持原行为。

## 安装

下载并覆盖安装 `YZJBridge-0.4.1-Windows-x64-setup.exe`，重启桥服务。
