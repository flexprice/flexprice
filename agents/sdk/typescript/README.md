# @flexprice/agent-sdk

Meters calls to LLM providers and sends one usage event per call to FlexPrice, so each customer's model usage can be tracked and billed. You wrap the LLM client you already use, and your app's code stays the same.

**Status:** in development. Not released yet. The Anthropic client is supported so far.

## Requirements

- Node 22 or newer.
- The package is ESM only. A CommonJS app can `require` it on Node 22.12 or newer. Before that, load it with `await import("@flexprice/agent-sdk")`.
- A TypeScript project that compiles to CommonJS needs `module` set to `nodenext`. With `node16` or `node18`, TypeScript refuses to `require` an ESM-only package.
- For Anthropic: `@anthropic-ai/sdk` 0.101.0 or newer. The SDK hooks in through the client's middleware, which older versions do not have.

## Quick start with Anthropic

```ts
import Anthropic from "@anthropic-ai/sdk";
import { createAgentMeter } from "@flexprice/agent-sdk";

// One meter per app.
const meter = createAgentMeter({
  apiKey: process.env.FLEXPRICE_API_KEY,
  baseUrl: "https://us.api.flexprice.io/v1", // India: https://api.cloud.flexprice.io/v1
});

// The same client, now metered.
const claude = meter.wrap(new Anthropic());

// Every call that starts inside the block is billed to this customer.
const message = await meter.withLabels({ customerId: "cust_42" }, () =>
  claude.messages.create({
    model: "claude-sonnet-5-5",
    max_tokens: 1024,
    messages: [{ role: "user", content: "Hello" }],
  }),
);
```

Events are sent in batches in the background. In a serverless function, or before the process exits, call `await meter.flush()` so that nothing still waiting is lost.

## What is metered

Every successful call that creates a message: `messages.create`, streamed or not, `messages.stream()`, `beta.messages.create`, and each call the tool runner makes. A call that fails is not metered, and a call the client retries counts once.

A stream is metered when it ends, with the tokens that had arrived by then. That includes a stream the app stops reading early, marked `cut_short`, and one whose connection breaks, marked `error`.

## Who is billed

Each call needs a customer. The SDK looks for one in this order:

1. The innermost `meter.withLabels({ customerId })` block the call started in.
2. The client's labels: `meter.wrap(client, { customerId })`.
3. The meter's defaults: `createAgentMeter({ defaults: { customerId } })`.

A call with no customer is not metered, and is reported as a problem. Labels can also carry an `agentId`, sent as `agent_id`, and `tags`, each sent as a property of its own.

## The event

One `model.usage` event per call. Its properties include:

| Property | What it holds |
|---|---|
| `input_tokens`, `output_tokens` | Input and output tokens |
| `cache_read_tokens` | Input tokens read from the prompt cache |
| `cache_write_tokens`, `cache_write_1h_tokens` | Input tokens written to the prompt cache. Writes kept for 1 hour are counted apart, since they cost more |
| `provider`, `model`, `streamed` | Where the call went, and whether it was streamed |
| `outcome` | How it ended: `completed`, `max_tokens`, `refused`, `cut_short` or `error` |
| `raw_*` | The provider's usage block, as it arrived |

The counts never overlap: each token is in exactly one of them, so a meter can add any of them together. Every value is text, as FlexPrice requires.

## Problems

A metering problem never fails your app's call, and the SDK never throws into it. Pass `onError` to see each problem. Without it, the first problem of each kind is logged.

```ts
const meter = createAgentMeter({
  apiKey: process.env.FLEXPRICE_API_KEY,
  baseUrl: "https://us.api.flexprice.io/v1",
  onError: (error, where) => console.error(`[metering, ${where}] ${error.message}`),
});
```

The one exception is setup. `createAgentMeter` throws a `TypeError` for a bad option, such as a missing API key, so the mistake shows up when the app starts.

## Trying it with real keys

[`examples/anthropic.mjs`](examples/anthropic.mjs) makes two real calls through a metered client, one streamed, and sends their events to FlexPrice. The top of the file says which keys to put in `.env`, which git ignores. Use a FlexPrice sandbox environment. Pass `debug: true` to `createAgentMeter` to log each event as it is built.

## Working on it

Run these from `agents/sdk/typescript`:

| Command | What it does |
|---|---|
| `npm install` | Installs the development tools |
| `npm test` | Runs the tests |
| `npm run typecheck` | Checks types in the source and the tests |
| `npm run lint` | Lints the source and the tests |
| `npm run build` | Compiles `src/` into `dist/` |
