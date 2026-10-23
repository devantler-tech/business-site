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
  assert.equal(docs.length, 5, "resource count");
  const app = docs.find((d) => d?.kind === "WebApp");
  const network = docs.find((d) => d?.kind === "CiliumNetworkPolicy");
  const budget = docs.find((d) => d?.kind === "PodDisruptionBudget");
  const store = docs.find((d) => d?.kind === "SecretStore");
  const disclosure = docs.find((d) => d?.kind === "ExternalSecret");
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
  for (const [resource, name] of [[store, "openbao"], [disclosure, "business-site-disclosure"]]) {
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
  assert.deepEqual(disclosure.spec, {
    refreshInterval: "1h",
    secretStoreRef: { name: "openbao", kind: "SecretStore" },
    target: { name: "business-site-disclosure", creationPolicy: "Owner" },
    data: [{ secretKey: "company-disclosure", remoteRef: { key: "apps/business-site/public-disclosure", property: "company-disclosure" } }],
  });
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
  assert.deepEqual(spec.args, ["--public", "/app/public", "--listen", "0.0.0.0:8080", "--company-disclosure-projection", "/run/webapp-inputs/disclosure"]);
  assert.deepEqual(spec.secretProjections, [{ secretName: "business-site-disclosure", items: [{ key: "company-disclosure", path: "disclosure" }] }]);
  assert.deepEqual(network.spec, {
    endpointSelector: { matchLabels: { "app.kubernetes.io/name": "business-site" } },
    ingress: [{ fromEntities: ["ingress"], toPorts: [{ ports: [{ port: "8080", protocol: "TCP" }] }] }],
    egress: [{}],
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
