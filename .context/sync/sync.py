#!/usr/bin/env python3
"""
Context Sync — tool-agnostic provenance engine for AGENTS.md files.

Usage:
  python3 .context/sync/sync.py --check         # Exit 1 if any node is stale
  python3 .context/sync/sync.py --sync          # Print prompt bundles for stale nodes
  python3 .context/sync/sync.py --sync --file F # Sync specific AGENTS.md only
  python3 .context/sync/sync.py --advance --file F  # Advance synced_sha after LLM update

No external dependencies. Requires: git, Python 3.8+, PyYAML.
Install PyYAML: pip install pyyaml
"""

import argparse
import fnmatch
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path

try:
    import yaml
except ImportError:
    print("ERROR: PyYAML required. Run: pip install pyyaml", file=sys.stderr)
    sys.exit(2)

MANIFEST_PATH = Path(".context/manifest.yaml")
REPO_ROOT = Path(subprocess.check_output(
    ["git", "rev-parse", "--show-toplevel"], text=True
).strip())


def run_git(*args) -> str:
    result = subprocess.run(
        ["git"] + list(args),
        capture_output=True, text=True, cwd=REPO_ROOT
    )
    if result.returncode != 0:
        print(f"git error: {result.stderr}", file=sys.stderr)
        sys.exit(1)
    return result.stdout.strip()


def head_sha() -> str:
    return run_git("rev-parse", "HEAD")


def changed_files(from_sha: str, to_sha: str = "HEAD") -> list[str]:
    """Files changed between from_sha and to_sha."""
    if from_sha == to_sha:
        return []
    output = run_git("diff", "--name-only", f"{from_sha}..{to_sha}")
    return [f for f in output.splitlines() if f]


def matches_globs(filepath: str, globs: list[str]) -> bool:
    """Check if filepath matches any of the given glob patterns."""
    for pattern in globs:
        if fnmatch.fnmatch(filepath, pattern):
            return True
        # Also match as path prefix (e.g. "internal/service/**" matches "internal/service/foo.go")
        clean = pattern.rstrip("*").rstrip("/")
        if filepath.startswith(clean + "/") or filepath == clean:
            return True
    return False


def scoped_diff(from_sha: str, owns: list[str]) -> str:
    """Get diff for owned paths only."""
    paths = []
    for glob in owns:
        # Convert glob to a git pathspec
        clean = glob.rstrip("*").rstrip("/")
        if clean:
            paths.append(clean)
    if not paths:
        return ""
    args = ["diff", from_sha, "HEAD", "--"] + paths
    return run_git(*args)


def load_manifest() -> dict:
    manifest_file = REPO_ROOT / MANIFEST_PATH
    if not manifest_file.exists():
        print(f"ERROR: {MANIFEST_PATH} not found. Run from repo root.", file=sys.stderr)
        sys.exit(1)
    with open(manifest_file) as f:
        return yaml.safe_load(f)


def save_manifest(data: dict):
    manifest_file = REPO_ROOT / MANIFEST_PATH
    with open(manifest_file, "w") as f:
        yaml.dump(data, f, default_flow_style=False, sort_keys=False)


def find_node(manifest: dict, file_path: str) -> dict | None:
    for node in manifest.get("nodes", []):
        if node["file"] == file_path:
            return node
    return None


def print_prompt_bundle(node: dict, diff: str):
    agents_file = REPO_ROOT / node["file"]
    current_content = agents_file.read_text() if agents_file.exists() else "(file not found)"
    print(f"\n{'='*60}")
    print(f"=== SYNC NEEDED: {node['file']} ===")
    print(f"{'='*60}")
    print("\n--- CURRENT CONTEXT ---")
    print(current_content)
    print(f"\n--- SCOPED DIFF (owns: {node['owns']}) ---")
    print(diff if diff else "(no diff output — check git history)")
    print("""
--- INSTRUCTIONS FOR THE LLM ---
Update the AGENTS.md above minimally to reflect the changes in the diff.
Rules:
  - Update "Key files" table if files were added, removed, or renamed.
  - Update "Patterns" or "Common pitfalls" if the diff reveals new patterns or removes old ones.
  - Do NOT rewrite sections that have not changed.
  - Do NOT add improvement suggestions or TODOs — those go in .context/findings/.
  - Bump synced_sha to HEAD SHA and synced_at to current ISO 8601 UTC timestamp in the frontmatter.
Output: the COMPLETE updated AGENTS.md content, nothing else.
After the LLM produces the file, run:
  python3 .context/sync/sync.py --advance --file """ + node['file'] + "\n")


