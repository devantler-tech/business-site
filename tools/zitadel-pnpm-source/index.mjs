// Native pnpm metadata/archive boundary for the frozen identity build. No import
// or no-argument invocation installs packages, contacts a registry or emits proof.
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
import { gunzipSync } from "node:zlib";
import { fileURLToPath } from "node:url";

const require = createRequire(import.meta.url);
const { filterLockfileByImportersAndEngine } = require("@pnpm/lockfile.filtering");
const { nameVerFromPkgSnapshot, packageIdFromSnapshot, pkgSnapshotToResolution } = require("@pnpm/lockfile.utils");
const { createCafs, getFilePathByModeInCafs, getIndexFilePathInCafs } = require("@pnpm/store.cafs");
const { readWantedLockfile, readCurrentLockfile } = require("@pnpm/lockfile.fs");
const { readModulesManifest } = require("@pnpm/modules-yaml");
const { pkgRequiresBuild } = require("@pnpm/exec.pkg-requires-build");
const ssri = require("ssri");
const importerIds = [".", "console", "packages/zitadel-client", "packages/zitadel-proto"];
const included = { dependencies: true, devDependencies: true, optionalDependencies: true };
const registry = "https://registry.npmjs.org/";
// pnpm 10.30.3's own config adds this unused scope even without a project setting.
// Matching that native metadata does not admit fetching JSR package sources.
const nativeRegistries = { default: registry, "@jsr": "https://npm.jsr.io/" };
const sha256 = (body) => createHash("sha256").update(body).digest("hex");
let phase = "arguments";
class InputFailure extends Error {}
const fail = (message) => {
  throw new InputFailure(message);
};
const sorted = (values) => [...values].sort();
const frozenLock = "b881c1adea82bb48195b0a5725b041f3a476c45adea565a531172f3ab398ddaf";
const metadataLimit = 32 * 1024 * 1024;
const packageLimit = 128 * 1024 * 1024;
const totalLimit = 2 * 1024 * 1024 * 1024;
const here = path.dirname(fileURLToPath(import.meta.url));

function archiveIntegrity(value) {
  if (typeof value !== "string" || !/^sha512-[A-Za-z0-9+/]{86}==$/.test(value))
    fail("complete original archive SHA-512 required");
  if (ssri.parse(value, { strict: true }).toString() !== value) fail("noncanonical archive SRI");
  return value;
}

