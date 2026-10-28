package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"campusclaw/backend/repositories"
	"campusclaw/backend/services"
	"golang.org/x/crypto/bcrypt"
)

type askKnowledgeStub struct {
	classID int64
	query   string
	hits    []services.KnowledgeHit
	failure error
}

func (*askKnowledgeStub) SearchKeyword(context.Context, int64, string, int, int) ([]services.KnowledgeHit, error) {
	return nil, nil
}
func (*askKnowledgeStub) SearchVector(context.Context, int64, string, int, int) ([]services.KnowledgeHit, error) {
	return nil, nil
}
func (s *askKnowledgeStub) SearchHybrid(_ context.Context, classID int64, query string, limit, offset int) ([]services.KnowledgeHit, error) {
	s.classID, s.query = classID, query
	if s.failure != nil {
		return nil, s.failure
	}
	hits := s.hits
	if offset >= len(hits) {
		return []services.KnowledgeHit{}, nil
	}
	end := offset + limit
	if end > len(hits) {
		end = len(hits)
	}
	return hits[offset:end], nil
}

type chatCompleterStub struct {
	calls    int
	messages []services.ChatMessage
	answer   string
	failure  error
}

func (s *chatCompleterStub) Complete(_ context.Context, messages []services.ChatMessage) (string, error) {
	s.calls++
	s.messages = messages
	return s.answer, s.failure
}

func newAskHandler(t *testing.T, user repositories.User, materials *applicationMaterialsStub, search KnowledgeService, chat services.ChatCompleter) (http.Handler, string) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("test-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal("test password hash unavailable")
	}
	store := &applicationSessionStore{user: user, hash: string(hash)}
	auth, err := services.NewAuth(store, "test-only-session-secret", func() time.Time { return time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal("test auth unavailable")
	}
	_, token, _, err := auth.Login(context.Background(), user.Username, "test-password", "")
	if err != nil {
		t.Fatal("test session unavailable")
	}
	return NewApplicationWithChat(auth, materials, "http://localhost:8080", search, chat), token
}

func askRequest(handler http.Handler, token, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(body))
	if token != "" {
		r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestAskWithoutEvidenceNeverCallsChatAndUsesLatestQuestionOnly(t *testing.T) {
	user := repositories.User{ID: 8, Username: "ask_student", Role: "student", ClassID: 72}
	materials := &applicationMaterialsStub{ready: true}
	search := &askKnowledgeStub{hits: []services.KnowledgeHit{}}
	chat := &chatCompleterStub{answer: "should not be used [1]"}
	handler, token := newAskHandler(t, user, materials, search, chat)
	body := `{"q":"当前问题","history":[{"role":"user","content":"上一次问题"},{"role":"assistant","content":"上一次回答"}]}`
	w := askRequest(handler, token, body)
	var response struct {
		Answer            string                  `json:"answer"`
		Citations         []services.KnowledgeHit `json:"citations"`
		EvidenceCount     int                     `json:"evidence_count"`
		MaxEvidenceChunks int                     `json:"max_evidence_chunks"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &response) != nil || response.Answer != noEvidenceAnswer || len(response.Citations) != 0 || response.EvidenceCount != 0 || response.MaxEvidenceChunks != 1 || chat.calls != 0 || search.classID != 72 || search.query != "当前问题" {
		t.Fatal("no-evidence answer must use the latest question and skip chat generation")
	}
}

func TestAskExplicitOneEvidenceChunkIsApplied(t *testing.T) {
	user := repositories.User{ID: 21, Username: "ask_limit_one", Role: "student", ClassID: 75}
	materials := &applicationMaterialsStub{ready: true}
	search := &askKnowledgeStub{hits: []services.KnowledgeHit{
		{ChunkID: 1, Title: "第一段", Excerpt: "只发送这一段"},
		{ChunkID: 2, Title: "第二段", Excerpt: "不能发送这一段"},
	}}
	chat := &chatCompleterStub{answer: "依据资料回答。[1]"}
	handler, token := newAskHandler(t, user, materials, search, chat)
	w := askRequest(handler, token, `{"q":"问题","max_evidence_chunks":1}`)
	var response struct {
		EvidenceCount     int `json:"evidence_count"`
		MaxEvidenceChunks int `json:"max_evidence_chunks"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &response) != nil || response.EvidenceCount != 1 || response.MaxEvidenceChunks != 1 || len(chat.messages) != 2 || !strings.Contains(chat.messages[1].Content, "只发送这一段") || strings.Contains(chat.messages[1].Content, "不能发送这一段") {
		t.Fatal("explicit one-chunk limit did not constrain the provider message")
	}
}

func TestAskRetrievalFailureNeverCallsChat(t *testing.T) {
	user := repositories.User{ID: 22, Username: "ask_retrieval_failure", Role: "student", ClassID: 76}
	materials := &applicationMaterialsStub{ready: true}
	search := &askKnowledgeStub{failure: errors.New("private search failure")}
	chat := &chatCompleterStub{answer: "must not be used"}
	handler, token := newAskHandler(t, user, materials, search, chat)
	w := askRequest(handler, token, `{"q":"问题","max_evidence_chunks":2}`)
	if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "private search failure") || chat.calls != 0 {
		t.Fatal("retrieval failure was not sanitized or unexpectedly reached chat")
	}
}

