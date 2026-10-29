import { error } from "@sveltejs/kit";
import posts from "virtual:journal";
import archives from "virtual:journal-archives";
import readings from "virtual:journal-reading";
import { paginateJournal, browseJournal } from "../../../../../src/components/business/journal-pages.js";
import { journalConfig, journalIntro, journalDescription } from "../../../../../src/components/business/journal-config.js";

export const load = ({ params }: { params:{ page?:string } }) => {
  if (params.page && !/^\d+$/.test(params.page)) {
    const reading = readings?.find(item => item.path === `/blog/${params.page}/`);
    if (!reading) error(404);
    return {kind:"reading" as const,reading};
  }
  let archive;
  try { archive = paginateJournal(posts, params.page); }
  catch { error(404); }
  return {kind:"archive" as const, ...archive, browse:browseJournal(posts), authors:archives.filter(item => item.kind === "authors"),
    slug:"", heading:"Journal", title:journalConfig.title, intro:journalIntro, description:journalDescription };
};
