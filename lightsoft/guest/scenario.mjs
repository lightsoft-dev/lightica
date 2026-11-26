// Guest (outside collaborator) regression scenario (LIGH-282).
//
// Checks upstream's guest role (MUL-9420) as it runs on the fork backend:
// what a guest sees and is refused, that a guest cannot start an agent run,
// and the add / narrow / promote / remove steps in lightsoft/README.md.
//
// Runs against a THROWAWAY backend, never production: it creates users, a
// workspace, agents and a fake daemon runtime, and needs the dev login code.
//
//   node lightsoft/guest/scenario.mjs http://127.0.0.1:18096
//   MULTICA_CLI=/path/to/multica node lightsoft/guest/scenario.mjs ...   # also drive the CLI as the guest
//   SECRETS_URL=http://127.0.0.1:<port> USAGE_URL=http://127.0.0.1:<port> ...   # also the fork's sidecars,
//     each started with its *_API_URL pointing at the same throwaway backend
//
// The backend must run with APP_ENV=development and
// MULTICA_DEV_VERIFICATION_CODE=888888 (lightsoft/backend/docker-compose.verify.yml
// does both). Exits non-zero when a check fails.
import { spawnSync } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const BASE = (process.argv[2] || "http://127.0.0.1:18096").replace(/\/$/, "");
if (/:(8080|3000|3443|3444|13000)\b/.test(BASE) || /lightica\.lightsoft\.dev|ts\.net/.test(BASE)) {
  console.error("refusing to run against what looks like production:", BASE);
  process.exit(2);
}
const GUEST_CAPABILITY = "workspace-guests-v1";
const tag = Math.random().toString(36).slice(2, 8);
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const results = [];
let failed = 0;

async function call(method, path, { body, token, ws, want, headers: extra } = {}) {
  const headers = { "Content-Type": "application/json", ...extra };
  if (token) headers.Authorization = "Bearer " + token;
  if (ws) headers["X-Workspace-ID"] = ws;
  const res = await fetch(BASE + path, { method, headers, body: body === undefined ? undefined : JSON.stringify(body) });
  const text = await res.text();
  let json = null;
  try { json = text ? JSON.parse(text) : null; } catch { json = text; }
  if (want && !want.includes(res.status)) throw new Error(`${method} ${path}: HTTP ${res.status} ${text.slice(0, 300)}`);
  return { status: res.status, json, text };
}

function check(area, name, ok, observed) {
  results.push({ ok, area, name, observed });
  if (!ok) failed++;
}

async function login(email) {
  await call("POST", "/auth/send-code", { body: { email }, want: [200] });
  return (await call("POST", "/auth/verify-code", { body: { email, code: "888888" }, want: [200] })).json.token;
}

const list = (j, key) => (Array.isArray(j) ? j : (j && j[key]) || []);
const ok2xx = (r) => r.status >= 200 && r.status < 300;

// ---------- owner setup ----------
const ownerEmail = `owner-${tag}@example.com`, guestEmail = `guest-${tag}@example.com`, guest2Email = `guest2-${tag}@example.com`;
const owner = await login(ownerEmail);
const ws = (await call("POST", "/api/workspaces", { token: owner, body: { name: "Guest test", slug: "guest-" + tag }, want: [200, 201] })).json.id;
const ownerPat = (await call("POST", "/api/tokens", { token: owner, body: { name: "t", expires_in_days: 1 }, want: [200, 201] })).json.token;
const O = (m, p, body, want = [200, 201, 204]) => call(m, p, { token: owner, ws, body, want });

const reg = (await call("POST", "/api/daemon/register", { token: ownerPat, want: [200, 201], body: {
  workspace_id: ws, daemon_id: crypto.randomUUID(), device_name: "owner-mac", cli_version: "0.13.0",
  runtimes: [{ name: "Claude (owner)", type: "claude", version: "2.1.0", status: "online" }] } })).json;
const runtime = reg.runtimes[0].id;
const agentPub = (await O("POST", "/api/agents", { name: `Public agent ${tag}`, runtime_id: runtime, instructions: `AGENT-INSTR-${tag}`, permission_mode: "public_to", invocation_targets: [{ target_type: "workspace" }] })).json.id;
await O("POST", "/api/squads", { name: `Squad-${tag}`, leader_id: agentPub });

