import type { components, operations } from "./schema";

export type ApiErrorEnvelope = components["schemas"]["ErrorEnvelope"];
export type ApiErrorBody = ApiErrorEnvelope["error"];
export type TokenPair = components["schemas"]["TokenPair"];
export type UserProfile = components["schemas"]["UserProfile"];
export type User = components["schemas"]["User"];
export type Role = components["schemas"]["Role"];
export type Permission = components["schemas"]["Permission"];
export type OperatingUnit = components["schemas"]["OperatingUnit"];
export type OperatingUnitSummary =
  components["schemas"]["OperatingUnitSummary"];
export type FranchiseSummary = components["schemas"]["FranchiseSummary"];
export type PincodeRecord = components["schemas"]["PincodeRecord"];
export type Zone = components["schemas"]["Zone"];
export type CourierService = components["schemas"]["CourierService"];
export type ServiceabilityResult =
  components["schemas"]["ServiceabilityResult"];
export type Quote = components["schemas"]["Quote"];
export type QuoteRequest = components["schemas"]["QuoteRequest"];
export type Customer = components["schemas"]["Customer"];
export type CustomerSummary = components["schemas"]["CustomerSummary"];
export type CustomerAddress = components["schemas"]["CustomerAddress"];
export type CreditProfile = components["schemas"]["CreditProfile"];
export type Shipment = components["schemas"]["Shipment"];
export type ShipmentListItem = components["schemas"]["ShipmentListItem"];
export type ShipmentEvent = components["schemas"]["ShipmentEvent"];
export type Label = components["schemas"]["Label"];
export type BookingRequest = components["schemas"]["BookingRequest"];
export type RateCardVersion = components["schemas"]["RateCardVersion"];
export type ImportJob = components["schemas"]["ImportJob"];
export type OperationalRef = components["schemas"]["OperationalRef"];
export type PickupRequest = components["schemas"]["PickupRequest"];
export type PickupSummary = components["schemas"]["PickupSummary"];
export type PickupPage = CursorPageOf<PickupSummary>;
export type AgentStop = components["schemas"]["AgentStop"];
export type AgentStopList = components["schemas"]["AgentStopList"];
export type PickupRun = components["schemas"]["PickupRun"];
export type PickupRunList = components["schemas"]["PickupRunList"];
export type CreatePickupRequest = components["schemas"]["CreatePickupRequest"];
export type AssignPickupRequest = components["schemas"]["AssignPickupRequest"];
export type CompletePickupRequest =
  components["schemas"]["CompletePickupRequest"];
export type ScanType = components["schemas"]["ScanType"];
export type ScanRequest = components["schemas"]["ScanRequest"];
export type ScanResult = components["schemas"]["ScanResult"];
export type ScanHistoryEntry = components["schemas"]["ScanHistoryEntry"];
export type ScanHistoryPage = CursorPageOf<ScanHistoryEntry>;
export type Bag = components["schemas"]["Bag"];
export type BagSummary = components["schemas"]["BagSummary"];
export type BagPage = CursorPageOf<BagSummary>;
export type CreateBagRequest = components["schemas"]["CreateBagRequest"];
export type AddBagItemsResult = components["schemas"]["AddBagItemsResult"];
export type Manifest = components["schemas"]["Manifest"];
export type ManifestSummary = components["schemas"]["ManifestSummary"];
export type ManifestPage = CursorPageOf<ManifestSummary>;
export type CreateManifestRequest =
  components["schemas"]["CreateManifestRequest"];
export type AddManifestContentResult =
  components["schemas"]["AddManifestContentResult"];
export type Carrier = components["schemas"]["Carrier"];
export type CarrierList = components["schemas"]["CarrierList"];
export type Vehicle = components["schemas"]["Vehicle"];
export type VehicleList = components["schemas"]["VehicleList"];
export type Driver = components["schemas"]["Driver"];
export type DriverList = components["schemas"]["DriverList"];
export type Trip = components["schemas"]["Trip"];
export type TripSummary = components["schemas"]["TripSummary"];
export type TripPage = CursorPageOf<TripSummary>;
export type HubWorkloadSummary = components["schemas"]["HubWorkloadSummary"];
export type HubInbound = components["schemas"]["HubInbound"];
export type CustodyItem = components["schemas"]["CustodyItem"];
export type CustodyPage = CursorPageOf<CustodyItem>;
export type OperationalException =
  components["schemas"]["OperationalException"];
