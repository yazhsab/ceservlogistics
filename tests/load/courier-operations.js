// Release 2 operational load profile.
//
// The endpoints here are the ones a real courier network hammers: a scanner at
// a sorting hub, a supervisor's dashboard, and consignees refreshing public
// tracking. The mix is weighted to match that reality rather than to produce a
// flattering number — public tracking is the highest-volume endpoint in any
// courier system, and the scanner is the highest-volume *write*.
//
// Run:
//   k6 run -e BASE_URL=http://localhost:8080 \
//          -e EMAIL=admin@demo.test -e PASSWORD='...' \
//          -e UNIT_ID=ou_... -e AWBS=AWB1,AWB2,... \
//          tests/load/courier-operations.js
//
// Targets are sized for the 4 vCPU / 16 GB VPS in the Constitution §2, not for
// a benchmark machine.

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';

const BASE = __ENV.BASE_URL || 'http://localhost:8080';
const EMAIL = __ENV.EMAIL;
const PASSWORD = __ENV.PASSWORD;
const UNIT_ID = __ENV.UNIT_ID;
const AWBS = (__ENV.AWBS || '').split(',').filter(Boolean);

// Business-meaningful metrics, so a run says something about the operation
// rather than only about HTTP.
const scansAccepted = new Counter('scans_accepted');
const scansRejected = new Counter('scans_rejected');
const scanLatency = new Trend('scan_latency', true);
const trackLatency = new Trend('tracking_latency', true);
const dashboardLatency = new Trend('hub_dashboard_latency', true);
const bagScanLatency = new Trend('bag_scan_latency', true);
const deliveryLatency = new Trend('delivery_update_latency', true);
const errorRate = new Rate('business_errors');

export const options = {
  scenarios: {
    // A sorting hub during the evening peak: continuous scanning.
    scanner: {
      executor: 'constant-arrival-rate',
      rate: 20, timeUnit: '1s', duration: '2m',
      preAllocatedVUs: 10, maxVUs: 30,
      exec: 'scannerFlow',
    },
    // Consignees refreshing tracking. Deliberately the heaviest arrival rate:
    // it is unauthenticated, cached, and in production it dwarfs everything.
    publicTracking: {
      executor: 'constant-arrival-rate',
      rate: 40, timeUnit: '1s', duration: '2m',
      preAllocatedVUs: 15, maxVUs: 50,
      exec: 'trackingFlow',
    },
    // Supervisors watching the floor.
    dashboards: {
      executor: 'constant-arrival-rate',
      rate: 4, timeUnit: '1s', duration: '2m',
      preAllocatedVUs: 5, maxVUs: 15,
      exec: 'dashboardFlow',
    },
    // Bag building at the counter.
    bagging: {
      executor: 'constant-arrival-rate',
      rate: 3, timeUnit: '1s', duration: '2m',
      preAllocatedVUs: 5, maxVUs: 15,
      exec: 'baggingFlow',
    },
  },
  thresholds: {
    // The scanner is the one an operator feels: a handheld that takes half a
    // second per parcel makes a hub queue.
    'scan_latency': ['p(95)<250', 'p(99)<500'],
    // Tracking is cached, so it should be very fast.
    'tracking_latency': ['p(95)<150'],
    'hub_dashboard_latency': ['p(95)<400'],
    'bag_scan_latency': ['p(95)<400'],
    'delivery_update_latency': ['p(95)<500'],
    'http_req_failed': ['rate<0.01'],
    'business_errors': ['rate<0.02'],
  },
};

export function setup() {
  if (!EMAIL || !PASSWORD) {
    throw new Error('EMAIL and PASSWORD are required');
  }
  const res = http.post(`${BASE}/api/v1/auth/login`,
    JSON.stringify({ email: EMAIL, password: PASSWORD }),
    { headers: { 'Content-Type': 'application/json' } });
  if (res.status !== 200) {
    throw new Error(`login failed: ${res.status} ${res.body}`);
  }
  const token = res.json('tokens.accessToken');

  // Discover a working facility if one was not supplied.
  let unitId = UNIT_ID;
  if (!unitId) {
    const units = http.get(`${BASE}/api/v1/network/operating-units?limit=5`, {
      headers: { Authorization: `Bearer ${token}` },
    });
    if (units.status === 200) {
      const rows = units.json('data') || [];
      if (rows.length > 0) unitId = rows[0].id;
    }
  }
  if (!unitId) {
    throw new Error('no operating unit available; pass UNIT_ID');
  }

  // Collect AWBs to scan and track. Using real ones means the hot path is
  // measured with real index lookups rather than with misses.
  let awbs = AWBS;
  if (awbs.length === 0) {
    const list = http.get(`${BASE}/api/v1/shipments?limit=100`, {
      headers: { Authorization: `Bearer ${token}` },
    });
    awbs = (list.json('data') || []).map((s) => s.awb).filter(Boolean);
  }
  if (awbs.length === 0) {
    throw new Error('no shipments to exercise; book some first or pass AWBS');
  }
  return { token, unitId, awbs };
}

