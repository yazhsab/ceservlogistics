// Release 5 production gate: the whole system under one realistic mix.
//
// The three earlier profiles each load one release in isolation. This one runs
// all fourteen scenarios the production gate names, simultaneously, because
// that is the only way to find the contention that isolated runs hide: a
// settlement read holding a connection while a scanner burst arrives, a
// dashboard aggregate competing with booking writes for the same pool.
//
// The arrival rates are a *shape*, not a benchmark. They are proportioned like
// a real courier day — tracking dwarfs everything, scanning is the busiest
// write, finance is low-rate and expensive — and sized for the 4 vCPU / 16 GB
// VPS in Constitution §2. Turning the numbers up until something breaks tells
// you where the ceiling is; that is what SATURATE=1 is for.
//
// Run:
//   k6 run -e BASE_URL=http://localhost:8080 \
//          -e EMAIL=admin@demo.test -e PASSWORD='...' \
//          -e UNIT_ID=ou_... -e CUSTOMER_ID=cus_... \
//          tests/load/courier-production-gate.js
//
// Optional:
//   -e SATURATE=1   ramping arrival rate, to find the knee
//   -e DURATION=5m

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';

const BASE = __ENV.BASE_URL || 'http://localhost:8080';
const EMAIL = __ENV.EMAIL;
const PASSWORD = __ENV.PASSWORD;
const UNIT_ID = __ENV.UNIT_ID || '';
const CUSTOMER_ID = __ENV.CUSTOMER_ID || '';
const DURATION = __ENV.DURATION || '3m';
const SATURATE = __ENV.SATURATE === '1';

// Business-level metrics. HTTP percentiles say the server responded; these say
// whether the operation the user wanted actually happened.
const bookings = new Counter('bookings_created');
const bookingLatency = new Trend('booking_latency', true);
const quoteLatency = new Trend('pricing_quote_latency', true);
const servLatency = new Trend('serviceability_latency', true);
const trackLatency = new Trend('tracking_latency', true);
const scanLatency = new Trend('scan_latency', true);
const bagLatency = new Trend('bag_ops_latency', true);
const manifestLatency = new Trend('manifest_latency', true);
const deliveryLatency = new Trend('delivery_latency', true);
const codLatency = new Trend('cod_latency', true);
const settlementLatency = new Trend('settlement_read_latency', true);
const hubLatency = new Trend('hub_dashboard_latency', true);
const franchiseLatency = new Trend('franchise_dashboard_latency', true);
const customerLatency = new Trend('customer_dashboard_latency', true);
const loginLatency = new Trend('login_latency', true);
const businessErrors = new Rate('business_errors');

function rate(r, execName, vus, maxVus) {
  if (SATURATE) {
    return {
      executor: 'ramping-arrival-rate',
      startRate: r, timeUnit: '1s',
      preAllocatedVUs: vus, maxVUs: maxVus * 4,
      stages: [
        { target: r, duration: '1m' },
        { target: r * 3, duration: '2m' },
        { target: r * 6, duration: '2m' },
      ],
      exec: execName,
    };
  }
  return {
    executor: 'constant-arrival-rate',
    rate: r, timeUnit: '1s', duration: DURATION,
    preAllocatedVUs: vus, maxVUs: maxVus,
    exec: execName,
  };
}

