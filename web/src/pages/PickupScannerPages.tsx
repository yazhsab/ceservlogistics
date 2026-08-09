import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  CalendarClock,
  CheckCircle2,
  ClipboardCheck,
  MapPin,
  PackageCheck,
  Plus,
  ScanLine,
  SearchX,
  Truck,
  UserRoundCheck,
  Volume2,
  VolumeX,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useForm } from "react-hook-form";
import { useNavigate, useParams } from "react-router-dom";
import { z } from "zod";
import {
  apiRequest,
  operationalHeaders,
  queryString,
  type AgentStop,
  type AgentStopList,
  type AssignPickupRequest,
  type CompletePickupRequest,
  type CreatePickupRequest,
  type PickupPage,
  type PickupRequest,
  type PickupRun,
  type PickupRunList,
  type ScanHistoryPage,
  type ScanRequest,
  type ScanResult,
  type ScanType,
} from "../api/client";
import { useAuth } from "../auth/AuthProvider";
import { useToast } from "../components/ToastProvider";
import {
  CursorPager,
  EntityLink,
  MobileActionBar,
  OperationalMetricStrip,
  ScannerInput,
  ScanFeedback,
  type ScannerInputHandle,
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
  Input,
  LoadingState,
  PageHeader,
  Panel,
  PanelHeader,
  Select,
  StatusBadge,
  Switch,
  TableCell,
  TableHead,
  Textarea,
} from "../components/ui";
import { formatDateTime, formatWeight, titleCase } from "../lib/utils";
import {
  optionalNonnegativeInteger,
  optionalPositiveInteger,
} from "../lib/validation";

const pickupStatuses = [
  "REQUESTED",
  "SCHEDULED",
  "ASSIGNED",
  "ACCEPTED",
  "IN_PROGRESS",
  "COMPLETED",
  "PARTIALLY_COMPLETED",
  "FAILED",
  "CANCELLED",
  "EXPIRED",
];

const pickupSchema = z
  .object({
    customerId: z.string().min(1, "Customer ID is required"),
    pickupType: z.enum(["SCHEDULED", "ON_DEMAND", "BULK", "RECURRING"]),
    addressId: z.string().optional(),
    contactName: z.string().optional(),
    contactPhone: z.string().optional(),
    line1: z.string().optional(),
    city: z.string().optional(),
    state: z.string().optional(),
    pincode: z.string().optional(),
    scheduledDate: z.string().min(1, "Date is required"),
    windowStart: z.string().min(1, "Start time is required"),
    windowEnd: z.string().min(1, "End time is required"),
    expectedPieceCount: z.coerce.number().int().min(1).max(5000),
    expectedWeightGrams: optionalPositiveInteger,
    specialInstructions: z.string().max(1000).optional(),
    shipmentIds: z.string().optional(),
  })
  .superRefine((value, context) => {
    if (!value.addressId) {
      for (const field of [
        "contactName",
        "contactPhone",
        "line1",
        "pincode",
      ] as const) {
        if (!value[field]?.trim())
          context.addIssue({
            code: "custom",
            path: [field],
            message: "Required without a saved address",
          });
      }
    }
    if (
      value.windowStart &&
      value.windowEnd &&
      value.windowEnd <= value.windowStart
    )
      context.addIssue({
        code: "custom",
        path: ["windowEnd"],
        message: "End must be after start",
      });
  });
type PickupFormInput = z.input<typeof pickupSchema>;
type PickupForm = z.output<typeof pickupSchema>;

