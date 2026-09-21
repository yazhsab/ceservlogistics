import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowRight,
  Ban,
  Box,
  CalendarDays,
  ChevronLeft,
  ChevronRight,
  CircleDollarSign,
  Clock3,
  Download,
  FileText,
  Pencil,
  Printer,
  Route,
  SearchX,
} from "lucide-react";
import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import Barcode from "react-barcode";
import { QRCodeSVG } from "qrcode.react";
import {
  Link,
  useNavigate,
  useParams,
  useSearchParams,
} from "react-router-dom";
import { z } from "zod";
import {
  apiRequest,
  queryString,
  type Label,
  type Shipment,
  type ShipmentCorrectionRequest,
  type ShipmentEvent,
  type ShipmentListResponse,
} from "../api/client";
import { CommercialSummary } from "./BookingCommercialFields";
import { insuranceRateLabel } from "../lib/insurance";
import { useAuth } from "../auth/AuthProvider";
import { useToast } from "../components/ToastProvider";
import {
  Badge,
  Button,
  Checkbox,
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
  SearchInput,
  Select,
  StatusBadge,
  TableCell,
  TableHead,
  Textarea,
} from "../components/ui";
import {
  formatDateTime,
  formatDimensionsCm,
  formatMoney,
  formatWeight,
  safeDownloadName,
  titleCase,
} from "../lib/utils";

const statusOptions = [
  "BOOKED",
  "PICKUP_SCHEDULED",
  "PICKUP_ASSIGNED",
  "PICKED_UP",
  "ORIGIN_BRANCH_RECEIVED",
  "ORIGIN_BAGGED",
  "ORIGIN_DISPATCHED",
  "IN_TRANSIT",
  "TRANSIT_HUB_RECEIVED",
  "TRANSIT_HUB_DISPATCHED",
  "DESTINATION_HUB_RECEIVED",
  "DESTINATION_BRANCH_RECEIVED",
  "OUT_FOR_DELIVERY",
  "DELIVERED",
  "DELIVERY_FAILED",
  "NDR",
  "RTO_INITIATED",
  "RTO_IN_TRANSIT",
  "RTO_DELIVERED",
  "CANCELLED",
  "LOST",
  "DAMAGED",
];

export function ShipmentsPage() {
  const navigate = useNavigate();
  const { hasPermission } = useAuth();
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState("");
  const [customerId, setCustomerId] = useState("");
  const [serviceCode, setServiceCode] = useState("");
  const [bookedFrom, setBookedFrom] = useState("");
  const [bookedTo, setBookedTo] = useState("");
  const [cursorStack, setCursorStack] = useState<(string | undefined)[]>([
    undefined,
  ]);
  const index = cursorStack.length - 1;
  const cursor = cursorStack[index];
  const query = useQuery({
    queryKey: [
      "shipments",
      { cursor, search, status, customerId, serviceCode, bookedFrom, bookedTo },
    ],
    queryFn: () =>
      apiRequest<ShipmentListResponse>(
        `/api/v1/shipments${queryString({ cursor, limit: 25, search, status, customerId, serviceCode, bookedFrom: bookedFrom || undefined, bookedTo: bookedTo || undefined })}`,
      ),
    placeholderData: (previous) => previous,
  });
  const rows = query.data?.data ?? [];
  const pagination = query.data?.pagination;
  const resetCursor = () => setCursorStack([undefined]);
  return (
    <>
      <PageHeader
        eyebrow="Operations"
        title="Shipments"
        description="Search by AWB or reference, filter operational state, and open the immutable booking record."
        actions={
          hasPermission("shipment.create") ? (
            <Button
              variant="primary"
              onClick={() => void navigate("/shipments/new")}
            >
              Book shipment
            </Button>
          ) : undefined
        }
      />
      <Panel>
        <div className="grid gap-3 border-b p-4 md:grid-cols-2 xl:grid-cols-[minmax(200px,1fr)_160px_150px_140px_135px_135px]">
          <SearchInput
            value={search}
            onChange={(event) => {
              setSearch(event.target.value);
              resetCursor();
            }}
            placeholder="Search AWB, reference, recipient"
            aria-label="Search shipments"
          />
          <Select
            aria-label="Filter by shipment status"
            value={status}
            onChange={(event) => {
              setStatus(event.target.value);
              resetCursor();
            }}
          >
            <option value="">All statuses</option>
            {statusOptions.map((option) => (
              <option key={option}>{option}</option>
            ))}
          </Select>
          <Input
            value={customerId}
            onChange={(event) => {
              setCustomerId(event.target.value);
              resetCursor();
            }}
            placeholder="Customer ID"
            aria-label="Filter by customer ID"
          />
          <Input
            value={serviceCode}
            onChange={(event) => {
              setServiceCode(event.target.value.toUpperCase());
              resetCursor();
            }}
            placeholder="Service code"
            aria-label="Filter by service code"
          />
          <Input
            type="date"
            value={bookedFrom}
            onChange={(event) => {
              setBookedFrom(event.target.value);
              resetCursor();
            }}
            aria-label="Booked from"
          />
          <Input
            type="date"
            value={bookedTo}
            onChange={(event) => {
              setBookedTo(event.target.value);
              resetCursor();
            }}
            aria-label="Booked to"
          />
        </div>
        {query.isLoading ? (
          <LoadingState label="Loading shipments" />
        ) : query.error ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : rows.length === 0 ? (
          <EmptyState
            icon={SearchX}
            title="No shipments found"
            description="Adjust the filters or book a new shipment."
          />
        ) : (
          <>
            <DataTable label="Shipments">
              <thead>
                <tr>
                  <TableHead>AWB & reference</TableHead>
                  <TableHead>Customer</TableHead>
                  <TableHead>Lane</TableHead>
                  <TableHead>Service</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Weight</TableHead>
                  <TableHead className="text-right">Amount</TableHead>
                  <TableHead>Booked</TableHead>
                  <TableHead>Action</TableHead>
                </tr>
              </thead>
              <tbody>
                {rows.map((shipment) => (
                  <tr
                    key={shipment.id}
                    onClick={() => void navigate(`/shipments/${shipment.id}`)}
                    className="cursor-pointer hover:bg-slate-50"
                  >
                    <TableCell>
                      <Link
                        to={`/shipments/${shipment.id}`}
                        onClick={(event) => event.stopPropagation()}
                        className="font-mono font-semibold text-primary hover:underline"
                      >
                        {shipment.awb}
                      </Link>
                      <span className="mt-0.5 block text-xs text-slate-500">
                        {shipment.referenceNumber || "No reference"}
                      </span>
                    </TableCell>
                    <TableCell>
                      <span className="font-medium">
                        {shipment.customer?.name}
                      </span>
                      <span className="block text-xs text-slate-500">
                        {shipment.customer?.code}
                      </span>
                    </TableCell>
                    <TableCell>
                      <span>{shipment.originPincode}</span>
                      <ArrowRight
                        aria-hidden
                        className="mx-1.5 inline h-3.5 w-3.5 text-slate-400"
                      />
                      <span>{shipment.destinationPincode}</span>
                      <span className="mt-0.5 block text-xs text-slate-500">
                        {shipment.recipientCity}
                      </span>
                    </TableCell>
                    <TableCell>
                      {shipment.service?.code}
                      <span className="block text-xs text-slate-500">
                        {shipment.service?.name}
                      </span>
                    </TableCell>
                    <TableCell>
                      <StatusBadge status={shipment.status} />
                    </TableCell>
                    <TableCell>
                      {formatWeight(shipment.chargeableWeightGrams)}
                      <span className="block text-xs text-slate-500">
                        {shipment.pieceCount} pc
                      </span>
                    </TableCell>
                    <TableCell className="text-right font-semibold">
                      {formatMoney(
                        shipment.totalAmountMinor,
                        shipment.currency,
                      )}
                    </TableCell>
                    <TableCell>{formatDateTime(shipment.bookedAt)}</TableCell>
                    <TableCell>
                      {shipment.status === "BOOKED" &&
                      hasPermission("shipment.edit") ? (
                        <Button
                          size="sm"
                          onClick={(event) => {
                            event.stopPropagation();
                            void navigate(`/shipments/${shipment.id}?edit=1`);
                          }}
                          aria-label={`Edit shipment ${shipment.awb}`}
                        >
                          <Pencil aria-hidden className="h-3.5 w-3.5" /> Edit
                        </Button>
                      ) : (
                        <span className="text-xs text-slate-400">Locked</span>
                      )}
                    </TableCell>
                  </tr>
                ))}
              </tbody>
            </DataTable>
            <div className="flex items-center justify-between border-t px-4 py-3">
              <p className="text-xs text-slate-500">
                Cursor page {index + 1} · {rows.length} shipments
              </p>
              <div className="flex gap-1">
                <Button
                  size="sm"
                  disabled={index === 0}
                  onClick={() => setCursorStack((items) => items.slice(0, -1))}
                >
                  <ChevronLeft aria-hidden className="h-4 w-4" /> Previous
                </Button>
                <Button
                  size="sm"
                  disabled={!pagination?.hasMore || !pagination.nextCursor}
                  onClick={() => {
                    if (pagination?.nextCursor)
                      setCursorStack((items) => [
                        ...items,
                        pagination.nextCursor,
                      ]);
                  }}
                >
                  Next <ChevronRight aria-hidden className="h-4 w-4" />
                </Button>
              </div>
            </div>
          </>
        )}
      </Panel>
    </>
  );
}

