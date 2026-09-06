/**
 * deploy/k0s — Korifi on a 3-node k0s-in-docker cluster in one `pulumi up`.
 *
 * Nodes:
 *   {cluster}-korifi    controller+worker (tainted) — Korifi, UAA, registry, deps
 *   {cluster}-osb       worker (tainted) — OSB broker + Everest/AI Gateway vclusters
 *   {cluster}-knative   worker (untainted) — Knative Serving (knative-runner) and CF apps
 *
 * Usage:
 *   cd deploy/k0s && bun install
 *   export PULUMI_CONFIG_PASSPHRASE=...
 *   pulumi up --stack dev
 */
import * as path from "node:path";
import * as k8s from "@pulumi/kubernetes";
import * as pulumi from "@pulumi/pulumi";
import {
	ContourGateway,
	K0sKorifiImages,
	K0sOsbBrokerImage,
	KnativeServing,
	KorifiDependencies,
	KorifiNamespaces,
	KorifiRelease,
	LocalRegistry,
	OsbServiceBroker,
	ServiceBrokerServices,
	UaaCerts,
	UaaVcluster,
	osbServicePath,
	k0sGatewayPorts,
	k0sKpackBuilderRepository,
	k0sRegistryPrefix,
	kindUaaHostname,
	kindUaaNodePort,
	placementFor,
} from "@korifi/deploy-lib";
import { K0sCluster } from "./cluster";
import { K0sEverestOperatorBundles } from "./everest-operator-bundles";
import { K0sImagePrefetch } from "./image-prefetch";
import { K0sPlatformPlacement } from "./platform-placement";
import { K0sLocalPathStorage } from "./storage";
import {
	adminEmail,
	aiGatewayBackends,
	apiUrl,
	appDomain,
	clusterName,
	kubeconfigPath,
	oidcPrefix,
	pinned,
	registryUser,
} from "./config";

const uaaUrl = `https://127.0.0.1:${kindUaaNodePort}/uaa`;
const adminUserName = `${oidcPrefix}:${adminEmail}`;
const korifiPlacement = placementFor("korifi");
const osbPlacement = placementFor("osb");

const certs = new UaaCerts("uaa-certs", {
	hostname: kindUaaHostname,
});

const cluster = new K0sCluster(
	"k0s",
	{
		clusterName,
		kubeconfigPath,
		k0sImage: pinned.k0sImage,
		oidc: {
			issuerUrl: `${uaaUrl}/oauth/token`,
			caDir: certs.outputDir,
			clientId: "cf",
			usernameClaim: "user_name",
			usernamePrefix: `${oidcPrefix}:`,
		},
	},
	{ dependsOn: [certs.filesReady] },
);

const storage = new K0sLocalPathStorage(
	"storage",
	{ provider: cluster.provider, dependsOn: [cluster] },
	{ dependsOn: [cluster] },
);

const prefetchedImages = new K0sImagePrefetch(
	"external-images",
	{ clusterName, dependsOn: [cluster] },
	{ dependsOn: [cluster] },
);

const namespaces = new KorifiNamespaces(
	"ns",
	{ provider: cluster.provider, installerNamespace: true },
	{ dependsOn: [cluster] },
);

const registry = new LocalRegistry(
	"registry",
	{
		provider: cluster.provider,
		username: registryUser,
		nodeSelector: korifiPlacement.nodeSelector,
		tolerations: korifiPlacement.tolerations,
		dependsOn: [namespaces, prefetchedImages.coreLoaded, storage],
	},
	{ dependsOn: [namespaces, prefetchedImages.coreLoaded, storage] },
);

const cfPullSecret = registry.pullSecret(
	"cf-registry-credentials",
	namespaces.root.metadata.name,
	{ provider: cluster.provider, dependsOn: [registry.release, namespaces.root] },
);

const korifiPullSecret = registry.pullSecret(
	"korifi-registry-credentials",
	namespaces.korifi.metadata.name,
	{
		provider: cluster.provider,
		dependsOn: [registry.release, namespaces.korifi],
	},
);

