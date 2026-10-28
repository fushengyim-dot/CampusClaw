package services

import "testing"

func TestParseSkillDocument(t *testing.T) {
	raw := "# note\n名称：数学解题引导\n适用条件：方程,去括号｜分数\n资料：数学\n工具：knowledge_search\n启用：是\n\n先指出下一步。"
	skill, err := ParseSkillDocument(raw)
	if err != nil || skill.Name != "数学解题引导" || !skill.Enabled || skill.SubjectText != "数学" || len(skill.Tools) != 1 || skill.Description != "先指出下一步。" {
		t.Fatalf("unexpected parse: %#v, %v", skill, err)
	}
}

func TestParseSkillRejectsInvalidDocuments(t *testing.T) {
	cases := []string{
		"名称：x\n适用条件：q\n启用：是\n未知：字段\n\nbody",
		"名称：x\n名称：y\n适用条件：q\n启用：是\n\nbody",
		"名称：x\n适用条件：q\n启用：是\n工具：shell\n\nbody",
		"名称：x\n适用条件：q\n启用：可能\n\nbody",
		"名称：x\n适用条件：q\n启用：是",
	}
	for i, raw := range cases {
		if _, err := ParseSkillDocument(raw); err == nil {
			t.Errorf("case %d unexpectedly accepted", i)
		}
	}
}

func TestSelectSkillBoundedDeterministicAndSingle(t *testing.T) {
	candidates := []RoutedSkill{
		{ID: 8, Name: "unbound", TriggerText: "方程,去括号", Enabled: true, Bound: false},
		{ID: 7, Name: "disabled", TriggerText: "方程,去括号", Enabled: false, Bound: true},
		{ID: 4, Name: "later", TriggerText: "方程,分数", Enabled: true, Bound: true},
		{ID: 2, Name: "winner", TriggerText: "方程,去括号", Enabled: true, Bound: true},
	}
	got := SelectSkill("这道方程怎么去括号", candidates)
	if got == nil || got.ID != 2 {
		t.Fatalf("expected lowest-ID tie winner, got %#v", got)
	}
	if got := SelectSkill("写一首诗", candidates); got != nil {
		t.Fatalf("zero-score skill selected: %#v", got)
	}
}

func TestPromptTemplatesAreAppScopedAndSeparate(t *testing.T) {
	if got, ok := PromptTemplateInstruction("review", "outline"); !ok || got == "" {
		t.Fatal("review template missing")
	}
	if _, ok := PromptTemplateInstruction("lecture", "outline"); ok {
		t.Fatal("review template leaked to lecture app")
	}
	if got, ok := PromptTemplateInstruction("guided", "free"); !ok || got != "" {
		t.Fatal("free template should add no instruction")
	}
}