type Tab =
  | "overview"
  | "route"
  | "packages"
  | "charges"
  | "tracking"
  | "addresses"
  | "activity";
interface EventsResponse {
  data?: ShipmentEvent[];
}
interface AuditResponse {
  data?: Record<string, unknown>[];
}

export function ShipmentDetailPage() {
  const { shipmentId = "" } = useParams();
  const [params, setParams] = useSearchParams();
  const { hasPermission } = useAuth();
  const { toast } = useToast();
  const client = useQueryClient();
  const [tab, setTab] = useState<Tab>("overview");
  const [cancelOpen, setCancelOpen] = useState(false);
  const [labelOpen, setLabelOpen] = useState(params.get("label") === "1");
  const [editOpen, setEditOpen] = useState(params.get("edit") === "1");
  const query = useQuery({
    queryKey: ["shipment", shipmentId],
    queryFn: () => apiRequest<Shipment>(`/api/v1/shipments/${shipmentId}`),
  });
  useEffect(() => {
    if (params.get("label") === "1") {
      setLabelOpen(true);
      setParams({}, { replace: true });
    }
    if (params.get("edit") === "1") {
      setEditOpen(true);
      setParams({}, { replace: true });
    }
  }, [params, setParams]);
  const cancel = useMutation({
    mutationFn: (reason: string) =>
      apiRequest<Shipment>(`/api/v1/shipments/${shipmentId}/cancel`, {
        method: "POST",
        body: { reason },
      }),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ["shipment", shipmentId] });
      void client.invalidateQueries({ queryKey: ["shipments"] });
      toast({ tone: "success", title: "Shipment cancelled" });
      setCancelOpen(false);
    },
  });
  if (query.isLoading) return <LoadingState label="Loading shipment" />;
  if (query.error || !query.data)
    return (
      <ErrorState
        error={query.error ?? new Error("Shipment not found")}
        retry={() => void query.refetch()}
      />
    );
  const shipment = query.data;
  const tabs: { id: Tab; label: string }[] = [
    { id: "overview", label: "Overview" },
    { id: "route", label: "Route" },
    { id: "packages", label: "Packages" },
    { id: "charges", label: "Charges" },
    { id: "tracking", label: "Tracking events" },
    { id: "addresses", label: "Customer & address" },
    ...(hasPermission("audit.read")
      ? [{ id: "activity" as const, label: "Activity" }]
      : []),
  ];
  return (
    <>
      <PageHeader
        eyebrow="Operations / Shipments"
        title={shipment.awb ?? "Shipment"}
        description={`${shipment.referenceNumber ? `Reference ${shipment.referenceNumber} · ` : ""}${shipment.customer?.code} · ${shipment.customer?.name}`}
        actions={
          <>
            <StatusBadge status={shipment.status} />
            {shipment.status !== "CANCELLED" &&
            hasPermission("shipment.label") ? (
              <Button onClick={() => setLabelOpen(true)}>
                <Printer aria-hidden className="h-4 w-4" /> Label
              </Button>
            ) : null}
            {shipment.status === "BOOKED" &&
            hasPermission("shipment.edit") ? (
              <Button onClick={() => setEditOpen(true)}>
                <Pencil aria-hidden className="h-4 w-4" /> Edit
              </Button>
            ) : null}
            {shipment.allowedTransitions?.includes("CANCELLED") &&
            hasPermission("shipment.cancel") ? (
              <Button variant="danger" onClick={() => setCancelOpen(true)}>
                <Ban aria-hidden className="h-4 w-4" /> Cancel
              </Button>
            ) : null}
          </>
        }
      />
      <div className="mb-5 flex gap-1 overflow-x-auto border-b">
        {tabs.map((item) => (
          <button
            key={item.id}
            onClick={() => setTab(item.id)}
            className={`whitespace-nowrap border-b-2 px-4 py-2.5 text-sm font-semibold ${tab === item.id ? "border-primary text-primary" : "border-transparent text-slate-500"}`}
          >
            {item.label}
          </button>
        ))}
      </div>
      {tab === "overview" ? (
        <Overview shipment={shipment} />
      ) : tab === "route" ? (
        <RouteTab shipment={shipment} />
      ) : tab === "packages" ? (
        <PackagesTab shipment={shipment} />
      ) : tab === "charges" ? (
        <ChargesTab shipment={shipment} />
      ) : tab === "tracking" ? (
        <EventsTab shipmentId={shipmentId} />
      ) : tab === "addresses" ? (
        <AddressesTab shipment={shipment} />
      ) : (
        <ActivityTab shipmentId={shipmentId} />
      )}
      <CancelDialog
        open={cancelOpen}
        onOpenChange={setCancelOpen}
        loading={cancel.isPending}
        error={cancel.error}
        onConfirm={(reason) => cancel.mutate(reason)}
      />
      <EditShipmentDialog
        open={editOpen}
        onOpenChange={setEditOpen}
        shipment={shipment}
      />
      <LabelDialog
        open={labelOpen}
        onOpenChange={setLabelOpen}
        shipmentId={shipmentId}
      />
    </>
  );
}

