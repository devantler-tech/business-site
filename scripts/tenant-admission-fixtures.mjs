// Emit admission requests for the production overlay without changing its allowed inputs.
import { readFileSync } from "node:fs";
import * as yaml from "js-yaml";

const namespace = "business-site";
const actor = `system:serviceaccount:${namespace}:${namespace}`;
const kroActor = "system:serviceaccount:kro-system:kro";
const podSecurity = "validate-pod-security";
const secretStores = "restrict-tenant-secret-stores";
const routeHostnames = "restrict-tenant-route-hostnames";
const childOwnership = "restrict-webapp-child-ownership";

function refusePrivilegedContainer(pod, kind) {
  const container = pod?.containers?.[0];
  if (!container) throw new Error(`expected a container in the ${kind} pod template`);
  container.securityContext = { ...container.securityContext, privileged: true };
}

const workloads = [
  {
    kind: "WebApp",
    version: "kro.run/v1alpha1",
    names: [namespace],
    policy: "restrict-tenant-webapps",
    deny: (object) => { object.spec.name = "fictional-other-tenant"; },
  },
  {
    kind: "Job",
    version: "batch/v1",
    names: ["business-site-portal-migration", "business-site-practice-migration"],
    policy: podSecurity,
    deny: (object) => refusePrivilegedContainer(object.spec?.template?.spec, object.kind),
  },
  {
    kind: "CronJob",
    version: "batch/v1",
    names: ["business-site-portal-retention"],
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
      policy: routeHostnames,
      deny: (object) => { object.spec.hostnames = ["fictional-other.example.invalid"]; },
    }, {
      policy: childOwnership,
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

const generatedKinds = [
  ["deployment", "Deployment", "apps/v1"],
  ["service", "Service", "v1"],
  ["httpRoute", "HTTPRoute", "gateway.networking.k8s.io/v1"],
];
const partOfLabel = "applyset.kubernetes.io/part-of";
const instanceLabel = "kro.run/instance-id";

function generatedObjects(filename) {
  const objects = JSON.parse(readFileSync(filename, "utf8"));
  if (!Array.isArray(objects) || objects.length !== generatedKinds.length) {
    throw new Error("incomplete generated WebApp workload");
  }
  const instanceID = objects[0]?.metadata?.labels?.[instanceLabel];
  const applySet = objects[0]?.metadata?.labels?.[partOfLabel];
  if (typeof instanceID !== "string" || !/^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/.test(instanceID) ||
      typeof applySet !== "string" || !/^applyset-[A-Za-z0-9_-]{43}-v1$/.test(applySet)) {
    throw new Error("invalid generated WebApp controller identity");
  }
  return generatedKinds.map(([node, kind, version]) => {
    const [object] = renderedObjects(objects, { kind, version }, [namespace]);
    const labels = object.metadata.labels;
    const expected = {
      "app.kubernetes.io/managed-by": "kro",
      [instanceLabel]: instanceID,
      [partOfLabel]: applySet,
      "kro.run/instance-kind": "WebApp",
      "kro.run/instance-group": "kro.run",
      "kro.run/instance-version": "v1alpha1",
      "kro.run/instance-name": namespace,
      "kro.run/instance-namespace": namespace,
      "kro.run/node-id": node,
    };
    if (!Object.entries(expected).every(([key, value]) => labels?.[key] === value)) {
      throw new Error(`invalid generated WebApp ownership for ${kind}`);
    }
    return object;
  });
}

function generatedOwnership(object, context) {
  const scope = `generated-ownership-${object.kind}`;
  const wrongNode = structuredClone(object);
  wrongNode.metadata.labels["kro.run/node-id"] = "different-node";
  const sameOwner = structuredClone(object);
  sameOwner.metadata.annotations = { ...sameOwner.metadata.annotations, "fixture.example/previous": "true" };
  const foreignOwner = structuredClone(sameOwner);
  foreignOwner.metadata.labels[partOfLabel] = "applyset-synthetic-foreign-v1";
  const unowned = structuredClone(sameOwner);
  unowned.metadata.labels = Object.fromEntries(Object.entries(unowned.metadata.labels)
    .filter(([key]) => !key.startsWith("kro.run/") && !key.startsWith("applyset.kubernetes.io/")));
  const base = { policy: childOwnership, actor: kroActor, ...context };
  return [
    { ...base, label: `${scope}-CREATE-pass`, operation: "CREATE", expected: "pass", object },
    { ...base, label: `${scope}-CREATE-wrong-node`, operation: "CREATE", expected: "fail", object: wrongNode },
    { ...base, label: `${scope}-CREATE-tenant-actor`, operation: "CREATE", expected: "fail", actor, object },
    { ...base, label: `${scope}-UPDATE-pass`, operation: "UPDATE", expected: "pass", object, oldObject: sameOwner },
    { ...base, label: `${scope}-UPDATE-foreign`, operation: "UPDATE", expected: "fail", object, oldObject: foreignOwner },
    { ...base, label: `${scope}-UPDATE-unowned`, operation: "UPDATE", expected: "fail", object, oldObject: unowned },
  ];
}

try {
  if (process.argv.length !== 4) throw new Error("expected the Tenant graph and generated child inventory");
  const documents = yaml.loadAll(readFileSync(0, "utf8"));
  const tenantNamespace = tenantNamespaceFrom(process.argv[2]);
  const tenantContext = { namespaces: [tenantNamespace], lookups: [tenantNamespace] };
  const children = generatedObjects(process.argv[3]);
  const [deployment, , route] = children;
  const generatedContext = { ...tenantContext, actor: kroActor };
  const fixtures = [
    ...workloads.flatMap((scope) =>
      renderedObjects(documents, scope, scope.names).flatMap((object) =>
        controls(object, `${scope.kind}-${object.metadata.name}`, scope),
      ),
    ),
    ...directResources.flatMap((scope) =>
      renderedObjects(documents, scope, scope.names).flatMap((object) =>
        scope.policies.flatMap((check) =>
          controls(object, `${check.policy}-${object.metadata.name}`, check, tenantContext),
        ),
      ),
    ),
    ...controls(deployment, "generated-pod-security", {
      policy: podSecurity,
      deny: (object) => { object.spec.template.spec.containers[0].securityContext.runAsNonRoot = false; },
    }, generatedContext),
    ...controls(route, "generated-route-hostname", {
      policy: routeHostnames,
      deny: (object) => { object.spec.hostnames = ["fictional-other.example.invalid"]; },
    }, generatedContext),
    ...children.flatMap((object) => generatedOwnership(object, tenantContext)),
  ];
  process.stdout.write(fixtures.map((fixture) => JSON.stringify(fixture)).join("\n") + "\n");
} catch (error) {
  console.error(`admission: ${error.message}`);
  process.exitCode = 1;
}
