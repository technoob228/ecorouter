package crypto

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// BalanceChecker queries wallet balances on Tron.
type BalanceChecker struct {
	trongridAPIKey string
	httpClient     *http.Client
}

func NewBalanceChecker(trongridAPIKey string) *BalanceChecker {
	return &BalanceChecker{
		trongridAPIKey: trongridAPIKey,
		httpClient:     &http.Client{Timeout: 10 * time.Second},
	}
}

// GetUSDTBalance returns the USDT TRC-20 balance for an address.
func (bc *BalanceChecker) GetUSDTBalance(address string) (float64, error) {
	url := fmt.Sprintf("https://api.trongrid.io/v1/accounts/%s/tokens?token_id=%s", address, USDTContract)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, err
	}
	if bc.trongridAPIKey != "" {
		req.Header.Set("TRON-PRO-API-KEY", bc.trongridAPIKey)
	}

	resp, err := bc.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("trongrid request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("trongrid status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			Balance string `json:"balance"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return 0, fmt.Errorf("decode: %w", err)
	}

	if len(result.Data) == 0 {
		return 0, nil
	}

	raw, err := strconv.ParseUint(result.Data[0].Balance, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse balance: %w", err)
	}

	return float64(raw) / 1e6, nil
}

// GetTRXBalance returns the TRX balance for an address (needed for tx fees).
func (bc *BalanceChecker) GetTRXBalance(address string) (float64, error) {
	url := fmt.Sprintf("https://api.trongrid.io/v1/accounts/%s", address)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, err
	}
	if bc.trongridAPIKey != "" {
		req.Header.Set("TRON-PRO-API-KEY", bc.trongridAPIKey)
	}

	resp, err := bc.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("trongrid request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Data []struct {
			Balance int64 `json:"balance"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decode: %w", err)
	}

	if len(result.Data) == 0 {
		return 0, nil
	}

	return float64(result.Data[0].Balance) / 1e6, nil
}
