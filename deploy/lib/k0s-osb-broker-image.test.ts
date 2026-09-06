import { expect, test } from "bun:test";
import * as fs from "node:fs";
import * as path from "node:path";

const text = fs.readFileSync(
	path.join(import.meta.dir, "k0s-osb-broker-image.ts"),
	"utf8",
);

test("k0s OSB image builds the osb-service Dockerfile and ctr-imports it", () => {
	expect(text).toContain("docker build -f image/Dockerfile");
	expect(text).toContain("k0s ctr images import");
	expect(text).not.toContain("kind load docker-image");
	expect(text).toContain("osb-service:");
	expect(text).toContain("osb-service sources not found");
});
