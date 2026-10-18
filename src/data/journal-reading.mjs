import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { readFileSync, readdirSync, openSync, fstatSync, lstatSync, closeSync, constants, mkdirSync, writeFileSync, existsSync } from "node:fs";
import { basename, join } from "node:path";
import { fileURLToPath } from "node:url";
import { parse, parseFragment, serialize } from "parse5";
import { readJournal, journalPaths } from "./journal.mjs";

const root = fileURLToPath(new URL("../../", import.meta.url));
const generated = join(root, ".astro", "journal-reading.json");
const input = join(root, ".astro", "journal-reading-input.txt");
/** @typedef {{nodeName:string,tagName?:string,value?:string,attrs?:{name:string,value:string}[],childNodes?:MarkupNode[],parentNode?:MarkupNode|null}} MarkupNode */
/** @param {MarkupNode} node @returns {MarkupNode[]} */
const all = node => [node, ...(node.childNodes ?? []).flatMap(all)];
/** @param {MarkupNode|undefined} node @param {string} name */
const attr = (node, name) => node?.attrs?.find(item => item.name === name)?.value;
/** @param {MarkupNode|undefined} node @returns {string} */
const text = node => { assert.ok(node, "Journal expected text node missing"); return node.nodeName === "#text" ? node.value ?? "" : (node.childNodes ?? []).map(text).join(""); };
/** @param {MarkupNode} node @param {string} name */
const hasClass = (node, name) => (attr(node, "class") ?? "").split(/\s+/).includes(name);
/** @param {string|Buffer} value */
const digest = value => createHash("sha256").update(value).digest("hex");
// These actual Mermaid articles require their page-level renderer; #180 owns parity.
export const deferredReadingPaths = [
  "/blog/autonomous-oss-with-github-agentic-workflows/",
  "/blog/ai-powered-github-issues-with-copilot-and-claude-opus/",
];

/** Admit every eligible article; nested routes need explicit router support before adoption. */
export function readingPaths() {
  const paths = readJournal().map(post => {
    const path = new URL(post.href).pathname;
    assert.match(path, /^\/blog\/[a-z0-9-]+\/$/, "Journal nested reading route needs explicit support");
    return path;
  });
  assert.ok(deferredReadingPaths.every(path => paths.includes(path)), "Journal deferred route must remain authored");
  return paths.filter(path => !deferredReadingPaths.includes(path));
}

/**
 * Project the maintained Astro reading output, never render a second Markdown body.
 * Unsupported interactive content is rejected rather than silently omitted.
 * @param {string} html Actual Astro artifact.
 * @param {{title:string,description:string,date:string,href:string}} post Authored metadata.
 */
