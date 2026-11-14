package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
