import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Archive,
  ArrowRight,
  Boxes,
  CheckCircle2,
  FileCheck2,
  LockKeyhole,
  PackageOpen,
  Plus,
  Printer,
  ScanLine,
  SearchX,
  ShieldCheck,
  Truck,
} from "lucide-react";
import { useRef, useState } from "react";
import { useForm } from "react-hook-form";
import { useNavigate, useParams } from "react-router-dom";
import { z } from "zod";
import {
  apiRequest,
  operationalHeaders,
  queryString,
  type AddBagItemsResult,
  type AddManifestContentResult,
  type Bag,
  type BagPage,
  type Manifest,
  type ManifestPage,
  type OperatingUnitListResponse,
  type Reconciliation,
} from "../api/client";
import { useAuth } from "../auth/AuthProvider";
import { useToast } from "../components/ToastProvider";
import {
  CursorPager,
  EntityLink,
  JourneyTimeline,
  OperationalMetricStrip,
  ScannerInput,
  type ScannerInputHandle,
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
import { formatDateTime, formatWeight, titleCase } from "../lib/utils";
import { optionalPositiveInteger } from "../lib/validation";

function useFacilities() {
  return useQuery({
    queryKey: ["operating-units", "operations-selector"],
    queryFn: () =>
      apiRequest<OperatingUnitListResponse>(
        "/api/v1/network/operating-units?page=1&pageSize=100&status=ACTIVE",
      ),
    staleTime: 5 * 60_000,
  });
}

const bagStatuses = [
  "OPEN",
  "CLOSED",
  "DISPATCHED",
  "RECEIVED",
  "OPENED",
  "RECONCILED",
  "CANCELLED",
];

export function BagsPage() {
  const { hasPermission } = useAuth();
  const [status, setStatus] = useState("");
  const [direction, setDirection] = useState("");
  const [originUnitId, setOriginUnitId] = useState("");
  const [destinationUnitId, setDestinationUnitId] = useState("");
  const [cursorStack, setCursorStack] = useState<(string | undefined)[]>([
    undefined,
  ]);
  const [createOpen, setCreateOpen] = useState(false);
  const cursor = cursorStack.at(-1);
  const query = useQuery({
    queryKey: [
      "bags",
      { cursor, status, direction, originUnitId, destinationUnitId },
    ],
    queryFn: () =>
      apiRequest<BagPage>(
        `/api/v1/bags${queryString({ cursor, limit: 25, status, direction, originUnitId, destinationUnitId })}`,
      ),
    placeholderData: (previous) => previous,
  });
  const rows = query.data?.data ?? [];
  const reset = () => setCursorStack([undefined]);
  return (
    <>
      <PageHeader
        eyebrow="Operations · Bagging"
        title="Bags"
        description="Build, seal, dispatch, receive, and reconcile shipment containers with immutable closure declarations."
        actions={
          <>
            {hasPermission("bag.receive") ? (
              <Button
                onClick={() => location.assign("/operations/bags/receive")}
              >
                Receive bag
              </Button>
            ) : null}
            {hasPermission("bag.manage") ? (
              <Button variant="primary" onClick={() => setCreateOpen(true)}>
                <Plus aria-hidden className="h-4 w-4" /> Create bag
              </Button>
            ) : null}
          </>
        }
      />
      <OperationalMetricStrip
        label="Visible bag workload"
        items={[
          { label: "Visible", value: rows.length, icon: Boxes },
          {
            label: "Open",
            value: rows.filter((item) => item.status === "OPEN").length,
            tone: "info",
            icon: PackageOpen,
          },
          {
            label: "Closed",
            value: rows.filter((item) => item.status === "CLOSED").length,
            tone: "warning",
            icon: LockKeyhole,
          },
          {
            label: "In transit",
            value: rows.filter((item) => item.status === "DISPATCHED").length,
            tone: "info",
            icon: Truck,
          },
          {
            label: "Received",
            value: rows.filter((item) => item.status === "RECEIVED").length,
            tone: "success",
            icon: CheckCircle2,
          },
          {
            label: "Reverse",
            value: rows.filter((item) => item.direction === "REVERSE").length,
            tone: "warning",
            icon: Archive,
          },
        ]}
      />
      <Panel>
        <div className="grid gap-3 border-b p-4 md:grid-cols-2 xl:grid-cols-4">
          <Select
            value={status}
            onChange={(event) => {
              setStatus(event.target.value);
              reset();
            }}
            aria-label="Bag status"
          >
            <option value="">All bag states</option>
            {bagStatuses.map((item) => (
              <option key={item}>{item}</option>
            ))}
          </Select>
          <Select
            value={direction}
            onChange={(event) => {
              setDirection(event.target.value);
              reset();
            }}
            aria-label="Movement direction"
          >
            <option value="">Both directions</option>
            <option>FORWARD</option>
            <option>REVERSE</option>
          </Select>
          <Input
            value={originUnitId}
            onChange={(event) => {
              setOriginUnitId(event.target.value);
              reset();
            }}
            placeholder="Origin unit ID"
            aria-label="Origin unit ID"
          />
          <Input
            value={destinationUnitId}
            onChange={(event) => {
              setDestinationUnitId(event.target.value);
              reset();
            }}
            placeholder="Destination unit ID"
            aria-label="Destination unit ID"
          />
        </div>
        {query.isLoading ? (
          <LoadingState label="Loading bags" />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : rows.length === 0 ? (
          <EmptyState
            icon={SearchX}
            title="No bags found"
            description="Change the filters or create a bag at the current facility."
          />
        ) : (
          <>
            <DataTable label="Bags">
              <thead>
                <tr>
                  <TableHead>Bag</TableHead>
                  <TableHead>Route</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead>Contents</TableHead>
                  <TableHead>Weight</TableHead>
                  <TableHead>State</TableHead>
                  <TableHead>Updated</TableHead>
                </tr>
              </thead>
              <tbody>
                {rows.map((bag) => (
                  <tr key={bag.id} className="hover:bg-slate-50">
                    <TableCell>
                      <EntityLink
                        to={`/operations/bags/${bag.id}`}
                        primary={bag.bagCode}
                        secondary={bag.barcode}
                      />
                    </TableCell>
                    <TableCell>
                      <span className="font-semibold">{bag.origin?.code}</span>
                      <ArrowRight
                        aria-hidden
                        className="mx-1.5 inline h-3.5 w-3.5 text-slate-400"
                      />
                      <span className="font-semibold">
                        {bag.destination?.code}
                      </span>
                      {bag.direction === "REVERSE" ? (
                        <Badge tone="warning" className="ml-2">
                          Return
                        </Badge>
                      ) : null}
                    </TableCell>
                    <TableCell>
                      {titleCase(bag.bagType ?? "")}
                      <span className="block text-xs text-slate-500">
                        {bag.serviceCode || "Any service"}
                      </span>
                    </TableCell>
                    <TableCell>
                      {bag.shipmentCount ?? 0} shipments
                      <span className="block text-xs text-slate-500">
                        {bag.pieceCount ?? 0} pieces
                      </span>
                    </TableCell>
                    <TableCell>{formatWeight(bag.totalWeightGrams)}</TableCell>
                    <TableCell>
                      <StatusBadge status={bag.status} />
                    </TableCell>
                    <TableCell>
                      {formatDateTime(
                        bag.receivedAt ||
                          bag.dispatchedAt ||
                          bag.closedAt ||
                          bag.createdAt,
                      )}
                    </TableCell>
                  </tr>
                ))}
              </tbody>
            </DataTable>
            <CursorPager
              page={cursorStack.length}
              count={rows.length}
              noun="bags"
              hasMore={query.data?.pagination?.hasMore}
              nextCursor={query.data?.pagination?.nextCursor}
              onPrevious={() => setCursorStack((items) => items.slice(0, -1))}
              onNext={(next) => setCursorStack((items) => [...items, next])}
            />
          </>
        )}
      </Panel>
      <CreateBagDialog open={createOpen} onOpenChange={setCreateOpen} />
    </>
  );
}

const createBagSchema = z.object({
  originUnitId: z.string().optional(),
  destinationUnitId: z.string().min(1, "Destination is required"),
  bagType: z.enum(["STANDARD", "SECURE", "FRAGILE", "DOCUMENT", "RETURN"]),
  direction: z.enum(["FORWARD", "REVERSE"]),
  serviceCode: z.string().optional(),
  maxWeightGrams: optionalPositiveInteger,
  maxShipments: optionalPositiveInteger,
});

function CreateBagDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const facilities = useFacilities();
  const client = useQueryClient();
  const navigate = useNavigate();
  const { toast } = useToast();
  const form = useForm<
    z.input<typeof createBagSchema>,
    unknown,
    z.output<typeof createBagSchema>
  >({
    resolver: zodResolver(createBagSchema),
    defaultValues: { bagType: "STANDARD", direction: "FORWARD" },
  });
  const mutation = useMutation({
    mutationFn: (body: z.output<typeof createBagSchema>) =>
      apiRequest<Bag>("/api/v1/bags", {
        method: "POST",
        body,
      }),
    onSuccess: (bag) => {
      void client.invalidateQueries({ queryKey: ["bags"] });
      toast({ tone: "success", title: "Bag opened", description: bag.bagCode });
      onOpenChange(false);
      if (bag.id) void navigate(`/operations/bags/${bag.id}`);
    },
  });
  const options = facilities.data?.data ?? [];
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Create bag"
      description="Open a mutable bag at the current facility. Its declaration freezes when closed."
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
            {mutation.isPending ? "Creating…" : "Create and scan"}
          </Button>
        </>
      }
    >
      <div className="grid gap-4 sm:grid-cols-2">
        <Field
          label="Origin facility"
          htmlFor="bag-origin"
          hint="Optional when your role covers one facility"
        >
          <Select id="bag-origin" {...form.register("originUnitId")}>
            <option value="">Use current facility</option>
            {options.map((item) => (
              <option key={item.id} value={item.id}>
                {item.code} · {item.name}
              </option>
            ))}
          </Select>
        </Field>
        <Field
          label="Destination facility"
          htmlFor="bag-destination"
          required
          error={form.formState.errors.destinationUnitId?.message}
        >
          <Select id="bag-destination" {...form.register("destinationUnitId")}>
            <option value="">Select destination</option>
            {options.map((item) => (
              <option key={item.id} value={item.id}>
                {item.code} · {item.name}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Bag type" htmlFor="bag-type" required>
          <Select id="bag-type" {...form.register("bagType")}>
            <option>STANDARD</option>
            <option>SECURE</option>
            <option>FRAGILE</option>
            <option>DOCUMENT</option>
            <option>RETURN</option>
          </Select>
        </Field>
        <Field label="Direction" htmlFor="bag-direction" required>
          <Select id="bag-direction" {...form.register("direction")}>
            <option>FORWARD</option>
            <option>REVERSE</option>
          </Select>
        </Field>
        <Field label="Service code" htmlFor="bag-service">
          <Input id="bag-service" {...form.register("serviceCode")} />
        </Field>
        <Field label="Maximum shipments" htmlFor="bag-capacity">
          <Input
            id="bag-capacity"
            type="number"
            min={1}
            {...form.register("maxShipments")}
          />
        </Field>
        <Field label="Maximum weight (g)" htmlFor="bag-max-weight">
          <Input
            id="bag-max-weight"
            type="number"
            min={1}
            {...form.register("maxWeightGrams")}
          />
        </Field>
      </div>
      {mutation.error ? <ErrorState error={mutation.error} /> : null}
    </Dialog>
  );
}

export function BagDetailPage() {
  const { bagId = "" } = useParams();
  const { hasPermission } = useAuth();
  const navigate = useNavigate();
  const client = useQueryClient();
  const { toast } = useToast();
  const [barcode, setBarcode] = useState("");
  const [facilityId, setFacilityId] = useState("");
  const [closeOpen, setCloseOpen] = useState(false);
  const [sealOpen, setSealOpen] = useState(false);
  const scannerRef = useRef<ScannerInputHandle>(null);
  const query = useQuery({
    queryKey: ["bag", bagId],
    queryFn: () => apiRequest<Bag>(`/api/v1/bags/${bagId}`),
    enabled: Boolean(bagId),
  });
  const add = useMutation({
    mutationFn: (value: string) =>
      apiRequest<AddBagItemsResult>(`/api/v1/bags/${bagId}/items`, {
        method: "POST",
        body: { barcodes: [value] },
        headers: operationalHeaders("bag-item"),
      }),
    onSuccess: (result) => {
      setBarcode("");
      void client.invalidateQueries({ queryKey: ["bag", bagId] });
      const row = result.results?.[0];
      toast({
        tone: row?.outcome === "ADDED" ? "success" : "error",
        title:
          row?.outcome === "ADDED" ? "Shipment added" : "Shipment rejected",
        description: row?.message || row?.awb,
      });
    },
    onSettled: () => window.setTimeout(() => scannerRef.current?.focus(), 0),
  });
  const transition = useMutation({
    mutationFn: ({ action, body }: { action: string; body?: unknown }) =>
      apiRequest<Bag>(
        `/api/v1/bags/${bagId}/${action}${queryString({ operatingUnitId: facilityId })}`,
        {
          method: "POST",
          body: body ?? {},
          headers: operationalHeaders(`bag-${action}`),
        },
      ),
    onSuccess: (bag, variables) => {
      void client.invalidateQueries({ queryKey: ["bag", bagId] });
      toast({
        tone: "success",
        title: `Bag ${titleCase(variables.action)}`,
        description: bag.bagCode,
      });
      setSealOpen(false);
    },
  });
  const reconcile = useMutation({
    mutationFn: () =>
      apiRequest<Reconciliation>(
        `/api/v1/reconciliations${queryString({ operatingUnitId: facilityId })}`,
        { method: "POST", body: { subjectType: "BAG", bagId } },
      ),
    onSuccess: (item) => {
      if (item.id) void navigate(`/operations/hub/reconciliations/${item.id}`);
    },
  });
  if (query.isLoading) return <LoadingState label="Loading bag" />;
  if (query.error)
    return (
      <ErrorState error={query.error} retry={() => void query.refetch()} />
    );
  const bag = query.data;
  if (!bag)
    return (
      <EmptyState
        title="Bag not found"
        description="Check the bag ID or scan its barcode again."
      />
    );
  const transitions = new Set(bag.allowedTransitions ?? []);
  const canAdd = bag.status === "OPEN" && hasPermission("bag.manage");
  return (
    <>
      <PageHeader
        eyebrow="Operations · Bagging"
        title={bag.bagCode ?? "Bag"}
        description={`${bag.origin?.code ?? "Current facility"} → ${bag.destination?.code ?? "Destination"} · ${titleCase(bag.direction ?? "")}`}
        actions={
          <>
            {bag.barcode ? <Badge tone="neutral">{bag.barcode}</Badge> : null}
            <StatusBadge status={bag.status} />
          </>
        }
      />
      {bag.status !== "OPEN" ? (
        <InlineNotice tone="info" title="Contents are immutable">
          This bag's closure declaration is frozen. Reconciliation compares
          receipt against that snapshot.
        </InlineNotice>
      ) : null}
      <OperationalMetricStrip
        items={[
          { label: "Shipments", value: bag.shipmentCount, icon: Boxes },
          { label: "Pieces", value: bag.pieceCount, icon: PackageOpen },
          {
            label: "Net weight",
            value: formatWeight(bag.totalWeightGrams),
            icon: Archive,
          },
          {
            label: "Gross weight",
            value: formatWeight(bag.grossWeightGrams),
            icon: Archive,
          },
          { label: "Seals", value: bag.seals?.length ?? 0, icon: ShieldCheck },
          {
            label: "State",
            value: titleCase(bag.status ?? ""),
            tone:
              bag.status === "OPEN"
                ? "info"
                : bag.status === "RECONCILED"
                  ? "success"
                  : "warning",
            icon: LockKeyhole,
          },
        ]}
      />
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.35fr)_minmax(320px,0.65fr)]">
        <div className="space-y-4">
          {canAdd ? (
            <Panel>
              <PanelHeader
                title="Bag scanner"
                description={`Only parcels routed through ${bag.destination?.code ?? "this destination"} will be accepted.`}
              />
              <div className="p-4">
                <ScannerInput
                  ref={scannerRef}
                  value={barcode}
                  onChange={setBarcode}
                  onScan={(value) => add.mutate(value)}
                  busy={add.isPending}
                  label="Scan shipment into bag"
                />
              </div>
            </Panel>
          ) : null}
          <Panel>
            <PanelHeader
              title="Current contents"
              description={
                bag.status === "OPEN"
                  ? "Mutable until closure."
                  : "Frozen closure declaration is authoritative."
              }
            />
            {bag.items?.length ? (
              <DataTable label="Bag contents">
                <thead>
                  <tr>
                    <TableHead>AWB</TableHead>
                    <TableHead>Shipment state</TableHead>
                    <TableHead>Bag state</TableHead>
                    <TableHead>Pieces</TableHead>
                    <TableHead>Weight</TableHead>
                    <TableHead>Destination</TableHead>
                  </tr>
                </thead>
                <tbody>
                  {bag.items.map((item) => (
                    <tr key={`${item.shipmentId}-${item.addedAt}`}>
                      <TableCell>
                        <EntityLink
                          to={`/shipments/${item.shipmentId}`}
                          primary={item.awb}
                        />
                      </TableCell>
                      <TableCell>
                        <StatusBadge status={item.status} />
                      </TableCell>
                      <TableCell>{titleCase(item.itemStatus ?? "")}</TableCell>
                      <TableCell>{item.pieceCount}</TableCell>
                      <TableCell>{formatWeight(item.weightGrams)}</TableCell>
                      <TableCell>{item.destinationPincode}</TableCell>
                    </tr>
                  ))}
                </tbody>
              </DataTable>
            ) : (
              <EmptyState
                icon={ScanLine}
                title="Bag is empty"
                description="Scan the first shipment to begin building this bag."
              />
            )}
          </Panel>
          {bag.declaredContents ? (
            <Panel>
              <PanelHeader
                title="Closure declaration"
                description="Permanent snapshot used by destination reconciliation."
              />
              <dl className="grid gap-4 p-4 sm:grid-cols-2 lg:grid-cols-4">
                <div>
                  <dt className="text-xs text-slate-500">Closed</dt>
                  <dd className="mt-1 text-sm font-semibold">
                    {formatDateTime(bag.declaredContents.closedAt)}
                  </dd>
                </div>
                <div>
                  <dt className="text-xs text-slate-500">Shipments</dt>
                  <dd className="mt-1 text-sm font-semibold">
                    {bag.declaredContents.shipmentCount}
                  </dd>
                </div>
                <div>
                  <dt className="text-xs text-slate-500">Pieces</dt>
                  <dd className="mt-1 text-sm font-semibold">
                    {bag.declaredContents.pieceCount}
                  </dd>
                </div>
                <div>
                  <dt className="text-xs text-slate-500">Weight</dt>
                  <dd className="mt-1 text-sm font-semibold">
                    {formatWeight(bag.declaredContents.totalWeightGrams)}
                  </dd>
                </div>
              </dl>
            </Panel>
          ) : null}
        </div>
        <div className="space-y-4">
          <Panel>
            <PanelHeader
              title="Next action"
              description="Only server-permitted transitions are shown."
            />
            <div className="space-y-3 p-4">
              <Field
                label="Current facility"
                htmlFor="bag-facility"
                hint="Needed for receiving and reconciliation when you cover multiple facilities."
              >
                <Input
                  id="bag-facility"
                  value={facilityId}
                  onChange={(event) => setFacilityId(event.target.value)}
                  placeholder="ou_…"
                />
              </Field>
              {transitions.has("CLOSED") && hasPermission("bag.close") ? (
                <Button
                  className="w-full"
                  variant="primary"
                  onClick={() => setCloseOpen(true)}
                >
                  <LockKeyhole aria-hidden className="h-4 w-4" /> Review and
                  close
                </Button>
              ) : null}
              {transitions.has("DISPATCHED") &&
              hasPermission("bag.dispatch") ? (
                <Button
                  className="w-full"
                  variant="primary"
                  disabled={transition.isPending}
                  onClick={() => transition.mutate({ action: "dispatch" })}
                >
                  <Truck aria-hidden className="h-4 w-4" /> Dispatch bag
                </Button>
              ) : null}
              {transitions.has("RECEIVED") && hasPermission("bag.receive") ? (
                <Button
                  className="w-full"
                  variant="primary"
                  disabled={!facilityId || transition.isPending}
                  onClick={() => transition.mutate({ action: "receive" })}
                >
                  Receive at facility
                </Button>
              ) : null}
              {bag.status === "RECEIVED" && hasPermission("bag.receive") ? (
                <Button className="w-full" onClick={() => setSealOpen(true)}>
                  <ShieldCheck aria-hidden className="h-4 w-4" /> Verify seal
                </Button>
              ) : null}
              {transitions.has("OPENED") && hasPermission("bag.open") ? (
                <Button
                  className="w-full"
                  disabled={!facilityId || transition.isPending}
                  onClick={() => transition.mutate({ action: "open" })}
                >
                  <PackageOpen aria-hidden className="h-4 w-4" /> Open at
                  destination
                </Button>
              ) : null}
              {bag.status === "OPENED" &&
              hasPermission("reconciliation.manage") ? (
                <Button
                  className="w-full"
                  variant="primary"
                  disabled={!facilityId || reconcile.isPending}
                  onClick={() => reconcile.mutate()}
                >
                  Start reconciliation
                </Button>
              ) : null}
              {!bag.allowedTransitions?.length ? (
                <p className="text-sm text-slate-500">
                  No further transition is available.
                </p>
              ) : null}
              {transition.error ? (
                <ErrorState error={transition.error} />
              ) : null}
            </div>
          </Panel>
          <Panel>
            <PanelHeader title="Bag journey" />
            {bag.events?.length ? (
              <div className="p-4">
                <JourneyTimeline
                  items={bag.events.map((event) => ({
                    key: event.id ?? String(event.sequence),
                    title:
                      event.description ??
                      titleCase(event.eventType ?? "Event"),
                    description: event.facility,
                    occurredAt: event.occurredAt,
                    status: event.toStatus,
                  }))}
                />
              </div>
            ) : (
              <EmptyState
                title="No journey events"
                description="Lifecycle events appear as the bag moves."
              />
            )}
          </Panel>
        </div>
      </div>
      <CloseBagDialog bag={bag} open={closeOpen} onOpenChange={setCloseOpen} />
      <VerifySealDialog
        bagId={bagId}
        facilityId={facilityId}
        open={sealOpen}
        onOpenChange={setSealOpen}
      />
    </>
  );
}