// Use pnpm's readers/filtering, preserving peer-qualified snapshots and native
// platform skips. A wanted lock is not evidence that these roots were installed.
export function selectInstalled({ wanted, current, modules, projects }, sourceRoot) {
  phase = "workspace-selection";
  if (!path.isAbsolute(sourceRoot) || path.normalize(sourceRoot) !== sourceRoot || !Array.isArray(projects))
    fail("invalid native workspace observation");
  const observed = projects.map((project) => {
    if (!project || typeof project.name !== "string" || !project.name || typeof project.path !== "string")
      fail("incomplete native project");
    const relative = path.relative(sourceRoot, project.path) || ".";
    if (path.resolve(sourceRoot, relative) !== project.path || relative.startsWith("..") || path.isAbsolute(relative))
      fail("project outside prepared source");
    return relative;
  });
  assert.deepEqual(sorted(observed), importerIds, "native selected workspace differs");
  phase = "installation-profile";
  if (!wanted || !current || wanted.lockfileVersion !== "9.0" || current.lockfileVersion !== "9.0")
    fail("native lock format differs");
  if (modules?.packageManager !== "pnpm@10.30.3") fail("native package manager differs");
  if (modules.layoutVersion !== 5) fail("native modules layout differs");
  if (modules.nodeLinker !== "isolated") fail("native package linker differs");
  assert.deepEqual(modules.included, included, "dependency categories differ");
  assert.deepEqual(modules.registries, nativeRegistries, "native registry differs");
  if (!Array.isArray(modules.skipped) || new Set(modules.skipped).size !== modules.skipped.length)
    fail("invalid native skips");
  function select(lock) {
    for (const id of importerIds) if (!lock.importers?.[id]) fail("missing installed importer");
    const skipped = new Set();
    const result = filterLockfileByImportersAndEngine(lock, importerIds, {
      currentEngine: { nodeVersion: "22.23.3", pnpmVersion: "10.30.3" },
      engineStrict: false,
      include: included,
      includeIncompatiblePackages: false,
      failOnMissingDependencies: true,
      lockfileDir: sourceRoot,
      skipped,
      supportedArchitectures: { os: ["linux"], cpu: ["x64"], libc: ["glibc"] },
    });
    assert.deepEqual(sorted(result.selectedImporterIds), importerIds, "workspace expansion differs");
    return {
      importers: Object.fromEntries(importerIds.map((id) => [id, result.lockfile.importers[id]])),
      packages: result.lockfile.packages,
      skipped: sorted(skipped),
    };
  }
  const selected = select(wanted);
  phase = "current-lock-join";
  const installed = select(current);
  assert.deepEqual(installed.importers, selected.importers, "installed importer records differ");
  assert.deepEqual(installed.packages, selected.packages, "installed package snapshots differ");
  assert.deepEqual(installed.skipped, selected.skipped, "installed skip classification differs");
  phase = "optional-skips";
  assert.deepEqual(sorted(modules.skipped), selected.skipped, "actual optional skips differ");
  const active = Object.entries(selected.packages).filter(([key]) => !selected.skipped.includes(key));
  phase = "selected-packages";
  if (active.length === 0 || active.length > 4000) fail("empty or unbounded native selection");
  const packages = active
    .sort(([a], [b]) => a.localeCompare(b, "en"))
    .map(([depPath, snapshot]) => {
      const { name, version } = nameVerFromPkgSnapshot(depPath, snapshot);
      const resolution = pkgSnapshotToResolution(depPath, snapshot, modules.registries);
      if (!name || !version || resolution.type || !resolution.tarball?.startsWith(registry))
        fail("non-registry selected input");
      const url = new URL(resolution.tarball);
      if (url.origin !== "https://registry.npmjs.org" || url.username || url.password || url.search || url.hash)
        fail("unadmitted archive origin");
      return {
        depPath,
        name,
        version,
        pkgId: packageIdFromSnapshot(depPath, snapshot),
        integrity: archiveIntegrity(resolution.integrity),
        tarball: resolution.tarball,
      };
    });
  return { importers: importerIds, skipped: selected.skipped, packages };
}

// No pnpm timestamp shortcuts: inspect regular, unlinked files and hash every
// byte through a non-following descriptor, checking identity again after read.
function readRegular(filename, limit, mode, links = 1) {
  const full = path.resolve(filename);
  if (full !== filename || fs.realpathSync(full) !== full) fail("linked native input");
  for (let parent = path.dirname(full); parent !== path.dirname(parent); parent = path.dirname(parent)) {
    const info = fs.lstatSync(parent);
    if (!info.isDirectory() || info.isSymbolicLink()) fail("linked native parent");
  }
  const before = fs.lstatSync(full);
  if (
    !before.isFile() ||
    before.nlink !== links ||
    before.size > limit ||
    (before.mode & 0o7000) !== 0 ||
    (mode !== undefined && (before.mode & 0o7777) !== mode)
  )
    fail("invalid native file type, size or mode");
  const fd = fs.openSync(full, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW);
  try {
    const opened = fs.fstatSync(fd);
    if (opened.ino !== before.ino || opened.dev !== before.dev || opened.nlink !== links)
      fail("native file replaced before read");
    const data = fs.readFileSync(fd);
    const after = fs.fstatSync(fd);
    const final = fs.lstatSync(full);
    if (
      data.length !== before.size ||
      after.size !== before.size ||
      after.mtimeMs !== before.mtimeMs ||
      after.ctimeMs !== before.ctimeMs ||
      after.nlink !== links ||
      final.ino !== opened.ino ||
      final.dev !== opened.dev ||
      final.nlink !== links ||
      fs.realpathSync(full) !== full
    )
      fail("native file changed during read");
    return data;
  } finally {
    fs.closeSync(fd);
  }
}

function fileMap(files) {
  if (!files || typeof files !== "object" || Array.isArray(files)) fail("missing native file map");
  return Object.fromEntries(
    Object.entries(files)
      .sort(([a], [b]) => a.localeCompare(b, "en"))
      .map(([name, file]) => {
        if (
          !name ||
          name.includes("\\") ||
          name.startsWith("/") ||
          path.posix.normalize(name) !== name ||
          name.split("/").includes("..") ||
          name.endsWith("/")
        )
          fail("unsafe native archive member");
        if (
          !Number.isInteger(file.mode) ||
          file.mode < 0 ||
          file.mode > 0o777 ||
          !Number.isSafeInteger(file.size) ||
          file.size < 0 ||
          file.size > 128 * 1024 * 1024
        )
          fail("invalid native archive attributes");
        return [name, { mode: file.mode, size: file.size, integrity: archiveIntegrity(String(file.integrity)) }];
      }),
  );
}

