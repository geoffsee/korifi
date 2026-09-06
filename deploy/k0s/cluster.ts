/**
 * Three-node k0s cluster in Docker: korifi (controller+worker), osb, knative.
 *
 * korifi and osb are NoSchedule-tainted; knative stays schedulable for apps.
 */
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import * as command from "@pulumi/command";
import * as k8s from "@pulumi/kubernetes";
import * as pulumi from "@pulumi/pulumi";
import { kindRegistry, nodeRoleKey, versions } from "@korifi/deploy-lib";

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

		const image = args.k0sImage ?? versions.k0sImage;
		const workDir = path.join(
			os.tmpdir(),
			`korifi-k0s-${args.clusterName}`,
		);
		fs.mkdirSync(workDir, { recursive: true });
		const configPath = path.join(workDir, "k0s.yaml");
		writeK0sConfig(configPath, args.oidc);
		const tokenPath = path.join(workDir, "worker.token");

		const korifi = this.nodeContainers.korifi;
		const osb = this.nodeContainers.osb;
		const knative = this.nodeContainers.knative;
		const registryHost = kindRegistry.clusterHost;
		const expectedIssuer = args.oidc?.issuerUrl ?? "";
		const caDir = args.oidc?.caDir ?? "";

		const dockerCommon = [
			"--privileged",
			"--tmpfs /run",
			"--tmpfs /tmp",
			"--security-opt seccomp=unconfined",
			"--device /dev/kmsg",
		].join(" ");

		const bootstrap = new command.local.Command(
			`${name}-bootstrap`,
			{
				create: [
					`set -euo pipefail`,
					`CLUSTER='${args.clusterName}'`,
					`KORIFI='${korifi}'`,
					`OSB='${osb}'`,
					`KNATIVE='${knative}'`,
					`IMAGE='${image}'`,
					`CONFIG='${configPath}'`,
					`TOKEN_FILE='${tokenPath}'`,
					`KUBECONFIG_OUT='${this.kubeconfigPath}'`,
					`ROLE_KEY='${nodeRoleKey}'`,
					`REGISTRY_HOST='${registryHost}'`,
					`CA_DIR='${caDir}'`,
					`EXPECTED_ISSUER='${expectedIssuer}'`,
					`need_create=0`,
					`if ! docker inspect "$KORIFI" "$OSB" "$KNATIVE" >/dev/null 2>&1; then`,
					`  need_create=1`,
					`elif [ -n "$EXPECTED_ISSUER" ]; then`,
					`  if ! docker exec "$KORIFI" sh -c "ps auxww | grep kube-apiserver | grep -Fq oidc-issuer-url=\$EXPECTED_ISSUER" 2>/dev/null; then`,
					`    echo "k0s cluster $CLUSTER exists without expected OIDC issuer; recreating"`,
					`    need_create=1`,
					`  fi`,
					`fi`,
					`if [ "$need_create" -eq 1 ]; then`,
					`  docker rm -f "$KORIFI" "$OSB" "$KNATIVE" >/dev/null 2>&1 || true`,
					`  docker volume rm "\${CLUSTER}-korifi-lib" "\${CLUSTER}-korifi-pods" "\${CLUSTER}-osb-lib" "\${CLUSTER}-osb-pods" "\${CLUSTER}-knative-lib" "\${CLUSTER}-knative-pods" >/dev/null 2>&1 || true`,
					`  CA_MOUNT=""`,
					`  if [ -n "$CA_DIR" ]; then CA_MOUNT="-v \${CA_DIR}:/etc/uaa-ssl:ro"; fi`,
					`  docker run -d --name "$KORIFI" --hostname "$KORIFI" ${dockerCommon} \\`,
					`    -v "\${CLUSTER}-korifi-lib:/var/lib/k0s" -v "\${CLUSTER}-korifi-pods:/var/log/pods" \\`,
					`    -v "$CONFIG:/etc/k0s/k0s.yaml:ro" $CA_MOUNT \\`,
					`    -p 6443:6443 -p 80:32080 -p 443:32443 -p ${kindRegistry.nodePort}:${kindRegistry.nodePort} -p 30443:30443 \\`,
					`    "$IMAGE" k0s controller --enable-worker --config /etc/k0s/k0s.yaml \\`,
					`      --labels "\${ROLE_KEY}=korifi" --taints "\${ROLE_KEY}=korifi:NoSchedule"`,
					`  for i in $(seq 1 90); do`,
					`    docker exec "$KORIFI" k0s kubectl get --raw=/readyz >/dev/null 2>&1 && break`,
					`    sleep 2`,
					`  done`,
					`  docker exec "$KORIFI" k0s kubectl get --raw=/readyz >/dev/null`,
					`  docker exec "$KORIFI" k0s token create --role=worker --expiry=24h | tr -d '\\n' > "$TOKEN_FILE"`,
					`  docker run -d --name "$OSB" --hostname "$OSB" ${dockerCommon} \\`,
					`    -v "\${CLUSTER}-osb-lib:/var/lib/k0s" -v "\${CLUSTER}-osb-pods:/var/log/pods" \\`,
					`    -v "$TOKEN_FILE:/run/k0s-worker.token:ro" \\`,
					`    "$IMAGE" k0s worker --token-file /run/k0s-worker.token \\`,
					`      --labels "\${ROLE_KEY}=osb" --taints "\${ROLE_KEY}=osb:NoSchedule"`,
					`  docker run -d --name "$KNATIVE" --hostname "$KNATIVE" ${dockerCommon} \\`,
					`    -v "\${CLUSTER}-knative-lib:/var/lib/k0s" -v "\${CLUSTER}-knative-pods:/var/log/pods" \\`,
					`    -v "$TOKEN_FILE:/run/k0s-worker.token:ro" \\`,
					`    "$IMAGE" k0s worker --token-file /run/k0s-worker.token \\`,
					`      --labels "\${ROLE_KEY}=knative"`,
					`  for i in $(seq 1 90); do`,
					`    ready=$(docker exec "$KORIFI" k0s kubectl get nodes --no-headers 2>/dev/null | awk '\$2=="Ready"' | wc -l | tr -d ' ')`,
					`    [ "\$ready" = "3" ] && break`,
					`    sleep 2`,
					`  done`,
					`  ready=$(docker exec "$KORIFI" k0s kubectl get nodes --no-headers | awk '\$2=="Ready"' | wc -l | tr -d ' ')`,
					`  [ "\$ready" = "3" ]`,
					`fi`,
					`mkdir -p "$(dirname "$KUBECONFIG_OUT")"`,
					`docker exec "$KORIFI" k0s kubeconfig admin | awk 'BEGIN{done=0} { if (!done && \$1=="server:") { sub(/https:\\/\\/[^[:space:]]+/, "https://127.0.0.1:6443"); done=1 } print }' > "$KUBECONFIG_OUT"`,
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
				delete: [
					`set -euo pipefail`,
					`docker rm -f '${korifi}' '${osb}' '${knative}' >/dev/null 2>&1 || true`,
					`docker volume rm '${args.clusterName}-korifi-lib' '${args.clusterName}-korifi-pods' '${args.clusterName}-osb-lib' '${args.clusterName}-osb-pods' '${args.clusterName}-knative-lib' '${args.clusterName}-knative-pods' >/dev/null 2>&1 || true`,
					`rm -f '${this.kubeconfigPath}' '${tokenPath}'`,
				].join("\n"),
				logging: command.local.Logging.Stderr,
			},
			{ parent: this, additionalSecretOutputs: ["stdout"] },
		);

		this.kubeconfig = bootstrap.stdout;
		this.provider = new k8s.Provider(
			`${name}-k8s`,
			{ kubeconfig: this.kubeconfig, enableServerSideApply: true },
			{ parent: this, dependsOn: [bootstrap] },
		);

		this.registerOutputs({
			clusterName: this.clusterName,
			kubeconfigPath: this.kubeconfigPath,
		});
	}
}

function writeK0sConfig(outPath: string, oidc?: K0sOidcArgs): void {
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
	fs.writeFileSync(
		outPath,
		`apiVersion: k0s.k0sproject.io/v1beta1
kind: ClusterConfig
metadata:
  name: k0s
spec:
  api:
${extraArgs}`,
	);
}
