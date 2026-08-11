import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, KeyboardEvent as ReactKeyboardEvent } from "react";
import { createPortal } from "react-dom";
import { invoke } from "@tauri-apps/api/core";
import "./App.css";

type StatusItem = {
  id: string;
  role_id: string;
  name: string;
  group: string;
  backend: string;
  inbound_mode: string;
  connected: boolean;
  ws_enabled: boolean;
  last_error: string;
  has_ws: boolean;
};

type LogLine = { seq: number; time: string; level: string; bot?: string; message: string };
type ThemeId = "aurora" | "midnight" | "sand" | "ice";
type PageId = "system" | "bots" | "settings" | "logs" | "help";

type BotForm = {
  id: string;
  name: string;
  backend: string;
  group: string;
  send_msg_url: string;
  system_prompt: string;
  model: string;
  openai_base_url: string;
  openai_api_key: string;
  inbound_mode: string;
  workspace: string;
};

type ChannelForm = {
  id: string;
  group: string;
  send_msg_url: string;
  model: string;
};

const THEMES: { id: ThemeId; label: string }[] = [
  { id: "ice", label: "冰蓝白" },
  { id: "aurora", label: "极光绿" },
  { id: "midnight", label: "午夜蓝" },
  { id: "sand", label: "暖砂" },
];

const DEFAULT_CURSOR_WORKSPACE = "~/.yzj-bridge/workspace/cursor_cli";
const DEFAULT_CLAUDE_WORKSPACE = "~/.yzj-bridge/workspace/claude_code";

const BACKENDS = ["cursor_cli", "claude_code", "openai", "opencode"];
const MIN_LOADING_MS = 500;

async function api(method: string, path: string, body?: unknown): Promise<string> {
  return invoke<string>("bridge_fetch", {
    method,
    path,
    body: body === undefined ? null : JSON.stringify(body),
  });
}

async function guiLog(message: string, level: "INFO" | "WARN" | "ERROR" = "INFO") {
  try {
    await api("POST", "/v1/logs", { level, message });
  } catch {
    /* bridge may be starting; ignore */
  }
}

async function openExternal(url: string) {
  try {
    const mod = await import("@tauri-apps/plugin-opener");
    await mod.openUrl(url);
  } catch {
    window.open(url, "_blank", "noopener,noreferrer");
  }
}

const GITHUB_URL = "https://github.com/JinRyu-online/yzj_bot_bridge";
const GITHUB_PROFILE_URL = "https://github.com/JinRyu-online";
const DEVELOPER_AVATAR = "/jinryu-avatar.jpg";
const DEVELOPER_NAME = "Jinryu";
const DEVELOPER_HANDLE = "JinRyu-online";

/** 将日志拆成圆角标签 + 正文；GUI 去掉旧版 [GUI] 前缀，机器人去掉 bot=id。 */
function formatLogLine(l: LogLine): { tag: string; message: string } {
  let message = l.message || "";
  if (l.bot === "gui" || message.startsWith("[GUI]")) {
    return { tag: "GUI", message: message.replace(/^\[GUI\]\s*/, "") };
  }
  let bot = (l.bot || "").trim();
  if (!bot) {
    const m = message.match(/\bbot=([^\s\]:,]+)/);
    if (m) bot = m[1];
  }
  if (bot) {
    message = message
      .replace(new RegExp(`\\bbot=${bot.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}\\b`), "")
      .replace(/\s{2,}/g, " ")
      .trim();
    return { tag: bot, message };
  }
  return { tag: "", message };
}

/** 按后端展示模型：机器人自身 model 优先，否则回退到 AI 设置里的引擎默认。 */
function resolveDisplayedModel(
  cfg: Record<string, unknown> | null | undefined,
  defaults?: { cursor_model?: string; claude_model?: string; openai_model?: string },
): string {
  if (!cfg) return "默认";
  const be = String(cfg.backend || "");
  const own = String(cfg.model || "").trim();
  if (own) return own;
  if (be === "openai") {
    return String(cfg.openai_model || defaults?.openai_model || "").trim() || "默认";
  }
  if (be === "claude_code" || be === "claude") {
    return String(cfg.claude_model || defaults?.claude_model || "").trim() || "默认";
  }
  return String(cfg.cursor_model || defaults?.cursor_model || "").trim() || "默认";
}

async function withMinLoading(
  setLoading: (v: boolean) => void,
  fn: () => Promise<void>,
  ms = MIN_LOADING_MS,
) {
  setLoading(true);
  const started = Date.now();
  try {
    await fn();
  } finally {
    const left = ms - (Date.now() - started);
    if (left > 0) await new Promise((r) => setTimeout(r, left));
    setLoading(false);
  }
}

function Switch({
  checked,
  onChange,
  disabled,
  loading,
  testId,
}: {
  checked: boolean;
  onChange: (v: boolean) => void;
  disabled?: boolean;
  loading?: boolean;
  testId?: string;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      data-testid={testId || "switch"}
      disabled={disabled || loading}
      className={`switch${checked ? " on" : ""}${loading ? " loading" : ""}`}
      onClick={() => onChange(!checked)}
    >
      <span className="switch-knob">{loading ? <span className="spinner" /> : null}</span>
    </button>
  );
}

function fuzzyMatch(text: string, query: string): boolean {
  const t = text.toLowerCase();
  const q = query.trim().toLowerCase();
  if (!q) return true;
  if (t.includes(q)) return true;
  let i = 0;
  for (const ch of t) {
    if (ch === q[i]) i += 1;
    if (i >= q.length) return true;
  }
  return false;
}

function FancySelect<T extends string>({
  value,
  options,
  onChange,
  testId,
  className,
  placeholder,
  searchable = true,
  disabled = false,
  onOpen,
}: {
  value: T;
  options: { id: T; label: string }[];
  onChange: (v: T) => void;
  testId?: string;
  className?: string;
  placeholder?: string;
  searchable?: boolean;
  disabled?: boolean;
  onOpen?: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [activeIdx, setActiveIdx] = useState(0);
  const rootRef = useRef<HTMLDivElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const [menuStyle, setMenuStyle] = useState<CSSProperties>({});
  const merged = useMemo(() => {
    const list = [...options];
    if (value && !list.some((o) => o.id === value)) {
      list.unshift({ id: value, label: value });
    }
    return list;
  }, [options, value]);
  const filtered = useMemo(() => {
    if (!searchable || !query.trim()) return merged;
    return merged.filter((o) => fuzzyMatch(o.label, query) || fuzzyMatch(o.id, query));
  }, [merged, query, searchable]);
  const current = merged.find((o) => o.id === value);

  useEffect(() => {
    if (disabled && open) setOpen(false);
  }, [disabled, open]);

  const placeMenu = useCallback(() => {
    const el = rootRef.current;
    if (!el) return;
    const rect = el.getBoundingClientRect();
    const width = Math.max(rect.width, 220);
    const maxH = 320;
    const gap = 8;
    const spaceBelow = window.innerHeight - rect.bottom - gap;
    const spaceAbove = rect.top - gap;
    const openUp = spaceBelow < 180 && spaceAbove > spaceBelow;
    const height = Math.max(140, Math.min(maxH, openUp ? spaceAbove : spaceBelow));
    const top = openUp ? Math.max(8, rect.top - gap - height) : rect.bottom + gap;
    let left = rect.left;
    if (left + width > window.innerWidth - 12) {
      left = Math.max(12, window.innerWidth - width - 12);
    }
    setMenuStyle({
      position: "fixed",
      top,
      left,
      width,
      maxHeight: height,
      zIndex: 10000,
    });
  }, []);

  useLayoutEffect(() => {
    if (!open) return;
    placeMenu();
    setQuery("");
    setActiveIdx(0);
    const t = window.setTimeout(() => searchRef.current?.focus(), 0);
    return () => window.clearTimeout(t);
  }, [open, placeMenu]);

  useLayoutEffect(() => {
    if (!open) return;
    placeMenu();
  }, [open, placeMenu, filtered.length, searchable]);

  useEffect(() => {
    if (!open) return;
    const onDoc = (e: MouseEvent) => {
      const t = e.target as Node;
      if (rootRef.current?.contains(t) || menuRef.current?.contains(t)) return;
      setOpen(false);
    };
    const onReposition = () => placeMenu();
    document.addEventListener("mousedown", onDoc);
    window.addEventListener("resize", onReposition);
    window.addEventListener("scroll", onReposition, true);
    return () => {
      document.removeEventListener("mousedown", onDoc);
      window.removeEventListener("resize", onReposition);
      window.removeEventListener("scroll", onReposition, true);
    };
  }, [open, placeMenu]);

  useEffect(() => {
    setActiveIdx(0);
  }, [query]);

  const pick = (id: T) => {
    onChange(id);
    setOpen(false);
    setQuery("");
  };

  const onSearchKeyDown = (e: ReactKeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Escape") {
      e.preventDefault();
      setOpen(false);
      return;
    }
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActiveIdx((i) => Math.min(i + 1, Math.max(filtered.length - 1, 0)));
      return;
    }
    if (e.key === "ArrowUp") {
      e.preventDefault();
      setActiveIdx((i) => Math.max(i - 1, 0));
      return;
    }
    if (e.key === "Enter") {
      e.preventDefault();
      const hit = filtered[activeIdx];
      if (hit) pick(hit.id);
    }
  };

  const menu =
    open && typeof document !== "undefined"
      ? createPortal(
          <div
            ref={menuRef}
            className="fancy-select-menu portal"
            role="listbox"
            data-testid={testId ? `${testId}-menu` : undefined}
            style={menuStyle}
            onMouseDown={(e) => e.stopPropagation()}
          >
            {searchable ? (
              <div className="fancy-select-search">
                <input
                  ref={searchRef}
                  data-testid={testId ? `${testId}-search` : undefined}
                  value={query}
                  placeholder="输入搜索…"
                  onChange={(e) => setQuery(e.target.value)}
                  onKeyDown={onSearchKeyDown}
                />
              </div>
            ) : null}
            <div className="fancy-select-options">
              {filtered.length ? (
                filtered.map((o, idx) => (
                  <button
                    key={o.id || "__empty"}
                    type="button"
                    role="option"
                    aria-selected={o.id === value}
                    className={`fancy-option${o.id === value ? " active" : ""}${idx === activeIdx ? " focus" : ""}`}
                    title={o.label}
                    onMouseEnter={() => setActiveIdx(idx)}
                    onClick={() => pick(o.id)}
                  >
                    {o.label}
                  </button>
                ))
              ) : (
                <div className="fancy-option muted">
                  {merged.length ? "无匹配项" : "暂无选项，请先拉取"}
                </div>
              )}
            </div>
          </div>,
          document.body,
        )
      : null;

  return (
    <div
      className={`fancy-select${open ? " open" : ""}${disabled ? " disabled" : ""}${className ? ` ${className}` : ""}`}
      ref={rootRef}
      data-testid={testId}
    >
      <button
        type="button"
        className="fancy-select-trigger"
        aria-expanded={open}
        aria-haspopup="listbox"
        disabled={disabled}
        onClick={(e) => {
          e.preventDefault();
          e.stopPropagation();
          if (disabled) return;
          setOpen((v) => {
            const next = !v;
            if (next) onOpen?.();
            return next;
          });
        }}
        title={current?.label || value || placeholder || ""}
      >
        <span className="fancy-select-label">
          {current?.label || value || placeholder || "请选择"}
        </span>
        <span className="fancy-caret" />
      </button>
      {menu}
    </div>
  );
}

