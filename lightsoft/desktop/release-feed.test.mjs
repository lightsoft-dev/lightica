// Tests for release-feed.mjs: node --test lightsoft/desktop/release-feed.test.mjs
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, it } from "node:test";
import { fileURLToPath } from "node:url";
import {
  ISSUE_SECTION_HEADING,
  planReleases,
  publish,
  readReleases,
  renderIssueSection,
  replaceIssueSection,
  withFileDigests,
  withSigningTeam,
} from "./release-feed.mjs";

const script = fileURLToPath(new URL("./release-feed.mjs", import.meta.url));
const dirs = [];
afterEach(() => {
  while (dirs.length) rmSync(dirs.pop(), { recursive: true, force: true });
});
function tempDir() {
  const dir = mkdtempSync(join(tmpdir(), "lightica-feed-test-"));
  dirs.push(dir);
  return dir;
}

// What electron-builder leaves in apps/desktop/dist for one mac build.
function fakeDist(version, arch = "arm64") {
  const dist = tempDir();
  const stem = `lightica-${version}-mac-${arch}`;
  for (const ext of ["dmg", "zip", "dmg.blockmap", "zip.blockmap"]) writeFileSync(join(dist, `${stem}.${ext}`), `${stem}.${ext}`);
  const yml = arch === "arm64" ? "latest-mac.yml" : "latest-x64-mac.yml";
  writeFileSync(
    join(dist, yml),
    // The digests are stale on purpose: build.sh changes the dmg after electron-builder.
    `version: ${version}\nfiles:\n  - url: ${stem}.zip\n    sha512: stale\n    size: 1\n  - url: ${stem}.dmg\n    sha512: stale\n    size: 1\npath: ${stem}.zip\nsha512: stale\nreleaseDate: '2026-11-19T06:00:00.000Z'\nminimumSystemVersion: 22.0.0\n`,
  );
  return dist;
}

const release = (version, extra = {}) => ({
  dist: fakeDist(version),
  version,
  arch: "arm64",
  team: "ABCDE12345",
  upstream: "v0.11.0",
  commit: "e250b62f1aaaaaaa",
  now: new Date("2026-11-19T06:00:00Z"),
  ...extra,
});

describe("publish", () => {
  it("publishes the build, its history and the metadata apps read", () => {
    const feed = tempDir();
    publish(feed, release("0.11.0-20261119"));

    const yml = readFileSync(join(feed, "latest-mac.yml"), "utf-8");
    assert.match(yml, /^version: 0\.11\.0-20261119$/m);
    assert.match(yml, /^lighticaSigningTeam: ABCDE12345$/m);
    assert.deepEqual(readReleases(feed).map((r) => r.version), ["0.11.0-20261119"]);
    const [record] = readReleases(feed);
    assert.equal(record.files.find((f) => f.name.endsWith(".dmg")).sha256.length, 64);
    assert.match(readFileSync(join(feed, "index.html"), "utf-8"), /lightica-0\.11\.0-20261119-mac-arm64\.dmg/);
    // The metadata describes the files as published.
    for (const ext of ["dmg", "zip"]) {
      const name = `lightica-0.11.0-20261119-mac-arm64.${ext}`;
      const sha512 = createHash("sha512").update(readFileSync(join(feed, name))).digest("base64");
      assert.match(yml, new RegExp(`- url: ${name.replaceAll(".", "\\.")}\n    sha512: ${sha512.replace(/[+/]/g, "\\$&")}\n    size: ${name.length}\n`));
    }
    assert.doesNotMatch(yml, /stale/);
    // Nothing staged is left behind.
    assert.deepEqual(readdirSync(feed).filter((name) => name.startsWith(".")), []);
  });

  it("refuses metadata from another build", () => {
    const feed = tempDir();
    const input = release("0.11.0-20261119");
    writeFileSync(join(input.dist, "latest-mac.yml"), "version: 0.11.0-20261118\n");
    assert.throws(() => publish(feed, input), /not 0\.11\.0-20261119/);
    assert.equal(existsSync(join(feed, "latest-mac.yml")), false);
  });

  it("keeps the files of the newest releases only", () => {
    const feed = tempDir();
    for (const day of [16, 17, 18, 19]) publish(feed, { ...release(`0.11.0-202611${day}`), keep: 2 });

    assert.deepEqual(
      readReleases(feed).map((r) => [r.version, Boolean(r.pruned)]),
      [["0.11.0-20261119", false], ["0.11.0-20261118", false], ["0.11.0-20261117", true], ["0.11.0-20261116", true]],
    );
    assert.equal(existsSync(join(feed, "lightica-0.11.0-20261116-mac-arm64.dmg")), false);
    assert.equal(existsSync(join(feed, "lightica-0.11.0-20261118-mac-arm64.dmg")), true);
  });

  it("feeds lightsoft/version.sh the versions already used", () => {
    const feed = tempDir();
    publish(feed, release("0.11.0-20261119"));
    publish(feed, release("0.11.0-20261119.2"));
    assert.equal(execFileSync("node", [script, "versions", feed], { encoding: "utf-8" }), "0.11.0-20261119.2\n0.11.0-20261119\n");
  });

  it("publishes Intel builds on their own channel", () => {
    const feed = tempDir();
    publish(feed, { ...release("0.11.0-20261119"), dist: fakeDist("0.11.0-20261119", "x64"), arch: "x64" });
    assert.equal(existsSync(join(feed, "latest-x64-mac.yml")), true);
    assert.equal(existsSync(join(feed, "latest-mac.yml")), false);
  });
});

