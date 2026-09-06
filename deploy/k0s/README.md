# deploy/k0s — Korifi on a 3-node k0s cluster in Docker

One `pulumi up` creates a **k0s** cluster (not kind) with three Docker
nodes and then applies the same platform layers as [`../kind`](../kind):
in-cluster registry, Korifi dependencies, Knative Serving, UAA in a
vcluster, and the OSB marketplace.

| Node | Docker name | Role |
| --- | --- | --- |
| Korifi | `{cluster}-korifi` | k0s controller + worker. Tainted. Korifi API/controllers, UAA, local registry, cert-manager, Contour, kpack controller. |
| OSB | `{cluster}-osb` | Worker. Tainted. `osb-service`, Everest vcluster, AI Gateway vcluster. |
| Knative | `{cluster}-knative` | Worker. Untainted. Knative Serving, kpack builds, CF app revisions. |

Host ports published on the korifi controller container match kind:
`80/443` (Contour NodePorts), `30050` (registry), `30443` (UAA), plus
`6443` for the Kubernetes API.

**Do not run this stack and `deploy/kind` at the same time** — they share
those host ports.

Korifi **controllers**, **api**, and **migration** images are built from
this checkout and imported with `k0s ctr images import`. Helm is pinned
to those tags.

## Quick start

```sh
cd deploy/lib && bun install
cd ../k0s && bun install
export PULUMI_CONFIG_PASSPHRASE=<stack passphrase>
pulumi stack init dev   # once
pulumi up --stack dev
```

Prerequisites: Docker, `pulumi`, `kubectl`, `cf` CLI v8+ (no kind binary).
First `pulumi up` compiles the Korifi Go images (a few minutes) and
prefetches external images into every k0s node.

The default AI Gateway backend requires an OpenAI key. Load it before
`pulumi up`:

```sh
set -a
. ../../.env.secrets
set +a
printf '%s' "$OPENAI_API_KEY" \
  | pulumi config set --secret aiGatewayBackendApiKey-openai --stack dev
```

Afterwards:

```sh
pulumi stack output
cf api https://localhost --skip-ssl-validation
cf login -u "$(pulumi stack output uaaAdminEmail)" \
  -p "$(pulumi stack output uaaAdminPassword --show-secrets)"
cf enable-service-access postgres
cf enable-service-access mysql
cf enable-service-access mongodb
cf enable-service-access ozone
cf enable-service-access nats
cf enable-service-access opensearch
cf enable-service-access redis
cf enable-service-access aigateway
cf marketplace
```

UAA is published at `https://127.0.0.1:30443/uaa` (NodePort). The k0s
kube-apiserver is configured with that issuer for OIDC.

## Configuration

| Key                             | Default                            | Meaning                                               |
| ------------------------------- | ---------------------------------- | ----------------------------------------------------- |
| `clusterName`                   | `korifi`                           | Prefix for Docker containers / k0s nodes              |
| `appDomain`                     | `apps-127-0-0-1.nip.io`            | CF apps wildcard domain                               |
| `apiUrl`                        | `localhost`                        | Korifi API host                                       |
| `adminEmail`                    | `admin@korifi.local`               | UAA admin / `cf login` user                           |
| `oidcPrefix`                    | `uaa`                              | OIDC username prefix                                  |
| `registryUser`                  | `user`                             | In-cluster registry username                          |
| `aiGatewayBackends`             | OpenAI with `gpt-5.6-luna`         | Backend origins, authentication types, and model maps |
| `aiGatewayBackendApiKey-<name>` | none                               | Encrypted API key for one authenticated backend       |
| `kubeconfigPath`                | `~/.kube/k0s-<clusterName>.config` | Written by the stack                                  |
| `korifiVersion`                 | pinned in `../lib/versions.ts`     | Helm chart release                                    |
| `installerImage`                | pinned digest                      | Dependencies Job image                                |
| `k0sImage`                      | pinned in `../lib/versions.ts`     | `k0sproject/k0s` OCI image                            |

## Teardown

```sh
pulumi destroy --stack dev
```
