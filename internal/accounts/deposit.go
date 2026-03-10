package accounts

import (
	"encoding/json"
	"log"
	"math"
	"net/http"
)

type NotifyDepositRequest struct {
	TxHash string `json:"tx_hash"`
}

type NotifyDepositResponse struct {
	Status         string  `json:"status"`
	Message        string  `json:"message,omitempty"`
	AmountUSDT     float64 `json:"amount_usdt,omitempty"`
	EcoFeeUSDT     float64 `json:"eco_fee_usdt,omitempty"`
	OpsFeeUSDT     float64 `json:"ops_fee_usdt,omitempty"`
	APICreditAdded float64 `json:"api_credit_added_usdt,omitempty"`
	APICreditTotal float64 `json:"api_credit_total_usdt,omitempty"`
}

// TxVerifier verifies transactions on-chain. Implemented by crypto.Watcher.
type TxVerifier interface {
	VerifyAndProcess(txHash string) (status string, err error)
}

// SetTxVerifier sets the transaction verifier (called from main after watcher is created).
func (s *Service) SetTxVerifier(v TxVerifier) {
	s.txVerifier = v
}

// ProcessDeposit handles the fee split and OR key limit update for a confirmed deposit.
func (s *Service) ProcessDeposit(userID int64, amountUSDT float64, txHash string) (*NotifyDepositResponse, error) {
	ecoFee := roundCents(amountUSDT * s.ecoPercent / 100)
	opsFee := roundCents(amountUSDT * s.opsPercent / 100)
	apiCredit := roundCents(amountUSDT - ecoFee - opsFee)

	// Record in DB
	if err := s.db.RecordDeposit(userID, amountUSDT, ecoFee, opsFee, apiCredit, txHash); err != nil {
		return nil, err
	}

	// Update OR key limit
	user, err := s.db.GetUserByID(userID)
	if err != nil {
		return nil, err
	}

	if user.ORKeyHash != "" {
		if err := s.or.UpdateKeyLimit(user.ORKeyHash, user.TotalAPICreditUSDT); err != nil {
			log.Printf("ERROR: update OR key limit for user %d: %v", userID, err)
		}
	}

	return &NotifyDepositResponse{
		Status:         "credited",
		AmountUSDT:     amountUSDT,
		EcoFeeUSDT:     ecoFee,
		OpsFeeUSDT:     opsFee,
		APICreditAdded: apiCredit,
		APICreditTotal: user.TotalAPICreditUSDT,
	}, nil
}

func (s *Service) HandleNotifyDeposit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	ecoKey := extractBearerToken(r)
	if ecoKey == "" {
		http.Error(w, `{"error":"missing api key"}`, http.StatusUnauthorized)
		return
	}

	_, err := s.db.GetUserByEcoKey(ecoKey)
	if err != nil {
		http.Error(w, `{"error":"invalid api key"}`, http.StatusUnauthorized)
		return
	}

	var req NotifyDepositRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.TxHash == "" {
		http.Error(w, `{"error":"tx_hash is required"}`, http.StatusBadRequest)
		return
	}

	if s.txVerifier == nil {
		resp := NotifyDepositResponse{
			Status:  "pending",
			Message: "transaction verification not configured",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}

	status, err := s.txVerifier.VerifyAndProcess(req.TxHash)
	if err != nil {
		log.Printf("ERROR: verify tx %s: %v", req.TxHash, err)
		resp := NotifyDepositResponse{
			Status:  "not_found",
			Message: "transaction not found on TronGrid, try again in a few seconds",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}

	resp := NotifyDepositResponse{Status: status, Message: "transaction processed"}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

type ClaimDepositRequest struct {
	TxHash string `json:"tx_hash"`
}

func (s *Service) HandleClaimDeposit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	ecoKey := extractBearerToken(r)
	if ecoKey == "" {
		http.Error(w, `{"error":"missing api key"}`, http.StatusUnauthorized)
		return
	}

	user, err := s.db.GetUserByEcoKey(ecoKey)
	if err != nil {
		http.Error(w, `{"error":"invalid api key"}`, http.StatusUnauthorized)
		return
	}

	var req ClaimDepositRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.TxHash == "" {
		http.Error(w, `{"error":"tx_hash is required"}`, http.StatusBadRequest)
		return
	}

	amount, err := s.db.ClaimUnmatchedDeposit(req.TxHash, user.ID)
	if err != nil {
		log.Printf("ERROR: claim deposit tx=%s user=%d: %v", req.TxHash, user.ID, err)
		http.Error(w, `{"error":"deposit not found or already claimed"}`, http.StatusNotFound)
		return
	}

	resp, err := s.ProcessDeposit(user.ID, amount, req.TxHash)
	if err != nil {
		log.Printf("ERROR: process claimed deposit tx=%s user=%d: %v", req.TxHash, user.ID, err)
		http.Error(w, `{"error":"failed to process deposit"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func roundCents(v float64) float64 {
	return math.Round(v*100) / 100
}
