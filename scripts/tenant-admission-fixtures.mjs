// Emit admission requests for the production overlay without changing its allowed inputs.
import { readFileSync } from "node:fs";
import * as yaml from "js-yaml";

const namespace = "business-site";
const actor = `system:serviceaccount:${namespace}:${namespace}`;
const podSecurity = "validate-pod-security";
const secretStores = "restrict-tenant-secret-stores";

function refusePrivilegedContainer(pod, kind) {
  const container = pod?.containers?.[0];
  if (!container) throw new Error(`expected a container in the ${kind} pod template`);
  container.securityContext = { ...container.securityContext, privileged: true };
}

const workloads = [
  {
    kind: "WebApp",
    version: "kro.run/v1alpha1",
    name: namespace,
    policy: "restrict-tenant-webapps",
    deny: (object) => { object.spec.name = "fictional-other-tenant"; },
  },
  {
    kind: "Job",
    version: "batch/v1",
    name: "business-site-portal-migration",
    policy: podSecurity,
    deny: (object) => refusePrivilegedContainer(object.spec?.template?.spec, object.kind),
  },
  {
    kind: "CronJob",
    version: "batch/v1",
    name: "business-site-portal-retention",
    policy: podSecurity,
    deny: (object) => refusePrivilegedContainer(object.spec?.jobTemplate?.spec?.template?.spec, object.kind),
  },
];

const directResources = [
  {
    kind: "HTTPRoute",
    version: "gateway.networking.k8s.io/v1",
    names: ["business-site-public", "business-site-portal"],
    policies: [{
      policy: "restrict-tenant-route-hostnames",
      deny: (object) => { object.spec.hostnames = ["fictional-other.example.invalid"]; },
    }, {
      policy: "restrict-webapp-child-ownership",
      deny: (object) => { object.metadata.labels = { ...object.metadata.labels, "kro.run/instance-kind": "WebApp" }; },
    }],
  },
  {
    kind: "ExternalSecret",
    version: "external-secrets.io/v1",
    names: ["business-site-disclosure", "business-site-numina", "business-site-stripe"],
    policies: [{
      policy: secretStores,
      deny: (object) => { object.spec.secretStoreRef.kind = "ClusterSecretStore"; },
    }],
  },
  {
    kind: "SecretStore",
    version: "external-secrets.io/v1",
    names: ["openbao"],
    policies: [{
      policy: secretStores,
      deny: (object) => { object.spec.provider.vault.auth.kubernetes.serviceAccountRef.namespace = "fictional-other-tenant"; },
    }],
  },
  {
    kind: "CiliumNetworkPolicy",
    version: "cilium.io/v2",
    names: [namespace],
    policies: [{
      policy: "restrict-tenant-network-policies",
      deny: (object) => {
        object.spec.ingress[0].fromEndpoints = [{ matchLabels: { "k8s:io.kubernetes.pod.namespace": "fictional-other-tenant" } }];
      },
    }],
  },
];

function renderedObjects(documents, { kind, version }, names) {
  const matches = documents.filter((doc) => doc?.kind === kind);
  const byName = new Map(matches.map((object) => [object.metadata?.name, object]));
  if (matches.length !== names.length || byName.size !== names.length || !names.every((name) => byName.has(name))) {
    throw new Error(`missing or duplicate rendered ${kind} identity`);
  }
  if (matches.some((object) => object.apiVersion !== version || object.metadata.namespace !== namespace)) {
    throw new Error(`unexpected identity for the rendered ${kind}`);
  }
  return names.map((name) => byName.get(name));
}

function tenantNamespaceFrom(filename) {
  const graph = yaml.load(readFileSync(filename, "utf8"));
  const matches = graph?.spec?.resources?.filter((resource) => resource.id === "tenantNamespace");
  const template = matches?.[0]?.template;
  const labels = template?.metadata?.labels;
  if (graph?.kind !== "ResourceGraphDefinition" || graph.spec?.schema?.kind !== "Tenant" || matches?.length !== 1 ||
      template?.apiVersion !== "v1" || template.kind !== "Namespace" ||
      !["ksail", "kro"].includes(labels?.["app.kubernetes.io/managed-by"])) {
    throw new Error("invalid tenant namespace template");
  }
  return { apiVersion: "v1", kind: "Namespace", metadata: { name: namespace, labels } };
}

function controls(object, label, { policy, deny }, context = {}) {
  const negative = structuredClone(object);
  deny(negative);
  const oldObject = structuredClone(object);
  oldObject.metadata.annotations = { ...oldObject.metadata.annotations, "fixture.example/previous": "true" };
  return ["CREATE", "UPDATE"].flatMap((operation) =>
    [["pass", object], ["fail", negative]].map(([expected, target]) => ({
      label: `${label}-${operation}-${expected}`,
      policy,
      actor,
      operation,
      expected,
      object: target,
      ...(operation === "UPDATE" ? { oldObject } : {}),
      ...context,
    })),
  );
}

try {
  const documents = yaml.loadAll(readFileSync(0, "utf8"));
  const tenantNamespace = tenantNamespaceFrom(process.argv[2]);
  const tenantContext = { namespaces: [tenantNamespace], lookups: [tenantNamespace] };
  const fixtures = [
    ...workloads.flatMap((scope) => {
      const [object] = renderedObjects(documents, scope, [scope.name]);
      return controls(object, scope.kind, scope);
    }),
    ...directResources.flatMap((scope) =>
      renderedObjects(documents, scope, scope.names).flatMap((object) =>
        scope.policies.flatMap((check) =>
          controls(object, `${check.policy}-${object.metadata.name}`, check, tenantContext),
        ),
      ),
    ),
  ];
  process.stdout.write(fixtures.map((fixture) => JSON.stringify(fixture)).join("\n") + "\n");
} catch (error) {
  console.error(`admission: ${error.message}`);
  process.exitCode = 1;
}