export type ExceptionSummary = components["schemas"]["ExceptionSummary"];
export type ExceptionPage = CursorPageOf<ExceptionSummary>;
export type Reconciliation = components["schemas"]["Reconciliation"];
export type ReconciliationSummary =
  components["schemas"]["ReconciliationSummary"];
export type ReconciliationPage = CursorPageOf<ReconciliationSummary>;
export type ReconciliationScanResult =
  components["schemas"]["ReconciliationScanResult"];
export type DeliveryRun = components["schemas"]["DeliveryRun"];
export type DeliveryRunSummary = components["schemas"]["DeliveryRunSummary"];
export type DeliveryRunPage = CursorPageOf<DeliveryRunSummary>;
export type DeliveryQueueItem = components["schemas"]["DeliveryQueueItem"];
export type DeliveryQueue = components["schemas"]["DeliveryQueue"];
export type DeliveryAttemptRequest =
  components["schemas"]["DeliveryAttemptRequest"];
export type DeliveryAttemptResult =
  components["schemas"]["DeliveryAttemptResult"];
export type IssuedOTP = components["schemas"]["IssuedOTP"];
export type NDRAction = components["schemas"]["NDRAction"];
export type NDRReason = components["schemas"]["NDRReason"];
export type NDRReasonList = components["schemas"]["NDRReasonList"];
export type NDRCase = components["schemas"]["NDRCase"];
export type NDRCaseSummary = components["schemas"]["NDRCaseSummary"];
export type NDRCasePage = CursorPageOf<NDRCaseSummary>;
export type RTOCase = components["schemas"]["RTOCase"];
export type RTOCaseSummary = components["schemas"]["RTOCaseSummary"];
export type RTOCasePage = CursorPageOf<RTOCaseSummary>;
export type ProofOfDelivery = components["schemas"]["ProofOfDelivery"];
export type TrackingResult = components["schemas"]["TrackingResult"];

// Release 3 — generated finance types. Response and request aliases are
// derived directly from OpenAPI operations so page code never maintains a
// second DTO contract by hand.
export type LedgerAccount = components["schemas"]["LedgerAccount"];
export type JournalTransaction = components["schemas"]["JournalTransaction"];
export type JournalEntry = components["schemas"]["JournalEntry"];
export type StatementLine = components["schemas"]["StatementLine"];
export type TrialBalance = components["schemas"]["TrialBalance"];
export type AccountingPeriod = components["schemas"]["AccountingPeriod"];
export type PostJournalRequest = components["schemas"]["PostJournalRequest"];
export type CommissionRule = components["schemas"]["CommissionRule"];
export type CommissionRuleVersion =
  components["schemas"]["CommissionRuleVersion"];
export type CommissionRuleRequest =
  components["schemas"]["CommissionRuleRequest"];
export type CommissionVersionRequest =
  components["schemas"]["CommissionVersionRequest"];
export type CommissionCalculation =
  components["schemas"]["CommissionCalculation"];
export type CommissionSimulateRequest =
  components["schemas"]["CommissionSimulateRequest"];
export type CommissionSimulateResult =
  components["schemas"]["CommissionSimulateResult"];
export type CODObligation = components["schemas"]["CODObligation"];
export type CODCollection = components["schemas"]["CODCollection"];
export type CODSummary = components["schemas"]["CODSummary"];
export type CODCustodyPosition = components["schemas"]["CODCustodyPosition"];
export type CODCustodyTransfer = components["schemas"]["CODCustodyTransfer"];
export type CODReconciliation = components["schemas"]["CODReconciliation"];
export type CODReconciliationItem =
  components["schemas"]["CODReconciliationItem"];
export type CODRemittance = components["schemas"]["CODRemittance"];
export type CODAdjustment = components["schemas"]["CODAdjustment"];
export type CODDispute = components["schemas"]["CODDispute"];
export type Settlement = components["schemas"]["Settlement"];
export type SettlementLine = components["schemas"]["SettlementLine"];
export type SettlementResult = components["schemas"]["SettlementResult"];
export type Invoice = components["schemas"]["Invoice"];
export type InvoiceLine = components["schemas"]["InvoiceLine"];
export type InvoiceResult = components["schemas"]["InvoiceResult"];
export type CreditNote = components["schemas"]["CreditNote"];
export type DraftInvoiceRequest = components["schemas"]["DraftInvoiceRequest"];

export type LedgerAccountListResponse =
  operations["listLedgerAccounts"]["responses"][200]["content"]["application/json"];