function Overview({ shipment }: { shipment: Shipment }) {
  return (
    <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_380px]">
      <Panel>
        <PanelHeader title="Shipment summary" />
        <dl className="grid gap-px bg-border sm:grid-cols-2">
          <Item
            label="Booked at"
            value={formatDateTime(shipment.bookedAt)}
            icon={CalendarDays}
          />
          <Item
            label="Promised delivery"
            value={formatDateTime(shipment.promisedDeliveryAt)}
            icon={Clock3}
          />
          <Item
            label="Service"
            value={`${shipment.service?.code ?? ""} · ${shipment.service?.name ?? ""}`}
          />
          <Item
            label="Payment"
            value={titleCase(shipment.paymentMode ?? "—")}
          />
          <Item label="Pieces" value={String(shipment.pieceCount ?? 0)} />
          <Item
            label="Chargeable weight"
            value={formatWeight(shipment.chargeableWeightGrams)}
          />
          <Item label="Contents" value={shipment.contentDescription ?? "—"} />
          <Item
            label="Special handling"
            value={
              [
                shipment.isFragile ? "Fragile" : "",
                shipment.isDangerousGoods ? "Dangerous goods" : "",
                shipment.insuranceRequired ? "Insurance requested" : "",
              ]
                .filter(Boolean)
                .join(", ") || "Standard"
            }
          />
        </dl>
      </Panel>
      <Panel>
        <PanelHeader title="Route at booking" />
        <div className="p-4">
          <div className="space-y-4">
            <Location
              label="Origin"
              pincode={shipment.origin?.pincode}
              branch={shipment.origin?.branch?.code}
              hub={shipment.origin?.hub?.code}
            />
            <div className="ml-4 h-8 border-l-2 border-dashed border-slate-300" />
            <Location
              label="Destination"
              pincode={shipment.destination?.pincode}
              branch={shipment.destination?.branch?.code}
              hub={shipment.destination?.hub?.code}
            />
          </div>
        </div>
      </Panel>
      {shipment.commercial ? (
        <Panel className="xl:col-span-2">
          <PanelHeader
            title="Insurance, billing & customs"
            description="Recorded at booking. These values preserve the customer's instructions."
          />
          <div className="space-y-4 p-4">
            <dl className="grid gap-4 sm:grid-cols-3">
              <div>
                <dt className="text-xs text-muted-foreground">
                  Insurance decision
                </dt>
                <dd className="mt-1 text-sm font-semibold">
                  {titleCase(shipment.commercial.insurance.status)}
                </dd>
              </div>
              {shipment.commercial.insurance.quote ? (
                <div>
                  <dt className="text-xs text-muted-foreground">
                    Accepted premium
                    {insuranceRateLabel(shipment.commercial.insurance.quote)
                      ? ` · ${insuranceRateLabel(shipment.commercial.insurance.quote)} of declared value`
                      : ""}
                  </dt>
                  <dd className="mt-1 text-sm font-semibold">
                    {formatMoney(
                      shipment.commercial.insurance.quote.premiumMinor,
                      shipment.commercial.insurance.quote.currency,
                    )}
                  </dd>
                </div>
              ) : null}
              {shipment.commercial.insurance.recordedAt ? (
                <div>
                  <dt className="text-xs text-muted-foreground">
                    Decision recorded
                  </dt>
                  <dd className="mt-1 text-sm">
                    {formatDateTime(shipment.commercial.insurance.recordedAt)}
                  </dd>
                </div>
              ) : null}
            </dl>
            <CommercialSummary
              commercial={shipment.commercial}
              currency={shipment.currency}
            />
            {shipment.commercial.insurance.quote?.rules?.length ? (
              <ul className="space-y-1 text-xs text-muted-foreground">
                {shipment.commercial.insurance.quote.rules.map((rule) => (
                  <li key={rule.ruleId}>
                    <strong>{rule.name}:</strong> {rule.explanation}
                    {rule.isTaxable ? " · Taxable" : " · Tax exempt"}
                  </li>
                ))}
              </ul>
            ) : null}
            {(["transportation", "dutyTax"] as const).map((kind) =>
              shipment.commercial?.billing[kind].customerId ? (
                <p className="text-xs" key={kind}>
                  {kind === "transportation"
                    ? "Transportation"
                    : "Duty and tax"}{" "}
                  account:{" "}
                  <Link
                    className="font-medium text-primary underline"
                    to={`/customers/${shipment.commercial.billing[kind].customerId}`}
                  >
                    {shipment.commercial.billing[kind].customerId}
                  </Link>
                </p>
              ) : null,
            )}
            {shipment.commercial.customs ? (
              <>
                <dl className="grid gap-3 text-sm sm:grid-cols-3">
                  <div>
                    <dt className="text-xs text-muted-foreground">
                      Customs invoice
                    </dt>
                    <dd>
                      {shipment.commercial.customs.declaration.invoiceNumber ||
                        "—"}
                    </dd>
                  </div>
                  <div>
                    <dt className="text-xs text-muted-foreground">
                      Reason for export
                    </dt>
                    <dd>
                      {shipment.commercial.customs.declaration.reasonForExport}
                    </dd>
                  </div>
                  <div>
                    <dt className="text-xs text-muted-foreground">
                      Terms of sale
                    </dt>
                    <dd>
                      {shipment.commercial.customs.declaration.termsOfSale ||
                        "—"}
                    </dd>
                  </div>
                </dl>
                <div className="overflow-x-auto">
                  <table className="w-full text-left text-sm">
                    <caption className="sr-only">
                      Customs declaration goods
                    </caption>
                    <thead>
                      <tr>
                        {[
                          "Description of goods",
                          "Quantity",
                          "Unit value",
                          "Origin",
                          "HS code",
                          "Line total",
                        ].map((label) => (
                          <th
                            key={label}
                            className="whitespace-nowrap border-b px-3 py-2 font-medium"
                          >
                            {label}
                          </th>
                        ))}
                      </tr>
                    </thead>
                    <tbody>
                      {shipment.commercial.customs.declaration.items.map(
                        (item, index) => (
                          <tr key={index}>
                            <td className="min-w-40 border-b px-3 py-2">
                              {item.description}
                            </td>
                            <td className="whitespace-nowrap border-b px-3 py-2">
                              {item.quantity} {item.unitOfMeasure}
                            </td>
                            <td className="whitespace-nowrap border-b px-3 py-2">
                              {formatMoney(
                                item.unitValueMinor,
                                shipment.commercial?.customs?.declaration
                                  .currency,
                              )}
                            </td>
                            <td className="border-b px-3 py-2">
                              {item.countryOfOrigin}
                            </td>
                            <td className="border-b px-3 py-2">
                              {item.hsCode || "—"}
                            </td>
                            <td className="whitespace-nowrap border-b px-3 py-2">
                              {formatMoney(
                                shipment.commercial?.customs?.lineTotalsMinor[
                                  index
                                ],
                                shipment.commercial?.customs?.declaration
                                  .currency,
                              )}
                            </td>
                          </tr>
                        ),
                      )}
                    </tbody>
                  </table>
                </div>
                {shipment.commercial.customs.declaration
                  .declarationStatement ? (
                  <p className="whitespace-pre-wrap text-sm">
                    {
                      shipment.commercial.customs.declaration
                        .declarationStatement
                    }
                  </p>
                ) : null}
              </>
            ) : null}
          </div>
        </Panel>
      ) : null}
      {shipment.status === "CANCELLED" ? (
        <Panel className="xl:col-span-2">
          <div className="flex items-start gap-3 p-4">
            <Ban aria-hidden className="mt-0.5 h-5 w-5 text-danger" />
            <div>
              <p className="text-sm font-semibold">
                Cancelled {formatDateTime(shipment.cancelledAt)}
              </p>
              <p className="mt-1 text-sm text-slate-600">
                {shipment.cancellationReason}
              </p>
            </div>
          </div>
        </Panel>
      ) : null}
    </div>
  );
}
function Item({
  label,
  value,
  icon: Icon,
}: {
  label: string;
  value: string;
  icon?: typeof CalendarDays;
}) {
  return (
    <div className="bg-white p-4">
      <dt className="flex items-center gap-1.5 text-xs uppercase tracking-wide text-slate-500">
        {Icon ? <Icon aria-hidden className="h-3.5 w-3.5" /> : null}
        {label}
      </dt>
      <dd className="mt-1.5 text-sm font-medium">{value}</dd>
    </div>
  );
}
function Location({
  label,
  pincode,
  branch,
  hub,
}: {
  label: string;
  pincode?: string;
  branch?: string;
  hub?: string;
}) {
  return (
    <div className="flex items-start gap-3">
      <span className="mt-1 h-3 w-3 rounded-full bg-primary ring-4 ring-emerald-100" />
      <div>
        <p className="text-xs uppercase tracking-wide text-slate-500">
          {label}
        </p>
        <p className="mt-1 text-sm font-semibold">{pincode}</p>
        <p className="text-xs text-slate-500">
          Branch {branch} · Hub {hub}
        </p>
      </div>
    </div>
  );
}

