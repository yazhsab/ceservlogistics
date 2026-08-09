import { Check, Clipboard, KeyRound, ShieldAlert } from "lucide-react";
import { useState } from "react";
import type { ReactNode } from "react";
import { titleCase } from "../lib/utils";
import { Badge, Button, Dialog, InlineNotice, Textarea } from "./ui";

export function ScopeList({ scopes }: { scopes?: string[] | null }) {
  if (!scopes?.length)
    return <span className="text-xs text-slate-500">No access scopes</span>;
  return (
    <span className="flex max-w-xl flex-wrap gap-1">
      {scopes.map((scope) => (
        <Badge key={scope} tone="neutral" className="font-mono">
          {scope}
        </Badge>
      ))}
    </span>
  );
}

export function IntegrationStatus({ status }: { status?: string | null }) {
  const normalized = status ?? "UNKNOWN";
  const tone = ["ACTIVE", "DELIVERED"].includes(normalized)
    ? "success"
    : ["FAILED", "DEAD_LETTER", "REVOKED", "DISABLED"].includes(normalized)
      ? "danger"
      : ["PAUSED", "SUSPENDED", "EXPIRED", "PENDING", "SENDING"].includes(
            normalized,
          )
        ? "warning"
        : "neutral";
  return <Badge tone={tone}>{titleCase(normalized)}</Badge>;
}

export function HttpStatus({ code }: { code?: number | null }) {
  if (!code) return <span className="text-slate-500">No response</span>;
  const tone = code >= 200 && code < 300 ? "success" : "danger";
  return <Badge tone={tone}>HTTP {code}</Badge>;
}

export function OneTimeSecretDialog({
  open,
  onOpenChange,
  title,
  secret,
  warning,
  label = "Secret",
  children,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  secret?: string | null;
  warning?: string | null;
  label?: string;
  children?: ReactNode;
}) {
  const [copyState, setCopyState] = useState<"idle" | "copied" | "failed">(
    "idle",
  );
  const updateOpen = (next: boolean) => {
    if (!next) setCopyState("idle");
    onOpenChange(next);
  };
  const copy = async () => {
    if (!secret) return;
    try {
      await navigator.clipboard.writeText(secret);
      setCopyState("copied");
    } catch {
      setCopyState("failed");
    }
  };
  return (
    <Dialog
      open={open}
      onOpenChange={updateOpen}
      title={title}
      description="Copy this now; it will not be shown again."
      footer={
        <Button variant="primary" onClick={() => updateOpen(false)}>
          I have stored it securely
        </Button>
      }
    >
      <InlineNotice tone="warning" title="One-time secret">
        {warning ??
          "Closing this window permanently removes the plaintext value from this browser view."}
      </InlineNotice>
      <div className="mt-4 space-y-2">
        <div className="flex items-center justify-between gap-3">
          <span className="flex items-center gap-2 text-sm font-semibold text-slate-800">
            <KeyRound aria-hidden className="h-4 w-4" /> {label}
          </span>
          <Button size="sm" onClick={() => void copy()} disabled={!secret}>
            {copyState === "copied" ? (
              <Check aria-hidden className="h-4 w-4" />
            ) : (
              <Clipboard aria-hidden className="h-4 w-4" />
            )}
            {copyState === "copied" ? "Copied" : "Copy"}
          </Button>
        </div>
        <Textarea
          readOnly
          value={secret ?? ""}
          aria-label={label}
          className="min-h-24 break-all font-mono text-xs"
        />
        {copyState === "failed" ? (
          <p role="alert" className="flex gap-2 text-xs text-danger">
            <ShieldAlert aria-hidden className="h-4 w-4 shrink-0" /> Copy was
            blocked by the browser. Select the value and copy it manually.
          </p>
        ) : null}
      </div>
      {children ? <div className="mt-4">{children}</div> : null}
    </Dialog>
  );
}
