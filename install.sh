#!/bin/sh
# tinfoil-proxy install script
# This script detects your OS/architecture, downloads the latest tinfoil-proxy
# binary, and installs it to /usr/local/bin.
#
# Usage: curl -fsSL https://github.com/tinfoilsh/tinfoil-proxy/raw/main/install.sh | sh
#
# Optional environment variables:
#   VERSION      Install a specific release (e.g. 0.0.8) instead of the latest.
#   INSTALL_DIR  Directory to install into (default: /usr/local/bin).

set -eu

REPO="tinfoilsh/tinfoil-proxy"
BIN_NAME="tinfoil-proxy"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

main() {
  echo "tinfoil-proxy install script"

  # -------------------------------
  # 1. Detect operating system
  # -------------------------------
  OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$OS" in
    linux)
      OS="linux"
      ;;
    darwin)
      OS="darwin"
      ;;
    *)
      echo "Error: Unsupported operating system: $OS"
      echo "Windows users: download tinfoil-proxy-windows-amd64.exe from the releases page."
      exit 1
      ;;
  esac

  # -------------------------------
  # 2. Detect CPU architecture
  # -------------------------------
  ARCH="$(uname -m)"
  case "$ARCH" in
    x86_64|amd64)
      ARCH="amd64"
      ;;
    arm64|aarch64)
      ARCH="arm64"
      ;;
    *)
      echo "Error: Unsupported architecture: $ARCH"
      exit 1
      ;;
  esac

  echo "Detected OS: $OS, Architecture: $ARCH"

  # -------------------------------
  # 3. Construct the download URL. Release assets are raw binaries named
  #    tinfoil-proxy-<os>-<arch>, so the version is not part of the filename.
  # -------------------------------
  ASSET="${BIN_NAME}-${OS}-${ARCH}"
  if [ -n "${VERSION:-}" ]; then
    URL="https://github.com/${REPO}/releases/download/v${VERSION#v}/${ASSET}"
    echo "Requested version: ${VERSION#v}"
  else
    URL="https://github.com/${REPO}/releases/latest/download/${ASSET}"
  fi
  echo "Downloading tinfoil-proxy from: $URL"

  # -------------------------------
  # 4. Download the binary
  # -------------------------------
  TMPDIR="$(mktemp -d)"
  trap 'rm -rf "$TMPDIR"' EXIT

  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$URL" -o "$TMPDIR/$BIN_NAME"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$TMPDIR/$BIN_NAME" "$URL"
  else
    echo "Error: This installer requires curl or wget."
    exit 1
  fi

  chmod +x "$TMPDIR/$BIN_NAME"

  # -------------------------------
  # 5. Install the binary
  # -------------------------------
  echo "Installing tinfoil-proxy to $INSTALL_DIR..."
  if [ -w "$INSTALL_DIR" ]; then
    mv "$TMPDIR/$BIN_NAME" "$INSTALL_DIR/$BIN_NAME"
  elif command -v sudo >/dev/null 2>&1; then
    sudo mv "$TMPDIR/$BIN_NAME" "$INSTALL_DIR/$BIN_NAME"
  else
    echo "Error: $INSTALL_DIR is not writable and sudo is unavailable."
    echo "Re-run as root or set INSTALL_DIR to a writable location."
    exit 1
  fi

  echo "tinfoil-proxy installed successfully!"
  echo "You can now run it with: tinfoil-proxy"
}

main