const closeBagSchema = z.object({
  sealNumbers: z.string().min(3, "At least one seal is required"),
  sealType: z.enum(["PLASTIC", "METAL", "ELECTRONIC", "TAPE"]),
  grossWeightGrams: optionalPositiveInteger,
});

function CloseBagDialog({
  bag,
  open,
  onOpenChange,
}: {
  bag: Bag;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const client = useQueryClient();
  const { toast } = useToast();
  const form = useForm<
    z.input<typeof closeBagSchema>,
    unknown,
    z.output<typeof closeBagSchema>
  >({
    resolver: zodResolver(closeBagSchema),
    defaultValues: { sealType: "PLASTIC" },
  });
  const mutation = useMutation({
    mutationFn: (values: z.output<typeof closeBagSchema>) =>
      apiRequest<Bag>(`/api/v1/bags/${bag.id}/close`, {
        method: "POST",
        body: {
          sealNumbers: values.sealNumbers.split(/[\s,]+/).filter(Boolean),
          sealType: values.sealType,
          grossWeightGrams: values.grossWeightGrams,
        },
      }),
    onSuccess: (closed) => {
      void client.invalidateQueries({ queryKey: ["bag", bag.id] });
      toast({
        tone: "success",
        title: "Bag closed and frozen",
        description: closed.bagCode,
      });
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Review and close bag"
      description="Closure freezes the content declaration. The bag cannot be edited through the normal scanner afterward."
      footer={
        <>
          <Button onClick={() => onOpenChange(false)}>Keep open</Button>
          <Button
            variant="primary"
            disabled={mutation.isPending}
            onClick={() =>
              void form.handleSubmit((values) => mutation.mutate(values))()
            }
          >
            <LockKeyhole aria-hidden className="h-4 w-4" /> Close bag
          </Button>
        </>
      }
    >
      <div className="mb-4 grid grid-cols-3 gap-3 rounded-md border bg-slate-50 p-4 text-center">
        <div>
          <strong className="block text-xl">{bag.shipmentCount ?? 0}</strong>
          <span className="text-xs text-slate-500">Shipments</span>
        </div>
        <div>
          <strong className="block text-xl">{bag.pieceCount ?? 0}</strong>
          <span className="text-xs text-slate-500">Pieces</span>
        </div>
        <div>
          <strong className="block text-xl">
            {formatWeight(bag.totalWeightGrams)}
          </strong>
          <span className="text-xs text-slate-500">Net weight</span>
        </div>
      </div>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field
          label="Seal numbers"
          htmlFor="bag-seals"
          required
          error={form.formState.errors.sealNumbers?.message}
          hint="Separate multiple seals with commas"
        >
          <Input id="bag-seals" {...form.register("sealNumbers")} />
        </Field>
        <Field label="Seal type" htmlFor="bag-seal-type" required>
          <Select id="bag-seal-type" {...form.register("sealType")}>
            <option>PLASTIC</option>
            <option>METAL</option>
            <option>ELECTRONIC</option>
            <option>TAPE</option>
          </Select>
        </Field>
        <Field label="Gross weight (g)" htmlFor="bag-gross">
          <Input
            id="bag-gross"
            type="number"
            min={1}
            {...form.register("grossWeightGrams")}
          />
        </Field>
        <div>
          <p className="text-xs font-medium text-slate-500">Destination</p>
          <p className="mt-2 text-sm font-semibold">
            {bag.destination?.code} · {bag.destination?.name}
          </p>
        </div>
      </div>
      {mutation.error ? <ErrorState error={mutation.error} /> : null}
    </Dialog>
  );
}

const verifySealSchema = z.object({
  sealNumber: z.string().min(3),
  result: z.enum(["INTACT", "BROKEN", "MISSING", "MISMATCH"]),
  remarks: z.string().optional(),
});
function VerifySealDialog({
  bagId,
  facilityId,
  open,
  onOpenChange,
}: {
  bagId: string;
  facilityId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const client = useQueryClient();
  const { toast } = useToast();
  const form = useForm<z.infer<typeof verifySealSchema>>({
    resolver: zodResolver(verifySealSchema),
    defaultValues: { result: "INTACT" },
  });
  const mutation = useMutation({
    mutationFn: (body: z.infer<typeof verifySealSchema>) =>
      apiRequest<Bag>(
        `/api/v1/bags/${bagId}/verify-seal${queryString({ operatingUnitId: facilityId })}`,
        { method: "POST", body },
      ),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ["bag", bagId] });
      toast({ tone: "success", title: "Seal verification recorded" });
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Verify bag seal"
      description="Record what is physically present before opening the bag."
      footer={
        <>
          <Button onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button
            variant="primary"
            onClick={() =>
              void form.handleSubmit((values) => mutation.mutate(values))()
            }
          >
            Record verification
          </Button>
        </>
      }
    >
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Seal number" htmlFor="seal-number" required>
          <Input id="seal-number" {...form.register("sealNumber")} />
        </Field>
        <Field label="Result" htmlFor="seal-result" required>
          <Select id="seal-result" {...form.register("result")}>
            <option>INTACT</option>
            <option>BROKEN</option>
            <option>MISSING</option>
            <option>MISMATCH</option>
          </Select>
        </Field>
        <Field className="sm:col-span-2" label="Remarks" htmlFor="seal-remarks">
          <Textarea id="seal-remarks" {...form.register("remarks")} />
        </Field>
      </div>
      {mutation.error ? <ErrorState error={mutation.error} /> : null}
    </Dialog>
  );
}

export function ReceiveBagPage() {
  const navigate = useNavigate();
  const [barcode, setBarcode] = useState("");
  const [lookup, setLookup] = useState("");
  const query = useQuery({
    queryKey: ["bag-barcode", lookup],
    queryFn: () =>
      apiRequest<Bag>(`/api/v1/bags/by-barcode/${encodeURIComponent(lookup)}`),
    enabled: Boolean(lookup),
    retry: false,
  });
  return (
    <>
      <PageHeader
        eyebrow="Operations · Bagging"
        title="Receive bag"
        description="Scan a bag barcode, verify its destination and declaration, then continue in the bag workspace."
      />
      <Panel className="mx-auto max-w-3xl">
        <div className="p-5">
          <ScannerInput
            value={barcode}
            onChange={setBarcode}
            onScan={(value) => setLookup(value)}
            busy={query.isFetching}
            label="Scan bag barcode"
          />
        </div>
        {query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : query.data ? (
          <div className="border-t p-5">
            <div className="flex flex-wrap items-start justify-between gap-4">
              <div>
                <p className="font-mono text-xl font-bold text-primary">
                  {query.data.bagCode}
                </p>
                <p className="mt-1 text-sm text-slate-500">
                  {query.data.origin?.code} → {query.data.destination?.code} ·{" "}
                  {query.data.shipmentCount} shipments
                </p>
              </div>
              <StatusBadge status={query.data.status} />
            </div>
            <Button
              className="mt-5 w-full"
              variant="primary"
              onClick={() =>
                void navigate(`/operations/bags/${query.data?.id}`)
              }
            >
              Open receiving workspace
            </Button>
          </div>
        ) : null}
      </Panel>
    </>
  );
}

const manifestStatuses = [
  "DRAFT",
  "CLOSED",
  "DISPATCHED",
  "RECEIVED",
  "RECONCILED",
  "CANCELLED",
];

export function ManifestsPage() {
  const { hasPermission } = useAuth();
  const [status, setStatus] = useState("");
  const [direction, setDirection] = useState("");
  const [cursorStack, setCursorStack] = useState<(string | undefined)[]>([
    undefined,
  ]);
  const [open, setOpen] = useState(false);
  const cursor = cursorStack.at(-1);
  const query = useQuery({
    queryKey: ["manifests", { cursor, status, direction }],
    queryFn: () =>
      apiRequest<ManifestPage>(
        `/api/v1/manifests${queryString({ cursor, limit: 25, status, direction })}`,
      ),
    placeholderData: (previous) => previous,
  });
  const rows = query.data?.data ?? [];
  return (
    <>
      <PageHeader
        eyebrow="Operations · Manifest"
        title="Manifest board"
        description="Build handover documents from closed bags and loose shipments, then dispatch and receive them as one atomic movement."
        actions={
          <>
            {hasPermission("manifest.receive") ? (
              <Button
                onClick={() => location.assign("/operations/manifests/receive")}
              >
                Receive manifest
              </Button>
            ) : null}
            {hasPermission("manifest.manage") ? (
              <Button variant="primary" onClick={() => setOpen(true)}>
                <Plus aria-hidden className="h-4 w-4" /> Create manifest
              </Button>
            ) : null}
          </>
        }
      />
      <OperationalMetricStrip
        items={manifestStatuses.slice(0, 5).map((item) => ({
          label: titleCase(item),
          value: rows.filter((row) => row.status === item).length,
          tone:
            item === "RECONCILED"
              ? ("success" as const)
              : item === "DISPATCHED"
                ? ("info" as const)
                : ("neutral" as const),
          icon:
            item === "DRAFT"
              ? FileCheck2
              : item === "DISPATCHED"
                ? Truck
                : Archive,
        }))}
      />
      <Panel>
        <div className="grid gap-3 border-b p-4 sm:grid-cols-2">
          <Select
            value={status}
            onChange={(event) => {
              setStatus(event.target.value);
              setCursorStack([undefined]);
            }}
          >
            <option value="">All manifest states</option>
            {manifestStatuses.map((item) => (
              <option key={item}>{item}</option>
            ))}
          </Select>
          <Select
            value={direction}
            onChange={(event) => {
              setDirection(event.target.value);
              setCursorStack([undefined]);
            }}
          >
            <option value="">Both directions</option>
            <option>FORWARD</option>
            <option>REVERSE</option>
          </Select>
        </div>
        {query.isLoading ? (
          <LoadingState label="Loading manifests" />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : rows.length ? (
          <>
            <DataTable label="Manifests">
              <thead>
                <tr>
                  <TableHead>Manifest</TableHead>
                  <TableHead>Route</TableHead>
                  <TableHead>Trip</TableHead>
                  <TableHead>Load</TableHead>
                  <TableHead>Weight</TableHead>
                  <TableHead>Document</TableHead>
                  <TableHead>Status</TableHead>
                </tr>
              </thead>
              <tbody>
                {rows.map((item) => (
                  <tr key={item.id}>
                    <TableCell>
                      <EntityLink
                        to={`/operations/manifests/${item.id}`}
                        primary={item.manifestCode}
                        secondary={
                          item.direction === "REVERSE"
                            ? "Return movement"
                            : undefined
                        }
                      />
                    </TableCell>
                    <TableCell>
                      {item.origin?.code}{" "}
                      <ArrowRight
                        aria-hidden
                        className="mx-1 inline h-3.5 w-3.5"
                      />{" "}
                      {item.destination?.code}
                    </TableCell>
                    <TableCell>{item.tripCode || "Not attached"}</TableCell>
                    <TableCell>
                      {item.bagCount ?? 0} bags
                      <span className="block text-xs text-slate-500">
                        {item.looseShipmentCount ?? 0} loose ·{" "}
                        {item.totalPieceCount ?? 0} pcs
                      </span>
                    </TableCell>
                    <TableCell>{formatWeight(item.totalWeightGrams)}</TableCell>
                    <TableCell>
                      {item.documentReady ? (
                        <Badge tone="success">
                          <Printer aria-hidden className="h-3 w-3" /> Ready
                        </Badge>
                      ) : (
                        <Badge tone="neutral">Pending</Badge>
                      )}
                    </TableCell>
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
              noun="manifests"
              hasMore={query.data?.pagination?.hasMore}
              nextCursor={query.data?.pagination?.nextCursor}
              onPrevious={() => setCursorStack((items) => items.slice(0, -1))}
              onNext={(next) => setCursorStack((items) => [...items, next])}
            />
          </>
        ) : (
          <EmptyState
            title="No manifests"
            description="Create a manifest when closed bags or loose shipments are ready for handover."
          />
        )}
      </Panel>
      <CreateManifestDialog open={open} onOpenChange={setOpen} />
    </>
  );
}

const manifestSchema = z.object({
  originUnitId: z.string().optional(),
  destinationUnitId: z.string().min(1),
  tripId: z.string().optional(),
  direction: z.enum(["FORWARD", "REVERSE"]),
  remarks: z.string().optional(),
});
function CreateManifestDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const facilities = useFacilities();
  const navigate = useNavigate();
  const client = useQueryClient();
  const { toast } = useToast();
  const form = useForm<z.infer<typeof manifestSchema>>({
    resolver: zodResolver(manifestSchema),
    defaultValues: { direction: "FORWARD" },
  });
  const mutation = useMutation({
    mutationFn: (body: z.infer<typeof manifestSchema>) =>
      apiRequest<Manifest>("/api/v1/manifests", {
        method: "POST",
        body,
      }),
    onSuccess: (item) => {
      void client.invalidateQueries({ queryKey: ["manifests"] });
      toast({
        tone: "success",
        title: "Manifest created",
        description: item.manifestCode,
      });
      onOpenChange(false);
      if (item.id) void navigate(`/operations/manifests/${item.id}`);
    },
  });
  const options = facilities.data?.data ?? [];
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Create manifest"
      description="Open a draft handover document between two facilities."
      footer={
        <>
          <Button onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button
            variant="primary"
            onClick={() =>
              void form.handleSubmit((values) => mutation.mutate(values))()
            }
          >
            Create manifest
          </Button>
        </>
      }
    >
      <div className="grid gap-4 sm:grid-cols-2">
        <Field
          label="Origin"
          htmlFor="manifest-origin"
          hint="Optional for single-facility roles"
        >
          <Select id="manifest-origin" {...form.register("originUnitId")}>
            <option value="">Use current facility</option>
            {options.map((item) => (
              <option key={item.id} value={item.id}>
                {item.code} · {item.name}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Destination" htmlFor="manifest-destination" required>
          <Select
            id="manifest-destination"
            {...form.register("destinationUnitId")}
          >
            <option value="">Select destination</option>
            {options.map((item) => (
              <option key={item.id} value={item.id}>
                {item.code} · {item.name}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Direction" htmlFor="manifest-direction">
          <Select id="manifest-direction" {...form.register("direction")}>
            <option>FORWARD</option>
            <option>REVERSE</option>
          </Select>
        </Field>
        <Field
          label="Trip ID"
          htmlFor="manifest-trip"
          hint="Optional at creation"
        >
          <Input id="manifest-trip" {...form.register("tripId")} />
        </Field>
        <Field
          className="sm:col-span-2"
          label="Remarks"
          htmlFor="manifest-remarks"
        >
          <Textarea id="manifest-remarks" {...form.register("remarks")} />
        </Field>
      </div>
      {mutation.error ? <ErrorState error={mutation.error} /> : null}
    </Dialog>
  );
}

export function ManifestDetailPage() {
  const { manifestId = "" } = useParams();
  const { hasPermission } = useAuth();
  const navigate = useNavigate();
  const client = useQueryClient();
  const { toast } = useToast();
  const scannerRef = useRef<ScannerInputHandle>(null);
  const [reference, setReference] = useState("");
  const [kind, setKind] = useState<"BAG" | "SHIPMENT">("BAG");
  const [facilityId, setFacilityId] = useState("");
  const [closeOpen, setCloseOpen] = useState(false);
  const query = useQuery({
    queryKey: ["manifest", manifestId],
    queryFn: () => apiRequest<Manifest>(`/api/v1/manifests/${manifestId}`),
    enabled: Boolean(manifestId),
    refetchInterval: (state) =>
      state.state.data?.documentReady === false &&
      state.state.data?.status === "CLOSED"
        ? 5_000
        : false,
  });
  const add = useMutation({
    mutationFn: (value: string) =>
      apiRequest<AddManifestContentResult>(
        `/api/v1/manifests/${manifestId}/contents`,
        {
          method: "POST",
          body: kind === "BAG" ? { bagCodes: [value] } : { barcodes: [value] },
          headers: operationalHeaders("manifest-content"),
        },
      ),
    onSuccess: (result) => {
      setReference("");
      void client.invalidateQueries({ queryKey: ["manifest", manifestId] });
      const row = result.results?.[0];
      toast({
        tone: row?.outcome === "ADDED" ? "success" : "error",
        title:
          row?.outcome === "ADDED"
            ? `${titleCase(kind)} added`
            : `${titleCase(kind)} rejected`,
        description: row?.message || row?.reference,
      });
    },
    onSettled: () => scannerRef.current?.focus(),
  });
  const action = useMutation({
    mutationFn: (name: "dispatch" | "receive") =>
      apiRequest<Manifest>(
        `/api/v1/manifests/${manifestId}/${name}${queryString({ operatingUnitId: facilityId })}`,
        {
          method: "POST",
          body: {},
          headers: operationalHeaders(`manifest-${name}`),
        },
      ),
    onSuccess: (item, name) => {
      void client.invalidateQueries({ queryKey: ["manifest", manifestId] });
      toast({
        tone: "success",
        title: `Manifest ${name === "dispatch" ? "dispatched" : "received"}`,
        description: item.manifestCode,
      });
    },
  });
  const reconcile = useMutation({
    mutationFn: () =>
      apiRequest<Reconciliation>(
        `/api/v1/reconciliations${queryString({ operatingUnitId: facilityId })}`,
        { method: "POST", body: { subjectType: "MANIFEST", manifestId } },
      ),
    onSuccess: (item) => {
      if (item.id) void navigate(`/operations/hub/reconciliations/${item.id}`);
    },
  });
  if (query.isLoading) return <LoadingState label="Loading manifest" />;
  if (query.error)
    return (
      <ErrorState error={query.error} retry={() => void query.refetch()} />
    );
  const item = query.data;
  if (!item)
    return (
      <EmptyState
        title="Manifest not found"
        description="Check the manifest identifier."
      />
    );
  const transitions = new Set(item.allowedTransitions ?? []);
  const draft = item.status === "DRAFT";
  return (
    <>
      <PageHeader
        eyebrow="Operations · Manifest"
        title={item.manifestCode ?? "Manifest"}
        description={`${item.origin?.code} → ${item.destination?.code} · ${titleCase(item.direction ?? "")}`}
        actions={
          <>
            <StatusBadge status={item.status} />
            {item.documentReady ? (
              <Badge tone="success">
                <Printer aria-hidden className="h-3 w-3" /> Document ready
              </Badge>
            ) : null}
          </>
        }
      />
      {!draft ? (
        <InlineNotice tone="info" title="Manifest declaration is frozen">
          Dispatch and receipt move every declared shipment atomically. Contents
          cannot be edited.
        </InlineNotice>
      ) : null}
      <OperationalMetricStrip
        items={[
          { label: "Bags", value: item.bagCount, icon: Boxes },
          {
            label: "Loose shipments",
            value: item.looseShipmentCount,
            icon: PackageOpen,
          },
          { label: "Shipments", value: item.totalShipmentCount, icon: Archive },
          { label: "Pieces", value: item.totalPieceCount, icon: PackageOpen },
          {
            label: "Weight",
            value: formatWeight(item.totalWeightGrams),
            icon: Archive,
          },
          {
            label: "State",
            value: titleCase(item.status ?? ""),
            tone: item.status === "RECONCILED" ? "success" : "info",
            icon: FileCheck2,
          },
        ]}
      />
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.4fr)_minmax(320px,0.6fr)]">
        <div className="space-y-4">
          {draft && hasPermission("manifest.manage") ? (
            <Panel>
              <PanelHeader
                title="Manifest builder"
                description="Only closed bags can be loaded. Loose shipments already in bags are refused."
                actions={
                  <Select
                    className="w-40"
                    value={kind}
                    onChange={(event) =>
                      setKind(event.target.value as "BAG" | "SHIPMENT")
                    }
                  >
                    <option value="BAG">Scan bags</option>
                    <option value="SHIPMENT">Scan loose AWB</option>
                  </Select>
                }
              />
              <div className="p-4">
                <ScannerInput
                  ref={scannerRef}
                  value={reference}
                  onChange={setReference}
                  onScan={(value) => add.mutate(value)}
                  busy={add.isPending}
                  label={
                    kind === "BAG" ? "Scan closed bag" : "Scan loose shipment"
                  }
                />
              </div>
            </Panel>
          ) : null}
          <Panel>
            <PanelHeader title="Bags" />
            {item.bags?.length ? (
              <DataTable label="Manifest bags">
                <thead>
                  <tr>
                    <TableHead>Bag</TableHead>
                    <TableHead>Destination</TableHead>
                    <TableHead>Declaration</TableHead>
                    <TableHead>Weight</TableHead>
                    <TableHead>Status</TableHead>
                  </tr>
                </thead>
                <tbody>
                  {item.bags.map((bag) => (
                    <tr key={bag.bagId}>
                      <TableCell>
                        <EntityLink
                          to={`/operations/bags/${bag.bagId}`}
                          primary={bag.bagCode}
                          secondary={bag.barcode}
                        />
                      </TableCell>
                      <TableCell>{bag.destinationCode}</TableCell>
                      <TableCell>
                        {bag.declaredShipmentCount} shipments ·{" "}
                        {bag.declaredPieceCount} pcs
                      </TableCell>
                      <TableCell>
                        {formatWeight(bag.declaredWeightGrams)}
                      </TableCell>
                      <TableCell>
                        <StatusBadge status={bag.lineStatus} />
                      </TableCell>
                    </tr>
                  ))}
                </tbody>
              </DataTable>
            ) : (
              <EmptyState
                title="No bags loaded"
                description="Scan a closed bag into this draft."
              />
            )}
          </Panel>
          <Panel>
            <PanelHeader title="Loose shipments" />
            {item.looseShipments?.length ? (
              <DataTable label="Loose shipments">
                <thead>
                  <tr>
                    <TableHead>AWB</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead>Pieces</TableHead>
                    <TableHead>Weight</TableHead>
                    <TableHead>Destination</TableHead>
                  </tr>
                </thead>
                <tbody>
                  {item.looseShipments.map((line) => (
                    <tr key={line.shipmentId}>
                      <TableCell>
                        <EntityLink
                          to={`/shipments/${line.shipmentId}`}
                          primary={line.awb}
                        />
                      </TableCell>
                      <TableCell>
                        <StatusBadge status={line.lineStatus ?? line.status} />
                      </TableCell>
                      <TableCell>{line.pieceCount}</TableCell>
                      <TableCell>{formatWeight(line.weightGrams)}</TableCell>
                      <TableCell>{line.destinationPincode}</TableCell>
                    </tr>
                  ))}
                </tbody>
              </DataTable>
            ) : (
              <EmptyState
                title="No loose shipments"
                description="Loose shipments are optional when all pieces travel in bags."
              />
            )}
          </Panel>
        </div>
        <div className="space-y-4">
          <Panel>
            <PanelHeader
              title="Next action"
              description="Actions follow server-provided transitions."
            />
            <div className="space-y-3 p-4">
              <Field label="Current facility" htmlFor="manifest-facility">
                <Input
                  id="manifest-facility"
                  value={facilityId}
                  onChange={(event) => setFacilityId(event.target.value)}
                  placeholder="ou_…"
                />
              </Field>
              {transitions.has("CLOSED") && hasPermission("manifest.close") ? (
                <Button
                  className="w-full"
                  variant="primary"
                  onClick={() => setCloseOpen(true)}
                >
                  Review and close
                </Button>
              ) : null}
              {transitions.has("DISPATCHED") &&
              hasPermission("manifest.dispatch") ? (
                <Button
                  className="w-full"
                  variant="primary"
                  onClick={() => action.mutate("dispatch")}
                >
                  Dispatch manifest
                </Button>
              ) : null}
              {transitions.has("RECEIVED") &&
              hasPermission("manifest.receive") ? (
                <Button
                  className="w-full"
                  variant="primary"
                  disabled={!facilityId}
                  onClick={() => action.mutate("receive")}
                >
                  Receive manifest
                </Button>
              ) : null}
              {item.status === "RECEIVED" &&
              hasPermission("reconciliation.manage") ? (
                <Button
                  className="w-full"
                  variant="primary"
                  disabled={!facilityId}
                  onClick={() => reconcile.mutate()}
                >
                  Start reconciliation
                </Button>
              ) : null}
              {action.error ? <ErrorState error={action.error} /> : null}
            </div>
          </Panel>
          <Panel>
            <PanelHeader title="Journey" />
            {item.events?.length ? (
              <div className="p-4">
                <JourneyTimeline
                  items={item.events.map((event) => ({
                    key: event.id ?? String(event.sequence),
                    title:
                      event.description ??
                      titleCase(event.eventType ?? "Event"),
                    description: event.facility,
                    occurredAt: event.occurredAt,
                    status: event.toStatus,
                  }))}
                />
              </div>
            ) : (
              <EmptyState
                title="No lifecycle events"
                description="Events appear after the first state change."
              />
            )}
          </Panel>
        </div>
      </div>
      <CloseManifestDialog
        item={item}
        open={closeOpen}
        onOpenChange={setCloseOpen}
      />
    </>
  );
}

const closeManifestSchema = z.object({
  declaredWeightGrams: optionalPositiveInteger,
});
function CloseManifestDialog({
  item,
  open,
  onOpenChange,
}: {
  item: Manifest;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const client = useQueryClient();
  const { toast } = useToast();
  const form = useForm<
    z.input<typeof closeManifestSchema>,
    unknown,
    z.output<typeof closeManifestSchema>
  >({ resolver: zodResolver(closeManifestSchema) });
  const mutation = useMutation({
    mutationFn: (body: z.output<typeof closeManifestSchema>) =>
      apiRequest<Manifest>(`/api/v1/manifests/${item.id}/close`, {
        method: "POST",
        body,
      }),
    onSuccess: (closed) => {
      void client.invalidateQueries({ queryKey: ["manifest", item.id] });
      toast({
        tone: "success",
        title: "Manifest closed",
        description: `${closed.manifestCode}; document generation queued.`,
      });
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Review and close manifest"
      description="Closure freezes every bag and loose shipment declaration and queues the printable document."
      footer={
        <>
          <Button onClick={() => onOpenChange(false)}>Keep draft</Button>
          <Button
            variant="primary"
            onClick={() =>
              void form.handleSubmit((values) => mutation.mutate(values))()
            }
          >
            Close manifest
          </Button>
        </>
      }
    >
      <div className="mb-4 grid grid-cols-3 gap-3 rounded-md border bg-slate-50 p-4 text-center">
        <div>
          <strong className="block text-xl">{item.bagCount ?? 0}</strong>
          <span className="text-xs text-slate-500">Bags</span>
        </div>
        <div>
          <strong className="block text-xl">
            {item.totalShipmentCount ?? 0}
          </strong>
          <span className="text-xs text-slate-500">Shipments</span>
        </div>
        <div>
          <strong className="block text-xl">
            {formatWeight(item.totalWeightGrams)}
          </strong>
          <span className="text-xs text-slate-500">Calculated</span>
        </div>
      </div>
      <Field
        label="Declared handover weight (g)"
        htmlFor="manifest-weight"
        hint="Optional physical scale reading"
      >
        <Input
          id="manifest-weight"
          type="number"
          min={1}
          {...form.register("declaredWeightGrams")}
        />
      </Field>
      {mutation.error ? <ErrorState error={mutation.error} /> : null}
    </Dialog>
  );
}

export function ReceiveManifestPage() {
  const [id, setId] = useState("");
  const navigate = useNavigate();
  return (
    <>
      <PageHeader
        eyebrow="Operations · Manifest"
        title="Receive manifest"
        description="Enter or scan the manifest public ID to open its receiving workspace."
      />
      <Panel className="mx-auto max-w-2xl">
        <form
          className="space-y-4 p-5"
          onSubmit={(event) => {
            event.preventDefault();
            if (id.trim()) void navigate(`/operations/manifests/${id.trim()}`);
          }}
        >
          <Field label="Manifest ID" htmlFor="receive-manifest" required>
            <Input
              id="receive-manifest"
              value={id}
              onChange={(event) => setId(event.target.value)}
              placeholder="mnf_…"
              autoFocus
            />
          </Field>
          <Button
            className="w-full"
            variant="primary"
            type="submit"
            disabled={!id.trim()}
          >
            Open receiving workspace
          </Button>
        </form>
      </Panel>
    </>
  );
}
