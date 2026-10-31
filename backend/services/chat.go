package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrChatUnavailable = errors.New("对话服务暂不可用")

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatCompleter interface {
	Complete(context.Context, []ChatMessage) (string, error)
}

type ChatConfig struct {
	BaseURL string
	APIKey  string
	Model   string
}

type ChatClient struct {
	endpoint   string
	apiKey     string
	model      string
	http       *http.Client
	streamHTTP *http.Client
}

func NewChatClient(cfg ChatConfig) (*ChatClient, error) {
	baseURL := strings.TrimSpace(cfg.BaseURL)
	u, err := url.Parse(baseURL)
	if err != nil || u == nil || u.Scheme != "https" || u.Hostname() != "ai-gateway.devops.hello1023.com" || u.Port() != "" || u.User != nil || u.Path != "/v1" || u.RawQuery != "" || u.Fragment != "" || cfg.APIKey == "" || cfg.APIKey != strings.TrimSpace(cfg.APIKey) || strings.ContainsAny(cfg.APIKey, "\r\n") || strings.TrimSpace(cfg.Model) == "" {
		return nil, ErrChatUnavailable
	}
	client := &http.Client{Transport: captureTransport(), Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &ChatClient{endpoint: strings.TrimRight(baseURL, "/") + "/chat/completions", apiKey: cfg.APIKey, model: strings.TrimSpace(cfg.Model), http: client, streamHTTP: &http.Client{Transport: captureTransport(), Timeout: 120 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *ChatClient) Complete(ctx context.Context, messages []ChatMessage) (string, error) {
	if c == nil || c.http == nil || len(messages) < 2 || len(messages) > 10 {
		return "", ErrChatUnavailable
	}
	for i, message := range messages {
		if message.Content == "" || (message.Role != "system" && message.Role != "user" && message.Role != "assistant") || (i == 0 && message.Role != "system") {
			return "", ErrChatUnavailable
		}
	}
	body, err := json.Marshal(map[string]any{"model": c.model, "messages": messages})
	if err != nil || len(body) > 20*1024*1024 {
		return "", ErrChatUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", ErrChatUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", ErrChatUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", ErrChatUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024+1))
	if err != nil || len(data) > 1024*1024 {
		return "", ErrChatUnavailable
	}
	var payload struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(data, &payload) != nil || len(payload.Choices) == 0 {
		return "", ErrChatUnavailable
	}
	answer := strings.TrimSpace(payload.Choices[0].Message.Content)
	if answer == "" || len([]rune(answer)) > 8000 {
		return "", ErrChatUnavailable
	}
	return answer, nil
}

// CompleteWithTools uses structured function-call responses; the host validates
// every proposed call and performs all tool execution locally.
func (c *ChatClient) CompleteWithTools(ctx context.Context, messages []MCPModelMessage, tools []MCPToolDefinition) (MCPModelTurn, error) {
	if c == nil || c.http == nil || len(messages) < 2 || len(messages) > 32 || len(tools) > 32 {
		return MCPModelTurn{}, ErrChatUnavailable
	}
	wireMessages, err := mcpWireMessages(messages)
	if err != nil {
		return MCPModelTurn{}, err
	}
	bodyValue := map[string]any{"model": c.model, "messages": wireMessages}
	if len(tools) > 0 {
		wireTools := make([]map[string]any, 0, len(tools))
		for _, tool := range tools {
			wireTools = append(wireTools, map[string]any{"type": "function", "function": map[string]any{"name": tool.Name, "description": tool.Description, "parameters": json.RawMessage(tool.InputSchema)}})
		}
		bodyValue["tools"], bodyValue["tool_choice"] = wireTools, "auto"
	} else {
		bodyValue["tool_choice"] = "none"
	}
	body, err := json.Marshal(bodyValue)
	if err != nil || len(body) > 2*1024*1024 {
		return MCPModelTurn{}, ErrChatUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return MCPModelTurn{}, ErrChatUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return MCPModelTurn{}, ErrChatUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return MCPModelTurn{}, ErrChatUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024+1))
	if err != nil || len(data) > 1024*1024 {
		return MCPModelTurn{}, ErrChatUnavailable
	}
	var payload struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(data, &payload) != nil || len(payload.Choices) == 0 {
		return MCPModelTurn{}, ErrChatUnavailable
	}
	source := payload.Choices[0].Message
	turn := MCPModelTurn{Content: strings.TrimSpace(source.Content)}
	if len([]rune(turn.Content)) > 8000 || len(source.ToolCalls) > 8 {
		return MCPModelTurn{}, ErrChatUnavailable
	}
	for _, call := range source.ToolCalls {
		if call.ID == "" || len(call.ID) > 128 || len(call.Function.Name) > 128 || len(call.Function.Arguments) > 16*1024 {
			return MCPModelTurn{}, ErrChatUnavailable
		}
		turn.ToolCalls = append(turn.ToolCalls, MCPProposedCall{ID: call.ID, Name: call.Function.Name, Arguments: json.RawMessage(call.Function.Arguments)})
	}
	if turn.Content == "" && len(turn.ToolCalls) == 0 {
		return MCPModelTurn{}, ErrChatUnavailable
	}
	return turn, nil
}

func mcpWireMessages(messages []MCPModelMessage) ([]map[string]any, error) {
	if len(messages) < 2 || len(messages) > 32 {
		return nil, ErrChatUnavailable
	}
	wireMessages := make([]map[string]any, 0, len(messages))
	for i, message := range messages {
		if message.Role != "system" && message.Role != "user" && message.Role != "assistant" && message.Role != "tool" || i == 0 && message.Role != "system" {
			return nil, ErrChatUnavailable
		}
		row := map[string]any{"role": message.Role}
		if message.Content != "" {
			row["content"] = message.Content
		} else if message.Role != "assistant" {
			return nil, ErrChatUnavailable
		}
		if message.Role == "tool" {
			if message.ToolCallID == "" || message.Name == "" {
				return nil, ErrChatUnavailable
			}
			row["tool_call_id"], row["name"] = message.ToolCallID, message.Name
		}
		if message.Role == "assistant" && len(message.ToolCalls) > 0 {
			calls := make([]map[string]any, 0, len(message.ToolCalls))
			for _, call := range message.ToolCalls {
				calls = append(calls, map[string]any{"id": call.ID, "type": "function", "function": map[string]any{"name": call.Name, "arguments": string(call.Arguments)}})
			}
			row["tool_calls"] = calls
		}
		wireMessages = append(wireMessages, row)
	}
	return wireMessages, nil
}
