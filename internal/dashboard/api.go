package dashboard

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/ecorouter/ecorouter/internal/db"
)

type Handler struct {
	db              *db.DB
	ecoWallet       string
	opsWallet       string
	donationThreshold float64
	fundRotation    []FundInfo
}

type FundInfo struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	Months   []int  `json:"months"`
}

func NewHandler(database *db.DB, ecoWallet, opsWallet string, threshold float64, rotation []FundInfo) *Handler {
	return &Handler{
		db:              database,
		ecoWallet:       ecoWallet,
		opsWallet:       opsWallet,
		donationThreshold: threshold,
		fundRotation:    rotation,
	}
}

type StatsResponse struct {
	TotalUsers         int              `json:"total_users"`
	TotalDepositedUSDT float64          `json:"total_deposited_usdt"`
	TotalEcoUSDT       float64          `json:"total_eco_collected_usdt"`
	TotalDonatedUSDT   float64          `json:"total_donated_usdt"`
	EcoWallet          string           `json:"eco_wallet_address"`
	OpsWallet          string           `json:"ops_wallet_address"`
	DonationThreshold  float64          `json:"donation_threshold_usdt"`
	NextFund           *NextFundInfo    `json:"next_fund"`
	FundRotation       []FundInfo       `json:"fund_rotation"`
	Donations          []DonationInfo   `json:"donations"`
}

type NextFundInfo struct {
	Name     string `json:"name"`
	Category string `json:"category"`
}

type DonationInfo struct {
	Date         string  `json:"date"`
	AmountUSDT   float64 `json:"amount_usdt"`
	FundName     string  `json:"fund_name"`
	FundCategory string  `json:"fund_category"`
	TxHash       string  `json:"tx_hash"`
	TronscanURL  string  `json:"tronscan_url"`
}

func (h *Handler) HandleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	stats, err := h.db.GetStats()
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	// Determine current month's fund
	var nextFund *NextFundInfo
	currentMonth := int(time.Now().Month())
	for _, f := range h.fundRotation {
		for _, m := range f.Months {
			if m == currentMonth {
				nextFund = &NextFundInfo{Name: f.Name, Category: f.Category}
				break
			}
		}
		if nextFund != nil {
			break
		}
	}

	var donations []DonationInfo
	for _, d := range stats.Donations {
		donations = append(donations, DonationInfo{
			Date:         d.CreatedAt.Format("2006-01-02"),
			AmountUSDT:   d.AmountUSDT,
			FundName:     d.FundName,
			FundCategory: d.FundCategory,
			TxHash:       d.TxHash,
			TronscanURL:  "https://tronscan.org/#/transaction/" + d.TxHash,
		})
	}

	resp := StatsResponse{
		TotalUsers:         stats.TotalUsers,
		TotalDepositedUSDT: stats.TotalDepositedUSDT,
		TotalEcoUSDT:       stats.TotalEcoUSDT,
		TotalDonatedUSDT:   stats.TotalDonatedUSDT,
		EcoWallet:          h.ecoWallet,
		OpsWallet:          h.opsWallet,
		DonationThreshold:  h.donationThreshold,
		NextFund:           nextFund,
		FundRotation:       h.fundRotation,
		Donations:          donations,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(resp)
}
