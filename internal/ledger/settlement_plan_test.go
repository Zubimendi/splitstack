package ledger

import (
	"math/rand"
	"testing"
	"time"
)

func TestComputeSettlementPlan(t *testing.T) {
	// Simple deterministic cases
	t.Run("simple match", func(t *testing.T) {
		balances := []Balance{
			{UserID: "A", NetBalanceCents: 100},  // Owed 100
			{UserID: "B", NetBalanceCents: -100}, // Owes 100
		}
		plan := ComputeSettlementPlan(balances)
		if len(plan) != 1 {
			t.Fatalf("expected 1 payment, got %d", len(plan))
		}
		if plan[0].FromUserID != "B" || plan[0].ToUserID != "A" || plan[0].AmountCents != 100 {
			t.Errorf("unexpected payment: %+v", plan[0])
		}
	})

	t.Run("multiple people", func(t *testing.T) {
		balances := []Balance{
			{UserID: "A", NetBalanceCents: 100},  // Owed 100
			{UserID: "B", NetBalanceCents: 50},   // Owed 50
			{UserID: "C", NetBalanceCents: -75},  // Owes 75
			{UserID: "D", NetBalanceCents: -75},  // Owes 75
		}
		plan := ComputeSettlementPlan(balances)

		// C and D each owe 75. A is owed 100, B is owed 50.
		// Expected payments (greedy):
		// 1. Largest debtor C (75) pays largest creditor A (100). C=0, A=25.
		// 2. Largest debtor D (75) pays largest creditor B (50). D=25, B=0.
		// 3. D (25) pays A (25). D=0, A=0.
		// Total 3 payments.
		if len(plan) != 3 {
			t.Fatalf("expected 3 payments, got %d", len(plan))
		}

		// Let's verify that the plan fully settles the balances.
		verifySettlement(t, balances, plan)
	})

	// Property-based style test with random valid balance sets
	t.Run("randomized balance sets always settle to zero", func(t *testing.T) {
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		for i := 0; i < 100; i++ {
			balances := generateValidBalances(rng, 5+rng.Intn(10))
			plan := ComputeSettlementPlan(balances)
			verifySettlement(t, balances, plan)
			
			// A known property: max payments is N - 1
			if len(plan) > len(balances)-1 {
				t.Errorf("expected max %d payments for %d people, but got %d", len(balances)-1, len(balances), len(plan))
			}
		}
	})
}

func verifySettlement(t *testing.T, initial []Balance, plan []SuggestedPayment) {
	t.Helper()
	current := make(map[string]int64)
	for _, b := range initial {
		current[b.UserID] = b.NetBalanceCents
	}

	for _, p := range plan {
		current[p.FromUserID] += p.AmountCents // Debtor pays, balance goes up towards 0
		current[p.ToUserID] -= p.AmountCents   // Creditor receives, balance goes down towards 0
	}

	for id, bal := range current {
		if bal != 0 {
			t.Errorf("user %s is not settled, remaining balance: %d", id, bal)
		}
	}
}

func generateValidBalances(rng *rand.Rand, numPeople int) []Balance {
	balances := make([]Balance, numPeople)
	var sum int64
	for i := 0; i < numPeople-1; i++ {
		// Random balance between -1000 and 1000
		bal := int64(rng.Intn(2001) - 1000)
		balances[i] = Balance{
			UserID:          string(rune('A' + i)),
			NetBalanceCents: bal,
		}
		sum += bal
	}
	// Last person absorbs the rest to make the sum exactly 0
	balances[numPeople-1] = Balance{
		UserID:          string(rune('A' + numPeople - 1)),
		NetBalanceCents: -sum,
	}
	return balances
}
