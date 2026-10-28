package services

import (
	"context"
	"errors"
	"testing"
)

func TestLocalStubStreamerEmitsSeparatePartsAndFails(t *testing.T) {
	var got []string
	s := LocalStubStreamer{Parts: []string{"甲", "乙"}}
	if err := s.Stream(context.Background(), nil, func(x string) error { got = append(got, x); return nil }); err != nil || len(got) != 2 || got[0] != "甲" || got[1] != "乙" {
		t.Fatalf("unexpected stream %v %v", got, err)
	}
	s.FailAfter = 1
	if err := s.Stream(context.Background(), nil, func(string) error { return nil }); !errors.Is(err, ErrChatStreamUnavailable) {
		t.Fatalf("expected simulated failure, got %v", err)
	}
}
