package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"strings"
	"time"
)

var ErrSeed = errors.New("账号初始化未通过；已有记录保留，请检查数据库或班级账号冲突")

type seedClass struct {
	id   int64
	name string
}
type seedAccount struct {
	username, role, variable string
	classID                  int64
}

var demoClasses = []seedClass{{1, "A班"}, {2, "B班"}}
var demoAccounts = []seedAccount{
	{"teacher_a", "teacher", "SEED_TEACHER_A_PASSWORD", 1},
	{"student_a1", "student", "SEED_STUDENT_A_PASSWORD", 1},
	{"teacher_b", "teacher", "SEED_TEACHER_B_PASSWORD", 2},
	{"student_b1", "student", "SEED_STUDENT_B_PASSWORD", 2},
}

// InitAccounts is explicit: normal startup never resets or seeds passwords.
func InitAccounts(parent context.Context, db *sql.DB, getenv func(string) string) error {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return ErrSeed
	}
	defer tx.Rollback()
	// All initializers serialize on the already-completed schema record.
	var version int
	if tx.QueryRowContext(ctx, "SELECT version FROM schema_migrations WHERE version=1 AND completed_at IS NOT NULL FOR UPDATE").Scan(&version) != nil {
		return ErrSeed
	}
	if err = seedAccountsTx(ctx, tx, getenv, demoClasses, demoAccounts); err != nil {
		return err
	}
	if tx.Commit() != nil {
		return errors.New("账号初始化提交结果未确认；请保留数据并重新检查，不要重置密码")
	}
	return nil
}

func seedAccountsTx(ctx context.Context, tx *sql.Tx, getenv func(string) string, classes []seedClass, accounts []seedAccount) error {
	for _, c := range classes {
		var name string
		err := tx.QueryRowContext(ctx, "SELECT name FROM classes WHERE id=?", c.id).Scan(&name)
		if errors.Is(err, sql.ErrNoRows) {
			if _, err = tx.ExecContext(ctx, "INSERT INTO classes(id,name) VALUES (?,?)", c.id, c.name); err != nil {
				return ErrSeed
			}
		} else if err != nil || name != c.name {
			return ErrSeed
		}
	}
	for _, a := range accounts {
		var role string
		var classID int64
		err := tx.QueryRowContext(ctx, "SELECT role,class_id FROM users WHERE username=?", a.username).Scan(&role, &classID)
		if err == nil {
			if role != a.role || classID != a.classID {
				return ErrSeed
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return ErrSeed
		}
		password := getenv(a.variable)
		if strings.TrimSpace(password) == "" || len([]byte(password)) > 72 {
			return fmt.Errorf("%s：新账号密码必填，且不得超过72字节", a.variable)
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
		if err != nil {
			return ErrSeed
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO users(username,password_hash,role,class_id) VALUES (?,?,?,?)", a.username, string(hash), a.role, a.classID); err != nil {
			return ErrSeed
		}
	}
	return nil
}
