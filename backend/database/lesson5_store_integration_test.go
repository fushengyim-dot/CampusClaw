package database

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"campusclaw/backend/repositories"
	"campusclaw/backend/services"
)

func TestLesson5ConversationOwnershipAndContextIntegration(t *testing.T) {
	if os.Getenv("CAMPUSCLAW_SCHEMA_INTEGRATION") != "1" {
		t.Skip("explicit schema integration required")
	}
	cfg := integrationConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
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
	tag := "lesson5-check-" + hex.EncodeToString(random[:])
	classRes, err := db.ExecContext(ctx, `INSERT INTO classes(name) VALUES (?)`, tag+"-A")
	if err != nil {
		t.Fatal("synthetic class insert failed")
	}
	classA, _ := classRes.LastInsertId()
	classRes, err = db.ExecContext(ctx, `INSERT INTO classes(name) VALUES (?)`, tag+"-B")
	if err != nil {
		t.Fatal("synthetic second class insert failed")
	}
	classB, _ := classRes.LastInsertId()
	var userIDs [3]int64
	for i, entry := range []struct {
		name    string
		classID int64
	}{{tag + "-owner", classA}, {tag + "-peer", classA}, {tag + "-foreign", classB}} {
		result, insertErr := db.ExecContext(ctx, `INSERT INTO users(username,password_hash,role,class_id) VALUES (?,?,'student',?)`, entry.name, "!disabled-"+entry.name, entry.classID)
		if insertErr != nil {
			t.Fatal("synthetic disabled user insert failed")
		}
		userIDs[i], _ = result.LastInsertId()
	}
	result, err := db.ExecContext(ctx, `INSERT INTO course_agents(class_id,name,prompt,enabled) VALUES (?,?,?,1)`, classA, tag, "Synthetic role")
	if err != nil {
		t.Fatal("synthetic agent insert failed")
	}
	agentA, _ := result.LastInsertId()
	result, err = db.ExecContext(ctx, `INSERT INTO course_agents(class_id,name,prompt,enabled) VALUES (?,?,?,1)`, classB, tag, "Synthetic role")
	if err != nil {
		t.Fatal("synthetic second agent insert failed")
	}
	agentB, _ := result.LastInsertId()
	store := services.NewLesson5Store(db)
	owner := repositories.User{ID: userIDs[0], Role: "student", ClassID: classA}
	peer := repositories.User{ID: userIDs[1], Role: "student", ClassID: classA}
	foreign := repositories.User{ID: userIDs[2], Role: "student", ClassID: classB}
	defer func() {
		clean, stop := context.WithTimeout(context.Background(), 12*time.Second)
		defer stop()
		_, _ = db.ExecContext(clean, `DELETE FROM conversation_messages WHERE class_id IN (?,?)`, classA, classB)
		_, _ = db.ExecContext(clean, `DELETE FROM conversations WHERE class_id IN (?,?)`, classA, classB)
		_, _ = db.ExecContext(clean, `DELETE FROM course_agent_materials WHERE class_id IN (?,?)`, classA, classB)
		_, _ = db.ExecContext(clean, `DELETE FROM course_agent_skills WHERE class_id IN (?,?)`, classA, classB)
		_, _ = db.ExecContext(clean, `DELETE FROM course_builtin_agents WHERE class_id IN (?,?)`, classA, classB)
		_, _ = db.ExecContext(clean, `DELETE FROM course_agents WHERE class_id IN (?,?)`, classA, classB)
		_, _ = db.ExecContext(clean, `DELETE FROM users WHERE id IN (?,?,?)`, userIDs[0], userIDs[1], userIDs[2])
		_, _ = db.ExecContext(clean, `DELETE FROM classes WHERE id IN (?,?)`, classA, classB)
	}()

	cid, err := store.CreateConversation(ctx, owner, agentA)
	if err != nil {
		t.Fatal("owner could not create a same-class conversation")
	}
	if _, err = store.CreateConversation(ctx, owner, agentB); !errors.Is(err, services.ErrLesson5NotFound) {
		t.Fatal("conversation accepted a foreign-class agent")
	}
	if _, _, _, err = store.ReadConversation(ctx, peer, cid); !errors.Is(err, services.ErrLesson5NotFound) {
		t.Fatal("same-class peer could read another user's conversation")
	}
	if _, _, _, err = store.ReadConversation(ctx, foreign, cid); !errors.Is(err, services.ErrLesson5NotFound) {
		t.Fatal("cross-class user could read a conversation")
	}
	if _, _, _, err = store.AgentContext(ctx, foreign, cid); !errors.Is(err, services.ErrLesson5NotFound) {
		t.Fatal("cross-class user could read locked agent context")
	}
	items, err := store.ListConversations(ctx, owner)
	if err != nil || len(items) != 1 || items[0].ID != cid {
		t.Fatal("owner's own conversation was not listed")
	}
	items, err = store.ListConversations(ctx, peer)
	if err != nil || len(items) != 0 {
		t.Fatal("conversation list leaked to a same-class peer")
	}

	longQuestion := strings.Repeat("甲", 30)
	if _, _, err = store.AppendQuestion(ctx, owner, cid, longQuestion); err != nil {
		t.Fatal("owner could not append a question")
	}
	conversation, _, _, err := store.ReadConversation(ctx, owner, cid)
	if err != nil || utf8RuneCount(conversation.Title) != 24 {
		t.Fatal("first question did not set a 24-character title")
	}
	if _, _, err = store.AppendQuestion(ctx, peer, cid, "他人提问"); !errors.Is(err, services.ErrLesson5NotFound) {
		t.Fatal("same-class peer could append to another user's conversation")
	}
	for i := 0; i < 6; i++ {
		if _, err = store.SaveAnswer(ctx, owner, cid, strings.Repeat("答", 600), nil, ""); err != nil {
			t.Fatal("owner could not save synthetic assistant history")
		}
		if _, _, err = store.AppendQuestion(ctx, owner, cid, strings.Repeat("问", 450)); err != nil {
			t.Fatal("owner could not append synthetic history")
		}
	}
	_, history, err := store.AppendQuestion(ctx, owner, cid, "最后一个问题")
	if err != nil || len(history) != 6 {
		t.Fatal("context did not retain exactly the most recent six messages")
	}
	for _, msg := range history {
		if utf8RuneCount(msg.Content) > 500 {
			t.Fatal("context message exceeded the 500-character limit")
		}
	}
	t.Log("CHECK: owner isolation, same-class/cross-class denial, agent lock, 24-character title, and six-by-500 context passed")
}

func utf8RuneCount(s string) int { return len([]rune(s)) }
