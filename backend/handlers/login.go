package handlers

import (
	"bytes"
	"campusclaw/backend/services"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

type CredentialChecker interface {
	Check(context.Context, string, string) error
}

// NewWithLogin adds credential verification only. No session is created.
func NewWithLogin(checker CredentialChecker, origin string) http.Handler {
	return loginRoutes(checker, nil, origin)
}
func NewWithSessions(auth *services.Auth, origin string) http.Handler {
	return loginRoutes(nil, auth, origin)
}
func loginRoutes(checker CredentialChecker, auth *services.Auth, origin string) http.Handler {
	fallback := New()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth != nil && (r.URL.Path == "/api/me" || r.URL.Path == "/api/logout") {
			sessionRequest(w, r, auth, origin)
			return
		}
		if r.URL.Path != "/api/login" {
			fallback.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeError(w, 405, "METHOD_NOT_ALLOWED", "请使用POST提交账号密码")
			return
		}
		if origin == "" || len(r.Header.Values("Origin")) != 1 || r.Header.Get("Origin") != origin {
			writeError(w, 403, "FORBIDDEN", "请求来源不允许")
			return
		}
		kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || kind != "application/json" {
			writeError(w, 400, "INVALID_INPUT", "请提交有效的账号密码JSON")
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				writeError(w, 413, "PAYLOAD_TOO_LARGE", "请求内容过大")
			} else {
				writeError(w, 400, "INVALID_INPUT", "请求内容无效")
			}
			return
		}
		var input struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		var extra any
		if !utf8.Valid(raw) || decoder.Decode(&input) != nil || decoder.Decode(&extra) != io.EOF || strings.TrimSpace(input.Username) == "" || utf8.RuneCountInString(input.Username) > 191 || input.Password == "" || len([]byte(input.Password)) > 72 {
			writeError(w, 400, "INVALID_INPUT", "账号密码格式不正确")
			return
		}
		if auth != nil {
			token, valid := requestToken(r)
			if !valid {
				writeError(w, 400, "INVALID_INPUT", "登录凭证格式不正确")
				return
			}
			user, fresh, expires, authErr := auth.Login(r.Context(), input.Username, input.Password, token)
			if authErr != nil {
				authError(w, authErr)
				return
			}
			setSessionCookie(w, origin, fresh, expires, int(services.SessionLifetime.Seconds()))
			writeJSON(w, user)
			return
		}
		err = checker.Check(r.Context(), input.Username, input.Password)
		if errors.Is(err, services.ErrCredentials) {
			writeError(w, 401, "INVALID_CREDENTIALS", "用户名或密码错误")
			return
		}
		if err != nil {
			writeError(w, 503, "SERVICE_UNAVAILABLE", "服务暂时不可用，请稍后再试")
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_ = json.NewEncoder(w).Encode(map[string]any{"verified": true, "session_created": false, "message": "账号密码核对通过，尚未建立登录状态"})
	})
}

const sessionCookieName = "campusclaw_session"

func requestToken(r *http.Request) (string, bool) {
	token := ""
	count := 0
	for _, cookie := range r.Cookies() {
		if cookie.Name == sessionCookieName {
			token = cookie.Value
			count++
		}
	}
	return token, count <= 1
}
func setSessionCookie(w http.ResponseWriter, origin, token string, expires time.Time, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: token, Path: "/", HttpOnly: true, Secure: strings.HasPrefix(origin, "https://"), SameSite: http.SameSiteLaxMode, Expires: expires, MaxAge: maxAge})
}
func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_ = json.NewEncoder(w).Encode(value)
}
func authError(w http.ResponseWriter, err error) {
	if errors.Is(err, services.ErrCredentials) {
		writeError(w, 401, "INVALID_CREDENTIALS", "用户名或密码错误")
		return
	}
	if errors.Is(err, services.ErrSession) {
		writeError(w, 401, "UNAUTHENTICATED", "请先登录或重新登录")
		return
	}
	writeError(w, 503, "SERVICE_UNAVAILABLE", "服务暂时不可用，请稍后再试")
}
func sessionRequest(w http.ResponseWriter, r *http.Request, auth *services.Auth, origin string) {
	method := http.MethodGet
	if r.URL.Path == "/api/logout" {
		method = http.MethodPost
	}
	if r.Method != method {
		w.Header().Set("Allow", method)
		writeError(w, 405, "METHOD_NOT_ALLOWED", "请求方法不允许")
		return
	}
	if method == http.MethodPost && (origin == "" || len(r.Header.Values("Origin")) != 1 || r.Header.Get("Origin") != origin) {
		writeError(w, 403, "FORBIDDEN", "请求来源不允许")
		return
	}
	token, valid := requestToken(r)
	if !valid {
		authError(w, services.ErrSession)
		return
	}
	if method == http.MethodGet {
		user, err := auth.Me(r.Context(), token)
		if err != nil {
			authError(w, err)
			return
		}
		writeJSON(w, user)
		return
	}
	if err := auth.Logout(r.Context(), token); err != nil {
		authError(w, err)
		return
	}
	setSessionCookie(w, origin, "", time.Unix(1, 0).UTC(), -1)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(204)
}
