"""Tests for redeploy_docs.py. Run: python3 scripts/redeploy_docs_test.py"""

import base64
import contextlib
import datetime
import difflib
import http.client
import io
import os
import re
import sys
import unittest
import urllib.error
from unittest import mock

sys.dont_write_bytecode = True  # keeps scripts/ free of __pycache__ when the tests import the script
import redeploy_docs as rd  # noqa: E402

WHEN = datetime.datetime(2026, 10, 16, 18, 22, tzinfo=datetime.timezone.utc)
V36_ROW = "| [v2.1.36](https://github.com/flexprice/flexprice/releases/tag/v2.1.36) | 2026-10-13 06:39 |"
PAGE = f"""---
title: "API reference version"
hidden: true
---

| Release | Docs updated (UTC) |
| --- | --- |
{V36_ROW}
"""
CONTENTS_PATH = "/repos/o/r/contents/version.mdx?ref=main"


class FakeApi:
    """Returns canned responses in order and records each call."""

    def __init__(self, responses):
        self.responses = list(responses)
        self.calls = []

    def __call__(self, method, path, body=None):
        self.calls.append((method, path, body))
        response = self.responses.pop(0)
        if isinstance(response, Exception):
            raise response
        return response


def response(body):
    """A urlopen() result that yields body."""
    return contextlib.nullcontext(io.BytesIO(body))


class BrokenBody(io.BytesIO):
    """An HTTP error body whose read fails part-way, like a truncated 502."""

    def __init__(self, failure):
        super().__init__()
        self.failure = failure

    def read(self, *_args):
        raise self.failure


def bad_gateway(failure):
    return urllib.error.HTTPError("https://api.github.com" + CONTENTS_PATH, 502, "Bad Gateway", {}, BrokenBody(failure))


class InsertRowTest(unittest.TestCase):
    def test_newest_release_goes_first(self):
        lines = rd.insert_row(PAGE, "v2.1.37", "https://x/v2.1.37", WHEN).splitlines()
        sep = lines.index("| --- | --- |")
        self.assertEqual(lines[sep + 1], "| [v2.1.37](https://x/v2.1.37) | 2026-10-16 18:22 |")
        self.assertEqual(lines[sep + 2], V36_ROW)

    def test_only_the_new_row_is_added(self):
        diff = difflib.ndiff(PAGE.splitlines(), rd.insert_row(PAGE, "v2.1.37", "u", WHEN).splitlines())
        changed = [line for line in diff if line.startswith(("+ ", "- "))]
        self.assertEqual(changed, ["+ | [v2.1.37](u) | 2026-10-16 18:22 |"])

    def test_rerun_of_the_same_release_still_changes_the_file(self):
        once = rd.insert_row(PAGE, "v2.1.37", "u", WHEN)
        self.assertNotEqual(rd.insert_row(once, "v2.1.37", "u", WHEN), once)

    def test_separator_on_the_last_line_without_newline(self):
        page = "| Release | Docs updated (UTC) |\n| --- | --- |"
        self.assertEqual(rd.insert_row(page, "v1", "u", WHEN).splitlines(),
                         ["| Release | Docs updated (UTC) |", "| --- | --- |", "| [v1](u) | 2026-10-16 18:22 |"])

    def test_aligned_separator(self):
        page = "| Release | Docs updated (UTC) |\n| :--- | ---: |\n"
        self.assertIn("| [v1](u) | 2026-10-16 18:22 |", rd.insert_row(page, "v1", "u", WHEN))

    def test_missing_table_fails(self):
        with self.assertRaises(ValueError):
            rd.insert_row("---\ntitle: x\n---\n\nNo table.\n", "v1", "u", WHEN)


