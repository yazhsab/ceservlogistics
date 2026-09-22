import { useQuery } from "@tanstack/react-query";
import {
  ArrowRight,
  BookOpen,
  Box,
  CircleAlert,
  FileText,
  Globe2,
  HandCoins,
  Home,
  Landmark,
  LogOut,
  MapPin,
  PackageCheck,
  PackagePlus,
  PackageSearch,
  ScanLine,
  UserRound,
  WalletCards,
  Warehouse,
  type LucideIcon,
} from "lucide-react";
import { useState, type ReactNode } from "react";
import {
  Link,
  NavLink,
  Outlet,
  useParams,
  useSearchParams,
} from "react-router-dom";
import {
  apiRequest,
  queryString,
  type PortalCustomerAccountListResponse,
  type PortalCustomerInvoiceListResponse,
  type PortalCustomerShipmentListResponse,
  type PortalCustomerSummary,
  type PortalFranchiseSettlementListResponse,
  type PortalFranchiseShipmentListResponse,
  type PortalFranchiseSummary,
  type TrackingResult,
} from "../api/client";
import { useAuth } from "../auth/AuthProvider";
import { Money } from "../components/financial";
import { JourneyTimeline } from "../components/operations";
import {
  Badge,
  Button,
  DataTable,
  EmptyState,
  ErrorState,
  FilterBar,
  InlineNotice,
  Input,
  LoadingState,
  Panel,
  PanelHeader,
  Select,
  StatusBadge,
  TableCell,
  TableHead,
} from "../components/ui";
import { cn, formatDateTime, formatMoney, titleCase } from "../lib/utils";

const portalLink =
  "inline-flex min-h-10 items-center justify-center gap-2 rounded-md border border-border bg-white px-3.5 text-sm font-semibold text-slate-800 transition hover:bg-slate-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2";

function AudienceShell({
  audience,
  links,
  children,
}: {
  audience: "customer" | "franchise";
  links: Array<{ to: string; label: string; icon: LucideIcon; end?: boolean }>;
  children: ReactNode;
}) {
  const { user, logout } = useAuth();
  const audienceName =
    audience === "customer" ? "Customer portal" : "Franchise workspace";
  return (
    <div className="min-h-screen bg-[#f5f3ef] text-slate-950">
      <a
        href="#portal-content"
        className="sr-only z-[100] rounded bg-white px-3 py-2 focus:not-sr-only focus:fixed focus:left-3 focus:top-3"
      >
        Skip to content
      </a>
      <header
        className={cn(
          "sticky top-0 z-40 border-b backdrop-blur",
          audience === "customer"
            ? "border-white/10 bg-[#15243a]/95 text-white"
            : "bg-white/95",
        )}
      >
        <div className="mx-auto flex min-h-16 max-w-7xl items-center justify-between gap-3 px-4 sm:px-6">
          <Link
            to={`/portal/${audience}`}
            className="flex min-w-0 items-center gap-3"
          >
            <span
              className={cn(
                "grid h-9 w-9 shrink-0 place-items-center rounded-md text-xs font-black text-white",
                audience === "customer" ? "bg-[#f2673d]" : "bg-primary",
              )}
            >
              CS
            </span>
            <span className="min-w-0">
              <strong className="block truncate text-sm">
                Ceserv Logistics
              </strong>
              <span
                className={cn(
                  "block truncate text-xs",
                  audience === "customer" ? "text-white/65" : "text-slate-500",
                )}
              >
                {audienceName}
              </span>
            </span>
          </Link>
          <div className="flex items-center gap-2">
            {audience === "customer" ? (
              <a
                href="https://www.ceservlogistics.com/"
                className="hidden items-center gap-2 text-sm font-semibold text-white/80 hover:text-white md:inline-flex"
              >
                <Globe2 aria-hidden className="h-4 w-4" /> Main website
              </a>
            ) : null}
            <span
              className={cn(
                "hidden max-w-48 truncate text-sm sm:block",
                audience === "customer" ? "text-white/70" : "text-slate-600",
              )}
            >
              {user?.fullName}
            </span>
            <Button
              size="sm"
              variant="ghost"
              className={
                audience === "customer"
                  ? "text-white hover:bg-white/10"
                  : undefined
              }
              onClick={() => void logout()}
            >
              <LogOut aria-hidden className="h-4 w-4" /> Sign out
            </Button>
          </div>
        </div>
        <nav
          className={cn(
            "mx-auto max-w-7xl pb-2",
            audience === "customer"
              ? "grid grid-cols-4 gap-0.5 px-2 sm:flex sm:gap-1 sm:px-6"
              : "flex gap-1 overflow-x-auto px-3 sm:px-6",
          )}
          aria-label={audienceName}
        >
          {links.map((item) => {
            const Icon = item.icon;
            return (
              <NavLink
                key={item.to}
                to={item.to}
                end={item.end}
                className={({ isActive }) =>
                  cn(
                    "inline-flex min-h-10 items-center justify-center rounded-md font-semibold focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary",
                    audience === "customer"
                      ? "min-w-0 flex-col gap-0.5 px-1 text-xs sm:flex-row sm:gap-2 sm:px-3 sm:text-sm"
                      : "shrink-0 gap-2 px-3 text-sm",
                    isActive
                      ? audience === "customer"
                        ? "bg-[#f2673d] text-white"
                        : "bg-emerald-50 text-primary"
                      : audience === "customer"
                        ? "text-white/70 hover:bg-white/10 hover:text-white"
                        : "text-slate-600 hover:bg-slate-100 hover:text-slate-950",
                  )
                }
              >
                <Icon aria-hidden className="h-4 w-4" /> {item.label}
              </NavLink>
            );
          })}
        </nav>
      </header>
      <main
        id="portal-content"
        className="mx-auto w-full min-w-0 max-w-7xl overflow-x-clip px-4 py-6 sm:px-6 sm:py-8"
      >
        {children}
      </main>
    </div>
  );
}

