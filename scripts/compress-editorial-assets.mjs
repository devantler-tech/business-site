import assert from 'node:assert/strict';
import { linkSync, mkdtempSync, readFileSync, rmSync, rmdirSync, writeFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import sharp from 'sharp';

/**
 * Prepare budgeted WebPs without changing source art or replacing another writer's output.
 * Write into an owned directory on the destination filesystem, then atomically link the
 * complete file into place. The optional writer lets tests simulate a partial storage failure.
 */
export async function compressEditorialAssets(files, write = writeFileSync) {
  for (const { source, output } of files) {
    let encoded;
    let quality;
    for (quality of [78, 72, 66, 60]) {
      encoded = await sharp(source).resize(1440, 810, { fit: 'cover' }).webp({ quality, effort: 6 }).toBuffer();
      if (encoded.length < 220_000) break;
    }
    assert.ok(encoded.length < 220_000, 'Editorial asset exceeds the delivery budget');
    const staging = mkdtempSync(join(dirname(output), '.editorial-'));
    const temporary = join(staging, 'asset.webp');
    try {
      write(temporary, encoded, { flag: 'wx' });
      // A hard link publishes only complete bytes and fails EEXIST instead of replacing a rival.
      linkSync(temporary, output);
    } finally {
      rmSync(temporary, { force: true });
      rmdirSync(staging);
    }
    console.log(`${output}: ${encoded.length} bytes, quality ${quality}`);
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  assert.equal(process.argv.length, 3, 'Usage: compress-editorial-assets.mjs <manifest.json>');
  await compressEditorialAssets(JSON.parse(readFileSync(process.argv[2], 'utf8')));
}
