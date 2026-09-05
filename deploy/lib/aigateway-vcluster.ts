/**
 * Envoy Gateway + Envoy AI Gateway in a vcluster, plus a shared vLLM model
 * fleet. OSB does not create models; it issues one tenant API key per CF
 * service instance against the shared Gateway.
 *
 * Port-forward pattern matches EverestVcluster; API listen port is distinct.
 */
import * as os from "node:os";
import * as path from "node:path";
import * as command from "@pulumi/command";
import * as k8s from "@pulumi/kubernetes";
import * as pulumi from "@pulumi/pulumi";
import * as random from "@pulumi/random";
import * as tls from "@pulumi/tls";
import { versions } from "./versions";

/** Host port for `kubectl port-forward` to the AI Gateway vcluster API. */
export const kindAIGatewayVclusterLocalApiPort = 18445 as const;

const models = [
	{ id: "opt-125m", hf: "facebook/opt-125m" },
	{ id: "tiny-gpt2", hf: "sshleifer/tiny-gpt2" },
] as const;

export interface AIGatewayVclusterArgs {
	provider: k8s.Provider;
	kindClusterName: string;
	dependsOn?: pulumi.Input<pulumi.Resource>[];
}

export class AIGatewayVcluster extends pulumi.ComponentResource {
	readonly namespace: string;
	readonly gatewayNamespace: string;
	readonly vclusterName: string;
	readonly virtualProvider: k8s.Provider;
	readonly inClusterKubeconfig: pulumi.Output<string>;
	readonly ready: pulumi.Resource;

