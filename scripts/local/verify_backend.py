"""Run bounded behavior checks against the local backend."""

from contextlib import closing
from datetime import datetime, timedelta, timezone
from http.client import HTTPConnection
import json
import os
from secrets import token_hex
import subprocess
import sys
import time


HOST = "127.0.0.1"
PORT = 8080


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def request(method, path, body=None, *, headers=None, expected=(200,)):
    request_headers = {"Content-Type": "application/json", **(headers or {})}
    with closing(HTTPConnection(HOST, PORT, timeout=15)) as conn:
        conn.request(method, path, json.dumps(body) if body is not None else None, request_headers)
        response = conn.getresponse()
        data = response.read()
        request_id = response.getheader("X-Request-ID", "unavailable")
        require(
            response.status in expected,
            f"{method} {path} returned HTTP {response.status}, expected {expected} (request ID {request_id})",
        )
        return json.loads(data) if data and response.status < 400 else {}


def verify_auth(tag):
    credentials = {"email": f"local-verify-{tag}@example.com", "password": f"Local1!{token_hex(16)}"}
    signup = request("POST", "/v1/auth/signup", {**credentials, "tenant_name": f"Local verify {tag}"})
    login = request("POST", "/v1/auth/login", credentials)
    session = {"Authorization": "Bearer " + login["token"]}
    user = request("GET", "/v1/users/me", headers=session)
    require(user["id"] == signup["user_id"] == login["user_id"], "Signup, login and user lookup returned different users")
    request("POST", "/v1/auth/login", {**credentials, "password": "Wrong-local-password1!"}, expected=(403,))
    request("GET", "/v1/users/me", expected=(401,))
    print("PASS signup, login, authenticated user lookup, wrong password and missing token", flush=True)
    return session


def verify_plan_sync(tag, api_headers):
    def api(method, path, body=None, expected=(200,)):
        return request(method, "/v1" + path, body, headers=api_headers, expected=expected)

    customer_id = api("POST", "/customers", {
        "external_id": f"local-sync-{tag}", "name": "Local price sync", "skip_onboarding_workflow": True,
    }, expected=(201,))["id"]
    plan_id = api("POST", "/plans", {"name": "Local price sync"}, expected=(201,))["id"]
    price_spec = {
        "amount": "2.00", "currency": "usd", "entity_type": "PLAN", "entity_id": plan_id,
        "price_unit_type": "FIAT", "billing_period": "MONTHLY", "billing_period_count": 1,
        "invoice_cadence": "ARREAR", "billing_model": "FLAT_FEE",
        "start_date": (datetime.now(timezone.utc) - timedelta(days=1)).isoformat(timespec="seconds"),
    }
    base_price_id = api("POST", "/prices", {**price_spec, "type": "FIXED"}, expected=(201,))["id"]
    sub_id = api("POST", "/subscriptions", {
        "customer_id": customer_id, "plan_id": plan_id, "currency": "usd",
        "billing_period": "MONTHLY", "billing_period_count": 1, "billing_cycle": "anniversary",
    }, expected=(201,))["id"]
    meter_id = api("POST", "/meters", {
        "name": "Local price sync meter", "event_name": f"local_sync_{tag}",
        "aggregation": {"type": "COUNT"}, "reset_usage": "BILLING_PERIOD",
    }, expected=(201,))["id"]
    metered_price_id = api("POST", "/prices", {
        **price_spec, "type": "USAGE", "meter_id": meter_id,
    }, expected=(201,))["id"]

    def line_prices():
        subscription = api("GET", f"/subscriptions/{sub_id}")
        return {item["price_id"] for item in subscription.get("line_items", [])}

    before = line_prices()
    require(base_price_id in before and metered_price_id not in before, "Unexpected pre-sync subscription line items")
    run = api("POST", f"/plans/{plan_id}/sync/subscriptions", {})
    workflow_id, run_id = run["workflow_id"], run["run_id"]
    require(workflow_id and run_id, "Price sync trigger did not return a workflow and run ID")
    status = "absent"
    deadline = time.monotonic() + 60
    while time.monotonic() < deadline:
        workflows = api("POST", "/workflows/search", {"workflow_id": workflow_id, "limit": 10})
        row = next((item for item in (workflows["items"] or []) if item["run_id"] == run_id), None)
        if row:
            status = row["status"]
            if status == "Completed":
                require(row["workflow_type"] == "PriceSyncV2Workflow", f"Unexpected workflow type for {run_id}")
                break
            require(status == "Running", f"Plan price sync {run_id} ended with status {status}")
        time.sleep(1)
    else:
        raise RuntimeError(f"Plan price sync {run_id} was {status} in workflow search after 60 seconds")

    summary = api("GET", f"/workflows/{workflow_id}/{run_id}/summary")
    require(
        summary["status"] == "Completed" and summary["activity_count"] > 0 and summary["failed_activities"] == 0,
        f"Unexpected workflow summary for {run_id}",
    )
    require(metered_price_id in line_prices(), "Completed plan price sync did not attach the metered price")
    print(f"PASS Temporal plan price sync applied a new metered price (run {run_id})", flush=True)


def main():
    suite = os.environ.get("LOCAL_SUITE", "all")
    require(suite in {"all", "auth", "events", "temporal", "billing"}, f"Unknown LOCAL_SUITE {suite!r}")
    request("GET", "/health")
    tag = token_hex(8)
    session = verify_auth(tag)
    if suite == "auth":
        return
    environments = request("GET", "/v1/environments", headers=session)["environments"]
    require(len(environments) == 1, f"Expected one test environment, found {len(environments)}")
    session["X-Environment-ID"] = environments[0]["id"]
    key = request("POST", "/v1/secrets/api/keys", {
        "name": "Local behavior tests", "type": "private_key",
        "expires_at": (datetime.now(timezone.utc) + timedelta(hours=3)).isoformat(timespec="seconds"),
    }, headers=session, expected=(200, 201))
    api_headers = {"x-api-key": key["api_key"], "X-Environment-ID": session["X-Environment-ID"]}
    probe_env = {
        **{k: v for k, v in os.environ.items() if not k.startswith("E2EPROBE_")},
        "E2EPROBE_API_HOST": f"http://{HOST}:{PORT}/v1",
        "E2EPROBE_API_KEY": key["api_key"],
        "E2EPROBE_ENVIRONMENT_ID": session["X-Environment-ID"],
        "E2EPROBE_OTEL_ENABLED": "false",
    }
    try:
        request("GET", "/v1/users/me", headers=api_headers)
        if suite in {"all", "events"}:
            subprocess.run([
                "go", "test", "-tags", "e2eprobe_integration", "-count=1", "-v", "-timeout", "3m",
                "./cmd/e2eprobe", "-run", "^TestE2EProbe_LocalSmoke$",
            ], check=True, env=probe_env)
        if suite in {"all", "temporal"}:
            verify_plan_sync(tag, api_headers)
        if suite in {"all", "billing"}:
            probe_env.setdefault("BM_FAMILIES", "opening,validation")
            subprocess.run([
                "go", "test", "-tags", "e2eprobe_integration", "-count=1", "-v", "-timeout", "2h",
                "./internal/ee/e2eprobe/billingmatrix", "-run", "^TestLiveMatrix$",
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
