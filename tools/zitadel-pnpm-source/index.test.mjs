import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createRequire } from "node:module";
import { execFileSync, spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { test } from "node:test";
import {
  selectInstalled,
  verifyPackageArchive,
  captureBuild,
  verifyCapsule,
  failureDiagnostic,
  readRegular,
  inspectBuild,
} from "./index.mjs";

const require = createRequire(import.meta.url);
const { createCafs, getFilePathByModeInCafs } = require("@pnpm/store.cafs");
const ssri = require("ssri");
const fixBin = require("bin-links/lib/fix-bin");
const { writeWantedLockfile, writeCurrentLockfile } = require("@pnpm/lockfile.fs");
const { writeModulesManifest } = require("@pnpm/modules-yaml");
const { getIndexFilePathInCafs } = require("@pnpm/store.cafs");
const ids = [".", "console", "packages/zitadel-client", "packages/zitadel-proto"];
const root = "/owned/prepared/source";
const sri = (body) => ssri.fromData(body, { algorithms: ["sha512"] }).toString();
const clone = (value) => structuredClone(value);
test("file refusal diagnostics contain only bounded metadata and no pathname", () => {
  const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), "private-file-diagnostic-")));
  const file = path.join(root, "fictional-sensitive-name");
  try {
    fs.writeFileSync(file, "bytes", { mode: 0o644 });
    let error;
    try {
      readRegular(file, 32, 0o600);
    } catch (caught) {
      error = caught;
    }
    assert.ok(error);
    const diagnostic = failureDiagnostic(error);
    assert.deepEqual(diagnostic.fileMetadata, {
      regular: true,
      mode: 0o644,
      expectedMode: 0o600,
      size: 5,
      limit: 32,
      links: 1,
      expectedLinks: 1,
    });
    assert.equal(JSON.stringify(diagnostic).includes(root), false);
    assert.equal(JSON.stringify(diagnostic).includes("fictional-sensitive-name"), false);
  } finally {
    fs.rmSync(root, { recursive: true });
  }
});
test("a FIFO input is refused without waiting for a writer", () => {
  const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), "native-fifo-")));
  const file = path.join(root, "payload");
  try {
    const created = spawnSync("mkfifo", [file], { encoding: "utf8" });
    assert.equal(created.status, 0, created.stderr);
    const entrypoint = fileURLToPath(new URL("./index.mjs", import.meta.url));
    const result = spawnSync(
      process.execPath,
      [
        "--input-type=module",
        "-e",
        `import {readRegular} from ${JSON.stringify(entrypoint)}; readRegular(${JSON.stringify(file)},32);`,
      ],
      { encoding: "utf8", timeout: 2000 },
    );
    assert.equal(result.status, 1, "special-file refusal hung or succeeded");
    assert.match(result.stderr, /invalid native file type, size or mode/);
  } finally {
    fs.rmSync(root, { recursive: true });
  }
});
test("native reads use bounded descriptor operations rather than unbounded readFile", () => {
  const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), "native-read-")));
  const file = path.join(root, "payload");
  fs.writeFileSync(file, "original", { mode: 0o600 });
  const readFile = fs.readFileSync;
  try {
    fs.readFileSync = () => {
      throw new Error("unbounded readFile was used");
    };
    assert.equal(readRegular(file, 8, 0o600).toString(), "original");
  } finally {
    fs.readFileSync = readFile;
    fs.rmSync(root, { recursive: true });
  }
});
test("a growing input fails after at most its admitted size plus one byte", () => {
  const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), "native-growth-")));
  const file = path.join(root, "payload");
  fs.writeFileSync(file, "original", { mode: 0o600 });
  const read = fs.readSync;
  let bytes = 0;
  try {
    fs.readSync = (...args) => {
      const n = read(...args);
      bytes += n;
      if (bytes === 8) fs.appendFileSync(file, Buffer.alloc(4096));
      return n;
    };
    assert.throws(() => readRegular(file, 8, 0o600), /native file changed during read/);
    assert.equal(bytes, 9);
  } finally {
    fs.readSync = read;
    fs.rmSync(root, { recursive: true });
  }
});
test("native failure diagnostics never echo external messages, stacks or invented codes", () => {
  const error = new Error("fictional-private-path and credential-like test data");
  error.code = "FICTIONAL_SECRET_VALUE";
  const diagnostic = JSON.stringify(failureDiagnostic(error));
  assert.equal(diagnostic.includes("fictional-private-path"), false);
  assert.equal(diagnostic.includes("FICTIONAL_SECRET_VALUE"), false);
  assert.equal(diagnostic.includes("native input rejected"), true);
  error.code = "ENOENT";
  assert.equal(failureDiagnostic(error).reason, "ENOENT");
});
test("the native source entrypoint is inert without arguments and fails closed for invalid operations", () => {
  const entrypoint = fileURLToPath(new URL("./index.mjs", import.meta.url));
  const inert = spawnSync(process.execPath, [entrypoint], { encoding: "utf8" });
  assert.equal(inert.status, 0);
  assert.equal(inert.stdout, "");
  assert.equal(inert.stderr, "");
  const invalid = spawnSync(process.execPath, [entrypoint, "unexpected-operation"], { encoding: "utf8" });
  assert.equal(invalid.status, 1);
  assert.equal(invalid.stdout, "");
});
function selectionFixture() {
  const wanted = {
    lockfileVersion: "9.0",
    importers: Object.fromEntries(ids.map((id) => [id, {}])),
    packages: {
      "alpha@1.0.0(peer@1.0.0)": { resolution: { integrity: sri("original") }, dependencies: { peer: "1.0.0" } },
      "alpha@1.0.0(peer@2.0.0)": { resolution: { integrity: sri("original") }, dependencies: { peer: "2.0.0" } },
      "peer@1.0.0": { resolution: { integrity: sri("one") } },
      "peer@2.0.0": { resolution: { integrity: sri("two") } },
      "native@1.0.0": { resolution: { integrity: sri("darwin") }, os: ["darwin"], optional: true },
      "unselected@1.0.0": { resolution: { integrity: sri("not installed") } },
    },
  };
  wanted.importers["."].dependencies = { alpha: "1.0.0(peer@1.0.0)" };
  wanted.importers.console.dependencies = { alpha: "1.0.0(peer@2.0.0)" };
  wanted.importers.console.optionalDependencies = { native: "1.0.0" };
  const modules = {
    layoutVersion: 5,
    nodeLinker: "isolated",
    packageManager: "pnpm@10.30.3",
    included: { dependencies: true, devDependencies: true, optionalDependencies: true },
    skipped: ["native@1.0.0"],
    storeDir: "/owned/pnpm-store/v10",
    virtualStoreDir: `${root}/node_modules/.pnpm`,
    registries: { default: "https://registry.npmjs.org/", "@jsr": "https://npm.jsr.io/" },
  };
  const projects = ids.map((id) => ({
    name: id === "." ? "@zitadel/zitadel" : `@zitadel/${path.basename(id)}`,
    path: path.resolve(root, id),
  }));
  return { wanted, current: clone(wanted), modules, projects };
}

