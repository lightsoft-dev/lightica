"""API smoke test for a fork backend on the throwaway verify stack (LIGH-190).

    python3 -I lightsoft/backend/smoke.py http://127.0.0.1:18080 <expected commit> <expected version>

Signs in with the dev verification code, registers a fake Claude runtime, sends a
chat message with a model / effort selection and claims the task the way a daemon
does — no agent CLI runs. Only for the verify stack: it creates data.
"""

import json
import sys
import urllib.error
import urllib.request
import uuid

BASE, WANT_COMMIT, WANT_VERSION = sys.argv[1].rstrip("/"), sys.argv[2], sys.argv[3]
failures = []


def call(method, path, body=None, token=None, workspace=None, want=(200, 201)):
    req = urllib.request.Request(BASE + path, method=method)
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", "Bearer " + token)
    if workspace:
        req.add_header("X-Workspace-ID", workspace)
    data = json.dumps(body).encode() if body is not None else None
    try:
        with urllib.request.urlopen(req, data, timeout=15) as resp:
            status, raw = resp.status, resp.read()
    except urllib.error.HTTPError as err:
        status, raw = err.code, err.read()
    if status not in want:
        raise SystemExit(f"{method} {path}: HTTP {status} {raw[:300]!r}")
    return json.loads(raw) if raw else None


def check(name, ok, detail=""):
    print(("ok   " if ok else "FAIL ") + name + (f" — {detail}" if detail else ""))
    if not ok:
        failures.append(name)


health = call("GET", "/health")
check("/health commit is the build's HEAD", health.get("commit") == WANT_COMMIT, health.get("commit", ""))
config = call("GET", "/api/config")
check("/api/config server_version is the team version", config.get("server_version") == WANT_VERSION, config.get("server_version", ""))

email = f"smoke-{uuid.uuid4().hex[:8]}@example.com"
call("POST", "/auth/send-code", {"email": email})
jwt = call("POST", "/auth/verify-code", {"email": email, "code": "888888"})["token"]
ws = call("POST", "/api/workspaces", {"name": "Fork smoke", "slug": "fork-smoke-" + uuid.uuid4().hex[:6]}, jwt)["id"]
pat = call("POST", "/api/tokens", {"name": "fork-smoke", "expires_in_days": 1}, jwt)["token"]

caps = call("GET", "/api/lightsoft/capabilities", token=jwt, workspace=ws)
check("/api/lightsoft/capabilities", caps.get("chat_model_selection") is True, json.dumps(caps))

reg = call("POST", "/api/daemon/register", {
    "workspace_id": ws,
    "daemon_id": str(uuid.uuid4()),
    "device_name": "fork-smoke",
    "cli_version": "0.11.0",
    "runtimes": [{"name": "Claude (smoke)", "type": "claude", "version": "2.1.0", "status": "online"}],
}, pat)
runtime = reg["runtimes"][0]["id"]

agent = call("POST", "/api/agents", {
    "name": "Smoke agent", "runtime_id": runtime, "visibility": "workspace",
    "model": "claude-sonnet-4-6", "thinking_level": "medium",
}, jwt, ws)["id"]
session = call("POST", "/api/chat/sessions", {"agent_id": agent, "title": "smoke"}, jwt, ws)["id"]

call("POST", f"/api/chat/sessions/{session}/messages",
     {"content": "hello", "model_selection": {"model": "claude-opus-4-1", "thinking_level": "high"}}, jwt, ws)
sel = call("GET", f"/api/chat/sessions/{session}/model-selection", token=jwt, workspace=ws)["model_selection"] or {}
check("session model-selection is recorded", (sel.get("model"), sel.get("thinking_level")) == ("claude-opus-4-1", "high"), json.dumps(sel))

call("POST", f"/api/chat/sessions/{session}/messages",
           {"content": "x", "model_selection": {"thinking_level": "turbo"}}, jwt, ws, want=(400,))
check("unknown effort is rejected (400)", True)

claim = call("POST", f"/api/daemon/runtimes/{runtime}/tasks/claim", {}, pat)
task_agent = (claim.get("task") or {}).get("agent") or {}
check("claim carries the chat selection",
      (task_agent.get("model"), task_agent.get("thinking_level")) == ("claude-opus-4-1", "high"),
      f"{task_agent.get('model')}/{task_agent.get('thinking_level')}")

print("\n" + ("all checks passed" if not failures else f"{len(failures)} check(s) failed"))
sys.exit(1 if failures else 0)
