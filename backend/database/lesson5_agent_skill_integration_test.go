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

func TestLesson5AgentAndSkillManagementIntegration(t *testing.T) {
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
	tag := "lesson5-config-check-" + hex.EncodeToString(random[:])
	insertClass := func(name string) int64 {
		t.Helper()
		result, e := db.ExecContext(ctx, `INSERT INTO classes(name) VALUES (?)`, tag+name)
		if e != nil {
			t.Fatal("synthetic class insert failed")
		}
		id, _ := result.LastInsertId()
		return id
	}
	classA, classB := insertClass("-A"), insertClass("-B")
	insertUser := func(name string, classID int64, role string) repositories.User {
		t.Helper()
		result, e := db.ExecContext(ctx, `INSERT INTO users(username,password_hash,role,class_id) VALUES (?,?,?,?)`, tag+name, "!disabled-"+tag+name, role, classID)
		if e != nil {
			t.Fatal("synthetic disabled user insert failed")
		}
		id, _ := result.LastInsertId()
		return repositories.User{ID: id, Username: tag + name, Role: role, ClassID: classID}
	}
	teacherA := insertUser("-teacherA", classA, "teacher")
	teacherB := insertUser("-teacherB", classB, "teacher")
	studentA := insertUser("-studentA", classA, "student")
	materialRes, err := db.ExecContext(ctx, `INSERT INTO materials(class_id,uploaded_by,title,original_filename,storage_key,size_bytes,sha256) VALUES (?,?,?,?,?,0,?)`, classA, teacherA.ID, tag, tag+".txt", tag, strings.Repeat("0", 64))
	if err != nil {
		t.Fatal("synthetic material row insert failed")
	}
	materialID, _ := materialRes.LastInsertId()
	store := services.NewLesson5Store(db)
	defer func() {
		clean, stop := context.WithTimeout(context.Background(), 12*time.Second)
		defer stop()
		_, _ = db.ExecContext(clean, `DELETE FROM course_agent_materials WHERE class_id IN (?,?)`, classA, classB)
		_, _ = db.ExecContext(clean, `DELETE FROM course_agent_skills WHERE class_id IN (?,?)`, classA, classB)
		_, _ = db.ExecContext(clean, `DELETE FROM course_builtin_agents WHERE class_id IN (?,?)`, classA, classB)
		_, _ = db.ExecContext(clean, `DELETE FROM course_agents WHERE class_id IN (?,?)`, classA, classB)
		_, _ = db.ExecContext(clean, `DELETE FROM course_skills WHERE class_id IN (?,?)`, classA, classB)
		_, _ = db.ExecContext(clean, `DELETE FROM materials WHERE id=? AND class_id=?`, materialID, classA)
		_, _ = db.ExecContext(clean, `DELETE FROM users WHERE id IN (?,?,?)`, teacherA.ID, teacherB.ID, studentA.ID)
		_, _ = db.ExecContext(clean, `DELETE FROM classes WHERE id IN (?,?)`, classA, classB)
	}()
	sourceA := "名称：验收技能\n适用条件：topic,debug\n资料：\n工具：knowledge_search\n启用：是\n\n只用于合成验收。"
	skillA, err := store.SaveSkill(ctx, teacherA, 0, sourceA)
	if err != nil {
		t.Fatal("teacher could not create a valid skill")
	}
	if _, err = store.SaveSkill(ctx, teacherA, skillA, sourceA); err != nil {
		t.Fatal("saving an unchanged skill was treated as missing")
	}
	sourceB := strings.Replace(sourceA, "验收技能", "异班技能", 1)
	skillB, err := store.SaveSkill(ctx, teacherB, 0, sourceB)
	if err != nil {
		t.Fatal("second teacher could not create a class skill")
	}
	if _, err = store.SaveSkill(ctx, teacherB, skillA, sourceA); !errors.Is(err, services.ErrLesson5NotFound) {
		t.Fatal("teacher modified another class skill")
	}
	if _, err = store.SaveSkill(ctx, studentA, 0, sourceA); !errors.Is(err, services.ErrNotTeacher) {
		t.Fatal("student created a skill")
	}
	agent := services.Agent{Name: tag + " agent", Prompt: "Synthetic role", Enabled: true}
	agentID, err := store.SaveAgent(ctx, teacherA, 0, agent, []int64{skillA}, []int64{materialID})
	if err != nil {
		t.Fatal("teacher could not save an agent with same-class bindings")
	}
	config, err := store.ReadAgentConfig(ctx, classA, agentID)
	if err != nil || len(config.SkillIDs) != 1 || config.SkillIDs[0] != skillA || len(config.MaterialIDs) != 1 || config.MaterialIDs[0] != materialID {
		t.Fatal("same-class skill/material bindings did not persist")
	}
	if _, err = store.SaveAgent(ctx, teacherA, agentID, agent, []int64{skillB}, nil); !errors.Is(err, services.ErrLesson5NotFound) {
		t.Fatal("teacher bound a foreign-class skill")
	}
	config, err = store.ReadAgentConfig(ctx, classA, agentID)
	if err != nil || len(config.SkillIDs) != 1 || config.SkillIDs[0] != skillA || len(config.MaterialIDs) != 1 {
		t.Fatal("rejected cross-class update partially replaced existing bindings")
	}
	if _, err = store.SaveAgent(ctx, studentA, 0, agent, nil, nil); !errors.Is(err, services.ErrNotTeacher) {
		t.Fatal("student created an agent")
	}
	if _, err = store.SaveAgent(ctx, teacherA, 0, services.Agent{Name: strings.Repeat("名", 65), Enabled: true}, nil, nil); !errors.Is(err, services.ErrLesson5Invalid) {
		t.Fatal("overlong agent name was accepted")
	}
	agent.Enabled = false
	if _, err = store.SaveAgent(ctx, teacherA, agentID, agent, []int64{skillA}, []int64{materialID}); err != nil {
		t.Fatal("teacher could not disable an agent")
	}
	if _, err = store.ReadAgent(ctx, classA, agentID, true); !errors.Is(err, services.ErrLesson5NotFound) {
		t.Fatal("student could read a disabled agent")
	}
	if services.SelectSkill("topic debug", []services.RoutedSkill{{ID: skillA, Enabled: true, Bound: true, TriggerText: "topic"}}) == nil {
		t.Fatal("bound skill was unavailable after validation")
	}
	t.Log("CHECK: skill CRUD/idempotent edit, teacher-only agent changes, class-scoped skill/material bindings and disable filtering passed")
}
