# Flexprice + Next.js Cost Dashboard (JavaScript SDK)

A minimal, production-grade Next.js example showing how to use the Flexprice JavaScript SDK to:

- Create a customer (idempotent by external id)
- Ingest usage events to Flexprice
- Aggregate events to render a simple cost dashboard

## Getting started

1. Install dependencies

```bash
npm i
# or
pnpm i
# or
yarn
```

2. Configure environment variables

```bash
cp .env.example .env
# Edit .env
```

Required:
- `FLEXPRICE_API_KEY` — your server key (starts with `sk_`)
- Optional: `FLEXPRICE_API_HOST` (defaults to `api.cloud.flexprice.io`)
- Optional: `UNIT_COST` — fallback per-unit cost if events don't include `properties.cost`

3. Run the app

```bash
npm run dev
```

Open http://localhost:3000 and:
- Set an `External Customer ID`
- Click “Ingest sample usage” to send an event to Flexprice
- See total cost, event count, units, and recent events update

## Project structure

```
examples/with-nextjs/
├─ app/
│  ├─ api/
│  │  └─ usage/
│  │     ├─ ingest/route.ts    # POST: create event via @flexprice/sdk
│  │     └─ summary/route.ts   # GET: read events and compute totals
│  ├─ globals.css
│  ├─ layout.tsx
│  └─ page.tsx
├─ components/
│  ├─ CostCard.tsx
│  └─ EventsTable.tsx
├─ lib/
│  └─ flexprice.ts             # SDK init + small promise wrappers
├─ .env.example
├─ next.config.mjs
├─ package.json
└─ tsconfig.json
```

## Notes
- The SDK uses callbacks; this example wraps them to work nicely with async/await in route handlers.
- Always use snake_case for request bodies and params sent to Flexprice.
- Never expose `FLEXPRICE_API_KEY` to the browser. All SDK calls happen in server route handlers.

## Links
- Flexprice repo: https://github.com/flexprice/flexprice
- JavaScript SDK (npm): https://www.npmjs.com/package/@flexprice/sdk
- Docs: https://docs.flexprice.io/