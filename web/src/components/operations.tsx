import {
  AlertTriangle,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  CircleDot,
  RotateCw,
  ScanLine,
  XCircle,
  type LucideIcon,
} from "lucide-react";
import {
  forwardRef,
  useEffect,
  useImperativeHandle,
  useRef,
  type FormEvent,
  type ReactNode,
} from "react";
import { Link } from "react-router-dom";
import type { ScanResult } from "../api/client";
import { cn, formatDateTime, titleCase } from "../lib/utils";
import { Badge, Button, Input } from "./ui";

export function OperationalMetricStrip({
  items,
  label = "Operational summary",
}: {
  items: Array<{
    label: string;
    value?: ReactNode;
    tone?: "neutral" | "success" | "warning" | "danger" | "info";
    icon?: LucideIcon;
  }>;
  label?: string;
}) {
  const xlColumns = [
    "",
    "xl:grid-cols-1",
    "xl:grid-cols-2",
    "xl:grid-cols-3",
    "xl:grid-cols-4",
    "xl:grid-cols-5",
    "xl:grid-cols-6",
    "xl:grid-cols-7",
    "xl:grid-cols-8",
    "xl:grid-cols-9",
    "xl:grid-cols-5",
  ][Math.min(items.length, 10)];
  return (
    <section
      aria-label={label}
      className={cn(
        "mb-4 grid overflow-hidden rounded-lg border border-border bg-surface shadow-sm sm:grid-cols-2",
        "lg:grid-cols-4",
        xlColumns,
      )}
    >
      {items.map((item) => {
        const Icon = item.icon;
        return (
          <div
            key={item.label}
            className={cn(
              "flex min-h-20 items-center gap-3 border-b border-border px-4 py-3 last:border-b-0 sm:border-r",
              items.length <= 4
                ? "lg:border-b-0"
                : items.length < 10
                  ? "xl:border-b-0"
                  : undefined,
            )}
          >
            {Icon ? (
              <span
                className={cn(
                  "grid h-9 w-9 shrink-0 place-items-center rounded-md bg-slate-100 text-slate-600",
                  item.tone === "success" && "bg-emerald-50 text-emerald-700",
                  item.tone === "warning" && "bg-amber-50 text-amber-700",
                  item.tone === "danger" && "bg-red-50 text-red-700",
                  item.tone === "info" && "bg-sky-50 text-sky-700",
                )}
              >
                <Icon aria-hidden className="h-4 w-4" />
              </span>
            ) : null}
            <span className="min-w-0">
              <strong className="block text-xl font-bold tabular-nums text-slate-950">
                {item.value ?? "—"}
              </strong>
              <span className="block truncate text-xs font-medium text-slate-500">
                {item.label}
              </span>
            </span>
          </div>
        );
      })}
    </section>
  );
}

export function CursorPager({
  page,
  hasMore,
  nextCursor,
  onPrevious,
  onNext,
  noun = "records",
  count,
}: {
  page: number;
  hasMore?: boolean;
  nextCursor?: string | null;
  onPrevious: () => void;
  onNext: (cursor: string) => void;
  noun?: string;
  count: number;
}) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border px-4 py-3">
      <p className="text-xs text-slate-500">
        Cursor page {page} · {count} {noun}
      </p>
      <div className="flex gap-1">
        <Button size="sm" disabled={page <= 1} onClick={onPrevious}>
          <ChevronLeft aria-hidden className="h-4 w-4" /> Previous
        </Button>
        <Button
          size="sm"
          disabled={!hasMore || !nextCursor}
          onClick={() => nextCursor && onNext(nextCursor)}
        >
          Next <ChevronRight aria-hidden className="h-4 w-4" />
        </Button>
      </div>
    </div>
  );
}

export interface ScannerInputHandle {
  focus: () => void;
}

export const ScannerInput = forwardRef<
  ScannerInputHandle,
  {
    value: string;
    onChange: (value: string) => void;
    onScan: (value: string) => void;
    busy?: boolean;
    label?: string;
    placeholder?: string;
    hint?: ReactNode;
    autoFocus?: boolean;
  }
>(
  (
    {
      value,
      onChange,
      onScan,
      busy,
      label = "Scan barcode",
      placeholder = "Scan AWB or piece barcode",
      hint,
      autoFocus = true,
    },
    forwardedRef,
  ) => {
    const inputRef = useRef<HTMLInputElement>(null);
    useImperativeHandle(forwardedRef, () => ({
      focus: () => inputRef.current?.focus(),
    }));
    useEffect(() => {
      if (autoFocus) inputRef.current?.focus();
    }, [autoFocus]);
    const submit = (event: FormEvent) => {
      event.preventDefault();
      const normalized = value.trim().toUpperCase();
      if (!normalized || busy) return;
      onScan(normalized);
    };
    return (
      <form onSubmit={submit} className="space-y-2">
        <label
          htmlFor="operations-scanner-input"
          className="flex items-center gap-2 text-sm font-semibold text-slate-800"
        >
          <ScanLine aria-hidden className="h-4 w-4 text-primary" /> {label}
        </label>
        <div className="flex gap-2">
          <Input
            ref={inputRef}
            id="operations-scanner-input"
            value={value}
            onChange={(event) => onChange(event.target.value.toUpperCase())}
            placeholder={placeholder}
            autoComplete="off"
            spellCheck={false}
            enterKeyHint="send"
            className="h-14 flex-1 border-2 font-mono text-lg font-semibold tracking-wide focus:border-primary"
            disabled={busy}
          />
          <Button
            type="submit"
            variant="primary"
            size="lg"
            disabled={busy || !value.trim()}
          >
            {busy ? (
              <RotateCw aria-hidden className="h-4 w-4 animate-spin" />
            ) : (
              <ScanLine aria-hidden className="h-4 w-4" />
            )}
            {busy ? "Checking" : "Submit"}
          </Button>
        </div>
        <p className="text-xs text-slate-500">
          {hint ?? "Scanner input stays focused. Press Enter to submit."}
        </p>
      </form>
    );
  },
);
ScannerInput.displayName = "ScannerInput";

