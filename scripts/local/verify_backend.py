"""Provision a local test tenant and run the selected Go E2E probe suites."""

from contextlib import closing
from datetime import datetime, timedelta, timezone
from http.client import HTTPConnection
import json
import os
from secrets import token_hex
import subprocess
import sys


HOST = "127.0.0.1"
PORT = 8080


def request(method, path, body=None, *, headers=None, expected=(200,)):
    request_headers = {"Content-Type": "application/json", **(headers or {})}
    with closing(HTTPConnection(HOST, PORT, timeout=15)) as conn:
        conn.request(method, path, json.dumps(body) if body is not None else None, request_headers)
        response = conn.getresponse()
        data = response.read()
        if response.status not in expected:
            request_id = response.getheader("X-Request-ID", "unavailable")
            raise RuntimeError(
                f"{method} {path} returned HTTP {response.status}, expected {expected} (request ID {request_id})"
            )
        return json.loads(data) if data else {}


def main():
    suite = os.environ.get("LOCAL_SUITE", "all")
    tests = {
        "all": "TestE2EProbe_Local(Auth|Smoke|Temporal)|TestLiveMatrix",
        "auth": "TestE2EProbe_LocalAuth",
        "events": "TestE2EProbe_LocalSmoke",
        "temporal": "TestE2EProbe_LocalTemporal",
        "billing": "TestLiveMatrix",
    }
    if suite not in tests:
        raise RuntimeError(f"Unknown LOCAL_SUITE {suite!r}")

    request("GET", "/health")
    tag = token_hex(8)
    credentials = {"email": f"local-verify-{tag}@example.com", "password": f"Local1!{token_hex(16)}"}
    signup = request("POST", "/v1/auth/signup", {**credentials, "tenant_name": f"Local verify {tag}"})
    session = {"Authorization": "Bearer " + signup["token"]}
    environments = request("GET", "/v1/environments", headers=session)["environments"]
    if len(environments) != 1:
        raise RuntimeError(f"Expected one test environment, found {len(environments)}")
    session["X-Environment-ID"] = environments[0]["id"]
    key = request("POST", "/v1/secrets/api/keys", {
        "name": "Local behavior tests", "type": "private_key",
        "expires_at": (datetime.now(timezone.utc) + timedelta(hours=3)).isoformat(timespec="seconds"),
    }, headers=session, expected=(200, 201))

    try:
        probe_env = {
            **{k: v for k, v in os.environ.items() if not k.startswith("E2EPROBE_")},
            "E2EPROBE_API_HOST": f"http://{HOST}:{PORT}/v1",
            "E2EPROBE_API_KEY": key["api_key"],
            "E2EPROBE_ENVIRONMENT_ID": session["X-Environment-ID"],
            "E2EPROBE_OTEL_ENABLED": "false",
        }
        if suite in {"all", "auth"}:
            probe_env.update(
                E2EPROBE_LOCAL_EMAIL=credentials["email"],
                E2EPROBE_LOCAL_PASSWORD=credentials["password"],
                E2EPROBE_LOCAL_SIGNUP_USER_ID=signup["user_id"],
            )

        packages = []
        if suite != "billing":
            packages.append("./cmd/e2eprobe")
        if suite in {"all", "billing"}:
            packages.append("./internal/ee/e2eprobe/billingmatrix")
            probe_env.setdefault("BM_FAMILIES", "opening,validation")

        subprocess.run([
            "go", "test", "-tags", "e2eprobe_integration", "-count=1", "-v",
            "-timeout", "2h", *packages, "-run", f"^({tests[suite]})$",
        ], check=True, env=probe_env)
    finally:
        active_error = sys.exc_info()[1]
        try:
            request("DELETE", "/v1/secrets/api/keys/" + key["secret"]["id"], headers=session, expected=(204,))
        except (OSError, RuntimeError) as cleanup_error:
            if active_error is None:
                raise
            print(f"FAIL revoking the local test key: {cleanup_error}", file=sys.stderr)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, RuntimeError, KeyError, subprocess.CalledProcessError) as error:
        sys.exit(f"FAIL local verification: {error}")
