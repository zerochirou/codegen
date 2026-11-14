# codegen

CLI, MCP (Model Context Protocol) server, dan MemoryTUI berbasis **100% Golang** dengan penyimpanan vektor lokal **ChromaDB** (`chroma-go` embedded PersistentClient + ONNX `all-MiniLM-L6-v2`).

## Quick Start

### Install dari source (Go >= 1.24)

```bash
go install github.com/zerochirou/codegen/cmd/codegen@latest
```

Atau build lokal dari repo ini:

```bash
./scripts/setup.sh
```

## Perintah CLI

```bash
codegen                # Menampilkan help
codegen --version      # Menampilkan versi (juga `codegen -v` / `codegen version`)
codegen init           # Setup interaktif (Per-project atau Global)
codegen tui            # Buka MemoryTUI untuk ChromaDB per-project/workspace
codegen tui --global   # Buka MemoryTUI untuk ChromaDB global (~/.codegen/chroma_data)
codegen run            # Jalankan MCP server (stdio)
codegen update         # Update binary codegen via `go install`
codegen uninstall      # Hapus binary global codegen
```

## Cara Kerja Scoping (`Per-project` vs `Global`)

- **`codegen init`** (Per-project):
  - Membuat `<project>/chroma_data` (otomatis menambahkan `/chroma_data` ke base path).
  - Menulis `.codegen/config.json` & `.gemini/mcp_config.json` (dan plugin `.agents/plugins/codegen-memory/` jika MCP global dinamis belum aktif).
  - Menyalin bundled skills ke `.agents/skills/`.
- **`codegen init`** (Global):
  - Membuat `~/.codegen/chroma_data` dan `~/.codegen/config.json`.
  - Mendaftarkan `codegen-memory` (`codegen run`) di `~/.gemini/config/mcp_config.json` tanpa mengunci `CHROMA_PERSIST_DIR` ke satu repo, sehingga saat dijalankan di project yang sudah `codegen init`, MCP otomatis menyimpan ke `<project>/chroma_data`, dan di folder lain fallback ke global `~/.codegen/chroma_data`.
- **`codegen tui`**:
  - Tanpa flag: membaca ChromaDB **per-project** dari workspace saat ini.
  - Dengan `--global` (`-g`): membaca ChromaDB **global** (`~/.codegen/chroma_data`).

## MCP Tools

- `save_memory(content, memory_id=None, tags=None, node_type=None, links=None, collection_name=None, workspace_path=None)`
- `search_memory(query, n_results=5, collection_name=None, workspace_path=None)`
- `delete_memory(memory_id, collection_name=None, workspace_path=None)`

## Struktur Project

```text
.
├── assets.go
├── cmd/codegen/
│   └── main.go
├── internal/
│   ├── chroma/       # Path scoping + embedded ChromaDB PersistentClient (ONNX all-MiniLM-L6-v2)
│   ├── cli/          # Cobra CLI entrypoint
│   ├── initwizard/   # Interactive Clack-style setup wizard
│   ├── mcp/          # Stdio MCP server (JSON-RPC 2.0 + roots/list workspace detection)
│   └── tui/          # Bubble Tea + Lipgloss MemoryTUI
├── .agents/skills/   # Workflow & memory skills (di-embed ke binary via go:embed)
├── scripts/
├── go.mod
└── go.sum
```

## Testing

```bash
go test ./...
```
