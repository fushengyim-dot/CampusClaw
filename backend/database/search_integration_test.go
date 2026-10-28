package database

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"campusclaw/backend/handlers"
	"campusclaw/backend/repositories"
	"campusclaw/backend/services"
	"golang.org/x/crypto/bcrypt"
)

func TestSearchIntegration(t *testing.T) {
	if os.Getenv("CAMPUSCLAW_SEARCH_INTEGRATION") != "1" {
		t.Skip("explicit class-scoped search integration required")
	}
	if os.Getenv("CAMPUSCLAW_MYSQL_INTEGRATION") != "1" {
		t.Fatal("explicit project MySQL integration environment required")
	}
	c := integrationConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db, err := Open(ctx, c)
	if err != nil {
		t.Fatal("project database unavailable")
	}
	t.Cleanup(func() { _ = db.Close() })
	var classAID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM classes WHERE name='A班' ORDER BY id LIMIT 1`).Scan(&classAID); err != nil || classAID < 1 {
		t.Fatal("class A unavailable for classroom material acceptance")
	}
	var targetMaterialID int64
	var targetTitle, sourceURL, sourceLocator string
	var entries, contentRunes, chunks, readyChunks, vectorReadyChunks int
	err = db.QueryRowContext(ctx, `SELECT m.id,m.title,COALESCE(m.source_url,''),COALESCE(m.source_locator,''),
		COUNT(DISTINCT e.id),COALESCE(MAX(CHAR_LENGTH(e.content)),0),COUNT(DISTINCT c.id),
		COUNT(DISTINCT CASE WHEN c.index_status='ready' THEN c.id END),
		COUNT(DISTINCT CASE WHEN v.status='ready' THEN c.id END)
		FROM materials m
		LEFT JOIN knowledge_entries e ON e.material_id=m.id AND e.class_id=m.class_id
		LEFT JOIN knowledge_chunks c ON c.knowledge_entry_id=e.id
		LEFT JOIN knowledge_vector_state v ON v.chunk_id=c.id
		WHERE m.class_id=? AND m.original_filename='lesson-03-auth-class-scope.md' AND m.title LIKE '%来源核对版%'
		GROUP BY m.id,m.title,m.source_url,m.source_locator ORDER BY m.id LIMIT 1`, classAID).Scan(&targetMaterialID, &targetTitle, &sourceURL, &sourceLocator, &entries, &contentRunes, &chunks, &readyChunks, &vectorReadyChunks)
	if err != nil || targetMaterialID < 1 || entries != 1 || contentRunes == 0 || chunks == 0 || readyChunks == 0 || sourceURL == "" || sourceLocator == "" {
		t.Fatal("source-verified classroom material, saved text, ready chunks, or traceable source metadata was missing before recovery")
	}
	removeAbortedVectorIndexFixtures(t, ctx, db)
	materialService, err := services.NewMaterials(db, os.Getenv("UPLOAD_ROOT"))
	if err != nil || materialService.Recover(ctx) != nil {
		t.Fatal("current project uploads did not pass material recovery")
	}
	if err := services.NewKnowledge(db).EnsureChunks(ctx); err != nil {
		t.Fatal("existing lesson materials could not be chunked")
	}
	auth, err := services.NewAuth(repositories.NewSessions(db), c.SessionSecret, nil)
	if err != nil {
		t.Fatal("session service unavailable")
	}
	suffix := time.Now().UTC().UnixNano()
	usernameA, usernameB := fmt.Sprintf("search_it_a_%d", suffix), fmt.Sprintf("search_it_b_%d", suffix)
	password := "local-search-integration-only"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal("temporary integration password hash unavailable")
	}
	for _, account := range []struct {
		username string
		classID  int
	}{{usernameA, 1}, {usernameB, 2}} {
		if _, err := db.ExecContext(ctx, "INSERT INTO users(username,password_hash,role,class_id) VALUES(?,?,'student',?)", account.username, string(hash), account.classID); err != nil {
			t.Fatal("temporary class-scoped test account could not be created")
		}
	}
	defer func() {
		for _, username := range []string{usernameA, usernameB} {
			if _, err := db.ExecContext(context.Background(), "DELETE FROM sessions WHERE user_id=(SELECT id FROM users WHERE username=?)", username); err != nil {
				t.Error("temporary search session cleanup failed")
			}
			if _, err := db.ExecContext(context.Background(), "DELETE FROM users WHERE username=?", username); err != nil {
				t.Error("temporary search account cleanup failed")
			}
		}
	}()
	handler := handlers.NewApplication(auth, materialService, c.AppOrigin, services.NewKnowledge(db))
	server := httptest.NewServer(handler)
	defer server.Close()
	type result struct {
		Items []services.KnowledgeHit `json:"items"`
	}
	search := func(username, password string, claimedClass int64, query string) result {
		t.Helper()
		if password == "" {
			t.Fatal("test user credential was not loaded into the isolated check container")
		}
		_, token, _, err := auth.Login(ctx, username, password, "")
		if err != nil {
			t.Fatalf("test user could not log in: %v", err)
		}
		payload, _ := json.Marshal(map[string]any{"q": query, "mode": "keyword", "class_id": claimedClass})
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/api/search", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "campusclaw_session", Value: token})
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal("search request failed")
		}
		defer response.Body.Close()
		var body result
		if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&body) != nil {
			t.Fatal("search did not return a safe successful response")
		}
		logout, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/api/logout", nil)
		logout.Header.Set("Origin", c.AppOrigin)
		logout.AddCookie(&http.Cookie{Name: "campusclaw_session", Value: token})
		logoutResponse, err := http.DefaultClient.Do(logout)
		if err != nil || logoutResponse.StatusCode != http.StatusNoContent {
			if logoutResponse != nil {
				logoutResponse.Body.Close()
			}
			t.Fatal("test session cleanup failed")
		}
		logoutResponse.Body.Close()
		return body
	}
	a := search(usernameA, password, 2, "授权")
	var targetFound bool
	for _, hit := range a.Items {
		if hit.MaterialID == targetMaterialID && hit.Title == targetTitle && hit.Excerpt != "" && hit.SourceURL != nil && *hit.SourceURL == sourceURL && hit.SourceLocator != nil && *hit.SourceLocator == sourceLocator {
			targetFound = true
			break
		}
	}
	if !targetFound {
		t.Fatal("authenticated A-class keyword retrieval did not return the source-verified classroom material with its traceable citation")
	}
	b := search(usernameB, password, 1, "授权")
	for _, hit := range b.Items {
		if hit.MaterialID == targetMaterialID {
			t.Fatal("B-class session received the A-class classroom material despite a forged class_id")
		}
	}
	qdrant, err := services.NewQdrant(os.Getenv("QDRANT_URL"))
	if err != nil {
		t.Fatal("local Qdrant was not available for the bound-material hybrid check")
	}
	queryVector := make([]float32, 2048)
	queryVector[0] = 1
	boundSearch := services.NewKnowledgeWithVector(db, qdrant, services.QueryEmbedderFunc(func(context.Context, string) ([]float32, error) {
		return queryVector, nil
	}), "campusclaw_chunks", 2048)
	var lectureAgentID int64
	if err := db.QueryRowContext(ctx, `SELECT agent_id FROM course_builtin_agents WHERE class_id=? AND builtin_key='lecture'`, classAID).Scan(&lectureAgentID); err != nil {
		t.Fatal("lecture assistant unavailable for persisted binding acceptance")
	}
	savedConfig, err := services.NewLesson5Store(db).ReadAgentConfig(ctx, classAID, lectureAgentID)
	if err != nil || len(savedConfig.MaterialIDs) != 1 || savedConfig.MaterialIDs[0] != targetMaterialID {
		t.Fatal("lecture assistant must have exactly the source-verified classroom material saved before bound retrieval acceptance")
	}
	boundHits, err := boundSearch.SearchHybridMaterials(ctx, classAID, "授权", savedConfig.MaterialIDs, 4)
	if err != nil {
		t.Fatal("local bound-material hybrid retrieval failed")
	}
	var boundTargetFound bool
	for _, hit := range boundHits {
		if hit.MaterialID != targetMaterialID {
			t.Fatal("hybrid retrieval escaped the assistant material allow-list")
		}
		if hit.Title == targetTitle && hit.Excerpt != "" && hit.SourceURL != nil && *hit.SourceURL == sourceURL && hit.SourceLocator != nil && *hit.SourceLocator == sourceLocator {
			boundTargetFound = true
		}
	}
	if !boundTargetFound {
		t.Fatal("hybrid retrieval with the selected material allow-list did not return the traceable classroom source")
	}
	crossClassHits, err := boundSearch.SearchHybridMaterials(ctx, 2, "授权", savedConfig.MaterialIDs, 4)
	if err != nil || len(crossClassHits) != 0 {
		t.Fatal("hybrid retrieval exposed the A-class material through a B-class session")
	}
	t.Logf("CHECK: A-class material ID %d had one saved text row, %d ready keyword chunks, and %d vector-ready chunks before recovery; assistant %d persisted exactly that binding; authenticated keyword and saved-binding hybrid retrieval returned only its cited source; forged class_id and B-class scope did not expose it", targetMaterialID, readyChunks, vectorReadyChunks, lectureAgentID)
}

func removeAbortedVectorIndexFixtures(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	rows, err := db.QueryContext(ctx, `SELECT m.id,e.id,c.id,m.title,m.original_filename,m.storage_key,m.size_bytes,m.sha256,e.content,c.chunk_text
		FROM materials m JOIN knowledge_entries e ON e.material_id=m.id AND e.class_id=m.class_id
		JOIN knowledge_chunks c ON c.knowledge_entry_id=e.id
		WHERE m.storage_key LIKE 'vector-index-check-%' AND m.class_id=1`)
	if err != nil {
		t.Fatal("could not inspect prior synthetic vector fixture")
	}
	type fixture struct{ materialID, entryID, chunkID int64 }
	fixtures := make([]fixture, 0)
	for rows.Next() {
		var item fixture
		var title, filename, key, digest, content, chunk string
		var size int64
		if rows.Scan(&item.materialID, &item.entryID, &item.chunkID, &title, &filename, &key, &size, &digest, &content, &chunk) != nil {
			rows.Close()
			t.Fatal("could not inspect synthetic vector fixture metadata")
		}
		if title != key || filename != "vector-check.txt" || size != 32 || digest != strings.Repeat("a", 64) || content != "Synthetic vector indexing fixture" || chunk != "synthetic vector indexing fixture" {
			rows.Close()
			t.Fatal("synthetic vector fixture marker did not match; preserved the row for manual review")
		}
		fixtures = append(fixtures, item)
	}
	if rows.Err() != nil || rows.Close() != nil {
		t.Fatal("could not finish inspecting synthetic vector fixture")
	}
	for _, item := range fixtures {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal("synthetic vector fixture cleanup could not start")
		}
		for _, step := range []struct {
			query string
			id    int64
		}{{"DELETE FROM knowledge_vector_state WHERE chunk_id=?", item.chunkID}, {"DELETE FROM knowledge_chunks WHERE id=?", item.chunkID}, {"DELETE FROM knowledge_entries WHERE id=?", item.entryID}, {"DELETE FROM materials WHERE id=?", item.materialID}} {
			if _, err := tx.ExecContext(ctx, step.query, step.id); err != nil {
				_ = tx.Rollback()
				t.Fatal("synthetic vector fixture cleanup failed")
			}
		}
		if tx.Commit() != nil {
			t.Fatal("synthetic vector fixture cleanup commit failed")
		}
	}
}

func TestVectorSearchIntegration(t *testing.T) {
	if os.Getenv("CAMPUSCLAW_VECTOR_SEARCH_INTEGRATION") != "1" || os.Getenv("CAMPUSCLAW_MYSQL_INTEGRATION") != "1" {
		t.Skip("explicit project MySQL and Qdrant integration required")
	}
	c := integrationConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db, err := Open(ctx, c)
	if err != nil {
		t.Fatal("project database unavailable")
	}
	t.Cleanup(func() { _ = db.Close() })
	removeAbortedVectorIndexFixtures(t, ctx, db)
	qdrant, err := services.NewQdrant(os.Getenv("QDRANT_URL"))
	if err != nil {
		t.Fatal("local Qdrant endpoint unavailable")
	}
	collection := "campusclaw_test_vector_" + strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
	if err := qdrant.EnsureCollection(ctx, collection, 4); err != nil {
		t.Fatal("isolated Qdrant collection creation failed")
	}
	t.Cleanup(func() {
		if err := qdrant.DeleteCollection(context.Background(), collection); err != nil {
			t.Errorf("isolated Qdrant collection cleanup failed: %v", err)
		}
	})
	tag := "vector-index-check-" + strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
	var teacherID int64
	if err := db.QueryRowContext(ctx, "SELECT id FROM users WHERE class_id=1 AND role='teacher' ORDER BY id LIMIT 1").Scan(&teacherID); err != nil || teacherID < 1 {
		t.Fatal("class A teacher unavailable for isolated vector check")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal("synthetic vector fixture transaction unavailable")
	}
	materialResult, err := tx.ExecContext(ctx, `INSERT INTO materials(class_id,uploaded_by,title,original_filename,storage_key,size_bytes,sha256)
		VALUES(1,?,?,?,?,?,?)`, teacherID, tag, "vector-check.txt", tag, 32, strings.Repeat("a", 64))
	if err != nil {
		_ = tx.Rollback()
		t.Fatal("synthetic vector material insert failed")
	}
	materialID, err := materialResult.LastInsertId()
	if err != nil {
		_ = tx.Rollback()
		t.Fatal("synthetic vector material ID unavailable")
	}
	entryResult, err := tx.ExecContext(ctx, "INSERT INTO knowledge_entries(material_id,class_id,content) VALUES(?,1,?)", materialID, "Synthetic vector indexing fixture")
	if err != nil {
		_ = tx.Rollback()
		t.Fatal("synthetic vector entry insert failed")
	}
	entryID, err := entryResult.LastInsertId()
	if err != nil {
		_ = tx.Rollback()
		t.Fatal("synthetic vector entry ID unavailable")
	}
	chunkResult, err := tx.ExecContext(ctx, `INSERT INTO knowledge_chunks(knowledge_entry_id,chunk_index,chunk_text,char_start,char_end,index_status)
		VALUES(?,0,'synthetic vector indexing fixture',0,33,'ready')`, entryID)
	if err != nil {
		_ = tx.Rollback()
		t.Fatal("synthetic vector chunk insert failed")
	}
	chunkID, err := chunkResult.LastInsertId()
	if err != nil || tx.Commit() != nil {
		t.Fatal("synthetic vector fixture commit failed")
	}
	t.Cleanup(func() {
		cleanCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		clean, e := db.BeginTx(cleanCtx, nil)
		if e != nil {
			t.Error("synthetic vector fixture cleanup could not start")
			return
		}
		defer clean.Rollback()
		for _, statement := range []struct {
			query string
			id    int64
		}{{"DELETE FROM knowledge_vector_state WHERE chunk_id=?", chunkID}, {"DELETE FROM knowledge_chunks WHERE id=?", chunkID}, {"DELETE FROM knowledge_entries WHERE id=?", entryID}, {"DELETE FROM materials WHERE id=?", materialID}} {
			if _, e = clean.ExecContext(cleanCtx, statement.query, statement.id); e != nil {
				t.Error("synthetic vector fixture cleanup failed")
				return
			}
		}
		if clean.Commit() != nil {
			t.Error("synthetic vector fixture cleanup commit failed")
		}
	})
	vector := []float32{1, 0, 0, 0}
	service := func() *services.Knowledge {
		return services.NewKnowledgeWithVector(db, qdrant, services.QueryEmbedderFunc(func(context.Context, string) ([]float32, error) { return vector, nil }), collection, 4)
	}
	indexed, err := service().IndexMaterialVectors(ctx, 1, materialID)
	if err != nil || indexed.Total != 1 || indexed.Ready != 1 || indexed.Pending != 0 || indexed.Processed != 1 {
		t.Fatal("explicit vector indexing failed for the synthetic class A chunk")
	}
	unchanged, err := service().IndexMaterialVectors(ctx, 1, materialID)
	if err != nil || unchanged.Processed != 0 || unchanged.Ready != 1 {
		t.Fatal("unchanged chunk was sent for embedding again")
	}
	// Deliberately mislabel an A-class chunk as B in Qdrant. The MySQL class
	// recheck must discard it even though the vector-store filter matches B.
	if err := qdrant.Upsert(ctx, collection, []services.VectorPoint{{ID: uint64(chunkID), ChunkID: chunkID, ClassID: 2, MaterialID: materialID, EntryID: entryID, Vector: vector}}, 4); err != nil {
		t.Fatal("synthetic Qdrant point setup failed")
	}
	if hits, err := service().SearchVector(ctx, 2, "synthetic query", 10, 0); err != nil || len(hits) != 0 {
		t.Fatal("MySQL class recheck failed to suppress a wrongly labeled cross-class vector")
	}
	if err := qdrant.Upsert(ctx, collection, []services.VectorPoint{{ID: uint64(chunkID), ChunkID: chunkID, ClassID: 1, MaterialID: materialID, EntryID: entryID, Vector: vector}}, 4); err != nil {
		t.Fatal("synthetic Qdrant point update failed")
	}
	hits, err := service().SearchVector(ctx, 1, "synthetic query", 10, 0)
	if err != nil || len(hits) != 1 || hits[0].ChunkID != chunkID || hits[0].Excerpt == "" || hits[0].Score < 0.99 {
		t.Fatal("class A vector retrieval did not return a traceable MySQL source")
	}
	t.Log("CHECK: synthetic embedding searched local Qdrant; MySQL rechecked class scope and returned only traceable source text; temporary collection cleanup is checked at test end")
}
