import { inspectCloudFoundryBindings } from "./cloudfoundry.ts";

const port = Number.parseInt(process.env.PORT ?? "8080", 10);

Bun.serve({
  port,
  fetch(request) {
    const url = new URL(request.url);

    if (url.pathname !== "/" && url.pathname !== "/healthz") {
      return Response.json({ error: "not found" }, { status: 404 });
    }

    const report = inspectCloudFoundryBindings();
    return Response.json(report, {
      status: report.healthy ? 200 : 503,
      headers: { "cache-control": "no-store" },
    });
  },
});

console.log(`DI Framework Cloud Foundry binding probe listening on ${port}`);
