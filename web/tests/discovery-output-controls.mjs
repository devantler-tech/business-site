import assert from "node:assert/strict";
import { artifact, assertDiscovery } from "./discovery-output.mjs";

const healthy=artifact("web/build"), baseline=artifact("dist");
assertDiscovery(healthy,baseline);
/** @param {Map<string,Buffer>} files @param {string} path @param {string|RegExp} before @param {string} after */
const replace = (files,path,before,after) => {
  const original=files.get(path); assert.ok(original);
  const changed=Buffer.from(original.toString().replace(before,after));
  assert.notDeepEqual(changed,original,"control must reach the actual artifact"); files.set(path,changed);
};
const portrait = [...healthy.keys()].find(path => /^_astro\/profile\..*\.webp$/.test(path));
assert.ok(portrait,"real sharing portrait required for the independent dependency control");
/** @type {[string,(files:Map<string,Buffer>)=>void][]} */
const controls = [
  ["missing CV",files => { files.delete("pdfs/nikolai-emil-damm-cv.pdf"); }],
  ["wrong but valid PDF",files => { files.set("pdfs/nikolai-emil-damm-cv.pdf",/** @type {Buffer} */ (healthy.get("pdfs/thesis.pdf"))); }],
  ["missing thesis beside healthy CV",files => { files.delete("pdfs/thesis.pdf"); }],
  ["missing favicon",files => { files.delete("favicon.png"); }],
  ["missing sharing dependency",files => { files.delete(/** @type {string} */ (portrait)); }],
  ["missing robots",files => { files.delete("robots.txt"); }],
  ["missing sitemap index",files => { files.delete("sitemap-index.xml"); }],
  ["missing canonical member",files => replace(files,"sitemap-0.xml","<url><loc>https://devantler.tech/about/</loc></url>","")],
  ["stale membership",files => replace(files,"sitemap-0.xml","https://devantler.tech/about/","https://devantler.tech/not-admitted/")],
  ["duplicate redirect member",files => replace(files,"sitemap-0.xml","</urlset>","<url><loc>https://devantler.tech/templates/gitops-tenant-template/</loc></url></urlset>")],
  ["missing real HTML",files => { files.delete("about/index.html"); }],
  ["incorrect Danish locale",files => replace(files,"da/about/index.html",'property="og:locale" content="da_DK"','property="og:locale" content="en_GB"')],
  ["incorrect canonical",files => replace(files,"da/about/index.html",'rel="canonical" href="https://devantler.tech/da/about/"','rel="canonical" href="https://devantler.tech/about/"')],
  ["incorrect language alternative",files => replace(files,"da/about/index.html",'hreflang="da"','hreflang="sv"')],
  ["broken social image",files => replace(files,"about/index.html",/property="og:image" content="[^"]+"/,'property="og:image" content="https://devantler.tech/missing.webp"')],
  ["duplicate social description",files => replace(files,"about/index.html","</head>",'<meta property="og:description" content="duplicate"></head>')],
  ["publisher configuration leaked",files => { files.set("CNAME",Buffer.from("devantler.tech")); }],
];
for (const [name,mutate] of controls) {
  const files=new Map(healthy);
  mutate(files);
  assert.throws(() => assertDiscovery(files,baseline),/Public discovery/,`reject ${name}`);
  console.log(`Rejected real public artifact control: ${name}`);
}
assertDiscovery(healthy,baseline);
