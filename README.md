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

## Run it

```bash
go test -race ./...                       # 22 unit tests, no dependencies
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
curl localhost:8080/available/SKU-1                 # read model, fed by Kafka
```

## What I verified

- 50 goroutines reserving from 10 units never oversell (`go test -race`).
- With Kafka stopped, writes still succeed and pile up in the outbox; after Kafka
  restarts, the relay drains them and the read model catches up to the same version.
- Replaying the event history rebuilds exactly the live state.

## CI

GitHub Actions and a Jenkinsfile run the same stages: `go vet`, unit tests
with the race detector, MySQL integration tests, and a Docker image build.

## Trade-offs I'd revisit

- Replaying a long stream gets slow; snapshots every N events would fix it.
- The read model is in memory, so it rebuilds from Kafka on restart. A real one
  would live in Redis or a table with the last applied version per SKU.
- The relay can publish a batch twice if it crashes between publish and mark.
  That's acceptable only because consumers are idempotent.
