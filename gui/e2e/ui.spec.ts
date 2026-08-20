import { test, expect, type Page } from "@playwright/test";

async function installTauriMock(page: Page) {
  await page.addInitScript(() => {
    localStorage.setItem("yzj-theme", "ice");
    type Bot = Record<string, unknown>;
    const state = {
      autostart: false,
      seq: 65,
      config: {
        defaults: {
          cursor_bin: "agent",
          claude_bin: "claude",
          cursor_api_key: "",
          anthropic_api_key: "",
          openai_api_key: "",
          openai_base_url: "",
          projects_root: "~",
          session_mode: "shared",
          cursor_model: "",
          memory: { enabled: false, gui_bind_enabled: false },
        },
        bots: [
          {
            id: "fairy",
            name: "Fairy",
            backend: "cursor_cli",
            channels: [
              {
                group: "workAssistant",
                send_msg_url:
                  "https://www.yunzhijia.com/gateway/robot/webhook/send?yzjtype=0&yzjtoken=a",
              },
              {
                group: "devlopment",
                send_msg_url:
                  "https://www.yunzhijia.com/gateway/robot/webhook/send?yzjtype=0&yzjtoken=c",
              },
            ],
          },
          {
            id: "youkai",
            name: "Youkai",
            backend: "claude_code",
            workspace: "~/.yzj-bridge/workspace/youkai",
            skills: ["hello-workspace", "kdlog"],
            system_prompt: "你是 Youkai，负责开发协助。",
            channels: [
              {
                group: "development",
                send_msg_url:
                  "https://www.yunzhijia.com/gateway/robot/webhook/send?yzjtype=0&yzjtoken=b",
              },
            ],
          },
        ] as Bot[],
      },
      status: [
        {
          id: "fairy__workAssistant",
          role_id: "fairy",
          name: "Fairy",
          group: "workAssistant",
          backend: "cursor_cli",
          inbound_mode: "websocket",
          connected: true,
          ws_enabled: true,
          last_error: "",
          has_ws: true,
        },
        {
          id: "fairy__devlopment",
          role_id: "fairy",
          name: "Fairy",
          group: "devlopment",
          backend: "cursor_cli",
          inbound_mode: "websocket",
          connected: false,
          ws_enabled: true,
          last_error: "",
          has_ws: true,
        },
        {
          id: "youkai",
          role_id: "youkai",
          name: "Youkai",
          group: "development",
          backend: "claude_code",
          inbound_mode: "websocket",
          connected: false,
          ws_enabled: false,
          last_error: "",
          has_ws: true,
        },
      ],
      logs: [
        {
          seq: 1,
          time: "12:00:00",
          level: "INFO",
          bot: "fairy__workAssistant",
          message: "bot=fairy__workAssistant ask from=Alice: work channel",
        },
        {
          seq: 2,
          time: "12:00:01",
          level: "INFO",
          bot: "fairy__devlopment",
          message: "bot=fairy__devlopment ask from=Bob: dev channel",
        },
        {
          seq: 3,
          time: "12:00:02",
          level: "INFO",
          bot: "fairy__workAssistant",
          message: "bot=fairy__workAssistant thinking: analyzing",
        },
        {
          seq: 4,
          time: "12:00:03",
          level: "INFO",
          bot: "fairy__workAssistant",
          message: "bot=fairy__workAssistant reply: done",
        },
        {
          seq: 5,
          time: "12:00:04",
          level: "INFO",
          message: "super_long_line_" + "x".repeat(240) + "_end",
        },
        ...Array.from({ length: 60 }, (_, i) => ({
          seq: 6 + i,
          time: "12:01:00",
          level: "INFO" as const,
          bot: "fairy__workAssistant",
          message: `bot=fairy__workAssistant filler ${i + 1} ` + "n".repeat(48),
        })),
      ],
      skills: [
        {
          id: "hello-workspace",
          name: "Hello Workspace",
          version: "1.0.0",
          description: "样例 Skill",
          dir: "C:\\\\Users\\\\mock\\\\.yzj-bridge\\\\skills\\\\hello-workspace",
        },
        {
          id: "kdlog",
          name: "KDLog",
          version: "1.0.0",
          description: "日志查询",
        },
      ] as { id: string; name: string; version?: string; description?: string; dir?: string }[],
      chatSessions: [
        {
          id: "chat-seed",
          title: "seed session",
          bot_id: "fairy",
          updated_at: "2026-08-12T00:00:00Z",
          messages: Array.from({ length: 20 }, (_, i) => ({
            role: i % 2 === 0 ? "user" : "assistant",
            content: `seed message ${i + 1} ` + "hello ".repeat(6),
            bot_id: "fairy",
            ts: "2026-08-12T00:00:00Z",
          })),
        },
      ] as {
        id: string;
        title: string;
        bot_id: string;
        updated_at: string;
        messages: { role: string; content: string; bot_id?: string; ts: string }[];
      }[],
      chatSeq: 0,
      revealed: [] as string[],
      opened: [] as string[],
    };

    const rebuildStatus = () => {
      const out: typeof state.status = [];
      for (const b of state.config.bots) {
        const id = String(b.id);
        const channels = Array.isArray(b.channels) ? (b.channels as Bot[]) : [];
        if (!channels.length) {
          out.push({
            id,
            role_id: id,
            name: String(b.name || id),
            group: String(b.group || "default"),
            backend: String(b.backend || "cursor_cli"),
            inbound_mode: String(b.inbound_mode || "websocket"),
            connected: id === "fairy",
            ws_enabled: true,
            last_error: "",
            has_ws: true,
          });
          continue;
        }
        for (const ch of channels) {
          const runtimeId =
            channels.length > 1 ? `${id}__${String(ch.group || "default")}` : id;
          out.push({
            id: runtimeId,
            role_id: id,
            name: String(b.name || id),
            group: String(ch.group || "default"),
            backend: String(b.backend || "cursor_cli"),
            inbound_mode: String(b.inbound_mode || "websocket"),
            connected: id === "fairy",
            ws_enabled: true,
            last_error: "",
            has_ws: true,
          });
        }
      }
      state.status = out;
    };

    // @ts-expect-error mock globals for browser
    window.__TAURI_INTERNALS__ = {
      invoke: async (cmd: string, args: Record<string, unknown> = {}) => {
        switch (cmd) {
          case "ensure_bridge":
            return null;
          case "get_autostart":
            return state.autostart;
          case "set_autostart":
            state.autostart = !!args.enabled;
            return state.autostart;
          case "reveal_path":
            state.revealed.push(String(args.path || ""));
            return null;
          case "open_path_default":
            state.opened.push(String(args.path || ""));
            return null;
          case "plugin:opener|open_path":
            state.opened.push(String(args.path || ""));
            return null;
          case "bridge_fetch": {
            const method = String(args.method || "GET");
            const path = String(args.path || "");
            const bodyRaw = args.body == null ? null : String(args.body);
            if (path.startsWith("/v1/status")) {
              return JSON.stringify({ bots: state.status });
            }
            if (path.startsWith("/v1/paths")) {
              return JSON.stringify({
                config: "C:\\\\Users\\\\mock\\\\.yzj-bridge\\\\config.yaml",
                data: "C:\\\\Users\\\\mock\\\\.yzj-bridge",
              });
            }
            if (path.startsWith("/v1/config") && method === "GET") {
              return JSON.stringify(state.config);
            }
            if (path.startsWith("/v1/config") && method === "PUT") {
              state.config = JSON.parse(bodyRaw || "{}");
              rebuildStatus();
              return JSON.stringify({ ok: true });
            }
            if (path.startsWith("/v1/reload")) {
              return JSON.stringify({ ok: true });
            }
            if (path.startsWith("/v1/wss/start")) {
              state.status = state.status.map((s) => ({ ...s, ws_enabled: true }));
              return JSON.stringify({ ok: true });
            }
            if (path.startsWith("/v1/wss/stop")) {
              state.status = state.status.map((s) => ({ ...s, ws_enabled: false }));
              return JSON.stringify({ ok: true });
            }
            if (path.startsWith("/v1/logs") && method === "POST") {
              const body = JSON.parse(bodyRaw || "{}") as { level?: string; message?: string };
              const msg = String(body.message || "").trim();
              state.seq += 1;
              state.logs.push({
                seq: state.seq,
                time: "12:00:99",
                level: String(body.level || "INFO").toUpperCase(),
                bot: "gui",
                message: msg,
              });
              return JSON.stringify({ ok: true });
            }
            if (path.startsWith("/v1/logs")) {
              const u = new URL(path, "http://local");
              const since = Number(u.searchParams.get("since_seq") || "0");
              const bot = u.searchParams.get("bot") || "";
              const lines = state.logs.filter((l) => {
                if (l.seq <= since) return false;
                if (!bot) return true;
                const id = l.bot || "";
                if (bot === "gui") return id === "gui";
                if (bot.includes("__")) return id === bot;
                return id === bot || id.startsWith(bot + "__");
              });
              return JSON.stringify({ lines });
            }
            if (path.startsWith("/v1/backends/cursor/models")) {
              return JSON.stringify({
                ok: true,
                models: [
                  { id: "composer-2", label: "Composer 2" },
                  { id: "gpt-5", label: "GPT-5" },
                ],
              });
            }
            if (path.startsWith("/v1/backends/claude/models")) {
              return JSON.stringify({
                ok: true,
                models: [
                  { id: "sonnet", label: "Sonnet (latest)" },
                  { id: "opus", label: "Opus (latest)" },
                  { id: "haiku", label: "Haiku (latest)" },
                ],
              });
            }
            if (path.startsWith("/v1/backends/openai/probe")) {
              return JSON.stringify({
                ok: true,
                latency_ms: 42,
                endpoint: "https://api.openai.com/v1/models",
                models: [
                  { id: "gpt-4o-mini", label: "gpt-4o-mini" },
                  { id: "gpt-4o", label: "gpt-4o" },
                ],
              });
            }
            if (path === "/v1/memory/enable-check" && method === "POST") {
              return JSON.stringify({ ok: true, reason: "ready", openai: { ok: true }, claude_ok: true });
            }
            if (path === "/v1/memory/profiles" && method === "GET") {
              return JSON.stringify({ profiles: [] });
            }
            if (path.startsWith("/v1/memory/profiles/")) {
              if (method === "DELETE") {
                if (!path.includes("confirm=1") && !(args.query as string | undefined)?.includes?.("confirm")) {
                  // path may include query in this mock — check raw path
                }
                const q = path.includes("?") ? path.slice(path.indexOf("?")) : "";
                if (!q.includes("confirm=1")) throw new Error("confirm=1 required");
                return JSON.stringify({ ok: true });
              }
              return JSON.stringify({ open_id: "mock", turn_count: 0 });
            }
            if (path.startsWith("/v1/skills/install") && method === "POST") {
              const body = JSON.parse(bodyRaw || "{}") as {
                source?: string;
                path?: string;
              };
              const id =
                (body.path ? String(body.path).split(/[/\\]/).pop()?.replace(/\.(zip|tgz|tar\.gz|md)$/i, "") : "") ||
                "imported";
              const name = id;
              if (!state.skills.some((s) => s.id === id)) {
                state.skills.push({
                  id,
                  name,
                  version: "1.0.0",
                  description: "installed in mock",
                });
              }
              return JSON.stringify({ ok: true, id, name });
            }
            if (path.startsWith("/v1/skills/") && method === "DELETE") {
              const id = decodeURIComponent(path.replace(/^\/v1\/skills\//, ""));
              state.skills = state.skills.filter((s) => s.id !== id);
              return JSON.stringify({ ok: true });
            }
            if (path.startsWith("/v1/skills/") && method === "GET") {
              const id = decodeURIComponent(path.replace(/^\/v1\/skills\//, ""));
              const sk = state.skills.find((s) => s.id === id);
              if (!sk) throw new Error(`skill not found: ${id}`);
              return JSON.stringify(sk);
            }
            if (path.startsWith("/v1/skills")) {
              return JSON.stringify({ skills: state.skills });
            }
            if (path === "/v1/chat/sessions" && method === "GET") {
              return JSON.stringify({
                sessions: state.chatSessions.map((s) => ({
                  id: s.id,
                  title: s.title,
                  bot_id: s.bot_id,
                  updated_at: s.updated_at,
                  message_count: s.messages.length,
                })),
              });
            }
            if (path === "/v1/chat/sessions" && method === "POST") {
              const body = JSON.parse(bodyRaw || "{}") as { bot_id?: string; title?: string };
              state.chatSeq += 1;
              const sess = {
                id: `chat-${state.chatSeq}`,
                title: body.title || "",
                bot_id: body.bot_id || "",
                updated_at: new Date().toISOString(),
                messages: [] as { role: string; content: string; bot_id?: string; ts: string }[],
              };
              state.chatSessions.unshift(sess);
              return JSON.stringify(sess);
            }
            if (path.startsWith("/v1/chat/sessions/")) {
              const rest = path.slice("/v1/chat/sessions/".length);
              const [id, sub] = rest.split("/");
              const sess = state.chatSessions.find((s) => s.id === id);
              if (!sess) throw new Error("session not found");
              if (!sub && method === "GET") return JSON.stringify(sess);
              if (!sub && method === "PATCH") {
                const body = JSON.parse(bodyRaw || "{}") as {
                  bot_id?: string;
                  title?: string;
                  bound_open_id?: string;
                };
                if (body.bot_id) sess.bot_id = body.bot_id;
                if (body.title) sess.title = body.title;
                if (body.bound_open_id !== undefined) {
                  (sess as { bound_open_id?: string }).bound_open_id = body.bound_open_id;
                }
                sess.updated_at = new Date().toISOString();
                return JSON.stringify(sess);
              }
              if (!sub && method === "DELETE") {
                state.chatSessions = state.chatSessions.filter((s) => s.id !== id);
                return JSON.stringify({ ok: true });
              }
              if (sub === "messages" && method === "POST") {
                const body = JSON.parse(bodyRaw || "{}") as { content?: string };
                const content = String(body.content || "").trim();
                if (!content) throw new Error("content required");
                const ts = new Date().toISOString();
                sess.messages.push({ role: "user", content, bot_id: sess.bot_id, ts });
                sess.messages.push({
                  role: "assistant",
                  content: `mock-reply: ${content}`,
                  bot_id: sess.bot_id,
                  ts,
                });
                if (!sess.title) sess.title = content.slice(0, 40);
                sess.updated_at = ts;
                return JSON.stringify({
                  reply: `mock-reply: ${content}`,
                  handler_bot_id: sess.bot_id,
                  receive_bot_id: sess.bot_id,
                  session: sess,
                });
              }
            }
            return JSON.stringify({ ok: true });
          }
          case "get_close_to_tray":
            return (state as any).closeToTray !== false;
          case "get_app_version":
            return "0.2.6";
          case "set_close_to_tray":
            (state as any).closeToTray = !!args.enabled;
            return (state as any).closeToTray;
          default:
            throw new Error(`unknown mock command: ${cmd}`);
        }
      },
      transformCallback: () => 0,
      unregisterCallback: () => undefined,
    };

    // @ts-expect-error expose for assertions
    window.__E2E_STATE__ = state;
  });
}

test.beforeEach(async ({ page }) => {
  await installTauriMock(page);
  await page.goto("/");
  await expect(page.getByTestId("app-root")).toBeVisible();
});

test("系统页：主题下拉可读、热重载按钮、路径可点", async ({ page }) => {
  await page.getByTestId("nav-system").click();
  await expect(page.getByTestId("page-system")).toBeVisible();
  await expect(page.getByTestId("bridge-summary")).toContainText("通道");
  await page.getByTestId("theme-select").locator(".fancy-select-trigger").click();
  const menu = page.getByTestId("theme-select-menu");
  await expect(menu).toBeVisible();
  await expect(menu.getByRole("option", { name: "冰蓝白" })).toBeVisible();
  await expect(menu.getByRole("option", { name: "午夜蓝" })).toBeVisible();
  // custom menu must use theme panel, not white native popup with white text
  const color = await menu.getByRole("option", { name: "午夜蓝" }).evaluate((el) => {
    const menuBg = getComputedStyle(el.parentElement as HTMLElement).backgroundColor;
    const text = getComputedStyle(el).color;
    return { text, menuBg };
  });
  // ice theme: light panel + dark text
  const tm = color.text.match(/rgb\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)/);
  expect(tm).toBeTruthy();
  const textLum = (Number(tm![1]) + Number(tm![2]) + Number(tm![3])) / 3;
  expect(textLum).toBeLessThan(120);
  await menu.getByRole("option", { name: "午夜蓝" }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "midnight");
  await page.getByTestId("theme-select").locator(".fancy-select-trigger").click();
  await page.getByTestId("theme-select-menu").getByRole("option", { name: "冰蓝白" }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "ice");

  const reload = page.getByTestId("reload-btn");
  await reload.click();
  await expect(reload).toContainText("重载中");
  await expect(reload).toContainText("立即重载", { timeout: 3000 });

  await page.getByTestId("open-config-path").click();
  const revealed = await page.evaluate(() => (window as any).__E2E_STATE__.revealed);
  expect(revealed.length).toBeGreaterThan(0);

  const system = page.getByTestId("page-system");
  await expect(system).toContainText("开机自启");
  await expect(system).not.toContainText("Run 注册表");
  await expect(system).not.toContainText("无控制台闪烁");
  await expect(system).toContainText("从磁盘重新加载 config.yaml");
  await expect(system).not.toContainText("WSS 启停状态");
  await expect(page.getByTestId("app-version")).toHaveText("v0.2.6");
});

test("系统页：关闭到托盘开关", async ({ page }) => {
  await page.getByTestId("nav-system").click();
  const sw = page.getByTestId("close-to-tray-switch");
  await expect(sw).toHaveAttribute("aria-checked", "true");
  await sw.click();
  await expect(sw).toHaveAttribute("aria-checked", "false");
});

test("AI 设置：Cursor 模型下拉与 OpenAI 连通性", async ({ page }) => {
  await page.getByTestId("nav-settings").click();
  await expect(page.getByRole("heading", { name: "AI 设置" })).toBeVisible();
  // Opening AI settings auto-fetches cursor/claude models once when bins are set.
  await page.getByTestId("cursor-model").locator(".fancy-select-trigger").click();
  const cursorMenu = page.getByTestId("cursor-model-menu");
  await expect(cursorMenu).toBeVisible();
  await expect(page.getByTestId("cursor-model-search")).toBeVisible();
  await page.getByTestId("cursor-model-search").fill("comp");
  await expect(cursorMenu.getByRole("option", { name: "Composer 2" })).toBeVisible();
  await expect(cursorMenu.getByRole("option", { name: "GPT-5" })).toHaveCount(0);
  await cursorMenu.getByRole("option", { name: "Composer 2" }).click();
  await expect(page.getByTestId("cursor-model-menu")).toHaveCount(0);

  await page.getByTestId("claude-model").locator(".fancy-select-trigger").click();
  await expect(
    page.getByTestId("claude-model-menu").getByRole("option", { name: "Sonnet (latest)" }),
  ).toBeVisible();
  await page.keyboard.press("Escape");

  // Filling Base URL + API Key on the settings page triggers one auto probe.
  await page.getByTestId("openai-base-url-global").fill("https://api.openai.com/v1");
  await page.getByTestId("openai-api-key-global").fill("sk-test");
  await expect(page.getByTestId("openai-probe-info")).toContainText("连通成功");
  await expect(page.getByTestId("openai-probe-info")).toHaveClass(/ok/);
  await page.getByTestId("openai-model-global").locator(".fancy-select-trigger").click();
  await expect(
    page.getByTestId("openai-model-global-menu").getByRole("option", { name: "gpt-4o-mini" }),
  ).toBeVisible();
});

test("帮助页含简介与 GitHub 入口", async ({ page }) => {
  await page.getByTestId("nav-help").click();
  await expect(page.getByTestId("page-help")).toBeVisible();
  await expect(page.getByText("YZJ Bridge 是什么")).toBeVisible();
  await expect(page.getByTestId("help-flow")).toContainText("配置后端引擎");
  await expect(page.getByTestId("help-flow")).toContainText("send_msg_url");
  await expect(page.getByTestId("developer-profile")).toBeVisible();
  await expect(page.getByTestId("developer-profile").locator("img")).toBeVisible();
  const link = page.getByTestId("github-link");
  await expect(link).toBeVisible();
  await expect(link).toHaveAttribute("href", "https://github.com/JinRyu-online/yzj_bot_bridge");
  await expect(page.getByTestId("page-help")).not.toContainText("配置目录");
  await expect(page.getByTestId("page-help")).not.toContainText("控制 API");
  await expect(page.getByTestId("page-help")).not.toContainText("独立开发者");
  await expect(page.getByTestId("page-help")).toContainText(
    "源码、Issue 与发布说明开源在 GitHub，欢迎 Star 与贡献。",
  );
});

test("保存设置写入 GUI 运行日志", async ({ page }) => {
  await page.getByTestId("nav-settings").click();
  await page.getByTestId("save-settings").click();
  await page.getByTestId("nav-logs").click();
  await page.getByTestId("log-bot-select").locator(".fancy-select-trigger").click();
  await page.getByTestId("log-bot-select-menu").getByRole("option", { name: "GUI 操作" }).click();
  await expect(page.getByTestId("logbox")).toContainText("保存 AI 设置成功");
  await expect(page.getByTestId("logbox").locator(".log-tag").filter({ hasText: "GUI" }).first()).toBeVisible();
  await expect(page.getByTestId("logbox")).not.toContainText("[GUI]");
});

test("启动遮罩在桥就绪后消失", async ({ page }) => {
  await expect(page.getByTestId("boot-overlay")).toHaveCount(0);
  await expect(page.getByTestId("bridge-status")).toContainText("桥已连接");
});

test("桥启动失败时显示错误而非无限转圈", async ({ page }) => {
  await page.addInitScript(() => {
    const internals = (window as any).__TAURI_INTERNALS__;
    if (!internals) return;
    const orig = internals.invoke.bind(internals);
    internals.invoke = async (cmd: string, args?: Record<string, unknown>) => {
      if (cmd === "ensure_bridge") {
        throw new Error("桥进程启动超时，请检查 yzj-bridge.exe 与配置");
      }
      return orig(cmd, args);
    };
  });
  await page.reload();
  await expect(page.getByTestId("boot-overlay")).toBeVisible({ timeout: 60000 });
  await expect(page.getByTestId("boot-error")).toContainText("桥进程启动超时");
  await expect(page.getByTestId("boot-overlay")).toContainText("桥接服务启动失败");
  await expect(page.getByTestId("boot-overlay").locator(".spinner")).toHaveCount(0);
});

test("聊天菜单在首位且默认仍是机器人页", async ({ page }) => {
  await expect(page.getByTestId("page-bots")).toBeVisible();
  const nav = page.locator("aside .nav-btn");
  await expect(nav.first()).toHaveAttribute("data-testid", "nav-chat");
  await expect(nav.first()).toHaveText("聊天");
  await expect(page.getByTestId("nav-bots")).toBeVisible();
  await page.getByTestId("nav-chat").click();
  await expect(page.getByTestId("page-chat")).toBeVisible();
  await expect(page.getByTestId("chat-input")).toBeVisible();
  await expect(page.getByTestId("chat-input")).toHaveAttribute(
    "placeholder",
    "输入消息… 使用 @ 指定机器人",
  );
  await expect(page.getByTestId("chat-history")).toBeVisible();
  await page.getByTestId("chat-new").click();
  await expect(page.getByTestId("chat-input")).toBeEnabled();
  await page.getByTestId("chat-bot-trigger").click();
  await expect(page.getByTestId("chat-bot-menu")).toBeVisible();
});

test("弹窗 X 关闭 + OpenAI 字段", async ({ page }) => {
  await page.getByTestId("nav-bots").click();
  await page.getByTestId("create-bot").click();
  await expect(page.getByTestId("bot-modal")).toBeVisible();
  await page.getByTestId("bot-backend").locator(".fancy-select-trigger").click();
  await page.getByTestId("bot-backend-menu").getByRole("option", { name: "openai" }).click();
  await expect(page.getByTestId("openai-use-defaults")).toBeVisible();
  await expect(page.getByTestId("openai-use-defaults")).toHaveAttribute("aria-checked", "true");
  await page.getByTestId("openai-use-defaults").click();
  await expect(page.getByTestId("openai-use-defaults")).toHaveAttribute("aria-checked", "false");
  await expect(page.getByTestId("openai-base-url")).toBeVisible();
  await expect(page.getByTestId("openai-api-key")).toBeVisible();
  await expect(page.getByTestId("openai-model")).toBeVisible();
  await page.getByTestId("save-bot").click();
  await expect(page.getByTestId("bot-modal-error")).toBeVisible();
  await page.getByTestId("bot-modal-close").click();
  await expect(page.getByTestId("bot-modal")).toHaveCount(0);
});

test("Skills 页入口与导入区", async ({ page }) => {
  await page.getByTestId("nav-skills").click();
  await expect(page.getByTestId("page-skills")).toBeVisible();
  await expect(page.getByTestId("skills-installed")).toBeVisible();
  await expect(page.getByTestId("skills-import")).toBeVisible();
  await expect(page.getByTestId("skills-installed").getByText("Hello Workspace")).toBeVisible();
  await expect(page.getByTestId("skill-browse-dir")).toBeVisible();
  await expect(page.getByTestId("skills-catalog")).toHaveCount(0);
  await page.getByTestId("skill-open-hello-workspace").click();
  const opened = await page.evaluate(() => (window as any).__E2E_STATE__.opened);
  expect(opened.some((p: string) => p.includes("hello-workspace"))).toBeTruthy();
});

test("新建机器人可见 Skills 勾选区", async ({ page }) => {
  await page.getByTestId("nav-bots").click();
  await page.getByTestId("create-bot").click();
  await expect(page.getByTestId("bot-modal")).toBeVisible();
  await expect(page.getByTestId("bot-skills")).toBeVisible();
  await page.getByTestId("bot-modal-close").click();
});

test("机器人摘要显示已配置 Skills 与系统提示词", async ({ page }) => {
  await page.getByTestId("nav-bots").click();
  await page.getByTestId("role-youkai").click();
  const summary = page.getByTestId("bot-skills-summary");
  await expect(summary).toContainText("Hello Workspace (hello-workspace)");
  await expect(summary).toContainText("KDLog (kdlog)");
  const prompt = page.getByTestId("bot-system-prompt-summary");
  await expect(prompt).toBeVisible();
  await expect(prompt).toContainText("你是 Youkai");
});

test("机器人排序稳定", async ({ page }) => {
  await page.getByTestId("nav-bots").click();
  const first = async () =>
    page.getByTestId("role-rail").locator(".role-card").first().locator("strong").innerText();
  const a = await first();
  await page.waitForTimeout(2500);
  const b = await first();
  expect(a).toBe(b);
  expect(a).toBe("Fairy");
});

test("设置页分组、密钥小眼睛、OpenAI 模型", async ({ page }) => {
  await page.getByTestId("nav-settings").click();
  await expect(page.getByTestId("page-settings")).toBeVisible();
  await expect(page.getByTestId("nav-settings")).toHaveText("AI 设置");
  await expect(page.getByTestId("group-cursor")).toBeVisible();
  await expect(page.getByTestId("group-claude")).toBeVisible();
  await expect(page.getByTestId("group-openai")).toBeVisible();
  await expect(page.getByTestId("group-dirs")).toBeVisible();
  await expect(page.getByTestId("cursor-bin")).toHaveValue("agent");
  await expect(page.getByTestId("claude-bin")).toHaveValue("claude");
  await expect(page.getByTestId("openai-model-global")).toBeVisible();
  const key = page.getByTestId("cursor-api-key");
  await key.fill("secret-key");
  await expect(key).toHaveAttribute("type", "password");
  await page.getByTestId("cursor-api-key-toggle").click();
  await expect(key).toHaveAttribute("type", "text");
  await expect(key).toHaveValue("secret-key");
});

test("主题下拉冰蓝白排第一", async ({ page }) => {
  await page.getByTestId("nav-system").click();
  await page.getByTestId("theme-select").locator(".fancy-select-trigger").click();
  const first = page.getByTestId("theme-select-menu").locator(".fancy-option").first();
  await expect(first).toHaveText("冰蓝白");
});

test("侧栏菜单带统一图标", async ({ page }) => {
  await expect(page.getByTestId("brand-mark")).toBeVisible();
  for (const id of ["chat", "bots", "settings", "skills", "memory", "logs", "help", "system"]) {
    await expect(page.getByTestId(`nav-${id}`).locator(".nav-icon")).toBeVisible();
  }
});

test("记忆页入口与默认关", async ({ page }) => {
  await page.getByTestId("nav-memory").click();
  await expect(page.getByTestId("page-memory")).toBeVisible();
  await expect(page.getByTestId("memory-list")).toBeVisible();
  await expect(page.getByTestId("memory-list")).not.toContainText(".jsonl");
  await expect(page.getByTestId("memory-bot-filter")).toBeVisible();
  await expect(page.getByTestId("memory-detail")).toContainText("尚未选择用户");

  await page.getByTestId("nav-settings").click();
  await expect(page.getByTestId("memory-enabled-switch")).toBeVisible();
  // Switch unchecked by default
  const sw = page.getByTestId("memory-enabled-switch");
  await expect(sw).toHaveAttribute("aria-checked", "false");

  await page.getByTestId("nav-chat").click();
  await expect(page.getByTestId("chat-bind-openid")).toHaveCount(0);
});

test("运行日志：进入时在底部，滚动条仅滚动时浮现", async ({ page }) => {
  await page.getByTestId("nav-logs").click();
  const box = page.getByTestId("logbox");
  await expect(box).toBeVisible();
  await expect(box).toContainText("filler 60");
  await expect.poll(async () =>
    box.evaluate((el) => el.scrollHeight > el.clientHeight + 8),
  ).toBeTruthy();
  await expect.poll(async () =>
    box.evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight < 24),
  ).toBeTruthy();
  const minimap = page.getByTestId("log-minimap");
  await expect(minimap).toHaveCount(1);
  await expect(minimap).toHaveAttribute("data-visible", "0");
  await expect(minimap).not.toHaveClass(/peek/);
  // peek 隐藏带 0.2s opacity 过渡，瞬时取样会得到 0.17~0.3（贴底 scroll 偶发 keepPeek）。
  await expect.poll(async () => {
    const s = await minimap.evaluate((el) => getComputedStyle(el).opacity);
    return Number(s);
  }).toBeLessThan(0.05);
  await expect(page.getByTestId("log-scroll-bottom")).toHaveCount(0);
  await box.evaluate((el) => {
    el.dispatchEvent(new WheelEvent("wheel", { deltaY: -240, bubbles: true }));
    el.scrollTop = Math.max(0, el.scrollTop - 240);
    el.dispatchEvent(new Event("scroll", { bubbles: true }));
  });
  await expect(minimap).toHaveAttribute("data-visible", "1");
  await expect(minimap).toHaveClass(/peek/);
  await expect(page.getByTestId("log-scroll-bottom")).toBeVisible();
  await page.getByTestId("log-scroll-bottom").click();
  await expect(page.getByTestId("log-scroll-bottom")).toHaveCount(0);
});

test("聊天页上滑后出现滚到底部按钮", async ({ page }) => {
  await page.getByTestId("nav-chat").click();
  await expect(page.getByTestId("page-chat")).toBeVisible();
  const messages = page.getByTestId("chat-messages");
  await expect(messages).toContainText("seed message");
  await messages.evaluate((el) => {
    (el as HTMLElement).style.maxHeight = "120px";
    (el as HTMLElement).style.height = "120px";
  });
  await messages.evaluate((el) => {
    el.scrollTop = 0;
    el.dispatchEvent(new Event("scroll", { bubbles: true }));
  });
  await expect(page.getByTestId("chat-scroll-bottom")).toBeVisible();
  await page.getByTestId("chat-scroll-bottom").click();
  await expect(page.getByTestId("chat-scroll-bottom")).toHaveCount(0);
});

test("通道级日志过滤互不串扰", async ({ page }) => {
  await page.getByTestId("nav-logs").click();
  await page.getByTestId("log-bot-select").locator(".fancy-select-trigger").click();
  await page.getByTestId("log-bot-select-menu").getByRole("option", { name: "fairy__workAssistant" }).click();
  const box = page.getByTestId("logbox");
  await expect(box.locator(".log-tag").first()).toHaveText("fairy__workAssistant");
  await expect(box).toContainText("ask from=Alice");
  await expect(box).not.toContainText("bot=fairy__workAssistant");
  await expect(box).not.toContainText("fairy__devlopment");
});

test("默认主题冰蓝白且输入框非深色底", async ({ page }) => {
  await expect(page.locator("html")).toHaveAttribute("data-theme", "ice");
  await page.getByTestId("nav-settings").click();
  const style = await page.getByTestId("cursor-bin").evaluate((el) => {
    const s = getComputedStyle(el);
    return { bg: s.backgroundColor, image: s.backgroundImage, color: s.color };
  });
  // ice inputs use light gradient / pale blue, not flat near-black fill
  const looksLightGradient = /linear-gradient/i.test(style.image) || /rgb\(\s*2\d{2}/.test(style.bg);
  expect(looksLightGradient).toBeTruthy();
  const cm = style.color.match(/rgb\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)/);
  expect(cm).toBeTruthy();
  const textLum = (Number(cm![1]) + Number(cm![2]) + Number(cm![3])) / 3;
  expect(textLum).toBeLessThan(100);
});

test("日志问答内容、无双时间戳、下拉不超宽", async ({ page }) => {
  await page.getByTestId("nav-logs").click();
  const box = page.getByTestId("logbox");
  await expect(box).toBeVisible();
  await expect(box).toContainText("ask from=");
  await expect(box).toContainText("thinking:");
  await expect(box).toContainText("reply:");
  const text = await box.innerText();
  // message itself should not start with another go-log timestamp like 2026/01/02
  expect(text).not.toMatch(/\d{4}\/\d{2}\/\d{2} \d{2}:\d{2}:\d{2}/);

  const metrics = await box.evaluate((el) => ({
    clientWidth: el.clientWidth,
    scrollWidth: el.scrollWidth,
    overflowX: getComputedStyle(el).overflowX,
  }));
  expect(metrics.overflowX === "hidden" || metrics.overflowX === "clip").toBeTruthy();
  expect(metrics.scrollWidth).toBeLessThanOrEqual(metrics.clientWidth + 2);

  const select = page.getByTestId("log-bot-select");
  const width = await select.evaluate((el) => el.getBoundingClientRect().width);
  expect(width).toBeLessThanOrEqual(200);
});
