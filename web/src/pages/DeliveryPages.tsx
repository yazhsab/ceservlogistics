import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Banknote,
  CheckCircle2,
  CircleAlert,
  MapPin,
  Navigation,
  PackageCheck,
  Phone,
  Plus,
  Route,
  SearchX,
  Send,
  Truck,
  UsersRound,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useForm } from "react-hook-form";
import { useNavigate, useParams } from "react-router-dom";
import { z } from "zod";
import {
  apiRequest,
  idempotencyHeaders,
  operationalHeaders,
  queryString,
  type DeliveryAttemptRequest,
  type DeliveryAttemptResult,
  type DeliveryQueue,
  type DeliveryRun,
  type DeliveryRunPage,
  type IssuedOTP,
  type NDRReasonList,
} from "../api/client";
import { useAuth } from "../auth/AuthProvider";
import { useToast } from "../components/ToastProvider";
import { CODCollectionForm } from "../components/CODCollectionForm";
import {
  CursorPager,
  EntityLink,
  MobileActionBar,
  OperationalMetricStrip,
} from "../components/operations";
import {
  Badge,
  Button,
  Checkbox,
  DataTable,
  Dialog,
  EmptyState,
  ErrorState,
  Field,
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
  Textarea,
} from "../components/ui";
import {
  formatDateTime,
  formatMoney,
  formatWeight,
  titleCase,
  toMinorUnits,
} from "../lib/utils";

type QueueView = "READY" | "HELD" | "UNASSIGNED" | "ASSIGNED";

export function DestinationQueuePage() {
  const { hasPermission } = useAuth();
  const [facilityId, setFacilityId] = useState("");
  const [view, setView] = useState<QueueView>("READY");
  const [selected, setSelected] = useState<string[]>([]);
  const [runId, setRunId] = useState("");
  const { toast } = useToast();
  const client = useQueryClient();
  const params =
    view === "READY"
      ? {
          status: "DESTINATION_BRANCH_RECEIVED",
          includeHeld: false,
          excludeAssigned: true,
        }
      : view === "HELD"
        ? { includeHeld: true, excludeAssigned: false }
        : view === "UNASSIGNED"
          ? { includeHeld: false, excludeAssigned: true }
          : { includeHeld: false, excludeAssigned: false };
  const query = useQuery({
    queryKey: ["delivery-queue", { facilityId, view }],
    queryFn: () =>
      apiRequest<DeliveryQueue>(
        `/api/v1/deliveries/queue${queryString({ operatingUnitId: facilityId, limit: 100, ...params })}`,
      ),
    enabled: true,
  });
  const rows = (query.data?.data ?? [])
    .filter((item) => view !== "HELD" || item.isHeld)
    .filter((item) => view !== "ASSIGNED" || item.onActiveRun);
  const assign = useMutation({
    mutationFn: () =>
      apiRequest(`/api/v1/delivery-runs/${runId}/stops`, {
        method: "POST",
        body: { barcodes: selected },
        headers: idempotencyHeaders("delivery-bulk-assign"),
      }),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ["delivery-queue"] });
      void client.invalidateQueries({ queryKey: ["delivery-run", runId] });
      toast({
        tone: "success",
        title: `${selected.length} shipments added to run`,
      });
      setSelected([]);
    },
  });
  return (
    <>
      <PageHeader
        eyebrow="Destination operations"
        title="Destination branch queue"
        description="Prepare branch custody for delivery, expose holds, and add selected shipments to a planned run."
        actions={
          <Input
            className="w-56"
            value={facilityId}
            onChange={(event) => setFacilityId(event.target.value)}
            placeholder="Branch facility ID"
            aria-label="Branch facility ID"
          />
        }
      />
      <div
        className="mb-4 flex gap-1 overflow-x-auto"
        role="tablist"
        aria-label="Destination queue categories"
      >
        {(["READY", "HELD", "UNASSIGNED", "ASSIGNED"] as QueueView[]).map(
          (item) => (
            <Button
              key={item}
              size="sm"
              variant={view === item ? "primary" : "secondary"}
              onClick={() => {
                setView(item);
                setSelected([]);
              }}
            >
              {item === "READY"
                ? "Ready for delivery"
                : item === "HELD"
                  ? "On hold"
                  : titleCase(item)}
            </Button>
          ),
        )}
      </div>
      <OperationalMetricStrip
        items={[
          { label: "Visible", value: rows.length, icon: PackageCheck },
          { label: "Selected", value: selected.length, icon: CheckCircle2 },
          {
            label: "On hold",
            value: rows.filter((item) => item.isHeld).length,
            tone: "warning",
            icon: CircleAlert,
          },
          {
            label: "On active run",
            value: rows.filter((item) => item.onActiveRun).length,
            tone: "info",
            icon: Truck,
          },
          {
            label: "COD",
            value: rows.filter((item) => (item.codAmountMinor ?? 0) > 0).length,
            icon: Banknote,
          },
          {
            label: "NDR context",
            value: rows.filter((item) => item.ndrCaseId).length,
            tone: "warning",
            icon: CircleAlert,
          },
        ]}
      />
      <Panel>
        {selected.length ? (
          <div className="flex flex-wrap items-end justify-between gap-3 border-b border-emerald-200 bg-emerald-50 p-3">
            <div>
              <strong className="text-sm">
                {selected.length} shipments selected
              </strong>
              <p className="text-xs text-slate-600">
                Add them to one planned delivery run.
              </p>
            </div>
            <div className="flex flex-wrap gap-2">
              <Input
                className="w-52"
                value={runId}
                onChange={(event) => setRunId(event.target.value)}
                placeholder="Delivery run ID"
                aria-label="Delivery run ID"
              />
              {hasPermission("delivery.manage") ? (
                <Button
                  variant="primary"
                  disabled={!runId || assign.isPending}
                  onClick={() => assign.mutate()}
                >
                  Add to run
                </Button>
              ) : null}
            </div>
          </div>
        ) : null}
        {query.isLoading ? (
          <LoadingState label="Loading destination queue" />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : rows.length ? (
          <DataTable label="Destination branch queue">
            <thead>
              <tr>
                <TableHead>
                  <span className="sr-only">Select</span>
                </TableHead>
                <TableHead>AWB</TableHead>
                <TableHead>Recipient</TableHead>
                <TableHead>Address</TableHead>
                <TableHead>Promise</TableHead>
                <TableHead>Pieces</TableHead>
                <TableHead>Payment</TableHead>
                <TableHead>Operational state</TableHead>
              </tr>
            </thead>
            <tbody>
              {rows.map((item) => (
                <tr key={item.shipmentId}>
                  <TableCell>
                    <Checkbox
                      aria-label={`Select ${item.awb}`}
                      checked={selected.includes(item.awb ?? "")}
                      onCheckedChange={(checked) =>
                        setSelected((current) =>
                          checked
                            ? [...current, item.awb ?? ""]
                            : current.filter((awb) => awb !== item.awb),
                        )
                      }
                    />
                  </TableCell>
                  <TableCell>
                    <EntityLink
                      to={`/shipments/${item.shipmentId}`}
                      primary={item.awb}
                      secondary={
                        item.movementDirection === "REVERSE"
                          ? "Return"
                          : undefined
                      }
                    />
                  </TableCell>
                  <TableCell>
                    {item.recipientName}
                    <span className="block text-xs text-slate-500">
                      {item.recipientPhone}
                    </span>
                  </TableCell>
                  <TableCell>
                    <span className="block max-w-xs truncate">
                      {item.line1}
                    </span>
                    <span className="text-xs text-slate-500">
                      {item.city} · {item.pincode}
                    </span>
                  </TableCell>
                  <TableCell>
                    {formatDateTime(item.promisedDeliveryAt)}
                  </TableCell>
                  <TableCell>
                    {item.pieceCount}
                    <span className="block text-xs text-slate-500">
                      {formatWeight(item.chargeableWeightGrams)}
                    </span>
                  </TableCell>
                  <TableCell>
                    {item.paymentMode === "COD" ? (
                      <strong>
                        {formatMoney(item.codAmountMinor, item.currency)}
                      </strong>
                    ) : (
                      titleCase(item.paymentMode ?? "")
                    )}
                  </TableCell>
                  <TableCell>
                    {item.isHeld ? (
                      <Badge tone="danger">Held: {item.holdReason}</Badge>
                    ) : item.onActiveRun ? (
                      <Badge tone="info">Assigned</Badge>
                    ) : (
                      <StatusBadge status={item.status} />
                    )}
                    {item.ndrAction ? (
                      <span className="mt-1 block text-xs text-amber-700">
                        NDR: {titleCase(item.ndrAction)}
                      </span>
                    ) : null}
                  </TableCell>
                </tr>
              ))}
            </tbody>
          </DataTable>
        ) : (
          <EmptyState
            icon={SearchX}
            title="Queue is clear"
            description="No shipments match this destination work view."
          />
        )}
      </Panel>
    </>
  );
}

