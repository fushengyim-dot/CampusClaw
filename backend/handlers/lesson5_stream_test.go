package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"campusclaw/backend/repositories"
	"campusclaw/backend/services"
)

type lesson5StreamStoreStub struct {
	agent          services.Agent
	skills         []services.RoutedSkill
	materials      []int64
	question       string
	saved          string
	savedSkill     string
	savedCitations []services.KnowledgeHit
}

func (s *lesson5StreamStoreStub) AgentContext(context.Context, repositories.User, int64) (services.Agent, []services.RoutedSkill, []int64, error) {
	return s.agent, s.skills, s.materials, nil
}
func (s *lesson5StreamStoreStub) AppendQuestion(_ context.Context, _ repositories.User, _ int64, question string) (int64, []services.ChatMessage, error) {
	s.question = question
	return 10, []services.ChatMessage{{Role: "user", Content: question}}, nil
}
func (s *lesson5StreamStoreStub) SaveAnswer(_ context.Context, _ repositories.User, _ int64, content string, citations []services.KnowledgeHit, skillName string) (int64, error) {
	s.saved, s.savedSkill, s.savedCitations = content, skillName, citations
	return 99, nil
}

type lesson5StreamChatStub struct {
	parts    []string
	failAt   int
	messages []services.ChatMessage
}

type lesson5KnowledgeStub struct{}

func (lesson5KnowledgeStub) SearchKeyword(context.Context, int64, string, int, int) ([]services.KnowledgeHit, error) {
	return nil, nil
}

type lesson5ScopedKnowledgeStub struct {
	items []services.KnowledgeHit
}

func (lesson5ScopedKnowledgeStub) SearchKeyword(context.Context, int64, string, int, int) ([]services.KnowledgeHit, error) {
	return nil, nil
}
func (s lesson5ScopedKnowledgeStub) SearchHybridMaterials(_ context.Context, _ int64, _ string, materialIDs []int64, limit int) ([]services.KnowledgeHit, error) {
	if len(materialIDs) != 1 || materialIDs[0] != 11 || limit != 4 {
		return nil, errors.New("retrieval scope changed")
	}
	return s.items, nil
}

func (s *lesson5StreamChatStub) Complete(context.Context, []services.ChatMessage) (string, error) {
	return "", errors.New("non-stream path must not be called")
}
func (s *lesson5StreamChatStub) Stream(_ context.Context, messages []services.ChatMessage, emit func(string) error) error {
	s.messages = messages
	for i, part := range s.parts {
		if s.failAt > 0 && i >= s.failAt {
			return errors.New("local stub stopped")
		}
		if err := emit(part); err != nil {
			return err
		}
	}
	return nil
}

func TestLesson5StreamEmitsAndPersistsWithoutEvidence(t *testing.T) {
	store := &lesson5StreamStoreStub{agent: services.Agent{Prompt: "teacher role", BuiltinKey: "guided"}, skills: []services.RoutedSkill{{ID: 2, Name: "解题引导", TriggerText: "递归", Description: "只提示下一步", Enabled: true, Bound: true}}}
	chat := &lesson5StreamChatStub{parts: []string{"下一步检查", "递归出口。[1]"}}
	r := httptest.NewRequest(http.MethodPost, "/api/conversations/7/messages", strings.NewReader(`{"content":"递归这里怎么改","prompt_template":"next"}`))
	w := httptest.NewRecorder()
	streamConversation(w, r, store, repositories.User{ID: 3, ClassID: 4, Role: "student"}, 7, lesson5KnowledgeStub{}, chat)
	body := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(body, "event: meta") || strings.Count(body, "event: delta") != 2 || !strings.Contains(body, `"message_id":99`) || !strings.Contains(body, "event: done") {
		t.Fatalf("unexpected successful SSE sequence: status=%d body=%s", w.Code, body)
	}
	if strings.Contains(body, "[1]") || strings.Contains(store.saved, "[1]") || store.saved != "下一步检查递归出口。" || store.savedSkill != "解题引导" {
		t.Fatalf("no-evidence response fabricated a citation or was not persisted safely: %#v %q", store, body)
	}
	if len(chat.messages) != 2 || chat.messages[0].Role != "system" || !strings.Contains(chat.messages[0].Content, "不得给出最终答案") || !strings.Contains(chat.messages[1].Content, "本轮学生提问模板") {
		t.Fatalf("role, skill, or student template was not placed in the right message: %#v", chat.messages)
	}
}

