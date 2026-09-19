import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  CalendarClock,
  Camera,
  CheckCircle2,
  CircleAlert,
  Clock3,
  Download,
  FileCheck2,
  Image as ImageIcon,
  MapPin,
  PackageCheck,
  PackageOpen,
  RotateCcw,
  Search,
  ShieldCheck,
  Truck,
  UserRound,
} from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import { useForm } from "react-hook-form";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";
import { z } from "zod";
import {
  ApiError,
  apiRequest,
  operationalHeaders,
  queryString,
  type NDRAction,
  type NDRCase,
  type NDRCasePage,
  type ProofOfDelivery,
  type RTOCase,
  type RTOCasePage,
  type TrackingResult,
} from "../api/client";
import { useAuth } from "../auth/AuthProvider";
import { useToast } from "../components/ToastProvider";
import {
  CursorPager,
  EntityLink,
  JourneyTimeline,
  OperationalMetricStrip,
} from "../components/operations";
import {
  Badge,
  Button,
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
import { formatDateTime, formatMoney, titleCase } from "../lib/utils";

const ndrQueues: Array<{ label: string; status?: string; action?: NDRAction }> =
  [
    { label: "New", status: "OPEN" },
    { label: "Contact required", action: "CONTACT_REQUIRED" },
    { label: "Rescheduled", action: "RESCHEDULE" },
    { label: "Reattempt", action: "REATTEMPT" },
    { label: "Escalated", action: "ESCALATE" },
    { label: "RTO candidate", action: "RTO" },
  ];

export function NDRWorkbenchPage() {
  const [queue, setQueue] = useState(0);
  const [branchId, setBranchId] = useState("");
  const [reasonCode, setReasonCode] = useState("");
  const [cursorStack, setCursorStack] = useState<(string | undefined)[]>([
    undefined,
  ]);
  const cursor = cursorStack.at(-1);
  const selected = ndrQueues[queue] ?? ndrQueues[0]!;
  const query = useQuery({
    queryKey: ["ndr", { cursor, queue, branchId, reasonCode }],
    queryFn: () =>
      apiRequest<NDRCasePage>(
        `/api/v1/ndr${queryString({ cursor, limit: 25, status: selected.status, action: selected.action, branchId, reasonCode })}`,
      ),
    placeholderData: (previous) => previous,
  });
  const rows = query.data?.data ?? [];
  return (
    <>
      <PageHeader
        eyebrow="Operations · NDR"
        title="NDR workbench"
        description="Turn every failed attempt into one clear next instruction, with the full customer-contact history preserved."
      />
      <div
        className="mb-4 flex gap-1 overflow-x-auto"
        role="tablist"
        aria-label="NDR queues"
      >
        {ndrQueues.map((item, index) => (
          <Button
            key={item.label}
            size="sm"
            variant={queue === index ? "primary" : "secondary"}
            onClick={() => {
              setQueue(index);
              setCursorStack([undefined]);
            }}
          >
            {item.label}
          </Button>
        ))}
      </div>
      <Panel>
        <div className="grid gap-3 border-b p-4 sm:grid-cols-2">
          <Input
            value={branchId}
            onChange={(event) => {
              setBranchId(event.target.value);
              setCursorStack([undefined]);
            }}
            placeholder="Branch ID"
            aria-label="Filter by branch"
          />
          <Input
            value={reasonCode}
            onChange={(event) => {
              setReasonCode(event.target.value.toUpperCase());
              setCursorStack([undefined]);
            }}
            placeholder="Reason code"
            aria-label="Filter by NDR reason"
          />
        </div>
        {query.isLoading ? (
          <LoadingState label="Loading NDR queue" />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : rows.length ? (
          <>
            <DataTable label={`${selected.label} NDR cases`}>
              <thead>
                <tr>
                  <TableHead>Case</TableHead>
                  <TableHead>Shipment</TableHead>
                  <TableHead>Reason</TableHead>
                  <TableHead>Next action</TableHead>
                  <TableHead>Attempts</TableHead>
                  <TableHead>Next attempt</TableHead>
                  <TableHead>Branch</TableHead>
                  <TableHead>Status</TableHead>
                </tr>
              </thead>
              <tbody>
                {rows.map((item) => (
                  <tr key={item.id}>
                    <TableCell>
                      <EntityLink
                        to={`/operations/ndr/${item.id}`}
                        primary={item.caseCode}
                        secondary={formatDateTime(item.openedAt)}
                      />
                    </TableCell>
                    <TableCell>
                      <EntityLink
                        to={`/shipments/${item.shipmentId}`}
                        primary={item.awb}
                        secondary={item.recipientName}
                      />
                    </TableCell>
                    <TableCell>
                      {item.reasonName}
                      <span className="block text-xs text-slate-500">
                        {item.currentReasonCode}
                      </span>
                    </TableCell>
                    <TableCell>
                      <Badge
                        tone={
                          item.currentAction === "RTO" ||
                          item.currentAction === "ESCALATE"
                            ? "warning"
                            : "info"
                        }
                      >
                        {titleCase(item.currentAction ?? "Unassigned")}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      {item.attemptCount ?? 0} / {item.maxAttempts ?? 0}
                    </TableCell>
                    <TableCell>{formatDateTime(item.nextAttemptAt)}</TableCell>
                    <TableCell>{item.branch?.code}</TableCell>
                    <TableCell>
                      <StatusBadge status={item.status} />
                    </TableCell>
                  </tr>
                ))}
              </tbody>
            </DataTable>
            <CursorPager
              page={cursorStack.length}
              count={rows.length}
              noun="cases"
              hasMore={query.data?.pagination?.hasMore}
              nextCursor={query.data?.pagination?.nextCursor}
              onPrevious={() => setCursorStack((items) => items.slice(0, -1))}
              onNext={(next) => setCursorStack((items) => [...items, next])}
            />
          </>
        ) : (
          <EmptyState
            title={`${selected.label} queue is clear`}
            description="No NDR case matches the selected branch and reason filters."
          />
        )}
      </Panel>
    </>
  );
}

export function NDRDetailPage() {
  const { caseId = "" } = useParams();
  const { hasPermission } = useAuth();
  const [actionOpen, setActionOpen] = useState(false);
  const query = useQuery({
    queryKey: ["ndr-case", caseId],
    queryFn: () => apiRequest<NDRCase>(`/api/v1/ndr/${caseId}`),
    enabled: Boolean(caseId),
  });
  if (query.isLoading) return <LoadingState label="Loading NDR case" />;
  if (query.error)
    return (
      <ErrorState error={query.error} retry={() => void query.refetch()} />
    );
  const item = query.data;
  if (!item)
    return (
      <EmptyState
        title="NDR case not found"
        description="Check the case identifier."
      />
    );
  const availableActions = (item.availableActions ?? []).filter(
    (action) => action !== "RTO" || hasPermission("rto.manage"),
  );
  const timeline = [
    ...(item.attempts ?? []).map((attempt) => ({
      key: attempt.id ?? `attempt-${attempt.attemptNumber}`,
      title: `Delivery attempt ${attempt.attemptNumber}`,
      description: `${attempt.reasonCode ?? "Reason not recorded"}${attempt.remarks ? ` · ${attempt.remarks}` : ""}`,
      occurredAt: attempt.occurredAt,
      status: "ATTEMPT",
    })),
    ...(item.actions ?? []).map((action) => ({
      key: action.id ?? `action-${action.sequence}`,
      title: `${titleCase(action.action ?? "Action")} instruction`,
      description:
        action.instructions ||
        action.contactNotes ||
        `Requested by ${titleCase(action.requestedBy ?? "operations")}`,
      occurredAt: action.createdAt,
      status: action.status,
    })),
  ].sort(
    (a, b) =>
      new Date(a.occurredAt ?? 0).getTime() -
      new Date(b.occurredAt ?? 0).getTime(),
  );
  return (
    <>
      <PageHeader
        eyebrow="Operations · NDR"
        title={item.caseCode ?? "NDR case"}
        description={`${item.awb} · ${item.reasonName ?? item.currentReasonCode} · opened ${formatDateTime(item.openedAt)}`}
        actions={
          <>
            <StatusBadge status={item.status} />
            {hasPermission("ndr.manage") && availableActions.length ? (
              <Button variant="primary" onClick={() => setActionOpen(true)}>
                Set next action
              </Button>
            ) : null}
          </>
        }
      />
      <InlineNotice
        tone={
          item.currentAction === "RTO" || item.currentAction === "ESCALATE"
            ? "warning"
            : "info"
        }
        title="Next required action"
      >
        {item.currentAction
          ? titleCase(item.currentAction)
          : "Choose an action for this case."}
        {item.nextAttemptAt
          ? ` · scheduled ${formatDateTime(item.nextAttemptAt)}`
          : ""}
      </InlineNotice>
      <OperationalMetricStrip
        items={[
          {
            label: "Attempts",
            value: `${item.attemptCount ?? 0} / ${item.maxAttempts ?? 0}`,
            icon: Truck,
          },
          {
            label: "Reason",
            value: item.reasonName ?? titleCase(item.currentReasonCode ?? ""),
            icon: CircleAlert,
          },
          {
            label: "Current action",
            value: titleCase(item.currentAction ?? "None"),
            tone: "warning",
            icon: CalendarClock,
          },
          {
            label: "Next attempt",
            value: formatDateTime(item.nextAttemptAt),
            icon: Clock3,
          },
          { label: "Branch", value: item.branch?.code, icon: MapPin },
          {
            label: "Shipment state",
            value: titleCase(item.shipmentStatus ?? ""),
            icon: PackageCheck,
          },
        ]}
      />
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.3fr)_minmax(320px,0.7fr)]">
        <Panel>
          <PanelHeader
            title="Case timeline"
            description="Attempts and operator instructions are append-only."
          />
          {timeline.length ? (
            <div className="p-5">
              <JourneyTimeline items={timeline} />
            </div>
          ) : (
            <EmptyState
              title="No case history"
              description="The first delivery attempt will appear here."
            />
          )}
        </Panel>
        <div className="space-y-4">
          <Panel>
            <PanelHeader title="Case summary" />
            <dl className="space-y-3 p-4 text-sm">
              <div>
                <dt className="text-slate-500">Reason</dt>
                <dd className="font-semibold">
                  {item.reasonName ?? item.currentReasonCode}
                </dd>
              </div>
              <div>
                <dt className="text-slate-500">Reason category</dt>
                <dd>{titleCase(item.reasonCategory ?? "")}</dd>
              </div>
              <div>
                <dt className="text-slate-500">Customer responsibility</dt>
                <dd>
                  {item.isCustomerFault ? "Customer-related" : "Operational"}
                </dd>
              </div>
              <div>
                <dt className="text-slate-500">Available actions</dt>
                <dd className="mt-1 flex flex-wrap gap-1">
                  {availableActions.map((action) => (
                    <Badge key={action}>{titleCase(action)}</Badge>
                  ))}
                </dd>
              </div>
            </dl>
          </Panel>
          {item.correctedAddress ? (
            <Panel>
              <PanelHeader
                title="Corrected delivery address"
                description="Stored on the case; the booking snapshot is unchanged."
              />
              <address className="p-4 text-sm not-italic leading-6">
                {item.correctedAddress.line1}
                <br />
                {item.correctedAddress.line2 ? (
                  <>
                    {item.correctedAddress.line2}
                    <br />
                  </>
                ) : null}
                {item.correctedAddress.landmark ? (
                  <>
                    {item.correctedAddress.landmark}
                    <br />
                  </>
                ) : null}
                {item.correctedAddress.pincode}
                <br />
                {item.correctedAddress.phone}
              </address>
            </Panel>
          ) : null}
        </div>
      </div>
      <NDRActionDialog
        item={{ ...item, availableActions }}
        open={actionOpen}
        onOpenChange={setActionOpen}
      />
    </>
  );
}