// A fresh native interpretation of original SRI-verified bytes must match the
// complete connected index, then every connected CAFS payload. Parsing writes
// only into the caller's newly reserved, otherwise empty validation store.
function interpretArchive(archive, selected, index, validationStore) {
  if (
    !Buffer.isBuffer(archive) ||
    archive.length === 0 ||
    archive.length > 128 * 1024 * 1024 ||
    !ssri.checkData(archive, archiveIntegrity(selected.integrity), { strict: true })
  )
    fail("original archive integrity differs");
  if (
    !index ||
    index.name !== selected.name ||
    index.version !== selected.version ||
    (index.sideEffects && Object.keys(index.sideEffects).length !== 0)
  )
    fail("connected package identity or side effects differ");
  if (Object.keys(index).some((key) => !["name", "version", "requiresBuild", "files", "sideEffects"].includes(key)))
    fail("unsupported native index field");
  const validation = fs.lstatSync(validationStore);
  if (
    !path.isAbsolute(validationStore) ||
    fs.realpathSync(validationStore) !== validationStore ||
    !validation.isDirectory() ||
    (validation.mode & 0o7777) !== 0o700 ||
    fs.readdirSync(validationStore).length !== 0
  )
    fail("validation store must be new, private and empty");
  if (archive[0] !== 0x1f || archive[1] !== 0x8b || archive[2] !== 8) fail("original npm archive is not gzip");
  const tar = gunzipSync(archive, { maxOutputLength: 512 * 1024 * 1024 });
  const native = createCafs(validationStore).addFilesFromTarball(tar, true);
  if (native.manifest?.name !== selected.name || native.manifest?.version !== selected.version)
    fail("original package manifest differs");
  const expected = fileMap(native.filesIndex);
  const actual = fileMap(index.files);
  assert.deepEqual(actual, expected, "connected index differs from original archive");
  if (
    typeof index.requiresBuild !== "boolean" ||
    index.requiresBuild !== pkgRequiresBuild(native.manifest, native.filesIndex)
  )
    fail("native lifecycle classification differs from original archive");
  if (Object.keys(actual).length === 0 || Object.keys(actual).length > 100000)
    fail("empty or unbounded archive file map");
  return actual;
}

export function verifyPackageArchive(archive, selected, index, store, validationStore, ownedLinks = new Map()) {
  const actual = interpretArchive(archive, selected, index, validationStore);
  let bytes = 0;
  for (const record of Object.values(actual)) {
    const filename = getFilePathByModeInCafs(store, record.integrity, record.mode);
    const stat = fs.lstatSync(filename);
    const links = 1 + (ownedLinks.get(`${stat.dev}:${stat.ino}`) || 0);
    const body = readRegular(filename, record.size, record.mode & ~0o022, links);
    if (body.length !== record.size || !ssri.checkData(body, record.integrity, { strict: true }))
      fail("connected CAFS content differs");
    bytes += body.length;
    if (bytes > 512 * 1024 * 1024) fail("unbounded package contents");
  }
  return { archiveSha256: sha256(archive), files: Object.keys(actual).length, bytes };
}

// pnpm may import by hard link on filesystems without reflinks. Admit only the
// additional regular names physically inside this build's own node_modules.
// A link elsewhere still makes nlink exceed this complete counted set.
function installedLinks(sourceRoot) {
  const result = new Map();
  let files = 0;
  function visit(directory) {
    if (fs.realpathSync(directory) !== directory) fail("linked installation directory");
    for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
      const filename = path.join(directory, entry.name);
      if (entry.isDirectory()) visit(filename);
      else if (entry.isFile()) {
        if (++files > 1000000) fail("unbounded native installation");
        const stat = fs.lstatSync(filename);
        const key = `${stat.dev}:${stat.ino}`;
        result.set(key, (result.get(key) || 0) + 1);
      } else if (!entry.isSymbolicLink()) fail("special native installation member");
    }
  }
  visit(path.join(sourceRoot, "node_modules"));
  return result;
}

