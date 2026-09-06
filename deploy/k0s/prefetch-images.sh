#!/usr/bin/env bash

set -euo pipefail

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
cluster_name=${K0S_CLUSTER:-korifi}
parallelism=${PREFETCH_JOBS:-6}
image_files=()
archives_dir=${KORIFI_IMAGE_ARCHIVES:-}

usage() {
	cat <<'EOF'
Usage: prefetch-images.sh [--cluster NAME] [--images FILE] [--jobs COUNT] [--archives DIR]

Import each image from sibling image-archives tarballs into every node of an
existing k0s-in-docker cluster (no registry pull). When no --images option is
supplied, both Kind image manifests are used.
EOF
}

while (($# > 0)); do
	case "$1" in
		--cluster)
			cluster_name=${2:?missing cluster name}
			shift 2
			;;
		--images)
			image_files+=("${2:?missing image manifest}")
			shift 2
			;;
		--jobs)
			parallelism=${2:?missing job count}
			shift 2
			;;
		--archives)
			archives_dir=${2:?missing archives directory}
			shift 2
			;;
		-h | --help)
			usage
			exit 0
			;;
		*)
			echo "unknown argument: $1" >&2
			usage >&2
			exit 2
			;;
	esac
done

if [[ ! $parallelism =~ ^[1-9][0-9]*$ ]]; then
	echo "--jobs must be a positive integer" >&2
	exit 2
fi

kind_dir=$(cd "$script_dir/../kind" && pwd)
if ((${#image_files[@]} == 0)); then
	image_files=(
		"$kind_dir/prefetch-core-images.txt"
		"$kind_dir/prefetch-service-images.txt"
	)
fi

for image_file in "${image_files[@]}"; do
	if [[ ! -f $image_file ]]; then
		echo "image manifest not found: $image_file" >&2
		exit 2
	fi
done

if [[ -z $archives_dir ]]; then
	repo_parent=$(cd "$script_dir/../../.." && pwd)
	case $(uname -m) in
		arm64 | aarch64) archives_dir=$repo_parent/image-archives ;;
		*) archives_dir=$repo_parent/image-archives-amd64 ;;
	esac
fi

manifest=$archives_dir/manifest.tsv
tars=$archives_dir/tars
if [[ ! -f $manifest || ! -d $tars ]]; then
	echo "image archives not found at $archives_dir" >&2
	echo "set KORIFI_IMAGE_ARCHIVES to the directory that contains manifest.tsv and tars/" >&2
	exit 2
fi

nodes=(
	"${cluster_name}-korifi"
	"${cluster_name}-osb"
	"${cluster_name}-knative"
)

for node in "${nodes[@]}"; do
	if ! docker inspect "$node" >/dev/null 2>&1; then
		echo "k0s node container not found: $node" >&2
		exit 2
	fi
done

images=()
while IFS= read -r image; do
	[[ -n $image ]] && images+=("$image")
done < <(awk '!/^[[:space:]]*(#|$)/ {print $1}' "${image_files[@]}" | sort -u)

if ((${#images[@]} == 0)); then
	echo "no images found in manifests" >&2
	exit 2
fi

tar_for_image() {
	local image=$1
	local file
	file=$(awk -F'\t' -v img="$image" 'NR > 1 && $2 == img { print $1; exit }' "$manifest")
	if [[ -z $file ]]; then
		return 1
	fi
	printf '%s/%s\n' "$tars" "$file"
}

import_image() {
	local node=$1
	local image=$2
	local tar
	if ! tar=$(tar_for_image "$image"); then
		echo "no image-archives tarball for $image (looked in $manifest)" >&2
		return 1
	fi
	if [[ ! -f $tar ]]; then
		echo "missing tarball $tar for $image" >&2
		return 1
	fi
	printf 'import  %s  %s\n' "$node" "$image" >&2
	docker exec -i "$node" k0s ctr images import - <"$tar" >/dev/null
	printf 'loaded  %s  %s\n' "$node" "$image" >&2
}

pids=()
wait_batch() {
	local failed=0
	local pid
	for pid in "${pids[@]}"; do
		if ! wait "$pid"; then
			failed=1
		fi
	done
	pids=()
	return "$failed"
}

status=0
for node in "${nodes[@]}"; do
	for image in "${images[@]}"; do
		import_image "$node" "$image" &
		pids+=("$!")
		if ((${#pids[@]} >= parallelism)); then
			if ! wait_batch; then
				status=1
			fi
		fi
	done
done

if ((${#pids[@]} > 0)) && ! wait_batch; then
	status=1
fi

if ((status != 0)); then
	echo "one or more images failed to preload from image-archives" >&2
	exit "$status"
fi

printf 'preloaded %d images into %d k0s node(s) from %s\n' "${#images[@]}" "${#nodes[@]}" "$archives_dir" >&2
