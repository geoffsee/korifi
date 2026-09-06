/** Preload external deployment images directly into Kind's container runtime. */
import * as fs from "node:fs";
import * as path from "node:path";
import * as command from "@pulumi/command";
import * as pulumi from "@pulumi/pulumi";

const scriptPath = path.join(__dirname, "prefetch-images.sh");
const coreManifestPath = path.join(__dirname, "prefetch-core-images.txt");
const serviceManifestPath = path.join(
	__dirname,
	"prefetch-service-images.txt",
);

export function readImageManifest(manifestPath: string): string[] {
	return fs
		.readFileSync(manifestPath, "utf8")
		.split("\n")
		.map((line) => line.trim())
		.filter((line) => line !== "" && !line.startsWith("#"));
}

export const kindCoreImages = readImageManifest(coreManifestPath);
export const kindServiceImages = readImageManifest(serviceManifestPath);

export class KindImagePrefetch extends pulumi.ComponentResource {
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
		super("korifi:deploy:KindImagePrefetch", name, {}, opts);

		const scriptFingerprint = fs.readFileSync(scriptPath, "utf8");
		const createCommand = () =>
			'"$PREFETCH_SCRIPT" --cluster "$KIND_CLUSTER" --images "$IMAGE_MANIFEST" --jobs "$PREFETCH_JOBS"';
		const environment = (manifestPath: string) => ({
			KIND_CLUSTER: args.clusterName,
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
					...kindCoreImages,
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
					...kindServiceImages,
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
