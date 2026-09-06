import { describe, expect, test } from "bun:test";
import { kindCoreImages, kindServiceImages } from "./image-prefetch";

describe("Kind image prefetch manifests", () => {
	test("contain unique explicit image references", () => {
		const images = [...kindCoreImages, ...kindServiceImages];
		expect(images.length).toBeGreaterThan(40);
		expect(new Set(images).size).toBe(images.length);
		for (const image of images) {
			expect(image).toMatch(/^[^\s]+(?:[:@])[^\s]+$/);
		}
	});

	test("include the heavyweight dedicated-service images", () => {
		expect(kindServiceImages).toContain("docker.io/apache/ozone:2.0.0");
		expect(kindServiceImages).toContain(
			"docker.io/opensearchproject/opensearch:2.19.2",
		);
		expect(kindServiceImages).toContain(
			"docker.io/percona/percona-xtradb-cluster:8.0.39-30.1",
		);
	});
});
