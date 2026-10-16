declare module "virtual:journal" {
  const posts: import("../../src/data/journal.mjs").JournalPost[];
  export default posts;
}
