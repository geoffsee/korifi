import { expect, test } from "bun:test";
import * as fs from "node:fs";
import * as path from "node:path";
import {
	buildAIGatewayRouteRules,
	validateAIGatewayBackends,
} from "./aigateway-vcluster";

const text = fs.readFileSync(
	path.join(import.meta.dir, "aigateway-vcluster.ts"),
	"utf8",
);

test("AI Gateway vcluster routes declared models to external OpenAI-compatible backends", () => {
	expect(text).toContain('chart: "vcluster"');
	expect(text).toContain("kindAIGatewayVclusterLocalApiPort");
	expect(text).toContain("command.local.runOutput");
	expect(text).toContain("apiForwardReady.stdout");
	expect(text).toContain("envoyproxy/gateway-helm");
	expect(text).toContain("envoyproxy/ai-gateway-helm");
	expect(text).toContain("envoyGatewayChart");
	expect(text).toContain("aiGatewayChart");
	expect(text).toContain('kind: "AIGatewayRoute"');
	expect(text).toContain('kind: "AIServiceBackend"');
	expect(text).toContain("buildAIGatewayRouteRules(args.backends)");
	expect(text).toContain('type: "Exact"');
	expect(text).toContain("modelsOwnedBy: backend.name");
	expect(text).toContain('wellKnownCACertificates: "System"');
	expect(text).toContain('url.protocol === "https:"');
	expect(text).toContain('kind: "SecurityPolicy"');
	expect(text).toContain("aigw-clients");
	expect(text).not.toContain("vllm-openai");
	expect(text).not.toContain('name: "vllm"');
	expect(text).not.toContain('kind: "DatabaseCluster"');
});

test("builds one exact Envoy route rule per configured model", () => {
	expect(
		buildAIGatewayRouteRules([
			{
				name: "openai",
				url: "https://api.openai.com",
				models: ["first-model", "second-model"],
			},
		]),
	).toEqual([
		{
			matches: [
				{
					headers: [
						{
							type: "Exact",
							name: "x-ai-eg-model",
							value: "first-model",
						},
					],
				},
			],
			backendRefs: [{ name: "openai" }],
			modelsOwnedBy: "openai",
		},
		{
			matches: [
				{
					headers: [
						{
							type: "Exact",
							name: "x-ai-eg-model",
							value: "second-model",
						},
					],
				},
			],
			backendRefs: [{ name: "openai" }],
			modelsOwnedBy: "openai",
		},
	]);
});

test("accepts multiple backends with distinct model associations", () => {
	expect(() =>
		validateAIGatewayBackends([
			{
				name: "openai",
				url: "https://api.openai.com",
				models: ["gpt-5.6-luna"],
			},
			{
				name: "local-vllm",
				url: "http://host.docker.internal:8000",
				models: ["meta-llama/Llama-3.1-8B-Instruct"],
			},
		]),
	).not.toThrow();
});

test("rejects ambiguous model associations", () => {
	expect(() =>
		validateAIGatewayBackends([
			{
				name: "first",
				url: "https://first.example.com",
				models: ["shared-model"],
			},
			{
				name: "second",
				url: "https://second.example.com",
				models: ["shared-model"],
			},
		]),
	).toThrow("assigned to both first and second");
});

test("rejects duplicate backend names and malformed origins", () => {
	expect(() =>
		validateAIGatewayBackends([
			{ name: "same", url: "https://one.example.com", models: ["one"] },
			{ name: "same", url: "https://two.example.com", models: ["two"] },
		]),
	).toThrow("duplicate AI Gateway backend name: same");

	expect(() =>
		validateAIGatewayBackends([
			{
				name: "openai",
				url: "https://api.openai.com/v1",
				models: ["gpt-5.6-luna"],
			},
		]),
	).toThrow("must be an origin without a path");
});