export function PickupDashboardPage() {
  const navigate = useNavigate();
  const { hasPermission } = useAuth();
  const [status, setStatus] = useState("");
  const [pickupType, setPickupType] = useState("");
  const [scheduledDate, setScheduledDate] = useState("");
  const [cursorStack, setCursorStack] = useState<(string | undefined)[]>([
    undefined,
  ]);
  const [selected, setSelected] = useState<string[]>([]);
  const [createOpen, setCreateOpen] = useState(false);
  const [assignOpen, setAssignOpen] = useState(false);
  const cursor = cursorStack.at(-1);
  const query = useQuery({
    queryKey: ["pickups", { cursor, status, pickupType, scheduledDate }],
    queryFn: () =>
      apiRequest<PickupPage>(
        `/api/v1/pickups${queryString({ cursor, limit: 25, status, pickupType, scheduledDate })}`,
      ),
    placeholderData: (previous) => previous,
  });
  const rows = query.data?.data ?? [];
  const reset = () => {
    setCursorStack([undefined]);
    setSelected([]);
  };
  const [now] = useState(Date.now);
  const visibleLate = rows.filter((item) => {
    const end = item.window?.end
      ? new Date(item.window.end).getTime()
      : Number.POSITIVE_INFINITY;
    return end < now && !["COMPLETED", "CANCELLED"].includes(item.status ?? "");
  }).length;
  return (
    <>
      <PageHeader
        eyebrow="Operations · Pickup"
        title="Pickup control"
        description="Plan collections, assign agents, and keep overdue work visible without leaving the queue."
        actions={
          <>
            {hasPermission("pickup.read") ? (
              <Button
                onClick={() => void navigate("/operations/pickups/my-stops")}
              >
                My stops
              </Button>
            ) : null}
            {hasPermission("pickup.assign") ? (
              <Button onClick={() => void navigate("/operations/pickups/runs")}>
                Pickup runs
              </Button>
            ) : null}
            {hasPermission("pickup.create") ? (
              <Button variant="primary" onClick={() => setCreateOpen(true)}>
                <Plus aria-hidden className="h-4 w-4" /> Raise pickup
              </Button>
            ) : null}
          </>
        }
      />
      <OperationalMetricStrip
        label="Visible pickup workload"
        items={[
          {
            label: "Visible on this page",
            value: rows.length,
            icon: ClipboardCheck,
          },
          {
            label: "Unassigned",
            value: rows.filter((item) => !item.agentId).length,
            tone: "warning",
            icon: UserRoundCheck,
          },
          {
            label: "Past window",
            value: visibleLate,
            tone: visibleLate ? "danger" : "success",
            icon: CalendarClock,
          },
          {
            label: "Assigned",
            value: rows.filter((item) => Boolean(item.agentId)).length,
            tone: "info",
            icon: Truck,
          },
          {
            label: "Completed",
            value: rows.filter((item) => item.status === "COMPLETED").length,
            tone: "success",
            icon: CheckCircle2,
          },
          { label: "Selected", value: selected.length, icon: PackageCheck },
        ]}
      />
      <Panel>
        <div className="grid gap-3 border-b p-4 md:grid-cols-3">
          <Select
            value={status}
            aria-label="Pickup status"
            onChange={(event) => {
              setStatus(event.target.value);
              reset();
            }}
          >
            <option value="">All pickup states</option>
            {pickupStatuses.map((item) => (
              <option key={item}>{item}</option>
            ))}
          </Select>
          <Select
            value={pickupType}
            aria-label="Pickup type"
            onChange={(event) => {
              setPickupType(event.target.value);
              reset();
            }}
          >
            <option value="">All pickup types</option>
            {(["SCHEDULED", "ON_DEMAND", "BULK", "RECURRING"] as const).map(
              (item) => (
                <option key={item}>{item}</option>
              ),
            )}
          </Select>
          <Input
            type="date"
            value={scheduledDate}
            aria-label="Scheduled date"
            onChange={(event) => {
              setScheduledDate(event.target.value);
              reset();
            }}
          />
        </div>
        {selected.length ? (
          <div className="flex flex-wrap items-center justify-between gap-3 border-b border-emerald-200 bg-emerald-50 px-4 py-2.5">
            <p className="text-sm font-semibold text-emerald-950">
              {selected.length} pickups selected
            </p>
            <div className="flex gap-2">
              <Button size="sm" onClick={() => setSelected([])}>
                Clear
              </Button>
              {hasPermission("pickup.assign") ? (
                <Button
                  size="sm"
                  variant="primary"
                  onClick={() => setAssignOpen(true)}
                >
                  Assign selected
                </Button>
              ) : null}
            </div>
          </div>
        ) : null}
        {query.isLoading ? (
          <LoadingState label="Loading pickup queue" />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : rows.length === 0 ? (
          <EmptyState
            icon={SearchX}
            title="No pickups in this queue"
            description="Change the filters or raise a pickup request."
          />
        ) : (
          <>
            <DataTable label="Pickup queue">
              <thead>
                <tr>
                  <TableHead>
                    <span className="sr-only">Select</span>
                  </TableHead>
                  <TableHead>Pickup</TableHead>
                  <TableHead>Customer & address</TableHead>
                  <TableHead>Window</TableHead>
                  <TableHead>Pieces</TableHead>
                  <TableHead>Agent</TableHead>
                  <TableHead>Status</TableHead>
                </tr>
              </thead>
              <tbody>
                {rows.map((pickup) => {
                  const id = pickup.id ?? "";
                  const late =
                    pickup.window?.end &&
                    new Date(pickup.window.end).getTime() < now &&
                    !["COMPLETED", "CANCELLED"].includes(pickup.status ?? "");
                  return (
                    <tr key={id} className="hover:bg-slate-50">
                      <TableCell>
                        <Checkbox
                          aria-label={`Select pickup ${pickup.referenceCode}`}
                          checked={selected.includes(id)}
                          onCheckedChange={(checked) =>
                            setSelected((current) =>
                              checked
                                ? [...current, id]
                                : current.filter((item) => item !== id),
                            )
                          }
                        />
                      </TableCell>
                      <TableCell>
                        <EntityLink
                          to={`/operations/pickups/${id}`}
                          primary={pickup.referenceCode}
                          secondary={titleCase(pickup.pickupType ?? "")}
                        />
                      </TableCell>
                      <TableCell>
                        <span className="font-medium">
                          {pickup.customer?.name}
                        </span>
                        <span className="block max-w-xs truncate text-xs text-slate-500">
                          {pickup.line1}, {pickup.city} · {pickup.pincode}
                        </span>
                      </TableCell>
                      <TableCell>
                        <span
                          className={late ? "font-semibold text-danger" : ""}
                        >
                          {formatDateTime(pickup.window?.start)}
                        </span>
                        <span className="block text-xs text-slate-500">
                          to {formatDateTime(pickup.window?.end)}
                        </span>
                        {late ? (
                          <Badge tone="danger" className="mt-1">
                            Late
                          </Badge>
                        ) : null}
                      </TableCell>
                      <TableCell>
                        {pickup.expectedPieceCount ?? 0}
                        <span className="block text-xs text-slate-500">
                          {pickup.actualPieceCount
                            ? `${pickup.actualPieceCount} collected`
                            : "expected"}
                        </span>
                      </TableCell>
                      <TableCell>
                        {pickup.agentName || (
                          <span className="text-amber-700">Unassigned</span>
                        )}
                      </TableCell>
                      <TableCell>
                        <StatusBadge status={pickup.status} />
                      </TableCell>
                    </tr>
                  );
                })}
              </tbody>
            </DataTable>
            <CursorPager
              page={cursorStack.length}
              count={rows.length}
              noun="pickups"
              hasMore={query.data?.pagination?.hasMore}
              nextCursor={query.data?.pagination?.nextCursor}
              onPrevious={() => setCursorStack((items) => items.slice(0, -1))}
              onNext={(next) => setCursorStack((items) => [...items, next])}
            />
          </>
        )}
      </Panel>
      <CreatePickupDialog open={createOpen} onOpenChange={setCreateOpen} />
      <AssignPickupDialog
        pickupIds={selected}
        open={assignOpen}
        onOpenChange={setAssignOpen}
        onDone={() => setSelected([])}
      />
    </>
  );
}

function CreatePickupDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const client = useQueryClient();
  const { toast } = useToast();
  const form = useForm<PickupFormInput, unknown, PickupForm>({
    resolver: zodResolver(pickupSchema),
    defaultValues: {
      pickupType: "SCHEDULED",
      expectedPieceCount: 1,
      scheduledDate: new Date().toISOString().slice(0, 10),
    },
  });
  const mutation = useMutation({
    mutationFn: (values: PickupForm) => {
      const request: CreatePickupRequest = {
        customerId: values.customerId,
        pickupType: values.pickupType,
        addressId: values.addressId || undefined,
        contactName: values.contactName || undefined,
        contactPhone: values.contactPhone || undefined,
        line1: values.line1 || undefined,
        city: values.city || undefined,
        state: values.state || undefined,
        pincode: values.pincode || undefined,
        scheduledDate: values.scheduledDate,
        windowStart: new Date(values.windowStart).toISOString(),
        windowEnd: new Date(values.windowEnd).toISOString(),
        expectedPieceCount: values.expectedPieceCount,
        expectedWeightGrams: values.expectedWeightGrams || undefined,
        specialInstructions: values.specialInstructions || undefined,
        maxAttempts: 3,
        shipmentIds: values.shipmentIds?.split(/[\s,]+/).filter(Boolean),
      };
      return apiRequest<PickupRequest>("/api/v1/pickups", {
        method: "POST",
        body: request,
      });
    },
    onSuccess: (pickup) => {
      void client.invalidateQueries({ queryKey: ["pickups"] });
      toast({
        tone: "success",
        title: "Pickup raised",
        description: pickup.referenceCode,
      });
      form.reset();
      onOpenChange(false);
    },
  });
  const errors = form.formState.errors;
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Raise pickup"
      description="Schedule a collection against a customer and a saved or one-time pickup address."
      footer={
        <>
          <Button onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button
            variant="primary"
            disabled={mutation.isPending}
            onClick={() =>
              void form.handleSubmit((values) => mutation.mutate(values))()
            }
          >
            {mutation.isPending ? "Raising…" : "Raise pickup"}
          </Button>
        </>
      }
    >
      <form
        className="grid gap-4 md:grid-cols-2"
        onSubmit={(event) =>
          void form.handleSubmit((values) => mutation.mutate(values))(event)
        }
      >
        <Field
          label="Customer ID"
          htmlFor="pickup-customer"
          required
          error={errors.customerId?.message}
        >
          <Input
            id="pickup-customer"
            {...form.register("customerId")}
            placeholder="cus_…"
          />
        </Field>
        <Field label="Pickup type" htmlFor="pickup-type" required>
          <Select id="pickup-type" {...form.register("pickupType")}>
            <option>SCHEDULED</option>
            <option>ON_DEMAND</option>
            <option>BULK</option>
            <option>RECURRING</option>
          </Select>
        </Field>
        <Field
          label="Saved address ID"
          htmlFor="pickup-address-id"
          hint="If supplied, manual address fields are ignored."
        >
          <Input
            id="pickup-address-id"
            {...form.register("addressId")}
            placeholder="cua_…"
          />
        </Field>
        <Field
          label="Contact name"
          htmlFor="pickup-contact"
          error={errors.contactName?.message}
        >
          <Input id="pickup-contact" {...form.register("contactName")} />
        </Field>
        <Field
          label="Contact phone"
          htmlFor="pickup-phone"
          error={errors.contactPhone?.message}
        >
          <Input
            id="pickup-phone"
            type="tel"
            {...form.register("contactPhone")}
          />
        </Field>
        <Field
          label="Address line"
          htmlFor="pickup-line"
          error={errors.line1?.message}
        >
          <Input id="pickup-line" {...form.register("line1")} />
        </Field>
        <Field label="City" htmlFor="pickup-city">
          <Input id="pickup-city" {...form.register("city")} />
        </Field>
        <Field label="State" htmlFor="pickup-state">
          <Input id="pickup-state" {...form.register("state")} />
        </Field>
        <Field
          label="Postal code"
          htmlFor="pickup-pin"
          error={errors.pincode?.message}
        >
          <Input
            id="pickup-pin"
            inputMode="numeric"
            {...form.register("pincode")}
          />
        </Field>
        <Field
          label="Scheduled date"
          htmlFor="pickup-date"
          required
          error={errors.scheduledDate?.message}
        >
          <Input
            id="pickup-date"
            type="date"
            {...form.register("scheduledDate")}
          />
        </Field>
        <Field
          label="Window starts"
          htmlFor="pickup-start"
          required
          error={errors.windowStart?.message}
        >
          <Input
            id="pickup-start"
            type="datetime-local"
            {...form.register("windowStart")}
          />
        </Field>
        <Field
          label="Window ends"
          htmlFor="pickup-end"
          required
          error={errors.windowEnd?.message}
        >
          <Input
            id="pickup-end"
            type="datetime-local"
            {...form.register("windowEnd")}
          />
        </Field>
        <Field
          label="Expected pieces"
          htmlFor="pickup-pieces"
          required
          error={errors.expectedPieceCount?.message}
        >
          <Input
            id="pickup-pieces"
            type="number"
            min={1}
            {...form.register("expectedPieceCount")}
          />
        </Field>
        <Field label="Expected weight (g)" htmlFor="pickup-weight">
          <Input
            id="pickup-weight"
            type="number"
            min={1}
            {...form.register("expectedWeightGrams")}
          />
        </Field>
        <Field
          className="md:col-span-2"
          label="Shipment IDs"
          htmlFor="pickup-shipments"
          hint="Optional. Separate IDs with commas or spaces."
        >
          <Input
            id="pickup-shipments"
            {...form.register("shipmentIds")}
            placeholder="shp_…"
          />
        </Field>
        <Field
          className="md:col-span-2"
          label="Instructions"
          htmlFor="pickup-notes"
        >
          <Textarea
            id="pickup-notes"
            {...form.register("specialInstructions")}
          />
        </Field>
        {mutation.error ? (
          <div className="md:col-span-2">
            <ErrorState error={mutation.error} />
          </div>
        ) : null}
      </form>
    </Dialog>
  );
}