export function CustomerPortalLayout() {
  const { user, hasPermission } = useAuth();
  if (!hasPermission("portal.customer") || !user?.portal?.isCustomerUser) {
    return (
      <main className="grid min-h-screen place-items-center bg-slate-50 p-6">
        <div className="max-w-lg rounded-lg border bg-white">
          <ErrorState
            error={
              new Error(
                "This sign-in is not linked to a customer account. Customer access is based on the account relationship, not the role name alone.",
              )
            }
          />
        </div>
      </main>
    );
  }
  return (
    <AudienceShell
      audience="customer"
      links={[
        { to: "/portal/customer", label: "Overview", icon: Home, end: true },
        {
          to: "/portal/customer/shipments",
          label: "Shipments",
          icon: PackageSearch,
        },
        { to: "/portal/customer/invoices", label: "Invoices", icon: FileText },
        { to: "/portal/customer/profile", label: "Profile", icon: UserRound },
      ]}
    >
      <Outlet />
    </AudienceShell>
  );
}

export function FranchisePortalLayout() {
  const { user, hasPermission } = useAuth();
  if (!hasPermission("portal.franchise") || !user?.portal?.franchise) {
    return (
      <main className="grid min-h-screen place-items-center bg-slate-50 p-6">
        <div className="max-w-lg rounded-lg border bg-white">
          <ErrorState
            error={
              new Error(
                "This sign-in is not bound to one franchise. Ask an administrator to correct the operating-unit assignment.",
              )
            }
          />
        </div>
      </main>
    );
  }
  return (
    <AudienceShell
      audience="franchise"
      links={[
        { to: "/portal/franchise", label: "Dashboard", icon: Home, end: true },
        {
          to: "/portal/franchise/shipments",
          label: "Operations",
          icon: Warehouse,
        },
        {
          to: "/portal/franchise/settlements",
          label: "Finance",
          icon: Landmark,
        },
        { to: "/portal/franchise/profile", label: "Profile", icon: UserRound },
      ]}
    >
      <Outlet />
    </AudienceShell>
  );
}

function PortalHeading({
  eyebrow,
  title,
  description,
  actions,
}: {
  eyebrow: string;
  title: string;
  description?: string;
  actions?: ReactNode;
}) {
  return (
    <header className="mb-6 flex flex-wrap items-start justify-between gap-4">
      <div>
        <p className="text-xs font-semibold uppercase tracking-wider text-primary">
          {eyebrow}
        </p>
        <h1 className="mt-1 text-2xl font-bold tracking-tight sm:text-3xl">
          {title}
        </h1>
        {description ? (
          <p className="mt-2 max-w-3xl text-sm leading-6 text-slate-600">
            {description}
          </p>
        ) : null}
      </div>
      {actions ? <div className="flex flex-wrap gap-2">{actions}</div> : null}
    </header>
  );
}

function MetricCard({
  label,
  value,
  detail,
  tone = "neutral",
  icon: Icon,
}: {
  label: string;
  value: ReactNode;
  detail?: string;
  tone?: "neutral" | "success" | "warning" | "danger";
  icon: LucideIcon;
}) {
  const tones = {
    neutral: "border-slate-200",
    success: "border-emerald-300",
    warning: "border-amber-300",
    danger: "border-red-300",
  };
  return (
    <div
      className={cn(
        "rounded-lg border-l-4 border-y border-r bg-white p-4",
        tones[tone],
      )}
    >
      <div className="flex items-center justify-between gap-3 text-slate-500">
        <span className="text-xs font-semibold uppercase tracking-wide">
          {label}
        </span>
        <Icon aria-hidden className="h-4 w-4" />
      </div>
      <strong className="mt-2 block text-2xl tabular-nums">{value}</strong>
      {detail ? <p className="mt-1 text-xs text-slate-500">{detail}</p> : null}
    </div>
  );
}

