# Tinfoil Proxy

A verified local HTTP proxy to a [Tinfoil](https://tinfoil.sh) secure enclave. It exposes an OpenAI-compatible endpoint at `http://127.0.0.1:3301/v1`, verifies the upstream enclave against the public attestation transparency log, pins the attested public key, and forwards your traffic. Point any OpenAI-compatible tool at the local URL and every request runs over a verified connection.

[![Documentation](https://img.shields.io/badge/docs-tinfoil.sh-blue)](https://docs.tinfoil.sh/local-proxy/cli)

It comes in two forms, both built from this repo and serving the same endpoint with the same attestation:

- **`tinfoil-proxy`** — a standalone CLI binary, great for scripts, CI, and headless servers.
- **Tinfoil Proxy** — a menu-bar app that wraps the binary with start/stop controls and live verification status.

## Install

Install script (macOS / Linux):

```sh
curl -fsSL https://github.com/tinfoilsh/tinfoil-proxy/raw/main/install.sh | sh
```

From source:

```sh
go install github.com/tinfoilsh/tinfoil-proxy@latest
```

Docker (binds `0.0.0.0` inside the container; publish to `127.0.0.1` to stay loopback-only):

```sh
docker run --rm -p 127.0.0.1:3301:3301 ghcr.io/tinfoilsh/tinfoil-proxy
```

Or grab a pre-built binary or the menu-bar app installer (`.pkg` / `.deb` / `.exe`) from the [releases page](https://github.com/tinfoilsh/tinfoil-proxy/releases/latest).

## Usage

```sh
tinfoil-proxy
```

It listens on `http://127.0.0.1:3301`, auto-selects a Tinfoil router enclave, verifies its attestation, and pins the attested key for the rest of the session (re-verifying if the enclave rotates its certificate). Point any OpenAI-compatible client at:

```text
Base URL: http://127.0.0.1:3301/v1
```

To pin a specific enclave, set `--host` and `--repo` together — they're all-or-nothing, so leave both unset for auto-discovery:

```sh
tinfoil-proxy -e inference.tinfoil.sh -r tinfoilsh/confidential-model-router -p 3301
```

### Options

| Flag | Default | Description |
| ---- | ------- | ----------- |
| `-p, --port` | `3301` | Port to listen on |
| `-b, --bind` | `127.0.0.1` | Address to bind to (use `0.0.0.0` in Docker) |
| `-e, --host` | auto | Pin a specific enclave hostname (set with `-r`) |
| `-r, --repo` | auto | Pin a specific config repo (set with `-e`) |
| `--log-format` | `text` | `text` or `json` |
| `-v, --verbose` | off | Verbose output |
| `-t, --trace` | off | Trace output |

Once it's running, the endpoint is just a regular OpenAI-compatible base URL — see the [coding agents guide](https://docs.tinfoil.sh/tutorials/coding-agents) for plug-and-play setups, or the [CLI docs](https://docs.tinfoil.sh/local-proxy/cli) for the full reference.

## Menu-bar app

Tinfoil Proxy wraps the same binary in a menu-bar app with start/stop, port, and live verification status. Install the `.pkg` (macOS), `.deb` (Linux), or `.exe` (Windows) from the [releases page](https://github.com/tinfoilsh/tinfoil-proxy/releases/latest), then open it once to put it in your menu bar. macOS and Windows builds auto-update.

On Linux it lives entirely in the system tray, so a StatusNotifierItem host must be present — on GNOME (Ubuntu's default) install the [AppIndicator extension](https://extensions.gnome.org/extension/615/appindicator-support/); KDE, Cinnamon, and XFCE work out of the box.

See the [app guide](https://docs.tinfoil.sh/local-proxy/app) for the full walkthrough.

## Development

Requires Node.js 20+ and Go 1.25+.

```sh
npm install
npm run dev    # builds the proxy into resources/bin/, then starts Electron with hot-reload
```

`scripts/build-cli.sh` cross-compiles the Go proxy into `resources/bin/`, and `electron-builder` bundles it into the installer. To cut a release, bump `"version"` in `package.json`, merge to `main`, then `git tag v0.X.Y && git push origin v0.X.Y` — the `release.yml` workflow publishes the installers, the standalone binaries, and the `ghcr.io/tinfoilsh/tinfoil-proxy` image.

This is the canonical home for the proxy; the legacy `tinfoil proxy` subcommand in [tinfoilsh/tinfoil-cli](https://github.com/tinfoilsh/tinfoil-cli) is deprecated.
