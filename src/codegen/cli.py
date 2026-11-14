"""CLI entrypoint for codegen."""

from __future__ import annotations

import argparse
import json
import shutil
import subprocess
from importlib.metadata import PackageNotFoundError, version
from pathlib import Path

from codegen.init_wizard import run_init_wizard
from codegen.mcp_server import main as mcp_main
from codegen.tui import main as tui_main


def _run_init() -> None:
    """Initialize .gemini/mcp_config.json in the current directory."""
    target = Path.cwd() / ".gemini" / "mcp_config.json"
    target.parent.mkdir(parents=True, exist_ok=True)

    config: dict = {}
    if target.exists():
        try:
            config = json.loads(target.read_text(encoding="utf-8"))
        except json.JSONDecodeError:
            config = {}

    servers = config.setdefault("mcpServers", {})
    servers["codegen-memory"] = {
        "command": "codegen",
        "args": ["run"],
    }
    target.write_text(json.dumps(config, indent=2) + "\n", encoding="utf-8")
    print(f"Initialized codegen-memory in {target}")


def _get_version() -> str:
    """Return installed codegen package version."""
    try:
        return version("codegen")
    except PackageNotFoundError:
        return "0.1.0"


def build_parser() -> argparse.ArgumentParser:
    """Build the top-level CLI parser."""
    parser = argparse.ArgumentParser(
        prog="codegen",
        description="codegen CLI — ChromaDB vector memory, MCP server, and MemoryTUI",
    )
    parser.add_argument("-v", "--version", action="version", version=f"codegen {_get_version()}")
    subparsers = parser.add_subparsers(dest="command")
    subparsers.add_parser("tui", help="Launch the MemoryTUI (default when no command is given)")
    subparsers.add_parser("run", help="Run the codegen-memory MCP server")
    subparsers.add_parser("init", help="Initialize .gemini/mcp_config.json in the current project")
    subparsers.add_parser("update", help="Update the global codegen CLI from GitHub")
    subparsers.add_parser("uninstall", aliases=["remove"], help="Uninstall the global codegen CLI")
    subparsers.add_parser("version", help="Show codegen version")
    subparsers.add_parser("help", help="Show this help message")
    return parser


def main(argv: list[str] | None = None) -> None:
    """CLI entrypoint for `codegen`."""
    parser = build_parser()
    args = parser.parse_args(argv)

    if args.command == "help":
        parser.print_help()
    elif args.command == "version":
        print(f"codegen {_get_version()}")
    elif args.command == "init":
        run_init_wizard()
    elif args.command == "update":
        if not shutil.which("uv"):
            print("Error: `uv` is required to update codegen.")
            return
        subprocess.run(
            ["uv", "tool", "install", "--force", "git+https://github.com/zerochirou/codegen"],
            check=False,
        )
    elif args.command in ("uninstall", "remove"):
        if not shutil.which("uv"):
            print("Error: `uv` is required to uninstall codegen.")
            return
        subprocess.run(["uv", "tool", "uninstall", "codegen"], check=False)
    elif args.command == "run":
        mcp_main()
    else:
        tui_main()
