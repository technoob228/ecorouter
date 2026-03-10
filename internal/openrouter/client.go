package openrouter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type Client struct {
	baseURL        string
	provisioningKey string
	httpClient     *http.Client
}

func NewClient(baseURL, provisioningKey string) *Client {
	return &Client{
		baseURL:        baseURL,
		provisioningKey: provisioningKey,
		httpClient:     &http.Client{},
	}
}

type CreateKeyRequest struct {
	Name  string   `json:"name"`
	Limit float64  `json:"limit"`
}

type KeyResponse struct {
	Key  string `json:"key"` // top-level, only present on create
	Data struct {
		Hash       string   `json:"hash"`
		Name       string   `json:"name"`
		Limit      *float64 `json:"limit"`
		Usage      float64  `json:"usage"`
		IsFreeTier bool     `json:"is_free_tier"`
	} `json:"data"`
}

type UpdateKeyRequest struct {
	Limit float64 `json:"limit"`
}

// CreateKey creates a new OpenRouter API key via Provisioning API.
func (c *Client) CreateKey(name string, limit float64) (*KeyResponse, error) {
	body, _ := json.Marshal(CreateKeyRequest{Name: name, Limit: limit})
	req, err := http.NewRequest("POST", c.baseURL+"/keys", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.provisioningKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("create key request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("create key: status %d: %s", resp.StatusCode, string(respBody))
	}

	var result KeyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// UpdateKeyLimit updates the spending limit on an OpenRouter key.
func (c *Client) UpdateKeyLimit(keyHash string, newLimit float64) error {
	body, _ := json.Marshal(UpdateKeyRequest{Limit: newLimit})
	req, err := http.NewRequest("PATCH", c.baseURL+"/keys/"+keyHash, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.provisioningKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("update key request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("update key: status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// GetKeyInfo returns current usage and limit for a key.
func (c *Client) GetKeyInfo(keyHash string) (*KeyResponse, error) {
	req, err := http.NewRequest("GET", c.baseURL+"/keys/"+keyHash, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.provisioningKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get key request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get key: status %d: %s", resp.StatusCode, string(respBody))
	}

	var result KeyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}