export type AccountStatementResponse =
  operations["getLedgerAccountStatement"]["responses"][200]["content"]["application/json"];
export type JournalListResponse =
  operations["listJournals"]["responses"][200]["content"]["application/json"];
export type JournalDetailResponse =
  operations["getJournal"]["responses"][200]["content"]["application/json"];
export type LedgerHealthResponse =
  operations["getLedgerHealth"]["responses"][200]["content"]["application/json"];
export type AccountingPeriodListResponse =
  operations["listAccountingPeriods"]["responses"][200]["content"]["application/json"];
export type CommissionRuleListResponse =
  operations["listCommissionRules"]["responses"][200]["content"]["application/json"];
export type CommissionRuleDetailResponse =
  operations["getCommissionRule"]["responses"][200]["content"]["application/json"];
export type CommissionCalculationListResponse =
  operations["listCommissionCalculations"]["responses"][200]["content"]["application/json"];
export type CODObligationListResponse =
  operations["listCODObligations"]["responses"][200]["content"]["application/json"];
export type CODObligationDetailResponse =
  operations["getCODObligation"]["responses"][200]["content"]["application/json"];
export type SettlementListResponse =
  operations["listSettlements"]["responses"][200]["content"]["application/json"];
export type SettlementDetailResponse =
  operations["getSettlement"]["responses"][200]["content"]["application/json"];
export type InvoiceListResponse =
  operations["listInvoices"]["responses"][200]["content"]["application/json"];
export type CustomerOutstandingResponse =
  operations["getCustomerOutstanding"]["responses"][200]["content"]["application/json"];

// Release 4 — partner integration administration. These aliases deliberately
// stay tied to the generated OpenAPI surface because secrets, scopes, delivery
// payloads, and replay state are security-sensitive contract boundaries.
export type PartnerScope = components["schemas"]["PartnerScope"];
export type APIKey = components["schemas"]["APIKey"];
export type IssueAPIKeyRequest = components["schemas"]["IssueAPIKeyRequest"];
export type IssuedAPIKey = components["schemas"]["IssuedAPIKey"];
export type APIKeyUsage = components["schemas"]["APIKeyUsage"];
export type WebhookEventType = components["schemas"]["WebhookEventType"];
export type CreateWebhookEndpointRequest =
  components["schemas"]["CreateWebhookEndpointRequest"];
export type WebhookEndpoint = components["schemas"]["WebhookEndpoint"];
export type CreatedWebhookEndpoint =
  components["schemas"]["CreatedWebhookEndpoint"];
export type WebhookSubscription = components["schemas"]["WebhookSubscription"];
export type WebhookDelivery = components["schemas"]["WebhookDelivery"];
export type WebhookDeliveryDetail =
  components["schemas"]["WebhookDeliveryDetail"];
export type WebhookDeliveryPage = components["schemas"]["WebhookDeliveryPage"];
export type WebhookAttempt = components["schemas"]["WebhookAttempt"];
export type APIKeyListResponse =
  operations["listAPIKeys"]["responses"][200]["content"]["application/json"];
export type WebhookEventListResponse =
  operations["listWebhookEventTypes"]["responses"][200]["content"]["application/json"];
export type WebhookEndpointListResponse =
  operations["listWebhookEndpoints"]["responses"][200]["content"]["application/json"];
export type WebhookSubscriptionListResponse =
  operations["listWebhookSubscriptions"]["responses"][200]["content"]["application/json"];
export type WebhookAttemptListResponse =
  operations["listWebhookAttempts"]["responses"][200]["content"]["application/json"];
export type ReplayWebhookResponse =
  operations["replayWebhookDelivery"]["responses"][202]["content"]["application/json"];

// Release 4 — productization surfaces. Keep response envelopes coupled to
// OpenAPI operations; portal projections and dashboard aggregates intentionally
// differ from the internal operational DTOs.
export type NotificationChannel = components["schemas"]["NotificationChannel"];
export type NotificationStatus = components["schemas"]["NotificationStatus"];
export type NotificationSummary = components["schemas"]["NotificationSummary"];
export type NotificationDetail = components["schemas"]["NotificationDetail"];
export type NotificationAttempt = components["schemas"]["NotificationAttempt"];
export type NotificationTemplate =
  components["schemas"]["NotificationTemplate"];
export type CreateNotificationTemplateRequest =
  components["schemas"]["CreateNotificationTemplateRequest"];
