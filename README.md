# Tinfoil Proxy

A verified local HTTP proxy to a [Tinfoil](https://tinfoil.sh) secure enclave. The Go
binary opens `http://127.0.0.1:3301/v1`, verifies the upstream enclave against the public
attestation transparency log, pins the attested public key, and forwards OpenAI-compatible
traffic. An Electron menu-bar wrapper (Tinfoil Proxy.app) is built from the same repo and
supervises the same binary, exposing start/stop, port, and live verification status from
the status bar.

Install the standalone CLI:

```sh
go install github.com/tinfoilsh/tinfoil-proxy@latest
tinfoil-proxy --port 3301
```

Or grab a pre-built CLI binary or the menu-bar app installer from the
[releases page](https://github.com/tinfoilsh/tinfoil-proxy/releases/latest).

This is the canonical home for the proxy server. The legacy `tinfoil proxy` subcommand in
[tinfoilsh/tinfoil-cli](https://github.com/tinfoilsh/tinfoil-cli) is deprecated.

## Layout

```
main.go         CLI entry point (cobra flags, defaults, error wiring)
proxy.go        Reverse proxy + verified-client wiring + handshake protocol
go.mod / go.sum Go module + locked dependency graph
src/
  main/         Electron main process (proxy lifecycle, IPC, menu, popup, updater)
  preload/      contextBridge exposing a typed window.tinfoil API to the renderer
  renderer/     React popup UI (status, on/off, port, endpoint)
assets/         App and tray-status icons (PNG + macOS .icns)
build/          Code-signing entitlements, .pkg postinstall, iconset source
scripts/        build-cli.sh — compiles the proxy from the local module
resources/bin/  (gitignored) The tinfoil-proxy binary copied in at build time
```

## How the bundled proxy works

`scripts/build-cli.sh` cross-compiles the proxy from the in-repo Go source for the target
OS/arch (universal binary on macOS), writes it to `resources/bin/tinfoil-proxy`, and
`electron-builder` ships it inside the installer. There's no fetching from a separate repo —
the Go and Electron sources live side by side and are released together.

## Handshake protocol

When the Electron app launches the bundled binary it passes a hidden `--handshake` flag.
The Go child then runs attestation, binds its listener, writes a single JSON line to
`stdout` (`{"event":"ready","enclave":"...","repo":"...","listen":"..."}`), and waits on
`stdin` for `go\n` or `abort\n`. The parent independently re-verifies the announced enclave
and only sends `go` when the JS-side verification matches. Without `--handshake` (the
standalone CLI case) the binary serves immediately, just like before.

## Requirements

- Node.js 20+
- Go 1.25+
- macOS 13+ recommended for development; Linux / Windows build paths exist but are less
  exercised locally

## Development

```bash
npm install
npm run dev
```

`npm run dev` first compiles the `tinfoil-proxy` binary into `resources/bin/`, then launches
the Electron app with hot-reload for the renderer.

### Useful scripts

| Script | Purpose |
|--------|---------|
| `npm run dev` | Build the proxy for the host OS and start Electron with hot-reload |
| `npm run build` | Production build of main + preload + renderer into `out/` |
| `npm run lint` | ESLint with `--max-warnings 0` |
| `npm run typecheck` | TypeScript projects for both Node and Web |
| `npm run build:cli` | Just rebuild the embedded `tinfoil-proxy` binary |
| `npm run package:mac:x64` | Build macOS installers for Intel (`.pkg`, `.dmg`, `.zip`) |
| `npm run package:mac:arm64` | Build macOS installers for Apple silicon |
| `npm run package:linux` | Build the Linux installer (`.deb`) |
| `npm run package:win` | Build Windows installer (`.exe`, NSIS) |

The macOS builds run per-architecture to avoid an
[electron-builder race](https://github.com/electron-userland/electron-builder/issues) where
the `.pkg` builder removes a shared `distribution.xml` file. CI runs them as two parallel
macOS jobs.

## Packaging for distribution

`electron-builder` reads `electron-builder.yml`. The macOS configuration enables Apple's
Hardened Runtime, ships entitlements for V8 JIT and dynamic library loading, and runs
notarization at build time.

To produce a signed + notarized macOS release locally:

```bash
export CSC_LINK="$(base64 -i /path/to/DeveloperIDApplication.p12)"
export CSC_KEY_PASSWORD='the export password'
export CSC_INSTALLER_LINK="$(base64 -i /path/to/DeveloperIDInstaller.p12)"
export CSC_INSTALLER_KEY_PASSWORD='the export password'
export APPLE_ID="releases@example.com"
export APPLE_APP_SPECIFIC_PASSWORD="abcd-efgh-ijkl-mnop"
export APPLE_TEAM_ID="ABCDE12345"

npm run package:mac:arm64
```

If those env vars are absent, set `CSC_IDENTITY_AUTO_DISCOVERY=false` to produce an
unsigned build for smoke testing.

### App bundle layout (macOS)

```
/Applications/Tinfoil.app/
  Contents/
    MacOS/Tinfoil                 # Electron host
    Resources/
      bin/tinfoil-proxy           # Universal (x64 + arm64) proxy binary
      app.asar                    # Renderer + main process bundle
      assets/icon-tray-*.png      # Menu-bar template icons
```

The `.pkg` installer drops the app into `/Applications` and runs `build/pkg-scripts/postinstall`,
which `open`s the app once so the tray shows up immediately.

## CI

Three GitHub Actions workflows live in `.github/workflows/`:

- `test.yml` — runs on every PR / push to `main`; installs deps, lints, typechecks, and
  produces an unsigned production build on Ubuntu.
- `release.yml` — runs on tag pushes (`v*`); builds, signs, and publishes installers for
  macOS (x64 + arm64) / Linux / Windows to the GitHub release matching the tag, then
  cross-compiles the standalone `tinfoil-proxy` binary for darwin/linux/windows (amd64 +
  arm64 where applicable) and uploads those alongside the Electron installers.
- `zizmor.yml` — audits all workflow files for common GitHub Actions security issues
  whenever a workflow changes.

The release workflow expects these repository secrets (only used by the macOS jobs):

| Secret | Purpose |
|--------|---------|
| `APPLE_CERTIFICATE_P12_BASE64` | Developer ID **Application** cert + key, base64 (`base64 -i cert.p12 \| tr -d '\n'`) |
| `APPLE_CERTIFICATE_P12_PASSWORD` | Export password for the Application `.p12` |
| `APPLE_INSTALLER_P12_BASE64` | Developer ID **Installer** cert + key, base64 |
| `APPLE_INSTALLER_P12_PASSWORD` | Export password for the Installer `.p12` |
| `APPLE_ID` | Apple ID email used for notarization |
| `APPLE_APP_SPECIFIC_PASSWORD` | App-specific password for notarytool |
| `APPLE_TEAM_ID` | 10-character Apple developer team ID |

`GH_TOKEN` is wired automatically from `secrets.GITHUB_TOKEN`. Each `.p12` must contain
both the certificate **and** its private key — when exporting from Keychain Access, select
the certificate row (not the indented private-key row) and use a real export password.

## Auto-updates

The packaged app polls GitHub Releases every 6 hours via `electron-updater` and prompts the
user when a new build is available. macOS `.zip` / `.dmg` update in-place. The Linux `.deb`
does not receive auto-updates; users on that target must reinstall the latest `.deb`
manually.

## Linux notes

The app lives entirely in the system tray, so a StatusNotifierItem host must be present:

- **GNOME (default on Ubuntu)** removed the legacy tray. Install the
  [AppIndicator and KStatusNotifierItem Support](https://extensions.gnome.org/extension/615/appindicator-support/)
  extension (or the `gnome-shell-extension-appindicator` package) so the icon appears.
  KDE, Cinnamon, XFCE, and most other panels work out of the box.
- The `.deb` declares the runtime indicator library (`libayatana-appindicator3-1`, falling
  back to `libappindicator3-1`); on a minimal system install one of those manually. Its GTK
  and at-spi dependencies are declared with `…t64 |` alternatives so the package installs on
  both pre- and post-`t64` releases (Ubuntu 22.04 and 24.04+).
- The tray icon uses a left-click context menu on Linux (the indicator backend doesn't
  forward click coordinates). **Show status…** opens the panel as a normal,
  centered window. Launching the app again while it's running re-opens that window, which is
  handy if the tray icon isn't visible.
- Linux ships only as a `.deb`, which installs its `chrome-sandbox` helper as root-owned
  SUID so the Chromium sandbox stays enabled. (An AppImage can't carry a SUID helper and
  would run unsandboxed on kernels that restrict unprivileged user namespaces, e.g. Ubuntu
  23.10+, so that target is intentionally not built.)
- Launch-at-login is managed by the desktop environment on Linux and is not exposed in the
  app menu.

## Security notes

- The renderer runs sandboxed: `contextIsolation: true`, `sandbox: true`,
  `nodeIntegration: false`. All renderer → main calls go through a typed preload bridge.
- The popup HTML ships a strict Content-Security-Policy that loads only local assets and
  disallows remote and embedded content (`frame-src 'none'`, `connect-src 'self'`).
- The macOS app runs under Hardened Runtime with notarization enabled by default;
  unsigned builds are only produced when `CSC_IDENTITY_AUTO_DISCOVERY=false` is set.

## Releasing

1. Bump `"version"` in `package.json`.
2. Commit, push, open a PR, merge to `main`.
3. From `main`: `git tag v0.X.Y && git push origin v0.X.Y`.
4. The release workflow builds and uploads installers; the resulting GitHub Release is what
   `electron-updater` consumes for auto-updates.
