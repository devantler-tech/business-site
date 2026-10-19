import assert from "node:assert/strict";
import { after, before, test } from "node:test";
import { cpSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath, pathToFileURL } from "node:url";
import { buildContactRenderer } from "../private-contact/build.mjs";

/** @typedef {import("../src/lib/private-contact/projection.js").ContactProjection} ContactProjection */
/** @type {Awaited<ReturnType<typeof buildContactRenderer>>} */
let artifact;
/** @type {(contact: ContactProjection) => string} */
let renderContact;

before(async () => {
  artifact = await buildContactRenderer();
  ({ renderContact } = await import(pathToFileURL(artifact.entry).href));
});
after(() => {
  if (artifact) rmSync(artifact.directory, { recursive: true });
});

/** @param {"en" | "da"} locale @returns {ContactProjection} */
function contact(locale = "en") {
  return {
    locale,
    name: "Example Client",
    email: "client@example.test",
    company: "Example Company",
    revision: "9223372036854775807",
    state: "normal",
    verification: "confirmable",
    csrf: "fictional-form-nonce",
    text: {
      title: "Your contact details",
      intro: "Keep your details together in your private client profile.",
      nameLabel: "Your name",
      emailLabel: "Contact email",
      companyLabel: "Company (optional)",
      saveLabel: "Save contact details",
      pendingText: "This contact email is not verified.",
      workspaceLabel: "Back to your workspace",
      verifyLabel: "Confirm with your sign-in provider",
      verificationHelp: "Confirm your saved address with the same sign-in account.",
      errorText: "",
      reloadLabel: "Reload contact details",
    },
  };
}

test("the actual compiled component renders the complete native contact form", () => {
  const body = renderContact(contact());
  assert.match(body, /<main id="main">/u);
  assert.match(body, /<h1>Your contact details<\/h1>/u);
  assert.match(body, /<form[^>]*method="post"[^>]*action="\/portal\/contact"/u);
  assert.match(body, /name="csrf_token" value="fictional-form-nonce"/u);
  assert.match(body, /name="revision" value="9223372036854775807"/u);
  assert.match(body, /id="contact-name"[^>]*name="name"[^>]*value="Example Client"[^>]*maxlength="120"[^>]*required/u);
  assert.match(body, /id="contact-email"[^>]*type="email"[^>]*maxlength="254"[^>]*aria-describedby="contact-pending"[^>]*required/u);
  assert.match(body, /id="contact-company"[^>]*maxlength="160"/u);
  assert.match(body, /<button type="submit">Save contact details<\/button>/u);
  assert.match(body, /href="\/portal\/\?lang=en"/u);
});

test("Danish output keeps native localized limits, destinations and labels", () => {
  const data = contact("da");
  Object.assign(data.text, { title: "Dine kontaktoplysninger", saveLabel: "Gem kontaktoplysninger", reloadLabel: "Genindlæs kontaktoplysninger" });
  const body = renderContact(data);
  assert.match(body, /<h1>Dine kontaktoplysninger<\/h1>/u);
  assert.match(body, /højst 120 tegn/u);
  assert.match(body, /højst 254 tegn/u);
  assert.match(body, /højst 160 tegn/u);
  assert.match(body, /name="lang" value="da"/u);
  assert.match(body, /href="\/portal\/contact\?lang=da"/u);
  assert.match(body, />Genindlæs kontaktoplysninger<\/a>/u);
  assert.match(body, /href="\/portal\/\?lang=da"/u);
});

for (const state of /** @type {const} */ (["invalid", "stale", "verification"])) {
  test(`${state} recovery preserves unsaved values and shows the supplied alert`, () => {
    const data = contact();
    data.state = state;
    data.text.errorText = "Reload your saved details before trying again.";
    data.name = "Unsaved Example";
    const body = renderContact(data);
    assert.match(body, /role="alert">Reload your saved details before trying again\.<\/p>/u);
    assert.match(body, /value="Unsaved Example"/u);
    assert.match(body, /href="\/portal\/contact\?lang=en"/u);
    assert.doesNotMatch(body, /action="\/portal\/contact\/verify"/u);
  });
}

test("confirmation requires an ordinary saved confirmable projection", () => {
  assert.match(renderContact(contact()), /action="\/portal\/contact\/verify"/u);
  assert.match(renderContact(contact()), /aria-describedby="contact-verification-help"/u);
  for (const verification of /** @type {const} */ (["unverified", "verified"])) {
    const data = contact();
    data.verification = verification;
    assert.doesNotMatch(renderContact(data), /action="\/portal\/contact\/verify"/u);
  }
  const initial = contact();
  initial.revision = "0";
  initial.name = "";
  initial.email = "";
  initial.company = "";
  assert.doesNotMatch(renderContact(initial), /action="\/portal\/contact\/verify"/u);
  assert.match(renderContact(initial), /name="revision" value="0"/u);
});

