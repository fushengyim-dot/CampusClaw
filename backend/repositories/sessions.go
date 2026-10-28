package repositories

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	ClassID  int64  `json:"class_id"`
}
type Sessions struct{ db *sql.DB }

func NewSessions(db *sql.DB) *Sessions { return &Sessions{db} }
func (r *Sessions) Credentials(ctx context.Context, name string) (User, string, error) {
	var u User
	var hash string
	err := r.db.QueryRowContext(ctx, "SELECT id,username,role,class_id,password_hash FROM users WHERE username=?", name).Scan(&u.ID, &u.Username, &u.Role, &u.ClassID, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return u, "", ErrUserNotFound
	}
	if err != nil {
		return u, "", ErrRead
	}
	return u, hash, nil
}
func (r *Sessions) Rotate(ctx context.Context, u User, matchedHash, oldDigest, newDigest string, expires time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrRead
	}
	defer tx.Rollback()
	var current string
	if tx.QueryRowContext(ctx, "SELECT password_hash FROM users WHERE id=? FOR UPDATE", u.ID).Scan(&current) != nil {
		return ErrRead
	}
	if current != matchedHash {
		return ErrUserNotFound
	}
	if oldDigest != "" {
		if _, err = tx.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash=?", oldDigest); err != nil {
			return ErrRead
		}
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO sessions(token_hash,user_id,expires_at) VALUES (?,?,?)", newDigest, u.ID, expires.UTC()); err != nil {
		return ErrRead
	}
	if tx.Commit() != nil {
		return ErrRead
	}
	return nil
}
func (r *Sessions) Resolve(ctx context.Context, digest string, now time.Time) (User, error) {
	var u User
	err := r.db.QueryRowContext(ctx, "SELECT u.id,u.username,u.role,u.class_id FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=? AND s.expires_at>?", digest, now.UTC()).Scan(&u.ID, &u.Username, &u.Role, &u.ClassID)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrUserNotFound
	}
	if err != nil {
		return u, ErrRead
	}
	return u, nil
}
func (r *Sessions) Revoke(ctx context.Context, digest string, now time.Time) (bool, error) {
	result, err := r.db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash=? AND expires_at>?", digest, now.UTC())
	if err != nil {
		return false, ErrRead
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, ErrRead
	}
	return n == 1, nil
}
