# 19. Public-only tenant package

Date: 2026-10-22

## Decision

The application owns a latent Kustomize package using Platform's namespace-bound
WebApp interface at `9e71ba39f3d3824329fdf992c41ab651a34ecfe9`:
`k8s/bases/infrastructure/resource-graph-definitions/webapp/resource-graph-definition.yaml`.
Its generated workload is a Deployment, Service and HTTPRoute. The platform owns
the namespace, restricted execution context, fixed read-only projection and route
admission. The application must not copy a legacy Deployment to evade that contract.

The package runs only the already-published production public profile. It pins
the independently verified image from source `ee69fc5ad5beb284960178d8a675afbb4e7d1389`
and main CI 39749372462 attempt 1. The future accepted publisher may replace that
image only after verifying the original source CI and complete signed receipt.
An immutable image spelling alone is not signature or provenance verification.

The executable consumes one selected company-disclosure file through its existing
non-root group-readable projection mode. No value is committed. No database,
broker, migration job or portal activation input belongs to this public-only
package. The deployment declaration does not create or populate its Secret.

The `business-site.example.invalid` hostname is intentionally non-routable. It is
not a production hostname approval. Current Platform hostname admission denies
unregistered tenant routes; registration must approve the actual hostname in a
separate reviewed change. Pages remains authoritative. No release workflow or
platform consumer applies this directory, so adding it activates nothing.

Two replicas and explicit resource bounds provide a starting configuration, not
production capacity, availability or recovery evidence. KRO uses soft topology
spreading and creates no disruption budget. The application therefore owns a
separate namespaced `maxUnavailable: 1` budget selecting the generated Pod label.
Current Platform `ba9d698cc5e791eb49f8b87d60de62f0a40bdba4` explicitly grants tenant
PDB management and retains this house pattern in its drain-safety policy.
Live acceptance must measure these properties rather than infer them from source.

## Validation boundary

The application policy validates the actual Kustomize output: a closed resource
set, immutable same-repository image, inert hostname, bounded resources, correct
runtime arguments/probe/identity, single selected input and ingress-only network
policy. Mutation controls exercise that real rendering/validation path. Actual
restricted-image controls consume those rendered arguments with fictional input.

This is source-contract and Docker runtime proof, not a generated Kubernetes API,
controller, kubelet, ingress, database or identity-provider trial. Platform's
`scripts/tests/test-webapp-compiler.sh` owns generated API/compiler evidence;
real tenant acceptance remains separate. Issue #102 retains the default-off
semantic-version publisher caller, signed registry artifact and actual consumer
proof. The accepted shared publisher is `publish-app.yaml` at
`467f07582436a26b62cf4aa756423512f85ba0c0`, not a floating catalogue revision.

## Alternatives

An image alone lacks the application declaration. A copied generic Deployment
duplicates the platform's contract. Activating private identity or migrating a
database during package preparation conflates separate release and permission
decisions. These alternatives are not used.
