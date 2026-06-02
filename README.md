# Tinfoil Proxy

A verified local HTTP proxy to a [Tinfoil](https://tinfoil.sh) secure enclave. The Go binary opens `http://127.0.0.1:3301/v1`, verifies the upstream enclave against the public attestation transparency log, pins the attested public key, and forwards OpenAI-compatible traffic. An Electron menu-bar app (Tinfoil Proxy.app) is built from the same repo, supervises the same binary, and shows live verification status in the menu bar.

This is the canonical home for the proxy server; the legacy `tinfoil proxy` subcommand in [tinfoilsh/tinfoil-cli](https://github.com/tinfoilsh/tinfoil-cli) is deprecated.

## Install

Install script (macOS / Linux):

```sh
curl -fsSL https://github.com/tinfoilsh/tinfoil-proxy/raw/main/install.sh | sh
tinfoil-proxy --port 3301
```

From source:

```sh
go install github.com/tinfoilsh/tinfoil-proxy@latest
```

Docker (binds `0.0.0.0` inside the container; publish to `127.0.0.1` to stay loopback-only):

```sh
docker run --rm -p 127.0.0.1:3301:3301 ghcr.io/tinfoilsh/tinfoil-proxy
```

Pre-built CLI binaries and the menu-bar app installer are on the [releases page](https://github.com/tinfoilsh/tinfoil-proxy/releases/latest).

## Layout

```text
main.go         CLI entry point (cobra flags, defaults, error wiring)
proxy.go        Reverse proxy + verified-client wiring + handshake protocol
src/main/       Electron main process (proxy lifecycle, IPC, menu, popup, updater)
src/preload/    contextBridge exposing a typed window.tinfoil API to the renderer
src/renderer/   React popup UI (status, on/off, port, endpoint)
scripts/        build-cli.sh — compiles the proxy from the local Go module
resources/bin/  (gitignored) the tinfoil-proxy binary copied in at build time
```

The Go and Electron sources live side by side and ship together: `scripts/build-cli.sh` cross-compiles the proxy (universal binary on macOS) into `resources/bin/`, and `electron-builder` bundles it into the installer.

## Handshake protocol

When the app launches the bundled binary it passes a hidden `--handshake` flag. The Go child runs attestation, binds its listener, writes a single JSON line to `stdout` (`{"event":"ready","enclave":"...","repo":"...","listen":"..."}`), and waits on `stdin` for `go\n` or `abort\n`. The parent independently re-verifies the announced enclave and only sends `go` when its verification matches. Without `--handshake` (the standalone CLI) the binary serves immediately.

## Development

```bash
npm install
npm run dev    # builds the proxy into resources/bin/, then starts Electron with hot-reload
```

Requires Node.js 20+ and Go 1.25+.

| Script | Purpose |
| ------ | ------- |
| `npm run dev` | Build the proxy for the host OS and start Electron with hot-reload |
| `npm run build` | Production build of main + preload + renderer into `out/` |
| `npm run lint` | ESLint with `--max-warnings 0` |
| `npm run typecheck` | TypeScript for both Node and Web |
| `npm run build:cli` | Rebuild just the embedded `tinfoil-proxy` binary |
| `npm run package:mac:x64` / `:arm64` | macOS installers (`.pkg`, `.dmg`, `.zip`), per-arch |
| `npm run package:linux` | Linux installer (`.deb`) |
| `npm run package:win` | Windows installer (`.exe`, NSIS) |

macOS packages build per-architecture to avoid an electron-builder race on the shared `distribution.xml`; CI runs them as two parallel jobs.

## Releasing

1. Bump `"version"` in `package.json` and merge to `main`.
2. Tag from `main`: `git tag v0.X.Y && git push origin v0.X.Y`.

The `release.yml` workflow (on `v*` tags) signs and publishes the macOS / Linux / Windows installers, the standalone `tinfoil-proxy` binaries, and a multi-arch Docker image (`ghcr.io/tinfoilsh/tinfoil-proxy`) to the matching GitHub release; `electron-updater` consumes that release for auto-updates. `test.yml` lints/typechecks/builds on every PR, and `zizmor.yml` audits workflow changes.

macOS signing + notarization needs these repository secrets (set `CSC_IDENTITY_AUTO_DISCOVERY=false` locally for an unsigned build):

| Secret | Purpose |
| ------ | ------- |
| `APPLE_CERTIFICATE_P12_BASE64` / `_PASSWORD` | Developer ID **Application** cert + key (base64) and its export password |
| `APPLE_INSTALLER_P12_BASE64` / `_PASSWORD` | Developer ID **Installer** cert + key (base64) and its export password |
| `APPLE_ID` | Apple ID email used for notarization |
| `APPLE_APP_SPECIFIC_PASSWORD` | App-specific password for notarytool |
| `APPLE_TEAM_ID` | 10-character Apple developer team ID |

Each `.p12` must contain both the certificate and its private key.

## Notes

- **Security:** the renderer is sandboxed (`contextIsolation`, `sandbox`, no `nodeIntegration`) and reaches the main process only through a typed preload bridge; the popup ships a strict CSP. macOS builds run under Hardened Runtime with notarization.
- **Linux:** ships only as a `.deb`. The app lives in the system tray, so a StatusNotifierItem host must be present — on GNOME (Ubuntu's default) install the [AppIndicator extension](https://extensions.gnome.org/extension/615/appindicator-support/); KDE, Cinnamon, and XFCE work out of the box. The `.deb` does not auto-update; reinstall the latest manually.
