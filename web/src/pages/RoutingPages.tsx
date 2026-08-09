import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowRight,
  CheckCircle2,
  Clock3,
  MapPin,
  Plus,
  Route as RouteIcon,
  Settings2,
  XCircle,
} from "lucide-react";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";
import {
  apiRequest,
  queryString,
  type OffsetPageOf,
  type ServiceabilityResult,
  type ServiceListResponse,
} from "../api/client";
import { useAuth } from "../auth/AuthProvider";
import { useToast } from "../components/ToastProvider";
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
  Pagination,
  Panel,
  PanelHeader,
  Select,
  StatusBadge,
  TableCell,
  TableHead,
  Textarea,
} from "../components/ui";
import { formatDateTime, titleCase } from "../lib/utils";

const testSchema = z.object({
  originPincode: z
    .string()
    .regex(/^[1-9][0-9]{5}$/, "Enter a valid origin postal code."),
  destinationPincode: z
    .string()
    .regex(/^[1-9][0-9]{5}$/, "Enter a valid destination postal code."),
  serviceCode: z.string().min(1, "Choose a courier product."),
});
type TestValues = z.infer<typeof testSchema>;

export function ServiceabilityPage() {
  const { hasPermission, user } = useAuth();
  const [result, setResult] = useState<ServiceabilityResult>();
  const [debug, setDebug] = useState(false);
  const services = useQuery({
    queryKey: ["courier-services", "active"],
    queryFn: () =>
      apiRequest<ServiceListResponse>(
        "/api/v1/courier-services?status=ACTIVE&limit=100",
      ),
    staleTime: 5 * 60_000,
  });
  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<TestValues>({
    resolver: zodResolver(testSchema),
    defaultValues: {
      originPincode: "",
      destinationPincode: "",
      serviceCode: "",
    },
  });
  const mutation = useMutation({
    mutationFn: (values: TestValues) =>
      apiRequest<ServiceabilityResult>(
        debug ? "/api/v1/serviceability/debug" : "/api/v1/serviceability/check",
        { method: "POST", body: values },
      ),
    onSuccess: setResult,
  });
  const submit = (values: TestValues) => mutation.mutate(values);
  return (
    <>
      <PageHeader
        eyebrow="Network / Routing"
        title="Serviceability tester"
        description="Confirm coverage, resolved facilities, route path, restrictions, and customer promise before booking."
      />
      <div className="grid gap-5 xl:grid-cols-[390px_minmax(0,1fr)]">
        <Panel className="h-fit">
          <PanelHeader
            title="Check a lane"
            description="Uses the same resolver as shipment booking."
          />
          <form
            className="space-y-4 p-4"
            onSubmit={(event) => void handleSubmit(submit)(event)}
          >
            <Field
              label="Origin postal code"
              htmlFor="originPincode"
              required
              error={errors.originPincode?.message}
            >
              <Input
                id="originPincode"
                inputMode="numeric"
                maxLength={6}
                autoFocus
                {...register("originPincode")}
              />
            </Field>
            <Field
              label="Destination postal code"
              htmlFor="destinationPincode"
              required
              error={errors.destinationPincode?.message}
            >
              <Input
                id="destinationPincode"
                inputMode="numeric"
                maxLength={6}
                {...register("destinationPincode")}
              />
            </Field>
            <Field
              label="Courier product"
              htmlFor="serviceCode"
              required
              error={errors.serviceCode?.message}
            >
              <Select id="serviceCode" {...register("serviceCode")}>
                <option value="">Select product</option>
                {services.data?.data?.map((service) => (
                  <option key={service.id} value={service.code}>
                    {service.code} — {service.name}
                  </option>
                ))}
              </Select>
            </Field>
            {hasPermission("routing.debug") ? (
              <label className="flex items-start gap-3 rounded-md border border-border p-3">
                <input
                  type="checkbox"
                  className="mt-0.5 h-4 w-4 accent-emerald-800"
                  checked={debug}
                  onChange={(event) => setDebug(event.target.checked)}
                />
                <span>
                  <span className="block text-sm font-medium">
                    Include decision trace
                  </span>
                  <span className="block text-xs text-slate-500">
                    Administrator view of every routing candidate.
                  </span>
                </span>
              </label>
            ) : null}
            {mutation.error ? (
              <InlineNotice tone="danger" title="Unable to resolve lane">
                {mutation.error.message}
              </InlineNotice>
            ) : null}
            <Button
              type="submit"
              variant="primary"
              loading={mutation.isPending}
              className="w-full"
            >
              <RouteIcon aria-hidden className="h-4 w-4" /> Check serviceability
            </Button>
          </form>
        </Panel>
        <Panel className="min-h-[430px]">
          {result ? (
            <ServiceabilityResultView
              result={result}
              timeZone={user?.organization?.timezone}
            />
          ) : (
            <EmptyState
              icon={RouteIcon}
              title="Enter a lane to begin"
              description="The resolved operational path and customer promise will appear here."
            />
          )}
        </Panel>
      </div>
    </>
  );
}

