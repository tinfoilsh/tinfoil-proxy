#!/usr/bin/env bash
set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_DIR="$(cd "$DIR/.." && pwd)"
ROOT="$(cd "$APP_DIR/.." && pwd)"

OUT_DIR="$APP_DIR/resources/bin"
mkdir -p "$OUT_DIR"

TARGET_OS="${TINFOIL_PROXY_GOOS:-$(go env GOOS)}"
BIN_NAME="tinfoil-proxy"
case "$TARGET_OS" in
  windows) BIN_NAME="tinfoil-proxy.exe" ;;
esac
OUT="$OUT_DIR/$BIN_NAME"

cd "$ROOT"

build_one() {
  local goos="$1" goarch="$2" out="$3"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags="-s -w" -o "$out" .
}

case "$TARGET_OS" in
  darwin)
    TMPDIR="$(mktemp -d)"
    trap 'rm -rf "$TMPDIR"' EXIT
    build_one darwin amd64 "$TMPDIR/tinfoil-proxy-amd64"
    build_one darwin arm64 "$TMPDIR/tinfoil-proxy-arm64"
    lipo -create "$TMPDIR/tinfoil-proxy-amd64" "$TMPDIR/tinfoil-proxy-arm64" -output "$OUT"
    ;;
  windows)
    build_one windows amd64 "$OUT"
    ;;
  linux)
    build_one linux "$(go env GOARCH)" "$OUT"
    ;;
  *)
    build_one "$TARGET_OS" "$(go env GOARCH)" "$OUT"
    ;;
esac

echo "Built $OUT"
