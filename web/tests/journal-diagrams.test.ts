import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import { compile } from "svelte/compiler";

const script = readFileSync(new URL("../src/lib/journal-diagrams.js", import.meta.url), "utf8");
const tick = () => new Promise(resolve => setTimeout(resolve, 0));
const svg = (content:string) => `<svg viewBox="0 0 1200 200">${content}</svg>`;

test("compiled diagram styles keep mindmap labels on the site's theme-safe palette", () => {
  const {css} = compile(readFileSync(new URL("../src/lib/JournalReading.svelte", import.meta.url), "utf8"), {filename:"JournalReading.svelte"});
  assert.ok(css);
  const shapes = css.code.match(/\.diagram-canvas \.mindmap-node \.label-container\s*\{([^}]+)\}/)?.[1];
  const labels = css.code.match(/\.diagram-canvas \.mindmap-node \.nodeLabel\s*\{([^}]+)\}/)?.[1];
  assert.ok(shapes && labels, "all mindmap nodes need the contained palette, not only the root");
  assert.match(shapes,/fill:\s*var\(--paper\)\s*!important/);
  assert.match(labels,/color:\s*var\(--ink\)\s*!important/);
});

function page(load:() => Promise<unknown>, count = 2, sanitize = (value:string) => value) {
  let themeChanged = () => {};
  const document = {documentElement:{dataset:{theme:"light"}},querySelectorAll:() => figures};
  const figures = Array.from({length:count}, (_, index) => {
    const attributes = new Map([["data-diagram-id", `diagram-${index}`]]);
    const image = {style:{width:""},getAttribute:() => canvas.innerHTML.match(/viewBox="([^"]*)"/)?.[1] ?? null,setAttribute() {}};
    const canvas = {innerHTML:"",set textContent(_value:string) {this.innerHTML = "";},setAttribute:(name:string,value:string) => attributes.set(name,value),querySelector:() => /<svg(?:\s|>)/.test(canvas.innerHTML) ? image : null};
    const status = {textContent:""};
    return {attributes,canvas,status,image,getAttribute:(name:string) => attributes.get(name),setAttribute:(name:string,value:string) => attributes.set(name,value),querySelector:(selector:string) => selector === ".diagram-canvas" ? canvas : selector === ".diagram-status" ? status : {textContent:selector === "details pre" ? `flowchart LR\n A${index}-->B${index}` : `Diagram ${index + 1}`}};
  });
  const actual = script.replace('import DOMPurify from "dompurify";', "").replace('import("mermaid")', "loadMermaid()");
  assert.notEqual(actual,script, "external renderer and sanitizer imports are replaced; the real controller runs");
  runInNewContext(actual, {document,DOMPurify:{sanitize},loadMermaid:load,MutationObserver:class {constructor(callback:() => void) {themeChanged = callback;} observe() {}}});
  return {figures,theme(value:string) {document.documentElement.dataset.theme = value; themeChanged();}};
}

test("the actual controller renders every figure and clears busy state", async () => {
  const view = page(async () => ({default:{initialize() {},async render(id:string,source:string) {return {svg:svg(`${id} ${source}`)};}}}));
  await tick();
  for (const [index, figure] of view.figures.entries()) {
    assert.match(figure.canvas.innerHTML, new RegExp(`A${index}-->B${index}`));
    assert.equal(figure.attributes.get("data-diagram-state"),"rendered");
    assert.equal(figure.attributes.get("aria-busy"),"false");
    assert.equal(figure.image.style.width,"1200px", "wide diagrams retain their authored rendering scale rather than becoming unreadable thumbnails");
  }
});

test("the actual controller sanitizes renderer output before inserting it", async () => {
  const received:string[] = [];
  const output = svg("untrusted renderer output");
  const clean = svg("sanitized renderer output");
  const view = page(async () => ({default:{initialize() {},async render() {return {svg:output};}}}), 1, value => {received.push(value); return clean;});
  await tick();
  assert.deepEqual(received,[output]);
  assert.equal(view.figures[0].canvas.innerHTML,clean);
});

test("a theme change during an asynchronous render never commits stale output or overlaps renderer work", async () => {
  let finish:(value:{svg:string}) => void = () => {};
  let calls = 0;
  let active = 0;
  let maximum = 0;
  let currentTheme = "";
  const view = page(async () => ({default:{initialize(config:{theme:string}) {currentTheme = config.theme;},async render() {
    calls++; active++; maximum = Math.max(maximum,active);
    const result = calls === 1 ? await new Promise<{svg:string}>(resolve => {finish = resolve;}) : {svg:svg(currentTheme)};
    active--; return result;
  }}}));
  await tick();
  view.theme("dark");
  finish({svg:svg("stale light")});
  await tick();
  assert.equal(maximum,1);
  for (const figure of view.figures) {
    assert.equal(figure.canvas.innerHTML,svg("dark"));
    assert.equal(figure.attributes.get("aria-busy"),"false");
  }
});

test("a failed diagram does not suppress a healthy neighbour and retains an actionable alternative", async () => {
  let calls = 0;
  const view = page(async () => ({default:{initialize() {},async render() {if (++calls === 1) throw new Error("bad source"); return {svg:svg("healthy")};}}}));
  await tick();
  assert.equal(view.figures[0].attributes.get("data-diagram-state"),"error");
  assert.match(view.figures[0].status.textContent,/Read its text below or reload/);
  assert.equal(view.figures[1].attributes.get("data-diagram-state"),"rendered");
});

test("missing SVG and invalid dimensions show failure rather than claim a rendered diagram", async () => {
  for (const output of ["<p>no diagram</p>", "<svg></svg>", '<svg viewBox="0 0 0 200"></svg>', '<svg viewBox="0 0 -1 200"></svg>', '<svg viewBox="0 0 NaN 200"></svg>']) {
    const view = page(async () => ({default:{initialize() {},async render() {return {svg:output};}}}), 1);
    await tick();
    const figure = view.figures[0];
    assert.equal(figure.attributes.get("data-diagram-state"),"error",output);
    assert.equal(figure.attributes.get("aria-busy"),"false");
    assert.equal(figure.canvas.innerHTML,"");
    assert.match(figure.status.textContent,/Read its text below or reload/);
  }
});

for (const height of ["0", "-1", "NaN", "Infinity"]) test(`invalid SVG height ${height} shows failure`, async () => {
  const view = page(async () => ({default:{initialize() {},async render() {return {svg:`<svg viewBox="0 0 1200 ${height}"></svg>`};}}}), 1);
  await tick();
  const figure = view.figures[0];
  assert.equal(figure.attributes.get("data-diagram-state"),"error");
  assert.equal(figure.attributes.get("aria-busy"),"false");
  assert.equal(figure.canvas.innerHTML,"");
  assert.match(figure.status.textContent,/Read its text below or reload/);
});

test("a module loading failure is visible for every diagram, never an endless loading state", async () => {
  const view = page(async () => {throw new Error("missing module");});
  await tick();
  for (const figure of view.figures) {
    assert.equal(figure.attributes.get("data-diagram-state"),"error");
    assert.equal(figure.attributes.get("aria-busy"),"false");
    assert.match(figure.status.textContent,/could not be loaded.*Read its text/);
  }
});
