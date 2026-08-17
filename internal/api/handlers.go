package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/Zubimendi/splitstack/internal/ledger"
	"github.com/Zubimendi/splitstack/internal/observability"
)

type Handlers struct {
	engine *ledger.Engine
	log    *zap.Logger
}

func NewHandlers(engine *ledger.Engine, log *zap.Logger) *Handlers {
	return &Handlers{engine: engine, log: log}
}

func (h *Handlers) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.Email == "" {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "name and email are required")
		return
	}
	user, err := h.engine.CreateUser(r.Context(), req.Name, req.Email)
	if err != nil {
		writeError(w, http.StatusConflict, "CREATE_FAILED", "failed to create user (email may already be in use)")
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

func (h *Handlers) CreateGroup(w http.ResponseWriter, r *http.Request) {
	var req createGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || len(req.MemberUserIDs) == 0 {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "name and at least one memberUserId are required")
		return
	}
	if req.Currency == "" {
		req.Currency = "USD"
	}
	group, err := h.engine.CreateGroup(r.Context(), req.Name, req.Currency, req.MemberUserIDs)
	if err != nil {
		h.log.Error("create group failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to create group")
		return
	}
	writeJSON(w, http.StatusCreated, group)
}

func (h *Handlers) AddExpense(w http.ResponseWriter, r *http.Request) {
	groupID := chi.URLParam(r, "groupId")
	var req addExpenseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "request body must be valid JSON")
		return
	}
	if req.Description == "" || req.TotalCents <= 0 || req.PaidByUserID == "" {
		writeError(w, http.StatusBadRequest, "MISSING_FIELDS", "description, totalAmountCents, and paidByUserId are required")
		return
	}
	if req.Currency == "" {
		req.Currency = "USD"
	}

	splits := make([]ledger.SplitInput, len(req.Splits))
	for i, s := range req.Splits {
		splits[i] = ledger.SplitInput{UserID: s.UserID, ShareAmountCents: s.ShareAmountCents}
	}

	expense, err := h.engine.AddExpense(r.Context(), ledger.AddExpenseInput{
		GroupID: groupID, Description: req.Description, TotalCents: req.TotalCents,
		Currency: req.Currency, PaidByUserID: req.PaidByUserID, IdempotencyKey: req.IdempotencyKey, Splits: splits,
	})

	if err != nil && err != ledger.ErrDuplicate {
		h.respondExpenseError(w, err)
		return
	}

	status := http.StatusCreated
	if err == ledger.ErrDuplicate {
		status = http.StatusOK
	} else {
		observability.ExpensesAdded.Inc()
	}
	writeJSON(w, status, expense)
}

func (h *Handlers) RecordSettlement(w http.ResponseWriter, r *http.Request) {
	groupID := chi.URLParam(r, "groupId")
	var req recordSettlementRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.FromUserID == "" || req.ToUserID == "" || req.AmountCents <= 0 {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "fromUserId, toUserId, and a positive amountCents are required")
		return
	}

	settlement, err := h.engine.RecordSettlement(r.Context(), ledger.RecordSettlementInput{
		GroupID: groupID, FromUserID: req.FromUserID, ToUserID: req.ToUserID,
		AmountCents: req.AmountCents, IdempotencyKey: req.IdempotencyKey,
	})

	if err != nil && err != ledger.ErrDuplicate {
		h.respondExpenseError(w, err)
		return
	}

	status := http.StatusCreated
	if err == ledger.ErrDuplicate {
		status = http.StatusOK
	} else {
		observability.SettlementsRecorded.Inc()
	}
	writeJSON(w, status, settlement)
}

func (h *Handlers) GetBalances(w http.ResponseWriter, r *http.Request) {
	groupID := chi.URLParam(r, "groupId")
	balances, err := h.engine.GetBalances(r.Context(), groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to load balances")
		return
	}
	writeJSON(w, http.StatusOK, balances)
}

func (h *Handlers) GetVerifiedBalances(w http.ResponseWriter, r *http.Request) {
	groupID := chi.URLParam(r, "groupId")
	balances, err := h.engine.VerifiedBalances(r.Context(), groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to compute verified balances")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"balances": balances,
		"note":     "recomputed directly from expenses/settlements; bypasses the group_balances cache entirely",
	})
}

func (h *Handlers) GetSettlementPlan(w http.ResponseWriter, r *http.Request) {
	groupID := chi.URLParam(r, "groupId")
	balances, err := h.engine.GetBalances(r.Context(), groupID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to load balances")
		return
	}
	plan := ledger.ComputeSettlementPlan(balances)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"suggestedPayments": plan,
		"note":              "a greedy debt-simplification heuristic - minimizes transaction count well in practice, not certified globally optimal. See docs/ARCHITECTURE.md.",
	})
}

func (h *Handlers) respondExpenseError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ledger.ErrSplitsDontSum):
		writeError(w, http.StatusUnprocessableEntity, "SPLITS_DONT_SUM", err.Error())
	case errors.Is(err, ledger.ErrNotGroupMember):
		writeError(w, http.StatusUnprocessableEntity, "NOT_GROUP_MEMBER", err.Error())
	case errors.Is(err, ledger.ErrConcurrentUpdate):
		observability.ConcurrentUpdateConflicts.Inc()
		writeError(w, http.StatusConflict, "CONCURRENT_UPDATE", err.Error())
	case errors.Is(err, ledger.ErrNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
	default:
		h.log.Error("unexpected error", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "unexpected error")
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{Error: message, Code: code})
}
