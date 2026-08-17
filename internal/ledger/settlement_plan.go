// Debt simplification: given a group's current net balances, compute a
// small set of suggested payments that settles everyone to zero.
//
// This is the well-known GREEDY heuristic real apps (including
// Splitwise) use: repeatedly match whoever is owed the most money
// (the largest creditor) with whoever owes the most (the largest
// debtor), have the debtor pay the creditor as much as possible, and
// repeat until everyone is at zero.
//
// IMPORTANT, and worth being precise about in an interview: this
// minimizes the transaction count WELL in practice, but it is not
// certified globally optimal. The true minimum-transaction-count
// version of this problem is equivalent to a partition/subset-sum
// problem and is NP-hard in general - an exhaustive search for the
// provably fewest possible payments does not scale beyond a handful of
// people. The greedy largest-vs-largest approach is a well-understood,
// practical tradeoff: fast (O(n log n)), always correct (it always
// fully settles the group, see SettlementPlan_test.go), and produces a
// small - if not always PROVABLY minimal - number of payments.
package ledger

import "sort"

func ComputeSettlementPlan(balances []Balance) []SuggestedPayment {
	type mutableBalance struct {
		userID string
		amount int64
	}

	var creditors, debtors []mutableBalance
	for _, b := range balances {
		if b.NetBalanceCents > 0 {
			creditors = append(creditors, mutableBalance{b.UserID, b.NetBalanceCents})
		} else if b.NetBalanceCents < 0 {
			debtors = append(debtors, mutableBalance{b.UserID, -b.NetBalanceCents}) // store as a positive "amount owed"
		}
	}

	sort.Slice(creditors, func(i, j int) bool { return creditors[i].amount > creditors[j].amount })
	sort.Slice(debtors, func(i, j int) bool { return debtors[i].amount > debtors[j].amount })

	var payments []SuggestedPayment
	i, j := 0, 0
	for i < len(debtors) && j < len(creditors) {
		debtor := &debtors[i]
		creditor := &creditors[j]

		payment := min64(debtor.amount, creditor.amount)
		if payment > 0 {
			payments = append(payments, SuggestedPayment{
				FromUserID: debtor.userID, ToUserID: creditor.userID, AmountCents: payment,
			})
		}

		debtor.amount -= payment
		creditor.amount -= payment

		if debtor.amount == 0 {
			i++
		}
		if creditor.amount == 0 {
			j++
		}
	}

	return payments
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
