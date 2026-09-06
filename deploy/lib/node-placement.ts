/**
 * Node role labels/taints used by the k0s 3-node local cluster.
 *
 * korifi and osb nodes are NoSchedule-tainted; knative is left untainted so
 * Knative Serving, kpack builds, and CF app revisions schedule there by default.
 */
export const nodeRoleKey = "korifi.cloudfoundry.org/node-role";

export type NodeRole = "korifi" | "osb" | "knative";

export interface NodeToleration {
	key: string;
	operator: "Equal";
	value: string;
	effect: "NoSchedule";
}

export interface NodePlacement {
	nodeSelector: Record<string, string>;
	tolerations: NodeToleration[];
}

export function placementFor(role: NodeRole): NodePlacement {
	return {
		nodeSelector: { [nodeRoleKey]: role },
		tolerations: [
			{
				key: nodeRoleKey,
				operator: "Equal",
				value: role,
				effect: "NoSchedule",
			},
		],
	};
}

/** Helm/vcluster values that pin the virtual control plane and synced pods. */
export function vclusterSchedulingValues(
	placement: NodePlacement,
): Record<string, unknown> {
	const taint = placement.tolerations[0];
	return {
		controlPlane: {
			statefulSet: {
				scheduling: {
					nodeSelector: placement.nodeSelector,
					tolerations: placement.tolerations,
				},
			},
		},
		sync: {
			fromHost: {
				nodes: {
					enabled: true,
					selector: { labels: placement.nodeSelector },
				},
			},
			toHost: {
				pods: {
					enforceTolerations: [
						`${taint.key}=${taint.value}:${taint.effect}`,
					],
				},
			},
		},
	};
}

export function mergeDeep(
	target: Record<string, unknown>,
	source: Record<string, unknown>,
): Record<string, unknown> {
	for (const [key, value] of Object.entries(source)) {
		if (
			value !== null &&
			typeof value === "object" &&
			!Array.isArray(value) &&
			typeof target[key] === "object" &&
			target[key] !== null &&
			!Array.isArray(target[key])
		) {
			mergeDeep(
				target[key] as Record<string, unknown>,
				value as Record<string, unknown>,
			);
		} else {
			target[key] = value;
		}
	}
	return target;
}
