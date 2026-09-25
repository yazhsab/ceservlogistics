import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Banknote,
  Calculator,
  CheckCircle2,
  CircleHelp,
  FilePlus2,
  Plus,
  SearchX,
} from "lucide-react";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { Link, useParams } from "react-router-dom";
import { z } from "zod";
import {
  apiRequest,
  queryString,
  type CustomerSummary,
  type OffsetPageOf,
  type Quote,
  type QuoteRequest,
  type RateCardVersion,
  type ServiceListResponse,
} from "../api/client";
import { useAuth } from "../auth/AuthProvider";
import type { components } from "../api/schema";
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
import {
  cmToMm,
  formatMoney,
  formatWeight,
  kgToGrams,
  titleCase,
  toMinorUnits,
} from "../lib/utils";

type RecordPage = OffsetPageOf<Record<string, unknown>>;
type WeightPriceRecord = Record<string, unknown> & {
  priceSource: "DOMESTIC" | "RATE_CARD";
};
const simulatorSchema = z.object({
  originPincode: z.string().regex(/^[1-9][0-9]{5}$/),
  destinationPincode: z.string().regex(/^[1-9][0-9]{5}$/),
  destinationCity: z.string().min(1, "Enter the destination city."),
  customerId: z.string().optional(),
  serviceCode: z.string().min(1),
  paymentMode: z.enum(["PREPAID", "COD", "CREDIT", "TO_PAY"]),
  actualWeightKg: z.number().min(0.001).multipleOf(0.001),
  lengthCm: z.number().min(0.1).multipleOf(0.1).optional(),
  widthCm: z.number().min(0.1).multipleOf(0.1).optional(),
  heightCm: z.number().min(0.1).multipleOf(0.1).optional(),
  codAmount: z.string().optional(),
  declaredValue: z.string().optional(),
  insuranceRequired: z.boolean(),
});
type SimulatorValues = z.infer<typeof simulatorSchema>;

export function PricingSimulatorPage() {
  const [quote, setQuote] = useState<Quote>();
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
    watch,
    formState: { errors },
  } = useForm<SimulatorValues>({
    resolver: zodResolver(simulatorSchema),
    defaultValues: {
      paymentMode: "PREPAID",
      actualWeightKg: 0.5,
      insuranceRequired: false,
    },
  });
  const paymentMode = watch("paymentMode");
  const mutation = useMutation({
    mutationFn: (values: SimulatorValues) => {
      const request: QuoteRequest = {
        originPincode: values.originPincode,
        destinationPincode: values.destinationPincode,
        destinationCity: values.destinationCity,
        serviceCode: values.serviceCode,
        paymentMode: values.paymentMode,
        packages: [
          {
            actualWeightGrams: kgToGrams(values.actualWeightKg) ?? 0,
            ...(values.lengthCm ? { lengthMm: cmToMm(values.lengthCm) } : {}),
            ...(values.widthCm ? { widthMm: cmToMm(values.widthCm) } : {}),
            ...(values.heightCm ? { heightMm: cmToMm(values.heightCm) } : {}),
          },
        ],
        ...(values.customerId ? { customerId: values.customerId } : {}),
        ...(toMinorUnits(values.codAmount) !== undefined
          ? { codAmountMinor: toMinorUnits(values.codAmount) }
          : {}),
        ...(toMinorUnits(values.declaredValue) !== undefined
          ? { declaredValueMinor: toMinorUnits(values.declaredValue) }
          : {}),
        insuranceRequired: values.insuranceRequired,
      };
      return apiRequest<Quote>("/api/v1/pricing/quote", {
        method: "POST",
        body: request,
      });
    },
    onSuccess: setQuote,
  });
  return (
    <>
      <PageHeader
        eyebrow="Commercial / Pricing"
        title="Pricing simulator"
        description="Run the authoritative pricing engine and see every weight decision, charge, discount, and tax explanation."
      />
      <div className="grid gap-5 xl:grid-cols-[430px_minmax(0,1fr)]">
        <Panel className="h-fit">
          <PanelHeader
            title="Shipment inputs"
            description="Measure in centimetres. Dimensional weight (kg) = length × width × height ÷ 5,000. No totals are calculated in the browser."
          />
          <form
            className="grid gap-4 p-4 sm:grid-cols-2 xl:grid-cols-1"
            onSubmit={(event) =>
              void handleSubmit((values) => mutation.mutate(values))(event)
            }
          >
            <div className="grid grid-cols-2 gap-3">
              <Field
                label="Origin postal code"
                htmlFor="priceOrigin"
                required
                error={errors.originPincode?.message}
              >
                <Input
                  id="priceOrigin"
                  inputMode="numeric"
                  maxLength={6}
                  autoFocus
                  {...register("originPincode")}
                />
              </Field>
              <Field
                label="Destination postal code"
                htmlFor="priceDestination"
                required
                error={errors.destinationPincode?.message}
              >
                <Input
                  id="priceDestination"
                  inputMode="numeric"
                  maxLength={6}
                  {...register("destinationPincode")}
                />
              </Field>
              <Field
                label="Destination city"
                htmlFor="simulator-destination-city"
                required
                error={errors.destinationCity?.message}
                hint="An exact city match applies the configured 2026 extended or remote-area charge."
              >
                <Input
                  id="simulator-destination-city"
                  {...register("destinationCity")}
                />
              </Field>
            </div>
            <Field
              label="Courier product"
              htmlFor="priceService"
              required
              error={errors.serviceCode?.message}
            >
              <Select id="priceService" {...register("serviceCode")}>
                <option value="">Choose product</option>
                {services.data?.data?.map((service) => (
                  <option key={service.id} value={service.code}>
                    {service.code} — {service.name}
                  </option>
                ))}
              </Select>
            </Field>
            <div className="grid grid-cols-2 gap-3">
              <Field label="Payment" htmlFor="paymentMode">
                <Select id="paymentMode" {...register("paymentMode")}>
                  <option>PREPAID</option>
                  <option>COD</option>
                  <option>CREDIT</option>
                  <option>TO_PAY</option>
                </Select>
              </Field>
              <Field label="Customer ID" htmlFor="priceCustomer">
                <Input
                  id="priceCustomer"
                  placeholder="Optional"
                  {...register("customerId")}
                />
              </Field>
            </div>
            <Field
              label="Shipment weight (kg)"
              htmlFor="actualWeightKg"
              required
              error={errors.actualWeightKg?.message}
            >
              <Input
                id="actualWeightKg"
                type="number"
                min={0.001}
                step={0.001}
                {...register("actualWeightKg", { valueAsNumber: true })}
              />
            </Field>
            <div className="grid grid-cols-3 gap-2">
              <Field label="Length (cm)" htmlFor="lengthCm">
                <Input
                  id="lengthCm"
                  type="number"
                  min={0.1}
                  step={0.1}
                  {...register("lengthCm", {
                    setValueAs: (value) =>
                      value === "" ? undefined : Number(value),
                  })}
                />
              </Field>
              <Field label="Width (cm)" htmlFor="widthCm">
                <Input
                  id="widthCm"
                  type="number"
                  min={0.1}
                  step={0.1}
                  {...register("widthCm", {
                    setValueAs: (value) =>
                      value === "" ? undefined : Number(value),
                  })}
                />
              </Field>
              <Field label="Height (cm)" htmlFor="heightCm">
                <Input
                  id="heightCm"
                  type="number"
                  min={0.1}
                  step={0.1}
                  {...register("heightCm", {
                    setValueAs: (value) =>
                      value === "" ? undefined : Number(value),
                  })}
                />
              </Field>
            </div>
            <div className="grid grid-cols-2 gap-3">
              {paymentMode === "COD" ? (
                <Field label="COD amount (₦)" htmlFor="codAmount">
                  <Input
                    id="codAmount"
                    inputMode="decimal"
                    {...register("codAmount")}
                  />
                </Field>
              ) : null}
              <Field label="Declared value (₦)" htmlFor="declaredValue">
                <Input
                  id="declaredValue"
                  inputMode="decimal"
                  {...register("declaredValue")}
                />
              </Field>
            </div>
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                className="h-4 w-4 accent-emerald-800"
                {...register("insuranceRequired")}
              />{" "}
              Insurance required
            </label>
            {mutation.error ? (
              <InlineNotice tone="danger" title="Pricing unavailable">
                {mutation.error.message}
              </InlineNotice>
            ) : null}
            <Button
              type="submit"
              variant="primary"
              loading={mutation.isPending}
            >
              <Calculator aria-hidden className="h-4 w-4" /> Calculate price
            </Button>
          </form>
        </Panel>
        <Panel className="min-h-[520px]">
          {quote ? (
            <QuoteView quote={quote} />
          ) : (
            <EmptyState
              icon={Calculator}
              title="Ready to calculate"
              description="Enter a lane, product, weight, and payment mode to see the server’s full pricing explanation."
            />
          )}
        </Panel>
      </div>
    </>
  );
}

