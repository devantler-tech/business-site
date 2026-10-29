// Native pnpm metadata/archive boundary for the frozen identity build. No import
// or no-argument invocation installs packages, contacts a registry or emits proof.
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
import { gunzipSync } from "node:zlib";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const require = createRequire(import.meta.url);
let nativeLoaded = false;
let filterLockfileByImportersAndEngine, nameVerFromPkgSnapshot, packageIdFromSnapshot, pkgSnapshotToResolution;
let createCafs, getFilePathByModeInCafs, getIndexFilePathInCafs, readWantedLockfile, readCurrentLockfile;
let readModulesManifest, pkgRequiresBuild, getBinsFromPackageManifest, ssri;

function sdkRequire(name) {
  const resolved = fs.realpathSync(require.resolve(name));
  if (!resolved.startsWith(path.join(here, "node_modules") + path.sep)) fail("SDK dependency resolved outside the owned installation");
  return require(name);
}

// Inert source/bootstrap authentication must not execute an installed package.
// Only an operation that actually interprets native pnpm data loads this set.
function loadNativeSDK() {
  if (nativeLoaded) return;
  ({ filterLockfileByImportersAndEngine } = sdkRequire("@pnpm/lockfile.filtering"));
  ({ nameVerFromPkgSnapshot, packageIdFromSnapshot, pkgSnapshotToResolution } = sdkRequire("@pnpm/lockfile.utils"));
  ({ createCafs, getFilePathByModeInCafs, getIndexFilePathInCafs } = sdkRequire("@pnpm/store.cafs"));
  ({ readWantedLockfile, readCurrentLockfile } = sdkRequire("@pnpm/lockfile.fs"));
  ({ readModulesManifest } = sdkRequire("@pnpm/modules-yaml"));
  ({ pkgRequiresBuild } = sdkRequire("@pnpm/exec.pkg-requires-build"));
  ({ getBinsFromPackageManifest } = sdkRequire("@pnpm/package-bins"));
  ssri = sdkRequire("ssri");
  nativeLoaded = true;
}
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
  if (Buffer.from(value.slice(7), "base64").toString("base64") !== value.slice(7))
    fail("noncanonical archive SRI");
  return value;
}

