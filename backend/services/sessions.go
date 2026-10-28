package services

import (
	"campusclaw/backend/repositories"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"golang.org/x/crypto/bcrypt"
	"time"
)

const SessionLifetime = 8 * time.Hour

var ErrSession = errors.New("请先登录或重新登录")

type SessionStore interface {
	Credentials(context.Context, string) (repositories.User, string, error)
	Rotate(context.Context, repositories.User, string, string, string, time.Time) error
	Resolve(context.Context, string, time.Time) (repositories.User, error)
	Revoke(context.Context, string, time.Time) (bool, error)
}
type Auth struct {
	store  SessionStore
	secret []byte
	dummy  []byte
	now    func() time.Time
}

func NewAuth(store SessionStore, secret string, now func() time.Time) (*Auth, error) {
	if secret == "" {
		return nil, ErrUnavailable
	}
	if now == nil {
		now = time.Now
	}
	var noise [32]byte
	if _, err := rand.Read(noise[:]); err != nil {
		return nil, ErrUnavailable
	}
	dummy, err := bcrypt.GenerateFromPassword(noise[:], 12)
	if err != nil {
		return nil, ErrUnavailable
	}
	return &Auth{store, []byte(secret), dummy, now}, nil
}
func (a *Auth) digest(token string) string {
	if len(token) != 64 {
		return ""
	}
	decoded, err := hex.DecodeString(token)
	if err != nil || hex.EncodeToString(decoded) != token {
		return ""
	}
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}
func (a *Auth) Login(parent context.Context, name, password, oldToken string) (repositories.User, string, time.Time, error) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	u, hash, err := a.store.Credentials(ctx, name)
	missing := errors.Is(err, repositories.ErrUserNotFound)
	if err != nil && !missing {
		return u, "", time.Time{}, ErrUnavailable
	}
	candidate := []byte(hash)
	if missing {
		candidate = a.dummy
	}
	err = bcrypt.CompareHashAndPassword(candidate, []byte(password))
	if missing || errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return repositories.User{}, "", time.Time{}, ErrCredentials
	}
	if err != nil {
		return repositories.User{}, "", time.Time{}, ErrUnavailable
	}
	var random [32]byte
	if _, err = rand.Read(random[:]); err != nil {
		return repositories.User{}, "", time.Time{}, ErrUnavailable
	}
	token := hex.EncodeToString(random[:])
	expires := a.now().UTC().Truncate(time.Second).Add(SessionLifetime)
	err = a.store.Rotate(ctx, u, hash, a.digest(oldToken), a.digest(token), expires)
	if errors.Is(err, repositories.ErrUserNotFound) {
		return repositories.User{}, "", time.Time{}, ErrCredentials
	}
	if err != nil {
		return repositories.User{}, "", time.Time{}, ErrUnavailable
	}
	return u, token, expires, nil
}
func (a *Auth) Me(parent context.Context, token string) (repositories.User, error) {
	digest := a.digest(token)
	if digest == "" {
		return repositories.User{}, ErrSession
	}
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	u, err := a.store.Resolve(ctx, digest, a.now())
	if errors.Is(err, repositories.ErrUserNotFound) {
		return repositories.User{}, ErrSession
	}
	if err != nil {
		return repositories.User{}, ErrUnavailable
	}
	return u, nil
}
func (a *Auth) Logout(parent context.Context, token string) error {
	digest := a.digest(token)
	if digest == "" {
		return ErrSession
	}
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	deleted, err := a.store.Revoke(ctx, digest, a.now())
	if err != nil {
		return ErrUnavailable
	}
	if !deleted {
		return ErrSession
	}
	return nil
}