const repoRoot = path.join(__dirname, "..", "..");
const localChart = path.join(repoRoot, "helm", "korifi");
const nodeContainers = [
	cluster.nodeContainers.korifi,
	cluster.nodeContainers.osb,
	cluster.nodeContainers.knative,
];

const images = new K0sKorifiImages(
	"images",
	{
		nodeContainers,
		repoRoot,
		dependsOn: [cluster],
	},
	{ dependsOn: [cluster] },
);

const dependencies = new KorifiDependencies(
	"deps",
	{
		provider: cluster.provider,
		knativeDomain: appDomain,
		clusterType: "k0s",
		insecureTlsMetricsServer: true,
		installerImage: pinned.installerImage,
		installerNamespace: namespaces.installer!.metadata.name,
		nodeSelector: korifiPlacement.nodeSelector,
		tolerations: korifiPlacement.tolerations,
		dependsOn: [namespaces.installer!, prefetchedImages.coreLoaded],
	},
	{ dependsOn: [namespaces, prefetchedImages.coreLoaded] },
);

const platformPlacement = new K0sPlatformPlacement(
	"platform-placement",
	{
		kubeconfigPath,
		dependsOn: [dependencies.job],
	},
	{ dependsOn: [dependencies] },
);

const uaa = new UaaVcluster(
	"uaa",
	{
		provider: cluster.provider,
		certs,
		kindClusterName: clusterName,
		kubeconfigPath,
		hostScheduling: korifiPlacement,
		uaaUrl,
		adminEmail,
		oidcPrefix,
		dependsOn: [dependencies.job, cluster, platformPlacement.patched],
	},
	{ dependsOn: [dependencies, certs, platformPlacement] },
);

const korifi = new KorifiRelease(
	"korifi",
	{
		provider: cluster.provider,
		namespace: namespaces.korifi.metadata.name,
		chart: localChart,
		values: {
			platform: "k0s",
			adminUserName,
			apiUrl,
			appDomain,
			containerRepositoryPrefix: k0sRegistryPrefix(),
			kpackBuilderRepository: k0sKpackBuilderRepository(),
			uaaUrl,
			networking: {
				gatewayClass: "contour",
				gatewayNamespace: namespaces.gatewayName,
				gatewayPorts: k0sGatewayPorts,
			},
			images: {
				controllers: images.controllersImage,
				api: images.apiImage,
				migration: images.migrationImage,
			},
			extraValues: {
				helm: { hooksImage: "alpine/k8s:1.36.4" },
				api: {
					nodeSelector: korifiPlacement.nodeSelector,
					tolerations: korifiPlacement.tolerations,
				},
				controllers: {
					nodeSelector: korifiPlacement.nodeSelector,
					tolerations: korifiPlacement.tolerations,
				},
				migration: {
					nodeSelector: korifiPlacement.nodeSelector,
					tolerations: korifiPlacement.tolerations,
				},
			},
		},
		dependsOn: [
			dependencies.job,
			registry.release,
			cfPullSecret,
			korifiPullSecret,
			namespaces.gateway,
			uaa.proxyService,
			images.loaded,
			platformPlacement.patched,
		],
	},
	{ dependsOn: [dependencies, registry, uaa, images, platformPlacement] },
);

const knative = new KnativeServing(
	"knative",
	{
		provider: cluster.provider,
		domain: appDomain,
		korifiNamespace: namespaces.korifiName,
		rootNamespace: namespaces.rootName,
		installRunnerSupport: false,
		dependsOn: [korifi.release, dependencies.job],
	},
	{ dependsOn: [korifi] },
);

new k8s.rbac.v1.ClusterRoleBinding(
	"uaa-admin-cluster-admin",
	{
		metadata: { name: "uaa-admin-cluster-admin" },
		roleRef: {
			apiGroup: "rbac.authorization.k8s.io",
			kind: "ClusterRole",
			name: "cluster-admin",
		},
		subjects: [
			{
				apiGroup: "rbac.authorization.k8s.io",
				kind: "User",
				name: adminUserName,
			},
		],
	},
	{ provider: cluster.provider, dependsOn: [cluster] },
);

