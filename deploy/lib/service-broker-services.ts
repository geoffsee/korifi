/**
 * Platform backends OSB offerings provision against.
 *
 * Databases are not shared clusters. OpenEverest (vcluster) installs the
 * PostgreSQL, PXC, and PSMDB operators; osb-service creates one
 * DatabaseCluster per CF service instance.
 *
 * Envoy AI Gateway (separate vcluster) routes configured models to external
 * backends; osb-service issues one tenant API key per CF service instance.
 */
import * as k8s from "@pulumi/kubernetes";
import * as pulumi from "@pulumi/pulumi";
import {
	AIGatewayVcluster,
	type AIGatewayBackend,
} from "./aigateway-vcluster";
import { EverestVcluster } from "./everest-vcluster";
import type { NodePlacement } from "./node-placement";

/** Generic connection facts for a custom broker backend. */
export interface ServiceBrokerServiceConnection {
	host: string;
	port: number;
	adminUser: string;
	adminPassword: pulumi.Output<string>;
	adminUrl?: pulumi.Output<string>;
	resources: pulumi.Resource[];
}

/** In-cluster access to the Everest vcluster API. */
export interface EverestConnection {
	kubeconfig: pulumi.Output<string>;
	/** Namespace inside the vcluster where DatabaseClusters are created. */
	namespace: string;
	/** Host-cluster namespace that holds synced Services. */
	hostNamespace: string;
	vclusterName: string;
	resources: pulumi.Resource[];
}

/** In-cluster access to the Envoy AI Gateway vcluster API. */
export interface AIGatewayConnection {
	kubeconfig: pulumi.Output<string>;
	/** Namespace inside the vcluster that holds the Gateway and tenant secrets. */
	namespace: string;
	hostNamespace: string;
	vclusterName: string;
	resources: pulumi.Resource[];
}

export interface ServiceBrokerServicesArgs {
	provider: k8s.Provider;
	/** Kind cluster name; used to reach the Everest vcluster API. */
	kindClusterName: string;
	kubeconfigPath?: string;
	hostScheduling?: NodePlacement;
	/** External OpenAI-compatible backends and their model associations. */
	aigatewayBackends?: AIGatewayBackend[];
	enable?: {
		postgres?: boolean;
		aigateway?: boolean;
	};
	dependsOn?: pulumi.Input<pulumi.Resource>[];
}

export class ServiceBrokerServices extends pulumi.ComponentResource {
	readonly everest?: EverestConnection;
	readonly aigateway?: AIGatewayConnection;

	constructor(
		name: string,
		args: ServiceBrokerServicesArgs,
		opts?: pulumi.ComponentResourceOptions,
	) {
		super("korifi:deploy:ServiceBrokerServices", name, {}, opts);

		const enable = {
			postgres: true,
			aigateway: true,
			...args.enable,
		};

		if (enable.postgres) {
			const everest = new EverestVcluster(
				`${name}-everest`,
				{
					provider: args.provider,
					kindClusterName: args.kindClusterName,
					kubeconfigPath: args.kubeconfigPath,
					hostScheduling: args.hostScheduling,
					dependsOn: args.dependsOn,
				},
				{ parent: this },
			);
			this.everest = {
				kubeconfig: everest.inClusterKubeconfig,
				namespace: everest.dbNamespace,
				hostNamespace: everest.namespace,
				vclusterName: everest.vclusterName,
				resources: [everest],
			};
		}

		if (enable.aigateway) {
			if (!args.aigatewayBackends) {
				throw new Error(
					"aigatewayBackends is required when the AI Gateway is enabled",
				);
			}
			const aigateway = new AIGatewayVcluster(
				`${name}-aigateway`,
				{
					provider: args.provider,
					kindClusterName: args.kindClusterName,
					kubeconfigPath: args.kubeconfigPath,
					hostScheduling: args.hostScheduling,
					backends: args.aigatewayBackends,
					dependsOn: args.dependsOn,
				},
				{ parent: this },
			);
			this.aigateway = {
				kubeconfig: aigateway.inClusterKubeconfig,
				namespace: aigateway.gatewayNamespace,
				hostNamespace: aigateway.namespace,
				vclusterName: aigateway.vclusterName,
				resources: [aigateway],
			};
		}

		this.registerOutputs({
			everestNamespace: this.everest?.namespace,
			aigatewayNamespace: this.aigateway?.namespace,
		});
	}
}

export function defaultServiceBrokerServiceEnable(): Required<
	NonNullable<ServiceBrokerServicesArgs["enable"]>
> {
	return { postgres: true, aigateway: true };
}