test("native selection retains peer instances, classifies skips and omits unrelated lock rows", () => {
  const result = selectInstalled(selectionFixture(), root);
  assert.deepEqual(result.importers, ids);
  assert.deepEqual(result.skipped, ["native@1.0.0"]);
  assert.deepEqual(
    result.packages.map((p) => p.depPath),
    ["alpha@1.0.0(peer@1.0.0)", "alpha@1.0.0(peer@2.0.0)", "peer@1.0.0", "peer@2.0.0"],
  );
  assert.equal(result.packages[0].integrity, result.packages[1].integrity);
  assert.equal(result.packages[0].tarball, "https://registry.npmjs.org/alpha/-/alpha-1.0.0.tgz");
});

test("native installation skips include incompatible optional packages from unselected workspace roots", () => {
  const fixture = selectionFixture();
  fixture.wanted.importers.docs = { optionalDependencies: { "outside-native": "1.0.0" } };
  fixture.wanted.packages["outside-native@1.0.0"] = {
    resolution: { integrity: sri("outside source") },
    optional: true,
    os: ["darwin"],
  };
  fixture.current = clone(fixture.wanted);
  fixture.modules.skipped.push("outside-native@1.0.0");
  const result = selectInstalled(fixture, root);
  assert.deepEqual(result.importers, ids);
  assert.deepEqual(result.skipped, ["native@1.0.0"]);
  assert.equal(
    result.packages.some((p) => p.depPath === "outside-native@1.0.0"),
    false,
  );
  fixture.wanted.packages["outside-native@1.0.0"].os = ["linux"];
  fixture.current = clone(fixture.wanted);
  assert.throws(() => selectInstalled(fixture, root), /actual optional skips differ/);
});

