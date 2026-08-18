# Flexprice CLI

Usage-based billing from your terminal — send events, inspect how they metered,
and drive the Flexprice API without leaving the command line.

    brew install flexprice/tap/flexprice
    flexprice init

## Install

**Homebrew (macOS, Linux)**

    brew install flexprice/tap/flexprice

**Install script (macOS, Linux)**

    curl -fsSL https://flexprice.io/install.sh | sh

**Go**

    go install github.com/flexprice/cli@latest

## Quickstart

`flexprice init` walks you through picking a data region and pasting an API
key, verifies it against the API, and stores it in your OS keychain (or an
encrypted file when no keychain is available, e.g. in a container or CI).

    $ flexprice init
    Setting up the Flexprice CLI.
    Your API key is scoped to one environment — you can add more later with `flexprice login`.

    Verified — stored as profile "default" in encrypted file (~/.flexprice/keys)
    Note: the API does not report which environment a key belongs to, so label your
    profiles yourself (--profile-name, --label) and check with: flexprice whoami

    Next steps:
      flexprice whoami            confirm what you are pointed at
      flexprice resources         see everything you can act on
      flexprice customers list    try a read
      flexprice env list          see your other environments

Confirm what you're pointed at:

    $ flexprice whoami
    Profile:      default
    Label:
    Region:       us
    Base URL:     https://us.api.flexprice.io/v1
    Key backend:  encrypted file (~/.flexprice/keys)
    Key:          sk_test_…4b

See everything you can act on:

    $ flexprice resources
    addons                       create, delete, list, lookup, retrieve, update
    alert-settings               create, delete, list, retrieve, update
    alerts                       list
    checkout                     create, delete, retrieve
    costs                        active, analytics, analytics-v2, create, delete, list, retrieve, update
    coupon-associations          list, retrieve
    coupons                      create, delete, list, lookup, retrieve, update
    credit-grants                create, delete, for-addon, for-plan, retrieve, update
    credit-notes                 create, finalize, retrieve, void
    customers                    by-external-id, create, delete, entitlements, entitlements-by-external-id, list, retrieve, subscriptions, upcoming-grants, update, usage
    ... and 24 more

## What you can do

Every resource in the API is a top-level command, grouped by action:

    flexprice customers list
    flexprice customers retrieve cust_01K...
    flexprice customers create --external_id=acme --email=billing@acme.com
    flexprice invoices finalize inv_01K...
    flexprice subscriptions cancel sub_01K...

For a request body too deep to express as flags — creating a subscription, for
example — open it in your editor with the required fields pre-filled:

    flexprice subscriptions create --edit

or supply it directly:

    flexprice subscriptions create --data @subscription.json

`subscriptions create --help` shows exactly which fields flags can reach and
which cannot:

    Fields you can set with flags:
      --billing_period  (required)  [string]
      --currency  (required)  [string]
      --plan_id  (required)  [string]
      ... (23 more optional fields)

    Nested fields — these cannot be set with flags:
      addons  [array]
      line_items  [array]
      phases  [array]
      ... (10 more)

    Use --edit to fill in a pre-built request body, or --data @file.json.

Anything not covered by a named command is reachable through the raw escape
hatch:

    flexprice get /customers/cust_01K...
    flexprice post /events --data @event.json

## Authentication

An API key belongs to exactly one environment — there is no `--environment`
flag, because the key itself already determines it. Switching environments
means switching profiles:

    flexprice login --label "production"    # stores a second profile
    flexprice config list                    # see every stored profile
    flexprice -p production customers list   # use one for a single command

`flexprice env list` shows every environment in your tenant, but the CLI
cannot tell you which one your active key belongs to — the API itself does not
expose that. See [ADR 0003](decisions/0003-environment-scoped-profiles-no-live-flag.md)
if you're curious why.

## Output & scripting

    flexprice customers list --output json > customers.json

Data always goes to stdout; progress messages, warnings, and footers always go
to stderr — redirecting stdout never mixes the two. Exit codes are stable and
safe to script against:

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Generic failure |
| 2 | Usage error |
| 3 | Authentication failure |
| 4 | Not found |
| 5 | Rate limited |

## Configuration

Non-secret settings live in `~/.flexprice/config.toml`; API keys live in your
OS keychain (or an encrypted file fallback where no keychain is available).
`FLEXPRICE_API_KEY` and `--api-key` override the stored key for a single
invocation or for CI.

## Full command reference

    flexprice <resource> --help

lists every action for a resource; a generated reference for every command is
published at https://docs.flexprice.io/cli.

## Contributing

**Source of truth is `flexprice/flexprice` at `cli/`.** This repository is a
release mirror — please open pull requests against the monorepo. Issues here
are welcome. See [ARCHITECTURE.md](ARCHITECTURE.md) for how the code is put
together.

## License

Apache-2.0. See [LICENSE](LICENSE).
