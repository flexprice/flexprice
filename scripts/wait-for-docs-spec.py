#!/usr/bin/env python3
"""Wait until Speakeasy's hosted docs spec reflects this release's OpenAPI spec.

The hosted spec is the base spec plus x-codeSamples from each SDK, rebuilt after
`speakeasy run` uploads. Mintlify only re-reads it on deploy, so redeploy the
docs after this passes.

Usage: scripts/wait-for-docs-spec.py docs/swagger/swagger-3-0.json [--url URL] [--timeout SECONDS]
"""

import argparse
import json
import os
import sys
import time
import urllib.request

DEFAULT_URL = "https://spec.speakeasy.com/flexprice/prod/swagger-json-with-code-samples"
SAMPLE_LANGS = {"go", "python", "typescript"}
HTTP_METHODS = {"get", "put", "post", "delete", "patch", "head", "options", "trace"}
# Added on top of the base spec by the SDK overlay and the code samples.
ADDED_KEYS = {"x-codeSamples", "x-speakeasy-name-override"}


def strip_added(node):
    if isinstance(node, dict):
        return {k: strip_added(v) for k, v in node.items() if k not in ADDED_KEYS}
    if isinstance(node, list):
        return [strip_added(v) for v in node]
    return node


def operations_missing_samples(spec):
    missing = []
    for path, item in spec.get("paths", {}).items():
        for method, op in item.items():
            if method not in HTTP_METHODS or op.get("deprecated"):
                continue
            langs = {sample.get("lang") for sample in op.get("x-codeSamples", [])}
            if not SAMPLE_LANGS <= langs:
                missing.append(f"{method.upper()} {path}")
    return missing


def sample_counts(spec):
    counts = dict.fromkeys(sorted(SAMPLE_LANGS), 0)
    for item in spec.get("paths", {}).values():
        for method, op in item.items():
            if method not in HTTP_METHODS:
                continue
            for lang in {sample.get("lang") for sample in op.get("x-codeSamples", [])} & SAMPLE_LANGS:
                counts[lang] += 1
    return counts


def check(url, expected):
    """Returns (why the hosted spec is stale or None, operations with samples per language)."""
    with urllib.request.urlopen(url, timeout=60) as resp:
        hosted = json.load(resp)
    if strip_added(hosted) != expected:
        return "base spec does not match this release yet", None
    missing = operations_missing_samples(hosted)
    if missing:
        return f"{len(missing)} operations have no code samples yet (e.g. {missing[0]})", None
    return None, sample_counts(hosted)


def write_step_summary(message):
    path = os.environ.get("GITHUB_STEP_SUMMARY")
    if path:
        with open(path, "a") as f:
            f.write(f"### Hosted docs spec\n\n{message}\n")


def main():
    parser = argparse.ArgumentParser(description="Wait for the hosted docs spec to match a local OpenAPI spec.")
    parser.add_argument("spec", help="local OpenAPI spec that this release uploaded")
    parser.add_argument("--url", default=DEFAULT_URL, help="hosted spec URL (default: %(default)s)")
    parser.add_argument("--timeout", type=int, default=300, help="seconds to wait (default: %(default)s)")
    parser.add_argument("--interval", type=int, default=30, help="seconds between checks (default: %(default)s)")
    args = parser.parse_args()

    with open(args.spec) as f:
        expected = strip_added(json.load(f))

    deadline = time.monotonic() + args.timeout
    while True:
        try:
            reason, counts = check(args.url, expected)
        except Exception as err:  # transient fetch or parse errors: keep polling until the deadline
            reason, counts = f"fetch failed: {err}", None
        if reason is None:
            samples = ", ".join(f"{lang} {n}" for lang, n in counts.items())
            message = f"Hosted docs spec matches {args.spec}. Operations with code samples: {samples}."
            print(message, flush=True)
            write_step_summary(message)
            return 0
        if time.monotonic() >= deadline:
            print(f"::error::Hosted docs spec is still stale after {args.timeout}s: {reason}", flush=True)
            return 1
        print(f"Waiting for hosted docs spec: {reason}", flush=True)
        time.sleep(args.interval)


if __name__ == "__main__":
    sys.exit(main())
