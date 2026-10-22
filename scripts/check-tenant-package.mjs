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
  assert.equal(docs.length, 3, "resource count");
  const app = docs.find((d) => d?.kind === "WebApp");
  const network = docs.find((d) => d?.kind === "CiliumNetworkPolicy");
  const budget = docs.find((d) => d?.kind === "PodDisruptionBudget");
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
  const spec = app.spec;
  keys(spec, ["name", "image", "host", "port", "replicas", "probePath", "cpuRequest", "memoryRequest", "cpuLimit", "memoryLimit", "args", "runAsUser", "runAsGroup", "readOnlyRootFilesystem", "secretProjections"]);
  assert.equal(spec.name, "business-site");
  assert.match(spec.image, /^ghcr\.io\/devantler-tech\/business-site@sha256:[0-9a-f]{64}$/);
  assert.equal(spec.host, "business-site.example.invalid");
  assert.equal(spec.port, 8080);
  assert.equal(spec.probePath, "/healthz");
  assert.ok(Number.isInteger(spec.replicas) && spec.replicas >= 1 && spec.replicas <= 3);
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
    egress: [],
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
