package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"campusclaw/backend/repositories"
)

func TestMCPManagementRejectsStudentsAndWrongOriginBeforeStorage(t *testing.T) {
	student := repositories.User{ID: 18, Username: "synthetic-student", Role: "student", ClassID: 2}
	studentAuth, studentToken := lesson5TestAuth(t, student)
	r := httptest.NewRequest(http.MethodGet, "/api/mcp/servers", nil)
	r.AddCookie(&http.Cookie{Name: "campusclaw_session", Value: studentToken})
	w := httptest.NewRecorder()
	handleMCPServers(w, r, studentAuth, "http://localhost:8080", nil)
	if w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), "token") {
		t.Fatal("student could inspect class MCP servers")
	}
	teacher := repositories.User{ID: 19, Username: "synthetic-teacher", Role: "teacher", ClassID: 2}
	teacherAuth, teacherToken := lesson5TestAuth(t, teacher)
	r = httptest.NewRequest(http.MethodPost, "/api/mcp/servers", strings.NewReader(`{"name":"synthetic","endpoint":"http://mcp-tools:8091/mcp","token":"synthetic-secret-value-long-enough-123456"}`))
	r.AddCookie(&http.Cookie{Name: "campusclaw_session", Value: teacherToken})
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://wrong.invalid")
	w = httptest.NewRecorder()
	handleMCPServers(w, r, teacherAuth, "http://localhost:8080", nil)
	if w.Code != http.StatusForbidden || strings.Contains(w.Body.String(), "synthetic-secret-value") {
		t.Fatal("wrong-origin server write reached storage or echoed token")
	}
}
