/**
 * Sibling docker-save tarballs used by the k0s stack instead of registry pulls.
 *
 * Layout (next to the korifi checkout):
 *   image-archives/          linux/arm64
 *   image-archives-amd64/    linux/amd64
 *
 * Override with KORIFI_IMAGE_ARCHIVES.
 */
import * as fs from "node:fs";
import * as path from "node:path";

const repoParent = path.resolve(__dirname, "..", "..", "..");

export function defaultImageArchivesDir(): string {
	const fromEnv = process.env.KORIFI_IMAGE_ARCHIVES?.trim();
	if (fromEnv) {
		return fromEnv;
	}
	const dirName =
		process.arch === "arm64" ? "image-archives" : "image-archives-amd64";
	return path.join(repoParent, dirName);
}

export function imageArchivesManifestPath(archivesDir: string): string {
	return path.join(archivesDir, "manifest.tsv");
}

/** Filename in `tars/` for an image ref, if present in manifest.tsv. */
export function tarNameForImage(
	archivesDir: string,
	imageRef: string,
): string | undefined {
	const manifestPath = imageArchivesManifestPath(archivesDir);
	if (!fs.existsSync(manifestPath)) {
		return undefined;
	}
	for (const line of fs.readFileSync(manifestPath, "utf8").split("\n")) {
		if (line === "" || line.startsWith("image_file")) {
			continue;
		}
		const [file, ref] = line.split("\t");
		if (file && ref === imageRef) {
			return file;
		}
	}
	return undefined;
}

export function tarPathForImage(
	archivesDir: string,
	imageRef: string,
): string | undefined {
	const name = tarNameForImage(archivesDir, imageRef);
	if (!name) {
		return undefined;
	}
	const tarPath = path.join(archivesDir, "tars", name);
	return fs.existsSync(tarPath) ? tarPath : undefined;
}
