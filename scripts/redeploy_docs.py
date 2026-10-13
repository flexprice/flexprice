#!/usr/bin/env python3
"""Record a release in the docs repo's version.mdx so Mintlify redeploys the docs.

Mintlify skips commits that change no files, so each release adds a row to version.mdx.
That commit makes Mintlify redeploy, which re-reads the hosted API spec.

Usage: scripts/redeploy_docs.py --repo flexprice/flexprice-docs --tag v2.1.37 --release-url URL
Needs DOCS_REPO_TOKEN with Contents read and write on the docs repo.
"""

import argparse
import base64
import datetime
import functools
import http.client
import json
import os
import re
import sys
import urllib.parse
import urllib.request

API = "https://api.github.com"
VERSION_FILE = "version.mdx"
SEPARATOR = re.compile(r"^\|\s*:?-{3,}:?\s*\|\s*:?-{3,}:?\s*\|\s*$")


def insert_row(text, tag, release_url, when):
    """Returns text with a row for the release directly under the table header, so the newest is first."""
    row = f"| [{tag}]({release_url}) | {when:%Y-%m-%d %H:%M} |\n"
    lines = text.splitlines(keepends=True)
    for i, line in enumerate(lines):
        if SEPARATOR.match(line.rstrip("\r\n")):
            head = "".join(lines[: i + 1])
            if not head.endswith("\n"):
                head += "\n"
            return head + row + "".join(lines[i + 1 :])
    raise ValueError(f"{VERSION_FILE} has no table separator line such as '| --- | --- |'")


def https_opener():
    """Returns an opener that handles only https URLs, so file:// and other schemes are refused."""
    opener = urllib.request.OpenerDirector()
    opener.add_handler(urllib.request.HTTPSHandler())
    opener.add_handler(urllib.request.UnknownHandler())
    return opener


def github(method, path, body=None, *, token=""):
    headers = {"Accept": "application/vnd.github+json", "X-GitHub-Api-Version": "2022-11-28"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    data = None
    if body is not None:
        headers["Content-Type"] = "application/json"
        data = json.dumps(body).encode()
    request = urllib.request.Request(API + path, data=data, headers=headers, method=method)
    try:
        with https_opener().open(request, timeout=60) as resp:
            try:
                raw = resp.read()
            except (OSError, http.client.HTTPException) as err:  # truncated or reset body
                raise RuntimeError(f"{method} {path}: HTTP {resp.status} (body unreadable: {err})") from None
            if not 200 <= resp.status < 300:
                raise RuntimeError(f"{method} {path}: HTTP {resp.status} {raw.decode(errors='replace')[:300]}")
            return json.loads(raw)
    except (OSError, http.client.HTTPException, ValueError) as err:  # dropped connections, non-JSON bodies
        raise RuntimeError(f"{method} {path}: {err}") from None


def record_release(api, repo, branch, tag, release_url, when):
    """Adds the release row to version.mdx on branch and returns the new commit's SHA."""
    path = f"/repos/{repo}/contents/{VERSION_FILE}"
    current = api("GET", f"{path}?ref={urllib.parse.quote(branch)}")
    if not (isinstance(current, dict) and current.get("type") == "file"
            and isinstance(current.get("content"), str) and isinstance(current.get("sha"), str)):
        raise ValueError(f"{VERSION_FILE} on {branch} is not a file")
    text = base64.b64decode(current["content"]).decode()
    updated = insert_row(text, tag, release_url, when)
    result = api("PUT", path, {
        "message": f"chore: update API reference for flexprice {tag}",
        "content": base64.b64encode(updated.encode()).decode(),
        "sha": current["sha"],
        "branch": branch,
    })
    commit = result.get("commit") if isinstance(result, dict) else None
    if not (isinstance(commit, dict) and isinstance(commit.get("sha"), str)):
        raise ValueError(f"writing {VERSION_FILE} returned no commit SHA")
    return commit["sha"]


def main():
    parser = argparse.ArgumentParser(description="Record a release in the docs repo's version.mdx so Mintlify redeploys.")
    parser.add_argument("--repo", required=True, help="docs repository as owner/name")
    parser.add_argument("--branch", default="main", help="docs branch (default: %(default)s)")
    parser.add_argument("--tag", required=True, help="release tag to record, e.g. v2.1.37")
    parser.add_argument("--release-url", required=True, help="link for the release row")
    args = parser.parse_args()

    token = os.environ.get("DOCS_REPO_TOKEN", "")
    if not token:
        parser.error("DOCS_REPO_TOKEN is not set")
    api = functools.partial(github, token=token)
    now = datetime.datetime.now(datetime.timezone.utc)
    try:
        sha = record_release(api, args.repo, args.branch, args.tag, args.release_url, now)
    except (RuntimeError, ValueError) as err:
        reason = " ".join(str(err).split())  # annotations end at the first newline
        print(f"::error::Could not record {args.tag} in {args.repo}@{args.branch}:{VERSION_FILE}: {reason}", flush=True)
        return 1
    print(f"Recorded {args.tag} in {args.repo}@{args.branch}:{VERSION_FILE} as commit {sha}", flush=True)
    return 0


if __name__ == "__main__":
    sys.exit(main())
