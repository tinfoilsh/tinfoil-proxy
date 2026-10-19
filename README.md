# Tinfoil Proxy

A verified local HTTP proxy to a [Tinfoil](https://tinfoil.sh) secure enclave. It exposes an OpenAI-compatible endpoint at `http://127.0.0.1:3301/v1`, forwards requests through the Tinfoil gateway to an enclave serving the requested model, verifies that enclave against the public attestation transparency log, and encrypts request and response bodies to it with the [Encrypted HTTP Body Protocol](https://github.com/tinfoilsh/encrypted-http-body-protocol), so neither the gateway nor any other intermediary can read them. Point any OpenAI-compatible tool at the local URL and every request runs over a verified connection.

[![Documentation](https://img.shields.io/badge/docs-tinfoil.sh-blue)](https://docs.tinfoil.sh/local-proxy/cli)

The proxy is a single Go binary at the repository root. `app/` holds an optional Electron menu-bar app that runs the same binary; see the [app guide](https://docs.tinfoil.sh/local-proxy/app).

## Installation

```sh
curl -fsSL https://github.com/tinfoilsh/tinfoil-proxy/raw/main/install.sh | sh
```

Or run `go install github.com/tinfoilsh/tinfoil-proxy@latest`, run `docker run --rm -p 127.0.0.1:3301:3301 ghcr.io/tinfoilsh/tinfoil-proxy`, or download a binary or desktop installer from the [releases page](https://github.com/tinfoilsh/tinfoil-proxy/releases/latest).

## Quick Start

```sh
tinfoil-proxy
```

Point any OpenAI-compatible client at `http://127.0.0.1:3301/v1`. The proxy forwards only the `Authorization`, `Content-Type`, and `Accept` headers and rejects request bodies over 64 MiB. Run `tinfoil-proxy --help` for all flags, and see the [CLI docs](https://docs.tinfoil.sh/local-proxy/cli) and the [coding agents guide](https://docs.tinfoil.sh/tutorials/coding-agents).

`--pin MODEL=REF` constrains a model to `tinfoilsh/name`, `tinfoilsh/name@tag`, or `tinfoilsh/name@sha256:<digest>`; repeat it for each model. A repository-only pin follows that repository's releases, and a digest pin accepts only that artifact. `--pinned-only` refuses every model without a pin. Restart the proxy to change pins.

`http://127.0.0.1:3301/verifications` lists the latest successful verification of each replica the proxy has used. These are historical results, not current health, and model names are routing labels, not attested identities.

Multi-user services must send a distinct `user_cache_secret` for each user on every eligible request rather than rely on the proxy-wide default; see [Prompt caching](https://docs.tinfoil.sh/sdk/prompt-caching).

## Development

Requires the Go version in [`go.mod`](go.mod); the desktop app also needs Node.js 20.

```sh
go run .                               # run the proxy
go test ./...
cd app && npm install && npm run dev   # desktop app with hot reload
```

Read [AGENTS.md](AGENTS.md) before cutting a release. This is the canonical home for the proxy; the `tinfoil proxy` subcommand in [tinfoil-cli](https://github.com/tinfoilsh/tinfoil-cli) is deprecated.

## Reporting Vulnerabilities

Please report security vulnerabilities by either:

- Emailing [security@tinfoil.sh](mailto:security@tinfoil.sh)
- Opening an issue on GitHub on this repository

We aim to respond to (legitimate) security reports within 24 hours.