	constructor(
		name: string,
		args: AIGatewayVclusterArgs,
		opts?: pulumi.ComponentResourceOptions,
	) {
		super("korifi:deploy:AIGatewayVcluster", name, {}, opts);

		this.namespace = "aigateway-vcluster";
		this.gatewayNamespace = "aigateway";
		this.vclusterName = "aigateway";
		const vclusterName = this.vclusterName;
		const gwNs = this.gatewayNamespace;

		const childOpts: pulumi.CustomResourceOptions = {
			parent: this,
			provider: args.provider,
			dependsOn: args.dependsOn,
		};

		const ns = new k8s.core.v1.Namespace(
			`${name}-ns`,
			{ metadata: { name: this.namespace } },
			childOpts,
		);

		const vclusterRelease = new k8s.helm.v3.Release(
			`${name}-vcluster`,
			{
				name: vclusterName,
				chart: "vcluster",
				version: versions.vclusterChart,
				repositoryOpts: { repo: "https://charts.loft.sh" },
				namespace: this.namespace,
				values: {
					exportKubeConfig: {
						server: `https://${vclusterName}.${this.namespace}.svc`,
						secret: { name: `vc-${vclusterName}` },
					},
					sync: {
						toHost: {
							services: { enabled: true },
						},
					},
				},
				timeout: 900,
			},
			{
				...childOpts,
				dependsOn: [...(args.dependsOn ?? []), ns],
				customTimeouts: { create: "20m", update: "20m" },
			},
		);

		const hostKubeconfig = `$HOME/.kube/kind-${args.kindClusterName}.config`;
		const pfPidFile = path.join(
			os.tmpdir(),
			`korifi-vcluster-${vclusterName}-pf.pid`,
		);
		const pfLogFile = path.join(
			os.tmpdir(),
			`korifi-vcluster-${vclusterName}-pf.log`,
		);
		const apiPort = String(kindAIGatewayVclusterLocalApiPort);

		const apiForward = new command.local.Command(
			`${name}-api-forward`,
			{
				create: aigwVclusterForwardScript({
					hostKubeconfig,
					namespace: this.namespace,
					svc: vclusterName,
					pidFile: pfPidFile,
					logFile: pfLogFile,
					port: apiPort,
					mode: "create",
				}),
				update: aigwVclusterForwardScript({
					hostKubeconfig,
					namespace: this.namespace,
					svc: vclusterName,
					pidFile: pfPidFile,
					logFile: pfLogFile,
					port: apiPort,
					mode: "update",
				}),
				delete: `set -euo pipefail
PIDFILE='${pfPidFile}'
if [ -f "$PIDFILE" ]; then
  kill "$(cat "$PIDFILE")" 2>/dev/null || true
  rm -f "$PIDFILE"
fi
`,
				triggers: ["aigateway-vcluster-api-forward-v1"],
			},
			{ parent: this, dependsOn: [vclusterRelease] },
		);

		const kubeconfigCmd = new command.local.Command(
			`${name}-kubeconfig`,
			{
				create: `set -euo pipefail
KUBECONFIG="${hostKubeconfig}"
NS="${this.namespace}"
SECRET="vc-${vclusterName}"
PORT='${apiPort}'
for i in $(seq 1 90); do
  if kubectl --kubeconfig "$KUBECONFIG" -n "$NS" get secret "$SECRET" -o jsonpath='{.data.config}' 2>/dev/null | grep -q .; then
    kubectl --kubeconfig "$KUBECONFIG" -n "$NS" get secret "$SECRET" -o go-template='{{ index .data.config | base64decode }}' \\
      | sed -E "s#server: https?://[^[:space:]]+#server: https://127.0.0.1:$PORT#"
    exit 0
  fi
  sleep 5
done
echo "timed out waiting for kubeconfig secret/$SECRET in $NS" >&2
exit 1
`,
			},
			{
				parent: this,
				dependsOn: [vclusterRelease, apiForward],
				additionalSecretOutputs: ["stdout"],
			},
		);

		const pulumiKubeconfig = kubeconfigCmd.stdout.apply((raw) =>
			raw.replace(
				/server:\s*https?:\/\/[^\s]+/,
				`server: https://127.0.0.1:${apiPort}`,
			),
		);
		this.inClusterKubeconfig = kubeconfigCmd.stdout.apply((raw) =>
			raw.replace(
				/server:\s*https?:\/\/[^\s]+/,
				`server: https://${vclusterName}.${this.namespace}.svc`,
			),
		);

		this.virtualProvider = new k8s.Provider(
			`${name}-virtual-k8s`,
			{
				kubeconfig: pulumiKubeconfig,
				enableServerSideApply: true,
			},
			{ parent: this, dependsOn: [kubeconfigCmd, apiForward] },
		);

		const virtualOpts: pulumi.CustomResourceOptions = {
			parent: this,
			provider: this.virtualProvider,
		};

		const envoyGateway = new k8s.helm.v3.Release(
			`${name}-envoy-gateway`,
			{
				name: "eg",
				chart: "oci://docker.io/envoyproxy/gateway-helm",
				version: versions.envoyGatewayChart,
				namespace: "envoy-gateway-system",
				createNamespace: true,
				timeout: 900,
				values: {
					config: {
						envoyGateway: {
							gateway: {
								controllerName:
									"gateway.envoyproxy.io/gatewayclass-controller",
							},
							logging: { level: { default: "info" } },
							provider: { type: "Kubernetes" },
							extensionApis: {
								enableEnvoyPatchPolicy: true,
								enableBackend: true,
							},
							extensionManager: {
								hooks: {
									xdsTranslator: {
										translation: {
											listener: { includeAll: true },
											route: { includeAll: true },
											cluster: { includeAll: true },
											secret: { includeAll: true },
										},
										post: ["Translation", "Cluster", "Route"],
									},
								},
								service: {
									fqdn: {
										hostname:
											"ai-gateway-controller.envoy-ai-gateway-system.svc.cluster.local",
										port: 1063,
									},
								},
							},
						},
					},
				},
			},
			{
				...virtualOpts,
				customTimeouts: { create: "20m", update: "20m" },
			},
		);

		const aiGatewayCrds = new k8s.helm.v3.Release(
			`${name}-aieg-crds`,
			{
				name: "aieg-crd",
				chart: "oci://docker.io/envoyproxy/ai-gateway-crds-helm",
				version: versions.aiGatewayChart,
				namespace: "envoy-ai-gateway-system",
				createNamespace: true,
				timeout: 600,
			},
			{
				...virtualOpts,
				dependsOn: [envoyGateway],
				customTimeouts: { create: "10m", update: "10m" },
			},
		);

		const aiGateway = new k8s.helm.v3.Release(
			`${name}-aieg`,
			{
				name: "aieg",
				chart: "oci://docker.io/envoyproxy/ai-gateway-helm",
				version: versions.aiGatewayChart,
				namespace: "envoy-ai-gateway-system",
				createNamespace: true,
				timeout: 600,
			},
			{
				...virtualOpts,
				dependsOn: [aiGatewayCrds],
				customTimeouts: { create: "10m", update: "10m" },
			},
		);

		const appNs = new k8s.core.v1.Namespace(
			`${name}-app-ns`,
			{ metadata: { name: gwNs } },
			{ ...virtualOpts, dependsOn: [aiGateway] },
		);

		const caKey = new tls.PrivateKey(
			`${name}-ca-key`,
			{ algorithm: "RSA", rsaBits: 2048 },
			{ parent: this },
		);
		const caCert = new tls.SelfSignedCert(
			`${name}-ca`,
			{
				privateKeyPem: caKey.privateKeyPem,
				validityPeriodHours: 24 * 365 * 5,
				isCaCertificate: true,
				allowedUses: ["cert_signing", "server_auth", "client_auth"],
				subject: { commonName: "korifi-aigateway-ca" },
			},
			{ parent: this },
		);
		const caConfig = new k8s.core.v1.ConfigMap(
			`${name}-ca`,
			{
				metadata: { name: "aigw-ca", namespace: gwNs },
				data: { "ca.crt": caCert.certPem },
			},
			{ ...virtualOpts, dependsOn: [appNs] },
		);

		const gatewayDns = [
			"aigw",
			`aigw.${gwNs}`,
			`aigw.${gwNs}.svc`,
			`aigw.${gwNs}.svc.cluster.local`,
		];
		const gwMaterial = signedCert(
			this,
			`${name}-gw`,
			"aigw",
			gatewayDns,
			caKey.privateKeyPem,
			caCert.certPem,
		);
		const gwTLS = new k8s.core.v1.Secret(
			`${name}-gw-tls`,
			{
				metadata: { name: "aigw-tls", namespace: gwNs },
				type: "kubernetes.io/tls",
				stringData: {
					"tls.crt": gwMaterial.certPem,
					"tls.key": gwMaterial.keyPem,
					"ca.crt": caCert.certPem,
				},
			},
			{ ...virtualOpts, dependsOn: [appNs] },
		);

		const bootstrapKey = new random.RandomPassword(
			`${name}-bootstrap-key`,
			{ length: 32, special: false },
			{ parent: this },
		);
		const bootstrapSecret = new k8s.core.v1.Secret(
			`${name}-bootstrap`,
			{
				metadata: { name: "aigw-bootstrap", namespace: gwNs },
				stringData: { bootstrap: bootstrapKey.result },
			},
			{ ...virtualOpts, dependsOn: [appNs] },
		);

		const routeRules: pulumi.Input<unknown>[] = [];
		const modelResources: pulumi.Resource[] = [];
		for (const model of models) {
			const created = this.installModel({
				name,
				model,
				gwNs,
				virtualOpts,
				appNs,
				caKeyPem: caKey.privateKeyPem,
				caCertPem: caCert.certPem,
				caConfig,
			});
			modelResources.push(...created.resources);
			routeRules.push(created.rule);
		}

		const gatewayClass = new k8s.apiextensions.CustomResource(
			`${name}-gatewayclass`,
			{
				apiVersion: "gateway.networking.k8s.io/v1",
				kind: "GatewayClass",
				metadata: { name: "aigateway" },
				spec: {
					controllerName: "gateway.envoyproxy.io/gatewayclass-controller",
				},
			},
			{ ...virtualOpts, dependsOn: [envoyGateway, appNs] },
		);

		const envoyProxy = new k8s.apiextensions.CustomResource(
			`${name}-envoyproxy`,
			{
				apiVersion: "gateway.envoyproxy.io/v1alpha1",
				kind: "EnvoyProxy",
				metadata: { name: "aigateway", namespace: gwNs },
				spec: {
					provider: {
						type: "Kubernetes",
						kubernetes: {
							envoyService: { name: "aigw", type: "ClusterIP" },
							envoyDeployment: {
								replicas: 1,
								container: {
									resources: {
										requests: { cpu: "100m", memory: "256Mi" },
									},
								},
							},
						},
					},
				},
			},
			{ ...virtualOpts, dependsOn: [appNs, envoyGateway] },
		);

		const gateway = new k8s.apiextensions.CustomResource(
			`${name}-gateway`,
			{
				apiVersion: "gateway.networking.k8s.io/v1",
				kind: "Gateway",
				metadata: { name: "aigateway", namespace: gwNs },
				spec: {
					gatewayClassName: "aigateway",
					listeners: [
						{
							name: "https",
							protocol: "HTTPS",
							port: 443,
							tls: {
								mode: "Terminate",
								certificateRefs: [{ kind: "Secret", name: "aigw-tls" }],
							},
						},
					],
					infrastructure: {
						parametersRef: {
							group: "gateway.envoyproxy.io",
							kind: "EnvoyProxy",
							name: "aigateway",
						},
					},
				},
			},
			{
				...virtualOpts,
				dependsOn: [gatewayClass, envoyProxy, gwTLS, appNs],
			},
		);

		const clientBuffer = new k8s.apiextensions.CustomResource(
			`${name}-client-buffer`,
			{
				apiVersion: "gateway.envoyproxy.io/v1alpha1",
				kind: "ClientTrafficPolicy",
				metadata: { name: "client-buffer-limit", namespace: gwNs },
				spec: {
					targetRefs: [
						{
							group: "gateway.networking.k8s.io",
							kind: "Gateway",
							name: "aigateway",
						},
					],
					connection: { bufferLimit: "50Mi" },
				},
			},
			{ ...virtualOpts, dependsOn: [gateway] },
		);

		const securityPolicy = new k8s.apiextensions.CustomResource(
			`${name}-securitypolicy`,
			{
				apiVersion: "gateway.envoyproxy.io/v1alpha1",
				kind: "SecurityPolicy",
				metadata: { name: "aigw-clients", namespace: gwNs },
				spec: {
					targetRefs: [
						{
							group: "gateway.networking.k8s.io",
							kind: "Gateway",
							name: "aigateway",
						},
					],
					apiKeyAuth: {
						credentialRefs: [
							{ group: "", kind: "Secret", name: "aigw-bootstrap" },
						],
						extractFrom: [{ headers: ["Authorization"] }],
						sanitize: true,
					},
				},
			},
			{ ...virtualOpts, dependsOn: [gateway, bootstrapSecret] },
		);

		const route = new k8s.apiextensions.CustomResource(
			`${name}-route`,
			{
				apiVersion: "aigateway.envoyproxy.io/v1beta1",
				kind: "AIGatewayRoute",
				metadata: { name: "aigateway", namespace: gwNs },
				spec: {
					parentRefs: [
						{
							name: "aigateway",
							kind: "Gateway",
							group: "gateway.networking.k8s.io",
						},
					],
					schema: { name: "OpenAI", version: "v1" },
					rules: routeRules,
				},
			},
			{
				...virtualOpts,
				dependsOn: [gateway, aiGateway, ...modelResources],
			},
		);

		this.ready = route;
		this.registerOutputs({
			namespace: this.namespace,
			gatewayNamespace: this.gatewayNamespace,
			clientBuffer: clientBuffer.metadata.name,
			securityPolicy: securityPolicy.metadata.name,
		});
	}