export function CustomerPortalDashboardPage() {
  const summary = useQuery({
    queryKey: ["portal-customer-summary"],
    queryFn: () =>
      apiRequest<PortalCustomerSummary>(
        "/api/v1/portal/customer/summary?days=90",
      ),
  });
  const accounts = useQuery({
    queryKey: ["portal-customer-accounts"],
    queryFn: () =>
      apiRequest<PortalCustomerAccountListResponse>(
        "/api/v1/portal/customer/accounts",
      ),
  });
  const shipments = useQuery({
    queryKey: ["portal-customer-shipments", "recent"],
    queryFn: () =>
      apiRequest<PortalCustomerShipmentListResponse>(
        "/api/v1/portal/customer/shipments?limit=6",
      ),
  });
  const invoices = useQuery({
    queryKey: ["portal-customer-invoices", "recent"],
    queryFn: () =>
      apiRequest<PortalCustomerInvoiceListResponse>(
        "/api/v1/portal/customer/invoices?limit=200",
      ),
  });
  const firstError =
    summary.error || accounts.error || shipments.error || invoices.error;
  if (
    summary.isLoading ||
    accounts.isLoading ||
    shipments.isLoading ||
    invoices.isLoading
  )
    return <LoadingState label="Loading your account overview" />;
  if (firstError)
    return (
      <ErrorState
        error={firstError}
        retry={() => {
          void summary.refetch();
          void accounts.refetch();
          void shipments.refetch();
          void invoices.refetch();
        }}
      />
    );
  const invoiceRows = invoices.data?.data ?? [];
  const outstandingRows = invoiceRows.filter(
    (invoice) => (invoice.outstandingMinor ?? 0) > 0,
  );
  const account = accounts.data?.data?.[0];
  return (
    <>
      <PortalHeading
        eyebrow="Your deliveries"
        title="Good to see you"
        description="Track current deliveries, review account activity, and find invoices without internal courier terminology."
        actions={
          <Link className={portalLink} to="/portal/customer/shipments">
            <PackageSearch aria-hidden className="h-4 w-4" /> Track a shipment
          </Link>
        }
      />
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <MetricCard
          label="Active shipments"
          value={summary.data?.inProgress ?? 0}
          detail="Currently moving"
          icon={Box}
        />
        <MetricCard
          label="Delivered"
          value={summary.data?.delivered ?? 0}
          detail={`Last ${summary.data?.windowDays ?? 90} days`}
          tone="success"
          icon={PackageCheck}
        />
        <MetricCard
          label="Exceptions"
          value={summary.data?.exceptions ?? 0}
          detail="May need attention"
          tone={(summary.data?.exceptions ?? 0) > 0 ? "warning" : "success"}
          icon={CircleAlert}
        />
        <MetricCard
          label="Outstanding invoices"
          value={outstandingRows.length}
          detail={
            invoiceRows.length === 200
              ? "Newest 200 invoices"
              : "Current account invoices"
          }
          tone={outstandingRows.length ? "warning" : "success"}
          icon={FileText}
        />
      </div>
      <div className="mt-5 grid gap-5 lg:grid-cols-[minmax(0,1.35fr)_minmax(300px,0.65fr)]">
        <Panel>
          <PanelHeader
            title="Recent activity"
            description="Latest shipment updates on your account"
            actions={
              <Link
                className="text-sm font-semibold text-primary hover:underline"
                to="/portal/customer/shipments"
              >
                View history
              </Link>
            }
          />
          {(shipments.data?.data ?? []).length ? (
            <div className="divide-y">
              {shipments.data?.data?.map((shipment) => (
                <Link
                  key={shipment.id}
                  to={`/portal/customer/shipments/${shipment.id}`}
                  className="flex min-h-20 items-center justify-between gap-4 px-4 py-3 hover:bg-slate-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary"
                >
                  <div className="min-w-0">
                    <strong className="block truncate font-mono text-sm">
                      {shipment.awb}
                    </strong>
                    <span className="mt-1 block truncate text-xs text-slate-500">
                      To{" "}
                      {shipment.recipientCity ||
                        shipment.destinationPincode ||
                        "destination"}{" "}
                      · {shipment.service}
                    </span>
                  </div>
                  <div className="shrink-0 text-right">
                    <StatusBadge status={shipment.status} />
                    <span className="mt-1 block text-xs text-slate-500">
                      {formatDateTime(shipment.statusChangedAt)}
                    </span>
                  </div>
                </Link>
              ))}
            </div>
          ) : (
            <EmptyState
              title="No shipment activity"
              description="Shipments linked to your account will appear here."
            />
          )}
        </Panel>
        <div className="space-y-5">
          <Panel>
            <PanelHeader title="Account position" />
            <dl className="space-y-3 p-4 text-sm">
              <div>
                <dt className="text-slate-500">Account</dt>
                <dd className="font-semibold">
                  {account?.name ?? "Linked customer"}
                </dd>
              </div>
              <div>
                <dt className="text-slate-500">Credit available</dt>
                <dd className="mt-1 text-lg">
                  <Money
                    amountMinor={account?.creditAvailableMinor}
                    currency={
                      account?.currency ?? summary.data?.currency ?? "NGN"
                    }
                  />
                </dd>
              </div>
              <div>
                <dt className="text-slate-500">Payment terms</dt>
                <dd className="font-semibold">
                  {account?.paymentTermsDays != null
                    ? `${account.paymentTermsDays} days`
                    : "Not configured"}
                </dd>
              </div>
            </dl>
          </Panel>
          <InlineNotice tone="info" title="Need to book or change an address?">
            Customer self-service booking and address changes are not enabled
            for this account yet. Contact your account team; existing deliveries
            and invoices remain available here.
          </InlineNotice>
        </div>
      </div>
    </>
  );
}

