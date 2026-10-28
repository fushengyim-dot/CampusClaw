package database

import (
	"bytes"
	"campusclaw/backend/handlers"
	"campusclaw/backend/repositories"
	"campusclaw/backend/services"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoginIntegration(t *testing.T) {
	if os.Getenv("CAMPUSCLAW_LOGIN_INTEGRATION") != "1" {
		t.Skip("explicit session test required")
	}
	c := integrationConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db, err := Open(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tables := append(append([]string{}, schemaTables...), "schema_migrations", "connection_check_probe")
	baseline := schemaSnapshot(t, ctx, db, tables)
	tokens := []string{}
	digest := func(token string) string {
		mac := hmac.New(sha256.New, []byte(c.SessionSecret))
		mac.Write([]byte(token))
		return hex.EncodeToString(mac.Sum(nil))
	}
	defer func() {
		for _, token := range tokens {
			if _, e := db.ExecContext(context.Background(), "DELETE FROM sessions WHERE token_hash=?", digest(token)); e != nil {
				t.Error("own test session cleanup failed")
			}
		}
		if !reflect.DeepEqual(baseline, schemaSnapshot(t, ctx, db, tables)) {
			t.Error("existing rows changed after session cleanup")
		}
	}()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("test port unavailable")
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	uploadRoot, err := os.MkdirTemp("", "campusclaw-login-integration-")
	if err != nil {
		t.Fatal("isolated integration upload directory unavailable")
	}
	defer os.RemoveAll(uploadRoot)
	cmd := exec.CommandContext(ctx, "/check/bin/campusclaw-linux")
	cmd.Env = append(os.Environ(), fmt.Sprintf("API_PORT=%d", port), "UPLOAD_ROOT="+uploadRoot)
	var logs bytes.Buffer
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	if cmd.Start() != nil {
		t.Fatal("backend start failed")
	}
	defer func() {
		if cmd.ProcessState == nil {
			cmd.Process.Kill()
			cmd.Wait()
		}
	}()
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 5 * time.Second}
	ready := false
	for i := 0; i < 50; i++ {
		r, e := client.Get(base + "/health")
		if e == nil {
			r.Body.Close()
			ready = r.StatusCode == 503
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		diagnostic := logs.String()
		for _, key := range []string{"MYSQL_PASSWORD", "SESSION_SECRET", "SEED_TEACHER_A_PASSWORD", "SEED_STUDENT_A_PASSWORD", "SEED_TEACHER_B_PASSWORD", "SEED_STUDENT_B_PASSWORD"} {
			if secret := os.Getenv(key); secret != "" {
				diagnostic = strings.ReplaceAll(diagnostic, secret, "***")
			}
		}
		t.Fatalf("backend did not enter the expected not-ready state; startup output=%q", diagnostic)
	}
	type reply struct {
		status int
		body   string
		cookie *http.Cookie
	}
	request := func(base, method, path, origin, token, username, password string) reply {
		t.Helper()
		var payload []byte
		if path == "/api/login" {
			payload, _ = json.Marshal(map[string]string{"username": username, "password": password})
		}
		r, _ := http.NewRequest(method, base+path, bytes.NewReader(payload))
		r.Header.Set("Content-Type", "application/json")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if token != "" {
			r.AddCookie(&http.Cookie{Name: "campusclaw_session", Value: token})
		}
		response, e := client.Do(r)
		if e != nil {
			t.Fatal("request failed")
		}
		defer response.Body.Close()
		body, e := io.ReadAll(response.Body)
		if e != nil {
			t.Fatal("response read failed")
		}
		result := reply{status: response.StatusCode, body: string(body)}
		for _, cookie := range response.Cookies() {
			if cookie.Name == "campusclaw_session" {
				result.cookie = cookie
				if cookie.Value != "" {
					tokens = append(tokens, cookie.Value)
				}
			}
		}
		if response.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("response permits caching")
		}
		if password != "" && strings.Contains(result.body, password) {
			t.Fatal("password in response")
		}
		if token != "" && strings.Contains(result.body, token) {
			t.Fatal("token in JSON")
		}
		return result
	}
	me := func(base, token string, want int) {
		t.Helper()
		if request(base, "GET", "/api/me", "", token, "", "").status != want {
			t.Fatal("me status mismatch")
		}
	}
	me(base, "", 401)
	me(base, "teacher:1", 401)
	me(base, strings.Repeat("a", 64), 401)
	for _, a := range demoAccounts {
		password := os.Getenv(a.variable)
		if password == "" {
			t.Fatal("private credential missing")
		}
		first := request(base, "POST", "/api/login", c.AppOrigin, "", a.username, password)
		if first.status != 200 || first.cookie == nil {
			t.Fatal("login did not issue cookie")
		}
		cookie := first.cookie
		if len(cookie.Value) != 64 || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.Domain != "" || cookie.Secure || cookie.MaxAge != 28800 {
			t.Fatal("cookie attributes incorrect")
		}
		var user repositories.User
		if json.Unmarshal([]byte(first.body), &user) != nil || user.Username != a.username || user.Role != a.role || user.ClassID != a.classID {
			t.Fatal("login identity mismatch")
		}
		current := request(base, "GET", "/api/me", "", cookie.Value, "", "")
		var identity repositories.User
		if current.status != 200 || current.cookie != nil || json.Unmarshal([]byte(current.body), &identity) != nil || identity != user {
			t.Fatal("me identity or fixed lifetime mismatch")
		}
		var stored string
		var expires time.Time
		if db.QueryRowContext(ctx, "SELECT token_hash,expires_at FROM sessions WHERE token_hash=?", digest(cookie.Value)).Scan(&stored, &expires) != nil || stored == cookie.Value || !expires.Equal(cookie.Expires) {
			t.Fatal("digest storage or expiry mismatch")
		}
		failed := request(base, "POST", "/api/login", c.AppOrigin, cookie.Value, a.username, "wrong-input")
		if failed.status != 401 || failed.cookie != nil {
			t.Fatal("failed login issued credential")
		}
		me(base, cookie.Value, 200)
		if request(base, "POST", "/api/logout", "http://wrong.invalid", cookie.Value, "", "").status != 403 {
			t.Fatal("logout origin bypass")
		}
		me(base, cookie.Value, 200)
		// Force a unique-key insert failure after the old row delete: transaction must restore it.
		repo := repositories.NewSessions(db)
		_, hash, e := repo.Credentials(ctx, a.username)
		if e != nil {
			t.Fatal("fixture lookup failed")
		}
		another := request(base, "POST", "/api/login", c.AppOrigin, "", a.username, password)
		if another.status != 200 || another.cookie == nil {
			t.Fatal("second device session failed")
		}
		if repo.Rotate(ctx, user, hash, digest(cookie.Value), digest(another.cookie.Value), expires) == nil {
			t.Fatal("duplicate session insertion unexpectedly succeeded")
		}
		me(base, cookie.Value, 200)
		rotated := request(base, "POST", "/api/login", c.AppOrigin, cookie.Value, a.username, password)
		if rotated.status != 200 || rotated.cookie == nil || rotated.cookie.Value == cookie.Value {
			t.Fatal("session not rotated")
		}
		me(base, cookie.Value, 401)
		me(base, rotated.cookie.Value, 200)
		me(base, another.cookie.Value, 200)
		for _, token := range []string{rotated.cookie.Value, another.cookie.Value} {
			out := request(base, "POST", "/api/logout", c.AppOrigin, token, "", "")
			if out.status != 204 || out.cookie == nil || out.cookie.Value != "" || out.cookie.MaxAge != -1 || !out.cookie.HttpOnly {
				t.Fatal("logout did not clear cookie")
			}
			me(base, token, 401)
			if request(base, "POST", "/api/logout", c.AppOrigin, token, "", "").status != 401 {
				t.Fatal("logout replay accepted")
			}
		}
	}
	wrong := request(base, "POST", "/api/login", c.AppOrigin, "", "teacher_a", "wrong-input")
	unknown := request(base, "POST", "/api/login", c.AppOrigin, "", "missing-session-user", "wrong-input")
	injection := request(base, "POST", "/api/login", c.AppOrigin, "", "' OR 1=1 --", "wrong-input")
	if wrong.status != 401 || unknown.status != 401 || wrong.body != unknown.body || injection.body != wrong.body {
		t.Fatal("credential error differs")
	}
	if request(base, "POST", "/api/login", "", "", "teacher_a", os.Getenv("SEED_TEACHER_A_PASSWORD")).status != 403 {
		t.Fatal("missing origin accepted")
	}
	for _, path := range []string{"/health", "/api/materials"} {
		if request(base, "GET", path, "", "", "", "").status != 503 {
			t.Fatal("unimplemented feature opened")
		}
	}
	// Controlled clock is injected into a test server only, with the real MySQL repository.
	start := time.Now().UTC().Truncate(time.Second)
	var clock atomic.Int64
	clock.Store(start.Unix())
	auth, e := services.NewAuth(repositories.NewSessions(db), c.SessionSecret, func() time.Time { return time.Unix(clock.Load(), 0).UTC() })
	if e != nil {
		t.Fatal(e)
	}
	timed := httptest.NewServer(handlers.NewWithSessions(auth, "https://course.example"))
	defer timed.Close()
	issued := request(timed.URL, "POST", "/api/login", "https://course.example", "", "teacher_a", os.Getenv("SEED_TEACHER_A_PASSWORD"))
	if issued.status != 200 || issued.cookie == nil || !issued.cookie.Secure || !issued.cookie.Expires.Equal(start.Add(8*time.Hour)) {
		t.Fatal("HTTPS cookie or fixed expiry incorrect")
	}
	clock.Store(start.Add(8*time.Hour - time.Second).Unix())
	me(timed.URL, issued.cookie.Value, 200)
	clock.Store(start.Add(8 * time.Hour).Unix())
	me(timed.URL, issued.cookie.Value, 401)
	clock.Store(start.Add(9 * time.Hour).Unix())
	me(timed.URL, issued.cookie.Value, 401)
	if request(timed.URL, "POST", "/api/logout", "https://course.example", issued.cookie.Value, "", "").status != 401 {
		t.Fatal("expired logout accepted")
	}
	// Duplicate cookie names are rejected instead of selecting an arbitrary identity.
	r, _ := http.NewRequest("GET", base+"/api/me", nil)
	r.Header.Set("Cookie", "campusclaw_session="+issued.cookie.Value+"; campusclaw_session="+issued.cookie.Value)
	resp, e := client.Do(r)
	if e != nil {
		t.Fatal("duplicate-cookie request failed")
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatal("duplicate cookie accepted")
	}
	closed, e := Open(ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	closed.Close()
	broken, e := services.NewAuth(repositories.NewSessions(closed), c.SessionSecret, nil)
	if e != nil {
		t.Fatal(e)
	}
	brokenServer := httptest.NewServer(handlers.NewWithSessions(broken, c.AppOrigin))
	defer brokenServer.Close()
	for _, item := range []struct{ method, path string }{{"POST", "/api/login"}, {"GET", "/api/me"}, {"POST", "/api/logout"}} {
		out := request(brokenServer.URL, item.method, item.path, c.AppOrigin, issued.cookie.Value, "teacher_a", os.Getenv("SEED_TEACHER_A_PASSWORD"))
		if out.status != 503 || out.cookie != nil || strings.Contains(out.body, "sql") {
			t.Fatal("DB failure not safely handled")
		}
	}
	if cmd.Process.Signal(os.Interrupt) != nil || cmd.Wait() != nil {
		t.Fatal("backend shutdown failed")
	}
	for _, secret := range append(tokens, c.MySQLPassword, c.SessionSecret, os.Getenv("SEED_TEACHER_A_PASSWORD"), os.Getenv("SEED_STUDENT_A_PASSWORD"), os.Getenv("SEED_TEACHER_B_PASSWORD"), os.Getenv("SEED_STUDENT_B_PASSWORD")) {
		if secret != "" && strings.Contains(logs.String(), secret) {
			t.Fatal("secret in logs")
		}
	}
	t.Log("PASS: four real login/me identities; cookie flags and HMAC-only storage; rotation/replay/logout; failed rotation rollback; unrelated sessions preserved")
	t.Log("PASS: controlled clock before/at/after 8h; HTTPS Secure flag; duplicate/tampered/missing cookies; Origin; all three DB failures503; no secret logs; materials still503")
}
