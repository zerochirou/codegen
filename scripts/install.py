#!/usr/bin/env python3
"""Interactive installer & setup script for codegen-mcp with agent detection."""

from __future__ import annotations

import argparse
import itertools
import json
import os
import shutil
import subprocess
import sys
import termios
import threading
import time
import tty
from dataclasses import dataclass
from pathlib import Path

REPO_URL = "https://github.com/zerochirou/codegen"
DEFAULT_CHROMA_DIR = Path.home() / ".codegen" / "chroma_data"

# ANSI colors
RESET = "\033[0m"
BOLD = "\033[1m"
DIM = "\033[2m"
GREEN = "\033[32m"
YELLOW = "\033[33m"
CYAN = "\033[36m"
RED = "\033[31m"


def supports_color() -> bool:
    return sys.stdout.isatty() and os.environ.get("NO_COLOR") is None


def c(text: str, color: str) -> str:
    return f"{color}{text}{RESET}" if supports_color() else text


class Spinner:
    """Simple terminal spinner for long-running steps."""

    FRAMES = ("⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏")

    def __init__(self, message: str) -> None:
        self.message = message
        self._stop = threading.Event()
        self._thread: threading.Thread | None = None

    def __enter__(self) -> "Spinner":
        if sys.stdout.isatty():
            self._thread = threading.Thread(target=self._spin, daemon=True)
            self._thread.start()
        else:
            print(f"... {self.message}")
        return self

    def _spin(self) -> None:
        for frame in itertools.cycle(self.FRAMES):
            if self._stop.is_set():
                break
            sys.stdout.write(f"\r{c(frame, CYAN)} {self.message}")
            sys.stdout.flush()
            time.sleep(0.08)

    def __exit__(self, exc_type, exc_val, exc_tb) -> None:
        self._stop.set()
        if self._thread is not None:
            self._thread.join()
            sys.stdout.write("\r\033[K")
            sys.stdout.flush()
        if exc_type is None:
            print(f"{c('✔', GREEN)} {self.message}")
        else:
            print(f"{c('✘', RED)} {self.message}")


@dataclass
class AgentInfo:
    key: str
    name: str
    binary: str
    project_config: Path
    global_config: Path
    detected: bool = False
    via: str = ""

    def config_for(self, scope: str) -> Path:
        return self.project_config if scope == "project" else self.global_config


def detect_agents(project_dir: Path) -> list[AgentInfo]:
    home = Path.home()
    vscode_global = (
        home / "Library" / "Application Support" / "Code" / "User" / "mcp.json"
        if sys.platform == "darwin"
        else home / ".config" / "Code" / "User" / "mcp.json"
    )
    candidates = [
        AgentInfo(
            key="agy",
            name="Antigravity CLI (agy)",
            binary="agy",
            project_config=project_dir / ".gemini" / "mcp_config.json",
            global_config=home / ".gemini" / "antigravity-cli" / "mcp_config.json",
        ),
        AgentInfo(
            key="claude",
            name="Claude Code",
            binary="claude",
            project_config=project_dir / ".mcp.json",
            global_config=home / ".claude.json",
        ),
        AgentInfo(
            key="codex",
            name="OpenAI Codex CLI",
            binary="codex",
            project_config=project_dir / ".codex" / "config.toml",
            global_config=home / ".codex" / "config.toml",
        ),
        AgentInfo(
            key="gemini",
            name="Gemini CLI",
            binary="gemini",
            project_config=project_dir / ".gemini" / "settings.json",
            global_config=home / ".gemini" / "settings.json",
        ),
        AgentInfo(
            key="cursor",
            name="Cursor",
            binary="cursor",
            project_config=project_dir / ".cursor" / "mcp.json",
            global_config=home / ".cursor" / "mcp.json",
        ),
        AgentInfo(
            key="vscode",
            name="VS Code",
            binary="code",
            project_config=project_dir / ".vscode" / "mcp.json",
            global_config=vscode_global,
        ),
        AgentInfo(
            key="opencode",
            name="OpenCode",
            binary="opencode",
            project_config=project_dir / "opencode.json",
            global_config=home / ".config" / "opencode" / "opencode.json",
        ),
    ]

    for agent in candidates:
        bin_path = shutil.which(agent.binary)
        if bin_path:
            agent.detected = True
            agent.via = bin_path
        elif agent.global_config.parent.exists() and agent.key != "vscode":
            agent.detected = True
            agent.via = str(agent.global_config.parent)

    return candidates


def run_cmd(cmd: list[str], cwd: Path | None = None) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        cmd,
        cwd=str(cwd) if cwd else None,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=True,
    )


def ensure_uv() -> str:
    uv_bin = shutil.which("uv")
    if uv_bin:
        return uv_bin

    with Spinner("Installing uv package manager..."):
        subprocess.run(
            ["sh", "-c", "curl -LsSf https://astral.sh/uv/install.sh | sh"],
            check=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )
    local_uv = Path.home() / ".local" / "bin" / "uv"
    if local_uv.exists():
        os.environ["PATH"] = f"{local_uv.parent}:{os.environ.get('PATH', '')}"
        return str(local_uv)
    return "uv"


