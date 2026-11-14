---
name: codegen-help
description: Quick reference guide for all /codegen skills, workflow phases, and memory commands.
---

# /codegen-help — Command & Skill Reference

Present this concise reference when the user asks what `/codegen` can do or which skill to use.

## Core Pipeline
- **`/codegen <task>`** — Full end-to-end engineering mode (`recall` → `architect` → `impact` → `build` → `review` → `ship`).
- **`/recall <topic>`** — Search `codegen-memory` for preferences, past decisions, and context.
- **`/architect <task>`** — Clarify ambiguities/edge cases and draft a technical blueprint.
- **`/impact`** — Analyze blast radius of the plan on existing code, callers, and tests before editing.
- **`/build`** — Implement modular, clean, zero-duplication code.
- **`/review`** — Self-audit diff like a Staff Engineer and run real tests/checks.
- **`/ship`** — Persist durable decisions to `codegen-memory` and summarize verified changes.

## Cross-Agent & Memory Tools
- **`/handoff`** — Save or resume an active workflow checkpoint when switching agents.
- **`/remember <info>`** — Save an atomic knowledge node (`node_type`, `links`, `tags`) to `codegen-memory`.
- **`/update-memory`** — Update an existing memory node in-place by `memory_id`.
- **`/forget`** — Delete obsolete or incorrect memory nodes.
- **`/codegen-setup`** — Verify `codegen-memory` MCP, ChromaDB, and skills setup.
