#!/usr/bin/env python3
"""
Async load test generator for the SSP Ad Server.

Sends bid requests at a configurable QPS with optional ramp-up, then prints
a detailed statistics report including latency percentiles and status code
distribution.

Usage:
    python load_test.py --url http://localhost:8080/bid --qps 100 --duration 10
    python load_test.py --url http://localhost:8080/bid --qps 200 --duration 30 --ramp-up 5
"""

import argparse
import asyncio
import json
import signal
import sys
import time
from dataclasses import dataclass, field
from typing import List, Optional

import aiohttp
import numpy as np


# ---------------------------------------------------------------------------
# Sample OpenRTB 2.5 bid request payload
# ---------------------------------------------------------------------------
SAMPLE_BID_REQUEST = {
    "id": "load-test-req",
    "imp": [
        {
            "id": "imp-1",
            "banner": {"w": 300, "h": 250},
            "bidfloor": 0.50,
        }
    ],
    "site": {
        "id": "site-001",
        "domain": "loadtest.example.com",
        "page": "https://loadtest.example.com/article",
    },
    "device": {
        "ua": "LoadTest/1.0",
        "ip": "203.0.113.42",
        "device_type": 2,
        "geo": {"country": "US", "city": "New York"},
    },
    "user": {"id": "load-test-user-001"},
}


# ---------------------------------------------------------------------------
# Result data class
# ---------------------------------------------------------------------------
@dataclass
class RequestResult:
    """Captures timing and outcome of a single HTTP request."""

    start: float
    end: float
    status: Optional[int] = None
    error: Optional[str] = None

    @property
    def latency_ms(self) -> float:
        return (self.end - self.start) * 1000.0


