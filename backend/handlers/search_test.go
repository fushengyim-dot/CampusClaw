package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"campusclaw/backend/repositories"
	"campusclaw/backend/services"
	"golang.org/x/crypto/bcrypt"
)

type searchKnowledgeStub struct {
	classID int64
	query   string
	calls   int
}

type vectorSearchStub struct {
	classID       int64
	mode          string
	failure       error
	indexCalls    int
	statusCalls   int
	materialID    int64
	indexProgress services.VectorIndexProgress
}

func (s *vectorSearchStub) SearchKeyword(context.Context, int64, string, int, int) ([]services.KnowledgeHit, error) {
	return nil, nil
}
func (s *vectorSearchStub) SearchVector(_ context.Context, classID int64, _ string, _, _ int) ([]services.KnowledgeHit, error) {
	s.classID, s.mode = classID, "vector"
	if s.failure != nil {
		return nil, s.failure
	}
	return []services.KnowledgeHit{{ChunkID: 3, Excerpt: "synthetic"}}, nil
}
func (s *vectorSearchStub) SearchHybrid(_ context.Context, classID int64, _ string, _, _ int) ([]services.KnowledgeHit, error) {
	s.classID, s.mode = classID, "hybrid"
	if s.failure != nil {
		return nil, s.failure
	}
	return []services.KnowledgeHit{{ChunkID: 4, Excerpt: "synthetic"}}, nil
}

func (s *vectorSearchStub) VectorIndexStatus(_ context.Context, classID, materialID int64) (services.VectorIndexProgress, error) {
	s.statusCalls++
	s.classID, s.materialID = classID, materialID
	return services.VectorIndexProgress{Total: 3, Ready: 1, Pending: 2}, nil
}

func (s *vectorSearchStub) IndexMaterialVectors(_ context.Context, classID, materialID int64) (services.VectorIndexProgress, error) {
	s.indexCalls++
	s.classID, s.materialID = classID, materialID
	return services.VectorIndexProgress{Total: 3, Ready: 2, Pending: 1, Processed: 1}, s.failure
}

func (s *searchKnowledgeStub) SearchKeyword(_ context.Context, classID int64, query string, _, _ int) ([]services.KnowledgeHit, error) {
	s.classID, s.query, s.calls = classID, query, s.calls+1
	return []services.KnowledgeHit{{MaterialID: 12, Title: "本班讲义", Excerpt: "授权流程"}}, nil
}

func TestVectorAndHybridSearchUseAuthenticatedClass(t *testing.T) {
	user := repositories.User{ID: 9, Username: "vector_student", Role: "student", ClassID: 72}
	hash, _ := bcrypt.GenerateFromPassword([]byte("test-password"), bcrypt.MinCost)
	store := &applicationSessionStore{user: user, hash: string(hash)}
	auth, err := services.NewAuth(store, "test-only-secret", func() time.Time { return time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal("test auth unavailable")
	}
	_, token, _, err := auth.Login(context.Background(), user.Username, "test-password", "")
	if err != nil {
		t.Fatal("test session unavailable")
	}
	materials := &applicationMaterialsStub{ready: true}
	knowledge := &vectorSearchStub{}
	handler := NewApplication(auth, materials, "http://localhost:8080", knowledge)
	for _, mode := range []string{"vector", "hybrid"} {
		r := httptest.NewRequest(http.MethodPost, "/api/search", strings.NewReader(`{"q":"检索问题","mode":"`+mode+`","class_id":999}`))
		r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK || knowledge.classID != user.ClassID || knowledge.mode != mode || !strings.Contains(w.Body.String(), `"mode":"`+mode+`"`) {
			t.Fatalf("%s search did not preserve session class scope: status=%d body=%s", mode, w.Code, w.Body.String())
		}
	}
	knowledge.failure = errors.New("local vector service unavailable")
	r := httptest.NewRequest(http.MethodPost, "/api/search", strings.NewReader(`{"q":"检索问题","mode":"vector"}`))
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("vector-store failure must return 503")
	}
}

