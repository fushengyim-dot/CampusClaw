package services

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"math"
	"sort"
	"strings"
	"sync"
)

var ErrKnowledgeUnavailable = errors.New("知识检索暂不可用")
var ErrVectorIndexUnavailable = errors.New("讲义向量索引暂不可用")

const VectorIndexBatchSize = 10

type VectorIndexProgress struct {
	Total     int `json:"total"`
	Ready     int `json:"ready"`
	Pending   int `json:"pending"`
	Failed    int `json:"failed"`
	Processed int `json:"processed"`
}

type KnowledgeHit struct {
	ChunkID       int64   `json:"chunk_id"`
	MaterialID    int64   `json:"material_id"`
	Title         string  `json:"title"`
	ChunkIndex    int     `json:"chunk_index"`
	CharStart     int     `json:"char_start"`
	CharEnd       int     `json:"char_end"`
	Excerpt       string  `json:"excerpt"`
	SourceURL     *string `json:"source_url"`
	SourceLocator *string `json:"source_locator"`
	Score         float64 `json:"score"`
}

// FuseKnowledgeRRF merges ranked keyword and vector results by rank, not by
// incomparable raw scores. Duplicate chunks within either input count once.
func FuseKnowledgeRRF(keyword, vector []KnowledgeHit, limit int) []KnowledgeHit {
	if limit < 1 || limit > 20 {
		return []KnowledgeHit{}
	}
	const k = 60.0
	byID := make(map[int64]KnowledgeHit)
	scores := make(map[int64]float64)
	add := func(items []KnowledgeHit) {
		seen := make(map[int64]bool, len(items))
		rank := 0
		for _, item := range items {
			if item.ChunkID < 1 || seen[item.ChunkID] {
				continue
			}
			seen[item.ChunkID] = true
			rank++
			if _, exists := byID[item.ChunkID]; !exists {
				byID[item.ChunkID] = item
			}
			scores[item.ChunkID] += 1 / (k + float64(rank))
		}
	}
	add(keyword)
	add(vector)
	merged := make([]KnowledgeHit, 0, len(byID))
	for id, item := range byID {
		item.Score = scores[id]
		merged = append(merged, item)
	}
	sort.Slice(merged, func(i, j int) bool {
		if math.Abs(merged[i].Score-merged[j].Score) < 1e-15 {
			return merged[i].ChunkID < merged[j].ChunkID
		}
		return merged[i].Score > merged[j].Score
	})
	if len(merged) > limit {
		merged = merged[:limit]
	}
	return merged
}

func FilterVectorCandidates(results []VectorHit) []VectorHit {
	filtered := make([]VectorHit, 0, len(results))
	seen := make(map[int64]bool, len(results))
	for _, result := range results {
		if result.ChunkID < 1 || result.Score < 0.35 || seen[result.ChunkID] {
			continue
		}
		seen[result.ChunkID] = true
		filtered = append(filtered, result)
	}
	return filtered
}

type QueryEmbedder interface {
	EmbedQuery(context.Context, string) ([]float32, error)
}

type QueryEmbedderFunc func(context.Context, string) ([]float32, error)

func (f QueryEmbedderFunc) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	return f(ctx, query)
}

type Knowledge struct {
	db         *sql.DB
	qdrant     *Qdrant
	embedder   QueryEmbedder
	collection string
	dimensions int
	indexMu    sync.Mutex
}

func NewKnowledge(db *sql.DB) *Knowledge { return &Knowledge{db: db} }

func NewKnowledgeWithVector(db *sql.DB, qdrant *Qdrant, embedder QueryEmbedder, collection string, dimensions int) *Knowledge {
	return &Knowledge{db: db, qdrant: qdrant, embedder: embedder, collection: collection, dimensions: dimensions}
}

// SearchVector embeds only the current query, filters in Qdrant by the
// authenticated class, applies the course threshold, then rechecks that class
// in MySQL before loading any source text.
func (k *Knowledge) SearchVector(ctx context.Context, classID int64, query string, limit, offset int) ([]KnowledgeHit, error) {
	return k.searchVector(ctx, classID, query, nil, limit, offset)
}