function privateDirectory(directory, empty = false) {
  if (
    !path.isAbsolute(directory) ||
    path.normalize(directory) !== directory ||
    fs.realpathSync(directory) !== directory
  )
    fail("directory must be physical and absolute");
  const stat = fs.lstatSync(directory);
  if (!stat.isDirectory() || (stat.mode & 0o7777) !== 0o700 || (empty && fs.readdirSync(directory).length !== 0))
    fail("directory must be private and empty when reserved");
  return stat;
}

function sameDirectory(directory, before) {
  const after = privateDirectory(directory);
  if (before.dev !== after.dev || before.ino !== after.ino) fail("reserved directory was replaced");
}

function reserve(directory) {
  if (
    !path.isAbsolute(directory) ||
    path.normalize(directory) !== directory ||
    fs.realpathSync(path.dirname(directory)) !== path.dirname(directory)
  )
    fail("new directory must have a physical absolute parent");
  fs.mkdirSync(directory, { mode: 0o700 });
  return privateDirectory(directory, true);
}

function separate(left, right) {
  for (const value of [left, right])
    if (!path.isAbsolute(value) || path.normalize(value) !== value) fail("physical absolute operation paths required");
  if (left === right || left.startsWith(`${right}/`) || right.startsWith(`${left}/`))
    fail("input and writable operation paths overlap");
}

function memberPath(root, name) {
  if (
    !name ||
    path.posix.normalize(name) !== name ||
    name.startsWith("/") ||
    name.includes("\\") ||
    name.split("/").includes("..")
  )
    fail("unsafe package member");
  return path.join(root, name);
}

function writeNew(root, name, body) {
  const filename = memberPath(root, name);
  const parent = path.dirname(filename);
  fs.mkdirSync(parent, { recursive: true, mode: 0o700 });
  if (fs.realpathSync(parent) !== parent) fail("linked output parent");
  const fd = fs.openSync(
    filename,
    fs.constants.O_WRONLY | fs.constants.O_CREAT | fs.constants.O_EXCL | fs.constants.O_NOFOLLOW,
    0o600,
  );
  try {
    fs.writeFileSync(fd, body);
    fs.fsyncSync(fd);
  } finally {
    fs.closeSync(fd);
  }
  if (!readRegular(filename, body.length, 0o600).equals(body)) fail("retained bytes differ");
}

function json(body) {
  return JSON.parse(body.toString("utf8"));
}
function encoded(value) {
  return Buffer.from(`${JSON.stringify(value, null, 2)}\n`);
}
const digest = (value) => typeof value === "string" && /^[a-f0-9]{64}$/.test(value);

const observations = [
  ["prepared/source/pnpm-lock.yaml", "records/source/pnpm-lock.yaml"],
  ["prepared/source/pnpm-workspace.yaml", "records/source/pnpm-workspace.yaml"],
  ["prepared/source/node_modules/.pnpm/lock.yaml", "records/source/node_modules/.pnpm/lock.yaml"],
  ["prepared/source/node_modules/.modules.yaml", "records/source/node_modules/.modules.yaml"],
  ["pnpm-selected-projects.json", "records/projects.json"],
  ...importerIds.map((id) => [
    `prepared/source/${id === "." ? "" : `${id}/`}package.json`,
    `records/source/${id === "." ? "" : `${id}/`}package.json`,
  ]),
];

async function loadObservation(capsule, sourceRoot) {
  phase = "native-metadata-readers";
  const copy = path.join(capsule, "records/source");
  const wanted = await readWantedLockfile(copy, {
    ignoreIncompatible: false,
    useGitBranchLockfile: false,
    mergeGitBranchLockfiles: false,
  });
  const current = await readCurrentLockfile(path.join(copy, "node_modules/.pnpm"), { ignoreIncompatible: false });
  const modules = await readModulesManifest(path.join(copy, "node_modules"));
  const projects = json(readRegular(path.join(capsule, "records/projects.json"), metadataLimit, 0o600));
  if (modules?.virtualStoreDir === path.join(copy, "node_modules/.pnpm"))
    modules.virtualStoreDir = path.join(sourceRoot, "node_modules/.pnpm");
  phase = "native-store-locations";
  if (modules?.virtualStoreDir !== path.join(sourceRoot, "node_modules/.pnpm"))
    fail("installation virtual store differs");
  if (modules.storeDir !== path.join(path.dirname(path.dirname(sourceRoot)), "pnpm-store/v10"))
    fail("installation CAFS store differs");
  if (!Array.isArray(projects)) fail("native workspace selection is not an array");
  phase = "workspace-source-identities";
  for (const project of projects) {
    if (!project || typeof project.path !== "string") fail("incomplete native workspace projection");
    const id = path.relative(sourceRoot, project.path) || ".";
    if (!importerIds.includes(id)) fail("unexpected native workspace");
    const manifest = json(
      readRegular(memberPath(copy, `${id === "." ? "" : `${id}/`}package.json`), metadataLimit, 0o600),
    );
    if (manifest.name !== project.name) fail("native workspace name differs from source");
  }
  return { selection: selectInstalled({ wanted, current, modules, projects }, sourceRoot), modules };
}

