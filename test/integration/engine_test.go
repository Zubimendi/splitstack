package integration

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Zubimendi/splitstack/internal/ledger"
	"github.com/google/uuid"
)

func ptr[T any](v T) *T {
	return &v
}

func TestBalanceReconciliation(t *testing.T) {
	pool, engine := setupTestDB(t)
	defer pool.Close()
	ctx := context.Background()

	// 1. Create Users
	u1, _ := engine.CreateUser(ctx, "Alice", "alice@test.com")
	u2, _ := engine.CreateUser(ctx, "Bob", "bob@test.com")
	u3, _ := engine.CreateUser(ctx, "Charlie", "charlie@test.com")
	u4, _ := engine.CreateUser(ctx, "Diana", "diana@test.com")

	// 2. Create Group
	g, _ := engine.CreateGroup(ctx, "Trip", "USD", []string{u1.ID, u2.ID, u3.ID, u4.ID})

	assertBalancesMatch := func() {
		t.Helper()
		cached, err := engine.GetBalances(ctx, g.ID)
		if err != nil {
			t.Fatalf("GetBalances failed: %v", err)
		}
		verified, err := engine.VerifiedBalances(ctx, g.ID)
		if err != nil {
			t.Fatalf("VerifiedBalances failed: %v", err)
		}

		// Sort both slices by UserID or map them to compare
		cMap := make(map[string]int64)
		for _, b := range cached {
			cMap[b.UserID] = b.NetBalanceCents
		}
		vMap := make(map[string]int64)
		for _, b := range verified {
			vMap[b.UserID] = b.NetBalanceCents
		}

		if !reflect.DeepEqual(cMap, vMap) {
			t.Fatalf("balances do not match!\nCached: %v\nVerified: %v", cMap, vMap)
		}
	}

	// 3. Add Expense 1: Alice pays 1000, split evenly (250 each)
	_, err := engine.AddExpense(ctx, ledger.AddExpenseInput{
		GroupID:      g.ID,
		Description:  "Dinner",
		TotalCents:   1000,
		Currency:     "USD",
		PaidByUserID: u1.ID,
	})
	if err != nil {
		t.Fatalf("AddExpense failed: %v", err)
	}
	assertBalancesMatch()

	// 4. Add Expense 2: Bob pays 500, explicit split (Bob 200, Charlie 300)
	_, err = engine.AddExpense(ctx, ledger.AddExpenseInput{
		GroupID:      g.ID,
		Description:  "Drinks",
		TotalCents:   500,
		Currency:     "USD",
		PaidByUserID: u2.ID,
		Splits: []ledger.SplitInput{
			{UserID: u2.ID, ShareAmountCents: 200},
			{UserID: u3.ID, ShareAmountCents: 300},
		},
	})
	if err != nil {
		t.Fatalf("AddExpense failed: %v", err)
	}
	assertBalancesMatch()

	// 5. Settlement: Charlie pays Alice 300
	_, err = engine.RecordSettlement(ctx, ledger.RecordSettlementInput{
		GroupID:     g.ID,
		FromUserID:  u3.ID,
		ToUserID:    u1.ID,
		AmountCents: 300,
	})
	if err != nil {
		t.Fatalf("RecordSettlement failed: %v", err)
	}
	assertBalancesMatch()
}

func TestConcurrentWrites(t *testing.T) {
	pool, engine := setupTestDB(t)
	defer pool.Close()
	ctx := context.Background()

	u1, _ := engine.CreateUser(ctx, "Alice", "alice2@test.com")
	u2, _ := engine.CreateUser(ctx, "Bob", "bob2@test.com")
	g, _ := engine.CreateGroup(ctx, "Trip", "USD", []string{u1.ID, u2.ID})

	const numReqs = 20
	var successes int32
	var conflicts int32

	var wg sync.WaitGroup
	wg.Add(numReqs)

	for i := 0; i < numReqs; i++ {
		go func(idx int) {
			defer wg.Done()
			_, err := engine.AddExpense(ctx, ledger.AddExpenseInput{
				GroupID:      g.ID,
				Description:  "Concurrent",
				TotalCents:   100,
				Currency:     "USD",
				PaidByUserID: u1.ID,
			})
			if err == nil {
				atomic.AddInt32(&successes, 1)
			} else if errors.Is(err, ledger.ErrConcurrentUpdate) {
				atomic.AddInt32(&conflicts, 1)
			} else {
				t.Errorf("unexpected error: %v", err)
			}
		}(i)
	}

	wg.Wait()

	if successes+conflicts != numReqs {
		t.Errorf("expected %d total responses, got %d successes and %d conflicts", numReqs, successes, conflicts)
	}
	if successes == 0 {
		t.Errorf("expected at least one success")
	}

	// Verify balances
	balances, err := engine.GetBalances(ctx, g.ID)
	if err != nil {
		t.Fatalf("GetBalances failed: %v", err)
	}
	var aliceBal, bobBal int64
	for _, b := range balances {
		if b.UserID == u1.ID {
			aliceBal = b.NetBalanceCents
		} else if b.UserID == u2.ID {
			bobBal = b.NetBalanceCents
		}
	}

	expectedAlice := int64(successes) * 50 // Owed 50 per success
	expectedBob := int64(successes) * -50  // Owes 50 per success

	if aliceBal != expectedAlice || bobBal != expectedBob {
		t.Errorf("balances incorrect. Alice: got %d, want %d. Bob: got %d, want %d", aliceBal, expectedAlice, bobBal, expectedBob)
	}
}

func TestIdempotency(t *testing.T) {
	pool, engine := setupTestDB(t)
	defer pool.Close()
	ctx := context.Background()

	u1, _ := engine.CreateUser(ctx, "A", uuid.NewString()+"@test.com")
	u2, _ := engine.CreateUser(ctx, "B", uuid.NewString()+"@test.com")
	g, _ := engine.CreateGroup(ctx, "G", "USD", []string{u1.ID, u2.ID})

	idempotencyKey := "key-123"

	// Call 1
	e1, err := engine.AddExpense(ctx, ledger.AddExpenseInput{
		GroupID:        g.ID,
		Description:    "Test",
		TotalCents:     100,
		Currency:       "USD",
		PaidByUserID:   u1.ID,
		IdempotencyKey: &idempotencyKey,
	})
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}

	// Call 2 - exact duplicate
	e2, err := engine.AddExpense(ctx, ledger.AddExpenseInput{
		GroupID:        g.ID,
		Description:    "Test",
		TotalCents:     100,
		Currency:       "USD",
		PaidByUserID:   u1.ID,
		IdempotencyKey: &idempotencyKey,
	})
	if !errors.Is(err, ledger.ErrDuplicate) {
		t.Fatalf("expected ErrDuplicate, got %v", err)
	}
	if e2.ID != e1.ID {
		t.Errorf("expected same ID %s, got %s", e1.ID, e2.ID)
	}

	// Call 3 - diff body
	_, err = engine.AddExpense(ctx, ledger.AddExpenseInput{
		GroupID:        g.ID,
		Description:    "Different",
		TotalCents:     200,
		Currency:       "USD",
		PaidByUserID:   u1.ID,
		IdempotencyKey: &idempotencyKey,
	})
	if !errors.Is(err, ledger.ErrIdempotencyMismatch) {
		t.Errorf("expected ErrIdempotencyMismatch on different payload, got %v", err)
	}
}
