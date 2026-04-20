# SSP Ad Server — Load Test Tool

An asynchronous load test generator for the SSP Ad Server `/bid` endpoint.
Built with Python `aiohttp` for high-concurrency HTTP requests and `numpy`
for latency percentile calculations.

## Prerequisites

- Python 3.10+ installed on your system

## Setup (Windows)

```powershell
cd load_test

# Create a virtual environment
python -m venv venv

# Activate it
venv\Scripts\activate

# Install dependencies
pip install -r requirements.txt
```

## Usage

### Basic run (100 QPS for 10 seconds)

```powershell
python load_test.py --url http://localhost:8080/bid --qps 100 --duration 10
```

### With ramp-up (5s linear ramp from 10 → 200 QPS, then 30s sustained)

```powershell
python load_test.py --url http://localhost:8080/bid --qps 200 --duration 30 --ramp-up 5
```

### Low QPS smoke test

```powershell
python load_test.py --url http://localhost:8080/bid --qps 10 --duration 3
```

## CLI Arguments

| Argument     | Default | Description                                         |
| ------------ | ------- | --------------------------------------------------- |
| `--url`      | —       | **(Required)** Target URL, e.g. `http://localhost:8080/bid` |
| `--qps`      | `100`   | Target queries per second                           |
| `--duration` | `10`    | Sustained load duration in seconds                  |
| `--ramp-up`  | `0`     | Ramp-up period — linearly increase from 10 QPS to target QPS |

## Output

After the run completes (or on `Ctrl+C`), a statistics report is printed:

```
============================================================
  LOAD TEST RESULTS
============================================================
  Total requests sent : 1000
  Successful (2xx)    : 450
  No-bid (204)        : 520
  Client errors (4xx) : 10
  Server errors (5xx) : 0
  Timeouts            : 20
  ────────────────────────────────────────
  Latency p50         : 12.3ms
  Latency p95         : 45.6ms
  Latency p99         : 78.9ms
  Latency max         : 102.1ms
  ────────────────────────────────────────
  Effective QPS       : 98.7
  Actual duration     : 10.1s
============================================================
```

## Checking Server Metrics

After running the load test, you can inspect server-side counters:

```powershell
# View metrics
curl http://localhost:8080/metrics

# View and reset metrics
curl "http://localhost:8080/metrics?reset=true"
```
