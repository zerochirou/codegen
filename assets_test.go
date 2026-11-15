package codegen

import (
	"io/fs"
	"strings"
	"testing"
)

func TestBundledAssetsIncludeSkillsAndAlwaysOnRules(t *testing.T) {
	requiredFiles := []string{
		".agents/skills/create-pr/SKILL.md",
		".agents/skills/antislop/SKILL.md",
		".agents/skills/antislop-ui/SKILL.md",
		".agents/skills/antislop-copywriting/SKILL.md",
		".agents/skills/antislop-human/SKILL.md",
		".agents/skills/antislop-human/contrast-check.py",
		".agents/skills/antislop-layoutmobile/SKILL.md",
		".agents/skills/antislop-code/SKILL.md",
		".agents/rules/antislop.md",
	}
	for _, rel := range requiredFiles {
		data, err := fs.ReadFile(BundledSkills, rel)
		if err != nil {
			t.Fatalf("missing embedded asset %s: %v", rel, err)
		}
		if len(data) == 0 {
			t.Fatalf("embedded asset %s is empty", rel)
		}
	}

	ruleRaw, _ := fs.ReadFile(BundledSkills, ".agents/rules/antislop.md")
	if !strings.Contains(string(ruleRaw), "trigger: always_on") {
		t.Fatalf("expected trigger: always_on in .agents/rules/antislop.md")
	}
}