export type NotificationPreference =
  components["schemas"]["NotificationPreference"];
export type NotificationListResponse =
  operations["listNotifications"]["responses"][200]["content"]["application/json"];
export type NotificationTemplateListResponse =
  operations["listNotificationTemplates"]["responses"][200]["content"]["application/json"];
export type NotificationChannelListResponse =
  operations["listNotificationChannels"]["responses"][200]["content"]["application/json"];
export type NotificationHealthResponse =
  operations["notificationHealth"]["responses"][200]["content"]["application/json"];
export type NotificationPreviewResponse =
  operations["previewNotificationTemplate"]["responses"][200]["content"]["application/json"];

export type PortalCustomerSummary =
  components["schemas"]["PortalCustomerSummary"];
export type PortalAccount = components["schemas"]["PortalAccount"];
export type PortalShipment = components["schemas"]["PortalShipment"];
export type PortalInvoice = components["schemas"]["PortalInvoice"];
export type PortalFranchiseSummary =
  components["schemas"]["PortalFranchiseSummary"];
export type FranchiseShipment = components["schemas"]["FranchiseShipment"];
export type PortalSettlement = components["schemas"]["PortalSettlement"];
export type PortalCustomerAccountListResponse =
  operations["portalCustomerAccounts"]["responses"][200]["content"]["application/json"];
export type PortalCustomerShipmentListResponse =
  operations["portalCustomerShipments"]["responses"][200]["content"]["application/json"];
export type PortalCustomerInvoiceListResponse =
  operations["portalCustomerInvoices"]["responses"][200]["content"]["application/json"];
export type PortalFranchiseShipmentListResponse =
  operations["portalFranchiseShipments"]["responses"][200]["content"]["application/json"];
export type PortalFranchiseSettlementListResponse =
  operations["portalFranchiseSettlements"]["responses"][200]["content"]["application/json"];

export type CommandOverview = components["schemas"]["CommandOverview"];
export type CommandDayPoint = components["schemas"]["CommandDayPoint"];
export type UnitPerformance = components["schemas"]["UnitPerformance"];
export type ServicePerformance = components["schemas"]["ServicePerformance"];
export type UnitBacklog = components["schemas"]["UnitBacklog"];
export type OperationalSnapshot = components["schemas"]["OperationalSnapshot"];
export type CommandTrendResponse =
  operations["commandCentreTrend"]["responses"][200]["content"]["application/json"];
export type CommandUnitResponse =
  operations["commandCentreByUnit"]["responses"][200]["content"]["application/json"];
export type CommandServiceResponse =
  operations["commandCentreByService"]["responses"][200]["content"]["application/json"];
export type CommandBacklogResponse =
  operations["commandCentreBacklog"]["responses"][200]["content"]["application/json"];
export type CommandSnapshotResponse =
  operations["commandCentreSnapshots"]["responses"][200]["content"]["application/json"];

export type ConsoleParcel = components["schemas"]["ConsoleParcel"];
export type ConsoleQueueItem = components["schemas"]["ConsoleQueueItem"];
export type ConsoleFacilitySummary =
  components["schemas"]["ConsoleFacilitySummary"];
export type ConsoleBag = components["schemas"]["ConsoleBag"];
export type ConsoleInboundManifest =
  components["schemas"]["ConsoleInboundManifest"];
export type ConsoleQueueResponse =
  operations["consoleQueue"]["responses"][200]["content"]["application/json"];
export type ConsoleBagResponse =
  operations["consoleBags"]["responses"][200]["content"]["application/json"];
export type ConsoleInboundResponse =
  operations["consoleInbound"]["responses"][200]["content"]["application/json"];

export type ReportType = components["schemas"]["ReportType"];
export type ReportStatus = components["schemas"]["ReportStatus"];
export type QueueReportRequest = components["schemas"]["QueueReportRequest"];
export type ReportRun = components["schemas"]["ReportRun"];
export type ReportListResponse =
  operations["listReportRuns"]["responses"][200]["content"]["application/json"];
export type ReportTypeListResponse =
  operations["listReportTypes"]["responses"][200]["content"]["application/json"];
export type QueuedReportResponse =
  operations["queueReport"]["responses"][202]["content"]["application/json"];

export type CODCollectionRequest =
  operations["recordCODCollection"]["requestBody"]["content"]["application/json"];
export type CODCollectionResult =
  operations["recordCODCollection"]["responses"][200]["content"]["application/json"];
