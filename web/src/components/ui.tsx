import * as CheckboxPrimitive from "@radix-ui/react-checkbox";
import * as DialogPrimitive from "@radix-ui/react-dialog";
import * as RadioGroupPrimitive from "@radix-ui/react-radio-group";
import * as SwitchPrimitive from "@radix-ui/react-switch";
import * as TabsPrimitive from "@radix-ui/react-tabs";
import * as TooltipPrimitive from "@radix-ui/react-tooltip";
import { cva, type VariantProps } from "class-variance-authority";
import {
  AlertCircle,
  CheckCircle2,
  Check,
  ChevronLeft,
  ChevronRight,
  LoaderCircle,
  Search,
  X,
  type LucideIcon,
} from "lucide-react";
import {
  createContext,
  forwardRef,
  useContext,
  type AriaAttributes,
  type ButtonHTMLAttributes,
  type HTMLAttributes,
  type InputHTMLAttributes,
  type ReactNode,
  type SelectHTMLAttributes,
  type TextareaHTMLAttributes,
} from "react";
import { cn, titleCase } from "../lib/utils";

type FieldAccessibility = Pick<
  AriaAttributes,
  "aria-describedby" | "aria-invalid" | "aria-required"
>;
const FieldContext = createContext<FieldAccessibility>({});

function describedBy(explicit?: string, inherited?: string) {
  return [explicit, inherited].filter(Boolean).join(" ") || undefined;
}

const buttonVariants = cva(
  "inline-flex min-h-10 items-center justify-center gap-2 rounded-md border px-3.5 text-sm font-semibold transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50",
  {
    variants: {
      variant: {
        primary: "border-primary bg-primary text-white hover:bg-primary/90",
        secondary: "border-border bg-surface text-foreground hover:bg-muted",
        ghost:
          "border-transparent bg-transparent text-foreground hover:bg-muted",
        danger: "border-danger bg-danger text-white hover:bg-danger/90",
      },
      size: {
        sm: "min-h-8 px-2.5 text-xs",
        md: "min-h-10",
        lg: "min-h-11 px-4",
        icon: "h-10 w-10 px-0",
      },
    },
    defaultVariants: { variant: "secondary", size: "md" },
  },
);

export interface ButtonProps
  extends
    ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  loading?: boolean;
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(
  (
    { className, variant, size, loading, children, disabled, ...props },
    ref,
  ) => (
    <button
      ref={ref}
      className={cn(buttonVariants({ variant, size }), className)}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      {...props}
    >
      {loading ? (
        <LoaderCircle aria-hidden className="h-4 w-4 animate-spin" />
      ) : null}
      {children}
    </button>
  ),
);
Button.displayName = "Button";

export const Input = forwardRef<
  HTMLInputElement,
  InputHTMLAttributes<HTMLInputElement>
>(
  (
    {
      className,
      "aria-describedby": ariaDescribedBy,
      "aria-invalid": ariaInvalid,
      "aria-required": ariaRequired,
      ...props
    },
    ref,
  ) => {
    const field = useContext(FieldContext);
    return (
      <input
        ref={ref}
        className={cn(
          "min-h-10 w-full rounded-md border border-border bg-surface px-3 text-sm text-foreground shadow-sm outline-none placeholder:text-slate-400 focus:border-primary focus:ring-2 focus:ring-primary/15 disabled:bg-muted disabled:text-slate-500",
          className,
        )}
        aria-describedby={describedBy(
          ariaDescribedBy,
          field["aria-describedby"],
        )}
        aria-invalid={ariaInvalid ?? field["aria-invalid"]}
        aria-required={ariaRequired ?? field["aria-required"]}
        {...props}
      />
    );
  },
);
Input.displayName = "Input";

export const Select = forwardRef<
  HTMLSelectElement,
  SelectHTMLAttributes<HTMLSelectElement>
>(
  (
    {
      className,
      children,
      "aria-describedby": ariaDescribedBy,
      "aria-invalid": ariaInvalid,
      "aria-required": ariaRequired,
      ...props
    },
    ref,
  ) => {
    const field = useContext(FieldContext);
    return (
      <select
        ref={ref}
        className={cn(
          "min-h-10 w-full rounded-md border border-border bg-surface px-3 text-sm text-foreground shadow-sm outline-none focus:border-primary focus:ring-2 focus:ring-primary/15 disabled:bg-muted",
          className,
        )}
        aria-describedby={describedBy(
          ariaDescribedBy,
          field["aria-describedby"],
        )}
        aria-invalid={ariaInvalid ?? field["aria-invalid"]}
        aria-required={ariaRequired ?? field["aria-required"]}
        {...props}
      >
        {children}
      </select>
    );
  },
);
Select.displayName = "Select";

