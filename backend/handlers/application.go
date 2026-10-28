package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"

	"campusclaw/backend/repositories"
	"campusclaw/backend/services"
)

type MaterialService interface {
	Ready() bool
	Recover(context.Context) error
	List(context.Context, int64, int, int) (services.MaterialPage, error)
	Detail(context.Context, int64, int64) (services.MaterialDetail, error)
	OpenDownload(context.Context, int64, int64) (services.Download, error)
	Upload(context.Context, repositories.User, string, string, string, string, []byte) (int64, error)
}

type KnowledgeService interface {
	SearchKeyword(context.Context, int64, string, int, int) ([]services.KnowledgeHit, error)
}

type VectorKnowledgeService interface {
	SearchVector(context.Context, int64, string, int, int) ([]services.KnowledgeHit, error)
	SearchHybrid(context.Context, int64, string, int, int) ([]services.KnowledgeHit, error)
}

type VectorIndexService interface {
	VectorIndexStatus(context.Context, int64, int64) (services.VectorIndexProgress, error)
	IndexMaterialVectors(context.Context, int64, int64) (services.VectorIndexProgress, error)
}

func NewApplication(auth *services.Auth, materials MaterialService, origin string, knowledge ...KnowledgeService) http.Handler {
	var search KnowledgeService
	if len(knowledge) > 0 {
		search = knowledge[0]
	}
	return NewApplicationWithChat(auth, materials, origin, search, nil)
}

func NewApplicationWithChat(auth *services.Auth, materials MaterialService, origin string, search KnowledgeService, chat services.ChatCompleter) http.Handler {
	return NewApplicationWithLesson5(auth, materials, origin, search, chat, nil)
}

func NewApplicationWithLesson5(auth *services.Auth, materials MaterialService, origin string, search KnowledgeService, chat services.ChatCompleter, lesson5 *services.Lesson5Store) http.Handler {
	return NewApplicationWithLesson6(auth, materials, origin, search, chat, lesson5, nil)
}

func NewApplicationWithLesson6(auth *services.Auth, materials MaterialService, origin string, search KnowledgeService, chat services.ChatCompleter, lesson5 *services.Lesson5Store, mcp *services.MCPRegistry, registration ...*services.RegistrationStore) http.Handler {
	sessions := loginRoutes(nil, auth, origin)
	var registrations *services.RegistrationStore
	if len(registration) > 0 {
		registrations = registration[0]
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/openapi.json" {
			serveOpenAPI(w, r)
			return
		}
		if r.URL.Path == "/swagger" || r.URL.Path == "/swagger/" {
			serveSwagger(w, r)
			return
		}
		if r.URL.Path == "/health" {
			if !materials.Ready() {
				_ = materials.Recover(r.Context())
			}
			if !materials.Ready() {
				writeError(w, 503, "SERVICE_UNAVAILABLE", "服务暂时不可用，请稍后再试")
				return
			}
			writeJSON(w, map[string]string{"status": "ok"})
			return
		}
		if r.URL.Path == "/api/register" || strings.HasPrefix(r.URL.Path, "/api/teacher/invitations") {
			if registrations == nil {
				writeError(w, 404, "NOT_FOUND", "请求的资源不存在")
				return
			}
			handleRegistration(w, r, auth, origin, registrations)
			return
		}
		if r.URL.Path == "/api/login" || r.URL.Path == "/api/me" || r.URL.Path == "/api/logout" {
			sessions.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/api/search" {
			handleSearch(w, r, auth, materials, search)
			return
		}
		if r.URL.Path == "/api/ask" {
			handleAsk(w, r, auth, materials, search, chat)
			return
		}
		if r.URL.Path == "/api/mcp/servers" || strings.HasPrefix(r.URL.Path, "/api/mcp/servers/") {
			if mcp == nil {
				writeError(w, 404, "NOT_FOUND", "请求的资源不存在")
				return
			}
			handleMCPServers(w, r, auth, origin, mcp)
			return
		}
		if r.URL.Path == "/api/mcp/calls" {
			if mcp == nil {
				writeError(w, 404, "NOT_FOUND", "请求的资源不存在")
				return
			}
			handleMCPCallHistory(w, r, auth, mcp)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/skills/") && strings.HasSuffix(r.URL.Path, "/mcp-tools") {
			if mcp == nil {
				writeError(w, 404, "NOT_FOUND", "请求的资源不存在")
				return
			}
			handleSkillMCPBindings(w, r, auth, origin, mcp)
			return
		}
		if r.URL.Path == "/api/apps" || strings.HasPrefix(r.URL.Path, "/api/agents") || strings.HasPrefix(r.URL.Path, "/api/skills") || strings.HasPrefix(r.URL.Path, "/api/conversations") {
			if lesson5 == nil {
				writeError(w, 404, "NOT_FOUND", "请求的资源不存在")
				return
			}
			handleLesson6(w, r, auth, origin, lesson5, search, chat, mcp)
			return
		}
		if r.URL.Path == "/api/materials" || strings.HasPrefix(r.URL.Path, "/api/materials/") {
			handleMaterials(w, r, auth, materials, search, origin)
			return
		}
		writeError(w, 404, "NOT_FOUND", "请求的资源不存在")
	})
}