// Projects: EXT (workspace-visible, shared with the guest), EXTP (private, shared),
// INT (private, not shared), PUB (workspace-visible, not shared), and an issue with no project.
const pExt = (await O("POST", "/api/projects", { title: `EXT-${tag}` })).json.id;
const pExtP = (await O("POST", "/api/projects", { title: `EXTP-${tag}`, visibility: "private" })).json.id;
const pInt = (await O("POST", "/api/projects", { title: `INT-SECRET-${tag}`, visibility: "private" })).json.id;
const pPub = (await O("POST", "/api/projects", { title: `PUB-SECRET-${tag}` })).json.id;
const mk = async (title, project, extra = {}) => (await O("POST", "/api/issues", { title, description: `body of ${title}`, ...(project ? { project_id: project } : {}), ...extra })).json.id;
const iExt = await mk(`EXT issue ${tag}`, pExt);
const iExtP = await mk(`EXTP issue ${tag}`, pExtP);
const iInt = await mk(`INT-SECRET issue ${tag}`, pInt);
const iPub = await mk(`PUB-SECRET issue ${tag}`, pPub);
const iNone = await mk(`NONE-SECRET issue ${tag}`, null);
for (const i of [iExt, iExtP, iInt, iPub, iNone]) await O("POST", `/api/issues/${i}/comments`, { content: `comment ${tag}` });
// The owner hands an EXT issue to the agent, so the guest meets that agent on an issue.
const iRun = await mk(`owner assigns ${tag}`, pExt, { assignee_type: "agent", assignee_id: agentPub });

// ---------- add: invite as guest with projects ----------
let r = await O("POST", `/api/workspaces/${ws}/members`, { email: guestEmail, role: "guest" }, [200, 201, 400]);
check("추가", "프로젝트 없이 guest 초대 → 400", r.status === 400, `HTTP ${r.status}`);
r = await O("POST", `/api/workspaces/${ws}/members`, { email: guestEmail, role: "guest", project_ids: [pExt, pExtP] });
check("추가", "프로젝트를 골라 guest로 초대", r.json?.role === "guest", `role=${r.json?.role}`);
const guest = await login(guestEmail);
const guestMe = (await call("GET", "/api/me", { token: guest, want: [200] })).json;
let inv = (await call("GET", "/api/invitations", { token: guest, want: [200] })).json;
await call("POST", `/api/invitations/${list(inv, "invitations")[0].id}/accept`, { token: guest, want: [200, 201] });
const G = (m, p, body, headers) => call(m, p, { token: guest, ws, body, headers });
const guestPat = (await call("POST", "/api/tokens", { token: guest, body: { name: "cli", expires_in_days: 1 }, want: [200, 201] })).json.token;

// A second guest on PUB only: the first guest shares no project with it.
await O("POST", `/api/workspaces/${ws}/members`, { email: guest2Email, role: "guest", project_ids: [pPub] });
const guest2 = await login(guest2Email);
inv = (await call("GET", "/api/invitations", { token: guest2, want: [200] })).json;
await call("POST", `/api/invitations/${list(inv, "invitations")[0].id}/accept`, { token: guest2, want: [200, 201] });

const hidden = [pInt, pPub, iInt, iPub, iNone, `INT-SECRET-${tag}`, `PUB-SECRET-${tag}`, `NONE-SECRET`];
const leaks = (res) => hidden.filter((s) => res.text.includes(s));
const has = (res, s) => res.text.includes(s);

// ---------- what the guest sees ----------
r = await G("GET", "/api/projects");
check("프로젝트", "목록: EXT·EXTP만", has(r, pExt) && has(r, pExtP) && leaks(r).length === 0, `HTTP ${r.status} leaks=${leaks(r)}`);
for (const [n, id, want] of [["EXT", pExt, 200], ["EXTP", pExtP, 200], ["PUB", pPub, 404], ["INT", pInt, 404]]) {
  r = await G("GET", `/api/projects/${id}`); check("프로젝트", `${n} 상세 → ${want}`, r.status === want, `HTTP ${r.status}`);
}
r = await G("GET", `/api/projects/search?q=${tag}`);
check("검색", "프로젝트 검색에 PUB·INT 없음", leaks(r).length === 0, `HTTP ${r.status} leaks=${leaks(r)}`);

