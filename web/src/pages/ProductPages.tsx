import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Box, Check, PackagePlus, Pencil, Plus, SearchX } from "lucide-react";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { Link, useParams } from "react-router-dom";
import { z } from "zod";
import {
  apiRequest,
  queryString,
  type CourierService,
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
  Input,
  LoadingState,
  PageHeader,
  Pagination,
  Panel,
  Select,
  StatusBadge,
  TableCell,
  TableHead,
} from "../components/ui";
import { formatMoney, formatWeight, titleCase } from "../lib/utils";

const productSchema = z.object({
  code: z.string().regex(/^[A-Z0-9][A-Z0-9_-]{1,31}$/),
  name: z.string().min(2),
  description: z.string().optional(),
  mode: z.enum(["AIR", "SURFACE", "RAIL", "LOCAL"]),
  minWeightGrams: z.number().int().min(1),
  maxWeightGrams: z.number().int().min(1),
  maxLengthMm: z.number().int().optional(),
  maxWidthMm: z.number().int().optional(),
  maxHeightMm: z.number().int().optional(),
  volumetricDivisor: z.number().int().min(1),
  weightRoundingGrams: z.number().int().min(1),
  slaTransitHours: z.number().int().min(1),
  cutoffTime: z.string().optional(),
  codAllowed: z.boolean(),
  insuranceAllowed: z.boolean(),
});
type ProductValues = z.infer<typeof productSchema>;

