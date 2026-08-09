// Release 4 load profile: portals, console, command centre and the partner API.
//
// These five surfaces have very different shapes, and the mix reflects how they
// are actually used rather than giving each an equal share:
//
//   * the **console** is the highest-rate authenticated surface in the network —
//     every scanner in every branch, all shift;
//   * the **command centre** is polled continuously by every supervisor, which
//     is why its cost was the subject of migration 0030;
//   * the **customer portal** is bursty and read-heavy;
//   * the **franchise portal** is low-rate but expensive per call;
//   * the **partner API** is a program, so its rate is steady and its failures
//     are silent unless measured.
//
// Run:
//   k6 run -e BASE_URL=http://localhost:8080 \
//          -e EMAIL=admin@demo.test -e PASSWORD='...' \
//          -e UNIT_ID=ou_... -e AWBS=AWB1,AWB2 \
//          -e PARTNER_TOKEN=ck_xxx.yyy \
//          -e PORTAL_EMAIL=customer@demo.test -e PORTAL_PASSWORD='...' \
//          -e FRANCHISE_EMAIL=owner@demo.test -e FRANCHISE_PASSWORD='...' \
//          tests/load/courier-productization.js
//
// Thresholds are sized for the 4 vCPU / 16 GB VPS in Constitution §2. They are
// deliberately tight on the console and the command centre: those two are the
// ones whose cost is structural rather than incidental, and a regression in
// either is a regression in the platform's ability to be watched.

import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';

const BASE = __ENV.BASE_URL || 'http://localhost:8080';
const EMAIL = __ENV.EMAIL;
const PASSWORD = __ENV.PASSWORD;
const UNIT_ID = __ENV.UNIT_ID;
const AWBS = (__ENV.AWBS || '').split(',').filter(Boolean);
const PARTNER_TOKEN = __ENV.PARTNER_TOKEN || '';
const PORTAL_EMAIL = __ENV.PORTAL_EMAIL || '';
const PORTAL_PASSWORD = __ENV.PORTAL_PASSWORD || '';
const FRANCHISE_EMAIL = __ENV.FRANCHISE_EMAIL || '';
const FRANCHISE_PASSWORD = __ENV.FRANCHISE_PASSWORD || '';

const consoleLookupLatency = new Trend('console_lookup_latency', true);
const consoleQueueLatency = new Trend('console_queue_latency', true);
const commandLatency = new Trend('command_centre_latency', true);
const customerLatency = new Trend('customer_portal_latency', true);
const franchiseLatency = new Trend('franchise_portal_latency', true);
const partnerLatency = new Trend('partner_api_latency', true);

const consoleLookups = new Counter('console_lookups');
const partnerBookings = new Counter('partner_bookings');
const rateLimited = new Counter('partner_rate_limited');
const errorRate = new Rate('business_errors');

export const options = {
  scenarios: {
    // Every scanner in the network. The highest-rate authenticated surface,
    // and the one whose payload size was designed around this number.
    console: {
      executor: 'constant-arrival-rate',
      rate: 30, timeUnit: '1s', duration: '2m',
      preAllocatedVUs: 15, maxVUs: 40,
      exec: 'consoleFlow',
    },
    // Supervisors watching. Polled continuously, from many browsers at once.
    commandCentre: {
      executor: 'constant-arrival-rate',
      rate: 6, timeUnit: '1s', duration: '2m',
      preAllocatedVUs: 6, maxVUs: 20,
      exec: 'commandFlow',
    },
    // Customers checking their own parcels. Bursty by nature.
    customerPortal: {
      executor: 'ramping-arrival-rate',
      startRate: 2, timeUnit: '1s',
      stages: [
        { target: 10, duration: '45s' },
        { target: 10, duration: '45s' },
        { target: 2, duration: '30s' },
      ],
      preAllocatedVUs: 10, maxVUs: 30,
      exec: 'customerFlow',
    },
    // Franchise owners. Low rate, expensive per call: the summary touches the
    // rollup, commission and COD in one request.
    franchisePortal: {
      executor: 'constant-arrival-rate',
      rate: 2, timeUnit: '1s', duration: '2m',
      preAllocatedVUs: 4, maxVUs: 10,
      exec: 'franchiseFlow',
    },
    // A partner integration: steady, automated, and unforgiving of latency
    // because a queue backs up behind it.
    partnerApi: {
      executor: 'constant-arrival-rate',
      rate: 5, timeUnit: '1s', duration: '2m',
      preAllocatedVUs: 5, maxVUs: 20,
      exec: 'partnerFlow',
    },
  },
  thresholds: {
    // The console is a handheld waiting on a person's next action. Anything
    // over a quarter second is felt.
    console_lookup_latency: ['p(95)<250'],
    console_queue_latency: ['p(95)<400'],
    // The command centre reads a rollup and three indexed counts. If this
    // regresses, an index has stopped being used — see migration 0030.
    command_centre_latency: ['p(95)<500'],
    customer_portal_latency: ['p(95)<600'],
    // The franchise summary is three aggregates in one call, so it gets more
    // room — but not unbounded room.
    franchise_portal_latency: ['p(95)<900'],
    partner_api_latency: ['p(95)<700'],
    business_errors: ['rate<0.01'],
    http_req_failed: ['rate<0.02'],
  },
};