export function DeliveryDispatcherPage() {
  const { hasPermission } = useAuth();
  const [status, setStatus] = useState("");
  const [runDate, setRunDate] = useState(new Date().toISOString().slice(0, 10));
  const [cursorStack, setCursorStack] = useState<(string | undefined)[]>([
    undefined,
  ]);
  const [createOpen, setCreateOpen] = useState(false);
  const cursor = cursorStack.at(-1);
  const query = useQuery({
    queryKey: ["delivery-runs", { cursor, status, runDate }],
    queryFn: () =>
      apiRequest<DeliveryRunPage>(
        `/api/v1/delivery-runs${queryString({ cursor, limit: 25, status, runDate })}`,
      ),
    placeholderData: (previous) => previous,
  });
  const rows = useMemo(() => query.data?.data ?? [], [query.data?.data]);
  const agents = useMemo(() => {
    const map = new Map<
      string,
      {
        name: string;
        runs: number;
        stops: number;
        delivered: number;
        failed: number;
      }
    >();
    for (const run of rows) {
      const key = run.agent?.id ?? run.agent?.code ?? "unassigned";
      const current = map.get(key) ?? {
        name: run.agent?.name ?? run.agent?.code ?? "Unassigned",
        runs: 0,
        stops: 0,
        delivered: 0,
        failed: 0,
      };
      current.runs += 1;
      current.stops += run.plannedStops ?? 0;
      current.delivered += run.deliveredCount ?? 0;
      current.failed += run.failedCount ?? 0;
      map.set(key, current);
    }
    return [...map.entries()];
  }, [rows]);
  return (
    <>
      <PageHeader
        eyebrow="Operations · Delivery"
        title="Delivery dispatcher"
        description="Build runs, monitor agent progress, and make OFD state changes explicit."
        actions={
          <>
            {hasPermission("delivery.read") ? (
              <Button
                onClick={() => location.assign("/operations/destination")}
              >
                Destination queue
              </Button>
            ) : null}
            {hasPermission("delivery.manage") ? (
              <Button variant="primary" onClick={() => setCreateOpen(true)}>
                <Plus aria-hidden className="h-4 w-4" /> Create run
              </Button>
            ) : null}
          </>
        }
      />
      <OperationalMetricStrip
        items={[
          { label: "Visible runs", value: rows.length, icon: Route },
          {
            label: "Planned stops",
            value: rows.reduce(
              (sum, item) => sum + (item.plannedStops ?? 0),
              0,
            ),
            icon: MapPin,
          },
          {
            label: "Delivered",
            value: rows.reduce(
              (sum, item) => sum + (item.deliveredCount ?? 0),
              0,
            ),
            tone: "success",
            icon: CheckCircle2,
          },
          {
            label: "Failed",
            value: rows.reduce((sum, item) => sum + (item.failedCount ?? 0), 0),
            tone: "warning",
            icon: CircleAlert,
          },
          {
            label: "OFD runs",
            value: rows.filter((item) =>
              ["DISPATCHED", "IN_PROGRESS"].includes(item.status ?? ""),
            ).length,
            tone: "info",
            icon: Truck,
          },
          {
            label: "Agents",
            value: agents.filter(([key]) => key !== "unassigned").length,
            icon: UsersRound,
          },
        ]}
      />
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.5fr)_minmax(280px,0.5fr)]">
        <Panel>
          <div className="grid gap-3 border-b p-4 sm:grid-cols-2">
            <Select
              value={status}
              onChange={(event) => {
                setStatus(event.target.value);
                setCursorStack([undefined]);
              }}
            >
              <option value="">All run states</option>
              <option>PLANNED</option>
              <option>ASSIGNED</option>
              <option>DISPATCHED</option>
              <option>IN_PROGRESS</option>
              <option>COMPLETED</option>
              <option>CLOSED</option>
            </Select>
            <Input
              type="date"
              value={runDate}
              onChange={(event) => {
                setRunDate(event.target.value);
                setCursorStack([undefined]);
              }}
            />
          </div>
          {query.isLoading ? (
            <LoadingState label="Loading delivery runs" />
          ) : query.error ? (
            <ErrorState
              error={query.error}
              retry={() => void query.refetch()}
            />
          ) : rows.length ? (
            <>
              <DataTable label="Delivery runs">
                <thead>
                  <tr>
                    <TableHead>Run</TableHead>
                    <TableHead>Agent</TableHead>
                    <TableHead>Branch</TableHead>
                    <TableHead>Progress</TableHead>
                    <TableHead>Delivered</TableHead>
                    <TableHead>Failed</TableHead>
                    <TableHead>COD</TableHead>
                    <TableHead>Status</TableHead>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((run) => (
                    <tr key={run.id}>
                      <TableCell>
                        <EntityLink
                          to={`/operations/delivery/runs/${run.id}`}
                          primary={run.runCode}
                          secondary={run.runDate}
                        />
                      </TableCell>
                      <TableCell>
                        {run.agent?.name ?? run.agent?.code ?? "Unassigned"}
                      </TableCell>
                      <TableCell>{run.branch?.code}</TableCell>
                      <TableCell>
                        {run.completedStops ?? 0} / {run.plannedStops ?? 0}
                      </TableCell>
                      <TableCell className="text-emerald-700">
                        {run.deliveredCount ?? 0}
                      </TableCell>
                      <TableCell className="text-amber-700">
                        {run.failedCount ?? 0}
                      </TableCell>
                      <TableCell>
                        {formatMoney(run.codCollectedMinor, run.currency)}
                        <span className="block text-xs text-slate-500">
                          of {formatMoney(run.codExpectedMinor, run.currency)}
                        </span>
                      </TableCell>
                      <TableCell>
                        <StatusBadge status={run.status} />
                      </TableCell>
                    </tr>
                  ))}
                </tbody>
              </DataTable>
              <CursorPager
                page={cursorStack.length}
                count={rows.length}
                noun="runs"
                hasMore={query.data?.pagination?.hasMore}
                nextCursor={query.data?.pagination?.nextCursor}
                onPrevious={() => setCursorStack((items) => items.slice(0, -1))}
                onNext={(next) => setCursorStack((items) => [...items, next])}
              />
            </>
          ) : (
            <EmptyState
              title="No delivery runs"
              description="Create a run or change the date and status filters."
            />
          )}
        </Panel>
        <Panel>
          <PanelHeader
            title="Agent workload"
            description="Aggregated from visible runs."
          />
          {agents.length ? (
            <div className="divide-y">
              {agents.map(([key, item]) => (
                <div key={key} className="p-4">
                  <div className="flex items-center justify-between">
                    <strong className="text-sm">{item.name}</strong>
                    <Badge tone={key === "unassigned" ? "warning" : "neutral"}>
                      {item.runs} run{item.runs === 1 ? "" : "s"}
                    </Badge>
                  </div>
                  <p className="mt-2 text-xs text-slate-500">
                    {item.delivered} delivered · {item.failed} failed ·{" "}
                    {item.stops} stops
                  </p>
                </div>
              ))}
            </div>
          ) : (
            <EmptyState
              title="No agent workload"
              description="Agents appear as runs are planned."
            />
          )}
        </Panel>
      </div>
      <CreateDeliveryRunDialog open={createOpen} onOpenChange={setCreateOpen} />
    </>
  );
}

