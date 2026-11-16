#!/bin/sh

set -eu

REPO_DIR="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)"
TEST_DIR="$(mktemp -d)"
trap 'rm -rf "$TEST_DIR"' EXIT
export TEST_DIR

case "$(uname -s)-$(uname -m)" in
  Linux-x86_64|Linux-amd64) EXPECTED_ASSET="tinfoil-proxy-linux-amd64" ;;
  Linux-aarch64|Linux-arm64) EXPECTED_ASSET="tinfoil-proxy-linux-arm64" ;;
  Darwin-x86_64) EXPECTED_ASSET="tinfoil-proxy-darwin-amd64" ;;
  Darwin-arm64) EXPECTED_ASSET="tinfoil-proxy-darwin-arm64" ;;
  *) echo "Unsupported test platform" >&2; exit 1 ;;
esac
RELEASE_DOWNLOAD_URL="https://github.com/tinfoilsh/tinfoil-proxy/releases/download"
EXPECTED_BINARY_URL="$RELEASE_DOWNLOAD_URL/v0.2.9/$EXPECTED_ASSET"
export EXPECTED_BINARY_URL

mkdir "$TEST_DIR/bin" "$TEST_DIR/install"
cat > "$TEST_DIR/bin/curl" <<'EOF'
#!/bin/sh
set -eu
test "$1" = "-fsSL"
test "$3" = "-o"
case "$2" in
  "$EXPECTED_METADATA_URL")
    cp "$TEST_DIR/release.json" "$4"
    ;;
  "$EXPECTED_BINARY_URL")
    cp "$TEST_DIR/download" "$4"
    ;;
  *)
    echo "Unexpected download URL: $2" >&2
    exit 1
    ;;
esac
EOF
cat > "$TEST_DIR/bin/jq" <<'EOF'
#!/bin/sh
echo "The installer must not depend on jq." >&2
exit 1
EOF
chmod +x "$TEST_DIR/bin/curl" "$TEST_DIR/bin/jq"

printf '#!/bin/sh\nexit 0\n' > "$TEST_DIR/binary"
if command -v sha256sum >/dev/null 2>&1; then
  DIGEST="sha256:$(sha256sum "$TEST_DIR/binary" | cut -d ' ' -f 1)"
else
  DIGEST="sha256:$(shasum -a 256 "$TEST_DIR/binary" | cut -d ' ' -f 1)"
fi
WRONG_DIGEST="sha256:$(printf '%064d' 0)"

# Write release metadata shaped like GitHub's, in the layout $1 (formatted or
# compact), for the tag $2, with the installer's asset named $3 and carrying
# the JSON digest value $4.
#
# The release notes and the release title only look like the members the
# installer reads, and the asset after the installer's carries the binary's
# real digest, so reading the wrong member changes the outcome.
write_release() {
  cat > "$TEST_DIR/formatted.json" <<EOF
{
  "body": "Notes, \"tag_name\": \"v9.9.9\", [{\"name\": \"$EXPECTED_ASSET\", \"digest\": \"$WRONG_DIGEST\"}],",
  "upload_url": "https://uploads.github.com/repos/tinfoilsh/tinfoil-proxy/releases/1/assets{?name,label}",
  "author": {
    "login": "github-actions[bot]",
    "id": 41898282
  },
  "name": "$EXPECTED_ASSET",
  "assets": [
    {
      "name": "another-asset",
      "label": "",
      "uploader": {
        "login": "github-actions[bot]",
        "id": 41898282
      },
      "size": 1,
      "digest": "$WRONG_DIGEST",
      "download_count": 1
    },
    {
      "name": "$3",
      "label": "",
      "uploader": {
        "login": "github-actions[bot]",
        "id": 41898282
      },
      "size": 2,
      "digest": $4,
      "download_count": 2
    },
    {
      "name": "$EXPECTED_ASSET.sig",
      "label": "",
      "uploader": {
        "login": "github-actions[bot]",
        "id": 41898282
      },
      "size": 3,
      "digest": "$DIGEST",
      "download_count": 3
    }
  ],
  "tag_name": "$2"
}
EOF
  if [ "$1" = compact ]; then
    sed -e 's/^ *//' -e 's/": /":/g' "$TEST_DIR/formatted.json" | tr -d '\n' > "$TEST_DIR/release.json"
  else
    cp "$TEST_DIR/formatted.json" "$TEST_DIR/release.json"
  fi
}

run_installer() {
  PATH="$TEST_DIR/bin:$PATH" INSTALL_DIR="$TEST_DIR/install" sh "$REPO_DIR/install.sh" > "$TEST_DIR/output" 2>&1
}

for VERSION in '' 0.2.9 v0.2.9; do
  export VERSION
  if [ -n "$VERSION" ]; then
    EXPECTED_METADATA_URL="https://github-proxy.tinfoil.sh/repos/tinfoilsh/tinfoil-proxy/releases/tags/v${VERSION#v}"
  else
    EXPECTED_METADATA_URL="https://github-proxy.tinfoil.sh/repos/tinfoilsh/tinfoil-proxy/releases/latest"
  fi
  export EXPECTED_METADATA_URL

  for layout in formatted compact; do
    write_release "$layout" v0.2.9 "$EXPECTED_ASSET" "\"$DIGEST\""
    cp "$TEST_DIR/binary" "$TEST_DIR/download"
    rm -f "$TEST_DIR/install/tinfoil-proxy"
    if ! run_installer; then
      cat "$TEST_DIR/output"
      exit 1
    fi
    cmp "$TEST_DIR/binary" "$TEST_DIR/install/tinfoil-proxy"
    test -x "$TEST_DIR/install/tinfoil-proxy"
  done
done

for layout in formatted compact; do
  for failure in corrupt missing-digest missing-asset invalid-digest invalid-json redirected-download; do
    write_release "$layout" v0.2.9 "$EXPECTED_ASSET" "\"$DIGEST\""
    cp "$TEST_DIR/binary" "$TEST_DIR/download"
    EXPECTED_BINARY_URL="$RELEASE_DOWNLOAD_URL/v0.2.9/$EXPECTED_ASSET"
    case "$failure" in
      corrupt) printf 'corrupt download\n' > "$TEST_DIR/download" ;;
      missing-digest) write_release "$layout" v0.2.9 "$EXPECTED_ASSET" null ;;
      missing-asset) write_release "$layout" v0.2.9 renamed-asset "\"$DIGEST\"" ;;
      invalid-digest) write_release "$layout" v0.2.9 "$EXPECTED_ASSET" '"sha256:invalid"' ;;
      invalid-json) printf '{' > "$TEST_DIR/release.json" ;;
      redirected-download)
        # Downloaders collapse ".." segments, so this version would fetch the
        # binary from another repository; serve it to prove the installer refuses.
        REDIRECTED_VERSION="0.2.9/../../../../../attacker/proxy/releases/download/v0.2.9"
        write_release "$layout" "v$REDIRECTED_VERSION" "$EXPECTED_ASSET" "\"$DIGEST\""
        EXPECTED_BINARY_URL="$RELEASE_DOWNLOAD_URL/v$REDIRECTED_VERSION/$EXPECTED_ASSET"
        ;;
    esac
    printf 'existing installation\n' > "$TEST_DIR/existing"
    cp "$TEST_DIR/existing" "$TEST_DIR/install/tinfoil-proxy"
    if run_installer; then
      echo "Installer accepted $failure in $layout metadata"
      exit 1
    fi
    cmp "$TEST_DIR/existing" "$TEST_DIR/install/tinfoil-proxy"
  done
done

echo "Installer tests passed."
