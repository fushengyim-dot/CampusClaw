package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"campusclaw/backend/services"
)

// @api get /api/mcp/calls List recent tool calls for the current user or the teacher's class.
func handleMCPCallHistory(w http.ResponseWriter, r *http.Request, auth *services.Auth, registry *services.MCPRegistry) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeError(w, 405, "METHOD_NOT_ALLOWED", "请求方法不允许")
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
	items, err := registry.CallHistory(r.Context(), user, 50)
	if err != nil {
		writeError(w, 503, "MCP_UNAVAILABLE", "工具调用记录暂时无法读取")
		return
	}
	writeJSON(w, map[string]any{"items": items})
}

// @api get /api/skills/{id}/mcp-tools Read saved references in the teacher's class.
// @api put /api/skills/{id}/mcp-tools Bind only tools in the latest connected discovery snapshot.
func handleSkillMCPBindings(w http.ResponseWriter, r *http.Request, auth *services.Auth, origin string, registry *services.MCPRegistry) {
	if r.Method != http.MethodGet && r.Method != http.MethodPut {
		w.Header().Set("Allow", "GET, PUT")
		writeError(w, 405, "METHOD_NOT_ALLOWED", "请求方法不允许")
		return
	}
	if r.Method == http.MethodPut && !lesson5Origin(w, r, origin) {
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
	if user.Role != "teacher" {
		writeError(w, 404, "NOT_FOUND", "请求的资源不存在")
		return
	}
	parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/skills/"), "/mcp-tools"), "/")
	if len(parts) != 1 {
		writeError(w, 404, "NOT_FOUND", "请求的资源不存在")
		return
	}
	skillID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || skillID < 1 {
		writeError(w, 404, "NOT_FOUND", "请求的资源不存在")
		return
	}
	if r.Method == http.MethodGet {
		refs, e := registry.SkillToolReferences(r.Context(), user.ClassID, skillID)
		if e != nil {
			writeError(w, 404, "NOT_FOUND", "请求的资源不存在")
			return
		}
		writeJSON(w, map[string]any{"references": refs})
		return
	}
	var in struct {
		References []string `json:"references"`
	}
	if !decodeLesson5JSON(w, r, &in) {
		return
	}
	if err = registry.BindSkillTools(r.Context(), user, skillID, in.References); err != nil {
		switch {
		case errors.Is(err, services.ErrNotTeacher):
			writeError(w, 403, "FORBIDDEN", "只有教师可以修改技能工具绑定")
		case errors.Is(err, services.ErrMCPInvalid):
			writeError(w, 400, "INVALID_INPUT", "只能绑定本班已连接服务器当前发现的工具")
		default:
			writeError(w, 404, "NOT_FOUND", "请求的资源不存在")
		}
		return
	}
	writeJSON(w, map[string]any{"references": in.References})
}

// @api get /api/mcp/servers List MCP servers in the teacher's class; tokens are never returned.
// @api post /api/mcp/servers Register a class-scoped MCP server with an encrypted bearer token.
// @api put /api/mcp/servers/{id} Update a class-scoped MCP server.
// @api delete /api/mcp/servers/{id} Delete a class-scoped MCP server.
// @api post /api/mcp/servers/{id}/connect Connect and replace the discovered tool snapshot.
// @api post /api/mcp/servers/{id}/disconnect Disconnect and clear the discovered tool snapshot.
func handleMCPServers(w http.ResponseWriter, r *http.Request, auth *services.Auth, origin string, registry *services.MCPRegistry) {
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
	if user.Role != "teacher" {
		writeError(w, 404, "NOT_FOUND", "请求的资源不存在")
		return
	}
	base := "/api/mcp/servers"
	if r.URL.Path == base {
		switch r.Method {
		case http.MethodGet:
			items, e := registry.List(r.Context(), user.ClassID)
			if e != nil {
				writeError(w, 503, "MCP_UNAVAILABLE", "服务器列表暂时无法读取")
				return
			}
			writeJSON(w, map[string]any{"servers": items})
		case http.MethodPost:
			if !lesson5Origin(w, r, origin) {
				return
			}
			var in mcpServerRequest
			if !decodeLesson5JSON(w, r, &in) {
				return
			}
			id, e := registry.Save(r.Context(), user, 0, in.Name, in.Endpoint, in.Token)
			if e != nil {
				writeMCPError(w, e)
				return
			}
			writeJSONStatus(w, 201, map[string]any{"id": id, "token_configured": true})
		default:
			w.Header().Set("Allow", "GET, POST")
			writeError(w, 405, "METHOD_NOT_ALLOWED", "请求方法不允许")
		}
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, base+"/"), "/")
	if len(parts) < 1 || len(parts) > 2 {
		writeError(w, 404, "NOT_FOUND", "请求的资源不存在")
		return
	}
	id, e := strconv.ParseInt(parts[0], 10, 64)
	if e != nil || id < 1 {
		writeError(w, 404, "NOT_FOUND", "请求的资源不存在")
		return
	}
	if !registry.Exists(r.Context(), user.ClassID, id) {
		writeError(w, 404, "NOT_FOUND", "请求的资源不存在")
		return
	}
	if len(parts) == 2 {
		if !lesson5Origin(w, r, origin) {
			return
		}
		switch parts[1] {
		case "connect":
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", "POST")
				writeError(w, 405, "METHOD_NOT_ALLOWED", "请求方法不允许")
				return
			}
			if e = registry.Discover(r.Context(), user.ClassID, id); e != nil {
				writeError(w, 503, "MCP_UNAVAILABLE", "连接或工具发现未完成；已清空旧工具清单")
				return
			}
			writeJSON(w, map[string]string{"status": "connected"})
		case "disconnect":
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", "POST")
				writeError(w, 405, "METHOD_NOT_ALLOWED", "请求方法不允许")
				return
			}
			if e = registry.Disconnect(r.Context(), user.ClassID, id); e != nil {
				writeError(w, 404, "NOT_FOUND", "请求的资源不存在")
				return
			}
			writeJSON(w, map[string]string{"status": "disconnected"})
		default:
			writeError(w, 404, "NOT_FOUND", "请求的资源不存在")
		}
		return
	}
	switch r.Method {
	case http.MethodPut:
		if !lesson5Origin(w, r, origin) {
			return
		}
		var in mcpServerRequest
		if !decodeLesson5JSON(w, r, &in) {
			return
		}
		if _, e = registry.Save(r.Context(), user, id, in.Name, in.Endpoint, in.Token); e != nil {
			writeMCPError(w, e)
			return
		}
		writeJSON(w, map[string]any{"id": id, "token_configured": true})
	case http.MethodDelete:
		if !lesson5Origin(w, r, origin) {
			return
		}
		if e = registry.Delete(r.Context(), user.ClassID, id); e != nil {
			writeError(w, 404, "NOT_FOUND", "请求的资源不存在")
			return
		}
		writeJSON(w, map[string]string{"status": "deleted"})
	default:
		w.Header().Set("Allow", "PUT, DELETE")
		writeError(w, 405, "METHOD_NOT_ALLOWED", "请求方法不允许")
	}
}

type mcpServerRequest struct {
	Name     string `json:"name"`
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
}

func writeMCPError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, services.ErrMCPInvalid):
		writeError(w, 400, "INVALID_INPUT", "服务器名称、地址或令牌格式不正确；地址仅支持本项目内置工具服务")
	case errors.Is(err, services.ErrNotTeacher):
		writeError(w, 404, "NOT_FOUND", "请求的资源不存在")
	default:
		writeError(w, 404, "NOT_FOUND", "请求的资源不存在")
	}
}
