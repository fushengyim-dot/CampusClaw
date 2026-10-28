package handlers

import (
	"campusclaw/backend/services"
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

type checkerStub struct {
	calls int
	err   error
}

func (s *checkerStub) Check(context.Context, string, string) error { s.calls++; return s.err }

func TestLoginInputAndSafeResponses(t *testing.T) {
	valid := `{"username":"example","password":"test-canary"}`
	for _, tc := range []struct {
		name, method, origin, body string
		want                       int
		err                        error
	}{
		{"valid", "POST", "http://localhost:5173", valid, 200, nil},
		{"wrong", "POST", "http://localhost:5173", valid, 401, services.ErrCredentials},
		{"unavailable", "POST", "http://localhost:5173", valid, 503, errors.New("private-sql-canary")},
		{"method", "GET", "http://localhost:5173", valid, 405, nil},
		{"origin missing", "POST", "", valid, 403, nil},
		{"origin wrong", "POST", "http://evil.invalid", valid, 403, nil},
		{"origin suffix", "POST", "http://localhost:5173/", valid, 403, nil},
		{"bad json", "POST", "http://localhost:5173", "{", 400, nil},
		{"unknown field", "POST", "http://localhost:5173", `{"username":"x","password":"y","role":"teacher"}`, 400, nil},
		{"trailing json", "POST", "http://localhost:5173", valid + "{}", 400, nil},
		{"empty", "POST", "http://localhost:5173", `{}`, 400, nil},
		{"null", "POST", "http://localhost:5173", `null`, 400, nil},
		{"overlong password", "POST", "http://localhost:5173", `{"username":"x","password":"` + strings.Repeat("界", 25) + `"}`, 400, nil},
		{"oversized", "POST", "http://localhost:5173", strings.Repeat(" ", 4097), 413, nil},
		{"bad utf8", "POST", "http://localhost:5173", string([]byte{0xff}), 400, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &checkerStub{err: tc.err}
			r := httptest.NewRequest(tc.method, "/api/login", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			w := httptest.NewRecorder()
			NewWithLogin(stub, "http://localhost:5173").ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status=%d want=%d", w.Code, tc.want)
			}
			if w.Header().Get("Set-Cookie") != "" || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "canary") {
				t.Fatal("credential leak, cookie, or caching")
			}
			if tc.want != 200 && tc.want != 401 && tc.want != 503 && stub.calls != 0 {
				t.Fatal("invalid request reached password checker")
			}
			if tc.want == 200 && !strings.Contains(w.Body.String(), `"session_created":false`) {
				t.Fatal("success implied session")
			}
		})
	}
	for _, mode := range []string{"content-type", "multiple-origin"} {
		stub := &checkerStub{}
		r := httptest.NewRequest("POST", "/api/login", strings.NewReader(valid))
		r.Header.Set("Origin", "http://localhost:5173")
		want := 400
		if mode == "multiple-origin" {
			r.Header.Set("Content-Type", "application/json")
			r.Header.Add("Origin", "http://localhost:5173")
			want = 403
		}
		w := httptest.NewRecorder()
		NewWithLogin(stub, "http://localhost:5173").ServeHTTP(w, r)
		if w.Code != want || stub.calls != 0 {
			t.Fatal("header boundary failed")
		}
	}
}
