"""Interactive Clack-style setup wizard for `codegen init`."""

from __future__ import annotations

import json
import shutil
import subprocess
import sys
import termios
import tty
from pathlib import Path

# ANSI colors matching Clack-style prompts
CYAN = "\033[36m"
GREEN = "\033[32m"
DIM = "\033[2m"
BOLD = "\033[1m"
RESET = "\033[0m"


def _read_key() -> str:
    """Read a single keypress (supports arrow keys, Enter, Space)."""
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
        if ch == " ":
            return "SPACE"
        if ch == "\x03":
            raise KeyboardInterrupt
        return ch
    finally:
        termios.tcsetattr(fd, termios.TCSADRAIN, old)


def _prompt_text(label: str, default: str = "") -> str:
    """Prompt for a text value in Clack style."""
    print(f"{CYAN}◇{RESET}  {BOLD}{label}{RESET}")
    hint = f" {DIM}({default}){RESET}" if default else ""
    val = input(f"{DIM}│{RESET}  {hint}").strip()
    return val or default


def _prompt_select(label: str, options: list[str]) -> str:
    """Interactive arrow-key selector in Clack style."""
    if not sys.stdin.isatty():
        return options[0]

    idx = 0
    print(f"{CYAN}◇{RESET}  {BOLD}{label}{RESET} {DIM}(↑/↓, Enter){RESET}")
    for i, opt in enumerate(options):
        marker = f"{GREEN}●{RESET}" if i == idx else f"{DIM}○{RESET}"
        text = f"{BOLD}{opt}{RESET}" if i == idx else f"{DIM}{opt}{RESET}"
        print(f"{DIM}│{RESET}  {marker} {text}")

    while True:
        key = _read_key()
        if key == "UP":
            idx = (idx - 1) % len(options)
        elif key == "DOWN":
            idx = (idx + 1) % len(options)
        elif key in ("ENTER", "SPACE"):
            break
        else:
            continue

        # Move cursor up to redraw options
        sys.stdout.write(f"\033[{len(options)}A")
        for i, opt in enumerate(options):
            sys.stdout.write("\033[2K")
            marker = f"{GREEN}●{RESET}" if i == idx else f"{DIM}○{RESET}"
            text = f"{BOLD}{opt}{RESET}" if i == idx else f"{DIM}{opt}{RESET}"
            sys.stdout.write(f"{DIM}│{RESET}  {marker} {text}\n")
        sys.stdout.flush()

    return options[idx]


def _find_bundled_skills() -> Path | None:
    """Locate .agents/skills relative to the package or repo."""
    for parent in Path(__file__).resolve().parents:
        candidate = parent / ".agents" / "skills"
        if candidate.is_dir():
            return candidate
    return None


CODEGEN_SKILLS = {
    "architect",
    "build",
    "codegen",
    "codegen-auto-pilot",
    "codegen-help",
    "codegen-memory",
    "codegen-setup",
    "forget",
    "handoff",
    "impact",
    "recall",
    "remember",
    "review",
    "ship",
    "the-opposite",
    "update-memory",
}


def _install_skills(dest_dir: Path, is_project: bool) -> int:
    """Install codegen skills via skills.sh (`npx skills add`), with local fallback."""
    if shutil.which("npx"):
        cmd = ["npx", "--yes", "skills", "add", "zerochirou/codegen", "--yes"]
        if not is_project:
            cmd.append("--global")
        result = subprocess.run(cmd, check=False)
        if result.returncode == 0:
            return len(CODEGEN_SKILLS)

    src = _find_bundled_skills()
    if src is None:
        return 0

    dest_dir.mkdir(parents=True, exist_ok=True)
    count = 0
    for item in src.iterdir():
        if item.is_dir() and item.name in CODEGEN_SKILLS and (item / "SKILL.md").exists():
            target = dest_dir / item.name
            if target.resolve() != item.resolve():
                shutil.copytree(item, target, dirs_exist_ok=True)
            count += 1
    return count


def _write_mcp_config(config_path: Path, chroma_dir: str) -> None:
    """Write or update codegen-memory entry in mcp_config.json."""
    config_path.parent.mkdir(parents=True, exist_ok=True)
    config: dict = {}
    if config_path.exists():
        try:
            config = json.loads(config_path.read_text(encoding="utf-8"))
        except json.JSONDecodeError:
            config = {}

    servers = config.setdefault("mcpServers", {})
    servers["codegen-memory"] = {
        "command": "codegen",
        "args": ["run"],
        "env": {"CHROMA_PERSIST_DIR": chroma_dir},
    }
    config_path.write_text(json.dumps(config, indent=2) + "\n", encoding="utf-8")


def run_init_wizard() -> None:
    """Run the interactive codegen setup wizard."""
    print(f"\n{CYAN}┌{RESET}  {BOLD}Let's configure codegen{RESET}")
    print(f"{DIM}│{RESET}")

    scope = _prompt_select(
        "Install scope:",
        ["Per-project (.agents/skills + .gemini/)", "Global (~/.gemini/antigravity-cli/)"],
    )
    is_project = scope.startswith("Per-project")

    project_dir = Path.cwd()
    if is_project:
        name = _prompt_text("Project name:", default=project_dir.name)
        default_chroma = str(project_dir)
        mcp_path = project_dir / ".gemini" / "mcp_config.json"
        skills_path = project_dir / ".agents" / "skills"
    else:
        name = "global"
        default_chroma = str(Path.home() / ".codegen")
        mcp_path = Path.home() / ".gemini" / "antigravity-cli" / "mcp_config.json"
        skills_path = Path.home() / ".gemini" / "antigravity-cli" / "skills"

    chroma_input = Path(_prompt_text("ChromaDB storage path:", default=default_chroma)).expanduser()
    chroma_dir = str(chroma_input if chroma_input.name == "chroma_data" else chroma_input / "chroma_data")
    install_skills = _prompt_select("Install agent skills?", ["Yes", "No"]) == "Yes"

    print(f"{DIM}│{RESET}")
    print(f"{GREEN}●{RESET}  {BOLD}About to configure:{RESET}")
    print(f"{DIM}│{RESET}    Scope:        {'Per-project' if is_project else 'Global'}")
    print(f"{DIM}│{RESET}    Project:      {name}")
    print(f"{DIM}│{RESET}    MCP config:   {mcp_path}")
    print(f"{DIM}│{RESET}    ChromaDB:     {chroma_dir}")
    print(f"{DIM}│{RESET}    Agent skills: {'yes (' + str(skills_path) + ')' if install_skills else 'no'}")
    print(f"{DIM}│{RESET}")

    if _prompt_select("Continue with these settings?", ["Yes", "No"]) != "Yes":
        print(f"{DIM}└{RESET}  Cancelled.\n")
        return

    Path(chroma_dir).expanduser().mkdir(parents=True, exist_ok=True)
    _write_mcp_config(mcp_path, chroma_dir)
    installed = _install_skills(skills_path, is_project) if install_skills else 0
    print(f"{GREEN}└{RESET}  Done! MCP configured at {mcp_path} ({installed} skills ready).\n")
