package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"campusclaw/backend/repositories"
	"golang.org/x/crypto/bcrypt"
)

var ErrRegistration = errors.New("注册未完成")

type RegistrationStore struct{ db *sql.DB }
type Invitation struct {
	ID        int64     `json:"id"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Revoked   bool      `json:"revoked"`
	Consumed  bool      `json:"consumed"`
}

func NewRegistrationStore(db *sql.DB) *RegistrationStore { return &RegistrationStore{db: db} }

func (s *RegistrationStore) Issue(ctx context.Context, teacher repositories.User, role string) (Invitation, string, error) {
	if teacher.Role != "teacher" {
		return Invitation{}, "", ErrNotTeacher
	}
	if role != "teacher" && role != "student" {
		return Invitation{}, "", ErrRegistration
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return Invitation{}, "", ErrRegistration
	}
	code := hex.EncodeToString(raw[:])
	digest := sha256.Sum256([]byte(code))
	now := time.Now().UTC()
	expires := now.Add(24 * time.Hour)
	res, err := s.db.ExecContext(ctx, `INSERT INTO account_invitations(token_hash,class_id,role,issued_by,expires_at) VALUES(?,?,?,?,?)`, hex.EncodeToString(digest[:]), teacher.ClassID, role, teacher.ID, expires)
	if err != nil {
		return Invitation{}, "", ErrRegistration
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Invitation{}, "", ErrRegistration
	}
	return Invitation{ID: id, Role: role, CreatedAt: now, ExpiresAt: expires}, code, nil
}

func (s *RegistrationStore) List(ctx context.Context, teacher repositories.User) ([]Invitation, error) {
	if teacher.Role != "teacher" {
		return nil, ErrNotTeacher
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,role,created_at,expires_at,revoked_at IS NOT NULL,consumed_at IS NOT NULL FROM account_invitations WHERE class_id=? ORDER BY id DESC LIMIT 100`, teacher.ClassID)
	if err != nil {
		return nil, ErrRegistration
	}
	defer rows.Close()
	items := []Invitation{}
	for rows.Next() {
		var item Invitation
		if rows.Scan(&item.ID, &item.Role, &item.CreatedAt, &item.ExpiresAt, &item.Revoked, &item.Consumed) != nil {
			return nil, ErrRegistration
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, ErrRegistration
	}
	return items, nil
}

func (s *RegistrationStore) Revoke(ctx context.Context, teacher repositories.User, id int64) error {
	if teacher.Role != "teacher" {
		return ErrNotTeacher
	}
	res, err := s.db.ExecContext(ctx, `UPDATE account_invitations SET revoked_at=UTC_TIMESTAMP(6) WHERE id=? AND class_id=? AND issued_by=? AND revoked_at IS NULL AND consumed_at IS NULL AND expires_at>UTC_TIMESTAMP(6)`, id, teacher.ClassID, teacher.ID)
	if err != nil {
		return ErrRegistration
	}
	n, err := res.RowsAffected()
	if err != nil || n != 1 {
		return ErrRegistration
	}
	return nil
}

func (s *RegistrationStore) Register(ctx context.Context, code, username, password string) error {
	username = strings.TrimSpace(username)
	if len(username) == 0 || !utf8.ValidString(username) || utf8.RuneCountInString(username) > 191 || !utf8.ValidString(password) || utf8.RuneCountInString(password) < 12 || len([]byte(password)) > 72 || !validInvitationCode(code) {
		return ErrRegistration
	}
	digest := sha256.Sum256([]byte(code))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrRegistration
	}
	defer tx.Rollback()
	var id, classID int64
	var role string
	var expires time.Time
	var revoked, consumed sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT id,class_id,role,expires_at,revoked_at,consumed_at FROM account_invitations WHERE token_hash=? FOR UPDATE`, hex.EncodeToString(digest[:])).Scan(&id, &classID, &role, &expires, &revoked, &consumed)
	if err != nil || (role != "teacher" && role != "student") || revoked.Valid || consumed.Valid || !expires.After(time.Now().UTC()) {
		return ErrRegistration
	}
	// Avoid bcrypt work for arbitrary invalid invitation probes.
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return ErrRegistration
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO users(username,password_hash,role,class_id) VALUES(?,?,?,?)`, username, string(hash), role, classID); err != nil {
		return ErrRegistration
	}
	res, err := tx.ExecContext(ctx, `UPDATE account_invitations SET consumed_at=UTC_TIMESTAMP(6) WHERE id=? AND consumed_at IS NULL AND revoked_at IS NULL AND expires_at>UTC_TIMESTAMP(6)`, id)
	if err != nil {
		return ErrRegistration
	}
	n, err := res.RowsAffected()
	if err != nil || n != 1 {
		return ErrRegistration
	}
	if tx.Commit() != nil {
		return ErrRegistration
	}
	return nil
}

func validInvitationCode(code string) bool {
	if len(code) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(code)
	return err == nil && hex.EncodeToString(decoded) == code
}
