import { error } from "@sveltejs/kit";
import { FEATURE_SVELTE_FRONTEND } from "$app/env/private";
import posts from "virtual:journal";
import archives from "virtual:journal-archives";
import { paginateJournal, browseJournal } from "../../../../../src/components/business/journal-pages.js";
import { journalConfig, journalIntro, journalDescription } from "../../../../../src/components/business/journal-config.js";

export const load = ({ params }: { params:{ page?:string } }) => {
  if (!FEATURE_SVELTE_FRONTEND) error(404);
  let archive;
  try { archive = paginateJournal(posts, params.page); }
  catch { error(404); }
  return { ...archive, browse:browseJournal(posts), authors:archives.filter(item => item.kind === "authors"),
    slug:"", heading:"Journal", title:journalConfig.title, intro:journalIntro, description:journalDescription };
};