export const options = {
  scenarios: {
    // Highest volume by a wide margin, and unauthenticated: consignees
    // refreshing a tracking page.
    tracking: rate(30, 'trackingFlow', 10, 40),
    // The busiest write in any courier network.
    scanner: rate(15, 'scannerFlow', 8, 30),
    // Pre-booking lookups. Two calls per booking is typical.
    lookup: rate(10, 'lookupFlow', 6, 20),
    booking: rate(5, 'bookingFlow', 5, 20),
    lastMile: rate(4, 'deliveryFlow', 4, 15),
    bagging: rate(3, 'baggingFlow', 3, 12),
    manifest: rate(2, 'manifestFlow', 3, 10),
    hubDash: rate(2, 'hubDashboardFlow', 3, 10),
    franchiseDash: rate(1, 'franchiseDashboardFlow', 2, 8),
    customerDash: rate(2, 'customerDashboardFlow', 3, 10),
    // Low rate, expensive queries. This is the one that hurts the pool.
    finance: rate(1, 'financeFlow', 2, 8),
    logins: rate(1, 'loginFlow', 2, 8),
  },
  thresholds: {
    // Sized for the target host, not for a benchmark machine.
    http_req_failed: ['rate<0.01'],
    'http_req_duration{expected_response:true}': ['p(95)<800', 'p(99)<2000'],
    business_errors: ['rate<0.02'],

    booking_latency: ['p(95)<1200'],
    pricing_quote_latency: ['p(95)<500'],
    serviceability_latency: ['p(95)<400'],
    tracking_latency: ['p(95)<300'],
    scan_latency: ['p(95)<500'],
    hub_dashboard_latency: ['p(95)<1500'],
    settlement_read_latency: ['p(95)<2000'],
  },
};

function login(email, password) {
  const res = http.post(`${BASE}/api/v1/auth/login`,
    JSON.stringify({ email, password }),
    { headers: { 'Content-Type': 'application/json' }, tags: { name: 'login' } });
  if (res.status !== 200) return null;
  return res.json('tokens.accessToken');
}

export function setup() {
  const token = login(EMAIL, PASSWORD);
  if (!token) throw new Error('setup login failed — check EMAIL and PASSWORD');

  const h = { headers: { Authorization: `Bearer ${token}` } };

  // Collect real AWBs to track and scan. A load test against invented
  // identifiers measures the 404 path, which is fast and meaningless.
  const list = http.get(`${BASE}/api/v1/shipments?limit=50`, h);
  const awbs = (list.json('data') || []).map((s) => s.awb).filter(Boolean);

  const units = http.get(`${BASE}/api/v1/operating-units?limit=10`, h);
  const unitIds = (units.json('data') || []).map((u) => u.id).filter(Boolean);

  const customers = http.get(`${BASE}/api/v1/customers?limit=5`, h);
  const customerIds = (customers.json('data') || []).map((c) => c.id).filter(Boolean);

  return {
    token,
    awbs,
    unitId: UNIT_ID || unitIds[0] || '',
    unitIds,
    customerId: CUSTOMER_ID || customerIds[0] || '',
  };
}

function authed(data, extra) {
  return {
    headers: Object.assign({
      Authorization: `Bearer ${data.token}`,
      'Content-Type': 'application/json',
    }, extra || {}),
  };
}

function pick(list) {
  if (!list || list.length === 0) return null;
  return list[Math.floor(Math.random() * list.length)];
}

// A tracking number is public. This is the only unauthenticated flow, and in
// production it is the majority of all traffic.
export function trackingFlow(data) {
  const awb = pick(data.awbs);
  if (!awb) return;
  const res = http.get(`${BASE}/api/v1/track/${awb}`, { tags: { name: 'tracking' } });
  trackLatency.add(res.timings.duration);
  businessErrors.add(res.status >= 500);
  check(res, { 'tracking resolves': (r) => r.status === 200 || r.status === 404 });
  sleep(0.2);
}

export function lookupFlow(data) {
  const s = http.get(
    `${BASE}/api/v1/serviceability/check?originPincode=100001&destinationPincode=900001&serviceCode=EXPRESS`,
    authed(data));
  servLatency.add(s.timings.duration);
  businessErrors.add(s.status >= 400);

  const q = http.post(`${BASE}/api/v1/pricing/quote`, JSON.stringify({
    originPincode: '100001', destinationPincode: '900001',
    serviceCode: 'EXPRESS', paymentMode: 'PREPAID',
    packages: [{ actualWeightGrams: 1000 }],
  }), authed(data));
  quoteLatency.add(q.timings.duration);
  businessErrors.add(q.status >= 400);
  check(q, { 'quote priced': (r) => r.status === 200 });
  sleep(0.3);
}