function RouteTab({ shipment }: { shipment: Shipment }) {
  const legs = shipment.route?.legs ?? [];
  return (
    <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_340px]">
      <Panel>
        <PanelHeader
          title="Immutable route snapshot"
          description="This path remains unchanged if route configuration changes later."
        />
        <div className="p-5">
          <div className="flex flex-wrap items-center gap-2">
            <Badge
              tone={
                shipment.route?.resolutionSource === "OVERRIDE" ||
                shipment.route?.resolutionSource === "FALLBACK_ROUTE"
                  ? "warning"
                  : "info"
              }
            >
              {titleCase(shipment.route?.resolutionSource ?? "Route")}
            </Badge>
            <Badge>{shipment.route?.routeCode ?? "Local delivery"}</Badge>
          </div>
          <div className="mt-5 space-y-3">
            {legs.length ? (
              legs.map((leg) => (
                <div
                  key={leg.sequence}
                  className="grid items-center gap-3 rounded-md border p-3 sm:grid-cols-[1fr_auto_1fr_auto]"
                >
                  <div>
                    <span className="text-xs text-slate-500">From</span>
                    <p className="text-sm font-semibold">
                      {leg.from} · {leg.fromName}
                    </p>
                  </div>
                  <ArrowRight aria-hidden className="h-4 w-4 text-slate-400" />
                  <div>
                    <span className="text-xs text-slate-500">To</span>
                    <p className="text-sm font-semibold">
                      {leg.to} · {leg.toName}
                    </p>
                  </div>
                  <Badge>
                    {leg.mode} · {leg.transitHours}h
                  </Badge>
                </div>
              ))
            ) : (
              <EmptyState
                icon={Route}
                title="Local route"
                description="No line-haul legs were required for this shipment."
              />
            )}
          </div>
        </div>
      </Panel>
      <Panel>
        <PanelHeader title="Promise" />
        <dl className="divide-y">
          <Row
            label="Transit"
            value={`${shipment.route?.transitHours ?? 0} hours`}
          />
          <Row
            label="SLA"
            value={`${shipment.route?.slaHours ?? shipment.slaHours ?? 0} hours`}
          />
          <Row
            label="Promised"
            value={formatDateTime(shipment.promisedDeliveryAt)}
          />
        </dl>
      </Panel>
    </div>
  );
}
function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between gap-4 px-4 py-3">
      <dt className="text-xs text-slate-500">{label}</dt>
      <dd className="text-right text-sm font-semibold">{value}</dd>
    </div>
  );
}

