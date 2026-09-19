import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Building2,
  GitBranch,
  List,
  Pencil,
  Plus,
  SearchX,
  Users,
} from "lucide-react";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { Link, useParams } from "react-router-dom";
import { z } from "zod";
import type { components } from "../api/schema";
import {
  apiRequest,
  queryString,
  type FranchiseSummary,
  type OffsetPageOf,
  type OperatingUnit,
  type OperatingUnitListResponse,
  type OperatingUnitSummary,
} from "../api/client";
import { useAuth } from "../auth/AuthProvider";
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
  Input,
  LoadingState,
  PageHeader,
  Pagination,
  Panel,
  SearchInput,
  Select,
  StatusBadge,
  TableCell,
  TableHead,
  Textarea,
} from "../components/ui";
import { titleCase } from "../lib/utils";

type UnitType = components["schemas"]["UnitType"];
interface HierarchyNode {
  id?: string;
  code?: string;
  name?: string;
  unitType?: UnitType;
  status?: string;
  depth?: number;
  path?: string[];
}
interface HierarchyResponse {
  data?: HierarchyNode[];
}
type FranchisePage = OffsetPageOf<FranchiseSummary>;

const unitSchema = z.object({
  code: z
    .string()
    .regex(
      /^[A-Z0-9][A-Z0-9_-]{1,31}$/,
      "Use 2–32 uppercase letters, numbers, hyphens, or underscores.",
    ),
  name: z.string().min(2).max(160),
  unitType: z.enum([
    "HEAD_OFFICE",
    "REGIONAL_HUB",
    "TRANSIT_HUB",
    "DELIVERY_HUB",
    "COMPANY_BRANCH",
    "FRANCHISE_BRANCH",
  ]),
  parentUnitId: z.string().optional(),
  addressLine1: z.string().min(3).max(200),
  addressLine2: z.string().optional(),
  pincode: z
    .string()
    .regex(/^[1-9][0-9]{5}$/, "Enter a valid 6-digit postal code."),
  contactName: z.string().optional(),
  contactPhone: z.string().optional(),
  contactEmail: z.union([z.email(), z.literal("")]).optional(),
});
type UnitValues = z.infer<typeof unitSchema>;

