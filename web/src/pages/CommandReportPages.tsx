import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  Box,
  Building2,
  CircleAlert,
  Clock3,
  Download,
  FileBarChart,
  FileText,
  HandCoins,
  Landmark,
  PackageCheck,
  PackagePlus,
  Play,
  RotateCcw,
  Truck,
  WalletCards,
  XCircle,
  type LucideIcon,
} from "lucide-react";
import { useState, type ReactNode } from "react";
import { useForm } from "react-hook-form";
import { Link } from "react-router-dom";
import { z } from "zod";
import {
  apiRequest,
  queryString,
  type CommandBacklogResponse,
  type CommandOverview,
  type CommandServiceResponse,
  type CommandSnapshotResponse,
  type CommandTrendResponse,
  type CommandUnitResponse,
  type OperatingUnitListResponse,
  type QueuedReportResponse,
  type QueueReportRequest,
  type ReportListResponse,
  type ReportRun,
  type ReportStatus,
  type ReportType,
  type ReportTypeListResponse,
  type ServiceListResponse,
} from "../api/client";
import { useAuth } from "../auth/AuthProvider";
import { Money } from "../components/financial";
import { useToast } from "../components/ToastProvider";
import {
  Badge,
  Button,
  ConfirmAction,
  DataTable,
  Dialog,
  EmptyState,
  ErrorState,
  Field,
  FilterBar,
  InlineNotice,
  Input,
  LoadingState,
  PageHeader,
  Panel,
  PanelHeader,
  Select,
  StatusBadge,
  TableCell,
  TableHead,
} from "../components/ui";
import {
  cn,
  formatDateTime,
  safeDownloadName,
  titleCase,
} from "../lib/utils";

