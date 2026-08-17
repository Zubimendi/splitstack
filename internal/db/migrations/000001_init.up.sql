-- SplitStack schema. Full reasoning in docs/ARCHITECTURE.md; the two
-- ideas worth understanding before reading the rest of the codebase:
--
--   1. group_balances is a MATERIALIZED, per-(group, user) net balance
--      cache - not the source of truth. The source of truth is the
--      append-only expenses/expense_splits/settlements tables; the
--      cache exists purely so "who owes what right now" is an O(1) read
--      instead of replaying a group's entire history on every request.
--      Every write that changes it is transactional (see
--      internal/ledger/engine.go), and a VerifiedBalances function
--      recomputes directly from source for anyone who needs certainty
--      instead of speed - the same cached-vs-verified split LedgerLine
--      uses for account balances, applied here to group-relative net
--      balances.
--
--   2. This project deliberately does NOT repeat LedgerLine's deferred-
--      constraint-trigger balance-check pattern. That pattern is right
--      when the invariant is "every individual transaction must balance
--      to exactly zero" (double-entry bookkeeping). SplitStack's
--      invariant is different and weaker: the SUM of every group
--      member's net balance must be zero (money isn't created or
--      destroyed by an expense split), but no single expense_splits row
--      needs to "balance" against anything by itself - it's one part of
--      a whole expense. Enforcing that at the database level would need
--      a genuinely different mechanism (checking a GROUP BY expense_id
--      sum equals total_amount_cents - which the schema below DOES
--      still enforce, at the expense level, via the same deferred-
--      trigger technique, because THAT specific invariant - a single
--      expense's splits must sum to its total - has the identical shape
--      to LedgerLine's original problem). Applying a pattern where it
--      fits and explaining why a different part of the same pattern
--      doesn't fit elsewhere is the actual point being made here.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE users (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL,
    email      TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE groups (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL,
    currency   TEXT NOT NULL DEFAULT 'USD',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE group_members (
    group_id  UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id   UUID NOT NULL REFERENCES users(id),
    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, user_id)
);

-- The materialized balance cache described above. Positive
-- net_balance_cents means the group owes this user money (net
-- creditor); negative means this user owes the group (net debtor).
-- `version` is the optimistic-locking column - see
-- internal/ledger/engine.go for why concurrent expense/settlement writes
-- need it in addition to row locking.
CREATE TABLE group_balances (
    group_id          UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id           UUID NOT NULL REFERENCES users(id),
    net_balance_cents BIGINT NOT NULL DEFAULT 0,
    version           INT NOT NULL DEFAULT 1,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, user_id)
);

CREATE TABLE expenses (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    group_id           UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    description        TEXT NOT NULL,
    total_amount_cents BIGINT NOT NULL CHECK (total_amount_cents > 0),
    currency           TEXT NOT NULL,
    paid_by_user_id    UUID NOT NULL REFERENCES users(id),
    idempotency_key    TEXT,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_expenses_group_idempotency ON expenses (group_id, idempotency_key) WHERE idempotency_key IS NOT NULL;
CREATE INDEX idx_expenses_group ON expenses (group_id, created_at);

CREATE TABLE expense_splits (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    expense_id         UUID NOT NULL REFERENCES expenses(id) ON DELETE CASCADE,
    user_id            UUID NOT NULL REFERENCES users(id),
    share_amount_cents BIGINT NOT NULL CHECK (share_amount_cents > 0)
);

CREATE INDEX idx_expense_splits_expense ON expense_splits (expense_id);
CREATE INDEX idx_expense_splits_user ON expense_splits (user_id);

-- Enforces, at the database level, that every expense's splits sum to
-- EXACTLY its total_amount_cents - the same deferred-constraint-trigger
-- technique LedgerLine uses, applied to the one invariant in this
-- schema that has the same "a set of rows must sum to a specific value"
-- shape. Deferred so it checks the complete set of an expense's splits
-- (inserted together in one transaction) rather than failing on the
-- first partial row.
CREATE OR REPLACE FUNCTION check_expense_splits_sum() RETURNS TRIGGER AS $$
DECLARE
    expected BIGINT;
    actual   BIGINT;
BEGIN
    SELECT total_amount_cents INTO expected FROM expenses WHERE id = NEW.expense_id;
    SELECT SUM(share_amount_cents) INTO actual FROM expense_splits WHERE expense_id = NEW.expense_id;

    IF actual != expected THEN
        RAISE EXCEPTION 'expense % splits sum to % but total_amount_cents is %', NEW.expense_id, actual, expected
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER trg_check_expense_splits_sum
    AFTER INSERT ON expense_splits
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION check_expense_splits_sum();

CREATE TABLE settlements (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    group_id        UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    from_user_id    UUID NOT NULL REFERENCES users(id),  -- who paid
    to_user_id      UUID NOT NULL REFERENCES users(id),  -- who received payment
    amount_cents    BIGINT NOT NULL CHECK (amount_cents > 0),
    idempotency_key TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (from_user_id <> to_user_id)
);

CREATE UNIQUE INDEX idx_settlements_group_idempotency ON settlements (group_id, idempotency_key) WHERE idempotency_key IS NOT NULL;
CREATE INDEX idx_settlements_group ON settlements (group_id, created_at);
