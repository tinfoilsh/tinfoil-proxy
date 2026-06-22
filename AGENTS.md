# Agent guide

This repo holds two independent things that ship together from one release pipeline:

- **The proxy (repo root)** — a self-contained Go binary (`main.go`, `proxy.go`). This is the core.
- **The menu-bar app (`app/`)** — an optional Electron wrapper around the same binary.

## Release invariant (read before tagging)

`app/package.json` `"version"` MUST equal the release tag (without the `v` prefix).

The release pipeline (`.github/workflows/release.yml`) selects its upload target two different ways:

- The **CLI binaries** job uploads to the git tag (`github.ref_name`).
- The **desktop installers** are uploaded by `electron-builder --publish always`, which targets the GitHub release matching `app/package.json` version, NOT the tag.

If the version and tag disagree, the two halves publish to different releases: the CLI binaries land on the tag while the `.dmg/.pkg/.zip/.exe/.deb` and `latest*.yml` auto-update files land on the release matching the stale version. This is exactly what happened with `v0.0.12` (package.json was still `0.0.11`).

## Cutting a release

1. Bump `"version"` in `app/package.json` to the new version.
2. Merge to `main`.
3. Tag and push: `git tag vX.Y.Z && git push origin vX.Y.Z` (the tag must match the bumped version).

The workflow then publishes the installers, the standalone CLI binaries, and the `ghcr.io/tinfoilsh/tinfoil-proxy` image to that single release.