test("optional-skip diagnostics are bounded and never echo unknown installation entries", () => {
  const fixture = selectionFixture();
  fixture.modules.skipped.push("fictional-private-path-or-credential");
  let error;
  try {
    selectInstalled(fixture, root);
  } catch (caught) {
    error = caught;
  }
  assert.ok(error);
  const diagnostic = failureDiagnostic(error);
  assert.equal(diagnostic.reason, "actual optional skips differ");
  assert.deepEqual(diagnostic.optionalSkips, { expected: 1, actual: 2, missing: [], extra: ["unrecognised"] });
  assert.equal(JSON.stringify(diagnostic).includes("fictional-private-path"), false);
});

test("the native built-in JSR metadata does not admit selected JSR downloads", () => {
  const fixture = selectionFixture();
  fixture.wanted.importers["."].dependencies["@jsr/example"] = "1.0.0";
  fixture.wanted.packages["@jsr/example@1.0.0"] = { resolution: { integrity: sri("jsr source") } };
  fixture.current = clone(fixture.wanted);
  assert.throws(() => selectInstalled(fixture, root), /non-registry selected input/);
});

for (const [name, mutate] of [
  [
    "empty native workspace observation",
    (f) => {
      f.projects = [];
    },
  ],
  [
    "asserted rather than observed workspace expansion",
    (f) => {
      f.projects = f.projects.slice(0, 2);
    },
  ],
  [
    "extra selected workspace",
    (f) => {
      f.projects.push({ name: "other", path: `${root}/apps/docs` });
    },
  ],
  [
    "outside workspace path",
    (f) => {
      f.projects[3].path = "/foreign";
    },
  ],
  [
    "duplicate selected workspace",
    (f) => {
      f.projects.push(f.projects[0]);
    },
  ],
  [
    "missing current package",
    (f) => {
      delete f.current.packages["peer@2.0.0"];
    },
  ],
  [
    "wrong current edge",
    (f) => {
      f.current.packages["alpha@1.0.0(peer@2.0.0)"].dependencies.peer = "1.0.0";
    },
  ],
  [
    "wrong current archive SRI",
    (f) => {
      f.current.packages["peer@1.0.0"].resolution.integrity = sri("changed");
    },
  ],
  [
    "wrong current importer",
    (f) => {
      f.current.importers.console.dependencies.alpha = "1.0.0(peer@1.0.0)";
    },
  ],
  [
    "missing optional skip",
    (f) => {
      f.modules.skipped = [];
    },
  ],
  [
    "invented optional skip",
    (f) => {
      f.modules.skipped.push("peer@1.0.0");
    },
  ],
  [
    "wrong package manager",
    (f) => {
      f.modules.packageManager = "pnpm@10.0.0";
    },
  ],
  [
    "production-only observation",
    (f) => {
      f.modules.included.devDependencies = false;
    },
  ],
  [
    "foreign registry",
    (f) => {
      f.modules.registries.default = "https://example.invalid/";
    },
  ],
  [
    "substituted native built-in registry",
    (f) => {
      f.modules.registries["@jsr"] = "https://example.invalid/";
    },
  ],
  [
    "extra scoped registry",
    (f) => {
      f.modules.registries["@other"] = "https://example.invalid/";
    },
  ],
  [
    "missing native built-in registry",
    (f) => {
      delete f.modules.registries["@jsr"];
    },
  ],
  [
    "non-registry resolution",
    (f) => {
      f.wanted.packages["peer@1.0.0"].resolution = { type: "git", repo: "x" };
      f.current = clone(f.wanted);
    },
  ],
  [
    "weak/incomplete archive SRI",
    (f) => {
      f.wanted.packages["peer@1.0.0"].resolution.integrity = "sha1-YQ==";
      f.current = clone(f.wanted);
    },
  ],
]) {
  test(`native selection rejects ${name}`, () => {
    const fixture = selectionFixture();
    mutate(fixture);
    assert.throws(() => selectInstalled(fixture, root));
  });
}

