---
name: setup-codegen-memory
description: Set up and verify the codegen-memory MCP server, ChromaDB storage, environment variables, and MemoryTUI.
---

# /setup-codegen-memory — Setup Codegen Memory

Use this skill when setting up `codegen-memory` in a new environment or troubleshooting its MCP server and TUI.

## Steps
1. **Install dependencies**:
   - Run `uv sync` (or `pip install -e .`) in the repository root.
2. **Configure environment**:
   - Ensure `.env` exists (copy from `.env.example` if needed) with `CHROMA_PERSIST_DIR` and `CHROMA_COLLECTION_NAME`.
3. **Register MCP server**:
   - Point the client's MCP config for `codegen-memory` to the project's `codegen-mcp` entrypoint (e.g., `.venv/bin/codegen-mcp` or `uv run codegen-mcp`) with the project root as working directory.
4. **Verify**:
   - Run `.venv/bin/pytest` to confirm ChromaDB, MCP tools, and TUI tests pass.
   - Optionally launch `.venv/bin/codegen-tui` to inspect stored context.
