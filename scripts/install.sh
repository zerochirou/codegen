#!/bin/sh
set -eu

REPO_URL="https://github.com/zerochirou/codegen"
CHROMA_DIR="${CHROMA_PERSIST_DIR:-$HOME/.codegen/chroma_data}"

echo "==> Checking uv installation..."
if ! command -v uv >/dev/null 2>&1; then
    echo "==> Installing uv..."
    curl -LsSf https://astral.sh/uv/install.sh | sh
    export PATH="$HOME/.local/bin:$PATH"
fi

echo "==> Installing codegen-mcp from $REPO_URL..."
uv tool install --force "git+$REPO_URL"

mkdir -p "$CHROMA_DIR"

echo ""
echo "codegen-mcp installed successfully!"
echo "ChromaDB data directory: $CHROMA_DIR"
echo ""
echo "Add this to Antigravity CLI (~/.gemini/antigravity-cli/mcp_config.json):"
cat <<EOF
{
  "mcpServers": {
    "codegen-memory": {
      "command": "codegen-mcp",
      "env": {
        "CHROMA_PERSIST_DIR": "$CHROMA_DIR"
      }
    }
  }
}
EOF
echo ""
echo "Or add this to VS Code (.vscode/mcp.json):"
cat <<EOF
{
  "servers": {
    "codegen-memory": {
      "type": "stdio",
      "command": "codegen-mcp",
      "env": {
        "CHROMA_PERSIST_DIR": "$CHROMA_DIR"
      }
    }
  }
}
EOF
