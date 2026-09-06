/**
 * Three-node k0s cluster in Docker: korifi (controller+worker), osb, knative.
 *
 * Nodes are docker.Container / docker.Volume resources. Join tokens, kubeconfig
 * rewrite, and containerd registry drop-ins stay as local Commands.
 *
 * korifi and osb are NoSchedule-tainted; knative stays schedulable for apps.
 */
import * as crypto from "node:crypto";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import * as command from "@pulumi/command";
import * as docker from "@pulumi/docker";
import * as k8s from "@pulumi/kubernetes";
import * as pulumi from "@pulumi/pulumi";
import { kindRegistry, nodeRoleKey, versions } from "@korifi/deploy-lib";
import { defaultImageArchivesDir, tarPathForImage } from "./image-archives";

export interface K0sOidcArgs {
	issuerUrl: string;
	caDir: string;
	caFileName?: string;
	clientId?: string;
	usernameClaim?: string;
	usernamePrefix?: string;
}

export interface K0sClusterArgs {
	clusterName: string;
	kubeconfigPath?: string;
	k0sImage?: string;
	oidc?: K0sOidcArgs;
}

export function k0sNodeContainers(clusterName: string): {
	korifi: string;
	osb: string;
	knative: string;
} {
	return {
		korifi: `${clusterName}-korifi`,
		osb: `${clusterName}-osb`,
		knative: `${clusterName}-knative`,
	};
}

export class K0sCluster extends pulumi.ComponentResource {
	readonly kubeconfig: pulumi.Output<string>;
	readonly kubeconfigPath: string;
	readonly provider: k8s.Provider;
	readonly clusterName: string;
	readonly nodeContainers: {
		korifi: string;
		osb: string;
		knative: string;
	};

