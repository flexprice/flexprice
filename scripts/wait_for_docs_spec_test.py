"""Run: python3 -B scripts/wait_for_docs_spec_test.py"""

import contextlib
import copy
import importlib.util
import io
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest import mock

sys.dont_write_bytecode = True
SPEC = importlib.util.spec_from_file_location("wait_for_docs_spec", Path(__file__).with_name("wait-for-docs-spec.py"))
docs_spec = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(docs_spec)


class DocsSpecTest(unittest.TestCase):
    def setUp(self):
        self.base = {
            "openapi": "3.0.0",
            "info": {"title": "Fixture", "version": "1.0.0"},
            "paths": {"/events": {"post": {"responses": {"200": {"description": "OK"}}}}},
        }
        self.hosted = copy.deepcopy(self.base)
        self.hosted["info"]["x-speakeasy-name-override"] = "Fixture"
        self.hosted["paths"]["/events"]["post"]["x-codeSamples"] = [
            {"lang": lang, "source": f"current {lang} sample"}
            for lang in ("go", "python", "typescript")
        ]

    def run_main(self, expected, bodies, *options):
        with tempfile.TemporaryDirectory(prefix="flexprice-docs-spec-test-") as directory:
            local = Path(directory) / "spec.json"
            saved = Path(directory) / "saved.json"
            local.write_text(json.dumps(expected))
            argv = ["wait-for-docs-spec.py", str(local), "--timeout", "0", "--save", str(saved), *options]
            output = io.StringIO()
            with mock.patch.object(sys, "argv", argv), \
                    mock.patch.dict(os.environ, {"GITHUB_STEP_SUMMARY": ""}), \
                    mock.patch.object(docs_spec, "fetch", side_effect=bodies), \
                    mock.patch.object(docs_spec.time, "sleep"), contextlib.redirect_stdout(output):
                status = docs_spec.main()
            return status, saved.read_bytes() if saved.exists() else None, output.getvalue()

    def test_default_mode_accepts_sdk_overlays_and_complete_samples(self):
        body = json.dumps(self.hosted, indent=3).encode() + b"\n\n"
        status, saved, _ = self.run_main(self.base, [body])
        self.assertEqual(status, 0)
        self.assertEqual(saved, body)

    def test_default_mode_rejects_incomplete_samples(self):
        self.hosted["paths"]["/events"]["post"]["x-codeSamples"].pop()
        status, saved, output = self.run_main(self.base, [json.dumps(self.hosted).encode()])
        self.assertEqual(status, 1)
        self.assertIsNone(saved)
        self.assertIn("operations have no code samples", output)

    def test_exact_mode_rejects_outdated_samples_in_each_language(self):
        for language in ("go", "python", "typescript"):
            with self.subTest(language=language):
                stale = copy.deepcopy(self.hosted)
                for sample in stale["paths"]["/events"]["post"]["x-codeSamples"]:
                    if sample["lang"] == language:
                        sample["source"] = "outdated sample"
                status, saved, output = self.run_main(self.hosted, [json.dumps(stale).encode()], "--exact")
                self.assertEqual(status, 1)
                self.assertIsNone(saved)
                self.assertIn("full spec does not match", output)

    def test_exact_mode_rejects_changed_sdk_overlays(self):
        stale = copy.deepcopy(self.hosted)
        stale["info"]["x-speakeasy-name-override"] = "OlderFixture"
        status, saved, _ = self.run_main(self.hosted, [json.dumps(stale).encode()], "--exact")
        self.assertEqual(status, 1)
        self.assertIsNone(saved)

    def test_exact_mode_accepts_equivalent_json_with_different_formatting(self):
        body = json.dumps(self.hosted, indent=3, sort_keys=True).encode() + b"\n\n"
        status, saved, _ = self.run_main(self.hosted, [body], "--exact")
        self.assertEqual(status, 0)
        self.assertEqual(saved, body)

    def test_exact_mode_waits_for_the_uploaded_samples(self):
        stale = copy.deepcopy(self.hosted)
        stale["paths"]["/events"]["post"]["x-codeSamples"][0]["source"] = "outdated sample"
        body = json.dumps(self.hosted).encode()
        status, saved, output = self.run_main(
            self.hosted, [json.dumps(stale).encode(), body], "--exact", "--timeout", "1"
        )
        self.assertEqual(status, 0)
        self.assertEqual(saved, body)
        self.assertIn("Waiting for hosted docs spec", output)


if __name__ == "__main__":
    unittest.main()
