import products from "./public-products.json" with { type: "json" };
import trackedSnapshot from "./github-stars.json" with { type: "json" };
import { closeSync, constants, fstatSync, openSync, readSync } from "node:fs";

type Product = (typeof products)[number] & { showcaseTool?: boolean };
type StarSnapshot = {
  observedAt: string;
  repositories: Record<string, number>;
  fetchedAt?: string;
  metadata?: Record<string, { private: boolean; archived: boolean; fork: boolean }>;
};

// Missing reads must fail the build, not make a popular product look unstarred.
export function rankPublicProducts(catalogue: Product[], stars: StarSnapshot) {
  if (
    !/^\d{4}-\d{2}-\d{2}$/.test(stars.observedAt) ||
    !Number.isFinite(Date.parse(stars.observedAt)) ||
    new Date(stars.observedAt).toISOString().slice(0, 10) !== stars.observedAt
  ) {
    throw new Error("Public product stars need a valid observation date");
  }
  const names = catalogue.map((product) => product.repository);
  if (new Set(names).size !== names.length || Object.keys(stars.repositories).length !== names.length) {
    throw new Error("Public product stars must match the complete catalogue");
  }
  return catalogue
    .map((product) => {
      const count = stars.repositories[product.repository];
      if (!Object.hasOwn(stars.repositories, product.repository) || !Number.isSafeInteger(count) || count < 0) {
        throw new Error(`Missing or invalid GitHub stars for ${product.repository}; refresh the complete snapshot`);
      }
      return { ...product, stars: count };
    })
    .sort((a, b) => b.stars - a.stars || (a.repository < b.repository ? -1 : a.repository > b.repository ? 1 : 0));
}

// The editorial inventory stays complete. Only maintained tools earn a business
// showcase; live visibility/status and popularity come from a complete forge read.
export function selectPublicTools(catalogue: Product[], stars: StarSnapshot) {
  if (
    !stars.fetchedAt ||
    !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/.test(stars.fetchedAt) ||
    !Number.isFinite(Date.parse(stars.fetchedAt)) ||
    new Date(stars.fetchedAt).toISOString() !== stars.fetchedAt.replace("Z", ".000Z") ||
    stars.fetchedAt.slice(0, 10) !== stars.observedAt
  ) {
    throw new Error("Tools need a valid GitHub observation timestamp");
  }
  if (!stars.metadata || Object.keys(stars.metadata).length !== catalogue.length)
    throw new Error("Incomplete repository metadata");
  for (const { repository } of catalogue) {
    const entry = stars.metadata[repository];
    if (!entry || ["private", "archived", "fork"].some((key) => typeof entry[key as keyof typeof entry] !== "boolean"))
      throw new Error(`Invalid repository metadata for ${repository}`);
  }
  const tools = rankPublicProducts(catalogue, stars).filter(({ repository, showcaseTool }) => {
    const entry = stars.metadata![repository];
    return showcaseTool === true && !entry.private && !entry.archived && !entry.fork;
  });
  if (tools.length < 6) throw new Error("Fewer than six eligible tools; keep the previous publication");
  return tools.slice(0, 6);
}

// This is a build input, not a visitor request or feature flag. The ordinary
// network-free build retains its tracked snapshot; explicit refreshes fail closed.
function buildSnapshot(): StarSnapshot {
  const filename = process.env.PUBLIC_STARS_SNAPSHOT;
  if (!filename) return trackedSnapshot;
  let descriptor: number | undefined;
  try {
    descriptor = openSync(filename, constants.O_RDONLY | constants.O_NONBLOCK | constants.O_NOFOLLOW);
    const info = fstatSync(descriptor);
    if (!info.isFile() || info.size > 1024 * 1024) throw new Error();
    const bytes = Buffer.alloc(1024 * 1024 + 1);
    const length = readSync(descriptor, bytes, 0, bytes.length, 0);
    if (length > 1024 * 1024 || length !== info.size) throw new Error();
    const value = JSON.parse(bytes.subarray(0, length).toString("utf8")) as StarSnapshot;
    selectPublicTools(products, value);
    const age = Date.now() - Date.parse(value.fetchedAt!);
    if (!Number.isFinite(age) || age < 0 || age > 60 * 60 * 1000) throw new Error();
    return value;
  } catch {
    throw new Error("The build-local public ranking is incomplete, invalid or stale");
  } finally {
    if (descriptor !== undefined) closeSync(descriptor);
  }
}

export const publicStarSnapshot = buildSnapshot();
export const publicProducts = rankPublicProducts(products, publicStarSnapshot);
export const starsObservedAt = publicStarSnapshot.observedAt;