r = await G("GET", "/api/issues?limit=100");
check("이슈", "목록: EXT·EXTP 이슈만", has(r, iExt) && has(r, iExtP) && leaks(r).length === 0, `HTTP ${r.status} leaks=${leaks(r)}`);
for (const [n, id, want] of [["EXT", iExt, 200], ["EXTP", iExtP, 200], ["PUB", iPub, 404], ["INT", iInt, 404], ["NONE", iNone, 404]]) {
  r = await G("GET", `/api/issues/${id}`); check("이슈", `${n} 상세 → ${want}`, r.status === want, `HTTP ${r.status}`);
}
for (const [n, id] of [["PUB", iPub], ["NONE", iNone]]) {
  r = await G("GET", `/api/issues/${id}/comments`); check("이슈", `${n} 코멘트 → 404`, r.status === 404, `HTTP ${r.status}`);
}
r = await G("GET", `/api/issues/search?q=${tag}`);
check("검색", "이슈 검색에 PUB·INT·NONE 없음", has(r, iExt) && leaks(r).length === 0, `HTTP ${r.status} leaks=${leaks(r)}`);
r = await G("POST", "/api/issues/table/rows", { query: { scope: { kind: "workspace" }, sort: { field: "position", direction: "asc" } }, group: { kind: "none" }, hierarchy: { enabled: false }, page: { limit: 100 } });
check("이슈", "표 보기(table/rows)에 숨긴 이슈 없음", r.status === 200 && has(r, iExt) && leaks(r).length === 0, `HTTP ${r.status} leaks=${leaks(r)} ${r.status === 200 ? "" : r.text.slice(0, 120)}`);
r = await G("POST", "/api/issues/table/groups", { query: { scope: { kind: "workspace" }, sort: { field: "position", direction: "asc" } }, group: { kind: "project" }, page: { limit: 100 } });
check("이슈", "표 보기 그룹(table/groups)에 숨긴 프로젝트 없음", r.status === 200 && leaks(r).length === 0, `HTTP ${r.status} leaks=${leaks(r)} ${r.status === 200 ? "" : r.text.slice(0, 120)}`);
r = await G("GET", "/api/search-index/snapshot");
check("검색", "로컬 검색 인덱스에 숨긴 이슈 없음", r.status !== 200 || leaks(r).length === 0, `HTTP ${r.status} leaks=${leaks(r)}`);

r = await G("GET", "/api/agents");
check("에이전트", "목록: 이슈에서 만난 에이전트의 이름만 (지시문 없음)", r.status === 200 && has(r, `Public agent ${tag}`) && !has(r, `AGENT-INSTR-${tag}`), `HTTP ${r.status}`);
r = await G("GET", `/api/agents/${agentPub}`); check("에이전트", "상세 → 403", r.status === 403, `HTTP ${r.status}`);

r = await G("GET", `/api/workspaces/${ws}/members`, undefined, { "X-Client-Capabilities": GUEST_CAPABILITY });
const rows = list(r.json, "members");
check("멤버", "본인 role=guest, 팀원 이메일은 보임", r.status === 200 && rows.find((m) => m.user_id === guestMe.id)?.role === "guest" && has(r, ownerEmail), `HTTP ${r.status}`);
check("멤버", "프로젝트를 같이 쓰지 않는 다른 guest는 안 보임", !has(r, guest2Email), `HTTP ${r.status}`);
check("멤버", "guest에게는 누구의 project_ids도 안 보임", rows.every((m) => m.project_ids === undefined), `HTTP ${r.status}`);
// The secrets and usage services read this list without the capability (lightsoft/secrets, lightsoft/usage).
r = await G("GET", `/api/workspaces/${ws}/members`);
check("멤버", "capability 없는 클라이언트에는 guest 행이 빠짐 (사이드카가 보는 목록)", r.status === 200 && !list(r.json, "members").some((m) => m.user_id === guestMe.id), `HTTP ${r.status}`);

r = await G("GET", `/api/workspaces/${ws}`);
check("워크스페이스", "상세: context·repos·설정 없음", r.status === 200 && r.json.context == null && (r.json.repos || []).length === 0, `HTTP ${r.status}`);

