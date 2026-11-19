#!/usr/bin/env node
// Lightica release feed (LIGH-191): the folder lightsoft/desktop/feed serves to
// the tailnet and installed apps read through electron-updater's generic
// provider. lightsoft/desktop/release.sh drives it; see lightsoft/desktop/README.md.
//
//   release-feed.mjs versions <feed> [--arch arm64|x64]
//       Published versions, one per line (input for lightsoft/version.sh --taken).
//       Per arch, so an Intel build the same day reuses the Apple silicon version.
//   release-feed.mjs publish <feed> --dist <dir> --version V --arch arm64|x64
//       --team TEAMID --upstream vX.Y.Z --commit SHA [--keep N] [--base-url URL]
//       Copy the build into the feed, record it in releases.json, rewrite
//       index.html, then switch latest-mac.yml to it.
//   release-feed.mjs issue-section <feed> [--base-url URL]
//       The "현재 배포 파일" section of the install issue (LIGH-116), markdown.
//   release-feed.mjs issue-description --current FILE --section FILE
//       FILE's description with that section replaced (appended when missing).
//
// Folder layout (flat: latest-mac.yml names its files relative to the folder):
//   latest-mac.yml, latest-x64-mac.yml   electron-updater metadata
//   lightica-<version>-mac-<arch>.{dmg,zip}[.blockmap]
//   releases.json                        release history, newest first
//   index.html                           release page for people
import { createHash } from "node:crypto";
import { copyFileSync, existsSync, readFileSync, renameSync, statSync, unlinkSync, writeFileSync } from "node:fs";
import { basename, join } from "node:path";
import { fileURLToPath } from "node:url";

export const DEFAULT_BASE_URL = "https://mac-studio.taile83671.ts.net:3444/lightica/";
// Must match LIGHTICA_FEED_SIGNING_TEAM_KEY in apps/desktop/src/shared/lightica.ts.
export const SIGNING_TEAM_KEY = "lighticaSigningTeam";
export const ISSUE_SECTION_HEADING = "## 현재 배포 파일";
const DEFAULT_KEEP = 5;
const ARCH_LABEL = { arm64: "Apple silicon", x64: "Intel" };

export function channelFile(arch) {
  if (arch === "arm64") return "latest-mac.yml";
  if (arch === "x64") return "latest-x64-mac.yml"; // scripts/package.mjs publish.channel
  throw new Error(`unknown arch: ${arch}`);
}

export function artifactNames(version, arch) {
  const stem = `lightica-${version}-mac-${arch}`;
  return { dmg: `${stem}.dmg`, zip: `${stem}.zip`, blockmaps: [`${stem}.dmg.blockmap`, `${stem}.zip.blockmap`] };
}

export function readReleases(feed) {
  const path = join(feed, "releases.json");
  if (!existsSync(path)) return [];
  const data = JSON.parse(readFileSync(path, "utf-8"));
  return Array.isArray(data.releases) ? data.releases : [];
}

/** Adds the signing team to electron-builder's latest-mac.yml (see lightica-update.ts). */
export function withSigningTeam(yml, team) {
  const kept = yml.split("\n").filter((line) => !line.startsWith(`${SIGNING_TEAM_KEY}:`)).join("\n");
  if (!team) return kept;
  return `${kept.replace(/\n*$/, "\n")}${SIGNING_TEAM_KEY}: ${team}\n`;
}

