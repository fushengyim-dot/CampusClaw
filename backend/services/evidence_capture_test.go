package services

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func captureFixture(t *testing.T, handler http.HandlerFunc) (*evidenceRecorder, CaptureCase, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := CaptureCase{Name: "B", Question: "authorized question"}
	r := &evidenceRecorder{cfg: CaptureConfig{Directory: t.TempDir(), Cases: []CaptureCase{c}}, base: http.DefaultTransport, testOrigin: srv.URL}
	return r, c, srv
}
func captureRequest(t *testing.T, r *evidenceRecorder, c CaptureCase, url, body string) (*http.Response, error) {
	t.Helper()
	ctx := context.WithValue(context.Background(), captureKey{}, captureScope{r, c})
	req, _ := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer SECRET_TEST_NEVER_RECORD")
	req.Header.Set("Cookie", "SECRET_COOKIE")
	return (&http.Client{Transport: r}).Do(req)
}

const captureJSON = `{"model":"local","messages":[{"role":"system","content":"rules"},{"role":"user","content":"authorized question"}]}`

func TestCaptureJSONBudgetRestartAndSecrets(t *testing.T) {
	var calls atomic.Int32
	r, c, srv := captureFixture(t, func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		got, _ := io.ReadAll(req.Body)
		if string(got) != captureJSON {
			t.Error("wire body changed")
		}
		w.Header().Set("Set-Cookie", "SECRET_RESPONSE_COOKIE")
		io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	})
	for i := 0; i < 4; i++ {
		resp, e := captureRequest(t, r, c, srv.URL+"/v1/chat/completions", captureJSON)
		if e != nil {
			t.Fatal(e)
		}
		io.Copy(io.Discard, resp.Body)
		if e = resp.Body.Close(); e != nil {
			t.Fatal(e)
		}
	}
	// A fresh recorder must observe the same consumed ledger.
	restarted := &evidenceRecorder{cfg: r.cfg, base: r.base, testOrigin: srv.URL}
	if _, e := captureRequest(t, restarted, c, srv.URL+"/v1/chat/completions", captureJSON); e == nil {
		t.Fatal("fifth case B chat allowed")
	}
	if calls.Load() != 4 {
		t.Fatal(calls.Load())
	}
	filepath.Walk(r.cfg.Directory, func(p string, i os.FileInfo, e error) error {
		if e == nil && !i.IsDir() {
			b, _ := os.ReadFile(p)
			if bytes.Contains(b, []byte("SECRET")) {
				t.Errorf("secret in %s", p)
			}
		}
		return nil
	})
}
func TestCaptureSSEIncrementalAndExact(t *testing.T) {
	first := "data: {\"choices\":[{\"delta\":{\"content\":\"你好\"}}]}\n\n"
	last := "data: {\"choices\":[{\"delta\":{\"content\":\"世界\"}}]}\n\ndata: [DONE]\n\n"
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	r, c, srv := captureFixture(t, func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, first)
		w.(http.Flusher).Flush()
		<-release
		io.WriteString(w, last)
	})
	body := strings.TrimSuffix(captureJSON, "}") + `,"stream":true}`
	resp, e := captureRequest(t, r, c, srv.URL+"/v1/chat/completions", body)
	if e != nil {
		t.Fatal(e)
	}
	arrived := make(chan []byte, 1)
	go func() { buf := make([]byte, 2048); n, _ := resp.Body.Read(buf); arrived <- buf[:n] }()
	var got []byte
	select {
	case got = <-arrived:
	case <-time.After(time.Second):
		once.Do(func() { close(release) })
		t.Fatal("stream buffered until completion")
	}
	once.Do(func() { close(release) })
	tail, e := io.ReadAll(resp.Body)
	if e != nil {
		t.Fatal(e)
	}
	got = append(got, tail...)
	if e = resp.Body.Close(); e != nil {
		t.Fatal(e)
	}
	saved, _ := os.ReadFile(filepath.Join(r.cfg.Directory, "B-chat-1", "response.raw"))
	if string(got) != first+last || !bytes.Equal(got, saved) {
		t.Fatal("SSE bytes changed")
	}
	meta, _ := os.ReadFile(filepath.Join(r.cfg.Directory, "B-chat-1", "complete.json"))
	var m map[string]any
	json.Unmarshal(meta, &m)
	if m["delta_count"] != float64(2) || m["sse_done"] != true {
		t.Fatal(string(meta))
	}
}
func TestCaptureIncompleteAndWriteFailureDoNotSendAgain(t *testing.T) {
	var calls atomic.Int32
	r, c, srv := captureFixture(t, func(w http.ResponseWriter, req *http.Request) { calls.Add(1); io.WriteString(w, "data: {}\n\n") })
	resp, e := captureRequest(t, r, c, srv.URL+"/v1/chat/completions", strings.TrimSuffix(captureJSON, "}")+`,"stream":true}`)
	if e != nil {
		t.Fatal(e)
	}
	io.Copy(io.Discard, resp.Body)
	if resp.Body.Close() == nil {
		t.Fatal("missing DONE accepted")
	}
	if _, e = captureRequest(t, r, c, srv.URL+"/v1/chat/completions", captureJSON); e == nil || calls.Load() != 1 {
		t.Fatal("failed case retried")
	}
	blocked := filepath.Join(t.TempDir(), "file")
	os.WriteFile(blocked, []byte("not directory"), 0600)
	r.cfg.Directory = blocked
	if _, e = captureRequest(t, r, c, srv.URL+"/v1/chat/completions", captureJSON); e == nil || calls.Load() != 1 {
		t.Fatal("write failure sent")
	}
}
func TestCaptureEmbeddingAndScope(t *testing.T) {
	var calls atomic.Int32
	r, c, srv := captureFixture(t, func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		io.WriteString(w, `{"data":[{"embedding":[1,2]}]}`)
	})
	for _, name := range []string{"A", "B"} {
		c.Name = name
		resp, e := captureRequest(t, r, c, srv.URL+"/v1/embeddings", `{"model":"local","input":"authorized question"}`)
		if e != nil {
			t.Fatal(e)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	if _, e := captureRequest(t, r, c, srv.URL+"/v1/embeddings", `{"model":"local","input":"authorized question"}`); e == nil {
		t.Fatal("third embedding allowed")
	}
	req, _ := http.NewRequest("POST", srv.URL+"/v1/chat/completions", strings.NewReader(captureJSON))
	if _, e := r.RoundTrip(req); e == nil {
		t.Fatal("unscoped allowed")
	}
	if calls.Load() != 2 {
		t.Fatal(calls.Load())
	}
}
func TestCaptureToolMessagesAndRawResults(t *testing.T) {
	r, c, srv := captureFixture(t, func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		if !bytes.Contains(b, []byte(`"tool_call_id":"call1"`)) {
			t.Error("tool link lost")
		}
		io.WriteString(w, `{"choices":[{"message":{"tool_calls":[{"id":"call2","type":"function","function":{"name":"next_step_hint","arguments":"{}"}}]}}]}`)
	})
	// Install local recorder for helper hooks only; no external endpoint is used.
	previous := activeCapture()
	captureInstance = r
	t.Cleanup(func() { captureInstance = previous })
	ctx := context.WithValue(context.Background(), captureKey{}, captureScope{r, c})
	for i := 0; i < 4; i++ {
		dir, e := CaptureToolStart(ctx, "lookup_course_term", map[string]any{"term": "认证"})
		if e != nil {
			t.Fatal(e)
		}
		if e = CaptureToolEnd(ctx, dir, MCPCallResult{Text: strings.Repeat("认证", 200)}, nil); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := CaptureToolStart(ctx, "next_step_hint", map[string]any{}); e == nil {
		t.Fatal("fifth tool allowed")
	}
	body := strings.TrimSuffix(captureJSON, "]}") + `,{"role":"assistant","tool_calls":[{"id":"call1","type":"function","function":{"name":"lookup_course_term","arguments":"{}"}}]},{"role":"tool","name":"lookup_course_term","tool_call_id":"call1","content":"term result"}]}`
	resp, e := captureRequest(t, r, c, srv.URL+"/v1/chat/completions", body)
	if e != nil {
		t.Fatal(e)
	}
	io.Copy(io.Discard, resp.Body)
	if e = resp.Body.Close(); e != nil {
		t.Fatal(e)
	}
}

func TestCaptureScopeAndDisabled(t *testing.T) {
	previous := activeCapture()
	defer func() { captureInstance = previous }()
	captureInstance = nil
	ctx := context.Background()
	if _, e := CaptureConversation(ctx, 0, 0, 0, "", Agent{}, "", "", nil, nil, nil); e != nil {
		t.Fatal(e)
	}
	if captureTransport() != http.DefaultTransport {
		t.Fatal("disabled capture changed transport")
	}
	c := CaptureCase{MaterialID: 42, Name: "A", UserID: 5, ClassID: 1, ConversationID: 85, AgentID: 4, Question: "q", Template: "explain", Prompt: "p"}
	captureInstance = &evidenceRecorder{cfg: CaptureConfig{Directory: t.TempDir(), Cases: []CaptureCase{c}}}
	call := func(history []ChatMessage, role string) error {
		_, e := CaptureConversation(ctx, 5, 1, 85, role, Agent{ID: 4, Prompt: "p"}, "q", "explain", []int64{42}, nil, history)
		return e
	}
	if e := call([]ChatMessage{{Role: "user", Content: "q"}}, "teacher"); e != nil {
		t.Fatal(e)
	}
	if call([]ChatMessage{{Role: "user", Content: "old"}, {Role: "user", Content: "q"}}, "teacher") == nil {
		t.Fatal("history allowed")
	}
	if call([]ChatMessage{{Role: "user", Content: "q"}}, "student") == nil {
		t.Fatal("wrong role allowed")
	}
}
func TestCaptureTimeoutIsIncomplete(t *testing.T) {
	r, c, srv := captureFixture(t, func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {}\n\n")
		w.(http.Flusher).Flush()
		<-req.Context().Done()
	})
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), captureKey{}, captureScope{r, c}), 100*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", srv.URL+"/v1/chat/completions", strings.NewReader(strings.TrimSuffix(captureJSON, "}")+`,"stream":true}`))
	resp, e := (&http.Client{Transport: r}).Do(req)
	if e != nil {
		t.Fatal(e)
	}
	io.ReadAll(resp.Body)
	if resp.Body.Close() == nil {
		t.Fatal("timeout complete")
	}
	if _, e = os.Stat(filepath.Join(r.cfg.Directory, "B-chat-1", "incomplete.json")); e != nil {
		t.Fatal(e)
	}
}

