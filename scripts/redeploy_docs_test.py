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
import urllib.request
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


def file_response(text, sha="blob1"):
    """A Contents API response for a file, shaped like GitHub's."""
    return {"type": "file", "encoding": "base64", "sha": sha, "content": base64.encodebytes(text.encode()).decode()}


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


class FakeResponse(io.BytesIO):
    """An HTTP response with a status, whose body read can fail part-way like a truncated 502."""

    def __init__(self, status, body=b"", failure=None):
        super().__init__(body)
        self.status = status
        self.failure = failure

    def read(self, *args):
        if self.failure is not None:
            raise self.failure
        return super().read(*args)


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
        api = FakeApi([file_response(PAGE), {"commit": {"sha": "c0ffee"}}])
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


class HttpsOpenerTest(unittest.TestCase):
    def test_refuses_non_https_urls(self):
        for url in ("file:///etc/passwd", "http://api.github.com/"):
            with self.subTest(url=url), self.assertRaises(urllib.error.URLError):
                rd.https_opener().open(url)


class GithubTest(unittest.TestCase):
    def setUp(self):
        # github() must go through https_opener(); urlopen would also accept file:// URLs.
        guard = mock.patch.object(urllib.request, "urlopen", side_effect=AssertionError("urlopen must not be used"))
        guard.start()
        self.addCleanup(guard.stop)

    def opener_returns(self, *results):
        return mock.patch.object(urllib.request.OpenerDirector, "open", side_effect=list(results))

    def test_returns_the_decoded_json(self):
        with self.opener_returns(FakeResponse(200, b'{"type": "file"}')):
            self.assertEqual(rd.github("GET", CONTENTS_PATH), {"type": "file"})

    def test_error_status_becomes_a_runtime_error_with_the_body(self):
        with self.opener_returns(FakeResponse(404, b'{"message": "Not Found"}')), \
                self.assertRaisesRegex(RuntimeError, f"^GET {re.escape(CONTENTS_PATH)}: HTTP 404 .*Not Found"):
            rd.github("GET", CONTENTS_PATH)

    def test_network_faults_become_runtime_errors(self):
        faults = [ConnectionResetError("reset by peer"), http.client.RemoteDisconnected("closed"),
                  http.client.IncompleteRead(b"{"), FakeResponse(200, b"<html>502 Bad Gateway</html>")]
        for fault in faults:
            with self.subTest(fault=repr(fault)), self.opener_returns(fault), \
                    self.assertRaisesRegex(RuntimeError, f"^GET {re.escape(CONTENTS_PATH)}: "):
                rd.github("GET", CONTENTS_PATH)

    def test_unreadable_error_body_still_reports_the_status(self):
        for failure in (http.client.IncompleteRead(b"<html>"), ConnectionResetError("reset by peer")):
            with self.subTest(failure=repr(failure)), self.opener_returns(FakeResponse(502, failure=failure)), \
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
                return file_response(PAGE)
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
            return file_response("No table here.\n")

        code, annotations = self.run_main(github)
        self.assertEqual(code, 1)
        self.assertEqual(len(annotations), 1)
        self.assertIn("no table separator", annotations[0])

    def test_directory_instead_of_a_file_fails_with_an_annotation(self):
        def github(method, path, body=None, *, token=""):
            # GitHub lists a directory's entries instead of returning one file.
            return [{"type": "file", "name": "a.mdx", "path": "version.mdx/a.mdx", "sha": "blob2"}]

        code, annotations = self.run_main(github)
        self.assertEqual(code, 1)
        self.assertEqual(len(annotations), 1)
        self.assertIn("version.mdx on main is not a file", annotations[0])

    def test_write_response_without_a_commit_fails_with_an_annotation(self):
        def github(method, path, body=None, *, token=""):
            return file_response(PAGE) if method == "GET" else []

        code, annotations = self.run_main(github)
        self.assertEqual(code, 1)
        self.assertEqual(len(annotations), 1)
        self.assertIn("no commit SHA", annotations[0])


if __name__ == "__main__":
    unittest.main()
