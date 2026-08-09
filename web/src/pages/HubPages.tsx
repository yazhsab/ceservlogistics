import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertTriangle,
  ArrowDownToLine,
  Boxes,
  CheckCircle2,
  CircleAlert,
  ClipboardCheck,
  Keyboard,
  Maximize2,
  Minimize2,
  PackageCheck,
  PackageX,
  ScanLine,
  Send,
  Truck,
} from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useForm } from "react-hook-form";
import { useNavigate, useParams } from "react-router-dom";
import { z } from "zod";
import {
  apiRequest,
  operationalHeaders,
  queryString,
  type ConsoleBagResponse,
  type ConsoleFacilitySummary,
  type ConsoleInboundResponse,
  type ConsoleParcel,
  type ConsoleQueueResponse,
  type CustodyPage,
  type ExceptionPage,
  type ExceptionSummary,
  type HubInbound,
  type HubWorkloadSummary,
  type OperationalException,
  type Reconciliation,
  type ReconciliationPage,
  type ReconciliationScanResult,
} from "../api/client";
import { useAuth } from "../auth/AuthProvider";
import { Money } from "../components/financial";
import { useToast } from "../components/ToastProvider";
import {
  CursorPager,
  EntityLink,
  OperationalMetricStrip,
  ScannerInput,
  type ScannerInputHandle,
} from "../components/operations";
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
  Panel,
  PanelHeader,
  Select,
  StatusBadge,
  Switch,
  TableCell,
  TableHead,
  Textarea,
} from "../components/ui";
import { formatDateTime, formatWeight, titleCase } from "../lib/utils";

type HubWorkspace =
  | "inbound"
  | "reconciliation"
  | "sorting"
  | "outbound"
  | "exceptions"
  | "custody";
const workspaces: Array<{ id: HubWorkspace; label: string }> = [
  { id: "inbound", label: "Inbound" },
  { id: "reconciliation", label: "Reconciliation" },
  { id: "sorting", label: "Sorting" },
  { id: "outbound", label: "Outbound" },
  { id: "exceptions", label: "Exceptions" },
  { id: "custody", label: "Custody" },
];
const terminalNavigation: Array<{
  id: TerminalView;
  label: string;
}> = [
  { id: "inbound", label: "Inbound" },
  { id: "sort", label: "Sort" },
  { id: "bag", label: "Bag" },
  { id: "manifest", label: "Manifest" },
  { id: "outbound", label: "Outbound" },
  { id: "exceptions", label: "Exceptions" },
];
type TerminalView =
  "inbound" | "sort" | "bag" | "manifest" | "outbound" | "exceptions";

export function HubConsolePage() {
  const navigate = useNavigate();
  const { hasPermission, user } = useAuth();
  const [facilityId, setFacilityId] = useState(
    () =>
      localStorage.getItem("courier.hub-facility") ??
      user?.operatingUnitIds?.[0] ??
      "",
  );
  const [workspace, setWorkspace] = useState<HubWorkspace>("inbound");
  const canUseTerminal = hasPermission("portal.console");
  const canViewHub = hasPermission("hub.dashboard");
  const [terminalView, setTerminalView] = useState<TerminalView>("inbound");
  const [terminalMode, setTerminalMode] = useState(
    () =>
      canUseTerminal &&
      (!canViewHub ||
        localStorage.getItem("courier.hub-terminal-mode") === "true"),
  );
  const summary = useQuery({
    queryKey: ["hub-summary", facilityId],
    queryFn: () =>
      apiRequest<HubWorkloadSummary>(
        `/api/v1/hub/summary${queryString({ operatingUnitId: facilityId })}`,
      ),
    refetchInterval: 30_000,
    enabled: !terminalMode && canViewHub,
  });
  const updateFacility = (value: string) => {
    setFacilityId(value);
    localStorage.setItem("courier.hub-facility", value);
  };
  const updateTerminalMode = (enabled: boolean) => {
    setTerminalMode(enabled);
    localStorage.setItem("courier.hub-terminal-mode", String(enabled));
  };
  useEffect(() => {
    if (!canUseTerminal && terminalMode) updateTerminalMode(false);
  }, [canUseTerminal, terminalMode]);
  useEffect(() => {
    const keyboard = (event: KeyboardEvent) => {
      if (event.altKey && event.key.toLowerCase() === "t" && canUseTerminal) {
        event.preventDefault();
        updateTerminalMode(!terminalMode);
        return;
      }
      if (!terminalMode) return;
      if (event.key === "Escape") {
        event.preventDefault();
        updateTerminalMode(false);
        return;
      }
      const index = event.altKey ? Number(event.key) - 1 : -1;
      const item = terminalNavigation[index];
      if (!item) return;
      event.preventDefault();
      setTerminalView(item.id);
    };
    window.addEventListener("keydown", keyboard);
    return () => window.removeEventListener("keydown", keyboard);
  }, [canUseTerminal, terminalMode]);
  return (
    <div
      className={
        terminalMode
          ? "fixed inset-0 z-[80] overflow-y-auto bg-slate-100 p-3 sm:p-5"
          : undefined
      }
    >
      <PageHeader
        eyebrow={
          terminalMode ? "Terminal · Current facility" : "Operations · Hub"
        }
        title={terminalMode ? "Hub / branch terminal" : "Hub console"}
        description={
          terminalMode
            ? "Full-screen, keyboard-first facility control. Alt + 1–6 changes workspace; Escape exits terminal mode."
            : "One operational workspace for expected inbound, custody, counts, sorting, outbound movement, and exceptions."
        }
        actions={
          <>
            <Input
              className="w-56"
              value={facilityId}
              onChange={(event) => updateFacility(event.target.value)}
              placeholder="Current facility ID"
              aria-label="Hub facility ID"
            />
            {!terminalMode ? (
              <Button size="sm" onClick={() => void summary.refetch()}>
                Refresh
              </Button>
            ) : null}
            {canUseTerminal ? (
              <Button
                size="sm"
                variant={terminalMode ? "primary" : "secondary"}
                onClick={() => updateTerminalMode(!terminalMode)}
              >
                {terminalMode ? (
                  <Minimize2 aria-hidden className="h-4 w-4" />
                ) : (
                  <Maximize2 aria-hidden className="h-4 w-4" />
                )}
                {terminalMode ? "Exit terminal" : "Terminal mode"}
              </Button>
            ) : null}
          </>
        }
      />
      {terminalMode ? (
        <div className="mb-4 flex flex-wrap items-center gap-2 rounded-md border border-slate-700 bg-slate-950 p-2 text-white">
          <span className="flex items-center gap-2 px-2 text-xs font-semibold uppercase tracking-wide text-slate-300">
            <Keyboard aria-hidden className="h-4 w-4" /> Shortcuts
          </span>
          {terminalNavigation.map((item, index) => (
            <Button
              key={item.label}
              size="sm"
              variant={item.id === terminalView ? "primary" : "secondary"}
              onClick={() => setTerminalView(item.id)}
              className="min-w-24"
            >
              <kbd className="rounded border bg-white/10 px-1 font-mono text-[10px]">
                Alt {index + 1}
              </kbd>
              {item.label}
            </Button>
          ))}
        </div>
      ) : null}
      {terminalMode ? (
        <FacilityTerminal
          facilityId={facilityId}
          view={terminalView}
          navigate={(to) => void navigate(to)}
        />
      ) : summary.isLoading ? (
        <Panel>
          <LoadingState label="Loading facility workload" />
        </Panel>
      ) : summary.error ? (
        <Panel>
          <ErrorState
            error={summary.error}
            retry={() => void summary.refetch()}
          />
        </Panel>
      ) : (
        <OperationalMetricStrip
          label="Hub workload"
          items={[
            {
              label: "Expected inbound",
              value:
                (summary.data?.inboundManifests ?? 0) +
                (summary.data?.inboundTrips ?? 0),
              tone: "info",
              icon: ArrowDownToLine,
            },
            {
              label: "In custody",
              value: summary.data?.shipmentsInCustody,
              icon: Boxes,
            },
            { label: "Open bags", value: summary.data?.openBags, icon: Boxes },
            {
              label: "Inbound bags",
              value: summary.data?.inboundBags,
              tone: "info",
              icon: PackageCheck,
            },
            {
              label: "Ready for delivery",
              value: summary.data?.readyForDelivery,
              tone: "success",
              icon: Send,
            },
            {
              label: "OFD",
              value: summary.data?.outForDelivery,
              tone: "info",
              icon: Truck,
            },
            {
              label: "NDR pending",
              value: summary.data?.ndrPending,
              tone: "warning",
              icon: AlertTriangle,
            },
            {
              label: "Exceptions",
              value: summary.data?.openExceptions,
              tone: summary.data?.openExceptions ? "danger" : "success",
              icon: CircleAlert,
            },
            {
              label: "On hold",
              value: summary.data?.onHold,
              tone: "warning",
              icon: PackageX,
            },
            {
              label: "Pending pickups",
              value: summary.data?.pendingPickups,
              icon: ClipboardCheck,
            },
          ]}
        />
      )}
      {!terminalMode ? (
        <Panel>
          <div
            className="flex gap-1 overflow-x-auto border-b p-2"
            role="tablist"
            aria-label="Hub workspaces"
          >
            {workspaces.map((item) => (
              <Button
                key={item.id}
                size="sm"
                variant={workspace === item.id ? "primary" : "ghost"}
                onClick={() => setWorkspace(item.id)}
                role="tab"
                aria-selected={workspace === item.id}
              >
                {item.label}
              </Button>
            ))}
          </div>
          {workspace === "inbound" ? (
            <InboundWorkspace facilityId={facilityId} />
          ) : workspace === "reconciliation" ? (
            <ReconciliationWorkspace />
          ) : workspace === "sorting" ? (
            <SortingWorkspace facilityId={facilityId} />
          ) : workspace === "outbound" ? (
            <OutboundWorkspace />
          ) : workspace === "exceptions" ? (
            <ExceptionsWorkspace facilityId={facilityId} />
          ) : (
            <CustodyWorkspace facilityId={facilityId} />
          )}
        </Panel>
      ) : null}
    </div>
  );
}

