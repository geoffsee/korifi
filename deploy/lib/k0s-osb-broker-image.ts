/**
 * Build the in-tree `osb-service` broker image and import it into k0s nodes.
 */
import * as fs from "node:fs";
import * as command from "@pulumi/command";
import * as pulumi from "@pulumi/pulumi";
import { hashOsbBrokerImageSources } from "./image-source-hash";

export interface K0sOsbBrokerImageArgs {
	/** Docker container names of k0s nodes that should receive the image. */
	nodeContainers: string[];
	/** Directory that contains `image/Dockerfile` (`osb-service` by default). */
	sourcePath: string;
	dependsOn?: pulumi.Input<pulumi.Resource>[];
}

export class K0sOsbBrokerImage extends pulumi.ComponentResource {
	readonly fingerprint: string;
	readonly tag: string;
	readonly image: string;
	readonly loaded: command.local.Command;

	constructor(
		name: string,
		args: K0sOsbBrokerImageArgs,
		opts?: pulumi.ComponentResourceOptions,
	) {
		super("korifi:deploy:K0sOsbBrokerImage", name, {}, opts);

		if (args.nodeContainers.length === 0) {
			throw new Error("K0sOsbBrokerImage requires at least one node container");
		}
		if (!fs.existsSync(args.sourcePath)) {
			throw new Error(
				`osb-service sources not found at ${args.sourcePath}`,
			);
		}
		const dockerfile = `${args.sourcePath}/image/Dockerfile`;
		if (!fs.existsSync(dockerfile)) {
			throw new Error(`osb-service Dockerfile missing: ${dockerfile}`);
		}

		this.fingerprint = hashOsbBrokerImageSources(args.sourcePath);
		this.tag = `k0s-${this.fingerprint.slice(0, 12)}`;
		this.image = `osb-service:${this.tag}`;

		const script = [
			`set -euo pipefail`,
			`cd "$OSB_ROOT"`,
			`docker build -f image/Dockerfile -t "$BROKER_IMAGE" .`,
			`for NODE in $K0S_NODES; do`,
			`  docker save "$BROKER_IMAGE" | docker exec -i "$NODE" k0s ctr images import -`,
			`done`,
			`echo "loaded $BROKER_IMAGE"`,
		].join("\n");

		this.loaded = new command.local.Command(
			`${name}-build-load`,
			{
				create: script,
				update: script,
				triggers: [
					this.fingerprint,
					args.sourcePath,
					...args.nodeContainers,
				],
				environment: {
					DOCKER_BUILDKIT: "1",
					OSB_ROOT: args.sourcePath,
					K0S_NODES: args.nodeContainers.join(" "),
					BROKER_IMAGE: this.image,
				},
				logging: command.local.Logging.Stderr,
			},
			{
				parent: this,
				dependsOn: args.dependsOn,
				customTimeouts: { create: "15m", update: "15m" },
			},
		);

		this.registerOutputs({
			fingerprint: this.fingerprint,
			image: this.image,
		});
	}
}