async function buildFixture(t, manifestExtra = {}) {
  const f = archiveFixture(t, false, 0o644, manifestExtra);
  const build = path.join(f.directory, "build");
  const source = path.join(build, "prepared/source");
  fs.mkdirSync(path.join(source, "node_modules/.pnpm"), { recursive: true });
  const store = path.join(build, "pnpm-store/v10");
  fs.mkdirSync(store, { recursive: true, mode: 0o700 });
  const native = createCafs(store).addFilesFromTarball(f.archive, true);
  const wanted = {
    lockfileVersion: "9.0",
    importers: Object.fromEntries(ids.map((id) => [id, { specifiers: {} }])),
    packages: { "alpha@1.0.0": { resolution: { integrity: f.selected.integrity } } },
  };
  wanted.importers["."] = { specifiers: { alpha: "1.0.0" }, dependencies: { alpha: "1.0.0" } };
  const projects = [];
  for (const id of ids) {
    const project = path.resolve(source, id);
    fs.mkdirSync(project, { recursive: true });
    const name = id === "." ? "@zitadel/zitadel" : `@zitadel/${path.basename(id)}`;
    fs.writeFileSync(path.join(project, "package.json"), JSON.stringify({ name, private: true }));
    projects.push({ name, path: project });
  }
  await writeWantedLockfile(source, wanted);
  await writeCurrentLockfile(path.join(source, "node_modules/.pnpm"), wanted);
  await writeModulesManifest(path.join(source, "node_modules"), {
    layoutVersion: 5,
    nodeLinker: "isolated",
    packageManager: "pnpm@10.30.3",
    included: { dependencies: true, devDependencies: true, optionalDependencies: true },
    skipped: [],
    storeDir: store,
    virtualStoreDir: path.join(source, "node_modules/.pnpm"),
    virtualStoreDirMaxLength: 120,
    registries: { default: "https://registry.npmjs.org/", "@jsr": "https://npm.jsr.io/" },
    hoistedDependencies: {},
    pendingBuilds: [],
    prunedAt: new Date().toISOString(),
  });
  fs.writeFileSync(path.join(source, "pnpm-workspace.yaml"), "packages:\n  - console\n  - packages/*\n");
  fs.writeFileSync(path.join(build, "pnpm-selected-projects.json"), JSON.stringify(projects));
  const files = Object.fromEntries(
    Object.entries(native.filesIndex).map(([name, info]) => [
      name,
      { mode: info.mode, size: info.size, integrity: String(info.integrity), checkedAt: info.checkedAt },
    ]),
  );
  const index = { name: "alpha", version: "1.0.0", requiresBuild: false, files };
  const indexPath = getIndexFilePathInCafs(store, f.selected.integrity, "alpha@1.0.0");
  fs.mkdirSync(path.dirname(indexPath), { recursive: true });
  fs.writeFileSync(indexPath, JSON.stringify(index));
  const expectedLock = createHash("sha256")
    .update(fs.readFileSync(path.join(source, "pnpm-lock.yaml")))
    .digest("hex");
  const output = path.join(f.directory, "capsule");
  const options = {
    build,
    output,
    sourceManifestSha256: "a".repeat(64),
    scratch: path.join(f.directory, "capture-work"),
    expectedLock,
    acquire: async () => f.archive,
  };
  return { ...f, build, source, store, indexPath, expectedLock, output, options };
}

test("the early installation check reads the real native metadata without creating a capsule or acquiring archives", async (t) => {
  const f = await buildFixture(t);
  const before = fs.readdirSync(f.build).sort();
  const result = await inspectBuild({ build: f.build, expectedLock: f.expectedLock });
  assert.equal(result.packages, 1);
  assert.equal(result.optionalSkips, 0);
  assert.equal(result.selectedPNPMSourcesCaptured, false);
  assert.equal(result.dependencyInputsCaptured, false);
  assert.equal(result.nativeOfflineReplayProved, false);
  assert.deepEqual(fs.readdirSync(f.build).sort(), before);
  assert.equal(fs.existsSync(f.output), false);
});

test("the early installation check rejects an invented optional skip and a substituted wanted lock", async (t) => {
  const f = await buildFixture(t);
  await assert.rejects(inspectBuild({ build: f.build, expectedLock: "b".repeat(64) }), /wanted lock differs/);
  const manifest = path.join(f.source, "node_modules/.modules.yaml");
  const data = JSON.parse(fs.readFileSync(manifest));
  data.skipped = ["fictional-unselected-package"];
  fs.writeFileSync(manifest, JSON.stringify(data));
  await assert.rejects(inspectBuild({ build: f.build, expectedLock: f.expectedLock }), /actual optional skips differ/);
});