const ndrActionSchema = z
  .object({
    action: z.enum([
      "REATTEMPT",
      "RESCHEDULE",
      "CONTACT_REQUIRED",
      "ADDRESS_CORRECTION",
      "CUSTOMER_PICKUP",
      "RTO",
      "ESCALATE",
    ]),
    requestedBy: z.enum([
      "OPERATIONS",
      "CUSTOMER",
      "CONSIGNEE",
      "SYSTEM",
      "PARTNER",
    ]),
    instructions: z.string().optional(),
    scheduledFor: z.string().optional(),
    contactName: z.string().optional(),
    contactPhone: z.string().optional(),
    contactNotes: z.string().optional(),
    correctedLine1: z.string().optional(),
    correctedLine2: z.string().optional(),
    correctedLandmark: z.string().optional(),
    correctedPincode: z.string().optional(),
    correctedPhone: z.string().optional(),
    assignToUserId: z.string().optional(),
  })
  .superRefine((value, context) => {
    if (
      ["RESCHEDULE", "REATTEMPT"].includes(value.action) &&
      !value.scheduledFor
    )
      context.addIssue({
        code: "custom",
        path: ["scheduledFor"],
        message: "Schedule is required",
      });
    if (
      value.action === "ADDRESS_CORRECTION" &&
      (!value.correctedLine1 || !value.correctedPincode)
    )
      context.addIssue({
        code: "custom",
        path: ["correctedLine1"],
        message: "Corrected address line and postal code are required",
      });
  });