func TestLesson5StreamPersistsPartialAnswerAndReportsGenericFailure(t *testing.T) {
	store := &lesson5StreamStoreStub{agent: services.Agent{Prompt: "teacher role"}}
	chat := &lesson5StreamChatStub{parts: []string{"partial ", "discarded"}, failAt: 1}
	r := httptest.NewRequest(http.MethodPost, "/api/conversations/7/messages", strings.NewReader(`{"content":"问题"}`))
	w := httptest.NewRecorder()
	streamConversation(w, r, store, repositories.User{ID: 3, ClassID: 4, Role: "student"}, 7, lesson5KnowledgeStub{}, chat)
	body := w.Body.String()
	if w.Code != http.StatusOK || store.saved != "partial " || !strings.Contains(body, "event: done") || !strings.Contains(body, "event: error") || strings.Contains(body, "local stub stopped") {
		t.Fatalf("partial stream was not safely persisted/reported: status=%d saved=%q body=%s", w.Code, store.saved, body)
	}
	if strings.Index(body, "event: done") > strings.Index(body, "event: error") {
		t.Fatalf("failure should follow the persisted done event: %s", body)
	}
}

func TestLesson5StreamKeepsBoundMaterialCitationsContiguous(t *testing.T) {
	store := &lesson5StreamStoreStub{agent: services.Agent{Prompt: "teacher role"}, materials: []int64{11}, skills: []services.RoutedSkill{{ID: 1, Name: "数学解题引导", TriggerText: "通分", SubjectText: "数学", Enabled: true, Bound: true}}}
	search := lesson5ScopedKnowledgeStub{items: []services.KnowledgeHit{
		{ChunkID: 1, MaterialID: 11, Title: "数学笔记", Excerpt: "先找公分母"},
		{ChunkID: 2, MaterialID: 11, Title: "语文笔记", Excerpt: "不属于当前学科"},
	}}
	chat := &lesson5StreamChatStub{parts: []string{"根据资料[1]，先找公分母。"}}
	r := httptest.NewRequest(http.MethodPost, "/api/conversations/7/messages", strings.NewReader(`{"content":"通分怎么开始"}`))
	w := httptest.NewRecorder()
	streamConversation(w, r, store, repositories.User{ID: 3, ClassID: 4, Role: "student"}, 7, search, chat)
	body := w.Body.String()
	if w.Code != http.StatusOK || strings.Count(body, `"chunk_id"`) != 1 || !strings.Contains(body, `"chunk_id":1`) || strings.Contains(body, "语文笔记") || !strings.Contains(body, `"text":"根据资料[1]，先找公分母。"`) {
		t.Fatalf("bound material or contiguous citation handling failed: %s", body)
	}
	if len(store.savedCitations) != 1 || store.savedCitations[0].ChunkID != 1 || !strings.Contains(store.saved, "[1]") {
		t.Fatalf("persisted answer lost citation alignment: %#v %q", store.savedCitations, store.saved)
	}
}

type lesson6RegistryStub struct {
	bound  []services.MCPBoundTool
	calls  int
	audits int
}

func (s *lesson6RegistryStub) BoundTools(context.Context, int64, int64) ([]services.MCPBoundTool, error) {
	return s.bound, nil
}
func (s *lesson6RegistryStub) CallTool(_ context.Context, _ services.MCPBoundTool, args map[string]any) (services.MCPCallResult, error) {
	s.calls++
	if args["term"] != "MCP" {
		return services.MCPCallResult{IsError: true}, errors.New("bad fixture")
	}
	return services.MCPCallResult{Text: "MCP词条"}, nil
}
func (s *lesson6RegistryStub) RecordCall(context.Context, repositories.User, int64, int, services.MCPBoundTool, string, string) error {
	s.audits++
	return nil
}

