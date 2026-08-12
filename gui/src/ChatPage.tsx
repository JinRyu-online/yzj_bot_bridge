import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { KeyboardEvent as ReactKeyboardEvent } from "react";
import { Channel, invoke } from "@tauri-apps/api/core";
import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";

type ChatBotOption = {
  id: string;
  name: string;
  backend?: string;
};

type ChatMessage = {
  role: string;
  content: string;
  reasoning?: string;
  bot_id?: string;
  ts: string;
};

type ChatSession = {
  id: string;
  title: string;
  bot_id: string;
  updated_at: string;
  messages: ChatMessage[];
};

type ChatSummary = {
  id: string;
  title: string;
  bot_id: string;
  updated_at: string;
  message_count?: number;
};

type ApiFn = (method: string, path: string, body?: unknown) => Promise<string>;

type Props = {
  api: ApiFn;
  bots: ChatBotOption[];
  ready: boolean;
  /** True while the chat page is the visible main view. */
  active?: boolean;
};

const ACTIVE_SESSION_KEY = "yzj-chat-active-session";
const mdPlugins = [remarkGfm];

/** Ensure GFM tables are preceded by a blank line (models often omit it). */
function normalizeMarkdown(src: string): string {
  return src
    .replace(/\r\n/g, "\n")
    .replace(/([^\n])\n(\|[^\n]+\|\n\s*\|[-:| \t]+\|)/g, "$1\n\n$2");
}

function ChatMarkdown({ children }: { children: string }) {
  const text = normalizeMarkdown(children || "");
  return (
    <div className="chat-md">
      <Markdown
        remarkPlugins={mdPlugins}
        components={{
          table: ({ children }) => (
            <div className="chat-md-table-wrap">
              <table>{children}</table>
            </div>
          ),
        }}
      >
        {text}
      </Markdown>
    </div>
  );
}

function ThinkingBlock({
  text,
  tools,
  streaming,
}: {
  text: string;
  tools: string[];
  streaming: boolean;
}) {
  const [open, setOpen] = useState(streaming);
  useEffect(() => {
    if (streaming) setOpen(true);
    else setOpen(false);
  }, [streaming]);
  if (!text && tools.length === 0 && !streaming) return null;
  return (
    <div className={`chat-thinking${streaming ? " live" : ""}`} data-testid="chat-thinking">
      <button type="button" className="chat-thinking-toggle" onClick={() => setOpen((v) => !v)}>
        <span className="chat-thinking-label">{streaming ? "思考中…" : "思考过程"}</span>
        <span className="chat-thinking-chevron" aria-hidden>
          {open ? "▾" : "▸"}
        </span>
      </button>
      {open ? (
        <div className="chat-thinking-body">
          {tools.length > 0 ? (
            <ul className="chat-thinking-tools">
              {tools.map((t, i) => (
                <li key={`${t}-${i}`}>{t}</li>
              ))}
            </ul>
          ) : null}
          {text ? <pre className="chat-thinking-text">{text}</pre> : null}
        </div>
      ) : null}
    </div>
  );
}

function botLabel(b: ChatBotOption) {
  return b.name && b.name !== b.id ? `${b.name} (${b.id})` : b.id;
}

function IconPlus() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden>
      <path d="M12 5v14M5 12h14" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" />
    </svg>
  );
}

function IconHistory() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden>
      <circle cx="12" cy="12" r="8.25" stroke="currentColor" strokeWidth="1.75" />
      <path d="M12 8v4.5l3 1.5" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