for (const p of [`/api/workspaces/${ws}/runtime-profiles`, `/api/workspaces/${ws}/mcp-servers`, `/api/workspaces/${ws}/member-limit`,
  "/api/dashboard/usage/by-agent?days=30", `/api/chat/sessions/${crypto.randomUUID()}`, "/api/autopilots", "/api/skills",
  `/api/issues/${iExt}/task-runs`, "/api/agent-run-counts",
  // Fork-only routes (lightsoft/README.md "팀 기능") are outside upstream's allow-list too.
  "/api/lightsoft/capabilities", `/api/chat/sessions/${crypto.randomUUID()}/model-selection`]) {
  r = await G("GET", p); check("차단", `GET ${p.replace(ws, "{ws}")} → 403`, r.status === 403, `HTTP ${r.status}`);
}
// Reads the shell makes on every page answer a guest empty, so the page renders.
for (const p of ["/api/runtimes", "/api/squads", "/api/agent-task-snapshot", "/api/chat/sessions", "/api/working-agents", `/api/workspaces/${ws}/invitations`]) {
  r = await G("GET", p);
  check("빈 응답", `GET ${p.replace(ws, "{ws}")} → 200, 내용 없음`, r.status === 200 && !has(r, agentPub) && !has(r, runtime) && !/"id"/.test(r.text), `HTTP ${r.status} ${r.text.slice(0, 60)}`);
}
for (const p of ["/api/labels", "/api/properties", "/api/issue-statuses", "/api/issue-workflows", "/api/inbox", `/api/issues/${iExt}/timeline`]) {
  r = await G("GET", p); check("허용", `GET ${p} → 200`, r.status === 200, `HTTP ${r.status}`);
}

// ---------- writes ----------
r = await G("POST", `/api/issues/${iExt}/comments`, { content: `guest comment ${tag}` }); check("쓰기", "EXT 코멘트 → 201", ok2xx(r), `HTTP ${r.status}`);
const guestComment = r.json?.id;
r = await G("POST", `/api/issues/${iExt}/reactions`, { emoji: "👍" }); check("쓰기", "EXT 반응 → 2xx", ok2xx(r), `HTTP ${r.status}`);
r = await G("POST", `/api/issues/${iExt}/subscribe`); check("쓰기", "EXT 구독 → 2xx", ok2xx(r), `HTTP ${r.status}`);
r = await G("PUT", `/api/comments/${guestComment}`, { content: `guest edited ${tag}` }); check("쓰기", "본인 코멘트 수정 → 200", r.status === 200, `HTTP ${r.status}`);
r = await G("POST", `/api/issues/${iNone}/comments`, { content: "x" }); check("쓰기", "NONE 코멘트 → 404", r.status === 404, `HTTP ${r.status}`);
r = await G("POST", `/api/issues/${iPub}/comments`, { content: "x" }); check("쓰기", "PUB 코멘트 → 404", r.status === 404, `HTTP ${r.status}`);
// Upstream scope: no issue create or edit for a guest. Opening that is a separate task (see lightsoft/README.md).
r = await G("POST", "/api/issues", { title: `guest issue ${tag}`, project_id: pExt }); check("쓰기", "EXT에 이슈 생성 → 403 (upstream 범위)", r.status === 403, `HTTP ${r.status}`);
r = await G("PUT", `/api/issues/${iExt}`, { title: `EXT edited by guest ${tag}` }); check("쓰기", "EXT 이슈 수정 → 403 (upstream 범위)", r.status === 403, `HTTP ${r.status}`);
r = await G("PUT", `/api/issues/${iExt}`, { status: "in_review" }); check("쓰기", "EXT 상태 변경 → 403", r.status === 403, `HTTP ${r.status}`);
r = await G("POST", "/api/projects", { title: `guest-made-${tag}` }); check("쓰기", "프로젝트 생성 → 403", r.status === 403, `HTTP ${r.status}`);
r = await G("PATCH", `/api/workspaces/${ws}`, { settings: {} }); check("쓰기", "워크스페이스 설정 변경 → 403", r.status === 403, `HTTP ${r.status}`);
r = await G("POST", `/api/workspaces/${ws}/members`, { email: `friend-${tag}@example.com`, role: "member" }); check("쓰기", "초대 → 403", r.status === 403, `HTTP ${r.status}`);
r = await G("POST", `/api/projects/${pExt}/members`, { user_ids: [guestMe.id] }); check("쓰기", "프로젝트 멤버 변경 → 403", r.status === 403, `HTTP ${r.status}`);