function consoleHeaders(facilityId: string) {
  return facilityId ? { "X-Operating-Unit": facilityId } : undefined;
}

function FacilityTerminal({
  facilityId,
  view,
  navigate,
}: {
  facilityId: string;
  view: TerminalView;
  navigate: (to: string) => void;
}) {
  const summary = useQuery({
    queryKey: ["console-summary", facilityId],
    queryFn: () =>
      apiRequest<ConsoleFacilitySummary>("/api/v1/console/summary", {
        headers: consoleHeaders(facilityId),
      }),
    enabled: Boolean(facilityId),
    refetchInterval: 30_000,
  });
  if (!facilityId) {
    return (
      <InlineNotice tone="warning" title="Choose the current facility">
        Enter the operating-unit ID above. The backend checks it against this
        sign-in before returning custody or workload data.
      </InlineNotice>
    );
  }
  if (summary.isLoading)
    return (
      <Panel>
        <LoadingState label="Loading terminal facility" />
      </Panel>
    );
  if (summary.error)
    return (
      <Panel>
        <ErrorState
          error={summary.error}
          retry={() => void summary.refetch()}
        />
      </Panel>
    );
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-slate-300 bg-white px-4 py-3">
        <div>
          <strong className="block text-sm">
            {summary.data?.unit?.name ?? "Current facility"}
          </strong>
          <span className="font-mono text-xs text-slate-500">
            {summary.data?.unit?.code ?? facilityId}
          </span>
        </div>
        <Button size="sm" onClick={() => void summary.refetch()}>
          Refresh terminal
        </Button>
      </div>
      <OperationalMetricStrip
        label="Live facility state"
        items={[
          {
            label: "Inbound expected",
            value: summary.data?.inboundExpected,
            tone: "info",
            icon: ArrowDownToLine,
          },
          { label: "In custody", value: summary.data?.inCustody, icon: Boxes },
          {
            label: "OFD",
            value: summary.data?.outForDelivery,
            tone: "info",
            icon: Truck,
          },
          { label: "Open bags", value: summary.data?.openBags, icon: Boxes },
          {
            label: "Held",
            value: summary.data?.held,
            tone: summary.data?.held ? "warning" : "success",
            icon: PackageX,
          },
          {
            label: "Overdue",
            value: summary.data?.overdue,
            tone: summary.data?.overdue ? "danger" : "success",
            icon: AlertTriangle,
          },
          {
            label: "Exceptions",
            value: summary.data?.exceptions,
            tone: summary.data?.exceptions ? "danger" : "success",
            icon: CircleAlert,
          },
        ]}
      />
      {view === "inbound" ? (
        <TerminalInbound facilityId={facilityId} />
      ) : view === "sort" ? (
        <TerminalLookup facilityId={facilityId} />
      ) : view === "bag" ? (
        <TerminalBags
          facilityId={facilityId}
          openBagWorkspace={() => navigate("/operations/bags")}
        />
      ) : view === "manifest" ? (
        <TerminalInbound
          facilityId={facilityId}
          manifestMode
          openManifestWorkspace={() => navigate("/operations/manifests")}
        />
      ) : view === "outbound" ? (
        <TerminalQueue facilityId={facilityId} mode="outbound" />
      ) : (
        <TerminalQueue facilityId={facilityId} mode="exceptions" />
      )}
    </div>
  );
}

