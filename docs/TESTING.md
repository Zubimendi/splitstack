# Testing SplitStack

Same shape as the rest of the portfolio: unit tests (pure logic), local
manual/scripted integration testing, and — the most important section —
the automated tests that prove this project's actual claims: the
balance cache never silently drifts from the verified recomputation, a
concurrent write never gets silently lost, and the settlement algorithm
always fully settles a group even though it isn't provably minimal.

**Status note:** as of this writing, none of the tests below exist yet —
`internal/ledger/engine.go` (the thing most of them exercise) hasn't been
written either. This document specifies what the suite needs to prove
once both exist; see `docs/CURSOR_CONTEXT.md` for the actual current
state and build order.

## 1. Unit tests

No Docker required — these test pure functions with no database
dependency, and should be the first tests written, before `engine.go`
even exists.

```bash
go test ./internal/ledger/... -run 'Split|Settlement' -v
```

Should cover:

- **`splitEvenly`** — a total that divides evenly across N members; a
  total that doesn't (asserting the remainder lands on exactly the first
  `remainder` members in sorted-ID order, and that every member's share
  is either `base` or `base+1`, never anything else); a single-member
  group (the whole total, deterministically); and — the property that
  matters most — calling it twice with identical inputs (including
  member-ID ordering shuffled before the call) always produces the exact
  same result, proving the sort-before-allocate step is actually doing
  its job.
- **`validateSplits`** — splits that sum correctly (accepted); splits
  that don't (rejects with `ErrSplitsDontSum`); a split naming a user not
  in `memberSet` (rejects with `ErrNotGroupMember`); a zero or negative
  share amount (rejects); an empty splits slice (rejects).
- **`ComputeSettlementPlan`** — the single most important unit test in
  this file: for a large number of randomly generated balance sets (each
  one constructed to already sum to zero, since that's the invariant
  `group_balances` is supposed to maintain), apply every suggested
  payment in `ComputeSettlementPlan`'s output and assert every balance
  ends at exactly zero. This proves *correctness* (the plan always fully
  settles the group) independently of *optimality* (whether the payment
  count happens to be minimal for that input) — the two are different
  claims, and only the first one is something this project asserts. A
  second, narrower test can assert the well-known upper bound directly:
  the greedy algorithm never needs more than `n - 1` payments to settle
  `n` people, and this should hold across every generated case too.

## 2. Local setup

Once the Makefile and Dockerfile exist (see `docs/CURSOR_CONTEXT.md`):

```bash
cp .env.example .env
make up                        # Postgres via docker-compose
make migrate                   # applies internal/db/migrations/0001_init.sql
make seed                      # a couple of demo users, one group, a few expenses
make run                       # API on :8080
```

## 3. The cached-vs-verified reconciliation test — the most important thing to run

This is the test that proves this project's central architectural claim
(`docs/ARCHITECTURE.md` §1) rather than just asserting it in a comment.

```go
// test/integration/balance_reconciliation_test.go (to be written)
```

Run a realistic sequence against a real Postgres: create a group with
4–5 members, add a dozen expenses with a mix of even and explicit splits,
record a few settlements between random pairs, interleaving reads of
both `GET .../balances` (cached) and `GET .../balances/verified`
(recomputed from source) after every single write. Assertion: **the two
reads agree, byte-for-byte per user, after every write in the sequence,
not just at the end.** This is the test that would fail immediately if a
future change to `Engine.AddExpense` or `Engine.RecordSettlement` ever
updated `group_balances` inconsistently with what the source tables
actually recorded — try it yourself once `engine.go` exists:

```bash
# Deliberately break it to see the test catch the regression:
# in Engine.AddExpense, skip updating one affected member's
# group_balances row (simulating a bug that only updates the payer's
# cache, not every split participant's), re-run the reconciliation
# test, and watch it fail on exactly that member's balance.
```

## 4. The concurrency test — proving optimistic locking actually holds

```go
// test/integration/concurrent_writes_test.go (to be written)
```