function NDRActionDialog({
  item,
  open,
  onOpenChange,
}: {
  item: NDRCase;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const client = useQueryClient();
  const { toast } = useToast();
  const form = useForm<z.infer<typeof ndrActionSchema>>({
    resolver: zodResolver(ndrActionSchema),
    defaultValues: {
      action: item.availableActions?.[0] ?? "CONTACT_REQUIRED",
      requestedBy: "OPERATIONS",
    },
  });
  const action = form.watch("action");
  const mutation = useMutation({
    mutationFn: (values: z.infer<typeof ndrActionSchema>) =>
      apiRequest<NDRCase>(`/api/v1/ndr/${item.id}/action`, {
        method: "POST",
        body: {
          ...values,
          scheduledFor: values.scheduledFor
            ? new Date(values.scheduledFor).toISOString()
            : undefined,
        },
      }),
    onSuccess: (updated) => {
      void client.invalidateQueries({ queryKey: ["ndr-case", item.id] });
      void client.invalidateQueries({ queryKey: ["ndr"] });
      toast({
        tone: "success",
        title: "Next action set",
        description: titleCase(updated.currentAction ?? ""),
      });
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Set next NDR action"
      description="This instruction supersedes any pending one, so the field agent sees only one decision."
      footer={
        <>
          <Button onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button
            variant={action === "RTO" ? "danger" : "primary"}
            onClick={() =>
              void form.handleSubmit((values) => mutation.mutate(values))()
            }
          >
            Set action
          </Button>
        </>
      }
    >
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Action" htmlFor="ndr-action" required>
          <Select id="ndr-action" {...form.register("action")}>
            {item.availableActions?.map((value) => (
              <option key={value}>{value}</option>
            ))}
          </Select>
        </Field>
        <Field label="Requested by" htmlFor="ndr-requester">
          <Select id="ndr-requester" {...form.register("requestedBy")}>
            <option>OPERATIONS</option>
            <option>CUSTOMER</option>
            <option>CONSIGNEE</option>
            <option>PARTNER</option>
          </Select>
        </Field>
        {["RESCHEDULE", "REATTEMPT"].includes(action) ? (
          <Field
            label="Scheduled for"
            htmlFor="ndr-schedule"
            required
            error={form.formState.errors.scheduledFor?.message}
          >
            <Input
              id="ndr-schedule"
              type="datetime-local"
              {...form.register("scheduledFor")}
            />
          </Field>
        ) : null}
        {["CONTACT_REQUIRED", "RESCHEDULE", "REATTEMPT"].includes(action) ? (
          <>
            <Field label="Contact name" htmlFor="ndr-contact">
              <Input id="ndr-contact" {...form.register("contactName")} />
            </Field>
            <Field label="Contact phone" htmlFor="ndr-phone">
              <Input id="ndr-phone" {...form.register("contactPhone")} />
            </Field>
            <Field
              className="sm:col-span-2"
              label="Contact notes"
              htmlFor="ndr-contact-notes"
            >
              <Textarea
                id="ndr-contact-notes"
                {...form.register("contactNotes")}
              />
            </Field>
          </>
        ) : null}
        {action === "ADDRESS_CORRECTION" ? (
          <>
            <Field
              label="Corrected address"
              htmlFor="ndr-corrected-line"
              required
              error={form.formState.errors.correctedLine1?.message}
            >
              <Input
                id="ndr-corrected-line"
                {...form.register("correctedLine1")}
              />
            </Field>
            <Field label="Postal code" htmlFor="ndr-corrected-pin" required>
              <Input
                id="ndr-corrected-pin"
                {...form.register("correctedPincode")}
              />
            </Field>
            <Field label="Landmark" htmlFor="ndr-corrected-landmark">
              <Input
                id="ndr-corrected-landmark"
                {...form.register("correctedLandmark")}
              />
            </Field>
            <Field label="Phone" htmlFor="ndr-corrected-phone">
              <Input
                id="ndr-corrected-phone"
                {...form.register("correctedPhone")}
              />
            </Field>
          </>
        ) : null}
        <Field
          className="sm:col-span-2"
          label="Instructions"
          htmlFor="ndr-instructions"
        >
          <Textarea id="ndr-instructions" {...form.register("instructions")} />
        </Field>
        {action === "RTO" ? (
          <div className="sm:col-span-2">
            <InlineNotice tone="warning" title="Starts the return workflow">
              RTO resolves this NDR case and flips the shipment to reverse
              movement. Forward delivery transitions will be refused.
            </InlineNotice>
          </div>
        ) : null}
      </div>
      {mutation.error ? <ErrorState error={mutation.error} /> : null}
    </Dialog>
  );
}

const rtoStatuses = [
  "INITIATED",
  "IN_TRANSIT",
  "AT_ORIGIN_BRANCH",
  "OUT_FOR_RETURN",
  "RETURNED",
  "RETURN_FAILED",
  "DISPOSED",
  "CANCELLED",
];
export function RTOPage() {
  const { hasPermission } = useAuth();
  const [status, setStatus] = useState("");
  const [cursorStack, setCursorStack] = useState<(string | undefined)[]>([
    undefined,
  ]);
  const [createOpen, setCreateOpen] = useState(false);
  const cursor = cursorStack.at(-1);
  const query = useQuery({
    queryKey: ["rto", { cursor, status }],
    queryFn: () =>
      apiRequest<RTOCasePage>(
        `/api/v1/rto${queryString({ cursor, limit: 25, status })}`,
      ),
    placeholderData: (previous) => previous,
  });
  const rows = query.data?.data ?? [];
  const [now] = useState(Date.now);
  return (
    <>
      <PageHeader
        eyebrow="Operations · RTO"
        title="Return journeys"
        description="Reverse network movement from initiation through handover to the original sender."
        actions={
          hasPermission("rto.manage") ? (
            <Button variant="primary" onClick={() => setCreateOpen(true)}>
              Initiate RTO
            </Button>
          ) : undefined
        }
      />
      <OperationalMetricStrip
        items={[
          { label: "Visible", value: rows.length, icon: RotateCcw },
          {
            label: "Initiated",
            value: rows.filter((item) => item.status === "INITIATED").length,
            icon: PackageOpen,
          },
          {
            label: "In transit",
            value: rows.filter((item) => item.status === "IN_TRANSIT").length,
            tone: "info",
            icon: Truck,
          },
          {
            label: "At origin",
            value: rows.filter((item) => item.status === "AT_ORIGIN_BRANCH")
              .length,
            tone: "warning",
            icon: MapPin,
          },
          {
            label: "Returned",
            value: rows.filter((item) => item.status === "RETURNED").length,
            tone: "success",
            icon: CheckCircle2,
          },
          {
            label: "Aged 7d+ (visible)",
            value: rows.filter(
              (item) =>
                item.initiatedAt &&
                now - new Date(item.initiatedAt).getTime() >= 7 * 86400_000 &&
                item.status !== "RETURNED",
            ).length,
            tone: "danger",
            icon: Clock3,
          },
        ]}
      />
      <Panel>
        <div className="border-b p-4">
          <Select
            className="max-w-sm"
            value={status}
            onChange={(event) => {
              setStatus(event.target.value);
              setCursorStack([undefined]);
            }}
          >
            <option value="">All return stages</option>
            {rtoStatuses.map((item) => (
              <option key={item}>{item}</option>
            ))}
          </Select>
        </div>
        {query.isLoading ? (
          <LoadingState label="Loading returns" />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : rows.length ? (
          <>
            <DataTable label="RTO cases">
              <thead>
                <tr>
                  <TableHead>Return</TableHead>
                  <TableHead>Shipment</TableHead>
                  <TableHead>Stage</TableHead>
                  <TableHead>Origin branch</TableHead>
                  <TableHead>Reason</TableHead>
                  <TableHead>Indicative charge</TableHead>
                  <TableHead>Aging</TableHead>
                </tr>
              </thead>
              <tbody>
                {rows.map((item) => (
                  <tr key={item.id}>
                    <TableCell>
                      <EntityLink
                        to={`/operations/rto/${item.id}`}
                        primary={item.caseCode}
                      />
                    </TableCell>
                    <TableCell>
                      <EntityLink
                        to={`/shipments/${item.shipmentId}`}
                        primary={item.awb}
                      />
                    </TableCell>
                    <TableCell>
                      <StatusBadge status={item.status} />
                    </TableCell>
                    <TableCell>{item.returnBranch?.code}</TableCell>
                    <TableCell>{item.reasonCode}</TableCell>
                    <TableCell>
                      {formatMoney(item.rtoChargeMinor, item.currency)}
                      <span className="block text-xs text-slate-500">
                        {titleCase(item.chargeBearer ?? "")}
                      </span>
                    </TableCell>
                    <TableCell>
                      {item.initiatedAt
                        ? Math.floor(
                            (now - new Date(item.initiatedAt).getTime()) /
                              86400_000,
                          )
                        : 0}{" "}
                      days
                    </TableCell>
                  </tr>
                ))}
              </tbody>
            </DataTable>
            <CursorPager
              page={cursorStack.length}
              count={rows.length}
              noun="returns"
              hasMore={query.data?.pagination?.hasMore}
              nextCursor={query.data?.pagination?.nextCursor}
              onPrevious={() => setCursorStack((items) => items.slice(0, -1))}
              onNext={(next) => setCursorStack((items) => [...items, next])}
            />
          </>
        ) : (
          <EmptyState
            title="No return journeys"
            description="No RTO case matches this stage."
          />
        )}
      </Panel>
      <InitiateRTODialog open={createOpen} onOpenChange={setCreateOpen} />
    </>
  );
}

const initiateSchema = z.object({
  barcode: z.string().min(1),
  reasonCode: z.string().optional(),
  notes: z.string().min(5),
});
function InitiateRTODialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const navigate = useNavigate();
  const client = useQueryClient();
  const { toast } = useToast();
  const form = useForm<z.infer<typeof initiateSchema>>({
    resolver: zodResolver(initiateSchema),
  });
  const mutation = useMutation({
    mutationFn: (body: z.infer<typeof initiateSchema>) =>
      apiRequest<RTOCase>("/api/v1/rto", { method: "POST", body }),
    onSuccess: (item) => {
      void client.invalidateQueries({ queryKey: ["rto"] });
      toast({
        tone: "success",
        title: "Return initiated",
        description: item.caseCode,
      });
      onOpenChange(false);
      if (item.id) void navigate(`/operations/rto/${item.id}`);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Initiate return to sender"
      description="The server resolves the reverse route and flips the parcel to reverse movement."
      footer={
        <>
          <Button onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button
            variant="danger"
            onClick={() =>
              void form.handleSubmit((values) => mutation.mutate(values))()
            }
          >
            Initiate RTO
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <Field label="AWB or barcode" htmlFor="rto-barcode" required>
          <Input id="rto-barcode" {...form.register("barcode")} />
        </Field>
        <Field label="Reason code" htmlFor="rto-reason">
          <Input id="rto-reason" {...form.register("reasonCode")} />
        </Field>
        <Field label="Decision notes" htmlFor="rto-notes" required>
          <Textarea id="rto-notes" {...form.register("notes")} />
        </Field>
      </div>
      {mutation.error ? <ErrorState error={mutation.error} /> : null}
    </Dialog>
  );
}

export function RTODetailPage() {
  const { caseId = "" } = useParams();
  const { hasPermission } = useAuth();
  const client = useQueryClient();
  const { toast } = useToast();
  const [facilityId, setFacilityId] = useState("");
  const [completeOpen, setCompleteOpen] = useState(false);
  const query = useQuery({
    queryKey: ["rto-case", caseId],
    queryFn: () => apiRequest<RTOCase>(`/api/v1/rto/${caseId}`),
    enabled: Boolean(caseId),
  });
  const action = useMutation({
    mutationFn: (name: "dispatch" | "receive") =>
      name === "dispatch"
        ? apiRequest<RTOCase>(
            `/api/v1/rto/${caseId}/dispatch${queryString({ operatingUnitId: facilityId })}`,
            {
              method: "POST",
              body: {},
              headers: operationalHeaders("rto-dispatch"),
            },
          )
        : apiRequest<RTOCase>(
            `/api/v1/rto/receive${queryString({ operatingUnitId: facilityId })}`,
            {
              method: "POST",
              body: { barcode: query.data?.awb },
              headers: operationalHeaders("rto-receive"),
            },
          ),
    onSuccess: (item, name) => {
      void client.invalidateQueries({ queryKey: ["rto-case", caseId] });
      toast({
        tone: "success",
        title:
          name === "dispatch" ? "Return dispatched" : "Return leg received",
        description: item.caseCode,
      });
    },
  });
  if (query.isLoading) return <LoadingState label="Loading return" />;
  if (query.error)
    return (
      <ErrorState error={query.error} retry={() => void query.refetch()} />
    );
  const item = query.data;
  if (!item)
    return (
      <EmptyState
        title="Return not found"
        description="Check the RTO case identifier."
      />
    );
  const journey = [
    {
      key: "initiated",
      title: "RTO initiated",
      occurredAt: item.initiatedAt,
      status: "COMPLETED",
    },
    ...(item.legs ?? []).map((leg) => ({
      key: String(leg.sequence),
      title: `${leg.from?.name ?? leg.from?.code ?? "Facility"} → ${leg.to?.name ?? leg.to?.code ?? (leg.legType === "BRANCH_TO_SENDER" ? "Sender" : "Facility")}`,
      description: titleCase(leg.legType ?? "Return leg"),
      occurredAt: leg.completedAt ?? leg.startedAt,
      status: leg.status,
      current: leg.status === "IN_PROGRESS",
    })),
    {
      key: "returned",
      title: "Returned to sender",
      description: item.returnedToName,
      occurredAt: item.returnedAt,
      status: item.status === "RETURNED" ? "COMPLETED" : "PENDING",
    },
  ];
  return (
    <>
      <PageHeader
        eyebrow="Operations · RTO"
        title={item.caseCode ?? "Return journey"}
        description={`${item.awb} · ${item.reasonCode ?? "Return to sender"}`}
        actions={<StatusBadge status={item.status} />}
      />
      <InlineNotice tone="warning" title="Reverse movement">
        This parcel cannot re-enter forward delivery. Progress is shown by
        return legs, not shipment status changes.
      </InlineNotice>
      <OperationalMetricStrip
        items={[
          { label: "Shipment", value: item.awb, icon: PackageCheck },
          {
            label: "Return branch",
            value: item.returnBranch?.code,
            icon: MapPin,
          },
          {
            label: "Current stage",
            value: titleCase(item.status ?? ""),
            tone: "warning",
            icon: RotateCcw,
          },
          {
            label: "Route source",
            value: titleCase(item.routeResolutionSource ?? ""),
            icon: ShieldCheck,
          },
          {
            label: "Indicative charge",
            value: formatMoney(item.rtoChargeMinor, item.currency),
            icon: FileCheck2,
          },
          {
            label: "Charge bearer",
            value: titleCase(item.chargeBearer ?? ""),
            icon: UserRound,
          },
        ]}
      />
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.3fr)_minmax(320px,0.7fr)]">
        <Panel>
          <PanelHeader title="Reverse courier journey" />
          <div className="p-5">
            <JourneyTimeline items={journey} />
          </div>
        </Panel>
        <div className="space-y-4">
          <Panel>
            <PanelHeader
              title="Return address"
              description="From the sender's immutable booking snapshot."
            />
            <address className="p-4 text-sm not-italic leading-6">
              <strong>{item.returnAddress?.contactName}</strong>
              <br />
              {item.returnAddress?.line1}
              <br />
              {item.returnAddress?.line2 ? (
                <>
                  {item.returnAddress.line2}
                  <br />
                </>
              ) : null}
              {item.returnAddress?.city}, {item.returnAddress?.state}{" "}
              {item.returnAddress?.pincode}
              <br />
              {item.returnAddress?.phone}
            </address>
          </Panel>
          {hasPermission("rto.manage") ? (
            <Panel>
              <PanelHeader title="Return action" />
              <div className="space-y-3 p-4">
                <Field label="Current facility" htmlFor="rto-facility">
                  <Input
                    id="rto-facility"
                    value={facilityId}
                    onChange={(event) => setFacilityId(event.target.value)}
                    placeholder="ou_…"
                  />
                </Field>
                {item.status === "INITIATED" ? (
                  <Button
                    className="w-full"
                    variant="primary"
                    disabled={!facilityId}
                    onClick={() => action.mutate("dispatch")}
                  >
                    Dispatch return
                  </Button>
                ) : null}
                {item.status === "IN_TRANSIT" ? (
                  <Button
                    className="w-full"
                    variant="primary"
                    disabled={!facilityId}
                    onClick={() => action.mutate("receive")}
                  >
                    Receive return leg
                  </Button>
                ) : null}
                {[
                  "AT_ORIGIN_BRANCH",
                  "OUT_FOR_RETURN",
                  "RETURN_FAILED",
                ].includes(item.status ?? "") ? (
                  <Button
                    className="w-full"
                    variant="primary"
                    disabled={!facilityId}
                    onClick={() => setCompleteOpen(true)}
                  >
                    Complete sender handover
                  </Button>
                ) : null}
                {action.error ? <ErrorState error={action.error} /> : null}
              </div>
            </Panel>
          ) : null}
        </div>
      </div>
      <CompleteRTODialog
        item={item}
        facilityId={facilityId}
        open={completeOpen}
        onOpenChange={setCompleteOpen}
      />
    </>
  );
}

const completeRTOSchema = z
  .object({
    outcome: z.enum(["RETURNED", "FAILED"]),
    receivedBy: z.string().optional(),
    remarks: z.string().optional(),
  })
  .superRefine((value, context) => {
    if (value.outcome === "RETURNED" && !value.receivedBy?.trim())
      context.addIssue({
        code: "custom",
        path: ["receivedBy"],
        message: "Recipient name is required",
      });
  });
function CompleteRTODialog({
  item,
  facilityId,
  open,
  onOpenChange,
}: {
  item: RTOCase;
  facilityId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const client = useQueryClient();
  const { toast } = useToast();
  const form = useForm<z.infer<typeof completeRTOSchema>>({
    resolver: zodResolver(completeRTOSchema),
    defaultValues: { outcome: "RETURNED" },
  });
  const mutation = useMutation({
    mutationFn: (body: z.infer<typeof completeRTOSchema>) =>
      apiRequest<RTOCase>(
        `/api/v1/rto/${item.id}/complete${queryString({ operatingUnitId: facilityId })}`,
        { method: "POST", body, headers: operationalHeaders("rto-complete") },
      ),
    onSuccess: (result) => {
      void client.invalidateQueries({ queryKey: ["rto-case", item.id] });
      toast({
        tone: result.status === "RETURNED" ? "success" : "info",
        title: titleCase(result.status ?? "Return updated"),
        description: result.awb,
      });
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Complete sender handover"
      description="A failed handover keeps the case open; the parcel remains the network's responsibility."
      footer={
        <>
          <Button onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button
            variant="primary"
            onClick={() =>
              void form.handleSubmit((values) => mutation.mutate(values))()
            }
          >
            Record outcome
          </Button>
        </>
      }
    >
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Outcome" htmlFor="rto-outcome">
          <Select id="rto-outcome" {...form.register("outcome")}>
            <option>RETURNED</option>
            <option>FAILED</option>
          </Select>
        </Field>
        <Field
          label="Received by"
          htmlFor="rto-received-by"
          error={form.formState.errors.receivedBy?.message}
        >
          <Input id="rto-received-by" {...form.register("receivedBy")} />
        </Field>
        <Field
          className="sm:col-span-2"
          label="Remarks"
          htmlFor="rto-complete-remarks"
        >
          <Textarea id="rto-complete-remarks" {...form.register("remarks")} />
        </Field>
      </div>
      {mutation.error ? <ErrorState error={mutation.error} /> : null}
    </Dialog>
  );
}

const podSchema = z.object({
  barcode: z.string().min(1),
  podType: z.enum(["DELIVERY", "RTO_RETURN", "CUSTOMER_PICKUP", "HANDOVER"]),
  recipientName: z.string().min(2),
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
  recipientIdType: z.string().optional(),
  recipientIdNumber: z.string().optional(),
  facilityId: z.string().optional(),
  remarks: z.string().optional(),
});
export function SubmitPODPage() {
  const [search] = useSearchParams();
  const navigate = useNavigate();
  const { toast } = useToast();
  const form = useForm<z.infer<typeof podSchema>>({
    resolver: zodResolver(podSchema),
    defaultValues: {
      barcode: search.get("awb") ?? "",
      podType: "DELIVERY",
      recipientRelationship: "SELF",
    },
  });
  const [signature, setSignature] = useState<File>();
  const [photo, setPhoto] = useState<File>();
  const [document, setDocument] = useState<File>();
  const mutation = useMutation({
    mutationFn: (values: z.infer<typeof podSchema>) => {
      if (!signature && !photo && !document)
        throw new Error("Attach at least one signature, photo, or document.");
      const data = new FormData();
      Object.entries(values).forEach(([key, value]) => {
        if (value && key !== "facilityId") data.append(key, value);
      });
      if (signature) data.append("signature", signature);
      if (photo) data.append("photo", photo);
      if (document) data.append("document", document);
      return apiRequest<ProofOfDelivery>(
        `/api/v1/pod${queryString({ operatingUnitId: values.facilityId })}`,
        {
          method: "POST",
          body: data,
          headers: operationalHeaders("pod-submit"),
        },
      );
    },
    onSuccess: (pod) => {
      toast({
        tone: "success",
        title: "Proof of delivery secured",
        description: `${pod.awb} · ${pod.artifacts?.length ?? 0} artifacts`,
      });
      if (pod.id) void navigate(`/operations/pod/${pod.id}`);
    },
  });
  return (
    <div className="mx-auto max-w-3xl">
      <PageHeader
        eyebrow="Operations · POD"
        title="Submit proof of delivery"
        description="Append-only delivery evidence. Files are private, checksummed, and type-checked from their actual bytes."
      />
      <Panel>
        <form
          className="grid gap-4 p-5 sm:grid-cols-2"
          onSubmit={(event) =>
            void form.handleSubmit((values) => mutation.mutate(values))(event)
          }
        >
          <Field label="AWB or barcode" htmlFor="pod-barcode" required>
            <Input id="pod-barcode" {...form.register("barcode")} />
          </Field>
          <Field label="POD type" htmlFor="pod-type">
            <Select id="pod-type" {...form.register("podType")}>
              <option>DELIVERY</option>
              <option>RTO_RETURN</option>
              <option>CUSTOMER_PICKUP</option>
              <option>HANDOVER</option>
            </Select>
          </Field>
          <Field label="Recipient name" htmlFor="pod-recipient" required>
            <Input id="pod-recipient" {...form.register("recipientName")} />
          </Field>
          <Field label="Relationship" htmlFor="pod-relationship">
            <Select
              id="pod-relationship"
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
          <Field label="Recipient phone" htmlFor="pod-phone">
            <Input id="pod-phone" {...form.register("recipientPhone")} />
          </Field>
          <Field label="Current facility" htmlFor="pod-facility">
            <Input
              id="pod-facility"
              {...form.register("facilityId")}
              placeholder="ou_…"
            />
          </Field>
          <Field label="ID type" htmlFor="pod-id-type">
            <Input id="pod-id-type" {...form.register("recipientIdType")} />
          </Field>
          <Field
            label="ID number"
            htmlFor="pod-id-number"
            hint="Masked before storage"
          >
            <Input id="pod-id-number" {...form.register("recipientIdNumber")} />
          </Field>
          <Field
            label="Signature image"
            htmlFor="pod-signature"
            hint="JPEG, PNG, WebP, or PDF"
          >
            <Input
              id="pod-signature"
              type="file"
              accept="image/jpeg,image/png,image/webp,application/pdf"
              onChange={(event) => setSignature(event.target.files?.[0])}
            />
          </Field>
          <Field label="Delivery photo" htmlFor="pod-photo">
            <Input
              id="pod-photo"
              type="file"
              accept="image/jpeg,image/png,image/webp"
              capture="environment"
              onChange={(event) => setPhoto(event.target.files?.[0])}
            />
          </Field>
          <Field label="Document" htmlFor="pod-document">
            <Input
              id="pod-document"
              type="file"
              accept="application/pdf,image/jpeg,image/png,image/webp"
              onChange={(event) => setDocument(event.target.files?.[0])}
            />
          </Field>
          <Field
            className="sm:col-span-2"
            label="Remarks"
            htmlFor="pod-remarks"
          >
            <Textarea id="pod-remarks" {...form.register("remarks")} />
          </Field>
          <div className="sm:col-span-2">
            <InlineNotice tone="info" title="Evidence cannot be edited">
              Confirm recipient and artifacts before submitting. Corrections
              require a new fact, not an edit.
            </InlineNotice>
          </div>
          {mutation.error ? (
            <div className="sm:col-span-2">
              <ErrorState error={mutation.error} />
            </div>
          ) : null}
          <div className="sm:col-span-2">
            <Button
              className="w-full"
              variant="primary"
              type="submit"
              disabled={mutation.isPending}
            >
              {mutation.isPending
                ? "Securing evidence…"
                : "Submit proof of delivery"}
            </Button>
          </div>
        </form>
      </Panel>
    </div>
  );
}

export function PODViewerPage() {
  const { podId = "" } = useParams();
  const query = useQuery({
    queryKey: ["pod", podId],
    queryFn: () => apiRequest<ProofOfDelivery>(`/api/v1/pod/${podId}`),
    enabled: Boolean(podId),
    retry: false,
  });
  if (query.isLoading)
    return <LoadingState label="Loading proof of delivery" />;
  if (query.error)
    return (
      <ErrorState error={query.error} retry={() => void query.refetch()} />
    );
  const pod = query.data;
  if (!pod)
    return (
      <EmptyState
        title="POD not found"
        description="The record may be unavailable or restricted."
      />
    );
  return (
    <>
      <PageHeader
        eyebrow="Operations · POD"
        title={`Proof of delivery · ${pod.awb}`}
        description={`${pod.recipientName} · ${formatDateTime(pod.deliveredAt)} · ${pod.facilityCode ?? "Authorized location"}`}
        actions={
          <Badge tone="success">
            <ShieldCheck aria-hidden className="h-3 w-3" /> Immutable
          </Badge>
        }
      />
      <OperationalMetricStrip
        items={[
          { label: "Recipient", value: pod.recipientName, icon: UserRound },
          {
            label: "Relationship",
            value: titleCase(pod.recipientRelationship ?? ""),
            icon: UserRound,
          },
          {
            label: "OTP",
            value: pod.otpVerified ? "Verified" : "Not recorded",
            tone: pod.otpVerified ? "success" : "neutral",
            icon: ShieldCheck,
          },
          {
            label: "Signature",
            value: pod.signatureCaptured ? "Captured" : "None",
            icon: FileCheck2,
          },
          {
            label: "Photos",
            value:
              pod.artifacts?.filter((item) => item.artifactType === "PHOTO")
                .length ?? 0,
            icon: Camera,
          },
          {
            label: "Recorded",
            value: formatDateTime(pod.recordedAt),
            icon: Clock3,
          },
        ]}
      />
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.3fr)_minmax(300px,0.7fr)]">
        <Panel>
          <PanelHeader
            title="Evidence artifacts"
            description="Load only when needed; retrieval is authorized and audited."
          />
          {pod.artifacts?.length ? (
            <div className="grid gap-4 p-4 sm:grid-cols-2">
              {pod.artifacts.map((artifact) => (
                <PODArtifactPreview
                  key={artifact.id}
                  podId={podId}
                  artifact={artifact}
                />
              ))}
            </div>
          ) : (
            <EmptyState
              title="No visible artifacts"
              description="Artifacts may be unavailable for this permission scope."
            />
          )}
        </Panel>
        <div className="space-y-4">
          <Panel>
            <PanelHeader title="Delivery verification" />
            <dl className="space-y-3 p-4 text-sm">
              <div>
                <dt className="text-slate-500">Delivered by</dt>
                <dd className="font-semibold">
                  {pod.deliveredBy?.name ?? pod.deliveredBy?.code}
                </dd>
              </div>
              <div>
                <dt className="text-slate-500">Recipient phone</dt>
                <dd>{pod.recipientPhone || "Not recorded"}</dd>
              </div>
              <div>
                <dt className="text-slate-500">Recipient ID</dt>
                <dd>
                  {pod.recipientIdType} {pod.recipientIdMasked}
                </dd>
              </div>
              <div>
                <dt className="text-slate-500">Location</dt>
                <dd>
                  {pod.latitude && pod.longitude
                    ? `${pod.latitude.toFixed(5)}, ${pod.longitude.toFixed(5)}`
                    : "Not recorded"}
                </dd>
              </div>
            </dl>
          </Panel>
          {pod.remarks ? (
            <Panel>
              <PanelHeader title="Remarks" />
              <p className="p-4 text-sm">{pod.remarks}</p>
            </Panel>
          ) : null}
        </div>
      </div>
    </>
  );
}

type Artifact = NonNullable<ProofOfDelivery["artifacts"]>[number];
function PODArtifactPreview({
  podId,
  artifact,
}: {
  podId: string;
  artifact: Artifact;
}) {
  const [url, setUrl] = useState<string>();
  const [error, setError] = useState<unknown>();
  const [loading, setLoading] = useState(false);
  useEffect(
    () => () => {
      if (url) URL.revokeObjectURL(url);
    },
    [url],
  );
  const load = async () => {
    setLoading(true);
    setError(undefined);
    try {
      const blob = await apiRequest<Blob>(
        `/api/v1/pod/${podId}/artifacts/${artifact.id}/download`,
        { responseType: "blob" },
      );
      const next = URL.createObjectURL(blob);
      setUrl((current) => {
        if (current) URL.revokeObjectURL(current);
        return next;
      });
    } catch (cause) {
      setError(cause);
    } finally {
      setLoading(false);
    }
  };
  const isImage = [
    "image/jpeg",
    "image/png",
    "image/webp",
    "image/gif",
  ].includes(artifact.mimeType?.toLowerCase() ?? "");
  return (
    <article className="overflow-hidden rounded-md border">
      <div className="flex aspect-video items-center justify-center bg-slate-100">
        {url && isImage ? (
          <img
            src={url}
            alt={`${titleCase(artifact.artifactType ?? "POD")} evidence`}
            loading="lazy"
            className="h-full w-full object-contain"
          />
        ) : (
          <ImageIcon aria-hidden className="h-10 w-10 text-slate-300" />
        )}
      </div>
      <div className="p-3">
        <div className="flex items-start justify-between gap-2">
          <div>
            <strong className="text-sm">
              {titleCase(artifact.artifactType ?? "Artifact")}
            </strong>
            <p className="text-xs text-slate-500">
              {artifact.mimeType} ·{" "}
              {Math.ceil((artifact.sizeBytes ?? 0) / 1024)} KB
            </p>
          </div>
          <Badge tone="neutral">SHA-256</Badge>
        </div>
        <p
          className="mt-2 truncate font-mono text-[10px] text-slate-400"
          title={artifact.checksumSha256}
        >
          {artifact.checksumSha256}
        </p>
        {error ? (
          <p role="alert" className="mt-2 text-xs text-danger">
            {error instanceof Error
              ? error.message
              : "Unable to load evidence."}
          </p>
        ) : null}
        <Button
          className="mt-3 w-full"
          size="sm"
          onClick={() => void load()}
          disabled={loading}
        >
          {loading ? (
            "Loading…"
          ) : url ? (
            "Reload securely"
          ) : (
            <>
              <Download aria-hidden className="h-4 w-4" /> Load evidence
            </>
          )}
        </Button>
      </div>
    </article>
  );
}

export function PublicTrackingPage() {
  const { awb: routeAwb = "" } = useParams();
  const normalizedRouteAwb = routeAwb.trim().toUpperCase();
  const [input, setInput] = useState(normalizedRouteAwb);
  const [awb, setAwb] = useState(normalizedRouteAwb);
  const query = useQuery({
    queryKey: ["public-tracking", awb],
    queryFn: () =>
      apiRequest<TrackingResult>(`/api/v1/track/${encodeURIComponent(awb)}`, {
        auth: false,
      }),
    enabled: Boolean(awb),
    retry: (count, error) =>
      !(error instanceof ApiError) || (error.status >= 500 && count < 1),
  });
  const submit = (event: FormEvent) => {
    event.preventDefault();
    const value = input.trim().toUpperCase();
    if (value) setAwb(value);
  };
  const notFound =
    query.error instanceof ApiError && query.error.status === 404;
  const rateLimited =
    query.error instanceof ApiError && query.error.status === 429;
  return (
    <main className="min-h-screen bg-[#f4f7f6] text-slate-950">
      <header className="border-b bg-white">
        <div className="mx-auto flex h-16 max-w-5xl items-center justify-between px-4">
          <div className="flex items-center gap-3">
            <span className="grid h-9 w-9 place-items-center rounded-md bg-emerald-950 text-sm font-black text-[#d8f25a]">
              CS
            </span>
            <span>
              <strong className="block text-sm tracking-wide">
                COURIER TRACKING
              </strong>
              <span className="block text-[10px] uppercase tracking-[0.18em] text-slate-500">
                Shipment journey
              </span>
            </span>
          </div>
          <ShieldCheck
            aria-label="Secure tracking"
            className="h-5 w-5 text-emerald-700"
          />
        </div>
      </header>
      <section className="border-b bg-emerald-950 text-white">
        <div className="mx-auto max-w-5xl px-4 py-12 sm:py-16">
          <p className="text-xs font-semibold uppercase tracking-[0.2em] text-[#d8f25a]">
            Track a shipment
          </p>
          <h1 className="mt-3 max-w-2xl text-3xl font-bold tracking-tight sm:text-4xl">
            See where your parcel is, without the warehouse jargon.
          </h1>
          <p className="mt-3 max-w-xl text-sm text-emerald-50/75">
            Enter the AWB printed on your receipt or shipping label.
          </p>
          <form onSubmit={submit} className="mt-7 flex max-w-2xl gap-2">
            <Input
              value={input}
              onChange={(event) => setInput(event.target.value.toUpperCase())}
              placeholder="Enter AWB"
              aria-label="Air waybill number"
              autoComplete="off"
              className="h-12 flex-1 border-white/20 bg-white font-mono text-base text-slate-950"
            />
            <Button
              type="submit"
              className="h-12 border-[#d8f25a] bg-[#d8f25a] text-emerald-950 hover:bg-[#cce84e]"
              disabled={!input.trim()}
            >
              <Search aria-hidden className="h-4 w-4" /> Track
            </Button>
          </form>
        </div>
      </section>
      <div className="mx-auto max-w-5xl px-4 py-8">
        {query.isFetching ? (
          <Panel>
            <LoadingState label="Looking up your shipment" />
          </Panel>
        ) : query.error ? (
          <Panel>
            {notFound ? (
              <EmptyState
                icon={Search}
                title="Tracking information not found"
                description="Check the AWB and try again. For privacy, unavailable and unrecognized numbers receive the same response."
                action={
                  <Button
                    onClick={() => {
                      setAwb("");
                      setInput("");
                    }}
                  >
                    Try another AWB
                  </Button>
                }
              />
            ) : rateLimited ? (
              <EmptyState
                icon={Clock3}
                title="Too many tracking requests"
                description="Please wait a moment before trying again."
                action={
                  <Button onClick={() => void query.refetch()}>
                    Try again
                  </Button>
                }
              />
            ) : (
              <ErrorState
                error={
                  new Error(
                    "Tracking is temporarily unavailable. Please try again shortly.",
                  )
                }
                retry={() => void query.refetch()}
              />
            )}
          </Panel>
        ) : query.data ? (
          <TrackingResultView result={query.data} />
        ) : (
          <div className="grid gap-4 sm:grid-cols-3">
            <div className="rounded-lg border bg-white p-5">
              <PackageOpen aria-hidden className="h-6 w-6 text-emerald-700" />
              <h2 className="mt-3 font-semibold">Simple milestones</h2>
              <p className="mt-1 text-sm text-slate-500">
                Only customer-safe journey updates are shown.
              </p>
            </div>
            <div className="rounded-lg border bg-white p-5">
              <Clock3 aria-hidden className="h-6 w-6 text-emerald-700" />
              <h2 className="mt-3 font-semibold">Delivery expectation</h2>
              <p className="mt-1 text-sm text-slate-500">
                Rescheduled dates appear when available.
              </p>
            </div>
            <div className="rounded-lg border bg-white p-5">
              <ShieldCheck aria-hidden className="h-6 w-6 text-emerald-700" />
              <h2 className="mt-3 font-semibold">Privacy protected</h2>
              <p className="mt-1 text-sm text-slate-500">
                Internal facilities and employee details stay private.
              </p>
            </div>
          </div>
        )}
      </div>
    </main>
  );
}

function TrackingResultView({ result }: { result: TrackingResult }) {
  return (
    <div className="grid gap-5 lg:grid-cols-[minmax(0,1.2fr)_minmax(300px,0.8fr)]">
      <div className="space-y-5">
        <Panel className="overflow-hidden">
          <div className="border-b bg-white p-5 sm:p-6">
            <div className="flex flex-wrap items-start justify-between gap-4">
              <div>
                <p className="font-mono text-sm font-semibold text-slate-500">
                  {result.awb}
                </p>
                <h2 className="mt-2 text-2xl font-bold">
                  {result.statusTitle}
                </h2>
                <p className="mt-2 max-w-xl text-sm text-slate-600">
                  {result.statusDescription}
                </p>
              </div>
              <Badge
                tone={
                  result.milestone === "DELIVERED"
                    ? "success"
                    : result.milestone === "EXCEPTION" ||
                        result.milestone === "RETURNING"
                      ? "warning"
                      : "info"
                }
              >
                {result.statusTitle ??
                  titleCase(result.milestone ?? "In transit")}
              </Badge>
            </div>
          </div>
          <div className="p-5 sm:p-6">
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
                description="Your shipment has been booked; movement updates will appear here."
              />
            )}
          </div>
        </Panel>
      </div>
      <div className="space-y-4">
        <Panel>
          <PanelHeader title="Shipment summary" />
          <dl className="space-y-4 p-5 text-sm">
            <div>
              <dt className="text-slate-500">Journey</dt>
              <dd className="mt-1 font-semibold">
                {result.origin || "Origin"}{" "}
                <span className="mx-1 text-slate-400">→</span>{" "}
                {result.destination || "Destination"}
              </dd>
            </div>
            <div>
              <dt className="text-slate-500">Service</dt>
              <dd className="mt-1 font-semibold">
                {result.service || "Courier service"}
              </dd>
            </div>
            <div>
              <dt className="text-slate-500">Expected delivery</dt>
              <dd className="mt-1 font-semibold">
                {result.expectedDelivery || "To be confirmed"}
              </dd>
            </div>
            <div>
              <dt className="text-slate-500">Pieces</dt>
              <dd className="mt-1 font-semibold">{result.pieceCount ?? 1}</dd>
            </div>
            <div>
              <dt className="text-slate-500">Last updated</dt>
              <dd className="mt-1 font-semibold">
                {formatDateTime(result.lastUpdatedAt)}
              </dd>
            </div>
          </dl>
        </Panel>
        {(result.amountDueOnDeliveryMinor ?? 0) > 0 ? (
          <div className="rounded-lg border border-amber-200 bg-amber-50 p-5">
            <p className="text-xs font-semibold uppercase tracking-wide text-amber-800">
              Amount due on delivery
            </p>
            <strong className="mt-1 block text-2xl text-amber-950">
              {formatMoney(result.amountDueOnDeliveryMinor, result.currency)}
            </strong>
            <p className="mt-1 text-xs text-amber-800">
              Please keep this amount ready.
            </p>
          </div>
        ) : null}
        {result.isReturning ? (
          <InlineNotice tone="warning" title="Returning to sender">
            This shipment is travelling back to its sender.
          </InlineNotice>
        ) : null}
      </div>
    </div>
  );
}
