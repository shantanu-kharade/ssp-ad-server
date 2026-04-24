# SSP Ad Server — Technical Documentation

> **What it is:** A Go-based Supply-Side Platform (SSP) ad server that receives OpenRTB 2.5 bid requests, runs a unified auction between internal campaigns and external DSPs, and returns bid responses.
>
> **What it is NOT:** A full ad stack. It lacks DSP bidding agents, a real ad exchange, campaign management UI, budget pacing enforcement, and production-grade analytics.

---

## Table of Contents

1. [Architecture Overview](#1-architecture-overview)
2. [Project Structure](#2-project-structure)
3. [Core Entities & Data Model](#3-core-entities--data-model)
4. [Ad Serving Flow](#4-ad-serving-flow)
5. [Feature Capability Map](#5-feature-capability-map)
6. [Data & Storage Layer](#6-data--storage-layer)
7. [API Endpoints](#7-api-endpoints)
8. [Middleware & Cross-Cutting Concerns](#8-middleware--cross-cutting-concerns)
9. [Resilience Patterns](#9-resilience-patterns)
10. [Observability](#10-observability)
11. [System Limitations](#11-system-limitations)
12. [Production Readiness Assessment](#12-production-readiness-assessment)
13. [Technology Stack Summary](#13-technology-stack-summary)

---

## 1. Architecture Overview

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         SSP Ad Server (Go / Fiber)                       │
│                                                                         │
│  ┌──────────────┐   ┌──────────────┐   ┌──────────────┐               │
│  │   /bid       │   │  /admin/*     │   │  /track/*    │               │
│  │  (OpenRTB)   │   │  (REST CRUD)  │   │  (Events)    │               │
│  └──────┬───────┘   └──────┬───────┘   └──────┬───────┘               │
│         │                  │                  │                        │
│  ┌──────▼───────┐   ┌──────▼───────┐   ┌──────▼───────┐             │
│  │ BidHandler   │   │CampaignHandler│  │ TrackHandler │             │
│  │              │   │ AdminHandler  │  │              │             │
│  └──────┬───────┘   └──────┬───────┘   └──────┬───────┘             │
│         │                  │                  │                        │
│  ┌──────▼──────────────────▼──────────────────▼───────┐              │
│  │                  Core Pipeline                      │              │
│  │  ┌────────────────┐      ┌────────────────────────┐│              │
│  │  │ Identity       │      │ DSP Fanout             ││              │
│  │  │ Resolver       │      │ (parallel HTTP calls)  ││              │
│  │  └───────┬────────┘      └───────────┬───────────┘│              │
│  │          │                            │            │              │
│  │  ┌───────▼─────────────────────────────▼────────┐ │              │
│  │  │           Auction Engine                       │ │              │
│  │  │  (Priority: PG > PMP > Open; Floor Enforce)   │ │              │
│  │  └─────────────────────────────────────────────────┘ │              │
│  └──────────────────────────────────────────────────────┘              │
│                                                                         │
└─────────────────────────────────────────────────────────────────────────┘
         │                        │                       │
    ┌────▼────┐             ┌────▼────┐            ┌────▼────┐
    │ Redis   │             │PostgreSQL│            │  Kafka  │
    │ (Cache) │             │ (Campaign)│           │ (Events)│
    └─────────┘             └──────────┘            └─────────┘
```

**Architecture Type:** Modular Monolith (single binary, single process, clearly separated packages)

---

## 2. Project Structure

```
ssp-ad-server/
├── cmd/server/main.go          # Entry point, wiring, graceful shutdown
├── internal/
│   ├── adserver/server.go      # Fiber app, routes, middleware registration
│   ├── handler/
│   │   ├── bid.go              # POST /bid — OpenRTB bid request handler
│   │   ├── bid_test.go         # Unit tests for bid handler
│   │   ├── campaign.go         # CRUD for campaigns/creatives
│   │   ├── admin.go            # Segment management, circuit breaker status
│   │   └── track.go            # Click tracking endpoint
│   ├── models/
│   │   └── openrtb.go          # OpenRTB 2.5 structs (BidRequest, BidResponse, etc.)
│   ├── campaign/
│   │   ├── models.go           # Campaign, Creative, TargetingRule domain models
│   │   ├── service.go          # Targeting evaluation logic
│   │   ├── repository.go       # PostgreSQL repository (interface + impl)
│   │   ├── cache.go            # Redis read-through cache decorator
│   │   └── repository_test.go  # Unit tests
│   ├── auction/
│   │   ├── models.go           # Auction Bid, AuctionResult, DealType enums
│   │   ├── engine.go          # Auction runner (second/first price)
│   │   ├── engine_test.go      # Unit tests for auction logic
│   │   ├── floor.go           # Floor price enforcement
│   │   └── deal.go            # Deal priority mapping
│   ├── dsp/
│   │   ├���─ config.go          # DSP endpoint config parsing
│   │   ├── client.go          # Single DSP HTTP client with connection pooling
│   │   ├── fanout.go          # Parallel DSP fanout coordinator
│   │   └── fanout_test.go     # Unit tests
│   ├── cache/
│   │   ├── redis.go           # Redis client wrapper
│   │   └── segments.go        # User segment fetcher (10ms timeout)
│   ├── identity/
│   │   ├── resolver.go        # User ID resolution chain
│   │   └── consent.go         # Consent validation (stub)
│   ├── events/
│   │   ├── types.go           # ImpressionEvent, ClickEvent
│   │   └── kafka_producer.go  # Kafka writer with retry logic
│   ├── middleware/
│   │   ├── rate_limit.go      # Redis-backed sliding window rate limiter
│   │   ├── auth.go            # API key validation for /admin
│   │   ├── logger.go          # Structured request logging
│   │   ├── recovery.go        # Panic recovery
│   │   └── metrics.go         # Metrics logging middleware
│   ├── resilience/
│   │   ├── rate_limiter.go    # Redis-backed distributed rate limiter
│   │   ├── circuit_breaker.go # DSP circuit breaker (gobreaker)
│   │   └── circuit_breaker_test.go
│   ├── metrics/
│   │   └── prometheus.go      # Prometheus metric definitions
│   ├── ads/
│   │   └── house_ad.go        # Fallback house ad provider
│   ├── config/
│   │   └── config.go          # Environment-based config loading
│   ├── db/
│   │   ├── postgres.go        # Connection pool + migration runner
│   │   └── migrations/        # SQL migrations
│   │       ├── 000001_create_campaigns.up.sql
│   │       └── 000002_add_bid_price_cpm.up.sql
│   └── server/
│       └── server.go
├── pkg/errors/errors.go      # Structured API error types
├── loadtest/                  # k6 load testing scripts
├── load_test/                 # Python load testing
├── post.lua                   # Redis Lua script (if any)
├── docker-compose.yml         # Full stack (SSP + Postgres + Redis + Kafka + OTEL)
├── Dockerfile
├── Makefile
├── go.mod / go.sum
└── .env.example
```

---

## 3. Core Entities & Data Model

### 3.1 Campaign

**File:** `internal/campaign/models.go`

| Field | Type | Description |
|-------|------|-------------|
| `ID` | `string` (UUID) | Primary key |
| `Name` | `string` | Display name |
| `AdvertiserID` | `string` (UUID) | Links to advertiser (stored as string, no foreign key enforced) |
| `Status` | `string` | `"active"`, `"paused"`, `"completed"` |
| `BudgetCents` | `int64` | Total campaign budget in cents |
| `SpentCents` | `int64` | Accumulated spend in cents |
| `BidPriceCPM` | `float64` | CPM bid price for auction participation |
| `StartDate` | `time.Time` | Campaign start |
| `EndDate` | `time.Time` | Campaign end |
| `CreatedAt` | `time.Time` | Record creation |
| `Creatives` | `[]Creative` | Associated ad creatives |
| `TargetingRules` | `[]TargetingRule` | Targeting criteria |

**Schema (PostgreSQL):**
```sql
campaigns(id, name, advertiser_id, status, budget_cents, spent_cents,
           bid_price_cpm, start_date, end_date, created_at)
-- Index: idx_campaigns_status ON (status)
```

**Used in:**
- `internal/campaign/service.go:24` — `GetActiveCampaigns()` fetches all `status = 'active'`
- `internal/handler/bid.go:286` — `EvaluateTargeting()` matches campaigns against bid request
- `internal/handler/campaign.go` — CRUD operations via REST API

---

### 3.2 Creative

**File:** `internal/campaign/models.go`

| Field | Type | Description |
|-------|------|-------------|
| `ID` | `string` (UUID) | Primary key |
| `CampaignID` | `string` (UUID) | FK to campaigns |
| `Format` | `string` | `"banner"`, `"video"`, `"native"` |
| `Width` | `int` | Creative width in pixels |
| `Height` | `int` | Creative height in pixels |
| `AdMarkup` | `string` | HTML/VAST content |
| `ClickURL` | `string` | Click-through URL |
| `Status` | `string` | `"active"`, etc. |
| `CreatedAt` | `time.Time` | Record creation |

**Schema:**
```sql
creatives(id, campaign_id, format, width, height, ad_markup, click_url, status, created_at)
-- Index: idx_creatives_campaign_id ON (campaign_id)
```

**Used in:**
- `internal/campaign/repository.go:105-123` — JOINed with campaigns via `ANY($1)` batch fetch
- `internal/handler/bid.go:334` — First creative selected for internal bid construction
- `internal/handler/bid.go:335-343` — Mapped to `auction.Bid` with `AdID = cr.ID`

**Limitation:** Only the first creative is used (`camp.Creatives[0]`). No multi-creative selection, A/B testing, or rotation.

---

### 3.3 TargetingRule

**File:** `internal/campaign/models.go`

| Field | Type | Description |
|-------|------|-------------|
| `ID` | `string` (UUID) | Primary key |
| `CampaignID` | `string` (UUID) | FK to campaigns |
| `RuleType` | `string` | `"geo"`, `"segment"`, `"device"` |
| `RuleValue` | `json.RawMessage` | JSON payload typed by RuleType |
| `CreatedAt` | `time.Time` | Record creation |

**Rule Types:**

| Type | Value Schema | Matcher |
|------|--------------|---------|
| `geo` | `{"ip_prefix": "192.168."}` | `strings.HasPrefix(device.ip, ip_prefix)` |
| `segment` | `{"segment_ids": ["seg-1", "seg-2"]}` | Any overlap with user's segments |
| `device` | `{"ua_substring": "iPhone"}` | Case-insensitive substring match on UA |

**Schema:**
```sql
targeting_rules(id, campaign_id, rule_type, rule_value JSONB, created_at)
-- Index: idx_targeting_rules_campaign_id ON (campaign_id)
```

**Used in:**
- `internal/campaign/service.go:49-83` — AND logic: all rules must match
- Fail-closed for unknown rule types (returns `false`)

**Limitations:**
- No date-based targeting (StartDate/EndDate checked at query level, not per-rule)
- No dayparting (time-of-day targeting)
- No frequency capping
- No browser/OS targeting beyond crude UA substring match
- Geo matching is primitive string prefix only (no MaxMind DB, no CIDR calculation)

---

### 3.4 Publisher / Ad Slot (Impression)

**File:** `internal/models/openrtb.go`

The system uses OpenRTB 2.5 structures for incoming requests:

```go
type BidRequest struct {
    ID       string       // Exchange-assigned request ID
    Imp      []Impression // 1-N ad slot opportunities
    Site     *Site        // Publisher website
    App      *App         // Mobile app (mutually exclusive with Site)
    Device   *Device      // User's device (IP, UA, Geo, OS, etc.)
    User     *User        // User object (ID, BuyerUID, Gender, YOB)
    Test     int          // 1 = test, 0 = live
    AuctionType int      // 1 = first price, 2 = second price
    TMax     int          // Max response time in ms
    Cur      []string     // Allowed currencies
}

type Impression struct {
    ID         string  // Impression ID (correlates bid response)
    Banner     *Banner // Banner ad slot dimensions
    BidFloor   float64 // Minimum CPM
    BidFloorCur string // Currency (default USD)
    Secure     *int    // HTTPS required
}

type Banner struct {
    W     int      // Width in pixels
    H     int      // Height in pixels
    Pos   int      // Position on page
    MIMEs []string // Supported MIME types
}
```

**Publisher extraction:**
- `internal/handler/bid.go:181-185` — Publisher ID extracted from `Site.Publisher.ID` or `App.Publisher.ID`
- Used for metrics labeling

**Limitations:**
- No real publisher management (Publisher table doesn't exist in schema)
- Publisher info is read-only from OpenRTB request, not persisted
- No site/app verification or allowlist

---

## 4. Ad Serving Flow

```
┌─────────────────────────────────────────────────────────────────────────┐
│                        Bid Request Lifecycle                           │
└─────────────────────────────────────────────────────────────────────────┘

1. HTTP Request Received
   POST /bid
   └─> Fiber timeout wrapper: 100ms hard deadline
       └─> Recovery middleware (panic catch)
           └─> Logger middleware (request start)
               └─> Rate limit middleware (10,000 RPS global)

2. Parse & Validate
   ├─> BodyParser into models.BidRequest
   ├─> OpenRTB struct tag validation (go-playground/validator)
   │   └─> 400 if invalid (missing ID, no impressions, etc.)
   └─> Consent check: X-Consent header must be non-empty
       └─> 200 + NBR=0 if missing consent

3. Parallel Pipeline (errgroup, cancellation propagated)
   │
   ├─[A] User Identity Resolution
   │   └─> Priority: X-User-ID header > uid cookie >
   │       BidRequest.User.ID > SHA256(IP) → "anon-xxxxxxxx"
   │
   ├─[B] Segment Fetch + Campaign Evaluation (sequential within goroutine)
   │   ├─> Redis: GET seg:{userID}  (10ms timeout)
   │   │   └─> []string userSegments
   │   └─> EvaluateTargeting()
   │       ├─> repo.GetActiveCampaigns() [Redis cache, 30s TTL]
   │       ├─> AND-match all TargetingRules per campaign
   │       └─> Return matched campaigns with creatives
   │
   └─[C] DSP Fanout (parallel with A/B)
       ├─> 50ms hard timeout on entire fanout
       ├─> For each DSP client:
       │   ├─> Circuit breaker check (5 requests half-open)
       │   ├─> HTTP POST OpenRTB BidRequest
       │   ├─> Read response (200 OK → parse SeatBid[], 204 → no bid)
       │   └─> Map to auction.Bid{ DealType: Open }
       └─> Collect all bids from all DSPs

4. Auction
   │
   ├─> Filter internal bids (skip if BidPriceCPM <= 0)
   ├─> Map campaigns to internal auction.Bid{ DealType: PG }
   ├─> Combine: external DSP bids + internal bids
   │
   ├─> For each impression (multi-imp if ENABLE_MULTI_IMP=true):
   │   ├─> Filter bids by ImpID (DSP bids)
   │   ├─> Add all internal bids (apply to all imps)
   │   │
   │   ├─> EnforceFloor():
   │   │   └─> Reject bids below BidFloor
   │   │
   │   ├─> Sort: highest Priority(DealType) first, then highest Price
   │   │   Priority: PG=3 > PMP=2 > Open=1
   │   │
   │   └─> RunAuction():
   │       ├─> If first-price enabled AND at=1: clearingPrice = winner.Price
   │       ├─> Else second-price: max(secondBid + $0.01, floor)
   │       └─> Return AuctionResult{Winner, ClearingPrice, ...}
   │
   └─> If no winner:
       └─> HouseAd fallback (if HOUSE_ADS_ENABLED=true)

5. Response Construction
   ├─> Build models.BidResponse
   │   └─> seatbid[].bid[].id, impid, price, adid, crid, w, h
   ├─> Fire NURL asynchronously (4-worker pool, 200ms timeout)
   │   └─> Substitute ${AUCTION_PRICE} macro
   └─> Publish ImpressionEvent to Kafka (16-worker pool, async queue)

6. Return
   └─> 200 OK with BidResponse JSON
       └─> OR 204 No Content if no bids at all
```

---

## 5. Feature Capability Map

### Ad Server Capabilities

| Feature | Status | Evidence |
|---------|--------|----------|
| OpenRTB 2.5 bid request parsing | ✅ Present | `internal/models/openrtb.go` — full subset of spec |
| OpenRTB 2.5 bid response generation | ✅ Present | `internal/models/openrtb.go:133-174` |
| Multi-impression support | ⚠️ Partial | Feature flag `ENABLE_MULTI_IMP`, off by default (`bid.go:345-349`) |
| First/second price auction | ✅ Present | `internal/auction/engine.go:67-88`, `ENABLE_FIRST_PRICE` env var |
| Floor price enforcement | ✅ Present | `internal/auction/floor.go` — `EnforceFloor()` |
| House ad fallback | ✅ Present | `internal/ads/house_ad.go`, env-configured |
| Win notice (NURL) firing | ✅ Present | `internal/handler/bid.go:104-143`, worker pool |
| Click tracking | ✅ Present | `internal/handler/track.go` — `/track/click` → Kafka |

### SSP Capabilities

| Feature | Status | Evidence |
|---------|--------|----------|
| Campaign management (CRUD) | ✅ Present | `internal/handler/campaign.go`, `GET/POST/PATCH /admin/campaigns` |
| Audience segment targeting | ⚠️ Partial | Redis-stored segments, geo/segment/device rules, no DMP integration |
| User-level targeting | ⚠️ Partial | Only 3 rule types; no behavioral, contextual, retargeting |
| Deal types (PG/PMP/Open) | ✅ Present | `internal/auction/deal.go` — priority 3/2/1 |
| Private Marketplace (PMP) | ⚠️ Partial | Deal type enum exists, but no PMP deal ID lookup or preferential pricing |
| Programmatic Guaranteed (PG) | ⚠️ Partial | Deal type prioritized, but no guaranteed fill logic or rate guarantees |

### DSP Capabilities

| Feature | Status | Evidence |
|---------|--------|----------|
| DSP fanout | ✅ Present | `internal/dsp/fanout.go` — parallel HTTP with 50ms timeout |
| DSP client with connection pooling | ✅ Present | `internal/dsp/client.go:55-85` — tuned transport |
| DSP response parsing | ✅ Present | `internal/dsp/client.go:183-202` |
| DSP circuit breaker | ✅ Present | `internal/resilience/circuit_breaker.go` — gobreaker |
| DSP weight/priority configuration | ❌ Missing | Config has `weight` field but fanout uses equal parallelism (no weighted round-robin) |

### Ad Exchange Behavior

| Feature | Status | Evidence |
|---------|--------|----------|
| Unified auction across DSPs + internal | ✅ Present | `internal/auction/engine.go` — all bids in single sort |
| Open auction support | ✅ Present | `auction.Bid{DealType: Open}` for external DSPs |
| Bid request routing to DSPs | ✅ Present | JSON POST with OpenRTB 2.5 body |
| Bid response aggregation | ✅ Present | `SeatBid` grouped by DSP, all returned to caller |

### Targeting

| Feature | Status | Evidence |
|---------|--------|----------|
| Geo targeting (IP prefix) | ⚠️ Partial | `service.go:85-90` — only string prefix match, no GeoIP DB |
| Device targeting (UA substring) | ⚠️ Partial | `service.go:107-111` — naive string Contains |
| Segment targeting | ⚠️ Partial | `service.go:92-105` — list intersection, no segment taxonomy |
| Date-based scheduling | ⚠️ Partial | StartDate/EndDate checked in SQL query only |
| Dayparting | ❌ Missing | No time-of-day targeting |
| Frequency capping | ❌ Missing | No impression frequency cap tracking |
| Viewability targeting | ❌ Missing | No viewability metrics in request/response |
| Brand safety / content category | ❌ Missing | No IAB category filtering |

### Budgeting & Pacing

| Feature | Status | Evidence |
|---------|--------|----------|
| Campaign budget tracking | ⚠️ Partial | `SpentCents` field exists but NEVER updated in code |
| Budget enforcement | ❌ Missing | `EvaluateTargeting()` does NOT check remaining budget |
| Daily pacing | ❌ Missing | No daily spend caps |
| Rate pacing | ❌ Missing | No impressions-per-second limits |

### Reporting & Analytics

| Feature | Status | Evidence |
|---------|--------|----------|
| Impression events to Kafka | ✅ Present | `internal/events/kafka_producer.go` |
| Click events to Kafka | ✅ Present | `internal/events/types.go` — `ClickEvent` |
| Prometheus metrics | ⚠️ Partial | `internal/metrics/prometheus.go` — 6 metrics defined |
| OpenTelemetry tracing | ⚠️ Partial | Tracer initialized, spans created but not fully propagated |
| Real-time dashboards | ❌ Missing | No Grafana/Superset integration |
| Revenue reporting | ❌ Missing | No win-rate, CPM, spend aggregations |

---

## 6. Data & Storage Layer

### 6.1 PostgreSQL

**Connection Pool:**
- `internal/db/postgres.go:23-25`
- MaxConns: 150, MinConns: 10, MaxConnIdleTime: 30min

**Schema:**

```sql
-- campaigns: primary campaign records
campaigns(id UUID PK, name, advertiser_id UUID, status TEXT,
          budget_cents BIGINT, spent_cents BIGINT,
          bid_price_cpm NUMERIC(10,4), start_date TIMESTAMPTZ,
          end_date TIMESTAMPTZ, created_at TIMESTAMPTZ)
-- Index: idx_campaigns_status ON (status)

-- creatives: ad creative assets
creatives(id UUID PK, campaign_id UUID FK→campaigns(id),
          format, width, height, ad_markup, click_url, status, created_at)
-- Index: idx_creatives_campaign_id ON (campaign_id)

-- targeting_rules: campaign targeting criteria
targeting_rules(id UUID PK, campaign_id UUID FK→campaigns(id),
                rule_type TEXT, rule_value JSONB, created_at TIMESTAMPTZ)
-- Index: idx_targeting_rules_campaign_id ON (campaign_id)
```

**Observations:**
- No indexes on `campaigns.start_date`, `end_date` (date range queries not indexed)
- No composite index on `(campaign_id, rule_type)` for targeting evaluation
- No indexes on `creatives.status` (active creative filtering)
- `advertiser_id` has no index and no foreign key
- `spent_cents` is never updated (dead column at DB level, though it exists in schema)

### 6.2 Redis

**Usage:**
- Campaign list cache: `campaigns:active` (JSON, 30s TTL)
- Individual campaign cache: `campaign:{id}` (JSON, 60s TTL)
- User segments: `seg:{userID}` (JSON array, 300s TTL)
- Rate limiting: `ratelimit:{client}:{bucket}` (sliding window)

**Pool Configuration:**
- `internal/cache/redis.go:45-46`: PoolSize: 1000, MinIdleConns: 100

**Observations:**
- No TTL jitter (thundering herd on cache expiry)
- No cache-aside write pattern (cache invalidation on updates is present but not atomic)
- Segment fetcher uses 10ms timeout (aggressive — may fail under Redis load)

### 6.3 Kafka

**Topics:**
- `impression_events` — won impressions with win price, geo, device
- `click_events` — click tracking events

**Writer Config:**
- Sync writer (Async: false) with custom retry loop (3 attempts, 100ms backoff)
- Batch size: 100, Batch timeout: 10ms
- Key: RequestID (for partition consistency)

**Observations:**
- No dead-letter queue for failed events
- Events are fire-and-forget from the bid handler (queue with backpressure drop if full)
- No event schema registry (avro/protobuf)
- No Kafka consumer in this codebase (events consumed by downstream analytics system)

---

## 7. API Endpoints

### 7.1 Bidding API

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| POST | `/bid` | None | OpenRTB 2.5 bid request endpoint. Returns BidResponse. |
| GET | `/health` | None | Health check (200 OK). Checks Kafka connectivity. |

### 7.2 Tracking API

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/track/click` | None | Click tracking. Publishes ClickEvent to Kafka. Query params: bid_id, imp_id, campaign_id, creative_id, user_id. |

### 7.3 Admin API (requires `X-API-Key` header)

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/admin/campaigns` | API Key | List all active campaigns with creatives and targeting rules |
| POST | `/admin/campaigns` | API Key | Create a new campaign |
| GET | `/admin/campaigns/:id` | API Key | Get campaign by ID |
| PATCH | `/admin/campaigns/:id` | API Key | Update campaign status/budget |
| POST | `/admin/campaigns/:id/creatives` | API Key | Add a creative to a campaign |
| POST | `/admin/segments` | API Key | Set user segments in Redis |
| GET | `/admin/circuit-breakers` | API Key | Get DSP circuit breaker states |

### 7.4 Observability

| Method | Path | Description |
|--------|------|-------------|
| GET | `/metrics` | Prometheus metrics (text format) |

### 7.5 API Inconsistencies / Gaps

- **No update for spent_cents**: PATCH `/admin/campaigns/:id` updates only `status` and `budget_cents`, never `spent_cents`
- **No delete endpoint**: No campaign deletion (soft or hard)
- **No bulk operations**: No batch campaign creation/update
- **No creative update/delete**: Only `AddCreative`, no `UpdateCreative` or `DeleteCreative`
- **No campaign pause with auto-resume**: Manual status changes only
- **No OpenRTB extension fields preserved**: `BidRequest.Ext` and `Bid.Ext` are not modeled
- **No `nbr` (No Bid Reason) populated**: Handler returns NBR=0 but doesn't map real reasons

---

## 8. Middleware & Cross-Cutting Concerns

### Middleware Stack (in order)

1. **Recovery** (`internal/middleware/recovery.go`) — panic recovery, returns 500 JSON
2. **RequestID** (Fiber built-in) — generates/forwards X-Request-ID
3. **Logger** (`internal/middleware/logger.go`) — structured JSON request logging
4. **Metrics** (`internal/middleware/metrics.go`) — per-request latency logging
5. **RateLimit** (`internal/middleware/rate_limit.go`) — Redis-backed sliding window, 10,000 RPS

### Custom Error Handling

`internal/server/server.go:144-167` — Custom Fiber error handler returns consistent JSON:
```json
{"type": "INTERNAL_ERROR", "status_code": 500, "message": "..."}
```

Structured error types in `internal/pkg/errors/`:
- `NewValidationError` (400)
- `NewBadRequestError` (400)
- `NewInternalError` (500)
- `NewTimeoutError` (504)
- `NewUnauthorizedError` (401)
- `NewNotFoundError` (404)

---

## 9. Resilience Patterns

### 9.1 Circuit Breaker

**File:** `internal/resilience/circuit_breaker.go`

- Library: `github.com/sony/gobreaker`
- Config: 5 max requests half-open, 60s interval, trips at 50% failure rate after 10 requests
- Applied per DSP client
- State changes logged as warnings
- Admin endpoint: `GET /admin/circuit-breakers` returns state per DSP

### 9.2 Rate Limiter

**File:** `internal/resilience/rate_limiter.go`

- Algorithm: Redis INCR + EXPIRE (sliding window, 60s buckets)
- Scope: Global (all pods share same Redis counter)
- Limit: 10,000 RPS (converted to 600,000 RPM)
- Client key: IP address from context
- Fail-open: if Redis is unavailable, requests are allowed

### 9.3 DSP Fanout Timeout

**File:** `internal/dsp/fanout.go:43`

- Hard 50ms timeout for entire DSP fanout operation
- Per-DSP HTTP client timeout: configurable via `DSP_ENDPOINTS[].timeout_ms` (default 50ms)
- Non-blocking NURL firing: 4-worker pool, 200ms timeout per win notice

### 9.4 Async Event Processing

**File:** `internal/handler/bid.go:78-88`

- Impression events: 16-worker pool, queue capacity 10,000
- NURL win notices: 4-worker pool, queue capacity 1,000
- Both use bounded queues with drop-on-full behavior (not blocking)

### 9.5 Graceful Degradation

The bid handler never returns a hard error to the caller for soft failures:
- Redis cache miss → continue without segments
- Campaign evaluation failure → continue with empty matched campaigns
- DSP fanout failure → continue with internal bids only
- Kafka publish failure → drop event (logged as error)

---

## 10. Observability

### 10.1 Structured Logging

- Library: `uber-go/zap`
- Format: JSON
- Fields include: request_id, publisher_id, user_id, latency, status, auction_id
- Sampled logging: 1% of successful bid responses (`bid.go:457`)

### 10.2 Prometheus Metrics

**File:** `internal/metrics/prometheus.go`

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `ssp_bid_requests_total` | Counter | publisher_id, status | Total bid requests |
| `ssp_bid_latency_seconds` | Histogram | — | Bid request latency |
| `ssp_dsp_bid_latency_seconds` | Histogram | dsp_id | Per-DSP latency |
| `ssp_auction_clearing_price` | Histogram | — | Win prices in CPM |
| `ssp_active_goroutines` | Gauge | — | Impression worker queue depth |
| `ssp_auction_type_total` | Counter | type | First/second price count |

### 10.3 OpenTelemetry Tracing

- Initialized in `cmd/server/main.go:145-181`
- OTLP exporter to `localhost:4317` (configurable via `OTEL_EXPORTER_OTLP_ENDPOINT`)
- Spans: `bid.handle`, `pipeline.fetch_segments`, `pipeline.evaluate_campaigns`, `pipeline.dsp_fanout`, `auction.run`, `dsp.bid`
- Propagators: TraceContext + Baggage

### 10.4 Health Checks

`GET /health` returns:
```json
{"status": "ok", "version": "1.0.0", "kafka": "ok|unavailable"}
```

---

## 11. System Limitations

### Critical Gaps

1. **No Budget Enforcement** — `EvaluateTargeting()` never checks `SpentCents` or `BudgetCents`. A campaign can spend infinitely beyond its budget.

2. **No Frequency Capping** — No mechanism to track or limit impressions per user.

3. **No Pacing** — Campaigns participate in every eligible auction. No rate limiting or daily caps.

4. **spent_cents Never Updated** — The field exists in schema but zero writes to it exist in the codebase. Revenue accounting is non-functional.

5. **Single Creative Per Campaign** — Only `camp.Creatives[0]` is ever selected (`bid.go:334`). No rotation, A/B testing, or fallback creatives.

6. **No Deal ID Resolution** — All DSP bids are mapped to `DealType: Open` with `DealID: "open-deal"`. PMP and PG deals have no preferential processing.

7. **No Publisher Management** — No Publisher table, no seat/publisher allowlist, no publisher-level floor prices.

### Performance Concerns

8. **EvaluateTargeting is O(n campaigns)** — Every bid request iterates ALL active campaigns. At scale, this requires indexing or pre-filtering by targeting criteria.

9. **No Pre-computed Targeting Index** — Campaigns are not indexed by rule type. A geo-targeted campaign still requires full scan.

10. **Redis Cache Stampede Risk** — `campaigns:active` cache has no TTL jitter. All pods refresh at the same time.

11. **Segment Fetch is 10ms Timeout** — Aggressive timeout may cause unnecessary cache misses under load.

12. **Multi-imp Auction Runs Sequentially** — `bid.go:353` — each impression is processed in a for-loop, not parallelized.

### Functional Gaps

13. **No Video/VAST Support** — Only `Banner` impressions are modeled. No video, native, or audio.

14. **No Viewability Targeting** — No measurement provider integration, no viewability scores.

15. **No Brand Safety** — No IAB category blocking, no domain/URL filtering.

16. **No Dayparting** — No time-of-day or day-of-week targeting.

17. **No GDPR/Privacy Enforcement** — Consent check is a stub (`consent.go:18-21`). No TC string parsing, no purpose/consentLegal basis mapping.

18. **No OpenRTB Extensions** — `Ext` fields exist but are not populated or propagated.

19. **No DSP Weighting** — All DSPs receive equal fanout; `weight` config field is ignored.

20. **No Real-time Bidding (RTB) Macros** — Only `${AUCTION_PRICE}` is substituted in NURLs. Missing: `${AUCTION_ID}`, `${AUCTION_BID_ID}`, `${AUCTION_IMP_ID}`, `${AUCTION_SEAT_ID}`.

21. **No Ad Markup Selection** — DSP-returned `adm` (ad markup) is NOT included in bid response (`bid.go:386-394`). The server constructs a BidResponse without the actual ad content from DSPs — DSP creatives are never served.

22. **No Creative Selection Optimization** — No size matching against impression dimensions, no format compatibility check.

---

## 12. Production Readiness Assessment

### What Is Production-Ready

| Concern | Status | Notes |
|---------|--------|-------|
| Structured logging | ✅ | Zap JSON, request correlation |
| Prometheus metrics | ✅ | 6 metrics, labeled appropriately |
| Distributed rate limiting | ✅ | Redis-backed, fail-open |
| Circuit breakers | ✅ | Per-DSP, with admin visibility |
| Panic recovery | ✅ | Middleware catches all panics |
| Graceful shutdown | ✅ | Signal handling, deferred closes |
| Database connection pooling | ✅ | 150 max conns, health checks |
| Redis connection pooling | ✅ | 1000 pool size |
| DSP connection pooling | ✅ | 100 idle per host, keep-alive tuned |
| Async event processing | ✅ | Worker pools with bounded queues |
| OpenTelemetry tracing | ⚠️ Partial | Initialized but not end-to-end |
| API key auth | ✅ | For /admin endpoints |
| Request timeouts | ✅ | Fiber timeout 100ms, Redis 10ms, DSP 50ms |
| Docker Compose stack | ✅ | Full dev environment |

### What Is NOT Production-Ready

| Concern | Status | Risk |
|---------|--------|------|
| No budget enforcement | ❌ | **Financial loss** — campaigns overspend |
| No spend tracking | ❌ | **Billing failure** — spent_cents is never written |
| No pacing/rate limiting | ❌ | **Resource exhaustion** — campaigns hammer the system |
| No frequency capping | ❌ | **User experience** — same user bombarded |
| No publisher management | ❌ | **Revenue leakage** — no seat verification |
| No consent/GDPR enforcement | ❌ | **Legal risk** — consent is stub |
| No ad markup from DSPs | ❌ | **Core functionality broken** — DSP creatives are discarded |
| No DSP weighting | ❌ | **Revenue optimization gap** — premium DSPs get same treatment |
| No video/native support | ❌ | **Market coverage gap** — majority of modern inventory unsupported |
| No real-time dashboards | ❌ | **Operational blindness** — no revenue, win-rate, spend visibility |
| No load testing at scale | ⚠️ | Load test scripts exist but not validated |

### Security Gaps

- API key is a static string in environment (no rotation mechanism)
- No request signing or content integrity verification
- No CSP headers or click-jacking protection on tracking pixels
- No rate limiting per-publisher (only global limit)
- No input sanitization on ad markup (XSS risk on creative rendering)

---

## 13. Technology Stack Summary

| Layer | Technology | Version |
|-------|------------|---------|
| Language | Go | 1.25 |
| HTTP Framework | GoFiber | v2.52.5 |
| Database | PostgreSQL | 16 |
| Cache | Redis | 7 |
| Message Queue | Apache Kafka | 7.5 |
| Metrics | Prometheus client_golang | 1.23.2 |
| Tracing | OpenTelemetry | 1.43.0 |
| Logging | Uber Zap | 1.27.0 |
| Rate Limiting | Redis + custom | — |
| Circuit Breaker | Sony gobreaker | 1.0.0 |
| Validation | go-playground/validator | v10.22.1 |
| Config | Kelseyhightower/envconfig | v1.4.0 |
| HTTP Client | stdlib | — |
| Kafka Client | segmentio/kafka-go | v0.4.50 |
| Redis Client | go-redis/v9 | v9.18.0 |
| DB Driver | jackc/pgx/v5 | v5.9.1 |
| Migrations | golang-migrate | v4.19.1 |
| ID Generation | google/uuid | v1.6.0 |

---

## Summary: What This System Is Today

This is a **functional SSP bid endpoint** that demonstrates the core real-time bidding pipeline:

1. **Receives OpenRTB 2.5 bid requests** ✅
2. **Fetches internal campaigns with targeting** ✅
3. **Fans out to external DSPs in parallel** ✅
4. **Runs a unified second-price auction** ✅
5. **Returns bid responses** ✅
6. **Fires win notices and tracks events to Kafka** ✅

### What it is missing to be production-grade:

- **Budget & pacing enforcement** (core financial integrity)
- **Spend tracking** (billing)
- **Frequency capping** (user experience, legal)
- **DSP creative markup propagation** (creative serving is broken for external DSPs)
- **Real-time analytics/reporting** (operational visibility)
- **Video/native ad support** (market coverage)
- **GDPR consent enforcement** (legal compliance)
- **Publisher management** (seat verification, floor management)

> **Verdict:** This is a well-structured educational/reference SSP implementation. It demonstrates correct patterns for auction engines, DSP integration, resilience, and observability. However, it requires significant work in budget enforcement, reporting, and compliance before it can serve production advertising traffic.