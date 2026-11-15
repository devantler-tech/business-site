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
/** The portal's privacy section is released separately from the rest of the notice. */
const portalPrivacy = process.env.FEATURE_PORTAL_PRIVACY === "true";
/** The published phone number, or "" until the business adds one to company.json. */
const companyPhone = JSON.parse(readFileSync(new URL("../../src/data/company.json", import.meta.url), "utf8")).phone?.trim() ?? "";

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

/** Validate real generated artifacts against literal public facts and private-input boundaries.
 * @param {string} candidate
 * @param {{identity:boolean,scope:boolean,template:boolean,address:string}} options
 */
export function assertCompanyOutput(candidate, {identity, scope, template, address}) {
  for (const locale of ["en", "da"]) assertMonthlyVATSpacing(candidate, locale);
  const expectedAddress = template ? ["__BUSINESS_ADDRESS_LINE_1__", "__BUSINESS_ADDRESS_LINE_2__"] : address.split("\n");
  const companyPages = new Set([join(candidate,"company/index.html"),join(candidate,"da/company/index.html")]);
  for (const [prefix,locale] of [["","en"],["da/","da"]]) {
    for (const route of ["", "about/", "projects/"]) {
      const path = `${prefix}${route}index.html`;
      const actual = load(join(candidate,path));
      const footer = find(actual,"data-business-footer");
      assert.ok(footer, "real shared business footer is required");
      for (const suffix of ["about/","projects/","#contact"]) assert.ok(links(footer).some(([,href])=>href===`/${prefix}${suffix}`), "localized footer journey is missing");


      if (identity) {
        assert.ok(text(footer).includes(locale === 'da' ? 'Enkeltmandsvirksomhed' : 'Sole proprietorship'), 'current ordinary registration is independently required');
        assert.doesNotMatch(text(footer), /PMV|personally owned small business|personligt ejet mindre virksomhed/i, 'current footer cannot retain PMV wording');
      }
      assert.equal(walk(footer).some(node => attr(node,"href") === `/${prefix}company/`),identity);
      for (const kind of ["privacy/","terms/"]) assert.equal(links(footer).some(([,href]) => href === `/${prefix}${kind}`),identity,"privacy and terms footer links follow company identity");
      assert.equal(actual.some(node => attr(node,"href") === "mailto:ned@devantler.tech"),identity);
      assert.equal(!!find(actual,"data-customer-scope"),scope);
      for (const language of ["en","da","x-default"]) assert.ok(actual.some(node => attr(node,"hreflang") === language && attr(node,"href") === `https://devantler.tech/${language === "da" ? "da/" : ""}${route}`), "canonical language alternative is local and complete");
      assert.ok(actual.some(node => attr(node,"rel") === "canonical" && attr(node,"href") === `https://devantler.tech/${prefix}${route}`));
      assert.ok(actual.some(node => node.tagName === "html" && attr(node,"lang") === locale));
      if (route === "") {
        const contact = actual.find(node => attr(node,"id") === "contact");
        assert.ok(contact);
        assert.ok(links(contact).some(([,href])=>href===(identity?"mailto:ned@devantler.tech":"https://www.linkedin.com/in/nikolai-emil-damm-14a786150/")), "contact journey retains its approved destination");


        /** @param {MarkupNode[]} nodes */
        const offers = nodes => nodes.filter(node => attr(node,"data-offer") !== undefined).map(node => [attr(node,"data-offer"),attr(node,"data-setup"),attr(node,"data-monthly"),text(node)]);

        assert.deepEqual(offers(actual).map(item => item.slice(0,3)),[["website","3750","125"],["app","10000","375"],["service","6250","250"],["other",undefined,undefined]]);
      }
    }
    for (const kind of ["privacy","terms"]) {
      const legalPath = join(candidate,`${prefix}${kind}/index.html`);
      if (!identity) { assert.equal(existsSync(legalPath),false,`disabled ${kind} route must be absent, not merely empty`); continue; }
      const page = load(legalPath), main = find(page,"data-legal-page");
      assert.ok(main && attr(main,"data-legal-page") === kind,`${kind} page renders its legal content`);
      assert.ok(page.some(node => node.tagName === "html" && attr(node,"lang") === locale));
      assert.ok(page.some(node => attr(node,"rel") === "canonical" && attr(node,"href") === `https://devantler.tech/${prefix}${kind}/`));
      for (const language of ["en","da","x-default"]) assert.ok(page.some(node => attr(node,"hreflang") === language && attr(node,"href") === `https://devantler.tech/${language === "da" ? "da/" : ""}${kind}/`));
      for (const fact of ["46830385","ned@devantler.tech"]) assert.ok(text(main).includes(fact),`${kind} page names the registered business`);
      if (kind === "terms") assert.ok(companyPhone ? text(main).includes(companyPhone) : !/Phone:|Telefon:/.test(text(main)), "terms name the phone number exactly when one is set");
      assert.ok(links(main).some(([,href]) => href === `/${prefix}company/`),`${kind} page links to the postal address instead of copying it`);
      const required = kind === "privacy"
        ? (locale === "da" ? ["Datatilsynet","seks måneder","fem år","cookies"] : ["Datatilsynet","six months","five years","cookies"])
        : (locale === "da" ? ["14 dage","Standardfortrydelsesformular","sætte kryds","helt udført","en måneds varsel","Nævnenes Hus","købeloven"] : ["14 days","Withdrawal form","ticking the box","completely finished","one month’s notice","Nævnenes Hus","Sale of Goods Act"]);
      for (const phrase of required) assert.ok(text(main).includes(phrase),`${kind} page is missing: ${phrase}`);
      // List items carry the withdrawal form and the recipients; headings alone must not satisfy them.
      const items = walk(main).filter(node => node.tagName === "li").map(text);
      const listed = kind === "privacy"
        ? ["Numina","Stripe","Cloudflare"]
        : (locale === "da" ? ["Forbrugerens underskrift","Bestilt den (*)/modtaget den (*)"] : ["Signature of consumer(s)","Ordered on (*)/received on (*)"]);
      for (const phrase of listed) assert.ok(items.some(item => item.includes(phrase)),`${kind} list is missing: ${phrase}`);
      // The portal's privacy text is unreleased content behind its own default-off flag.
      if (kind === "privacy") for (const phrase of ["accounts.devantler.tech", locale === "da" ? "30 dage" : "30 days", "GitHub"]) {
        assert.equal(text(main).includes(phrase), portalPrivacy, `portal privacy text follows FEATURE_PORTAL_PRIVACY: ${phrase}`);
      }
    }
    const path = join(candidate,`${prefix}company/index.html`);
    if (!identity) { assert.equal(existsSync(path),false,"disabled company route must be absent, not merely empty"); continue; }
    const actual = load(path);
    const disclosure = find(actual,"data-company-disclosure");
    assert.ok(disclosure,"complete registered facts and escaped address disclosure is required");

    const registration = locale === 'da' ? 'Enkeltmandsvirksomhed' : 'Sole proprietorship';
    for (const fact of ["Devantler Tech","46830385",registration,"Nikolai Emil Damm","ned@devantler.tech"]) assert.ok(text(disclosure).includes(fact),"registered company fact is missing");
    assert.equal(!!find(actual,"data-company-phone"), companyPhone !== "", "company page shows the phone number exactly when one is set");
    if (companyPhone) assert.ok(text(disclosure).includes(companyPhone), "company page shows the published phone number");
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
    for (const kind of ["/privacy/","/terms/"]) assert.equal(links(footer).some(([,href]) => href === kind),identity,"supporting footer links the privacy notice and terms");
    if (identity) {
      assert.ok(text(footer).includes('Sole proprietorship'), 'supporting footer independently identifies the ordinary business');
      assert.doesNotMatch(text(footer), /\bPMV\b/i);
    }
  }
  for (const file of files(candidate)) {
    const bytes = readFileSync(file);
    // Consumer law needs the phone only before an agreement: on the company page and the terms.
    if (companyPhone) assert.ok(!bytes.includes(Buffer.from(companyPhone)) || /^(?:da\/)?(?:company|terms)\/(?:index\.html|__data\.json)$/.test(file.slice(candidate.length + 1)), `phone number leaked into ${file.slice(candidate.length + 1)}`);
    for (const needle of ["company-address-fixture-187", "1234 Exampletown"]) {
      assert.ok(!bytes.includes(Buffer.from(needle)) || (identity && !template && companyPages.has(file)), "address leaked outside the two disclosure HTML pages");
    }
    assert.ok(!bytes.includes(Buffer.from("__BUSINESS_ADDRESS_LINE_")) || (identity && template && companyPages.has(file)),"template address leaked into unrelated output");
    if (file.endsWith(".html") && !companyPages.has(file)) assert.ok(!find(walk(parse(bytes.toString())),"data-company-address"),"address marker leaked into unrelated route");
  }
}