// SearchVectorMaterials additionally restricts retrieval at the vector store and MySQL layers.
func (k *Knowledge) SearchVectorMaterials(ctx context.Context, classID int64, query string, materialIDs []int64, limit int) ([]KnowledgeHit, error) {
	if len(materialIDs) == 0 {
		return []KnowledgeHit{}, nil
	}
	return k.searchVector(ctx, classID, query, materialIDs, limit, 0)
}

func (k *Knowledge) searchVector(ctx context.Context, classID int64, query string, materialIDs []int64, limit, offset int) ([]KnowledgeHit, error) {
	if k == nil || k.db == nil || k.qdrant == nil || k.embedder == nil || k.collection == "" || classID < 1 || strings.TrimSpace(query) == "" || limit < 1 || limit > 20 || offset < 0 || offset > 500 {
		return nil, ErrKnowledgeUnavailable
	}
	vector, err := k.embedder.EmbedQuery(ctx, query)
	if err != nil || len(vector) != k.dimensions {
		return nil, ErrKnowledgeUnavailable
	}
	var results []VectorHit
	if materialIDs == nil {
		results, err = k.qdrant.Search(ctx, k.collection, vector, classID, limit, offset, k.dimensions)
	} else {
		results, err = k.qdrant.Search(ctx, k.collection, vector, classID, limit, offset, k.dimensions, materialIDs)
	}
	if err != nil {
		return nil, ErrKnowledgeUnavailable
	}
	accepted := FilterVectorCandidates(results)
	ids := make([]string, 0, len(results))
	args := make([]any, 0, len(results)+1)
	for _, result := range accepted {
		ids = append(ids, "?")
		args = append(args, result.ChunkID)
	}
	if len(accepted) == 0 {
		return []KnowledgeHit{}, nil
	}
	args = append(args, classID)
	materialClause := ""
	if materialIDs != nil {
		materialClause = " AND m.id IN (" + placeholders(len(materialIDs)) + ")"
		for _, id := range materialIDs {
			args = append(args, id)
		}
	}
	rows, err := k.db.QueryContext(ctx, `SELECT c.id,m.id,m.title,c.chunk_index,c.char_start,c.char_end,c.chunk_text,m.source_url,m.source_locator
		FROM knowledge_chunks c JOIN knowledge_entries e ON e.id=c.knowledge_entry_id
		JOIN materials m ON m.id=e.material_id AND m.class_id=e.class_id
		JOIN knowledge_vector_state vs ON vs.chunk_id=c.id AND vs.status='ready' AND vs.content_sha256=LOWER(SHA2(c.chunk_text,256))
		WHERE c.id IN (`+strings.Join(ids, ",")+`) AND e.class_id=? AND c.index_status='ready'`+materialClause, args...)
	if err != nil {
		return nil, ErrKnowledgeUnavailable
	}
	byID := make(map[int64]KnowledgeHit, len(accepted))
	for rows.Next() {
		var hit KnowledgeHit
		if err := rows.Scan(&hit.ChunkID, &hit.MaterialID, &hit.Title, &hit.ChunkIndex, &hit.CharStart, &hit.CharEnd, &hit.Excerpt, &hit.SourceURL, &hit.SourceLocator); err != nil {
			rows.Close()
			return nil, ErrKnowledgeUnavailable
		}
		byID[hit.ChunkID] = hit
	}
	if rows.Err() != nil {
		rows.Close()
		return nil, ErrKnowledgeUnavailable
	}
	if rows.Close() != nil {
		return nil, ErrKnowledgeUnavailable
	}
	hits := make([]KnowledgeHit, 0, len(byID))
	for _, result := range accepted {
		if hit, ok := byID[result.ChunkID]; ok {
			hit.Score = result.Score
			hits = append(hits, hit)
		}
	}
	return hits, nil
}