export const Textarea = forwardRef<
  HTMLTextAreaElement,
  TextareaHTMLAttributes<HTMLTextAreaElement>
>(
  (
    {
      className,
      "aria-describedby": ariaDescribedBy,
      "aria-invalid": ariaInvalid,
      "aria-required": ariaRequired,
      ...props
    },
    ref,
  ) => {
    const field = useContext(FieldContext);
    return (
      <textarea
        ref={ref}
        className={cn(
          "min-h-24 w-full resize-y rounded-md border border-border bg-surface px-3 py-2 text-sm text-foreground shadow-sm outline-none placeholder:text-slate-400 focus:border-primary focus:ring-2 focus:ring-primary/15",
          className,
        )}
        aria-describedby={describedBy(
          ariaDescribedBy,
          field["aria-describedby"],
        )}
        aria-invalid={ariaInvalid ?? field["aria-invalid"]}
        aria-required={ariaRequired ?? field["aria-required"]}
        {...props}
      />
    );
  },
);
Textarea.displayName = "Textarea";

export const DatePicker = forwardRef<
  HTMLInputElement,
  Omit<InputHTMLAttributes<HTMLInputElement>, "type">
>((props, ref) => <Input ref={ref} type="date" {...props} />);
DatePicker.displayName = "DatePicker";

