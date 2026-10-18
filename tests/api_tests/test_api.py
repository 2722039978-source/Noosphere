"""Noosphere Backend API Test Suite"""

import urllib.request
import json
import sys

BASE = "http://127.0.0.1:8080"
passed = 0
failed = 0

def test(name, url, method="GET", body=None, expected_status=200, check_key=None):
    global passed, failed
    try:
        data = json.dumps(body).encode() if body else None
        req = urllib.request.Request(url, data=data, method=method)
        req.add_header("Content-Type", "application/json")
        with urllib.request.urlopen(req, timeout=5) as resp:
            result = json.loads(resp.read())
            if check_key and check_key not in str(result):
                raise Exception(f"Key '{check_key}' not found")
            print(f"  [PASS] {name}")
            passed += 1
    except Exception as e:
        print(f"  [FAIL] {name}: {e}")
        failed += 1

print("=== Noosphere API Tests ===\n")

test("Health Check", f"{BASE}/health", check_key="status")
test("Gateway Health", f"{BASE}/api/gateway/health", check_key="ok")
test("Models List", f"{BASE}/api/gateway/models", check_key="models")
test("Stats", f"{BASE}/api/gateway/stats", check_key="total")
test("Logs", f"{BASE}/api/gateway/logs", check_key="logs")
test("Nebula Sessions", f"{BASE}/api/v1/stats", check_key="sessions")
test("DevOps Metrics", f"{BASE}/api/v1/devops/metrics", check_key="cpu")
test("DevOps Tools", f"{BASE}/api/v1/devops/tools", check_key="tools")

print(f"\nResults: {passed}/8 passed, {failed} failed")
sys.exit(0 if failed == 0 else 1)