func TestKeywordSearchUsesAuthenticatedClassAndRejectsUnsupportedModes(t *testing.T) {
	user := repositories.User{ID: 7, Username: "student_test", Role: "student", ClassID: 41}
	hash, err := bcrypt.GenerateFromPassword([]byte("test-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal("test password hash unavailable")
	}
	store := &applicationSessionStore{user: user, hash: string(hash)}
	auth, err := services.NewAuth(store, "test-only-secret", func() time.Time { return time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal("test auth unavailable")
	}
	_, token, _, err := auth.Login(context.Background(), user.Username, "test-password", "")
	if err != nil {
		t.Fatal("test session unavailable")
	}
	materials := &applicationMaterialsStub{ready: true}
	knowledge := &searchKnowledgeStub{}
	handler := NewApplication(auth, materials, "http://localhost:8080", knowledge)
	request := func(body string, cookie bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/search", strings.NewReader(body))
		if cookie {
			r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := request(`{"q":"授权","mode":"keyword","class_id":999}`, true); w.Code != http.StatusOK || knowledge.calls != 1 || knowledge.classID != 41 || knowledge.query != "授权" || !strings.Contains(w.Body.String(), "本班讲义") {
		t.Fatal("keyword search did not use the authenticated class or return traceable hits")
	}
	if w := request(`{"q":"授权","mode":"hybrid"}`, true); w.Code != http.StatusServiceUnavailable || knowledge.calls != 1 {
		t.Fatal("unconfigured hybrid search did not fail closed")
	}
	if w := request(`{"q":"授权","mode":"keyword"}`, false); w.Code != http.StatusUnauthorized || knowledge.calls != 1 {
		t.Fatal("unauthenticated keyword search reached the retrieval service")
	}
	if w := request(`{"q":"   ","mode":"keyword"}`, true); w.Code != http.StatusBadRequest || knowledge.calls != 1 {
		t.Fatal("empty keyword query was accepted")
	}
}

func TestVectorIndexRequiresSameClassTeacherAndExpectedOrigin(t *testing.T) {
	makeHandler := func(user repositories.User, materials *applicationMaterialsStub, indexer *vectorSearchStub) (http.Handler, string) {
		hash, err := bcrypt.GenerateFromPassword([]byte("test-password"), bcrypt.MinCost)
		if err != nil {
			t.Fatal("test password hash unavailable")
		}
		store := &applicationSessionStore{user: user, hash: string(hash)}
		auth, err := services.NewAuth(store, "test-only-secret", func() time.Time { return time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC) })
		if err != nil {
			t.Fatal("test auth unavailable")
		}
		_, token, _, err := auth.Login(context.Background(), user.Username, "test-password", "")
		if err != nil {
			t.Fatal("test session unavailable")
		}
		return NewApplication(auth, materials, "http://localhost:8080", indexer), token
	}
	call := func(handler http.Handler, token, method, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/materials/42/vector-index", nil)
		r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}

	teacher := repositories.User{ID: 1, Username: "teacher_index", Role: "teacher", ClassID: 41}
	teacherMaterials := &applicationMaterialsStub{ready: true, allowDetail: true, detailClassID: 41, detailID: 42}
	teacherIndex := &vectorSearchStub{indexProgress: services.VectorIndexProgress{Total: 3, Ready: 2, Pending: 1, Processed: 1}}
	teacherHandler, teacherToken := makeHandler(teacher, teacherMaterials, teacherIndex)
	if w := call(teacherHandler, teacherToken, http.MethodPost, "http://evil.example"); w.Code != http.StatusForbidden || teacherIndex.indexCalls != 0 {
		t.Fatal("vector indexing accepted a foreign origin")
	}
	if w := call(teacherHandler, teacherToken, http.MethodPost, "http://localhost:8080"); w.Code != http.StatusOK || teacherIndex.indexCalls != 1 || teacherIndex.classID != 41 || teacherIndex.materialID != 42 || !strings.Contains(w.Body.String(), `"processed":1`) {
		t.Fatal("same-class teacher could not index their material with session scope")
	}
	if w := call(teacherHandler, teacherToken, http.MethodGet, ""); w.Code != http.StatusOK || teacherIndex.statusCalls != 1 || teacherIndex.classID != 41 || teacherIndex.materialID != 42 || !strings.Contains(w.Body.String(), `"ready":1`) {
		t.Fatal("vector index status did not use authenticated class and material")
	}

	student := repositories.User{ID: 2, Username: "student_index", Role: "student", ClassID: 41}
	studentMaterials := &applicationMaterialsStub{ready: true, allowDetail: true, detailClassID: 41, detailID: 42}
	studentIndex := &vectorSearchStub{}
	studentHandler, studentToken := makeHandler(student, studentMaterials, studentIndex)
	if w := call(studentHandler, studentToken, http.MethodPost, "http://localhost:8080"); w.Code != http.StatusForbidden || studentIndex.indexCalls != 0 {
		t.Fatal("student was allowed to send lecture chunks for vectorization")
	}

	otherClassMaterials := &applicationMaterialsStub{ready: true, allowDetail: true, detailClassID: 99, detailID: 42}
	otherClassIndex := &vectorSearchStub{}
	otherClassHandler, otherClassToken := makeHandler(teacher, otherClassMaterials, otherClassIndex)
	if w := call(otherClassHandler, otherClassToken, http.MethodPost, "http://localhost:8080"); w.Code != http.StatusNotFound || otherClassIndex.indexCalls != 0 {
		t.Fatal("cross-class teacher learned or indexed another class material")
	}
}

var _ KnowledgeService = (*searchKnowledgeStub)(nil)
