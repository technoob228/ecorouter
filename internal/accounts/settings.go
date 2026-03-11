package accounts

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/ecorouter/ecorouter/internal/db"
	"golang.org/x/crypto/bcrypt"
)

type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type ChangePasswordResponse struct {
	OK bool `json:"ok"`
}

func (s *Service) HandleChangePassword(w http.ResponseWriter, r *http.Request) {
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

	var req ChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if len(req.NewPassword) < 6 {
		http.Error(w, `{"error":"new_password must be at least 6 characters"}`, http.StatusBadRequest)
		return
	}

	// For accounts with existing password, verify current password
	if user.PasswordHash != "" {
		if req.CurrentPassword == "" {
			http.Error(w, `{"error":"current_password is required"}`, http.StatusBadRequest)
			return
		}
		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.CurrentPassword)); err != nil {
			http.Error(w, `{"error":"invalid current password"}`, http.StatusUnauthorized)
			return
		}
	}

	// Hash new password
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("ERROR: hash password: %v", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	if err := s.db.UpdateUserPassword(user.ID, string(hash)); err != nil {
		log.Printf("ERROR: update password for user %d: %v", user.ID, err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ChangePasswordResponse{OK: true})
}

type RegenerateKeyRequest struct {
	Password string `json:"password"`
}

type RegenerateKeyResponse struct {
	APIKey string `json:"api_key"`
}

func (s *Service) HandleRegenerateKey(w http.ResponseWriter, r *http.Request) {
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

	var req RegenerateKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	// For accounts with existing password, verify password
	if user.PasswordHash != "" {
		if req.Password == "" {
			http.Error(w, `{"error":"password is required"}`, http.StatusBadRequest)
			return
		}
		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
			http.Error(w, `{"error":"invalid password"}`, http.StatusUnauthorized)
			return
		}
	}

	// Generate new API key
	newKey, err := db.GenerateAPIKey()
	if err != nil {
		log.Printf("ERROR: generate api key for user %d: %v", user.ID, err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	if err := s.db.UpdateUserAPIKey(user.ID, newKey); err != nil {
		log.Printf("ERROR: update api key for user %d: %v", user.ID, err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(RegenerateKeyResponse{APIKey: newKey})
}