function PackagesTab({ shipment }: { shipment: Shipment }) {
  return (
    <Panel>
      <PanelHeader
        title={`${shipment.packages?.length ?? 0} package pieces`}
        description="Measured values recorded at booking."
      />
      {shipment.packages?.length ? (
        <DataTable label="Shipment packages">
          <thead>
            <tr>
              <TableHead>Piece</TableHead>
              <TableHead>Barcode</TableHead>
              <TableHead>Reference</TableHead>
              <TableHead>Actual</TableHead>
              <TableHead>Volumetric</TableHead>
              <TableHead>Dimensions</TableHead>
              <TableHead>Contents</TableHead>
            </tr>
          </thead>
          <tbody>
            {shipment.packages.map((item) => (
              <tr key={item.id ?? item.sequence}>
                <TableCell>#{item.sequence}</TableCell>
                <TableCell className="font-mono text-xs">
                  {item.pieceBarcode}
                </TableCell>
                <TableCell>{item.reference || "—"}</TableCell>
                <TableCell>{formatWeight(item.actualWeightGrams)}</TableCell>
                <TableCell>
                  {formatWeight(item.volumetricWeightGrams)}
                </TableCell>
                <TableCell>
                  {formatDimensionsCm(
                    item.lengthMm,
                    item.widthMm,
                    item.heightMm,
                  )}
                </TableCell>
                <TableCell>{item.contentDescription}</TableCell>
              </tr>
            ))}
          </tbody>
        </DataTable>
      ) : (
        <EmptyState
          icon={Box}
          title="No package data"
          description="No packages were returned for this shipment."
        />
      )}
    </Panel>
  );
}

function ChargesTab({ shipment }: { shipment: Shipment }) {
  const charges = shipment.charges;
  return (
    <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_340px]">
      <Panel>
        <PanelHeader
          title="Immutable charge breakdown"
          description="Every line was calculated and rounded by the authoritative pricing engine."
        />
        {charges?.lineItems?.length ? (
          <DataTable label="Shipment charges">
            <thead>
              <tr>
                <TableHead>Charge</TableHead>
                <TableHead>Explanation</TableHead>
                <TableHead className="text-right">Amount</TableHead>
              </tr>
            </thead>
            <tbody>
              {charges.lineItems.map((line, index) => (
                <tr key={`${line.code}-${index}`}>
                  <TableCell>
                    <strong>{line.label}</strong>
                    <span className="block text-[10px] text-slate-400">
                      {line.kind}
                    </span>
                  </TableCell>
                  <TableCell className="max-w-xl text-xs">
                    {line.explanation}
                  </TableCell>
                  <TableCell
                    className={`text-right font-semibold ${Number(line.amountMinor) < 0 ? "text-success" : ""}`}
                  >
                    {formatMoney(line.amountMinor, charges.currency)}
                  </TableCell>
                </tr>
              ))}
            </tbody>
          </DataTable>
        ) : (
          <EmptyState
            icon={CircleDollarSign}
            title="No charge lines"
            description="The API did not return the price snapshot lines."
          />
        )}
      </Panel>
      <Panel>
        <PanelHeader title="Price record" />
        <dl className="divide-y">
          <Row
            label="Rate card"
            value={`${charges?.rateCardCode ?? "—"} v${charges?.rateCardVersion ?? "—"}`}
          />
          <Row
            label="Zones"
            value={`${charges?.originZoneCode ?? "—"} → ${charges?.destinationZoneCode ?? "—"}`}
          />
          <Row
            label="Freight"
            value={formatMoney(charges?.freightMinor, charges?.currency)}
          />
          <Row
            label="Surcharges"
            value={formatMoney(charges?.surchargeTotalMinor, charges?.currency)}
          />
          <Row
            label="Discounts"
            value={formatMoney(charges?.discountTotalMinor, charges?.currency)}
          />
          <Row
            label="Tax"
            value={formatMoney(charges?.taxTotalMinor, charges?.currency)}
          />
          <div className="flex justify-between bg-slate-50 p-4">
            <dt className="font-semibold">Final total</dt>
            <dd className="text-xl font-bold">
              {formatMoney(
                charges?.totalMinor ?? shipment.totalAmountMinor,
                charges?.currency ?? shipment.currency,
              )}
            </dd>
          </div>
        </dl>
      </Panel>
    </div>
  );
}

function EventsTab({ shipmentId }: { shipmentId: string }) {
  const query = useQuery({
    queryKey: ["shipment-events", shipmentId],
    queryFn: () =>
      apiRequest<EventsResponse>(
        `/api/v1/shipments/${shipmentId}/events?limit=100`,
      ),
  });
  if (query.isLoading)
    return (
      <Panel>
        <LoadingState />
      </Panel>
    );
  if (query.error)
    return (
      <Panel>
        <ErrorState error={query.error} />
      </Panel>
    );
  const events = query.data?.data ?? [];
  return (
    <Panel>
      <PanelHeader
        title="Tracking events"
        description="Append-only, oldest first. Descriptions are customer-safe."
      />
      {events.length ? (
        <ol className="p-5">
          {events.map((event, index) => (
            <li
              key={event.id ?? index}
              className="relative grid gap-1 border-l-2 border-slate-200 pb-6 pl-6 last:border-transparent last:pb-0 sm:grid-cols-[190px_minmax(0,1fr)]"
            >
              <span className="absolute -left-[7px] top-1 h-3 w-3 rounded-full border-2 border-white bg-primary" />
              <time className="text-xs text-slate-500">
                {formatDateTime(event.occurredAt)}
              </time>
              <div>
                <div className="flex flex-wrap items-center gap-2">
                  <strong className="text-sm">
                    {titleCase(event.eventType ?? "Event")}
                  </strong>
                  {event.toStatus ? (
                    <StatusBadge status={event.toStatus} />
                  ) : null}
                </div>
                <p className="mt-1 text-sm text-slate-700">
                  {event.description}
                </p>
                <p className="mt-1 text-xs text-slate-500">
                  {event.operatingUnitCode
                    ? `${event.operatingUnitCode} · `
                    : ""}
                  {event.actorName ?? event.actorType}
                </p>
              </div>
            </li>
          ))}
        </ol>
      ) : (
        <EmptyState
          icon={Clock3}
          title="No events"
          description="No tracking events were returned."
        />
      )}
    </Panel>
  );
}

