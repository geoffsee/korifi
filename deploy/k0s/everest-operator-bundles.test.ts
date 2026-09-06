import { expect, test } from "bun:test";
import * as fs from "node:fs";
import * as path from "node:path";
import { everestOperatorBundleImages } from "./everest-operator-bundles";

const text = fs.readFileSync(
	path.join(import.meta.dir, "everest-operator-bundles.ts"),
	"utf8",
);

test("preloads the pinned OpenEverest OLM bundles into k0s nodes", () => {
	expect(everestOperatorBundleImages).toHaveLength(3);
	for (const image of everestOperatorBundleImages) {
		expect(image).toMatch(/^docker\.io\/percona\/.+:[\d.]+-community-bundle$/);
	}
	expect(text).toContain("prefetch-images.sh");
	expect(text).toContain("KORIFI_IMAGE_ARCHIVES");
	expect(text).not.toContain("k0s ctr images pull");
	expect(text).not.toContain("kind get nodes");
});