const createRunSchema = z.object({
  branchId: z.string().optional(),
  agentId: z.string().min(1),
  runDate: z.string().min(1),
  vehicleReference: z.string().optional(),
  barcodes: z.string().optional(),
});
function CreateDeliveryRunDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const navigate = useNavigate();
  const client = useQueryClient();
  const { toast } = useToast();
  const form = useForm<z.infer<typeof createRunSchema>>({
    resolver: zodResolver(createRunSchema),
    defaultValues: { runDate: new Date().toISOString().slice(0, 10) },
  });
  const mutation = useMutation({
    mutationFn: (values: z.infer<typeof createRunSchema>) =>
      apiRequest<DeliveryRun>("/api/v1/delivery-runs", {
        method: "POST",
        body: {
          ...values,
          barcodes: values.barcodes?.split(/[\s,]+/).filter(Boolean),
        },
      }),
    onSuccess: (run) => {
      void client.invalidateQueries({ queryKey: ["delivery-runs"] });
      toast({
        tone: "success",
        title: "Delivery run created",
        description: run.runCode,
      });
      onOpenChange(false);
      if (run.id) void navigate(`/operations/delivery/runs/${run.id}`);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Create delivery run"
      description="Plan one agent's ordered branch stops for a day."
      footer={
        <>
          <Button onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button
            variant="primary"
            onClick={() =>
              void form.handleSubmit((values) => mutation.mutate(values))()
            }
          >
            Create run
          </Button>
        </>
      }
    >
      <div className="grid gap-4 sm:grid-cols-2">
        <Field
          label="Branch ID"
          htmlFor="delivery-branch"
          hint="Optional for a single-branch role"
        >
          <Input id="delivery-branch" {...form.register("branchId")} />
        </Field>
        <Field label="Agent user ID" htmlFor="delivery-agent" required>
          <Input id="delivery-agent" {...form.register("agentId")} />
        </Field>
        <Field label="Run date" htmlFor="delivery-date" required>
          <Input id="delivery-date" type="date" {...form.register("runDate")} />
        </Field>
        <Field label="Vehicle reference" htmlFor="delivery-vehicle">
          <Input id="delivery-vehicle" {...form.register("vehicleReference")} />
        </Field>
        <Field
          className="sm:col-span-2"
          label="Initial AWBs"
          htmlFor="delivery-awbs"
          hint="Optional. Separate with commas or spaces."
        >
          <Textarea id="delivery-awbs" {...form.register("barcodes")} />
        </Field>
      </div>
      {mutation.error ? <ErrorState error={mutation.error} /> : null}
    </Dialog>
  );
}

export function DeliveryRunDetailPage() {
  const { runId = "" } = useParams();
  const { hasPermission } = useAuth();
  const client = useQueryClient();
  const { toast } = useToast();
  const [addOpen, setAddOpen] = useState(false);
  const [assignOpen, setAssignOpen] = useState(false);
  const query = useQuery({
    queryKey: ["delivery-run", runId],
    queryFn: () => apiRequest<DeliveryRun>(`/api/v1/delivery-runs/${runId}`),
    enabled: Boolean(runId),
    refetchInterval: 30_000,
  });
  const dispatch = useMutation({
    mutationFn: () =>
      apiRequest<DeliveryRun>(`/api/v1/delivery-runs/${runId}/dispatch`, {
        method: "POST",
        body: {},
        headers: operationalHeaders("delivery-dispatch"),
      }),
    onSuccess: (run) => {
      void client.invalidateQueries({ queryKey: ["delivery-run", runId] });
      toast({
        tone: "success",
        title: "Run dispatched",
        description: `${run.runCode}: every stop is now out for delivery.`,
      });
    },
  });
  if (query.isLoading) return <LoadingState label="Loading delivery run" />;
  if (query.error)
    return (
      <ErrorState error={query.error} retry={() => void query.refetch()} />
    );
  const run = query.data;
  if (!run)
    return (
      <EmptyState
        title="Run not found"
        description="Check the run identifier."
      />
    );
  const transitions = new Set(run.allowedTransitions ?? []);
  return (
    <>
      <PageHeader
        eyebrow="Operations · Delivery"
        title={run.runCode ?? "Delivery run"}
        description={`${run.agent?.name ?? "Unassigned agent"} · ${run.runDate} · ${run.branch?.name ?? run.branch?.code}`}
        actions={<StatusBadge status={run.status} />}
      />
      <OperationalMetricStrip
        items={[
          { label: "Planned stops", value: run.plannedStops, icon: MapPin },
          { label: "Completed", value: run.completedStops, icon: CheckCircle2 },
          {
            label: "Delivered",
            value: run.deliveredCount,
            tone: "success",
            icon: PackageCheck,
          },
          {
            label: "Failed",
            value: run.failedCount,
            tone: "warning",
            icon: CircleAlert,
          },
          {
            label: "COD expected",
            value: formatMoney(run.codExpectedMinor, run.currency),
            icon: Banknote,
          },
          {
            label: "COD collected",
            value: formatMoney(run.codCollectedMinor, run.currency),
            tone: "success",
            icon: Banknote,
          },
        ]}
      />
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.5fr)_minmax(300px,0.5fr)]">
        <Panel>
          <PanelHeader
            title="Stops"
            description="Ordered exactly as supplied; route optimisation is outside Release 2."
            actions={
              hasPermission("delivery.manage") &&
              ["PLANNED", "ASSIGNED"].includes(run.status ?? "") ? (
                <Button size="sm" onClick={() => setAddOpen(true)}>
                  Add stops
                </Button>
              ) : undefined
            }
          />
          {run.stops?.length ? (
            <DataTable label="Delivery stops">
              <thead>
                <tr>
                  <TableHead>Stop</TableHead>
                  <TableHead>AWB</TableHead>
                  <TableHead>Recipient</TableHead>
                  <TableHead>Address</TableHead>
                  <TableHead>COD</TableHead>
                  <TableHead>Promise</TableHead>
                  <TableHead>Status</TableHead>
                </tr>
              </thead>
              <tbody>
                {run.stops.map((stop) => (
                  <tr key={stop.id}>
                    <TableCell>{stop.stopSequence}</TableCell>
                    <TableCell>
                      <EntityLink
                        to={`/operations/delivery/runs/${runId}/stops/${stop.awb}`}
                        primary={stop.awb}
                        secondary={`${stop.pieceCount} pcs`}
                      />
                    </TableCell>
                    <TableCell>
                      {stop.recipientName}
                      <span className="block text-xs text-slate-500">
                        {stop.recipientPhone}
                      </span>
                    </TableCell>
                    <TableCell>
                      <span className="block max-w-xs truncate">
                        {stop.line1}
                      </span>
                      <span className="text-xs text-slate-500">
                        {stop.city} · {stop.pincode}
                      </span>
                    </TableCell>
                    <TableCell>
                      {formatMoney(stop.codAmountMinor, stop.currency)}
                    </TableCell>
                    <TableCell>
                      {formatDateTime(stop.promisedDeliveryAt)}
                    </TableCell>
                    <TableCell>
                      <StatusBadge status={stop.status} />
                    </TableCell>
                  </tr>
                ))}
              </tbody>
            </DataTable>
          ) : (
            <EmptyState
              title="Run has no stops"
              description="Add shipments from the destination queue before dispatch."
            />
          )}
        </Panel>
        <div className="space-y-4">
          <Panel>
            <PanelHeader
              title="Run control"
              description="Dispatch is the only action that makes shipments OFD."
            />
            <div className="space-y-2 p-4">
              {hasPermission("delivery.assign") &&
              ["PLANNED", "ASSIGNED"].includes(run.status ?? "") ? (
                <Button className="w-full" onClick={() => setAssignOpen(true)}>
                  Assign agent
                </Button>
              ) : null}
              {transitions.has("DISPATCHED") &&
              hasPermission("delivery.dispatch") ? (
                <Button
                  className="w-full"
                  variant="primary"
                  disabled={dispatch.isPending || !run.stops?.length}
                  onClick={() => dispatch.mutate()}
                >
                  <Send aria-hidden className="h-4 w-4" /> Dispatch run
                </Button>
              ) : null}
              {dispatch.error ? <ErrorState error={dispatch.error} /> : null}
            </div>
          </Panel>
          <Panel>
            <PanelHeader title="Agent and vehicle" />
            <dl className="space-y-3 p-4 text-sm">
              <div>
                <dt className="text-slate-500">Agent</dt>
                <dd className="font-semibold">
                  {run.agent?.name ?? run.agent?.code ?? "Unassigned"}
                </dd>
              </div>
              <div>
                <dt className="text-slate-500">Vehicle</dt>
                <dd className="font-semibold">
                  {run.vehicleReference || "Not recorded"}
                </dd>
              </div>
              <div>
                <dt className="text-slate-500">Dispatched</dt>
                <dd>{formatDateTime(run.dispatchedAt)}</dd>
              </div>
            </dl>
          </Panel>
        </div>
      </div>
      <AddStopsDialog runId={runId} open={addOpen} onOpenChange={setAddOpen} />
      <AssignDeliveryRunDialog
        runId={runId}
        open={assignOpen}
        onOpenChange={setAssignOpen}
      />
    </>
  );
}

