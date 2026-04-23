# SSP Ad Server — Load Test

Repeatable k6 performance benchmark for the SSP ad server.  
Establishes baseline numbers and enforces the RTB p95 < 100 ms SLA as a hard gate.

---

## Prerequisites

### Install k6

| Platform | Command |
|---|---|
| macOS (Homebrew) | `brew install k6` |
| Ubuntu / Debian | `sudo apt install k6` *(or see below)* |
| Windows (winget) | `winget install k6 --source winget` |
| Windows (Chocolatey) | `choco install k6` |
| Docker | `docker pull grafana/k6` |

**Ubuntu manual install** (if the apt package is unavailable):
```bash
sudo gpg -k
sudo gpg --no-default-keyring \
    --keyring /usr/share/keyrings/k6-archive-keyring.gpg \
    --keyserver hkp://keyserver.ubuntu.com:80 \
    --recv-keys C5AD17C747E3415A3642D57D77C6C491D6AC1D69
echo "deb [signed-by=/usr/share/keyrings/k6-archive-keyring.gpg] https://dl.k6.io/deb stable main" \
    | sudo tee /etc/apt/sources.list.d/k6.list
sudo apt update && sudo apt install k6
```

Verify: `k6 version`

---

## Running the Benchmark

### Against a local server

**Unix / macOS / WSL:**
```bash
# Start the server (if not already running)
go run ./cmd/server

# In a second terminal — run the load test
k6 run loadtest/bid_request.js
```

**PowerShell (Windows):**
```powershell
# Start the server
go run ./cmd/server

# In a second terminal
k6 run loadtest/bid_request.js
```

### Against the docker-compose stack

```bash
# Spin up the full stack (Postgres, Redis, Kafka, DSP simulators, SSP)
docker compose up -d

# Wait for health checks to pass
docker compose ps

# Run the load test (TARGET_URL defaults to http://localhost:8080)
k6 run loadtest/bid_request.js
```

### Against a remote / staging environment

**Unix / macOS / WSL:**
```bash
TARGET_URL=https://ssp-staging.example.com k6 run loadtest/bid_request.js
```

**PowerShell (Windows):**
```powershell
$env:TARGET_URL="https://ssp-staging.example.com"; k6 run loadtest/bid_request.js
```

### With first-price auctions enabled

**Unix / macOS / WSL:**
```bash
ENABLE_FIRST_PRICE=true go run ./cmd/server &
k6 run loadtest/bid_request.js
```

**PowerShell (Windows):**
```powershell
$env:ENABLE_FIRST_PRICE="true"; go run ./cmd/server &
k6 run loadtest/bid_request.js
# Clean up afterwards: Remove-Item Env:ENABLE_FIRST_PRICE
```

### With multi-impression mode enabled

The payload already sends **2 impression objects**.  
Enable the server-side flag to exercise the full multi-imp code path:

**Unix / macOS / WSL:**
```bash
ENABLE_MULTI_IMP=true go run ./cmd/server &
k6 run loadtest/bid_request.js
```

**PowerShell (Windows):**
```powershell
$env:ENABLE_MULTI_IMP="true"; go run ./cmd/server &
k6 run loadtest/bid_request.js
# Clean up afterwards: Remove-Item Env:ENABLE_MULTI_IMP
```

---

## Traffic Shape

| Phase | Duration | VUs |
|---|---|---|
| Ramp-up | 30 s | 0 → 500 |
| Sustained load | 60 s | 500 |
| Ramp-down | 30 s | 500 → 0 |

**Total wall-clock time:** ~2 minutes  
**Peak concurrency:** 500 virtual users

---

## Pass / Fail Thresholds

The test exits with a non-zero code (CI failure) if either threshold is breached:

| Metric | Threshold | Why |
|---|---|---|
| `http_req_duration` p95 | **< 100 ms** | OpenRTB SLA — the exchange must respond within the publisher's `tmax` |
| `http_req_failed` rate | **< 1 %** | More than 1 % non-2xx / non-204 indicates a systemic problem |

---

## Key Metrics to Watch

After the test completes, k6 prints a summary table and writes
`loadtest/results/latest.json`. The most important fields:

| Metric | Field in JSON | What it tells you |
|---|---|---|
| **p50 latency** | `http_req_duration.values.med` | Typical user experience |
| **p95 latency** | `http_req_duration.values['p(95)']` | SLA gate — must be < 100 ms |
| **p99 latency** | `http_req_duration.values['p(99)']` | Tail latency / worst-case outliers |
| **Throughput** | `http_reqs.values.rate` | Requests per second achieved |
| **Error rate** | `http_req_failed.values.rate` | Fraction of failed requests |
| **Check pass rate** | `checks.values.rate` | Inline per-request assertion pass rate |

### Reading the JSON results

```bash
# Pretty-print the summary after a run
cat loadtest/results/latest.json | python -m json.tool | less

# Extract key latency percentiles with jq
jq '{
  p50:  .metrics.http_req_duration.values.med,
  p95:  .metrics.http_req_duration.values["p(95)"],
  p99:  .metrics.http_req_duration.values["p(99)"],
  rps:  .metrics.http_reqs.values.rate,
  errors: .metrics.http_req_failed.values.rate
}' loadtest/results/latest.json
```

---

## Before / After Benchmarking

To compare the impact of a change:

```bash
# 1. Run baseline (e.g. on main branch)
git stash
k6 run loadtest/bid_request.js
cp loadtest/results/latest.json loadtest/results/baseline.json

# 2. Apply change and rebuild
git stash pop
go build ./...

# 3. Run again
k6 run loadtest/bid_request.js
cp loadtest/results/latest.json loadtest/results/after.json

# 4. Compare p95 latency
jq -r '"baseline p95: " + (.metrics.http_req_duration.values["p(95)"] | tostring) + " ms"' \
    loadtest/results/baseline.json
jq -r '"after    p95: " + (.metrics.http_req_duration.values["p(95)"] | tostring) + " ms"' \
    loadtest/results/after.json
```

---

## CI Integration (GitHub Actions example)

```yaml
- name: Start SSP server
  run: ENABLE_MULTI_IMP=true go run ./cmd/server &
  
- name: Wait for server
  run: |
    for i in $(seq 1 20); do
      curl -sf http://localhost:8080/health && break || sleep 1
    done

- name: k6 load test
  uses: grafana/k6-action@v0.3.1
  with:
    filename: loadtest/bid_request.js
  env:
    TARGET_URL: http://localhost:8080
```

The action returns exit code 1 if any threshold fails, blocking the merge.

---

## Files

```
loadtest/
├── bid_request.js          # k6 test script (edit me to adjust VUs / thresholds)
├── results/
│   ├── .gitkeep            # Keeps the directory in git
│   └── latest.json         # Written after each run (gitignored — add to .gitignore)
└── README.md               # This file
```

> **Tip:** Add `loadtest/results/latest.json` to `.gitignore` to avoid committing result
> files, but keep `results/.gitkeep` so the directory always exists for k6 to write into.
