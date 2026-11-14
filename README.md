# codegen

MCP (Model Context Protocol) server untuk memory tools AI berbasis **ChromaDB**. Server ini memungkinkan AI menyimpan, mencari secara semantik, dan menghapus context/memory di penyimpanan vektor lokal ChromaDB.

## Quick Start (One-Command Setup)

### Opsi 1: Installer via `curl`

```bash
curl -LsSf https://raw.githubusercontent.com/zerochirou/codegen/master/scripts/install.sh | sh
```

### Opsi 2: Langsung via `uvx` / `uv tool` (tanpa clone)

```bash
# Jalankan langsung tanpa install permanen
uvx --from git+https://github.com/zerochirou/codegen codegen-mcp

# Atau install global sebagai CLI tool
uv tool install git+https://github.com/zerochirou/codegen
```

## Struktur Project

```text
.
├── src/codegen/
│   ├── __init__.py
│   ├── chroma.py        # Helper client & collection ChromaDB
│   └── mcp_server.py    # MCP server (save_memory, search_memory, delete_memory)
├── tests/
│   ├── test_chroma.py
│   └── test_mcp_server.py
├── .env.example
├── pyproject.toml
└── README.md
```

## Setup

1. Install dependensi menggunakan `uv`:

```bash
uv sync --extra dev
```

2. Salin konfigurasi environment:

```bash
cp .env.example .env
```

Variabel di `.env`:
- `CHROMA_PERSIST_DIR`: direktori penyimpanan ChromaDB (default: `./chroma_data`)
- `CHROMA_COLLECTION_NAME`: nama collection default (default: `default`)
- `MCP_SERVER_NAME`: nama MCP server (default: `codegen-mcp`)

## MCP Tools

- `save_memory(content, memory_id=None, tags=None, node_type=None, links=None, collection_name=None)` — Menyimpan atau memperbarui knowledge node/memory ke ChromaDB.
- `search_memory(query, n_results=5, collection_name=None)` — Mencari memory berdasarkan kemiripan semantik.
- `delete_memory(memory_id, collection_name=None)` — Menghapus memory berdasarkan `memory_id`.

## Skills (`/codegen`)

Direktori `.agents/skills/` menyediakan workflow engineering terstruktur yang terintegrasi dengan `codegen-memory`:

- **`/codegen <task>`** — Mode end-to-end (`recall` → `architect` → `impact` → `build` → `review` → `ship`) dengan **auto-checkpoint** di setiap perpindahan fase.
- **`/recall`**, **`/architect`**, **`/impact`**, **`/build`**, **`/review`**, **`/ship`** — Skill modular untuk masing-masing fase.
- **`/handoff`** — Simpan atau lanjutkan checkpoint aktif saat berpindah agent.
- **`/remember`**, **`/update-memory`**, **`/forget`** — Kelola knowledge nodes (`node_type`, `links`, `tags`) di ChromaDB.
- **`/codegen-setup`**, **`/codegen-help`** — Health check setup dan panduan referensi cepat.

## Menjalankan MCP Server

```bash
uv run codegen-mcp
```

### Konfigurasi di Antigravity CLI

Tambahkan ke `~/.gemini/antigravity-cli/mcp_config.json` (global) atau `.gemini/mcp_config.json` (per-project):

```json
{
  "mcpServers": {
    "codegen-memory": {
      "command": "uv",
      "args": [
        "--directory",
        "/Users/zerochirou/Developer/Projects/codegen",
        "run",
        "codegen-mcp"
      ]
    }
  }
}
```

### Konfigurasi di VS Code

Tambahkan ke `.vscode/mcp.json`:

```json
{
  "servers": {
    "codegen-memory": {
      "type": "stdio",
      "command": "uv",
      "args": [
        "--directory",
        "/Users/zerochirou/Developer/Projects/codegen",
        "run",
        "codegen-mcp"
      ]
    }
  }
}
```

## Testing & Linting

```bash
uv run pytest
uv run ruff check src tests
```
