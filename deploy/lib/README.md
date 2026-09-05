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
| `contour-gateway.ts` | GatewayClass (+ NodePort params on kind) |
| `knative.ts` | Knative Operator Helm + `KnativeServing` CR (Kourier ClusterIP) |
| `kind-images.ts` | Build Korifi images from the checkout and `kind load` them |
| `kind-osb-broker-image.ts` | Build the in-tree `osb-service` image and `kind load` it |
| `ecr-kpack-irsa.ts` | Annotate kpack controller SA for ECR |
| `service-broker-services.ts` | Shared Postgres (etc.) for OSB brokers — enable flags per service |
| `osb-service-broker.ts` | Deploy `osb-service` over HTTPS and register `CFServiceBroker` |
| `custom-broker-service.example.ts` | Copy-paste template for adding a custom broker backend |

Stacks under `deploy/{kind,eks,gke}` compose these components; they do not
duplicate Helm value logic.

## Test

```sh
bun install
bun test
```
