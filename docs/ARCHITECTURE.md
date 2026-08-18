# Architecture: principles → code

Same purpose as the companion documents across this portfolio: the code
should never be more authoritative than the reasoning behind it. Each
section is one design principle, what it means for a group-expense
ledger specifically, and exactly where it's implemented.

## System shape

```
                 ┌──────────────────┐
  clients   ───▶ │  Go API (chi)     │  REST, JSON, versionless (v1 is implicit)
 (frontend,      │  (internal/api)   │
  scripts)       └─────────┬────────┘
                            │ writes: internal/ledger.Engine —
                            │ one Postgres transaction per write
                            ▼
                 ┌──────────────────┐
                 │   PostgreSQL      │  expenses, expense_splits, settlements
                 │  (source of       │  — append-only, the actual history
                 │   truth: the      │
                 │   append-only     │
                 │   tables)         │
                 └─────────┬────────┘
                            │ every write also updates...
                            ▼
                 ┌──────────────────┐
                 │  group_balances   │  materialized cache, same transaction,
                 │  (fast, cached    │  optimistic-locked via `version`
                 │   read path)      │
                 └──────────────────┘
                            ▲
                            │ VerifiedBalances bypasses this entirely,
                            │ recomputes straight from the tables above
                 ┌──────────────────┐
                 │ GET .../verified   │
                 └──────────────────┘
```

---

## 1. `group_balances` is a cache, not a source of truth

**Where:** `group_balances` (schema) holds one row per `(group_id,
user_id)` with a `net_balance_cents` value maintained by every write in
`internal/ledger/engine.go` (`AddExpense`, `RecordSettlement`) inside the
same transaction as the expense or settlement it's derived from. It
exists purely so `GET /groups/:id/balances` is an O(1) indexed read
instead of replaying a group's entire history on every request. The
actual source of truth is the append-only `expenses` /
`expense_splits` / `settlements` tables — nothing about the cache is
ever treated as authoritative on its own. `Handlers.GetVerifiedBalances`
(`internal/api/handlers.go`) exposes a second read path,
`Engine.VerifiedBalances`, that recomputes net balances directly from
those source tables, bypassing `group_balances` entirely — the same
cached-vs-verified split LedgerLine uses for account balances, applied
here to group-relative net balances instead of double-entry account
balances. A client that needs speed calls the cached endpoint; a client
that needs certainty — reconciliation, an audit, a support investigation
— calls the verified one.

## 2. Two different invariants, enforced by two deliberately different mechanisms

**Where:** this schema has two distinct correctness invariants that
*look* similar but aren't, and the migration comments are explicit about
why they're handled differently:

- **"A single expense's splits must sum to exactly its
  `total_amount_cents`"** — a *per-row-group* invariant with the same
  shape as LedgerLine's original double-entry problem (a specific set of
  rows, inserted together, must sum to a specific value). This one gets
  LedgerLine's deferred-constraint-trigger treatment:
  `check_expense_splits_sum()`, a `CONSTRAINT TRIGGER ... DEFERRABLE
  INITIALLY DEFERRED` on `expense_splits`, checked once at transaction
  commit against the complete set of an expense's splits rather than
  failing on the first partial row inserted.
- **"The sum of every group member's net balance must be zero"** — a
  *whole-group, whole-history* invariant (money isn't created or
  destroyed by an expense split), and it deliberately does **not** get
  the same deferred-trigger treatment. No single `expense_splits` row
  needs to balance against anything by itself; it's one part of a whole
  expense, and the group-wide zero-sum property only makes sense to check
  across the *entire* `group_balances` table, not row-by-row within one
  transaction. Applying the same trigger pattern here would need a
  fundamentally different mechanism — this is exactly why
  `Engine.VerifiedBalances` exists as an independent, on-demand
  recomputation rather than a database constraint: some invariants belong
  in a trigger, and some belong in a verification query, and knowing
  which is which — not just "add a trigger, it worked last time" — is the
  actual design decision being made here.