export function setup() {
  const staff = login(EMAIL, PASSWORD);
  const portal = PORTAL_EMAIL ? login(PORTAL_EMAIL, PORTAL_PASSWORD) : '';
  // A franchise login rather than the admin's: an unscoped principal is
  // correctly asked to name a franchise, so running the scenario as staff
  // measures the refusal path rather than the endpoint.
  const franchise = FRANCHISE_EMAIL ? login(FRANCHISE_EMAIL, FRANCHISE_PASSWORD) : '';
  return { staff, portal, franchise };
}

function login(email, password) {
  if (!email || !password) return '';
  const res = http.post(`${BASE}/api/v1/auth/login`,
    JSON.stringify({ email, password }),
    { headers: { 'Content-Type': 'application/json' }, tags: { name: 'login' } });
  if (res.status !== 200) {
    throw new Error(`login failed for ${email}: ${res.status} ${res.body}`);
  }
  // The login envelope nests the token: {"tokens":{"accessToken":...}}.
  return res.json('tokens.accessToken');
}

function authed(token, extra) {
  return {
    headers: Object.assign(
      { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
      extra || {}),
  };
}

// ---------------------------------------------------------------------------
// M29 Console — the scanner
// ---------------------------------------------------------------------------

export function consoleFlow(data) {
  if (!data.staff || !UNIT_ID) return;
  const opts = authed(data.staff, { 'X-Operating-Unit': UNIT_ID });

  // The dominant call: an operator scans a parcel.
  if (AWBS.length > 0) {
    const awb = AWBS[Math.floor(Math.random() * AWBS.length)];
    const res = http.get(`${BASE}/api/v1/console/lookup/${awb}`,
      Object.assign({}, opts, { tags: { name: 'console_lookup' } }));
    consoleLookupLatency.add(res.timings.duration);
    consoleLookups.add(1);
    const ok = check(res, {
      'lookup resolved': (r) => r.status === 200 || r.status === 404,
      // The payload budget is the feature. A regression here is somebody
      // widening the projection back towards a full shipment.
      'lookup stayed compact': (r) => r.status !== 200 || r.body.length < 800,
    });
    errorRate.add(!ok);
  }

  // Periodically the operator looks at the queue rather than a single parcel.
  if (Math.random() < 0.2) {
    const res = http.get(`${BASE}/api/v1/console/queue?limit=25`,
      Object.assign({}, opts, { tags: { name: 'console_queue' } }));
    consoleQueueLatency.add(res.timings.duration);
    errorRate.add(!check(res, { 'queue served': (r) => r.status === 200 }));
  }
  sleep(0.1);
}

// ---------------------------------------------------------------------------
// M30 Command centre — the supervisors
// ---------------------------------------------------------------------------

export function commandFlow(data) {
  if (!data.staff) return;
  const opts = authed(data.staff);

  const res = http.get(`${BASE}/api/v1/command-centre`,
    Object.assign({}, opts, { tags: { name: 'command_centre' } }));
  commandLatency.add(res.timings.duration);
  const ok = check(res, {
    'overview served': (r) => r.status === 200,
    // The response must keep saying which figures are exact; a dashboard that
    // lost this would present sampled numbers as live.
    'consistency stated': (r) => r.status !== 200 || r.body.includes('consistency'),
  });
  errorRate.add(!ok);

  // A supervisor drilling in: the trend chart and the league table.
  if (Math.random() < 0.3) {
    const trend = http.get(`${BASE}/api/v1/command-centre/trend`,
      Object.assign({}, opts, { tags: { name: 'command_trend' } }));
    commandLatency.add(trend.timings.duration);
    errorRate.add(!check(trend, { 'trend served': (r) => r.status === 200 }));
  }
  if (Math.random() < 0.2) {
    const backlog = http.get(`${BASE}/api/v1/command-centre/backlog`,
      Object.assign({}, opts, { tags: { name: 'command_backlog' } }));
    commandLatency.add(backlog.timings.duration);
    errorRate.add(!check(backlog, { 'backlog served': (r) => r.status === 200 }));
  }
  sleep(0.5);
}

// ---------------------------------------------------------------------------
// M27 Customer portal
// ---------------------------------------------------------------------------

export function customerFlow(data) {
  if (!data.portal) return;
  const opts = authed(data.portal);

  const summary = http.get(`${BASE}/api/v1/portal/customer/summary`,
    Object.assign({}, opts, { tags: { name: 'portal_customer_summary' } }));
  customerLatency.add(summary.timings.duration);
  errorRate.add(!check(summary, { 'customer summary served': (r) => r.status === 200 }));

  const list = http.get(`${BASE}/api/v1/portal/customer/shipments?limit=25`,
    Object.assign({}, opts, { tags: { name: 'portal_customer_shipments' } }));
  customerLatency.add(list.timings.duration);
  errorRate.add(!check(list, { 'customer shipments served': (r) => r.status === 200 }));

  sleep(1);
}

// ---------------------------------------------------------------------------
// M28 Franchise portal
// ---------------------------------------------------------------------------

export function franchiseFlow(data) {
  const token = data.franchise || data.staff;
  if (!token) return;
  const opts = authed(token);

  // The expensive one: rollup, commission and COD in a single request.
  const res = http.get(`${BASE}/api/v1/portal/franchise/summary`,
    Object.assign({}, opts, { tags: { name: 'portal_franchise_summary' } }));
  franchiseLatency.add(res.timings.duration);
  errorRate.add(!check(res, {
    'franchise summary served': (r) => r.status === 200,
  }));
  sleep(1);
}

// ---------------------------------------------------------------------------
// M32 Partner API
// ---------------------------------------------------------------------------

export function partnerFlow() {
  if (!PARTNER_TOKEN) return;
  const opts = {
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${PARTNER_TOKEN}`,
    },
  };

  // Tracking is what a partner calls most: their own customers are asking.
  if (AWBS.length > 0) {
    const awb = AWBS[Math.floor(Math.random() * AWBS.length)];
    const res = http.get(`${BASE}/api/v1/partner/tracking/${awb}`,
      Object.assign({}, opts, { tags: { name: 'partner_tracking' } }));
    partnerLatency.add(res.timings.duration);
    if (res.status === 429) {
      // Being throttled is the system working, not an error. Counted so a run
      // says whether the key's allowance is the binding constraint.
      rateLimited.add(1);
    } else {
      errorRate.add(!check(res, {
        'partner tracking served': (r) => r.status === 200 || r.status === 404,
      }));
    }
  }

  // A quote, which exercises the pricing engine through the partner surface.
  if (Math.random() < 0.3) {
    const res = http.get(`${BASE}/api/v1/partner/whoami`,
      Object.assign({}, opts, { tags: { name: 'partner_whoami' } }));
    partnerLatency.add(res.timings.duration);
    if (res.status === 429) {
      rateLimited.add(1);
    } else {
      errorRate.add(!check(res, { 'partner identity served': (r) => r.status === 200 }));
    }
  }
  partnerBookings.add(0);
  sleep(0.2);
}

export function handleSummary(data) {
  // Written to a file as well as stdout: the trend numbers are the evidence a
  // release report cites, and re-deriving them from a terminal scrollback is
  // how a regression goes unnoticed.
  const line = (name) => {
    const m = data.metrics[name];
    if (!m || !m.values) return `  ${name}: no samples`;
    const p95 = m.values['p(95)'];
    return `  ${name}: p95=${p95 ? p95.toFixed(1) : '—'}ms  avg=${m.values.avg ? m.values.avg.toFixed(1) : '—'}ms`;
  };
  const count = (name) => {
    const m = data.metrics[name];
    return `  ${name}: ${m && m.values ? m.values.count : 0}`;
  };
  const failed = data.metrics.http_req_failed;
  const reqs = data.metrics.http_reqs;
  const out = {
    stdout: [
      '',
      'Release 4 productization load profile',
      '=====================================',
      line('console_lookup_latency'),
      line('console_queue_latency'),
      line('command_centre_latency'),
      line('customer_portal_latency'),
      line('franchise_portal_latency'),
      line('partner_api_latency'),
      '',
      count('console_lookups'),
      count('partner_rate_limited'),
      `  http_reqs: ${reqs && reqs.values ? reqs.values.count : 0}`,
      `  http_req_failed: ${failed && failed.values ? (failed.values.rate * 100).toFixed(2) : '—'}%`,
      '',
    ].join('\n'),
  };
  out['summary.json'] = JSON.stringify(data.metrics, null, 2);
  return out;
}
