#!/bin/sh
set -eu

REPO_PKG="github.com/zerochirou/codegen/cmd/codegen@latest"
INSTALL_DIR="${CODEGEN_INSTALL_DIR:-$HOME/.local/bin}"
GO_INSTALL_DIR="${CODEGEN_GO_INSTALL_DIR:-$HOME/.local/go}"
GO_MIN_MAJOR=1
GO_MIN_MINOR=24
GO_FALLBACK_VERSION="go1.26.3"

have_cmd() {
    command -v "$1" >/dev/null 2>&1
}

fetch_text() {
    if have_cmd curl; then
        curl -fsSL "$1"
    elif have_cmd wget; then
        wget -qO- "$1"
    else
        echo "Error: curl or wget is required to download dependencies." >&2
        return 1
    fi
}

download_file() {
    if have_cmd curl; then
        curl -fsSL "$1" -o "$2"
    elif have_cmd wget; then
        wget -qO "$2" "$1"
    else
        echo "Error: curl or wget is required to download dependencies." >&2
        return 1
    fi
}

find_go_bin() {
    if have_cmd go; then
        command -v go
        return 0
    fi
    if [ -x "$GO_INSTALL_DIR/bin/go" ]; then
        printf "%s\n" "$GO_INSTALL_DIR/bin/go"
        return 0
    fi
    search_dirs="${CODEGEN_GO_SEARCH_DIRS-/usr/local/go/bin /opt/homebrew/bin /home/linuxbrew/.linuxbrew/bin}"
    for dir in $search_dirs; do
        if [ -x "$dir/go" ]; then
            printf "%s\n" "$dir/go"
            return 0
        fi
    done
    return 1
}

go_version_ok() {
    ver_line=$("$1" version 2>/dev/null || true)
    ver_num=$(printf "%s" "$ver_line" | sed -n 's/.*go\([0-9][0-9]*\.[0-9][0-9]*\).*/\1/p')
    if [ -z "$ver_num" ]; then
        return 1
    fi
    major=$(printf "%s" "$ver_num" | cut -d. -f1)
    minor=$(printf "%s" "$ver_num" | cut -d. -f2)
    if [ "$major" -gt "$GO_MIN_MAJOR" ]; then
        return 0
    fi
    if [ "$major" -eq "$GO_MIN_MAJOR" ] && [ "$minor" -ge "$GO_MIN_MINOR" ]; then
        return 0
    fi
    return 1
}

confirm_install_go() {
    prompt_msg="$1"
    case "${CODEGEN_AUTO_INSTALL_GO:-}" in
        1|y|Y|yes|YES|true|TRUE) return 0 ;;
        0|n|N|no|NO|false|FALSE) return 1 ;;
    esac

    printf "%s Install Go automatically now? [y/N]: " "$prompt_msg" >&2
    ans=""
    if [ -t 0 ]; then
        IFS= read -r ans || ans=""
    elif { exec 3</dev/tty; } 2>/dev/null; then
        IFS= read -r ans <&3 || ans=""
        exec 3<&-
    else
        IFS= read -r ans || ans=""
    fi

    case "$ans" in
        [Yy]|[Yy][Ee][Ss]) return 0 ;;
        *) return 1 ;;
    esac
}

detect_platform() {
    uname_s=$(uname -s 2>/dev/null || printf "unknown")
    uname_m=$(uname -m 2>/dev/null || printf "unknown")

    case "$uname_s" in
        Darwin) GO_OS="darwin" ;;
        Linux) GO_OS="linux" ;;
        FreeBSD) GO_OS="freebsd" ;;
        OpenBSD) GO_OS="openbsd" ;;
        NetBSD) GO_OS="netbsd" ;;
        MINGW*|MSYS*|CYGWIN*) GO_OS="windows" ;;
        *)
            echo "Error: Unsupported OS '$uname_s'. Install Go manually from https://go.dev/dl/" >&2
            return 1
            ;;
    esac

    case "$uname_m" in
        x86_64|amd64) GO_ARCH="amd64" ;;
        arm64|aarch64) GO_ARCH="arm64" ;;
        i386|i686|x86) GO_ARCH="386" ;;
        armv6l|armv7l) GO_ARCH="armv6l" ;;
        ppc64le) GO_ARCH="ppc64le" ;;
        s390x) GO_ARCH="s390x" ;;
        *)
            echo "Error: Unsupported architecture '$uname_m'. Install Go manually from https://go.dev/dl/" >&2
            return 1
            ;;
    esac
}

persist_path_entry() {
    dir_to_add="$1"
    case ":$PATH:" in
        *":$dir_to_add:"*) return 0 ;;
    esac
    export PATH="$dir_to_add:$PATH"

    rc_file=""
    shell_name=$(basename "${SHELL:-sh}")
    case "$shell_name" in
        zsh) rc_file="$HOME/.zshrc" ;;
        bash)
            if [ -f "$HOME/.bash_profile" ] && [ ! -f "$HOME/.bashrc" ]; then
                rc_file="$HOME/.bash_profile"
            else
                rc_file="$HOME/.bashrc"
            fi
            ;;
        *) rc_file="$HOME/.profile" ;;
    esac

    line="export PATH=\"$dir_to_add:\$PATH\""
    if [ -n "$rc_file" ]; then
        if [ ! -f "$rc_file" ] || ! grep -F "$dir_to_add" "$rc_file" >/dev/null 2>&1; then
            mkdir -p "$(dirname "$rc_file")"
            printf "\n%s\n" "$line" >> "$rc_file"
            echo "==> Added $dir_to_add to $rc_file"
        fi
    fi
}

