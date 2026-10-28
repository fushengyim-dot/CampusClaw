package database

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"campusclaw/backend/repositories"
	"campusclaw/backend/services"
	"github.com/go-sql-driver/mysql"
)

func TestMCPRegistryClassIsolationCiphertextAndDisconnect(t *testing.T) {
	if os.Getenv("CAMPUSCLAW_SCHEMA_INTEGRATION") != "1" {
		t.Skip("explicit schema integration required")
	}
	cfg := integrationConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	db, err := Open(ctx, cfg)
	if err != nil {
		t.Fatal("database unavailable")
	}
	defer db.Close()
	if err = InitSchema(ctx, db); err != nil {
		t.Fatal("schema migration unavailable")
	}
	cleanupPriorMCPRegistryFixtures(t, ctx, db)
	var nonce [8]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		t.Fatal("fixture id unavailable")
	}
	tag := "mcp-registry-check-" + hex.EncodeToString(nonce[:])
	skillName := tag[:32]
	conversationBase := int64(9_000_000_000_000_000_000) + int64(binary.BigEndian.Uint64(nonce[:])%1_000_000)
	insertClass := func(name string) int64 {
		t.Helper()
		res, e := db.ExecContext(ctx, `INSERT INTO classes(name) VALUES(?)`, tag+name)
		if e != nil {
			t.Fatalf("synthetic class insert failed: %T", e)
		}
		id, _ := res.LastInsertId()
		return id
	}
	classA, classB := insertClass("-A"), insertClass("-B")
	insertUser := func(name string, classID int64, role string) repositories.User {
		t.Helper()
		res, e := db.ExecContext(ctx, `INSERT INTO users(username,password_hash,role,class_id) VALUES(?,?,?,?)`, tag+name, "!disabled-"+tag, role, classID)
		if e != nil {
			t.Fatal("synthetic disabled user insert failed")
		}
		id, _ := res.LastInsertId()
		return repositories.User{ID: id, Username: tag + name, Role: role, ClassID: classID}
	}
	teacherA := insertUser("-teacherA", classA, "teacher")
	teacherB := insertUser("-teacherB", classB, "teacher")
	studentA := insertUser("-studentA", classA, "student")
	defer func() {
		cleanCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, e := db.ExecContext(cleanCtx, `DELETE FROM mcp_tool_calls WHERE class_id=? AND conversation_id IN (?,?)`, classA, conversationBase+1, conversationBase+2); e != nil {
			t.Error("synthetic MCP audit cleanup failed")
		}
		if _, e := db.ExecContext(cleanCtx, `DELETE FROM mcp_servers WHERE class_id=? AND name=?`, classA, tag+"-server"); e != nil {
			t.Error("synthetic MCP server cleanup failed")
		}
		for _, query := range []string{`DELETE FROM conversation_messages WHERE class_id IN (?,?)`, `DELETE FROM conversations WHERE class_id IN (?,?)`, `DELETE FROM course_agent_materials WHERE class_id IN (?,?)`, `DELETE FROM course_agent_skills WHERE class_id IN (?,?)`, `DELETE FROM course_builtin_agents WHERE class_id IN (?,?)`, `DELETE FROM course_agents WHERE class_id IN (?,?)`, `DELETE FROM knowledge_entries WHERE class_id IN (?,?)`, `DELETE FROM materials WHERE class_id IN (?,?)`, `DELETE FROM sessions WHERE user_id IN (SELECT id FROM users WHERE class_id IN (?,?))`, `DELETE FROM assignments WHERE class_id IN (?,?)`, `DELETE FROM tutor_agents WHERE class_id IN (?,?)`, `DELETE FROM skills WHERE class_id IN (?,?)`} {
			if _, e := db.ExecContext(cleanCtx, query, classA, classB); e != nil {
				t.Error("synthetic class-dependent fixture cleanup failed")
			}
		}
		if _, e := db.ExecContext(cleanCtx, `DELETE FROM course_skills WHERE class_id=? AND name=?`, classA, skillName); e != nil {
			t.Error("synthetic skill cleanup failed")
		}
		for _, u := range []repositories.User{teacherA, teacherB, studentA} {
			if _, e := db.ExecContext(cleanCtx, `DELETE FROM users WHERE id=? AND class_id=?`, u.ID, u.ClassID); e != nil {
				t.Error("synthetic user cleanup failed")
			}
		}
		for _, id := range []int64{classA, classB} {
			if _, e := db.ExecContext(cleanCtx, `DELETE FROM classes WHERE id=?`, id); e != nil {
				t.Error("synthetic class cleanup failed")
			}
		}
	}()
	registry, err := services.NewMCPRegistry(db, strings.Repeat("session-test-secret-", 3))
	if err != nil {
		t.Fatal("registry initialization failed")
	}
	const token = "synthetic-mcp-registration-secret-123456"
	serverID, err := registry.Save(ctx, teacherA, 0, tag+"-server", "http://mcp-tools:8091/mcp", token)
	if err != nil {
		t.Fatal("teacher could not register the allowed local server")
	}
	if _, err = registry.Save(ctx, studentA, 0, "student-server", "http://mcp-tools:8091/mcp", token); err == nil {
		t.Fatal("student registered a server")
	}
	if _, err = registry.Save(ctx, teacherA, 0, "unsafe-server", "http://127.0.0.1:8091/mcp", token); err == nil {
		t.Fatal("loopback endpoint passed server URL policy")
	}
	if registry.Exists(ctx, classB, serverID) || registry.Exists(ctx, classA+100000, serverID) {
		t.Fatal("server crossed class boundary")
	}
	if _, err = registry.Save(ctx, teacherB, serverID, "foreign-edit", "http://mcp-tools:8091/mcp", ""); err == nil {
		t.Fatal("teacher modified another class server")
	}
	var ciphertext []byte
	if db.QueryRowContext(ctx, `SELECT token_ciphertext FROM mcp_servers WHERE id=? AND class_id=?`, serverID, classA).Scan(&ciphertext) != nil || strings.Contains(string(ciphertext), token) {
		t.Fatal("database stored or exposed the token in plaintext")
	}
	listed, err := registry.List(ctx, classA)
	if err != nil || len(listed) != 1 || !listed[0].TokenConfigured || listed[0].Connected {
		t.Fatal("teacher server response did not expose only configuration status")
	}
	other, err := registry.List(ctx, classB)
	if err != nil || len(other) != 0 {
		t.Fatal("other class listed this server")
	}
	res, err := db.ExecContext(ctx, `INSERT INTO course_skills(class_id,name,trigger_text,tools_json,enabled,description,source_text) VALUES(?,?,'tool','[]',1,'synthetic','synthetic')`, classA, skillName)
	if err != nil {
		t.Fatal("synthetic skill creation failed")
	}
	skillID, _ := res.LastInsertId()
	if _, err = db.ExecContext(ctx, `INSERT INTO mcp_server_tools(server_id,class_id,tool_name,description,input_schema,schema_hash) VALUES(?,?,?,'synthetic',JSON_OBJECT('type','object'),?)`, serverID, classA, "safe_tool", strings.Repeat("a", 64)); err != nil {
		t.Fatal("synthetic discovered tool insert failed")
	}
	if _, err = db.ExecContext(ctx, `UPDATE mcp_servers SET connected=1 WHERE id=? AND class_id=?`, serverID, classA); err != nil {
		t.Fatal("synthetic server connect state setup failed")
	}
	if err = registry.BindSkillTools(ctx, teacherA, skillID, []string{"safe_tool@" + tag + "-server"}); err != nil {
		t.Fatal("teacher could not bind a discovered same-class tool")
	}
	if err = registry.BindSkillTools(ctx, teacherA, skillID, []string{"unknown@" + tag + "-server"}); err == nil {
		t.Fatal("teacher bound a tool missing from the latest snapshot")
	}
	bound, err := registry.BoundTools(ctx, classA, skillID)
	if err != nil || len(bound) != 1 || bound[0].Tool.Name != "safe_tool" {
		t.Fatal("saved skill binding did not resolve to the current schema")
	}
	if err = registry.BindSkillTools(ctx, studentA, skillID, []string{"safe_tool@" + tag + "-server"}); err == nil {
		t.Fatal("student changed skill tool bindings")
	}
	if err = registry.RecordCall(ctx, teacherA, conversationBase+1, 1, bound[0], "success", token); err != nil {
		t.Fatal("teacher tool audit did not save")
	}
	if err = registry.RecordCall(ctx, studentA, conversationBase+2, 1, bound[0], "error", token); err != nil {
		t.Fatal("student tool audit did not save")
	}
	teacherHistory, err := registry.CallHistory(ctx, teacherA, 50)
	if err != nil || len(teacherHistory) != 2 {
		t.Fatal("teacher could not see class tool history")
	}
	studentHistory, err := registry.CallHistory(ctx, studentA, 50)
	if err != nil || len(studentHistory) != 1 || studentHistory[0].Summary != "工具执行失败" {
		t.Fatal("student history was not limited to the triggering user or safe status summary")
	}
	otherHistory, err := registry.CallHistory(ctx, teacherB, 50)
	if err != nil || len(otherHistory) != 0 {
		t.Fatal("another class saw this class tool history")
	}
	if err = registry.Disconnect(ctx, classA, serverID); err != nil {
		t.Fatal("teacher could not disconnect the server")
	}
	var connected int
	var toolCount int
	if db.QueryRowContext(ctx, `SELECT connected FROM mcp_servers WHERE id=?`, serverID).Scan(&connected) != nil || connected != 0 || db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mcp_server_tools WHERE server_id=?`, serverID).Scan(&toolCount) != nil || toolCount != 0 {
		t.Fatal("disconnect retained a callable discovery snapshot")
	}
	if !registry.Exists(ctx, classA, serverID) {
		t.Fatal("disconnect unexpectedly deleted the server record")
	}
	bound, err = registry.BoundTools(ctx, classA, skillID)
	if err != nil || len(bound) != 0 {
		t.Fatal("disconnected tool remained callable through a stale binding")
	}
	teacherHistory, err = registry.CallHistory(ctx, teacherA, 50)
	if err != nil || len(teacherHistory) != 2 {
		t.Fatal("disconnect removed historical audit records")
	}
	t.Log("CHECK: class-scoped server access, teacher-only registration, encrypted token, safe response, and disconnect snapshot clearing passed")
}

func cleanupPriorMCPRegistryFixtures(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for i, query := range []string{
		`DELETE FROM mcp_tool_calls WHERE class_id IN (SELECT id FROM classes WHERE name LIKE 'mcp-registry-check-%')`,
		`DELETE FROM mcp_servers WHERE class_id IN (SELECT id FROM classes WHERE name LIKE 'mcp-registry-check-%')`,
		`DELETE FROM conversation_messages WHERE class_id IN (SELECT id FROM classes WHERE name LIKE 'mcp-registry-check-%')`,
		`DELETE FROM conversations WHERE class_id IN (SELECT id FROM classes WHERE name LIKE 'mcp-registry-check-%')`,
		`DELETE FROM course_agent_materials WHERE class_id IN (SELECT id FROM classes WHERE name LIKE 'mcp-registry-check-%')`,
		`DELETE FROM course_agent_skills WHERE class_id IN (SELECT id FROM classes WHERE name LIKE 'mcp-registry-check-%')`,
		`DELETE FROM course_builtin_agents WHERE class_id IN (SELECT id FROM classes WHERE name LIKE 'mcp-registry-check-%')`,
		`DELETE FROM course_agents WHERE class_id IN (SELECT id FROM classes WHERE name LIKE 'mcp-registry-check-%')`,
		`DELETE FROM course_skills WHERE class_id IN (SELECT id FROM classes WHERE name LIKE 'mcp-registry-check-%')`,
		`DELETE FROM knowledge_entries WHERE class_id IN (SELECT id FROM classes WHERE name LIKE 'mcp-registry-check-%')`,
		`DELETE FROM materials WHERE class_id IN (SELECT id FROM classes WHERE name LIKE 'mcp-registry-check-%')`,
		`DELETE FROM sessions WHERE user_id IN (SELECT id FROM users WHERE class_id IN (SELECT id FROM classes WHERE name LIKE 'mcp-registry-check-%'))`,
		`DELETE FROM assignments WHERE class_id IN (SELECT id FROM classes WHERE name LIKE 'mcp-registry-check-%')`,
		`DELETE FROM tutor_agents WHERE class_id IN (SELECT id FROM classes WHERE name LIKE 'mcp-registry-check-%')`,
		`DELETE FROM skills WHERE class_id IN (SELECT id FROM classes WHERE name LIKE 'mcp-registry-check-%')`,
		`DELETE FROM users WHERE class_id IN (SELECT id FROM classes WHERE name LIKE 'mcp-registry-check-%')`,
		`DELETE FROM classes WHERE name LIKE 'mcp-registry-check-%'`,
	} {
		if _, err := db.ExecContext(ctx, query); err != nil {
			var mysqlErr *mysql.MySQLError
			if errors.As(err, &mysqlErr) {
				t.Fatalf("prior synthetic MCP fixture cleanup failed at step %d with MySQL error %d: %s", i+1, mysqlErr.Number, mysqlErr.Message)
			}
			t.Fatalf("prior synthetic MCP fixture cleanup failed at step %d: %T", i+1, err)
		}
	}
}
