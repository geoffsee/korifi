#!/usr/bin/env bash

set -euo pipefail

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
cluster_name=${KIND_CLUSTER:-korifi}
parallelism=${PREFETCH_JOBS:-6}
image_files=()

usage() {
	cat <<'EOF'
Usage: prefetch-images.sh [--cluster NAME] [--images FILE] [--jobs COUNT]

Pull each image directly into every node in an existing Kind cluster. When no
--images option is supplied, both checked-in Korifi image manifests are used.
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

if ((${#image_files[@]} == 0)); then
	image_files=(
		"$script_dir/prefetch-core-images.txt"
		"$script_dir/prefetch-service-images.txt"
	)
fi

for image_file in "${image_files[@]}"; do
	if [[ ! -f $image_file ]]; then
		echo "image manifest not found: $image_file" >&2
		exit 2
	fi
done

if ! kind get clusters 2>/dev/null | grep -Fxq "$cluster_name"; then
	echo "Kind cluster not found: $cluster_name" >&2
	exit 2
fi

nodes=()
while IFS= read -r node; do
	[[ -n $node ]] && nodes+=("$node")
done < <(kind get nodes --name "$cluster_name")

if ((${#nodes[@]} == 0)); then
	echo "Kind cluster has no nodes: $cluster_name" >&2
	exit 2
fi

images=()
while IFS= read -r image; do
	[[ -n $image ]] && images+=("$image")
done < <(awk '!/^[[:space:]]*(#|$)/ {print $1}' "${image_files[@]}" | sort -u)

if ((${#images[@]} == 0)); then
	echo "no images found in manifests" >&2
	exit 2
fi

pull_image() {
	local node=$1
	local image=$2
	if docker exec "$node" crictl inspecti "$image" >/dev/null 2>&1; then
		printf 'cached  %s  %s\n' "$node" "$image" >&2
		return
	fi
	printf 'pulling %s  %s\n' "$node" "$image" >&2
	docker exec "$node" crictl pull "$image" >/dev/null
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
		pull_image "$node" "$image" &
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
	echo "one or more images failed to preload" >&2
	exit "$status"
fi

printf 'preloaded %d images into %d Kind node(s)\n' "${#images[@]}" "${#nodes[@]}" >&2
