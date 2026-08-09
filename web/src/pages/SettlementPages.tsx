import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  CheckCheck,
  FileClock,
  Landmark,
  LockKeyhole,
  Plus,
} from "lucide-react";
import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useAuth } from "../auth/AuthProvider";
import {
  apiRequest,
  queryString,
  type Settlement,
  type SettlementDetailResponse,
  type SettlementGenerateRequest,
  type SettlementListResponse,
  type SettlementPaymentRequest,
  type SettlementResult,
} from "../api/client";
import {
  ApprovalTimeline,
  FinancialStatus,
  FinancialSummary,
  ImmutableBadge,
  Money,
  ReferenceLink,
} from "../components/financial";
import {
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
  Panel,
  PanelHeader,
  Select,
  TabContent,
  TableCell,
  TableHead,
  Tabs,
  Textarea,
} from "../components/ui";
import { titleCase, toMinorUnits } from "../lib/utils";

const settlementStatuses = [
  "DRAFT",
  "CALCULATED",
  "UNDER_REVIEW",
  "APPROVED",
  "PARTIALLY_PAID",
  "PAID",
  "CLOSED",
  "CANCELLED",
] as const;

function GenerateSettlementDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const queryClient = useQueryClient();
  const [franchiseId, setFranchiseId] = useState("");
  const [periodType, setPeriodType] =
    useState<SettlementGenerateRequest["periodType"]>("MONTHLY");
  const [periodStart, setPeriodStart] = useState("");
  const [periodEnd, setPeriodEnd] = useState("");
  const [notes, setNotes] = useState("");
  const [result, setResult] = useState<SettlementResult>();
  const mutation = useMutation({
    mutationFn: () => {
      const body: SettlementGenerateRequest = {
        franchiseId,
        periodType,
        periodStart,
        periodEnd,
        notes: notes || undefined,
      };
      return apiRequest<SettlementResult>("/api/v1/settlements", {
        method: "POST",
        body,
      });
    },
    onSuccess: (value) => {
      setResult(value);
      void queryClient.invalidateQueries({ queryKey: ["settlements"] });
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Generate settlement"
      description="The backend selects immutable commission, COD, charge, tax, and opening-balance sources for this franchise and period."
      footer={
        result ? (
          <Button variant="primary" onClick={() => onOpenChange(false)}>
            Done
          </Button>
        ) : (
          <>
            <Button onClick={() => onOpenChange(false)}>Cancel</Button>
            <Button
              variant="primary"
              loading={mutation.isPending}
              disabled={!franchiseId || !periodStart || !periodEnd}
              onClick={() => mutation.mutate()}
            >
              Calculate settlement
            </Button>
          </>
        )
      }
    >
      {result ? (
        <InlineNotice
          tone={result.replayed ? "info" : "success"}
          title={
            result.replayed
              ? "Existing settlement returned"
              : "Settlement calculated"
          }
        >
          {result.replayed
            ? "Nothing new was created. The existing period statement is shown below."
            : "The backend generated the period statement without a browser-calculated total."}
          <div className="mt-3">
            <Link
              className="font-semibold underline"
              to={`/finance/settlements/${result.settlement?.id}`}
            >
              Open {result.settlement?.settlementNumber}
            </Link>
          </div>
        </InlineNotice>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Franchise ID" htmlFor="settlement-franchise" required>
            <Input
              id="settlement-franchise"
              value={franchiseId}
              onChange={(event) => setFranchiseId(event.target.value)}
            />
          </Field>
          <Field label="Period type" htmlFor="settlement-period">
            <Select
              id="settlement-period"
              value={periodType}
              onChange={(event) =>
                setPeriodType(
                  event.target.value as SettlementGenerateRequest["periodType"],
                )
              }
            >
              <option>WEEKLY</option>
              <option>FORTNIGHTLY</option>
              <option>MONTHLY</option>
              <option>CUSTOM</option>
            </Select>
          </Field>
          <Field label="Period start" htmlFor="settlement-start" required>
            <Input
              id="settlement-start"
              type="date"
              value={periodStart}
              onChange={(event) => setPeriodStart(event.target.value)}
            />
          </Field>
          <Field label="Period end" htmlFor="settlement-end" required>
            <Input
              id="settlement-end"
              type="date"
              value={periodEnd}
              onChange={(event) => setPeriodEnd(event.target.value)}
            />
          </Field>
          <Field
            className="sm:col-span-2"
            label="Notes"
            htmlFor="settlement-notes"
          >
            <Textarea
              id="settlement-notes"
              value={notes}
              onChange={(event) => setNotes(event.target.value)}
            />
          </Field>
          {mutation.error ? (
            <div className="sm:col-span-2">
              <ErrorState error={mutation.error} />
            </div>
          ) : null}
        </div>
      )}
    </Dialog>
  );
}

export function SettlementsPage() {
  const { hasPermission } = useAuth();
  const [status, setStatus] = useState("");
  const [generateOpen, setGenerateOpen] = useState(false);
  const query = useQuery({
    queryKey: ["settlements", status],
    queryFn: () =>
      apiRequest<SettlementListResponse>(
        `/api/v1/settlements${queryString({ status, limit: 50 })}`,
      ),
  });
  const rows = query.data?.data ?? [];
  return (
    <>
      <PageHeader
        eyebrow="Finance · Franchise"
        title="Settlements"
        description="Period statements make every source traceable and state clearly who pays whom. Approved statements are frozen."
        actions={
          hasPermission("settlement.calculate") ? (
            <Button variant="primary" onClick={() => setGenerateOpen(true)}>
              <Plus className="h-4 w-4" /> Generate settlement
            </Button>
          ) : undefined
        }
      />
      <Panel>
        <FilterBar>
          <Select
            aria-label="Settlement status"
            className="w-52"
            value={status}
            onChange={(event) => setStatus(event.target.value)}
          >
            <option value="">All settlement states</option>
            {settlementStatuses.map((value) => (
              <option key={value}>{titleCase(value)}</option>
            ))}
          </Select>
        </FilterBar>
        {query.isPending ? (
          <LoadingState label="Loading settlements" />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : rows.length === 0 ? (
          <EmptyState
            icon={Landmark}
            title="No settlements"
            description="No franchise period statements match this state."
          />
        ) : (
          <DataTable label="Settlements">
            <thead>
              <tr>
                <TableHead>Settlement</TableHead>
                <TableHead>Period</TableHead>
                <TableHead>Franchise</TableHead>
                <TableHead className="text-right">Commission</TableHead>
                <TableHead className="text-right">COD liability</TableHead>
                <TableHead className="text-right">Adjustments</TableHead>
                <TableHead className="text-right">Net / direction</TableHead>
                <TableHead>Status</TableHead>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={row.id}>
                  <TableCell>
                    <Link
                      className="font-semibold text-primary hover:underline"
                      to={`/finance/settlements/${row.id}`}
                    >
                      {row.settlementNumber}
                    </Link>
                  </TableCell>
                  <TableCell>
                    {row.periodStart} – {row.periodEnd}
                    <span className="block text-xs text-slate-500">
                      {titleCase(row.periodType ?? "Custom")}
                    </span>
                  </TableCell>
                  <TableCell>
                    {row.franchiseName}
                    <span className="block text-xs text-slate-500">
                      {row.franchiseCode}
                    </span>
                  </TableCell>
                  <TableCell className="text-right">
                    <Money
                      amountMinor={row.commissionMinor}
                      currency={row.currency}
                    />
                  </TableCell>
                  <TableCell className="text-right">
                    <Money
                      amountMinor={row.codLiabilityMinor}
                      currency={row.currency}
                      showPositiveSign
                    />
                  </TableCell>
                  <TableCell className="text-right">
                    <Money
                      amountMinor={row.adjustmentsMinor}
                      currency={row.currency}
                      showPositiveSign
                    />
                  </TableCell>
                  <TableCell className="text-right">
                    <Money
                      amountMinor={row.netAmountMinor}
                      currency={row.currency}
                      className="font-bold"
                      showPositiveSign
                    />
                    <span className="block text-xs text-slate-500">
                      {(row.netAmountMinor ?? 0) >= 0
                        ? "Head Office pays Franchise"
                        : "Franchise pays Head Office"}
                    </span>
                  </TableCell>
                  <TableCell>
                    <FinancialStatus status={row.status} />
                  </TableCell>
                </tr>
              ))}
            </tbody>
          </DataTable>
        )}
      </Panel>
      <GenerateSettlementDialog
        open={generateOpen}
        onOpenChange={setGenerateOpen}
      />
    </>
  );
}