export function ProductsPage() {
  const { hasPermission } = useAuth();
  const [page, setPage] = useState(1);
  const [status, setStatus] = useState("");
  const [mode, setMode] = useState("");
  const [open, setOpen] = useState(false);
  const query = useQuery({
    queryKey: ["courier-services", { page, status, mode }],
    queryFn: () =>
      apiRequest<ServiceListResponse>(
        `/api/v1/courier-services${queryString({ page, limit: 25, status, mode })}`,
      ),
    placeholderData: (previous) => previous,
    staleTime: 5 * 60_000,
  });
  const rows = query.data?.data ?? [];
  return (
    <>
      <PageHeader
        eyebrow="Commercial"
        title="Courier products"
        description="Configure weight envelopes, service mode, COD and insurance eligibility, SLA, and cut-off."
        actions={
          hasPermission("courier_service.manage") ? (
            <Button variant="primary" onClick={() => setOpen(true)}>
              <Plus aria-hidden className="h-4 w-4" /> Add product
            </Button>
          ) : undefined
        }
      />
      <Panel>
        <div className="flex gap-3 border-b p-4">
          <Select
            className="w-44"
            value={status}
            onChange={(event) => setStatus(event.target.value)}
          >
            <option value="">All statuses</option>
            <option>ACTIVE</option>
            <option>INACTIVE</option>
          </Select>
          <Select
            className="w-44"
            value={mode}
            onChange={(event) => setMode(event.target.value)}
          >
            <option value="">All modes</option>
            <option>AIR</option>
            <option>SURFACE</option>
            <option>RAIL</option>
            <option>LOCAL</option>
          </Select>
        </div>
        {query.isLoading ? (
          <LoadingState label="Loading products" />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : rows.length === 0 ? (
          <EmptyState
            icon={SearchX}
            title="No courier products found"
            description="Adjust the filters or create the first product."
          />
        ) : (
          <>
            <DataTable label="Courier products">
              <thead>
                <tr>
                  <TableHead>Code & product</TableHead>
                  <TableHead>Mode</TableHead>
                  <TableHead>Weight envelope</TableHead>
                  <TableHead>SLA</TableHead>
                  <TableHead>Options</TableHead>
                  <TableHead>Status</TableHead>
                </tr>
              </thead>
              <tbody>
                {rows.map((service) => (
                  <tr key={service.id} className="hover:bg-slate-50">
                    <TableCell>
                      <Link
                        to={`/products/${service.id}`}
                        className="font-semibold text-primary hover:underline"
                      >
                        {service.code}
                      </Link>
                      <span className="ml-2">{service.name}</span>
                    </TableCell>
                    <TableCell>
                      <Badge>{titleCase(service.mode ?? "Mode")}</Badge>
                    </TableCell>
                    <TableCell>
                      {formatWeight(service.minWeightGrams)} –{" "}
                      {formatWeight(service.maxWeightGrams)}
                    </TableCell>
                    <TableCell>{service.slaTransitHours ?? 0} hours</TableCell>
                    <TableCell>
                      <div className="flex gap-1">
                        {service.codAllowed ? (
                          <Badge tone="info">COD</Badge>
                        ) : null}
                        {service.insuranceAllowed ? (
                          <Badge tone="primary">Insurance</Badge>
                        ) : null}
                      </div>
                    </TableCell>
                    <TableCell>
                      <StatusBadge status={service.status} />
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
      <CreateProductDialog open={open} onOpenChange={setOpen} />
    </>
  );
}

function CreateProductDialog({
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
  } = useForm<ProductValues>({
    resolver: zodResolver(productSchema),
    defaultValues: {
      mode: "SURFACE",
      minWeightGrams: 1,
      maxWeightGrams: 30000,
      volumetricDivisor: 5000,
      weightRoundingGrams: 500,
      slaTransitHours: 48,
      codAllowed: false,
      insuranceAllowed: false,
    },
  });
  const mutation = useMutation({
    mutationFn: (values: ProductValues) =>
      apiRequest<CourierService>("/api/v1/courier-services", {
        method: "POST",
        body: {
          ...values,
          code: values.code.toUpperCase(),
          description: values.description || undefined,
          cutoffTime: values.cutoffTime || undefined,
        },
      }),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ["courier-services"] });
      toast({ tone: "success", title: "Courier product created" });
      reset();
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Create courier product"
      description="Product code is immutable. All behavioural differences remain server configuration."
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
            Create product
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
          htmlFor="productCode"
          required
          error={errors.code?.message}
        >
          <Input id="productCode" className="uppercase" {...register("code")} />
        </Field>
        <Field
          label="Name"
          htmlFor="productName"
          required
          error={errors.name?.message}
        >
          <Input id="productName" {...register("name")} />
        </Field>
        <Field
          label="Description"
          htmlFor="productDescription"
          className="sm:col-span-2"
        >
          <Input id="productDescription" {...register("description")} />
        </Field>
        <Field label="Mode" htmlFor="productMode">
          <Select id="productMode" {...register("mode")}>
            <option>AIR</option>
            <option>SURFACE</option>
            <option>RAIL</option>
            <option>LOCAL</option>
          </Select>
        </Field>
        <Field
          label="SLA transit hours"
          htmlFor="slaTransitHours"
          required
          error={errors.slaTransitHours?.message}
        >
          <Input
            id="slaTransitHours"
            type="number"
            {...register("slaTransitHours", { valueAsNumber: true })}
          />
        </Field>
        <Field label="Minimum weight (g)" htmlFor="minWeightGrams">
          <Input
            id="minWeightGrams"
            type="number"
            {...register("minWeightGrams", { valueAsNumber: true })}
          />
        </Field>
        <Field label="Maximum weight (g)" htmlFor="maxWeightGrams">
          <Input
            id="maxWeightGrams"
            type="number"
            {...register("maxWeightGrams", { valueAsNumber: true })}
          />
        </Field>
        <Field label="Volumetric divisor" htmlFor="volumetricDivisor">
          <Input
            id="volumetricDivisor"
            type="number"
            {...register("volumetricDivisor", { valueAsNumber: true })}
          />
        </Field>
        <Field label="Weight rounding (g)" htmlFor="weightRoundingGrams">
          <Input
            id="weightRoundingGrams"
            type="number"
            {...register("weightRoundingGrams", { valueAsNumber: true })}
          />
        </Field>
        <Field label="Maximum length (mm)" htmlFor="maxLengthMm">
          <Input
            id="maxLengthMm"
            type="number"
            {...register("maxLengthMm", {
              setValueAs: (value) => (value === "" ? undefined : Number(value)),
            })}
          />
        </Field>
        <Field label="Maximum width (mm)" htmlFor="maxWidthMm">
          <Input
            id="maxWidthMm"
            type="number"
            {...register("maxWidthMm", {
              setValueAs: (value) => (value === "" ? undefined : Number(value)),
            })}
          />
        </Field>
        <Field label="Maximum height (mm)" htmlFor="maxHeightMm">
          <Input
            id="maxHeightMm"
            type="number"
            {...register("maxHeightMm", {
              setValueAs: (value) => (value === "" ? undefined : Number(value)),
            })}
          />
        </Field>
        <Field label="Cut-off time" htmlFor="cutoffTime">
          <Input id="cutoffTime" type="time" {...register("cutoffTime")} />
        </Field>
        <div className="flex gap-6 sm:col-span-2">
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              className="h-4 w-4 accent-emerald-800"
              {...register("codAllowed")}
            />{" "}
            COD allowed
          </label>
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              className="h-4 w-4 accent-emerald-800"
              {...register("insuranceAllowed")}
            />{" "}
            Insurance allowed
          </label>
        </div>
        {mutation.error ? (
          <p role="alert" className="sm:col-span-2 text-sm text-danger">
            {mutation.error.message}
          </p>
        ) : null}
      </form>
    </Dialog>
  );
}

export function ProductDetailPage() {
  const { serviceId = "" } = useParams();
  const { hasPermission } = useAuth();
  const [editOpen, setEditOpen] = useState(false);
  const query = useQuery({
    queryKey: ["courier-service", serviceId],
    queryFn: () =>
      apiRequest<CourierService>(`/api/v1/courier-services/${serviceId}`),
  });
  if (query.isLoading) return <LoadingState label="Loading courier product" />;
  if (query.error || !query.data)
    return (
      <ErrorState
        error={query.error ?? new Error("Courier product not found")}
        retry={() => void query.refetch()}
      />
    );
  const service = query.data;
  return (
    <>
      <PageHeader
        eyebrow="Commercial / Courier products"
        title={`${service.code ?? ""} · ${service.name ?? "Product"}`}
        description={service.description}
        actions={
          <>
            <StatusBadge status={service.status} />
            {hasPermission("courier_service.manage") ? (
              <Button onClick={() => setEditOpen(true)}>
                <Pencil aria-hidden className="h-4 w-4" /> Edit product
              </Button>
            ) : null}
          </>
        }
      />
      <div className="grid gap-5 xl:grid-cols-3">
        <Section title="Basics" icon={Box}>
          <Item label="Mode" value={titleCase(service.mode ?? "—")} />
          <Item label="SLA" value={`${service.slaTransitHours ?? 0} hours`} />
          <Item label="Cut-off" value={service.cutoffTime ?? "—"} />
        </Section>
        <Section title="Weight & dimensions" icon={PackagePlus}>
          <Item
            label="Weight range"
            value={`${formatWeight(service.minWeightGrams)} – ${formatWeight(service.maxWeightGrams)}`}
          />
          <Item
            label="Limits"
            value={`${service.maxLengthMm ?? "—"} × ${service.maxWidthMm ?? "—"} × ${service.maxHeightMm ?? "—"} mm`}
          />
          <Item
            label="Rounding"
            value={`${service.weightRoundingGrams ?? "—"} g`}
          />
          <Item
            label="Volumetric divisor"
            value={String(service.volumetricDivisor ?? "—")}
          />
        </Section>
        <Section title="Availability" icon={Check}>
          <Item
            label="COD"
            value={
              service.codAllowed
                ? `Allowed up to ${formatMoney(service.maxCodAmountMinor)}`
                : "Not allowed"
            }
          />
          <Item
            label="Insurance"
            value={
              service.insuranceAllowed
                ? `Allowed up to ${formatMoney(service.maxDeclaredValueMinor)}`
                : "Not allowed"
            }
          />
          <Item
            label="Effective period"
            value={`${service.effectiveFrom ?? "—"} – ${service.effectiveTo ?? "Open"}`}
          />
        </Section>
      </div>
      <EditProductDialog
        service={service}
        open={editOpen}
        onOpenChange={setEditOpen}
      />
    </>
  );
}

const editProductSchema = z.object({
  name: z.string().min(2).max(160),
  description: z.string().optional(),
  mode: z.enum(["AIR", "SURFACE", "RAIL", "LOCAL"]),
  maxWeightGrams: z.number().int().min(1),
  slaTransitHours: z.number().int().min(1),
  codAllowed: z.boolean(),
  status: z.enum(["ACTIVE", "INACTIVE"]),
});
type EditProductValues = z.infer<typeof editProductSchema>;

function EditProductDialog({
  service,
  open,
  onOpenChange,
}: {
  service: CourierService;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const client = useQueryClient();
  const { toast } = useToast();
  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<EditProductValues>({
    resolver: zodResolver(editProductSchema),
    values: {
      name: service.name ?? "",
      description: service.description ?? "",
      mode: service.mode ?? "SURFACE",
      maxWeightGrams: service.maxWeightGrams ?? 1,
      slaTransitHours: service.slaTransitHours ?? 24,
      codAllowed: Boolean(service.codAllowed),
      status: service.status ?? "ACTIVE",
    },
  });
  const mutation = useMutation({
    mutationFn: (values: EditProductValues) =>
      apiRequest<CourierService>(`/api/v1/courier-services/${service.id}`, {
        method: "PATCH",
        body: {
          expectedVersion: service.version ?? 1,
          ...values,
          description: values.description || undefined,
        },
      }),
    onSuccess: (updated) => {
      client.setQueryData(["courier-service", service.id], updated);
      void client.invalidateQueries({ queryKey: ["courier-services"] });
      toast({ tone: "success", title: "Courier product updated" });
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Edit courier product"
      description={`${service.code} is immutable. The server will check configuration version ${service.version ?? 1}.`}
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
            Save product
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
          htmlFor="editProductName"
          required
          error={errors.name?.message}
          className="sm:col-span-2"
        >
          <Input id="editProductName" {...register("name")} />
        </Field>
        <Field
          label="Description"
          htmlFor="editProductDescription"
          className="sm:col-span-2"
        >
          <Input id="editProductDescription" {...register("description")} />
        </Field>
        <Field label="Mode" htmlFor="editProductMode">
          <Select id="editProductMode" {...register("mode")}>
            <option>AIR</option>
            <option>SURFACE</option>
            <option>RAIL</option>
            <option>LOCAL</option>
          </Select>
        </Field>
        <Field label="Status" htmlFor="editProductStatus">
          <Select id="editProductStatus" {...register("status")}>
            <option>ACTIVE</option>
            <option>INACTIVE</option>
          </Select>
        </Field>
        <Field
          label="Maximum weight (g)"
          htmlFor="editProductWeight"
          required
          error={errors.maxWeightGrams?.message}
        >
          <Input
            id="editProductWeight"
            type="number"
            {...register("maxWeightGrams", { valueAsNumber: true })}
          />
        </Field>
        <Field
          label="SLA transit hours"
          htmlFor="editProductSla"
          required
          error={errors.slaTransitHours?.message}
        >
          <Input
            id="editProductSla"
            type="number"
            {...register("slaTransitHours", { valueAsNumber: true })}
          />
        </Field>
        <label className="flex items-center gap-2 text-sm sm:col-span-2">
          <input
            type="checkbox"
            className="h-4 w-4 accent-emerald-800"
            {...register("codAllowed")}
          />{" "}
          COD allowed
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

function Section({
  title,
  icon: Icon,
  children,
}: {
  title: string;
  icon: typeof Box;
  children: React.ReactNode;
}) {
  return (
    <Panel>
      <div className="flex items-center gap-2 border-b p-4">
        <Icon aria-hidden className="h-4 w-4 text-primary" />
        <h2 className="text-sm font-semibold">{title}</h2>
      </div>
      <dl className="divide-y divide-border">{children}</dl>
    </Panel>
  );
}
function Item({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-start justify-between gap-4 px-4 py-3">
      <dt className="text-xs text-slate-500">{label}</dt>
      <dd className="text-right text-sm font-medium">{value}</dd>
    </div>
  );
}
