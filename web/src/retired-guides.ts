/** A retired page forwards to where its content now lives; `fallback` is used when the visitor brings no bookmark. */
export type RetiredGuide = { destination: string; fallback: `#${string}`; name: string };

const applicationTemplate = { destination: "https://github.com/devantler-tech/application-template", fallback: "#readme", name: "the Application Template on GitHub" } as const;

/**
 * The technical guides were retired in #697. Their old addresses stay reachable:
 * template guides forward to the template's own README, the templates index to
 * Projects, and the Agentic Engineering reference to the Journal post that now
 * carries its diagram.
 */
export const retiredGuides = {
  "/agentic-engineering/": { destination: "/blog/how-my-agentic-engineer-turns-problems-into-proved-working-solutions/", fallback: "#_top", name: "How My Agentic Engineer Turns Problems into Proved Working Solutions" },
  "/templates/": { destination: "/projects/", fallback: "#open-title", name: "Projects" },
  "/templates/dotnet/": applicationTemplate,
  "/templates/gitops-tenant-template/": applicationTemplate,
  "/templates/go/": applicationTemplate,
  "/templates/platform-template/": { destination: "https://github.com/devantler-tech/platform-template", fallback: "#readme", name: "the Platform Template on GitHub" },
  "/templates/platform-tenant-template/": applicationTemplate,
} as const satisfies Record<`/${string}/`, RetiredGuide>;

export type RetiredGuidePath = keyof typeof retiredGuides;
export const retiredGuidePaths = Object.keys(retiredGuides) as RetiredGuidePath[];
export const isRetiredGuide = (path: string): path is RetiredGuidePath => Object.hasOwn(retiredGuides, path);

/** A local destination is the canonical page; an external one leaves the noindex redirect canonical to itself. */
export const retiredGuideCanonical = (path: RetiredGuidePath) => {
  const { destination } = retiredGuides[path];
  return `https://devantler.tech${destination.startsWith("/") ? destination : path}`;
};
