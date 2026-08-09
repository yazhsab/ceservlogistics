import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Archive,
  CalendarClock,
  Gauge,
  PackageCheck,
  Plus,
  Route,
  Truck,
} from "lucide-react";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { useNavigate, useParams } from "react-router-dom";
import { z } from "zod";
import {
  apiRequest,
  operationalHeaders,
  queryString,
  type Carrier,
  type CarrierList,
  type Driver,
  type DriverList,
  type OperatingUnitListResponse,
  type Trip,
  type TripPage,
  type TripSummary,
  type Vehicle,
  type VehicleList,
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
import {
  optionalNonnegativeInteger,
  optionalPositiveInteger,
} from "../lib/validation";

const tripStatuses = [
  "PLANNED",
  "LOADING",
  "DEPARTED",
  "IN_TRANSIT",
  "ARRIVED",
  "CLOSED",
] as const;

export function TripsPage() {
  const { hasPermission } = useAuth();
  const [direction, setDirection] = useState("");
  const [cursorStack, setCursorStack] = useState<(string | undefined)[]>([
    undefined,
  ]);
  const [createOpen, setCreateOpen] = useState(false);
  const cursor = cursorStack.at(-1);
  const query = useQuery({
    queryKey: ["trips", { cursor, direction }],
    queryFn: () =>
      apiRequest<TripPage>(
        `/api/v1/trips${queryString({ cursor, limit: 25, direction })}`,
      ),
    placeholderData: (previous) => previous,
  });
  const rows = query.data?.data ?? [];
  return (
    <>
      <PageHeader
        eyebrow="Operations · Line haul"
        title="Trip board"
        description="Plan and monitor vehicle movement from loading through arrival. Arrival never implies parcel receipt."
        actions={
          <>
            {hasPermission("vehicle.read") ? (
              <Button onClick={() => location.assign("/operations/fleet")}>
                Vehicles & carriers
              </Button>
            ) : null}
            {hasPermission("trip.manage") ? (
              <Button variant="primary" onClick={() => setCreateOpen(true)}>
                <Plus aria-hidden className="h-4 w-4" /> Create trip
              </Button>
            ) : null}
          </>
        }
      />
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <p className="text-xs text-slate-500">
          Board shows the current cursor page. Open a lane for full trip detail.
        </p>
        <Select
          className="w-44"
          value={direction}
          onChange={(event) => {
            setDirection(event.target.value);
            setCursorStack([undefined]);
          }}
          aria-label="Trip direction"
        >
          <option value="">Both directions</option>
          <option>FORWARD</option>
          <option>REVERSE</option>
        </Select>
      </div>
      {query.isLoading ? (
        <Panel>
          <LoadingState label="Loading trip board" />
        </Panel>
      ) : query.error ? (
        <Panel>
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        </Panel>
      ) : rows.length === 0 ? (
        <Panel>
          <EmptyState
            title="No trips found"
            description="Create a trip or change the movement direction."
          />
        </Panel>
      ) : (
        <>
          <div
            className="grid gap-3 xl:grid-cols-6"
            aria-label="Trip status board"
          >
            {tripStatuses.map((status) => (
              <section
                key={status}
                className="min-w-0 rounded-lg border border-border bg-slate-50"
              >
                <header className="flex items-center justify-between border-b px-3 py-2">
                  <h2 className="text-xs font-bold uppercase tracking-wide text-slate-600">
                    {titleCase(status)}
                  </h2>
                  <Badge
                    tone={
                      status === "CLOSED"
                        ? "success"
                        : status === "IN_TRANSIT"
                          ? "info"
                          : "neutral"
                    }
                  >
                    {rows.filter((trip) => trip.status === status).length}
                  </Badge>
                </header>
                <div className="space-y-2 p-2">
                  {rows
                    .filter((trip) => trip.status === status)
                    .map((trip) => (
                      <TripCard key={trip.id} trip={trip} />
                    ))}
                  {!rows.some((trip) => trip.status === status) ? (
                    <p className="px-2 py-6 text-center text-xs text-slate-400">
                      No trips
                    </p>
                  ) : null}
                </div>
              </section>
            ))}
          </div>
          <Panel className="mt-4">
            <CursorPager
              page={cursorStack.length}
              count={rows.length}
              noun="trips"
              hasMore={query.data?.pagination?.hasMore}
              nextCursor={query.data?.pagination?.nextCursor}
              onPrevious={() => setCursorStack((items) => items.slice(0, -1))}
              onNext={(next) => setCursorStack((items) => [...items, next])}
            />
          </Panel>
        </>
      )}
      <CreateTripDialog open={createOpen} onOpenChange={setCreateOpen} />
    </>
  );
}

function TripCard({ trip }: { trip: TripSummary }) {
  const navigate = useNavigate();
  return (
    <button
      onClick={() => trip.id && void navigate(`/operations/trips/${trip.id}`)}
      className="w-full rounded-md border border-border bg-white p-3 text-left shadow-sm hover:border-emerald-300 hover:shadow"
    >
      <div className="flex items-start justify-between gap-2">
        <span className="font-mono text-xs font-bold text-primary">
          {trip.tripCode}
        </span>
        {trip.direction === "REVERSE" ? (
          <Badge tone="warning">RTO</Badge>
        ) : null}
      </div>
      <p className="mt-2 text-sm font-semibold">
        {trip.origin?.code} → {trip.destination?.code}
      </p>
      <p className="mt-1 text-xs text-slate-500">
        {trip.mode} · {trip.vehicleRegistration || "No vehicle"}
      </p>
      <div className="mt-3 flex items-center justify-between border-t pt-2 text-[11px] text-slate-500">
        <span>{trip.manifestCount ?? 0} manifests</span>
        <span>{formatDateTime(trip.scheduled?.departure)}</span>
      </div>
    </button>
  );
}

const tripSchema = z
  .object({
    mode: z.enum(["ROAD", "AIR", "RAIL", "PARTNER"]),
    originUnitId: z.string().optional(),
    destinationUnitId: z.string().min(1),
    direction: z.enum(["FORWARD", "REVERSE"]),
    carrierId: z.string().optional(),
    vehicleId: z.string().optional(),
    driverId: z.string().optional(),
    externalReference: z.string().optional(),
    scheduledDeparture: z.string().min(1),
    scheduledArrival: z.string().min(1),
  })
  .superRefine((value, context) => {
    if (value.scheduledArrival <= value.scheduledDeparture)
      context.addIssue({
        code: "custom",
        path: ["scheduledArrival"],
        message: "Arrival must be after departure",
      });
  });

function CreateTripDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const client = useQueryClient();
  const navigate = useNavigate();
  const { toast } = useToast();
  const facilities = useQuery({
    queryKey: ["operating-units", "trip-selector"],
    queryFn: () =>
      apiRequest<OperatingUnitListResponse>(
        "/api/v1/network/operating-units?page=1&pageSize=100&status=ACTIVE",
      ),
    staleTime: 300_000,
  });
  const carriers = useQuery({
    queryKey: ["carriers"],
    queryFn: () => apiRequest<CarrierList>("/api/v1/carriers?limit=100"),
    staleTime: 300_000,
  });
  const vehicles = useQuery({
    queryKey: ["vehicles"],
    queryFn: () => apiRequest<VehicleList>("/api/v1/vehicles?limit=100"),
    staleTime: 300_000,
  });
  const drivers = useQuery({
    queryKey: ["drivers"],
    queryFn: () => apiRequest<DriverList>("/api/v1/drivers?limit=100"),
    staleTime: 300_000,
  });
  const form = useForm<z.infer<typeof tripSchema>>({
    resolver: zodResolver(tripSchema),
    defaultValues: { mode: "ROAD", direction: "FORWARD" },
  });
  const mutation = useMutation({
    mutationFn: (values: z.infer<typeof tripSchema>) =>
      apiRequest<Trip>("/api/v1/trips", {
        method: "POST",
        body: {
          ...values,
          scheduledDeparture: new Date(values.scheduledDeparture).toISOString(),
          scheduledArrival: new Date(values.scheduledArrival).toISOString(),
        },
      }),
    onSuccess: (trip) => {
      void client.invalidateQueries({ queryKey: ["trips"] });
      toast({
        tone: "success",
        title: "Trip planned",
        description: trip.tripCode,
      });
      onOpenChange(false);
      if (trip.id) void navigate(`/operations/trips/${trip.id}`);
    },
  });
  const units = facilities.data?.data ?? [];
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Create trip"
      description="Plan a direct line-haul movement. Vehicle and driver can be assigned later before departure."
      footer={
        <>
          <Button onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button
            variant="primary"
            onClick={() =>
              void form.handleSubmit((values) => mutation.mutate(values))()
            }
          >
            Create trip
          </Button>
        </>
      }
    >
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Mode" htmlFor="trip-mode" required>
          <Select id="trip-mode" {...form.register("mode")}>
            <option>ROAD</option>
            <option>AIR</option>
            <option>RAIL</option>
            <option>PARTNER</option>
          </Select>
        </Field>
        <Field label="Direction" htmlFor="trip-direction">
          <Select id="trip-direction" {...form.register("direction")}>
            <option>FORWARD</option>
            <option>REVERSE</option>
          </Select>
        </Field>
        <Field
          label="Origin"
          htmlFor="trip-origin"
          hint="Optional for single-facility roles"
        >
          <Select id="trip-origin" {...form.register("originUnitId")}>
            <option value="">Use current facility</option>
            {units.map((unit) => (
              <option key={unit.id} value={unit.id}>
                {unit.code} · {unit.name}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Destination" htmlFor="trip-destination" required>
          <Select id="trip-destination" {...form.register("destinationUnitId")}>
            <option value="">Select destination</option>
            {units.map((unit) => (
              <option key={unit.id} value={unit.id}>
                {unit.code} · {unit.name}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Scheduled departure" htmlFor="trip-depart" required>
          <Input
            id="trip-depart"
            type="datetime-local"
            {...form.register("scheduledDeparture")}
          />
        </Field>
        <Field
          label="Scheduled arrival"
          htmlFor="trip-arrival"
          required
          error={form.formState.errors.scheduledArrival?.message}
        >
          <Input
            id="trip-arrival"
            type="datetime-local"
            {...form.register("scheduledArrival")}
          />
        </Field>
        <Field label="Carrier" htmlFor="trip-carrier">
          <Select id="trip-carrier" {...form.register("carrierId")}>
            <option value="">No carrier selected</option>
            {carriers.data?.data?.map((item) => (
              <option key={item.id} value={item.id}>
                {item.code} · {item.name}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Vehicle" htmlFor="trip-vehicle">
          <Select id="trip-vehicle" {...form.register("vehicleId")}>
            <option value="">Assign later</option>
            {vehicles.data?.data?.map((item) => (
              <option key={item.id} value={item.id}>
                {item.registrationNumber} · {item.vehicleType}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Driver" htmlFor="trip-driver">
          <Select id="trip-driver" {...form.register("driverId")}>
            <option value="">Assign later</option>
            {drivers.data?.data?.map((item) => (
              <option key={item.id} value={item.id}>
                {item.code} · {item.fullName}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Flight/train/partner reference" htmlFor="trip-reference">
          <Input id="trip-reference" {...form.register("externalReference")} />
        </Field>
      </div>
      {mutation.error ? <ErrorState error={mutation.error} /> : null}
    </Dialog>
  );
}

export function TripDetailPage() {
  const { tripId = "" } = useParams();
  const { hasPermission } = useAuth();
  const client = useQueryClient();
  const { toast } = useToast();
  const [assignOpen, setAssignOpen] = useState(false);
  const [manifestOpen, setManifestOpen] = useState(false);
  const [movementOpen, setMovementOpen] = useState<
    "depart" | "arrive" | "close" | "cancel" | undefined
  >();
  const query = useQuery({
    queryKey: ["trip", tripId],
    queryFn: () => apiRequest<Trip>(`/api/v1/trips/${tripId}`),
    enabled: Boolean(tripId),
  });
  const action = useMutation({
    mutationFn: ({ action, body }: { action: string; body?: unknown }) =>
      apiRequest<Trip>(`/api/v1/trips/${tripId}/${action}`, {
        method: "POST",
        body: body ?? {},
        headers: operationalHeaders(`trip-${action}`),
      }),
    onSuccess: (trip, variables) => {
      void client.invalidateQueries({ queryKey: ["trip", tripId] });
      void client.invalidateQueries({ queryKey: ["trips"] });
      toast({
        tone: "success",
        title: `Trip ${titleCase(variables.action)}`,
        description: trip.tripCode,
      });
      setMovementOpen(undefined);
    },
    retry: false,
  });
  if (query.isLoading) return <LoadingState label="Loading trip" />;
  if (query.error)
    return (
      <ErrorState error={query.error} retry={() => void query.refetch()} />
    );
  const trip = query.data;
  if (!trip)
    return (
      <EmptyState
        title="Trip not found"
        description="Check the trip identifier."
      />
    );
  const transitions = new Set(trip.allowedTransitions ?? []);
  return (
    <>
      <div className="-mx-4 -mt-4 mb-5 border-b bg-emerald-950 px-4 py-5 text-white sm:-mx-6 sm:px-6">
        <p className="text-xs font-semibold uppercase tracking-wider text-emerald-200">
          Line haul · {trip.mode}
        </p>
        <div className="mt-1 flex flex-wrap items-end justify-between gap-4">
          <div>
            <h1 className="font-mono text-2xl font-bold">{trip.tripCode}</h1>
            <p className="mt-1 text-sm text-emerald-50/80">
              {trip.origin?.name ?? trip.origin?.code} →{" "}
              {trip.destination?.name ?? trip.destination?.code}
            </p>
          </div>
          <div className="rounded-md bg-white/10 px-4 py-3 text-right">
            <span className="block text-[10px] uppercase tracking-wide text-emerald-100/70">
              Current state
            </span>
            <strong className="text-lg">{titleCase(trip.status ?? "")}</strong>
          </div>
        </div>
      </div>
      <OperationalMetricStrip
        items={[
          { label: "Manifests", value: trip.manifestCount, icon: PackageCheck },
          { label: "Bags", value: trip.bagCount, icon: Truck },
          { label: "Shipments", value: trip.shipmentCount, icon: Archive },
          {
            label: "Weight",
            value: formatWeight(trip.totalWeightGrams),
            icon: Gauge,
          },
          {
            label: "Current leg",
            value: `${trip.currentLegSequence ?? 0} / ${trip.legs?.length ?? 0}`,
            icon: Route,
          },
          {
            label: "ETA",
            value: formatDateTime(trip.scheduled?.arrival),
            icon: CalendarClock,
          },
        ]}
      />
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.35fr)_minmax(320px,0.65fr)]">
        <div className="space-y-4">
          <Panel>
            <PanelHeader
              title="Trip legs"
              description="Vehicle arrival and manifest receipt remain separate operational facts."
            />
            {trip.legs?.length ? (
              <div className="p-4">
                <JourneyTimeline
                  items={trip.legs.map((leg) => ({
                    key: leg.id ?? String(leg.sequence),
                    title: `${leg.origin?.code} → ${leg.destination?.code}`,
                    description: `${formatDateTime(leg.scheduled?.departure)} · ${formatDateTime(leg.scheduled?.arrival)}`,
                    occurredAt: leg.actualArrival || leg.actualDeparture,
                    status: leg.status,
                    current: leg.sequence === trip.currentLegSequence,
                  }))}
                />
              </div>
            ) : (
              <EmptyState
                title="No legs"
                description="This trip has no configured movement legs."
              />
            )}
          </Panel>
          <Panel>
            <PanelHeader
              title="Manifests"
              actions={
                hasPermission("trip.manage") &&
                ["PLANNED", "LOADING"].includes(trip.status ?? "") ? (
                  <Button size="sm" onClick={() => setManifestOpen(true)}>
                    Attach manifest
                  </Button>
                ) : undefined
              }
            />
            {trip.manifests?.length ? (
              <DataTable label="Trip manifests">
                <thead>
                  <tr>
                    <TableHead>Manifest</TableHead>
                    <TableHead>Route</TableHead>
                    <TableHead>Load</TableHead>
                    <TableHead>Weight</TableHead>
                    <TableHead>Status</TableHead>
                  </tr>
                </thead>
                <tbody>
                  {trip.manifests.map((item) => (
                    <tr key={item.id}>
                      <TableCell>
                        <EntityLink
                          to={`/operations/manifests/${item.id}`}
                          primary={item.manifestCode}
                        />
                      </TableCell>
                      <TableCell>
                        {item.originCode} → {item.destinationCode}
                      </TableCell>
                      <TableCell>
                        {item.bagCount} bags · {item.shipmentCount} shipments
                      </TableCell>
                      <TableCell>{formatWeight(item.weightGrams)}</TableCell>
                      <TableCell>
                        <StatusBadge status={item.status} />
                      </TableCell>
                    </tr>
                  ))}
                </tbody>
              </DataTable>
            ) : (
              <EmptyState
                title="Trip is empty"
                description="Attach a closed manifest before departure."
              />
            )}
          </Panel>
          <Panel>
            <PanelHeader title="Event history" />
            {trip.events?.length ? (
              <div className="p-4">
                <JourneyTimeline
                  items={trip.events.map((event) => ({
                    key: event.id ?? String(event.sequence),
                    title:
                      event.description ??
                      titleCase(event.eventType ?? "Event"),
                    occurredAt: event.occurredAt,
                    status: event.toStatus,
                  }))}
                />
              </div>
            ) : (
              <EmptyState
                title="No events yet"
                description="Trip lifecycle events appear here."
              />
            )}
          </Panel>
        </div>
        <div className="space-y-4">
          <Panel>
            <PanelHeader
              title="Vehicle and crew"
              actions={
                hasPermission("trip.manage") &&
                ["PLANNED", "LOADING"].includes(trip.status ?? "") ? (
                  <Button size="sm" onClick={() => setAssignOpen(true)}>
                    Assign
                  </Button>
                ) : undefined
              }
            />
            <dl className="space-y-3 p-4 text-sm">
              <div>
                <dt className="text-slate-500">Carrier</dt>
                <dd className="font-semibold">
                  {trip.carrier?.name ?? "Not assigned"}
                </dd>
              </div>
              <div>
                <dt className="text-slate-500">Vehicle</dt>
                <dd className="font-semibold">
                  {trip.vehicle?.registrationNumber ?? "Not assigned"}
                </dd>
              </div>
              <div>
                <dt className="text-slate-500">Driver</dt>
                <dd className="font-semibold">
                  {trip.driver?.name ?? "Not assigned"}
                </dd>
                <dd className="text-xs text-slate-500">{trip.driver?.phone}</dd>
              </div>
              <div>
                <dt className="text-slate-500">External reference</dt>
                <dd className="font-semibold">
                  {trip.externalReference || "—"}
                </dd>
              </div>
            </dl>
          </Panel>
          <Panel>
            <PanelHeader
              title="Trip action"
              description="Departure validates every prerequisite before moving anything."
            />
            <div className="space-y-2 p-4">
              {transitions.has("DEPARTED") &&
              hasPermission("linehaul.depart") ? (
                <Button
                  className="w-full"
                  variant="primary"
                  onClick={() => setMovementOpen("depart")}
                >
                  Depart trip
                </Button>
              ) : null}
              {transitions.has("ARRIVED") &&
              hasPermission("linehaul.arrive") ? (
                <Button
                  className="w-full"
                  variant="primary"
                  onClick={() => setMovementOpen("arrive")}
                >
                  Record arrival
                </Button>
              ) : null}
              {transitions.has("CLOSED") && hasPermission("linehaul.arrive") ? (
                <Button
                  className="w-full"
                  onClick={() => setMovementOpen("close")}
                >
                  Close trip
                </Button>
              ) : null}
              {transitions.has("CANCELLED") && hasPermission("trip.cancel") ? (
                <Button
                  className="w-full"
                  variant="danger"
                  onClick={() => setMovementOpen("cancel")}
                >
                  Cancel trip
                </Button>
              ) : null}
              {!trip.allowedTransitions?.length ? (
                <p className="text-sm text-slate-500">
                  This trip has no further transition.
                </p>
              ) : null}
              {action.error ? <ErrorState error={action.error} /> : null}
            </div>
          </Panel>
        </div>
      </div>
      <AssignTripDialog
        trip={trip}
        open={assignOpen}
        onOpenChange={setAssignOpen}
      />
      <AttachManifestDialog
        tripId={tripId}
        open={manifestOpen}
        onOpenChange={setManifestOpen}
      />
      <MovementDialog
        action={movementOpen}
        open={Boolean(movementOpen)}
        onOpenChange={(open) => !open && setMovementOpen(undefined)}
        onSubmit={(body) =>
          movementOpen && action.mutate({ action: movementOpen, body })
        }
        pending={action.isPending}
      />
    </>
  );
}

const assignTripSchema = z.object({
  vehicleId: z.string().optional(),
  driverId: z.string().optional(),
  carrierId: z.string().optional(),
  externalReference: z.string().optional(),
});
function AssignTripDialog({
  trip,
  open,
  onOpenChange,
}: {
  trip: Trip;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const client = useQueryClient();
  const { toast } = useToast();
  const vehicles = useQuery({
    queryKey: ["vehicles"],
    queryFn: () => apiRequest<VehicleList>("/api/v1/vehicles?limit=100"),
  });
  const drivers = useQuery({
    queryKey: ["drivers"],
    queryFn: () => apiRequest<DriverList>("/api/v1/drivers?limit=100"),
  });
  const carriers = useQuery({
    queryKey: ["carriers"],
    queryFn: () => apiRequest<CarrierList>("/api/v1/carriers?limit=100"),
  });
  const form = useForm<z.infer<typeof assignTripSchema>>({
    resolver: zodResolver(assignTripSchema),
    defaultValues: {
      vehicleId: trip.vehicle?.id,
      driverId: trip.driver?.id,
      carrierId: trip.carrier?.id,
      externalReference: trip.externalReference,
    },
  });
  const mutation = useMutation({
    mutationFn: (body: z.infer<typeof assignTripSchema>) =>
      apiRequest<Trip>(`/api/v1/trips/${trip.id}/assign`, {
        method: "POST",
        body,
      }),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ["trip", trip.id] });
      toast({ tone: "success", title: "Trip assignment updated" });
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Assign vehicle and crew"
      description="Live-trip conflicts are enforced by the server."
      footer={
        <>
          <Button onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button
            variant="primary"
            onClick={() =>
              void form.handleSubmit((values) => mutation.mutate(values))()
            }
          >
            Save assignment
          </Button>
        </>
      }
    >
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Carrier" htmlFor="assign-carrier">
          <Select id="assign-carrier" {...form.register("carrierId")}>
            <option value="">None</option>
            {carriers.data?.data?.map((item) => (
              <option key={item.id} value={item.id}>
                {item.code} · {item.name}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Vehicle" htmlFor="assign-vehicle">
          <Select id="assign-vehicle" {...form.register("vehicleId")}>
            <option value="">None</option>
            {vehicles.data?.data?.map((item) => (
              <option key={item.id} value={item.id}>
                {item.registrationNumber}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Driver" htmlFor="assign-driver">
          <Select id="assign-driver" {...form.register("driverId")}>
            <option value="">None</option>
            {drivers.data?.data?.map((item) => (
              <option key={item.id} value={item.id}>
                {item.code} · {item.fullName}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="External reference" htmlFor="assign-reference">
          <Input
            id="assign-reference"
            {...form.register("externalReference")}
          />
        </Field>
      </div>
      {mutation.error ? <ErrorState error={mutation.error} /> : null}
    </Dialog>
  );
}

const attachSchema = z.object({
  manifestId: z.string().min(1),
  legSequence: optionalPositiveInteger,
});
function AttachManifestDialog({
  tripId,
  open,
  onOpenChange,
}: {
  tripId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const client = useQueryClient();
  const { toast } = useToast();
  const form = useForm<
    z.input<typeof attachSchema>,
    unknown,
    z.output<typeof attachSchema>
  >({ resolver: zodResolver(attachSchema) });
  const mutation = useMutation({
    mutationFn: (body: z.output<typeof attachSchema>) =>
      apiRequest<Trip>(`/api/v1/trips/${tripId}/manifests`, {
        method: "POST",
        body,
      }),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ["trip", tripId] });
      toast({ tone: "success", title: "Manifest attached" });
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Attach manifest"
      description="Attach a closed manifest to the whole trip or a specific leg."
      footer={
        <>
          <Button onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button
            variant="primary"
            onClick={() =>
              void form.handleSubmit((values) => mutation.mutate(values))()
            }
          >
            Attach manifest
          </Button>
        </>
      }
    >
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Manifest ID" htmlFor="attach-manifest" required>
          <Input id="attach-manifest" {...form.register("manifestId")} />
        </Field>
        <Field
          label="Leg sequence"
          htmlFor="attach-leg"
          hint="Only for partial multi-leg travel"
        >
          <Input
            id="attach-leg"
            type="number"
            min={1}
            {...form.register("legSequence")}
          />
        </Field>
      </div>
      {mutation.error ? <ErrorState error={mutation.error} /> : null}
    </Dialog>
  );
}

const movementSchema = z.object({
  odometerKm: optionalNonnegativeInteger,
  sealNumber: z.string().optional(),
  legSequence: optionalPositiveInteger,
  reason: z.string().optional(),
});
function MovementDialog({
  action,
  open,
  onOpenChange,
  onSubmit,
  pending,
}: {
  action?: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (body: unknown) => void;
  pending: boolean;
}) {
  const form = useForm<
    z.input<typeof movementSchema>,
    unknown,
    z.output<typeof movementSchema>
  >({ resolver: zodResolver(movementSchema) });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={`${titleCase(action ?? "Trip")} trip`}
      description={
        action === "arrive"
          ? "Arrival records the vehicle at the facility. Receive each manifest separately afterward."
          : action === "depart"
            ? "The server checks manifests, vehicle, driver, and references before any parcel moves."
            : "Record this trip transition with an operational note where relevant."
      }
      footer={
        <>
          <Button onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button
            variant={action === "cancel" ? "danger" : "primary"}
            disabled={pending}
            onClick={() => void form.handleSubmit(onSubmit)()}
          >
            Confirm {action}
          </Button>
        </>
      }
    >
      <div className="grid gap-4 sm:grid-cols-2">
        {["depart", "arrive"].includes(action ?? "") ? (
          <Field label="Odometer (km)" htmlFor="move-odometer">
            <Input
              id="move-odometer"
              type="number"
              min={0}
              {...form.register("odometerKm")}
            />
          </Field>
        ) : null}
        {action === "depart" ? (
          <Field label="Trip seal" htmlFor="move-seal">
            <Input id="move-seal" {...form.register("sealNumber")} />
          </Field>
        ) : null}
        {action === "arrive" ? (
          <Field label="Leg sequence" htmlFor="move-leg">
            <Input
              id="move-leg"
              type="number"
              min={1}
              {...form.register("legSequence")}
            />
          </Field>
        ) : null}
        {action === "cancel" ? (
          <Field
            className="sm:col-span-2"
            label="Cancellation reason"
            htmlFor="move-reason"
            required
          >
            <Textarea id="move-reason" {...form.register("reason")} />
          </Field>
        ) : null}
      </div>
    </Dialog>
  );
}

type FleetTab = "vehicles" | "carriers" | "drivers";
export function FleetPage() {
  const { hasPermission } = useAuth();
  const [tab, setTab] = useState<FleetTab>("vehicles");
  const [open, setOpen] = useState(false);
  const vehicles = useQuery({
    queryKey: ["vehicles"],
    queryFn: () => apiRequest<VehicleList>("/api/v1/vehicles?limit=100"),
  });
  const carriers = useQuery({
    queryKey: ["carriers"],
    queryFn: () => apiRequest<CarrierList>("/api/v1/carriers?limit=100"),
  });
  const drivers = useQuery({
    queryKey: ["drivers"],
    queryFn: () => apiRequest<DriverList>("/api/v1/drivers?limit=100"),
  });
  const query =
    tab === "vehicles" ? vehicles : tab === "carriers" ? carriers : drivers;
  const canManage = hasPermission(
    tab === "vehicles"
      ? "vehicle.manage"
      : tab === "carriers"
        ? "carrier.manage"
        : "driver.manage",
  );
  return (
    <>
      <PageHeader
        eyebrow="Operations · Line haul"
        title="Fleet and carriers"
        description="Low-volume configuration used by trip planning and departure validation."
        actions={
          canManage ? (
            <Button variant="primary" onClick={() => setOpen(true)}>
              <Plus aria-hidden className="h-4 w-4" /> Add {tab.slice(0, -1)}
            </Button>
          ) : undefined
        }
      />
      <Panel>
        <div className="flex gap-1 border-b p-2">
          {(["vehicles", "carriers", "drivers"] as FleetTab[]).map((item) => (
            <Button
              key={item}
              size="sm"
              variant={tab === item ? "primary" : "ghost"}
              onClick={() => setTab(item)}
            >
              {titleCase(item)}
            </Button>
          ))}
        </div>
        {query.isLoading ? (
          <LoadingState />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : tab === "vehicles" ? (
          <VehicleTable rows={vehicles.data?.data ?? []} />
        ) : tab === "carriers" ? (
          <CarrierTable rows={carriers.data?.data ?? []} />
        ) : (
          <DriverTable rows={drivers.data?.data ?? []} />
        )}
      </Panel>
      <FleetDialog tab={tab} open={open} onOpenChange={setOpen} />
    </>
  );
}
function VehicleTable({ rows }: { rows: Vehicle[] }) {
  return rows.length ? (
    <DataTable label="Vehicles">
      <thead>
        <tr>
          <TableHead>Registration</TableHead>
          <TableHead>Type</TableHead>
          <TableHead>Carrier</TableHead>
          <TableHead>Capacity</TableHead>
          <TableHead>State</TableHead>
        </tr>
      </thead>
      <tbody>
        {rows.map((item) => (
          <tr key={item.id}>
            <TableCell className="font-mono font-semibold">
              {item.registrationNumber}
            </TableCell>
            <TableCell>{titleCase(item.vehicleType ?? "")}</TableCell>
            <TableCell>
              {item.carrierName ?? item.carrierCode ?? "Own fleet"}
            </TableCell>
            <TableCell>{formatWeight(item.capacityWeightGrams)}</TableCell>
            <TableCell>
              <Badge tone={item.isActive ? "success" : "neutral"}>
                {item.isActive ? "Active" : "Inactive"}
              </Badge>
            </TableCell>
          </tr>
        ))}
      </tbody>
    </DataTable>
  ) : (
    <EmptyState
      title="No vehicles"
      description="Add the first road vehicle used for line haul."
    />
  );
}
function CarrierTable({ rows }: { rows: Carrier[] }) {
  return rows.length ? (
    <DataTable label="Carriers">
      <thead>
        <tr>
          <TableHead>Carrier</TableHead>
          <TableHead>Type</TableHead>
          <TableHead>Modes</TableHead>
          <TableHead>Contact</TableHead>
          <TableHead>State</TableHead>
        </tr>
      </thead>
      <tbody>
        {rows.map((item) => (
          <tr key={item.id}>
            <TableCell>
              <span className="font-mono font-semibold">{item.code}</span>
              <span className="block text-xs text-slate-500">{item.name}</span>
            </TableCell>
            <TableCell>{titleCase(item.carrierType ?? "")}</TableCell>
            <TableCell>{item.modes?.join(", ")}</TableCell>
            <TableCell>
              {item.contactName}
              <span className="block text-xs text-slate-500">
                {item.contactPhone}
              </span>
            </TableCell>
            <TableCell>
              <Badge tone={item.isActive ? "success" : "neutral"}>
                {item.isActive ? "Active" : "Inactive"}
              </Badge>
            </TableCell>
          </tr>
        ))}
      </tbody>
    </DataTable>
  ) : (
    <EmptyState
      title="No carriers"
      description="Add an own, contracted, or partner carrier."
    />
  );
}
function DriverTable({ rows }: { rows: Driver[] }) {
  return rows.length ? (
    <DataTable label="Drivers">
      <thead>
        <tr>
          <TableHead>Driver</TableHead>
          <TableHead>Phone</TableHead>
          <TableHead>Carrier</TableHead>
          <TableHead>State</TableHead>
        </tr>
      </thead>
      <tbody>
        {rows.map((item) => (
          <tr key={item.id}>
            <TableCell>
              <span className="font-mono font-semibold">{item.code}</span>
              <span className="block text-xs text-slate-500">
                {item.fullName}
              </span>
            </TableCell>
            <TableCell>{item.phone}</TableCell>
            <TableCell>{item.carrierCode || "Own fleet"}</TableCell>
            <TableCell>
              <Badge tone={item.isActive ? "success" : "neutral"}>
                {item.isActive ? "Active" : "Inactive"}
              </Badge>
            </TableCell>
          </tr>
        ))}
      </tbody>
    </DataTable>
  ) : (
    <EmptyState
      title="No drivers"
      description="Add the first driver available for assignment."
    />
  );
}

const vehicleSchema = z.object({
  carrierId: z.string().optional(),
  registrationNumber: z.string().min(3),
  vehicleType: z.enum([
    "BIKE",
    "VAN",
    "TEMPO",
    "TRUCK",
    "CONTAINER",
    "TRAILER",
    "OTHER",
  ]),
  capacityWeightGrams: optionalPositiveInteger,
  baseUnitId: z.string().optional(),
});
const carrierSchema = z.object({
  code: z.string().min(2),
  name: z.string().min(2),
  carrierType: z.enum(["OWN", "CONTRACTED", "PARTNER", "COURIER_PARTNER"]),
  modes: z.string().min(1),
  contactName: z.string().optional(),
  contactPhone: z.string().optional(),
  contactEmail: z.string().email().optional().or(z.literal("")),
});
const driverSchema = z.object({
  carrierId: z.string().optional(),
  code: z.string().min(2),
  fullName: z.string().min(2),
  phone: z.string().min(8),
  licenceNumber: z.string().optional(),
  baseUnitId: z.string().optional(),
});
function FleetDialog({
  tab,
  open,
  onOpenChange,
}: {
  tab: FleetTab;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const client = useQueryClient();
  const { toast } = useToast();
  const vehicleForm = useForm<
    z.input<typeof vehicleSchema>,
    unknown,
    z.output<typeof vehicleSchema>
  >({
    resolver: zodResolver(vehicleSchema),
    defaultValues: { vehicleType: "VAN" },
  });
  const carrierForm = useForm<z.infer<typeof carrierSchema>>({
    resolver: zodResolver(carrierSchema),
    defaultValues: { carrierType: "CONTRACTED", modes: "ROAD" },
  });
  const driverForm = useForm<z.infer<typeof driverSchema>>({
    resolver: zodResolver(driverSchema),
  });
  const mutation = useMutation({
    mutationFn: (body: unknown) =>
      apiRequest(`/${"api/v1"}/${tab}`, { method: "POST", body }),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: [tab] });
      toast({ tone: "success", title: `${titleCase(tab.slice(0, -1))} added` });
      onOpenChange(false);
    },
  });
  const submit =
    tab === "vehicles"
      ? vehicleForm.handleSubmit((values) => mutation.mutate(values))
      : tab === "carriers"
        ? carrierForm.handleSubmit((values) =>
            mutation.mutate({
              ...values,
              modes: values.modes.split(/[\s,]+/).filter(Boolean),
            }),
          )
        : driverForm.handleSubmit((values) => mutation.mutate(values));
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={`Add ${tab.slice(0, -1)}`}
      description="Create a line-haul planning resource."
      footer={
        <>
          <Button onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button variant="primary" onClick={() => void submit()}>
            Add {tab.slice(0, -1)}
          </Button>
        </>
      }
    >
      {tab === "vehicles" ? (
        <div className="grid gap-4 sm:grid-cols-2">
          <Field
            label="Registration number"
            htmlFor="fleet-registration"
            required
          >
            <Input
              id="fleet-registration"
              {...vehicleForm.register("registrationNumber")}
            />
          </Field>
          <Field label="Vehicle type" htmlFor="fleet-vehicle-type">
            <Select
              id="fleet-vehicle-type"
              {...vehicleForm.register("vehicleType")}
            >
              <option>BIKE</option>
              <option>VAN</option>
              <option>TEMPO</option>
              <option>TRUCK</option>
              <option>CONTAINER</option>
              <option>TRAILER</option>
              <option>OTHER</option>
            </Select>
          </Field>
          <Field label="Carrier ID" htmlFor="fleet-vehicle-carrier">
            <Input
              id="fleet-vehicle-carrier"
              {...vehicleForm.register("carrierId")}
            />
          </Field>
          <Field label="Capacity (g)" htmlFor="fleet-capacity">
            <Input
              id="fleet-capacity"
              type="number"
              {...vehicleForm.register("capacityWeightGrams")}
            />
          </Field>
        </div>
      ) : tab === "carriers" ? (
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Code" htmlFor="fleet-carrier-code" required>
            <Input id="fleet-carrier-code" {...carrierForm.register("code")} />
          </Field>
          <Field label="Name" htmlFor="fleet-carrier-name" required>
            <Input id="fleet-carrier-name" {...carrierForm.register("name")} />
          </Field>
          <Field label="Type" htmlFor="fleet-carrier-type">
            <Select
              id="fleet-carrier-type"
              {...carrierForm.register("carrierType")}
            >
              <option>OWN</option>
              <option>CONTRACTED</option>
              <option>PARTNER</option>
              <option>COURIER_PARTNER</option>
            </Select>
          </Field>
          <Field label="Modes" htmlFor="fleet-carrier-modes">
            <Input
              id="fleet-carrier-modes"
              {...carrierForm.register("modes")}
            />
          </Field>
          <Field label="Contact name" htmlFor="fleet-contact">
            <Input
              id="fleet-contact"
              {...carrierForm.register("contactName")}
            />
          </Field>
          <Field label="Contact phone" htmlFor="fleet-contact-phone">
            <Input
              id="fleet-contact-phone"
              {...carrierForm.register("contactPhone")}
            />
          </Field>
        </div>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Code" htmlFor="fleet-driver-code" required>
            <Input id="fleet-driver-code" {...driverForm.register("code")} />
          </Field>
          <Field label="Full name" htmlFor="fleet-driver-name" required>
            <Input
              id="fleet-driver-name"
              {...driverForm.register("fullName")}
            />
          </Field>
          <Field label="Phone" htmlFor="fleet-driver-phone" required>
            <Input id="fleet-driver-phone" {...driverForm.register("phone")} />
          </Field>
          <Field label="Licence" htmlFor="fleet-driver-licence">
            <Input
              id="fleet-driver-licence"
              {...driverForm.register("licenceNumber")}
            />
          </Field>
          <Field label="Carrier ID" htmlFor="fleet-driver-carrier">
            <Input
              id="fleet-driver-carrier"
              {...driverForm.register("carrierId")}
            />
          </Field>
        </div>
      )}
      {mutation.error ? <ErrorState error={mutation.error} /> : null}
    </Dialog>
  );
}
