#!/usr/bin/env python3
"""
Merges docs/swagger/webhooks.json into docs/swagger/swagger-3-0.json.

Adds:
- webhooks key (top-level) with all webhook event definitions
- webhook-specific schemas into components.schemas

The $ref pointers in webhooks.json use the same schema names as the
main spec (e.g. dto.InvoiceResponse), so they resolve automatically
once merged.
"""

import json
import os
import sys

REPO_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SWAGGER_FILE = os.path.join(REPO_ROOT, "docs", "swagger", "swagger-3-0.json")
WEBHOOKS_FILE = os.path.join(REPO_ROOT, "docs", "swagger", "webhooks.json")


def main():
    if not os.path.exists(SWAGGER_FILE):
        print(f"Error: {SWAGGER_FILE} not found. Run 'make swagger-3-0' first.")
        sys.exit(1)

    if not os.path.exists(WEBHOOKS_FILE):
        print(f"Error: {WEBHOOKS_FILE} not found.")
        sys.exit(1)

    with open(SWAGGER_FILE) as f:
        spec = json.load(f)

    with open(WEBHOOKS_FILE) as f:
        webhooks = json.load(f)

    # Merge webhooks top-level key
    if "webhooks" in webhooks:
        spec["webhooks"] = webhooks["webhooks"]
        print(f"Added {len(webhooks['webhooks'])} webhook events")

    # Merge webhook-specific schemas into components.schemas
    webhook_schemas = webhooks.get("components", {}).get("schemas", {})
    if webhook_schemas:
        if "components" not in spec:
            spec["components"] = {}
        if "schemas" not in spec["components"]:
            spec["components"]["schemas"] = {}

        for name, schema in webhook_schemas.items():
            # Skip schemas that already exist in the main spec (e.g. dto.InvoiceResponse)
            if name.startswith("dto."):
                continue
            spec["components"]["schemas"][name] = schema
            print(f"  Added schema: {name}")

    with open(SWAGGER_FILE, "w") as f:
        json.dump(spec, f, indent=2)
        f.write("\n")

    print(f"Merged webhooks into {SWAGGER_FILE}")


if __name__ == "__main__":
    main()