// ---------- agent invocation ----------
r = await G("POST", "/api/chat/sessions", { agent_id: agentPub, title: "guest chat" }); check("에이전트 호출", "채팅 → 403", r.status === 403, `HTTP ${r.status}`);
r = await G("POST", "/api/issues", { title: `guest assigns agent ${tag}`, project_id: pExt, assignee_type: "agent", assignee_id: agentPub });
check("에이전트 호출", "에이전트 할당 이슈 생성 → 403", r.status === 403, `HTTP ${r.status}`);
r = await call("POST", "/api/daemon/register", { token: guestPat, body: { workspace_id: ws, daemon_id: crypto.randomUUID(), device_name: "guest-mac", cli_version: "0.13.0", runtimes: [{ name: "x", type: "claude", version: "2", status: "online" }] } });
check("에이전트 호출", "guest PAT로 데몬 등록 → 403", r.status === 403, `HTTP ${r.status}`);

// The owner's own run on iRun is settled first, so a new run is not coalesced into it.
const tasks = async () => list((await O("GET", `/api/issues/${iRun}/task-runs`)).json, "tasks");
for (const t of await tasks()) await O("POST", `/api/issues/${iRun}/tasks/${t.id}/cancel`, undefined, [200, 204, 400, 404, 409]);
await sleep(500);
const before = (await tasks()).length;
r = await G("POST", `/api/issues/${iRun}/comments`, { content: "please do X" });
await sleep(800);
const afterComment = (await tasks()).length;
const mention = await G("POST", `/api/issues/${iRun}/comments`, { content: `[@Public agent](mention://agent/${agentPub}) do Y` });
await sleep(800);
const afterMention = (await tasks()).length;
check("에이전트 호출", "에이전트 담당 이슈에 guest 코멘트 → task 안 생김", ok2xx(r) && afterComment === before, `comment HTTP ${r.status}, task ${before}→${afterComment}`);
check("에이전트 호출", "guest의 에이전트 멘션은 거부, task 안 생김", mention.status >= 400 && afterMention === before, `mention HTTP ${mention.status}, task ${afterComment}→${afterMention}`);
await O("POST", `/api/issues/${iRun}/comments`, { content: `[@Public agent](mention://agent/${agentPub}) owner asks` });
await sleep(800);
const afterOwner = (await tasks()).length;
check("에이전트 호출", "대조군: owner 멘션은 task 생성", afterOwner > afterMention, `task ${afterMention}→${afterOwner}`);

// ---------- inbox ----------
await O("POST", `/api/issues/${iPub}/comments`, { content: `hey [@guest](mention://member/${guestMe.id}) PUB-SECRET-MENTION-${tag}` });
await O("POST", `/api/issues/${iExt}/comments`, { content: `hey [@guest](mention://member/${guestMe.id}) EXT-MENTION-${tag}` });
await sleep(1200);
r = await G("GET", "/api/inbox");
check("인박스", "EXT 멘션은 오고 PUB 멘션은 안 옴", has(r, iExt) && !has(r, iPub) && !has(r, `PUB-SECRET-MENTION-${tag}`), `HTTP ${r.status}`);

// ---------- realtime: guests get no WebSocket, the screen polls instead ----------
async function listen(token, ms, during) {
  const events = [];
  let closed = false;
  const sock = new WebSocket(BASE.replace(/^http/, "ws") + `/ws?workspace_id=${ws}`);
  sock.onmessage = (ev) => events.push(String(ev.data));
  sock.onclose = () => { closed = true; };
  await new Promise((res) => { sock.onopen = res; sock.onerror = res; setTimeout(res, 3000); });
  if (sock.readyState === WebSocket.OPEN) sock.send(JSON.stringify({ type: "auth", payload: { token } }));
  await sleep(1000);
  await during();
  await sleep(ms);
  sock.close();
  return { events, closed };
}
const ownerLive = await listen(owner, 1500, async () => { await O("PUT", `/api/issues/${iExt}`, { title: `EXT-LIVE-OWNER-${tag}` }); });
check("실시간", "대조군: owner는 EXT 변경 이벤트를 받음", ownerLive.events.some((e) => e.includes(`EXT-LIVE-OWNER-${tag}`)), `${ownerLive.events.length} events`);
const guestLive = await listen(guest, 2000, async () => {
  await O("PUT", `/api/issues/${iExt}`, { title: `EXT-LIVE-${tag}` });
  await O("PUT", `/api/issues/${iPub}`, { title: `PUB-SECRET-LIVE-${tag}` });
  await O("PUT", `/api/issues/${iNone}`, { title: `NONE-SECRET-LIVE-${tag}` });
  await O("PUT", `/api/agents/${agentPub}`, { description: `AGENT-LIVE-${tag}` });
});
const guestTypes = [...new Set(guestLive.events.map((e) => { try { return JSON.parse(e).type; } catch { return "?"; } }))];
check("실시간", "guest는 워크스페이스 이벤트를 하나도 안 받음", !guestLive.events.some((e) => e.includes(tag) || e.includes(ws)), `${guestLive.events.length} events (${guestTypes}), closed=${guestLive.closed}: ${guestLive.events.join(" ").slice(0, 100)}`);

