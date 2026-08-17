package api

import "github.com/Zubimendi/splitstack/internal/ledger"

type createUserRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type createGroupRequest struct {
	Name          string   `json:"name"`
	Currency      string   `json:"currency"`
	MemberUserIDs []string `json:"memberUserIds"`
}

type addExpenseRequest struct {
	Description    string              `json:"description"`
	TotalCents     int64               `json:"totalAmountCents"`
	Currency       string              `json:"currency"`
	PaidByUserID   string              `json:"paidByUserId"`
	IdempotencyKey *string             `json:"idempotencyKey,omitempty"`
	Splits         []ledger.SplitInput `json:"splits"`
}

type recordSettlementRequest struct {
	FromUserID     string  `json:"fromUserId"`
	ToUserID       string  `json:"toUserId"`
	AmountCents    int64   `json:"amountCents"`
	IdempotencyKey *string `json:"idempotencyKey,omitempty"`
}

type errorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}