function AddressesTab({ shipment }: { shipment: Shipment }) {
  return (
    <div className="grid gap-5 xl:grid-cols-3">
      <Panel>
        <PanelHeader title="Customer" />
        <div className="p-4">
          <p className="text-sm font-semibold">{shipment.customer?.name}</p>
          <p className="mt-1 text-xs text-slate-500">
            {shipment.customer?.code}
          </p>
        </div>
      </Panel>
      <AddressCard
        title="Sender snapshot"
        address={shipment.addresses?.sender}
      />
      <AddressCard
        title="Recipient snapshot"
        address={shipment.addresses?.recipient}
      />
    </div>
  );
}
function AddressCard({
  title,
  address,
}: {
  title: string;
  address?: Record<string, unknown>;
}) {
  return (
    <Panel>
      <PanelHeader
        title={title}
        description="Current waybill details; original retained in audit history"
      />
      <div className="p-4 text-sm leading-6">
        <strong>{string(address, "contactName", "name")}</strong>
        <br />
        {string(address, "companyName", "company") ? (
          <>
            {string(address, "companyName", "company")}
            <br />
          </>
        ) : null}
        {string(address, "line1")}
        <br />
        {string(address, "line2") ? (
          <>
            {string(address, "line2")}
            <br />
          </>
        ) : null}
        {string(address, "city")}, {string(address, "state")}{" "}
        {string(address, "pincode")}
        <p className="mt-2 text-xs text-slate-500">
          {string(address, "phone")}
        </p>
      </div>
    </Panel>
  );
}

function ActivityTab({ shipmentId }: { shipmentId: string }) {
  const query = useQuery({
    queryKey: ["shipment-audit", shipmentId],
    queryFn: () =>
      apiRequest<AuditResponse>(
        `/api/v1/audit-events${queryString({ resourceType: "SHIPMENT", resourceId: shipmentId, limit: 50 })}`,
      ),
  });
  if (query.isLoading)
    return (
      <Panel>
        <LoadingState />
      </Panel>
    );
  if (query.error)
    return (
      <Panel>
        <ErrorState error={query.error} />
      </Panel>
    );
  return (
    <Panel>
      <PanelHeader
        title="Audit activity"
        description="Append-only administrative record."
      />
      <div className="divide-y">
        {query.data?.data?.map((record, index) => (
          <div
            key={string(record, "id") || index}
            className="grid gap-2 px-4 py-3 sm:grid-cols-[180px_180px_1fr]"
          >
            <span className="text-xs text-slate-500">
              {formatDateTime(string(record, "occurredAt"))}
            </span>
            <span className="text-sm font-semibold">
              {titleCase(string(record, "action"))}
            </span>
            <span className="text-sm text-slate-600">
              {string(record, "actorName", "actorEmail") || "System"}
            </span>
          </div>
        )) ?? (
          <EmptyState
            icon={FileText}
            title="No audit activity"
            description="No audit events were returned."
          />
        )}
      </div>
    </Panel>
  );
}

const correctionAddressSchema = z.object({
  contactName: z.string().trim().min(2).max(160),
  companyName: z.string().trim().max(160),
  phone: z.string().trim().min(7).max(24),
  altPhone: z.string().trim().max(24),
  email: z.union([z.literal(""), z.string().trim().email()]),
  line1: z.string().trim().min(3).max(200),
  line2: z.string().trim().max(200),
  landmark: z.string().trim().max(120),
});

const shipmentCorrectionSchema = z.object({
  reason: z.string().trim().min(5).max(500),
  referenceNumber: z.string().trim().max(64),
  contentDescription: z.string().trim().min(2).max(500),
  specialInstructions: z.string().trim().max(1000),
  isFragile: z.boolean(),
  sender: correctionAddressSchema,
  recipient: correctionAddressSchema,
});

type ShipmentCorrectionForm = z.infer<typeof shipmentCorrectionSchema>;

function correctionDefaults(shipment: Shipment): ShipmentCorrectionForm {
  const address = (role: "sender" | "recipient", key: string) =>
    string(shipment.addresses?.[role], key);
  const correctedAddress = (role: "sender" | "recipient") => ({
    contactName: address(role, "contactName"),
    companyName: address(role, "companyName"),
    phone: address(role, "phone"),
    altPhone: address(role, "altPhone"),
    email: address(role, "email"),
    line1: address(role, "line1"),
    line2: address(role, "line2"),
    landmark: address(role, "landmark"),
  });
  return {
    reason: "",
    referenceNumber: shipment.referenceNumber ?? "",
    contentDescription: shipment.contentDescription ?? "",
    specialInstructions: shipment.specialInstructions ?? "",
    isFragile: shipment.isFragile ?? false,
    sender: correctedAddress("sender"),
    recipient: correctedAddress("recipient"),
  };
}

