package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQdrantEnsureCollectionAndClassFilteredSearch(t *testing.T) {
	created := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/collections/campusclaw_test_chunks" && !strings.HasPrefix(r.URL.Path, "/collections/campusclaw_test_chunks/points") {
			t.Fatalf("unexpected endpoint: %s", r.URL.Path)
		}
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/collections/campusclaw_test_chunks":
			created = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/collections/campusclaw_test_chunks":
			if !created {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":{"config":{"params":{"vectors":{"size":4,"distance":"Cosine"}}}}}`))
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/points") && r.URL.Query().Get("wait") == "true":
			var body struct {
				Points []struct {
					Payload map[string]any `json:"payload"`
					Vector  []float32      `json:"vector"`
				} `json:"points"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Points) != 1 || body.Points[0].Payload["class_id"] != float64(8) || body.Points[0].Payload["text"] != nil {
				t.Error("upsert must contain identifier metadata only")
				http.Error(w, "invalid", http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/points/search"):
			var body struct {
				Filter struct {
					Must []struct {
						Key   string `json:"key"`
						Match struct {
							Value int64 `json:"value"`
						} `json:"match"`
					} `json:"must"`
				} `json:"filter"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Filter.Must) != 1 || body.Filter.Must[0].Key != "class_id" || body.Filter.Must[0].Match.Value != 8 {
				t.Error("search must apply the server class filter")
				http.Error(w, "invalid", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":[{"payload":{"chunk_id":99},"score":0.9}]}`))
		default:
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := NewQdrant(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := client.EnsureCollection(ctx, "campusclaw_test_chunks", 4); err != nil {
		t.Fatal(err)
	}
	if err := client.Upsert(ctx, "campusclaw_test_chunks", []VectorPoint{{ID: 99, ChunkID: 99, ClassID: 8, MaterialID: 7, EntryID: 6, Vector: []float32{1, 0, 0, 0}}}, 4); err != nil {
		t.Fatal(err)
	}
	hits, err := client.Search(ctx, "campusclaw_test_chunks", []float32{1, 0, 0, 0}, 8, 5, 0, 4)
	if err != nil || len(hits) != 1 || hits[0].ChunkID != 99 {
		t.Fatalf("unexpected hits %#v, %v", hits, err)
	}
}

func TestQdrantRejectsDimensionMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			http.Error(w, "already exists", http.StatusConflict)
			return
		}
		_, _ = w.Write([]byte(`{"result":{"config":{"params":{"vectors":{"size":3,"distance":"Cosine"}}}}}`))
	}))
	defer server.Close()
	client, _ := NewQdrant(server.URL)
	if err := client.EnsureCollection(context.Background(), "campusclaw_test_chunks", 4); err == nil {
		t.Fatal("expected incompatible collection to be rejected")
	}
}

func TestQdrantUpsertRequiresChunkIDAsPointID(t *testing.T) {
	client, err := NewQdrant("http://127.0.0.1:6333")
	if err != nil {
		t.Fatal(err)
	}
	err = client.Upsert(context.Background(), "campusclaw_test_chunks", []VectorPoint{{ID: 1, ChunkID: 2, ClassID: 3, Vector: []float32{1, 0}}}, 2)
	if err == nil {
		t.Fatal("expected Qdrant point ID to equal its MySQL chunk ID")
	}
}