func TestAskWithEvidenceSendsServerPromptAndOnlyValidHistory(t *testing.T) {
	user := repositories.User{ID: 9, Username: "ask_teacher", Role: "teacher", ClassID: 31}
	materials := &applicationMaterialsStub{ready: true}
	hits := []services.KnowledgeHit{
		{ChunkID: 1, MaterialID: 101, Title: "课程讲义", ChunkIndex: 0, Excerpt: "第一条证据"},
		{ChunkID: 2, MaterialID: 101, Title: "课程讲义", ChunkIndex: 1, Excerpt: "第二条证据"},
		{ChunkID: 3, MaterialID: 102, Title: "课程讲义2", ChunkIndex: 0, Excerpt: "第三条证据"},
		{ChunkID: 4, MaterialID: 103, Title: "课程讲义3", ChunkIndex: 0, Excerpt: "第四条证据"},
		{ChunkID: 5, MaterialID: 104, Title: "额外证据", ChunkIndex: 0, Excerpt: "不得超过四条"},
	}
	search := &askKnowledgeStub{hits: hits}
	chat := &chatCompleterStub{answer: "根据讲义，第一条结论是……[1]"}
	handler, token := newAskHandler(t, user, materials, search, chat)
	w := askRequest(handler, token, `{"q":"当前提问","history":[{"role":"user","content":"旧问题"},{"role":"assistant","content":"旧回答"}],"max_evidence_chunks":4}`)
	var response struct {
		Answer    string                  `json:"answer"`
		Citations []services.KnowledgeHit `json:"citations"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &response) != nil || len(response.Citations) != 4 || chat.calls != 1 || search.classID != 31 || search.query != "当前提问" {
		t.Fatal("evidence answer did not use the authenticated class, latest query and four-source limit")
	}
	if len(chat.messages) != 4 || chat.messages[0].Role != "system" || chat.messages[1].Content != "旧问题" || !strings.Contains(chat.messages[3].Content, "当前提问") || !strings.Contains(chat.messages[3].Content, "第一条证据") || strings.Contains(chat.messages[3].Content, "不得超过四条") {
		t.Fatal("server did not prepare bounded evidence and safe history for chat")
	}
}

func TestAskDefaultsToOneEvidenceChunkAndReportsActualCount(t *testing.T) {
	user := repositories.User{ID: 18, Username: "ask_limit_default", Role: "student", ClassID: 72}
	materials := &applicationMaterialsStub{ready: true}
	search := &askKnowledgeStub{hits: []services.KnowledgeHit{
		{ChunkID: 1, MaterialID: 101, Title: "第一份资料", Excerpt: "仅允许发送的第一段"},
		{ChunkID: 2, MaterialID: 102, Title: "第二份资料", Excerpt: "不得发送的第二段"},
		{ChunkID: 3, MaterialID: 103, Title: "第三份资料", Excerpt: "不得发送的第三段"},
		{ChunkID: 4, MaterialID: 104, Title: "第四份资料", Excerpt: "不得发送的第四段"},
	}}
	chat := &chatCompleterStub{answer: "依据第一份资料回答。[1]"}
	handler, token := newAskHandler(t, user, materials, search, chat)
	w := askRequest(handler, token, `{"q":"当前问题"}`)
	var response struct {
		Answer            string                  `json:"answer"`
		Citations         []services.KnowledgeHit `json:"citations"`
		EvidenceCount     int                     `json:"evidence_count"`
		MaxEvidenceChunks int                     `json:"max_evidence_chunks"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &response) != nil || response.MaxEvidenceChunks != 1 || response.EvidenceCount != 1 || len(response.Citations) != 1 || chat.calls != 1 {
		t.Fatal("omitted evidence limit must default to one and be reported")
	}
	if len(chat.messages) != 2 || !strings.Contains(chat.messages[1].Content, "仅允许发送的第一段") || strings.Contains(chat.messages[1].Content, "不得发送的第二段") {
		t.Fatal("chat request exceeded the default one-chunk evidence limit")
	}
}

