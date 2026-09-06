# @korifi/deploy-lib

Reusable Pulumi components and pure helpers for installing Korifi.

## Layout

| Module | Role |
| --- | --- |
| `values.ts` | Pure `buildKorifiValues` + registry prefix helpers (unit-tested) |
| `versions.ts` | Pinned Korifi / installer / chart versions |
| `namespaces.ts` | `cf` / `korifi` / `korifi-gateway` (+ optional installer ns) |
| `dependencies.ts` | Job running release-tested `install-dependencies.sh` |
| `local-registry.ts` | kind in-cluster registry + pull-secret helper |
| `korifi-release.ts` | Helm `Release` wrapper |
| `kind-images.ts` | Build Korifi images from the checkout and `kind load` them |
| `k0s-images.ts` | Build Korifi images from the checkout and `k0s ctr images import` them |
| `contour-gateway.ts` | GatewayClass (+ NodePort params on kind/k0s) |
| `knative.ts` | Knative Operator Helm + `KnativeServing` CR (Kourier ClusterIP) |
| `kind-osb-broker-image.ts` | Build the in-tree `osb-service` image and `kind load` it |
| `k0s-osb-broker-image.ts` | Build the in-tree `osb-service` image and import it into k0s |
| `node-placement.ts` | k0s node role labels, taints, and vcluster scheduling values |
| `ecr-kpack-irsa.ts` | Annotate kpack controller SA for ECR |
| `everest-vcluster.ts` | OpenEverest + OpenSearch operator in a vcluster (OSB creates clusters) |
| `aigateway-vcluster.ts` | Envoy AI Gateway + external OpenAI-compatible backend (OSB issues tenant API keys) |
| `service-broker-services.ts` | Everest/AI Gateway kubeconfig/namespace facts the OSB broker consumes |
| `osb-service-broker.ts` | Deploy `osb-service` over HTTPS and register `CFServiceBroker` |
| `uaa-certs.ts` | TLS CA + server PEMs for kind OIDC mount and UAA proxy |
| `uaa-vcluster.ts` | vcluster + UAA + TLS NodePort proxy (kind UAA) |
| `custom-broker-service.example.ts` | Copy-paste template for adding a custom broker backend |

Stacks under `deploy/{kind,k0s,eks,gke}` compose these components; they do not
duplicate Helm value logic.

## Test

```sh
bun install
bun test
```
