# The story behind SplitStack

*Use this as a base for a LinkedIn post, a Medium article, or interview
talking points. Rewrite it in your own voice — this is scaffolding, not a
script.*

## The short version (LinkedIn post)

Splitwise-style apps look like CRUD with a computed field: track who paid
what, keep a running total per person, done. Two things make that
description wrong once you take it seriously. First, "who owes what right
now" needs to be a fast, cached read across a group's entire history — and
a cache that can't prove it still agrees with the history it came from is
a liability, not a feature. Second, minimizing the number of payments
needed to settle a group is a genuinely hard problem — equivalent to a
partition problem, NP-hard in general — and most implementations either
skip it (settle every pair individually) or quietly claim to solve it
"optimally" without meaning what that word means.

I built **SplitStack**: a Go + PostgreSQL expense-splitting engine where
the cached balance table is explicitly *not* the source of truth — a
`VerifiedBalances` read path recomputes every balance directly from the
append-only expense and settlement history on demand, so "does the cache
still match reality" is a question with a provable answer, not an
assumption. And the settlement-suggestion algorithm is the well-known
greedy heuristic, documented in the code and in the API response itself
as fast and always-correct but not certified minimal — because pretending
otherwise is exactly the kind of overclaim this project exists to avoid.

It's open source. The interesting part is
[`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md), which explains why two
invariants that look almost identical — "one expense's splits must sum to
its total" and "a group's balances must sum to zero" — get two
deliberately different enforcement mechanisms, not the same one applied
twice.

Repo: `<your-fork-url>`

---

## The longer version (Medium article)

### Title: Splitting a Bill Is a Consistency Problem Before It's a Math Problem

### The part everyone gets right, and the two parts almost nobody proves

Ask someone to describe how a bill-splitting app works and you'll get
roughly the same answer every time: track expenses, track who paid, keep
a running balance per person. That part is genuinely simple, and most
implementations of it are fine. What's much rarer is an implementation
that can *prove* two specific things stay true as the system grows: that
the fast, cached "who owes what" number you show on every page load
actually still matches the full history it's derived from, and that the
"here's how to settle up" suggestion the app offers is something more
rigorous than "seemed like a reasonable set of payments."

Neither of these is hard to get *approximately* right. Both are
surprisingly easy to get *silently* wrong in a way that only shows up
months later, after real usage, when someone's balance is off by a few
dollars and nobody can say why.

### A cache is only trustworthy if you can prove it against its own source

SplitStack keeps a `group_balances` table — one row per group member,
holding their current net balance — because recomputing "who owes what"
by replaying a group's entire expense and settlement history on every
page load doesn't scale, and shouldn't have to. But a cache is a
liability the moment nothing checks whether it's still correct. So
alongside the cached read, there's a second, independent read path:
`VerifiedBalances`, which ignores the cache entirely and recomputes every
member's net balance directly from the append-only `expenses`,
`expense_splits`, and `settlements` tables — the actual history, not a
running total someone's trusting to have stayed correct.

This isn't a performance optimization dressed up as a correctness
feature. It's a genuine, separate code path, callable on demand, and the
project's test suite treats "does the cached read agree with the
verified read after every single write in a realistic sequence" as its
single most important claim to prove — not "does the happy path work,"
but "does the shortcut we built for speed ever quietly drift from the
truth it's supposed to represent." A cache you can't independently verify
against its source isn't really a cache — it's an assumption with good
performance.

### Two invariants that look the same and aren't

The schema has two rules that, described casually, sound almost
identical: "an expense's splits must add up to its total," and "a
group's balances must add up to zero." Both are "a set of numbers must
sum to a specific value" — which makes it tempting to reach for the same
tool for both. That would be a mistake, and the reason why is the most
interesting design decision in this project.

The first rule — one expense's splits summing to that expense's total —
is a *local* invariant: a specific, small set of rows, all inserted
together in one transaction, that must sum correctly before that
transaction is allowed to commit. That's exactly the shape a Postgres
deferred constraint trigger is built for: check the complete set once, at
commit time, not row by row as each one comes in. So that's what enforces
it — a trigger that sums every `expense_splits` row for an expense and
compares it against that expense's total, deferred until the whole
transaction's inserts are visible to it.

The second rule — a group's balances summing to zero across its *entire*
history — is not a local invariant at all. No single row needs to
"balance" against anything; a group-wide zero-sum property only makes
sense checked against everything, not any one transaction's writes. Using
the same trigger mechanism here would be solving the wrong-shaped
problem with the right-shaped tool from a different problem. So this
project doesn't try — the group-wide invariant gets `VerifiedBalances`
instead: an on-demand recomputation, not a per-transaction constraint. The
lesson isn't "triggers are good" or "triggers are bad." It's that two
invariants that sound alike in an product spec can have genuinely
different shapes underneath, and the discipline is noticing which shape
you actually have before reaching for whichever tool worked last time.

### Being honest about what "optimal" doesn't mean

The other half of this project is suggesting how a group should settle
up — which payments, between which people, clear every balance to zero
with as few transactions as possible. The genuinely correct version of
"as few transactions as possible" is a hard combinatorial problem;
finding the provably minimum number of payments is equivalent to a
partition problem and doesn't scale past a small group size with an exact
algorithm. Most implementations either don't try (settle every pair
individually — a payment count that grows with group size for no good
reason) or imply a stronger guarantee than they actually deliver.

SplitStack does neither. It uses the standard, well-understood greedy
approach — repeatedly match whoever's owed the most with whoever owes the
most, settle as much of that pair as the smaller side allows, repeat —
which is fast, always fully settles the group, and produces a small
number of payments in practice. And it says exactly that, in the code
comment and in the API response's own text: this is a good heuristic,
not a certified-minimal solver. That's a small thing to be precise about,
and it's exactly the kind of small thing that separates a project you can
defend clearly in an interview from one where a sharp follow-up question
("is that actually the minimum?") has no good answer.

### What's next (v2)

With the core MVP stabilization complete, the boundaries for v2 are explicit.
While basic API-key authentication secures the service today, full user-level
JWT authorization is slated for the next cycle. Multi-currency conversion
(beyond enforcing that expenses strictly match their group's currency) is
also pushed to v2. The ledger is append-only by design — correcting a mistake
means recording an offsetting entry, not editing history. Tooling to automate
these offsetting entries will arrive in a future update. All of these are
written down as deliberate boundaries, so they aren't discovered later by
someone wondering why they're missing.

### Conclusion: the hard part was never the arithmetic

It's tempting to think a bill-splitting app is a math problem — compute a
sum, divide it, done. The actual engineering lives somewhere else
entirely: in making sure a fast cached answer can always be checked
against the slow, true one it's standing in for; in noticing that two
rules which sound alike can have genuinely different shapes and need
genuinely different enforcement; and in being precise, in the code and
in the API itself, about exactly what an algorithm proves and what it
merely tends to do well. None of that shows up if you only look at the
arithmetic. All of it shows up the first time a cache silently drifts, or
two people add an expense to the same group at the same second, or
someone asks "wait, is that really the fewest payments possible?" —
which is exactly why those were the parts worth building deliberately,
and the parts worth being able to explain clearly, rather than the sums
and totals wrapped around them.

---

## Talking points for an interview

1. **Lead with the two specific claims, not "I built a Splitwise
   clone."** "A cached balance that's independently verifiable against
   its own source, and a settlement algorithm that's honest about not
   being provably optimal" tells an interviewer exactly what's
   interesting about this project in one sentence.
2. **Explain the two-invariants-two-mechanisms decision in your own
   words** — this is the single best differentiator in this project's
   story, and being able to say precisely *why* a deferred trigger fits
   one invariant and not the other proves real understanding, not
   pattern-matching "add a constraint."
3. **Be precise about the settlement algorithm's guarantee.** Say
   "greedy, always fully settles the group, not certified minimal, here's
   why that's NP-hard to guarantee exactly" — not "it finds the optimal
   set of payments." A good follow-up question here is a gift, not a
   trap, if you've actually thought about the trade-off.
4. **Point at the reconciliation test** (cached vs. verified balances
   agreeing after every write in a realistic sequence) as the concrete,
   verifiable claim backing the cache-is-not-source-of-truth design — not
   a vague assurance that "the cache stays in sync."
5. **Be upfront about what's pushed to v2.** "We stabilized the MVP with
   API key authentication and strict currency enforcement, but left user-level
   auth and multi-currency conversion to the next cycle" is a stronger answer
   than pretending the project has features it doesn't.

## Suggested post formats

**Short (LinkedIn/X):**
> Built an expense-splitting engine in Go + PostgreSQL where the cached
> "who owes what" balance is explicitly not the source of truth — a
> separate read path recomputes every balance directly from the
> append-only expense/settlement history on demand, so the cache's
> correctness is provable, not assumed. Two invariants that sound almost
> identical ("one expense's splits sum to its total" vs "a group's
> balances sum to zero") get two deliberately different enforcement
> mechanisms, because they're actually different shapes of problem.
> Settlement suggestions use the well-known greedy heuristic — fast,
> always fully settles the group, honestly documented as not provably
> minimal, because the true minimum is NP-hard to guarantee. Open source:
> `<link>`

**Medium article structure:** *Splitting a Bill Is a Consistency Problem
Before It's a Math Problem* → the part everyone gets right vs. the two
parts nobody proves → the cache-vs-verified-balance split → two
invariants that look alike but aren't (and why they need different
mechanisms) → being honest about what "optimal" doesn't mean in the
settlement algorithm → explicit boundaries for v2 → conclusion.
`ARCHITECTURE.md` §1–2 and §7 can be lifted almost directly into the
technical middle of the article.
