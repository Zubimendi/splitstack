package ledger

import "sort"

// splitEvenly divides totalCents as evenly as possible across members,
// handling the remainder deterministically: totalCents / len(members)
// as the base share, with the remaining cents (totalCents % len(members))
// distributed one-per-person to the FIRST N members in sorted-ID order.
// Deterministic ordering matters here for the same reason it mattered
// for lock ordering elsewhere in this project - given the same inputs,
// this must always produce the same split, not an arbitrary one that
// happens to also sum correctly (which would make a repeated identical
// request produce different-looking, if equally-valid, splits - bad for
// idempotency and confusing for users comparing two runs).
func splitEvenly(totalCents int64, memberIDs []string) []SplitInput {
	sorted := make([]string, len(memberIDs))
	copy(sorted, memberIDs)
	sort.Strings(sorted)

	n := int64(len(sorted))
	base := totalCents / n
	remainder := totalCents % n

	splits := make([]SplitInput, len(sorted))
	for i, userID := range sorted {
		share := base
		if int64(i) < remainder {
			share++
		}
		splits[i] = SplitInput{UserID: userID, ShareAmountCents: share}
	}
	return splits
}

func validateSplits(splits []SplitInput, totalCents int64, memberSet map[string]bool) error {
	if len(splits) == 0 {
		return ErrSplitsDontSum
	}
	var sum int64
	for _, s := range splits {
		if !memberSet[s.UserID] {
			return ErrNotGroupMember
		}
		if s.ShareAmountCents <= 0 {
			return ErrSplitsDontSum
		}
		sum += s.ShareAmountCents
	}
	if sum != totalCents {
		return ErrSplitsDontSum
	}
	return nil
}