// VectorIndexStatus returns vector readiness for one material in the caller's
// class. A ready record counts only while its saved content hash still matches.
func (k *Knowledge) VectorIndexStatus(ctx context.Context, classID, materialID int64) (VectorIndexProgress, error) {
	if k == nil || k.db == nil || classID < 1 || materialID < 1 {
		return VectorIndexProgress{}, ErrVectorIndexUnavailable
	}
	var progress VectorIndexProgress
	var failed int
	err := k.db.QueryRowContext(ctx, `SELECT COUNT(*),
		COALESCE(SUM(CASE WHEN vs.status='ready' AND vs.content_sha256=LOWER(SHA2(c.chunk_text,256)) THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN vs.status='failed' AND vs.content_sha256=LOWER(SHA2(c.chunk_text,256)) THEN 1 ELSE 0 END),0)
		FROM knowledge_chunks c
		JOIN knowledge_entries e ON e.id=c.knowledge_entry_id
		JOIN materials m ON m.id=e.material_id AND m.class_id=e.class_id
		LEFT JOIN knowledge_vector_state vs ON vs.chunk_id=c.id
		WHERE e.class_id=? AND m.id=? AND c.index_status='ready'`, classID, materialID).Scan(&progress.Total, &progress.Ready, &failed)
	if err != nil {
		return VectorIndexProgress{}, ErrVectorIndexUnavailable
	}
	progress.Failed = failed
	progress.Pending = progress.Total - progress.Ready - progress.Failed
	return progress, nil
}

// IndexMaterialVectors processes at most ten missing or changed chunks. Upload
// never calls this method; a same-class teacher must explicitly invoke it.
func (k *Knowledge) IndexMaterialVectors(ctx context.Context, classID, materialID int64) (VectorIndexProgress, error) {
	if k == nil || k.db == nil || k.qdrant == nil || k.embedder == nil || k.collection == "" || k.dimensions < 1 || classID < 1 || materialID < 1 {
		return VectorIndexProgress{}, ErrVectorIndexUnavailable
	}
	k.indexMu.Lock()
	defer k.indexMu.Unlock()
	rows, err := k.db.QueryContext(ctx, `SELECT c.id,e.id,m.id,c.chunk_text
		FROM knowledge_chunks c
		JOIN knowledge_entries e ON e.id=c.knowledge_entry_id
		JOIN materials m ON m.id=e.material_id AND m.class_id=e.class_id
		LEFT JOIN knowledge_vector_state vs ON vs.chunk_id=c.id
		WHERE e.class_id=? AND m.id=? AND c.index_status='ready'
		AND (vs.status IS NULL OR vs.status<>'ready' OR vs.content_sha256 IS NULL OR vs.content_sha256<>LOWER(SHA2(c.chunk_text,256)))
		ORDER BY c.chunk_index,c.id LIMIT ?`, classID, materialID, VectorIndexBatchSize)
	if err != nil {
		return VectorIndexProgress{}, ErrVectorIndexUnavailable
	}
	type chunkToIndex struct {
		id, entryID, materialID int64
		text                    string
	}
	chunks := make([]chunkToIndex, 0, VectorIndexBatchSize)
	for rows.Next() {
		var item chunkToIndex
		if err := rows.Scan(&item.id, &item.entryID, &item.materialID, &item.text); err != nil {
			rows.Close()
			return VectorIndexProgress{}, ErrVectorIndexUnavailable
		}
		chunks = append(chunks, item)
	}
	if rows.Err() != nil || rows.Close() != nil {
		return VectorIndexProgress{}, ErrVectorIndexUnavailable
	}
	progress := VectorIndexProgress{}
	for _, chunk := range chunks {
		digest := sha256.Sum256([]byte(chunk.text))
		hash := hex.EncodeToString(digest[:])
		if _, err := k.db.ExecContext(ctx, `INSERT INTO knowledge_vector_state(chunk_id,status,content_sha256,indexed_at)
			VALUES(?,'pending',?,NULL) ON DUPLICATE KEY UPDATE status='pending',content_sha256=VALUES(content_sha256),indexed_at=NULL`, chunk.id, hash); err != nil {
			return progress, ErrVectorIndexUnavailable
		}
		vector, err := k.embedder.EmbedQuery(ctx, chunk.text)
		if err == nil && len(vector) != k.dimensions {
			err = ErrEmbeddingUnavailable
		}
		if err == nil {
			err = k.qdrant.Upsert(ctx, k.collection, []VectorPoint{{ID: uint64(chunk.id), ChunkID: chunk.id, ClassID: classID, MaterialID: chunk.materialID, EntryID: chunk.entryID, Vector: vector}}, k.dimensions)
		}
		if err != nil {
			_, _ = k.db.ExecContext(ctx, `UPDATE knowledge_vector_state SET status='failed',content_sha256=?,indexed_at=NULL WHERE chunk_id=?`, hash, chunk.id)
			break // No implicit retries: a teacher can resume by clicking again.
		}
		if _, err := k.db.ExecContext(ctx, `UPDATE knowledge_vector_state SET status='ready',content_sha256=?,indexed_at=CURRENT_TIMESTAMP(6) WHERE chunk_id=?`, hash, chunk.id); err != nil {
			return progress, ErrVectorIndexUnavailable
		}
		progress.Processed++
	}
	status, statusErr := k.VectorIndexStatus(ctx, classID, materialID)
	if statusErr != nil {
		return progress, statusErr
	}
	status.Processed = progress.Processed
	return status, nil
}

