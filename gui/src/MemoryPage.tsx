import { useCallback, useEffect, useState } from "react";

type Field = {
  manual?: string;
  inferred?: string;
  locked?: boolean;
};

type DontsField = {
  manual?: string[];
  inferred?: string[];
  locked?: boolean;
};

type Profile = {
  open_id: string;
  display_name?: string;
  how_to_address?: Field;
  role?: Field;
  ask_style?: Field;
  reply_style?: Field;
  donts?: DontsField;
  notes?: Field;
  last_seen?: string;
  bots_seen?: string[];
  opted_out?: boolean;
  turn_count?: number;
  profiled_count?: number;
  last_error?: string;
};

type ApiFn = (method: string, path: string, body?: unknown) => Promise<string>;

type Props = {
  api: ApiFn;
  ready: boolean;
  bots: { id: string; name: string }[];
};

function fieldText(f?: Field): string {
  const m = (f?.manual || "").trim();
  if (m) return m;
  return (f?.inferred || "").trim();
}

export function MemoryPage({ api, ready, bots }: Props) {
  const [profiles, setProfiles] = useState<Profile[]>([]);
  const [botFilter, setBotFilter] = useState("");
  const [selected, setSelected] = useState<Profile | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [draft, setDraft] = useState({
    display_name: "",
    how_to_address: "",
    role: "",
    ask_style: "",
    reply_style: "",
    donts: "",
    notes: "",
  });

  const refresh = useCallback(async () => {
    if (!ready) return;
    setError("");
    const q = botFilter ? `?bot=${encodeURIComponent(botFilter)}` : "";
    const raw = await api("GET", `/v1/memory/profiles${q}`);
    const data = JSON.parse(raw) as { profiles?: Profile[] };
    setProfiles(data.profiles || []);
  }, [api, ready, botFilter]);

  useEffect(() => {
    void refresh().catch((e) => setError(String(e)));
  }, [refresh]);

  useEffect(() => {
    if (!selected) return;
    setDraft({
      display_name: selected.display_name || "",
      how_to_address: fieldText(selected.how_to_address),
      role: fieldText(selected.role),
      ask_style: fieldText(selected.ask_style),
      reply_style: fieldText(selected.reply_style),
      donts: (selected.donts?.manual?.length
        ? selected.donts.manual
        : selected.donts?.inferred || []
      ).join("；"),
      notes: fieldText(selected.notes),
    });
  }, [selected]);

  async function savePatch() {
    if (!selected) return;
    setBusy(true);
    setError("");
    try {
      const body = {
        display_name: draft.display_name,
        how_to_address: { manual: draft.how_to_address },
        role: { manual: draft.role },
        ask_style: { manual: draft.ask_style },
        reply_style: { manual: draft.reply_style },
        notes: { manual: draft.notes },
        donts: {
          manual: draft.donts
            .split(/[；;,\n]/)
            .map((s) => s.trim())
            .filter(Boolean),
        },
      };
      const raw = await api(
        "PATCH",
        `/v1/memory/profiles/${encodeURIComponent(selected.open_id)}`,
        body,
      );
      const p = JSON.parse(raw) as Profile;
      setSelected(p);
      await refresh();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }

  async function resetInferred() {
    if (!selected) return;
    setBusy(true);
    try {
      const raw = await api(
        "POST",
        `/v1/memory/profiles/${encodeURIComponent(selected.open_id)}/reset-inferred`,
      );
      setSelected(JSON.parse(raw) as Profile);
      await refresh();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }

  async function runProfiler() {
    if (!selected) return;
    setBusy(true);
    try {
      await api("POST", `/v1/memory/profiles/${encodeURIComponent(selected.open_id)}/run`);
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }

  async function deleteProfile() {
    if (!selected) return;
    if (!confirm(`确认删除用户记忆档案 ${selected.open_id}？此操作不可恢复。`)) return;
    setBusy(true);
    try {
      await api(
        "DELETE",
        `/v1/memory/profiles/${encodeURIComponent(selected.open_id)}?confirm=1`,
      );
      setSelected(null);
      await refresh();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }

  async function toggleLock(field: string, locked: boolean) {
    if (!selected) return;
    setBusy(true);
    try {
      const raw = await api(
        "POST",
        `/v1/memory/profiles/${encodeURIComponent(selected.open_id)}/lock`,
        { fields: { [field]: locked } },
      );
      setSelected(JSON.parse(raw) as Profile);
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="page" key="memory" data-testid="page-memory">
      <header className="page-head">
        <div>
          <h1>记忆</h1>
          <p className="subtitle">按云之家 openID 维护的用户画像（不含对话原文）</p>
        </div>
        <div className="head-actions">
          <button type="button" className="btn" data-testid="memory-refresh" onClick={() => void refresh()}>
            刷新
          </button>
        </div>
      </header>
      <div className="stack page-body memory-layout">
        {error ? (
          <div className="err" data-testid="memory-error">
            {error}
          </div>
        ) : null}
        <div className="row" style={{ gap: 12, alignItems: "center" }}>
          <label>
            按机器人过滤{" "}
            <select
              data-testid="memory-bot-filter"
              value={botFilter}
              onChange={(e) => setBotFilter(e.target.value)}
            >
              <option value="">全部</option>
              {bots.map((b) => (
                <option key={b.id} value={b.id}>
                  {b.name || b.id}
                </option>
              ))}
            </select>
          </label>
        </div>
        <div className="memory-split">
          <div className="card soft pad" data-testid="memory-list">
            <h3 className="section-inline">用户列表</h3>
            {profiles.length === 0 ? (
              <p className="muted">暂无档案（需完成问答或已有画像）</p>
            ) : (
              <ul className="memory-user-list">
                {profiles.map((p) => (
                  <li key={p.open_id}>
                    <button
                      type="button"
                      className={selected?.open_id === p.open_id ? "active" : ""}
                      data-testid={`memory-user-${p.open_id}`}
                      onClick={() => setSelected(p)}
                    >
                      <strong>{p.display_name || p.open_id}</strong>
                      <span>
                        问答 {p.turn_count ?? 0}
                        {p.opted_out ? " · 已关闭" : ""}
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>
          <div className="card soft pad" data-testid="memory-detail">
            {!selected ? (
              <p className="muted">选择左侧用户查看结构化画像</p>
            ) : (
              <>
                <h3 className="section-inline">{selected.open_id}</h3>
                <p className="muted" data-testid="memory-meta">
                  问答对 {selected.turn_count ?? 0} · 已画像游标 {selected.profiled_count ?? 0}
                  {selected.last_error ? ` · 错误 ${selected.last_error}` : ""}
                </p>
                <div className="form-grid">
                  <label className="full">
                    <span className="field-label">显示名</span>
                    <input
                      data-testid="memory-field-display_name"
                      value={draft.display_name}
                      onChange={(e) => setDraft({ ...draft, display_name: e.target.value })}
                    />
                  </label>
                  {(
                    [
                      ["how_to_address", "如何称呼", selected.how_to_address],
                      ["role", "角色", selected.role],
                      ["ask_style", "提问风格", selected.ask_style],
                      ["reply_style", "回答风格", selected.reply_style],
                      ["notes", "风格说明", selected.notes],
                    ] as const
                  ).map(([key, label, field]) => (
                    <label key={key} className="full">
                      <span className="field-label">
                        {label}
                        <button
                          type="button"
                          className="btn ghost small"
                          style={{ marginLeft: 8 }}
                          data-testid={`memory-lock-${key}`}
                          onClick={() => void toggleLock(key, !field?.locked)}
                        >
                          {field?.locked ? "解锁" : "锁定"}
                        </button>
                      </span>
                      <input
                        data-testid={`memory-field-${key}`}
                        value={draft[key]}
                        onChange={(e) => setDraft({ ...draft, [key]: e.target.value })}
                      />
                    </label>
                  ))}
                  <label className="full">
                    <span className="field-label">忌口（；分隔）</span>
                    <input
                      data-testid="memory-field-donts"
                      value={draft.donts}
                      onChange={(e) => setDraft({ ...draft, donts: e.target.value })}
                    />
                  </label>
                </div>
                <div className="head-actions" style={{ marginTop: 12, gap: 8 }}>
                  <button
                    type="button"
                    className="btn"
                    data-testid="memory-save"
                    disabled={busy}
                    onClick={() => void savePatch()}
                  >
                    保存手动字段
                  </button>
                  <button
                    type="button"
                    className="btn ghost"
                    data-testid="memory-reset-inferred"
                    disabled={busy}
                    onClick={() => void resetInferred()}
                  >
                    只重置推断
                  </button>
                  <button
                    type="button"
                    className="btn ghost"
                    data-testid="memory-run"
                    disabled={busy}
                    onClick={() => void runProfiler()}
                  >
                    立即画像
                  </button>
                  <button
                    type="button"
                    className="btn danger"
                    data-testid="memory-delete"
                    disabled={busy}
                    onClick={() => void deleteProfile()}
                  >
                    删除档案
                  </button>
                </div>
              </>
            )}
          </div>
        </div>
      </div>
    </section>
  );
}