const gateway = new ContourGateway(
	"contour",
	{
		provider: cluster.provider,
		publishType: "NodePortService",
		nodeSelector: korifiPlacement.nodeSelector,
		tolerations: korifiPlacement.tolerations,
		dependsOn: [korifi.release, platformPlacement.patched],
	},
	{ dependsOn: [korifi, platformPlacement] },
);

const everestOperatorBundles = new K0sEverestOperatorBundles(
	"everest-operator-bundles",
	{
		clusterName,
		dependsOn: [cluster, prefetchedImages.servicesLoaded],
	},
	{ dependsOn: [cluster, prefetchedImages.servicesLoaded] },
);

const brokerServices = new ServiceBrokerServices(
	"broker-services",
	{
		provider: cluster.provider,
		kindClusterName: clusterName,
		kubeconfigPath,
		hostScheduling: osbPlacement,
		aigatewayBackends: aiGatewayBackends,
		enable: { postgres: true, aigateway: true },
		dependsOn: [korifi.release, everestOperatorBundles.loaded],
	},
	{ dependsOn: [korifi, everestOperatorBundles] },
);

const osbImage = new K0sOsbBrokerImage(
	"osb-broker-image",
	{
		nodeContainers,
		sourcePath: osbServicePath(repoRoot),
		dependsOn: [cluster],
	},
	{ dependsOn: [cluster] },
);

const osbBroker = new OsbServiceBroker(
	"osb-service",
	{
		provider: cluster.provider,
		image: osbImage.image,
		imagePullPolicy: "Never",
		nodeSelector: osbPlacement.nodeSelector,
		tolerations: osbPlacement.tolerations,
		backends: {
			everest: brokerServices.everest,
			aigateway: brokerServices.aigateway,
		},
		rootNamespace: namespaces.rootName,
		dependsOn: [korifi.release, osbImage.loaded, storage],
	},
	{ dependsOn: [brokerServices, osbImage, korifi, storage] },
);

export const kubeconfig = kubeconfigPath;
export const cfApiUrl = `https://${apiUrl}`;
export const appsDomain = `*.${appDomain}`;
export const orgHint = "cf create-org org && cf create-space -o org space";
export const authHint = `cf api ${cfApiUrl} --skip-ssl-validation && cf login -u ${adminEmail} -p "$(pulumi stack output uaaAdminPassword --show-secrets)"`;
export const uaaIssuerUrl = uaaUrl;
export const uaaAdminEmail = adminEmail;
export const uaaAdminPassword = pulumi.secret(uaa.adminPassword);
export const cfAdminUserName = adminUserName;
export const gatewayClass = gateway.gatewayClass.metadata.name;
export const registryHost = registry.clusterHost;
export const knativeServing = knative.serving.metadata.name;
export const controllersImage = images.controllersImage;
export const apiImage = images.apiImage;
export const k0sNodes = cluster.nodeContainers;

export const everest = brokerServices.everest
	? {
			namespace: brokerServices.everest.namespace,
			hostNamespace: brokerServices.everest.hostNamespace,
			vclusterName: brokerServices.everest.vclusterName,
		}
	: undefined;

export const aigateway = brokerServices.aigateway
	? {
			namespace: brokerServices.aigateway.namespace,
			hostNamespace: brokerServices.aigateway.hostNamespace,
			vclusterName: brokerServices.aigateway.vclusterName,
		}
	: undefined;

export const osbBrokerUrl = osbBroker.url;
export const osbServiceImage = osbImage.image;
export const marketplaceHint =
	"cf enable-service-access postgres && cf enable-service-access mysql && cf enable-service-access mongodb && cf enable-service-access ozone && cf enable-service-access nats && cf enable-service-access opensearch && cf enable-service-access redis && cf enable-service-access aigateway && cf marketplace && cf create-service postgres dedicated mydb";
