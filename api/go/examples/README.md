# Go SDK examples

1. From repo root: `make sdk-all` or `make go-sdk` then `make merge-custom`.
2. Copy `.env.sample` to `.env`; set `FLEXPRICE_API_KEY` and optionally `FLEXPRICE_API_HOST` (include `/v1`, e.g. `us.api.flexprice.io/v1`).
3. From `api/go/examples`: `go mod tidy && go run main.go`.

Includes sync event ingest and the custom async client (async.go). Custom files live in `api/custom/go/`.
