package services

import (
	"campusclaw/backend/repositories"
	"context"
	"errors"
	"golang.org/x/crypto/bcrypt"
	"testing"
)

type userStub struct {
	hash string
	err  error
}

func (s *userStub) PasswordHash(context.Context, string) (string, error) { return s.hash, s.err }
func TestPasswordVerification(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("synthetic-test-input"), 12)
	if err != nil {
		t.Fatal("test setup failed")
	}
	repo := &userStub{hash: string(hash)}
	service, err := NewLogin(repo)
	if err != nil {
		t.Fatal(err)
	}
	if service.Check(context.Background(), "example", "synthetic-test-input") != nil {
		t.Fatal("correct password refused")
	}
	if service.Check(context.Background(), "example", "wrong") != ErrCredentials {
		t.Fatal("wrong password accepted")
	}
	repo.err = repositories.ErrUserNotFound
	if service.Check(context.Background(), "missing", "synthetic-test-input") != ErrCredentials {
		t.Fatal("unknown user not uniform")
	}
	repo.err = errors.New("private-db-error")
	if service.Check(context.Background(), "example", "input") != ErrUnavailable {
		t.Fatal("database failure unsafe")
	}
	repo.err = nil
	repo.hash = "corrupted-hash"
	if service.Check(context.Background(), "example", "input") != ErrUnavailable {
		t.Fatal("corrupted hash not unavailable")
	}
}