def cmd_check(manifest: dict, target_file: str | None = None) -> int:
    """Return 1 if any node is stale, 0 if all current."""
    current_head = head_sha()
    stale = []
    for node in manifest.get("nodes", []):
        if target_file and node["file"] != target_file:
            continue
        node_sha = node.get("synced_sha", "")
        if node_sha == current_head:
            continue
        files = changed_files(node_sha)
        owned_changes = [f for f in files if matches_globs(f, node.get("owns", []))]
        if owned_changes:
            stale.append((node["file"], owned_changes))

    if stale:
        print("STALE context nodes detected:", file=sys.stderr)
        for path, changes in stale:
            print(f"  {path}: {len(changes)} owned file(s) changed", file=sys.stderr)
            for c in changes[:5]:
                print(f"    - {c}", file=sys.stderr)
        return 1

    print("All context nodes are current.")
    return 0


def cmd_sync(manifest: dict, target_file: str | None = None):
    """Print prompt bundles for stale nodes; advance SHA for already-current nodes."""
    current_head = head_sha()
    any_stale = False

    for node in manifest.get("nodes", []):
        if target_file and node["file"] != target_file:
            continue

        node_sha = node.get("synced_sha", "")
        if node_sha == current_head:
            print(f"[OK] {node['file']} — already at HEAD")
            continue

        files = changed_files(node_sha)
        owned_changes = [f for f in files if matches_globs(f, node.get("owns", []))]

        if not owned_changes:
            # Advance SHA without LLM call
            node["synced_sha"] = current_head
            node["synced_at"] = datetime.now(timezone.utc).isoformat()
            print(f"[ADVANCE] {node['file']} — no owned changes, SHA bumped to HEAD")
            continue

        any_stale = True
        diff = scoped_diff(node_sha, node.get("owns", []))
        print_prompt_bundle(node, diff)

    save_manifest(manifest)

    if any_stale:
        print("\nNext steps:")
        print("  1. Feed each prompt bundle to your LLM (Claude/Cursor/Codex).")
        print("  2. Save the LLM output as the new AGENTS.md.")
        print("  3. Run: python3 .context/sync/sync.py --advance --file <path>")
        print("  4. Review and merge the context update as a PR.")


def cmd_advance(manifest: dict, target_file: str):
    """After LLM has updated the file, advance synced_sha to HEAD."""
    current_head = head_sha()
    node = find_node(manifest, target_file)
    if not node:
        print(f"ERROR: {target_file} not found in manifest.", file=sys.stderr)
        sys.exit(1)

    node["synced_sha"] = current_head
    node["synced_at"] = datetime.now(timezone.utc).isoformat()
    save_manifest(manifest)
    print(f"[ADVANCED] {target_file} → synced_sha = {current_head}")


def main():
    parser = argparse.ArgumentParser(description="Context sync for AGENTS.md files")
    group = parser.add_mutually_exclusive_group(required=True)
    group.add_argument("--check", action="store_true", help="Exit 1 if any node is stale")
    group.add_argument("--sync", action="store_true", help="Print prompt bundles for stale nodes")
    group.add_argument("--advance", action="store_true", help="Advance synced_sha after LLM update")
    parser.add_argument("--file", help="Target a specific AGENTS.md (relative to repo root)")
    args = parser.parse_args()

    manifest = load_manifest()

    if args.check:
        sys.exit(cmd_check(manifest, args.file))
    elif args.sync:
        cmd_sync(manifest, args.file)
    elif args.advance:
        if not args.file:
            print("ERROR: --advance requires --file", file=sys.stderr)
            sys.exit(1)
        cmd_advance(manifest, args.file)


if __name__ == "__main__":
    main()