	constructor(
		name: string,
		args: K0sClusterArgs,
		opts?: pulumi.ComponentResourceOptions,
	) {
		super("korifi:deploy:K0sCluster", name, {}, opts);

		this.clusterName = args.clusterName;
		this.nodeContainers = k0sNodeContainers(args.clusterName);
		this.kubeconfigPath =
			args.kubeconfigPath ??
			path.join(os.homedir(), ".kube", `k0s-${args.clusterName}.config`);

		const imageRef = args.k0sImage ?? versions.k0sImage;
		const workDir = path.join(
			os.tmpdir(),
			`korifi-k0s-${args.clusterName}`,
		);
		fs.mkdirSync(workDir, { recursive: true });
		const tokenPath = path.join(workDir, "worker.token");
		const k0sYaml = k0sClusterConfig(args.oidc);
		const configSha = crypto
			.createHash("sha256")
			.update(k0sYaml)
			.digest("hex");

		const korifi = this.nodeContainers.korifi;
		const osb = this.nodeContainers.osb;
		const knative = this.nodeContainers.knative;
		const registryHost = kindRegistry.clusterHost;
		const archivesDir = defaultImageArchivesDir();
		const k0sTar = tarPathForImage(archivesDir, imageRef) ?? "";

		const imageId = new command.local.Command(
			`${name}-k0s-image`,
			{
				create: [
					`set -euo pipefail`,
					`IMAGE='${imageRef}'`,
					`TAR='${k0sTar}'`,
					`if docker image inspect "$IMAGE" >/dev/null 2>&1; then`,
					`  docker image inspect --format '{{.Id}}' "$IMAGE"`,
					`elif [ -n "$TAR" ] && [ -f "$TAR" ]; then`,
					`  docker load -i "$TAR" >/dev/null`,
					`  docker image inspect --format '{{.Id}}' "$IMAGE"`,
					`else`,
					`  echo "k0s image $IMAGE is not in the local Docker cache and no image-archives tarball was found" >&2`,
					`  echo "load it with: docker load -i <archives>/tars/<k0s tar>   (or set KORIFI_IMAGE_ARCHIVES)" >&2`,
					`  exit 1`,
					`fi`,
				].join("\n"),
				triggers: [imageRef, k0sTar],
				logging: command.local.Logging.Stderr,
			},
			{ parent: this },
		);

		const volumes = {
			korifiLib: nodeVolume(name, args.clusterName, "korifi", "lib", this),
			korifiPods: nodeVolume(name, args.clusterName, "korifi", "pods", this),
			osbLib: nodeVolume(name, args.clusterName, "osb", "lib", this),
			osbPods: nodeVolume(name, args.clusterName, "osb", "pods", this),
			knativeLib: nodeVolume(name, args.clusterName, "knative", "lib", this),
			knativePods: nodeVolume(name, args.clusterName, "knative", "pods", this),
		};

		const korifiContainer = new docker.Container(
			`${name}-korifi`,
			{
				...k0sRuntime(),
				name: korifi,
				hostname: korifi,
				image: imageId.stdout.apply((id) => id.trim()),
				command: [
					"k0s",
					"controller",
					"--enable-worker",
					"--config",
					"/etc/k0s/k0s.yaml",
					"--labels",
					`${nodeRoleKey}=korifi`,
					"--taints",
					`${nodeRoleKey}=korifi:NoSchedule`,
				],
				envs: [
					`K0S_CONFIG_SHA=${configSha}`,
					`KORIFI_OIDC_ISSUER=${args.oidc?.issuerUrl ?? ""}`,
				],
				labels: [{ label: nodeRoleKey, value: "korifi" }],
				ports: [
					{ internal: 6443, external: 6443 },
					{ internal: 32080, external: 80 },
					{ internal: 32443, external: 443 },
					{
						internal: kindRegistry.nodePort,
						external: kindRegistry.nodePort,
					},
					{ internal: 30443, external: 30443 },
				],
				uploads: [
					{
						file: "/etc/k0s/k0s.yaml",
						content: k0sYaml,
					},
				],
				volumes: [
					{
						volumeName: volumes.korifiLib.name,
						containerPath: "/var/lib/k0s",
					},
					{
						volumeName: volumes.korifiPods.name,
						containerPath: "/var/log/pods",
					},
					...(args.oidc?.caDir
						? [
								{
									hostPath: args.oidc.caDir,
									containerPath: "/etc/uaa-ssl",
									readOnly: true,
								},
							]
						: []),
				],
			},
			{ parent: this, dependsOn: [imageId] },
		);

		const joinToken = new command.local.Command(
			`${name}-join-token`,
			{
				create: pulumi.interpolate`set -euo pipefail
KORIFI='${korifi}'
TOKEN_FILE='${tokenPath}'
# ${korifiContainer.id}
for i in $(seq 1 90); do
  docker exec "$KORIFI" k0s kubectl get --raw=/readyz >/dev/null 2>&1 && break
  sleep 2
done
docker exec "$KORIFI" k0s kubectl get --raw=/readyz >/dev/null
docker exec "$KORIFI" k0s token create --role=worker --expiry=24h | tr -d '\\n' > "$TOKEN_FILE"
sha256sum "$TOKEN_FILE" | awk '{print $1}'
`,
				delete: `rm -f '${tokenPath}'`,
				logging: command.local.Logging.Stderr,
			},
			{ parent: this, dependsOn: [korifiContainer] },
		);

		const workerVolumes = (lib: docker.Volume, pods: docker.Volume) => [
			{ volumeName: lib.name, containerPath: "/var/lib/k0s" },
			{ volumeName: pods.name, containerPath: "/var/log/pods" },
			{
				hostPath: tokenPath,
				containerPath: "/run/k0s-worker.token",
				readOnly: true,
			},
		];

		const osbContainer = new docker.Container(
			`${name}-osb`,
			{
				...k0sRuntime(),
				name: osb,
				hostname: osb,
				image: imageId.stdout.apply((id) => id.trim()),
				command: [
					"k0s",
					"worker",
					"--token-file",
					"/run/k0s-worker.token",
					"--labels",
					`${nodeRoleKey}=osb`,
					"--taints",
					`${nodeRoleKey}=osb:NoSchedule`,
				],
				envs: [pulumi.interpolate`K0S_JOIN_GENERATION=${joinToken.stdout}`],
				labels: [{ label: nodeRoleKey, value: "osb" }],
				volumes: workerVolumes(volumes.osbLib, volumes.osbPods),
			},
			{ parent: this, dependsOn: [imageId, joinToken] },
		);

		const knativeContainer = new docker.Container(
			`${name}-knative`,
			{
				...k0sRuntime(),
				name: knative,
				hostname: knative,
				image: imageId.stdout.apply((id) => id.trim()),
				command: [
					"k0s",
					"worker",
					"--token-file",
					"/run/k0s-worker.token",
					"--labels",
					`${nodeRoleKey}=knative`,
				],
				envs: [pulumi.interpolate`K0S_JOIN_GENERATION=${joinToken.stdout}`],
				labels: [{ label: nodeRoleKey, value: "knative" }],
				volumes: workerVolumes(volumes.knativeLib, volumes.knativePods),
			},
			{ parent: this, dependsOn: [imageId, joinToken] },
		);

		const ready = new command.local.Command(
			`${name}-kubeconfig`,
			{
				create: [
					`set -euo pipefail`,
					`KORIFI='${korifi}'`,
					`OSB='${osb}'`,
					`KNATIVE='${knative}'`,
					`KUBECONFIG_OUT='${this.kubeconfigPath}'`,
					`REGISTRY_HOST='${registryHost}'`,
					`for i in $(seq 1 90); do`,
					`  ready=$(docker exec "$KORIFI" k0s kubectl get nodes --no-headers 2>/dev/null | awk '$2=="Ready"' | wc -l | tr -d ' ')`,
					`  [ "$ready" = "3" ] && break`,
					`  sleep 2`,
					`done`,
					`ready=$(docker exec "$KORIFI" k0s kubectl get nodes --no-headers | awk '$2=="Ready"' | wc -l | tr -d ' ')`,
					`[ "$ready" = "3" ]`,
					`mkdir -p "$(dirname "$KUBECONFIG_OUT")"`,
					`docker exec "$KORIFI" k0s kubeconfig admin | awk 'BEGIN{done=0} { if (!done && $1=="server:") { sub(/https:\\/\\/[^[:space:]]+/, "https://127.0.0.1:6443"); done=1 } print }' > "$KUBECONFIG_OUT"`,
					`for NODE in "$KORIFI" "$OSB" "$KNATIVE"; do`,
					`  docker exec "$NODE" sh -c "mkdir -p /etc/k0s/containerd.d/certs.d/\${REGISTRY_HOST}"`,
					`  docker exec "$NODE" sh -c "cat > /etc/k0s/containerd.d/cri-registry.toml" <<'TOML'`,
					`version = 3`,
					``,
					`[plugins."io.containerd.cri.v1.images".registry]`,
					`config_path = "/etc/k0s/containerd.d/certs.d"`,
					`TOML`,
					`  docker exec "$NODE" sh -c "cat > /etc/k0s/containerd.d/certs.d/\${REGISTRY_HOST}/hosts.toml" <<'TOML'`,
					`[host."http://127.0.0.1:${kindRegistry.nodePort}"]`,
					`capabilities = ["pull", "resolve", "push"]`,
					`skip_verify = true`,
					`TOML`,
					`done`,
					`cat "$KUBECONFIG_OUT"`,
				].join("\n"),
				delete: `rm -f '${this.kubeconfigPath}'`,
				logging: command.local.Logging.Stderr,
			},
			{
				parent: this,
				dependsOn: [korifiContainer, osbContainer, knativeContainer],
				additionalSecretOutputs: ["stdout"],
			},
		);

		this.kubeconfig = ready.stdout;
		this.provider = new k8s.Provider(
			`${name}-k8s`,
			{ kubeconfig: this.kubeconfig, enableServerSideApply: true },
			{ parent: this, dependsOn: [ready] },
		);

		this.registerOutputs({
			clusterName: this.clusterName,
			kubeconfigPath: this.kubeconfigPath,
		});
	}
}

