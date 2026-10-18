import test from "node:test";
import assert from "node:assert/strict";

test("copy writes the actual code and reports success only after clipboard completion", async () => {
  let copy;
  await assert.doesNotReject(async () => { copy = (await import("../src/lib/journal-copy.js")).copyJournalCode; }, "the reading copy controller must exist");
  const status = {textContent:""};
  const button = {dataset:{code:"echo hello\u007fecho world"},disabled:false,parentElement:{querySelector:() => status}};
  let written = "";
  await copy!(button, {writeText:async (value:string) => { written = value; assert.equal(status.textContent, ""); }});
  assert.equal(written, "echo hello\necho world");
  assert.equal(status.textContent, "Copied!");
  assert.equal(button.disabled, false);
});

test("clipboard denial is actionable, resets the control and never reports success", async () => {
  let copy;
  await assert.doesNotReject(async () => { copy = (await import("../src/lib/journal-copy.js")).copyJournalCode; });
  const status = {textContent:""};
  const button = {dataset:{code:"echo hello"},disabled:false,parentElement:{querySelector:() => status}};
  await copy!(button, {writeText:async () => { throw new Error("denied"); }});
  assert.equal(status.textContent, "Copy failed. Select the code to copy it.");
  assert.equal(button.disabled, false);
});

test("an empty renderer payload never clears the clipboard or reports success", async () => {
  const {copyJournalCode} = await import("../src/lib/journal-copy.js");
  const status = {textContent:""};
  const button = {dataset:{code:""},disabled:false,parentElement:{querySelector:() => status}};
  let writes = 0;
  await copyJournalCode(button, {writeText:async () => { writes++; }});
  assert.equal(writes, 0);
  assert.equal(status.textContent, "Copy failed. Select the code to copy it.");
  assert.equal(button.disabled, false);
});
