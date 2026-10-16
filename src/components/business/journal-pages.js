import { journalConfig } from "./journal-config.js";

/** Group published topic destinations, retaining the first label and the archive's recent-post order.
 * @param {import('../../data/journal.mjs').JournalPost[]} posts
 */
export function browseJournal(posts) {
  /** @type {Map<string,{label:string,href:string,count:number}>} */
  const topics = new Map();
  for (const post of posts) for (const tag of post.tags) {
    const topic = topics.get(tag.href) ?? { ...tag, count:0 };
    topic.count++;
    topics.set(tag.href, topic);
  }
  return { recent:posts.slice(0, journalConfig.recentPostCount),
    tags:[...topics.values()].sort((a, b) => b.count - a.count || a.label.localeCompare(b.label)) };
}

/** Match the published archive boundaries without carrying build-time filesystem access into a route.
 * @template T
 * @param {T[]} posts
 * @param {string | undefined} [requested]
 */
export function paginateJournal(posts, requested) {
  const count = Math.ceil(posts.length / journalConfig.postCount);
  const page = requested === undefined ? 1 : /^(?:[2-9]|[1-9]\d+)$/.test(requested) ? Number(requested) : NaN;
  if (!Number.isSafeInteger(page) || page > count || page < 1) throw new Error("Journal page is outside the actual archive");
  /** @param {number} number */
  const href = number => number === 1 ? "/blog/" : `/blog/${number}/`;
  return { page, count, posts: posts.slice((page - 1) * journalConfig.postCount, page * journalConfig.postCount),
    previous: page > 1 ? href(page - 1) : null, next: page < count ? href(page + 1) : null };
}