install_go() {
    detect_platform

    go_ver=$(fetch_text "https://go.dev/VERSION?m=text" 2>/dev/null | head -n 1 | tr -d '\r' || true)
    case "$go_ver" in
        go[0-9]*) ;;
        *) go_ver="$GO_FALLBACK_VERSION" ;;
    esac

    tmp_dir=$(mktemp -d)
    trap 'rm -rf "$tmp_dir"' EXIT INT TERM

    rm -rf "$GO_INSTALL_DIR"
    mkdir -p "$(dirname "$GO_INSTALL_DIR")" "$tmp_dir/extract"

    if [ "$GO_OS" = "windows" ]; then
        archive="$tmp_dir/${go_ver}.${GO_OS}-${GO_ARCH}.zip"
        url="https://go.dev/dl/${go_ver}.${GO_OS}-${GO_ARCH}.zip"
        echo "==> Downloading $go_ver ($GO_OS/$GO_ARCH)..."
        download_file "$url" "$archive"
        if have_cmd unzip; then
            unzip -q "$archive" -d "$tmp_dir/extract"
        elif have_cmd powershell.exe; then
            ps_archive="$archive"
            ps_extract="$tmp_dir/extract"
            if have_cmd cygpath; then
                ps_archive=$(cygpath -w "$archive")
                ps_extract=$(cygpath -w "$tmp_dir/extract")
            fi
            powershell.exe -NoProfile -Command "Expand-Archive -Force -Path '$ps_archive' -DestinationPath '$ps_extract'"
        else
            echo "Error: unzip or powershell.exe is required to extract $archive." >&2
            return 1
        fi
    else
        archive="$tmp_dir/${go_ver}.${GO_OS}-${GO_ARCH}.tar.gz"
        url="https://go.dev/dl/${go_ver}.${GO_OS}-${GO_ARCH}.tar.gz"
        echo "==> Downloading $go_ver ($GO_OS/$GO_ARCH)..."
        download_file "$url" "$archive"
        tar -xzf "$archive" -C "$tmp_dir/extract"
    fi

    mv "$tmp_dir/extract/go" "$GO_INSTALL_DIR"
    rm -rf "$tmp_dir"
    trap - EXIT INT TERM

    if ! "$GO_INSTALL_DIR/bin/go" version >/dev/null 2>&1; then
        # Fallback for musl-based Linux distros (e.g. Alpine) without glibc compatibility
        if [ "$GO_OS" = "linux" ] && have_cmd apk; then
            echo "==> Installing Go via apk..."
            apk add --no-cache go
            return 0
        fi
        echo "Error: Installed Go binary failed to execute. Please install Go manually from https://go.dev/dl/" >&2
        return 1
    fi

    persist_path_entry "$GO_INSTALL_DIR/bin"
    echo "==> Installed $($GO_INSTALL_DIR/bin/go version)"
}

echo "==> Checking Go toolchain..."
GO_BIN=$(find_go_bin || true)
need_go_prompt=""
cancel_msg=""

if [ -z "$GO_BIN" ]; then
    need_go_prompt="Go (>= ${GO_MIN_MAJOR}.${GO_MIN_MINOR}) is not installed."
    cancel_msg="==> Cancelled. Install Go (>= ${GO_MIN_MAJOR}.${GO_MIN_MINOR}) from https://go.dev/dl/ and run again."
elif ! go_version_ok "$GO_BIN"; then
    current_ver=$("$GO_BIN" version 2>/dev/null || echo "unknown")
    need_go_prompt="Found outdated Go ($current_ver); >= ${GO_MIN_MAJOR}.${GO_MIN_MINOR} is required."
    cancel_msg="==> Cancelled. Please upgrade Go to >= ${GO_MIN_MAJOR}.${GO_MIN_MINOR} from https://go.dev/dl/."
fi

if [ -n "$need_go_prompt" ]; then
    if confirm_install_go "$need_go_prompt"; then
        install_go
        if [ -x "$GO_INSTALL_DIR/bin/go" ]; then
            GO_BIN="$GO_INSTALL_DIR/bin/go"
        else
            GO_BIN=$(find_go_bin)
        fi
    else
        echo "$cancel_msg"
        exit 0
    fi
else
    persist_path_entry "$(dirname "$GO_BIN")"
fi

echo "==> Installing codegen CLI from $REPO_PKG..."
mkdir -p "$INSTALL_DIR"
GOBIN="$INSTALL_DIR" "$GO_BIN" install "$REPO_PKG"
persist_path_entry "$INSTALL_DIR"

echo ""
echo "codegen installed successfully at: $INSTALL_DIR/codegen"
echo ""
echo "Next step: run 'codegen init' inside your project (or choose Global scope)."
