package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"go.uber.org/zap"

	"github.com/Zubimendi/splitstack/internal/config"
	"github.com/Zubimendi/splitstack/internal/db"
	"github.com/Zubimendi/splitstack/internal/ledger"
)

func main() {
	cfg := config.Load()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer pool.Close()

	logger, _ := zap.NewDevelopment()
	engine := ledger.NewEngine(pool, logger)

	fmt.Println("Seeding data...")

	u1, _ := engine.CreateUser(ctx, "Alice", "alice@example.com", "password123")
	u2, _ := engine.CreateUser(ctx, "Bob", "bob@example.com", "password123")
	u3, _ := engine.CreateUser(ctx, "Charlie", "charlie@example.com", "password123")

	g, _ := engine.CreateGroup(ctx, "Weekend Trip", "USD", []string{u1.ID, u2.ID, u3.ID})

	fmt.Printf("Created Group: %s\n", g.ID)

	_, err = engine.AddExpense(ctx, ledger.AddExpenseInput{
		GroupID:      g.ID,
		Description:  "Dinner",
		TotalCents:   12000, // $120.00
		Currency:     "USD",
		PaidByUserID: u1.ID,
	})
	if err != nil {
		log.Printf("expense error: %v", err)
	}

	_, err = engine.AddExpense(ctx, ledger.AddExpenseInput{
		GroupID:      g.ID,
		Description:  "Drinks",
		TotalCents:   4500, // $45.00
		Currency:     "USD",
		PaidByUserID: u2.ID,
		Splits: []ledger.SplitInput{
			{UserID: u2.ID, ShareAmountCents: 1500},
			{UserID: u3.ID, ShareAmountCents: 3000},
		},
	})
	if err != nil {
		log.Printf("expense error: %v", err)
	}

	fmt.Println("Done seeding.")
}