test("an acquired archive cannot be written before its complete locked SRI matches", async (t) => {
  const f = await buildFixture(t);
  f.options.acquire = async () => Buffer.from("different downloaded bytes");
  await assert.rejects(captureBuild(f.options), /original selected archive differs/);
  assert.equal(fs.existsSync(path.join(f.output, "archives")), false);
  assert.equal(fs.existsSync(path.join(f.output, "manifest.json")), false);
});

test("capture, final readback and fresh native CAFS restoration preserve literal inputs", async (t) => {
  const f = await buildFixture(t);
  const captured = await captureBuild(f.options);
  assert.equal(captured.selectedPNPMSourcesCaptured, true);
  assert.equal(captured.packages, 1);
  assert.equal(captured.archives, 1);
  assert.equal(captured.dependencyInputsCaptured, false);
  assert.equal(captured.nativeOfflineReplayProved, false);
  const restored = path.join(f.directory, "restored");
  const verified = await verifyCapsule({
    capsule: f.output,
    sha256: captured.manifestSha256,
    scratch: path.join(f.directory, "verify-work"),
    expectedLock: f.expectedLock,
    restore: restored,
  });
  assert.deepEqual(verified, { ...captured, nativeStoreRestored: true });
  assert.deepEqual(
    fs.readFileSync(getIndexFilePathInCafs(restored, f.selected.integrity, "alpha@1.0.0")),
    fs.readFileSync(f.indexPath),
  );
});

test("a later selected package's declared bin can share an earlier package's CAFS inode through complete capture and restore", async (t) => {
  const f = await buildFixture(t);
  const other = archiveFixture(t, false, 0o644, { name: "zeta", bin: "index.js" });
  createCafs(f.store).addFilesFromTarball(other.archive, true);
  const indexPath = getIndexFilePathInCafs(f.store, other.selected.integrity, "zeta@1.0.0");
  fs.mkdirSync(path.dirname(indexPath), { recursive: true });
  fs.writeFileSync(indexPath, JSON.stringify(other.index));
  const wanted = await require("@pnpm/lockfile.fs").readWantedLockfile(f.source, { ignoreIncompatible: false });
  wanted.packages["zeta@1.0.0"] = { resolution: { integrity: other.selected.integrity } };
  wanted.importers["."].dependencies.zeta = "1.0.0";
  wanted.importers["."].specifiers.zeta = "1.0.0";
  await writeWantedLockfile(f.source, wanted);
  await writeCurrentLockfile(path.join(f.source, "node_modules/.pnpm"), wanted);
  const expectedLock = createHash("sha256")
    .update(fs.readFileSync(path.join(f.source, "pnpm-lock.yaml")))
    .digest("hex");
  const entry = other.index.files["index.js"];
  assert.equal(entry.integrity, f.index.files["index.js"].integrity);
  const file = getFilePathByModeInCafs(f.store, entry.integrity, entry.mode);
  const installed = path.join(f.source, "node_modules/installed-command");
  fs.linkSync(file, installed);
  await fixBin(installed, 0o755);
  const captured = await captureBuild({
    ...f.options,
    expectedLock,
    acquire: async (selected) => (selected.name === "zeta" ? other.archive : f.archive),
  });
  assert.equal(captured.packages, 2);
  const target = path.join(f.directory, "restored");
  const restored = await verifyCapsule({
    capsule: f.output,
    sha256: captured.manifestSha256,
    scratch: path.join(f.directory, "restore-work"),
    expectedLock,
    restore: target,
  });
  assert.equal(restored.nativeStoreRestored, true);
  assert.equal(fs.statSync(getFilePathByModeInCafs(target, entry.integrity, entry.mode)).mode & 0o777, 0o644);
});

test("capture accounts for native hard links only inside this build installation", async (t) => {
  const f = await buildFixture(t);
  const index = JSON.parse(fs.readFileSync(f.indexPath));
  const member = index.files["index.js"];
  const source = getFilePathByModeInCafs(f.store, member.integrity, member.mode);
  const copy = path.join(f.source, "node_modules/installed-fixture");
  fs.linkSync(source, copy);
  const captured = await captureBuild(f.options);
  assert.equal(captured.selectedPNPMSourcesCaptured, true);
});