function EditShipmentDialog({
  open,
  onOpenChange,
  shipment,
}: {
  open: boolean;
  onOpenChange: (value: boolean) => void;
  shipment: Shipment;
}) {
  const client = useQueryClient();
  const { toast } = useToast();
  const form = useForm<ShipmentCorrectionForm>({
    resolver: zodResolver(shipmentCorrectionSchema),
    defaultValues: correctionDefaults(shipment),
  });
  useEffect(() => {
    if (open) form.reset(correctionDefaults(shipment));
  }, [form, open, shipment]);
  const mutation = useMutation({
    mutationFn: (values: ShipmentCorrectionForm) =>
      apiRequest<Shipment>(`/api/v1/shipments/${shipment.id}`, {
        method: "PATCH",
        body: {
          ...values,
          expectedVersion: shipment.version ?? 1,
        } satisfies ShipmentCorrectionRequest,
      }),
    onSuccess: (updated) => {
      client.setQueryData(["shipment", shipment.id], updated);
      void client.invalidateQueries({ queryKey: ["shipments"] });
      void client.invalidateQueries({
        queryKey: ["shipment-label", shipment.id],
      });
      toast({
        tone: "success",
        title: "Shipment corrected",
        description: `${shipment.awb} keeps the same tracking number. Reprint its labels.`,
      });
      onOpenChange(false);
    },
  });
  const errors = form.formState.errors;
  const lockedAddress = (role: "sender" | "recipient") => {
    const address = shipment.addresses?.[role];
    return [
      string(address, "city"),
      string(address, "state"),
      string(address, "pincode"),
      string(address, "countryCode"),
    ]
      .filter(Boolean)
      .join(" · ");
  };
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={`Edit shipment ${shipment.awb ?? ""}`}
      description="Correct non-routing details before pickup. The AWB stays the same and the change is recorded in shipment history."
      footer={
        <>
          <Button onClick={() => onOpenChange(false)}>Keep unchanged</Button>
          <Button
            variant="primary"
            loading={mutation.isPending}
            onClick={() => void form.handleSubmit((values) => mutation.mutate(values))()}
          >
            Save correction
          </Button>
        </>
      }
    >
      <form
        className="space-y-5"
        onSubmit={(event) => {
          event.preventDefault();
          void form.handleSubmit((values) => mutation.mutate(values))();
        }}
      >
        <div className="rounded-md border border-info/30 bg-blue-50 px-3 py-2 text-xs leading-5 text-slate-700">
          Postal codes, cities, service, weight, payment, declared value and
          price stay locked because they determine the route and charge.
        </div>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field
            label="Customer reference"
            htmlFor="correction-reference"
            hint="Optional"
            error={errors.referenceNumber?.message}
          >
            <Input
              id="correction-reference"
              {...form.register("referenceNumber")}
            />
          </Field>
          <Field
            label="Correction reason"
            htmlFor="correction-reason"
            required
            error={errors.reason?.message}
          >
            <Input
              id="correction-reason"
              placeholder="Example: corrected recipient phone"
              {...form.register("reason")}
            />
          </Field>
        </div>
        <Field
          label="Contents description"
          htmlFor="correction-contents"
          required
          error={errors.contentDescription?.message}
        >
          <Input
            id="correction-contents"
            {...form.register("contentDescription")}
          />
        </Field>
        <Field
          label="Special instructions"
          htmlFor="correction-instructions"
          hint="Optional"
          error={errors.specialInstructions?.message}
        >
          <Textarea
            id="correction-instructions"
            className="min-h-20"
            {...form.register("specialInstructions")}
          />
        </Field>
        <label className="flex items-center gap-2 text-sm font-medium">
          <Checkbox
            aria-label="Fragile shipment"
            checked={form.watch("isFragile")}
            onCheckedChange={(checked) =>
              form.setValue("isFragile", checked, { shouldDirty: true })
            }
          />
          Fragile shipment
        </label>

        {(["sender", "recipient"] as const).map((role) => (
          <fieldset key={role} className="space-y-4 border-t pt-4">
            <legend className="text-sm font-semibold capitalize">
              {role} details
            </legend>
            <p className="text-xs text-slate-500">
              Locked route: {lockedAddress(role)}
            </p>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field
                label="Contact name"
                htmlFor={`${role}-correction-name`}
                required
                error={errors[role]?.contactName?.message}
              >
                <Input
                  id={`${role}-correction-name`}
                  {...form.register(`${role}.contactName`)}
                />
              </Field>
              <Field
                label="Company"
                htmlFor={`${role}-correction-company`}
                hint="Optional"
                error={errors[role]?.companyName?.message}
              >
                <Input
                  id={`${role}-correction-company`}
                  {...form.register(`${role}.companyName`)}
                />
              </Field>
              <Field
                label="Phone"
                htmlFor={`${role}-correction-phone`}
                required
                error={errors[role]?.phone?.message}
              >
                <Input
                  id={`${role}-correction-phone`}
                  inputMode="tel"
                  {...form.register(`${role}.phone`)}
                />
              </Field>
              <Field
                label="Alternative phone"
                htmlFor={`${role}-correction-alt-phone`}
                hint="Optional"
                error={errors[role]?.altPhone?.message}
              >
                <Input
                  id={`${role}-correction-alt-phone`}
                  inputMode="tel"
                  {...form.register(`${role}.altPhone`)}
                />
              </Field>
              <Field
                label="Email"
                htmlFor={`${role}-correction-email`}
                hint="Optional"
                error={errors[role]?.email?.message}
              >
                <Input
                  id={`${role}-correction-email`}
                  type="email"
                  {...form.register(`${role}.email`)}
                />
              </Field>
              <Field
                label="Landmark"
                htmlFor={`${role}-correction-landmark`}
                hint="Optional"
                error={errors[role]?.landmark?.message}
              >
                <Input
                  id={`${role}-correction-landmark`}
                  {...form.register(`${role}.landmark`)}
                />
              </Field>
            </div>
            <Field
              label="Address line 1"
              htmlFor={`${role}-correction-line1`}
              required
              error={errors[role]?.line1?.message}
            >
              <Input
                id={`${role}-correction-line1`}
                {...form.register(`${role}.line1`)}
              />
            </Field>
            <Field
              label="Address line 2"
              htmlFor={`${role}-correction-line2`}
              hint="Optional"
              error={errors[role]?.line2?.message}
            >
              <Input
                id={`${role}-correction-line2`}
                {...form.register(`${role}.line2`)}
              />
            </Field>
          </fieldset>
        ))}
        {mutation.error ? (
          <p role="alert" className="text-sm text-danger">
            {mutation.error.message}
          </p>
        ) : null}
      </form>
    </Dialog>
  );
}

function CancelDialog({
  open,
  onOpenChange,
  onConfirm,
  loading,
  error,
}: {
  open: boolean;
  onOpenChange: (value: boolean) => void;
  onConfirm: (reason: string) => void;
  loading: boolean;
  error: Error | null;
}) {
  const schema = z.object({
    reason: z
      .string()
      .min(5, "Explain the reason in at least five characters.")
      .max(500),
  });
  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<{ reason: string }>({ resolver: zodResolver(schema) });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Cancel shipment"
      description="Cancellation is terminal and releases reserved credit for credit bookings."
      footer={
        <>
          <Button onClick={() => onOpenChange(false)}>Keep shipment</Button>
          <Button
            variant="danger"
            loading={loading}
            onClick={() =>
              void handleSubmit((values) => onConfirm(values.reason))()
            }
          >
            Cancel shipment
          </Button>
        </>
      }
    >
      <Field
        label="Cancellation reason"
        htmlFor="cancelReason"
        required
        error={errors.reason?.message}
      >
        <Input id="cancelReason" autoFocus {...register("reason")} />
      </Field>
      {error ? (
        <p role="alert" className="mt-3 text-sm text-danger">
          {error.message}
        </p>
      ) : null}
    </Dialog>
  );
}