## 3. Optimistic locking on the balance cache, not just row locking

**Where:** `group_balances.version` (schema), incremented on every write,
checked via a `WHERE ... AND version = $expected_version` clause in
`Engine.AddExpense` and `Engine.RecordSettlement` (the update-in-place
step for each affected member's cache row). Two concurrent writes to the
same group's balances — two expenses added at once, or an expense and a
settlement landing together — must never have one silently overwrite the
other's effect. A version mismatch means someone else's write landed
first; the engine surfaces this as `ErrConcurrentUpdate`, which
`Handlers.respondExpenseError` maps to a `409` and increments
`observability.ConcurrentUpdateConflicts` — an explicit, visible signal
that contention happened, not a silently-lost update. Plain row-level
locking (`SELECT ... FOR UPDATE`) alone would serialize these writes
correctly too, but optimistic locking was chosen for the same reason it
was chosen elsewhere in this portfolio's ledger-family projects: it
doesn't hold a database lock across the rest of the transaction's work
(computing the new balances, inserting the expense/settlement row), which
matters once a transaction does more than one thing.

## 4. Deterministic even-split allocation

**Where:** `splitEvenly` (`internal/ledger/split.go`) divides
`totalCents` by the member count, then allocates the integer remainder
cents one-per-person to the *first N members in sorted-user-ID order* —
never to an arbitrary or insertion-order-dependent subset. This matters
for the same reason idempotency matters elsewhere: given identical
inputs, this must always produce the identical split. Without
deterministic ordering, two functionally-identical requests (or a
retried request that recomputes rather than replays) could produce two
different-looking, equally-valid-but-different splits — confusing for a
user comparing two runs, and actively bad for reasoning about
idempotency, since "the same request produced a different result" is
exactly the kind of thing an idempotency guarantee is supposed to rule
out.

## 5. Two-layer validation for the sum-to-total invariant

**Where:** `validateSplits` (`internal/ledger/split.go`) runs
*before* any database write, checking membership, positive amounts, and
that shares sum to the total, returning `ErrSplitsDontSum` or
`ErrNotGroupMember` with a fast, clear error message. This is
deliberately not the *only* check — `check_expense_splits_sum()`'s
deferred trigger (§2) is the backstop that holds even if a future code
path (a bulk-import script, a data migration, a bug that skips calling
`validateSplits`) tries to insert `expense_splits` rows directly. The
application-level check exists for a fast, specific, actionable error
message; the database-level check exists as the guarantee that doesn't
depend on every future code path remembering to call the right Go
function — the same "put the guarantee somewhere no application bug can
route around it" instinct as every other project in this portfolio,
applied here as a second layer rather than the only layer, since a
same-transaction application check genuinely can give a better error
message than a constraint violation can.

## 6. Idempotency via a database uniqueness constraint, not a request-cache interceptor

**Where:** `expenses.idempotency_key` and `settlements.idempotency_key`
are nullable `TEXT` columns backed by a *partial* unique index —
`idx_expenses_group_idempotency ON expenses (group_id, idempotency_key)
WHERE idempotency_key IS NOT NULL` (and the equivalent for settlements) —
so idempotency is scoped per-group, and requests that omit a key entirely
are never constrained against each other. `Engine.AddExpense` and
`Engine.RecordSettlement` attempt the insert and translate the resulting
unique-constraint violation into `ErrDuplicate`, which
`Handlers.AddExpense`/`RecordSettlement` treat specially — returning the
original record with a `200` instead of a `201`, and deliberately *not*
incrementing the `ExpensesAdded`/`SettlementsRecorded` counters a second
time. This is a genuinely different mechanism from SlotForge's or
FlagForge's Redis-backed idempotency interceptor, and that difference is
deliberate, not an oversight: those projects short-circuit *before* any
work happens, which matters when the work involves side effects beyond
one transaction (queuing a job, calling an external service). SplitStack's
writes are a single, self-contained database transaction with no external
side effects to avoid duplicating — a database-level uniqueness
constraint is simpler, needs no additional infrastructure (no Redis
dependency), and gives the identical safety guarantee for this specific
shape of write. Choosing the heavier mechanism here "because that's what
the other projects did" would be pattern-matching, not design.

## 7. Debt simplification: a documented heuristic, not an unproven claim of optimality

**Where:** `ComputeSettlementPlan` (`internal/ledger/settlement_plan.go`)
implements the standard greedy approach — repeatedly match the largest
current creditor with the largest current debtor, settle as much of that
pair as possible, advance whichever side hits zero, repeat. The file's
own top-of-file comment is explicit about what this does and doesn't
prove: it always fully settles the group (every balance reaches exactly
zero — proven by a dedicated test, see `docs/TESTING.md`), it runs in
`O(n log n)`, and it produces a small number of payments in practice —
but the true minimum-transaction-count version of this problem is
equivalent to a partition/subset-sum problem and is NP-hard in general,
so this is *not* a certified-minimal solver, and the API response
(`Handlers.GetSettlementPlan`) says so directly in a `note` field rather
than letting a client assume "suggested payments" means "the fewest
possible payments." This is the kind of claim discipline the whole
portfolio tries to model: ship the fast, practical, well-understood
algorithm, and be precise in both the code comment and the API response
about exactly what guarantee it does and doesn't carry.

## 8. Explicit, typed error handling mapped to specific HTTP semantics

**Where:** `internal/ledger`'s sentinel errors (`ErrDuplicate`,
`ErrSplitsDontSum`, `ErrNotGroupMember`, `ErrConcurrentUpdate`,
`ErrNotFound`) are matched via `errors.Is` in
`Handlers.respondExpenseError`, each mapped to a specific, meaningful
status code — `422` for a validation-shaped failure
(`SPLITS_DONT_SUM`, `NOT_GROUP_MEMBER`), `409` for a concurrency conflict
(`CONCURRENT_UPDATE`, also incrementing a metric), `404` for a missing
resource, and only an unrecognized error falls through to a logged `500`.
A client can distinguish "you sent bad data" from "someone else's write
raced yours, retry" from "this doesn't exist" programmatically via the
`code` field in every error response, not just by string-matching an
error message.

