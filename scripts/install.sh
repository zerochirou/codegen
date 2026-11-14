#!/bin/sh
set -eu

REPO_PKG="github.com/zerochirou/codegen/cmd/codegen@latest"
INSTALL_DIR="${CODEGEN_INSTALL_DIR:-$HOME/.local/bin}"

echo "==> Checking Go toolchain..."
if ! command -v go >/dev/null 2>&1; then
    echo "Error: Go (>= 1.24) is required. Install from https://go.dev/dl/" >&2
    exit 1
fi

echo "==> Installing codegen CLI from $REPO_PKG..."
GOBIN="$INSTALL_DIR" go install "$REPO_PKG"

echo ""
echo "codegen installed successfully at: $INSTALL_DIR/codegen"
echo "Make sure $INSTALL_DIR is in your PATH."
echo ""
echo "Next step: run 'codegen init' inside your project (or choose Global scope)."
