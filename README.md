# Tinfoil Proxy

A verified local HTTP proxy to a [Tinfoil](https://tinfoil.sh) secure enclave. It exposes an OpenAI-compatible endpoint at `http://127.0.0.1:3301/v1`, forwards requests directly to a verified Tinfoil router enclave, verifies that enclave against the public attestation transparency log, and by default pins every connection to the enclave's attested TLS key, so intermediaries cannot read or alter them. Point any OpenAI-compatible tool at the local URL and its requests go to the verified enclave.

[![Documentation](https://img.shields.io/badge/docs-tinfoil.sh-blue)](https://docs.tinfoil.sh/local-proxy/cli)

The proxy is a single Go binary at the repository root. `app/` holds an optional Electron menu-bar app that runs the same binary; see the [app guide](https://docs.tinfoil.sh/local-proxy/app).

## Installation

```sh
curl -fsSL https://github.com/tinfoilsh/tinfoil-proxy/raw/main/install.sh | sh
```

Or run `go install github.com/tinfoilsh/tinfoil-proxy@latest`, run `docker run --rm -p 127.0.0.1:3301:3301 ghcr.io/tinfoilsh/tinfoil-proxy`, or download a binary or desktop installer from the [releases page](https://github.com/tinfoilsh/tinfoil-proxy/releases/latest).

To run the proxy next to an API gateway on Kubernetes, see the [Kubernetes guide](deploy/kubernetes/README.md).

The install script verifies the downloaded binary against the SHA-256 digest published by the GitHub release API, fetched through Tinfoil's GitHub proxy to avoid per-IP anonymous API rate limits and served independently of the release download CDN. For provenance, download an artifact and run `gh attestation verify` against the release workflow identity.

## Quick Start

```sh
tinfoil-proxy
```

Point any OpenAI-compatible client at `http://127.0.0.1:3301/v1`. Run `tinfoil-proxy --help` for all flags, and see the [CLI docs](https://docs.tinfoil.sh/local-proxy/cli) and the [coding agents guide](https://docs.tinfoil.sh/tutorials/coding-agents).

By default, the proxy selects and verifies a healthy Tinfoil router. Use `--host` to select a specific enclave, with `--repo` for the GitHub repository it must run a release of or `--project` for its config registry project. The two are separate namespaces; setting both accepts either. For an enclave whose config is never published, pass the config file itself with `--config` alongside `--host`; the proxy then accepts only an enclave running exactly those bytes:

```sh
tinfoil-proxy --host enclave.example.com --config tinfoil-config.yml
```

On networks that intercept TLS, a pinned connection cannot be established. `--transport ehbp` instead seals request and response bodies to the enclave with the [Encrypted HTTP Body Protocol](https://github.com/tinfoilsh/encrypted-http-body-protocol). In that mode the method, URL, and headers (including the API key) are visible to the intercepting party, requests without a body and their responses are not sealed, and WebSockets are refused. An error response to a sealed request that the enclave did not seal is withheld: the client receives its status with a body written by the proxy.

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

Read [AGENTS.md](AGENTS.md) before cutting a release. This is the canonical home for the proxy; the `tinfoil proxy` subcommand in [tinfoil-cli](https://github.com/tinfoilsh/tinfoil-cli) is deprecated.

## Reporting Vulnerabilities

Please report security vulnerabilities by either:

- Emailing [security@tinfoil.sh](mailto:security@tinfoil.sh)
- Opening an issue on GitHub on this repository

We aim to respond to (legitimate) security reports within 24 hours.
