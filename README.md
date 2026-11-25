# StockLedger

An event-sourced inventory service in Go. It keeps no "stock" column. Every
change is stored as an event (`StockReceived`, `StockReserved`,
`ReservationReleased`, `ReservationCommitted`), and the current stock is rebuilt
by replaying those events. Events reach other services through Kafka via a
transactional outbox.

It is a focused rewrite of the inventory-locking flow from
[GrainMart](https://github.com/Yang-Zhang1994) (Java/Spring, RabbitMQ), done to
learn Go, Kafka and event sourcing on a problem I already understood.

```
HTTP command ──► Service ──► StockItem aggregate (decide) ──► MySQL events table
                   ▲  retry on version conflict                │ published_at IS NULL
                   └───────────────────────────────────────────┘ = outbox
                                                               ▼
GET /available/{sku} ◄── projection (read model) ◄── Kafka ◄── relay (every 500 ms)
```

## Design decisions

| Problem | Decision | Where |
|---|---|---|
| Two requests reserve the last unit at once | Optimistic concurrency: `UNIQUE (sku, version)`; the loser reloads and retries | `eventstore/mysql.go`, `inventory/service.go` |
| Business rules scattered across handlers | Domain-Driven Design aggregate: all rules live in `StockItem`, the only place state changes is `Apply` | `inventory/item.go` |
| Event saved but Kafka publish lost (or vice versa) | Transactional outbox: the event row *is* the outbox entry; a relay publishes and then stamps `published_at` | `relay/relay.go` |
| Kafka delivers at least once | Consumers skip any event at or below the version they already applied | `projection/available.go` |
| Events for one SKU arriving out of order | Messages keyed by SKU, so one SKU always maps to one partition; a gap is reported, not silently applied | `relay/kafka.go` |
| Client retries a reservation | Same order + same qty is a no-op; a different qty is rejected | `inventory/item.go` |
| Someone edits the events table directly | Hash chain per SKU: each event stores SHA-256 over its fields and the previous event's hash; `GET /skus/{sku}/verify` recomputes it | `eventstore/chain.go` |

## Run it

```bash
go test -race ./...                       # unit tests, no dependencies
go run ./cmd/stockledger -mem             # in-memory store, no Kafka

docker compose up -d --wait               # MySQL 8.4 on :3307, Kafka 3.8 (KRaft) on :9092
go test -tags integration ./internal/eventstore/...
go run ./cmd/stockledger
```

```bash
curl -X POST localhost:8080/skus/SKU-1/receive      -d '{"qty":5}'
curl -X POST localhost:8080/skus/SKU-1/reservations -d '{"orderId":"o1","qty":3}'
curl -X POST localhost:8080/skus/SKU-1/reservations/o1/commit
curl localhost:8080/skus/SKU-1/events               # full history
curl localhost:8080/skus/SKU-1/verify               # recompute the hash chain
curl localhost:8080/available/SKU-1                 # read model, fed by Kafka
```

## What I verified

- 50 goroutines reserving from 10 units never oversell (`go test -race`).
- With Kafka stopped, writes still succeed and pile up in the outbox; after Kafka
  restarts, the relay drains them and the read model catches up to the same version.
- Replaying the event history rebuilds exactly the live state.
- Editing, deleting or reordering any stored event is caught by `verify`, which
  names the first bad version (unit tests cover each case; an integration test
  edits a row in MySQL directly).

## Tamper detection

The events table is the only record of what happened to stock, so a changed
row would silently change history. Each SKU's events form a hash chain: an
event's hash covers its own fields and the hash before it, so a change to any
event breaks every link after it.

```bash
curl localhost:8080/skus/DEMO-1/verify
{"sku":"DEMO-1","events":3,"ok":true,"head":"b81b7cb2…"}

# lower a reservation behind the service's back
mysql> UPDATE events SET qty = 1 WHERE sku = 'DEMO-1' AND version = 2;

curl localhost:8080/skus/DEMO-1/verify
{"sku":"DEMO-1","events":3,"ok":false,"brokenAt":2,"reason":"hash mismatch (event edited, or an earlier event changed)"}
```

The chain is per SKU, like the optimistic lock, so writes to different SKUs
never wait on each other. A chain on its own cannot catch two things: deleting
the newest events, or rewriting an event and recomputing every hash after it.
Both change the head hash, which `verify` returns so it can be recorded
somewhere the database can't touch.

## Load test (JMeter)

`perf/run-jmeter.sh` starts the service, stocks 100 units, then runs
`perf/reserve-contention.jmx` in Docker: 50 threads × 10 reservations against the
same SKU. Every response must be 200 (reserved) or 409 (sold out), and the number
of 200s must equal the units that left stock.

```
requests=500 reserved=100 sold_out=400 assertion_failures=0 p95_ms=298
stock=100 available_after=0
PASS: no oversell under 50 concurrent clients
```

## Replenishment assistant (`replenish/`)

A Python planner on top of the ledger: it forecasts each part's demand, works
out this week's order, and answers questions about it in plain English.
StockLedger supplies what is on the shelf right now; the sales history supplies
demand. The history is **simulated**: 12 rail spare parts over 34 months, with
steady, weekday-heavy, winter, summer, growing and intermittent demand.

- **Forecast:** a gradient-boosted model (scikit-learn) trained across all parts
  predicts the next lead time + 1 week of demand as a ratio to each part's recent
  level, using last year's same window and how demand moved then. Every feature
  uses only days before the forecast date; a test changes later days and checks
  nothing moves.
- **Policy:** weekly order-up-to with safety stock sized from the forecast's own
  out-of-sample errors, rounded up to the supplier's case pack.
- **Assistant:** Claude answers through two read-only tools that return numbers
  computed in code. It can't order anything or do its own arithmetic.

Held out May–Oct 2026 and replayed day by day ([full results](replenish/results/backtest.md)):

| | 28-day moving average | Model |
|---|---|---|
| Forecast error (WAPE) | 17.1% | **11.8%** |
| Stock needed for a 99% fill rate | 1,063 units | **878 (−17%)** |
| Stock needed for a 99.5% fill rate | 1,406 units | **1,005 (−29%)** |

The first version over-forecast winter parts in summer by 20–30% and needed more
stock than the baseline at high service levels. Its safety stock came from a model
fit on too little history to see a year earlier; a longer history and a feature for
how demand moved at this time last year fixed both. Since the same person wrote the
simulator and the model, real data would be the real test.

```bash
cd replenish && python -m venv .venv && .venv/bin/pip install -e ".[dev]"
.venv/bin/pytest -q
.venv/bin/python -m replenish backtest                 # writes results/backtest.md
go run ../cmd/stockledger -mem &                       # a live ledger
.venv/bin/python -m replenish seed && .venv/bin/python -m replenish plan
ANTHROPIC_API_KEY=... .venv/bin/python -m replenish ask "What should I reorder this week, and why?"
```

## CI

GitHub Actions and a Jenkinsfile run the same stages: `go vet`, unit tests
with the race detector, MySQL integration tests, and a Docker image build.
GitHub Actions also runs the JMeter contention test.

## Trade-offs I'd revisit

- Replaying a long stream gets slow; snapshots every N events would fix it.
- The read model is in memory, so it rebuilds from Kafka on restart. A real one
  would live in Redis or a table with the last applied version per SKU.
- Publishing the head hash with each Kafka event would anchor the chain outside
  MySQL without a separate store.
- The relay can publish a batch twice if it crashes between publish and mark.
  That's acceptable only because consumers are idempotent.
