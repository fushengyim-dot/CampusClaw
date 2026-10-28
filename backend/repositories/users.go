package repositories

import (
	"context"
	"database/sql"
	"errors"
)

var ErrUserNotFound = errors.New("user not found")
var ErrRead = errors.New("user lookup unavailable")

type Users struct{ db *sql.DB }

func NewUsers(db *sql.DB) *Users { return &Users{db: db} }

// The username is a bound value, never concatenated into SQL.
func (r *Users) PasswordHash(ctx context.Context, username string) (string, error) {
	var hash string
	err := r.db.QueryRowContext(ctx, "SELECT password_hash FROM users WHERE username=?", username).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrUserNotFound
	}
	if err != nil {
		return "", ErrRead
	}
	return hash, nil
}
