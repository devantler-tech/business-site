#!/usr/bin/env node

import { createProcessor } from "@mdx-js/mdx";
import { readFile } from "node:fs/promises";
import { relative } from "node:path";

const [homepagePath, activeProjectsPath] = process.argv.slice(2);
if (!homepagePath || !activeProjectsPath) {
  console.error("usage: check-homepage-project-parity.mjs <homepage.mdx> <active-projects.mdx>");
  process.exit(2);
}

const workspace = process.env.GITHUB_WORKSPACE || process.cwd();
const displayPath = (path) => relative(workspace, path) || path;
const escapeCommandData = (value) =>
  String(value).replaceAll("%", "%25").replaceAll("\r", "%0D").replaceAll("\n", "%0A");
const escapeCommandProperty = (value) =>
  escapeCommandData(value).replaceAll(":", "%3A").replaceAll(",", "%2C");
const annotate = (path, message) => {
  console.error(
    `::error file=${escapeCommandProperty(displayPath(path))}::${escapeCommandData(message)}`,
  );
  process.exitCode = 1;
};

const descendants = (tree) => {
  const found = [];
  const visit = (node, ancestors = []) => {
    found.push({ node, ancestors });
    for (const child of node.children || []) visit(child, [...ancestors, node]);
  };
  visit(tree);
  return found;
};
const nodes = (tree) => descendants(tree).map(({ node }) => node);

const isJsxNode = (node) =>
  node.type === "mdxJsxFlowElement" || node.type === "mdxJsxTextElement";
const hasRuntimeJsxAttribute = (node) =>
  isJsxNode(node) &&
  (node.attributes || []).some(
    (attribute) =>
      attribute.type === "mdxJsxExpressionAttribute" ||
      (attribute.type === "mdxJsxAttribute" &&
        attribute.value !== null &&
        typeof attribute.value === "object"),
  );
const isRuntimeControlledNode = (node) =>
  ((node.type === "mdxFlowExpression" || node.type === "mdxTextExpression") &&
    (node.data?.estree?.body?.length || 0) > 0) ||
  hasRuntimeJsxAttribute(node);

const text = (node) => {
  if (typeof node.value === "string" && (node.type === "text" || node.type === "inlineCode")) {
    return node.value;
  }
  return (node.children || []).map(text).join("");
};

const maskAstroFrontmatter = (source) => {
  if (!/^(?:\uFEFF)?---[ \t]*\r?\n/.test(source)) return source;

  const frontmatter = source.match(/^(?:\uFEFF)?---[ \t]*\r?\n[\s\S]*?^---[ \t]*(?:\r?\n|$)/m);
  if (!frontmatter) throw new Error("Unterminated Astro frontmatter block");

  return frontmatter[0].replace(/[^\r\n]/g, " ") + source.slice(frontmatter[0].length);
};

const parse = async (path) => {
  try {
    const source = maskAstroFrontmatter(await readFile(path, "utf8"));
    return createProcessor({ format: "mdx" }).parse({ path, value: source });
  } catch (error) {
    error.inputPath = path;
    throw error;
  }
};

let homepage;
let activeProjects;
try {
  [homepage, activeProjects] = await Promise.all([parse(homepagePath), parse(activeProjectsPath)]);
} catch (error) {
  annotate(error.inputPath || homepagePath, `Unable to parse project metadata as MDX: ${error.message}`);
  process.exit(1);
}

const homepageNodes = nodes(homepage);
const homepageH2s = homepageNodes
  .filter((node) => node.type === "heading" && node.depth === 2)
  .sort((left, right) => left.position.start.offset - right.position.start.offset);
const topLevelHomepageH2s = homepage.children
  .filter((node) => node.type === "heading" && node.depth === 2)
  .sort((left, right) => left.position.start.offset - right.position.start.offset);
const featuredHeadings = homepageH2s.filter((heading) => text(heading).trim() === "Featured Projects");

if (featuredHeadings.length !== 1) {
  annotate(
    homepagePath,
    `Expected exactly one rendered '## Featured Projects' heading, found ${featuredHeadings.length}.`,
  );
  process.exit(1);
}

const featuredHeading = featuredHeadings[0];
if (!homepage.children.includes(featuredHeading)) {
  annotate(
    homepagePath,
    "The Featured Projects heading must remain top-level so no runtime JSX ancestor can conditionally hide the guarded section.",
  );
  process.exit(1);
}

