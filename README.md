# Tinfoil Proxy

A verified local HTTP proxy to a [Tinfoil](https://tinfoil.sh) secure enclave. It exposes an OpenAI-compatible endpoint at `http://127.0.0.1:3301/v1`, forwards requests directly to a verified Tinfoil router enclave, verifies that enclave against the public attestation transparency log, and encrypts request and response bodies to it with the [Encrypted HTTP Body Protocol](https://github.com/tinfoilsh/encrypted-http-body-protocol), so intermediaries cannot read them. Point any OpenAI-compatible tool at the local URL and every request runs over a verified connection.

[![Documentation](https://img.shields.io/badge/docs-tinfoil.sh-blue)](https://docs.tinfoil.sh/local-proxy/cli)

The proxy is a single Go binary at the repository root. `app/` holds an optional Electron menu-bar app that runs the same binary; see the [app guide](https://docs.tinfoil.sh/local-proxy/app).

## Installation

```sh
curl -fsSL https://github.com/tinfoilsh/tinfoil-proxy/raw/main/install.sh | sh
```

Or run `go install github.com/tinfoilsh/tinfoil-proxy@latest`, run `docker run --rm -p 127.0.0.1:3301:3301 ghcr.io/tinfoilsh/tinfoil-proxy`, or download a binary or desktop installer from the [releases page](https://github.com/tinfoilsh/tinfoil-proxy/releases/latest).

The install script verifies the downloaded binary against the SHA-256 digest published by the GitHub release API, which is served independently of the release download CDN. For provenance, download an artifact and run `gh attestation verify` against the release workflow identity.

## Quick Start

```sh
tinfoil-proxy
```

Point any OpenAI-compatible client at `http://127.0.0.1:3301/v1`. Run `tinfoil-proxy --help` for all flags, and see the [CLI docs](https://docs.tinfoil.sh/local-proxy/cli) and the [coding agents guide](https://docs.tinfoil.sh/tutorials/coding-agents).

By default, the proxy selects and verifies a healthy Tinfoil router. Use `--host` and `--repo` to select a specific enclave and its configuration repository.

`http://127.0.0.1:3301/verification-document` exposes the active enclave verification document. The desktop app displays the same verification data from its proxy process.

Multi-user services must send a distinct `user_cache_secret` for each user on every eligible request rather than rely on the proxy-wide default; see [Prompt caching](https://docs.tinfoil.sh/sdk/prompt-caching).

## Desktop updates

The app authenticates downloaded updates against the release workflow's Sigstore attestation before offering them. The first release that contains the updater is installed by the previous version without verification; subsequent updates are verified.

## Development

Requires the Go version in [`go.mod`](go.mod); the desktop app also needs Node.js 20.

```sh
go test ./...
go run .   # run the proxy until interrupted
```

For the desktop app with hot reload, run `npm install && npm run dev` in `app/`.

See [Testing desktop updates](docs/testing-desktop-updates.md) for unpublished test builds and native update checks on macOS, Windows, and Linux.

Read [AGENTS.md](AGENTS.md) before cutting a release. This is the canonical home for the proxy; the `tinfoil proxy` subcommand in [tinfoil-cli](https://github.com/tinfoilsh/tinfoil-cli) is deprecated.

## Reporting Vulnerabilities

Please report security vulnerabilities by either:

- Emailing [security@tinfoil.sh](mailto:security@tinfoil.sh)
- Opening an issue on GitHub on this repository

We aim to respond to (legitimate) security reports within 24 hours.
