package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatClientSendsOnlyConfiguredModelAndMessages(t *testing.T) {
	const key = "mock-chat-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer "+key {
			t.Fatal("chat request metadata did not match the configured endpoint")
		}
		var body struct {
			Model    string        `json:"model"`
			Messages []ChatMessage `json:"messages"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Model != "course-chat" || len(body.Messages) != 2 || body.Messages[0].Role != "system" || body.Messages[1].Content != "synthetic question and source" {
			t.Fatal("chat request did not contain expected server-prepared messages")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"本地模拟回答 [1]"}}]}`))
	}))
	defer server.Close()
	client := &ChatClient{endpoint: server.URL + "/v1/chat/completions", apiKey: key, model: "course-chat", http: server.Client()}
	answer, err := client.Complete(context.Background(), []ChatMessage{{Role: "system", Content: "only evidence"}, {Role: "user", Content: "synthetic question and source"}})
	if err != nil || answer != "本地模拟回答 [1]" {
		t.Fatal("mock chat completion did not return the expected answer")
	}
}

func TestChatClientRejectsUnsafeConfigMessagesAndProviderBody(t *testing.T) {
	if _, err := NewChatClient(ChatConfig{BaseURL: "http://ai-gateway.devops.hello1023.com/v1", APIKey: "x", Model: "m"}); !errors.Is(err, ErrChatUnavailable) {
		t.Fatal("insecure chat endpoint must be rejected")
	}
	if _, err := NewChatClient(ChatConfig{BaseURL: "https://other.example/v1", APIKey: "x", Model: "m"}); !errors.Is(err, ErrChatUnavailable) {
		t.Fatal("non-course chat endpoint must be rejected")
	}
	if _, err := NewChatClient(ChatConfig{BaseURL: "https://ai-gateway.devops.hello1023.com/v1", APIKey: "bad\nheader", Model: "m"}); !errors.Is(err, ErrChatUnavailable) {
		t.Fatal("header-injection key must be rejected")
	}
	client := &ChatClient{endpoint: "", apiKey: "x", model: "m", http: http.DefaultClient}
	if _, err := client.Complete(context.Background(), []ChatMessage{{Role: "user", Content: "no server system prompt"}}); !errors.Is(err, ErrChatUnavailable) {
		t.Fatal("client-supplied history must not replace the server system prompt")
	}
	const key = "mock-chat-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(key))
	}))
	defer server.Close()
	client.endpoint, client.apiKey, client.http = server.URL, key, server.Client()
	_, err := client.Complete(context.Background(), []ChatMessage{{Role: "system", Content: "prompt"}, {Role: "user", Content: "question"}})
	if !errors.Is(err, ErrChatUnavailable) || strings.Contains(err.Error(), key) {
		t.Fatal("provider body or secret leaked through the error")
	}
}

func TestChatClientUsesStructuredToolCallsWithoutExecutingThem(t *testing.T) {
	const key = "mock-chat-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+key {
			t.Fatal("structured tool request omitted configured authentication")
		}
		var body struct {
			Tools []struct {
				Function struct {
					Name       string         `json:"name"`
					Parameters map[string]any `json:"parameters"`
				} `json:"function"`
			} `json:"tools"`
			ToolChoice string           `json:"tool_choice"`
			Messages   []map[string]any `json:"messages"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Tools) != 1 || body.ToolChoice != "auto" || body.Tools[0].Function.Name != "mcp_0123456789abcdef" || body.Tools[0].Function.Parameters["type"] != "object" {
			t.Fatal("chat request did not include only the approved structured tool declaration")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":null,"tool_calls":[{"id":"call-1","type":"function","function":{"name":"mcp_0123456789abcdef","arguments":"{\"term\":\"MCP\"}"}}]}}]}`))
	}))
	defer server.Close()
	client := &ChatClient{endpoint: server.URL, apiKey: key, model: "course-chat", http: server.Client()}
	turn, err := client.CompleteWithTools(context.Background(), []MCPModelMessage{{Role: "system", Content: "rules"}, {Role: "user", Content: "question"}}, []MCPToolDefinition{{Name: "mcp_0123456789abcdef", Description: "local only", InputSchema: json.RawMessage(`{"type":"object","properties":{"term":{"type":"string"}},"required":["term"]}`)}})
	if err != nil || len(turn.ToolCalls) != 1 || turn.ToolCalls[0].ID != "call-1" || string(turn.ToolCalls[0].Arguments) != `{"term":"MCP"}` {
		t.Fatal("structured response was not parsed as an unexecuted proposal")
	}
}