	private installModel(args: {
		name: string;
		model: (typeof models)[number];
		gwNs: string;
		virtualOpts: pulumi.CustomResourceOptions;
		appNs: pulumi.Resource;
		caKeyPem: pulumi.Input<string>;
		caCertPem: pulumi.Input<string>;
		caConfig: k8s.core.v1.ConfigMap;
	}): { resources: pulumi.Resource[]; rule: Record<string, unknown> } {
		const { name, model, gwNs, virtualOpts, appNs, caKeyPem, caCertPem, caConfig } =
			args;
		const resName = `vllm-${model.id}`;
		const dns = [
			resName,
			`${resName}.${gwNs}`,
			`${resName}.${gwNs}.svc`,
			`${resName}.${gwNs}.svc.cluster.local`,
		];
		const material = signedCert(
			this,
			`${name}-${model.id}`,
			resName,
			dns,
			caKeyPem,
			caCertPem,
		);
		const tlsSecret = new k8s.core.v1.Secret(
			`${name}-${model.id}-tls`,
			{
				metadata: { name: `${resName}-tls`, namespace: gwNs },
				type: "kubernetes.io/tls",
				stringData: {
					"tls.crt": material.certPem,
					"tls.key": material.keyPem,
					"ca.crt": caCertPem,
				},
			},
			{ ...virtualOpts, dependsOn: [appNs] },
		);
		const apiKey = new random.RandomPassword(
			`${name}-${model.id}-key`,
			{ length: 32, special: false },
			{ parent: this },
		);
		const authSecret = new k8s.core.v1.Secret(
			`${name}-${model.id}-auth`,
			{
				metadata: { name: `${resName}-auth`, namespace: gwNs },
				stringData: { apiKey: apiKey.result },
			},
			{ ...virtualOpts, dependsOn: [appNs] },
		);
		const labels = { app: resName, "osb.korifi/offering": "aigateway" };
		const deploy = new k8s.apps.v1.Deployment(
			`${name}-${model.id}-deploy`,
			{
				metadata: { name: resName, namespace: gwNs, labels },
				spec: {
					replicas: 1,
					selector: { matchLabels: { app: resName } },
					template: {
						metadata: { labels },
						spec: {
							containers: [
								{
									name: "vllm",
									image: versions.vllmImage,
									imagePullPolicy: "IfNotPresent",
									command: ["vllm"],
									args: [
										"serve",
										model.hf,
										"--host",
										"0.0.0.0",
										"--port",
										"8000",
										"--served-model-name",
										model.id,
										"--ssl-certfile",
										"/tls/tls.crt",
										"--ssl-keyfile",
										"/tls/tls.key",
										"--device",
										"cpu",
										"--dtype",
										"float32",
										"--max-model-len",
										"256",
									],
									env: [
										{
											name: "VLLM_API_KEY",
											valueFrom: {
												secretKeyRef: {
													name: `${resName}-auth`,
													key: "apiKey",
												},
											},
										},
									],
									ports: [{ containerPort: 8000, name: "https" }],
									resources: {
										requests: { cpu: "250m", memory: "2Gi" },
									},
									volumeMounts: [
										{ name: "tls", mountPath: "/tls", readOnly: true },
										{ name: "cache", mountPath: "/root/.cache/huggingface" },
									],
									livenessProbe: {
										tcpSocket: { port: 8000 },
										initialDelaySeconds: 60,
										periodSeconds: 20,
									},
									readinessProbe: {
										tcpSocket: { port: 8000 },
										initialDelaySeconds: 30,
										periodSeconds: 10,
									},
								},
							],
							volumes: [
								{
									name: "tls",
									secret: { secretName: `${resName}-tls` },
								},
								{ name: "cache", emptyDir: {} },
							],
						},
					},
				},
			},
			{ ...virtualOpts, dependsOn: [appNs, tlsSecret, authSecret] },
		);
		const svc = new k8s.core.v1.Service(
			`${name}-${model.id}-svc`,
			{
				metadata: { name: resName, namespace: gwNs, labels },
				spec: {
					selector: { app: resName },
					ports: [{ name: "https", port: 8000, targetPort: 8000 }],
				},
			},
			{ ...virtualOpts, dependsOn: [deploy] },
		);
		const backend = new k8s.apiextensions.CustomResource(
			`${name}-${model.id}-backend`,
			{
				apiVersion: "gateway.envoyproxy.io/v1alpha1",
				kind: "Backend",
				metadata: { name: resName, namespace: gwNs },
				spec: {
					endpoints: [
						{
							fqdn: {
								hostname: `${resName}.${gwNs}.svc.cluster.local`,
								port: 8000,
							},
						},
					],
					tls: {
						caCertificateRefs: [
							{ group: "", kind: "ConfigMap", name: "aigw-ca" },
						],
					},
				},
			},
			{ ...virtualOpts, dependsOn: [svc, caConfig] },
		);
		const backendTLS = new k8s.apiextensions.CustomResource(
			`${name}-${model.id}-backend-tls`,
			{
				apiVersion: "gateway.networking.k8s.io/v1alpha3",
				kind: "BackendTLSPolicy",
				metadata: { name: `${resName}-tls`, namespace: gwNs },
				spec: {
					targetRefs: [
						{
							group: "gateway.envoyproxy.io",
							kind: "Backend",
							name: resName,
							sectionName: "8000",
						},
					],
					validation: {
						caCertificateRefs: [
							{ group: "", kind: "ConfigMap", name: "aigw-ca" },
						],
						hostname: `${resName}.${gwNs}.svc.cluster.local`,
					},
				},
			},
			{ ...virtualOpts, dependsOn: [backend, caConfig] },
		);
		const aiBackend = new k8s.apiextensions.CustomResource(
			`${name}-${model.id}-aibackend`,
			{
				apiVersion: "aigateway.envoyproxy.io/v1beta1",
				kind: "AIServiceBackend",
				metadata: { name: resName, namespace: gwNs },
				spec: {
					schema: { name: "OpenAI", version: "v1" },
					backendRef: {
						name: resName,
						kind: "Backend",
						group: "gateway.envoyproxy.io",
					},
				},
			},
			{ ...virtualOpts, dependsOn: [backend] },
		);
		const backendAuth = new k8s.apiextensions.CustomResource(
			`${name}-${model.id}-backend-auth`,
			{
				apiVersion: "aigateway.envoyproxy.io/v1beta1",
				kind: "BackendSecurityPolicy",
				metadata: { name: `${resName}-auth`, namespace: gwNs },
				spec: {
					targetRefs: [
						{
							group: "aigateway.envoyproxy.io",
							kind: "AIServiceBackend",
							name: resName,
						},
					],
					type: "APIKey",
					apiKey: {
						secretRef: { name: `${resName}-auth`, namespace: gwNs },
					},
				},
			},
			{ ...virtualOpts, dependsOn: [aiBackend, authSecret] },
		);
		return {
			resources: [
				tlsSecret,
				authSecret,
				deploy,
				svc,
				backend,
				backendTLS,
				aiBackend,
				backendAuth,
			],
			rule: {
				matches: [
					{
						headers: [
							{
								type: "Exact",
								name: "x-ai-eg-model",
								value: model.id,
							},
						],
					},
				],
				modelsOwnedBy: "osb-service",
				backendRefs: [{ name: resName }],
			},
		};
	}
}