export type InvoiceCreditNotePage =
  operations["listInvoiceCreditNotes"]["responses"][200]["content"]["application/json"];
export type CODTransferRequest =
  operations["declareCODTransfer"]["requestBody"]["content"]["application/json"];
export type CODReconciliationRequest =
  operations["openCODReconciliation"]["requestBody"]["content"]["application/json"];
export type CODReconciliationResult =
  operations["openCODReconciliation"]["responses"][201]["content"]["application/json"];
export type CODCountRequest =
  operations["recordCODCount"]["requestBody"]["content"]["application/json"];
export type CODCompleteReconciliationRequest =
  operations["completeCODReconciliation"]["requestBody"]["content"]["application/json"];
export type SettlementGenerateRequest =
  operations["generateSettlement"]["requestBody"]["content"]["application/json"];
export type SettlementPaymentRequest =
  operations["paySettlement"]["requestBody"]["content"]["application/json"];
export type InvoicePaymentRequest =
  operations["payInvoice"]["requestBody"]["content"]["application/json"];
export type CreditNoteRequest =
  operations["raiseCreditNote"]["requestBody"]["content"]["application/json"];

export type OffsetPage = components["schemas"]["OffsetPage"];
export type CursorPage = components["schemas"]["CursorPage"];
export type OffsetPageOf<T> = Omit<OffsetPage, "data"> & { data?: T[] };
export type CursorPageOf<T> = Omit<CursorPage, "data"> & { data?: T[] };
export type UserListResponse = OffsetPageOf<User>;
export type OperatingUnitListResponse = OffsetPageOf<OperatingUnit>;
export type ServiceListResponse = OffsetPageOf<CourierService>;
export type ShipmentListResponse = CursorPageOf<ShipmentListItem>;

export class ApiError extends Error {
  code: string;
  details: Record<string, unknown>;
  requestId?: string;
  status: number;

  constructor(status: number, error: ApiErrorBody, requestId?: string) {
    super(error.message ?? "The request could not be completed.");
    this.name = "ApiError";
    this.status = status;
    this.code = error.code ?? "INTERNAL_ERROR";
    this.details = error.details ?? {};
    this.requestId = requestId ?? error.requestId;
  }
}

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? "";
const REFRESH_KEY = "courier.refresh-token";
let accessToken: string | undefined;
let refreshPromise: Promise<boolean> | undefined;
let onAuthenticationLost: (() => void) | undefined;

const channel =
  typeof BroadcastChannel !== "undefined"
    ? new BroadcastChannel("courier-auth")
    : undefined;
channel?.addEventListener(
  "message",
  (event: MessageEvent<{ type: string }>) => {
    if (event.data.type === "logout") clearTokens(false);
  },
);

export function configureApiAuth(callbacks: {
  onAuthenticationLost: () => void;
}) {
  onAuthenticationLost = callbacks.onAuthenticationLost;
}

export function setTokens(tokens: TokenPair) {
  if (!tokens.accessToken || !tokens.refreshToken)
    throw new Error("The authentication response did not include both tokens.");
  accessToken = tokens.accessToken;
  sessionStorage.setItem(REFRESH_KEY, tokens.refreshToken);
}

export function clearTokens(broadcast = true) {
  accessToken = undefined;
  sessionStorage.removeItem(REFRESH_KEY);
  if (broadcast) channel?.postMessage({ type: "logout" });
}

export function hasRefreshToken() {
  return Boolean(sessionStorage.getItem(REFRESH_KEY));
}

async function parseError(response: Response) {
  let body: ApiErrorEnvelope | undefined;
  try {
    body = (await response.json()) as ApiErrorEnvelope;
  } catch {
    body = undefined;
  }
  const requestId = response.headers.get("X-Request-Id") ?? undefined;
  const fallbackMessages: Record<number, string> = {
    401: "Your secure session could not be verified. Sign in and try again.",
    403: "You do not have permission to perform this action.",
    404: "The requested information could not be found.",
    409: "This record changed or conflicts with its current state. Refresh before trying again.",
    422: "Some submitted information is invalid. Review the highlighted fields and try again.",
    429: "Too many requests were sent. Wait a moment before trying again.",
    500: "The service could not complete this request. Try again or contact support with the request ID.",
  };
  return new ApiError(
    response.status,
    body?.error ?? {
      code: "INTERNAL_ERROR",
      message:
        fallbackMessages[response.status] ??
        "The server returned an unreadable response.",
    },
    requestId,
  );
}