class RecordReleaseTest(unittest.TestCase):
    def test_writes_the_updated_file_on_the_branch(self):
        # GitHub returns file content as base64 with line breaks.
        api = FakeApi([{"sha": "blob1", "content": base64.encodebytes(PAGE.encode()).decode()},
                       {"commit": {"sha": "c0ffee"}}])
        sha = rd.record_release(api, "flexprice/flexprice-docs", "main", "v2.1.37", "https://x/v2.1.37", WHEN)
        self.assertEqual(sha, "c0ffee")
        self.assertEqual(api.calls[0][:2], ("GET", "/repos/flexprice/flexprice-docs/contents/version.mdx?ref=main"))
        method, path, body = api.calls[1]
        self.assertEqual((method, path), ("PUT", "/repos/flexprice/flexprice-docs/contents/version.mdx"))
        self.assertEqual((body["sha"], body["branch"]), ("blob1", "main"))
        self.assertEqual(body["message"], "chore: update API reference for flexprice v2.1.37")
        written = base64.b64decode(body["content"]).decode()
        v37_row = "| [v2.1.37](https://x/v2.1.37) | 2026-10-16 18:22 |"
        self.assertEqual(written, PAGE.replace(V36_ROW, f"{v37_row}\n{V36_ROW}"))


class GithubTest(unittest.TestCase):
    def test_network_faults_become_runtime_errors(self):
        faults = [ConnectionResetError("reset by peer"), http.client.RemoteDisconnected("closed"),
                  http.client.IncompleteRead(b"{"), response(b"<html>502 Bad Gateway</html>")]
        for fault in faults:
            with self.subTest(fault=repr(fault)), \
                    mock.patch.object(rd.urllib.request, "urlopen", side_effect=[fault]), \
                    self.assertRaisesRegex(RuntimeError, f"^GET {re.escape(CONTENTS_PATH)}: "):
                rd.github("GET", CONTENTS_PATH)

    def test_unreadable_error_body_still_reports_the_status(self):
        for failure in (http.client.IncompleteRead(b"<html>"), ConnectionResetError("reset by peer")):
            with self.subTest(failure=repr(failure)), \
                    mock.patch.object(rd.urllib.request, "urlopen", side_effect=[bad_gateway(failure)]), \
                    self.assertRaisesRegex(RuntimeError, f"^GET {re.escape(CONTENTS_PATH)}: HTTP 502 "):
                rd.github("GET", CONTENTS_PATH)


class MainTest(unittest.TestCase):
    def run_main(self, github):
        out = io.StringIO()
        argv = ["redeploy_docs.py", "--repo", "o/r", "--tag", "v2.1.37", "--release-url", "u"]
        with mock.patch.object(sys, "argv", argv), \
                mock.patch.dict(os.environ, {"DOCS_REPO_TOKEN": "t"}, clear=True), \
                mock.patch.object(rd, "github", github), contextlib.redirect_stdout(out):
            code = rd.main()
        annotations = [line for line in out.getvalue().splitlines() if line.startswith("::error::")]
        return code, annotations

    def test_records_the_release_without_waiting_for_mintlify(self):
        calls = []

        def github(method, path, body=None, *, token=""):
            calls.append((method, path))
            if (method, path) == ("GET", CONTENTS_PATH):
                return {"sha": "blob1", "content": base64.b64encode(PAGE.encode()).decode()}
            if (method, path) == ("PUT", "/repos/o/r/contents/version.mdx"):
                return {"commit": {"sha": "c0ffee"}}
            raise AssertionError(f"unexpected call: {method} {path}")

        code, annotations = self.run_main(github)
        self.assertEqual(code, 0)
        self.assertEqual(annotations, [])
        self.assertEqual([method for method, _ in calls], ["GET", "PUT"])

    def test_missing_version_file_fails_with_an_annotation(self):
        def github(method, path, body=None, *, token=""):
            raise RuntimeError(f'{method} {path}: HTTP 404 {{\n  "message": "Not Found"\n}}')

        code, annotations = self.run_main(github)
        self.assertEqual(code, 1)
        self.assertEqual(len(annotations), 1)
        self.assertIn('"message": "Not Found"', annotations[0])

    def test_version_file_without_a_table_fails_with_an_annotation(self):
        def github(method, path, body=None, *, token=""):
            return {"sha": "blob1", "content": base64.b64encode(b"No table here.\n").decode()}

        code, annotations = self.run_main(github)
        self.assertEqual(code, 1)
        self.assertEqual(len(annotations), 1)
        self.assertIn("no table separator", annotations[0])


if __name__ == "__main__":
    unittest.main()