export function OperatingUnitsPage({ kind }: { kind: "hubs" | "branches" }) {
  const { hasPermission } = useAuth();
  const [page, setPage] = useState(1);
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState("");
  const [view, setView] = useState<"table" | "tree">("table");
  const [createOpen, setCreateOpen] = useState(false);
  const unitTypes =
    kind === "hubs"
      ? ["REGIONAL_HUB", "TRANSIT_HUB", "DELIVERY_HUB"]
      : ["COMPANY_BRANCH", "FRANCHISE_BRANCH"];
  const endpoint =
    kind === "hubs" ? "/api/v1/network/hubs" : "/api/v1/network/branches";
  const query = useQuery({
    queryKey: ["operating-units", kind, { page, search, status }],
    queryFn: () =>
      apiRequest<OperatingUnitListResponse>(
        `${endpoint}${queryString({ page, limit: 25, search, status })}`,
      ),
    placeholderData: (previous) => previous,
  });
  const hierarchy = useQuery({
    queryKey: ["operating-units", "hierarchy"],
    queryFn: () =>
      apiRequest<HierarchyResponse>(
        "/api/v1/network/operating-units/hierarchy?maxDepth=8",
      ),
    enabled: view === "tree",
  });
  const rows = (query.data?.data ?? []).filter(
    (unit) => !unit.unitType || unitTypes.includes(unit.unitType),
  );
  const label = kind === "hubs" ? "Hubs" : "Branches";
  return (
    <>
      <PageHeader
        eyebrow="Network"
        title={label}
        description={
          kind === "hubs"
            ? "Regional, transit, and delivery facilities that anchor the courier network."
            : "Company and franchise branches that book, collect, and deliver shipments."
        }
        actions={
          hasPermission("operating_unit.manage") ? (
            <Button variant="primary" onClick={() => setCreateOpen(true)}>
              <Plus aria-hidden className="h-4 w-4" /> Add{" "}
              {kind === "hubs" ? "hub" : "branch"}
            </Button>
          ) : undefined
        }
      />
      <Panel>
        <div className="flex flex-wrap gap-3 border-b border-border p-4">
          <SearchInput
            value={search}
            onChange={(event) => {
              setSearch(event.target.value);
              setPage(1);
            }}
            placeholder={`Search ${kind}`}
            className="min-w-[240px] flex-1"
            aria-label={`Search ${kind}`}
          />
          <Select
            aria-label={`Filter ${kind} by status`}
            value={status}
            onChange={(event) => setStatus(event.target.value)}
            className="w-44"
          >
            <option value="">All statuses</option>
            <option>ACTIVE</option>
            <option>INACTIVE</option>
            <option>SUSPENDED</option>
          </Select>
          <div className="flex rounded-md border border-border p-0.5">
            <button
              className={`rounded px-2.5 py-1.5 text-xs font-semibold ${view === "table" ? "bg-muted" : ""}`}
              onClick={() => setView("table")}
            >
              <List aria-hidden className="mr-1 inline h-3.5 w-3.5" /> Table
            </button>
            <button
              className={`rounded px-2.5 py-1.5 text-xs font-semibold ${view === "tree" ? "bg-muted" : ""}`}
              onClick={() => setView("tree")}
            >
              <GitBranch aria-hidden className="mr-1 inline h-3.5 w-3.5" />{" "}
              Hierarchy
            </button>
          </div>
        </div>
        {view === "tree" ? (
          hierarchy.isLoading ? (
            <LoadingState label="Loading hierarchy" />
          ) : hierarchy.error ? (
            <ErrorState
              error={hierarchy.error}
              retry={() => void hierarchy.refetch()}
            />
          ) : (
            <div className="p-4">
              {hierarchy.data?.data
                ?.filter(
                  (unit) => !unit.unitType || unitTypes.includes(unit.unitType),
                )
                .map((unit) => (
                  <Link
                    to={`/network/units/${unit.id}`}
                    key={unit.id}
                    className="mb-1 flex items-center gap-3 rounded-md border border-transparent px-3 py-2 hover:border-border hover:bg-slate-50"
                    style={{
                      marginLeft: `${Math.min(unit.depth ?? 0, 6) * 20}px`,
                    }}
                  >
                    <span className="grid h-8 w-8 place-items-center rounded bg-emerald-50">
                      <Building2 aria-hidden className="h-4 w-4 text-primary" />
                    </span>
                    <span className="min-w-0 flex-1">
                      <strong className="block truncate text-sm">
                        {unit.code} · {unit.name}
                      </strong>
                      <span className="text-xs text-slate-500">
                        {titleCase(unit.unitType ?? "Facility")}
                      </span>
                    </span>
                    <StatusBadge status={unit.status} />
                  </Link>
                ))}
            </div>
          )
        ) : query.isLoading ? (
          <LoadingState label={`Loading ${kind}`} />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : rows.length === 0 ? (
          <EmptyState
            icon={SearchX}
            title={`No ${kind} found`}
            description="Adjust the search or filters."
          />
        ) : (
          <>
            <DataTable label={label}>
              <thead>
                <tr>
                  <TableHead>Code & name</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead>Parent</TableHead>
                  <TableHead>Location</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Version</TableHead>
                </tr>
              </thead>
              <tbody>
                {rows.map((unit) => (
                  <tr key={unit.id} className="hover:bg-slate-50">
                    <TableCell>
                      <Link
                        to={`/network/units/${unit.id}`}
                        className="font-semibold text-primary hover:underline"
                      >
                        {unit.code}
                      </Link>
                      <span className="ml-2 text-slate-800">{unit.name}</span>
                    </TableCell>
                    <TableCell>
                      {titleCase(unit.unitType ?? "Facility")}
                    </TableCell>
                    <TableCell>
                      {unit.parent
                        ? `${unit.parent.code} · ${unit.parent.name}`
                        : "Root"}
                    </TableCell>
                    <TableCell>
                      {unit.address?.city || "—"}{" "}
                      {unit.address?.pincode ? `· ${unit.address.pincode}` : ""}
                    </TableCell>
                    <TableCell>
                      <StatusBadge status={unit.status} />
                    </TableCell>
                    <TableCell>v{unit.version ?? 1}</TableCell>
                  </tr>
                ))}
              </tbody>
            </DataTable>
            <Pagination
              page={query.data?.pagination?.page ?? page}
              totalPages={query.data?.pagination?.totalPages ?? 1}
              onPageChange={setPage}
              label={`${query.data?.pagination?.totalItems ?? rows.length} facilities`}
            />
          </>
        )}{" "}
      </Panel>
      <CreateUnitDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        defaultType={kind === "hubs" ? "REGIONAL_HUB" : "COMPANY_BRANCH"}
      />
    </>
  );
}

function CreateUnitDialog({
  open,
  onOpenChange,
  defaultType,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  defaultType: UnitType;
}) {
  const client = useQueryClient();
  const { toast } = useToast();
  const parents = useQuery({
    queryKey: ["operating-units", "picker"],
    queryFn: () =>
      apiRequest<OperatingUnitListResponse>(
        "/api/v1/network/operating-units?limit=100",
      ),
    enabled: open,
  });
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<UnitValues>({
    resolver: zodResolver(unitSchema),
    defaultValues: {
      unitType: defaultType,
      code: "",
      name: "",
      addressLine1: "",
      pincode: "",
    },
  });
  const mutation = useMutation({
    mutationFn: (values: UnitValues) =>
      apiRequest<OperatingUnitSummary>("/api/v1/network/operating-units", {
        method: "POST",
        body: {
          ...values,
          code: values.code.toUpperCase(),
          parentUnitId: values.parentUnitId || undefined,
          addressLine2: values.addressLine2 || undefined,
          contactName: values.contactName || undefined,
          contactPhone: values.contactPhone || undefined,
          contactEmail: values.contactEmail || undefined,
        },
      }),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ["operating-units"] });
      toast({ tone: "success", title: "Operating unit created" });
      reset();
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Add operating unit"
      description="Codes and facility type become immutable after creation."
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
            Create unit
          </Button>
        </>
      }
    >
      <form
        className="grid gap-4 sm:grid-cols-2"
        onSubmit={(event) => event.preventDefault()}
      >
        <Field
          label="Code"
          htmlFor="code"
          required
          error={errors.code?.message}
        >
          <Input id="code" className="uppercase" {...register("code")} />
        </Field>
        <Field
          label="Name"
          htmlFor="name"
          required
          error={errors.name?.message}
        >
          <Input id="name" {...register("name")} />
        </Field>
        <Field
          label="Facility type"
          htmlFor="unitType"
          required
          error={errors.unitType?.message}
        >
          <Select id="unitType" {...register("unitType")}>
            <option value="REGIONAL_HUB">Regional hub</option>
            <option value="TRANSIT_HUB">Transit hub</option>
            <option value="DELIVERY_HUB">Delivery hub</option>
            <option value="COMPANY_BRANCH">Company branch</option>
            <option value="FRANCHISE_BRANCH">Franchise branch</option>
          </Select>
        </Field>
        <Field label="Parent facility" htmlFor="parentUnitId">
          <Select id="parentUnitId" {...register("parentUnitId")}>
            <option value="">No parent</option>
            {parents.data?.data?.map((unit) => (
              <option key={unit.id} value={unit.id}>
                {unit.code} — {unit.name}
              </option>
            ))}
          </Select>
        </Field>
        <Field
          label="Address line 1"
          htmlFor="addressLine1"
          required
          error={errors.addressLine1?.message}
          className="sm:col-span-2"
        >
          <Input id="addressLine1" {...register("addressLine1")} />
        </Field>
        <Field label="Address line 2" htmlFor="addressLine2">
          <Input id="addressLine2" {...register("addressLine2")} />
        </Field>
        <Field
          label="Postal code"
          htmlFor="pincode"
          required
          error={errors.pincode?.message}
        >
          <Input
            id="pincode"
            inputMode="numeric"
            maxLength={6}
            {...register("pincode")}
          />
        </Field>
        <Field label="Contact name" htmlFor="contactName">
          <Input id="contactName" {...register("contactName")} />
        </Field>
        <Field label="Contact phone" htmlFor="contactPhone">
          <Input
            id="contactPhone"
            inputMode="tel"
            {...register("contactPhone")}
          />
        </Field>
        <Field
          label="Contact email"
          htmlFor="contactEmail"
          error={errors.contactEmail?.message}
          className="sm:col-span-2"
        >
          <Input id="contactEmail" type="email" {...register("contactEmail")} />
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

export function OperatingUnitDetailPage() {
  const { unitId = "" } = useParams();
  const { hasPermission } = useAuth();
  const { toast } = useToast();
  const client = useQueryClient();
  const [editOpen, setEditOpen] = useState(false);
  const [deactivateOpen, setDeactivateOpen] = useState(false);
  const [reason, setReason] = useState("");
  const query = useQuery({
    queryKey: ["operating-unit", unitId],
    queryFn: () =>
      apiRequest<OperatingUnit>(`/api/v1/network/operating-units/${unitId}`),
  });
  const deactivateMutation = useMutation({
    mutationFn: () =>
      apiRequest(`/api/v1/network/operating-units/${unitId}/deactivate`, {
        method: "POST",
        body: { reason },
      }),
    onSuccess: () => {
      void query.refetch();
      void client.invalidateQueries({ queryKey: ["operating-units"] });
      toast({ tone: "success", title: "Operating unit deactivated" });
      setDeactivateOpen(false);
      setReason("");
    },
  });
  if (query.isLoading) return <LoadingState label="Loading facility" />;
  if (query.error || !query.data)
    return (
      <ErrorState
        error={query.error ?? new Error("Facility not found")}
        retry={() => void query.refetch()}
      />
    );
  const unit = query.data;
  return (
    <>
      <PageHeader
        eyebrow="Network / Facility"
        title={`${unit.code ?? ""} · ${unit.name ?? "Operating unit"}`}
        description={titleCase(unit.unitType ?? "Facility")}
        actions={
          <>
            <StatusBadge status={unit.status} />
            {hasPermission("operating_unit.manage") ? (
              <Button onClick={() => setEditOpen(true)}>
                <Pencil aria-hidden className="h-4 w-4" /> Edit facility
              </Button>
            ) : null}
            {hasPermission("operating_unit.manage") &&
            unit.status !== "INACTIVE" ? (
              <Button variant="danger" onClick={() => setDeactivateOpen(true)}>
                Deactivate
              </Button>
            ) : null}
          </>
        }
      />
      <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_380px]">
        <Panel>
          <div className="grid gap-px bg-border sm:grid-cols-2">
            <Detail label="Parent">
              {unit.parent
                ? `${unit.parent.code} · ${unit.parent.name}`
                : "Top-level facility"}
            </Detail>
            <Detail label="Region">{unit.regionCode || "—"}</Detail>
            <Detail label="Address">
              {[
                unit.address?.line1,
                unit.address?.line2,
                unit.address?.city,
                unit.address?.state,
                unit.address?.pincode,
              ]
                .filter(Boolean)
                .join(", ")}
            </Detail>
            <Detail label="Contact">
              {unit.contact?.name || "—"}
              <span className="block text-xs text-slate-500">
                {unit.contact?.phone} {unit.contact?.email}
              </span>
            </Detail>
            <Detail label="Effective from">{unit.effectiveFrom || "—"}</Detail>
            <Detail label="Configuration version">v{unit.version ?? 1}</Detail>
          </div>
        </Panel>
        <Panel>
          <div className="p-4">
            <h2 className="text-sm font-semibold">Capabilities</h2>
            <div className="mt-3 flex flex-wrap gap-1.5">
              {unit.capabilities?.length ? (
                unit.capabilities.map((capability, index) => (
                  <Badge
                    key={`${capability.capability ?? "cap"}-${index}`}
                    tone="primary"
                  >
                    {titleCase(capability.capability ?? "Capability")}
                  </Badge>
                ))
              ) : (
                <p className="text-sm text-slate-500">
                  No capabilities configured.
                </p>
              )}
            </div>
          </div>
        </Panel>
        <Panel className="xl:col-span-2">
          <div className="grid gap-px bg-border md:grid-cols-2">
            <Detail label="Operating hours">
              {unit.operatingHours && Object.keys(unit.operatingHours).length
                ? Object.entries(unit.operatingHours).map(([day, hours]) => (
                    <span key={day} className="mr-3 inline-block">
                      <strong className="capitalize">{day}</strong>{" "}
                      {formatHours(hours)}
                    </span>
                  ))
                : "Not configured"}
            </Detail>
            <Detail label="Users & children">
              View linked facilities in the hierarchy view. An administrator can
              review staff assignments under Users.
            </Detail>
          </div>
        </Panel>
      </div>
      <EditUnitDialog unit={unit} open={editOpen} onOpenChange={setEditOpen} />
      <ConfirmAction
        open={deactivateOpen}
        onOpenChange={setDeactivateOpen}
        title="Deactivate operating unit?"
        description="The facility is retained for historical shipments. Deactivation is refused while active routes, service areas, or child units still reference it."
        confirmLabel="Deactivate facility"
        loading={deactivateMutation.isPending}
        onConfirm={() => deactivateMutation.mutate()}
      >
        <Field
          label="Reason"
          htmlFor="deactivateUnitReason"
          required
          hint="At least five characters; retained in operational context."
        >
          <Textarea
            id="deactivateUnitReason"
            value={reason}
            onChange={(event) => setReason(event.target.value)}
          />
        </Field>
        {reason.length > 0 && reason.length < 5 ? (
          <p role="alert" className="mt-2 text-xs text-danger">
            Enter at least five characters.
          </p>
        ) : null}
        {deactivateMutation.error ? (
          <p role="alert" className="mt-2 text-sm text-danger">
            {deactivateMutation.error.message}
          </p>
        ) : null}
      </ConfirmAction>
    </>
  );
}

