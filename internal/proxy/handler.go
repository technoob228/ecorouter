package proxy

import (
	"io"
	"net/http"
	"strings"

	"github.com/ecorouter/ecorouter/internal/db"
)

type Handler struct {
	db         *db.DB
	upstreamURL string
	domain      string
}

func NewHandler(database *db.DB, upstreamURL, domain string) *Handler {
	return &Handler{
		db:         database,
		upstreamURL: upstreamURL,
		domain:      domain,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Extract eco key from Authorization header
	ecoKey := extractBearerToken(r)
	if ecoKey == "" {
		http.Error(w, `{"error":{"message":"missing api key","type":"auth_error"}}`, http.StatusUnauthorized)
		return
	}

	// Look up user
	user, err := h.db.GetUserByEcoKey(ecoKey)
	if err != nil {
		http.Error(w, `{"error":{"message":"invalid api key","type":"auth_error"}}`, http.StatusUnauthorized)
		return
	}

	if user.ORKeySecret == "" {
		http.Error(w, `{"error":{"message":"account not provisioned yet, please try again shortly","type":"auth_error"}}`, http.StatusServiceUnavailable)
		return
	}

	// Build upstream request — upstreamURL already has /v1, path starts with /v1
	// e.g. upstream=https://openrouter.ai/api/v1, path=/v1/models → /api/v1/models
	path := strings.TrimPrefix(r.URL.Path, "/v1")
	upstreamURL := h.upstreamURL + path
	if r.URL.RawQuery != "" {
		upstreamURL += "?" + r.URL.RawQuery
	}

	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, r.Body)
	if err != nil {
		http.Error(w, `{"error":{"message":"internal error","type":"server_error"}}`, http.StatusInternalServerError)
		return
	}

	// Copy headers from original request
	for key, values := range r.Header {
		k := strings.ToLower(key)
		if k == "host" || k == "authorization" {
			continue
		}
		for _, v := range values {
			proxyReq.Header.Add(key, v)
		}
	}

	// Set auth and branding headers
	proxyReq.Header.Set("Authorization", "Bearer "+user.ORKeySecret)
	proxyReq.Header.Set("HTTP-Referer", "https://"+h.domain)
	proxyReq.Header.Set("X-Title", "EcoRouter")

	// Forward request
	resp, err := http.DefaultClient.Do(proxyReq)
	if err != nil {
		http.Error(w, `{"error":{"message":"upstream error","type":"server_error"}}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for key, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	// Stream response body (works for SSE/streaming)
	if f, ok := w.(http.Flusher); ok {
		buf := make([]byte, 4096)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				w.Write(buf[:n])
				f.Flush()
			}
			if err != nil {
				break
			}
		}
	} else {
		io.Copy(w, resp.Body)
	}
}

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}