function authHeaders(data, extra) {
  return Object.assign({
    Authorization: `Bearer ${data.token}`,
    'Content-Type': 'application/json',
    'X-Client-Source': 'SCANNER',
  }, extra || {});
}

function pick(list) {
  return list[Math.floor(Math.random() * list.length)];
}

// scannerFlow is the hot write path: one barcode, one scan.
//
// Most scans in this profile will be rejected on state, because the seeded
// shipments are not all at the scanning facility. That is intentional: a
// rejection does the same work as an acceptance — resolve the barcode, lock the
// row, evaluate custody, write a scan record — so it measures the same cost,
// and it exercises the rejection-recording path that a benchmark would
// otherwise skip.
export function scannerFlow(data) {
  const awb = pick(data.awbs);
  const res = http.post(`${BASE}/api/v1/scans`, JSON.stringify({
    barcode: awb,
    scanType: 'RECEIVE',
    operatingUnitId: data.unitId,
  }), {
    headers: authHeaders(data, {
      'X-Device-Id': `k6-scanner-${__VU}`,
      'X-Device-Event-Id': `k6-${__VU}-${__ITER}-${Date.now()}`,
    }),
    tags: { endpoint: 'scan' },
  });

  scanLatency.add(res.timings.duration);
  const ok = check(res, { 'scan answered': (r) => r.status === 200 });
  errorRate.add(!ok);
  if (res.status === 200) {
    const outcome = res.json('outcome');
    if (outcome === 'ACCEPTED') scansAccepted.add(1);
    else scansRejected.add(1);
  }
}

export function trackingFlow(data) {
  const awb = pick(data.awbs);
  const res = http.get(`${BASE}/api/v1/track/${awb}`, {
    tags: { endpoint: 'track' },
  });
  trackLatency.add(res.timings.duration);
  const ok = check(res, {
    'tracking resolved': (r) => r.status === 200,
    'milestone present': (r) => r.status === 200 && r.json('milestone') !== '',
  });
  errorRate.add(!ok);
}

export function dashboardFlow(data) {
  const headers = { headers: authHeaders(data), tags: { endpoint: 'dashboard' } };

  const summary = http.get(
    `${BASE}/api/v1/hub/summary?operatingUnitId=${data.unitId}`, headers);
  dashboardLatency.add(summary.timings.duration);
  errorRate.add(!check(summary, { 'hub summary': (r) => r.status === 200 }));

  const inbound = http.get(
    `${BASE}/api/v1/hub/inbound?operatingUnitId=${data.unitId}&limit=25`, headers);
  errorRate.add(!check(inbound, { 'hub inbound': (r) => r.status === 200 }));

  const queue = http.get(
    `${BASE}/api/v1/deliveries/queue?operatingUnitId=${data.unitId}&limit=25`, headers);
  deliveryLatency.add(queue.timings.duration);
  errorRate.add(!check(queue, { 'delivery queue': (r) => r.status === 200 }));

  sleep(0.2);
}

export function baggingFlow(data) {
  const headers = { headers: authHeaders(data), tags: { endpoint: 'bagging' } };

  const list = http.get(`${BASE}/api/v1/bags?limit=25`, headers);
  bagScanLatency.add(list.timings.duration);
  errorRate.add(!check(list, { 'bag list': (r) => r.status === 200 }));

  const manifests = http.get(`${BASE}/api/v1/manifests?limit=25`, headers);
  errorRate.add(!check(manifests, { 'manifest list': (r) => r.status === 200 }));

  const scans = http.get(
    `${BASE}/api/v1/scans?operatingUnitId=${data.unitId}&limit=25`, headers);
  errorRate.add(!check(scans, { 'scan history': (r) => r.status === 200 }));

  sleep(0.3);
}

export function handleSummary(data) {
  const m = data.metrics;
  const line = (name, metric, key) =>
    metric ? `${name}: ${(metric.values[key] || 0).toFixed(2)}` : `${name}: n/a`;

  const summary = [
    'Release 2 operational load profile',
    '',
    line('requests', m.http_reqs, 'count'),
    line('requests/s', m.http_reqs, 'rate'),
    line('failed rate', m.http_req_failed, 'rate'),
    line('scans accepted', m.scans_accepted, 'count'),
    line('scans rejected', m.scans_rejected, 'count'),
    '',
    line('scan p95 ms', m.scan_latency, 'p(95)'),
    line('scan p99 ms', m.scan_latency, 'p(99)'),
    line('tracking p95 ms', m.tracking_latency, 'p(95)'),
    line('hub dashboard p95 ms', m.hub_dashboard_latency, 'p(95)'),
    line('bag list p95 ms', m.bag_scan_latency, 'p(95)'),
    line('delivery queue p95 ms', m.delivery_update_latency, 'p(95)'),
    '',
  ].join('\n');

  return {
    stdout: '\n' + summary + '\n',
    'tests/load/results/operations-summary.json': JSON.stringify(data, null, 2),
  };
}
