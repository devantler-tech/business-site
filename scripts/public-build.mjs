import assert from "node:assert/strict";
import { readFileSync, readdirSync, lstatSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { parse } from "parse5";

/** Snapshot only regular files, never follow a substituted directory or symlink. @param {string} root */
function files(root) {
  assert.ok(lstatSync(root).isDirectory() && !lstatSync(root).isSymbolicLink(),"Public selection requires a regular directory");
  /** @type {Map<string,Buffer>} */
  const result = new Map();
  const visit = (/** @type {string} */ dir, prefix = "") => {
    for (const entry of readdirSync(dir,{withFileTypes:true})) {
      const name = prefix + entry.name, path = resolve(dir,entry.name);
      assert.ok(!entry.isSymbolicLink(),"Public selection requires regular files");
      if (entry.isDirectory()) visit(path,name + "/");
      else { assert.ok(entry.isFile(),"Public selection requires regular files"); result.set(name,readFileSync(path)); }
    }
  };
  visit(root);
  return result;
}
/** @param {any} node @returns {any[]} */
const nodes = node => [node,...(node.childNodes ?? []).flatMap(nodes)];
/** @param {any} node @param {string} name */
const attr = (node,name) => node.attrs?.find((/** @type {any} */ item) => item.name === name)?.value;

/** Independently compare the selected route inventory and every local HTML/CSS asset dependency. @param {string} candidate @param {string} reference */
export function validateSelection(candidate,reference) {
  const selected = files(candidate), baseline = files(reference);
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
        if (node.tagName === "link" && ["stylesheet","icon","preload","modulepreload"].includes(attr(node,"rel"))) dependencies.push(attr(node,"href"));
      }
    } else if (name.endsWith(".css")) for (const match of bytes.toString().matchAll(/url\(\s*["']?([^\s"')]+)["']?\s*\)/g)) dependencies.push(match[1]);
    for (const href of dependencies) {
      if (!href || /^(?:data:|https?:|#)/.test(href)) continue;
      const url = new URL(href,`https://devantler.tech/${name}`);
      assert.ok(selected.has(decodeURIComponent(url.pathname.slice(1))),`Public selection missing dependency ${url.pathname}`);
    }
  }
  const cname = baseline.get("CNAME");
  assert.equal(cname?.toString(),"devantler.tech\n","Public selection requires the canonical CNAME");
  selected.set("CNAME",cname);
  return selected;
}

/** Replace generated output completely; no stale incumbent assets survive selection. @param {string} candidate @param {string} reference @param {string} target */
export function promoteSelection(candidate,reference,target) {
  const selected = validateSelection(candidate,reference);
  rmSync(target,{recursive:true,force:true});
  for (const [name,bytes] of selected) { const destination = resolve(target,name); mkdirSync(dirname(destination),{recursive:true}); writeFileSync(destination,bytes); }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  assert.equal(process.argv.length,2,"Public selection accepts only the repository's canonical paths");
  promoteSelection("web/build","dist","dist");
}
