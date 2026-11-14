"""Tests for codegen CLI and persist_dir resolution."""

from __future__ import annotations

import json
import os
from pathlib import Path
from unittest.mock import patch

from codegen import cli
from codegen.chroma import resolve_persist_dir


def test_resolve_persist_dir_project_mcp_config(tmp_path: Path, monkeypatch) -> None:
    monkeypatch.delenv("CHROMA_PERSIST_DIR", raising=False)
    monkeypatch.chdir(tmp_path)

    mcp_dir = tmp_path / ".gemini"
    mcp_dir.mkdir()
    custom_db = tmp_path / "custom_chroma"
    (mcp_dir / "mcp_config.json").write_text(
        json.dumps(
            {
                "mcpServers": {
                    "codegen-memory": {
                        "command": "codegen",
                        "args": ["run"],
                        "env": {"CHROMA_PERSIST_DIR": str(custom_db)},
                    }
                }
            }
        ),
        encoding="utf-8",
    )

    assert resolve_persist_dir(is_global=False) == custom_db


def test_resolve_persist_dir_project_default(tmp_path: Path, monkeypatch) -> None:
    monkeypatch.delenv("CHROMA_PERSIST_DIR", raising=False)
    monkeypatch.chdir(tmp_path)

    assert resolve_persist_dir(is_global=False) == tmp_path / "chroma_data"


def test_resolve_persist_dir_global(tmp_path: Path, monkeypatch) -> None:
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.setenv("CHROMA_PERSIST_DIR", "/should/be/ignored/for/global")

    assert resolve_persist_dir(is_global=True) == tmp_path / ".codegen" / "chroma_data"


def test_cli_no_args_prints_help(capsys) -> None:
    with patch("codegen.cli.tui_main") as mock_tui:
        cli.main([])
        mock_tui.assert_not_called()

    captured = capsys.readouterr()
    assert "usage: codegen" in captured.out


def test_cli_version(capsys) -> None:
    cli.main(["version"])
    captured = capsys.readouterr()
    assert captured.out.startswith("codegen ")


def test_cli_tui_project_and_global(tmp_path: Path, monkeypatch) -> None:
    monkeypatch.delenv("CHROMA_PERSIST_DIR", raising=False)
    monkeypatch.chdir(tmp_path)
    monkeypatch.setenv("HOME", str(tmp_path / "home"))

    with patch("codegen.cli.tui_main") as mock_tui:
        cli.main(["tui"])
        mock_tui.assert_called_once()
        assert os.environ["CHROMA_PERSIST_DIR"] == str(tmp_path / "chroma_data")

    with patch("codegen.cli.tui_main") as mock_tui:
        cli.main(["tui", "--global"])
        mock_tui.assert_called_once()
        assert os.environ["CHROMA_PERSIST_DIR"] == str(tmp_path / "home" / ".codegen" / "chroma_data")