export function QuoteView({
  quote,
  compact = false,
}: {
  quote: Quote;
  compact?: boolean;
}) {
  return (
    <div>
      <div className="flex flex-wrap items-start justify-between gap-4 border-b p-5">
        <div>
          <Badge tone="success">
            <CheckCircle2 aria-hidden className="h-3 w-3" /> Authoritative quote
          </Badge>
          <h2 className="mt-2 text-lg font-semibold">
            {quote.rateCardCode} · Version {quote.rateCardVersion}
          </h2>
          <p className="mt-1 text-xs text-slate-500">
            {quote.originZoneCode} → {quote.destinationZoneCode} ·{" "}
            {quote.serviceCode}
          </p>
        </div>
        <div className="text-right">
          <p className="text-xs font-semibold uppercase tracking-wide text-slate-500">
            Final total
          </p>
          <p className="mt-1 text-3xl font-bold tracking-tight text-slate-950">
            {formatMoney(quote.totalMinor, quote.currency)}
          </p>
        </div>
      </div>
      <div className={compact ? "p-4" : "p-5"}>
        <div className="grid gap-3 sm:grid-cols-3">
          <Metric
            label="Actual weight"
            value={formatWeight(quote.weight?.actualWeightGrams)}
          />
          <Metric
            label="Volumetric weight"
            value={formatWeight(quote.weight?.volumetricWeightGrams)}
          />
          <Metric
            label="Chargeable weight"
            value={formatWeight(quote.weight?.chargeableWeightGrams)}
            emphasis
          />
        </div>
        {quote.weight?.explanation ? (
          <InlineNotice title="Why this weight was charged">
            {quote.weight.explanation}
          </InlineNotice>
        ) : null}
        <div className="mt-5 overflow-hidden rounded-md border">
          <table className="w-full text-sm" aria-label="Price breakdown">
            <thead>
              <tr>
                <TableHead>Charge</TableHead>
                <TableHead>Explanation</TableHead>
                <TableHead className="text-right">Amount</TableHead>
              </tr>
            </thead>
            <tbody>
              {quote.lineItems?.map((item, index) => (
                <tr key={`${item.code}-${index}`}>
                  <TableCell>
                    <span className="font-medium">{item.label}</span>
                    <span className="mt-0.5 block text-[10px] uppercase tracking-wide text-slate-400">
                      {item.kind}
                    </span>
                  </TableCell>
                  <TableCell className="max-w-xl text-xs text-slate-600">
                    {item.explanation || "—"}
                  </TableCell>
                  <TableCell
                    className={`text-right font-semibold ${Number(item.amountMinor) < 0 ? "text-success" : ""}`}
                  >
                    {formatMoney(item.amountMinor, quote.currency)}
                  </TableCell>
                </tr>
              ))}
            </tbody>
            <tfoot>
              <tr className="bg-slate-50">
                <td colSpan={2} className="px-4 py-3 text-sm font-semibold">
                  Total charged by pricing engine
                </td>
                <td className="px-4 py-3 text-right text-base font-bold">
                  {formatMoney(quote.totalMinor, quote.currency)}
                </td>
              </tr>
            </tfoot>
          </table>
        </div>
      </div>
    </div>
  );
}

function Metric({
  label,
  value,
  emphasis,
}: {
  label: string;
  value: string;
  emphasis?: boolean;
}) {
  return (
    <div
      className={`rounded-md border p-3 ${emphasis ? "border-emerald-200 bg-emerald-50" : "bg-slate-50"}`}
    >
      <p className="text-xs text-slate-500">{label}</p>
      <p className="mt-1 text-base font-semibold">{value}</p>
    </div>
  );
}