func (k *Knowledge) SearchHybrid(ctx context.Context, classID int64, query string, limit, offset int) ([]KnowledgeHit, error) {
	if offset+limit > 20 {
		return nil, ErrKnowledgeUnavailable
	}
	top := limit + offset
	keyword, err := k.SearchKeyword(ctx, classID, query, top, 0)
	if err != nil {
		return nil, err
	}
	vector, err := k.SearchVector(ctx, classID, query, top, 0)
	if err != nil {
		return nil, err
	}
	merged := FuseKnowledgeRRF(keyword, vector, top)
	if offset >= len(merged) {
		return []KnowledgeHit{}, nil
	}
	end := offset + limit
	if end > len(merged) {
		end = len(merged)
	}
	return merged[offset:end], nil
}

// SearchHybridMaterials enforces the agent's material allow-list in both retrieval paths.
func (k *Knowledge) SearchHybridMaterials(ctx context.Context, classID int64, query string, materialIDs []int64, limit int) ([]KnowledgeHit, error) {
	if k == nil || len(materialIDs) == 0 {
		return []KnowledgeHit{}, nil
	}
	if limit < 1 || limit > 20 {
		return nil, ErrKnowledgeUnavailable
	}
	keyword, err := k.searchKeywordMaterials(ctx, classID, query, materialIDs, limit)
	if err != nil {
		return nil, err
	}
	vector, err := k.SearchVectorMaterials(ctx, classID, query, materialIDs, limit)
	if err != nil {
		return nil, err
	}
	return FuseKnowledgeRRF(keyword, vector, limit), nil
}

func (k *Knowledge) searchKeywordMaterials(ctx context.Context, classID int64, query string, materialIDs []int64, limit int) ([]KnowledgeHit, error) {
	if k == nil || k.db == nil || classID < 1 || strings.TrimSpace(query) == "" || len(materialIDs) == 0 || limit < 1 || limit > 20 {
		return []KnowledgeHit{}, nil
	}
	marks := placeholders(len(materialIDs))
	args := []any{query, classID, query}
	for _, id := range materialIDs {
		args = append(args, id)
	}
	args = append(args, limit)
	rows, err := k.db.QueryContext(ctx, `SELECT c.id,m.id,m.title,c.chunk_index,c.char_start,c.char_end,c.chunk_text,m.source_url,m.source_locator,MATCH(c.chunk_text) AGAINST (? IN NATURAL LANGUAGE MODE) AS relevance
	FROM knowledge_chunks c JOIN knowledge_entries e ON e.id=c.knowledge_entry_id JOIN materials m ON m.id=e.material_id AND m.class_id=e.class_id
	WHERE e.class_id=? AND c.index_status='ready' AND MATCH(c.chunk_text) AGAINST (? IN NATURAL LANGUAGE MODE)>0 AND m.id IN (`+marks+`) ORDER BY relevance DESC,c.id ASC LIMIT ?`, args...)
	if err != nil {
		return nil, ErrKnowledgeUnavailable
	}
	defer rows.Close()
	hits := []KnowledgeHit{}
	for rows.Next() {
		var h KnowledgeHit
		if rows.Scan(&h.ChunkID, &h.MaterialID, &h.Title, &h.ChunkIndex, &h.CharStart, &h.CharEnd, &h.Excerpt, &h.SourceURL, &h.SourceLocator, &h.Score) != nil {
			return nil, ErrKnowledgeUnavailable
		}
		hits = append(hits, h)
	}
	if rows.Err() != nil {
		return nil, ErrKnowledgeUnavailable
	}
	return hits, nil
}

