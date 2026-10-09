import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { test } from 'node:test';
import { runInNewContext } from 'node:vm';

const [directory, ...extra] = process.argv.slice(2);
assert.ok(directory && extra.length === 0, 'Usage: check-client-facing.mjs <build-directory>');
const page = (path) => readFileSync(resolve(directory, path, 'index.html'), 'utf8');
const enabled = (process.env.FEATURE_CLIENT_PORTFOLIO ?? 'true') === 'true';
const featured = (process.env.FEATURE_FEATURED_PORTFOLIO ?? 'true') === 'true';
const catalogue = JSON.parse(readFileSync(new URL('../src/data/public-products.json', import.meta.url), 'utf8'));
const stars = JSON.parse(readFileSync(new URL('../src/data/github-stars.json', import.meta.url), 'utf8'));
const anchors = {
  ksail: ['️-ksail---'], 'data-product-controller': ['-data-product-controller--'],
  'world-at-ruin': ['️-world-at-ruin--'], actions: ['-reusable-workflows-', '-actions-'],
  'agent-skills': ['-agent-skills--'], 'agent-plugins': ['-agent-plugins---'],
  'provider-upjet-unifi': ['-unifi-provider---'], 'kyverno-policies': ['️-kyverno-policies--'],
};

for (const locale of ['en', 'da']) {
  const prefix = locale === 'da' ? 'da/' : '';
  const home = page(prefix);
  const projects = page(`${prefix}projects`);
  test(`${locale}: rollout follows the build-time flag and enabled production default`, () => {
    assert.equal(home.includes('data-client-quality'), enabled || featured);
    assert.equal(projects.includes('data-client-portfolio'), enabled || featured);
  });
  if (!enabled && !featured) {
    test(`${locale}: disabled rollout preserves the existing Home portfolio destination`, () => {
      assert.ok(home.includes(`href="/${prefix}projects/#open-title"`));
    });
    continue;
  }
  test(`${locale}: Home stays client-facing and Projects uses only controlled public destinations`, () => {
    for (const html of [home, projects]) {
      for (const [, href] of html.matchAll(/href="([^"]+)"/g)) {
        assert.ok(!/^https?:\/\/(?:www\.)?github\.com\//i.test(href) || (featured && html === projects && /^https:\/\/github\.com\/(?:devantler-tech\/[\w.-]+|orgs\/devantler-tech\/repositories)$/.test(href)), `Unexpected source-code destination: ${href}`);
      }
    }
  });
  test(`${locale}: visible Home quality and security precede prices and examples`, () => {
    const quality = home.match(/<section\b[^>]*id="quality"[^>]*>([\s\S]*?)<\/section>/)?.[1];
    assert.ok(quality, 'A directly addressable quality highlight is on Home');
    assert.ok(home.indexOf('id="quality"') < home.indexOf('id="services"'));
    assert.ok(home.indexOf('id="quality"') < home.indexOf('id="work"'));
    assert.doesNotMatch(quality, /<details\b/);
    assert.equal((quality.match(/<h3\b/g) ?? []).length, 3);
    assert.ok(quality.includes(locale === 'da' ? 'Sikkerhed' : 'Security'));
    assert.ok(quality.includes(locale === 'da' ? 'aftales' : 'agreed'));
    assert.doesNotMatch(quality, /CodeQL|GitHub|Kubernetes|linters|mergeregler/);
  });
  test(`${locale}: Projects keep honest examples with tools leading the featured presentation`, () => {
    const examples = projects.indexOf('aria-labelledby="family-title"');
    const tools = projects.indexOf('aria-labelledby="open-title"');
    assert.ok(examples >= 0 && tools >= 0);
    assert.ok(featured ? tools < examples : examples < tools);
    const index = projects.match(/<nav[^>]*class="project-index[^>]*>([\s\S]*?)<\/nav>/)?.[1];
    assert.ok(index);
    assert.deepEqual([...index.matchAll(/href="#([^"]+)"/g)].map(([, id]) => id), featured ? ['open-title', 'family-title', 'research'] : ['family-title', 'open-title', 'research']);
    if (!featured) assert.ok(projects.includes(locale === 'da' ? 'ikke betalte kundeopgaver' : 'not paid client commissions'));
    for (const id of featured ? ['coaching'] : ['coaching', 'wedding']) assert.match(projects, new RegExp(`data-work-example="${id}"`));
    assert.ok(projects.includes('href="https://ascoachingogvaner.dk/"'));
    if (!featured) assert.ok(projects.includes(locale === 'da' ? 'Anonymiseret demo' : 'Anonymized guest-view demo'));
  });
  test(`${locale}: the selected catalogue stays visible, ranked and individually identifiable`, () => {
    const shelf = projects.match(/<section\b[^>]*aria-labelledby="open-title"[^>]*>([\s\S]*?)<\/section>/)?.[1];
    assert.ok(shelf);
    assert.doesNotMatch(shelf, /<details\b/);
    const cards = [...shelf.matchAll(/<article\b[^>]*data-public-product="([^"]+)"[^>]*data-stars="(\d+)"[^>]*>([\s\S]*?)<\/article>/g)];
    assert.equal(cards.length, featured ? 6 : 13);
    if (!featured) assert.deepEqual(cards.map(([, repo]) => repo).sort(), catalogue.map((item) => item.repository).sort());
    for (const [, repo, count, html] of cards) {
      assert.equal(Number(count), stars.repositories[repo]);
      assert.ok(html.includes(catalogue.find((item) => item.repository === repo).name));
      for (const id of anchors[repo] ?? []) assert.ok(html.includes(`id="${id}"`));
    }
    for (let i = 1; i < cards.length; i++) {
      const before = cards[i - 1], after = cards[i];
      assert.ok(Number(before[2]) > Number(after[2]) || (before[2] === after[2] && before[1] < after[1]));
    }
    assert.ok(shelf.includes(locale === 'da' ? 'Kildekode tilgængelig' : 'Source-available'));
    assert.ok(shelf.includes('PolyForm Shield'));
    assert.ok(shelf.includes(locale === 'da' ? 'licens' : 'licence'));
    assert.ok(shelf.includes(featured ? (locale === 'da' ? 'alt="KSails desktopprogram' : 'alt="KSail desktop application') : 'alt="KSail CLI"'));
    assert.ok(shelf.includes('href="https://ksail.devantler.tech"'));
  });
  test(`${locale}: old engineering bookmark now leads to visible Home highlights`, () => {
    assert.doesNotMatch(projects, /<details[^>]*id="engineering-checks"/);
    const bridge = projects.match(/<aside[^>]*id="engineering-checks"[^>]*>([\s\S]*?)<\/aside>/)?.[1];
    assert.ok(bridge?.includes(`href="/${prefix}#quality"`));
  });
  test(`${locale}: research, hosting and earlier bookmarks remain real destinations`, () => {
    assert.ok(projects.includes('data-hosting-project'));
    assert.ok(projects.includes(locale === 'da' ? 'ikke en hostinggaranti' : 'not a hosting guarantee'));
    for (const id of ['technical-title', 'technical-projects', 'more-public-products', '️-platform---', '-self-hosted-personal-apps', '-data-product-', '-exploration-of-state-of-the-art-technology-architectures-and-tools-to-create-future-proof-data-spaces']) {
      assert.ok(projects.includes(`id="${id}"`), `Preserve ${id}`);
    }
    assert.ok(projects.includes('href="/pdfs/thesis.pdf"'));
    assert.ok(projects.includes('alt="Data Space as a Data Mesh"'));
    const script = projects.match(/<script[^>]*data-project-reveal[^>]*>([\s\S]*?)<\/script>/)?.[1];
    assert.ok(script);
    const details = { open: false };
    let scrolled = false;
    runInNewContext(script, {
      location: { hash: '#-data-product-' },
      document: { getElementById: (id) => id === '-data-product-' ? { closest: () => details, scrollIntoView: () => { scrolled = true; } } : null },
      window: { addEventListener() {} },
    });
    assert.ok(details.open && scrolled);
  });
}
