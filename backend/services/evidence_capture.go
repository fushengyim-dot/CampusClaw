package services

// Opt-in evidence capture for explicitly authorized acceptance runs. No headers
// carrying credentials are serialized. Attempt directories are a durable ledger.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var ErrCapture = errors.New("本轮取证范围、额度或记录检查未通过")

type CaptureCase struct {
	MaterialID, ChunkID                                                         int64
	ExcerptChars                                                                int
	ExcerptSHA                                                                  string
	Name                                                                        string
	UserID, ClassID, ConversationID, AgentID                                    int64
	Question, Template, Prompt, SkillName, SkillDescription, ToolDefinitionsSHA string
}
type CaptureConfig struct {
	Directory string
	Cases     []CaptureCase
}
type evidenceRecorder struct {
	cfg        CaptureConfig
	err        error
	base       http.RoundTripper
	testOrigin string
}
type captureScope struct {
	recorder *evidenceRecorder
	c        CaptureCase
}
type captureKey struct{}

var captureOnce sync.Once
var captureInstance *evidenceRecorder

func activeCapture() *evidenceRecorder {
	captureOnce.Do(func() {
		name := os.Getenv("CAMPUSCLAW_CAPTURE_CONFIG")
		if name == "" {
			return
		}
		r := &evidenceRecorder{base: http.DefaultTransport}
		captureInstance = r
		b, e := os.ReadFile(name)
		if e != nil || json.Unmarshal(b, &r.cfg) != nil || !filepath.IsAbs(r.cfg.Directory) || len(r.cfg.Cases) != 2 {
			r.err = ErrCapture
			return
		}
		seen := map[string]bool{}
		conversations := map[int64]bool{}
		for _, c := range r.cfg.Cases {
			if (c.Name != "A" && c.Name != "B") || seen[c.Name] || c.UserID < 1 || c.ClassID < 1 || c.ConversationID < 1 || c.AgentID < 1 || c.Question == "" || c.Prompt == "" {
				r.err = ErrCapture
			}
			fingerprint, hashErr := hex.DecodeString(c.ExcerptSHA)
			if c.MaterialID < 1 || c.ChunkID < 1 || c.ExcerptChars < 1 || c.ExcerptChars > 800 || hashErr != nil || len(fingerprint) != 32 || conversations[c.ConversationID] {
				r.err = ErrCapture
			}
			if c.Name == "B" {
				toolHash, toolErr := hex.DecodeString(c.ToolDefinitionsSHA)
				if c.SkillName == "" || c.SkillDescription == "" || toolErr != nil || len(toolHash) != 32 {
					r.err = ErrCapture
				}
			}
			conversations[c.ConversationID] = true
			seen[c.Name] = true
		}
		if os.MkdirAll(r.cfg.Directory, 0700) != nil {
			r.err = ErrCapture
		}
	})
	return captureInstance
}
func captureTransport() http.RoundTripper {
	if r := activeCapture(); r != nil {
		return r
	}
	return http.DefaultTransport
}
func CaptureConversation(ctx context.Context, userID, classID, cid int64, role string, a Agent, question, template string, materials []int64, skill *RoutedSkill, history []ChatMessage) (context.Context, error) {
	r := activeCapture()
	if r == nil {
		return ctx, nil
	}
	if r.err != nil {
		return ctx, ErrCapture
	}
	for _, c := range r.cfg.Cases {
		if c.UserID != userID || c.ClassID != classID || c.ConversationID != cid {
			continue
		}
		if role != "teacher" || a.ID != c.AgentID || a.Prompt != c.Prompt || question != c.Question || template != c.Template || len(materials) != 1 || materials[0] != c.MaterialID || len(history) != 1 || history[0].Role != "user" || history[0].Content != question {
			return ctx, ErrCapture
		}
		if c.Name == "A" && skill != nil {
			return ctx, ErrCapture
		}
		if c.Name == "B" && (skill == nil || skill.Name != c.SkillName || skill.Description != c.SkillDescription || skill.SubjectText != "") {
			return ctx, ErrCapture
		}
		return context.WithValue(ctx, captureKey{}, captureScope{r, c}), nil
	}
	return ctx, ErrCapture
}
func CaptureCitations(ctx context.Context, hits []KnowledgeHit) error {
	if activeCapture() == nil {
		return nil
	}
	scope, ok := ctx.Value(captureKey{}).(captureScope)
	if !ok {
		return ErrCapture
	}
	if len(hits) != 1 || hits[0].MaterialID != scope.c.MaterialID || hits[0].ChunkID != scope.c.ChunkID || len([]rune(hits[0].Excerpt)) != scope.c.ExcerptChars || digest([]byte(hits[0].Excerpt)) != scope.c.ExcerptSHA {
		return ErrCapture
	}
	return nil
}
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func writeEvidence(path string, b []byte) error {
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return ErrCapture
	}
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil || ce != nil {
		return ErrCapture
	}
	return nil
}
func writeJSONEvidence(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return ErrCapture
	}
	return writeEvidence(path, b)
}
func (r *evidenceRecorder) reserve(c CaptureCase, kind string) (string, error) {
	if r.err != nil {
		return "", ErrCapture
	}
	if _, e := os.Stat(filepath.Join(r.cfg.Directory, c.Name+"-failed")); !os.IsNotExist(e) {
		return "", ErrCapture
	}
	// Exclusive directory claims serialize across processes and survive restarts.
	limit := 1
	if c.Name == "B" && (kind == "chat" || kind == "tool") {
		limit = 4
	}
	if kind != "chat" && kind != "embedding" && kind != "tool" {
		return "", ErrCapture
	}
	if kind == "tool" && c.Name != "B" {
		return "", ErrCapture
	}
	for i := 1; i <= limit; i++ {
		dir := filepath.Join(r.cfg.Directory, c.Name+"-"+kind+"-"+string(rune('0'+i)))
		e := os.Mkdir(dir, 0700)
		if os.IsExist(e) {
			// A preceding incomplete attempt may not be followed by another request.
			if _, done := os.Stat(filepath.Join(dir, "complete.json")); done != nil {
				return "", ErrCapture
			}
			continue
		}
		if e != nil {
			return "", ErrCapture
		}
		return dir, nil
	}
	return "", ErrCapture
}
func (r *evidenceRecorder) fail(c CaptureCase) {
	_ = writeEvidence(filepath.Join(r.cfg.Directory, c.Name+"-failed"), []byte("incomplete or failed; no retry\n"))
}
func (r *evidenceRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	scope, ok := req.Context().Value(captureKey{}).(captureScope)
	if r.err != nil || !ok || scope.recorder != r || req.Method != "POST" {
		return nil, ErrCapture
	}
	origin := req.URL.Scheme + "://" + req.URL.Host
	if origin != "https://ai-gateway.devops.hello1023.com" && (r.testOrigin == "" || origin != r.testOrigin) {
		return nil, ErrCapture
	}
	kind := "chat"
	if req.URL.Path == "/v1/embeddings" {
		kind = "embedding"
	} else if req.URL.Path != "/v1/chat/completions" {
		return nil, ErrCapture
	}
	body, e := io.ReadAll(io.LimitReader(req.Body, 2*1024*1024+1))
	req.Body.Close()
	if e != nil || len(body) > 2*1024*1024 {
		return nil, ErrCapture
	}
	var payload map[string]json.RawMessage
	if json.Unmarshal(body, &payload) != nil {
		return nil, ErrCapture
	}
	if kind == "embedding" {
		var input string
		if json.Unmarshal(payload["input"], &input) != nil || input != scope.c.Question {
			return nil, ErrCapture
		}
	}
	if kind == "chat" {
		var messages []MCPModelMessage
		if json.Unmarshal(payload["messages"], &messages) != nil || len(messages) < 2 || messages[0].Role != "system" || messages[1].Role != "user" || !strings.Contains(messages[1].Content, scope.c.Question) {
			return nil, ErrCapture
		}
		if scope.c.Name == "A" && (len(messages) != 2 || len(payload["tools"]) > 0) {
			return nil, ErrCapture
		}
		if len(payload["tools"]) > 0 {
			var compact bytes.Buffer
			if json.Compact(&compact, payload["tools"]) != nil || digest(compact.Bytes()) != scope.c.ToolDefinitionsSHA {
				return nil, ErrCapture
			}
		}
	}
	dir, e := r.reserve(scope.c, kind)
	if e != nil {
		return nil, e
	}
	if writeEvidence(filepath.Join(dir, "request.json"), body) != nil || writeJSONEvidence(filepath.Join(dir, "request-meta.json"), map[string]any{"time": time.Now().UTC(), "url": req.URL.String(), "method": req.Method, "sha256": digest(body), "case": scope.c.Name, "conversation_id": scope.c.ConversationID, "content_type": req.Header.Get("Content-Type"), "accept": req.Header.Get("Accept")}) != nil {
		r.fail(scope.c)
		return nil, ErrCapture
	}
	f, e := os.OpenFile(filepath.Join(dir, "response.raw"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		r.fail(scope.c)
		return nil, ErrCapture
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.GetBody = nil
	resp, e := r.base.RoundTrip(req)
	if e != nil {
		f.Close()
		r.fail(scope.c)
		_ = writeJSONEvidence(filepath.Join(dir, "incomplete.json"), map[string]any{"reason": "transport error", "time": time.Now().UTC()})
		return nil, ErrCapture
	}
	stream := bytes.Equal(payload["stream"], []byte("true"))
	recorded := &captureBody{ReadCloser: resp.Body, file: f, dir: dir, recorder: r, c: scope.c, status: resp.StatusCode, stream: stream}
	resp.Body = recorded
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Callers can reject status without reading Body. Preserve the bounded error
		// response before returning, while retaining a readable body for the caller.
		_, _ = io.Copy(io.Discard, recorded)
		_ = recorded.Close()
		resp.Body = io.NopCloser(bytes.NewReader(recorded.raw.Bytes()))
	}
	return resp, nil
}

type captureBody struct {
	io.ReadCloser
	file                        *os.File
	dir                         string
	recorder                    *evidenceRecorder
	c                           CaptureCase
	status                      int
	stream, eof, failed, closed bool
	raw                         bytes.Buffer
	timing                      []map[string]any
}

func (b *captureBody) Read(p []byte) (int, error) {
	n, e := b.ReadCloser.Read(p)
	if n > 0 {
		if b.raw.Len()+n > 8*1024*1024 {
			b.failed = true
			return 0, ErrCapture
		}
		b.raw.Write(p[:n])
		b.timing = append(b.timing, map[string]any{"time": time.Now().UTC(), "bytes": n})
		if _, we := b.file.Write(p[:n]); we != nil {
			b.failed = true
			return 0, ErrCapture
		}
	}
	if e == io.EOF {
		b.eof = true
	} else if e != nil {
		b.failed = true
	}
	return n, e
}
func (b *captureBody) Close() error {
	if b.closed {
		return nil
	}
	b.closed = true
	e := b.ReadCloser.Close()
	if e != nil {
		b.failed = true
	}
	if b.file.Sync() != nil {
		b.failed = true
	}
	if b.file.Close() != nil {
		b.failed = true
	}
	done := false
	deltas := 0
	if b.stream {
		for _, line := range bytes.Split(b.raw.Bytes(), []byte("\n")) {
			line = bytes.TrimSpace(line)
			if bytes.Equal(line, []byte("data: [DONE]")) {
				done = true
			}
			if bytes.HasPrefix(line, []byte("data:")) && !bytes.Equal(line, []byte("data: [DONE]")) {
				var v struct {
					Choices []struct{ Delta struct{ Content string } }
				}
				if json.Unmarshal(bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:"))), &v) == nil {
					for _, ch := range v.Choices {
						if ch.Delta.Content != "" {
							deltas++
						}
					}
				}
			}
		}
	}
	complete := !b.failed && b.status >= 200 && b.status < 300 && ((b.stream && done) || (!b.stream && b.eof))
	name := "incomplete.json"
	if complete {
		name = "complete.json"
	}
	if writeJSONEvidence(filepath.Join(b.dir, name), map[string]any{"status": b.status, "complete": complete, "eof": b.eof, "sse_done": done, "delta_count": deltas, "response_sha256": digest(b.raw.Bytes()), "chunks": b.timing}) != nil {
		complete = false
	}
	if !complete {
		b.recorder.fail(b.c)
		return ErrCapture
	}
	return e
}
func CaptureToolStart(ctx context.Context, name string, args map[string]any) (string, error) {
	r := activeCapture()
	if r == nil {
		return "", nil
	}
	s, ok := ctx.Value(captureKey{}).(captureScope)
	if !ok || s.recorder != r || (name != "lookup_course_term" && name != "next_step_hint") {
		return "", ErrCapture
	}
	dir, e := r.reserve(s.c, "tool")
	if e != nil {
		return "", e
	}
	if writeJSONEvidence(filepath.Join(dir, "request.json"), map[string]any{"name": name, "arguments": args, "time": time.Now().UTC()}) != nil {
		r.fail(s.c)
		return "", ErrCapture
	}
	return dir, nil
}
func CaptureToolEnd(ctx context.Context, dir string, result MCPCallResult, callErr error) error {
	if dir == "" {
		return nil
	}
	s, ok := ctx.Value(captureKey{}).(captureScope)
	if !ok {
		return ErrCapture
	}
	if writeJSONEvidence(filepath.Join(dir, "response.json"), result) != nil || callErr != nil || result.IsError {
		s.recorder.fail(s.c)
		return ErrCapture
	}
	if writeJSONEvidence(filepath.Join(dir, "complete.json"), map[string]any{"complete": true, "time": time.Now().UTC()}) != nil {
		s.recorder.fail(s.c)
		return ErrCapture
	}
	return nil
}
