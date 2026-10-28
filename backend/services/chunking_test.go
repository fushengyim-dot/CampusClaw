package services

import (
	"strings"
	"testing"
)

func TestAutoChunksRespectRunesWindowsAndOverlap(t *testing.T) {
	text := strings.Repeat("界", 1700)
	chunks, err := ChunkText(text, ChunkOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}
	for i, chunk := range chunks {
		if len([]rune(chunk.Text)) > 800 || chunk.Start >= chunk.End || chunk.End-chunk.Start != len([]rune(chunk.Text)) {
			t.Fatalf("invalid rune range for chunk %d: %+v", i, chunk)
		}
		if chunk.Index != i || string([]rune(text)[chunk.Start:chunk.End]) != chunk.Text {
			t.Fatalf("chunk %d lost source positioning", i)
		}
		if i > 0 && chunks[i-1].End-chunk.Start != 80 {
			t.Fatalf("expected 80-rune overlap before chunk %d, got %d", i, chunks[i-1].End-chunk.Start)
		}
	}
}

func TestAutoPrefersSentenceBoundary(t *testing.T) {
	text := strings.Repeat("甲", 780) + "。" + strings.Repeat("乙", 400)
	chunks, err := ChunkText(text, ChunkOptions{Strategy: ChunkAuto})
	if err != nil {
		t.Fatal(err)
	}
	if got := []rune(chunks[0].Text)[len([]rune(chunks[0].Text))-1]; got != '。' {
		t.Fatalf("expected the first chunk to end at sentence punctuation, got %q", got)
	}
}

func TestCustomChunksBySeparatorAndRejectsInvalidOptions(t *testing.T) {
	text := strings.Repeat("甲", 120) + "。" + strings.Repeat("乙", 120)
	chunks, err := ChunkText(text, ChunkOptions{Strategy: ChunkCustom, Separator: "。", MaxChars: 200, OverlapPercent: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 {
		t.Fatalf("expected two custom chunks, got %d", len(chunks))
	}
	firstRunes := []rune(chunks[0].Text)
	if firstRunes[len(firstRunes)-1] != '。' {
		t.Fatalf("unexpected custom chunks: %+v", chunks)
	}
	if _, err := ChunkText("body", ChunkOptions{Strategy: ChunkCustom, Separator: "", MaxChars: 200}); err != ErrChunkOptions {
		t.Fatalf("expected invalid custom options, got %v", err)
	}
}

func TestHierarchyKeepsHeadingsAndOffsets(t *testing.T) {
	text := "# 第一章\n正文一\n## 第二节\n正文二\n"
	chunks, err := ChunkText(text, ChunkOptions{Strategy: ChunkHierarchy})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 || !strings.HasPrefix(chunks[0].Text, "# 第一章") || !strings.HasPrefix(chunks[1].Text, "## 第二节") {
		t.Fatalf("headings were not retained: %+v", chunks)
	}
	runes := []rune(text)
	for _, chunk := range chunks {
		if string(runes[chunk.Start:chunk.End]) != chunk.Text {
			t.Fatalf("wrong source range: %+v", chunk)
		}
	}
}

func TestChunkPreprocessingOffsetsReferToProcessedText(t *testing.T) {
	chunks, err := ChunkText("查看 https://example.com/a 再问 user@example.com 这里。", ChunkOptions{Strategy: ChunkAuto, RemoveURLsEmails: true, CollapseWhitespace: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 || strings.Contains(chunks[0].Text, "example.com") || strings.Contains(chunks[0].Text, "user@") || chunks[0].Start != 0 {
		t.Fatalf("preprocessing failed: %+v", chunks)
	}
}

func TestEmptyAndUnknownChunkStrategy(t *testing.T) {
	chunks, err := ChunkText("", ChunkOptions{Strategy: ChunkHierarchy})
	if err != nil || len(chunks) != 0 {
		t.Fatalf("expected empty result, got %#v, %v", chunks, err)
	}
	if _, err := ChunkText("body", ChunkOptions{Strategy: "unknown"}); err != ErrChunkOptions {
		t.Fatalf("expected invalid strategy error, got %v", err)
	}
}
