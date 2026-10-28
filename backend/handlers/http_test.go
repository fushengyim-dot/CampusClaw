package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNotReadyDoesNotExposeRequestData(t *testing.T) {
	for _, path := range []string{"/health", "/api/login", "/api/me", "/api/materials", "/api/materials/1/download"} {
		for _, method := range []string{"GET", "POST"} {
			t.Run(method+path, func(t *testing.T) {
				r := httptest.NewRequest(method, path+"?password=request-canary", strings.NewReader("request-canary"))
				r.Header.Set("Cookie", "campusclaw_session=request-canary")
				w := httptest.NewRecorder()
				New().ServeHTTP(w, r)
				if w.Code != 503 {
					t.Fatalf("status=%d", w.Code)
				}
				var body struct {
					Error struct{ Code, Message string }
				}
				if json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Error.Code != "SERVICE_UNAVAILABLE" || body.Error.Message == "" {
					t.Fatal("wrong error envelope")
				}
				if strings.Contains(w.Body.String(), "request-canary") || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("response leaks or permits caching")
				}
			})
		}
	}
}

func TestUnknownRouteUsesSafeJSON(t *testing.T) {
	w := httptest.NewRecorder()
	New().ServeHTTP(w, httptest.NewRequest("GET", "/private-canary", nil))
	if w.Code != 404 || !json.Valid(w.Body.Bytes()) || strings.Contains(w.Body.String(), "private-canary") {
		t.Fatal("unsafe unknown-route response")
	}
}