export function Combobox({
  id,
  options,
  ...props
}: InputHTMLAttributes<HTMLInputElement> & {
  id: string;
  options: Array<{ value: string; label: string }>;
}) {
  const listId = `${id}-options`;
  return (
    <>
      <Input id={id} list={listId} role="combobox" {...props} />
      <datalist id={listId}>
        {options.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </datalist>
    </>
  );
}

export function Checkbox({
  checked,
  onCheckedChange,
  disabled,
  "aria-label": ariaLabel,
}: {
  checked?: boolean;
  onCheckedChange?: (checked: boolean) => void;
  disabled?: boolean;
  "aria-label": string;
}) {
  return (
    <CheckboxPrimitive.Root
      checked={checked}
      disabled={disabled}
      onCheckedChange={(value) => onCheckedChange?.(value === true)}
      aria-label={ariaLabel}
      className="grid h-5 w-5 shrink-0 place-items-center rounded border border-border bg-white text-white outline-none data-[state=checked]:border-primary data-[state=checked]:bg-primary focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-60"
    >
      <CheckboxPrimitive.Indicator>
        <Check aria-hidden className="h-3.5 w-3.5" />
      </CheckboxPrimitive.Indicator>
    </CheckboxPrimitive.Root>
  );
}

export function RadioGroup({
  value,
  onValueChange,
  options,
  label,
}: {
  value?: string;
  onValueChange?: (value: string) => void;
  options: Array<{ value: string; label: string; description?: string }>;
  label: string;
}) {
  return (
    <RadioGroupPrimitive.Root
      value={value}
      onValueChange={onValueChange}
      aria-label={label}
      className="grid gap-2"
    >
      {options.map((option) => (
        <label
          key={option.value}
          className="flex cursor-pointer items-start gap-3 rounded-md border border-border p-3 hover:bg-muted"
        >
          <RadioGroupPrimitive.Item
            value={option.value}
            className="mt-0.5 grid h-5 w-5 place-items-center rounded-full border border-border bg-white outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2 data-[state=checked]:border-primary"
          >
            <RadioGroupPrimitive.Indicator className="h-2.5 w-2.5 rounded-full bg-primary" />
          </RadioGroupPrimitive.Item>
          <span>
            <span className="block text-sm font-medium">{option.label}</span>
            {option.description ? (
              <span className="mt-0.5 block text-xs text-slate-500">
                {option.description}
              </span>
            ) : null}
          </span>
        </label>
      ))}
    </RadioGroupPrimitive.Root>
  );
}

export function Switch({
  checked,
  onCheckedChange,
  label,
}: {
  checked?: boolean;
  onCheckedChange?: (checked: boolean) => void;
  label: string;
}) {
  return (
    <SwitchPrimitive.Root
      checked={checked}
      onCheckedChange={onCheckedChange}
      aria-label={label}
      className="relative h-6 w-11 rounded-full bg-slate-300 outline-none transition data-[state=checked]:bg-primary focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2"
    >
      <SwitchPrimitive.Thumb className="block h-5 w-5 translate-x-0.5 rounded-full bg-white shadow transition-transform data-[state=checked]:translate-x-5" />
    </SwitchPrimitive.Root>
  );
}

export function Tabs({
  value,
  onValueChange,
  items,
  children,
  label,
}: {
  value: string;
  onValueChange: (value: string) => void;
  items: Array<{ value: string; label: string }>;
  children: ReactNode;
  label: string;
}) {
  return (
    <TabsPrimitive.Root value={value} onValueChange={onValueChange}>
      <TabsPrimitive.List
        aria-label={label}
        className="flex gap-1 overflow-x-auto border-b border-border"
      >
        {items.map((item) => (
          <TabsPrimitive.Trigger
            key={item.value}
            value={item.value}
            className="whitespace-nowrap border-b-2 border-transparent px-3 py-2.5 text-sm font-semibold text-slate-500 outline-none data-[state=active]:border-primary data-[state=active]:text-primary focus-visible:ring-2 focus-visible:ring-primary"
          >
            {item.label}
          </TabsPrimitive.Trigger>
        ))}
      </TabsPrimitive.List>
      {children}
    </TabsPrimitive.Root>
  );
}

export const TabContent = TabsPrimitive.Content;

export function Field({
  label,
  htmlFor,
  required,
  error,
  hint,
  children,
  className,
}: {
  label: string;
  htmlFor: string;
  required?: boolean;
  error?: string;
  hint?: string;
  children: ReactNode;
  className?: string;
}) {
  const messageId = `${htmlFor}-message`;
  return (
    <div className={cn("space-y-1.5", className)}>
      <label
        htmlFor={htmlFor}
        className="block text-sm font-medium text-slate-700"
      >
        {label}{" "}
        {required ? (
          <span className="text-danger" aria-hidden>
            *
          </span>
        ) : null}
      </label>
      <FieldContext.Provider
        value={{
          "aria-describedby": error || hint ? messageId : undefined,
          "aria-invalid": error ? true : undefined,
          "aria-required": required ? true : undefined,
        }}
      >
        {children}
      </FieldContext.Provider>
      {error ? (
        <p
          id={messageId}
          role="alert"
          className="flex items-start gap-1.5 text-xs text-danger"
        >
          <AlertCircle aria-hidden className="mt-0.5 h-3.5 w-3.5 shrink-0" />{" "}
          {error}
        </p>
      ) : hint ? (
        <p id={messageId} className="text-xs text-slate-500">
          {hint}
        </p>
      ) : null}
    </div>
  );
}

export function SearchInput({
  className,
  ...props
}: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <div className={cn("relative", className)}>
      <Search
        aria-hidden
        className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400"
      />
      <Input type="search" className="pl-9" {...props} />
    </div>
  );
}

export function Badge({
  children,
  tone = "neutral",
  className,
}: {
  children: ReactNode;
  tone?: "neutral" | "success" | "warning" | "danger" | "info" | "primary";
  className?: string;
}) {
  const tones = {
    neutral: "border-slate-200 bg-slate-50 text-slate-700",
    success: "border-emerald-200 bg-emerald-50 text-emerald-800",
    warning: "border-amber-200 bg-amber-50 text-amber-800",
    danger: "border-red-200 bg-red-50 text-red-800",
    info: "border-sky-200 bg-sky-50 text-sky-800",
    primary: "border-emerald-200 bg-emerald-50 text-emerald-900",
  } as const;
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-xs font-semibold",
        tones[tone],
        className,
      )}
    >
      {children}
    </span>
  );
}

