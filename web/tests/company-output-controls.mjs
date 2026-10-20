import assert from "node:assert/strict";
import { cpSync, mkdtempSync, rmSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { assertCompanyOutput } from "./company-output.mjs";

const options = {identity:true,scope:true,vat:false,template:false,address:process.env.COMPANY_POSTAL_ADDRESS ?? ""};
assertCompanyOutput("web/build","dist",options);
for (const kind of ["missing-locale", "missing-address", "unrelated-script-leak", "second-line-leak"]) {
  const scratch = mkdtempSync(join(tmpdir(),"company-output-187-"));
  try {
    const candidate = join(scratch,"candidate");
    cpSync("web/build",candidate,{recursive:true});
    if (kind === "missing-locale") rmSync(join(candidate,"da/company/index.html"));
    if (kind === "missing-address") {
      const page = join(candidate,"company/index.html");
      writeFileSync(page,readFileSync(page,"utf8").replace(/<address\b[\s\S]*?<\/address>/,""));
    }
    if (kind === "unrelated-script-leak") writeFileSync(join(candidate,"unexpected.js"),"company-address-fixture-187");
    if (kind === "second-line-leak") writeFileSync(join(candidate,"unexpected.js"),"1234 Exampletown");
    assert.throws(() => assertCompanyOutput(candidate,"dist",options), kind === "missing-locale" ? /required bilingual page is missing/ : kind === "missing-address" ? /registered facts and escaped address/ : /address leaked outside/,kind);
    console.log(`Rejected real company artifact control: ${kind}`);
  } finally { rmSync(scratch,{recursive:true,force:true}); }
}