function closedMembers(root, expected, privateModes = true) {
  const wanted = new Set(expected);
  const directories = new Set(["."]);
  // pnpm's native initStore creates all buckets up front, including empty ones.
  if (!privateModes)
    for (const type of ["files", "index"]) {
      directories.add(type);
      for (let i = 0; i < 256; i++) directories.add(`${type}/${i.toString(16).padStart(2, "0")}`);
    }
  for (const name of wanted)
    for (let p = path.posix.dirname(name); p !== "."; p = path.posix.dirname(p)) directories.add(p);
  function visit(relative) {
    const directory = relative === "." ? root : memberPath(root, relative);
    const stat = fs.lstatSync(directory);
    if (
      !directories.has(relative) ||
      !stat.isDirectory() ||
      (stat.mode & 0o7000) !== 0 ||
      (privateModes && (stat.mode & 0o7777) !== 0o700)
    )
      fail("unexpected or unsafe directory");
    for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
      const name = relative === "." ? entry.name : `${relative}/${entry.name}`;
      if (entry.isDirectory()) visit(name);
      else if (!entry.isFile() || !wanted.delete(name)) fail("extra or linked package member");
    }
  }
  visit(".");
  if (wanted.size) fail("missing package members");
}

function record(root, name, max = packageLimit) {
  const body = readRegular(memberPath(root, name), max, 0o600);
  return { path: name, bytes: body.length, sha256: sha256(body) };
}

function receipt(manifest, seal) {
  return {
    schemaVersion: 1,
    manifestSha256: seal,
    sourceManifestSha256: manifest.sourceManifestSha256,
    packages: manifest.packages.length,
    archives: new Set(manifest.packages.map((p) => p.archive)).size,
    files: manifest.files.length,
    bytes: manifest.files.reduce((n, f) => n + f.bytes, 0),
    selectedPNPMSourcesCaptured: true,
    dependencyInputsCaptured: false,
    nativeOfflineReplayProved: false,
  };
}

async function downloadOriginal(selected) {
  const response = await fetch(selected.tarball, { redirect: "error", signal: AbortSignal.timeout(120000) });
  if (!response.ok || !response.body) fail("original archive acquisition failed");
  const chunks = [];
  let size = 0;
  for await (const chunk of response.body) {
    size += chunk.length;
    if (size > packageLimit) {
      await response.body.cancel().catch(() => {});
      fail("original archive exceeds limit");
    }
    chunks.push(chunk);
  }
  return Buffer.concat(chunks);
}

