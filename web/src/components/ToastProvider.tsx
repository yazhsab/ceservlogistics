import { AlertCircle, CheckCircle2, Info, X } from "lucide-react";
import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { cn } from "../lib/utils";

type ToastTone = "success" | "error" | "info";
interface Toast {
  id: number;
  title: string;
  description?: string;
  tone: ToastTone;
}
interface ToastContextValue {
  toast: (toast: Omit<Toast, "id">) => void;
}
const ToastContext = createContext<ToastContextValue | undefined>(undefined);

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const remove = useCallback(
    (id: number) =>
      setToasts((current) => current.filter((item) => item.id !== id)),
    [],
  );
  const toast = useCallback(
    (item: Omit<Toast, "id">) => {
      const id = Date.now() + Math.random();
      setToasts((current) => [...current, { ...item, id }]);
      window.setTimeout(() => remove(id), 5000);
    },
    [remove],
  );
  const value = useMemo(() => ({ toast }), [toast]);
  return (
    <ToastContext.Provider value={value}>
      {children}
      <div
        className="fixed right-4 top-4 z-[100] flex w-[min(380px,calc(100%-2rem))] flex-col gap-2"
        aria-live="polite"
      >
        {toasts.map((item) => {
          const Icon =
            item.tone === "success"
              ? CheckCircle2
              : item.tone === "error"
                ? AlertCircle
                : Info;
          return (
            <div
              key={item.id}
              role={item.tone === "error" ? "alert" : "status"}
              className={cn(
                "flex items-start gap-3 rounded-lg border bg-white p-4 shadow-overlay",
                item.tone === "success"
                  ? "border-emerald-200"
                  : item.tone === "error"
                    ? "border-red-200"
                    : "border-sky-200",
              )}
            >
              <Icon
                aria-hidden
                className={cn(
                  "mt-0.5 h-5 w-5 shrink-0",
                  item.tone === "success"
                    ? "text-success"
                    : item.tone === "error"
                      ? "text-danger"
                      : "text-info",
                )}
              />
              <div className="min-w-0 flex-1">
                <p className="text-sm font-semibold text-slate-900">
                  {item.title}
                </p>
                {item.description ? (
                  <p className="mt-0.5 text-xs text-slate-600">
                    {item.description}
                  </p>
                ) : null}
              </div>
              <button
                aria-label="Dismiss notification"
                onClick={() => remove(item.id)}
                className="rounded p-1 text-slate-400 hover:bg-muted"
              >
                <X aria-hidden className="h-4 w-4" />
              </button>
            </div>
          );
        })}
      </div>
    </ToastContext.Provider>
  );
}

export function useToast() {
  const context = useContext(ToastContext);
  if (!context) throw new Error("useToast must be used inside ToastProvider");
  return context;
}
