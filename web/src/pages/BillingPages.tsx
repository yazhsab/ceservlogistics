import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { FileMinus2, FileText, Plus, Printer } from "lucide-react";
import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useAuth } from "../auth/AuthProvider";
import {
  apiRequest,
  queryString,
  type CreditNote,
  type CreditNoteRequest,
  type DraftInvoiceRequest,
  type Invoice,
  type InvoiceListResponse,
  type InvoicePaymentRequest,
  type InvoiceResult,
  type InvoiceCreditNotePage,
} from "../api/client";
import { CursorPager } from "../components/operations";
import {
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
  Pagination,
  Panel,
  PanelHeader,
  Select,
  TableCell,
  TableHead,
  Textarea,
} from "../components/ui";
import { titleCase, toMinorUnits } from "../lib/utils";

function DraftInvoiceDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const queryClient = useQueryClient();
  const [customerId, setCustomerId] = useState("");
  const [periodStart, setPeriodStart] = useState("");
  const [periodEnd, setPeriodEnd] = useState("");
  const [billingMode, setBillingMode] =
    useState<DraftInvoiceRequest["billingMode"]>("PERIODIC");
  const [summary, setSummary] = useState(false);
  const [result, setResult] = useState<InvoiceResult>();
  const mutation = useMutation({
    mutationFn: () => {
      const body: DraftInvoiceRequest = {
        customerId,
        periodStart,
        periodEnd,
        billingMode,
        summary,
        issueDate: new Date().toISOString().slice(0, 10),
      };
      return apiRequest<InvoiceResult>("/api/v1/invoices", {
        method: "POST",
        body,
      });
    },
    onSuccess: (value) => {
      setResult(value);
      void queryClient.invalidateQueries({ queryKey: ["invoices"] });
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Draft invoice"
      description="Builds lines from immutable shipment charge snapshots. No statutory number is allocated and nothing posts until issue."
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
              disabled={!customerId || !periodStart || !periodEnd}
              onClick={() => mutation.mutate()}
            >
              Create draft
            </Button>
          </>
        )
      }
    >
      {result ? (
        <InlineNotice tone="success" title="Draft prepared">
          <strong>{result.invoice?.invoiceNumber}</strong> is a draft reference,
          not a statutory invoice number.
          <div className="mt-3">
            <Link
              className="font-semibold underline"
              to={`/finance/billing/invoices/${result.invoice?.id}`}
            >
              Review draft invoice
            </Link>
          </div>
        </InlineNotice>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Customer ID" htmlFor="invoice-customer" required>
            <Input
              id="invoice-customer"
              value={customerId}
              onChange={(event) => setCustomerId(event.target.value)}
            />
          </Field>
          <Field label="Billing mode" htmlFor="billing-mode">
            <Select
              id="billing-mode"
              value={billingMode}
              onChange={(event) =>
                setBillingMode(
                  event.target.value as DraftInvoiceRequest["billingMode"],
                )
              }
            >
              <option>PERIODIC</option>
              <option>IMMEDIATE</option>
            </Select>
          </Field>
          <Field label="Period start" htmlFor="invoice-start" required>
            <Input
              id="invoice-start"
              type="date"
              value={periodStart}
              onChange={(event) => setPeriodStart(event.target.value)}
            />
          </Field>
          <Field label="Period end" htmlFor="invoice-end" required>
            <Input
              id="invoice-end"
              type="date"
              value={periodEnd}
              onChange={(event) => setPeriodEnd(event.target.value)}
            />
          </Field>
          <label className="sm:col-span-2 flex items-start gap-3 rounded-md border border-border p-3 text-sm">
            <input
              className="mt-1"
              type="checkbox"
              checked={summary}
              onChange={(event) => setSummary(event.target.checked)}
            />
            <span>
              <strong className="block">Summary invoice line</strong>
              <span className="text-xs text-slate-500">
                Roll up the visible invoice lines; shipment traceability remains
                on the backend.
              </span>
            </span>
          </label>
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

export function InvoicesPage() {
  const { hasPermission } = useAuth();
  const [status, setStatus] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  const query = useQuery({
    queryKey: ["invoices", status],
    queryFn: () =>
      apiRequest<InvoiceListResponse>(
        `/api/v1/invoices${queryString({ status, limit: 50 })}`,
      ),
  });
  const rows = query.data?.data ?? [];
  return (
    <>
      <PageHeader
        eyebrow="Finance · Billing"
        title="Invoices"
        description="Drafts are reviewable; issued invoices are statutory, posted, and immutable. Corrections use credit or debit notes."
        actions={
          hasPermission("invoice.create") ? (
            <Button variant="primary" onClick={() => setCreateOpen(true)}>
              <Plus className="h-4 w-4" /> Draft invoice
            </Button>
          ) : undefined
        }
      />
      <InlineNotice title="Available billing features">
        Create and review individual invoices here. Bulk billing runs,
        credit/debit-note registers, and invoice PDF downloads are not available
        in this workspace.
      </InlineNotice>
      <Panel className="mt-4">
        <FilterBar>
          <Select
            aria-label="Invoice status"
            value={status}
            onChange={(event) => setStatus(event.target.value)}
            className="w-52"
          >
            <option value="">All invoice states</option>
            {[
              "DRAFT",
              "ISSUED",
              "PARTIALLY_PAID",
              "PAID",
              "OVERDUE",
              "CANCELLED",
              "WRITTEN_OFF",
            ].map((value) => (
              <option key={value}>{titleCase(value)}</option>
            ))}
          </Select>
        </FilterBar>
        {query.isPending ? (
          <LoadingState label="Loading invoices" />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : rows.length === 0 ? (
          <EmptyState
            icon={FileText}
            title="No invoices"
            description="No invoices match this state."
          />
        ) : (
          <DataTable label="Invoices">
            <thead>
              <tr>
                <TableHead>Invoice</TableHead>
                <TableHead>Customer</TableHead>
                <TableHead>Period</TableHead>
                <TableHead>Shipments</TableHead>
                <TableHead className="text-right">Subtotal</TableHead>
                <TableHead className="text-right">Tax</TableHead>
                <TableHead className="text-right">Total</TableHead>
                <TableHead>Payment state</TableHead>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={row.id}>
                  <TableCell>
                    <Link
                      className="font-semibold text-primary hover:underline"
                      to={`/finance/billing/invoices/${row.id}`}
                    >
                      {row.status === "DRAFT"
                        ? "Draft invoice"
                        : row.invoiceNumber}
                    </Link>
                    {row.status === "DRAFT" ? (
                      <span className="block font-mono text-xs text-slate-500">
                        {row.invoiceNumber}
                      </span>
                    ) : null}
                  </TableCell>
                  <TableCell>
                    {row.customerName}
                    <span className="block text-xs text-slate-500">
                      {row.customerCode}
                    </span>
                  </TableCell>
                  <TableCell>
                    {row.periodStart ?? row.issueDate} –{" "}
                    {row.periodEnd ?? row.dueDate}
                  </TableCell>
                  <TableCell>{row.shipmentCount ?? 0}</TableCell>
                  <TableCell className="text-right">
                    <Money
                      amountMinor={row.subtotalMinor}
                      currency={row.currency}
                    />
                  </TableCell>
                  <TableCell className="text-right">
                    <Money amountMinor={row.taxMinor} currency={row.currency} />
                  </TableCell>
                  <TableCell className="text-right font-bold">
                    <Money
                      amountMinor={row.totalMinor}
                      currency={row.currency}
                    />
                  </TableCell>
                  <TableCell>
                    <FinancialStatus status={row.status} />
                    <span className="mt-1 block text-xs text-slate-500">
                      Paid{" "}
                      <Money
                        amountMinor={row.paidMinor}
                        currency={row.currency}
                      />{" "}
                      · Credited{" "}
                      <Money
                        amountMinor={row.creditedMinor}
                        currency={row.currency}
                      />
                    </span>
                  </TableCell>
                </tr>
              ))}
            </tbody>
          </DataTable>
        )}
      </Panel>
      <DraftInvoiceDialog open={createOpen} onOpenChange={setCreateOpen} />
    </>
  );
}

function CreditNoteDialog({
  invoice,
  initialNote,
  open,
  onOpenChange,
}: {
  invoice: Invoice;
  initialNote?: CreditNote;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { hasPermission } = useAuth();
  const queryClient = useQueryClient();
  const [noteType, setNoteType] =
    useState<CreditNoteRequest["noteType"]>("CREDIT");
  const [reasonCode, setReasonCode] =
    useState<CreditNoteRequest["reasonCode"]>("BILLING_ERROR");
  const [reason, setReason] = useState("");
  const [amount, setAmount] = useState("");
  const [tax, setTax] = useState("");
  const [note, setNote] = useState<CreditNote | undefined>(initialNote);
  const create = useMutation({
    mutationFn: () => {
      const body: CreditNoteRequest = {
        invoiceId: invoice.id ?? "",
        noteType,
        reasonCode,
        reason,
        amountMinor: toMinorUnits(amount) ?? 0,
        taxMinor: toMinorUnits(tax),
        issueDate: new Date().toISOString().slice(0, 10),
      };
      return apiRequest<CreditNote>("/api/v1/credit-notes", {
        method: "POST",
        body,
      });
    },
    onSuccess: (value) => {
      setNote(value);
      void queryClient.invalidateQueries({
        queryKey: ["invoice-notes", invoice.id],
      });
    },
  });
  const issue = useMutation({
    mutationFn: () =>
      apiRequest<CreditNote>(`/api/v1/credit-notes/${note?.id}/issue`, {
        method: "POST",
      }),
    onSuccess: (value) => {
      setNote(value);
      void queryClient.invalidateQueries({ queryKey: ["invoice", invoice.id] });
      void queryClient.invalidateQueries({
        queryKey: ["invoice-notes", invoice.id],
      });
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Credit or debit note"
      description="A maker raises the draft; a different checker must issue and post it."
      footer={
        note ? (
          <>
            <Button onClick={() => onOpenChange(false)}>Close</Button>
            {initialNote &&
            note.status === "DRAFT" &&
            hasPermission("creditnote.approve") ? (
              <Button
                variant="danger"
                loading={issue.isPending}
                onClick={() => issue.mutate()}
              >
                Issue note as checker
              </Button>
            ) : null}
          </>
        ) : (
          <>
            <Button onClick={() => onOpenChange(false)}>Cancel</Button>
            <Button
              variant="primary"
              loading={create.isPending}
              disabled={!reason || !amount}
              onClick={() => create.mutate()}
            >
              Raise draft note
            </Button>
          </>
        )
      }
    >
      {note ? (
        <div className="space-y-3">
          <InlineNotice
            tone="success"
            title={`${titleCase(note.noteType ?? "Credit")} note ${titleCase(note.status ?? "Draft")}`}
          >
            {note.noteNumber} ·{" "}
            <Money amountMinor={note.totalMinor} currency={note.currency} />.{" "}
            {note.status === "DRAFT"
              ? "Nothing has posted yet."
              : "The note is posted and immutable."}
          </InlineNotice>
          <p className="text-sm">{note.reason}</p>
          {note.status === "DRAFT" ? (
            <p className="text-sm text-slate-600">
              A different finance user must review and issue this note from the
              invoice's Credit and debit notes section.
            </p>
          ) : null}
          {issue.error ? <ErrorState error={issue.error} /> : null}
        </div>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Note type" htmlFor="note-type">
            <Select
              id="note-type"
              value={noteType}
              onChange={(event) =>
                setNoteType(event.target.value as CreditNoteRequest["noteType"])
              }
            >
              <option>CREDIT</option>
              <option>DEBIT</option>
            </Select>
          </Field>
          <Field label="Reason code" htmlFor="note-reason-code">
            <Select
              id="note-reason-code"
              value={reasonCode}
              onChange={(event) =>
                setReasonCode(
                  event.target.value as CreditNoteRequest["reasonCode"],
                )
              }
            >
              {[
                "BILLING_ERROR",
                "SERVICE_FAILURE",
                "RATE_CORRECTION",
                "GOODWILL",
                "RETURN",
                "TAX_CORRECTION",
                "SHORT_SHIPMENT",
                "OTHER",
              ].map((value) => (
                <option key={value}>{value}</option>
              ))}
            </Select>
          </Field>
          <Field label="Amount before tax (₦)" htmlFor="note-amount" required>
            <Input
              id="note-amount"
              inputMode="decimal"
              value={amount}
              onChange={(event) => setAmount(event.target.value)}
            />
          </Field>
          <Field label="Tax amount (₦)" htmlFor="note-tax">
            <Input
              id="note-tax"
              inputMode="decimal"
              value={tax}
              onChange={(event) => setTax(event.target.value)}
            />
          </Field>
          <Field
            className="sm:col-span-2"
            label="Substantive reason"
            htmlFor="note-reason"
            required
          >
            <Textarea
              id="note-reason"
              value={reason}
              onChange={(event) => setReason(event.target.value)}
            />
          </Field>
          {create.error ? (
            <div className="sm:col-span-2">
              <ErrorState error={create.error} />
            </div>
          ) : null}
        </div>
      )}
    </Dialog>
  );
}

export function InvoiceDetailPage() {
  const { invoiceId = "" } = useParams();
  const { hasPermission } = useAuth();
  const queryClient = useQueryClient();
  const [issueOpen, setIssueOpen] = useState(false);
  const [payOpen, setPayOpen] = useState(false);
  const [noteOpen, setNoteOpen] = useState(false);
  const [selectedNote, setSelectedNote] = useState<CreditNote>();
  const [noteCursors, setNoteCursors] = useState<(string | undefined)[]>([
    undefined,
  ]);
  const [linePage, setLinePage] = useState(1);
  const [amount, setAmount] = useState("");
  const [mode, setMode] =
    useState<InvoicePaymentRequest["paymentMode"]>("BANK_TRANSFER");
  const [reference, setReference] = useState("");
  const query = useQuery({
    queryKey: ["invoice", invoiceId],
    queryFn: () => apiRequest<InvoiceResult>(`/api/v1/invoices/${invoiceId}`),
    enabled: Boolean(invoiceId),
  });
  const notes = useQuery({
    queryKey: ["invoice-notes", invoiceId, noteCursors.at(-1)],
    queryFn: () =>
      apiRequest<InvoiceCreditNotePage>(
        `/api/v1/invoices/${invoiceId}/credit-notes${queryString({ limit: 25, cursor: noteCursors.at(-1) })}`,
      ),
    enabled: Boolean(invoiceId) && Boolean(query.data?.invoice),
  });
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ["invoice", invoiceId] });
    void queryClient.invalidateQueries({ queryKey: ["invoices"] });
  };
  const issue = useMutation({
    mutationFn: () =>
      apiRequest<Invoice>(`/api/v1/invoices/${invoiceId}/issue`, {
        method: "POST",
      }),
    onSuccess: () => {
      setIssueOpen(false);
      refresh();
    },
  });
  const pay = useMutation({
    mutationFn: () => {
      const body: InvoicePaymentRequest = {
        amountMinor: toMinorUnits(amount) ?? 0,
        paymentMode: mode,
        reference: reference || undefined,
        receivedOn: new Date().toISOString().slice(0, 10),
      };
      return apiRequest(`/api/v1/invoices/${invoiceId}/payments`, {
        method: "POST",
        body,
      });
    },
    onSuccess: () => {
      setPayOpen(false);
      setAmount("");
      setReference("");
      refresh();
    },
  });
  if (query.isPending) return <LoadingState label="Loading invoice" />;
  if (query.error)
    return (
      <ErrorState error={query.error} retry={() => void query.refetch()} />
    );
  const invoice = query.data?.invoice;
  if (!invoice)
    return (
      <EmptyState
        title="Invoice unavailable"
        description="The backend did not return this invoice."
      />
    );
  const lines = query.data?.lines ?? [];
  const taxes = query.data?.taxes ?? [];
  const issued = invoice.status !== "DRAFT";
  const pageLines = lines.slice((linePage - 1) * 50, linePage * 50);
  return (
    <>
      <PageHeader
        eyebrow="Finance · Invoice"
        title={issued ? (invoice.invoiceNumber ?? "Invoice") : "Draft invoice"}
        description={`${invoice.customerName ?? "Customer"} · ${invoice.customerCode ?? ""}`}
        actions={
          <>
            {issued ? (
              <ImmutableBadge
                label={`${titleCase(invoice.status ?? "Issued")} · Immutable`}
              />
            ) : (
              <FinancialStatus status="DRAFT" />
            )}
            {invoice.status === "DRAFT" && hasPermission("invoice.issue") ? (
              <Button variant="primary" onClick={() => setIssueOpen(true)}>
                Issue invoice
              </Button>
            ) : null}
            {["ISSUED", "PARTIALLY_PAID", "OVERDUE"].includes(
              invoice.status ?? "",
            ) && hasPermission("invoice.payment") ? (
              <Button variant="primary" onClick={() => setPayOpen(true)}>
                Record payment
              </Button>
            ) : null}
            {issued && hasPermission("creditnote.create") ? (
              <Button
                onClick={() => {
                  setSelectedNote(undefined);
                  setNoteOpen(true);
                }}
              >
                <FileMinus2 className="h-4 w-4" /> Credit / debit note
              </Button>
            ) : null}
            <Button
              disabled
              title="Invoice PDF downloads are not available yet"
            >
              <Printer className="h-4 w-4" /> PDF unavailable
            </Button>
          </>
        }
      />
      {!issued ? (
        <InlineNotice tone="warning" title="Draft reference only">
          {invoice.invoiceNumber} is not a statutory invoice number. Review the
          shipment snapshots and tax components before issue.
        </InlineNotice>
      ) : null}
      <div className="mt-4 grid gap-4 xl:grid-cols-[minmax(0,1fr)_360px]">
        <Panel>
          <PanelHeader
            title="Shipment charges"
            description={`${invoice.shipmentCount ?? 0} shipments · immutable snapshots${lines.length > 50 ? " · visually paginated in this response" : ""}`}
          />
          <DataTable label="Invoice lines">
            <thead>
              <tr>
                <TableHead>Line</TableHead>
                <TableHead>Description</TableHead>
                <TableHead>Shipment</TableHead>
                <TableHead className="text-right">Taxable</TableHead>
                <TableHead className="text-right">Tax</TableHead>
                <TableHead className="text-right">Total</TableHead>
              </tr>
            </thead>
            <tbody>
              {pageLines.map((line) => (
                <tr key={line.id}>
                  <TableCell>{line.lineNo}</TableCell>
                  <TableCell>
                    {line.description}
                    <span className="block text-xs text-slate-500">
                      {titleCase(line.lineType ?? "Other")} · HSN/SAC{" "}
                      {line.hsnSacCode ?? "—"}
                    </span>
                  </TableCell>
                  <TableCell>
                    {line.awb ? (
                      <ReferenceLink
                        to={`/shipments?search=${encodeURIComponent(line.awb)}`}
                        type="shipment"
                        reference={line.awb}
                      />
                    ) : (
                      "Summary"
                    )}
                  </TableCell>
                  <TableCell className="text-right">
                    <Money
                      amountMinor={line.taxableMinor}
                      currency={line.currency}
                    />
                  </TableCell>
                  <TableCell className="text-right">
                    <Money
                      amountMinor={line.taxMinor}
                      currency={line.currency}
                    />
                  </TableCell>
                  <TableCell className="text-right font-semibold">
                    <Money
                      amountMinor={line.totalMinor}
                      currency={line.currency}
                    />
                  </TableCell>
                </tr>
              ))}
            </tbody>
          </DataTable>
          {lines.length > 50 ? (
            <>
              <Pagination
                page={linePage}
                totalPages={Math.ceil(lines.length / 50)}
                onPageChange={setLinePage}
              />
              <InlineNotice title="Server pagination gap">
                The invoice endpoint returned all {lines.length} lines. This
                pager reduces visual density but cannot prevent the full payload
                until a server-paginated line endpoint exists.
              </InlineNotice>
            </>
          ) : null}
        </Panel>
        <div className="space-y-4">
          <FinancialSummary
            title="Invoice totals"
            currency={invoice.currency}
            items={[
              { label: "Subtotal", amountMinor: invoice.subtotalMinor },
              { label: "Discount", amountMinor: invoice.discountMinor },
              { label: "Taxable", amountMinor: invoice.taxableMinor },
              { label: "Tax", amountMinor: invoice.taxMinor },
              { label: "Rounding", amountMinor: invoice.roundingMinor },
              {
                label: "Total",
                amountMinor: invoice.totalMinor,
                emphasis: true,
              },
              { label: "Paid", amountMinor: invoice.paidMinor },
              { label: "Credited", amountMinor: invoice.creditedMinor },
            ]}
          />
          <Panel>
            <PanelHeader title="Tax components" />
            {taxes.length ? (
              <div className="divide-y divide-border">
                {taxes.map((tax) => (
                  <div
                    key={tax.componentCode}
                    className="flex items-center justify-between gap-3 px-4 py-3 text-sm"
                  >
                    <div>
                      <strong>{tax.componentCode}</strong>
                      <span className="block text-xs text-slate-500">
                        {tax.componentName} · {(tax.rateBp ?? 0) / 100}%
                      </span>
                    </div>
                    <Money amountMinor={tax.taxMinor} currency={tax.currency} />
                  </div>
                ))}
              </div>
            ) : (
              <EmptyState
                title="No tax components"
                description="The backend returned no configured tax lines."
              />
            )}
          </Panel>
        </div>
      </div>
      <Panel className="mt-4">
        <PanelHeader
          title="Credit and debit notes"
          description="Review drafts raised by another finance user. Issued notes remain visible and cannot be edited."
        />
        {notes.isPending ? (
          <LoadingState label="Loading invoice notes" />
        ) : notes.error ? (
          <ErrorState error={notes.error} retry={() => void notes.refetch()} />
        ) : notes.data?.data?.length ? (
          <>
            <DataTable label="Invoice credit and debit notes">
              <thead>
                <tr>
                  <TableHead>Note</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead>Reason</TableHead>
                  <TableHead>Total</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Action</TableHead>
                </tr>
              </thead>
              <tbody>
                {notes.data.data.map((note) => (
                  <tr key={note.id}>
                    <TableCell>{note.noteNumber}</TableCell>
                    <TableCell>{titleCase(note.noteType ?? "")}</TableCell>
                    <TableCell>{note.reason}</TableCell>
                    <TableCell>
                      <Money
                        amountMinor={note.totalMinor}
                        currency={note.currency}
                      />
                    </TableCell>
                    <TableCell>
                      <FinancialStatus status={note.status} />
                    </TableCell>
                    <TableCell>
                      <Button
                        size="sm"
                        onClick={() => {
                          setSelectedNote(note);
                          setNoteOpen(true);
                        }}
                      >
                        Review note
                      </Button>
                    </TableCell>
                  </tr>
                ))}
              </tbody>
            </DataTable>
            <CursorPager
              page={noteCursors.length}
              hasMore={notes.data.pagination?.hasMore}
              nextCursor={notes.data.pagination?.nextCursor}
              count={notes.data.data.length}
              noun="notes"
              onPrevious={() => setNoteCursors((items) => items.slice(0, -1))}
              onNext={(cursor) => setNoteCursors((items) => [...items, cursor])}
            />
          </>
        ) : (
          <EmptyState
            title="No credit or debit notes"
            description="Drafts and issued corrections for this invoice appear here."
          />
        )}
      </Panel>
      <ConfirmAction
        open={issueOpen}
        onOpenChange={setIssueOpen}
        title="Issue this statutory invoice?"
        description="Issuing allocates a gapless statutory number, posts the receivable, revenue and tax, and permanently freezes the header and lines."
        confirmLabel="Issue invoice"
        loading={issue.isPending}
        onConfirm={() => issue.mutate()}
      >
        {issue.error ? (
          <ErrorState error={issue.error} />
        ) : (
          <InlineNotice tone="warning" title="This cannot be edited afterwards">
            Corrections require a separately approved credit or debit note.
          </InlineNotice>
        )}
      </ConfirmAction>
      <Dialog
        open={payOpen}
        onOpenChange={setPayOpen}
        title="Record customer payment"
        description="The backend refuses overpayment and treats a repeated payment reference as an idempotent replay."
        footer={
          <>
            <Button onClick={() => setPayOpen(false)}>Cancel</Button>
            <Button
              variant="primary"
              loading={pay.isPending}
              disabled={!amount}
              onClick={() => pay.mutate()}
            >
              Record payment
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <Field label="Amount (₦)" htmlFor="invoice-payment" required>
            <Input
              id="invoice-payment"
              inputMode="decimal"
              value={amount}
              onChange={(event) => setAmount(event.target.value)}
            />
          </Field>
          <Field label="Payment mode" htmlFor="invoice-payment-mode">
            <Select
              id="invoice-payment-mode"
              value={mode}
              onChange={(event) =>
                setMode(
                  event.target.value as InvoicePaymentRequest["paymentMode"],
                )
              }
            >
              {[
                "BANK_TRANSFER",
                "UPI",
                "CARD",
                "CHEQUE",
                "CASH",
                "WALLET",
                "ADJUSTMENT",
                "OTHER",
              ].map((value) => (
                <option key={value}>{value}</option>
              ))}
            </Select>
          </Field>
          <Field
            label="Unique payment reference"
            htmlFor="invoice-reference"
            hint="Retries with the same reference return the original payment."
          >
            <Input
              id="invoice-reference"
              value={reference}
              onChange={(event) => setReference(event.target.value)}
            />
          </Field>
          {pay.error ? <ErrorState error={pay.error} /> : null}
        </div>
      </Dialog>
      <CreditNoteDialog
        key={`${selectedNote?.id ?? "new"}-${noteOpen}`}
        invoice={invoice}
        initialNote={selectedNote}
        open={noteOpen}
        onOpenChange={setNoteOpen}
      />
    </>
  );
}