function apiUrl(path: string) {
  if (!path.startsWith("/") || path.startsWith("//") || path.includes("\\")) {
    throw new Error("Refused an unsafe API request URL.");
  }
  return `${API_BASE.replace(/\/$/, "")}${path}`;
}

async function rotateRefreshToken() {
  const tokenBeforeLock = sessionStorage.getItem(REFRESH_KEY);
  if (!tokenBeforeLock) return false;
  const perform = async () => {
    const currentToken = sessionStorage.getItem(REFRESH_KEY);
    if (!currentToken) return false;
    if (currentToken !== tokenBeforeLock) return true;
    const response = await fetch(apiUrl("/api/v1/auth/refresh"), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ refreshToken: currentToken }),
    });
    if (!response.ok) {
      const error = await parseError(response);
      clearTokens();
      onAuthenticationLost?.();
      if (error.code !== "TOKEN_EXPIRED") throw error;
      return false;
    }
    setTokens((await response.json()) as TokenPair);
    return true;
  };

  const locks = navigator.locks;
  return locks ? locks.request("courier-auth-refresh", perform) : perform();
}

async function refreshOnce() {
  refreshPromise ??= rotateRefreshToken().finally(() => {
    refreshPromise = undefined;
  });
  return refreshPromise;
}

export interface ApiRequestOptions extends Omit<RequestInit, "body"> {
  body?: unknown;
  auth?: boolean;
  retryAuth?: boolean;
  responseType?: "auto" | "blob";
}

export async function apiRequest<T>(
  path: string,
  options: ApiRequestOptions = {},
): Promise<T> {
  const {
    body,
    auth = true,
    retryAuth = true,
    responseType = "auto",
    ...init
  } = options;
  if (auth && retryAuth && !accessToken && hasRefreshToken()) {
    if (!(await refreshOnce())) {
      throw new ApiError(401, {
        code: "TOKEN_EXPIRED",
        message: "Your secure session has expired. Sign in again.",
      });
    }
  }
  const headers = new Headers(init.headers);
  if (body != null && !(body instanceof FormData))
    headers.set("Content-Type", "application/json");
  if (auth && accessToken)
    headers.set("Authorization", `Bearer ${accessToken}`);
  const response = await fetch(apiUrl(path), {
    ...init,
    headers,
    ...(body != null
      ? { body: body instanceof FormData ? body : JSON.stringify(body) }
      : {}),
  });
  if (response.ok) {
    if (response.status === 204) return undefined as T;
    if (responseType === "blob") return (await response.blob()) as T;
    const contentType = response.headers.get("content-type") ?? "";
    return (
      contentType.includes("application/json")
        ? await response.json()
        : await response.text()
    ) as T;
  }
  const error = await parseError(response);
  if (
    auth &&
    retryAuth &&
    response.status === 401 &&
    error.code === "TOKEN_EXPIRED" &&
    (await refreshOnce())
  ) {
    return apiRequest<T>(path, { ...options, retryAuth: false });
  }
  if (
    response.status === 401 &&
    ["TOKEN_INVALID", "TOKEN_REVOKED"].includes(error.code)
  ) {
    clearTokens();
    onAuthenticationLost?.();
  }
  throw error;
}

const DEVICE_KEY = "courier.operations-device-id";

export function operationalHeaders(kind = "event") {
  let deviceId = localStorage.getItem(DEVICE_KEY);
  if (!deviceId) {
    deviceId = `web-${crypto.randomUUID()}`;
    localStorage.setItem(DEVICE_KEY, deviceId);
  }
  return {
    "X-Device-Id": deviceId,
    "X-Device-Event-Id": `${kind}-${crypto.randomUUID()}`,
  };
}

export function idempotencyHeaders(kind = "console") {
  return { "Idempotency-Key": `${kind}-${crypto.randomUUID()}` };
}

export function queryString(
  params: Record<string, string | number | boolean | undefined>,
) {
  const search = new URLSearchParams();
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== "") search.set(key, String(value));
  });
  const value = search.toString();
  return value ? `?${value}` : "";
}

export function getValidationFields(error: unknown) {
  if (!(error instanceof ApiError)) return [];
  const fields = error.details.fields;
  if (!Array.isArray(fields)) return [];
  return fields.filter(
    (field): field is { field: string; message: string } =>
      typeof field === "object" &&
      field !== null &&
      "field" in field &&
      "message" in field,
  );
}