func handleMaterials(w http.ResponseWriter, r *http.Request, auth *services.Auth, s MaterialService, search KnowledgeService, origin string) {
	if !s.Ready() {
		writeError(w, 503, "SERVICE_UNAVAILABLE", "服务暂时不可用，请稍后再试")
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
	if r.URL.Path == "/api/materials" {
		switch r.Method {
		case http.MethodGet:
			page, size := 1, 20
			if v := r.URL.Query().Get("page"); v != "" {
				n, e := strconv.Atoi(v)
				if e != nil || strconv.Itoa(n) != v || n < 1 {
					writeError(w, 400, "INVALID_INPUT", "分页参数不正确")
					return
				}
				page = n
			}
			if v := r.URL.Query().Get("page_size"); v != "" {
				n, e := strconv.Atoi(v)
				if e != nil || strconv.Itoa(n) != v || n < 1 || n > 100 {
					writeError(w, 400, "INVALID_INPUT", "分页参数不正确")
					return
				}
				size = n
			}
			result, e := s.List(r.Context(), user.ClassID, page, size)
			if e != nil {
				writeMaterialError(w, e)
				return
			}
			writeJSON(w, result)
		case http.MethodPost:
			if len(r.Header.Values("Origin")) != 1 || r.Header.Get("Origin") != origin || origin == "" {
				writeError(w, 403, "FORBIDDEN", "请求来源不允许")
				return
			}
			if user.Role != "teacher" {
				writeError(w, 403, "FORBIDDEN", "只有教师可以上传讲义")
				return
			}
			handleUpload(w, r, s, user)
		default:
			w.Header().Set("Allow", "GET, POST")
			writeError(w, 405, "METHOD_NOT_ALLOWED", "请求方法不允许")
		}
		return
	}
	parts := strings.Split(strings.TrimPrefix(path.Clean(r.URL.Path), "/api/materials/"), "/")
	if len(parts) < 1 || len(parts) > 2 || parts[0] == "" {
		writeError(w, 404, "NOT_FOUND", "请求的资源不存在")
		return
	}
	id, e := strconv.ParseInt(parts[0], 10, 64)
	if e != nil || id < 1 {
		writeError(w, 404, "NOT_FOUND", "请求的资源不存在")
		return
	}
	if len(parts) == 2 && parts[1] == "vector-index" {
		if _, e := s.Detail(r.Context(), user.ClassID, id); e != nil {
			writeMaterialError(w, e)
			return
		}
		indexer, ok := search.(VectorIndexService)
		if !ok {
			writeError(w, 503, "VECTOR_INDEX_UNAVAILABLE", "向量索引服务尚未配置")
			return
		}
		switch r.Method {
		case http.MethodGet:
			status, e := indexer.VectorIndexStatus(r.Context(), user.ClassID, id)
			if e != nil {
				writeError(w, 503, "VECTOR_INDEX_UNAVAILABLE", "暂时无法读取向量索引状态")
				return
			}
			writeJSON(w, status)
		case http.MethodPost:
			if len(r.Header.Values("Origin")) != 1 || r.Header.Get("Origin") != origin || origin == "" {
				writeError(w, 403, "FORBIDDEN", "请求来源不允许")
				return
			}
			if user.Role != "teacher" {
				writeError(w, 403, "FORBIDDEN", "只有教师可以启动讲义向量索引")
				return
			}
			progress, e := indexer.IndexMaterialVectors(r.Context(), user.ClassID, id)
			if e != nil {
				writeError(w, 503, "VECTOR_INDEX_FAILED", "本次向量索引未完成；已完成部分会保留，可稍后继续")
				return
			}
			writeJSON(w, progress)
		default:
			w.Header().Set("Allow", "GET, POST")
			writeError(w, 405, "METHOD_NOT_ALLOWED", "请求方法不允许")
		}
		return
	}
	if len(parts) == 2 && parts[1] == "download" {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			writeError(w, 405, "METHOD_NOT_ALLOWED", "请求方法不允许")
			return
		}
		d, e := s.OpenDownload(r.Context(), user.ClassID, id)
		if e != nil {
			writeMaterialError(w, e)
			return
		}
		f, e := openCheckedDownload(d.Path)
		if e != nil {
			writeError(w, 503, "SERVICE_UNAVAILABLE", "服务暂时不可用，请稍后再试")
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": d.Filename}))
		w.Header().Set("Content-Length", strconv.FormatInt(d.Size, 10))
		w.WriteHeader(200)
		_, _ = io.Copy(w, f)
		return
	}
	if len(parts) != 1 || r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeError(w, 405, "METHOD_NOT_ALLOWED", "请求方法不允许")
		return
	}
	d, e := s.Detail(r.Context(), user.ClassID, id)
	if e != nil {
		writeMaterialError(w, e)
		return
	}
	writeJSON(w, d)
}

