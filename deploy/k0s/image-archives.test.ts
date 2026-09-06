import { expect, test } from "bun:test";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import {
	defaultImageArchivesDir,
	tarPathForImage,
} from "./image-archives";

test("default archives dir is the arch-specific sibling of this checkout", () => {
	const dir = defaultImageArchivesDir();
	if (process.arch === "arm64") {
		expect(dir.endsWith(`${path.sep}image-archives`)).toBe(true);
	} else {
		expect(dir.endsWith(`${path.sep}image-archives-amd64`)).toBe(true);
	}
	expect(dir).not.toContain("/korifi/korifi/");
});

test("resolves an image ref from manifest.tsv to a tarball", () => {
	const dir = fs.mkdtempSync(path.join(os.tmpdir(), "k0s-archives-"));
	fs.mkdirSync(path.join(dir, "tars"));
	fs.writeFileSync(
		path.join(dir, "manifest.tsv"),
		[
			"image_file\tref\tsource",
			"docker.io_library_registry_3.0.0.tar\tdocker.io/library/registry:3.0.0\tdocker-pull",
			"",
		].join("\n"),
	);
	const tar = path.join(dir, "tars", "docker.io_library_registry_3.0.0.tar");
	fs.writeFileSync(tar, "stub");
	expect(tarPathForImage(dir, "docker.io/library/registry:3.0.0")).toBe(tar);
	expect(tarPathForImage(dir, "docker.io/missing:tag")).toBeUndefined();
	fs.rmSync(dir, { recursive: true, force: true });
});