// Retain only selected original archives and literal connected observations.
// The SDK/tool dependencies are separate from the application's source closure.
export async function captureBuild({
  build,
  sourceManifestSha256,
  output,
  scratch,
  expectedLock = frozenLock,
  acquire = downloadOriginal,
}) {
  phase = "capture-paths";
  if (!digest(sourceManifestSha256) || !digest(expectedLock)) fail("complete external source/lock seals required");
  const sourceRoot = path.join(build, "prepared/source");
  if (!path.isAbsolute(build) || fs.realpathSync(build) !== build) fail("physical build root required");
  separate(output, sourceRoot);
  separate(output, path.join(build, "pnpm-store"));
  separate(output, scratch);
  separate(scratch, sourceRoot);
  separate(scratch, path.join(build, "pnpm-store"));
  const reservation = reserve(output);
  reserve(scratch);
  for (const [from, to] of observations) {
    phase = `literal-observation:${from}`;
    writeNew(output, to, readRegular(memberPath(build, from), metadataLimit));
  }
  for (const name of ["index.mjs", "package.json", "package-lock.json"]) {
    phase = `literal-sdk:${name}`;
    writeNew(output, `records/sdk/${name}`, readRegular(path.join(here, name), metadataLimit));
  }
  if (sha256(readRegular(path.join(output, "records/source/pnpm-lock.yaml"), metadataLimit, 0o600)) !== expectedLock)
    fail("wanted lock differs from frozen source");
  const { selection, modules } = await loadObservation(output, sourceRoot);
  phase = "connected-installation";
  const ownedLinks = installedLinks(sourceRoot);
  const packages = [],
    files = new Set(observations.map(([, to]) => to));
  for (const name of ["index.mjs", "package.json", "package-lock.json"]) files.add(`records/sdk/${name}`);
  const originals = new Map();
  const storeMembers = new Set();
  const payloads = new Map();
  const indexes = new Map();
  let bytes = 0;
  for (const selected of selection.packages) {
    phase = "connected-index";
    const indexPath = getIndexFilePathInCafs(modules.storeDir, selected.integrity, selected.pkgId);
    const rawIndex = readRegular(indexPath, metadataLimit);
    const index = json(rawIndex);
    const existing = originals.get(selected.integrity);
    let original;
    if (!existing) {
      phase = "original-archive-acquisition";
      original = await acquire(selected);
      if (!Buffer.isBuffer(original) || !ssri.checkData(original, selected.integrity, { strict: true }))
        fail("original selected archive differs");
      bytes += original.length;
      if (bytes > totalLimit) fail("selected original archives exceed limit");
      const name = `archives/${sha256(original)}.tgz`;
      writeNew(output, name, original);
      files.add(name);
      originals.set(selected.integrity, name);
    } else original = readRegular(memberPath(output, existing), packageLimit, 0o600);
    const validation = path.join(scratch, `package-${packages.length}`);
    fs.mkdirSync(validation, { mode: 0o700 });
    phase = "original-archive-cafs-join";
    verifyPackageArchive(original, selected, index, modules.storeDir, validation, ownedLinks);
    const retainedIndex = `indexes/${sha256(rawIndex)}.json`;
    if (!files.has(retainedIndex)) {
      writeNew(output, retainedIndex, rawIndex);
      files.add(retainedIndex);
    }
    packages.push({ ...selected, archive: `archives/${sha256(original)}.tgz`, index: retainedIndex });
    indexes.set(indexPath, retainedIndex);
    storeMembers.add(path.relative(modules.storeDir, indexPath));
    for (const f of Object.values(fileMap(index.files))) {
      const filename = getFilePathByModeInCafs(modules.storeDir, f.integrity, f.mode);
      storeMembers.add(path.relative(modules.storeDir, filename));
      payloads.set(filename, f);
    }
  }
  closedMembers(modules.storeDir, storeMembers, false);
  const finalLinks = installedLinks(sourceRoot);
  for (const [filename, f] of payloads) {
    const stat = fs.lstatSync(filename);
    const links = 1 + (finalLinks.get(`${stat.dev}:${stat.ino}`) || 0);
    const body = readRegular(filename, f.size, f.mode & ~0o022, links);
    if (body.length !== f.size || !ssri.checkData(body, f.integrity, { strict: true }))
      fail("connected CAFS changed before finalization");
  }
  for (const [filename, retained] of indexes)
    if (!readRegular(filename, metadataLimit).equals(readRegular(memberPath(output, retained), metadataLimit, 0o600)))
      fail("connected index changed during capture");
  for (const [from, to] of observations)
    if (
      !readRegular(memberPath(build, from), metadataLimit).equals(
        readRegular(memberPath(output, to), metadataLimit, 0o600),
      )
    )
      fail("connected observation changed during capture");
  const manifest = {
    schemaVersion: 1,
    profile: "frozen-zitadel-pnpm-source-v1",
    sourceRoot,
    sourceManifestSha256,
    wantedLockSha256: expectedLock,
    target: { os: "linux", cpu: "x64", libc: "glibc", node: "22.23.3", pnpm: "10.30.3" },
    importers: selection.importers,
    skipped: selection.skipped,
    packages,
    files: sorted(files).map((name) => record(output, name)),
  };
  if (manifest.files.reduce((n, f) => n + f.bytes, 0) > totalLimit) fail("complete retained package exceeds limit");
  sameDirectory(output, reservation);
  writeNew(output, "manifest.json", encoded(manifest));
  closedMembers(output, [...files, "manifest.json"]);
  // Full final readback catches a later acquisition callback changing earlier bytes.
  const result = await verifyCapsule({
    capsule: output,
    sha256: sha256(encoded(manifest)),
    scratch: path.join(scratch, "readback"),
    expectedLock,
  });
  sameDirectory(output, reservation);
  return result;
}

