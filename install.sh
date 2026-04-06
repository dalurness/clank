#!/bin/sh
# Install Clank — https://github.com/dalurness/clank
# Usage: curl -fsSL https://raw.githubusercontent.com/dalurness/clank/main/install.sh | sh
set -e

REPO="dalurness/clank"
INSTALL_DIR="/usr/local/bin"

# Detect OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

case "$OS" in
  linux)  PLATFORM="linux" ;;
  darwin) PLATFORM="darwin" ;;
  *)      echo "Unsupported OS: $OS (use Windows release manually)"; exit 1 ;;
esac

BINARY="clank-${PLATFORM}-${ARCH}"

# Get latest release tag
TAG=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | cut -d'"' -f4)
if [ -z "$TAG" ]; then
  echo "Failed to fetch latest release"
  exit 1
fi

URL="https://github.com/${REPO}/releases/download/${TAG}/${BINARY}"

echo "Installing clank ${TAG} (${PLATFORM}/${ARCH})..."

# Download to temp location
TMP=$(mktemp)
curl -fsSL "$URL" -o "$TMP"
chmod +x "$TMP"

# Install — try /usr/local/bin first, fall back to ~/.local/bin
if [ -w "$INSTALL_DIR" ]; then
  mv "$TMP" "$INSTALL_DIR/clank"
  echo "Installed to $INSTALL_DIR/clank"
elif [ -w /usr/local/bin ] || sudo -n true 2>/dev/null; then
  sudo mv "$TMP" "$INSTALL_DIR/clank"
  echo "Installed to $INSTALL_DIR/clank (sudo)"
else
  INSTALL_DIR="$HOME/.local/bin"
  mkdir -p "$INSTALL_DIR"
  mv "$TMP" "$INSTALL_DIR/clank"
  echo "Installed to $INSTALL_DIR/clank"
  case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;
    *) echo "Add to PATH: export PATH=\"$INSTALL_DIR:\$PATH\"" ;;
  esac
fi

echo "Done! Run 'clank --help' to get started."