Fire N concurrent `AddExpense` requests at the same group (different
expenses, all touching overlapping members' balances) — the same shape
as SlotForge's 20-concurrent-requests test, applied here to balance
writes instead of resource bookings. Assertions: every request either
succeeds (`201`) or fails explicitly with `409 CONCURRENT_UPDATE` — never
a silent success that actually lost another write's effect; after all N
requests resolve, `GetVerifiedBalances` matches the sum of every expense
that returned `201`, exactly; and `observability.ConcurrentUpdateConflicts`
incremented by exactly the number of `409`s observed, not more or fewer.
An optimistic-locking implementation with a bug (e.g. checking `version`
but forgetting to also update it, or checking it outside the same
transaction as the write) would pass a *single*-request test and still
fail this one — that gap is exactly why this needs a real concurrent
test, not just a unit test of the locking logic in isolation.

## 5. Proving idempotency

```go
// test/integration/idempotency_test.go (to be written)
```

**Test 1** — call `AddExpense` twice with an identical `idempotencyKey`
and identical body. Assertion: the first call returns `201` with a new
expense id; the second returns `200` with the *same* expense id, and
`ExpensesAdded` incremented exactly once, not twice — proving the second
call did no real work rather than just returning a similar-looking
result.

**Test 2** — same shape, for `RecordSettlement`.

**Test 3** — call `AddExpense` twice with the *same* `idempotencyKey` but
a *different* body (e.g. a different `totalAmountCents`). This is a
genuine ambiguity the current design doesn't resolve — decide and
document the answer when writing `engine.go`: either the second call
should also return the original (first-write-wins, idempotency keys are
opaque and the body is assumed identical) or it should reject with a
distinct error (detecting an idempotency-key reuse with a differing
payload, which is a stronger and more expensive guarantee). Whichever is
chosen, this test should assert that behavior explicitly rather than
leaving it as an accident of whatever the unique-constraint-violation
handling happens to do.

## 6. Proving the deferred trigger is the real backstop, not just documentation

```bash
# Once engine.go exists, add a raw-SQL integration test that
# deliberately bypasses validateSplits entirely:
BEGIN;
INSERT INTO expenses (id, group_id, description, total_amount_cents, currency, paid_by_user_id)
  VALUES ('...', '<group>', 'test', 1000, 'USD', '<user>');
INSERT INTO expense_splits (expense_id, user_id, share_amount_cents)
  VALUES ('<expense-id>', '<user-a>', 400), ('<expense-id>', '<user-b>', 500);
  -- sums to 900, not 1000
COMMIT;
-- expect: COMMIT fails with the check_expense_splits_sum() exception,
-- ERRCODE 23514 (check_violation), and neither expense_splits row nor
-- the expenses row persists.
```

This is the test that would fail to catch a regression if the deferred
trigger were ever accidentally dropped or the `DEFERRABLE INITIALLY
DEFERRED` clause were removed (turning it into an immediate constraint
that fails on the *first* partial row instead of the complete set) —
run it, then deliberately comment out the trigger's creation in the
migration, re-run, and confirm the bad data now commits successfully,
silently, exactly the failure this trigger exists to prevent.

## 7. Proving `GetSettlementPlan`'s upper-bound claim end-to-end

Not just the unit test in §1 — an integration-level version that runs
against a real group with real expense/settlement history, calls
`GET .../settlement-plan`, applies every suggested payment as real
`RecordSettlement` calls against the API, and confirms
`GetVerifiedBalances` shows every member at exactly zero afterward. This
is the version of the test that would catch a bug where the *unit* test's
synthetic balance generation didn't match the actual shape of balances
`group_balances` produces in practice (e.g. if real balances always end
up as whole-dollar cents due to how splits are typically entered, and
some edge case only shows up with genuinely arbitrary cent values).

## Known gaps in test coverage (to plan for once the base suite exists)

- No load test on `group_balances` writes — the concurrency test in §4
  proves correctness at a fixed N; it doesn't characterize how the
  optimistic-lock conflict rate scales as N grows, or whether an internal
  retry-with-backoff (an open design decision, see `docs/CURSOR_CONTEXT.md`)
  would meaningfully reduce client-visible `409`s under realistic
  contention.
- No test for the idempotency-key-reused-with-different-body ambiguity
  until that behavior is actually decided and implemented (§5, Test 3).
- No test for `splitEvenly` behavior with a total smaller than the member
  count in cents (e.g. splitting 3 cents across 5 people) — the current
  design should still produce a valid split (some members get 0 cents,
  which `validateSplits`'s positive-amount check would then reject) but
  this edge case isn't explicitly covered yet and the correct behavior
  (reject the expense entirely below some minimum, vs. allow zero-cent
  shares) hasn't been decided.
