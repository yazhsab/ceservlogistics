import {
  ArrowDownLeft,
  ArrowUpRight,
  CheckCircle2,
  Circle,
  Clock3,
  FileText,
  LockKeyhole,
  Scale,
  XCircle,
} from "lucide-react";
import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import { cn, formatMoney, titleCase, type MinorAmount } from "../lib/utils";
import { Badge } from "./ui";

export function Money({
  amountMinor,
  currency = "NGN",
  locale = "en-NG",
  className,
  showPositiveSign = false,
  fallback = "—",
}: {
  amountMinor?: MinorAmount | null;
  currency?: string;
  locale?: string;
  className?: string;
  showPositiveSign?: boolean;
  fallback?: string;
}) {
  if (amountMinor == null) return <span className={className}>{fallback}</span>;
  const numeric = BigInt(amountMinor);
  const formatted = formatMoney(amountMinor, currency, locale);
  return (
    <span
      className={cn("whitespace-nowrap font-medium tabular-nums", className)}
      data-amount-minor={amountMinor.toString()}
      data-currency={currency}
    >
      {showPositiveSign && numeric > 0n ? "+" : ""}
      {formatted}
    </span>
  );
}

export function DebitCreditAmount({
  amountMinor,
  direction,
  currency = "NGN",
  className,
}: {
  amountMinor?: MinorAmount | null;
  direction: "DEBIT" | "CREDIT";
  currency?: string;
  className?: string;
}) {
  const isDebit = direction === "DEBIT";
  const Icon = isDebit ? ArrowUpRight : ArrowDownLeft;
  return (
    <span
      className={cn("inline-flex items-center gap-2", className)}
      aria-label={`${titleCase(direction)} ${formatMoney(amountMinor, currency)}`}
    >
      <span
        aria-hidden
        className={cn(
          "inline-flex h-6 min-w-6 items-center justify-center rounded border px-1 text-[10px] font-bold",
          isDebit
            ? "border-sky-200 bg-sky-50 text-sky-800"
            : "border-violet-200 bg-violet-50 text-violet-800",
        )}
      >
        {isDebit ? "DR" : "CR"}
      </span>
      <Icon aria-hidden className="h-3.5 w-3.5 text-slate-500" />
      <Money amountMinor={amountMinor} currency={currency} />
    </span>
  );
}

const successfulFinancialStatuses = new Set([
  "APPROVED",
  "ISSUED",
  "PAID",
  "POSTED",
  "RECONCILED",
  "REMITTED",
  "SETTLED",
]);
const warningFinancialStatuses = new Set([
  "AWAITING_APPROVAL",
  "DISPUTED",
  "PARTIALLY_PAID",
  "PENDING",
  "SUBMITTED",
]);
const dangerousFinancialStatuses = new Set([
  "CANCELLED",
  "FAILED",
  "OVERDUE",
  "REJECTED",
  "REVERSED",
  "VOID",
]);

export function FinancialStatus({
  status,
  detail,
}: {
  status?: string | null;
  detail?: string;
}) {
  const normalized = (status || "UNKNOWN").toUpperCase();
  const tone = successfulFinancialStatuses.has(normalized)
    ? "success"
    : warningFinancialStatuses.has(normalized)
      ? "warning"
      : dangerousFinancialStatuses.has(normalized)
        ? "danger"
        : normalized === "DRAFT"
          ? "info"
          : "neutral";
  return (
    <span className="inline-flex flex-wrap items-center gap-2">
      <Badge tone={tone}>{titleCase(normalized)}</Badge>
      {detail ? <span className="text-xs text-slate-600">{detail}</span> : null}
    </span>
  );
}

export function ReferenceLink({
  to,
  type,
  reference,
  className,
}: {
  to: string;
  type: string;
  reference: string;
  className?: string;
}) {
  return (
    <Link
      to={to}
      className={cn(
        "inline-flex items-center gap-1.5 font-mono text-xs font-semibold text-primary underline-offset-2 hover:underline focus-visible:rounded-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary",
        className,
      )}
      aria-label={`View ${type} ${reference}`}
    >
      <FileText aria-hidden className="h-3.5 w-3.5" />
      {reference}
    </Link>
  );
}

export function ImmutableBadge({
  label = "Posted · Immutable",
  reason = "This financial record cannot be edited. Use an authorized reversal or adjustment workflow when correction is required.",
}: {
  label?: string;
  reason?: string;
}) {
  return (
    <span
      className="inline-flex items-center gap-1.5 rounded-md border border-slate-300 bg-slate-100 px-2.5 py-1 text-xs font-bold uppercase tracking-wide text-slate-800"
      title={reason}
      aria-label={`${label}. ${reason}`}
    >
      <LockKeyhole aria-hidden className="h-3.5 w-3.5" />
      {label}
    </span>
  );
}

export interface ApprovalStep {
  id: string;
  label: string;
  status: "COMPLETE" | "CURRENT" | "PENDING" | "REJECTED";
  actor?: string | null;
  occurredAt?: string | null;
  note?: string | null;
}