const editUnitSchema = z.object({
  name: z.string().min(2).max(160),
  parentUnitId: z.string().optional(),
  addressLine1: z.string().min(3).max(200),
  pincode: z.string().regex(/^[1-9][0-9]{5}$/),
  contactName: z.string().optional(),
  contactPhone: z.string().optional(),
  contactEmail: z.union([z.email(), z.literal("")]).optional(),
  status: z.enum(["ACTIVE", "SUSPENDED"]),
});
type EditUnitValues = z.infer<typeof editUnitSchema>;

function EditUnitDialog({
  unit,
  open,
  onOpenChange,
}: {
  unit: OperatingUnit;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const client = useQueryClient();
  const { toast } = useToast();
  const parents = useQuery({
    queryKey: ["operating-units", "picker"],
    queryFn: () =>
      apiRequest<OperatingUnitListResponse>(
        "/api/v1/network/operating-units?limit=100",
      ),
    enabled: open,
  });
  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<EditUnitValues>({
    resolver: zodResolver(editUnitSchema),
    values: {
      name: unit.name ?? "",
      parentUnitId: unit.parent?.id ?? "",
      addressLine1: unit.address?.line1 ?? "",
      pincode: unit.address?.pincode ?? "",
      contactName: unit.contact?.name ?? "",
      contactPhone: unit.contact?.phone ?? "",
      contactEmail: unit.contact?.email ?? "",
      status: unit.status === "SUSPENDED" ? "SUSPENDED" : "ACTIVE",
    },
  });
  const mutation = useMutation({
    mutationFn: (values: EditUnitValues) =>
      apiRequest<OperatingUnitSummary>(
        `/api/v1/network/operating-units/${unit.id}`,
        {
          method: "PATCH",
          body: {
            expectedVersion: unit.version ?? 1,
            ...values,
            parentUnitId: values.parentUnitId ?? "",
            contactName: values.contactName || undefined,
            contactPhone: values.contactPhone || undefined,
            contactEmail: values.contactEmail || undefined,
          },
        },
      ),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ["operating-unit", unit.id] });
      void client.invalidateQueries({ queryKey: ["operating-units"] });
      toast({ tone: "success", title: "Operating unit updated" });
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Edit operating unit"
      description={`${unit.code} and ${titleCase(unit.unitType ?? "facility")} are immutable. Version ${unit.version ?? 1} will be checked before saving.`}
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
            Save facility
          </Button>
        </>
      }
    >
      <form
        className="grid gap-4 sm:grid-cols-2"
        onSubmit={(event) => event.preventDefault()}
      >
        <Field
          label="Name"
          htmlFor="editUnitName"
          required
          error={errors.name?.message}
        >
          <Input id="editUnitName" {...register("name")} />
        </Field>
        <Field label="Status" htmlFor="editUnitStatus">
          <Select id="editUnitStatus" {...register("status")}>
            <option>ACTIVE</option>
            <option>SUSPENDED</option>
          </Select>
        </Field>
        <Field label="Parent facility" htmlFor="editUnitParent">
          <Select id="editUnitParent" {...register("parentUnitId")}>
            <option value="">No parent</option>
            {parents.data?.data
              ?.filter((parent) => parent.id !== unit.id)
              .map((parent) => (
                <option key={parent.id} value={parent.id}>
                  {parent.code} — {parent.name}
                </option>
              ))}
          </Select>
        </Field>
        <Field
          label="Postal code"
          htmlFor="editUnitPincode"
          required
          error={errors.pincode?.message}
        >
          <Input
            id="editUnitPincode"
            inputMode="numeric"
            maxLength={6}
            {...register("pincode")}
          />
        </Field>
        <Field
          label="Address line 1"
          htmlFor="editUnitAddress"
          required
          error={errors.addressLine1?.message}
          className="sm:col-span-2"
        >
          <Input id="editUnitAddress" {...register("addressLine1")} />
        </Field>
        <Field label="Contact name" htmlFor="editUnitContact">
          <Input id="editUnitContact" {...register("contactName")} />
        </Field>
        <Field label="Contact phone" htmlFor="editUnitPhone">
          <Input id="editUnitPhone" {...register("contactPhone")} />
        </Field>
        <Field
          label="Contact email"
          htmlFor="editUnitEmail"
          error={errors.contactEmail?.message}
          className="sm:col-span-2"
        >
          <Input
            id="editUnitEmail"
            type="email"
            {...register("contactEmail")}
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

function formatHours(value: unknown) {
  if (!value || typeof value !== "object") return "—";
  const record = value as Record<string, unknown>;
  return [record.open, record.close].filter(Boolean).join("–") || "Closed";
}

export function FranchisesPage() {
  const { hasPermission } = useAuth();
  const [page, setPage] = useState(1);
  const [status, setStatus] = useState("");
  const [category, setCategory] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  const query = useQuery({
    queryKey: ["franchises", { page, status, category }],
    queryFn: () =>
      apiRequest<FranchisePage>(
        `/api/v1/network/franchises${queryString({ page, limit: 25, status, category })}`,
      ),
  });
  const rows = query.data?.data ?? [];
  return (
    <>
      <PageHeader
        eyebrow="Network"
        title="Franchises"
        description="Manage franchise operators, their linked branches, categories, and account status."
        actions={
          hasPermission("franchise.manage") ? (
            <Button variant="primary" onClick={() => setCreateOpen(true)}>
              <Plus aria-hidden className="h-4 w-4" /> Add franchise
            </Button>
          ) : undefined
        }
      />
      <Panel>
        <div className="flex flex-wrap gap-3 border-b p-4">
          <Select
            value={status}
            onChange={(event) => setStatus(event.target.value)}
            className="w-48"
          >
            <option value="">All statuses</option>
            <option>ONBOARDING</option>
            <option>ACTIVE</option>
            <option>SUSPENDED</option>
            <option>TERMINATED</option>
          </Select>
          <Select
            value={category}
            onChange={(event) => setCategory(event.target.value)}
            className="w-48"
          >
            <option value="">All categories</option>
            <option>PLATINUM</option>
            <option>GOLD</option>
            <option>SILVER</option>
            <option>STANDARD</option>
          </Select>
        </div>
        {query.isLoading ? (
          <LoadingState label="Loading franchises" />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : rows.length === 0 ? (
          <EmptyState
            icon={Users}
            title="No franchises found"
            description="No franchise agreements match the selected filters."
          />
        ) : (
          <>
            <DataTable label="Franchises">
              <thead>
                <tr>
                  <TableHead>Code & name</TableHead>
                  <TableHead>Category</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Version</TableHead>
                </tr>
              </thead>
              <tbody>
                {rows.map((franchise) => (
                  <tr key={franchise.id}>
                    <TableCell>
                      <strong className="text-primary">{franchise.code}</strong>
                      <span className="ml-2">{franchise.name}</span>
                    </TableCell>
                    <TableCell>
                      {titleCase(franchise.category ?? "Standard")}
                    </TableCell>
                    <TableCell>
                      <StatusBadge status={franchise.status} />
                    </TableCell>
                    <TableCell>v{franchise.version ?? 1}</TableCell>
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
      <CreateFranchiseDialog open={createOpen} onOpenChange={setCreateOpen} />
    </>
  );
}

const franchiseSchema = z.object({
  code: z.string().regex(/^[A-Z0-9][A-Z0-9_-]{1,31}$/),
  name: z.string().min(2).max(160),
  operatingUnitId: z.string().min(2),
  category: z.enum(["PLATINUM", "GOLD", "SILVER", "STANDARD"]),
  ownerName: z.string().min(2),
  ownerPhone: z.string().min(5),
  ownerEmail: z.union([z.email(), z.literal("")]).optional(),
  gstNumber: z.string().optional(),
  panNumber: z.string().optional(),
  status: z.enum(["ONBOARDING", "ACTIVE", "SUSPENDED", "TERMINATED"]),
});
type FranchiseValues = z.infer<typeof franchiseSchema>;

function CreateFranchiseDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const client = useQueryClient();
  const { toast } = useToast();
  const branches = useQuery({
    queryKey: ["operating-units", "franchise-picker"],
    queryFn: () =>
      apiRequest<OperatingUnitListResponse>(
        "/api/v1/network/operating-units?unitType=FRANCHISE_BRANCH&status=ACTIVE&limit=100",
      ),
    enabled: open,
  });
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<FranchiseValues>({
    resolver: zodResolver(franchiseSchema),
    defaultValues: { category: "STANDARD", status: "ONBOARDING" },
  });
  const mutation = useMutation({
    mutationFn: (values: FranchiseValues) =>
      apiRequest<FranchiseSummary>("/api/v1/network/franchises", {
        method: "POST",
        body: {
          ...values,
          code: values.code.toUpperCase(),
          ownerEmail: values.ownerEmail || undefined,
          gstNumber: values.gstNumber || undefined,
          panNumber: values.panNumber || undefined,
        },
      }),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ["franchises"] });
      toast({ tone: "success", title: "Franchise created" });
      reset();
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Create franchise"
      description="Link the franchise to an active franchise branch. Commission and settlement settings are managed separately."
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
            Create franchise
          </Button>
        </>
      }
    >
      <form
        className="grid gap-4 sm:grid-cols-2"
        onSubmit={(event) => event.preventDefault()}
      >
        <Field
          label="Code"
          htmlFor="franchiseCode"
          required
          error={errors.code?.message}
        >
          <Input
            id="franchiseCode"
            className="uppercase"
            {...register("code")}
          />
        </Field>
        <Field
          label="Name"
          htmlFor="franchiseName"
          required
          error={errors.name?.message}
        >
          <Input id="franchiseName" {...register("name")} />
        </Field>
        <Field
          label="Franchise branch"
          htmlFor="franchiseUnit"
          required
          error={errors.operatingUnitId?.message}
        >
          <Select id="franchiseUnit" {...register("operatingUnitId")}>
            <option value="">Choose branch</option>
            {branches.data?.data?.map((branch) => (
              <option key={branch.id} value={branch.id}>
                {branch.code} — {branch.name}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Category" htmlFor="franchiseCategory">
          <Select id="franchiseCategory" {...register("category")}>
            <option>PLATINUM</option>
            <option>GOLD</option>
            <option>SILVER</option>
            <option>STANDARD</option>
          </Select>
        </Field>
        <Field
          label="Owner name"
          htmlFor="franchiseOwner"
          required
          error={errors.ownerName?.message}
        >
          <Input id="franchiseOwner" {...register("ownerName")} />
        </Field>
        <Field
          label="Owner phone"
          htmlFor="franchisePhone"
          required
          error={errors.ownerPhone?.message}
        >
          <Input id="franchisePhone" {...register("ownerPhone")} />
        </Field>
        <Field
          label="Owner email"
          htmlFor="franchiseEmail"
          error={errors.ownerEmail?.message}
        >
          <Input id="franchiseEmail" type="email" {...register("ownerEmail")} />
        </Field>
        <Field label="Initial status" htmlFor="franchiseStatus">
          <Select id="franchiseStatus" {...register("status")}>
            <option>ONBOARDING</option>
            <option>ACTIVE</option>
            <option>SUSPENDED</option>
            <option>TERMINATED</option>
          </Select>
        </Field>
        <Field label="Tax identification number (TIN)" htmlFor="franchiseGst">
          <Input
            id="franchiseGst"
            className="uppercase"
            {...register("gstNumber")}
          />
        </Field>
        <Field label="CAC / RC number" htmlFor="franchisePan">
          <Input
            id="franchisePan"
            className="uppercase"
            {...register("panNumber")}
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

function Detail({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="bg-white p-4">
      <p className="text-xs font-medium uppercase tracking-wide text-slate-500">
        {label}
      </p>
      <div className="mt-1.5 text-sm text-slate-900">{children}</div>
    </div>
  );
}