def find_repo_root() -> Path | None:
    cwd = Path.cwd()
    if (cwd / "pyproject.toml").exists() and (cwd / "src" / "codegen").exists():
        return cwd
    script_root = Path(__file__).resolve().parent.parent
    if (script_root / "pyproject.toml").exists() and (script_root / "src" / "codegen").exists():
        return script_root
    return None


def install_codegen(uv_bin: str, repo_root: Path | None) -> Path:
    if repo_root is not None:
        with Spinner("Syncing local project dependencies (uv sync --extra dev)..."):
            run_cmd([uv_bin, "sync", "--extra", "dev"], cwd=repo_root)

        env_file = repo_root / ".env"
        env_example = repo_root / ".env.example"
        if not env_file.exists() and env_example.exists():
            with Spinner("Creating .env from .env.example..."):
                shutil.copy2(env_example, env_file)

        chroma_dir = repo_root / "chroma_data"
        if env_file.exists():
            for line in env_file.read_text().splitlines():
                if line.startswith("CHROMA_PERSIST_DIR="):
                    val = line.split("=", 1)[1].strip()
                    if val:
                        chroma_dir = (repo_root / val).resolve() if not Path(val).is_absolute() else Path(val)
        chroma_dir.mkdir(parents=True, exist_ok=True)
        return chroma_dir

    with Spinner(f"Installing codegen-mcp from {REPO_URL}..."):
        run_cmd([uv_bin, "tool", "install", "--force", f"git+{REPO_URL}"])

    chroma_dir = Path(os.environ.get("CHROMA_PERSIST_DIR", str(DEFAULT_CHROMA_DIR)))
    chroma_dir.mkdir(parents=True, exist_ok=True)
    return chroma_dir


def build_mcp_entry(repo_root: Path | None, chroma_dir: Path) -> dict:
    if repo_root is not None:
        return {
            "command": "uv",
            "args": ["--directory", str(repo_root), "run", "codegen-mcp"],
            "env": {"CHROMA_PERSIST_DIR": str(chroma_dir)},
        }
    return {
        "command": "codegen-mcp",
        "env": {"CHROMA_PERSIST_DIR": str(chroma_dir)},
    }


def configure_json_mcp(config_path: Path, root_key: str, entry: dict) -> bool:
    config_path.parent.mkdir(parents=True, exist_ok=True)
    data: dict = {}
    if config_path.exists():
        try:
            data = json.loads(config_path.read_text(encoding="utf-8"))
        except json.JSONDecodeError:
            return False

    servers = data.setdefault(root_key, {})
    servers["codegen-memory"] = entry
    config_path.write_text(json.dumps(data, indent=2) + "\n", encoding="utf-8")
    return True


def read_key() -> str:
    fd = sys.stdin.fileno()
    old = termios.tcgetattr(fd)
    try:
        tty.setraw(fd)
        ch = sys.stdin.read(1)
        if ch == "\x1b":
            seq = sys.stdin.read(2)
            if seq == "[A":
                return "UP"
            if seq == "[B":
                return "DOWN"
            return "ESC"
        if ch in ("\r", "\n"):
            return "ENTER"
        if ch == "\x03":
            raise KeyboardInterrupt
        if ch in ("k", "K"):
            return "UP"
        if ch in ("j", "J"):
            return "DOWN"
        return ch
    finally:
        termios.tcsetattr(fd, termios.TCSADRAIN, old)


def select_menu(title: str, options: list[tuple[str, str]], default_index: int = 0) -> int:
    """Interactive arrow-key selector styled like skills.sh."""
    if not (sys.stdin.isatty() and sys.stdout.isatty()):
        return default_index

    idx = default_index
    print(f"{c('◆', CYAN)} {c(title, BOLD)}")

    def render(first: bool = False) -> None:
        if not first:
            sys.stdout.write(f"\033[{len(options)}A")
        for i, (label, hint) in enumerate(options):
            sys.stdout.write("\r\033[K")
            if i == idx:
                bullet = c("●", GREEN)
                text = c(label, BOLD + GREEN)
            else:
                bullet = c("○", DIM)
                text = label
            hint_str = f" {c(hint, DIM)}" if hint else ""
            sys.stdout.write(f"  {bullet} {text}{hint_str}\n")
        sys.stdout.flush()

    render(first=True)
    while True:
        key = read_key()
        if key == "UP":
            idx = (idx - 1) % len(options)
            render()
        elif key == "DOWN":
            idx = (idx + 1) % len(options)
            render()
        elif key == "ENTER":
            return idx