test("profile and localized text remain text in their actual HTML contexts", () => {
  const data = contact();
  data.name = '\"><script>alert(1)</script>&';
  data.company = '<img src=x onerror="alert(2)">';
  data.text.title = "<script>private</script>&";
  data.csrf = 'nonce" autofocus="true';
  const body = renderContact(data);
  assert.match(body, /value="&quot;>&lt;script>alert\(1\)&lt;\/script>&amp;"/u);
  assert.match(body, /value="&lt;img src=x onerror=&quot;alert\(2\)&quot;>"/u);
  assert.match(body, /<h1>&lt;script>private&lt;\/script>&amp;<\/h1>/u);
  assert.match(body, /value="nonce&quot; autofocus=&quot;true"/u);
  assert.doesNotMatch(body, /<script\b|<img\b| autofocus="true"/u);
});

test("independent repeated calls do not reuse another profile's values", () => {
  const first = contact();
  const second = contact("da");
  first.name = "First Example";
  first.csrf = "first-fictional-nonce";
  second.name = "Second Example";
  second.csrf = "second-fictional-nonce";
  const visits = /** @type {[ContactProjection, string, string][]} */ ([[first, "First Example", "Second Example"], [second, "Second Example", "First Example"], [first, "First Example", "Second Example"]]);
  for (const [data, expected, forbidden] of visits) {
    const body = renderContact(data);
    assert.ok(body.includes(`value="${expected}"`));
    assert.ok(!body.includes(forbidden));
    assert.ok(body.includes(`value="${data.csrf}"`));
    assert.doesNotMatch(body, /<script\b|<head\b|<meta\b|rel="canonical"/u);
  }
});

test("an actual render failure exposes only the generic error", () => {
  const data = contact();
  Object.defineProperty(data, "text", { get() { throw new Error("fictional-private-diagnostic"); } });
  assert.throws(() => renderContact(data), (error) => {
    assert.ok(error instanceof Error);
    assert.equal(error.message, "contact rendering unavailable");
    assert.equal(error.cause, undefined);
    assert.doesNotMatch(error.stack ?? "", /fictional-private-diagnostic/u);
    return true;
  });
});

test("the private artifact retains the upstream runtime license texts", () => {
  const web = fileURLToPath(new URL("../", import.meta.url));
  for (const [name, license] of [["svelte", "LICENSE.md"], ["clsx", "license"], ["esm-env", "LICENSE"]]) {
    assert.equal(readFileSync(join(artifact.directory, "notices", name, license), "utf8"), readFileSync(join(web, "node_modules", name, license), "utf8"));
  }
});

test("an actual compiler failure removes only the builder's temporary artifact", () => {
  const workspace = mkdtempSync(join(tmpdir(), "business-contact-build-failure-"));
  const temporary = join(workspace, "temporary");
  try {
    mkdirSync(temporary);
    cpSync(fileURLToPath(new URL("../private-contact/build.mjs", import.meta.url)), join(workspace, "build.mjs"));
    symlinkSync(fileURLToPath(new URL("../node_modules", import.meta.url)), join(workspace, "node_modules"), "dir");
    writeFileSync(join(workspace, "entry.mjs"), "export function invalid( {\n");
    const result = spawnSync(process.execPath, ["--input-type=module", "-e", `
      import { buildContactRenderer } from "./build.mjs";
      try { await buildContactRenderer(); process.exitCode = 1; }
      catch { process.stdout.write("compiler failure observed"); }
    `], { cwd: workspace, env: { PATH: dirname(process.execPath), LC_ALL: "C", TMPDIR: temporary }, encoding: "utf8", timeout: 10_000, maxBuffer: 128 * 1024 });
    assert.equal(result.status, 0, result.stderr);
    assert.equal(result.stdout, "compiler failure observed");
    assert.deepEqual(readdirSync(temporary), []);
    assert.equal(readFileSync(join(workspace, "entry.mjs"), "utf8"), "export function invalid( {\n");
  } finally {
    rmSync(workspace, { recursive: true });
  }
});

test("the copied bundle runs with no checkout or project dependencies", () => {
  const relocated = mkdtempSync(join(tmpdir(), "business-contact-relocated-"));
  try {
    cpSync(artifact.directory, relocated, { recursive: true });
    const result = spawnSync(process.execPath, ["--input-type=module", "-e", `
      import { pathToFileURL } from "node:url";
      const { renderContact } = await import(pathToFileURL(process.argv[1]).href);
      process.stdout.write(renderContact(JSON.parse(process.argv[2])));
    `, join(relocated, "contact.mjs"), JSON.stringify(contact())], {
      cwd: relocated,
      env: { PATH: dirname(process.execPath), LC_ALL: "C" },
      encoding: "utf8",
      timeout: 10_000,
      maxBuffer: 128 * 1024,
    });
    assert.equal(result.status, 0, result.stderr);
    assert.match(result.stdout, /value="Example Client"/u);
    assert.match(result.stdout, /name="revision" value="9223372036854775807"/u);
  } finally {
    rmSync(relocated, { recursive: true });
  }
});
