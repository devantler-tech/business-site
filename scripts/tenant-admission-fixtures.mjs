// Emit admission requests for the production overlay without changing its allowed inputs.
import { readFileSync } from "node:fs";
import * as yaml from "js-yaml";

const namespace = "business-site";
const actor = `system:serviceaccount:${namespace}:${namespace}`;
const podSecurity = "validate-pod-security";

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

try {
  const documents = yaml.loadAll(readFileSync(0, "utf8"));
  const fixtures = workloads.flatMap(({ kind, version, name, policy, deny }) => {
    const matches = documents.filter((doc) => doc?.kind === kind);
    if (matches.length !== 1) throw new Error(`expected one rendered ${kind}, found ${matches.length}`);
    const [object] = matches;
    if (object.apiVersion !== version || object.metadata?.name !== name || object.metadata?.namespace !== namespace) {
      throw new Error(`unexpected identity for the rendered ${kind}`);
    }
    const negative = structuredClone(object);
    deny(negative);
    const oldObject = structuredClone(object);
    oldObject.metadata.annotations = { ...oldObject.metadata.annotations, "fixture.example/previous": "true" };
    return ["CREATE", "UPDATE"].flatMap((operation) =>
      [["pass", object], ["fail", negative]].map(([expected, target]) => ({
        label: `${kind}-${operation}-${expected}`,
        policy,
        actor,
        operation,
        expected,
        object: target,
        ...(operation === "UPDATE" ? { oldObject } : {}),
      })),
    );
  });
  process.stdout.write(fixtures.map((fixture) => JSON.stringify(fixture)).join("\n") + "\n");
} catch (error) {
  console.error(`admission: ${error.message}`);
  process.exitCode = 1;
}
