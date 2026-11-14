#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

echo "==> Building codegen (Go CLI) in $ROOT_DIR"

if ! command -v go >/dev/null 2>&1; then
    echo "Error: Go (>= 1.24) is required." >&2
    exit 1
fi

INSTALL_DIR="${CODEGEN_INSTALL_DIR:-$HOME/.local/bin}"
mkdir -p "$INSTALL_DIR"
go build -o "$INSTALL_DIR/codegen" ./cmd/codegen

echo "==> Installed binary to $INSTALL_DIR/codegen"

if [ ! -f ".env" ] && [ -f ".env.example" ]; then
    cp .env.example .env
fi

echo "==> Running tests..."
go test ./...

echo ""
echo "Setup complete! Useful commands:"
echo "  codegen --help        # Show CLI help"
echo "  codegen init          # Configure per-project or global ChromaDB + MCP + skills"
echo "  codegen tui           # Inspect per-project ChromaDB memory"
echo "  codegen tui --global  # Inspect global ChromaDB memory"
echo "  codegen run           # Run MCP stdio server"