export function StatusBadge({ status }: { status?: string | null }) {
  const key = status ?? "UNKNOWN";
  const tone = [
    "ACTIVE",
    "DELIVERED",
    "COMPLETED",
    "GOOD",
    "ACCEPTED",
    "MATCHED",
    "RECEIVED",
    "RECONCILED",
    "RETURNED",
    "RTO_DELIVERED",
  ].includes(key)
    ? "success"
    : [
          "INACTIVE",
          "CANCELLED",
          "CLOSED",
          "FAILED",
          "LOCKED",
          "TERMINATED",
          "BLOCKED",
          "REJECTED",
          "LOST",
          "DELIVERY_FAILED",
          "RETURN_FAILED",
        ].includes(key)
      ? "danger"
      : [
            "SUSPENDED",
            "WARNING",
            "ON_HOLD",
            "COMPLETED_WITH_ERRORS",
            "NDR",
            "DAMAGED",
            "MISSING",
            "EXCESS",
            "PARTIALLY_COMPLETED",
            "COMPLETED_WITH_EXCEPTIONS",
            "ESCALATED",
            "RETURNING",
          ].includes(key)
        ? "warning"
        : [
              "BOOKED",
              "PROCESSING",
              "IN_TRANSIT",
              "OUT_FOR_DELIVERY",
              "ACTIVE",
              "REQUESTED",
              "SCHEDULED",
              "ASSIGNED",
              "ACCEPTED",
              "ARRIVED",
              "OPEN",
              "DRAFT",
              "PLANNED",
              "LOADING",
              "DEPARTED",
              "RECEIVED",
              "OPENED",
              "IN_PROGRESS",
              "INITIATED",
              "AT_ORIGIN_BRANCH",
              "OUT_FOR_RETURN",
            ].includes(key)
          ? "info"
          : "neutral";
  return <Badge tone={tone}>{titleCase(key)}</Badge>;
}

export function PageHeader({
  title,
  description,
  eyebrow,
  actions,
}: {
  title: string;
  description?: string;
  eyebrow?: string;
  actions?: ReactNode;
}) {
  return (
    <header className="mb-5 flex flex-wrap items-start justify-between gap-4">
      <div className="min-w-0">
        {eyebrow ? (
          <p className="mb-1 text-xs font-semibold uppercase tracking-wider text-primary">
            {eyebrow}
          </p>
        ) : null}
        <h1 className="text-2xl font-bold tracking-tight text-slate-950">
          {title}
        </h1>
        {description ? (
          <p className="mt-1 max-w-3xl text-sm text-slate-600">{description}</p>
        ) : null}
      </div>
      {actions ? (
        <div className="flex flex-wrap items-center gap-2">{actions}</div>
      ) : null}
    </header>
  );
}

export function Panel({
  className,
  children,
  ...props
}: HTMLAttributes<HTMLDivElement>) {
  return (
    <section
      className={cn(
        "min-w-0 max-w-full rounded-lg border border-border bg-surface shadow-panel",
        className,
      )}
      {...props}
    >
      {children}
    </section>
  );
}

export function PanelHeader({
  title,
  description,
  actions,
}: {
  title: string;
  description?: string;
  actions?: ReactNode;
}) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-4 py-3">
      <div>
        <h2 className="text-sm font-semibold text-slate-900">{title}</h2>
        {description ? (
          <p className="mt-0.5 text-xs text-slate-500">{description}</p>
        ) : null}
      </div>
      {actions}
    </div>
  );
}

export function LoadingState({ label = "Loading" }: { label?: string }) {
  return (
    <div
      className="flex min-h-52 items-center justify-center gap-2 text-sm text-slate-600"
      role="status"
      aria-live="polite"
    >
      <LoaderCircle aria-hidden className="h-5 w-5 animate-spin text-primary" />{" "}
      {label}…
    </div>
  );
}

export function EmptyState({
  icon: Icon,
  title,
  description,
  action,
}: {
  icon?: LucideIcon;
  title: string;
  description: string;
  action?: ReactNode;
}) {
  return (
    <div className="flex min-h-56 flex-col items-center justify-center px-6 py-10 text-center">
      {Icon ? (
        <div className="mb-3 rounded-full bg-muted p-3">
          <Icon aria-hidden className="h-5 w-5 text-slate-500" />
        </div>
      ) : null}
      <h2 className="text-sm font-semibold text-slate-900">{title}</h2>
      <p className="mt-1 max-w-md text-sm text-slate-500">{description}</p>
      {action ? <div className="mt-4">{action}</div> : null}
    </div>
  );
}

