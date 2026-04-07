#!/usr/bin/env bash
set -euo pipefail

INSTALL_DIR="$HOME/.smoko/bin"
REPO_DIR="$(cd "$(dirname "$0")" && pwd)"
VERSION=$(cat "$REPO_DIR/VERSION")
COMMIT=$(git -C "$REPO_DIR" rev-parse --short HEAD 2>/dev/null || echo "unknown")

echo "Building smoko v${VERSION}+${COMMIT}..."
cd "$REPO_DIR/src"
go build -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" -o "$REPO_DIR/smoko" ./cmd/smoko

echo "Installing to $INSTALL_DIR..."
mkdir -p "$INSTALL_DIR"
mv "$REPO_DIR/smoko" "$INSTALL_DIR/smoko"
chmod +x "$INSTALL_DIR/smoko"

# Add to PATH if not already present
add_to_path() {
    local profile="$1"
    if [ -f "$profile" ] && grep -q 'smoko/bin' "$profile"; then
        return
    fi
    echo '' >> "$profile"
    echo '# smoko CLI' >> "$profile"
    echo 'export PATH="$HOME/.smoko/bin:$PATH"' >> "$profile"
    echo "Added to $profile"
}

if echo "$PATH" | tr ':' '\n' | grep -q "$INSTALL_DIR"; then
    echo "PATH already contains $INSTALL_DIR"
else
    shell_name="$(basename "${SHELL:-/bin/bash}")"
    case "$shell_name" in
        zsh)  add_to_path "$HOME/.zshrc" ;;
        bash)
            if [ -f "$HOME/.bash_profile" ]; then
                add_to_path "$HOME/.bash_profile"
            else
                add_to_path "$HOME/.bashrc"
            fi
            ;;
        fish)
            fish_conf="$HOME/.config/fish/conf.d/smoko.fish"
            if [ ! -f "$fish_conf" ]; then
                mkdir -p "$(dirname "$fish_conf")"
                echo 'fish_add_path $HOME/.smoko/bin' > "$fish_conf"
                echo "Added to $fish_conf"
            fi
            ;;
        *)
            echo "Unknown shell '$shell_name'. Add $INSTALL_DIR to your PATH manually."
            ;;
    esac
    echo "Restart your shell or run: export PATH=\"$INSTALL_DIR:\$PATH\""
fi

echo "Done. Run 'smoko --help' to get started."