function signedCert(
	parent: pulumi.Resource,
	name: string,
	commonName: string,
	dnsNames: string[],
	caKeyPem: pulumi.Input<string>,
	caCertPem: pulumi.Input<string>,
): { certPem: pulumi.Output<string>; keyPem: pulumi.Output<string> } {
	const key = new tls.PrivateKey(
		`${name}-key`,
		{ algorithm: "RSA", rsaBits: 2048 },
		{ parent },
	);
	const csr = new tls.CertRequest(
		`${name}-csr`,
		{
			privateKeyPem: key.privateKeyPem,
			subject: { commonName },
			dnsNames,
		},
		{ parent },
	);
	const cert = new tls.LocallySignedCert(
		`${name}-cert`,
		{
			caPrivateKeyPem: caKeyPem,
			caCertPem,
			certRequestPem: csr.certRequestPem,
			validityPeriodHours: 24 * 365 * 5,
			allowedUses: ["server_auth"],
		},
		{ parent },
	);
	return { certPem: cert.certPem, keyPem: key.privateKeyPem };
}

function aigwVclusterForwardScript(args: {
	hostKubeconfig: string;
	namespace: string;
	svc: string;
	pidFile: string;
	logFile: string;
	port: string;
	mode: "create" | "update";
}): string {
	const skipIfUp =
		args.mode === "update"
			? `if curl -sk --connect-timeout 1 "https://127.0.0.1:${args.port}/readyz" >/dev/null 2>&1; then
  echo "vcluster API already forwarded on 127.0.0.1:${args.port}"
  exit 0
fi
`
			: "";
	return `set -euo pipefail
KUBECONFIG="${args.hostKubeconfig}"
NS="${args.namespace}"
SVC="${args.svc}"
PIDFILE='${args.pidFile}'
LOGFILE='${args.logFile}'
PORT='${args.port}'
${skipIfUp}if [ -f "$PIDFILE" ]; then
  kill "$(cat "$PIDFILE")" 2>/dev/null || true
  rm -f "$PIDFILE"
fi
for i in $(seq 1 90); do
  kubectl --kubeconfig "$KUBECONFIG" -n "$NS" get svc "$SVC" >/dev/null 2>&1 && break
  sleep 2
done
kubectl --kubeconfig "$KUBECONFIG" -n "$NS" get svc "$SVC" >/dev/null
nohup kubectl --kubeconfig "$KUBECONFIG" -n "$NS" port-forward "svc/$SVC" "$PORT:443" >"$LOGFILE" 2>&1 &
echo $! >"$PIDFILE"
for i in $(seq 1 60); do
  if curl -sk --connect-timeout 1 "https://127.0.0.1:$PORT/readyz" >/dev/null 2>&1 \\
    || nc -z 127.0.0.1 "$PORT" 2>/dev/null; then
    echo "vcluster API forwarded on 127.0.0.1:$PORT"
    exit 0
  fi
  sleep 1
done
echo "timed out waiting for port-forward on $PORT" >&2
cat "$LOGFILE" >&2 || true
exit 1
`;
}
