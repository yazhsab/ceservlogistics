import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Calculator, FilePlus2, GitBranch, Percent, Plus } from "lucide-react";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { Link, useNavigate, useParams } from "react-router-dom";
import { z } from "zod";
import { useAuth } from "../auth/AuthProvider";
import {
  apiRequest,
  queryString,
  type CommissionCalculation,
  type CommissionCalculationListResponse,
  type CommissionRule,
  type CommissionRuleDetailResponse,
  type CommissionRuleListResponse,
  type CommissionRuleRequest,
  type CommissionSimulateRequest,
  type CommissionSimulateResult,
  type CommissionVersionRequest,
} from "../api/client";
import { FinancialStatus, Money, ReferenceLink } from "../components/financial";
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
  Pagination,
  Panel,
  PanelHeader,
  Select,
  TableCell,
  TableHead,
  Textarea,
} from "../components/ui";
import { formatDateTime, titleCase, toMinorUnits } from "../lib/utils";

const commissionTypes = [
  "BOOKING",
  "PICKUP",
  "ORIGIN_HANDLING",
  "TRANSIT_HANDLING",
  "DESTINATION_HANDLING",
  "DELIVERY",
  "COD",
  "VOLUME_INCENTIVE",
  "CUSTOM",
] as const;
const recipientRoles = [
  "ORIGIN_FRANCHISE",
  "DESTINATION_FRANCHISE",
  "PICKUP_AGENT",
  "DELIVERY_AGENT",
  "ORIGIN_UNIT",
  "TRANSIT_UNIT",
  "DESTINATION_UNIT",
  "CUSTOM",
] as const;
const bases = [
  "FREIGHT",
  "SURCHARGE",
  "FREIGHT_PLUS_SURCHARGE",
  "TOTAL_BEFORE_TAX",
  "TOTAL",
  "COD_AMOUNT",
  "CHARGEABLE_WEIGHT",
  "SHIPMENT_COUNT",
] as const;

const ruleSchema = z.object({
  schemeCode: z.string().trim().min(1),
  code: z.string().trim().min(2),
  name: z.string().trim().min(2),
  commissionType: z.enum(commissionTypes),
  recipientRole: z.enum(recipientRoles),
  franchiseId: z.string().trim().optional(),
  franchiseCategory: z.string().trim().optional(),
  serviceCode: z.string().trim().optional(),
  customerCategory: z.string().trim().optional(),
  paymentMode: z.enum(["", "PREPAID", "COD", "CREDIT", "TO_PAY"]),
  priority: z.string().regex(/^\d+$/, "Enter a whole number"),
  description: z.string().trim().optional(),
});
type RuleForm = z.infer<typeof ruleSchema>;

function RuleDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const form = useForm<RuleForm>({
    resolver: zodResolver(ruleSchema),
    defaultValues: {
      schemeCode: "STANDARD",
      commissionType: "DELIVERY",
      recipientRole: "DESTINATION_FRANCHISE",
      paymentMode: "",
      priority: "0",
    },
  });
  const mutation = useMutation({
    mutationFn: (values: RuleForm) => {
      const body: CommissionRuleRequest = {
        ...values,
        code: values.code.toUpperCase(),
        schemeCode: values.schemeCode.toUpperCase(),
        franchiseId: values.franchiseId || null,
        franchiseCategory: values.franchiseCategory || null,
        serviceCode: values.serviceCode || null,
        customerCategory: values.customerCategory || null,
        paymentMode: values.paymentMode || null,
        priority: Number(values.priority),
      };
      return apiRequest<CommissionRule>("/api/v1/commission/rules", {
        method: "POST",
        body,
      });
    },
    onSuccess: (rule) => {
      void queryClient.invalidateQueries({ queryKey: ["commission-rules"] });
      onOpenChange(false);
      if (rule.id) void navigate(`/finance/commission/rules/${rule.id}`);
    },
  });
  const submitRule = form.handleSubmit((value) => mutation.mutate(value));
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Create commission rule"
      description="Scope determines precedence. Rates are added as an effective-dated version after the rule is created."
      footer={
        <>
          <Button onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button
            variant="primary"
            loading={mutation.isPending}
            onClick={() => void submitRule()}
          >
            Create rule
          </Button>
        </>
      }
    >
      <form
        className="grid gap-4 sm:grid-cols-2"
        onSubmit={(event) => void submitRule(event)}
      >
        <Field
          label="Scheme code"
          htmlFor="scheme"
          required
          error={form.formState.errors.schemeCode?.message}
        >
          <Input id="scheme" {...form.register("schemeCode")} />
        </Field>
        <Field
          label="Rule code"
          htmlFor="rule-code"
          required
          error={form.formState.errors.code?.message}
        >
          <Input id="rule-code" {...form.register("code")} />
        </Field>
        <Field
          className="sm:col-span-2"
          label="Rule name"
          htmlFor="rule-name"
          required
          error={form.formState.errors.name?.message}
        >
          <Input id="rule-name" {...form.register("name")} />
        </Field>
        <Field label="Commission type" htmlFor="commission-type" required>
          <Select id="commission-type" {...form.register("commissionType")}>
            {commissionTypes.map((value) => (
              <option key={value} value={value}>
                {titleCase(value)}
              </option>
            ))}
          </Select>
        </Field>
        <Field label="Recipient" htmlFor="recipient-role" required>
          <Select id="recipient-role" {...form.register("recipientRole")}>
            {recipientRoles.map((value) => (
              <option key={value} value={value}>
                {titleCase(value)}
              </option>
            ))}
          </Select>
        </Field>
        <Field
          label="Franchise ID"
          htmlFor="franchise-id"
          hint="Unset means any franchise."
        >
          <Input id="franchise-id" {...form.register("franchiseId")} />
        </Field>
        <Field
          label="Franchise category"
          htmlFor="franchise-category"
          hint="Unset means any category."
        >
          <Input
            id="franchise-category"
            {...form.register("franchiseCategory")}
          />
        </Field>
        <Field
          label="Service code"
          htmlFor="service-code"
          hint="Unset means any service."
        >
          <Input id="service-code" {...form.register("serviceCode")} />
        </Field>
        <Field label="Customer category" htmlFor="customer-category">
          <Input
            id="customer-category"
            {...form.register("customerCategory")}
          />
        </Field>
        <Field label="Payment mode" htmlFor="payment-mode">
          <Select id="payment-mode" {...form.register("paymentMode")}>
            <option value="">Any payment mode</option>
            {(["PREPAID", "COD", "CREDIT", "TO_PAY"] as const).map((value) => (
              <option key={value}>{value}</option>
            ))}
          </Select>
        </Field>
        <Field
          label="Priority"
          htmlFor="priority"
          hint="Higher wins after specificity."
        >
          <Input id="priority" type="number" {...form.register("priority")} />
        </Field>
        <Field
          className="sm:col-span-2"
          label="Description"
          htmlFor="rule-description"
        >
          <Textarea id="rule-description" {...form.register("description")} />
        </Field>
        {mutation.error ? (
          <div className="sm:col-span-2">
            <ErrorState error={mutation.error} />
          </div>
        ) : null}
      </form>
    </Dialog>
  );
}

