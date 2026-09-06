# DI Framework Cloud Foundry binding probe

This Bun app validates the Cloud Foundry connector added by
[`di-framework/di-framework@5b0cb7f`](https://github.com/di-framework/di-framework/commit/5b0cb7fc28eb93fda5a0f9a30969463e1f40e196).
The dependency is pinned to that exact Git commit.

The `/healthz` endpoint uses `@CloudFoundryService` and `@VcapApplication`
decorators backed by a DI Framework `Container`. It expects the eight `*-smoke`
service instances provisioned by this stack. Its JSON response contains only
service metadata and credential key names, never credential values.

## Local checks

```sh
bun install --frozen-lockfile
bun test
bun run typecheck
bun run build
```

## Deploy with `cf push`

Compile the app and its pinned dependencies into a self-contained Linux Bun
executable, then push it with the Procfile buildpack:

```sh
bun run package:cf
cf push
```

Bind each provisioned service and restart the app:

```sh
for service in \
  aigateway-smoke mongodb-smoke mysql-smoke nats-smoke \
  opensearch-smoke ozone-smoke postgres-smoke redis-smoke; do
  cf bind-service di-framework-bindings "$service"
done
cf restart di-framework-bindings
```

Then request `https://di-framework-bindings.apps-127-0-0-1.nip.io/healthz`.
A `200` response with `"healthy": true` confirms VCAP discovery, DI container
registration, decorator injection, and the expected connector classifications.