export function ApprovalTimeline({ steps }: { steps: ApprovalStep[] }) {
  return (
    <ol className="space-y-0" aria-label="Approval history">
      {steps.map((step, index) => {
        const Icon =
          step.status === "COMPLETE"
            ? CheckCircle2
            : step.status === "REJECTED"
              ? XCircle
              : step.status === "CURRENT"
                ? Clock3
                : Circle;
        return (
          <li key={step.id} className="relative flex gap-3 pb-5 last:pb-0">
            {index < steps.length - 1 ? (
              <span
                aria-hidden
                className="absolute left-[9px] top-5 h-[calc(100%-1rem)] w-px bg-border"
              />
            ) : null}
            <Icon
              aria-hidden
              className={cn(
                "relative z-10 mt-0.5 h-5 w-5 shrink-0 bg-surface",
                step.status === "COMPLETE" && "text-success",
                step.status === "CURRENT" && "text-info",
                step.status === "REJECTED" && "text-danger",
                step.status === "PENDING" && "text-slate-400",
              )}
            />
            <div className="min-w-0">
              <div className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
                <span className="text-sm font-semibold text-slate-900">
                  {step.label}
                </span>
                <span className="text-xs font-medium text-slate-500">
                  {titleCase(step.status)}
                </span>
              </div>
              {step.actor || step.occurredAt ? (
                <p className="mt-0.5 text-xs text-slate-600">
                  {[step.actor, step.occurredAt].filter(Boolean).join(" · ")}
                </p>
              ) : null}
              {step.note ? (
                <p className="mt-1 text-xs text-slate-500">{step.note}</p>
              ) : null}
            </div>
          </li>
        );
      })}
    </ol>
  );
}

export interface FinancialSummaryItem {
  label: string;
  amountMinor?: MinorAmount | null;
  description?: string;
  emphasis?: boolean;
  value?: ReactNode;
}

const settlementDirectionCopy = {
  HEAD_OFFICE_PAYS_FRANCHISE: {
    title: "Head Office pays Franchise",
    description: "This is an amount payable to the franchise.",
    icon: ArrowDownLeft,
  },
  FRANCHISE_PAYS_HEAD_OFFICE: {
    title: "Franchise pays Head Office",
    description: "This is an amount receivable from the franchise.",
    icon: ArrowUpRight,
  },
  NO_PAYMENT_DUE: {
    title: "No payment due",
    description: "The backend-calculated settlement is balanced.",
    icon: Scale,
  },
} as const;

export function FinancialSummary({
  direction,
  totalMinor,
  currency = "NGN",
  items,
  title = "Financial summary",
}: {
  direction?: keyof typeof settlementDirectionCopy;
  totalMinor?: MinorAmount | null;
  currency?: string;
  items: FinancialSummaryItem[];
  title?: string;
}) {
  const directionContent = direction
    ? settlementDirectionCopy[direction]
    : undefined;
  const DirectionIcon = directionContent?.icon;
  return (
    <section
      className="overflow-hidden rounded-lg border border-border bg-surface"
      aria-label={title}
    >
      {directionContent && DirectionIcon ? (
        <div className="flex flex-wrap items-center justify-between gap-4 border-b border-border bg-slate-950 px-4 py-4 text-white">
          <div className="flex items-start gap-3">
            <span className="rounded-md bg-white/10 p-2">
              <DirectionIcon aria-hidden className="h-5 w-5" />
            </span>
            <div>
              <h2 className="text-base font-bold">{directionContent.title}</h2>
              <p className="mt-0.5 text-xs text-slate-300">
                {directionContent.description}
              </p>
            </div>
          </div>
          <Money
            amountMinor={totalMinor}
            currency={currency}
            className="text-xl font-bold text-white"
          />
        </div>
      ) : (
        <div className="border-b border-border px-4 py-3">
          <h2 className="text-sm font-semibold text-slate-900">{title}</h2>
        </div>
      )}
      <dl className="divide-y divide-border">
        {items.map((item) => (
          <div
            key={item.label}
            className={cn(
              "grid gap-1 px-4 py-3 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center",
              item.emphasis && "bg-slate-50",
            )}
          >
            <dt>
              <span className="block text-sm font-medium text-slate-800">
                {item.label}
              </span>
              {item.description ? (
                <span className="mt-0.5 block text-xs text-slate-500">
                  {item.description}
                </span>
              ) : null}
            </dt>
            <dd className="text-sm font-semibold text-slate-950 sm:text-right">
              {item.value ?? (
                <Money amountMinor={item.amountMinor} currency={currency} />
              )}
            </dd>
          </div>
        ))}
      </dl>
      <p className="border-t border-border bg-slate-50 px-4 py-2 text-xs text-slate-500">
        Amounts shown are supplied by the backend; this summary does not
        calculate authoritative totals in the browser.
      </p>
    </section>
  );
}
