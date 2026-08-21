import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { invoke } from "@tauri-apps/api/core";
import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { ChatPage } from "./ChatPage";
import { FancySelect } from "./FancySelect";
import { MemoryPage } from "./MemoryPage";
import { findWebhookConflict } from "./webhookUnique";
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
type PageId = "chat" | "memory" | "system" | "bots" | "settings" | "skills" | "logs" | "help";

type SkillInfo = {
  id: string;
  name: string;
  version?: string;
  description?: string;
  author?: string;
  tags?: string[];
  dir?: string;
};

type UpdateCheckResult = {
  available: boolean;
  currentVersion: string;
  latestVersion: string;
  notes: string;
  downloadUrl: string;
  publishedAt: string;
  skipped: boolean;
  message: string;
};

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
  /** 为 true 时 Base URL / API Key / model 使用 AI 设置中的全局默认。 */
  openai_use_defaults: boolean;
  skills: string[];
  inbound_mode: string;
  session_mode: string;
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

function defaultBotWorkspace(botId: string): string {
  const id = botId.trim();
  return id ? `~/.yzj-bridge/workspace/${id}` : "~/.yzj-bridge/workspace/";
}

function formatBotSkillsSummary(skills: unknown, installed: SkillInfo[]): string {
  const ids = Array.isArray(skills)
    ? skills.map((x) => String(x).trim()).filter(Boolean)
    : [];
  if (!ids.length) {
    return "（未配置）";
  }
  return ids
    .map((id) => {
      const sk = installed.find((s) => s.id === id);
      if (sk?.name && sk.name !== id) {
        return `${sk.name} (${id})`;
      }
      return id;
    })
    .join("、");
}

function FieldLabel({ children, tip }: { children: React.ReactNode; tip?: string }) {
  const iconRef = useRef<HTMLSpanElement | null>(null);
  const [open, setOpen] = useState(false);
  const [coords, setCoords] = useState<{ left: number; top: number; place: "above" | "below" }>({
    left: 0,
    top: 0,
    place: "above",
  });

  const updatePosition = useCallback(() => {
    const el = iconRef.current;
    if (!el) return;
    const rect = el.getBoundingClientRect();
    const gap = 8;
    const preferAbove = rect.top > 88;
    if (preferAbove) {
      setCoords({ left: rect.left + rect.width / 2, top: rect.top - gap, place: "above" });
    } else {
      setCoords({ left: rect.left + rect.width / 2, top: rect.bottom + gap, place: "below" });
    }
  }, []);

  useLayoutEffect(() => {
    if (!open) return;
    updatePosition();
    const onReposition = () => updatePosition();
    window.addEventListener("scroll", onReposition, true);
    window.addEventListener("resize", onReposition);
    return () => {
      window.removeEventListener("scroll", onReposition, true);
      window.removeEventListener("resize", onReposition);
    };
  }, [open, updatePosition]);

  if (!tip) {
    return <span className="field-label">{children}</span>;
  }

  return (
    <span className="field-label with-tip">
      <span className="field-label-text">{children}</span>
      <span
        className={`field-tip${open ? " open" : ""}`}
        onMouseEnter={() => {
          updatePosition();
          setOpen(true);
        }}
        onMouseLeave={() => setOpen(false)}
        onFocus={() => {
          updatePosition();
          setOpen(true);
        }}
        onBlur={() => setOpen(false)}
      >
        <span ref={iconRef} className="field-tip-icon" tabIndex={0} aria-label="说明">
          ?
        </span>
        {open
          ? createPortal(
              <span
                className={`field-tip-bubble portal ${coords.place}`}
                role="tooltip"
                style={{ left: coords.left, top: coords.top }}
              >
                {tip}
              </span>,
              document.body,
            )
          : null}
      </span>
    </span>
  );
}

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

async function openLocalPath(path: string) {
  try {
    await invoke("open_path_default", { path });
    return;
  } catch {
    /* fall through */
  }
  try {
    const mod = await import("@tauri-apps/plugin-opener");
    await mod.openPath(path);
  } catch {
    await invoke("reveal_path", { path });
  }
}

function detectSkillInstallSource(path: string): "dir" | "zip" | "tgz" | "md" {
  const lower = path.trim().toLowerCase();
  if (lower.endsWith(".tar.gz") || lower.endsWith(".tgz")) return "tgz";
  if (lower.endsWith(".zip")) return "zip";
  if (lower.endsWith(".md")) return "md";
  return "dir";
}

