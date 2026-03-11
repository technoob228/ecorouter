package accounts

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"golang.org/x/crypto/bcrypt"
)

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginResponse struct {
	APIKey         string  `json:"api_key"`
	DepositAddress string  `json:"deposit_address"`
	DepositHint    string  `json:"deposit_hint"`
	BalanceUSDT    float64 `json:"balance_usdt"`
	APICreditUSDT  float64 `json:"api_credit_usdt"`
}

func (s *Service) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.Email == "" {
		http.Error(w, `{"error":"email is required"}`, http.StatusBadRequest)
		return
	}

	user, err := s.db.GetUserByEmail(req.Email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, `{"error":"account not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	// Verify password
	if req.Password == "" {
		http.Error(w, `{"error":"password is required"}`, http.StatusBadRequest)
		return
	}
	if user.PasswordHash == "" {
		http.Error(w, `{"error":"password not set, please contact support"}`, http.StatusForbidden)
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		http.Error(w, `{"error":"invalid password"}`, http.StatusUnauthorized)
		return
	}

	hint := fmt.Sprintf("Send any whole dollar amount + $0.%03d cents (e.g. $10.%03d, $25.%03d)",
		user.DepositCentsSuffix, user.DepositCentsSuffix, user.DepositCentsSuffix)

	resp := LoginResponse{
		APIKey:         user.EcoAPIKey,
		DepositAddress: s.depositAddress,
		DepositHint:    hint,
		BalanceUSDT:    user.TotalDepositedUSDT,
		APICreditUSDT:  user.TotalAPICreditUSDT,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
