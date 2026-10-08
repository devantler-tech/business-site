import assert from 'node:assert/strict';
import { readFileSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import sharp from 'sharp';

// One-off asset preparation, not part of the production build. Never overwrite source art.
export async function compressEditorialAssets(files) {
  for (const { source, output } of files) {
    let encoded;
    let quality;
    for (quality of [78, 72, 66, 60]) {
      encoded = await sharp(source).resize(1440, 810, { fit: 'cover' }).webp({ quality, effort: 6 }).toBuffer();
      if (encoded.length < 220_000) break;
    }
    assert.ok(encoded.length < 220_000, 'Editorial asset exceeds the delivery budget');
    // Exclusive creation checks and opens atomically, even if a rival writes while encoding.
    writeFileSync(output, encoded, { flag: 'wx' });
    console.log(`${output}: ${encoded.length} bytes, quality ${quality}`);
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  assert.equal(process.argv.length, 3, 'Usage: compress-editorial-assets.mjs <manifest.json>');
  await compressEditorialAssets(JSON.parse(readFileSync(process.argv[2], 'utf8')));
}