// ---------- CLI-style access with the guest PAT ----------
r = await call("GET", "/api/issues?limit=100", { token: guestPat, ws });
check("CLI", "PAT로도 EXT·EXTP 이슈만", r.status === 200 && has(r, iExt) && leaks(r).length === 0, `HTTP ${r.status}`);
r = await call("GET", "/api/agents/" + agentPub, { token: guestPat, ws });
check("CLI", "PAT로 에이전트 상세 → 403", r.status === 403, `HTTP ${r.status}`);
if (process.env.MULTICA_CLI) {
  // A throwaway HOME and working directory keep the real CLI config and any
  // agent task marker out of it; the token goes through env only.
  const home = mkdtempSync(join(tmpdir(), "guest-cli-"));
  const cli = (...args) => spawnSync(process.env.MULTICA_CLI, args, { encoding: "utf8", timeout: 30000, cwd: home,
    env: { PATH: process.env.PATH, HOME: home, MULTICA_SERVER_URL: BASE, MULTICA_TOKEN: guestPat, MULTICA_WORKSPACE_ID: ws } });
  let out = cli("issue", "list", "--output", "json");
  const text = out.stdout + out.stderr;
  check("CLI", "multica issue list: EXT·EXTP 이슈만", out.status === 0 && text.includes(iExt) && !hidden.some((s) => text.includes(s)), `exit ${out.status} ${out.status ? out.stderr.slice(0, 120) : ""}`);
  out = cli("issue", "get", iExt, "--output", "json");
  check("CLI", "multica issue get EXT → 성공", out.status === 0 && out.stdout.includes(iExt), `exit ${out.status}`);
  out = cli("issue", "get", iPub, "--output", "json");
  check("CLI", "multica issue get PUB → 실패 (not found)", out.status !== 0 && /not found|404/i.test(out.stderr) && !out.stdout.includes(`PUB-SECRET`), `exit ${out.status} ${out.stderr.trim().slice(0, 80)}`);
  out = cli("agent", "list", "--output", "json");
  check("CLI", "multica agent list: 이름만, 지시문 없음", out.status === 0 && out.stdout.includes(agentPub) && !out.stdout.includes(`AGENT-INSTR-${tag}`), `exit ${out.status}`);
  out = cli("issue", "comment", "add", iExt, "--content", `cli comment ${tag}`);
  check("CLI", "multica issue comment add EXT → 성공", out.status === 0, `exit ${out.status} ${out.status ? out.stderr.trim().slice(0, 80) : ""}`);
  out = cli("issue", "create", "--title", `cli issue ${tag}`, "--project", pExt);
  check("CLI", "multica issue create → 거부 (upstream 범위)", out.status !== 0, `exit ${out.status} ${out.stderr.trim().slice(0, 80)}`);
  rmSync(home, { recursive: true, force: true });
}

