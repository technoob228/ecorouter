package chat

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const braveSearchURL = "https://api.search.brave.com/res/v1/web/search"
const duckDuckGoSearchURL = "https://html.duckduckgo.com/html/"

// SearchClient is a web search client. Uses Brave Search API when an API key
// is configured, and falls back to DuckDuckGo HTML search otherwise.
type SearchClient struct {
	apiKey     string
	httpClient *http.Client
}

// NewSearchClient creates a new search client. If apiKey is non-empty, Brave
// Search is used; otherwise DuckDuckGo is used as a free fallback.
func NewSearchClient(apiKey string) *SearchClient {
	return &SearchClient{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// SearchResult represents a single web search result.
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// braveResponse mirrors the relevant parts of the Brave Search API response.
type braveResponse struct {
	Web struct {
		Results []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
		} `json:"results"`
	} `json:"web"`
}

// Search performs a web search and returns up to 5 results.
// It uses Brave Search when an API key is configured, and DuckDuckGo otherwise.
func (sc *SearchClient) Search(query string) ([]SearchResult, error) {
	if sc.apiKey != "" {
		return sc.searchBrave(query)
	}
	return sc.searchDuckDuckGo(query)
}

// searchBrave queries the Brave Search API.
func (sc *SearchClient) searchBrave(query string) ([]SearchResult, error) {
	reqURL := fmt.Sprintf("%s?q=%s&count=5", braveSearchURL, url.QueryEscape(query))

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", sc.apiKey)

	resp, err := sc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("brave search status %d: %s", resp.StatusCode, string(body))
	}

	var braveResp braveResponse
	if err := json.Unmarshal(body, &braveResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	results := make([]SearchResult, 0, len(braveResp.Web.Results))
	for _, r := range braveResp.Web.Results {
		results = append(results, SearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Description,
		})
	}

	return results, nil
}

// Regex patterns for parsing DuckDuckGo HTML results.
var (
	ddgResultRe  = regexp.MustCompile(`(?s)<a[^>]+class="result__a"[^>]*href="([^"]*)"[^>]*>(.*?)</a>`)
	ddgSnippetRe = regexp.MustCompile(`(?s)<a[^>]+class="result__snippet"[^>]*>(.*?)</a>`)
	htmlTagRe    = regexp.MustCompile(`<[^>]*>`)
)

// searchDuckDuckGo queries DuckDuckGo's HTML search endpoint and parses the
// results. This is used as a free fallback when no Brave API key is set.
func (sc *SearchClient) searchDuckDuckGo(query string) ([]SearchResult, error) {
	reqURL := duckDuckGoSearchURL + "?q=" + url.QueryEscape(query)

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; EcoRouter/1.0)")

	resp, err := sc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("duckduckgo search status %d", resp.StatusCode)
	}

	page := string(body)

	// Extract result links: <a class="result__a" href="URL">Title</a>
	linkMatches := ddgResultRe.FindAllStringSubmatch(page, 10)

	// Extract snippets: <a class="result__snippet">text</a>
	snippetMatches := ddgSnippetRe.FindAllStringSubmatch(page, 10)

	var results []SearchResult
	for i, m := range linkMatches {
		if len(results) >= 5 {
			break
		}

		rawURL := m[1]
		title := stripHTML(m[2])

		// DuckDuckGo wraps URLs in a redirect; extract the actual URL.
		parsedURL := extractDDGUrl(rawURL)
		if parsedURL == "" {
			continue
		}

		snippet := ""
		if i < len(snippetMatches) {
			snippet = stripHTML(snippetMatches[i][1])
		}

		results = append(results, SearchResult{
			Title:   title,
			URL:     parsedURL,
			Snippet: snippet,
		})
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("duckduckgo: no results found for %q", query)
	}

	return results, nil
}

// extractDDGUrl extracts the actual destination URL from a DuckDuckGo redirect
// link. DDG links look like: //duckduckgo.com/l/?uddg=https%3A%2F%2F...&rut=...
func extractDDGUrl(raw string) string {
	// If it's already a direct URL, use it.
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}

	// Parse the redirect URL to extract the uddg parameter.
	if strings.Contains(raw, "uddg=") {
		// Prepend scheme if needed for url.Parse.
		if strings.HasPrefix(raw, "//") {
			raw = "https:" + raw
		}
		u, err := url.Parse(raw)
		if err != nil {
			return ""
		}
		dest := u.Query().Get("uddg")
		if dest != "" {
			return dest
		}
	}

	return ""
}

// stripHTML removes HTML tags and decodes HTML entities from a string.
func stripHTML(s string) string {
	s = htmlTagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = strings.TrimSpace(s)
	return s
}
