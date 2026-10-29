import assert from "node:assert/strict";
import { constants, openSync, fstatSync, readFileSync, closeSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { createHash } from "node:crypto";
import { basename, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import sharp from "sharp";
import { cv } from "./cv.ts";
import { renderCvPdf } from "../lib/cv-pdf.ts";
import { readJournal } from "./journal.mjs";
import { articleSource, guideMetadata, guidePaths, readingFingerprint, prepareReadingReference, captureReadingReference } from "./journal-reading.mjs";
import { parseFrontmatter } from "./frontmatter.mjs";
import { renderMarkdownBody, escapeHtml as e } from "./markdown.mjs";
import { journalConfig, journalDescription } from "../components/business/journal-config.js";
import { captureJournalFeedReference } from "./journal-feed-reference.mjs";
import { capturePublicDiscoveryReference } from "./public-discovery-reference.mjs";
import { committedContentDate } from "./content-history.mjs";

const root = new URL("../../", import.meta.url);
const output = new URL(".generated/content/", root);
const hash = bytes => createHash("sha256").update(bytes).digest("hex").slice(0,16);
function input(file) {
  assert.ok(file.href.startsWith(root.href), "Content input escapes the repository");
  const fd = openSync(file, constants.O_RDONLY | constants.O_NOFOLLOW | constants.O_NONBLOCK);
  try { assert.ok(fstatSync(fd).isFile(), "Content input must be a regular file"); return readFileSync(fd); }
  finally { closeSync(fd); }
}
function write(path, bytes) {
  const file = new URL(path.replace(/^\//, ""), output);
  assert.ok(file.href.startsWith(output.href), "Content output escapes its owned directory");
  mkdirSync(dirname(fileURLToPath(file)), {recursive:true});
  writeFileSync(file, bytes);
}
const imageCache = new Map();
async function image(file) {
  if (imageCache.has(file.href)) return imageCache.get(file.href);
  const source = input(file);
  const bytes = await sharp(source).rotate().webp({quality:80}).toBuffer();
  const info = await sharp(bytes).metadata();
  // Preserve the public resource namespace for feed readers and bookmarked images.
  const src = `/_astro/${basename(file.pathname).replace(/\.[^.]+$/, "")}.${hash(bytes)}.webp`;
  write(src,bytes);
  const result = {src,width:info.width,height:info.height,loading:"lazy",decoding:"async"};
  imageCache.set(file.href,result);
  return result;
}
const attributes = values => Object.entries(values).map(([name,value]) => ` ${name}="${e(value)}"`).join("");
const dateLabel = date => new Intl.DateTimeFormat("en", {year:"numeric",month:"short",day:"numeric",timeZone:"UTC"}).format(new Date(date));
function social(post, structured) {
  const values = {"og:title":post.title,"og:url":post.href,"og:locale":"en","og:description":post.description,"og:site_name":"Devantler Tech","og:image":"https://devantler.tech/author.png","og:type":"website"};
  return `<title>${e(post.title)} | Devantler Tech</title><meta name="description" content="${e(post.description)}"><link rel="canonical" href="${e(post.href)}">` +
    Object.entries(values).map(([property,content])=>`<meta property="${property}" content="${e(content)}">`).join("") +
    '<meta name="twitter:card" content="summary_large_image"><meta name="twitter:image" content="https://devantler.tech/author.png"><meta name="author" content="Nikolai Emil Damm">' +
    (structured ? `<script type="application/ld+json">${JSON.stringify(structured).replaceAll("<","\\u003c")}</script>` : "");
}
const guideSequence = ["/about/","/agentic-engineering/","/projects/","/templates/",...guidePaths().filter(path=>path.startsWith("/templates/") && path!=="/templates/")];
function neighbours(path, posts, technical) {
  const sequence = technical ? guideSequence.map(path=>({href:`https://devantler.tech${path}`,title:path==="/about/"?"About Devantler Tech":path==="/projects/"?"Projects":guideMetadata(path).title})) : posts;
  const index = sequence.findIndex(post=>new URL(post.href).pathname===path);
  return [["prev",sequence[index-1]],["next",sequence[index+1]]].filter(([,post])=>post).map(([rel,post])=>`<a rel="${rel}" href="${e(post.href)}">${technical?'<span class="link-title">':""}${e(post.title)}${technical?"</span>":""}</a>`).join("");
}

/** Generate data and download dependencies for the sole Svelte application. */
export async function buildContent() {
  prepareReadingReference();
  const fingerprint = readingFingerprint();
  rmSync(output,{recursive:true,force:true});
  mkdirSync(output,{recursive:true});
  imageCache.clear();
  const posts = readJournal();
  const bodies = new Map();
  for (const post of [...posts,...guidePaths().map(guideMetadata)]) {
    const path = new URL(post.href).pathname;
    const technical = !path.startsWith("/blog/");
    const sourcePath = articleSource(path);
    const file = new URL(sourcePath,root);
    const {frontmatter,content} = parseFrontmatter(input(file).toString());
    const rendered = await renderMarkdownBody(content,{image:src=>image(new URL(src,file))});
    let body = rendered.html;
    if (path==="/templates/") body += '<div class="card-grid">'+guidePaths().filter(path=>path.startsWith("/templates/") && path!=="/templates/").map(path=>{
      const guide = guideMetadata(path);
      return `<div class="sl-link-card"><span class="stack"><a href="${path}" class="title">${e(guide.title)}</a><span class="description">${e(guide.description)}</span></span></div>`;
    }).join("")+"</div>";
    // The feed uses the full authored body, without page-only metadata/navigation.
    bodies.set(path,body);
    let structured;
    const adjacent = neighbours(path,posts,technical);
    if (!technical) {
      const cover = await image(new URL(frontmatter.cover.image,file));
      body = `<figure class="sl-blog-cover"><img${attributes({...cover,loading:"eager",alt:frontmatter.cover.alt})}></figure>` +
        `<div class="sl-blog-metadata"><time datetime="${post.date}">${post.dateLabel}</time><div class="sl-blog-metadata-authors">`+
        post.authors.map(author=>`<a class="sl-blog-author" href="${e(author.url)}"><img class="sl-blog-author-picture" src="${e(author.picture)}" alt="${e(author.name)}" width="40" height="40"><div><div>${e(author.name)}</div><div class="sl-blog-author-title">${e(author.title)}</div></div></a>`).join("")+"</div></div>"+body+
        `<div class="sl-blog-post-footer"><div class="sl-blog-tags"><b>Tags:</b><ul class="sl-blog-tags-list">${post.tags.map(tag=>`<li><a href="${e(tag.href)}">${e(tag.label)}</a></li>`).join("")}</ul></div><div class="sl-blog-pagination"><div class="card-grid">${adjacent}</div></div></div>`;
      structured = {"@context":"https://schema.org","@graph":[{"@type":"BlogPosting",datePublished:post.date,headline:post.title,inLanguage:"en",isPartOf:{"@id":"https://devantler.tech/blog/#blog","@type":"Blog",name:journalConfig.title,url:"https://devantler.tech/blog/"},mainEntityOfPage:post.href,url:post.href,author:post.authors.map(author=>({"@type":"Person","@id":"https://devantler.tech/blog/authors/nikolai-emil-damm/#author",name:author.name,url:author.url})),description:post.description,image:`https://devantler.tech${cover.src}`,keywords:post.tags.map(tag=>tag.label)},{"@type":"BreadcrumbList",itemListElement:[{"@type":"ListItem",position:1,name:journalConfig.title,item:"https://devantler.tech/blog/"},{"@type":"ListItem",position:2,name:post.title}]}]};
    }
    if (body.includes('class="expressive-code"')) {
      const css = `/_astro/reading-code.${hash(rendered.css)}.css`;
      write(css,rendered.css);
      body = `<link rel="stylesheet" href="${css}">`+body;
    }
    const updated = committedContentDate(sourcePath);
    write(`${path}index.html`,`<!doctype html><html lang="en"><head>${social(post,structured)}</head><body><div class="sl-markdown-content">${body}</div>${technical?adjacent:""}<time datetime="${updated}">${dateLabel(updated)}</time></body></html>`);
  }
  const xml = `<?xml version="1.0" encoding="UTF-8"?><rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom" xmlns:content="http://purl.org/rss/1.0/modules/content/" xmlns:fh="http://purl.org/syndication/history/1.0"><channel><title>Devantler Tech | ${e(journalConfig.title)}</title><description>${e(journalDescription)}</description><link>https://devantler.tech/</link><language>en</language><fh:complete/><atom:link rel="self" href="https://devantler.tech/blog/rss.xml"/>`+
    posts.map(post=>`<item><title>${e(post.title)}</title><link>${post.href}</link><guid isPermaLink="true">${post.href}</guid><description>${e(post.description)}</description><pubDate>${new Date(post.date).toUTCString()}</pubDate><content:encoded>${e(bodies.get(new URL(post.href).pathname))}</content:encoded>${post.tags.map(tag=>`<category>${e(tag.label)}</category>`).join("")}</item>`).join("")+"</channel></rss>";
  write("blog/rss.xml",xml);
  for (const file of ["author-avatar.png","author.png","favicon.png","robots.txt","pdfs/thesis.pdf"]) write(file,input(new URL(`public/${file}`,root)));
  const portrait = await image(new URL("src/assets/profile.jpg",root));
  write("index.html",`<meta property="og:image" content="https://devantler.tech${portrait.src}">`);
  const photo = input(new URL("src/assets/profile.jpg",root));
  write("pdfs/nikolai-emil-damm-cv.pdf",await renderCvPdf(cv,`data:image/jpeg;base64,${photo.toString("base64")}`));
  assert.equal(readingFingerprint(),fingerprint,"Content source changed during native compilation");
  await captureReadingReference();
  captureJournalFeedReference();
  capturePublicDiscoveryReference();
  console.log(`Compiled ${posts.length} complete articles and ${guidePaths().length} guides for Svelte.`);
}

if (process.argv[1] === fileURLToPath(import.meta.url)) await buildContent();
