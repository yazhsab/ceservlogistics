import type { Page, Route } from "@playwright/test";

export const allPermissions = [
  "shipment.read",
  "shipment.create",
  "shipment.cancel",
  "shipment.label",
  "operating_unit.read",
  "operating_unit.manage",
  "franchise.read",
  "pincode.read",
  "pincode.manage",
  "zone.read",
  "zone.manage",
  "serviceability.check",
  "service_area.read",
  "routing.debug",
  "route.read",
  "route.manage",
  "courier_service.read",
  "courier_service.manage",
  "rate_card.read",
  "rate_card.manage",
  "pricing.quote",
  "customer.read",
  "customer.create",
  "customer.update",
  "user.read",
  "user.create",
  "organization.read",
  "audit.read",
  "pickup.read",
  "pickup.create",
  "pickup.assign",
  "pickup.respond",
  "pickup.complete",
  "pickup.cancel",
  "scan.read",
  "scan.inbound",
  "scan.outbound",
  "scan.sort",
  "scan.hold",
  "scan.exception",
  "bag.read",
  "bag.manage",
  "bag.close",
  "bag.dispatch",
  "bag.receive",
  "bag.open",
  "bag.exception_edit",
  "manifest.read",
  "manifest.manage",
  "manifest.close",
  "manifest.dispatch",
  "manifest.receive",
  "carrier.read",
  "carrier.manage",
  "vehicle.read",
  "vehicle.manage",
  "driver.read",
  "driver.manage",
  "trip.read",
  "trip.manage",
  "trip.cancel",
  "linehaul.depart",
  "linehaul.arrive",
  "hub.dashboard",
  "exception.read",
  "exception.create",
  "exception.resolve",
  "reconciliation.manage",
  "delivery.read",
  "delivery.manage",
  "delivery.assign",
  "delivery.dispatch",
  "delivery.complete",
  "delivery.otp_issue",
  "ndr.read",
  "ndr.manage",
  "ndr.config",
  "rto.read",
  "rto.manage",
  "pod.read",
  "pod.submit",
  "ledger.read",
  "ledger.account_manage",
  "ledger.post",
  "ledger.reverse",
  "ledger.period_manage",
  "commission.read",
  "commission.config",
  "commission.simulate",
  "commission.calculate",
  "commission.post",
  "commission.reverse",
  "cod.read",
  "cod.collect",
  "cod.transfer",
  "cod.accept",
  "cod.reconcile",
  "cod.remit",
  "cod.adjust_request",
  "cod.adjust_approve",
  "cod.dispute",
  "settlement.read",
  "settlement.calculate",
  "settlement.submit",
  "settlement.approve",
  "settlement.pay",
  "settlement.adjust_request",
  "settlement.adjust_approve",
  "settlement.cancel",
  "invoice.read",
  "invoice.create",
  "invoice.issue",
  "invoice.payment",
  "creditnote.create",
  "creditnote.approve",
  "notification.read",
  "notification.template",
  "notification.preference",
  "notification.retry",
  "portal.customer",
  "portal.franchise",
  "portal.console",
  "command.read",
  "report.read",
  "report.run",
  "report.finance",
  "apikey.read",
  "apikey.manage",
  "webhook.read",
  "webhook.manage",
  "webhook.replay",
];

export const shipment = {
  id: "shp_test_01",
  awb: "CSV260808000001",
  referenceNumber: "WEB-001",
  status: "BOOKED",
  statusChangedAt: "2026-08-08T10:00:00+05:30",
  paymentMode: "PREPAID",
  bookedAt: "2026-08-08T10:00:00+05:30",
  createdAt: "2026-08-08T10:00:00+05:30",
  customer: { id: "cus_test_01", code: "ACME", name: "Acme Retail" },
  service: { id: "svc_test_01", code: "EXPRESS", name: "Express Air" },
  origin: {
    pincode: "100001",
    branch: { code: "LOS01", name: "Lagos Branch" },
    hub: { code: "LOSH", name: "Lagos Hub" },
    isRemote: false,
  },
  destination: {
    pincode: "900001",
    branch: { code: "ABV01", name: "Federal Capital Territory Branch" },
    hub: { code: "ABVH", name: "Federal Capital Territory Hub" },
    isRemote: false,
  },
  route: {
    routeCode: "LOS-ABV-AIR",
    resolutionSource: "SERVICE_ROUTE",
    transitHours: 24,
    slaHours: 30,
    legs: [
      {
        sequence: 1,
        from: "LOSH",
        fromName: "Lagos Hub",
        to: "ABVH",
        toName: "Federal Capital Territory Hub",
        mode: "AIR",
        transitHours: 3,
      },
    ],
  },
  charges: {
    currency: "NGN",
    rateCardCode: "RETAIL",
    rateCardVersion: 1,
    originZoneCode: "SOUTH",
    destinationZoneCode: "NORTH",
    freightMinor: 7500,
    surchargeTotalMinor: 1388,
    discountTotalMinor: 0,
    taxTotalMinor: 1600,
    totalMinor: 10488,
    lineItems: [
      {
        kind: "FREIGHT",
        code: "FREIGHT",
        label: "Freight charge",
        amountMinor: 7500,
        explanation: "Base 500g plus one additional weight step.",
      },
      {
        kind: "SURCHARGE",
        code: "FUEL",
        label: "Fuel surcharge",
        amountMinor: 1388,
        explanation: "18.50% on freight.",
      },
      {
        kind: "TAX",
        code: "VAT",
        label: "VAT",
        amountMinor: 1600,
        explanation: "Nigeria VAT.",
      },
    ],
  },
  packages: [
    {
      id: "pkg_01",
      sequence: 1,
      pieceBarcode: "CSV260808000001-001",
      actualWeightGrams: 500,
      volumetricWeightGrams: 600,
      lengthMm: 200,
      widthMm: 150,
      heightMm: 100,
      contentDescription: "Documents",
    },
  ],
  addresses: {
    sender: {
      contactName: "Chiamaka Okafor",
      phone: "08031234567",
      line1: "12 MG Road",
      city: "Lagos",
      state: "Lagos",
      pincode: "100001",
    },
    recipient: {
      contactName: "Amina Bello",
      phone: "08037654321",
      line1: "8 Janpath",
      city: "Abuja",
      state: "Federal Capital Territory",
      pincode: "900001",
    },
  },
  pieceCount: 1,
  actualWeightGrams: 500,
  volumetricWeightGrams: 600,
  chargeableWeightGrams: 1000,
  currency: "NGN",
  totalAmountMinor: 10488,
  slaHours: 30,
  promisedDeliveryAt: "2026-08-09T16:00:00+05:30",
  contentDescription: "Documents",
  allowedTransitions: ["CANCELLED"],
};

const facility = {
  id: "ou_blr_01",
  code: "LOS01",
  name: "Lagos Branch",
  unitType: "COMPANY_BRANCH",
};

const destinationFacility = {
  id: "ou_del_01",
  code: "ABV01",
  name: "Federal Capital Territory Branch",
  unitType: "COMPANY_BRANCH",
};

const pickupFixture = {
  id: "pup_01",
  referenceCode: "PU-260808-001",
  pickupType: "SCHEDULED",
  status: "SCHEDULED",
  scheduledDate: "2026-08-08",
  window: {
    start: "2026-08-08T09:00:00+05:30",
    end: "2026-08-08T11:00:00+05:30",
  },
  customer: { id: "cus_test_01", code: "ACME", name: "Acme Retail" },
  address: {
    contactName: "Chiamaka Okafor",
    contactPhone: "08031234567",
    line1: "12 MG Road",
    city: "Lagos",
    state: "Lagos",
    pincode: "100001",
  },
  expectedPieceCount: 3,
  expectedWeightGrams: 2500,
  specialInstructions: "Call at the gate",
  allowedTransitions: ["ASSIGNED", "CANCELLED"],
};

const bagFixture = {
  id: "bag_01",
  bagCode: "BAG-BLR-0001",
  barcode: "BAG-BLR-0001",
  bagType: "STANDARD",
  direction: "FORWARD",
  status: "OPEN",
  origin: facility,
  destination: destinationFacility,
  shipmentCount: 1,
  pieceCount: 1,
  totalWeightGrams: 1000,
  sealNumber: "SEAL-001",
  allowedTransitions: ["CLOSED"],
  contents: [
    {
      shipmentId: shipment.id,
      awb: shipment.awb,
      pieceBarcode: `${shipment.awb}-001`,
      status: "AT_ORIGIN_BRANCH",
      weightGrams: 1000,
      destinationPincode: "900001",
    },
  ],
  events: [],
};

const manifestFixture = {
  id: "mnf_01",
  manifestCode: "MNF-BLR-DEL-0001",
  direction: "FORWARD",
  status: "DRAFT",
  origin: facility,
  destination: destinationFacility,
  bagCount: 1,
  looseShipmentCount: 0,
  totalShipmentCount: 1,
  totalPieceCount: 1,
  totalWeightGrams: 1000,
  documentReady: false,
  allowedTransitions: ["CLOSED"],
  bags: [
    {
      bagId: bagFixture.id,
      bagCode: bagFixture.bagCode,
      barcode: bagFixture.barcode,
      destinationCode: destinationFacility.code,
      declaredShipmentCount: 1,
      declaredPieceCount: 1,
      declaredWeightGrams: 1000,
      lineStatus: "CLOSED",
    },
  ],
  looseShipments: [],
  events: [],
};

const tripFixture = {
  id: "trip_01",
  tripCode: "TRIP-LOS-ABV-0001",
  direction: "FORWARD",
  mode: "ROAD",
  status: "PLANNED",
  origin: facility,
  destination: destinationFacility,
  carrier: { id: "car_01", code: "CSV", name: "Ceserve Fleet" },
  vehicle: {
    id: "veh_01",
    registrationNumber: "LAG-482-XY",
    vehicleType: "TRUCK",
  },
  driver: {
    id: "drv_01",
    code: "DRV-001",
    name: "Tunde Balogun",
    phone: "08031230000",
  },
  scheduled: {
    departure: "2026-08-09T08:00:00+01:00",
    arrival: "2026-08-09T18:00:00+01:00",
  },
  manifestCount: 1,
  bagCount: 1,
  shipmentCount: 1,
  totalWeightGrams: 1000,
  currentLegSequence: 1,
  externalReference: "LINEHAUL-001",
  allowedTransitions: ["DEPARTED", "CANCELLED"],
  legs: [
    {
      id: "leg_01",
      sequence: 1,
      status: "PLANNED",
      origin: facility,
      destination: destinationFacility,
      scheduled: {
        departure: "2026-08-09T08:00:00+01:00",
        arrival: "2026-08-09T18:00:00+01:00",
      },
    },
  ],
  manifests: [
    {
      id: manifestFixture.id,
      manifestCode: manifestFixture.manifestCode,
      originCode: facility.code,
      destinationCode: destinationFacility.code,
      bagCount: 1,
      shipmentCount: 1,
      weightGrams: 1000,
      status: "CLOSED",
    },
  ],
  events: [],
};

