import { expect, test } from "bun:test";
import * as fs from "node:fs";
import * as path from "node:path";
import { everestOperatorBundleImages } from "./everest-operator-bundles";

const text = fs.readFileSync(
	path.join(import.meta.dir, "everest-operator-bundles.ts"),
	"utf8",
);

test("preloads the pinned OpenEverest OLM bundles into every kind node", () => {
	expect(everestOperatorBundleImages).toHaveLength(3);
	for (const image of everestOperatorBundleImages) {
		expect(image).toMatch(/^docker\.io\/percona\/.+:[\d.]+-community-bundle$/);
	}
	expect(text).toContain('kind get nodes --name "$KIND_CLUSTER"');
	expect(text).toContain('docker exec "$NODE" crictl pull "$IMAGE"');
});