function lagosDate(offsetDays = 0) {
  const date = new Date(Date.now() + offsetDays * 86_400_000);
  const parts = new Intl.DateTimeFormat("en-CA", {
    timeZone: "Africa/Lagos",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).formatToParts(date);
  const value = Object.fromEntries(
    parts.map((part) => [part.type, part.value]),
  );
  return `${value.year}-${value.month}-${value.day}`;
}

function basisPoints(value?: number | null) {
  return `${new Intl.NumberFormat("en-NG", { maximumFractionDigits: 2 }).format((value ?? 0) / 100)}%`;
}

function number(value?: number | null) {
  return new Intl.NumberFormat("en-NG").format(value ?? 0);
}

type PeriodPreset = "today" | "yesterday" | "7days" | "custom";

function CommandMetric({
  label,
  value,
  detail,
  tone = "neutral",
  icon: Icon,
}: {
  label: string;
  value: ReactNode;
  detail?: string;
  tone?: "neutral" | "info" | "success" | "warning" | "danger";
  icon: LucideIcon;
}) {
  const tones = {
    neutral: "border-l-slate-300",
    info: "border-l-sky-400",
    success: "border-l-emerald-400",
    warning: "border-l-amber-400",
    danger: "border-l-red-400",
  };
  return (
    <div
      className={cn("rounded-lg border border-l-4 bg-white p-3", tones[tone])}
    >
      <div className="flex items-center justify-between gap-2 text-slate-500">
        <span className="text-[11px] font-semibold uppercase tracking-wide">
          {label}
        </span>
        <Icon aria-hidden className="h-4 w-4" />
      </div>
      <strong className="mt-2 block text-xl tabular-nums">{value}</strong>
      {detail ? (
        <span className="mt-1 block text-xs text-slate-500">{detail}</span>
      ) : null}
    </div>
  );
}

export function CommandCentrePage() {
  const { hasPermission } = useAuth();
  const [preset, setPreset] = useState<PeriodPreset>("today");
  const [from, setFrom] = useState(lagosDate());
  const [to, setTo] = useState(lagosDate());
  const [unitId, setUnitId] = useState("");
  const [serviceCode, setServiceCode] = useState("");
  const applyPreset = (value: PeriodPreset) => {
    setPreset(value);
    if (value === "today") {
      setFrom(lagosDate());
      setTo(lagosDate());
    }
    if (value === "yesterday") {
      setFrom(lagosDate(-1));
      setTo(lagosDate(-1));
    }
    if (value === "7days") {
      setFrom(lagosDate(-6));
      setTo(lagosDate());
    }
  };
  const filters = { from, to, unitId, serviceCode };
  const suffix = queryString(filters);
  const overview = useQuery({
    queryKey: ["command-centre", filters],
    queryFn: () =>
      apiRequest<CommandOverview>(`/api/v1/command-centre${suffix}`),
    refetchInterval: 60_000,
  });
  const trend = useQuery({
    queryKey: ["command-centre-trend", from, to, unitId],
    queryFn: () =>
      apiRequest<CommandTrendResponse>(
        `/api/v1/command-centre/trend${queryString({ from, to, unitId })}`,
      ),
  });
  const unitPerformance = useQuery({
    queryKey: ["command-centre-units", from, to, unitId],
    queryFn: () =>
      apiRequest<CommandUnitResponse>(
        `/api/v1/command-centre/units${queryString({ from, to, unitId, limit: 20 })}`,
      ),
  });
  const servicePerformance = useQuery({
    queryKey: ["command-centre-services", from, to, unitId],
    queryFn: () =>
      apiRequest<CommandServiceResponse>(
        `/api/v1/command-centre/services${queryString({ from, to, unitId, limit: 20 })}`,
      ),
  });
  const backlog = useQuery({
    queryKey: ["command-centre-backlog", unitId],
    queryFn: () =>
      apiRequest<CommandBacklogResponse>(
        `/api/v1/command-centre/backlog${queryString({ unitId, limit: 20 })}`,
      ),
    refetchInterval: 60_000,
  });
  const snapshots = useQuery({
    queryKey: ["command-centre-snapshots", unitId],
    queryFn: () =>
      apiRequest<CommandSnapshotResponse>(
        `/api/v1/command-centre/snapshots${queryString({ unitId, hours: 72, limit: 200 })}`,
      ),
  });
  const units = useQuery({
    queryKey: ["operating-units", "command-filter"],
    queryFn: () =>
      apiRequest<OperatingUnitListResponse>(
        "/api/v1/network/operating-units?status=ACTIVE&limit=200&offset=0",
      ),
    enabled: hasPermission("operating_unit.read"),
  });
  const services = useQuery({
    queryKey: ["services", "command-filter"],
    queryFn: () =>
      apiRequest<ServiceListResponse>(
        "/api/v1/courier-services?status=ACTIVE&limit=200&offset=0",
      ),
    enabled: hasPermission("courier_service.read"),
  });
  const firstError =
    overview.error ||
    trend.error ||
    unitPerformance.error ||
    servicePerformance.error ||
    backlog.error ||
    snapshots.error;
  const data = overview.data;
  const period = data?.period;
  const live = data?.live;
  const alerts = data?.alerts;
  const movement = data?.movement;
  const money = data?.money;
  const pickupPending =
    live?.byStatus?.PICKUP_PENDING ?? live?.byStatus?.BOOKED ?? 0;
  const ofd = live?.byStatus?.OUT_FOR_DELIVERY ?? period?.outForDelivery ?? 0;
  const maxTrend = Math.max(
    1,
    ...(trend.data?.data ?? []).flatMap((point) => [
      point.booked ?? 0,
      point.delivered ?? 0,
    ]),
  );
  return (
    <>
      <PageHeader
        eyebrow="Head office · Operations"
        title="Command centre"
        description="Exact period totals, live backlog, and timestamped samples are labelled separately so operational decisions use the right guarantee."
        actions={
          <Button
            onClick={() => {
              void overview.refetch();
              void backlog.refetch();
            }}
          >
            <RotateCcw aria-hidden className="h-4 w-4" /> Refresh live data
          </Button>
        }
      />
      <Panel className="mb-5">
        <FilterBar>
          <div
            className="flex flex-wrap gap-1"
            role="group"
            aria-label="Command centre period"
          >
            {(
              [
                ["today", "Today"],
                ["yesterday", "Yesterday"],
                ["7days", "7 days"],
                ["custom", "Custom"],
              ] as Array<[PeriodPreset, string]>
            ).map(([value, label]) => (
              <Button
                key={value}
                size="sm"
                variant={preset === value ? "primary" : "secondary"}
                onClick={() => applyPreset(value)}
              >
                {label}
              </Button>
            ))}
          </div>
          {preset === "custom" ? (
            <>
              <Input
                aria-label="From date"
                type="date"
                value={from}
                onChange={(event) => setFrom(event.target.value)}
              />
              <Input
                aria-label="To date"
                type="date"
                value={to}
                onChange={(event) => setTo(event.target.value)}
              />
            </>
          ) : null}
          {hasPermission("operating_unit.read") ? (
            <Select
              aria-label="Region hub or branch"
              value={unitId}
              onChange={(event) => setUnitId(event.target.value)}
            >
              <option value="">All permitted units</option>
              {units.data?.data?.map((unit) => (
                <option key={unit.id} value={unit.id}>
                  {unit.name} · {titleCase(unit.unitType ?? "Unit")}
                </option>
              ))}
            </Select>
          ) : null}
          {hasPermission("courier_service.read") ? (
            <Select
              aria-label="Courier service"
              value={serviceCode}
              onChange={(event) => setServiceCode(event.target.value)}
            >
              <option value="">All services</option>
              {services.data?.data?.map((service) => (
                <option key={service.id} value={service.code}>
                  {service.name}
                </option>
              ))}
            </Select>
          ) : null}
        </FilterBar>
      </Panel>
      {overview.isLoading ? (
        <LoadingState label="Loading operational command centre" />
      ) : firstError ? (
        <Panel>
          <ErrorState
            error={firstError}
            retry={() => {
              void overview.refetch();
              void trend.refetch();
              void unitPerformance.refetch();
              void servicePerformance.refetch();
              void backlog.refetch();
              void snapshots.refetch();
            }}
          />
        </Panel>
      ) : (
        <>
          <div className="grid gap-3 sm:grid-cols-2 md:grid-cols-3 xl:grid-cols-9">
            <CommandMetric
              label="Bookings"
              value={number(period?.booked)}
              detail="Exact period"
              icon={PackagePlus}
            />
            <CommandMetric
              label="Pickup pending"
              value={number(pickupPending)}
              detail="Live state"
              tone={pickupPending ? "warning" : "success"}
              icon={Clock3}
            />
            <CommandMetric
              label="In transit"
              value={number(period?.inTransit)}
              detail="Period movement"
              tone="info"
              icon={Truck}
            />
            <CommandMetric
              label="Hub backlog"
              value={number(live?.total)}
              detail="Live custody"
              tone={(live?.total ?? 0) > 0 ? "warning" : "success"}
              icon={Building2}
            />
            <CommandMetric
              label="OFD"
              value={number(ofd)}
              detail="Out for delivery"
              tone="info"
              icon={Truck}
            />
            <CommandMetric
              label="Delivered"
              value={number(period?.delivered)}
              detail={basisPoints(period?.deliveryRateBasisPoints)}
              tone="success"
              icon={PackageCheck}
            />
            <CommandMetric
              label="NDR"
              value={number(period?.ndr)}
              detail={`${basisPoints(period?.ndrRateBasisPoints)} of booked`}
              tone={(period?.ndr ?? 0) ? "warning" : "success"}
              icon={CircleAlert}
            />
            <CommandMetric
              label="RTO"
              value={number(period?.rto)}
              tone={(period?.rto ?? 0) ? "warning" : "neutral"}
              icon={RotateCcw}
            />
            <CommandMetric
              label="SLA breach"
              value={number(period?.slaBreaches)}
              tone={(period?.slaBreaches ?? 0) ? "danger" : "success"}
              icon={AlertTriangle}
            />
          </div>
          <div className="mt-5 grid gap-5 xl:grid-cols-[minmax(0,1.25fr)_minmax(360px,0.75fr)]">
            <Panel>
              <PanelHeader
                title="Bookings and deliveries"
                description={`Exact daily rollup · ${trend.data?.consistency ?? data?.consistency?.periodTotals ?? "server-maintained"}`}
              />
              <div className="w-full min-w-0 max-w-full overflow-x-auto p-4">
                <div
                  className="flex min-w-[560px] items-end gap-2"
                  role="img"
                  aria-label="Daily bookings and deliveries bar chart"
                >
                  {trend.data?.data?.map((point) => (
                    <div
                      key={point.date}
                      className="flex min-w-10 flex-1 flex-col items-center gap-2"
                    >
                      <div className="flex h-40 w-full items-end justify-center gap-1">
                        <div
                          className="w-2/5 rounded-t bg-sky-500"
                          style={{
                            height: `${Math.max(2, ((point.booked ?? 0) / maxTrend) * 100)}%`,
                          }}
                          title={`${point.date}: ${point.booked ?? 0} bookings`}
                        />
                        <div
                          className="w-2/5 rounded-t bg-emerald-500"
                          style={{
                            height: `${Math.max(2, ((point.delivered ?? 0) / maxTrend) * 100)}%`,
                          }}
                          title={`${point.date}: ${point.delivered ?? 0} delivered`}
                        />
                      </div>
                      <span className="text-[10px] text-slate-500">
                        {point.date?.slice(5)}
                      </span>
                    </div>
                  ))}
                </div>
                <div className="mt-3 flex gap-4 text-xs">
                  <span className="flex items-center gap-1.5">
                    <i className="h-2.5 w-2.5 rounded-sm bg-sky-500" /> Booked
                  </span>
                  <span className="flex items-center gap-1.5">
                    <i className="h-2.5 w-2.5 rounded-sm bg-emerald-500" />{" "}
                    Delivered
                  </span>
                </div>
              </div>
            </Panel>
            <Panel>
              <PanelHeader
                title="Action required"
                description="Open the owning workspace to act"
              />
              <div className="divide-y">
                {[
                  {
                    label: "Pickup pending",
                    value: pickupPending,
                    detail: "Review uncollected bookings",
                    to: "/operations/pickups",
                    icon: Clock3,
                    tone: pickupPending ? "warning" : "success",
                  },
                  {
                    label: "SLA breaches",
                    value: alerts?.slaBreached ?? 0,
                    detail: `${alerts?.slaBreachedToday ?? 0} breached today`,
                    to: "/shipments",
                    icon: AlertTriangle,
                    tone: (alerts?.slaBreached ?? 0) ? "danger" : "success",
                  },
                  {
                    label: "Open NDR",
                    value: alerts?.openNdr ?? 0,
                    detail: `${Object.keys(alerts?.ndrByReason ?? {}).length} reason groups`,
                    to: "/operations/ndr",
                    icon: CircleAlert,
                    tone: (alerts?.openNdr ?? 0) ? "warning" : "success",
                  },
                  {
                    label: "Open exceptions",
                    value: alerts?.openExceptions ?? 0,
                    detail: `${alerts?.criticalExceptions ?? 0} critical`,
                    to: "/operations/hub",
                    icon: XCircle,
                    tone:
                      (alerts?.criticalExceptions ?? 0) ? "danger" : "warning",
                  },
                  {
                    label: "COD aged over 48h",
                    value: money?.codAgedOver48h ?? 0,
                    detail: `${number(money?.codObligations)} obligations`,
                    to: "/finance/cod",
                    icon: WalletCards,
                    tone: (money?.codAgedOver48h ?? 0) ? "danger" : "success",
                  },
                ].map((item) => {
                  const Icon = item.icon;
                  return (
                    <Link
                      key={item.label}
                      to={item.to}
                      className="flex items-center justify-between gap-3 px-4 py-3 hover:bg-slate-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary"
                    >
                      <span className="flex items-start gap-3">
                        <Icon
                          aria-hidden
                          className="mt-0.5 h-4 w-4 text-slate-500"
                        />
                        <span>
                          <strong className="block text-sm">
                            {item.label}
                          </strong>
                          <span className="text-xs text-slate-500">
                            {item.detail}
                          </span>
                        </span>
                      </span>
                      <Badge
                        tone={item.tone as "warning" | "success" | "danger"}
                      >
                        {number(item.value)}
                      </Badge>
                    </Link>
                  );
                })}
              </div>
            </Panel>
          </div>
          <div className="mt-5 grid gap-5 xl:grid-cols-2">
            <Panel>
              <PanelHeader
                title="Facility backlog"
                description={`Live · ${backlog.data?.consistency ?? data?.consistency?.liveBacklog ?? "counted now"}`}
              />
              {backlog.data?.data?.length ? (
                <DataTable label="Live facility backlog">
                  <thead>
                    <tr>
                      <TableHead>Facility</TableHead>
                      <TableHead>Code</TableHead>
                      <TableHead className="text-right">Parcels</TableHead>
                    </tr>
                  </thead>
                  <tbody>
                    {backlog.data.data.map((unit) => (
                      <tr key={unit.unitId}>
                        <TableCell>{unit.name}</TableCell>
                        <TableCell className="font-mono">{unit.code}</TableCell>
                        <TableCell className="text-right font-mono tabular-nums">
                          {number(unit.count)}
                        </TableCell>
                      </tr>
                    ))}
                  </tbody>
                </DataTable>
              ) : (
                <EmptyState
                  title="No facility backlog"
                  description="There is no live custody backlog in the selected scope."
                />
              )}
            </Panel>
            <Panel>
              <PanelHeader
                title="Network movement"
                description="Signals that show whether work is moving now"
              />
              <div className="grid gap-px bg-border sm:grid-cols-2">
                <div className="bg-white p-4">
                  <span className="text-xs text-slate-500">
                    Scans last hour
                  </span>
                  <strong className="mt-1 block text-2xl tabular-nums">
                    {number(movement?.scansLastHour)}
                  </strong>
                </div>
                <div className="bg-white p-4">
                  <span className="text-xs text-slate-500">Open bags</span>
                  <strong className="mt-1 block text-2xl tabular-nums">
                    {number(movement?.openBags)}
                  </strong>
                </div>
                <div className="bg-white p-4">
                  <span className="text-xs text-slate-500">
                    Manifests in flight
                  </span>
                  <strong className="mt-1 block text-2xl tabular-nums">
                    {number(movement?.manifestsInFlight)}
                  </strong>
                </div>
                <div className="bg-white p-4">
                  <span className="text-xs text-slate-500">COD in flight</span>
                  <strong className="mt-1 block text-xl">
                    <Money
                      amountMinor={money?.codOutstandingMinor}
                      currency={money?.currency ?? "NGN"}
                    />
                  </strong>
                </div>
              </div>
            </Panel>
          </div>
          <div className="mt-5 grid gap-5 xl:grid-cols-2">
            <PerformanceTable
              title="Facility performance"
              rows={
                unitPerformance.data?.data?.map((item) => ({
                  key: item.unitId ?? item.code ?? "unit",
                  name: item.name ?? item.code ?? "Unit",
                  booked: item.booked,
                  delivered: item.delivered,
                  ndr: item.ndr,
                  sla: item.slaBreaches,
                  rate: item.deliveryRateBasisPoints,
                })) ?? []
              }
            />
            <PerformanceTable
              title="Service performance"
              rows={
                servicePerformance.data?.data?.map((item) => ({
                  key: item.code ?? "service",
                  name: item.name ?? item.code ?? "Service",
                  booked: item.booked,
                  delivered: item.delivered,
                  ndr: item.ndr,
                  sla: item.slaBreaches,
                  rate: item.deliveryRateBasisPoints,
                })) ?? []
              }
            />
          </div>
          <InlineNotice tone="info" title="Data guarantees">
            Period totals are {data?.consistency?.periodTotals ?? "exact"}. Live
            backlog is{" "}
            {data?.consistency?.liveBacklog ?? "counted at request time"}.
            Snapshot history is{" "}
            {snapshots.data?.consistency ??
              data?.consistency?.snapshots ??
              "captured periodically"}
            ; its newest reading is{" "}
            {formatDateTime(snapshots.data?.data?.[0]?.capturedAt)}.
          </InlineNotice>
        </>
      )}
    </>
  );
}

function PerformanceTable({
  title,
  rows,
}: {
  title: string;
  rows: Array<{
    key: string;
    name: string;
    booked?: number;
    delivered?: number;
    ndr?: number;
    sla?: number;
    rate?: number;
  }>;
}) {
  return (
    <Panel>
      <PanelHeader title={title} description="Bounded server-ranked view" />
      {rows.length ? (
        <DataTable label={title}>
          <thead>
            <tr>
              <TableHead>Name</TableHead>
              <TableHead className="text-right">Booked</TableHead>
              <TableHead className="text-right">Delivered</TableHead>
              <TableHead className="text-right">NDR</TableHead>
              <TableHead className="text-right">SLA</TableHead>
              <TableHead className="text-right">Delivery rate</TableHead>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.key}>
                <TableCell>
                  <strong>{row.name}</strong>
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {number(row.booked)}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {number(row.delivered)}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {number(row.ndr)}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {number(row.sla)}
                </TableCell>
                <TableCell className="text-right font-semibold tabular-nums">
                  {basisPoints(row.rate)}
                </TableCell>
              </tr>
            ))}
          </tbody>
        </DataTable>
      ) : (
        <EmptyState
          title={`No ${title.toLowerCase()}`}
          description="No data is available for this period and scope."
        />
      )}
    </Panel>
  );
}