describe("withFileDigests", () => {
  it("rewrites the listed files it has digests for and leaves the rest", () => {
    const yml = "version: 1.0.0-1\nfiles:\n  - url: a.zip\n    sha512: old-a\n    size: 1\n  - url: a.dmg\n    sha512: old-dmg\n    size: 2\npath: a.zip\nsha512: old-a\nreleaseDate: '2026-11-19T06:00:00.000Z'\n";
    assert.equal(
      withFileDigests(yml, { "a.dmg": { sha512: "new-dmg", size: 3 } }),
      "version: 1.0.0-1\nfiles:\n  - url: a.zip\n    sha512: old-a\n    size: 1\n  - url: a.dmg\n    sha512: new-dmg\n    size: 3\npath: a.zip\nsha512: old-a\nreleaseDate: '2026-11-19T06:00:00.000Z'\n",
    );
    assert.match(withFileDigests(yml, { "a.zip": { sha512: "new-a", size: 4 } }), /^path: a\.zip\nsha512: new-a$/m);
  });
});

describe("withSigningTeam", () => {
  it("replaces a previous team and drops it for an unsigned build", () => {
    const yml = "version: 1\nlighticaSigningTeam: OLD\n";
    assert.equal(withSigningTeam(yml, "NEW"), "version: 1\nlighticaSigningTeam: NEW\n");
    assert.equal(withSigningTeam(yml, ""), "version: 1\n");
  });
});

describe("planReleases", () => {
  it("replaces a re-published version instead of listing it twice", () => {
    const old = { version: "0.11.0-20261119", arch: "arm64", files: [], commit: "a" };
    const again = { ...old, commit: "b" };
    assert.deepEqual(planReleases([old], again).releases, [again]);
  });
});

describe("install issue section", () => {
  const description = `Lightica 설치 안내\n\n## 업데이트\n\n자동\n\n${ISSUE_SECTION_HEADING}\n\n> **아직 공유하지 마세요.** 임시 빌드\n\n- 옛 파일\n\n## 문제 해결\n\n표\n`;

  it("lists the current download with its checksum", () => {
    const feed = tempDir();
    publish(feed, release("0.11.0-20261119"));
    const section = renderIssueSection(readReleases(feed));
    assert.match(section, /^## 현재 배포 파일$/m);
    assert.match(section, /\*\*0\.11\.0-20261119\*\*/);
    assert.match(section, /\[`lightica-0\.11\.0-20261119-mac-arm64\.dmg`\]\(https:\/\/mac-studio\.taile83671\.ts\.net:3444\/lightica\/lightica-0\.11\.0-20261119-mac-arm64\.dmg\)/);
    assert.match(section, /SHA-256 `[0-9a-f]{64}`/);
  });

  it("replaces only that section", () => {
    const next = replaceIssueSection(description, `${ISSUE_SECTION_HEADING}\n\n- 새 파일\n`);
    assert.equal(next, `Lightica 설치 안내\n\n## 업데이트\n\n자동\n\n${ISSUE_SECTION_HEADING}\n\n- 새 파일\n\n## 문제 해결\n\n표\n`);
  });

  it("replaces a trailing section and appends a missing one", () => {
    const trailing = `안내\n\n${ISSUE_SECTION_HEADING}\n\n- 옛 파일\n`;
    assert.equal(replaceIssueSection(trailing, `${ISSUE_SECTION_HEADING}\n\n- 새 파일`), `안내\n\n${ISSUE_SECTION_HEADING}\n\n- 새 파일\n`);
    assert.equal(replaceIssueSection("안내\n", `${ISSUE_SECTION_HEADING}\n\n- 새 파일`), `안내\n\n${ISSUE_SECTION_HEADING}\n\n- 새 파일\n`);
  });

  it("works from the command line", () => {
    const dir = tempDir();
    mkdirSync(join(dir, "in"));
    writeFileSync(join(dir, "in", "current.md"), description);
    writeFileSync(join(dir, "in", "section.md"), `${ISSUE_SECTION_HEADING}\n\n- 새 파일\n`);
    const out = execFileSync("node", [script, "issue-description", "--current", join(dir, "in", "current.md"), "--section", join(dir, "in", "section.md")], { encoding: "utf-8" });
    assert.match(out, /- 새 파일\n\n## 문제 해결/);
    assert.doesNotMatch(out, /옛 파일/);
  });
});
