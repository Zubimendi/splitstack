# PRD: SplitStack

## Problem

Splitting shared expenses across a group — rent, a trip, a dinner — and
figuring out who actually needs to pay whom to settle up is a problem
almost everyone has solved badly with a spreadsheet at least once. The
naive version tracks a running total per person and, when it's time to
settle, has every debtor pay every creditor individually — a payment
count that grows with group size for no good reason. The slightly-less-
naive version keeps a single "who owes what" number per person but has
no way to prove that number is actually correct months later, after
hundreds of expenses and partial settlements, without replaying the
entire history by hand. SplitStack is built to get both of these right:
fast, cached balances that are always reconcilable against the append-
only history that produced them, and a settlement plan that minimizes
payment count well, honestly described as a heuristic rather than an
unproven claim of optimality.

## Who this is for

- A group of roommates, trip companions, or a small shared household that
  wants "who owes what" to be always current and always trustworthy, not
  a spreadsheet someone forgot to update.
- Anyone evaluating whether a cached-aggregate-plus-verified-recompute
  pattern (the same one LedgerLine uses for account balances) generalizes
  to a different, weaker invariant — group-relative net balances instead
  of double-entry account balances — which is exactly the design question
  this project exists to answer.

## Core requirements

1. **Users and groups.** A user can belong to multiple groups; a group
   has a currency and a member list.
2. **Expenses.** Adding an expense records who paid, the total amount,
   and how it's split across group members — either explicit per-member
   shares (which must sum to exactly the total, enforced at both the
   application layer and the database layer) or, if no explicit split is
   given, an even division across every current group member, with
   remainder cents allocated deterministically so the same inputs always
   produce the same split.
3. **Settlements.** Recording a settlement (`from` paid `to` some amount)
   updates both parties' balances. A user cannot settle with themselves.
4. **Idempotent writes.** Both expense creation and settlement recording
   accept an optional idempotency key; replaying the same key returns the
   original result rather than creating a duplicate record.
5. **Fast balance reads.** `GET /groups/:id/balances` returns each
   member's current net balance (positive = owed money by the group,
   negative = owes the group) from a cache, not by replaying the group's
   entire expense/settlement history on every request.
6. **Verifiable balance reads.** `GET /groups/:id/balances/verified`
   recomputes the same balances directly from the append-only source
   tables, bypassing the cache entirely — an explicit, on-demand
   certainty check, not something a client is expected to call on every
   page load.
7. **Settlement suggestions.** `GET /groups/:id/settlement-plan` returns a
   small set of suggested payments that would fully settle the group to
   zero, computed by a fast, well-understood greedy heuristic — documented
   as such, not marketed as provably minimal.
8. **Correctness under concurrent writes.** Two expenses or settlements
   hitting the same group's balances concurrently must never silently
   lose one of the updates — a losing write must fail explicitly
   (surfaced as a 409) rather than overwrite the other's effect.

## Explicit non-goals for v1

- **No multi-currency conversion.** Expenses must strictly match their
  group's currency, preventing silent bugs without requiring complex FX math.
  Automatic conversion is slated for v2.
- **No editing or deleting past expenses or settlements.** The ledger is
  append-only, matching LedgerLine's philosophy — correcting a mistake
  means recording an offsetting entry, not mutating history. Tooling to
  automate these offsetting entries is slated for v2.
- **No recurring expenses, receipt attachments, or notifications.** Pure
  ledger-and-settlement logic, not a full consumer product.
- **API-key authentication only.** The API is secured via a strict
  API key, but user-level authorization (e.g., JWT) is deferred to v2.
- **No provably-optimal debt simplification.** The true minimum-
  transaction-count version of this problem is equivalent to a
  partition/subset-sum problem and is NP-hard in general; an exhaustive
  search doesn't scale past a handful of people. v1 ships the greedy
  largest-creditor-vs-largest-debtor heuristic and documents the
  trade-off explicitly rather than either skipping the feature or
  overclaiming what it does.

## Success criteria

- `VerifiedBalances` and the cached `group_balances` read agree, for
  every group, at all times outside of a write in flight — proven by an
  automated reconciliation test that runs a realistic sequence of
  expenses and settlements and diffs the two reads after each one.
- Two concurrent `AddExpense` calls against the same group never result
  in a `group_balances` row reflecting only one of the two writes — one
  succeeds, the other retries or fails explicitly with a 409, and after
  both have resolved, the cache matches the verified recomputation.
- An expense whose splits don't sum to its total is rejected before any
  row commits — proven at two layers: the application-level
  `validateSplits` check (fast, a clear error message) and, as a
  backstop, the database's deferred constraint trigger (the guarantee
  that holds even if a future code path bypasses application validation
  entirely).
- Replaying an identical idempotency key for either an expense or a
  settlement returns the original record, unchanged, and creates no new
  row — proven by an automated test, not just implied by a unique index.
- `ComputeSettlementPlan`'s output, for any set of balances, always fully
  settles the group (every suggested payment applied brings every
  balance to exactly zero) — proven directly by a property-based-style
  test across many random balance sets, independent of whether the
  payment count happens to be minimal for that particular input.

## Named risks / open questions

- **User-level authorization is pending.** Basic API key secures the service,
  but proper multi-tenant authorization is a real blocker before consumer launch.
  Flagged explicitly — see `docs/CURSOR_CONTEXT.md`'s next-priorities list.
- **Optimistic-lock retry storms under high contention.** A group with
  many members adding expenses simultaneously could see a meaningful
  retry rate on `group_balances` writes; v1 doesn't yet cap or back off
  retries explicitly (a risk this project shares with any optimistic-
  locking design, and worth a load test before calling this
  production-grade — see `docs/TESTING.md`'s known gaps).
- **The greedy settlement plan can, for certain balance distributions,
  suggest more payments than a smarter (but exponentially more
  expensive) algorithm would.** This is a known, accepted, documented
  trade-off, not a bug to eventually "fix" without changing the
  fundamental approach — see `docs/ARCHITECTURE.md`.
