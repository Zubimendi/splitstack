package ledger

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

type Engine struct {
	db  *pgxpool.Pool
	log *zap.Logger
}

func NewEngine(db *pgxpool.Pool, log *zap.Logger) *Engine {
	return &Engine{db: db, log: log}
}

func (e *Engine) CreateUser(ctx context.Context, name, email string) (User, error) {
	var u User
	err := e.db.QueryRow(ctx, `
		INSERT INTO users (name, email) VALUES ($1, $2)
		RETURNING id, name, email
	`, name, email).Scan(&u.ID, &u.Name, &u.Email)
	if err != nil {
		return User{}, err
	}
	return u, nil
}

func (e *Engine) GetUsers(ctx context.Context) ([]User, error) {
	rows, err := e.db.Query(ctx, `SELECT id, name, email FROM users`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Name, &u.Email); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}

func (e *Engine) GetUser(ctx context.Context, userID string) (User, error) {
	var u User
	err := e.db.QueryRow(ctx, `SELECT id, name, email FROM users WHERE id = $1`, userID).Scan(&u.ID, &u.Name, &u.Email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, err
	}
	return u, nil
}

func (e *Engine) CreateGroup(ctx context.Context, name, currency string, memberUserIDs []string) (Group, error) {
	tx, err := e.db.Begin(ctx)
	if err != nil {
		return Group{}, err
	}
	defer tx.Rollback(ctx)

	var g Group
	err = tx.QueryRow(ctx, `
		INSERT INTO groups (name, currency) VALUES ($1, $2)
		RETURNING id, name, currency
	`, name, currency).Scan(&g.ID, &g.Name, &g.Currency)
	if err != nil {
		return Group{}, err
	}

	for _, uid := range memberUserIDs {
		_, err = tx.Exec(ctx, `
			INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)
		`, g.ID, uid)
		if err != nil {
			return Group{}, err
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO group_balances (group_id, user_id, net_balance_cents, version)
			VALUES ($1, $2, 0, 1)
		`, g.ID, uid)
		if err != nil {
			return Group{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Group{}, err
	}

	return g, nil
}

func (e *Engine) GetGroups(ctx context.Context) ([]Group, error) {
	rows, err := e.db.Query(ctx, `SELECT id, name, currency FROM groups`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name, &g.Currency); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, nil
}

func (e *Engine) GetGroup(ctx context.Context, groupID string) (Group, error) {
	var g Group
	err := e.db.QueryRow(ctx, `SELECT id, name, currency FROM groups WHERE id = $1`, groupID).Scan(&g.ID, &g.Name, &g.Currency)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Group{}, ErrNotFound
		}
		return Group{}, err
	}
	return g, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

func computePayloadHash(payload interface{}) string {
	b, _ := json.Marshal(payload)
	hash := sha256.Sum256(b)
	return fmt.Sprintf("%x", hash)
}

func (e *Engine) AddExpense(ctx context.Context, input AddExpenseInput) (Expense, error) {
	// First, lookup group members to validate splits and compute even split if needed
	var groupCurrency string
	err := e.db.QueryRow(ctx, `SELECT currency FROM groups WHERE id = $1`, input.GroupID).Scan(&groupCurrency)
	if err != nil {
		return Expense{}, ErrNotFound
	}
	if input.Currency != "" && input.Currency != groupCurrency {
		return Expense{}, ErrCurrencyMismatch
	}
	input.Currency = groupCurrency

	rows, err := e.db.Query(ctx, `SELECT user_id FROM group_members WHERE group_id = $1`, input.GroupID)
	if err != nil {
		return Expense{}, err
	}
	defer rows.Close()

	members := make(map[string]bool)
	var memberIDs []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return Expense{}, err
		}
		members[uid] = true
		memberIDs = append(memberIDs, uid)
	}

	if len(members) == 0 {
		return Expense{}, ErrNotFound
	}

	if len(input.Splits) == 0 {
		input.Splits = splitEvenly(input.TotalCents, memberIDs)
	} else {
		if err := validateSplits(input.Splits, input.TotalCents, members); err != nil {
			return Expense{}, err
		}
	}

	tx, err := e.db.Begin(ctx)
	if err != nil {
		return Expense{}, err
	}
	defer tx.Rollback(ctx)

	// Build the idempotent key format: key|hash
	var idempKey string
	if input.IdempotencyKey != nil {
		// Create a hash of the relevant payload to detect mismatch
		hash := computePayloadHash(struct {
			Desc   string
			Amount int64
			Payer  string
			Splits []SplitInput
		}{input.Description, input.TotalCents, input.PaidByUserID, input.Splits})
		idempKey = *input.IdempotencyKey + "|" + hash
	}

	var expenseID string
	var createdAt time.Time
	err = tx.QueryRow(ctx, `
		INSERT INTO expenses (group_id, description, total_amount_cents, currency, paid_by_user_id, idempotency_key)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at
	`, input.GroupID, input.Description, input.TotalCents, input.Currency, input.PaidByUserID, nullString(idempKey)).Scan(&expenseID, &createdAt)

	if err != nil {
		if isUniqueViolation(err) {
			// Idempotency check. See if the key matches but hash is different
			var existingKey string
			var existingID string
			errFind := e.db.QueryRow(ctx, `SELECT id, idempotency_key FROM expenses WHERE group_id = $1 AND idempotency_key LIKE $2`, input.GroupID, *input.IdempotencyKey+"|%").Scan(&existingID, &existingKey)
			if errFind == nil {
				if existingKey != idempKey {
					return Expense{}, ErrIdempotencyMismatch
				}
				// It's an exact duplicate
				// We should fetch the full expense here, but for brevity just returning ID is usually enough or we fetch it properly.
				return Expense{ID: existingID, GroupID: input.GroupID}, ErrDuplicate
			}
		}
		return Expense{}, err
	}

	// Insert splits
	for _, split := range input.Splits {
		_, err = tx.Exec(ctx, `
			INSERT INTO expense_splits (expense_id, user_id, share_amount_cents)
			VALUES ($1, $2, $3)
		`, expenseID, split.UserID, split.ShareAmountCents)
		if err != nil {
			return Expense{}, err
		}
	}

	// Calculate net balance changes
	// Payer gets +TotalCents. Everyone else gets -ShareAmountCents
	changes := make(map[string]int64)
	changes[input.PaidByUserID] += input.TotalCents
	for _, split := range input.Splits {
		changes[split.UserID] -= split.ShareAmountCents
	}

	for uid, change := range changes {
		if change == 0 {
			continue
		}
		err = e.updateBalance(ctx, tx, input.GroupID, uid, change)
		if err != nil {
			return Expense{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Expense{}, err
	}

	return Expense{
		ID:           expenseID,
		GroupID:      input.GroupID,
		Description:  input.Description,
		TotalCents:   input.TotalCents,
		Currency:     input.Currency,
		PaidByUserID: input.PaidByUserID,
		CreatedAt:    createdAt,
		Splits:       input.Splits,
	}, nil
}

func (e *Engine) updateBalance(ctx context.Context, tx pgx.Tx, groupID, userID string, deltaCents int64) error {
	var currentVersion int
	var currentBalance int64
	err := tx.QueryRow(ctx, `SELECT net_balance_cents, version FROM group_balances WHERE group_id = $1 AND user_id = $2`, groupID, userID).Scan(&currentBalance, &currentVersion)
	if err != nil {
		return err
	}

	res, err := tx.Exec(ctx, `
		UPDATE group_balances 
		SET net_balance_cents = $1, version = version + 1, updated_at = now()
		WHERE group_id = $2 AND user_id = $3 AND version = $4
	`, currentBalance+deltaCents, groupID, userID, currentVersion)
	
	if err != nil {
		return err
	}

	if res.RowsAffected() == 0 {
		return ErrConcurrentUpdate
	}

	return nil
}

func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}


func (e *Engine) RecordSettlement(ctx context.Context, input RecordSettlementInput) (Settlement, error) {
	tx, err := e.db.Begin(ctx)
	if err != nil {
		return Settlement{}, err
	}
	defer tx.Rollback(ctx)

	var idempKey string
	if input.IdempotencyKey != nil {
		hash := computePayloadHash(struct {
			From   string
			To     string
			Amount int64
		}{input.FromUserID, input.ToUserID, input.AmountCents})
		idempKey = *input.IdempotencyKey + "|" + hash
	}

	var settlementID string
	var createdAt time.Time
	err = tx.QueryRow(ctx, `
		INSERT INTO settlements (group_id, from_user_id, to_user_id, amount_cents, idempotency_key)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at
	`, input.GroupID, input.FromUserID, input.ToUserID, input.AmountCents, nullString(idempKey)).Scan(&settlementID, &createdAt)

	if err != nil {
		if isUniqueViolation(err) {
			var existingKey string
			var existingID string
			errFind := e.db.QueryRow(ctx, `SELECT id, idempotency_key FROM settlements WHERE group_id = $1 AND idempotency_key LIKE $2`, input.GroupID, *input.IdempotencyKey+"|%").Scan(&existingID, &existingKey)
			if errFind == nil {
				if existingKey != idempKey {
					return Settlement{}, ErrIdempotencyMismatch
				}
				return Settlement{ID: existingID, GroupID: input.GroupID}, ErrDuplicate
			}
		}
		return Settlement{}, err
	}

	// From pays To.
	// From (debtor) balance goes up (closer to 0) -> +AmountCents
	// To (creditor) balance goes down (closer to 0) -> -AmountCents
	if err := e.updateBalance(ctx, tx, input.GroupID, input.FromUserID, input.AmountCents); err != nil {
		return Settlement{}, err
	}
	if err := e.updateBalance(ctx, tx, input.GroupID, input.ToUserID, -input.AmountCents); err != nil {
		return Settlement{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Settlement{}, err
	}

	return Settlement{
		ID:          settlementID,
		GroupID:     input.GroupID,
		FromUserID:  input.FromUserID,
		ToUserID:    input.ToUserID,
		AmountCents: input.AmountCents,
		CreatedAt:   createdAt,
	}, nil
}

func (e *Engine) GetBalances(ctx context.Context, groupID string) ([]Balance, error) {
	rows, err := e.db.Query(ctx, `
		SELECT user_id, net_balance_cents
		FROM group_balances
		WHERE group_id = $1
	`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var balances []Balance
	for rows.Next() {
		var b Balance
		if err := rows.Scan(&b.UserID, &b.NetBalanceCents); err != nil {
			return nil, err
		}
		balances = append(balances, b)
	}
	return balances, nil
}

func (e *Engine) VerifiedBalances(ctx context.Context, groupID string) ([]Balance, error) {
	// Recompute from expenses and settlements
	rows, err := e.db.Query(ctx, `
		WITH expense_credits AS (
			SELECT paid_by_user_id as user_id, SUM(total_amount_cents) as amount
			FROM expenses WHERE group_id = $1 GROUP BY paid_by_user_id
		),
		expense_debits AS (
			SELECT user_id, SUM(share_amount_cents) as amount
			FROM expense_splits JOIN expenses ON expenses.id = expense_splits.expense_id
			WHERE expenses.group_id = $1 GROUP BY user_id
		),
		settlement_credits AS (
			SELECT from_user_id as user_id, SUM(amount_cents) as amount
			FROM settlements WHERE group_id = $1 GROUP BY from_user_id
		),
		settlement_debits AS (
			SELECT to_user_id as user_id, SUM(amount_cents) as amount
			FROM settlements WHERE group_id = $1 GROUP BY to_user_id
		)
		SELECT 
			m.user_id,
			COALESCE(ec.amount, 0) - COALESCE(ed.amount, 0) + COALESCE(sc.amount, 0) - COALESCE(sd.amount, 0) as net_balance_cents
		FROM group_members m
		LEFT JOIN expense_credits ec ON m.user_id = ec.user_id
		LEFT JOIN expense_debits ed ON m.user_id = ed.user_id
		LEFT JOIN settlement_credits sc ON m.user_id = sc.user_id
		LEFT JOIN settlement_debits sd ON m.user_id = sd.user_id
		WHERE m.group_id = $1
	`, groupID)
	
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var balances []Balance
	for rows.Next() {
		var b Balance
		if err := rows.Scan(&b.UserID, &b.NetBalanceCents); err != nil {
			return nil, err
		}
		balances = append(balances, b)
	}
	return balances, nil
}
