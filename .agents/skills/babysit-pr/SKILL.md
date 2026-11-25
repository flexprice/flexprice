---
name: babysit-pr
description: Use when asked to watch, babysit or land a pull request, or to handle review comments from CodeRabbit, Devin, Strix or a maintainer. Verifies each finding against the source, replies in the house format, keeps the PR scoped, and stops when the bots are green.
---

# Babysitting a pull request

Branch protection on `develop` needs one human approval and dismisses stale approvals on every push, so each round costs a reviewer's attention. Make rounds few and final.

## Loop

1. If your harness has `watch_pull_request`, call it and end your turn; it wakes you on checks, comments and conflicts. Never poll or sleep.
2. For every bot or human finding, open the code it points at and decide: **real**, **already handled**, **out of scope**, or **wrong**. Read the whole function, not the diff hunk.
3. Fix the real ones in one commit per round. A fix must not widen the PR: anything that is not this PR's problem becomes a follow-up ticket, listed in the PR body.
4. Re-run `make check-changed`, push, and reply to every thread. Then watch again.
5. Stop when checks and bots are green on the latest commit and every thread has a reply. Say so in one line.

## Reply format

Name the commit and the proof, so the reviewer can check without re-reading the PR:

- `Fixed in 3afe93e: reused afterCommit instead of the ent-level hook; it runs after commit and is dropped on rollback.`
- `Not changing: every persisted invoice goes through ToDraftInvoice, which always sets issue_date, so the nil branch cannot happen.`
- `Follow-up FLE-2470: the same gap exists on develop for Stripe payments; fixing both paths together.`
- `Not applicable: we do not take partial payments, so a paid invoice is settled by one payment.`

Do not thank the bot, restate the finding, or promise work without a ticket.

## What reviewers here ask for most

From recent PR threads, in order: reuse an existing helper or flow instead of a new one; move logic to the package that owns the concern; keep gateway-specific types out of generic services; add the test to the existing suite file instead of a new file; mutate domain entities through builders; regenerate Swagger when a DTO changed; list new webhook events in `cmd/migrate/data.json`; sort a new migration after develop's newest and record its checksum in `migrations/.hashes`. Check your own PR for these before the bots do.

## Never

- Add `//nolint` or `//lint:ignore` without a reason, or to make a bot quiet.
- Force-push over a reviewer's in-progress review.
- Merge. A maintainer merges.
