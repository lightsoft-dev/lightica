// Outside collaborator (guest) regression scenario (LIGH-282).
//
// Runs against a THROWAWAY fork backend, never production: it creates users,
// a workspace, agents and a fake daemon runtime, and needs the dev login code.
//
//   node lightsoft/guest/scenario.mjs http://127.0.0.1:18282
//
// The backend must run with APP_ENV=development and
// MULTICA_DEV_VERIFICATION_CODE=888888. Exits non-zero when a check fails.
const BASE = (process.argv[2] || "http://127.0.0.1:18282").replace(/\/$/, "");
if (/:(8080|3443|3444)\b/.test(BASE) || /lightica\.lightsoft\.dev/.test(BASE)) {
  console.error("refusing to run against what looks like production:", BASE);
  process.exit(2);
}
const tag = Math.random().toString(36).slice(2, 8);
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const results = [];
let failed = 0;

async function call(method, path, { body, token, ws, want } = {}) {
  const headers = { "Content-Type": "application/json" };
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

// ---------- owner setup ----------
const ownerEmail = `owner-${tag}@example.com`, guestEmail = `guest-${tag}@example.com`;
const owner = await login(ownerEmail);
const ws = (await call("POST", "/api/workspaces", { token: owner, body: { name: "Guest test", slug: "guest-" + tag }, want: [200, 201] })).json.id;
const pat = (await call("POST", "/api/tokens", { token: owner, body: { name: "t", expires_in_days: 1 }, want: [200, 201] })).json.token;
const O = (m, p, body, want = [200, 201, 204]) => call(m, p, { token: owner, ws, body, want });

const reg = (await call("POST", "/api/daemon/register", { token: pat, want: [200, 201], body: {
  workspace_id: ws, daemon_id: crypto.randomUUID(), device_name: "owner-mac", cli_version: "0.11.1",
  runtimes: [{ name: "Claude (owner)", type: "claude", version: "2.1.0", status: "online" }] } })).json;
const runtime = reg.runtimes[0].id;
const agentPub = (await O("POST", "/api/agents", { name: `Public agent ${tag}`, runtime_id: runtime, instructions: `AGENT-INSTR-${tag}`, permission_mode: "public_to", invocation_targets: [{ target_type: "workspace" }] })).json.id;
await O("POST", "/api/squads", { name: `Squad-${tag}`, leader_id: agentPub });

// The guest is listed BEFORE the invitation, so the account is a guest from its first request.
let r = await O("POST", "/api/lightsoft/guests", { email: guestEmail.toUpperCase() });
check("관리", "guest 목록에 추가 (대문자 입력은 소문자로 저장)", list(r.json, "guests").some((g) => g.email === guestEmail), JSON.stringify(r.json));
await O("POST", `/api/workspaces/${ws}/members`, { email: guestEmail, role: "member" });
const guest = await login(guestEmail);
const guestMe = (await call("GET", "/api/me", { token: guest, want: [200] })).json;
const inv = (await call("GET", "/api/invitations", { token: guest, want: [200] })).json;
await call("POST", `/api/invitations/${list(inv, "invitations")[0].id}/accept`, { token: guest, want: [200, 201] });
const G = (m, p, body) => call(m, p, { token: guest, ws, body });
const guestPat = (await call("POST", "/api/tokens", { token: guest, body: { name: "cli", expires_in_days: 1 }, want: [200, 201] })).json.token;

// Projects: EXT (workspace-visible, guest is a project member), EXTP (private, guest member),
// INT (private, no guest), PUB (workspace-visible, no guest), and an issue with no project.
const pExt = (await O("POST", "/api/projects", { title: `EXT-${tag}` })).json.id;
await O("POST", `/api/projects/${pExt}/members`, { user_ids: [guestMe.id] });
const pExtP = (await O("POST", "/api/projects", { title: `EXTP-${tag}`, visibility: "private", member_ids: [guestMe.id] })).json.id;
const pInt = (await O("POST", "/api/projects", { title: `INT-SECRET-${tag}`, visibility: "private" })).json.id;
const pPub = (await O("POST", "/api/projects", { title: `PUB-SECRET-${tag}` })).json.id;
const mk = async (title, project) => (await O("POST", "/api/issues", { title, description: `body of ${title}`, ...(project ? { project_id: project } : {}) })).json.id;
const iExt = await mk(`EXT issue ${tag}`, pExt);
const iExtP = await mk(`EXTP issue ${tag}`, pExtP);
const iInt = await mk(`INT-SECRET issue ${tag}`, pInt);
const iPub = await mk(`PUB-SECRET issue ${tag}`, pPub);
const iNone = await mk(`NONE-SECRET issue ${tag}`, null);
for (const i of [iExt, iExtP, iInt, iPub, iNone]) await O("POST", `/api/issues/${i}/comments`, { content: `comment ${tag}` });

const hidden = [pInt, pPub, iInt, iPub, iNone, `INT-SECRET-${tag}`, `PUB-SECRET-${tag}`, `NONE-SECRET`];
const leaks = (res) => hidden.filter((s) => res.text.includes(s));
const has = (res, s) => res.text.includes(s);

// ---------- what the guest sees ----------
r = await G("GET", "/api/lightsoft/capabilities");
check("화면", "capabilities.guest = true", r.status === 200 && r.json.guest === true && r.json.guest_management === false, r.text);
r = await O("GET", "/api/lightsoft/capabilities");
check("화면", "owner는 guest=false, guest_management=true", r.json.guest === false && r.json.guest_management === true, r.text);

r = await G("GET", "/api/projects");
check("프로젝트", "목록: EXT·EXTP만", has(r, pExt) && has(r, pExtP) && leaks(r).length === 0, `HTTP ${r.status} leaks=${leaks(r)}`);
for (const [n, id, want] of [["EXT", pExt, 200], ["EXTP", pExtP, 200], ["PUB", pPub, 404], ["INT", pInt, 404]]) {
  r = await G("GET", `/api/projects/${id}`); check("프로젝트", `${n} 상세 → ${want}`, r.status === want, `HTTP ${r.status}`);
}
r = await G("GET", `/api/projects/search?q=${tag}`);
check("검색", "프로젝트 검색에 PUB·INT 없음", leaks(r).length === 0, `HTTP ${r.status} leaks=${leaks(r)}`);

r = await G("GET", "/api/issues?limit=100");
check("이슈", "목록: EXT·EXTP 이슈만", has(r, iExt) && has(r, iExtP) && leaks(r).length === 0, `HTTP ${r.status} leaks=${leaks(r)}`);
for (const [n, id, want] of [["EXT", iExt, 200], ["PUB", iPub, 404], ["INT", iInt, 404], ["NONE", iNone, 404]]) {
  r = await G("GET", `/api/issues/${id}`); check("이슈", `${n} 상세 → ${want}`, r.status === want, `HTTP ${r.status}`);
}
r = await G("GET", `/api/issues/${iPub}/comments`); check("이슈", "PUB 코멘트 → 404", r.status === 404, `HTTP ${r.status}`);
r = await G("GET", `/api/issues/search?q=${tag}`);
check("검색", "이슈 검색에 PUB·INT·NONE 없음", leaks(r).length === 0 && has(r, iExt), `HTTP ${r.status} leaks=${leaks(r)}`);
r = await G("POST", "/api/issues/table/rows", { query: { scope: { kind: "workspace" }, sort: { field: "position", direction: "asc" } }, group: { kind: "none" }, hierarchy: { enabled: false }, page: { limit: 100 } });
check("이슈", "표 보기(table/rows)에 숨긴 이슈 없음", r.status === 200 && has(r, iExt) && leaks(r).length === 0, `HTTP ${r.status} leaks=${leaks(r)} ${r.status === 200 ? "" : r.text.slice(0, 120)}`);
r = await G("POST", "/api/issues/table/groups", { query: { scope: { kind: "workspace" }, sort: { field: "position", direction: "asc" } }, group: { kind: "project" }, page: { limit: 100 } });
check("이슈", "표 보기 그룹(table/groups)에 숨긴 프로젝트 없음", r.status === 200 && leaks(r).length === 0, `HTTP ${r.status} leaks=${leaks(r)} ${r.status === 200 ? "" : r.text.slice(0, 120)}`);

r = await G("GET", "/api/agents");
check("에이전트", "목록은 이름만 (지시문 없음)", r.status === 200 && has(r, `Public agent ${tag}`) && !has(r, `AGENT-INSTR-${tag}`), `HTTP ${r.status}`);
r = await G("GET", `/api/agents/${agentPub}`); check("에이전트", "상세 → 403", r.status === 403, `HTTP ${r.status}`);
r = await G("GET", `/api/workspaces/${ws}/members`);
const meRow = list(r.json, "members").find((m) => m.user_id === guestMe.id);
check("멤버", "목록에 이메일 없음, 본인은 role=guest", r.status === 200 && !has(r, ownerEmail) && meRow?.role === "guest", `HTTP ${r.status} role=${meRow?.role}`);
r = await G("GET", `/api/workspaces/${ws}`);
check("워크스페이스", "상세는 되지만 guest 목록·context 없음", r.status === 200 && !has(r, guestEmail) && r.json.context == null, `HTTP ${r.status}`);
r = await O("GET", `/api/workspaces/${ws}`);
check("워크스페이스", "owner 응답에도 settings.lightsoft_guests 없음", !has(r, "lightsoft_guests"), `HTTP ${r.status}`);

for (const p of [`/api/workspaces/${ws}/invitations`, "/api/dashboard/usage/by-agent?days=30", `/api/workspaces/${ws}/runtime-profiles`,
  `/api/workspaces/${ws}/mcp-servers`, "/api/lightsoft/guests", `/api/chat/sessions/${crypto.randomUUID()}`, "/api/docs", `/api/projects/${pExt}/join`]) {
  r = await G("GET", p); check("차단", `GET ${p.replace(ws, "{ws}")} → 403`, r.status === 403, `HTTP ${r.status}`);
}
// Lists every page prefetches answer empty for a guest, so the shell renders.
for (const p of ["/api/runtimes", "/api/autopilots", "/api/skills", "/api/agent-task-snapshot", "/api/chat/sessions", "/api/working-agents",
  `/api/issues/${iExt}/task-runs`, "/api/quick-actions", `/api/issues/${iExt}/active-task`]) {
  r = await G("GET", p);
  check("빈 응답", `GET ${p} → 200, 내용 없음`, r.status === 200 && !has(r, agentPub) && !has(r, runtime) && !/"id"/.test(r.text), `HTTP ${r.status} ${r.text.slice(0, 60)}`);
}
r = await G("GET", "/api/search-index/manifest"); check("빈 응답", "로컬 검색 인덱스 → 404 (서버 검색으로)", r.status === 404, `HTTP ${r.status}`);
for (const p of ["/api/labels", "/api/properties", "/api/issue-statuses", "/api/issue-workflows", "/api/inbox", "/api/squads"]) {
  r = await G("GET", p); check("허용", `GET ${p} → 200`, r.status === 200, `HTTP ${r.status}`);
}

// ---------- writes ----------
r = await G("POST", "/api/projects", { title: `guest-made-${tag}` }); check("쓰기", "프로젝트 생성 → 403", r.status === 403, `HTTP ${r.status}`);
r = await G("POST", "/api/issues", { title: `guest no project ${tag}` }); check("쓰기", "프로젝트 없는 이슈 생성 → 400", r.status === 400, `HTTP ${r.status}`);
r = await G("POST", "/api/issues", { title: `guest into PUB ${tag}`, project_id: pPub }); check("쓰기", "PUB에 이슈 생성 → 400", r.status === 400, `HTTP ${r.status}`);
r = await G("POST", "/api/issues", { title: `guest issue ${tag}`, project_id: pExt });
const iGuest = r.json?.id;
check("쓰기", "EXT에 이슈 생성 → 201", r.status === 201 || r.status === 200, `HTTP ${r.status}`);
r = await G("PUT", `/api/issues/${iExt}`, { title: `EXT edited by guest ${tag}` }); check("쓰기", "EXT 이슈 수정 → 200", r.status === 200, `HTTP ${r.status}`);
r = await G("PUT", `/api/issues/${iExt}`, { project_id: null }); check("쓰기", "EXT 이슈를 프로젝트 밖으로 → 400", r.status === 400, `HTTP ${r.status}`);
r = await G("PUT", `/api/issues/${iExt}`, { project_id: pPub }); check("쓰기", "EXT 이슈를 PUB로 이동 → 400", r.status === 400, `HTTP ${r.status}`);
r = await G("PUT", `/api/issues/${iNone}`, { title: "x" }); check("쓰기", "NONE 이슈 수정 → 404", r.status === 404, `HTTP ${r.status}`);
r = await G("POST", `/api/issues/${iExt}/comments`, { content: `guest comment ${tag}` }); check("쓰기", "EXT 코멘트 → 201", r.status === 201 || r.status === 200, `HTTP ${r.status}`);
r = await G("POST", `/api/issues/${iNone}/comments`, { content: "x" }); check("쓰기", "NONE 코멘트 → 404", r.status === 404, `HTTP ${r.status}`);
r = await G("PATCH", `/api/workspaces/${ws}`, { settings: {} }); check("쓰기", "워크스페이스 설정 변경 → 403", r.status === 403, `HTTP ${r.status}`);
r = await G("POST", `/api/workspaces/${ws}/members`, { email: `friend-${tag}@example.com`, role: "member" }); check("쓰기", "초대 → 403", r.status === 403, `HTTP ${r.status}`);
r = await G("POST", "/api/lightsoft/guests", { email: `x-${tag}@example.com` }); check("쓰기", "guest 목록 변경 → 403", r.status === 403, `HTTP ${r.status}`);

// ---------- agent invocation ----------
r = await G("POST", "/api/issues", { title: `guest assigns agent ${tag}`, project_id: pExt, assignee_type: "agent", assignee_id: agentPub });
check("에이전트 호출", "에이전트에 할당한 이슈 생성 → 403", r.status === 403, `HTTP ${r.status}`);
r = await G("PUT", `/api/issues/${iExt}`, { assignee_type: "agent", assignee_id: agentPub });
check("에이전트 호출", "기존 이슈를 에이전트에 할당 → 403", r.status === 403, `HTTP ${r.status}`);
r = await G("POST", "/api/chat/sessions", { agent_id: agentPub, title: "guest chat" }); check("에이전트 호출", "채팅 → 403", r.status === 403, `HTTP ${r.status}`);
r = await call("POST", "/api/daemon/register", { token: guestPat, body: { workspace_id: ws, daemon_id: crypto.randomUUID(), device_name: "guest-mac", cli_version: "0.11.1", runtimes: [{ name: "x", type: "claude", version: "2", status: "online" }] } });
check("에이전트 호출", "guest PAT로 데몬 등록 → 403", r.status === 403, `HTTP ${r.status}`);

// The owner hands an EXT issue to the agent; a guest comment or mention must not start a run.
const iRun = (await O("POST", "/api/issues", { title: `owner assigns ${tag}`, project_id: pExt, assignee_type: "agent", assignee_id: agentPub })).json.id;
await sleep(500);
const runs = async () => list((await O("GET", `/api/issues/${iRun}/task-runs`)).json, "tasks").length;
const queued = async () => (await O("GET", `/api/issues/${iRun}/task-runs`)).json;
// Settle the owner's own run so a new one is not coalesced into it.
for (const t of list(await queued(), "tasks")) await O("POST", `/api/issues/${iRun}/tasks/${t.id}/cancel`, undefined, [200, 204, 400, 404, 409]);
await sleep(500);
const before = await runs();
r = await G("POST", `/api/issues/${iRun}/comments`, { content: "please do X" });
await sleep(800);
const afterComment = await runs();
const mention = await G("POST", `/api/issues/${iRun}/comments`, { content: `[@Public agent](mention://agent/${agentPub}) do Y` });
await sleep(800);
const afterMention = await runs();
check("에이전트 호출", "guest 코멘트·멘션으로 task 안 생김", afterComment === before && afterMention === before,
  `task ${before}→${afterComment}→${afterMention} (comment HTTP ${r.status}, mention HTTP ${mention.status})`);
await O("POST", `/api/issues/${iRun}/comments`, { content: `[@Public agent](mention://agent/${agentPub}) owner asks` });
await sleep(800);
check("에이전트 호출", "대조군: owner 멘션은 task 생성", (await runs()) > afterMention, `task → ${await runs()}`);

// A workflow step that hands issues to the agent: a guest moving an issue into it starts no run.
const wfs = list((await O("GET", "/api/issue-workflows")).json, "workflows");
const wf = wfs.find((w) => w.is_default) || wfs[0];
r = await O("PUT", `/api/issue-workflows/${wf.id}/handoffs/in_review`, { status_key: "in_review", handler: { type: "agent", id: agentPub }, instructions: "review it" }, [200, 201, 204, 400, 404, 422]);
const handoffSet = r.status < 300;
const iFlow = (await O("POST", "/api/issues", { title: `workflow ${tag}`, project_id: pExt })).json.id;
r = await G("PUT", `/api/issues/${iFlow}`, { status: "in_review" });
await sleep(800);
const flowRuns = list((await O("GET", `/api/issues/${iFlow}/task-runs`)).json, "tasks").length;
const flowIssue = (await O("GET", `/api/issues/${iFlow}`)).json;
check("에이전트 호출", "guest가 인계 단계로 옮겨도 task 안 생김", handoffSet && flowRuns === 0,
  `handoff=${handoffSet} status HTTP ${r.status} → ${flowIssue.status}, assignee=${flowIssue.assignee_type || "-"}, tasks=${flowRuns}`);
r = await G("GET", `/api/issues/${iFlow}/workflow-handoff?status=in_review`);
check("에이전트 호출", "인계 미리보기는 비어 있음 (에이전트 노출 없음)", r.status === 200 && r.json.handoff === false && !has(r, agentPub), `HTTP ${r.status}`);

// Independent review findings (LIGH-282): other id spellings, junk workspace ids, backlog promotion, key case.
const WSU = ws.toUpperCase();
const GU = (m, p, body) => call(m, p, { token: guest, ws: WSU, body });
r = await GU("GET", `/api/agents/${agentPub}`); check("우회", "대문자 워크스페이스 ID로 에이전트 상세 → 403", r.status === 403, `HTTP ${r.status}`);
r = await GU("POST", "/api/chat/sessions", { agent_id: agentPub, title: "x" }); check("우회", "대문자 워크스페이스 ID로 채팅 → 403", r.status === 403, `HTTP ${r.status}`);
r = await GU("GET", `/api/issues/${iNone}`); check("우회", "대문자 워크스페이스 ID로 NONE 이슈 → 404", r.status === 404, `HTTP ${r.status}`);
r = await call("GET", `/api/workspaces/${WSU}/members`, { token: guest });
check("우회", "대문자 경로로 멤버 목록도 이메일 없음", r.status >= 400 || !has(r, ownerEmail), `HTTP ${r.status}`);
r = await call("POST", "/api/slack/binding/redeem", { token: guest, ws: "x", body: { token: "t" } }); check("우회", "엉뚱한 워크스페이스 ID로 메신저 바인딩 → 403", r.status === 403, `HTTP ${r.status}`);
r = await call("GET", "/api/runtimes", { token: guest, ws: crypto.randomUUID() }); check("우회", "남의 워크스페이스 ID로 런타임 → 403", r.status === 403, `HTTP ${r.status}`);
r = await G("PUT", `/api/issues/${iExt}`, { project_id: pExt, PROJECT_ID: null }); check("우회", "대소문자 다른 project_id 키 → 400", r.status === 400, `HTTP ${r.status}`);
r = await G("GET", `/api/workspaces/${ws}`); check("우회", "워크스페이스 설정은 비어 있음", r.status === 200 && JSON.stringify(r.json.settings) === "{}", `settings=${JSON.stringify(r.json?.settings)}`);
const iBacklog = (await O("POST", "/api/issues", { title: `backlog agent ${tag}`, project_id: pExt, status: "backlog", assignee_type: "agent", assignee_id: agentPub })).json.id;
await sleep(500);
const backlogRuns = async () => list((await O("GET", `/api/issues/${iBacklog}/task-runs`)).json, "tasks").length;
const b0 = await backlogRuns();
r = await G("PUT", `/api/issues/${iBacklog}`, { status: "todo" });
await sleep(800);
const b1 = await backlogRuns();
check("에이전트 호출", "에이전트 담당 backlog 이슈를 guest가 todo로 → task 안 생김", b1 === b0, `status HTTP ${r.status}, tasks ${b0}→${b1}`);
const iBacklog2 = (await O("POST", "/api/issues", { title: `backlog agent owner ${tag}`, project_id: pExt, status: "backlog", assignee_type: "agent", assignee_id: agentPub })).json.id;
await O("PUT", `/api/issues/${iBacklog2}`, { status: "todo" });
await sleep(800);
const ownerRuns = list((await O("GET", `/api/issues/${iBacklog2}/task-runs`)).json, "tasks").length;
check("에이전트 호출", "대조군: owner가 todo로 옮기면 task 생성", ownerRuns > 0, `tasks=${ownerRuns}`);

// ---------- inbox ----------
await O("POST", `/api/issues/${iPub}/comments`, { content: `hey [@guest](mention://member/${guestMe.id}) PUB-SECRET-MENTION-${tag}` });
await O("POST", `/api/issues/${iExt}/comments`, { content: `hey [@guest](mention://member/${guestMe.id}) EXT-MENTION-${tag}` });
await sleep(1200);
r = await G("GET", "/api/inbox");
check("인박스", "EXT 멘션은 오고 PUB 멘션은 안 옴", has(r, iExt) && !has(r, iPub) && !has(r, `PUB-SECRET-MENTION-${tag}`), `HTTP ${r.status}`);

// ---------- realtime ----------
const events = [];
const sock = new WebSocket(BASE.replace(/^http/, "ws") + `/ws?workspace_id=${ws}`);
await new Promise((res, rej) => { sock.onopen = res; sock.onerror = rej; });
sock.onmessage = (ev) => events.push(String(ev.data));
sock.send(JSON.stringify({ type: "auth", payload: { token: guest } }));
await sleep(1000);
await O("PUT", `/api/issues/${iExt}`, { title: `EXT-LIVE-${tag}` });
await O("PUT", `/api/issues/${iPub}`, { title: `PUB-SECRET-LIVE-${tag}` });
await O("PUT", `/api/issues/${iInt}`, { title: `INT-SECRET-LIVE-${tag}` });
await O("PUT", `/api/issues/${iNone}`, { title: `NONE-SECRET-LIVE-${tag}` });
await O("POST", `/api/issues/${iPub}/comments`, { content: `PUB-SECRET-LIVE-COMMENT-${tag}` });
await O("PUT", `/api/agents/${agentPub}`, { description: `AGENT-LIVE-${tag}` });
await mk(`NONE-SECRET live new ${tag}`, null);
await sleep(2500);
sock.close();
const wsLeak = events.filter((e) => hidden.some((s) => e.includes(s)) || e.includes(`AGENT-LIVE-${tag}`));
check("실시간", "EXT 변경은 받음", events.some((e) => e.includes(`EXT-LIVE-${tag}`)), `${events.length} events`);
check("실시간", "PUB·INT·NONE·에이전트 변경은 안 받음", wsLeak.length === 0, `leaks=${wsLeak.length} types=${[...new Set(events.map((e) => { try { return JSON.parse(e).type; } catch { return "?"; } }))]}`);

// ---------- settings writes keep the guest list ----------
await O("PATCH", `/api/workspaces/${ws}`, { settings: {} });
r = await G("GET", "/api/lightsoft/capabilities");
check("관리", "설정 저장 뒤에도 guest 유지", r.json?.guest === true, r.text);

// ---------- CLI-style access with the guest PAT ----------
r = await call("GET", "/api/issues?limit=100", { token: guestPat, ws });
check("CLI", "PAT로도 EXT 이슈만", r.status === 200 && has(r, iExt) && leaks(r).length === 0, `HTTP ${r.status}`);
r = await call("GET", "/api/agents/" + agentPub, { token: guestPat, ws });
check("CLI", "PAT로 에이전트 상세 → 403", r.status === 403, `HTTP ${r.status}`);

// ---------- revoke ----------
r = await O("DELETE", `/api/lightsoft/guests?email=${encodeURIComponent(guestEmail)}`, undefined, [200, 409]);
check("회수", "멤버인 guest는 목록에서 바로 못 뺌 (409)", r.status === 409, `HTTP ${r.status}`);
await O("DELETE", `/api/projects/${pExt}/members/${guestMe.id}`);
r = await G("GET", `/api/issues/${iExt}`); check("회수", "EXT 멤버 제거 뒤 EXT 이슈 → 404", r.status === 404, `HTTP ${r.status}`);
r = await G("GET", `/api/issues/${iGuest}`); check("회수", "guest가 만든 EXT 이슈도 → 404", r.status === 404, `HTTP ${r.status}`);
const members = list((await O("GET", `/api/workspaces/${ws}/members`)).json, "members");
const gm = members.find((m) => m.user_id === guestMe.id);
await O("DELETE", `/api/workspaces/${ws}/members/${gm.id}`);
r = await G("GET", "/api/issues"); check("회수", "워크스페이스에서 제거 뒤 → 4xx", r.status >= 400, `HTTP ${r.status}`);
r = await O("GET", "/api/lightsoft/guests");
check("회수", "제거 뒤에도 목록에 남아 재초대 시 다시 guest", list(r.json, "guests").some((g) => g.email === guestEmail && g.member_id == null), r.text.slice(0, 200));
r = await O("DELETE", `/api/lightsoft/guests?email=${encodeURIComponent(guestEmail)}`);
check("회수", "멤버가 아니면 목록에서 삭제 → 200", r.status === 200, `HTTP ${r.status}`);

for (const x of results) console.log(`${x.ok ? "PASS" : "FAIL"}  [${x.area}] ${x.name}  — ${x.observed}`);
console.log(`\n${results.length - failed}/${results.length} passed (tag ${tag})`);
process.exit(failed ? 1 : 0);