function TerminalInbound({
  facilityId,
  manifestMode = false,
  openManifestWorkspace,
}: {
  facilityId: string;
  manifestMode?: boolean;
  openManifestWorkspace?: () => void;
}) {
  const query = useQuery({
    queryKey: ["console-inbound", facilityId],
    queryFn: () =>
      apiRequest<ConsoleInboundResponse>("/api/v1/console/inbound?limit=100", {
        headers: consoleHeaders(facilityId),
      }),
    refetchInterval: 30_000,
  });
  return (
    <Panel className="shadow-none">
      <PanelHeader
        title={manifestMode ? "Manifest arrivals" : "Inbound manifests"}
        description="Lightweight terminal projection for consignments on the way"
        actions={
          manifestMode && openManifestWorkspace ? (
            <Button size="sm" onClick={openManifestWorkspace}>
              Open manifest workspace
            </Button>
          ) : undefined
        }
      />
      {query.isLoading ? (
        <LoadingState label="Loading inbound consignments" />
      ) : query.error ? (
        <ErrorState error={query.error} retry={() => void query.refetch()} />
      ) : query.data?.data?.length ? (
        <DataTable label="Terminal inbound manifests">
          <thead>
            <tr>
              <TableHead>Manifest</TableHead>
              <TableHead>Origin</TableHead>
              <TableHead>Status</TableHead>
              <TableHead className="text-right">Bags</TableHead>
              <TableHead className="text-right">Shipments</TableHead>
              <TableHead className="text-right">Weight</TableHead>
              <TableHead>Dispatched</TableHead>
            </tr>
          </thead>
          <tbody>
            {query.data.data.map((item) => (
              <tr key={item.id}>
                <TableCell>
                  <strong className="font-mono">{item.manifestCode}</strong>
                </TableCell>
                <TableCell>{item.origin}</TableCell>
                <TableCell>
                  <StatusBadge status={item.status} />
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {item.bags ?? 0}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {item.shipments ?? 0}
                </TableCell>
                <TableCell className="text-right">
                  {formatWeight(item.weightGrams)}
                </TableCell>
                <TableCell>{formatDateTime(item.dispatchedAt)}</TableCell>
              </tr>
            ))}
          </tbody>
        </DataTable>
      ) : (
        <EmptyState
          title="No inbound manifests"
          description="Nothing is currently dispatched to this facility."
        />
      )}
    </Panel>
  );
}

function TerminalLookup({ facilityId }: { facilityId: string }) {
  const scannerRef = useRef<ScannerInputHandle>(null);
  const [barcode, setBarcode] = useState("");
  const [recent, setRecent] = useState<
    Array<{
      barcode: string;
      parcel?: ConsoleParcel;
      error?: string;
      at: string;
    }>
  >([]);
  const lookup = useMutation({
    mutationFn: (value: string) =>
      apiRequest<ConsoleParcel>(
        `/api/v1/console/lookup/${encodeURIComponent(value)}`,
        { headers: consoleHeaders(facilityId) },
      ),
    onSuccess: (parcel, value) => {
      setRecent((items) => [
        { barcode: value, parcel, at: new Date().toISOString() },
        ...items.slice(0, 19),
      ]);
      setBarcode("");
    },
    onError: (error, value) => {
      setRecent((items) => [
        {
          barcode: value,
          error: error instanceof Error ? error.message : "Lookup rejected",
          at: new Date().toISOString(),
        },
        ...items.slice(0, 19),
      ]);
      setBarcode("");
    },
    onSettled: () => requestAnimationFrame(() => scannerRef.current?.focus()),
  });
  const current = recent[0];
  return (
    <div className="grid gap-4 xl:grid-cols-[minmax(0,0.8fr)_minmax(0,1.2fr)]">
      <Panel className="shadow-none">
        <PanelHeader
          title="Scanner-assisted sorting"
          description="Scan → submit → feedback → refocus. No mouse required."
        />
        <div className="space-y-4 p-4">
          <ScannerInput
            ref={scannerRef}
            value={barcode}
            onChange={setBarcode}
            onScan={(value) => lookup.mutate(value)}
            busy={lookup.isPending}
            label="Scan AWB or piece barcode"
          />
          {!current ? (
            <div className="rounded-md border border-dashed p-5 text-center text-sm text-slate-500">
              Awaiting the next barcode.
            </div>
          ) : current.parcel ? (
            <div
              className="rounded-md border border-emerald-300 bg-emerald-50 p-4"
              role="status"
              aria-live="assertive"
            >
              <div className="flex items-start justify-between gap-3">
                <div>
                  <strong className="block font-mono text-xl">
                    {current.parcel.awb}
                  </strong>
                  <span className="mt-1 block text-sm">
                    {current.parcel.destinationCity ||
                      current.parcel.destinationPincode}{" "}
                    → {current.parcel.destinationBranch || "Sort lane"}
                  </span>
                </div>
                <StatusBadge status={current.parcel.status} />
              </div>
              <div className="mt-4 grid gap-2 sm:grid-cols-3">
                <span className="text-sm">
                  <strong>{current.parcel.pieces ?? 1}</strong> pieces
                </span>
                <span className="text-sm">
                  {current.parcel.isHeld
                    ? "HELD — check exception"
                    : "Clear to sort"}
                </span>
                {current.parcel.amountDueMinor != null ? (
                  <span className="text-sm font-semibold">
                    COD{" "}
                    <Money
                      amountMinor={current.parcel.amountDueMinor}
                      currency={current.parcel.currency ?? "NGN"}
                    />
                  </span>
                ) : (
                  <span className="text-sm font-semibold">
                    Prepaid · no cash due
                  </span>
                )}
              </div>
            </div>
          ) : (
            <div
              className="rounded-md border border-red-300 bg-red-50 p-4 text-red-950"
              role="alert"
            >
              <strong className="block">Rejected · {current.barcode}</strong>
              <span className="mt-1 block text-sm">{current.error}</span>
            </div>
          )}
        </div>
      </Panel>
      <Panel className="shadow-none">
        <PanelHeader
          title="Recent scans"
          description={`${recent.filter((item) => item.parcel).length} accepted · ${recent.filter((item) => item.error).length} rejected this session`}
        />
        {recent.length ? (
          <DataTable label="Recent terminal lookups">
            <thead>
              <tr>
                <TableHead>Time</TableHead>
                <TableHead>Barcode</TableHead>
                <TableHead>Result</TableHead>
                <TableHead>Destination</TableHead>
              </tr>
            </thead>
            <tbody>
              {recent.map((item, index) => (
                <tr key={`${item.barcode}-${item.at}-${index}`}>
                  <TableCell>{formatDateTime(item.at)}</TableCell>
                  <TableCell className="font-mono">{item.barcode}</TableCell>
                  <TableCell>
                    <Badge tone={item.parcel ? "success" : "danger"}>
                      {item.parcel ? "Accepted" : "Rejected"}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    {item.parcel?.destinationBranch ?? item.error}
                  </TableCell>
                </tr>
              ))}
            </tbody>
          </DataTable>
        ) : (
          <EmptyState
            title="No scans this session"
            description="Accepted and rejected lookups stay visible here."
          />
        )}
      </Panel>
    </div>
  );
}

