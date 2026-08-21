import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, KeyboardEvent as ReactKeyboardEvent, ReactNode } from "react";
import { createPortal } from "react-dom";

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

export function FancySelect<T extends string>({
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
  options: { id: T; label: string; icon?: ReactNode }[];
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
    // 向上展开时以菜单实际渲染高度定位（受 maxHeight 约束），保证菜单底边紧贴
    // trigger 顶边（间隔 gap）；选项少时菜单矮，避免出现一大段空隙。
    let height = Math.max(140, Math.min(maxH, openUp ? spaceAbove : spaceBelow));
    if (openUp && menuRef.current) {
      const actual = menuRef.current.offsetHeight;
      if (actual > 0) {
        height = Math.min(actual, height);
      }
    }
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

  // 首次打开时菜单刚挂载到 portal，offsetHeight 可能为 0；等一帧再精确定位，
  // 保证向上展开时底边紧贴 trigger（选项多时 maxHeight 生效后高度才稳定）。
  useLayoutEffect(() => {
    if (!open) return;
    const t = window.setTimeout(placeMenu, 0);
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
                    {o.icon ? <span className="fancy-option-icon">{o.icon}</span> : null}
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
        {current?.icon ? (
          <span className="fancy-trigger-icon">{current.icon}</span>
        ) : null}
        <span className="fancy-select-label">
          {current?.label || value || placeholder || "请选择"}
        </span>
        <span className="fancy-caret" />
      </button>
      {menu}
    </div>
  );
}
