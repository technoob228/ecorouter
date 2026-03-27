package db

import "time"

type User struct {
	ID                 int64     `json:"id"`
	Email              string    `json:"email"`
	EcoAPIKey          string    `json:"eco_api_key"`
	DepositCentsSuffix int       `json:"deposit_cents_suffix"` // unique cents suffix for deposit matching
	ORKeyHash          string    `json:"-"`
	ORKeySecret        string    `json:"-"`
	PasswordHash       string    `json:"-"`
	TotalDepositedUSDT float64   `json:"total_deposited_usdt"`
	TotalEcoUSDT       float64   `json:"total_eco_usdt"`
	TotalOpsUSDT       float64   `json:"total_ops_usdt"`
	TotalAPICreditUSDT float64   `json:"total_api_credit_usdt"`
	FreeRequestsUsed   int       `json:"free_requests_used"`
	CreatedAt          time.Time `json:"created_at"`
}

type Deposit struct {
	ID           int64     `json:"id"`
	UserID       int64     `json:"user_id"`
	AmountUSDT   float64   `json:"amount_usdt"`
	EcoFeeUSDT   float64   `json:"eco_fee_usdt"`
	OpsFeeUSDT   float64   `json:"ops_fee_usdt"`
	APICreditUSDT float64  `json:"api_credit_usdt"`
	TxHash       string    `json:"tx_hash"`
	CreatedAt    time.Time `json:"created_at"`
}

type Donation struct {
	ID           int64     `json:"id"`
	AmountUSDT   float64   `json:"amount_usdt"`
	FundName     string    `json:"fund_name"`
	FundCategory string    `json:"fund_category"`
	TxHash       string    `json:"tx_hash"`
	CreatedAt    time.Time `json:"created_at"`
}

type DepositLog struct {
	ID          int64     `json:"id"`
	TxHash      string    `json:"tx_hash"`
	FromAddress string    `json:"from_address"`
	AmountUSDT  float64   `json:"amount_usdt"`
	UserID      *int64    `json:"user_id"`
	Processed   bool      `json:"processed"`
	CreatedAt   time.Time `json:"created_at"`
}

type Chat struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	Title     string    `json:"title"`
	Messages  string    `json:"messages"` // JSON string
	Model     string    `json:"model"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