const reportGroups: Array<{
  name: string;
  types: ReportType[];
  icon: LucideIcon;
}> = [
  {
    name: "Operations",
    types: ["SHIPMENT_VOLUME", "BRANCH_PERFORMANCE", "SERVICE_PERFORMANCE"],
    icon: Box,
  },
  { name: "SLA", types: ["SLA"], icon: Clock3 },
  { name: "Franchise", types: ["FRANCHISE_PERFORMANCE"], icon: Building2 },
  { name: "Finance", types: ["REVENUE"], icon: Landmark },
  { name: "COD", types: ["COD_AGING"], icon: WalletCards },
  { name: "Commission", types: ["COMMISSION"], icon: HandCoins },
  { name: "Settlement", types: ["SETTLEMENT"], icon: FileText },
  {
    name: "Exceptions",
    types: ["NDR", "RTO", "EXCEPTIONS"],
    icon: CircleAlert,
  },
];

const reportSchema = z
  .object({
    reportType: z.string().min(1, "Select a report"),
    format: z.enum(["CSV", "JSON"]),
    periodStart: z.string().min(1, "Select a start date"),
    periodEnd: z.string().min(1, "Select an end date"),
    unitId: z.string(),
    status: z.string(),
  })
  .superRefine((value, context) => {
    const start = new Date(`${value.periodStart}T00:00:00Z`);
    const end = new Date(`${value.periodEnd}T00:00:00Z`);
    if (end < start)
      context.addIssue({
        code: "custom",
        path: ["periodEnd"],
        message: "End date must be on or after start date",
      });
    if ((end.getTime() - start.getTime()) / 86_400_000 > 399)
      context.addIssue({
        code: "custom",
        path: ["periodEnd"],
        message: "Reports are limited to 400 days",
      });
  });