func placeholders(n int) string {
	if n < 1 {
		return ""
	}
	return strings.TrimRight(strings.Repeat("?,", n), ",")
}

// EnsureChunks backfills legacy knowledge entries that predate chunk indexing.
// Existing source text and complete chunk sets are left unchanged.
func (k *Knowledge) EnsureChunks(ctx context.Context) error {
	if k == nil || k.db == nil {
		return ErrKnowledgeUnavailable
	}
	rows, err := k.db.QueryContext(ctx, `SELECT e.id,e.content FROM knowledge_entries e
		WHERE NOT EXISTS (SELECT 1 FROM knowledge_chunks c WHERE c.knowledge_entry_id=e.id)
		ORDER BY e.id`)
	if err != nil {
		return ErrKnowledgeUnavailable
	}
	type source struct {
		id      int64
		content string
	}
	var sources []source
	for rows.Next() {
		var item source
		if err := rows.Scan(&item.id, &item.content); err != nil {
			rows.Close()
			return ErrKnowledgeUnavailable
		}
		sources = append(sources, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return ErrKnowledgeUnavailable
	}
	if err := rows.Close(); err != nil {
		return ErrKnowledgeUnavailable
	}
	for _, item := range sources {
		chunks, err := ChunkText(item.content, ChunkOptions{})
		if err != nil {
			return ErrKnowledgeUnavailable
		}
		tx, err := k.db.BeginTx(ctx, nil)
		if err != nil {
			return ErrKnowledgeUnavailable
		}
		for start := 0; start < len(chunks); start += 100 {
			end := start + 100
			if end > len(chunks) {
				end = len(chunks)
			}
			if err = insertChunks(ctx, tx, item.id, chunks[start:end]); err != nil {
				_ = tx.Rollback()
				return ErrKnowledgeUnavailable
			}
		}
		if err = tx.Commit(); err != nil {
			return ErrKnowledgeUnavailable
		}
	}
	return nil
}

func (k *Knowledge) SearchKeyword(ctx context.Context, classID int64, query string, limit, offset int) ([]KnowledgeHit, error) {
	if k == nil || k.db == nil || classID < 1 || query == "" || limit < 1 || limit > 20 || offset < 0 || offset > 500 {
		return nil, ErrKnowledgeUnavailable
	}
	rows, err := k.db.QueryContext(ctx, `SELECT c.id,m.id,m.title,c.chunk_index,c.char_start,c.char_end,c.chunk_text,
		m.source_url,m.source_locator,MATCH(c.chunk_text) AGAINST (? IN NATURAL LANGUAGE MODE) AS relevance
		FROM knowledge_chunks c
		JOIN knowledge_entries e ON e.id=c.knowledge_entry_id
		JOIN materials m ON m.id=e.material_id AND m.class_id=e.class_id
		WHERE e.class_id=? AND c.index_status='ready'
		AND MATCH(c.chunk_text) AGAINST (? IN NATURAL LANGUAGE MODE) > 0
		ORDER BY relevance DESC,c.id ASC LIMIT ? OFFSET ?`, query, classID, query, limit, offset)
	if err != nil {
		return nil, ErrKnowledgeUnavailable
	}
	defer rows.Close()
	hits := make([]KnowledgeHit, 0)
	for rows.Next() {
		var hit KnowledgeHit
		if err := rows.Scan(&hit.ChunkID, &hit.MaterialID, &hit.Title, &hit.ChunkIndex, &hit.CharStart, &hit.CharEnd, &hit.Excerpt, &hit.SourceURL, &hit.SourceLocator, &hit.Score); err != nil {
			return nil, ErrKnowledgeUnavailable
		}
		hits = append(hits, hit)
	}
	if rows.Err() != nil {
		return nil, ErrKnowledgeUnavailable
	}
	return hits, nil
}
