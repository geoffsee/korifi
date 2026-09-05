/**
 * Stack configuration for deploy/kind (INSTALL.kind.md).
 */
import * as os from "node:os";
import * as path from "node:path";
import * as pulumi from "@pulumi/pulumi";
import {
	type AIGatewayBackend,
	validateAIGatewayBackends,
	versions,
} from "@korifi/deploy-lib";

const cfg = new pulumi.Config();

export const clusterName = cfg.get("clusterName") ?? "korifi";
export const appDomain = cfg.get("appDomain") ?? "apps-127-0-0-1.nip.io";
export const apiUrl = cfg.get("apiUrl") ?? "localhost";
/** UAA admin email (OIDC user_name / CF login username). */
export const adminEmail = cfg.get("adminEmail") ?? "admin@korifi.local";
/** OIDC username prefix baked into kind apiserver + Korifi adminUserName. */
export const oidcPrefix = cfg.get("oidcPrefix") ?? "uaa";
export const registryUser = cfg.get("registryUser") ?? "user";

type AIGatewayBackendAuthentication = "apiKey" | "none";
type AIGatewayBackendConfig = Omit<AIGatewayBackend, "apiKey"> & {
	/** Upstream authentication; API keys live in separate encrypted config. */
	authentication?: AIGatewayBackendAuthentication;
};

const configuredAIGatewayBackends =
	cfg.getObject<AIGatewayBackendConfig[]>("aiGatewayBackends") ??
	([
		{
			name: "openai",
			url: "https://api.openai.com",
			models: ["gpt-5.6-luna"],
			authentication: "apiKey",
		},
	] satisfies AIGatewayBackendConfig[]);

for (const backend of configuredAIGatewayBackends) {
	if (backend && typeof backend === "object" && "apiKey" in backend) {
		throw new Error(
			`AI Gateway backend ${backend.name ?? "<unnamed>"} must keep apiKey in separate Pulumi secret config`,
		);
	}
}
validateAIGatewayBackends(configuredAIGatewayBackends);

/** OpenAI-compatible backends and exact model-to-backend route associations. */
export const aiGatewayBackends: AIGatewayBackend[] =
	configuredAIGatewayBackends.map((backend) => {
		const authentication = backend.authentication ?? "none";
		if (authentication !== "none" && authentication !== "apiKey") {
			throw new Error(
				`AI Gateway backend ${backend.name} has unsupported authentication: ${authentication}`,
			);
		}
		const secretKey = `aiGatewayBackendApiKey-${backend.name}`;
		const apiKey =
			authentication === "apiKey" ? cfg.getSecret(secretKey) : undefined;
		if (authentication === "apiKey" && !apiKey) {
			throw new Error(
				`${secretKey} must be set with pulumi config set --secret`,
			);
		}
		return {
			name: backend.name,
			url: backend.url,
			models: backend.models,
			apiKey,
		};
	});

export const kubeconfigPath =
	cfg.get("kubeconfigPath") ??
	path.join(os.homedir(), ".kube", `kind-${clusterName}.config`);

export const pinned = {
	korifi: cfg.get("korifiVersion") ?? versions.korifi,
	installerImage: cfg.get("installerImage") ?? versions.korifiInstallerImage,
} as const;
