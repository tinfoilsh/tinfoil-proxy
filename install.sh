#!/bin/sh
# tinfoil-proxy install script
# This script detects your OS/architecture, downloads the latest tinfoil-proxy
# binary, verifies its SHA-256 checksum, and installs it to /usr/local/bin.
#
# Usage: curl -fsSL https://github.com/tinfoilsh/tinfoil-proxy/raw/main/install.sh | sh
#
# Optional environment variables:
#   VERSION      Install a specific release (e.g. 0.0.8) instead of the latest.
#   INSTALL_DIR  Directory to install into (default: /usr/local/bin).

set -eu

REPO="tinfoilsh/tinfoil-proxy"
BIN_NAME="tinfoil-proxy"
CHECKSUMS_FILE="SHA256SUMS"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

# Download $1 to $2 using curl or wget.
download() {
  url="$1"
  out="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$out"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$out" "$url"
  else
    echo "Error: This installer requires curl or wget."
    exit 1
  fi
}

# Verify that file $1 matches its entry for name $2 in the checksums file $3.
verify_checksum() {
  file="$1"
  name="$2"
  sums="$3"

  expected="$(awk -v n="$name" '$2 == n {print $1}' "$sums" | head -n 1)"
  if [ -z "$expected" ]; then
    echo "Error: no checksum found for $name in $CHECKSUMS_FILE."
    exit 1
  fi

  if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "$file" | awk '{print $1}')"
  elif command -v shasum >/dev/null 2>&1; then
    actual="$(shasum -a 256 "$file" | awk '{print $1}')"
  else
    echo "Error: need sha256sum or shasum to verify the download."
    exit 1
  fi

  if [ "$expected" != "$actual" ]; then
    echo "Error: checksum mismatch for $name."
    echo "  expected: $expected"
    echo "  actual:   $actual"
    exit 1
  fi
  echo "Checksum verified."
}

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
  # 3. Construct the download URLs. Release assets are raw binaries named
  #    tinfoil-proxy-<os>-<arch>, so the version is not part of the filename.
  # -------------------------------
  ASSET="${BIN_NAME}-${OS}-${ARCH}"
  if [ -n "${VERSION:-}" ]; then
    BASE_URL="https://github.com/${REPO}/releases/download/v${VERSION#v}"
    echo "Requested version: ${VERSION#v}"
  else
    BASE_URL="https://github.com/${REPO}/releases/latest/download"
  fi

  # -------------------------------
  # 4. Download the binary and its checksums file
  # -------------------------------
  TMPDIR="$(mktemp -d)"
  trap 'rm -rf "$TMPDIR"' EXIT

  echo "Downloading tinfoil-proxy from: ${BASE_URL}/${ASSET}"
  download "${BASE_URL}/${ASSET}" "$TMPDIR/$ASSET"
  echo "Downloading checksums from: ${BASE_URL}/${CHECKSUMS_FILE}"
  download "${BASE_URL}/${CHECKSUMS_FILE}" "$TMPDIR/$CHECKSUMS_FILE"

  # -------------------------------
  # 5. Verify integrity before installing
  # -------------------------------
  verify_checksum "$TMPDIR/$ASSET" "$ASSET" "$TMPDIR/$CHECKSUMS_FILE"

  chmod +x "$TMPDIR/$ASSET"

  # -------------------------------
  # 6. Install the binary
  # -------------------------------
  echo "Installing tinfoil-proxy to $INSTALL_DIR..."
  if [ -w "$INSTALL_DIR" ]; then
    mv "$TMPDIR/$ASSET" "$INSTALL_DIR/$BIN_NAME"
  elif command -v sudo >/dev/null 2>&1; then
    sudo mv "$TMPDIR/$ASSET" "$INSTALL_DIR/$BIN_NAME"
  else
    echo "Error: $INSTALL_DIR is not writable and sudo is unavailable."
    echo "Re-run as root or set INSTALL_DIR to a writable location."
    exit 1
  fi

  echo "tinfoil-proxy installed successfully!"
  echo "You can now run it with: tinfoil-proxy"
}

main
