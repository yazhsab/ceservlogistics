import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  Banknote,
  CheckCircle2,
  Landmark,
  Scale,
} from "lucide-react";
import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useAuth } from "../auth/AuthProvider";
import {
  apiRequest,
  queryString,
  type CODCompleteReconciliationRequest,
  type CODCountRequest,
  type CODCustodyPosition,
  type CODObligationDetailResponse,
  type CODObligationListResponse,
  type CODReconciliationRequest,
  type CODReconciliationResult,
  type CODSummary,
} from "../api/client";
import {
  FinancialStatus,
  ImmutableBadge,
  Money,
  ReferenceLink,
} from "../components/financial";
import { OperationalMetricStrip } from "../components/operations";
import {
  Button,
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
  TableCell,
  TableHead,
  Textarea,
} from "../components/ui";
import { formatDateTime, titleCase, toMinorUnits } from "../lib/utils";

const codStatuses = [
  "EXPECTED",
  "AGENT_COLLECTED",
  "BRANCH_RECEIVED",
  "FRANCHISE_CONFIRMED",
  "RECONCILED",
  "REMITTED",
  "CLOSED",
  "CANCELLED",
  "WRITTEN_OFF",
] as const;

export function CODControlCenterPage() {
  const { hasPermission } = useAuth();
  const [status, setStatus] = useState("");
  const [awb, setAwb] = useState("");
  const [reconcileOpen, setReconcileOpen] = useState(false);
  const [custodyOpen, setCustodyOpen] = useState(false);
  const summary = useQuery({
    queryKey: ["cod-summary"],
    queryFn: () => apiRequest<CODSummary>("/api/v1/cod/summary"),
    staleTime: 30_000,
  });
  const obligations = useQuery({
    queryKey: ["cod-obligations", status, awb],
    queryFn: () =>
      apiRequest<CODObligationListResponse>(
        `/api/v1/cod/obligations${queryString({ status, awb, limit: 50 })}`,
      ),
  });
  const rows = obligations.data?.data ?? [];
  const currency = summary.data?.currency ?? "NGN";
  return (
    <>
      <PageHeader
        eyebrow="Finance · COD"
        title="COD control centre"
        description="Cash custody and accounting are shown together. The current holder remains liable until the next party accepts the hand-off."
        actions={
          <>
            {hasPermission("cod.read") ? (
              <Button onClick={() => setCustodyOpen(true)}>
                <Scale className="h-4 w-4" /> Check custody
              </Button>
            ) : null}
            {hasPermission("cod.reconcile") ? (
              <Button variant="primary" onClick={() => setReconcileOpen(true)}>
                <Landmark className="h-4 w-4" /> Reconcile custody
              </Button>
            ) : null}
          </>
        }
      />
      {summary.error ? (
        <ErrorState
          error={summary.error}
          retry={() => void summary.refetch()}
        />
      ) : (
        <OperationalMetricStrip
          items={[
            {
              label: "Expected COD",
              value: (
                <Money
                  amountMinor={summary.data?.expectedMinor}
                  currency={currency}
                />
              ),
              icon: Banknote,
            },
            {
              label: "Currently in custody",
              value: (
                <Money
                  amountMinor={summary.data?.inCustodyMinor}
                  currency={currency}
                />
              ),
              tone: "warning",
            },
            {
              label: "Remitted",
              value: (
                <Money
                  amountMinor={summary.data?.remittedMinor}
                  currency={currency}
                />
              ),
              tone: "success",
            },
            { label: "Agent held", value: summary.data?.withAgentCount ?? "—" },
            {
              label: "Branch held",
              value: summary.data?.withBranchCount ?? "—",
            },
            {
              label: "Franchise outstanding",
              value: summary.data?.withFranchiseCount ?? "—",
              tone: "warning",
            },
          ]}
        />
      )}
      <InlineNotice title="Available COD records">
        This page lists COD obligations and summary totals. Separate registers
        for transfers, reconciliations, remittances, adjustments, and disputes
        are not available here.
      </InlineNotice>
      <Panel className="mt-4">
        <FilterBar>
          <Select
            aria-label="COD queue"
            value={status}
            onChange={(event) => setStatus(event.target.value)}
            className="w-56"
          >
            <option value="">All custody states</option>
            {codStatuses.map((value) => (
              <option key={value}>{titleCase(value)}</option>
            ))}
          </Select>
          <Input
            aria-label="Search COD by AWB"
            placeholder="Search AWB"
            value={awb}
            onChange={(event) => setAwb(event.target.value.toUpperCase())}
            className="w-64 font-mono"
          />
        </FilterBar>
        {obligations.isPending ? (
          <LoadingState label="Loading COD obligations" />
        ) : obligations.error ? (
          <ErrorState
            error={obligations.error}
            retry={() => void obligations.refetch()}
          />
        ) : rows.length === 0 ? (
          <EmptyState
            icon={Banknote}
            title="No COD obligations"
            description="No obligations match this custody queue."
          />
        ) : (
          <DataTable label="COD obligations">
            <thead>
              <tr>
                <TableHead>Shipment</TableHead>
                <TableHead>Expected</TableHead>
                <TableHead>Collected</TableHead>
                <TableHead>Remitted</TableHead>
                <TableHead>Current holder</TableHead>
                <TableHead>State</TableHead>
                <TableHead>Age</TableHead>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={row.id}>
                  <TableCell>
                    <Link
                      className="font-mono text-xs font-semibold text-primary hover:underline"
                      to={`/finance/cod/obligations/${row.id}`}
                    >
                      {row.awb}
                    </Link>
                  </TableCell>
                  <TableCell>
                    <Money
                      amountMinor={row.expectedMinor}
                      currency={row.currency}
                    />
                  </TableCell>
                  <TableCell>
                    <Money
                      amountMinor={row.collectedMinor}
                      currency={row.currency}
                    />
                  </TableCell>
                  <TableCell>
                    <Money
                      amountMinor={row.remittedMinor}
                      currency={row.currency}
                    />
                  </TableCell>
                  <TableCell>
                    {row.custodianType
                      ? `${titleCase(row.custodianType)} · ${row.custodianId ?? "—"}`
                      : "Not collected"}
                  </TableCell>
                  <TableCell>
                    <FinancialStatus status={row.status} />
                  </TableCell>
                  <TableCell>
                    {row.collectedAt
                      ? formatDateTime(row.collectedAt)
                      : "Awaiting collection"}
                  </TableCell>
                </tr>
              ))}
            </tbody>
          </DataTable>
        )}
      </Panel>
      <ReconciliationDialog
        open={reconcileOpen}
        onOpenChange={setReconcileOpen}
      />
      <CustodyDialog open={custodyOpen} onOpenChange={setCustodyOpen} />
    </>
  );
}

function CustodyDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [partyType, setPartyType] = useState("OPERATING_UNIT");
  const [partyId, setPartyId] = useState("");
  const [position, setPosition] = useState<CODCustodyPosition>();
  const mutation = useMutation({
    mutationFn: () =>
      apiRequest<CODCustodyPosition>(
        `/api/v1/cod/custody/${partyType}/${partyId}`,
      ),
    onSuccess: setPosition,
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Cross-check COD custody"
      description="Compares the operational obligations with the independent ledger balance."
      footer={<Button onClick={() => onOpenChange(false)}>Close</Button>}
    >
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Party type" htmlFor="custody-party-type">
          <Select
            id="custody-party-type"
            value={partyType}
            onChange={(event) => setPartyType(event.target.value)}
          >
            <option>AGENT</option>
            <option>OPERATING_UNIT</option>
            <option>FRANCHISE</option>
          </Select>
        </Field>
        <Field label="Party ID" htmlFor="custody-party-id">
          <Input
            id="custody-party-id"
            type="number"
            value={partyId}
            onChange={(event) => setPartyId(event.target.value)}
          />
        </Field>
        <Button
          className="sm:col-span-2"
          variant="primary"
          disabled={!partyId}
          loading={mutation.isPending}
          onClick={() => mutation.mutate()}
        >
          Compare custody and ledger
        </Button>
      </div>
      {mutation.error ? <ErrorState error={mutation.error} /> : null}
      {position ? (
        <div className="mt-4 space-y-3">
          {position.reconciled ? (
            <InlineNotice tone="success" title="Custody reconciles">
              <CheckCircle2 className="inline h-4 w-4" /> Operational and ledger
              holdings agree.
            </InlineNotice>
          ) : (
            <InlineNotice tone="danger" title="Finance incident">
              <AlertTriangle className="inline h-4 w-4" /> Operational custody
              and ledger balance differ. Investigate before accepting or
              remitting cash.
            </InlineNotice>
          )}
          <div className="grid grid-cols-2 gap-px overflow-hidden rounded-md border border-border bg-border">
            <div className="bg-white p-3">
              <p className="text-xs text-slate-500">Obligation holding</p>
              <Money
                amountMinor={position.heldMinor}
                currency={position.currency}
                className="text-lg font-bold"
              />
            </div>
            <div className="bg-white p-3">
              <p className="text-xs text-slate-500">Ledger balance</p>
              <Money
                amountMinor={position.ledgerBalanceMinor}
                currency={position.currency}
                className="text-lg font-bold"
              />
            </div>
          </div>
        </div>
      ) : null}
    </Dialog>
  );
}

function ReconciliationDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const queryClient = useQueryClient();
  const [partyType, setPartyType] = useState("OPERATING_UNIT");
  const [partyId, setPartyId] = useState("");
  const [periodStart, setPeriodStart] = useState("");
  const [periodEnd, setPeriodEnd] = useState("");
  const [result, setResult] = useState<CODReconciliationResult>();
  const [obligationId, setObligationId] = useState("");
  const [counted, setCounted] = useState("");
  const [notes, setNotes] = useState("");
  const [varianceReason, setVarianceReason] = useState("");
  const openRecon = useMutation({
    mutationFn: () => {
      const body: CODReconciliationRequest = {
        partyType: partyType as CODReconciliationRequest["partyType"],
        partyId: Number(partyId),
        periodStart,
        periodEnd,
      };
      return apiRequest<CODReconciliationResult>(
        "/api/v1/cod/reconciliations",
        { method: "POST", body },
      );
    },
    onSuccess: setResult,
  });
  const reconciliationId = result?.reconciliation?.id;
  const count = useMutation({
    mutationFn: () => {
      const body: CODCountRequest = {
        obligationId,
        countedMinor: toMinorUnits(counted) ?? 0,
        notes: notes || undefined,
      };
      return apiRequest(
        `/api/v1/cod/reconciliations/${reconciliationId}/count`,
        { method: "POST", body },
      );
    },
    onSuccess: () => {
      setObligationId("");
      setCounted("");
      setNotes("");
    },
  });
  const complete = useMutation({
    mutationFn: () => {
      const body: CODCompleteReconciliationRequest = {
        varianceReason: varianceReason || undefined,
      };
      return apiRequest(
        `/api/v1/cod/reconciliations/${reconciliationId}/complete`,
        { method: "POST", body },
      );
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["cod-summary"] });
      void queryClient.invalidateQueries({ queryKey: ["cod-obligations"] });
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="COD reconciliation"
      description="Only one open count is allowed per party. Completion posts any shortage or excess to the ledger."
      footer={
        result ? (
          <>
            <Button onClick={() => onOpenChange(false)}>Keep open</Button>
            <Button
              variant="danger"
              loading={complete.isPending}
              onClick={() => complete.mutate()}
            >
              Complete reconciliation
            </Button>
          </>
        ) : (
          <>
            <Button onClick={() => onOpenChange(false)}>Cancel</Button>
            <Button
              variant="primary"
              loading={openRecon.isPending}
              disabled={!partyId || !periodStart || !periodEnd}
              onClick={() => openRecon.mutate()}
            >
              Open count
            </Button>
          </>
        )
      }
    >
      {!result ? (
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Party type" htmlFor="recon-party-type">
            <Select
              id="recon-party-type"
              value={partyType}
              onChange={(event) => setPartyType(event.target.value)}
            >
              <option>AGENT</option>
              <option>OPERATING_UNIT</option>
              <option>FRANCHISE</option>
            </Select>
          </Field>
          <Field label="Party ID" htmlFor="recon-party-id">
            <Input
              id="recon-party-id"
              type="number"
              value={partyId}
              onChange={(event) => setPartyId(event.target.value)}
            />
          </Field>
          <Field label="Period start" htmlFor="recon-start">
            <Input
              id="recon-start"
              type="date"
              value={periodStart}
              onChange={(event) => setPeriodStart(event.target.value)}
            />
          </Field>
          <Field label="Period end" htmlFor="recon-end">
            <Input
              id="recon-end"
              type="date"
              value={periodEnd}
              onChange={(event) => setPeriodEnd(event.target.value)}
            />
          </Field>
          {openRecon.error ? (
            <div className="sm:col-span-2">
              <ErrorState error={openRecon.error} />
            </div>
          ) : null}
        </div>
      ) : (
        <div className="space-y-4">
          <InlineNotice
            title={`Count ${result.reconciliation?.reconciliationCode ?? reconciliationId}`}
            tone="warning"
          >
            Expected{" "}
            <Money
              amountMinor={result.reconciliation?.expectedMinor}
              currency={result.reconciliation?.currency}
            />{" "}
            across {result.expected?.length ?? 0} obligations. Enter each
            counted line; the backend derives its outcome.
          </InlineNotice>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="Obligation ID" htmlFor="count-obligation">
              <Input
                id="count-obligation"
                value={obligationId}
                onChange={(event) => setObligationId(event.target.value)}
              />
            </Field>
            <Field label="Counted amount (₦)" htmlFor="count-amount">
              <Input
                id="count-amount"
                inputMode="decimal"
                value={counted}
                onChange={(event) => setCounted(event.target.value)}
              />
            </Field>
            <Field
              className="sm:col-span-2"
              label="Count notes"
              htmlFor="count-notes"
            >
              <Input
                id="count-notes"
                value={notes}
                onChange={(event) => setNotes(event.target.value)}
              />
            </Field>
            <Button
              className="sm:col-span-2"
              disabled={!obligationId || !counted}
              loading={count.isPending}
              onClick={() => count.mutate()}
            >
              Record counted line
            </Button>
          </div>
          <Field
            label="Variance reason"
            htmlFor="variance-reason"
            hint="Required when shortage or excess exists."
          >
            <Textarea
              id="variance-reason"
              value={varianceReason}
              onChange={(event) => setVarianceReason(event.target.value)}
            />
          </Field>
          {count.error ? (
            <ErrorState error={count.error} />
          ) : complete.error ? (
            <ErrorState error={complete.error} />
          ) : null}
        </div>
      )}
    </Dialog>
  );
}