export function ErrorState({
  error,
  retry,
  title = "Unable to load this information",
}: {
  error: unknown;
  retry?: () => void;
  title?: string;
}) {
  const message =
    error instanceof Error
      ? error.message
      : "The request could not be completed.";
  const requestId =
    typeof error === "object" && error && "requestId" in error
      ? String(error.requestId ?? "")
      : "";
  return (
    <div
      className="flex min-h-56 flex-col items-center justify-center px-6 py-10 text-center"
      role="alert"
    >
      <div className="mb-3 rounded-full bg-red-50 p-3">
        <AlertCircle aria-hidden className="h-5 w-5 text-danger" />
      </div>
      <h2 className="text-sm font-semibold text-slate-900">{title}</h2>
      <p className="mt-1 max-w-lg text-sm text-slate-600">{message}</p>
      {requestId ? (
        <p className="mt-2 font-mono text-xs text-slate-500">
          Request {requestId}
        </p>
      ) : null}
      {retry ? (
        <Button className="mt-4" onClick={retry}>
          Try again
        </Button>
      ) : null}
    </div>
  );
}

export function PermissionDenied() {
  return (
    <ErrorState
      title="Access denied"
      error={
        new Error(
          "You do not have permission to view this area. Ask an administrator if you need access.",
        )
      }
    />
  );
}

export function Pagination({
  page,
  totalPages,
  onPageChange,
  label,
}: {
  page: number;
  totalPages: number;
  onPageChange: (page: number) => void;
  label?: string;
}) {
  return (
    <nav
      className="flex items-center justify-between border-t border-border px-4 py-3"
      aria-label="Pagination"
    >
      <p className="text-xs text-slate-500">
        {label ?? `Page ${page} of ${Math.max(totalPages, 1)}`}
      </p>
      <div className="flex gap-1">
        <Button
          size="sm"
          aria-label="Previous page"
          disabled={page <= 1}
          onClick={() => onPageChange(page - 1)}
        >
          <ChevronLeft aria-hidden className="h-4 w-4" /> Previous
        </Button>
        <Button
          size="sm"
          aria-label="Next page"
          disabled={page >= totalPages}
          onClick={() => onPageChange(page + 1)}
        >
          Next <ChevronRight aria-hidden className="h-4 w-4" />
        </Button>
      </div>
    </nav>
  );
}

export function Dialog({
  open,
  onOpenChange,
  title,
  description,
  children,
  footer,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description?: string;
  children: ReactNode;
  footer?: ReactNode;
}) {
  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-40 bg-slate-950/45 data-[state=open]:animate-in" />
        <DialogPrimitive.Content className="fixed left-1/2 top-1/2 z-50 flex max-h-[calc(100dvh-2rem)] w-[min(620px,calc(100%-2rem))] -translate-x-1/2 -translate-y-1/2 flex-col overflow-hidden rounded-lg border border-border bg-surface shadow-overlay focus:outline-none">
          <div className="shrink-0 border-b border-border px-5 py-4">
            <DialogPrimitive.Title className="text-base font-semibold text-slate-950">
              {title}
            </DialogPrimitive.Title>
            {description ? (
              <DialogPrimitive.Description className="mt-1 text-sm text-slate-600">
                {description}
              </DialogPrimitive.Description>
            ) : null}
            <DialogPrimitive.Close
              className="absolute right-4 top-4 rounded-md p-1 text-slate-500 hover:bg-muted focus:outline-none focus:ring-2 focus:ring-primary"
              aria-label="Close"
            >
              <X aria-hidden className="h-4 w-4" />
            </DialogPrimitive.Close>
          </div>
          <div className="min-h-0 overflow-y-auto px-5 py-4">{children}</div>
          {footer ? (
            <div className="flex shrink-0 flex-wrap justify-end gap-2 border-t border-border bg-slate-50 px-5 py-3">
              {footer}
            </div>
          ) : null}
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}

export function Sheet({
  open,
  onOpenChange,
  title,
  description,
  children,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description?: string;
  children: ReactNode;
}) {
  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-slate-950/45" />
        <DialogPrimitive.Content className="fixed inset-y-0 right-0 z-50 w-[min(520px,calc(100%-1.5rem))] overflow-y-auto overscroll-contain border-l border-border bg-surface shadow-overlay focus:outline-none">
          <div className="sticky top-0 z-10 border-b border-border bg-white px-5 py-4">
            <DialogPrimitive.Title className="text-base font-semibold">
              {title}
            </DialogPrimitive.Title>
            {description ? (
              <DialogPrimitive.Description className="mt-1 text-sm text-slate-600">
                {description}
              </DialogPrimitive.Description>
            ) : null}
            <DialogPrimitive.Close
              className="absolute right-4 top-4 rounded-md p-1 text-slate-500 hover:bg-muted focus-visible:ring-2 focus-visible:ring-primary"
              aria-label="Close"
            >
              <X aria-hidden className="h-4 w-4" />
            </DialogPrimitive.Close>
          </div>
          <div className="p-5">{children}</div>
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}

