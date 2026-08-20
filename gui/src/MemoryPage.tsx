import { useCallback, useEffect, useMemo, useState } from "react";
import { FancySelect } from "./FancySelect";

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

function pendingTurns(p: Profile): number {
  return Math.max(0, (p.turn_count ?? 0) - (p.profiled_count ?? 0));
}

function formatSeen(iso?: string): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString("zh-CN", { hour12: false });
}

export function MemoryPage({ api, ready, bots }: Props) {
  const [profiles, setProfiles] = useState<Profile[]>([]);
  const [botFilter, setBotFilter] = useState("");
  const [selected, setSelected] = useState<Profile | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [draft, setDraft] = useState({
    display_name: "",
    how_to_address: "",
    role: "",
    ask_style: "",
    reply_style: "",
    donts: "",
    notes: "",
  });

  const botOptions = useMemo(
    () => [
      { id: "", label: "全部机器人" },
      ...bots.map((b) => ({ id: b.id, label: b.name || b.id })),
    ],
    [bots],
  );

  const refresh = useCallback(async () => {
    if (!ready) return;
    setError("");
    setRefreshing(true);
    try {
      const q = botFilter ? `?bot=${encodeURIComponent(botFilter)}` : "";
      const raw = await api("GET", `/v1/memory/profiles${q}`);
      const data = JSON.parse(raw) as { profiles?: Profile[] };
      const list = data.profiles || [];
      setProfiles(list);
      setSelected((prev) => {
        if (!prev) return null;
        return list.find((p) => p.open_id === prev.open_id) || null;
      });
    } finally {
      setRefreshing(false);
    }
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
      await refresh();
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
    <section className="page memory-page" key="memory" data-testid="page-memory">
      <header className="page-head">
        <div>
          <h1>记忆</h1>
          <p className="subtitle">按云之家 openID 维护的用户画像（不含对话原文）</p>
        </div>
        <div className="head-actions memory-head-actions">
          <div className="memory-filter" data-testid="memory-filter">
            <span className="memory-filter-label">机器人</span>
            <FancySelect
              testId="memory-bot-filter"
              className="compact"
              value={botFilter}
              options={botOptions}
              searchable={bots.length > 6}
              onChange={setBotFilter}
            />
          </div>
          <button
            type="button"
            className={`action-chip${refreshing ? " loading" : ""}`}
            disabled={refreshing || !ready}
            data-testid="memory-refresh"
            onClick={() => void refresh().catch((e) => setError(String(e)))}
          >
            {refreshing ? <span className="spinner dark" /> : null}
            <span>{refreshing ? "刷新中" : "刷新"}</span>
          </button>
        </div>
      </header>

      {error ? (
        <div className="err memory-page-error" data-testid="memory-error">
          {error}
        </div>
      ) : null}

      <div className="memory-layout page-body">
        <div className="card soft pad memory-panel" data-testid="memory-list">
          <div className="memory-panel-head">
            <div>
              <h3 className="section-inline">用户列表</h3>
              <p className="group-desc">有完成问答或已有档案的人会出现在这里</p>
            </div>
            <span className="memory-count">{profiles.length}</span>
          </div>
          {profiles.length === 0 ? (
            <div className="memory-empty">
              <strong>暂无档案</strong>
              <span>开启 AI 设置中的「用户记忆」后，完成若干问答或手动画像即可出现</span>
            </div>
          ) : (
            <ul className="memory-user-list">
              {profiles.map((p) => {
                const pending = pendingTurns(p);
                const active = selected?.open_id === p.open_id;
                return (
                  <li key={p.open_id}>
                    <button
                      type="button"
                      className={active ? "active" : ""}
                      data-testid={`memory-user-${p.open_id}`}
                      onClick={() => setSelected(p)}
                    >
                      <span className="memory-user-top">
                        <strong>{p.display_name || p.open_id}</strong>
                        {p.opted_out ? <span className="memory-chip warn">已关闭</span> : null}
                        {p.last_error ? <span className="memory-chip danger">失败</span> : null}
                      </span>
                      <span className="memory-user-id">{p.open_id}</span>
                      <span className="memory-user-meta">
                        问答 {p.turn_count ?? 0}
                        {pending > 0 ? ` · 待画像 ${pending}` : ""}
                        {p.bots_seen?.length ? ` · ${p.bots_seen.length} bot` : ""}
                      </span>
                    </button>
                  </li>
                );
              })}
            </ul>
          )}
        </div>

        <div className="card soft pad memory-panel memory-detail" data-testid="memory-detail">
          {!selected ? (
            <>
              <div className="memory-panel-head">
                <div>
                  <h3 className="section-inline">画像详情</h3>
                  <p className="group-desc">选择左侧用户后可编辑手动字段、锁定与画像操作</p>
                </div>
              </div>
              <div className="memory-empty">
                <strong>尚未选择用户</strong>
                <span>从左侧列表点选一条档案，查看结构化画像并维护</span>
              </div>
            </>
          ) : (
            <>
              <div className="memory-panel-head">
                <div>
                  <h3 className="section-inline">{selected.display_name || selected.open_id}</h3>
                  <p className="group-desc memory-openid" title={selected.open_id}>
                    {selected.open_id}
                  </p>
                </div>
              </div>
              <div className="memory-meta-row" data-testid="memory-meta">
                <span className="memory-stat">
                  <em>问答对</em>
                  <strong>{selected.turn_count ?? 0}</strong>
                </span>
                <span className="memory-stat">
                  <em>已画像</em>
                  <strong>{selected.profiled_count ?? 0}</strong>
                </span>
                <span className="memory-stat">
                  <em>待更新</em>
                  <strong>{pendingTurns(selected)}</strong>
                </span>
                <span className="memory-stat wide">
                  <em>最近出现</em>
                  <strong>{formatSeen(selected.last_seen)}</strong>
                </span>
              </div>
              {selected.last_error ? (
                <div className="memory-last-error" data-testid="memory-last-error">
                  上次画像：{selected.last_error}
                </div>
              ) : null}
              <div className="form-grid memory-fields">
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
                    <span className="field-label memory-field-label">
                      <span>{label}</span>
                      <button
                        type="button"
                        className={`btn ghost tiny${field?.locked ? " memory-lock-on" : ""}`}
                        data-testid={`memory-lock-${key}`}
                        disabled={busy}
                        onClick={() => void toggleLock(key, !field?.locked)}
                      >
                        {field?.locked ? "已锁定" : "锁定"}
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
              <div className="memory-actions">
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
    </section>
  );
}
