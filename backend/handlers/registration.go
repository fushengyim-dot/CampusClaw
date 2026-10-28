package handlers

import (
	"campusclaw/backend/services"
	"net/http"
	"strconv"
	"strings"
)

// @api post /api/register Create a class-bound account with a one-time invitation.
// @api get /api/teacher/invitations List invitation status for the authenticated teacher's class.
// @api post /api/teacher/invitations Issue a one-time invitation for the teacher's class.
// @api delete /api/teacher/invitations/{id} Revoke an unused invitation created by the current teacher.
func handleRegistration(w http.ResponseWriter, r *http.Request, auth *services.Auth, origin string, store *services.RegistrationStore) {
	if r.URL.Path == "/api/register" {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writeError(w, 405, "METHOD_NOT_ALLOWED", "请使用POST提交注册信息")
			return
		}
		if !lesson5Origin(w, r, origin) {
			return
		}
		var in struct {
			Code     string `json:"code"`
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if !decodeLesson5JSON(w, r, &in) {
			return
		}
		if store.Register(r.Context(), in.Code, in.Username, in.Password) != nil {
			writeError(w, 400, "REGISTRATION_FAILED", "注册未完成，请检查邀请码和填写内容，或联系发码教师")
			return
		}
		user, token, expires, err := auth.Login(r.Context(), strings.TrimSpace(in.Username), in.Password, "")
		if err != nil {
			writeError(w, 503, "SERVICE_UNAVAILABLE", "账号已创建，但暂时无法建立登录状态；请使用登录页登录")
			return
		}
		setSessionCookie(w, origin, token, expires, int(services.SessionLifetime.Seconds()))
		writeJSONStatus(w, 201, user)
		return
	}
	if r.URL.Path != "/api/teacher/invitations" && !strings.HasPrefix(r.URL.Path, "/api/teacher/invitations/") {
		writeError(w, 404, "NOT_FOUND", "请求的资源不存在")
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
		writeError(w, 403, "FORBIDDEN", "只有教师可以管理邀请码")
		return
	}
	if r.URL.Path == "/api/teacher/invitations" {
		switch r.Method {
		case http.MethodGet:
			items, e := store.List(r.Context(), user)
			if e != nil {
				writeError(w, 503, "SERVICE_UNAVAILABLE", "邀请码列表暂时无法读取")
				return
			}
			writeJSON(w, map[string]any{"items": items})
		case http.MethodPost:
			if !lesson5Origin(w, r, origin) {
				return
			}
			var in struct {
				Role string `json:"role"`
			}
			if !decodeLesson5JSON(w, r, &in) {
				return
			}
			item, code, e := store.Issue(r.Context(), user, in.Role)
			if e != nil {
				writeError(w, 400, "INVALID_INPUT", "只能签发本班教师码或学生码")
				return
			}
			writeJSONStatus(w, 201, map[string]any{"invitation": item, "code": code})
		default:
			w.Header().Set("Allow", "GET, POST")
			writeError(w, 405, "METHOD_NOT_ALLOWED", "请求方法不允许")
		}
		return
	}
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", "DELETE")
		writeError(w, 405, "METHOD_NOT_ALLOWED", "请使用DELETE撤销邀请码")
		return
	}
	if !lesson5Origin(w, r, origin) {
		return
	}
	text := strings.TrimPrefix(r.URL.Path, "/api/teacher/invitations/")
	id, e := strconv.ParseInt(text, 10, 64)
	if e != nil || id < 1 || store.Revoke(r.Context(), user, id) != nil {
		writeError(w, 404, "NOT_FOUND", "邀请码不存在或不能撤销")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
