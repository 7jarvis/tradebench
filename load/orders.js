// k6 load test: steady order flow against the sandbox broker.
//
//   make up && make load
//
// Open model (constant-arrival-rate): requests keep arriving at the target
// rate even when the service slows down, so latency problems surface as
// latency instead of being hidden by fewer requests (coordinated omission).
import http from 'k6/http';
import { check } from 'k6';

const BASE = __ENV.BASE_URL || 'http://localhost:8080';
const TOKEN = __ENV.API_TOKEN || 'local-dev-token';
const ACCOUNTS = 20;

const params = (name) => ({
  headers: { Authorization: `Bearer ${TOKEN}`, 'Content-Type': 'application/json' },
  tags: { name },
});

export const options = {
  scenarios: {
    place_orders: {
      executor: 'constant-arrival-rate',
      rate: Number(__ENV.RATE || 50),
      timeUnit: '1s',
      duration: __ENV.DURATION || '1m',
      preAllocatedVUs: 20,
      maxVUs: 100,
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    'http_req_duration{name:place_order}': ['p(95)<200', 'p(99)<500'],
    'http_req_duration{name:get_account}': ['p(95)<100'],
    checks: ['rate>0.99'],
  },
};

// Fresh funded accounts per run; orders spread across them so the test
// measures throughput, not contention on a single account row lock.
export function setup() {
  const ids = [];
  for (let i = 0; i < ACCOUNTS; i++) {
    const acc = http.post(`${BASE}/v1/accounts`, JSON.stringify({ owner_name: `k6 load ${i}` }), params('setup'));
    check(acc, { 'account created': (r) => r.status === 201 });
    const id = acc.json('id');
    http.post(`${BASE}/v1/accounts/${id}/deposits`, JSON.stringify({ amount: '1000000.00' }), params('setup'));
    ids.push(id);
  }
  return { ids };
}

export default function (data) {
  const id = data.ids[__ITER % data.ids.length];

  const order = http.post(
    `${BASE}/v1/accounts/${id}/orders`,
    JSON.stringify({ symbol: 'AAPL', side: 'buy', type: 'market', qty: '1' }),
    params('place_order'),
  );
  check(order, {
    'order 201': (r) => r.status === 201,
    'order filled': (r) => r.status === 201 && r.json('status') === 'filled',
  });

  if (__ITER % 5 === 0) {
    const acc = http.get(`${BASE}/v1/accounts/${id}`, params('get_account'));
    check(acc, { 'account 200': (r) => r.status === 200 });
  }
}

export function teardown(data) {
  for (const id of data.ids) {
    http.del(`${BASE}/v1/accounts/${id}`, null, params('teardown'));
  }
}