export function CustomerShipmentsPage() {
  const [params, setParams] = useSearchParams();
  const status = params.get("status") ?? "";
  const search = params.get("search") ?? "";
  const cursor = params.get("cursor") ?? "";
  const query = useQuery({
    queryKey: ["portal-customer-shipments", status, search, cursor],
    queryFn: () =>
      apiRequest<PortalCustomerShipmentListResponse>(
        `/api/v1/portal/customer/shipments${queryString({ status, search, cursor, limit: 50 })}`,
      ),
  });
  return (
    <>
      <PortalHeading
        eyebrow="Shipment history"
        title="Your shipments"
        description="Search by tracking number or reference. Journey updates use customer-friendly milestones."
      />
      <Panel>
        <FilterBar>
          <Input
            aria-label="Search shipments"
            className="min-w-56 flex-1"
            defaultValue={search}
            placeholder="Tracking number or reference"
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                const next = new URLSearchParams(params);
                next.set("search", event.currentTarget.value);
                next.delete("cursor");
                setParams(next);
              }
            }}
          />
          <Select
            aria-label="Shipment status"
            value={status}
            onChange={(event) => {
              const next = new URLSearchParams(params);
              if (event.target.value) next.set("status", event.target.value);
              else next.delete("status");
              next.delete("cursor");
              setParams(next);
            }}
          >
            <option value="">All statuses</option>
            <option>BOOKED</option>
            <option>IN_TRANSIT</option>
            <option>OUT_FOR_DELIVERY</option>
            <option>DELIVERED</option>
            <option>NDR</option>
            <option>RTO_INITIATED</option>
          </Select>
        </FilterBar>
        {query.isLoading ? (
          <LoadingState label="Loading shipment history" />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : (query.data?.data ?? []).length ? (
          <>
            <div className="divide-y divide-border sm:hidden">
              {query.data?.data?.map((shipment) => (
                <article className="space-y-3 p-4" key={shipment.id}>
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0">
                      <strong className="block truncate font-mono text-sm">
                        {shipment.awb}
                      </strong>
                      {shipment.reference ? (
                        <span className="block truncate text-xs text-slate-500">
                          {shipment.reference}
                        </span>
                      ) : null}
                    </div>
                    <StatusBadge status={shipment.status} />
                  </div>
                  <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-xs">
                    <div>
                      <dt className="text-slate-500">Recipient</dt>
                      <dd className="mt-0.5 font-medium text-slate-900">
                        {shipment.recipientName ?? "—"}
                      </dd>
                    </div>
                    <div>
                      <dt className="text-slate-500">Destination</dt>
                      <dd className="mt-0.5 font-medium text-slate-900">
                        {shipment.recipientCity ?? shipment.destinationPincode}
                      </dd>
                    </div>
                    <div>
                      <dt className="text-slate-500">Service</dt>
                      <dd className="mt-0.5 font-medium text-slate-900">
                        {shipment.service}
                      </dd>
                    </div>
                    <div>
                      <dt className="text-slate-500">Booked</dt>
                      <dd className="mt-0.5 font-medium text-slate-900">
                        {formatDateTime(shipment.bookedAt)}
                      </dd>
                    </div>
                  </dl>
                  <Link
                    className="inline-flex min-h-11 items-center font-semibold text-primary hover:underline"
                    to={`/portal/customer/shipments/${shipment.id}`}
                  >
                    Track shipment{" "}
                    <ArrowRight aria-hidden className="ml-2 h-4 w-4" />
                  </Link>
                </article>
              ))}
            </div>
            <div className="hidden sm:block">
              <DataTable
                label="Customer shipment history"
                tableClassName="min-w-[640px]"
              >
                <thead>
                  <tr>
                    <TableHead>Tracking number</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead>Recipient</TableHead>
                    <TableHead>Destination</TableHead>
                    <TableHead>Service</TableHead>
                    <TableHead>Booked</TableHead>
                    <TableHead>
                      <span className="sr-only">Open</span>
                    </TableHead>
                  </tr>
                </thead>
                <tbody>
                  {query.data?.data?.map((shipment) => (
                    <tr key={shipment.id}>
                      <TableCell>
                        <strong className="font-mono">{shipment.awb}</strong>
                        {shipment.reference ? (
                          <span className="block text-xs text-slate-500">
                            {shipment.reference}
                          </span>
                        ) : null}
                      </TableCell>
                      <TableCell>
                        <StatusBadge status={shipment.status} />
                      </TableCell>
                      <TableCell>{shipment.recipientName ?? "—"}</TableCell>
                      <TableCell>
                        {shipment.recipientCity ?? shipment.destinationPincode}
                      </TableCell>
                      <TableCell>{shipment.service}</TableCell>
                      <TableCell>{formatDateTime(shipment.bookedAt)}</TableCell>
                      <TableCell>
                        <Link
                          className="font-semibold text-primary hover:underline"
                          to={`/portal/customer/shipments/${shipment.id}`}
                        >
                          Track <span className="sr-only">{shipment.awb}</span>
                        </Link>
                      </TableCell>
                    </tr>
                  ))}
                </tbody>
              </DataTable>
            </div>
            <div className="flex justify-end border-t p-3">
              <Button
                disabled={!query.data?.nextCursor}
                onClick={() => {
                  const next = new URLSearchParams(params);
                  if (query.data?.nextCursor)
                    next.set("cursor", query.data.nextCursor);
                  setParams(next);
                }}
              >
                Load next page <ArrowRight aria-hidden className="h-4 w-4" />
              </Button>
            </div>
          </>
        ) : (
          <EmptyState
            title="No matching shipments"
            description="Try a different tracking number or status."
          />
        )}
      </Panel>
    </>
  );
}

