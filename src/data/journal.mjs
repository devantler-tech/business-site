import { readdirSync, readFileSync, lstatSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { parseFrontmatter } from "@astrojs/internal-helpers/frontmatter";
import { markdownToHtml } from "satteri";
import { slug } from "github-slugger";
import { journalConfig } from "../components/business/journal-config.js";

const defaultDirectory = new URL("../content/docs/blog/", import.meta.url);
/** @typedef {{title:string, description:string, href:string, date:string, dateLabel:string, excerpt:string, authors:(typeof journalConfig.authors.devantler)[], tags:{label:string,href:string}[], cover:string}} JournalPost */
/** @param {unknown} value @param {string} name */
const requiredText = (value, name) => {
  if (typeof value !== "string" || !value.trim()) throw new Error(`Journal ${name} must be nonempty text`);
  return value;
};

/** Build-only projection of the actual authored collection, never an independent post inventory. */
export function readJournal(directory = defaultDirectory) {
  /** @type {JournalPost[]} */
  const posts = [];
  const ids = new Set();
  /** @param {URL} folder */
  function visit(folder, prefix = "") {
    for (const entry of readdirSync(folder, { withFileTypes: true })) {
      const file = new URL(entry.name + (entry.isDirectory() ? "/" : ""), folder);
      if (entry.isSymbolicLink()) throw new Error("Journal source must not contain symlinks");
      if (entry.isDirectory()) { visit(file, `${prefix}${entry.name}/`); continue; }
      if (!/\.mdx?$/.test(entry.name)) continue;
      const id = prefix + entry.name.replace(/\.mdx?$/, "");
      if (!/^[a-z0-9-]+(?:\/[a-z0-9-]+)*$/.test(id) || /^(?:\d+|tags|authors)(?:\/|$)/.test(id)) throw new Error(`Journal unsupported route: ${id}`);
      const source = readFileSync(file, "utf8");
      let data;
      // Use Astro's actual frontmatter parser, including its YAML timestamp semantics for MD and MDX.
      try {
        const parsed = parseFrontmatter(source);
        if (!parsed.rawFrontmatter) throw new Error("frontmatter required");
        data = parsed.frontmatter;
      }
      catch (cause) { throw new Error(`Journal ${id}: malformed YAML`, { cause }); }
      if (!data || typeof data !== "object" || Array.isArray(data)) throw new Error(`Journal ${id}: metadata object required`);
      if (data.draft !== undefined && typeof data.draft !== "boolean") throw new Error(`Journal ${id}: draft must be boolean`);
      if (data.draft) continue;
      if (data.slug !== undefined) throw new Error(`Journal ${id}: custom slug needs explicit route-parity support`);
      if (data.featured !== undefined && data.featured !== false) throw new Error(`Journal ${id}: featured ordering needs explicit parity support`);
      if (ids.has(id)) throw new Error(`Journal duplicate route: ${id}`);
      ids.add(id);
      const title = requiredText(data.title, `${id} title`);
      const description = requiredText(data.description, `${id} description`);
      const excerpt = requiredText(data.excerpt, `${id} excerpt`);
      if (!(data.date instanceof Date) || !Number.isFinite(data.date.getTime())) throw new Error(`Journal ${id}: valid YAML publication date required`);
      const authors = (Array.isArray(data.authors) ? data.authors : [data.authors]).map(author => {
        if (typeof author !== "string" || !Object.hasOwn(journalConfig.authors, author)) throw new Error(`Journal ${id}: unknown author`);
        return journalConfig.authors[/** @type {keyof typeof journalConfig.authors} */ (author)];
      });
      if (!authors.length) throw new Error(`Journal ${id}: author required`);
      if (!Array.isArray(data.tags) || data.tags.some(tag => typeof tag !== "string" || !tag.trim() || !slug(tag)) || new Set(data.tags).size !== data.tags.length) throw new Error(`Journal ${id}: distinct text tags with nonempty slugs required`);
      const cover = data.cover;
      if (!cover || typeof cover.image !== "string" || !/^(?:\.\.\/){3,}assets\/editorial\/journal-[a-z0-9-]+\.webp$/.test(cover.image)) throw new Error(`Journal ${id}: local editorial cover required`);
      const coverFile = new URL(cover.image, new URL(`${id}.md`, defaultDirectory));
      if (!coverFile.href.startsWith(new URL("../assets/editorial/", import.meta.url).href)) throw new Error(`Journal ${id}: cover escapes editorial assets`);
      if (!lstatSync(coverFile).isFile() || lstatSync(coverFile).isSymbolicLink()) throw new Error(`Journal ${id}: regular cover asset required`);
      requiredText(cover.alt, `${id} cover alt`);
      posts.push({
        title, description, href: `https://devantler.tech/blog/${id}/`, date: data.date.toISOString(),
        dateLabel: new Intl.DateTimeFormat("en", { year:"numeric", month:"short", day:"numeric", timeZone:"UTC" }).format(data.date),
        excerpt: markdownToHtml(excerpt, { features: { frontmatter:false, gfm:true } }).html,
        authors, tags: data.tags.map(label => ({ label, href: `https://devantler.tech/blog/tags/${slug(label)}/` })),
        cover: fileURLToPath(coverFile).split("/").at(-1) ?? "",
      });
    }
  }
  visit(directory);
  if (!posts.length) throw new Error("Journal eligible post inventory must not be empty");
  return posts.sort((a, b) => Date.parse(b.date) - Date.parse(a.date) || a.title.localeCompare(b.title));
}

export { paginateJournal } from "../components/business/journal-pages.js";

/** Admit exactly the main archive pages required by the authored production inventory. */
export function journalPaths() {
  const count = Math.ceil(readJournal().length / journalConfig.postCount);
  return Array.from({ length:count }, (_, index) => index === 0 ? "/blog/" : `/blog/${index + 1}/`);
}
