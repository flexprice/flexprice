# Hosted invoice page — why it is a standalone app

> **TL;DR.** The ERD first served the hosted invoice page from each region's existing frontend, behind an edge router on `invoice.flexprice.io` that forwarded `/<region>/…` to that region. The regional frontends are built to live at the root of their own host, so behind the router their assets and routes did not resolve. We now ship the page as one standalone app on `invoice.flexprice.io`. The region in the link only picks which API the browser calls.

## Rejected: edge router to the regional frontends

```mermaid
flowchart LR
  C([Customer browser]) -->|invoice.flexprice.io/us/token| E[Edge router<br/>invoice.flexprice.io]
  E -->|/us → strip prefix| USF[US frontend<br/>dashboard + invoice page]
  E -->|/in| INF[India frontend]
  E -->|/ca| CAF[Canada frontend]
  C -.->|page data| USB[US backend<br/>/public/v1]
```

### Where it breaks

```mermaid
sequenceDiagram
  participant B as Browser (address bar: invoice.flexprice.io/us/token)
  participant E as Edge router
  participant US as US frontend (built for its own host root)

  B->>E: GET /us/token
  E->>US: GET /token (prefix stripped)
  US-->>B: index.html → <script src="/assets/index-abc.js">
  Note over B: "/assets/…" resolves against invoice.flexprice.io, not the US frontend
  B->>E: GET /assets/index-abc.js (no /us/ prefix)
  E-->>B: 404, the router cannot tell which region it belongs to
  Note over B,US: Even with assets loaded, the app router sees /us/token,<br/>while its routes expect /invoice/:token, so no route matches
```

Every workaround patched the same mismatch between the address bar host and the host the frontend was built for:

| Workaround | Why we dropped it |
|---|---|
| Asset base set to the full regional URL | The page loads its code from another host, and each build is tied to one region's host |
| Asset base `/us/` plus a router rewrite | A separate frontend build per region for one page, and that build no longer works on its own host |
| Relative asset base (`./`) | Breaks the dashboard's deep links: `/customers/123` asks for `/customers/assets/…` |
| Extra `/<region>/:token` routes in the frontend | Region names leak into the dashboard's routes, and the asset problem stays |

## Chosen: one standalone app on the invoice domain

```mermaid
flowchart LR
  C([Customer browser]) -->|invoice.flexprice.io/us/token<br/>page and assets, same host| P[Hosted invoice page<br/>standalone app]
  C -.->|/us links| USB[US backend<br/>/public/v1]
  C -.->|/in links| INB[India backend<br/>/public/v1]
  C -.->|/ca links| CAB[Canada backend<br/>/public/v1]
```

```mermaid
sequenceDiagram
  participant B as Browser (invoice.flexprice.io/us/token)
  participant P as Hosted invoice page
  participant US as US backend

  B->>P: GET /us/token
  P-->>B: index.html + /assets/* from the same host
  Note over B: Route /<region>/<token> → region "us" → API origin from the build-time map
  B->>US: GET /public/v1/invoices, X-Session-Token: token
  US-->>B: Invoice, payments, pay.enabled
```

| | Edge router (rejected) | Standalone app (chosen) |
|---|---|---|
| Frontend deployments touched | One per region | One new app |
| Region handling | Path prefix the frontends had to tolerate | Data in the path: picks the API origin |
| Edge routing | A router on `invoice.flexprice.io` | None, the domain points at the app |
| Dashboard impact | Build and route changes | None; shares the UI components only |
| Assets and routes | Rooted at the regional host, broken behind the router | Rooted at `invoice.flexprice.io`, resolve as built |
