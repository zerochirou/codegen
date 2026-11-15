package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestResolveTargetAndFindProjectRoot(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()

	// Create a project with .codegen/config.json and a nested subdir.
	proj := filepath.Join(workspace, "app")
	sub := filepath.Join(proj, "internal", "pkg")
	if err := os.MkdirAll(filepath.Join(proj, ".codegen"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, ".codegen", "config.json"), []byte(`{"persist_dir":"chroma_data"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	target, err := ResolveTarget(TargetOptions{Cwd: sub, HomeDir: home})
	if err != nil {
		t.Fatalf("ResolveTarget from subdir failed: %v", err)
	}
	if target.Scope != "project" || target.RootDir != proj {
		t.Fatalf("got target %+v, want root %s", target, proj)
	}
	if target.SkillsDir != filepath.Join(proj, ".agents", "skills") {
		t.Fatalf("SkillsDir = %q", target.SkillsDir)
	}

	// Git repo fallback when no .codegen or .agents marker exists yet.
	gitRepo := filepath.Join(workspace, "git-only")
	gitSub := filepath.Join(gitRepo, "cmd", "api")
	if err := os.MkdirAll(filepath.Join(gitRepo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(gitSub, 0o755); err != nil {
		t.Fatal(err)
	}
	gitTarget, err := ResolveTarget(TargetOptions{Cwd: gitSub, HomeDir: home})
	if err != nil {
		t.Fatalf("ResolveTarget from git subdir failed: %v", err)
	}
	if gitTarget.RootDir != gitRepo {
		t.Fatalf("gitRoot = %q, want %q", gitTarget.RootDir, gitRepo)
	}

	// Running directly inside home without --global or --path should fail safely.
	if _, err := ResolveTarget(TargetOptions{Cwd: home, HomeDir: home}); err == nil {
		t.Fatalf("expected error when running inside home directory without --global or --path")
	}

	// Global and path together must fail.
	if _, err := ResolveTarget(TargetOptions{Global: true, ProjectPath: proj, HomeDir: home}); err == nil {
		t.Fatalf("expected error when both --global and --path are set")
	}

	// Global target paths.
	globalTarget, err := ResolveTarget(TargetOptions{Global: true, HomeDir: home})
	if err != nil {
		t.Fatalf("ResolveTarget --global failed: %v", err)
	}
	if globalTarget.SkillsDir != filepath.Join(home, ".gemini", "config", "skills") {
		t.Fatalf("global SkillsDir = %q", globalTarget.SkillsDir)
	}
	if globalTarget.RulesDir != filepath.Join(home, ".gemini", "config", "plugins", "codegen-rules") {
		t.Fatalf("global RulesDir = %q", globalTarget.RulesDir)
	}
}

func TestSyncAddUpdateDryRunAndPreserveCustom(t *testing.T) {
	project := t.TempDir()
	skillsDir := filepath.Join(project, ".agents", "skills")
	rulesDir := filepath.Join(project, ".agents", "rules")

	// Pre-install a custom skill that must never be touched or removed.
	customDir := filepath.Join(skillsDir, "my-custom")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatal(err)
	}
	customContent := []byte("# custom skill\n")
	if err := os.WriteFile(filepath.Join(customDir, "SKILL.md"), customContent, 0o644); err != nil {
		t.Fatal(err)
	}

	v1FS := fstest.MapFS{
		".agents/skills/recall/SKILL.md":         &fstest.MapFile{Data: []byte("# recall v1\n")},
		".agents/skills/antislop-human/SKILL.md": &fstest.MapFile{Data: []byte("# human v1\n")},
		".agents/skills/antislop-human/check.py": &fstest.MapFile{Data: []byte("print('v1')\n")},
		".agents/rules/antislop.md":              &fstest.MapFile{Data: []byte("---\ntrigger: always_on\n---\nrule v1\n")},
	}

	target := projectTarget(project)
	res1, err := Sync(SyncOptions{Target: target, SkillsFS: v1FS, IncludeRules: true})
	if err != nil {
		t.Fatalf("initial Sync failed: %v", err)
	}
	if res1.AddedCount != 2 || res1.UpdatedCount != 0 || res1.UnchangedCount != 0 {
		t.Fatalf("unexpected v1 counts: %+v", res1)
	}
	if len(res1.CustomSkills) != 1 || res1.CustomSkills[0] != "my-custom" {
		t.Fatalf("expected custom skill preserved, got %v", res1.CustomSkills)
	}

	// Re-running with identical bundle must report all up-to-date.
	resRepeat, err := Sync(SyncOptions{Target: target, SkillsFS: v1FS, IncludeRules: true})
	if err != nil {
		t.Fatalf("repeat Sync failed: %v", err)
	}
	if resRepeat.AddedCount != 0 || resRepeat.UpdatedCount != 0 || resRepeat.UnchangedCount != 2 {
		t.Fatalf("expected all up-to-date on repeat sync: %+v", resRepeat)
	}
	if len(resRepeat.Rules) != 1 || resRepeat.Rules[0].Status != StatusUpToDate {
		t.Fatalf("expected rule up-to-date, got %+v", resRepeat.Rules)
	}

	// v2 introduces a new skill (create-pr), updates recall + rule, keeps antislop-human unchanged.
	v2FS := fstest.MapFS{
		".agents/skills/recall/SKILL.md":         &fstest.MapFile{Data: []byte("# recall v2\n")},
		".agents/skills/antislop-human/SKILL.md": &fstest.MapFile{Data: []byte("# human v1\n")},
		".agents/skills/antislop-human/check.py": &fstest.MapFile{Data: []byte("print('v1')\n")},
		".agents/skills/create-pr/SKILL.md":      &fstest.MapFile{Data: []byte("# create-pr v2\n")},
		".agents/rules/antislop.md":              &fstest.MapFile{Data: []byte("---\ntrigger: always_on\n---\nrule v2\n")},
	}

	// Dry-run / Inspect must report changes without modifying disk.
	dryRes, err := Inspect(SyncOptions{Target: target, SkillsFS: v2FS, IncludeRules: true})
	if err != nil {
		t.Fatalf("Inspect failed: %v", err)
	}
	if dryRes.AddedCount != 1 || dryRes.UpdatedCount != 1 || dryRes.UnchangedCount != 1 {
		t.Fatalf("unexpected dry-run counts: %+v", dryRes)
	}
	if _, err := os.Stat(filepath.Join(skillsDir, "create-pr")); !os.IsNotExist(err) {
		t.Fatalf("dry-run should not create new skill directory")
	}
	if got, _ := os.ReadFile(filepath.Join(skillsDir, "recall", "SKILL.md")); string(got) != "# recall v1\n" {
		t.Fatalf("dry-run modified recall skill: %q", string(got))
	}

	// Selective update of only `create-pr` must not alter `recall` or rules.
	selRes, err := Sync(SyncOptions{
		Target:       target,
		SkillsFS:     v2FS,
		FilterNames:  []string{"create-pr"},
		IncludeRules: true,
	})
	if err != nil {
		t.Fatalf("selective Sync failed: %v", err)
	}
	if selRes.AddedCount != 1 || selRes.TotalBundled != 1 || len(selRes.Rules) != 0 {
		t.Fatalf("unexpected selective result: %+v", selRes)
	}
	if got, _ := os.ReadFile(filepath.Join(skillsDir, "recall", "SKILL.md")); string(got) != "# recall v1\n" {
		t.Fatalf("selective sync should not update recall: %q", string(got))
	}

	// Unknown skill filter returns clear error.
	if _, err := Sync(SyncOptions{Target: target, SkillsFS: v2FS, FilterNames: []string{"does-not-exist"}}); err == nil {
		t.Fatalf("expected error for unknown bundled skill name")
	}

	// Full v2 sync updates recall + rule, keeps create-pr and antislop-human up-to-date, and preserves custom skill.
	res2, err := Sync(SyncOptions{Target: target, SkillsFS: v2FS, IncludeRules: true})
	if err != nil {
		t.Fatalf("v2 Sync failed: %v", err)
	}
	if res2.AddedCount != 0 || res2.UpdatedCount != 1 || res2.UnchangedCount != 2 {
		t.Fatalf("unexpected v2 counts: %+v", res2)
	}
	if got, _ := os.ReadFile(filepath.Join(skillsDir, "recall", "SKILL.md")); string(got) != "# recall v2\n" {
		t.Fatalf("recall not updated: %q", string(got))
	}
	if got, _ := os.ReadFile(filepath.Join(rulesDir, "antislop.md")); !strings.Contains(string(got), "rule v2") {
		t.Fatalf("rule not updated: %q", string(got))
	}
	if got, _ := os.ReadFile(filepath.Join(customDir, "SKILL.md")); string(got) != string(customContent) {
		t.Fatalf("custom skill was altered: %q", string(got))
	}
}

func TestSyncGlobalRulesPluginLifecycle(t *testing.T) {
	home := t.TempDir()
	target, err := ResolveTarget(TargetOptions{Global: true, HomeDir: home})
	if err != nil {
		t.Fatal(err)
	}

	fsV1 := fstest.MapFS{
		".agents/skills/codegen/SKILL.md": &fstest.MapFile{Data: []byte("# codegen\n")},
		".agents/rules/antislop.md":       &fstest.MapFile{Data: []byte("---\ntrigger: always_on\n---\n# global rule v1\n")},
	}
	r1, err := Sync(SyncOptions{Target: target, SkillsFS: fsV1, IncludeRules: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(r1.Rules) != 1 || r1.Rules[0].Status != StatusAdded {
		t.Fatalf("expected global rule added, got %+v", r1.Rules)
	}
	agentsMD := filepath.Join(target.RulesDir, "rules", "AGENTS.md")
	data, err := os.ReadFile(agentsMD)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "trigger: always_on") || !strings.Contains(string(data), "global rule v1") {
		t.Fatalf("unexpected global AGENTS.md content: %q", string(data))
	}

	// Repeat sync should report up-to-date.
	r2, err := Sync(SyncOptions{Target: target, SkillsFS: fsV1, IncludeRules: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(r2.Rules) != 1 || r2.Rules[0].Status != StatusUpToDate {
		t.Fatalf("expected global rule up-to-date, got %+v", r2.Rules)
	}

	// Rule change should report updated and write new content.
	fsV2 := fstest.MapFS{
		".agents/skills/codegen/SKILL.md": &fstest.MapFile{Data: []byte("# codegen\n")},
		".agents/rules/antislop.md":       &fstest.MapFile{Data: []byte("---\ntrigger: always_on\n---\n# global rule v2\n")},
	}
	r3, err := Sync(SyncOptions{Target: target, SkillsFS: fsV2, IncludeRules: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(r3.Rules) != 1 || r3.Rules[0].Status != StatusUpdated {
		t.Fatalf("expected global rule updated, got %+v", r3.Rules)
	}
	if updated, _ := os.ReadFile(agentsMD); !strings.Contains(string(updated), "global rule v2") {
		t.Fatalf("global AGENTS.md not updated: %q", string(updated))
	}
}