func handleUpload(w http.ResponseWriter, r *http.Request, s MaterialService, user repositories.User) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, 405, "METHOD_NOT_ALLOWED", "请求方法不允许")
		return
	}
	media, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || media != "multipart/form-data" {
		writeError(w, 400, "INVALID_INPUT", "请使用讲义上传表单")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 6*1024*1024)
	if e := r.ParseMultipartForm(7 * 1024 * 1024); e != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(e, &tooLarge) {
			writeError(w, 413, "PAYLOAD_TOO_LARGE", "上传请求过大")
		} else {
			writeError(w, 400, "INVALID_INPUT", "上传表单无效")
		}
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	for key := range r.MultipartForm.Value {
		if key != "title" && key != "source_url" && key != "source_locator" && key != "class_id" && key != "uploaded_by" && key != "role" {
			writeError(w, 400, "INVALID_INPUT", "上传表单字段无效")
			return
		}
	}
	for key := range r.MultipartForm.File {
		if key != "file" {
			writeError(w, 400, "INVALID_INPUT", "上传表单字段无效")
			return
		}
	}
	file, header, e := r.FormFile("file")
	if e != nil {
		writeError(w, 400, "INVALID_INPUT", "请提供讲义文件")
		return
	}
	defer file.Close()
	data, e := io.ReadAll(io.LimitReader(file, services.MaxMaterialBytes+1))
	if e != nil {
		writeError(w, 400, "INVALID_INPUT", "讲义文件读取失败")
		return
	}
	if len(data) > services.MaxMaterialBytes {
		writeError(w, 413, "PAYLOAD_TOO_LARGE", "讲义文件过大")
		return
	}
	id, e := s.Upload(r.Context(), user, r.FormValue("title"), header.Filename, r.FormValue("source_url"), r.FormValue("source_locator"), data)
	if e != nil {
		writeMaterialError(w, e)
		return
	}
	writeJSONStatus(w, 201, map[string]int64{"id": id})
}

func writeJSONStatus(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeMaterialError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, services.ErrMaterialsUnavailable):
		writeError(w, 503, "SERVICE_UNAVAILABLE", "服务暂时不可用，请稍后再试")
	case errors.Is(err, services.ErrUploadUnknown):
		writeError(w, 503, "UPLOAD_RESULT_UNKNOWN", "上传结果暂未确认，请稍后刷新列表确认，勿重复上传")
	case errors.Is(err, services.ErrNotTeacher):
		writeError(w, 403, "FORBIDDEN", "只有教师可以上传讲义")
	case errors.Is(err, services.ErrMaterialNotFound):
		writeError(w, 404, "NOT_FOUND", "请求的资源不存在")
	case errors.Is(err, services.ErrPage), errors.Is(err, services.ErrUploadRejected):
		writeError(w, 400, "INVALID_INPUT", "请求内容不符合要求")
	case errors.Is(err, services.ErrUploadFailed):
		writeError(w, 500, "INTERNAL_ERROR", "保存讲义失败")
	case errors.Is(err, services.ErrUnsupportedType):
		writeError(w, 415, "UNSUPPORTED_MEDIA_TYPE", "只支持UTF-8的TXT或Markdown文件")
	default:
		writeError(w, 500, "INTERNAL_ERROR", "保存讲义失败")
	}
}

func openCheckedDownload(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("file unavailable")
	}
	return os.Open(path)
}
