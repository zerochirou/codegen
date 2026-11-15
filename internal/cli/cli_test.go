package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/zerochirou/codegen/internal/skills"
	"github.com/zerochirou/codegen/internal/tui"
)

func TestCLIDefaultShowsHelpNotTUI(t *testing.T) {
	var out bytes.Buffer
	tuiCalled := false
	cmd := NewRootCmd(Deps{
		RunTUI: func(_ tui.Options) error {
			tuiCalled = true
			return nil
		},
		Out: &out,
		Err: &out,
	})
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if tuiCalled {
		t.Fatalf("bare `codegen` should show help, not launch TUI")
	}
	if !strings.Contains(out.String(), "Available Commands:") {
		t.Fatalf("expected help output, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "██████╗") {
		t.Fatalf("expected CODEGEN banner in help output, got:\n%s", out.String())
	}
}

func TestCLIVersion(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"--version"}, {"-v"}} {
		var out bytes.Buffer
		cmd := NewRootCmd(Deps{Out: &out, Err: &out})
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("Execute(%v) failed: %v", args, err)
		}
		if !strings.HasPrefix(out.String(), "codegen ") {
			t.Fatalf("Execute(%v) = %q, want prefix 'codegen '", args, out.String())
		}
	}
}

func TestCLITUIProjectAndGlobal(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CHROMA_PERSIST_DIR", "")
	t.Setenv("CODEGEN_GLOBAL_CHROMA_DIR", "")

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}

	evalProject, err := filepath.EvalSymlinks(project)
	if err != nil {
		evalProject = project
	}

	var gotOpts tui.Options
	run := func(args []string) tui.Options {
		var out bytes.Buffer
		cmd := NewRootCmd(Deps{
			RunTUI: func(opts tui.Options) error {
				gotOpts = opts
				return nil
			},
			Out: &out,
			Err: &out,
		})
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("Execute(%v) failed: %v", args, err)
		}
		return gotOpts
	}

	projOpts := run([]string{"tui"})
	if projOpts.IsGlobal {
		t.Fatalf("expected IsGlobal=false")
	}
	if projOpts.PersistDir != filepath.Join(evalProject, "chroma_data") {
		t.Fatalf("project PersistDir = %q, want %q", projOpts.PersistDir, filepath.Join(evalProject, "chroma_data"))
	}

	globalOpts := run([]string{"tui", "--global"})
	if !globalOpts.IsGlobal {
		t.Fatalf("expected IsGlobal=true")
	}
	if globalOpts.PersistDir != filepath.Join(home, ".codegen", "chroma_data") {
		t.Fatalf("global PersistDir = %q", globalOpts.PersistDir)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestCLISkillsUpdateAndListPerProject(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)

	// Initialize existing project markers and an older installed skill + custom skill.
	if err := os.MkdirAll(filepath.Join(project, ".codegen"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".codegen", "config.json"), []byte(`{"project_name":"demo","persist_dir":"chroma_data"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(project, ".agents", "skills", "recall"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".agents", "skills", "recall", "SKILL.md"), []byte("# recall v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(project, ".agents", "skills", "custom-local"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".agents", "skills", "custom-local", "SKILL.md"), []byte("# custom\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	subDir := filepath.Join(project, "src", "api")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	if err := os.Chdir(subDir); err != nil {
		t.Fatal(err)
	}

	bundledFS := fstest.MapFS{
		".agents/skills/recall/SKILL.md":    &fstest.MapFile{Data: []byte("# recall v2\n")},
		".agents/skills/create-pr/SKILL.md": &fstest.MapFile{Data: []byte("# create-pr v2\n")},
		".agents/rules/antislop.md":         &fstest.MapFile{Data: []byte("---\ntrigger: always_on\n---\nrule v2\n")},
	}

	runCLI := func(deps Deps, args ...string) string {
		var out bytes.Buffer
		deps.Out = &out
		deps.Err = &out
		cmd := NewRootCmd(deps)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("Execute(%v) failed: %v\nOutput:\n%s", args, err, out.String())
		}
		return out.String()
	}

	// 1. `codegen skills` shows help.
	helpOut := runCLI(Deps{SkillsFS: bundledFS}, "skills")
	if !strings.Contains(helpOut, "update") || !strings.Contains(helpOut, "list") {
		t.Fatalf("expected skills subcommands in help:\n%s", helpOut)
	}

	// 2. `codegen skills list` from subdir detects project root and reports statuses.
	listOut := runCLI(Deps{SkillsFS: bundledFS}, "skills", "list")
	if !strings.Contains(listOut, "[new]") || !strings.Contains(listOut, "create-pr") ||
		!strings.Contains(listOut, "[outdated]") || !strings.Contains(listOut, "recall") ||
		!strings.Contains(listOut, "[custom]") || !strings.Contains(listOut, "custom-local") {
		t.Fatalf("unexpected list output:\n%s", listOut)
	}

	// 3. `codegen skills list --json` produces valid SyncResult JSON.
	jsonOut := runCLI(Deps{SkillsFS: bundledFS}, "skills", "list", "--json")
	var parsed skills.SyncResult
	if err := json.Unmarshal([]byte(jsonOut), &parsed); err != nil {
		t.Fatalf("invalid list --json output: %v\n%s", err, jsonOut)
	}
	if parsed.AddedCount != 1 || parsed.UpdatedCount != 1 || len(parsed.CustomSkills) != 1 {
		t.Fatalf("unexpected parsed JSON result: %+v", parsed)
	}

	// 4. `codegen skills update --dry-run` does not modify files.
	dryOut := runCLI(Deps{SkillsFS: bundledFS}, "skills", "update", "--dry-run")
	if !strings.Contains(dryOut, "Dry run: 1 to add, 1 to update, 0 up-to-date.") {
		t.Fatalf("unexpected dry-run output:\n%s", dryOut)
	}
	if _, err := os.Stat(filepath.Join(project, ".agents", "skills", "create-pr")); !os.IsNotExist(err) {
		t.Fatalf("dry-run should not install create-pr")
	}

	// 5. `codegen skills update` applies updates and preserves custom skills.
	updOut := runCLI(Deps{SkillsFS: bundledFS}, "skills", "update")
	if !strings.Contains(updOut, "Done: 1 added, 1 updated, 0 up-to-date.") {
		t.Fatalf("unexpected update output:\n%s", updOut)
	}
	if got, _ := os.ReadFile(filepath.Join(project, ".agents", "skills", "create-pr", "SKILL.md")); string(got) != "# create-pr v2\n" {
		t.Fatalf("create-pr not installed: %q", string(got))
	}
	if got, _ := os.ReadFile(filepath.Join(project, ".agents", "skills", "recall", "SKILL.md")); string(got) != "# recall v2\n" {
		t.Fatalf("recall not updated: %q", string(got))
	}
	if got, _ := os.ReadFile(filepath.Join(project, ".agents", "skills", "custom-local", "SKILL.md")); string(got) != "# custom\n" {
		t.Fatalf("custom skill altered: %q", string(got))
	}
	if got, _ := os.ReadFile(filepath.Join(project, ".agents", "rules", "antislop.md")); !strings.Contains(string(got), "rule v2") {
		t.Fatalf("rule not installed: %q", string(got))
	}

	// 6. `codegen skills update --global` updates ~/.gemini/config/skills & plugin rules.
	globalOut := runCLI(Deps{SkillsFS: bundledFS}, "skills", "update", "--global")
	if !strings.Contains(globalOut, "Done: 2 added, 0 updated, 0 up-to-date.") {
		t.Fatalf("unexpected global update output:\n%s", globalOut)
	}
	if _, err := os.Stat(filepath.Join(home, ".gemini", "config", "plugins", "codegen-rules", "rules", "AGENTS.md")); err != nil {
		t.Fatalf("expected global AGENTS.md rule: %v", err)
	}
}

func TestCLISkillsUpdateRemote(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range map[string]string{
		"codegen-master/.agents/skills/remote-skill/SKILL.md": "# remote skill\n",
		"codegen-master/.agents/rules/antislop.md":            "---\ntrigger: always_on\n---\nremote rule\n",
	} {
		data := []byte(body)
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	_ = tw.Close()
	_ = gz.Close()

	client := &http.Client{
		Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(buf.Bytes())),
				Header:     make(http.Header),
			}, nil
		}),
	}

	var out bytes.Buffer
	cmd := NewRootCmd(Deps{
		RemoteHTTPClient: client,
		Out:              &out,
		Err:              &out,
	})
	cmd.SetArgs([]string{"skills", "update", "--path", project, "--remote"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("skills update --remote failed: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "remote (github.com/zerochirou/codegen@master)") {
		t.Fatalf("expected remote source line in output:\n%s", out.String())
	}
	if got, _ := os.ReadFile(filepath.Join(project, ".agents", "skills", "remote-skill", "SKILL.md")); string(got) != "# remote skill\n" {
		t.Fatalf("remote skill not written: %q", string(got))
	}
}
