"""Tests for MCP memory tools backed by ChromaDB."""

from pathlib import Path

import pytest

from codegen.mcp_server import delete_memory, save_memory, search_memory


def test_mcp_memory_tools(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("CHROMA_PERSIST_DIR", str(tmp_path / "chroma"))
    monkeypatch.setenv("CHROMA_COLLECTION_NAME", "test_memory")

    saved = save_memory(
        content="User prefers concise Indonesian responses.",
        memory_id="pref-1",
        tags=["preference", "language"],
    )
    assert saved == {"id": "pref-1", "status": "saved"}

    results = search_memory(query="bahasa apa yang disukai user?", n_results=1)
    assert len(results) == 1
    assert results[0]["id"] == "pref-1"
    assert "Indonesian" in results[0]["content"]
    assert results[0]["metadata"] == {"tags": "preference,language"}

    deleted = delete_memory(memory_id="pref-1")
    assert deleted == {"id": "pref-1", "status": "deleted"}

    after_delete = search_memory(query="Indonesian", n_results=1)
    assert after_delete == []
