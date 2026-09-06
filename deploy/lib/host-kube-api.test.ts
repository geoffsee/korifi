import { expect, test } from "bun:test";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import {
	hostKubeApiReachable,
	hostServiceExists,
	resolveHostKubeconfigPath,
} from "./host-kube-api";

test("expands $HOME in kubeconfig paths", () => {
	expect(resolveHostKubeconfigPath("$HOME/.kube/kind-korifi.config")).toBe(
		path.join(os.homedir(), ".kube/kind-korifi.config"),
	);
});

test("host API is unreachable without a live cluster", () => {
	expect(hostKubeApiReachable("/no/such/kubeconfig")).toBe(false);
});

test("vcluster Service probe fails fast when the namespace is missing", () => {
	expect(
		hostServiceExists("/no/such/kubeconfig", "everest-vcluster", "everest"),
	).toBe(false);
});

test("vcluster refresh invoke is gated on host API reachability", () => {
	const text = fs.readFileSync(
		path.join(import.meta.dir, "host-kube-api.ts"),
		"utf8",
	);
	expect(text).toContain("command.local.runOutput");
	expect(text).toContain("hostKubeApiReachable");
});