for (const [name, mutate] of [
  ["extra connected index", (f) => fs.writeFileSync(path.join(path.dirname(f.indexPath), "unselected.json"), "{}")],
  ["extra connected payload", (f) => fs.writeFileSync(path.join(f.store, "extra"), "unselected")],
  [
    "foreign hard link to native payload",
    (f) => {
      const index = JSON.parse(fs.readFileSync(f.indexPath));
      const member = index.files["index.js"];
      fs.linkSync(
        getFilePathByModeInCafs(f.store, member.integrity, member.mode),
        path.join(f.directory, "foreign-alias"),
      );
    },
  ],
  [
    "failing archive acquisition",
    (f) => {
      f.options.acquire = async () => {
        throw new Error("failed");
      };
    },
  ],
  [
    "substituted wanted lock",
    (f) => {
      f.options.expectedLock = "b".repeat(64);
    },
  ],
  [
    "changed prior observation during acquisition",
    (f) => {
      f.options.acquire = async () => {
        fs.appendFileSync(path.join(f.output, "records/projects.json"), " ");
        return f.archive;
      };
    },
  ],
]) {
  test(`capture emits no receipt for ${name}`, async (t) => {
    const f = await buildFixture(t);
    mutate(f);
    await assert.rejects(captureBuild(f.options));
  });
}

for (const [name, mutate] of [
  ["extra capsule member", (f) => fs.writeFileSync(path.join(f.output, "extra"), "not admitted", { mode: 0o600 })],
  [
    "changed archive",
    (f) => {
      const archive = fs.readdirSync(path.join(f.output, "archives"))[0];
      fs.appendFileSync(path.join(f.output, "archives", archive), "changed");
    },
  ],
  ["missing observation", (f) => fs.unlinkSync(path.join(f.output, "records/projects.json"))],
  ["wrong capsule mode", (f) => fs.chmodSync(path.join(f.output, "records/projects.json"), 0o644)],
]) {
  test(`complete readback rejects ${name}`, async (t) => {
    const f = await buildFixture(t);
    const receipt = await captureBuild(f.options);
    mutate(f);
    await assert.rejects(
      verifyCapsule({
        capsule: f.output,
        sha256: receipt.manifestSha256,
        scratch: path.join(f.directory, "verify-work"),
        expectedLock: f.expectedLock,
      }),
    );
  });
}

for (const kind of ["scratch", "restore"]) {
  test(`readback rejects ${kind} inside the sealed capsule without changing it`, async (t) => {
    const f = await buildFixture(t);
    const receipt = await captureBuild(f.options);
    const nested = path.join(f.output, "unwanted-write");
    await assert.rejects(
      verifyCapsule({
        capsule: f.output,
        sha256: receipt.manifestSha256,
        expectedLock: f.expectedLock,
        scratch: kind === "scratch" ? nested : path.join(f.directory, "verify-work"),
        restore: kind === "restore" ? nested : undefined,
      }),
    );
    assert.equal(fs.existsSync(nested), false, "a rejected input capsule must not be mutated");
  });
}

test("capture refuses output in the prepared source before changing it", async (t) => {
  const f = await buildFixture(t);
  const nested = path.join(f.source, "unwanted-write");
  await assert.rejects(captureBuild({ ...f.options, output: nested }));
  assert.equal(fs.existsSync(nested), false);
});

function archiveFixture(t, needsBuild = false, archiveMode = 0o644, manifestExtra = {}) {
  const directory = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), "zitadel-pnpm-native-test-")));
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  const source = path.join(directory, "package");
  fs.mkdirSync(source);
  fs.writeFileSync(
    path.join(source, "package.json"),
    `${JSON.stringify({ name: "alpha", version: "1.0.0", ...manifestExtra, ...(needsBuild ? { scripts: { postinstall: "node build.js" } } : {}) })}\n`,
    { mode: 0o644 },
  );
  fs.writeFileSync(path.join(source, "index.js"), "export const value = 1;\n", { mode: 0o644 });
  fs.chmodSync(path.join(source, "index.js"), archiveMode);
  fs.writeFileSync(path.join(source, "bin"), "#!/bin/sh\nexit 0\n", { mode: 0o755 });
  const archive = execFileSync("tar", ["-czf", "-", "-C", directory, "package"], {
    env: { ...process.env, COPYFILE_DISABLE: "1" },
  });
  const store = path.join(directory, "store");
  fs.mkdirSync(store, { mode: 0o700 });
  const native = createCafs(store).addFilesFromTarball(archive, true);
  const files = Object.fromEntries(
    Object.entries(native.filesIndex).map(([name, f]) => [
      name,
      {
        mode: f.mode,
        size: f.size,
        integrity: String(f.integrity),
        checkedAt: f.checkedAt,
      },
    ]),
  );
  const index = { name: manifestExtra.name || "alpha", version: "1.0.0", requiresBuild: false, files };
  const selected = { name: index.name, version: "1.0.0", integrity: sri(archive) };
  const validation = path.join(directory, "validation");
  fs.mkdirSync(validation, { mode: 0o700 });
  return { directory, archive, store, index, selected, validation };
}