func TestCaptureConfiguredToolEncodingLocal(t *testing.T) {
	r, c, srv := captureFixture(t, func(w http.ResponseWriter, req *http.Request) {
		io.WriteString(w, `{"choices":[{"message":{"content":"local verified"}}]}`)
	})
	c.Prompt = "local rules"
	wire := `[{"function":{"description":"local fixture","name":"fixture_tool","parameters":{"type":"object"}},"type":"function"}]`
	c.ToolDefinitionsSHA = digest([]byte(wire))
	defs := []MCPToolDefinition{{Name: "fixture_tool", Description: "local fixture", InputSchema: json.RawMessage(`{"type":"object"}`)}}
	client := &ChatClient{endpoint: srv.URL + "/v1/chat/completions", apiKey: "SECRET_TEST", model: "local", http: &http.Client{Transport: r}}
	ctx := context.WithValue(context.Background(), captureKey{}, captureScope{r, c})
	turn, e := client.CompleteWithTools(ctx, []MCPModelMessage{{Role: "system", Content: c.Prompt}, {Role: "user", Content: c.Question}}, defs)
	if e != nil || turn.Content != "local verified" {
		t.Fatalf("encoding preflight failed: %v", e)
	}
	defs[0].Description = "changed definition"
	if _, e = client.CompleteWithTools(ctx, []MCPModelMessage{{Role: "system", Content: c.Prompt}, {Role: "user", Content: c.Question}}, defs); e == nil {
		t.Fatal("changed tool definition allowed")
	}
}