const reconciliationFixture = {
  id: "rec_01",
  reconciliationCode: "REC-DEL-0001",
  subjectType: "MANIFEST",
  status: "IN_PROGRESS",
  facility: destinationFacility,
  manifest: {
    id: manifestFixture.id,
    code: manifestFixture.manifestCode,
  },
  expectedCount: 1,
  scannedCount: 0,
  matchedCount: 0,
  missingCount: 1,
  excessCount: 0,
  damagedCount: 0,
  items: [
    {
      id: "reci_01",
      barcode: shipment.awb,
      awb: shipment.awb,
      expected: true,
      scanned: false,
      outcome: "MISSING",
    },
  ],
  allowedTransitions: ["COMPLETED"],
};

const deliveryRunFixture = {
  id: "dr_01",
  runCode: "DR-DEL-0001",
  status: "PLANNED",
  runDate: "2026-08-08",
  branch: destinationFacility,
  agent: { id: "usr_agent", code: "AG001", name: "Tunde Balogun" },
  plannedStops: 1,
  completedStops: 0,
  deliveredCount: 0,
  failedCount: 0,
  allowedTransitions: ["DISPATCHED"],
  stops: [
    {
      id: "dst_01",
      stopSequence: 1,
      shipmentId: shipment.id,
      awb: shipment.awb,
      status: "PLANNED",
      recipientName: "Amina Bello",
      recipientPhone: "08037654321",
      line1: "8 Janpath",
      city: "Abuja",
      state: "Federal Capital Territory",
      pincode: "900001",
      latitude: 28.6139,
      longitude: 77.209,
      pieceCount: 1,
      paymentMode: "PREPAID",
      codAmountMinor: 0,
      currency: "NGN",
      allowedActions: ["ATTEMPT"],
    },
  ],
};

const ndrFixture = {
  id: "ndr_01",
  caseCode: "NDR-0001",
  status: "OPEN",
  currentAction: "CONTACT_REQUIRED",
  awb: shipment.awb,
  shipmentId: shipment.id,
  recipientName: "Amina Bello",
  recipientPhone: "08037654321",
  reasonCode: "CUSTOMER_UNAVAILABLE",
  reasonLabel: "Customer unavailable",
  currentReasonCode: "CUSTOMER_UNAVAILABLE",
  reasonName: "Customer unavailable",
  reasonCategory: "CUSTOMER",
  openedAt: "2026-08-08T14:00:00+05:30",
  createdAt: "2026-08-08T14:00:00+05:30",
  nextAttemptAt: "2026-08-09T10:00:00+05:30",
  attemptCount: 1,
  maxAttempts: 3,
  shipmentStatus: "NDR",
  branch: destinationFacility,
  availableActions: ["REATTEMPT", "RESCHEDULE", "RTO"],
  timeline: [
    {
      id: "ndre_01",
      action: "DELIVERY_FAILED",
      notes: "Customer did not answer",
      occurredAt: "2026-08-08T14:00:00+05:30",
      actorName: "Tunde Balogun",
    },
  ],
};

const rtoFixture = {
  id: "rto_01",
  caseCode: "RTO-0001",
  status: "INITIATED",
  awb: shipment.awb,
  shipmentId: shipment.id,
  reasonCode: "NDR_EXHAUSTED",
  initiatedAt: "2026-08-08T15:00:00+05:30",
  currentFacility: destinationFacility,
  returnBranch: facility,
  returnAddress: shipment.addresses.sender,
  originalSender: shipment.addresses.sender,
  routeResolutionSource: "REVERSE_SERVICE_ROUTE",
  chargeBearer: "CUSTOMER",
  currency: "NGN",
  rtoChargeMinor: 7500,
  allowedTransitions: ["DISPATCHED"],
  timeline: [
    {
      id: "rtoe_01",
      status: "INITIATED",
      occurredAt: "2026-08-08T15:00:00+05:30",
      facility: destinationFacility,
      description: "Return approved after delivery attempts.",
    },
  ],
};

const podFixture = {
  id: "pod_01",
  awb: shipment.awb,
  shipmentId: shipment.id,
  podType: "DELIVERY",
  deliveredAt: "2026-08-08T13:45:00+05:30",
  recipientName: "Amina Bello",
  recipientRelationship: "SELF",
  relationship: "SELF",
  verificationMethod: "OTP",
  otpVerified: true,
  signatureCaptured: false,
  recordedAt: "2026-08-08T13:46:00+05:30",
  deliveredBy: { id: "usr_agent", code: "AG001", name: "Tunde Balogun" },
  latitude: 28.6139,
  longitude: 77.209,
  location: { latitude: 28.6139, longitude: 77.209, accuracyMetres: 18 },
  artifacts: [],
  submittedBy: { id: "usr_agent", name: "Tunde Balogun" },
  remarks: "Delivered in good condition",
};

const commissionRuleFixture = {
  id: "crl_01",
  code: "DEL-GOLD",
  name: "Gold franchise delivery commission",
  commissionType: "DELIVERY",
  recipientRole: "DESTINATION_FRANCHISE",
  schemeCode: "STANDARD",
  franchiseCategory: "GOLD",
  paymentMode: null,
  specificity: 8,
  priority: 20,
  status: "ACTIVE",
};

const commissionVersionFixture = {
  id: "crv_01",
  versionNo: 2,
  calculationMethod: "PERCENTAGE",
  currency: "NGN",
  rateBp: 1000,
  basis: "FREIGHT",
  effectiveFrom: "2026-08-01",
  effectiveTo: null,
  status: "ACTIVE",
};

const commissionCalculationFixture = {
  id: "ccl_01",
  commissionType: "DELIVERY",
  awb: shipment.awb,
  qualifyingEvent: "DELIVERED",
  qualifiedAt: "2026-08-08T13:45:00+05:30",
  recipientType: "FRANCHISE",
  franchiseCode: "FRN-LOS-01",
  ruleCode: "DEL-GOLD",
  ruleVersionNo: 2,
  calculationMethod: "PERCENTAGE",
  basis: "FREIGHT",
  baseAmountMinor: 50000,
  rateBp: 1000,
  grossAmountMinor: 5000,
  amountMinor: 5000,
  currency: "NGN",
  calculationTrace: [
    {
      description: "Basis FREIGHT",
      value: 50000,
      detail: "Shipment freight snapshot",
    },
    {
      description: "Apply 10% of basis",
      value: 5000,
      detail: "50000 × 1000 bp, rounded half-up",
    },
  ],
  status: "POSTED",
};

const ledgerAccountFixture = {
  id: "lac_01",
  code: "1200",
  name: "Trade Receivable",
  accountType: "ASSET",
  normalBalance: "DEBIT",
  currency: "NGN",
  isSystem: true,
  isActive: true,
  balanceMinor: 67850,
  totalDebitMinor: 67850,
  totalCreditMinor: 0,
};

const journalFixture = {
  id: "jrn_01",
  transactionNumber: "JV-202608-000001",
  status: "POSTED",
  postingDate: "2026-08-08",
  currency: "NGN",
  totalMinor: 67850,
  entryCount: 3,
  sourceType: "INVOICE",
  sourceId: "inv_01",
  purpose: "ISSUE",
  description: "Invoice INV/2026-27/000042",
  postedAt: "2026-08-08T10:00:00+05:30",
};

const journalEntriesFixture = [
  {
    id: "jen_01",
    lineNo: 1,
    accountCode: "1200",
    accountName: "Trade Receivable",
    debitMinor: 67850,
    creditMinor: 0,
    currency: "NGN",
    memo: "Customer receivable",
  },
  {
    id: "jen_02",
    lineNo: 2,
    accountCode: "4000",
    accountName: "Freight Revenue",
    debitMinor: 0,
    creditMinor: 57500,
    currency: "NGN",
    memo: "Freight revenue",
  },
  {
    id: "jen_03",
    lineNo: 3,
    accountCode: "2300",
    accountName: "Tax Payable",
    debitMinor: 0,
    creditMinor: 10350,
    currency: "NGN",
    memo: "VAT payable",
  },
];

const codObligationFixture = {
  id: "cod_01",
  awb: shipment.awb,
  expectedMinor: 200000,
  collectedMinor: 200000,
  remittedMinor: 0,
  adjustedMinor: 0,
  currency: "NGN",
  status: "BRANCH_RECEIVED",
  custodianType: "OPERATING_UNIT",
  custodianId: 12,
  collectedAt: "2026-08-08T13:45:00+05:30",
  remittedAt: null,
};

const settlementFixture = {
  id: "stl_01",
  settlementNumber: "STL-202608-00001",
  franchiseCode: "FRN-LOS-01",
  franchiseName: "Lagos Central Franchise",
  periodType: "MONTHLY",
  periodStart: "2026-08-01",
  periodEnd: "2026-08-31",
  status: "UNDER_REVIEW",
  currency: "NGN",
  commissionMinor: 50000,
  incentiveMinor: 5000,
  codLiabilityMinor: -90000,
  chargesMinor: -5000,
  penaltiesMinor: 0,
  adjustmentsMinor: 0,
  taxMinor: 0,
  withholdingMinor: 0,
  openingBalanceMinor: 0,
  netAmountMinor: -40000,
  paidMinor: 0,
  calculationHash: "sha256:finance-fixture",
  calculatedBy: 101,
  approvedBy: null,
};

const settlementLinesFixture = [
  {
    id: "sln_01",
    lineNo: 1,
    category: "DELIVERY_COMMISSION",
    description: "Delivery commission",
    amountMinor: 50000,
    currency: "NGN",
    quantity: 10,
    sourceType: "COMMISSION_CALCULATION",
    sourceId: 1,
    sourcePublicId: "ccl_01",
    awb: shipment.awb,
  },
  {
    id: "sln_02",
    lineNo: 2,
    category: "COD_LIABILITY",
    description: "COD held by franchise",
    amountMinor: -90000,
    currency: "NGN",
    quantity: 1,
    sourceType: "COD_OBLIGATION",
    sourceId: 1,
    sourcePublicId: "cod_01",
    awb: shipment.awb,
  },
];

const invoiceFixture = {
  id: "inv_01",
  invoiceNumber: "INV/2026-27/000042",
  seriesCode: "INV",
  customerCode: "ACME",
  customerName: "Acme Retail",
  billingMode: "PERIODIC",
  periodStart: "2026-08-01",
  periodEnd: "2026-08-31",
  issueDate: "2026-08-31",
  dueDate: "2026-09-15",
  status: "ISSUED",
  currency: "NGN",
  subtotalMinor: 57500,
  discountMinor: 0,
  taxableMinor: 57500,
  taxMinor: 10350,
  roundingMinor: 0,
  totalMinor: 67850,
  paidMinor: 0,
  creditedMinor: 0,
  shipmentCount: 1,
  documentObjectId: null,
};