export const Drawer = Sheet;

export function ConfirmAction({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel,
  onConfirm,
  loading,
  confirmDisabled,
  children,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: string;
  confirmLabel: string;
  onConfirm: () => void;
  loading?: boolean;
  confirmDisabled?: boolean;
  children?: ReactNode;
}) {
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={title}
      description={description}
      footer={
        <>
          <Button onClick={() => onOpenChange(false)}>Keep unchanged</Button>
          <Button
            variant="danger"
            loading={loading}
            disabled={confirmDisabled}
            onClick={onConfirm}
          >
            {confirmLabel}
          </Button>
        </>
      }
    >
      {children}
    </Dialog>
  );
}

export function Tooltip({
  content,
  children,
}: {
  content: string;
  children: ReactNode;
}) {
  return (
    <TooltipPrimitive.Provider delayDuration={300}>
      <TooltipPrimitive.Root>
        <TooltipPrimitive.Trigger asChild>{children}</TooltipPrimitive.Trigger>
        <TooltipPrimitive.Portal>
          <TooltipPrimitive.Content
            sideOffset={6}
            className="z-[70] rounded bg-slate-950 px-2 py-1 text-xs text-white shadow-overlay"
          >
            {content}
            <TooltipPrimitive.Arrow className="fill-slate-950" />
          </TooltipPrimitive.Content>
        </TooltipPrimitive.Portal>
      </TooltipPrimitive.Root>
    </TooltipPrimitive.Provider>
  );
}

export function InlineNotice({
  tone = "info",
  title,
  children,
}: {
  tone?: "info" | "success" | "warning" | "danger";
  title: string;
  children: ReactNode;
}) {
  const palette = {
    info: "border-sky-200 bg-sky-50 text-sky-950",
    success: "border-emerald-200 bg-emerald-50 text-emerald-950",
    warning: "border-amber-200 bg-amber-50 text-amber-950",
    danger: "border-red-200 bg-red-50 text-red-950",
  };
  return (
    <div
      className={cn("rounded-md border p-3 text-sm", palette[tone])}
      role={tone === "danger" ? "alert" : tone === "success" ? "status" : undefined}
    >
      <p className="flex items-center gap-2 font-semibold">
        {tone === "success" ? (
          <CheckCircle2 aria-hidden className="h-4 w-4" />
        ) : (
          <AlertCircle aria-hidden className="h-4 w-4" />
        )}
        {title}
      </p>
      <div className="mt-1 pl-6 text-xs opacity-85">{children}</div>
    </div>
  );
}

export function DataTable({
  children,
  label,
  tableClassName,
}: {
  children: ReactNode;
  label: string;
  tableClassName?: string;
}) {
  return (
    <div
      className="w-full min-w-0 max-w-full overflow-x-auto [contain:inline-size] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-inset"
      role="region"
      aria-label={`${label} scroll area`}
      tabIndex={0}
    >
      <table
        className={cn(
          "w-full border-collapse text-left text-sm",
          tableClassName ?? "min-w-[760px]",
        )}
        aria-label={label}
      >
        {children}
      </table>
    </div>
  );
}

export function FilterBar({ children }: { children: ReactNode }) {
  return (
    <div className="flex flex-wrap gap-3 border-b border-border p-4">
      {children}
    </div>
  );
}

export function Skeleton({ className }: { className?: string }) {
  return (
    <span
      aria-hidden
      className={cn("block animate-pulse rounded bg-slate-200", className)}
    />
  );
}

export const DataGrid = DataTable;

export const TableHead = ({
  children,
  className,
}: {
  children: ReactNode;
  className?: string;
}) => (
  <th
    scope="col"
    className={cn(
      "sticky top-0 z-10 whitespace-nowrap border-b border-border bg-slate-50 px-4 py-2.5 text-xs font-semibold uppercase tracking-wide text-slate-500",
      className,
    )}
  >
    {children}
  </th>
);
export const TableCell = ({
  children,
  className,
}: {
  children: ReactNode;
  className?: string;
}) => (
  <td
    className={cn(
      "border-b border-border px-4 py-3 align-middle text-slate-700",
      className,
    )}
  >
    {children}
  </td>
);
