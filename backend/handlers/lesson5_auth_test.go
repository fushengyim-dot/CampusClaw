package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"campusclaw/backend/repositories"
	"campusclaw/backend/services"
	"golang.org/x/crypto/bcrypt"
)

func lesson5TestAuth(t *testing.T, user repositories.User) (*services.Auth, string) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("local-test-only"), bcrypt.MinCost)
	if err != nil {
		t.Fatal("test password hash unavailable")
	}
	auth, err := services.NewAuth(&applicationSessionStore{user: user, hash: string(hash)}, "lesson5-test-secret", func() time.Time {
		return time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal("test auth unavailable")
	}
	_, token, _, err := auth.Login(context.Background(), user.Username, "local-test-only", "")
	if err != nil {
		t.Fatal("test session unavailable")
	}
	return auth, token
}

func TestLesson5RoleAndOriginGatesPrecedeWrites(t *testing.T) {
	student := repositories.User{ID: 8, Username: "test-student", Role: "student", ClassID: 2}
	auth, studentToken := lesson5TestAuth(t, student)
	for _, route := range []struct{ path, body string }{
		{"/api/agents", `{"name":"nope","prompt":"nope","enabled":true}`},
		{"/api/skills", `{"source_text":"名称：拒绝测试\n适用条件：x\n启用：是\n\n正文"}`},
	} {
		r := httptest.NewRequest(http.MethodPost, route.path, strings.NewReader(route.body))
		r.AddCookie(&http.Cookie{Name: "campusclaw_session", Value: studentToken})
		r.Header.Set("Origin", "http://localhost:8080")
		w := httptest.NewRecorder()
		handleLesson5(w, r, auth, "http://localhost:8080", nil, nil, nil)
		if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), `"code":"FORBIDDEN"`) {
			t.Fatalf("student write gate failed for %s: %d %s", route.path, w.Code, w.Body.String())
		}
	}
	teacher := repositories.User{ID: 7, Username: "test-teacher", Role: "teacher", ClassID: 2}
	teacherAuth, teacherToken := lesson5TestAuth(t, teacher)
	r := httptest.NewRequest(http.MethodPost, "/api/agents", strings.NewReader(`{"name":"nope","prompt":"nope","enabled":true}`))
	r.AddCookie(&http.Cookie{Name: "campusclaw_session", Value: teacherToken})
	r.Header.Set("Origin", "http://wrong.invalid")
	w := httptest.NewRecorder()
	handleLesson5(w, r, teacherAuth, "http://localhost:8080", nil, nil, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("foreign-origin write was not refused before reaching storage: %d %s", w.Code, w.Body.String())
	}
}
