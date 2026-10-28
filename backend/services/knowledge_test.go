package services

import (
	"math"
	"testing"
)

func TestFuseKnowledgeRRFUsesRanksAndDeduplicates(t *testing.T) {
	keyword := []KnowledgeHit{{ChunkID: 1, Title: "keyword top", Score: 1000}, {ChunkID: 2, Title: "keyword second", Score: 0.01}, {ChunkID: 2, Title: "duplicate"}}
	vector := []KnowledgeHit{{ChunkID: 2, Title: "vector top", Score: 0.99}, {ChunkID: 3, Title: "vector second", Score: 0.8}}
	got := FuseKnowledgeRRF(keyword, vector, 20)
	if len(got) != 3 || got[0].ChunkID != 2 || got[1].ChunkID != 1 || got[2].ChunkID != 3 {
		t.Fatalf("unexpected fused order: %#v", got)
	}
	want := 1.0/62 + 1.0/61
	if math.Abs(got[0].Score-want) > 1e-12 {
		t.Fatalf("RRF score = %g, want %g", got[0].Score, want)
	}
	if got[0].Title != "keyword second" {
		t.Fatalf("expected canonical keyword metadata, got %q", got[0].Title)
	}
}

func TestFuseKnowledgeRRFHonorsLimitAndEmpty(t *testing.T) {
	hits := []KnowledgeHit{{ChunkID: 1}, {ChunkID: 2}}
	if got := FuseKnowledgeRRF(hits, nil, 1); len(got) != 1 || got[0].ChunkID != 1 {
		t.Fatalf("unexpected limited results: %#v", got)
	}
	if got := FuseKnowledgeRRF(nil, nil, 10); len(got) != 0 {
		t.Fatalf("expected no hits: %#v", got)
	}
	if got := FuseKnowledgeRRF(hits, nil, 0); len(got) != 0 {
		t.Fatalf("invalid limit must return empty: %#v", got)
	}
}

func TestFilterVectorCandidatesUsesInclusiveCourseThreshold(t *testing.T) {
	got := FilterVectorCandidates([]VectorHit{{ChunkID: 1, Score: 0.3499}, {ChunkID: 2, Score: 0.35}, {ChunkID: 2, Score: 0.9}, {ChunkID: 3, Score: 0.8}})
	if len(got) != 2 || got[0].ChunkID != 2 || got[0].Score != 0.35 || got[1].ChunkID != 3 {
		t.Fatalf("vector threshold or de-duplication failed: %#v", got)
	}
}
