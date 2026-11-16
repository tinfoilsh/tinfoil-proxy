#!/bin/sh
# tinfoil-proxy install script
# This script detects your OS/architecture, downloads the latest tinfoil-proxy
# binary, verifies it against the SHA-256 digest published by the GitHub
# release API, and installs it to /usr/local/bin.
#
# The digest comes from GitHub's API via Tinfoil's GitHub proxy, independently
# of the release download CDN. A compromised download path cannot substitute
# a different binary. For provenance verification against the release workflow
# identity, download the binary and verify its Sigstore attestation with
# `gh attestation verify` before installing.
#
# Usage: curl -fsSL https://github.com/tinfoilsh/tinfoil-proxy/raw/main/install.sh | sh
#
# Optional environment variables:
#   VERSION      Install a specific release (e.g. 0.0.8) instead of the latest.
#   INSTALL_DIR  Directory to install into (default: /usr/local/bin).

set -eu

REPO="tinfoilsh/tinfoil-proxy"
GITHUB_PROXY="https://github-proxy.tinfoil.sh"
BIN_NAME="tinfoil-proxy"
RELEASE_VERSION_PATTERN='^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$'
SHA256_PATTERN='^[0-9a-f]{64}$'
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

# Print the string members of the JSON file $1 as name=value lines, in order,
# whether the file is formatted or on a single line.
#
# Every structural character starts a new line, including those inside string
# values. Quotes inside a string value are always escaped, so a piece of one
# never has the shape of a member and is dropped. The C locale keeps the tools
# working on bytes, whatever the user's locale.
json_string_members() {
  LC_ALL=C tr '{}[],' '\n' < "$1" |
    LC_ALL=C sed -n 's/^[[:space:]]*"\([^"\\]*\)":[[:space:]]*"\([^"\\]*\)"[[:space:]]*$/\1=\2/p'
}

# Print the version of the release described by the metadata file $1, without
# a leading "v".
release_version() {
  json_string_members "$1" |
    LC_ALL=C awk '!found && sub(/^tag_name=v?/, "") { print; found = 1 }'
}

# Print the SHA-256 digest that the metadata file $1 publishes for the asset
# named $2. An asset lists its name before its digest.
asset_sha256() {
  json_string_members "$1" |
    LC_ALL=C awk -v asset="$2" '
      /^name=/ { selected = ($0 == "name=" asset) }
      selected && !found && sub(/^digest=sha256:/, "") { print; found = 1 }
    '
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
    API_URL="${GITHUB_PROXY}/repos/${REPO}/releases/tags/v${VERSION#v}"
  else
    API_URL="${GITHUB_PROXY}/repos/${REPO}/releases/latest"
  fi

  TMPDIR="$(mktemp -d)"
  trap 'rm -rf "$TMPDIR"' EXIT

  echo "Fetching release metadata..."
  if ! download "$API_URL" "$TMPDIR/release.json"; then
    echo "Error: Could not fetch release metadata."
    exit 1
  fi

  # The tag_name might be like "v0.2.6". We remove a leading "v". The version
  # becomes part of the download URL, so only a plain version is accepted to
  # keep the download inside this repository's releases.
  VERSION="$(release_version "$TMPDIR/release.json")"
  if ! printf '%s\n' "$VERSION" | LC_ALL=C grep -Eq "$RELEASE_VERSION_PATTERN"; then
    echo "Error: Could not determine release version."
    exit 1
  fi
  echo "Release version: $VERSION"

  EXPECTED_DIGEST="$(asset_sha256 "$TMPDIR/release.json" "$ASSET")"
  if ! printf '%s\n' "$EXPECTED_DIGEST" | LC_ALL=C grep -Eq "$SHA256_PATTERN"; then
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
