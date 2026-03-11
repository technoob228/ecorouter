package chat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

const (
	openRouterCompletionsURL = "https://openrouter.ai/api/v1/chat/completions"
	maxToolIterations        = 5
)

// AgentRequest is the request body for POST /v1/chat/agent.
type AgentRequest struct {
	ChatID       *int64          `json:"chat_id"`
	Model        string          `json:"model"`
	Messages     []Message       `json:"messages"`
	ToolsEnabled map[string]bool `json:"tools_enabled"`
	Stream       bool            `json:"stream"`
	Thinking     bool            `json:"thinking"`
}

// Message represents a chat message in the OpenAI/OpenRouter format.
type Message struct {
	Role       string      `json:"role"`
	Content    interface{} `json:"content"`               // string or array (for vision)
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
	Name       string      `json:"name,omitempty"`
}

// ToolCall represents a tool call from the assistant.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall is the function name + arguments within a tool call.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// orRequest is the request body sent to OpenRouter.
type orRequest struct {
	Model     string                 `json:"model"`
	Messages  []Message              `json:"messages"`
	Tools     []orTool               `json:"tools,omitempty"`
	Stream    bool                   `json:"stream"`
	Reasoning *orReasoning           `json:"reasoning,omitempty"`
}

type orReasoning struct {
	Effort string `json:"effort"` // "high", "medium", "low"
}

// orTool is a tool definition in the OpenAI function-calling format.
type orTool struct {
	Type     string     `json:"type"`
	Function orFunction `json:"function"`
}

// orFunction describes a callable function.
type orFunction struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"`
}

// orResponse is the parsed response from OpenRouter (non-streaming).
type orResponse struct {
	ID      string     `json:"id"`
	Choices []orChoice `json:"choices"`
	Model   string     `json:"model"`
	Usage   orUsage    `json:"usage"`
}

type orChoice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type orUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// HandleAgent handles POST /v1/chat/agent — the agentic chat endpoint.
func (s *Service) HandleAgent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// 1. Auth
	user, err := s.authenticate(r)
	if err != nil {
		http.Error(w, `{"error":{"message":"invalid api key","type":"auth_error"}}`, http.StatusUnauthorized)
		return
	}

	if user.ORKeySecret == "" {
		http.Error(w, `{"error":{"message":"account not provisioned yet","type":"auth_error"}}`, http.StatusServiceUnavailable)
		return
	}

	// 2. Parse request
	var req AgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":{"message":"invalid request body","type":"invalid_request"}}`, http.StatusBadRequest)
		return
	}

	if len(req.Messages) == 0 {
		http.Error(w, `{"error":{"message":"messages array is required","type":"invalid_request"}}`, http.StatusBadRequest)
		return
	}
	if req.Model == "" {
		req.Model = "openai/gpt-4o-mini"
	}

	// 3. Build tool definitions if any tools are enabled
	//    Skip tools when messages contain image content (vision + tools not always supported)
	hasImages := false
	for _, m := range req.Messages {
		if arr, ok := m.Content.([]interface{}); ok {
			for _, part := range arr {
				if pm, ok := part.(map[string]interface{}); ok {
					if pm["type"] == "image_url" {
						hasImages = true
						break
					}
				}
			}
		}
		if hasImages {
			break
		}
	}

	var tools []orTool
	if len(req.ToolsEnabled) > 0 && !hasImages {
		tools = s.buildToolDefs(req.ToolsEnabled)
	}

	// 4. If images present and streaming, skip agentic loop and stream directly
	messages := req.Messages
	if hasImages && req.Stream {
		log.Printf("Image detected, streaming directly for user %d model %s", user.ID, req.Model)
		s.streamFinalResponse(w, r, user.ORKeySecret, req.Model, messages, req.Thinking)
		go s.saveChat(user.ID, req.ChatID, req.Model, req.Messages, messages, nil)
		return
	}

	// Agentic loop: call OpenRouter, handle tool_calls, repeat
	var finalResp *orResponse

	for i := 0; i < maxToolIterations; i++ {
		resp, err := s.callOpenRouter(r, user.ORKeySecret, req.Model, messages, tools, false, req.Thinking)
		if err != nil {
			log.Printf("ERROR: openrouter call for user %d iteration %d: %v", user.ID, i, err)
			http.Error(w, fmt.Sprintf(`{"error":{"message":"upstream error: %s","type":"server_error"}}`, err.Error()), http.StatusBadGateway)
			return
		}

		if len(resp.Choices) == 0 {
			http.Error(w, `{"error":{"message":"no response from model","type":"server_error"}}`, http.StatusBadGateway)
			return
		}

		assistantMsg := resp.Choices[0].Message

		// If no tool calls, we have the final response
		if len(assistantMsg.ToolCalls) == 0 {
			finalResp = resp
			break
		}

		// Append the assistant message (with tool_calls) to conversation
		messages = append(messages, assistantMsg)

		// Execute each tool call and append results
		for _, tc := range assistantMsg.ToolCalls {
			result := s.executeTool(tc)
			toolMsg := Message{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
				Name:       tc.Function.Name,
			}
			messages = append(messages, toolMsg)
		}

		log.Printf("Agent loop iteration %d: executed %d tool calls for user %d", i+1, len(assistantMsg.ToolCalls), user.ID)

		// Continue loop — will send messages back to model
	}

	// If we exhausted iterations without a final response, use the last one
	if finalResp == nil {
		// Make one more call without tools to force a text response
		resp, err := s.callOpenRouter(r, user.ORKeySecret, req.Model, messages, nil, false, req.Thinking)
		if err != nil {
			log.Printf("ERROR: final openrouter call for user %d: %v", user.ID, err)
			http.Error(w, `{"error":{"message":"upstream error","type":"server_error"}}`, http.StatusBadGateway)
			return
		}
		finalResp = resp
	}

	// 5. Stream or return the final response
	if req.Stream && finalResp != nil {
		// For streaming: make a new request with the full conversation (including
		// any tool results) but stream=true. This gives the client SSE chunks.
		s.streamFinalResponse(w, r, user.ORKeySecret, req.Model, messages, req.Thinking)
	} else {
		// Non-streaming: return the JSON response
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(finalResp)
	}

	// 6. Save to DB (async-ish, after response is sent for non-streaming)
	go s.saveChat(user.ID, req.ChatID, req.Model, req.Messages, messages, finalResp)
}

