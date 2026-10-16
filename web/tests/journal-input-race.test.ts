import test from "node:test";
import { execFileSync } from "node:child_process";

test("fingerprinting reads the opened file, never a replacement after its inspection", () => {
  // Patch only this child's filesystem seam. Both implementations reach a real
  // path replacement: the old one after lstat, the repaired one after open.
  execFileSync(process.execPath, ["--input-type=module", "-e", String.raw`
    import assert from 'node:assert/strict';
    import fs from 'node:fs';
    import { tmpdir } from 'node:os';
    import { join } from 'node:path';
    import { syncBuiltinESMExports } from 'node:module';
    import { fileURLToPath } from 'node:url';
    const {readingFingerprint} = await import(process.argv[1]);
    const file = fileURLToPath(new URL('./.journal-race-' + process.pid + '.txt', process.argv[1]));
    const temporary = fs.mkdtempSync(join(tmpdir(), 'journal-race-'));
    const replacement = join(temporary, 'replacement.txt');
    let created = false;
    try {
      fs.writeFileSync(file, 'reviewed source bytes', {flag:'wx'});
      created = true;
      fs.writeFileSync(replacement, 'unreviewed replacement bytes');
      const before = readingFingerprint();
      let replaced = false;
      const replace = path => {
        if (path !== file || replaced) return;
        fs.unlinkSync(file);
        fs.symlinkSync(replacement, file);
        replaced = true;
      };
      const lstat = fs.lstatSync;
      const open = fs.openSync;
      fs.lstatSync = (...args) => { const result = lstat(...args); replace(args[0]); return result; };
      fs.openSync = (...args) => { const result = open(...args); replace(args[0]); return result; };
      syncBuiltinESMExports();
      const after = readingFingerprint();
      assert.ok(replaced, 'the race control must actually replace the input path');
      assert.equal(after, before, 'read the inspected file descriptor, not replacement bytes');
      assert.throws(() => readingFingerprint(), /ELOOP|Journal/, 'an already substituted symlink must be rejected');
    } finally {
      if (created) fs.unlinkSync(file);
      fs.rmSync(temporary, {recursive:true});
    }
  `, new URL("../../src/data/journal-reading.mjs", import.meta.url).href], {stdio:"pipe"});
});
