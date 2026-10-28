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

func TestEmbeddingClientSendsCourseRequestAndChecksDimension(t *testing.T) {
	key := "test-secret-never-log"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/embeddings" || r.Header.Get("Authorization") != "Bearer "+key {
			t.Fatalf("unexpected request metadata")
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil || body["model"] != "course-embedding" || body["input"] != "synthetic smoke test" || len(body) != 2 {
			t.Fatalf("request body must contain model and input only")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.25,-0.5]}]}`))
	}))
	defer server.Close()
	client := &EmbeddingClient{endpoint: server.URL + "/v1/embeddings", apiKey: key, model: "course-embedding", dim: 2, http: server.Client()}
	vector, err := client.EmbedQuery(context.Background(), "synthetic smoke test")
	if err != nil || len(vector) != 2 || vector[0] != 0.25 || vector[1] != -0.5 {
		t.Fatalf("valid embedding failed: dimensions=%d err=%v", len(vector), err)
	}
}

func TestEmbeddingClientRejectsBadConfigAndResponseWithoutLeakingKey(t *testing.T) {
	if _, err := NewEmbeddingClient(EmbeddingConfig{BaseURL: "http://bad.example/v1", APIKey: "x", Model: "m", Dimensions: 2}); !errors.Is(err, ErrEmbeddingUnavailable) {
		t.Fatal("insecure endpoint must be rejected")
	}
	if _, err := NewEmbeddingClient(EmbeddingConfig{BaseURL: "https://other.example/v1", APIKey: "x", Model: "m", Dimensions: 2}); !errors.Is(err, ErrEmbeddingUnavailable) {
		t.Fatal("non-course endpoint must be rejected")
	}
	if _, err := NewEmbeddingClient(EmbeddingConfig{BaseURL: "https://gateway.example/v1", APIKey: "x\r\nX-Test: injected", Model: "m", Dimensions: 2}); !errors.Is(err, ErrEmbeddingUnavailable) {
		t.Fatal("header-injection key must be rejected")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("rejected"))
	}))
	defer server.Close()
	secret := "test-secret-never-log"
	client := &EmbeddingClient{endpoint: server.URL, apiKey: secret, model: "m", dim: 2, http: server.Client()}
	_, err := client.EmbedQuery(context.Background(), "test")
	if !errors.Is(err, ErrEmbeddingUnavailable) || strings.Contains(err.Error(), secret) {
		t.Fatal("upstream failure must be generic and must not leak credentials")
	}
	wrongSize := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"embedding":[1]}]}`))
	}))
	defer wrongSize.Close()
	client.endpoint, client.http = wrongSize.URL, wrongSize.Client()
	if _, err := client.EmbedQuery(context.Background(), "test"); !errors.Is(err, ErrEmbeddingUnavailable) {
		t.Fatal("wrong vector dimension must fail closed")
	}
}
