#!/bin/sh

set -eu

REPO_DIR="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)"
TEST_DIR="$(mktemp -d)"
trap 'rm -rf "$TEST_DIR"' EXIT
export TEST_DIR

mkdir "$TEST_DIR/bin" "$TEST_DIR/install"
cat > "$TEST_DIR/bin/uname" <<'EOF'
#!/bin/sh
case "$1" in
  -s) echo Linux ;;
  -m) echo x86_64 ;;
  *) exit 1 ;;
esac
EOF
cat > "$TEST_DIR/bin/curl" <<'EOF'
#!/bin/sh
set -eu
test "$1" = "-fsSL"
test "$3" = "-o"
case "$2" in
  "$EXPECTED_METADATA_URL")
    cp "$TEST_DIR/release.json" "$4"
    ;;
  https://github.com/tinfoilsh/tinfoil-proxy/releases/download/v0.2.9/tinfoil-proxy-linux-amd64)
    cp "$TEST_DIR/download" "$4"
    ;;
  *)
    echo "Unexpected download URL: $2" >&2
    exit 1
    ;;
esac
EOF
chmod +x "$TEST_DIR/bin/uname" "$TEST_DIR/bin/curl"

printf '#!/bin/sh\nexit 0\n' > "$TEST_DIR/binary"
if command -v sha256sum >/dev/null 2>&1; then
  DIGEST="$(sha256sum "$TEST_DIR/binary" | cut -d ' ' -f 1)"
else
  DIGEST="$(shasum -a 256 "$TEST_DIR/binary" | cut -d ' ' -f 1)"
fi
jq -n --arg digest "sha256:$DIGEST" '{
  body: "Release notes containing \"tag_name\": \"v999\" and \"digest\": \"sha256:wrong\"",
  assets: [
    {name: "another-binary", digest: "sha256:wrong"},
    {name: "tinfoil-proxy-linux-amd64", digest: $digest},
    {name: "neighboring-binary", digest: $digest}
  ],
  tag_name: "v0.2.9"
}' > "$TEST_DIR/pretty.json"

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

  for format in pretty compact; do
    if [ "$format" = compact ]; then
      jq -c . "$TEST_DIR/pretty.json" > "$TEST_DIR/release.json"
    else
      cp "$TEST_DIR/pretty.json" "$TEST_DIR/release.json"
    fi
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

for failure in corrupt missing-digest missing-asset invalid-digest invalid-json; do
  cp "$TEST_DIR/pretty.json" "$TEST_DIR/release.json"
  cp "$TEST_DIR/binary" "$TEST_DIR/download"
  case "$failure" in
    corrupt) printf 'corrupt download\n' > "$TEST_DIR/download" ;;
    missing-digest) jq '.assets[1].digest = null' "$TEST_DIR/pretty.json" > "$TEST_DIR/release.json" ;;
    missing-asset) jq 'del(.assets[1])' "$TEST_DIR/pretty.json" > "$TEST_DIR/release.json" ;;
    invalid-digest) jq '.assets[1].digest = "sha256:invalid"' "$TEST_DIR/pretty.json" > "$TEST_DIR/release.json" ;;
    invalid-json) printf '{' > "$TEST_DIR/release.json" ;;
  esac
  printf 'existing installation\n' > "$TEST_DIR/existing"
  cp "$TEST_DIR/existing" "$TEST_DIR/install/tinfoil-proxy"
  if run_installer; then
    echo "Installer accepted $failure"
    exit 1
  fi
  cmp "$TEST_DIR/existing" "$TEST_DIR/install/tinfoil-proxy"
done

echo "Installer tests passed."
