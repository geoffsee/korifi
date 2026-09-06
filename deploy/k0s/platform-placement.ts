/** Patch installer-created platform Deployments onto the korifi node. */
import * as command from "@pulumi/command";
import * as pulumi from "@pulumi/pulumi";
import { nodeRoleKey } from "@korifi/deploy-lib";

export class K0sPlatformPlacement extends pulumi.ComponentResource {
	readonly patched: command.local.Command;

	constructor(
		name: string,
		args: {
			kubeconfigPath: string;
			dependsOn?: pulumi.Input<pulumi.Resource>[];
		},
		opts?: pulumi.ComponentResourceOptions,
	) {
		super("korifi:deploy:K0sPlatformPlacement", name, {}, opts);

		const patch = JSON.stringify({
			spec: {
				template: {
					spec: {
						nodeSelector: { [nodeRoleKey]: "korifi" },
						tolerations: [
							{
								key: nodeRoleKey,
								operator: "Equal",
								value: "korifi",
								effect: "NoSchedule",
							},
						],
					},
				},
			},
		});

		const script = [
			"set -euo pipefail",
			'export KUBECONFIG="$KUBECONFIG_PATH"',
			"patch_ns() {",
			"  local ns=$1",
			'  kubectl get deploy -n "$ns" -o name 2>/dev/null | while IFS= read -r deploy; do',
			'    kubectl patch -n "$ns" "$deploy" --type strategic -p "$PLACEMENT_PATCH"',
			"  done",
			"}",
			"for i in $(seq 1 60); do",
			"  kubectl get ns cert-manager >/dev/null 2>&1 && break",
			"  sleep 2",
			"done",
			"patch_ns cert-manager",
			"patch_ns kpack",
			"patch_ns projectcontour",
		].join("\n");

		this.patched = new command.local.Command(
			`${name}-patch`,
			{
				create: script,
				update: script,
				environment: {
					KUBECONFIG_PATH: args.kubeconfigPath,
					PLACEMENT_PATCH: patch,
				},
				logging: command.local.Logging.Stderr,
			},
			{
				parent: this,
				dependsOn: args.dependsOn,
				customTimeouts: { create: "10m", update: "10m" },
			},
		);

		this.registerOutputs();
	}
}