function settlementReference(
  sourceType?: string,
  publicId?: string | null,
  awb?: string | null,
) {
  if (sourceType === "COMMISSION_CALCULATION" && publicId)
    return {
      to: `/finance/commission/entries/${publicId}`,
      reference: publicId,
      type: "commission calculation",
    };
  if (sourceType === "COD_OBLIGATION" && publicId)
    return {
      to: `/finance/cod/obligations/${publicId}`,
      reference: awb ?? publicId,
      type: "COD obligation",
    };
  if (awb)
    return {
      to: `/shipments?search=${encodeURIComponent(awb)}`,
      reference: awb,
      type: "shipment",
    };
  return undefined;
}

export function SettlementDetailPage() {
  const { settlementId = "" } = useParams();
  const { hasPermission } = useAuth();
  const queryClient = useQueryClient();
  const [tab, setTab] = useState("summary");
  const [approveOpen, setApproveOpen] = useState(false);
  const [cancelOpen, setCancelOpen] = useState(false);
  const [payOpen, setPayOpen] = useState(false);
  const [comment, setComment] = useState("");
  const [reason, setReason] = useState("");
  const [paymentAmount, setPaymentAmount] = useState("");
  const [paymentMode, setPaymentMode] =
    useState<SettlementPaymentRequest["paymentMode"]>("BANK_TRANSFER");
  const [paymentReference, setPaymentReference] = useState("");
  const query = useQuery({
    queryKey: ["settlement", settlementId],
    queryFn: () =>
      apiRequest<SettlementDetailResponse>(
        `/api/v1/settlements/${settlementId}`,
      ),
    enabled: Boolean(settlementId),
  });
  const refresh = () => {
    void queryClient.invalidateQueries({
      queryKey: ["settlement", settlementId],
    });
    void queryClient.invalidateQueries({ queryKey: ["settlements"] });
  };
  const action = useMutation({
    mutationFn: ({ path, body }: { path: string; body?: unknown }) =>
      apiRequest<Settlement>(`/api/v1/settlements/${settlementId}/${path}`, {
        method: "POST",
        body,
      }),
    onSuccess: () => {
      setApproveOpen(false);
      setCancelOpen(false);
      setComment("");
      setReason("");
      refresh();
    },
  });
  const pay = useMutation({
    mutationFn: () => {
      const body: SettlementPaymentRequest = {
        amountMinor: toMinorUnits(paymentAmount) ?? 0,
        paymentMode,
        reference: paymentReference || undefined,
        paidOn: new Date().toISOString().slice(0, 10),
      };
      return apiRequest(`/api/v1/settlements/${settlementId}/payments`, {
        method: "POST",
        body,
      });
    },
    onSuccess: () => {
      setPayOpen(false);
      setPaymentAmount("");
      setPaymentReference("");
      refresh();
    },
  });
  if (query.isPending) return <LoadingState label="Loading settlement" />;
  if (query.error)
    return (
      <ErrorState error={query.error} retry={() => void query.refetch()} />
    );
  const settlement = query.data?.settlement;
  const lines = query.data?.lines ?? [];
  if (!settlement)
    return (
      <EmptyState
        title="Settlement unavailable"
        description="The backend did not return this settlement."
      />
    );
  const frozen = [
    "APPROVED",
    "PARTIALLY_PAID",
    "PAID",
    "CLOSED",
    "CANCELLED",
  ].includes(settlement.status ?? "");
  const direction =
    (settlement.netAmountMinor ?? 0) > 0
      ? "HEAD_OFFICE_PAYS_FRANCHISE"
      : (settlement.netAmountMinor ?? 0) < 0
        ? "FRANCHISE_PAYS_HEAD_OFFICE"
        : "NO_PAYMENT_DUE";
  const grouped = {
    commission: lines.filter(
      (line) =>
        line.category?.includes("COMMISSION") ||
        [
          "ORIGIN_HANDLING",
          "DESTINATION_HANDLING",
          "VOLUME_INCENTIVE",
          "INCENTIVE",
        ].includes(line.category ?? ""),
    ),
    cod: lines.filter((line) => line.category === "COD_LIABILITY"),
    charges: lines.filter((line) =>
      ["CHARGE", "PENALTY", "TAX", "WITHHOLDING"].includes(line.category ?? ""),
    ),
    adjustments: lines.filter((line) =>
      ["ADJUSTMENT", "OPENING_BALANCE"].includes(line.category ?? ""),
    ),
  };
  const renderLines = (selected: typeof lines) =>
    selected.length ? (
      <DataTable label="Settlement source lines">
        <thead>
          <tr>
            <TableHead>Line</TableHead>
            <TableHead>Category / description</TableHead>
            <TableHead>Shipment / source</TableHead>
            <TableHead>Quantity</TableHead>
            <TableHead className="text-right">Amount</TableHead>
          </tr>
        </thead>
        <tbody>
          {selected.map((line) => {
            const ref = settlementReference(
              line.sourceType,
              line.sourcePublicId,
              line.awb,
            );
            return (
              <tr key={line.id}>
                <TableCell>{line.lineNo}</TableCell>
                <TableCell>
                  {titleCase(line.category ?? "Unknown")}
                  <span className="block text-xs text-slate-500">
                    {line.description}
                  </span>
                </TableCell>
                <TableCell>
                  {ref ? (
                    <ReferenceLink {...ref} />
                  ) : (
                    <span className="font-mono text-xs">
                      {line.sourceType} {line.sourcePublicId}
                    </span>
                  )}
                </TableCell>
                <TableCell>{line.quantity ?? 1}</TableCell>
                <TableCell className="text-right">
                  <Money
                    amountMinor={line.amountMinor}
                    currency={line.currency}
                    showPositiveSign
                  />
                </TableCell>
              </tr>
            );
          })}
        </tbody>
      </DataTable>
    ) : (
      <EmptyState
        title="No lines in this category"
        description="The backend returned no source lines for this section."
      />
    );
  return (
    <>
      <PageHeader
        eyebrow="Finance · Settlement"
        title={settlement.settlementNumber ?? "Settlement"}
        description={`${settlement.franchiseName ?? "Franchise"} · ${settlement.periodStart} to ${settlement.periodEnd}`}
        actions={
          <>
            {frozen ? (
              <ImmutableBadge
                label={`${titleCase(settlement.status ?? "Frozen")} · Read only`}
              />
            ) : (
              <FinancialStatus status={settlement.status} />
            )}
            {!frozen && hasPermission("settlement.calculate") ? (
              <Button
                onClick={() => action.mutate({ path: "recalculate" })}
                loading={action.isPending}
              >
                Recalculate
              </Button>
            ) : null}
            {settlement.status === "CALCULATED" &&
            hasPermission("settlement.submit") ? (
              <Button
                variant="primary"
                onClick={() =>
                  action.mutate({
                    path: "submit",
                    body: { comment: "Submitted for finance review" },
                  })
                }
              >
                Submit for review
              </Button>
            ) : null}
            {settlement.status === "UNDER_REVIEW" &&
            hasPermission("settlement.approve") ? (
              <Button variant="primary" onClick={() => setApproveOpen(true)}>
                <CheckCheck className="h-4 w-4" /> Approve
              </Button>
            ) : null}
            {["APPROVED", "PARTIALLY_PAID"].includes(settlement.status ?? "") &&
            hasPermission("settlement.pay") ? (
              <Button variant="primary" onClick={() => setPayOpen(true)}>
                Record payment
              </Button>
            ) : null}
            {!frozen && hasPermission("settlement.cancel") ? (
              <Button variant="danger" onClick={() => setCancelOpen(true)}>
                Cancel
              </Button>
            ) : null}
          </>
        }
      />
      {frozen ? (
        <InlineNotice title="Financial statement is immutable" tone="warning">
          <LockKeyhole className="inline h-4 w-4" /> Totals and source lines
          cannot be recalculated or edited. Corrections require the backend
          settlement-adjustment workflow.
        </InlineNotice>
      ) : null}
      <div className="mt-4">
        <Tabs
          value={tab}
          onValueChange={setTab}
          label="Settlement sections"
          items={[
            "summary",
            "commission",
            "cod",
            "charges",
            "adjustments",
            "ledger",
            "approval",
            "payments",
            "audit",
          ].map((value) => ({ value, label: titleCase(value) }))}
        >
          <TabContent value="summary" className="pt-4">
            <FinancialSummary
              direction={direction}
              totalMinor={Math.abs(settlement.netAmountMinor ?? 0)}
              currency={settlement.currency}
              items={[
                {
                  label: "Commission",
                  amountMinor: settlement.commissionMinor,
                },
                { label: "Incentives", amountMinor: settlement.incentiveMinor },
                {
                  label: "COD liability",
                  amountMinor: settlement.codLiabilityMinor,
                  description: "COD held reduces what Head Office owes.",
                },
                { label: "Charges", amountMinor: settlement.chargesMinor },
                { label: "Penalties", amountMinor: settlement.penaltiesMinor },
                {
                  label: "Adjustments",
                  amountMinor: settlement.adjustmentsMinor,
                },
                { label: "Tax", amountMinor: settlement.taxMinor },
                {
                  label: "Withholding",
                  amountMinor: settlement.withholdingMinor,
                },
                {
                  label: "Opening balance",
                  amountMinor: settlement.openingBalanceMinor,
                },
              ]}
            />
            <div className="mt-4 rounded-md border border-border bg-slate-50 p-3 text-xs text-slate-600">
              <strong>Calculation hash:</strong>{" "}
              <code className="break-all">
                {settlement.calculationHash ?? "Not calculated"}
              </code>
            </div>
          </TabContent>
          <TabContent value="commission" className="pt-4">
            <Panel>{renderLines(grouped.commission)}</Panel>
          </TabContent>
          <TabContent value="cod" className="pt-4">
            <Panel>{renderLines(grouped.cod)}</Panel>
          </TabContent>
          <TabContent value="charges" className="pt-4">
            <Panel>{renderLines(grouped.charges)}</Panel>
          </TabContent>
          <TabContent value="adjustments" className="pt-4">
            <Panel>{renderLines(grouped.adjustments)}</Panel>
          </TabContent>
          <TabContent value="ledger" className="pt-4">
            <Panel>
              <EmptyState
                icon={Landmark}
                title="Ledger references not contracted"
                description="Settlement source lines are traceable, but Release 3 settlement detail does not expose posted journal public IDs."
              />
            </Panel>
          </TabContent>
          <TabContent value="approval" className="pt-4">
            <Panel className="p-4">
              <ApprovalTimeline
                steps={[
                  {
                    id: "calculated",
                    label: "Calculated",
                    status: settlement.calculatedBy ? "COMPLETE" : "PENDING",
                    actor: settlement.calculatedBy
                      ? `User ${settlement.calculatedBy}`
                      : undefined,
                  },
                  {
                    id: "review",
                    label: "Submitted for review",
                    status: ["UNDER_REVIEW"].includes(settlement.status ?? "")
                      ? "CURRENT"
                      : [
                            "APPROVED",
                            "PARTIALLY_PAID",
                            "PAID",
                            "CLOSED",
                          ].includes(settlement.status ?? "")
                        ? "COMPLETE"
                        : "PENDING",
                  },
                  {
                    id: "approved",
                    label: "Approved and posted",
                    status: settlement.approvedBy
                      ? "COMPLETE"
                      : settlement.status === "UNDER_REVIEW"
                        ? "CURRENT"
                        : "PENDING",
                    actor: settlement.approvedBy
                      ? `User ${settlement.approvedBy}`
                      : undefined,
                  },
                ]}
              />
            </Panel>
          </TabContent>
          <TabContent value="payments" className="pt-4">
            <Panel>
              <PanelHeader title="Payment position" />
              <div className="grid gap-px bg-border sm:grid-cols-3">
                <div className="bg-white p-4">
                  <p className="text-xs text-slate-500">Net</p>
                  <Money
                    amountMinor={settlement.netAmountMinor}
                    currency={settlement.currency}
                    className="text-lg font-bold"
                  />
                </div>
                <div className="bg-white p-4">
                  <p className="text-xs text-slate-500">Paid</p>
                  <Money
                    amountMinor={settlement.paidMinor}
                    currency={settlement.currency}
                    className="text-lg font-bold"
                  />
                </div>
                <div className="bg-white p-4">
                  <p className="text-xs text-slate-500">State</p>
                  <FinancialStatus status={settlement.status} />
                </div>
              </div>
              <InlineNotice title="Payment audit fields are incomplete">
                OpenAPI exposes opaque payment records on settlement detail. The
                UI does not guess their reference, mode, actor, or timestamp
                fields.
              </InlineNotice>
            </Panel>
          </TabContent>
          <TabContent value="audit" className="pt-4">
            <Panel>
              <EmptyState
                icon={FileClock}
                title="Audit events not exposed"
                description="The Release 3 settlement response has no typed audit-event collection. State, maker, checker, source lines, and hash remain visible."
              />
            </Panel>
          </TabContent>
        </Tabs>
      </div>
      {action.error ? (
        <div className="mt-4">
          <ErrorState error={action.error} />
        </div>
      ) : null}
      <ConfirmAction
        open={approveOpen}
        onOpenChange={setApproveOpen}
        title="Approve and post this settlement?"
        description={`${direction === "HEAD_OFFICE_PAYS_FRANCHISE" ? "Head Office will owe the Franchise" : direction === "FRANCHISE_PAYS_HEAD_OFFICE" ? "The Franchise will owe Head Office" : "No payment will be due"}. Approval freezes every total and line.`}
        confirmLabel="Approve settlement"
        loading={action.isPending}
        onConfirm={() => action.mutate({ path: "approve", body: { comment } })}
      >
        <Field label="Approval comment" htmlFor="approval-comment">
          <Textarea
            id="approval-comment"
            value={comment}
            onChange={(event) => setComment(event.target.value)}
          />
        </Field>
        <FinancialSummary
          direction={direction}
          totalMinor={Math.abs(settlement.netAmountMinor ?? 0)}
          currency={settlement.currency}
          items={[]}
        />
      </ConfirmAction>
      <ConfirmAction
        open={cancelOpen}
        onOpenChange={setCancelOpen}
        title="Cancel this settlement?"
        description="Cancellation releases swept commission back to the pool. It does not delete the statement."
        confirmLabel="Cancel settlement"
        loading={action.isPending}
        onConfirm={() => action.mutate({ path: "cancel", body: { reason } })}
      >
        <Field label="Substantive reason" htmlFor="cancel-reason" required>
          <Textarea
            id="cancel-reason"
            value={reason}
            onChange={(event) => setReason(event.target.value)}
          />
        </Field>
      </ConfirmAction>
      <Dialog
        open={payOpen}
        onOpenChange={setPayOpen}
        title="Record settlement payment"
        description={
          direction === "HEAD_OFFICE_PAYS_FRANCHISE"
            ? "Outbound: Head Office pays Franchise."
            : "Inbound: Franchise pays Head Office."
        }
        footer={
          <>
            <Button onClick={() => setPayOpen(false)}>Cancel</Button>
            <Button
              variant="primary"
              loading={pay.isPending}
              disabled={!paymentAmount}
              onClick={() => pay.mutate()}
            >
              Record payment
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <InlineNotice
            title={
              direction === "HEAD_OFFICE_PAYS_FRANCHISE"
                ? "Head Office pays Franchise"
                : "Franchise pays Head Office"
            }
          >
            The backend refuses overpayment and advances the settlement to
            Partially Paid or Paid.
          </InlineNotice>
          <Field label="Amount (₦)" htmlFor="settlement-payment" required>
            <Input
              id="settlement-payment"
              inputMode="decimal"
              value={paymentAmount}
              onChange={(event) => setPaymentAmount(event.target.value)}
            />
          </Field>
          <Field label="Payment mode" htmlFor="settlement-payment-mode">
            <Select
              id="settlement-payment-mode"
              value={paymentMode}
              onChange={(event) =>
                setPaymentMode(
                  event.target.value as SettlementPaymentRequest["paymentMode"],
                )
              }
            >
              <option>BANK_TRANSFER</option>
              <option>UPI</option>
              <option>CHEQUE</option>
              <option>CASH</option>
              <option>ADJUSTMENT</option>
              <option>OTHER</option>
            </Select>
          </Field>
          <Field label="Reference" htmlFor="settlement-reference">
            <Input
              id="settlement-reference"
              value={paymentReference}
              onChange={(event) => setPaymentReference(event.target.value)}
            />
          </Field>
          {pay.error ? <ErrorState error={pay.error} /> : null}
        </div>
      </Dialog>
    </>
  );
}
