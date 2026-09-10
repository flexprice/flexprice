# Enterprise Edition (`ee/`)

This directory holds Flexprice enterprise features, gated behind the `ee` Go
build tag. Every file here starts with `//go:build ee`.

The dependency is one-directional: `ee/` imports core `internal/...`; **core
never imports `ee/`.** The only bridge is the tagged `cmd/server/ee_enabled.go`,
which pulls in `ee.Module()` and triggers the `init()` functions that populate
the core extension registries (temporal contributors, HTTP route registrars,
auth providers). In a community build those registries are empty and nothing in
`ee/` is referenced or compiled.

## Two builds, two images

| | Community (OSS) | Enterprise |
|---|---|---|
| build | `go build ./cmd/server` | `go build -tags ee ./cmd/server ./ee/...` |
| image | `ghcr.io/flexprice/flexprice-oss` | `ghcr.io/flexprice/flexprice` |
| `ee/` code (Phase 1: SAML SSO, usage-alert workflow) | excluded | included |

The `flexprice` image is the enterprise build (feature-parity with the
pre-boundary single image). `flexprice-oss` is the community build.

## Building the OSS image (no `ee/` code)

The OSS image is the **default** build — no build tag, no build-arg:

```bash
# Local binary (no ee/ compiled in)
go build ./cmd/server ./internal/...

# OSS Docker image
docker build -t flexprice-oss:local .
```

`docker build` with no `--build-arg` produces the community binary because the
Dockerfile defaults `ARG BUILD_TAGS=""`. Passing `--build-arg BUILD_TAGS=ee`
produces the enterprise binary instead.

To prove an OSS build carries no `ee/` code, delete `ee/` entirely and it still
compiles — a public clone with no `ee/` directory builds the community server:

```bash
mv ee /tmp/ee-aside
go build ./cmd/server ./internal/...   # exit 0
mv /tmp/ee-aside ee
```

## Scope (as of Phase 1)

`ee/` currently contains **SAML SSO** (`ee/auth/saml`) and the **usage-alert
Temporal workflow** (`ee/alerts`). These are excluded from the OSS build.

> **Note:** `internal/ee/service` is a separate, historically-named directory
> holding the core service layer (billing, invoicing, subscriptions). It is
> **not** behind the `ee` build tag and is present in **both** images. Do not
> confuse `internal/ee/` (core, untagged) with this `ee/` directory (enterprise,
> tagged). Later phases migrate genuinely-commercial pieces of
> `internal/ee/service` across this boundary.

## Make targets

```bash
make build-ee          # go build -tags ee ./cmd/server ./internal/... ./ee/...
make test-ee           # go test  -tags ee ./ee/... ./cmd/server/... ./internal/...
make docker-build-ee   # docker build --build-arg BUILD_TAGS=ee -t flexprice .
```

## License

`ee/` is commercial-licensed enterprise code, not covered by the repository's
AGPL-3.0 core license. Building or running the `ee`-tagged binary requires a
commercial license (see `internal/ee/LICENSE`).

Each published image declares its license via the
`org.opencontainers.image.licenses` OCI label and ships the text in its
filesystem:

| Image | `licenses` label | Files in image |
|---|---|---|
| `flexprice-oss` (community) | `AGPL-3.0-only` | `/app/LICENSE` |
| `flexprice` (enterprise, `-tags ee`) | `LicenseRef-Flexprice-Commercial` | `/app/LICENSE` (AGPL core) + `/app/LICENSE.enterprise` |

The enterprise image is **not** AGPL-only — it contains commercial `ee/` code,
so its label is the commercial identifier, not `AGPL-3.0-only`.
