import { error } from "@sveltejs/kit";
import posts from "virtual:journal";
import archives from "virtual:journal-archives";
import { browseJournal } from "../../../../../../src/components/business/journal-pages.js";
import { journalDescription } from "../../../../../../src/components/business/journal-config.js";

/** Admit only an actual complete topic/author archive; unknown categories and slugs are 404. */
export const load = ({params}:{params:{kind:string,slug:string}}) => {
  const archive = archives.find(item => item.path === `/blog/${params.kind}/${params.slug}/`);
  if (!archive) error(404);
  return {posts:archive.posts, browse:browseJournal(posts), authors:archives.filter(item => item.kind === "authors"),
    page:1, count:1, previous:null, next:null, slug:`${params.kind}/${params.slug}`,
    heading:archive.label, title:archive.label, description:journalDescription,
    intro:archive.kind === "tags" ? `${archive.posts.length} ${archive.posts.length === 1 ? "post" : "posts"} tagged with “${archive.label}”.`
      : `${archive.posts.length} ${archive.posts.length === 1 ? "post" : "posts"} by ${archive.label}.`};
};
