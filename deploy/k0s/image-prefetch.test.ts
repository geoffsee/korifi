import { describe, expect, test } from "bun:test";
import { k0sCoreImages, k0sServiceImages } from "./image-prefetch";

describe("k0s image prefetch manifests", () => {
	test("contain unique explicit image references", () => {
		const images = [...k0sCoreImages, ...k0sServiceImages];
		expect(images.length).toBeGreaterThan(40);
		expect(new Set(images).size).toBe(images.length);
		for (const image of images) {
			expect(image).toMatch(/^[^\s]+(?:[:@])[^\s]+$/);
		}
	});

	test("include the heavyweight dedicated-service images", () => {
		expect(k0sServiceImages).toContain("docker.io/apache/ozone:2.0.0");
		expect(k0sServiceImages).toContain(
			"docker.io/opensearchproject/opensearch:2.19.2",
		);
		expect(k0sServiceImages).toContain(
			"docker.io/percona/percona-xtradb-cluster:8.0.39-30.1",
		);
	});
});
