package services

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
)

type ChunkStrategy string

const (
	ChunkAuto      ChunkStrategy = "auto"
	ChunkCustom    ChunkStrategy = "custom"
	ChunkHierarchy ChunkStrategy = "hierarchy"
)

type ChunkOptions struct {
	Strategy           ChunkStrategy
	Separator          string
	MaxChars           int
	OverlapPercent     int
	RemoveURLsEmails   bool
	CollapseWhitespace bool
}

type TextChunk struct {
	Index int    `json:"chunk_index"`
	Text  string `json:"chunk_text"`
	Start int    `json:"char_start"`
	End   int    `json:"char_end"`
}

var (
	ErrChunkOptions = errors.New("切片参数不符合要求")
	urlPattern      = regexp.MustCompile(`(?i)\b(?:https?://|www\.)[^\s<>]+`)
	emailPattern    = regexp.MustCompile(`(?i)\b[[:alnum:]._%+-]+@[[:alnum:].-]+\.[[:alpha:]]{2,}\b`)
)

// ChunkText splits text without modifying the caller's source. Offsets are rune
// indexes into the post-preprocessing text, matching the lesson's stated limit.
func ChunkText(source string, options ChunkOptions) ([]TextChunk, error) {
	strategy := options.Strategy
	if strategy == "" {
		strategy = ChunkAuto
	}
	text := source
	if options.RemoveURLsEmails {
		text = urlPattern.ReplaceAllString(text, " ")
		text = emailPattern.ReplaceAllString(text, " ")
	}
	if options.CollapseWhitespace {
		text = strings.Join(strings.FieldsFunc(text, unicode.IsSpace), " ")
	}

	switch strategy {
	case ChunkAuto:
		return windowChunks(text, 800, 80), nil
	case ChunkCustom:
		if options.Separator == "" || options.MaxChars < 100 || options.MaxChars > 2000 || options.OverlapPercent < 0 || options.OverlapPercent > 50 {
			return nil, ErrChunkOptions
		}
		return separatorChunks(text, options.Separator, options.MaxChars, options.MaxChars*options.OverlapPercent/100), nil
	case ChunkHierarchy:
		return hierarchyChunks(text), nil
	default:
		return nil, ErrChunkOptions
	}
}

func windowChunks(text string, maxChars, overlap int) []TextChunk {
	runes := []rune(text)
	if len(runes) == 0 {
		return []TextChunk{}
	}
	chunks := make([]TextChunk, 0, (len(runes)+maxChars-overlap-1)/(maxChars-overlap))
	for start := 0; start < len(runes); {
		hardEnd := start + maxChars
		if hardEnd > len(runes) {
			hardEnd = len(runes)
		}
		end := hardEnd
		if hardEnd < len(runes) {
			if boundary := preferredBoundary(runes, start, hardEnd); boundary > start {
				end = boundary
			}
		}
		chunks = append(chunks, TextChunk{Index: len(chunks), Text: string(runes[start:end]), Start: start, End: end})
		if end == len(runes) {
			break
		}
		next := end - overlap
		if next <= start {
			next = end
		}
		start = next
	}
	return chunks
}

func preferredBoundary(runes []rune, start, hardEnd int) int {
	for i := hardEnd; i > start; i-- {
		if i >= 2 && runes[i-2] == '\n' && runes[i-1] == '\n' {
			return i
		}
	}
	for i := hardEnd; i > start; i-- {
		if runes[i-1] == '\n' {
			return i
		}
	}
	for i := hardEnd; i > start; i-- {
		switch runes[i-1] {
		case '。', '！', '？', '.', '!', '?', '；', ';':
			return i
		}
	}
	return hardEnd
}

func separatorChunks(text, separator string, maxChars, overlap int) []TextChunk {
	runes := []rune(text)
	separatorRunes := []rune(separator)
	if len(runes) == 0 {
		return []TextChunk{}
	}
	// The configured delimiter creates preferred boundaries; long sections still
	// use the automatic safe window so no output exceeds maxChars.
	sections := make([][2]int, 0)
	start := 0
	for i := 0; i+len(separatorRunes) <= len(runes); {
		if hasRunesAt(runes, i, separatorRunes) {
			end := i + len(separatorRunes)
			sections = append(sections, [2]int{start, end})
			start = end
			i = end
		} else {
			i++
		}
	}
	if start < len(runes) {
		sections = append(sections, [2]int{start, len(runes)})
	}
	chunks := make([]TextChunk, 0)
	for _, section := range sections {
		part := string(runes[section[0]:section[1]])
		for _, chunk := range windowChunks(part, maxChars, overlap) {
			chunk.Start += section[0]
			chunk.End += section[0]
			chunk.Index = len(chunks)
			chunks = append(chunks, chunk)
		}
	}
	return chunks
}

func hasRunesAt(source []rune, index int, target []rune) bool {
	if len(target) == 0 || index+len(target) > len(source) {
		return false
	}
	for i, r := range target {
		if source[index+i] != r {
			return false
		}
	}
	return true
}

func hierarchyChunks(text string) []TextChunk {
	runes := []rune(text)
	if len(runes) == 0 {
		return []TextChunk{}
	}
	sections := make([][2]int, 0)
	sectionStart, lineStart := 0, 0
	for lineStart < len(runes) {
		lineEnd := lineStart
		for lineEnd < len(runes) && runes[lineEnd] != '\n' {
			lineEnd++
		}
		heading := markdownHeading(runes[lineStart:lineEnd])
		if heading && lineStart > sectionStart {
			sections = append(sections, [2]int{sectionStart, lineStart})
			sectionStart = lineStart
		}
		lineStart = lineEnd + 1
	}
	sections = append(sections, [2]int{sectionStart, len(runes)})

	chunks := make([]TextChunk, 0)
	for _, section := range sections {
		for _, chunk := range windowChunks(string(runes[section[0]:section[1]]), 800, 80) {
			chunk.Start += section[0]
			chunk.End += section[0]
			chunk.Index = len(chunks)
			chunks = append(chunks, chunk)
		}
	}
	return chunks
}

func markdownHeading(line []rune) bool {
	level := 0
	for _, r := range line {
		if r != '#' {
			break
		}
		level++
	}
	return level >= 1 && level <= 3 && level < len(line) && unicode.IsSpace(line[level])
}