const invoiceResultFixture = {
  invoice: invoiceFixture,
  lines: [
    {
      id: "ivl_01",
      lineNo: 1,
      lineType: "FREIGHT",
      description: "Freight for shipment",
      hsnSacCode: "996812",
      quantity: 1,
      unitPriceMinor: 57500,
      amountMinor: 57500,
      discountMinor: 0,
      taxableMinor: 57500,
      taxMinor: 10350,
      totalMinor: 67850,
      currency: "NGN",
      awb: shipment.awb,
    },
  ],
  taxes: [
    {
      componentCode: "VAT",
      componentName: "VAT",
      rateBp: 1800,
      taxableMinor: 57500,
      taxMinor: 10350,
      currency: "NGN",
    },
  ],
};

const apiKeyFixture = {
  id: "apk_01JRELEASE4TESTCREDENTIAL01",
  name: "Warehouse platform",
  keyId: "key_live_csv_01",
  secretHint: "s3cr",
  scopes: ["shipment:create", "shipment:read", "tracking:read"],
  allowedCidrs: ["203.0.113.0/24"],
  status: "ACTIVE",
  expiresAt: "2027-08-09T00:00:00Z",
  lastUsedAt: "2026-08-09T09:45:00+05:30",
  requestCount: 1842,
  rateLimitPerMinute: 240,
  createdAt: "2026-08-01T10:00:00+05:30",
};

const webhookEvents = [
  "shipment.booked",
  "shipment.in_transit",
  "shipment.delivered",
  "shipment.delivery_failed",
  "shipment.ndr",
  "shipment.rto_initiated",
  "pickup.completed",
  "pod.captured",
  "cod.collected",
];

const webhookEndpointFixture = {
  id: "whe_01JRELEASE4TESTENDPOINT001",
  name: "Order platform production",
  url: "https://partner.example.com/ceserve/events",
  status: "ACTIVE",
  consecutiveFailures: 0,
  lastSuccessAt: "2026-08-09T09:45:00+05:30",
  lastFailureAt: null,
  maxAttempts: 6,
  timeoutSeconds: 10,
  createdAt: "2026-08-01T11:00:00+05:30",
};

const webhookDeliveryFixture = {
  id: "whd_01JRELEASE4TESTDELIVERY01",
  endpoint: webhookEndpointFixture.id,
  eventType: "shipment.delivered",
  eventId: "evt_01JRELEASE4BUSINESSEVENT01",
  status: "FAILED",
  attemptCount: 2,
  lastStatusCode: 503,
  lastError: "Partner endpoint returned Service Unavailable",
  nextAttemptAt: "2026-08-09T10:15:00+05:30",
  deliveredAt: null,
  createdAt: "2026-08-09T10:00:00+05:30",
};

