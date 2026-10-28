package database

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

// Explicit opt-in: these tests create and remove only their own synthetic rows.
// No source files, real accounts, uploads, or existing rows are removed.
func TestSchemaIntegration(t *testing.T) {
	if os.Getenv("CAMPUSCLAW_SCHEMA_INTEGRATION") != "1" {
		t.Skip("explicit schema test required")
	}
	c := integrationConfig(t)
	if c.MySQLDatabase != "campusclaw_course" || c.MySQLUser != "campusclaw" {
		t.Fatal("unexpected project scope")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	db, err := Open(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Preserve the prior lesson's persistent marker without printing it.
	var probeBefore, probeAfter string
	if db.QueryRowContext(ctx, "SELECT marker FROM connection_check_probe WHERE id=1").Scan(&probeBefore) != nil {
		t.Fatal("prior persistence probe missing")
	}
	if err := InitSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	tables := append(append([]string{}, schemaTables...), "schema_migrations")
	baseline := schemaSnapshot(t, ctx, db, tables)

	suffix := make([]byte, 12)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatal("random fixture identifier failed")
	}
	tag := "schema-check-" + hex.EncodeToString(suffix)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal("fixture transaction failed")
	}
	defer tx.Rollback()
	insert := func(query string, args ...any) int64 {
		t.Helper()
		result, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			t.Fatal("synthetic fixture insert failed")
		}
		id, err := result.LastInsertId()
		if err != nil {
			t.Fatal("fixture identifier failed")
		}
		return id
	}
	a := insert("INSERT INTO classes(name) VALUES (?)", tag+"-A")
	b := insert("INSERT INTO classes(name) VALUES (?)", tag+"-B")
	// Disabled marker, never a real password or a login-capable account.
	u := insert("INSERT INTO users(username,password_hash,role,class_id) VALUES (?,?,'teacher',?)", tag, "!disabled-"+tag, a)
	uB := insert("INSERT INTO users(username,password_hash,role,class_id) VALUES (?,?,'student',?)", tag+"-B", "!disabled-"+tag+"-B", b)
	m := insert("INSERT INTO materials(class_id,uploaded_by,title,original_filename,storage_key,size_bytes,sha256) VALUES (?,?,?,?,?,0,?)", a, u, tag, "schema-check-only.txt", tag, strings.Repeat("0", 64))
	insert("INSERT INTO knowledge_entries(material_id,class_id,content) VALUES (?,?,?)", m, a, "虚构结构检查记录，不是上传讲义")
	insert("INSERT INTO sessions(token_hash,user_id,expires_at) VALUES (?,?,?)", fmt.Sprintf("%x", sha256.Sum256(suffix)), u, time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC))
	insert("INSERT INTO assignments(class_id,title) VALUES (?,?)", a, tag)
	insert("INSERT INTO tutor_agents(class_id,name) VALUES (?,?)", a, tag)
	insert("INSERT INTO skills(class_id,name) VALUES (?,?)", a, tag)
	agent := insert("INSERT INTO course_agents(class_id,name,prompt,enabled) VALUES (?,?,?,1)", a, tag, "")
	insert("INSERT INTO course_builtin_agents(class_id,builtin_key,agent_id) VALUES (?,'lecture',?)", a, agent)
	skillA := insert("INSERT INTO course_skills(class_id,name,trigger_text,tools_json,enabled,description,source_text) VALUES (?,?,?,'[]',1,'body','source')", a, tag[:24]+"-A", "term")
	skillB := insert("INSERT INTO course_skills(class_id,name,trigger_text,tools_json,enabled,description,source_text) VALUES (?,?,?,'[]',1,'body','source')", b, tag[:24]+"-B", "term")
	insert("INSERT INTO course_agent_skills(agent_id,skill_id,class_id) VALUES (?,?,?)", agent, skillA, a)
	insert("INSERT INTO course_agent_materials(agent_id,material_id,class_id) VALUES (?,?,?)", agent, m, a)
	conversation := insert("INSERT INTO conversations(class_id,user_id,agent_id) VALUES (?,?,?)", a, u, agent)
	insert("INSERT INTO conversation_messages(conversation_id,user_id,class_id,role,content) VALUES (?,?,?,'user','synthetic')", conversation, u, a)
	if tx.Commit() != nil {
		t.Fatal("fixture commit failed; inspect uniquely tagged test rows before retry")
	}
	defer func() {
		cleanCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		clean, e := db.BeginTx(cleanCtx, nil)
		if e != nil {
			t.Error("fixture cleanup could not start")
			return
		}
		defer clean.Rollback()
		for _, item := range []struct {
			query string
			id    int64
		}{
			{"DELETE FROM conversation_messages WHERE conversation_id=?", conversation},
			{"DELETE FROM conversations WHERE id=?", conversation},
			{"DELETE FROM course_builtin_agents WHERE agent_id=?", agent},
			{"DELETE FROM course_agents WHERE id=?", agent},
			{"DELETE FROM course_skills WHERE id=?", skillA},
			{"DELETE FROM course_skills WHERE id=?", skillB},
			{"DELETE FROM knowledge_entries WHERE material_id=?", m},
			{"DELETE FROM materials WHERE id=?", m},
			{"DELETE FROM sessions WHERE user_id=?", u},
			{"DELETE FROM assignments WHERE class_id=?", a},
			{"DELETE FROM tutor_agents WHERE class_id=?", a},
			{"DELETE FROM skills WHERE class_id=?", a},
			{"DELETE FROM users WHERE id=?", uB},
			{"DELETE FROM users WHERE id=?", u},
			{"DELETE FROM classes WHERE id=?", a},
			{"DELETE FROM classes WHERE id=?", b},
		} {
			if _, e = clean.ExecContext(cleanCtx, item.query, item.id); e != nil {
				t.Error("fixture cleanup failed; existing data not touched")
				return
			}
		}
		if clean.Commit() != nil {
			t.Error("fixture cleanup commit failed")
			return
		}
		if !reflect.DeepEqual(baseline, schemaSnapshot(t, cleanCtx, db, tables)) {
			t.Error("baseline rows changed after test cleanup")
		}
		if db.QueryRowContext(cleanCtx, "SELECT marker FROM connection_check_probe WHERE id=1").Scan(&probeAfter) != nil || probeBefore != probeAfter {
			t.Error("prior persistence marker changed")
		}
		t.Log("CHECK: only own fixtures removed; all baseline rows and previous persistence marker unchanged")
	}()

	before := schemaSnapshot(t, ctx, db, tables)
	for i := 0; i < 2; i++ {
		if err := InitSchema(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(before, schemaSnapshot(t, ctx, db, tables)) {
		t.Fatal("repeat initialization changed rows")
	}
	t.Log("CHECK: repeated initialization preserved every field in all lesson tables and migration records")
	for _, tc := range []struct {
		name  string
		query string
		args  []any
	}{
		{"agent cannot bind another class skill", "INSERT INTO course_agent_skills(agent_id,skill_id,class_id) VALUES (?,?,?)", []any{agent, skillB, a}},
		{"message must match conversation owner and class", "INSERT INTO conversation_messages(conversation_id,user_id,class_id,role,content) VALUES (?,?,?,'user','synthetic')", []any{conversation, uB, b}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			trial, e := db.BeginTx(ctx, nil)
			if e != nil {
				t.Fatal("constraint transaction failed")
			}
			defer trial.Rollback()
			if _, e = trial.ExecContext(ctx, tc.query, tc.args...); e == nil {
				t.Fatal("cross-class reference unexpectedly accepted")
			}
		})
	}

	for _, tc := range []struct {
		name, query string
		args        []any
		code        uint16
	}{
		{"duplicate username", "INSERT INTO users(username,password_hash,role,class_id) VALUES (?,'!disabled','student',?)", []any{tag, a}, 1062},
		{"duplicate storage key", "INSERT INTO materials(class_id,uploaded_by,title,original_filename,storage_key,size_bytes,sha256) VALUES (?,?,'check','check.txt',?,0,?)", []any{a, u, tag, strings.Repeat("0", 64)}, 1062},
		{"null storage key", "INSERT INTO materials(class_id,uploaded_by,title,original_filename,storage_key,size_bytes,sha256) VALUES (?,?,'check','check.txt',NULL,0,?)", []any{a, u, strings.Repeat("0", 64)}, 1048},
		{"cross-class knowledge", "UPDATE knowledge_entries SET class_id=? WHERE material_id=?", []any{b, m}, 1452},
		{"cross-class uploader", "INSERT INTO materials(class_id,uploaded_by,title,original_filename,storage_key,size_bytes,sha256) VALUES (?,?,'check','check.txt',?,0,?)", []any{b, u, tag + "-other", strings.Repeat("0", 64)}, 1452},
		{"invalid role", "UPDATE users SET role='admin' WHERE id=?", []any{u}, 3819},
		{"referenced class deletion", "DELETE FROM classes WHERE id=?", []any{a}, 1451},
		{"duplicate knowledge", "INSERT INTO knowledge_entries(material_id,class_id,content) VALUES (?,?,'check')", []any{m, a}, 1062},
	} {
		t.Run(tc.name, func(t *testing.T) {
			trial, e := db.BeginTx(ctx, nil)
			if e != nil {
				t.Fatal("constraint transaction failed")
			}
			defer trial.Rollback()
			_, e = trial.ExecContext(ctx, tc.query, tc.args...)
			var sqlErr *mysql.MySQLError
			if !errors.As(e, &sqlErr) || sqlErr.Number != tc.code {
				t.Fatal("constraint did not reject with expected error category")
			}
		})
	}
	// Simulate a modified historical SQL file in test memory only.
	original := initialSchema
	initialSchema += "\n"
	conflict := InitSchema(ctx, db)
	initialSchema = original
	if !errors.Is(conflict, ErrSchemaConflict) {
		t.Fatal("modified migration not rejected")
	}
	t.Log("CHECK: modified migration refused without changing rows")
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { done <- InitSchema(ctx, db) }()
	}
	for i := 0; i < 2; i++ {
		if <-done != nil {
			t.Fatal("concurrent initialization failed")
		}
	}
	if !reflect.DeepEqual(before, schemaSnapshot(t, ctx, db, tables)) {
		t.Fatal("failure/concurrent checks changed rows")
	}
	var versions int
	if db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version BETWEEN 1 AND 7 AND completed_at IS NOT NULL").Scan(&versions) != nil || versions != 7 {
		t.Fatal("all seven migration completions were not recorded exactly once")
	}
	t.Log("CHECK: concurrent initialization serialized; no duplicate version or rows")
}

// Hash all fields without logging contents, password hashes, or session digests.
func schemaSnapshot(t *testing.T, ctx context.Context, db *sql.DB, tables []string) map[string]string {
	t.Helper()
	result := map[string]string{}
	for _, table := range tables {
		rows, err := db.QueryContext(ctx, "SELECT * FROM `"+table+"` ORDER BY 1")
		if err != nil {
			t.Fatal("snapshot query failed")
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			t.Fatal("snapshot columns failed")
		}
		hash := sha256.New()
		encoder := json.NewEncoder(hash)
		for rows.Next() {
			values := make([]any, len(columns))
			targets := make([]any, len(columns))
			for i := range values {
				targets[i] = &values[i]
			}
			if rows.Scan(targets...) != nil || encoder.Encode(values) != nil {
				rows.Close()
				t.Fatal("snapshot encoding failed")
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal("snapshot read failed")
		}
		result[table] = hex.EncodeToString(hash.Sum(nil))
	}
	return result
}