## 9. Observability

**Where:** `internal/observability` defines Prometheus counters for
expenses added, settlements recorded, and concurrent-update conflicts —
the last one specifically because a rising rate of `409`s on balance
writes is the earliest signal of real contention, before it shows up as
a user-visible complaint. `zap`'s structured logger is used for anything
unexpected (`Handlers`' `default` error branch), not `fmt.Println`, and
`observability.MetricsHandler()` exposes everything at `/metrics` via
`promhttp`.

## Explicit boundaries for v2

Full detail and next steps are in `docs/CURSOR_CONTEXT.md`:

- **API-key authentication is strict, but user-level authorization is pending.**
  The service is secured via basic auth, but tying expenses to specific
  user tokens is pushed to v2.
- **Strict currency matching, no multi-currency conversion.** Expenses must
  match the group currency. Automatic conversion is deferred.
- **The ledger is append-only; there is no edit or delete for expenses or
  settlements.** Correcting a mistake means recording an offsetting
  entry. Tooling to automate this is slated for v2.
- **The greedy settlement algorithm is not provably minimal** — a
  documented, deliberate trade-off (§7), not a bug.
- **No explicit backoff or retry cap is implemented yet for optimistic-
  lock conflicts on the client-facing write path** — a `409` is returned
  immediately on the first version mismatch rather than the engine
  retrying internally a bounded number of times before giving up; whether
  to add internal retry-with-backoff versus leaving retry to the caller
  is an open decision, not yet made either way.