async function pickSkillDirectory(): Promise<string | null> {
  const { open } = await import("@tauri-apps/plugin-dialog");
  const selected = await open({
    directory: true,
    multiple: false,
    title: "选择 Skill 目录（需含 SKILL.md）",
  });
  return typeof selected === "string" ? selected : null;
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

/** 弹窗内悬浮错误：固定在可视区中上，约 3 秒后带退场动画消失。 */
function ModalFloatMessage({
  message,
  testId,
  onDismissed,
}: {
  message: string;
  testId?: string;
  onDismissed: () => void;
}) {
  const [phase, setPhase] = useState<"enter" | "exit">("enter");
  const onDismissedRef = useRef(onDismissed);
  onDismissedRef.current = onDismissed;

  useEffect(() => {
    setPhase("enter");
    const exitTimer = window.setTimeout(() => setPhase("exit"), 3000);
    return () => window.clearTimeout(exitTimer);
  }, [message]);

  useEffect(() => {
    if (phase !== "exit") return;
    const done = window.setTimeout(() => onDismissedRef.current(), 220);
    return () => window.clearTimeout(done);
  }, [phase]);

  return (
    <div
      className={`modal-float-msg ${phase}`}
      data-testid={testId}
      role="alert"
    >
      {message}
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
    openai_use_defaults: true,
    skills: [],
    inbound_mode: "websocket",
    session_mode: "shared",
    workspace: defaultBotWorkspace(""),
  };
}

/** 机器人未写 OpenAI 三项覆盖时，视为使用 AI 设置默认。 */
function botUsesOpenaiDefaults(cfg: Record<string, unknown>): boolean {
  return (
    !String(cfg.openai_base_url || "").trim() &&
    !String(cfg.openai_api_key || "").trim() &&
    !String(cfg.model || "").trim()
  );
}

function BrandMark() {
  return (
    <svg
      className="brand-mark"
      width="28"
      height="18"
      viewBox="0 0 36 22"
      fill="none"
      aria-hidden
      data-testid="brand-mark"
    >
      <g stroke="currentColor" strokeLinejoin="miter">
        <path d="M1.4 20.6h33.2" strokeWidth="1.45" strokeLinecap="square" />
        <path
          d="M3 20.6V5.4H2.15V3.65h2.15V4.7h1.55V3.65h2.15V5.4H7.15V20.6"
          strokeWidth="1.45"
          strokeLinecap="square"
        />
        <path
          d="M28.85 20.6V5.4h.85V3.65h2.15V4.7h1.55V3.65h2.15V5.4H33V20.6"
          strokeWidth="1.45"
          strokeLinecap="square"
        />
        <path d="M5.1 8.35v2.15M30.9 8.35v2.15" strokeWidth="1.35" strokeLinecap="square" />
        <path d="M7.15 13.85h21.7M7.15 14.9h21.7" strokeWidth="1.35" strokeLinecap="square" />
        <path d="M5.1 5.4Q18 20.2 30.9 5.4" strokeWidth="1.25" strokeLinecap="round" />
        <path d="M5.1 6.35Q18 19.35 30.9 6.35" strokeWidth="1.1" strokeLinecap="round" />
        <path
          d="M10 9.96v3.89M14 12.09v1.76M18 12.8v1.05M22 12.09v1.76M26 9.96v3.89"
          strokeWidth="1.05"
          strokeLinecap="square"
        />
      </g>
    </svg>
  );
}

function NavIcon({ id }: { id: PageId }) {
  const stroke = {
    stroke: "currentColor",
    strokeWidth: 1.7,
    strokeLinecap: "round" as const,
    strokeLinejoin: "round" as const,
  };
  return (
    <svg className="nav-icon" width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden>
      {id === "chat" ? (
        <path
          {...stroke}
          d="M6.5 5.75h11a2.25 2.25 0 012.25 2.25v7a2.25 2.25 0 01-2.25 2.25H11.2L7 19.6v-2.35H6.5A2.25 2.25 0 014.25 15V8A2.25 2.25 0 016.5 5.75z"
        />
      ) : null}
      {id === "memory" ? (
        <>
          <path
            {...stroke}
            d="M8.2 6.2h7.6A2.3 2.3 0 0118.1 8.5v7A2.3 2.3 0 0115.8 17.8H8.2A2.3 2.3 0 015.9 15.5v-7A2.3 2.3 0 018.2 6.2z"
          />
          <path {...stroke} d="M9.4 10.2h5.2M9.4 13h3.6" />
          <circle cx="12" cy="7.4" r="0.9" fill="currentColor" />
        </>
      ) : null}
      {id === "bots" ? (
        <>
          <rect {...stroke} x="6.25" y="8" width="11.5" height="10.25" rx="3.2" />
          <path {...stroke} d="M12 8V5.6" />
          <circle cx="12" cy="4.7" r="1" stroke="currentColor" strokeWidth="1.5" />
          <path {...stroke} d="M9.6 13.1h.01M14.4 13.1h.01" />
        </>
      ) : null}
      {id === "settings" ? (
        <>
          <path
            {...stroke}
            d="M12 4.2l.95 3.55L16.5 8.7l-2.8 2.05.95 3.55L12 12.45 8.35 14.3l.95-3.55L6.5 8.7l3.55-.95L12 4.2z"
          />
          <path
            {...stroke}
            d="M18.35 14.35l.45 1.7 1.75.45-1.75.45-.45 1.7-.45-1.7-1.75-.45 1.75-.45.45-1.7z"
          />
        </>
      ) : null}
      {id === "skills" ? (
        <>
          <rect {...stroke} x="8.1" y="8.2" width="10.4" height="10.4" rx="2.1" />
          <path
            {...stroke}
            d="M15.7 8.2V6.4A2.15 2.15 0 0013.55 4.25H7.4A2.15 2.15 0 005.25 6.4v6.15A2.15 2.15 0 007.4 14.7h1.7"
          />
        </>
      ) : null}
      {id === "logs" ? (
        <>
          <path
            {...stroke}
            d="M7.2 4.5h9.1A2.3 2.3 0 0118.6 6.8v10.4a2.3 2.3 0 01-2.3 2.3H7.2A2.2 2.2 0 015 17.3V6.7A2.2 2.2 0 017.2 4.5z"
          />
          <path {...stroke} d="M8.6 9.2h6.8M8.6 12.4h6.8M8.6 15.6h4.4" />
        </>
      ) : null}
      {id === "help" ? (
        <>
          <circle {...stroke} cx="12" cy="12" r="8.15" />
          <path {...stroke} d="M9.55 9.45a2.45 2.45 0 114.15 1.75c-.7.55-1.45 1-1.45 2.15" />
          <path {...stroke} d="M12 16.85h.01" />
        </>
      ) : null}
      {id === "system" ? (
        <>
          <circle {...stroke} cx="12" cy="12" r="3.05" />
          <path
            {...stroke}
            d="M12 5.15v1.7M12 17.15v1.7M5.15 12h1.7M17.15 12h1.7M7.05 7.05l1.2 1.2M15.75 15.75l1.2 1.2M7.05 16.95l1.2-1.2M15.75 8.25l1.2-1.2"
          />
        </>
      ) : null}
    </svg>
  );
}

function LogMinimap({
  logs,
  logboxRef,
  theme,
}: {
  logs: LogLine[];
  logboxRef: { current: HTMLPreElement | null };
  theme: ThemeId;
}) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const trackRef = useRef<HTMLDivElement>(null);
  const [overflow, setOverflow] = useState(false);
  const [peek, setPeek] = useState(false);
  const [slider, setSlider] = useState({ top: 0, height: 28 });
  const dragRef = useRef<{ active: boolean; fromSlider: boolean; grab: number }>({
    active: false,
    fromSlider: false,
    grab: 0,
  });
  const hoverRef = useRef(false);
  const hideTimer = useRef<number | null>(null);
  const sliderRef = useRef(slider);
  sliderRef.current = slider;

  const keepPeek = useCallback(() => {
    setPeek(true);
    if (hideTimer.current) window.clearTimeout(hideTimer.current);
    hideTimer.current = window.setTimeout(() => {
      if (!dragRef.current.active && !hoverRef.current) setPeek(false);
    }, 900);
  }, []);

  const syncSlider = useCallback(() => {
    const box = logboxRef.current;
    if (!box) return;
    const { scrollTop, scrollHeight, clientHeight } = box;
    const hasOverflow = scrollHeight > clientHeight + 8 && logs.length > 0;
    setOverflow(hasOverflow);
    if (!hasOverflow) {
      setPeek(false);
      return;
    }
    const h = box.clientHeight;
    const height = Math.max(22, (clientHeight / scrollHeight) * h);
    const maxTop = Math.max(0, h - height);
    const range = Math.max(1, scrollHeight - clientHeight);
    const top = maxTop * (scrollTop / range);
    setSlider({ top, height });
  }, [logboxRef, logs.length]);

  const paint = useCallback(() => {
    const canvas = canvasRef.current;
    const box = logboxRef.current;
    if (!canvas || !box) return;
    const w = canvas.clientWidth;
    const h = canvas.clientHeight;
    if (w < 2 || h < 2) return;
    const dpr = Math.min(2, window.devicePixelRatio || 1);
    canvas.width = Math.round(w * dpr);
    canvas.height = Math.round(h * dpr);
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, w, h);
    if (!logs.length) return;

    const cs = getComputedStyle(box);
    const muted = cs.getPropertyValue("--muted").trim() || "#8eaea0";
    const text = cs.getPropertyValue("--text").trim() || "#e7f5ee";
    const accent = cs.getPropertyValue("--accent").trim() || "#3dd68c";
    const accent2 = cs.getPropertyValue("--accent-2").trim() || accent;
    const danger = cs.getPropertyValue("--danger").trim() || "#ff6b7a";
    const row = h / logs.length;
    const fontPx = Math.min(3.5, Math.max(1.35, row * 0.9));
    ctx.font = `${fontPx}px ui-monospace, Consolas, "Cascadia Code", monospace`;
    ctx.textBaseline = "top";

    for (let i = 0; i < logs.length; i++) {
      const l = logs[i];
      const formatted = formatLogLine(l);
      const lv = (l.level || "").toUpperCase();
      if (lv === "ERROR" || lv === "FATAL") ctx.fillStyle = danger;
      else if (lv === "WARN" || lv === "WARNING") ctx.fillStyle = accent2;
      else if (formatted.tag === "GUI") ctx.fillStyle = accent;
      else if (formatted.tag) ctx.fillStyle = text;
      else ctx.fillStyle = muted;
      const preview = `${l.time || ""} ${formatted.tag ? `${formatted.tag} ` : ""}${formatted.message}`.replace(
        /\s+/g,
        " ",
      );
      ctx.globalAlpha = 0.82;
      ctx.fillText(preview.slice(0, 52), 3, i * row, w - 5);
    }
    ctx.globalAlpha = 1;
  }, [logs, logboxRef, theme]);

  const scrollToY = useCallback(
    (clientY: number) => {
      const track = trackRef.current;
      const box = logboxRef.current;
      if (!track || !box) return;
      const rect = track.getBoundingClientRect();
      const max = Math.max(0, box.scrollHeight - box.clientHeight);
      if (max <= 0) return;
      const drag = dragRef.current;
      if (drag.fromSlider) {
        const sh = sliderRef.current.height;
        const maxTop = Math.max(1, rect.height - sh);
        const top = Math.min(Math.max(clientY - rect.top - drag.grab, 0), maxTop);
        box.scrollTop = (top / maxTop) * max;
        return;
      }
      const y = Math.min(Math.max(clientY - rect.top, 0), rect.height);
      box.scrollTop = (y / Math.max(1, rect.height)) * box.scrollHeight - box.clientHeight / 2;
      if (box.scrollTop < 0) box.scrollTop = 0;
      if (box.scrollTop > max) box.scrollTop = max;
    },
    [logboxRef],
  );

  useLayoutEffect(() => {
    if (!overflow) return;
    paint();
    syncSlider();
  }, [overflow, paint, syncSlider, logs]);

  useEffect(() => {
    const box = logboxRef.current;
    if (!box) return;
    const onUserScroll = () => keepPeek();
    const onScroll = () => syncSlider();
    box.addEventListener("wheel", onUserScroll, { passive: true });
    box.addEventListener("touchmove", onUserScroll, { passive: true });
    box.addEventListener("scroll", onScroll, { passive: true });
    const ro = new ResizeObserver(() => {
      paint();
      syncSlider();
    });
    ro.observe(box);
    paint();
    syncSlider();
    return () => {
      box.removeEventListener("wheel", onUserScroll);
      box.removeEventListener("touchmove", onUserScroll);
      box.removeEventListener("scroll", onScroll);
      ro.disconnect();
      if (hideTimer.current) window.clearTimeout(hideTimer.current);
    };
  }, [logboxRef, paint, syncSlider, keepPeek]);

  useEffect(() => {
    const onMove = (e: PointerEvent) => {
      if (!dragRef.current.active) return;
      scrollToY(e.clientY);
    };
    const onUp = () => {
      if (!dragRef.current.active) return;
      dragRef.current.active = false;
      keepPeek();
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
    return () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
    };
  }, [scrollToY, keepPeek]);

  if (!overflow) return null;

  return (
    <div
      ref={trackRef}
      className={`log-minimap${peek ? " peek" : ""}`}
      data-testid="log-minimap"
      data-visible={peek ? "1" : "0"}
      aria-hidden={peek ? "false" : "true"}
      title="滚动预览"
      onPointerEnter={() => {
        hoverRef.current = true;
        if (peek) keepPeek();
      }}
      onPointerLeave={() => {
        hoverRef.current = false;
        if (hideTimer.current) window.clearTimeout(hideTimer.current);
        hideTimer.current = window.setTimeout(() => {
          if (!dragRef.current.active && !hoverRef.current) setPeek(false);
        }, 900);
      }}
      onPointerDown={(e) => {
        if (e.button !== 0) return;
        e.preventDefault();
        const sliderEl = e.currentTarget.querySelector(".log-minimap-slider") as HTMLElement | null;
        const sliderRect = sliderEl?.getBoundingClientRect();
        const fromSlider = !!(
          sliderRect &&
          e.clientY >= sliderRect.top &&
          e.clientY <= sliderRect.bottom
        );
        dragRef.current = {
          active: true,
          fromSlider,
          grab: fromSlider && sliderRect ? e.clientY - sliderRect.top : 0,
        };
        keepPeek();
        scrollToY(e.clientY);
      }}
    >
      <canvas ref={canvasRef} className="log-minimap-canvas" />
      <div
        className="log-minimap-slider"
        style={{ transform: `translateY(${slider.top}px)`, height: slider.height }}
      />
    </div>
  );
}

