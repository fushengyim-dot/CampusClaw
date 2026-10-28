package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var ErrVectorStore = errors.New("向量库暂不可用")
var qdrantCollectionName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

// Qdrant is a small REST client. It stores identifiers and class metadata only;
// source text remains in MySQL.
type Qdrant struct {
	base string
	http *http.Client
}

type VectorPoint struct {
	ID         uint64
	Vector     []float32
	ClassID    int64
	MaterialID int64
	EntryID    int64
	ChunkID    int64
}

type VectorHit struct {
	ChunkID int64   `json:"chunk_id"`
	Score   float64 `json:"score"`
}

func NewQdrant(baseURL string) (*Qdrant, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, ErrVectorStore
	}
	return &Qdrant{base: strings.TrimRight(baseURL, "/"), http: &http.Client{Timeout: 5 * time.Second}}, nil
}

func (q *Qdrant) request(ctx context.Context, method, path string, in, out any) error {
	if q == nil || q.http == nil {
		return ErrVectorStore
	}
	var body *bytes.Reader
	if in == nil {
		body = bytes.NewReader(nil)
	} else {
		data, err := json.Marshal(in)
		if err != nil {
			return ErrVectorStore
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, q.base+path, body)
	if err != nil {
		return ErrVectorStore
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := q.http.Do(req)
	if err != nil {
		return ErrVectorStore
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ErrVectorStore
	}
	if out != nil && json.NewDecoder(resp.Body).Decode(out) != nil {
		return ErrVectorStore
	}
	return nil
}

func collectionPath(name string) string { return "/collections/" + url.PathEscape(name) }

// EnsureCollection creates a missing collection and refuses incompatible settings.
// It never deletes or silently recreates an existing collection.
func (q *Qdrant) EnsureCollection(ctx context.Context, name string, dimensions int) error {
	if !qdrantCollectionName.MatchString(name) || dimensions < 1 || dimensions > 65536 {
		return ErrVectorStore
	}
	path := collectionPath(name)
	create := map[string]any{"vectors": map[string]any{"size": dimensions, "distance": "Cosine"}}
	if err := q.request(ctx, http.MethodPut, path, create, nil); err == nil {
		return nil
	}
	// A pre-existing collection or concurrent creator is accepted only when its
	// stored dimension and distance match. Network errors still fail the GET.
	var info struct {
		Result struct {
			Config struct {
				Params struct {
					Vectors struct {
						Size     int    `json:"size"`
						Distance string `json:"distance"`
					} `json:"vectors"`
				} `json:"params"`
			} `json:"config"`
		} `json:"result"`
	}
	err := q.request(ctx, http.MethodGet, path, nil, &info)
	if err == nil {
		if info.Result.Config.Params.Vectors.Size != dimensions || !strings.EqualFold(info.Result.Config.Params.Vectors.Distance, "Cosine") {
			return fmt.Errorf("%w: collection settings mismatch", ErrVectorStore)
		}
		return nil
	}
	return ErrVectorStore
}

func (q *Qdrant) Upsert(ctx context.Context, collection string, points []VectorPoint, dimensions int) error {
	if !qdrantCollectionName.MatchString(collection) || len(points) == 0 || len(points) > 256 || dimensions < 1 {
		return ErrVectorStore
	}
	items := make([]map[string]any, 0, len(points))
	for _, p := range points {
		if p.ID == 0 || p.ID != uint64(p.ChunkID) || p.ClassID < 1 || p.ChunkID < 1 || len(p.Vector) != dimensions {
			return ErrVectorStore
		}
		items = append(items, map[string]any{"id": p.ID, "vector": p.Vector, "payload": map[string]any{"class_id": p.ClassID, "material_id": p.MaterialID, "entry_id": p.EntryID, "chunk_id": p.ChunkID}})
	}
	return q.request(ctx, http.MethodPut, collectionPath(collection)+"/points?wait=true", map[string]any{"points": items}, nil)
}

// DeleteCollection is intended for uniquely named temporary acceptance collections.
func (q *Qdrant) DeleteCollection(ctx context.Context, collection string) error {
	if !qdrantCollectionName.MatchString(collection) || !strings.HasPrefix(collection, "campusclaw_test_") {
		return ErrVectorStore
	}
	return q.request(ctx, http.MethodDelete, collectionPath(collection), nil, nil)
}

func (q *Qdrant) Search(ctx context.Context, collection string, vector []float32, classID int64, limit, offset, dimensions int, materialScopes ...[]int64) ([]VectorHit, error) {
	if !qdrantCollectionName.MatchString(collection) || classID < 1 || len(vector) != dimensions || limit < 1 || limit > 20 || offset < 0 || offset > 500 {
		return nil, ErrVectorStore
	}
	var response struct {
		Result []struct {
			Payload struct {
				ChunkID int64 `json:"chunk_id"`
			} `json:"payload"`
			Score float64 `json:"score"`
		} `json:"result"`
	}
	must := []any{map[string]any{"key": "class_id", "match": map[string]any{"value": classID}}}
	if len(materialScopes) > 0 {
		if len(materialScopes[0]) == 0 {
			return []VectorHit{}, nil
		}
		ids := make([]any, 0, len(materialScopes[0]))
		for _, id := range materialScopes[0] {
			if id < 1 {
				return nil, ErrVectorStore
			}
			ids = append(ids, id)
		}
		must = append(must, map[string]any{"key": "material_id", "match": map[string]any{"any": ids}})
	}
	body := map[string]any{"vector": vector, "limit": limit, "offset": offset, "with_payload": []string{"chunk_id"}, "filter": map[string]any{"must": must}}
	if err := q.request(ctx, http.MethodPost, collectionPath(collection)+"/points/search", body, &response); err != nil {
		return nil, err
	}
	hits := make([]VectorHit, 0, len(response.Result))
	for _, item := range response.Result {
		hits = append(hits, VectorHit{ChunkID: item.Payload.ChunkID, Score: item.Score})
	}
	return hits, nil
}