def choose_scope(arg_scope: str | None, repo_root: Path | None) -> str:
    if arg_scope in ("project", "global"):
        return arg_scope
    default_scope = "project" if repo_root is not None else "global"
    if not (sys.stdin.isatty() and sys.stdout.isatty()):
        return default_scope

    default_idx = 0 if default_scope == "project" else 1
    chosen = select_menu(
        "Select MCP installation scope",
        [
            ("Per-project", f"({Path.cwd()})"),
            ("Global", "(all projects)"),
        ],
        default_index=default_idx,
    )
    return "project" if chosen == 0 else "global"


def register_agy_mcp(entry: dict) -> bool:
    agy_bin = shutil.which("agy")
    if not agy_bin:
        return False
    cmd = [agy_bin, "mcp", "add"]
    for k, v in entry.get("env", {}).items():
        cmd.extend(["--env", f"{k}={v}"])
    cmd.extend(["codegen-memory", "--", entry["command"], *entry.get("args", [])])
    try:
        run_cmd(cmd)
        return True
    except subprocess.CalledProcessError:
        return False


def main() -> int:
    parser = argparse.ArgumentParser(description="Install & setup codegen-mcp")
    parser.add_argument(
        "--scope",
        choices=("project", "global"),
        help="Install MCP config per-project or global",
    )
    parser.add_argument(
        "--auto-config",
        action="store_true",
        help="Automatically write MCP config for detected JSON-based agents",
    )
    args = parser.parse_args()

    print(c("\n=== codegen-mcp Installer & Setup ===\n", BOLD + CYAN))

    repo_root = find_repo_root()
    scope = choose_scope(args.scope, repo_root)
    mode = f"Local repo ({repo_root})" if repo_root else "Global tool install"
    print(f"\n{c('Mode:', BOLD)} {mode}")
    print(f"{c('Scope:', BOLD)} {scope}\n")

    try:
        uv_bin = ensure_uv()
        print(f"{c('✔', GREEN)} uv ready ({c(uv_bin, DIM)})")
        chroma_dir = install_codegen(uv_bin, repo_root)
        print(f"{c('✔', GREEN)} ChromaDB directory ready ({c(str(chroma_dir), DIM)})")
    except subprocess.CalledProcessError as exc:
        print(f"\n{c('Error:', RED)} Command failed: {' '.join(exc.cmd)}")
        if exc.stderr:
            print(exc.stderr.strip())
        return 1

    print()
    with Spinner("Detecting installed AI agents..."):
        time.sleep(0.3)
        agents = detect_agents(repo_root or Path.cwd())

    detected = [a for a in agents if a.detected]
    print(f"\n{c('Detected AI Agents:', BOLD)}")
    if not detected:
        print(f"  {c('No supported AI agents detected.', YELLOW)}")
    else:
        for agent in detected:
            cfg = agent.config_for(scope)
            print(f"  {c('●', GREEN)} {c(agent.name, BOLD)} {c(f'-> {cfg}', DIM)}")

    entry = build_mcp_entry(repo_root, chroma_dir)

    should_configure = args.auto_config
    if not should_configure and detected and sys.stdin.isatty() and sys.stdout.isatty():
        print()
        choice = select_menu(
            f"Configure MCP for detected agents ({scope})?",
            [
                ("Yes", "(write config automatically)"),
                ("No", "(show config snippet only)"),
            ],
            default_index=0,
        )
        should_configure = choice == 0

    if should_configure and detected:
        print(f"\n{c(f'Configuring detected agents ({scope})...', BOLD)}")
        json_targets = {
            "agy": "mcpServers",
            "claude": "mcpServers",
            "gemini": "mcpServers",
            "cursor": "mcpServers",
            "vscode": "servers",
        }
        for agent in detected:
            if agent.key == "agy" and scope == "global" and register_agy_mcp(entry):
                print(f"  {c('✔', GREEN)} Registered via `agy mcp add`")
                continue
            cfg_path = agent.config_for(scope)
            root_key = json_targets.get(agent.key)
            if not root_key:
                print(f"  {c('•', YELLOW)} {agent.name}: manual setup required ({cfg_path})")
                continue
            target_entry = dict(entry)
            if agent.key == "vscode":
                target_entry = {"type": "stdio", **target_entry}
            ok = configure_json_mcp(cfg_path, root_key, target_entry)
            if ok:
                print(f"  {c('✔', GREEN)} Updated {cfg_path}")
            else:
                print(f"  {c('✘', RED)} Skipped {cfg_path} (invalid JSON)")

    print(f"\n{c('MCP Config Snippet:', BOLD)}")
    print(json.dumps({"mcpServers": {"codegen-memory": entry}}, indent=2))

    if not args.auto_config and detected:
        print(
            f"\n{c('Tip:', CYAN)} Run with {c(f'--scope {scope} --auto-config', BOLD)} "
            "to write this config to detected agents automatically."
        )

    print(f"\n{c('Done!', GREEN + BOLD)}\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
