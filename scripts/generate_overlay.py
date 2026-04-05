#!/usr/bin/env python3
"""
generate_overlay.py — Generates .speakeasy/overlays/flexprice-sdk.yaml

Reads docs/swagger/swagger-3-0.json and produces a Speakeasy overlay that:
  - Strips dto. prefix from all 218 dto.* schemas
  - Renames top-level entity response types (e.g. CustomerResponse → Customer)
  - Renames errors.* schemas to clean names
  - Renames the 'error' property on ErrorResponse → 'detail' (avoids Error_ in Go)
  - Patches 177 timestamp fields with format: date-time
"""

import json
import re
import sys
from pathlib import Path

SPEC_PATH = Path("docs/swagger/swagger-3-0.json")
OVERLAY_PATH = Path(".speakeasy/overlays/flexprice-sdk.yaml")

# Top-level entity response types — strip Response suffix for cleaner SDK names
ENTITY_RENAMES = {
    "dto.CustomerResponse":     "Customer",
    "dto.SubscriptionResponse": "Subscription",
    "dto.InvoiceResponse":      "Invoice",
    "dto.PlanResponse":         "Plan",
    "dto.PriceResponse":        "Price",
    "dto.AddonResponse":        "Addon",
    "dto.WalletResponse":       "Wallet",
    "dto.PaymentResponse":      "Payment",
    "dto.CouponResponse":       "Coupon",
    "dto.FeatureResponse":      "Feature",
}

# Timestamp-like field name patterns — any string field matching these gets format: date-time
TIMESTAMP_PATTERNS = re.compile(
    r"(_at|_date|_start|_end|_time|_anchor|_period|expires_at|expiry|"
    r"due_date|close_time|archived_at|applied_at|executed_at|failed_at|"
    r"finalized_at|completed_at|last_used_at|balance_updated_at)$"
)


def quote(s: str) -> str:
    """Wrap a schema name in double-quoted JSONPath bracket notation (RFC 9535)."""
    return f'["{s}"]'


def build_actions(spec: dict) -> list:
    actions = []
    schemas = spec.get("components", {}).get("schemas", {})

    # ── 1. Strip dto. prefix & apply entity renames ─────────────────────────
    dto_schemas = [n for n in schemas if n.startswith("dto.")]
    if dto_schemas:
        for name in dto_schemas:
            if name in ENTITY_RENAMES:
                override = ENTITY_RENAMES[name]
            else:
                override = name[4:]  # strip "dto."
            actions.append({
                "target": f"$.components.schemas{quote(name)}",
                "update": {"x-speakeasy-name-override": override},
            })
    else:
        # Schema names are already clean — add entity renames directly
        for clean_name, override in ENTITY_RENAMES.items():
            schema_name = clean_name[4:]  # strip "dto." to get "CustomerResponse"
            if schema_name in schemas:
                actions.append({
                    "target": f"$.components.schemas{quote(schema_name)}",
                    "update": {"x-speakeasy-name-override": override},
                })

    # ── 2. Rename errors.* schemas ──────────────────────────────────────────
    for name in schemas:
        if not name.startswith("errors."):
            continue
        override = name[7:]  # strip "errors."
        actions.append({
            "target": f"$.components.schemas{quote(name)}",
            "update": {"x-speakeasy-name-override": override},
        })

    # ── 3. Rename 'error' property on ErrorResponse → 'detail' ─────────────
    #    The JSON field name 'error' is a Go reserved keyword; Speakeasy
    #    escapes it to Error_ which is confusing. Renaming to 'detail' in
    #    the SDK (via x-speakeasy-name-override on the property) fixes this.
    if "errors.ErrorResponse" in schemas:
        err_props = schemas["errors.ErrorResponse"].get("properties", {})
        if "error" in err_props:
            actions.append({
                "target": (
                    f"$.components.schemas{quote('errors.ErrorResponse')}"
                    f".properties{quote('error')}"
                ),
                "update": {"x-speakeasy-name-override": "detail"},
            })

    # ── 4. Patch timestamp fields with format: date-time ────────────────────
    def _get_properties(schema: dict) -> dict:
        """Return all properties from a schema, including those nested in allOf/anyOf/oneOf."""
        props = dict(schema.get("properties", {}))
        for combiner in ("allOf", "anyOf", "oneOf"):
            for sub in schema.get(combiner, []):
                props.update(sub.get("properties", {}))
        return props

    for schema_name, schema in schemas.items():
        for prop_name, prop in _get_properties(schema).items():
            if (
                prop.get("type") == "string"
                and prop.get("format") != "date-time"
                and TIMESTAMP_PATTERNS.search(prop_name)
            ):
                actions.append({
                    "target": (
                        f"$.components.schemas{quote(schema_name)}"
                        f".properties{quote(prop_name)}"
                    ),
                    "update": {"format": "date-time"},
                })

    return actions


def write_overlay(actions: list) -> None:
    # Write as YAML manually to avoid requiring PyYAML and to control formatting
    lines = [
        "overlay: 1.0.0",
        "info:",
        "  title: Flexprice SDK and MCP customizations",
        "  version: 2.1.0",
        "actions:",
    ]
    for action in actions:
        target = action["target"]
        update = action["update"]
        lines.append(f"  - target: '{target}'")
        lines.append("    update:")
        for k, v in update.items():
            lines.append(f"      {k}: {json.dumps(v)}")
    OVERLAY_PATH.write_text("\n".join(lines) + "\n", encoding="utf-8")


def main() -> None:
    if not SPEC_PATH.exists():
        print(f"ERROR: {SPEC_PATH} not found. Run from repo root.", file=sys.stderr)
        sys.exit(1)

    spec = json.loads(SPEC_PATH.read_text(encoding="utf-8"))
    actions = build_actions(spec)
    write_overlay(actions)

    print(f"Written {len(actions)} overlay actions to {OVERLAY_PATH}")

    # Print a summary breakdown
    name_overrides = sum(1 for a in actions if "x-speakeasy-name-override" in a["update"])
    ts_patches = sum(1 for a in actions if a["update"].get("format") == "date-time")
    print(f"  Name overrides:   {name_overrides}")
    print(f"  Timestamp patches: {ts_patches}")


if __name__ == "__main__":
    main()