export function CustomerTrackingDetailPage() {
  const { shipmentId = "" } = useParams();
  const query = useQuery({
    queryKey: ["portal-customer-track", shipmentId],
    queryFn: () =>
      apiRequest<TrackingResult>(
        `/api/v1/portal/customer/shipments/${shipmentId}/track`,
      ),
    enabled: Boolean(shipmentId),
    retry: false,
  });
  if (query.isLoading) return <LoadingState label="Loading tracking journey" />;
  if (query.error)
    return (
      <ErrorState error={query.error} retry={() => void query.refetch()} />
    );
  const result = query.data;
  if (!result)
    return (
      <EmptyState
        title="Shipment not found"
        description="It may not belong to an account linked to this sign-in."
      />
    );
  return (
    <>
      <PortalHeading
        eyebrow="Track"
        title={
          result.statusTitle ?? titleCase(result.milestone ?? "Shipment update")
        }
        description={result.statusDescription}
        actions={
          <Link className={portalLink} to="/portal/customer/shipments">
            Back to shipments
          </Link>
        }
      />
      <div className="grid gap-5 lg:grid-cols-[minmax(0,1.25fr)_minmax(280px,0.75fr)]">
        <Panel>
          <PanelHeader
            title={result.awb ?? "Shipment journey"}
            description="Only customer-safe journey milestones are shown"
          />
          <div className="p-5">
            {result.events?.length ? (
              <JourneyTimeline
                items={result.events.map((event, index) => ({
                  key: `${event.milestone}-${event.occurredAt}-${index}`,
                  title: event.title ?? titleCase(event.milestone ?? "Update"),
                  description: [event.description, event.location]
                    .filter(Boolean)
                    .join(" · "),
                  occurredAt: event.occurredAt,
                  status: event.milestone,
                  current: index === result.events!.length - 1,
                }))}
              />
            ) : (
              <EmptyState
                title="No journey updates yet"
                description="Movement updates will appear here."
              />
            )}
          </div>
        </Panel>
        <div className="space-y-4">
          <Panel>
            <PanelHeader title="Shipment summary" />
            <dl className="space-y-3 p-4 text-sm">
              <div>
                <dt className="text-slate-500">Tracking number</dt>
                <dd className="font-mono font-semibold">{result.awb}</dd>
              </div>
              <div>
                <dt className="text-slate-500">Journey</dt>
                <dd className="font-semibold">
                  {result.origin || "Origin"} →{" "}
                  {result.destination || "Destination"}
                </dd>
              </div>
              <div>
                <dt className="text-slate-500">Expected delivery</dt>
                <dd className="font-semibold">
                  {result.expectedDelivery || "To be confirmed"}
                </dd>
              </div>
              <div>
                <dt className="text-slate-500">Last updated</dt>
                <dd>{formatDateTime(result.lastUpdatedAt)}</dd>
              </div>
            </dl>
          </Panel>
          {(result.amountDueOnDeliveryMinor ?? 0) > 0 ? (
            <div className="rounded-lg border border-amber-300 bg-amber-50 p-4">
              <p className="text-xs font-semibold uppercase tracking-wide text-amber-900">
                Amount due on delivery
              </p>
              <strong className="mt-1 block text-2xl">
                {formatMoney(
                  result.amountDueOnDeliveryMinor,
                  result.currency ?? "NGN",
                  "en-NG",
                )}
              </strong>
            </div>
          ) : null}
        </div>
      </div>
    </>
  );
}

