package services

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"campusclaw/backend/repositories"
)

type mcpRegistryFake struct {
	bound                 []MCPBoundTool
	result                MCPCallResult
	callCount, auditCount int
	statuses              []string
}

func (f *mcpRegistryFake) BoundTools(context.Context, int64, int64) ([]MCPBoundTool, error) {
	return f.bound, nil
}
func (f *mcpRegistryFake) CallTool(_ context.Context, _ MCPBoundTool, args map[string]any) (MCPCallResult, error) {
	f.callCount++
	if args["term"] != "MCP" {
		return MCPCallResult{}, ErrMCPInvalid
	}
	return f.result, nil
}
func (f *mcpRegistryFake) RecordCall(_ context.Context, _ repositories.User, _ int64, _ int, _ MCPBoundTool, status, _ string) error {
	f.auditCount++
	f.statuses = append(f.statuses, status)
	return nil
}

type mcpModelFake struct {
	turns       []MCPModelTurn
	calls       int
	sawToolData bool
	toolsCounts []int
}

func (m *mcpModelFake) CompleteWithTools(_ context.Context, messages []MCPModelMessage, tools []MCPToolDefinition) (MCPModelTurn, error) {
	m.calls++
	m.toolsCounts = append(m.toolsCounts, len(tools))
	for _, message := range messages {
		if message.Role == "tool" && message.Content == "本地词条说明" {
			m.sawToolData = true
		}
	}
	if len(m.turns) == 0 {
		return MCPModelTurn{}, errors.New("unexpected model turn")
	}
	turn := m.turns[0]
	m.turns = m.turns[1:]
	return turn, nil
}

func fakeBoundTool() MCPBoundTool {
	return MCPBoundTool{ServerID: 1, ServerName: "campus-tools", Endpoint: builtinMCPEndpoint, Token: "synthetic-token-that-is-long-enough-123456", Tool: MCPDiscoveredTool{Name: "lookup_course_term", Description: "只读术语查询", InputSchema: json.RawMessage(`{"type":"object","properties":{"term":{"type":"string","minLength":1,"maxLength":80}},"required":["term"],"additionalProperties":false}`), SchemaHash: "synthetic-hash"}}
}

func TestMCPOrchestratorRunsBoundToolThenReturnsDataRoleBeforeText(t *testing.T) {
	bound := fakeBoundTool()
	registry := &mcpRegistryFake{bound: []MCPBoundTool{bound}, result: MCPCallResult{Text: "本地词条说明"}}
	model := &mcpModelFake{turns: []MCPModelTurn{{ToolCalls: []MCPProposedCall{{ID: "call-1", Name: modelToolAlias("lookup_course_term@campus-tools"), Arguments: json.RawMessage(`{"term":"MCP"}`)}}}, {Content: "已查到 MCP 的本地解释。"}}}
	orchestrator := &MCPOrchestrator{Registry: registry, Model: model}
	events := []MCPToolEvent{}
	textStarted := false
	answer, err := orchestrator.Run(context.Background(), repositories.User{ID: 1, Role: "student", ClassID: 1}, 42, 9, []ChatMessage{{Role: "system", Content: "safe instructions"}, {Role: "user", Content: "MCP是什么意思"}}, func(event MCPToolEvent) {
		if textStarted {
			t.Fatal("tool metadata arrived after answer text")
		}
		events = append(events, event)
	}, func(text string) error {
		textStarted = true
		if text != "已查到 MCP 的本地解释。" {
			t.Fatal("unexpected final answer")
		}
		return nil
	})
	if err != nil || answer != "已查到 MCP 的本地解释。" || registry.callCount != 1 || registry.auditCount != 1 || registry.statuses[0] != "success" || !model.sawToolData {
		t.Fatal("bounded tool orchestration did not complete with tool data in its own role")
	}
	if len(events) != 2 || events[0].Status != "running" || events[1].Status != "success" || !textStarted || model.toolsCounts[0] != 1 || model.toolsCounts[1] != 1 {
		t.Fatal("tool events were not emitted before text or candidate set was not bounded")
	}
}

func TestMCPOrchestratorRejectsUnboundAndInvalidCallsWithoutNetwork(t *testing.T) {
	for _, proposal := range []MCPProposedCall{{ID: "unknown", Name: "not-bound", Arguments: json.RawMessage(`{"term":"MCP"}`)}, {ID: "invalid", Name: modelToolAlias("lookup_course_term@campus-tools"), Arguments: json.RawMessage(`{"term":"MCP","extra":true}`)}} {
		registry := &mcpRegistryFake{bound: []MCPBoundTool{fakeBoundTool()}, result: MCPCallResult{Text: "unused"}}
		model := &mcpModelFake{turns: []MCPModelTurn{{ToolCalls: []MCPProposedCall{proposal}}, {Content: "我会按已有资料继续说明。"}}}
		orchestrator := &MCPOrchestrator{Registry: registry, Model: model}
		statuses := []string{}
		_, err := orchestrator.Run(context.Background(), repositories.User{ID: 1, Role: "student", ClassID: 1}, 42, 9, []ChatMessage{{Role: "system", Content: "rules"}, {Role: "user", Content: "question"}}, func(event MCPToolEvent) { statuses = append(statuses, event.Status) }, nil)
		if err != nil || registry.callCount != 0 {
			t.Fatal("unbound or schema-invalid call reached an MCP server")
		}
		if proposal.Name != "not-bound" && (registry.auditCount != 1 || registry.statuses[0] != "rejected") {
			t.Fatal("known invalid proposal was not safely audited")
		}
		if len(statuses) != 1 || statuses[0] != "rejected" {
			t.Fatal("rejected tool status was not visible")
		}
	}
}