function nodeVolume(
	name: string,
	clusterName: string,
	role: string,
	kind: "lib" | "pods",
	parent: pulumi.Resource,
): docker.Volume {
	return new docker.Volume(
		`${name}-${role}-${kind}`,
		{ name: `${clusterName}-${role}-${kind}` },
		{ parent },
	);
}

function k0sRuntime(): Pick<
	docker.ContainerArgs,
	| "privileged"
	| "tmpfs"
	| "securityOpts"
	| "devices"
	| "restart"
	| "mustRun"
> {
	return {
		privileged: true,
		tmpfs: {
			"/run": "",
			"/tmp": "",
		},
		securityOpts: ["seccomp=unconfined"],
		devices: [
			{
				hostPath: "/dev/kmsg",
				containerPath: "/dev/kmsg",
			},
		],
		restart: "no",
		mustRun: true,
	};
}

function k0sClusterConfig(oidc?: K0sOidcArgs): string {
	const extraArgs = oidc
		? `    extraArgs:
      oidc-issuer-url: ${oidc.issuerUrl}
      oidc-client-id: ${oidc.clientId ?? "cf"}
      oidc-ca-file: /etc/uaa-ssl/${oidc.caFileName ?? "ca.pem"}
      oidc-username-claim: ${oidc.usernameClaim ?? "user_name"}
      oidc-username-prefix: "${oidc.usernamePrefix ?? "uaa:"}"
      oidc-signing-algs: RS256
`
		: "";
	return `apiVersion: k0s.k0sproject.io/v1beta1
kind: ClusterConfig
metadata:
  name: k0s
spec:
  api:
${extraArgs}`;
}
