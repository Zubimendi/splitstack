# Agent context: resuming work on SplitStack

Read this file first, then `docs/ARCHITECTURE.md`, before changing
anything. If you're an AI coding agent (or a future version of the person
building this) picking this up in a fresh session, this is what you need
to not re-derive — or accidentally undo — decisions that were made
deliberately.

## Project identity

- **Name:** SplitStack — an expense-splitting and debt-settlement engine.
- **Stack:** Go 1.22, PostgreSQL 16 (via `pgx/v5`), `go-chi/chi` for
  routing, Prometheus (`client_golang`) for metrics, `zap` for structured
  logging, Docker Compose for local infra.
- **Purpose:** week 4, project 7 of a portfolio backend roadmap. Solves
  the Splitwise problem — tracking who owes whom across a group and
  minimizing the number of payments needed to settle up — with the same
  design discipline as the rest of the portfolio: a materialized cache
  that's provably reconcilable against its source of truth, and an
  honestly-scoped algorithm rather than an overclaimed one. See
  `docs/PRD.md` for the full brief, `docs/ARCHITECTURE.md` for the
  principle→code mapping.
- **Companion projects:** same lineage as Dispatcher, SlotForge,
  FlagForge, and SearchCraft — and most directly, **LedgerLine** (Go,
  double-entry bookkeeping), whose cached-vs-verified balance split and
  deferred-constraint-trigger pattern this project deliberately reuses
  for *one* invariant and deliberately does *not* reuse for a different,
  weaker invariant. **Do not "simplify" this project by applying
  LedgerLine's deferred trigger to the group-wide zero-sum balance
  invariant** — re-read `docs/ARCHITECTURE.md` §2 first; that invariant
  needs `VerifiedBalances`-style recomputation, not a trigger, and the
  distinction is the actual point being demonstrated here.

## Current state (as of this handoff)

### Done
- `go.mod` — chi, uuid, pgx/v5, prometheus client, zap.
- Schema (initial migration content, not yet split into its own file in
  `internal/db/migrations/` — see "NOT done" below): `users`, `groups`,
  `group_members`, `group_balances` (the materialized cache, with
  `version` for optimistic locking), `expenses` (with a nullable,
  partially-uniquely-indexed `idempotency_key`), `expense_splits` (with
  the deferred-constraint-trigger sum check), `settlements` (same
  idempotency-key pattern, plus a `CHECK (from_user_id <> to_user_id)`).
- `internal/config/config.go` — env-var configuration
  (`DATABASE_URL`, `HTTP_PORT`) with sane local defaults.
- `internal/db/db.go` — `pgxpool` connection setup (`MaxConns: 20,
  MinConns: 2`), with a startup `Ping`.
- `internal/ledger/models.go` — domain types: `User`, `Group`,
  `SplitInput`, `AddExpenseInput`, `Expense`, `RecordSettlementInput`,
  `Settlement`, `Balance`, `SuggestedPayment`.
- `internal/ledger/split.go` — `splitEvenly` (deterministic remainder
  allocation via sorted user IDs) and `validateSplits` (membership +
  positive-amount + sum-equals-total checks).
- `internal/ledger/settlement_plan.go` — `ComputeSettlementPlan`, the
  greedy debt-simplification heuristic, with an explicit top-of-file
  comment documenting the NP-hardness of the true-minimum version and
  why greedy is the deliberate choice here.
- `internal/observability/observability.go` — Prometheus counters
  (`ExpensesAdded`, `SettlementsRecorded`, `ConcurrentUpdateConflicts`),
  a `zap` production logger constructor, and a `promhttp` metrics
  handler.
- `internal/api/handlers.go` — all HTTP handlers
  (`CreateUser`, `CreateGroup`, `AddExpense`, `RecordSettlement`,
  `GetBalances`, `GetVerifiedBalances`, `GetSettlementPlan`), idempotency-
  aware status codes (`200` on a replayed key vs `201` on a new record),
  and `respondExpenseError`'s sentinel-error-to-HTTP-status mapping.
- Docs: `README.md`, `docs/PRD.md`, `docs/ARCHITECTURE.md`,
  `docs/TESTING.md`, `docs/STORY.md`, this file.

### NOT done — this is the actual next work, not a formality to skim

- **`internal/ledger/engine.go` does not exist yet.** This is the single
  most important file left to write — `internal/api/handlers.go` already
  calls `h.engine.CreateUser(...)`, `h.engine.AddExpense(...)`,
  `h.engine.RecordSettlement(...)`, `h.engine.GetBalances(...)`,
  `h.engine.VerifiedBalances(...)` against an `*ledger.Engine` type that
  isn't defined anywhere yet. This is where the actual transactional
  logic belongs: `AddExpense` needs to, in one transaction, insert the
  `expenses` row, insert every `expense_splits` row (letting the deferred
  trigger validate the sum at commit), and update `group_balances` for
  every affected member with the optimistic-lock `version` check,
  retrying or surfacing `ErrConcurrentUpdate` on a mismatch;
  `RecordSettlement` does the equivalent two-sided balance update;
  `VerifiedBalances` needs a query that recomputes net balances directly
  from `expenses`/`expense_splits`/`settlements`, bypassing
  `group_balances` entirely, and should be checked against the cached
  path by an integration test (see `docs/TESTING.md` §3) before this
  project's central claim can be considered proven rather than just
  designed.
- **`internal/ledger/errors.go` does not exist yet.** `split.go` already
  references `ErrSplitsDontSum` and `ErrNotGroupMember`; `handlers.go`
  already references `ErrDuplicate`, `ErrConcurrentUpdate`, and
  `ErrNotFound`. None of these sentinel errors are defined anywhere in
  the current tree — this is a small file but it blocks everything else
  from compiling, and is the natural first thing to write.
