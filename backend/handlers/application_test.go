package handlers

import (
	"bytes"
	"campusclaw/backend/repositories"
	"campusclaw/backend/services"
	"context"
	"golang.org/x/crypto/bcrypt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type applicationSessionStore struct {
	user repositories.User
	hash string
}

func (s *applicationSessionStore) Credentials(context.Context, string) (repositories.User, string, error) {
	return s.user, s.hash, nil
}
func (*applicationSessionStore) Rotate(context.Context, repositories.User, string, string, string, time.Time) error {
	return nil
}
func (s *applicationSessionStore) Resolve(context.Context, string, time.Time) (repositories.User, error) {
	return s.user, nil
}
func (*applicationSessionStore) Revoke(context.Context, string, time.Time) (bool, error) {
	return true, nil
}

type applicationMaterialsStub struct {
	ready         bool
	allowDetail   bool
	detailClassID int64
	detailID      int64
	listCalls     int
	detailCalls   int
	downloadCalls int
	uploadCalls   int
}

func (s *applicationMaterialsStub) Ready() bool                   { return s.ready }
func (s *applicationMaterialsStub) Recover(context.Context) error { s.ready = true; return nil }
func (s *applicationMaterialsStub) List(context.Context, int64, int, int) (services.MaterialPage, error) {
	s.listCalls++
	return services.MaterialPage{Items: []services.Material{}}, nil
}
func (s *applicationMaterialsStub) Detail(_ context.Context, classID, id int64) (services.MaterialDetail, error) {
	s.detailCalls++
	if s.allowDetail && (s.detailClassID == 0 || s.detailClassID == classID) && (s.detailID == 0 || s.detailID == id) {
		return services.MaterialDetail{Material: services.Material{ID: id}}, nil
	}
	return services.MaterialDetail{}, services.ErrMaterialNotFound
}
func (s *applicationMaterialsStub) OpenDownload(context.Context, int64, int64) (services.Download, error) {
	s.downloadCalls++
	return services.Download{}, services.ErrMaterialNotFound
}
func (s *applicationMaterialsStub) Upload(context.Context, repositories.User, string, string, string, string, []byte) (int64, error) {
	s.uploadCalls++
	return 0, services.ErrUploadUnknown
}

