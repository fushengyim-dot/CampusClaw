package database

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
	"time"

	"campusclaw/backend/services"
)

func TestBuiltinAgentSeedIntegration(t *testing.T) {
	if os.Getenv("CAMPUSCLAW_SCHEMA_INTEGRATION") != "1" {
		t.Skip("explicit schema integration required")
	}
	cfg := integrationConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var random [8]byte
	if _, err = rand.Read(random[:]); err != nil {
		t.Fatal("fixture id unavailable")
	}
	tag := "builtin-check-" + hex.EncodeToString(random[:])
	res, err := db.ExecContext(ctx, `INSERT INTO classes(name) VALUES (?)`, tag)
	if err != nil {
		t.Fatal("fixture class insert failed")
	}
	classID, _ := res.LastInsertId()
	defer func() {
		clean, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_, _ = db.ExecContext(clean, `DELETE FROM course_builtin_agents WHERE class_id=?`, classID)
		_, _ = db.ExecContext(clean, `DELETE FROM course_agents WHERE class_id=?`, classID)
		_, _ = db.ExecContext(clean, `DELETE FROM classes WHERE id=? AND name=?`, classID, tag)
	}()
	store := services.NewLesson5Store(db)
	if err = store.EnsureBuiltinAgents(ctx); err != nil {
		t.Fatalf("first built-in application seed failed: %v", err)
	}
	apps, err := store.ListBuiltinApps(ctx, classID)
	if err != nil || len(apps) != 3 {
		t.Fatal("three built-in applications were not created")
	}
	var originalName string
	if err = db.QueryRowContext(ctx, `SELECT name FROM course_agents WHERE id=? AND class_id=?`, apps[0].AgentID, classID).Scan(&originalName); err != nil {
		t.Fatal("built-in agent row missing")
	}
	changed := originalName + "（教师修改）"
	if _, err = db.ExecContext(ctx, `UPDATE course_agents SET name=? WHERE id=? AND class_id=?`, changed, apps[0].AgentID, classID); err != nil {
		t.Fatal("synthetic teacher edit failed")
	}
	if err = store.EnsureBuiltinAgents(ctx); err != nil {
		t.Fatal("repeat built-in seed failed")
	}
	apps, err = store.ListBuiltinApps(ctx, classID)
	if err != nil || len(apps) != 3 {
		t.Fatal("repeat startup duplicated built-in applications")
	}
	var saved string
	if err = db.QueryRowContext(ctx, `SELECT name FROM course_agents WHERE id=? AND class_id=?`, apps[0].AgentID, classID).Scan(&saved); err != nil || saved != changed {
		t.Fatal("repeat startup overwrote a teacher edit")
	}
	var count int
	if db.QueryRowContext(ctx, `SELECT COUNT(*) FROM course_agents WHERE class_id=?`, classID).Scan(&count) != nil || count != 3 {
		t.Fatal(fmt.Sprintf("expected three agents, got %d", count))
	}
	t.Log("CHECK: three built-in apps seeded once; repeat startup preserved the teacher edit")
}
