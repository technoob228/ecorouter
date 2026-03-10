package accounts

import (
	"log"
	"time"

	"github.com/ecorouter/ecorouter/internal/db"
	"github.com/ecorouter/ecorouter/internal/openrouter"
)

// StartUsageTracker periodically polls OpenRouter for usage stats of all active users.
// This is monitoring-only — OpenRouter is the source of truth for spend tracking.
func StartUsageTracker(database *db.DB, orClient *openrouter.Client, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	log.Printf("Usage tracker started (interval: %s)", interval)

	// Run immediately on start, then on each tick
	pollUsage(database, orClient)

	for range ticker.C {
		pollUsage(database, orClient)
	}
}

func pollUsage(database *db.DB, orClient *openrouter.Client) {
	users, err := database.GetAllActiveUsers()
	if err != nil {
		log.Printf("ERROR: usage poll: get active users: %v", err)
		return
	}

	var totalSpend float64
	var polled int

	for _, u := range users {
		if u.ORKeyHash == "" {
			continue
		}

		info, err := orClient.GetKeyInfo(u.ORKeyHash)
		if err != nil {
			log.Printf("WARN: usage poll: get key info for user %d: %v", u.ID, err)
			continue
		}

		totalSpend += info.Data.Usage
		polled++
	}

	log.Printf("Usage poll: %d users, total spend $%.2f", polled, totalSpend)
}
