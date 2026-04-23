/**
 * SSP Ad Server — k6 Load Test
 *
 * Sends valid OpenRTB 2.5 bid requests to the SSP and measures latency and
 * error rate under realistic ramp-up traffic.
 *
 * Usage:
 *   k6 run loadtest/bid_request.js
 *   TARGET_URL=http://localhost:8080 k6 run loadtest/bid_request.js
 *
 * Results are written to loadtest/results/latest.json after each run.
 */

import http from 'k6/http';
import { check } from 'k6';
import { textSummary } from 'https://jslib.k6.io/k6-summary/0.0.1/index.js';

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

/** Base URL of the SSP server. Override with TARGET_URL env var. */
const BASE_URL = __ENV.TARGET_URL || 'http://localhost:8080';

export const options = {
  /**
   * Traffic shape:
   *   0 → 500 VUs over 30 s  (ramp-up, warms connection pools and caches)
   *   500 VUs for 60 s       (sustained load — this is the measurement window)
   *   500 → 0 VUs over 30 s  (ramp-down, lets in-flight requests complete)
   *
   * Total wall-clock: ~2 minutes.
   */
  stages: [
    { duration: '30s', target: 500 },
    { duration: '60s', target: 500 },
    { duration: '30s', target: 0 },
  ],

  /**
   * Hard SLA thresholds — the test fails if either is breached.
   *
   * p95 < 100ms  — OpenRTB requires DSPs respond within 100 ms; the SSP must
   *                return a response within that same window end-to-end.
   * error < 1%   — More than 1 % non-2xx / non-204 responses indicates a
   *                systemic problem (connection refused, 500s, timeouts).
   */
  thresholds: {
    http_req_duration: ['p(95)<100'],
    http_req_failed:   ['rate<0.01'],
  },
};

// ---------------------------------------------------------------------------
// Payload — realistic OpenRTB 2.5 bid request
// ---------------------------------------------------------------------------

/**
 * Two impression objects:
 *   imp[0] — 300×250 banner (standard IAB Medium Rectangle)
 *   imp[1] — 728×90 banner (Leaderboard)
 *
 * Having two impressions exercises the ENABLE_MULTI_IMP code path and
 * ensures the multi-seat response assembly is exercised under load.
 */
const PAYLOAD = JSON.stringify({
  id: 'k6-perf-req-001',
  at: 2,      // second-price auction (set to 1 + ENABLE_FIRST_PRICE=true for first-price testing)
  tmax: 100,  // 100 ms — mirrors real-world publisher setting
  cur: ['USD'],

  imp: [
    {
      id: 'imp-mrec',
      banner: {
        w: 300,
        h: 250,
        pos: 1,
        mimes: ['image/jpeg', 'image/png', 'image/gif'],
      },
      bidfloor: 0.50,
      bidfloorcur: 'USD',
      secure: 1,
    },
    {
      id: 'imp-leader',
      banner: {
        w: 728,
        h: 90,
        pos: 3,
        mimes: ['image/jpeg', 'image/png'],
      },
      bidfloor: 0.30,
      bidfloorcur: 'USD',
      secure: 1,
    },
  ],

  site: {
    id: 'site-news-001',
    name: 'Example News',
    domain: 'news.example.com',
    page: 'https://news.example.com/politics/article-456',
    publisher: {
      id: 'pub-001',
      name: 'Example Media Group',
    },
  },

  user: {
    id: 'user-k6-perf-001',
    buyeruid: 'buyer-uid-abc123xyz',
    gender: 'M',
    yob: 1988,
  },

  device: {
    ua: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36',
    ip: '203.0.113.42',
    language: 'en',
    os: 'Windows',
    devicetype: 2, // Personal Computer (OpenRTB §5.21)
    geo: {
      country: 'USA',
      city: 'New York',
      lat: 40.7128,
      lon: -74.0060,
    },
  },
});

const HEADERS = {
  'Content-Type': 'application/json',
  // The consent validator checks for a non-empty X-Consent header.
  // Real traffic would carry a TCF string; for load testing any value works.
  'X-Consent': '1',
};

// ---------------------------------------------------------------------------
// Virtual user entrypoint
// ---------------------------------------------------------------------------

export default function () {
  const res = http.post(`${BASE_URL}/bid`, PAYLOAD, { headers: HEADERS });

  check(res, {
    // 200 OK  → auction produced at least one winning bid
    // 204 No Content → no winners (floor too high / no DSP bids) — still valid
    'status 200 or 204': (r) => r.status === 200 || r.status === 204,

    // Enforce RTB SLA inline per-request (mirrors the p95 threshold above,
    // but gives per-iteration visibility in the k6 checks output).
    'response < 100ms': (r) => r.timings.duration < 100,
  });
}

// ---------------------------------------------------------------------------
// Post-test summary export
// ---------------------------------------------------------------------------

/**
 * handleSummary runs once after all VUs finish.
 * It writes the full structured summary to loadtest/results/latest.json
 * AND prints the human-readable table to stdout — no need for separate flags.
 */
export function handleSummary(data) {
  return {
    'loadtest/results/latest.json': JSON.stringify(data, null, 2),
    stdout: textSummary(data, { indent: '  ', enableColors: true }),
  };
}