function TerminalBags({
  facilityId,
  openBagWorkspace,
}: {
  facilityId: string;
  openBagWorkspace: () => void;
}) {
  const query = useQuery({
    queryKey: ["console-bags", facilityId],
    queryFn: () =>
      apiRequest<ConsoleBagResponse>("/api/v1/console/bags?limit=100", {
        headers: consoleHeaders(facilityId),
      }),
    refetchInterval: 30_000,
  });
  return (
    <Panel className="shadow-none">
      <PanelHeader
        title="Facility bags"
        description="Dense open and recently closed bag register"
        actions={
          <Button size="sm" onClick={openBagWorkspace}>
            Create or scan bag
          </Button>
        }
      />
      {query.isLoading ? (
        <LoadingState label="Loading facility bags" />
      ) : query.error ? (
        <ErrorState error={query.error} retry={() => void query.refetch()} />
      ) : query.data?.data?.length ? (
        <DataTable label="Terminal facility bags">
          <thead>
            <tr>
              <TableHead>Bag</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Destination</TableHead>
              <TableHead className="text-right">Shipments</TableHead>
              <TableHead className="text-right">Weight</TableHead>
            </tr>
          </thead>
          <tbody>
            {query.data.data.map((bag) => (
              <tr key={bag.id}>
                <TableCell>
                  <strong className="font-mono">{bag.bagCode}</strong>
                </TableCell>
                <TableCell>
                  <StatusBadge status={bag.status} />
                </TableCell>
                <TableCell>{bag.destination}</TableCell>
                <TableCell className="text-right tabular-nums">
                  {bag.shipments ?? 0}
                </TableCell>
                <TableCell className="text-right">
                  {formatWeight(bag.weightGrams)}
                </TableCell>
              </tr>
            ))}
          </tbody>
        </DataTable>
      ) : (
        <EmptyState
          title="No facility bags"
          description="Create a bag when shipments are ready to group."
        />
      )}
    </Panel>
  );
}

function TerminalQueue({
  facilityId,
  mode,
}: {
  facilityId: string;
  mode: "outbound" | "exceptions";
}) {
  const query = useQuery({
    queryKey: ["console-queue", facilityId],
    queryFn: () =>
      apiRequest<ConsoleQueueResponse>("/api/v1/console/queue?limit=100", {
        headers: consoleHeaders(facilityId),
      }),
    refetchInterval: 30_000,
  });
  const rows =
    mode === "exceptions"
      ? (query.data?.data ?? []).filter(
          (item) =>
            item.isHeld ||
            (item.promisedBy && new Date(item.promisedBy) < new Date()),
        )
      : (query.data?.data ?? []);
  return (
    <Panel className="shadow-none">
      <PanelHeader
        title={
          mode === "exceptions"
            ? "Held and overdue in loaded queue"
            : "Outbound queue"
        }
        description={
          mode === "exceptions"
            ? "The facility summary is authoritative; this table highlights exceptions in the current server page."
            : "Lightweight custody queue for rapid outbound decisions."
        }
      />
      {query.isLoading ? (
        <LoadingState label="Loading facility queue" />
      ) : query.error ? (
        <ErrorState error={query.error} retry={() => void query.refetch()} />
      ) : rows.length ? (
        <DataTable
          label={
            mode === "exceptions"
              ? "Terminal exception queue"
              : "Terminal outbound queue"
          }
        >
          <thead>
            <tr>
              <TableHead>AWB</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Destination</TableHead>
              <TableHead className="text-right">Pieces</TableHead>
              <TableHead>Promised by</TableHead>
              <TableHead>Handling state</TableHead>
            </tr>
          </thead>
          <tbody>
            {rows.map((item) => (
              <tr key={item.id}>
                <TableCell>
                  <strong className="font-mono">{item.awb}</strong>
                </TableCell>
                <TableCell>
                  <StatusBadge status={item.status} />
                </TableCell>
                <TableCell>
                  {item.destinationBranch || item.destinationPincode}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {item.pieces ?? 1}
                </TableCell>
                <TableCell>{formatDateTime(item.promisedBy)}</TableCell>
                <TableCell>
                  {item.isHeld ? (
                    <Badge tone="warning">Held</Badge>
                  ) : item.promisedBy &&
                    new Date(item.promisedBy) < new Date() ? (
                    <Badge tone="danger">Overdue</Badge>
                  ) : (
                    <Badge tone="success">Clear</Badge>
                  )}
                </TableCell>
              </tr>
            ))}
          </tbody>
        </DataTable>
      ) : (
        <EmptyState
          title={
            mode === "exceptions"
              ? "No held or overdue parcels"
              : "No outbound queue"
          }
          description="The current server page has no matching work."
        />
      )}
    </Panel>
  );
}