export async function installMockApi(
  page: Page,
  options?: {
    bookingDelayMs?: number;
    permissions?: string[];
    user?: {
      email?: string;
      fullName?: string;
      roles?: string[];
      portal?: Record<string, unknown>;
      operatingUnitIds?: string[];
      hasOrganizationWideAccess?: boolean;
    };
  },
) {
  let shipmentPosts = 0;
  let pickupStatus = pickupFixture.status;
  let pickupAgent: { id: string; code: string; name: string } | undefined;
  let bagStatus = bagFixture.status;
  let manifestStatus = manifestFixture.status;
  let tripStatus = tripFixture.status;
  let reconciliationScanned = false;
  let reconciliationStatus = reconciliationFixture.status;
  let deliveryStatus = deliveryRunFixture.status;
  let settlementStatus = settlementFixture.status;
  let settlementPaidMinor = 0;
  let invoiceStatus = invoiceFixture.status;
  let invoicePaidMinor = 0;
  let apiKeys = [apiKeyFixture];
  let webhookEndpoints = [webhookEndpointFixture];
  let webhookSubscriptions = [
    { id: "whs_01", eventType: "shipment.delivered", isActive: true },
  ];
  const permissions = options?.permissions ?? allPermissions;
  const userProfile = {
    id: "usr_01",
    email: "operator@ceserve.test",
    fullName: "Kemi Adeyemi",
    status: "ACTIVE",
    isSuperAdmin: false,
    mustChangePassword: false,
    organization: {
      id: "org_01",
      code: "CSV",
      name: "Ceserve Logistics",
      timezone: "Africa/Lagos",
      currency: "NGN",
      awbPrefix: "CSV",
    },
    roles: ["OPERATIONS_ADMIN"],
    permissions,
    portal: {
      isCustomerUser: false,
      customers: [],
      franchise: null,
    },
    operatingUnitIds: [],
    hasOrganizationWideAccess: true,
    ...options?.user,
  };
  const portalShipmentFixture = {
    id: "shp_portal_01",
    awb: "CSV260809000101",
    reference: "LAG-ORDER-7781",
    status: "IN_TRANSIT",
    statusChangedAt: "2026-08-09T10:15:00+01:00",
    service: "Express",
    pieces: 2,
    originPincode: "100001",
    destinationPincode: "900001",
    recipientName: "Amina Bello",
    recipientCity: "Abuja",
    paymentMode: "PREPAID",
    totalAmountMinor: 185000,
    codAmountMinor: 0,
    currency: "NGN",
    promisedDeliveryAt: "2026-08-10T17:00:00+01:00",
    bookedAt: "2026-08-09T08:00:00+01:00",
  };
  const notificationFixture = {
    id: "ntf_01JRELEASE4NOTIFICATION001",
    eventType: "SHIPMENT_OUT_FOR_DELIVERY",
    channel: "SMS",
    status: "FAILED",
    recipientType: "RECIPIENT",
    recipientName: "Amina Bello",
    subject: null,
    attemptCount: 2,
    lastError: "Provider timeout",
    nextAttemptAt: null,
    awb: portalShipmentFixture.awb,
    reference: portalShipmentFixture.reference,
    createdAt: "2026-08-09T10:20:00+01:00",
  };
  let notificationTemplates: Array<
    Record<string, unknown> & { id: string; variables: string[] }
  > = [
    {
      id: "ntt_01JRELEASE4TEMPLATE000001",
      code: "SHIPMENT_OFD_SMS_EN_NG",
      name: "Out for delivery · SMS",
      eventType: "SHIPMENT_OUT_FOR_DELIVERY",
      channel: "SMS",
      locale: "en-NG",
      subject: null,
      body: "Your shipment {{awb}} is out for delivery. Track: {{trackingUrl}}",
      providerRef: "CESERVE",
      variables: ["awb", "trackingUrl"],
      isActive: true,
      createdAt: "2026-08-08T09:00:00+01:00",
      updatedAt: "2026-08-09T09:00:00+01:00",
    },
  ];
  let reportRuns: Array<
    Record<string, unknown> & {
      id: string;
      status: string;
      reportType?: string;
      format?: string;
    }
  > = [
    {
      id: "rpt_01JRELEASE4COMPLETED0001",
      reportType: "SHIPMENT_VOLUME",
      format: "CSV",
      status: "COMPLETED",
      rowCount: 1842,
      byteSize: 104820,
      periodStart: "2026-08-01",
      periodEnd: "2026-08-09",
      startedAt: "2026-08-09T09:00:00+01:00",
      completedAt: "2026-08-09T09:00:04+01:00",
      durationMs: 4120,
      expiresAt: "2026-08-10T09:00:04+01:00",
      requestedBy: "Kemi Adeyemi",
      createdAt: "2026-08-09T09:00:00+01:00",
    },
  ];
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname;
    const method = request.method();
    if (path === "/api/v1/auth/login")
      return json(route, {
        tokens: {
          accessToken: "access",
          refreshToken: "refresh",
          tokenType: "Bearer",
          expiresIn: 900,
          accessTokenExpiresAt: "2030-08-08T10:15:00+01:00",
          refreshTokenExpiresAt: "2030-09-08T10:00:00+01:00",
          sessionId: "ses_01",
        },
        user: userProfile,
      });
    if (path === "/api/v1/auth/refresh")
      return json(route, {
        accessToken: "access-refreshed",
        refreshToken: "refresh-rotated",
        tokenType: "Bearer",
        expiresIn: 900,
        accessTokenExpiresAt: "2030-08-08T10:30:00+01:00",
        refreshTokenExpiresAt: "2030-09-08T10:15:00+01:00",
        sessionId: "ses_01",
      });
    if (path === "/api/v1/auth/me") return json(route, userProfile);
    if (path === "/api/v1/portal/customer/summary")
      return json(route, {
        windowDays: 90,
        total: 18,
        delivered: 12,
        inProgress: 4,
        exceptions: 2,
        returning: 0,
        spendMinor: 2845000,
        codBookedMinor: 450000,
        currency: "NGN",
        deliveryRateBasisPoints: 9231,
      });
    if (path === "/api/v1/portal/customer/accounts")
      return json(route, {
        data: [
          {
            id: "cus_test_01",
            code: "LAGRETAIL",
            name: "Lagos Retail Limited",
            customerType: "BUSINESS",
            status: "ACTIVE",
            creditLimitMinor: 50000000,
            creditUsedMinor: 18250000,
            creditAvailableMinor: 31750000,
            paymentTermsDays: 30,
            creditStatus: "GOOD",
            currency: "NGN",
          },
        ],
      });
    if (path === "/api/v1/portal/customer/shipments")
      return json(route, { data: [portalShipmentFixture] });
    if (
      path ===
      `/api/v1/portal/customer/shipments/${portalShipmentFixture.id}/track`
    )
      return json(route, {
        awb: portalShipmentFixture.awb,
        milestone: "IN_TRANSIT",
        statusTitle: "On the way",
        statusDescription: "Your shipment is travelling to Abuja.",
        origin: "Lagos",
        destination: "Abuja",
        service: "Express",
        expectedDelivery: "10 Aug 2026",
        pieceCount: 2,
        amountDueOnDeliveryMinor: 0,
        currency: "NGN",
        lastUpdatedAt: "2026-08-09T10:15:00+01:00",
        events: [
          {
            milestone: "BOOKED",
            title: "Shipment booked",
            description: "We received the shipment details.",
            location: "Lagos",
            occurredAt: "2026-08-09T08:00:00+01:00",
          },
          {
            milestone: "IN_TRANSIT",
            title: "On the way",
            description: "The shipment is moving toward its destination.",
            location: "Nigeria",
            occurredAt: "2026-08-09T10:15:00+01:00",
          },
        ],
      });
    if (path === "/api/v1/portal/customer/invoices")
      return json(route, {
        data: [
          {
            id: "inv_portal_01",
            invoiceNumber: "INV/NG/2026/00142",
            status: "OVERDUE",
            issueDate: "2026-07-31",
            dueDate: "2026-08-07",
            totalMinor: 845000,
            paidMinor: 300000,
            outstandingMinor: 545000,
            currency: "NGN",
            periodStart: "2026-07-01",
            periodEnd: "2026-07-31",
            customer: "Lagos Retail Limited",
          },
        ],
      });
    if (path === "/api/v1/portal/franchise/summary")
      return json(route, {
        franchise: {
          id: "frn_portal_01",
          code: "LAG-IKJ",
          name: "Ikeja Franchise",
        },
        from: "2026-08-01",
        to: "2026-08-09",
        booked: 128,
        delivered: 104,
        ndr: 7,
        rto: 2,
        revenueMinor: 18450000,
        commissionEarnedMinor: 1845000,
        commissionSettledMinor: 1000000,
        commissionOutstandingMinor: 845000,
        codInCustodyMinor: 2320000,
        codObligations: 14,
        codAgedOver48h: 3,
        currency: "NGN",
        deliveryRateBasisPoints: 8125,
      });
    if (path === "/api/v1/portal/franchise/shipments")
      return json(route, {
        franchise: {
          id: "frn_portal_01",
          code: "LAG-IKJ",
          name: "Ikeja Franchise",
        },
        role: url.searchParams.get("role") ?? "ORIGIN",
        data: [
          {
            id: "shp_franchise_01",
            awb: "CSV260809000201",
            status: "BOOKED",
            customer: "Walk-in customer",
            service: "Express",
            pieces: 1,
            destinationPincode: "500001",
            paymentMode: "COD",
            totalAmountMinor: 145000,
            codAmountMinor: 750000,
            currency: "NGN",
            bookedAt: "2026-08-09T09:30:00+01:00",
            role:
              url.searchParams.get("role") === "DESTINATION"
                ? "DESTINATION"
                : "ORIGIN",
          },
        ],
      });
    if (path === "/api/v1/portal/franchise/settlements")
      return json(route, {
        franchise: {
          id: "frn_portal_01",
          code: "LAG-IKJ",
          name: "Ikeja Franchise",
        },
        data: [
          {
            id: "set_portal_01",
            settlementNumber: "SET/LAG-IKJ/2026-08-01",
            status: "APPROVED",
            periodStart: "2026-08-01",
            periodEnd: "2026-08-07",
            netAmountMinor: 645000,
            paidMinor: 0,
            outstandingMinor: 645000,
            currency: "NGN",
            approvedAt: "2026-08-08T11:00:00+01:00",
          },
        ],
      });
    if (path === "/api/v1/console/summary")
      return json(route, {
        unit: { id: "ou_lag_01", code: "LAG-IKJ", name: "Ikeja Branch" },
        inCustody: 284,
        outForDelivery: 92,
        exceptions: 8,
        held: 3,
        overdue: 5,
        openBags: 11,
        inboundExpected: 6,
      });
    if (path === "/api/v1/console/inbound")
      return json(route, {
        data: [
          {
            id: "man_console_01",
            manifestCode: "MAN-ABJ-LAG-0042",
            status: "DISPATCHED",
            bags: 5,
            shipments: 86,
            weightGrams: 128400,
            origin: "Abuja Hub",
            dispatchedAt: "2026-08-09T06:00:00+01:00",
          },
        ],
      });
    if (path === "/api/v1/console/bags")
      return json(route, {
        data: [
          {
            id: "bag_console_01",
            bagCode: "BAG-LAG-00981",
            status: "OPEN",
            shipments: 18,
            weightGrams: 28400,
            destination: "Abuja Hub",
          },
        ],
      });
    if (path === "/api/v1/console/queue")
      return json(route, {
        unit: { id: "ou_lag_01", code: "LAG-IKJ", name: "Ikeja Branch" },
        data: [
          {
            id: "shp_console_01",
            awb: "CSV260809000301",
            status: "IN_TRANSIT",
            pieces: 1,
            destinationPincode: "900001",
            destinationBranch: "Abuja Central",
            promisedBy: "2026-08-09T08:00:00+01:00",
            isHeld: true,
          },
        ],
      });
    if (path.startsWith("/api/v1/console/lookup/"))
      return json(route, {
        id: "shp_console_01",
        awb: decodeURIComponent(path.split("/").at(-1) ?? ""),
        status: "IN_TRANSIT",
        pieces: 1,
        destinationPincode: "900001",
        destinationCity: "Abuja",
        destinationBranch: "Abuja Central",
        currency: "NGN",
        isHeld: false,
        isReturning: false,
        custodyUnit: "LAG-IKJ",
      });
    if (path === "/api/v1/command-centre") {
      const scoped = Boolean(url.searchParams.get("unitId"));
      return json(route, {
        period: {
          from: url.searchParams.get("from") ?? "2026-08-09",
          to: url.searchParams.get("to") ?? "2026-08-09",
          booked: scoped ? 42 : 1280,
          pickedUp: 1104,
          inTransit: 462,
          outForDelivery: 184,
          delivered: 948,
          ndr: 38,
          rto: 12,
          cancelled: 8,
          damaged: 2,
          lost: 1,
          slaBreaches: 17,
          revenueMinor: 482500000,
          codAmountMinor: 118400000,
          weightGrams: 2084000,
          deliveryRateBasisPoints: 7406,
          ndrRateBasisPoints: 297,
        },
        live: {
          byStatus: { BOOKED: 74, IN_TRANSIT: 462, OUT_FOR_DELIVERY: 184 },
          total: 720,
          onHold: 9,
        },
        alerts: {
          slaBreached: 17,
          slaBreachedToday: 6,
          openNdr: 38,
          ndrByReason: { CUSTOMER_UNAVAILABLE: 24, ADDRESS_PROBLEM: 14 },
          openExceptions: 12,
          criticalExceptions: 3,
        },
        movement: {
          scansLastHour: 584,
          activeTrips: { IN_TRANSIT: 7 },
          openBags: 42,
          draftManifests: 8,
          manifestsInFlight: 12,
        },
        money: {
          codOutstandingMinor: 28450000,
          codObligations: 84,
          codAgedOver48h: 9,
          currency: "NGN",
        },
        consistency: {
          periodTotals: "exact; transaction-maintained",
          liveBacklog: "live; counted at request time",
          snapshots: "sampled every 15 minutes",
        },
      });
    }
    if (path === "/api/v1/command-centre/trend")
      return json(route, {
        data: [
          {
            date: "2026-08-08",
            booked: 1180,
            delivered: 901,
            ndr: 41,
            rto: 11,
            slaBreaches: 19,
          },
          {
            date: "2026-08-09",
            booked: 1280,
            delivered: 948,
            ndr: 38,
            rto: 12,
            slaBreaches: 17,
          },
        ],
        consistency: "exact; transaction-maintained",
      });
    if (path === "/api/v1/command-centre/units")
      return json(route, {
        data: [
          {
            unitId: "ou_lag_01",
            code: "LAG-IKJ",
            name: "Ikeja Branch",
            unitType: "COMPANY_BRANCH",
            booked: 420,
            delivered: 338,
            ndr: 11,
            rto: 4,
            slaBreaches: 5,
            deliveryRateBasisPoints: 8048,
          },
        ],
      });
    if (path === "/api/v1/command-centre/services")
      return json(route, {
        data: [
          {
            code: "EXPRESS",
            name: "Express",
            booked: 760,
            delivered: 602,
            ndr: 21,
            slaBreaches: 9,
            deliveryRateBasisPoints: 7921,
          },
        ],
      });
    if (path === "/api/v1/command-centre/backlog")
      return json(route, {
        data: [
          {
            unitId: "ou_lag_01",
            code: "LAG-IKJ",
            name: "Ikeja Branch",
            count: 284,
          },
        ],
        consistency: "live; counted at request time",
      });
    if (path === "/api/v1/command-centre/snapshots")
      return json(route, {
        data: [
          {
            capturedAt: "2026-08-09T10:15:00+01:00",
            inCustody: 720,
            awaitingPickup: 74,
            inbound: 164,
            outbound: 92,
            readyForDelivery: 184,
            ndrOpen: 38,
            exceptionsOpen: 12,
            codInCustodyMinor: 28450000,
          },
        ],
        consistency: "sampled every 15 minutes",
      });
    if (path === "/api/v1/notifications/templates" && method === "GET")
      return json(route, { data: notificationTemplates });
    if (path === "/api/v1/notifications/templates" && method === "POST") {
      const body = request.postDataJSON() as Record<string, unknown>;
      const templateBody = String(body.body ?? "");
      const variables = [
        ...templateBody.matchAll(/{{\s*([a-zA-Z0-9_.-]+)\s*}}/g),
      ]
        .map((match) => match[1])
        .filter((value): value is string => Boolean(value));
      const template = {
        ...body,
        id: "ntt_01JRELEASE4NEWTEMPLATE001",
        variables,
        isActive: true,
        createdAt: "2026-08-09T11:00:00+01:00",
        updatedAt: "2026-08-09T11:00:00+01:00",
      };
      notificationTemplates = [template, ...notificationTemplates];
      return json(route, template, 201);
    }
    if (
      path.endsWith("/preview") &&
      path.includes("/api/v1/notifications/templates/")
    ) {
      const body = request.postDataJSON() as {
        variables?: Record<string, string>;
      };
      const template = notificationTemplates.find((item) =>
        path.includes(String(item.id)),
      );
      const missingVariables =
        template?.variables?.filter(
          (variable) => !body.variables?.[variable],
        ) ?? [];
      let rendered = String(template?.body ?? "");
      Object.entries(body.variables ?? {}).forEach(([name, value]) => {
        rendered = rendered.replaceAll(`{{${name}}}`, value);
      });
      return json(route, {
        subject: template?.subject,
        body: rendered,
        missingVariables,
      });
    }
    if (
      path.startsWith("/api/v1/notifications/templates/") &&
      method === "PATCH"
    ) {
      const id = path.split("/").at(-1);
      const body = request.postDataJSON() as Record<string, unknown>;
      notificationTemplates = notificationTemplates.map((item) =>
        item.id === id
          ? { ...item, ...body, updatedAt: "2026-08-09T11:05:00+01:00" }
          : item,
      );
      return json(
        route,
        notificationTemplates.find((item) => item.id === id),
      );
    }
    if (path === "/api/v1/notifications/channels")
      return json(route, {
        data: [
          { channel: "SMS", configured: true },
          { channel: "EMAIL", configured: true },
          { channel: "WHATSAPP", configured: true },
          { channel: "PUSH", configured: false },
        ],
      });
    if (path === "/api/v1/notifications/health")
      return json(route, {
        windowHours: 24,
        byStatus: { DELIVERED: 842, FAILED: 12, SUPPRESSED: 31 },
        configuredChannels: ["SMS", "EMAIL", "WHATSAPP"],
      });
    if (path === "/api/v1/notifications" && method === "GET")
      return json(route, { data: [notificationFixture] });
    if (path === `/api/v1/notifications/${notificationFixture.id}`)
      return json(route, {
        ...notificationFixture,
        recipientAddress: "+234••••••4182",
        locale: "en-NG",
        templateCode: "SHIPMENT_OFD_SMS_EN_NG",
        body: `Your shipment ${portalShipmentFixture.awb} is out for delivery.`,
        maxRetries: 5,
        attempts: [
          {
            attemptNo: 1,
            provider: "Termii",
            outcome: "FAILED",
            providerStatus: "TIMEOUT",
            errorMessage: "Provider timeout",
            retryable: true,
            durationMs: 10000,
            attemptedAt: "2026-08-09T10:20:10+01:00",
          },
          {
            attemptNo: 2,
            provider: "Termii",
            outcome: "FAILED",
            providerStatus: "TIMEOUT",
            errorMessage: "Provider timeout",
            retryable: true,
            durationMs: 10000,
            attemptedAt: "2026-08-09T10:22:10+01:00",
          },
        ],
      });
    if (path.endsWith("/retry") && path.includes("/api/v1/notifications/"))
      return json(
        route,
        { id: notificationFixture.id, status: "PENDING", attemptCount: 0 },
        202,
      );
    if (path.endsWith("/cancel") && path.includes("/api/v1/notifications/"))
      return json(route, { id: notificationFixture.id, status: "CANCELLED" });
    if (path === "/api/v1/reports/types")
      return json(route, {
        data: [
          "SHIPMENT_VOLUME",
          "BRANCH_PERFORMANCE",
          "FRANCHISE_PERFORMANCE",
          "SERVICE_PERFORMANCE",
          "SLA",
          "NDR",
          "RTO",
          "COD_AGING",
          "COMMISSION",
          "SETTLEMENT",
          "REVENUE",
          "EXCEPTIONS",
        ].map((reportType) => ({
          reportType,
          requiresFinancePermission: [
            "COD_AGING",
            "COMMISSION",
            "SETTLEMENT",
            "REVENUE",
          ].includes(reportType),
        })),
      });
    if (path === "/api/v1/reports" && method === "GET")
      return json(route, { data: reportRuns });
    if (path === "/api/v1/reports" && method === "POST") {
      const body = request.postDataJSON() as Record<string, unknown>;
      const run = {
        id: "rpt_01JRELEASE4QUEUED000001",
        ...body,
        status: "QUEUED",
        createdAt: "2026-08-09T11:30:00+01:00",
        pollUrl: "/api/v1/reports/rpt_01JRELEASE4QUEUED000001",
      };
      reportRuns = [run, ...reportRuns];
      return json(route, run, 202);
    }
    if (path.endsWith("/download") && path.startsWith("/api/v1/reports/"))
      return route.fulfill({
        status: 200,
        contentType: "text/csv",
        headers: { "Content-Disposition": "attachment; filename=report.csv" },
        body: "awb,status\nCSV260809000101,IN_TRANSIT\n",
      });
    if (path.endsWith("/cancel") && path.startsWith("/api/v1/reports/"))
      return json(route, { id: path.split("/")[4], status: "CANCELLED" });
    if (path.startsWith("/api/v1/reports/") && method === "GET")
      return json(
        route,
        reportRuns.find((run) => run.id === path.split("/")[4]),
      );
    if (path === "/api/v1/api-keys" && method === "GET")
      return json(route, {
        data: apiKeys,
        pagination: {
          limit: 50,
          offset: 0,
          totalItems: apiKeys.length,
          hasMore: false,
        },
      });
    if (path === "/api/v1/api-keys" && method === "POST") {
      const body = request.postDataJSON() as {
        name: string;
        scopes?: string[];
        allowedCidrs?: string[];
        expiresAt?: string;
        rateLimitPerMinute?: number;
      };
      const key = {
        ...apiKeyFixture,
        id: "apk_01JRELEASE4NEWCREDENTIAL001",
        name: body.name,
        keyId: "key_live_csv_new",
        secretHint: "newS",
        scopes: body.scopes ?? [],
        allowedCidrs: body.allowedCidrs ?? [],
        expiresAt: body.expiresAt ?? apiKeyFixture.expiresAt,
        rateLimitPerMinute:
          body.rateLimitPerMinute ?? apiKeyFixture.rateLimitPerMinute,
        requestCount: 0,
        lastUsedAt: apiKeyFixture.lastUsedAt,
      };
      apiKeys = [key, ...apiKeys];
      return json(
        route,
        {
          key,
          secret: "one-time-secret-value",
          token: "key_live_csv_new.one-time-secret-value",
          warning:
            "Store this token now. It is hashed and cannot be shown again.",
        },
        201,
      );
    }
    if (path === `/api/v1/api-keys/${apiKeyFixture.id}/usage`)
      return json(route, {
        windowHours: 24,
        requestCount: 1842,
        errorCount: 12,
        avgDurationMs: 184,
        recent: [
          {
            method: "GET",
            route: "/api/v1/partner/tracking/{awb}",
            statusCode: 200,
            durationMs: 91,
            occurredAt: "2026-08-09T09:45:00+05:30",
          },
          {
            method: "POST",
            route: "/api/v1/partner/shipments",
            statusCode: 422,
            durationMs: 210,
            errorCode: "VALIDATION_FAILED",
            occurredAt: "2026-08-09T09:42:00+05:30",
          },
        ],
      });
    if (path.includes("/api/v1/api-keys/") && path.endsWith("/suspend")) {
      const body = request.postDataJSON() as { suspend: boolean };
      const id = path.split("/")[4];
      apiKeys = apiKeys.map((key) =>
        key.id === id
          ? { ...key, status: body.suspend ? "SUSPENDED" : "ACTIVE" }
          : key,
      );
      return json(
        route,
        apiKeys.find((key) => key.id === id),
      );
    }
    if (path.includes("/api/v1/api-keys/") && path.endsWith("/revoke")) {
      const id = path.split("/")[4];
      apiKeys = apiKeys.map((key) =>
        key.id === id ? { ...key, status: "REVOKED" } : key,
      );
      return json(
        route,
        apiKeys.find((key) => key.id === id),
      );
    }
    if (path === "/api/v1/webhooks/events")
      return json(route, { data: webhookEvents });
    if (path === "/api/v1/webhooks/endpoints" && method === "GET")
      return json(route, {
        data: webhookEndpoints,
        pagination: {
          limit: 50,
          offset: 0,
          totalItems: webhookEndpoints.length,
          hasMore: false,
        },
      });
    if (path === "/api/v1/webhooks/endpoints" && method === "POST") {
      const body = request.postDataJSON() as {
        name: string;
        url: string;
        events: string[];
      };
      const endpoint = {
        ...webhookEndpointFixture,
        id: "whe_01JRELEASE4NEWENDPOINT0001",
        name: body.name,
        url: body.url,
        lastSuccessAt: webhookEndpointFixture.lastSuccessAt,
      };
      webhookEndpoints = [endpoint, ...webhookEndpoints];
      webhookSubscriptions = body.events.map((eventType, index) => ({
        id: `whs_new_${index}`,
        eventType,
        isActive: true,
      }));
      return json(
        route,
        {
          endpoint,
          signingSecret: "whsec_one_time_signing_secret",
          signatureHeader: {
            header: "Webhook-Signature",
            scheme: 'v1=HMAC_SHA256(secret, "<timestamp>.<body>")',
            timestamp: "Webhook-Timestamp",
            toleranceSec: 300,
          },
          warning: "Store this signing secret now; it cannot be recovered.",
        },
        201,
      );
    }
    if (
      path.includes("/api/v1/webhooks/endpoints/") &&
      path.endsWith("/status")
    ) {
      const body = request.postDataJSON() as {
        status: "ACTIVE" | "PAUSED" | "DISABLED";
      };
      const id = path.split("/")[5];
      webhookEndpoints = webhookEndpoints.map((endpoint) =>
        endpoint.id === id ? { ...endpoint, status: body.status } : endpoint,
      );
      return json(
        route,
        webhookEndpoints.find((endpoint) => endpoint.id === id),
      );
    }
    if (
      path ===
        `/api/v1/webhooks/endpoints/${webhookEndpointFixture.id}/subscriptions` &&
      method === "GET"
    )
      return json(route, { data: webhookSubscriptions });
    if (
      path.includes("/api/v1/webhooks/endpoints/") &&
      path.endsWith("/subscriptions") &&
      method === "POST"
    ) {
      const body = request.postDataJSON() as { eventType: string };
      const subscription = {
        id: `whs_${webhookSubscriptions.length + 1}`,
        eventType: body.eventType,
        isActive: true,
      };
      webhookSubscriptions = [
        ...webhookSubscriptions.filter(
          (item) => item.eventType !== body.eventType,
        ),
        subscription,
      ];
      return json(route, subscription);
    }
    if (
      path.includes("/api/v1/webhooks/endpoints/") &&
      path.includes("/subscriptions/") &&
      method === "DELETE"
    ) {
      const eventType = decodeURIComponent(path.split("/").at(-1) ?? "");
      webhookSubscriptions = webhookSubscriptions.filter(
        (item) => item.eventType !== eventType,
      );
      return json(route, undefined, 204);
    }
    if (path === "/api/v1/webhooks/deliveries" && method === "GET")
      return json(route, { data: [webhookDeliveryFixture] });
    if (path === `/api/v1/webhooks/deliveries/${webhookDeliveryFixture.id}`)
      return json(route, {
        ...webhookDeliveryFixture,
        url: webhookEndpointFixture.url,
        payload: {
          id: webhookDeliveryFixture.eventId,
          type: webhookDeliveryFixture.eventType,
          createdAt: "2026-08-09T10:00:00+05:30",
          data: {
            shipmentId: shipment.id,
            awb: shipment.awb,
            status: "DELIVERED",
          },
        },
      });
    if (
      path ===
      `/api/v1/webhooks/deliveries/${webhookDeliveryFixture.id}/attempts`
    )
      return json(route, {
        data: [
          {
            attemptNo: 1,
            statusCode: 503,
            responseBody: "Service Unavailable",
            durationMs: 418,
            attemptedAt: "2026-08-09T10:00:10+05:30",
          },
          {
            attemptNo: 2,
            errorMessage: "Connection timeout",
            durationMs: 10000,
            attemptedAt: "2026-08-09T10:00:30+05:30",
          },
        ],
      });
    if (
      path === `/api/v1/webhooks/deliveries/${webhookDeliveryFixture.id}/replay`
    )
      return json(
        route,
        {
          id: "whd_01JRELEASE4REPLAYDELIVERY01",
          replayOf: webhookDeliveryFixture.id,
          status: "PENDING",
          eventType: webhookDeliveryFixture.eventType,
          eventId: webhookDeliveryFixture.eventId,
        },
        202,
      );
    if (path === "/api/v1/commission/rules" && method === "GET")
      return json(route, { data: [commissionRuleFixture], total: 1 });
    if (path === "/api/v1/commission/rules" && method === "POST")
      return json(route, commissionRuleFixture, 201);
    if (path === `/api/v1/commission/rules/${commissionRuleFixture.id}`)
      return json(route, {
        rule: commissionRuleFixture,
        versions: [commissionVersionFixture],
      });
    if (
      path === `/api/v1/commission/rules/${commissionRuleFixture.id}/versions`
    )
      return json(route, commissionVersionFixture, 201);
    if (path === "/api/v1/commission/simulate")
      return json(route, {
        matched: true,
        ruleCode: commissionRuleFixture.code,
        ruleName: commissionRuleFixture.name,
        versionNo: 2,
        explanation:
          "Rule DEL-GOLD version 2 was selected from 2 matching rules on specificity 8.",
        calculation: {
          method: "PERCENTAGE",
          basis: "FREIGHT",
          basisValue: 50000,
          rateBp: 1000,
          grossAmountMinor: 5000,
          amountMinor: 5000,
          clamped: false,
          currency: "NGN",
          trace: commissionCalculationFixture.calculationTrace,
        },
        candidates: [
          {
            code: "DEL-GOLD",
            name: "Gold franchise delivery commission",
            specificity: 8,
            priority: 20,
            selected: true,
          },
          {
            code: "DEL-ALL",
            name: "Default delivery commission",
            specificity: 0,
            priority: 0,
            selected: false,
          },
        ],
      });
    if (path === "/api/v1/commission/calculations")
      return json(route, { data: [commissionCalculationFixture] });
    if (
      path ===
      `/api/v1/commission/calculations/${commissionCalculationFixture.id}`
    )
      return json(route, commissionCalculationFixture);
    if (path === "/api/v1/ledger/health")
      return json(route, {
        balanced: true,
        unbalancedCount: 0,
        unbalancedTransactions: [],
      });
    if (path === "/api/v1/ledger/accounts" && method === "GET")
      return json(route, { data: [ledgerAccountFixture], total: 1 });
    if (path === `/api/v1/ledger/accounts/${ledgerAccountFixture.id}/statement`)
      return json(route, {
        account: ledgerAccountFixture,
        data: [
          {
            id: "stl_ledger_01",
            transactionId: journalFixture.id,
            transactionNumber: journalFixture.transactionNumber,
            postingDate: "2026-08-08",
            description: journalFixture.description,
            sourceType: "INVOICE",
            sourceId: invoiceFixture.id,
            purpose: "ISSUE",
            debitMinor: 67850,
            creditMinor: 0,
            runningBalanceMinor: 67850,
            currency: "NGN",
            memo: "Customer receivable",
          },
        ],
        total: 1,
      });
    if (path === "/api/v1/ledger/journals" && method === "GET")
      return json(route, { data: [journalFixture] });
    if (path === `/api/v1/ledger/journals/${journalFixture.id}`)
      return json(route, {
        transaction: journalFixture,
        entries: journalEntriesFixture,
      });
    if (path === "/api/v1/ledger/trial-balance")
      return json(route, {
        asOf: "2026-08-31",
        currency: "NGN",
        totalDebitMinor: 67850,
        totalCreditMinor: 67850,
        differenceMinor: 0,
        balanced: true,
        rows: [
          {
            code: "1200",
            name: "Trade Receivable",
            accountType: "ASSET",
            normalBalance: "DEBIT",
            totalDebitMinor: 67850,
            totalCreditMinor: 0,
            balanceMinor: 67850,
          },
          {
            code: "4000",
            name: "Freight Revenue",
            accountType: "REVENUE",
            normalBalance: "CREDIT",
            totalDebitMinor: 0,
            totalCreditMinor: 67850,
            balanceMinor: 67850,
          },
        ],
      });
    if (path === "/api/v1/ledger/periods")
      return json(route, {
        data: [
          {
            id: "acp_01",
            code: "2026-08",
            startsOn: "2026-08-01",
            endsOn: "2026-08-31",
            status: "OPEN",
          },
        ],
      });
    if (path === "/api/v1/cod/summary")
      return json(route, {
        expectedCount: 1,
        withAgentCount: 0,
        withBranchCount: 1,
        withFranchiseCount: 0,
        remittedCount: 0,
        expectedMinor: 200000,
        inCustodyMinor: 200000,
        remittedMinor: 0,
        currency: "NGN",
      });
    if (path === "/api/v1/cod/obligations" && method === "GET")
      return json(route, { data: [codObligationFixture] });
    if (path === `/api/v1/cod/obligations/${codObligationFixture.id}`)
      return json(route, {
        obligation: codObligationFixture,
        collections: [
          {
            id: "cdc_01",
            amountMinor: 200000,
            currency: "NGN",
            paymentMode: "CASH",
            reference: "DELIVERY-CASH-01",
            collectedAt: "2026-08-08T13:45:00+05:30",
          },
        ],
      });
    if (path === "/api/v1/cod/custody/OPERATING_UNIT/12")
      return json(route, {
        partyType: "OPERATING_UNIT",
        partyId: "12",
        heldMinor: 200000,
        ledgerBalanceMinor: 200000,
        obligationCount: 1,
        reconciled: true,
        currency: "NGN",
      });
    if (path === "/api/v1/cod/reconciliations" && method === "POST")
      return json(
        route,
        {
          reconciliation: {
            id: "cdr_01",
            reconciliationCode: "CDR-202608-0001",
            partyType: "OPERATING_UNIT",
            partyId: 12,
            periodStart: "2026-08-01",
            periodEnd: "2026-08-31",
            expectedMinor: 200000,
            countedMinor: 0,
            shortageMinor: 0,
            excessMinor: 0,
            status: "OPEN",
            currency: "NGN",
          },
          expected: [{ obligationId: 1, expectedMinor: 200000 }],
        },
        201,
      );
    if (path === "/api/v1/cod/reconciliations/cdr_01/count")
      return json(route, {
        obligationId: 1,
        expectedMinor: 200000,
        countedMinor: 200000,
        outcome: "MATCHED",
      });
    if (path === "/api/v1/cod/reconciliations/cdr_01/complete")
      return json(route, {
        id: "cdr_01",
        reconciliationCode: "CDR-202608-0001",
        status: "COMPLETED",
        expectedMinor: 200000,
        countedMinor: 200000,
        currency: "NGN",
      });
    if (path === "/api/v1/settlements" && method === "GET")
      return json(route, {
        data: [
          {
            ...settlementFixture,
            status: settlementStatus,
            paidMinor: settlementPaidMinor,
          },
        ],
      });
    if (path === "/api/v1/settlements" && method === "POST")
      return json(
        route,
        {
          settlement: { ...settlementFixture, status: "CALCULATED" },
          lines: settlementLinesFixture,
          replayed: false,
        },
        201,
      );
    if (path === `/api/v1/settlements/${settlementFixture.id}`)
      return json(route, {
        settlement: {
          ...settlementFixture,
          status: settlementStatus,
          paidMinor: settlementPaidMinor,
          approvedBy: settlementStatus === "UNDER_REVIEW" ? null : 202,
        },
        lines: settlementLinesFixture,
        byCategory: [],
        approvals: [],
        payments: [],
      });
    if (path === `/api/v1/settlements/${settlementFixture.id}/approve`) {
      settlementStatus = "APPROVED";
      return json(route, {
        ...settlementFixture,
        status: settlementStatus,
        approvedBy: 202,
      });
    }
    if (path === `/api/v1/settlements/${settlementFixture.id}/payments`) {
      settlementPaidMinor = 40000;
      settlementStatus = "PAID";
      return json(
        route,
        {
          payment: { reference: "SETTLE-01" },
          settlement: {
            ...settlementFixture,
            status: settlementStatus,
            paidMinor: settlementPaidMinor,
          },
        },
        201,
      );
    }
    if (path === "/api/v1/invoices" && method === "GET")
      return json(route, {
        data: [
          {
            ...invoiceFixture,
            status: invoiceStatus,
            paidMinor: invoicePaidMinor,
          },
        ],
      });
    if (path === "/api/v1/invoices" && method === "POST")
      return json(
        route,
        {
          ...invoiceResultFixture,
          invoice: {
            ...invoiceFixture,
            id: "inv_draft",
            invoiceNumber: "DRAFT-202608-001",
            status: "DRAFT",
          },
        },
        201,
      );
    if (path === `/api/v1/invoices/${invoiceFixture.id}`)
      return json(route, {
        ...invoiceResultFixture,
        invoice: {
          ...invoiceFixture,
          status: invoiceStatus,
          paidMinor: invoicePaidMinor,
        },
      });
    if (path === `/api/v1/invoices/${invoiceFixture.id}/issue`) {
      invoiceStatus = "ISSUED";
      return json(route, { ...invoiceFixture, status: invoiceStatus });
    }
    if (path === `/api/v1/invoices/${invoiceFixture.id}/payments`) {
      invoiceStatus = "PAID";
      invoicePaidMinor = invoiceFixture.totalMinor;
      return json(route, {
        payment: { reference: "PAY-01" },
        invoice: {
          ...invoiceFixture,
          status: invoiceStatus,
          paidMinor: invoicePaidMinor,
        },
      });
    }
    if (path === "/api/v1/credit-notes" && method === "POST")
      return json(
        route,
        {
          id: "crn_01",
          noteNumber: "DRAFT-CRN-01",
          noteType: "CREDIT",
          invoiceNumber: invoiceFixture.invoiceNumber,
          customerCode: "ACME",
          reasonCode: "BILLING_ERROR",
          reason: "Duplicate surcharge on source shipment",
          issueDate: "2026-08-08",
          currency: "NGN",
          subtotalMinor: 1000,
          taxMinor: 180,
          totalMinor: 1180,
          status: "DRAFT",
          createdBy: 101,
        },
        201,
      );
    if (path === "/api/v1/credit-notes/crn_01/issue")
      return json(route, {
        id: "crn_01",
        noteNumber: "CRN/2026-27/000003",
        noteType: "CREDIT",
        invoiceNumber: invoiceFixture.invoiceNumber,
        customerCode: "ACME",
        reasonCode: "BILLING_ERROR",
        reason: "Duplicate surcharge on source shipment",
        issueDate: "2026-08-08",
        currency: "NGN",
        subtotalMinor: 1000,
        taxMinor: 180,
        totalMinor: 1180,
        status: "ISSUED",
        createdBy: 101,
        approvedBy: 202,
      });
    if (path === "/api/v1/geography/countries")
      return json(route, {
        data: [
          {
            id: "country-ng",
            iso2: "NG",
            iso3: "NGA",
            name: "Nigeria",
            currency: "NGN",
            phoneCode: "+234",
          },
          {
            id: "country-gb",
            iso2: "GB",
            iso3: "GBR",
            name: "United Kingdom",
            currency: "GBP",
            phoneCode: "+44",
          },
        ],
      });
    if (path === "/api/v1/shipments/preview")
      return json(route, {
        quote: {
          currency: "NGN",
          totalMinor: 10488,
          lineItems: shipment.charges.lineItems,
        },
        serviceability: {
          serviceable: true,
          serviceName: "Express Air",
          origin: { countryCode: "NG", pincode: "100001" },
          destination: { countryCode: "NG", pincode: "900001" },
        },
        declaredValueMinor: 0,
        commercial: {
          insurance: { status: "NOT_REQUESTED" },
          billing: {
            transportation: { party: "SHIPPER" },
            dutyTax: { party: "RECEIVER" },
          },
        },
      });
    if (path === "/api/v1/shipments" && method === "GET")
      return json(route, {
        data: [
          {
            id: shipment.id,
            awb: shipment.awb,
            referenceNumber: shipment.referenceNumber,
            status: shipment.status,
            paymentMode: shipment.paymentMode,
            pieceCount: 1,
            chargeableWeightGrams: 1000,
            currency: "NGN",
            totalAmountMinor: 10488,
            originPincode: "100001",
            destinationPincode: "900001",
            bookedAt: shipment.bookedAt,
            customer: shipment.customer,
            service: shipment.service,
            recipientCity: "Abuja",
          },
        ],
        pagination: { limit: 25, hasMore: false },
      });
    if (path === "/api/v1/shipments" && method === "POST") {
      shipmentPosts += 1;
      if (options?.bookingDelayMs)
        await new Promise((resolve) =>
          setTimeout(resolve, options.bookingDelayMs),
        );
      return json(route, shipment, 201);
    }
    if (path === `/api/v1/shipments/${shipment.id}`)
      return json(route, shipment);
    if (path.endsWith("/events"))
      return json(route, {
        data: [
          {
            id: "evt_01",
            eventType: "BOOKED",
            toStatus: "BOOKED",
            occurredAt: shipment.bookedAt,
            description: "Shipment booked successfully.",
            actorName: "Kemi Adeyemi",
            actorType: "USER",
            operatingUnitCode: "LOS01",
          },
        ],
      });
    if (path.endsWith("/cancel"))
      return json(route, {
        ...shipment,
        status: "CANCELLED",
        allowedTransitions: [],
        cancellationReason: "Customer requested cancellation",
      });
    if (path.endsWith("/label"))
      return json(route, {
        awb: shipment.awb,
        shipmentId: shipment.id,
        barcodePayload: shipment.awb,
        barcodeFormat: "CODE128",
        qrPayload: `CSV1|${shipment.awb}|EXPRESS|ABV01|900001|1|1000|PREPAID|0`,
        carrierCode: "CSV",
        serviceCode: "EXPRESS",
        serviceName: "Express Air",
        serviceMode: "AIR",
        routingCode: "ABVH-ABV01",
        originBranchCode: "LOS01",
        originHubCode: "LOSH",
        destinationHubCode: "ABVH",
        destinationBranchCode: "ABV01",
        recipient: {
          name: "Amina Bello",
          phone: "08037654321",
          line1: "8 Janpath",
          city: "Abuja",
          state: "Federal Capital Territory",
          pincode: "900001",
        },
        pieceCount: 1,
        weightLabel: "1 kg",
        paymentMode: "PREPAID",
      });
    if (path === "/api/v1/network/operating-units" && method === "POST")
      return json(
        route,
        {
          ...facility,
          id: "ou_created_01",
          code: "KAN-HUB",
          name: "Kano Regional Hub",
          status: "ACTIVE",
          version: 1,
        },
        201,
      );
    if (path === "/api/v1/network/operating-units")
      return json(route, {
        data: [facility, destinationFacility],
        pagination: { limit: 100, hasMore: false },
      });
    if (path === "/api/v1/trips" && method === "GET")
      return json(route, {
        data: [{ ...tripFixture, status: tripStatus }],
        pagination: { limit: 25, hasMore: false },
      });
    if (path === `/api/v1/trips/${tripFixture.id}/depart`) {
      tripStatus = "IN_TRANSIT";
      return json(route, {
        ...tripFixture,
        status: tripStatus,
        allowedTransitions: ["ARRIVED"],
      });
    }
    if (path === `/api/v1/trips/${tripFixture.id}/arrive`) {
      tripStatus = "ARRIVED";
      return json(route, {
        ...tripFixture,
        status: tripStatus,
        allowedTransitions: ["CLOSED"],
      });
    }
    if (path === `/api/v1/trips/${tripFixture.id}`)
      return json(route, {
        ...tripFixture,
        status: tripStatus,
        allowedTransitions:
          tripStatus === "PLANNED"
            ? ["DEPARTED", "CANCELLED"]
            : tripStatus === "IN_TRANSIT"
              ? ["ARRIVED"]
              : tripStatus === "ARRIVED"
                ? ["CLOSED"]
                : [],
      });
    if (path === "/api/v1/pickups" && method === "GET")
      return json(route, {
        data: [
          {
            ...pickupFixture,
            status: pickupStatus,
            agentId: pickupAgent?.id,
            agent: pickupAgent,
          },
        ],
        pagination: { limit: 25, hasMore: false },
      });
    if (path === "/api/v1/pickups" && method === "POST")
      return json(route, pickupFixture, 201);
    if (path === "/api/v1/pickups/my-stops")
      return json(route, {
        data: [
          {
            assignmentId: "pua_01",
            requestId: pickupFixture.id,
            referenceCode: pickupFixture.referenceCode,
            status: pickupStatus,
            stopSequence: 1,
            customerName: "Acme Retail",
            address: {
              contactName: "Chiamaka Okafor",
              phone: "08031234567",
              line1: "12 MG Road",
              city: "Lagos",
              state: "Lagos",
              pincode: "100001",
              latitude: 12.9756,
              longitude: 77.6066,
            },
            expectedPieceCount: 3,
            specialInstructions: "Call at the gate",
            window: pickupFixture.window,
          },
        ],
      });
    if (path === `/api/v1/pickups/${pickupFixture.id}/assign`) {
      pickupStatus = "ASSIGNED";
      pickupAgent = { id: "usr_agent", code: "AG001", name: "Tunde Balogun" };
      return json(route, {
        ...pickupFixture,
        status: pickupStatus,
        agentId: pickupAgent.id,
        agent: pickupAgent,
        allowedTransitions: ["ARRIVED", "COMPLETED"],
      });
    }
    if (path.endsWith("/respond") || path.endsWith("/arrive")) {
      pickupStatus = path.endsWith("/arrive") ? "ARRIVED" : "ACCEPTED";
      return json(route, { status: pickupStatus });
    }
    if (path === `/api/v1/pickups/${pickupFixture.id}/complete`) {
      pickupStatus = "COMPLETED";
      return json(route, {
        ...pickupFixture,
        status: pickupStatus,
        actualPieceCount: 3,
        allowedTransitions: [],
      });
    }
    if (path === `/api/v1/pickups/${pickupFixture.id}`)
      return json(route, {
        ...pickupFixture,
        status: pickupStatus,
        agentId: pickupAgent?.id,
        agent: pickupAgent,
        shipments: [],
        visits: [],
      });
    if (path === "/api/v1/pickup-runs" && method === "GET")
      return json(route, {
        data: [],
        pagination: { limit: 25, hasMore: false },
      });
    if (path === "/api/v1/scans" && method === "GET")
      return json(route, {
        data: [],
        pagination: { limit: 25, hasMore: false },
      });
    if (path === "/api/v1/scans" && method === "POST") {
      const rejected = request.postData()?.includes("BAD") ?? false;
      return json(route, {
        scanId: rejected ? "scn_bad" : "scn_ok",
        barcode: rejected ? "BAD-0001" : shipment.awb,
        awb: rejected ? undefined : shipment.awb,
        scanType: "RECEIVE",
        outcome: rejected ? "REJECTED" : "ACCEPTED",
        fromStatus: rejected ? undefined : "BOOKED",
        toStatus: rejected ? undefined : "AT_ORIGIN_BRANCH",
        nextAction: rejected
          ? undefined
          : "Sort to Federal Capital Territory hub",
        rejectionCode: rejected ? "BARCODE_NOT_FOUND" : undefined,
        rejectionMessage: rejected ? "Barcode was not found" : undefined,
        occurredAt: "2026-08-08T10:30:00+05:30",
      });
    }
    if (path === "/api/v1/bags" && method === "GET")
      return json(route, {
        data: [{ ...bagFixture, status: bagStatus }],
        pagination: { limit: 25, hasMore: false },
      });
    if (path === "/api/v1/bags" && method === "POST") {
      bagStatus = "OPEN";
      return json(route, bagFixture, 201);
    }
    if (path === `/api/v1/bags/${bagFixture.id}/items`)
      return json(route, {
        bagId: bagFixture.id,
        bagCode: bagFixture.bagCode,
        results: [
          { barcode: shipment.awb, awb: shipment.awb, outcome: "ADDED" },
        ],
        acceptedCount: 1,
        rejectedCount: 0,
      });
    if (path === `/api/v1/bags/${bagFixture.id}/close`) {
      bagStatus = "CLOSED";
      return json(route, {
        ...bagFixture,
        status: bagStatus,
        allowedTransitions: ["DISPATCHED"],
        closureDeclaration: {
          shipmentCount: 1,
          pieceCount: 1,
          totalWeightGrams: 1000,
        },
      });
    }
    if (path === `/api/v1/bags/${bagFixture.id}`)
      return json(route, {
        ...bagFixture,
        status: bagStatus,
        allowedTransitions: bagStatus === "OPEN" ? ["CLOSED"] : ["DISPATCHED"],
      });
    if (path === "/api/v1/manifests" && method === "GET")
      return json(route, {
        data: [{ ...manifestFixture, status: manifestStatus }],
        pagination: { limit: 25, hasMore: false },
      });
    if (path === "/api/v1/manifests" && method === "POST") {
      manifestStatus = "DRAFT";
      return json(route, manifestFixture, 201);
    }
    if (path === `/api/v1/manifests/${manifestFixture.id}/contents`)
      return json(route, {
        manifestId: manifestFixture.id,
        manifestCode: manifestFixture.manifestCode,
        results: [
          {
            reference: bagFixture.bagCode,
            referenceType: "BAG",
            outcome: "ADDED",
          },
        ],
        acceptedCount: 1,
        rejectedCount: 0,
      });
    if (path === `/api/v1/manifests/${manifestFixture.id}/close`) {
      manifestStatus = "CLOSED";
      return json(route, {
        ...manifestFixture,
        status: manifestStatus,
        allowedTransitions: ["DISPATCHED"],
      });
    }
    if (path === `/api/v1/manifests/${manifestFixture.id}/receive`) {
      manifestStatus = "RECEIVED";
      return json(route, {
        ...manifestFixture,
        status: manifestStatus,
        allowedTransitions: [],
      });
    }
    if (path === `/api/v1/manifests/${manifestFixture.id}`)
      return json(route, {
        ...manifestFixture,
        status: manifestStatus,
        allowedTransitions:
          manifestStatus === "DRAFT"
            ? ["CLOSED"]
            : manifestStatus === "CLOSED"
              ? ["DISPATCHED", "RECEIVED"]
              : [],
      });
    if (path === "/api/v1/reconciliations" && method === "GET")
      return json(route, {
        data: [
          {
            ...reconciliationFixture,
            status: reconciliationStatus,
            scannedCount: reconciliationScanned ? 1 : 0,
            matchedCount: reconciliationScanned ? 1 : 0,
            missingCount: reconciliationScanned ? 0 : 1,
            facilityCode: destinationFacility.code,
            manifestCode: manifestFixture.manifestCode,
          },
        ],
        pagination: { limit: 25, hasMore: false },
      });
    if (path === `/api/v1/reconciliations/${reconciliationFixture.id}/scan`) {
      reconciliationScanned = true;
      return json(route, {
        reconciliationId: reconciliationFixture.id,
        results: [
          { barcode: shipment.awb, awb: shipment.awb, outcome: "MATCHED" },
        ],
      });
    }
    if (
      path === `/api/v1/reconciliations/${reconciliationFixture.id}/complete`
    ) {
      reconciliationStatus = "COMPLETED";
      return json(route, {
        ...reconciliationFixture,
        status: reconciliationStatus,
        scannedCount: 1,
        matchedCount: 1,
        missingCount: 0,
        allowedTransitions: [],
      });
    }
    if (path === `/api/v1/reconciliations/${reconciliationFixture.id}`)
      return json(route, {
        ...reconciliationFixture,
        status: reconciliationStatus,
        scannedCount: reconciliationScanned ? 1 : 0,
        matchedCount: reconciliationScanned ? 1 : 0,
        missingCount: reconciliationScanned ? 0 : 1,
        items: reconciliationFixture.items.map((item) => ({
          ...item,
          itemType: "SHIPMENT",
          wasDeclared: true,
          scanned: reconciliationScanned,
          outcome: reconciliationScanned ? "MATCHED" : "MISSING",
        })),
      });
    if (path === "/api/v1/hub/summary")
      return json(route, {
        inboundManifests: 2,
        inboundTrips: 1,
        shipmentsInCustody: 84,
        openBags: 6,
        inboundBags: 12,
        readyForDelivery: 35,
        outForDelivery: 28,
        ndrPending: 4,
        openExceptions: 2,
        onHold: 3,
      });
    if (path === "/api/v1/hub/inbound")
      return json(route, { manifests: [], trips: [] });
    if (path === "/api/v1/hub/custody")
      return json(route, {
        data: [],
        pagination: { limit: 25, hasMore: false },
      });
    if (path === "/api/v1/deliveries/queue")
      return json(route, {
        data: [
          {
            shipmentId: shipment.id,
            awb: shipment.awb,
            status: "DESTINATION_BRANCH_RECEIVED",
            recipientName: "Amina Bello",
            recipientPhone: "08037654321",
            line1: "8 Janpath",
            city: "Abuja",
            pincode: "900001",
            promisedDeliveryAt: shipment.promisedDeliveryAt,
            pieceCount: 1,
            chargeableWeightGrams: 1000,
            paymentMode: "PREPAID",
            currency: "NGN",
            isHeld: false,
            onActiveRun: false,
          },
        ],
      });
    if (path === "/api/v1/delivery-runs" && method === "GET")
      return json(route, {
        data: [{ ...deliveryRunFixture, status: deliveryStatus }],
        pagination: { limit: 25, hasMore: false },
      });
    if (path === "/api/v1/delivery-runs" && method === "POST") {
      deliveryStatus = "PLANNED";
      return json(route, deliveryRunFixture, 201);
    }
    if (path === `/api/v1/delivery-runs/${deliveryRunFixture.id}/dispatch`) {
      deliveryStatus = "DISPATCHED";
      return json(route, {
        ...deliveryRunFixture,
        status: deliveryStatus,
        allowedTransitions: [],
      });
    }
    if (path === `/api/v1/delivery-runs/${deliveryRunFixture.id}/stops`)
      return json(route, { acceptedCount: 1, rejectedCount: 0, results: [] });
    if (path === `/api/v1/delivery-runs/${deliveryRunFixture.id}`)
      return json(route, {
        ...deliveryRunFixture,
        status: deliveryStatus,
        allowedTransitions: deliveryStatus === "PLANNED" ? ["DISPATCHED"] : [],
      });
    if (path === "/api/v1/deliveries/otp")
      return json(route, {
        otp: "482913",
        sentToMasked: "******3211",
        expiresAt: "2026-08-08T14:10:00+05:30",
      });
    if (path === "/api/v1/deliveries/attempts")
      return json(route, {
        awb: shipment.awb,
        outcome: "DELIVERED",
        shipmentStatus: "DELIVERED",
        nextAction: "Capture proof of delivery.",
      });
    if (path === "/api/v1/ndr/reasons")
      return json(route, {
        data: [
          {
            id: "ndrr_01",
            code: "CUSTOMER_UNAVAILABLE",
            name: "Customer unavailable",
            isActive: true,
          },
        ],
      });
    if (path === "/api/v1/ndr" && method === "GET")
      return json(route, {
        data: [ndrFixture],
        pagination: { limit: 25, hasMore: false },
      });
    if (path === `/api/v1/ndr/${ndrFixture.id}/action`)
      return json(route, { ...ndrFixture, currentAction: "REATTEMPT" });
    if (path === `/api/v1/ndr/${ndrFixture.id}`) return json(route, ndrFixture);
    if (path === "/api/v1/rto" && method === "GET")
      return json(route, {
        data: [rtoFixture],
        pagination: { limit: 25, hasMore: false },
      });
    if (path === "/api/v1/rto" && method === "POST")
      return json(route, rtoFixture, 201);
    if (path === `/api/v1/rto/${rtoFixture.id}`) return json(route, rtoFixture);
    if (path === "/api/v1/pod" && method === "POST")
      return json(route, podFixture, 201);
    if (path === `/api/v1/pod/${podFixture.id}`) return json(route, podFixture);
    if (path === `/api/v1/track/${shipment.awb}`)
      return json(route, {
        awb: shipment.awb,
        milestone: "IN_TRANSIT",
        statusTitle: "On the way",
        statusDescription: "Your parcel is moving toward Abuja.",
        origin: "Lagos",
        destination: "Abuja",
        service: "Express Air",
        expectedDelivery: "9 August 2026",
        pieceCount: 1,
        lastUpdatedAt: "2026-08-08T11:30:00+05:30",
        isReturning: false,
        events: [
          {
            milestone: "BOOKED",
            title: "Shipment booked",
            description: "We received the shipment details.",
            location: "Lagos",
            occurredAt: "2026-08-08T10:00:00+05:30",
          },
          {
            milestone: "IN_TRANSIT",
            title: "On the way",
            description: "The parcel left the origin city.",
            location: "Lagos",
            occurredAt: "2026-08-08T11:30:00+05:30",
          },
        ],
      });
    if (path.startsWith("/api/v1/track/"))
      return json(
        route,
        {
          code: "TRACKING_NOT_FOUND",
          message: "Tracking information is unavailable",
        },
        404,
      );
    if (path === "/api/v1/courier-services")
      return json(route, {
        data: [
          {
            id: "svc_test_01",
            code: "EXPRESS",
            name: "Express Air",
            mode: "AIR",
            status: "ACTIVE",
            slaTransitHours: 30,
          },
        ],
        pagination: { page: 1, totalPages: 1, totalItems: 1 },
      });
    if (path === "/api/v1/customers" && method === "GET")
      return json(route, {
        data: [
          {
            id: "cus_test_01",
            code: "ACME",
            name: "Acme Retail",
            customerType: "RETAIL",
            status: "ACTIVE",
            version: 1,
          },
        ],
        pagination: { page: 1, totalPages: 1, totalItems: 1 },
      });
    if (path === "/api/v1/customers" && method === "POST")
      return json(
        route,
        {
          id: "cus_new",
          code: "CUS-002",
          name: "New Customer",
          customerType: "RETAIL",
          status: "ACTIVE",
          version: 1,
        },
        201,
      );
    if (path.includes("/customers/") && path.endsWith("/addresses"))
      return json(route, { data: [] });
    if (
      path === "/api/v1/serviceability/check" ||
      path === "/api/v1/serviceability/debug"
    )
      return json(route, {
        serviceable: true,
        serviceCode: "EXPRESS",
        serviceName: "Express Air",
        serviceMode: "AIR",
        origin: {
          pincode: "100001",
          city: "Lagos",
          state: "Lagos",
          zoneCode: "SOUTH",
          isRemote: false,
        },
        destination: {
          pincode: "900001",
          city: "Abuja",
          state: "Federal Capital Territory",
          zoneCode: "NORTH",
          isRemote: false,
        },
        originBranch: {
          code: "LOS01",
          name: "Lagos Branch",
          unitType: "COMPANY_BRANCH",
        },
        originHub: {
          code: "LOSH",
          name: "Lagos Hub",
          unitType: "REGIONAL_HUB",
        },
        destinationHub: {
          code: "ABVH",
          name: "Federal Capital Territory Hub",
          unitType: "REGIONAL_HUB",
        },
        destinationBranch: {
          code: "ABV01",
          name: "Federal Capital Territory Branch",
          unitType: "COMPANY_BRANCH",
        },
        routeCode: "LOS-ABV-AIR",
        resolutionSource: "SERVICE_ROUTE",
        legs: [
          {
            sequence: 1,
            from: "LOSH",
            fromName: "Lagos Hub",
            to: "ABVH",
            toName: "Federal Capital Territory Hub",
            mode: "AIR",
            transitHours: 3,
          },
        ],
        transitHours: 24,
        slaHours: 30,
        promisedDeliveryAt: "2026-08-09T16:00:00+05:30",
        cutoffApplied: false,
        restrictions: [],
      });
    if (path === "/api/v1/pricing/quote")
      return json(route, {
        currency: "NGN",
        rateCardCode: "RETAIL",
        rateCardVersion: 1,
        originZoneCode: "SOUTH",
        destinationZoneCode: "NORTH",
        serviceCode: "EXPRESS",
        weight: {
          actualWeightGrams: 500,
          volumetricWeightGrams: 600,
          chargeableWeightGrams: 1000,
          explanation:
            "Volumetric basis selected and rounded to the nearest 500g.",
        },
        totalMinor: 10488,
        lineItems: shipment.charges.lineItems,
      });
    if (path === "/api/v1/users")
      return json(route, {
        data: [
          {
            id: "usr_01",
            fullName: "Kemi Adeyemi",
            email: "operator@ceserve.test",
            status: "ACTIVE",
            roles: [
              { roleCode: "OPERATIONS_ADMIN", roleName: "Operations Admin" },
            ],
          },
        ],
        pagination: { page: 1, totalPages: 1, totalItems: 1 },
      });
    return json(route, {
      data: [],
      pagination: { page: 1, totalPages: 1, totalItems: 0 },
    });
  });
  return {
    get shipmentPosts() {
      return shipmentPosts;
    },
  };
}

async function json(route: Route, body: unknown, status = 200) {
  await route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
    headers: { "X-Request-Id": "req_e2e" },
  });
}
