package initwizard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestNormalizeChromaDir(t *testing.T) {
	base := "/tmp/my-project"
	if got := NormalizeChromaDir("/tmp/my-project", base); got != "/tmp/my-project/chroma_data" {
		t.Fatalf("got %q, want /tmp/my-project/chroma_data", got)
	}
	if got := NormalizeChromaDir("/tmp/my-project/chroma_data", base); got != "/tmp/my-project/chroma_data" {
		t.Fatalf("got %q, want /tmp/my-project/chroma_data", got)
	}
	if got := NormalizeChromaDir("memory", base); got != "/tmp/my-project/memory/chroma_data" {
		t.Fatalf("got %q, want /tmp/my-project/memory/chroma_data", got)
	}
}

func TestApplyProjectAndGlobal(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)

	skillsFS := fstest.MapFS{
		".agents/skills/remember/SKILL.md": &fstest.MapFile{Data: []byte("# remember\n")},
		".agents/skills/recall/SKILL.md":   &fstest.MapFile{Data: []byte("# recall\n")},
	}

	// 1. Project init without global dynamic MCP -> creates plugin + .codegen + .gemini + skills
	res, err := Apply(ApplyConfig{
		IsProject:     true,
		ProjectDir:    project,
		HomeDir:       home,
		ProjectName:   "coffe",
		ChromaDir:     project,
		InstallSkills: true,
		SkillsFS:      skillsFS,
	})
	if err != nil {
		t.Fatalf("Apply project failed: %v", err)
	}
	if res.ChromaDir != filepath.Join(project, "chroma_data") {
		t.Fatalf("ChromaDir = %q", res.ChromaDir)
	}
	if res.SkillsInstalled != 2 {
		t.Fatalf("SkillsInstalled = %d, want 2", res.SkillsInstalled)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "plugins", "codegen-memory", "mcp_config.json")); err != nil {
		t.Fatalf("expected plugin mcp_config.json: %v", err)
	}

	// 2. Global init -> writes ~/.gemini/config/mcp_config.json without CHROMA_PERSIST_DIR
	globalRes, err := Apply(ApplyConfig{
		IsProject:     false,
		ProjectDir:    project,
		HomeDir:       home,
		ProjectName:   "global",
		ChromaDir:     filepath.Join(home, ".codegen"),
		InstallSkills: true,
		SkillsFS:      skillsFS,
	})
	if err != nil {
		t.Fatalf("Apply global failed: %v", err)
	}
	if globalRes.ChromaDir != filepath.Join(home, ".codegen", "chroma_data") {
		t.Fatalf("global ChromaDir = %q", globalRes.ChromaDir)
	}

	raw, err := os.ReadFile(filepath.Join(home, ".gemini", "config", "mcp_config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	srv := parsed["mcpServers"].(map[string]any)["codegen-memory"].(map[string]any)
	if srv["command"] != "codegen" {
		t.Fatalf("unexpected global command: %+v", srv)
	}
	if _, hasEnv := srv["env"]; hasEnv {
		t.Fatalf("global MCP entry should not hardcode env.CHROMA_PERSIST_DIR: %+v", srv)
	}

	// 3. Project init AFTER dynamic global MCP -> skips duplicate plugin
	project2 := t.TempDir()
	_, err = Apply(ApplyConfig{
		IsProject:     true,
		ProjectDir:    project2,
		HomeDir:       home,
		ProjectName:   "coffe2",
		ChromaDir:     project2,
		InstallSkills: false,
		SkillsFS:      skillsFS,
	})
	if err != nil {
		t.Fatalf("Apply project2 failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project2, ".agents", "plugins", "codegen-memory", "mcp_config.json")); !os.IsNotExist(err) {
		t.Fatalf("expected no duplicate plugin when dynamic global MCP is active")
	}
}
