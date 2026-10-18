declare module "virtual:journal" {
  const posts: import("../../src/data/journal.mjs").JournalPost[];
  export default posts;
}
declare module "virtual:journal-feed" {
  const feed: {xml:string;assets:{filename:string;source:string;digest:string}[]} | null;
  export default feed;
}
declare module "virtual:journal-archives" {
  const archives: {kind:'tags'|'authors',path:string,label:string,posts:import("../../src/data/journal.mjs").JournalPost[]}[];
  export default archives;
}
declare module "virtual:journal-reading" {
  const reading: {
    path:string; title:string; description:string; html:string; modified:string;
    headings:{id:string;title:string;level:number}[];
    adjacent:{rel:string;href:string;title:string}[];
    metadata:{name?:string;property?:string;content:string}[];
    structuredData:string[];
  }[] | null;
  export default reading;
}