export function ymlVersion(yml) {
  return /^version:\s*['"]?([^'"\s]+)/m.exec(yml)?.[1] ?? null;
}

function sha256(path) {
  return createHash("sha256").update(readFileSync(path)).digest("hex");
}

// Write next to the target under a dot name (lightica-feed never serves those),
// then rename, so a reader sees the old file or the new one, never half of one.
function writeAtomic(path, data) {
  const staged = join(path, "..", `.${basename(path)}.tmp`);
  writeFileSync(staged, data);
  renameSync(staged, path);
}

function copyAtomic(from, to) {
  const staged = join(to, "..", `.${basename(to)}.tmp`);
  copyFileSync(from, staged);
  renameSync(staged, to);
}

/** Records newest first; older releases beyond `keep` versions lose their files. */
export function planReleases(releases, record, keep = DEFAULT_KEEP) {
  const rest = releases.filter((r) => !(r.version === record.version && r.arch === record.arch));
  const next = [record, ...rest];
  const keptVersions = [...new Set(next.map((r) => r.version))].slice(0, keep);
  const prune = [];
  const result = next.map((r) => {
    if (r.pruned || keptVersions.includes(r.version)) return r;
    prune.push(...r.files.map((f) => f.name));
    return { ...r, pruned: true };
  });
  return { releases: result, prune };
}

export function publish(feed, { dist, version, arch, team, upstream, commit, keep = DEFAULT_KEEP, baseUrl = DEFAULT_BASE_URL, now = new Date() }) {
  const names = artifactNames(version, arch);
  const ymlName = channelFile(arch);
  const ymlPath = join(dist, ymlName);
  for (const required of [names.dmg, names.zip, ymlName]) {
    if (!existsSync(join(dist, required))) throw new Error(`missing ${required} in ${dist}`);
  }
  const yml = readFileSync(ymlPath, "utf-8");
  if (ymlVersion(yml) !== version) throw new Error(`${ymlName} is for ${ymlVersion(yml)}, not ${version}`);

  const artifacts = [names.dmg, names.zip, ...names.blockmaps].filter((name) => existsSync(join(dist, name)));
  for (const name of artifacts) copyAtomic(join(dist, name), join(feed, name));

  const record = {
    version,
    arch,
    publishedAt: now.toISOString(),
    upstream,
    commit,
    signingTeam: team || null,
    files: artifacts.map((name) => ({ name, size: statSync(join(feed, name)).size, sha256: sha256(join(feed, name)) })),
  };
  const plan = planReleases(readReleases(feed), record, keep);
  writeAtomic(join(feed, "releases.json"), `${JSON.stringify({ releases: plan.releases }, null, 2)}\n`);
  writeAtomic(join(feed, "index.html"), renderIndex(plan.releases, baseUrl));
  // Last: this is what installed apps read.
  writeAtomic(join(feed, ymlName), withSigningTeam(yml, team));
  for (const name of plan.prune) if (existsSync(join(feed, name))) unlinkSync(join(feed, name));
  return record;
}

function escapeHtml(value) {
  return String(value).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}

function seoulDate(iso) {
  return new Date(iso).toLocaleString("sv-SE", { timeZone: "Asia/Seoul" }).slice(0, 16);
}

/** Newest published (not pruned) release per arch. */
export function currentReleases(releases) {
  const seen = new Set();
  return releases.filter((r) => !r.pruned && !seen.has(r.arch) && seen.add(r.arch));
}

export function renderIndex(releases, baseUrl = DEFAULT_BASE_URL) {
  const url = (name) => escapeHtml(new URL(name, baseUrl).toString());
  const downloads = currentReleases(releases)
    .map((r) => {
      const dmg = r.files.find((f) => f.name.endsWith(".dmg"));
      return dmg
        ? `<li><a href="${url(dmg.name)}">${escapeHtml(dmg.name)}</a> (${escapeHtml(ARCH_LABEL[r.arch] ?? r.arch)})<br><code>SHA-256 ${escapeHtml(dmg.sha256)}</code></li>`
        : "";
    })
    .join("\n");
  const rows = releases
    .map((r) => {
      const files = r.pruned
        ? "정리됨"
        : r.files
            .filter((f) => !f.name.endsWith(".blockmap"))
            .map((f) => `<a href="${url(f.name)}">${escapeHtml(f.name.split(".").pop())}</a>`)
            .join(" ");
      return `<tr><td><code>${escapeHtml(r.version)}</code></td><td>${escapeHtml(ARCH_LABEL[r.arch] ?? r.arch)}</td><td>${escapeHtml(seoulDate(r.publishedAt))}</td><td>${escapeHtml(r.upstream)} · <code>${escapeHtml(String(r.commit).slice(0, 9))}</code></td><td>${files}</td></tr>`;
    })
    .join("\n");
  return `<!doctype html>
<html lang="ko">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Lightica 릴리스</title>
<style>
body { font: 15px/1.6 -apple-system, BlinkMacSystemFont, sans-serif; max-width: 860px; margin: 40px auto; padding: 0 20px; color: #1d1d1f; }
code { font: 13px ui-monospace, SFMono-Regular, Menlo, monospace; }
table { border-collapse: collapse; width: 100%; } td, th { text-align: left; padding: 6px 8px; border-bottom: 1px solid #e5e5e5; }
li { margin-bottom: 8px; }
</style>
</head>
<body>
<h1>Lightica 릴리스</h1>
<h2>지금 버전</h2>
<ul>
${downloads || "<li>아직 게시된 버전이 없습니다.</li>"}
</ul>
<p>dmg를 열어 Lightica를 Applications로 옮깁니다. 이미 설치돼 있으면 Lightica를 종료하고 덮어씁니다. 로그인과 설정은 그대로 남습니다. 설치한 Lightica는 새 버전이 나오면 알려 줍니다.</p>
<h2>기록</h2>
<table>
<tr><th>버전</th><th>맥</th><th>게시(KST)</th><th>기준</th><th>파일</th></tr>
${rows}
</table>
</body>
</html>
`;
}

export function renderIssueSection(releases, baseUrl = DEFAULT_BASE_URL) {
  const current = currentReleases(releases);
  if (current.length === 0) return `${ISSUE_SECTION_HEADING}\n\n아직 게시된 버전이 없습니다.\n`;
  const [latest] = current;
  const lines = [
    ISSUE_SECTION_HEADING,
    "",
    `**${latest.version}** · ${seoulDate(latest.publishedAt)} KST 게시 · upstream ${latest.upstream} + 팀 기능(\`lightsoft/dev\` \`${String(latest.commit).slice(0, 9)}\`)`,
    "",
    `- 받는 곳: ${baseUrl} (Tailscale 필요). 설치한 Lightica는 새 버전이 나오면 알려 줍니다.`,
  ];
  for (const r of current) {
    const dmg = r.files.find((f) => f.name.endsWith(".dmg"));
    if (dmg) lines.push(`- ${ARCH_LABEL[r.arch] ?? r.arch}: [\`${dmg.name}\`](${new URL(dmg.name, baseUrl)}) · SHA-256 \`${dmg.sha256}\``);
  }
  lines.push(`- 서명: ${latest.signingTeam ? `Developer ID(팀 \`${latest.signingTeam}\`), Apple 공증` : "Developer ID 아님"}`);
  lines.push("", "받은 파일은 `shasum -a 256 <파일>`로 대조할 수 있습니다. 게시 절차는 레포의 `lightsoft/desktop/README.md`에 있습니다.", "");
  return lines.join("\n");
}

/** Replaces the section from its heading to the next `## ` heading. */
export function replaceIssueSection(description, section) {
  const lines = description.split("\n");
  const start = lines.findIndex((line) => line.trim() === ISSUE_SECTION_HEADING);
  const body = section.replace(/\n*$/, "");
  if (start === -1) return `${description.replace(/\n*$/, "")}\n\n${body}\n`;
  let end = lines.findIndex((line, i) => i > start && line.startsWith("## "));
  if (end === -1) end = lines.length;
  const after = lines.slice(end);
  return [...lines.slice(0, start), body, ...(after.length ? ["", ...after] : [])].join("\n").replace(/\n*$/, "\n");
}

function parseFlags(argv) {
  const flags = {};
  const positional = [];
  for (let i = 0; i < argv.length; i += 1) {
    if (argv[i].startsWith("--")) {
      flags[argv[i].slice(2)] = argv[i + 1];
      i += 1;
    } else positional.push(argv[i]);
  }
  return { flags, positional };
}

function main() {
  const [command, ...rest] = process.argv.slice(2);
  const { flags, positional } = parseFlags(rest);
  const feed = positional[0];
  const baseUrl = flags["base-url"] ?? DEFAULT_BASE_URL;
  switch (command) {
    case "versions":
      for (const r of readReleases(feed)) if (!flags.arch || r.arch === flags.arch) console.log(r.version);
      return;
    case "publish": {
      for (const key of ["dist", "version", "arch", "upstream", "commit"]) {
        if (!flags[key]) throw new Error(`publish needs --${key}`);
      }
      const record = publish(feed, {
        dist: flags.dist,
        version: flags.version,
        arch: flags.arch,
        team: flags.team,
        upstream: flags.upstream,
        commit: flags.commit,
        keep: flags.keep ? Number(flags.keep) : DEFAULT_KEEP,
        baseUrl,
      });
      console.log(JSON.stringify(record, null, 2));
      return;
    }
    case "issue-section":
      process.stdout.write(renderIssueSection(readReleases(feed), baseUrl));
      return;
    case "issue-description":
      process.stdout.write(replaceIssueSection(readFileSync(flags.current, "utf-8"), readFileSync(flags.section, "utf-8")));
      return;
    default:
      throw new Error(`usage: release-feed.mjs versions|publish|issue-section|issue-description (got ${command})`);
  }
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  try {
    main();
  } catch (err) {
    console.error(`release-feed: ${err.message}`);
    process.exit(1);
  }
}
