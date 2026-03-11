package accounts

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/ecorouter/ecorouter/internal/db"
	"github.com/ecorouter/ecorouter/internal/openrouter"
	"golang.org/x/crypto/bcrypt"
)

type Service struct {
	db             *db.DB
	or             *openrouter.Client
	depositAddress string
	ecoPercent     float64
	opsPercent     float64
	txVerifier     TxVerifier
}

func NewService(database *db.DB, orClient *openrouter.Client, depositAddress string, ecoPercent, opsPercent float64) *Service {
	return &Service{
		db:             database,
		or:             orClient,
		depositAddress: depositAddress,
		ecoPercent:     ecoPercent,
		opsPercent:     opsPercent,
	}
}

type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type RegisterResponse struct {
	APIKey         string  `json:"api_key"`
	DepositAddress string  `json:"deposit_address"`
	DepositHint    string  `json:"deposit_hint"`
	BalanceUSDT    float64 `json:"balance_usdt"`
	APICreditUSDT  float64 `json:"api_credit_usdt"`
}

func (s *Service) HandleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if req.Email == "" {
		http.Error(w, `{"error":"email is required"}`, http.StatusBadRequest)
		return
	}
	if req.Password == "" || len(req.Password) < 6 {
		http.Error(w, `{"error":"password must be at least 6 characters"}`, http.StatusBadRequest)
		return
	}

	// Hash password
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("ERROR: hash password: %v", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	// Get unique cents suffix
	suffix, err := s.db.NextCentsSuffix()
	if err != nil {
		log.Printf("ERROR: next cents suffix: %v", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	// Create user in DB
	user, err := s.db.CreateUser(req.Email, suffix, string(hash))
	if err != nil {
		log.Printf("ERROR: create user: %v", err)
		http.Error(w, `{"error":"email already registered or internal error"}`, http.StatusConflict)
		return
	}

	// Create OpenRouter key (async-safe: if this fails, user exists but has no OR key)
	keyName := fmt.Sprintf("ecorouter_%d", user.ID)
	orKey, err := s.or.CreateKey(keyName, 0)
	if err != nil {
		log.Printf("ERROR: create openrouter key for user %d: %v", user.ID, err)
		// User created but OR key failed — will retry or admin fixes
		// Still return the eco key so user can start
	} else {
		// Store OR key mapping
		if err := s.db.UpdateUserORKey(user.ID, orKey.Data.Hash, orKey.Key); err != nil {
			log.Printf("ERROR: store OR key for user %d: %v", user.ID, err)
		}
	}

	hint := fmt.Sprintf("Send any whole dollar amount + $0.%03d cents (e.g. $10.%03d, $25.%03d)", suffix, suffix, suffix)

	resp := RegisterResponse{
		APIKey:         user.EcoAPIKey,
		DepositAddress: s.depositAddress,
		DepositHint:    hint,
		BalanceUSDT:    0,
		APICreditUSDT:  0,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}
