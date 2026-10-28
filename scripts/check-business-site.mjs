import assert from 'node:assert/strict';
import { readFileSync, readdirSync, existsSync, statSync } from 'node:fs';
import { basename, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { runInNewContext } from 'node:vm';
import sharp from 'sharp';
import { parse } from 'parse5';
import { imageDigest, readJournalCovers } from './check-editorial-assets.mjs';
import { assertNoRetiredRepositoryLinks } from './check-retired-links.mjs';
import { assertVisitorPrivacy } from './visitor-privacy.mjs';
import { readerText } from './reader-text.mjs';
import { publicEntries } from '../web/public-selection.ts';
import { journalPaths } from '../src/data/journal.mjs';
import { readingPaths, guidePaths } from '../src/data/journal-reading.mjs';

const [directory, ...extraArguments] = process.argv.slice(2);
assert.ok(directory && extraArguments.length === 0, 'Usage: check-business-site.mjs <build-directory>');
const root = resolve(directory);
const company = JSON.parse(readFileSync(new URL('../src/data/company.json', import.meta.url), 'utf8'));
const companyContact = `mailto:${company.email}`;
const companyEnabled = publicEntries().includes('/company/');
const all = node => [node, ...(node.childNodes ?? []).flatMap(all)];
const attr = (node, name) => node?.attrs?.find(item => item.name === name)?.value;
const text = node => node.nodeName === '#text' ? node.value : (node.childNodes ?? []).map(text).join('');
const hasClass = (node, name) => (attr(node, 'class') ?? '').split(/\s+/).includes(name);
const pageAt = path => readFileSync(resolve(root, path.replace(/^\//, ''), 'index.html'), 'utf8');
const localAsset = href => {
  const url = new URL(href, 'https://devantler.tech');
  assert.equal(url.origin, 'https://devantler.tech', 'Visitor dependencies stay local');
  const file = resolve(root, `.${url.pathname}`);
  assert.ok(file.startsWith(root + '/') && existsSync(file), `Missing local dependency: ${url.pathname}`);
  return file;
};
const metadata = node => [
  ...(node.tagName === 'meta' ? node.attrs.filter(item => item.name === 'content').map(item => item.value) : []),
  ...(node.attrs ?? []).filter(item => ['alt', 'aria-label'].includes(item.name)).map(item => item.value),
  ...(node.childNodes ?? []).flatMap(metadata),
];

for (const path of readdirSync(root, {recursive:true})) if (path.endsWith('.html') && statSync(resolve(root, path)).isFile()) {
  const html = readFileSync(resolve(root, path), 'utf8');
  assertNoRetiredRepositoryLinks(html);
  assertVisitorPrivacy(html, path);
  const nodes = all(parse(html));
  const footer = nodes.find(node => node.tagName === 'footer' && attr(node, 'data-business-footer') !== undefined);
  if (!footer) continue; // Static compatibility redirects and the error document have no business frame.
  if (companyEnabled) {
    const locale = attr(nodes.find(node => node.tagName === 'html'), 'lang');
    for (const fact of [company.name, company.cvr, company.owner, company.email, locale === 'da' ? 'Enkeltmandsvirksomhed' : 'Sole proprietorship']) {
      assert.ok(text(footer).includes(fact), `${path}: shared footer identifies the registered business`);
    }
    assert.doesNotMatch(text(footer), /\bPMV\b/i);
    assert.ok(all(footer).some(node => node.tagName === 'a' && attr(node, 'href') === companyContact));
  } else assert.ok(!text(footer).includes(company.cvr) && !text(footer).includes(company.email), `${path}: unreleased registration remains absent`);
}

function navigation(nodes, locale) {
  const nav = nodes.find(node => node.tagName === 'nav' && attr(node, 'aria-label') === (locale === 'da' ? 'Hovednavigation' : 'Main navigation'));
  assert.ok(nav, 'Every business and supporting page must expose the shared business navigation');
  const prefix = locale === 'da' ? '/da' : '';
  for (const href of [`${prefix}/about/`, `${prefix}/projects/`, '/blog/']) assert.ok(all(nav).some(node => node.tagName === 'a' && attr(node, 'href') === href), `Visitors can reach ${href}`);
  assert.equal(nodes.filter(node => node.tagName === 'select').length, 1, 'One shared appearance picker');
  assert.ok(nodes.some(node => node.tagName === 'select' && attr(node, 'id') === 'theme-select'));
  assert.ok(nodes.some(node => attr(node, 'data-business-footer') !== undefined));
}

async function workExample(nodes, locale) {
  const card = nodes.find(node => node.tagName === 'article' && hasClass(node, 'work-example') && all(node).some(child => child.tagName === 'h3' && text(child) === 'AS Coaching og Vaner'));
  assert.ok(card, 'Visitors can identify the public AS Coaching example');
  const preview = all(card).find(node => node.tagName === 'a' && all(node).some(child => child.tagName === 'img'));
  assert.ok(preview, 'The thumbnail opens the full local capture');
  const destination = attr(preview, 'href');
  assert.match(basename(destination), /^ascoaching-homepage\.[^.]+\.jpe?g$/);
  assert.equal(attr(preview, 'target'), '_blank');
  assert.ok((attr(preview, 'rel') ?? '').split(/\s+/).includes('noopener'));
  const image = all(preview).find(node => node.tagName === 'img');
  assert.ok(image);
  assert.equal(attr(image, 'alt'), locale === 'da' ? 'Skærmbillede af AS Coaching og Vaners forside.' : 'Screenshot of AS Coaching og Vaner’s homepage.');
  assert.notEqual(attr(image, 'src'), destination, 'The compact thumbnail remains separate from the full capture');
  assert.ok(Number(attr(image, 'width')) <= 640 && Number(attr(image, 'height')) > 0);
  localAsset(attr(image, 'src'));
  const full = await sharp(localAsset(destination)).metadata();
  assert.ok(full.width >= 1200 && full.height >= 600, 'The full desktop capture remains readable');
  assert.ok(text(preview).includes(locale === 'da' ? 'Se større billede (åbner i en ny fane)' : 'View larger (opens in a new tab)'));
  assert.ok(all(card).some(node => node.tagName === 'a' && attr(node, 'href') === 'https://ascoachingogvaner.dk/'));
}

for (const locale of ['en', 'da']) {
  const prefix = locale === 'da' ? 'da/' : '';
  for (const section of ['', 'about', 'projects']) {
    const path = `/${prefix}${section}${section ? '/' : ''}`;
    const html = pageAt(path), nodes = all(parse(html));
    // Keep the original visitor/accessibility policy, independently of framework markup.
    const marketing = [readerText(html), ...metadata(parse(html))].join(' ').replaceAll('Personally owned small business (PMV)', 'PMV');
    assert.doesNotMatch(marketing, /(?<!\p{L})(?:small|små|lille)(?!\p{L})/iu, `${locale} ${section || 'Home'}: offering copy must not undersell project size`);
    navigation(nodes, locale);
    assert.equal(attr(nodes.find(node => node.tagName === 'html'), 'lang'), locale);
    assert.equal(nodes.filter(node => node.tagName === 'h1').length, 1);
    assert.ok(nodes.some(node => node.tagName === 'link' && attr(node, 'rel') === 'canonical' && attr(node, 'href') === `https://devantler.tech${path}`));
    const alternate = `${locale === 'da' ? '/' : '/da/'}${section}${section ? '/' : ''}`;
    assert.ok(nodes.some(node => node.tagName === 'a' && attr(node, 'href') === alternate), 'Language switching retains the corresponding page');
    if (section !== 'projects') {
      const portrait = nodes.find(node => node.tagName === 'img' && attr(node, 'alt') === 'Nikolai Emil Damm');
      assert.ok(portrait, 'The biography identifies its real founder');
      assert.match(basename(attr(portrait, 'src')), /^profile\.[^.]+\.(?:webp|avif|jpe?g)$/);
      localAsset(attr(portrait, 'src'));
    }
    if (section === 'about') assert.ok(nodes.some(node => node.tagName === 'a' && attr(node, 'href') === '/pdfs/nikolai-emil-damm-cv.pdf' && attr(node, 'download') !== undefined));
    if (section !== 'about') await workExample(nodes, locale);
    if (section) continue;
    for (const id of ['services','work','products','process','contact']) assert.ok(nodes.some(node => attr(node, 'id') === id), `Missing visitor section: ${id}`);
    for (const [service, setup, monthly] of [['website','3750','125'],['app','10000','375'],['service','6250','250']]) {
      const offer = nodes.find(node => attr(node, 'data-offer') === service);
      assert.ok(offer && attr(offer, 'data-setup') === setup && attr(offer, 'data-monthly') === monthly);
    }
    const markers = nodes.filter(node => hasClass(node, 'vat-marker'));
    assert.equal(markers.length, 6);
    assert.ok(markers.every(node => text(node).includes(locale === 'da' ? 'inkl. moms' : 'incl. VAT')));
    assert.ok(readerText(html).includes(locale === 'da' ? 'Priserne er inkl. 25 % moms.' : 'Prices include 25% Danish VAT.'));
    const contact = nodes.find(node => attr(node, 'id') === 'contact');
    assert.ok(contact);
    assert.equal(all(contact).some(node => node.tagName === 'a' && attr(node, 'href') === companyContact), companyEnabled);
    assert.ok(all(contact).some(node => node.tagName === 'a' && attr(node, 'href') === 'https://www.linkedin.com/in/nikolai-emil-damm-14a786150/'));
    assert.ok(!nodes.some(node => node.tagName === 'form'), 'No undelivered contact form');
    const theme = nodes.find(node => node.tagName === 'script' && /\/theme\.[^/]+\.js$/.test(attr(node, 'src') ?? ''));
    assert.ok(theme, 'The existing local appearance controller is emitted');
    const element = {dataset:{},style:{}};
    runInNewContext(readFileSync(localAsset(attr(theme, 'src')), 'utf8'), {document:{documentElement:element,readyState:'loading',addEventListener() {}},window:{matchMedia:()=>({matches:false,addEventListener() {}}),localStorage:{getItem:()=> 'light'},addEventListener() {}}});
    assert.equal(element.dataset.theme, 'light', 'The actual delivered controller restores the saved preference');
    for (const node of nodes.filter(node => node.tagName === 'a' && (attr(node, 'href') ?? '').startsWith('/'))) {
      const url = new URL(attr(node, 'href'), 'https://devantler.tech');
      const file = /\.[a-z0-9]+$/i.test(url.pathname) ? url.pathname : `${url.pathname}index.html`;
      localAsset(file);
    }
  }
}

for (const path of [...journalPaths(), ...readingPaths(), ...guidePaths()]) navigation(all(parse(pageAt(path))), 'en');
const covers = await readJournalCovers(resolve(fileURLToPath(new URL('..', import.meta.url))));
const paths = new Set(), pixels = new Set();
for (const {slug,path:sourcePath} of covers) {
  const nodes = all(parse(pageAt(`/blog/${slug}/`)));
  const figure = nodes.find(node => node.tagName === 'figure' && hasClass(node, 'sl-blog-cover'));
  const image = figure && all(figure).find(node => node.tagName === 'img');
  assert.ok(image, `Journal post ${slug} retains its complete cover`);
  assert.match(attr(image, 'alt'), /^Illustration of /);
  assert.equal(attr(image, 'width'), '1440'); assert.equal(attr(image, 'height'), '810');
  const asset = attr(image, 'src');
  assert.ok(asset.includes(`-${basename(sourcePath,'.webp')}.`) && asset.endsWith('.webp'), `${slug}: intended cover subject`);
  const file = localAsset(asset);
  assert.ok(statSync(file).size < 220_000, 'Editorial cover budget');
  const digest = await imageDigest(file);
  assert.ok(!paths.has(asset) && !pixels.has(digest), `${slug}: distinct delivered cover`);
  paths.add(asset); pixels.add(digest);
}
console.log('Published Svelte business journeys, accessibility copy, privacy, appearance and editorial artwork verified.');
