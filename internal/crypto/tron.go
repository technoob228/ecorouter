package crypto

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/ecorouter/ecorouter/internal/accounts"
	"github.com/ecorouter/ecorouter/internal/db"
)

const (
	// USDT TRC-20 contract on Tron mainnet
	USDTContract = "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"
	// USDT has 6 decimals
	USDTDecimals = 6
)

// Watcher polls TronGrid for incoming USDT deposits.
type Watcher struct {
	db             *db.DB
	accounts       *accounts.Service
	depositAddress string
	trongridAPIKey string
	pollInterval   time.Duration
	httpClient     *http.Client
	lastTimestamp  int64
}

func NewWatcher(database *db.DB, accountsSvc *accounts.Service, depositAddress, trongridAPIKey string, pollInterval time.Duration) *Watcher {
	return &Watcher{
		db:             database,
		accounts:       accountsSvc,
		depositAddress: depositAddress,
		trongridAPIKey: trongridAPIKey,
		pollInterval:   pollInterval,
		httpClient:     &http.Client{Timeout: 15 * time.Second},
		lastTimestamp:  time.Now().UnixMilli(),
	}
}

// TronGrid TRC-20 transfer response
type trc20Response struct {
	Data    []trc20Transfer `json:"data"`
	Success bool            `json:"success"`
	Meta    struct {
		Links struct {
			Next string `json:"next"`
		} `json:"links"`
	} `json:"meta"`
}

type trc20Transfer struct {
	TransactionID string `json:"transaction_id"`
	TokenInfo     struct {
		Address  string `json:"address"`
		Decimals int    `json:"decimals"`
	} `json:"token_info"`
	From      string `json:"from"`
	To        string `json:"to"`
	Value     string `json:"value"`
	BlockTS   int64  `json:"block_timestamp"`
}

// Start begins the polling loop. Call in a goroutine.
func (w *Watcher) Start() {
	log.Printf("Deposit watcher started for %s (poll every %s)", w.depositAddress, w.pollInterval)
	// Poll immediately on start, then on interval
	w.poll()
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	for range ticker.C {
		w.poll()
	}
}

func (w *Watcher) poll() {
	transfers, err := w.fetchTransfers()
	if err != nil {
		log.Printf("ERROR: fetch transfers: %v", err)
		return
	}

	for _, tx := range transfers {
		w.processTransfer(tx)
	}
}

func (w *Watcher) fetchTransfers() ([]trc20Transfer, error) {
	url := fmt.Sprintf(
		"https://api.trongrid.io/v1/accounts/%s/transactions/trc20?only_to=true&only_confirmed=true&limit=50&contract_address=%s&min_timestamp=%d",
		w.depositAddress, USDTContract, w.lastTimestamp,
	)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if w.trongridAPIKey != "" {
		req.Header.Set("TRON-PRO-API-KEY", w.trongridAPIKey)
	}

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("trongrid request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("trongrid status %d: %s", resp.StatusCode, string(body))
	}

	var result trc20Response
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if !result.Success {
		return nil, fmt.Errorf("trongrid returned success=false")
	}

	return result.Data, nil
}

func (w *Watcher) processTransfer(tx trc20Transfer) {
	// Update last timestamp for next poll
	if tx.BlockTS > w.lastTimestamp {
		w.lastTimestamp = tx.BlockTS + 1
	}

	// Parse amount
	rawValue, err := strconv.ParseUint(tx.Value, 10, 64)
	if err != nil {
		log.Printf("ERROR: parse transfer value %q: %v", tx.Value, err)
		return
	}
	amountUSDT := float64(rawValue) / math.Pow(10, USDTDecimals)

	if amountUSDT < 0.01 {
		return // dust, ignore
	}

	// Try to match user by cents suffix
	user, err := w.db.MatchUserByCents(amountUSDT)
	var userID *int64
	if err == nil {
		userID = &user.ID
	}

	// Log the deposit (idempotent — INSERT OR IGNORE on tx_hash)
	if err := w.db.LogDeposit(tx.TransactionID, tx.From, amountUSDT, userID); err != nil {
		log.Printf("ERROR: log deposit %s: %v", tx.TransactionID, err)
		return
	}

	if user == nil {
		log.Printf("WARN: deposit %.3f USDT from %s — no user matched (tx: %s)", amountUSDT, tx.From, tx.TransactionID)
		return
	}

	// Process: fee split + OR key limit update
	result, err := w.accounts.ProcessDeposit(user.ID, amountUSDT, tx.TransactionID)
	if err != nil {
		log.Printf("ERROR: process deposit for user %d: %v", user.ID, err)
		return
	}

	if err := w.db.MarkDepositProcessed(tx.TransactionID); err != nil {
		log.Printf("ERROR: mark deposit processed %s: %v", tx.TransactionID, err)
	}

	log.Printf("INFO: deposit credited — user %d, amount $%.2f, eco $%.2f, api_credit $%.2f (tx: %s)",
		user.ID, amountUSDT, result.EcoFeeUSDT, result.APICreditAdded, tx.TransactionID)
}

// VerifyTransaction checks a specific tx_hash on TronGrid and returns transfer details if valid.
func (w *Watcher) VerifyTransaction(txHash string) (*trc20Transfer, error) {
	url := fmt.Sprintf("https://api.trongrid.io/v1/transactions/%s/events", txHash)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if w.trongridAPIKey != "" {
		req.Header.Set("TRON-PRO-API-KEY", w.trongridAPIKey)
	}

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("trongrid request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("trongrid status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			EventName       string `json:"event_name"`
			ContractAddress string `json:"contract_address"`
			Result          map[string]string `json:"result"`
			BlockTimestamp  int64  `json:"block_timestamp"`
		} `json:"data"`
		Success bool `json:"success"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	// Find Transfer event for USDT contract to our address
	for _, event := range result.Data {
		if event.EventName != "Transfer" {
			continue
		}
		// TronGrid returns contract address without 41 prefix in events
		to := event.Result["to"]
		value := event.Result["value"]
		from := event.Result["from"]

		if to == "" || value == "" {
			continue
		}

		return &trc20Transfer{
			TransactionID: txHash,
			From:          from,
			To:            to,
			Value:         value,
			BlockTS:       event.BlockTimestamp,
		}, nil
	}

	return nil, fmt.Errorf("no USDT transfer found in tx %s", txHash)
}

// VerifyAndProcess implements accounts.TxVerifier — instant deposit confirmation.
func (w *Watcher) VerifyAndProcess(txHash string) (string, error) {
	tx, err := w.VerifyTransaction(txHash)
	if err != nil {
		return "not_found", err
	}

	// Process like a normal deposit
	w.processTransfer(*tx)
	return "credited", nil
}
