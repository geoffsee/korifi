/** Preload external deployment images into k0s node containerd. */
import * as fs from "node:fs";
import * as path from "node:path";
import * as command from "@pulumi/command";
import * as pulumi from "@pulumi/pulumi";
import { k0sNodeContainers } from "./cluster";

const scriptPath = path.join(__dirname, "prefetch-images.sh");
const kindDir = path.join(__dirname, "..", "kind");
const coreManifestPath = path.join(kindDir, "prefetch-core-images.txt");
const serviceManifestPath = path.join(kindDir, "prefetch-service-images.txt");

export function readImageManifest(manifestPath: string): string[] {
	return fs
		.readFileSync(manifestPath, "utf8")
		.split("\n")
		.map((line) => line.trim())
		.filter((line) => line !== "" && !line.startsWith("#"));
}

export const k0sCoreImages = readImageManifest(coreManifestPath);
export const k0sServiceImages = readImageManifest(serviceManifestPath);

export class K0sImagePrefetch extends pulumi.ComponentResource {
	readonly coreLoaded: command.local.Command;
	readonly servicesLoaded: command.local.Command;

	constructor(
		name: string,
		args: {
			clusterName: string;
			parallelism?: number;
			dependsOn?: pulumi.Input<pulumi.Resource>[];
		},
		opts?: pulumi.ComponentResourceOptions,
	) {
		super("korifi:deploy:K0sImagePrefetch", name, {}, opts);

		const nodes = k0sNodeContainers(args.clusterName);
		const scriptFingerprint = fs.readFileSync(scriptPath, "utf8");
		const createCommand = () =>
			'"$PREFETCH_SCRIPT" --cluster "$K0S_CLUSTER" --images "$IMAGE_MANIFEST" --jobs "$PREFETCH_JOBS"';
		const environment = (manifestPath: string) => ({
			K0S_CLUSTER: args.clusterName,
			PREFETCH_JOBS: String(args.parallelism ?? 6),
			PREFETCH_SCRIPT: scriptPath,
			IMAGE_MANIFEST: manifestPath,
		});

		this.coreLoaded = new command.local.Command(
			`${name}-core`,
			{
				create: createCommand(),
				update: createCommand(),
				triggers: [
					args.clusterName,
					scriptFingerprint,
					nodes.korifi,
					nodes.osb,
					nodes.knative,
					...k0sCoreImages,
				],
				environment: environment(coreManifestPath),
				logging: command.local.Logging.Stderr,
			},
			{
				parent: this,
				dependsOn: args.dependsOn,
				customTimeouts: { create: "45m", update: "45m" },
			},
		);

		this.servicesLoaded = new command.local.Command(
			`${name}-services`,
			{
				create: createCommand(),
				update: createCommand(),
				triggers: [
					args.clusterName,
					scriptFingerprint,
					nodes.korifi,
					nodes.osb,
					nodes.knative,
					...k0sServiceImages,
				],
				environment: environment(serviceManifestPath),
				logging: command.local.Logging.Stderr,
			},
			{
				parent: this,
				dependsOn: args.dependsOn,
				customTimeouts: { create: "45m", update: "45m" },
			},
		);

		this.registerOutputs();
	}
}