const assignSchema = z.object({
  agentId: z.string().min(1, "Agent ID is required"),
  runId: z.string().optional(),
});

function AssignPickupDialog({
  pickupIds,
  open,
  onOpenChange,
  onDone,
}: {
  pickupIds: string[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onDone: () => void;
}) {
  const client = useQueryClient();
  const { toast } = useToast();
  const form = useForm<z.infer<typeof assignSchema>>({
    resolver: zodResolver(assignSchema),
  });
  const mutation = useMutation({
    mutationFn: async (values: z.infer<typeof assignSchema>) => {
      const body: AssignPickupRequest = {
        agentId: values.agentId,
        runId: values.runId || undefined,
      };
      const outcomes = await Promise.allSettled(
        pickupIds.map((id) =>
          apiRequest<PickupRequest>(`/api/v1/pickups/${id}/assign`, {
            method: "POST",
            body,
          }),
        ),
      );
      return {
        succeeded: outcomes.filter((item) => item.status === "fulfilled")
          .length,
        failed: outcomes.filter((item) => item.status === "rejected").length,
      };
    },
    onSuccess: (result) => {
      void client.invalidateQueries({ queryKey: ["pickups"] });
      toast({
        tone: result.failed ? "info" : "success",
        title: `${result.succeeded} pickups assigned`,
        description: result.failed
          ? `${result.failed} could not be assigned and remain selected in the queue.`
          : undefined,
      });
      if (!result.failed) onDone();
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Assign selected pickups"
      description={`Send ${pickupIds.length} selected pickups to one agent. Each assignment is committed independently; partial failures remain visible.`}
      footer={
        <>
          <Button onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button
            variant="primary"
            disabled={!pickupIds.length || mutation.isPending}
            onClick={() =>
              void form.handleSubmit((values) => mutation.mutate(values))()
            }
          >
            {mutation.isPending ? "Assigning…" : "Assign pickups"}
          </Button>
        </>
      }
    >
      <div className="grid gap-4 sm:grid-cols-2">
        <Field
          label="Agent user ID"
          htmlFor="assign-agent"
          required
          error={form.formState.errors.agentId?.message}
        >
          <Input
            id="assign-agent"
            {...form.register("agentId")}
            placeholder="usr_…"
          />
        </Field>
        <Field label="Pickup run ID" htmlFor="assign-run" hint="Optional">
          <Input
            id="assign-run"
            {...form.register("runId")}
            placeholder="prun_…"
          />
        </Field>
      </div>
      {mutation.error ? <ErrorState error={mutation.error} /> : null}
    </Dialog>
  );
}

export function PickupDetailPage() {
  const { pickupId = "" } = useParams();
  const { hasPermission } = useAuth();
  const [assignOpen, setAssignOpen] = useState(false);
  const query = useQuery({
    queryKey: ["pickup", pickupId],
    queryFn: () => apiRequest<PickupRequest>(`/api/v1/pickups/${pickupId}`),
    enabled: Boolean(pickupId),
  });
  if (query.isLoading) return <LoadingState label="Loading pickup" />;
  if (query.error)
    return (
      <ErrorState error={query.error} retry={() => void query.refetch()} />
    );
  const pickup = query.data;
  if (!pickup)
    return (
      <EmptyState
        title="Pickup not found"
        description="The request may no longer be available."
      />
    );
  return (
    <>
      <PageHeader
        eyebrow="Operations · Pickup"
        title={pickup.referenceCode ?? "Pickup request"}
        description={`${pickup.customer?.name ?? "Customer"} · ${pickup.address?.city ?? "Unknown city"} · ${pickup.expectedPieceCount ?? 0} pieces`}
        actions={
          hasPermission("pickup.assign") ? (
            <Button variant="primary" onClick={() => setAssignOpen(true)}>
              Assign agent
            </Button>
          ) : undefined
        }
      />
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.4fr)_minmax(300px,0.6fr)]">
        <div className="space-y-4">
          <Panel>
            <PanelHeader title="Collection brief" />
            <dl className="grid gap-4 p-4 sm:grid-cols-2 lg:grid-cols-3">
              {[
                ["Status", <StatusBadge key="status" status={pickup.status} />],
                ["Pickup type", titleCase(pickup.pickupType ?? "")],
                [
                  "Window",
                  `${formatDateTime(pickup.window?.start)} – ${formatDateTime(pickup.window?.end)}`,
                ],
                [
                  "Expected",
                  `${pickup.expectedPieceCount ?? 0} pieces · ${formatWeight(pickup.expectedWeightGrams)}`,
                ],
                [
                  "Attempts",
                  `${pickup.attemptCount ?? 0} of ${pickup.maxAttempts ?? 0}`,
                ],
                ["Branch", pickup.branch?.name ?? pickup.branch?.code ?? "—"],
              ].map(([label, value]) => (
                <div key={String(label)}>
                  <dt className="text-xs font-medium uppercase tracking-wide text-slate-500">
                    {label}
                  </dt>
                  <dd className="mt-1 text-sm font-semibold text-slate-900">
                    {value}
                  </dd>
                </div>
              ))}
            </dl>
          </Panel>
          <Panel>
            <PanelHeader
              title="Linked shipments"
              description="Only the server records what was actually collected."
            />
            {pickup.shipments?.length ? (
              <DataTable label="Pickup shipments">
                <thead>
                  <tr>
                    <TableHead>AWB</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead>Pieces</TableHead>
                    <TableHead>Weight</TableHead>
                    <TableHead>Payment</TableHead>
                  </tr>
                </thead>
                <tbody>
                  {pickup.shipments.map((item) => (
                    <tr key={item.shipmentId}>
                      <TableCell>
                        <EntityLink
                          to={`/shipments/${item.shipmentId}`}
                          primary={item.awb}
                        />
                      </TableCell>
                      <TableCell>
                        <StatusBadge status={item.status} />
                      </TableCell>
                      <TableCell>{item.pieceCount}</TableCell>
                      <TableCell>{formatWeight(item.weightGrams)}</TableCell>
                      <TableCell>{titleCase(item.paymentMode ?? "")}</TableCell>
                    </tr>
                  ))}
                </tbody>
              </DataTable>
            ) : (
              <EmptyState
                title="No linked shipments"
                description="This request expects loose pieces or shipments added later."
              />
            )}
          </Panel>
          <Panel>
            <PanelHeader title="Visit history" />
            {pickup.attempts?.length ? (
              <div className="divide-y">
                {pickup.attempts.map((attempt) => (
                  <div
                    key={attempt.id}
                    className="flex flex-wrap items-start justify-between gap-3 p-4"
                  >
                    <div>
                      <strong className="text-sm">
                        Attempt {attempt.attemptNumber}:{" "}
                        {titleCase(attempt.outcome ?? "")}
                      </strong>
                      <p className="mt-1 text-xs text-slate-500">
                        {attempt.remarks ||
                          attempt.failureReasonCode ||
                          "No remarks"}
                      </p>
                    </div>
                    <time className="text-xs text-slate-500">
                      {formatDateTime(attempt.occurredAt)}
                    </time>
                  </div>
                ))}
              </div>
            ) : (
              <EmptyState
                title="No visits recorded"
                description="The first attempt will appear after the agent records it."
              />
            )}
          </Panel>
        </div>
        <div className="space-y-4">
          <Panel>
            <PanelHeader title="Pickup address" />
            <address className="p-4 text-sm not-italic leading-6">
              <strong>{pickup.address?.contactName}</strong>
              <br />
              {pickup.address?.line1}
              <br />
              {pickup.address?.line2 ? (
                <>
                  {pickup.address.line2}
                  <br />
                </>
              ) : null}
              {pickup.address?.landmark ? (
                <>
                  {pickup.address.landmark}
                  <br />
                </>
              ) : null}
              {pickup.address?.city}, {pickup.address?.state}{" "}
              {pickup.address?.pincode}
              <br />
              <a
                className="font-semibold text-primary"
                href={`tel:${pickup.address?.phone}`}
              >
                {pickup.address?.phone}
              </a>
            </address>
          </Panel>
          <Panel>
            <PanelHeader title="Current assignment" />
            {pickup.assignment ? (
              <dl className="space-y-3 p-4 text-sm">
                <div>
                  <dt className="text-slate-500">Agent</dt>
                  <dd className="font-semibold">
                    {pickup.assignment.agent?.name ??
                      pickup.assignment.agent?.code}
                  </dd>
                </div>
                <div>
                  <dt className="text-slate-500">Assignment</dt>
                  <dd>
                    <StatusBadge status={pickup.assignment.status} />
                  </dd>
                </div>
                <div>
                  <dt className="text-slate-500">Stop sequence</dt>
                  <dd className="font-semibold">
                    {pickup.assignment.stopSequence ?? "—"}
                  </dd>
                </div>
              </dl>
            ) : (
              <EmptyState
                title="Not assigned"
                description="Assign an agent when the pickup is ready to dispatch."
              />
            )}
          </Panel>
          {pickup.specialInstructions ? (
            <Panel>
              <PanelHeader title="Special instructions" />
              <p className="p-4 text-sm text-slate-700">
                {pickup.specialInstructions}
              </p>
            </Panel>
          ) : null}
        </div>
      </div>
      <AssignPickupDialog
        pickupIds={[pickupId]}
        open={assignOpen}
        onOpenChange={setAssignOpen}
        onDone={() => void query.refetch()}
      />
    </>
  );
}

const runSchema = z.object({
  branchId: z.string().optional(),
  agentId: z.string().min(1),
  runDate: z.string().min(1),
  vehicleReference: z.string().optional(),
});

export function PickupRunsPage() {
  const { hasPermission } = useAuth();
  const [status, setStatus] = useState("");
  const [runDate, setRunDate] = useState("");
  const [open, setOpen] = useState(false);
  const query = useQuery({
    queryKey: ["pickup-runs", { status, runDate }],
    queryFn: () =>
      apiRequest<PickupRunList>(
        `/api/v1/pickup-runs${queryString({ limit: 50, status, runDate })}`,
      ),
  });
  const rows = query.data?.data ?? [];
  return (
    <>
      <PageHeader
        eyebrow="Operations · Pickup"
        title="Pickup runs"
        description="Planned agent routes and collection progress for the day."
        actions={
          hasPermission("pickup.assign") ? (
            <Button variant="primary" onClick={() => setOpen(true)}>
              <Plus aria-hidden className="h-4 w-4" /> Create run
            </Button>
          ) : undefined
        }
      />
      <Panel>
        <div className="grid gap-3 border-b p-4 sm:grid-cols-2">
          <Select
            value={status}
            onChange={(event) => setStatus(event.target.value)}
            aria-label="Run status"
          >
            <option value="">All states</option>
            <option>PLANNED</option>
            <option>STARTED</option>
            <option>COMPLETED</option>
            <option>CANCELLED</option>
          </Select>
          <Input
            type="date"
            value={runDate}
            onChange={(event) => setRunDate(event.target.value)}
            aria-label="Run date"
          />
        </div>
        {query.isLoading ? (
          <LoadingState label="Loading pickup runs" />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : rows.length ? (
          <DataTable label="Pickup runs">
            <thead>
              <tr>
                <TableHead>Run</TableHead>
                <TableHead>Date</TableHead>
                <TableHead>Agent</TableHead>
                <TableHead>Branch</TableHead>
                <TableHead>Stops</TableHead>
                <TableHead>Pieces</TableHead>
                <TableHead>Status</TableHead>
              </tr>
            </thead>
            <tbody>
              {rows.map((run) => (
                <tr key={run.id}>
                  <TableCell>
                    <span className="font-mono font-semibold text-primary">
                      {run.runCode}
                    </span>
                  </TableCell>
                  <TableCell>{run.runDate}</TableCell>
                  <TableCell>{run.agent?.name ?? run.agent?.code}</TableCell>
                  <TableCell>{run.branch?.code}</TableCell>
                  <TableCell>
                    {run.completedStops ?? 0} / {run.plannedStops ?? 0}
                  </TableCell>
                  <TableCell>{run.collectedPieces ?? 0}</TableCell>
                  <TableCell>
                    <StatusBadge status={run.status} />
                  </TableCell>
                </tr>
              ))}
            </tbody>
          </DataTable>
        ) : (
          <EmptyState
            title="No pickup runs"
            description="Create a run for an agent and assign pickup stops to it."
          />
        )}
      </Panel>
      <CreatePickupRunDialog open={open} onOpenChange={setOpen} />
    </>
  );
}

function CreatePickupRunDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const client = useQueryClient();
  const { toast } = useToast();
  const form = useForm<z.infer<typeof runSchema>>({
    resolver: zodResolver(runSchema),
    defaultValues: { runDate: new Date().toISOString().slice(0, 10) },
  });
  const mutation = useMutation({
    mutationFn: (body: z.infer<typeof runSchema>) =>
      apiRequest<PickupRun>("/api/v1/pickup-runs", { method: "POST", body }),
    onSuccess: (run) => {
      void client.invalidateQueries({ queryKey: ["pickup-runs"] });
      toast({
        tone: "success",
        title: "Pickup run created",
        description: run.runCode,
      });
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Create pickup run"
      description="Plan one agent's collection route for a day."
      footer={
        <>
          <Button onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button
            variant="primary"
            disabled={mutation.isPending}
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
          htmlFor="run-branch"
          hint="Optional when your role covers one branch"
        >
          <Input id="run-branch" {...form.register("branchId")} />
        </Field>
        <Field
          label="Agent user ID"
          htmlFor="run-agent"
          required
          error={form.formState.errors.agentId?.message}
        >
          <Input id="run-agent" {...form.register("agentId")} />
        </Field>
        <Field label="Run date" htmlFor="run-date" required>
          <Input id="run-date" type="date" {...form.register("runDate")} />
        </Field>
        <Field label="Vehicle reference" htmlFor="run-vehicle">
          <Input id="run-vehicle" {...form.register("vehicleReference")} />
        </Field>
      </div>
      {mutation.error ? <ErrorState error={mutation.error} /> : null}
    </Dialog>
  );
}

export function PickupAgentPage() {
  const { hasPermission } = useAuth();
  const [date, setDate] = useState(new Date().toISOString().slice(0, 10));
  const [selected, setSelected] = useState<AgentStop>();
  const [completeOpen, setCompleteOpen] = useState(false);
  const client = useQueryClient();
  const { toast } = useToast();
  const query = useQuery({
    queryKey: ["pickup-stops", date],
    queryFn: () =>
      apiRequest<AgentStopList>(
        `/api/v1/pickups/my-stops${queryString({ date, onlyOpen: true, limit: 50 })}`,
      ),
  });
  const action = useMutation({
    mutationFn: ({
      stop,
      kind,
    }: {
      stop: AgentStop;
      kind: "accept" | "arrive";
    }) =>
      kind === "accept"
        ? apiRequest<PickupRequest>(
            `/api/v1/pickups/assignments/${stop.assignmentId}/respond`,
            { method: "POST", body: { accept: true } },
          )
        : apiRequest<PickupRequest>(
            `/api/v1/pickups/assignments/${stop.assignmentId}/arrive`,
            {
              method: "POST",
              body: {},
              headers: operationalHeaders("pickup-arrive"),
            },
          ),
    onSuccess: (_, variables) => {
      void client.invalidateQueries({ queryKey: ["pickup-stops"] });
      toast({
        tone: "success",
        title:
          variables.kind === "accept"
            ? "Assignment accepted"
            : "Arrival recorded",
      });
    },
  });
  const stops = query.data?.data ?? [];
  const current = selected ?? stops[0];
  return (
    <div className="mx-auto max-w-3xl">
      <PageHeader
        eyebrow="Field · Pickup"
        title="My pickup route"
        description="Your next collection stays at the top. Actions are sized for one-handed use."
        actions={
          <Input
            className="w-40"
            type="date"
            value={date}
            onChange={(event) => setDate(event.target.value)}
            aria-label="Pickup route date"
          />
        }
      />
      {query.isLoading ? (
        <LoadingState label="Loading today's stops" />
      ) : query.error ? (
        <ErrorState error={query.error} retry={() => void query.refetch()} />
      ) : !current ? (
        <Panel>
          <EmptyState
            icon={CheckCircle2}
            title="Route clear"
            description="There are no open pickup stops for this date."
          />
        </Panel>
      ) : (
        <>
          <Panel className="overflow-hidden">
            <div className="border-b bg-emerald-950 px-5 py-4 text-white">
              <p className="text-xs font-semibold uppercase tracking-wider text-emerald-200">
                Next pickup · stop {current.stopSequence ?? "—"}
              </p>
              <h2 className="mt-1 text-xl font-bold">{current.customerName}</h2>
              <p className="mt-1 text-sm text-emerald-50/80">
                {current.referenceCode} · {current.expectedPieceCount ?? 0}{" "}
                pieces
              </p>
            </div>
            <div className="space-y-4 p-5">
              <address className="flex gap-3 text-sm not-italic leading-6">
                <MapPin
                  aria-hidden
                  className="mt-1 h-5 w-5 shrink-0 text-primary"
                />
                <span>
                  {current.address?.line1}
                  <br />
                  {current.address?.landmark ? (
                    <>
                      {current.address.landmark}
                      <br />
                    </>
                  ) : null}
                  {current.address?.city}, {current.address?.state}{" "}
                  {current.address?.pincode}
                </span>
              </address>
              <div className="grid gap-3 sm:grid-cols-2">
                <a
                  className="inline-flex min-h-12 items-center justify-center rounded-md border border-border bg-white px-4 text-sm font-semibold"
                  href={`tel:${current.address?.phone}`}
                >
                  Call {current.address?.phone}
                </a>
                {current.address?.latitude && current.address.longitude ? (
                  <a
                    className="inline-flex min-h-12 items-center justify-center rounded-md border border-border bg-white px-4 text-sm font-semibold"
                    href={`https://www.google.com/maps/dir/?api=1&destination=${current.address.latitude},${current.address.longitude}`}
                    target="_blank"
                    rel="noreferrer"
                  >
                    Open navigation
                  </a>
                ) : null}
              </div>
              {current.specialInstructions ? (
                <div className="rounded-md border border-amber-200 bg-amber-50 p-3 text-sm text-amber-950">
                  <strong className="block">Instructions</strong>
                  {current.specialInstructions}
                </div>
              ) : null}
            </div>
          </Panel>
          {stops.length > 1 ? (
            <Panel className="mt-4">
              <PanelHeader title="Up next" />
              <div className="divide-y">
                {stops.slice(1).map((stop) => (
                  <button
                    key={stop.assignmentId}
                    onClick={() => setSelected(stop)}
                    className="flex w-full items-center justify-between gap-3 p-4 text-left hover:bg-slate-50"
                  >
                    <span>
                      <strong className="block text-sm">
                        {stop.stopSequence}. {stop.customerName}
                      </strong>
                      <span className="text-xs text-slate-500">
                        {stop.address?.city} · {stop.expectedPieceCount} pieces
                      </span>
                    </span>
                    <StatusBadge status={stop.status} />
                  </button>
                ))}
              </div>
            </Panel>
          ) : null}
          <MobileActionBar>
            {current.status === "ASSIGNED" &&
            hasPermission("pickup.respond") ? (
              <Button
                className="min-h-12 flex-1"
                variant="primary"
                disabled={action.isPending}
                onClick={() => action.mutate({ stop: current, kind: "accept" })}
              >
                Accept stop
              </Button>
            ) : null}
            {["ACCEPTED", "ASSIGNED"].includes(current.status ?? "") &&
            hasPermission("pickup.complete") ? (
              <Button
                className="min-h-12 flex-1"
                disabled={action.isPending}
                onClick={() => action.mutate({ stop: current, kind: "arrive" })}
              >
                I've arrived
              </Button>
            ) : null}
            {hasPermission("pickup.complete") ? (
              <Button
                className="min-h-12 flex-1"
                variant="primary"
                onClick={() => setCompleteOpen(true)}
              >
                Record pickup
              </Button>
            ) : null}
          </MobileActionBar>
          <CompletePickupDialog
            stop={current}
            open={completeOpen}
            onOpenChange={setCompleteOpen}
          />
        </>
      )}
    </div>
  );
}

const completeSchema = z
  .object({
    outcome: z.enum([
      "COMPLETED",
      "PARTIAL",
      "FAILED",
      "RESCHEDULED",
      "CANCELLED_ON_SITE",
    ]),
    collectedShipmentIds: z.string().optional(),
    loosePieces: optionalNonnegativeInteger,
    weightGrams: optionalNonnegativeInteger,
    failureReasonCode: z.string().optional(),
    nextAttemptAt: z.string().optional(),
    remarks: z.string().optional(),
  })
  .superRefine((value, context) => {
    if (
      ["FAILED", "RESCHEDULED"].includes(value.outcome) &&
      !value.failureReasonCode
    )
      context.addIssue({
        code: "custom",
        path: ["failureReasonCode"],
        message: "Reason is required",
      });
  });

function CompletePickupDialog({
  stop,
  open,
  onOpenChange,
}: {
  stop: AgentStop;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const client = useQueryClient();
  const { toast } = useToast();
  const form = useForm<
    z.input<typeof completeSchema>,
    unknown,
    z.output<typeof completeSchema>
  >({
    resolver: zodResolver(completeSchema),
    defaultValues: {
      outcome: "COMPLETED",
      loosePieces: stop.expectedPieceCount ?? 0,
    },
  });
  const mutation = useMutation({
    mutationFn: (values: z.output<typeof completeSchema>) => {
      const body: CompletePickupRequest = {
        ...values,
        collectedShipmentIds: values.collectedShipmentIds
          ?.split(/[\s,]+/)
          .filter(Boolean),
        nextAttemptAt: values.nextAttemptAt
          ? new Date(values.nextAttemptAt).toISOString()
          : undefined,
        occurredAt: new Date().toISOString(),
      };
      return apiRequest<PickupRequest>(
        `/api/v1/pickups/${stop.requestId}/complete`,
        {
          method: "POST",
          body,
          headers: operationalHeaders("pickup-complete"),
        },
      );
    },
    onSuccess: (pickup) => {
      void client.invalidateQueries({ queryKey: ["pickup-stops"] });
      toast({
        tone: "success",
        title: `Pickup ${titleCase(pickup.status ?? "")}`,
        description: pickup.referenceCode,
      });
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Record pickup visit"
      description="Record exactly what the customer handed over. The server owns the resulting shipment state."
      footer={
        <>
          <Button onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button
            variant="primary"
            disabled={mutation.isPending}
            onClick={() =>
              void form.handleSubmit((values) => mutation.mutate(values))()
            }
          >
            {mutation.isPending ? "Recording…" : "Record outcome"}
          </Button>
        </>
      }
    >
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Outcome" htmlFor="pickup-outcome" required>
          <Select id="pickup-outcome" {...form.register("outcome")}>
            <option>COMPLETED</option>
            <option>PARTIAL</option>
            <option>FAILED</option>
            <option>RESCHEDULED</option>
            <option>CANCELLED_ON_SITE</option>
          </Select>
        </Field>
        <Field label="Pieces collected" htmlFor="pickup-loose">
          <Input
            id="pickup-loose"
            type="number"
            min={0}
            {...form.register("loosePieces")}
          />
        </Field>
        <Field label="Weight (g)" htmlFor="pickup-complete-weight">
          <Input
            id="pickup-complete-weight"
            type="number"
            min={0}
            {...form.register("weightGrams")}
          />
        </Field>
        <Field
          label="Failure reason code"
          htmlFor="pickup-failure"
          error={form.formState.errors.failureReasonCode?.message}
        >
          <Input id="pickup-failure" {...form.register("failureReasonCode")} />
        </Field>
        <Field
          className="sm:col-span-2"
          label="Collected shipment IDs"
          htmlFor="pickup-collected"
          hint="Separate with commas or spaces"
        >
          <Input
            id="pickup-collected"
            {...form.register("collectedShipmentIds")}
          />
        </Field>
        <Field label="Next attempt" htmlFor="pickup-next">
          <Input
            id="pickup-next"
            type="datetime-local"
            {...form.register("nextAttemptAt")}
          />
        </Field>
        <Field label="Remarks" htmlFor="pickup-remarks">
          <Textarea id="pickup-remarks" {...form.register("remarks")} />
        </Field>
      </div>
      {mutation.error ? <ErrorState error={mutation.error} /> : null}
    </Dialog>
  );
}

const scanModes: Array<{
  type: ScanType;
  key: string;
  label: string;
  permission: string;
}> = [
  { type: "RECEIVE", key: "1", label: "Receive", permission: "scan.inbound" },
  { type: "ARRIVAL", key: "2", label: "Arrival", permission: "scan.inbound" },
  {
    type: "DEPARTURE",
    key: "3",
    label: "Departure",
    permission: "scan.outbound",
  },
  { type: "SORT", key: "4", label: "Sort", permission: "scan.sort" },
  { type: "HOLD", key: "5", label: "Hold", permission: "scan.hold" },
  { type: "RELEASE", key: "6", label: "Release", permission: "scan.hold" },
  { type: "DAMAGE", key: "7", label: "Damage", permission: "scan.exception" },
  {
    type: "EXCEPTION",
    key: "8",
    label: "Exception",
    permission: "scan.exception",
  },
];

function playTone(outcome?: string) {
  try {
    const AudioContextClass = window.AudioContext;
    const context = new AudioContextClass();
    const oscillator = context.createOscillator();
    const gain = context.createGain();
    oscillator.frequency.value =
      outcome === "ACCEPTED" ? 880 : outcome === "DUPLICATE" ? 560 : 220;
    gain.gain.setValueAtTime(0.08, context.currentTime);
    gain.gain.exponentialRampToValueAtTime(0.001, context.currentTime + 0.14);
    oscillator.connect(gain).connect(context.destination);
    oscillator.start();
    oscillator.stop(context.currentTime + 0.14);
  } catch {
    // Audible feedback is optional; visual feedback remains authoritative.
  }
}

export function ScannerConsolePage() {
  const { hasPermission } = useAuth();
  const availableModes = useMemo(
    () => scanModes.filter((mode) => hasPermission(mode.permission)),
    [hasPermission],
  );
  const [mode, setMode] = useState<ScanType>(
    availableModes[0]?.type ?? "RECEIVE",
  );
  const [facilityId, setFacilityId] = useState(
    () => localStorage.getItem("courier.scan-facility") ?? "",
  );
  const [barcode, setBarcode] = useState("");
  const [reason, setReason] = useState("");
  const [sound, setSound] = useState(
    () => localStorage.getItem("courier.scan-sound") !== "false",
  );
  const [session, setSession] = useState<ScanResult[]>([]);
  const scannerRef = useRef<ScannerInputHandle>(null);
  const { toast } = useToast();
  const history = useQuery({
    queryKey: ["scan-history", mode, facilityId],
    queryFn: () =>
      apiRequest<ScanHistoryPage>(
        `/api/v1/scans${queryString({ limit: 25, scanType: mode, operatingUnitId: facilityId })}`,
      ),
    refetchInterval: 30_000,
  });
  const mutation = useMutation({
    mutationFn: (value: string) => {
      const body: ScanRequest = {
        barcode: value,
        scanType: mode,
        operatingUnitId: facilityId || undefined,
        occurredAt: new Date().toISOString(),
        reason: reason || undefined,
      };
      return apiRequest<ScanResult>(
        `/api/v1/scans${queryString({ operatingUnitId: facilityId })}`,
        { method: "POST", body, headers: operationalHeaders("scan") },
      );
    },
    onSuccess: (result) => {
      setSession((items) => [result, ...items].slice(0, 50));
      setBarcode("");
      if (sound) playTone(result.outcome);
      void history.refetch();
    },
    onError: (error) => {
      if (sound) playTone("REJECTED");
      toast({
        tone: "error",
        title: "Scanner request failed",
        description: error instanceof Error ? error.message : undefined,
      });
    },
    onSettled: () => window.setTimeout(() => scannerRef.current?.focus(), 0),
  });
  useEffect(() => {
    localStorage.setItem("courier.scan-facility", facilityId);
  }, [facilityId]);
  useEffect(() => {
    localStorage.setItem("courier.scan-sound", String(sound));
  }, [sound]);
  useEffect(() => {
    const handler = (event: KeyboardEvent) => {
      if (!event.altKey) return;
      const target = availableModes.find((item) => item.key === event.key);
      if (target) {
        event.preventDefault();
        setMode(target.type);
        scannerRef.current?.focus();
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [availableModes]);
  const accepted = session.filter((item) => item.outcome === "ACCEPTED").length;
  const duplicate = session.filter(
    (item) => item.outcome === "DUPLICATE",
  ).length;
  const rejected = session.filter((item) => item.outcome === "REJECTED").length;
  return (
    <>
      <PageHeader
        eyebrow="Operations · Scanning"
        title="Scanner console"
        description="Keyboard-first parcel processing with recorded refusals, persistent focus, and immediate work instructions."
        actions={
          <div className="flex items-center gap-2 rounded-md border bg-white px-3 py-2">
            <span className="text-xs font-semibold text-slate-600">
              {sound ? (
                <Volume2 aria-hidden className="inline h-4 w-4" />
              ) : (
                <VolumeX aria-hidden className="inline h-4 w-4" />
              )}{" "}
              Sound
            </span>
            <Switch
              label="Audible scan feedback"
              checked={sound}
              onCheckedChange={setSound}
            />
          </div>
        }
      />
      <OperationalMetricStrip
        items={[
          {
            label: "Accepted this session",
            value: accepted,
            tone: "success",
            icon: CheckCircle2,
          },
          {
            label: "Duplicates",
            value: duplicate,
            tone: "warning",
            icon: ClipboardCheck,
          },
          {
            label: "Rejected",
            value: rejected,
            tone: rejected ? "danger" : "neutral",
            icon: SearchX,
          },
          { label: "Total scans", value: session.length, icon: ScanLine },
        ]}
      />
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.45fr)_minmax(320px,0.55fr)]">
        <Panel>
          <PanelHeader
            title="Operation mode"
            description="Alt + number changes mode without leaving the scanner."
          />
          <div className="grid grid-cols-2 gap-2 border-b p-4 sm:grid-cols-4">
            {availableModes.map((item) => (
              <button
                key={item.type}
                onClick={() => {
                  setMode(item.type);
                  scannerRef.current?.focus();
                }}
                className={`rounded-md border px-3 py-2.5 text-left text-sm font-semibold ${mode === item.type ? "border-primary bg-emerald-50 text-primary" : "border-border bg-white text-slate-600 hover:bg-slate-50"}`}
              >
                <span className="mr-2 rounded border bg-white px-1.5 py-0.5 font-mono text-[10px]">
                  Alt {item.key}
                </span>
                {item.label}
              </button>
            ))}
          </div>
          <div className="space-y-4 p-4">
            <Field
              label="Current facility"
              htmlFor="scan-facility"
              hint="Required when your role covers more than one facility."
            >
              <Input
                id="scan-facility"
                value={facilityId}
                onChange={(event) => setFacilityId(event.target.value)}
                placeholder="ou_…"
              />
            </Field>
            {["HOLD", "DAMAGE", "EXCEPTION"].includes(mode) ? (
              <Field label="Reason" htmlFor="scan-reason" required>
                <Input
                  id="scan-reason"
                  value={reason}
                  onChange={(event) => setReason(event.target.value)}
                  placeholder="Describe the operational reason"
                />
              </Field>
            ) : null}
            <ScannerInput
              ref={scannerRef}
              value={barcode}
              onChange={setBarcode}
              onScan={(value) => mutation.mutate(value)}
              busy={mutation.isPending}
              label={`${titleCase(mode)} scan`}
              hint={`Mode ${titleCase(mode)} · Enter submits · field refocuses after every response`}
            />
            <ScanFeedback result={session[0]} />
          </div>
        </Panel>
        <Panel>
          <PanelHeader
            title="Session results"
            description="Latest first; recorded by the server."
          />
          {session.length ? (
            <div className="max-h-[540px] divide-y overflow-y-auto">
              {session.map((item, index) => (
                <div
                  key={`${item.scanId}-${index}`}
                  className="flex items-start justify-between gap-3 p-3"
                >
                  <div>
                    <strong className="font-mono text-sm">
                      {item.awb ?? item.barcode}
                    </strong>
                    <p className="mt-0.5 text-xs text-slate-500">
                      {item.nextAction ||
                        item.rejectionMessage ||
                        `${titleCase(item.fromStatus ?? "")} → ${titleCase(item.toStatus ?? "")}`}
                    </p>
                  </div>
                  <Badge
                    tone={
                      item.outcome === "ACCEPTED"
                        ? "success"
                        : item.outcome === "DUPLICATE"
                          ? "warning"
                          : "danger"
                    }
                  >
                    {titleCase(item.outcome ?? "")}
                  </Badge>
                </div>
              ))}
            </div>
          ) : (
            <EmptyState
              icon={ScanLine}
              title="No scans yet"
              description="The first server response will appear here."
            />
          )}
        </Panel>
      </div>
      <Panel className="mt-4">
        <PanelHeader
          title="Recorded scan history"
          description="Includes rejected scans so supervisors can identify recurring custody or routing problems."
          actions={
            <Button size="sm" onClick={() => void history.refetch()}>
              Refresh
            </Button>
          }
        />
        {history.isLoading ? (
          <LoadingState label="Loading scan history" />
        ) : history.error ? (
          <ErrorState
            error={history.error}
            retry={() => void history.refetch()}
          />
        ) : history.data?.data?.length ? (
          <DataTable label="Scan history">
            <thead>
              <tr>
                <TableHead>Time</TableHead>
                <TableHead>AWB / barcode</TableHead>
                <TableHead>Operation</TableHead>
                <TableHead>State movement</TableHead>
                <TableHead>Facility</TableHead>
                <TableHead>Result</TableHead>
              </tr>
            </thead>
            <tbody>
              {history.data.data.map((item) => (
                <tr key={item.id}>
                  <TableCell>{formatDateTime(item.occurredAt)}</TableCell>
                  <TableCell>
                    <span className="font-mono font-semibold">
                      {item.awb ?? item.barcode}
                    </span>
                  </TableCell>
                  <TableCell>{titleCase(item.scanType ?? "")}</TableCell>
                  <TableCell>
                    {titleCase(item.fromStatus ?? "")} →{" "}
                    {titleCase(item.toStatus ?? "")}
                  </TableCell>
                  <TableCell>{item.facility?.code}</TableCell>
                  <TableCell>
                    <Badge
                      tone={
                        item.outcome === "ACCEPTED"
                          ? "success"
                          : item.outcome === "DUPLICATE"
                            ? "warning"
                            : "danger"
                      }
                    >
                      {titleCase(item.outcome ?? "")}
                    </Badge>
                    {item.rejectionMessage ? (
                      <span className="mt-1 block max-w-xs text-xs text-slate-500">
                        {item.rejectionMessage}
                      </span>
                    ) : null}
                  </TableCell>
                </tr>
              ))}
            </tbody>
          </DataTable>
        ) : (
          <EmptyState
            title="No scan history"
            description="Scans at the selected facility and mode will appear here."
          />
        )}
      </Panel>
    </>
  );
}