function App() {
  const [page, setPage] = useState<PageId>("bots");
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
  const logboxRef = useRef<HTMLPreElement>(null);
  const logBottomRef = useRef<HTMLDivElement>(null);
  const logNearBottomRef = useRef(true);
  const [showLogScrollBottom, setShowLogScrollBottom] = useState(false);
  const [logBot, setLogBot] = useState("");
  const [selected, setSelected] = useState("");
  const [rawConfig, setRawConfig] = useState<Record<string, unknown> | null>(null);

  const [loadingAuto, setLoadingAuto] = useState(false);
  const [loadingReload, setLoadingReload] = useState(false);
  const [savingCli, setSavingCli] = useState(false);
  const [saveToast, setSaveToast] = useState("");
  const saveToastTimer = useRef<number | null>(null);
  const [closeToTray, setCloseToTray] = useState(true);
  const [loadingCloseTray, setLoadingCloseTray] = useState(false);
  const [appVersion, setAppVersion] = useState("");
  const [updateInfo, setUpdateInfo] = useState<UpdateCheckResult | null>(null);
  const [updateModal, setUpdateModal] = useState(false);
  const [checkingUpdate, setCheckingUpdate] = useState(false);
  const [updatingApp, setUpdatingApp] = useState(false);
  const updateAutoChecked = useRef(false);
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
  const [botModalError, setBotModalError] = useState<{ text: string; seq: number } | null>(null);
  const [botFieldErrors, setBotFieldErrors] = useState<Record<string, string>>({});
  const [botWorkspaceTouched, setBotWorkspaceTouched] = useState(false);
  const [channelForm, setChannelForm] = useState<ChannelForm>({
    id: "",
    group: "",
    send_msg_url: "",
    model: "",
  });
  const [channelModalError, setChannelModalError] = useState<{ text: string; seq: number } | null>(
    null,
  );
  const [editingChannelIdx, setEditingChannelIdx] = useState<number | null>(null);
  const [saving, setSaving] = useState(false);
  const [installedSkills, setInstalledSkills] = useState<SkillInfo[]>([]);
  const [skillsBusy, setSkillsBusy] = useState(false);
  const [skillInstallPath, setSkillInstallPath] = useState("");
  const [skillsPageError, setSkillsPageError] = useState<{ text: string; seq: number } | null>(null);
  const [skillDropActive, setSkillDropActive] = useState(false);

  const [cliForm, setCliForm] = useState({
    cursor_bin: "agent",
    claude_bin: "claude",
    cursor_api_key: "",
    anthropic_api_key: "",
    openai_api_key: "",
    openai_base_url: "",
    projects_root: "~",
    cursor_model: "",
    claude_model: "",
    openai_model: "",
    memory_enabled: false,
    memory_gui_bind_enabled: false,
  });
  const [memoryEnableHint, setMemoryEnableHint] = useState("");
  const [memoryEnableBusy, setMemoryEnableBusy] = useState(false);
  const [cursorDiscover, setCursorDiscover] = useState<{
    found: boolean;
    path?: string;
    version?: string;
    message?: string;
    install?: { shell: string; command: string; hint: string };
  } | null>(null);
  const [claudeDiscover, setClaudeDiscover] = useState<{
    found: boolean;
    path?: string;
    version?: string;
    message?: string;
    install?: { shell: string; command: string; hint: string };
  } | null>(null);
  const [discoveringCursor, setDiscoveringCursor] = useState(false);
  const [discoveringClaude, setDiscoveringClaude] = useState(false);
  const [installingCursor, setInstallingCursor] = useState(false);
  const [installingClaude, setInstallingClaude] = useState(false);
  const cliDiscoverTried = useRef(false);

  useEffect(() => {
    document.documentElement.setAttribute("data-theme", theme);
    localStorage.setItem("yzj-theme", theme);
  }, [theme]);

  useEffect(() => {
    if (!botModal && !channelModal && !updateModal) return;
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (e.key === "Escape") {
        if (updatingApp) return;
        setBotModal(null);
        setChannelModal(false);
        setUpdateModal(false);
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [botModal, channelModal, updateModal, updatingApp]);

  const showToast = useCallback((msg: string) => {
    if (saveToastTimer.current) window.clearTimeout(saveToastTimer.current);
    setSaveToast(msg);
    saveToastTimer.current = window.setTimeout(() => {
      setSaveToast("");
      saveToastTimer.current = null;
    }, 2200);
  }, []);

  const runCheckForUpdate = useCallback(async (force: boolean) => {
    const info = await invoke<UpdateCheckResult>("check_for_update", { force });
    if (info.available) {
      setUpdateInfo(info);
      setUpdateModal(true);
    }
    return info;
  }, []);

  const checkUpdateManual = useCallback(async () => {
    setCheckingUpdate(true);
    try {
      const info = await runCheckForUpdate(true);
      if (info.available) {
        return;
      }
      if (info.message?.trim()) {
        showToast(info.message.trim());
        return;
      }
      showToast(`已是最新（v${info.currentVersion || appVersion || "?"}）`);
    } catch (e) {
      showToast(`检查更新失败：${e}`);
    } finally {
      setCheckingUpdate(false);
    }
  }, [appVersion, runCheckForUpdate, showToast]);

  const skipUpdateVersion = useCallback(async () => {
    if (!updateInfo?.latestVersion) {
      setUpdateModal(false);
      return;
    }
    try {
      await invoke("set_skipped_update_version", { version: updateInfo.latestVersion });
      setUpdateModal(false);
      showToast(`已跳过 v${updateInfo.latestVersion}`);
    } catch (e) {
      showToast(`跳过失败：${e}`);
    }
  }, [showToast, updateInfo]);

  const confirmUpdate = useCallback(async () => {
    if (!updateInfo?.downloadUrl) return;
    setUpdatingApp(true);
    try {
      await invoke("download_and_launch_update", { downloadUrl: updateInfo.downloadUrl });
    } catch (e) {
      setUpdatingApp(false);
      showToast(`更新失败：${e}`);
    }
  }, [showToast, updateInfo]);

  useEffect(() => {
    if (!ready || updateAutoChecked.current) return;
    // tauri/vite 本地开发不自动弹更新，避免干扰调试；系统设置里仍可手动检查。
    if (import.meta.env.DEV) {
      updateAutoChecked.current = true;
      return;
    }
    updateAutoChecked.current = true;
    let cancelled = false;
    void (async () => {
      try {
        const info = await invoke<UpdateCheckResult>("check_for_update", { force: false });
        if (cancelled || !info.available) {
          return;
        }
        setUpdateInfo(info);
        setUpdateModal(true);
      } catch {
        // 启动自动检测失败静默忽略
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [ready]);

  const boot = useCallback(async () => {
    setBooting(true);
    setError("");
    let lastErr = "";
    // ensure_bridge already waits up to ~15s; keep retries low to avoid minutes-long spinner.
    for (let i = 0; i < 3; i++) {
      try {
        await invoke("ensure_bridge");
        const [auto, tray, version] = await Promise.all([
          invoke<boolean>("get_autostart"),
          invoke<boolean>("get_close_to_tray"),
          invoke<string>("get_app_version"),
        ]);
        setAutostart(auto);
        setCloseToTray(tray);
        setAppVersion(version);
        setReady(true);
        setError("");
        setBooting(false);
        return;
      } catch (e) {
        lastErr = String(e);
        if (i < 2) {
          await new Promise((r) => setTimeout(r, 500));
        }
      }
    }
    setReady(false);
    setBooting(false);
    setError(lastErr || "桥启动超时，请检查 yzj-bridge.exe 与 ~/.yzj-bridge/config.yaml");
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

  const discoverCli = useCallback(
    async (engine: "cursor" | "claude", opts?: { autofill?: boolean }) => {
      const autofill = opts?.autofill !== false;
      const setBusy = engine === "cursor" ? setDiscoveringCursor : setDiscoveringClaude;
      const setResult = engine === "cursor" ? setCursorDiscover : setClaudeDiscover;
      const currentBin = engine === "cursor" ? cliForm.cursor_bin : cliForm.claude_bin;
      setBusy(true);
      try {
        const raw = await api("POST", "/v1/backends/cli/discover", {
          engine,
          bin: currentBin,
        });
        const data = JSON.parse(raw) as {
          found?: boolean;
          path?: string;
          version?: string;
          message?: string;
          install?: { shell: string; command: string; hint: string };
        };
        setResult({
          found: !!data.found,
          path: data.path,
          version: data.version,
          message: data.message,
          install: data.install,
        });
        if (autofill && data.found && data.path) {
          const cur = currentBin.trim();
          const bare =
            !cur ||
            cur === "agent" ||
            cur === "claude" ||
            cur === "cursor-agent" ||
            (!cur.includes("/") && !cur.includes("\\") && !cur.includes(":"));
          if (bare && data.path !== cur) {
            if (engine === "cursor") {
              setCliForm((prev) => ({ ...prev, cursor_bin: data.path || prev.cursor_bin }));
            } else {
              setCliForm((prev) => ({ ...prev, claude_bin: data.path || prev.claude_bin }));
            }
            void guiLog(`自动填入 ${engine} 路径: ${data.path}`);
          }
        }
        return data;
      } catch (e) {
        setResult({
          found: false,
          message: String(e),
        });
        return null;
      } finally {
        setBusy(false);
      }
    },
    [cliForm.cursor_bin, cliForm.claude_bin],
  );

  const openCliInstall = useCallback(async (engine: "cursor" | "claude") => {
    const setBusy = engine === "cursor" ? setInstallingCursor : setInstallingClaude;
    setBusy(true);
    try {
      await invoke("open_cli_install_terminal", { engine });
      void guiLog(`已打开 ${engine === "cursor" ? "Cursor CLI" : "Claude Code"} 安装终端`);
    } catch (e) {
      setError(String(e));
      void guiLog(`打开安装终端失败: ${e}`, "ERROR");
    } finally {
      setBusy(false);
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
      projects_root: pick("projects_root", "~"),
      cursor_model: String(defaults.cursor_model || ""),
      claude_model: String(defaults.claude_model || ""),
      openai_model: String(defaults.openai_model || ""),
      memory_enabled: Boolean(
        ((defaults.memory as Record<string, unknown> | undefined)?.enabled ?? false) === true,
      ),
      memory_gui_bind_enabled: Boolean(
        ((defaults.memory as Record<string, unknown> | undefined)?.gui_bind_enabled ?? false) ===
          true,
      ),
    });
  }, []);

  const refreshSkills = useCallback(async () => {
    try {
      const installedRaw = await api("GET", "/v1/skills");
      const installed = JSON.parse(installedRaw) as { skills?: SkillInfo[] };
      setInstalledSkills(installed.skills || []);
    } catch (e) {
      setSkillsPageError((prev) => ({
        text: String(e),
        seq: (prev?.seq ?? 0) + 1,
      }));
    }
  }, []);

  const openInstalledSkill = useCallback(async (sk: SkillInfo) => {
    try {
      let dir = (sk.dir || "").trim();
      if (!dir) {
        const raw = await api("GET", `/v1/skills/${encodeURIComponent(sk.id)}`);
        const detail = JSON.parse(raw) as { dir?: string };
        dir = (detail.dir || "").trim();
      }
      if (!dir) {
        throw new Error(`找不到 Skill 目录：${sk.id}`);
      }
      await openLocalPath(dir);
    } catch (e) {
      setSkillsPageError((prev) => ({
        text: String(e),
        seq: (prev?.seq ?? 0) + 1,
      }));
    }
  }, []);

  const installSkillFromPath = useCallback(
    async (rawPath: string) => {
      const p = rawPath.trim();
      if (!p) return;
      const source = detectSkillInstallSource(p);
      setSkillsBusy(true);
      try {
        await api("POST", "/v1/skills/install", { source, path: p });
        setSkillInstallPath("");
        await refreshSkills();
        void guiLog(`导入 Skill（${source}） ${p}`);
      } catch (e) {
        setSkillsPageError((prev) => ({
          text: String(e),
          seq: (prev?.seq ?? 0) + 1,
        }));
      } finally {
        setSkillsBusy(false);
      }
    },
    [refreshSkills],
  );

  useEffect(() => {
    if (page !== "skills") return;
    let disposed = false;
    let unlisten: (() => void) | undefined;
    void (async () => {
      try {
        const { getCurrentWebview } = await import("@tauri-apps/api/webview");
        if (disposed) return;
        unlisten = await getCurrentWebview().onDragDropEvent((event) => {
          const kind = event.payload.type;
          if (kind === "over") {
            setSkillDropActive(true);
            return;
          }
          if (kind === "leave") {
            setSkillDropActive(false);
            return;
          }
          if (kind === "drop") {
            setSkillDropActive(false);
            const first = event.payload.paths?.[0];
            if (first) setSkillInstallPath(first);
          }
        });
      } catch {
        /* browser / e2e preview：无 Tauri 拖拽路径 */
      }
    })();
    return () => {
      disposed = true;
      unlisten?.();
      setSkillDropActive(false);
    };
  }, [page]);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      await boot();
      if (cancelled) return;
      try {
        await Promise.all([refreshStatus(), refreshConfig(), refreshSkills()]);
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
    logNearBottomRef.current = true;
    setShowLogScrollBottom(false);
  }, [logBot]);

  const updateLogScrollBottomVisibility = useCallback(() => {
    const el = logboxRef.current;
    if (!el) return;
    const dist = el.scrollHeight - el.scrollTop - el.clientHeight;
    const near = dist <= 72;
    logNearBottomRef.current = near;
    setShowLogScrollBottom(!near && el.scrollHeight > el.clientHeight + 8);
  }, []);

  const jumpLogsToBottom = useCallback(() => {
    const el = logboxRef.current;
    if (!el) {
      logBottomRef.current?.scrollIntoView({ block: "end" });
      return;
    }
    el.scrollTop = el.scrollHeight;
  }, []);

  const scrollLogsToBottom = useCallback(
    (behavior: ScrollBehavior = "smooth") => {
      logNearBottomRef.current = true;
      setShowLogScrollBottom(false);
      if (behavior === "auto") {
        jumpLogsToBottom();
      } else {
        logBottomRef.current?.scrollIntoView({ block: "end", behavior });
      }
    },
    [jumpLogsToBottom],
  );

  useEffect(() => {
    if (page !== "logs") return;
    if (logNearBottomRef.current) {
      jumpLogsToBottom();
    } else {
      setShowLogScrollBottom(true);
    }
  }, [logs, page, jumpLogsToBottom]);

  useEffect(() => {
    if (page !== "logs") return;
    logNearBottomRef.current = true;
    setShowLogScrollBottom(false);
    const jump = () => {
      // 用户已上滑时不要再强制贴底，否则会吞掉「滚到底部」按钮（e2e / 快速操作会踩中 80ms 定时器）。
      if (!logNearBottomRef.current) {
        return;
      }
      jumpLogsToBottom();
    };
    jump();
    const id = window.requestAnimationFrame(jump);
    const t = window.setTimeout(jump, 80);
    return () => {
      window.cancelAnimationFrame(id);
      window.clearTimeout(t);
    };
  }, [page, jumpLogsToBottom]);

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

    if (!cliDiscoverTried.current) {
      cliDiscoverTried.current = true;
      void discoverCli("cursor");
      void discoverCli("claude");
    }

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
    discoverCli,
  ]);

  useEffect(() => {
    if (page !== "settings") {
      cliDiscoverTried.current = false;
    }
  }, [page]);

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

  function flashBotModalError(text: string) {
    setBotModalError((prev) => ({ text, seq: (prev?.seq ?? 0) + 1 }));
  }

  function flashChannelModalError(text: string) {
    setChannelModalError((prev) => ({ text, seq: (prev?.seq ?? 0) + 1 }));
  }

  function openCreateBot() {
    setBotForm(emptyBotForm());
    setBotWorkspaceTouched(false);
    setBotModalError(null);
    setBotFieldErrors({});
    setBotModal("create");
  }

  function openEditBot() {
    if (!selectedRoleConfig) return;
    const useDefaults =
      String(selectedRoleConfig.backend || "") === "openai"
        ? botUsesOpenaiDefaults(selectedRoleConfig)
        : true;
    const botId = String(selectedRoleConfig.id || "");
    setBotForm({
      id: botId,
      name: String(selectedRoleConfig.name || ""),
      backend: String(selectedRoleConfig.backend || "cursor_cli"),
      group: String(selectedRoleConfig.group || "default"),
      send_msg_url: String(selectedRoleConfig.send_msg_url || ""),
      system_prompt: String(selectedRoleConfig.system_prompt || ""),
      model: String(selectedRoleConfig.model || ""),
      openai_base_url: String(selectedRoleConfig.openai_base_url || ""),
      openai_api_key: String(selectedRoleConfig.openai_api_key || ""),
      openai_use_defaults: useDefaults,
      skills: Array.isArray(selectedRoleConfig.skills)
        ? (selectedRoleConfig.skills as unknown[]).map((x) => String(x))
        : [],
      inbound_mode: String(selectedRoleConfig.inbound_mode || "websocket"),
      session_mode: String(
        selectedRoleConfig.session_mode ||
          (rawConfig?.defaults as Record<string, unknown> | undefined)?.session_mode ||
          "shared",
      ),
      workspace: String(selectedRoleConfig.workspace || "").trim() || defaultBotWorkspace(botId),
    });
    setBotWorkspaceTouched(true);
    setBotModalError(null);
    setBotFieldErrors({});
    setBotModal("edit");
  }

  async function submitBot() {
    if (!rawConfig) return;
    const fieldErrs: Record<string, string> = {};
    if (!botForm.id.trim()) fieldErrs.id = "请填写机器人 ID";
    if (!botForm.name.trim()) fieldErrs.name = "请填写显示名称";
    const needChannelFields =
      botModal === "create" || !Array.isArray(selectedRoleConfig?.channels);
    if (needChannelFields && !botForm.send_msg_url.trim()) {
      fieldErrs.send_msg_url = "请填写云之家 send_msg_url（含 yzjtoken）";
    } else if (needChannelFields && botForm.send_msg_url.trim()) {
      const skip =
        botModal === "edit" && selectedRoleId
          ? { botId: selectedRoleId, root: true }
          : undefined;
      const conflict = findWebhookConflict(
        (rawConfig.bots as Record<string, unknown>[]) || [],
        botForm.send_msg_url,
        skip,
      );
      if (conflict) fieldErrs.send_msg_url = conflict;
    }
    if (botForm.backend === "openai") {
      if (botForm.openai_use_defaults) {
        if (
          !cliForm.openai_base_url.trim() ||
          !cliForm.openai_api_key.trim() ||
          !cliForm.openai_model.trim()
        ) {
          fieldErrs.openai =
            "AI 设置中尚未配齐 Base URL、API Key 与模型，请先到「AI 设置」填写，或关闭开关单独配置";
        }
      } else {
        if (!botForm.openai_base_url.trim()) fieldErrs.openai_base_url = "请填写 Base URL";
        if (!botForm.openai_api_key.trim()) fieldErrs.openai_api_key = "请填写 API Key";
        if (!botForm.model.trim()) fieldErrs.model = "请选择或填写模型";
      }
    }
    if (Object.keys(fieldErrs).length) {
      setBotFieldErrors(fieldErrs);
      flashBotModalError(
        fieldErrs.send_msg_url ||
          fieldErrs.openai ||
          fieldErrs.id ||
          fieldErrs.name ||
          "请完善表单后再保存",
      );
      return;
    }
    setBotFieldErrors({});
    setBotModalError(null);
    const bots = [...((rawConfig.bots as Record<string, unknown>[]) || [])];
    const payload: Record<string, unknown> = {
      id: botForm.id.trim(),
      name: botForm.name.trim(),
      backend: botForm.backend,
      inbound_mode: botForm.inbound_mode,
      session_mode: botForm.session_mode,
      system_prompt: botForm.system_prompt,
      workspace: botForm.workspace.trim() || defaultBotWorkspace(botForm.id),
      skills: botForm.skills,
    };
    if (botForm.backend === "openai") {
      if (botForm.openai_use_defaults) {
        // 不写覆盖字段，运行时回退到 defaults；编辑时需删掉旧覆盖。
      } else {
        payload.openai_base_url = botForm.openai_base_url.trim();
        payload.openai_api_key = botForm.openai_api_key.trim();
        payload.model = botForm.model.trim();
      }
    } else if (botForm.model.trim()) {
      payload.model = botForm.model.trim();
    }
    if (botModal === "create") {
      if (bots.some((b) => String(b.id) === payload.id)) {
        setBotFieldErrors({ id: "该 ID 已被占用" });
        flashBotModalError("机器人 id 已存在");
        return;
      }
      payload.group = botForm.group || "default";
      payload.send_msg_url = botForm.send_msg_url.trim();
      bots.push(payload);
    } else {
      const idx = bots.findIndex((b) => String(b.id) === selectedRoleId);
      if (idx < 0) return;
      const prev = { ...bots[idx] };
      Object.assign(prev, payload);
      if (botForm.backend === "openai" && botForm.openai_use_defaults) {
        delete prev.openai_base_url;
        delete prev.openai_api_key;
        delete prev.model;
      }
      if (botForm.backend !== "openai") {
        // 切离 OpenAI 后清掉角色/通道上的旧 model，避免 Cursor CLI 误用 deepseek 等网关模型名。
        delete prev.openai_base_url;
        delete prev.openai_api_key;
        if (!botForm.model.trim()) delete prev.model;
        if (Array.isArray(prev.channels)) {
          prev.channels = (prev.channels as Record<string, unknown>[]).map((ch) => {
            const next = { ...ch };
            delete next.model;
            return next;
          });
        }
      }
      if (!Array.isArray(prev.channels)) {
        prev.group = botForm.group || prev.group || "default";
        if (botForm.send_msg_url.trim()) prev.send_msg_url = botForm.send_msg_url.trim();
      }
      bots[idx] = prev;
    }
    try {
      await saveConfig({ ...rawConfig, bots });
    } catch (e) {
      flashBotModalError(String(e));
      return;
    }
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
    setChannelModalError(null);
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
    setChannelModalError(null);
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
      flashChannelModalError("通道 group / send_msg_url 不能为空");
      return;
    }
    const conflict = findWebhookConflict(
      (rawConfig.bots as Record<string, unknown>[]) || [],
      channelForm.send_msg_url,
      selectedRoleId && editingChannelIdx !== null
        ? { botId: selectedRoleId, channelIndex: editingChannelIdx }
        : undefined,
    );
    if (conflict) {
      flashChannelModalError(conflict);
      return;
    }
    setChannelModalError(null);
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
    // 通道模型覆盖仅对 OpenAI 有意义；非 OpenAI 保存时显式丢掉，避免残留网关模型名。
    if (String(bot.backend || "") === "openai" && channelForm.model.trim()) {
      entry.model = channelForm.model.trim();
    }
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
      delete defaults.workspace;
      delete defaults.cursor_workspace;
      delete defaults.claude_workspace;
      defaults.projects_root = cliForm.projects_root.trim() || "~";
      // 引擎模型分字段保存，不写共享 defaults.model，避免 Cursor/Claude/OpenAI 互相覆盖。
      defaults.cursor_model = cliForm.cursor_model.trim();
      defaults.claude_model = cliForm.claude_model.trim();
      defaults.openai_model = cliForm.openai_model.trim();
      const prevMem = {
        ...(((defaults.memory as Record<string, unknown> | undefined) || {}) as Record<
          string,
          unknown
        >),
      };
      prevMem.enabled = cliForm.memory_enabled;
      prevMem.gui_bind_enabled = cliForm.memory_gui_bind_enabled;
      defaults.memory = prevMem;
      await saveConfig({ ...rawConfig, defaults });
      await refreshConfig();
      showToast("设置已保存");
      void guiLog("保存 AI 设置成功");
    });
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
  const sessionModeOptions = [
    { id: "per_user", label: "per_user（按用户隔离）" },
    { id: "shared", label: "shared（通道共享）" },
    { id: "oneshot", label: "oneshot（单次，无上下文）" },
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
            {booting ? <span className="spinner dark lg" /> : null}
            <strong>
              {booting
                ? "正在启动桥接服务…"
                : error
                  ? "桥接服务启动失败"
                  : "等待桥就绪…"}
            </strong>
            <span>
              {booting
                ? "加载配置与控制通道，请稍候"
                : error
                  ? "请查看下方错误信息，或检查 ~/.yzj-bridge/config.yaml"
                  : "正在连接控制 API…"}
            </span>
            {error ? (
              <span className="err" data-testid="boot-error">
                {error}
              </span>
            ) : null}
          </div>
        </div>
      )}
      <aside className="nav">
        <div className="brand">
          <BrandMark />
          <span className="brand-title">YZJ Bridge</span>
          {import.meta.env.DEV ? (
            <span className="dev-badge" title="正在连接 Vite 开发服（可热更新）">
              DEV
            </span>
          ) : null}
        </div>
        {(
          [
            ["chat", "聊天"],
            ["bots", "机器人"],
            ["settings", "AI 设置"],
            ["skills", "Skills"],
            ["memory", "记忆"],
            ["logs", "运行日志"],
            ["help", "帮助"],
            ["system", "系统设置"],
          ] as const
        ).map(([id, label]) => (
          <button
            key={id}
            data-testid={`nav-${id}`}
            className={page === id ? "nav-btn active" : "nav-btn"}
            onClick={() => {
              setPage(id);
              if (id === "skills") void refreshSkills();
            }}
          >
            <NavIcon id={id} />
            <span>{label}</span>
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
        {ready ? (
          <div
            className="chat-page-host"
            hidden={page !== "chat"}
            aria-hidden={page !== "chat"}
          >
            <ChatPage
              api={api}
              ready={ready}
              active={page === "chat"}
              bots={status.map((b) => ({ id: b.id, name: b.name, backend: b.backend }))}
              guiBindEnabled={cliForm.memory_gui_bind_enabled}
            />
          </div>
        ) : null}
        {page === "memory" ? (
          <MemoryPage api={api} ready={ready} />
        ) : null}
        {page === "system" && (
          <section className="page" key="system" data-testid="page-system">
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
                  </div>
                  <Switch checked={autostart} loading={loadingAuto} onChange={toggleAutostart} />
                </div>
                <div className="row">
                  <div className="row-text">
                    <strong>热重载配置</strong>
                    <span>从磁盘重新加载 config.yaml</span>
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
                <div className="row">
                  <div className="row-text">
                    <strong>应用版本</strong>
                    <span data-testid="app-version">
                      {appVersion ? `v${appVersion}` : "—"}
                    </span>
                  </div>
                  <button
                    type="button"
                    data-testid="check-update-btn"
                    className={`action-chip${checkingUpdate ? " loading" : ""}`}
                    disabled={checkingUpdate || updatingApp}
                    onClick={() => void checkUpdateManual()}
                  >
                    {checkingUpdate ? <span className="spinner dark" /> : null}
                    <span>{checkingUpdate ? "检查中" : "检查更新"}</span>
                  </button>
                </div>
              </div>
            </div>
          </section>
        )}

        {page === "settings" && (
          <section className="page" key="settings" data-testid="page-settings">
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
                    <div className="inline-field cli-bin-row">
                      <input
                        data-testid="cursor-bin"
                        value={cliForm.cursor_bin}
                        onChange={(e) => setCliForm({ ...cliForm, cursor_bin: e.target.value })}
                        placeholder="agent 或绝对路径"
                      />
                      <button
                        type="button"
                        className={`action-chip side${discoveringCursor ? " loading" : ""}`}
                        data-testid="discover-cursor"
                        disabled={discoveringCursor}
                        onClick={() => void discoverCli("cursor", { autofill: true })}
                      >
                        {discoveringCursor ? <span className="spinner dark" /> : null}
                        <span>{discoveringCursor ? "扫描中" : "重新扫描"}</span>
                      </button>
                      {cursorDiscover && !cursorDiscover.found ? (
                        <button
                          type="button"
                          className={`action-chip side${installingCursor ? " loading" : ""}`}
                          data-testid="install-cursor"
                          disabled={installingCursor}
                          onClick={() => void openCliInstall("cursor")}
                        >
                          {installingCursor ? <span className="spinner dark" /> : null}
                          <span>{installingCursor ? "打开中" : "一键安装"}</span>
                        </button>
                      ) : null}
                    </div>
                    {cursorDiscover ? (
                      <span
                        className={`field-hint${cursorDiscover.found ? " ok" : " error"}`}
                        data-testid="cursor-discover-hint"
                      >
                        {cursorDiscover.found
                          ? `已找到${cursorDiscover.version ? `（${cursorDiscover.version}）` : ""}：${cursorDiscover.path || ""}`
                          : cursorDiscover.message ||
                            cursorDiscover.install?.hint ||
                            "未找到 Cursor CLI，可一键打开终端安装"}
                      </span>
                    ) : (
                      <span className="field-hint spacer" aria-hidden="true">
                        &nbsp;
                      </span>
                    )}
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
                </div>
              </div>

              <div className="card soft pad settings-group" data-testid="group-claude">
                <h3 className="section-inline">Claude Code</h3>
                <p className="group-desc">claude 可执行文件、Anthropic Key 与默认模型</p>
                <div className="form-grid">
                  <label className="full">
                    可执行路径（claude_bin）
                    <div className="inline-field cli-bin-row">
                      <input
                        data-testid="claude-bin"
                        value={cliForm.claude_bin}
                        onChange={(e) => setCliForm({ ...cliForm, claude_bin: e.target.value })}
                        placeholder="claude 或绝对路径"
                      />
                      <button
                        type="button"
                        className={`action-chip side${discoveringClaude ? " loading" : ""}`}
                        data-testid="discover-claude"
                        disabled={discoveringClaude}
                        onClick={() => void discoverCli("claude", { autofill: true })}
                      >
                        {discoveringClaude ? <span className="spinner dark" /> : null}
                        <span>{discoveringClaude ? "扫描中" : "重新扫描"}</span>
                      </button>
                      {claudeDiscover && !claudeDiscover.found ? (
                        <button
                          type="button"
                          className={`action-chip side${installingClaude ? " loading" : ""}`}
                          data-testid="install-claude"
                          disabled={installingClaude}
                          onClick={() => void openCliInstall("claude")}
                        >
                          {installingClaude ? <span className="spinner dark" /> : null}
                          <span>{installingClaude ? "打开中" : "一键安装"}</span>
                        </button>
                      ) : null}
                    </div>
                    {claudeDiscover ? (
                      <span
                        className={`field-hint${claudeDiscover.found ? " ok" : " error"}`}
                        data-testid="claude-discover-hint"
                      >
                        {claudeDiscover.found
                          ? `已找到${claudeDiscover.version ? `（${claudeDiscover.version}）` : ""}：${claudeDiscover.path || ""}`
                          : claudeDiscover.message ||
                            claudeDiscover.install?.hint ||
                            "未找到 Claude Code，可一键打开终端安装"}
                      </span>
                    ) : (
                      <span className="field-hint spacer" aria-hidden="true">
                        &nbsp;
                      </span>
                    )}
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
                </div>
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

              <div className="card soft pad settings-group" data-testid="group-memory">
                <h3 className="section-inline">用户记忆</h3>
                <p className="group-desc">
                  按云之家 openID 维护画像并在回复时注入附录；默认关闭。开启前会探测 OpenAI 或 Claude。
                </p>
                <div className="row">
                  <div className="row-text">
                    <strong>启用用户记忆</strong>
                    <span>defaults.memory.enabled（默认关）</span>
                  </div>
                  <Switch
                    testId="memory-enabled-switch"
                    checked={cliForm.memory_enabled}
                    loading={memoryEnableBusy}
                    onChange={async (v) => {
                      if (!v) {
                        setCliForm({ ...cliForm, memory_enabled: false });
                        setMemoryEnableHint("");
                        return;
                      }
                      setMemoryEnableBusy(true);
                      setMemoryEnableHint("");
                      try {
                        const raw = await api("POST", "/v1/memory/enable-check");
                        const res = JSON.parse(raw) as { ok?: boolean; reason?: string };
                        if (!res.ok) {
                          setMemoryEnableHint(res.reason || "探测失败，无法开启");
                          return;
                        }
                        setCliForm({ ...cliForm, memory_enabled: true });
                        setMemoryEnableHint("探测通过，保存设置后生效");
                      } catch (e) {
                        setMemoryEnableHint(String(e));
                      } finally {
                        setMemoryEnableBusy(false);
                      }
                    }}
                  />
                </div>
                {memoryEnableHint ? (
                  <span className="field-hint" data-testid="memory-enable-hint">
                    {memoryEnableHint}
                  </span>
                ) : null}
                <div className="row">
                  <div className="row-text">
                    <strong>GUI 聊天绑定 openID</strong>
                    <span>仅调试；默认关。开启后聊天页可绑定真人 openID 走记忆</span>
                  </div>
                  <Switch
                    testId="memory-gui-bind-switch"
                    checked={cliForm.memory_gui_bind_enabled}
                    onChange={(v) => setCliForm({ ...cliForm, memory_gui_bind_enabled: v })}
                  />
                </div>
              </div>

              <div className="card soft pad settings-group" data-testid="group-dirs">
                <h3 className="section-inline">项目目录</h3>
                <p className="group-desc">
                  projects_root 用于解析聊天里的项目名（如 --project api-sms）到本机代码目录。
                  机器人工作目录请在各机器人设置中的 workspace 配置；留空则自动补全为
                  ~/.yzj-bridge/workspace/&#123;机器人 id&#125;。
                </p>
                <div className="form-grid">
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

        {page === "skills" && (
          <section className="page" key="skills" data-testid="page-skills">
            <header className="page-head">
              <div>
                <h1>Skills</h1>
                <p className="subtitle">统一 Skill 包：本地导入后供各机器人勾选启用</p>
              </div>
              <div className="head-actions">
                <button
                  type="button"
                  className={`action-chip${skillsBusy ? " loading" : ""}`}
                  disabled={skillsBusy}
                  data-testid="skills-refresh"
                  onClick={() => void refreshSkills()}
                >
                  {skillsBusy ? <span className="spinner dark" /> : null}
                  <span>{skillsBusy ? "刷新中" : "刷新"}</span>
                </button>
              </div>
            </header>
            {skillsPageError ? (
              <ModalFloatMessage
                key={skillsPageError.seq}
                message={skillsPageError.text}
                testId="skills-page-error"
                onDismissed={() => setSkillsPageError(null)}
              />
            ) : null}
            <div className="stack page-body skills-page">
              <div className="card soft pad skills-panel" data-testid="skills-installed">
                <div className="skills-panel-head">
                  <div>
                    <h3 className="section-inline">已安装</h3>
                    <p className="group-desc">点击条目用系统默认方式打开；可在机器人编辑页勾选启用</p>
                  </div>
                  <span className="skills-count">{installedSkills.length}</span>
                </div>
                {installedSkills.length === 0 ? (
                  <div className="skills-empty">
                    <strong>尚未安装 Skill</strong>
                    <span>将含 SKILL.md 的目录、.zip / .tar.gz，或 .md 拖入下方导入区</span>
                  </div>
                ) : (
                  <div className="skill-list">
                    {installedSkills.map((sk) => (
                      <div key={sk.id} className="skill-row">
                        <div className="skill-row-main">
                          <button
                            type="button"
                            className="skill-row-open"
                            title="用系统默认方式打开"
                            data-testid={`skill-open-${sk.id}`}
                            onClick={() => void openInstalledSkill(sk)}
                          >
                            <div className="skill-row-title">
                              <strong>{sk.name || sk.id}</strong>
                              <span className="skill-id-chip">{sk.id}</span>
                            </div>
                            {sk.description ? <p className="skill-desc">{sk.description}</p> : null}
                          </button>
                        </div>
                        <button
                          type="button"
                          className="btn ghost tiny"
                          disabled={skillsBusy}
                          onClick={() =>
                            void (async () => {
                              setSkillsBusy(true);
                              try {
                                await api("DELETE", `/v1/skills/${encodeURIComponent(sk.id)}`);
                                await refreshSkills();
                                void guiLog(`卸载 Skill ${sk.id}`, "WARN");
                              } catch (e) {
                                setSkillsPageError((prev) => ({
                                  text: String(e),
                                  seq: (prev?.seq ?? 0) + 1,
                                }));
                              } finally {
                                setSkillsBusy(false);
                              }
                            })()
                          }
                        >
                          卸载
                        </button>
                      </div>
                    ))}
                  </div>
                )}
              </div>

              <div
                className={`card soft pad skills-panel${skillDropActive ? " drop-active" : ""}`}
                data-testid="skills-import"
                onDragOver={(e) => {
                  e.preventDefault();
                  setSkillDropActive(true);
                }}
                onDragLeave={() => setSkillDropActive(false)}
                onDrop={(e) => {
                  e.preventDefault();
                  setSkillDropActive(false);
                  const file = e.dataTransfer.files?.[0] as (File & { path?: string }) | undefined;
                  if (file?.path) setSkillInstallPath(file.path);
                }}
              >
                <div className="skills-panel-head">
                  <div>
                    <h3 className="section-inline">从本地导入</h3>
                    <p className="group-desc">
                      文件夹图标选目录；.zip / .tar.gz / .md 可拖拽到此处或手输路径（目录需含 SKILL.md）
                    </p>
                  </div>
                </div>
                <div className="skills-import-row">
                  <div className="skills-path-field">
                    <input
                      className="skills-import-input"
                      value={skillInstallPath}
                      onChange={(e) => setSkillInstallPath(e.target.value)}
                      placeholder="拖拽到此处，或手输路径；点右侧图标选择文件夹"
                      data-testid="skill-install-path"
                    />
                    <button
                      type="button"
                      className="skills-browse-btn"
                      title="选择 Skill 文件夹"
                      aria-label="选择 Skill 文件夹"
                      data-testid="skill-browse-dir"
                      disabled={skillsBusy}
                      onClick={() =>
                        void (async () => {
                          try {
                            const selected = await pickSkillDirectory();
                            if (selected) setSkillInstallPath(selected);
                          } catch (e) {
                            setSkillsPageError((prev) => ({
                              text: String(e),
                              seq: (prev?.seq ?? 0) + 1,
                            }));
                          }
                        })()
                      }
                    >
                      <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
                        <path
                          fill="currentColor"
                          d="M10 4H4c-1.1 0-2 .9-2 2v12c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V8c0-1.1-.9-2-2-2h-8l-2-2z"
                        />
                      </svg>
                    </button>
                  </div>
                  <button
                    type="button"
                    className="btn"
                    data-testid="skill-import-btn"
                    disabled={skillsBusy || !skillInstallPath.trim()}
                    onClick={() => void installSkillFromPath(skillInstallPath)}
                  >
                    导入
                  </button>
                </div>
                <span className="field-hint">
                  以 SKILL.md 的 name 安装到本机；压缩包根目录直接放 SKILL.md 也可。单独 .md 按文件名生成包
                </span>
              </div>
            </div>
          </section>
        )}

        {page === "bots" && (
          <section className="page bots" key="bots" data-testid="page-bots">
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
                        <div className="kv-full">
                          <span>Skills</span>
                          <strong data-testid="bot-skills-summary">
                            {formatBotSkillsSummary(selectedRoleConfig.skills, installedSkills)}
                          </strong>
                        </div>
                        <div className="kv-full">
                          <span>系统提示词</span>
                          <pre
                            className="prompt-preview"
                            data-testid="bot-system-prompt-summary"
                          >
                            {String(selectedRoleConfig.system_prompt || "（未设置系统提示词）")}
                          </pre>
                        </div>
                      </div>
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
          <section className="page logs-page" key="logs" data-testid="page-logs">
            <header className="page-head">
              <div>
                <h1>运行日志</h1>
                <p className="subtitle">桥与面板事件 · 可按来源过滤 · 同时写入 ~/.yzj-bridge/logs/runtime-日期.jsonl</p>
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
                    logNearBottomRef.current = true;
                    setShowLogScrollBottom(false);
                  }}
                >
                  清空视图
                </button>
              </div>
            </header>
            <div className="logbox-wrap page-body">
              <pre
                className="logbox"
                data-testid="logbox"
                ref={logboxRef}
                onScroll={updateLogScrollBottomVisibility}
              >
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
                <div ref={logBottomRef} />
              </pre>
              <LogMinimap logs={logs} logboxRef={logboxRef} theme={theme} />
              {showLogScrollBottom ? (
                <button
                  type="button"
                  className="scroll-to-bottom"
                  data-testid="log-scroll-bottom"
                  title="滚动到底部"
                  aria-label="滚动到底部"
                  onClick={() => scrollLogsToBottom("smooth")}
                >
                  <svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden>
                    <path
                      d="M6 10l6 6 6-6"
                      stroke="currentColor"
                      strokeWidth="1.9"
                      strokeLinecap="round"
                      strokeLinejoin="round"
                    />
                  </svg>
                </button>
              ) : null}
            </div>
          </section>
        )}

        {page === "help" && (
          <section className="page help-page" key="help" data-testid="page-help">
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
                <h2>使用流程</h2>
                <ol className="help-flow" data-testid="help-flow">
                  <li>
                    <span className="help-flow-idx">01</span>
                    <div>
                      <strong>配置后端引擎</strong>
                      <em>打开「AI 设置」，至少配置好一个可用引擎（Cursor CLI / Claude Code / OpenAI 等）。</em>
                    </div>
                  </li>
                  <li>
                    <span className="help-flow-idx">02</span>
                    <div>
                      <strong>创建云之家机器人</strong>
                      <em>
                        在云之家群组中新建通知型机器人，或前往{" "}
                        <a
                          href="https://yunzhijia.com/im/personalRobotCreate"
                          onClick={(e) => {
                            e.preventDefault();
                            void openExternal("https://yunzhijia.com/im/personalRobotCreate");
                          }}
                        >
                          个人机器人创建页
                        </a>{" "}
                        创建，并复制其 Webhook 发送地址。
                      </em>
                    </div>
                  </li>
                  <li>
                    <span className="help-flow-idx">03</span>
                    <div>
                      <strong>在本应用接入</strong>
                      <em>
                        打开「机器人」页新建机器人，将第 2 步的 Webhook 链接粘贴到发送地址（send_msg_url），保存后即可收发消息。
                      </em>
                    </div>
                  </li>
                  <li>
                    <span className="help-flow-idx">04</span>
                    <div>
                      <strong>本地聊天测试</strong>
                      <em>
                        打开「聊天」页，点右上角 + 新建会话，用下拉或 @ 指定机器人，即可在 GUI 内测对话（不推送云之家）。
                      </em>
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
                      源码、Issue 与发布说明开源在 GitHub，欢迎 Star 与贡献。
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
        <div className="modal-backdrop" data-testid="bot-modal">
          <div className="modal" role="dialog" aria-modal="true">
            <div className="modal-head">
              <h3>{botModal === "create" ? "新建机器人" : "编辑机器人"}</h3>
              <button
                type="button"
                className="modal-close"
                data-testid="bot-modal-close"
                aria-label="关闭"
                onClick={() => setBotModal(null)}
              >
                ×
              </button>
            </div>
            {botModalError ? (
              <ModalFloatMessage
                key={botModalError.seq}
                message={botModalError.text}
                testId="bot-modal-error"
                onDismissed={() => setBotModalError(null)}
              />
            ) : null}
            <div className="modal-scroll">
            <div className="form-grid">
              <div className="field">
                <FieldLabel tip="唯一标识，保存后不可修改；也用于默认工作目录名">ID</FieldLabel>
                <input
                  data-testid="bot-id"
                  disabled={botModal === "edit"}
                  className={botFieldErrors.id ? "invalid" : undefined}
                  value={botForm.id}
                  onChange={(e) => {
                    const id = e.target.value;
                    setBotForm((prev) => ({
                      ...prev,
                      id,
                      workspace:
                        botModal === "create" && !botWorkspaceTouched
                          ? defaultBotWorkspace(id)
                          : prev.workspace,
                    }));
                    if (botFieldErrors.id) setBotFieldErrors({ ...botFieldErrors, id: "" });
                  }}
                  placeholder="唯一标识，如 fairy"
                />
                {botFieldErrors.id ? (
                  <span className="field-hint error">{botFieldErrors.id}</span>
                ) : null}
              </div>
              <div className="field">
                <FieldLabel tip="界面与日志中显示的名称">名称</FieldLabel>
                <input
                  data-testid="bot-name"
                  className={botFieldErrors.name ? "invalid" : undefined}
                  value={botForm.name}
                  onChange={(e) => {
                    setBotForm({ ...botForm, name: e.target.value });
                    if (botFieldErrors.name) setBotFieldErrors({ ...botFieldErrors, name: "" });
                  }}
                  placeholder="显示名称，如 Fairy"
                />
                {botFieldErrors.name ? (
                  <span className="field-hint error">{botFieldErrors.name}</span>
                ) : null}
              </div>
              <div className="field">
                <FieldLabel tip="选择本机执行引擎：Cursor CLI / Claude Code / OpenAI 兼容接口等">
                  后端引擎
                </FieldLabel>
                <FancySelect
                  testId="bot-backend"
                  value={botForm.backend}
                  options={backendOptions}
                  onChange={(v) => {
                    setBotForm({
                      ...botForm,
                      backend: v,
                      // 离开 OpenAI 时清空角色模型，改用对应引擎的全局默认（如 cursor_model）。
                      model: v === "openai" ? botForm.model : "",
                      openai_use_defaults: v === "openai" ? botForm.openai_use_defaults : true,
                    });
                    if (v === "openai" && !openaiModels.length) {
                      const useDef = botForm.openai_use_defaults;
                      void probeOpenai(
                        useDef ? cliForm.openai_base_url : botForm.openai_base_url || cliForm.openai_base_url,
                        useDef ? cliForm.openai_api_key : botForm.openai_api_key || cliForm.openai_api_key,
                      );
                    }
                  }}
                />
              </div>
              <div className="field">
                <FieldLabel tip="消息如何进入桥接：WebSocket、Webhook 或两者">入站模式</FieldLabel>
                <FancySelect
                  value={botForm.inbound_mode}
                  options={inboundOptions}
                  onChange={(v) => setBotForm({ ...botForm, inbound_mode: v })}
                />
              </div>
              <div className="field">
                <FieldLabel tip="多轮上下文隔离方式：per_user 按用户；shared 同通道共享（默认通道内排队）；oneshot 每次新会话">
                  会话模式 session_mode
                </FieldLabel>
                <FancySelect
                  testId="bot-session-mode"
                  value={botForm.session_mode}
                  options={sessionModeOptions}
                  onChange={(v) => setBotForm({ ...botForm, session_mode: v })}
                />
              </div>
              {botModal === "create" || !Array.isArray(selectedRoleConfig?.channels) ? (
                <>
                  <div className="field">
                    <FieldLabel tip="云之家通道分组标识，多通道时用于区分会话来源">
                      分组 Group
                    </FieldLabel>
                    <input
                      value={botForm.group}
                      onChange={(e) => setBotForm({ ...botForm, group: e.target.value })}
                      placeholder="如 workAssistant"
                    />
                  </div>
                  <div className="field full">
                    <FieldLabel tip="云之家机器人 Webhook 发送 URL（含 yzjtoken）">
                      发送地址 send_msg_url
                    </FieldLabel>
                    <input
                      data-testid="bot-send-url"
                      className={botFieldErrors.send_msg_url ? "invalid" : undefined}
                      value={botForm.send_msg_url}
                      onChange={(e) => {
                        setBotForm({ ...botForm, send_msg_url: e.target.value });
                        if (botFieldErrors.send_msg_url) {
                          setBotFieldErrors({ ...botFieldErrors, send_msg_url: "" });
                        }
                      }}
                      placeholder="https://www.yunzhijia.com/gateway/robot/webhook/send?..."
                    />
                    {botFieldErrors.send_msg_url ? (
                      <span className="field-hint error">{botFieldErrors.send_msg_url}</span>
                    ) : null}
                  </div>
                </>
              ) : null}
              <div className="field full">
                <FieldLabel tip="机器人跑任务时的工作目录（cwd）；新建时默认 ~/.yzj-bridge/workspace/{ID}">
                  启动工作目录 workspace
                </FieldLabel>
                <input
                  value={botForm.workspace}
                  onChange={(e) => {
                    setBotWorkspaceTouched(true);
                    setBotForm({ ...botForm, workspace: e.target.value });
                  }}
                  placeholder={defaultBotWorkspace(botForm.id || "your-id")}
                />
              </div>
              {botForm.backend === "openai" ? (
                <>
                  <div className="field full field-switch">
                    <div className="field-switch-text">
                      <FieldLabel tip="开启后 Base URL、API Key、模型取自「AI 设置」；关闭后可为本机器人单独配置">
                        使用 AI 设置默认配置
                      </FieldLabel>
                    </div>
                    <Switch
                      testId="openai-use-defaults"
                      checked={botForm.openai_use_defaults}
                      onChange={(v) => {
                        setBotForm((prev) => {
                          const next = { ...prev, openai_use_defaults: v };
                          if (!v) {
                            if (!prev.openai_base_url.trim()) {
                              next.openai_base_url = cliForm.openai_base_url;
                            }
                            if (!prev.openai_api_key.trim()) {
                              next.openai_api_key = cliForm.openai_api_key;
                            }
                            if (!prev.model.trim()) {
                              next.model = cliForm.openai_model;
                            }
                          }
                          return next;
                        });
                        setBotModalError(null);
                        setBotFieldErrors((errs) => {
                          const next = { ...errs };
                          delete next.openai;
                          delete next.openai_base_url;
                          delete next.openai_api_key;
                          delete next.model;
                          return next;
                        });
                        if (!v && !openaiModels.length) {
                          void probeOpenai(
                            botForm.openai_base_url || cliForm.openai_base_url,
                            botForm.openai_api_key || cliForm.openai_api_key,
                          );
                        }
                      }}
                    />
                  </div>
                  {botForm.openai_use_defaults ? (
                    <div className="field full">
                      <span className="field-hint">
                        将使用：{cliForm.openai_base_url.trim() || "（未配置 Base URL）"}
                        {" · "}
                        模型 {cliForm.openai_model.trim() || "（未配置）"}
                      </span>
                      {botFieldErrors.openai ? (
                        <span className="field-hint error" data-testid="openai-defaults-error">
                          {botFieldErrors.openai}
                        </span>
                      ) : null}
                    </div>
                  ) : (
                    <>
                      <div className="field full">
                        <FieldLabel tip="OpenAI 兼容接口地址，通常以 /v1 结尾">Base URL</FieldLabel>
                        <input
                          data-testid="openai-base-url"
                          className={botFieldErrors.openai_base_url ? "invalid" : undefined}
                          value={botForm.openai_base_url}
                          onChange={(e) => {
                            setBotForm({ ...botForm, openai_base_url: e.target.value });
                            if (botFieldErrors.openai_base_url) {
                              setBotFieldErrors({ ...botFieldErrors, openai_base_url: "" });
                            }
                          }}
                          placeholder="https://api.openai.com/v1"
                        />
                        {botFieldErrors.openai_base_url ? (
                          <span className="field-hint error">{botFieldErrors.openai_base_url}</span>
                        ) : null}
                      </div>
                      <div className="field full">
                        <FieldLabel tip="调用该接口所需的 API Key">API Key</FieldLabel>
                        <SecretInput
                          testId="openai-api-key"
                          value={botForm.openai_api_key}
                          onChange={(v) => {
                            setBotForm({ ...botForm, openai_api_key: v });
                            if (botFieldErrors.openai_api_key) {
                              setBotFieldErrors({ ...botFieldErrors, openai_api_key: "" });
                            }
                          }}
                        />
                        {botFieldErrors.openai_api_key ? (
                          <span className="field-hint error">{botFieldErrors.openai_api_key}</span>
                        ) : null}
                      </div>
                      <div className="field full">
                        <FieldLabel tip="该机器人实际调用的大模型名称（如 gpt-4o-mini）">
                          模型 Model
                        </FieldLabel>
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
                            onChange={(v) => {
                              setBotForm({ ...botForm, model: v });
                              if (botFieldErrors.model) {
                                setBotFieldErrors({ ...botFieldErrors, model: "" });
                              }
                            }}
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
                        {botFieldErrors.model ? (
                          <span className="field-hint error">{botFieldErrors.model}</span>
                        ) : null}
                      </div>
                    </>
                  )}
                </>
              ) : (
                <div className="field">
                  <FieldLabel tip="指定本机器人使用的模型 ID；留空则回退到 AI 设置里的默认模型">
                    模型 Model
                  </FieldLabel>
                  <input
                    value={botForm.model}
                    onChange={(e) => setBotForm({ ...botForm, model: e.target.value })}
                    placeholder="可留空，使用 AI 设置中的默认模型"
                  />
                </div>
              )}
              <div className="field full bot-skills-field" data-testid="bot-skills">
                <div className="bot-skills-head">
                  <div>
                    <FieldLabel tip="按机器人白名单启用；OpenAI 加载说明，Cursor/Claude 物化到工作区">
                      启用 Skills
                    </FieldLabel>
                  </div>
                  {installedSkills.length > 0 ? (
                    <span className="bot-skills-count">
                      {botForm.skills.filter((id) => installedSkills.some((s) => s.id === id)).length}/
                      {installedSkills.length}
                    </span>
                  ) : null}
                </div>
                {installedSkills.length === 0 ? (
                  <div className="bot-skills-empty">
                    <strong>暂无已安装 Skill</strong>
                    <span>请先到「Skills」页导入后再勾选</span>
                  </div>
                ) : (
                  <div className="bot-skills-list">
                    {installedSkills.map((sk) => {
                      const on = botForm.skills.includes(sk.id);
                      const title = sk.name || sk.id;
                      const showId = sk.id && sk.id !== title;
                      return (
                        <div
                          key={sk.id}
                          className={`bot-skill-row${on ? " on" : ""}`}
                        >
                          <div className="bot-skill-row-text">
                            <strong>{title}</strong>
                            {sk.description ? (
                              <span>{sk.description}</span>
                            ) : showId ? (
                              <span className="bot-skill-id">{sk.id}</span>
                            ) : (
                              <span>启用后对本机器人生效</span>
                            )}
                          </div>
                          <Switch
                            testId={`bot-skill-${sk.id}`}
                            checked={on}
                            onChange={(v) => {
                              setBotForm((prev) => ({
                                ...prev,
                                skills: v
                                  ? prev.skills.includes(sk.id)
                                    ? prev.skills
                                    : [...prev.skills, sk.id]
                                  : prev.skills.filter((x) => x !== sk.id),
                              }));
                            }}
                          />
                        </div>
                      );
                    })}
                  </div>
                )}
              </div>
              <div className="field full">
                <FieldLabel tip="每次对话前注入给模型的角色说明与行为约束">
                  系统提示词 System Prompt
                </FieldLabel>
                <textarea
                  rows={5}
                  value={botForm.system_prompt}
                  onChange={(e) => setBotForm({ ...botForm, system_prompt: e.target.value })}
                  placeholder="定义机器人人设、回答风格与约束"
                />
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
        </div>
      )}

      {channelModal && (
        <div className="modal-backdrop" data-testid="channel-modal">
          <div className="modal" role="dialog" aria-modal="true">
            <div className="modal-head">
              <h3>{editingChannelIdx === null ? "新增通道" : "编辑通道"}</h3>
              <button
                type="button"
                className="modal-close"
                data-testid="channel-modal-close"
                aria-label="关闭"
                onClick={() => setChannelModal(false)}
              >
                ×
              </button>
            </div>
            {channelModalError ? (
              <ModalFloatMessage
                key={channelModalError.seq}
                message={channelModalError.text}
                testId="channel-modal-error"
                onDismissed={() => setChannelModalError(null)}
              />
            ) : null}
            <div className="modal-scroll">
            <div className="form-grid">
              <div className="field">
                <FieldLabel tip="可选；不填则按默认规则生成运行时通道 ID">
                  自定义通道 ID（可选）
                </FieldLabel>
                <input
                  value={channelForm.id}
                  onChange={(e) => setChannelForm({ ...channelForm, id: e.target.value })}
                />
              </div>
              <div className="field">
                <FieldLabel tip="通道分组标识，用于区分同一机器人下的不同会话入口">
                  分组 Group
                </FieldLabel>
                <input
                  value={channelForm.group}
                  onChange={(e) => {
                    setChannelForm({ ...channelForm, group: e.target.value });
                    if (channelModalError) setChannelModalError(null);
                  }}
                  placeholder="如 workAssistant"
                />
              </div>
              <div className="field full">
                <FieldLabel tip="该通道对应的云之家机器人 Webhook 发送 URL">
                  发送地址 send_msg_url
                </FieldLabel>
                <input
                  value={channelForm.send_msg_url}
                  onChange={(e) => {
                    setChannelForm({ ...channelForm, send_msg_url: e.target.value });
                    if (channelModalError) setChannelModalError(null);
                  }}
                />
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
        </div>
      )}

      {updateModal && updateInfo ? (
        <div className="modal-backdrop" data-testid="update-modal">
          <div className="modal update-modal" role="dialog" aria-modal="true" aria-labelledby="update-modal-title">
            <div className="modal-head">
              <div>
                <h3 id="update-modal-title">发现新版本 v{updateInfo.latestVersion}</h3>
                <p className="subtitle update-modal-sub">
                  当前版本 v{updateInfo.currentVersion || appVersion || "—"}
                  {updateInfo.publishedAt
                    ? ` · 发布于 ${updateInfo.publishedAt.slice(0, 10)}`
                    : ""}
                </p>
              </div>
              <button
                type="button"
                className="modal-close"
                data-testid="update-modal-close"
                aria-label="关闭"
                disabled={updatingApp}
                onClick={() => setUpdateModal(false)}
              >
                ×
              </button>
            </div>
            <div className="modal-scroll update-notes" data-testid="update-notes">
              {updateInfo.notes.trim() ? (
                <Markdown remarkPlugins={[remarkGfm]}>{updateInfo.notes}</Markdown>
              ) : (
                <p className="field-hint">暂无更新日志</p>
              )}
            </div>
            <p className="field-hint update-install-hint">
              确认后将下载安装包并启动安装向导；安装前会退出本应用以便覆盖文件。
            </p>
            <div className="modal-actions">
              <button
                type="button"
                className="btn ghost"
                data-testid="update-later"
                disabled={updatingApp}
                onClick={() => setUpdateModal(false)}
              >
                稍后提醒
              </button>
              <button
                type="button"
                className="btn ghost"
                data-testid="update-skip"
                disabled={updatingApp}
                onClick={() => void skipUpdateVersion()}
              >
                跳过此版本
              </button>
              <button
                type="button"
                className="btn"
                data-testid="update-confirm"
                disabled={updatingApp || !updateInfo.downloadUrl}
                onClick={() => void confirmUpdate()}
              >
                {updatingApp ? (
                  <>
                    <span className="spinner dark" />
                    <span>下载中…</span>
                  </>
                ) : (
                  "立即更新"
                )}
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </div>
  );
}

export default App;
