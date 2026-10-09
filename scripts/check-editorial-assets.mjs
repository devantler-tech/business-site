import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { readdirSync, readFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { load } from 'js-yaml';
import sharp from 'sharp';

/** Reject reused paths or decoded pixels, including covers assigned to another site subject. */
export function assertUniqueIllustrations(covers, otherImages = [], label = 'Journal cover') {
  assert.ok(covers.length > 0, 'Journal cover inventory must not be empty');
  const paths = new Set();
  const pixels = new Set();
  for (const cover of covers) {
    assert.ok(!paths.has(cover.path), `${cover.slug}: duplicate ${label} path`);
    assert.ok(!pixels.has(cover.digest), `${cover.slug}: duplicate ${label} pixels under another filename`);
    assert.ok(!otherImages.some(image => image.path === cover.path || image.digest === cover.digest),
      `${cover.slug}: Journal cover reused for an unrelated site subject`);
    paths.add(cover.path);
    pixels.add(cover.digest);
  }
}

/** Hash decoded dimensions and RGBA pixels so a re-encoding cannot disguise duplicate artwork. */
export async function imageDigest(path) {
  const { data, info } = await sharp(path).ensureAlpha().raw().toBuffer({ resolveWithObject: true });
  return createHash('sha256').update(`${info.width}x${info.height}:`).update(data).digest('hex');
}

/** Read every actual post's local cover and decoded fingerprint from its validated frontmatter. */
export async function readJournalCovers(repository) {
  const blog = join(repository, 'src/content/docs/blog');
  const covers = [];
  for (const file of readdirSync(blog).filter(name => /\.mdx?$/.test(name))) {
    const source = readFileSync(join(blog, file), 'utf8');
    const frontmatter = source.match(/^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)/);
    assert.ok(frontmatter, `${file}: parseable frontmatter required`);
    const cover = load(frontmatter[1]).cover;
    assert.equal(typeof cover?.image, 'string', `${file}: local cover image required`);
    const path = resolve(blog, cover.image);
    assert.ok(path.startsWith(join(repository, 'src/assets') + '/'), `${file}: cover must remain a local source asset`);
    covers.push({ slug: file.replace(/\.mdx?$/, ''), path, alt: cover.alt, digest: await imageDigest(path) });
  }
  return covers;
}

/** Validate the complete Journal inventory against site imports and the two research subjects. */
export async function checkEditorialAssets(repository) {
  const covers = await readJournalCovers(repository);
  const otherImages = [];
  const source = join(repository, 'src');
  for (const file of readdirSync(source, { recursive: true }).filter(name => /\.(?:astro|mdx?|tsx?)$/.test(name) && !name.startsWith('content/docs/blog/'))) {
    const path = join(source, file);
    for (const match of readFileSync(path, 'utf8').matchAll(/(?:from\s*|src=)\s*["']([^"']+\.(?:webp|png|jpe?g))["']/g)) {
      const asset = resolve(dirname(path), match[1]);
      if (asset.startsWith(join(repository, 'src/assets') + '/')) otherImages.push({ path: asset, digest: await imageDigest(asset) });
    }
  }
  assertUniqueIllustrations(covers, otherImages);
  const researchSubjects = [];
  for (const [slug, file, variable] of [
    ['data-space-research', 'components/business/BusinessProjects.astro', 'research'],
    ['historical-data-product', 'content/docs/projects/completed.mdx', 'dataProductImg'],
  ]) {
    const filePath = join(source, file);
    const imported = readFileSync(filePath, 'utf8').match(new RegExp(`^import ${variable} from ["']([^"']+)["'];`, 'm'));
    assert.ok(imported, `${slug}: actual subject illustration import required`);
    const path = resolve(dirname(filePath), imported[1]);
    researchSubjects.push({ slug, path, digest: await imageDigest(path) });
  }
  assertUniqueIllustrations(researchSubjects, [], 'research illustration');
  for (const cover of covers) assert.match(cover.alt, /^Illustration of /, `${cover.slug}: honest descriptive illustration alt text`);
  console.log(`Editorial uniqueness: ${covers.length} distinct Journal covers; no reuse among ${otherImages.length} other site image references; both research subjects distinct.`);
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  await checkEditorialAssets(resolve(fileURLToPath(new URL('..', import.meta.url))));
}
