package chroma

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePersistDir_ProjectVsGlobal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CHROMA_PERSIST_DIR", "")
	t.Setenv("CODEGEN_GLOBAL_CHROMA_DIR", "")

	projectDir := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	if err := os.Chdir(projectDir); err != nil {
		t.Fatal(err)
	}

	// Default project path (no config yet) -> <project>/chroma_data
	gotProject := ResolvePersistDir(false)
	evalProjectDir, err := filepath.EvalSymlinks(projectDir)
	if err != nil {
		evalProjectDir = projectDir
	}
	wantProject := filepath.Join(evalProjectDir, "chroma_data")
	if gotProject != wantProject {
		t.Fatalf("ResolvePersistDir(false) = %q, want %q", gotProject, wantProject)
	}

	// Global path -> ~/.codegen/chroma_data
	gotGlobal := ResolvePersistDir(true)
	wantGlobal := filepath.Join(home, ".codegen", "chroma_data")
	if gotGlobal != wantGlobal {
		t.Fatalf("ResolvePersistDir(true) = %q, want %q", gotGlobal, wantGlobal)
	}

	// Write .codegen/config.json with custom persist dir
	customDB := filepath.Join(projectDir, "custom_mem", "chroma_data")
	if err := WriteProjectConfig(projectDir, "demo", customDB); err != nil {
		t.Fatal(err)
	}
	gotConfigured := ResolvePersistDir(false)
	wantConfigured, _ := filepath.EvalSymlinks(projectDir)
	wantConfigured = filepath.Join(wantConfigured, "custom_mem", "chroma_data")
	if gotConfigured != wantConfigured {
		// Compare clean paths in case customDB doesn't exist yet on macOS /var -> /private/var
		if filepath.Base(filepath.Dir(gotConfigured)) != "custom_mem" {
			t.Fatalf("ResolvePersistDir(false) with .codegen/config.json = %q, want %q", gotConfigured, wantConfigured)
		}
	}
}

func TestResolveMCPPersistDir_WorkspaceFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CHROMA_PERSIST_DIR", "")
	t.Setenv("CODEGEN_GLOBAL_CHROMA_DIR", "")

	uninitDir := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	if err := os.Chdir(uninitDir); err != nil {
		t.Fatal(err)
	}

	// Uninitialized workspace falls back to global
	got := ResolveMCPPersistDir(uninitDir)
	wantGlobal := filepath.Join(home, ".codegen", "chroma_data")
	if got != wantGlobal {
		t.Fatalf("ResolveMCPPersistDir(uninitialized) = %q, want %q", got, wantGlobal)
	}

	// Workspace with .gemini/mcp_config.json uses project chroma_data
	ws := t.TempDir()
	geminiDir := filepath.Join(ws, ".gemini")
	if err := os.MkdirAll(geminiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	projDB := filepath.Join(ws, "chroma_data")
	cfgBytes, _ := json.Marshal(map[string]any{
		"mcpServers": map[string]any{
			"codegen-memory": map[string]any{
				"command": "codegen",
				"args":    []string{"run"},
				"env":     map[string]string{"CHROMA_PERSIST_DIR": projDB},
			},
		},
	})
	if err := os.WriteFile(filepath.Join(geminiDir, "mcp_config.json"), cfgBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	gotWS := ResolveMCPPersistDir(ws)
	if gotWS != projDB {
		t.Fatalf("ResolveMCPPersistDir(ws) = %q, want %q", gotWS, projDB)
	}
}

func TestStoreSaveQueryListDelete(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "chroma_data")
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open(%q) failed: %v", dir, err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	id1, err := store.Save(ctx, "test_col", "", "Go CLI replaces Python implementation", map[string]string{
		"tags":      "architecture,golang",
		"node_type": "architecture",
	})
	if err != nil {
		t.Fatalf("Save 1 failed: %v", err)
	}

	_, err = store.Save(ctx, "test_col", "mem-2", "ChromaDB stores semantic vector embeddings locally", map[string]string{
		"tags": "database,chromadb",
	})
	if err != nil {
		t.Fatalf("Save 2 failed: %v", err)
	}

	records, err := store.List(ctx, "test_col")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("List returned %d records, want 2", len(records))
	}

	matches, err := store.Query(ctx, "test_col", "vector database embeddings", 1)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if len(matches) != 1 || matches[0].ID != "mem-2" {
		t.Fatalf("Query returned %+v, want ID mem-2", matches)
	}

	if err := store.Delete(ctx, "test_col", id1); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	recordsAfter, err := store.List(ctx, "test_col")
	if err != nil {
		t.Fatalf("List after delete failed: %v", err)
	}
	if len(recordsAfter) != 1 || recordsAfter[0].ID != "mem-2" {
		t.Fatalf("List after delete = %+v, want only mem-2", recordsAfter)
	}
}
