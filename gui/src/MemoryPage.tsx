import { useCallback, useEffect, useState } from "react";
import { useToast } from "./toast";

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
};

function fieldText(f?: Field): string {
  return (f?.manual || "").trim();
}

function inferredText(f?: Field): string {
  return (f?.inferred || "").trim();
}

function fieldSource(f?: Field): "manual" | "inferred" | "empty" {
  if (fieldText(f)) return "manual";
  if (inferredText(f)) return "inferred";
  return "empty";
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

export function MemoryPage({ api, ready }: Props) {
  const { showToast } = useToast();
  const [profiles, setProfiles] = useState<Profile[]>([]);
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
  const refresh = useCallback(
    async (opts?: { notify?: boolean }) => {
      if (!ready) return;
      const notify = !!opts?.notify;
      setError("");
      setRefreshing(true);
      try {
        const raw = await api("GET", "/v1/memory/profiles");
        const data = JSON.parse(raw) as { profiles?: Profile[] };
        const list = data.profiles || [];
        setProfiles(list);
        setSelected((prev) => {
          if (!prev) return null;
          return list.find((p) => p.open_id === prev.open_id) || null;
        });
        if (notify) showToast(`已刷新记忆（${list.length}）`, "ok");
      } catch (e) {
        const msg = String(e);
        setError(msg);
        if (notify) showToast(`刷新失败：${msg}`, "err");
        throw e;
      } finally {
        setRefreshing(false);
      }
    },
    [api, ready, showToast],
  );

  useEffect(() => {
    void refresh().catch(() => {
      /* initial load errors stay in page error bar */
    });
  }, [refresh]);

  useEffect(() => {
    if (!selected) return;
    setDraft({
      display_name: selected.display_name || "",
      // 输入框只编辑手动值；推断值单独展示为徽标/小字，避免误固化。
      how_to_address: fieldText(selected.how_to_address),
      role: fieldText(selected.role),
      ask_style: fieldText(selected.ask_style),
      reply_style: fieldText(selected.reply_style),
      donts: (selected.donts?.manual || []).join("；"),
      notes: fieldText(selected.notes),
    });
  }, [selected]);

  async function savePatch() {
    if (!selected) return;
    setBusy(true);
    setError("");
    try {
      // 只提交用户实际动过的字段；清空某字段时发送 manual:"" 以删除手动值（推断值保留）。
      const body: Record<string, unknown> = {};
      if (draft.display_name !== (selected.display_name || "")) {
        body.display_name = draft.display_name;
      }
      const setIfChanged = (key: string, value: string, field?: Field) => {
        if (value === fieldText(field)) return;
        body[key] = { manual: value };
      };
      setIfChanged("how_to_address", draft.how_to_address, selected.how_to_address);
      setIfChanged("role", draft.role, selected.role);
      setIfChanged("ask_style", draft.ask_style, selected.ask_style);
      setIfChanged("reply_style", draft.reply_style, selected.reply_style);
      setIfChanged("notes", draft.notes, selected.notes);
      const donts = draft.donts
        .split(/[；;,\n]/)
        .map((s) => s.trim())
        .filter(Boolean);
      const oldDonts = (selected.donts?.manual || []).join("；");
      if (donts.join("；") !== oldDonts) {
        body.donts = { manual: donts };
      }
      const raw = await api(
        "PATCH",
        `/v1/memory/profiles/${encodeURIComponent(selected.open_id)}`,
        body,
      );
      const p = JSON.parse(raw) as Profile;
      setSelected(p);
      await refresh();
      showToast("手动字段已保存", "ok");
    } catch (e) {
      const msg = String(e);
      setError(msg);
      showToast(`保存失败：${msg}`, "err");
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
      showToast("已重置推断字段", "ok");
    } catch (e) {
      const msg = String(e);
      setError(msg);
      showToast(`重置失败：${msg}`, "err");
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
      showToast("画像任务已触发", "ok");
    } catch (e) {
      const msg = String(e);
      setError(msg);
      showToast(`画像失败：${msg}`, "err");
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
      showToast("档案已删除", "ok");
    } catch (e) {
      const msg = String(e);
      setError(msg);
      showToast(`删除失败：${msg}`, "err");
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
      showToast(locked ? "字段已锁定" : "字段已解锁", "ok");
    } catch (e) {
      const msg = String(e);
      setError(msg);
      showToast(`锁定失败：${msg}`, "err");
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
        <div className="head-actions">
          <button
            type="button"
            className={`action-chip${refreshing ? " loading" : ""}`}
            disabled={refreshing || !ready}
            data-testid="memory-refresh"
            onClick={() => void refresh({ notify: true })}
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
                    placeholder="（未设置）"
                  />
                  <span className="memory-field-hint">仅手动设置，不参与自动画像</span>
                </label>
                {(
                  [
                    ["how_to_address", "如何称呼", selected.how_to_address],
                    ["role", "角色", selected.role],
                    ["ask_style", "提问风格", selected.ask_style],
                    ["reply_style", "回答风格", selected.reply_style],
                    ["notes", "风格说明", selected.notes],
                  ] as const
                ).map(([key, label, field]) => {
                  const src = fieldSource(field);
                  const locked = !!field?.locked;
                  const inferred = inferredText(field);
                  return (
                    <label key={key} className="full">
                      <span className="field-label memory-field-label">
                        <span className="memory-field-name">
                          <span>{label}</span>
                          {src !== "empty" ? (
                            <span
                              className={`memory-src-tag ${src}`}
                              data-testid={`memory-src-${key}`}
                              data-tip={src === "inferred" ? inferred : undefined}
                              title={
                                src === "manual"
                                  ? "手动设置：人工填写的值，优先于推断"
                                  : "推断值（画像器自动提取）：" + inferred
                              }
                            >
                              {src === "manual" ? "手动" : "推断"}
                            </span>
                          ) : null}
                          {locked ? (
                            <span
                              className="memory-src-tag locked"
                              data-testid={`memory-locked-${key}`}
                              title="已锁定：画像器不再自动更新此字段"
                            >
                              已锁定
                            </span>
                          ) : null}
                        </span>
                        <button
                          type="button"
                          className={`btn ghost xs${locked ? " memory-lock-on" : ""}`}
                          data-testid={`memory-lock-${key}`}
                          disabled={busy}
                          onClick={(e) => {
                            // 按钮在 <label> 内：阻止 label 默认聚焦 input，避免点击范围误触。
                            e.preventDefault();
                            e.stopPropagation();
                            void toggleLock(key, !locked);
                          }}
                          title={
                            locked
                              ? "已锁定：画像器不会自动覆盖此字段。点击解锁。"
                              : "锁定后画像器不会自动覆盖此字段（手动编辑仍可保存）。"
                          }
                        >
                          <span aria-hidden="true">{locked ? "🔒" : "🔓"}</span>
                          {locked ? "已锁定" : "锁定"}
                        </button>
                      </span>
                      <input
                        data-testid={`memory-field-${key}`}
                        value={draft[key]}
                        onChange={(e) => setDraft({ ...draft, [key]: e.target.value })}
                        placeholder={
                          src === "inferred" ? inferred : "（未设置）"
                        }
                      />
                    </label>
                  );
                })}
                <label className="full">
                  <span className="field-label memory-field-label">
                    <span className="memory-field-name">
                      <span>忌口（；分隔）</span>
                      {(() => {
                        const d = selected.donts;
                        const dManual = (d?.manual || []).join("；").trim();
                        const dInferred = (d?.inferred || []).join("；").trim();
                        const dSrc: "manual" | "inferred" | "empty" = dManual
                          ? "manual"
                          : dInferred
                            ? "inferred"
                            : "empty";
                        return (
                          <>
                            {dSrc !== "empty" ? (
                              <span
                                className={`memory-src-tag ${dSrc}`}
                                data-testid="memory-src-donts"
                                data-tip={dSrc === "inferred" ? dInferred : undefined}
                                title={
                                  dSrc === "manual"
                                    ? "手动设置：人工填写的值，优先于推断"
                                    : "推断值（画像器自动提取）：" + dInferred
                                }
                              >
                                {dSrc === "manual" ? "手动" : "推断"}
                              </span>
                            ) : null}
                            {d?.locked ? (
                              <span
                                className="memory-src-tag locked"
                                data-testid="memory-locked-donts"
                                title="已锁定：画像器不再自动更新此字段"
                              >
                                已锁定
                              </span>
                            ) : null}
                          </>
                        );
                      })()}
                    </span>
                    <button
                      type="button"
                      className={`btn ghost xs${selected.donts?.locked ? " memory-lock-on" : ""}`}
                      data-testid="memory-lock-donts"
                      disabled={busy}
                      onClick={(e) => {
                        e.preventDefault();
                        e.stopPropagation();
                        void toggleLock("donts", !selected.donts?.locked);
                      }}
                      title={
                        selected.donts?.locked
                          ? "已锁定：画像器不会自动覆盖此字段。点击解锁。"
                          : "锁定后画像器不会自动覆盖此字段（手动编辑仍可保存）。"
                      }
                    >
                      <span aria-hidden="true">{selected.donts?.locked ? "🔒" : "🔓"}</span>
                      {selected.donts?.locked ? "已锁定" : "锁定"}
                    </button>
                  </span>
                  <input
                    data-testid="memory-field-donts"
                    value={draft.donts}
                    onChange={(e) => setDraft({ ...draft, donts: e.target.value })}
                    placeholder={
                      selected.donts?.inferred?.length
                        ? selected.donts.inferred.join("；")
                        : "（未设置）"
                    }
                  />
                </label>
              </div>
              <div className="memory-actions">
                <button
                  type="button"
                  className={`btn${busy ? " loading" : ""}`}
                  data-testid="memory-save"
                  disabled={busy}
                  onClick={() => void savePatch()}
                >
                  {busy ? <span className="spinner dark" /> : null}
                  <span>保存手动字段</span>
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
