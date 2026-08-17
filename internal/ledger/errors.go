package ledger

import "errors"

var (
	// ErrDuplicate is returned when an idempotency key reuse is detected (or unique constraint violation).
	ErrDuplicate = errors.New("duplicate idempotency key")

	// ErrSplitsDontSum is returned when the provided splits do not sum up to the total expense amount.
	ErrSplitsDontSum = errors.New("splits must sum to the total expense amount")

	// ErrNotGroupMember is returned when an expense or settlement refers to a user who is not part of the group.
	ErrNotGroupMember = errors.New("user is not a member of the group")

	// ErrConcurrentUpdate is returned when optimistic locking detects a concurrent update to group balances.
	ErrConcurrentUpdate = errors.New("concurrent update detected, please retry")

	// ErrNotFound is returned when a requested resource (user, group, etc.) cannot be found.
	ErrNotFound = errors.New("resource not found")

	// ErrIdempotencyMismatch is returned when an idempotency key is reused but the payload differs from the original request.
	ErrIdempotencyMismatch = errors.New("idempotency key reused with a different payload")
)