function InboundWorkspace({ facilityId }: { facilityId: string }) {
  const query = useQuery({
    queryKey: ["hub-inbound", facilityId],
    queryFn: () =>
      apiRequest<HubInbound>(
        `/api/v1/hub/inbound${queryString({ operatingUnitId: facilityId, limit: 50 })}`,
      ),
    refetchInterval: 30_000,
  });
  if (query.isLoading) return <LoadingState label="Loading expected inbound" />;
  if (query.error)
    return (
      <ErrorState error={query.error} retry={() => void query.refetch()} />
    );
  const manifests = query.data?.manifests ?? [];
  const trips = query.data?.trips ?? [];
  return (
    <div className="grid gap-4 p-4 xl:grid-cols-2">
      <section className="rounded-md border">
        <PanelHeader
          title="Inbound manifests"
          description="Oldest dispatched first."
        />
        {manifests.length ? (
          <DataTable label="Inbound manifests">
            <thead>
              <tr>
                <TableHead>Manifest</TableHead>
                <TableHead>Origin</TableHead>
                <TableHead>Load</TableHead>
                <TableHead>ETA</TableHead>
              </tr>
            </thead>
            <tbody>
              {manifests.map((item) => (
                <tr key={item.id}>
                  <TableCell>
                    <EntityLink
                      to={`/operations/manifests/${item.id}`}
                      primary={item.manifestCode}
                      secondary={item.vehicleRegistration}
                    />
                  </TableCell>
                  <TableCell>{item.origin?.code}</TableCell>
                  <TableCell>
                    {item.bagCount} bags · {item.totalPieceCount} pcs
                  </TableCell>
                  <TableCell>{formatDateTime(item.expectedArrival)}</TableCell>
                </tr>
              ))}
            </tbody>
          </DataTable>
        ) : (
          <EmptyState
            title="No inbound manifests"
            description="Nothing is currently dispatched to this facility."
          />
        )}
      </section>
      <section className="rounded-md border">
        <PanelHeader title="Trips on the road" />
        {trips.length ? (
          <DataTable label="Inbound trips">
            <thead>
              <tr>
                <TableHead>Trip</TableHead>
                <TableHead>Origin</TableHead>
                <TableHead>Vehicle</TableHead>
                <TableHead>ETA</TableHead>
              </tr>
            </thead>
            <tbody>
              {trips.map((item) => (
                <tr key={item.id}>
                  <TableCell>
                    <EntityLink
                      to={`/operations/trips/${item.id}`}
                      primary={item.tripCode}
                      secondary={`${item.manifestCount} manifests`}
                    />
                  </TableCell>
                  <TableCell>{item.origin?.code}</TableCell>
                  <TableCell>
                    {item.vehicleRegistration}
                    <span className="block text-xs text-slate-500">
                      {item.driverName}
                    </span>
                  </TableCell>
                  <TableCell>{formatDateTime(item.expectedArrival)}</TableCell>
                </tr>
              ))}
            </tbody>
          </DataTable>
        ) : (
          <EmptyState
            title="No inbound trips"
            description="No line-haul trip is currently heading here."
          />
        )}
      </section>
    </div>
  );
}

function ReconciliationWorkspace() {
  const navigate = useNavigate();
  const [status, setStatus] = useState("");
  const [cursorStack, setCursorStack] = useState<(string | undefined)[]>([
    undefined,
  ]);
  const cursor = cursorStack.at(-1);
  const query = useQuery({
    queryKey: ["reconciliations", { cursor, status }],
    queryFn: () =>
      apiRequest<ReconciliationPage>(
        `/api/v1/reconciliations${queryString({ cursor, limit: 25, status })}`,
      ),
  });
  const rows = query.data?.data ?? [];
  return (
    <div>
      <div className="border-b p-4">
        <Select
          className="max-w-xs"
          value={status}
          onChange={(event) => {
            setStatus(event.target.value);
            setCursorStack([undefined]);
          }}
          aria-label="Reconciliation status"
        >
          <option value="">All count states</option>
          <option>IN_PROGRESS</option>
          <option>COMPLETED</option>
          <option>COMPLETED_WITH_EXCEPTIONS</option>
          <option>ABANDONED</option>
        </Select>
      </div>
      {query.isLoading ? (
        <LoadingState label="Loading reconciliations" />
      ) : query.error ? (
        <ErrorState error={query.error} retry={() => void query.refetch()} />
      ) : rows.length ? (
        <>
          <DataTable label="Reconciliations">
            <thead>
              <tr>
                <TableHead>Count</TableHead>
                <TableHead>Subject</TableHead>
                <TableHead>Progress</TableHead>
                <TableHead>Matched</TableHead>
                <TableHead>Missing</TableHead>
                <TableHead>Excess</TableHead>
                <TableHead>Status</TableHead>
              </tr>
            </thead>
            <tbody>
              {rows.map((item) => (
                <tr
                  key={item.id}
                  className="cursor-pointer hover:bg-slate-50"
                  onClick={() =>
                    item.id &&
                    void navigate(`/operations/hub/reconciliations/${item.id}`)
                  }
                >
                  <TableCell>
                    <EntityLink
                      to={`/operations/hub/reconciliations/${item.id}`}
                      primary={item.reconciliationCode}
                      secondary={item.facilityCode}
                    />
                  </TableCell>
                  <TableCell>
                    {titleCase(item.subjectType ?? "")}
                    <span className="block font-mono text-xs text-slate-500">
                      {item.bagCode ?? item.manifestCode}
                    </span>
                  </TableCell>
                  <TableCell>
                    {item.scannedCount ?? 0} / {item.expectedCount ?? 0}
                  </TableCell>
                  <TableCell className="text-emerald-700">
                    {item.matchedCount ?? 0}
                  </TableCell>
                  <TableCell className="text-red-700">
                    {item.missingCount ?? 0}
                  </TableCell>
                  <TableCell className="text-amber-700">
                    {item.excessCount ?? 0}
                  </TableCell>
                  <TableCell>
                    <StatusBadge status={item.status} />
                  </TableCell>
                </tr>
              ))}
            </tbody>
          </DataTable>
          <CursorPager
            page={cursorStack.length}
            count={rows.length}
            noun="counts"
            hasMore={query.data?.pagination?.hasMore}
            nextCursor={query.data?.pagination?.nextCursor}
            onPrevious={() => setCursorStack((items) => items.slice(0, -1))}
            onNext={(next) => setCursorStack((items) => [...items, next])}
          />
        </>
      ) : (
        <EmptyState
          title="No reconciliations"
          description="Receive and open a bag or manifest to start a count."
        />
      )}
    </div>
  );
}

function SortingWorkspace({ facilityId }: { facilityId: string }) {
  const navigate = useNavigate();
  return (
    <div className="grid gap-4 p-4 md:grid-cols-2">
      <div className="rounded-md border bg-slate-50 p-5">
        <ScanLine aria-hidden className="h-7 w-7 text-primary" />
        <h2 className="mt-3 text-base font-bold">Scanner-assisted sorting</h2>
        <p className="mt-1 text-sm text-slate-600">
          Use Sort mode in the scanner console. The server records the routing
          decision without changing custody.
        </p>
        <Button
          className="mt-4"
          variant="primary"
          onClick={() => void navigate("/operations/scanner")}
        >
          Open scanner console
        </Button>
      </div>
      <div className="rounded-md border p-5">
        <Boxes aria-hidden className="h-7 w-7 text-slate-600" />
        <h2 className="mt-3 text-base font-bold">Facility context</h2>
        <p className="mt-1 text-sm text-slate-600">
          Current facility:{" "}
          <span className="font-mono font-semibold">
            {facilityId || "Resolved from your role"}
          </span>
        </p>
        <p className="mt-3 text-xs text-slate-500">
          The sorter never chooses the shipment state. Routing and custody
          decisions remain server-authoritative.
        </p>
      </div>
    </div>
  );
}

