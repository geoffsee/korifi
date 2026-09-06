import { expect, test } from "bun:test";
import * as fs from "node:fs";
import * as path from "node:path";

const text = fs.readFileSync(
	path.join(import.meta.dir, "everest-vcluster.ts"),
	"utf8",
);

test("Everest vcluster installs OpenEverest and required monitoring CRDs", () => {
	expect(text).toContain('chart: "vcluster"');
	expect(text).toContain('chart: "victoria-metrics-operator-crds"');
	expect(text).toContain("skipCrds: true");
	expect(text).toContain('chart: "openeverest"');
	expect(text).toContain("dependsOn: [systemNs, monitoringCrds]");
	expect(text).toContain("kindEverestVclusterLocalApiPort");
	expect(text).toContain("command.local.runOutput");
	expect(text).toContain("apiForwardReady.stdout");
	expect(text).toContain("postgresql");
	expect(text).toContain("inClusterKubeconfig");
	expect(text).toContain("everest-vcluster");
	expect(text).not.toContain("${this.namespace}.svc");
	expect(text).not.toContain("kind: \"DatabaseCluster\"");
	expect(text).not.toContain("kind: DatabaseCluster");
});