export function extractReading(html, post) {
  const document = all(parse(html));
  assert.equal(text(document.find(node => node.tagName === "title")), `${post.title} | Devantler Tech`, "Journal reference title mismatch");
  assert.equal(attr(document.find(node => node.tagName === "meta" && attr(node, "name") === "description"), "content"), post.description, "Journal reference description mismatch");
  assert.equal(attr(document.find(node => node.tagName === "link" && attr(node, "rel") === "canonical"), "href"), post.href, "Journal reference route mismatch");
  const bodies = document.filter(node => hasClass(node, "sl-markdown-content"));
  assert.equal(bodies.length, 1, "Journal requires one complete reading body");
  const body = bodies[0];
  const nodes = all(body);
  const headings = nodes.filter(node => /^h[2-6]$/.test(node.tagName ?? "")).map(node => ({id:attr(node, "id"),title:text(node),level:Number(node.tagName?.slice(1))}));
  assert.ok(headings.every(item => item.id), "Journal heading bookmarks required");
  assert.equal(new Set(headings.map(item => item.id)).size, headings.length, "Journal duplicate bookmark");
  const blocks = nodes.filter(node => node.tagName === "pre");
  const buttons = nodes.filter(node => node.tagName === "button");
  assert.equal(blocks.length, buttons.length, "Journal each code block requires its actual copy control");
  for (const button of buttons) {
    const payload = attr(button, "data-code");
    // Expressive Code deliberately omits comment-only lines from copied code.
    // Retain its actual rendered payload; do not reconstruct it from highlighting.
    const frame = button.parentNode?.parentNode;
    const lines = frame ? all(frame).filter(node => hasClass(node, "ec-line")) : [];
    const commentOnly = lines.length > 0 && lines.every(line => /^\s*#/.test(text(line)));
    assert.ok(typeof payload === "string" && (payload.trim() || commentOnly), "Journal actual copy payload required");
  }
  for (const node of nodes) {
    if (node.tagName === "table") {
      const parent = node.parentNode;
      assert.ok(parent?.childNodes, "Journal table parent missing");
      // Scroll the comparison independently, without changing native table semantics.
      const wrapper = /** @type {MarkupNode} */ (parseFragment('<div class="reading-table" tabindex="0" role="region" aria-label="Scrollable table"></div>').childNodes[0]);
      wrapper.parentNode = parent;
      wrapper.childNodes = [node];
      parent.childNodes = parent.childNodes.map(child => child === node ? wrapper : child);
      node.parentNode = wrapper;
    }
    if (node.tagName === "a") {
      const href = node.attrs?.find(item => item.name === "href");
      if (href && !href.value.startsWith("#")) href.value = new URL(href.value, post.href).href;
    }
    if (hasClass(node, "sl-blog-pagination")) {
      // Reuse the renderer's one real adjacent navigation, rather than adding
      // another copy beneath the preserved body.
      node.tagName = node.nodeName = "nav";
      node.attrs?.push({name:"aria-label",value:"More articles"});
    }
    assert.ok(!["iframe", "form", "input", "object", "embed"].includes(node.tagName ?? ""), "Journal unsupported interactive body");
    assert.ok(!(node.attrs ?? []).some(item => /^on/i.test(item.name)), "Journal unsupported inline handler");
    if (node.tagName === "script") {
      assert.match(attr(node, "src") ?? "", /^\/_astro\/ec\.[A-Za-z0-9_-]+\.js$/, "Journal unreviewed body script");
      assert.equal(text(node), "", "Journal inline script is unsupported");
      assert.ok(node.parentNode?.childNodes, "Journal script parent missing");
      node.parentNode.childNodes = node.parentNode.childNodes.filter(child => child !== node);
    }
  }
  const times = document.filter(node => node.tagName === "time").map(node => attr(node, "datetime"));
  assert.equal(times[0], post.date, "Journal publication date mismatch");
  assert.equal(times.length, 2, "Journal committed modification date required");
  assert.ok(times[1] && Number.isFinite(Date.parse(times[1])), "Journal committed modification date required");
  const adjacent = document.filter(node => node.tagName === "a" && ["prev", "next"].includes(attr(node, "rel") ?? "")).map(node => {
    const destination = attr(node, "href");
    assert.ok(destination, "Journal adjacent article destination missing");
    const href = new URL(destination, "https://devantler.tech");
    assert.ok(href.origin === "https://devantler.tech" && href.pathname.startsWith("/blog/") && text(node).trim(), "Journal adjacent article destination required");
    return {rel:attr(node, "rel"),href:href.href,title:text(node).trim()};
  });
  assert.ok(adjacent.length && new Set(adjacent.map(item => item.rel)).size === adjacent.length, "Journal distinct adjacent articles required");
  const metadata = document.filter(node => node.tagName === "meta" &&
    (attr(node, "property")?.startsWith("og:") || ["author", "twitter:card", "twitter:image"].includes(attr(node, "name") ?? "")))
    .map(node => ({name:attr(node,"name"),property:attr(node,"property"),content:attr(node,"content")}));
  const structuredData = document.filter(node => node.tagName === "script" && attr(node,"type") === "application/ld+json")
    .map(node => JSON.stringify(JSON.parse(text(node))).replace(/</g, "\\u003c"));
  return {html:serialize(/** @type {import('parse5').DefaultTreeAdapterMap['parentNode']} */ (body)),headings,modified:times[1],adjacent,metadata,structuredData};
}

/** Bind the artifact to actual renderer inputs and the article's committed history. */
export function readingFingerprint() {
  const hash = createHash("sha256");
  /** @param {string} path */
  function visit(path) {
    const file = join(root, path);
    // Inspect and read the same open descriptor. A path check followed by a
    // separate path read can consume replacement bytes instead of the checked file.
    assert.ok(Number.isInteger(constants.O_NOFOLLOW), "Journal requires no-follow input reads");
    const descriptor = openSync(file, constants.O_RDONLY | constants.O_NOFOLLOW | constants.O_NONBLOCK);
    try {
      const info = fstatSync(descriptor);
      hash.update(path + "\0");
      if (info.isDirectory()) {
        for (const name of readdirSync(file).sort()) visit(`${path}/${name}`);
        // Node cannot list an open directory descriptor, so prove afterwards that the
        // path still names the opened directory. A directory replaced, or swapped for a
        // symlink, while it or anything below it was read fails closed.
        const after = lstatSync(file);
        assert.ok(after.isDirectory() && after.dev === info.dev && after.ino === info.ino,
          "Journal renderer input directory changed while it was read");
      } else { assert.ok(info.isFile(), "Journal renderer input must be a file"); hash.update(readFileSync(descriptor)); }
    } finally { closeSync(descriptor); }
  }
  for (const path of ["src", "public", "astro.config.mjs", "package.json", "package-lock.json"]) visit(path);
  const env = {...process.env};
  for (const key of ["GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_PREFIX", "GIT_NAMESPACE"]) delete env[key];
  for (const path of readingPaths()) {
    const stem = `src/content/docs${path.slice(0, -1)}`;
    const sourcePath = [stem + ".md", stem + ".mdx"].find(file => existsSync(join(root, file)));
    assert.ok(sourcePath, "Journal reading source required");
    hash.update(sourcePath + "\0");
    hash.update(execFileSync("git", ["--no-replace-objects", "log", "-1", "--format=%cI", "--", sourcePath], {cwd:root,encoding:"utf8",env}));
  }
  return hash.digest("hex");
}

/** Record the source boundary before the authoritative renderer starts. */
export function prepareReadingReference() {
  mkdirSync(join(root, ".astro"), {recursive:true});
  writeFileSync(input, readingFingerprint());
}

/** Capture only reviewed reading bytes/assets after a successful Astro render. */
export function captureReadingReference() {
  const fingerprint = readingFingerprint();
  assert.equal(readFileSync(input, "utf8"), fingerprint, "Journal source changed during rendering");
  const paths = readingPaths();
  const admitted = new Set([...paths, ...journalPaths()]);
  /** @type {{filename:string,source:string,digest:string}[]} */
  const assets = [];
  /** @param {string} href */
  const projectAsset = href => {
    assert.match(href, /^(?:\/_astro\/[A-Za-z0-9._-]+\.(?:webp|png|jpg|css)|\/author-avatar\.png)$/, "Journal unsupported body asset");
    const bytes = readFileSync(join(root, "dist", href));
    if (href.endsWith(".css")) assert.ok(!/@import|url\(\s*["']?(?!data:)[^"')\s]/i.test(bytes.toString()), "Journal stylesheet has unsupported dependencies");
    const filename = `_app/immutable/assets/reading-${digest(bytes).slice(0, 16)}-${basename(href)}`;
    if (!assets.some(asset => asset.filename === filename)) assets.push({filename,source:bytes.toString("base64"),digest:digest(bytes)});
    return `/${filename}`;
  };
  const readings = readJournal().filter(post => paths.includes(new URL(post.href).pathname)).map(post => {
    const path = new URL(post.href).pathname;
    const reading = extractReading(readFileSync(join(root, "dist", path, "index.html"), "utf8"), post);
    const fragment = parseFragment(reading.html);
    for (const node of all(fragment)) for (const item of node.attrs ?? []) {
      if (node.tagName === "a" && item.name === "href" && !item.value.startsWith("#")) {
        const href = new URL(item.value, post.href);
        if (href.origin === "https://devantler.tech" && admitted.has(href.pathname)) item.value = href.pathname + href.search + href.hash;
      }
      if (["img", "source"].includes(node.tagName ?? "") && item.name === "srcset") {
        item.value = item.value.split(",").map(entry => {
          const match = entry.trim().match(/^(\/_astro\/[A-Za-z0-9._-]+\.(?:webp|png|jpg))\s+([1-9]\d*w)$/);
          assert.ok(match, "Journal unsupported responsive body asset");
          return `${projectAsset(match[1])} ${match[2]}`;
        }).join(", ");
        continue;
      }
      if (!((node.tagName === "img" && item.name === "src") || (node.tagName === "link" && item.name === "href"))) continue;
      item.value = projectAsset(item.value);
    }
    return {path,...post,...reading,html:serialize(fragment)};
  });
  const value = {version:2,fingerprint,readings,assets};
  writeFileSync(generated, JSON.stringify({...value,checksum:digest(JSON.stringify(value))}));
}

/** Read a complete current reference; a missing/stale build never becomes an empty article. */
export function readReadingReference() {
  const value = JSON.parse(readFileSync(generated, "utf8"));
  assert.ok(value.version === 2 && value.fingerprint === readingFingerprint(), "Journal reference is stale; run bash scripts/build-journal-reference.sh");
  const {checksum,...content} = value;
  assert.equal(checksum,digest(JSON.stringify(content)), "Journal reference content is incomplete or altered");
  assert.ok(Array.isArray(value.readings), "Journal complete reading inventory required");
  /** @type {{path:string,html:string,headings:unknown[]}[]} */
  const readings = value.readings;
  assert.deepEqual(readings.map(reading => reading.path), readingPaths(), "Journal complete reading inventory required");
  assert.ok(readings.every(reading => typeof reading.html === "string" && reading.html.trim() && Array.isArray(reading.headings)), "Journal complete reading body required");
  assert.ok(value.assets?.length, "Journal asset inventory is missing");
  for (const asset of value.assets) {
    assert.match(asset.filename, /^_app\/immutable\/assets\/reading-[a-f0-9]{16}-[A-Za-z0-9._-]+$/);
    assert.equal(digest(Buffer.from(asset.source, "base64")), asset.digest, "Journal asset digest mismatch");
  }
  const filenames = new Set(value.assets.map((/** @type {{filename:string}} */ asset) => `/${asset.filename}`));
  for (const reading of readings) for (const node of all(parseFragment(reading.html))) for (const item of node.attrs ?? []) {
    if (["img", "source"].includes(node.tagName ?? "") && item.name === "srcset") {
      for (const entry of item.value.split(",")) assert.ok(filenames.has(entry.trim().split(/\s+/)[0]), "Journal referenced asset is missing");
    }
    if ((node.tagName === "img" && item.name === "src") || (node.tagName === "link" && item.name === "href")) {
      assert.ok(filenames.has(item.value), "Journal referenced asset is missing");
    }
  }
  return value;
}
