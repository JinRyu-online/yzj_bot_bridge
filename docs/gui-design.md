# YZJ Bridge GUI 设计规则

本文档约束 `gui/` 面板的视觉与交互，作为后续 GUI 改版与新组件的依据。
标准交互参考：**系统页 →「检查更新」**（`action-chip` + loading + 右上角 toast）。

相关实现：

- 样式：[gui/src/App.css](../gui/src/App.css)
- Toast：[gui/src/toast.tsx](../gui/src/toast.tsx)
- 下拉：[gui/src/FancySelect.tsx](../gui/src/FancySelect.tsx)

---

## 1. 主题与基础变量

- 主题通过 `data-theme` 切换：`aurora` / `midnight` / `sand` / `ice`。
- 颜色与圆角一律使用 CSS 变量（`--accent`、`--danger`、`--panel-border`、`--radius` 等），禁止硬编码品牌色扩散到新组件。
- Ice 主题下实心 primary 按钮文字为白；**ghost 不受该覆盖影响**（选择器须 `:not(.ghost)`）。

---

## 2. 按钮语义分轨（外观）

保留语义色，**不要**把所有按钮改成同一种半透明 chip：

| 语义 | 类名 | 用途 |
|------|------|------|
| Primary | `.btn` | 主 CTA：保存、新建、确认、导入 |
| Ghost / Secondary | `.btn.ghost` | 取消、编辑、次要操作、清空视图 |
| Danger | `.btn.danger` / `.btn.danger.ghost` | 删除、破坏性操作 |
| Chip | `.action-chip`（可加 `.side`） | 行内探测/安装/刷新/重载/检查更新 |

尺寸：

- 默认：md（pill `border-radius: 999px`）
- 小尺寸：`.btn.sm` / `.btn.xs`（保留别名 `.small` / `.tiny`，新代码优先 `sm`/`xs`）
- 表单旁 chip：`.action-chip.side`（高度对齐输入框，圆角约 12px）

---

## 3. 交互标准（所有可点操作控件）

对齐「检查更新」手感：

| 状态 | 行为 |
|------|------|
| Hover | `translateY(-1px)` + 短 transition（约 0.12–0.15s） |
| Active（按下） | 取消上浮 / 略压，可加轻微 `filter: brightness(0.96)` |
| Disabled / Loading | 降透明、`cursor: not-allowed`；禁止再触发 |

**异步操作按钮**还必须：

1. `loading` 类 + `spinner`（常用 `spinner dark`）
2. 文案切换（如「检查中」「保存中」）
3. `disabled` 至结束
4. 结束后用右上角 **toast** 反馈

同步操作（打开模态、切换页、本地 UI）只需 hover/active，**不要**假 loading。

---

## 4. Toast 反馈

- API：`useToast()` → `showToast(message, tone?)`
- 语气：`ok` | `err` | `neutral`（失败禁止用绿色 `ok`）
- 时长约 2.2s，右上角，`data-testid="save-toast"`
- **自动后台探测**（进入设置页自动 discover/probe）默认不 toast；仅用户点击时传 `notify: true`
- 弹窗字段校验继续用 `ModalFloatMessage`；桥启动失败等持久错误继续用页内 `err`，不强制改 toast

---

## 5. 控件分类与可否动

| 类型 | 示例 | 规则 |
|------|------|------|
| 操作按钮 | `.btn`、`.action-chip`、`.link` | 遵循第 2–4 节 |
| 图标钮 | `.modal-close`、`.secret-toggle`、`.skills-browse-btn`、`.chat-icon-btn`、`.chat-scroll-bottom` | 统一 hover/active；无 toast（除非异步失败） |
| 导航/列表选择 | `.nav-btn`、`.role-card`、`.memory-user-list button`、chat picker/history | 统一 hover/active；选中态不加位移；无 loading/toast |
| 保留专用 | `Switch`、`FancySelect`、思考折叠、帮助头像、外链卡片 | 不硬套 `.btn` 外观 |

链接按钮：

- 内联文本操作用 `.link` / `.link.danger`
- 路径打开用 `.path-link`（等宽、下划线）

---

## 6. 表单与选择

- 表单输入：圆角约 12px，focus 时 accent 边框 + 外发光环
- 自定义下拉统一用 `FancySelect`（支持搜索、portal 菜单）
- 聊天「记忆用户绑定」等调试入口：下拉选项来自记忆档案；空选项文案为「不模拟用户调用（不调用记忆）」

---

## 7. 布局与动效

- 页面进入可用既有 `page-rise`；避免新增花哨大面积动画
- 卡片：`.card.soft` + panel 边框/阴影；不在 hero 堆叠无关统计块
- 滚动条全局隐藏；日志/聊天等用自有滚动容器

---

## 8. 无障碍与测试

- 可交互元素优先 `type="button"`；图标钮补 `aria-label` / `title`
- 关键控件保留稳定 `data-testid`（如 `check-update-btn`、`save-toast`）
- 新增异步按钮时，e2e 应能断言 loading 文案或 toast

---

## 9. 反模式（禁止）

- 新造第三套「主按钮」外观，与 `.btn` / `.action-chip` 并行
- 只有 hover、没有 `:active`
- 异步成功/失败只改页内文字、不 toast（调试 hint 可并存，但不能替代结果反馈）
- 失败 toast 使用 `.ok` 绿色
- 在导航/列表行上套假 loading
- Ice 主题下 ghost 文字被刷成白色导致不可读
