import { expect, test } from "bun:test";
import * as fs from "node:fs";
import * as path from "node:path";

const text = fs.readFileSync(path.join(import.meta.dir, "k0s-images.ts"), "utf8");

test("k0s images build controllers, api, and migration then ctr-import them", () => {
	expect(text).toContain("docker build -f controllers/Dockerfile");
	expect(text).toContain("docker build -f api/Dockerfile");
	expect(text).toContain("docker build -f migration/Dockerfile");
	expect(text).toContain("k0s ctr images import");
	expect(text).not.toContain("kind load docker-image");
	expect(text).toContain("korifi-controllers:");
	expect(text).toContain("k0s-");
	expect(text).not.toContain("cloudfoundry/korifi-controllers:latest");
});
