package services

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"campusclaw/backend/repositories"
)

type materialSeed struct {
	teacher, title, filename, sourceURL, locator, file string
}

// EnsureCourseMaterials adds only missing course fixtures. Existing records are
// accepted only when their uploader, bytes, and recorded digest match; conflicts
// stop startup instead of replacing a user's material.
func (s *Materials) EnsureCourseMaterials(ctx context.Context, seedRoot string) error {
	if !s.Ready() {
		return ErrMaterialsUnavailable
	}
	if seedRoot == "" {
		seedRoot = filepath.Join("..", "docs", "lesson-03", "materials")
	}
	seeds := []materialSeed{
		{teacher: "teacher_a", title: "A班虚构验收样例", filename: "A-class-fictional-demo.md", file: "A-class-fictional-demo.md"},
		{teacher: "teacher_b", title: "B班虚构验收样例", filename: "B-class-fictional-demo.md", file: "B-class-fictional-demo.md"},
		{teacher: "teacher_a", title: "第3课认证授权与班级材料复习（来源核对版）", filename: "lesson-03-auth-class-scope.md", sourceURL: "https://devops.hello1023.com/课件/第3课-课件-认证授权与知识库入库/pages/scope.html", locator: "本课目标与范围；角色、班级归属与材料边界", file: "lesson-03-auth-class-scope.md"},
	}
	for _, seed := range seeds {
		content, err := os.ReadFile(filepath.Join(seedRoot, seed.file))
		if err != nil || !validMaterialContent(content) {
			return ErrMaterialsUnavailable
		}
		var teacherID, classID int64
		if err := s.db.QueryRowContext(ctx, "SELECT id,class_id FROM users WHERE username=? AND role='teacher'", seed.teacher).Scan(&teacherID, &classID); err != nil || teacherID <= 0 || classID <= 0 {
			return ErrMaterialsUnavailable
		}
		wantHash := fmt.Sprintf("%x", sha256.Sum256(content))
		var existing int
		if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM materials WHERE class_id=? AND title=?", classID, seed.title).Scan(&existing); err != nil {
			return ErrMaterialsUnavailable
		}
		if existing > 1 {
			return ErrUploadFailed
		}
		var id, uploader, size int64
		var hash string
		err = s.db.QueryRowContext(ctx, "SELECT id,uploaded_by,size_bytes,sha256 FROM materials WHERE class_id=? AND title=?", classID, seed.title).Scan(&id, &uploader, &size, &hash)
		if err == nil {
			if uploader != teacherID || size != int64(len(content)) || hash != wantHash {
				return ErrUploadFailed
			}
			detail, err := s.Detail(ctx, classID, id)
			if err != nil || detail.Content != string(content) {
				return ErrUploadFailed
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return ErrMaterialsUnavailable
		}
		user := repositories.User{ID: teacherID, Username: seed.teacher, Role: "teacher", ClassID: classID}
		if _, err := s.Upload(ctx, user, seed.title, seed.filename, seed.sourceURL, seed.locator, content); err != nil {
			return err
		}
	}
	return nil
}
