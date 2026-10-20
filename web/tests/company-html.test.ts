import test from "node:test";
import assert from "node:assert/strict";
import { companyHtml } from "../src/lib/company-html.ts";

const html = '<address><span>__BUSINESS_ADDRESS_LINE_1__</span><span>__BUSINESS_ADDRESS_LINE_2__</span></address>';

test("address text cannot inject HTML or replacement-string operations", () => {
  assert.equal(companyHtml(html, ['Example <script>"&\'</script> $& 1', "1234 Exampletown"]), '<address><span>Example &lt;script&gt;&quot;&amp;&#39;&lt;/script&gt; $&amp; 1</span><span>1234 Exampletown</span></address>');
});

test("inserted text is never interpreted as a second address slot", () => {
  assert.equal(companyHtml(html,["__BUSINESS_ADDRESS_LINE_2__", "1234 Exampletown"]), '<address><span>__BUSINESS_ADDRESS_LINE_2__</span><span>1234 Exampletown</span></address>');
});

test("missing or duplicate disclosure slots fail instead of losing an address line", () => {
  for (const altered of [html.replace("__BUSINESS_ADDRESS_LINE_1__",""),html + "__BUSINESS_ADDRESS_LINE_2__"]) {
    assert.throws(() => companyHtml(altered,["Example Road 1","1234 Exampletown"]), /exactly two address slots/);
  }
  assert.throws(() => companyHtml(html,["Example Road 1"]), /exactly two address slots/);
});
