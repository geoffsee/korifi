import { expect, test } from "bun:test";
import * as fs from "node:fs";
import * as path from "node:path";

const dir = import.meta.dir;
const sources = fs
	.readdirSync(dir)
	.filter((name) => name.endsWith(".ts") && !name.endsWith(".test.ts"))
	.map((name) => ({
		name,
		text: fs.readFileSync(path.join(dir, name), "utf8"),
	}));
const all = sources.map((file) => file.text).join("\n");

test("k0s stack uses k0s, not kind", () => {
	expect(all).toContain("--enable-worker");
	expect(all).toContain("k0s token create");
	expect(all).toContain("K0sCluster");
	expect(all).toContain("k0sImage");
	expect(all).toContain("new docker.Container");
	expect(all).toContain("new docker.Volume");
	expect(all).not.toContain("docker run");
	expect(all).not.toContain("kind create cluster");
	expect(all).not.toContain("kind load docker-image");
});

test("k0s stack is a 3-node docker cluster with role split", () => {
	expect(all).toContain("-korifi");
	expect(all).toContain("-osb");
	expect(all).toContain("-knative");
	expect(all).toContain("nodeRoleKey");
	expect(all).toContain('placementFor("korifi")');
	expect(all).toContain('placementFor("osb")');
	expect(all).toContain("NoSchedule");
	expect(all).toContain("prefetch-images.sh");
	expect(all).toContain("image-archives");
	expect(all).toContain("KORIFI_IMAGE_ARCHIVES");
});

test("k0s stack composes shared lib components", () => {
	expect(all).toContain("KorifiDependencies");
	expect(all).toContain("KorifiRelease");
	expect(all).toContain("LocalRegistry");
	expect(all).toContain("ContourGateway");
	expect(all).toContain("ServiceBrokerServices");
	expect(all).toContain("K0sEverestOperatorBundles");
	expect(all).toContain("everestOperatorBundles.loaded");
	expect(all).toContain("UaaCerts");
	expect(all).toContain("UaaVcluster");
	expect(all).toContain("KnativeServing");
	expect(all).toContain("K0sOsbBrokerImage");
	expect(all).toContain("OsbServiceBroker");
	expect(all).toContain("osbServicePath");
	expect(all).toContain("osb-service");
	expect(all).toContain("K0sLocalPathStorage");
	expect(all).toContain("K0sPlatformPlacement");
	expect(all).toContain("everest: brokerServices.everest");
	expect(all).toContain("aigateway: brokerServices.aigateway");
	expect(all).toContain("postgres dedicated");
	expect(all).toContain("enable-service-access mysql");
	expect(all).toContain("enable-service-access mongodb");
	expect(all).toContain("enable-service-access ozone");
	expect(all).toContain("enable-service-access nats");
	expect(all).toContain("enable-service-access opensearch");
	expect(all).toContain("enable-service-access redis");
	expect(all).toContain("enable-service-access aigateway");
	expect(all).not.toContain("PostgresServiceBroker");
	expect(all).not.toContain("--insecure");
	expect(all).not.toContain('sslMode: "disable"');
	expect(all).toContain('platform: "k0s"');
	expect(all).toContain("insecureTlsMetricsServer: true");
	expect(all).toContain("NodePortService");
	expect(all).toContain("uaaUrl");
	expect(all).toContain("oidc");
});

test("k0s cluster maps the same host ports as kind plus the API", () => {
	expect(all).toContain("internal: 6443");
	expect(all).toContain("external: 6443");
	expect(all).toContain("internal: 32080");
	expect(all).toContain("external: 80");
	expect(all).toContain("internal: 32443");
	expect(all).toContain("external: 443");
	expect(all).toContain("kindRegistry.nodePort");
	expect(all).toContain("internal: 30443");
	expect(all).toContain("external: 30443");
	expect(all).toContain("containerd.d");
	expect(all).toContain("io.containerd.cri.v1.images");
});

test("knative-runner is the default run reconciler", () => {
	expect(all).toContain("knative-runner");
	expect(all).toContain("k0sRegistryPrefix");
	expect(all).toContain("KnativeServing");
	expect(all).toContain("domain: appDomain");
	expect(all).toContain("localChart");
	expect(all).toContain("K0sKorifiImages");
	expect(all).toContain("images.controllersImage");
	expect(all).not.toContain("cloudfoundry/korifi-controllers:latest");
});

test("auth flow uses cf login against UAA", () => {
	expect(all).toContain("cf login");
	expect(all).toContain("adminEmail");
	expect(all).not.toContain("cf auth kubernetes-admin");
});

test("AI Gateway backend routes and credentials use separate config", () => {
	expect(all).toContain('getObject<AIGatewayBackendConfig[]>("aiGatewayBackends")');
	expect(all).toContain('models: ["gpt-5.6-luna"]');
	expect(all).toContain("`aiGatewayBackendApiKey-${backend.name}`");
	expect(all).toContain("cfg.getSecret(secretKey)");
	expect(all).not.toContain('cfg.get("aiGatewayApiKey")');
	expect(all).not.toContain('cfg.get("aiGatewayBackendUrl")');
});