type ReportForm = z.infer<typeof reportSchema>;

export function ReportCenterPage() {
  const { hasPermission } = useAuth();
  const queryClient = useQueryClient();
  const { toast } = useToast();
  const [typeFilter, setTypeFilter] = useState("");
  const [statusFilter, setStatusFilter] = useState("");
  const [selectedRun, setSelectedRun] = useState<ReportRun>();
  const [cancelRun, setCancelRun] = useState<ReportRun>();
  const form = useForm<ReportForm>({
    resolver: zodResolver(reportSchema),
    defaultValues: {
      reportType: "SHIPMENT_VOLUME",
      format: "CSV",
      periodStart: lagosDate(-29),
      periodEnd: lagosDate(),
      unitId: "",
      status: "",
    },
  });
  const catalogue = useQuery({
    queryKey: ["report-types"],
    queryFn: () => apiRequest<ReportTypeListResponse>("/api/v1/reports/types"),
  });
  const runs = useQuery({
    queryKey: ["report-runs", typeFilter, statusFilter],
    queryFn: () =>
      apiRequest<ReportListResponse>(
        `/api/v1/reports${queryString({ reportType: typeFilter, status: statusFilter, limit: 200, mine: false })}`,
      ),
    refetchInterval: (query) =>
      query.state.data?.data?.some((item) =>
        ["QUEUED", "RUNNING"].includes(item.status ?? ""),
      )
        ? 5_000
        : false,
  });
  const units = useQuery({
    queryKey: ["operating-units", "report-filter"],
    queryFn: () =>
      apiRequest<OperatingUnitListResponse>(
        "/api/v1/network/operating-units?status=ACTIVE&limit=200&offset=0",
      ),
    enabled: hasPermission("operating_unit.read"),
  });
  const queue = useMutation({
    mutationFn: (values: ReportForm) => {
      const body: QueueReportRequest = {
        reportType: values.reportType as ReportType,
        format: values.format,
        periodStart: values.periodStart,
        periodEnd: values.periodEnd,
        unitId: values.unitId || undefined,
        status: values.status || undefined,
      };
      return apiRequest<QueuedReportResponse>("/api/v1/reports", {
        method: "POST",
        body,
      });
    },
    onSuccess: (receipt) => {
      toast({
        tone: "success",
        title: "Report queued",
        description: "Generation continues in the background.",
      });
      setSelectedRun({
        ...receipt,
        reportType: receipt.reportType as ReportType,
        status: receipt.status as ReportStatus,
      });
      void queryClient.invalidateQueries({ queryKey: ["report-runs"] });
    },
  });
  const cancel = useMutation({
    mutationFn: () =>
      apiRequest(`/api/v1/reports/${cancelRun?.id}/cancel`, { method: "POST" }),
    onSuccess: () => {
      setCancelRun(undefined);
      toast({ tone: "success", title: "Report cancelled" });
      void queryClient.invalidateQueries({ queryKey: ["report-runs"] });
    },
  });
  const download = useMutation({
    mutationFn: async (run: ReportRun) => {
      const blob = await apiRequest<Blob>(
        `/api/v1/reports/${run.id}/download`,
        { responseType: "blob" },
      );
      const href = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = href;
      const stem = safeDownloadName(
        `${(run.reportType ?? "report").toLowerCase()}-${run.id}`,
        "report",
      );
      const requestedExtension = run.format?.toLowerCase() ?? "";
      const extension = ["csv", "xlsx", "pdf"].includes(requestedExtension)
        ? requestedExtension
        : "csv";
      anchor.download = `${stem}.${extension}`;
      anchor.click();
      URL.revokeObjectURL(href);
    },
    onSuccess: () => toast({ tone: "success", title: "Download started" }),
  });
  const allowedTypes = catalogue.data?.data ?? [];
  const reportType = form.watch("reportType") as ReportType;
  const selectedDefinition = allowedTypes.find(
    (item) => item.reportType === reportType,
  );
  const financeBlocked = Boolean(
    selectedDefinition?.requiresFinancePermission &&
    !hasPermission("report.finance"),
  );
  const rows = runs.data?.data ?? [];
  return (
    <>
      <PageHeader
        eyebrow="Reports"
        title="Report centre"
        description="Run bounded exports in the background, monitor honest lifecycle states, and download completed files before they expire."
      />
      <div className="grid gap-5 xl:grid-cols-[minmax(320px,0.72fr)_minmax(0,1.28fr)]">
        <Panel>
          <PanelHeader
            title="Run a report"
            description="Exports never block this browser"
          />
          <form
            className="space-y-4 p-4"
            onSubmit={(event) =>
              void form.handleSubmit((values) => queue.mutate(values))(event)
            }
            noValidate
          >
            <Field
              label="Report"
              htmlFor="report-type"
              required
              error={form.formState.errors.reportType?.message}
            >
              <Select id="report-type" {...form.register("reportType")}>
                {reportGroups.map((group) => (
                  <optgroup key={group.name} label={group.name}>
                    {group.types
                      .filter((type) =>
                        allowedTypes.some((item) => item.reportType === type),
                      )
                      .map((type) => {
                        const definition = allowedTypes.find(
                          (item) => item.reportType === type,
                        );
                        return (
                          <option
                            key={type}
                            value={type}
                            disabled={Boolean(
                              definition?.requiresFinancePermission &&
                              !hasPermission("report.finance"),
                            )}
                          >
                            {titleCase(type)}
                            {definition?.requiresFinancePermission
                              ? " · Finance permission"
                              : ""}
                          </option>
                        );
                      })}
                  </optgroup>
                ))}
              </Select>
            </Field>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field
                label="From"
                htmlFor="report-from"
                required
                error={form.formState.errors.periodStart?.message}
              >
                <Input
                  id="report-from"
                  type="date"
                  {...form.register("periodStart")}
                />
              </Field>
              <Field
                label="To"
                htmlFor="report-to"
                required
                error={form.formState.errors.periodEnd?.message}
              >
                <Input
                  id="report-to"
                  type="date"
                  {...form.register("periodEnd")}
                />
              </Field>
            </div>
            <Field label="Format" htmlFor="report-format">
              <Select id="report-format" {...form.register("format")}>
                <option>CSV</option>
                <option>JSON</option>
              </Select>
            </Field>
            {hasPermission("operating_unit.read") ? (
              <Field
                label="Operating unit"
                htmlFor="report-unit"
                hint="Optional; access scope still applies"
              >
                <Select id="report-unit" {...form.register("unitId")}>
                  <option value="">All permitted units</option>
                  {units.data?.data?.map((unit) => (
                    <option key={unit.id} value={unit.id}>
                      {unit.name}
                    </option>
                  ))}
                </Select>
              </Field>
            ) : null}
            <Field
              label="Shipment status"
              htmlFor="report-status"
              hint="Optional; supported by applicable reports"
            >
              <Input
                id="report-status"
                {...form.register("status")}
                placeholder="For example DELIVERED"
              />
            </Field>
            {financeBlocked ? (
              <InlineNotice tone="warning" title="Finance report unavailable">
                This report contains income or liability data and requires
                report.finance.
              </InlineNotice>
            ) : null}
            <InlineNotice tone="info" title="No browser-side preview">
              The backend creates one authoritative file. A completed run shows
              row count, size, duration, and expiry before download.
            </InlineNotice>
            {queue.error ? <ErrorState error={queue.error} /> : null}
            <Button
              className="w-full"
              type="submit"
              variant="primary"
              loading={queue.isPending}
              disabled={financeBlocked}
            >
              <Play aria-hidden className="h-4 w-4" /> Run in background
            </Button>
          </form>
        </Panel>
        <div className="space-y-5">
          <Panel>
            <PanelHeader
              title="Report catalogue"
              description="Grouped by operational purpose"
            />
            <div className="grid gap-px bg-border sm:grid-cols-2 lg:grid-cols-4">
              {reportGroups.map((group) => {
                const Icon = group.icon;
                const available = group.types.filter((type) =>
                  allowedTypes.some((item) => item.reportType === type),
                );
                return (
                  <div key={group.name} className="bg-white p-3">
                    <div className="flex items-center gap-2">
                      <Icon aria-hidden className="h-4 w-4 text-primary" />
                      <strong className="text-sm">{group.name}</strong>
                    </div>
                    <span className="mt-1 block text-xs text-slate-500">
                      {available.length} available
                    </span>
                  </div>
                );
              })}
            </div>
          </Panel>
          <Panel>
            <PanelHeader
              title="Recent report runs"
              description="Queued and running records refresh every five seconds"
              actions={
                <Button size="sm" onClick={() => void runs.refetch()}>
                  <RotateCcw aria-hidden className="h-4 w-4" /> Refresh
                </Button>
              }
            />
            <FilterBar>
              <Select
                aria-label="Report type filter"
                value={typeFilter}
                onChange={(event) => setTypeFilter(event.target.value)}
              >
                <option value="">All report types</option>
                {allowedTypes.map((item) => (
                  <option key={item.reportType}>{item.reportType}</option>
                ))}
              </Select>
              <Select
                aria-label="Report status filter"
                value={statusFilter}
                onChange={(event) => setStatusFilter(event.target.value)}
              >
                <option value="">All statuses</option>
                <option>QUEUED</option>
                <option>RUNNING</option>
                <option>COMPLETED</option>
                <option>FAILED</option>
                <option>EXPIRED</option>
                <option>CANCELLED</option>
              </Select>
            </FilterBar>
            {runs.isLoading || catalogue.isLoading ? (
              <LoadingState label="Loading report runs" />
            ) : runs.error || catalogue.error ? (
              <ErrorState
                error={runs.error || catalogue.error}
                retry={() => {
                  void runs.refetch();
                  void catalogue.refetch();
                }}
              />
            ) : rows.length ? (
              <DataTable label="Report runs">
                <thead>
                  <tr>
                    <TableHead>Report</TableHead>
                    <TableHead>Period</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead className="text-right">Rows</TableHead>
                    <TableHead>Requested</TableHead>
                    <TableHead>Expires</TableHead>
                    <TableHead>
                      <span className="sr-only">Actions</span>
                    </TableHead>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((run) => (
                    <tr key={run.id}>
                      <TableCell>
                        <strong>{titleCase(run.reportType ?? "Report")}</strong>
                        <span className="block text-xs text-slate-500">
                          {run.format}
                        </span>
                      </TableCell>
                      <TableCell>
                        {run.periodStart} – {run.periodEnd}
                      </TableCell>
                      <TableCell>
                        <ReportRunStatus run={run} />
                      </TableCell>
                      <TableCell className="text-right font-mono tabular-nums">
                        {run.rowCount == null ? "—" : number(run.rowCount)}
                      </TableCell>
                      <TableCell>{formatDateTime(run.createdAt)}</TableCell>
                      <TableCell>{formatDateTime(run.expiresAt)}</TableCell>
                      <TableCell>
                        <div className="flex justify-end gap-1">
                          <Button size="sm" onClick={() => setSelectedRun(run)}>
                            Details
                          </Button>
                          {run.status === "COMPLETED" ? (
                            <Button
                              size="sm"
                              loading={download.isPending}
                              onClick={() => download.mutate(run)}
                            >
                              <Download aria-hidden className="h-4 w-4" />{" "}
                              Download
                            </Button>
                          ) : null}
                          {hasPermission("report.run") &&
                          ["QUEUED", "RUNNING"].includes(run.status ?? "") ? (
                            <Button
                              size="sm"
                              variant="danger"
                              onClick={() => setCancelRun(run)}
                            >
                              Cancel
                            </Button>
                          ) : null}
                        </div>
                      </TableCell>
                    </tr>
                  ))}
                </tbody>
              </DataTable>
            ) : (
              <EmptyState
                icon={FileBarChart}
                title="No report runs"
                description="Choose a report and period to create the first background export."
              />
            )}
          </Panel>
        </div>
      </div>
      <ReportDetailDialog
        run={selectedRun}
        onClose={() => setSelectedRun(undefined)}
        onDownload={(run) => download.mutate(run)}
        downloading={download.isPending}
      />
      <ConfirmAction
        open={Boolean(cancelRun)}
        onOpenChange={(open) => !open && setCancelRun(undefined)}
        title="Cancel this report run?"
        description="Generation stops if the run is still queued or running. Completed files cannot be cancelled."
        confirmLabel="Cancel report"
        loading={cancel.isPending}
        onConfirm={() => cancel.mutate()}
      >
        {cancel.error ? <ErrorState error={cancel.error} /> : null}
      </ConfirmAction>
    </>
  );
}

