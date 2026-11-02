import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { parse } from "parse5";
import { readingPaths } from "../src/data/journal-reading.mjs";
import { journalPaths } from "../src/data/journal.mjs";

const root=process.argv[2]??"dist";
const walk=node=>[node,...(node.childNodes??[]).flatMap(walk)];
const attr=(node,name)=>node?.attrs?.find(item=>item.name===name)?.value;
const hasClass=(node,name)=>(attr(node,"class")??"").split(/\s+/).includes(name);
for(const path of [...journalPaths(),...readingPaths()]) {
  const html=readFileSync(join(root,path,"index.html"),"utf8");
  const nodes=walk(parse(html));
  assert.equal(nodes.filter(node=>node.tagName==="h1").length,1, path+": one page heading");
  assert.ok(nodes.some(node=>node.tagName==="a"&&attr(node,"href")==="/blog/rss.xml"), path+": visible RSS journey");
  assert.ok(nodes.some(node=>attr(node,"data-business-footer")!==undefined), path+": shared business frame");
  const cards=nodes.filter(node=>hasClass(node,"sl-blog-preview"));
  if(readingPaths().includes(path)) {
    assert.equal(cards.length,0,path+": reading prose is not an archive card");
    assert.ok(nodes.some(node=>hasClass(node,"sl-markdown-content")),path+": complete article body");
    assert.ok(nodes.some(node=>node.tagName==="article"),path+": native reading article");
  } else {
    assert.ok(cards.length>0,path+": complete story archive");
    assert.ok(nodes.some(node=>node.tagName==="details"&&hasClass(node,"browse")),path+": native browse disclosure");
    assert.equal(nodes.filter(node=>hasClass(node,"browse-group")).length,2,path+": recent posts and topic choices");
    if(path==="/blog/") assert.equal(cards.length,5,"first archive page retains five stories");
  }
  assert.ok(!html.includes("/da/blog/")&&!html.includes("sveltekit.start"),path+": English reading without application hydration");
}
console.log("Journal: complete Svelte archives, reading surfaces, native browsing and shared business frame verified.");
