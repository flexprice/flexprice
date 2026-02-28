# TypeScript SDK examples

1. Install the SDK: `npm i @flexprice/sdk`.
2. Copy `.env.sample` to `.env` and set `FLEXPRICE_API_KEY` (optionally `FLEXPRICE_API_HOST`; include `/v1`, e.g. `us.api.flexprice.io/v1`).
3. Run the example: from the package root, `npx tsx examples/quick-start.ts`; or from the `examples/` directory, `npx tsx quick-start.ts`.

**Verified tests:** The same API flows are covered and verified by the integration test suite at **api/tests/ts/test_sdk.ts**. Run with `npm test` from `api/tests/ts`; see [api/tests/README.md](../../tests/README.md).