# ---------------------------------------------------------------------------
# Load test runner
# ---------------------------------------------------------------------------
class LoadTestRunner:
    """Async load generator that fires bid requests at a target QPS."""

    def __init__(
        self,
        url: str,
        qps: int,
        duration: int,
        ramp_up: int,
    ):
        self.url = url
        self.qps = qps
        self.duration = duration
        self.ramp_up = ramp_up
        self.results: List[RequestResult] = []
        self._stop = False

    async def _send_request(
        self, session: aiohttp.ClientSession, payload: bytes
    ) -> RequestResult:
        """Send a single POST request and record the result."""
        start = time.monotonic()
        try:
            async with session.post(
                self.url,
                data=payload,
                headers={
                    "Content-Type": "application/json",
                    "X-Consent": "true",
                },
                timeout=aiohttp.ClientTimeout(total=2),
            ) as resp:
                # Drain response body to release connection back to pool.
                await resp.read()
                end = time.monotonic()
                return RequestResult(start=start, end=end, status=resp.status)
        except asyncio.TimeoutError:
            end = time.monotonic()
            return RequestResult(start=start, end=end, error="timeout")
        except aiohttp.ClientError as exc:
            end = time.monotonic()
            return RequestResult(start=start, end=end, error=str(exc))
        except Exception as exc:  # noqa: BLE001
            end = time.monotonic()
            return RequestResult(start=start, end=end, error=str(exc))

    def _compute_qps_at(self, elapsed: float) -> float:
        """Return the target QPS at a given elapsed second, accounting for ramp-up."""
        if self.ramp_up <= 0 or elapsed >= self.ramp_up:
            return float(self.qps)
        # Linear ramp from 10 QPS to target QPS over ramp_up seconds.
        start_qps = 10.0
        progress = elapsed / self.ramp_up
        return start_qps + (self.qps - start_qps) * progress

    async def run(self) -> None:
        """Execute the load test."""
        connector = aiohttp.TCPConnector(limit=1000)
        payload = json.dumps(SAMPLE_BID_REQUEST).encode()

        total_duration = self.ramp_up + self.duration
        print(f"\n{'='*60}")
        print(f"  SSP Ad Server Load Test")
        print(f"{'='*60}")
        print(f"  Target URL     : {self.url}")
        print(f"  Target QPS     : {self.qps}")
        print(f"  Duration       : {self.duration}s")
        print(f"  Ramp-up        : {self.ramp_up}s")
        print(f"  Total time     : {total_duration}s")
        print(f"  Max connections: 1000")
        print(f"{'='*60}\n")

        async with aiohttp.ClientSession(connector=connector) as session:
            start_time = time.monotonic()
            tasks: List[asyncio.Task] = []
            requests_sent = 0

            try:
                while not self._stop:
                    elapsed = time.monotonic() - start_time
                    if elapsed >= total_duration:
                        break

                    current_qps = self._compute_qps_at(elapsed)
                    interval = 1.0 / current_qps if current_qps > 0 else 1.0

                    # Schedule one request.
                    task = asyncio.create_task(
                        self._send_request(session, payload)
                    )
                    tasks.append(task)
                    requests_sent += 1

                    # Progress indicator every 100 requests.
                    if requests_sent % 100 == 0:
                        print(
                            f"  [{elapsed:6.1f}s] Sent {requests_sent} requests "
                            f"(current QPS target: {current_qps:.0f})"
                        )

                    await asyncio.sleep(interval)

            except asyncio.CancelledError:
                pass

            # Wait for in-flight requests to complete (with a generous timeout).
            if tasks:
                print(f"\n  Waiting for {len(tasks)} in-flight requests...")
                done, pending = await asyncio.wait(
                    tasks, timeout=5.0, return_when=asyncio.ALL_COMPLETED
                )
                # Cancel any stragglers.
                for t in pending:
                    t.cancel()

                for t in done:
                    exc = t.exception()
                    if exc is None:
                        self.results.append(t.result())

        actual_duration = time.monotonic() - start_time
        self._print_report(actual_duration)

    def _print_report(self, actual_duration: float) -> None:
        """Print the statistics report."""
        results = self.results
        total = len(results)

        if total == 0:
            print("\n  No requests completed — nothing to report.")
            return

        successful = sum(1 for r in results if r.status and 200 <= r.status < 300 and r.status != 204)
        no_bid = sum(1 for r in results if r.status == 204)
        client_errors = sum(1 for r in results if r.status and 400 <= r.status < 500)
        server_errors = sum(1 for r in results if r.status and 500 <= r.status < 600)
        timeouts = sum(1 for r in results if r.error == "timeout")
        other_errors = sum(1 for r in results if r.error and r.error != "timeout")

        latencies = np.array([r.latency_ms for r in results])

        p50 = float(np.percentile(latencies, 50))
        p95 = float(np.percentile(latencies, 95))
        p99 = float(np.percentile(latencies, 99))
        p_max = float(np.max(latencies))
        effective_qps = total / actual_duration if actual_duration > 0 else 0

        print(f"\n{'='*60}")
        print(f"  LOAD TEST RESULTS")
        print(f"{'='*60}")
        print(f"  Total requests sent : {total}")
        print(f"  Successful (2xx)    : {successful}")
        print(f"  No-bid (204)        : {no_bid}")
        print(f"  Client errors (4xx) : {client_errors}")
        print(f"  Server errors (5xx) : {server_errors}")
        print(f"  Timeouts            : {timeouts}")
        if other_errors:
            print(f"  Other errors        : {other_errors}")
        print(f"  {'─'*40}")
        print(f"  Latency p50         : {p50:.1f}ms")
        print(f"  Latency p95         : {p95:.1f}ms")
        print(f"  Latency p99         : {p99:.1f}ms")
        print(f"  Latency max         : {p_max:.1f}ms")
        print(f"  {'─'*40}")
        print(f"  Effective QPS       : {effective_qps:.1f}")
        print(f"  Actual duration     : {actual_duration:.1f}s")
        print(f"{'='*60}\n")

    def stop(self) -> None:
        """Signal the runner to stop gracefully."""
        self._stop = True


# ---------------------------------------------------------------------------
# Entrypoint
# ---------------------------------------------------------------------------
def main() -> None:
    parser = argparse.ArgumentParser(
        description="Async load test generator for the SSP Ad Server."
    )
    parser.add_argument(
        "--url",
        required=True,
        help="Target URL (e.g. http://localhost:8080/bid)",
    )
    parser.add_argument(
        "--qps",
        type=int,
        default=100,
        help="Target queries per second (default: 100)",
    )
    parser.add_argument(
        "--duration",
        type=int,
        default=10,
        help="Sustained load duration in seconds (default: 10)",
    )
    parser.add_argument(
        "--ramp-up",
        type=int,
        default=0,
        dest="ramp_up",
        help="Ramp-up period in seconds — linearly increase from 10 QPS to "
        "target QPS (default: 0, no ramp-up)",
    )
    args = parser.parse_args()

    runner = LoadTestRunner(
        url=args.url,
        qps=args.qps,
        duration=args.duration,
        ramp_up=args.ramp_up,
    )

    # Handle Ctrl+C gracefully — print partial stats.
    def _sigint_handler(sig, frame):
        print("\n\n  >>> KeyboardInterrupt received — stopping...")
        runner.stop()

    signal.signal(signal.SIGINT, _sigint_handler)

    try:
        asyncio.run(runner.run())
    except KeyboardInterrupt:
        # Fallback if asyncio.run itself raises before we can handle it.
        print("\n  Interrupted.")
        sys.exit(1)


if __name__ == "__main__":
    main()
