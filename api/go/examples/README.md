# Go SDK examples

Uses the published module **[github.com/flexprice/go-sdk/v2](https://github.com/flexprice/go-sdk)** (v2.0.15+).

## Run

1. Copy `.env.sample` to `.env` and set `FLEXPRICE_API_KEY`. Optionally set `FLEXPRICE_API_HOST` (must include `/v1`, e.g. `https://us.api.flexprice.io/v1`).
2. From this directory:

```bash
go mod tidy && go run .
```

## Monorepo workflow

Source of truth for this sample is `api/custom/go/examples` (merged here by `make merge-custom`).

**Verified tests:** Integration coverage lives in **api/tests/go** (see [api/tests/README.md](../../tests/README.md)).
