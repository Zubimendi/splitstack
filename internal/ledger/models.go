package ledger

import "time"

type User struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type Group struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Currency string `json:"currency"`
}

type SplitInput struct {
	UserID           string `json:"userId"`
	ShareAmountCents int64  `json:"shareAmountCents"`
}

type AddExpenseInput struct {
	GroupID         string
	Description     string
	TotalCents      int64
	Currency        string
	PaidByUserID    string
	IdempotencyKey  *string
	// If Splits is empty, the amount is divided EVENLY among every
	// current group member (see engine.go's splitEvenly for how integer
	// remainder cents are allocated deterministically). If provided,
	// Splits must sum to exactly TotalCents - validated before anything
	// is written.
	Splits []SplitInput
}

type Expense struct {
	ID           string       `json:"id"`
	GroupID      string       `json:"groupId"`
	Description  string       `json:"description"`
	TotalCents   int64        `json:"totalAmountCents"`
	Currency     string       `json:"currency"`
	PaidByUserID string       `json:"paidByUserId"`
	CreatedAt    time.Time    `json:"createdAt"`
	Splits       []SplitInput `json:"splits"`
}

type RecordSettlementInput struct {
	GroupID        string
	FromUserID     string // who paid
	ToUserID       string // who received
	AmountCents    int64
	IdempotencyKey *string
}

type Settlement struct {
	ID          string    `json:"id"`
	GroupID     string    `json:"groupId"`
	FromUserID  string    `json:"fromUserId"`
	ToUserID    string    `json:"toUserId"`
	AmountCents int64     `json:"amountCents"`
	CreatedAt   time.Time `json:"createdAt"`
}

type Balance struct {
	UserID          string `json:"userId"`
	NetBalanceCents int64  `json:"netBalanceCents"` // positive = owed money by the group; negative = owes the group
}

// SuggestedPayment is one payment in a debt-simplification plan: FromUserID
// should pay ToUserID AmountCents to help settle the group.
type SuggestedPayment struct {
	FromUserID  string `json:"fromUserId"`
	ToUserID    string `json:"toUserId"`
	AmountCents int64  `json:"amountCents"`
}