function SecretInput({
  value,
  onChange,
  placeholder,
  testId,
}: {
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  testId?: string;
}) {
  const [visible, setVisible] = useState(false);
  return (
    <div className="secret-input">
      <input
        data-testid={testId}
        type={visible ? "text" : "password"}
        value={value}
        placeholder={placeholder}
        onChange={(e) => onChange(e.target.value)}
        autoComplete="off"
        spellCheck={false}
      />
      <button
        type="button"
        className="secret-toggle"
        data-testid={testId ? `${testId}-toggle` : undefined}
        aria-label={visible ? "隐藏密钥" : "显示密钥"}
        aria-pressed={visible}
        title={visible ? "隐藏" : "显示"}
        onClick={() => setVisible((v) => !v)}
      >
        <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
          {visible ? (
            <>
              <path
                fill="none"
                stroke="currentColor"
                strokeWidth="1.8"
                d="M3 3l18 18M10.6 10.6a2.5 2.5 0 003.5 3.5"
              />
              <path
                fill="none"
                stroke="currentColor"
                strokeWidth="1.8"
                d="M9.9 5.1A10.5 10.5 0 0121 12c-.7 1.2-1.7 2.4-2.9 3.3M6.1 6.1C4.6 7.3 3.5 8.8 2.8 12c1.6 3.7 5.1 6 9.2 6 1.3 0 2.6-.3 3.8-.7"
              />
            </>
          ) : (
            <>
              <path
                fill="none"
                stroke="currentColor"
                strokeWidth="1.8"
                d="M2.8 12C4.4 8.3 7.9 6 12 6s7.6 2.3 9.2 6c-1.6 3.7-5.1 6-9.2 6s-7.6-2.3-9.2-6z"
              />
              <circle cx="12" cy="12" r="2.6" fill="none" stroke="currentColor" strokeWidth="1.8" />
            </>
          )}
        </svg>
      </button>
    </div>
  );
}

function emptyBotForm(): BotForm {
  return {
    id: "",
    name: "",
    backend: "cursor_cli",
    group: "default",
    send_msg_url: "",
    system_prompt: "",
    model: "",
    openai_base_url: "",
    openai_api_key: "",
    inbound_mode: "websocket",
    workspace: "",
  };
}

