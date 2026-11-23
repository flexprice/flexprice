# @flexprice/agent-sdk

Meters calls to LLM providers and sends one usage event per call to FlexPrice, so each customer's model usage can be tracked and billed. You wrap the LLM client you already use, and your app's code stays the same.

**Status:** in development. Not released yet.

## Requirements

- Node 22 or newer.
- The package is ESM only. A CommonJS app can `require` it on Node 22.12 or newer. Before that, load it with `await import("@flexprice/agent-sdk")`.
- A TypeScript project that compiles to CommonJS needs `module` set to `nodenext`. With `node16` or `node18`, TypeScript refuses to `require` an ESM-only package.

## Working on it

Run these from `agents/sdk/typescript`:

| Command | What it does |
|---|---|
| `npm install` | Installs the development tools |
| `npm test` | Runs the tests |
| `npm run typecheck` | Checks types in the source and the tests |
| `npm run lint` | Lints the source and the tests |
| `npm run build` | Compiles `src/` into `dist/` |
