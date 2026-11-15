// Application policy over rendered documents; not a Kubernetes admission engine.
import assert from "node:assert/strict";
import { pathToFileURL } from "node:url";
import * as yaml from "js-yaml";

/** Reject extra capabilities rather than trying to enumerate dangerous ones. */
function keys(value, allowed) {
  assert.ok(value && typeof value === "object" && !Array.isArray(value));
  assert.deepEqual(Object.keys(value).sort(), [...allowed].sort());
}

/** Admit only this application's dormant public package; return its runtime spec. */
export function validateTenantPackage(input) {
  assert.ok(typeof input === "string" && Buffer.byteLength(input) <= 262144, "input size");
  const docs = yaml.loadAll(input);
  assert.equal(docs.length, 10, "resource count");
  const app = docs.find((d) => d?.kind === "WebApp");
  const network = docs.find((d) => d?.kind === "CiliumNetworkPolicy");
  const budget = docs.find((d) => d?.kind === "PodDisruptionBudget");
  const store = docs.find((d) => d?.kind === "SecretStore");
  const secrets = docs.filter((d) => d?.kind === "ExternalSecret");
  const secret = (name) => secrets.find((d) => d?.metadata?.name === name);
  const [disclosure, numina, stripe] = ["business-site-disclosure", "business-site-numina", "business-site-stripe"].map(secret);
  assert.equal(secrets.length, 3, "external secret count");
  const routes = docs.filter((d) => d?.kind === "HTTPRoute");
  for (const resource of [app, network, budget]) {
    keys(resource, ["apiVersion", "kind", "metadata", "spec"]);
    keys(resource.metadata, ["name", "namespace"]);
    assert.equal(resource.metadata.name, "business-site");
    assert.equal(resource.metadata.namespace, "business-site");
  }
  assert.equal(app.apiVersion, "kro.run/v1alpha1");
  assert.equal(network.apiVersion, "cilium.io/v2");
  assert.equal(budget.apiVersion, "policy/v1");
  assert.deepEqual(budget.spec, {
    maxUnavailable: 1,
    selector: { matchLabels: { "app.kubernetes.io/name": "business-site" } },
  });
  for (const [resource, name] of [[store, "openbao"], [disclosure, "business-site-disclosure"], [numina, "business-site-numina"], [stripe, "business-site-stripe"]]) {
    keys(resource, ["apiVersion", "kind", "metadata", "spec"]);
    keys(resource.metadata, ["name", "namespace"]);
    assert.equal(resource.apiVersion, "external-secrets.io/v1");
    assert.equal(resource.metadata.name, name);
    assert.equal(resource.metadata.namespace, "business-site");
  }
  assert.deepEqual(store.spec, {
    provider: {
      vault: {
        server: "http://openbao-active.openbao.svc.cluster.local:8200",
        path: "secret",
        version: "v2",
        auth: { kubernetes: { mountPath: "kubernetes", role: "business-site", serviceAccountRef: { name: "business-site" } } },
      },
    },
  });
  // Each Secret reads named properties of one exact entry through the namespaced store.
  const synchronized = (target, key, properties) => ({
    refreshInterval: "1h",
    secretStoreRef: { name: "openbao", kind: "SecretStore" },
    target: { name: target, creationPolicy: "Owner" },
    data: properties.map(([secretKey, property]) => ({ secretKey, remoteRef: { key, property } })),
  });
  assert.deepEqual(disclosure.spec, synchronized("business-site-disclosure", "apps/business-site/public-disclosure", [["company-disclosure", "company-disclosure"]]));
  assert.deepEqual(numina.spec, synchronized("business-site-numina", "apps/business-site/numina", [["api-key", "api-key"]]));
  assert.deepEqual(stripe.spec, synchronized("business-site-stripe", "apps/business-site/stripe", [["api-key", "api-key"], ["webhook-secret", "webhook-secret"]]));
  // Each public name attaches to its own listener; nothing else may claim a host.
  const route = (name, listeners, hostnames) => ({
    apiVersion: "gateway.networking.k8s.io/v1",
    kind: "HTTPRoute",
    metadata: { name, namespace: "business-site" },
    spec: {
      parentRefs: listeners.map((sectionName) => ({ name: "platform", namespace: "kube-system", sectionName })),
      hostnames,
      rules: [{ backendRefs: [{ name: "business-site", port: 80 }] }],
    },
  });
  assert.deepEqual(routes.sort((a, b) => a.metadata.name.localeCompare(b.metadata.name)), [
    route("business-site-portal", ["https-portal-devantler-tech"], ["portal.devantler.tech"]),
    route("business-site-public", ["https-devantler-tech", "https-www-devantler-tech"], ["devantler.tech", "www.devantler.tech"]),
  ]);
  const spec = app.spec;
  keys(spec, ["name", "image", "host", "port", "replicas", "probePath", "cpuRequest", "memoryRequest", "cpuLimit", "memoryLimit", "args", "runAsUser", "runAsGroup", "readOnlyRootFilesystem", "secretProjections"]);
  assert.equal(spec.name, "business-site");
  // Only the independently verified baseline is admitted before publisher receipt adoption.
  assert.equal(spec.image, "ghcr.io/devantler-tech/business-site@sha256:6f64cba41b8d545c927db23cff5f4fc82dfefca3cfa350ad1ce40757339d708e");
  assert.equal(spec.host, "business-site.platform.devantler.tech");
  assert.equal(spec.port, 8080);
  assert.equal(spec.probePath, "/healthz");
  assert.ok(Number.isInteger(spec.replicas) && spec.replicas >= 2 && spec.replicas <= 3);
  // Deliberately narrow resource policy; capacity changes require reviewed evidence.
  const milliCPU = (v) => {
    assert.ok(typeof v === "string" && /^[1-9][0-9]*m$/.test(v));
    return Number(v.slice(0, -1));
  };
  const mebibytes = (v) => {
    assert.ok(typeof v === "string" && /^[1-9][0-9]*Mi$/.test(v));
    return Number(v.slice(0, -2));
  };
  assert.ok(milliCPU(spec.cpuRequest) >= 10 && milliCPU(spec.cpuRequest) <= milliCPU(spec.cpuLimit));
  assert.ok(milliCPU(spec.cpuLimit) <= 1000);
  assert.ok(mebibytes(spec.memoryRequest) >= 64 && mebibytes(spec.memoryRequest) <= mebibytes(spec.memoryLimit));
  assert.ok(mebibytes(spec.memoryLimit) <= 256);
  assert.equal(spec.runAsUser, 65532);
  assert.equal(spec.runAsGroup, 65532);
  assert.equal(spec.readOnlyRootFilesystem, true);
  assert.deepEqual(spec.args, ["--public", "/app/public", "--listen", "0.0.0.0:8080", "--company-disclosure-projection", "/run/webapp-inputs/disclosure", "--public-host", "devantler.tech", "--portal-host", "portal.devantler.tech", "--portal-settings-projection", "/run/webapp-inputs/portal-settings"]);
  // The platform composes this one Secret. Its settings item keeps sign-in off
  // until it holds activation keys; no migration or other private input is admitted.
  assert.deepEqual(spec.secretProjections, [{
    secretName: "business-site-runtime",
    items: [
      { key: "company-disclosure", path: "disclosure" },
      { key: "portal-settings", path: "portal-settings" },
      { key: "portal-client-secret", path: "portal-client-secret" },
      { key: "portal-database-ca", path: "portal-database-ca" },
      { key: "smtp-username", path: "smtp-username" },
      { key: "smtp-password", path: "smtp-password" },
    ],
  }]);
  // The one migration Job runs this release's own image with only migration
  // arguments. It reads the platform's migration Secret and nothing else, so the
  // package names that Secret but carries no migration input.
  assert.deepEqual(docs.find((d) => d?.kind === "Job"), {
    apiVersion: "batch/v1",
    kind: "Job",
    metadata: {
      name: "business-site-portal-migration",
      namespace: "business-site",
      annotations: { "kustomize.toolkit.fluxcd.io/force": "enabled" },
    },
    spec: {
      backoffLimit: 2,
      activeDeadlineSeconds: 240,
      template: {
        metadata: { labels: { "app.kubernetes.io/name": "business-site-portal-migration" } },
        spec: {
          restartPolicy: "Never",
          automountServiceAccountToken: false,
          enableServiceLinks: false,
          securityContext: { runAsNonRoot: true, runAsUser: 65532, runAsGroup: 65532, fsGroup: 65532, seccompProfile: { type: "RuntimeDefault" } },
          containers: [{
            name: "migrate",
            image: spec.image,
            args: ["--migrate", "--migration-database-projection", "/run/portal-migration/database-url", "--runtime-role", "portal_web"],
            resources: { requests: { cpu: "10m", memory: "32Mi" }, limits: { cpu: "500m", memory: "128Mi" } },
            securityContext: { allowPrivilegeEscalation: false, readOnlyRootFilesystem: true, capabilities: { drop: ["ALL"] } },
            volumeMounts: [
              { name: "migration", mountPath: "/run/portal-migration", readOnly: true },
              { name: "tmp", mountPath: "/tmp" },
            ],
          }],
          volumes: [
            {
              name: "migration",
              secret: {
                secretName: "business-site-portal-db-migration",
                defaultMode: 0o400,
                items: [{ key: "database-url", path: "database-url" }, { key: "ca.crt", path: "ca.crt" }],
              },
            },
            { name: "tmp", emptyDir: { medium: "Memory", sizeLimit: "1Mi" } },
          ],
        },
      },
    },
  });
  assert.deepEqual(network.spec, {
    endpointSelector: { matchLabels: { "app.kubernetes.io/name": "business-site" } },
    ingress: [{ fromEntities: ["ingress"], toPorts: [{ ports: [{ port: "8080", protocol: "TCP" }] }] }],
    // Exact HTTPS names for the two billing APIs; nothing else leaves the pod.
    egress: [{
      toFQDNs: [{ matchName: "api.numina.app" }, { matchName: "api.stripe.com" }],
      toPorts: [{ ports: [{ port: "443", protocol: "TCP" }] }],
    }],
  });
  return spec;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    const chunks = [];
    let size = 0;
    for await (const chunk of process.stdin) {
      size += chunk.length;
      assert.ok(size <= 262144);
      chunks.push(chunk);
    }
    validateTenantPackage(Buffer.concat(chunks).toString("utf8"));
    process.stdout.write("tenant package: public-only package accepted\n");
  } catch {
    // Never echo rejected input: it could contain a mistakenly supplied Secret.
    process.stderr.write("tenant package: rejected input; review the public-only package contract\n");
    process.exitCode = 1;
  }
}
