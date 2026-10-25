import assert from "node:assert/strict";
import { readFileSync, readdirSync, existsSync } from "node:fs";
import { join } from "node:path";
import { parse } from "parse5";

/** @typedef {{nodeName:string,tagName?:string,value?:string,attrs?:{name:string,value:string}[],childNodes?:MarkupNode[]}} MarkupNode */
/** @param {MarkupNode} node @returns {MarkupNode[]} */
const walk = node => [node, ...(node.childNodes ?? []).flatMap(walk)];
/** @param {MarkupNode|undefined} node @param {string} name */
const attr = (node, name) => node?.attrs?.find(item => item.name === name)?.value;
/** @param {MarkupNode} node */
const text = node => walk(node).filter(item => item.nodeName === "#text").map(item => item.value).join(" ").replace(/\s+/g, " ").trim();
/** @param {MarkupNode[]} nodes @param {string} name */
const find = (nodes, name) => nodes.find(node => attr(node, name) !== undefined);
/** @param {string} path */
const load = path => { assert.ok(existsSync(path), "required bilingual page is missing"); return walk(parse(readFileSync(path, "utf8"))); };
/** @param {MarkupNode} node */
const links = node => walk(node).filter(item => item.tagName === "a").map(item => [text(item), attr(item,"href")]);
/** @param {string} dir @returns {string[]} */
const files = dir => readdirSync(dir, {withFileTypes:true}).flatMap(entry => entry.isDirectory() ? files(join(dir,entry.name)) : [join(dir,entry.name)]);

/** Validate inline text exactly as the rendered paragraph joins its text nodes.
 * @param {string} candidate @param {string} locale
 */
export function assertMonthlyVATSpacing(candidate, locale) {
  const nodes = load(join(candidate, locale === "da" ? "da/index.html" : "index.html"));
  const prices = nodes.filter(node => node.tagName === "p" && attr(node,"class")?.split(/\s+/).includes("hosting-price"));
  assert.equal(prices.length, 3, "all monthly prices remain visible");
  for (const price of prices) {
    const inlineText = walk(price).filter(node => node.nodeName === "#text").map(node => node.value).join("");
    assert.match(inlineText, locale === "da" ? /\/ måned\s+inkl[.] moms/ : /\/ month\s+incl[.] VAT/,
      "monthly period and VAT label must remain separate words");
  }
}

/** Compare real generated artifacts; never import the copy builders under test.
 * @param {string} candidate @param {string} reference
 * @param {{identity:boolean,scope:boolean,template:boolean,address:string}} options
 */