function ServiceabilityResultView({
  result,
  timeZone,
}: {
  result: ServiceabilityResult;
  timeZone?: string;
}) {
  if (!result.serviceable)
    return (
      <div className="p-6">
        <div className="flex items-start gap-4 rounded-lg border border-red-200 bg-red-50 p-5">
          <XCircle aria-hidden className="mt-0.5 h-7 w-7 text-danger" />
          <div>
            <Badge tone="danger">Not serviceable</Badge>
            <h2 className="mt-3 text-lg font-semibold text-red-950">
              {result.reasonMessage ?? reasonMessage(result.reasonCode)}
            </h2>
            <p className="mt-1 text-sm text-red-800">
              Reason: {titleCase(result.reasonCode ?? "Unavailable")}
            </p>
          </div>
        </div>
      </div>
    );
  const stops = [
    result.originBranch,
    result.originHub,
    ...(result.legs ?? []).slice(1, -1).map((leg) => ({
      code: leg.to,
      name: leg.toName,
      unitType: "TRANSIT_HUB",
    })),
    result.destinationHub,
    result.destinationBranch,
  ].filter(
    (item, index, values) =>
      item?.code &&
      values.findIndex((candidate) => candidate?.code === item.code) === index,
  );
  return (
    <div>
      <div className="flex flex-wrap items-start justify-between gap-4 border-b border-border p-5">
        <div className="flex items-start gap-3">
          <CheckCircle2 aria-hidden className="mt-0.5 h-6 w-6 text-success" />
          <div>
            <Badge tone="success">Serviceable</Badge>
            <h2 className="mt-2 text-lg font-semibold">
              {result.serviceName} · {result.serviceMode}
            </h2>
            <p className="mt-1 text-sm text-slate-500">
              {result.origin?.city}, {result.origin?.state} to{" "}
              {result.destination?.city}, {result.destination?.state}
            </p>
          </div>
        </div>
        <div className="text-right">
          <p className="text-xs font-medium uppercase tracking-wide text-slate-500">
            Promised delivery
          </p>
          <p className="mt-1 text-sm font-semibold">
            {formatDateTime(result.promisedDeliveryAt, timeZone)}
          </p>
          <p className="mt-1 text-xs text-slate-500">
            {result.slaHours} hour SLA
          </p>
        </div>
      </div>
      <div className="p-5">
        <div className="flex flex-wrap items-center gap-2">
          {result.resolutionSource ? (
            <Badge
              tone={
                result.resolutionSource === "OVERRIDE" ||
                result.resolutionSource === "FALLBACK_ROUTE"
                  ? "warning"
                  : "info"
              }
            >
              {titleCase(result.resolutionSource)}
            </Badge>
          ) : null}
          {result.cutoffApplied ? (
            <Badge tone="warning">
              <Clock3 aria-hidden className="h-3 w-3" /> Cut-off applied
            </Badge>
          ) : null}
          {result.destination?.isRemote || result.origin?.isRemote ? (
            <Badge tone="warning">
              <MapPin aria-hidden className="h-3 w-3" /> Remote area
            </Badge>
          ) : null}
        </div>
        <h3 className="mt-5 text-xs font-semibold uppercase tracking-wide text-slate-500">
          Resolved route
        </h3>
        <div className="mt-3 flex items-stretch overflow-x-auto pb-2">
          {stops.map((stop, index) => (
            <div
              key={`${stop?.code}-${index}`}
              className="flex min-w-fit items-center"
            >
              <div className="w-40 rounded-md border border-border bg-white p-3">
                <span className="text-[10px] font-semibold uppercase tracking-wide text-slate-400">
                  {titleCase(stop?.unitType ?? "Facility")}
                </span>
                <strong className="mt-1 block text-sm text-slate-900">
                  {stop?.code}
                </strong>
                <span className="mt-0.5 block truncate text-xs text-slate-500">
                  {stop?.name}
                </span>
              </div>
              {index < stops.length - 1 ? (
                <ArrowRight
                  aria-hidden
                  className="mx-2 h-4 w-4 shrink-0 text-slate-400"
                />
              ) : null}
            </div>
          ))}
        </div>
        <div className="mt-5 grid gap-3 sm:grid-cols-3">
          <Metric
            label="Transit time"
            value={`${result.transitHours ?? 0} hours`}
          />
          <Metric label="Route" value={result.routeCode ?? "Local delivery"} />
          <Metric
            label="Restrictions"
            value={`${result.restrictions?.length ?? 0}`}
          />
        </div>
        {result.cutoffApplied ? (
          <InlineNotice tone="warning" title="Dispatch cut-off was applied">
            This booking joins the next dispatch. The promised delivery already
            reflects that delay.
          </InlineNotice>
        ) : null}
        {result.restrictions?.length ? (
          <div className="mt-4 space-y-2">
            {result.restrictions.map((restriction, index) => (
              <div
                key={index}
                className="rounded-md border border-amber-200 bg-amber-50 p-3 text-sm text-amber-950"
              >
                {String(
                  restriction.message ??
                    restriction.code ??
                    "Restriction applies",
                )}
              </div>
            ))}
          </div>
        ) : null}
        {result.explanation?.steps?.length ? (
          <div className="mt-6">
            <h3 className="text-sm font-semibold">Routing decision trace</h3>
            <div className="mt-3 border-l-2 border-slate-200 pl-4">
              {result.explanation.steps.map((step, index) => (
                <div key={index} className="relative mb-4">
                  <span className="absolute -left-[21px] top-1 h-2.5 w-2.5 rounded-full bg-primary" />
                  <p className="text-sm font-medium">
                    {String(step.step ?? `Decision ${index + 1}`)}
                  </p>
                  <p className="mt-1 text-xs text-slate-500">
                    {String(
                      step.detail ??
                        step.outcome ??
                        "Routing candidate evaluated",
                    )}
                  </p>
                </div>
              ))}
            </div>
          </div>
        ) : null}
      </div>
    </div>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md bg-slate-50 p-3">
      <p className="text-xs text-slate-500">{label}</p>
      <p className="mt-1 text-sm font-semibold">{value}</p>
    </div>
  );
}
function reasonMessage(code?: string) {
  const messages: Record<string, string> = {
    ORIGIN_NOT_SERVICEABLE: "We do not collect from this postal code.",
    DESTINATION_NOT_SERVICEABLE: "We do not deliver to this postal code.",
    NO_ROUTE_AVAILABLE: "No route is configured for this lane and service.",
    PINCODE_NOT_FOUND: "We do not recognise this postal code.",
    ORIGIN_HUB_NOT_CONFIGURED:
      "The origin network configuration is incomplete. Contact operations.",
    DESTINATION_HUB_NOT_CONFIGURED:
      "The destination network configuration is incomplete. Contact operations.",
  };
  return messages[code ?? ""] ?? "This lane is not currently available.";
}

