import test from "node:test";
import assert from "node:assert/strict";

const base = process.env.SAMPLE_URL;
if (!["http://127.0.0.1:5881", "http://127.0.0.1:5882"].includes(base)) {
  throw new Error("SAMPLE_URL must name one of the two fixed loopback samples");
}
const htmlHeaders = { Accept: "text/html" };

async function postAmount(path, amount, origin = base) {
  const html = await (await fetch(base + path, { headers: htmlHeaders })).text();
  const action = html.match(/<form[^>]*\saction="([^"]*)"/)?.[1] || path;
  const target = new URL(action.replaceAll("&amp;", "&"), base);
  assert.equal(target.origin, base, "fixture form must remain on its own loopback server");
  return fetch(target, {
    method: "POST",
    headers: {
      ...htmlHeaders,
      "Content-Type": "application/x-www-form-urlencoded",
      Origin: origin,
      Referer: base + path,
    },
    body: new URLSearchParams({ amount }),
  });
}

for (const path of ["/", "/da/", "/portal/", "/da/portal/"]) {
  test(path, async () => {
    const response = await fetch(base + path, { headers: htmlHeaders });
    assert.equal(response.status, 200);
    assert.match(response.headers.get("content-type"), /text\/html/);
    const html = await response.text();
    assert.equal((html.match(/<article(?:[ >\n])/g) || []).length, 6);
    assert.match(html, /<h1[ >]/);
    assert.match(html, /href="#main"/);
    const title = html.match(/<title[^>]*>(.*?)<\/title>/s)?.[1];
    assert.ok(title && !title.includes("<!--"), "readable metadata, not hydration markers");
    assert.match(html, new RegExp(`<html[^>]*lang="${path.startsWith("/da/") ? "da" : "en"}"`));
    assert.ok(!/<script\b/.test(html), "these samples deliberately require no browser scripts");
    if (path.includes("portal")) {
      assert.match(html, /method="post"/);
      assert.match(html, /name="amount"/);
      assert.match(html, /required/);
    }
  });
}

test("server refuses invalid amounts without relying on browser validation", async () => {
  for (const amount of ["-1", "NaN", "1.111", "<script>alert(1)</script>", "0.00", ""]) {
    const response = await postAmount("/portal/", amount);
    assert.equal(response.status, 400);
    assert.match(response.headers.get("content-type"), /text\/html/);
    const html = await response.text();
    assert.ok(!html.includes("Amount checked:"));
    assert.ok(!html.includes("<script>alert(1)</script>"));
    assert.match(html, /Enter a positive amount/);
  }
});

test("native HTML form results preserve both locales", async () => {
  for (const [path, phrase] of [
    ["/portal/", "Amount checked"],
    ["/da/portal/", "Beløb kontrolleret"],
  ]) {
    const response = await postAmount(path, "1234.56");
    assert.equal(response.status, 200);
    assert.equal(new URL(response.url).pathname, path);
    assert.match(response.headers.get("content-type"), /text\/html/);
    const html = await response.text();
    assert.ok(html.includes(phrase) && html.includes("1234.56"));
  }
});

test("the native same-origin boundary refuses a foreign-origin POST", async () => {
  const response = await postAmount("/portal/", "1234.56", "https://invalid.example");
  assert.equal(response.status, 403);
});
