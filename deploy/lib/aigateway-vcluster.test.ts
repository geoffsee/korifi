import { expect, test } from "bun:test";
import * as fs from "node:fs";
import * as path from "node:path";

const text = fs.readFileSync(
	path.join(import.meta.dir, "aigateway-vcluster.ts"),
	"utf8",
);

test("AI Gateway vcluster installs Envoy AI Gateway and a vLLM fleet", () => {
	expect(text).toContain('chart: "vcluster"');
	expect(text).toContain("kindAIGatewayVclusterLocalApiPort");
	expect(text).toContain("envoyproxy/gateway-helm");
	expect(text).toContain("envoyproxy/ai-gateway-helm");
	expect(text).toContain("envoyGatewayChart");
	expect(text).toContain("aiGatewayChart");
	expect(text).toContain("vllmImage");
	expect(text).toContain("kind: \"AIGatewayRoute\"");
	expect(text).toContain("kind: \"AIServiceBackend\"");
	expect(text).toContain("kind: \"SecurityPolicy\"");
	expect(text).toContain("aigw-clients");
	expect(text).toContain("opt-125m");
	expect(text).toContain("tiny-gpt2");
	expect(text).not.toContain("kind: \"DatabaseCluster\"");
});