- **`internal/api/types.go` does not exist yet** — the request/response
  structs `handlers.go` already references (`createUserRequest`,
  `createGroupRequest`, `addExpenseRequest`, `recordSettlementRequest`,
  `errorResponse`) aren't defined anywhere yet.
- **`internal/api/router.go` and `cmd/api/main.go` do not exist yet** —
  nothing currently wires config → db pool → engine → handlers → an
  actual chi router → an HTTP server with graceful shutdown. Nothing in
  this repo is runnable yet.
- **The schema exists only as one undifferentiated block of SQL** — it
  needs to be split into `internal/db/migrations/0001_init.sql` (the
  directory was created, the file wasn't yet). Note the schema's own
  in-line comments already explain the two-invariants-two-mechanisms
  reasoning (`docs/ARCHITECTURE.md` §2 lifts this almost directly) —
  don't lose those comments when moving the SQL into its migration file.
- **No `docker-compose.yml`, `Dockerfile`, `Makefile`, or `.env.example`
  yet.** The `README.md`'s Quickstart section describes the intended
  `make up` / `make migrate` / `make seed` / `make run` / `make test` /
  `make test-integration` targets; none of those targets exist yet.
- **No seed script.**
- **No tests of any kind yet** — not the pure-logic unit tests for
  `split.go`/`settlement_plan.go` (which need no database and are the
  fastest, most valuable tests to write first), and not the integration
  suite in `test/integration/` that would prove the concurrency,
  idempotency, and cached-vs-verified-balance claims this project's whole
  design is built around. See `docs/TESTING.md` for exactly what each
  test needs to prove — right now that document describes the *intended*
  test suite, not an existing one.
- **No authentication or authorization anywhere.** Flagged in
  `docs/PRD.md` and `docs/ARCHITECTURE.md` as a real gap, not a
  formality — every handler is currently reachable by anyone.

## Design decisions already made — don't relitigate without reason

1. **`group_balances` is a cache; `VerifiedBalances` recomputes from
   source.** Don't let a future "optimization" make the cache the only
   read path, or add a way to write to `group_balances` directly outside
   of `Engine`'s transactional writes — see `docs/ARCHITECTURE.md` §1.
2. **The deferred-constraint-trigger pattern applies to the expense-level
   sum invariant only, not the group-wide zero-sum invariant.** This is
   the specific design lesson this project exists to demonstrate — see
   `docs/ARCHITECTURE.md` §2 before "fixing" this by adding a trigger for
   the group-wide case.
3. **Optimistic locking (`version` column) on `group_balances`, not row
   locking alone**, for concurrent expense/settlement writes to the same
   group. Preserve the `WHERE version = $expected` pattern in whatever
   `engine.go` implementation gets written.
4. **Idempotency is a database uniqueness constraint per group, not a
   Redis-backed request-cache interceptor** — deliberately different from
   SlotForge/FlagForge's approach, and deliberately so; see
   `docs/ARCHITECTURE.md` §6 for exactly why this project's write shape
   doesn't need the heavier mechanism.
5. **`ComputeSettlementPlan` stays the greedy heuristic.** If asked to
   make settlement suggestions "more optimal," the correct next step is
   understanding and stating the complexity trade-off explicitly (this is
   an NP-hard problem to solve exactly), not silently swapping in an
   exponential-time exact solver that will fall over past a small group
   size.
6. **Deterministic sorted-ID remainder allocation in `splitEvenly`.**
   Don't replace this with map-iteration order or any other
   non-deterministic allocation — see `docs/ARCHITECTURE.md` §4.

## Suggested next-session priorities, in order

1. Write `internal/ledger/errors.go` — small, and it's what's currently
   blocking the rest of the tree from compiling.
2. Write `internal/ledger/engine.go` — the core transactional logic.
   This is the most important file in the project to get right; budget
   real time for it, and write the integration tests for it (§3 below)
   alongside it, not after.
3. Write `internal/api/types.go`, `internal/api/router.go`, and
   `cmd/api/main.go` to get something that actually compiles and runs.
4. Split the schema block into `internal/db/migrations/0001_init.sql`.
5. Write `docker-compose.yml`, `Dockerfile`, `Makefile`, `.env.example`,
   and a seed script — get `make up && make migrate && make seed && make
   run` working end to end.
6. Unit tests for `split.go` and `settlement_plan.go` — pure functions,
   no database, the fastest tests to add and the ones that catch the most
   embarrassing bugs cheaply.
7. The integration suite from `docs/TESTING.md`: concurrent-write
   conflict handling, idempotency-key replay, the deferred-trigger
   violation, and — the most important one — cached-vs-verified-balance
   agreement across a realistic sequence of writes.
8. Authentication. Not optional before this is anything but a portfolio
   piece.

## How to give a fresh agent session everything it needs

Point it at, in this order: this file → `docs/ARCHITECTURE.md` →
`docs/PRD.md`. Tell it explicitly: "this codebase doesn't compile yet —
`internal/ledger/engine.go`, `errors.go`, `internal/api/types.go`,
`router.go`, and `cmd/api/main.go` don't exist, and `handlers.go` already
references types and methods on all of them; write those next, in that
order, before anything else," and "the deferred-constraint trigger
applies to the expense-splits-sum invariant only, not the group-wide
zero-sum balance invariant — that second invariant is what
`VerifiedBalances` is for, don't add a trigger for it without
understanding why this project deliberately didn't."