test("native archive interpretation is joined to every connected CAFS byte and executable mode", async (t) => {
  const f = archiveFixture(t);
  const result = await verifyPackageArchive(f.archive, f.selected, f.index, f.store, f.validation);
  assert.equal(result.files, 3);
  assert.equal(result.archiveSha256, createHash("sha256").update(f.archive).digest("hex"));
});

for (const manifest of [{ bin: "index.js" }, { bin: { alpha: "index.js" } }, { directories: { bin: "." } }]) {
  test(`native bin linking may make its own hard-linked source executable: ${JSON.stringify(manifest)}`, async (t) => {
    const f = archiveFixture(t, false, 0o644, manifest);
    const entry = f.index.files["index.js"];
    const file = getFilePathByModeInCafs(f.store, entry.integrity, entry.mode);
    const installed = path.join(f.directory, "installed-index.js");
    fs.linkSync(file, installed);
    // This is pnpm's actual fix-bin implementation, not a chmod simulation.
    await fixBin(installed, 0o755);
    assert.equal(fs.statSync(file).mode & 0o777, 0o755);
    const stat = fs.statSync(installed);
    const owned = new Map([[`${stat.dev}:${stat.ino}`, 1]]);
    const result = await verifyPackageArchive(f.archive, f.selected, f.index, f.store, f.validation, owned);
    assert.equal(result.files, 3);
    assert.equal(entry.mode, 0o644, "the original source map must not be rewritten");
  });
}

for (const name of [
  "undeclared executable",
  "unlinked declared executable",
  "foreign linked bin",
  "wrong linked bin mode",
]) {
  test(`native bin mode admission rejects ${name}`, async (t) => {
    const f = archiveFixture(t, false, 0o644, name === "undeclared executable" ? {} : { bin: "index.js" });
    const entry = f.index.files["index.js"];
    const file = getFilePathByModeInCafs(f.store, entry.integrity, entry.mode);
    const owned = new Map();
    if (name !== "unlinked declared executable") {
      fs.linkSync(file, path.join(f.directory, "installed"));
      if (name !== "foreign linked bin") {
        const stat = fs.statSync(file);
        owned.set(`${stat.dev}:${stat.ino}`, 1);
      }
    }
    await fixBin(file, 0o755);
    if (name === "wrong linked bin mode") fs.chmodSync(file, 0o777);
    await assert.rejects(
      verifyPackageArchive(f.archive, f.selected, f.index, f.store, f.validation, owned),
      /invalid native file/,
    );
  });
}

for (const mode of [0o444, 0o640, 0o744, 0o711]) {
  test(`native CAFS permissions normalize archive mode ${mode.toString(8)}`, async (t) => {
    const f = archiveFixture(t, false, mode);
    const result = await verifyPackageArchive(f.archive, f.selected, f.index, f.store, f.validation);
    assert.equal(result.files, 3);
    const entry = f.index.files["index.js"];
    const physical = getFilePathByModeInCafs(f.store, entry.integrity, entry.mode);
    assert.equal(fs.statSync(physical).mode & 0o777, mode & 0o111 ? 0o755 : 0o644);
  });
}

for (const mode of [0o444, 0o744]) {
  test(`archive mode ${mode.toString(8)} cannot substitute for the exact physical CAFS mode`, async (t) => {
    const f = archiveFixture(t, false, mode);
    const entry = f.index.files["index.js"];
    fs.chmodSync(getFilePathByModeInCafs(f.store, entry.integrity, entry.mode), mode);
    await assert.rejects(
      verifyPackageArchive(f.archive, f.selected, f.index, f.store, f.validation),
      /invalid native file type, size or mode/,
    );
  });
}

