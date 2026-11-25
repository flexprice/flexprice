---
name: activity-log-entity
description: Use when a new entity should appear in the activity log, when a registered entity gains a field or a state change, or when a write to a registered entity is a bulk update or delete. Covers registration, snapshot safety, declared actions and the returning write forms.
---

# Onboarding an entity to the activity log

An ent hook on the writer client records every write to a registered entity (`internal/activity/entities.go`) and publishes it to ClickHouse after commit.

## Registering

1. **Entity type.** Add `SystemEntityType<Name>` in `internal/types/entity.go` if missing.
2. **Converter.** Expose `FromEnt(*ent.<Name>) *<Name>` in `internal/domain/<name>/model.go`.
3. **Tags.** On the domain struct: `activity:"redact"` for secrets and gateway data (tokens, provider payloads, payment details), stored as `"[redacted]"`; `activity:"drop"` for bookkeeping (`version`, sequences) and personal data (names, emails, phones, addresses, free-form metadata). Every untagged field is stored in the snapshot.
4. **Registration.** Add a `Registration` in `internal/activity/entities.go`: `EntityType`, `EntName` (the ent type name), `Snapshot: snapshotOf(<name>.FromEnt)`, `Label` (snapshot keys that name the entity, first non-empty wins), `Actions`.
5. **Declared actions** (optional). Add `<entity>.<past-tense verb>` constants in `internal/activity/actions.go`, list them in `Actions` highest priority first, and declare before the write: `ctx = activity.WithAction(ctx, id, activity.<Name>Voided)`. Undeclared writes log as `.created`, `.updated` or `.deleted`.
6. **Customer.** An entry takes `customer_id` from the snapshot. If the entity has none, add `GetLinkedCustomerID(ctx, id)` to its repository (reading rows in any status) and register it in `NewLinkedCustomers` (`internal/activity/linked_customer.go`).
7. **Writes.** The hook only sees rows: `Create…Save`, `UpdateOne…Save`, `CreateBulk…Save`, and for bulk `Update()…SaveReturning(ctx)` / `Delete()…ExecReturning(ctx)`. Convert any count-only `Save`/`Exec` on the entity. Lint CL004 enforces this, and `tools/lint/codelint/entwrites.go` lists the tracked entities; `TestTrackedEntities` fails until you add the new one there too.
8. **Tests.** Add the entity to `entColumns` and `bareRow` in `internal/activity/registry_test.go`; the registry tests skip entities missing there.

## Actors

`types.Actor` holds a type and an id, never a name or email; the read API resolves labels from Postgres. A new system actor whose id is not readable gets its label in `fixedActorLabel` (`internal/ee/service/activity.go`).