function IconChevron({ open }: { open: boolean }) {
  return (
    <svg
      width="14"
      height="14"
      viewBox="0 0 24 24"
      fill="none"
      aria-hidden
      style={{ transform: open ? "rotate(180deg)" : undefined, transition: "transform .15s" }}
    >
      <path d="M6 9l6 6 6-6" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

export function ChatPage({ api, bots, ready, active = true }: Props) {
  const [summaries, setSummaries] = useState<ChatSummary[]>([]);
  const [activeId, setActiveId] = useState<string | null>(() => {
    try {
      return localStorage.getItem(ACTIVE_SESSION_KEY);
    } catch {
      return null;
    }
  });
  const [session, setSession] = useState<ChatSession | null>(null);
  const [draft, setDraft] = useState("");
  const [sending, setSending] = useState(false);
  const [error, setError] = useState("");
  const [atPicker, setAtPicker] = useState(false);
  const [atFilter, setAtFilter] = useState("");
  const [atIndex, setAtIndex] = useState(0);
  const [atStart, setAtStart] = useState(-1);
  const [botMenuOpen, setBotMenuOpen] = useState(false);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [bootstrapped, setBootstrapped] = useState(false);
  const [streamReasoning, setStreamReasoning] = useState("");
  const [streamContent, setStreamContent] = useState("");
  const [streamTools, setStreamTools] = useState<string[]>([]);
  const [expandedReasoning, setExpandedReasoning] = useState<Record<string, boolean>>({});
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const bottomRef = useRef<HTMLDivElement>(null);
  const messagesRef = useRef<HTMLDivElement>(null);
  const nearBottomRef = useRef(true);
  const [showScrollBottom, setShowScrollBottom] = useState(false);
  const botMenuRef = useRef<HTMLDivElement>(null);
  const historyRef = useRef<HTMLDivElement>(null);

  const rememberActive = (id: string | null) => {
    setActiveId(id);
    try {
      if (id) localStorage.setItem(ACTIVE_SESSION_KEY, id);
      else localStorage.removeItem(ACTIVE_SESSION_KEY);
    } catch {
      /* ignore */
    }
  };

  const loadSummaries = useCallback(async () => {
    const raw = await api("GET", "/v1/chat/sessions");
    const data = JSON.parse(raw) as { sessions: ChatSummary[] };
    setSummaries(data.sessions || []);
    return data.sessions || [];
  }, [api]);

  const loadSession = useCallback(
    async (id: string) => {
      const raw = await api("GET", `/v1/chat/sessions/${encodeURIComponent(id)}`);
      const sess = JSON.parse(raw) as ChatSession;
      setSession(sess);
      rememberActive(sess.id);
      setHistoryOpen(false);
      return sess;
    },
    [api],
  );

  useEffect(() => {
    if (!ready || bootstrapped) return;
    void (async () => {
      try {
        setError("");
        const list = await loadSummaries();
        const preferred =
          (activeId && list.find((s) => s.id === activeId)?.id) || list[0]?.id || null;
        if (preferred) {
          await loadSession(preferred);
        }
      } catch (e) {
        setError(String(e));
      } finally {
        setBootstrapped(true);
      }
    })();
  }, [ready, bootstrapped, activeId, loadSummaries, loadSession]);

  const updateScrollBottomVisibility = useCallback(() => {
    const el = messagesRef.current;
    if (!el) return;
    const dist = el.scrollHeight - el.scrollTop - el.clientHeight;
    const near = dist <= 72;
    nearBottomRef.current = near;
    setShowScrollBottom(!near && el.scrollHeight > el.clientHeight + 8);
  }, []);

  const scrollToBottom = useCallback(
    (behavior: ScrollBehavior = "smooth") => {
      bottomRef.current?.scrollIntoView({ block: "end", behavior });
      nearBottomRef.current = true;
      setShowScrollBottom(false);
      window.requestAnimationFrame(updateScrollBottomVisibility);
    },
    [updateScrollBottomVisibility],
  );

  useEffect(() => {
    if (nearBottomRef.current) {
      bottomRef.current?.scrollIntoView({ block: "end" });
    } else {
      setShowScrollBottom(true);
    }
  }, [session?.messages?.length, sending, streamContent, streamReasoning, streamTools.length]);

  // When switching back to the chat page, jump to the latest messages.
  useEffect(() => {
    if (!active) return;
    const id = window.requestAnimationFrame(() => {
      nearBottomRef.current = true;
      bottomRef.current?.scrollIntoView({ block: "end" });
      setShowScrollBottom(false);
    });
    return () => window.cancelAnimationFrame(id);
  }, [active, session?.id]);

  useEffect(() => {
    const onDoc = (e: MouseEvent) => {
      const t = e.target as Node;
      if (botMenuRef.current && !botMenuRef.current.contains(t)) setBotMenuOpen(false);
      if (historyRef.current && !historyRef.current.contains(t)) setHistoryOpen(false);
    };
    document.addEventListener("mousedown", onDoc);
    return () => document.removeEventListener("mousedown", onDoc);
  }, []);

  const filteredAtBots = useMemo(() => {
    const q = atFilter.trim().toLowerCase();
    if (!q) return bots;
    return bots.filter(
      (b) => b.id.toLowerCase().includes(q) || (b.name || "").toLowerCase().includes(q),
    );
  }, [bots, atFilter]);

  useEffect(() => {
    setAtIndex(0);
  }, [atFilter, atPicker]);

  const closeAtPicker = () => {
    setAtPicker(false);
    setAtFilter("");
    setAtStart(-1);
  };

  const detectAtPicker = (value: string, caret: number) => {
    const before = value.slice(0, caret);
    const caretMatch = before.match(/(?:^|[\s])@([^\s@]*)$/);
    if (!caretMatch) {
      closeAtPicker();
      return;
    }
    const filter = caretMatch[1] || "";
    const start = before.length - filter.length - 1;
    setAtPicker(true);
    setAtStart(start);
    setAtFilter(filter);
  };

  const applyAtPicker = (bot: ChatBotOption) => {
    const el = inputRef.current;
    const value = draft;
    const start = atStart >= 0 ? atStart : value.length;
    const end = el?.selectionStart ?? value.length;
    const next = value.slice(0, start) + `@${bot.id} ` + value.slice(end);
    setDraft(next);
    closeAtPicker();
    requestAnimationFrame(() => {
      const pos = start + bot.id.length + 2;
      el?.focus();
      el?.setSelectionRange(pos, pos);
    });
  };

  const bindBot = async (bot: ChatBotOption) => {
    setBotMenuOpen(false);
    try {
      setError("");
      if (!activeId || !session) {
        const created = JSON.parse(
          await api("POST", "/v1/chat/sessions", { bot_id: bot.id }),
        ) as ChatSession;
        await loadSummaries();
        setSession(created);
        rememberActive(created.id);
        return;
      }
      const updated = JSON.parse(
        await api("PATCH", `/v1/chat/sessions/${encodeURIComponent(activeId)}`, {
          bot_id: bot.id,
        }),
      ) as ChatSession;
      setSession(updated);
      await loadSummaries();
    } catch (e) {
      setError(String(e));
    }
  };

  const ensureSession = async (botId?: string) => {
    if (activeId && session) return session;
    const defaultBot = botId || bots[0]?.id || "";
    const created = JSON.parse(
      await api("POST", "/v1/chat/sessions", { bot_id: defaultBot }),
    ) as ChatSession;
    await loadSummaries();
    setSession(created);
    rememberActive(created.id);
    return created;
  };

  const send = async () => {
    const text = draft.trim();
    if (!text || sending) return;
    setError("");
    closeAtPicker();
    setDraft("");
    setStreamReasoning("");
    setStreamContent("");
    setStreamTools([]);

    let sess: ChatSession;
    try {
      sess = await ensureSession();
    } catch (e) {
      setDraft(text);
      setError(String(e));
      return;
    }

    if (!sess.bot_id && !/^@\S+/.test(text) && !bots.length) {
      setDraft(text);
      setError("请先选择机器人，或用 @ 指定机器人");
      return;
    }

    try {
      if (!sess.bot_id && !/^@\S+/.test(text) && bots[0]) {
        const updated = JSON.parse(
          await api("PATCH", `/v1/chat/sessions/${encodeURIComponent(sess.id)}`, {
            bot_id: bots[0].id,
          }),
        ) as ChatSession;
        sess = updated;
        setSession(updated);
      }
    } catch (e) {
      setDraft(text);
      setError(String(e));
      return;
    }

    const optimistic: ChatMessage = {
      role: "user",
      content: text,
      bot_id: sess.bot_id,
      ts: new Date().toISOString(),
    };
    setSession((prev) =>
      prev && prev.id === sess.id
        ? { ...prev, messages: [...(prev.messages || []), optimistic] }
        : { ...sess, messages: [...(sess.messages || []), optimistic] },
    );
    setSending(true);

    const path = `/v1/chat/sessions/${encodeURIComponent(sess.id)}/messages/stream`;
    const onEvent = new Channel<{ event: string; data: unknown }>();
    let sawDone = false;

    onEvent.onmessage = (msg) => {
      const ev = msg?.event || "";
      const data = (msg?.data || {}) as Record<string, unknown>;
      if (ev === "reasoning") {
        const t = String(data.text || "");
        if (t) setStreamReasoning((prev) => prev + t);
      } else if (ev === "content") {
        const t = String(data.text || "");
        if (t) setStreamContent((prev) => prev + t);
      } else if (ev === "tool_start") {
        const name = String(data.name || "tool");
        setStreamTools((prev) => [...prev, `→ ${name}`]);
      } else if (ev === "tool_result") {
        const name = String(data.name || "tool");
        const snippet = String(data.text || "").slice(0, 120).replace(/\s+/g, " ");
        setStreamTools((prev) => [...prev, `✓ ${name}${snippet ? `: ${snippet}` : ""}`]);
      } else if (ev === "error") {
        setError(String(data.text || data.message || "stream error"));
      } else if (ev === "done") {
        sawDone = true;
        const session = data.session as ChatSession | undefined;
        if (session) {
          setSession(session);
          rememberActive(session.id);
        }
        setStreamReasoning("");
        setStreamContent("");
        setStreamTools([]);
      }
    };

    try {
      await invoke("bridge_chat_stream", {
        path,
        body: JSON.stringify({ content: text }),
        onEvent,
      });
      if (!sawDone) {
        const raw = await api("POST", `/v1/chat/sessions/${encodeURIComponent(sess.id)}/messages`, {
          content: text,
        });
        const data = JSON.parse(raw) as { session: ChatSession };
        setSession(data.session);
        rememberActive(data.session.id);
      }
      await loadSummaries();
    } catch (e) {
      if (sawDone) {
        setError(String(e));
        await loadSummaries();
      } else {
        try {
          const raw = await api("POST", `/v1/chat/sessions/${encodeURIComponent(sess.id)}/messages`, {
            content: text,
          });
          const data = JSON.parse(raw) as { session: ChatSession };
          setSession(data.session);
          rememberActive(data.session.id);
          await loadSummaries();
        } catch (e2) {
          setSession((prev) => {
            if (!prev) return prev;
            const msgs = prev.messages || [];
            if (msgs.length && msgs[msgs.length - 1]?.content === text && msgs[msgs.length - 1]?.role === "user") {
              return { ...prev, messages: msgs.slice(0, -1) };
            }
            return prev;
          });
          setDraft(text);
          setError(String(e2 || e));
        }
      }
    } finally {
      setSending(false);
      setStreamReasoning("");
      setStreamContent("");
      setStreamTools([]);
    }
  };

  const onNew = async () => {
    try {
      setError("");
      setHistoryOpen(false);
      const botId = session?.bot_id || bots[0]?.id || "";
      const created = JSON.parse(
        await api("POST", "/v1/chat/sessions", { bot_id: botId }),
      ) as ChatSession;
      await loadSummaries();
      setSession(created);
      rememberActive(created.id);
      setDraft("");
    } catch (e) {
      setError(String(e));
    }
  };

  const onDelete = async (id: string) => {
    try {
      await api("DELETE", `/v1/chat/sessions/${encodeURIComponent(id)}`);
      const list = await loadSummaries();
      if (activeId === id) {
        const next = list[0]?.id || null;
        if (next) await loadSession(next);
        else {
          setSession(null);
          rememberActive(null);
        }
      }
    } catch (e) {
      setError(String(e));
    }
  };

  const onKeyDown = (e: ReactKeyboardEvent<HTMLTextAreaElement>) => {
    if (atPicker && filteredAtBots.length > 0) {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setAtIndex((i) => (i + 1) % filteredAtBots.length);
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        setAtIndex((i) => (i - 1 + filteredAtBots.length) % filteredAtBots.length);
        return;
      }
      if (e.key === "Enter" && !e.shiftKey) {
        e.preventDefault();
        applyAtPicker(filteredAtBots[atIndex]);
        return;
      }
      if (e.key === "Escape") {
        e.preventDefault();
        closeAtPicker();
        return;
      }
    }
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      void send();
    }
  };

  const currentBot =
    bots.find((b) => b.id === session?.bot_id) ||
    (session?.bot_id ? { id: session.bot_id, name: session.bot_id } : null);

  return (
    <section className="page chat-page" key="chat" data-testid="page-chat">
      <header className="page-head">
        <div>
          <h1>聊天</h1>
          <p className="subtitle">本地测试已配置的机器人（不推送云之家）</p>
        </div>
      </header>
      <div className="chat-layout page-body">
        <div className="chat-main card soft">
          <div className="chat-main-head">
            <div className="chat-bot-select" ref={botMenuRef}>
              <button
                type="button"
                className="chat-bot-trigger"
                data-testid="chat-bot-trigger"
                aria-expanded={botMenuOpen}
                onClick={() => {
                  setBotMenuOpen((v) => !v);
                  setHistoryOpen(false);
                }}
              >
                <span>
                  当前机器人{" "}
                  <b data-testid="chat-current-bot">{currentBot ? botLabel(currentBot) : "未选择"}</b>
                </span>
                <span className="chat-bot-chevron" aria-hidden>
                  <IconChevron open={botMenuOpen} />
                </span>
              </button>
              {botMenuOpen ? (
                <div className="chat-dropdown" data-testid="chat-bot-menu" role="listbox">
                  {bots.length === 0 ? (
                    <div className="chat-picker-empty">暂无已配置机器人</div>
                  ) : (
                    bots.map((b) => (
                      <button
                        key={b.id}
                        type="button"
                        role="option"
                        aria-selected={b.id === session?.bot_id}
                        className={`chat-picker-item${b.id === session?.bot_id ? " active" : ""}`}
                        onClick={() => void bindBot(b)}
                      >
                        <span>{botLabel(b)}</span>
                        {b.backend ? <span className="chat-picker-backend">{b.backend}</span> : null}
                      </button>
                    ))
                  )}
                </div>
              ) : null}
            </div>

            <div className="chat-head-actions" ref={historyRef}>
              <button
                type="button"
                className="chat-icon-btn"
                data-testid="chat-new"
                title="新建会话"
                aria-label="新建会话"
                onClick={() => void onNew()}
              >
                <IconPlus />
              </button>
              <button
                type="button"
                className={`chat-icon-btn${historyOpen ? " active" : ""}`}
                data-testid="chat-history"
                title="历史会话"
                aria-label="历史会话"
                aria-expanded={historyOpen}
                onClick={() => {
                  setHistoryOpen((v) => !v);
                  setBotMenuOpen(false);
                  if (!historyOpen) void loadSummaries().catch((e) => setError(String(e)));
                }}
              >
                <IconHistory />
              </button>
              {historyOpen ? (
                <div className="chat-history-panel" data-testid="chat-history-panel">
                  <div className="chat-history-title">历史会话</div>
                  {summaries.length === 0 ? (
                    <div className="chat-picker-empty">暂无历史</div>
                  ) : (
                    summaries.map((s) => (
                      <div
                        key={s.id}
                        className={`chat-history-item${activeId === s.id ? " active" : ""}`}
                        data-testid={`chat-session-${s.id}`}
                      >
                        <button
                          type="button"
                          className="chat-history-main"
                          onClick={() => void loadSession(s.id).catch((e) => setError(String(e)))}
                        >
                          <span className="chat-session-title">{s.title || "未命名会话"}</span>
                          <span className="chat-session-meta">
                            {s.bot_id ? `@${s.bot_id}` : "未绑定"} · {s.message_count ?? 0} 条
                          </span>
                        </button>
                        <button
                          type="button"
                          className="btn ghost small chat-session-del"
                          title="删除"
                          onClick={() => void onDelete(s.id)}
                        >
                          ×
                        </button>
                      </div>
                    ))
                  )}
                </div>
              ) : null}
            </div>
          </div>

          <div className="chat-messages-wrap">
          <div
            className="chat-messages"
            data-testid="chat-messages"
            ref={messagesRef}
            onScroll={updateScrollBottomVisibility}
          >
            {!session ? (
              <div className="empty chat-empty">点右上角 + 新建会话，或从历史打开</div>
            ) : session.messages?.length || sending ? (
              <>
                {(session.messages || []).map((m, i) => {
                  const key = `${m.ts}-${i}`;
                  const reasoningOpen = expandedReasoning[key];
                  return (
                    <div
                      key={key}
                      className={`chat-bubble ${m.role === "user" ? "user" : "assistant"}`}
                    >
                      {m.role !== "user" && m.bot_id ? (
                        <div className="chat-bubble-meta">{m.bot_id}</div>
                      ) : null}
                      {m.role === "assistant" ? (
                        <>
                          {m.reasoning ? (
                            <div className="chat-thinking" data-testid="chat-thinking-saved">
                              <button
                                type="button"
                                className="chat-thinking-toggle"
                                onClick={() =>
                                  setExpandedReasoning((prev) => ({
                                    ...prev,
                                    [key]: !prev[key],
                                  }))
                                }
                              >
                                <span className="chat-thinking-label">思考过程</span>
                                <span className="chat-thinking-chevron" aria-hidden>
                                  {reasoningOpen ? "▾" : "▸"}
                                </span>
                              </button>
                              {reasoningOpen ? (
                                <div className="chat-thinking-body">
                                  <pre className="chat-thinking-text">{m.reasoning}</pre>
                                </div>
                              ) : null}
                            </div>
                          ) : null}
                          <ChatMarkdown>{m.content}</ChatMarkdown>
                        </>
                      ) : (
                        <div className="chat-bubble-body">{m.content}</div>
                      )}
                    </div>
                  );
                })}
                {sending ? (
                  <div className="chat-bubble assistant" data-testid="chat-stream-bubble">
                    <ThinkingBlock
                      text={streamReasoning}
                      tools={streamTools}
                      streaming
                    />
                    {streamContent ? (
                      <>
                        <ChatMarkdown>{streamContent}</ChatMarkdown>
                        <span className="chat-caret" aria-hidden>
                          ▍
                        </span>
                      </>
                    ) : !streamReasoning && streamTools.length === 0 ? (
                      <div className="chat-typing">连接模型中…</div>
                    ) : null}
                  </div>
                ) : null}
              </>
            ) : (
              <div className="empty chat-empty">
                输入消息开始。可用 <kbd>@</kbd> 指定本条机器人。
              </div>
            )}
            <div ref={bottomRef} />
          </div>
          {showScrollBottom ? (
            <button
              type="button"
              className="chat-scroll-bottom"
              data-testid="chat-scroll-bottom"
              title="滚动到底部"
              aria-label="滚动到底部"
              onClick={() => scrollToBottom("smooth")}
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

          <div className="chat-composer">
            <div className="chat-composer-hint">
              <kbd>@</kbd> 指定机器人 · Enter 发送 · Shift+Enter 换行
            </div>
            {atPicker ? (
              <div className="chat-picker" data-testid="chat-picker" role="listbox">
                {filteredAtBots.length === 0 ? (
                  <div className="chat-picker-empty">无匹配机器人</div>
                ) : (
                  filteredAtBots.map((b, i) => (
                    <button
                      key={b.id}
                      type="button"
                      role="option"
                      aria-selected={i === atIndex}
                      className={`chat-picker-item${i === atIndex ? " active" : ""}`}
                      onMouseDown={(ev) => {
                        ev.preventDefault();
                        applyAtPicker(b);
                      }}
                    >
                      <span>{botLabel(b)}</span>
                      {b.backend ? <span className="chat-picker-backend">{b.backend}</span> : null}
                    </button>
                  ))
                )}
              </div>
            ) : null}
            <textarea
              ref={inputRef}
              className="chat-input"
              data-testid="chat-input"
              rows={3}
              disabled={sending || !ready}
              placeholder={ready ? "输入消息… 使用 @ 指定机器人" : "等待桥就绪…"}
              value={draft}
              onChange={(e) => {
                const v = e.target.value;
                setDraft(v);
                detectAtPicker(v, e.target.selectionStart ?? v.length);
              }}
              onKeyUp={(e) => {
                const t = e.currentTarget;
                detectAtPicker(t.value, t.selectionStart ?? t.value.length);
              }}
              onClick={(e) => {
                const t = e.currentTarget;
                detectAtPicker(t.value, t.selectionStart ?? t.value.length);
              }}
              onKeyDown={onKeyDown}
            />
            {error ? (
              <div className="err" data-testid="chat-error">
                {error}
              </div>
            ) : null}
          </div>
        </div>
      </div>
    </section>
  );
}