func TestMaterialReadinessPrecedesAuthenticationAndRecovers(t *testing.T) {
	user := repositories.User{ID: 7, Username: "teacher_test", Role: "teacher", ClassID: 1}
	hash, err := bcrypt.GenerateFromPassword([]byte("canary"), bcrypt.MinCost)
	if err != nil {
		t.Fatal("test password hash unavailable")
	}
	store := &applicationSessionStore{user: user, hash: string(hash)}
	auth, err := services.NewAuth(store, "test-only-secret", func() time.Time { return time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal("test auth unavailable")
	}
	_, token, _, err := auth.Login(context.Background(), user.Username, "canary", "")
	if err != nil {
		t.Fatal("test session unavailable")
	}
	student := repositories.User{ID: 8, Username: "student_test", Role: "student", ClassID: 1}
	studentStore := &applicationSessionStore{user: student, hash: string(hash)}
	studentAuth, err := services.NewAuth(studentStore, "test-only-secret", func() time.Time { return time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal("student test auth unavailable")
	}
	_, studentToken, _, err := studentAuth.Login(context.Background(), student.Username, "canary", "")
	if err != nil {
		t.Fatal("student test session unavailable")
	}
	materials := &applicationMaterialsStub{}
	handler := NewApplication(auth, materials, "http://localhost:8080")
	paths := []struct{ method, path, body string }{
		{http.MethodGet, "/api/materials", ""},
		{http.MethodGet, "/api/materials/42", ""},
		{http.MethodGet, "/api/materials/42/download", ""},
		{http.MethodPost, "/api/materials", "not-a-multipart-body"},
	}
	cookies := []string{"", "campusclaw_session=bad", "campusclaw_session=" + token, "campusclaw_session=" + studentToken}
	for _, path := range paths {
		for _, cookie := range cookies {
			r := httptest.NewRequest(path.method, path.path, strings.NewReader(path.body))
			if cookie != "" {
				r.Header.Set("Cookie", cookie)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), `"code":"SERVICE_UNAVAILABLE"`) || strings.Contains(w.Body.String(), "teacher_test") {
				t.Fatalf("not-ready request leaked past the readiness gate: %s %s => %d %s", path.method, path.path, w.Code, w.Body.String())
			}
		}
	}
	if materials.listCalls+materials.detailCalls+materials.downloadCalls+materials.uploadCalls != 0 {
		t.Fatal("material operation ran before recovery completed")
	}

	// Health performs recovery; after it succeeds the ordinary authentication and
	// authorization rules resume instead of treating the prior 503 as a login.
	r := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !materials.ready {
		t.Fatal("successful recovery did not restore health")
	}
	r = httptest.NewRequest(http.MethodGet, "/api/materials", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatal("unavailable period did not resume unauthenticated 401 after recovery")
	}
	if _, err := auth.Me(context.Background(), token); err != nil {
		t.Fatal("valid test session became invalid")
	}
	r = httptest.NewRequest(http.MethodGet, "/api/materials", nil)
	r.AddCookie(&http.Cookie{Name: "campusclaw_session", Value: token})
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK || materials.listCalls != 1 {
		t.Fatal("valid class list did not resume after recovery")
	}
	var oversized bytes.Buffer
	form := multipart.NewWriter(&oversized)
	filePart, err := form.CreateFormFile("file", "oversized.txt")
	if err != nil {
		t.Fatal("oversized form unavailable")
	}
	_, _ = filePart.Write(bytes.Repeat([]byte{'x'}, 6*1024*1024))
	_ = form.WriteField("title", "oversized")
	_ = form.Close()
	r = httptest.NewRequest(http.MethodPost, "/api/materials", &oversized)
	r.Header.Set("Origin", "http://localhost:8080")
	r.Header.Set("Content-Type", form.FormDataContentType())
	r.AddCookie(&http.Cookie{Name: "campusclaw_session", Value: token})
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusRequestEntityTooLarge || materials.uploadCalls != 0 {
		t.Fatalf("Go request limit did not reject oversized body: status=%d calls=%d", w.Code, materials.uploadCalls)
	}
	var unknown bytes.Buffer
	unknownForm := multipart.NewWriter(&unknown)
	unknownFile, err := unknownForm.CreateFormFile("file", "unknown.md")
	if err != nil {
		t.Fatal("unknown-result form unavailable")
	}
	_, _ = unknownFile.Write([]byte("valid text"))
	_ = unknownForm.WriteField("title", "unknown result")
	_ = unknownForm.Close()
	r = httptest.NewRequest(http.MethodPost, "/api/materials", &unknown)
	r.Header.Set("Origin", "http://localhost:8080")
	r.Header.Set("Content-Type", unknownForm.FormDataContentType())
	r.AddCookie(&http.Cookie{Name: "campusclaw_session", Value: token})
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), `"code":"UPLOAD_RESULT_UNKNOWN"`) || !strings.Contains(w.Body.String(), "勿重复上传") {
		t.Fatal("unknown upload result did not preserve the safe retry guidance")
	}

	r = httptest.NewRequest(http.MethodPost, "/api/materials", strings.NewReader(""))
	r.Header.Set("Origin", "http://localhost:8080")
	r.Header.Set("Content-Type", "multipart/form-data; boundary=test")
	r.AddCookie(&http.Cookie{Name: "campusclaw_session", Value: studentToken})
	w = httptest.NewRecorder()
	NewApplication(studentAuth, materials, "http://localhost:8080").ServeHTTP(w, r)
	if w.Code != http.StatusForbidden || materials.uploadCalls != 1 {
		t.Fatalf("student role was not rejected before upload writes: status=%d body=%s calls=%d", w.Code, w.Body.String(), materials.uploadCalls)
	}
}
