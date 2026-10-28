package mcpserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testToken = "local-mcp-token-for-tests-only-0123456789"

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	s, err := New(testToken)
	if err != nil {
		t.Fatal("MCP test service did not initialize")
	}
	return s.Handler()
}

func postRPC(t *testing.T, h http.Handler, token string, body string, protocol string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	r.Header.Set("Accept", "application/json, text/event-stream")
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if protocol != "" {
		r.Header.Set("MCP-Protocol-Version", protocol)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestInitializeDiscoversTwoToolsAndUsesAuth(t *testing.T) {
	h := newTestHandler(t)
	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"local-test","version":"1"}}}`
	bad := postRPC(t, h, "wrong-token", initialize, "")
	if bad.Code != http.StatusUnauthorized || strings.Contains(bad.Body.String(), testToken) {
		t.Fatal("invalid Bearer token was not refused safely")
	}
	init := postRPC(t, h, testToken, initialize, "")
	if init.Code != http.StatusOK || !strings.Contains(init.Body.String(), `"protocolVersion":"2025-11-25"`) || !strings.Contains(init.Body.String(), `"id":1`) {
		t.Fatalf("MCP initialize failed: %d %s", init.Code, init.Body.String())
	}
	list := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
	for i := 0; i < 2; i++ {
		got := postRPC(t, h, testToken, list, ProtocolVersion)
		var envelope struct {
			Result struct {
				Tools []Tool `json:"tools"`
			} `json:"result"`
		}
		if got.Code != http.StatusOK || json.Unmarshal(got.Body.Bytes(), &envelope) != nil || len(envelope.Result.Tools) != 2 {
			t.Fatalf("tools/list did not return exactly two deterministic tools: %d %s", got.Code, got.Body.String())
		}
		if envelope.Result.Tools[0].Name != "lookup_course_term" || envelope.Result.Tools[1].Name != "next_step_hint" || envelope.Result.Tools[0].InputSchema["type"] != "object" {
			t.Fatal("tool discovery order or input schemas changed")
		}
	}
	missingVersion := postRPC(t, h, testToken, list, "")
	if missingVersion.Code != http.StatusBadRequest {
		t.Fatal("post-initialize request without protocol header was accepted")
	}
}

func TestToolsCallReturnBoundedReadOnlyResultsAndErrors(t *testing.T) {
	h := newTestHandler(t)
	call := func(name string, args string) *httptest.ResponseRecorder {
		body := `{"jsonrpc":"2.0","id":"call-1","method":"tools/call","params":{"name":"` + name + `","arguments":` + args + `}}`
		return postRPC(t, h, testToken, body, ProtocolVersion)
	}
	lookup := call("lookup_course_term", `{"term":"MCP"}`)
	if lookup.Code != http.StatusOK || !strings.Contains(lookup.Body.String(), "统一协议") || !strings.Contains(lookup.Body.String(), `"isError":false`) {
		t.Fatalf("course glossary tool did not return local data: %d %s", lookup.Code, lookup.Body.String())
	}
	hint := call("next_step_hint", `{"topic":"authorization","observed_result":"forbidden"}`)
	if hint.Code != http.StatusOK || !strings.Contains(hint.Body.String(), "服务端") || strings.Contains(hint.Body.String(), "forbidden") {
		t.Fatalf("next-step tool echoed untrusted input or missed the fixed hint: %s", hint.Body.String())
	}
	badArgs := call("next_step_hint", `{"topic":"run-shell","extra":"value"}`)
	if !strings.Contains(badArgs.Body.String(), `"isError":true`) {
		t.Fatal("unsupported topic or extra fields were accepted")
	}
	unknown := call("not-a-tool", `{}`)
	if !strings.Contains(unknown.Body.String(), `"isError":true`) || strings.Contains(unknown.Body.String(), "token") {
		t.Fatal("unknown tool was not refused safely")
	}
}

func TestMCPHTTPMethodAndJSONRPCNotifications(t *testing.T) {
	h := newTestHandler(t)
	get := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	get.Header.Set("Accept", "application/json, text/event-stream")
	get.Header.Set("Authorization", "Bearer "+testToken)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, get)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatal("MCP endpoint accepted GET")
	}
	notification := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	got := postRPC(t, h, testToken, notification, ProtocolVersion)
	if got.Code != http.StatusAccepted || len(bytes.TrimSpace(got.Body.Bytes())) != 0 {
		t.Fatal("initialized notification did not receive HTTP 202")
	}
}

func TestMCPRequiresJSONAndRejectsUnexpectedArguments(t *testing.T) {
	h := newTestHandler(t)
	noContentType := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	noContentType.Header.Set("Accept", "application/json, text/event-stream")
	noContentType.Header.Set("Authorization", "Bearer "+testToken)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, noContentType)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatal("MCP endpoint accepted a request without JSON content type")
	}
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"next_step_hint","arguments":{"topic":"database","unexpected":"x"}}}`
	got := postRPC(t, h, testToken, body, ProtocolVersion)
	if !strings.Contains(got.Body.String(), `"isError":true`) {
		t.Fatal("MCP tool accepted an unexpected argument")
	}
}

func TestHealthEndpointIsReadOnlyAndMethodBound(t *testing.T) {
	h := newTestHandler(t)
	get := httptest.NewRecorder()
	h.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/health", nil))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"status":"ok"`) {
		t.Fatalf("health endpoint did not report ready: %d %s", get.Code, get.Body.String())
	}
	post := httptest.NewRecorder()
	h.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/health", nil))
	if post.Code != http.StatusMethodNotAllowed {
		t.Fatal("health endpoint accepted a non-GET method")
	}
}