function OutboundWorkspace() {
  const navigate = useNavigate();
  return (
    <div className="grid gap-4 p-4 md:grid-cols-3">
      <button
        onClick={() => void navigate("/operations/bags?status=CLOSED")}
        className="rounded-md border p-5 text-left hover:border-primary"
      >
        <Boxes aria-hidden className="h-6 w-6 text-primary" />
        <h2 className="mt-3 font-bold">Closed bags</h2>
        <p className="mt-1 text-sm text-slate-500">
          Review containers waiting for manifesting.
        </p>
      </button>
      <button
        onClick={() => void navigate("/operations/manifests?status=CLOSED")}
        className="rounded-md border p-5 text-left hover:border-primary"
      >
        <PackageCheck aria-hidden className="h-6 w-6 text-primary" />
        <h2 className="mt-3 font-bold">Closed manifests</h2>
        <p className="mt-1 text-sm text-slate-500">
          Dispatch handovers when their trip is ready.
        </p>
      </button>
      <button
        onClick={() => void navigate("/operations/trips")}
        className="rounded-md border p-5 text-left hover:border-primary"
      >
        <Truck aria-hidden className="h-6 w-6 text-primary" />
        <h2 className="mt-3 font-bold">Trip board</h2>
        <p className="mt-1 text-sm text-slate-500">
          Load, depart, arrive, and close line haul.
        </p>
      </button>
    </div>
  );
}

function CustodyWorkspace({ facilityId }: { facilityId: string }) {
  const [status, setStatus] = useState("");
  const [cursorStack, setCursorStack] = useState<(string | undefined)[]>([
    undefined,
  ]);
  const cursor = cursorStack.at(-1);
  const query = useQuery({
    queryKey: ["hub-custody", { facilityId, status, cursor }],
    queryFn: () =>
      apiRequest<CustodyPage>(
        `/api/v1/hub/custody${queryString({ operatingUnitId: facilityId, status, cursor, limit: 25 })}`,
      ),
  });
  const rows = query.data?.data ?? [];
  return (
    <div>
      <div className="border-b p-4">
        <Input
          className="max-w-xs"
          value={status}
          onChange={(event) => {
            setStatus(event.target.value.toUpperCase());
            setCursorStack([undefined]);
          }}
          placeholder="Filter shipment status"
        />
      </div>
      {query.isLoading ? (
        <LoadingState label="Loading custody stocktake" />
      ) : query.error ? (
        <ErrorState error={query.error} retry={() => void query.refetch()} />
      ) : rows.length ? (
        <>
          <DataTable label="Facility custody">
            <thead>
              <tr>
                <TableHead>AWB</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Direction</TableHead>
                <TableHead>Bag</TableHead>
                <TableHead>Destination</TableHead>
                <TableHead>Weight</TableHead>
                <TableHead>Hold</TableHead>
              </tr>
            </thead>
            <tbody>
              {rows.map((item) => (
                <tr key={item.shipmentId}>
                  <TableCell>
                    <EntityLink
                      to={`/shipments/${item.shipmentId}`}
                      primary={item.awb}
                    />
                  </TableCell>
                  <TableCell>
                    <StatusBadge status={item.status} />
                  </TableCell>
                  <TableCell>
                    {item.movementDirection === "REVERSE" ? (
                      <Badge tone="warning">Return</Badge>
                    ) : (
                      <Badge tone="neutral">Forward</Badge>
                    )}
                  </TableCell>
                  <TableCell>{item.bagCode || "Loose"}</TableCell>
                  <TableCell>
                    {item.destinationBranchCode || item.destinationPincode}
                  </TableCell>
                  <TableCell>
                    {formatWeight(item.chargeableWeightGrams)}
                  </TableCell>
                  <TableCell>
                    {item.isHeld ? <Badge tone="danger">Held</Badge> : "—"}
                  </TableCell>
                </tr>
              ))}
            </tbody>
          </DataTable>
          <CursorPager
            page={cursorStack.length}
            count={rows.length}
            noun="shipments"
            hasMore={query.data?.pagination?.hasMore}
            nextCursor={query.data?.pagination?.nextCursor}
            onPrevious={() => setCursorStack((items) => items.slice(0, -1))}
            onNext={(next) => setCursorStack((items) => [...items, next])}
          />
        </>
      ) : (
        <EmptyState
          title="No custody items"
          description="No shipments match this facility and status filter."
        />
      )}
    </div>
  );
}

function ExceptionsWorkspace({ facilityId }: { facilityId: string }) {
  const { hasPermission } = useAuth();
  const [status, setStatus] = useState("OPEN");
  const [severity, setSeverity] = useState("");
  const [selected, setSelected] = useState<ExceptionSummary>();
  const query = useQuery({
    queryKey: ["exceptions", { facilityId, status, severity }],
    queryFn: () =>
      apiRequest<ExceptionPage>(
        `/api/v1/exceptions${queryString({ operatingUnitId: facilityId, status, severity, limit: 50 })}`,
      ),
  });
  const rows = query.data?.data ?? [];
  return (
    <div>
      <div className="grid gap-3 border-b p-4 sm:grid-cols-2">
        <Select
          value={status}
          onChange={(event) => setStatus(event.target.value)}
        >
          <option value="">All states</option>
          <option>OPEN</option>
          <option>INVESTIGATING</option>
          <option>RESOLVED</option>
          <option>WRITTEN_OFF</option>
        </Select>
        <Select
          value={severity}
          onChange={(event) => setSeverity(event.target.value)}
        >
          <option value="">All severities</option>
          <option>LOW</option>
          <option>MEDIUM</option>
          <option>HIGH</option>
          <option>CRITICAL</option>
        </Select>
      </div>
      {query.isLoading ? (
        <LoadingState label="Loading exceptions" />
      ) : query.error ? (
        <ErrorState error={query.error} retry={() => void query.refetch()} />
      ) : rows.length ? (
        <DataTable label="Operational exceptions">
          <thead>
            <tr>
              <TableHead>Exception</TableHead>
              <TableHead>Type</TableHead>
              <TableHead>Subject</TableHead>
              <TableHead>Description</TableHead>
              <TableHead>Age</TableHead>
              <TableHead>Severity</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>
                <span className="sr-only">Actions</span>
              </TableHead>
            </tr>
          </thead>
          <tbody>
            {rows.map((item) => (
              <tr key={item.id}>
                <TableCell>
                  <span className="font-mono font-semibold text-primary">
                    {item.exceptionCode}
                  </span>
                </TableCell>
                <TableCell>{titleCase(item.exceptionType ?? "")}</TableCell>
                <TableCell>
                  {item.awb ?? item.bagCode ?? item.rawBarcode ?? "—"}
                </TableCell>
                <TableCell>
                  <span className="block max-w-xs truncate">
                    {item.description}
                  </span>
                </TableCell>
                <TableCell>{formatDateTime(item.raisedAt)}</TableCell>
                <TableCell>
                  <Badge
                    tone={
                      item.severity === "CRITICAL" || item.severity === "HIGH"
                        ? "danger"
                        : item.severity === "MEDIUM"
                          ? "warning"
                          : "neutral"
                    }
                  >
                    {titleCase(item.severity ?? "")}
                  </Badge>
                </TableCell>
                <TableCell>
                  <StatusBadge status={item.status} />
                </TableCell>
                <TableCell>
                  {hasPermission("exception.resolve") ? (
                    <Button size="sm" onClick={() => setSelected(item)}>
                      Resolve
                    </Button>
                  ) : null}
                </TableCell>
              </tr>
            ))}
          </tbody>
        </DataTable>
      ) : (
        <EmptyState
          title="No exceptions"
          description="No operational exception matches these filters."
        />
      )}
      <ResolveExceptionDialog
        item={selected}
        open={Boolean(selected)}
        onOpenChange={(open) => !open && setSelected(undefined)}
      />
    </div>
  );
}

