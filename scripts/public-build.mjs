import assert from "node:assert/strict";
import { readFileSync, readdirSync, lstatSync, mkdirSync, writeFileSync, rmSync, openSync, fstatSync, closeSync, constants } from "node:fs";
import { createHash, randomUUID } from "node:crypto";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { parse } from "parse5";

/** Snapshot only regular files, never follow a substituted directory or symlink. @param {string} root */
function files(root) {
  /** @type {Map<string,Buffer>} */
  const result = new Map();
  const visit = (/** @type {string} */ path, name = "") => {
    const descriptor = openSync(path,constants.O_RDONLY | constants.O_NOFOLLOW | constants.O_NONBLOCK);
    try {
      const opened = fstatSync(descriptor);
      if (opened.isFile()) {
        assert.ok(name,"Public selection requires a regular directory");
        result.set(name,readFileSync(descriptor));
        return;
      }
      assert.ok(opened.isDirectory(),"Public selection requires regular files or directories");
      const sameDirectory = () => { const current=lstatSync(path); assert.ok(current.isDirectory() && current.dev===opened.dev && current.ino===opened.ino,"Public selection directory changed while reading"); };
      const entries = readdirSync(path,{withFileTypes:true});
      sameDirectory();
      for (const entry of entries) visit(resolve(path,entry.name),name ? `${name}/${entry.name}` : entry.name);
      sameDirectory();
    } finally { closeSync(descriptor); }
  };
  visit(root);
  return result;
}
/** @param {any} node @returns {any[]} */
const nodes = node => [node,...(node.childNodes ?? []).flatMap(nodes)];
/** @param {any} node @param {string} name */
const attr = (node,name) => node.attrs?.find((/** @type {any} */ item) => item.name === name)?.value;
/** @param {Buffer} bytes */
const digest = bytes => createHash("sha256").update(bytes).digest("hex");
/** Capture the adapter's completed, freshly replaced output outside the public tree.
 * @param {string} candidate @param {string} manifest @param {string|undefined} nonce */
export function capturePublicArtifact(candidate,manifest,nonce = randomUUID()) {
  assert.match(nonce,/^[a-f0-9-]{36}$/,"Invalid internal public-build nonce");
  const inventory = Object.fromEntries([...files(candidate)].map(([name,bytes])=>[name,digest(bytes)]));
  mkdirSync(dirname(manifest),{recursive:true});
  const descriptor=openSync(manifest,constants.O_WRONLY | constants.O_CREAT | constants.O_TRUNC | constants.O_NOFOLLOW,0o600);
  try { writeFileSync(descriptor,JSON.stringify({version:1,nonce,files:inventory})); }
  finally { closeSync(descriptor); }
}

/** Independently validate routes/assets and bind the complete snapshot to this actual build.
 * @param {string} candidate @param {string} reference @param {{nonce:string,manifest:string}|undefined} options */
export function validateSelection(candidate,reference,options) {
  const selected = files(candidate), baseline = files(reference);
  const actual = new Map(selected);
  for (const name of selected.keys()) {
    assert.ok(name !== "publication-source.json","Public selection cannot copy a publisher receipt");
    assert.ok(!name.split("/").some(part => part.startsWith(".") || ["portal","server"].includes(part)) && !/\.(?:go|sql|pem|key)$/.test(name),"Public selection contains private output");
    assert.notEqual(name,"CNAME","Public selection owns CNAME, not the candidate");
  }
  // Preserve only the two inert, identity-off compatibility redirects. Never
  // copy an incumbent page, asset tree or company disclosure over the candidate.
  for (const name of ["company/index.html","da/company/index.html"]) {
    const bytes = baseline.get(name);
    if (!selected.has(name) && bytes) {
      const html = bytes.toString();
      assert.match(html,/<meta name="robots" content="noindex">/);
      assert.match(html,/<meta http-equiv="refresh" content="2;url=\/(?:da\/)?">/);
      assert.ok(!/<(?:script|style|img|link\b[^>]*rel="stylesheet")\b/i.test(html),"Compatibility redirect must be inert");
      selected.set(name,bytes);
    }
  }
  for (const name of ["index.html","da/index.html",...baseline.keys()].filter(name => name.endsWith(".html"))) assert.ok(selected.has(name),`Public selection missing route ${name}`);
  for (const name of selected.keys()) if (name.endsWith(".html")) assert.ok(baseline.has(name),`Public selection unexpected route ${name}`);
  for (const [name,bytes] of selected) {
    const dependencies = [];
    if (name.endsWith(".html")) {
      for (const node of nodes(parse(bytes.toString()))) {
        if (["script","img","source"].includes(node.tagName)) {
          if (attr(node,"src")) dependencies.push(attr(node,"src"));
          for (const entry of (attr(node,"srcset") ?? "").split(",")) if (entry.trim()) dependencies.push(entry.trim().split(/\s+/)[0]);
        }
        const rel = (attr(node,"rel") ?? "").toLowerCase().split(/\s+/);
        if (node.tagName === "link" && rel.some((/** @type {string} */ token)=>["stylesheet","icon","apple-touch-icon","apple-touch-icon-precomposed","mask-icon","preload","modulepreload","manifest"].includes(token))) dependencies.push(attr(node,"href"));
      }
    } else if (name.endsWith(".css")) for (const match of bytes.toString().matchAll(/url\(\s*["']?([^\s"')]+)["']?\s*\)/g)) dependencies.push(match[1]);
    for (const href of dependencies) {
      if (!href || /^(?:data:|#)/.test(href)) continue;
      const url = new URL(href,`https://devantler.tech/${name}`);
      if (url.origin !== "https://devantler.tech") continue;
      assert.ok(selected.has(decodeURIComponent(url.pathname.slice(1))),`Public selection missing dependency ${url.pathname}`);
    }
  }
  assert.ok(options,"Public selection requires a current build inventory");
  const inventory=JSON.parse(readFileSync(options.manifest,"utf8"));
  assert.ok(inventory.version===1 && inventory.nonce===options.nonce,"Public selection inventory does not belong to the current build");
  assert.ok(inventory.files && typeof inventory.files==="object" && !Array.isArray(inventory.files),"Public selection missing current file inventory");
  assert.deepEqual([...actual.keys()].sort(),Object.keys(inventory.files).sort(),"Public selection stale or unexpected file inventory");
  for (const [name,bytes] of actual) {
    assert.equal(digest(bytes),inventory.files[name],`Public selection stale file: ${name}`);
    assert.ok(!bytes.includes(Buffer.from(options.nonce)),"Public selection leaked its private build marker");
  }
  const cname = baseline.get("CNAME");
  assert.ok(cname,"Public selection missing canonical CNAME");
  assert.equal(cname?.toString(),"devantler.tech\n","Public selection requires the canonical CNAME");
  selected.set("CNAME",cname);
  return selected;
}

/** Replace generated output completely; no stale incumbent assets survive selection.
 * @param {string} candidate @param {string} reference @param {string} target @param {{nonce:string,manifest:string}} options */
export function promoteSelection(candidate,reference,target,options) {
  const selected = validateSelection(candidate,reference,options);
  rmSync(target,{recursive:true,force:true});
  for (const [name,bytes] of selected) { const destination = resolve(target,name); mkdirSync(dirname(destination),{recursive:true}); writeFileSync(destination,bytes); }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  assert.equal(process.argv.length,3,"Public selection requires its caller's current build nonce");
  promoteSelection("web/build","dist","dist",{nonce:process.argv[2],manifest:".astro/selected-public-artifact.json"});
}
