"""Tests for the ChromaDB Memory Inspector TUI."""

from pathlib import Path

import pytest

from codegen.mcp_server import save_memory
from codegen.tui import MemoryTUI


@pytest.mark.anyio
async def test_tui_displays_memories(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("CHROMA_PERSIST_DIR", str(tmp_path / "chroma"))
    monkeypatch.setenv("CHROMA_COLLECTION_NAME", "test_tui")

    save_memory(
        content="Context untuk testing TUI inspector.",
        memory_id="tui-1",
        tags=["tui", "test"],
    )

    app = MemoryTUI()
    async with app.run_test() as pilot:
        await pilot.pause()
        assert "tui-1" in app._records
        assert "Context untuk testing TUI inspector." in app._records["tui-1"]["content"]
