import assert from "node:assert/strict";
import { cpSync, mkdtempSync, rmSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { assertCompanyOutput } from "./company-output.mjs";

const options = {identity:true,scope:true,template:false,address:process.env.COMPANY_POSTAL_ADDRESS ?? ""};
assertCompanyOutput("web/build",options);
for (const kind of ["missing-locale", "missing-address", "unrelated-script-leak", "second-line-leak", "stale-registration", "stale-footer", "missing-legal-page", "broken-withdrawal-form", "phone-leak"]) {
  const scratch = mkdtempSync(join(tmpdir(),"company-output-187-"));
  try {
    const candidate = join(scratch,"candidate");
    cpSync("web/build",candidate,{recursive:true});
    if (kind === "missing-locale") rmSync(join(candidate,"da/company/index.html"));
    if (kind === "missing-address") {
      const page = join(candidate,"company/index.html");
      writeFileSync(page,readFileSync(page,"utf8").replace(/<address\b[\s\S]*?<\/address>/,""));
    }
    if (kind === "phone-leak") writeFileSync(join(candidate,"about/__data.json"),JSON.parse(readFileSync("src/data/company.json","utf8")).phone);
    if (kind === "missing-legal-page") rmSync(join(candidate,"da/terms/index.html"));
    if (kind === "broken-withdrawal-form") {
      const page = join(candidate,"terms/index.html");
      const source = readFileSync(page,"utf8"), changed = source.replace("Signature of consumer(s)","Signature");
      assert.notEqual(changed, source, "the mutation reaches the actual withdrawal form");
      writeFileSync(page,changed);
    }
    if (kind === "unrelated-script-leak") writeFileSync(join(candidate,"unexpected.js"),"company-address-fixture-187");
    if (kind === "second-line-leak") writeFileSync(join(candidate,"unexpected.js"),"1234 Exampletown");
    if (kind === "stale-registration" || kind === "stale-footer") {
      for (const directory of [candidate]) {
        const page = join(directory, kind === 'stale-registration' ? 'company/index.html' : 'index.html');
        const source = readFileSync(page, 'utf8');
        const changed = source.replaceAll('Sole proprietorship', 'Personally owned small business (PMV)');
        assert.notEqual(changed, source, 'the mutation reaches actual current registration output');
        writeFileSync(page, changed);
      }
    }
    assert.throws(() => assertCompanyOutput(candidate,options), kind === "missing-locale" || kind === "missing-legal-page" ? /required bilingual page is missing/ : kind === "broken-withdrawal-form" ? /terms list is missing/ : kind === "phone-leak" ? /phone number leaked/ : kind === "missing-address" ? /required address/ : kind === "stale-registration" ? /registered company fact is missing/ : kind === "stale-footer" ? /ordinary registration/ : /address leaked outside/,kind);
    console.log(`Rejected real company artifact control: ${kind}`);
  } finally { rmSync(scratch,{recursive:true,force:true}); }
}