func TestCaptureErrorBodyWithoutCallerRead(t *testing.T) {
	r, c, srv := captureFixture(t, func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(429)
		io.WriteString(w, `{"error":{"message":"local rate limit"}}`)
	})
	resp, e := captureRequest(t, r, c, srv.URL+"/v1/chat/completions", captureJSON)
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	raw, e := os.ReadFile(filepath.Join(r.cfg.Directory, "B-chat-1", "response.raw"))
	if e != nil || string(raw) != `{"error":{"message":"local rate limit"}}` {
		t.Fatalf("missing error body: %v", e)
	}
	if _, e = captureRequest(t, r, c, srv.URL+"/v1/chat/completions", captureJSON); e == nil {
		t.Fatal("failed status retried")
	}
}
func TestCaptureConfiguredMaterialFingerprint(t *testing.T) {
	previous := activeCapture()
	defer func() { captureInstance = previous }()
	r := &evidenceRecorder{}
	captureInstance = r
	c := CaptureCase{MaterialID: 101, ChunkID: 202, ExcerptChars: 2, ExcerptSHA: digest([]byte("资料"))}
	ctx := context.WithValue(context.Background(), captureKey{}, captureScope{r, c})
	hits := []KnowledgeHit{{MaterialID: 101, ChunkID: 202, Excerpt: "资料"}}
	if e := CaptureCitations(ctx, hits); e != nil {
		t.Fatal(e)
	}
	hits[0].Excerpt = "其他"
	if CaptureCitations(ctx, hits) == nil {
		t.Fatal("different material sent")
	}
}

