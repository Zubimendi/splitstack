# SplitStack

An expense-splitting and debt-settlement engine — the Splitwise problem,
solved with the same discipline the rest of this portfolio applies to
harder-than-they-look correctness problems: a materialized balance cache
that's provably reconcilable against its own source of truth, two
different invariants enforced by two different mechanisms chosen for
what each one actually needs, and a debt-simplification algorithm that's
honest about being a good heuristic rather than dressed up as an exact
solver.

**Stack:** Go 1.22 · PostgreSQL 16 (pgx) · chi · Prometheus · zap ·
Docker.

**Companion projects:** week 4, project 7 of the same portfolio backend
roadmap as Dispatcher (Go, webhook delivery via a transactional outbox),
SlotForge (NestJS, correctness via a Postgres exclusion constraint),
FlagForge (NestJS, cache-only reads + real-time propagation), SearchCraft
(FastAPI, trigger-driven outbox + zero-downtime reindex), and — most
directly — LedgerLine (Go, double-entry bookkeeping), whose two ideas
SplitStack deliberately borrows one of and deliberately does *not*
borrow the other of, on purpose, for reasons explained in
[`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).

## Why this exists

Tracking who owes whom across a group sounds like CRUD with a computed
field. It isn't, for two reasons that only show up once you take
correctness seriously: **"who owes what right now" needs to be a fast
read**, which means it has to be cached, which means the cache and the
append-only history it's derived from can silently drift apart unless
something proves they haven't — and **minimizing the number of payments
needed to settle a group is a genuinely hard combinatorial problem**
(equivalent to a partition problem, NP-hard in general), which most
implementations either ignore (settle every pair individually, N² 
payments) or quietly claim to solve "optimally" without being honest
about what that word means at scale.

SplitStack's `group_balances` table is a materialized cache, not a
source of truth — the source of truth is the append-only
`expenses` / `expense_splits` / `settlements` tables, and a
`VerifiedBalances` read path recomputes net balances directly from that
history for anyone who needs certainty instead of speed, the same
cached-vs-verified split LedgerLine uses for account balances, applied
here to group-relative net balances. And `ComputeSettlementPlan` is
documented, in the code itself, as a fast, always-correct, well-known
greedy heuristic — not a certified-minimal solver — because pretending
otherwise would be the kind of claim this whole portfolio is built to
avoid making. See [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) for the
full reasoning.

## Repo layout

```
SplitStack/
├── cmd/api/                     # main.go — entrypoint, wiring, graceful shutdown
├── internal/
│   ├── config/                  # env-var configuration
│   ├── db/
│   │   └── migrations/          # hand-written SQL migrations
│   ├── ledger/                  # the core: models, split logic, settlement
│   │   │                        # planning, and the transactional engine
│   │   ├── models.go
│   │   ├── split.go             # even-split + validation logic
│   │   ├── settlement_plan.go   # greedy debt-simplification
│   │   ├── engine.go            # transactional writes, optimistic locking
│   │   └── errors.go            # sentinel errors
│   ├── api/                     # HTTP handlers, request/response types, router
│   └── observability/           # Prometheus metrics + zap logging
├── test/integration/            # DB-backed concurrency/idempotency tests
├── docs/
│   ├── PRD.md
│   ├── ARCHITECTURE.md
│   ├── CURSOR_CONTEXT.md
│   ├── STORY.md
│   └── TESTING.md
├── docker-compose.yml
├── Dockerfile
├── Makefile
└── go.mod
```

## Quickstart

```bash
cp .env.example .env
make up                    # Postgres via docker-compose
make migrate                # applies internal/db/migrations/*.sql
make seed                   # demo users, a group, a few expenses
make run                    # API on :8080
make test                   # unit tests — split.go, settlement_plan.go
make test-integration       # requires make up running
```

```bash
curl -X POST localhost:8080/groups/<id>/expenses \
  -H "Content-Type: application/json" \
  -d '{"description":"dinner","totalAmountCents":6000,"paidByUserId":"<uid>"}'
# no "splits" provided -> split evenly across every current group member

curl localhost:8080/groups/<id>/balances              # cached, fast
curl localhost:8080/groups/<id>/balances/verified      # recomputed from source, slower, certain
curl localhost:8080/groups/<id>/settlement-plan         # greedy minimal-ish payment plan
```

## Status

Core domain logic (models, splitting, debt-simplification, schema,
observability, HTTP handlers) is written; the transactional engine that
ties writes to the database, the router/entrypoint wiring, and the test
suite are in progress — see
[`docs/CURSOR_CONTEXT.md`](docs/CURSOR_CONTEXT.md) for exactly what's
done and what's next.

## License

MIT — see [`LICENSE`](LICENSE).