// Error objects from native libraries can carry source records or private paths.
// Retain only our literal reasons and a small native error-code vocabulary.
export function failureDiagnostic(error) {
  const codes = new Set([
    "ENOENT",
    "EACCES",
    "EPERM",
    "EINTEGRITY",
    "ERR_ASSERTION",
    "ERR_PNPM_LOCKFILE_MISSING_DEPENDENCY",
  ]);
  const assertions = new Set([
    "native selected workspace differs",
    "dependency categories differ",
    "native registry differs",
    "workspace expansion differs",
    "installed importer records differ",
    "installed package snapshots differ",
    "installed skip classification differs",
    "actual optional skips differ",
    "connected index differs from original archive",
  ]);
  const firstLine = typeof error?.message === "string" ? error.message.split("\n")[0] : "";
  return {
    phase,
    reason:
      error instanceof InputFailure
        ? error.message
        : error?.code === "ERR_ASSERTION" && assertions.has(firstLine)
          ? firstLine
          : codes.has(error?.code)
            ? error.code
            : "native input rejected",
  };
}

export async function verifyCapsule({ capsule, sha256: seal, scratch, expectedLock = frozenLock, restore }) {
  if (!digest(seal) || !digest(expectedLock)) fail("external seals required");
  separate(capsule, scratch);
  if (restore) {
    separate(capsule, restore);
    separate(scratch, restore);
  }
  const reservation = privateDirectory(capsule);
  reserve(scratch);
  const raw = readRegular(path.join(capsule, "manifest.json"), metadataLimit, 0o600);
  if (sha256(raw) !== seal) fail("external pnpm manifest seal differs");
  const manifest = json(raw);
  assert.deepEqual(
    sorted(Object.keys(manifest)),
    sorted([
      "schemaVersion",
      "profile",
      "sourceRoot",
      "sourceManifestSha256",
      "wantedLockSha256",
      "target",
      "importers",
      "skipped",
      "packages",
      "files",
    ]),
  );
  if (
    manifest.schemaVersion !== 1 ||
    manifest.profile !== "frozen-zitadel-pnpm-source-v1" ||
    !digest(manifest.sourceManifestSha256) ||
    manifest.wantedLockSha256 !== expectedLock ||
    !Array.isArray(manifest.files) ||
    manifest.files.length > 10000
  )
    fail("unsupported retained pnpm profile");
  assert.deepEqual(manifest.target, { os: "linux", cpu: "x64", libc: "glibc", node: "22.23.3", pnpm: "10.30.3" });
  if (!raw.equals(encoded(manifest))) fail("noncanonical retained pnpm manifest");
  const expected = new Set();
  let bytes = 0;
  for (const file of manifest.files) {
    if (
      !file ||
      expected.has(file.path) ||
      !digest(file.sha256) ||
      !Number.isSafeInteger(file.bytes) ||
      file.bytes < 0 ||
      file.bytes > packageLimit
    )
      fail("invalid retained member table");
    assert.deepEqual(record(capsule, file.path), file, "retained member differs");
    expected.add(file.path);
    bytes += file.bytes;
    if (bytes > totalLimit) fail("retained package exceeds limit");
  }
  assert.deepEqual(
    manifest.files.map((f) => f.path),
    sorted(expected),
    "retained member table is unordered",
  );
  closedMembers(capsule, [...expected, "manifest.json"]);
  for (const name of ["index.mjs", "package.json", "package-lock.json"]) {
    if (
      !readRegular(path.join(capsule, "records/sdk", name), metadataLimit, 0o600).equals(
        readRegular(path.join(here, name), metadataLimit),
      )
    )
      fail("retained native adapter or SDK differs");
  }
  if (sha256(readRegular(path.join(capsule, "records/source/pnpm-lock.yaml"), metadataLimit, 0o600)) !== expectedLock)
    fail("retained wanted lock differs");
  const { selection } = await loadObservation(capsule, manifest.sourceRoot);
  assert.deepEqual(manifest.importers, selection.importers);
  assert.deepEqual(manifest.skipped, selection.skipped);
  if (!Array.isArray(manifest.packages) || manifest.packages.length !== selection.packages.length)
    fail("retained selected package count differs");
  const wanted = new Set(observations.map(([, to]) => to));
  for (const name of ["index.mjs", "package.json", "package-lock.json"]) wanted.add(`records/sdk/${name}`);
  const storeMembers = new Set();
  const target = restore;
  const targetReservation = target ? reserve(target) : undefined;
  for (let i = 0; i < selection.packages.length; i++) {
    const selected = selection.packages[i];
    const retained = manifest.packages[i];
    const { archive, index: indexName, ...identity } = retained;
    assert.deepEqual(identity, selected, "retained peer-qualified package differs");
    const original = readRegular(memberPath(capsule, archive), packageLimit, 0o600);
    const rawIndex = readRegular(memberPath(capsule, indexName), metadataLimit, 0o600);
    if (archive !== `archives/${sha256(original)}.tgz` || indexName !== `indexes/${sha256(rawIndex)}.json`)
      fail("retained input name differs from bytes");
    wanted.add(archive);
    wanted.add(indexName);
    const validation = path.join(scratch, `package-${i}`);
    fs.mkdirSync(validation, { mode: 0o700 });
    const index = json(rawIndex);
    const members = interpretArchive(original, selected, index, validation);
    if (target) {
      createCafs(target).addFilesFromTarball(gunzipSync(original, { maxOutputLength: 512 * 1024 * 1024 }), true);
      const indexPath = getIndexFilePathInCafs(target, selected.integrity, selected.pkgId);
      const relative = path.relative(target, indexPath);
      if (!storeMembers.has(relative)) writeNew(target, relative, rawIndex);
      storeMembers.add(relative);
      for (const member of Object.values(members)) {
        const filename = getFilePathByModeInCafs(target, member.integrity, member.mode);
        const body = readRegular(filename, member.size, member.mode & ~0o022);
        if (body.length !== member.size || !ssri.checkData(body, member.integrity, { strict: true }))
          fail("restored CAFS bytes differ");
        storeMembers.add(path.relative(target, filename));
      }
    }
  }
  assert.deepEqual(sorted(expected), sorted(wanted), "retained archive/index membership differs");
  if (target) {
    closedMembers(target, storeMembers, false);
    sameDirectory(target, targetReservation);
  }
  for (const file of manifest.files)
    assert.deepEqual(record(capsule, file.path), file, "retained member changed during native interpretation");
  closedMembers(capsule, [...expected, "manifest.json"]);
  sameDirectory(capsule, reservation);
  return { ...receipt(manifest, seal), ...(target ? { nativeStoreRestored: true } : {}) };
}

