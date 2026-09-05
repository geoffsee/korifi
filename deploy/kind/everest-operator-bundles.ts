/** Preload OpenEverest's OLM bundles into kind before its installer hook runs. */
import * as command from "@pulumi/command";
import * as pulumi from "@pulumi/pulumi";

/** Bundle tags published by the OpenEverest 1.16.2 catalog. */
export const everestOperatorBundleImages = [
	"docker.io/percona/percona-postgresql-operator:3.0.0-community-bundle",
	"docker.io/percona/percona-server-mongodb-operator:1.22.0-community-bundle",
	"docker.io/percona/percona-xtradb-cluster-operator:1.20.0-community-bundle",
] as const;

export class KindEverestOperatorBundles extends pulumi.ComponentResource {
	readonly loaded: command.local.Command;

	constructor(
		name: string,
		args: {
			clusterName: string;
			dependsOn?: pulumi.Input<pulumi.Resource>[];
		},
		opts?: pulumi.ComponentResourceOptions,
	) {
		super("korifi:deploy:KindEverestOperatorBundles", name, {}, opts);

		const script = [
			"set -euo pipefail",
			'kind get nodes --name "$KIND_CLUSTER" | while IFS= read -r NODE; do',
			'  printf "%s\\n" "$EVEREST_OPERATOR_BUNDLE_IMAGES" | while IFS= read -r IMAGE; do',
			'    docker exec "$NODE" crictl pull "$IMAGE"',
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
					KIND_CLUSTER: args.clusterName,
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