function ReportRunStatus({ run }: { run: ReportRun }) {
  if (run.status === "RUNNING")
    return (
      <span className="inline-flex items-center gap-2">
        <span className="h-2 w-2 animate-pulse rounded-full bg-sky-500" />
        <Badge tone="info">Running · progress unavailable</Badge>
      </span>
    );
  if (run.status === "QUEUED") return <Badge tone="warning">Queued</Badge>;
  if (run.status === "EXPIRED")
    return <Badge tone="danger">Expired · file deleted</Badge>;
  return <StatusBadge status={run.status} />;
}

function ReportDetailDialog({
  run,
  onClose,
  onDownload,
  downloading,
}: {
  run?: ReportRun;
  onClose: () => void;
  onDownload: (run: ReportRun) => void;
  downloading: boolean;
}) {
  const detail = useQuery({
    queryKey: ["report-run", run?.id],
    queryFn: () => apiRequest<ReportRun>(`/api/v1/reports/${run?.id}`),
    enabled: Boolean(run?.id),
    refetchInterval: (query) =>
      ["QUEUED", "RUNNING"].includes(query.state.data?.status ?? "")
        ? 5_000
        : false,
  });
  const value = detail.data ?? run;
  return (
    <Dialog
      open={Boolean(run)}
      onOpenChange={(open) => !open && onClose()}
      title={titleCase(value?.reportType ?? "Report run")}
      description="Authoritative generation receipt and file lifecycle"
      footer={
        <>
          <Button onClick={onClose}>Close</Button>
          {value?.status === "COMPLETED" ? (
            <Button
              variant="primary"
              loading={downloading}
              onClick={() => value && onDownload(value)}
            >
              <Download aria-hidden className="h-4 w-4" /> Download before
              expiry
            </Button>
          ) : null}
        </>
      }
    >
      {detail.isLoading ? (
        <LoadingState label="Loading report run" />
      ) : detail.error ? (
        <ErrorState error={detail.error} retry={() => void detail.refetch()} />
      ) : (
        <div className="space-y-4">
          <div className="flex items-center justify-between gap-3">
            <ReportRunStatus run={value ?? {}} />
            <span className="font-mono text-xs text-slate-500">
              {value?.id}
            </span>
          </div>
          <dl className="grid gap-4 rounded-md border bg-slate-50 p-4 text-sm sm:grid-cols-2">
            <div>
              <dt className="text-slate-500">Period</dt>
              <dd className="font-semibold">
                {value?.periodStart} – {value?.periodEnd}
              </dd>
            </div>
            <div>
              <dt className="text-slate-500">Format</dt>
              <dd className="font-semibold">{value?.format}</dd>
            </div>
            <div>
              <dt className="text-slate-500">Rows</dt>
              <dd className="font-semibold">
                {value?.rowCount == null
                  ? "Available when complete"
                  : number(value.rowCount)}
              </dd>
            </div>
            <div>
              <dt className="text-slate-500">Size</dt>
              <dd className="font-semibold">
                {value?.byteSize == null
                  ? "Available when complete"
                  : `${number(value.byteSize)} bytes`}
              </dd>
            </div>
            <div>
              <dt className="text-slate-500">Duration</dt>
              <dd>
                {value?.durationMs == null
                  ? "—"
                  : `${number(value.durationMs)} ms`}
              </dd>
            </div>
            <div>
              <dt className="text-slate-500">Expires</dt>
              <dd>{formatDateTime(value?.expiresAt)}</dd>
            </div>
          </dl>
          {value?.status === "RUNNING" ? (
            <InlineNotice tone="info" title="Generation in progress">
              The export streams by cursor and cannot know a truthful percentage
              in advance. You can leave this page.
            </InlineNotice>
          ) : null}
          {value?.status === "FAILED" ? (
            <InlineNotice tone="danger" title="Report failed">
              {value.errorMessage ??
                "The worker could not generate this report."}
            </InlineNotice>
          ) : null}
          {value?.status === "EXPIRED" ? (
            <InlineNotice tone="warning" title="Download window ended">
              The completed file and its signed access have been deleted. Run
              the report again to recreate it.
            </InlineNotice>
          ) : null}
          {value?.status === "COMPLETED" ? (
            <InlineNotice tone="success" title="Ready to download">
              The file is complete. Download links are short-lived and are not
              stored by the browser.
            </InlineNotice>
          ) : null}
        </div>
      )}
    </Dialog>
  );
}
