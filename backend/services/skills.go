package services

import (
	"errors"
	"strings"
	"unicode/utf8"
)

var ErrInvalidSkill = errors.New("技能文件格式不正确")

// SkillDocument is the parsed course-specific plain text format, not SKILL.md.
type SkillDocument struct {
	Name        string
	TriggerText string
	SubjectText string
	Tools       []string
	Enabled     bool
	Description string
	SourceText  string
}

func ParseSkillDocument(raw string) (SkillDocument, error) {
	if !utf8.ValidString(raw) || utf8.RuneCountInString(raw) > 8000 {
		return SkillDocument{}, ErrInvalidSkill
	}
	parts := strings.SplitN(strings.ReplaceAll(raw, "\r\n", "\n"), "\n\n", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
		return SkillDocument{}, ErrInvalidSkill
	}
	values := map[string]string{}
	for _, line := range strings.Split(parts[0], "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "：")
		if !ok {
			key, value, ok = strings.Cut(line, ":")
		}
		if !ok {
			return SkillDocument{}, ErrInvalidSkill
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		switch key {
		case "名称", "适用条件", "资料", "工具", "启用":
		default:
			return SkillDocument{}, ErrInvalidSkill
		}
		if _, exists := values[key]; exists {
			return SkillDocument{}, ErrInvalidSkill
		}
		values[key] = value
	}
	for _, required := range []string{"名称", "适用条件", "启用"} {
		if strings.TrimSpace(values[required]) == "" {
			return SkillDocument{}, ErrInvalidSkill
		}
	}
	if values["启用"] != "是" && values["启用"] != "否" {
		return SkillDocument{}, ErrInvalidSkill
	}
	if utf8.RuneCountInString(values["名称"]) > 32 || utf8.RuneCountInString(values["适用条件"]) > 120 || utf8.RuneCountInString(values["资料"]) > 32 || utf8.RuneCountInString(parts[1]) > 2000 {
		return SkillDocument{}, ErrInvalidSkill
	}
	tools := []string{}
	if value := strings.TrimSpace(values["工具"]); value != "" {
		if value != "knowledge_search" {
			return SkillDocument{}, ErrInvalidSkill
		}
		tools = append(tools, value)
	}
	return SkillDocument{
		Name: strings.TrimSpace(values["名称"]), TriggerText: strings.TrimSpace(values["适用条件"]),
		SubjectText: strings.TrimSpace(values["资料"]), Tools: tools, Enabled: values["启用"] == "是",
		Description: strings.TrimSpace(parts[1]), SourceText: raw,
	}, nil
}

// SelectSkill deterministically chooses at most one enabled bound skill.
// Ties retain the lowest ID; zero-score skills are never selected.
func SelectSkill(query string, candidates []RoutedSkill) *RoutedSkill {
	query = strings.ToLower(query)
	var selected *RoutedSkill
	best := 0
	for i := range candidates {
		s := &candidates[i]
		if !s.Enabled || !s.Bound {
			continue
		}
		score := 0
		for _, word := range splitSkillTerms(s.TriggerText) {
			if word != "" && strings.Contains(query, strings.ToLower(word)) {
				score++
			}
		}
		if score > best || (score == best && score > 0 && (selected == nil || s.ID < selected.ID)) {
			best, selected = score, s
		}
	}
	return selected
}

type RoutedSkill struct {
	ID          int64
	Name        string
	TriggerText string
	Description string
	SubjectText string
	Enabled     bool
	Bound       bool
}

func PromptTemplateInstruction(appKey, templateKey string) (string, bool) {
	if templateKey == "" || templateKey == "free" {
		return "", true
	}
	sets := map[string]map[string]string{
		"lecture": {"explain": "请用通俗中文解释问题中的课程概念，并给一个简短例子。", "locate": "请优先指出相关讲义依据和位置，再回答问题。"},
		"guided":  {"next": "请只给出一个可执行的下一步提示，先接住我已经尝试的内容。", "debug": "请按从最容易检查到较深入的顺序，引导我排查问题，不要直接替我完成全部工作。"},
		"review":  {"outline": "请整理成复习提纲，包含核心概念、它们之间的关系和容易混淆之处。", "quiz": "请整理关键知识点，并给出3个自测问题；把答案放在问题后供我对照。"},
	}
	text, ok := sets[appKey][templateKey]
	return text, ok
}

func splitSkillTerms(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		switch r {
		case ',', '，', '、', ';', '；', '|':
			return true
		default:
			return false
		}
	})
}