export function bookingFlow(data) {
  if (!data.customerId) return;
  // A fresh key per iteration: reusing one would exercise the idempotency
  // replay path, which is a different (and much cheaper) measurement.
  const key = `k6-${__VU}-${__ITER}-${Date.now()}`;
  const res = http.post(`${BASE}/api/v1/shipments`, JSON.stringify({
    customerId: data.customerId, serviceCode: 'EXPRESS', paymentMode: 'PREPAID',
    contentDescription: 'load test',
    sender: {
      contactName: 'Adebayo Okonkwo', phone: '08031234567', line1: '1 Marina',
      city: 'Lagos', state: 'Lagos', pincode: '100001',
    },
    recipient: {
      contactName: 'Chidinma Eze', phone: '08099887766', line1: '2 Wuse',
      city: 'Abuja', state: 'Federal Capital Territory', pincode: '900001',
    },
    packages: [{ actualWeightGrams: 1200 }],
  }), authed(data, { 'Idempotency-Key': key }));

  bookingLatency.add(res.timings.duration);
  if (res.status === 201) bookings.add(1);
  businessErrors.add(res.status >= 400);
  check(res, { 'booked': (r) => r.status === 201 });
  sleep(0.5);
}

export function scannerFlow(data) {
  const awb = pick(data.awbs);
  if (!awb || !data.unitId) return;
  // Only fields the contract declares: the API rejects unknown fields as a
  // mass-assignment control, so an invented one is a 422 rather than a scan.
  // operatingUnitId goes in the body, not the header: an admin whose role
  // covers the whole network has no unit to infer, and the API says so rather
  // than guessing.
  const res = http.post(`${BASE}/api/v1/scans`, JSON.stringify({
    barcode: awb, scanType: 'RECEIVE', operatingUnitId: data.unitId,
  }), authed(data));
  scanLatency.add(res.timings.duration);
  // A rejected scan is a *correct* outcome here — the shipment may not be in a
  // state that accepts it. Only 5xx is a failure.
  businessErrors.add(res.status >= 500);
  sleep(0.2);
}

export function baggingFlow(data) {
  if (!data.unitId) return;
  const res = http.get(`${BASE}/api/v1/bags?limit=20`, authed(data, { 'X-Operating-Unit': data.unitId }));
  bagLatency.add(res.timings.duration);
  businessErrors.add(res.status >= 400);
  sleep(0.4);
}

export function manifestFlow(data) {
  if (!data.unitId) return;
  const res = http.get(`${BASE}/api/v1/manifests?limit=20`, authed(data, { 'X-Operating-Unit': data.unitId }));
  manifestLatency.add(res.timings.duration);
  businessErrors.add(res.status >= 400);
  sleep(0.5);
}

export function deliveryFlow(data) {
  if (!data.unitId) return;
  const runs = http.get(`${BASE}/api/v1/delivery-runs?limit=10`,
    authed(data, { 'X-Operating-Unit': data.unitId }));
  deliveryLatency.add(runs.timings.duration);
  businessErrors.add(runs.status >= 400);
  sleep(0.4);
}

// Finance reads are low-rate and expensive: aggregates over ledger entries and
// settlement lines. They are in the mix because their cost lands on the same
// bounded pool everything else uses.
export function financeFlow(data) {
  const s = http.get(`${BASE}/api/v1/settlements?limit=20`, authed(data));
  settlementLatency.add(s.timings.duration);
  businessErrors.add(s.status >= 400);

  const c = http.get(`${BASE}/api/v1/cod/obligations?limit=20`, authed(data));
  codLatency.add(c.timings.duration);
  businessErrors.add(c.status >= 400);
  sleep(1);
}

