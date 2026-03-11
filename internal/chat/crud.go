package chat

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/ecorouter/ecorouter/internal/db"
	"github.com/ecorouter/ecorouter/internal/openrouter"
)

type Service struct {
	db     *db.DB
	or     *openrouter.Client
	domain string
	search *SearchClient
}

func NewService(database *db.DB, orClient *openrouter.Client, domain string, searchAPIKey string) *Service {
	return &Service{
		db:     database,
		or:     orClient,
		domain: domain,
		search: NewSearchClient(searchAPIKey),
	}
}

// HandleListChats handles GET /v1/chat/list
func (s *Service) HandleListChats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	user, err := s.authenticate(r)
	if err != nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	chats, err := s.db.ListChats(user.ID)
	if err != nil {
		log.Printf("ERROR: list chats for user %d: %v", user.ID, err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	// Return empty array instead of null
	if chats == nil {
		chats = []db.Chat{}
	}

	type chatItem struct {
		ID        int64  `json:"id"`
		Title     string `json:"title"`
		Model     string `json:"model"`
		UpdatedAt string `json:"updated_at"`
	}

	items := make([]chatItem, len(chats))
	for i, c := range chats {
		items[i] = chatItem{
			ID:        c.ID,
			Title:     c.Title,
			Model:     c.Model,
			UpdatedAt: c.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(items)
}

// HandleNewChat handles POST /v1/chat/new
func (s *Service) HandleNewChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	user, err := s.authenticate(r)
	if err != nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var req struct {
		Model string `json:"model"`
	}
	// Body is optional; ignore decode errors
	json.NewDecoder(r.Body).Decode(&req)

	chat, err := s.db.CreateChat(user.ID, req.Model)
	if err != nil {
		log.Printf("ERROR: create chat for user %d: %v", user.ID, err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	resp := struct {
		ID    int64  `json:"id"`
		Title string `json:"title"`
		Model string `json:"model"`
	}{
		ID:    chat.ID,
		Title: chat.Title,
		Model: chat.Model,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// HandleChatByID handles GET /v1/chat/{id} and DELETE /v1/chat/{id}
func (s *Service) HandleChatByID(w http.ResponseWriter, r *http.Request) {
	user, err := s.authenticate(r)
	if err != nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	// Parse chat ID from URL path: /v1/chat/123
	idStr := strings.TrimPrefix(r.URL.Path, "/v1/chat/")
	chatID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || chatID <= 0 {
		http.Error(w, `{"error":"invalid chat id"}`, http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		chat, err := s.db.GetChat(chatID, user.ID)
		if err != nil {
			http.Error(w, `{"error":"chat not found"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(chat)

	case http.MethodDelete:
		if err := s.db.DeleteChat(chatID, user.ID); err != nil {
			log.Printf("ERROR: delete chat %d for user %d: %v", chatID, user.ID, err)
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))

	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// authenticate extracts the bearer token and returns the user.
func (s *Service) authenticate(r *http.Request) (*db.User, error) {
	token := extractBearerToken(r)
	if token == "" {
		return nil, http.ErrNoCookie // just a non-nil error
	}
	return s.db.GetUserByEcoKey(token)
}

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}