export function CustomerInvoicesPage() {
  const [status, setStatus] = useState("");
  const query = useQuery({
    queryKey: ["portal-customer-invoices", status],
    queryFn: () =>
      apiRequest<PortalCustomerInvoiceListResponse>(
        `/api/v1/portal/customer/invoices${queryString({ status, limit: 200 })}`,
      ),
  });
  return (
    <>
      <PortalHeading
        eyebrow="Billing"
        title="Invoices"
        description="Review issue dates, payment state, and the amount still outstanding."
      />
      <Panel>
        <FilterBar>
          <Select
            aria-label="Invoice status"
            value={status}
            onChange={(event) => setStatus(event.target.value)}
          >
            <option value="">All statuses</option>
            <option>ISSUED</option>
            <option>PARTIALLY_PAID</option>
            <option>PAID</option>
            <option>OVERDUE</option>
          </Select>
        </FilterBar>
        {query.isLoading ? (
          <LoadingState label="Loading invoices" />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : (query.data?.data ?? []).length ? (
          <DataTable label="Customer invoices">
            <thead>
              <tr>
                <TableHead>Invoice</TableHead>
                <TableHead>Period</TableHead>
                <TableHead>Due</TableHead>
                <TableHead>Status</TableHead>
                <TableHead className="text-right">Total</TableHead>
                <TableHead className="text-right">Outstanding</TableHead>
              </tr>
            </thead>
            <tbody>
              {query.data?.data?.map((invoice) => (
                <tr key={invoice.id}>
                  <TableCell>
                    <strong>{invoice.invoiceNumber}</strong>
                    <span className="block text-xs text-slate-500">
                      {invoice.customer}
                    </span>
                  </TableCell>
                  <TableCell>
                    {invoice.periodStart && invoice.periodEnd
                      ? `${invoice.periodStart} – ${invoice.periodEnd}`
                      : "—"}
                  </TableCell>
                  <TableCell>{invoice.dueDate ?? "—"}</TableCell>
                  <TableCell>
                    <StatusBadge status={invoice.status} />
                  </TableCell>
                  <TableCell className="text-right">
                    <Money
                      amountMinor={invoice.totalMinor}
                      currency={invoice.currency ?? "NGN"}
                    />
                  </TableCell>
                  <TableCell className="text-right">
                    <Money
                      amountMinor={invoice.outstandingMinor}
                      currency={invoice.currency ?? "NGN"}
                    />
                  </TableCell>
                </tr>
              ))}
            </tbody>
          </DataTable>
        ) : (
          <EmptyState
            title="No invoices"
            description="Invoices for linked accounts will appear here."
          />
        )}
      </Panel>
    </>
  );
}

export function PortalProfilePage({
  audience,
}: {
  audience: "customer" | "franchise";
}) {
  const { user } = useAuth();
  const subject =
    audience === "customer"
      ? user?.portal?.customers?.map((item) => item.name).join(", ")
      : user?.portal?.franchise?.name;
  return (
    <>
      <PortalHeading
        eyebrow="Account"
        title="Profile"
        description="Your sign-in and the commercial account it represents."
      />
      <div className="grid gap-5 md:grid-cols-2">
        <Panel>
          <PanelHeader title="Sign-in" />
          <dl className="space-y-3 p-4 text-sm">
            <div>
              <dt className="text-slate-500">Name</dt>
              <dd className="font-semibold">{user?.fullName}</dd>
            </div>
            <div>
              <dt className="text-slate-500">Email</dt>
              <dd>{user?.email}</dd>
            </div>
            <div>
              <dt className="text-slate-500">Status</dt>
              <dd>
                <StatusBadge status={user?.status} />
              </dd>
            </div>
          </dl>
        </Panel>
        <Panel>
          <PanelHeader
            title={
              audience === "customer"
                ? "Linked customer account"
                : "Linked franchise"
            }
          />
          <div className="p-4">
            <strong>{subject || "Not linked"}</strong>
            <p className="mt-2 text-sm text-slate-600">
              Account relationships and access are maintained by an
              administrator and cannot be changed from this screen.
            </p>
          </div>
        </Panel>
      </div>
    </>
  );
}

const actionDefinitions = [
  {
    label: "Book shipment",
    to: "/shipments/new",
    permission: "shipment.create",
    icon: PackagePlus,
  },
  {
    label: "Scan",
    to: "/operations/scanner",
    permission: "scan.read",
    icon: ScanLine,
  },
  {
    label: "Create bag",
    to: "/operations/bags",
    permission: "bag.read",
    icon: Box,
  },
  {
    label: "Create manifest",
    to: "/operations/manifests",
    permission: "manifest.read",
    icon: BookOpen,
  },
  {
    label: "Receive",
    to: "/operations/hub",
    permission: "hub.dashboard",
    icon: Warehouse,
  },
  {
    label: "Assign delivery",
    to: "/operations/delivery",
    permission: "delivery.manage",
    icon: MapPin,
  },
];

export function FranchisePortalDashboardPage() {
  const { hasPermission, user } = useAuth();
  const summary = useQuery({
    queryKey: ["portal-franchise-summary"],
    queryFn: () =>
      apiRequest<PortalFranchiseSummary>("/api/v1/portal/franchise/summary"),
  });
  const shipments = useQuery({
    queryKey: ["portal-franchise-shipments", "dashboard"],
    queryFn: () =>
      apiRequest<PortalFranchiseShipmentListResponse>(
        "/api/v1/portal/franchise/shipments?role=ORIGIN&limit=6",
      ),
  });
  const settlements = useQuery({
    queryKey: ["portal-franchise-settlements", "dashboard"],
    queryFn: () =>
      apiRequest<PortalFranchiseSettlementListResponse>(
        "/api/v1/portal/franchise/settlements?limit=6",
      ),
  });
  const error = summary.error || shipments.error || settlements.error;
  if (summary.isLoading || shipments.isLoading || settlements.isLoading)
    return <LoadingState label="Loading franchise workspace" />;
  if (error)
    return (
      <ErrorState
        error={error}
        retry={() => {
          void summary.refetch();
          void shipments.refetch();
          void settlements.refetch();
        }}
      />
    );
  const data = summary.data;
  const openSettlement = settlements.data?.data?.find(
    (item) => !["PAID", "CLOSED", "CANCELLED"].includes(item.status ?? ""),
  );
  return (
    <>
      <PortalHeading
        eyebrow={user?.portal?.franchise?.code ?? "Franchise"}
        title={data?.franchise?.name ?? "Franchise dashboard"}
        description={`Commercial and operational position for ${data?.from ?? "the current period"} to ${data?.to ?? "today"}.`}
      />
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
        <MetricCard
          label="Bookings"
          value={data?.booked ?? 0}
          detail="Selected period"
          icon={PackagePlus}
        />
        <MetricCard
          label="Delivered"
          value={data?.delivered ?? 0}
          tone="success"
          icon={PackageCheck}
        />
        <MetricCard
          label="NDR / exceptions"
          value={data?.ndr ?? 0}
          detail={`${data?.rto ?? 0} return-to-origin`}
          tone={(data?.ndr ?? 0) > 0 ? "warning" : "neutral"}
          icon={CircleAlert}
        />
        <MetricCard
          label="COD outstanding"
          value={
            <Money
              amountMinor={data?.codInCustodyMinor}
              currency={data?.currency ?? "NGN"}
            />
          }
          tone={(data?.codAgedOver48h ?? 0) > 0 ? "warning" : "neutral"}
          detail={`${data?.codAgedOver48h ?? 0} aged over 48h`}
          icon={WalletCards}
        />
        <MetricCard
          label="Commission outstanding"
          value={
            <Money
              amountMinor={data?.commissionOutstandingMinor}
              currency={data?.currency ?? "NGN"}
            />
          }
          icon={HandCoins}
        />
      </div>
      <Panel className="mt-5">
        <PanelHeader
          title="Quick actions"
          description="Only actions allowed by your role are shown"
        />
        <div className="grid gap-2 p-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6">
          {actionDefinitions
            .filter((item) => hasPermission(item.permission))
            .map((item) => {
              const Icon = item.icon;
              return (
                <Link
                  key={item.label}
                  className={cn(portalLink, "justify-start")}
                  to={item.to}
                >
                  <Icon aria-hidden className="h-4 w-4 text-primary" />
                  {item.label}
                </Link>
              );
            })}
        </div>
      </Panel>
      <div className="mt-5 grid gap-5 lg:grid-cols-[minmax(0,1.25fr)_minmax(320px,0.75fr)]">
        <Panel>
          <PanelHeader
            title="Recent bookings"
            description="Origin relationship — matches the booking total"
            actions={
              <Link
                className="text-sm font-semibold text-primary hover:underline"
                to="/portal/franchise/shipments"
              >
                Open operations
              </Link>
            }
          />
          {(shipments.data?.data ?? []).length ? (
            <div className="divide-y">
              {shipments.data?.data?.map((item) => (
                <div
                  key={item.id}
                  className="flex items-center justify-between gap-3 px-4 py-3"
                >
                  <div>
                    <strong className="font-mono text-sm">{item.awb}</strong>
                    <span className="block text-xs text-slate-500">
                      {item.customer} · {item.destinationPincode}
                    </span>
                  </div>
                  <div className="text-right">
                    <StatusBadge status={item.status} />
                    <span className="mt-1 block text-xs font-semibold text-slate-500">
                      {titleCase(item.role ?? "ORIGIN")}
                    </span>
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <EmptyState
              title="No bookings in this view"
              description="Origin bookings will appear here."
            />
          )}
        </Panel>
        <Panel>
          <PanelHeader title="Settlement due" />
          <div className="p-4">
            {openSettlement ? (
              <>
                <StatusBadge status={openSettlement.status} />
                <strong className="mt-3 block text-2xl">
                  <Money
                    amountMinor={openSettlement.outstandingMinor}
                    currency={
                      openSettlement.currency ?? data?.currency ?? "NGN"
                    }
                  />
                </strong>
                <p className="mt-1 text-sm text-slate-600">
                  {openSettlement.settlementNumber} ·{" "}
                  {openSettlement.periodStart} to {openSettlement.periodEnd}
                </p>
                <Link
                  className={cn(portalLink, "mt-4 w-full")}
                  to="/portal/franchise/settlements"
                >
                  Review settlement
                </Link>
              </>
            ) : (
              <EmptyState
                title="No settlement due"
                description="Open statements will appear here."
              />
            )}
          </div>
        </Panel>
      </div>
    </>
  );
}

export function FranchiseShipmentsPage() {
  const [role, setRole] = useState("ORIGIN");
  const [status, setStatus] = useState("");
  const query = useQuery({
    queryKey: ["portal-franchise-shipments", role, status],
    queryFn: () =>
      apiRequest<PortalFranchiseShipmentListResponse>(
        `/api/v1/portal/franchise/shipments${queryString({ role, status, limit: 200 })}`,
      ),
  });
  return (
    <>
      <PortalHeading
        eyebrow="Franchise operations"
        title="Shipment workload"
        description="Origin means booked here; destination means inbound delivery responsibility. The relationship is always shown per shipment."
      />
      <Panel>
        <FilterBar>
          <Select
            aria-label="Shipment relationship"
            value={role}
            onChange={(event) => setRole(event.target.value)}
          >
            <option>ORIGIN</option>
            <option>DESTINATION</option>
            <option>ANY</option>
          </Select>
          <Select
            aria-label="Shipment status"
            value={status}
            onChange={(event) => setStatus(event.target.value)}
          >
            <option value="">All statuses</option>
            <option>BOOKED</option>
            <option>IN_TRANSIT</option>
            <option>OUT_FOR_DELIVERY</option>
            <option>DELIVERED</option>
            <option>NDR</option>
          </Select>
        </FilterBar>
        {query.isLoading ? (
          <LoadingState label="Loading franchise shipments" />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : (query.data?.data ?? []).length ? (
          <DataTable label="Franchise shipment workload">
            <thead>
              <tr>
                <TableHead>AWB</TableHead>
                <TableHead>Relationship</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Customer</TableHead>
                <TableHead>Destination postcode</TableHead>
                <TableHead>Payment</TableHead>
                <TableHead className="text-right">COD</TableHead>
                <TableHead>Booked</TableHead>
              </tr>
            </thead>
            <tbody>
              {query.data?.data?.map((item) => (
                <tr key={item.id}>
                  <TableCell>
                    <strong className="font-mono">{item.awb}</strong>
                  </TableCell>
                  <TableCell>
                    <Badge tone={item.role === "ORIGIN" ? "info" : "warning"}>
                      {titleCase(item.role ?? "UNKNOWN")}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    <StatusBadge status={item.status} />
                  </TableCell>
                  <TableCell>{item.customer}</TableCell>
                  <TableCell>{item.destinationPincode}</TableCell>
                  <TableCell>{titleCase(item.paymentMode ?? "")}</TableCell>
                  <TableCell className="text-right">
                    <Money
                      amountMinor={item.codAmountMinor}
                      currency={item.currency ?? "NGN"}
                    />
                  </TableCell>
                  <TableCell>{formatDateTime(item.bookedAt)}</TableCell>
                </tr>
              ))}
            </tbody>
          </DataTable>
        ) : (
          <EmptyState
            title="No shipments in this relationship"
            description="Change the relationship or status filter."
          />
        )}
      </Panel>
    </>
  );
}

export function FranchiseSettlementsPage() {
  const { hasPermission } = useAuth();
  const [status, setStatus] = useState("");
  const query = useQuery({
    queryKey: ["portal-franchise-settlements", status],
    queryFn: () =>
      apiRequest<PortalFranchiseSettlementListResponse>(
        `/api/v1/portal/franchise/settlements${queryString({ status, limit: 200 })}`,
      ),
  });
  return (
    <>
      <PortalHeading
        eyebrow="Franchise finance"
        title="Settlements"
        description="Each statement shows its signed net, amount paid, and amount still outstanding. Approval and payment actions remain in the permission-controlled finance workflow."
      />
      <Panel>
        <FilterBar>
          <Select
            aria-label="Settlement status"
            value={status}
            onChange={(event) => setStatus(event.target.value)}
          >
            <option value="">All statuses</option>
            <option>DRAFT</option>
            <option>CALCULATED</option>
            <option>APPROVED</option>
            <option>PARTIALLY_PAID</option>
            <option>PAID</option>
            <option>CLOSED</option>
          </Select>
        </FilterBar>
        {query.isLoading ? (
          <LoadingState label="Loading settlements" />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : (query.data?.data ?? []).length ? (
          <DataTable label="Franchise settlements">
            <thead>
              <tr>
                <TableHead>Settlement</TableHead>
                <TableHead>Period</TableHead>
                <TableHead>Status</TableHead>
                <TableHead className="text-right">Net</TableHead>
                <TableHead className="text-right">Paid</TableHead>
                <TableHead className="text-right">Outstanding</TableHead>
              </tr>
            </thead>
            <tbody>
              {query.data?.data?.map((item) => (
                <tr key={item.id}>
                  <TableCell>
                    <strong>{item.settlementNumber}</strong>
                  </TableCell>
                  <TableCell>
                    {item.periodStart} – {item.periodEnd}
                  </TableCell>
                  <TableCell>
                    <StatusBadge status={item.status} />
                  </TableCell>
                  <TableCell className="text-right">
                    <Money
                      amountMinor={item.netAmountMinor}
                      currency={item.currency ?? "NGN"}
                      showPositiveSign
                    />
                  </TableCell>
                  <TableCell className="text-right">
                    <Money
                      amountMinor={item.paidMinor}
                      currency={item.currency ?? "NGN"}
                    />
                  </TableCell>
                  <TableCell className="text-right">
                    <Money
                      amountMinor={item.outstandingMinor}
                      currency={item.currency ?? "NGN"}
                    />
                  </TableCell>
                </tr>
              ))}
            </tbody>
          </DataTable>
        ) : (
          <EmptyState
            title="No settlements"
            description="Statements for this franchise will appear here."
          />
        )}
      </Panel>
      <div className="mt-4 grid gap-3 sm:grid-cols-4">
        {[
          {
            label: "COD",
            to: "/finance/cod",
            permission: "cod.read",
            icon: WalletCards,
          },
          {
            label: "Commission",
            to: "/finance/commission/entries",
            permission: "commission.read",
            icon: HandCoins,
          },
          {
            label: "Ledger",
            to: "/finance/ledger/accounts",
            permission: "ledger.read",
            icon: BookOpen,
          },
          {
            label: "Settlement workflow",
            to: "/finance/settlements",
            permission: "settlement.read",
            icon: Landmark,
          },
        ]
          .filter((item) => hasPermission(item.permission))
          .map((item) => {
            const Icon = item.icon;
            return (
              <Link key={item.label} className={portalLink} to={item.to}>
                <Icon aria-hidden className="h-4 w-4" />
                {item.label}
              </Link>
            );
          })}
      </div>
    </>
  );
}