type lesson6ChatStub struct {
	turns       []services.MCPModelTurn
	sawToolData bool
}

func (*lesson6ChatStub) Complete(context.Context, []services.ChatMessage) (string, error) {
	return "", errors.New("unexpected non-stream call")
}
func (*lesson6ChatStub) Stream(context.Context, []services.ChatMessage, func(string) error) error {
	return errors.New("structured tool path should not use ordinary stream")
}
func (c *lesson6ChatStub) CompleteWithTools(_ context.Context, messages []services.MCPModelMessage, tools []services.MCPToolDefinition) (services.MCPModelTurn, error) {
	for _, message := range messages {
		if message.Role == "tool" && message.Content == "MCP词条" {
			c.sawToolData = true
		}
	}
	if len(c.turns) == 0 {
		return services.MCPModelTurn{}, errors.New("missing synthetic model turn")
	}
	turn := c.turns[0]
	c.turns = c.turns[1:]
	return turn, nil
}

func TestLesson6SSEToolMetadataPrecedesDeltaAndUsesMockOnly(t *testing.T) {
	store := &lesson5StreamStoreStub{agent: services.Agent{Prompt: "teacher role"}, skills: []services.RoutedSkill{{ID: 5, Name: "课程术语工具", TriggerText: "协议", Description: "只查询课程术语", Enabled: true, Bound: true}}}
	bound := services.MCPBoundTool{ServerID: 2, ServerName: "campus-tools", Endpoint: "http://mcp-tools:8091/mcp", Token: "synthetic-token-that-is-long-enough-123456", Tool: services.MCPDiscoveredTool{Name: "lookup_course_term", Description: "只读词条", InputSchema: json.RawMessage(`{"type":"object","properties":{"term":{"type":"string","minLength":1,"maxLength":80}},"required":["term"],"additionalProperties":false}`), SchemaHash: "synthetic-hash"}}
	registry := &lesson6RegistryStub{bound: []services.MCPBoundTool{bound}}
	call := services.MCPProposedCall{ID: "call-1", Arguments: json.RawMessage(`{"term":"MCP"}`)}
	model := &lesson6ChatStub{turns: []services.MCPModelTurn{{ToolCalls: []services.MCPProposedCall{call}}, {Content: "MCP是课程中使用的工具协议。"}}}
	modelAlias := func() string { // mirror the public, stable tool-name encoding without exposing any secret
		const alphabet = "0123456789abcdef"
		sum := sha256.Sum256([]byte("lookup_course_term@campus-tools"))
		var b strings.Builder
		b.WriteString("mcp_")
		for _, v := range sum[:8] {
			b.WriteByte(alphabet[v>>4])
			b.WriteByte(alphabet[v&15])
		}
		return b.String()
	}
	model.turns[0].ToolCalls[0].Name = modelAlias()
	r := httptest.NewRequest(http.MethodPost, "/api/conversations/7/messages", strings.NewReader(`{"content":"这个协议是什么意思"}`))
	w := httptest.NewRecorder()
	streamConversationWithMCP(w, r, store, repositories.User{ID: 3, ClassID: 4, Role: "student"}, 7, lesson5KnowledgeStub{}, model, registry)
	body := w.Body.String()
	meta := strings.Index(body, "event: meta")
	tool := strings.Index(body, "event: tool")
	delta := strings.Index(body, "event: delta")
	if w.Code != http.StatusOK || meta < 0 || tool < 0 || delta < 0 || !(meta < tool && tool < delta) || !strings.Contains(body, `"tools"`) || !strings.Contains(body, "lookup_course_term") || registry.calls != 1 || registry.audits != 1 || !model.sawToolData || store.saved != "MCP是课程中使用的工具协议。" {
		t.Fatalf("tool host/SSE sequence failed: %d %s", w.Code, body)
	}
}