type RecordPage = OffsetPageOf<Record<string, unknown>>;
export function RoutesPage() {
  const { hasPermission } = useAuth();
  const [page, setPage] = useState(1);
  const [open, setOpen] = useState(false);
  const query = useQuery({
    queryKey: ["routes", page],
    queryFn: () =>
      apiRequest<RecordPage>(
        `/api/v1/routes${queryString({ page, limit: 25 })}`,
      ),
  });
  const rows = query.data?.data ?? [];
  return (
    <>
      <PageHeader
        eyebrow="Network / Routing"
        title="Routes"
        description="Configured facility-to-facility paths used by deterministic serviceability resolution."
        actions={
          hasPermission("route.manage") ? (
            <Button variant="primary" onClick={() => setOpen(true)}>
              <Plus aria-hidden className="h-4 w-4" /> Create route
            </Button>
          ) : undefined
        }
      />
      <Panel>
        {query.isLoading ? (
          <LoadingState label="Loading routes" />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : rows.length === 0 ? (
          <EmptyState
            icon={RouteIcon}
            title="No routes configured"
            description="Create a route to connect origin and destination facilities."
          />
        ) : (
          <>
            <DataTable label="Routes">
              <thead>
                <tr>
                  <TableHead>Code & name</TableHead>
                  <TableHead>Origin</TableHead>
                  <TableHead>Destination</TableHead>
                  <TableHead>Service</TableHead>
                  <TableHead>Transit</TableHead>
                  <TableHead>Status</TableHead>
                </tr>
              </thead>
              <tbody>
                {rows.map((row, index) => (
                  <tr key={String(row.id ?? index)}>
                    <TableCell>
                      <strong className="text-primary">
                        {text(row, "code")}
                      </strong>
                      <span className="ml-2">{text(row, "name")}</span>
                    </TableCell>
                    <TableCell>
                      {text(row, "originUnitCode", "originCode")}
                    </TableCell>
                    <TableCell>
                      {text(row, "destinationUnitCode", "destinationCode")}
                    </TableCell>
                    <TableCell>
                      {text(row, "serviceCode") || "All products"}
                    </TableCell>
                    <TableCell>{text(row, "transitHours")} hours</TableCell>
                    <TableCell>
                      <StatusBadge status={text(row, "status") || "ACTIVE"} />
                    </TableCell>
                  </tr>
                ))}
              </tbody>
            </DataTable>
            <Pagination
              page={query.data?.pagination?.page ?? page}
              totalPages={query.data?.pagination?.totalPages ?? 1}
              onPageChange={setPage}
            />
          </>
        )}
      </Panel>
      <CreateRouteDialog open={open} onOpenChange={setOpen} />
    </>
  );
}

const routeSchema = z.object({
  code: z.string().regex(/^[A-Z0-9][A-Z0-9_-]{1,31}$/),
  name: z.string().min(2),
  originUnitCode: z.string().min(2),
  destinationUnitCode: z.string().min(2),
  serviceCode: z.string().optional(),
  transitHours: z.number().int().min(1).max(8760),
  mode: z.enum(["ROAD", "AIR", "RAIL", "PARTNER"]),
  isFallback: z.boolean(),
});
type RouteValues = z.infer<typeof routeSchema>;
function CreateRouteDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (value: boolean) => void;
}) {
  const client = useQueryClient();
  const { toast } = useToast();
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<RouteValues>({
    resolver: zodResolver(routeSchema),
    defaultValues: { transitHours: 24, mode: "ROAD", isFallback: false },
  });
  const mutation = useMutation({
    mutationFn: (values: RouteValues) =>
      apiRequest("/api/v1/routes", {
        method: "POST",
        body: {
          code: values.code.toUpperCase(),
          name: values.name,
          originUnitCode: values.originUnitCode.toUpperCase(),
          destinationUnitCode: values.destinationUnitCode.toUpperCase(),
          serviceCode: values.serviceCode?.toUpperCase() || undefined,
          transitHours: values.transitHours,
          isFallback: values.isFallback,
          legs: [
            {
              sequence: 1,
              fromUnitCode: values.originUnitCode.toUpperCase(),
              toUnitCode: values.destinationUnitCode.toUpperCase(),
              mode: values.mode,
              transitHours: values.transitHours,
            },
          ],
        },
      }),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ["routes"] });
      toast({ tone: "success", title: "Route created" });
      reset();
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Create route"
      description="Create a direct first leg. Additional multi-leg route editing needs an updated backend contract."
      footer={
        <>
          <Button onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button
            variant="primary"
            loading={mutation.isPending}
            onClick={() =>
              void handleSubmit((values) => mutation.mutate(values))()
            }
          >
            Create route
          </Button>
        </>
      }
    >
      <form
        className="grid gap-4 sm:grid-cols-2"
        onSubmit={(event) => event.preventDefault()}
      >
        <Field
          label="Route code"
          htmlFor="routeCode"
          required
          error={errors.code?.message}
        >
          <Input id="routeCode" className="uppercase" {...register("code")} />
        </Field>
        <Field
          label="Route name"
          htmlFor="routeName"
          required
          error={errors.name?.message}
        >
          <Input id="routeName" {...register("name")} />
        </Field>
        <Field
          label="Origin facility code"
          htmlFor="originUnitCode"
          required
          error={errors.originUnitCode?.message}
        >
          <Input
            id="originUnitCode"
            className="uppercase"
            {...register("originUnitCode")}
          />
        </Field>
        <Field
          label="Destination facility code"
          htmlFor="destinationUnitCode"
          required
          error={errors.destinationUnitCode?.message}
        >
          <Input
            id="destinationUnitCode"
            className="uppercase"
            {...register("destinationUnitCode")}
          />
        </Field>
        <Field label="Courier product code" htmlFor="serviceCode">
          <Input
            id="serviceCode"
            className="uppercase"
            {...register("serviceCode")}
          />
        </Field>
        <Field
          label="Transit hours"
          htmlFor="transitHours"
          required
          error={errors.transitHours?.message}
        >
          <Input
            id="transitHours"
            type="number"
            {...register("transitHours", { valueAsNumber: true })}
          />
        </Field>
        <Field label="Transport mode" htmlFor="mode">
          <Select id="mode" {...register("mode")}>
            <option>ROAD</option>
            <option>AIR</option>
            <option>RAIL</option>
            <option>PARTNER</option>
          </Select>
        </Field>
        <label className="flex items-center gap-2 self-end pb-2 text-sm">
          <input
            type="checkbox"
            className="h-4 w-4 accent-emerald-800"
            {...register("isFallback")}
          />{" "}
          Fallback route
        </label>
        {mutation.error ? (
          <p role="alert" className="sm:col-span-2 text-sm text-danger">
            {mutation.error.message}
          </p>
        ) : null}
      </form>
    </Dialog>
  );
}

