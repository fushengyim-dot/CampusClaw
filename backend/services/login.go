package services

import (
	"campusclaw/backend/repositories"
	"context"
	"crypto/rand"
	"errors"
	"golang.org/x/crypto/bcrypt"
	"time"
)

var ErrCredentials = errors.New("用户名或密码错误")
var ErrUnavailable = errors.New("服务暂时不可用，请稍后再试")

type PasswordReader interface {
	PasswordHash(context.Context, string) (string, error)
}
type Login struct {
	users PasswordReader
	dummy []byte
}

func NewLogin(users PasswordReader) (*Login, error) {
	// Not an account password: perform comparable work for an unknown username.
	var noise [32]byte
	if _, err := rand.Read(noise[:]); err != nil {
		return nil, ErrUnavailable
	}
	dummy, err := bcrypt.GenerateFromPassword(noise[:], 12)
	if err != nil {
		return nil, ErrUnavailable
	}
	return &Login{users: users, dummy: dummy}, nil
}

func (s *Login) Check(parent context.Context, username, password string) error {
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	hash, err := s.users.PasswordHash(ctx, username)
	missing := errors.Is(err, repositories.ErrUserNotFound)
	if err != nil && !missing {
		return ErrUnavailable
	}
	candidate := []byte(hash)
	if missing {
		candidate = s.dummy
	}
	err = bcrypt.CompareHashAndPassword(candidate, []byte(password))
	if ctx.Err() != nil {
		return ErrUnavailable
	}
	if missing || errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return ErrCredentials
	}
	if err != nil {
		return ErrUnavailable
	}
	return nil
}
