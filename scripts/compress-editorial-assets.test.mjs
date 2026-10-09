import assert from 'node:assert/strict';
import { existsSync, mkdtempSync, readdirSync, readFileSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import { test } from 'node:test';
import sharp from 'sharp';
import { compressEditorialAssets } from './compress-editorial-assets.mjs';

/** Create an owned disposable image fixture and register cleanup with the test runner. */
async function fixture(t) {
  const dir = mkdtempSync(join(tmpdir(), 'editorial-compression-'));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  const source = join(dir, 'source.png');
  const output = join(dir, 'output.webp');
  await sharp({ create: { width: 32, height: 18, channels: 3, background: '#101a15' } }).png().toFile(source);
  return { dir, source, output };
}

test('prepares a new budgeted 16:9 WebP without changing its source', async t => {
  const { source, output } = await fixture(t);
  const original = readFileSync(source);
  await compressEditorialAssets([{ source, output }]);
  const image = await sharp(output).metadata();
  assert.deepEqual([image.width, image.height, image.format], [1440, 810, 'webp']);
  assert.ok(readFileSync(output).length < 220_000);
  assert.deepEqual(readFileSync(source), original);
});

test('an output created while encoding is never overwritten', async t => {
  const { source, output } = await fixture(t);
  const pending = compressEditorialAssets([{ source, output }]);
  // Encoding awaits asynchronously: another writer wins before the final write.
  writeFileSync(output, 'another writer owns this artwork');
  await assert.rejects(pending, /EEXIST/);
  assert.equal(readFileSync(output, 'utf8'), 'another writer owns this artwork');
});

test('two competing preparations cannot both write the same destination', async t => {
  const { source, output } = await fixture(t);
  const results = await Promise.allSettled([
    compressEditorialAssets([{ source, output }]),
    compressEditorialAssets([{ source, output }]),
  ]);
  assert.equal(results.filter(result => result.status === 'fulfilled').length, 1);
  const loser = results.find(result => result.status === 'rejected');
  assert.equal(loser?.reason.code, 'EEXIST');
  assert.equal((await sharp(output).metadata()).format, 'webp');
});

test('the actual command refuses an existing destination without modifying it', async t => {
  const { dir, source, output } = await fixture(t);
  writeFileSync(output, 'original artwork');
  const manifest = join(dir, 'manifest.json');
  writeFileSync(manifest, JSON.stringify([{ source, output }]));
  const result = spawnSync(process.execPath, ['scripts/compress-editorial-assets.mjs', manifest], { encoding: 'utf8' });
  assert.notEqual(result.status, 0);
  assert.equal(readFileSync(output, 'utf8'), 'original artwork');
});

test('a partial encoding write never publishes a destination and leaves no temporary files', async t => {
  const { dir, source, output } = await fixture(t);
  const before = readdirSync(dir);
  const failedWrite = (path, bytes, options) => {
    writeFileSync(path, bytes.subarray(0, 8), options);
    throw Object.assign(new Error('simulated storage failure'), { code: 'EIO' });
  };
  await assert.rejects(compressEditorialAssets([{ source, output }], failedWrite), { code: 'EIO' });
  assert.equal(existsSync(output), false, 'an incomplete asset must never become the published path');
  assert.deepEqual(readdirSync(dir), before, 'failed preparation cleans only its own temporary files');
  await compressEditorialAssets([{ source, output }]);
  assert.equal((await sharp(output).metadata()).format, 'webp', 'a retry can publish a complete image');
});
