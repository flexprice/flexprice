@AGENTS.md

## Comment Rules

- Keep comments to one or two lines unless the extra detail is essential.
- Explain intent, invariants, or non-obvious constraints; never narrate obvious code.
- Do not include implementation history, redundant parameter descriptions, or speculative context.

## Testing Rules

- Tests are required only for service-layer business logic.
- Do not add tests solely for types, DTOs, models, or other trivial plumbing unless explicitly requested.

## Logging Rules

- Log only meaningful state changes, failures, and operational decisions.
- Include the relevant IDs and error; omit redundant inputs, duplicate messages, and routine success logs.
