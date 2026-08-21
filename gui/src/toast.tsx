import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";

export type ToastTone = "ok" | "err" | "neutral";

type ToastState = {
  id: number;
  message: string;
  tone: ToastTone;
};

type ToastApi = {
  showToast: (message: string, tone?: ToastTone) => void;
};

const ToastContext = createContext<ToastApi | null>(null);

const TOAST_MS = 2200;
const TOAST_MAX = 3;

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<ToastState[]>([]);
  const seqRef = useRef(0);
  /** 当前在列 toast 的 id（按出现顺序），用于超限时挤出最旧一条。 */
  const activeIdsRef = useRef<number[]>([]);
  /** id → 自动关闭定时器，便于挤出时取消。 */
  const timersRef = useRef(new Map<number, number>());

  const dismiss = useCallback((id: number) => {
    const t = timersRef.current.get(id);
    if (t) {
      window.clearTimeout(t);
      timersRef.current.delete(id);
    }
    activeIdsRef.current = activeIdsRef.current.filter((x) => x !== id);
    setToasts((prev) => prev.filter((x) => x.id !== id));
  }, []);

  const showToast = useCallback(
    (message: string, tone: ToastTone = "neutral") => {
      seqRef.current += 1;
      const id = seqRef.current;
      activeIdsRef.current.push(id);
      // 最多 3 条：第 4 条出现时立即关闭最旧的一条。
      if (activeIdsRef.current.length > TOAST_MAX) {
        const oldest = activeIdsRef.current.shift()!;
        dismiss(oldest);
      }
      setToasts((prev) => [...prev, { id, message, tone }]);
      timersRef.current.set(id, window.setTimeout(() => dismiss(id), TOAST_MS));
    },
    [dismiss],
  );

  const api = useMemo(() => ({ showToast }), [showToast]);

  return (
    <ToastContext.Provider value={api}>
      {children}
      {toasts.length ? (
        <div className="toast-stack" data-testid="toast-stack">
          {toasts.map((t) => (
            <div
              key={t.id}
              className={`toast ${t.tone}`}
              data-testid="save-toast"
              data-tone={t.tone}
              role="status"
            >
              {t.message}
            </div>
          ))}
        </div>
      ) : null}
    </ToastContext.Provider>
  );
}

export function useToast(): ToastApi {
  const ctx = useContext(ToastContext);
  if (!ctx) {
    throw new Error("useToast must be used within ToastProvider");
  }
  return ctx;
}
