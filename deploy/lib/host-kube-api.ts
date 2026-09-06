/**
 * Host-side kube-apiserver probe used to skip Pulumi invokes that would
 * otherwise run `kubectl` during preview of a cluster that does not exist yet.
 */
import { execFileSync } from "node:child_process";
import * as os from "node:os";
import * as command from "@pulumi/command";
import * as pulumi from "@pulumi/pulumi";

export function resolveHostKubeconfigPath(kubeconfig: string): string {
	return kubeconfig.replace(/^\$HOME/, os.homedir());
}

export function hostKubeApiReachable(kubeconfig: string): boolean {
	try {
		execFileSync(
			"kubectl",
			[
				"--kubeconfig",
				resolveHostKubeconfigPath(kubeconfig),
				"--request-timeout=2s",
				"get",
				"--raw=/readyz",
			],
			{ stdio: "ignore", timeout: 4000 },
		);
		return true;
	} catch {
		return false;
	}
}

export function hostServiceExists(
	kubeconfig: string,
	namespace: string,
	service: string,
): boolean {
	try {
		execFileSync(
			"kubectl",
			[
				"--kubeconfig",
				resolveHostKubeconfigPath(kubeconfig),
				"--request-timeout=2s",
				"-n",
				namespace,
				"get",
				"svc",
				service,
			],
			{ stdio: "ignore", timeout: 4000 },
		);
		return true;
	} catch {
		return false;
	}
}

/** Re-establish a vcluster port-forward only when that Service already exists. */
export function refreshVclusterForwardIfReachable(args: {
	hostKubeconfig: string;
	namespace: string;
	service: string;
	script: string;
	parent: pulumi.Resource;
	dependsOn: pulumi.Resource[];
}): { stdout: pulumi.Output<string> } {
	if (!hostServiceExists(args.hostKubeconfig, args.namespace, args.service)) {
		return { stdout: pulumi.output("") };
	}
	return command.local.runOutput(
		{ command: args.script },
		{ parent: args.parent, dependsOn: args.dependsOn },
	);
}
