package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"campusclaw/backend/services"
)

type searchInput struct {
	Query   string          `json:"q"`
	Mode    string          `json:"mode"`
	Limit   int             `json:"limit"`
	Offset  int             `json:"offset"`
	ClassID json.RawMessage `json:"class_id"` // accepted for compatibility, never trusted
}

func handleSearch(w http.ResponseWriter, r *http.Request, auth *services.Auth, materials MaterialService, search KnowledgeService) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "请求方法不允许")
		return
	}
	if materials == nil || !materials.Ready() {
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "服务暂时不可用，请稍后再试")
		return
	}
	token, ok := requestToken(r)
	if !ok {
		authError(w, services.ErrSession)
		return
	}
	user, err := auth.Me(r.Context(), token)
	if err != nil {
		authError(w, err)
		return
	}
	if r.Body == nil {
		writeError(w, http.StatusBadRequest, "INVALID_INPUT", "检索请求格式不正确")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	raw, err := io.ReadAll(r.Body)
	if err != nil || !utf8.Valid(raw) {
		writeError(w, http.StatusBadRequest, "INVALID_INPUT", "检索请求格式不正确")
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var input searchInput
	var extra any
	if decoder.Decode(&input) != nil || decoder.Decode(&extra) != io.EOF || strings.TrimSpace(input.Query) == "" || utf8.RuneCountInString(input.Query) > 1000 {
		writeError(w, http.StatusBadRequest, "INVALID_INPUT", "检索问题不能为空且不能超过1000个字符")
		return
	}
	if input.Mode == "" {
		input.Mode = "hybrid"
	}
	if input.Mode != "keyword" && input.Mode != "vector" && input.Mode != "hybrid" {
		writeError(w, http.StatusBadRequest, "INVALID_INPUT", "检索模式不正确")
		return
	}
	if input.Limit == 0 {
		input.Limit = 10
	}
	if input.Limit < 1 || input.Limit > 20 || input.Offset < 0 || input.Offset > 500 {
		writeError(w, http.StatusBadRequest, "INVALID_INPUT", "分页参数不正确")
		return
	}
	if search == nil {
		writeError(w, http.StatusServiceUnavailable, "RETRIEVAL_UNAVAILABLE", "关键词检索服务暂不可用")
		return
	}
	var hits []services.KnowledgeHit
	query := strings.TrimSpace(input.Query)
	switch input.Mode {
	case "keyword":
		hits, err = search.SearchKeyword(r.Context(), user.ClassID, query, input.Limit, input.Offset)
	case "vector", "hybrid":
		if input.Mode == "hybrid" && input.Offset+input.Limit > 20 {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "混合检索暂支持前20条结果")
			return
		}
		vectorSearch, ok := search.(VectorKnowledgeService)
		if !ok {
			writeError(w, http.StatusServiceUnavailable, "RETRIEVAL_UNAVAILABLE", "向量检索服务尚未配置")
			return
		}
		if input.Mode == "vector" {
			hits, err = vectorSearch.SearchVector(r.Context(), user.ClassID, query, input.Limit, input.Offset)
		} else {
			hits, err = vectorSearch.SearchHybrid(r.Context(), user.ClassID, query, input.Limit, input.Offset)
		}
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "RETRIEVAL_UNAVAILABLE", "检索暂时不可用，请稍后再试")
		return
	}
	if hits == nil {
		hits = []services.KnowledgeHit{}
	}
	writeJSON(w, map[string]any{"items": hits, "total": len(hits), "mode": input.Mode})
}
