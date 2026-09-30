# Tinfoil Proxy

A verified local HTTP proxy to a [Tinfoil](https://tinfoil.sh) secure enclave. It exposes an OpenAI-compatible endpoint at `http://127.0.0.1:3301/v1`, forwards requests through the Tinfoil gateway to an enclave serving the requested model, verifies that enclave against the public attestation transparency log, and encrypts request and response bodies to it with the [Encrypted HTTP Body Protocol](https://github.com/tinfoilsh/encrypted-http-body-protocol), so neither the gateway nor any other intermediary can read them. Point any OpenAI-compatible tool at the local URL and every request runs over a verified connection.

[![Documentation](https://img.shields.io/badge/docs-tinfoil.sh-blue)](https://docs.tinfoil.sh/local-proxy/cli)

## Two separate things live in this repo

The proxy and the desktop app are independent. Most people only need the proxy.

- **The proxy (repo root)** — a tiny, self-contained Go program. Two source files (`main.go`, `proxy.go`), four direct dependencies, compiled to a single static binary with no runtime requirements. This is the whole proxy. It's all you need for scripts, CI, servers, and any OpenAI-compatible client.
- **The menu-bar app (`app/`)** — an *optional* Electron desktop wrapper that runs the exact same proxy binary with start/stop buttons and live verification status. Everything Electron, Node.js, and the build tooling lives under `app/`. If you don't want a desktop app, you can ignore that whole folder.

> The Electron/Node.js footprint lives entirely in `app/`, **not** at the root. The proxy itself is lightweight: a single Go binary that does verification and forwarding, nothing more.

Both serve the same endpoint with the same attestation, because the app just launches the binary.

## Repository layout

```
.                      The proxy (lightweight Go binary) — the core
  main.go              CLI entrypoint, flags, bind handling
  proxy.go             attestation, reverse proxy, local-only guard
  go.mod / go.sum      4 direct deps, builds with CGO disabled
  Dockerfile           container image for the binary
  install.sh           downloads the released binary

app/                   The desktop app (optional Electron wrapper)
  src/                 Electron main / preload / renderer
  package.json         Node/Electron deps and build scripts
  electron-builder.yml installer config (.pkg / .deb / .exe)
  scripts/build-cli.sh compiles the root Go binary into app/resources/bin
  assets/, build/, resources/
```

## Install the proxy

This is the lightweight path: a single binary, no desktop app.

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

Or grab a pre-built binary from the [releases page](https://github.com/tinfoilsh/tinfoil-proxy/releases/latest). If you'd rather have a desktop app instead, see [Menu-bar app](#menu-bar-app) below.

## Usage

```sh
tinfoil-proxy
```

It listens on `http://127.0.0.1:3301` and forwards to `https://inference-gateway.tinfoil.sh`. Each request is sealed to an enclave serving the model it names; the proxy verifies an enclave's attestation before its first request and re-verifies when the enclave rotates its key. `GET /v1/models` lists catalog models eligible under the configured pinning policy. Only the `Authorization`, `Content-Type` and `Accept` headers are forwarded, and request bodies over 64 MiB are rejected. Point any OpenAI-compatible client at:

```text
Base URL: http://127.0.0.1:3301/v1
```

Audio transcription uploads can use standard `multipart/form-data` with a
`model` field and a `file` part in either order. The proxy buffers the upload
in memory within the 64 MiB request limit, preserves its bytes and boundary,
and can replay it after a gateway reroute or enclave key rotation. Missing,
empty, or duplicate model fields and malformed uploads return HTTP 400;
oversized uploads return HTTP 413.

To use another gateway, set `--gateway`:

```sh
tinfoil-proxy --gateway https://inference-gateway.tinfoil.sh -p 3301
```

### Options

| Flag | Default | Description |
| ---- | ------- | ----------- |
| `-p, --port` | `3301` | Port to listen on |
| `-b, --bind` | `127.0.0.1` | Address to bind to (use `0.0.0.0` in Docker) |
| `--gateway` | `https://inference-gateway.tinfoil.sh` | Tinfoil gateway URL |
| `--pin MODEL=REF` | unset | Pin a model's repository, tag, or digest; repeat for multiple models |
| `--pinned-only` | off | Serve only explicitly pinned models; requires at least one `--pin` |
| `--log-format` | `text` | `text` or `json` |
| `--user-cache-secret` | generated | Prompt-cache scoping secret — see [Prompt Cache Scoping](#prompt-cache-scoping) |
| `-v, --verbose` | off | Verbose output |
| `-t, --trace` | off | Trace output |

Once it's running, the endpoint is just a regular OpenAI-compatible base URL — see the [coding agents guide](https://docs.tinfoil.sh/tutorials/coding-agents) for plug-and-play setups, or the [CLI docs](https://docs.tinfoil.sh/local-proxy/cli) for the full reference.

## Model pinning

Pinning is off by default. Add `--pin MODEL=REF` to constrain a model to a
repository reference. Repeat the flag for each model:

```sh
tinfoil-proxy \
  --pin "glm-5-3=$GLM_RELEASE_REF" \
  --pin "deepseek-v4-1-flash=$DEEPSEEK_RELEASE_REF" \
  --pinned-only
```

Set each release reference to `tinfoilsh/name`, `tinfoilsh/name@tag`, or
`tinfoilsh/name@sha256:<64 lowercase hex characters>`. A reference can include
both a tag and a digest. A repository-only pin follows releases within that
repository. A digest pin restricts the accepted artifact. Only the `tinfoilsh`
owner is supported. Commas and `=` within a valid tag are preserved.

Without `--pinned-only`, models without pins remain eligible. With it, every
request must name an explicitly pinned model, including after catalog refreshes.
Duplicate model pins and `--pinned-only` without any pins fail at startup. Model
identifiers are exact and case-sensitive. Restart the proxy to change pins.

`GET /v1/models` filters the current catalog through the same eligibility policy.
A listing does not guarantee availability or successful attestation, and a
catalog refresh can change it. A catalog repository that conflicts with a pin
fails as an attestation error. The proxy refuses to send inference to that
replica and reports the existing attestation-failure event to the desktop app.

The CLI accepts repository references only. Per-model register and freshness
policies are available through the Go SDK. Desktop pin configuration is not
included in this release.

## Verification data

Open `http://127.0.0.1:3301/verifications`, or use **View verification data** in
the desktop app, to inspect the latest successful verification for each cached
replica. The JSON includes requested model names, the expected repository
reference, accepted measurements and keys, verification time, and evidence expiry.
Model names describe routing and are not attested model identities.

The list starts empty and updates as requests verify replicas. Reading it does
not contact enclaves. Results remain available after catalog removal and may
have expired or been invalidated. These are historical results, not current
health, failed-attempt history, or an audit of individual requests. Restarting
the proxy clears the list and changes its instance ID.

## Prompt Cache Scoping

Each enclave partitions its prompt cache by a `cache_salt` derived from both
`user_cache_secret` and the bearer API key. Requests with the same key and
secret can share cached prompt prefixes and cache-hit timing. Changing either
separates the cache namespace. The secret stays on the machine; the proxy
replaces it with the derived salt inside the encrypted body.

The SDK uses HKDF-SHA256 with separate `tinfoil/client-cache-salt/v2` and
`tinfoil/client-cache-route/v2` labels. Both use the user secret as input key
material and the API key as the HKDF salt. The gateway receives only an
HMAC-SHA256 of the conversation's prompt prefix under the derived routing key.
It can recognize repeated routing values, but knowing the API key does not
let it derive the cache salt without the private user secret. Use a strong,
random user secret. This derivation change starts new cache namespaces, as
does rotating the API key; the persisted user secret stays unchanged.

Treat a cache secret as sensitive cache-partition data. It is not authentication, authorization, or encryption, and knowing one does not grant API access, but reusing or disclosing one can place requests in the same timing-sharing namespace.

By default the proxy generates a random secret and persists it at `~/.tinfoil/user_cache_secret` (mode `0600`, shared with the Tinfoil SDKs on the same machine), providing a stable per-machine namespace. Resolution uses the first non-empty value in this order: a `user_cache_secret` string in the request body, `--user-cache-secret`, `TINFOIL_USER_CACHE_SECRET`, then the persisted or newly generated secret:

```sh
# Pin the secret for every request this proxy forwards
tinfoil-proxy --user-cache-secret "$SECRET"

# Or provision it via the environment
TINFOIL_USER_CACHE_SECRET="$SECRET" tinfoil-proxy
```

Multi-user services must supply a stable, non-empty, opaque per-user or per-group `user_cache_secret` on every eligible request. Do not rely on the proxy-level default for user separation:

```json
{"model": "gpt-oss-120b", "messages": [], "user_cache_secret": "<per-user secret>"}
```

Scoping applies only to JSON POST bodies for chat completions, completions, and responses endpoints.

If the generated secret cannot be persisted (for example, no home directory or a read-only filesystem), the proxy warns and uses a process-lifetime in-memory secret. Requests remain partitioned for that runtime, but cache continuity resets on restart. Containerized deployments that need continuity across replicas should provide a stable non-empty value, while multi-user services should still override it per eligible request.

## Menu-bar app

This is the *optional* desktop wrapper. It is not required to use the proxy. Tinfoil Proxy wraps the same binary in a menu-bar app with start/stop, port, and live verification status. Install the `.pkg` (macOS), `.deb` (Linux), or `.exe` (Windows) from the [releases page](https://github.com/tinfoilsh/tinfoil-proxy/releases/latest), then open it once to put it in your menu bar. macOS and Windows builds auto-update.

On Linux it lives entirely in the system tray, so a StatusNotifierItem host must be present — on GNOME (Ubuntu's default) install the [AppIndicator extension](https://extensions.gnome.org/extension/615/appindicator-support/); KDE, Cinnamon, and XFCE work out of the box.

See the [app guide](https://docs.tinfoil.sh/local-proxy/app) for the full walkthrough.

## Development

### Proxy only (Go)

Requires Go 1.27.1+. No Node.js needed.

```sh
go run .            # run the proxy locally
go build -o tinfoil-proxy .
```

### Desktop app (Electron)

Requires Node.js 20+ and Go 1.27.1+ (the app embeds the Go binary). All app commands run from the `app/` directory.

```sh
cd app
npm install
npm run dev    # builds the proxy into app/resources/bin/, then starts Electron with hot-reload
```

`app/scripts/build-cli.sh` cross-compiles the root Go proxy into `app/resources/bin/`, and `electron-builder` bundles it into the installer. To cut a release, bump `"version"` in `app/package.json`, merge to `main`, then `git tag v0.X.Y && git push origin v0.X.Y` — the `release.yml` workflow publishes the installers, the standalone binaries, and the `ghcr.io/tinfoilsh/tinfoil-proxy` image.

This is the canonical home for the proxy; the legacy `tinfoil proxy` subcommand in [tinfoilsh/tinfoil-cli](https://github.com/tinfoilsh/tinfoil-cli) is deprecated.
