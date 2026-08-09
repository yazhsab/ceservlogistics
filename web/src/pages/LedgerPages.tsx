import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, BookOpen, LockKeyhole, RotateCcw } from "lucide-react";
import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useAuth } from "../auth/AuthProvider";
import {
  apiRequest,
  queryString,
  type AccountStatementResponse,
  type AccountingPeriodListResponse,
  type JournalDetailResponse,
  type JournalListResponse,
  type LedgerAccountListResponse,
  type LedgerHealthResponse,
  type TrialBalance,
} from "../api/client";
import {
  DebitCreditAmount,
  FinancialStatus,
  ImmutableBadge,
  Money,
  ReferenceLink,
} from "../components/financial";
import {
  Button,
  ConfirmAction,
  DataTable,
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
import { formatDateTime, titleCase } from "../lib/utils";

function journalSourceHref(
  sourceType?: string | null,
  sourceId?: string | number | null,
) {
  if (!sourceId) return null;
  const id = encodeURIComponent(String(sourceId));
  switch (sourceType) {
    case "COMMISSION":
      return `/finance/commission/entries/${id}`;
    case "COD":
      return `/finance/cod/obligations/${id}`;
    case "SETTLEMENT":
      return `/finance/settlements/${id}`;
    case "INVOICE":
      return `/finance/billing/invoices/${id}`;
    default:
      return null;
  }
}

export function LedgerAccountsPage() {
  const [accountType, setAccountType] = useState("");
  const [search, setSearch] = useState("");
  const [offset, setOffset] = useState(0);
  const query = useQuery({
    queryKey: ["ledger-accounts", accountType, search, offset],
    queryFn: () =>
      apiRequest<LedgerAccountListResponse>(
        `/api/v1/ledger/accounts${queryString({ accountType, search, activeOnly: true, limit: 50, offset })}`,
      ),
  });
  const health = useQuery({
    queryKey: ["ledger-health"],
    queryFn: () => apiRequest<LedgerHealthResponse>("/api/v1/ledger/health"),
    staleTime: 30_000,
  });
  const accounts = query.data?.data ?? [];
  const total = query.data?.total ?? accounts.length;
  return (
    <>
      <PageHeader
        eyebrow="Finance · Ledger"
        title="Chart of accounts"
        description="Balances are server-computed from immutable postings. Positive means more of the account's normal balance—not necessarily cash in."
        actions={
          <>
            <Link
              className="inline-flex min-h-10 items-center rounded-md border border-border bg-white px-3.5 text-sm font-semibold"
              to="/finance/ledger/journals"
            >
              Journals
            </Link>
            <Link
              className="inline-flex min-h-10 items-center rounded-md border border-border bg-white px-3.5 text-sm font-semibold"
              to="/finance/ledger/trial-balance"
            >
              Trial balance
            </Link>
          </>
        }
      />
      {health.data && health.data.balanced === false ? (
        <InlineNotice tone="danger" title="Ledger integrity incident">
          <strong>The books do not balance.</strong> The invariant check found{" "}
          {health.data.unbalancedCount ?? "one or more"} unbalanced
          transaction(s). Escalate immediately; do not post corrective entries
          without investigation.
        </InlineNotice>
      ) : null}
      <Panel className="mt-4">
        <FilterBar>
          <Input
            aria-label="Search chart of accounts"
            placeholder="Search code or account name"
            value={search}
            onChange={(event) => {
              setSearch(event.target.value);
              setOffset(0);
            }}
            className="max-w-sm"
          />
          <Select
            aria-label="Account type"
            value={accountType}
            onChange={(event) => {
              setAccountType(event.target.value);
              setOffset(0);
            }}
            className="w-48"
          >
            <option value="">All account types</option>
            {["ASSET", "LIABILITY", "EQUITY", "REVENUE", "EXPENSE"].map(
              (value) => (
                <option key={value}>{value}</option>
              ),
            )}
          </Select>
        </FilterBar>
        {query.isPending ? (
          <LoadingState label="Loading chart of accounts" />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : accounts.length === 0 ? (
          <EmptyState
            icon={BookOpen}
            title="No matching accounts"
            description="Change the filters to see the chart of accounts."
          />
        ) : (
          <>
            <DataTable label="Chart of accounts">
              <thead>
                <tr>
                  <TableHead>Code / account</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead>Normal balance</TableHead>
                  <TableHead>Party</TableHead>
                  <TableHead className="text-right">Debit total</TableHead>
                  <TableHead className="text-right">Credit total</TableHead>
                  <TableHead className="text-right">Balance</TableHead>
                </tr>
              </thead>
              <tbody>
                {accounts.map((account) => (
                  <tr key={account.id}>
                    <TableCell>
                      <Link
                        className="font-semibold text-primary hover:underline"
                        to={`/finance/ledger/accounts/${account.id}`}
                      >
                        {account.code}
                      </Link>
                      <span className="block text-xs text-slate-500">
                        {account.name}
                      </span>
                    </TableCell>
                    <TableCell>{account.accountType}</TableCell>
                    <TableCell>{account.normalBalance}</TableCell>
                    <TableCell>
                      {account.partyType
                        ? titleCase(account.partyType)
                        : account.isSystem
                          ? "Control / system"
                          : "Organization"}
                    </TableCell>
                    <TableCell className="text-right">
                      <Money
                        amountMinor={account.totalDebitMinor}
                        currency={account.currency}
                      />
                    </TableCell>
                    <TableCell className="text-right">
                      <Money
                        amountMinor={account.totalCreditMinor}
                        currency={account.currency}
                      />
                    </TableCell>
                    <TableCell className="text-right font-semibold">
                      <Money
                        amountMinor={account.balanceMinor}
                        currency={account.currency}
                      />
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
    </>
  );
}

export function AccountStatementPage() {
  const { accountId = "" } = useParams();
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [offset, setOffset] = useState(0);
  const query = useQuery({
    queryKey: ["account-statement", accountId, from, to, offset],
    queryFn: () =>
      apiRequest<AccountStatementResponse>(
        `/api/v1/ledger/accounts/${accountId}/statement${queryString({ from, to, limit: 50, offset })}`,
      ),
    enabled: Boolean(accountId),
  });
  const lines = query.data?.data ?? [];
  const account = query.data?.account;
  const total = query.data?.total ?? lines.length;
  return (
    <>
      <PageHeader
        eyebrow="Finance · Account statement"
        title={`${account?.code ?? "Account"} · ${account?.name ?? "Statement"}`}
        description="Opening, running, and closing balances are supplied by the ledger. The browser does not accumulate transactions."
        actions={
          account ? (
            <Money
              amountMinor={account.balanceMinor}
              currency={account.currency}
              className="text-xl font-bold"
            />
          ) : undefined
        }
      />
      <Panel>
        <FilterBar>
          <Field label="From" htmlFor="statement-from">
            <Input
              id="statement-from"
              type="date"
              value={from}
              onChange={(event) => {
                setFrom(event.target.value);
                setOffset(0);
              }}
            />
          </Field>
          <Field label="To" htmlFor="statement-to">
            <Input
              id="statement-to"
              type="date"
              value={to}
              onChange={(event) => {
                setTo(event.target.value);
                setOffset(0);
              }}
            />
          </Field>
        </FilterBar>
        {query.isPending ? (
          <LoadingState label="Loading account statement" />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : lines.length === 0 ? (
          <EmptyState
            title="No movements"
            description="No transactions were posted in this period."
          />
        ) : (
          <>
            <DataTable label="Account statement">
              <thead>
                <tr>
                  <TableHead>Date</TableHead>
                  <TableHead>Journal / description</TableHead>
                  <TableHead>Reference</TableHead>
                  <TableHead className="text-right">Debit</TableHead>
                  <TableHead className="text-right">Credit</TableHead>
                  <TableHead className="text-right">Running balance</TableHead>
                </tr>
              </thead>
              <tbody>
                {lines.map((line) => (
                  <tr key={line.id}>
                    <TableCell>{line.postingDate}</TableCell>
                    <TableCell>
                      <Link
                        className="font-semibold text-primary hover:underline"
                        to={`/finance/ledger/journals/${line.transactionId}`}
                      >
                        {line.transactionNumber}
                      </Link>
                      <span className="block text-xs text-slate-500">
                        {line.description}
                      </span>
                    </TableCell>
                    <TableCell>
                      {line.sourceId ? (
                        <span className="font-mono text-xs">
                          {line.sourceType} · {line.sourceId}
                        </span>
                      ) : (
                        line.sourceType
                      )}
                    </TableCell>
                    <TableCell className="text-right">
                      {line.debitMinor ? (
                        <Money
                          amountMinor={line.debitMinor}
                          currency={line.currency}
                        />
                      ) : (
                        "—"
                      )}
                    </TableCell>
                    <TableCell className="text-right">
                      {line.creditMinor ? (
                        <Money
                          amountMinor={line.creditMinor}
                          currency={line.currency}
                        />
                      ) : (
                        "—"
                      )}
                    </TableCell>
                    <TableCell className="text-right font-semibold">
                      <Money
                        amountMinor={line.runningBalanceMinor}
                        currency={line.currency}
                      />
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
    </>
  );
}

export function JournalTransactionsPage() {
  const [status, setStatus] = useState("");
  const [sourceType, setSourceType] = useState("");
  const query = useQuery({
    queryKey: ["journals", status, sourceType],
    queryFn: () =>
      apiRequest<JournalListResponse>(
        `/api/v1/ledger/journals${queryString({ status, sourceType, limit: 50 })}`,
      ),
  });
  const rows = query.data?.data ?? [];
  return (
    <>
      <PageHeader
        eyebrow="Finance · Ledger"
        title="Journal transactions"
        description="Every posted transaction balances and remains visible. Reversed journals are retained with their mirror transaction."
      />
      <Panel>
        <FilterBar>
          <Select
            aria-label="Journal status"
            value={status}
            onChange={(event) => setStatus(event.target.value)}
            className="w-44"
          >
            <option value="">All states</option>
            <option>DRAFT</option>
            <option>POSTED</option>
            <option>REVERSED</option>
          </Select>
          <Input
            aria-label="Journal source type"
            placeholder="Source type"
            value={sourceType}
            onChange={(event) => setSourceType(event.target.value)}
            className="w-52"
          />
        </FilterBar>
        {query.isPending ? (
          <LoadingState label="Loading journals" />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : rows.length === 0 ? (
          <EmptyState
            title="No journal transactions"
            description="No transactions match these filters."
          />
        ) : (
          <DataTable label="Journal transactions">
            <thead>
              <tr>
                <TableHead>Journal</TableHead>
                <TableHead>Posting date</TableHead>
                <TableHead>Source / purpose</TableHead>
                <TableHead>Description</TableHead>
                <TableHead>Entries</TableHead>
                <TableHead className="text-right">Balanced total</TableHead>
                <TableHead>Status</TableHead>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr
                  key={row.id}
                  className={
                    row.status === "REVERSED"
                      ? "text-slate-500 line-through"
                      : undefined
                  }
                >
                  <TableCell>
                    <Link
                      className="font-semibold text-primary hover:underline"
                      to={`/finance/ledger/journals/${row.id}`}
                    >
                      {row.transactionNumber}
                    </Link>
                  </TableCell>
                  <TableCell>{row.postingDate}</TableCell>
                  <TableCell>
                    {titleCase(row.sourceType ?? "Unknown")}
                    <span className="block text-xs text-slate-500">
                      {titleCase(row.purpose ?? "")}
                    </span>
                  </TableCell>
                  <TableCell className="max-w-md">{row.description}</TableCell>
                  <TableCell>{row.entryCount}</TableCell>
                  <TableCell className="text-right">
                    <Money
                      amountMinor={row.totalMinor}
                      currency={row.currency}
                    />
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
    </>
  );
}

export function JournalDetailPage() {
  const { journalId = "" } = useParams();
  const { hasPermission } = useAuth();
  const queryClient = useQueryClient();
  const [reverseOpen, setReverseOpen] = useState(false);
  const [reason, setReason] = useState("");
  const query = useQuery({
    queryKey: ["journal", journalId],
    queryFn: () =>
      apiRequest<JournalDetailResponse>(`/api/v1/ledger/journals/${journalId}`),
    enabled: Boolean(journalId),
  });
  const reverse = useMutation({
    mutationFn: () =>
      apiRequest(`/api/v1/ledger/journals/${journalId}/reverse`, {
        method: "POST",
        body: { reason },
      }),
    onSuccess: () => {
      setReverseOpen(false);
      setReason("");
      void queryClient.invalidateQueries({ queryKey: ["journal", journalId] });
      void queryClient.invalidateQueries({ queryKey: ["journals"] });
    },
  });
  if (query.isPending) return <LoadingState label="Loading journal" />;
  if (query.error)
    return (
      <ErrorState error={query.error} retry={() => void query.refetch()} />
    );
  const transaction = query.data?.transaction;
  const entries = query.data?.entries ?? [];
  const sourceHref = journalSourceHref(
    transaction?.sourceType,
    transaction?.sourceId,
  );
  return (
    <>
      <PageHeader
        eyebrow="Finance · Journal"
        title={transaction?.transactionNumber ?? "Journal transaction"}
        description={transaction?.description}
        actions={
          <>
            {transaction?.status === "POSTED" ? (
              <ImmutableBadge />
            ) : (
              <FinancialStatus status={transaction?.status} />
            )}
            {transaction?.status === "POSTED" &&
            hasPermission("ledger.reverse") ? (
              <Button variant="danger" onClick={() => setReverseOpen(true)}>
                <RotateCcw className="h-4 w-4" /> Reverse journal
              </Button>
            ) : null}
          </>
        }
      />
      <Panel>
        <PanelHeader
          title="Balanced entries"
          description="Debit and credit totals are backend-enforced; posted entries have no Edit action."
        />
        <DataTable label="Journal entries">
          <thead>
            <tr>
              <TableHead>Account</TableHead>
              <TableHead className="text-right">Debit</TableHead>
              <TableHead className="text-right">Credit</TableHead>
              <TableHead>Reference</TableHead>
              <TableHead>Description</TableHead>
            </tr>
          </thead>
          <tbody>
            {entries.map((entry) => (
              <tr key={entry.id}>
                <TableCell>
                  <span className="font-mono font-semibold">
                    {entry.accountCode}
                  </span>
                  <span className="block text-xs text-slate-500">
                    {entry.accountName}
                  </span>
                </TableCell>
                <TableCell className="text-right">
                  {entry.debitMinor ? (
                    <DebitCreditAmount
                      amountMinor={entry.debitMinor}
                      currency={entry.currency}
                      direction="DEBIT"
                    />
                  ) : (
                    "—"
                  )}
                </TableCell>
                <TableCell className="text-right">
                  {entry.creditMinor ? (
                    <DebitCreditAmount
                      amountMinor={entry.creditMinor}
                      currency={entry.currency}
                      direction="CREDIT"
                    />
                  ) : (
                    "—"
                  )}
                </TableCell>
                <TableCell>
                  {transaction?.sourceId ? (
                    sourceHref ? (
                      <ReferenceLink
                        to={sourceHref}
                        type={titleCase(transaction.sourceType ?? "reference")}
                        reference={String(transaction.sourceId)}
                      />
                    ) : (
                      <span className="font-mono text-xs">
                        {transaction.sourceId}
                      </span>
                    )
                  ) : (
                    "—"
                  )}
                </TableCell>
                <TableCell>{entry.memo ?? transaction?.description}</TableCell>
              </tr>
            ))}
          </tbody>
          <tfoot>
            <tr className="bg-slate-50">
              <TableCell className="font-bold">Totals</TableCell>
              <TableCell className="text-right font-bold">
                <Money
                  amountMinor={transaction?.totalMinor}
                  currency={transaction?.currency}
                />
              </TableCell>
              <TableCell className="text-right font-bold">
                <Money
                  amountMinor={transaction?.totalMinor}
                  currency={transaction?.currency}
                />
              </TableCell>
              <TableCell>{null}</TableCell>
              <TableCell>
                <span className="font-semibold text-success">Balanced</span>
              </TableCell>
            </tr>
          </tfoot>
        </DataTable>
      </Panel>
      {transaction?.status === "REVERSED" ? (
        <InlineNotice title="Reversed but retained" tone="warning">
          This journal remains in the books and is cancelled arithmetically by
          its reversal. It has not been deleted or subtracted in the browser.
        </InlineNotice>
      ) : null}
      <ConfirmAction
        open={reverseOpen}
        onOpenChange={setReverseOpen}
        title="Reverse posted journal?"
        description="This creates and posts a mirror transaction. The original remains visible and cannot be restored by editing."
        confirmLabel="Post reversal"
        loading={reverse.isPending}
        onConfirm={() => reverse.mutate()}
      >
        <Field
          label="Substantive reversal reason"
          htmlFor="reverse-reason"
          required
          hint="Minimum 10 characters; this becomes part of the audit trail."
        >
          <Textarea
            id="reverse-reason"
            value={reason}
            onChange={(event) => setReason(event.target.value)}
          />
        </Field>
        {reason.trim().length < 10 ? (
          <p className="mt-2 text-xs text-danger">
            Enter at least 10 characters.
          </p>
        ) : null}
        {reverse.error ? <ErrorState error={reverse.error} /> : null}
      </ConfirmAction>
    </>
  );
}

export function TrialBalancePage() {
  const [asOf, setAsOf] = useState("");
  const query = useQuery({
    queryKey: ["trial-balance", asOf],
    queryFn: () =>
      apiRequest<TrialBalance>(
        `/api/v1/ledger/trial-balance${queryString({ asOf })}`,
      ),
  });
  const periods = useQuery({
    queryKey: ["accounting-periods"],
    queryFn: () =>
      apiRequest<AccountingPeriodListResponse>("/api/v1/ledger/periods"),
  });
  const report = query.data;
  return (
    <>
      <PageHeader
        eyebrow="Finance · Ledger"
        title="Trial balance"
        description="The backend checks this report against itself. Any non-zero difference is a production finance incident."
      />
      {report && !report.balanced ? (
        <InlineNotice tone="danger" title="Trial balance does not balance">
          <AlertTriangle className="inline h-4 w-4" /> Difference{" "}
          <Money
            amountMinor={report.differenceMinor}
            currency={report.currency}
          />
          . Stop period close and investigate.
        </InlineNotice>
      ) : null}
      <div className="mt-4 grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
        <Panel>
          <FilterBar>
            <Field label="As of" htmlFor="trial-as-of">
              <Input
                id="trial-as-of"
                type="date"
                value={asOf}
                onChange={(event) => setAsOf(event.target.value)}
              />
            </Field>
          </FilterBar>
          {query.isPending ? (
            <LoadingState label="Preparing trial balance" />
          ) : query.error ? (
            <ErrorState
              error={query.error}
              retry={() => void query.refetch()}
            />
          ) : (
            <>
              <DataTable label="Trial balance">
                <thead>
                  <tr>
                    <TableHead>Account</TableHead>
                    <TableHead>Type / normal balance</TableHead>
                    <TableHead className="text-right">Debit</TableHead>
                    <TableHead className="text-right">Credit</TableHead>
                    <TableHead className="text-right">Balance</TableHead>
                  </tr>
                </thead>
                <tbody>
                  {report?.rows?.map((row) => (
                    <tr key={row.code}>
                      <TableCell>
                        <span className="font-mono font-semibold">
                          {row.code}
                        </span>
                        <span className="block text-xs text-slate-500">
                          {row.name}
                        </span>
                      </TableCell>
                      <TableCell>
                        {row.accountType}
                        <span className="block text-xs text-slate-500">
                          {row.normalBalance} normal
                        </span>
                      </TableCell>
                      <TableCell className="text-right">
                        <Money
                          amountMinor={row.totalDebitMinor}
                          currency={report.currency}
                        />
                      </TableCell>
                      <TableCell className="text-right">
                        <Money
                          amountMinor={row.totalCreditMinor}
                          currency={report.currency}
                        />
                      </TableCell>
                      <TableCell className="text-right font-semibold">
                        <Money
                          amountMinor={row.balanceMinor}
                          currency={report.currency}
                        />
                      </TableCell>
                    </tr>
                  ))}
                </tbody>
                <tfoot>
                  <tr className="bg-slate-950 text-white">
                    <TableCell className="font-bold text-white">
                      Total
                    </TableCell>
                    <TableCell className="text-white">
                      {report?.balanced ? "BALANCED" : "UNBALANCED"}
                    </TableCell>
                    <TableCell className="text-right text-white">
                      <Money
                        amountMinor={report?.totalDebitMinor}
                        currency={report?.currency}
                      />
                    </TableCell>
                    <TableCell className="text-right text-white">
                      <Money
                        amountMinor={report?.totalCreditMinor}
                        currency={report?.currency}
                      />
                    </TableCell>
                    <TableCell className="text-right text-white">
                      <Money
                        amountMinor={report?.differenceMinor}
                        currency={report?.currency}
                      />
                    </TableCell>
                  </tr>
                </tfoot>
              </DataTable>
            </>
          )}
        </Panel>
        <Panel>
          <PanelHeader
            title="Accounting periods"
            description="Closed periods reject all postings."
          />
          {periods.isPending ? (
            <LoadingState label="Loading periods" />
          ) : periods.error ? (
            <ErrorState error={periods.error} />
          ) : (
            <div className="divide-y divide-border">
              {periods.data?.data?.map((period) => (
                <div key={period.id} className="p-4">
                  <div className="flex items-center justify-between gap-2">
                    <strong>{period.code}</strong>
                    <FinancialStatus status={period.status} />
                  </div>
                  <p className="mt-1 text-xs text-slate-500">
                    {period.startsOn} – {period.endsOn}
                  </p>
                  {period.status === "CLOSED" ? (
                    <p className="mt-2 flex items-center gap-1 text-xs text-slate-600">
                      <LockKeyhole className="h-3.5 w-3.5" /> Closed{" "}
                      {formatDateTime(period.closedAt)}
                    </p>
                  ) : null}
                </div>
              ))}
            </div>
          )}
        </Panel>
      </div>
    </>
  );
}
