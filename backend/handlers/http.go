// Package handlers 提供凭据核对；其他业务仍未就绪。
package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
)

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func New() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" || r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
			// 会话和材料链路尚未完成，不能将整个服务标记为就绪。
			writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "服务暂时不可用，请稍后再试")
			return
		}
		writeError(w, http.StatusNotFound, "NOT_FOUND", "请求的资源不存在")
	})
}
