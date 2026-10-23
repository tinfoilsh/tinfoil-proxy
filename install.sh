#!/bin/sh
# tinfoil-proxy install script
# This script detects your OS/architecture, downloads the latest tinfoil-proxy
# binary, verifies it against the SHA-256 digest published by the GitHub
# release API, and installs it to /usr/local/bin.
#
# The digest comes from api.github.com, an origin independent of the release
# download CDN, so a compromised download path cannot substitute a different
# binary. For provenance verification against the release workflow identity,
# download the binary and verify its Sigstore attestation with
# `gh attestation verify` before installing.
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
  # 3. Resolve the release and the published asset digest
  # -------------------------------
  ASSET="${BIN_NAME}-${OS}-${ARCH}"
  if [ -n "${VERSION:-}" ]; then
    API_URL="https://api.github.com/repos/${REPO}/releases/tags/v${VERSION#v}"
  else
    API_URL="https://api.github.com/repos/${REPO}/releases/latest"
  fi

  TMPDIR="$(mktemp -d)"
  trap 'rm -rf "$TMPDIR"' EXIT

  echo "Fetching release metadata..."
  if ! download "$API_URL" "$TMPDIR/release.json"; then
    echo "Error: Could not fetch release metadata."
    exit 1
  fi
  RELEASE_JSON="$(cat "$TMPDIR/release.json")"

  # The tag_name might be like "v0.2.6". We remove a leading "v".
  VERSION="$(printf '%s\n' "$RELEASE_JSON" | grep '"tag_name":' | sed -E 's/.*"v?([^"]+)".*/\1/')"
  if [ -z "$VERSION" ]; then
    echo "Error: Could not determine release version."
    exit 1
  fi
  echo "Release version: $VERSION"

  ASSETS_JSON="$(printf '%s\n' "$RELEASE_JSON" | sed -n '/"assets": \[/,/^  \],/p')"
  EXPECTED_DIGEST="$(printf '%s\n' "$ASSETS_JSON" | grep -A 80 "\"name\": \"${ASSET}\"" | grep -m1 '"digest": "sha256:' | sed -E 's/.*sha256:([0-9a-f]{64}).*/\1/')"
  if [ -z "$EXPECTED_DIGEST" ]; then
    echo "Error: No SHA-256 digest published for ${ASSET}."
    echo "The download was not installed."
    exit 1
  fi

  # -------------------------------
  # 4. Download the binary from the pinned tag URL
  # -------------------------------
  URL="https://github.com/${REPO}/releases/download/v${VERSION}/${ASSET}"
  echo "Downloading tinfoil-proxy from: $URL"
  if ! download "$URL" "$TMPDIR/$ASSET"; then
    echo "Error: Could not download ${ASSET}."
    exit 1
  fi

  # -------------------------------
  # 5. Verify the digest before installing
  # -------------------------------
  echo "Verifying SHA-256 digest..."
  if command -v sha256sum >/dev/null 2>&1; then
    if ! printf '%s  %s\n' "$EXPECTED_DIGEST" "$ASSET" | (cd "$TMPDIR" && sha256sum -c -); then
      echo "Error: SHA-256 digest mismatch. The download was not installed."
      exit 1
    fi
  elif command -v shasum >/dev/null 2>&1; then
    if ! printf '%s  %s\n' "$EXPECTED_DIGEST" "$ASSET" | (cd "$TMPDIR" && shasum -a 256 -c -); then
      echo "Error: SHA-256 digest mismatch. The download was not installed."
      exit 1
    fi
  else
    echo "Error: sha256sum or shasum is required to verify the download."
    exit 1
  fi
  echo "Digest verified."

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