const featuredStart = featuredHeading.position.end.offset;
const nextH2 = topLevelHomepageH2s.find(
  (heading) => heading.position.start.offset > featuredStart,
);
const featuredEnd = nextH2?.position.start.offset ?? Number.POSITIVE_INFINITY;
const featuredSectionNodes = homepageNodes.filter(
  (node) => node.position?.start.offset > featuredStart && node.position.start.offset < featuredEnd,
);
const supportedFeaturedComponents = new Set(["Card", "CardGrid", "LinkCard"]);
const unsupportedFeaturedComponents = featuredSectionNodes
  .filter((node) => isJsxNode(node) && !supportedFeaturedComponents.has(node.name))
  .map((node) => node.name || "fragment");

if (unsupportedFeaturedComponents.length > 0) {
  annotate(
    homepagePath,
    `Unsupported JSX component in Featured Projects: [${[
      ...new Set(unsupportedFeaturedComponents),
    ].join(",")}]. Only Card, CardGrid, and statically inspected LinkCard components may render inside the guarded section.`,
  );
  process.exit(1);
}

const collectPatternNames = (pattern) => {
  if (!pattern) return [];
  if (pattern.type === "Identifier") return [pattern.name];
  if (pattern.type === "AssignmentPattern") return collectPatternNames(pattern.left);
  if (pattern.type === "RestElement") return collectPatternNames(pattern.argument);
  if (pattern.type === "ArrayPattern") return pattern.elements.flatMap(collectPatternNames);
  if (pattern.type === "ObjectPattern") {
    return pattern.properties.flatMap((property) =>
      collectPatternNames(property.type === "Property" ? property.value : property.argument),
    );
  }
  return [];
};

const explicitBindings = new Map();
const recordBinding = (name, binding) => {
  if (!name) return;
  explicitBindings.set(name, [...(explicitBindings.get(name) || []), binding]);
};
const esmStatements = homepageNodes
  .filter((node) => node.type === "mdxjsEsm")
  .flatMap((node) => node.data?.estree?.body || []);

for (const statement of esmStatements) {
  if (statement.type === "ImportDeclaration") {
    for (const specifier of statement.specifiers) {
      recordBinding(specifier.local?.name, {
        kind: "import",
        source: statement.source.value,
        imported:
          specifier.type === "ImportSpecifier"
            ? specifier.imported.name || specifier.imported.value
            : specifier.type === "ImportDefaultSpecifier"
              ? "default"
              : "*",
      });
    }
    continue;
  }

  const declaration = statement.declaration || statement;
  if (declaration.type === "VariableDeclaration") {
    for (const declarator of declaration.declarations) {
      for (const name of collectPatternNames(declarator.id)) recordBinding(name, { kind: "local" });
    }
  } else if (
    declaration.type === "FunctionDeclaration" ||
    declaration.type === "ClassDeclaration"
  ) {
    recordBinding(declaration.id?.name, { kind: "local" });
  }
}

const starlightComponentsModule = "@astrojs/starlight/components";
const usedFeaturedComponents = new Set(
  featuredSectionNodes.filter(isJsxNode).map((node) => node.name),
);
const invalidFeaturedBindings = [...usedFeaturedComponents].filter((name) => {
  const bindings = explicitBindings.get(name) || [];
  return (
    bindings.length > 0 &&
    (bindings.length !== 1 ||
      bindings[0].kind !== "import" ||
      bindings[0].source !== starlightComponentsModule ||
      bindings[0].imported !== name)
  );
});

if (invalidFeaturedBindings.length > 0) {
  annotate(
    homepagePath,
    `Featured Projects component bindings must use named exports from ${starlightComponentsModule} without rebinding. Invalid binding(s): [${invalidFeaturedBindings.join(",")}].`,
  );
  process.exit(1);
}

const hasRuntimeExpression = featuredSectionNodes.some(
  (node) =>
    (node.type === "mdxFlowExpression" || node.type === "mdxTextExpression") &&
    (node.data?.estree?.body?.length || 0) > 0,
);
const hasRuntimeContainerAttribute = featuredSectionNodes.some(
  (node) =>
    node.name !== "LinkCard" && hasRuntimeJsxAttribute(node),
);

if (hasRuntimeExpression || hasRuntimeContainerAttribute) {
  annotate(
    homepagePath,
    "Featured Projects must remain statically inspectable. Conditional or computed MDX content can hide rendered LinkCards from the cross-page invariant.",
  );
  process.exit(1);
}

