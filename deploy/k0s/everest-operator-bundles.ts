/** Preload OpenEverest's OLM bundles into k0s before its installer hook runs. */
import * as command from "@pulumi/command";
import * as pulumi from "@pulumi/pulumi";
import { k0sNodeContainers } from "./cluster";

/** Bundle tags published by the OpenEverest 1.16.2 catalog. */
export const everestOperatorBundleImages = [
	"docker.io/percona/percona-postgresql-operator:3.0.0-community-bundle",
	"docker.io/percona/percona-server-mongodb-operator:1.22.0-community-bundle",
	"docker.io/percona/percona-xtradb-cluster-operator:1.20.0-community-bundle",
] as const;

export class K0sEverestOperatorBundles extends pulumi.ComponentResource {
	readonly loaded: command.local.Command;

	constructor(
		name: string,
		args: {
			clusterName: string;
			dependsOn?: pulumi.Input<pulumi.Resource>[];
		},
		opts?: pulumi.ComponentResourceOptions,
	) {
		super("korifi:deploy:K0sEverestOperatorBundles", name, {}, opts);

		const nodes = k0sNodeContainers(args.clusterName);
		const script = [
			"set -euo pipefail",
			'for NODE in $K0S_NODES; do',
			'  printf "%s\\n" "$EVEREST_OPERATOR_BUNDLE_IMAGES" | while IFS= read -r IMAGE; do',
			'    docker exec "$NODE" k0s ctr images pull "$IMAGE"',
			"  done",
			"done",
		].join("\n");

		this.loaded = new command.local.Command(
			`${name}-load`,
			{
				create: script,
				update: script,
				triggers: [args.clusterName, ...everestOperatorBundleImages],
				environment: {
					K0S_NODES: `${nodes.korifi} ${nodes.osb} ${nodes.knative}`,
					EVEREST_OPERATOR_BUNDLE_IMAGES:
						everestOperatorBundleImages.join("\n"),
				},
				logging: command.local.Logging.Stderr,
			},
			{
				parent: this,
				dependsOn: args.dependsOn,
				customTimeouts: { create: "15m", update: "15m" },
			},
		);

		this.registerOutputs();
	}
}
