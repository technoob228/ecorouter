package accounts

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

type BalanceResponse struct {
	TotalDepositedUSDT  float64 `json:"total_deposited_usdt"`
	EcoContributedUSDT  float64 `json:"eco_contributed_usdt"`
	OpsFeeUSDT          float64 `json:"ops_fee_usdt"`
	APICreditTotalUSDT  float64 `json:"api_credit_total_usdt"`
	APICreditUsedUSDT   float64 `json:"api_credit_used_usdt"`
	APICreditRemainUSDT float64 `json:"api_credit_remaining_usdt"`
}

func (s *Service) HandleBalance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
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

	// Try to get live usage from OpenRouter
	var usedUSDT float64
	if user.ORKeyHash != "" {
		info, err := s.or.GetKeyInfo(user.ORKeyHash)
		if err != nil {
			log.Printf("WARN: get OR key info for user %d: %v", user.ID, err)
		} else {
			usedUSDT = info.Data.Usage
		}
	}

	resp := BalanceResponse{
		TotalDepositedUSDT:  user.TotalDepositedUSDT,
		EcoContributedUSDT:  user.TotalEcoUSDT,
		OpsFeeUSDT:          user.TotalOpsUSDT,
		APICreditTotalUSDT:  user.TotalAPICreditUSDT,
		APICreditUsedUSDT:   usedUSDT,
		APICreditRemainUSDT: user.TotalAPICreditUSDT - usedUSDT,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}