// callOpenRouter makes a non-streaming POST to OpenRouter and returns the parsed response.
func (s *Service) callOpenRouter(r *http.Request, orKey, model string, messages []Message, tools []orTool, stream bool, thinking bool) (*orResponse, error) {
	body := orRequest{
		Model:    model,
		Messages: messages,
		Tools:    tools,
		Stream:   stream,
	}
	if thinking {
		body.Reasoning = &orReasoning{Effort: "medium"}
	}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(r.Context(), "POST", openRouterCompletionsURL, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+orKey)
	req.Header.Set("HTTP-Referer", "https://"+s.domain)
	req.Header.Set("X-Title", "EcoRouter")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var orResp orResponse
	if err := json.Unmarshal(respBody, &orResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &orResp, nil
}

// streamFinalResponse makes a streaming request to OpenRouter and pipes SSE directly to the client.
func (s *Service) streamFinalResponse(w http.ResponseWriter, r *http.Request, orKey, model string, messages []Message, thinking bool) {
	body := orRequest{
		Model:    model,
		Messages: messages,
		Stream:   true,
	}
	if thinking {
		body.Reasoning = &orReasoning{Effort: "medium"}
	}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		log.Printf("ERROR: marshal stream request: %v", err)
		http.Error(w, `{"error":{"message":"internal error","type":"server_error"}}`, http.StatusInternalServerError)
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), "POST", openRouterCompletionsURL, bytes.NewReader(bodyJSON))
	if err != nil {
		log.Printf("ERROR: create stream request: %v", err)
		http.Error(w, `{"error":{"message":"internal error","type":"server_error"}}`, http.StatusInternalServerError)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+orKey)
	req.Header.Set("HTTP-Referer", "https://"+s.domain)
	req.Header.Set("X-Title", "EcoRouter")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("ERROR: stream request: %v", err)
		http.Error(w, `{"error":{"message":"upstream error","type":"server_error"}}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("ERROR: stream response status %d: %s", resp.StatusCode, string(respBody))
		http.Error(w, fmt.Sprintf(`{"error":{"message":"upstream error","type":"server_error"}}`), http.StatusBadGateway)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // for nginx

	// Pipe SSE bytes to client, flushing after each chunk
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

// executeTool runs a tool call and returns the result string.
func (s *Service) executeTool(toolCall ToolCall) string {
	switch toolCall.Function.Name {
	case "web_search":
		var args struct {
			Query string `json:"query"`
		}
		if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
			return fmt.Sprintf(`{"error": "invalid arguments: %s"}`, err.Error())
		}

		results, err := s.search.Search(args.Query)
		if err != nil {
			return fmt.Sprintf(`{"error": "%s"}`, err.Error())
		}

		// Format results as readable text for the model
		var sb strings.Builder
		for i, r := range results {
			fmt.Fprintf(&sb, "%d. %s\n   %s\n   %s\n\n", i+1, r.Title, r.URL, r.Snippet)
		}
		return sb.String()
	default:
		return fmt.Sprintf(`{"error": "unknown tool: %s"}`, toolCall.Function.Name)
	}
}

// buildToolDefs returns OpenAI-format tool definitions for enabled tools.
func (s *Service) buildToolDefs(enabled map[string]bool) []orTool {
	var tools []orTool

	if enabled["web_search"] {
		tools = append(tools, orTool{
			Type: "function",
			Function: orFunction{
				Name:        "web_search",
				Description: "Search the web for current information. Use this when the user asks about recent events, facts you're unsure about, or anything that benefits from up-to-date information.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query": map[string]interface{}{
							"type":        "string",
							"description": "The search query",
						},
					},
					"required": []string{"query"},
				},
			},
		})
	}

	return tools
}

// saveChat persists the conversation to the database.
func (s *Service) saveChat(userID int64, chatID *int64, model string, originalMessages, fullMessages []Message, resp *orResponse) {
	// Build the final message list to save: the original messages + any new messages
	// from the agentic loop (tool calls, tool results, final assistant message)
	var allMessages []Message

	// Start with the full conversation (includes tool interactions)
	allMessages = fullMessages

	// Append the final assistant response if it's not already in fullMessages
	if resp != nil && len(resp.Choices) > 0 {
		finalMsg := resp.Choices[0].Message
		// Check if the last message in fullMessages is already the assistant response
		if len(allMessages) == 0 || allMessages[len(allMessages)-1].Role != "assistant" || len(allMessages[len(allMessages)-1].ToolCalls) > 0 {
			allMessages = append(allMessages, finalMsg)
		}
	}

	messagesJSON, err := json.Marshal(allMessages)
	if err != nil {
		log.Printf("ERROR: marshal messages for chat save: %v", err)
		return
	}

	if chatID != nil && *chatID > 0 {
		// Update existing chat
		chat, err := s.db.GetChat(*chatID, userID)
		if err != nil {
			log.Printf("ERROR: get chat %d for save: %v", *chatID, err)
			return
		}
		title := chat.Title
		// Auto-title if still default
		if title == "New chat" {
			for _, m := range originalMessages {
				if m.Role == "user" {
					if s, ok := m.Content.(string); ok && s != "" {
						title = s
						if len(title) > 50 {
							title = title[:50]
						}
						if len(title) == 50 {
							if idx := strings.LastIndex(title, " "); idx > 20 {
								title = title[:idx] + "..."
							}
						}
						break
					}
				}
			}
		}
		if err := s.db.UpdateChatMessages(chat.ID, title, string(messagesJSON)); err != nil {
			log.Printf("ERROR: update chat %d messages: %v", chat.ID, err)
		}
	} else {
		// Create new chat with auto-title from first user message
		title := "New chat"
		for _, m := range originalMessages {
			if m.Role == "user" {
				if s, ok := m.Content.(string); ok && s != "" {
					title = s
					if len(title) > 50 {
						title = title[:50]
					}
					// Trim to last word boundary if we truncated
					if len(title) == 50 {
						if idx := strings.LastIndex(title, " "); idx > 20 {
							title = title[:idx] + "..."
						}
					}
					break
				}
			}
		}

		chat, err := s.db.CreateChat(userID, model)
		if err != nil {
			log.Printf("ERROR: create chat for user %d: %v", userID, err)
			return
		}
		if err := s.db.UpdateChatMessages(chat.ID, title, string(messagesJSON)); err != nil {
			log.Printf("ERROR: update new chat %d: %v", chat.ID, err)
		}
	}
}