const addStopsSchema = z.object({ barcodes: z.string().min(1) });
function AddStopsDialog({
  runId,
  open,
  onOpenChange,
}: {
  runId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const client = useQueryClient();
  const { toast } = useToast();
  const form = useForm<z.infer<typeof addStopsSchema>>({
    resolver: zodResolver(addStopsSchema),
  });
  const mutation = useMutation({
    mutationFn: (values: z.infer<typeof addStopsSchema>) =>
      apiRequest<{
        run?: DeliveryRun;
        results?: Array<{ outcome?: string; message?: string }>;
      }>(`/api/v1/delivery-runs/${runId}/stops`, {
        method: "POST",
        body: { barcodes: values.barcodes.split(/[\s,]+/).filter(Boolean) },
      }),
    onSuccess: (result) => {
      void client.invalidateQueries({ queryKey: ["delivery-run", runId] });
      const rejected =
        result.results?.filter((item) => item.outcome === "REJECTED").length ??
        0;
      toast({
        tone: rejected ? "info" : "success",
        title: "Stops processed",
        description: rejected
          ? `${rejected} shipments were rejected; inspect the server result before dispatch.`
          : undefined,
      });
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Add delivery stops"
      description="Add eligible destination-branch shipments by AWB or piece barcode."
      footer={
        <>
          <Button onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button
            variant="primary"
            onClick={() =>
              void form.handleSubmit((values) => mutation.mutate(values))()
            }
          >
            Add stops
          </Button>
        </>
      }
    >
      <Field label="AWBs or barcodes" htmlFor="stop-barcodes" required>
        <Textarea
          id="stop-barcodes"
          {...form.register("barcodes")}
          placeholder="One per line or separated by commas"
        />
      </Field>
      {mutation.error ? <ErrorState error={mutation.error} /> : null}
    </Dialog>
  );
}
const assignRunSchema = z.object({ agentId: z.string().min(1) });
function AssignDeliveryRunDialog({
  runId,
  open,
  onOpenChange,
}: {
  runId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const client = useQueryClient();
  const { toast } = useToast();
  const form = useForm<z.infer<typeof assignRunSchema>>({
    resolver: zodResolver(assignRunSchema),
  });
  const mutation = useMutation({
    mutationFn: (body: z.infer<typeof assignRunSchema>) =>
      apiRequest<DeliveryRun>(`/api/v1/delivery-runs/${runId}/assign`, {
        method: "POST",
        body,
      }),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ["delivery-run", runId] });
      toast({ tone: "success", title: "Delivery agent assigned" });
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Assign delivery agent"
      description="A shipment can belong to only one live run."
      footer={
        <>
          <Button onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button
            variant="primary"
            onClick={() =>
              void form.handleSubmit((values) => mutation.mutate(values))()
            }
          >
            Assign agent
          </Button>
        </>
      }
    >
      <Field label="Agent user ID" htmlFor="delivery-assign-agent" required>
        <Input id="delivery-assign-agent" {...form.register("agentId")} />
      </Field>
      {mutation.error ? <ErrorState error={mutation.error} /> : null}
    </Dialog>
  );
}

export function DeliveryMyRunPage() {
  const navigate = useNavigate();
  const [date, setDate] = useState(new Date().toISOString().slice(0, 10));
  const query = useQuery({
    queryKey: ["delivery-runs", "mine", date],
    queryFn: () =>
      apiRequest<DeliveryRunPage>(
        `/api/v1/delivery-runs${queryString({ mine: true, runDate: date, limit: 10 })}`,
      ),
  });
  const runSummary =
    query.data?.data?.find((item) =>
      ["DISPATCHED", "IN_PROGRESS", "ASSIGNED", "PLANNED"].includes(
        item.status ?? "",
      ),
    ) ?? query.data?.data?.[0];
  const detail = useQuery({
    queryKey: ["delivery-run", runSummary?.id],
    queryFn: () =>
      apiRequest<DeliveryRun>(`/api/v1/delivery-runs/${runSummary?.id}`),
    enabled: Boolean(runSummary?.id),
    refetchInterval: 30_000,
  });
  return (
    <div className="mx-auto max-w-3xl">
      <PageHeader
        eyebrow="Field · Delivery"
        title="My delivery run"
        description="Stops, contact, navigation, COD, OTP, POD, and failure actions in one mobile-first route."
        actions={
          <Input
            className="w-40"
            type="date"
            value={date}
            onChange={(event) => setDate(event.target.value)}
          />
        }
      />
      {query.isLoading || detail.isLoading ? (
        <Panel>
          <LoadingState label="Loading your run" />
        </Panel>
      ) : query.error || detail.error ? (
        <Panel>
          <ErrorState
            error={query.error ?? detail.error}
            retry={() => {
              void query.refetch();
              void detail.refetch();
            }}
          />
        </Panel>
      ) : !detail.data ? (
        <Panel>
          <EmptyState
            icon={CheckCircle2}
            title="No active run"
            description="Your dispatcher has not assigned a delivery run for this date."
          />
        </Panel>
      ) : (
        <>
          <div className="mb-4 rounded-lg bg-emerald-950 p-5 text-white">
            <div className="flex items-start justify-between gap-3">
              <div>
                <p className="text-xs font-semibold uppercase tracking-wider text-emerald-200">
                  {detail.data.runCode}
                </p>
                <h2 className="mt-1 text-xl font-bold">
                  {detail.data.completedStops ?? 0} of{" "}
                  {detail.data.plannedStops ?? 0} stops complete
                </h2>
              </div>
              <StatusBadge status={detail.data.status} />
            </div>
            <div className="mt-4 h-2 overflow-hidden rounded-full bg-white/15">
              <div
                className="h-full bg-[#d8f25a]"
                style={{
                  width: `${Math.min(100, ((detail.data.completedStops ?? 0) / Math.max(detail.data.plannedStops ?? 1, 1)) * 100)}%`,
                }}
              />
            </div>
          </div>
          <Panel>
            {detail.data.stops?.length ? (
              <div className="divide-y">
                {detail.data.stops.map((stop) => (
                  <button
                    key={stop.id}
                    onClick={() =>
                      void navigate(
                        `/operations/delivery/runs/${detail.data?.id}/stops/${stop.awb}`,
                      )
                    }
                    className="flex w-full items-start gap-3 p-4 text-left hover:bg-slate-50"
                  >
                    <span className="grid h-9 w-9 shrink-0 place-items-center rounded-full bg-slate-100 text-sm font-bold">
                      {stop.stopSequence}
                    </span>
                    <span className="min-w-0 flex-1">
                      <strong className="block font-mono text-sm">
                        {stop.awb}
                      </strong>
                      <span className="mt-1 block truncate text-sm">
                        {stop.recipientName} · {stop.line1}
                      </span>
                      <span className="mt-1 block text-xs text-slate-500">
                        {stop.city} {stop.pincode} · {stop.pieceCount} pcs
                        {(stop.codAmountMinor ?? 0) > 0
                          ? ` · COD ${formatMoney(stop.codAmountMinor, stop.currency)}`
                          : ""}
                      </span>
                    </span>
                    <StatusBadge status={stop.status} />
                  </button>
                ))}
              </div>
            ) : (
              <EmptyState
                title="No stops"
                description="This run has no assigned shipments."
              />
            )}
          </Panel>
        </>
      )}
    </div>
  );
}

const attemptSchema = z
  .object({
    outcome: z.enum(["DELIVERED", "FAILED", "RESCHEDULED", "CANCELLED"]),
    recipientName: z.string().optional(),
    recipientRelationship: z
      .enum([
        "SELF",
        "FAMILY",
        "NEIGHBOUR",
        "SECURITY",
        "RECEPTION",
        "COLLEAGUE",
        "OTHER",
      ])
      .optional(),
    recipientPhone: z.string().optional(),
    otp: z.string().optional(),
    codCollected: z.string().optional(),
    codPaymentMode: z
      .enum(["CASH", "UPI", "CARD", "WALLET", "BANK_TRANSFER", "CHEQUE"])
      .optional(),
    codReference: z.string().optional(),
    failureReasonCode: z.string().optional(),
    remarks: z.string().optional(),
    nextAttemptAt: z.string().optional(),
  })
  .superRefine((value, context) => {
    if (value.outcome === "DELIVERED" && !value.recipientName?.trim())
      context.addIssue({
        code: "custom",
        path: ["recipientName"],
        message: "Recipient name is required",
      });
    if (
      ["FAILED", "RESCHEDULED"].includes(value.outcome) &&
      !value.failureReasonCode
    )
      context.addIssue({
        code: "custom",
        path: ["failureReasonCode"],
        message: "Failure reason is required",
      });
  });

export function DeliveryStopPage() {
  const { runId = "", awb = "" } = useParams();
  const navigate = useNavigate();
  const { hasPermission } = useAuth();
  const { toast } = useToast();
  const client = useQueryClient();
  const [facilityId, setFacilityId] = useState("");
  const [issuedOTP, setIssuedOTP] = useState<IssuedOTP>();
  const [result, setResult] = useState<DeliveryAttemptResult>();
  const run = useQuery({
    queryKey: ["delivery-run", runId],
    queryFn: () => apiRequest<DeliveryRun>(`/api/v1/delivery-runs/${runId}`),
    enabled: Boolean(runId),
  });
  const reasons = useQuery({
    queryKey: ["ndr-reasons"],
    queryFn: () => apiRequest<NDRReasonList>("/api/v1/ndr/reasons?limit=100"),
    staleTime: 300_000,
  });
  const stop = run.data?.stops?.find((item) => item.awb === awb);
  const form = useForm<z.infer<typeof attemptSchema>>({
    resolver: zodResolver(attemptSchema),
    defaultValues: {
      outcome: "DELIVERED",
      recipientRelationship: "SELF",
      codCollected: stop?.codAmountMinor
        ? String(stop.codAmountMinor / 100)
        : "",
      codPaymentMode: "CASH",
    },
  });
  useEffect(() => {
    if (stop?.codAmountMinor)
      form.setValue("codCollected", String(stop.codAmountMinor / 100));
  }, [form, stop?.codAmountMinor]);
  const otp = useMutation({
    mutationFn: () =>
      apiRequest<IssuedOTP>("/api/v1/deliveries/otp", {
        method: "POST",
        body: { barcode: awb },
      }),
    onSuccess: (data) => {
      setIssuedOTP(data);
      toast({
        tone: "info",
        title: "OTP issued",
        description: `Shown once; expires ${formatDateTime(data.expiresAt)}.`,
      });
    },
  });
  const attempt = useMutation({
    mutationFn: (values: z.infer<typeof attemptSchema>) => {
      const body: DeliveryAttemptRequest = {
        barcode: awb,
        outcome: values.outcome,
        runId,
        recipientName: values.recipientName,
        recipientRelationship: values.recipientRelationship,
        recipientPhone: values.recipientPhone,
        otp: values.otp,
        codCollectedMinor: toMinorUnits(values.codCollected),
        codPaymentMode: values.codPaymentMode,
        codReference: values.codReference,
        failureReasonCode: values.failureReasonCode,
        remarks: values.remarks,
        nextAttemptAt: values.nextAttemptAt
          ? new Date(values.nextAttemptAt).toISOString()
          : undefined,
        occurredAt: new Date().toISOString(),
      };
      return apiRequest<DeliveryAttemptResult>(
        `/api/v1/deliveries/attempts${queryString({ operatingUnitId: facilityId })}`,
        {
          method: "POST",
          body,
          headers: {
            ...operationalHeaders("delivery-attempt"),
            ...idempotencyHeaders("delivery-attempt"),
          },
        },
      );
    },
    onSuccess: (data) => {
      setResult(data);
      setIssuedOTP(undefined);
      void client.invalidateQueries({ queryKey: ["delivery-run", runId] });
      toast({
        tone: data.outcome === "DELIVERED" ? "success" : "info",
        title:
          data.outcome === "DELIVERED"
            ? "Delivery completed"
            : "Delivery exception recorded",
        description: data.nextAction,
      });
    },
  });
  if (run.isLoading) return <LoadingState label="Loading stop" />;
  if (run.error)
    return <ErrorState error={run.error} retry={() => void run.refetch()} />;
  if (!stop)
    return (
      <EmptyState
        title="Stop not found"
        description="This AWB is not on the selected run."
      />
    );
  if (!hasPermission("delivery.complete"))
    return (
      <>
        <PageHeader
          eyebrow={`Stop ${stop.stopSequence} · ${run.data?.runCode}`}
          title={stop.recipientName ?? stop.awb ?? "Delivery stop"}
          description={`${stop.awb} · ${stop.pieceCount} pieces`}
          actions={<StatusBadge status={stop.status} />}
        />
        <Panel>
          <div className="p-5">
            <InlineNotice tone="info" title="Read-only delivery view">
              You can inspect this stop, but only the holding delivery agent or
              an authorized supervisor can record an attempt.
            </InlineNotice>
            <address className="mt-5 text-sm not-italic leading-6">
              {stop.line1}
              <br />
              {stop.line2 ? (
                <>
                  {stop.line2}
                  <br />
                </>
              ) : null}
              {stop.city} {stop.pincode}
            </address>
          </div>
        </Panel>
      </>
    );
  const delivered =
    result?.outcome === "DELIVERED" || stop.status === "DELIVERED";
  if (result || delivered)
    return (
      <div className="mx-auto max-w-2xl">
        <Panel className="overflow-hidden">
          <div
            className={`p-6 text-center ${delivered ? "bg-emerald-950 text-white" : "bg-amber-50 text-amber-950"}`}
          >
            <CheckCircle2 aria-hidden className="mx-auto h-12 w-12" />
            <h1 className="mt-3 text-2xl font-bold">
              {delivered ? "Delivery completed" : "Attempt recorded"}
            </h1>
            <p className="mt-1 font-mono">{result?.awb ?? stop.awb}</p>
          </div>
          <div className="space-y-3 p-5">
            <p className="text-sm text-slate-600">
              {result?.nextAction ??
                "Delivery has already been recorded. You can submit proof of delivery below."}
            </p>
            {delivered &&
            (result?.codCollectedMinor ?? stop.codCollectedMinor ?? 0) > 0 &&
            stop.shipmentId &&
            hasPermission("cod.collect") ? (
              <CODCollectionForm
                shipmentId={stop.shipmentId}
                amountMinor={
                  result?.codCollectedMinor ?? stop.codCollectedMinor ?? 0
                }
                currency={result?.currency ?? stop.currency}
              />
            ) : null}
            {result?.ndrCaseId ? (
              <Button
                className="w-full"
                onClick={() =>
                  void navigate(`/operations/ndr/${result.ndrCaseId}`)
                }
              >
                Open NDR case
              </Button>
            ) : null}
            {delivered ? (
              <Button
                className="w-full"
                variant="primary"
                onClick={() =>
                  void navigate(
                    `/operations/pod/new?awb=${encodeURIComponent(awb)}`,
                  )
                }
              >
                Submit proof of delivery
              </Button>
            ) : null}
            <Button
              className="w-full"
              onClick={() => void navigate("/operations/delivery/my-run")}
            >
              Back to my run
            </Button>
          </div>
        </Panel>
      </div>
    );
  const outcome = form.watch("outcome");
  const isCOD = (stop.codAmountMinor ?? 0) > 0;
  return (
    <div className="mx-auto max-w-3xl">
      <PageHeader
        eyebrow={`Stop ${stop.stopSequence} · ${run.data?.runCode}`}
        title={stop.recipientName ?? stop.awb ?? "Delivery stop"}
        description={`${stop.awb} · ${stop.pieceCount} pieces · ${titleCase(stop.status ?? "")}`}
      />
      <Panel className="overflow-hidden">
        <div className="border-b p-5">
          <address className="flex gap-3 text-sm not-italic leading-6">
            <MapPin
              aria-hidden
              className="mt-1 h-5 w-5 shrink-0 text-primary"
            />
            <span>
              {stop.line1}
              <br />
              {stop.line2 ? (
                <>
                  {stop.line2}
                  <br />
                </>
              ) : null}
              {stop.landmark ? (
                <>
                  {stop.landmark}
                  <br />
                </>
              ) : null}
              {stop.city} {stop.pincode}
            </span>
          </address>
          <div className="mt-4 grid grid-cols-2 gap-2">
            <a
              className="inline-flex min-h-12 items-center justify-center gap-2 rounded-md border font-semibold"
              href={`tel:${stop.recipientPhone}`}
            >
              <Phone aria-hidden className="h-4 w-4" /> Contact
            </a>
            {stop.latitude && stop.longitude ? (
              <a
                className="inline-flex min-h-12 items-center justify-center gap-2 rounded-md border font-semibold"
                href={`https://www.google.com/maps/dir/?api=1&destination=${stop.latitude},${stop.longitude}`}
                target="_blank"
                rel="noreferrer"
              >
                <Navigation aria-hidden className="h-4 w-4" /> Navigate
              </a>
            ) : null}
          </div>
        </div>
        {isCOD ? (
          <div className="border-b bg-amber-50 p-5">
            <p className="text-xs font-semibold uppercase tracking-wide text-amber-800">
              Collect exactly
            </p>
            <strong className="mt-1 block text-3xl text-amber-950">
              {formatMoney(stop.codAmountMinor, stop.currency)}
            </strong>
            <p className="mt-1 text-xs text-amber-800">
              The server refuses under-collection.
            </p>
          </div>
        ) : null}
        <form
          className="grid gap-4 p-5 sm:grid-cols-2"
          onSubmit={(event) =>
            void form.handleSubmit((values) => attempt.mutate(values))(event)
          }
        >
          <Field label="Outcome" htmlFor="attempt-outcome" required>
            <Select id="attempt-outcome" {...form.register("outcome")}>
              <option>DELIVERED</option>
              <option>FAILED</option>
              <option>RESCHEDULED</option>
              <option>CANCELLED</option>
            </Select>
          </Field>
          <Field
            label="Current branch"
            htmlFor="attempt-facility"
            hint="Optional for a single-facility role"
          >
            <Input
              id="attempt-facility"
              value={facilityId}
              onChange={(event) => setFacilityId(event.target.value)}
              placeholder="ou_…"
            />
          </Field>
          {outcome === "DELIVERED" ? (
            <>
              <Field
                label="Recipient name"
                htmlFor="attempt-recipient"
                required
                error={form.formState.errors.recipientName?.message}
              >
                <Input
                  id="attempt-recipient"
                  {...form.register("recipientName")}
                />
              </Field>
              <Field label="Relationship" htmlFor="attempt-relationship">
                <Select
                  id="attempt-relationship"
                  {...form.register("recipientRelationship")}
                >
                  <option>SELF</option>
                  <option>FAMILY</option>
                  <option>NEIGHBOUR</option>
                  <option>SECURITY</option>
                  <option>RECEPTION</option>
                  <option>COLLEAGUE</option>
                  <option>OTHER</option>
                </Select>
              </Field>
              <Field
                label="OTP"
                htmlFor="attempt-otp"
                hint="Issue only when required"
              >
                <div className="flex gap-2">
                  <Input
                    id="attempt-otp"
                    inputMode="numeric"
                    {...form.register("otp")}
                  />
                  {hasPermission("delivery.otp_issue") ? (
                    <Button
                      type="button"
                      disabled={otp.isPending}
                      onClick={() => otp.mutate()}
                    >
                      Issue
                    </Button>
                  ) : null}
                </div>
              </Field>
              {issuedOTP ? (
                <div className="sm:col-span-2">
                  <InlineNotice tone="warning" title="One-time code shown once">
                    <span className="font-mono text-lg font-bold">
                      {issuedOTP.otp}
                    </span>{" "}
                    · for {issuedOTP.sentToMasked} · expires{" "}
                    {formatDateTime(issuedOTP.expiresAt)}. Do not store or share
                    this screen.
                  </InlineNotice>
                </div>
              ) : null}
              {isCOD ? (
                <>
                  <Field label="COD collected" htmlFor="attempt-cod" required>
                    <Input
                      id="attempt-cod"
                      inputMode="decimal"
                      {...form.register("codCollected")}
                    />
                  </Field>
                  <Field label="Payment mode" htmlFor="attempt-cod-mode">
                    <Select
                      id="attempt-cod-mode"
                      {...form.register("codPaymentMode")}
                    >
                      <option>CASH</option>
                      <option>UPI</option>
                      <option>CARD</option>
                      <option>WALLET</option>
                      <option>BANK_TRANSFER</option>
                      <option>CHEQUE</option>
                    </Select>
                  </Field>
                  <Field label="Payment reference" htmlFor="attempt-cod-ref">
                    <Input
                      id="attempt-cod-ref"
                      {...form.register("codReference")}
                    />
                  </Field>
                </>
              ) : null}
            </>
          ) : (
            <>
              <Field
                label="Failure reason"
                htmlFor="attempt-reason"
                required
                error={form.formState.errors.failureReasonCode?.message}
              >
                <Select
                  id="attempt-reason"
                  {...form.register("failureReasonCode")}
                >
                  <option value="">Select reason</option>
                  {reasons.data?.data
                    ?.filter((item) => item.isActive)
                    .map((item) => (
                      <option key={item.id} value={item.code}>
                        {item.name}
                      </option>
                    ))}
                </Select>
              </Field>
              {outcome === "RESCHEDULED" ? (
                <Field label="Next attempt" htmlFor="attempt-next">
                  <Input
                    id="attempt-next"
                    type="datetime-local"
                    {...form.register("nextAttemptAt")}
                  />
                </Field>
              ) : null}
            </>
          )}
          <Field
            className="sm:col-span-2"
            label="Remarks"
            htmlFor="attempt-remarks"
          >
            <Textarea id="attempt-remarks" {...form.register("remarks")} />
          </Field>
          {attempt.error ? (
            <div className="sm:col-span-2">
              <ErrorState error={attempt.error} />
            </div>
          ) : null}
          <div className="sm:col-span-2">
            <MobileActionBar>
              <Button
                className="min-h-12 flex-1"
                type="button"
                onClick={() => void navigate("/operations/delivery/my-run")}
              >
                Back
              </Button>
              <Button
                className="min-h-12 flex-1"
                variant={outcome === "DELIVERED" ? "primary" : "danger"}
                type="submit"
                disabled={attempt.isPending}
              >
                {attempt.isPending
                  ? "Recording…"
                  : outcome === "DELIVERED"
                    ? "Confirm delivered"
                    : "Record exception"}
              </Button>
            </MobileActionBar>
          </div>
        </form>
      </Panel>
    </div>
  );
}
