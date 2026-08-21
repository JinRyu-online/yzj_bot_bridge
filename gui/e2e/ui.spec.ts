import { test, expect, type Page } from "@playwright/test";

async function installTauriMock(
  page: Page,
  opts?: {
    updateCheck?: Record<string, unknown>;
    updateCheckThrow?: string;
    updateDownloadDelayMs?: number;
  },
) {
  await page.addInitScript((boot) => {
    localStorage.setItem("yzj-theme", "ice");
    type Bot = Record<string, unknown>;
    const state = {
      autostart: false,
      seq: 65,
      updateCheck: boot.updateCheck || null,
      updateCheckThrow: boot.updateCheckThrow || "",
      updateDownloadDelayMs: boot.updateDownloadDelayMs || 0,
      updateCheckCalls: [] as { force: boolean }[],
      skippedUpdateVersion: "",
      updateLaunched: "",
      updateDownloadThrow: "",
      eventHandlers: {} as Record<string, ((e: { payload?: unknown }) => void) | null>,
      eventSeq: 0,
      _cbSeq: 0,
      _cbMap: new Map<number, unknown>(),
      memoryProfiles: [] as any[],
      /** 测试辅助：向监听 update-download-progress 的前端推送进度事件。 */
      __emitUpdateProgress: (payload: unknown) => {
        const h = (state as any).eventHandlers["update-download-progress"];
        if (h) h({ payload });
      },
      config: {
        defaults: {
          cursor_bin: "agent",
          claude_bin: "claude",
          cursor_api_key: "",
          openai_api_key: "",
          openai_base_url: "",
          projects_root: "~",
          session_mode: "shared",
          cursor_model: "",
          node_bin: "node",
          dsh_entry: "",
          dsh_profile: "jsonrpc",
          dsh_provider: "kuaidi100",
          dsh_model: "",
          dsh_timeout: 600,
          dsh_ttl_seconds: 300,
          dsh_max_warm: 3,
          dsh_home: "",
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
          case "open_cli_install_terminal":
            state.opened.push(`install:${String(args.engine || "")}`);
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
            if (path.startsWith("/v1/backends/dsh/models")) {
              return JSON.stringify({
                ok: true,
                models: [
                  { id: "deepseek-v4-flash", label: "DeepSeek-V4-Flash" },
                  { id: "deepseek-v4-pro", label: "DeepSeek-V4-Pro" },
                ],
              });
            }
            if (path.startsWith("/v1/backends/available")) {
              return JSON.stringify({
                backends: [
                  { id: "cursor_cli", label: "Cursor CLI", available: true },
                  { id: "claude_code", label: "Claude Code", available: false, reason: "未找到可执行文件" },
                  { id: "openai", label: "OpenAI 兼容", available: true },
                  { id: "dsh", label: "DSH（DeepSeek Harness）", available: true },
                  { id: "opencode", label: "OpenCode", available: false, reason: "占位后端，尚未实现" },
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
            if (path.startsWith("/v1/backends/cli/discover")) {
              const body = JSON.parse(bodyRaw || "{}") as { engine?: string };
              const engine = String(body.engine || "cursor");
              if (engine === "claude") {
                return JSON.stringify({
                  engine: "claude",
                  found: false,
                  message: "未在 PATH 或常见安装目录找到，可一键打开终端安装",
                  install: {
                    shell: "powershell",
                    command: "irm https://claude.ai/install.ps1 | iex",
                    hint: "将打开 PowerShell，确认后执行 Claude Code 官方安装脚本",
                  },
                });
              }
              if (engine === "dsh") {
                return JSON.stringify({
                  engine: "dsh",
                  found: true,
                  path: "C:\\\\Users\\\\mock\\\\AppData\\\\Local\\\\dsh\\\\bin.js",
                  version: "0.1.0",
                  message: "已找到可执行文件",
                  install: {
                    shell: "powershell",
                    command: "npm i -g @deepseek/dsh",
                    hint: "将打开 PowerShell，确认后执行 DSH 安装命令",
                  },
                });
              }
              if (engine === "node") {
                return JSON.stringify({
                  engine: "node",
                  found: true,
                  path: "C:\\\\Program Files\\\\nodejs\\\\node.exe",
                  version: "v24.14.0",
                  message: "已找到可执行文件",
                  install: {
                    shell: "powershell",
                    command: "winget install OpenJS.NodeJS.LTS",
                    hint: "将打开 PowerShell，确认后执行 Node 安装命令",
                  },
                });
              }
              return JSON.stringify({
                engine: "cursor",
                found: true,
                path: "C:\\\\Users\\\\mock\\\\AppData\\\\Local\\\\cursor-agent\\\\agent.exe",
                version: "2026.08.11",
                message: "已找到可执行文件",
                install: {
                  shell: "powershell",
                  command: "irm 'https://cursor.com/install?win32=true' | iex",
                  hint: "将打开 PowerShell，确认后执行 Cursor 官方安装脚本",
                },
              });
            }
            if (path === "/v1/memory/enable-check" && method === "POST") {
              return JSON.stringify({ ok: true, reason: "ready", openai: { ok: true }, claude_ok: true });
            }
            if (path === "/v1/memory/profiles" && method === "GET") {
              return JSON.stringify({
                profiles: (state as any).memoryProfiles || [],
              });
            }
            if (path.startsWith("/v1/memory/profiles/")) {
              const rest = path.slice("/v1/memory/profiles/".length);
              const [oid, sub] = rest.split("/");
              const profs: any[] = (state as any).memoryProfiles || [];
              if (method === "DELETE") {
                const q = path.includes("?") ? path.slice(path.indexOf("?")) : "";
                if (!q.includes("confirm=1")) throw new Error("confirm=1 required");
                (state as any).memoryProfiles = profs.filter((p) => p.open_id !== oid);
                return JSON.stringify({ ok: true });
              }
              if (sub === "lock" && method === "POST") {
                const body = JSON.parse(bodyRaw || "{}") as { fields?: Record<string, boolean> };
                const p = profs.find((x) => x.open_id === oid);
                if (p) {
                  for (const [k, v] of Object.entries(body.fields || {})) {
                    if (p[k] && typeof p[k] === "object") p[k].locked = v;
                  }
                  return JSON.stringify(p);
                }
                return JSON.stringify({ open_id: oid, turn_count: 0 });
              }
              if (sub === "reset-inferred" && method === "POST") {
                const p = profs.find((x) => x.open_id === oid);
                if (p) {
                  for (const k of ["how_to_address", "role", "ask_style", "reply_style", "notes"]) {
                    if (p[k] && typeof p[k] === "object") delete p[k].inferred;
                  }
                  if (p.donts) delete p.donts.inferred;
                  return JSON.stringify(p);
                }
                return JSON.stringify({ open_id: oid, turn_count: 0 });
              }
              if (method === "PATCH") {
                const body = JSON.parse(bodyRaw || "{}") as Record<string, unknown>;
                const p = profs.find((x) => x.open_id === oid);
                const target = p || { open_id: oid, turn_count: 0 };
                if (body.display_name !== undefined) target.display_name = body.display_name;
                for (const k of ["how_to_address", "role", "ask_style", "reply_style", "notes"]) {
                  const v = body[k] as { manual?: string } | undefined;
                  if (v === undefined) continue;
                  target[k] = {
                    ...(target[k] || {}),
                    manual: v.manual || "",
                  };
                }
                if (body.donts) {
                  target.donts = {
                    ...(target.donts || {}),
                    manual: (body.donts as { manual?: string[] }).manual || [],
                  };
                }
                if (!p) (state as any).memoryProfiles = [target, ...profs];
                return JSON.stringify(target);
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
          case "check_for_update": {
            const force = !!(args as { force?: boolean }).force;
            (state as any).updateCheckCalls = [
              ...((state as any).updateCheckCalls || []),
              { force },
            ];
            if ((state as any).updateCheckThrow) {
              throw new Error(String((state as any).updateCheckThrow));
            }
            const mock = (state as any).updateCheck as
              | {
                  available?: boolean;
                  currentVersion?: string;
                  latestVersion?: string;
                  notes?: string;
                  downloadUrl?: string;
                  publishedAt?: string;
                  skipped?: boolean;
                  message?: string;
                }
              | undefined;
            if (mock) {
              const skipped = !!mock.skipped;
              const availableBase = !!mock.available;
              const available = availableBase && (force || !skipped);
              return {
                available,
                currentVersion: mock.currentVersion || "0.2.6",
                latestVersion: mock.latestVersion || "0.2.6",
                notes: mock.notes || "",
                downloadUrl: mock.downloadUrl || "",
                publishedAt: mock.publishedAt || "",
                skipped,
                message: mock.message || "",
              };
            }
            return {
              available: false,
              currentVersion: "0.2.6",
              latestVersion: "0.2.6",
              notes: "",
              downloadUrl: "",
              publishedAt: "",
              skipped: false,
              message: "",
            };
          }
          case "set_skipped_update_version": {
            const version = String((args as { version?: string }).version || "");
            (state as any).skippedUpdateVersion = version;
            if ((state as any).updateCheck) {
              (state as any).updateCheck.skipped = true;
            }
            return null;
          }
          case "start_update_download": {
            if ((state as any).updateDownloadThrow) {
              throw new Error(String((state as any).updateDownloadThrow));
            }
            (state as any).updateLaunched = String(
              (args as { downloadUrl?: string }).downloadUrl || "",
            );
            // 后台下载：命令立即返回，进度通过事件推送。
            const total = 10 * 1024 * 1024;
            const payload = (received: number, phase = "downloading") => ({
              received,
              total,
              phase,
              error: "",
            });
            (state as any).__emitUpdateProgress(payload(0));
            return null;
          }
          case "plugin:event|listen": {
            const evt = String((args as { event?: string }).event || "");
            const handlerId = Number((args as { handler?: unknown }).handler) || 0;
            const handler = (state as any)._cbMap.get(handlerId) || null;
            (state as any).eventHandlers[evt] = handler;
            // eventId 由 mock 内部维护，unlisten 时用相同值即可。
            return String((state as any).eventSeq);
          }
          case "plugin:event|unlisten": {
            const evt = String((args as { event?: string }).event || "");
            delete (state as any).eventHandlers[evt];
            return null;
          }
          default:
            throw new Error(`unknown mock command: ${cmd}`);
        }
      },
      transformCallback: (cb: unknown) => {
        (state as any)._cbSeq += 1;
        const id = (state as any)._cbSeq;
        (state as any)._cbMap.set(id, cb);
        return id;
      },
      unregisterCallback: () => undefined,
    };

    // Tauri event plugin internals：前端 _unlisten 会调用这里，必须存在。
    // @ts-expect-error mock globals for browser
    window.__TAURI_EVENT_PLUGIN_INTERNALS__ = {
      registerListener: () => undefined,
      unregisterListener: () => undefined,
    };

    // @ts-expect-error expose for assertions
    window.__E2E_STATE__ = state;
  }, {
    updateCheck: opts?.updateCheck || null,
    updateCheckThrow: opts?.updateCheckThrow || "",
    updateDownloadDelayMs: opts?.updateDownloadDelayMs || 0,
  });
}

const SAMPLE_UPDATE = {
  available: true,
  currentVersion: "0.2.6",
  latestVersion: "0.3.0",
  notes: "## 变更\n\n- 新增自动更新\n- 修复若干问题",
  downloadUrl:
    "https://github.com/JinRyu-online/yzj_bot_bridge/releases/download/v0.3.0/YZJBridge-0.3.0-Windows-x64-setup.exe",
  publishedAt: "2026-08-21T00:00:00Z",
  skipped: false,
  message: "",
};

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
  await expect(page.getByTestId("check-update-btn")).toBeVisible();
  await page.getByTestId("check-update-btn").click();
  await expect(page.getByTestId("save-toast").filter({ hasText: "已是最新" })).toBeVisible();
});

test("更新：启动自动检测有新版本时弹窗", async ({ page }) => {
  test.setTimeout(60_000);
  await installTauriMock(page, { updateCheck: SAMPLE_UPDATE });
  await page.goto("/");
  await expect(page.getByTestId("app-root")).toBeVisible();
  await expect(page.getByTestId("update-modal")).toBeVisible();
  await expect(page.getByTestId("update-modal")).toContainText("发现新版本 v0.3.0");
  const calls = await page.evaluate(() => (window as any).__E2E_STATE__.updateCheckCalls);
  expect(calls.some((c: { force: boolean }) => c.force === false)).toBeTruthy();
});

test("更新：启动检测失败时静默（无弹窗无 toast）", async ({ page }) => {
  await installTauriMock(page, { updateCheckThrow: "network down" });
  await page.goto("/");
  await expect(page.getByTestId("app-root")).toBeVisible();
  await expect(page.getByTestId("update-modal")).toHaveCount(0);
  await expect(page.getByTestId("save-toast")).toHaveCount(0);
  // 手动检查仍应提示失败
  await page.getByTestId("nav-system").click();
  await page.getByTestId("check-update-btn").click();
  await expect(page.getByTestId("save-toast").filter({ hasText: "检查更新失败" })).toBeVisible();
});

test("系统页：发现更新时弹窗展示日志并可确认", async ({ page }) => {
  await page.evaluate((u) => {
    (window as any).__E2E_STATE__.updateCheck = u;
  }, SAMPLE_UPDATE);
  await page.getByTestId("nav-system").click();
  await page.getByTestId("check-update-btn").click();
  const modal = page.getByTestId("update-modal");
  await expect(modal).toBeVisible();
  await expect(modal).toContainText("发现新版本 v0.3.0");
  await expect(page.getByTestId("update-notes")).toContainText("新增自动更新");
  await expect(page.getByTestId("update-later")).toBeVisible();
  await expect(page.getByTestId("update-skip")).toBeVisible();
  await expect(page.getByTestId("update-confirm")).toBeVisible();
  await page.getByTestId("update-confirm").click();
  // 后台下载：命令立即返回，弹窗关闭，左下角出现进度条。
  await expect(page.getByTestId("update-modal")).toHaveCount(0);
  await expect(page.getByTestId("update-dl-bar")).toBeVisible();
  const launched = await page.evaluate(() => (window as any).__E2E_STATE__.updateLaunched);
  expect(launched).toContain("YZJBridge-0.3.0-Windows-x64-setup.exe");
});

test("更新：稍后提醒不持久化跳过", async ({ page }) => {
  await page.evaluate((u) => {
    (window as any).__E2E_STATE__.updateCheck = { ...u };
  }, SAMPLE_UPDATE);
  await page.getByTestId("nav-system").click();
  await page.getByTestId("check-update-btn").click();
  await expect(page.getByTestId("update-modal")).toBeVisible();
  await page.getByTestId("update-later").click();
  await expect(page.getByTestId("update-modal")).toHaveCount(0);
  const skipped = await page.evaluate(() => (window as any).__E2E_STATE__.skippedUpdateVersion);
  expect(skipped).toBe("");
});

test("更新：跳过此版本后自动检测不再弹，手动 force 仍可弹", async ({ page }) => {
  await installTauriMock(page, {
    updateCheck: { ...SAMPLE_UPDATE, skipped: true },
  });
  await page.goto("/");
  await expect(page.getByTestId("app-root")).toBeVisible();
  await expect(page.getByTestId("update-modal")).toHaveCount(0);

  await page.getByTestId("nav-system").click();
  await page.getByTestId("check-update-btn").click();
  await expect(page.getByTestId("update-modal")).toBeVisible();
  await page.getByTestId("update-skip").click();
  await expect(page.getByTestId("save-toast").filter({ hasText: "已跳过 v0.3.0" })).toBeVisible();
  const skipped = await page.evaluate(() => (window as any).__E2E_STATE__.skippedUpdateVersion);
  expect(skipped).toBe("0.3.0");
});

test("更新：后台下载显示左下角进度条，悬浮气泡展示速度与剩余时间", async ({ page }) => {
  await page.evaluate((u) => {
    (window as any).__E2E_STATE__.updateCheck = { ...u };
  }, SAMPLE_UPDATE);
  await page.getByTestId("nav-system").click();
  await page.getByTestId("check-update-btn").click();
  await expect(page.getByTestId("update-modal")).toBeVisible();
  await page.getByTestId("update-confirm").click();
  // 弹窗关闭、进度条出现（后台下载不影响使用）
  await expect(page.getByTestId("update-modal")).toHaveCount(0);
  const bar = page.getByTestId("update-dl-bar");
  await expect(bar).toBeVisible();
  await expect(bar).toContainText("正在后台下载更新包");
  // 推送若干进度事件模拟下载中
  await page.evaluate(() => {
    const s = (window as any).__E2E_STATE__;
    s.__emitUpdateProgress({ received: 2 * 1024 * 1024, total: 10 * 1024 * 1024, phase: "downloading", error: "" });
  });
  // 悬浮气泡显示已下载 / 速度 / 剩余
  await expect(bar).toContainText("已下载");
  await expect(bar).toContainText("速度");
  await expect(bar).toContainText("剩余");
  // 下载完成后提示安装中
  await page.evaluate(() => {
    (window as any).__E2E_STATE__.__emitUpdateProgress({
      received: 10 * 1024 * 1024,
      total: 10 * 1024 * 1024,
      phase: "done",
      error: "",
    });
  });
  await expect(bar).toContainText("下载完成");
});

test("更新：后台下载失败时进度条消失并 toast 提示", async ({ page }) => {
  await page.evaluate((u) => {
    (window as any).__E2E_STATE__.updateCheck = { ...u };
  }, SAMPLE_UPDATE);
  await page.getByTestId("nav-system").click();
  await page.getByTestId("check-update-btn").click();
  await expect(page.getByTestId("update-modal")).toBeVisible();
  await page.getByTestId("update-confirm").click();
  await page.evaluate(() => {
    (window as any).__E2E_STATE__.__emitUpdateProgress({
      received: 0,
      total: 0,
      phase: "error",
      error: "连接被重置",
    });
  });
  await expect(page.getByTestId("update-dl-bar")).toHaveCount(0);
  await expect(page.getByTestId("save-toast").filter({ hasText: "更新下载失败" })).toBeVisible();
});

test("更新：缺少安装包时手动检查提示明确文案", async ({ page }) => {
  await page.evaluate(() => {
    (window as any).__E2E_STATE__.updateCheck = {
      available: false,
      currentVersion: "0.2.6",
      latestVersion: "0.3.0",
      notes: "",
      downloadUrl: "",
      publishedAt: "",
      skipped: false,
      message: "发现新版本 v0.3.0，但 Release 中缺少 YZJBridge-0.3.0-Windows-x64-setup.exe",
    };
  });
  await page.getByTestId("nav-system").click();
  await page.getByTestId("check-update-btn").click();
  await expect(page.getByTestId("update-modal")).toHaveCount(0);
  await expect(page.getByTestId("save-toast").filter({ hasText: "缺少" })).toBeVisible();
});

test("系统页：关闭到托盘开关", async ({ page }) => {
  await page.getByTestId("nav-system").click();
  const sw = page.getByTestId("close-to-tray-switch");
  await expect(sw).toHaveAttribute("aria-checked", "true");
  await sw.click();
  await expect(sw).toHaveAttribute("aria-checked", "false");
});

test("AI 设置：CLI 自动扫描与一键安装入口", async ({ page }) => {
  await page.getByTestId("nav-settings").click();
  await expect(page.getByTestId("group-cursor")).toBeVisible();
  await expect(page.getByTestId("discover-cursor")).toBeVisible();
  await expect(page.getByTestId("discover-claude")).toBeVisible();
  // Mock: cursor found → autofill absolute path; claude missing → install button.
  await expect(page.getByTestId("cursor-discover-hint")).toContainText("已找到");
  await expect(page.getByTestId("cursor-bin")).toHaveValue(/cursor-agent/);
  await expect(page.getByTestId("claude-discover-hint")).toContainText("未在 PATH");
  await expect(page.getByTestId("install-claude")).toBeVisible();
  await page.getByTestId("install-claude").click();
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

test("AI 设置：DSH 精简卡片（入口/Node/模型）与保存回读", async ({ page }) => {
  await page.getByTestId("nav-settings").click();
  await expect(page.getByTestId("group-dsh")).toBeVisible();
  // 精简后仅保留 3 项配置：DSH CLI 入口 / Node 可执行 / 默认模型。
  await expect(page.getByTestId("dsh-entry")).toBeVisible();
  await expect(page.getByTestId("dsh-node-bin")).toBeVisible();
  await expect(page.getByTestId("dsh-model")).toBeVisible();
  // 其余 6 个字段的输入框不再渲染。
  await expect(page.getByTestId("dsh-profile")).toHaveCount(0);
  await expect(page.getByTestId("dsh-provider")).toHaveCount(0);
  await expect(page.getByTestId("dsh-timeout")).toHaveCount(0);
  await expect(page.getByTestId("dsh-ttl-seconds")).toHaveCount(0);
  await expect(page.getByTestId("dsh-max-warm")).toHaveCount(0);
  await expect(page.getByTestId("dsh-home")).toHaveCount(0);
  // 进入设置页自动扫描（与 Cursor CLI 一致）：DSH/Node 命中 mock 并 autofill。
  await expect(page.getByTestId("dsh-discover-hint")).toContainText("已找到");
  await expect(page.getByTestId("dsh-entry")).toHaveValue(/bin\.js/);
  await expect(page.getByTestId("dsh-node-bin")).toHaveValue(/node\.exe/);
  // 手动重新扫描仍可用。
  await page.getByTestId("discover-dsh").click();
  await expect(page.getByTestId("dsh-discover-hint")).toContainText("已找到");
  await expect(page.getByTestId("dsh-entry")).toHaveValue(/bin\.js/);
  // 默认模型：进入设置页已自动拉取一次；手动刷新仍可用。
  await page.getByTestId("refresh-dsh-models").click();
  await expect(page.getByTestId("save-toast").filter({ hasText: "已拉取 2 个 DSH 模型" })).toBeVisible();
  await page.getByTestId("dsh-model").locator(".fancy-select-trigger").click();
  const dshMenu = page.getByTestId("dsh-model-menu");
  await expect(dshMenu.getByRole("option", { name: "DeepSeek-V4-Flash" })).toBeVisible();
  await dshMenu.getByRole("option", { name: "DeepSeek-V4-Flash" }).click();
  // 保存 → 回读保留 dsh_model。
  await page.getByTestId("save-settings").click();
  await expect(page.getByTestId("save-toast").filter({ hasText: "设置已保存" })).toBeVisible();
  await expect(page.getByTestId("dsh-model")).toContainText("DeepSeek-V4-Flash");
});

test("AI 设置：卡片顺序 cursor → dsh → openai → claude → memory → dirs", async ({ page }) => {
  await page.getByTestId("nav-settings").click();
  await expect(page.getByTestId("group-cursor")).toBeVisible();
  const ids = await page
    .locator(
      '[data-testid="group-cursor"], [data-testid="group-dsh"], [data-testid="group-openai"], [data-testid="group-claude"], [data-testid="group-memory"], [data-testid="group-dirs"]',
    )
    .evaluateAll((els) => els.map((el) => el.getAttribute("data-testid")));
  expect(ids).toEqual([
    "group-cursor",
    "group-dsh",
    "group-openai",
    "group-claude",
    "group-memory",
    "group-dirs",
  ]);
});

test("新建机器人：后端下拉仅含可用引擎", async ({ page }) => {
  await page.getByTestId("nav-bots").click();
  await page.getByTestId("create-bot").click();
  await expect(page.getByTestId("bot-modal")).toBeVisible();
  await page.getByTestId("bot-backend").locator(".fancy-select-trigger").click();
  const menu = page.getByTestId("bot-backend-menu");
  await expect(menu).toBeVisible();
  // 仅 available===true 的引擎（label 为服务端返回的人类名）。
  await expect(menu.getByRole("option", { name: "Cursor CLI" })).toBeVisible();
  await expect(menu.getByRole("option", { name: "OpenAI 兼容" })).toBeVisible();
  await expect(menu.getByRole("option", { name: "DSH（DeepSeek Harness）" })).toBeVisible();
  await expect(menu.getByRole("option", { name: "Claude Code" })).toHaveCount(0);
  await expect(menu.getByRole("option", { name: "OpenCode" })).toHaveCount(0);
  await page.getByTestId("bot-modal-close").click();
  await expect(page.getByTestId("bot-modal")).toHaveCount(0);
});

test("新建机器人：底部保存按钮吸底，滚动表单后仍可见", async ({ page }) => {
  await page.getByTestId("nav-bots").click();
  await page.getByTestId("create-bot").click();
  await expect(page.getByTestId("bot-modal")).toBeVisible();
  // 压缩滚动区高度模拟长表单，验证 modal-actions 固定在 modal 底部（不随内容滚走）
  await page.locator(".modal-scroll").evaluate((el) => {
    el.style.maxHeight = "120px";
  });
  const btn = page.getByTestId("save-bot");
  await expect(btn).toBeVisible();
  // 保存按钮在 modal 内，且位于视口下部（modal 内吸底，而非滚出视口）
  const box = await btn.boundingBox();
  expect(box).toBeTruthy();
  expect(box!.y).toBeGreaterThanOrEqual(0);
  expect(box!.y + box!.height).toBeLessThanOrEqual(800);
  await page.getByTestId("bot-modal-close").click();
  await expect(page.getByTestId("bot-modal")).toHaveCount(0);
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
  await page.getByTestId("bot-backend-menu").getByRole("option", { name: "OpenAI 兼容" }).click();
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
  // 进入设置页自动扫描：cursor 命中 mock 并 autofill 绝对路径（等扫描完成）；
  // claude mock 未找到，不覆盖初始值 "claude"。
  await expect(page.getByTestId("cursor-bin")).toHaveValue(/agent\.exe/);
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

test("记忆页：推断值直接显示可编辑，编辑后保存为手动值且不覆盖原推断", async ({ page }) => {
  await page.evaluate(() => {
    (window as any).__E2E_STATE__.memoryProfiles = [
      {
        open_id: "mem-user-1",
        display_name: "测试用户",
        how_to_address: { inferred: "小王", locked: false },
        role: { manual: "运营" },
        ask_style: { inferred: "简洁" },
        reply_style: {},
        notes: {},
        donts: { inferred: ["不刷屏"] },
        turn_count: 12,
        profiled_count: 5,
        last_seen: "2026-08-21T00:00:00Z",
      },
    ];
  });
  await page.getByTestId("nav-memory").click();
  await page.getByTestId("memory-user-mem-user-1").click();
  await expect(page.getByTestId("memory-detail")).toContainText("测试用户");

  // 推断字段：推断值直接显示在输入框（可编辑），徽标为「推断」
  const field = page.getByTestId("memory-field-how_to_address");
  await expect(field).toHaveValue("小王");
  await expect(page.getByTestId("memory-src-how_to_address")).toHaveText("推断");
  // 手动字段：输入框直接显示 manual，徽标为「手动」
  await expect(page.getByTestId("memory-field-role")).toHaveValue("运营");
  await expect(page.getByTestId("memory-src-role")).toHaveText("手动");
  // 空字段无徽标
  await expect(page.getByTestId("memory-src-reply_style")).toHaveCount(0);
  // 忌口（donts）推断值也直接显示在输入框
  await expect(page.getByTestId("memory-field-donts")).toHaveValue("不刷屏");
  await expect(page.getByTestId("memory-src-donts")).toHaveText("推断");

  // 用户直接编辑推断值并保存：写入手动值，原推断保留
  await field.fill("老李");
  await page.getByTestId("memory-save").click();
  await expect(page.getByTestId("save-toast").filter({ hasText: "手动字段已保存" })).toBeVisible();
  const saved = await page.evaluate(() => (window as any).__E2E_STATE__.memoryProfiles[0]);
  expect(saved.how_to_address.manual).toBe("老李");
  expect(saved.how_to_address.inferred).toBe("小王");
  expect(saved.role.manual).toBe("运营");
  // 未编辑的推断字段不被发送、不被固化：manual 不存在
  expect(saved.ask_style.manual).toBeUndefined();
  expect(saved.ask_style.inferred).toBe("简洁");
  // 未编辑的推断忌口不被固化
  expect(saved.donts.manual).toBeUndefined();
  expect(saved.donts.inferred).toEqual(["不刷屏"]);
});

test("记忆页：锁定按钮语义清晰，点击后锁定徽标出现且推断仍展示", async ({ page }) => {
  await page.evaluate(() => {
    (window as any).__E2E_STATE__.memoryProfiles = [
      {
        open_id: "mem-user-2",
        display_name: "锁定测试",
        how_to_address: { inferred: "小李", locked: false },
        role: {},
        ask_style: {},
        reply_style: {},
        notes: {},
        donts: { inferred: ["不刷屏"] },
        turn_count: 3,
        profiled_count: 1,
      },
    ];
  });
  await page.getByTestId("nav-memory").click();
  await page.getByTestId("memory-user-mem-user-2").click();

  const lockBtn = page.getByTestId("memory-lock-how_to_address");
  await expect(lockBtn).toContainText("锁定");
  await expect(lockBtn).toHaveAttribute("title", /锁定后该字段不可编辑/);
  await lockBtn.click();
  await expect(page.getByTestId("memory-locked-how_to_address")).toHaveText("已锁定");
  await expect(lockBtn).toContainText("已锁定");
  // 推断值仍在输入框，未被破坏
  await expect(page.getByTestId("memory-field-how_to_address")).toHaveValue("小李");
  // 锁定字段不可编辑
  await expect(page.getByTestId("memory-field-how_to_address")).toBeDisabled();
  // donts 也有锁定按钮
  await expect(page.getByTestId("memory-lock-donts")).toBeVisible();
});

test("记忆页：全部输入框锁定后不可编辑、解锁后可编辑", async ({ page }) => {
  await page.evaluate(() => {
    (window as any).__E2E_STATE__.memoryProfiles = [
      {
        open_id: "mem-user-5",
        display_name: "全字段锁定测试",
        how_to_address: { manual: "小王", locked: false },
        role: { manual: "运营", locked: false },
        ask_style: { manual: "简洁", locked: false },
        reply_style: { manual: "结构化", locked: false },
        notes: { manual: "细节", locked: false },
        donts: { manual: ["不刷屏"], locked: false },
        turn_count: 6,
        profiled_count: 3,
      },
    ];
  });
  await page.getByTestId("nav-memory").click();
  await page.getByTestId("memory-user-mem-user-5").click();

  const fields: Array<[fieldId: string, lockId: string]> = [
    ["memory-field-how_to_address", "memory-lock-how_to_address"],
    ["memory-field-role", "memory-lock-role"],
    ["memory-field-ask_style", "memory-lock-ask_style"],
    ["memory-field-reply_style", "memory-lock-reply_style"],
    ["memory-field-notes", "memory-lock-notes"],
    ["memory-field-donts", "memory-lock-donts"],
  ];

  for (const [fieldId, lockId] of fields) {
    const field = page.getByTestId(fieldId);
    const lockBtn = page.getByTestId(lockId);
    // 初始可编辑
    await expect(field).toBeEnabled();
    // 锁定
    await lockBtn.click();
    await expect(field).toBeDisabled();
    // 锁定后值保持不变（disabled 输入框无法修改）
    const lockedValue = await field.inputValue();
    await field.evaluate((el, v) => {
      (el as HTMLInputElement).value = "强改";
      el.dispatchEvent(new Event("input", { bubbles: true }));
    }, "强改");
    await expect(field).toHaveValue(lockedValue);
    // 解锁后可编辑
    await lockBtn.click();
    await expect(field).toBeEnabled();
    // 解锁后可以正常输入
    await field.fill("改好了");
    await expect(field).toHaveValue("改好了");
    // 复原，避免影响下一字段（同一行不同字段互不影响，但清空刚输入的测试值）
    await field.fill("");
  }
});

test("记忆页：点击标签文本不触发锁定按钮（按钮左侧点击范围）", async ({ page }) => {
  await page.evaluate(() => {
    (window as any).__E2E_STATE__.memoryProfiles = [
      {
        open_id: "mem-user-6",
        display_name: "点击范围测试",
        how_to_address: { manual: "小王" },
        role: {},
        ask_style: {},
        reply_style: {},
        notes: {},
        donts: {},
        turn_count: 6,
        profiled_count: 3,
      },
    ];
  });
  await page.getByTestId("nav-memory").click();
  await page.getByTestId("memory-user-mem-user-6").click();

  const lockBtn = page.getByTestId("memory-lock-how_to_address");
  await expect(lockBtn).toContainText("锁定");
  // 点击字段标签文本（按钮左侧），不应触发锁定
  const fieldName = page.getByText("如何称呼", { exact: true });
  await fieldName.click();
  await expect(lockBtn).toContainText("锁定");
  await expect(page.getByTestId("memory-locked-how_to_address")).toHaveCount(0);
  // 点击「手动」徽标（按钮左侧的另一个元素），也不应触发锁定
  await page.getByTestId("memory-src-how_to_address").click();
  await expect(lockBtn).toContainText("锁定");
  // 点击标签区域空白（label 文本行）也不触发
  await page.locator(".memory-field-label").filter({ hasText: "如何称呼" }).click();
  await expect(lockBtn).toContainText("锁定");
  // 输入框仍可正常编辑（label 关联正确）
  await expect(page.getByTestId("memory-field-how_to_address")).toBeEnabled();
});

test("记忆页：清空手动值后可保存删除 manual", async ({ page }) => {
  await page.evaluate(() => {
    (window as any).__E2E_STATE__.memoryProfiles = [
      {
        open_id: "mem-user-3",
        display_name: "清空测试",
        how_to_address: { manual: "老张", inferred: "小张" },
        role: {},
        ask_style: {},
        reply_style: {},
        notes: {},
        donts: {},
        turn_count: 3,
        profiled_count: 1,
      },
    ];
  });
  await page.getByTestId("nav-memory").click();
  await page.getByTestId("memory-user-mem-user-3").click();
  const field = page.getByTestId("memory-field-how_to_address");
  await expect(field).toHaveValue("老张");
  await field.fill("");
  await page.getByTestId("memory-save").click();
  await expect(page.getByTestId("save-toast").filter({ hasText: "手动字段已保存" })).toBeVisible();
  const saved = await page.evaluate(() => (window as any).__E2E_STATE__.memoryProfiles[0]);
  expect(saved.how_to_address.manual).toBe("");
  // 推断值仍在，删除手动值后回退显示推断值（输入框直接显示，仍可编辑）
  await expect(field).toHaveValue("小张");
  await expect(page.getByTestId("memory-src-how_to_address")).toHaveText("推断");
});

test("记忆页：详情操作按钮吸底，滚动字段后仍可见", async ({ page }) => {
  await page.evaluate(() => {
    (window as any).__E2E_STATE__.memoryProfiles = [
      {
        open_id: "mem-user-4",
        display_name: "吸底测试",
        how_to_address: { inferred: "小王" },
        role: { manual: "运营" },
        ask_style: { inferred: "简洁" },
        reply_style: {},
        notes: {},
        donts: { inferred: ["不刷屏"] },
        turn_count: 12,
        profiled_count: 5,
      },
    ];
  });
  await page.getByTestId("nav-memory").click();
  await page.getByTestId("memory-user-mem-user-4").click();
  await expect(page.getByTestId("memory-detail")).toContainText("吸底测试");
  // 压缩滚动区高度模拟长表单，验证 memory-actions 吸底仍可见
  await page.locator(".memory-detail").evaluate((el) => {
    el.style.maxHeight = "180px";
    el.style.overflow = "auto";
  });
  const save = page.getByTestId("memory-save");
  await expect(save).toBeVisible();
  const box = await save.boundingBox();
  expect(box).toBeTruthy();
  expect(box!.y).toBeGreaterThanOrEqual(0);
  expect(box!.y + box!.height).toBeLessThanOrEqual(800);
  // 滚动后仍可见（吸底）
  await page.locator(".memory-detail").evaluate((el) => {
    el.scrollTop = el.scrollHeight;
  });
  await expect(save).toBeVisible();
});