// ---------- fork sidecars (lightsoft/secrets, lightsoft/usage) ----------
async function sidecar(base, method, path, token, body) {
  const res = await fetch(base.replace(/\/$/, "") + path, { method, headers: { Authorization: "Bearer " + token, "Content-Type": "application/json" }, body: body && JSON.stringify(body) });
  return { status: res.status, text: await res.text() };
}
if (process.env.SECRETS_URL) {
  const sec = (token, method = "GET", path = "/secrets", body) => sidecar(process.env.SECRETS_URL, method, `/lightsoft-secrets/api/w/${ws}${path}`, token, body);
  r = await sec(owner); check("사이드카", "시크릿: owner 목록 → 200", r.status === 200, `HTTP ${r.status}`);
  r = await sec(guest); check("사이드카", "시크릿: guest 목록 → 403", r.status === 403, `HTTP ${r.status} ${r.text.slice(0, 80)}`);
  r = await sec(guest, "GET", "/audit"); check("사이드카", "시크릿: guest 감사 기록 → 403", r.status === 403, `HTTP ${r.status}`);
  r = await sec(owner, "POST", "/secrets", { name: `GUEST_GRANT_${tag.toUpperCase()}`, value: "v", grants: [{ principal_type: "member", principal_id: guestMe.id }] });
  check("사이드카", "시크릿: guest에게 권한 주기 → 400", r.status === 400, `HTTP ${r.status} ${r.text.slice(0, 80)}`);
}
if (process.env.USAGE_URL) {
  const lim = (token) => sidecar(process.env.USAGE_URL, "GET", `/lightsoft-usage/api/w/${ws}/limits`, token);
  r = await lim(owner); check("사이드카", "구독 한도: owner → 200", r.status === 200, `HTTP ${r.status}`);
  r = await lim(guest); check("사이드카", "구독 한도: guest → 403", r.status === 403, `HTTP ${r.status} ${r.text.slice(0, 80)}`);
}

// ---------- narrow, promote, demote, remove ----------
const guestRow = async () => list((await call("GET", `/api/workspaces/${ws}/members`, { token: owner, ws, headers: { "X-Client-Capabilities": GUEST_CAPABILITY }, want: [200] })).json, "members").find((m) => m.user_id === guestMe.id);
let gm = await guestRow();
check("관리", "owner 목록에 guest와 열어 준 프로젝트가 보임", gm?.role === "guest" && gm.project_ids?.includes(pExt) && gm.project_ids?.includes(pExtP), `role=${gm?.role} projects=${gm?.project_ids?.length}`);
r = await O("PUT", `/api/workspaces/${ws}/members/${gm.id}/projects`, { project_ids: [pExtP] });
r = await G("GET", `/api/issues/${iExt}`); check("회수", "EXT를 닫으면 EXT 이슈 → 404", r.status === 404, `HTTP ${r.status}`);
r = await G("GET", `/api/issues/${iExtP}`); check("회수", "EXTP는 그대로 200", r.status === 200, `HTTP ${r.status}`);
r = await O("PUT", `/api/workspaces/${ws}/members/${gm.id}/projects`, { project_ids: [] }, [400]);
check("회수", "프로젝트를 전부 빼는 것은 400 (내보내기로)", r.status === 400, `HTTP ${r.status}`);

r = await O("PATCH", `/api/workspaces/${ws}/members/${gm.id}`, { role: "member" });
r = await G("GET", `/api/issues/${iPub}`); check("전환", "member로 올리면 PUB 이슈 → 200", r.status === 200, `HTTP ${r.status}`);
r = await O("PATCH", `/api/workspaces/${ws}/members/${gm.id}`, { role: "guest", project_ids: [pExt] });
r = await G("GET", `/api/issues/${iPub}`); check("전환", "다시 guest로 내리면 PUB 이슈 → 404", r.status === 404, `HTTP ${r.status}`);
r = await G("GET", `/api/issues/${iExt}`); check("전환", "내릴 때 고른 EXT는 200", r.status === 200, `HTTP ${r.status}`);

gm = await guestRow();
await O("DELETE", `/api/workspaces/${ws}/members/${gm.id}`);
r = await G("GET", "/api/issues"); check("회수", "워크스페이스에서 내보내면 → 4xx", r.status >= 400, `HTTP ${r.status}`);
r = await call("GET", "/api/issues", { token: guestPat, ws }); check("회수", "내보낸 뒤 PAT도 → 4xx", r.status >= 400, `HTTP ${r.status}`);

for (const x of results) console.log(`${x.ok ? "PASS" : "FAIL"}  [${x.area}] ${x.name}  — ${x.observed}`);
console.log(`\n${results.length - failed}/${results.length} passed (tag ${tag})`);
process.exit(failed ? 1 : 0);
