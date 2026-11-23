// Exercise the rendered application package, not the spelling of its source YAML.
import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { mkdtempSync, cpSync, writeFileSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import * as yaml from "js-yaml";

const root = fileURLToPath(new URL("../", import.meta.url));
const checker = path.join(root, "scripts/check-tenant-package.mjs");
const render = (directory = path.join(root, "deploy")) =>
  execFileSync("kubectl", ["kustomize", directory], { encoding: "utf8", timeout: 15000 });
const check = (input) => spawnSync(process.execPath, [checker], { input, encoding: "utf8", timeout: 5000 });

test("the real Kustomize package renders deterministically and admits the public runtime", () => {
  let first;
  assert.doesNotThrow(() => { first = render(); }, "the source-owned package must exist and render");
  assert.equal(render(), first);
  assert.ok(yaml.loadAll(first).some((d) => d?.kind === "PodDisruptionBudget"), "each workload needs a drain-safe budget");
  const result = check(first);
  assert.equal(result.status, 0, result.stderr);
  assert.equal(result.stdout, "tenant package: public-only package accepted\n");
});

test("the artifact root renders the production overlay", () => {
  assert.equal(render(), render(path.join(root, "deploy/prod")));
});

test("the hosted preview uses only its own disclosure input", () => {
  const preview = yaml.loadAll(render(path.join(root, "deploy/dev")));
  assert.deepEqual(preview.map((document) => document.kind), ["WebApp"]);
  const [app] = preview;
  assert.equal(app.metadata.namespace, "business-site-dev");
  assert.equal(app.metadata.name, "business-site-dev");
  assert.equal(app.spec.name, "business-site-dev");
  assert.equal(app.spec.host, "dev.devantler.tech");
  assert.equal(app.spec.replicas, 1);
  assert.deepEqual(app.spec.secretProjections, [{
    secretName: "business-site-dev-disclosure",
    items: [{ key: "company-disclosure", path: "disclosure" }],
  }]);
  const [base] = yaml.loadAll(render(path.join(root, "deploy/base")));
  assert.equal(app.spec.image, base.spec.image);
  assert.deepEqual(app.spec.args, [
    "--public", "/app/public",
    "--listen", "0.0.0.0:8080",
    "--company-disclosure-projection", "/run/webapp-inputs/disclosure",
  ]);
});

test("the rendered network policy allows only the two billing APIs on HTTPS", () => {
  const network = yaml.loadAll(render()).find((d) => d?.kind === "CiliumNetworkPolicy");
  // One named-destination rule keeps egress default deny for every other peer.
  assert.deepEqual(network.spec.egress, [{
    toFQDNs: [{ matchName: "api.numina.app" }, { matchName: "api.stripe.com" }],
    toPorts: [{ ports: [{ port: "443", protocol: "TCP" }] }],
  }]);
  assert.equal(network.spec.enableDefaultDeny, undefined);
});

test("the billing keys synchronize through the same namespaced store without being projected", () => {
  const docs = yaml.loadAll(render());
  const store = docs.find((d) => d?.kind === "SecretStore");
  const app = docs.find((d) => d?.kind === "WebApp");
  const secret = (name) => docs.find((d) => d?.kind === "ExternalSecret" && d.metadata.name === name);
  assert.deepEqual(secret("business-site-numina").spec.secretStoreRef, { name: store.metadata.name, kind: "SecretStore" });
  assert.deepEqual(secret("business-site-stripe").spec.secretStoreRef, { name: store.metadata.name, kind: "SecretStore" });
  assert.deepEqual(secret("business-site-numina").spec.data.map((d) => d.secretKey), ["api-key"]);
  assert.deepEqual(secret("business-site-stripe").spec.data.map((d) => d.secretKey), ["api-key", "webhook-secret"]);
  // The platform WebApp mounts one Secret, never optionally: the platform's
  // business-site-runtime. The billing keys are not in it, so a missing billing
  // entry cannot stop the public pages from starting.
  assert.deepEqual(app.spec.secretProjections.map((p) => p.secretName), ["business-site-runtime"]);
  assert.ok(!app.spec.secretProjections.some((p) => /numina|stripe/.test(p.secretName)));
});

test("the rendered package projects the platform's runtime Secret with portal settings", () => {
  const docs = yaml.loadAll(render());
  const store = docs.find((d) => d?.kind === "SecretStore");
  const disclosure = docs.find((d) => d?.kind === "ExternalSecret" && d.metadata.name === "business-site-disclosure");
  const app = docs.find((d) => d?.kind === "WebApp");
  // The platform composes business-site-runtime; the retained application-owned
  // disclosure synchronization stays valid but is no longer projected.
  assert.equal(store.metadata.namespace, "business-site");
  assert.equal(store.spec.provider.vault.auth.kubernetes.role, "business-site");
  assert.deepEqual(store.spec.provider.vault.auth.kubernetes.serviceAccountRef, { name: "business-site" });
  assert.deepEqual(disclosure.spec.secretStoreRef, { name: store.metadata.name, kind: "SecretStore" });
  assert.notEqual(disclosure.spec.target.name, app.spec.secretProjections[0].secretName);
  assert.deepEqual(app.spec.secretProjections, [{
    secretName: "business-site-runtime",
    items: ["disclosure", "portal-settings", "portal-client-secret", "portal-database-ca", "smtp-username", "smtp-password"].map((path) => ({ key: path === "disclosure" ? disclosure.spec.data[0].secretKey : path, path })),
  }]);
  // Each projected file the runtime reads is a path the projection actually supplies.
  for (const flag of ["--company-disclosure-projection", "--portal-settings-projection"]) {
    const file = app.spec.args[app.spec.args.indexOf(flag) + 1];
    assert.ok(app.spec.secretProjections[0].items.some((item) => `/run/webapp-inputs/${item.path}` === file), flag);
  }
  assert.equal(check(render()).status, 0);
});

test("the disruption budget admits two or three replicas", () => {
  for (const replicas of [2, 3]) {
    const docs = yaml.loadAll(render());
    docs.find((d) => d?.kind === "WebApp").spec.replicas = replicas;
    const result = check(docs.map((d) => yaml.dump(d)).join("---\n"));
    assert.equal(result.status, 0, result.stderr);
  }
});

test("the real admission command accepts the exact proposed staging destination", () => {
  const docs = yaml.loadAll(render());
  docs.find((d) => d?.kind === "WebApp").spec.host = "business-site.platform.devantler.tech";
  const result = check(docs.map((d) => yaml.dump(d)).join("---\n"));
  assert.equal(result.status, 0, result.stderr);
  assert.equal(result.stdout, "tenant package: public-only package accepted\n");
});

for (const host of [
  "business-site.example.invalid",
  "devantler.tech",
  "www.devantler.tech",
  "*.platform.devantler.tech",
  "business-site.*.devantler.tech",
  "business-site.platform.devantler.tech.attacker.invalid",
  "business-site.platform.devantler.tech.",
  "BUSINESS-SITE.platform.devantler.tech",
  "wedding.platform.devantler.tech",
  "business-site.platform.lan",
  "",
]) {
  test(`the real admission command refuses the unapproved hostname ${JSON.stringify(host)}`, () => {
    const docs = yaml.loadAll(render());
    docs.find((d) => d?.kind === "WebApp").spec.host = host;
    const result = check(docs.map((d) => yaml.dump(d)).join("---\n"));
    assert.equal(result.status, 1, result.stdout);
    assert.equal(result.stdout, "");
    assert.equal(result.stderr, "tenant package: rejected input; review the public-only package contract\n");
  });
}

// Each mutation names a distinct forbidden capability or runtime failure.
for (const [name, mutate] of [
  ["an extra Secret resource", (d) => d.push({ apiVersion: "v1", kind: "Secret", metadata: { name: "extra" } })],
  ["a foreign namespace", (d) => { d[0].metadata.namespace = "foreign"; }],
  ["a missing namespace", (d) => { delete d[0].metadata.namespace; }],
  ["a mutable image tag", (d) => { d[0].spec.image = "ghcr.io/devantler-tech/business-site:latest"; }],
  ["a foreign image", (d) => { d[0].spec.image = `ghcr.io/other/business-site@sha256:${"a".repeat(64)}`; }],
  ["an unverified same-repository image digest", (d) => { d[0].spec.image = `ghcr.io/devantler-tech/business-site@sha256:${"a".repeat(64)}`; }],
  ["a production hostname", (d) => { d[0].spec.host = "devantler.tech"; }],
  ["an unknown schema field", (d) => { d[0].spec.command = ["/bin/sh"]; }],
  ["portal activation input", (d) => { d[0].spec.env = [{ name: "PORTAL_IDENTITY", value: "true" }]; }],
  ["migration arguments", (d) => { d[0].spec.args = ["--migrate", "--migration-database-file", "/private/db"]; }],
  ["a broken public directory", (d) => { d[0].spec.args[1] = "/missing"; }],
  ["root execution", (d) => { d[0].spec.runAsUser = 0; }],
  ["a foreign primary group", (d) => { d[0].spec.runAsGroup = 65533; }],
  ["a writable root", (d) => { d[0].spec.readOnlyRootFilesystem = false; }],
  ["a wrong probe", (d) => { d[0].spec.probePath = "/portal/sign-in"; }],
  ["a wrong service port", (d) => { d[0].spec.port = 3000; }],
  ["unbounded memory", (d) => { d[0].spec.memoryLimit = "4Gi"; }],
  ["zero replicas", (d) => { d[0].spec.replicas = 0; }],
  ["one replica under a one-unavailable budget", (d) => { d[0].spec.replicas = 1; }],
  ["missing disclosure", (d) => { d[0].spec.secretProjections = []; }],
  ["an extra private key", (d) => { d[0].spec.secretProjections[0].items.push({ key: "db", path: "db" }); }],
  ["a traversal projection", (d) => { d[0].spec.secretProjections[0].items[0].path = "../disclosure"; }],
  ["an additional Secret projection", (d) => { d[0].spec.secretProjections.push({ secretName: "other", items: [] }); }],
  ["the retired disclosure-only Secret", (d) => { d[0].spec.secretProjections[0].secretName = "business-site-disclosure"; }],
  ["a foreign runtime Secret", (d) => { d[0].spec.secretProjections[0].secretName = "other-runtime"; }],
  ["a missing portal settings item", (d) => { d[0].spec.secretProjections[0].items.splice(1, 1); }],
  ["a missing portal client secret item", (d) => { d[0].spec.secretProjections[0].items.splice(2, 1); }],
  ["a missing database trust item", (d) => { d[0].spec.secretProjections[0].items.splice(3, 1); }],
  ["a migration credential item", (d) => { d[0].spec.secretProjections[0].items.push({ key: "migration-database", path: "migration-database" }); }],
  ["a renamed settings path", (d) => { d[0].spec.secretProjections[0].items[1].path = "settings"; }],
  ["a settings key projected at another key's path", (d) => { d[0].spec.secretProjections[0].items[1].key = "portal-client-secret"; }],
  ["a missing settings argument", (d) => { d[0].spec.args.splice(-2, 2); }],
  ["settings outside the projection", (d) => { d[0].spec.args[d[0].spec.args.length - 1] = "/tmp/portal-settings"; }],
  ["a repeated settings argument", (d) => { d[0].spec.args.push("--portal-settings-projection", "/run/webapp-inputs/portal-settings"); }],
  ["a one-shot invitation argument", (d) => { d[0].spec.args.push("--portal-invite"); }],
  ["world ingress", (d) => { d[1].spec.ingress[0].fromEntities = ["world"]; }],
  ["extra egress", (d) => { d[1].spec.egress = [{ toEntities: ["world"] }]; }],
  ["an empty egress list without default-deny enforcement", (d) => { d[1].spec.egress = []; }],
  ["an omitted egress policy", (d) => { delete d[1].spec.egress; }],
  ["disabled egress default deny", (d) => { d[1].spec.enableDefaultDeny = { egress: false }; }],
  ["an all-pod selector", (d) => { d[1].spec.endpointSelector = {}; }],
  ["a missing disruption budget", (d) => { d.splice(2, 1); }],
  ["a foreign disruption-budget namespace", (d) => { d[2].metadata.namespace = "foreign"; }],
  ["an empty disruption-budget selector", (d) => { d[2].spec.selector = {}; }],
  ["a budget selecting another workload", (d) => { d[2].spec.selector.matchLabels["app.kubernetes.io/name"] = "other"; }],
  ["a drain-blocking budget", (d) => { d[2].spec.maxUnavailable = 0; }],
  ["a percentage budget", (d) => { d[2].spec.maxUnavailable = "50%"; }],
  ["a conflicting minimum-available budget", (d) => { d[2].spec.minAvailable = 1; }],
  ["a missing namespaced store", (d) => { d.splice(3, 1); }],
  ["a missing disclosure synchronization", (d) => { d.splice(4, 1); }],
  ["a duplicate store", (d) => { d.push(structuredClone(d[3])); }],
  ["a foreign store namespace", (d) => { d[3].metadata.namespace = "foreign"; }],
  ["the shared infrastructure role", (d) => { d[3].spec.provider.vault.auth.kubernetes.role = "external-secrets"; }],
  ["a missing store service account", (d) => { delete d[3].spec.provider.vault.auth.kubernetes.serviceAccountRef; }],
  ["a foreign service account", (d) => { d[3].spec.provider.vault.auth.kubernetes.serviceAccountRef.name = "other"; }],
  ["a cross-namespace service account", (d) => { d[3].spec.provider.vault.auth.kubernetes.serviceAccountRef.namespace = "other"; }],
  ["token-based store authentication", (d) => { d[3].spec.provider.vault.auth = { tokenSecretRef: { name: "other", key: "token" } }; }],
  ["a foreign vault endpoint", (d) => { d[3].spec.provider.vault.server = "https://other.invalid"; }],
  ["a different vault mount", (d) => { d[3].spec.provider.vault.path = "other"; }],
  ["a different vault version", (d) => { d[3].spec.provider.vault.version = "v1"; }],
  ["a different authentication mount", (d) => { d[3].spec.provider.vault.auth.kubernetes.mountPath = "other"; }],
  ["a shared cluster store", (d) => { d[4].spec.secretStoreRef.kind = "ClusterSecretStore"; }],
  ["a different namespaced store", (d) => { d[4].spec.secretStoreRef.name = "other"; }],
  ["a foreign disclosure namespace", (d) => { d[4].metadata.namespace = "foreign"; }],
  ["a foreign disclosure target", (d) => { d[4].spec.target.name = "other"; }],
  ["bulk private-data imports", (d) => { d[4].spec.dataFrom = [{ extract: { key: "other" } }]; }],
  ["additional private keys", (d) => { d[4].spec.data.push({ secretKey: "other", remoteRef: { key: "other" } }); }],
  ["a different disclosure source", (d) => { d[4].spec.data[0].remoteRef.key = "apps/other/public-disclosure"; }],
  ["a different disclosure property", (d) => { d[4].spec.data[0].remoteRef.property = "other"; }],
  ["an inline disclosure value", (d) => { d[4].spec.target.template = { data: { "company-disclosure": "fictional private value" } }; }],
  ["a private-value writer", (d) => { d[4].kind = "PushSecret"; }],
  ["public pages served on the portal host", (d) => { d[0].spec.args = d[0].spec.args.slice(0, 6); }],
  ["swapped public and portal hosts", (d) => { d[0].spec.args.splice(6, 4, "--public-host", "portal.devantler.tech", "--portal-host", "devantler.tech"); }],
  ["a missing portal route", (d) => { d.splice(5, 1); }],
  ["a missing public route", (d) => { d.splice(6, 1); }],
  ["a third route", (d) => { const extra = structuredClone(d[5]); extra.metadata.name = "business-site-extra"; d.push(extra); }],
  ["a route claiming another hostname", (d) => { d[6].spec.hostnames.push("evil.devantler.tech"); }],
  ["a portal route on the public listener", (d) => { d[5].spec.parentRefs[0].sectionName = "https-devantler-tech"; }],
  ["a route on the shared platform listener", (d) => { d[6].spec.parentRefs[0].sectionName = "https"; }],
  ["a route to another backend", (d) => { d[5].spec.rules[0].backendRefs[0].name = "other"; }],
  ["a route with a request filter", (d) => { d[6].spec.rules[0].filters = [{ type: "RequestRedirect", requestRedirect: { hostname: "evil.example" } }]; }],
  ["a route in another namespace", (d) => { d[5].metadata.namespace = "foreign"; }],
  ["a missing Numina synchronization", (d) => { d.splice(7, 1); }],
  ["a missing Stripe synchronization", (d) => { d.splice(8, 1); }],
  ["a duplicate billing synchronization", (d) => { d[8] = structuredClone(d[7]); }],
  ["a Numina key from another entry", (d) => { d[7].spec.data[0].remoteRef.key = "apps/business-site/public-disclosure"; }],
  ["a Numina key from another tenant", (d) => { d[7].spec.data[0].remoteRef.key = "apps/other/numina"; }],
  ["a different Numina property", (d) => { d[7].spec.data[0].remoteRef.property = "token"; }],
  ["a Numina synchronization through the shared cluster store", (d) => { d[7].spec.secretStoreRef.kind = "ClusterSecretStore"; }],
  ["a Numina Secret overwriting the disclosure", (d) => { d[7].spec.target.name = "business-site-disclosure"; }],
  ["a merged Numina Secret", (d) => { d[7].spec.target.creationPolicy = "Merge"; }],
  ["an inline Numina value", (d) => { d[7].spec.target.template = { data: { "api-key": "fictional private value" } }; }],
  ["a bulk Stripe import", (d) => { d[8].spec.dataFrom = [{ extract: { key: "apps/business-site/stripe" } }]; }],
  ["an extra Stripe property", (d) => { d[8].spec.data.push({ secretKey: "publishable-key", remoteRef: { key: "apps/business-site/stripe", property: "publishable-key" } }); }],
  ["a missing Stripe webhook secret", (d) => { d[8].spec.data.pop(); }],
  ["a Stripe key from the Numina entry", (d) => { d[8].spec.data[0].remoteRef.key = "apps/business-site/numina"; }],
  ["a billing key projected into the WebApp", (d) => { d[0].spec.secretProjections = [{ secretName: "business-site-stripe", items: [{ key: "api-key", path: "stripe-api-key" }] }]; }],
  ["a third billing API", (d) => { d[1].spec.egress[0].toFQDNs.push({ matchName: "api.example.invalid" }); }],
  ["a billing API wildcard", (d) => { d[1].spec.egress[0].toFQDNs[1] = { matchPattern: "*.stripe.com" }; }],
  ["a catch-all name pattern", (d) => { d[1].spec.egress[0].toFQDNs = [{ matchPattern: "*" }]; }],
  ["plain-HTTP billing egress", (d) => { d[1].spec.egress[0].toPorts[0].ports[0].port = "80"; }],
  ["an extra billing port", (d) => { d[1].spec.egress[0].toPorts[0].ports.push({ port: "8443", protocol: "TCP" }); }],
  ["port-only billing egress", (d) => { delete d[1].spec.egress[0].toFQDNs; }],
  ["unrestricted billing ports", (d) => { delete d[1].spec.egress[0].toPorts; }],
  ["a second egress rule", (d) => { d[1].spec.egress.push(structuredClone(d[1].spec.egress[0])); }],
  ["the previous egress-free policy", (d) => { d[1].spec.egress = [{}]; }],
  ["a missing migration Job", (d) => { d.splice(9, 1); }],
  ["a second migration Job", (d) => { const extra = structuredClone(d[9]); extra.metadata.name = "business-site-extra"; d.push(extra); }],
  ["a migration Job on another image", (d) => { d[9].spec.template.spec.containers[0].image = `ghcr.io/devantler-tech/business-site@sha256:${"a".repeat(64)}`; }],
  ["a mutable migration image tag", (d) => { d[9].spec.template.spec.containers[0].image = "ghcr.io/devantler-tech/business-site:latest"; }],
  ["a migration Job that serves the web", (d) => { d[9].spec.template.spec.containers[0].args = structuredClone(d[0].spec.args); }],
  ["the strict file option, which cannot read a Secret projection", (d) => { d[9].spec.template.spec.containers[0].args[1] = "--migration-database-file"; }],
  ["a migration without the web role grant", (d) => { d[9].spec.template.spec.containers[0].args.splice(3, 2); }],
  ["a migration granting another role", (d) => { d[9].spec.template.spec.containers[0].args[4] = "portal_migrator"; }],
  ["a migration Job running as root", (d) => { d[9].spec.template.spec.securityContext.runAsUser = 0; }],
  ["a migration Job without its file group", (d) => { delete d[9].spec.template.spec.securityContext.fsGroup; }],
  ["a migration Job with a service-account token", (d) => { d[9].spec.template.spec.automountServiceAccountToken = true; }],
  ["a disk-backed migration temp directory", (d) => { delete d[9].spec.template.spec.volumes[1].emptyDir.medium; }],
  ["a writable migration root", (d) => { d[9].spec.template.spec.containers[0].securityContext.readOnlyRootFilesystem = false; }],
  ["a group-readable migration Secret mode", (d) => { d[9].spec.template.spec.volumes[0].secret.defaultMode = 0o440; }],
  ["the web runtime Secret in the migration Job", (d) => { d[9].spec.template.spec.volumes[0].secret.secretName = "business-site-runtime"; }],
  ["an extra migration Secret item", (d) => { d[9].spec.template.spec.volumes[0].secret.items.push({ key: "password", path: "password" }); }],
  ["migration input from the environment", (d) => { d[9].spec.template.spec.containers[0].envFrom = [{ secretRef: { name: "business-site-portal-db-migration" } }]; }],
  ["unbounded migration retries", (d) => { d[9].spec.backoffLimit = 10; }],
  ["no migration deadline", (d) => { delete d[9].spec.activeDeadlineSeconds; }],
  ["a migration Job Flux cannot recreate", (d) => { delete d[9].metadata.annotations; }],
  ["a migration pod labelled as the web workload", (d) => { d[9].spec.template.metadata.labels["app.kubernetes.io/name"] = "business-site"; }],
  ["a migration container that does not itself refuse root", (d) => { delete d[9].spec.template.spec.containers[0].securityContext.runAsNonRoot; }],
  ["a retention container that does not itself refuse root", (d) => { delete d[10].spec.jobTemplate.spec.template.spec.containers[0].securityContext.runAsNonRoot; }],
  ["a missing retention CronJob", (d) => { d.splice(10, 1); }],
  ["a retention CronJob on another image", (d) => { d[10].spec.jobTemplate.spec.template.spec.containers[0].image = `ghcr.io/devantler-tech/business-site@sha256:${"a".repeat(64)}`; }],
  ["a retention CronJob that migrates", (d) => { d[10].spec.jobTemplate.spec.template.spec.containers[0].args = structuredClone(d[9].spec.template.spec.containers[0].args); }],
  ["the strict file option in the retention CronJob", (d) => { d[10].spec.jobTemplate.spec.template.spec.containers[0].args[1] = "--migration-database-file"; }],
  ["overlapping retention runs", (d) => { d[10].spec.concurrencyPolicy = "Allow"; }],
  ["a retention purge more than once a day", (d) => { d[10].spec.schedule = "*/5 * * * *"; }],
  ["retention retries", (d) => { d[10].spec.jobTemplate.spec.backoffLimit = 3; }],
  ["no retention deadline", (d) => { delete d[10].spec.jobTemplate.spec.activeDeadlineSeconds; }],
  ["a retention pod running as root", (d) => { d[10].spec.jobTemplate.spec.template.spec.securityContext.runAsUser = 0; }],
  ["a retention pod with a service-account token", (d) => { d[10].spec.jobTemplate.spec.template.spec.automountServiceAccountToken = true; }],
  ["the web runtime Secret in the retention CronJob", (d) => { d[10].spec.jobTemplate.spec.template.spec.volumes[0].secret.secretName = "business-site-runtime"; }],
  ["a retention pod labelled as the web workload", (d) => { d[10].spec.jobTemplate.spec.template.metadata.labels["app.kubernetes.io/name"] = "business-site"; }],
]) {
  test(`the actual rendered package rejects ${name}`, () => {
    const rendered = yaml.loadAll(render());
    // Each target is selected by kind, independent of Kustomize's sort order.
    const docs = ["WebApp", "CiliumNetworkPolicy", "PodDisruptionBudget", "SecretStore"].map((kind) => rendered.find((d) => d.kind === kind));
    const named = (kind, name) => rendered.find((d) => d.kind === kind && d.metadata.name === name);
    // Index 4 is the disclosure synchronization, selected by name.
    docs.push(named("ExternalSecret", "business-site-disclosure"));
    // The two routes follow at indexes 5 (portal) and 6 (public), selected by name.
    for (const name of ["business-site-portal", "business-site-public"]) docs.push(named("HTTPRoute", name));
    // The billing synchronizations follow at indexes 7 (Numina) and 8 (Stripe).
    for (const name of ["business-site-numina", "business-site-stripe"]) docs.push(named("ExternalSecret", name));
    // The migration Job follows at index 9 and the retention CronJob at 10.
    docs.push(named("Job", "business-site-portal-migration"), named("CronJob", "business-site-portal-retention"));
    assert.equal(check(docs.map((d) => yaml.dump(d)).join("---\n")).status, 0,
      "each refusal must start from a complete accepted package");
    mutate(docs);
    const result = check(docs.map((d) => yaml.dump(d)).join("---\n"));
    assert.equal(result.status, 1, result.stdout);
    assert.equal(result.stdout, "");
    assert.match(result.stderr, /^tenant package: rejected /);
  });
}

test("every package pod passes the cluster's pod-security admission rule", () => {
  // Mirrors the platform's Kyverno validate-pod-security policy, which refused
  // v3.9.0's Job for setting runAsNonRoot only at pod level: each container must
  // itself refuse root and privilege escalation and drop ALL capabilities, and
  // the pod must use the RuntimeDefault or Localhost seccomp profile.
  const docs = yaml.loadAll(render());
  const pods = [
    ...docs.filter((d) => d?.kind === "Job").map((d) => d.spec.template.spec),
    ...docs.filter((d) => d?.kind === "CronJob").map((d) => d.spec.jobTemplate.spec.template.spec),
  ];
  assert.equal(pods.length, 2, "the migration Job and the retention CronJob");
  for (const pod of pods) {
    assert.ok(["RuntimeDefault", "Localhost"].includes(pod.securityContext?.seccompProfile?.type));
    for (const c of [...(pod.initContainers ?? []), ...pod.containers]) {
      const s = c.securityContext ?? {};
      assert.equal(s.allowPrivilegeEscalation, false, `${c.name} allowPrivilegeEscalation`);
      assert.notEqual(s.privileged, true, `${c.name} privileged`);
      assert.equal(s.runAsNonRoot, true, `${c.name} runAsNonRoot`);
      assert.deepEqual(s.capabilities?.drop, ["ALL"], `${c.name} capabilities`);
    }
  }
});

test("the migration Job and retention CronJob run whatever image the publisher pins in the WebApp", () => {
  // The shared publisher pins the WebApp image. The production overlay copies
  // that image into both jobs.
  const fixture = mkdtempSync(path.join(tmpdir(), "tenant-package-"));
  try {
    cpSync(path.join(root, "deploy"), fixture, { recursive: true });
    const pinned = `ghcr.io/devantler-tech/business-site@sha256:${"b".repeat(64)}`;
    const appManifest = path.join(fixture, "base/webapp.yaml");
    const app = yaml.load(readFileSync(appManifest, "utf8"));
    app.spec.image = pinned;
    writeFileSync(appManifest, yaml.dump(app));
    const rendered = yaml.loadAll(render(fixture));
    const job = rendered.find((d) => d?.kind === "Job");
    assert.equal(job.spec.template.spec.containers[0].image, pinned);
    const cron = rendered.find((d) => d?.kind === "CronJob");
    assert.equal(cron.spec.jobTemplate.spec.template.spec.containers[0].image, pinned);
    const cronSource = yaml.load(readFileSync(path.join(root, "deploy/prod/cronjob-portal-retention.yaml"), "utf8"));
    assert.equal(cronSource.spec.jobTemplate.spec.template.spec.containers[0].image, undefined, "the source CronJob names no image of its own");
    const source = yaml.load(readFileSync(path.join(root, "deploy/prod/job-portal-migration.yaml"), "utf8"));
    assert.equal(source.spec.template.spec.containers[0].image, undefined, "the source Job names no image of its own");
  } finally {
    rmSync(fixture, { recursive: true });
  }
});

test("a local Kustomize overlay cannot smuggle an unsupported resource into validation", () => {
  const fixture = mkdtempSync(path.join(tmpdir(), "tenant-package-"));
  try {
    cpSync(path.join(root, "deploy"), fixture, { recursive: true });
    writeFileSync(path.join(fixture, "extra.yaml"), "apiVersion: v1\nkind: Secret\nmetadata:\n  name: extra\n");
    const config = yaml.load(readFileSync(path.join(root, "deploy/kustomization.yaml"), "utf8"));
    config.resources.push("extra.yaml");
    writeFileSync(path.join(fixture, "kustomization.yaml"), yaml.dump(config));
    assert.equal(check(render(fixture)).status, 1);
  } finally {
    rmSync(fixture, { recursive: true });
  }
});

for (const input of ["", "null\n", "apiVersion: v1\napiVersion: v2\n", "---\n", "a".repeat(262145)]) {
  test(`incomplete/ambiguous input is refused (${input.length} bytes)`, () => {
    const result = check(input);
    assert.equal(result.status, 1);
    assert.equal(result.stdout, "");
    assert.match(result.stderr, /^tenant package: rejected /);
  });
}

/** The real CI execution must be unconditional and covered by both aggregates. */
function assertCI(workflow) {
  const job = workflow.jobs["test-combined-runtime"];
  assert.equal(job.if, undefined);
  for (const command of [
    "node --test scripts/tenant-package.test.mjs",
    "node scripts/tenant-runtime-smoke.test.mjs business-site:production-test",
  ]) {
    const steps = job.steps.filter((step) => step.run === command);
    assert.equal(steps.length, 1);
    assert.equal(steps[0].if, undefined);
    assert.ok(steps[0]["continue-on-error"] == null || steps[0]["continue-on-error"] === false);
    assert.equal(steps[0]["working-directory"], ".");
  }
  for (const aggregate of ["validation", "status"]) {
    assert.ok(workflow.jobs[aggregate].needs.includes("test-combined-runtime"));
    assert.equal(workflow.jobs[aggregate].if, "always()");
  }
}

test("actual tenant package and image checks cannot silently become optional in CI", () => {
  const workflow = yaml.load(readFileSync(path.join(root, ".github/workflows/ci.yaml"), "utf8"));
  assertCI(workflow);
  for (const field of ["if", "continue-on-error", "working-directory"]) {
    const changed = structuredClone(workflow);
    const step = changed.jobs["test-combined-runtime"].steps.find((s) => s.run?.startsWith("node scripts/tenant-runtime-smoke"));
    step[field] = field === "working-directory" ? "server" : true;
    assert.throws(() => assertCI(changed));
  }
  const missing = structuredClone(workflow);
  missing.jobs["test-combined-runtime"].steps = missing.jobs["test-combined-runtime"].steps.filter((s) => !s.run?.includes("tenant-runtime-smoke"));
  assert.throws(() => assertCI(missing));
});