export function CODObligationDetailPage() {
  const { obligationId = "" } = useParams();
  const query = useQuery({
    queryKey: ["cod-obligation", obligationId],
    queryFn: () =>
      apiRequest<CODObligationDetailResponse>(
        `/api/v1/cod/obligations/${obligationId}`,
      ),
    enabled: Boolean(obligationId),
  });
  if (query.isPending) return <LoadingState label="Loading COD obligation" />;
  if (query.error)
    return (
      <ErrorState error={query.error} retry={() => void query.refetch()} />
    );
  const obligation = query.data?.obligation;
  const collections = query.data?.collections ?? [];
  const currentIndex = codStatuses.indexOf(obligation?.status ?? "EXPECTED");
  return (
    <>
      <PageHeader
        eyebrow="Finance · COD obligation"
        title={obligation?.awb ?? "COD detail"}
        description="Expected COD is fixed at booking. Corrections are append-only adjustments, never edits."
        actions={
          <>
            <FinancialStatus status={obligation?.status} />
            <ImmutableBadge label="Expected amount immutable" />
          </>
        }
      />
      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_360px]">
        <div className="space-y-4">
          <Panel>
            <PanelHeader
              title="Custody timeline"
              description="The highlighted step is the backend-reported current state."
            />
            <ol
              className="grid gap-0 p-4 sm:grid-cols-5"
              aria-label="COD custody timeline"
            >
              {[
                "EXPECTED",
                "AGENT_COLLECTED",
                "BRANCH_RECEIVED",
                "FRANCHISE_CONFIRMED",
                "REMITTED",
              ].map((status, index) => (
                <li key={status} className="relative pb-4 sm:pb-0 sm:pr-3">
                  <span
                    className={`mb-2 block h-2.5 w-2.5 rounded-full ${index <= currentIndex ? "bg-primary" : "bg-slate-300"}`}
                  />
                  <span className="block text-xs font-semibold">
                    {titleCase(status)}
                  </span>
                  {index < 4 ? (
                    <span
                      aria-hidden
                      className="absolute left-1 top-3 h-[calc(100%-0.5rem)] w-px bg-border sm:left-2 sm:top-1 sm:h-px sm:w-[calc(100%-0.5rem)]"
                    />
                  ) : null}
                </li>
              ))}
            </ol>
          </Panel>
          <Panel>
            <PanelHeader
              title="Collections"
              description="Append-only receipts recorded against this shipment."
            />
            {collections.length === 0 ? (
              <EmptyState
                title="No collection recorded"
                description="The obligation is still expected or the collection has not been banked."
              />
            ) : (
              <DataTable label="COD collections">
                <thead>
                  <tr>
                    <TableHead>Collected</TableHead>
                    <TableHead>Mode</TableHead>
                    <TableHead>Reference</TableHead>
                    <TableHead>Timestamp</TableHead>
                  </tr>
                </thead>
                <tbody>
                  {collections.map((collection) => (
                    <tr key={collection.id}>
                      <TableCell>
                        <Money
                          amountMinor={collection.amountMinor}
                          currency={collection.currency}
                        />
                      </TableCell>
                      <TableCell>{collection.paymentMode}</TableCell>
                      <TableCell className="font-mono text-xs">
                        {collection.reference ?? "—"}
                      </TableCell>
                      <TableCell>
                        {formatDateTime(collection.collectedAt)}
                      </TableCell>
                    </tr>
                  ))}
                </tbody>
              </DataTable>
            )}
          </Panel>
        </div>
        <Panel>
          <PanelHeader title="Current position" />
          <dl className="divide-y divide-border">
            {[
              ["Shipment", obligation?.awb],
              [
                "Current holder",
                obligation?.custodianType
                  ? `${titleCase(obligation.custodianType)} · ${obligation.custodianId}`
                  : "Nobody yet",
              ],
              ["Current state", titleCase(obligation?.status ?? "Unknown")],
            ].map(([label, value]) => (
              <div
                key={String(label)}
                className="flex justify-between gap-4 px-4 py-3 text-sm"
              >
                <dt className="text-slate-500">{label}</dt>
                <dd className="text-right font-medium">{value}</dd>
              </div>
            ))}
          </dl>
          <div className="border-t border-border p-4">
            <div className="flex justify-between text-sm">
              <span>Expected</span>
              <Money
                amountMinor={obligation?.expectedMinor}
                currency={obligation?.currency}
              />
            </div>
            <div className="mt-2 flex justify-between text-sm">
              <span>Collected</span>
              <Money
                amountMinor={obligation?.collectedMinor}
                currency={obligation?.currency}
              />
            </div>
            <div className="mt-2 flex justify-between text-sm">
              <span>Adjusted</span>
              <Money
                amountMinor={obligation?.adjustedMinor}
                currency={obligation?.currency}
                showPositiveSign
              />
            </div>
            <div className="mt-2 flex justify-between border-t border-border pt-2 text-sm font-bold">
              <span>Remitted</span>
              <Money
                amountMinor={obligation?.remittedMinor}
                currency={obligation?.currency}
              />
            </div>
          </div>
          {obligation?.awb ? (
            <div className="border-t border-border p-4">
              <ReferenceLink
                to={`/shipments?search=${encodeURIComponent(obligation.awb)}`}
                type="shipment"
                reference={obligation.awb}
              />
            </div>
          ) : null}
        </Panel>
      </div>
    </>
  );
}