// Use pnpm's readers/filtering, preserving peer-qualified snapshots and native
// platform skips. A wanted lock is not evidence that these roots were installed.
export function selectInstalled({ wanted, current, modules, projects }, sourceRoot) {
  loadNativeSDK();
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
  if (
    !Array.isArray(modules.skipped) ||
    modules.skipped.length > 4000 ||
    !modules.skipped.every((value) => typeof value === "string" && value.length <= 1024) ||
    new Set(modules.skipped).size !== modules.skipped.length
  )
    fail("invalid native skips");
  function select(lock, roots = importerIds) {
    for (const id of roots) if (!lock.importers?.[id]) fail("missing installed importer");
    const skipped = new Set();
    const result = filterLockfileByImportersAndEngine(lock, roots, {
      currentEngine: { nodeVersion: "22.23.3", pnpmVersion: "10.30.3" },
      engineStrict: false,
      include: included,
      includeIncompatiblePackages: false,
      failOnMissingDependencies: true,
      lockfileDir: sourceRoot,
      skipped,
      supportedArchitectures: { os: ["linux"], cpu: ["x64"], libc: ["glibc"] },
    });
    assert.deepEqual(sorted(result.selectedImporterIds), roots, "workspace expansion differs");
    return {
      importers: Object.fromEntries(roots.map((id) => [id, result.lockfile.importers[id]])),
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
  // pnpm's installation manifest carries workspace-wide optional exclusions,
  // including incompatible packages belonging only to unselected roots. Check
  // that separate native classification exactly; source acquisition stays on the
  // four observed roots and their selected current/wanted correspondence above.
  const installationSkipped = select(wanted, sorted(Object.keys(wanted.importers))).skipped;
  if (JSON.stringify(sorted(modules.skipped)) !== JSON.stringify(installationSkipped)) {
    const error = new InputFailure("actual optional skips differ");
    // Only identities from the checked frozen public lock can enter diagnostics;
    // never echo an unknown installation row, external message or host path.
    const display = (values) =>
      values
        .slice(0, 24)
        .map((key) =>
          typeof key === "string" &&
          key.length <= 256 &&
          /^[A-Za-z0-9@/._+()=-]+$/.test(key) &&
          Object.hasOwn(wanted.packages, key)
            ? key
            : "unrecognised",
        );
    error.optionalSkips = {
      expected: installationSkipped.length,
      actual: modules.skipped.length,
      missing: display(installationSkipped.filter((key) => !modules.skipped.includes(key))),
      extra: display(sorted(modules.skipped).filter((key) => !installationSkipped.includes(key))),
    };
    throw error;
  }
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
export function readRegular(filename, limit, mode, links = 1) {
  const full = path.resolve(filename);
  if (full !== filename) fail("noncanonical native input");
  for (let parent = path.dirname(full); parent !== path.dirname(parent); parent = path.dirname(parent)) {
    const info = fs.lstatSync(parent);
    if (!info.isDirectory() || info.isSymbolicLink()) fail("linked native parent");
  }
  // O_NONBLOCK lets descriptor validation reject a FIFO without waiting for a writer.
  const fd = fs.openSync(full, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW | fs.constants.O_NONBLOCK);
  try {
    const opened = fs.fstatSync(fd);
    if (
      !opened.isFile() ||
      opened.nlink !== links ||
      opened.size > limit ||
      (opened.mode & 0o7000) !== 0 ||
      (mode !== undefined && (opened.mode & 0o7777) !== mode)
    ) {
      const error = new InputFailure("invalid native file type, size or mode");
      error.fileMetadata = {
        regular: opened.isFile(),
        mode: opened.mode & 0o7777,
        expectedMode: mode ?? null,
        size: opened.size,
        limit,
        links: opened.nlink,
        expectedLinks: links,
      };
      throw error;
    }
    const before = fs.lstatSync(full);
    if (opened.ino !== before.ino || opened.dev !== before.dev || !before.isFile() || fs.realpathSync(full) !== full)
      fail("native file replaced before read");
    // Allocate from the admitted descriptor size, never from a second path/stat
    // read inside readFile. One extra byte detects growth without an unbounded read.
    const buffer = Buffer.allocUnsafe(opened.size + 1);
    let size = 0;
    while (size < buffer.length) {
      const n = fs.readSync(fd, buffer, size, buffer.length - size, null);
      if (n === 0) break;
      size += n;
    }
    const data = buffer.subarray(0, size);
    const after = fs.fstatSync(fd);
    const final = fs.lstatSync(full);
    if (
      data.length !== opened.size ||
      after.size !== opened.size ||
      after.mode !== opened.mode ||
      after.mtimeMs !== opened.mtimeMs ||
      after.ctimeMs !== opened.ctimeMs ||
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

// pnpm's native CAFS stores executable content as 0755 and all other content
// with the default 0666 creation mode under the required umask 022. The archive's
// original permissions remain separately checked in its complete file map.
function nativeCafsMode(mode) {
  return mode & 0o111 ? 0o755 : 0o644;
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
async function interpretArchive(archive, selected, index, validationStore) {
  loadNativeSDK();
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
  // Let the exact native package-bins reader classify command targets from the
  // SRI-verified original manifest. Its directories.bin reader gets a private
  // filename-only view of the original file map, never the live installation.
  const binRoot = path.join(validationStore, "original-bin-view");
  if (!native.manifest.bin && native.manifest.directories?.bin) {
    fs.mkdirSync(binRoot, { mode: 0o700 });
    for (const name of Object.keys(actual)) writeNew(binRoot, name, Buffer.alloc(0));
  }
  const commands = await getBinsFromPackageManifest(native.manifest, binRoot);
  const binMembers = new Set();
  for (const command of commands) {
    const name = path.relative(binRoot, command.path).split(path.sep).join("/");
    if (Object.hasOwn(actual, name)) binMembers.add(name);
  }
  return { files: actual, binMembers };
}

// link-bins calls fix-bin(0755). On a hard-link import that changes the CAFS
// inode too, even when its original archive mode is non-executable. Admit that
// one native transition only for original declared bins and counted owned links.
function connectedMode(record, filename, stat, ownedLinks, binaryFiles) {
  const originalMode = nativeCafsMode(record.mode);
  return originalMode === 0o644 &&
    (stat.mode & 0o7777) === 0o755 &&
    (ownedLinks.get(`${stat.dev}:${stat.ino}`) || 0) > 0 &&
    binaryFiles.has(filename)
    ? 0o755
    : originalMode;
}

function verifyConnectedPackage(actual, store, ownedLinks, binaryFiles) {
  let bytes = 0;
  for (const record of Object.values(actual)) {
    const filename = getFilePathByModeInCafs(store, record.integrity, record.mode);
    const stat = fs.lstatSync(filename);
    const links = 1 + (ownedLinks.get(`${stat.dev}:${stat.ino}`) || 0);
    const body = readRegular(
      filename,
      record.size,
      connectedMode(record, filename, stat, ownedLinks, binaryFiles),
      links,
    );
    if (body.length !== record.size || !ssri.checkData(body, record.integrity, { strict: true }))
      fail("connected CAFS content differs");
    bytes += body.length;
    if (bytes > 512 * 1024 * 1024) fail("unbounded package contents");
  }
  return { files: Object.keys(actual).length, bytes };
}

export async function verifyPackageArchive(archive, selected, index, store, validationStore, ownedLinks = new Map()) {
  const { files, binMembers } = await interpretArchive(archive, selected, index, validationStore);
  const binaries = new Set(
    [...binMembers].map((name) => getFilePathByModeInCafs(store, files[name].integrity, files[name].mode)),
  );
  return { archiveSha256: sha256(archive), ...verifyConnectedPackage(files, store, ownedLinks, binaries) };
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
  loadNativeSDK();
  return readInstallation(
    path.join(capsule, "records/source"),
    path.join(capsule, "records/projects.json"),
    sourceRoot,
    0o600,
  );
}

async function readInstallation(copy, projectsFile, sourceRoot, mode) {
  loadNativeSDK();
  phase = "native-metadata-readers";
  const wanted = await readWantedLockfile(copy, {
    ignoreIncompatible: false,
    useGitBranchLockfile: false,
    mergeGitBranchLockfiles: false,
  });
  const current = await readCurrentLockfile(path.join(copy, "node_modules/.pnpm"), { ignoreIncompatible: false });
  const modules = await readModulesManifest(path.join(copy, "node_modules"));
  const projects = json(readRegular(projectsFile, metadataLimit, mode));
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
      readRegular(memberPath(copy, `${id === "." ? "" : `${id}/`}package.json`), metadataLimit, mode),
    );
    if (manifest.name !== project.name) fail("native workspace name differs from source");
  }
  return { selection: selectInstalled({ wanted, current, modules, projects }, sourceRoot), modules };
}

// This early read-only gate uses the same native selection as final capture,
// before expensive Go generation. It never acquires, writes or claims sources.
export async function inspectBuild({ build, expectedLock = frozenLock }) {
  phase = "installation-paths";
  if (!path.isAbsolute(build) || fs.realpathSync(build) !== build || !digest(expectedLock))
    fail("physical build root and frozen lock required");
  const sourceRoot = path.join(build, "prepared/source");
  const originals = new Map(observations.map(([name]) => [name, readRegular(memberPath(build, name), metadataLimit)]));
  if (sha256(originals.get("prepared/source/pnpm-lock.yaml")) !== expectedLock)
    fail("wanted lock differs from frozen source");
  const { selection } = await readInstallation(sourceRoot, path.join(build, "pnpm-selected-projects.json"), sourceRoot);
  for (const [name, body] of originals)
    if (!readRegular(memberPath(build, name), metadataLimit).equals(body))
      fail("installation observation changed during early check");
  return {
    schemaVersion: 1,
    profile: "native-installation-selection-v1",
    packages: selection.packages.length,
    optionalSkips: selection.skipped.length,
    selectedPNPMSourcesCaptured: false,
    dependencyInputsCaptured: false,
    nativeOfflineReplayProved: false,
  };
}

function checkWorkspaceRegistration(filename, projectRoot) {
  const before = fs.lstatSync(filename);
  if (!before.isSymbolicLink() || before.size < 1 || before.size > 4096) fail("invalid native workspace registration");
  const target = fs.readlinkSync(filename);
  const after = fs.lstatSync(filename);
  if (
    path.resolve(path.dirname(filename), target) !== projectRoot ||
    before.dev !== after.dev ||
    before.ino !== after.ino ||
    before.size !== after.size ||
    before.mtimeMs !== after.mtimeMs ||
    before.ctimeMs !== after.ctimeMs ||
    !after.isSymbolicLink()
  )
    fail("native workspace registration changed or escaped");
}

function closedMembers(root, expected, privateModes = true, projectRoot = undefined) {
  const wanted = new Set(expected);
  const directories = new Set(["."]);
  // pnpm 10.30.3 get-context registers exactly its lockfileDir using
  // package-store's projects/<first 32 hex SHA-256 characters> symlink.
  // Inspect this operational link without following or retaining it. Restored
  // stores have no workspace registration until the native install creates it.
  const registration = !privateModes && projectRoot ? `projects/${sha256(projectRoot).slice(0, 32)}` : undefined;
  let registered = false;
  if (registration) {
    if (!path.isAbsolute(projectRoot) || fs.realpathSync(projectRoot) !== projectRoot)
      fail("physical registered workspace required");
    directories.add("projects");
  }
  // pnpm's native initStore creates all buckets up front, including empty ones.
  if (!privateModes) {
    // create-cafs-store uses this exact temporary parent. It contributes no
    // source inputs: every descendant still fails the closed member check.
    directories.add("tmp");
    for (const type of ["files", "index"]) {
      directories.add(type);
      for (let i = 0; i < 256; i++) directories.add(`${type}/${i.toString(16).padStart(2, "0")}`);
    }
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
    ) {
      const error = new InputFailure("unexpected or unsafe directory");
      const location = privateModes
        ? "retained-directory"
        : relative === "."
          ? "store-root"
          : ["files", "index", "tmp"].includes(relative)
            ? relative
            : /^files\/[a-f0-9]{2}$/.test(relative)
              ? "file-bucket"
              : /^index\/[a-f0-9]{2}$/.test(relative)
                ? "index-bucket"
                : relative.startsWith("tmp/")
                  ? "temporary-descendant"
                  : "unrecognised";
      error.directoryMetadata = {
        location,
        expected: directories.has(relative),
        directory: stat.isDirectory(),
        mode: stat.mode & 0o7777,
        entries: stat.isDirectory() ? fs.readdirSync(directory).length : null,
      };
      throw error;
    }
    const entries = fs.readdirSync(directory, { withFileTypes: true });
    if (relative === "projects" && entries.length !== 1) fail("native workspace registry differs");
    for (const entry of entries) {
      const name = relative === "." ? entry.name : `${relative}/${entry.name}`;
      if (name === registration) {
        checkWorkspaceRegistration(memberPath(root, name), projectRoot);
        registered = true;
      } else if (entry.isDirectory()) visit(name);
      else if (!entry.isFile() || !wanted.delete(name)) fail("extra or linked package member");
    }
  }
  visit(".");
  if (registration && !registered) fail("missing native workspace registration");
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
  const connectedPackages = [];
  const binaryFiles = new Set();
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
    const interpreted = await interpretArchive(original, selected, index, validation);
    connectedPackages.push(interpreted.files);
    for (const name of interpreted.binMembers) {
      const entry = interpreted.files[name];
      binaryFiles.add(getFilePathByModeInCafs(modules.storeDir, entry.integrity, entry.mode));
    }
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
  // A deduplicated CAFS payload can also appear in another selected package's
  // file map. Finish all original manifest joins before checking shared inodes.
  for (const files of connectedPackages) verifyConnectedPackage(files, modules.storeDir, ownedLinks, binaryFiles);
  phase = "closed-native-store";
  closedMembers(modules.storeDir, storeMembers, false, sourceRoot);
  const finalLinks = installedLinks(sourceRoot);
  for (const [filename, f] of payloads) {
    const stat = fs.lstatSync(filename);
    const links = 1 + (finalLinks.get(`${stat.dev}:${stat.ino}`) || 0);
    const body = readRegular(filename, f.size, connectedMode(f, filename, stat, finalLinks, binaryFiles), links);
    if (body.length !== f.size || !ssri.checkData(body, f.integrity, { strict: true }))
      fail("connected CAFS changed before finalization");
  }
  for (const [filename, retained] of indexes)
    if (!readRegular(filename, metadataLimit).equals(readRegular(memberPath(output, retained), metadataLimit, 0o600)))
      fail("connected index changed during capture");
  closedMembers(modules.storeDir, storeMembers, false, sourceRoot);
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
    ...(error instanceof InputFailure && error.optionalSkips ? { optionalSkips: error.optionalSkips } : {}),
    ...(error instanceof InputFailure && error.fileMetadata ? { fileMetadata: error.fileMetadata } : {}),
    ...(error instanceof InputFailure && error.directoryMetadata ? { directoryMetadata: error.directoryMetadata } : {}),
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
    const { files: members } = await interpretArchive(original, selected, index, validation);
    if (target) {
      createCafs(target).addFilesFromTarball(gunzipSync(original, { maxOutputLength: 512 * 1024 * 1024 }), true);
      const indexPath = getIndexFilePathInCafs(target, selected.integrity, selected.pkgId);
      const relative = path.relative(target, indexPath);
      if (!storeMembers.has(relative)) writeNew(target, relative, rawIndex);
      storeMembers.add(relative);
      for (const member of Object.values(members)) {
        const filename = getFilePathByModeInCafs(target, member.integrity, member.mode);
        const body = readRegular(filename, member.size, nativeCafsMode(member.mode));
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

const sdkBootstrapPolicy = { lock: "644afeb662510e90c782e5148821d59ee5de520873a11c5f062ed4df4118e25e", packages: 132 };
const sdkSourceNames = ["index.mjs", "package.json", "package-lock.json"];
// Supplemental upstream/standard terms remain distinct from the complete
// original npm archives (including their README, headers and AUTHORS bodies).
const sdkSupplements = [
  { path: "pnpm-components-LICENSE", bytes: 1170, sha256: "127161487fae720d42adf2bbb7e5e047174fc443050e799c3fb2cad407d721ab",
    package: "@pnpm/util.lex-comparator@3.0.2", kind: "version-bound-upstream-terms",
    uri: "https://github.com/pnpm/components/blob/5a7eefc9f6242e7c5f7d97348592ca87a796a880/LICENSE" },
  { path: "CC-BY-3.0.txt", bytes: 19467, sha256: "e6bc9e9c474700b708f568bac9e5a8a9bcb2b1dad53442f5ba449fcb848b8e76",
    package: "spdx-exceptions@2.5.0", kind: "official-standard-legal-code-supplement", uri: "https://creativecommons.org/licenses/by/3.0/legalcode.txt" },
  { path: "CC0-1.0.txt", bytes: 7048, sha256: "a2010f343487d3f7618affe54f789f5487602331c0a8d03f49e9a7c547cf0499",
    package: "spdx-license-ids@3.0.24", kind: "official-standard-legal-code-supplement", uri: "https://creativecommons.org/publicdomain/zero/1.0/legalcode.txt" },
];

function sdkPackages(source, policy) {
  const raw = readRegular(path.join(source, "package-lock.json"), metadataLimit);
  if (sha256(raw) !== policy.lock) fail("SDK original lock differs");
  const lock = json(raw);
  if (lock.lockfileVersion !== 3 || !lock.packages || Object.keys(lock.packages).length !== policy.packages + 1)
    fail("SDK original package membership differs");
  return Object.entries(lock.packages).filter(([name]) => name !== "").sort(([a], [b]) => a.localeCompare(b, "en")).map(([install, p]) => {
    if (!/^node_modules\/(?:@[^/]+\/)?[^/]+(?:\/node_modules\/(?:@[^/]+\/)?[^/]+)*$/.test(install) || p.link || !p.version)
      fail("SDK native install path differs");
    const u = new URL(p.resolved);
    if (u.origin !== registry.slice(0, -1) || u.username || u.password || u.search || u.hash)
      fail("SDK original registry differs");
    return { install, name: p.name || install.slice(install.lastIndexOf("node_modules/") + 13), version: p.version,
      integrity: archiveIntegrity(p.integrity), tarball: p.resolved, dev: p.dev === true, optional: p.optional === true, peer: p.peer === true };
  });
}

// Raw installed-file authentication uses only Node built-ins. In-root npm bin
// links are observed explicitly; no package code is loaded while checking it.
function sdkInstallation(sdk) {
  let config;
  try { config = fs.lstatSync(path.join(sdk, ".npmrc")); } catch (error) { if (error.code !== "ENOENT") throw error; }
  if (config) fail("unbound SDK project configuration");
  const root = path.join(sdk, "node_modules"), files = [], links = [], directories = [];
  if (fs.realpathSync(root) !== root || !fs.lstatSync(root).isDirectory()) fail("physical SDK installation required");
  let total = 0, entries = 0;
  function visit(dir) {
    const before = fs.lstatSync(dir);
    if (!before.isDirectory() || fs.realpathSync(dir) !== dir || (before.mode & 0o7777) !== 0o755) fail("unsafe SDK directory");
    directories.push({ path: path.relative(sdk, dir).split(path.sep).join("/"), mode: 0o755 });
    const names = fs.readdirSync(dir).sort();
    for (const name of names) {
      if (++entries > 50000) fail("SDK installation exceeds member bound");
      const full = path.join(dir, name), relative = path.relative(sdk, full).split(path.sep).join("/"), info = fs.lstatSync(full);
      if (info.isDirectory()) visit(full);
      else if (info.isSymbolicLink()) {
        const target = fs.readlinkSync(full), resolved = path.resolve(dir, target);
        if (!relative.includes("/.bin/") || path.isAbsolute(target) || !resolved.startsWith(root + "/") || fs.realpathSync(resolved) !== resolved || !fs.lstatSync(resolved).isFile())
          fail("unsafe SDK operational link");
        links.push({ path: relative, target });
      } else {
        const body = readRegular(full, packageLimit);
        total += body.length;
        if (total > 128 * 1024 * 1024 || files.length >= 50000) fail("SDK installation exceeds bounds");
        files.push({ path: relative, bytes: body.length, mode: info.mode & 0o777, sha256: sha256(body) });
      }
    }
    const after = fs.lstatSync(dir);
    if (before.ino !== after.ino || before.dev !== after.dev || before.mtimeMs !== after.mtimeMs || JSON.stringify(names) !== JSON.stringify(fs.readdirSync(dir).sort()))
      fail("SDK installation changed during observation");
  }
  visit(root);
  return { files: files.sort((a, b) => a.path.localeCompare(b.path, "en")), links: links.sort((a, b) => a.path.localeCompare(b.path, "en")),
    directories: directories.sort((a, b) => a.path.localeCompare(b.path, "en")) };
}

function sdkNpmIdentity(npmCLI) {
  const node = fs.realpathSync(process.execPath);
  const cli = fs.realpathSync(npmCLI || path.resolve(path.dirname(node), "../lib/node_modules/npm/bin/npm-cli.js"));
  const original = readRegular(fs.realpathSync(path.resolve(path.dirname(cli), "../package.json")), metadataLimit);
  const metadata = json(original);
  if (metadata.name !== "npm" || !/^\d+\.\d+\.\d+$/.test(metadata.version)) fail("native npm identity incomplete");
  return { node, cli, nodeVersion: process.versions.node, nodeSha256: sha256(readRegular(node, packageLimit)), npmVersion: metadata.version,
    npmCliSha256: sha256(readRegular(cli, metadataLimit)), npmPackageSha256: sha256(original) };
}

function sdkNpm(args, work, cache, npmCLI) {
  const identity = sdkNpmIdentity(npmCLI);
  for (const name of ["userconfig", "globalconfig"]) {
    const full = path.join(work, name);
    if (!fs.existsSync(full)) writeNew(work, name, Buffer.alloc(0));
    if (readRegular(full, 0, 0o600).length !== 0) fail("empty SDK npm configuration required");
  }
  const result = spawnSync(identity.node, [identity.cli, "--userconfig", path.join(work, "userconfig"), "--globalconfig", path.join(work, "globalconfig"),
    "--cache", cache, "--offline", "--ignore-scripts", "--audit=false", "--fund=false", ...args],
    { cwd: work, encoding: "utf8", env: { PATH: path.dirname(identity.node) + ":/usr/bin:/bin", HOME: work }, timeout: 120000, maxBuffer: metadataLimit });
  if (result.error || result.status !== 0) fail("native SDK npm operation failed");
  return result.stdout;
}

function sdkBootstrapReceipt(manifest, seal) {
  return { schemaVersion: 1, profile: manifest.profile, manifestSha256: seal, sourceManifestSha256: manifest.sourceManifestSha256,
    toolSourceSha256: manifest.toolSourceSha256, recipeSha256: manifest.recipeSha256, packages: manifest.packages.length,
    sdkReconstructed: false, nativeOfflineReplayProved: false };
}

export async function captureSDKBootstrap({ sdkRoot, sourceManifestSha256, toolSourceSha256, recipeSha256, output, scratch,
  policy = sdkBootstrapPolicy, acquire = downloadOriginal, npmCLI }) {
  for (const value of [sourceManifestSha256, toolSourceSha256, recipeSha256]) if (!digest(value)) fail("SDK source bindings required");
  separate(sdkRoot, output); separate(sdkRoot, scratch); separate(output, scratch);
  const outputReservation = reserve(output), scratchReservation = reserve(scratch);
  const packages = sdkPackages(sdkRoot, policy), before = sdkInstallation(sdkRoot), expected = new Set(["node_modules/.package-lock.json"]), expectedLinks = new Map();
  const records = [];
  const put = (name, body) => { writeNew(output, name, body); records.push(record(output, name)); };
  for (const name of sdkSourceNames) put("records/sdk/" + name, readRegular(path.join(sdkRoot, name), metadataLimit));
  for (const term of sdkSupplements) {
    const body = readRegular(path.resolve(here, "../../third_party/zitadel-pnpm-sdk", term.path), term.bytes);
    if (body.length !== term.bytes || sha256(body) !== term.sha256) fail("SDK supplemental terms differ");
    put("terms/" + term.path, body);
  }
  loadNativeSDK();
  for (let i = 0; i < packages.length; i++) {
    const p = packages[i], archive = await acquire(p);
    if (!Buffer.isBuffer(archive) || archive.length === 0 || archive.length > packageLimit || "sha512-" + createHash("sha512").update(archive).digest("base64") !== p.integrity)
      fail("SDK original archive integrity differs");
    const validation = path.join(scratch, "package-" + i); fs.mkdirSync(validation, { mode: 0o700 });
    const native = createCafs(validation).addFilesFromTarball(gunzipSync(archive, { maxOutputLength: 512 * 1024 * 1024 }), true);
    if (native.manifest?.name !== p.name || native.manifest?.version !== p.version) fail("SDK original package identity differs");
    const files = fileMap(native.filesIndex), binRoot = path.join(validation, "bin-view");
    if (!native.manifest.bin && native.manifest.directories?.bin) {
      fs.mkdirSync(binRoot, { mode: 0o700 });
      for (const name of Object.keys(files)) writeNew(binRoot, name, Buffer.alloc(0));
    }
    const commands = await getBinsFromPackageManifest(native.manifest, binRoot);
    const bins = new Set(commands.map(c => path.relative(binRoot, c.path).split(path.sep).join("/")));
    let binIndex = 0;
    for (const [name, f] of Object.entries(files)) {
      const relative = p.install + "/" + name, full = memberPath(sdkRoot, relative), mode = bins.has(name) ? 0o755 : nativeCafsMode(f.mode);
      const body = readRegular(full, f.size, mode);
      let expectedIntegrity = f.integrity, expectedSize = f.size;
      if (bins.has(name)) {
        // npm fixes executable permissions and a CRLF shebang only. Ask the
        // unchanged native bin-links implementation to interpret that transition
        // on a private copy; the original archive and CAFS bytes stay untouched.
        const original = readRegular(getFilePathByModeInCafs(validation, f.integrity, f.mode), f.size);
        const copy = path.join(validation, "normalized-bin-" + binIndex++);
        writeNew(validation, path.basename(copy), original);
        await sdkRequire("bin-links/lib/fix-bin")(copy);
        const normalized = readRegular(copy, f.size, 0o755);
        expectedIntegrity = "sha512-" + createHash("sha512").update(normalized).digest("base64");
        expectedSize = normalized.length;
      }
      if (body.length !== expectedSize || "sha512-" + createHash("sha512").update(body).digest("base64") !== expectedIntegrity) fail("SDK installed bytes differ from original");
      expected.add(relative);
    }
    for (const command of commands) {
      const prefix = p.install.slice(0, p.install.lastIndexOf("node_modules/") + 12);
      const link = prefix + "/.bin/" + command.name, target = path.posix.relative(path.posix.dirname(link), p.install + "/" + path.relative(binRoot, command.path).split(path.sep).join("/"));
      if (!expectedLinks.has(link)) expectedLinks.set(link, new Set());
      expectedLinks.get(link).add(target);
    }
    p.archive = "archives/" + sha256(archive) + ".tgz";
    put(p.archive, archive);
  }
  assert.deepEqual(before.files.map(f => f.path), sorted(expected).sort((a, b) => a.localeCompare(b, "en")), "SDK native membership differs from originals");
  assert.deepEqual(before.links.map(link => link.path), [...expectedLinks.keys()].sort((a, b) => a.localeCompare(b, "en")), "SDK native command membership differs");
  for (const link of before.links)
    if (!expectedLinks.get(link.path).has(link.target)) fail("SDK native command target differs from originals");
  const expectedDirectories = new Set(["node_modules"]);
  for (const name of [...expected, ...expectedLinks.keys()])
    for (let parent = path.posix.dirname(name); parent !== "."; parent = path.posix.dirname(parent)) expectedDirectories.add(parent);
  assert.deepEqual(before.directories, [...expectedDirectories].sort((a, b) => a.localeCompare(b, "en")).map(path => ({ path, mode: 0o755 })), "SDK native directory membership differs");
  const identity = sdkNpmIdentity(npmCLI);
  const composition = sdkNpm(["ls", "--all", "--json", "--prefix", sdkRoot], scratch, path.join(scratch, "npm-cache"), npmCLI);
  json(Buffer.from(composition));
  put("records/composition.json", Buffer.from(composition));
  put("records/installation.json", encoded(before));
  assert.deepEqual(sdkInstallation(sdkRoot), before, "SDK changed during original capture");
  const manifest = { schemaVersion: 1, profile: "identity-pnpm-sdk-bootstrap-v1", sourceManifestSha256, toolSourceSha256, recipeSha256,
    runtime: { nodeVersion: identity.nodeVersion, nodeSha256: identity.nodeSha256, npmVersion: identity.npmVersion, npmCliSha256: identity.npmCliSha256, npmPackageSha256: identity.npmPackageSha256 },
    packages, supplements: sdkSupplements, files: records.sort((a, b) => a.path.localeCompare(b.path, "en")) };
  const raw = encoded(manifest), seal = sha256(raw); writeNew(output, "manifest.json", raw);
  sameDirectory(output, outputReservation); sameDirectory(scratch, scratchReservation);
  return verifySDKBootstrap({ capsule: output, sha256: seal, sourceManifestSha256, toolSourceSha256, recipeSha256, sdkRoot, policy, npmCLI });
}

export function verifySDKBootstrap({ capsule, sha256: seal, sourceManifestSha256, toolSourceSha256, recipeSha256, sdkRoot, sourceRoot = sdkRoot || here, policy = sdkBootstrapPolicy, npmCLI }) {
  for (const value of [sourceManifestSha256, toolSourceSha256, recipeSha256]) if (!digest(value)) fail("SDK source bindings required");
  const reservation = privateDirectory(capsule), raw = readRegular(path.join(capsule, "manifest.json"), metadataLimit, 0o600);
  if (!digest(seal) || sha256(raw) !== seal) fail("SDK bootstrap seal differs");
  const manifest = json(raw);
  if (!encoded(manifest).equals(raw) || manifest.schemaVersion !== 1 || manifest.profile !== "identity-pnpm-sdk-bootstrap-v1" || manifest.sourceManifestSha256 !== sourceManifestSha256 || manifest.toolSourceSha256 !== toolSourceSha256 || manifest.recipeSha256 !== recipeSha256 || !Array.isArray(manifest.files))
    fail("SDK bootstrap context differs");
  const payloads = sdkSourceNames.map(n => "records/sdk/" + n).concat(["records/composition.json", "records/installation.json"]);
  assert.deepEqual(manifest.supplements, sdkSupplements, "SDK supplemental terms identities differ");
  for (const term of sdkSupplements) {
    const body = readRegular(path.join(capsule, "terms", term.path), term.bytes, 0o600);
    if (body.length !== term.bytes || sha256(body) !== term.sha256) fail("SDK retained supplemental terms differ");
    payloads.push("terms/" + term.path);
  }
  const packages = sdkPackages(path.join(capsule, "records/sdk"), policy);
  if (manifest.packages.length !== packages.length) fail("SDK bootstrap package membership differs");
  for (let i = 0; i < packages.length; i++) {
    const { archive, ...retained } = manifest.packages[i];
    assert.deepEqual(retained, packages[i], "SDK retained lock selection differs");
    const body = readRegular(memberPath(capsule, archive), packageLimit, 0o600);
    if (archive !== "archives/" + sha256(body) + ".tgz" || "sha512-" + createHash("sha512").update(body).digest("base64") !== retained.integrity) fail("SDK retained archive differs");
    payloads.push(archive);
  }
  assert.deepEqual(manifest.files.map(f => f.path), payloads.sort((a, b) => a.localeCompare(b, "en")), "SDK bootstrap file membership differs");
  for (const f of manifest.files) assert.deepEqual(record(capsule, f.path), f, "SDK retained payload changed");
  closedMembers(capsule, [...payloads, "manifest.json"]);
  for (const name of sdkSourceNames)
    if (!readRegular(path.join(sourceRoot, name), metadataLimit).equals(readRegular(path.join(capsule, "records/sdk", name), metadataLimit, 0o600))) fail("SDK source differs from authenticated original");
  if (sdkRoot) {
    assert.deepEqual(sdkInstallation(sdkRoot), json(readRegular(path.join(capsule, "records/installation.json"), metadataLimit, 0o600)), "SDK installed payload differs");
  }
  const identity = sdkNpmIdentity(npmCLI);
  assert.deepEqual(manifest.runtime, { nodeVersion: identity.nodeVersion, nodeSha256: identity.nodeSha256, npmVersion: identity.npmVersion, npmCliSha256: identity.npmCliSha256, npmPackageSha256: identity.npmPackageSha256 }, "SDK native tool identity differs");
  sameDirectory(capsule, reservation);
  return sdkBootstrapReceipt(manifest, seal);
}

export async function recaptureBuild({ build, sourceManifestSha256, capsule, sha256: seal, output, scratch, originalScratch, expectedLock = frozenLock }) {
  for (const writable of [output, scratch, originalScratch]) separate(capsule, writable);
  separate(originalScratch, output); separate(originalScratch, scratch);
  const original = await verifyCapsule({ capsule, sha256: seal, scratch: originalScratch, expectedLock });
  if (original.sourceManifestSha256 !== sourceManifestSha256) fail("retained pnpm source context differs");
  const manifest = json(readRegular(path.join(capsule, "manifest.json"), metadataLimit, 0o600));
  const selected = new Map(manifest.packages.map(p => [p.depPath, p]));
  const result = await captureBuild({ build, sourceManifestSha256, output, scratch, expectedLock, acquire: async p => {
    const previous = selected.get(p.depPath);
    if (!previous) fail("recaptured native package is outside original selection");
    const { archive, index, ...identity } = previous;
    assert.deepEqual(p, identity, "recaptured native package identity differs");
    return readRegular(memberPath(capsule, archive), packageLimit, 0o600);
  } });
  if (result.packages !== original.packages) fail("recaptured native package set incomplete");
  const final = await verifyCapsule({ capsule, sha256: seal, scratch: path.join(originalScratch, "final-readback"), expectedLock });
  assert.deepEqual(final, original, "original pnpm capsule changed during recapture");
  return result;
}

export async function reconstructSDKBootstrap({ capsule, sha256: seal, sourceManifestSha256, toolSourceSha256, recipeSha256,
  sourceRoot = here, output, scratch, policy = sdkBootstrapPolicy, npmCLI }) {
  separate(capsule, output); separate(capsule, scratch); separate(output, scratch); separate(sourceRoot, output); separate(sourceRoot, scratch);
  const args = { capsule, sha256: seal, sourceManifestSha256, toolSourceSha256, recipeSha256, sourceRoot, policy, npmCLI };
  const original = verifySDKBootstrap(args);
  const outputReservation = reserve(output), scratchReservation = reserve(scratch);
  for (const name of sdkSourceNames) writeNew(output, name, readRegular(path.join(capsule, "records/sdk", name), metadataLimit, 0o600));
  const cache = path.join(scratch, "native-cache"); reserve(cache);
  const manifest = json(readRegular(path.join(capsule, "manifest.json"), metadataLimit, 0o600));
  for (const p of manifest.packages) {
    const body = readRegular(memberPath(capsule, p.archive), packageLimit, 0o600);
    if ("sha512-" + createHash("sha512").update(body).digest("base64") !== p.integrity) fail("SDK original archive changed before native cache seeding");
    sdkNpm(["cache", "add", memberPath(capsule, p.archive)], scratch, cache, npmCLI);
  }
  sdkNpm(["ci", "--prefix", output], scratch, cache, npmCLI);
  // This built-in-only complete readback precedes every external SDK import.
  verifySDKBootstrap({ ...args, sdkRoot: output });
  const composition = sdkNpm(["ls", "--all", "--json", "--prefix", output], scratch, cache, npmCLI);
  if (!readRegular(path.join(capsule, "records/composition.json"), metadataLimit, 0o600).equals(Buffer.from(composition)))
    fail("reconstructed SDK native composition differs");
  verifySDKBootstrap({ ...args, sdkRoot: output });
  sameDirectory(output, outputReservation); sameDirectory(scratch, scratchReservation);
  return { ...original, sdkReconstructed: true, installationMatched: true, compositionMatched: true };
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
      !["--build", "--source-sha256", "--output", "--scratch", "--capsule", "--sha256", "--store", "--tool-source-sha256", "--recipe-sha256", "--sdk-root",
        "--sdk-bootstrap", "--sdk-bootstrap-sha256", "--sdk-source-sha256", "--sdk-tool-source-sha256", "--sdk-recipe-sha256",
        "--original-capsule", "--original-sha256", "--original-scratch"].includes(key) ||
      !value ||
      values[key]
    )
      fail("one complete explicit pnpm operation required");
    values[key] = value;
  }
  const guardNames = ["--sdk-bootstrap", "--sdk-bootstrap-sha256", "--sdk-source-sha256", "--sdk-tool-source-sha256", "--sdk-recipe-sha256"];
  let sdkGuard;
  if (guardNames.some(name => values[name])) {
    if (guardNames.some(name => !values[name]) || operation.startsWith("bootstrap-")) fail("complete exclusive SDK authentication required");
    sdkGuard = { capsule: values["--sdk-bootstrap"], sha256: values["--sdk-bootstrap-sha256"], sourceManifestSha256: values["--sdk-source-sha256"],
      toolSourceSha256: values["--sdk-tool-source-sha256"], recipeSha256: values["--sdk-recipe-sha256"], sdkRoot: here };
    verifySDKBootstrap(sdkGuard);
    for (const name of guardNames) delete values[name];
  }
  let result;
  if (operation === "bootstrap-capture" && Object.keys(values).length === 5 && values["--source-sha256"] && values["--tool-source-sha256"] && values["--recipe-sha256"] && values["--output"] && values["--scratch"]) {
    result = await captureSDKBootstrap({ sdkRoot: here, sourceManifestSha256: values["--source-sha256"], toolSourceSha256: values["--tool-source-sha256"],
      recipeSha256: values["--recipe-sha256"], output: values["--output"], scratch: values["--scratch"] });
  } else if (["bootstrap-verify", "bootstrap-restore"].includes(operation) && Object.keys(values).length === (operation === "bootstrap-restore" ? 7 : values["--sdk-root"] ? 6 : 5) &&
    values["--capsule"] && values["--sha256"] && values["--source-sha256"] && values["--tool-source-sha256"] && values["--recipe-sha256"] &&
    (operation !== "bootstrap-restore" || values["--output"] && values["--scratch"])) {
    const options = { capsule: values["--capsule"], sha256: values["--sha256"], sourceManifestSha256: values["--source-sha256"],
      toolSourceSha256: values["--tool-source-sha256"], recipeSha256: values["--recipe-sha256"] };
    result = operation === "bootstrap-restore" ? await reconstructSDKBootstrap({ ...options, output: values["--output"], scratch: values["--scratch"] }) :
      verifySDKBootstrap({ ...options, sdkRoot: values["--sdk-root"] });
  } else if (operation === "recapture" && Object.keys(values).length === 7 && values["--build"] && values["--source-sha256"] && values["--output"] && values["--scratch"] && values["--original-capsule"] && values["--original-sha256"] && values["--original-scratch"]) {
    result = await recaptureBuild({ build: values["--build"], sourceManifestSha256: values["--source-sha256"], output: values["--output"], scratch: values["--scratch"],
      capsule: values["--original-capsule"], sha256: values["--original-sha256"], originalScratch: values["--original-scratch"] });
  } else if (operation === "inspect" && Object.keys(values).length === 1 && values["--build"]) {
    result = await inspectBuild({ build: values["--build"] });
  } else if (
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
  if (sdkGuard) verifySDKBootstrap(sdkGuard);
  process.stdout.write(`${JSON.stringify(result)}\n`);
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url))
  main(process.argv.slice(2)).catch((error) => {
    process.stderr.write(`zitadel-pnpm-source: ${JSON.stringify(failureDiagnostic(error))}\n`);
    process.exitCode = 1;
  });