export function RateCardsPage() {
  const { hasPermission } = useAuth();
  const [page, setPage] = useState(1);
  const [scope, setScope] = useState("");
  const [open, setOpen] = useState(false);
  const query = useQuery({
    queryKey: ["rate-cards", { page, scope }],
    queryFn: () =>
      apiRequest<RecordPage>(
        `/api/v1/rate-cards${queryString({ page, limit: 25, scope })}`,
      ),
  });
  const rows = query.data?.data ?? [];
  return (
    <>
      <PageHeader
        eyebrow="Commercial / Pricing"
        title="Rate cards"
        description="Retail, business, and franchise pricing configurations with immutable published versions."
        actions={
          hasPermission("rate_card.manage") ? (
            <Button variant="primary" onClick={() => setOpen(true)}>
              <Plus aria-hidden className="h-4 w-4" /> Add rate card
            </Button>
          ) : undefined
        }
      />
      <Panel>
        <div className="border-b p-4">
          <Select
            value={scope}
            onChange={(event) => setScope(event.target.value)}
            className="w-48"
          >
            <option value="">All scopes</option>
            <option>RETAIL</option>
            <option>BUSINESS</option>
            <option>FRANCHISE</option>
          </Select>
        </div>
        {query.isLoading ? (
          <LoadingState label="Loading rate cards" />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : rows.length === 0 ? (
          <EmptyState
            icon={SearchX}
            title="No rate cards found"
            description="Create a rate card, then add a draft version and its lane prices."
          />
        ) : (
          <>
            <DataTable label="Rate cards">
              <thead>
                <tr>
                  <TableHead>Code & name</TableHead>
                  <TableHead>Scope</TableHead>
                  <TableHead>Association</TableHead>
                  <TableHead>Active version</TableHead>
                  <TableHead>Status</TableHead>
                </tr>
              </thead>
              <tbody>
                {rows.map((row, index) => (
                  <tr key={text(row, "id") || index}>
                    <TableCell>
                      <Link
                        to={`/pricing/rate-cards/${text(row, "id")}`}
                        className="font-semibold text-primary hover:underline"
                      >
                        {text(row, "code")}
                      </Link>
                      <span className="ml-2">{text(row, "name")}</span>
                    </TableCell>
                    <TableCell>
                      <Badge>{titleCase(text(row, "scope"))}</Badge>
                    </TableCell>
                    <TableCell>
                      {text(row, "customerName", "franchiseName") ||
                        (bool(row, "isDefault") ? "Default retail" : "—")}
                    </TableCell>
                    <TableCell>
                      {text(row, "activeVersion") || "No active version"}
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
      <CreateRateCardDialog open={open} onOpenChange={setOpen} />
    </>
  );
}

function CreateRateCardDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const schema = z
    .object({
      code: z.string().regex(/^[A-Z0-9][A-Z0-9_-]{1,31}$/),
      name: z.string().min(2),
      description: z.string().optional(),
      scope: z.enum(["RETAIL", "BUSINESS", "FRANCHISE"]),
      customerId: z.string().optional(),
      franchiseId: z.string().optional(),
      isDefault: z.boolean(),
    })
    .superRefine((value, context) => {
      if (value.scope === "BUSINESS" && !value.customerId)
        context.addIssue({
          code: "custom",
          path: ["customerId"],
          message: "Choose the customer receiving this rate card.",
        });
    });
  type Values = z.infer<typeof schema>;
  const client = useQueryClient();
  const { toast } = useToast();
  const [customerSearch, setCustomerSearch] = useState("");
  const [selectedCustomer, setSelectedCustomer] = useState<CustomerSummary>();
  const {
    register,
    handleSubmit,
    watch,
    setValue,
    reset,
    formState: { errors },
  } = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: { scope: "RETAIL", isDefault: false },
  });
  const scope = watch("scope");
  const customers = useQuery({
    queryKey: ["customers", "rate-card-lookup", customerSearch],
    queryFn: () =>
      apiRequest<OffsetPageOf<CustomerSummary>>(
        `/api/v1/customers${queryString({ search: customerSearch, status: "ACTIVE", limit: 8 })}`,
      ),
    enabled: scope === "BUSINESS" && customerSearch.trim().length >= 2,
    staleTime: 30_000,
  });
  const mutation = useMutation({
    mutationFn: (values: Values) =>
      apiRequest("/api/v1/rate-cards", {
        method: "POST",
        body: {
          code: values.code.toUpperCase(),
          name: values.name,
          description: values.description || undefined,
          scope: values.scope,
          customerId: values.customerId || undefined,
          franchiseId: values.franchiseId || undefined,
          isDefault: values.isDefault,
        },
      }),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ["rate-cards"] });
      toast({ tone: "success", title: "Rate card created" });
      reset();
      setCustomerSearch("");
      setSelectedCustomer(undefined);
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Create rate card"
      description="Create the commercial container, then add a draft version for prices."
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
            Create rate card
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
          htmlFor="cardCode"
          required
          error={errors.code?.message}
        >
          <Input id="cardCode" className="uppercase" {...register("code")} />
        </Field>
        <Field
          label="Name"
          htmlFor="cardName"
          required
          error={errors.name?.message}
        >
          <Input id="cardName" {...register("name")} />
        </Field>
        <Field label="Scope" htmlFor="cardScope">
          <Select id="cardScope" {...register("scope")}>
            <option>RETAIL</option>
            <option>BUSINESS</option>
            <option>FRANCHISE</option>
          </Select>
        </Field>
        {scope === "BUSINESS" ? (
          <Field
            label="Customer"
            htmlFor="cardCustomer"
            required
            error={errors.customerId?.message}
            className="relative"
          >
            <Input
              id="cardCustomer"
              value={customerSearch}
              placeholder="Search code, name, email or phone"
              autoComplete="off"
              onChange={(event) => {
                setCustomerSearch(event.target.value);
                setSelectedCustomer(undefined);
                setValue("customerId", "", { shouldValidate: true });
              }}
            />
            <input type="hidden" {...register("customerId")} />
            {selectedCustomer ? (
              <p className="mt-1 text-xs text-success">
                Selected: {selectedCustomer.code} · {selectedCustomer.name}
              </p>
            ) : customerSearch.trim().length >= 2 ? (
              <div className="absolute left-0 right-0 top-[68px] z-20 max-h-56 overflow-y-auto rounded-md border bg-white p-1 shadow-overlay">
                {customers.isLoading ? (
                  <p className="p-3 text-xs text-muted-foreground">
                    Searching…
                  </p>
                ) : customers.data?.data?.length ? (
                  customers.data.data.map((customer) => (
                    <button
                      key={customer.id}
                      type="button"
                      className="w-full rounded px-3 py-2 text-left text-sm hover:bg-muted"
                      onClick={() => {
                        setSelectedCustomer(customer);
                        setCustomerSearch(
                          `${customer.code} · ${customer.name}`,
                        );
                        setValue("customerId", customer.id, {
                          shouldDirty: true,
                          shouldValidate: true,
                        });
                      }}
                    >
                      <strong>{customer.name}</strong>
                      <span className="ml-2 text-xs text-muted-foreground">
                        {customer.code}
                      </span>
                    </button>
                  ))
                ) : (
                  <p className="p-3 text-xs text-muted-foreground">
                    No active customer found.
                  </p>
                )}
              </div>
            ) : null}
          </Field>
        ) : scope === "FRANCHISE" ? (
          <Field label="Franchise ID" htmlFor="cardFranchise">
            <Input id="cardFranchise" {...register("franchiseId")} />
          </Field>
        ) : (
          <label className="flex items-center gap-2 self-end pb-2 text-sm">
            <input
              type="checkbox"
              className="h-4 w-4 accent-emerald-800"
              {...register("isDefault")}
            />{" "}
            Default retail card
          </label>
        )}
        <Field
          label="Description"
          htmlFor="cardDescription"
          className="sm:col-span-2"
        >
          <Input id="cardDescription" {...register("description")} />
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

export function RateCardDetailPage() {
  const { cardId = "" } = useParams();
  const { hasPermission } = useAuth();
  const [page, setPage] = useState(1);
  const [createOpen, setCreateOpen] = useState(false);
  const query = useQuery({
    queryKey: ["rate-card-versions", cardId, page],
    queryFn: () =>
      apiRequest<RecordPage>(
        `/api/v1/rate-cards/${cardId}/versions${queryString({ page, limit: 25 })}`,
      ),
  });
  const rows = query.data?.data ?? [];
  return (
    <>
      <PageHeader
        eyebrow="Commercial / Rate cards"
        title="Rate card versions"
        description="Draft versions are editable. Activation atomically supersedes the current version and makes prices immutable."
        actions={
          hasPermission("rate_card.manage") ? (
            <Button variant="primary" onClick={() => setCreateOpen(true)}>
              <Plus aria-hidden className="h-4 w-4" /> New draft version
            </Button>
          ) : undefined
        }
      />
      <Panel>
        {query.isLoading ? (
          <LoadingState />
        ) : query.error ? (
          <ErrorState error={query.error} />
        ) : rows.length === 0 ? (
          <EmptyState
            icon={FilePlus2}
            title="No versions yet"
            description="Create a draft version to begin setting lane prices."
          />
        ) : (
          <>
            <DataTable label="Rate card versions">
              <thead>
                <tr>
                  <TableHead>Version</TableHead>
                  <TableHead>Effective from</TableHead>
                  <TableHead>Effective to</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Editing</TableHead>
                </tr>
              </thead>
              <tbody>
                {rows.map((row, index) => (
                  <tr key={text(row, "id") || index}>
                    <TableCell>
                      <Link
                        to={`/pricing/versions/${text(row, "id")}`}
                        className="font-semibold text-primary"
                      >
                        Version {text(row, "version")}
                      </Link>
                    </TableCell>
                    <TableCell>{text(row, "effectiveFrom")}</TableCell>
                    <TableCell>{text(row, "effectiveTo") || "Open"}</TableCell>
                    <TableCell>
                      <StatusBadge status={text(row, "status")} />
                    </TableCell>
                    <TableCell>
                      {bool(row, "editable") ? (
                        <Badge tone="success">Editable</Badge>
                      ) : (
                        <Badge>Immutable</Badge>
                      )}
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
      <CreateVersionDialog
        cardId={cardId}
        open={createOpen}
        onOpenChange={setCreateOpen}
      />
    </>
  );
}

const versionSchema = z.object({
  effectiveFrom: z.string().optional(),
  effectiveTo: z.string().optional(),
  notes: z.string().max(1000).optional(),
});
type VersionValues = z.infer<typeof versionSchema>;

function CreateVersionDialog({
  cardId,
  open,
  onOpenChange,
}: {
  cardId: string;
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
  } = useForm<VersionValues>({ resolver: zodResolver(versionSchema) });
  const mutation = useMutation({
    mutationFn: (values: VersionValues) =>
      apiRequest(`/api/v1/rate-cards/${cardId}/versions`, {
        method: "POST",
        body: {
          effectiveFrom: values.effectiveFrom
            ? new Date(values.effectiveFrom).toISOString()
            : undefined,
          effectiveTo: values.effectiveTo
            ? new Date(values.effectiveTo).toISOString()
            : undefined,
          notes: values.notes || undefined,
        },
      }),
    onSuccess: () => {
      void client.invalidateQueries({
        queryKey: ["rate-card-versions", cardId],
      });
      toast({ tone: "success", title: "Draft version created" });
      reset();
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Create draft version"
      description="Published versions remain immutable. Configure at least one lane rate before activation."
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
            Create draft
          </Button>
        </>
      }
    >
      <form
        className="grid gap-4 sm:grid-cols-2"
        onSubmit={(event) => event.preventDefault()}
      >
        <Field
          label="Effective from"
          htmlFor="versionFrom"
          error={errors.effectiveFrom?.message}
        >
          <Input
            id="versionFrom"
            type="datetime-local"
            {...register("effectiveFrom")}
          />
        </Field>
        <Field
          label="Effective to"
          htmlFor="versionTo"
          error={errors.effectiveTo?.message}
        >
          <Input
            id="versionTo"
            type="datetime-local"
            {...register("effectiveTo")}
          />
        </Field>
        <Field
          label="Notes"
          htmlFor="versionNotes"
          error={errors.notes?.message}
          className="sm:col-span-2"
        >
          <Textarea id="versionNotes" {...register("notes")} />
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

export function RateCardVersionPage() {
  const { versionId = "" } = useParams();
  const { hasPermission } = useAuth();
  const { toast } = useToast();
  const client = useQueryClient();
  const [rateOpen, setRateOpen] = useState(false);
  const [slabOpen, setSlabOpen] = useState(false);
  const [surchargeOpen, setSurchargeOpen] = useState(false);
  const [discountOpen, setDiscountOpen] = useState(false);
  const [activateOpen, setActivateOpen] = useState(false);
  const query = useQuery({
    queryKey: ["rate-card-version", versionId],
    queryFn: () =>
      apiRequest<RateCardVersion>(`/api/v1/rate-cards/versions/${versionId}`),
  });
  const activateMutation = useMutation({
    mutationFn: () =>
      apiRequest(`/api/v1/rate-cards/versions/${versionId}/activate`, {
        method: "POST",
      }),
    onSuccess: () => {
      void client.invalidateQueries({
        queryKey: ["rate-card-version", versionId],
      });
      void client.invalidateQueries({ queryKey: ["rate-card-versions"] });
      toast({ tone: "success", title: "Rate card version activated" });
      setActivateOpen(false);
    },
  });
  if (query.isLoading) return <LoadingState />;
  if (query.error || !query.data)
    return <ErrorState error={query.error ?? new Error("Version not found")} />;
  const version = query.data;
  const weightPrices: WeightPriceRecord[] = [
    ...((version.domesticWeightSlabs ?? []).map((slab) => ({
      ...(slab as Record<string, unknown>),
      priceSource: "DOMESTIC",
    })) as WeightPriceRecord[]),
    ...((version.weightSlabs ?? []).map((slab) => ({
      ...(slab as Record<string, unknown>),
      priceSource: "RATE_CARD",
    })) as WeightPriceRecord[]),
  ];
  return (
    <>
      <PageHeader
        eyebrow="Commercial / Rate cards"
        title={`${version.rateCardCode ?? "Rate card"} · Version ${version.version ?? "—"}`}
        description={version.notes}
        actions={
          <>
            <StatusBadge status={version.status} />
            {version.editable ? (
              <Badge tone="success">Editable draft</Badge>
            ) : (
              <Badge>Immutable</Badge>
            )}
            {version.editable && hasPermission("rate_card.manage") ? (
              <>
                <Button onClick={() => setRateOpen(true)}>Add lane rate</Button>
                <Button onClick={() => setSlabOpen(true)}>
                  Add weight price
                </Button>
                <Button onClick={() => setSurchargeOpen(true)}>
                  Add surcharge
                </Button>
                <Button onClick={() => setDiscountOpen(true)}>
                  Add discount
                </Button>
              </>
            ) : null}
            {version.editable && hasPermission("rate_card.activate") ? (
              <Button variant="primary" onClick={() => setActivateOpen(true)}>
                Activate version
              </Button>
            ) : null}
          </>
        }
      />
      <div className="grid gap-5 xl:grid-cols-2">
        <Panel>
          <PanelHeader
            title="Zone rates"
            description="Linear base weight and additional step pricing by lane."
          />
          {version.zoneRates?.length ? (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr>
                    <TableHead>Service</TableHead>
                    <TableHead>Lane</TableHead>
                    <TableHead>Base</TableHead>
                    <TableHead>Additional</TableHead>
                  </tr>
                </thead>
                <tbody>
                  {version.zoneRates.map((rate, index) => (
                    <tr key={index}>
                      <TableCell>{String(rate.serviceCode ?? "—")}</TableCell>
                      <TableCell>
                        {String(rate.originZoneCode ?? "—")} →{" "}
                        {String(rate.destinationZoneCode ?? "—")}
                      </TableCell>
                      <TableCell>
                        {formatMoney(
                          Number(rate.basePriceMinor ?? 0),
                          version.currency,
                        )}{" "}
                        / {formatWeight(Number(rate.baseWeightGrams ?? 0))}
                      </TableCell>
                      <TableCell>
                        {formatMoney(
                          Number(rate.additionalPriceMinor ?? 0),
                          version.currency,
                        )}{" "}
                        / {formatWeight(Number(rate.additionalStepGrams ?? 0))}
                      </TableCell>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <EmptyState
              icon={Banknote}
              title="No zone rates"
              description={
                version.editable
                  ? "Add a lane rate before activation."
                  : "No lane rates were returned."
              }
            />
          )}
        </Panel>
        <Panel>
          <PanelHeader
            title="Surcharges"
            description="Applied in priority order. Insurance uses the configured rules only when requested."
          />
          {version.surcharges?.length ? (
            <div className="divide-y">
              {version.surcharges.map((rule, index) => (
                <div
                  key={index}
                  className="flex items-start justify-between gap-3 p-4"
                >
                  <div>
                    <p className="text-sm font-semibold">
                      {String(rule.code ?? "Rule")} ·{" "}
                      {String(rule.name ?? "Surcharge")}
                    </p>
                    <p className="mt-1 text-xs text-slate-500">
                      {titleCase(
                        String(rule.surchargeType ?? rule.type ?? "CUSTOM"),
                      )}{" "}
                      · {titleCase(String(rule.calcType ?? "FIXED"))}
                    </p>
                    {(rule.surchargeType ?? rule.type) === "INSURANCE" ? (
                      <p className="mt-1 text-xs text-muted-foreground">
                        Insurance requested only ·{" "}
                        {titleCase(String(rule.appliesTo ?? "FREIGHT"))}
                        {rule.serviceCode
                          ? ` · ${String(rule.serviceCode)}`
                          : " · All services"}
                      </p>
                    ) : null}
                    <p className="mt-1 text-xs text-muted-foreground">
                      {rule.isTaxable === false ? "Tax exempt" : "Taxable"}
                      {typeof rule.minAmountMinor === "number"
                        ? ` · Minimum ${formatMoney(rule.minAmountMinor, version.currency)}`
                        : ""}
                      {typeof rule.maxAmountMinor === "number"
                        ? ` · Maximum ${formatMoney(rule.maxAmountMinor, version.currency)}`
                        : ""}
                    </p>
                  </div>
                  <Badge>
                    {typeof rule.percentageBp === "number"
                      ? `${rule.percentageBp / 100}%`
                      : typeof rule.valueMinor === "number"
                        ? formatMoney(rule.valueMinor, version.currency)
                        : "Configured"}
                  </Badge>
                </div>
              ))}
            </div>
          ) : (
            <EmptyState
              icon={CircleHelp}
              title="No surcharge rules"
              description="No surcharge rules are attached to this version."
            />
          )}
        </Panel>
        <Panel>
          <PanelHeader
            title="Weight price list"
            description="Exact chargeable-weight categories. A matching slab takes priority over the linear lane rate."
          />
          {weightPrices.length ? (
            <DataTable label="Weight price list">
              <thead>
                <tr>
                  <TableHead>Service</TableHead>
                  <TableHead>Lane</TableHead>
                  <TableHead>Weight category</TableHead>
                  <TableHead className="text-right">Price</TableHead>
                </tr>
              </thead>
              <tbody>
                {weightPrices.map((slab, index) => (
                  <tr key={String(slab.id ?? index)}>
                    <TableCell>{String(slab.serviceCode ?? "—")}</TableCell>
                    <TableCell>
                      {slab.priceSource === "DOMESTIC"
                        ? `${String(slab.originStateName ?? slab.originStateCode ?? "—")} → tariff zone ${String(slab.rateZoneCode ?? "—")}`
                        : `${String(slab.originZoneCode ?? "—")} → ${String(slab.destinationZoneCode ?? "—")}`}
                    </TableCell>
                    <TableCell>
                      {formatWeight(Number(slab.fromWeightGrams ?? 0))} to{" "}
                      {typeof slab.toWeightGrams === "number"
                        ? formatWeight(slab.toWeightGrams - 1)
                        : "and above"}
                    </TableCell>
                    <TableCell className="text-right font-semibold tabular-nums">
                      {formatMoney(
                        Number(slab.priceMinor ?? 0),
                        version.currency,
                      )}
                      {typeof slab.additionalStepGrams === "number" &&
                      typeof slab.additionalPriceMinor === "number" ? (
                        <span className="block text-xs font-normal text-muted-foreground">
                          +
                          {formatMoney(
                            slab.additionalPriceMinor,
                            version.currency,
                          )}{" "}
                          / {formatWeight(slab.additionalStepGrams)}
                        </span>
                      ) : null}
                    </TableCell>
                  </tr>
                ))}
              </tbody>
            </DataTable>
          ) : (
            <EmptyState
              icon={Banknote}
              title="No exact weight prices"
              description="This version currently uses its linear lane prices. Add slabs when every weight category has a supplied price."
            />
          )}
        </Panel>
        <Panel>
          <PanelHeader
            title="Discounts"
            description="Rules on this rate card are applied by the server after freight and surcharges."
          />
          {version.discounts?.length ? (
            <div className="divide-y">
              {version.discounts.map((rule, index) => (
                <div
                  key={String(rule.id ?? index)}
                  className="flex items-start justify-between gap-3 p-4"
                >
                  <div>
                    <p className="text-sm font-semibold">
                      {String(rule.code ?? "Rule")} ·{" "}
                      {String(rule.name ?? "Discount")}
                    </p>
                    <p className="mt-1 text-xs text-muted-foreground">
                      Applies to{" "}
                      {titleCase(String(rule.appliesTo ?? "FREIGHT"))}
                      {rule.serviceCode
                        ? ` · ${String(rule.serviceCode)}`
                        : " · All services"}
                      {rule.isStackable ? " · Stackable" : ""}
                    </p>
                  </div>
                  <Badge tone="success">
                    {typeof rule.percentageBp === "number"
                      ? `${rule.percentageBp / 100}%`
                      : typeof rule.valueMinor === "number"
                        ? formatMoney(rule.valueMinor, version.currency)
                        : "Configured"}
                  </Badge>
                </div>
              ))}
            </div>
          ) : (
            <EmptyState
              icon={CircleHelp}
              title="No discounts"
              description="For a customer-specific discount, add the rule to a BUSINESS rate card associated with that customer."
            />
          )}
        </Panel>
      </div>
      <ZoneRateDialog
        versionId={versionId}
        open={rateOpen}
        onOpenChange={setRateOpen}
        onSaved={() => void query.refetch()}
      />
      <SurchargeDialog
        versionId={versionId}
        open={surchargeOpen}
        onOpenChange={setSurchargeOpen}
        onSaved={() => void query.refetch()}
      />
      <WeightSlabDialog
        versionId={versionId}
        open={slabOpen}
        onOpenChange={setSlabOpen}
        onSaved={() => void query.refetch()}
      />
      <DiscountDialog
        versionId={versionId}
        open={discountOpen}
        onOpenChange={setDiscountOpen}
        onSaved={() => void query.refetch()}
      />
      <ConfirmAction
        open={activateOpen}
        onOpenChange={setActivateOpen}
        title="Activate this rate card version?"
        description="Activation supersedes the current version atomically. This version and all of its prices become immutable."
        confirmLabel="Activate version"
        loading={activateMutation.isPending}
        onConfirm={() => activateMutation.mutate()}
      >
        <InlineNotice tone="warning" title="Permanent pricing publication">
          Review every lane, surcharge, effective date, and simulator result
          before activation.
        </InlineNotice>
        {activateMutation.error ? (
          <p role="alert" className="mt-3 text-sm text-danger">
            {activateMutation.error.message}
          </p>
        ) : null}
      </ConfirmAction>
    </>
  );
}

const zoneRateSchema = z.object({
  serviceCode: z.string().min(2),
  originZoneCode: z.string().min(2),
  destinationZoneCode: z.string().min(2),
  baseWeightGrams: z.number().int().min(1),
  basePrice: z.string().regex(/^\d+(\.\d{1,2})?$/),
  additionalStepGrams: z.number().int().min(1).optional(),
  additionalPrice: z.string().optional(),
  minChargeableWeightGrams: z.number().int().min(0).optional(),
});
type ZoneRateValues = z.infer<typeof zoneRateSchema>;

function ZoneRateDialog({
  versionId,
  open,
  onOpenChange,
  onSaved,
}: {
  versionId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSaved: () => void;
}) {
  const { toast } = useToast();
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<ZoneRateValues>({
    resolver: zodResolver(zoneRateSchema),
    defaultValues: {
      baseWeightGrams: 500,
      additionalStepGrams: 500,
      minChargeableWeightGrams: 0,
    },
  });
  const mutation = useMutation({
    mutationFn: (values: ZoneRateValues) =>
      apiRequest(`/api/v1/rate-cards/versions/${versionId}/zone-rates`, {
        method: "PUT",
        body: {
          serviceCode: values.serviceCode.toUpperCase(),
          originZoneCode: values.originZoneCode.toUpperCase(),
          destinationZoneCode: values.destinationZoneCode.toUpperCase(),
          baseWeightGrams: values.baseWeightGrams,
          basePriceMinor: toMinorUnits(values.basePrice),
          additionalStepGrams: values.additionalStepGrams,
          additionalPriceMinor: toMinorUnits(values.additionalPrice),
          minChargeableWeightGrams: values.minChargeableWeightGrams,
        },
      }),
    onSuccess: () => {
      onSaved();
      toast({ tone: "success", title: "Lane rate saved" });
      reset();
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Set lane rate"
      description="This updates the exact service and zone pair in the draft. Amounts are sent in minor units."
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
            Save lane rate
          </Button>
        </>
      }
    >
      <form
        className="grid gap-4 sm:grid-cols-2"
        onSubmit={(event) => event.preventDefault()}
      >
        <Field
          label="Service code"
          htmlFor="rateService"
          required
          error={errors.serviceCode?.message}
        >
          <Input
            id="rateService"
            className="uppercase"
            {...register("serviceCode")}
          />
        </Field>
        <div />
        <Field
          label="Origin zone"
          htmlFor="rateOriginZone"
          required
          error={errors.originZoneCode?.message}
        >
          <Input
            id="rateOriginZone"
            className="uppercase"
            {...register("originZoneCode")}
          />
        </Field>
        <Field
          label="Destination zone"
          htmlFor="rateDestinationZone"
          required
          error={errors.destinationZoneCode?.message}
        >
          <Input
            id="rateDestinationZone"
            className="uppercase"
            {...register("destinationZoneCode")}
          />
        </Field>
        <Field
          label="Base weight (g)"
          htmlFor="rateBaseWeight"
          required
          error={errors.baseWeightGrams?.message}
        >
          <Input
            id="rateBaseWeight"
            type="number"
            {...register("baseWeightGrams", { valueAsNumber: true })}
          />
        </Field>
        <Field
          label="Base price (₦)"
          htmlFor="rateBasePrice"
          required
          error={errors.basePrice?.message}
        >
          <Input
            id="rateBasePrice"
            inputMode="decimal"
            {...register("basePrice")}
          />
        </Field>
        <Field label="Additional step (g)" htmlFor="rateStep">
          <Input
            id="rateStep"
            type="number"
            {...register("additionalStepGrams", {
              setValueAs: (value) => (value === "" ? undefined : Number(value)),
            })}
          />
        </Field>
        <Field label="Additional price (₦)" htmlFor="rateStepPrice">
          <Input
            id="rateStepPrice"
            inputMode="decimal"
            {...register("additionalPrice")}
          />
        </Field>
        <Field label="Minimum chargeable weight (g)" htmlFor="rateMinimum">
          <Input
            id="rateMinimum"
            type="number"
            {...register("minChargeableWeightGrams", {
              setValueAs: (value) => (value === "" ? undefined : Number(value)),
            })}
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

const weightSlabSchema = z
  .object({
    serviceCode: z.string().min(2),
    originZoneCode: z.string().min(2),
    destinationZoneCode: z.string().min(2),
    fromWeightKg: z.number().min(0).multipleOf(0.001),
    toWeightKg: z.number().min(0.001).multipleOf(0.001).optional(),
    price: z.string().regex(/^\d+(\.\d{1,2})?$/, "Enter a valid price."),
  })
  .superRefine((value, context) => {
    if (
      value.toWeightKg !== undefined &&
      value.toWeightKg <= value.fromWeightKg
    )
      context.addIssue({
        code: "custom",
        path: ["toWeightKg"],
        message: "The upper weight must be greater than the lower weight.",
      });
  });
type WeightSlabValues = z.infer<typeof weightSlabSchema>;

function WeightSlabDialog({
  versionId,
  open,
  onOpenChange,
  onSaved,
}: {
  versionId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSaved: () => void;
}) {
  const { toast } = useToast();
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<WeightSlabValues>({
    resolver: zodResolver(weightSlabSchema),
    defaultValues: { fromWeightKg: 0, toWeightKg: 0.5 },
  });
  const mutation = useMutation({
    mutationFn: (values: WeightSlabValues) =>
      apiRequest(`/api/v1/rate-cards/versions/${versionId}/weight-slabs`, {
        method: "POST",
        body: {
          serviceCode: values.serviceCode.toUpperCase(),
          originZoneCode: values.originZoneCode.toUpperCase(),
          destinationZoneCode: values.destinationZoneCode.toUpperCase(),
          fromWeightGrams: kgToGrams(values.fromWeightKg),
          toWeightGrams:
            values.toWeightKg === undefined
              ? undefined
              : kgToGrams(values.toWeightKg),
          priceMinor: toMinorUnits(values.price),
        },
      }),
    onSuccess: () => {
      onSaved();
      toast({ tone: "success", title: "Weight price added" });
      reset();
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Add weight price"
      description="Create one non-overlapping category for the selected service and lane. The upper bound is exclusive."
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
            Add weight price
          </Button>
        </>
      }
    >
      <form
        className="grid gap-4 sm:grid-cols-2"
        onSubmit={(event) => event.preventDefault()}
      >
        <Field
          label="Service code"
          htmlFor="slabService"
          required
          error={errors.serviceCode?.message}
        >
          <Input
            id="slabService"
            className="uppercase"
            {...register("serviceCode")}
          />
        </Field>
        <div />
        <Field
          label="Origin zone"
          htmlFor="slabOrigin"
          required
          error={errors.originZoneCode?.message}
        >
          <Input
            id="slabOrigin"
            className="uppercase"
            {...register("originZoneCode")}
          />
        </Field>
        <Field
          label="Destination zone"
          htmlFor="slabDestination"
          required
          error={errors.destinationZoneCode?.message}
        >
          <Input
            id="slabDestination"
            className="uppercase"
            {...register("destinationZoneCode")}
          />
        </Field>
        <Field
          label="From weight (kg)"
          htmlFor="slabFrom"
          required
          error={errors.fromWeightKg?.message}
        >
          <Input
            id="slabFrom"
            type="number"
            min={0}
            step={0.001}
            {...register("fromWeightKg", { valueAsNumber: true })}
          />
        </Field>
        <Field
          label="Up to weight (kg)"
          htmlFor="slabTo"
          hint="Leave blank only for the final open-ended category."
          error={errors.toWeightKg?.message}
        >
          <Input
            id="slabTo"
            type="number"
            min={0.001}
            step={0.001}
            {...register("toWeightKg", {
              setValueAs: (value) => (value === "" ? undefined : Number(value)),
            })}
          />
        </Field>
        <Field
          label="Price (₦)"
          htmlFor="slabPrice"
          required
          error={errors.price?.message}
        >
          <Input id="slabPrice" inputMode="decimal" {...register("price")} />
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

const discountSchema = z
  .object({
    code: z.string().regex(/^[A-Z0-9][A-Z0-9_-]{1,31}$/),
    name: z.string().min(2),
    discountType: z.enum(["PERCENTAGE", "FIXED"]),
    percentage: z.string().optional(),
    amount: z.string().optional(),
    appliesTo: z.enum(["FREIGHT", "FREIGHT_PLUS_SURCHARGES"]),
    serviceCode: z.string().optional(),
    minimumSubtotal: z.string().optional(),
    maximumDiscount: z.string().optional(),
    priority: z.number().int().min(0).max(1000),
    isStackable: z.boolean(),
  })
  .superRefine((value, context) => {
    if (
      value.discountType === "PERCENTAGE" &&
      (!value.percentage ||
        !/^\d+(\.\d{1,2})?$/.test(value.percentage) ||
        Number(value.percentage) > 100)
    )
      context.addIssue({
        code: "custom",
        path: ["percentage"],
        message: "Enter a percentage from 0 to 100.",
      });
    if (
      value.discountType === "FIXED" &&
      (!value.amount || !/^\d+(\.\d{1,2})?$/.test(value.amount))
    )
      context.addIssue({
        code: "custom",
        path: ["amount"],
        message: "Enter a fixed discount amount.",
      });
  });
type DiscountValues = z.infer<typeof discountSchema>;

function DiscountDialog({
  versionId,
  open,
  onOpenChange,
  onSaved,
}: {
  versionId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSaved: () => void;
}) {
  const { toast } = useToast();
  const {
    register,
    handleSubmit,
    watch,
    reset,
    formState: { errors },
  } = useForm<DiscountValues>({
    resolver: zodResolver(discountSchema),
    defaultValues: {
      discountType: "PERCENTAGE",
      appliesTo: "FREIGHT",
      priority: 100,
      isStackable: false,
    },
  });
  const discountType = watch("discountType");
  const mutation = useMutation({
    mutationFn: (values: DiscountValues) =>
      apiRequest(`/api/v1/rate-cards/versions/${versionId}/discounts`, {
        method: "POST",
        body: {
          code: values.code.toUpperCase(),
          name: values.name,
          discountType: values.discountType,
          percentageBp:
            values.discountType === "PERCENTAGE"
              ? Math.round(Number(values.percentage) * 100)
              : undefined,
          valueMinor:
            values.discountType === "FIXED"
              ? toMinorUnits(values.amount)
              : undefined,
          appliesTo: values.appliesTo,
          serviceCode: values.serviceCode?.toUpperCase() || undefined,
          minSubtotalMinor: toMinorUnits(values.minimumSubtotal) ?? 0,
          maxDiscountMinor: toMinorUnits(values.maximumDiscount),
          priority: values.priority,
          isStackable: values.isStackable,
        },
      }),
    onSuccess: () => {
      onSaved();
      toast({ tone: "success", title: "Discount rule added" });
      reset();
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Add discount"
      description="Use a BUSINESS rate card associated with one customer for a customer-specific discount. The server applies this rule to every matching quote."
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
            Add discount
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
          htmlFor="discountCode"
          required
          error={errors.code?.message}
        >
          <Input
            id="discountCode"
            className="uppercase"
            {...register("code")}
          />
        </Field>
        <Field
          label="Name"
          htmlFor="discountName"
          required
          error={errors.name?.message}
        >
          <Input id="discountName" {...register("name")} />
        </Field>
        <Field label="Discount type" htmlFor="discountType">
          <Select id="discountType" {...register("discountType")}>
            <option value="PERCENTAGE">Percentage</option>
            <option value="FIXED">Fixed amount</option>
          </Select>
        </Field>
        {discountType === "PERCENTAGE" ? (
          <Field
            label="Percentage"
            htmlFor="discountPercentage"
            required
            error={errors.percentage?.message}
          >
            <Input
              id="discountPercentage"
              inputMode="decimal"
              {...register("percentage")}
            />
          </Field>
        ) : (
          <Field
            label="Amount (₦)"
            htmlFor="discountAmount"
            required
            error={errors.amount?.message}
          >
            <Input
              id="discountAmount"
              inputMode="decimal"
              {...register("amount")}
            />
          </Field>
        )}
        <Field label="Apply to" htmlFor="discountAppliesTo">
          <Select id="discountAppliesTo" {...register("appliesTo")}>
            <option value="FREIGHT">Freight</option>
            <option value="FREIGHT_PLUS_SURCHARGES">
              Freight plus surcharges
            </option>
          </Select>
        </Field>
        <Field
          label="Service code"
          htmlFor="discountService"
          hint="Leave blank for every service."
        >
          <Input
            id="discountService"
            className="uppercase"
            {...register("serviceCode")}
          />
        </Field>
        <Field label="Minimum subtotal (₦)" htmlFor="discountMinimum">
          <Input
            id="discountMinimum"
            inputMode="decimal"
            {...register("minimumSubtotal")}
          />
        </Field>
        <Field label="Maximum discount (₦)" htmlFor="discountMaximum">
          <Input
            id="discountMaximum"
            inputMode="decimal"
            {...register("maximumDiscount")}
          />
        </Field>
        <Field label="Priority" htmlFor="discountPriority">
          <Input
            id="discountPriority"
            type="number"
            {...register("priority", { valueAsNumber: true })}
          />
        </Field>
        <label className="flex items-center gap-2 self-end pb-2 text-sm">
          <input
            type="checkbox"
            className="h-4 w-4 accent-emerald-800"
            {...register("isStackable")}
          />{" "}
          Stack with other discounts
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

const optionalSurchargeAmount = z
  .string()
  .optional()
  .refine(
    (value) =>
      !value ||
      (/^\d+(\.\d{1,2})?$/.test(value) &&
        Number.isSafeInteger(toMinorUnits(value)) &&
        Number(value) <= 10_000_000_000),
    "Enter a nonnegative amount with up to two decimal places.",
  );
const surchargeSchema = z
  .object({
    code: z.string().regex(/^[A-Z0-9][A-Z0-9_-]{1,31}$/),
    name: z.string().min(2),
    surchargeType: z.enum([
      "FUEL",
      "REMOTE_AREA",
      "COD",
      "INSURANCE",
      "HANDLING",
      "OVERSIZE",
      "DOCUMENTATION",
      "PACKAGING",
      "APPOINTMENT",
      "CUSTOM",
    ]),
    calcType: z.enum(["FIXED", "PERCENTAGE", "PER_KG"]),
    value: optionalSurchargeAmount,
    minimum: optionalSurchargeAmount,
    maximum: optionalSurchargeAmount,
    percentage: z.number().min(0).max(100).multipleOf(0.01).optional(),
    appliesTo: z.enum([
      "FREIGHT",
      "FREIGHT_PLUS_SURCHARGES",
      "DECLARED_VALUE",
      "COD_AMOUNT",
    ]),
    serviceCode: z.string().optional(),
    priority: z.number().int().min(0),
    isTaxable: z.boolean(),
  })
  .superRefine((values, ctx) => {
    if (values.calcType === "PERCENTAGE" && values.percentage === undefined) {
      ctx.addIssue({
        code: "custom",
        path: ["percentage"],
        message: "Enter the percentage.",
      });
    }
    if (values.calcType !== "PERCENTAGE" && !values.value) {
      ctx.addIssue({
        code: "custom",
        path: ["value"],
        message: "Enter the surcharge value.",
      });
    }
    const minimum = toMinorUnits(values.minimum);
    const maximum = toMinorUnits(values.maximum);
    if (minimum !== undefined && maximum !== undefined && maximum < minimum) {
      ctx.addIssue({
        code: "custom",
        path: ["maximum"],
        message: "Maximum cannot be lower than minimum.",
      });
    }
  });
type SurchargeValues = z.infer<typeof surchargeSchema>;

function SurchargeDialog({
  versionId,
  open,
  onOpenChange,
  onSaved,
}: {
  versionId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSaved: () => void;
}) {
  const { toast } = useToast();
  const {
    register,
    handleSubmit,
    watch,
    reset,
    setValue,
    formState: { errors },
  } = useForm<SurchargeValues>({
    resolver: zodResolver(surchargeSchema),
    defaultValues: {
      surchargeType: "FUEL",
      calcType: "PERCENTAGE",
      appliesTo: "FREIGHT",
      priority: 100,
      isTaxable: true,
    },
  });
  const calcType = watch("calcType");
  const mutation = useMutation({
    mutationFn: (values: SurchargeValues) => {
      if (values.calcType === "PERCENTAGE" && values.percentage === undefined)
        throw new Error("Enter the percentage.");
      if (
        values.calcType !== "PERCENTAGE" &&
        toMinorUnits(values.value) === undefined
      )
        throw new Error("Enter the surcharge value.");
      return apiRequest(`/api/v1/rate-cards/versions/${versionId}/surcharges`, {
        method: "POST",
        body: {
          code: values.code.toUpperCase(),
          name: values.name,
          surchargeType: values.surchargeType,
          calcType: values.calcType,
          valueMinor:
            values.calcType === "PERCENTAGE"
              ? undefined
              : toMinorUnits(values.value),
          percentageBp:
            values.calcType === "PERCENTAGE"
              ? Math.round((values.percentage ?? 0) * 100)
              : undefined,
          appliesTo: values.appliesTo,
          serviceCode: values.serviceCode?.toUpperCase() || undefined,
          priority: values.priority,
          isTaxable: values.isTaxable,
          minAmountMinor: toMinorUnits(values.minimum),
          maxAmountMinor: toMinorUnits(values.maximum),
          conditions:
            values.surchargeType === "INSURANCE"
              ? { requiresInsurance: true }
              : undefined,
        } satisfies components["schemas"]["CreateSurchargeRequest"],
      });
    },
    onSuccess: () => {
      onSaved();
      toast({ tone: "success", title: "Surcharge added" });
      reset();
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Add surcharge"
      description="Rules run in ascending priority. Published versions cannot be changed."
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
            Add surcharge
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
          htmlFor="surchargeCode"
          required
          error={errors.code?.message}
        >
          <Input
            id="surchargeCode"
            className="uppercase"
            {...register("code")}
          />
        </Field>
        <Field
          label="Name"
          htmlFor="surchargeName"
          required
          error={errors.name?.message}
        >
          <Input id="surchargeName" {...register("name")} />
        </Field>
        <Field label="Type" htmlFor="surchargeType">
          <Select
            id="surchargeType"
            {...register("surchargeType")}
            onChange={(event) => {
              setValue(
                "surchargeType",
                event.target.value as SurchargeValues["surchargeType"],
                { shouldDirty: true },
              );
              if (event.target.value === "INSURANCE") {
                setValue("calcType", "PERCENTAGE");
                setValue("appliesTo", "DECLARED_VALUE");
              }
            }}
          >
            <option>FUEL</option>
            <option>REMOTE_AREA</option>
            <option>COD</option>
            <option>INSURANCE</option>
            <option>HANDLING</option>
            <option>OVERSIZE</option>
            <option>DOCUMENTATION</option>
            <option>PACKAGING</option>
            <option>APPOINTMENT</option>
            <option>CUSTOM</option>
          </Select>
        </Field>
        {watch("surchargeType") === "INSURANCE" ? (
          <p className="text-xs text-muted-foreground sm:col-span-2">
            Set the insurance rate for this version. It applies only when
            requested, with any service restriction, limits and tax setting
            below. Matching rules are added in priority order.
          </p>
        ) : null}
        <Field label="Calculation" htmlFor="surchargeCalc">
          <Select id="surchargeCalc" {...register("calcType")}>
            <option>FIXED</option>
            <option>PERCENTAGE</option>
            <option>PER_KG</option>
          </Select>
        </Field>
        {calcType === "PERCENTAGE" ? (
          <Field
            label="Percentage"
            htmlFor="surchargePercentage"
            required
            error={errors.percentage?.message}
          >
            <Input
              id="surchargePercentage"
              type="number"
              step="0.01"
              {...register("percentage", {
                setValueAs: (value) =>
                  value === "" ? undefined : Number(value),
              })}
            />
          </Field>
        ) : (
          <Field
            label="Value"
            htmlFor="surchargeValue"
            hint="In this rate card’s currency."
            error={errors.value?.message}
          >
            <Input
              id="surchargeValue"
              inputMode="decimal"
              {...register("value")}
            />
          </Field>
        )}
        <Field label="Applies to" htmlFor="surchargeBasis">
          <Select id="surchargeBasis" {...register("appliesTo")}>
            <option>FREIGHT</option>
            <option>FREIGHT_PLUS_SURCHARGES</option>
            <option>DECLARED_VALUE</option>
            <option>COD_AMOUNT</option>
          </Select>
        </Field>
        <Field
          label="Minimum charge"
          htmlFor="surchargeMinimum"
          hint="Optional, in this rate card’s currency."
          error={errors.minimum?.message}
        >
          <Input
            id="surchargeMinimum"
            inputMode="decimal"
            {...register("minimum")}
          />
        </Field>
        <Field
          label="Maximum charge"
          htmlFor="surchargeMaximum"
          hint="Optional, in this rate card’s currency."
          error={errors.maximum?.message}
        >
          <Input
            id="surchargeMaximum"
            inputMode="decimal"
            {...register("maximum")}
          />
        </Field>
        <Field label="Service code" htmlFor="surchargeService">
          <Input
            id="surchargeService"
            className="uppercase"
            {...register("serviceCode")}
          />
        </Field>
        <Field label="Priority" htmlFor="surchargePriority">
          <Input
            id="surchargePriority"
            type="number"
            {...register("priority", { valueAsNumber: true })}
          />
        </Field>
        <label className="flex items-center gap-2 self-end pb-2 text-sm">
          <input
            type="checkbox"
            className="h-4 w-4 accent-emerald-800"
            {...register("isTaxable")}
          />{" "}
          Taxable
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

function text(record: Record<string, unknown>, ...keys: string[]) {
  for (const key of keys) {
    const value = record[key];
    if (typeof value === "string" || typeof value === "number")
      return String(value);
  }
  return "";
}
function bool(record: Record<string, unknown>, key: string) {
  return record[key] === true;
}