export function CommissionRulesPage() {
  const { hasPermission } = useAuth();
  const [createOpen, setCreateOpen] = useState(false);
  const [type, setType] = useState("");
  const [status, setStatus] = useState("");
  const [offset, setOffset] = useState(0);
  const query = useQuery({
    queryKey: ["commission-rules", type, status, offset],
    queryFn: () =>
      apiRequest<CommissionRuleListResponse>(
        `/api/v1/commission/rules${queryString({ commissionType: type, status, limit: 50, offset })}`,
      ),
  });
  const rules = query.data?.data ?? [];
  const total = query.data?.total ?? rules.length;
  return (
    <>
      <PageHeader
        eyebrow="Finance · Commission"
        title="Commission rules"
        description="Resolver order is specificity, then priority, then oldest rule. Every rate change creates a new effective-dated version."
        actions={
          <>
            <Link
              className="inline-flex min-h-10 items-center gap-2 rounded-md border border-border bg-white px-3.5 text-sm font-semibold"
              to="/finance/commission/simulator"
            >
              <Calculator className="h-4 w-4" /> Simulator
            </Link>
            <Link
              className="inline-flex min-h-10 items-center gap-2 rounded-md border border-border bg-white px-3.5 text-sm font-semibold"
              to="/finance/commission/entries"
            >
              Entries
            </Link>
            {hasPermission("commission.config") ? (
              <Button variant="primary" onClick={() => setCreateOpen(true)}>
                <Plus className="h-4 w-4" /> Create rule
              </Button>
            ) : null}
          </>
        }
      />
      <InlineNotice title="Scheme administration boundary">
        Rules expose their scheme code, but Release 3 has no scheme list/detail
        endpoint. This register therefore does not fabricate editable scheme
        records.
      </InlineNotice>
      <Panel className="mt-4">
        <FilterBar>
          <Select
            aria-label="Commission type"
            value={type}
            onChange={(event) => {
              setType(event.target.value);
              setOffset(0);
            }}
            className="w-56"
          >
            <option value="">All commission types</option>
            {commissionTypes.map((value) => (
              <option key={value} value={value}>
                {titleCase(value)}
              </option>
            ))}
          </Select>
          <Select
            aria-label="Rule status"
            value={status}
            onChange={(event) => {
              setStatus(event.target.value);
              setOffset(0);
            }}
            className="w-44"
          >
            <option value="">All states</option>
            <option>DRAFT</option>
            <option>ACTIVE</option>
            <option>INACTIVE</option>
          </Select>
        </FilterBar>
        {query.isPending ? (
          <LoadingState label="Loading commission rules" />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : rules.length === 0 ? (
          <EmptyState
            icon={Percent}
            title="No matching rules"
            description="Create the first scoped rule or change the filters."
          />
        ) : (
          <>
            <DataTable label="Commission rules">
              <thead>
                <tr>
                  <TableHead>Rule</TableHead>
                  <TableHead>Scheme</TableHead>
                  <TableHead>Type / recipient</TableHead>
                  <TableHead>Scope</TableHead>
                  <TableHead>Precedence</TableHead>
                  <TableHead>Status</TableHead>
                </tr>
              </thead>
              <tbody>
                {rules.map((rule) => (
                  <tr key={rule.id}>
                    <TableCell>
                      <Link
                        className="font-semibold text-primary hover:underline"
                        to={`/finance/commission/rules/${rule.id}`}
                      >
                        {rule.code}
                      </Link>
                      <span className="block text-xs text-slate-500">
                        {rule.name}
                      </span>
                    </TableCell>
                    <TableCell>{rule.schemeCode ?? "—"}</TableCell>
                    <TableCell>
                      {titleCase(rule.commissionType ?? "Unknown")}
                      <span className="block text-xs text-slate-500">
                        {titleCase(rule.recipientRole ?? "Unknown")}
                      </span>
                    </TableCell>
                    <TableCell className="max-w-xs text-xs">
                      {[
                        rule.franchiseCategory,
                        rule.paymentMode,
                        rule.serviceId ? `Service ${rule.serviceId}` : null,
                      ]
                        .filter(Boolean)
                        .join(" · ") || "Any"}
                    </TableCell>
                    <TableCell>
                      <strong>{rule.specificity ?? 0}</strong>
                      <span className="block text-xs text-slate-500">
                        Priority {rule.priority ?? 0}
                      </span>
                    </TableCell>
                    <TableCell>
                      <FinancialStatus status={rule.status} />
                    </TableCell>
                  </tr>
                ))}
              </tbody>
            </DataTable>
            <Pagination
              page={Math.floor(offset / 50) + 1}
              totalPages={Math.max(1, Math.ceil(total / 50))}
              onPageChange={(page) => setOffset((page - 1) * 50)}
            />
          </>
        )}
      </Panel>
      <RuleDialog open={createOpen} onOpenChange={setCreateOpen} />
    </>
  );
}

const versionSchema = z.object({
  calculationMethod: z.enum(["FIXED", "PERCENTAGE", "SLAB"]),
  fixedAmount: z.string().optional(),
  rateBp: z.string().regex(/^\d+$/, "Enter basis points").optional(),
  basis: z.enum(["", ...bases]),
  minAmount: z.string().optional(),
  maxAmount: z.string().optional(),
  effectiveFrom: z.string().min(1),
  notes: z.string().optional(),
});
type VersionForm = z.infer<typeof versionSchema>;
type SlabDraft = {
  fromMinor: string;
  toMinor: string;
  amountMinor: string;
  label: string;
};

export function CommissionRuleDetailPage() {
  const { ruleId = "" } = useParams();
  const { hasPermission } = useAuth();
  const [versionOpen, setVersionOpen] = useState(false);
  const [slabs, setSlabs] = useState<SlabDraft[]>([
    { fromMinor: "0", toMinor: "", amountMinor: "", label: "" },
  ]);
  const queryClient = useQueryClient();
  const query = useQuery({
    queryKey: ["commission-rule", ruleId],
    queryFn: () =>
      apiRequest<CommissionRuleDetailResponse>(
        `/api/v1/commission/rules/${ruleId}`,
      ),
    enabled: Boolean(ruleId),
  });
  const form = useForm<VersionForm>({
    resolver: zodResolver(versionSchema),
    defaultValues: {
      calculationMethod: "PERCENTAGE",
      basis: "FREIGHT",
      effectiveFrom: new Date().toISOString().slice(0, 10),
    },
  });
  const method = form.watch("calculationMethod");
  const mutation = useMutation({
    mutationFn: (value: VersionForm) => {
      const body: CommissionVersionRequest = {
        calculationMethod: value.calculationMethod,
        effectiveFrom: value.effectiveFrom,
        notes: value.notes,
        ...(value.fixedAmount
          ? { fixedAmountMinor: toMinorUnits(value.fixedAmount) }
          : {}),
        ...(value.rateBp ? { rateBp: Number(value.rateBp) } : {}),
        ...(value.basis ? { basis: value.basis } : {}),
        ...(value.minAmount
          ? { minAmountMinor: toMinorUnits(value.minAmount) }
          : {}),
        ...(value.maxAmount
          ? { maxAmountMinor: toMinorUnits(value.maxAmount) }
          : {}),
        ...(value.calculationMethod === "SLAB"
          ? {
              slabs: slabs.map((slab) => ({
                fromMinor: Number(slab.fromMinor),
                toMinor: slab.toMinor ? Number(slab.toMinor) : null,
                amountMinor: Number(slab.amountMinor),
                label: slab.label || undefined,
              })),
            }
          : {}),
      };
      return apiRequest(`/api/v1/commission/rules/${ruleId}/versions`, {
        method: "POST",
        body,
      });
    },
    onSuccess: () => {
      setVersionOpen(false);
      void queryClient.invalidateQueries({
        queryKey: ["commission-rule", ruleId],
      });
    },
  });
  const submitVersion = form.handleSubmit((value) => mutation.mutate(value));
  const slabsValid =
    method !== "SLAB" ||
    (slabs.length > 0 &&
      slabs[0]?.fromMinor === "0" &&
      slabs.every((slab, index) => {
        const previous = slabs[index - 1];
        const isLast = index === slabs.length - 1;
        return (
          /^\d+$/.test(slab.fromMinor) &&
          /^\d+$/.test(slab.amountMinor) &&
          (isLast ? slab.toMinor === "" : /^\d+$/.test(slab.toMinor)) &&
          (!previous || previous.toMinor === slab.fromMinor)
        );
      }));
  if (query.isPending) return <LoadingState label="Loading commission rule" />;
  if (query.error)
    return (
      <ErrorState error={query.error} retry={() => void query.refetch()} />
    );
  const rule = query.data?.rule;
  const versions = query.data?.versions ?? [];
  if (!rule)
    return (
      <EmptyState
        title="Rule unavailable"
        description="The backend did not return this commission rule."
      />
    );
  return (
    <>
      <PageHeader
        eyebrow="Finance · Commission rule"
        title={`${rule.code ?? "Rule"} · ${rule.name ?? ""}`}
        description="Scope and resolver precedence are permanent context. Change rates by adding a version."
        actions={
          hasPermission("commission.config") ? (
            <Button variant="primary" onClick={() => setVersionOpen(true)}>
              <FilePlus2 className="h-4 w-4" /> New version
            </Button>
          ) : undefined
        }
      />
      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_360px]">
        <Panel>
          <PanelHeader
            title="Version history"
            description="Newest effective version first; versions that produced calculations are frozen."
          />
          {versions.length ? (
            <DataTable label="Commission rule versions">
              <thead>
                <tr>
                  <TableHead>Version</TableHead>
                  <TableHead>Effective period</TableHead>
                  <TableHead>Method</TableHead>
                  <TableHead>Rate / amount</TableHead>
                  <TableHead>Status</TableHead>
                </tr>
              </thead>
              <tbody>
                {versions.map((version) => (
                  <tr key={version.id}>
                    <TableCell className="font-semibold">
                      v{version.versionNo}
                    </TableCell>
                    <TableCell>
                      {version.effectiveFrom} →{" "}
                      {version.effectiveTo ?? "Present"}
                    </TableCell>
                    <TableCell>
                      {titleCase(version.calculationMethod ?? "Unknown")}
                      <span className="block text-xs text-slate-500">
                        {titleCase(version.basis ?? "No basis")}
                      </span>
                    </TableCell>
                    <TableCell>
                      {version.fixedAmountMinor != null ? (
                        <Money
                          amountMinor={version.fixedAmountMinor}
                          currency={version.currency}
                        />
                      ) : version.rateBp != null ? (
                        `${(version.rateBp / 100).toFixed(2)}%`
                      ) : (
                        `${version.slabs?.length ?? 0} slabs`
                      )}
                    </TableCell>
                    <TableCell>
                      <FinancialStatus status={version.status} />
                    </TableCell>
                  </tr>
                ))}
              </tbody>
            </DataTable>
          ) : (
            <EmptyState
              icon={GitBranch}
              title="No rate version"
              description="This rule cannot calculate commission until a version is added."
            />
          )}
        </Panel>
        <Panel>
          <PanelHeader title="Scope and precedence" />
          <dl className="divide-y divide-border text-sm">
            {[
              ["Scheme", rule.schemeCode],
              ["Commission type", titleCase(rule.commissionType ?? "Unknown")],
              ["Recipient", titleCase(rule.recipientRole ?? "Unknown")],
              ["Franchise category", rule.franchiseCategory ?? "Any"],
              ["Payment mode", rule.paymentMode ?? "Any"],
              ["Specificity", rule.specificity ?? 0],
              ["Priority", rule.priority ?? 0],
            ].map(([label, value]) => (
              <div
                key={String(label)}
                className="flex justify-between gap-4 px-4 py-3"
              >
                <dt className="text-slate-500">{label}</dt>
                <dd className="text-right font-medium">{value}</dd>
              </div>
            ))}
          </dl>
        </Panel>
      </div>
      <Dialog
        open={versionOpen}
        onOpenChange={setVersionOpen}
        title="Add commission rule version"
        description="This creates a new effective-dated rate; it does not edit historical calculations."
        footer={
          <>
            <Button onClick={() => setVersionOpen(false)}>Cancel</Button>
            <Button
              variant="primary"
              loading={mutation.isPending}
              disabled={!slabsValid}
              onClick={() => void submitVersion()}
            >
              Create version
            </Button>
          </>
        }
      >
        <form
          className="grid gap-4 sm:grid-cols-2"
          onSubmit={(event) => void submitVersion(event)}
        >
          <Field label="Calculation method" htmlFor="method" required>
            <Select id="method" {...form.register("calculationMethod")}>
              <option>FIXED</option>
              <option>PERCENTAGE</option>
              <option>SLAB</option>
            </Select>
          </Field>
          <Field label="Effective from" htmlFor="effective-from" required>
            <Input
              id="effective-from"
              type="date"
              {...form.register("effectiveFrom")}
            />
          </Field>
          {method === "FIXED" ? (
            <Field label="Fixed amount (₦)" htmlFor="fixed-amount" required>
              <Input
                id="fixed-amount"
                inputMode="decimal"
                {...form.register("fixedAmount")}
              />
            </Field>
          ) : null}
          {method === "PERCENTAGE" ? (
            <>
              <Field
                label="Rate (basis points)"
                htmlFor="rate-bp"
                hint="1200 = 12%"
                required
              >
                <Input
                  id="rate-bp"
                  type="number"
                  {...form.register("rateBp")}
                />
              </Field>
              <Field label="Basis" htmlFor="basis" required>
                <Select id="basis" {...form.register("basis")}>
                  {bases.map((value) => (
                    <option key={value}>{value}</option>
                  ))}
                </Select>
              </Field>
            </>
          ) : null}
          {method === "SLAB" ? (
            <div className="space-y-3 sm:col-span-2">
              <Field label="Basis" htmlFor="slab-basis" required>
                <Select id="slab-basis" {...form.register("basis")}>
                  {bases.map((value) => (
                    <option key={value}>{value}</option>
                  ))}
                </Select>
              </Field>
              <div className="overflow-x-auto rounded-md border border-border">
                <table
                  className="w-full min-w-[620px] text-sm"
                  aria-label="Commission slabs"
                >
                  <thead className="bg-slate-50 text-left text-xs uppercase text-slate-500">
                    <tr>
                      <th className="px-3 py-2">From units</th>
                      <th className="px-3 py-2">To units</th>
                      <th className="px-3 py-2">Commission minor units</th>
                      <th className="px-3 py-2">Label</th>
                      <th className="px-3 py-2">Action</th>
                    </tr>
                  </thead>
                  <tbody>
                    {slabs.map((slab, index) => (
                      <tr key={index} className="border-t border-border">
                        <td className="p-2">
                          <Input
                            aria-label={`Slab ${index + 1} from`}
                            value={slab.fromMinor}
                            onChange={(event) =>
                              setSlabs((current) =>
                                current.map((item, itemIndex) =>
                                  itemIndex === index
                                    ? { ...item, fromMinor: event.target.value }
                                    : item,
                                ),
                              )
                            }
                          />
                        </td>
                        <td className="p-2">
                          <Input
                            aria-label={`Slab ${index + 1} to`}
                            placeholder={
                              index === slabs.length - 1
                                ? "Open ended"
                                : "Required"
                            }
                            value={slab.toMinor}
                            onChange={(event) =>
                              setSlabs((current) =>
                                current.map((item, itemIndex) =>
                                  itemIndex === index
                                    ? { ...item, toMinor: event.target.value }
                                    : item,
                                ),
                              )
                            }
                          />
                        </td>
                        <td className="p-2">
                          <Input
                            aria-label={`Slab ${index + 1} amount`}
                            value={slab.amountMinor}
                            onChange={(event) =>
                              setSlabs((current) =>
                                current.map((item, itemIndex) =>
                                  itemIndex === index
                                    ? {
                                        ...item,
                                        amountMinor: event.target.value,
                                      }
                                    : item,
                                ),
                              )
                            }
                          />
                        </td>
                        <td className="p-2">
                          <Input
                            aria-label={`Slab ${index + 1} label`}
                            value={slab.label}
                            onChange={(event) =>
                              setSlabs((current) =>
                                current.map((item, itemIndex) =>
                                  itemIndex === index
                                    ? { ...item, label: event.target.value }
                                    : item,
                                ),
                              )
                            }
                          />
                        </td>
                        <td className="p-2">
                          <Button
                            type="button"
                            size="sm"
                            disabled={slabs.length === 1}
                            onClick={() =>
                              setSlabs((current) =>
                                current.filter(
                                  (_, itemIndex) => itemIndex !== index,
                                ),
                              )
                            }
                          >
                            Remove
                          </Button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <div className="flex flex-wrap items-center justify-between gap-2">
                <p className="text-xs text-slate-500">
                  Bands must start at 0, remain contiguous, and leave the last
                  “To” blank.
                </p>
                <Button
                  type="button"
                  size="sm"
                  onClick={() =>
                    setSlabs((current) => [
                      ...current,
                      {
                        fromMinor: current.at(-1)?.toMinor ?? "",
                        toMinor: "",
                        amountMinor: "",
                        label: "",
                      },
                    ])
                  }
                >
                  Add slab
                </Button>
              </div>
              {!slabsValid ? (
                <p className="text-xs font-medium text-danger">
                  Complete a contiguous table and leave only the final upper
                  bound blank.
                </p>
              ) : null}
            </div>
          ) : null}
          <Field label="Minimum (₦)" htmlFor="min-amount">
            <Input id="min-amount" {...form.register("minAmount")} />
          </Field>
          <Field label="Maximum (₦)" htmlFor="max-amount">
            <Input id="max-amount" {...form.register("maxAmount")} />
          </Field>
          <Field
            className="sm:col-span-2"
            label="Notes"
            htmlFor="version-notes"
          >
            <Textarea id="version-notes" {...form.register("notes")} />
          </Field>
          {mutation.error ? (
            <div className="sm:col-span-2">
              <ErrorState error={mutation.error} />
            </div>
          ) : null}
        </form>
      </Dialog>
    </>
  );
}

const simulationSchema = z.object({
  commissionType: z.enum(commissionTypes),
  franchiseId: z.string().optional(),
  franchiseCategory: z.string().optional(),
  customerCategory: z.string().optional(),
  paymentMode: z.string().optional(),
  on: z.string().optional(),
  freight: z.string().optional(),
  surcharge: z.string().optional(),
  total: z.string().optional(),
  cod: z.string().optional(),
  weight: z.string().optional(),
  shipments: z.string().optional(),
});
type SimulationForm = z.infer<typeof simulationSchema>;

export function CommissionSimulatorPage() {
  const [result, setResult] = useState<CommissionSimulateResult>();
  const form = useForm<SimulationForm>({
    resolver: zodResolver(simulationSchema),
    defaultValues: {
      commissionType: "DELIVERY",
      on: new Date().toISOString().slice(0, 10),
      shipments: "1",
    },
  });
  const mutation = useMutation({
    mutationFn: (value: SimulationForm) => {
      const body: CommissionSimulateRequest = {
        commissionType: value.commissionType,
        franchiseId: value.franchiseId || undefined,
        franchiseCategory: value.franchiseCategory || undefined,
        customerCategory: value.customerCategory || undefined,
        paymentMode: value.paymentMode || undefined,
        on: value.on || undefined,
        basis: {
          freightMinor: toMinorUnits(value.freight),
          surchargeMinor: toMinorUnits(value.surcharge),
          totalMinor: toMinorUnits(value.total),
          codAmountMinor: toMinorUnits(value.cod),
          chargeableWeightGrams: value.weight
            ? Number(value.weight)
            : undefined,
          shipmentCount: value.shipments ? Number(value.shipments) : undefined,
        },
      };
      return apiRequest<CommissionSimulateResult>(
        "/api/v1/commission/simulate",
        { method: "POST", body },
      );
    },
    onSuccess: setResult,
  });
  const submitSimulation = form.handleSubmit((value) => mutation.mutate(value));
  const calc = result?.calculation;
  return (
    <>
      <PageHeader
        eyebrow="Finance · Commission"
        title="Commission simulator"
        description="Uses the backend resolver and calculator without posting. Candidate rules explain exactly why the selected rule won."
      />
      <div className="grid gap-4 xl:grid-cols-[420px_minmax(0,1fr)]">
        <Panel>
          <PanelHeader
            title="Qualifying shipment"
            description="Enter backend basis inputs in rupees; the request is sent as integer minor units."
          />
          <form
            className="grid gap-4 p-4 sm:grid-cols-2 xl:grid-cols-1"
            onSubmit={(event) => void submitSimulation(event)}
          >
            <Field label="Commission type" htmlFor="sim-type" required>
              <Select id="sim-type" {...form.register("commissionType")}>
                {commissionTypes.map((value) => (
                  <option key={value}>{value}</option>
                ))}
              </Select>
            </Field>
            <Field label="Franchise ID" htmlFor="sim-franchise">
              <Input id="sim-franchise" {...form.register("franchiseId")} />
            </Field>
            <Field label="Freight (₦)" htmlFor="sim-freight">
              <Input
                id="sim-freight"
                inputMode="decimal"
                {...form.register("freight")}
              />
            </Field>
            <Field label="Surcharge (₦)" htmlFor="sim-surcharge">
              <Input
                id="sim-surcharge"
                inputMode="decimal"
                {...form.register("surcharge")}
              />
            </Field>
            <Field label="Total (₦)" htmlFor="sim-total">
              <Input
                id="sim-total"
                inputMode="decimal"
                {...form.register("total")}
              />
            </Field>
            <Field label="COD amount (₦)" htmlFor="sim-cod">
              <Input
                id="sim-cod"
                inputMode="decimal"
                {...form.register("cod")}
              />
            </Field>
            <Field label="Chargeable weight (g)" htmlFor="sim-weight">
              <Input
                id="sim-weight"
                type="number"
                {...form.register("weight")}
              />
            </Field>
            <Field label="Shipment count" htmlFor="sim-count">
              <Input
                id="sim-count"
                type="number"
                {...form.register("shipments")}
              />
            </Field>
            <Button
              variant="primary"
              type="submit"
              loading={mutation.isPending}
            >
              <Calculator className="h-4 w-4" /> Run simulation
            </Button>
            {mutation.error ? <ErrorState error={mutation.error} /> : null}
          </form>
        </Panel>
        <div className="space-y-4">
          {!result ? (
            <Panel>
              <EmptyState
                icon={Calculator}
                title="Ready to simulate"
                description="The result will show the selected rule, version, arithmetic trace, and every matching candidate."
              />
            </Panel>
          ) : !result.matched ? (
            <InlineNotice tone="warning" title="No matching commission rule">
              This is a valid backend result. Adjust the scope or configure an
              effective rule.
            </InlineNotice>
          ) : (
            <>
              <Panel>
                <PanelHeader
                  title="Selected rule and calculation"
                  description={result.explanation}
                />
                <div className="grid gap-4 p-4 sm:grid-cols-3">
                  <div>
                    <p className="text-xs text-slate-500">Rule</p>
                    <p className="font-semibold">
                      {result.ruleCode} · v{result.versionNo}
                    </p>
                    <p className="text-xs text-slate-500">{result.ruleName}</p>
                  </div>
                  <div>
                    <p className="text-xs text-slate-500">Basis</p>
                    <Money
                      amountMinor={calc?.basisValue}
                      currency={calc?.currency}
                      className="text-lg"
                    />
                    <p className="text-xs text-slate-500">
                      {titleCase(calc?.basis ?? "Unknown")}
                    </p>
                  </div>
                  <div>
                    <p className="text-xs text-slate-500">Commission</p>
                    <Money
                      amountMinor={calc?.amountMinor}
                      currency={calc?.currency}
                      className="text-xl font-bold"
                    />
                    {calc?.clamped ? (
                      <Badge tone="warning">
                        Clamped from{" "}
                        <Money
                          amountMinor={calc.grossAmountMinor}
                          currency={calc.currency}
                        />
                      </Badge>
                    ) : null}
                  </div>
                </div>
                <ol
                  className="border-t border-border p-4"
                  aria-label="Calculation trace"
                >
                  {calc?.trace?.map((step, index) => (
                    <li
                      key={`${step.description}-${index}`}
                      className="grid gap-1 border-b border-border py-3 last:border-0 sm:grid-cols-[1fr_auto]"
                    >
                      <div>
                        <p className="text-sm font-medium">
                          {step.description}
                        </p>
                        <p className="text-xs text-slate-500">{step.detail}</p>
                      </div>
                      <Money
                        amountMinor={step.value}
                        currency={calc.currency}
                      />
                    </li>
                  ))}
                </ol>
              </Panel>
              <Panel>
                <PanelHeader
                  title="Rule candidates"
                  description="Selected rule is marked; specificity wins before priority."
                />
                <DataTable label="Commission candidates">
                  <thead>
                    <tr>
                      <TableHead>Rule</TableHead>
                      <TableHead>Specificity</TableHead>
                      <TableHead>Priority</TableHead>
                      <TableHead>Decision</TableHead>
                    </tr>
                  </thead>
                  <tbody>
                    {result.candidates?.map((candidate) => (
                      <tr key={candidate.code}>
                        <TableCell>
                          {candidate.code}
                          <span className="block text-xs text-slate-500">
                            {candidate.name}
                          </span>
                        </TableCell>
                        <TableCell>{candidate.specificity}</TableCell>
                        <TableCell>{candidate.priority}</TableCell>
                        <TableCell>
                          {candidate.selected ? (
                            <Badge tone="success">Selected</Badge>
                          ) : (
                            <Badge>Not selected</Badge>
                          )}
                        </TableCell>
                      </tr>
                    ))}
                  </tbody>
                </DataTable>
              </Panel>
            </>
          )}
        </div>
      </div>
    </>
  );
}

export function CommissionEntriesPage() {
  const [status, setStatus] = useState("");
  const query = useQuery({
    queryKey: ["commission-calculations", status],
    queryFn: () =>
      apiRequest<CommissionCalculationListResponse>(
        `/api/v1/commission/calculations${queryString({ status, limit: 50 })}`,
      ),
  });
  const rows = query.data?.data ?? [];
  return (
    <>
      <PageHeader
        eyebrow="Finance · Commission"
        title="Commission entries"
        description="Immutable calculation register with the exact rule version and qualifying event behind every amount."
      />
      <Panel>
        <FilterBar>
          <Select
            aria-label="Entry status"
            className="w-48"
            value={status}
            onChange={(event) => setStatus(event.target.value)}
          >
            <option value="">All states</option>
            <option>CALCULATED</option>
            <option>POSTED</option>
            <option>REVERSED</option>
            <option>CANCELLED</option>
          </Select>
        </FilterBar>
        {query.isPending ? (
          <LoadingState label="Loading commission entries" />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : rows.length === 0 ? (
          <EmptyState
            title="No commission entries"
            description="No calculations match this state."
          />
        ) : (
          <DataTable label="Commission entries">
            <thead>
              <tr>
                <TableHead>Reference</TableHead>
                <TableHead>Shipment / event</TableHead>
                <TableHead>Recipient</TableHead>
                <TableHead>Rule version</TableHead>
                <TableHead>Amount</TableHead>
                <TableHead>Status</TableHead>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <CommissionRow key={row.id} row={row} />
              ))}
            </tbody>
          </DataTable>
        )}
      </Panel>
    </>
  );
}

function CommissionRow({ row }: { row: CommissionCalculation }) {
  return (
    <tr>
      <TableCell>
        <Link
          className="font-mono text-xs font-semibold text-primary hover:underline"
          to={`/finance/commission/entries/${row.id}`}
        >
          {row.id}
        </Link>
        <span className="block text-xs text-slate-500">
          {formatDateTime(row.qualifiedAt)}
        </span>
      </TableCell>
      <TableCell>
        {row.awb ? (
          <ReferenceLink
            to={`/shipments?search=${encodeURIComponent(row.awb)}`}
            type="shipment"
            reference={row.awb}
          />
        ) : (
          "—"
        )}
        <span className="block text-xs text-slate-500">
          {row.qualifyingEvent}
        </span>
      </TableCell>
      <TableCell>
        {row.franchiseCode ?? titleCase(row.recipientType ?? "Unknown")}
      </TableCell>
      <TableCell>
        {row.ruleCode} · v{row.ruleVersionNo}
        <span className="block text-xs text-slate-500">
          {titleCase(row.calculationMethod ?? "Unknown")}
        </span>
      </TableCell>
      <TableCell>
        <Money amountMinor={row.amountMinor} currency={row.currency} />
      </TableCell>
      <TableCell>
        <FinancialStatus status={row.status} />
      </TableCell>
    </tr>
  );
}

export function CommissionEntryDetailPage() {
  const { calculationId = "" } = useParams();
  const { hasPermission } = useAuth();
  const queryClient = useQueryClient();
  const [postOpen, setPostOpen] = useState(false);
  const [reverseOpen, setReverseOpen] = useState(false);
  const [reason, setReason] = useState("");
  const query = useQuery({
    queryKey: ["commission-calculation", calculationId],
    queryFn: () =>
      apiRequest<CommissionCalculation>(
        `/api/v1/commission/calculations/${calculationId}`,
      ),
    enabled: Boolean(calculationId),
  });
  const action = useMutation({
    mutationFn: ({ kind }: { kind: "post" | "reverse" }) =>
      apiRequest<CommissionCalculation>(
        `/api/v1/commission/calculations/${calculationId}/${kind}`,
        { method: "POST", ...(kind === "reverse" ? { body: { reason } } : {}) },
      ),
    onSuccess: () => {
      setPostOpen(false);
      setReverseOpen(false);
      setReason("");
      void queryClient.invalidateQueries({
        queryKey: ["commission-calculation", calculationId],
      });
      void queryClient.invalidateQueries({
        queryKey: ["commission-calculations"],
      });
    },
  });
  if (query.isPending)
    return <LoadingState label="Loading commission calculation" />;
  if (query.error)
    return (
      <ErrorState error={query.error} retry={() => void query.refetch()} />
    );
  const row = query.data;
  return (
    <>
      <PageHeader
        eyebrow="Finance · Commission entry"
        title={
          row?.awb ? `Commission for ${row.awb}` : "Commission calculation"
        }
        description={`Rule ${row?.ruleCode ?? "—"} · version ${row?.ruleVersionNo ?? "—"}`}
        actions={
          <>
            <FinancialStatus status={row?.status} />
            {row?.status === "CALCULATED" &&
            hasPermission("commission.post") ? (
              <Button variant="primary" onClick={() => setPostOpen(true)}>
                Post to ledger
              </Button>
            ) : null}
            {row?.status === "POSTED" && hasPermission("commission.reverse") ? (
              <Button variant="danger" onClick={() => setReverseOpen(true)}>
                Reverse commission
              </Button>
            ) : null}
          </>
        }
      />
      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_340px]">
        <Panel>
          <PanelHeader
            title="Calculation trace"
            description="Backend-authored arithmetic shown verbatim."
          />
          <ol className="divide-y divide-border p-4">
            {row?.calculationTrace?.map((step, index) => (
              <li
                key={index}
                className="grid gap-2 py-3 sm:grid-cols-[1fr_auto]"
              >
                <div>
                  <p className="font-medium">{step.description}</p>
                  <p className="text-xs text-slate-500">{step.detail}</p>
                </div>
                <Money amountMinor={step.value} currency={row.currency} />
              </li>
            ))}
          </ol>
        </Panel>
        <Panel>
          <PanelHeader title="Result" />
          <dl className="divide-y divide-border">
            {[
              ["Qualifying event", row?.qualifyingEvent],
              ["Method", titleCase(row?.calculationMethod ?? "Unknown")],
              ["Basis", titleCase(row?.basis ?? "Unknown")],
              [
                "Rate",
                row?.rateBp != null ? `${(row.rateBp / 100).toFixed(2)}%` : "—",
              ],
            ].map(([label, value]) => (
              <div
                key={String(label)}
                className="flex justify-between gap-3 px-4 py-3 text-sm"
              >
                <dt className="text-slate-500">{label}</dt>
                <dd className="font-medium">{value}</dd>
              </div>
            ))}
          </dl>
          <div className="border-t border-border bg-slate-950 p-4 text-white">
            <p className="text-xs text-slate-300">Commission</p>
            <Money
              amountMinor={row?.amountMinor}
              currency={row?.currency}
              className="text-2xl font-bold text-white"
            />
          </div>
        </Panel>
      </div>
      <ConfirmAction
        open={postOpen}
        onOpenChange={setPostOpen}
        title="Post this commission?"
        description="This creates immutable ledger entries. Future correction requires a reversal; the amount is not editable after posting."
        confirmLabel="Post commission"
        loading={action.isPending}
        onConfirm={() => action.mutate({ kind: "post" })}
      >
        {action.error ? (
          <ErrorState error={action.error} />
        ) : (
          <InlineNotice title="Commission payable accrual">
            Posting credits Commission Payable. It moves to the franchise
            payable only when a settlement is approved.
          </InlineNotice>
        )}
      </ConfirmAction>
      <ConfirmAction
        open={reverseOpen}
        onOpenChange={setReverseOpen}
        title="Reverse posted commission?"
        description="This writes a negative commission entry and mirrors the ledger posting. Both records remain visible."
        confirmLabel="Post reversal"
        loading={action.isPending}
        onConfirm={() => action.mutate({ kind: "reverse" })}
      >
        <Field
          label="Substantive reversal reason"
          htmlFor="commission-reverse-reason"
          required
        >
          <Textarea
            id="commission-reverse-reason"
            value={reason}
            onChange={(event) => setReason(event.target.value)}
          />
        </Field>
        {action.error ? <ErrorState error={action.error} /> : null}
      </ConfirmAction>
    </>
  );
}
