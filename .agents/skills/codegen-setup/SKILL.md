---
name: codegen-setup
description: Set up and verify the codegen ecosystem (codegen-memory MCP server, ChromaDB storage, skills, and MemoryTUI).
---

# /codegen-setup — Setup & Health Check

Use this skill to initialize or verify the `codegen` environment on a machine or workspace.

## Steps
1. **Verify Python & Dependencies**: Check that `uv` / Python environment is ready (`uv sync` if needed) and `chromadb` + `mcp` are installed.
2. **Verify MCP Server (`codegen-memory`)**: Confirm `codegen-memory` is registered and `save_memory`, `search_memory`, and `delete_memory` are callable.
3. **Verify ChromaDB Storage**: Check that the persistent ChromaDB directory exists and is writable.
4. **Verify Skills**: Confirm `.agents/skills/` contains the core workflow skills (`/codegen`, `/recall`, `/architect`, `/impact`, `/build`, `/review`, `/ship`, `/handoff`).
5. **Report Status**: Output a concise pass/fail checklist and exact next steps if anything is missing.
