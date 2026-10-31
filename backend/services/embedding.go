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

var ErrEmbeddingUnavailable = errors.New("嵌入服务暂不可用")

// EmbeddingDiagnostic only carries a safe failure category and, when available,
// the upstream HTTP status. It never contains response bodies, URLs, or secrets.
type EmbeddingDiagnostic struct {
	Category   string
	HTTPStatus int
}

func (e *EmbeddingDiagnostic) Error() string { return ErrEmbeddingUnavailable.Error() }
func (e *EmbeddingDiagnostic) Unwrap() error { return ErrEmbeddingUnavailable }

func embeddingFailure(category string, status int) error {
	return &EmbeddingDiagnostic{Category: category, HTTPStatus: status}
}

type EmbeddingConfig struct {
	BaseURL    string
	APIKey     string
	Model      string
	Dimensions int
}

type EmbeddingClient struct {
	endpoint string
	apiKey   string
	model    string
	dim      int
	http     *http.Client
}

func NewEmbeddingClient(cfg EmbeddingConfig) (*EmbeddingClient, error) {
	baseURL := strings.TrimSpace(cfg.BaseURL)
	u, err := url.Parse(baseURL)
	if err != nil || u == nil || u.Scheme != "https" || u.Hostname() != "ai-gateway.devops.hello1023.com" || u.Port() != "" || u.User != nil || u.Path != "/v1" || u.RawQuery != "" || u.Fragment != "" || cfg.APIKey == "" || cfg.APIKey != strings.TrimSpace(cfg.APIKey) || strings.ContainsAny(cfg.APIKey, "\r\n") || strings.TrimSpace(cfg.Model) == "" || cfg.Dimensions < 1 || cfg.Dimensions > 65536 {
		return nil, ErrEmbeddingUnavailable
	}
	client := &http.Client{Transport: captureTransport(), Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &EmbeddingClient{endpoint: strings.TrimRight(baseURL, "/") + "/embeddings", apiKey: cfg.APIKey, model: strings.TrimSpace(cfg.Model), dim: cfg.Dimensions, http: client}, nil
}

func (c *EmbeddingClient) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	if c == nil || c.http == nil || strings.TrimSpace(text) == "" || len(text) > 10000 {
		return nil, embeddingFailure("invalid local input", 0)
	}
	body, err := json.Marshal(map[string]any{"model": c.model, "input": text})
	if err != nil || len(body) > 20*1024*1024 {
		return nil, embeddingFailure("request preparation failed", 0)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, embeddingFailure("request preparation failed", 0)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, embeddingFailure("network or timeout", 0)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, embeddingFailure("gateway rejected request", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024+1))
	if err != nil || len(data) > 4*1024*1024 {
		return nil, embeddingFailure("response read failed or too large", resp.StatusCode)
	}
	var payload struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if json.Unmarshal(data, &payload) != nil {
		return nil, embeddingFailure("invalid response JSON", resp.StatusCode)
	}
	if len(payload.Data) != 1 {
		return nil, embeddingFailure("unexpected response item count", resp.StatusCode)
	}
	if len(payload.Data[0].Embedding) != c.dim {
		return nil, embeddingFailure("vector dimension mismatch", resp.StatusCode)
	}
	vector := make([]float32, len(payload.Data[0].Embedding))
	for i, value := range payload.Data[0].Embedding {
		if value != value || value > 3.4028235e38 || value < -3.4028235e38 {
			return nil, embeddingFailure("invalid vector values", resp.StatusCode)
		}
		vector[i] = float32(value)
	}
	if len(vector) != c.dim {
		return nil, embeddingFailure("vector dimension mismatch", resp.StatusCode)
	}
	return vector, nil
}