const resolveSchema = z
  .object({
    status: z.enum(["INVESTIGATING", "RESOLVED", "WRITTEN_OFF", "CANCELLED"]),
    resolutionAction: z
      .enum([
        "FOUND",
        "RETURNED_TO_ORIGIN",
        "FORWARDED",
        "REBAGGED",
        "DELIVERED",
        "WRITTEN_OFF",
        "CLAIM_FILED",
        "CORRECTED",
        "NO_ACTION",
      ])
      .optional(),
    resolutionNotes: z.string().optional(),
    assignToUserId: z.string().optional(),
  })
  .superRefine((value, context) => {
    if (["RESOLVED", "WRITTEN_OFF", "CANCELLED"].includes(value.status)) {
      if (!value.resolutionAction)
        context.addIssue({
          code: "custom",
          path: ["resolutionAction"],
          message: "Action is required when closing",
        });
      if (!value.resolutionNotes?.trim())
        context.addIssue({
          code: "custom",
          path: ["resolutionNotes"],
          message: "Resolution note is required",
        });
    }
  });
function ResolveExceptionDialog({
  item,
  open,
  onOpenChange,
}: {
  item?: ExceptionSummary;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const client = useQueryClient();
  const { toast } = useToast();
  const form = useForm<z.infer<typeof resolveSchema>>({
    resolver: zodResolver(resolveSchema),
    defaultValues: { status: "INVESTIGATING" },
  });
  const mutation = useMutation({
    mutationFn: (body: z.infer<typeof resolveSchema>) =>
      apiRequest<OperationalException>(
        `/api/v1/exceptions/${item?.id}/resolve`,
        { method: "POST", body },
      ),
    onSuccess: (result) => {
      void client.invalidateQueries({ queryKey: ["exceptions"] });
      toast({
        tone: "success",
        title: `Exception ${titleCase(result.status ?? "updated")}`,
        description: result.exceptionCode,
      });
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={`Resolve ${item?.exceptionCode ?? "exception"}`}
      description="Closing requires both a resolution action and a written note."
      footer={
        <>
          <Button onClick={() => onOpenChange(false)}>Cancel</Button>
          <Button
            variant="primary"
            onClick={() =>
              void form.handleSubmit((values) => mutation.mutate(values))()
            }
          >
            Save resolution
          </Button>
        </>
      }
    >
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Status" htmlFor="exception-status">
          <Select id="exception-status" {...form.register("status")}>
            <option>INVESTIGATING</option>
            <option>RESOLVED</option>
            <option>WRITTEN_OFF</option>
            <option>CANCELLED</option>
          </Select>
        </Field>
        <Field
          label="Resolution action"
          htmlFor="exception-action"
          error={form.formState.errors.resolutionAction?.message}
        >
          <Select id="exception-action" {...form.register("resolutionAction")}>
            <option value="">Select action</option>
            <option>FOUND</option>
            <option>RETURNED_TO_ORIGIN</option>
            <option>FORWARDED</option>
            <option>REBAGGED</option>
            <option>DELIVERED</option>
            <option>WRITTEN_OFF</option>
            <option>CLAIM_FILED</option>
            <option>CORRECTED</option>
            <option>NO_ACTION</option>
          </Select>
        </Field>
        <Field label="Assign to user" htmlFor="exception-assignee">
          <Input id="exception-assignee" {...form.register("assignToUserId")} />
        </Field>
        <Field
          className="sm:col-span-2"
          label="Resolution notes"
          htmlFor="exception-notes"
          error={form.formState.errors.resolutionNotes?.message}
        >
          <Textarea
            id="exception-notes"
            {...form.register("resolutionNotes")}
          />
        </Field>
      </div>
      {mutation.error ? <ErrorState error={mutation.error} /> : null}
    </Dialog>
  );
}

export function ReconciliationDetailPage() {
  const { reconciliationId = "" } = useParams();
  const client = useQueryClient();
  const { toast } = useToast();
  const scannerRef = useRef<ScannerInputHandle>(null);
  const [barcode, setBarcode] = useState("");
  const [damaged, setDamaged] = useState(false);
  const [facilityId, setFacilityId] = useState("");
  const [completeOpen, setCompleteOpen] = useState(false);
  const query = useQuery({
    queryKey: ["reconciliation", reconciliationId],
    queryFn: () =>
      apiRequest<Reconciliation>(`/api/v1/reconciliations/${reconciliationId}`),
    enabled: Boolean(reconciliationId),
  });
  const scan = useMutation({
    mutationFn: (value: string) =>
      apiRequest<ReconciliationScanResult>(
        `/api/v1/reconciliations/${reconciliationId}/scan${queryString({ operatingUnitId: facilityId })}`,
        {
          method: "POST",
          body: damaged ? { damagedBarcodes: [value] } : { barcodes: [value] },
          headers: operationalHeaders("reconciliation-scan"),
        },
      ),
    onSuccess: (result) => {
      setBarcode("");
      void client.invalidateQueries({
        queryKey: ["reconciliation", reconciliationId],
      });
      const row = result.results?.[0];
      toast({
        tone:
          row?.outcome === "MATCHED"
            ? "success"
            : row?.outcome === "EXCESS" || row?.outcome === "DAMAGED"
              ? "error"
              : "info",
        title: titleCase(row?.outcome ?? "Scanned"),
        description: row?.message || row?.barcode,
      });
    },
    onSettled: () => window.setTimeout(() => scannerRef.current?.focus(), 0),
  });
  if (query.isLoading) return <LoadingState label="Loading reconciliation" />;
  if (query.error)
    return (
      <ErrorState error={query.error} retry={() => void query.refetch()} />
    );
  const item = query.data;
  if (!item)
    return (
      <EmptyState
        title="Reconciliation not found"
        description="Check the count identifier."
      />
    );
  const complete = item.status !== "IN_PROGRESS";
  return (
    <>
      <PageHeader
        eyebrow="Hub · Reconciliation"
        title={item.reconciliationCode ?? "Reconciliation"}
        description={`${titleCase(item.subjectType ?? "")} ${item.bagCode ?? item.manifestCode ?? ""} · ${item.facility?.name ?? item.facility?.code}`}
        actions={<StatusBadge status={item.status} />}
      />
      <OperationalMetricStrip
        items={[
          {
            label: "Expected",
            value: item.expectedCount,
            icon: ClipboardCheck,
          },
          { label: "Scanned", value: item.scannedCount, icon: ScanLine },
          {
            label: "Matched",
            value: item.matchedCount,
            tone: "success",
            icon: CheckCircle2,
          },
          {
            label: "Missing",
            value: item.missingCount,
            tone: item.missingCount ? "danger" : "neutral",
            icon: PackageX,
          },
          {
            label: "Excess",
            value: item.excessCount,
            tone: item.excessCount ? "warning" : "neutral",
            icon: CircleAlert,
          },
          {
            label: "Damaged",
            value: item.damagedCount,
            tone: item.damagedCount ? "danger" : "neutral",
            icon: AlertTriangle,
          },
        ]}
      />
      {complete ? (
        <InlineNotice
          tone={item.status === "COMPLETED" ? "success" : "warning"}
          title="Count is closed"
        >
          {item.status === "COMPLETED"
            ? "Everything matched the declaration."
            : "Exceptions were raised for missing, excess, or damaged items."}
        </InlineNotice>
      ) : null}
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.4fr)_minmax(320px,0.6fr)]">
        <Panel>
          <PanelHeader
            title="Count items"
            description="Expected items are materialised from the immutable closure declaration."
          />
          {item.items?.length ? (
            <DataTable label="Reconciliation items">
              <thead>
                <tr>
                  <TableHead>Barcode</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead>Declaration</TableHead>
                  <TableHead>Outcome</TableHead>
                  <TableHead>Scanned</TableHead>
                  <TableHead>Exception</TableHead>
                </tr>
              </thead>
              <tbody>
                {item.items.map((row, index) => (
                  <tr key={`${row.barcode}-${index}`}>
                    <TableCell className="font-mono font-semibold">
                      {row.awb ?? row.bagCode ?? row.barcode}
                    </TableCell>
                    <TableCell>{titleCase(row.itemType ?? "")}</TableCell>
                    <TableCell>
                      {row.wasDeclared ? "Declared" : "Undeclared"}
                    </TableCell>
                    <TableCell>
                      <StatusBadge status={row.outcome} />
                    </TableCell>
                    <TableCell>{formatDateTime(row.scannedAt)}</TableCell>
                    <TableCell>
                      {row.exceptionId ? (
                        <span className="font-mono text-xs text-danger">
                          {row.exceptionId}
                        </span>
                      ) : (
                        "—"
                      )}
                    </TableCell>
                  </tr>
                ))}
              </tbody>
            </DataTable>
          ) : (
            <EmptyState
              title="No count items"
              description="The declaration did not contain any items."
            />
          )}
        </Panel>
        <div className="space-y-4">
          <Panel>
            <PanelHeader
              title="High-speed count"
              description="Focus returns after every response."
            />
            <div className="space-y-4 p-4">
              <Field label="Current facility" htmlFor="reconcile-facility">
                <Input
                  id="reconcile-facility"
                  value={facilityId}
                  onChange={(event) => setFacilityId(event.target.value)}
                  placeholder="ou_…"
                />
              </Field>
              <div className="flex items-center justify-between rounded-md border p-3">
                <span>
                  <strong className="block text-sm">
                    Mark next barcode damaged
                  </strong>
                  <span className="text-xs text-slate-500">
                    Raises a damage exception during the count.
                  </span>
                </span>
                <Switch
                  label="Mark next scan damaged"
                  checked={damaged}
                  onCheckedChange={setDamaged}
                />
              </div>
              {!complete ? (
                <ScannerInput
                  ref={scannerRef}
                  value={barcode}
                  onChange={setBarcode}
                  onScan={(value) => scan.mutate(value)}
                  busy={scan.isPending}
                  label="Scan counted item"
                />
              ) : null}
              {scan.error ? <ErrorState error={scan.error} /> : null}
            </div>
          </Panel>
          {!complete ? (
            <Button
              className="w-full"
              variant="primary"
              disabled={!facilityId}
              onClick={() => setCompleteOpen(true)}
            >
              Complete reconciliation
            </Button>
          ) : null}
        </div>
      </div>
      <CompleteReconciliationDialog
        item={item}
        facilityId={facilityId}
        open={completeOpen}
        onOpenChange={setCompleteOpen}
      />
    </>
  );
}

function CompleteReconciliationDialog({
  item,
  facilityId,
  open,
  onOpenChange,
}: {
  item: Reconciliation;
  facilityId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const client = useQueryClient();
  const { toast } = useToast();
  const [remarks, setRemarks] = useState("");
  const mutation = useMutation({
    mutationFn: () =>
      apiRequest<Reconciliation>(
        `/api/v1/reconciliations/${item.id}/complete${queryString({ operatingUnitId: facilityId })}`,
        { method: "POST", body: { remarks } },
      ),
    onSuccess: (result) => {
      void client.invalidateQueries({ queryKey: ["reconciliation", item.id] });
      toast({
        tone: result.status === "COMPLETED" ? "success" : "info",
        title: titleCase(result.status ?? "Count completed"),
        description:
          result.status === "COMPLETED_WITH_EXCEPTIONS"
            ? "Missing items were converted into owned exceptions."
            : undefined,
      });
      onOpenChange(false);
    },
  });
  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Complete reconciliation"
      description={`${Math.max((item.expectedCount ?? 0) - (item.scannedCount ?? 0), 0)} expected items remain unscanned. Completing converts them to MISSING and opens exceptions.`}
      footer={
        <>
          <Button onClick={() => onOpenChange(false)}>Continue counting</Button>
          <Button
            variant="primary"
            disabled={mutation.isPending}
            onClick={() => mutation.mutate()}
          >
            Complete count
          </Button>
        </>
      }
    >
      <Field label="Completion remarks" htmlFor="count-remarks">
        <Textarea
          id="count-remarks"
          value={remarks}
          onChange={(event) => setRemarks(event.target.value)}
        />
      </Field>
      {mutation.error ? <ErrorState error={mutation.error} /> : null}
    </Dialog>
  );
}
