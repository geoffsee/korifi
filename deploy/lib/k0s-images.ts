/**
 * Build Korifi images from this checkout and import them into k0s nodes
 * via `k0s ctr images import` (no kind load).
 */
import * as command from "@pulumi/command";
import * as pulumi from "@pulumi/pulumi";
import { hashKorifiImageSources } from "./image-source-hash";

export interface K0sKorifiImagesArgs {
	/** Docker container names of k0s nodes that should receive the images. */
	nodeContainers: string[];
	/** Korifi repo root (directory that contains `controllers/Dockerfile`). */
	repoRoot: string;
	dependsOn?: pulumi.Input<pulumi.Resource>[];
}

export class K0sKorifiImages extends pulumi.ComponentResource {
	readonly fingerprint: string;
	readonly tag: string;
	readonly controllersImage: string;
	readonly apiImage: string;
	readonly migrationImage: string;
	readonly loaded: command.local.Command;

	constructor(
		name: string,
		args: K0sKorifiImagesArgs,
		opts?: pulumi.ComponentResourceOptions,
	) {
		super("korifi:deploy:K0sKorifiImages", name, {}, opts);

		if (args.nodeContainers.length === 0) {
			throw new Error("K0sKorifiImages requires at least one node container");
		}

		this.fingerprint = hashKorifiImageSources(args.repoRoot);
		this.tag = `k0s-${this.fingerprint.slice(0, 12)}`;
		this.controllersImage = `korifi-controllers:${this.tag}`;
		this.apiImage = `korifi-api:${this.tag}`;
		this.migrationImage = `korifi-migration:${this.tag}`;

		const script = [
			`set -euo pipefail`,
			`cd "$KORIFI_ROOT"`,
			`docker build -f controllers/Dockerfile -t "$CONTROLLERS_IMAGE" --build-arg "version=$VERSION" .`,
			`docker build -f api/Dockerfile -t "$API_IMAGE" --build-arg "version=$VERSION" .`,
			`docker build -f migration/Dockerfile -t "$MIGRATION_IMAGE" --build-arg "version=$VERSION" .`,
			`for NODE in $K0S_NODES; do`,
			`  docker save "$CONTROLLERS_IMAGE" "$API_IMAGE" "$MIGRATION_IMAGE" | docker exec -i "$NODE" k0s ctr images import -`,
			`done`,
			`echo "loaded $CONTROLLERS_IMAGE $API_IMAGE $MIGRATION_IMAGE"`,
		].join("\n");

		this.loaded = new command.local.Command(
			`${name}-build-load`,
			{
				create: script,
				update: script,
				triggers: [
					this.fingerprint,
					args.repoRoot,
					...args.nodeContainers,
				],
				environment: {
					DOCKER_BUILDKIT: "1",
					KORIFI_ROOT: args.repoRoot,
					K0S_NODES: args.nodeContainers.join(" "),
					VERSION: this.tag,
					CONTROLLERS_IMAGE: this.controllersImage,
					API_IMAGE: this.apiImage,
					MIGRATION_IMAGE: this.migrationImage,
				},
				logging: command.local.Logging.Stderr,
			},
			{
				parent: this,
				dependsOn: args.dependsOn,
				customTimeouts: { create: "25m", update: "25m" },
			},
		);

		this.registerOutputs({
			fingerprint: this.fingerprint,
			controllersImage: this.controllersImage,
			apiImage: this.apiImage,
			migrationImage: this.migrationImage,
		});
	}
}
