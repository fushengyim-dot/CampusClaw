package services

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

var ErrChatStreamUnavailable = errors.New("流式对话服务暂不可用")

type ChatStreamer interface {
	Stream(context.Context, []ChatMessage, func(string) error) error
}

// Stream reads OpenAI-compatible SSE chunks and emits only newly generated text.
func (c *ChatClient) Stream(ctx context.Context, messages []ChatMessage, emit func(string) error) error {
	if c == nil || c.http == nil || emit == nil || len(messages) < 2 || len(messages) > 10 {
		return ErrChatStreamUnavailable
	}
	for i, m := range messages {
		if m.Content == "" || (m.Role != "system" && m.Role != "user" && m.Role != "assistant") || (i == 0 && m.Role != "system") {
			return ErrChatStreamUnavailable
		}
	}
	return c.streamPayload(ctx, map[string]any{"model": c.model, "messages": messages, "stream": true}, emit)
}

// StreamToolAnswer generates new text from the validated tool conversation.
func (c *ChatClient) StreamToolAnswer(ctx context.Context, messages []MCPModelMessage, emit func(string) error) error {
	if c == nil || c.http == nil || emit == nil {
		return ErrChatStreamUnavailable
	}
	wire, err := mcpWireMessages(messages)
	if err != nil {
		return ErrChatStreamUnavailable
	}
	return c.streamPayload(ctx, map[string]any{"model": c.model, "messages": wire, "stream": true, "tool_choice": "none"}, emit)
}

func (c *ChatClient) streamPayload(ctx context.Context, payload map[string]any, emit func(string) error) error {
	body, err := json.Marshal(payload)
	if err != nil || len(body) > 20*1024*1024 {
		return ErrChatStreamUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, strings.NewReader(string(body)))
	if err != nil {
		return ErrChatStreamUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	client := c.streamHTTP
	if client == nil {
		client = c.http
	}
	resp, err := client.Do(req)
	if err != nil {
		return ErrChatStreamUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ErrChatStreamUnavailable
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	var answer int
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			if answer == 0 {
				return ErrChatStreamUnavailable
			}
			return nil
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(data), &chunk) != nil {
			return ErrChatStreamUnavailable
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		text := chunk.Choices[0].Delta.Content
		if text == "" {
			continue
		}
		answer += len([]rune(text))
		if answer > 8000 || emit(text) != nil {
			return ErrChatStreamUnavailable
		}
	}
	return ErrChatStreamUnavailable
}

// LocalStubStreamer is used only for offline acceptance; it never performs network I/O.
type LocalStubStreamer struct {
	Parts     []string
	FailAfter int
}

func (s LocalStubStreamer) Stream(_ context.Context, _ []ChatMessage, emit func(string) error) error {
	for i, p := range s.Parts {
		if s.FailAfter > 0 && i >= s.FailAfter {
			return fmt.Errorf("%w", ErrChatStreamUnavailable)
		}
		if err := emit(p); err != nil {
			return err
		}
	}
	if len(s.Parts) == 0 {
		return ErrChatStreamUnavailable
	}
	return nil
}