export function assertCompanyOutput(candidate, reference, {identity, scope, template, address}) {
  for (const locale of ["en", "da"]) assertMonthlyVATSpacing(candidate, locale);
  const expectedAddress = template ? ["__BUSINESS_ADDRESS_LINE_1__", "__BUSINESS_ADDRESS_LINE_2__"] : address.split("\n");
  const companyPages = new Set([join(candidate,"company/index.html"),join(candidate,"da/company/index.html")]);
  for (const [prefix,locale] of [["","en"],["da/","da"]]) {
    for (const route of ["", "about/", "projects/"]) {
      const path = `${prefix}${route}index.html`;
      const actual = load(join(candidate,path)), original = load(join(reference,path));
      const footer = find(actual,"data-business-footer"), originalFooter = find(original,"data-business-footer");
      assert.ok(footer && originalFooter, "real shared business footer is required");
      assert.equal(text(footer),text(originalFooter), "complete localized company/footer copy matches Astro");
      assert.deepEqual(links(footer),links(originalFooter), "footer destinations and labels match Astro");
      if (identity) {
        assert.ok(text(footer).includes(locale === 'da' ? 'Enkeltmandsvirksomhed' : 'Sole proprietorship'), 'current ordinary registration is independently required');
        assert.doesNotMatch(text(footer), /PMV|personally owned small business|personligt ejet mindre virksomhed/i, 'current footer cannot retain PMV wording');
      }
      assert.equal(walk(footer).some(node => attr(node,"href") === `/${prefix}company/`),identity);
      assert.equal(actual.some(node => attr(node,"href") === "mailto:ned@devantler.tech"),identity);
      assert.equal(!!find(actual,"data-customer-scope"),scope);
      for (const language of ["en","da","x-default"]) assert.ok(actual.some(node => attr(node,"hreflang") === language && attr(node,"href") === `https://devantler.tech/${language === "da" ? "da/" : ""}${route}`), "canonical language alternative is local and complete");
      assert.ok(actual.some(node => attr(node,"rel") === "canonical" && attr(node,"href") === `https://devantler.tech/${prefix}${route}`));
      assert.ok(actual.some(node => node.tagName === "html" && attr(node,"lang") === locale));
      if (route === "") {
        const contact = actual.find(node => attr(node,"id") === "contact"), oldContact = original.find(node => attr(node,"id") === "contact");
        assert.ok(contact && oldContact);
        assert.equal(text(contact),text(oldContact),"actual contact invitation/alternative/maintenance copy matches Astro");
        assert.deepEqual(links(contact),links(oldContact),"actual inquiry and LinkedIn alternative match Astro");
        /** @param {MarkupNode[]} nodes */
        const offers = nodes => nodes.filter(node => attr(node,"data-offer") !== undefined).map(node => [attr(node,"data-offer"),attr(node,"data-setup"),attr(node,"data-monthly"),text(node)]);
        assert.deepEqual(offers(actual),offers(original), "complete offers, VAT markers and amounts match the actual Astro artifact");
        assert.deepEqual(offers(actual).map(item => item.slice(0,3)),[["website","3750","125"],["app","10000","375"],["service","6250","250"]]);
      }
    }
    const path = join(candidate,`${prefix}company/index.html`);
    if (!identity) { assert.equal(existsSync(path),false,"disabled company route must be absent, not merely empty"); continue; }
    const actual = load(path), original = load(join(reference,`${prefix}company/index.html`));
    const disclosure = find(actual,"data-company-disclosure"), oldDisclosure = find(original,"data-company-disclosure");
    assert.ok(disclosure && oldDisclosure,"complete company disclosure is required");
    assert.ok(text(disclosure) === text(oldDisclosure),"all registered facts and escaped address match Astro");
    const registration = locale === 'da' ? 'Enkeltmandsvirksomhed' : 'Sole proprietorship';
    for (const fact of ["Devantler Tech","46830385",registration,"Nikolai Emil Damm","ned@devantler.tech"]) assert.ok(text(disclosure).includes(fact),"registered company fact is missing");
    {
      const document = actual.find(node => node.tagName === 'html');
      assert.ok(document, 'current disclosure has a document element');
      assert.doesNotMatch(text(document), /PMV|personally owned small business|personligt ejet mindre virksomhed/i, 'current disclosure and serialized facts cannot retain PMV wording');
    }
    const addressNode = find(actual,"data-company-address");
    assert.ok(addressNode && attr(addressNode,"data-pagefind-ignore") !== undefined,"required address stays out of search");
    assert.ok(JSON.stringify((addressNode.childNodes ?? []).filter(node => node.tagName).map(node => text(node))) === JSON.stringify(expectedAddress),"both address lines render as escaped text");
    assert.ok(walk(addressNode).every(node => !node.tagName || ["address","span"].includes(node.tagName)),"address cannot inject markup");
    assert.ok(actual.some(node => attr(node,"rel") === "canonical" && attr(node,"href") === `https://devantler.tech/${prefix}company/`));
    for (const language of ["en","da","x-default"]) assert.ok(actual.some(node => attr(node,"hreflang") === language && attr(node,"href") === `https://devantler.tech/${language === "da" ? "da/" : ""}company/`));
  }
  // Supporting frames participate too, not only the six top-level pages.
  for (const route of ["blog/index.html", "blog/welcome/index.html", "templates/index.html"]) {
    const nodes = load(join(candidate,route)), footer = find(nodes,"data-business-footer");
    assert.ok(footer,"supporting page retains the business footer");
    assert.equal(links(footer).some(([,href]) => href === "/company/"),identity);
    if (identity) {
      assert.ok(text(footer).includes('Sole proprietorship'), 'supporting footer independently identifies the ordinary business');
      assert.doesNotMatch(text(footer), /\bPMV\b/i);
    }
  }
  for (const file of files(candidate)) {
    const bytes = readFileSync(file);
    for (const needle of ["company-address-fixture-187", "1234 Exampletown"]) {
      assert.ok(!bytes.includes(Buffer.from(needle)) || (identity && !template && companyPages.has(file)), "address leaked outside the two disclosure HTML pages");
    }
    assert.ok(!bytes.includes(Buffer.from("__BUSINESS_ADDRESS_LINE_")) || (identity && template && companyPages.has(file)),"template address leaked into unrelated output");
    if (file.endsWith(".html") && !companyPages.has(file)) assert.ok(!find(walk(parse(bytes.toString())),"data-company-address"),"address marker leaked into unrelated route");
  }
}
