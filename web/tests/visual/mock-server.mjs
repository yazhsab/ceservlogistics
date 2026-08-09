import { createServer } from "node:http";

const permissions = [
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
  "manifest.read",
  "manifest.manage",
  "manifest.close",
  "manifest.dispatch",
  "manifest.receive",
  "carrier.read",
  "vehicle.read",
  "driver.read",
  "trip.read",
  "trip.manage",
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
  "notification.retry",
  "command.read",
  "report.read",
  "report.run",
  "report.finance",
  "portal.console",
  "apikey.read",
  "apikey.manage",
  "webhook.read",
  "webhook.manage",
  "webhook.replay",
];
const user = {
  id: "usr_visual",
  email: "visual@ceserve.test",
  fullName: "Kemi Adeyemi",
  status: "ACTIVE",
  mustChangePassword: false,
  organization: {
    id: "org_visual",
    code: "CSV",
    name: "Ceserve Logistics",
    timezone: "Africa/Lagos",
    currency: "NGN",
    awbPrefix: "CSV",
  },
  roles: ["OPERATIONS_ADMIN"],
  permissions,
  operatingUnitIds: [],
  hasOrganizationWideAccess: true,
};
const shipment = {
  id: "shp_visual",
  awb: "CSV260808000001",
  referenceNumber: "WEB-001",
  status: "BOOKED",
  paymentMode: "PREPAID",
  bookedAt: "2026-08-08T10:00:00+05:30",
  customer: { id: "cus_visual", code: "ACME", name: "Acme Retail" },
  service: { id: "svc_visual", code: "EXPRESS", name: "Express Air" },
  origin: {
    pincode: "100001",
    branch: { code: "LOS01", name: "Lagos Branch" },
    hub: { code: "LOSH", name: "Lagos Hub" },
  },
  destination: {
    pincode: "900001",
    branch: { code: "ABV01", name: "Federal Capital Territory Branch" },
    hub: { code: "ABVH", name: "Federal Capital Territory Hub" },
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
      id: "pkg_visual",
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
const quote = {
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
    explanation: "Volumetric basis selected and rounded to the nearest 500g.",
  },
  totalMinor: 10488,
  lineItems: shipment.charges.lineItems,
};
const serviceability = {
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
  originHub: { code: "LOSH", name: "Lagos Hub", unitType: "REGIONAL_HUB" },
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
  transitHours: 24,
  slaHours: 30,
  promisedDeliveryAt: "2026-08-09T16:00:00+05:30",
  cutoffApplied: false,
  restrictions: [],
};

const visualFacility = {
  id: "ou_lag_01",
  code: "LAG-IKJ",
  name: "Ikeja Branch",
  unitType: "COMPANY_BRANCH",
};

const visualRun = {
  id: "dr_visual",
  runCode: "DR-DEL-0042",
  status: "DISPATCHED",
  runDate: "2026-08-08",
  branch: visualFacility,
  agent: { id: "usr_agent", code: "AG042", name: "Tunde Balogun" },
  plannedStops: 8,
  completedStops: 3,
  deliveredCount: 3,
  failedCount: 0,
  codExpectedMinor: 249900,
  codCollectedMinor: 89900,
  currency: "NGN",
  allowedTransitions: [],
  stops: [
    {
      id: "dst_visual",
      stopSequence: 4,
      shipmentId: shipment.id,
      awb: shipment.awb,
      status: "OUT_FOR_DELIVERY",
      recipientName: "Amina Bello",
      recipientPhone: "08037654321",
      line1: "8 Janpath",
      landmark: "Opposite Central Park",
      city: "Abuja",
      state: "Federal Capital Territory",
      pincode: "900001",
      latitude: 28.6139,
      longitude: 77.209,
      pieceCount: 1,
      paymentMode: "COD",
      codAmountMinor: 160000,
      currency: "NGN",
      promisedDeliveryAt: "2026-08-08T18:00:00+05:30",
    },
  ],
};

const commissionRule = {
  id: "crl_visual",
  code: "DEL-GOLD",
  name: "Gold franchise delivery commission",
  commissionType: "DELIVERY",
  recipientRole: "DESTINATION_FRANCHISE",
  schemeCode: "STANDARD",
  franchiseCategory: "GOLD",
  specificity: 8,
  priority: 20,
  status: "ACTIVE",
};

const commissionCalculation = {
  id: "ccl_visual",
  commissionType: "DELIVERY",
  awb: shipment.awb,
  qualifyingEvent: "DELIVERED",
  qualifiedAt: "2026-08-08T13:45:00+05:30",
  recipientType: "FRANCHISE",
  franchiseCode: "FRN-LOS-01",
  ruleCode: commissionRule.code,
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

const journal = {
  id: "jrn_visual",
  transactionNumber: "JV-202608-000001",
  status: "POSTED",
  postingDate: "2026-08-08",
  currency: "NGN",
  totalMinor: 67850,
  entryCount: 3,
  sourceType: "INVOICE",
  sourceId: "inv_visual",
  purpose: "ISSUE",
  description: "Invoice INV/2026-27/000042",
  postedAt: "2026-08-08T10:00:00+05:30",
};

const journalEntries = [
  {
    id: "jen_visual_1",
    lineNo: 1,
    accountCode: "1200",
    accountName: "Trade Receivable",
    debitMinor: 67850,
    creditMinor: 0,
    currency: "NGN",
    memo: "Customer receivable",
  },
  {
    id: "jen_visual_2",
    lineNo: 2,
    accountCode: "4000",
    accountName: "Freight Revenue",
    debitMinor: 0,
    creditMinor: 57500,
    currency: "NGN",
    memo: "Freight revenue",
  },
  {
    id: "jen_visual_3",
    lineNo: 3,
    accountCode: "2300",
    accountName: "Tax Payable",
    debitMinor: 0,
    creditMinor: 10350,
    currency: "NGN",
    memo: "VAT payable",
  },
];

const codObligation = {
  id: "cod_visual",
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

const settlement = {
  id: "stl_visual",
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
  calculationHash: "sha256:finance-visual-fixture",
  calculatedBy: 101,
  approvedBy: null,
};

const settlementLines = [
  {
    id: "sln_visual_1",
    lineNo: 1,
    category: "DELIVERY_COMMISSION",
    description: "Delivery commission",
    amountMinor: 50000,
    currency: "NGN",
    quantity: 10,
    sourceType: "COMMISSION_CALCULATION",
    sourceId: 1,
    sourcePublicId: commissionCalculation.id,
    awb: shipment.awb,
  },
  {
    id: "sln_visual_2",
    lineNo: 2,
    category: "COD_LIABILITY",
    description: "COD held by franchise",
    amountMinor: -90000,
    currency: "NGN",
    quantity: 1,
    sourceType: "COD_OBLIGATION",
    sourceId: 1,
    sourcePublicId: codObligation.id,
    awb: shipment.awb,
  },
];

const invoice = {
  id: "inv_visual",
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

const invoiceResult = {
  invoice,
  lines: [
    {
      id: "ivl_visual",
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

const apiKey = {
  id: "apk_01JRELEASE4VISUALKEY00001",
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

const webhookEndpoint = {
  id: "whe_01JRELEASE4VISUALENDPOINT1",
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

const webhookDelivery = {
  id: "whd_01JRELEASE4VISUALDELIVERY1",
  endpoint: webhookEndpoint.id,
  eventType: "shipment.delivered",
  eventId: "evt_01JRELEASE4VISUALEVENT001",
  status: "FAILED",
  attemptCount: 2,
  lastStatusCode: 503,
  lastError: "Partner endpoint returned Service Unavailable",
  nextAttemptAt: "2026-08-09T10:15:00+05:30",
  deliveredAt: null,
  createdAt: "2026-08-09T10:00:00+05:30",
};

const notificationTemplate = {
  id: "ntt_visual_01",
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
};

const notificationDelivery = {
  id: "ntf_visual_01",
  eventType: "SHIPMENT_OUT_FOR_DELIVERY",
  channel: "SMS",
  status: "FAILED",
  recipientType: "RECIPIENT",
  recipientName: "Amina Bello",
  subject: null,
  attemptCount: 2,
  lastError: "Provider timeout",
  nextAttemptAt: null,
  awb: "CSV260809000101",
  reference: "LAG-ORDER-7781",
  createdAt: "2026-08-09T10:20:00+01:00",
};

const reportRun = {
  id: "rpt_visual_01",
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
};

createServer((request, response) => {
  const url = new URL(request.url ?? "/", "http://127.0.0.1");
  const path = url.pathname;
  let body = {
    data: [],
    pagination: { page: 1, totalPages: 1, totalItems: 0 },
  };
  let status = 200;
  if (path === "/api/v1/visual/customer") {
    Object.assign(user, {
      email: "customer@lagos.test",
      fullName: "Nneka Okafor",
      roles: ["CUSTOMER"],
      permissions: ["portal.customer"],
      portal: {
        isCustomerUser: true,
        customers: [
          {
            id: "cus_test_01",
            code: "LAGRETAIL",
            name: "Lagos Retail Limited",
          },
        ],
        franchise: null,
      },
      operatingUnitIds: [],
      hasOrganizationWideAccess: false,
    });
    body = { mode: "customer" };
  } else if (path === "/api/v1/auth/login")
    body = {
      tokens: {
        accessToken: "visual-access",
        refreshToken: "visual-refresh",
        tokenType: "Bearer",
        expiresIn: 900,
      },
      user,
    };
  else if (path === "/api/v1/auth/refresh")
    body = {
      accessToken: "visual-access-refreshed",
      refreshToken: "visual-refresh-refreshed",
      tokenType: "Bearer",
      expiresIn: 900,
    };
  else if (path === "/api/v1/auth/me") body = user;
  else if (path === "/api/v1/auth/sessions") body = { data: [] };
  else if (path === "/api/v1/portal/customer/summary")
    body = {
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
    };
  else if (path === "/api/v1/portal/customer/accounts")
    body = {
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
    };
  else if (path === "/api/v1/portal/customer/shipments")
    body = {
      data: [
        {
          id: "shp_portal_visual",
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
        },
      ],
    };
  else if (path === "/api/v1/portal/customer/invoices")
    body = {
      data: [
        {
          id: "inv_portal_visual",
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
    };
  else if (path === "/api/v1/command-centre") {
    const scoped = Boolean(url.searchParams.get("unitId"));
    body = {
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
    };
  } else if (path === "/api/v1/command-centre/trend")
    body = {
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
    };
  else if (path === "/api/v1/command-centre/units")
    body = {
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
    };
  else if (path === "/api/v1/command-centre/services")
    body = {
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
    };
  else if (path === "/api/v1/command-centre/backlog")
    body = {
      data: [
        {
          unitId: "ou_lag_01",
          code: "LAG-IKJ",
          name: "Ikeja Branch",
          count: 284,
        },
      ],
      consistency: "live; counted at request time",
    };
  else if (path === "/api/v1/command-centre/snapshots")
    body = {
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
    };
  else if (path === "/api/v1/notifications/templates")
    body = { data: [notificationTemplate] };
  else if (
    path === `/api/v1/notifications/templates/${notificationTemplate.id}`
  )
    body = notificationTemplate;
  else if (path === "/api/v1/notifications/channels")
    body = {
      data: [
        { channel: "SMS", configured: true },
        { channel: "EMAIL", configured: true },
        { channel: "WHATSAPP", configured: true },
        { channel: "PUSH", configured: false },
      ],
    };
  else if (path === "/api/v1/notifications/health")
    body = {
      windowHours: 24,
      byStatus: { DELIVERED: 842, FAILED: 12, SUPPRESSED: 31 },
      configuredChannels: ["SMS", "EMAIL", "WHATSAPP"],
    };
  else if (path === "/api/v1/notifications")
    body = { data: [notificationDelivery] };
  else if (path === `/api/v1/notifications/${notificationDelivery.id}`)
    body = {
      ...notificationDelivery,
      recipientAddress: "+234••••••4182",
      locale: "en-NG",
      templateCode: notificationTemplate.code,
      body: "Your shipment CSV260809000101 is out for delivery.",
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
    };
  else if (path === "/api/v1/reports/types")
    body = {
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
    };
  else if (path === "/api/v1/reports") body = { data: [reportRun] };
  else if (path === `/api/v1/reports/${reportRun.id}`) body = reportRun;
  else if (path === "/api/v1/console/summary")
    body = {
      unit: { id: "ou_lag_01", code: "LAG-IKJ", name: "Ikeja Branch" },
      inCustody: 284,
      outForDelivery: 92,
      exceptions: 8,
      held: 3,
      overdue: 5,
      openBags: 11,
      inboundExpected: 6,
    };
  else if (path === "/api/v1/console/inbound")
    body = {
      data: [
        {
          id: "man_visual_release4",
          manifestCode: "MAN-ABJ-LAG-0042",
          status: "DISPATCHED",
          bags: 5,
          shipments: 86,
          weightGrams: 128400,
          origin: "Abuja Hub",
          dispatchedAt: "2026-08-09T06:00:00+01:00",
        },
      ],
    };
  else if (path === "/api/v1/console/bags")
    body = {
      data: [
        {
          id: "bag_visual_release4",
          bagCode: "BAG-LAG-00981",
          status: "OPEN",
          shipments: 18,
          weightGrams: 28400,
          destination: "Abuja Hub",
        },
      ],
    };
  else if (path === "/api/v1/console/queue")
    body = {
      unit: { id: "ou_lag_01", code: "LAG-IKJ", name: "Ikeja Branch" },
      data: [
        {
          id: "shp_visual_release4",
          awb: "CSV260809000301",
          status: "IN_TRANSIT",
          pieces: 1,
          destinationPincode: "900001",
          destinationBranch: "Abuja Central",
          promisedBy: "2026-08-09T08:00:00+01:00",
          isHeld: true,
        },
      ],
    };
  else if (path === "/api/v1/api-keys" && request.method === "GET")
    body = { data: [apiKey] };
  else if (path === "/api/v1/api-keys" && request.method === "POST") {
    status = 201;
    body = {
      key: { ...apiKey, id: "apk_visual_new", name: "Returns platform" },
      token: "key_live_csv_new.one-time-secret-value",
      warning: "Store this token now. It is hashed and cannot be shown again.",
    };
  } else if (path === `/api/v1/api-keys/${apiKey.id}/usage`)
    body = {
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
    };
  else if (path === "/api/v1/webhooks/events") body = { data: webhookEvents };
  else if (path === "/api/v1/webhooks/endpoints" && request.method === "GET")
    body = { data: [webhookEndpoint] };
  else if (path === "/api/v1/webhooks/endpoints" && request.method === "POST") {
    status = 201;
    body = {
      endpoint: {
        ...webhookEndpoint,
        id: "whe_visual_new",
        name: "Returns event receiver",
      },
      signingSecret: "whsec_one_time_signing_secret",
      signatureHeader: {
        header: "Webhook-Signature",
        timestamp: "Webhook-Timestamp",
        scheme: 'v1=HMAC_SHA256(secret, "<timestamp>.<body>")',
        toleranceSec: 300,
      },
      warning: "Store this signing secret now; it cannot be recovered.",
    };
  } else if (
    path === `/api/v1/webhooks/endpoints/${webhookEndpoint.id}/subscriptions`
  )
    body = {
      data: [
        { id: "whs_visual", eventType: "shipment.delivered", isActive: true },
      ],
    };
  else if (path === "/api/v1/webhooks/deliveries")
    body = { data: [webhookDelivery] };
  else if (path === `/api/v1/webhooks/deliveries/${webhookDelivery.id}`)
    body = {
      ...webhookDelivery,
      url: webhookEndpoint.url,
      payload: {
        id: webhookDelivery.eventId,
        type: webhookDelivery.eventType,
        createdAt: webhookDelivery.createdAt,
        data: {
          shipmentId: shipment.id,
          awb: shipment.awb,
          status: "DELIVERED",
        },
      },
    };
  else if (
    path === `/api/v1/webhooks/deliveries/${webhookDelivery.id}/attempts`
  )
    body = {
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
    };
  else if (path === "/api/v1/commission/rules" && request.method === "GET")
    body = { data: [commissionRule], total: 1 };
  else if (path === "/api/v1/commission/simulate")
    body = {
      matched: true,
      ruleCode: commissionRule.code,
      ruleName: commissionRule.name,
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
        trace: commissionCalculation.calculationTrace,
      },
      candidates: [
        {
          code: "DEL-GOLD",
          name: commissionRule.name,
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
    };
  else if (path === "/api/v1/commission/calculations")
    body = { data: [commissionCalculation] };
  else if (
    path === `/api/v1/commission/calculations/${commissionCalculation.id}`
  )
    body = commissionCalculation;
  else if (path === `/api/v1/ledger/journals/${journal.id}`)
    body = { transaction: journal, entries: journalEntries };
  else if (path === "/api/v1/cod/summary")
    body = {
      expectedCount: 1,
      withAgentCount: 0,
      withBranchCount: 1,
      withFranchiseCount: 0,
      remittedCount: 0,
      expectedMinor: 200000,
      inCustodyMinor: 200000,
      remittedMinor: 0,
      currency: "NGN",
    };
  else if (path === "/api/v1/cod/obligations") body = { data: [codObligation] };
  else if (path === `/api/v1/cod/obligations/${codObligation.id}`)
    body = {
      obligation: codObligation,
      collections: [
        {
          id: "cdc_visual",
          amountMinor: 200000,
          currency: "NGN",
          paymentMode: "CASH",
          reference: "DELIVERY-CASH-01",
          collectedAt: "2026-08-08T13:45:00+05:30",
        },
      ],
    };
  else if (path === `/api/v1/settlements/${settlement.id}`)
    body = {
      settlement,
      lines: settlementLines,
      byCategory: [],
      approvals: [],
      payments: [],
    };
  else if (path === `/api/v1/invoices/${invoice.id}`) body = invoiceResult;
  else if (path === "/api/v1/shipments" && request.method === "GET")
    body = {
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
    };
  else if (path === "/api/v1/shipments" && request.method === "POST") {
    body = shipment;
    status = 201;
  } else if (path === "/api/v1/shipments/shp_visual") body = shipment;
  else if (path.endsWith("/events"))
    body = {
      data: [
        {
          id: "evt_visual",
          eventType: "BOOKED",
          toStatus: "BOOKED",
          occurredAt: shipment.bookedAt,
          description: "Shipment booked successfully.",
          actorName: "Kemi Adeyemi",
        },
      ],
    };
  else if (path.endsWith("/label"))
    body = {
      awb: shipment.awb,
      shipmentId: shipment.id,
      barcodePayload: shipment.awb,
      barcodeFormat: "CODE128",
      qrPayload: `CSV1|${shipment.awb}|EXPRESS|ABV01|900001|1|1000|PREPAID|0`,
      serviceCode: "EXPRESS",
      serviceMode: "AIR",
      routingCode: "ABVH-ABV01",
      originBranchCode: "LOS01",
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
    };
  else if (path === "/api/v1/courier-services")
    body = {
      data: [
        {
          id: "svc_visual",
          code: "EXPRESS",
          name: "Express Air",
          mode: "AIR",
          status: "ACTIVE",
          slaTransitHours: 30,
        },
      ],
      pagination: { page: 1, totalPages: 1, totalItems: 1 },
    };
  else if (path === "/api/v1/customers")
    body = {
      data: [
        {
          id: "cus_visual",
          code: "ACME",
          name: "Acme Retail",
          customerType: "RETAIL",
          status: "ACTIVE",
        },
      ],
      pagination: { page: 1, totalPages: 1, totalItems: 1 },
    };
  else if (path.endsWith("/addresses")) body = { data: [] };
  else if (
    path === "/api/v1/serviceability/check" ||
    path === "/api/v1/serviceability/debug"
  )
    body = serviceability;
  else if (path === "/api/v1/pricing/quote") body = quote;
  else if (path === "/api/v1/scans" && request.method === "GET")
    body = {
      data: [
        {
          id: "scn_visual_1",
          barcode: shipment.awb,
          awb: shipment.awb,
          scanType: "RECEIVE",
          outcome: "ACCEPTED",
          fromStatus: "BOOKED",
          toStatus: "AT_ORIGIN_BRANCH",
          occurredAt: "2026-08-08T10:30:00+05:30",
          facility: { code: "LOS01", name: "Lagos Branch" },
        },
      ],
      pagination: { limit: 25, hasMore: false },
    };
  else if (path === "/api/v1/scans" && request.method === "POST")
    body = {
      scanId: "scn_visual_2",
      barcode: shipment.awb,
      awb: shipment.awb,
      scanType: "RECEIVE",
      outcome: "ACCEPTED",
      fromStatus: "BOOKED",
      toStatus: "AT_ORIGIN_BRANCH",
      nextAction: "Sort to Federal Capital Territory hub",
      occurredAt: "2026-08-08T10:31:00+05:30",
    };
  else if (path === "/api/v1/hub/summary")
    body = {
      inboundManifests: 12,
      inboundTrips: 3,
      shipmentsInCustody: 1840,
      openBags: 46,
      inboundBags: 118,
      readyForDelivery: 635,
      outForDelivery: 428,
      ndrPending: 37,
      openExceptions: 9,
      onHold: 14,
      pendingPickups: 21,
    };
  else if (path === "/api/v1/hub/inbound")
    body = {
      manifests: [
        {
          id: "mnf_visual",
          manifestCode: "MNF-BLR-DEL-0218",
          status: "DISPATCHED",
          origin: { code: "LOS01", name: "Lagos Branch" },
          destination: visualFacility,
          bagCount: 18,
          totalShipmentCount: 486,
          totalPieceCount: 486,
          totalWeightGrams: 618000,
          dispatchedAt: "2026-08-08T08:10:00+05:30",
          expectedArrival: "2026-08-08T15:15:00+05:30",
          vehicleRegistration: "KA01AB2042",
        },
      ],
      trips: [
        {
          id: "trip_visual",
          tripCode: "TRP-BLR-DEL-0087",
          status: "IN_TRANSIT",
          origin: { code: "LOSH", name: "Lagos Hub" },
          destination: { code: "ABVH", name: "Federal Capital Territory Hub" },
          manifestCount: 4,
          vehicleRegistration: "KA01AB2042",
          driverName: "Emeka Nwosu",
          expectedArrival: "2026-08-08T15:30:00+05:30",
        },
      ],
    };
  else if (path === "/api/v1/delivery-runs" && request.method === "GET")
    body = { data: [visualRun], pagination: { limit: 25, hasMore: false } };
  else if (path === `/api/v1/delivery-runs/${visualRun.id}`) body = visualRun;
  else if (path === "/api/v1/ndr/reasons")
    body = {
      data: [
        {
          id: "ndrr_visual",
          code: "CUSTOMER_UNAVAILABLE",
          name: "Customer unavailable",
          isActive: true,
        },
      ],
    };
  else if (path === `/api/v1/track/${shipment.awb}`)
    body = {
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
    };
  response.writeHead(status, {
    "Content-Type": "application/json",
    "X-Request-Id": "req_visual",
  });
  response.end(JSON.stringify(body));
}).listen(8080, "127.0.0.1", () =>
  console.log("Visual API ready at http://127.0.0.1:8080"),
);