test("native lifecycle classification cannot be changed from required to false", async (t) => {
  const f = archiveFixture(t, true);
  await assert.rejects(verifyPackageArchive(f.archive, f.selected, f.index, f.store, f.validation));
});

test("the exact native empty CAFS buckets do not count as unrelated inputs", async (t) => {
  const f = await buildFixture(t);
  for (const type of ["files", "index"])
    for (let i = 0; i < 256; i++)
      fs.mkdirSync(path.join(f.store, type, i.toString(16).padStart(2, "0")), { recursive: true });
  const result = await captureBuild(f.options);
  assert.equal(result.selectedPNPMSourcesCaptured, true);
});

test("the native empty store temporary root carries no source inputs", async (t) => {
  const f = await buildFixture(t);
  fs.mkdirSync(path.join(f.store, "tmp"));
  const captured = await captureBuild(f.options);
  assert.equal(captured.selectedPNPMSourcesCaptured, true);
});

for (const kind of ["file", "directory", "symlink"]) {
  test(`the native temporary root cannot hide an unrelated ${kind}`, async (t) => {
    const f = await buildFixture(t);
    const tmp = path.join(f.store, "tmp");
    fs.mkdirSync(tmp);
    if (kind === "directory") fs.mkdirSync(path.join(tmp, "extra"));
    else if (kind === "symlink") fs.symlinkSync(f.source, path.join(tmp, "extra"));
    else fs.writeFileSync(path.join(tmp, "extra"), "unrelated");
    await assert.rejects(captureBuild(f.options));
  });
}

for (const [name, mutate] of [
  [
    "changed original archive",
    (f) => {
      f.archive = Buffer.concat([f.archive, Buffer.from("changed")]);
    },
  ],
  [
    "coherent substituted index",
    (f) => {
      f.index.files["index.js"].integrity = sri("substituted");
    },
  ],
  [
    "missing index member",
    (f) => {
      delete f.index.files["index.js"];
    },
  ],
  [
    "extra index member",
    (f) => {
      f.index.files.extra = clone(f.index.files["index.js"]);
    },
  ],
  [
    "wrong index mode",
    (f) => {
      f.index.files.bin.mode = 0o644;
    },
  ],
  [
    "changed native lifecycle classification",
    (f) => {
      f.index.requiresBuild = true;
    },
  ],
  [
    "nonboolean lifecycle classification",
    (f) => {
      f.index.requiresBuild = "false";
    },
  ],
  [
    "extra side effects",
    (f) => {
      f.index.sideEffects = { linux: { added: {} } };
    },
  ],
  [
    "wrong package identity",
    (f) => {
      f.index.name = "other";
    },
  ],
  [
    "wrong original package identity",
    (f) => {
      f.selected.name = f.index.name = "other";
    },
  ],
  [
    "pre-existing validation contents",
    (f) => {
      fs.writeFileSync(path.join(f.validation, "foreign"), "keep");
    },
  ],
  [
    "missing connected payload",
    (f) => {
      fs.unlinkSync(getFilePathByModeInCafs(f.store, f.index.files.bin.integrity, f.index.files.bin.mode));
    },
  ],
  [
    "same-size stale-timestamp corruption",
    (f) => {
      const member = f.index.files["index.js"];
      const file = getFilePathByModeInCafs(f.store, member.integrity, member.mode);
      fs.writeFileSync(file, Buffer.alloc(member.size, "x"));
      fs.utimesSync(file, new Date(0), new Date(0));
      member.checkedAt = Date.now() + 100000;
    },
  ],
  [
    "hard-linked connected payload",
    (f) => {
      const member = f.index.files["index.js"];
      fs.linkSync(getFilePathByModeInCafs(f.store, member.integrity, member.mode), path.join(f.directory, "alias"));
    },
  ],
  [
    "linked connected payload",
    (f) => {
      const member = f.index.files["index.js"];
      const file = getFilePathByModeInCafs(f.store, member.integrity, member.mode);
      const moved = path.join(f.directory, "moved");
      fs.renameSync(file, moved);
      fs.symlinkSync(moved, file);
    },
  ],
]) {
  test(`native archive proof rejects ${name}`, async (t) => {
    const fixture = archiveFixture(t);
    mutate(fixture);
    await assert.rejects(
      verifyPackageArchive(fixture.archive, fixture.selected, fixture.index, fixture.store, fixture.validation),
    );
  });
}
