import { expect, test } from "bun:test";
import {
	mergeDeep,
	nodeRoleKey,
	placementFor,
	vclusterSchedulingValues,
} from "./node-placement";

test("placementFor labels and taints a node role", () => {
	const placement = placementFor("osb");
	expect(placement.nodeSelector).toEqual({ [nodeRoleKey]: "osb" });
	expect(placement.tolerations).toEqual([
		{
			key: nodeRoleKey,
			operator: "Equal",
			value: "osb",
			effect: "NoSchedule",
		},
	]);
});

test("vclusterSchedulingValues pin the control plane and synced pods", () => {
	const values = vclusterSchedulingValues(placementFor("korifi"));
	expect(values).toEqual({
		controlPlane: {
			statefulSet: {
				scheduling: {
					nodeSelector: { [nodeRoleKey]: "korifi" },
					tolerations: [
						{
							key: nodeRoleKey,
							operator: "Equal",
							value: "korifi",
							effect: "NoSchedule",
						},
					],
				},
			},
		},
		sync: {
			fromHost: {
				nodes: {
					enabled: true,
					selector: { labels: { [nodeRoleKey]: "korifi" } },
				},
			},
			toHost: {
				pods: {
					enforceTolerations: [
						`${nodeRoleKey}=korifi:NoSchedule`,
					],
				},
			},
		},
	});
});

test("mergeDeep keeps existing nested keys", () => {
	const merged = mergeDeep(
		{ sync: { toHost: { services: { enabled: true } } } },
		vclusterSchedulingValues(placementFor("osb")),
	);
	expect(
		(merged.sync as Record<string, unknown>).toHost,
	).toEqual({
		services: { enabled: true },
		pods: {
			enforceTolerations: [`${nodeRoleKey}=osb:NoSchedule`],
		},
	});
});