function LabelDialog({
  open,
  onOpenChange,
  shipmentId,
}: {
  open: boolean;
  onOpenChange: (value: boolean) => void;
  shipmentId: string;
}) {
  const [printFormat, setPrintFormat] = useState<"STICKER" | "COURIER_SHEET">(
    "STICKER",
  );
  const query = useQuery({
    queryKey: ["shipment-label", shipmentId],
    queryFn: () =>
      apiRequest<Label>(`/api/v1/shipments/${shipmentId}/label?format=JSON`),
    enabled: open,
  });
  const downloadZpl = async () => {
    const zpl = await apiRequest<string>(
      `/api/v1/shipments/${shipmentId}/label?format=ZPL`,
    );
    const href = URL.createObjectURL(new Blob([zpl], { type: "text/plain" }));
    const anchor = document.createElement("a");
    anchor.href = href;
    anchor.download = `${safeDownloadName(query.data?.awb, "shipment")}.zpl`;
    anchor.click();
    URL.revokeObjectURL(href);
  };
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Shipment label"
      description="Print one scannable label per package as a 4 × 6 sticker or a two-up A4 courier sheet."
      footer={
        <>
          <Select
            aria-label="Print format"
            className="no-print"
            value={printFormat}
            onChange={(event) =>
              setPrintFormat(event.target.value as typeof printFormat)
            }
          >
            <option value="STICKER">4 × 6 sticker labels</option>
            <option value="COURIER_SHEET">A4 courier sheet</option>
          </Select>
          <Button onClick={() => void downloadZpl()} disabled={!query.data}>
            <Download aria-hidden className="h-4 w-4" /> Download ZPL
          </Button>
          <Button
            variant="primary"
            onClick={() => window.print()}
            disabled={!query.data}
          >
            <Printer aria-hidden className="h-4 w-4" /> Print all pieces
          </Button>
        </>
      }
    >
      {query.isLoading ? (
        <LoadingState label="Generating label" />
      ) : query.error ? (
        <ErrorState error={query.error} retry={() => void query.refetch()} />
      ) : query.data ? (
        <ShippingLabel label={query.data} printFormat={printFormat} />
      ) : null}
    </Dialog>
  );
}

function ShippingLabel({
  label,
  printFormat,
}: {
  label: Label;
  printFormat: "STICKER" | "COURIER_SHEET";
}) {
  const pieces = label.pieces?.length
    ? label.pieces
    : [{ sequence: 1, barcode: label.barcodePayload }];
  return (
    <div
      className={`label-print-sheet label-format-${printFormat.toLowerCase()} space-y-4 print:space-y-0`}
    >
      {pieces.map((piece) => (
        <PieceShippingLabel
          key={piece.barcode ?? `${label.awb}-${piece.sequence}`}
          label={label}
          piece={piece}
          total={pieces.length}
        />
      ))}
    </div>
  );
}

function PieceShippingLabel({
  label,
  piece,
  total,
}: {
  label: Label;
  piece: NonNullable<Label["pieces"]>[number];
  total: number;
}) {
  const pieceBarcode = piece.barcode ?? label.awb ?? "";
  const pieceQrPayload = `${label.qrPayload ?? "CSV1|"}|${piece.sequence}|${pieceBarcode}`;
  return (
    <div className="shipping-label-page mx-auto aspect-[2/3] w-full max-w-[420px] border-2 border-slate-950 bg-white p-4 text-slate-950">
      <div className="flex items-start justify-between border-b-2 border-slate-950 pb-3">
        <div>
          <strong className="text-xl tracking-wide">CESERVE</strong>
          <p className="text-[10px] uppercase tracking-widest">Courier</p>
        </div>
        <div className="text-right">
          <strong className="block text-2xl">{label.serviceCode}</strong>
          <span className="text-xs">{label.serviceMode}</span>
        </div>
      </div>
      <div className="border-b-2 border-slate-950 py-2 text-center">
        <p className="text-[10px] font-bold uppercase">Routing code</p>
        <p className="mt-1 text-3xl font-black tracking-wide">
          {label.routingCode}
        </p>
      </div>
      <div className="grid grid-cols-[1fr_88px] gap-3 border-b py-3">
        <div>
          <p className="text-[10px] font-bold uppercase">Deliver to</p>
          <p className="mt-1 text-sm font-bold">{label.recipient?.name}</p>
          <p className="text-xs leading-5">
            {label.recipient?.line1}
            <br />
            {label.recipient?.city}, {label.recipient?.state}
            <br />
            <strong className="text-lg">{label.recipient?.pincode}</strong>
            <br />
            {label.recipient?.phone}
          </p>
        </div>
        {label.qrPayload ? (
          <QRCodeSVG value={pieceQrPayload} size={84} level="M" />
        ) : null}
      </div>
      <div className="grid grid-cols-2 gap-3 border-b py-2 text-[10px]">
        <div>
          <strong className="block uppercase">From</strong>
          <span>{label.sender?.name}</span>
          <br />
          <span>{label.sender?.line1}</span>
          <br />
          <span>
            {label.sender?.city}, {label.sender?.state}
          </span>
        </div>
        <div>
          <strong className="block uppercase">Shipment</strong>
          <span>AWB: {label.awb}</span>
          <br />
          <span>Contents: {label.contentDescription}</span>
          {label.referenceNumber ? (
            <>
              <br />
              <span>Reference: {label.referenceNumber}</span>
            </>
          ) : null}
        </div>
      </div>
      <div className="py-2 text-center">
        {label.barcodePayload ? (
          <Barcode
            value={pieceBarcode}
            format="CODE128"
            height={54}
            width={1.6}
            margin={0}
            fontSize={14}
          />
        ) : null}
      </div>
      <div className="grid grid-cols-3 gap-px bg-slate-950 text-center text-xs">
        <div className="bg-white p-2">
          <span className="block text-[9px] uppercase">Package</span>
          <strong>
            {piece.sequence} of {total}
          </strong>
        </div>
        <div className="bg-white p-2">
          <span className="block text-[9px] uppercase">Weight</span>
          <strong>
            {piece.actualWeightGrams
              ? formatWeight(piece.actualWeightGrams)
              : label.weightLabel}
          </strong>
        </div>
        <div className="bg-white p-2">
          <span className="block text-[9px] uppercase">Payment</span>
          <strong>{label.paymentMode}</strong>
        </div>
      </div>
      <div className="mt-2 flex justify-between text-[9px]">
        <span>
          {label.originBranchCode} → {label.destinationBranchCode}
        </span>
        <span>{label.isFragile ? "FRAGILE" : ""}</span>
      </div>
    </div>
  );
}

function string(
  record: Record<string, unknown> | undefined,
  ...keys: string[]
) {
  for (const key of keys) {
    const value = record?.[key];
    if (typeof value === "string" || typeof value === "number")
      return String(value);
  }
  return "";
}