const featuredCards = featuredSectionNodes.filter(
  (node) =>
    (node.type === "mdxJsxFlowElement" || node.type === "mdxJsxTextElement") &&
    node.name === "LinkCard",
);

if (featuredCards.length === 0) {
  annotate(
    homepagePath,
    "No literal LinkCard titles found in the homepage Featured Projects section. Keep literal featured-project titles under that H2 so drift remains checkable.",
  );
  process.exit(1);
}

const featuredTitles = [];
let unsupportedCard = false;
for (const card of featuredCards) {
  const attributes = card.attributes || [];
  const titleAttributes = attributes.filter(
    (attribute) => attribute.type === "mdxJsxAttribute" && attribute.name === "title",
  );
  const hasRuntimeAttribute = attributes.some(
    (attribute) =>
      attribute.type === "mdxJsxExpressionAttribute" ||
      (attribute.type === "mdxJsxAttribute" &&
        attribute.value !== null &&
        typeof attribute.value === "object"),
  );
  const title = titleAttributes[0]?.value;

  if (
    titleAttributes.length !== 1 ||
    typeof title !== "string" ||
    title.trim() === "" ||
    hasRuntimeAttribute
  ) {
    unsupportedCard = true;
    continue;
  }
  featuredTitles.push(title.trim());
}

const uniqueFeaturedTitles = [...new Set(featuredTitles)].sort();
if (
  unsupportedCard ||
  featuredTitles.length !== featuredCards.length ||
  uniqueFeaturedTitles.length !== featuredTitles.length
) {
  annotate(
    homepagePath,
    `Every homepage Featured Projects LinkCard must have exactly one literal title, and titles must be unique. Found ${featuredCards.length} LinkCard(s), ${featuredTitles.length} literal title(s), and ${uniqueFeaturedTitles.length} unique title(s). Expression-valued attributes and spread attributes are unsupported.`,
  );
  process.exit(1);
}

const isActiveProjectsLayoutWrapper = (node) => {
  if (node.type === "root") return true;
  if (node.type !== "mdxJsxFlowElement" || node.name !== "div") return false;

  const attributes = node.attributes || [];
  return (
    attributes.length === 1 &&
    attributes[0].type === "mdxJsxAttribute" &&
    attributes[0].name === "class" &&
    attributes[0].value === "projects-page"
  );
};

const activeHeadings = descendants(activeProjects)
  .filter(
    ({ node, ancestors }) =>
      node.type === "heading" &&
      node.depth === 2 &&
      ancestors.every(isActiveProjectsLayoutWrapper),
  )
  .map(({ node }) => node)
  .filter((heading) => heading.children?.[0]?.type === "link");

const literalActiveTitleNodeTypes = new Set([
  "link",
  "text",
  "inlineCode",
  "emphasis",
  "strong",
  "delete",
]);
if (activeHeadings.some((heading) =>
  descendants(heading.children[0]).some(
    ({ node }) => !literalActiveTitleNodeTypes.has(node.type),
  ),
)) {
  annotate(
    activeProjectsPath,
    "Linked Active Projects H2 titles must remain literal. Runtime expressions or computed JSX can change a rendered project identity without a statically verifiable title.",
  );
  process.exit(1);
}

const activeTitles = [
  ...new Set(
    activeHeadings
      .map((heading) => text(heading.children[0]).trim())
      .filter(Boolean),
  ),
].sort();

if (activeTitles.length === 0) {
  annotate(
    activeProjectsPath,
    "No linked H2 project titles found on Active Projects. The homepage featured-project guard cannot verify its source set.",
  );
  process.exit(1);
}

const activeTitleSet = new Set(activeTitles);
const homepageOnly = uniqueFeaturedTitles.filter((title) => !activeTitleSet.has(title));
if (homepageOnly.length > 0) {
  annotate(
    homepagePath,
    `Homepage featured-project drift: these Featured Projects have no matching linked H2 on Active Projects: [${homepageOnly.join(
      ",",
    )}]. Add or rename the Active Projects section, or remove the stale featured card.`,
  );
  process.exit(1);
}

console.log(
  `OK: Homepage Featured Projects are present on Active Projects (${uniqueFeaturedTitles.length} featured within ${activeTitles.length} linked sections).`,
);