func TestCaptureConfigurationValidation(t *testing.T) {
	previous := activeCapture()
	t.Cleanup(func() { captureInstance = previous; captureOnce = sync.Once{}; captureOnce.Do(func() {}) })
	configPath := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("CAMPUSCLAW_CAPTURE_CONFIG", configPath)
	a := CaptureCase{Name: "A", UserID: 1, ClassID: 2, ConversationID: 100, AgentID: 3, Question: "local question", Prompt: "local prompt", MaterialID: 101, ChunkID: 202, ExcerptChars: 2, ExcerptSHA: digest([]byte("资料"))}
	b := a
	b.Name = "B"
	b.ConversationID = 200
	b.SkillName = "local skill"
	b.SkillDescription = "local description"
	b.ToolDefinitionsSHA = digest([]byte("[]"))
	valid := CaptureConfig{Directory: t.TempDir(), Cases: []CaptureCase{a, b}}
	check := func(cfg CaptureConfig, wantError bool) {
		t.Helper()
		data, _ := json.Marshal(cfg)
		if e := os.WriteFile(configPath, data, 0600); e != nil {
			t.Fatal(e)
		}
		captureOnce = sync.Once{}
		captureInstance = nil
		r := activeCapture()
		if r == nil || (r.err != nil) != wantError {
			t.Fatalf("validation error=%v wanted=%v", r, wantError)
		}
	}
	check(valid, false)
	broken := valid
	broken.Cases = append([]CaptureCase(nil), valid.Cases...)
	broken.Cases[1].ConversationID = 100
	check(broken, true)
	broken.Cases[1] = b
	broken.Cases[0].ExcerptSHA = "invalid"
	check(broken, true)
	broken.Cases[0] = a
	broken.Cases[1].ToolDefinitionsSHA = ""
	check(broken, true)
}