export function hubDashboardFlow(data) {
  const res = http.get(`${BASE}/api/v1/command-centre`, authed(data));
  hubLatency.add(res.timings.duration);
  businessErrors.add(res.status >= 400);
  sleep(1);
}

// The franchise portal is gated on the portal.franchise permission, which an
// operations admin does not hold — correctly. Run this flow with a token for a
// FRANCHISE_OWNER to measure it properly; with an admin token a 403 is the
// expected answer and is not an error.
export function franchiseDashboardFlow(data) {
  const res = http.get(`${BASE}/api/v1/portal/franchise/summary`, authed(data));
  franchiseLatency.add(res.timings.duration);
  businessErrors.add(res.status >= 500);
  check(res, { 'franchise portal answers': (r) => r.status === 200 || r.status === 403 });
  sleep(1);
}

export function customerDashboardFlow(data) {
  const res = http.get(`${BASE}/api/v1/shipments?limit=20`, authed(data));
  customerLatency.add(res.timings.duration);
  businessErrors.add(res.status >= 400);
  sleep(0.8);
}

// Login is deliberately low-rate: Argon2id is meant to be expensive, and a
// realistic mix has far more requests than sign-ins. Running it hot would
// measure the hash, not the system.
export function loginFlow() {
  const start = Date.now();
  const token = login(EMAIL, PASSWORD);
  loginLatency.add(Date.now() - start);
  businessErrors.add(token === null);
  sleep(2);
}

export function handleSummary(data) {
  return {
    stdout: textSummary(data),
    'load-summary.json': JSON.stringify(data, null, 2),
  };
}

function textSummary(data) {
  const m = data.metrics;
  const g = (name, stat) => (m[name] && m[name].values[stat] !== undefined
    ? m[name].values[stat].toFixed(1) : 'n/a');

  const lines = [
    '',
    '  Production gate — mixed workload',
    '  ' + '='.repeat(58),
    `  requests            ${m.http_reqs ? m.http_reqs.values.count : 0}`,
    `  failed              ${m.http_req_failed ? (m.http_req_failed.values.rate * 100).toFixed(2) : 'n/a'}%`,
    `  bookings created    ${m.bookings_created ? m.bookings_created.values.count : 0}`,
    '',
    '  latency ms          p50      p95      p99',
    `  overall           ${g('http_req_duration', 'med').padStart(6)}   ${g('http_req_duration', 'p(95)').padStart(6)}   ${g('http_req_duration', 'p(99)').padStart(6)}`,
    '',
    '  per flow            p95',
  ];
  for (const [label, metric] of [
    ['tracking', 'tracking_latency'],
    ['serviceability', 'serviceability_latency'],
    ['pricing quote', 'pricing_quote_latency'],
    ['booking', 'booking_latency'],
    ['scan', 'scan_latency'],
    ['bag ops', 'bag_ops_latency'],
    ['manifest', 'manifest_latency'],
    ['delivery', 'delivery_latency'],
    ['cod', 'cod_latency'],
    ['settlement read', 'settlement_read_latency'],
    ['hub dashboard', 'hub_dashboard_latency'],
    ['franchise dash', 'franchise_dashboard_latency'],
    ['customer dash', 'customer_dashboard_latency'],
    ['login', 'login_latency'],
  ]) {
    lines.push(`  ${label.padEnd(18)}${g(metric, 'p(95)').padStart(6)}`);
  }

  lines.push('');
  const failed = Object.entries(data.metrics)
    .flatMap(([name, metric]) => Object.entries(metric.thresholds || {})
      .filter(([, t]) => !t.ok).map(([expr]) => `${name}: ${expr}`));
  lines.push(failed.length === 0
    ? '  all thresholds passed'
    : `  THRESHOLDS FAILED:\n${failed.map((f) => `    ${f}`).join('\n')}`);
  lines.push('');
  return lines.join('\n');
}