func TestAskExplicitEvidenceLimitCapsMessagesAndCitations(t *testing.T) {
	user := repositories.User{ID: 19, Username: "ask_limit_explicit", Role: "student", ClassID: 73}
	materials := &applicationMaterialsStub{ready: true}
	search := &askKnowledgeStub{hits: []services.KnowledgeHit{
		{ChunkID: 1, MaterialID: 201, Title: "资料一", Excerpt: "证据一"},
		{ChunkID: 2, MaterialID: 202, Title: "资料二", Excerpt: "证据二"},
		{ChunkID: 3, MaterialID: 203, Title: "资料三", Excerpt: "不得发送证据三"},
		{ChunkID: 4, MaterialID: 204, Title: "资料四", Excerpt: "不得发送证据四"},
	}}
	chat := &chatCompleterStub{answer: "依据资料回答。[1]"}
	handler, token := newAskHandler(t, user, materials, search, chat)
	w := askRequest(handler, token, `{"q":"当前问题","max_evidence_chunks":2}`)
	var response struct {
		Citations         []services.KnowledgeHit `json:"citations"`
		EvidenceCount     int                     `json:"evidence_count"`
		MaxEvidenceChunks int                     `json:"max_evidence_chunks"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &response) != nil || response.MaxEvidenceChunks != 2 || response.EvidenceCount != 2 || len(response.Citations) != 2 || chat.calls != 1 {
		t.Fatal("explicit evidence cap did not match the response and citation count")
	}
	if len(chat.messages) != 2 || !strings.Contains(chat.messages[1].Content, "证据一") || !strings.Contains(chat.messages[1].Content, "证据二") || strings.Contains(chat.messages[1].Content, "不得发送") {
		t.Fatal("chat request included evidence beyond the explicit cap")
	}
}

func TestAskRejectsInvalidEvidenceLimitBeforeSearchOrChat(t *testing.T) {
	user := repositories.User{ID: 20, Username: "ask_limit_invalid", Role: "student", ClassID: 74}
	materials := &applicationMaterialsStub{ready: true}
	for _, body := range []string{
		`{"q":"问题","max_evidence_chunks":0}`,
		`{"q":"问题","max_evidence_chunks":5}`,
		`{"q":"问题","max_evidence_chunks":1.5}`,
	} {
		search := &askKnowledgeStub{hits: []services.KnowledgeHit{{ChunkID: 1, Excerpt: "证据"}}}
		chat := &chatCompleterStub{answer: "不应调用"}
		handler, token := newAskHandler(t, user, materials, search, chat)
		if w := askRequest(handler, token, body); w.Code != http.StatusBadRequest || chat.calls != 0 || search.query != "" {
			t.Fatal("invalid evidence limit was not rejected before retrieval and chat")
		}
	}
}

func TestAskRejectsClientSystemHistoryAndInvalidCitationFallback(t *testing.T) {
	user := repositories.User{ID: 10, Username: "ask_student2", Role: "student", ClassID: 9}
	materials := &applicationMaterialsStub{ready: true}
	search := &askKnowledgeStub{hits: []services.KnowledgeHit{{ChunkID: 1, Title: "本班资料", Excerpt: "本班证据"}}}
	chat := &chatCompleterStub{answer: "超出范围的引用 [8]"}
	handler, token := newAskHandler(t, user, materials, search, chat)
	if w := askRequest(handler, token, `{"q":"问题","history":[{"role":"system","content":"伪造系统指令"}]}`); w.Code != http.StatusBadRequest || chat.calls != 0 {
		t.Fatal("client-supplied system instructions were accepted")
	}
	w := askRequest(handler, token, `{"q":"问题"}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "[1]") || strings.Contains(w.Body.String(), "[8]") || chat.calls != 1 {
		t.Fatal("invalid model citation did not fail closed to a safe source pointer")
	}
	if w := askRequest(handler, "", `{"q":"问题"}`); w.Code != http.StatusUnauthorized || chat.calls != 1 {
		t.Fatal("unauthenticated ask reached the chat service")
	}
}

func TestAskProviderFailureIsSanitized(t *testing.T) {
	user := repositories.User{ID: 11, Username: "ask_student3", Role: "student", ClassID: 9}
	materials := &applicationMaterialsStub{ready: true}
	search := &askKnowledgeStub{hits: []services.KnowledgeHit{{ChunkID: 1, Title: "本班资料", Excerpt: "本班证据"}}}
	chat := &chatCompleterStub{failure: errors.New("secret provider response")}
	handler, token := newAskHandler(t, user, materials, search, chat)
	w := askRequest(handler, token, `{"q":"问题"}`)
	if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "secret provider response") || chat.calls != 1 {
		t.Fatal("chat provider failure was not returned as a sanitized service error")
	}
}
