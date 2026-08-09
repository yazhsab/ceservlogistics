// k6 load profile for the Release 1 surface.
//
//   k6 run -e BASE_URL=http://localhost:8080 \
//          -e EMAIL=admin@demo.test -e PASSWORD='...' \
//          -e CUSTOMER_ID=cus_... -e SERVICE_CODE=EXPRESS \
//          tests/load/courier-load.js
//
// The profile mirrors a real branch day rather than a synthetic maximum: many
// more lookups than bookings, and reads dominating writes. Thresholds are set
// at what the 4 vCPU target should sustain, so a regression fails the run
// instead of quietly degrading.

import http from 'k6/http';
import { check, group, sleep } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';
import { randomIntBetween } from 'https://jslib.k6.io/k6-utils/1.4.0/index.js';

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const EMAIL = __ENV.EMAIL || 'admin@demo.test';
const PASSWORD = __ENV.PASSWORD || 'DemoPassw0rd!2026';
const CUSTOMER_ID = __ENV.CUSTOMER_ID || '';
const SERVICE_CODE = __ENV.SERVICE_CODE || 'EXPRESS';
const ORIGIN_PINCODE = __ENV.ORIGIN_PINCODE || '560001';
const DEST_PINCODE = __ENV.DEST_PINCODE || '110001';

const bookingFailures = new Rate('booking_failures');
const bookingDuration = new Trend('booking_duration', true);
const quoteDuration = new Trend('quote_duration', true);
const lookupDuration = new Trend('pincode_lookup_duration', true);
const serviceabilityDuration = new Trend('serviceability_duration', true);
const listDuration = new Trend('shipment_list_duration', true);
const detailDuration = new Trend('shipment_detail_duration', true);
const loginDuration = new Trend('login_duration', true);
const duplicateAwbs = new Counter('duplicate_awbs');

const seenAwbs = new Set();

export const options = {
  scenarios: {
    // The bulk of traffic: counter staff quoting and checking coverage.
    lookups: {
      executor: 'ramping-vus',
      exec: 'lookupFlow',
      startVUs: 0,
      stages: [
        { duration: '30s', target: 20 },
        { duration: '2m', target: 20 },
        { duration: '30s', target: 0 },
      ],
    },
    // Bookings: the expensive path, at a realistic fraction of lookups.
    bookings: {
      executor: 'ramping-vus',
      exec: 'bookingFlow',
      startVUs: 0,
      stages: [
        { duration: '30s', target: 8 },
        { duration: '2m', target: 8 },
        { duration: '30s', target: 0 },
      ],
    },
    // Operations console: listing and drilling into shipments.
    console: {
      executor: 'constant-vus',
      exec: 'consoleFlow',
      vus: 5,
      duration: '3m',
    },
    // Logins are rare but expensive (Argon2id), so they are measured
    // separately rather than being averaged away.
    logins: {
      executor: 'constant-arrival-rate',
      exec: 'loginFlow',
      rate: 2,
      timeUnit: '1s',
      duration: '3m',
      preAllocatedVUs: 5,
      maxVUs: 20,
    },
  },
  thresholds: {
    // Reads are cached and index-served; anything slower indicates a plan or
    // cache regression.
    'pincode_lookup_duration': ['p(95)<50', 'p(99)<150'],
    'serviceability_duration': ['p(95)<120', 'p(99)<300'],
    'quote_duration': ['p(95)<150', 'p(99)<400'],
    'shipment_list_duration': ['p(95)<200', 'p(99)<500'],
    'shipment_detail_duration': ['p(95)<150', 'p(99)<400'],
    // A booking does routing, pricing, AWB allocation and eight writes.
    'booking_duration': ['p(95)<600', 'p(99)<1200'],
    // Argon2id at production cost is deliberately slow.
    'login_duration': ['p(95)<400'],
    'booking_failures': ['rate<0.01'],
    'http_req_failed': ['rate<0.01'],
    // Every AWB must be unique; even one duplicate fails the run.
    'duplicate_awbs': ['count==0'],
  },
};

function login() {
  const res = http.post(`${BASE_URL}/api/v1/auth/login`,
    JSON.stringify({ email: EMAIL, password: PASSWORD }),
    { headers: { 'Content-Type': 'application/json' }, tags: { name: 'login' } });
  loginDuration.add(res.timings.duration);
  check(res, { 'login succeeded': (r) => r.status === 200 });
  if (res.status !== 200) return null;
  return res.json('tokens.accessToken');
}

export function setup() {
  const token = login();
  if (!token) {
    throw new Error('setup failed: could not authenticate; check EMAIL and PASSWORD');
  }
  if (!CUSTOMER_ID) {
    throw new Error('setup failed: CUSTOMER_ID is required for the booking scenario');
  }
  return { token };
}