export function RoutingRulesPage() {
  const { hasPermission } = useAuth();
  const [tab, setTab] = useState<"areas" | "closures">("areas");
  const [page, setPage] = useState(1);
  const [createOpen, setCreateOpen] = useState(false);
  const path =
    tab === "areas"
      ? "/api/v1/serviceability/service-areas"
      : "/api/v1/serviceability/closures";
  const query = useQuery({
    queryKey: ["routing-rules", tab, page],
    queryFn: () =>
      apiRequest<RecordPage>(`${path}${queryString({ page, limit: 25 })}`),
  });
  const rows = query.data?.data ?? [];
  return (
    <>
      <PageHeader
        eyebrow="Network / Routing"
        title="Routing rules"
        description="Coverage declarations and temporary closures evaluated before a route is selected."
        actions={
          (tab === "areas" && hasPermission("service_area.manage")) ||
          (tab === "closures" && hasPermission("closure.manage")) ? (
            <Button variant="primary" onClick={() => setCreateOpen(true)}>
              <Plus aria-hidden className="h-4 w-4" />
              {tab === "areas" ? "Declare coverage" : "Declare closure"}
            </Button>
          ) : undefined
        }
      />
      <Panel>
        <div className="flex gap-1 border-b p-2">
          <button
            className={`rounded-md px-3 py-2 text-sm font-semibold ${tab === "areas" ? "bg-emerald-50 text-primary" : "text-slate-600"}`}
            onClick={() => setTab("areas")}
          >
            Service areas
          </button>
          <button
            className={`rounded-md px-3 py-2 text-sm font-semibold ${tab === "closures" ? "bg-emerald-50 text-primary" : "text-slate-600"}`}
            onClick={() => setTab("closures")}
          >
            Temporary closures
          </button>
        </div>
        {query.isLoading ? (
          <LoadingState />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : rows.length === 0 ? (
          <EmptyState
            icon={Settings2}
            title={`No ${tab === "areas" ? "service areas" : "closures"} found`}
            description="No configuration records were returned by the API."
          />
        ) : (
          <>
            <DataTable
              label={tab === "areas" ? "Service areas" : "Temporary closures"}
            >
              <thead>
                <tr>
                  <TableHead>Facility / postal code</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead>Priority / reason</TableHead>
                  <TableHead>Effective period</TableHead>
                  <TableHead>Status</TableHead>
                </tr>
              </thead>
              <tbody>
                {rows.map((row, index) => (
                  <tr key={String(row.id ?? index)}>
                    <TableCell>
                      {text(row, "operatingUnitCode", "pincode")}
                    </TableCell>
                    <TableCell>
                      {titleCase(text(row, "areaType", "closureType"))}
                    </TableCell>
                    <TableCell>{text(row, "priority", "reason")}</TableCell>
                    <TableCell>
                      {text(row, "effectiveFrom", "startsAt")} →{" "}
                      {text(row, "effectiveTo", "endsAt") || "Open"}
                    </TableCell>
                    <TableCell>
                      <StatusBadge status={text(row, "status") || "ACTIVE"} />
                    </TableCell>
                  </tr>
                ))}
              </tbody>
            </DataTable>
            <Pagination
              page={query.data?.pagination?.page ?? page}
              totalPages={query.data?.pagination?.totalPages ?? 1}
              onPageChange={setPage}
            />
          </>
        )}
      </Panel>
      <RoutingRuleDialog
        type={tab}
        open={createOpen}
        onOpenChange={setCreateOpen}
      />
    </>
  );
}

const serviceAreaSchema = z.object({
  operatingUnitId: z.string().min(2),
  pincode: z.string().regex(/^[1-9][0-9]{5}$/),
  areaType: z.enum(["PICKUP", "DELIVERY", "BOTH"]),
  priority: z.number().int().min(0).max(1000),
  isRemote: z.boolean(),
  cutoffTime: z.string().optional(),
  effectiveFrom: z.string().optional(),
  effectiveTo: z.string().optional(),
});
const closureSchema = z
  .object({
    operatingUnitCode: z.string().optional(),
    pincode: z.string().optional(),
    closureType: z.enum(["FULL", "PICKUP_ONLY", "DELIVERY_ONLY"]),
    reasonCode: z.enum([
      "WEATHER",
      "STRIKE",
      "HOLIDAY",
      "LAW_AND_ORDER",
      "INFRASTRUCTURE",
      "OTHER",
    ]),
    reason: z.string().min(5).max(500),
    startsAt: z.string().min(1),
    endsAt: z.string().min(1),
  })
  .superRefine((value, context) => {
    if (Boolean(value.operatingUnitCode) === Boolean(value.pincode))
      context.addIssue({
        code: "custom",
        path: ["operatingUnitCode"],
        message: "Enter exactly one facility code or postal code.",
      });
    if (value.pincode && !/^[1-9][0-9]{5}$/.test(value.pincode))
      context.addIssue({
        code: "custom",
        path: ["pincode"],
        message: "Enter a valid postal code.",
      });
  });

function RoutingRuleDialog({
  type,
  open,
  onOpenChange,
}: {
  type: "areas" | "closures";
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  return type === "areas" ? (
    <ServiceAreaDialog open={open} onOpenChange={onOpenChange} />
  ) : (
    <ClosureDialog open={open} onOpenChange={onOpenChange} />
  );
}

function ServiceAreaDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const client = useQueryClient();
  const { toast } = useToast();
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<z.infer<typeof serviceAreaSchema>>({
    resolver: zodResolver(serviceAreaSchema),
    defaultValues: { areaType: "BOTH", priority: 100, isRemote: false },
  });
  const mutation = useMutation({
    mutationFn: (values: z.infer<typeof serviceAreaSchema>) =>
      apiRequest("/api/v1/serviceability/service-areas", {
        method: "POST",
        body: {
          ...values,
          cutoffTime: values.cutoffTime || undefined,
          effectiveFrom: values.effectiveFrom
            ? new Date(values.effectiveFrom).toISOString()
            : undefined,
          effectiveTo: values.effectiveTo
            ? new Date(values.effectiveTo).toISOString()
            : undefined,
        },
      }),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ["routing-rules", "areas"] });
      toast({ tone: "success", title: "Service area declared" });
      reset();
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Declare service area"
      description="Higher priority coverage wins when multiple facilities serve the same postal code."
      footer={
        <>
          <Button onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button
            variant="primary"
            loading={mutation.isPending}
            onClick={() =>
              void handleSubmit((values) => mutation.mutate(values))()
            }
          >
            Declare coverage
          </Button>
        </>
      }
    >
      <form
        className="grid gap-4 sm:grid-cols-2"
        onSubmit={(event) => event.preventDefault()}
      >
        <Field
          label="Operating unit ID"
          htmlFor="areaUnit"
          required
          error={errors.operatingUnitId?.message}
        >
          <Input id="areaUnit" {...register("operatingUnitId")} />
        </Field>
        <Field
          label="Postal code"
          htmlFor="areaPincode"
          required
          error={errors.pincode?.message}
        >
          <Input
            id="areaPincode"
            inputMode="numeric"
            maxLength={6}
            {...register("pincode")}
          />
        </Field>
        <Field label="Coverage" htmlFor="areaType">
          <Select id="areaType" {...register("areaType")}>
            <option>PICKUP</option>
            <option>DELIVERY</option>
            <option>BOTH</option>
          </Select>
        </Field>
        <Field label="Priority" htmlFor="areaPriority">
          <Input
            id="areaPriority"
            type="number"
            {...register("priority", { valueAsNumber: true })}
          />
        </Field>
        <Field label="Cut-off time" htmlFor="areaCutoff">
          <Input id="areaCutoff" type="time" {...register("cutoffTime")} />
        </Field>
        <label className="flex items-center gap-2 self-end pb-2 text-sm">
          <input
            type="checkbox"
            className="h-4 w-4 accent-emerald-800"
            {...register("isRemote")}
          />{" "}
          Remote area
        </label>
        <Field label="Effective from" htmlFor="areaFrom">
          <Input
            id="areaFrom"
            type="datetime-local"
            {...register("effectiveFrom")}
          />
        </Field>
        <Field label="Effective to" htmlFor="areaTo">
          <Input
            id="areaTo"
            type="datetime-local"
            {...register("effectiveTo")}
          />
        </Field>
        {mutation.error ? (
          <p role="alert" className="sm:col-span-2 text-sm text-danger">
            {mutation.error.message}
          </p>
        ) : null}
      </form>
    </Dialog>
  );
}

function ClosureDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const client = useQueryClient();
  const { toast } = useToast();
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<z.infer<typeof closureSchema>>({
    resolver: zodResolver(closureSchema),
    defaultValues: { closureType: "FULL", reasonCode: "OTHER" },
  });
  const mutation = useMutation({
    mutationFn: (values: z.infer<typeof closureSchema>) =>
      apiRequest("/api/v1/serviceability/closures", {
        method: "POST",
        body: {
          operatingUnitCode:
            values.operatingUnitCode?.toUpperCase() || undefined,
          pincode: values.pincode || undefined,
          closureType: values.closureType,
          reasonCode: values.reasonCode,
          reason: values.reason,
          startsAt: new Date(values.startsAt).toISOString(),
          endsAt: new Date(values.endsAt).toISOString(),
        },
      }),
    onSuccess: () => {
      void client.invalidateQueries({
        queryKey: ["routing-rules", "closures"],
      });
      toast({ tone: "success", title: "Temporary closure declared" });
      reset();
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Declare temporary closure"
      description="Enter exactly one facility code or postal code. Active bookings will receive the configured customer-safe reason."
      footer={
        <>
          <Button onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button
            variant="danger"
            loading={mutation.isPending}
            onClick={() =>
              void handleSubmit((values) => mutation.mutate(values))()
            }
          >
            Declare closure
          </Button>
        </>
      }
    >
      <form
        className="grid gap-4 sm:grid-cols-2"
        onSubmit={(event) => event.preventDefault()}
      >
        <Field
          label="Facility code"
          htmlFor="closureUnit"
          error={errors.operatingUnitCode?.message}
          hint="Use this or a postal code, not both."
        >
          <Input
            id="closureUnit"
            className="uppercase"
            {...register("operatingUnitCode")}
          />
        </Field>
        <Field
          label="Postal code"
          htmlFor="closurePincode"
          error={errors.pincode?.message}
        >
          <Input
            id="closurePincode"
            inputMode="numeric"
            maxLength={6}
            {...register("pincode")}
          />
        </Field>
        <Field label="Closure type" htmlFor="closureType">
          <Select id="closureType" {...register("closureType")}>
            <option>FULL</option>
            <option>PICKUP_ONLY</option>
            <option>DELIVERY_ONLY</option>
          </Select>
        </Field>
        <Field label="Reason code" htmlFor="closureReasonCode">
          <Select id="closureReasonCode" {...register("reasonCode")}>
            <option>WEATHER</option>
            <option>STRIKE</option>
            <option>HOLIDAY</option>
            <option>LAW_AND_ORDER</option>
            <option>INFRASTRUCTURE</option>
            <option>OTHER</option>
          </Select>
        </Field>
        <Field label="Starts at" htmlFor="closureStarts" required>
          <Input
            id="closureStarts"
            type="datetime-local"
            {...register("startsAt")}
          />
        </Field>
        <Field label="Ends at" htmlFor="closureEnds" required>
          <Input
            id="closureEnds"
            type="datetime-local"
            {...register("endsAt")}
          />
        </Field>
        <Field
          label="Customer-safe reason"
          htmlFor="closureReason"
          required
          error={errors.reason?.message}
          className="sm:col-span-2"
        >
          <Textarea id="closureReason" {...register("reason")} />
        </Field>
        {mutation.error ? (
          <p role="alert" className="sm:col-span-2 text-sm text-danger">
            {mutation.error.message}
          </p>
        ) : null}
      </form>
    </Dialog>
  );
}

export function OverridesPage() {
  const { toast } = useToast();
  const schema = z.object({
    originPincode: z.string().regex(/^[1-9][0-9]{5}$/),
    destinationPincode: z.string().regex(/^[1-9][0-9]{5}$/),
    serviceCode: z.string().optional(),
    routeCode: z.string().min(2),
    priority: z.number().int(),
    reason: z.string().min(5).max(500),
    effectiveFrom: z.string().optional(),
    effectiveTo: z.string().optional(),
  });
  type Values = z.infer<typeof schema>;
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { priority: 500 },
  });
  const mutation = useMutation({
    mutationFn: (values: Values) =>
      apiRequest("/api/v1/routes/overrides", {
        method: "POST",
        body: {
          ...values,
          serviceCode: values.serviceCode?.toUpperCase() || undefined,
          routeCode: values.routeCode.toUpperCase(),
          effectiveFrom: values.effectiveFrom || undefined,
          effectiveTo: values.effectiveTo || undefined,
        },
      }),
    onSuccess: () => {
      toast({
        tone: "success",
        title: "Routing override created",
        description: "It now takes precedence for this exact lane.",
      });
      reset();
    },
  });
  return (
    <>
      <PageHeader
        eyebrow="Network / Routing"
        title="Create routing override"
        description="Force an exact lane onto a configured route. The API does not yet expose override listing or removal."
      />
      <Panel className="max-w-3xl">
        <PanelHeader
          title="Override details"
          description="A substantive reason is copied into the audit trail and affected route snapshots."
        />
        <form
          className="grid gap-4 p-5 sm:grid-cols-2"
          onSubmit={(event) =>
            void handleSubmit((values) => mutation.mutate(values))(event)
          }
        >
          <Field
            label="Origin postal code"
            htmlFor="origin"
            required
            error={errors.originPincode?.message}
          >
            <Input
              id="origin"
              inputMode="numeric"
              maxLength={6}
              {...register("originPincode")}
            />
          </Field>
          <Field
            label="Destination postal code"
            htmlFor="destination"
            required
            error={errors.destinationPincode?.message}
          >
            <Input
              id="destination"
              inputMode="numeric"
              maxLength={6}
              {...register("destinationPincode")}
            />
          </Field>
          <Field label="Courier product code" htmlFor="overrideService">
            <Input
              id="overrideService"
              className="uppercase"
              {...register("serviceCode")}
            />
          </Field>
          <Field
            label="Route code"
            htmlFor="overrideRoute"
            required
            error={errors.routeCode?.message}
          >
            <Input
              id="overrideRoute"
              className="uppercase"
              {...register("routeCode")}
            />
          </Field>
          <Field label="Priority" htmlFor="priority">
            <Input
              id="priority"
              type="number"
              {...register("priority", { valueAsNumber: true })}
            />
          </Field>
          <Field label="Effective from" htmlFor="effectiveFrom">
            <Input
              id="effectiveFrom"
              type="datetime-local"
              {...register("effectiveFrom")}
            />
          </Field>
          <Field label="Effective to" htmlFor="effectiveTo">
            <Input
              id="effectiveTo"
              type="datetime-local"
              {...register("effectiveTo")}
            />
          </Field>
          <Field
            label="Reason"
            htmlFor="reason"
            required
            error={errors.reason?.message}
            className="sm:col-span-2"
          >
            <Input id="reason" {...register("reason")} />
          </Field>
          {mutation.error ? (
            <p role="alert" className="sm:col-span-2 text-sm text-danger">
              {mutation.error.message}
            </p>
          ) : null}
          <div className="sm:col-span-2">
            <Button
              type="submit"
              variant="primary"
              loading={mutation.isPending}
            >
              Create override
            </Button>
          </div>
        </form>
      </Panel>
    </>
  );
}

function text(record: Record<string, unknown>, ...keys: string[]) {
  for (const key of keys) {
    const value = record[key];
    if (typeof value === "string" || typeof value === "number")
      return String(value);
  }
  return "";
}
