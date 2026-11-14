package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zerochirou/codegen/internal/chroma"
)

func TestMCPInitializeAndToolsList(t *testing.T) {
	srv := NewServer("codegen-mcp", "0.2.0", chroma.NewManager())

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"ping"}`,
	}, "\n") + "\n"

	var out bytes.Buffer
	if err := srv.ServeIO(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatalf("ServeIO failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 JSON-RPC responses, got %d:\n%s", len(lines), out.String())
	}

	var initResp rpcMessage
	if err := json.Unmarshal([]byte(lines[0]), &initResp); err != nil {
		t.Fatal(err)
	}
	if initResp.Error != nil {
		t.Fatalf("initialize error: %+v", initResp.Error)
	}

	var toolsResp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &toolsResp); err != nil {
		t.Fatal(err)
	}
	if len(toolsResp.Result.Tools) != 3 {
		t.Fatalf("expected 3 tools, got %+v", toolsResp.Result.Tools)
	}
}

func TestMCPToolCallsWorkspaceIsolation(t *testing.T) {
	globalDir := filepath.Join(t.TempDir(), "global_chroma")
	t.Setenv("CHROMA_PERSIST_DIR", "")
	t.Setenv("CODEGEN_GLOBAL_CHROMA_DIR", globalDir)

	projectDir := t.TempDir()
	projectChroma := filepath.Join(projectDir, "chroma_data")
	if err := chroma.WriteProjectConfig(projectDir, "demo", projectChroma); err != nil {
		t.Fatal(err)
	}

	srv := NewServer("codegen-mcp", "0.2.0", chroma.NewManager())
	saveReq, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "save_memory",
			"arguments": map[string]any{
				"content":        "Project coffee uses Astro and Tailwind",
				"tags":           []string{"architecture", "astro"},
				"node_type":      "architecture",
				"workspace_path": projectDir,
			},
		},
	})
	searchReq, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "search_memory",
			"arguments": map[string]any{
				"query":          "Astro Tailwind",
				"n_results":      3,
				"workspace_path": projectDir,
			},
		},
	})

	input := string(saveReq) + "\n" + string(searchReq) + "\n"
	var out bytes.Buffer
	if err := srv.ServeIO(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatalf("ServeIO failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 responses, got %d:\n%s", len(lines), out.String())
	}

	var searchResp struct {
		Result struct {
			IsError           bool `json:"isError"`
			StructuredContent struct {
				PersistDir string           `json:"persist_dir"`
				Results    []map[string]any `json:"results"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &searchResp); err != nil {
		t.Fatal(err)
	}
	if searchResp.Result.IsError {
		t.Fatalf("search_memory returned error: %s", lines[1])
	}
	if len(searchResp.Result.StructuredContent.Results) != 1 {
		t.Fatalf("expected 1 search result, got %+v", searchResp.Result.StructuredContent.Results)
	}
}