function authHeaders(token, extra = {}) {
  return {
    headers: Object.assign({
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${token}`,
    }, extra),
  };
}

export function loginFlow() {
  login();
  sleep(1);
}

export function lookupFlow(data) {
  group('lookups', () => {
    const pin = http.get(`${BASE_URL}/api/v1/geography/pincodes/${ORIGIN_PINCODE}`,
      authHeaders(data.token));
    lookupDuration.add(pin.timings.duration);
    check(pin, { 'pincode lookup ok': (r) => r.status === 200 });

    const serviceability = http.post(`${BASE_URL}/api/v1/serviceability/check`,
      JSON.stringify({
        originPincode: ORIGIN_PINCODE,
        destinationPincode: DEST_PINCODE,
        serviceCode: SERVICE_CODE,
      }), authHeaders(data.token));
    serviceabilityDuration.add(serviceability.timings.duration);
    check(serviceability, {
      'serviceability ok': (r) => r.status === 200,
      'lane is serviceable': (r) => r.json('serviceable') === true,
    });

    const quote = http.post(`${BASE_URL}/api/v1/pricing/quote`,
      JSON.stringify({
        originPincode: ORIGIN_PINCODE,
        destinationPincode: DEST_PINCODE,
        serviceCode: SERVICE_CODE,
        paymentMode: 'PREPAID',
        packages: [{ actualWeightGrams: randomIntBetween(200, 5000) }],
      }), authHeaders(data.token));
    quoteDuration.add(quote.timings.duration);
    check(quote, {
      'quote ok': (r) => r.status === 200,
      'quote has a breakdown': (r) => (r.json('lineItems') || []).length > 0,
    });
  });
  sleep(randomIntBetween(1, 3));
}

export function bookingFlow(data) {
  const weight = randomIntBetween(200, 8000);
  const body = JSON.stringify({
    customerId: CUSTOMER_ID,
    serviceCode: SERVICE_CODE,
    paymentMode: 'PREPAID',
    sender: {
      contactName: 'Load Sender', phone: '+919800000001',
      line1: '12 Origin Street', city: 'Bengaluru', state: 'Karnataka',
      pincode: ORIGIN_PINCODE,
    },
    recipient: {
      contactName: 'Load Recipient', phone: '+919800000002',
      line1: '34 Destination Road', city: 'New Delhi', state: 'Delhi',
      pincode: DEST_PINCODE,
    },
    packages: [{ actualWeightGrams: weight, lengthMm: 200, widthMm: 150, heightMm: 100 }],
    contentDescription: 'Load test parcel',
  });

  // A unique key per attempt: this measures the booking path, not the replay
  // path. Idempotent replay is covered by the integration suite.
  const key = `load-${__VU}-${__ITER}-${Date.now()}`;
  const res = http.post(`${BASE_URL}/api/v1/shipments`, body,
    authHeaders(data.token, { 'Idempotency-Key': key }));

  bookingDuration.add(res.timings.duration);
  const ok = check(res, {
    'booking created': (r) => r.status === 201,
    'booking returned an AWB': (r) => !!r.json('awb'),
  });
  bookingFailures.add(!ok);

  if (res.status === 201) {
    const awb = res.json('awb');
    // Per-VU uniqueness only; the authoritative proof is the database UNIQUE
    // constraint and the concurrency test in the integration suite.
    if (seenAwbs.has(awb)) {
      duplicateAwbs.add(1);
    }
    seenAwbs.add(awb);
  }
  sleep(randomIntBetween(1, 4));
}

export function consoleFlow(data) {
  group('operations console', () => {
    const list = http.get(`${BASE_URL}/api/v1/shipments?limit=25`, authHeaders(data.token));
    listDuration.add(list.timings.duration);
    const listed = check(list, { 'shipment list ok': (r) => r.status === 200 });

    if (listed) {
      const rows = list.json('data') || [];
      if (rows.length > 0) {
        const id = rows[randomIntBetween(0, rows.length - 1)].id;
        const detail = http.get(`${BASE_URL}/api/v1/shipments/${id}`, authHeaders(data.token));
        detailDuration.add(detail.timings.duration);
        check(detail, {
          'shipment detail ok': (r) => r.status === 200,
          'detail carries its charge snapshot': (r) => r.json('charges') !== null,
        });
      }
      // Paging with a cursor is the expensive read path; exercise it too.
      const cursor = list.json('pagination.nextCursor');
      if (cursor) {
        const page2 = http.get(`${BASE_URL}/api/v1/shipments?limit=25&cursor=${cursor}`,
          authHeaders(data.token));
        listDuration.add(page2.timings.duration);
        check(page2, { 'second page ok': (r) => r.status === 200 });
      }
    }
  });
  sleep(randomIntBetween(2, 5));
}

export function handleSummary(data) {
  return {
    'stdout': textSummary(data),
    'tests/load/results/summary.json': JSON.stringify(data, null, 2),
  };
}

// A compact summary of the metrics that matter for the release record.
function textSummary(data) {
  const lines = ['', 'Courier OS — Release 1 load profile', ''];
  const interesting = [
    'http_reqs', 'http_req_failed', 'http_req_duration',
    'login_duration', 'pincode_lookup_duration', 'serviceability_duration',
    'quote_duration', 'booking_duration', 'booking_failures',
    'shipment_list_duration', 'shipment_detail_duration', 'duplicate_awbs',
  ];
  for (const name of interesting) {
    const m = data.metrics[name];
    if (!m) continue;
    const v = m.values;
    if (v.p95 !== undefined || v['p(95)'] !== undefined) {
      lines.push(`${name.padEnd(30)} avg=${fmt(v.avg)} p50=${fmt(v.med)} p95=${fmt(v['p(95)'])} p99=${fmt(v['p(99)'])} max=${fmt(v.max)}`);
    } else if (v.rate !== undefined) {
      lines.push(`${name.padEnd(30)} rate=${(v.rate * 100).toFixed(2)}%`);
    } else if (v.count !== undefined) {
      lines.push(`${name.padEnd(30)} count=${v.count}`);
    }
  }
  lines.push('');
  return lines.join('\n');
}

function fmt(v) {
  return v === undefined ? '-' : `${v.toFixed(1)}ms`;
}
