package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"campusclaw/backend/config"
	"campusclaw/backend/database"
	"campusclaw/backend/repositories"
	"golang.org/x/crypto/bcrypt"
)

func TestMaterialRecoveryIntegration(t *testing.T) {
	if os.Getenv("CAMPUSCLAW_MATERIAL_INTEGRATION") != "1" {
		t.Skip("explicit MySQL upload and recovery verification required")
	}
	if os.Getenv("CAMPUSCLAW_MYSQL_INTEGRATION") != "1" {
		t.Skip("explicit project MySQL integration environment required")
	}
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		t.Fatal("integration configuration invalid")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	db, err := database.Open(ctx, cfg)
	if err != nil {
		t.Fatal("project database unavailable")
	}
	root := os.Getenv("CAMPUSCLAW_UPLOAD_ROOT")
	removeWholeRoot := root == ""
	if removeWholeRoot {
		root = t.TempDir()
	}
	var classID, userID int64
	className := "materials-integration-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte("not-a-real-login-secret"), bcrypt.MinCost)
	classResult, err := db.ExecContext(ctx, "INSERT INTO classes(name) VALUES(?)", className)
	if err != nil {
		t.Fatal("isolated test class was not created")
	}
	classID, err = classResult.LastInsertId()
	if err != nil || classID <= 0 {
		t.Fatal("isolated test class id unavailable")
	}
	username := "test_material_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if _, err := db.ExecContext(ctx, "INSERT INTO users(username,password_hash,role,class_id) VALUES(?,?,?,?)", username, string(passwordHash), "teacher", classID); err != nil {
		t.Fatal("isolated test user was not created")
	}
	if err := db.QueryRowContext(ctx, "SELECT id FROM users WHERE username=?", username).Scan(&userID); err != nil {
		t.Fatal("isolated test user id unavailable")
	}
	// Recover only stale fixtures bearing this test's explicit prefix, and only
	// when no material row references their class.
	stale, err := db.QueryContext(ctx, "SELECT id FROM classes WHERE name LIKE 'materials-integration-%' AND id<>?", classID)
	if err != nil {
		t.Fatal("test fixture cleanup could not inspect its own prefix")
	}
	var staleIDs []int64
	for stale.Next() {
		var oldID int64
		if stale.Scan(&oldID) == nil {
			staleIDs = append(staleIDs, oldID)
		}
	}
	stale.Close()
	for _, oldID := range staleIDs {
		rows, err := db.QueryContext(ctx, "SELECT m.id,m.storage_key FROM materials m JOIN users u ON u.id=m.uploaded_by AND u.class_id=m.class_id WHERE m.class_id=? AND u.username LIKE 'test_material_%'", oldID)
		if err != nil {
			continue
		}
		type staleMaterial struct {
			id  int64
			key string
		}
		var staleMaterials []staleMaterial
		for rows.Next() {
			var x staleMaterial
			if rows.Scan(&x.id, &x.key) == nil {
				staleMaterials = append(staleMaterials, x)
			}
		}
		rows.Close()
		for _, x := range staleMaterials {
			if storageNamePattern.MatchString(x.key) {
				_ = os.Remove(filepath.Join(root, strconv.FormatInt(oldID, 10), x.key))
			}
			_, _ = db.ExecContext(ctx, "DELETE FROM knowledge_chunks WHERE knowledge_entry_id IN (SELECT id FROM knowledge_entries WHERE material_id=?)", x.id)
			_, _ = db.ExecContext(ctx, "DELETE FROM knowledge_entries WHERE material_id=?", x.id)
			_, _ = db.ExecContext(ctx, "DELETE FROM materials WHERE id=? AND class_id=? AND storage_key=?", x.id, oldID, x.key)
		}
		_, _ = db.ExecContext(ctx, "DELETE FROM users WHERE class_id=? AND username LIKE 'test_material_%'", oldID)
		_, _ = db.ExecContext(ctx, "DELETE FROM classes WHERE id=? AND name LIKE 'materials-integration-%' AND NOT EXISTS (SELECT 1 FROM materials WHERE class_id=?)", oldID, oldID)
		_ = os.Remove(filepath.Join(root, strconv.FormatInt(oldID, 10)))
	}
	user := repositories.User{ID: userID, Username: username, Role: "teacher", ClassID: classID}
	s, err := NewMaterials(db, root)
	if err != nil {
		t.Fatal("temporary upload area unavailable")
	}
	if err := s.Recover(ctx); err != nil {
		detailErr := s.withUploadLock(ctx, func(conn *sql.Conn) error { return s.reconcile(ctx, conn) })
		t.Fatalf("project upload area did not pass recovery; diagnostic=%v", detailErr)
	}
	t.Cleanup(func() {
		if db != nil {
			rows, e := db.QueryContext(context.Background(), "SELECT id,storage_key FROM materials WHERE class_id=? AND uploaded_by=?", classID, userID)
			if e == nil {
				type item struct {
					id  int64
					key string
				}
				var all []item
				for rows.Next() {
					var x item
					if rows.Scan(&x.id, &x.key) == nil {
						all = append(all, x)
					}
				}
				rows.Close()
				for _, x := range all {
					_ = os.Remove(filepath.Join(root, strconv.FormatInt(classID, 10), x.key))
					_, _ = db.Exec("DELETE FROM knowledge_chunks WHERE knowledge_entry_id IN (SELECT id FROM knowledge_entries WHERE material_id=?)", x.id)
					_, _ = db.Exec("DELETE FROM knowledge_entries WHERE material_id=?", x.id)
					_, _ = db.Exec("DELETE FROM materials WHERE id=? AND class_id=? AND uploaded_by=?", x.id, classID, userID)
				}
			}
			_, _ = db.Exec("DELETE FROM users WHERE id=? AND class_id=?", userID, classID)
			_, _ = db.Exec("DELETE FROM classes WHERE id=? AND name=?", classID, className)
			_ = db.Close()
		}
		_ = os.RemoveAll(filepath.Join(root, strconv.FormatInt(classID, 10)))
		if removeWholeRoot {
			_ = os.RemoveAll(root)
		}
	})
	count := func() int {
		var n int
		if db == nil || db.QueryRowContext(ctx, "SELECT COUNT(*) FROM materials WHERE class_id=?", classID).Scan(&n) != nil {
			t.Fatal("test material count unavailable")
		}
		return n
	}
	files := func() int {
		n := 0
		_ = filepath.WalkDir(root, func(_ string, d os.DirEntry, e error) error {
			if e == nil && !d.IsDir() {
				n++
			}
			return nil
		})
		return n
	}
	verifyStored := func(id int64, want []byte) {
		t.Helper()
		var key, hash, body string
		var size int64
		if err := db.QueryRowContext(ctx, `SELECT m.storage_key,m.sha256,m.size_bytes,k.content FROM materials m JOIN knowledge_entries k ON k.material_id=m.id AND k.class_id=m.class_id WHERE m.id=? AND m.class_id=?`, id, classID).Scan(&key, &hash, &size, &body); err != nil {
			t.Fatal("two-table material record missing")
		}
		data, err := os.ReadFile(filepath.Join(root, strconv.FormatInt(classID, 10), key))
		if err != nil || int64(len(data)) != size || fmt.Sprintf("%x", sha256.Sum256(data)) != hash || !bytes.Equal(data, want) || body != strings.TrimPrefix(string(want), "\uFEFF") {
			t.Fatal("database body and source file did not match")
		}
		var chunks, ready int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*),SUM(index_status='ready') FROM knowledge_chunks c
			JOIN knowledge_entries k ON k.id=c.knowledge_entry_id WHERE k.material_id=?`, id).Scan(&chunks, &ready); err != nil || chunks == 0 || chunks != ready {
			t.Fatal("uploaded knowledge did not get ready keyword-search chunks")
		}
	}
	baseCount, baseFiles := count(), files()
	roundTripBody := []byte("# stable\n原始材料包含授权流程说明。\noriginal bytes\n")
	id, err := s.Upload(ctx, user, "首份验收材料", "roundtrip.md", "", "", roundTripBody)
	if err != nil {
		t.Fatal("valid transactional upload failed")
	}
	verifyStored(id, roundTripBody)
	hits, err := NewKnowledge(db).SearchKeyword(ctx, classID, "授权", 10, 0)
	if err != nil || len(hits) != 1 || hits[0].MaterialID != id || hits[0].Title != "首份验收材料" || !strings.Contains(hits[0].Excerpt, "授权流程") {
		t.Fatal("MySQL keyword search did not return the matching source and excerpt")
	}
	if _, err := db.ExecContext(ctx, `DELETE c FROM knowledge_chunks c JOIN knowledge_entries e ON e.id=c.knowledge_entry_id WHERE e.material_id=?`, id); err != nil {
		t.Fatal("could not prepare legacy material backfill fixture")
	}
	if err := NewKnowledge(db).EnsureChunks(ctx); err != nil {
		t.Fatal("legacy material backfill failed")
	}
	if err := NewKnowledge(db).EnsureChunks(ctx); err != nil {
		t.Fatal("repeat legacy material backfill failed")
	}
	verifyStored(id, roundTripBody)
	hits, err = NewKnowledge(db).SearchKeyword(ctx, classID, "授权", 10, 0)
	if err != nil || len(hits) != 1 || hits[0].MaterialID != id {
		t.Fatal("backfilled material was not keyword searchable")
	}
	otherClassHits, err := NewKnowledge(db).SearchKeyword(ctx, classID+1000000, "授权", 10, 0)
	if err != nil || len(otherClassHits) != 0 {
		t.Fatal("keyword search did not isolate results by the supplied server-side class")
	}
	if count() != baseCount+1 || files() != baseFiles+1 {
		t.Fatal("upload did not create exactly one row pair and file")
	}
	beforeCount, beforeFiles := count(), files()
	s.testHooks = &materialTestHooks{fileWrite: func(*os.File, []byte) error { return errors.New("injected file write failure") }}
	if _, err := s.Upload(ctx, user, "file write failure", "write-failure.md", "", "", []byte("not saved")); !errors.Is(err, ErrUploadFailed) {
		t.Fatalf("confirmed file write failure did not return a known failure: %v", err)
	}
	s.testHooks = nil
	if count() != beforeCount || files() != beforeFiles || !s.Ready() {
		t.Fatal("file write failure left a partial upload or disabled the service")
	}
	student := user
	student.Role = "student"
	beforeCount, beforeFiles = count(), files()
	if _, err := s.Upload(ctx, student, "student denied", "denied.txt", "", "", []byte("denied")); !errors.Is(err, ErrNotTeacher) {
		t.Fatal("student upload was not rejected")
	}
	if count() != beforeCount || files() != beforeFiles {
		t.Fatal("student rejection changed database or files")
	}
	if _, err := s.Upload(ctx, user, "unsupported", "bad.pdf", "", "", []byte("pdf")); !errors.Is(err, ErrUnsupportedType) {
		t.Fatal("unsupported file type did not produce a distinct rejection")
	}
	invalid := [][]byte{nil, []byte(" \n\t "), []byte("has\x00nul"), {0xff, 0xfe}}
	for _, body := range invalid {
		beforeCount, beforeFiles = count(), files()
		if _, err := s.Upload(ctx, user, "invalid", "bad.txt", "", "", body); !errors.Is(err, ErrUploadRejected) {
			t.Fatal("invalid text was accepted")
		}
		if count() != beforeCount || files() != beforeFiles {
			t.Fatal("invalid text left a file or database row")
		}
	}
	tooLarge := bytes.Repeat([]byte{'x'}, MaxMaterialBytes+1)
	beforeCount, beforeFiles = count(), files()
	if _, err := s.Upload(ctx, user, "too large", "large.txt", "", "", tooLarge); !errors.Is(err, ErrUploadRejected) {
		t.Fatal("oversized file was accepted")
	}
	if count() != beforeCount || files() != beforeFiles {
		t.Fatal("oversized file left a file or row")
	}
	exact := bytes.Repeat([]byte{'x'}, MaxMaterialBytes)
	exactID, err := s.Upload(ctx, user, "size boundary", "exact.TXT", "", "", exact)
	if err != nil {
		diagnostic := s.withUploadLock(ctx, func(conn *sql.Conn) error { return s.reconcile(ctx, conn) })
		var matchingRows int
		_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM materials WHERE class_id=? AND uploaded_by=? AND title=?", classID, userID, "size boundary").Scan(&matchingRows)
		t.Fatalf("exact 5 MiB boundary was rejected: ready=%v error=%v recovery_check=%v matching_rows=%d", s.Ready(), err, diagnostic, matchingRows)
	}
	verifyStored(exactID, exact)

	// A confirmed pre-commit failure rolls back both rows and removes only its file.
	s.testHooks = &materialTestHooks{beforeKnowledgeInsert: func() error { return errors.New("injected statement failure") }}
	beforeCount, beforeFiles = count(), files()
	if _, err := s.Upload(ctx, user, "rollback", "rollback.md", "", "", []byte("rollback body")); !errors.Is(err, ErrUploadFailed) {
		t.Fatal("confirmed transaction failure did not return a failed-upload result")
	}
	s.testHooks = nil
	if count() != beforeCount || files() != beforeFiles || !s.Ready() {
		t.Fatal("confirmed rollback left a partial record/file or disabled the service")
	}
	s.testHooks = &materialTestHooks{commit: func(tx *sql.Tx) error {
		if err := tx.Rollback(); err != nil {
			return err
		}
		return errors.New("injected commit failure after confirmed rollback")
	}}
	beforeCount, beforeFiles = count(), files()
	if _, err := s.Upload(ctx, user, "confirmed commit absence", "absent.md", "", "", []byte("confirmed absent")); !errors.Is(err, ErrUploadFailed) {
		t.Fatalf("confirmed absent commit did not resolve as a known failure: %v", err)
	}
	s.testHooks = nil
	if count() != beforeCount || files() != beforeFiles || !s.Ready() {
		t.Fatal("confirmed absent commit left a partial upload or disabled the service")
	}

	// A lost acknowledgement after a successful COMMIT is reconciled on a new DB session.
	s.testHooks = &materialTestHooks{afterCommit: func() error { return errors.New("injected lost acknowledgement") }}
	committedID, err := s.Upload(ctx, user, "commit acknowledgement loss", "ack.md", "", "", []byte("committed body"))
	s.testHooks = nil
	if err != nil || committedID <= 0 || !s.Ready() {
		t.Fatal("committed upload was not recovered as a complete success")
	}
	verifyStored(committedID, []byte("committed body"))

	// When the commit acknowledgment is lost and the authority is temporarily
	// unavailable, preserve the file and return an explicit unknown outcome.
	pendingID := int64(0)
	s.testHooks = &materialTestHooks{afterCommit: func() error {
		if err := db.Close(); err != nil {
			return err
		}
		return errors.New("injected lost acknowledgment with database unavailable")
	}}
	pendingID, err = s.Upload(ctx, user, "unknown while database unavailable", "pending.md", "", "", []byte("pending commit body"))
	s.testHooks = nil
	if !errors.Is(err, ErrUploadUnknown) || s.Ready() {
		t.Fatalf("database outage during unknown commit did not preserve an unready unknown result: id=%d ready=%v error=%v", pendingID, s.Ready(), err)
	}
	if _, err := os.Stat(filepath.Join(root, strconv.FormatInt(classID, 10))); err != nil {
		t.Fatal("unknown commit file was not preserved while database was unavailable")
	}
	db, err = database.Open(ctx, cfg)
	if err != nil {
		t.Fatal("database did not return after unknown commit")
	}
	s.db = db
	if err := s.Recover(ctx); err != nil || !s.Ready() {
		t.Fatal("unknown committed upload did not recover after database returned")
	}
	if err := db.QueryRowContext(ctx, "SELECT id FROM materials WHERE class_id=? AND uploaded_by=? AND title=?", classID, userID, "unknown while database unavailable").Scan(&pendingID); err != nil {
		t.Fatal("recovered committed upload was not visible to its class")
	}
	verifyStored(pendingID, []byte("pending commit body"))

	// Recovery must wait behind the database upload lock while an older transaction is still open.
	entered, release := make(chan struct{}), make(chan struct{})
	s.testHooks = &materialTestHooks{beforeCommit: func() error { close(entered); <-release; return nil }}
	uploadDone := make(chan error, 1)
	go func() {
		_, e := s.Upload(ctx, user, "late commit", "late.md", "", "", []byte("late transaction body"))
		uploadDone <- e
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("upload did not reach the transaction commit gate")
	}
	other, err := NewMaterials(db, root)
	if err != nil {
		t.Fatal("second recovery worker unavailable")
	}
	recoveryDone := make(chan error, 1)
	go func() { recoveryDone <- other.Recover(ctx) }()
	select {
	case e := <-recoveryDone:
		s.testHooks = nil
		close(release)
		t.Fatalf("recovery returned while the original transaction still held its lock: %v", e)
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	if e := <-uploadDone; e != nil {
		t.Fatal("delayed transaction did not commit")
	}
	s.testHooks = nil
	if e := <-recoveryDone; e != nil {
		t.Fatal("recovery did not resume after the old transaction ended")
	}

	// Cleanup failure and a temporarily unavailable database both retain the file and pause service.
	s.testHooks = &materialTestHooks{beforeKnowledgeInsert: func() error { return errors.New("injected row failure") }, removeFile: func(string) error { return errors.New("injected cleanup failure") }}
	_, err = s.Upload(ctx, user, "cleanup failure", "cleanup.md", "", "", []byte("keep while cleanup is uncertain"))
	s.testHooks = nil
	if !errors.Is(err, ErrUploadUnknown) || s.Ready() {
		t.Fatal("cleanup failure was not kept in the unknown/unready state")
	}
	if e := s.Recover(ctx); e != nil || !s.Ready() {
		t.Fatal("repeated recovery did not safely clean the orphan and restore readiness")
	}

	// Missing file, wrong hash, missing body, and wrong tenant association must fail closed.
	var key, body string
	if err := db.QueryRowContext(ctx, "SELECT m.storage_key,k.content FROM materials m JOIN knowledge_entries k ON k.material_id=m.id WHERE m.id=?", id).Scan(&key, &body); err != nil {
		t.Fatal("roundtrip record disappeared")
	}
	filePath := filepath.Join(root, strconv.FormatInt(classID, 10), key)
	saved, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal("roundtrip file missing before recovery checks")
	}
	if err := os.Remove(filePath); err != nil {
		t.Fatal("could not set up missing-file recovery case")
	}
	if e := other.Recover(ctx); e == nil || other.Ready() {
		t.Fatal("missing file did not keep the service unavailable")
	}
	if _, e := db.ExecContext(ctx, "SELECT 1"); e != nil {
		t.Fatal("missing-file diagnosis changed database availability")
	}
	if err := os.WriteFile(filePath, saved, 0600); err != nil {
		t.Fatal("could not restore test file")
	}
	if e := other.Recover(ctx); e != nil {
		t.Fatal("service did not recover after file restoration")
	}
	corrupt := append([]byte(nil), saved...)
	corrupt[0] ^= 1
	if err := os.WriteFile(filePath, corrupt, 0600); err != nil {
		t.Fatal("could not set up hash mismatch")
	}
	if _, err := other.OpenDownload(ctx, classID, id); !errors.Is(err, ErrMaterialsUnavailable) {
		t.Fatal("download served a file whose content no longer matched its recorded SHA-256")
	}
	if e := other.Recover(ctx); e == nil || other.Ready() {
		t.Fatal("hash mismatch did not fail closed")
	}
	if _, e := os.Stat(filePath); e != nil {
		t.Fatal("recovery deleted mismatched evidence")
	}
	if err := os.WriteFile(filePath, saved, 0600); err != nil {
		t.Fatal("could not restore hash test file")
	}
	if e := other.Recover(ctx); e != nil {
		t.Fatal("service did not recover after restoring hash")
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM knowledge_chunks WHERE knowledge_entry_id IN (SELECT id FROM knowledge_entries WHERE material_id=?)", id); err != nil {
		t.Fatal("could not remove test chunks before controlled body case")
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM knowledge_entries WHERE material_id=?", id); err != nil {
		t.Fatal("could not set up missing-body case")
	}
	if e := other.Recover(ctx); e == nil || other.Ready() {
		t.Fatal("missing body did not fail closed")
	}
	if _, e := os.Stat(filePath); e != nil {
		t.Fatal("missing-body recovery deleted the source file")
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO knowledge_entries(material_id,class_id,content) VALUES(?,?,?)", id, classID, body); err != nil {
		t.Fatal("could not restore test body")
	}
	if e := other.Recover(ctx); e != nil {
		t.Fatal("service did not recover after body restoration")
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal("could not create controlled tenant mismatch")
	}
	if _, err = conn.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS=0"); err == nil {
		_, err = conn.ExecContext(ctx, "UPDATE knowledge_entries SET class_id=? WHERE material_id=?", classID+1, id)
	}
	_, _ = conn.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS=1")
	_ = conn.Close()
	if err != nil {
		t.Fatal("could not set up controlled class mismatch")
	}
	if e := other.Recover(ctx); e == nil || other.Ready() {
		t.Fatal("class mismatch did not fail closed")
	}
	_, err = db.ExecContext(ctx, "UPDATE knowledge_entries SET class_id=? WHERE material_id=?", classID, id)
	if err != nil {
		t.Fatal("could not restore test class association")
	}
	if e := other.Recover(ctx); e != nil {
		t.Fatal("service did not recover after class restoration")
	}

	// An unreachable authoritative database must not cause orphan deletion.
	orphanDir := filepath.Join(root, strconv.FormatInt(classID, 10))
	if err := os.MkdirAll(orphanDir, 0700); err != nil {
		t.Fatal("orphan test directory unavailable")
	}
	orphanPath := filepath.Join(orphanDir, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.txt")
	if err := os.WriteFile(orphanPath, []byte("orphan"), 0600); err != nil {
		t.Fatal("orphan test file unavailable")
	}
	if err := db.Close(); err != nil {
		t.Fatal("temporary database disconnect failed")
	}
	closed, err := NewMaterials(db, root)
	if err != nil {
		t.Fatal("closed database recovery object unavailable")
	}
	if e := closed.Recover(ctx); e == nil || closed.Ready() {
		t.Fatal("database outage did not keep recovery unavailable")
	}
	if _, err := os.Stat(orphanPath); err != nil {
		t.Fatal("orphan was deleted without an authoritative database check")
	}
	db, err = database.Open(ctx, cfg)
	if err != nil {
		t.Fatal("database could not be restored for recovery")
	}
	s.db = db
	other.db = db
	closed.db = db
	if e := closed.Recover(ctx); e != nil {
		t.Fatal("recovery did not resume after database availability returned")
	}
	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Fatal("confirmed orphan was not removed after lock-protected recovery")
	}
}