export function ScanFeedback({ result }: { result?: ScanResult }) {
  if (!result) {
    return (
      <div className="flex min-h-24 items-center gap-3 rounded-md border border-dashed border-slate-300 bg-slate-50 p-4 text-sm text-slate-500">
        <CircleDot aria-hidden className="h-5 w-5" /> Awaiting the next barcode.
      </div>
    );
  }
  const accepted = result.outcome === "ACCEPTED";
  const duplicate = result.outcome === "DUPLICATE";
  const Icon = accepted ? CheckCircle2 : duplicate ? AlertTriangle : XCircle;
  return (
    <div
      role="status"
      aria-live="assertive"
      className={cn(
        "min-h-24 rounded-md border p-4",
        accepted && "border-emerald-300 bg-emerald-50 text-emerald-950",
        duplicate && "border-amber-300 bg-amber-50 text-amber-950",
        !accepted && !duplicate && "border-red-300 bg-red-50 text-red-950",
      )}
    >
      <div className="flex items-start gap-3">
        <Icon aria-hidden className="mt-0.5 h-6 w-6 shrink-0" />
        <div>
          <strong className="block text-base">
            {titleCase(result.outcome ?? "Rejected")}:{" "}
            {result.awb ?? result.barcode}
          </strong>
          <p className="mt-1 text-sm">
            {result.rejectionMessage ||
              result.nextAction ||
              `${titleCase(result.fromStatus ?? "Unknown")} → ${titleCase(result.toStatus ?? "No change")}`}
          </p>
          {result.rejectionCode ? (
            <Badge tone="danger" className="mt-2">
              {titleCase(result.rejectionCode)}
            </Badge>
          ) : null}
        </div>
      </div>
    </div>
  );
}

export function JourneyTimeline({
  items,
  compact = false,
}: {
  items: Array<{
    key: string;
    title: string;
    description?: string;
    occurredAt?: string;
    status?: string;
    current?: boolean;
  }>;
  compact?: boolean;
}) {
  return (
    <ol
      className="relative ml-2 border-l border-slate-200"
      aria-label="Journey timeline"
    >
      {items.map((item, index) => (
        <li
          key={item.key}
          className={cn(
            "relative ml-5",
            compact ? "pb-4" : "pb-6",
            index === items.length - 1 && "pb-0",
          )}
        >
          <span
            className={cn(
              "absolute -left-[27px] top-1 grid h-3.5 w-3.5 place-items-center rounded-full border-2 border-white bg-slate-300 ring-1 ring-slate-300",
              item.occurredAt && "bg-primary ring-primary",
              item.current &&
                "h-4 w-4 -left-[29px] bg-[#d8f25a] ring-2 ring-primary",
            )}
          />
          <div className="flex flex-wrap items-start justify-between gap-2">
            <div>
              <strong className="block text-sm text-slate-900">
                {item.title}
              </strong>
              {item.description ? (
                <p className="mt-0.5 text-xs text-slate-500">
                  {item.description}
                </p>
              ) : null}
            </div>
            <div className="text-right">
              {item.status ? (
                <Badge tone={item.current ? "primary" : "neutral"}>
                  {titleCase(item.status)}
                </Badge>
              ) : null}
              {item.occurredAt ? (
                <time className="mt-1 block text-xs text-slate-500">
                  {formatDateTime(item.occurredAt)}
                </time>
              ) : null}
            </div>
          </div>
        </li>
      ))}
    </ol>
  );
}

export function EntityLink({
  to,
  primary,
  secondary,
}: {
  to?: string;
  primary?: string | null;
  secondary?: string | null;
}) {
  const content = (
    <>
      <span className="block font-mono text-sm font-semibold text-primary">
        {primary || "—"}
      </span>
      {secondary ? (
        <span className="mt-0.5 block text-xs text-slate-500">{secondary}</span>
      ) : null}
    </>
  );
  return to ? (
    <Link to={to} className="hover:underline">
      {content}
    </Link>
  ) : (
    <span>{content}</span>
  );
}

export function MobileActionBar({ children }: { children: ReactNode }) {
  return (
    <>
      <div aria-hidden className="h-20 sm:hidden" />
      <div className="fixed inset-x-0 bottom-0 z-30 flex gap-2 border-t border-border bg-white/95 px-4 py-3 pb-[calc(0.75rem+env(safe-area-inset-bottom))] shadow-[0_-8px_24px_rgba(15,23,42,0.08)] backdrop-blur sm:sticky sm:mx-0 sm:mt-5 sm:rounded-lg sm:border">
        {children}
      </div>
    </>
  );
}