async function main(args) {
  if (args.length === 0) return;
  if (
    process.platform !== "linux" ||
    process.arch !== "x64" ||
    process.versions.node !== "22.23.3" ||
    process.umask() !== 0o022
  )
    fail("frozen native Linux/amd64 Node 22.23.3 and umask 022 required");
  const operation = args.shift();
  const values = {};
  while (args.length) {
    const key = args.shift();
    const value = args.shift();
    if (
      !["--build", "--source-sha256", "--output", "--scratch", "--capsule", "--sha256", "--store"].includes(key) ||
      !value ||
      values[key]
    )
      fail("one complete explicit pnpm operation required");
    values[key] = value;
  }
  let result;
  if (
    operation === "capture" &&
    Object.keys(values).length === 4 &&
    values["--build"] &&
    values["--source-sha256"] &&
    values["--output"] &&
    values["--scratch"]
  ) {
    result = await captureBuild({
      build: values["--build"],
      sourceManifestSha256: values["--source-sha256"],
      output: values["--output"],
      scratch: values["--scratch"],
    });
  } else if (
    ["verify", "restore"].includes(operation) &&
    Object.keys(values).length === (operation === "restore" ? 4 : 3) &&
    values["--capsule"] &&
    values["--sha256"] &&
    values["--scratch"] &&
    (operation !== "restore" || values["--store"])
  ) {
    result = await verifyCapsule({
      capsule: values["--capsule"],
      sha256: values["--sha256"],
      scratch: values["--scratch"],
      restore: values["--store"],
    });
  } else fail("one complete explicit pnpm operation required");
  process.stdout.write(`${JSON.stringify(result)}\n`);
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url))
  main(process.argv.slice(2)).catch((error) => {
    process.stderr.write(`zitadel-pnpm-source: ${JSON.stringify(failureDiagnostic(error))}\n`);
    process.exitCode = 1;
  });
