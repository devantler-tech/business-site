declare module "virtual:journal" {
  const posts: import("../../src/data/journal.mjs").JournalPost[];
  export default posts;
}
declare module "virtual:journal-reading" {
  const reading: {
    path:string; title:string; description:string; html:string; modified:string;
    headings:{id:string;title:string;level:number}[];
    adjacent:{rel:string;href:string;title:string}[];
    metadata:{name?:string;property?:string;content:string}[];
    structuredData:string[];
  } | null;
  export default reading;
}
