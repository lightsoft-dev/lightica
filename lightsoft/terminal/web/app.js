"use strict";

(() => {
  const PREFIX = "/lightsoft-term";
  const projectId = decodeURIComponent(location.pathname.split("/").pop() || "");
  const workspaceId = new URLSearchParams(location.search).get("ws") || "";
  const query = `ws=${encodeURIComponent(workspaceId)}&project=${encodeURIComponent(projectId)}`;

  const $ = (id) => document.getElementById(id);
  const statusEl = $("status");
  const noticeEl = $("notice");
  const closeBtn = $("close");
  const reconnectBtn = $("reconnect");

  const SOURCE_LABEL = {
    local_directory: "프로젝트 local_directory",
    description: "프로젝트 설명의 로컬 폴더",
    home: "연결된 폴더가 없어 홈 폴더",
  };

  function setStatus(text, kind) {
    statusEl.textContent = text;
    statusEl.className = `status ${kind || ""}`;
  }

  function notice(text, isError) {
    noticeEl.textContent = text;
    noticeEl.className = isError ? "error" : "";
    noticeEl.hidden = !text;
  }

  const term = new Terminal({
    cursorBlink: true,
    fontFamily: 'Menlo, Monaco, "D2Coding", "Apple SD Gothic Neo", monospace',
    fontSize: 13,
    scrollback: 5000,
    theme: { background: "#0f1115", foreground: "#d7dae0" },
  });
  const fit = new FitAddon.FitAddon();
  term.loadAddon(fit);
  term.open($("terminal"));
  fit.fit();
  window.addEventListener("resize", () => fit.fit());

  const encoder = new TextEncoder();
  let sock = null;
  let live = false;

  function send(data) {
    if (sock && sock.readyState === WebSocket.OPEN) sock.send(data);
  }
  term.onData((d) => send(encoder.encode(d)));
  term.onBinary((d) => send(Uint8Array.from(d, (c) => c.charCodeAt(0))));
  term.onResize(({ cols, rows }) => send(JSON.stringify({ type: "resize", cols, rows })));

  window.addEventListener("beforeunload", (e) => {
    if (live) {
      e.preventDefault();
      e.returnValue = "";
    }
  });

  closeBtn.addEventListener("click", () => {
    if (sock) sock.close(1000, "closed by user");
  });
  reconnectBtn.addEventListener("click", () => {
    term.reset();
    notice("");
    connect();
  });

  function ended(text) {
    live = false;
    setStatus(text, "ended");
    closeBtn.disabled = true;
    reconnectBtn.hidden = false;
  }

  function connect() {
    reconnectBtn.hidden = true;
    setStatus("연결 중…");
    const proto = location.protocol === "https:" ? "wss" : "ws";
    sock = new WebSocket(
      `${proto}://${location.host}${PREFIX}/ws?${query}&cols=${term.cols}&rows=${term.rows}`,
    );
    sock.binaryType = "arraybuffer";
    let ready = false;
    let endedMessage = "";

    sock.onmessage = (e) => {
      if (typeof e.data !== "string") {
        term.write(new Uint8Array(e.data));
        return;
      }
      let msg;
      try {
        msg = JSON.parse(e.data);
      } catch {
        return;
      }
      if (msg.type === "ready") {
        ready = true;
        live = true;
        closeBtn.disabled = false;
        setStatus("연결됨", "live");
        term.focus();
      } else if (msg.type === "closing") {
        endedMessage = msg.message;
      } else if (msg.type === "exit") {
        endedMessage = `셸이 종료됐습니다 (exit ${msg.code}).`;
      } else if (msg.type === "error") {
        endedMessage = msg.message;
      }
    };
    sock.onclose = () => {
      if (!ready) {
        notice(
          endedMessage ||
            "터미널을 열지 못했습니다. 동시에 열 수 있는 터미널 수를 넘었거나 권한이 없을 수 있습니다.",
          true,
        );
        ended("연결 실패");
        return;
      }
      if (endedMessage) notice(endedMessage);
      ended("종료됨");
    };
  }

  async function start() {
    if (!projectId || !workspaceId) {
      notice("프로젝트 화면의 메뉴에서 터미널을 열어 주세요.", true);
      ended("열 수 없음");
      reconnectBtn.hidden = true;
      return;
    }
    let info;
    try {
      const res = await fetch(`${PREFIX}/api/info?${query}`, { credentials: "same-origin" });
      info = await res.json();
      if (!res.ok) throw new Error(info.error || `HTTP ${res.status}`);
    } catch (err) {
      notice(err.message, true);
      ended("열 수 없음");
      reconnectBtn.hidden = true;
      return;
    }
    document.title = `터미널 · ${info.project.title}`;
    $("project").textContent = info.project.title;
    $("dir").textContent = info.dir;
    $("dir").title = `시작 폴더: ${SOURCE_LABEL[info.dir_source] || info.dir_source}`;
    if (info.dir_source === "home") {
      notice("이 프로젝트에 연결된 로컬 폴더가 없어 홈 폴더에서 시작합니다. 프로젝트 설명에 \"로컬: ~/폴더\" 줄을 넣으면 그 폴더에서 열립니다.");
    }
    const idleMin = Math.round(info.idle_timeout_seconds / 60);
    term.writeln(
      `\x1b[2m${info.user.email} · 탭을 닫거나 ${idleMin}분 동안 입출력이 없으면 셸이 종료됩니다.\x1b[0m`,
    );
    connect();
  }

  start();
})();