function App() {
  const [page, setPage] = useState<PageId>("system");
  const [theme, setTheme] = useState<ThemeId>(() => {
    const saved = localStorage.getItem("yzj-theme") as ThemeId | null;
    return saved && THEMES.some((t) => t.id === saved) ? saved : "ice";
  });
  const [ready, setReady] = useState(false);
  const [booting, setBooting] = useState(true);
  const [error, setError] = useState("");
  const [status, setStatus] = useState<StatusItem[]>([]);
  const [paths, setPaths] = useState<{ config: string; data: string } | null>(null);
  const [autostart, setAutostart] = useState(false);
  const [logs, setLogs] = useState<LogLine[]>([]);
  const logSeqRef = useRef(0);
  const [logBot, setLogBot] = useState("");
  const [selected, setSelected] = useState("");
  const [rawConfig, setRawConfig] = useState<Record<string, unknown> | null>(null);

  const [loadingAuto, setLoadingAuto] = useState(false);
  const [loadingWss, setLoadingWss] = useState(false);
  const [loadingReload, setLoadingReload] = useState(false);
  const [savingCli, setSavingCli] = useState(false);
  const [saveToast, setSaveToast] = useState("");
  const saveToastTimer = useRef<number | null>(null);
  const [closeToTray, setCloseToTray] = useState(true);
  const [loadingCloseTray, setLoadingCloseTray] = useState(false);
  const cursorModelsAutoTried = useRef(false);
  const claudeModelsAutoTried = useRef(false);
  /** 已自动探测过的 OpenAI base\\nkey 指纹，变更凭据后可再次自动探测。 */
  const openaiAutoFingerprint = useRef("");
  const [cursorModels, setCursorModels] = useState<{ id: string; label: string }[]>([]);
  const [loadingCursorModels, setLoadingCursorModels] = useState(false);
  const [cursorModelsHint, setCursorModelsHint] = useState("");
  const [claudeModels, setClaudeModels] = useState<{ id: string; label: string }[]>([]);
  const [loadingClaudeModels, setLoadingClaudeModels] = useState(false);
  const [claudeModelsHint, setClaudeModelsHint] = useState("");
  const [openaiModels, setOpenaiModels] = useState<{ id: string; label: string }[]>([]);
  const [openaiProbeInfo, setOpenaiProbeInfo] = useState("");
  const [openaiProbeOk, setOpenaiProbeOk] = useState<boolean | null>(null);
  const [probingOpenai, setProbingOpenai] = useState(false);

  const [botModal, setBotModal] = useState<"create" | "edit" | null>(null);
  const [channelModal, setChannelModal] = useState(false);
  const [botForm, setBotForm] = useState<BotForm>(emptyBotForm());
  const [channelForm, setChannelForm] = useState<ChannelForm>({
    id: "",
    group: "",
    send_msg_url: "",
    model: "",
  });
  const [editingChannelIdx, setEditingChannelIdx] = useState<number | null>(null);
  const [saving, setSaving] = useState(false);

  const [cliForm, setCliForm] = useState({
    cursor_bin: "agent",
    claude_bin: "claude",
    cursor_api_key: "",
    anthropic_api_key: "",
    openai_api_key: "",
    openai_base_url: "",
    cursor_workspace: DEFAULT_CURSOR_WORKSPACE,
    claude_workspace: DEFAULT_CLAUDE_WORKSPACE,
    projects_root: "~",
    workspace: "~",
    cursor_model: "",
    claude_model: "",
    openai_model: "",
  });

  useEffect(() => {
    document.documentElement.setAttribute("data-theme", theme);
    localStorage.setItem("yzj-theme", theme);
  }, [theme]);

  useEffect(() => {
    if (!botModal && !channelModal) return;
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (e.key === "Escape") {
        setBotModal(null);
        setChannelModal(false);
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [botModal, channelModal]);

  const boot = useCallback(async () => {
    setBooting(true);
    setError("");
    let lastErr = "";
    for (let i = 0; i < 40; i++) {
      try {
        await invoke("ensure_bridge");
        const [auto, tray] = await Promise.all([
          invoke<boolean>("get_autostart"),
          invoke<boolean>("get_close_to_tray"),
        ]);
        setAutostart(auto);
        setCloseToTray(tray);
        setReady(true);
        setError("");
        setBooting(false);
        return;
      } catch (e) {
        lastErr = String(e);
        await new Promise((r) => setTimeout(r, 250));
      }
    }
    setReady(false);
    setBooting(false);
    setError(lastErr || "桥启动超时");
  }, []);

  const refreshCursorModels = useCallback(async () => {
    setLoadingCursorModels(true);
    setCursorModelsHint("");
    try {
      const raw = await api("GET", "/v1/backends/cursor/models");
      const data = JSON.parse(raw) as {
        ok?: boolean;
        models?: { id: string; label: string }[];
        error?: string;
      };
      if (data.ok === false) {
        setCursorModelsHint(data.error || "拉取 Cursor 模型失败");
        return;
      }
      const list = (data.models || []).map((m) => ({ id: m.id, label: m.label || m.id }));
      setCursorModels(list);
      if (!list.length) setCursorModelsHint("未解析到模型列表");
      else void guiLog(`拉取 Cursor 模型 ${list.length} 个`);
    } catch (e) {
      setCursorModelsHint(String(e));
      void guiLog(`拉取 Cursor 模型失败: ${e}`, "ERROR");
    } finally {
      setLoadingCursorModels(false);
    }
  }, []);

  const refreshClaudeModels = useCallback(async () => {
    setLoadingClaudeModels(true);
    setClaudeModelsHint("");
    try {
      const raw = await api("GET", "/v1/backends/claude/models");
      const data = JSON.parse(raw) as {
        ok?: boolean;
        models?: { id: string; label: string }[];
        error?: string;
        warning?: string;
      };
      if (data.ok === false) {
        setClaudeModelsHint(data.error || "拉取 Claude 模型失败");
        return;
      }
      const list = (data.models || []).map((m) => ({ id: m.id, label: m.label || m.id }));
      setClaudeModels(list);
      if (data.warning) setClaudeModelsHint(data.warning);
      else if (!list.length) setClaudeModelsHint("未解析到模型列表");
      else void guiLog(`拉取 Claude 模型 ${list.length} 个`);
    } catch (e) {
      setClaudeModelsHint(String(e));
      void guiLog(`拉取 Claude 模型失败: ${e}`, "ERROR");
    } finally {
      setLoadingClaudeModels(false);
    }
  }, []);

  const probeOpenai = useCallback(
    async (baseURL?: string, apiKey?: string) => {
      setProbingOpenai(true);
      setOpenaiProbeInfo("");
      setOpenaiProbeOk(null);
      try {
        const raw = await api("POST", "/v1/backends/openai/probe", {
          base_url: baseURL ?? cliForm.openai_base_url,
          api_key: apiKey ?? cliForm.openai_api_key,
        });
        const data = JSON.parse(raw) as {
          ok?: boolean;
          latency_ms?: number;
          models?: { id: string; label: string }[];
          error?: string;
          endpoint?: string;
        };
        if (!data.ok) {
          setOpenaiProbeOk(false);
          setOpenaiProbeInfo(data.error || "连通性测试失败");
          void guiLog(`OpenAI 连通失败: ${data.error || "未知错误"}`, "ERROR");
          return false;
        }
        const models = (data.models || []).map((m) => ({
          id: m.id,
          label: m.label || m.id,
        }));
        setOpenaiModels(models);
        setOpenaiProbeOk(true);
        setOpenaiProbeInfo(`连通成功 · ${data.latency_ms ?? 0}ms · ${models.length} 个模型`);
        void guiLog(`OpenAI 连通成功 · ${models.length} 个模型 · ${data.latency_ms ?? 0}ms`);
        return true;
      } catch (e) {
        setOpenaiProbeOk(false);
        setOpenaiProbeInfo(String(e));
        void guiLog(`OpenAI 连通失败: ${e}`, "ERROR");
        return false;
      } finally {
        setProbingOpenai(false);
      }
    },
    [cliForm.openai_base_url, cliForm.openai_api_key],
  );

  const refreshStatus = useCallback(async () => {
    try {
      const raw = await api("GET", "/v1/status");
      const data = JSON.parse(raw) as { bots: StatusItem[] };
      setStatus(data.bots || []);
      const p = JSON.parse(await api("GET", "/v1/paths")) as { config: string; data: string };
      setPaths(p);
      setError((prev) =>
        /18765|Connect error|积极拒绝|Connection Failed|no token/i.test(prev) ? "" : prev,
      );
    } catch (e) {
      const msg = String(e);
      // Keep UI calm while bridge is still coming up.
      if (!/积极拒绝|Connection Failed|Connect error|no token|bridge not ready/i.test(msg)) {
        setError(msg);
      }
    }
  }, []);

  const refreshConfig = useCallback(async () => {
    const raw = await api("GET", "/v1/config");
    const cfg = JSON.parse(raw) as Record<string, unknown>;
    setRawConfig(cfg);
    const defaults = (cfg.defaults as Record<string, unknown>) || {};
    const pick = (key: string, fallback: string) => {
      const v = String(defaults[key] ?? "").trim();
      return v || fallback;
    };
    setCliForm({
      cursor_bin: pick("cursor_bin", "agent"),
      claude_bin: pick("claude_bin", "claude"),
      cursor_api_key: String(defaults.cursor_api_key || ""),
      anthropic_api_key: String(defaults.anthropic_api_key || ""),
      openai_api_key: String(defaults.openai_api_key || ""),
      openai_base_url: String(defaults.openai_base_url || ""),
      cursor_workspace: pick("cursor_workspace", DEFAULT_CURSOR_WORKSPACE),
      claude_workspace: pick("claude_workspace", DEFAULT_CLAUDE_WORKSPACE),
      projects_root: pick("projects_root", "~"),
      workspace: pick("workspace", "~"),
      cursor_model: String(defaults.cursor_model || ""),
      claude_model: String(defaults.claude_model || ""),
      openai_model: String(defaults.openai_model || ""),
    });
  }, []);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      await boot();
      if (cancelled) return;
      try {
        await Promise.all([refreshStatus(), refreshConfig()]);
      } catch {
        /* ignore until next poll */
      }
    })();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    if (!ready) return;
    const t = setInterval(refreshStatus, 2000);
    return () => clearInterval(t);
  }, [ready, refreshStatus]);

  useEffect(() => {
    setLogs([]);
    logSeqRef.current = 0;
  }, [logBot]);

  useEffect(() => {
    if (!ready || page !== "logs") return;
    let cancelled = false;
    const pull = async () => {
      try {
        const since = logSeqRef.current;
        const q = `/v1/logs?since_seq=${since}${logBot ? `&bot=${encodeURIComponent(logBot)}` : ""}`;
        const raw = await api("GET", q);
        if (cancelled) return;
        const data = JSON.parse(raw) as { lines: LogLine[] };
        if (data.lines?.length) {
          logSeqRef.current = data.lines[data.lines.length - 1].seq;
          setLogs((prev) => [...prev, ...data.lines].slice(-500));
        }
      } catch {
        /* ignore */
      }
    };
    pull();
    const t = setInterval(pull, 1500);
    return () => {
      cancelled = true;
      clearInterval(t);
    };
  }, [ready, page, logBot]);

  const summary = useMemo(() => {
    const total = status.length;
    const enabled = status.filter((s) => s.ws_enabled).length;
    const connected = status.filter((s) => s.connected).length;
    return { total, enabled, connected };
  }, [status]);

  const wssAllOn = useMemo(() => {
    const ws = status.filter((s) => s.has_ws);
    return ws.length > 0 && ws.every((s) => s.ws_enabled);
  }, [status]);

  // Stable role order from config.bots, not map iteration of status polls.
  const roles = useMemo(() => {
    const byRole = new Map<string, StatusItem[]>();
    for (const s of status) {
      const key = s.role_id || s.id;
      if (!byRole.has(key)) byRole.set(key, []);
      byRole.get(key)!.push(s);
    }
    const orderedIds: string[] = [];
    const bots = (rawConfig?.bots as Record<string, unknown>[]) || [];
    for (const b of bots) {
      const id = String(b.id || "");
      if (id && byRole.has(id)) orderedIds.push(id);
    }
    for (const id of byRole.keys()) {
      if (!orderedIds.includes(id)) orderedIds.push(id);
    }
    return orderedIds.map((id) => [id, byRole.get(id)!] as const);
  }, [status, rawConfig]);

  const selectedItem = status.find((s) => s.id === selected) || status[0];
  const selectedRoleId = selectedItem?.role_id || selectedItem?.id || "";

  const selectedRoleConfig = useMemo(() => {
    if (!rawConfig || !selectedRoleId) return null;
    const bots = (rawConfig.bots as Record<string, unknown>[]) || [];
    return bots.find((b) => String(b.id) === selectedRoleId) || null;
  }, [rawConfig, selectedRoleId]);

  const selectedChannels = useMemo(() => {
    if (!selectedRoleConfig) return [] as Record<string, unknown>[];
    const ch = selectedRoleConfig.channels;
    if (Array.isArray(ch) && ch.length) return ch as Record<string, unknown>[];
    return [
      {
        group: selectedRoleConfig.group || "default",
        send_msg_url: selectedRoleConfig.send_msg_url || "",
      },
    ];
  }, [selectedRoleConfig]);

  const cliBots = useMemo(() => {
    const bots = (rawConfig?.bots as Record<string, unknown>[]) || [];
    return bots.filter((b) => {
      const be = String(b.backend || "cursor_cli");
      return be === "cursor_cli" || be === "cursor" || be === "claude_code" || be === "claude";
    });
  }, [rawConfig]);

  const cursorBots = useMemo(
    () =>
      cliBots.filter((b) => {
        const be = String(b.backend || "");
        return be === "cursor_cli" || be === "cursor";
      }),
    [cliBots],
  );

  const claudeBots = useMemo(
    () =>
      cliBots.filter((b) => {
        const be = String(b.backend || "");
        return be === "claude_code" || be === "claude";
      }),
    [cliBots],
  );

  async function saveConfig(next: Record<string, unknown>) {
    setSaving(true);
    try {
      await api("PUT", "/v1/config?keep_wss_state=1", next);
      setRawConfig(next);
      await refreshStatus();
    } finally {
      setSaving(false);
    }
  }

  // 进入 AI 设置页时自动拉模型：Cursor/Claude 有 bin 则各一次；OpenAI 凭据齐全后 debounce，避免打字中途锁死。
  useEffect(() => {
    if (!ready || page !== "settings") return;

    if (!cursorModelsAutoTried.current && !loadingCursorModels) {
      const bin = cliForm.cursor_bin.trim();
      if (bin) {
        cursorModelsAutoTried.current = true;
        void refreshCursorModels();
      }
    }

    if (!claudeModelsAutoTried.current && !loadingClaudeModels) {
      const bin = cliForm.claude_bin.trim();
      if (bin) {
        claudeModelsAutoTried.current = true;
        void refreshClaudeModels();
      }
    }

    const base = cliForm.openai_base_url.trim();
    const key = cliForm.openai_api_key.trim();
    if (!base || !key || probingOpenai) return;
    const fingerprint = `${base}\n${key}`;
    if (openaiAutoFingerprint.current === fingerprint) return;
    const timer = window.setTimeout(() => {
      if (openaiAutoFingerprint.current === fingerprint) return;
      openaiAutoFingerprint.current = fingerprint;
      void probeOpenai(base, key);
    }, 450);
    return () => window.clearTimeout(timer);
  }, [
    ready,
    page,
    cliForm.cursor_bin,
    cliForm.claude_bin,
    cliForm.openai_base_url,
    cliForm.openai_api_key,
    loadingCursorModels,
    loadingClaudeModels,
    probingOpenai,
    refreshCursorModels,
    refreshClaudeModels,
    probeOpenai,
  ]);

  async function toggleCloseToTray(v: boolean) {
    await withMinLoading(setLoadingCloseTray, async () => {
      try {
        const next = await invoke<boolean>("set_close_to_tray", { enabled: v });
        setCloseToTray(next);
        void guiLog(`关闭窗口行为 → ${next ? "隐藏到托盘" : "立即退出"}`);
      } catch (e) {
        setError(String(e));
        void guiLog(`设置关闭行为失败: ${e}`, "ERROR");
      }
    });
  }

  async function toggleAutostart(v: boolean) {
    await withMinLoading(setLoadingAuto, async () => {
      try {
        await invoke("set_autostart", { enabled: v });
        setAutostart(v);
        void guiLog(`开机自启 → ${v ? "开启" : "关闭"}`);
      } catch (e) {
        setError(String(e));
        void guiLog(`设置开机自启失败: ${e}`, "ERROR");
      }
    });
  }

  async function toggleWssAll(v: boolean) {
    await withMinLoading(setLoadingWss, async () => {
      try {
        await api("POST", v ? "/v1/wss/start" : "/v1/wss/stop");
        await refreshStatus();
        void guiLog(`全部 WebSocket → ${v ? "启动" : "停止"}`);
      } catch (e) {
        setError(String(e));
        void guiLog(`切换 WebSocket 失败: ${e}`, "ERROR");
      }
    });
  }

  async function triggerReload() {
    await withMinLoading(setLoadingReload, async () => {
      try {
        await api("POST", "/v1/reload?keep_wss_state=1");
        await Promise.all([refreshStatus(), refreshConfig()]);
        void guiLog("热重载配置完成");
      } catch (e) {
        setError(String(e));
        void guiLog(`热重载失败: ${e}`, "ERROR");
      }
    });
  }

  async function setChannelEnabled(id: string, enabled: boolean) {
    await api("POST", `/v1/wss/channel/${encodeURIComponent(id)}`, { enabled });
    await refreshStatus();
  }

  async function openConfigPath() {
    if (!paths?.config) return;
    try {
      await invoke("reveal_path", { path: paths.config });
    } catch (e) {
      setError(String(e));
    }
  }

  function openCreateBot() {
    setBotForm(emptyBotForm());
    setBotModal("create");
  }

  function openEditBot() {
    if (!selectedRoleConfig) return;
    setBotForm({
      id: String(selectedRoleConfig.id || ""),
      name: String(selectedRoleConfig.name || ""),
      backend: String(selectedRoleConfig.backend || "cursor_cli"),
      group: String(selectedRoleConfig.group || "default"),
      send_msg_url: String(selectedRoleConfig.send_msg_url || ""),
      system_prompt: String(selectedRoleConfig.system_prompt || ""),
      model: String(selectedRoleConfig.model || ""),
      openai_base_url: String(selectedRoleConfig.openai_base_url || ""),
      openai_api_key: String(selectedRoleConfig.openai_api_key || ""),
      inbound_mode: String(selectedRoleConfig.inbound_mode || "websocket"),
      workspace: String(selectedRoleConfig.workspace || ""),
    });
    setBotModal("edit");
  }

  async function submitBot() {
    if (!rawConfig) return;
    if (!botForm.id.trim() || !botForm.name.trim()) {
      setError("机器人 id / name 不能为空");
      return;
    }
    if (botForm.backend === "openai") {
      if (!botForm.openai_base_url.trim() || !botForm.openai_api_key.trim() || !botForm.model.trim()) {
        setError("OpenAI 机器人需要填写 Base URL、API Key 与模型名称");
        return;
      }
    }
    const bots = [...((rawConfig.bots as Record<string, unknown>[]) || [])];
    const payload: Record<string, unknown> = {
      id: botForm.id.trim(),
      name: botForm.name.trim(),
      backend: botForm.backend,
      inbound_mode: botForm.inbound_mode,
      system_prompt: botForm.system_prompt,
      model: botForm.model,
      workspace: botForm.workspace,
    };
    if (botForm.backend === "openai") {
      payload.openai_base_url = botForm.openai_base_url.trim();
      payload.openai_api_key = botForm.openai_api_key.trim();
    }
    if (botModal === "create") {
      if (bots.some((b) => String(b.id) === payload.id)) {
        setError("机器人 id 已存在");
        return;
      }
      payload.group = botForm.group || "default";
      payload.send_msg_url = botForm.send_msg_url;
      bots.push(payload);
    } else {
      const idx = bots.findIndex((b) => String(b.id) === selectedRoleId);
      if (idx < 0) return;
      const prev = { ...bots[idx] };
      Object.assign(prev, payload);
      if (!Array.isArray(prev.channels)) {
        prev.group = botForm.group || prev.group || "default";
        if (botForm.send_msg_url) prev.send_msg_url = botForm.send_msg_url;
      }
      bots[idx] = prev;
    }
    await saveConfig({ ...rawConfig, bots });
    setBotModal(null);
    setSelected(String(payload.id));
    void guiLog(
      botModal === "create"
        ? `新建机器人 ${payload.id}（backend=${payload.backend}）`
        : `编辑机器人 ${payload.id}`,
    );
  }

  async function deleteBot() {
    if (!rawConfig || !selectedRoleId) return;
    if (!confirm(`确认删除机器人 ${selectedRoleId}？`)) return;
    const bots = ((rawConfig.bots as Record<string, unknown>[]) || []).filter(
      (b) => String(b.id) !== selectedRoleId,
    );
    await saveConfig({ ...rawConfig, bots });
    setSelected("");
    void guiLog(`删除机器人 ${selectedRoleId}`, "WARN");
  }

  function openAddChannel() {
    setEditingChannelIdx(null);
    setChannelForm({ id: "", group: "", send_msg_url: "", model: "" });
    setChannelModal(true);
    if (String(selectedRoleConfig?.backend || "") === "openai" && !openaiModels.length) {
      void probeOpenai(
        String(selectedRoleConfig?.openai_base_url || cliForm.openai_base_url || ""),
        String(selectedRoleConfig?.openai_api_key || cliForm.openai_api_key || ""),
      );
    }
  }

  function openEditChannel(idx: number) {
    const ch = selectedChannels[idx] || {};
    setEditingChannelIdx(idx);
    setChannelForm({
      id: String(ch.id || ""),
      group: String(ch.group || ""),
      send_msg_url: String(ch.send_msg_url || ""),
      model: String(ch.model || ""),
    });
    setChannelModal(true);
    if (String(selectedRoleConfig?.backend || "") === "openai" && !openaiModels.length) {
      void probeOpenai(
        String(selectedRoleConfig?.openai_base_url || cliForm.openai_base_url || ""),
        String(selectedRoleConfig?.openai_api_key || cliForm.openai_api_key || ""),
      );
    }
  }

  async function submitChannel() {
    if (!rawConfig || !selectedRoleConfig) return;
    if (!channelForm.group.trim() || !channelForm.send_msg_url.trim()) {
      setError("通道 group / send_msg_url 不能为空");
      return;
    }
    const bots = [...((rawConfig.bots as Record<string, unknown>[]) || [])];
    const idx = bots.findIndex((b) => String(b.id) === selectedRoleId);
    if (idx < 0) return;
    const bot = { ...bots[idx] };
    let channels = Array.isArray(bot.channels)
      ? [...(bot.channels as Record<string, unknown>[])]
      : [
          {
            group: bot.group || "default",
            send_msg_url: bot.send_msg_url || "",
          },
        ];
    const entry: Record<string, unknown> = {
      group: channelForm.group.trim(),
      send_msg_url: channelForm.send_msg_url.trim(),
    };
    if (channelForm.id.trim()) entry.id = channelForm.id.trim();
    if (channelForm.model.trim()) entry.model = channelForm.model.trim();
    if (editingChannelIdx === null) channels.push(entry);
    else channels[editingChannelIdx] = entry;
    bot.channels = channels;
    delete bot.group;
    delete bot.send_msg_url;
    bots[idx] = bot;
    await saveConfig({ ...rawConfig, bots });
    setChannelModal(false);
    void guiLog(
      editingChannelIdx === null
        ? `机器人 ${selectedRoleId} 新增通道 ${entry.group}`
        : `机器人 ${selectedRoleId} 编辑通道 ${entry.group}`,
    );
  }

  async function deleteChannel(idx: number) {
    if (!rawConfig || !selectedRoleConfig) return;
    if (selectedChannels.length <= 1) {
      setError("至少保留一个通道");
      return;
    }
    if (!confirm("确认删除该通道？")) return;
    const removed = String(selectedChannels[idx]?.group || idx);
    const bots = [...((rawConfig.bots as Record<string, unknown>[]) || [])];
    const bidx = bots.findIndex((b) => String(b.id) === selectedRoleId);
    if (bidx < 0) return;
    const bot = { ...bots[bidx] };
    const channels = [...selectedChannels];
    channels.splice(idx, 1);
    bot.channels = channels;
    delete bot.group;
    delete bot.send_msg_url;
    bots[bidx] = bot;
    await saveConfig({ ...rawConfig, bots });
    void guiLog(`机器人 ${selectedRoleId} 删除通道 ${removed}`, "WARN");
  }

  async function saveCliSettings() {
    if (!rawConfig) return;
    await withMinLoading(setSavingCli, async () => {
      const defaults = { ...((rawConfig.defaults as Record<string, unknown>) || {}) };
      defaults.cursor_bin = cliForm.cursor_bin.trim() || "agent";
      defaults.claude_bin = cliForm.claude_bin.trim() || "claude";
      defaults.cursor_api_key = cliForm.cursor_api_key.trim();
      defaults.anthropic_api_key = cliForm.anthropic_api_key.trim();
      defaults.openai_api_key = cliForm.openai_api_key.trim();
      defaults.openai_base_url = cliForm.openai_base_url.trim();
      defaults.cursor_workspace = cliForm.cursor_workspace.trim() || DEFAULT_CURSOR_WORKSPACE;
      defaults.claude_workspace = cliForm.claude_workspace.trim() || DEFAULT_CLAUDE_WORKSPACE;
      defaults.projects_root = cliForm.projects_root.trim() || "~";
      defaults.workspace = cliForm.workspace.trim() || "~";
      // 引擎模型分字段保存，不写共享 defaults.model，避免 Cursor/Claude/OpenAI 互相覆盖。
      defaults.cursor_model = cliForm.cursor_model.trim();
      defaults.claude_model = cliForm.claude_model.trim();
      defaults.openai_model = cliForm.openai_model.trim();
      // One write: defaults + in-memory bot workspace edits (avoid stale overwrite).
      await saveConfig({ ...rawConfig, defaults });
      await refreshConfig();
      if (saveToastTimer.current) window.clearTimeout(saveToastTimer.current);
      setSaveToast("设置已保存");
      saveToastTimer.current = window.setTimeout(() => {
        setSaveToast("");
        saveToastTimer.current = null;
      }, 2200);
      void guiLog("保存 AI 设置成功");
    });
  }

  async function updateBotWorkspace(botId: string, workspace: string) {
    if (!rawConfig) return;
    const bots = ((rawConfig.bots as Record<string, unknown>[]) || []).map((b) =>
      String(b.id) === botId ? { ...b, workspace } : b,
    );
    setRawConfig({ ...rawConfig, bots });
  }

  const logBotOptions = useMemo(
    () => [
      { id: "", label: "全部" },
      { id: "gui", label: "GUI 操作" },
      ...status.map((s) => ({ id: s.id, label: s.id })),
    ],
    [status],
  );

  const backendOptions = BACKENDS.map((b) => ({ id: b, label: b }));
  const inboundOptions = [
    { id: "websocket", label: "websocket" },
    { id: "webhook", label: "webhook" },
    { id: "both", label: "both" },
  ];

  return (
    <div className="app" data-testid="app-root">
      {saveToast ? (
        <div className="toast ok" data-testid="save-toast" role="status">
          {saveToast}
        </div>
      ) : null}
      {(booting || !ready) && (
        <div className="boot-overlay" data-testid="boot-overlay">
          <div className="boot-card">
            <span className="spinner dark lg" />
            <strong>{booting ? "正在启动桥接服务…" : "等待桥就绪…"}</strong>
            <span>加载配置与控制通道，请稍候</span>
          </div>
        </div>
      )}
      <aside className="nav">
        <div className="brand">
          <span className="brand-mark" />
          YZJ Bridge
        </div>
        {(
          [
            ["system", "系统设置"],
            ["bots", "机器人"],
            ["settings", "AI 设置"],
            ["logs", "运行日志"],
            ["help", "帮助"],
          ] as const
        ).map(([id, label]) => (
          <button
            key={id}
            data-testid={`nav-${id}`}
            className={page === id ? "nav-btn active" : "nav-btn"}
            onClick={() => setPage(id)}
          >
            {label}
          </button>
        ))}
        <div className="nav-foot">
          <div className={`status-chip${ready ? " ok" : ""}`} data-testid="bridge-status">
            <div>{booting ? "桥启动中…" : ready ? "桥已连接" : "桥未就绪"}</div>
            <div className="status-chip-sub" data-testid="bridge-summary">
              通道 {summary.total} · 启用 {summary.enabled} · 已连接 {summary.connected}
            </div>
          </div>
          {error ? (
            <div className="err" data-testid="app-error">
              {error}
            </div>
          ) : null}
        </div>
      </aside>

      <main className="main">
        {page === "system" && (
          <section className="page" data-testid="page-system">
            <header className="page-head">
              <div>
                <h1>系统设置</h1>
                <p className="subtitle">运行控制与外观</p>
              </div>
            </header>
            <div className="stack page-body">
              <div className="card soft">
                <div className="row">
                  <div className="row-text">
                    <strong>开机自启</strong>
                    <span>写入当前用户 Run 注册表（无控制台闪烁）</span>
                  </div>
                  <Switch checked={autostart} loading={loadingAuto} onChange={toggleAutostart} />
                </div>
                <div className="row">
                  <div className="row-text">
                    <strong>全部 WebSocket</strong>
                    <span>
                      通道 {summary.total} · 启用 {summary.enabled} · 已连接 {summary.connected}
                    </span>
                  </div>
                  <Switch checked={wssAllOn} loading={loadingWss} onChange={toggleWssAll} />
                </div>
                <div className="row">
                  <div className="row-text">
                    <strong>热重载配置</strong>
                    <span>从磁盘重新加载 config.yaml，并尽量保留 WSS 启停状态</span>
                  </div>
                  <button
                    type="button"
                    data-testid="reload-btn"
                    className={`action-chip${loadingReload ? " loading" : ""}`}
                    disabled={loadingReload}
                    onClick={triggerReload}
                  >
                    {loadingReload ? <span className="spinner dark" /> : null}
                    <span>{loadingReload ? "重载中" : "立即重载"}</span>
                  </button>
                </div>
                <div className="row">
                  <div className="row-text">
                    <strong>关闭窗口行为</strong>
                    <span>
                      {closeToTray
                        ? "关闭时隐藏到托盘（托盘「退出」才停止桥）"
                        : "关闭时立即退出并停止桥"}
                    </span>
                  </div>
                  <Switch
                    testId="close-to-tray-switch"
                    checked={closeToTray}
                    loading={loadingCloseTray}
                    onChange={toggleCloseToTray}
                  />
                </div>
                <div className="row">
                  <div className="row-text">
                    <strong>界面主题</strong>
                    <span>即时切换，偏好会保存在本地</span>
                  </div>
                  <FancySelect
                    testId="theme-select"
                    value={theme}
                    options={THEMES}
                    onChange={setTheme}
                  />
                </div>
                <div className="row">
                  <div className="row-text">
                    <strong>配置路径</strong>
                    <button
                      type="button"
                      className="path-link"
                      data-testid="open-config-path"
                      onClick={openConfigPath}
                      title="在资源管理器中打开"
                    >
                      {paths?.config || "-"}
                    </button>
                  </div>
                </div>
              </div>
            </div>
          </section>
        )}

        {page === "settings" && (
          <section className="page" data-testid="page-settings">
            <header className="page-head">
              <div>
                <h1>AI 设置</h1>
                <p className="subtitle">按引擎分组的路径、密钥与目录配置</p>
              </div>
              <div className="head-actions">
                <button
                  className="btn"
                  data-testid="save-settings"
                  disabled={savingCli || !rawConfig}
                  onClick={async () => {
                    await saveCliSettings();
                  }}
                >
                  {savingCli ? "保存中…" : "保存设置"}
                </button>
              </div>
            </header>
            <div className="stack page-body settings-groups">
              <div className="card soft pad settings-group" data-testid="group-cursor">
                <h3 className="section-inline">Cursor CLI</h3>
                <p className="group-desc">agent 可执行文件、API Key、默认模型与启动目录</p>
                <div className="form-grid">
                  <label className="full">
                    可执行路径（cursor_bin）
                    <input
                      data-testid="cursor-bin"
                      value={cliForm.cursor_bin}
                      onChange={(e) => setCliForm({ ...cliForm, cursor_bin: e.target.value })}
                      placeholder="agent"
                    />
                  </label>
                  <label className="full">
                    API Key（cursor_api_key）
                    <SecretInput
                      testId="cursor-api-key"
                      value={cliForm.cursor_api_key}
                      onChange={(v) => setCliForm({ ...cliForm, cursor_api_key: v })}
                      placeholder="CURSOR_API_KEY"
                    />
                  </label>
                  <div className="field">
                    <span className="field-label">默认模型</span>
                    <div className="inline-field">
                      <FancySelect
                        testId="cursor-model"
                        className="form-select"
                        value={cliForm.cursor_model}
                        options={cursorModels}
                        disabled={loadingCursorModels}
                        placeholder={loadingCursorModels ? "拉取中…" : "选择模型"}
                        onChange={(v) => setCliForm({ ...cliForm, cursor_model: v })}
                      />
                      <button
                        type="button"
                        className={`action-chip side${loadingCursorModels ? " loading" : ""}`}
                        data-testid="refresh-cursor-models"
                        disabled={loadingCursorModels}
                        onClick={() => void refreshCursorModels()}
                      >
                        {loadingCursorModels ? <span className="spinner dark" /> : null}
                        <span>{loadingCursorModels ? "拉取中" : "刷新模型"}</span>
                      </button>
                    </div>
                    {cursorModelsHint ? (
                      <span className="field-hint error" data-testid="cursor-models-hint">
                        {cursorModelsHint}
                      </span>
                    ) : (
                      <span className="field-hint spacer" aria-hidden="true">
                        &nbsp;
                      </span>
                    )}
                  </div>
                  <div className="field">
                    <span className="field-label">默认启动目录</span>
                    <input
                      data-testid="cursor-workspace"
                      value={cliForm.cursor_workspace}
                      onChange={(e) => setCliForm({ ...cliForm, cursor_workspace: e.target.value })}
                      placeholder={DEFAULT_CURSOR_WORKSPACE}
                      title="Cursor 机器人未单独配置 workspace 时，agent 的工作目录（cwd）"
                    />
                    <span className="field-hint">
                      未单独配置机器人 workspace 时使用（agent cwd）
                    </span>
                  </div>
                </div>
                {cursorBots.length ? (
                  <div className="cli-bot-list nested">
                    <div className="nested-title">该引擎下机器人目录覆盖</div>
                    {cursorBots.map((b) => (
                      <label key={String(b.id)} className="cli-bot-row">
                        <span>
                          <strong>{String(b.name || b.id)}</strong>
                          <em>{String(b.id)}</em>
                        </span>
                        <input
                          value={String(b.workspace || "~")}
                          onChange={(e) => updateBotWorkspace(String(b.id), e.target.value)}
                          placeholder="~"
                        />
                      </label>
                    ))}
                  </div>
                ) : null}
              </div>

              <div className="card soft pad settings-group" data-testid="group-claude">
                <h3 className="section-inline">Claude Code</h3>
                <p className="group-desc">claude 可执行文件、Anthropic Key 与启动目录</p>
                <div className="form-grid">
                  <label className="full">
                    可执行路径（claude_bin）
                    <input
                      data-testid="claude-bin"
                      value={cliForm.claude_bin}
                      onChange={(e) => setCliForm({ ...cliForm, claude_bin: e.target.value })}
                      placeholder="claude"
                    />
                  </label>
                  <label className="full">
                    API Key（anthropic_api_key）
                    <SecretInput
                      testId="anthropic-api-key"
                      value={cliForm.anthropic_api_key}
                      onChange={(v) => setCliForm({ ...cliForm, anthropic_api_key: v })}
                      placeholder="ANTHROPIC_API_KEY"
                    />
                  </label>
                  <div className="field">
                    <span className="field-label">默认模型</span>
                    <div className="inline-field">
                      <FancySelect
                        testId="claude-model"
                        className="form-select"
                        value={cliForm.claude_model}
                        options={claudeModels}
                        disabled={loadingClaudeModels}
                        placeholder={loadingClaudeModels ? "拉取中…" : "选择模型"}
                        onChange={(v) => setCliForm({ ...cliForm, claude_model: v })}
                      />
                      <button
                        type="button"
                        className={`action-chip side${loadingClaudeModels ? " loading" : ""}`}
                        data-testid="refresh-claude-models"
                        disabled={loadingClaudeModels}
                        onClick={() => void refreshClaudeModels()}
                      >
                        {loadingClaudeModels ? <span className="spinner dark" /> : null}
                        <span>{loadingClaudeModels ? "拉取中" : "刷新模型"}</span>
                      </button>
                    </div>
                    {claudeModelsHint ? (
                      <span className="field-hint error" data-testid="claude-models-hint">
                        {claudeModelsHint}
                      </span>
                    ) : (
                      <span className="field-hint spacer" aria-hidden="true">
                        &nbsp;
                      </span>
                    )}
                  </div>
                  <div className="field full">
                    <span className="field-label">默认启动目录</span>
                    <input
                      data-testid="claude-workspace"
                      value={cliForm.claude_workspace}
                      onChange={(e) => setCliForm({ ...cliForm, claude_workspace: e.target.value })}
                      placeholder={DEFAULT_CLAUDE_WORKSPACE}
                      title="Claude Code 机器人未单独配置 workspace 时的工作目录（cwd）"
                    />
                    <span className="field-hint">
                      未单独配置机器人 workspace 时使用（claude cwd）
                    </span>
                  </div>
                </div>
                {claudeBots.length ? (
                  <div className="cli-bot-list nested">
                    <div className="nested-title">该引擎下机器人目录覆盖</div>
                    {claudeBots.map((b) => (
                      <label key={String(b.id)} className="cli-bot-row">
                        <span>
                          <strong>{String(b.name || b.id)}</strong>
                          <em>{String(b.id)}</em>
                        </span>
                        <input
                          value={String(b.workspace || "~")}
                          onChange={(e) => updateBotWorkspace(String(b.id), e.target.value)}
                          placeholder="~"
                        />
                      </label>
                    ))}
                  </div>
                ) : null}
              </div>

              <div className="card soft pad settings-group" data-testid="group-openai">
                <h3 className="section-inline">OpenAI 兼容</h3>
                <p className="group-desc">全局默认 Base URL、API Key 与模型（可被单个机器人/通道覆盖）</p>
                <div className="form-grid">
                  <label className="full">
                    Base URL
                    <input
                      data-testid="openai-base-url-global"
                      value={cliForm.openai_base_url}
                      onChange={(e) => setCliForm({ ...cliForm, openai_base_url: e.target.value })}
                      placeholder="https://api.openai.com/v1"
                    />
                  </label>
                  <label className="full">
                    API Key
                    <SecretInput
                      testId="openai-api-key-global"
                      value={cliForm.openai_api_key}
                      onChange={(v) => setCliForm({ ...cliForm, openai_api_key: v })}
                    />
                  </label>
                  <div className="field full">
                    <span className="field-label">模型名称（model）</span>
                    <div className="inline-field">
                      <FancySelect
                        testId="openai-model-global"
                        className="form-select"
                        value={cliForm.openai_model}
                        options={openaiModels}
                        disabled={probingOpenai}
                        placeholder={
                          probingOpenai
                            ? "测试中…"
                            : openaiModels.length
                              ? "选择模型"
                              : "填写 Base URL / API Key 后自动拉取"
                        }
                        onChange={(v) => setCliForm({ ...cliForm, openai_model: v })}
                      />
                      <button
                        type="button"
                        className={`action-chip side${probingOpenai ? " loading" : ""}`}
                        data-testid="probe-openai"
                        disabled={probingOpenai}
                        onClick={() => void probeOpenai()}
                      >
                        {probingOpenai ? <span className="spinner dark" /> : null}
                        <span>{probingOpenai ? "测试中" : "重新测试"}</span>
                      </button>
                    </div>
                    {openaiProbeInfo ? (
                      <span
                        className={`field-hint${
                          openaiProbeOk === false ? " error" : openaiProbeOk === true ? " ok" : ""
                        }`}
                        data-testid="openai-probe-info"
                      >
                        {openaiProbeInfo}
                      </span>
                    ) : null}
                  </div>
                </div>
              </div>

              <div className="card soft pad settings-group" data-testid="group-dirs">
                <h3 className="section-inline">全局目录</h3>
                <p className="group-desc">
                  工作区是机器人跑任务时的默认 cwd。projects_root 只用于解析聊天里的项目名（如
                  --project api-sms）到本机代码目录，与 YZJBridge.exe 自身打包无关。
                </p>
                <div className="form-grid">
                  <label className="full">
                    通用工作区 workspace
                    <input
                      data-testid="workspace"
                      value={cliForm.workspace}
                      onChange={(e) => setCliForm({ ...cliForm, workspace: e.target.value })}
                      placeholder="~"
                    />
                    <span className="field-hint">
                      全局兜底 cwd：机器人 / 引擎目录都未指定时使用。填 ~ 表示用户主目录（Windows 即
                      %USERPROFILE%）。当前磁盘里若仍是旧绝对路径，改完后请点「保存设置」。
                    </span>
                  </label>
                  <label className="full">
                    项目检索根目录 projects_root
                    <input
                      data-testid="projects-root"
                      value={cliForm.projects_root}
                      onChange={(e) => setCliForm({ ...cliForm, projects_root: e.target.value })}
                      placeholder="~"
                    />
                  </label>
                </div>
              </div>
            </div>
          </section>
        )}

        {page === "bots" && (
          <section className="page bots" data-testid="page-bots">
            <header className="page-head">
              <div>
                <h1>机器人</h1>
                <p className="subtitle">机器人、通道与运行状态</p>
              </div>
              <div className="head-actions">
                <button className="btn ghost" disabled={!selectedRoleConfig || saving} onClick={openEditBot}>
                  编辑机器人
                </button>
                <button className="btn" data-testid="create-bot" disabled={saving} onClick={openCreateBot}>
                  新建机器人
                </button>
              </div>
            </header>
            <div className="bots-layout page-body">
              <div className="role-rail" data-testid="role-rail">
                {roles.map(([roleId, channels]) => {
                  const primary = channels[0];
                  const active = selectedRoleId === roleId;
                  return (
                    <button
                      key={roleId}
                      data-testid={`role-${roleId}`}
                      className={`role-card${active ? " active" : ""}`}
                      onClick={() => setSelected(primary.id)}
                    >
                      <div className="role-card-top">
                        <strong>{primary.name || roleId}</strong>
                        <span className={`pill${channels.some((c) => c.connected) ? " live" : ""}`}>
                          {channels.some((c) => c.connected) ? "在线" : "离线"}
                        </span>
                      </div>
                      <div className="role-meta">
                        <span>{primary.backend}</span>
                        <span>{channels.length} 通道</span>
                      </div>
                    </button>
                  );
                })}
                {!roles.length ? <div className="empty">暂无机器人</div> : null}
              </div>

              <div className="role-panel card soft">
                {selectedItem && selectedRoleConfig ? (
                  <>
                    <div className="panel-hero">
                      <div>
                        <h2>{selectedItem.name}</h2>
                        <p>
                          {selectedRoleId} · {selectedItem.backend} · {selectedItem.inbound_mode}
                        </p>
                      </div>
                      <button className="btn danger ghost" disabled={saving} onClick={deleteBot}>
                        删除机器人
                      </button>
                    </div>

                    <div className="panel-section">
                      <div className="section-title">
                        <h3>通道</h3>
                        <button className="btn tiny" data-testid="add-channel" disabled={saving} onClick={openAddChannel}>
                          新增通道
                        </button>
                      </div>
                      <div className="channel-grid">
                        {selectedChannels.map((ch, idx) => {
                          const runtime =
                            status.find(
                              (s) =>
                                s.role_id === selectedRoleId &&
                                (s.group === String(ch.group || "") ||
                                  s.id === String(ch.id || "") ||
                                  s.id === `${selectedRoleId}__${ch.group}`),
                            ) ||
                            status.find((s) => s.role_id === selectedRoleId && selectedChannels.length === 1);
                          return (
                            <div className="channel-card" key={`${ch.group}-${idx}`}>
                              <div className="channel-card-head">
                                <strong>{String(ch.group || "default")}</strong>
                                {runtime?.has_ws ? (
                                  <Switch
                                    checked={!!runtime.ws_enabled}
                                    onChange={(v) => setChannelEnabled(runtime.id, v)}
                                  />
                                ) : (
                                  <span className="pill">无 WSS</span>
                                )}
                              </div>
                              <code className="url-line">{String(ch.send_msg_url || "-")}</code>
                              <div className="channel-card-foot">
                                <span className={`dot${runtime?.connected ? " on" : ""}`} />
                                <span>{runtime?.connected ? "已连接" : runtime?.last_error || "未连接"}</span>
                                <div className="spacer" />
                                <button className="link" onClick={() => openEditChannel(idx)}>
                                  编辑
                                </button>
                                <button className="link danger" onClick={() => deleteChannel(idx)}>
                                  删除
                                </button>
                              </div>
                            </div>
                          );
                        })}
                      </div>
                    </div>

                    <div className="panel-section">
                      <div className="section-title">
                        <h3>机器人摘要</h3>
                      </div>
                      <div className="kv">
                        <div>
                          <span>模型</span>
                          <strong>
                            {resolveDisplayedModel(selectedRoleConfig, cliForm)}
                          </strong>
                        </div>
                        <div>
                          <span>工作目录</span>
                          <strong>{String(selectedRoleConfig.workspace || "-")}</strong>
                        </div>
                      </div>
                      <pre className="prompt-preview">
                        {String(selectedRoleConfig.system_prompt || "（未设置系统提示词）")}
                      </pre>
                    </div>
                  </>
                ) : (
                  <div className="empty tall">选择或新建一个机器人开始配置</div>
                )}
              </div>
            </div>
          </section>
        )}

        {page === "logs" && (
          <section className="page logs-page" data-testid="page-logs">
            <header className="page-head">
              <div>
                <h1>运行日志</h1>
                <p className="subtitle">桥与面板事件 · 可按来源过滤</p>
              </div>
              <div className="head-actions">
                <FancySelect
                  testId="log-bot-select"
                  className="compact"
                  value={logBot}
                  options={logBotOptions}
                  onChange={setLogBot}
                />
                <button
                  className="btn ghost"
                  onClick={() => {
                    setLogs([]);
                    logSeqRef.current = 0;
                  }}
                >
                  清空视图
                </button>
              </div>
            </header>
            <pre className="logbox page-body" data-testid="logbox">
              {logs.length ? (
                logs.map((l) => {
                  const formatted = formatLogLine(l);
                  return (
                    <div
                      key={l.seq}
                      className={`log-line${formatted.tag === "GUI" ? " gui" : formatted.tag ? " bot" : ""}`}
                      data-source={formatted.tag === "GUI" ? "gui" : "bridge"}
                    >
                      <span className="log-time">{l.time}</span>{" "}
                      <span className="log-level">[{l.level}]</span>{" "}
                      {formatted.tag ? <span className="log-tag">{formatted.tag}</span> : null}{" "}
                      {formatted.message}
                    </div>
                  );
                })
              ) : (
                <div className="empty">暂无日志</div>
              )}
            </pre>
          </section>
        )}

        {page === "help" && (
          <section className="page help-page" data-testid="page-help">
            <header className="page-head help-hero">
              <div>
                <p className="help-brand">YZJ Bridge</p>
                <h1>帮助</h1>
                <p className="subtitle">把云之家会话接到本机 AI，再把结果送回群聊</p>
              </div>
            </header>
            <div className="help-layout page-body">
              <article className="help-panel help-about">
                <h2>YZJ Bridge 是什么</h2>
                <p className="help-lead">
                  云之家机器人桥接器：消息经 Go 桥收发与调度，本面板负责配置、通道启停与运行日志。支持 Cursor
                  CLI、Claude Code、OpenAI 兼容接口等本机引擎。
                </p>
                <ol className="help-flow">
                  <li>
                    <span className="help-flow-idx">01</span>
                    <div>
                      <strong>接入</strong>
                      <em>云之家 WebSocket / Webhook 入站</em>
                    </div>
                  </li>
                  <li>
                    <span className="help-flow-idx">02</span>
                    <div>
                      <strong>执行</strong>
                      <em>本机引擎按机器人配置跑任务</em>
                    </div>
                  </li>
                  <li>
                    <span className="help-flow-idx">03</span>
                    <div>
                      <strong>回写</strong>
                      <em>回复发回对应通道与会话</em>
                    </div>
                  </li>
                </ol>
              </article>
              <article className="help-panel help-repo">
                <p className="help-eyebrow">Developer</p>
                <div className="help-dev">
                  <button
                    type="button"
                    className="help-avatar-btn"
                    data-testid="developer-profile"
                    title={`打开 ${DEVELOPER_HANDLE} 的 GitHub`}
                    onClick={() => void openExternal(GITHUB_PROFILE_URL)}
                  >
                    <img
                      className="help-avatar"
                      src={DEVELOPER_AVATAR}
                      alt={DEVELOPER_NAME}
                      width={72}
                      height={72}
                      decoding="async"
                    />
                    <span className="help-avatar-ring" aria-hidden="true" />
                  </button>
                  <div className="help-dev-meta">
                    <h2>{DEVELOPER_NAME}</h2>
                    <p className="help-handle">@{DEVELOPER_HANDLE}</p>
                    <p className="help-lead help-dev-bio">
                      独立开发者 · 维护 YZJ Bridge。源码、Issue 与发布说明开源在 GitHub，欢迎 Star
                      与贡献。
                    </p>
                  </div>
                </div>
                <a
                  className="github-link"
                  data-testid="github-link"
                  href={GITHUB_URL}
                  onClick={(e) => {
                    e.preventDefault();
                    void openExternal(GITHUB_URL);
                  }}
                  title={GITHUB_URL}
                >
                  <svg className="github-icon" viewBox="0 0 24 24" aria-hidden="true">
                    <path
                      fill="currentColor"
                      d="M12 2C6.48 2 2 6.58 2 12.26c0 4.52 2.87 8.35 6.84 9.71.5.1.68-.22.68-.49 0-.24-.01-.87-.01-1.71-2.78.62-3.37-1.37-3.37-1.37-.45-1.18-1.11-1.5-1.11-1.5-.91-.64.07-.63.07-.63 1 .07 1.53 1.06 1.53 1.06.89 1.56 2.34 1.11 2.91.85.09-.66.35-1.11.63-1.37-2.22-.26-4.55-1.14-4.55-5.07 0-1.12.39-2.03 1.03-2.75-.1-.26-.45-1.31.1-2.73 0 0 .84-.27 2.75 1.05A9.3 9.3 0 0112 6.84c.85.01 1.71.12 2.51.34 1.9-1.32 2.74-1.05 2.74-1.05.55 1.42.2 2.47.1 2.73.64.72 1.03 1.63 1.03 2.75 0 3.94-2.34 4.8-4.57 5.06.36.32.68.94.68 1.9 0 1.37-.01 2.47-.01 2.81 0 .27.18.6.69.49A10.03 10.03 0 0022 12.26C22 6.58 17.52 2 12 2z"
                    />
                  </svg>
                  <span>
                    <strong>yzj_bot_bridge</strong>
                    <em>在 GitHub 打开仓库</em>
                  </span>
                  <span className="github-arrow" aria-hidden="true">
                    →
                  </span>
                </a>
              </article>
            </div>
          </section>
        )}
      </main>

      {botModal && (
        <div className="modal-backdrop" data-testid="bot-modal" onClick={() => setBotModal(null)}>
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <h3>{botModal === "create" ? "新建机器人" : "编辑机器人"}</h3>
            <div className="form-grid">
              <label>
                ID
                <input
                  data-testid="bot-id"
                  disabled={botModal === "edit"}
                  value={botForm.id}
                  onChange={(e) => setBotForm({ ...botForm, id: e.target.value })}
                  placeholder="唯一标识，如 fairy"
                />
              </label>
              <label>
                名称
                <input
                  data-testid="bot-name"
                  value={botForm.name}
                  onChange={(e) => setBotForm({ ...botForm, name: e.target.value })}
                  placeholder="显示名称，如 Fairy"
                />
              </label>
              <div className="field">
                <span className="field-label">后端引擎</span>
                <FancySelect
                  testId="bot-backend"
                  value={botForm.backend}
                  options={backendOptions}
                  onChange={(v) => {
                    setBotForm({ ...botForm, backend: v });
                    if (v === "openai" && !openaiModels.length) {
                      void probeOpenai(
                        botForm.openai_base_url || cliForm.openai_base_url,
                        botForm.openai_api_key || cliForm.openai_api_key,
                      );
                    }
                  }}
                />
              </div>
              <div className="field">
                <span className="field-label">入站模式</span>
                <FancySelect
                  value={botForm.inbound_mode}
                  options={inboundOptions}
                  onChange={(v) => setBotForm({ ...botForm, inbound_mode: v })}
                />
              </div>
              {botModal === "create" || !Array.isArray(selectedRoleConfig?.channels) ? (
                <>
                  <div className="field">
                    <span className="field-label">分组 Group</span>
                    <input
                      value={botForm.group}
                      onChange={(e) => setBotForm({ ...botForm, group: e.target.value })}
                      placeholder="如 workAssistant"
                    />
                    <span className="field-hint">云之家通道分组标识，多通道时用于区分会话来源</span>
                  </div>
                  <div className="field full">
                    <span className="field-label">发送地址 send_msg_url</span>
                    <input
                      data-testid="bot-send-url"
                      value={botForm.send_msg_url}
                      onChange={(e) => setBotForm({ ...botForm, send_msg_url: e.target.value })}
                      placeholder="https://www.yunzhijia.com/gateway/robot/webhook/send?..."
                    />
                    <span className="field-hint">云之家机器人 Webhook 发送 URL（含 yzjtoken）</span>
                  </div>
                </>
              ) : null}
              <div className="field full">
                <span className="field-label">启动工作目录 workspace</span>
                <input
                  value={botForm.workspace}
                  onChange={(e) => setBotForm({ ...botForm, workspace: e.target.value })}
                  placeholder="本机路径或 ~ ；留空则用引擎/全局默认目录"
                />
                <span className="field-hint">机器人跑任务时的工作目录（cwd）</span>
              </div>
              {botForm.backend === "openai" ? (
                <>
                  <label className="full">
                    Base URL
                    <input
                      data-testid="openai-base-url"
                      value={botForm.openai_base_url}
                      onChange={(e) => setBotForm({ ...botForm, openai_base_url: e.target.value })}
                      placeholder="https://api.openai.com/v1"
                    />
                  </label>
                  <label className="full">
                    API Key
                    <SecretInput
                      testId="openai-api-key"
                      value={botForm.openai_api_key}
                      onChange={(v) => setBotForm({ ...botForm, openai_api_key: v })}
                    />
                  </label>
                  <div className="field full">
                    <span className="field-label">模型 Model</span>
                    <div className="inline-field">
                      <FancySelect
                        testId="openai-model"
                        className="form-select"
                        value={botForm.model}
                        options={openaiModels}
                        disabled={probingOpenai}
                        placeholder={
                          probingOpenai
                            ? "测试中…"
                            : openaiModels.length
                              ? "选择模型"
                              : "点击选择（首次自动测试连通）"
                        }
                        onChange={(v) => setBotForm({ ...botForm, model: v })}
                        onOpen={() => {
                          if (!probingOpenai && !openaiModels.length) {
                            void probeOpenai(botForm.openai_base_url, botForm.openai_api_key);
                          }
                        }}
                      />
                      <button
                        type="button"
                        className={`action-chip side${probingOpenai ? " loading" : ""}`}
                        data-testid="probe-openai-bot"
                        disabled={probingOpenai}
                        onClick={() =>
                          void probeOpenai(botForm.openai_base_url, botForm.openai_api_key)
                        }
                      >
                        {probingOpenai ? <span className="spinner dark" /> : null}
                        <span>{probingOpenai ? "测试中" : "重新测试"}</span>
                      </button>
                    </div>
                    <span className="field-hint">该机器人实际调用的大模型名称（如 gpt-4o-mini）</span>
                  </div>
                </>
              ) : (
                <div className="field">
                  <span className="field-label">模型 Model</span>
                  <input
                    value={botForm.model}
                    onChange={(e) => setBotForm({ ...botForm, model: e.target.value })}
                    placeholder="可留空，使用 AI 设置中的默认模型"
                  />
                  <span className="field-hint">
                    指定本机器人使用的模型 ID；留空则回退到 AI 设置里的默认模型
                  </span>
                </div>
              )}
              <div className="field full">
                <span className="field-label">系统提示词 System Prompt</span>
                <textarea
                  rows={5}
                  value={botForm.system_prompt}
                  onChange={(e) => setBotForm({ ...botForm, system_prompt: e.target.value })}
                  placeholder="定义机器人人设、回答风格与约束"
                />
                <span className="field-hint">每次对话前注入给模型的角色说明与行为约束</span>
              </div>
            </div>
            <div className="modal-actions">
              <button className="btn ghost" onClick={() => setBotModal(null)}>
                取消
              </button>
              <button className="btn" data-testid="save-bot" disabled={saving} onClick={submitBot}>
                {saving ? "保存中…" : "保存"}
              </button>
            </div>
          </div>
        </div>
      )}

      {channelModal && (
        <div className="modal-backdrop" data-testid="channel-modal" onClick={() => setChannelModal(false)}>
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <h3>{editingChannelIdx === null ? "新增通道" : "编辑通道"}</h3>
            <div className="form-grid">
              <div className="field">
                <span className="field-label">自定义通道 ID（可选）</span>
                <input
                  value={channelForm.id}
                  onChange={(e) => setChannelForm({ ...channelForm, id: e.target.value })}
                />
              </div>
              <div className="field">
                <span className="field-label">分组 Group</span>
                <input
                  value={channelForm.group}
                  onChange={(e) => setChannelForm({ ...channelForm, group: e.target.value })}
                  placeholder="如 workAssistant"
                />
                <span className="field-hint">通道分组标识，用于区分同一机器人下的不同会话入口</span>
              </div>
              <div className="field full">
                <span className="field-label">发送地址 send_msg_url</span>
                <input
                  value={channelForm.send_msg_url}
                  onChange={(e) => setChannelForm({ ...channelForm, send_msg_url: e.target.value })}
                />
                <span className="field-hint">该通道对应的云之家机器人 Webhook 发送 URL</span>
              </div>
              {String(selectedRoleConfig?.backend || "") === "openai" ? (
                <div className="field full">
                  <span className="field-label">通道模型（可选，覆盖机器人默认）</span>
                  <div className="inline-field">
                    <FancySelect
                      testId="channel-openai-model"
                      className="form-select"
                      value={channelForm.model}
                      options={[
                        { id: "", label: "使用角色默认模型" },
                        ...openaiModels,
                      ]}
                      disabled={probingOpenai}
                      placeholder={
                        probingOpenai
                          ? "测试中…"
                          : openaiModels.length
                            ? "选择模型"
                            : "点击选择（首次自动测试连通）"
                      }
                      onChange={(v) => setChannelForm({ ...channelForm, model: v })}
                      onOpen={() => {
                        if (!probingOpenai && !openaiModels.length) {
                          void probeOpenai(
                            String(selectedRoleConfig?.openai_base_url || cliForm.openai_base_url || ""),
                            String(selectedRoleConfig?.openai_api_key || cliForm.openai_api_key || ""),
                          );
                        }
                      }}
                    />
                    <button
                      type="button"
                      className={`action-chip side${probingOpenai ? " loading" : ""}`}
                      data-testid="probe-openai-channel"
                      disabled={probingOpenai}
                      onClick={() =>
                        void probeOpenai(
                          String(selectedRoleConfig?.openai_base_url || cliForm.openai_base_url || ""),
                          String(selectedRoleConfig?.openai_api_key || cliForm.openai_api_key || ""),
                        )
                      }
                    >
                      {probingOpenai ? <span className="spinner dark" /> : null}
                      <span>{probingOpenai ? "测试中" : "重新测试"}</span>
                    </button>
                  </div>
                </div>
              ) : null}
            </div>
            <div className="modal-actions">
              <button className="btn ghost" onClick={() => setChannelModal(false)}>
                取消
              </button>
              <button className="btn" disabled={saving} onClick={submitChannel}>
                {saving ? "保存中…" : "保存"}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

export default App;
