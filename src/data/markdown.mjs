import assert from "node:assert/strict";
import { markdownToHast } from "satteri";
import GithubSlugger from "github-slugger";
import { ExpressiveCode, loadShikiTheme } from "expressive-code";
import { toHtml } from "hast-util-to-html";

const code = new ExpressiveCode({themes:await Promise.all([loadShikiTheme("github-dark"),loadShikiTheme("github-light")]), themeCssSelector:theme => `[data-theme="${theme.type}"]`});
const elements = new Set("a p h1 h2 h3 h4 h5 h6 em strong del s b i u code pre blockquote ul ol li table thead tbody tfoot tr th td caption img br hr div span section aside details summary sup sub kbd samp mark abbr time figure figcaption".split(" "));
/** @param {any} node @returns {string} */
const text = node => node.type === "text" ? node.value : (node.children ?? []).map(text).join("");
/** @param {unknown} value */
export const escapeHtml = value => String(value).replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll('"', "&quot;");

/** Compile authored Markdown, never execute MDX imports, expressions or components.
 * Images are resolved by the build owner; rendering does not fetch remote resources.
 * @param {string} source @param {{image?:(src:string)=>Promise<Record<string,unknown>>}} options
 */
export async function renderMarkdownBody(source, {image} = {}) {
  const tree = markdownToHast(source, {features:{gfm:true,frontmatter:false,rawHtml:true,smartPunctuation:true}});
  const slugger = new GithubSlugger();
  const styles = new Set();
  /** @param {any} parent */
  async function visit(parent) {
    for (const [index,node] of (parent.children ?? []).entries()) {
      if (node.type === "text") assert.ok(!/^(?:import|export)\s/m.test(node.value), "Executable MDX is unsupported");
      if (node.type === "raw") throw new Error("Unsupported raw Markdown content");
      if (node.type !== "element") continue;
      assert.ok(elements.has(node.tagName), "Unsupported executable Markdown element");
      for (const [name,value] of Object.entries(node.properties ?? {})) {
        assert.ok(!/^on/i.test(name), "Unsupported executable Markdown attribute");
        if (["href","src"].includes(name)) assert.ok(["https:","http:","mailto:","tel:"].includes(new URL(String(value),"https://devantler.tech/").protocol), "Unsupported executable Markdown URL");
      }
      if (/^h[1-6]$/.test(node.tagName)) node.properties.id = slugger.slug(text(node));
      if (node.tagName === "pre" && node.children?.[0]?.tagName === "code") {
        const block = node.children[0];
        const language = block.data?.lang ?? block.properties?.className?.find((/** @type {string} */ name) => name.startsWith("language-"))?.slice(9) ?? "text";
        const value = text(block).replace(/\n$/, "");
        if (language === "mermaid") {
          parent.children[index] = {type:"element",tagName:"pre",properties:{className:["mermaid"]},children:[{type:"text",value}]};
        } else {
          const result = await code.render({code:value,language,meta:block.data?.meta ?? ""});
          for (const style of result.styles) styles.add(style);
          parent.children[index] = result.renderedGroupAst;
        }
        continue;
      }
      if (node.tagName === "img" && image && !/^https:\/\//.test(node.properties.src)) Object.assign(node.properties, await image(node.properties.src));
      // GitHub-style alerts keep their native prose and visible label.
      if (node.tagName === "blockquote") {
        const first = node.children?.find((/** @type {any} */ child) => child.tagName === "p");
        const start = first?.children?.[0];
        const alert = start?.type === "text" && start.value.match(/^\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\]\s*/);
        if (alert) {
          start.value = start.value.slice(alert[0].length);
          node.tagName = "aside";
          const kinds = /** @type {Record<string,string>} */ ({NOTE:"note",TIP:"tip",IMPORTANT:"caution",WARNING:"caution",CAUTION:"danger"});
          const kind = kinds[alert[1]];
          node.properties = {className:["starlight-aside", `starlight-aside--${kind}`]};
          node.children.unshift({type:"element",tagName:"p",properties:{className:["starlight-aside__title"]},children:[{type:"text",value:kind[0].toUpperCase()+kind.slice(1)}]});
        }
      }
      await visit(node);
      if (/^h[2-6]$/.test(node.tagName)) {
        const label = `Section titled “${text(node)}”`;
        const icon = {type:"element",tagName:"svg",properties:{width:16,height:16,viewBox:"0 0 24 24",fill:"none",stroke:"currentColor",strokeWidth:2},children:[{type:"element",tagName:"path",properties:{d:"M10 13a5 5 0 0 0 7 0l3-3a5 5 0 0 0-7-7l-2 2M14 11a5 5 0 0 0-7 0l-3 3a5 5 0 0 0 7 7l2-2"},children:[]}]};
        parent.children[index] = {type:"element",tagName:"div",properties:{className:["sl-heading-wrapper",`level-${node.tagName}`]},children:[node,{type:"element",tagName:"a",properties:{className:["sl-anchor-link"],href:`#${node.properties.id}`},children:[{type:"element",tagName:"span",properties:{ariaHidden:"true",className:["sl-anchor-icon"]},children:[icon]},{type:"element",tagName:"span",properties:{className:["sr-only"],dataPagefindIgnore:""},children:[{type:"text",value:label}]}]}]};
      }
    }
  }
  await visit(tree);
  return {html:toHtml(tree),css:[await code.getBaseStyles(),await code.getThemeStyles(),...styles].join("\n")};
}
