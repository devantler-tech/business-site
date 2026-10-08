#!/usr/bin/env node

// Keeps the About page's hand-written experience roster in step with the CV data. The page keeps
// real markdown headings for each role because they feed its table of contents, so it cannot map
// the roles from the data the way it renders its header and skills; this guard fails instead.

import { createProcessor } from "@mdx-js/mdx";
import { readFile } from "node:fs/promises";
import { relative, resolve } from "node:path";
import { pathToFileURL } from "node:url";

const [aboutPath, cvPath] = process.argv.slice(2);
if (!aboutPath || !cvPath) {
  console.error("usage: check-cv-drift.mjs <about.mdx> <cv.ts>");
  process.exit(2);
}

const workspace = process.env.GITHUB_WORKSPACE || process.cwd();
const displayPath = (path) => relative(workspace, path) || path;

// Roles on the page live under these H2 headings, as `### <title> <span>period</span>` followed by
// an emphasised `organisation, location — engagement` line.
const ROSTER_SECTIONS = new Set(["Professional Experience", "Previous Experience"]);

/** Flattens an MDX node to its plain words, so a heading can be compared with the data. */
const text = (node) => {
  if (typeof node.value === "string" && (node.type === "text" || node.type === "inlineCode")) return node.value;
  return (node.children || []).map(text).join("");
};

/** Blanks the frontmatter so its fences are not parsed as Markdown, keeping line numbers intact. */
const maskAstroFrontmatter = (source) => {
  const frontmatter = source.match(/^(?:﻿)?---[ \t]*\r?\n[\s\S]*?^---[ \t]*(?:\r?\n|$)/m);
  return frontmatter ? frontmatter[0].replace(/[^\r\n]/g, " ") + source.slice(frontmatter[0].length) : source;
};

/** Collects the document's headings and paragraphs in source order, at any nesting depth. */
const flowNodes = (tree) => {
  const found = [];
  const visit = (node) => {
    for (const child of node.children || []) {
      if (child.type === "heading" || child.type === "paragraph") found.push(child);
      visit(child);
    }
  };
  visit(tree);
  return found;
};

/** Reads the roles the page states by hand: each title, period, and organisation line. */
const pageRoster = (tree) => {
  const nodes = flowNodes(tree);
  const roster = [];
  let inRoster = false;
  nodes.forEach((node, i) => {
    if (node.type === "heading" && node.depth === 2) {
      inRoster = ROSTER_SECTIONS.has(text(node).trim());
      return;
    }
    if (!inRoster || node.type !== "heading" || node.depth !== 3) return;
    const span = node.children.find((child) => child.type === "mdxJsxTextElement" && child.name === "span");
    const next = nodes[i + 1];
    const emphasis = next?.type === "paragraph" ? next.children?.[0] : undefined;
    roster.push({
      line: node.position.start.line,
      title: node.children.filter((child) => child !== span).map(text).join("").trim(),
      period: span ? text(span).trim() : "",
      organisation: emphasis?.type === "emphasis" ? text(emphasis).trim() : "",
    });
  });
  return roster;
};

/** The same roles as the CV data states them, written the way the page writes them. */
const dataRoster = (cv) =>
  [...cv.experience, ...cv.earlierExperience].map((role) => ({
    title: role.title,
    period: role.period,
    organisation: `${role.organisation}, ${role.location}` + (role.engagement ? ` — ${role.engagement}` : ""),
  }));

const source = maskAstroFrontmatter(await readFile(aboutPath, "utf8"));
const page = pageRoster(createProcessor({ format: "mdx" }).parse({ path: aboutPath, value: source }));

let cv;
try {
  ({ cv } = await import(pathToFileURL(resolve(cvPath)).href));
} catch (error) {
  console.error(`Unable to load ${displayPath(cvPath)} (Node 22.18+ is needed to import TypeScript): ${error.message}`);
  process.exit(2);
}
const data = dataRoster(cv);

const problems = [];
if (page.length !== data.length) {
  problems.push(
    `${displayPath(aboutPath)} lists ${page.length} roles under Professional/Previous Experience but ${displayPath(cvPath)} has ${data.length}.`,
  );
}
page.slice(0, data.length).forEach((role, i) => {
  for (const field of ["title", "period", "organisation"]) {
    if (role[field] !== data[i][field]) {
      problems.push(
        `${displayPath(aboutPath)}:${role.line}: ${field} "${role[field]}" but role ${i + 1} in ${displayPath(cvPath)} has "${data[i][field]}".`,
      );
    }
  }
});

if (problems.length > 0) {
  console.error("CV drift: the About page and the CV data disagree — update both together.");
  for (const problem of problems) console.error(`  - ${problem}`);
  process.exit(1);
}
console.log(`OK: the About page and ${displayPath(cvPath)} agree on ${data.length} roles.`);
