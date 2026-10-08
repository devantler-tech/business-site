import assert from 'node:assert/strict';
import { existsSync, readFileSync, writeFileSync } from 'node:fs';
import sharp from 'sharp';

// One-off asset preparation, not part of the production build. Never overwrite source art.
const files = JSON.parse(readFileSync(process.argv[2], 'utf8'));
for (const { source, output } of files) {
  assert.ok(!existsSync(output), `Refusing to overwrite ${output}`);
  let encoded;
  let quality;
  for (quality of [78, 72, 66, 60]) {
    encoded = await sharp(source).resize(1440, 810, { fit: 'cover' }).webp({ quality, effort: 6 }).toBuffer();
    if (encoded.length < 220_000) break;
  }
  assert.ok(encoded.length < 220_000, 'Editorial asset exceeds the delivery budget');
  writeFileSync(output, encoded);
  console.log(`${output}: ${encoded.length} bytes, quality ${quality}`);
}
