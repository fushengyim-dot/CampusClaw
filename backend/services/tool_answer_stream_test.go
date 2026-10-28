package services

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"campusclaw/backend/repositories"
)

func TestToolAnswerStreamsBeforeCompletionAndPreservesToolMessages(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	release := make(chan struct{})
	received := make(chan string, 2)
	done := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Stream     bool                         `json:"stream"`
			ToolChoice string                       `json:"tool_choice"`
			Messages   []map[string]json.RawMessage `json:"messages"`
			Tools      json.RawMessage              `json:"tools"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || !body.Stream || body.ToolChoice != "none" || len(body.Tools) > 0 || len(body.Messages) != 4 {
			t.Error("final request did not disable tools and retain context")
			w.WriteHeader(400)
			return
		}
		if string(body.Messages[2]["tool_calls"]) == "" || string(body.Messages[3]["tool_call_id"]) != `"call-1"` || string(body.Messages[3]["role"]) != `"tool"` {
			t.Error("tool identifiers lost")
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"第一段\"}}]}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"第二段\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	client := &ChatClient{endpoint: server.URL, model: "local-test", apiKey: "synthetic", http: server.Client()}
	messages := []MCPModelMessage{{Role: "system", Content: "rules"}, {Role: "user", Content: "question"}, {Role: "assistant", ToolCalls: []MCPProposedCall{{ID: "call-1", Name: "term", Arguments: json.RawMessage(`{"term":"MCP"}`)}}}, {Role: "tool", Name: "term", ToolCallID: "call-1", Content: "local tool result"}}
	go func() {
		done <- client.StreamToolAnswer(ctx, messages, func(s string) error { received <- s; return nil })
	}()
	select {
	case first := <-received:
		if first != "第一段" {
			t.Fatal("unexpected first delta")
		}
	case <-ctx.Done():
		t.Fatal("first delta was buffered until completion")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if second := <-received; second != "第二段" {
		t.Fatal("missing second delta")
	}
}

func TestFinalStreamReportsTruncatedResponseWithoutReplayingOrRetrying(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
	}))
	defer server.Close()
	client := &ChatClient{endpoint: server.URL, model: "local", http: server.Client()}
	var got strings.Builder
	err := client.StreamToolAnswer(context.Background(), []MCPModelMessage{{Role: "system", Content: "rules"}, {Role: "user", Content: "question"}}, func(s string) error { got.WriteString(s); return nil })
	if err == nil || got.String() != "partial" || calls != 1 {
		t.Fatal("truncated response was accepted or retried")
	}
}

type streamingMCPModelFake struct {
	mcpModelFake
	finalCalls     int
	contextHasTool bool
}

func (m *streamingMCPModelFake) StreamToolAnswer(ctx context.Context, messages []MCPModelMessage, emit func(string) error) error {
	m.finalCalls++
	if ctx.Err() != nil {
		return ctx.Err()
	}
	for _, message := range messages {
		if message.Role == "tool" {
			m.contextHasTool = true
		}
	}
	if err := emit("first"); err != nil {
		return err
	}
	return emit("second")
}

func TestOrchestratorStreamsFinalAnswerWithinDecisionLimit(t *testing.T) {
	for _, rounds := range []int{0, 3} {
		t.Run(string(rune('0'+rounds)), func(t *testing.T) {
			model := &streamingMCPModelFake{}
			if rounds == 0 {
				model.turns = []MCPModelTurn{{Content: "discard this complete draft"}}
			} else {
				for i := 0; i < rounds; i++ {
					model.turns = append(model.turns, MCPModelTurn{ToolCalls: []MCPProposedCall{{ID: string(rune('a' + i)), Name: modelToolAlias("lookup_course_term@campus-tools"), Arguments: json.RawMessage(`{"term":"MCP"}`)}}})
				}
			}
			registry := &mcpRegistryFake{bound: []MCPBoundTool{fakeBoundTool()}, result: MCPCallResult{Text: "本地词条说明"}}
			var chunks []string
			o := MCPOrchestrator{Registry: registry, Model: model}
			answer, err := o.Run(context.Background(), repositories.User{ID: 1, ClassID: 1}, 1, 1, []ChatMessage{{Role: "system", Content: "rules"}, {Role: "user", Content: "question"}}, nil, func(s string) error { chunks = append(chunks, s); return nil })
			expected := rounds
			if expected == 0 {
				expected = 1
			}
			if err != nil || answer != "firstsecond" || len(chunks) != 2 || model.calls != expected || model.finalCalls != 1 || registry.callCount != rounds || model.contextHasTool != (rounds > 0) {
				t.Fatalf("incorrect final streaming or request bound: calls=%d final=%d err=%v", model.calls, model.finalCalls, err)
			}
		})
	}
}
