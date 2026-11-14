#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

echo "==> Setting up codegen in $ROOT_DIR"

if ! command -v uv >/dev/null 2>&1; then
    echo "==> uv not found. Installing uv..."
    curl -LsSf https://astral.sh/uv/install.sh | sh
    export PATH="$HOME/.local/bin:$PATH"
fi

echo "==> Syncing dependencies (including dev extras)..."
uv sync --extra dev

if [ ! -f ".env" ]; then
    if [ -f ".env.example" ]; then
        echo "==> Creating .env from .env.example..."
        cp .env.example .env
    else
        echo "==> Warning: .env.example not found, skipping .env creation."
    fi
else
    echo "==> .env already exists, keeping existing configuration."
fi

CHROMA_DIR="./chroma_data"
if [ -f ".env" ]; then
    ENV_CHROMA_DIR="$(grep -E '^CHROMA_PERSIST_DIR=' .env | tail -n1 | cut -d'=' -f2- || true)"
    if [ -n "$ENV_CHROMA_DIR" ]; then
        CHROMA_DIR="$ENV_CHROMA_DIR"
    fi
fi

mkdir -p "$CHROMA_DIR"
echo "==> ChromaDB directory ready: $CHROMA_DIR"

if command -v agy >/dev/null 2>&1; then
    echo "==> Registering codegen-memory in Antigravity CLI (agy)..."
    agy mcp add --env "CHROMA_PERSIST_DIR=$ROOT_DIR/$CHROMA_DIR" codegen-memory -- uv --directory "$ROOT_DIR" run codegen-mcp || true
fi

echo ""
echo "Setup complete! Useful commands:"
echo "  uv run codegen-mcp          # Run MCP server"
echo "  uv run codegen-tui          # Run TUI"
echo "  uv run pytest               # Run tests"
echo "  uv run ruff check src tests # Run linter"
