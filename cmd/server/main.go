package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ecorouter/ecorouter/internal/accounts"
	"github.com/ecorouter/ecorouter/internal/chat"
	"github.com/ecorouter/ecorouter/internal/crypto"
	"github.com/ecorouter/ecorouter/internal/dashboard"
	"github.com/ecorouter/ecorouter/internal/db"
	"github.com/ecorouter/ecorouter/internal/openrouter"
	"github.com/ecorouter/ecorouter/internal/proxy"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// Config from env (config.yaml parsing can be added later)
	port := envOr("PORT", "8080")
	dbPath := envOr("DB_PATH", "./ecorouter.db")
	orBaseURL := envOr("OPENROUTER_BASE_URL", "https://openrouter.ai/api/v1")
	orProvKey := os.Getenv("OPENROUTER_PROVISIONING_KEY")
	depositAddr := os.Getenv("TRON_DEPOSIT_ADDRESS")
	if depositAddr == "" {
		log.Fatal("TRON_DEPOSIT_ADDRESS environment variable is required")
	}
	ecoWallet := depositAddr // single wallet architecture: deposit = eco = ops
	opsWallet := depositAddr
	trongridKey := os.Getenv("TRONGRID_API_KEY")
	domain := envOr("DOMAIN", "api.ecorouter.org")
	pollSec := 30

	// Database
	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()
	log.Printf("Database opened: %s", dbPath)

	// OpenRouter client
	orClient := openrouter.NewClient(orBaseURL, orProvKey)
	if orProvKey == "" {
		log.Println("WARN: OPENROUTER_PROVISIONING_KEY not set, key provisioning will fail")
	}

	// Services
	accountsSvc := accounts.NewService(database, orClient, depositAddr, 10, 1)

	// Dashboard
	fundRotation := []dashboard.FundInfo{
		{Name: "COTAP", Category: "carbon", Months: []int{1, 4, 7, 10}},
		{Name: "WaterAid", Category: "water", Months: []int{2, 5, 8, 11}},
		{Name: "Oceanic Society", Category: "ocean", Months: []int{3, 6, 9, 12}},
	}
	dashHandler := dashboard.NewHandler(database, ecoWallet, opsWallet, 50, fundRotation)

	// Deposit watcher (Phase 2)
	watcher := crypto.NewWatcher(database, accountsSvc, depositAddr, trongridKey, time.Duration(pollSec)*time.Second)
	accountsSvc.SetTxVerifier(watcher)
	go watcher.Start()

	// Usage tracker (monitoring only, polls OR for spend stats)
	go accounts.StartUsageTracker(database, orClient, 5*time.Minute)

	// Chat service
	braveKey := os.Getenv("BRAVE_SEARCH_API_KEY")
	chatSvc := chat.NewService(database, orClient, domain, braveKey)

	// Proxy
	proxyHandler := proxy.NewHandler(database, orBaseURL, domain)

	// Router
	mux := http.NewServeMux()

	// EcoRouter-specific endpoints
	mux.HandleFunc("/v1/auth/register", accountsSvc.HandleRegister)
	mux.HandleFunc("/v1/auth/login", accountsSvc.HandleLogin)
	mux.HandleFunc("/v1/auth/balance", accountsSvc.HandleBalance)
	mux.HandleFunc("/v1/auth/notify-deposit", accountsSvc.HandleNotifyDeposit)
	mux.HandleFunc("/v1/auth/claim-deposit", accountsSvc.HandleClaimDeposit)
	mux.HandleFunc("/v1/auth/impact", accountsSvc.HandleImpact)
	mux.HandleFunc("/v1/auth/change-password", accountsSvc.HandleChangePassword)
	mux.HandleFunc("/v1/auth/regenerate-key", accountsSvc.HandleRegenerateKey)
	mux.HandleFunc("/v1/stats", dashHandler.HandleStats)

	// Chat CRUD endpoints
	mux.HandleFunc("/v1/chat/list", chatSvc.HandleListChats)
	mux.HandleFunc("/v1/chat/new", chatSvc.HandleNewChat)
	mux.HandleFunc("/v1/chat/agent", chatSvc.HandleAgent)
	mux.HandleFunc("/v1/chat/", chatSvc.HandleChatByID) // handles GET /v1/chat/{id} and DELETE /v1/chat/{id}

	// Public badge and verify pages
	mux.HandleFunc("/badge/", accountsSvc.HandleBadge)
	mux.HandleFunc("/verify/", accountsSvc.HandleVerify)

	// Chat page
	mux.HandleFunc("/chat", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "web/chat.html")
	})

	// Transparency page
	mux.HandleFunc("/transparency", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "web/transparency.html")
	})

	// API docs page
	mux.HandleFunc("/docs", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "web/docs.html")
	})

	// Static files (landing page)
	mux.Handle("/", http.FileServer(http.Dir("web")))

	// Proxy: /v1/chat/completions, /v1/embeddings, /v1/models
	mux.Handle("/v1/chat/completions", proxyHandler)
	mux.Handle("/v1/embeddings", proxyHandler)
	mux.Handle("/v1/models", proxyHandler)

	// CORS middleware
	handler := corsMiddleware(mux)

	// Server
	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second, // long for streaming
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("Shutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	log.Printf("EcoRouter starting on :%s", port)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
	log.Println("Server stopped")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Only add CORS to /v1/stats and auth endpoints, not proxy
		path := r.URL.Path
		if strings.HasPrefix(path, "/v1/auth/") || path == "/v1/stats" || strings.HasPrefix(path, "/v1/chat/") {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}

		next.ServeHTTP(w, r)
	})
}
